package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	// Server
	Port    string
	GinMode string

	// Line OA
	LineChannelSecret      string
	LineChannelAccessToken string

	// Gemini AI
	GeminiAPIKey string
	GeminiModel  string

	// MongoDB Atlas
	MongoDBURI  string
	MongoDBName string

	// Firebase Cloud Storage (optional)
	FirebaseCredentials   string // JSON string of service account credentials
	FirebaseStorageBucket string
}

func (c *Config) HasFirebase() bool {
	return c.FirebaseCredentials != "" && c.FirebaseStorageBucket != ""
}

func Load() (*Config, error) {
	// Load .env file if exists
	_ = godotenv.Load()

	cfg := &Config{
		Port:                   getEnv("PORT", "3000"),
		GinMode:                getEnv("GIN_MODE", "debug"),
		LineChannelSecret:      getEnv("LINE_CHANNEL_SECRET", ""),
		LineChannelAccessToken: getEnv("LINE_CHANNEL_ACCESS_TOKEN", ""),
		GeminiAPIKey:           getEnv("GEMINI_API_KEY", ""),
		GeminiModel:            getEnv("GEMINI_MODEL", "gemini-2.5-flash-lite"),
		MongoDBURI:             getEnv("MONGODB_ATLAS_URI", ""),
		MongoDBName:            getEnv("MONGODB_ATLAS_DBNAME", "satistang"),
		FirebaseCredentials:    getEnv("FIREBASE_CREDENTIALS", ""),
		FirebaseStorageBucket:  getEnv("FIREBASE_STORAGE_BUCKET", ""),
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) Validate() error {
	if c.LineChannelSecret == "" {
		return fmt.Errorf("LINE_CHANNEL_SECRET is required")
	}
	if c.LineChannelAccessToken == "" {
		return fmt.Errorf("LINE_CHANNEL_ACCESS_TOKEN is required")
	}
	if c.GeminiAPIKey == "" {
		return fmt.Errorf("GEMINI_API_KEY is required")
	}
	if c.MongoDBURI == "" {
		return fmt.Errorf("MONGODB_ATLAS_URI is required")
	}
	return nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
