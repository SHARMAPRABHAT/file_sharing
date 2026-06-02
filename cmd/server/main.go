package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	razorpay "github.com/razorpay/razorpay-go"
)

type Content struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Description     string `json:"description"`
	Category        string `json:"category"`
	Price           int    `json:"price"`
	OfferPrice      int    `json:"offer_price"`
	DiscountPercent int    `json:"discount_percent"`
	FilePath        string `json:"-"`
}

type Category struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
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

var categories = []Category{
	{
		ID:          "paper-1-dictionary",
		Title:       "PAPER 1 DICTIONARY",
		Description: "Paper 1 dictionary notes for quick concept revision.",
	},
	{
		ID:          "commerce-dictionary",
		Title:       "COMMERCE DICTIONARY",
		Description: "Commerce dictionary notes for important terms and definitions.",
	},
	{
		ID:          "paper-1-special-notes",
		Title:       "PAPER 1 SPECIAL NOTES",
		Description: "Special Paper 1 notes for focused exam preparation.",
	},
	{
		ID:          "commerce-special-notes",
		Title:       "COMMERCE SPECIAL NOTES",
		Description: "Special Commerce notes for revision and practice.",
	},
}

var contents = map[string]Content{
	"paper1-dictionary": {
		ID:              "paper1-dictionary",
		Title:           "Paper 1 Dictionary Notes",
		Description:     "Important Paper 1 terms, definitions, and quick revision points.",
		Category:        "paper-1-dictionary",
		Price:           99,
		OfferPrice:      199,
		DiscountPercent: 50,
		FilePath:        "./assets/abc123.pdf",
	},
	"commerce-dictionary": {
		ID:              "commerce-dictionary",
		Title:           "Commerce Dictionary Notes",
		Description:     "Commerce terms and definitions arranged for fast recall.",
		Category:        "commerce-dictionary",
		Price:           99,
		OfferPrice:      199,
		DiscountPercent: 50,
		FilePath:        "./assets/sample123.pdf",
	},
	"paper1-special-notes": {
		ID:              "paper1-special-notes",
		Title:           "Paper 1 Special Notes",
		Description:     "Focused Paper 1 notes for teaching aptitude, research, ICT, and revision.",
		Category:        "paper-1-special-notes",
		Price:           149,
		OfferPrice:      499,
		DiscountPercent: 70,
		FilePath:        "./assets/abc123.pdf",
	},
	"commerce-special-notes": {
		ID:              "commerce-special-notes",
		Title:           "Commerce Special Notes",
		Description:     "Special Commerce notes for core concepts, short notes, and exam revision.",
		Category:        "commerce-special-notes",
		Price:           149,
		OfferPrice:      499,
		DiscountPercent: 70,
		FilePath:        "./assets/sample123.pdf",
	},
}

var (
	tokenStore = map[string]AccessToken{}
	tokenMu    sync.Mutex
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Printf("⚠️ could not load .env file: %v", err)
	}

	port := getenv("APP_PORT", "8080")
	baseURL := getenv("APP_BASE_URL", "http://localhost:"+port)
	ttlMinutes := getenvInt("TOKEN_TTL_MINUTES", 30)
	tokenTTL := time.Duration(ttlMinutes) * time.Minute

	keyID := getenv("RAZORPAY_KEY_ID", "")
	keySecret := getenv("RAZORPAY_KEY_SECRET", "")
	resendAPIKey := getenv("RESEND_API_KEY", "")
	resendFrom := getenv("RESEND_FROM", "")

	var razorpayClient *razorpay.Client
	if keyID != "" && keySecret != "" {
		razorpayClient = razorpay.NewClient(keyID, keySecret)
		log.Printf("✅ Razorpay client configured")
	} else {
		log.Printf("⚠️ Razorpay env vars not set; create-order will return an error until configured")
	}

	if resendAPIKey == "" || resendFrom == "" {
		log.Printf("⚠️ Resend is not fully configured; payment verification will fail until RESEND_API_KEY and RESEND_FROM are set")
	}

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	r.GET("/health", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	r.Static("/static", "./web/static")
	r.StaticFile("/assets/learning-banner.png", "./assets/learning-banner.png")

	r.GET("/", func(c *gin.Context) {
		c.File("./web/static/landing.html")
	})

	r.GET("/buy", func(c *gin.Context) {
		c.File("./web/static/buy.html")
	})

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

	r.GET("/api/categories", func(c *gin.Context) {
		c.JSON(http.StatusOK, categories)
	})

	r.GET("/api/contents", func(c *gin.Context) {
		list := make([]Content, 0, len(contents))
		for _, content := range contents {
			list = append(list, content)
		}
		c.JSON(http.StatusOK, list)
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
		if resendAPIKey == "" || resendFrom == "" {
			c.JSON(http.StatusInternalServerError, VerifyPaymentResp{Status: "failed", Message: "Resend is not configured"})
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
		if err := sendDeliveryEmail(resendAPIKey, resendFrom, buyerEmail, buyerName, content, accessURL, expiresAt); err != nil {
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

type resendAttachment struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
}

type resendPayload struct {
	From        string             `json:"from"`
	To          []string           `json:"to"`
	Subject     string             `json:"subject"`
	Text        string             `json:"text"`
	Attachments []resendAttachment `json:"attachments,omitempty"`
}

func sendDeliveryEmail(resendAPIKey, resendFrom, to, buyerName string, content Content, accessURL string, expiresAt time.Time) error {
	if resendAPIKey == "" || resendFrom == "" {
		return fmt.Errorf("Resend is not configured")
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

	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, map[string]string{
		"BuyerName": buyerName,
		"Title":     content.Title,
		"AccessURL": accessURL,
		"ExpiresAt": expiresAt.Format(time.RFC1123),
	}); err != nil {
		return fmt.Errorf("failed to render email template: %w", err)
	}

	body, err := json.Marshal(resendPayload{
		From:    resendFrom,
		To:      []string{to},
		Subject: "Your purchase is confirmed",
		Text:    rendered.String(),
		Attachments: []resendAttachment{{
			Filename: filepath.Base(content.FilePath),
			Content:  base64Encode(payload),
		}},
	})
	if err != nil {
		return fmt.Errorf("failed to marshal resend payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create resend request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+resendAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call Resend: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read resend response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("Resend API error (%d): %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}

	return nil
}

func base64Encode(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}
