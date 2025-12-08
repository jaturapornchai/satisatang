package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

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
}

func NewLineWebhookHandler(channelSecret, channelToken string, gemini *services.GeminiService) (*LineWebhookHandler, error) {
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
		}
	}

	c.Status(http.StatusOK)
}

func (h *LineWebhookHandler) handleMessage(ctx context.Context, event webhook.MessageEvent) {
	log.Printf("Message type: %T", event.Message)
	
	switch message := event.Message.(type) {
	case webhook.ImageMessageContent:
		log.Printf("Processing image message")
		h.handleImageMessage(ctx, event.Source, message)
	case webhook.TextMessageContent:
		log.Printf("Processing text message: %s", message.Text)
		h.handleTextMessage(ctx, event.Source, message)
	default:
		log.Printf("Unknown message type: %T", event.Message)
	}
}

func (h *LineWebhookHandler) handleImageMessage(ctx context.Context, source webhook.SourceInterface, message webhook.ImageMessageContent) {
	userID := h.getUserID(source)
	if userID == "" {
		log.Println("Failed to get user ID")
		return
	}

	go func() {
		content, err := h.blobAPI.GetMessageContent(message.Id)
		if err != nil {
			log.Printf("Failed to get message content: %v", err)
			h.replyText(userID, "ขออภัยค่ะ ไม่สามารถดาวน์โหลดรูปภาพได้")
			return
		}
		defer content.Body.Close()

		h.replyText(userID, "กำลังวิเคราะห์ใบเสร็จ กรุณารอสักครู่นะคะ... 🔍")

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
			h.replyText(userID, "ขออภัยค่ะ ไม่สามารถอ่านข้อมูลจากใบเสร็จได้ กรุณาลองใหม่อีกครั้ง")
			return
		}

		h.replyTransactionFlex(userID, transactionData)
	}()
}

func (h *LineWebhookHandler) handleTextMessage(ctx context.Context, source webhook.SourceInterface, message webhook.TextMessageContent) {
	userID := h.getUserID(source)
	log.Printf("handleTextMessage - userID: %s, source type: %T", userID, source)
	
	if userID == "" {
		log.Printf("userID is empty, cannot reply")
		return
	}

	go func() {
		log.Printf("Calling Gemini AI with message: %s", message.Text)
		
		response, err := h.gemini.Chat(context.Background(), message.Text)
		if err != nil {
			log.Printf("Failed to chat with Gemini: %v", err)
			h.replyText(userID, "ขออภัยค่ะ เกิดข้อผิดพลาด กรุณาลองใหม่อีกครั้ง")
			return
		}

		log.Printf("Gemini response: %s", response)
		
		response = cleanJSONResponse(response)
		
		// Try to parse as JSON array first
		var txArray []services.TransactionData
		if err := json.Unmarshal([]byte(response), &txArray); err == nil && len(txArray) > 0 {
			// Successfully parsed as array
			h.replyTransactionFlexMultiple(userID, txArray)
			return
		}
		
		// Try to parse as single JSON object (backward compatibility)
		var txData services.TransactionData
		if err := json.Unmarshal([]byte(response), &txData); err == nil && txData.Amount > 0 {
			h.replyTransactionFlex(userID, &txData)
			return
		}
		
		// Not JSON - send as plain text
		h.replyText(userID, response)
	}()
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

func (h *LineWebhookHandler) replyText(userID, text string) {
	_, err := h.bot.PushMessage(&messaging_api.PushMessageRequest{
		To: userID,
		Messages: []messaging_api.MessageInterface{
			messaging_api.TextMessage{
				Text: text,
			},
		},
	}, "")
	if err != nil {
		log.Printf("Failed to send reply: %v", err)
	}
}

func (h *LineWebhookHandler) replyTransactionFlex(userID string, tx *services.TransactionData) {
	typeText := "💸 รายจ่าย"
	typeColor := "#E74C3C"
	if tx.Type == "income" {
		typeText = "💰 รายรับ"
		typeColor = "#27AE60"
	}

	// Build content items
	bodyContents := []messaging_api.FlexComponentInterface{
		// Amount row
		&messaging_api.FlexBox{
			Layout: messaging_api.FlexBoxLAYOUT_HORIZONTAL,
			Contents: []messaging_api.FlexComponentInterface{
				&messaging_api.FlexText{
					Text:  "💵 จำนวนเงิน",
					Size:  "md",
					Color: "#555555",
					Flex:  4,
				},
				&messaging_api.FlexText{
					Text:   fmt.Sprintf("฿%.2f", tx.Amount),
					Size:   "lg",
					Color:  typeColor,
					Weight: messaging_api.FlexTextWEIGHT_BOLD,
					Flex:   5,
					Align:  messaging_api.FlexTextALIGN_END,
				},
			},
		},
		// Separator
		&messaging_api.FlexSeparator{
			Margin: "lg",
		},
		// Date row
		&messaging_api.FlexBox{
			Layout: messaging_api.FlexBoxLAYOUT_HORIZONTAL,
			Margin: "lg",
			Contents: []messaging_api.FlexComponentInterface{
				&messaging_api.FlexText{
					Text:  "📅 วันที่",
					Size:  "sm",
					Color: "#555555",
					Flex:  3,
				},
				&messaging_api.FlexText{
					Text:  tx.Date,
					Size:  "sm",
					Color: "#333333",
					Flex:  5,
					Align: messaging_api.FlexTextALIGN_END,
				},
			},
		},
		// Category row
		&messaging_api.FlexBox{
			Layout: messaging_api.FlexBoxLAYOUT_HORIZONTAL,
			Margin: "md",
			Contents: []messaging_api.FlexComponentInterface{
				&messaging_api.FlexText{
					Text:  "🏷️ หมวดหมู่",
					Size:  "sm",
					Color: "#555555",
					Flex:  3,
				},
				&messaging_api.FlexText{
					Text:  tx.Category,
					Size:  "sm",
					Color: "#333333",
					Flex:  5,
					Align: messaging_api.FlexTextALIGN_END,
				},
			},
		},
	}

	// Add merchant if available
	if tx.Merchant != "" {
		bodyContents = append(bodyContents, &messaging_api.FlexBox{
			Layout: messaging_api.FlexBoxLAYOUT_HORIZONTAL,
			Margin: "md",
			Contents: []messaging_api.FlexComponentInterface{
				&messaging_api.FlexText{
					Text:  "🏪 ร้านค้า",
					Size:  "sm",
					Color: "#555555",
					Flex:  3,
				},
				&messaging_api.FlexText{
					Text:  tx.Merchant,
					Size:  "sm",
					Color: "#333333",
					Flex:  5,
					Align: messaging_api.FlexTextALIGN_END,
				},
			},
		})
	}

	// Add items if available
	if len(tx.Items) > 0 {
		var itemTexts []string
		for _, item := range tx.Items {
			itemTexts = append(itemTexts, fmt.Sprintf("• %s x%.0f", item.Name, item.Quantity))
		}
		bodyContents = append(bodyContents,
			&messaging_api.FlexSeparator{Margin: "lg"},
			&messaging_api.FlexBox{
				Layout: messaging_api.FlexBoxLAYOUT_VERTICAL,
				Margin: "lg",
				Contents: []messaging_api.FlexComponentInterface{
					&messaging_api.FlexText{
						Text:  "📝 รายการ:",
						Size:  "sm",
						Color: "#555555",
					},
					&messaging_api.FlexText{
						Text:  strings.Join(itemTexts, "\n"),
						Size:  "xs",
						Color: "#666666",
						Wrap:  true,
					},
				},
			},
		)
	}

	// Create postback data
	postbackData := fmt.Sprintf("amount=%.2f&type=%s&category=%s&date=%s", tx.Amount, tx.Type, tx.Category, tx.Date)

	flexMessage := messaging_api.FlexMessage{
		AltText: fmt.Sprintf("%s %.2f บาท", typeText, tx.Amount),
		Contents: &messaging_api.FlexBubble{
			Size: messaging_api.FlexBubbleSIZE_MEGA,
			Header: &messaging_api.FlexBox{
				Layout:          messaging_api.FlexBoxLAYOUT_VERTICAL,
				BackgroundColor: "#FFFFFF",
				PaddingAll:      "20px",
				Contents: []messaging_api.FlexComponentInterface{
					&messaging_api.FlexText{
						Text:   "สติสตางค์",
						Weight: messaging_api.FlexTextWEIGHT_BOLD,
						Size:   "sm",
						Color:  "#AAAAAA",
					},
					&messaging_api.FlexText{
						Text:   typeText,
						Weight: messaging_api.FlexTextWEIGHT_BOLD,
						Size:   "xxl",
						Color:  typeColor,
					},
				},
			},
			Body: &messaging_api.FlexBox{
				Layout:     messaging_api.FlexBoxLAYOUT_VERTICAL,
				PaddingAll: "20px",
				Contents:   bodyContents,
			},
			Footer: &messaging_api.FlexBox{
				Layout:     messaging_api.FlexBoxLAYOUT_HORIZONTAL,
				Spacing:    "sm",
				PaddingAll: "15px",
				Contents: []messaging_api.FlexComponentInterface{
					&messaging_api.FlexButton{
						Style:  messaging_api.FlexButtonSTYLE_PRIMARY,
						Height: messaging_api.FlexButtonHEIGHT_SM,
						Color:  "#27AE60",
						Flex:   1,
						Action: &messaging_api.PostbackAction{
							Label: "✅ บันทึก",
							Data:  "action=save&" + postbackData,
						},
					},
					&messaging_api.FlexButton{
						Style:  messaging_api.FlexButtonSTYLE_SECONDARY,
						Height: messaging_api.FlexButtonHEIGHT_SM,
						Flex:   1,
						Action: &messaging_api.PostbackAction{
							Label: "✏️ แก้ไข",
							Data:  "action=edit&" + postbackData,
						},
					},
					&messaging_api.FlexButton{
						Style:  messaging_api.FlexButtonSTYLE_SECONDARY,
						Height: messaging_api.FlexButtonHEIGHT_SM,
						Flex:   1,
						Action: &messaging_api.PostbackAction{
							Label: "🗑️ ลบ",
							Data:  "action=delete&" + postbackData,
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
		log.Printf("Failed to send flex message: %v", err)
		h.replyText(userID, fmt.Sprintf("%s: %.2f บาท (%s)", typeText, tx.Amount, tx.Category))
	}
}

func (h *LineWebhookHandler) replyTransactionFlexMultiple(userID string, transactions []services.TransactionData) {
	if len(transactions) == 0 {
		return
	}

	// If only one transaction, use single flex
	if len(transactions) == 1 {
		h.replyTransactionFlex(userID, &transactions[0])
		return
	}

	// Build bubbles for carousel
	var bubbles []messaging_api.FlexBubble
	for i := range transactions {
		tx := &transactions[i]
		bubble := h.buildTransactionBubble(tx)
		bubbles = append(bubbles, bubble)
	}

	// Create carousel
	flexMessage := messaging_api.FlexMessage{
		AltText: fmt.Sprintf("รายการธุรกรรม %d รายการ", len(transactions)),
		Contents: &messaging_api.FlexCarousel{
			Contents: bubbles,
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
		h.replyText(userID, strings.Join(texts, "\n"))
	}
}

func (h *LineWebhookHandler) buildTransactionBubble(tx *services.TransactionData) messaging_api.FlexBubble {
	typeText := "💸 รายจ่าย"
	typeColor := "#E74C3C"
	if tx.Type == "income" {
		typeText = "💰 รายรับ"
		typeColor = "#27AE60"
	}

	postbackData := fmt.Sprintf("amount=%.2f&type=%s&category=%s&date=%s&desc=%s", 
		tx.Amount, tx.Type, tx.Category, tx.Date, tx.Description)

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
		Footer: &messaging_api.FlexBox{
			Layout:  messaging_api.FlexBoxLAYOUT_HORIZONTAL,
			Spacing: "sm",
			Contents: []messaging_api.FlexComponentInterface{
				&messaging_api.FlexButton{
					Style:  messaging_api.FlexButtonSTYLE_PRIMARY,
					Height: messaging_api.FlexButtonHEIGHT_SM,
					Color:  "#27AE60",
					Flex:   1,
					Action: &messaging_api.PostbackAction{
						Label: "✅",
						Data:  "action=save&" + postbackData,
					},
				},
				&messaging_api.FlexButton{
					Style:  messaging_api.FlexButtonSTYLE_SECONDARY,
					Height: messaging_api.FlexButtonHEIGHT_SM,
					Flex:   1,
					Action: &messaging_api.PostbackAction{
						Label: "✏️",
						Data:  "action=edit&" + postbackData,
					},
				},
				&messaging_api.FlexButton{
					Style:  messaging_api.FlexButtonSTYLE_SECONDARY,
					Height: messaging_api.FlexButtonHEIGHT_SM,
					Flex:   1,
					Action: &messaging_api.PostbackAction{
						Label: "🗑️",
						Data:  "action=delete&" + postbackData,
					},
				},
			},
		},
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
