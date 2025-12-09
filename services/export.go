package services

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/signintech/gopdf"
	"github.com/xuri/excelize/v2"
)

// ExportService handles Excel and PDF export
type ExportService struct {
	mongo *MongoDBService
}

// NewExportService creates a new export service
func NewExportService(mongo *MongoDBService) *ExportService {
	return &ExportService{mongo: mongo}
}

// สีสันแบบวัยรุ่น - Gradient Palette
var (
	// Primary Colors
	colorPrimary   = "#6C5CE7" // Purple
	colorSecondary = "#00CEC9" // Teal
	colorAccent    = "#FF7675" // Coral
	colorSuccess   = "#00B894" // Green
	colorWarning   = "#FDCB6E" // Yellow
	colorDanger    = "#D63031" // Red

	// Pastel for categories
	categoryColors = []string{
		"#A29BFE", // Light Purple
		"#74B9FF", // Light Blue
		"#81ECEC", // Light Teal
		"#FFEAA7", // Light Yellow
		"#FAB1A0", // Light Coral
		"#DFE6E9", // Light Gray
		"#55EFC4", // Mint
		"#FD79A8", // Pink
		"#E17055", // Orange
		"#00CEC9", // Cyan
	}
)

// ExportToExcel generates Excel file for user's transactions - สไตล์วัยรุ่น
func (s *ExportService) ExportToExcel(ctx context.Context, lineID string, days int) ([]byte, string, error) {
	if days <= 0 {
		days = 30
	}

	// Get date range
	endDate := time.Now()
	startDate := endDate.AddDate(0, 0, -days)

	// Get transactions
	results, err := s.mongo.SearchByDateRange(ctx, lineID, startDate.Format("2006-01-02"), endDate.Format("2006-01-02"), 1000)
	if err != nil {
		return nil, "", fmt.Errorf("ไม่สามารถดึงข้อมูลได้: %w", err)
	}

	// Create Excel file
	f := excelize.NewFile()
	defer f.Close()

	// ===== Sheet 1: รายการทั้งหมด =====
	sheetName := "รายการทั้งหมด"
	f.SetSheetName("Sheet1", sheetName)

	// Title row with gradient effect
	titleStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Bold:   true,
			Size:   18,
			Color:  "#FFFFFF",
			Family: "Tahoma",
		},
		Fill: excelize.Fill{
			Type:    "gradient",
			Color:   []string{colorPrimary, colorSecondary},
			Shading: 1,
		},
		Alignment: &excelize.Alignment{
			Horizontal: "center",
			Vertical:   "center",
		},
	})
	f.MergeCell(sheetName, "A1", "F1")
	f.SetCellValue(sheetName, "A1", fmt.Sprintf("📊 สติสตางค์ - รายงาน %d วัน", days))
	f.SetCellStyle(sheetName, "A1", "F1", titleStyle)
	f.SetRowHeight(sheetName, 1, 35)

	// Subtitle with date range
	subtitleStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Size:   11,
			Color:  "#636E72",
			Italic: true,
		},
		Alignment: &excelize.Alignment{
			Horizontal: "center",
		},
	})
	f.MergeCell(sheetName, "A2", "F2")
	f.SetCellValue(sheetName, "A2", fmt.Sprintf("วันที่ %s ถึง %s", startDate.Format("02/01/2006"), endDate.Format("02/01/2006")))
	f.SetCellStyle(sheetName, "A2", "F2", subtitleStyle)
	f.SetRowHeight(sheetName, 2, 20)

	// Headers - Row 3
	headers := []string{"📅 วันที่", "💰 ประเภท", "🏷️ หมวดหมู่", "📝 รายละเอียด", "💵 จำนวน (บาท)", "🏦 ช่องทาง"}
	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Bold:  true,
			Size:  11,
			Color: "#FFFFFF",
		},
		Fill: excelize.Fill{
			Type:    "pattern",
			Color:   []string{colorPrimary},
			Pattern: 1,
		},
		Alignment: &excelize.Alignment{
			Horizontal: "center",
			Vertical:   "center",
		},
		Border: []excelize.Border{
			{Type: "bottom", Color: colorSecondary, Style: 2},
		},
	})
	for i, header := range headers {
		cell := fmt.Sprintf("%c3", 'A'+i)
		f.SetCellValue(sheetName, cell, header)
	}
	f.SetCellStyle(sheetName, "A3", "F3", headerStyle)
	f.SetRowHeight(sheetName, 3, 25)

	// Data styles
	incomeStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 10, Color: "#00B894"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#D4EFDF"}, Pattern: 1},
		Alignment: &excelize.Alignment{Vertical: "center"},
	})
	expenseStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 10, Color: "#D63031"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#FADBD8"}, Pattern: 1},
		Alignment: &excelize.Alignment{Vertical: "center"},
	})
	numberStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 10, Bold: true},
		Alignment: &excelize.Alignment{Horizontal: "right", Vertical: "center"},
		NumFmt:    4, // #,##0.00
	})

	// Add data (excluding transfers)
	var totalIncome, totalExpense float64
	row := 4
	for _, result := range results {
		tx := result.Transaction

		// Skip transfer transactions
		if tx.Category == "โอนเงิน" {
			continue
		}

		// Type
		txType := "💸 รายจ่าย"
		rowStyle := expenseStyle
		if tx.Type == 1 {
			txType = "💚 รายรับ"
			rowStyle = incomeStyle
			totalIncome += tx.Amount
		} else {
			totalExpense += tx.Amount
		}

		// Payment method
		payment := getPaymentInfo(tx.UseType, tx.BankName, tx.CreditCardName)

		// Description
		desc := tx.Description
		if desc == "" {
			desc = tx.CustName
		}

		f.SetCellValue(sheetName, fmt.Sprintf("A%d", row), result.Date)
		f.SetCellValue(sheetName, fmt.Sprintf("B%d", row), txType)
		f.SetCellValue(sheetName, fmt.Sprintf("C%d", row), tx.Category)
		f.SetCellValue(sheetName, fmt.Sprintf("D%d", row), desc)
		f.SetCellValue(sheetName, fmt.Sprintf("E%d", row), tx.Amount)
		f.SetCellValue(sheetName, fmt.Sprintf("F%d", row), payment)

		f.SetCellStyle(sheetName, fmt.Sprintf("A%d", row), fmt.Sprintf("D%d", row), rowStyle)
		f.SetCellStyle(sheetName, fmt.Sprintf("E%d", row), fmt.Sprintf("E%d", row), numberStyle)
		f.SetCellStyle(sheetName, fmt.Sprintf("F%d", row), fmt.Sprintf("F%d", row), rowStyle)
		row++
	}

	// Summary section
	summaryStartRow := row + 1

	// Summary title
	summaryTitleStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 12, Color: "#FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{colorSecondary}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center"},
	})
	f.MergeCell(sheetName, fmt.Sprintf("D%d", summaryStartRow), fmt.Sprintf("E%d", summaryStartRow))
	f.SetCellValue(sheetName, fmt.Sprintf("D%d", summaryStartRow), "📊 สรุปยอด")
	f.SetCellStyle(sheetName, fmt.Sprintf("D%d", summaryStartRow), fmt.Sprintf("E%d", summaryStartRow), summaryTitleStyle)

	// Summary values
	summaryLabelStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 11},
		Alignment: &excelize.Alignment{Horizontal: "right"},
	})
	incomeValueStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 11, Color: "#00B894"},
		NumFmt:    4,
		Alignment: &excelize.Alignment{Horizontal: "right"},
	})
	expenseValueStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 11, Color: "#D63031"},
		NumFmt:    4,
		Alignment: &excelize.Alignment{Horizontal: "right"},
	})
	balanceStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 12, Color: colorPrimary},
		NumFmt:    4,
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#E8DAEF"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "right"},
	})

	f.SetCellValue(sheetName, fmt.Sprintf("D%d", summaryStartRow+1), "💚 รวมรายรับ:")
	f.SetCellValue(sheetName, fmt.Sprintf("E%d", summaryStartRow+1), totalIncome)
	f.SetCellStyle(sheetName, fmt.Sprintf("D%d", summaryStartRow+1), fmt.Sprintf("D%d", summaryStartRow+1), summaryLabelStyle)
	f.SetCellStyle(sheetName, fmt.Sprintf("E%d", summaryStartRow+1), fmt.Sprintf("E%d", summaryStartRow+1), incomeValueStyle)

	f.SetCellValue(sheetName, fmt.Sprintf("D%d", summaryStartRow+2), "💸 รวมรายจ่าย:")
	f.SetCellValue(sheetName, fmt.Sprintf("E%d", summaryStartRow+2), totalExpense)
	f.SetCellStyle(sheetName, fmt.Sprintf("D%d", summaryStartRow+2), fmt.Sprintf("D%d", summaryStartRow+2), summaryLabelStyle)
	f.SetCellStyle(sheetName, fmt.Sprintf("E%d", summaryStartRow+2), fmt.Sprintf("E%d", summaryStartRow+2), expenseValueStyle)

	f.SetCellValue(sheetName, fmt.Sprintf("D%d", summaryStartRow+3), "💰 คงเหลือ:")
	f.SetCellValue(sheetName, fmt.Sprintf("E%d", summaryStartRow+3), totalIncome-totalExpense)
	f.SetCellStyle(sheetName, fmt.Sprintf("D%d", summaryStartRow+3), fmt.Sprintf("D%d", summaryStartRow+3), summaryLabelStyle)
	f.SetCellStyle(sheetName, fmt.Sprintf("E%d", summaryStartRow+3), fmt.Sprintf("E%d", summaryStartRow+3), balanceStyle)

	// Set column widths
	f.SetColWidth(sheetName, "A", "A", 14)
	f.SetColWidth(sheetName, "B", "B", 14)
	f.SetColWidth(sheetName, "C", "C", 16)
	f.SetColWidth(sheetName, "D", "D", 28)
	f.SetColWidth(sheetName, "E", "E", 16)
	f.SetColWidth(sheetName, "F", "F", 18)

	// ===== Sheet 2: สรุปหมวดหมู่ =====
	summarySheet := "สรุปหมวดหมู่"
	f.NewSheet(summarySheet)

	// Title
	f.MergeCell(summarySheet, "A1", "D1")
	f.SetCellValue(summarySheet, "A1", "🏷️ สรุปรายจ่ายตามหมวดหมู่")
	f.SetCellStyle(summarySheet, "A1", "D1", titleStyle)
	f.SetRowHeight(summarySheet, 1, 35)

	// Get spending by category
	spending, _ := s.mongo.GetMonthlySpendingByCategory(ctx, lineID)

	// Sort by amount (highest first)
	type catSpend struct {
		Category string
		Amount   float64
	}
	var sortedSpending []catSpend
	for cat, amt := range spending {
		sortedSpending = append(sortedSpending, catSpend{cat, amt})
	}
	sort.Slice(sortedSpending, func(i, j int) bool {
		return sortedSpending[i].Amount > sortedSpending[j].Amount
	})

	// Headers
	catHeaders := []string{"🏆 อันดับ", "🏷️ หมวดหมู่", "💵 จำนวนเงิน", "📊 สัดส่วน"}
	for i, header := range catHeaders {
		cell := fmt.Sprintf("%c2", 'A'+i)
		f.SetCellValue(summarySheet, cell, header)
	}
	f.SetCellStyle(summarySheet, "A2", "D2", headerStyle)
	f.SetRowHeight(summarySheet, 2, 25)

	row = 3
	for i, cs := range sortedSpending {
		percentage := 0.0
		if totalExpense > 0 {
			percentage = (cs.Amount / totalExpense) * 100
		}

		// Rank emoji
		rankEmoji := fmt.Sprintf("%d.", i+1)
		if i == 0 {
			rankEmoji = "🥇"
		} else if i == 1 {
			rankEmoji = "🥈"
		} else if i == 2 {
			rankEmoji = "🥉"
		}

		// Color based on rank
		catStyle, _ := f.NewStyle(&excelize.Style{
			Font:      &excelize.Font{Size: 11},
			Fill:      excelize.Fill{Type: "pattern", Color: []string{categoryColors[i%len(categoryColors)]}, Pattern: 1},
			Alignment: &excelize.Alignment{Vertical: "center"},
		})

		f.SetCellValue(summarySheet, fmt.Sprintf("A%d", row), rankEmoji)
		f.SetCellValue(summarySheet, fmt.Sprintf("B%d", row), cs.Category)
		f.SetCellValue(summarySheet, fmt.Sprintf("C%d", row), cs.Amount)
		f.SetCellValue(summarySheet, fmt.Sprintf("D%d", row), fmt.Sprintf("%.1f%%", percentage))
		f.SetCellStyle(summarySheet, fmt.Sprintf("A%d", row), fmt.Sprintf("D%d", row), catStyle)
		row++
	}

	// Total row
	totalStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 12, Color: "#FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{colorPrimary}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center"},
	})
	f.SetCellValue(summarySheet, fmt.Sprintf("A%d", row), "")
	f.SetCellValue(summarySheet, fmt.Sprintf("B%d", row), "รวมทั้งหมด")
	f.SetCellValue(summarySheet, fmt.Sprintf("C%d", row), totalExpense)
	f.SetCellValue(summarySheet, fmt.Sprintf("D%d", row), "100%")
	f.SetCellStyle(summarySheet, fmt.Sprintf("A%d", row), fmt.Sprintf("D%d", row), totalStyle)

	f.SetColWidth(summarySheet, "A", "A", 10)
	f.SetColWidth(summarySheet, "B", "B", 20)
	f.SetColWidth(summarySheet, "C", "C", 16)
	f.SetColWidth(summarySheet, "D", "D", 12)

	// Set active sheet to first
	f.SetActiveSheet(0)

	// Write to buffer
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, "", fmt.Errorf("cannot create Excel: %w", err)
	}

	// Generate unique filename with random numbers only
	randomNum := fmt.Sprintf("%d%d", time.Now().UnixNano(), time.Now().UnixMicro()%10000)
	filename := fmt.Sprintf("%s.xlsx", randomNum)
	return buf.Bytes(), filename, nil
}

// ExportToPDF generates PDF report with Thai font support using gopdf
func (s *ExportService) ExportToPDF(ctx context.Context, lineID string, days int) ([]byte, string, error) {
	if days <= 0 {
		days = 30
	}

	// Get balance summary
	balance, err := s.mongo.GetBalanceSummary(ctx, lineID)
	if err != nil {
		return nil, "", fmt.Errorf("ไม่สามารถดึงข้อมูลยอดคงเหลือ: %w", err)
	}

	// Get spending by category
	spending, _ := s.mongo.GetMonthlySpendingByCategory(ctx, lineID)

	// Get budget status
	budgetStatus, _ := s.mongo.GetBudgetStatus(ctx, lineID)

	// Create PDF with gopdf
	pdf := gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})

	// Add Thai font from embedded bytes
	if err := pdf.AddTTFFontData("Sarabun", SarabunRegular); err != nil {
		return nil, "", fmt.Errorf("ไม่สามารถโหลดฟอนต์: %w", err)
	}
	if err := pdf.AddTTFFontData("SarabunBold", SarabunBold); err != nil {
		return nil, "", fmt.Errorf("ไม่สามารถโหลดฟอนต์ตัวหนา: %w", err)
	}

	pdf.AddPage()

	// Background header
	pdf.SetFillColor(108, 92, 231) // Primary purple
	pdf.RectFromUpperLeftWithStyle(0, 0, 595, 120, "F")

	// Title
	pdf.SetTextColor(255, 255, 255)
	pdf.SetFont("SarabunBold", "", 28)
	pdf.SetX(40)
	pdf.SetY(35)
	pdf.Cell(nil, "สติสตางค์")

	pdf.SetFont("Sarabun", "", 16)
	pdf.SetX(40)
	pdf.SetY(70)
	pdf.Cell(nil, "รายงานสรุปการเงินส่วนตัว")

	pdf.SetFont("Sarabun", "", 12)
	pdf.SetX(40)
	pdf.SetY(95)
	pdf.Cell(nil, fmt.Sprintf("วันที่: %s", time.Now().Format("02/01/2006")))

	// Summary Box
	pdf.SetFillColor(245, 247, 250)
	pdf.RectFromUpperLeftWithStyle(30, 135, 535, 100, "F")

	pdf.SetTextColor(45, 52, 54)
	pdf.SetFont("SarabunBold", "", 18)
	pdf.SetX(50)
	pdf.SetY(150)
	pdf.Cell(nil, "สรุปยอด")

	// Income
	pdf.SetFont("Sarabun", "", 14)
	pdf.SetX(50)
	pdf.SetY(180)
	pdf.SetTextColor(0, 184, 148)
	pdf.Cell(nil, "รายรับทั้งหมด:")
	pdf.SetFont("SarabunBold", "", 14)
	pdf.SetX(180)
	pdf.Cell(nil, fmt.Sprintf("%.2f บาท", balance.TotalIncome))

	// Expense
	pdf.SetFont("Sarabun", "", 14)
	pdf.SetX(300)
	pdf.SetY(180)
	pdf.SetTextColor(214, 48, 49)
	pdf.Cell(nil, "รายจ่ายทั้งหมด:")
	pdf.SetFont("SarabunBold", "", 14)
	pdf.SetX(420)
	pdf.Cell(nil, fmt.Sprintf("%.2f บาท", balance.TotalExpense))

	// Balance
	pdf.SetFont("Sarabun", "", 14)
	pdf.SetX(50)
	pdf.SetY(210)
	pdf.SetTextColor(108, 92, 231)
	pdf.Cell(nil, "ยอดคงเหลือ:")
	pdf.SetFont("SarabunBold", "", 16)
	pdf.SetX(180)
	pdf.Cell(nil, fmt.Sprintf("%.2f บาท", balance.Balance))

	// Category section
	yPos := 260.0
	pdf.SetTextColor(45, 52, 54)

	if len(spending) > 0 {
		// Sort spending
		type catSpend struct {
			Category string
			Amount   float64
		}
		var sortedSpending []catSpend
		for cat, amt := range spending {
			sortedSpending = append(sortedSpending, catSpend{cat, amt})
		}
		sort.Slice(sortedSpending, func(i, j int) bool {
			return sortedSpending[i].Amount > sortedSpending[j].Amount
		})

		pdf.SetFont("SarabunBold", "", 16)
		pdf.SetX(30)
		pdf.SetY(yPos)
		pdf.Cell(nil, "รายจ่ายแยกตามหมวดหมู่")
		yPos += 30

		// Category bars
		colors := [][]uint8{
			{162, 155, 254}, // Light Purple
			{116, 185, 255}, // Light Blue
			{129, 236, 236}, // Light Teal
			{255, 234, 167}, // Light Yellow
			{250, 177, 160}, // Light Coral
		}

		pdf.SetFont("Sarabun", "", 12)
		maxWidth := 250.0
		for i, cs := range sortedSpending {
			if i >= 8 {
				break
			}

			percentage := 0.0
			if balance.TotalExpense > 0 {
				percentage = (cs.Amount / balance.TotalExpense) * 100
			}

			colorIdx := i % len(colors)
			pdf.SetFillColor(colors[colorIdx][0], colors[colorIdx][1], colors[colorIdx][2])

			// Category name
			pdf.SetTextColor(45, 52, 54)
			pdf.SetX(30)
			pdf.SetY(yPos)
			pdf.Cell(nil, cs.Category)

			// Bar
			barWidth := (percentage / 100.0) * maxWidth
			if barWidth < 10 {
				barWidth = 10
			}
			pdf.RectFromUpperLeftWithStyle(150, yPos, barWidth, 15, "F")

			// Percentage
			pdf.SetX(420)
			pdf.SetY(yPos)
			pdf.Cell(nil, fmt.Sprintf("%.1f%% (%.0f บาท)", percentage, cs.Amount))

			yPos += 22
		}
	}

	// Budget section
	if len(budgetStatus) > 0 {
		yPos += 20
		pdf.SetFont("SarabunBold", "", 16)
		pdf.SetX(30)
		pdf.SetY(yPos)
		pdf.Cell(nil, "สถานะงบประมาณ")
		yPos += 30

		pdf.SetFont("Sarabun", "", 12)
		for _, status := range budgetStatus {
			// Status indicator
			if status.IsOverBudget {
				pdf.SetTextColor(214, 48, 49) // Red
				pdf.SetX(30)
				pdf.SetY(yPos)
				pdf.Cell(nil, "[เกิน]")
			} else if status.Percentage >= 80 {
				pdf.SetTextColor(253, 203, 110) // Yellow
				pdf.SetX(30)
				pdf.SetY(yPos)
				pdf.Cell(nil, "[เตือน]")
			} else {
				pdf.SetTextColor(0, 184, 148) // Green
				pdf.SetX(30)
				pdf.SetY(yPos)
				pdf.Cell(nil, "[ปกติ]")
			}

			pdf.SetTextColor(45, 52, 54)
			pdf.SetX(80)
			pdf.Cell(nil, status.Category)
			pdf.SetX(200)
			pdf.Cell(nil, fmt.Sprintf("%.0f / %.0f บาท (%.0f%%)", status.Spent, status.Budget, status.Percentage))
			yPos += 20
		}
	}

	// Footer
	pdf.SetFillColor(245, 247, 250)
	pdf.RectFromUpperLeftWithStyle(0, 790, 595, 52, "F")

	pdf.SetFont("Sarabun", "", 10)
	pdf.SetTextColor(99, 110, 114)
	pdf.SetX(30)
	pdf.SetY(800)
	pdf.Cell(nil, "สร้างโดย สติสตางค์ - ผู้ช่วยจัดการเงินส่วนตัว | LINE: @satisatang")

	// Write to buffer
	var buf bytes.Buffer
	if _, err := pdf.WriteTo(&buf); err != nil {
		return nil, "", fmt.Errorf("ไม่สามารถสร้างไฟล์ PDF: %w", err)
	}

	// Generate unique filename with random numbers only
	randomNum := fmt.Sprintf("%d%d", time.Now().UnixNano(), time.Now().UnixMicro()%10000)
	filename := fmt.Sprintf("%s.pdf", randomNum)
	return buf.Bytes(), filename, nil
}

// GetCategorySpendingForChart returns spending data formatted for chart display
func (s *ExportService) GetCategorySpendingForChart(ctx context.Context, lineID string) ([]CategoryChartData, float64, error) {
	spending, err := s.mongo.GetMonthlySpendingByCategory(ctx, lineID)
	if err != nil {
		return nil, 0, err
	}

	var total float64
	for _, amount := range spending {
		total += amount
	}

	var result []CategoryChartData
	i := 0
	for category, amount := range spending {
		percentage := 0.0
		if total > 0 {
			percentage = (amount / total) * 100
		}
		result = append(result, CategoryChartData{
			Category:   category,
			Amount:     amount,
			Percentage: percentage,
			Color:      categoryColors[i%len(categoryColors)],
		})
		i++
	}

	// Sort by amount descending
	sort.Slice(result, func(i, j int) bool {
		return result[i].Amount > result[j].Amount
	})

	return result, total, nil
}

// CategoryChartData represents data for chart display
type CategoryChartData struct {
	Category   string  `json:"category"`
	Amount     float64 `json:"amount"`
	Percentage float64 `json:"percentage"`
	Color      string  `json:"color"`
}
