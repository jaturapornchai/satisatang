package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

func getCurrentDate() string {
	return time.Now().Format("2006-01-02")
}

// TransactionData represents extracted receipt data
type TransactionData struct {
	Date           string            `json:"date"`
	Merchant       string            `json:"merchant"`
	Amount         float64           `json:"amount"`
	Category       string            `json:"category"`
	Type           string            `json:"type"`
	Description    string            `json:"description"`
	Items          []TransactionItem `json:"items"`
	UseType        int               `json:"usetype"`        // 0=เงินสด, 1=บัตรเครดิต, 2=ธนาคาร
	BankName       string            `json:"bankname"`
	CreditCardName string            `json:"creditcardname"`
}

// TransferEntry represents a single transfer source or destination
type TransferEntry struct {
	Amount         float64 `json:"amount"`
	UseType        int     `json:"usetype"`        // 0=เงินสด, 1=บัตรเครดิต, 2=ธนาคาร
	BankName       string  `json:"bankname"`
	CreditCardName string  `json:"creditcardname"`
}

// TransferData represents transfers between accounts (many-to-many)
type TransferData struct {
	From        []TransferEntry `json:"from"`        // ต้นทาง (หลายบัญชีได้)
	To          []TransferEntry `json:"to"`          // ปลายทาง (หลายบัญชีได้)
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

// AIResponse represents the AI's response with action
type AIResponse struct {
	Action         string            `json:"action"`          // "new", "update", "transfer", "balance", "search", "analyze", "budget", "export", "chart", "chat"
	Transactions   []TransactionData `json:"transactions"`    // for "new" action
	Transfer       *TransferData     `json:"transfer"`        // for "transfer" action (many-to-many)
	UpdateTxID     string            `json:"update_txid"`     // for "update" action
	UpdateField    string            `json:"update_field"`    // "amount", "usetype", etc.
	UpdateValue    interface{}       `json:"update_value"`
	SearchQuery    string            `json:"search_query"`    // for "search" action - keyword to search
	Analysis       *AnalysisData     `json:"analysis"`        // for "analyze" action
	Budget         *BudgetData       `json:"budget"`          // for "budget" action
	Export         *ExportData       `json:"export"`          // for "export" action
	Message        string            `json:"message"`         // for "chat" action
}

type TransactionItem struct {
	Name     string  `json:"name"`
	Quantity float64 `json:"quantity"`
	Price    float64 `json:"price"`
}

const (
	aiAPIEndpoint = "https://aiapi-bjvy6dhba-jaturapornchais-projects.vercel.app/api/chat"
	aiAPITimeout  = 30 * time.Second
)

// AIChat interface for AI services
type AIChat interface {
	ChatWithContext(ctx context.Context, message string, lastTxInfo string, chatHistory string) (string, error)
	ProcessReceiptImage(ctx context.Context, imageData io.Reader, mimeType string) (*TransactionData, error)
	Close() error
}

// AIService handles AI chat via external API
type AIService struct {
	httpClient *http.Client
	systemPrompt string
}

// AIAPIRequest represents the request to AI API
type AIAPIRequest struct {
	Message string `json:"message"`
}

// AIAPIResponse represents the response from AI API
type AIAPIResponse struct {
	Response string `json:"response"`
	Model    string `json:"model"`
	Error    string `json:"error,omitempty"`
}

func NewAIService() *AIService {
	return &AIService{
		httpClient: &http.Client{
			Timeout: aiAPITimeout,
		},
		systemPrompt: buildSystemPrompt(),
	}
}

func buildSystemPrompt() string {
	return `คุณคือ "สติสตางค์" เลขาส่วนตัวด้านการเงิน ทำงานให้เจ้านายที่ไม่ค่อยเก่งเรื่องเทคโนโลยี

บทบาทของคุณ:
- เป็นเลขาที่ใส่ใจ ช่วยจดบันทึก ย้ำทวน และยืนยันก่อนทำการสำคัญ
- ไม่ทึกทักเอาเอง ถ้าไม่แน่ใจให้ถามกลับ
- ใช้ภาษาง่ายๆ เหมือนคุยกับเพื่อน ไม่ใช้ศัพท์เทคนิค
- สรุปให้หลังทำรายการเสร็จ
- ถ้าเห็นว่างบเกิน หรือมีอะไรผิดปกติ ให้เตือนด้วยความห่วงใย

ตอบเป็น JSON เท่านั้น:
action: new|update|transfer|balance|search|analyze|budget|export|chart|chat
usetype: 0=เงินสด,1=บัตรเครดิต,2=ธนาคาร
type: "income"=รายรับ(เงินเข้า), "expense"=รายจ่าย(เงินออก)

คำสำคัญรายได้ (income): เงินเดือน,โบนัส,ได้รับ,รายรับ,เงินเข้า,ขายของได้,ค่าจ้าง,รายได้,ได้เงิน,รับเงิน,เก็บเงิน
คำสำคัญรายจ่าย (expense): จ่าย,ซื้อ,กิน,ค่า,รายจ่าย,เสีย,หมดไป,โดน,จัดไป
คำสำคัญค้นหา (search): ตอนไหน,เมื่อไหร่,หา,ค้นหา,ประวัติ,เคย,จ่ายไป,ซื้อไป
คำสำคัญวิเคราะห์ (analyze): สรุป,วิเคราะห์,เปรียบเทียบ,แนะนำ,ประเมิน,ใช้จ่ายอะไรเยอะ,หมดไปกับ,เดือนนี้,สัปดาห์นี้,7วัน,วันนี้,จ่ายอะไรบ้าง,ใช้ไปเท่าไหร่,เงินพอไหม,ออมเท่าไหร่,50/30/20,ดูงบ
คำสำคัญตั้งงบ (budget): ตั้งงบ,กำหนดงบ,งบอาหาร,งบเดินทาง,งบช้อปปิ้ง,budget
คำสำคัญ export (export): ส่งออก,export,excel,ดาวน์โหลด,ไฟล์,รายงาน,pdf
คำสำคัญ chart (chart): กราฟ,แผนภูมิ,สัดส่วน,donut,pie,chart

รูปแบบ JSON:
1. รายการใหม่: {"action":"new","transactions":[{"amount":100,"type":"expense|income","category":"...","description":"...","usetype":0,"bankname":"","creditcardname":""}],"message":"..."}
2. แก้ไข: {"action":"update","update_field":"amount|usetype","update_value":...,"message":"..."}
3. โอน/ฝาก/ถอน: {"action":"transfer","transfer":{"from":[...],"to":[...],"description":"..."},"message":"..."}
4. ดูยอด: {"action":"balance","message":"..."}
5. ค้นหา: {"action":"search","search_query":"คำค้น","message":"..."}
6. วิเคราะห์/สรุป: {"action":"analyze","analysis":{"title":"หัวข้อ","summary":"สรุปสั้นๆ","insights":[{"label":"หมวด","value":"ค่า","amount":1000}],"advice":"คำแนะนำ"},"message":"..."}
7. ตั้งงบประมาณ: {"action":"budget","budget":{"category":"อาหาร","amount":5000},"message":"..."}
8. ส่งออกไฟล์: {"action":"export","export":{"format":"excel|pdf","days":30},"message":"..."}
9. แสดงกราฟ: {"action":"chart","message":"..."}
10. สนทนา: {"action":"chat","message":"..."}

ตัวอย่างรายได้ (income):
- "เงินเดือน 30000 เข้ากรุงไทย" → {"action":"new","transactions":[{"amount":30000,"type":"income","category":"เงินเดือน","usetype":2,"bankname":"กรุงไทย"}]}
- "โบนัส 10000 เข้า SCB" → {"action":"new","transactions":[{"amount":10000,"type":"income","category":"โบนัส","usetype":2,"bankname":"SCB"}]}

ตัวอย่างรายจ่าย (expense):
- "กินข้าว 150" → {"action":"new","transactions":[{"amount":150,"type":"expense","category":"อาหาร","usetype":0}]}
- "จ่ายค่าน้ำมัน 1500 บัตร KTC" → {"action":"new","transactions":[{"amount":1500,"type":"expense","category":"เดินทาง","usetype":1,"creditcardname":"KTC"}]}

ตัวอย่างการโอน (transfer):
- "โอน 1000 จากกรุงไทยไปกรุงเทพ" → from:[{amount:1000,usetype:2,bankname:"กรุงไทย"}] to:[{amount:1000,usetype:2,bankname:"กรุงเทพ"}]
- "ฝากเงิน 5000 เข้ากรุงไทย" → from:[{amount:5000,usetype:0}] to:[{amount:5000,usetype:2,bankname:"กรุงไทย"}]
- "ถอนเงิน 2000 จากกสิกร" → from:[{amount:2000,usetype:2,bankname:"กสิกร"}] to:[{amount:2000,usetype:0}]

การจับคู่ธนาคาร/บัตรเครดิต/หมวดหมู่:
- ถ้า context มี "บัญชีที่มี:" ให้ match ชื่อจากรายการนั้น
- ถ้า context มี "หมวดหมู่ที่มี:" ให้ match หมวดหมู่จากรายการนั้น
- ถ้าชื่อไม่ตรงกับที่มี → ให้ถามยืนยันก่อน

หมวดหมู่มาตรฐาน:
- รายจ่าย: อาหาร,เดินทาง,ที่อยู่,ค่าน้ำ,ค่าไฟ,สาธารณูปโภค,ช้อปปิ้ง,บันเทิง,สุขภาพ,การศึกษา,ของใช้,อื่นๆ
- รายรับ: เงินเดือน,โบนัส,ค่าจ้าง,ขายของ,ดอกเบี้ย,เงินปันผล,รายได้เสริม,รายรับอื่นๆ

เงินโอน (transfer) ไม่นับเป็นรายรับ/รายจ่าย`
}

// ChatWithContext sends a message to AI API with context
func (s *AIService) ChatWithContext(ctx context.Context, message string, lastTxInfo string, chatHistory string) (string, error) {
	// Build prompt with system instruction and context
	prompt := s.systemPrompt + "\n\n---\n\n"
	prompt += "วันนี้: " + getCurrentDate()
	if lastTxInfo != "" {
		prompt += "\nล่าสุด: " + lastTxInfo
	}
	if chatHistory != "" {
		prompt += "\nประวัติ: " + chatHistory
	}
	prompt += "\nผู้ใช้: " + message

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

	var apiResp AIAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse AI response: %w", err)
	}

	if apiResp.Error != "" {
		return "", fmt.Errorf("AI API error: %s", apiResp.Error)
	}

	return apiResp.Response, nil
}

// ProcessReceiptImage processes receipt image (not supported by AI API)
func (s *AIService) ProcessReceiptImage(ctx context.Context, imageData io.Reader, mimeType string) (*TransactionData, error) {
	return nil, fmt.Errorf("image processing not supported by AI API")
}

// Close closes the AI service (no-op for HTTP client)
func (s *AIService) Close() error {
	return nil
}
