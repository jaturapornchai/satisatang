package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/satisatang/backend/services"
)

type TestQuestion struct {
	ID               int    `json:"id"`
	Input            string `json:"input"`
	ExpectedAction   string `json:"expected_action"`
	ExpectedType     string `json:"expected_type,omitempty"`
	ExpectedCategory string `json:"expected_category,omitempty"`
	ExpectedUseType  *int   `json:"expected_usetype,omitempty"`
	ExpectedBankName string `json:"expected_bankname,omitempty"`
	ExpectedDays     int    `json:"expected_days,omitempty"`
	ExpectedField    string `json:"expected_field,omitempty"`
}

type TestQuestions struct {
	Questions []TestQuestion `json:"test_questions"`
}

type TestResult struct {
	ID           int    `json:"id"`
	Input        string `json:"input"`
	Expected     string `json:"expected"`
	Got          string `json:"got"`
	Pass         bool   `json:"pass"`
	Error        string `json:"error,omitempty"`
	Response     string `json:"response,omitempty"`
	CategoryPass bool   `json:"category_pass,omitempty"`
	TypePass     bool   `json:"type_pass,omitempty"`
}

func main() {
	// Load test questions
	data, err := os.ReadFile("tests/questions.json")
	if err != nil {
		fmt.Printf("Failed to load questions: %v\n", err)
		return
	}

	var questions TestQuestions
	if err := json.Unmarshal(data, &questions); err != nil {
		fmt.Printf("Failed to parse questions: %v\n", err)
		return
	}

	// Create AI service
	ai := services.NewAIService()
	defer ai.Close()

	results := make([]TestResult, 0)
	passed := 0
	failed := 0
	actionFailed := 0
	categoryIssues := 0

	// Test subset or all
	testCount := len(questions.Questions)
	if len(os.Args) > 1 && os.Args[1] == "quick" {
		testCount = 20 // Quick test
	}

	fmt.Printf("Testing %d questions...\n\n", testCount)

	for i := 0; i < testCount; i++ {
		q := questions.Questions[i]
		result := testSingleQuestion(ai, q)
		results = append(results, result)

		status := "✓"
		if !result.Pass {
			status = "✗"
			failed++
			if result.Got != q.ExpectedAction {
				actionFailed++
			}
		} else {
			passed++
		}

		if result.Error != "" && strings.Contains(result.Error, "category") {
			categoryIssues++
		}

		fmt.Printf("%s Q%d: %s\n", status, q.ID, q.Input)
		if !result.Pass {
			fmt.Printf("   Expected: %s, Got: %s\n", result.Expected, result.Got)
			if result.Error != "" {
				fmt.Printf("   Error: %s\n", result.Error)
			}
		}

		// Rate limiting
		time.Sleep(300 * time.Millisecond)
	}

	// Summary
	fmt.Printf("\n=== Summary ===\n")
	fmt.Printf("Total: %d, Passed: %d, Failed: %d\n", testCount, passed, failed)
	fmt.Printf("Action mismatches: %d\n", actionFailed)
	fmt.Printf("Category issues: %d\n", categoryIssues)
	fmt.Printf("Pass Rate: %.1f%%\n", float64(passed)/float64(testCount)*100)

	// Save results
	resultData, _ := json.MarshalIndent(results, "", "  ")
	os.WriteFile("tests/test_results.json", resultData, 0644)
	fmt.Printf("\nResults saved to tests/test_results.json\n")

	// Print failed questions for analysis
	if failed > 0 {
		fmt.Printf("\n=== Failed Questions ===\n")
		for _, r := range results {
			if !r.Pass {
				fmt.Printf("Q%d: %s\n  → %s\n", r.ID, r.Input, r.Response[:min(200, len(r.Response))])
			}
		}
	}
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
		result.Got = truncate(response, 100)
		return result
	}

	result.Got = aiResp.Action

	// Check action
	if aiResp.Action != q.ExpectedAction {
		result.Error = fmt.Sprintf("action: expected %s, got %s", q.ExpectedAction, aiResp.Action)
		return result
	}

	// For new transactions, check type
	if q.ExpectedAction == "new" {
		if len(aiResp.Transactions) == 0 {
			result.Error = "no transactions"
			return result
		}

		tx := aiResp.Transactions[0]

		// Check type
		if q.ExpectedType != "" && tx.Type != q.ExpectedType {
			result.Error = fmt.Sprintf("type: expected %s, got %s", q.ExpectedType, tx.Type)
			result.TypePass = false
			return result
		}
		result.TypePass = true

		// Check category (soft check)
		if q.ExpectedCategory != "" {
			if tx.Category == q.ExpectedCategory {
				result.CategoryPass = true
			} else {
				result.Error = fmt.Sprintf("category: expected %s, got %s", q.ExpectedCategory, tx.Category)
				// Still pass if action and type are correct
			}
		}
	}

	result.Pass = true
	return result
}

func cleanJSON(s string) string {
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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
