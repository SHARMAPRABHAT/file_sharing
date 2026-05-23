package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"mime/multipart"
	"net/http"
	"net/smtp"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/gin-gonic/gin"
	razorpay "github.com/razorpay/razorpay-go"
)

type Content struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Price       int    `json:"price"`
	FilePath    string `json:"-"`
}

type CreateOrderReq struct {
	ContentID string `json:"content_id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Mobile    string `json:"mobile"`
}

type CreateOrderResp struct {
	OrderID      string `json:"order_id"`
	KeyID        string `json:"key_id"`
	Amount       int    `json:"amount"`
	Currency     string `json:"currency"`
	ContentTitle string `json:"content_title"`
}

type VerifyPaymentReq struct {
	ContentID         string `json:"content_id"`
	Name              string `json:"name"`
	Email             string `json:"email"`
	RazorpayOrderID   string `json:"order_id"`
	RazorpayPaymentID string `json:"payment_id"`
	RazorpaySignature string `json:"signature"`
}

type VerifyPaymentResp struct {
	Status    string `json:"status"`
	Message   string `json:"message"`
	AccessURL string `json:"access_url,omitempty"`
}

type AccessToken struct {
	Token     string
	ContentID string
	Email     string
	ExpiresAt time.Time
	Used      bool
	PaymentID string
	CreatedAt time.Time
}

var contents = map[string]Content{
	"abc123": {
		ID:          "abc123",
		Title:       "Kubernetes Notes",
		Description: "Concise notes to revise Kubernetes quickly (PDF).",
		Price:       99,
		FilePath:    "./assets/abc123.pdf",
	},
	"sample123": {
		ID:          "sample123",
		Title:       "Golang Concurrency Cheatsheet",
		Description: "Channels, worker pools, select, and common pitfalls (PDF).",
		Price:       79,
		FilePath:    "./assets/sample123.pdf",
	},
}

var (
	tokenStore = map[string]AccessToken{}
	tokenMu    sync.Mutex
)

func main() {
	port := getenv("APP_PORT", "8080")
	baseURL := getenv("APP_BASE_URL", "http://localhost:"+port)
	ttlMinutes := getenvInt("TOKEN_TTL_MINUTES", 30)
	tokenTTL := time.Duration(ttlMinutes) * time.Minute

	keyID := getenv("RAZORPAY_KEY_ID", "")
	keySecret := getenv("RAZORPAY_KEY_SECRET", "")
	smtpHost := getenv("SMTP_HOST", "")
	smtpPort := getenv("SMTP_PORT", "587")
	smtpUser := getenv("SMTP_USERNAME", "")
	smtpPass := getenv("SMTP_PASSWORD", "")
	smtpFrom := getenv("SMTP_FROM", "")

	var razorpayClient *razorpay.Client
	if keyID != "" && keySecret != "" {
		razorpayClient = razorpay.NewClient(keyID, keySecret)
		log.Printf("✅ Razorpay client configured")
	} else {
		log.Printf("⚠️ Razorpay env vars not set; create-order will return an error until configured")
	}

	if smtpHost == "" || smtpFrom == "" {
		log.Printf("⚠️ SMTP is not fully configured; payment verification will fail until SMTP_HOST and SMTP_FROM are set")
	}

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	r.GET("/health", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	r.Static("/static", "./web/static")

	r.GET("/buy/:contentId", func(c *gin.Context) {
		c.File("./web/static/buy.html")
	})

	r.GET("/api/content/:contentId", func(c *gin.Context) {
		id := c.Param("contentId")
		content, ok := contents[id]
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "content not found"})
			return
		}
		c.JSON(http.StatusOK, content)
	})

	r.POST("/api/create-order", func(c *gin.Context) {
		var req CreateOrderReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
			return
		}

		req.ContentID = strings.TrimSpace(req.ContentID)
		req.Name = strings.TrimSpace(req.Name)
		req.Email = strings.TrimSpace(req.Email)
		req.Mobile = strings.TrimSpace(req.Mobile)

		if req.ContentID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "content_id is required"})
			return
		}
		content, ok := contents[req.ContentID]
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "content not found"})
			return
		}
		if len(req.Name) < 2 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
			return
		}
		if !strings.Contains(req.Email, "@") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "valid email is required"})
			return
		}
		if !isValidMobile10(req.Mobile) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "valid 10-digit mobile is required"})
			return
		}
		if razorpayClient == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Razorpay is not configured"})
			return
		}

		params := map[string]interface{}{
			"amount":   content.Price * 100,
			"currency": "INR",
			"receipt":  "purchase_" + req.ContentID + "_" + randHex(6),
		}

		body, err := razorpayClient.Order.Create(params, nil)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create Razorpay order"})
			return
		}

		orderID, _ := body["id"].(string)
		if orderID == "" {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid Razorpay response"})
			return
		}

		c.JSON(http.StatusOK, CreateOrderResp{
			OrderID:      orderID,
			KeyID:        keyID,
			Amount:       content.Price * 100,
			Currency:     "INR",
			ContentTitle: content.Title,
		})
	})

	r.POST("/api/verify-payment", func(c *gin.Context) {
		var req VerifyPaymentReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
			return
		}

		req.RazorpayOrderID = strings.TrimSpace(req.RazorpayOrderID)
		req.RazorpayPaymentID = strings.TrimSpace(req.RazorpayPaymentID)
		req.RazorpaySignature = strings.TrimSpace(req.RazorpaySignature)
		if req.RazorpayOrderID == "" || req.RazorpayPaymentID == "" || req.RazorpaySignature == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "order_id, payment_id, and signature are required"})
			return
		}
		if keySecret == "" {
			c.JSON(http.StatusInternalServerError, VerifyPaymentResp{Status: "failed", Message: "Razorpay is not configured"})
			return
		}
		if smtpHost == "" || smtpFrom == "" {
			c.JSON(http.StatusInternalServerError, VerifyPaymentResp{Status: "failed", Message: "SMTP is not configured"})
			return
		}
		if !verifySignature(req.RazorpayOrderID, req.RazorpayPaymentID, req.RazorpaySignature, keySecret) {
			c.JSON(http.StatusUnauthorized, VerifyPaymentResp{Status: "failed", Message: "invalid payment signature"})
			return
		}

		contentID := strings.TrimSpace(req.ContentID)
		buyerName := strings.TrimSpace(req.Name)
		buyerEmail := strings.TrimSpace(req.Email)
		if contentID == "" || buyerEmail == "" {
			c.JSON(http.StatusBadRequest, VerifyPaymentResp{Status: "failed", Message: "content_id and email are required"})
			return
		}
		if buyerName == "" {
			buyerName = "there"
		}

		content, ok := contents[contentID]
		if !ok {
			c.JSON(http.StatusNotFound, VerifyPaymentResp{Status: "failed", Message: "content not found"})
			return
		}

		token := generateToken()
		expiresAt := time.Now().Add(tokenTTL)
		tokenMu.Lock()
		tokenStore[token] = AccessToken{
			Token:     token,
			ContentID: contentID,
			Email:     buyerEmail,
			ExpiresAt: expiresAt,
			PaymentID: req.RazorpayPaymentID,
			CreatedAt: time.Now(),
		}
		tokenMu.Unlock()

		accessURL := fmt.Sprintf("%s/access/%s", strings.TrimRight(baseURL, "/"), token)
		if err := sendDeliveryEmail(smtpHost, smtpPort, smtpUser, smtpPass, smtpFrom, buyerEmail, buyerName, content, accessURL, expiresAt); err != nil {
			c.JSON(http.StatusInternalServerError, VerifyPaymentResp{Status: "failed", Message: "failed to send delivery email: " + err.Error()})
			return
		}

		c.JSON(http.StatusOK, VerifyPaymentResp{Status: "ok", Message: "payment verified", AccessURL: accessURL})
	})

	r.GET("/access/:token", func(c *gin.Context) {
		token := c.Param("token")
		tokenMu.Lock()
		accessToken, ok := tokenStore[token]
		if !ok {
			tokenMu.Unlock()
			c.JSON(http.StatusNotFound, gin.H{"error": "invalid or expired token"})
			return
		}
		if accessToken.Used {
			tokenMu.Unlock()
			c.JSON(http.StatusGone, gin.H{"error": "token already used"})
			return
		}
		if time.Now().After(accessToken.ExpiresAt) {
			delete(tokenStore, token)
			tokenMu.Unlock()
			c.JSON(http.StatusNotFound, gin.H{"error": "invalid or expired token"})
			return
		}
		accessToken.Used = true
		tokenStore[token] = accessToken
		tokenMu.Unlock()

		content, ok := contents[accessToken.ContentID]
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "content not found"})
			return
		}
		if _, err := os.Stat(content.FilePath); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "file not available"})
			return
		}
		c.Header("Content-Type", "application/pdf")
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filepath.Base(content.FilePath)))
		c.File(content.FilePath)
	})

	r.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/buy/sample123")
	})

	addr := ":" + port
	log.Printf("✅ Gin server started: http://localhost%s", addr)
	log.Printf("✅ Try: http://localhost%s/buy/abc123", addr)
	log.Printf("✅ API:  http://localhost%s/api/content/abc123", addr)

	if err := r.Run(addr); err != nil {
		log.Fatal(err)
	}
}

func getenv(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}

func getenvInt(key string, fallback int) int {
	value := getenv(key, "")
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func randHex(nBytes int) string {
	b := make([]byte, nBytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func generateToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func verifySignature(orderID, paymentID, signature, secret string) bool {
	payload := orderID + "|" + paymentID
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

func isValidMobile10(m string) bool {
	if len(m) != 10 {
		return false
	}
	for _, ch := range m {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func sendDeliveryEmail(smtpHost, smtpPort, smtpUser, smtpPass, smtpFrom, to, buyerName string, content Content, accessURL string, expiresAt time.Time) error {
	if smtpHost == "" || smtpFrom == "" {
		return fmt.Errorf("SMTP is not configured")
	}
	if buyerName == "" {
		buyerName = "there"
	}

	payload, err := os.ReadFile(content.FilePath)
	if err != nil {
		return fmt.Errorf("failed to read attachment: %w", err)
	}

	templateText, err := os.ReadFile("./web/email_templates/purchase_success.txt")
	if err != nil {
		return fmt.Errorf("failed to read email template: %w", err)
	}

	tmpl, err := template.New("purchase_success").Parse(string(templateText))
	if err != nil {
		return fmt.Errorf("failed to parse email template: %w", err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	textPart, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {"text/plain; charset=utf-8"},
		"Content-Transfer-Encoding": {"7bit"},
	})
	if err != nil {
		return fmt.Errorf("failed to create text part: %w", err)
	}

	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, map[string]string{
		"BuyerName": buyerName,
		"Title":     content.Title,
		"AccessURL": accessURL,
		"ExpiresAt": expiresAt.Format(time.RFC1123),
	}); err != nil {
		return fmt.Errorf("failed to render email template: %w", err)
	}
	if _, err := textPart.Write(rendered.Bytes()); err != nil {
		return fmt.Errorf("failed to write text part: %w", err)
	}

	encodedPayload := []byte(base64Encode(payload))
	filePart, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {"application/pdf"},
		"Content-Disposition":       {fmt.Sprintf(`attachment; filename="%s"`, filepath.Base(content.FilePath))},
		"Content-Transfer-Encoding": {"base64"},
	})
	if err != nil {
		return fmt.Errorf("failed to create file part: %w", err)
	}
	if _, err := filePart.Write(encodedPayload); err != nil {
		return fmt.Errorf("failed to write file part: %w", err)
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to close multipart writer: %w", err)
	}

	messageBytes := []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: Your purchase is confirmed\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=%s\r\n\r\n%s", smtpFrom, to, writer.Boundary(), body.String()))

	addr := fmt.Sprintf("%s:%s", smtpHost, smtpPort)
	var auth smtp.Auth
	if smtpUser != "" || smtpPass != "" {
		auth = smtp.PlainAuth("", smtpUser, smtpPass, smtpHost)
	}

	return smtp.SendMail(addr, auth, smtpFrom, []string{to}, messageBytes)
}

func base64Encode(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}
