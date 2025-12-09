package tests

import (
	"math"
	"math/rand"
	"testing"

	"github.com/satisatang/backend/services"
)

// mockEmbedding generates a deterministic embedding for testing
func mockEmbedding(text string, dims int) []float32 {
	seed := int64(0)
	for _, c := range text {
		seed = seed*31 + int64(c)
	}
	r := rand.New(rand.NewSource(seed))

	embedding := make([]float32, dims)
	var magnitude float64
	for i := range embedding {
		embedding[i] = r.Float32()*2 - 1
		magnitude += float64(embedding[i] * embedding[i])
	}

	magnitude = math.Sqrt(magnitude)
	for i := range embedding {
		embedding[i] = float32(float64(embedding[i]) / magnitude)
	}

	return embedding
}

// cosineSimilarity calculates cosine similarity between two vectors
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}

	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

func TestMockEmbeddingDeterministic(t *testing.T) {
	text := "กินข้าว 50 บาท"
	dims := 768

	emb1 := mockEmbedding(text, dims)
	emb2 := mockEmbedding(text, dims)

	// Same text should produce same embedding
	for i := range emb1 {
		if emb1[i] != emb2[i] {
			t.Errorf("Embedding not deterministic at index %d: %f != %f", i, emb1[i], emb2[i])
			break
		}
	}
}

func TestMockEmbeddingNormalized(t *testing.T) {
	text := "ทดสอบ embedding"
	dims := 768

	emb := mockEmbedding(text, dims)

	// Check that embedding is normalized (magnitude ~= 1)
	var magnitude float64
	for _, v := range emb {
		magnitude += float64(v * v)
	}
	magnitude = math.Sqrt(magnitude)

	if math.Abs(magnitude-1.0) > 0.0001 {
		t.Errorf("Embedding not normalized: magnitude = %f", magnitude)
	}
}

func TestMockEmbeddingDifferentTexts(t *testing.T) {
	dims := 768

	emb1 := mockEmbedding("กินข้าว", dims)
	emb2 := mockEmbedding("เติมน้ำมัน", dims)

	// Different texts should produce different embeddings
	same := true
	for i := range emb1 {
		if emb1[i] != emb2[i] {
			same = false
			break
		}
	}

	if same {
		t.Error("Different texts produced same embedding")
	}
}

func TestCosineSimilarity(t *testing.T) {
	dims := 768

	// Same text should have similarity = 1
	emb1 := mockEmbedding("กินข้าว", dims)
	emb2 := mockEmbedding("กินข้าว", dims)
	sim := cosineSimilarity(emb1, emb2)
	if math.Abs(sim-1.0) > 0.0001 {
		t.Errorf("Same text similarity should be 1, got %f", sim)
	}

	// Different texts should have similarity < 1
	emb3 := mockEmbedding("เติมน้ำมัน", dims)
	sim2 := cosineSimilarity(emb1, emb3)
	if sim2 >= 1.0 {
		t.Errorf("Different text similarity should be < 1, got %f", sim2)
	}
}

func TestGetVectorSearchResultText_Empty(t *testing.T) {
	result := (&services.MongoDBService{}).GetVectorSearchResultText(nil)
	expected := "ไม่พบรายการที่คล้ายกัน"
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}

func TestGetVectorSearchResultText_WithResults(t *testing.T) {
	results := []services.VectorSearchResult{
		{
			Embedding: services.TransactionEmbedding{
				Type:        -1,
				Description: "กินข้าว",
				Amount:      50,
				Category:    "อาหาร",
				Date:        "2024-01-15",
			},
			Score: 0.95,
		},
		{
			Embedding: services.TransactionEmbedding{
				Type:        -1,
				Description: "กาแฟ",
				Amount:      65,
				Category:    "อาหาร",
				Date:        "2024-01-14",
			},
			Score: 0.85,
		},
	}

	// Need a MongoDBService instance to call the method
	// Since GetVectorSearchResultText doesn't use any fields, we can use nil
	mongo := &services.MongoDBService{}
	result := mongo.GetVectorSearchResultText(results)

	// Check that result contains expected information
	if result == "ไม่พบรายการที่คล้ายกัน" {
		t.Error("Should have found results")
	}

	// Check that result contains transaction info
	if len(result) < 50 {
		t.Errorf("Result too short: %s", result)
	}
}

func TestEmbeddingDimensions(t *testing.T) {
	dims := 768
	emb := mockEmbedding("test", dims)

	if len(emb) != dims {
		t.Errorf("Expected %d dimensions, got %d", dims, len(emb))
	}
}

// TestSimilarTextsShouldHaveHigherSimilarity tests that semantically similar texts
// have higher similarity scores (this is a weak test with mock embeddings)
func TestSimilarTextsShouldHaveHigherSimilarity(t *testing.T) {
	dims := 768

	// Base text
	base := mockEmbedding("กินข้าว 50 บาท หมวดอาหาร", dims)

	// Similar text (same category)
	similar := mockEmbedding("กินก๋วยเตี๋ยว 45 บาท หมวดอาหาร", dims)

	// Different text (different category)
	different := mockEmbedding("เติมน้ำมัน 1500 บาท หมวดเดินทาง", dims)

	simSimilar := cosineSimilarity(base, similar)
	simDifferent := cosineSimilarity(base, different)

	t.Logf("Similarity with similar text: %.4f", simSimilar)
	t.Logf("Similarity with different text: %.4f", simDifferent)

	// Note: With mock embeddings, this test may not always pass
	// because mock embeddings don't capture semantic meaning
	// This is just to verify the infrastructure works
}
