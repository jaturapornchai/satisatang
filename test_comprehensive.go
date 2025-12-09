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
	Category       string
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
- 2024-12-09: รายจ่าย 150 บาท (ข้าวกะเพรา) เงินสด หมวดหมู่: อาหาร
- 2024-12-09: รายจ่าย 1500 บาท (เติมน้ำมัน) บัตรKTC หมวดหมู่: เดินทาง
- 2024-12-08: รายรับ 50000 บาท (เงินเดือน) ธ.กรุงไทย
- 2024-12-08: รายจ่าย 350 บาท (ซื้อของ Lotus) ธ.กสิกร หมวดหมู่: ช้อปปิ้ง
- 2024-12-07: รายจ่าย 200 บาท (ค่าไฟ) ธ.กรุงไทย หมวดหมู่: บิล
- 2024-12-06: รายจ่าย 89 บาท (Netflix) บัตรกรุงเทพ หมวดหมู่: บันเทิง
สรุป 7 วัน: รายรับ 50000 บาท, รายจ่าย 2289 บาท

งบประมาณที่ตั้งไว้:
- อาหาร: 5000 บาท/เดือน (ใช้ไป 2100 บาท = 42%)
- เดินทาง: 3000 บาท/เดือน (ใช้ไป 1500 บาท = 50%)
- ช้อปปิ้ง: 2000 บาท/เดือน (ใช้ไป 350 บาท = 17.5%)`

	tests := []TestCase{
		// === รายจ่าย (Expense) - 15 cases ===
		{"กินข้าว 100", "new", "รายจ่ายพื้นฐาน", "expense"},
		{"กาแฟ 60 บาท", "new", "รายจ่ายมีหน่วย", "expense"},
		{"ค่าแท็กซี่ 150 เงินสด", "new", "รายจ่ายเงินสด", "expense"},
		{"ซื้อหนังสือ 350 บัตร KTC", "new", "รายจ่ายบัตรเครดิต", "expense"},
		{"ค่าเน็ต 599 กสิกร", "new", "รายจ่ายธนาคาร", "expense"},
		{"grab 85", "new", "รายจ่ายสั้น", "expense"},
		{"netflix 289", "new", "subscription", "expense"},
		{"จ่ายค่าน้ำ 200", "new", "ค่าน้ำ", "expense"},
		{"ค่าประกันรถ 15000", "new", "รายจ่ายใหญ่", "expense"},
		{"จัดไป 500", "new", "ภาษาพูด จัดไป", "expense"},
		{"โดน 200", "new", "ภาษาพูด โดน", "expense"},
		{"หมดไป 1000 ค่ากิน", "new", "ภาษาพูด หมดไป", "expense"},
		{"กินข้าว 100 กาแฟ 50", "new", "หลายรายการ", "expense"},
		{"1,500 บาท ค่าน้ำมัน", "new", "มี comma", "expense"},
		{"ค่ารถ 2.5k", "new", "ใช้ k", "expense"},

		// === รายรับ (Income) - 8 cases ===
		{"เงินเดือน 45000", "new", "เงินเดือน", "income"},
		{"โบนัส 20000 เข้า SCB", "new", "โบนัส", "income"},
		{"ได้เงินคืนภาษี 5000", "new", "คืนภาษี", "income"},
		{"ขายของได้ 800", "new", "ขายของ", "income"},
		{"เพื่อนคืนเงิน 500", "new", "ได้คืน", "income"},
		{"ได้มา 3000", "new", "ภาษาพูด ได้มา", "income"},
		{"รับเงิน 1000", "new", "รับเงิน", "income"},
		{"เงินปันผล 2500", "new", "ปันผล", "income"},

		// === โอน/ฝาก/ถอน (Transfer) - 6 cases ===
		{"โอน 3000 จากกรุงไทยไปกสิกร", "transfer", "โอนระหว่างธนาคาร", "transfer"},
		{"ฝากเงิน 5000 เข้ากรุงไทย", "transfer", "ฝากเงิน", "transfer"},
		{"ถอน 2000 จาก SCB", "transfer", "ถอนเงิน", "transfer"},
		{"จ่ายบัตร KTC 5000 จากกสิกร", "transfer", "จ่ายบัตรเครดิต", "transfer"},
		{"โอนเข้าบัญชีออม 10000", "transfer", "โอนเข้าออม", "transfer"},
		{"ย้ายเงิน 5000 จากออมเข้ากระแส", "transfer", "ย้ายเงิน", "transfer"},

		// === ดูยอด (Balance) - 4 cases ===
		{"ยอดเงิน", "balance", "ดูยอดสั้น", "balance"},
		{"ดูยอดคงเหลือ", "balance", "ดูยอดเต็ม", "balance"},
		{"เงินในบัญชี", "balance", "ถามเงินในบัญชี", "balance"},
		{"มีเงินเท่าไหร่", "balance", "ถามยอดแบบพูด", "balance"},

		// === ค้นหา (Search) - 5 cases ===
		{"จ่ายค่าไฟไปเมื่อไหร่", "search", "ค้นหาค่าไฟ", "search"},
		{"เคยซื้อเสื้อไหม", "search", "ค้นหาเสื้อ", "search"},
		{"ดูประวัติ Netflix", "search", "ค้นหา Netflix", "search"},
		{"หารายการอาหาร", "search", "หารายการ", "search"},
		{"ค้นหาค่าเดินทาง", "search", "ค้นหาหมวดหมู่", "search"},

		// === วิเคราะห์ (Analyze) - 12 cases ===
		{"วันนี้จ่ายอะไรบ้าง", "analyze", "สรุปวันนี้", "analyze"},
		{"สรุป 7 วัน", "analyze", "สรุปสัปดาห์", "analyze"},
		{"ใช้จ่ายอะไรเยอะสุด", "analyze", "วิเคราะห์หมวด", "analyze"},
		{"แนะนำการออม", "analyze", "คำแนะนำออม", "analyze"},
		{"เดือนนี้จ่ายไปเท่าไหร่", "analyze", "สรุปเดือน", "analyze"},
		{"เทียบกับเดือนก่อน", "analyze", "เปรียบเทียบ", "analyze"},
		{"ทำไมเงินหมด", "analyze", "ถามเหตุผล", "analyze"},
		{"จะเหลือเงินไหม", "analyze", "ถามอนาคต", "analyze"},
		{"50/30/20 คืออะไร", "analyze", "กฎ 50/30/20", "analyze"},
		{"เงินพอใช้ไหมเดือนนี้", "analyze", "สถานะการเงิน", "analyze"},
		{"ควรออมเท่าไหร่", "analyze", "ถามการออม", "analyze"},
		{"แนะนำวิธีลดรายจ่าย", "analyze", "ขอคำแนะนำ", "analyze"},

		// === งบประมาณ (Budget) - 6 cases ===
		{"ตั้งงบอาหาร 5000", "budget", "ตั้งงบอาหาร", "budget"},
		{"งบเดินทาง 3000 บาท", "budget", "ตั้งงบเดินทาง", "budget"},
		{"ตั้งงบช้อปปิ้ง 2000", "budget", "ตั้งงบช้อปปิ้ง", "budget"},
		{"เพิ่มงบบันเทิง 1500", "budget", "เพิ่มงบ", "budget"},
		{"แก้งบอาหารเป็น 6000", "budget", "แก้งบ", "budget"},
		{"ตั้งงบค่าน้ำค่าไฟ 2000", "budget", "งบค่าสาธารณูปโภค", "budget"},

		// === Export - 8 cases ===
		{"ส่ง Excel", "export", "ส่งออก Excel", "export"},
		{"export excel", "export", "export ภาษาอังกฤษ", "export"},
		{"ส่งไฟล์ Excel 30 วัน", "export", "Excel 30 วัน", "export"},
		{"สร้าง PDF", "export", "สร้าง PDF", "export"},
		{"ส่งรายงาน PDF", "export", "ส่ง PDF", "export"},
		{"ดาวน์โหลดรายงาน", "export", "ดาวน์โหลด", "export"},
		{"ขอไฟล์ Excel", "export", "ขอไฟล์", "export"},
		{"export รายงานเดือนนี้", "export", "export รายงาน", "export"},

		// === Chart - 6 cases ===
		{"ดูกราฟ", "chart", "ดูกราฟ", "chart"},
		{"แสดงกราฟ", "chart", "แสดงกราฟ", "chart"},
		{"ดูแผนภูมิรายจ่าย", "chart", "ดูแผนภูมิ", "chart"},
		{"สัดส่วนการใช้จ่าย", "chart", "สัดส่วน", "chart"},
		{"กราฟรายจ่ายตามหมวด", "chart", "กราฟตามหมวด", "chart"},
		{"chart", "chart", "chart ภาษาอังกฤษ", "chart"},

		// === แก้ไข (Update) - 5 cases ===
		{"ไม่ใช่ 150 เป็น 200", "update", "แก้ยอด", "update"},
		{"เปลี่ยนเป็นบัตร KTC", "update", "แก้ช่องทาง", "update"},
		{"แก้เป็น 500", "update", "แก้สั้น", "update"},
		{"อันล่าสุดเปลี่ยนเป็น 300", "update", "แก้อันล่าสุด", "update"},
		{"แก้หมวดเป็นอาหาร", "update", "แก้หมวดหมู่", "update"},

		// === สนทนา (Chat) - 6 cases ===
		{"สวัสดี", "chat", "ทักทาย", "chat"},
		{"ขอบคุณ", "chat", "ขอบคุณ", "chat"},
		{"ช่วยอะไรได้บ้าง", "chat", "ถาม feature", "chat"},
		{"เธอเป็นใคร", "chat", "ถามตัวตน", "chat"},
		{"555", "chat", "หัวเราะ", "chat"},
		{"โอเค", "chat", "รับทราบ", "chat"},
	}

	fmt.Println("=" + strings.Repeat("=", 99))
	fmt.Printf("🧪 ทดสอบระบบครบทุก Feature - %d คำถาม\n", len(tests))
	fmt.Println("=" + strings.Repeat("=", 99))

	ctx := context.Background()

	// Track results by category
	categoryResults := make(map[string]struct {
		passed int
		failed int
	})

	var failures []string
	total := 0
	passed := 0

	for i, tc := range tests {
		total++

		resp, err := gemini.ChatWithContext(ctx, tc.Input, mockContext, "")
		if err != nil {
			fmt.Printf("[%2d] ❌ ERROR: %v\n", i+1, err)
			cat := categoryResults[tc.Category]
			cat.failed++
			categoryResults[tc.Category] = cat
			failures = append(failures, fmt.Sprintf("[%s] %s: API ERROR", tc.Category, tc.Input))
			continue
		}

		resp = cleanJSON(resp)
		var aiResp services.AIResponse
		if err := json.Unmarshal([]byte(resp), &aiResp); err != nil {
			fmt.Printf("[%2d] ❌ JSON Error | %s\n", i+1, tc.Input)
			cat := categoryResults[tc.Category]
			cat.failed++
			categoryResults[tc.Category] = cat
			failures = append(failures, fmt.Sprintf("[%s] %s: JSON parse error", tc.Category, tc.Input))
			continue
		}

		status := "✅"
		cat := categoryResults[tc.Category]
		if aiResp.Action != tc.ExpectedAction {
			status = "❌"
			cat.failed++
			failures = append(failures, fmt.Sprintf("[%s] %s: expected '%s', got '%s'", tc.Category, tc.Input, tc.ExpectedAction, aiResp.Action))
		} else {
			status = "✅"
			cat.passed++
			passed++
		}
		categoryResults[tc.Category] = cat

		detail := getDetail(aiResp)
		fmt.Printf("[%2d] %s %-26s → %-8s | %s\n", i+1, status, truncate(tc.Input, 24), aiResp.Action, truncate(detail, 40))
	}

	// Summary
	fmt.Println(strings.Repeat("=", 100))
	fmt.Printf("\n📊 สรุปผลตาม Category:\n")
	fmt.Println(strings.Repeat("-", 50))

	categories := []string{"expense", "income", "transfer", "balance", "search", "analyze", "budget", "export", "chart", "update", "chat"}
	for _, cat := range categories {
		result := categoryResults[cat]
		total := result.passed + result.failed
		if total > 0 {
			pct := float64(result.passed) / float64(total) * 100
			icon := "✅"
			if pct < 80 {
				icon = "⚠️"
			}
			if pct < 50 {
				icon = "❌"
			}
			fmt.Printf("%s %-12s: %2d/%2d (%.0f%%)\n", icon, cat, result.passed, total, pct)
		}
	}

	fmt.Println(strings.Repeat("-", 50))
	pct := float64(passed) / float64(total) * 100
	fmt.Printf("📈 รวมทั้งหมด: %d/%d (%.1f%%)\n", passed, total, pct)

	if len(failures) > 0 {
		fmt.Printf("\n❌ รายการที่ไม่ผ่าน (%d รายการ):\n", len(failures))
		for _, f := range failures {
			fmt.Printf("   - %s\n", f)
		}
	}

	// Grade
	fmt.Println()
	if pct >= 95 {
		fmt.Println("🏆 เกรด: A (ยอดเยี่ยม!)")
	} else if pct >= 90 {
		fmt.Println("🥈 เกรด: B+ (ดีมาก)")
	} else if pct >= 85 {
		fmt.Println("🥉 เกรด: B (ดี)")
	} else if pct >= 80 {
		fmt.Println("📝 เกรด: C+ (ผ่าน)")
	} else {
		fmt.Println("⚠️ เกรด: C (ต้องปรับปรุง)")
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
			return fmt.Sprintf("%s %.0f฿ %s%s", tx.Type, tx.Amount, tx.Category, extra)
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
		return "แสดงกราฟสัดส่วนรายจ่าย"
	case "budget":
		if aiResp.Budget != nil {
			return fmt.Sprintf("%s %.0f฿", aiResp.Budget.Category, aiResp.Budget.Amount)
		}
	case "chat":
		msg := aiResp.Message
		if len([]rune(msg)) > 35 {
			msg = string([]rune(msg)[:35]) + "..."
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
