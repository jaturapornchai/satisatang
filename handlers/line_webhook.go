package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/line/line-bot-sdk-go/v8/linebot/messaging_api"
	"github.com/line/line-bot-sdk-go/v8/linebot/webhook"
	"github.com/satisatang/backend/services"
)

type LineWebhookHandler struct {
	channelSecret string
	bot           *messaging_api.MessagingApiAPI
	blobAPI       *messaging_api.MessagingApiBlobAPI
	ai            services.AIChat
	mongo         *services.MongoDBService
	export        *services.ExportService
	firebase      *services.FirebaseService
}

func NewLineWebhookHandler(channelSecret, channelToken string, ai services.AIChat, mongo *services.MongoDBService, firebase *services.FirebaseService) (*LineWebhookHandler, error) {
	bot, err := messaging_api.NewMessagingApiAPI(channelToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create Line bot: %w", err)
	}

	blobAPI, err := messaging_api.NewMessagingApiBlobAPI(channelToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create Line blob API: %w", err)
	}

	return &LineWebhookHandler{
		channelSecret: channelSecret,
		bot:           bot,
		blobAPI:       blobAPI,
		ai:            ai,
		mongo:         mongo,
		export:        services.NewExportService(mongo),
		firebase:      firebase,
	}, nil
}

func (h *LineWebhookHandler) HandleWebhook(c *gin.Context) {
	cb, err := webhook.ParseRequest(h.channelSecret, c.Request)
	if err != nil {
		log.Printf("Failed to parse webhook: %v", err)
		if err == webhook.ErrInvalidSignature {
			c.Status(http.StatusBadRequest)
		} else {
			c.Status(http.StatusInternalServerError)
		}
		return
	}

	for _, event := range cb.Events {
		log.Printf("Got event: %v", event)

		switch e := event.(type) {
		case webhook.MessageEvent:
			h.handleMessage(c.Request.Context(), e)
		case webhook.PostbackEvent:
			h.handlePostback(c.Request.Context(), e)
		}
	}

	c.Status(http.StatusOK)
}

func (h *LineWebhookHandler) handleMessage(ctx context.Context, event webhook.MessageEvent) {
	log.Printf("Message type: %T", event.Message)
	replyToken := event.ReplyToken

	switch message := event.Message.(type) {
	case webhook.ImageMessageContent:
		log.Printf("Processing image message")
		h.handleImageMessage(ctx, event.Source, message, replyToken)
	case webhook.TextMessageContent:
		log.Printf("Processing text message: %s", message.Text)
		h.handleTextMessage(ctx, event.Source, message, replyToken)
	default:
		log.Printf("Unknown message type: %T", event.Message)
	}
}

func (h *LineWebhookHandler) handleImageMessage(ctx context.Context, source webhook.SourceInterface, message webhook.ImageMessageContent, replyToken string) {
	userID := h.getUserID(source)
	if userID == "" {
		log.Println("Failed to get user ID")
		return
	}

	// Process synchronously for serverless compatibility
	content, err := h.blobAPI.GetMessageContent(message.Id)
	if err != nil {
		log.Printf("Failed to get message content: %v", err)
		h.replyText(replyToken, "ขออภัยค่ะ ไม่สามารถดาวน์โหลดรูปภาพได้")
		return
	}
	defer content.Body.Close()

	contentType := content.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/jpeg"
	}
	log.Printf("Image content type: %s", contentType)

	// Read image data into bytes for both AI processing and storage
	imageBytes, err := io.ReadAll(content.Body)
	if err != nil {
		log.Printf("Failed to read image data: %v", err)
		h.replyText(replyToken, "ขออภัยค่ะ ไม่สามารถอ่านรูปภาพได้")
		return
	}

	// Convert to base64 for storage
	imageBase64 := base64.StdEncoding.EncodeToString(imageBytes)

	// Process image with AI (using bytes.Reader to allow re-reading)
	transactionData, err := h.ai.ProcessReceiptImage(context.Background(), bytes.NewReader(imageBytes), contentType)
	if err != nil {
		log.Printf("Failed to process image with Gemini: %v", err)
		h.replyText(replyToken, "ขออภัยค่ะ ไม่สามารถอ่านข้อมูลจากรูปภาพได้ กรุณาลองใหม่อีกครั้ง")
		return
	}

	// Store image base64 in transaction data for MongoDB
	transactionData.ImageBase64 = imageBase64
	transactionData.ImageMimeType = contentType

	// Check if it's a transfer slip - ask user if income or expense
	if transactionData.ImageType == "slip" {
		h.replySlipConfirmFlex(replyToken, userID, transactionData)
		return
	}

	// Regular receipt - process directly
	h.replyTransactionFlex(replyToken, userID, transactionData)
}

func (h *LineWebhookHandler) handleTextMessage(ctx context.Context, source webhook.SourceInterface, message webhook.TextMessageContent, replyToken string) {
	userID := h.getUserID(source)
	log.Printf("handleTextMessage - userID: %s, source type: %T", userID, source)

	if userID == "" {
		log.Printf("userID is empty, cannot reply")
		return
	}

	bgCtx := context.Background()

	// Check if user has pending slip waiting for category
	pendingKey := fmt.Sprintf("slip_pending_%s", userID)
	if pendingJSON, err := h.mongo.GetTempData(bgCtx, pendingKey); err == nil && pendingJSON != "" {
		// User typed category for pending slip
		h.handleSlipCategoryText(bgCtx, replyToken, userID, message.Text, pendingJSON)
		return
	}

	// Get last transaction for update reference
	lastTx, _, _ := h.mongo.GetLastTransaction(bgCtx, userID)

	// Get user's data structure for AI context (compact)
	userBanks, userCards, _ := h.mongo.GetDistinctPaymentMethods(bgCtx, userID)
	_, expenseCategories, _ := h.mongo.GetDistinctCategories(bgCtx, userID)

	// Build compact schema for AI
	schema := ""
	if len(userBanks) > 0 {
		schema += "ธนาคาร:" + strings.Join(userBanks, ",")
	}
	if len(userCards) > 0 {
		if schema != "" {
			schema += "|"
		}
		schema += "บัตร:" + strings.Join(userCards, ",")
	}
	if len(expenseCategories) > 0 {
		if schema != "" {
			schema += "|"
		}
		schema += "หมวด:" + strings.Join(expenseCategories, ",")
	}

	// Add balance summary for AI context (important!)
	balanceSummary := h.buildBalanceSummaryForAI(bgCtx, userID)
	if balanceSummary != "" {
		schema += "\n" + balanceSummary
	}

	// Get chat history (last 20 messages)
	chatHistory := ""
	if history, err := h.mongo.GetChatHistory(bgCtx, userID, 20); err == nil && len(history) > 0 {
		var historyLines []string
		for _, msg := range history {
			historyLines = append(historyLines, msg.Role+": "+msg.Content)
		}
		chatHistory = strings.Join(historyLines, "\n")
	}

	// Save user message to history
	h.mongo.SaveChatMessage(bgCtx, userID, "user", message.Text)

	log.Printf("Calling AI with message: %s", message.Text)

	// Send schema and chat history to AI
	response, err := h.ai.ChatWithContext(bgCtx, message.Text, schema, chatHistory)
	if err != nil {
		log.Printf("Failed to chat with AI: %v", err)
		h.replyText(replyToken, "ขออภัยค่ะ เกิดข้อผิดพลาด กรุณาลองใหม่อีกครั้ง")
		return
	}

	log.Printf("AI response: %s", response)
	response = cleanJSONResponse(response)

	if response == "" {
		h.replyText(replyToken, "ขออภัยค่ะ ไม่สามารถประมวลผลได้ กรุณาลองใหม่อีกครั้ง")
		return
	}

	// Parse AI response
	var aiResp services.AIResponse
	if err := json.Unmarshal([]byte(response), &aiResp); err != nil {
		if response != "" {
			h.replyText(replyToken, response)
		} else {
			h.replyText(replyToken, "ขออภัยค่ะ ไม่เข้าใจคำสั่ง กรุณาลองใหม่")
		}
		return
	}

	// Go handles query and flex creation
	flexSent := false

	// Process actions
	switch aiResp.Action {
	case "new":
		for _, tx := range aiResp.Transactions {
			if tx.Amount > 0 {
				h.mongo.SaveTransaction(bgCtx, userID, &tx)
			}
		}
		// Send flex for new transaction
		if len(aiResp.Transactions) > 0 {
			flexSent = h.replyTransactionsFlex(bgCtx, userID, replyToken, aiResp.Transactions, aiResp.Message)
		}

	case "balance":
		// Go queries MongoDB and creates flex
		balances, _ := h.mongo.GetBalanceByPaymentType(bgCtx, userID)
		flexSent = h.replyBalanceFlex(bgCtx, userID, replyToken, balances, aiResp.Query, aiResp.Message)

	case "search", "analyze":
		// Go queries using AI's query filter
		results := h.queryTransactions(bgCtx, userID, aiResp.Query)
		flexSent = h.replyQueryResultsFlex(bgCtx, userID, replyToken, results, aiResp.Query, aiResp.Message)

	case "update":
		if lastTx != nil {
			txID := lastTx.ID.Hex()
			switch aiResp.UpdateField {
			case "amount":
				if val, ok := aiResp.UpdateValue.(float64); ok {
					h.mongo.UpdateTransactionAmount(bgCtx, userID, txID, val)
				}
			case "usetype":
				bankName := ""
				creditCard := ""
				var useType int
				if val, ok := aiResp.UpdateValue.(float64); ok {
					useType = int(val)
				} else if valMap, ok := aiResp.UpdateValue.(map[string]interface{}); ok {
					if ut, ok := valMap["usetype"].(float64); ok {
						useType = int(ut)
					}
					if bn, ok := valMap["bankname"].(string); ok {
						bankName = bn
					}
					if cc, ok := valMap["creditcardname"].(string); ok {
						creditCard = cc
					}
				}
				h.mongo.UpdateTransactionPayment(bgCtx, userID, txID, useType, bankName, creditCard)
			case "bankname":
				if val, ok := aiResp.UpdateValue.(string); ok {
					h.mongo.UpdateTransactionPayment(bgCtx, userID, txID, 2, val, "")
				}
			case "creditcardname":
				if val, ok := aiResp.UpdateValue.(string); ok {
					h.mongo.UpdateTransactionPayment(bgCtx, userID, txID, 1, "", val)
				}
			}
		}

	case "transfer":
		if aiResp.Transfer != nil {
			transfer := &services.TransferData{
				From:        make([]services.TransferEntry, len(aiResp.Transfer.From)),
				To:          make([]services.TransferEntry, len(aiResp.Transfer.To)),
				Description: aiResp.Transfer.Description,
			}
			for i, e := range aiResp.Transfer.From {
				transfer.From[i] = services.TransferEntry{
					Amount:         e.Amount,
					UseType:        e.UseType,
					BankName:       e.BankName,
					CreditCardName: e.CreditCardName,
				}
			}
			for i, e := range aiResp.Transfer.To {
				transfer.To[i] = services.TransferEntry{
					Amount:         e.Amount,
					UseType:        e.UseType,
					BankName:       e.BankName,
					CreditCardName: e.CreditCardName,
				}
			}
			h.mongo.SaveTransfer(bgCtx, userID, transfer)
		}

	case "budget":
		if aiResp.Budget != nil && aiResp.Budget.Category != "" && aiResp.Budget.Amount > 0 {
			h.mongo.SetBudget(bgCtx, userID, aiResp.Budget.Category, aiResp.Budget.Amount)
		}

	case "export":
		if aiResp.Export != nil {
			format := aiResp.Export.Format
			if format == "" {
				format = "excel"
			}
			days := aiResp.Export.Days
			if days <= 0 {
				days = 30
			}
			if format == "pdf" {
				data, filename, err := h.export.ExportToPDF(bgCtx, userID, days)
				if err == nil {
					h.replyAndSendFile(replyToken, userID, aiResp.Message, data, filename, "application/pdf")
					flexSent = true
				}
			} else {
				data, filename, err := h.export.ExportToExcel(bgCtx, userID, days)
				if err == nil {
					h.replyAndSendFile(replyToken, userID, aiResp.Message, data, filename, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
					flexSent = true
				}
			}
		}
	}

	// If flex wasn't sent, fallback to text message
	if !flexSent {
		msg := aiResp.Message
		if msg == "" {
			msg = response
		}
		if msg != "" {
			h.replyText(replyToken, msg)
		}
	}

	// Save chat history
	if aiResp.Message != "" {
		h.mongo.SaveChatMessage(bgCtx, userID, "assistant", aiResp.Message)
	}
}

func (h *LineWebhookHandler) getUserID(source webhook.SourceInterface) string {
	switch src := source.(type) {
	case *webhook.UserSource:
		return src.UserId
	case webhook.UserSource:
		return src.UserId
	case *webhook.GroupSource:
		return src.UserId
	case webhook.GroupSource:
		return src.UserId
	case *webhook.RoomSource:
		return src.UserId
	case webhook.RoomSource:
		return src.UserId
	}
	return ""
}

func (h *LineWebhookHandler) replyText(replyToken, text string) {
	_, err := h.bot.ReplyMessage(&messaging_api.ReplyMessageRequest{
		ReplyToken: replyToken,
		Messages: []messaging_api.MessageInterface{
			messaging_api.TextMessage{
				Text: text,
			},
		},
	})
	if err != nil {
		log.Printf("Failed to send reply: %v", err)
	}
}

// cleanFlexData removes empty contents arrays from flex data
func cleanFlexData(data interface{}) interface{} {
	switch v := data.(type) {
	case map[string]interface{}:
		cleaned := make(map[string]interface{})
		for key, val := range v {
			if key == "contents" {
				if arr, ok := val.([]interface{}); ok && len(arr) == 0 {
					continue // Skip empty contents
				}
			}
			cleaned[key] = cleanFlexData(val)
		}
		return cleaned
	case []interface{}:
		result := make([]interface{}, 0, len(v))
		for _, item := range v {
			result = append(result, cleanFlexData(item))
		}
		return result
	default:
		return data
	}
}

// replyFlexFromAI sends Flex Message created by AI
func (h *LineWebhookHandler) replyFlexFromAI(replyToken string, flex interface{}, altText string) bool {
	if flex == nil {
		return false
	}

	// Clean flex data to remove empty contents
	flex = cleanFlexData(flex)

	var flexData interface{}

	// Handle both array and object flex
	switch v := flex.(type) {
	case []interface{}:
		if len(v) == 0 {
			return false
		}
		// If array, wrap in carousel or use first bubble
		if len(v) == 1 {
			flexData = v[0]
		} else {
			// Multiple bubbles -> carousel
			flexData = map[string]interface{}{
				"type":     "carousel",
				"contents": v,
			}
		}
	case map[string]interface{}:
		flexData = v
	default:
		log.Printf("Unknown flex type: %T", flex)
		return false
	}

	// Convert flex to JSON string
	flexJSON, err := json.Marshal(flexData)
	if err != nil {
		log.Printf("Failed to marshal flex: %v", err)
		return false
	}

	// Parse as FlexContainer
	container, err := messaging_api.UnmarshalFlexContainer(flexJSON)
	if err != nil {
		log.Printf("Failed to parse flex container: %v (json: %s)", err, string(flexJSON))
		return false
	}

	if altText == "" {
		altText = "สติสตางค์"
	}

	_, err = h.bot.ReplyMessage(&messaging_api.ReplyMessageRequest{
		ReplyToken: replyToken,
		Messages: []messaging_api.MessageInterface{
			messaging_api.FlexMessage{
				AltText:  altText,
				Contents: container,
			},
		},
	})
	if err != nil {
		log.Printf("Failed to send flex reply: %v", err)
		return false
	}
	return true
}

// queryTransactions queries MongoDB using AI's query filter
func (h *LineWebhookHandler) queryTransactions(ctx context.Context, userID string, query *services.QueryFilter) []services.SearchResult {
	if query == nil {
		return nil
	}

	days := query.Days
	if days <= 0 {
		days = 30
	}

	// Use keyword search if provided (Regex Only)
	if query.Keyword != "" {
		results, _ := h.mongo.SearchTransactions(ctx, userID, query.Keyword, query.Limit)
		return results
	}

	// Use category search if provided
	if len(query.Categories) > 0 {
		results, _ := h.mongo.SearchTransactions(ctx, userID, query.Categories[0], query.Limit)
		return results
	}

	// Default: get recent transactions
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}
	results, _ := h.mongo.SearchByDateRange(ctx, userID,
		time.Now().AddDate(0, 0, -days).Format("2006-01-02"),
		time.Now().Format("2006-01-02"),
		limit)
	return results
}

// replyTransactionsFlex sends flex for new transactions (carousel: transaction + summary)
func (h *LineWebhookHandler) replyTransactionsFlex(ctx context.Context, userID, replyToken string, txs []services.TransactionData, msg string) bool {
	if len(txs) == 0 {
		return false
	}

	tx := txs[0]
	emoji := "💸"
	headerColor := "#E74C3C" // Red for expense
	typeText := "รายจ่าย"
	if tx.Type == "income" {
		emoji = "💰"
		headerColor = "#27AE60" // Green for income
		typeText = "รายรับ"
	}

	// Fallback for empty values
	description := tx.Description
	if description == "" {
		description = tx.Category
	}
	if description == "" {
		description = typeText
	}

	// Get date
	txDate := tx.Date
	if txDate == "" {
		txDate = time.Now().Format("2006-01-02")
	}

	// Get payment method text
	paymentText := getPaymentName(tx.UseType, tx.BankName, tx.CreditCardName)
	if paymentText == "" {
		paymentText = "เงินสด"
	}

	// Get balance summary
	balances, _ := h.mongo.GetBalanceByPaymentType(ctx, userID)
	var cashTotal, bankTotal, creditTotal float64
	for _, b := range balances {
		switch b.UseType {
		case 0:
			cashTotal += b.Balance
		case 1:
			creditTotal += b.Balance // Negative = debt
		case 2:
			bankTotal += b.Balance
		}
	}

	// Assets = cash + bank, Liabilities = credit card debt
	assets := cashTotal + bankTotal
	liabilities := 0.0
	if creditTotal < 0 {
		liabilities = -creditTotal
	}
	equity := assets - liabilities

	// Get income/expense totals
	var totalIncome, totalExpense float64
	if summary, err := h.mongo.GetBalanceSummary(ctx, userID); err == nil && summary != nil {
		totalIncome = summary.TotalIncome
		totalExpense = summary.TotalExpense
	}

	// Build body contents - AI message at top, summary at bottom
	bodyContents := []interface{}{
		// Transaction detail
		map[string]interface{}{"type": "text", "text": description, "size": "md", "weight": "bold", "color": "#333333"},
		map[string]interface{}{"type": "text", "text": formatNumber(tx.Amount), "size": "lg", "weight": "bold", "color": headerColor},
		map[string]interface{}{
			"type": "box", "layout": "horizontal", "margin": "sm",
			"contents": []interface{}{
				map[string]interface{}{"type": "text", "text": "📅 " + txDate, "size": "xxs", "color": "#888888", "flex": 1},
				map[string]interface{}{"type": "text", "text": "📎 " + tx.Category, "size": "xxs", "color": "#888888", "flex": 1},
			},
		},
	}

	// Add AI message after transaction detail (activity log at top)
	if msg != "" {
		bodyContents = append(bodyContents,
			map[string]interface{}{"type": "text", "text": msg, "size": "xs", "color": "#666666", "wrap": true, "margin": "sm"},
		)
	}

	// Add separator and summary section at bottom
	bodyContents = append(bodyContents,
		map[string]interface{}{"type": "separator", "margin": "md"},
		// Summary section
		map[string]interface{}{
			"type": "box", "layout": "horizontal", "margin": "sm",
			"contents": []interface{}{
				map[string]interface{}{"type": "text", "text": "💰 ทุน", "size": "xs", "color": "#3498DB", "flex": 1},
				map[string]interface{}{"type": "text", "text": formatNumber(equity), "size": "xs", "weight": "bold", "color": "#3498DB", "align": "end", "flex": 2},
			},
		},
		map[string]interface{}{
			"type": "box", "layout": "horizontal",
			"contents": []interface{}{
				map[string]interface{}{"type": "text", "text": "🏦 ทรัพย์สิน", "size": "xxs", "color": "#27AE60", "flex": 1},
				map[string]interface{}{"type": "text", "text": formatNumber(assets), "size": "xxs", "color": "#27AE60", "align": "end", "flex": 2},
			},
		},
		map[string]interface{}{
			"type": "box", "layout": "horizontal",
			"contents": []interface{}{
				map[string]interface{}{"type": "text", "text": "💳 หนี้สิน", "size": "xxs", "color": "#E74C3C", "flex": 1},
				map[string]interface{}{"type": "text", "text": formatNumber(liabilities), "size": "xxs", "color": "#E74C3C", "align": "end", "flex": 2},
			},
		},
		map[string]interface{}{"type": "separator", "margin": "sm"},
		map[string]interface{}{
			"type": "box", "layout": "horizontal", "margin": "sm",
			"contents": []interface{}{
				map[string]interface{}{"type": "text", "text": "📈 รายได้", "size": "xxs", "color": "#27AE60", "flex": 1},
				map[string]interface{}{"type": "text", "text": formatNumber(totalIncome), "size": "xxs", "color": "#27AE60", "align": "end", "flex": 2},
			},
		},
		map[string]interface{}{
			"type": "box", "layout": "horizontal",
			"contents": []interface{}{
				map[string]interface{}{"type": "text", "text": "📉 ค่าใช้จ่าย", "size": "xxs", "color": "#E74C3C", "flex": 1},
				map[string]interface{}{"type": "text", "text": formatNumber(totalExpense), "size": "xxs", "color": "#E74C3C", "align": "end", "flex": 2},
			},
		},
	)

	// Single bubble with transaction + summary
	flex := map[string]interface{}{
		"type": "bubble",
		"size": "kilo",
		"header": map[string]interface{}{
			"type":            "box",
			"layout":          "vertical",
			"backgroundColor": headerColor,
			"paddingAll":      "sm",
			"contents": []interface{}{
				map[string]interface{}{"type": "text", "text": emoji + " " + typeText, "color": "#FFFFFF", "weight": "bold", "size": "sm"},
			},
		},
		"body": map[string]interface{}{
			"type":       "box",
			"layout":     "vertical",
			"paddingAll": "md",
			"contents":   bodyContents,
		},
		"footer": map[string]interface{}{
			"type":       "box",
			"layout":     "vertical",
			"paddingAll": "sm",
			"contents": []interface{}{
				map[string]interface{}{
					"type": "button", "style": "secondary", "height": "sm",
					"action": map[string]interface{}{"type": "message", "label": "🗑️ ลบรายการนี้", "text": "ลบรายการล่าสุด"},
				},
			},
		},
	}

	return h.replyFlexFromAI(replyToken, flex, msg)
}

// replyBalanceFlex sends flex for balance query
func (h *LineWebhookHandler) replyBalanceFlex(ctx context.Context, userID, replyToken string, balances []services.PaymentBalance, query *services.QueryFilter, msg string) bool {
	if len(balances) == 0 {
		return false
	}

	// Filter by query if provided
	var filtered []services.PaymentBalance
	for _, b := range balances {
		if query != nil {
			if query.UseType >= 0 && b.UseType != query.UseType {
				continue
			}
			if query.BankName != "" && b.BankName != query.BankName {
				continue
			}
		}
		filtered = append(filtered, b)
	}

	if len(filtered) == 0 {
		filtered = balances
	}

	// Build flex contents
	contents := []interface{}{}
	var total float64

	for _, b := range filtered {
		name := getPaymentName(b.UseType, b.BankName, b.CreditCardName)
		color := "#27AE60"
		if b.Balance < 0 {
			color = "#E74C3C"
		}
		total += b.Balance

		contents = append(contents, map[string]interface{}{
			"type":   "box",
			"layout": "horizontal",
			"contents": []interface{}{
				map[string]interface{}{"type": "text", "text": name, "size": "sm", "color": "#666666", "flex": 2},
				map[string]interface{}{"type": "text", "text": formatNumber(b.Balance), "size": "sm", "weight": "bold", "color": color, "align": "end", "flex": 3},
			},
		})
	}

	// Add total
	totalColor := "#27AE60"
	if total < 0 {
		totalColor = "#E74C3C"
	}
	contents = append(contents,
		map[string]interface{}{"type": "separator", "margin": "md"},
		map[string]interface{}{
			"type":   "box",
			"layout": "horizontal",
			"margin": "md",
			"contents": []interface{}{
				map[string]interface{}{"type": "text", "text": "💰 รวม", "size": "md", "weight": "bold", "flex": 2},
				map[string]interface{}{"type": "text", "text": formatNumber(total), "size": "lg", "weight": "bold", "color": totalColor, "align": "end", "flex": 3},
			},
		},
	)

	// Add AI message at the bottom if provided
	if msg != "" {
		contents = append(contents,
			map[string]interface{}{"type": "separator", "margin": "md"},
			map[string]interface{}{"type": "text", "text": msg, "size": "sm", "color": "#666666", "wrap": true, "margin": "md"},
		)
	}

	flex := map[string]interface{}{
		"type": "bubble",
		"size": "kilo",
		"body": map[string]interface{}{
			"type":     "box",
			"layout":   "vertical",
			"contents": contents,
		},
	}

	return h.replyFlexFromAI(replyToken, flex, msg)
}

// replyQueryResultsFlex sends flex for search/analyze results
func (h *LineWebhookHandler) replyQueryResultsFlex(ctx context.Context, userID, replyToken string, results []services.SearchResult, query *services.QueryFilter, msg string) bool {
	if len(results) == 0 {
		return false
	}

	// Group by category if requested
	groupBy := "none"
	if query != nil && query.GroupBy != "" {
		groupBy = query.GroupBy
	}

	contents := []interface{}{}
	var totalIncome, totalExpense float64

	if groupBy == "category" {
		// Group by category
		categoryTotals := make(map[string]float64)
		for _, r := range results {
			categoryTotals[r.Transaction.Category] += r.Transaction.Amount * float64(r.Transaction.Type)
		}

		for cat, amount := range categoryTotals {
			emoji := getCategoryEmoji(cat)
			color := "#27AE60"
			if amount < 0 {
				color = "#E74C3C"
				amount = -amount
				totalExpense += amount
			} else {
				totalIncome += amount
			}

			contents = append(contents, map[string]interface{}{
				"type":   "box",
				"layout": "horizontal",
				"contents": []interface{}{
					map[string]interface{}{"type": "text", "text": emoji + " " + cat, "size": "sm", "flex": 2},
					map[string]interface{}{"type": "text", "text": formatNumber(amount), "size": "sm", "weight": "bold", "color": color, "align": "end", "flex": 2},
				},
			})
		}
	} else {
		// Show individual transactions (limit 10)
		limit := 10
		if len(results) < limit {
			limit = len(results)
		}

		for i := 0; i < limit; i++ {
			r := results[i]
			emoji := getCategoryEmoji(r.Transaction.Category)
			color := "#27AE60"
			amount := r.Transaction.Amount
			if r.Transaction.Type == -1 {
				color = "#E74C3C"
				totalExpense += amount
			} else {
				totalIncome += amount
			}

			desc := r.Transaction.Description
			if desc == "" {
				desc = r.Transaction.Category
			}

			contents = append(contents, map[string]interface{}{
				"type":   "box",
				"layout": "horizontal",
				"contents": []interface{}{
					map[string]interface{}{"type": "text", "text": emoji + " " + desc, "size": "xs", "color": "#666666", "flex": 3},
					map[string]interface{}{"type": "text", "text": formatNumber(amount), "size": "xs", "weight": "bold", "color": color, "align": "end", "flex": 2},
				},
			})
		}
	}

	// Add summary
	contents = append(contents, map[string]interface{}{"type": "separator", "margin": "md"})
	if totalIncome > 0 {
		contents = append(contents, map[string]interface{}{
			"type": "box", "layout": "horizontal", "margin": "sm",
			"contents": []interface{}{
				map[string]interface{}{"type": "text", "text": "รายรับ", "size": "sm", "color": "#666666"},
				map[string]interface{}{"type": "text", "text": formatNumber(totalIncome), "size": "sm", "color": "#27AE60", "align": "end"},
			},
		})
	}
	if totalExpense > 0 {
		contents = append(contents, map[string]interface{}{
			"type": "box", "layout": "horizontal", "margin": "sm",
			"contents": []interface{}{
				map[string]interface{}{"type": "text", "text": "รายจ่าย", "size": "sm", "color": "#666666"},
				map[string]interface{}{"type": "text", "text": formatNumber(totalExpense), "size": "sm", "color": "#E74C3C", "align": "end"},
			},
		})
	}

	// Add balance summary footer
	if summary := h.buildBalanceSummaryContents(ctx, userID); summary != nil {
		contents = append(contents, summary...)
	}

	// Add AI message at the bottom if provided
	if msg != "" {
		contents = append(contents,
			map[string]interface{}{"type": "separator", "margin": "md"},
			map[string]interface{}{"type": "text", "text": msg, "size": "sm", "color": "#666666", "wrap": true, "margin": "md"},
		)
	}

	flex := map[string]interface{}{
		"type": "bubble",
		"size": "kilo",
		"body": map[string]interface{}{
			"type":     "box",
			"layout":   "vertical",
			"contents": contents,
		},
	}

	return h.replyFlexFromAI(replyToken, flex, msg)
}

// buildBalanceSummaryContents returns flex contents for balance summary footer
func (h *LineWebhookHandler) buildBalanceSummaryContents(ctx context.Context, userID string) []interface{} {
	balances, _ := h.mongo.GetBalanceByPaymentType(ctx, userID)
	if len(balances) == 0 {
		return nil
	}

	// Calculate totals by type
	var cashTotal, bankTotal, creditTotal float64
	for _, b := range balances {
		switch b.UseType {
		case 0:
			cashTotal += b.Balance
		case 1:
			creditTotal += b.Balance // Negative = debt
		case 2:
			bankTotal += b.Balance
		}
	}
	grandTotal := cashTotal + bankTotal + creditTotal

	// Build compact summary
	contents := []interface{}{
		map[string]interface{}{"type": "separator", "margin": "lg"},
		map[string]interface{}{"type": "text", "text": "📊 สรุปยอด", "size": "xs", "color": "#888888", "margin": "md"},
	}

	// Cash
	if cashTotal != 0 {
		color := "#27AE60"
		if cashTotal < 0 {
			color = "#E74C3C"
		}
		contents = append(contents, map[string]interface{}{
			"type": "box", "layout": "horizontal", "margin": "sm",
			"contents": []interface{}{
				map[string]interface{}{"type": "text", "text": "💵 เงินสด", "size": "xs", "color": "#666666", "flex": 2},
				map[string]interface{}{"type": "text", "text": formatNumber(cashTotal), "size": "xs", "color": color, "align": "end", "flex": 2},
			},
		})
	}

	// Bank
	if bankTotal != 0 {
		color := "#27AE60"
		if bankTotal < 0 {
			color = "#E74C3C"
		}
		contents = append(contents, map[string]interface{}{
			"type": "box", "layout": "horizontal", "margin": "sm",
			"contents": []interface{}{
				map[string]interface{}{"type": "text", "text": "🏦 ธนาคาร", "size": "xs", "color": "#666666", "flex": 2},
				map[string]interface{}{"type": "text", "text": formatNumber(bankTotal), "size": "xs", "color": color, "align": "end", "flex": 2},
			},
		})
	}

	// Credit card
	if creditTotal != 0 {
		color := "#27AE60"
		if creditTotal < 0 {
			color = "#E74C3C"
		}
		contents = append(contents, map[string]interface{}{
			"type": "box", "layout": "horizontal", "margin": "sm",
			"contents": []interface{}{
				map[string]interface{}{"type": "text", "text": "💳 บัตรเครดิต", "size": "xs", "color": "#666666", "flex": 2},
				map[string]interface{}{"type": "text", "text": formatNumber(creditTotal), "size": "xs", "color": color, "align": "end", "flex": 2},
			},
		})
	}

	// Grand total
	totalColor := "#1E88E5"
	if grandTotal < 0 {
		totalColor = "#E74C3C"
	}
	contents = append(contents, map[string]interface{}{
		"type": "box", "layout": "horizontal", "margin": "md",
		"contents": []interface{}{
			map[string]interface{}{"type": "text", "text": "💰 รวม", "size": "sm", "weight": "bold", "flex": 2},
			map[string]interface{}{"type": "text", "text": formatNumber(grandTotal), "size": "sm", "weight": "bold", "color": totalColor, "align": "end", "flex": 2},
		},
	})

	return contents
}

// buildBalanceSummaryForAI returns text summary of balances for AI context
func (h *LineWebhookHandler) buildBalanceSummaryForAI(ctx context.Context, userID string) string {
	// Get balance by payment type
	balances, _ := h.mongo.GetBalanceByPaymentType(ctx, userID)

	// Get income/expense summary
	summary, _ := h.mongo.GetBalanceSummary(ctx, userID)

	var parts []string

	// Build balance details
	var cashTotal, bankTotal, creditTotal, grandTotal float64
	var bankDetails, cardDetails []string

	for _, b := range balances {
		switch b.UseType {
		case 0:
			cashTotal += b.Balance
		case 1:
			creditTotal += b.Balance
			name := b.CreditCardName
			if name == "" {
				name = "บัตรเครดิต"
			}
			cardDetails = append(cardDetails, fmt.Sprintf("%s:%.0f", name, b.Balance))
		case 2:
			bankTotal += b.Balance
			name := b.BankName
			if name == "" {
				name = "ธนาคาร"
			}
			bankDetails = append(bankDetails, fmt.Sprintf("%s:%.0f", name, b.Balance))
		}
		grandTotal += b.Balance
	}

	// Add summary line
	parts = append(parts, fmt.Sprintf("ยอดรวม:%.0f", grandTotal))

	if cashTotal != 0 {
		parts = append(parts, fmt.Sprintf("เงินสด:%.0f", cashTotal))
	}
	if bankTotal != 0 {
		parts = append(parts, fmt.Sprintf("ธนาคารรวม:%.0f", bankTotal))
	}
	if len(bankDetails) > 0 {
		parts = append(parts, strings.Join(bankDetails, ","))
	}
	if creditTotal != 0 {
		parts = append(parts, fmt.Sprintf("บัตรเครดิตรวม:%.0f", creditTotal))
	}
	if len(cardDetails) > 0 {
		parts = append(parts, strings.Join(cardDetails, ","))
	}

	// Add income/expense from summary
	if summary != nil {
		parts = append(parts, fmt.Sprintf("รายได้รวม:%.0f", summary.TotalIncome))
		parts = append(parts, fmt.Sprintf("รายจ่ายรวม:%.0f", summary.TotalExpense))
		if summary.TodayIncome > 0 || summary.TodayExpense > 0 {
			parts = append(parts, fmt.Sprintf("วันนี้รับ:%.0f,จ่าย:%.0f", summary.TodayIncome, summary.TodayExpense))
		}
	}

	if len(parts) == 0 {
		return ""
	}

	return "สรุปยอด|" + strings.Join(parts, "|")
}

// getCategoryEmoji returns emoji for category
func getCategoryEmoji(category string) string {
	emojis := map[string]string{
		"อาหาร": "🍔", "เดินทาง": "🚗", "ที่อยู่": "🏠", "ค่าน้ำ": "💧", "ค่าไฟ": "💡",
		"ช้อปปิ้ง": "🛒", "บันเทิง": "🎬", "สุขภาพ": "💊", "การศึกษา": "📚", "ของใช้": "🧴",
		"เงินเดือน": "💵", "โบนัส": "🎁", "โอนเงิน": "🔄",
	}
	if e, ok := emojis[category]; ok {
		return e
	}
	return "💰"
}

// replyDeleteConfirmFlex sends flex message for delete confirmation
func (h *LineWebhookHandler) replyDeleteConfirmFlex(replyToken string, balance float64) {
	flex := map[string]interface{}{
		"type": "bubble",
		"size": "kilo",
		"body": map[string]interface{}{
			"type":       "box",
			"layout":     "vertical",
			"paddingAll": "md",
			"contents": []interface{}{
				map[string]interface{}{"type": "text", "text": "🗑️ ลบรายการแล้ว", "weight": "bold", "size": "sm", "color": "#E74C3C"},
				map[string]interface{}{"type": "separator", "margin": "sm"},
				map[string]interface{}{"type": "text", "text": "ยอดคงเหลือ", "size": "xxs", "color": "#888888", "margin": "sm"},
				map[string]interface{}{"type": "text", "text": formatNumber(balance) + " บาท", "size": "lg", "weight": "bold", "color": "#3498DB"},
			},
		},
	}

	jsonData, err := json.Marshal(flex)
	if err != nil {
		log.Printf("Failed to marshal delete flex: %v", err)
		h.replyText(replyToken, fmt.Sprintf("🗑️ ลบรายการแล้ว คงเหลือ %s บาท", formatNumber(balance)))
		return
	}

	container, err := messaging_api.UnmarshalFlexContainer(jsonData)
	if err != nil {
		log.Printf("Failed to unmarshal delete flex: %v", err)
		h.replyText(replyToken, fmt.Sprintf("🗑️ ลบรายการแล้ว คงเหลือ %s บาท", formatNumber(balance)))
		return
	}

	_, err = h.bot.ReplyMessage(&messaging_api.ReplyMessageRequest{
		ReplyToken: replyToken,
		Messages: []messaging_api.MessageInterface{
			messaging_api.FlexMessage{
				AltText:  "ลบรายการแล้ว",
				Contents: container,
			},
		},
	})
	if err != nil {
		log.Printf("Failed to send delete flex: %v", err)
	}
}

// replyTextWithSuggestions sends text with quick reply suggestions
func (h *LineWebhookHandler) replyTextWithSuggestions(replyToken, text string) {
	_, err := h.bot.ReplyMessage(&messaging_api.ReplyMessageRequest{
		ReplyToken: replyToken,
		Messages: []messaging_api.MessageInterface{
			messaging_api.TextMessage{
				Text: text,
				QuickReply: &messaging_api.QuickReply{
					Items: []messaging_api.QuickReplyItem{
						{Action: &messaging_api.MessageAction{Label: "💰 ดูยอดคงเหลือ", Text: "ยอดคงเหลือ"}},
						{Action: &messaging_api.MessageAction{Label: "📊 สรุปวันนี้", Text: "สรุปวันนี้"}},
						{Action: &messaging_api.MessageAction{Label: "🔄 โอนเงิน", Text: "โอนเงิน"}},
						{Action: &messaging_api.MessageAction{Label: "💵 ฝากเงิน", Text: "ฝากเงิน"}},
						{Action: &messaging_api.MessageAction{Label: "🏧 ถอนเงิน", Text: "ถอนเงิน"}},
						{Action: &messaging_api.MessageAction{Label: "💳 จ่ายบัตร", Text: "จ่ายบัตรเครดิต"}},
					},
				},
			},
		},
	})
	if err != nil {
		log.Printf("Failed to send reply with suggestions: %v", err)
	}
}

// replyTransferFlex shows transfer confirmation with Flex Message
func (h *LineWebhookHandler) replyTransferFlex(replyToken, userID string, transfer *services.TransferData, transferID string, message string) {
	ctx := context.Background()

	// Get balance by payment type for detailed view
	balances, _ := h.mongo.GetBalanceByPaymentType(ctx, userID)

	// Build from entries text
	var fromTexts []string
	var totalFrom float64
	for _, e := range transfer.From {
		name := getPaymentName(e.UseType, e.BankName, e.CreditCardName)
		fromTexts = append(fromTexts, fmt.Sprintf("%s %s", name, formatNumber(e.Amount)))
		totalFrom += e.Amount
	}

	// Build to entries text
	var toTexts []string
	for _, e := range transfer.To {
		name := getPaymentName(e.UseType, e.BankName, e.CreditCardName)
		toTexts = append(toTexts, fmt.Sprintf("%s %s", name, formatNumber(e.Amount)))
	}

	// Build body contents
	bodyContents := []messaging_api.FlexComponentInterface{
		&messaging_api.FlexText{
			Text:  message,
			Size:  "sm",
			Color: "#666666",
			Wrap:  true,
		},
		&messaging_api.FlexSeparator{Margin: "lg"},
		// From section
		&messaging_api.FlexText{
			Text:   "📤 จาก",
			Size:   "sm",
			Color:  "#E74C3C",
			Weight: messaging_api.FlexTextWEIGHT_BOLD,
			Margin: "lg",
		},
	}

	for _, text := range fromTexts {
		bodyContents = append(bodyContents, &messaging_api.FlexText{
			Text:   "   " + text,
			Size:   "sm",
			Color:  "#555555",
			Margin: "xs",
		})
	}

	// To section
	bodyContents = append(bodyContents,
		&messaging_api.FlexText{
			Text:   "📥 ไป",
			Size:   "sm",
			Color:  "#27AE60",
			Weight: messaging_api.FlexTextWEIGHT_BOLD,
			Margin: "lg",
		},
	)

	for _, text := range toTexts {
		bodyContents = append(bodyContents, &messaging_api.FlexText{
			Text:   "   " + text,
			Size:   "sm",
			Color:  "#555555",
			Margin: "xs",
		})
	}

	// Total amount
	bodyContents = append(bodyContents,
		&messaging_api.FlexSeparator{Margin: "lg"},
		&messaging_api.FlexBox{
			Layout: messaging_api.FlexBoxLAYOUT_HORIZONTAL,
			Margin: "lg",
			Contents: []messaging_api.FlexComponentInterface{
				&messaging_api.FlexText{
					Text:   "💵 จำนวนเงิน",
					Size:   "md",
					Color:  "#333333",
					Weight: messaging_api.FlexTextWEIGHT_BOLD,
					Flex:   2,
				},
				&messaging_api.FlexText{
					Text:   fmt.Sprintf("%s", formatNumber(totalFrom)),
					Size:   "lg",
					Color:  "#1E88E5",
					Weight: messaging_api.FlexTextWEIGHT_BOLD,
					Align:  messaging_api.FlexTextALIGN_END,
					Flex:   2,
				},
			},
		},
	)

	// Add detailed balance section
	if len(balances) > 0 {
		// Calculate totals by type
		cashBalance := &services.PaymentBalance{}
		bankBalances := make(map[string]*services.PaymentBalance)
		cardBalances := make(map[string]*services.PaymentBalance)
		netWorth := 0.0

		for _, pb := range balances {
			switch pb.UseType {
			case 0:
				cashBalance.TotalIncome += pb.TotalIncome
				cashBalance.TotalExpense += pb.TotalExpense
				cashBalance.Balance += pb.Balance
			case 1:
				key := pb.CreditCardName
				if key == "" {
					key = "บัตรเครดิต"
				}
				if _, exists := cardBalances[key]; !exists {
					cardBalances[key] = &services.PaymentBalance{CreditCardName: key}
				}
				cardBalances[key].Balance += pb.Balance
			case 2:
				key := pb.BankName
				if key == "" {
					key = "ธนาคาร"
				}
				if _, exists := bankBalances[key]; !exists {
					bankBalances[key] = &services.PaymentBalance{BankName: key}
				}
				bankBalances[key].Balance += pb.Balance
			}
		}

		netWorth = cashBalance.Balance
		for _, pb := range bankBalances {
			netWorth += pb.Balance
		}
		for _, pb := range cardBalances {
			netWorth += pb.Balance
		}

		// Add balance header
		bodyContents = append(bodyContents,
			&messaging_api.FlexSeparator{Margin: "lg"},
			&messaging_api.FlexBox{
				Layout: messaging_api.FlexBoxLAYOUT_HORIZONTAL,
				Margin: "lg",
				Contents: []messaging_api.FlexComponentInterface{
					&messaging_api.FlexText{
						Text:   "💰 ยอดคงเหลือทั้งหมด",
						Size:   "md",
						Color:  "#333333",
						Weight: messaging_api.FlexTextWEIGHT_BOLD,
						Flex:   3,
					},
					&messaging_api.FlexText{
						Text:   formatBalanceText(netWorth),
						Size:   "lg",
						Color:  getBalanceColor(netWorth),
						Weight: messaging_api.FlexTextWEIGHT_BOLD,
						Align:  messaging_api.FlexTextALIGN_END,
						Flex:   2,
					},
				},
			},
		)

		// Cash balance
		if cashBalance.TotalIncome > 0 || cashBalance.TotalExpense > 0 {
			bodyContents = append(bodyContents,
				&messaging_api.FlexBox{
					Layout: messaging_api.FlexBoxLAYOUT_HORIZONTAL,
					Margin: "md",
					Contents: []messaging_api.FlexComponentInterface{
						&messaging_api.FlexText{
							Text:  "   💵 เงินสด",
							Size:  "sm",
							Color: "#555555",
							Flex:  3,
						},
						&messaging_api.FlexText{
							Text:   formatBalanceText(cashBalance.Balance),
							Size:   "sm",
							Color:  getBalanceColor(cashBalance.Balance),
							Weight: messaging_api.FlexTextWEIGHT_BOLD,
							Align:  messaging_api.FlexTextALIGN_END,
							Flex:   2,
						},
					},
				},
			)
		}

		// Bank balances
		for name, pb := range bankBalances {
			bodyContents = append(bodyContents,
				&messaging_api.FlexBox{
					Layout: messaging_api.FlexBoxLAYOUT_HORIZONTAL,
					Margin: "sm",
					Contents: []messaging_api.FlexComponentInterface{
						&messaging_api.FlexText{
							Text:  "   🏦 " + name,
							Size:  "sm",
							Color: "#555555",
							Flex:  3,
						},
						&messaging_api.FlexText{
							Text:   formatBalanceText(pb.Balance),
							Size:   "sm",
							Color:  getBalanceColor(pb.Balance),
							Weight: messaging_api.FlexTextWEIGHT_BOLD,
							Align:  messaging_api.FlexTextALIGN_END,
							Flex:   2,
						},
					},
				},
			)
		}

		// Credit card balances
		for name, pb := range cardBalances {
			label := name
			if pb.Balance < 0 {
				label += " (หนี้)"
			}
			bodyContents = append(bodyContents,
				&messaging_api.FlexBox{
					Layout: messaging_api.FlexBoxLAYOUT_HORIZONTAL,
					Margin: "sm",
					Contents: []messaging_api.FlexComponentInterface{
						&messaging_api.FlexText{
							Text:  "   💳 " + label,
							Size:  "sm",
							Color: "#555555",
							Flex:  3,
						},
						&messaging_api.FlexText{
							Text:   formatBalanceText(pb.Balance),
							Size:   "sm",
							Color:  getBalanceColor(pb.Balance),
							Weight: messaging_api.FlexTextWEIGHT_BOLD,
							Align:  messaging_api.FlexTextALIGN_END,
							Flex:   2,
						},
					},
				},
			)
		}
	}

	flexMessage := messaging_api.FlexMessage{
		AltText: fmt.Sprintf("โอนเงิน %s", formatNumber(totalFrom)),
		Contents: &messaging_api.FlexBubble{
			Size: messaging_api.FlexBubbleSIZE_MEGA,
			Header: &messaging_api.FlexBox{
				Layout:          messaging_api.FlexBoxLAYOUT_VERTICAL,
				BackgroundColor: "#1E88E5",
				PaddingAll:      "20px",
				Contents: []messaging_api.FlexComponentInterface{
					&messaging_api.FlexText{
						Text:   "🔄 โอนเงินสำเร็จ",
						Weight: messaging_api.FlexTextWEIGHT_BOLD,
						Size:   "lg",
						Color:  "#FFFFFF",
					},
					&messaging_api.FlexText{
						Text:   transfer.Description,
						Size:   "sm",
						Color:  "#B3E5FC",
						Margin: "xs",
					},
				},
			},
			Body: &messaging_api.FlexBox{
				Layout:     messaging_api.FlexBoxLAYOUT_VERTICAL,
				PaddingAll: "20px",
				Contents:   bodyContents,
			},
		},
		QuickReply: &messaging_api.QuickReply{
			Items: []messaging_api.QuickReplyItem{
				{
					Action: &messaging_api.PostbackAction{
						Label: "🗑️ ยกเลิกการโอน",
						Data:  "action=delete_transfer&transfer_id=" + transferID,
					},
				},
				{Action: &messaging_api.MessageAction{Label: "💰 ดูยอด", Text: "ยอดคงเหลือ"}},
				{Action: &messaging_api.MessageAction{Label: "🔄 โอนอีก", Text: "โอนเงิน"}},
			},
		},
	}

	_, err := h.bot.ReplyMessage(&messaging_api.ReplyMessageRequest{
		ReplyToken: replyToken,
		Messages:   []messaging_api.MessageInterface{flexMessage},
	})
	if err != nil {
		log.Printf("Failed to send transfer flex: %v", err)
	}
}

// getPaymentName returns display name for payment type
// useType 0 = เงินสด/ทรัพย์สินอื่นๆ (ทอง, คริปโต, หุ้น)
func getPaymentName(useType int, bankName, creditCardName string) string {
	switch useType {
	case 0:
		if bankName != "" {
			return "💰 " + bankName // ทรัพย์สินอื่นๆ
		}
		return "💵 เงินสด"
	case 1:
		if creditCardName != "" {
			return "💳 " + creditCardName
		}
		return "💳 บัตรเครดิต"
	case 2:
		if bankName != "" {
			return "🏦 " + bankName
		}
		return "🏦 ธนาคาร"
	}
	return "💵 เงินสด"
}

// replySlipConfirmFlex shows slip details and asks user if it's income or expense
func (h *LineWebhookHandler) replySlipConfirmFlex(replyToken, userID string, slip *services.TransactionData) {
	ctx := context.Background()

	// Save slip data temporarily for later use
	slipJSON, _ := json.Marshal(slip)
	slipDataKey := fmt.Sprintf("slip_%s_%d", userID, time.Now().Unix())
	h.mongo.SaveTempData(ctx, slipDataKey, string(slipJSON), 10*time.Minute)

	// Use default values for empty fields to avoid LINE API errors
	fromName := orDefault(slip.FromName, "-")
	fromBank := orDefault(slip.FromBank, "-")
	fromAccount := orDefault(slip.FromAccount, "-")
	toName := orDefault(slip.ToName, "-")
	toBank := orDefault(slip.ToBank, "-")
	toAccount := orDefault(slip.ToAccount, "-")
	slipDate := orDefault(slip.Date, "-")
	refNo := orDefault(slip.RefNo, "-")

	// Format bank info with account number
	fromBankInfo := fromBank
	if fromAccount != "-" {
		fromBankInfo = fromBank + " (" + fromAccount + ")"
	}
	toBankInfo := toBank
	if toAccount != "-" {
		toBankInfo = toBank + " (" + toAccount + ")"
	}

	// Smart suggestion based on sender
	// If sender name matches user's display name, suggest expense; otherwise suggest income
	suggestion := "💡 น่าจะเป็นรายรับ (เงินโอนเข้า)"
	suggestionColor := "#27AE60"
	// Check if user is the sender (simple heuristic - can be improved with user profile matching)
	// For now, we'll show a neutral message
	suggestion = "💡 เลือกว่าเป็นรายรับหรือรายจ่าย"
	suggestionColor = "#666666"

	// Build Flex message showing slip details
	flex := map[string]interface{}{
		"type": "bubble",
		"size": "kilo",
		"header": map[string]interface{}{
			"type":            "box",
			"layout":          "vertical",
			"backgroundColor": "#3498DB",
			"paddingAll":      "sm",
			"contents": []interface{}{
				map[string]interface{}{"type": "text", "text": "📄 สลิปโอนเงิน", "color": "#FFFFFF", "weight": "bold", "size": "sm"},
			},
		},
		"body": map[string]interface{}{
			"type":       "box",
			"layout":     "vertical",
			"paddingAll": "md",
			"contents": []interface{}{
				// Amount
				map[string]interface{}{"type": "text", "text": formatNumber(slip.Amount) + " บาท", "size": "xl", "weight": "bold", "color": "#3498DB", "align": "center"},
				map[string]interface{}{"type": "separator", "margin": "md"},
				// From section
				map[string]interface{}{"type": "text", "text": "ผู้โอน", "size": "xxs", "color": "#888888", "margin": "md"},
				map[string]interface{}{
					"type": "box", "layout": "horizontal",
					"contents": []interface{}{
						map[string]interface{}{"type": "text", "text": "👤 " + fromName, "size": "xs", "color": "#333333", "flex": 1, "wrap": true},
					},
				},
				map[string]interface{}{
					"type": "box", "layout": "horizontal",
					"contents": []interface{}{
						map[string]interface{}{"type": "text", "text": "🏦 " + fromBankInfo, "size": "xxs", "color": "#666666", "flex": 1, "wrap": true},
					},
				},
				map[string]interface{}{"type": "separator", "margin": "sm"},
				// To section
				map[string]interface{}{"type": "text", "text": "ผู้รับ", "size": "xxs", "color": "#888888", "margin": "sm"},
				map[string]interface{}{
					"type": "box", "layout": "horizontal",
					"contents": []interface{}{
						map[string]interface{}{"type": "text", "text": "👤 " + toName, "size": "xs", "color": "#333333", "flex": 1, "wrap": true},
					},
				},
				map[string]interface{}{
					"type": "box", "layout": "horizontal",
					"contents": []interface{}{
						map[string]interface{}{"type": "text", "text": "🏦 " + toBankInfo, "size": "xxs", "color": "#666666", "flex": 1, "wrap": true},
					},
				},
				map[string]interface{}{"type": "separator", "margin": "sm"},
				// Date & Ref
				map[string]interface{}{
					"type": "box", "layout": "horizontal", "margin": "sm",
					"contents": []interface{}{
						map[string]interface{}{"type": "text", "text": "📅 " + slipDate, "size": "xxs", "color": "#888888", "flex": 1},
						map[string]interface{}{"type": "text", "text": "🔖 " + refNo, "size": "xxs", "color": "#888888", "flex": 1},
					},
				},
				map[string]interface{}{"type": "separator", "margin": "md"},
				// Suggestion
				map[string]interface{}{"type": "text", "text": suggestion, "size": "xs", "color": suggestionColor, "align": "center", "margin": "md"},
				// Status
				map[string]interface{}{"type": "text", "text": "⏳ รอบันทึกบัญชี", "size": "sm", "color": "#E67E22", "align": "center", "weight": "bold", "margin": "sm"},
			},
		},
		"footer": map[string]interface{}{
			"type":       "box",
			"layout":     "horizontal",
			"paddingAll": "sm",
			"contents": []interface{}{
				map[string]interface{}{
					"type": "button", "style": "primary", "color": "#27AE60", "height": "sm",
					"action": map[string]interface{}{"type": "postback", "label": "💰 รายรับ", "data": fmt.Sprintf("action=slip_income&key=%s", slipDataKey)},
				},
				map[string]interface{}{
					"type": "button", "style": "primary", "color": "#E74C3C", "height": "sm",
					"action": map[string]interface{}{"type": "postback", "label": "💸 รายจ่าย", "data": fmt.Sprintf("action=slip_expense&key=%s", slipDataKey)},
				},
			},
		},
	}

	jsonData, err := json.Marshal(flex)
	if err != nil {
		log.Printf("Failed to marshal slip flex: %v", err)
		h.replyText(replyToken, fmt.Sprintf("📄 สลิปโอนเงิน %s บาท\nผู้โอน: %s\nผู้รับ: %s\n\nตอบ 'รายรับ' หรือ 'รายจ่าย'", formatNumber(slip.Amount), slip.FromName, slip.ToName))
		return
	}

	container, err := messaging_api.UnmarshalFlexContainer(jsonData)
	if err != nil {
		log.Printf("Failed to unmarshal slip flex: %v", err)
		h.replyText(replyToken, fmt.Sprintf("📄 สลิปโอนเงิน %s บาท\nผู้โอน: %s\nผู้รับ: %s\n\nตอบ 'รายรับ' หรือ 'รายจ่าย'", formatNumber(slip.Amount), slip.FromName, slip.ToName))
		return
	}

	_, err = h.bot.ReplyMessage(&messaging_api.ReplyMessageRequest{
		ReplyToken: replyToken,
		Messages: []messaging_api.MessageInterface{
			messaging_api.FlexMessage{
				AltText:  fmt.Sprintf("สลิปโอนเงิน %s บาท", formatNumber(slip.Amount)),
				Contents: container,
			},
		},
	})
	if err != nil {
		log.Printf("Failed to send slip flex: %v", err)
	}
}

// handleSlipCategoryText handles user typing category text for pending slip
func (h *LineWebhookHandler) handleSlipCategoryText(ctx context.Context, replyToken, userID, categoryText, pendingJSON string) {
	// Parse pending slip data
	var pending struct {
		SlipKey string `json:"slip_key"`
		Type    string `json:"type"` // "income" or "expense"
	}
	if err := json.Unmarshal([]byte(pendingJSON), &pending); err != nil {
		log.Printf("Failed to parse pending slip data: %v", err)
		h.replyText(replyToken, "เกิดข้อผิดพลาด กรุณาส่งรูปสลิปใหม่")
		return
	}

	// Get slip data from temp storage
	slipJSON, err := h.mongo.GetTempData(ctx, pending.SlipKey)
	if err != nil {
		log.Printf("Failed to get slip data: %v", err)
		h.replyText(replyToken, "ข้อมูลสลิปหมดอายุ กรุณาส่งรูปใหม่")
		return
	}

	// Parse slip data
	var slip services.TransactionData
	if err := json.Unmarshal([]byte(slipJSON), &slip); err != nil {
		log.Printf("Failed to parse slip data: %v", err)
		h.replyText(replyToken, "เกิดข้อผิดพลาด กรุณาส่งรูปใหม่")
		return
	}

	// Set type and category based on user choice
	slip.Type = pending.Type
	slip.Category = categoryText
	if pending.Type == "income" {
		slip.Description = fmt.Sprintf("รับโอนจาก %s (%s) - %s", slip.FromName, slip.FromBank, categoryText)
		slip.BankName = slip.ToBank
	} else {
		slip.Description = fmt.Sprintf("โอนให้ %s (%s) - %s", slip.ToName, slip.ToBank, categoryText)
		slip.BankName = slip.FromBank
	}
	slip.UseType = 2 // Bank transfer

	// Delete temp data
	pendingKey := fmt.Sprintf("slip_pending_%s", userID)
	h.mongo.DeleteTempData(ctx, pendingKey)
	h.mongo.DeleteTempData(ctx, pending.SlipKey)

	// Save transaction and reply with flex
	h.replyTransactionFlex(replyToken, userID, &slip)
}

// replyTransactionFlex sends transaction flex message using reply (free, no quota)
func (h *LineWebhookHandler) replyTransactionFlex(replyToken, userID string, tx *services.TransactionData) {
	ctx := context.Background()

	// Auto save to MongoDB
	txID, err := h.mongo.SaveTransaction(ctx, userID, tx)
	if err != nil {
		log.Printf("Failed to save transaction: %v", err)
		h.replyText(replyToken, "ขออภัยค่ะ ไม่สามารถบันทึกข้อมูลได้")
		return
	}
	log.Printf("Transaction saved with ID: %s", txID)

	// Get balance summary
	balance, _ := h.mongo.GetBalanceSummary(ctx, userID)

	// Build transaction bubble
	bubble := h.buildTransactionBubble(tx)

	// Build bubbles for carousel (transaction + balance)
	bubbles := []messaging_api.FlexBubble{bubble}
	if balance != nil {
		balanceBubble := h.buildBalanceBubble(balance)
		bubbles = append(bubbles, balanceBubble)
	}

	// Create flex message with edit/delete options
	flexMessage := messaging_api.FlexMessage{
		AltText: fmt.Sprintf("บันทึกแล้ว %s บาท", formatNumber(tx.Amount)),
		Contents: &messaging_api.FlexCarousel{
			Contents: bubbles,
		},
		QuickReply: &messaging_api.QuickReply{
			Items: []messaging_api.QuickReplyItem{
				{
					Action: &messaging_api.PostbackAction{
						Label: "✏️ แก้ไข",
						Data:  fmt.Sprintf("action=edit_request&txid=%s", txID),
					},
				},
				{
					Action: &messaging_api.PostbackAction{
						Label: "🗑️ ลบรายการนี้",
						Data:  fmt.Sprintf("action=delete&txid=%s", txID),
					},
				},
			},
		},
	}

	_, replyErr := h.bot.ReplyMessage(&messaging_api.ReplyMessageRequest{
		ReplyToken: replyToken,
		Messages:   []messaging_api.MessageInterface{flexMessage},
	})
	if replyErr != nil {
		log.Printf("Failed to send flex reply: %v", replyErr)
		// Fallback to text reply - but token may be used, try anyway
		typeText := "💸 รายจ่าย"
		if tx.Type == "income" {
			typeText = "💰 รายรับ"
		}
		log.Printf("Fallback: %s: %.2f บาท (บันทึกแล้ว)", typeText, tx.Amount)
	}
}

// replyTransactionFlexMultiple sends multiple transactions using reply (free, no quota)
func (h *LineWebhookHandler) replyTransactionFlexMultiple(replyToken, userID string, transactions []services.TransactionData) {
	h.replyTransactionFlexMultipleWithAlert(replyToken, userID, transactions, nil)
}

// replyTransactionFlexMultipleWithAlert sends multiple transactions with optional alert messages using reply
func (h *LineWebhookHandler) replyTransactionFlexMultipleWithAlert(replyToken, userID string, transactions []services.TransactionData, alertMsgs []string) {
	if len(transactions) == 0 {
		return
	}

	// If only one transaction and no alerts, use single flex
	if len(transactions) == 1 && len(alertMsgs) == 0 {
		h.replyTransactionFlex(replyToken, userID, &transactions[0])
		return
	}

	// Auto save all transactions
	var txIDs []string
	for i := range transactions {
		tx := &transactions[i]
		txID, err := h.mongo.SaveTransaction(context.Background(), userID, tx)
		if err != nil {
			log.Printf("Failed to save transaction: %v", err)
			continue
		}
		txIDs = append(txIDs, txID)
	}

	// Get balance summary
	balance, _ := h.mongo.GetBalanceSummary(context.Background(), userID)

	// Build bubbles for carousel
	var bubbles []messaging_api.FlexBubble
	for i := range transactions {
		tx := &transactions[i]
		bubble := h.buildTransactionBubble(tx)
		bubbles = append(bubbles, bubble)
	}

	// Add balance bubble at the end
	if balance != nil {
		balanceBubble := h.buildBalanceBubble(balance)
		bubbles = append(bubbles, balanceBubble)
	}

	// Create carousel
	flexMessage := messaging_api.FlexMessage{
		AltText: fmt.Sprintf("บันทึก %d รายการแล้ว", len(txIDs)),
		Contents: &messaging_api.FlexCarousel{
			Contents: bubbles,
		},
		QuickReply: &messaging_api.QuickReply{
			Items: []messaging_api.QuickReplyItem{
				{
					Action: &messaging_api.PostbackAction{
						Label: "🗑️ ลบทั้งหมด",
						Data:  "action=delete_all&txids=" + strings.Join(txIDs, ","),
					},
				},
			},
		},
	}

	// Build messages array - flex message first, then alerts
	messages := []messaging_api.MessageInterface{flexMessage}
	for _, alertMsg := range alertMsgs {
		messages = append(messages, messaging_api.TextMessage{Text: alertMsg})
	}

	_, err := h.bot.ReplyMessage(&messaging_api.ReplyMessageRequest{
		ReplyToken: replyToken,
		Messages:   messages,
	})
	if err != nil {
		log.Printf("Failed to send flex carousel reply: %v", err)
	}
}

func (h *LineWebhookHandler) buildTransactionBubble(tx *services.TransactionData) messaging_api.FlexBubble {
	typeText := "💸 รายจ่าย"
	typeColor := "#E74C3C"
	if tx.Type == "income" {
		typeText = "💰 รายรับ"
		typeColor = "#27AE60"
	}

	return messaging_api.FlexBubble{
		Size: messaging_api.FlexBubbleSIZE_KILO,
		Header: &messaging_api.FlexBox{
			Layout:          messaging_api.FlexBoxLAYOUT_VERTICAL,
			BackgroundColor: typeColor,
			PaddingAll:      "15px",
			Contents: []messaging_api.FlexComponentInterface{
				&messaging_api.FlexText{
					Text:   "✅ บันทึกบัญชีแล้ว",
					Weight: messaging_api.FlexTextWEIGHT_BOLD,
					Size:   "sm",
					Color:  "#FFFFFF",
				},
				&messaging_api.FlexText{
					Text:   typeText,
					Weight: messaging_api.FlexTextWEIGHT_BOLD,
					Size:   "lg",
					Color:  "#FFFFFF",
				},
			},
		},
		Body: &messaging_api.FlexBox{
			Layout:     messaging_api.FlexBoxLAYOUT_VERTICAL,
			PaddingAll: "15px",
			Contents: []messaging_api.FlexComponentInterface{
				&messaging_api.FlexText{
					Text:   tx.Description,
					Size:   "md",
					Color:  "#333333",
					Weight: messaging_api.FlexTextWEIGHT_BOLD,
				},
				&messaging_api.FlexText{
					Text:   fmt.Sprintf("%s", formatNumber(tx.Amount)),
					Size:   "lg",
					Color:  typeColor,
					Weight: messaging_api.FlexTextWEIGHT_BOLD,
					Margin: "sm",
				},
				&messaging_api.FlexText{
					Text:   fmt.Sprintf("📅 %s | 🏷️ %s", tx.Date, tx.Category),
					Size:   "xs",
					Color:  "#888888",
					Margin: "md",
				},
			},
		},
	}
}

func (h *LineWebhookHandler) buildBalanceBubble(balance *services.BalanceSummary) messaging_api.FlexBubble {
	balanceColor := "#1E88E5"
	if balance.Balance < 0 {
		balanceColor = "#E74C3C"
	}

	return messaging_api.FlexBubble{
		Size: messaging_api.FlexBubbleSIZE_KILO,
		Header: &messaging_api.FlexBox{
			Layout:          messaging_api.FlexBoxLAYOUT_VERTICAL,
			BackgroundColor: "#1E88E5",
			PaddingAll:      "15px",
			Contents: []messaging_api.FlexComponentInterface{
				&messaging_api.FlexText{
					Text:   "💰 สรุปยอด",
					Weight: messaging_api.FlexTextWEIGHT_BOLD,
					Size:   "lg",
					Color:  "#FFFFFF",
				},
			},
		},
		Body: &messaging_api.FlexBox{
			Layout:     messaging_api.FlexBoxLAYOUT_VERTICAL,
			PaddingAll: "15px",
			Contents: []messaging_api.FlexComponentInterface{
				&messaging_api.FlexText{
					Text:  "ยอดคงเหลือ",
					Size:  "sm",
					Color: "#888888",
				},
				&messaging_api.FlexText{
					Text:   fmt.Sprintf("%s", formatNumber(balance.Balance)),
					Size:   "xl",
					Color:  balanceColor,
					Weight: messaging_api.FlexTextWEIGHT_BOLD,
					Margin: "sm",
				},
				&messaging_api.FlexSeparator{Margin: "lg"},
				&messaging_api.FlexBox{
					Layout: messaging_api.FlexBoxLAYOUT_HORIZONTAL,
					Margin: "lg",
					Contents: []messaging_api.FlexComponentInterface{
						&messaging_api.FlexText{
							Text:  "รายรับรวม",
							Size:  "xs",
							Color: "#27AE60",
							Flex:  1,
						},
						&messaging_api.FlexText{
							Text:  fmt.Sprintf("%s", formatNumber(balance.TotalIncome)),
							Size:  "xs",
							Color: "#27AE60",
							Align: messaging_api.FlexTextALIGN_END,
							Flex:  1,
						},
					},
				},
				&messaging_api.FlexBox{
					Layout: messaging_api.FlexBoxLAYOUT_HORIZONTAL,
					Margin: "sm",
					Contents: []messaging_api.FlexComponentInterface{
						&messaging_api.FlexText{
							Text:  "รายจ่ายรวม",
							Size:  "xs",
							Color: "#E74C3C",
							Flex:  1,
						},
						&messaging_api.FlexText{
							Text:  fmt.Sprintf("%s", formatNumber(balance.TotalExpense)),
							Size:  "xs",
							Color: "#E74C3C",
							Align: messaging_api.FlexTextALIGN_END,
							Flex:  1,
						},
					},
				},
			},
		},
	}
}

func (h *LineWebhookHandler) replyUpdatedTransaction(replyToken, userID string, tx *services.Transaction, message string, txID string) {
	ctx := context.Background()

	// Get balance by payment type for detailed view
	balances, _ := h.mongo.GetBalanceByPaymentType(ctx, userID)

	typeText := "💸 รายจ่าย"
	typeColor := "#E74C3C"
	if tx.Type == 1 {
		typeText = "💰 รายรับ"
		typeColor = "#27AE60"
	}

	// Payment method text
	paymentText := "💵 เงินสด"
	switch tx.UseType {
	case 1:
		paymentText = "💳 บัตรเครดิต"
		if tx.CreditCardName != "" {
			paymentText += " " + tx.CreditCardName
		}
	case 2:
		paymentText = "🏦 ธนาคาร"
		if tx.BankName != "" {
			paymentText += " " + tx.BankName
		}
	}

	// Ensure description is not empty
	description := tx.Description
	if description == "" {
		description = tx.Category
	}
	if description == "" {
		description = "-"
	}

	// Build body contents
	bodyContents := []messaging_api.FlexComponentInterface{
		&messaging_api.FlexText{
			Text:  message,
			Size:  "sm",
			Color: "#666666",
			Wrap:  true,
		},
		&messaging_api.FlexSeparator{Margin: "md"},
		&messaging_api.FlexText{
			Text:   typeText,
			Size:   "sm",
			Color:  typeColor,
			Margin: "md",
		},
		&messaging_api.FlexText{
			Text:   fmt.Sprintf("%s", formatNumber(tx.Amount)),
			Size:   "xl",
			Color:  typeColor,
			Weight: messaging_api.FlexTextWEIGHT_BOLD,
		},
		&messaging_api.FlexText{
			Text:   description,
			Size:   "sm",
			Color:  "#888888",
			Margin: "sm",
		},
		&messaging_api.FlexText{
			Text:   paymentText,
			Size:   "sm",
			Color:  "#888888",
			Margin: "sm",
		},
	}

	// Add detailed balance section
	if len(balances) > 0 {
		// Calculate totals by type
		cashBalance := &services.PaymentBalance{}
		bankBalances := make(map[string]*services.PaymentBalance)
		cardBalances := make(map[string]*services.PaymentBalance)
		netWorth := 0.0

		for _, pb := range balances {
			switch pb.UseType {
			case 0:
				cashBalance.TotalIncome += pb.TotalIncome
				cashBalance.TotalExpense += pb.TotalExpense
				cashBalance.Balance += pb.Balance
			case 1:
				key := pb.CreditCardName
				if key == "" {
					key = "บัตรเครดิต"
				}
				if _, exists := cardBalances[key]; !exists {
					cardBalances[key] = &services.PaymentBalance{CreditCardName: key}
				}
				cardBalances[key].Balance += pb.Balance
			case 2:
				key := pb.BankName
				if key == "" {
					key = "ธนาคาร"
				}
				if _, exists := bankBalances[key]; !exists {
					bankBalances[key] = &services.PaymentBalance{BankName: key}
				}
				bankBalances[key].Balance += pb.Balance
			}
		}

		netWorth = cashBalance.Balance
		for _, pb := range bankBalances {
			netWorth += pb.Balance
		}
		for _, pb := range cardBalances {
			netWorth += pb.Balance
		}

		// Add balance header
		bodyContents = append(bodyContents,
			&messaging_api.FlexSeparator{Margin: "lg"},
			&messaging_api.FlexBox{
				Layout: messaging_api.FlexBoxLAYOUT_HORIZONTAL,
				Margin: "lg",
				Contents: []messaging_api.FlexComponentInterface{
					&messaging_api.FlexText{
						Text:   "💰 ยอดคงเหลือทั้งหมด",
						Size:   "md",
						Color:  "#333333",
						Weight: messaging_api.FlexTextWEIGHT_BOLD,
						Flex:   3,
					},
					&messaging_api.FlexText{
						Text:   formatBalanceText(netWorth),
						Size:   "lg",
						Color:  getBalanceColor(netWorth),
						Weight: messaging_api.FlexTextWEIGHT_BOLD,
						Align:  messaging_api.FlexTextALIGN_END,
						Flex:   2,
					},
				},
			},
		)

		// Cash balance
		if cashBalance.TotalIncome > 0 || cashBalance.TotalExpense > 0 {
			bodyContents = append(bodyContents,
				&messaging_api.FlexBox{
					Layout: messaging_api.FlexBoxLAYOUT_HORIZONTAL,
					Margin: "md",
					Contents: []messaging_api.FlexComponentInterface{
						&messaging_api.FlexText{
							Text:  "   💵 เงินสด",
							Size:  "sm",
							Color: "#555555",
							Flex:  3,
						},
						&messaging_api.FlexText{
							Text:   formatBalanceText(cashBalance.Balance),
							Size:   "sm",
							Color:  getBalanceColor(cashBalance.Balance),
							Weight: messaging_api.FlexTextWEIGHT_BOLD,
							Align:  messaging_api.FlexTextALIGN_END,
							Flex:   2,
						},
					},
				},
			)
		}

		// Bank balances
		for name, pb := range bankBalances {
			bodyContents = append(bodyContents,
				&messaging_api.FlexBox{
					Layout: messaging_api.FlexBoxLAYOUT_HORIZONTAL,
					Margin: "sm",
					Contents: []messaging_api.FlexComponentInterface{
						&messaging_api.FlexText{
							Text:  "   🏦 " + name,
							Size:  "sm",
							Color: "#555555",
							Flex:  3,
						},
						&messaging_api.FlexText{
							Text:   formatBalanceText(pb.Balance),
							Size:   "sm",
							Color:  getBalanceColor(pb.Balance),
							Weight: messaging_api.FlexTextWEIGHT_BOLD,
							Align:  messaging_api.FlexTextALIGN_END,
							Flex:   2,
						},
					},
				},
			)
		}

		// Credit card balances
		for name, pb := range cardBalances {
			label := name
			if pb.Balance < 0 {
				label += " (หนี้)"
			}
			bodyContents = append(bodyContents,
				&messaging_api.FlexBox{
					Layout: messaging_api.FlexBoxLAYOUT_HORIZONTAL,
					Margin: "sm",
					Contents: []messaging_api.FlexComponentInterface{
						&messaging_api.FlexText{
							Text:  "   💳 " + label,
							Size:  "sm",
							Color: "#555555",
							Flex:  3,
						},
						&messaging_api.FlexText{
							Text:   formatBalanceText(pb.Balance),
							Size:   "sm",
							Color:  getBalanceColor(pb.Balance),
							Weight: messaging_api.FlexTextWEIGHT_BOLD,
							Align:  messaging_api.FlexTextALIGN_END,
							Flex:   2,
						},
					},
				},
			)
		}
	}

	flexMessage := messaging_api.FlexMessage{
		AltText: fmt.Sprintf("แก้ไขแล้ว: %s บาท", formatNumber(tx.Amount)),
		Contents: &messaging_api.FlexBubble{
			Size: messaging_api.FlexBubbleSIZE_KILO,
			Header: &messaging_api.FlexBox{
				Layout:          messaging_api.FlexBoxLAYOUT_VERTICAL,
				BackgroundColor: "#FF9800",
				PaddingAll:      "15px",
				Contents: []messaging_api.FlexComponentInterface{
					&messaging_api.FlexText{
						Text:   "✏️ แก้ไขแล้ว",
						Weight: messaging_api.FlexTextWEIGHT_BOLD,
						Size:   "md",
						Color:  "#FFFFFF",
					},
				},
			},
			Body: &messaging_api.FlexBox{
				Layout:     messaging_api.FlexBoxLAYOUT_VERTICAL,
				PaddingAll: "15px",
				Contents:   bodyContents,
			},
		},
		QuickReply: &messaging_api.QuickReply{
			Items: []messaging_api.QuickReplyItem{
				{
					Action: &messaging_api.PostbackAction{
						Label: "✏️ แก้ไขอีก",
						Data:  fmt.Sprintf("action=edit_request&txid=%s", txID),
					},
				},
				{
					Action: &messaging_api.PostbackAction{
						Label: "🗑️ ลบรายการนี้",
						Data:  "action=delete&txid=" + txID,
					},
				},
			},
		},
	}

	_, err := h.bot.ReplyMessage(&messaging_api.ReplyMessageRequest{
		ReplyToken: replyToken,
		Messages:   []messaging_api.MessageInterface{flexMessage},
	})
	if err != nil {
		log.Printf("Failed to send updated transaction: %v", err)
	}
}

func (h *LineWebhookHandler) handlePostback(ctx context.Context, event webhook.PostbackEvent) {
	userID := h.getUserID(event.Source)
	replyToken := event.ReplyToken
	if userID == "" {
		log.Println("Failed to get user ID from postback")
		return
	}

	data := event.Postback.Data
	log.Printf("Postback data: %s", data)

	// Parse postback data
	params := make(map[string]string)
	for _, pair := range strings.Split(data, "&") {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) == 2 {
			params[kv[0]] = kv[1]
		}
	}

	action := params["action"]

	switch action {
	case "delete":
		txID := params["txid"]
		if txID == "" {
			h.replyText(replyToken, "ไม่พบรหัสรายการ")
			return
		}

		err := h.mongo.DeleteTransaction(ctx, userID, txID)
		if err != nil {
			log.Printf("Failed to delete transaction: %v", err)
			h.replyText(replyToken, "ไม่สามารถลบรายการได้")
			return
		}

		// Get updated balance from payment types (accurate)
		balances, _ := h.mongo.GetBalanceByPaymentType(ctx, userID)
		var grandTotal float64
		for _, b := range balances {
			grandTotal += b.Balance
		}

		// Reply with Flex showing delete confirmation and balance
		h.replyDeleteConfirmFlex(replyToken, grandTotal)

	case "delete_all":
		txIDs := params["txids"]
		if txIDs == "" {
			h.replyText(replyToken, "ไม่พบรหัสรายการ")
			return
		}

		ids := strings.Split(txIDs, ",")
		deletedCount := 0
		for _, txID := range ids {
			if txID == "" {
				continue
			}
			err := h.mongo.DeleteTransaction(ctx, userID, txID)
			if err != nil {
				log.Printf("Failed to delete transaction %s: %v", txID, err)
				continue
			}
			deletedCount++
		}

		// Get updated balance
		balanceText := h.getBalanceText(ctx, userID)
		h.replyText(replyToken, fmt.Sprintf("🗑️ ลบ %d รายการเรียบร้อยแล้ว\n\n%s", deletedCount, balanceText))

	case "delete_transfer":
		transferID := params["transfer_id"]
		if transferID == "" {
			h.replyText(replyToken, "ไม่พบรหัสการโอน")
			return
		}

		err := h.mongo.DeleteTransfer(ctx, userID, transferID)
		if err != nil {
			log.Printf("Failed to delete transfer: %v", err)
			h.replyText(replyToken, "ไม่สามารถยกเลิกการโอนได้")
			return
		}

		// Get updated balance
		balanceText := h.getBalanceText(ctx, userID)
		h.replyText(replyToken, fmt.Sprintf("🗑️ ยกเลิกการโอนเรียบร้อยแล้ว\n\n%s", balanceText))

	case "edit_request":
		// Handle edit request - guide user how to edit
		// We don't need txID here as the user will type the edit command naturally
		// But keeping it in data is good for future context if we implement stateful conversation
		h.replyText(replyToken, "✏️ หากต้องการแก้ไข ให้พิมพ์บอกได้เลยค่ะ\nเช่น \"แก้เป็นค่าอาหาร 500 บาท\" หรือ \"เปลี่ยนเป็นบัตรเครดิต\"")

	case "slip_income", "slip_expense":
		// Handle slip type selection - ask for category
		key := params["key"]
		if key == "" {
			h.replyText(replyToken, "ข้อมูลสลิปหมดอายุ กรุณาส่งรูปใหม่")
			return
		}

		// Verify slip data exists
		_, err := h.mongo.GetTempData(ctx, key)
		if err != nil {
			log.Printf("Failed to get slip data: %v", err)
			h.replyText(replyToken, "ข้อมูลสลิปหมดอายุ กรุณาส่งรูปใหม่")
			return
		}

		// Determine type
		txType := "income"
		typeText := "รายรับ"
		if action == "slip_expense" {
			txType = "expense"
			typeText = "รายจ่าย"
		}

		// Save pending state so user can type category instead of using Quick Reply
		pendingKey := fmt.Sprintf("slip_pending_%s", userID)
		pendingData := fmt.Sprintf(`{"slip_key":"%s","type":"%s"}`, key, txType)
		h.mongo.SaveTempData(ctx, pendingKey, pendingData, 10*time.Minute)

		// Build category quick replies based on type
		var quickItems []messaging_api.QuickReplyItem
		if action == "slip_income" {
			categories := []string{"เงินเดือน", "โบนัส", "รายได้เสริม", "เงินคืน", "ของขวัญ", "อื่นๆ"}
			for _, cat := range categories {
				quickItems = append(quickItems, messaging_api.QuickReplyItem{
					Action: &messaging_api.PostbackAction{
						Label: cat,
						Data:  fmt.Sprintf("action=slip_save&key=%s&type=income&category=%s", key, cat),
					},
				})
			}
		} else {
			categories := []string{"โอนเงิน", "ค่าสินค้า", "ค่าบริการ", "ค่าอาหาร", "ค่าเดินทาง", "อื่นๆ"}
			for _, cat := range categories {
				quickItems = append(quickItems, messaging_api.QuickReplyItem{
					Action: &messaging_api.PostbackAction{
						Label: cat,
						Data:  fmt.Sprintf("action=slip_save&key=%s&type=expense&category=%s", key, cat),
					},
				})
			}
		}

		_, err = h.bot.ReplyMessage(&messaging_api.ReplyMessageRequest{
			ReplyToken: replyToken,
			Messages: []messaging_api.MessageInterface{
				messaging_api.TextMessage{
					Text: fmt.Sprintf("✅ เลือก %s แล้ว\n\nเป็นค่าอะไรคะ? (เลือกหรือพิมพ์ได้เลย)", typeText),
					QuickReply: &messaging_api.QuickReply{
						Items: quickItems,
					},
				},
			},
		})
		if err != nil {
			log.Printf("Failed to send category selection: %v", err)
		}

	case "slip_save":
		// Final save of slip transaction
		key := params["key"]
		txType := params["type"]
		category := params["category"]

		if key == "" {
			h.replyText(replyToken, "ข้อมูลสลิปหมดอายุ กรุณาส่งรูปใหม่")
			return
		}

		// Get slip data from temp storage
		slipJSON, err := h.mongo.GetTempData(ctx, key)
		if err != nil {
			log.Printf("Failed to get slip data: %v", err)
			h.replyText(replyToken, "ข้อมูลสลิปหมดอายุ กรุณาส่งรูปใหม่")
			return
		}

		// Parse slip data
		var slip services.TransactionData
		if err := json.Unmarshal([]byte(slipJSON), &slip); err != nil {
			log.Printf("Failed to parse slip data: %v", err)
			h.replyText(replyToken, "เกิดข้อผิดพลาด กรุณาส่งรูปใหม่")
			return
		}

		// Set type and category based on user choice
		slip.Type = txType
		slip.Category = category
		if txType == "income" {
			slip.Description = fmt.Sprintf("รับโอนจาก %s (%s) - %s", slip.FromName, slip.FromBank, category)
			slip.BankName = slip.ToBank
		} else {
			slip.Description = fmt.Sprintf("โอนให้ %s (%s) - %s", slip.ToName, slip.ToBank, category)
			slip.BankName = slip.FromBank
		}
		slip.UseType = 2 // Bank transfer

		// Delete temp data (slip key and pending state)
		h.mongo.DeleteTempData(ctx, key)
		pendingKey := fmt.Sprintf("slip_pending_%s", userID)
		h.mongo.DeleteTempData(ctx, pendingKey)

		// Save transaction and reply with flex
		h.replyTransactionFlex(replyToken, userID, &slip)

	default:
		log.Printf("Unknown postback action: %s", action)
	}
}

// replyBalanceByPaymentType shows balance breakdown by payment type with total assets
func (h *LineWebhookHandler) replyBalanceByPaymentType(replyToken, userID string) {
	ctx := context.Background()

	// Get balance by payment type
	balances, err := h.mongo.GetBalanceByPaymentType(ctx, userID)
	if err != nil || len(balances) == 0 {
		h.replyText(replyToken, "ยังไม่มีรายการค่ะ")
		return
	}

	// Get distinct payment methods for quick reply buttons
	banks, creditCards, _ := h.mongo.GetDistinctPaymentMethods(ctx, userID)

	// Group by usetype and calculate totals
	// การคำนวณ: balance = sum(amount * type) โดย type=1 คือ income, type=-1 คือ expense
	cashBalance := &services.PaymentBalance{}
	bankBalances := make(map[string]*services.PaymentBalance)
	cardBalances := make(map[string]*services.PaymentBalance)

	for _, pb := range balances {
		switch pb.UseType {
		case 0: // Cash
			cashBalance.TotalIncome += pb.TotalIncome
			cashBalance.TotalExpense += pb.TotalExpense
			cashBalance.Balance += pb.Balance // Balance คำนวณมาแล้ว = sum(amount * type)
		case 1: // Credit Card
			key := pb.CreditCardName
			if key == "" {
				key = "บัตรเครดิต"
			}
			if _, exists := cardBalances[key]; !exists {
				cardBalances[key] = &services.PaymentBalance{CreditCardName: key}
			}
			cardBalances[key].TotalIncome += pb.TotalIncome
			cardBalances[key].TotalExpense += pb.TotalExpense
			cardBalances[key].Balance += pb.Balance
		case 2: // Bank
			key := pb.BankName
			if key == "" {
				key = "ธนาคาร"
			}
			if _, exists := bankBalances[key]; !exists {
				bankBalances[key] = &services.PaymentBalance{BankName: key}
			}
			bankBalances[key].TotalIncome += pb.TotalIncome
			bankBalances[key].TotalExpense += pb.TotalExpense
			bankBalances[key].Balance += pb.Balance
		}
	}

	// Calculate total net worth (sum of all balances)
	netWorth := cashBalance.Balance
	for _, pb := range bankBalances {
		netWorth += pb.Balance
	}
	for _, pb := range cardBalances {
		netWorth += pb.Balance // บัตรเครดิต: ใช้จ่าย = ติดลบ, รายรับ = บวก
	}

	// Build the flex message
	var bodyContents []messaging_api.FlexComponentInterface

	// Total Assets Section
	netWorthText := fmt.Sprintf("%s", formatNumber(netWorth))
	if netWorth < 0 {
		netWorthText = fmt.Sprintf("-%s", formatNumber(-netWorth))
	}
	bodyContents = append(bodyContents,
		&messaging_api.FlexText{
			Text:   "ทรัพย์สินทั้งหมด",
			Size:   "sm",
			Color:  "#888888",
			Weight: messaging_api.FlexTextWEIGHT_BOLD,
		},
		&messaging_api.FlexText{
			Text:   netWorthText,
			Size:   "xl",
			Color:  getBalanceColor(netWorth),
			Weight: messaging_api.FlexTextWEIGHT_BOLD,
			Margin: "sm",
		},
		&messaging_api.FlexSeparator{Margin: "lg"},
	)

	// Cash Section
	if cashBalance.TotalIncome > 0 || cashBalance.TotalExpense > 0 {
		cashBalanceText := formatBalanceText(cashBalance.Balance)
		bodyContents = append(bodyContents,
			&messaging_api.FlexBox{
				Layout: messaging_api.FlexBoxLAYOUT_HORIZONTAL,
				Margin: "lg",
				Contents: []messaging_api.FlexComponentInterface{
					&messaging_api.FlexText{
						Text:   "💵 เงินสด",
						Size:   "lg",
						Color:  "#333333",
						Weight: messaging_api.FlexTextWEIGHT_BOLD,
						Flex:   3,
					},
					&messaging_api.FlexText{
						Text:   cashBalanceText,
						Size:   "lg",
						Color:  getBalanceColor(cashBalance.Balance),
						Weight: messaging_api.FlexTextWEIGHT_BOLD,
						Align:  messaging_api.FlexTextALIGN_END,
						Flex:   2,
					},
				},
			},
			&messaging_api.FlexBox{
				Layout: messaging_api.FlexBoxLAYOUT_HORIZONTAL,
				Margin: "sm",
				Contents: []messaging_api.FlexComponentInterface{
					&messaging_api.FlexText{
						Text:  fmt.Sprintf("   +%s", formatNumber(cashBalance.TotalIncome)),
						Size:  "sm",
						Color: "#27AE60",
						Flex:  1,
					},
					&messaging_api.FlexText{
						Text:  fmt.Sprintf("-%s", formatNumber(cashBalance.TotalExpense)),
						Size:  "sm",
						Color: "#E74C3C",
						Align: messaging_api.FlexTextALIGN_END,
						Flex:  1,
					},
				},
			},
		)
	}

	// Bank Section
	if len(bankBalances) > 0 {
		bodyContents = append(bodyContents,
			&messaging_api.FlexSeparator{Margin: "lg"},
			&messaging_api.FlexText{
				Text:   "🏦 ธนาคาร",
				Size:   "lg",
				Color:  "#2196F3",
				Weight: messaging_api.FlexTextWEIGHT_BOLD,
				Margin: "lg",
			},
		)

		for name, pb := range bankBalances {
			bodyContents = append(bodyContents,
				&messaging_api.FlexBox{
					Layout: messaging_api.FlexBoxLAYOUT_HORIZONTAL,
					Margin: "md",
					Contents: []messaging_api.FlexComponentInterface{
						&messaging_api.FlexText{
							Text:   "   " + name,
							Size:   "md",
							Color:  "#555555",
							Weight: messaging_api.FlexTextWEIGHT_BOLD,
							Flex:   3,
						},
						&messaging_api.FlexText{
							Text:   formatBalanceText(pb.Balance),
							Size:   "md",
							Color:  getBalanceColor(pb.Balance),
							Weight: messaging_api.FlexTextWEIGHT_BOLD,
							Align:  messaging_api.FlexTextALIGN_END,
							Flex:   2,
						},
					},
				},
				&messaging_api.FlexBox{
					Layout: messaging_api.FlexBoxLAYOUT_HORIZONTAL,
					Margin: "sm",
					Contents: []messaging_api.FlexComponentInterface{
						&messaging_api.FlexText{
							Text:  fmt.Sprintf("   +%s", formatNumber(pb.TotalIncome)),
							Size:  "sm",
							Color: "#27AE60",
							Flex:  1,
						},
						&messaging_api.FlexText{
							Text:  fmt.Sprintf("-%s", formatNumber(pb.TotalExpense)),
							Size:  "sm",
							Color: "#E74C3C",
							Align: messaging_api.FlexTextALIGN_END,
							Flex:  1,
						},
					},
				},
			)
		}
	}

	// Credit Card Section
	if len(cardBalances) > 0 {
		bodyContents = append(bodyContents,
			&messaging_api.FlexSeparator{Margin: "lg"},
			&messaging_api.FlexText{
				Text:   "💳 บัตรเครดิต (หนี้)",
				Size:   "lg",
				Color:  "#9C27B0",
				Weight: messaging_api.FlexTextWEIGHT_BOLD,
				Margin: "lg",
			},
		)

		for name, pb := range cardBalances {
			// Balance = sum(amount * type) -> ติดลบ = หนี้ค้างจ่าย, บวก = จ่ายเกินไป
			// แสดงเป็น "ค้างจ่าย" ถ้าติดลบ
			balanceText := formatBalanceText(pb.Balance)
			balanceLabel := ""
			if pb.Balance < 0 {
				balanceLabel = " (ค้างจ่าย)"
			}
			bodyContents = append(bodyContents,
				&messaging_api.FlexBox{
					Layout: messaging_api.FlexBoxLAYOUT_HORIZONTAL,
					Margin: "md",
					Contents: []messaging_api.FlexComponentInterface{
						&messaging_api.FlexText{
							Text:   "   " + name,
							Size:   "md",
							Color:  "#555555",
							Weight: messaging_api.FlexTextWEIGHT_BOLD,
							Flex:   3,
						},
						&messaging_api.FlexText{
							Text:   balanceText + balanceLabel,
							Size:   "md",
							Color:  getBalanceColor(pb.Balance),
							Weight: messaging_api.FlexTextWEIGHT_BOLD,
							Align:  messaging_api.FlexTextALIGN_END,
							Flex:   2,
						},
					},
				},
				&messaging_api.FlexBox{
					Layout: messaging_api.FlexBoxLAYOUT_HORIZONTAL,
					Margin: "sm",
					Contents: []messaging_api.FlexComponentInterface{
						&messaging_api.FlexText{
							Text:  fmt.Sprintf("   จ่ายแล้ว +%s", formatNumber(pb.TotalIncome)),
							Size:  "sm",
							Color: "#27AE60",
							Flex:  1,
						},
						&messaging_api.FlexText{
							Text:  fmt.Sprintf("ใช้จ่าย -%s", formatNumber(pb.TotalExpense)),
							Size:  "sm",
							Color: "#E74C3C",
							Align: messaging_api.FlexTextALIGN_END,
							Flex:  1,
						},
					},
				},
			)
		}
	}

	// Build quick reply items
	quickReplyItems := []messaging_api.QuickReplyItem{}

	// Add bank buttons
	for _, bank := range banks {
		if len(quickReplyItems) >= 10 {
			break
		}
		quickReplyItems = append(quickReplyItems, messaging_api.QuickReplyItem{
			Action: &messaging_api.MessageAction{
				Label: "🏦 " + truncateLabel(bank, 17),
				Text:  "ยอด " + bank,
			},
		})
	}

	// Add credit card buttons
	for _, cc := range creditCards {
		if len(quickReplyItems) >= 13 {
			break
		}
		quickReplyItems = append(quickReplyItems, messaging_api.QuickReplyItem{
			Action: &messaging_api.MessageAction{
				Label: "💳 " + truncateLabel(cc, 17),
				Text:  "ยอด " + cc,
			},
		})
	}

	flexMessage := messaging_api.FlexMessage{
		AltText: fmt.Sprintf("ทรัพย์สินทั้งหมด %s", formatNumber(netWorth)),
		Contents: &messaging_api.FlexBubble{
			Size: messaging_api.FlexBubbleSIZE_MEGA,
			Header: &messaging_api.FlexBox{
				Layout:          messaging_api.FlexBoxLAYOUT_VERTICAL,
				BackgroundColor: "#1E88E5",
				PaddingAll:      "20px",
				Contents: []messaging_api.FlexComponentInterface{
					&messaging_api.FlexText{
						Text:   "💰 สติสตางค์",
						Weight: messaging_api.FlexTextWEIGHT_BOLD,
						Size:   "lg",
						Color:  "#FFFFFF",
					},
					&messaging_api.FlexText{
						Text:   "สรุปทรัพย์สินและหนี้สิน",
						Size:   "sm",
						Color:  "#B3E5FC",
						Margin: "xs",
					},
				},
			},
			Body: &messaging_api.FlexBox{
				Layout:     messaging_api.FlexBoxLAYOUT_VERTICAL,
				PaddingAll: "20px",
				Contents:   bodyContents,
			},
		},
	}

	if len(quickReplyItems) > 0 {
		flexMessage.QuickReply = &messaging_api.QuickReply{
			Items: quickReplyItems,
		}
	}

	_, err = h.bot.ReplyMessage(&messaging_api.ReplyMessageRequest{
		ReplyToken: replyToken,
		Messages:   []messaging_api.MessageInterface{flexMessage},
	})
	if err != nil {
		log.Printf("Failed to send balance by payment type: %v", err)
	}
}

// getBalanceText returns balance summary text for combining with other messages
func (h *LineWebhookHandler) getBalanceText(ctx context.Context, userID string) string {
	balances, err := h.mongo.GetBalanceByPaymentType(ctx, userID)
	if err != nil || len(balances) == 0 {
		return ""
	}

	// Group by usetype and calculate totals
	cashBalance := 0.0
	bankBalances := make(map[string]float64)
	cardBalances := make(map[string]float64)

	for _, pb := range balances {
		switch pb.UseType {
		case 0: // Cash
			cashBalance += pb.Balance
		case 1: // Credit Card
			key := pb.CreditCardName
			if key == "" {
				key = "บัตรเครดิต"
			}
			cardBalances[key] += pb.Balance
		case 2: // Bank
			key := pb.BankName
			if key == "" {
				key = "ธนาคาร"
			}
			bankBalances[key] += pb.Balance
		}
	}

	// Calculate net worth
	netWorth := cashBalance
	for _, bal := range bankBalances {
		netWorth += bal
	}
	for _, bal := range cardBalances {
		netWorth += bal
	}

	return fmt.Sprintf("💰 ยอดคงเหลือ: %s", formatBalanceText(netWorth))
}

func getBalanceColor(balance float64) string {
	if balance < 0 {
		return "#E74C3C"
	}
	return "#27AE60"
}

func formatBalanceText(balance float64) string {
	if balance < 0 {
		return fmt.Sprintf("-%s", formatNumber(-balance))
	}
	return fmt.Sprintf("%s", formatNumber(balance))
}

func formatNumber(n float64) string {
	if n < 0 {
		n = -n
	}
	// Format with commas
	s := fmt.Sprintf("%.2f", n)
	parts := strings.Split(s, ".")
	intPart := parts[0]
	decPart := parts[1]

	// Add commas
	var result []rune
	for i, r := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, r)
	}
	return string(result) + "." + decPart
}

func truncateLabel(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-2]) + ".."
}

// orDefault returns the string if not empty, otherwise returns the default value
func orDefault(s, defaultVal string) string {
	if strings.TrimSpace(s) == "" {
		return defaultVal
	}
	return s
}

// replyAnalysisFlex displays AI analysis with beautiful Flex Message
func (h *LineWebhookHandler) replyAnalysisFlex(replyToken, userID string, analysis *services.AnalysisData, message string) {
	// Build body contents
	var bodyContents []messaging_api.FlexComponentInterface

	// Summary section
	if analysis.Summary != "" {
		bodyContents = append(bodyContents,
			&messaging_api.FlexText{
				Text:   analysis.Summary,
				Size:   "md",
				Color:  "#333333",
				Wrap:   true,
				Weight: messaging_api.FlexTextWEIGHT_BOLD,
			},
			&messaging_api.FlexSeparator{Margin: "lg"},
		)
	}

	// Insights section
	if len(analysis.Insights) > 0 {
		bodyContents = append(bodyContents,
			&messaging_api.FlexText{
				Text:   "📊 รายละเอียด",
				Size:   "sm",
				Color:  "#888888",
				Margin: "lg",
			},
		)

		// Color palette for insights
		colors := []string{"#E74C3C", "#3498DB", "#27AE60", "#F39C12", "#9B59B6", "#1ABC9C"}

		for i, insight := range analysis.Insights {
			color := colors[i%len(colors)]

			// Build value+amount text
			valueText := ""
			if insight.Value != "" {
				valueText = insight.Value
			}
			if insight.Amount > 0 {
				if valueText != "" {
					valueText += " • "
				}
				valueText += fmt.Sprintf("%s", formatNumber(insight.Amount))
			}

			// Each insight as a vertical box with label on top, value below
			bodyContents = append(bodyContents,
				&messaging_api.FlexBox{
					Layout: messaging_api.FlexBoxLAYOUT_HORIZONTAL,
					Margin: "md",
					Contents: []messaging_api.FlexComponentInterface{
						&messaging_api.FlexText{
							Text:  insight.Label,
							Size:  "sm",
							Color: "#555555",
							Flex:  4,
							Wrap:  true,
						},
						&messaging_api.FlexText{
							Text:   valueText,
							Size:   "sm",
							Color:  color,
							Weight: messaging_api.FlexTextWEIGHT_BOLD,
							Align:  messaging_api.FlexTextALIGN_END,
							Flex:   3,
							Wrap:   true,
						},
					},
				},
			)
		}
	}

	// Advice section
	if analysis.Advice != "" {
		bodyContents = append(bodyContents,
			&messaging_api.FlexSeparator{Margin: "lg"},
			&messaging_api.FlexBox{
				Layout:          messaging_api.FlexBoxLAYOUT_VERTICAL,
				Margin:          "lg",
				BackgroundColor: "#FFF9E6",
				CornerRadius:    "8px",
				PaddingAll:      "12px",
				Contents: []messaging_api.FlexComponentInterface{
					&messaging_api.FlexText{
						Text:   "💡 คำแนะนำ",
						Size:   "sm",
						Color:  "#F39C12",
						Weight: messaging_api.FlexTextWEIGHT_BOLD,
					},
					&messaging_api.FlexText{
						Text:   analysis.Advice,
						Size:   "sm",
						Color:  "#666666",
						Wrap:   true,
						Margin: "sm",
					},
				},
			},
		)
	}

	// AI message at the bottom
	if message != "" && message != analysis.Summary {
		bodyContents = append(bodyContents,
			&messaging_api.FlexSeparator{Margin: "lg"},
			&messaging_api.FlexText{
				Text:   message,
				Size:   "sm",
				Color:  "#888888",
				Wrap:   true,
				Margin: "lg",
			},
		)
	}

	// Title for header
	title := analysis.Title
	if title == "" {
		title = "📈 วิเคราะห์การเงิน"
	}

	flexMessage := messaging_api.FlexMessage{
		AltText: title,
		Contents: &messaging_api.FlexBubble{
			Size: messaging_api.FlexBubbleSIZE_GIGA,
			Header: &messaging_api.FlexBox{
				Layout:          messaging_api.FlexBoxLAYOUT_VERTICAL,
				BackgroundColor: "#00B900",
				PaddingAll:      "20px",
				Contents: []messaging_api.FlexComponentInterface{
					&messaging_api.FlexText{
						Text:  "🤖 สติสตางค์ AI",
						Size:  "sm",
						Color: "#FFFFFF",
					},
					&messaging_api.FlexText{
						Text:   title,
						Weight: messaging_api.FlexTextWEIGHT_BOLD,
						Size:   "xl",
						Color:  "#FFFFFF",
						Margin: "sm",
						Wrap:   true,
					},
				},
			},
			Body: &messaging_api.FlexBox{
				Layout:     messaging_api.FlexBoxLAYOUT_VERTICAL,
				PaddingAll: "20px",
				Contents:   bodyContents,
			},
		},
		QuickReply: &messaging_api.QuickReply{
			Items: []messaging_api.QuickReplyItem{
				{Action: &messaging_api.MessageAction{Label: "💰 ดูยอดคงเหลือ", Text: "ยอดคงเหลือ"}},
				{Action: &messaging_api.MessageAction{Label: "📊 สรุป 7 วัน", Text: "สรุป 7 วัน"}},
				{Action: &messaging_api.MessageAction{Label: "📈 วิเคราะห์เพิ่ม", Text: "แนะนำการออม"}},
			},
		},
	}

	_, err := h.bot.ReplyMessage(&messaging_api.ReplyMessageRequest{
		ReplyToken: replyToken,
		Messages:   []messaging_api.MessageInterface{flexMessage},
	})
	if err != nil {
		log.Printf("Failed to send analysis flex: %v", err)
	}
}

// replyBudgetFlex displays budget setting confirmation with Flex Message
func (h *LineWebhookHandler) replyBudgetFlex(replyToken, userID string, category string, amount float64, message string) {
	bgCtx := context.Background()

	// Get current spending for this category
	spending, _ := h.mongo.GetMonthlySpendingByCategory(bgCtx, userID)
	spent := spending[category]
	remaining := amount - spent
	percentage := 0.0
	if amount > 0 {
		percentage = (spent / amount) * 100
	}

	// Status emoji
	statusEmoji := "✅"
	statusColor := "#27AE60"
	if spent > amount {
		statusEmoji = "🔴"
		statusColor = "#E74C3C"
	} else if percentage >= 80 {
		statusEmoji = "🟡"
		statusColor = "#F39C12"
	}

	flexMessage := messaging_api.FlexMessage{
		AltText: fmt.Sprintf("ตั้งงบ %s %s บาท", category, formatNumber(amount)),
		Contents: &messaging_api.FlexBubble{
			Size: messaging_api.FlexBubbleSIZE_KILO,
			Header: &messaging_api.FlexBox{
				Layout:          messaging_api.FlexBoxLAYOUT_VERTICAL,
				BackgroundColor: "#9B59B6",
				PaddingAll:      "15px",
				Contents: []messaging_api.FlexComponentInterface{
					&messaging_api.FlexText{
						Text:  "📋 ตั้งงบประมาณ",
						Size:  "sm",
						Color: "#FFFFFF",
					},
					&messaging_api.FlexText{
						Text:   fmt.Sprintf("หมวด: %s", category),
						Weight: messaging_api.FlexTextWEIGHT_BOLD,
						Size:   "lg",
						Color:  "#FFFFFF",
						Margin: "sm",
					},
				},
			},
			Body: &messaging_api.FlexBox{
				Layout:     messaging_api.FlexBoxLAYOUT_VERTICAL,
				PaddingAll: "15px",
				Contents: []messaging_api.FlexComponentInterface{
					&messaging_api.FlexBox{
						Layout: messaging_api.FlexBoxLAYOUT_HORIZONTAL,
						Contents: []messaging_api.FlexComponentInterface{
							&messaging_api.FlexText{
								Text:  "งบประมาณ",
								Size:  "sm",
								Color: "#888888",
								Flex:  3,
							},
							&messaging_api.FlexText{
								Text:   fmt.Sprintf("%s", formatNumber(amount)),
								Size:   "sm",
								Weight: messaging_api.FlexTextWEIGHT_BOLD,
								Align:  messaging_api.FlexTextALIGN_END,
								Flex:   2,
							},
						},
					},
					&messaging_api.FlexBox{
						Layout: messaging_api.FlexBoxLAYOUT_HORIZONTAL,
						Margin: "sm",
						Contents: []messaging_api.FlexComponentInterface{
							&messaging_api.FlexText{
								Text:  "ใช้ไปแล้ว",
								Size:  "sm",
								Color: "#888888",
								Flex:  3,
							},
							&messaging_api.FlexText{
								Text:   fmt.Sprintf("%s (%.0f%%)", formatNumber(spent), percentage),
								Size:   "sm",
								Color:  statusColor,
								Weight: messaging_api.FlexTextWEIGHT_BOLD,
								Align:  messaging_api.FlexTextALIGN_END,
								Flex:   2,
							},
						},
					},
					&messaging_api.FlexBox{
						Layout: messaging_api.FlexBoxLAYOUT_HORIZONTAL,
						Margin: "sm",
						Contents: []messaging_api.FlexComponentInterface{
							&messaging_api.FlexText{
								Text:  "คงเหลือ",
								Size:  "sm",
								Color: "#888888",
								Flex:  3,
							},
							&messaging_api.FlexText{
								Text:   fmt.Sprintf("%s %s", statusEmoji, formatNumber(remaining)),
								Size:   "sm",
								Weight: messaging_api.FlexTextWEIGHT_BOLD,
								Align:  messaging_api.FlexTextALIGN_END,
								Flex:   2,
							},
						},
					},
					&messaging_api.FlexSeparator{Margin: "lg"},
					&messaging_api.FlexText{
						Text:   message,
						Size:   "sm",
						Color:  "#666666",
						Wrap:   true,
						Margin: "lg",
					},
				},
			},
		},
		QuickReply: &messaging_api.QuickReply{
			Items: []messaging_api.QuickReplyItem{
				{Action: &messaging_api.MessageAction{Label: "📊 ดูงบทั้งหมด", Text: "ดูงบประมาณ"}},
				{Action: &messaging_api.MessageAction{Label: "➕ ตั้งงบเพิ่ม", Text: "ตั้งงบ"}},
				{Action: &messaging_api.MessageAction{Label: "💰 ดูยอด", Text: "ยอดคงเหลือ"}},
			},
		},
	}

	_, err := h.bot.ReplyMessage(&messaging_api.ReplyMessageRequest{
		ReplyToken: replyToken,
		Messages:   []messaging_api.MessageInterface{flexMessage},
	})
	if err != nil {
		log.Printf("Failed to send budget flex: %v", err)
	}
}

// replyAndSendFile replies with text and then sends file download link
func (h *LineWebhookHandler) replyAndSendFile(replyToken, userID, message string, data []byte, filename string, mimeType string) {
	fileSize := len(data) / 1024 // KB
	var fileType string
	if strings.Contains(mimeType, "pdf") {
		fileType = "PDF"
	} else {
		fileType = "Excel"
	}

	// Check if Firebase is configured
	if h.firebase == nil {
		log.Println("Firebase not configured, cannot upload file")
		h.replyText(replyToken, "❌ ระบบยังไม่พร้อมส่งไฟล์ค่ะ\n\nกรุณาติดต่อผู้ดูแลระบบ")
		return
	}

	// Upload to Firebase Cloud Storage
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	downloadURL, err := h.firebase.UploadFile(ctx, data, filename, mimeType)
	if err != nil {
		log.Printf("Failed to upload file to Firebase: %v", err)
		h.replyText(replyToken, "❌ ไม่สามารถอัปโหลดไฟล์ได้\n\nกรุณาลองใหม่อีกครั้งค่ะ")
		return
	}

	// Reply with Flex Message containing download button
	h.replyFileDownloadFlex(replyToken, userID, message, fileType, filename, fileSize, downloadURL)
}

// replyFileDownloadFlex replies with a Flex Message with download button (uses ReplyMessage)
func (h *LineWebhookHandler) replyFileDownloadFlex(replyToken, userID, message, fileType, filename string, fileSize int, downloadURL string) {
	emoji := "📊"
	if fileType == "PDF" {
		emoji = "📄"
	}

	flexMessage := &messaging_api.FlexMessage{
		AltText: fmt.Sprintf("ไฟล์ %s พร้อมดาวน์โหลด", fileType),
		Contents: &messaging_api.FlexBubble{
			Size: "kilo",
			Header: &messaging_api.FlexBox{
				Layout:          messaging_api.FlexBoxLAYOUT_VERTICAL,
				BackgroundColor: "#00B900",
				PaddingAll:      "15px",
				Contents: []messaging_api.FlexComponentInterface{
					&messaging_api.FlexText{
						Text:   fmt.Sprintf("%s ไฟล์ %s พร้อมแล้ว!", emoji, fileType),
						Color:  "#FFFFFF",
						Weight: messaging_api.FlexTextWEIGHT_BOLD,
						Size:   "lg",
					},
				},
			},
			Body: &messaging_api.FlexBox{
				Layout:     messaging_api.FlexBoxLAYOUT_VERTICAL,
				PaddingAll: "15px",
				Spacing:    "md",
				Contents: []messaging_api.FlexComponentInterface{
					&messaging_api.FlexText{
						Text:  message,
						Size:  "sm",
						Color: "#666666",
						Wrap:  true,
					},
					&messaging_api.FlexSeparator{Margin: "md"},
					&messaging_api.FlexBox{
						Layout:  messaging_api.FlexBoxLAYOUT_HORIZONTAL,
						Spacing: "sm",
						Contents: []messaging_api.FlexComponentInterface{
							&messaging_api.FlexText{
								Text:  "📁 ชื่อไฟล์:",
								Color: "#666666",
								Size:  "sm",
								Flex:  2,
							},
							&messaging_api.FlexText{
								Text: filename,
								Size: "sm",
								Flex: 3,
							},
						},
					},
					&messaging_api.FlexBox{
						Layout:  messaging_api.FlexBoxLAYOUT_HORIZONTAL,
						Spacing: "sm",
						Contents: []messaging_api.FlexComponentInterface{
							&messaging_api.FlexText{
								Text:  "📊 ขนาด:",
								Color: "#666666",
								Size:  "sm",
								Flex:  2,
							},
							&messaging_api.FlexText{
								Text: fmt.Sprintf("%d KB", fileSize),
								Size: "sm",
								Flex: 3,
							},
						},
					},
					&messaging_api.FlexSeparator{Margin: "lg"},
					&messaging_api.FlexText{
						Text:  "⚠️ ลิงก์จะหมดอายุหลังดาวน์โหลดครั้งแรก หรือใน 14 วัน",
						Color: "#FF6B6B",
						Size:  "xs",
						Wrap:  true,
					},
				},
			},
			Footer: &messaging_api.FlexBox{
				Layout:     messaging_api.FlexBoxLAYOUT_VERTICAL,
				PaddingAll: "15px",
				Contents: []messaging_api.FlexComponentInterface{
					&messaging_api.FlexButton{
						Style:  messaging_api.FlexButtonSTYLE_PRIMARY,
						Color:  "#00B900",
						Height: "sm",
						Action: &messaging_api.UriAction{
							Label: fmt.Sprintf("📥 ดาวน์โหลด %s", fileType),
							Uri:   downloadURL,
						},
					},
				},
			},
		},
	}

	_, err := h.bot.ReplyMessage(&messaging_api.ReplyMessageRequest{
		ReplyToken: replyToken,
		Messages:   []messaging_api.MessageInterface{flexMessage},
	})
	if err != nil {
		log.Printf("Failed to send file download flex: %v", err)
	}
}

// replyChartFlex displays spending chart as Flex Message with visual bars
func (h *LineWebhookHandler) replyChartFlex(replyToken, userID string) {
	bgCtx := context.Background()

	// Get spending data
	chartData, total, err := h.export.GetCategorySpendingForChart(bgCtx, userID)
	if err != nil || len(chartData) == 0 {
		h.replyText(replyToken, "ไม่มีข้อมูลรายจ่ายเดือนนี้ค่ะ")
		return
	}

	// Build chart items
	var chartItems []messaging_api.FlexComponentInterface

	// Sort by amount (highest first) - simple bubble sort
	for i := 0; i < len(chartData); i++ {
		for j := i + 1; j < len(chartData); j++ {
			if chartData[j].Amount > chartData[i].Amount {
				chartData[i], chartData[j] = chartData[j], chartData[i]
			}
		}
	}

	// Add chart bars (max 8 categories)
	maxItems := 8
	if len(chartData) < maxItems {
		maxItems = len(chartData)
	}

	for i := 0; i < maxItems; i++ {
		item := chartData[i]

		// Calculate bar width percentage (max 100%)
		barWidth := int(item.Percentage)
		if barWidth < 5 {
			barWidth = 5 // minimum visible width
		}
		if barWidth > 100 {
			barWidth = 100
		}

		chartItems = append(chartItems,
			// Category label and percentage
			&messaging_api.FlexBox{
				Layout: messaging_api.FlexBoxLAYOUT_HORIZONTAL,
				Margin: "md",
				Contents: []messaging_api.FlexComponentInterface{
					&messaging_api.FlexText{
						Text:  item.Category,
						Size:  "sm",
						Color: "#555555",
						Flex:  4,
					},
					&messaging_api.FlexText{
						Text:  fmt.Sprintf("%s (%.0f%%)", formatNumber(item.Amount), item.Percentage),
						Size:  "xs",
						Color: "#888888",
						Align: messaging_api.FlexTextALIGN_END,
						Flex:  3,
					},
				},
			},
			// Bar visualization
			&messaging_api.FlexBox{
				Layout: messaging_api.FlexBoxLAYOUT_HORIZONTAL,
				Margin: "xs",
				Contents: []messaging_api.FlexComponentInterface{
					&messaging_api.FlexBox{
						Layout:          messaging_api.FlexBoxLAYOUT_VERTICAL,
						BackgroundColor: item.Color,
						Height:          "8px",
						CornerRadius:    "4px",
						Flex:            int32(barWidth),
						Contents:        []messaging_api.FlexComponentInterface{&messaging_api.FlexFiller{}},
					},
					&messaging_api.FlexBox{
						Layout:   messaging_api.FlexBoxLAYOUT_VERTICAL,
						Height:   "8px",
						Flex:     int32(100 - barWidth),
						Contents: []messaging_api.FlexComponentInterface{&messaging_api.FlexFiller{}},
					},
				},
			},
		)
	}

	// Add total
	chartItems = append(chartItems,
		&messaging_api.FlexSeparator{Margin: "lg"},
		&messaging_api.FlexBox{
			Layout: messaging_api.FlexBoxLAYOUT_HORIZONTAL,
			Margin: "lg",
			Contents: []messaging_api.FlexComponentInterface{
				&messaging_api.FlexText{
					Text:   "รวมทั้งหมด",
					Size:   "md",
					Weight: messaging_api.FlexTextWEIGHT_BOLD,
					Flex:   4,
				},
				&messaging_api.FlexText{
					Text:   fmt.Sprintf("%s", formatNumber(total)),
					Size:   "md",
					Weight: messaging_api.FlexTextWEIGHT_BOLD,
					Color:  "#E74C3C",
					Align:  messaging_api.FlexTextALIGN_END,
					Flex:   3,
				},
			},
		},
	)

	flexMessage := messaging_api.FlexMessage{
		AltText: "กราฟสัดส่วนรายจ่าย",
		Contents: &messaging_api.FlexBubble{
			Size: messaging_api.FlexBubbleSIZE_GIGA,
			Header: &messaging_api.FlexBox{
				Layout:          messaging_api.FlexBoxLAYOUT_VERTICAL,
				BackgroundColor: "#3498DB",
				PaddingAll:      "20px",
				Contents: []messaging_api.FlexComponentInterface{
					&messaging_api.FlexText{
						Text:  "📊 กราฟรายจ่ายเดือนนี้",
						Size:  "sm",
						Color: "#FFFFFF",
					},
					&messaging_api.FlexText{
						Text:   "สัดส่วนการใช้จ่ายแยกตามหมวด",
						Weight: messaging_api.FlexTextWEIGHT_BOLD,
						Size:   "lg",
						Color:  "#FFFFFF",
						Margin: "sm",
					},
				},
			},
			Body: &messaging_api.FlexBox{
				Layout:     messaging_api.FlexBoxLAYOUT_VERTICAL,
				PaddingAll: "20px",
				Contents:   chartItems,
			},
		},
		QuickReply: &messaging_api.QuickReply{
			Items: []messaging_api.QuickReplyItem{
				{Action: &messaging_api.MessageAction{Label: "📄 Export Excel", Text: "ส่งออก excel"}},
				{Action: &messaging_api.MessageAction{Label: "📑 Export PDF", Text: "ดาวน์โหลด pdf"}},
				{Action: &messaging_api.MessageAction{Label: "📈 วิเคราะห์เพิ่ม", Text: "สรุป 7 วัน"}},
			},
		},
	}

	_, err = h.bot.ReplyMessage(&messaging_api.ReplyMessageRequest{
		ReplyToken: replyToken,
		Messages:   []messaging_api.MessageInterface{flexMessage},
	})
	if err != nil {
		log.Printf("Failed to send chart flex: %v", err)
	}
}

// replySearchResults displays search results with Flex Message carousel
func (h *LineWebhookHandler) replySearchResults(replyToken, userID string, results []services.SearchResult, keyword string) {
	if len(results) == 0 {
		h.replyText(replyToken, "ไม่พบรายการที่ค้นหา")
		return
	}

	// Calculate totals
	var totalIncome, totalExpense float64
	for _, r := range results {
		if r.Transaction.Type == 1 {
			totalIncome += r.Transaction.Amount
		} else {
			totalExpense += r.Transaction.Amount
		}
	}

	// Build body contents
	var bodyContents []messaging_api.FlexComponentInterface

	// Summary section
	bodyContents = append(bodyContents,
		&messaging_api.FlexText{
			Text:  fmt.Sprintf("พบ %d รายการ", len(results)),
			Size:  "sm",
			Color: "#888888",
		},
	)

	if totalExpense > 0 {
		bodyContents = append(bodyContents,
			&messaging_api.FlexBox{
				Layout: messaging_api.FlexBoxLAYOUT_HORIZONTAL,
				Margin: "sm",
				Contents: []messaging_api.FlexComponentInterface{
					&messaging_api.FlexText{
						Text:  "💸 รวมจ่าย",
						Size:  "sm",
						Color: "#E74C3C",
						Flex:  2,
					},
					&messaging_api.FlexText{
						Text:   fmt.Sprintf("%s", formatNumber(totalExpense)),
						Size:   "md",
						Color:  "#E74C3C",
						Weight: messaging_api.FlexTextWEIGHT_BOLD,
						Align:  messaging_api.FlexTextALIGN_END,
						Flex:   3,
					},
				},
			},
		)
	}

	if totalIncome > 0 {
		bodyContents = append(bodyContents,
			&messaging_api.FlexBox{
				Layout: messaging_api.FlexBoxLAYOUT_HORIZONTAL,
				Margin: "sm",
				Contents: []messaging_api.FlexComponentInterface{
					&messaging_api.FlexText{
						Text:  "💰 รวมรับ",
						Size:  "sm",
						Color: "#27AE60",
						Flex:  2,
					},
					&messaging_api.FlexText{
						Text:   fmt.Sprintf("%s", formatNumber(totalIncome)),
						Size:   "md",
						Color:  "#27AE60",
						Weight: messaging_api.FlexTextWEIGHT_BOLD,
						Align:  messaging_api.FlexTextALIGN_END,
						Flex:   3,
					},
				},
			},
		)
	}

	bodyContents = append(bodyContents, &messaging_api.FlexSeparator{Margin: "lg"})

	// List transactions (max 10)
	maxShow := 10
	if len(results) < maxShow {
		maxShow = len(results)
	}

	for i := 0; i < maxShow; i++ {
		r := results[i]
		typeIcon := "💸"
		typeColor := "#E74C3C"
		if r.Transaction.Type == 1 {
			typeIcon = "💰"
			typeColor = "#27AE60"
		}

		// Payment method
		paymentIcon := "💵"
		switch r.Transaction.UseType {
		case 1:
			paymentIcon = "💳"
		case 2:
			paymentIcon = "🏦"
		}

		description := r.Transaction.Description
		if description == "" {
			description = r.Transaction.Category
		}

		bodyContents = append(bodyContents,
			&messaging_api.FlexBox{
				Layout: messaging_api.FlexBoxLAYOUT_HORIZONTAL,
				Margin: "md",
				Contents: []messaging_api.FlexComponentInterface{
					&messaging_api.FlexText{
						Text:  fmt.Sprintf("%s %s", typeIcon, description),
						Size:  "sm",
						Color: "#333333",
						Flex:  4,
						Wrap:  true,
					},
					&messaging_api.FlexText{
						Text:   fmt.Sprintf("%s", formatNumber(r.Transaction.Amount)),
						Size:   "sm",
						Color:  typeColor,
						Weight: messaging_api.FlexTextWEIGHT_BOLD,
						Align:  messaging_api.FlexTextALIGN_END,
						Flex:   2,
					},
				},
			},
			&messaging_api.FlexText{
				Text:   fmt.Sprintf("   📅 %s %s", r.Date, paymentIcon),
				Size:   "xs",
				Color:  "#AAAAAA",
				Margin: "xs",
			},
		)

		// Show image if available
		if r.Transaction.ImageBase64 != "" {
			bodyContents = append(bodyContents,
				&messaging_api.FlexText{
					Text:   "   📷 มีรูปใบเสร็จแนบ",
					Size:   "xs",
					Color:  "#1E88E5",
					Margin: "xs",
				},
			)
		}
	}

	// Show "and more" if there are more results
	if len(results) > maxShow {
		bodyContents = append(bodyContents,
			&messaging_api.FlexText{
				Text:   fmt.Sprintf("...และอีก %d รายการ", len(results)-maxShow),
				Size:   "xs",
				Color:  "#888888",
				Margin: "lg",
				Align:  messaging_api.FlexTextALIGN_CENTER,
			},
		)
	}

	flexMessage := messaging_api.FlexMessage{
		AltText: fmt.Sprintf("ค้นหา \"%s\" พบ %d รายการ", keyword, len(results)),
		Contents: &messaging_api.FlexBubble{
			Size: messaging_api.FlexBubbleSIZE_MEGA,
			Header: &messaging_api.FlexBox{
				Layout:          messaging_api.FlexBoxLAYOUT_VERTICAL,
				BackgroundColor: "#6C5CE7",
				PaddingAll:      "20px",
				Contents: []messaging_api.FlexComponentInterface{
					&messaging_api.FlexText{
						Text:   "🔍 ผลการค้นหา",
						Weight: messaging_api.FlexTextWEIGHT_BOLD,
						Size:   "lg",
						Color:  "#FFFFFF",
					},
					&messaging_api.FlexText{
						Text:   fmt.Sprintf("\"%s\"", keyword),
						Size:   "sm",
						Color:  "#DDD6FE",
						Margin: "xs",
					},
				},
			},
			Body: &messaging_api.FlexBox{
				Layout:     messaging_api.FlexBoxLAYOUT_VERTICAL,
				PaddingAll: "20px",
				Contents:   bodyContents,
			},
		},
		QuickReply: &messaging_api.QuickReply{
			Items: []messaging_api.QuickReplyItem{
				{Action: &messaging_api.MessageAction{Label: "💰 ดูยอดคงเหลือ", Text: "ยอดคงเหลือ"}},
				{Action: &messaging_api.MessageAction{Label: "📊 สรุปวันนี้", Text: "สรุปวันนี้"}},
			},
		},
	}

	_, err := h.bot.ReplyMessage(&messaging_api.ReplyMessageRequest{
		ReplyToken: replyToken,
		Messages:   []messaging_api.MessageInterface{flexMessage},
	})
	if err != nil {
		log.Printf("Failed to send search results: %v", err)
	}
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
