package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"time"

	"github.com/satisatang/backend/services"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// MockEmbedding generates a random embedding vector for testing
// In production, this would call the actual embedding API
func mockEmbedding(text string, dims int) []float32 {
	// Use text hash as seed for reproducibility
	seed := int64(0)
	for _, c := range text {
		seed = seed*31 + int64(c)
	}
	r := rand.New(rand.NewSource(seed))

	embedding := make([]float32, dims)
	var magnitude float64
	for i := range embedding {
		embedding[i] = r.Float32()*2 - 1 // Random value between -1 and 1
		magnitude += float64(embedding[i] * embedding[i])
	}

	// Normalize to unit vector
	magnitude = math.Sqrt(magnitude)
	for i := range embedding {
		embedding[i] = float32(float64(embedding[i]) / magnitude)
	}

	return embedding
}

// TestTransaction represents a test transaction
type TestTransaction struct {
	Type        string
	Description string
	Amount      float64
	Category    string
}

func main() {
	// Get MongoDB URI from env
	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
		log.Println("MONGODB_URI not set, using localhost")
	}

	dbName := os.Getenv("MONGODB_DB")
	if dbName == "" {
		dbName = "satisatang_test"
	}

	// Connect to MongoDB
	mongo, err := services.NewMongoDBService(mongoURI, dbName)
	if err != nil {
		log.Fatalf("Failed to connect to MongoDB: %v", err)
	}
	defer mongo.Close()

	// Create AI service
	ai := services.NewAIService()
	defer ai.Close()

	ctx := context.Background()
	testLineID := "Utest_vector_search_" + time.Now().Format("20060102150405")

	fmt.Println("=== Vector Search Test ===")
	fmt.Printf("Test Line ID: %s\n", testLineID)
	fmt.Printf("Database: %s\n\n", dbName)

	// Check if embedding API is available
	fmt.Println("Checking embedding API availability...")
	embeddingAvailable := ai.IsEmbeddingAvailable(ctx)
	if embeddingAvailable {
		fmt.Println("✓ Embedding API is available")
	} else {
		fmt.Println("⚠ Embedding API is NOT available - using mock embeddings")
	}

	// Check if vector search is available
	fmt.Println("Checking vector search availability...")
	vectorSearchAvailable := mongo.IsVectorSearchAvailable(ctx)
	if vectorSearchAvailable {
		fmt.Println("✓ Vector Search Index is available")
	} else {
		fmt.Println("⚠ Vector Search Index is NOT available")
		fmt.Println("  See markdown/vector-search-setup.md for setup instructions")
	}
	fmt.Println()

	// Test transactions
	testTransactions := []TestTransaction{
		{"expense", "กินข้าวมันไก่", 50, "อาหาร"},
		{"expense", "กาแฟ Amazon", 65, "อาหาร"},
		{"expense", "ข้าวกล่อง 7-11", 45, "อาหาร"},
		{"expense", "ก๋วยเตี๋ยวเรือ", 60, "อาหาร"},
		{"expense", "ชานมไข่มุก", 55, "อาหาร"},
		{"expense", "เติมน้ำมัน ปตท", 1500, "เดินทาง"},
		{"expense", "ค่า Grab", 120, "เดินทาง"},
		{"expense", "ค่า BTS สยาม", 42, "เดินทาง"},
		{"expense", "ค่าแท็กซี่", 150, "เดินทาง"},
		{"expense", "Netflix", 419, "บันเทิง"},
		{"expense", "Spotify", 129, "บันเทิง"},
		{"expense", "ดูหนัง Major", 280, "บันเทิง"},
		{"expense", "ซื้อเสื้อ Uniqlo", 590, "ช้อปปิ้ง"},
		{"expense", "Shopee ซื้อของ", 350, "ช้อปปิ้ง"},
		{"expense", "Lazada หูฟัง", 899, "ช้อปปิ้ง"},
		{"income", "เงินเดือน", 30000, "เงินเดือน"},
		{"income", "โบนัส", 10000, "โบนัส"},
		{"income", "ขายของมือสอง", 500, "อื่นๆ"},
	}

	fmt.Println("Step 1: Creating test embeddings...")

	// Create embeddings for test data
	embeddingDims := 768
	for i, tx := range testTransactions {
		txType := -1
		typeStr := "รายจ่าย"
		if tx.Type == "income" {
			txType = 1
			typeStr = "รายรับ"
		}

		// Create transaction object
		transaction := &services.Transaction{
			ID:          primitive.NewObjectID(),
			Type:        txType,
			Description: tx.Description,
			Amount:      tx.Amount,
			Category:    tx.Category,
			CreatedAt:   time.Now(),
		}

		// Generate text for embedding
		text := fmt.Sprintf("%s %s %.0f บาท หมวด%s", typeStr, tx.Description, tx.Amount, tx.Category)

		// Try real embedding first, fallback to mock
		var embedding []float32
		if embeddingAvailable {
			embedding, err = ai.GenerateEmbedding(ctx, text)
			if err != nil {
				log.Printf("Real embedding failed, using mock: %v", err)
				embedding = mockEmbedding(text, embeddingDims)
			}
		} else {
			embedding = mockEmbedding(text, embeddingDims)
		}

		// Save embedding
		date := time.Now().AddDate(0, 0, -i).Format("2006-01-02") // Spread across different dates
		_, err := mongo.SaveTransactionEmbedding(ctx, testLineID, transaction, date, embedding)
		if err != nil {
			log.Printf("Failed to save embedding %d: %v", i+1, err)
			continue
		}

		fmt.Printf("  ✓ Saved: %s (%.0f บาท)\n", tx.Description, tx.Amount)

		// Small delay to avoid rate limiting
		if embeddingAvailable {
			time.Sleep(100 * time.Millisecond)
		}
	}

	fmt.Println("\nStep 2: Testing Vector Search...")

	// Test queries
	testQueries := []struct {
		Query    string
		Expected string
	}{
		{"กินข้าว", "ควรพบรายการอาหาร"},
		{"ค่าเดินทาง", "ควรพบรายการเดินทาง"},
		{"ดูหนัง ฟังเพลง", "ควรพบรายการบันเทิง"},
		{"ซื้อของออนไลน์", "ควรพบรายการช้อปปิ้ง"},
		{"รายได้", "ควรพบรายการรายรับ"},
	}

	for _, tq := range testQueries {
		fmt.Printf("\n  Query: \"%s\"\n", tq.Query)
		fmt.Printf("  Expected: %s\n", tq.Expected)

		// Generate query embedding
		var queryVector []float32
		if embeddingAvailable {
			queryVector, err = ai.GenerateEmbedding(ctx, tq.Query)
			if err != nil {
				log.Printf("Real embedding failed for query, using mock: %v", err)
				queryVector = mockEmbedding(tq.Query, embeddingDims)
			}
		} else {
			queryVector = mockEmbedding(tq.Query, embeddingDims)
		}

		// Search
		results, err := mongo.VectorSearch(ctx, testLineID, queryVector, 5)
		if err != nil {
			fmt.Printf("  ✗ Error: %v\n", err)
			fmt.Println("  Note: Vector Search requires Atlas Search Index. See markdown/vector-search-setup.md")
			continue
		}

		if len(results) == 0 {
			fmt.Println("  ⚠ No results found")
			continue
		}

		fmt.Printf("  Results (%d):\n", len(results))
		for j, r := range results {
			typeStr := "รายจ่าย"
			if r.Embedding.Type == 1 {
				typeStr = "รายรับ"
			}
			fmt.Printf("    %d. %s: %s %.0f บาท (%s) - Score: %.2f%%\n",
				j+1,
				typeStr,
				r.Embedding.Description,
				r.Embedding.Amount,
				r.Embedding.Category,
				r.Score*100,
			)
		}
	}

	fmt.Println("\n\nStep 3: Testing GetVectorSearchResultText...")

	var queryVector3 []float32
	if embeddingAvailable {
		queryVector3, err = ai.GenerateEmbedding(ctx, "ค่าอาหาร")
		if err != nil {
			queryVector3 = mockEmbedding("ค่าอาหาร", embeddingDims)
		}
	} else {
		queryVector3 = mockEmbedding("ค่าอาหาร", embeddingDims)
	}

	results, err := mongo.VectorSearch(ctx, testLineID, queryVector3, 5)
	if err != nil {
		fmt.Printf("  ✗ Error: %v\n", err)
	} else {
		resultText := mongo.GetVectorSearchResultText(results)
		fmt.Println(resultText)
	}

	// Step 4: Check embedding count
	fmt.Println("\nStep 4: Checking embedding count...")
	count, err := mongo.GetEmbeddingCount(ctx, testLineID)
	if err != nil {
		fmt.Printf("  ✗ Error: %v\n", err)
	} else {
		fmt.Printf("  Total embeddings for test user: %d\n", count)
	}

	// Step 5: Cleanup test data
	fmt.Println("\nStep 5: Cleanup test data...")
	err = mongo.DeleteAllUserEmbeddings(ctx, testLineID)
	if err != nil {
		fmt.Printf("  ✗ Error deleting test data: %v\n", err)
	} else {
		fmt.Println("  ✓ Test data cleaned up")
	}

	fmt.Println("\n=== Test Complete ===")
	fmt.Println("\nSummary:")
	if embeddingAvailable {
		fmt.Println("✓ Embedding API: Available")
	} else {
		fmt.Println("⚠ Embedding API: Not available (used mock embeddings)")
	}
	if vectorSearchAvailable {
		fmt.Println("✓ Vector Search: Available")
	} else {
		fmt.Println("⚠ Vector Search: Not available")
		fmt.Println("  → Create search index on Atlas. See markdown/vector-search-setup.md")
	}
}
