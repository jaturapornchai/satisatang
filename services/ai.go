package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func getCurrentDate() string {
	return time.Now().Format("2006-01-02")
}

// TransactionData represents extracted receipt data
type TransactionData struct {
	ImageType      string            `json:"image_type"` // "receipt" or "slip"
	Date           string            `json:"date"`
	Merchant       string            `json:"merchant"`
	Amount         float64           `json:"amount"`
	Category       string            `json:"category"`
	Type           string            `json:"type"`
	Description    string            `json:"description"`
	Items          []TransactionItem `json:"items"`
	UseType        int               `json:"usetype"` // 0=เงินสด, 1=บัตรเครดิต, 2=ธนาคาร
	BankName       string            `json:"bankname"`
	CreditCardName string            `json:"creditcardname"`
	// Slip-specific fields
	FromName    string `json:"from_name"`    // ผู้โอน
	FromBank    string `json:"from_bank"`    // ธนาคารผู้โอน
	FromAccount string `json:"from_account"` // เลขบัญชีผู้โอน
	ToName      string `json:"to_name"`      // ผู้รับ
	ToBank      string `json:"to_bank"`      // ธนาคารผู้รับ
	ToAccount   string `json:"to_account"`   // เลขบัญชีผู้รับ
	RefNo       string `json:"ref_no"`       // เลขอ้างอิง
	// Image storage fields
	ImageBase64   string `json:"image_base64,omitempty"`   // รูปภาพ base64
	ImageMimeType string `json:"image_mime_type,omitempty"` // mime type ของรูป
}

// TransferEntry represents a single transfer source or destination
type TransferEntry struct {
	Amount         float64 `json:"amount"`
	UseType        int     `json:"usetype"` // 0=เงินสด, 1=บัตรเครดิต, 2=ธนาคาร
	BankName       string  `json:"bankname"`
	CreditCardName string  `json:"creditcardname"`
}

// TransferData represents transfers between accounts (many-to-many)
type TransferData struct {
	From        []TransferEntry `json:"from"` // ต้นทาง (หลายบัญชีได้)
	To          []TransferEntry `json:"to"`   // ปลายทาง (หลายบัญชีได้)
	Description string          `json:"description"`
}

// AnalysisInsight represents a single insight item in analysis
type AnalysisInsight struct {
	Label  string  `json:"label"`
	Value  string  `json:"value"`
	Amount float64 `json:"amount"`
}

// AnalysisData represents AI analysis result
type AnalysisData struct {
	Title    string            `json:"title"`
	Summary  string            `json:"summary"`
	Insights []AnalysisInsight `json:"insights"`
	Advice   string            `json:"advice"`
}

// BudgetData represents budget setting from AI
type BudgetData struct {
	Category string  `json:"category"`
	Amount   float64 `json:"amount"`
}

// ExportData represents export request from AI
type ExportData struct {
	Format string `json:"format"` // "excel" or "pdf"
	Days   int    `json:"days"`   // number of days to export (default 30)
}

// QueryFilter represents AI-generated query parameters for MongoDB
type QueryFilter struct {
	Type       string   `json:"type"`       // "income", "expense", "all"
	Categories []string `json:"categories"` // filter by categories
	DateFrom   string   `json:"date_from"`  // YYYY-MM-DD
	DateTo     string   `json:"date_to"`    // YYYY-MM-DD
	Days       int      `json:"days"`       // shortcut: last N days
	UseType    int      `json:"usetype"`    // -1=all, 0=cash, 1=credit, 2=bank
	BankName   string   `json:"bankname"`   // filter by bank
	Keyword    string   `json:"keyword"`    // search keyword
	GroupBy    string   `json:"group_by"`   // "category", "date", "payment", "none"
	Limit      int      `json:"limit"`      // max results
}

// AIResponse represents the AI's response with action
type AIResponse struct {
	Action       string            `json:"action"`       // "new", "update", "transfer", "balance", "search", "analyze", "budget", "export", "chat"
	Transactions []TransactionData `json:"transactions"` // for "new" action
	Transfer     *TransferData     `json:"transfer"`     // for "transfer" action
	UpdateField  string            `json:"update_field"` // "amount", "usetype", etc.
	UpdateValue  interface{}       `json:"update_value"`
	Query        *QueryFilter      `json:"query"`  // for balance/search/analyze - AI creates query
	Budget       *BudgetData       `json:"budget"` // for "budget" action
	Export       *ExportData       `json:"export"` // for "export" action
	Message      string            `json:"message"`
}

type TransactionItem struct {
	Name     string  `json:"name"`
	Quantity float64 `json:"quantity"`
	Price    float64 `json:"price"`
}

const (
	aiAPIEndpoint = "https://aiapi-e4y6ekwr1-jaturapornchais-projects.vercel.app/api/chat"
	aiAPITimeout  = 60 * time.Second
)

// AIChat interface for AI services
type AIChat interface {
	ChatWithContext(ctx context.Context, message string, lastTxInfo string, chatHistory string) (string, error)
	ProcessReceiptImage(ctx context.Context, imageData io.Reader, mimeType string) (*TransactionData, error)
	Close() error
}

// AIService handles AI chat via external API
type AIService struct {
	httpClient     *http.Client
	systemPrompt   string
	examplesPrompt string
	receiptPrompt  string
}

// AIAPIRequest represents the request to AI API
type AIAPIRequest struct {
	Message string `json:"message"`
}

// AIAPIResponse represents the response from AI API (simple format)
type AIAPIResponse struct {
	Response string `json:"response"`
	Model    string `json:"model"`
	Error    string `json:"error,omitempty"`
}

// GeminiResponse represents the raw Gemini API response format
type GeminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	Error string `json:"error,omitempty"`
}

func NewAIService() *AIService {
	svc := &AIService{
		httpClient: &http.Client{
			Timeout: aiAPITimeout,
		},
	}
	svc.loadPrompts()
	return svc
}

// loadPrompts loads prompt templates from markdown files
func (s *AIService) loadPrompts() {
	// Try to find prompts directory
	promptsDir := findPromptsDir()

	// Load system prompt
	s.systemPrompt = loadPromptFile(filepath.Join(promptsDir, "system.md"))
	if s.systemPrompt == "" {
		s.systemPrompt = getDefaultSystemPrompt()
	}

	// Load examples prompt
	s.examplesPrompt = loadPromptFile(filepath.Join(promptsDir, "examples.md"))

	// Load receipt prompt
	s.receiptPrompt = loadPromptFile(filepath.Join(promptsDir, "receipt.md"))
	if s.receiptPrompt == "" {
		s.receiptPrompt = getDefaultReceiptPrompt()
	}

	log.Printf("Loaded prompts from: %s", promptsDir)
}

// findPromptsDir finds the prompts directory
func findPromptsDir() string {
	// Try relative paths
	paths := []string{
		"prompts",
		"./prompts",
		"../prompts",
	}

	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	return "prompts"
}

// loadPromptFile reads a prompt file and returns its content
func loadPromptFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("Could not load prompt file %s: %v", path, err)
		return ""
	}
	return strings.TrimSpace(string(data))
}

func getDefaultSystemPrompt() string {
	return `คุณคือ "สติสตางค์" ตอบ JSON เท่านั้น
action: new|update|transfer|balance|search|analyze|budget|export|chat
usetype: 0=เงินสด, 1=บัตรเครดิต, 2=ธนาคาร
type: income|expense`
}

func getDefaultReceiptPrompt() string {
	return `วิเคราะห์ใบเสร็จนี้และตอบเป็น JSON: {"date":"YYYY-MM-DD","merchant":"ร้าน","amount":0,"category":"หมวด","type":"expense","description":"รายละเอียด","usetype":0}`
}

// ChatWithContext sends a message to AI API with context
// schema contains user's data structure: "ธนาคาร:SCB,KBank|บัตร:CITI|หมวด:อาหาร,เดินทาง"
// chatHistory contains recent messages in format "user: xxx\nassistant: yyy\n..."
func (s *AIService) ChatWithContext(ctx context.Context, message string, schema string, chatHistory string) (string, error) {
	// Build prompt with system instruction, examples, and context
	prompt := s.systemPrompt

	// Add examples if available
	if s.examplesPrompt != "" {
		prompt += "\n\n" + s.examplesPrompt
	}

	prompt += "\n\n---\n\n"
	prompt += "วันนี้: " + getCurrentDate()

	if schema != "" {
		prompt += "\nข้อมูลที่มี: " + schema
	}

	// Add chat history for context
	if chatHistory != "" {
		prompt += "\n\nประวัติการสนทนา:\n" + chatHistory
	}

	prompt += "\n\nผู้ใช้: " + message

	// Call AI API
	reqBody := AIAPIRequest{Message: prompt}
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", aiAPIEndpoint, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call AI API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("AI API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Log raw response for debugging
	log.Printf("AI API raw response: %s", string(body))

	// Try parsing as simple format first
	var apiResp AIAPIResponse
	if err := json.Unmarshal(body, &apiResp); err == nil && apiResp.Response != "" {
		return apiResp.Response, nil
	}

	// Try parsing as Gemini raw format
	var geminiResp GeminiResponse
	if err := json.Unmarshal(body, &geminiResp); err != nil {
		return "", fmt.Errorf("failed to parse AI response: %w (raw: %s)", err, string(body))
	}

	if geminiResp.Error != "" {
		return "", fmt.Errorf("AI API error: %s", geminiResp.Error)
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("empty response from AI API")
	}

	return geminiResp.Candidates[0].Content.Parts[0].Text, nil
}

// ProcessReceiptImage processes receipt image via AI API simplified image endpoint
func (s *AIService) ProcessReceiptImage(ctx context.Context, imageData io.Reader, mimeType string) (*TransactionData, error) {
	// Read image data
	imgBytes, err := io.ReadAll(imageData)
	if err != nil {
		return nil, fmt.Errorf("failed to read image data: %w", err)
	}

	// Convert to base64
	base64Image := base64.StdEncoding.EncodeToString(imgBytes)

	// Use receipt prompt from file + current date
	receiptPrompt := s.receiptPrompt + "\n\nวันที่ปัจจุบัน: " + getCurrentDate()

	// Use /api/chat with contents format (Gemini full mode)
	reqBody := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"role": "user",
				"parts": []map[string]interface{}{
					{"text": receiptPrompt},
					{
						"inlineData": map[string]string{
							"mimeType": mimeType,
							"data":     base64Image,
						},
					},
				},
			},
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", aiAPIEndpoint, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call AI API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("AI API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Parse Gemini response format
	var geminiResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		Error string `json:"error,omitempty"`
	}

	if err := json.Unmarshal(body, &geminiResp); err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w", err)
	}

	if geminiResp.Error != "" {
		return nil, fmt.Errorf("AI API error: %s", geminiResp.Error)
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("empty response from AI API")
	}

	responseText := geminiResp.Candidates[0].Content.Parts[0].Text

	// Clean JSON response (remove markdown code blocks if present)
	responseText = cleanJSONResponse(responseText)

	// Parse transaction data
	var txData TransactionData
	if err := json.Unmarshal([]byte(responseText), &txData); err != nil {
		return nil, fmt.Errorf("failed to parse transaction data: %w (response: %s)", err, responseText)
	}

	return &txData, nil
}

// cleanJSONResponse removes markdown code blocks from JSON response
func cleanJSONResponse(s string) string {
	// Remove ```json prefix and ``` suffix if present
	if len(s) > 7 && s[:7] == "```json" {
		s = s[7:]
	} else if len(s) > 3 && s[:3] == "```" {
		s = s[3:]
	}
	// Remove trailing ```
	if len(s) > 3 && s[len(s)-3:] == "```" {
		s = s[:len(s)-3]
	}
	// Trim whitespace
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\n' || s[0] == '\r' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

// Close closes the AI service (no-op for HTTP client)
func (s *AIService) Close() error {
	return nil
}
