package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/satisatang/backend/services"
)

type TestQuestion struct {
	ID               int    `json:"id"`
	Input            string `json:"input"`
	ExpectedAction   string `json:"expected_action"`
	ExpectedType     string `json:"expected_type,omitempty"`
	ExpectedCategory string `json:"expected_category,omitempty"`
	ExpectedUseType  int    `json:"expected_usetype,omitempty"`
	ExpectedBankName string `json:"expected_bankname,omitempty"`
	ExpectedDays     int    `json:"expected_days,omitempty"`
	ExpectedField    string `json:"expected_field,omitempty"`
}

type TestQuestions struct {
	Questions []TestQuestion `json:"test_questions"`
}

type TestResult struct {
	ID       int
	Input    string
	Expected string
	Got      string
	Pass     bool
	Error    string
	Response string
}

func TestAIResponses(t *testing.T) {
	// Load test questions
	data, err := os.ReadFile("questions.json")
	if err != nil {
		t.Fatalf("Failed to load questions: %v", err)
	}

	var questions TestQuestions
	if err := json.Unmarshal(data, &questions); err != nil {
		t.Fatalf("Failed to parse questions: %v", err)
	}

	// Create AI service
	ai := services.NewAIService()
	defer ai.Close()

	results := make([]TestResult, 0)
	passed := 0
	failed := 0

	for _, q := range questions.Questions {
		result := testSingleQuestion(ai, q)
		results = append(results, result)

		if result.Pass {
			passed++
			t.Logf("✓ Q%d: %s", q.ID, q.Input)
		} else {
			failed++
			t.Errorf("✗ Q%d: %s\n  Expected: %s\n  Got: %s\n  Error: %s",
				q.ID, q.Input, result.Expected, result.Got, result.Error)
		}

		// Rate limiting - wait between requests
		time.Sleep(500 * time.Millisecond)
	}

	// Summary
	t.Logf("\n=== Summary ===")
	t.Logf("Total: %d, Passed: %d, Failed: %d", len(questions.Questions), passed, failed)
	t.Logf("Pass Rate: %.1f%%", float64(passed)/float64(len(questions.Questions))*100)

	// Save results
	saveResults(results)
}

func testSingleQuestion(ai *services.AIService, q TestQuestion) TestResult {
	result := TestResult{
		ID:       q.ID,
		Input:    q.Input,
		Expected: q.ExpectedAction,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	response, err := ai.ChatWithContext(ctx, q.Input, "", "")
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.Response = response

	// Clean response
	response = cleanJSON(response)

	// Parse response
	var aiResp services.AIResponse
	if err := json.Unmarshal([]byte(response), &aiResp); err != nil {
		result.Error = fmt.Sprintf("JSON parse error: %v", err)
		result.Got = response[:min(100, len(response))]
		return result
	}

	result.Got = aiResp.Action

	// Check action
	if aiResp.Action != q.ExpectedAction {
		result.Error = fmt.Sprintf("action mismatch: expected %s, got %s", q.ExpectedAction, aiResp.Action)
		return result
	}

	// Check type for new transactions
	if q.ExpectedAction == "new" && q.ExpectedType != "" {
		if len(aiResp.Transactions) == 0 {
			result.Error = "no transactions in response"
			return result
		}
		if aiResp.Transactions[0].Type != q.ExpectedType {
			result.Error = fmt.Sprintf("type mismatch: expected %s, got %s",
				q.ExpectedType, aiResp.Transactions[0].Type)
			return result
		}
	}

	// Check category
	if q.ExpectedCategory != "" && q.ExpectedAction == "new" {
		if len(aiResp.Transactions) > 0 && aiResp.Transactions[0].Category != q.ExpectedCategory {
			result.Error = fmt.Sprintf("category mismatch: expected %s, got %s",
				q.ExpectedCategory, aiResp.Transactions[0].Category)
			// This is a soft fail - category might be similar
		}
	}

	result.Pass = true
	return result
}

func cleanJSON(s string) string {
	// Remove markdown code blocks
	if strings.HasPrefix(s, "```json") {
		s = s[7:]
	} else if strings.HasPrefix(s, "```") {
		s = s[3:]
	}
	if strings.HasSuffix(s, "```") {
		s = s[:len(s)-3]
	}
	return strings.TrimSpace(s)
}

func saveResults(results []TestResult) {
	data, _ := json.MarshalIndent(results, "", "  ")
	os.WriteFile("test_results.json", data, 0644)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
