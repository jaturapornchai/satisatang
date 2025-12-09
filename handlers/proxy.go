package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

type ProxyHandler struct {
	apiKey string
}

func NewProxyHandler() *ProxyHandler {
	return &ProxyHandler{
		apiKey: os.Getenv("GEMINI_API_KEY"),
	}
}

// Request structures to parse partial incoming data for "Simple Mode"
type SimpleRequest struct {
	Message string `json:"message"`
	Model   string `json:"model"`
}

// Full structure effectively just passes through, but we want to inspect 'model' if present.
// We can use a map[string]interface{} to just forward everything else.

func (h *ProxyHandler) HandleChat(c *gin.Context) {
	if h.apiKey == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "GEMINI_API_KEY not set"})
		return
	}

	// 1. Read body
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read request body"})
		return
	}

	// 2. Parse to map to manipulate structure
	var requestData map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &requestData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}

	// 3. Determine Model
	model := "gemini-2.0-flash-lite" // Default as per spec default (though spec says 2.5-flash-lite, 2.0 is usually current, sticking to spec default if valid or reasonable default)
	// Wait, spec says default: `gemini-2.5-flash-lite`. Let's use that.
	model = "gemini-2.5-flash-lite"

	if m, ok := requestData["model"].(string); ok && m != "" {
		model = m
		delete(requestData, "model") // Remove model from body as it goes in URL usually, or we can keep it if API ignores it. Gemini API usually takes it in URL.
	}

	// 4. Handle "Simple Mode" -> transform to "Full Mode"
	// Check if "message" exists and "contents" does not
	_, hasMessage := requestData["message"]
	_, hasContents := requestData["contents"]

	if hasMessage && !hasContents {
		msg, _ := requestData["message"].(string)
		// Construct contents
		contents := []map[string]interface{}{
			{
				"role": "user",
				"parts": []map[string]interface{}{
					{"text": msg},
				},
			},
		}
		requestData["contents"] = contents
		delete(requestData, "message")
	}

	// 5. Construct Upstream Request
	targetURL := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", model, h.apiKey)

	upstreamBody, err := json.Marshal(requestData)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to re-encode body"})
		return
	}

	req, err := http.NewRequest("POST", targetURL, bytes.NewBuffer(upstreamBody))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create upstream request"})
		return
	}
	req.Header.Set("Content-Type", "application/json")

	// 6. Execute Request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("Failed to call Gemini API: %v", err)})
		return
	}
	defer resp.Body.Close()

	// 7. Proxy Response back
	// Read upstream response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read upstream response"})
		return
	}

	// Set header and status
	c.Data(resp.StatusCode, "application/json", respBody)
}
