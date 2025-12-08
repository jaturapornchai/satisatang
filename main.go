package main

import (
	"log"

	"github.com/gin-gonic/gin"
	"github.com/satisatang/backend/config"
	"github.com/satisatang/backend/handlers"
	"github.com/satisatang/backend/services"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize Gemini service
	geminiService, err := services.NewGeminiService(cfg.GeminiAPIKey, cfg.GeminiModel)
	if err != nil {
		log.Fatalf("Failed to initialize Gemini service: %v", err)
	}
	defer geminiService.Close()

	// Initialize Line webhook handler
	lineWebhook, err := handlers.NewLineWebhookHandler(cfg.LineChannelSecret, cfg.LineChannelAccessToken, geminiService)
	if err != nil {
		log.Fatalf("Failed to initialize Line webhook handler: %v", err)
	}

	// Setup Gin
	if cfg.GinMode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.Default()

	// Health check
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok", "service": "satisatang"})
	})

	// Line webhook
	r.POST("/webhook/line", lineWebhook.HandleWebhook)

	// Start server
	log.Printf("Starting Satisatang server on port %s", cfg.Port)
	if err := r.Run(":" + cfg.Port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
