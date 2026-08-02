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
	"net/url"
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
	ID              string            `json:"id"`
	Title           string            `json:"title"`
	Description     string            `json:"description"`
	Category        string            `json:"category"`
	Price           int               `json:"price"`
	OfferPrice      int               `json:"offer_price"`
	DiscountPercent int               `json:"discount_percent"`
	GoogleDriveID   string            `json:"-"` // Hidden by default, only exposed for free items via MarshalJSON
	GoogleDriveIDs  []GoogleDriveLink `json:"google_drive_ids"`
	FilePath        string            `json:"-"`
}

// MarshalJSON customizes JSON encoding to only include GoogleDriveID for free items (price == 0)
func (c Content) MarshalJSON() ([]byte, error) {
	m := map[string]interface{}{
		"id":               c.ID,
		"title":            c.Title,
		"description":      c.Description,
		"category":         c.Category,
		"price":            c.Price,
		"offer_price":      c.OfferPrice,
		"discount_percent": c.DiscountPercent,
		"google_drive_ids": c.GoogleDriveIDs,
	}
	// Only include google_drive_id if this is a free item (price == 0)
	if c.Price == 0 && c.GoogleDriveID != "" {
		m["google_drive_id"] = c.GoogleDriveID
	}
	return json.Marshal(m)
}

type GoogleDriveLink struct {
	Title string `json:"title"`
	ID    string `json:"id"`
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

type DriveOAuthConfig struct {
	ClientID     string
	ClientSecret string
	RefreshToken string
}

type ContactReq struct {
	Name    string `json:"name"`
	Email   string `json:"email"`
	Message string `json:"message"`
}

type CourseOfferState struct {
	Title           string            `json:"title"`
	Description     string            `json:"description,omitempty"`
	Price           int               `json:"price,omitempty"`
	OfferPrice      int               `json:"offer_price,omitempty"`
	DiscountPercent int               `json:"discount_percent,omitempty"`
	Sold            int               `json:"sold"`
	Total           int               `json:"total"`
	GoogleDriveID   string            `json:"google_drive_id,omitempty"`
	GoogleDriveIDs  []GoogleDriveLink `json:"google_drive_ids,omitempty"`
	FilePath        string            `json:"file_path,omitempty"`
}

type CourseOfferFile struct {
	Courses map[string]CourseOfferState `json:"courses"`
}

type CoursePurchaseReq struct {
	CourseID string `json:"course_id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Mobile   string `json:"mobile"`
}

var offerStateMu sync.Mutex
var offerStatePath = "./data/course-offers.json"

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
	"free-sample-paper-1-special-notes": {
		ID:              "free-sample-paper-1-special-notes",
		Title:           "Free Sample - Paper I Notes",
		Description:     "Get started with a free comprehensive sample of Paper I notes. Covers key concepts, terminology, and exam-focused revision tips to help you decide if these notes are right for you.",
		Category:        "paper-1-special-notes",
		Price:           0,
		OfferPrice:      299,
		DiscountPercent: 100,
		GoogleDriveID:   "https://drive.google.com/file/d/16jtJ1iy1Q1uviMnES-7ZyDaeFN-mH3Lf/view?usp=drive_link",
		FilePath:        "",
	},
	"free-sample-paper-1-dictionary": {
		ID:              "free-sample-paper-1-dictionary",
		Title:           "Free Sample - Paper I Notes",
		Description:     "Get started with a free comprehensive sample of Paper I notes. Covers key concepts, terminology, and exam-focused revision tips to help you decide if these notes are right for you.",
		Category:        "paper-1-dictionary",
		Price:           0,
		OfferPrice:      299,
		DiscountPercent: 100,
		GoogleDriveID:   "https://drive.google.com/file/d/16jtJ1iy1Q1uviMnES-7ZyDaeFN-mH3Lf/view?usp=drive_link",
		FilePath:        "",
	},
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
		GoogleDriveID:   "https://drive.google.com/file/d/1zHJjE3ETURKBzaoVMCiAZVvX_U_effxs/view?usp=drive_link",
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
		GoogleDriveID:   "https://drive.google.com/file/d/1L1fNOTfOKCrJNGT3T_Q5P8Cab6FyAgRD/view?usp=drive_link",
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
		GoogleDriveID:   "https://drive.google.com/file/d/1CoHFvFCr16j1fLdskonLp1gR7uFlp5iQ/view?usp=drive_link",
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
		GoogleDriveID:   "https://drive.google.com/file/d/1E_zMzMObBKsI5MUvfSAfN01oFckelAzt/view?usp=drive_link",
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
		GoogleDriveID:   "https://drive.google.com/file/d/1MzIlyXjUT3L_vZVtEG_KXSgwQVFwQaNC/view?usp=drive_link",
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
		GoogleDriveID:   "https://drive.google.com/file/d/1RexzDnF83huZUc4HJGVmBf4sn6GCupx2/view?usp=drive_link",
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
		GoogleDriveIDs: []GoogleDriveLink{
			{Title: "Teaching Aptitude Dictionary", ID: "https://drive.google.com/file/d/1G4YvpeS9IVB8YZtAYhGp-nuyFP0Kmyso/view?usp=drive_link"},
			{Title: "Research Aptitude Dictionary", ID: "https://drive.google.com/file/d/1zHJjE3ETURKBzaoVMCiAZVvX_U_effxs/view?usp=drive_link"},
			{Title: "Communication Core Terms Dictionary", ID: "https://drive.google.com/file/d/1L1fNOTfOKCrJNGT3T_Q5P8Cab6FyAgRD/view?usp=drive_link"},
			{Title: "Logical Reasoning Cheatsheet", ID: "https://drive.google.com/file/d/1CoHFvFCr16j1fLdskonLp1gR7uFlp5iQ/view?usp=drive_link"},
			{Title: "ICT Glossary & Abbreviations", ID: "https://drive.google.com/file/d/1E_zMzMObBKsI5MUvfSAfN01oFckelAzt/view?usp=drive_link"},
			{Title: "People & Environment Facts", ID: "https://drive.google.com/file/d/1MzIlyXjUT3L_vZVtEG_KXSgwQVFwQaNC/view?usp=drive_link"},
			{Title: "Higher Education Quick Guide", ID: "https://drive.google.com/file/d/1RexzDnF83huZUc4HJGVmBf4sn6GCupx2/view?usp=drive_link"},
		},
		FilePath: "./assets/paper-1-dictionary/all-units-combo.pdf",
	},
	"people-development-environment-eng": {
		ID:              "people-development-environment-eng",
		Title:           "People, Development & Environment",
		Description:     "Master Unit 9 with high-yield theoretical concepts, real-world applications, and latest environmental updates. Includes step-by-step guidance to solve tricky application-based questions easily! (PDF).",
		Category:        "paper-1-special-notes",
		Price:           149,
		OfferPrice:      499,
		DiscountPercent: 70,
		GoogleDriveID:   "https://drive.google.com/file/d/1FCi8_TmiUMj5l0hkfXFTK_cJ9OCdiUko/view?usp=drive_link",
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
		GoogleDriveID:   "https://drive.google.com/file/d/1KVClqjrUi4eTuvdJXJT4uhM2HNQscdEe/view?usp=drive_link",
		FilePath:        "./assets/paper-1-special-notes/people-development-environment-hindi.pdf",
	},
	"people-development-environment-bilingual": {
		ID:    "people-development-environment-bilingual",
		Title: "PDE Special Notes with Application (Bilingual Combo)",
		Description: `Master Unit 9 in your preferred language! Complete theoretical concepts, real-world applications, and latest environment updates provided in both Hindi & English.
	
	यूनिट 9 पर मजबूत पकड़ बनाएं! इसमें आपको मिलेंगे उच्च स्तरीय सैद्धांतिक विषय, वास्तविक अनुप्रयोग और पर्यावरण के नवीनतम करंट अफेयर्स हिंदी और अंग्रेजी दोनों भाषाओं में। (PDF).`,

		DiscountPercent: 70,
		Category:        "paper-1-special-notes",
		Price:           249,
		OfferPrice:      849,
		GoogleDriveIDs: []GoogleDriveLink{
			{Title: "People, Development & Environment (English)", ID: "https://drive.google.com/file/d/1FCi8_TmiUMj5l0hkfXFTK_cJ9OCdiUko/view?usp=drive_link"},
			{Title: "लोग, विकास और पर्यावरण (हिंदी)", ID: "https://drive.google.com/file/d/1KVClqjrUi4eTuvdJXJT4uhM2HNQscdEe/view?usp=drive_link"},
		},
		FilePath: "./assets/paper-1-special-notes/people-development-environment-bilingual.pdf",
	},
	"higher-education-system-eng": {
		ID:              "higher-education-system-eng",
		Title:           "Higher Education System",
		Description:     "Comprehensive notes covering Ancient Education, Pre/Post Independence Committees, NEP 2020, and Governance. Master application-based questions on institutional bodies and latest digital schemes easily! (PDF).",
		Category:        "paper-1-special-notes",
		Price:           149,
		OfferPrice:      499,
		DiscountPercent: 70,
		GoogleDriveID:   "https://drive.google.com/file/d/1IM1MZhCxhuSx3k3BD8dcojeBZ5juIa33/view?usp=drive_link",
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
		GoogleDriveID:   "https://drive.google.com/file/d/1c1nlUoOR9DMC0i77FlWnI5yNA0lI6_D_/view?usp=drive_link",
		FilePath:        "./assets/paper-1-special-notes/higher-education-system-hindi.pdf",
	},
	"higher-education-system-bilingual": {
		ID:    "higher-education-system-bilingual",
		Title: "HES Special Notes with Application (Bilingual Combo)",
		Description: `Comprehensive notes covering Ancient Education, Committees, NEP 2020, and Governance. Master tricky application-based questions with content available in both Hindi & English.
	
	प्राचीन शिक्षा, समितियां, NEP 2020 और शासन व्यवस्था को कवर करने वाले संपूर्ण नोट्स। कठिन और एप्लीकेशन-आधारित प्रश्नों को हल करने का तरीका सीखें, अब हिंदी और अंग्रेजी दोनों भाषाओं में उपलब्ध। (PDF).`,

		Category:        "paper-1-special-notes",
		Price:           249,
		OfferPrice:      849,
		DiscountPercent: 70,
		FilePath:        "./assets/paper-1-special-notes/higher-education-system-bilingual.pdf",
		GoogleDriveIDs: []GoogleDriveLink{
			{Title: "Higher Education System (English)", ID: "https://drive.google.com/file/d/1IM1MZhCxhuSx3k3BD8dcojeBZ5juIa33/view?usp=drive_link"},
			{Title: "उच्च शिक्षा प्रणाली (हिंदी)", ID: "https://drive.google.com/file/d/1c1nlUoOR9DMC0i77FlWnI5yNA0lI6_D_/view?usp=drive_link"},
		},
	},
	"research-aptitude-eng": {
		ID:              "research-aptitude-eng",
		Title:           "Master Research Aptitude (English) in One Go!",
		Description:     "100% Updated Syllabus Coverage. Concept Clarity + Smart Revision Tricks. Designed to lock your 10/10 marks in Paper 1. Complete Research Aptitude explained with expert guidance and high-yield concepts (PDF).",
		Category:        "paper-1-special-notes",
		Price:           149,
		OfferPrice:      499,
		DiscountPercent: 70,
		GoogleDriveID:   "https://drive.google.com/file/d/1WIq-M0Y1r7qCeuegz7Mj0hEAmrI-zO_2/view?usp=drive_link",
		FilePath:        "./assets/paper-1-special-notes/research-aptitude-eng.pdf",
	},
	"research-aptitude-hindi": {
		ID:              "research-aptitude-hindi",
		Title:           "Master Research Aptitude (अनुसंधान योग्यता) - Hindi",
		Description:     "अनुसंधान योग्यता को हिंदी में सरलतम भाषा में समझें। 100% Updated Syllabus Coverage के साथ Concept Clarity और Smart Revision Tricks। Paper 1 में 10/10 अंक प्राप्त करने के लिए डिज़ाइन किया गया है। संपूर्ण Research Aptitude आसान उदाहरणों के साथ समझाया गया है (PDF)।",
		Category:        "paper-1-special-notes",
		Price:           149,
		OfferPrice:      499,
		DiscountPercent: 70,
		GoogleDriveID:   "https://drive.google.com/file/d/1MxrWd7xmkSSW9YovtyMLIMCxB3g1xO8L/view?usp=drive_link",
		FilePath:        "./assets/paper-1-special-notes/research-aptitude-hindi.pdf",
	},
	"research-aptitude-bilingual": {
		ID:    "research-aptitude-bilingual",
		Title: "Research Aptitude Special Notes (English & Hindi Combo)",
		Description: `Master Research Aptitude in both Easy English & Hindi! Complex concepts simplified with daily-life examples. Perfect for both Hindi & English medium students to score 10/10 in Paper 1.
	
	Research Aptitude को हिंदी और अंग्रेजी दोनों में सीखें! कठिन concepts को रोजमर्रा के उदाहरणों से समझाया गया है। हिंदी और अंग्रेजी माध्यम दोनों के छात्रों के लिए Paper 1 में 10/10 अंक लाने के लिए परफेक्ट (PDF).`,

		Category:        "paper-1-special-notes",
		Price:           249,
		OfferPrice:      849,
		DiscountPercent: 70,
		FilePath:        "./assets/paper-1-special-notes/research-aptitude-bilingual.pdf",
		GoogleDriveIDs: []GoogleDriveLink{
			{Title: "Master Research Aptitude (English)", ID: "https://drive.google.com/file/d/1WIq-M0Y1r7qCeuegz7Mj0hEAmrI-zO_2/view?usp=drive_link"},
			{Title: "Master Research Aptitude (हिंदी)", ID: "https://drive.google.com/file/d/1MxrWd7xmkSSW9YovtyMLIMCxB3g1xO8L/view?usp=drive_link"},
		},
	},
}

// contentOrder defines the sequence of contents as they should appear on the webpage
var contentOrder = []string{
	"free-sample-paper-1-special-notes",
	"teaching-aptitude-TA",
	"people-development-environment-eng",
	"free-sample-paper-1-dictionary",
	"research-aptitude-RA",
	"communication",
	"logical-reasoning-LR",
	"information-communication-technology-ICT",
	"people-development-environment-PDE",
	"higher-education-system-HES",
	"all-units-combo",
	"people-development-environment-hindi",
	"people-development-environment-bilingual",
	"higher-education-system-eng",
	"higher-education-system-hindi",
	"higher-education-system-bilingual",
	"research-aptitude-eng",
	"research-aptitude-hindi",
	"research-aptitude-bilingual",
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
	contactSheetWebhookURL := getenv("CONTACT_SHEET_WEBHOOK_URL", "")
	driveOAuth := DriveOAuthConfig{
		ClientID:     getenv("GOOGLE_DRIVE_CLIENT_ID", ""),
		ClientSecret: getenv("GOOGLE_DRIVE_CLIENT_SECRET", ""),
		RefreshToken: getenv("GOOGLE_DRIVE_REFRESH_TOKEN", ""),
	}

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
	if contactSheetWebhookURL == "" {
		log.Printf("⚠️ CONTACT_SHEET_WEBHOOK_URL is not set; contact form submissions will fail until configured")
	}
	if !driveOAuth.Configured() {
		log.Printf("⚠️ Google Drive OAuth is not fully configured; buyer-specific Drive viewer permissions will be skipped")
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

	r.GET("/policy", func(c *gin.Context) {
		c.File("./web/static/policy.html")
	})

	r.GET("/buy-courses", func(c *gin.Context) {
		c.File("./web/static/buy-course.html")
	})

	r.GET("/buy-course", func(c *gin.Context) {
		c.File("./web/static/buy-course.html")
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

	r.GET("/api/course-offers", func(c *gin.Context) {
		state, err := loadCourseOffers()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load course offer state"})
			return
		}
		c.JSON(http.StatusOK, state)
	})

	r.POST("/api/course-offers/claim", func(c *gin.Context) {
		var req CoursePurchaseReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
			return
		}
		req.CourseID = strings.TrimSpace(req.CourseID)
		req.Name = strings.TrimSpace(req.Name)
		req.Email = strings.TrimSpace(req.Email)
		req.Mobile = strings.TrimSpace(req.Mobile)

		if req.CourseID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "course_id is required"})
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

		state, err := loadCourseOffers()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load course offer state"})
			return
		}

		course, ok := state.Courses[req.CourseID]
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "course not found"})
			return
		}
		if course.Sold >= course.Total {
			c.JSON(http.StatusConflict, gin.H{"error": "offer seats already filled"})
			return
		}

		offerStateMu.Lock()
		defer offerStateMu.Unlock()

		state, err = loadCourseOffers()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to reload course offer state"})
			return
		}
		course = state.Courses[req.CourseID]
		if course.Sold >= course.Total {
			c.JSON(http.StatusConflict, gin.H{"error": "offer seats already filled"})
			return
		}
		course.Sold++
		state.Courses[req.CourseID] = course
		if err := saveCourseOffers(state); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save course offer state"})
			return
		}

		if resendAPIKey != "" && resendFrom != "" {
			if err := sendCourseOfferEmail(resendAPIKey, resendFrom, req.Email, req.Name, course); err != nil {
				log.Printf("failed to send course delivery email for course_id=%q email=%q: %v", req.CourseID, req.Email, err)
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"status":    "ok",
			"message":   "offer claim recorded",
			"course_id": req.CourseID,
			"sold":      course.Sold,
			"total":     course.Total,
			"remaining": course.Total - course.Sold,
		})
	})

	r.GET("/api/contents", func(c *gin.Context) {
		list := make([]Content, 0, len(contentOrder))
		for _, id := range contentOrder {
			if content, ok := contents[id]; ok {
				list = append(list, content)
			}
		}
		c.JSON(http.StatusOK, list)
	})

	r.POST("/api/contact", func(c *gin.Context) {
		var req ContactReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
			return
		}

		req.Name = strings.TrimSpace(req.Name)
		req.Email = strings.TrimSpace(req.Email)
		req.Message = strings.TrimSpace(req.Message)

		if len(req.Name) < 2 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
			return
		}
		if !strings.Contains(req.Email, "@") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "valid email is required"})
			return
		}
		if len(req.Message) < 5 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "message is required"})
			return
		}
		if contactSheetWebhookURL == "" {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "contact sheet is not configured"})
			return
		}

		if err := appendContactToSheet(contactSheetWebhookURL, req); err != nil {
			log.Printf("failed to save contact form submission: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save contact details"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "contact details saved"})
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
		if _, err := deliveryAttachments(content); len(deliveryLinks(content)) == 0 && err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "delivery is not configured for this content: " + err.Error()})
			return
		}

		params := map[string]interface{}{
			"amount":   content.Price * 100,
			"currency": "INR",
			"receipt":  razorpayReceipt(req.ContentID),
		}

		body, err := razorpayClient.Order.Create(params, nil)
		if err != nil {
			log.Printf("failed to create Razorpay order for content_id=%q: %v", req.ContentID, err)
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

		links := deliveryLinks(content)
		driveLink := ""
		if len(links) > 0 {
			driveLink = links[0].URL
		}

		if driveOAuth.Configured() {
			if err := grantDriveViewerAccess(driveOAuth, content, buyerEmail); err != nil {
				log.Printf("failed to grant Drive viewer access for content_id=%q email=%q: %v", content.ID, buyerEmail, err)
				c.JSON(http.StatusInternalServerError, VerifyPaymentResp{Status: "failed", Message: "payment verified but failed to grant Drive access"})
				return
			}
		}

		if err := sendDeliveryEmail(resendAPIKey, resendFrom, buyerEmail, buyerName, content, links); err != nil {
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

func loadCourseOffers() (CourseOfferFile, error) {
	state := CourseOfferFile{Courses: map[string]CourseOfferState{}}
	data, err := os.ReadFile(offerStatePath)
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}
		return state, err
	}
	if len(data) == 0 {
		return state, nil
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, err
	}
	if state.Courses == nil {
		state.Courses = map[string]CourseOfferState{}
	}
	return state, nil
}

func saveCourseOffers(state CourseOfferFile) error {
	if err := os.MkdirAll(filepath.Dir(offerStatePath), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(offerStatePath, data, 0o644)
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
	if strings.HasPrefix(fileID, "http://") || strings.HasPrefix(fileID, "https://") {
		return fileID
	}
	return fmt.Sprintf("https://drive.google.com/file/d/%s/view?usp=sharing", fileID)
}

func driveFileIDs(content Content) []string {
	seen := map[string]bool{}
	ids := make([]string, 0)
	add := func(value string) {
		id := extractDriveFileID(value)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}

	add(content.GoogleDriveID)
	for _, link := range content.GoogleDriveIDs {
		add(link.ID)
	}
	return ids
}

func extractDriveFileID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "REPLACE_WITH_DRIVE_ID") {
		return ""
	}
	if !strings.HasPrefix(value, "http://") && !strings.HasPrefix(value, "https://") {
		return value
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for i, part := range parts {
		if part == "d" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	if id := parsed.Query().Get("id"); id != "" {
		return id
	}
	return ""
}

func (cfg DriveOAuthConfig) Configured() bool {
	return strings.TrimSpace(cfg.ClientID) != "" &&
		strings.TrimSpace(cfg.ClientSecret) != "" &&
		strings.TrimSpace(cfg.RefreshToken) != ""
}

func googleDriveAccessToken(cfg DriveOAuthConfig) (string, error) {
	form := url.Values{}
	form.Set("client_id", cfg.ClientID)
	form.Set("client_secret", cfg.ClientSecret)
	form.Set("refresh_token", cfg.RefreshToken)
	form.Set("grant_type", "refresh_token")

	req, err := http.NewRequest(http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to create Google OAuth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to refresh Google access token: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read Google OAuth response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("Google OAuth error (%d): %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(responseBody, &tokenResp); err != nil {
		return "", fmt.Errorf("failed to parse Google OAuth response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("Google OAuth response did not include an access token")
	}
	return tokenResp.AccessToken, nil
}

func grantDriveViewerAccess(cfg DriveOAuthConfig, content Content, email string) error {
	fileIDs := driveFileIDs(content)
	if len(fileIDs) == 0 {
		return nil
	}

	accessToken, err := googleDriveAccessToken(cfg)
	if err != nil {
		return err
	}
	for _, fileID := range fileIDs {
		if err := grantDriveFileViewer(accessToken, fileID, email); err != nil {
			return err
		}
	}
	return nil
}

func grantDriveFileViewer(accessToken, fileID, email string) error {
	payload := map[string]string{
		"type":         "user",
		"role":         "reader",
		"emailAddress": email,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal Drive permission payload: %w", err)
	}

	endpoint := "https://www.googleapis.com/drive/v3/files/" + url.PathEscape(fileID) + "/permissions?supportsAllDrives=true&sendNotificationEmail=false"
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create Drive permission request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call Drive permissions API for %s: %w", fileID, err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read Drive permissions response for %s: %w", fileID, err)
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("Drive permissions API error for %s (%d): %s", fileID, resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	return nil
}

func razorpayReceipt(contentID string) string {
	contentID = strings.TrimSpace(contentID)
	if len(contentID) > 14 {
		contentID = contentID[:14]
	}
	return "purchase_" + contentID + "_" + randHex(6)
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

func appendContactToSheet(webhookURL string, contact ContactReq) error {
	ist, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		ist = time.FixedZone("IST", 5*60*60+30*60)
	}

	payload := map[string]string{
		"submitted_at": time.Now().In(ist).Format("02/01/2006 15:04:05"),
		"name":         contact.Name,
		"email":        contact.Email,
		"message":      contact.Message,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal contact payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create contact sheet request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call contact sheet webhook: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read contact sheet response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("contact sheet webhook error (%d): %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}

	return nil
}

type resendAttachment struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
}

type deliveryLink struct {
	Title string
	URL   string
}

type resendPayload struct {
	From        string             `json:"from"`
	To          []string           `json:"to"`
	Subject     string             `json:"subject"`
	Text        string             `json:"text"`
	Attachments []resendAttachment `json:"attachments,omitempty"`
}

func sendDeliveryEmail(resendAPIKey, resendFrom, to, buyerName string, content Content, links []deliveryLink) error {
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
	if err := tmpl.Execute(&rendered, map[string]interface{}{
		"BuyerName": buyerName,
		"Title":     content.Title,
		"Links":     links,
	}); err != nil {
		return fmt.Errorf("failed to render email template: %w", err)
	}

	payload := resendPayload{
		From:    resendFrom,
		To:      []string{to},
		Subject: "Your purchase is confirmed - Download link",
		Text:    rendered.String(),
	}

	if len(links) == 0 {
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

func sendCourseOfferEmail(resendAPIKey, resendFrom, to, buyerName string, course CourseOfferState) error {
	if resendAPIKey == "" || resendFrom == "" {
		return fmt.Errorf("Resend is not configured")
	}
	if buyerName == "" {
		buyerName = "there"
	}

	driveLink := strings.TrimSpace(course.GoogleDriveID)
	if driveLink == "" && len(course.GoogleDriveIDs) > 0 {
		driveLink = strings.TrimSpace(course.GoogleDriveIDs[0].ID)
	}
	if driveLink != "" {
		driveLink = driveShareLink(driveLink)
	}

	bodyLines := []string{
		"Hi " + buyerName + ",",
		"",
		"Thanks for buying " + course.Title + ".",
		"",
		"Course details:",
		course.Title,
	}
	if course.Description != "" {
		bodyLines = append(bodyLines, course.Description)
	}
	if course.Price > 0 {
		bodyLines = append(bodyLines, fmt.Sprintf("Course price: ₹%d", course.Price))
	}
	if course.OfferPrice > 0 {
		bodyLines = append(bodyLines, fmt.Sprintf("Launch offer price: ₹%d", course.OfferPrice))
	}
	if course.DiscountPercent > 0 {
		bodyLines = append(bodyLines, fmt.Sprintf("Discount: %d%%", course.DiscountPercent))
	}
	if driveLink != "" {
		bodyLines = append(bodyLines, "")
		bodyLines = append(bodyLines, "Google Drive delivery link:")
		bodyLines = append(bodyLines, driveLink)
	}

	payload := resendPayload{
		From:    resendFrom,
		To:      []string{to},
		Subject: "Thanks for buying " + course.Title,
		Text:    strings.Join(bodyLines, "\n"),
	}

	attachments, err := courseOfferAttachments(course)
	if err == nil {
		payload.Attachments = attachments
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal course email payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create course email request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+resendAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call Resend API for course email: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read course email response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("Resend API error (%d): %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}

	return nil
}

func courseOfferAttachments(course CourseOfferState) ([]resendAttachment, error) {
	path := normalizeAssetPath(course.FilePath)
	if path == "" || !fileExists(path) {
		return nil, fmt.Errorf("no local attachment path for %s", course.Title)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read attachment for %s: %w", course.Title, err)
	}
	filename := filepath.Base(path)
	if filename == "." || filename == string(filepath.Separator) {
		filename = course.Title + ".pdf"
	}
	return []resendAttachment{{Filename: filename, Content: base64Encode(data)}}, nil
}

func deliveryLinks(content Content) []deliveryLink {
	if len(content.GoogleDriveIDs) > 0 {
		links := make([]deliveryLink, 0, len(content.GoogleDriveIDs))
		for _, item := range content.GoogleDriveIDs {
			link := driveShareLink(item.ID)
			if link == "" {
				continue
			}
			title := strings.TrimSpace(item.Title)
			if title == "" {
				title = content.Title
			}
			links = append(links, deliveryLink{Title: title, URL: link})
		}
		return links
	}

	link := driveShareLink(content.GoogleDriveID)
	if link == "" {
		return nil
	}
	return []deliveryLink{{Title: content.Title, URL: link}}
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
