package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

func main() {
	baseURL := "http://localhost:8080/api/chat"

	// Test 1: Simple Mode
	fmt.Println("--- Test 1: Simple Mode ---")
	simplePayload := map[string]interface{}{
		"message": "Hello, answer in 1 word.",
	}
	sendRequest(baseURL, simplePayload)

	// Test 2: Full Mode
	fmt.Println("\n--- Test 2: Full Mode ---")
	fullPayload := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"role": "user",
				"parts": []map[string]interface{}{
					{"text": "What is 2+2?"},
				},
			},
		},
	}
	sendRequest(baseURL, fullPayload)

	// Test 3: Model Switch
	fmt.Println("\n--- Test 3: Model Switch ---")
	modelPayload := map[string]interface{}{
		"model":   "gemini-2.0-flash", // Testing slightly different string if valid, or just logic
		"message": "Hello from flash",
	}
	sendRequest(baseURL, modelPayload)

	// Manual check note
	fmt.Println("\nNote: Make sure GEMINI_API_KEY is set in environment or .env when running the server.")
}

func sendRequest(url string, payload map[string]interface{}) {
	jsonBody, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("Status: %s\n", resp.Status)
	fmt.Printf("Response: %s\n", string(body))
}
