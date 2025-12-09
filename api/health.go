package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Handler is the serverless function entry point for Vercel
func Handler(w http.ResponseWriter, r *http.Request) {
	// Create a Gin engine
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()

	// Define the health endpoint
	engine.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status":  "ok",
			"service": "satisatang",
		})
	})

	// Serve the request
	engine.ServeHTTP(w, r)
}
