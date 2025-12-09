package handlers

import (
	"context"
	"encoding/json"
	"fmt"
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
	gemini        *services.GeminiService
	mongo         *services.MongoDBService
	export        *services.ExportService
	firebase      *services.FirebaseService
}

func NewLineWebhookHandler(channelSecret, channelToken string, gemini *services.GeminiService, mongo *services.MongoDBService, firebase *services.FirebaseService) (*LineWebhookHandler, error) {
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
		gemini:        gemini,
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

	// Use replyToken for immediate response (free, no quota)
	h.replyText(replyToken, "กำลังวิเคราะห์ใบเสร็จ กรุณารอสักครู่นะคะ... 🔍")

	contentType := content.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/jpeg"
	}
	log.Printf("Image content type: %s", contentType)

	imageFormat := contentType
	if len(contentType) > 6 && contentType[:6] == "image/" {
		imageFormat = contentType[6:]
	}

	transactionData, err := h.gemini.ProcessReceiptImage(context.Background(), content.Body, imageFormat)
	if err != nil {
		log.Printf("Failed to process image with Gemini: %v", err)
		// replyToken already used, must use push
		h.pushText(userID, "ขออภัยค่ะ ไม่สามารถอ่านข้อมูลจากใบเสร็จได้ กรุณาลองใหม่อีกครั้ง")
		return
	}

	// replyToken already used, use push for flex message
	h.pushTransactionFlex(userID, transactionData)
}

func (h *LineWebhookHandler) handleTextMessage(ctx context.Context, source webhook.SourceInterface, message webhook.TextMessageContent, replyToken string) {
	userID := h.getUserID(source)
	log.Printf("handleTextMessage - userID: %s, source type: %T", userID, source)

	if userID == "" {
		log.Printf("userID is empty, cannot reply")
		return
	}

	// Process synchronously for serverless compatibility
	bgCtx := context.Background()

		// Get last transaction info for context
		lastTxInfo := ""
		lastTx, txType, err := h.mongo.GetLastTransaction(bgCtx, userID)
		if err == nil && lastTx != nil {
			lastTxInfo = fmt.Sprintf("%s %.0f บาท (%s)", txType, lastTx.Amount, lastTx.Description)
		}

		// Get recent transactions context (7 days) for AI analysis
		recentContext := h.mongo.GetRecentTransactionsContext(bgCtx, userID, 7)

		// Get user's existing banks and credit cards for matching
		userBanks, userCards, _ := h.mongo.GetDistinctPaymentMethods(bgCtx, userID)
		paymentContext := ""
		if len(userBanks) > 0 || len(userCards) > 0 {
			paymentContext = "\nบัญชีที่มี:"
			if len(userBanks) > 0 {
				paymentContext += "\nธนาคาร: " + strings.Join(userBanks, ", ")
			}
			if len(userCards) > 0 {
				paymentContext += "\nบัตรเครดิต: " + strings.Join(userCards, ", ")
			}
		}

		// Get user's existing categories for matching
		incomeCategories, expenseCategories, _ := h.mongo.GetDistinctCategories(bgCtx, userID)
		categoryContext := ""
		if len(incomeCategories) > 0 || len(expenseCategories) > 0 {
			categoryContext = "\nหมวดหมู่ที่มี:"
			if len(incomeCategories) > 0 {
				categoryContext += "\nรายรับ: " + strings.Join(incomeCategories, ", ")
			}
			if len(expenseCategories) > 0 {
				categoryContext += "\nรายจ่าย: " + strings.Join(expenseCategories, ", ")
			}
		}

		// Get budget summary for context
		budgetContext := h.mongo.GetBudgetSummaryText(bgCtx, userID)

		// Get chat history (last 5 messages for context, save tokens)
		chatHistory := ""
		history, _ := h.mongo.GetChatHistory(bgCtx, userID, 5)
		for _, msg := range history {
			chatHistory += msg.Role + ": " + msg.Content + "\n"
		}

		// Combine context: lastTxInfo + recentContext + paymentContext + categoryContext + budgetContext
		fullContext := lastTxInfo
		if recentContext != "" {
			fullContext += "\n" + recentContext
		}
		if paymentContext != "" {
			fullContext += paymentContext
		}
		if categoryContext != "" {
			fullContext += categoryContext
		}
		if budgetContext != "" {
			fullContext += "\n" + budgetContext
		}

		// Save user message to history
		h.mongo.SaveChatMessage(bgCtx, userID, "user", message.Text)

		log.Printf("Calling Gemini AI with message: %s", message.Text)

		response, err := h.gemini.ChatWithContext(bgCtx, message.Text, fullContext, chatHistory)
		if err != nil {
			log.Printf("Failed to chat with Gemini: %v", err)
			// Use replyToken for quick error response (free, no quota)
			h.replyText(replyToken, "ขออภัยค่ะ เกิดข้อผิดพลาด กรุณาลองใหม่อีกครั้ง")
			return
		}

		log.Printf("Gemini response: %s", response)
		response = cleanJSONResponse(response)

		// Parse AI response
		var aiResp services.AIResponse
		if err := json.Unmarshal([]byte(response), &aiResp); err != nil {
			// Try old format (array of transactions)
			var txArray []services.TransactionData
			if err := json.Unmarshal([]byte(response), &txArray); err == nil && len(txArray) > 0 {
				h.pushTransactionFlexMultiple(userID, txArray)
				return
			}
			// Not JSON - send as plain text
			h.pushText(userID, response)
			return
		}

		// Handle different actions
		switch aiResp.Action {
		case "new":
			// Filter out transactions with amount = 0 (likely AI errors)
			var validTransactions []services.TransactionData
			for _, tx := range aiResp.Transactions {
				if tx.Amount > 0 {
					validTransactions = append(validTransactions, tx)
				}
			}

			if len(validTransactions) > 0 {
				h.pushTransactionFlexMultiple(userID, validTransactions)
				// Balance is now included in the transaction flex message
				h.mongo.SaveChatMessage(bgCtx, userID, "assistant", "บันทึกรายการแล้ว")

				// Check budget alerts for expense transactions
				for _, tx := range validTransactions {
					if tx.Type == "expense" && tx.Category != "" {
						hasAlert, alertMsg := h.mongo.CheckBudgetAlert(bgCtx, userID, tx.Category, tx.Amount)
						if hasAlert {
							h.pushText(userID, alertMsg)
						}
					}
				}
			} else if len(aiResp.Transactions) > 0 {
				// Had transactions but all were 0 - likely AI error, reply with chat message
				if aiResp.Message != "" {
					h.pushTextWithSuggestions(userID, aiResp.Message)
				} else {
					h.pushTextWithSuggestions(userID, "ไม่สามารถอ่านจำนวนเงินได้ กรุณาลองใหม่อีกครั้งค่ะ")
				}
			}

		case "update":
			if lastTx == nil {
				h.pushText(userID, "ไม่พบรายการที่จะแก้ไขค่ะ")
				return
			}

			txID := lastTx.ID.Hex()
			var updateMsg string

			switch aiResp.UpdateField {
			case "amount":
				if val, ok := aiResp.UpdateValue.(float64); ok {
					h.mongo.UpdateTransactionAmount(bgCtx, userID, txID, val)
					updateMsg = fmt.Sprintf("แก้ไขยอดเป็น %.0f บาทแล้วค่ะ", val)
				}
			case "usetype":
				// UpdateValue can be either a float64 (just usetype) or a map with usetype, bankname, creditcardname
				bankName := ""
				creditCard := ""
				var useType int

				if val, ok := aiResp.UpdateValue.(float64); ok {
					// Simple case: just usetype number
					useType = int(val)
				} else if valMap, ok := aiResp.UpdateValue.(map[string]interface{}); ok {
					// Complex case: map with usetype and optional bank/card info
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
				updateMsg = aiResp.Message
				if updateMsg == "" {
					switch useType {
					case 0:
						updateMsg = "แก้ไขเป็นเงินสดแล้วค่ะ"
					case 1:
						if creditCard != "" {
							updateMsg = fmt.Sprintf("แก้ไขเป็นบัตรเครดิต %s แล้วค่ะ", creditCard)
						} else {
							updateMsg = "แก้ไขเป็นบัตรเครดิตแล้วค่ะ"
						}
					case 2:
						if bankName != "" {
							updateMsg = fmt.Sprintf("แก้ไขเป็นธนาคาร %s แล้วค่ะ", bankName)
						} else {
							updateMsg = "แก้ไขเป็นธนาคารแล้วค่ะ"
						}
					}
				}
			case "bankname":
				if val, ok := aiResp.UpdateValue.(string); ok {
					// Update to bank payment with specified bank name
					h.mongo.UpdateTransactionPayment(bgCtx, userID, txID, 2, val, "")
					updateMsg = fmt.Sprintf("แก้ไขเป็นธนาคาร %s แล้วค่ะ", val)
				}
			case "creditcardname":
				if val, ok := aiResp.UpdateValue.(string); ok {
					// Update to credit card payment with specified card name
					h.mongo.UpdateTransactionPayment(bgCtx, userID, txID, 1, "", val)
					updateMsg = fmt.Sprintf("แก้ไขเป็นบัตรเครดิต %s แล้วค่ะ", val)
				}
			}

			if updateMsg == "" {
				updateMsg = aiResp.Message
			}

			// Show updated transaction with balance included
			updatedTx, _ := h.mongo.GetTransactionByID(bgCtx, userID, txID)
			if updatedTx != nil {
				h.replyUpdatedTransaction(userID, updatedTx, updateMsg, txID)
			} else {
				h.pushText(userID, updateMsg)
			}
			// Balance is now included in the updated transaction flex message
			h.mongo.SaveChatMessage(bgCtx, userID, "assistant", updateMsg)

		case "transfer":
			if aiResp.Transfer != nil {
				// Convert to mongodb format
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

				transferID, _, err := h.mongo.SaveTransfer(bgCtx, userID, transfer)
				if err != nil {
					log.Printf("Failed to save transfer: %v", err)
					h.pushText(userID, "ไม่สามารถบันทึกการโอนได้")
					return
				}
				h.replyTransferFlex(userID, transfer, transferID, aiResp.Message)
				// Balance is now included in the transfer flex message
				h.mongo.SaveChatMessage(bgCtx, userID, "assistant", aiResp.Message)
			}

		case "balance":
			h.pushBalanceByPaymentType(userID)
			h.mongo.SaveChatMessage(bgCtx, userID, "assistant", "แสดงยอดคงเหลือ")

		case "search":
			if aiResp.SearchQuery != "" {
				// Search transactions
				results, err := h.mongo.SearchTransactions(bgCtx, userID, aiResp.SearchQuery, 20)
				if err != nil {
					log.Printf("Failed to search transactions: %v", err)
					h.pushText(userID, "ไม่สามารถค้นหาได้ กรุณาลองใหม่")
					return
				}

				if len(results) == 0 {
					h.pushTextWithSuggestions(userID, fmt.Sprintf("ไม่พบรายการ \"%s\" ในประวัติค่ะ", aiResp.SearchQuery))
					h.mongo.SaveChatMessage(bgCtx, userID, "assistant", "ไม่พบรายการ")
					return
				}

				// Show search results with Flex Message
				h.replySearchResults(userID, results, aiResp.SearchQuery)
				h.mongo.SaveChatMessage(bgCtx, userID, "assistant", fmt.Sprintf("พบ %d รายการ", len(results)))
			}

		case "analyze":
			if aiResp.Analysis != nil {
				h.replyAnalysisFlex(userID, aiResp.Analysis, aiResp.Message)
				h.mongo.SaveChatMessage(bgCtx, userID, "assistant", aiResp.Message)
			} else if aiResp.Message != "" {
				h.pushTextWithSuggestions(userID, aiResp.Message)
				h.mongo.SaveChatMessage(bgCtx, userID, "assistant", aiResp.Message)
			}

		case "budget":
			if aiResp.Budget != nil && aiResp.Budget.Category != "" && aiResp.Budget.Amount > 0 {
				err := h.mongo.SetBudget(bgCtx, userID, aiResp.Budget.Category, aiResp.Budget.Amount)
				if err != nil {
					log.Printf("Failed to set budget: %v", err)
					h.pushText(userID, "ไม่สามารถตั้งงบประมาณได้ ลองใหม่นะคะ")
				} else {
					// Get updated budget status and show
					h.replyBudgetFlex(userID, aiResp.Budget.Category, aiResp.Budget.Amount, aiResp.Message)
					h.mongo.SaveChatMessage(bgCtx, userID, "assistant", aiResp.Message)
				}
			} else if aiResp.Message != "" {
				h.pushTextWithSuggestions(userID, aiResp.Message)
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

				h.pushText(userID, aiResp.Message)

				if format == "pdf" {
					data, filename, err := h.export.ExportToPDF(bgCtx, userID, days)
					if err != nil {
						log.Printf("Failed to export PDF: %v", err)
						h.pushText(userID, "ไม่สามารถสร้างไฟล์ PDF ได้ ลองใหม่นะคะ")
					} else {
						h.sendFile(userID, data, filename, "application/pdf")
					}
				} else {
					data, filename, err := h.export.ExportToExcel(bgCtx, userID, days)
					if err != nil {
						log.Printf("Failed to export Excel: %v", err)
						h.pushText(userID, "ไม่สามารถสร้างไฟล์ Excel ได้ ลองใหม่นะคะ")
					} else {
						h.sendFile(userID, data, filename, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
					}
				}
				h.mongo.SaveChatMessage(bgCtx, userID, "assistant", aiResp.Message)
			}

		case "chart":
			h.replyChartFlex(userID)
			h.mongo.SaveChatMessage(bgCtx, userID, "assistant", "แสดงกราฟสัดส่วนรายจ่าย")

		case "chat":
			h.pushTextWithSuggestions(userID, aiResp.Message)
			h.mongo.SaveChatMessage(bgCtx, userID, "assistant", aiResp.Message)

		default:
			// Fallback: check if there are transactions (filter out amount = 0)
			var validTx []services.TransactionData
			for _, tx := range aiResp.Transactions {
				if tx.Amount > 0 {
					validTx = append(validTx, tx)
				}
			}

			if len(validTx) > 0 {
				h.pushTransactionFlexMultiple(userID, validTx)
			} else if aiResp.Message != "" {
				h.pushTextWithSuggestions(userID, aiResp.Message)
			} else {
				h.pushText(userID, response)
			}
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

// pushText sends a push message (uses quota but works anytime)
func (h *LineWebhookHandler) pushText(userID, text string) {
	_, err := h.bot.PushMessage(&messaging_api.PushMessageRequest{
		To: userID,
		Messages: []messaging_api.MessageInterface{
			messaging_api.TextMessage{
				Text: text,
			},
		},
	}, "")
	if err != nil {
		log.Printf("Failed to push message: %v", err)
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

// pushTextWithSuggestions sends text with quick reply suggestions (uses quota)
func (h *LineWebhookHandler) pushTextWithSuggestions(userID, text string) {
	_, err := h.bot.PushMessage(&messaging_api.PushMessageRequest{
		To: userID,
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
	}, "")
	if err != nil {
		log.Printf("Failed to push message with suggestions: %v", err)
	}
}

// replyTransferFlex shows transfer confirmation with Flex Message
func (h *LineWebhookHandler) replyTransferFlex(userID string, transfer *services.TransferData, transferID string, message string) {
	ctx := context.Background()

	// Get balance by payment type for detailed view
	balances, _ := h.mongo.GetBalanceByPaymentType(ctx, userID)

	// Build from entries text
	var fromTexts []string
	var totalFrom float64
	for _, e := range transfer.From {
		name := getPaymentName(e.UseType, e.BankName, e.CreditCardName)
		fromTexts = append(fromTexts, fmt.Sprintf("%s ฿%s", name, formatNumber(e.Amount)))
		totalFrom += e.Amount
	}

	// Build to entries text
	var toTexts []string
	for _, e := range transfer.To {
		name := getPaymentName(e.UseType, e.BankName, e.CreditCardName)
		toTexts = append(toTexts, fmt.Sprintf("%s ฿%s", name, formatNumber(e.Amount)))
	}

	// Build body contents
	bodyContents := []messaging_api.FlexComponentInterface{
		&messaging_api.FlexText{
			Text:   message,
			Size:   "sm",
			Color:  "#666666",
			Wrap:   true,
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
					Text:   fmt.Sprintf("฿%s", formatNumber(totalFrom)),
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
		AltText: fmt.Sprintf("โอนเงิน ฿%s", formatNumber(totalFrom)),
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

	_, err := h.bot.PushMessage(&messaging_api.PushMessageRequest{
		To:       userID,
		Messages: []messaging_api.MessageInterface{flexMessage},
	}, "")
	if err != nil {
		log.Printf("Failed to send transfer flex: %v", err)
		h.pushText(userID, message)
	}
}

// getPaymentName returns display name for payment type
func getPaymentName(useType int, bankName, creditCardName string) string {
	switch useType {
	case 0:
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

func (h *LineWebhookHandler) pushTransactionFlex(userID string, tx *services.TransactionData) {
	ctx := context.Background()

	// Auto save to MongoDB
	txID, err := h.mongo.SaveTransaction(ctx, userID, tx)
	if err != nil {
		log.Printf("Failed to save transaction: %v", err)
		h.pushText(userID, "ขออภัยค่ะ ไม่สามารถบันทึกข้อมูลได้")
		return
	}
	log.Printf("Transaction saved with ID: %s", txID)

	// Get balance by payment type for detailed view
	balances, _ := h.mongo.GetBalanceByPaymentType(ctx, userID)

	typeText := "💸 รายจ่าย"
	typeColor := "#E74C3C"
	headerBgColor := "#E74C3C"
	if tx.Type == "income" {
		typeText = "💰 รายรับ"
		typeColor = "#27AE60"
		headerBgColor = "#27AE60"
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

	// Ensure category is not empty
	category := tx.Category
	if category == "" {
		category = "-"
	}

	// Build content items
	bodyContents := []messaging_api.FlexComponentInterface{
		// Amount row
		&messaging_api.FlexBox{
			Layout: messaging_api.FlexBoxLAYOUT_HORIZONTAL,
			Contents: []messaging_api.FlexComponentInterface{
				&messaging_api.FlexText{
					Text:  "จำนวนเงิน",
					Size:  "md",
					Color: "#555555",
					Flex:  3,
				},
				&messaging_api.FlexText{
					Text:   fmt.Sprintf("฿%s", formatNumber(tx.Amount)),
					Size:   "xl",
					Color:  typeColor,
					Weight: messaging_api.FlexTextWEIGHT_BOLD,
					Flex:   4,
					Align:  messaging_api.FlexTextALIGN_END,
				},
			},
		},
		// Category row
		&messaging_api.FlexBox{
			Layout: messaging_api.FlexBoxLAYOUT_HORIZONTAL,
			Margin: "md",
			Contents: []messaging_api.FlexComponentInterface{
				&messaging_api.FlexText{
					Text:  "หมวดหมู่",
					Size:  "sm",
					Color: "#888888",
					Flex:  3,
				},
				&messaging_api.FlexText{
					Text:  category,
					Size:  "sm",
					Color: "#333333",
					Flex:  4,
					Align: messaging_api.FlexTextALIGN_END,
				},
			},
		},
		// Payment method row
		&messaging_api.FlexBox{
			Layout: messaging_api.FlexBoxLAYOUT_HORIZONTAL,
			Margin: "sm",
			Contents: []messaging_api.FlexComponentInterface{
				&messaging_api.FlexText{
					Text:  "ช่องทาง",
					Size:  "sm",
					Color: "#888888",
					Flex:  3,
				},
				&messaging_api.FlexText{
					Text:  paymentText,
					Size:  "sm",
					Color: "#333333",
					Flex:  4,
					Align: messaging_api.FlexTextALIGN_END,
				},
			},
		},
	}

	// Add merchant if available
	if tx.Merchant != "" {
		bodyContents = append(bodyContents, &messaging_api.FlexBox{
			Layout: messaging_api.FlexBoxLAYOUT_HORIZONTAL,
			Margin: "sm",
			Contents: []messaging_api.FlexComponentInterface{
				&messaging_api.FlexText{
					Text:  "ร้านค้า",
					Size:  "sm",
					Color: "#888888",
					Flex:  3,
				},
				&messaging_api.FlexText{
					Text:  tx.Merchant,
					Size:  "sm",
					Color: "#333333",
					Flex:  4,
					Align: messaging_api.FlexTextALIGN_END,
				},
			},
		})
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

	// Create postback data with txID for delete
	postbackData := fmt.Sprintf("txid=%s", txID)

	flexMessage := messaging_api.FlexMessage{
		AltText: fmt.Sprintf("%s %.2f บาท (บันทึกแล้ว)", typeText, tx.Amount),
		Contents: &messaging_api.FlexBubble{
			Size: messaging_api.FlexBubbleSIZE_MEGA,
			Header: &messaging_api.FlexBox{
				Layout:          messaging_api.FlexBoxLAYOUT_VERTICAL,
				BackgroundColor: headerBgColor,
				PaddingAll:      "20px",
				Contents: []messaging_api.FlexComponentInterface{
					&messaging_api.FlexText{
						Text:   "สติสตางค์ ✅ บันทึกแล้ว",
						Weight: messaging_api.FlexTextWEIGHT_BOLD,
						Size:   "sm",
						Color:  "#FFFFFF",
					},
					&messaging_api.FlexText{
						Text:   typeText,
						Weight: messaging_api.FlexTextWEIGHT_BOLD,
						Size:   "xxl",
						Color:  "#FFFFFF",
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
						Label: "🗑️ ลบรายการนี้",
						Data:  "action=delete&" + postbackData,
					},
				},
			},
		},
	}

	_, pushErr := h.bot.PushMessage(&messaging_api.PushMessageRequest{
		To:       userID,
		Messages: []messaging_api.MessageInterface{flexMessage},
	}, "")
	if pushErr != nil {
		log.Printf("Failed to send flex message: %v", pushErr)
		h.pushText(userID, fmt.Sprintf("%s: %.2f บาท (บันทึกแล้ว)", typeText, tx.Amount))
	}
}

func (h *LineWebhookHandler) pushTransactionFlexMultiple(userID string, transactions []services.TransactionData) {
	if len(transactions) == 0 {
		return
	}

	// If only one transaction, use single flex
	if len(transactions) == 1 {
		h.pushTransactionFlex(userID, &transactions[0])
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

	// Create carousel - already saved, only delete option
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

	_, err := h.bot.PushMessage(&messaging_api.PushMessageRequest{
		To:       userID,
		Messages: []messaging_api.MessageInterface{flexMessage},
	}, "")
	if err != nil {
		log.Printf("Failed to send flex carousel: %v", err)
		// Fallback to text
		var texts []string
		for _, tx := range transactions {
			typeText := "💸"
			if tx.Type == "income" {
				typeText = "💰"
			}
			texts = append(texts, fmt.Sprintf("%s %s: %.2f บาท", typeText, tx.Description, tx.Amount))
		}
		h.pushText(userID, strings.Join(texts, "\n")+" (บันทึกแล้ว)")
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
					Text:   fmt.Sprintf("฿%.2f", tx.Amount),
					Size:   "xl",
					Color:  typeColor,
					Weight: messaging_api.FlexTextWEIGHT_BOLD,
					Margin: "sm",
				},
				&messaging_api.FlexText{
					Text:  fmt.Sprintf("📅 %s | 🏷️ %s", tx.Date, tx.Category),
					Size:  "xs",
					Color: "#888888",
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
					Text:   "ยอดคงเหลือ",
					Size:   "sm",
					Color:  "#888888",
				},
				&messaging_api.FlexText{
					Text:   fmt.Sprintf("฿%.2f", balance.Balance),
					Size:   "xxl",
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
							Text:  fmt.Sprintf("฿%.2f", balance.TotalIncome),
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
							Text:  fmt.Sprintf("฿%.2f", balance.TotalExpense),
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

func (h *LineWebhookHandler) replyUpdatedTransaction(userID string, tx *services.Transaction, message string, txID string) {
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
			Text:   fmt.Sprintf("฿%.2f", tx.Amount),
			Size:   "xxl",
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
		AltText: fmt.Sprintf("แก้ไขแล้ว: %.0f บาท", tx.Amount),
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
						Label: "🗑️ ลบรายการนี้",
						Data:  "action=delete&txid=" + txID,
					},
				},
			},
		},
	}

	_, err := h.bot.PushMessage(&messaging_api.PushMessageRequest{
		To:       userID,
		Messages: []messaging_api.MessageInterface{flexMessage},
	}, "")
	if err != nil {
		log.Printf("Failed to send updated transaction: %v", err)
		h.pushText(userID, message)
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

		// Get updated balance
		balance, _ := h.mongo.GetBalanceSummary(ctx, userID)
		balanceText := ""
		if balance != nil {
			balanceText = fmt.Sprintf("\n💰 ยอดคงเหลือ: ฿%.2f", balance.Balance)
		}

		h.replyText(replyToken, "🗑️ ลบรายการเรียบร้อยแล้ว"+balanceText)

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

		h.replyText(replyToken, fmt.Sprintf("🗑️ ลบ %d รายการเรียบร้อยแล้ว", deletedCount))
		// Show updated balance - must use push since replyToken already used
		h.pushBalanceByPaymentType(userID)

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

		h.replyText(replyToken, "🗑️ ยกเลิกการโอนเรียบร้อยแล้ว")
		// Show updated balance - must use push since replyToken already used
		h.pushBalanceByPaymentType(userID)

	default:
		log.Printf("Unknown postback action: %s", action)
	}
}

// pushBalanceByPaymentType shows balance breakdown by payment type with total assets
func (h *LineWebhookHandler) pushBalanceByPaymentType(userID string) {
	ctx := context.Background()

	// Get balance by payment type
	balances, err := h.mongo.GetBalanceByPaymentType(ctx, userID)
	if err != nil || len(balances) == 0 {
		h.pushText(userID, "ยังไม่มีรายการค่ะ")
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
	netWorthText := fmt.Sprintf("฿%s", formatNumber(netWorth))
	if netWorth < 0 {
		netWorthText = fmt.Sprintf("-฿%s", formatNumber(-netWorth))
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
			Size:   "xxl",
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
						Text:  fmt.Sprintf("   +฿%s", formatNumber(cashBalance.TotalIncome)),
						Size:  "sm",
						Color: "#27AE60",
						Flex:  1,
					},
					&messaging_api.FlexText{
						Text:  fmt.Sprintf("-฿%s", formatNumber(cashBalance.TotalExpense)),
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
							Text:  fmt.Sprintf("   +฿%s", formatNumber(pb.TotalIncome)),
							Size:  "sm",
							Color: "#27AE60",
							Flex:  1,
						},
						&messaging_api.FlexText{
							Text:  fmt.Sprintf("-฿%s", formatNumber(pb.TotalExpense)),
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
							Text:  fmt.Sprintf("   จ่ายแล้ว +฿%s", formatNumber(pb.TotalIncome)),
							Size:  "sm",
							Color: "#27AE60",
							Flex:  1,
						},
						&messaging_api.FlexText{
							Text:  fmt.Sprintf("ใช้จ่าย -฿%s", formatNumber(pb.TotalExpense)),
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
		AltText: fmt.Sprintf("ทรัพย์สินทั้งหมด ฿%s", formatNumber(netWorth)),
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

	_, err = h.bot.PushMessage(&messaging_api.PushMessageRequest{
		To:       userID,
		Messages: []messaging_api.MessageInterface{flexMessage},
	}, "")
	if err != nil {
		log.Printf("Failed to send balance by payment type: %v", err)
	}
}

func getBalanceColor(balance float64) string {
	if balance < 0 {
		return "#E74C3C"
	}
	return "#27AE60"
}

func formatBalanceText(balance float64) string {
	if balance < 0 {
		return fmt.Sprintf("-฿%s", formatNumber(-balance))
	}
	return fmt.Sprintf("฿%s", formatNumber(balance))
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

// replyAnalysisFlex displays AI analysis with beautiful Flex Message
func (h *LineWebhookHandler) replyAnalysisFlex(userID string, analysis *services.AnalysisData, message string) {
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
				valueText += fmt.Sprintf("฿%s", formatNumber(insight.Amount))
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
						Text:   "🤖 สติสตางค์ AI",
						Size:   "sm",
						Color:  "#FFFFFF",
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

	_, err := h.bot.PushMessage(&messaging_api.PushMessageRequest{
		To:       userID,
		Messages: []messaging_api.MessageInterface{flexMessage},
	}, "")
	if err != nil {
		log.Printf("Failed to send analysis flex: %v", err)
		// Fallback to text
		fallbackText := analysis.Title + "\n" + analysis.Summary
		if analysis.Advice != "" {
			fallbackText += "\n💡 " + analysis.Advice
		}
		h.pushText(userID, fallbackText)
	}
}

// replyBudgetFlex displays budget setting confirmation with Flex Message
func (h *LineWebhookHandler) replyBudgetFlex(userID string, category string, amount float64, message string) {
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
		AltText: fmt.Sprintf("ตั้งงบ %s %.0f บาท", category, amount),
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
								Text:   fmt.Sprintf("฿%s", formatNumber(amount)),
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
								Text:   fmt.Sprintf("฿%s (%.0f%%)", formatNumber(spent), percentage),
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
								Text:   fmt.Sprintf("%s ฿%s", statusEmoji, formatNumber(remaining)),
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

	_, err := h.bot.PushMessage(&messaging_api.PushMessageRequest{
		To:       userID,
		Messages: []messaging_api.MessageInterface{flexMessage},
	}, "")
	if err != nil {
		log.Printf("Failed to send budget flex: %v", err)
		h.pushText(userID, message)
	}
}

// sendFile uploads file to Firebase Cloud Storage and sends download link to user
func (h *LineWebhookHandler) sendFile(userID string, data []byte, filename string, mimeType string) {
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
		h.pushText(userID, "❌ ระบบยังไม่พร้อมส่งไฟล์ค่ะ\n\nกรุณาติดต่อผู้ดูแลระบบ")
		return
	}

	// Upload to Firebase Cloud Storage
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	downloadURL, err := h.firebase.UploadFile(ctx, data, filename, mimeType)
	if err != nil {
		log.Printf("Failed to upload file to Firebase: %v", err)
		h.pushText(userID, "❌ ไม่สามารถอัปโหลดไฟล์ได้\n\nกรุณาลองใหม่อีกครั้งค่ะ")
		return
	}

	// Send Flex Message with download button
	h.sendFileDownloadFlex(userID, fileType, filename, fileSize, downloadURL)
}

// sendFileDownloadFlex sends a Flex Message with download button
func (h *LineWebhookHandler) sendFileDownloadFlex(userID, fileType, filename string, fileSize int, downloadURL string) {
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

	_, err := h.bot.PushMessage(&messaging_api.PushMessageRequest{
		To:       userID,
		Messages: []messaging_api.MessageInterface{flexMessage},
	}, "")
	if err != nil {
		log.Printf("Failed to send file download flex: %v", err)
		// Fallback to text
		message := fmt.Sprintf("✅ ไฟล์ %s พร้อมแล้ว!\n📁 %s (%d KB)\n\n📥 ดาวน์โหลด: %s\n\n⚠️ ลิงก์หมดอายุหลังดาวน์โหลดครั้งแรก", fileType, filename, fileSize, downloadURL)
		h.pushText(userID, message)
	}
}

// replyChartFlex displays spending chart as Flex Message with visual bars
func (h *LineWebhookHandler) replyChartFlex(userID string) {
	bgCtx := context.Background()

	// Get spending data
	chartData, total, err := h.export.GetCategorySpendingForChart(bgCtx, userID)
	if err != nil || len(chartData) == 0 {
		h.pushText(userID, "ไม่มีข้อมูลรายจ่ายเดือนนี้ค่ะ")
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
						Text:   fmt.Sprintf("฿%s (%.0f%%)", formatNumber(item.Amount), item.Percentage),
						Size:   "xs",
						Color:  "#888888",
						Align:  messaging_api.FlexTextALIGN_END,
						Flex:   3,
					},
				},
			},
			// Bar visualization
			&messaging_api.FlexBox{
				Layout:          messaging_api.FlexBoxLAYOUT_HORIZONTAL,
				Margin:          "xs",
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
					Text:   fmt.Sprintf("฿%s", formatNumber(total)),
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

	_, err = h.bot.PushMessage(&messaging_api.PushMessageRequest{
		To:       userID,
		Messages: []messaging_api.MessageInterface{flexMessage},
	}, "")
	if err != nil {
		log.Printf("Failed to send chart flex: %v", err)
		h.pushText(userID, "ไม่สามารถแสดงกราฟได้ ลองใหม่นะคะ")
	}
}

// replySearchResults displays search results with Flex Message carousel
func (h *LineWebhookHandler) replySearchResults(userID string, results []services.SearchResult, keyword string) {
	if len(results) == 0 {
		h.pushText(userID, "ไม่พบรายการที่ค้นหา")
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
			Text:   fmt.Sprintf("พบ %d รายการ", len(results)),
			Size:   "sm",
			Color:  "#888888",
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
						Text:   fmt.Sprintf("฿%s", formatNumber(totalExpense)),
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
						Text:   fmt.Sprintf("฿%s", formatNumber(totalIncome)),
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
						Text:   fmt.Sprintf("฿%s", formatNumber(r.Transaction.Amount)),
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

	_, err := h.bot.PushMessage(&messaging_api.PushMessageRequest{
		To:       userID,
		Messages: []messaging_api.MessageInterface{flexMessage},
	}, "")
	if err != nil {
		log.Printf("Failed to send search results: %v", err)
		// Fallback to text
		summaryText := h.mongo.GetTransactionSummaryText(results)
		h.pushText(userID, summaryText)
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
