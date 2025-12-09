//go:build ignore

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/satisatang/backend/services"
)

type TestCase struct {
	Input          string
	ExpectedAction string
	Description    string
}

func main() {
	godotenv.Load()

	apiKey := os.Getenv("GEMINI_API_KEY")
	model := os.Getenv("GEMINI_MODEL")
	if model == "" {
		model = "gemini-2.0-flash-lite"
	}

	gemini, err := services.NewGeminiService(apiKey, model)
	if err != nil {
		fmt.Printf("ERROR: %v\n", err)
		return
	}
	defer gemini.Close()

	// Mock context - realistic data
	mockContext := `รายการ 7 วันล่าสุด:
- 2024-12-09: รายจ่าย 150 บาท (ข้าวกะเพรา) เงินสด
- 2024-12-09: รายจ่าย 45 บาท (กาแฟ) เงินสด
- 2024-12-09: รายจ่าย 1500 บาท (เติมน้ำมัน) บัตรKTC
- 2024-12-08: รายจ่าย 350 บาท (ซื้อของที่ Lotus) ธ.กสิกร
- 2024-12-08: รายรับ 50000 บาท (เงินเดือน) ธ.กรุงไทย
- 2024-12-07: รายจ่าย 200 บาท (ค่าไฟ) ธ.กรุงไทย
- 2024-12-07: รายจ่าย 89 บาท (Netflix) บัตรกรุงเทพ
- 2024-12-06: รายจ่าย 250 บาท (ข้าวมันไก่) เงินสด
- 2024-12-05: รายจ่าย 500 บาท (ค่าโทรศัพท์ AIS) ธ.กสิกร
- 2024-12-04: รายจ่าย 1200 บาท (ซื้อเสื้อ) บัตรKTC

สรุป 7 วัน: รายรับ 50000 บาท, รายจ่าย 4284 บาท, คงเหลือ 45716 บาท`

	// 30 test cases - comprehensive
	tests := []TestCase{
		// === รายจ่าย (expense) ===
		{"กินข้าว 120", "new", "รายจ่ายพื้นฐาน"},
		{"กาแฟ 60 บาท", "new", "รายจ่ายมีหน่วย"},
		{"ค่าแท็กซี่ 150", "new", "รายจ่ายเดินทาง"},
		{"ซื้อหนังสือ 350", "new", "รายจ่ายซื้อของ"},
		{"จ่ายค่าเน็ต 599 บัตร KTC", "new", "รายจ่ายบัตรเครดิต"},
		{"ตัดผม 200 เงินสด", "new", "รายจ่ายระบุเงินสด"},
		{"ค่าประกันรถ 15000 กสิกร", "new", "รายจ่ายธนาคาร"},
		{"netflix 289", "new", "subscription"},
		{"grab 85", "new", "แอพเรียกรถ"},
		{"shopee 1500 บัตรกรุงเทพ", "new", "ซื้อของออนไลน์"},

		// === รายรับ (income) ===
		{"เงินเดือน 45000 เข้ากรุงไทย", "new", "รายรับเงินเดือน"},
		{"โบนัส 20000 เข้า SCB", "new", "รายรับโบนัส"},
		{"ได้เงินคืนภาษี 5000", "new", "รายรับคืนภาษี"},
		{"ขายของได้ 800", "new", "รายรับขายของ"},
		{"เพื่อนคืนเงิน 500", "new", "รายรับได้คืน"},

		// === โอน/ฝาก/ถอน (transfer) ===
		{"โอน 3000 จากกรุงไทยไปกสิกร", "transfer", "โอนระหว่างธนาคาร"},
		{"ฝากเงิน 5000 เข้ากรุงไทย", "transfer", "ฝากเงินสด"},
		{"ถอน 2000 จาก SCB", "transfer", "ถอนเงิน"},
		{"จ่ายบัตร KTC 5000 จากกสิกร", "transfer", "จ่ายบัตรเครดิต"},

		// === ดูยอด (balance) ===
		{"ยอดเงิน", "balance", "ดูยอดแบบสั้น"},
		{"เหลือเท่าไหร่", "balance", "ถามยอดคงเหลือ"},
		{"ดูยอดคงเหลือ", "balance", "ดูยอดแบบเต็ม"},

		// === ค้นหา (search) ===
		{"จ่ายค่าไฟไปเมื่อไหร่", "search", "ค้นหาค่าไฟ"},
		{"เคยซื้อเสื้อไหม", "search", "ค้นหาเสื้อ"},
		{"ดูประวัติ Netflix", "search", "ค้นหา subscription"},

		// === วิเคราะห์ (analyze) ===
		{"วันนี้จ่ายอะไรบ้าง", "analyze", "สรุปวันนี้"},
		{"สรุป 7 วัน", "analyze", "สรุปสัปดาห์"},
		{"ใช้จ่ายอะไรเยอะสุด", "analyze", "วิเคราะห์หมวด"},
		{"แนะนำการออม", "analyze", "ขอคำแนะนำ"},

		// === สนทนา (chat) ===
		{"สวัสดี", "chat", "ทักทาย"},
		{"ขอบคุณ", "chat", "ขอบคุณ"},
	}

	fmt.Println("=" + strings.Repeat("=", 89))
	fmt.Printf("ทดสอบ AI - %d คำถาม\n", len(tests))
	fmt.Println("=" + strings.Repeat("=", 89))

	ctx := context.Background()
	passed := 0
	failed := 0
	var failures []string

	for i, tc := range tests {
		resp, err := gemini.ChatWithContext(ctx, tc.Input, mockContext, "")
		if err != nil {
			fmt.Printf("[%2d] ❌ ERROR: %v\n", i+1, err)
			failed++
			failures = append(failures, fmt.Sprintf("%s: ERROR", tc.Input))
			continue
		}

		resp = cleanJSON(resp)
		var aiResp services.AIResponse
		if err := json.Unmarshal([]byte(resp), &aiResp); err != nil {
			fmt.Printf("[%2d] ❌ JSON Error: %s\n", i+1, tc.Input)
			failed++
			failures = append(failures, fmt.Sprintf("%s: JSON parse error", tc.Input))
			continue
		}

		status := "✅"
		if aiResp.Action != tc.ExpectedAction {
			status = "❌"
			failed++
			failures = append(failures, fmt.Sprintf("%s: expected %s, got %s", tc.Input, tc.ExpectedAction, aiResp.Action))
		} else {
			passed++
		}

		// Show details
		detail := ""
		switch aiResp.Action {
		case "new":
			if len(aiResp.Transactions) > 0 {
				tx := aiResp.Transactions[0]
				detail = fmt.Sprintf("%s %.0f฿ %s", tx.Type, tx.Amount, getCategoryOrBank(tx))
			}
		case "transfer":
			if aiResp.Transfer != nil && len(aiResp.Transfer.From) > 0 {
				detail = fmt.Sprintf("%.0f฿", aiResp.Transfer.From[0].Amount)
			}
		case "search":
			detail = fmt.Sprintf("query=%s", aiResp.SearchQuery)
		case "analyze":
			if aiResp.Analysis != nil {
				detail = aiResp.Analysis.Title
			}
		}

		fmt.Printf("[%2d] %s %-28s → %-8s %s\n", i+1, status, truncate(tc.Input, 26), aiResp.Action, truncate(detail, 30))
	}

	// Summary
	fmt.Println(strings.Repeat("=", 90))
	fmt.Printf("ผลรวม: ✅ %d/%d (%.0f%%)\n", passed, len(tests), float64(passed)/float64(len(tests))*100)

	if len(failures) > 0 {
		fmt.Println("\n❌ รายการที่ผิด:")
		for _, f := range failures {
			fmt.Printf("   - %s\n", f)
		}
	}
}

func getCategoryOrBank(tx services.TransactionData) string {
	if tx.BankName != "" {
		return "ธ." + tx.BankName
	}
	if tx.CreditCardName != "" {
		return "บัตร" + tx.CreditCardName
	}
	if tx.Category != "" {
		return tx.Category
	}
	return ""
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-2]) + ".."
}

func cleanJSON(text string) string {
	if len(text) > 7 && text[:7] == "```json" {
		text = text[7:]
	}
	if len(text) > 3 && text[:3] == "```" {
		text = text[3:]
	}
	if len(text) > 3 && text[len(text)-3:] == "```" {
		text = text[:len(text)-3]
	}
	start := 0
	end := len(text)
	for start < end && (text[start] == ' ' || text[start] == '\n' || text[start] == '\r' || text[start] == '\t') {
		start++
	}
	for end > start && (text[end-1] == ' ' || text[end-1] == '\n' || text[end-1] == '\r' || text[end-1] == '\t') {
		end--
	}
	return text[start:end]
}
