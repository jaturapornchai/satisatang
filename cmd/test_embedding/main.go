package main

import (
	"context"
	"fmt"
	"time"

	"github.com/satisatang/backend/services"
)

func main() {
	fmt.Println("=== Embedding API Test ===\n")

	ai := services.NewAIService()
	defer ai.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Test 1: Check availability
	fmt.Println("Test 1: Checking API availability...")
	if ai.IsEmbeddingAvailable(ctx) {
		fmt.Println("✓ Embedding API is available\n")
	} else {
		fmt.Println("✗ Embedding API is NOT available")
		return
	}

	// Test 2: Generate embeddings for various texts
	testTexts := []string{
		"กินข้าว 50 บาท",
		"เติมน้ำมัน 1500 บาท",
		"เงินเดือน 30000 บาท",
		"ค่า Netflix 419 บาท",
		"ซื้อของ Shopee 350 บาท",
	}

	fmt.Println("Test 2: Generating embeddings...")
	for i, text := range testTexts {
		embedding, err := ai.GenerateEmbedding(ctx, text)
		if err != nil {
			fmt.Printf("  ✗ Text %d failed: %v\n", i+1, err)
			continue
		}
		fmt.Printf("  ✓ Text %d: \"%s\" -> %d dimensions (first 5: [%.4f, %.4f, %.4f, %.4f, %.4f])\n",
			i+1, text, len(embedding),
			embedding[0], embedding[1], embedding[2], embedding[3], embedding[4])

		// Small delay
		time.Sleep(200 * time.Millisecond)
	}

	// Test 3: Check consistency
	fmt.Println("\nTest 3: Checking embedding consistency...")
	text := "กินข้าว 50 บาท"
	emb1, _ := ai.GenerateEmbedding(ctx, text)
	time.Sleep(200 * time.Millisecond)
	emb2, _ := ai.GenerateEmbedding(ctx, text)

	if len(emb1) == len(emb2) && emb1[0] == emb2[0] && emb1[100] == emb2[100] {
		fmt.Println("  ✓ Same text produces consistent embeddings")
	} else {
		fmt.Println("  ⚠ Embeddings differ slightly (acceptable for some models)")
	}

	fmt.Println("\n=== Test Complete ===")
}
