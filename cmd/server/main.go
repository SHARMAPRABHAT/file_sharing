package main

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

type Content struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Price       int    `json:"price"` // INR
}

// MVP in-memory content store (later: DB / Sheets)
var contents = map[string]Content{
	"abc123": {
		ID:          "abc123",
		Title:       "Kubernetes Notes",
		Description: "Concise notes to revise Kubernetes quickly (PDF).",
		Price:       99,
	},
	"sample123": {
		ID:          "sample123",
		Title:       "Golang Concurrency Cheatsheet",
		Description: "Channels, worker pools, select, and common pitfalls (PDF).",
		Price:       79,
	},
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
	Amount       int    `json:"amount"`   // in paise (Razorpay expects paise)
	Currency     string `json:"currency"` // INR
	ContentTitle string `json:"content_title"`
}

func main() {
	port := getenv("APP_PORT", "8080")

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	// ✅ Health
	r.GET("/health", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	// ✅ Static assets (CSS/JS)
	r.Static("/static", "./web/static")

	// ✅ Buy page
	r.GET("/buy/:contentId", func(c *gin.Context) {
		c.File("./web/static/buy.html")
	})

	// ✅ API: get content details
	r.GET("/api/content/:contentId", func(c *gin.Context) {
		id := c.Param("contentId")

		content, ok := contents[id]
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "content not found"})
			return
		}
		c.JSON(http.StatusOK, content)
	})

	// ✅ API: create order (STUB)
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

		// Create a fake order id (like Razorpay order id)
		orderID := "order_" + randHex(8)

		// Convert INR to paise (Razorpay uses paise)
		amountPaise := content.Price * 100

		resp := CreateOrderResp{
			OrderID:      orderID,
			KeyID:        "rzp_test_stub_key", // stub key id (public)
			Amount:       amountPaise,
			Currency:     "INR",
			ContentTitle: content.Title,
		}

		c.JSON(http.StatusOK, resp)
	})

	// Optional: root redirect
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

// Helpers

func getenv(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}

func randHex(nBytes int) string {
	b := make([]byte, nBytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
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
