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
	GoogleDriveID   string `json:"-"`
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
	Status       string `json:"status"`
	Message      string `json:"message"`
	DownloadLink string `json:"download_link,omitempty"`
}

// AccessToken deprecated: using Google Drive links instead
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
	"teaching-aptitude-TA": {
		ID:              "teaching-aptitude-TA",
		Title:           "Teaching Aptitude Dictionary",
		Description:     "200+ core concepts, basic terms, and definitions explained in simple Hinglish (PDF).",
		Category:        "paper-1-dictionary",
		Price:           49,
		OfferPrice:      199,
		DiscountPercent: 75,
		GoogleDriveID:   "https://drive.google.com/file/d/1G4YvpeS9IVB8YZtAYhGp-nuyFP0Kmyso/view?usp=drive_link",
		FilePath:        ".assets/paper-1-dictionary/teaching-aptitude-TA.pdf",
	},
	"research-aptitude-RA": {
		ID:              "research-aptitude-RA",
		Title:           "Research Aptitude Dictionary",
		Description:     "Core research terminologies, types of research, hypothesis, and sampling terms made easy (PDF).",
		Category:        "paper-1-dictionary",
		Price:           49,
		OfferPrice:      199,
		DiscountPercent: 75,
		GoogleDriveID:   "https://drive.google.com/file/d/1G4YvpeS9IVB8YZtAYhGp-nuyFP0Kmyso/view?usp=drive_link",
		FilePath:        ".assets/paper-1-dictionary/research-aptitude-RA.pdf",
	},
	"communication": {
		ID:              "communication",
		Title:           "Communication Core Terms Dictionary",
		Description:     "Commerce terms and definitions arranged for fast recall.",
		Category:        "paper-1-dictionary",
		Price:           49,
		OfferPrice:      199,
		DiscountPercent: 75,
		FilePath:        "./assets/paper-1-dictionary/communication.pdf",
	},
	"logical-reasoning-LR": {
		ID:              "logical-reasoning-LR",
		Title:           "Logical Reasoning Cheatsheet",
		Description:     "Indian Logic (Pramanas), Syllogism, and Fallacies explained with simple examples and terms (PDF).",
		Category:        "paper-1-dictionary",
		Price:           49,
		OfferPrice:      199,
		DiscountPercent: 75,
		FilePath:        "./assets/paper-1-dictionary/logical-reasoning-LR.pdf",
	},
	"information-communication-technology-ICT": {
		ID:              "information-communication-technology-ICT",
		Title:           "ICT Glossary & Abbreviations",
		Description:     "Complete A-to-Z of networking, internet terms, memory units, and acronyms for quick marks (PDF).",
		Category:        "paper-1-dictionary",
		Price:           49,
		OfferPrice:      199,
		DiscountPercent: 75,
		FilePath:        "./assets/paper-1-dictionary/information-communication-technology-ICT.pdf",
	},
	"people-development-environment-PDE": {
		ID:              "people-development-environment-PDE",
		Title:           "People & Environment Facts",
		Description:     "Important protocols (Kyoto, Paris), pollutants, and environmental terms in brief pointers (PDF).",
		Category:        "paper-1-dictionary",
		Price:           49,
		OfferPrice:      199,
		DiscountPercent: 75,
		FilePath:        "./assets/paper-1-dictionary/people-development-environment-PDE.pdf",
	},
	"higher-education-system-HES": {
		ID:              "higher-education-system-HES",
		Title:           "Higher Education Quick Guide",
		Description:     "Ancient universities, pre/post-independence committees, and digital initiatives simplified (PDF).",
		Category:        "paper-1-dictionary",
		Price:           49,
		OfferPrice:      199,
		DiscountPercent: 75,
		FilePath:        "./assets/paper-1-dictionary/higher-education-system-HES.pdf",
	},
	"all-units-combo": {
		ID:              "all-units-combo",
		Title:           "All 7 Units Ultimate Dictionary Combo",
		Description:     "Get complete access to all 7 units (Teaching, Research, Communication, Logical Reasoning, ICT, PDE, HES) in one single download. Perfect for last-minute revision! (PDF).",
		Category:        "paper-1-dictionary",
		Price:           299,
		OfferPrice:      1399,
		DiscountPercent: 79,
		FilePath:        "./assets/paper-1-dictionary/all-units-combo.pdf",
	},
	"people-development-environment-eng": {
		ID:              "people-development-environment-eng",
		Title:           "People, Development & Environment",
		Description:     "Master Unit 9 with high-yield theoretical concepts, real-world applications, and latest environmental updates. Includes step-by-step guidance to solve tricky application-based questions easily! (PDF).",
		Category:        "paper-1-special-notes",
		Price:           149,
		OfferPrice:      499,
		DiscountPercent: 70,
		FilePath:        "./assets/paper-1-special-notes/people-development-environment-eng.pdf",
	},
	"people-development-environment-hindi": {
		ID:              "people-development-environment-hindi",
		Title:           "लोग, विकास और पर्यावरण (PDE): स्पेशल नोट्स",
		Description:     "यूनिट 9 पर मजबूत पकड़ बनाएं! इसमें आपको मिलेंगे उच्च स्तरीय सैद्धांतिक विषय, वास्तविक अनुप्रयोग (Applications), और पर्यावरण से जुड़े नवीनतम करंट अफेयर्स। कठिन और एप्लीकेशन-आधारित प्रश्नों को आसानी से हल करने के लिए स्टेप-बाय-स्टेप मार्गदर्शन शामिल है! (PDF).",
		Category:        "paper-1-special-notes",
		Price:           149,
		OfferPrice:      499,
		DiscountPercent: 70,
		FilePath:        "./assets/paper-1-special-notes/people-development-environment-hindi.pdf",
	},
	"people-development-environment-bilingual": {
		ID:    "people-development-environment-bilingual",
		Title: "PDE Special Notes with Application (Bilingual Combo)",
		Description: `Master Unit 9 in your preferred language! Complete theoretical concepts, real-world applications, and latest environment updates provided in both Hindi & English.
	
	यूनिट 9 पर मजबूत पकड़ बनाएं! इसमें आपको मिलेंगे उच्च स्तरीय सैद्धांतिक विषय, वास्तविक अनुप्रयोग और पर्यावरण के नवीनतम करंट अफेयर्स हिंदी और अंग्रेजी दोनों भाषाओं में। (PDF).`,

		DiscountPercent: 70,
		Category:        "paper-1-special-notes",
		Price:           199,
		OfferPrice:      599,
		FilePath:        "./assets/paper-1-special-notes/people-development-environment-bilingual.pdf",
	},
	"higher-education-system-eng": {
		ID:              "higher-education-system-eng",
		Title:           "Higher Education System",
		Description:     "Comprehensive notes covering Ancient Education, Pre/Post Independence Committees, NEP 2020, and Governance. Master application-based questions on institutional bodies and latest digital schemes easily! (PDF).",
		Category:        "paper-1-special-notes",
		Price:           149,
		OfferPrice:      499,
		DiscountPercent: 70,
		FilePath:        "./assets/paper-1-special-notes/higher-education-system-eng.pdf",
	},
	"higher-education-system-hindi": {
		ID:              "higher-education-system-hindi",
		Title:           "उच्च शिक्षा प्रणाली (HES): स्पेशल नोट्स",
		Description:     "प्राचीन शिक्षा, स्वतंत्रता से पहले और बाद की समितियां, NEP 2020 और शासन व्यवस्था को कवर करने वाले संपूर्ण नोट्स। विभिन्न संस्थागत निकायों (Institutional Bodies) और सरकार की नवीनतम डिजिटल योजनाओं पर आने वाले एप्लीकेशन-आधारित प्रश्नों को आसानी से हल करना सीखें! (PDF).",
		Category:        "paper-1-special-notes",
		Price:           149,
		OfferPrice:      499,
		DiscountPercent: 70,
		FilePath:        "./assets/paper-1-special-notes/higher-education-system-hindi.pdf",
	},
	"higher-education-system-bilingual": {
		ID:    "higher-education-system-bilingual",
		Title: "HES Special Notes with Application (Bilingual Combo)",
		Description: `Comprehensive notes covering Ancient Education, Committees, NEP 2020, and Governance. Master tricky application-based questions with content available in both Hindi & English.
	
	प्राचीन शिक्षा, समितियां, NEP 2020 और शासन व्यवस्था को कवर करने वाले संपूर्ण नोट्स। कठिन और एप्लीकेशन-आधारित प्रश्नों को हल करने का तरीका सीखें, अब हिंदी और अंग्रेजी दोनों भाषाओं में उपलब्ध। (PDF).`,

		Category:        "paper-1-special-notes",
		Price:           149,
		OfferPrice:      499,
		DiscountPercent: 70,
		FilePath:        "./assets/paper-1-special-notes/higher-education-system-bilingual.pdf",
	},
}

// Deprecated: token-based system replaced with Google Drive direct links
var (
	tokenStore = map[string]AccessToken{}
	tokenMu    sync.Mutex
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Printf("⚠️ could not load .env file: %v", err)
	}

	port := getenv("APP_PORT", "8080")

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
		if _, err := deliveryAttachments(content); driveShareLink(content.GoogleDriveID) == "" && err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "delivery is not configured for this content: " + err.Error()})
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

		driveLink := driveShareLink(content.GoogleDriveID)

		if err := sendDeliveryEmail(resendAPIKey, resendFrom, buyerEmail, buyerName, content, driveLink); err != nil {
			c.JSON(http.StatusInternalServerError, VerifyPaymentResp{Status: "failed", Message: "failed to send delivery email: " + err.Error()})
			return
		}

		c.JSON(http.StatusOK, VerifyPaymentResp{Status: "ok", Message: "payment verified, download link sent to email", DownloadLink: driveLink})
	})

	// Deprecated: Token-based access replaced with Google Drive direct links
	r.GET("/access/:token", func(c *gin.Context) {
		c.JSON(http.StatusGone, gin.H{"error": "this endpoint is deprecated", "message": "use Google Drive links sent via email instead"})
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

func driveShareLink(fileID string) string {
	fileID = strings.TrimSpace(fileID)
	if fileID == "" || strings.EqualFold(fileID, "REPLACE_WITH_DRIVE_ID") {
		return ""
	}
	return fmt.Sprintf("https://drive.google.com/file/d/%s/view?usp=sharing", fileID)
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

func sendDeliveryEmail(resendAPIKey, resendFrom, to, buyerName string, content Content, driveLink string) error {
	if resendAPIKey == "" || resendFrom == "" {
		return fmt.Errorf("Resend is not configured")
	}
	if buyerName == "" {
		buyerName = "there"
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
		"BuyerName":    buyerName,
		"Title":        content.Title,
		"DownloadLink": driveLink,
	}); err != nil {
		return fmt.Errorf("failed to render email template: %w", err)
	}

	payload := resendPayload{
		From:    resendFrom,
		To:      []string{to},
		Subject: "Your purchase is confirmed - Download link",
		Text:    rendered.String(),
	}

	if driveLink == "" {
		attachments, err := deliveryAttachments(content)
		if err != nil {
			return err
		}
		payload.Subject = "Your purchase is confirmed - PDF attached"
		payload.Attachments = attachments
	}

	body, err := json.Marshal(payload)
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

func deliveryAttachments(content Content) ([]resendAttachment, error) {
	paths, err := deliveryFilePaths(content)
	if err != nil {
		return nil, err
	}

	attachments := make([]resendAttachment, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read local PDF %q: %w", path, err)
		}

		filename := filepath.Base(path)
		if filename == "." || filename == string(filepath.Separator) {
			filename = content.ID + ".pdf"
		}

		attachments = append(attachments, resendAttachment{
			Filename: filename,
			Content:  base64Encode(data),
		})
	}

	return attachments, nil
}

func deliveryFilePaths(content Content) ([]string, error) {
	path := normalizeAssetPath(content.FilePath)
	if path != "" && fileExists(path) {
		return []string{path}, nil
	}

	if strings.HasSuffix(content.ID, "-bilingual") {
		prefix := strings.TrimSuffix(content.ID, "-bilingual")
		dir := filepath.Dir(path)
		if dir == "." || dir == "" {
			dir = "./assets/paper-1-special-notes"
		}

		paths := []string{
			filepath.Join(dir, prefix+"-eng.pdf"),
			filepath.Join(dir, prefix+"-hindi.pdf"),
		}
		if fileExists(paths[0]) && fileExists(paths[1]) {
			return paths, nil
		}
	}

	if strings.HasPrefix(path, ".assets") {
		path = normalizeAssetPath(path)
	}
	if path == "" {
		return nil, fmt.Errorf("Google Drive ID is not configured and local PDF path is missing for %s", content.ID)
	}
	return nil, fmt.Errorf("Google Drive ID is not configured and local PDF %q does not exist", path)
}

func normalizeAssetPath(path string) string {
	path = strings.TrimSpace(path)
	if strings.HasPrefix(path, ".assets") {
		return "." + strings.TrimPrefix(path, ".assets")
	}
	return path
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func base64Encode(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}
