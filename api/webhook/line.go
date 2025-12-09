package handler

import (
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/satisatang/backend/config"
	"github.com/satisatang/backend/handlers"
	"github.com/satisatang/backend/services"
)

// Global variables for reusing connections (important for serverless)
var (
	lineWebhook *handlers.LineWebhookHandler
	engine      *gin.Engine
)

// init initializes services once (on cold start)
func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}

// Handler is the serverless function entry point for Vercel
func Handler(w http.ResponseWriter, r *http.Request) {
	// Initialize services if not already done (handle cold starts)
	if lineWebhook == nil || engine == nil {
		if err := initServices(); err != nil {
			log.Printf("Failed to initialize services: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
	}

	// Debug logging for every request
	secret := os.Getenv("LINE_CHANNEL_SECRET")
	signature := r.Header.Get("X-Line-Signature")
	log.Printf("DEBUG REQUEST: SecretLen=%d, SecretPrefix=%s, SignatureLen=%d, Signature=%s",
		len(secret),
		getPrefix(secret),
		len(signature),
		signature,
	)

	// Serve the request through Gin
	engine.ServeHTTP(w, r)
}

func getPrefix(s string) string {
	if len(s) > 4 {
		return s[:4]
	}
	return s
}

func initServices() error {
	// Load environment variables
	cfg := &config.Config{
		LineChannelSecret:      os.Getenv("LINE_CHANNEL_SECRET"),
		LineChannelAccessToken: os.Getenv("LINE_CHANNEL_ACCESS_TOKEN"),
		GeminiAPIKey:           os.Getenv("GEMINI_API_KEY"),
		GeminiModel:            getEnv("GEMINI_MODEL", "gemini-2.5-flash-lite"),
		MongoDBURI:             os.Getenv("MONGODB_ATLAS_URI"),
		MongoDBName:            getEnv("MONGODB_ATLAS_DBNAME", "satistang"),
		FirebaseCredentials:    os.Getenv("FIREBASE_CREDENTIALS"),
		FirebaseStorageBucket:  os.Getenv("FIREBASE_STORAGE_BUCKET"),
	}

	// Validate required config
	if err := cfg.Validate(); err != nil {
		return err
	}

	// Initialize MongoDB service
	mongoService, err := services.NewMongoDBService(cfg.MongoDBURI, cfg.MongoDBName)
	if err != nil {
		return err
	}

	// Initialize Gemini service
	geminiService, err := services.NewGeminiService(cfg.GeminiAPIKey, cfg.GeminiModel)
	if err != nil {
		return err
	}

	// Initialize Firebase service (optional)
	var firebaseService *services.FirebaseService
	if cfg.HasFirebase() {
		firebaseService, err = services.NewFirebaseService(cfg.FirebaseCredentials, cfg.FirebaseStorageBucket)
		if err != nil {
			log.Printf("Warning: Failed to initialize Firebase service: %v", err)
			log.Println("File upload feature will be disabled")
		}
	}

	// Initialize Line webhook handler
	lineWebhook, err = handlers.NewLineWebhookHandler(
		cfg.LineChannelSecret,
		cfg.LineChannelAccessToken,
		geminiService,
		mongoService,
		firebaseService,
	)
	if err != nil {
		return err
	}

	// Setup Gin engine
	gin.SetMode(gin.ReleaseMode)
	engine = gin.New()
	engine.Use(gin.Recovery())

	// Register webhook handler for any path since Vercel routes to this function
	engine.POST("/*any", lineWebhook.HandleWebhook)

	log.Println("Services initialized successfully")
	return nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
