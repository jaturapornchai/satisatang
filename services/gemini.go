package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

func getCurrentDate() string {
	return time.Now().Format("2006-01-02")
}

// TransactionData represents extracted receipt data
type TransactionData struct {
	Date        string            `json:"date"`
	Merchant    string            `json:"merchant"`
	Amount      float64           `json:"amount"`
	Category    string            `json:"category"`
	Type        string            `json:"type"`
	Description string            `json:"description"`
	Items       []TransactionItem `json:"items"`
}

type TransactionItem struct {
	Name     string  `json:"name"`
	Quantity float64 `json:"quantity"`
	Price    float64 `json:"price"`
}

type GeminiService struct {
	client *genai.Client
	model  *genai.GenerativeModel
}

func NewGeminiService(apiKey, modelName string) (*GeminiService, error) {
	ctx := context.Background()
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini client: %w", err)
	}

	model := client.GenerativeModel(modelName)
	model.SetTemperature(0.7)

	return &GeminiService{
		client: client,
		model:  model,
	}, nil
}

// Chat sends a text message to Gemini and returns the response
func (s *GeminiService) Chat(ctx context.Context, message string) (string, error) {
	systemPrompt := `คุณคือผู้ช่วยระบบการเงินส่วนบุคคล "สติสตางค์"

หน้าที่ของคุณคือ:
1. วิเคราะห์ข้อความที่ได้รับว่าเป็น รายรับ หรือ รายจ่าย
2. ดึงข้อมูลสำคัญ: จำนวนเงิน, หมวดหมู่, รายละเอียด
3. ตอบกลับเป็น JSON array format เสมอ (แม้มีรายการเดียว):

[
  {
    "type": "expense" หรือ "income",
    "amount": จำนวนเงิน (ตัวเลข),
    "category": "หมวดหมู่ เช่น อาหาร, เดินทาง, เงินเดือน, ขายของ",
    "description": "รายละเอียดสั้นๆ",
    "date": "YYYY-MM-DD" (ถ้าระบุ หรือใช้วันนี้)
  }
]

ตัวอย่าง:
- "กินข้าว 50 บาท" → [{"type":"expense","amount":50,"category":"อาหาร","description":"กินข้าว","date":"` + getCurrentDate() + `"}]
- "กาแฟ 55 บาท ข้าวผัด 60 บาท" → [{"type":"expense","amount":55,"category":"อาหาร","description":"กาแฟ","date":"` + getCurrentDate() + `"},{"type":"expense","amount":60,"category":"อาหาร","description":"ข้าวผัด","date":"` + getCurrentDate() + `"}]

สำคัญ: ตอบเป็น JSON array เท่านั้น ไม่ต้องมีคำอธิบายเพิ่มเติม
หากข้อความไม่เกี่ยวกับการเงิน ให้ตอบเป็นข้อความปกติ (ไม่ต้องเป็น JSON)`

	fullPrompt := systemPrompt + "\n\nข้อความจากผู้ใช้: " + message

	resp, err := s.model.GenerateContent(ctx, genai.Text(fullPrompt))
	if err != nil {
		return "", fmt.Errorf("failed to generate content: %w", err)
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("no response from Gemini")
	}

	var responseText string
	for _, part := range resp.Candidates[0].Content.Parts {
		if text, ok := part.(genai.Text); ok {
			responseText += string(text)
		}
	}

	return responseText, nil
}

func (s *GeminiService) ProcessReceiptImage(ctx context.Context, imageData io.Reader, mimeType string) (*TransactionData, error) {
	imgBytes, err := io.ReadAll(imageData)
	if err != nil {
		return nil, fmt.Errorf("failed to read image: %w", err)
	}

	prompt := `วิเคราะห์ใบเสร็จในภาพนี้และแปลงเป็น JSON โดยใช้โครงสร้างดังนี้:

{
  "date": "YYYY-MM-DD",
  "merchant": "ชื่อร้านค้า",
  "amount": ยอดรวมทั้งหมด (เป็นตัวเลข),
  "category": "หมวดหมู่ เช่น อาหาร, ของใช้, เดินทาง, ฯลฯ",
  "type": "expense" (ค่าใช้จ่าย) หรือ "income" (รายรับ),
  "items": [
    {
      "name": "ชื่อสินค้า",
      "quantity": จำนวน (เป็นตัวเลข),
      "price": ราคา (เป็นตัวเลข)
    }
  ]
}

กรุณาตอบเป็น JSON เท่านั้น ไม่ต้องมีคำอธิบายเพิ่มเติม หากไม่สามารถอ่านค่าได้ ให้ใส่ null หรือ [] สำหรับ items
หากเป็นใบเสร็จภาษาไทย ให้แปลงวันที่พุทธศักราชเป็นคริสต์ศักราช`

	resp, err := s.model.GenerateContent(ctx,
		genai.Text(prompt),
		genai.ImageData(mimeType, imgBytes),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to generate content: %w", err)
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("no response from Gemini")
	}

	var responseText string
	for _, part := range resp.Candidates[0].Content.Parts {
		if text, ok := part.(genai.Text); ok {
			responseText += string(text)
		}
	}

	responseText = cleanJSONResponse(responseText)
	log.Printf("Gemini response: %s", responseText)

	var transactionData TransactionData
	if err := json.Unmarshal([]byte(responseText), &transactionData); err != nil {
		return nil, fmt.Errorf("failed to parse JSON response: %w", err)
	}

	return &transactionData, nil
}

func (s *GeminiService) Close() error {
	return s.client.Close()
}

func cleanJSONResponse(text string) string {
	if len(text) > 7 && text[:7] == "```json" {
		text = text[7:]
	}
	if len(text) > 3 && text[:3] == "```" {
		text = text[3:]
	}
	if len(text) > 3 && text[len(text)-3:] == "```" {
		text = text[:len(text)-3]
	}
	return trimWhitespace(text)
}

func trimWhitespace(s string) string {
	start := 0
	end := len(s)

	for start < end && (s[start] == ' ' || s[start] == '\n' || s[start] == '\r' || s[start] == '\t') {
		start++
	}

	for end > start && (s[end-1] == ' ' || s[end-1] == '\n' || s[end-1] == '\r' || s[end-1] == '\t') {
		end--
	}

	return s[start:end]
}
