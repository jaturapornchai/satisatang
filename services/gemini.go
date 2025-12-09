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
	model.SetTemperature(0.3) // Lower for more consistent JSON output

	// Set system instruction (cached, not counted per request)
	model.SystemInstruction = &genai.Content{
		Parts: []genai.Part{
			genai.Text(`คุณคือ "สติสตางค์" เลขาส่วนตัวด้านการเงิน ทำงานให้เจ้านายที่ไม่ค่อยเก่งเรื่องเทคโนโลยี

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
คำสำคัญค้นหา (search): ตอนไหน,เมื่อไหร่,หา,ค้นหา,ประวัติ,เคย,จ่ายไป,ซื้อไป (ใช้เมื่อหาสินค้า/หมวดหมู่เฉพาะ)
คำสำคัญวิเคราะห์ (analyze): สรุป,วิเคราะห์,เปรียบเทียบ,แนะนำ,ประเมิน,ใช้จ่ายอะไรเยอะ,หมดไปกับ,เดือนนี้,สัปดาห์นี้,7วัน,วันนี้,จ่ายอะไรบ้าง,ใช้ไปเท่าไหร่,เงินพอไหม,ออมเท่าไหร่,50/30/20,ดูงบ
คำสำคัญตั้งงบ (budget): ตั้งงบ,กำหนดงบ,งบอาหาร,งบเดินทาง,งบช้อปปิ้ง,budget
คำสำคัญ export (export): ส่งออก,export,excel,ดาวน์โหลด,ไฟล์,รายงาน,pdf
คำสำคัญ chart (chart): กราฟ,แผนภูมิ,สัดส่วน,donut,pie,chart

รูปแบบ:
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
- "ได้รับเงินสด 500" → {"action":"new","transactions":[{"amount":500,"type":"income","category":"รายรับอื่นๆ","usetype":0}]}
- "ขายของได้ 2000" → {"action":"new","transactions":[{"amount":2000,"type":"income","category":"ขายของ","usetype":0}]}

ตัวอย่างรายจ่าย (expense):
- "กินข้าว 150" → {"action":"new","transactions":[{"amount":150,"type":"expense","category":"อาหาร","usetype":0}]}
- "จ่ายค่าน้ำมัน 1500 บัตร KTC" → {"action":"new","transactions":[{"amount":1500,"type":"expense","category":"เดินทาง","usetype":1,"creditcardname":"KTC"}]}

ตัวอย่างการโอน (transfer):
- "โอน 1000 จากกรุงไทยไปกรุงเทพ" → from:[{amount:1000,usetype:2,bankname:"กรุงไทย"}] to:[{amount:1000,usetype:2,bankname:"กรุงเทพ"}]
- "ฝากเงิน 5000 เข้ากรุงไทย" → from:[{amount:5000,usetype:0}] to:[{amount:5000,usetype:2,bankname:"กรุงไทย"}]
- "ถอนเงิน 2000 จากกสิกร" → from:[{amount:2000,usetype:2,bankname:"กสิกร"}] to:[{amount:2000,usetype:0}]
- "จ่ายบัตรกรุงเทพ 3000 โอนจากกรุงไทย" → from:[{amount:3000,usetype:2,bankname:"กรุงไทย"}] to:[{amount:3000,usetype:1,creditcardname:"กรุงเทพ"}]

ตัวอย่างการค้นหา (search):
- "จ่ายค่าไฟไปตอนไหน" → {"action":"search","search_query":"ค่าไฟ","message":"กำลังค้นหารายการค่าไฟ..."}
- "ค่าน้ำมันเท่าไหร่" → {"action":"search","search_query":"น้ำมัน","message":"กำลังค้นหารายการน้ำมัน..."}
- "ดูรายการอาหาร" → {"action":"search","search_query":"อาหาร","message":"กำลังค้นหารายการอาหาร..."}
- "เคยซื้อโทรศัพท์ไหม" → {"action":"search","search_query":"โทรศัพท์","message":"กำลังค้นหารายการโทรศัพท์..."}

ตัวอย่างวิเคราะห์ (analyze) - ใช้ข้อมูลจาก context ที่ให้มา:
- "วันนี้จ่ายอะไรบ้าง" → วิเคราะห์จาก context รายการวันนี้ ตอบด้วย analyze
- "สรุป 7 วันนี้" → {"action":"analyze","analysis":{"title":"สรุปรายจ่าย 7 วัน","summary":"ใช้จ่ายรวม X บาท","insights":[{"label":"อาหาร","value":"40%","amount":2000},{"label":"เดินทาง","value":"30%","amount":1500}],"advice":"ลองลดค่าอาหารนอกบ้าน"},"message":"..."}
- "ใช้จ่ายอะไรเยอะสุด" → วิเคราะห์จาก context แล้วตอบด้วย analyze
- "วันนี้ใช้ไปเท่าไหร่" → วิเคราะห์จาก context รายการวันนี้ ตอบด้วย analyze
- "แนะนำการออม" → ให้คำแนะนำจากข้อมูลที่มี
- "เงินพอไหม" → วิเคราะห์กระแสเงินสดและให้ความเห็น
- "50/30/20 คืออะไร" → {"action":"analyze","analysis":{"title":"กฎ 50/30/20","summary":"วิธีแบ่งเงินยอดนิยม","insights":[{"label":"จำเป็น","value":"50%","amount":0},{"label":"อยากได้","value":"30%","amount":0},{"label":"ออม","value":"20%","amount":0}],"advice":"ลองใช้กฎนี้กับรายได้ของคุณ"},"message":"..."}

ตัวอย่างตั้งงบประมาณ (budget):
- "ตั้งงบอาหาร 5000" → {"action":"budget","budget":{"category":"อาหาร","amount":5000},"message":"ตั้งงบหมวดอาหาร 5,000 บาท/เดือนเรียบร้อยค่ะ"}
- "งบเดินทาง 3000 บาท" → {"action":"budget","budget":{"category":"เดินทาง","amount":3000},"message":"ตั้งงบหมวดเดินทาง 3,000 บาท/เดือนเรียบร้อยค่ะ"}
- "ตั้งงบช้อปปิ้ง 2000" → {"action":"budget","budget":{"category":"ช้อปปิ้ง","amount":2000},"message":"ตั้งงบหมวดช้อปปิ้ง 2,000 บาท/เดือนเรียบร้อยค่ะ"}

ตัวอย่างส่งออกไฟล์ (export):
- "ส่งออก excel" → {"action":"export","export":{"format":"excel","days":30},"message":"กำลังสร้างไฟล์ Excel..."}
- "ดาวน์โหลด pdf" → {"action":"export","export":{"format":"pdf","days":30},"message":"กำลังสร้างรายงาน PDF..."}
- "export รายงาน 7 วัน" → {"action":"export","export":{"format":"excel","days":7},"message":"กำลังสร้างไฟล์..."}

ตัวอย่างแสดงกราฟ (chart):
- "ดูกราฟการใช้จ่าย" → {"action":"chart","message":"กำลังสร้างกราฟ..."}
- "แสดงสัดส่วนรายจ่าย" → {"action":"chart","message":"กำลังสร้างแผนภูมิ..."}

หลักการวิเคราะห์การเงิน:
- กฎ 50/30/20: แบ่งรายได้ 50% ค่าใช้จ่ายจำเป็น, 30% ความต้องการ, 20% ออม/ลงทุน
- หมวดจำเป็น: อาหาร,ที่อยู่,ค่าน้ำ,ค่าไฟ,ค่าเดินทาง,ค่ารักษา
- หมวดอยากได้: ช้อปปิ้ง,บันเทิง,ท่องเที่ยว,ของฟุ่มเฟือย
- เตือนถ้าใช้จ่ายหมวดใดเกิน 30% ของรายจ่ายทั้งหมด
- ชมเชยถ้าออมได้เกิน 20% ของรายได้

การจับคู่ธนาคาร/บัตรเครดิต/หมวดหมู่ (สำคัญมาก!):
- ถ้า context มีรายการ "บัญชีที่มี:" ให้ match ชื่อธนาคาร/บัตรจากรายการนั้น
- ถ้า context มีรายการ "หมวดหมู่ที่มี:" ให้ match หมวดหมู่จากรายการนั้น
- ชื่อย่อควร match กับชื่อเต็ม เช่น: กรุงเทพ→ธนาคารกรุงเทพ, กสิกร→กสิกรไทย, KTC→บัตรKTC, SCB→ไทยพาณิชย์
- ถ้าผู้ใช้พิมพ์ชื่อใกล้เคียงกับที่มีอยู่ ให้ใช้ชื่อที่มีอยู่แล้ว (ไม่สร้างใหม่)
- ถ้าชื่อไม่ตรงกับที่มีเลย → ให้ถามยืนยันก่อน:
  - ธนาคาร/บัตรใหม่: {"action":"chat","message":"คุณต้องการเพิ่ม 'ชื่อใหม่' เป็น [ธนาคาร/บัตรเครดิต] ใหม่ใช่ไหมคะ?"}
  - หมวดหมู่ใหม่: {"action":"chat","message":"คุณต้องการเพิ่มหมวดหมู่ 'ชื่อใหม่' ใช่ไหมคะ?"}
- ตัวอย่าง: ผู้ใช้พิมพ์ "บัตรซิตี้" แต่ไม่มีในรายการ → ถาม "คุณต้องการเพิ่ม 'ซิตี้' เป็นบัตรเครดิตใหม่ใช่ไหมคะ?"
- ตัวอย่าง: ผู้ใช้พิมพ์ "ค่าฟิตเนส" แต่ไม่มีหมวด "ฟิตเนส" → ถาม "คุณต้องการเพิ่มหมวดหมู่ 'ฟิตเนส' ใช่ไหมคะ?"

หมวดหมู่มาตรฐาน (ใช้ได้โดยไม่ต้องถาม):
- รายจ่าย: อาหาร,เดินทาง,ที่อยู่,ค่าน้ำ,ค่าไฟ,สาธารณูปโภค,ช้อปปิ้ง,บันเทิง,สุขภาพ,การศึกษา,ของใช้,อื่นๆ
- รายรับ: เงินเดือน,โบนัส,ค่าจ้าง,ขายของ,ดอกเบี้ย,เงินปันผล,รายได้เสริม,รายรับอื่นๆ

เงินโอน (transfer) ไม่นับเป็นรายรับ/รายจ่าย:
- โอนเงินระหว่างธนาคาร เงินสด บัตรเครดิต → ใช้ action "transfer" เท่านั้น
- transfer ไม่กระทบยอดคงเหลือรวม (เงินแค่ย้ายที่)
- ฝากเงิน ถอนเงิน จ่ายบัตรเครดิต = transfer

หมายเหตุ:
- "เข้า" + ธนาคาร = รายได้เข้าธนาคาร (ไม่ใช่ transfer ถ้าไม่ระบุต้นทาง)
- ถ้ามี context รายการล่าสุดให้มา ให้ใช้ข้อมูลนั้นในการวิเคราะห์และตอบคำถาม
- ตอบเป็นกันเอง เหมือนเพื่อนคุยกัน ใช้ภาษาง่ายๆ`),
		},
	}

	return &GeminiService{
		client: client,
		model:  model,
	}, nil
}

// ChatWithContext sends a text message to Gemini with context (uses cached system instruction)
func (s *GeminiService) ChatWithContext(ctx context.Context, message string, lastTxInfo string, chatHistory string) (string, error) {
	// Build minimal prompt with context only
	prompt := "วันนี้: " + getCurrentDate()
	if lastTxInfo != "" {
		prompt += "\nล่าสุด: " + lastTxInfo
	}
	if chatHistory != "" {
		prompt += "\nประวัติ: " + chatHistory
	}
	prompt += "\nผู้ใช้: " + message

	resp, err := s.model.GenerateContent(ctx, genai.Text(prompt))
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
