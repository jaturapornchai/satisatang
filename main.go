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

	// Initialize MongoDB service
	mongoService, err := services.NewMongoDBService(cfg.MongoDBURI, cfg.MongoDBName)
	if err != nil {
		log.Fatalf("Failed to initialize MongoDB service: %v", err)
	}
	defer mongoService.Close()

	// Initialize AI service
	aiService := services.NewAIService()
	defer aiService.Close()

	// Initialize Firebase service (optional)
	var firebaseService *services.FirebaseService
	if cfg.HasFirebase() {
		firebaseService, err = services.NewFirebaseService(cfg.FirebaseCredentials, cfg.FirebaseStorageBucket)
		if err != nil {
			log.Printf("Warning: Failed to initialize Firebase service: %v", err)
			log.Println("File upload feature will be disabled")
		} else {
			defer firebaseService.Close()
		}
	} else {
		log.Println("Firebase not configured - file upload feature disabled")
	}

	// Initialize Line webhook handler
	lineWebhook, err := handlers.NewLineWebhookHandler(cfg.LineChannelSecret, cfg.LineChannelAccessToken, aiService, mongoService, firebaseService)
	if err != nil {
		log.Fatalf("Failed to initialize Line webhook handler: %v", err)
	}

	// Initialize Proxy Handler
	proxyHandler := handlers.NewProxyHandler()

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

	// AI API Proxy
	r.POST("/api/chat", proxyHandler.HandleChat)

	// Start server
	log.Printf("Starting Satisatang server on port %s", cfg.Port)
	if err := r.Run(":" + cfg.Port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
