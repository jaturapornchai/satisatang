package services

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// DailyRecord represents a daily financial record
type DailyRecord struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	LineID         string             `bson:"lineid" json:"lineid"`
	Date           string             `bson:"date" json:"date"`
	Time           string             `bson:"time" json:"time"`
	Incomes        []Transaction      `bson:"incomes" json:"incomes"`
	Expenses       []Transaction      `bson:"expenses" json:"expenses"`
	UseType        int                `bson:"usetype" json:"usetype"`                 // 0=เงินสด, 1=บัตรเครดิต, 2=ธนาคาร
	BankName       string             `bson:"bankname" json:"bankname"`               // ชื่อธนาคาร
	CreditCardName string             `bson:"creditcardname" json:"creditcardname"`   // ชื่อบัตรเครดิต
	TotalIncome    float64            `bson:"totalIncome" json:"totalIncome"`
	TotalExpense   float64            `bson:"totalExpense" json:"totalExpense"`
	CreatedAt      time.Time          `bson:"createdAt" json:"createdAt"`
	UpdatedAt      time.Time          `bson:"updatedAt" json:"updatedAt"`
}

// ChatMessage represents a chat history message
type ChatMessage struct {
	Role      string    `bson:"role" json:"role"`           // "user" or "assistant"
	Content   string    `bson:"content" json:"content"`
	Timestamp time.Time `bson:"timestamp" json:"timestamp"`
}

// UserChat represents chat history for a user
type UserChat struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	LineID    string             `bson:"lineid" json:"lineid"`
	Messages  []ChatMessage      `bson:"messages" json:"messages"`
	UpdatedAt time.Time          `bson:"updatedAt" json:"updatedAt"`
}

// Transaction represents a single income or expense entry
type Transaction struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Type           int                `bson:"type" json:"type"` // 1 = income, -1 = expense
	CustName       string             `bson:"custname" json:"custname"`
	Amount         float64            `bson:"amount" json:"amount"`
	Category       string             `bson:"category" json:"category"`
	Description    string             `bson:"description" json:"description"`
	ImageBase64    string             `bson:"imagebase64" json:"imagebase64"`
	UseType        int                `bson:"usetype" json:"usetype"`               // 0=เงินสด, 1=บัตรเครดิต, 2=ธนาคาร
	BankName       string             `bson:"bankname" json:"bankname"`
	CreditCardName string             `bson:"creditcardname" json:"creditcardname"`
	TransferID     string             `bson:"transfer_id" json:"transfer_id"`       // link to transfers collection
	CreatedAt      time.Time          `bson:"created_at" json:"created_at"`
}

// TransferEntryDB represents a single transfer source or destination in DB
type TransferEntryDB struct {
	Amount         float64 `bson:"amount" json:"amount"`
	UseType        int     `bson:"usetype" json:"usetype"`
	BankName       string  `bson:"bankname" json:"bankname"`
	CreditCardName string  `bson:"creditcardname" json:"creditcardname"`
}

// Note: TransactionData, TransferEntry, TransferData are defined in gemini.go

// TransferRecord represents a transfer record in MongoDB
type TransferRecord struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	LineID      string             `bson:"lineid" json:"lineid"`
	Date        string             `bson:"date" json:"date"`
	Description string             `bson:"description" json:"description"`
	From        []TransferEntryDB  `bson:"from" json:"from"`
	To          []TransferEntryDB  `bson:"to" json:"to"`
	TotalAmount float64            `bson:"total_amount" json:"total_amount"`
	CreatedAt   time.Time          `bson:"created_at" json:"created_at"`
}

// Budget represents a category budget
type Budget struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	LineID    string             `bson:"lineid" json:"lineid"`
	Category  string             `bson:"category" json:"category"`
	Amount    float64            `bson:"amount" json:"amount"`       // งบประมาณต่อเดือน
	CreatedAt time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time          `bson:"updated_at" json:"updated_at"`
}

// BudgetStatus represents budget vs actual spending
type BudgetStatus struct {
	Category   string  `json:"category"`
	Budget     float64 `json:"budget"`
	Spent      float64 `json:"spent"`
	Remaining  float64 `json:"remaining"`
	Percentage float64 `json:"percentage"` // spent/budget * 100
	IsOverBudget bool  `json:"is_over_budget"`
}

type MongoDBService struct {
	client             *mongo.Client
	database           *mongo.Database
	collection         *mongo.Collection
	chatCollection     *mongo.Collection
	transferCollection *mongo.Collection
	budgetCollection   *mongo.Collection
}

func NewMongoDBService(uri, dbName string) (*MongoDBService, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	// Ping to verify connection
	if err := client.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	log.Println("Connected to MongoDB Atlas")

	database := client.Database(dbName)
	collection := database.Collection("daily_records")
	chatCollection := database.Collection("chat_history")
	transferCollection := database.Collection("transfers")
	budgetCollection := database.Collection("budgets")

	return &MongoDBService{
		client:             client,
		database:           database,
		collection:         collection,
		chatCollection:     chatCollection,
		transferCollection: transferCollection,
		budgetCollection:   budgetCollection,
	}, nil
}

// SaveTransaction saves a transaction to the daily record
func (s *MongoDBService) SaveTransaction(ctx context.Context, lineID string, tx *TransactionData) (string, error) {
	today := time.Now().Format("2006-01-02")
	currentTime := time.Now().Format("15:04")

	// Determine transaction type
	txType := -1 // expense
	if tx.Type == "income" {
		txType = 1
	}

	newTx := Transaction{
		ID:             primitive.NewObjectID(),
		Type:           txType,
		CustName:       tx.Merchant,
		Amount:         tx.Amount,
		Category:       tx.Category,
		Description:    tx.Description,
		UseType:        tx.UseType,
		BankName:       tx.BankName,
		CreditCardName: tx.CreditCardName,
		CreatedAt:      time.Now(),
	}

	// Find or create daily record
	filter := bson.M{
		"lineid": lineID,
		"date":   today,
	}

	var record DailyRecord
	err := s.collection.FindOne(ctx, filter).Decode(&record)

	if err == mongo.ErrNoDocuments {
		// Create new daily record
		record = DailyRecord{
			LineID:    lineID,
			Date:      today,
			Time:      currentTime,
			Incomes:   []Transaction{},
			Expenses:  []Transaction{},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		if txType == 1 {
			record.Incomes = append(record.Incomes, newTx)
			record.TotalIncome = tx.Amount
		} else {
			record.Expenses = append(record.Expenses, newTx)
			record.TotalExpense = tx.Amount
		}

		result, err := s.collection.InsertOne(ctx, record)
		if err != nil {
			return "", fmt.Errorf("failed to insert daily record: %w", err)
		}
		return newTx.ID.Hex(), nil
		_ = result
	} else if err != nil {
		return "", fmt.Errorf("failed to find daily record: %w", err)
	}

	// Update existing record
	var update bson.M
	if txType == 1 {
		update = bson.M{
			"$push": bson.M{"incomes": newTx},
			"$inc":  bson.M{"totalIncome": tx.Amount},
			"$set":  bson.M{"updatedAt": time.Now()},
		}
	} else {
		update = bson.M{
			"$push": bson.M{"expenses": newTx},
			"$inc":  bson.M{"totalExpense": tx.Amount},
			"$set":  bson.M{"updatedAt": time.Now()},
		}
	}

	_, err = s.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return "", fmt.Errorf("failed to update daily record: %w", err)
	}

	return newTx.ID.Hex(), nil
}

// DeleteTransaction removes a transaction from the daily record
func (s *MongoDBService) DeleteTransaction(ctx context.Context, lineID, txID string) error {
	objectID, err := primitive.ObjectIDFromHex(txID)
	if err != nil {
		return fmt.Errorf("invalid transaction ID: %w", err)
	}

	today := time.Now().Format("2006-01-02")
	filter := bson.M{
		"lineid": lineID,
		"date":   today,
	}

	// Try to find and remove from incomes
	updateIncome := bson.M{
		"$pull": bson.M{"incomes": bson.M{"_id": objectID}},
		"$set":  bson.M{"updatedAt": time.Now()},
	}

	result, err := s.collection.UpdateOne(ctx, filter, updateIncome)
	if err != nil {
		return fmt.Errorf("failed to delete from incomes: %w", err)
	}

	if result.ModifiedCount == 0 {
		// Try to remove from expenses
		updateExpense := bson.M{
			"$pull": bson.M{"expenses": bson.M{"_id": objectID}},
			"$set":  bson.M{"updatedAt": time.Now()},
		}

		_, err = s.collection.UpdateOne(ctx, filter, updateExpense)
		if err != nil {
			return fmt.Errorf("failed to delete from expenses: %w", err)
		}
	}

	// Recalculate totals
	return s.recalculateTotals(ctx, lineID, today)
}

func (s *MongoDBService) recalculateTotals(ctx context.Context, lineID, date string) error {
	filter := bson.M{
		"lineid": lineID,
		"date":   date,
	}

	var record DailyRecord
	if err := s.collection.FindOne(ctx, filter).Decode(&record); err != nil {
		return err
	}

	var totalIncome, totalExpense float64
	for _, tx := range record.Incomes {
		totalIncome += tx.Amount
	}
	for _, tx := range record.Expenses {
		totalExpense += tx.Amount
	}

	update := bson.M{
		"$set": bson.M{
			"totalIncome":  totalIncome,
			"totalExpense": totalExpense,
			"updatedAt":    time.Now(),
		},
	}

	_, err := s.collection.UpdateOne(ctx, filter, update)
	return err
}

// BalanceSummary represents the balance information
type BalanceSummary struct {
	TotalIncome    float64 `json:"totalIncome"`
	TotalExpense   float64 `json:"totalExpense"`
	Balance        float64 `json:"balance"`
	TodayIncome    float64 `json:"todayIncome"`
	TodayExpense   float64 `json:"todayExpense"`
	TodayBalance   float64 `json:"todayBalance"`
}

// GetBalanceSummary returns the balance summary for a user
// Note: Excludes "โอนเงิน" (transfers) as they don't affect actual balance
func (s *MongoDBService) GetBalanceSummary(ctx context.Context, lineID string) (*BalanceSummary, error) {
	today := time.Now().Format("2006-01-02")

	// Get all records for this user
	filter := bson.M{"lineid": lineID}
	cursor, err := s.collection.Find(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to find records: %w", err)
	}
	defer cursor.Close(ctx)

	var totalIncome, totalExpense float64
	var todayIncome, todayExpense float64

	for cursor.Next(ctx) {
		var record DailyRecord
		if err := cursor.Decode(&record); err != nil {
			continue
		}

		// Calculate from individual transactions, excluding transfers
		for _, tx := range record.Incomes {
			if tx.Category == "โอนเงิน" {
				continue // Skip transfer income
			}
			totalIncome += tx.Amount
			if record.Date == today {
				todayIncome += tx.Amount
			}
		}

		for _, tx := range record.Expenses {
			if tx.Category == "โอนเงิน" {
				continue // Skip transfer expense
			}
			totalExpense += tx.Amount
			if record.Date == today {
				todayExpense += tx.Amount
			}
		}
	}

	return &BalanceSummary{
		TotalIncome:  totalIncome,
		TotalExpense: totalExpense,
		Balance:      totalIncome - totalExpense,
		TodayIncome:  todayIncome,
		TodayExpense: todayExpense,
		TodayBalance: todayIncome - todayExpense,
	}, nil
}

// SaveChatMessage saves a chat message to history
func (s *MongoDBService) SaveChatMessage(ctx context.Context, lineID, role, content string) error {
	filter := bson.M{"lineid": lineID}

	msg := ChatMessage{
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
	}

	update := bson.M{
		"$push": bson.M{
			"messages": bson.M{
				"$each":  []ChatMessage{msg},
				"$slice": -20, // Keep only last 20 messages
			},
		},
		"$set": bson.M{
			"updatedAt": time.Now(),
		},
		"$setOnInsert": bson.M{
			"lineid": lineID,
		},
	}

	opts := options.Update().SetUpsert(true)
	_, err := s.chatCollection.UpdateOne(ctx, filter, update, opts)
	return err
}

// GetChatHistory returns recent chat messages for a user
func (s *MongoDBService) GetChatHistory(ctx context.Context, lineID string, limit int) ([]ChatMessage, error) {
	filter := bson.M{"lineid": lineID}

	var userChat UserChat
	err := s.chatCollection.FindOne(ctx, filter).Decode(&userChat)
	if err == mongo.ErrNoDocuments {
		return []ChatMessage{}, nil
	}
	if err != nil {
		return nil, err
	}

	// Return last N messages
	messages := userChat.Messages
	if len(messages) > limit {
		messages = messages[len(messages)-limit:]
	}

	return messages, nil
}

// GetLastTransaction returns the last transaction for a user (for update reference)
func (s *MongoDBService) GetLastTransaction(ctx context.Context, lineID string) (*Transaction, string, error) {
	today := time.Now().Format("2006-01-02")
	filter := bson.M{
		"lineid": lineID,
		"date":   today,
	}

	var record DailyRecord
	err := s.collection.FindOne(ctx, filter).Decode(&record)
	if err != nil {
		return nil, "", err
	}

	// Check expenses first (more common)
	if len(record.Expenses) > 0 {
		lastTx := record.Expenses[len(record.Expenses)-1]
		return &lastTx, "expense", nil
	}

	// Then check incomes
	if len(record.Incomes) > 0 {
		lastTx := record.Incomes[len(record.Incomes)-1]
		return &lastTx, "income", nil
	}

	return nil, "", fmt.Errorf("no transactions found")
}

// UpdateTransactionPayment updates the payment method of a transaction
func (s *MongoDBService) UpdateTransactionPayment(ctx context.Context, lineID, txID string, useType int, bankName, creditCardName string) (*Transaction, error) {
	objectID, err := primitive.ObjectIDFromHex(txID)
	if err != nil {
		return nil, fmt.Errorf("invalid transaction ID: %w", err)
	}

	today := time.Now().Format("2006-01-02")

	// Try updating in expenses
	filter := bson.M{
		"lineid":       lineID,
		"date":         today,
		"expenses._id": objectID,
	}

	update := bson.M{
		"$set": bson.M{
			"expenses.$.usetype":        useType,
			"expenses.$.bankname":       bankName,
			"expenses.$.creditcardname": creditCardName,
			"updatedAt":                 time.Now(),
		},
	}

	result, err := s.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return nil, err
	}

	if result.ModifiedCount == 0 {
		// Try updating in incomes
		filter = bson.M{
			"lineid":      lineID,
			"date":        today,
			"incomes._id": objectID,
		}

		update = bson.M{
			"$set": bson.M{
				"incomes.$.usetype":        useType,
				"incomes.$.bankname":       bankName,
				"incomes.$.creditcardname": creditCardName,
				"updatedAt":                time.Now(),
			},
		}

		_, err = s.collection.UpdateOne(ctx, filter, update)
		if err != nil {
			return nil, err
		}
	}

	// Return updated transaction
	return s.GetTransactionByID(ctx, lineID, txID)
}

// UpdateTransactionAmount updates the amount of a transaction
func (s *MongoDBService) UpdateTransactionAmount(ctx context.Context, lineID, txID string, amount float64) error {
	objectID, err := primitive.ObjectIDFromHex(txID)
	if err != nil {
		return fmt.Errorf("invalid transaction ID: %w", err)
	}

	today := time.Now().Format("2006-01-02")

	// Try updating in expenses
	filter := bson.M{
		"lineid":       lineID,
		"date":         today,
		"expenses._id": objectID,
	}

	update := bson.M{
		"$set": bson.M{
			"expenses.$.amount": amount,
			"updatedAt":         time.Now(),
		},
	}

	result, err := s.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}

	if result.ModifiedCount == 0 {
		// Try updating in incomes
		filter = bson.M{
			"lineid":      lineID,
			"date":        today,
			"incomes._id": objectID,
		}

		update = bson.M{
			"$set": bson.M{
				"incomes.$.amount": amount,
				"updatedAt":        time.Now(),
			},
		}

		_, err = s.collection.UpdateOne(ctx, filter, update)
		if err != nil {
			return err
		}
	}

	// Recalculate totals
	return s.recalculateTotals(ctx, lineID, today)
}

// GetTransactionByID returns a transaction by its ID
func (s *MongoDBService) GetTransactionByID(ctx context.Context, lineID, txID string) (*Transaction, error) {
	objectID, err := primitive.ObjectIDFromHex(txID)
	if err != nil {
		return nil, fmt.Errorf("invalid transaction ID: %w", err)
	}

	today := time.Now().Format("2006-01-02")
	filter := bson.M{
		"lineid": lineID,
		"date":   today,
	}

	var record DailyRecord
	err = s.collection.FindOne(ctx, filter).Decode(&record)
	if err != nil {
		return nil, err
	}

	// Search in expenses
	for _, tx := range record.Expenses {
		if tx.ID == objectID {
			return &tx, nil
		}
	}

	// Search in incomes
	for _, tx := range record.Incomes {
		if tx.ID == objectID {
			return &tx, nil
		}
	}

	return nil, fmt.Errorf("transaction not found")
}

// PaymentMethod represents a payment method with name
type PaymentMethod struct {
	UseType        int    `json:"usetype"` // 0=เงินสด, 1=บัตรเครดิต, 2=ธนาคาร
	BankName       string `json:"bankname"`
	CreditCardName string `json:"creditcardname"`
}

// PaymentBalance represents balance for each payment method
type PaymentBalance struct {
	UseType        int     `json:"usetype"`
	BankName       string  `json:"bankname"`
	CreditCardName string  `json:"creditcardname"`
	TotalIncome    float64 `json:"totalIncome"`
	TotalExpense   float64 `json:"totalExpense"`
	Balance        float64 `json:"balance"`
}

// GetDistinctPaymentMethods returns unique banks and credit cards for a user
func (s *MongoDBService) GetDistinctPaymentMethods(ctx context.Context, lineID string) ([]string, []string, error) {
	filter := bson.M{"lineid": lineID}
	cursor, err := s.collection.Find(ctx, filter)
	if err != nil {
		return nil, nil, err
	}
	defer cursor.Close(ctx)

	bankSet := make(map[string]bool)
	creditCardSet := make(map[string]bool)

	for cursor.Next(ctx) {
		var record DailyRecord
		if err := cursor.Decode(&record); err != nil {
			continue
		}

		// Check record-level payment info
		if record.BankName != "" {
			bankSet[record.BankName] = true
		}
		if record.CreditCardName != "" {
			creditCardSet[record.CreditCardName] = true
		}

		// Check transaction-level payment info
		for _, tx := range record.Incomes {
			if tx.BankName != "" {
				bankSet[tx.BankName] = true
			}
			if tx.CreditCardName != "" {
				creditCardSet[tx.CreditCardName] = true
			}
		}
		for _, tx := range record.Expenses {
			if tx.BankName != "" {
				bankSet[tx.BankName] = true
			}
			if tx.CreditCardName != "" {
				creditCardSet[tx.CreditCardName] = true
			}
		}
	}

	banks := make([]string, 0, len(bankSet))
	for bank := range bankSet {
		banks = append(banks, bank)
	}

	creditCards := make([]string, 0, len(creditCardSet))
	for cc := range creditCardSet {
		creditCards = append(creditCards, cc)
	}

	return banks, creditCards, nil
}

// GetDistinctCategories returns unique categories for a user
func (s *MongoDBService) GetDistinctCategories(ctx context.Context, lineID string) ([]string, []string, error) {
	filter := bson.M{"lineid": lineID}
	cursor, err := s.collection.Find(ctx, filter)
	if err != nil {
		return nil, nil, err
	}
	defer cursor.Close(ctx)

	incomeCategories := make(map[string]bool)
	expenseCategories := make(map[string]bool)

	for cursor.Next(ctx) {
		var record DailyRecord
		if err := cursor.Decode(&record); err != nil {
			continue
		}

		for _, tx := range record.Incomes {
			if tx.Category != "" && tx.Category != "โอนเงิน" {
				incomeCategories[tx.Category] = true
			}
		}
		for _, tx := range record.Expenses {
			if tx.Category != "" && tx.Category != "โอนเงิน" {
				expenseCategories[tx.Category] = true
			}
		}
	}

	incomes := make([]string, 0, len(incomeCategories))
	for cat := range incomeCategories {
		incomes = append(incomes, cat)
	}

	expenses := make([]string, 0, len(expenseCategories))
	for cat := range expenseCategories {
		expenses = append(expenses, cat)
	}

	return incomes, expenses, nil
}

// GetBalanceByPaymentType returns balance breakdown by payment type
// การคำนวณ: balance = sum(amount * type) โดย type=1 คือ income, type=-1 คือ expense
func (s *MongoDBService) GetBalanceByPaymentType(ctx context.Context, lineID string) ([]PaymentBalance, error) {
	filter := bson.M{"lineid": lineID}
	cursor, err := s.collection.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	// Key: "usetype:bankname:creditcardname"
	balanceMap := make(map[string]*PaymentBalance)

	for cursor.Next(ctx) {
		var record DailyRecord
		if err := cursor.Decode(&record); err != nil {
			continue
		}

		// Process all transactions (both incomes and expenses arrays)
		allTx := append(record.Incomes, record.Expenses...)
		for _, tx := range allTx {
			key := fmt.Sprintf("%d:%s:%s", tx.UseType, tx.BankName, tx.CreditCardName)
			if _, exists := balanceMap[key]; !exists {
				balanceMap[key] = &PaymentBalance{
					UseType:        tx.UseType,
					BankName:       tx.BankName,
					CreditCardName: tx.CreditCardName,
				}
			}
			// คำนวณ: amount * type (type=1 รายรับ, type=-1 รายจ่าย)
			balanceMap[key].Balance += tx.Amount * float64(tx.Type)

			// เก็บ income/expense แยกสำหรับแสดงรายละเอียด
			if tx.Type == 1 {
				balanceMap[key].TotalIncome += tx.Amount
			} else {
				balanceMap[key].TotalExpense += tx.Amount
			}
		}
	}

	// Convert to slice
	result := make([]PaymentBalance, 0, len(balanceMap))
	for _, pb := range balanceMap {
		result = append(result, *pb)
	}

	return result, nil
}

// SaveTransfer saves a transfer and creates corresponding transactions
// Returns transfer ID and array of transaction IDs
func (s *MongoDBService) SaveTransfer(ctx context.Context, lineID string, transfer *TransferData) (string, []string, error) {
	today := time.Now().Format("2006-01-02")

	// Calculate total amount from "from" entries
	var totalAmount float64
	for _, entry := range transfer.From {
		totalAmount += entry.Amount
	}

	// Convert to DB format
	fromEntries := make([]TransferEntryDB, len(transfer.From))
	for i, e := range transfer.From {
		fromEntries[i] = TransferEntryDB{
			Amount:         e.Amount,
			UseType:        e.UseType,
			BankName:       e.BankName,
			CreditCardName: e.CreditCardName,
		}
	}

	toEntries := make([]TransferEntryDB, len(transfer.To))
	for i, e := range transfer.To {
		toEntries[i] = TransferEntryDB{
			Amount:         e.Amount,
			UseType:        e.UseType,
			BankName:       e.BankName,
			CreditCardName: e.CreditCardName,
		}
	}

	// Create transfer record
	transferRecord := TransferRecord{
		ID:          primitive.NewObjectID(),
		LineID:      lineID,
		Date:        today,
		Description: transfer.Description,
		From:        fromEntries,
		To:          toEntries,
		TotalAmount: totalAmount,
		CreatedAt:   time.Now(),
	}

	// Save transfer record
	_, err := s.transferCollection.InsertOne(ctx, transferRecord)
	if err != nil {
		return "", nil, fmt.Errorf("failed to save transfer: %w", err)
	}

	transferID := transferRecord.ID.Hex()
	var txIDs []string

	// Create expense transactions for "from" entries (money going out)
	for _, entry := range transfer.From {
		txData := &TransactionData{
			Type:           "expense",
			Amount:         entry.Amount,
			Category:       "โอนเงิน",
			Description:    transfer.Description,
			UseType:        entry.UseType,
			BankName:       entry.BankName,
			CreditCardName: entry.CreditCardName,
		}
		txID, err := s.saveTransactionWithTransferID(ctx, lineID, txData, transferID)
		if err != nil {
			log.Printf("Failed to save from transaction: %v", err)
			continue
		}
		txIDs = append(txIDs, txID)
	}

	// Create income transactions for "to" entries (money coming in)
	for _, entry := range transfer.To {
		txData := &TransactionData{
			Type:           "income",
			Amount:         entry.Amount,
			Category:       "โอนเงิน",
			Description:    transfer.Description,
			UseType:        entry.UseType,
			BankName:       entry.BankName,
			CreditCardName: entry.CreditCardName,
		}
		txID, err := s.saveTransactionWithTransferID(ctx, lineID, txData, transferID)
		if err != nil {
			log.Printf("Failed to save to transaction: %v", err)
			continue
		}
		txIDs = append(txIDs, txID)
	}

	return transferID, txIDs, nil
}

// saveTransactionWithTransferID saves a transaction with transfer_id
func (s *MongoDBService) saveTransactionWithTransferID(ctx context.Context, lineID string, tx *TransactionData, transferID string) (string, error) {
	today := time.Now().Format("2006-01-02")
	currentTime := time.Now().Format("15:04")

	txType := -1
	if tx.Type == "income" {
		txType = 1
	}

	newTx := Transaction{
		ID:             primitive.NewObjectID(),
		Type:           txType,
		CustName:       tx.Merchant,
		Amount:         tx.Amount,
		Category:       tx.Category,
		Description:    tx.Description,
		UseType:        tx.UseType,
		BankName:       tx.BankName,
		CreditCardName: tx.CreditCardName,
		TransferID:     transferID,
		CreatedAt:      time.Now(),
	}

	filter := bson.M{
		"lineid": lineID,
		"date":   today,
	}

	var record DailyRecord
	err := s.collection.FindOne(ctx, filter).Decode(&record)

	if err == mongo.ErrNoDocuments {
		record = DailyRecord{
			LineID:    lineID,
			Date:      today,
			Time:      currentTime,
			Incomes:   []Transaction{},
			Expenses:  []Transaction{},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		if txType == 1 {
			record.Incomes = append(record.Incomes, newTx)
			record.TotalIncome = tx.Amount
		} else {
			record.Expenses = append(record.Expenses, newTx)
			record.TotalExpense = tx.Amount
		}

		_, err := s.collection.InsertOne(ctx, record)
		if err != nil {
			return "", fmt.Errorf("failed to insert daily record: %w", err)
		}
		return newTx.ID.Hex(), nil
	} else if err != nil {
		return "", fmt.Errorf("failed to find daily record: %w", err)
	}

	var update bson.M
	if txType == 1 {
		update = bson.M{
			"$push": bson.M{"incomes": newTx},
			"$inc":  bson.M{"totalIncome": tx.Amount},
			"$set":  bson.M{"updatedAt": time.Now()},
		}
	} else {
		update = bson.M{
			"$push": bson.M{"expenses": newTx},
			"$inc":  bson.M{"totalExpense": tx.Amount},
			"$set":  bson.M{"updatedAt": time.Now()},
		}
	}

	_, err = s.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return "", fmt.Errorf("failed to update daily record: %w", err)
	}

	return newTx.ID.Hex(), nil
}

// GetTransferByID returns a transfer by its ID
func (s *MongoDBService) GetTransferByID(ctx context.Context, transferID string) (*TransferRecord, error) {
	objectID, err := primitive.ObjectIDFromHex(transferID)
	if err != nil {
		return nil, fmt.Errorf("invalid transfer ID: %w", err)
	}

	filter := bson.M{"_id": objectID}
	var transfer TransferRecord
	err = s.transferCollection.FindOne(ctx, filter).Decode(&transfer)
	if err != nil {
		return nil, err
	}
	return &transfer, nil
}

// DeleteTransfer deletes a transfer and its related transactions
func (s *MongoDBService) DeleteTransfer(ctx context.Context, lineID, transferID string) error {
	today := time.Now().Format("2006-01-02")

	// Delete from incomes where transfer_id matches
	filterIncome := bson.M{
		"lineid": lineID,
		"date":   today,
	}
	updateIncome := bson.M{
		"$pull": bson.M{"incomes": bson.M{"transfer_id": transferID}},
		"$set":  bson.M{"updatedAt": time.Now()},
	}
	s.collection.UpdateOne(ctx, filterIncome, updateIncome)

	// Delete from expenses where transfer_id matches
	updateExpense := bson.M{
		"$pull": bson.M{"expenses": bson.M{"transfer_id": transferID}},
		"$set":  bson.M{"updatedAt": time.Now()},
	}
	s.collection.UpdateOne(ctx, filterIncome, updateExpense)

	// Delete transfer record
	objectID, err := primitive.ObjectIDFromHex(transferID)
	if err != nil {
		return fmt.Errorf("invalid transfer ID: %w", err)
	}
	s.transferCollection.DeleteOne(ctx, bson.M{"_id": objectID})

	// Recalculate totals
	return s.recalculateTotals(ctx, lineID, today)
}

// SearchResult represents a search result with full transaction details
type SearchResult struct {
	Transaction Transaction `json:"transaction"`
	Date        string      `json:"date"`      // date from daily record
	RecordID    string      `json:"record_id"` // ID of the daily record
}

// SearchTransactions searches transactions by keyword across description, category, custname
// Returns matching transactions with their dates
func (s *MongoDBService) SearchTransactions(ctx context.Context, lineID, keyword string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 20
	}

	// Build regex pattern for case-insensitive search
	filter := bson.M{
		"lineid": lineID,
		"$or": []bson.M{
			{"incomes.description": bson.M{"$regex": keyword, "$options": "i"}},
			{"incomes.category": bson.M{"$regex": keyword, "$options": "i"}},
			{"incomes.custname": bson.M{"$regex": keyword, "$options": "i"}},
			{"expenses.description": bson.M{"$regex": keyword, "$options": "i"}},
			{"expenses.category": bson.M{"$regex": keyword, "$options": "i"}},
			{"expenses.custname": bson.M{"$regex": keyword, "$options": "i"}},
		},
	}

	// Sort by date descending (newest first)
	opts := options.Find().SetSort(bson.D{{Key: "date", Value: -1}})
	cursor, err := s.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []SearchResult

	for cursor.Next(ctx) {
		var record DailyRecord
		if err := cursor.Decode(&record); err != nil {
			continue
		}

		// Search in incomes
		for _, tx := range record.Incomes {
			if matchesKeyword(tx, keyword) {
				results = append(results, SearchResult{
					Transaction: tx,
					Date:        record.Date,
					RecordID:    record.ID.Hex(),
				})
				if len(results) >= limit {
					break
				}
			}
		}

		// Search in expenses
		for _, tx := range record.Expenses {
			if matchesKeyword(tx, keyword) {
				results = append(results, SearchResult{
					Transaction: tx,
					Date:        record.Date,
					RecordID:    record.ID.Hex(),
				})
				if len(results) >= limit {
					break
				}
			}
		}

		if len(results) >= limit {
			break
		}
	}

	return results, nil
}

// matchesKeyword checks if a transaction matches the keyword
func matchesKeyword(tx Transaction, keyword string) bool {
	keyword = strings.ToLower(keyword)
	return strings.Contains(strings.ToLower(tx.Description), keyword) ||
		strings.Contains(strings.ToLower(tx.Category), keyword) ||
		strings.Contains(strings.ToLower(tx.CustName), keyword)
}

// SearchByCategory searches transactions by category
func (s *MongoDBService) SearchByCategory(ctx context.Context, lineID, category string, limit int) ([]SearchResult, error) {
	return s.SearchTransactions(ctx, lineID, category, limit)
}

// SearchByDateRange searches transactions within a date range
func (s *MongoDBService) SearchByDateRange(ctx context.Context, lineID, startDate, endDate string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 50
	}

	filter := bson.M{
		"lineid": lineID,
		"date": bson.M{
			"$gte": startDate,
			"$lte": endDate,
		},
	}

	opts := options.Find().SetSort(bson.D{{Key: "date", Value: -1}})
	cursor, err := s.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []SearchResult

	for cursor.Next(ctx) {
		var record DailyRecord
		if err := cursor.Decode(&record); err != nil {
			continue
		}

		// Add all incomes
		for _, tx := range record.Incomes {
			results = append(results, SearchResult{
				Transaction: tx,
				Date:        record.Date,
				RecordID:    record.ID.Hex(),
			})
		}

		// Add all expenses
		for _, tx := range record.Expenses {
			results = append(results, SearchResult{
				Transaction: tx,
				Date:        record.Date,
				RecordID:    record.ID.Hex(),
			})
		}

		if len(results) >= limit {
			break
		}
	}

	return results, nil
}

// GetTransactionSummaryText returns a text summary of search results for AI context
func (s *MongoDBService) GetTransactionSummaryText(results []SearchResult) string {
	if len(results) == 0 {
		return "ไม่พบรายการที่ค้นหา"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("พบ %d รายการ:\n", len(results)))

	for i, r := range results {
		if i >= 10 { // Limit to first 10 for AI context
			sb.WriteString(fmt.Sprintf("...และอีก %d รายการ\n", len(results)-10))
			break
		}

		typeStr := "รายจ่าย"
		if r.Transaction.Type == 1 {
			typeStr = "รายรับ"
		}

		sb.WriteString(fmt.Sprintf("- %s: %s %.0f บาท (%s) วันที่ %s\n",
			typeStr,
			r.Transaction.Description,
			r.Transaction.Amount,
			r.Transaction.Category,
			r.Date,
		))
	}

	// Calculate total
	var totalIncome, totalExpense float64
	for _, r := range results {
		if r.Transaction.Type == 1 {
			totalIncome += r.Transaction.Amount
		} else {
			totalExpense += r.Transaction.Amount
		}
	}

	if totalIncome > 0 {
		sb.WriteString(fmt.Sprintf("รวมรายรับ: %.0f บาท\n", totalIncome))
	}
	if totalExpense > 0 {
		sb.WriteString(fmt.Sprintf("รวมรายจ่าย: %.0f บาท\n", totalExpense))
	}

	return sb.String()
}

// GetRecentTransactionsContext returns recent transactions (last N days) as text context for AI
// Excludes base64 images to keep context small
func (s *MongoDBService) GetRecentTransactionsContext(ctx context.Context, lineID string, days int) string {
	if days <= 0 {
		days = 7
	}

	// Calculate date range
	endDate := time.Now().Format("2006-01-02")
	startDate := time.Now().AddDate(0, 0, -days).Format("2006-01-02")

	filter := bson.M{
		"lineid": lineID,
		"date": bson.M{
			"$gte": startDate,
			"$lte": endDate,
		},
	}

	opts := options.Find().SetSort(bson.D{{Key: "date", Value: -1}})
	cursor, err := s.collection.Find(ctx, filter, opts)
	if err != nil {
		return ""
	}
	defer cursor.Close(ctx)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("รายการ %d วันล่าสุด:\n", days))

	totalIncome := 0.0
	totalExpense := 0.0
	txCount := 0

	for cursor.Next(ctx) {
		var record DailyRecord
		if err := cursor.Decode(&record); err != nil {
			continue
		}

		// Process incomes
		for _, tx := range record.Incomes {
			if txCount < 30 { // Limit to 30 transactions for context
				desc := tx.Description
				if desc == "" {
					desc = tx.Category
				}
				paymentInfo := getPaymentInfo(tx.UseType, tx.BankName, tx.CreditCardName)
				sb.WriteString(fmt.Sprintf("- %s: รายรับ %.0f บาท (%s) %s\n", record.Date, tx.Amount, desc, paymentInfo))
				txCount++
			}
			totalIncome += tx.Amount
		}

		// Process expenses
		for _, tx := range record.Expenses {
			if txCount < 30 {
				desc := tx.Description
				if desc == "" {
					desc = tx.Category
				}
				paymentInfo := getPaymentInfo(tx.UseType, tx.BankName, tx.CreditCardName)
				sb.WriteString(fmt.Sprintf("- %s: รายจ่าย %.0f บาท (%s) %s\n", record.Date, tx.Amount, desc, paymentInfo))
				txCount++
			}
			totalExpense += tx.Amount
		}
	}

	if txCount == 0 {
		return "ไม่มีรายการในช่วง 7 วันที่ผ่านมา"
	}

	sb.WriteString(fmt.Sprintf("\nสรุป %d วัน: รายรับ %.0f บาท, รายจ่าย %.0f บาท, คงเหลือ %.0f บาท",
		days, totalIncome, totalExpense, totalIncome-totalExpense))

	return sb.String()
}

// getPaymentInfo returns payment method info string
func getPaymentInfo(useType int, bankName, creditCardName string) string {
	switch useType {
	case 1:
		if creditCardName != "" {
			return "บัตร" + creditCardName
		}
		return "บัตรเครดิต"
	case 2:
		if bankName != "" {
			return "ธ." + bankName
		}
		return "ธนาคาร"
	}
	return "เงินสด"
}

// SetBudget creates or updates a category budget
func (s *MongoDBService) SetBudget(ctx context.Context, lineID, category string, amount float64) error {
	filter := bson.M{
		"lineid":   lineID,
		"category": category,
	}

	update := bson.M{
		"$set": bson.M{
			"amount":     amount,
			"updated_at": time.Now(),
		},
		"$setOnInsert": bson.M{
			"lineid":     lineID,
			"category":   category,
			"created_at": time.Now(),
		},
	}

	opts := options.Update().SetUpsert(true)
	_, err := s.budgetCollection.UpdateOne(ctx, filter, update, opts)
	return err
}

// GetBudget returns budget for a specific category
func (s *MongoDBService) GetBudget(ctx context.Context, lineID, category string) (*Budget, error) {
	filter := bson.M{
		"lineid":   lineID,
		"category": category,
	}

	var budget Budget
	err := s.budgetCollection.FindOne(ctx, filter).Decode(&budget)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &budget, nil
}

// GetAllBudgets returns all budgets for a user
func (s *MongoDBService) GetAllBudgets(ctx context.Context, lineID string) ([]Budget, error) {
	filter := bson.M{"lineid": lineID}
	cursor, err := s.budgetCollection.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var budgets []Budget
	if err := cursor.All(ctx, &budgets); err != nil {
		return nil, err
	}
	return budgets, nil
}

// DeleteBudget removes a category budget
func (s *MongoDBService) DeleteBudget(ctx context.Context, lineID, category string) error {
	filter := bson.M{
		"lineid":   lineID,
		"category": category,
	}
	_, err := s.budgetCollection.DeleteOne(ctx, filter)
	return err
}

// GetMonthlySpendingByCategory returns spending by category for current month
func (s *MongoDBService) GetMonthlySpendingByCategory(ctx context.Context, lineID string) (map[string]float64, error) {
	// Get first and last day of current month
	now := time.Now()
	firstDay := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	lastDay := firstDay.AddDate(0, 1, -1)

	filter := bson.M{
		"lineid": lineID,
		"date": bson.M{
			"$gte": firstDay.Format("2006-01-02"),
			"$lte": lastDay.Format("2006-01-02"),
		},
	}

	cursor, err := s.collection.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	spendingByCategory := make(map[string]float64)

	for cursor.Next(ctx) {
		var record DailyRecord
		if err := cursor.Decode(&record); err != nil {
			continue
		}

		// Sum expenses by category (exclude transfers)
		for _, tx := range record.Expenses {
			category := tx.Category
			if category == "" {
				category = "อื่นๆ"
			}
			// Skip transfer transactions - they're not real expenses
			if category == "โอนเงิน" {
				continue
			}
			spendingByCategory[category] += tx.Amount
		}
	}

	return spendingByCategory, nil
}

// GetBudgetStatus returns budget status with spending comparison
func (s *MongoDBService) GetBudgetStatus(ctx context.Context, lineID string) ([]BudgetStatus, error) {
	// Get all budgets
	budgets, err := s.GetAllBudgets(ctx, lineID)
	if err != nil {
		return nil, err
	}

	if len(budgets) == 0 {
		return []BudgetStatus{}, nil
	}

	// Get monthly spending
	spending, err := s.GetMonthlySpendingByCategory(ctx, lineID)
	if err != nil {
		return nil, err
	}

	var statuses []BudgetStatus
	for _, budget := range budgets {
		spent := spending[budget.Category]
		remaining := budget.Amount - spent
		percentage := 0.0
		if budget.Amount > 0 {
			percentage = (spent / budget.Amount) * 100
		}

		statuses = append(statuses, BudgetStatus{
			Category:     budget.Category,
			Budget:       budget.Amount,
			Spent:        spent,
			Remaining:    remaining,
			Percentage:   percentage,
			IsOverBudget: spent > budget.Amount,
		})
	}

	return statuses, nil
}

// CheckBudgetAlert checks if a category is over budget and returns alert message
func (s *MongoDBService) CheckBudgetAlert(ctx context.Context, lineID, category string, newAmount float64) (bool, string) {
	budget, err := s.GetBudget(ctx, lineID, category)
	if err != nil || budget == nil {
		return false, "" // No budget set for this category
	}

	// Get current month spending for this category
	spending, err := s.GetMonthlySpendingByCategory(ctx, lineID)
	if err != nil {
		return false, ""
	}

	currentSpent := spending[category]
	totalAfterNew := currentSpent + newAmount
	percentage := (totalAfterNew / budget.Amount) * 100

	if totalAfterNew > budget.Amount {
		return true, fmt.Sprintf("⚠️ งบหมวด %s เกิน! (%.0f/%.0f บาท = %.0f%%)",
			category, totalAfterNew, budget.Amount, percentage)
	}

	if percentage >= 80 {
		return true, fmt.Sprintf("⚡ งบหมวด %s ใกล้หมด! (%.0f/%.0f บาท = %.0f%%)",
			category, totalAfterNew, budget.Amount, percentage)
	}

	return false, ""
}

// GetBudgetSummaryText returns budget summary as text for AI context
func (s *MongoDBService) GetBudgetSummaryText(ctx context.Context, lineID string) string {
	statuses, err := s.GetBudgetStatus(ctx, lineID)
	if err != nil || len(statuses) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("งบประมาณเดือนนี้:\n")

	for _, status := range statuses {
		emoji := "✅"
		if status.IsOverBudget {
			emoji = "🔴"
		} else if status.Percentage >= 80 {
			emoji = "🟡"
		}

		sb.WriteString(fmt.Sprintf("%s %s: %.0f/%.0f บาท (%.0f%%)\n",
			emoji, status.Category, status.Spent, status.Budget, status.Percentage))
	}

	return sb.String()
}

func (s *MongoDBService) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.client.Disconnect(ctx)
}
