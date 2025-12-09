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

	mockContext := `รายการ 7 วันล่าสุด:
- 2024-12-09: รายจ่าย 150 บาท (ข้าวกะเพรา) เงินสด
- 2024-12-09: รายจ่าย 1500 บาท (เติมน้ำมัน) บัตรKTC
- 2024-12-08: รายรับ 50000 บาท (เงินเดือน) ธ.กรุงไทย
- 2024-12-07: รายจ่าย 200 บาท (ค่าไฟ) ธ.กรุงไทย
สรุป 7 วัน: รายรับ 50000 บาท, รายจ่าย 1850 บาท`

	// Edge cases - ยากๆ กวนๆ
	tests := []TestCase{
		// === กำกวม / สั้นมาก ===
		{"100", "new", "ตัวเลขอย่างเดียว - ควรถามกลับหรือ expense"},
		{"ร้อย", "chat", "คำไม่ชัดเจน"},
		{"อาหาร", "chat", "หมวดหมู่อย่างเดียว"},

		// === ภาษาพูด / สแลง ===
		{"กิน 200 โดนไป", "new", "ภาษาพูด"},
		{"จัดไป 500", "new", "สแลง จัดไป = จ่าย"},
		{"หมดไป 1000 กับค่ากิน", "new", "หมดไป = expense"},
		{"ได้มา 3000", "new", "ได้มา = income"},
		{"โดน 150", "new", "โดน = expense"},

		// === หลายรายการ ===
		{"กินข้าว 100 กาแฟ 50", "new", "2 รายการในประโยคเดียว"},
		{"ค่าน้ำ 200 ค่าไฟ 300", "new", "2 bills"},

		// === มีหน่วย / format แปลก ===
		{"1,500 บาท ค่าน้ำมัน", "new", "มี comma"},
		{"ค่ารถ 2.5k", "new", "ใช้ k แทนพัน"},
		{"กิน 1 ร้อย", "new", "เขียนเป็นคำ"},

		// === คำถามซับซ้อน ===
		{"เดือนนี้จ่ายไปเท่าไหร่", "analyze", "ถามยอดรวมเดือน"},
		{"เทียบกับเดือนก่อน", "analyze", "เปรียบเทียบ"},
		{"ทำไมเงินหมด", "analyze", "ถามเหตุผล"},
		{"จะเหลือเงินไหม", "analyze", "ถามอนาคต"},

		// === update / แก้ไข ===
		{"ไม่ใช่ 150 เป็น 200", "update", "แก้ยอดเงิน"},
		{"เปลี่ยนเป็นบัตร KTC", "update", "แก้ช่องทางจ่าย"},
		{"แก้เป็น 500", "update", "แก้ไขสั้นๆ"},

		// === ยกเลิก / ลบ ===
		{"ยกเลิกอันล่าสุด", "chat", "ยกเลิก (ไม่มี action delete)"},
		{"ลบรายการก่อนหน้า", "chat", "ลบ"},

		// === คำถามทั่วไป ===
		{"ช่วยอะไรได้บ้าง", "chat", "ถาม feature"},
		{"ใช้ยังไง", "chat", "ถามวิธีใช้"},
		{"เธอเป็นใคร", "chat", "ถามตัวตน"},

		// === คำแนะนำการเงิน (ใหม่) ===
		{"50/30/20 คืออะไร", "analyze", "ถามกฎ 50/30/20"},
		{"เงินพอใช้ไหมเดือนนี้", "analyze", "ถามกระแสเงินสด"},
		{"ควรออมเท่าไหร่", "analyze", "ถามการออม"},
		{"แนะนำวิธีลดรายจ่าย", "analyze", "ขอคำแนะนำลดรายจ่าย"},
		{"ดูงบประมาณ", "analyze", "ดูงบประมาณที่ตั้งไว้"},

		// === ตั้งงบประมาณ (budget) ===
		{"ตั้งงบอาหาร 5000", "budget", "ตั้งงบหมวดอาหาร"},
		{"งบเดินทาง 3000 บาท", "budget", "ตั้งงบหมวดเดินทาง"},
		{"ตั้งงบช้อปปิ้ง 2000", "budget", "ตั้งงบหมวดช้อปปิ้ง"},

		// === Export (export) ===
		{"ส่ง Excel", "export", "ส่งออก Excel"},
		{"export excel", "export", "export ภาษาอังกฤษ"},
		{"ส่งไฟล์ Excel 7 วัน", "export", "ส่งออก Excel 7 วัน"},
		{"สร้าง PDF", "export", "สร้าง PDF"},
		{"ส่งรายงาน PDF", "export", "ส่งรายงาน PDF"},
		{"ดาวน์โหลดรายงาน", "export", "ดาวน์โหลด"},

		// === Chart (chart) ===
		{"ดูกราฟ", "chart", "ดูกราฟ"},
		{"แสดงกราฟ", "chart", "แสดงกราฟ"},
		{"ดูแผนภูมิรายจ่าย", "chart", "ดูแผนภูมิ"},
		{"สัดส่วนการใช้จ่าย", "chart", "สัดส่วน"},
	}

	fmt.Println("=" + strings.Repeat("=", 89))
	fmt.Printf("ทดสอบ Edge Cases - %d คำถาม\n", len(tests))
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
			continue
		}

		resp = cleanJSON(resp)
		var aiResp services.AIResponse
		if err := json.Unmarshal([]byte(resp), &aiResp); err != nil {
			fmt.Printf("[%2d] ❌ JSON Error | %s\n", i+1, tc.Input)
			failed++
			failures = append(failures, fmt.Sprintf("%s → JSON error", tc.Input))
			continue
		}

		status := "✅"
		if aiResp.Action != tc.ExpectedAction {
			status = "⚠️"
			failed++
			failures = append(failures, fmt.Sprintf("%s → expected '%s', got '%s'", tc.Input, tc.ExpectedAction, aiResp.Action))
		} else {
			passed++
		}

		detail := getDetail(aiResp)
		fmt.Printf("[%2d] %s %-26s → %-8s | %s\n", i+1, status, truncate(tc.Input, 24), aiResp.Action, truncate(detail, 35))
	}

	fmt.Println(strings.Repeat("=", 90))
	fmt.Printf("ผลรวม: ✅ %d/%d (%.0f%%)\n", passed, len(tests), float64(passed)/float64(len(tests))*100)

	if len(failures) > 0 {
		fmt.Println("\n⚠️ รายการที่ไม่ตรง (อาจไม่ใช่ error จริง):")
		for _, f := range failures {
			fmt.Printf("   - %s\n", f)
		}
	}
}

func getDetail(aiResp services.AIResponse) string {
	switch aiResp.Action {
	case "new":
		if len(aiResp.Transactions) > 0 {
			tx := aiResp.Transactions[0]
			extra := ""
			if len(aiResp.Transactions) > 1 {
				extra = fmt.Sprintf(" (+%d)", len(aiResp.Transactions)-1)
			}
			return fmt.Sprintf("%s %.0f฿%s", tx.Type, tx.Amount, extra)
		}
	case "transfer":
		if aiResp.Transfer != nil && len(aiResp.Transfer.From) > 0 {
			return fmt.Sprintf("%.0f฿", aiResp.Transfer.From[0].Amount)
		}
	case "search":
		return "query=" + aiResp.SearchQuery
	case "analyze":
		if aiResp.Analysis != nil {
			return aiResp.Analysis.Title
		}
	case "update":
		return fmt.Sprintf("%s=%v", aiResp.UpdateField, aiResp.UpdateValue)
	case "export":
		if aiResp.Export != nil {
			return fmt.Sprintf("format=%s days=%d", aiResp.Export.Format, aiResp.Export.Days)
		}
	case "chart":
		return "แสดงกราฟ"
	case "budget":
		if aiResp.Budget != nil {
			return fmt.Sprintf("%s %.0f฿", aiResp.Budget.Category, aiResp.Budget.Amount)
		}
	case "chat":
		msg := aiResp.Message
		if len([]rune(msg)) > 30 {
			msg = string([]rune(msg)[:30]) + "..."
		}
		return msg
	}
	return aiResp.Message
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
