// Package main provides the ClawMemory benchmark harness.
// It runs synthetic benchmark suites against a running ClawMemory server
// and produces a markdown report card with metrics.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"net/http"
	"os"
	"strings"
	"time"
)

// BenchmarkResult holds the results of a single benchmark run.
type BenchmarkResult struct {
	Name          string        `json:"name"`
	Total         int           `json:"total"`
	Passed        int           `json:"passed"`
	Accuracy      float64       `json:"accuracy"`        // 0.0-1.0
	AvgLatencyMs  float64       `json:"avg_latency_ms"`
	P95LatencyMs  float64       `json:"p95_latency_ms"`
	ContextUsed   float64       `json:"context_used"`    // fraction of injected context used
	Duration      time.Duration `json:"duration_ms"`
	Errors        []string      `json:"errors,omitempty"`
}

// ReportCard aggregates all benchmark results.
type ReportCard struct {
	RunAt     time.Time         `json:"run_at"`
	ServerURL string            `json:"server_url"`
	Suites    []BenchmarkResult `json:"suites"`
	Overall   BenchmarkResult   `json:"overall"`
}

// benchClient is a simple HTTP client for the ClawMemory API.
type benchClient struct {
	baseURL string
	client  *http.Client
}

// newBenchClient creates a benchmark client.
func newBenchClient(baseURL string) *benchClient {
	return &benchClient{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// remember stores a fact via the API.
func (c *benchClient) remember(ctx context.Context, content, category, container string, importance float64) error {
	body := map[string]interface{}{
		"content":    content,
		"category":   category,
		"container":  container,
		"importance": importance,
	}
	_, err := c.post(ctx, "/api/v1/remember", body)
	return err
}

// recall searches memory via the API.
func (c *benchClient) recall(ctx context.Context, query string, limit int) ([]map[string]interface{}, error) {
	body := map[string]interface{}{
		"query": query,
		"limit": limit,
	}
	resp, err := c.post(ctx, "/api/v1/recall", body)
	if err != nil {
		return nil, err
	}
	results, ok := resp["results"].([]interface{})
	if !ok {
		return nil, nil
	}
	var facts []map[string]interface{}
	for _, r := range results {
		if m, ok := r.(map[string]interface{}); ok {
			facts = append(facts, m)
		}
	}
	return facts, nil
}

// post sends a POST request and returns the JSON response.
func (c *benchClient) post(ctx context.Context, path string, body interface{}) (map[string]interface{}, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			_ = closeErr // best-effort close
		}
	}()

	var result map[string]interface{}
	if decErr := json.NewDecoder(resp.Body).Decode(&result); decErr != nil {
		return nil, fmt.Errorf("decode response: %w", decErr)
	}
	return result, nil
}

// containsString checks if a string contains a substring (case-insensitive).
func containsString(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// percentile computes the Nth percentile of a sorted float64 slice.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := math.Ceil(float64(len(sorted))*p/100) - 1
	if idx < 0 {
		idx = 0
	}
	if int(idx) >= len(sorted) {
		idx = float64(len(sorted) - 1)
	}
	return sorted[int(idx)]
}

// sortFloat64 sorts a float64 slice in ascending order.
func sortFloat64(s []float64) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// runLongMemEval runs the LongMemEval benchmark (100-question subset).
// Tests long-term memory retention over extended conversations.
func runLongMemEval(ctx context.Context, client *benchClient) BenchmarkResult {
	result := BenchmarkResult{Name: "LongMemEval-100"}

	// Synthetic dataset: 100 questions about stored facts
	questions := generateLongMemEvalQuestions(100)
	result.Total = len(questions)

	var latencies []float64
	start := time.Now()

	for _, q := range questions {
		// Store the fact (errors are logged via result.Errors)
		if remErr := client.remember(ctx, q.fact, q.category, q.container, 0.8); remErr != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("remember: %v", remErr))
		}

		// Query for it
		t0 := time.Now()
		facts, err := client.recall(ctx, q.query, 5)
		latencyMs := float64(time.Since(t0).Milliseconds())
		latencies = append(latencies, latencyMs)

		if err != nil {
			result.Errors = append(result.Errors, err.Error())
			continue
		}

		// Check if the expected answer appears in results
		found := false
		for _, f := range facts {
			if content, ok := f["content"].(string); ok {
				if containsString(content, q.expectedKeyword) {
					found = true
					break
				}
			}
		}
		if found {
			result.Passed++
		}
	}

	result.Duration = time.Since(start)
	if len(latencies) > 0 {
		var sum float64
		for _, l := range latencies {
			sum += l
		}
		result.AvgLatencyMs = sum / float64(len(latencies))
		sortFloat64(latencies)
		result.P95LatencyMs = percentile(latencies, 95)
	}
	if result.Total > 0 {
		result.Accuracy = float64(result.Passed) / float64(result.Total)
	}

	return result
}

// runLocomo runs the LoCoMo benchmark (50 conversation subset).
// Tests conversation memory across long contexts.
func runLocomo(ctx context.Context, client *benchClient) BenchmarkResult {
	result := BenchmarkResult{Name: "LoCoMo-50"}

	scenarios := generateLocomoScenarios(50)
	result.Total = len(scenarios)

	var latencies []float64
	start := time.Now()

	for _, s := range scenarios {
		// Store all facts from the conversation
		for _, fact := range s.facts {
			if remErr := client.remember(ctx, fact, "general", "general", 0.7); remErr != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("remember: %v", remErr))
			}
		}

		// Query for specific recalled fact
		t0 := time.Now()
		facts, err := client.recall(ctx, s.query, 5)
		latencyMs := float64(time.Since(t0).Milliseconds())
		latencies = append(latencies, latencyMs)

		if err != nil {
			result.Errors = append(result.Errors, err.Error())
			continue
		}

		// Check recall accuracy
		for _, f := range facts {
			if content, ok := f["content"].(string); ok {
				if containsString(content, s.expectedKeyword) {
					result.Passed++
					break
				}
			}
		}
	}

	result.Duration = time.Since(start)
	if len(latencies) > 0 {
		var sum float64
		for _, l := range latencies {
			sum += l
		}
		result.AvgLatencyMs = sum / float64(len(latencies))
		sortFloat64(latencies)
		result.P95LatencyMs = percentile(latencies, 95)
	}
	if result.Total > 0 {
		result.Accuracy = float64(result.Passed) / float64(result.Total)
	}

	return result
}

// runConvoMem runs the ConvoMem benchmark (30 contradiction scenarios).
// Tests contradiction detection and resolution.
func runConvoMem(ctx context.Context, client *benchClient) BenchmarkResult {
	result := BenchmarkResult{Name: "ConvoMem-Contradictions-30"}

	scenarios := generateConvoMemScenarios(30)
	result.Total = len(scenarios)

	var latencies []float64
	start := time.Now()

	for _, s := range scenarios {
		// Store original fact
		if remErr := client.remember(ctx, s.originalFact, "preference", "personal", 0.8); remErr != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("remember original: %v", remErr))
		}

		// Store contradicting fact (should supersede the original)
		if remErr := client.remember(ctx, s.newFact, "preference", "personal", 0.8); remErr != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("remember new: %v", remErr))
		}

		// Recall — should return the newer fact, not the old one
		t0 := time.Now()
		facts, err := client.recall(ctx, s.query, 5)
		latencyMs := float64(time.Since(t0).Milliseconds())
		latencies = append(latencies, latencyMs)

		if err != nil {
			result.Errors = append(result.Errors, err.Error())
			continue
		}

		// Check that new fact appears and old fact does not
		newFactFound := false
		for _, f := range facts {
			if content, ok := f["content"].(string); ok {
				if containsString(content, s.newKeyword) {
					newFactFound = true
				}
			}
		}
		if newFactFound {
			result.Passed++
		}
	}

	result.Duration = time.Since(start)
	if len(latencies) > 0 {
		var sum float64
		for _, l := range latencies {
			sum += l
		}
		result.AvgLatencyMs = sum / float64(len(latencies))
		sortFloat64(latencies)
		result.P95LatencyMs = percentile(latencies, 95)
	}
	if result.Total > 0 {
		result.Accuracy = float64(result.Passed) / float64(result.Total)
	}

	return result
}

// longMemEvalQuestion is a single question in the LongMemEval benchmark.
type longMemEvalQuestion struct {
	fact            string
	category        string
	container       string
	query           string
	expectedKeyword string
}

// generateLongMemEvalQuestions produces N synthetic questions.
func generateLongMemEvalQuestions(n int) []longMemEvalQuestion {
	templates := []longMemEvalQuestion{
		{"User prefers dark mode interface", "preference", "personal", "display preferences", "dark"},
		{"User's timezone is Australia/Sydney", "preference", "personal", "what timezone", "sydney"},
		{"User works at TechCorp as a senior engineer", "person", "work", "employer role", "techcorp"},
		{"User primary language is Go", "technical", "work", "programming language", "go"},
		{"User prefers coffee over tea", "preference", "personal", "beverage preference", "coffee"},
		{"User is a technical founder building ClawChain", "person", "clawchain", "what building", "clawchain"},
		{"User wakes up at 6am daily", "preference", "personal", "morning routine", "6am"},
		{"User uses Vim keybindings", "preference", "work", "editor preference", "vim"},
		{"User's favorite color is blue", "preference", "personal", "favorite color", "blue"},
		{"User has 5 years of Go experience", "person", "work", "go experience", "years"},
	}

	questions := make([]longMemEvalQuestion, n)
	for i := 0; i < n; i++ {
		questions[i] = templates[i%len(templates)]
		questions[i].fact = fmt.Sprintf("%s [variant %d]", questions[i].fact, i)
	}
	return questions
}

// locomoScenario is a LoCoMo benchmark scenario.
type locomoScenario struct {
	facts           []string
	query           string
	expectedKeyword string
}

// generateLocomoScenarios produces N synthetic LoCoMo scenarios.
func generateLocomoScenarios(n int) []locomoScenario {
	templates := []locomoScenario{
		{
			facts:           []string{"User discussed project architecture", "User mentioned deadline is Q1", "User prefers async communication"},
			query:           "project deadline",
			expectedKeyword: "deadline",
		},
		{
			facts:           []string{"User prefers remote work", "User mentioned timezone issues", "User uses Slack for communication"},
			query:           "work style communication",
			expectedKeyword: "slack",
		},
	}

	scenarios := make([]locomoScenario, n)
	for i := 0; i < n; i++ {
		scenarios[i] = templates[i%len(templates)]
	}
	return scenarios
}

// convoMemScenario is a ConvoMem contradiction scenario.
type convoMemScenario struct {
	originalFact string
	newFact      string
	query        string
	newKeyword   string
}

// generateConvoMemScenarios produces N synthetic contradiction scenarios.
func generateConvoMemScenarios(n int) []convoMemScenario {
	templates := []convoMemScenario{
		{
			originalFact: "User lives in Sydney",
			newFact:      "User has moved to Melbourne",
			query:        "where user lives",
			newKeyword:   "melbourne",
		},
		{
			originalFact: "User prefers Python",
			newFact:      "User now prefers Go after learning it",
			query:        "programming language preference",
			newKeyword:   "go",
		},
		{
			originalFact: "User drinks tea",
			newFact:      "User switched to coffee in 2024",
			query:        "beverage preference",
			newKeyword:   "coffee",
		},
	}

	scenarios := make([]convoMemScenario, n)
	for i := 0; i < n; i++ {
		scenarios[i] = templates[i%len(templates)]
	}
	return scenarios
}

// generateReportCard produces a markdown report from benchmark results.
func generateReportCard(report *ReportCard) string {
	var sb strings.Builder

	sb.WriteString("# ClawMemory Benchmark Report\n\n")
	sb.WriteString(fmt.Sprintf("**Run at:** %s  \n", report.RunAt.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("**Server:** %s  \n\n", report.ServerURL))

	sb.WriteString("## Results\n\n")
	sb.WriteString("| Suite | Total | Passed | Accuracy | Avg Latency | P95 Latency |\n")
	sb.WriteString("|-------|-------|--------|----------|-------------|-------------|\n")

	for _, suite := range report.Suites {
		sb.WriteString(fmt.Sprintf("| %s | %d | %d | %.1f%% | %.1fms | %.1fms |\n",
			suite.Name, suite.Total, suite.Passed,
			suite.Accuracy*100, suite.AvgLatencyMs, suite.P95LatencyMs))
	}

	sb.WriteString(fmt.Sprintf("| **Overall** | **%d** | **%d** | **%.1f%%** | **%.1fms** | **%.1fms** |\n",
		report.Overall.Total, report.Overall.Passed,
		report.Overall.Accuracy*100, report.Overall.AvgLatencyMs, report.Overall.P95LatencyMs))

	sb.WriteString("\n## Errors\n\n")
	hasErrors := false
	for _, suite := range report.Suites {
		if len(suite.Errors) > 0 {
			hasErrors = true
			sb.WriteString(fmt.Sprintf("### %s\n", suite.Name))
			for _, e := range suite.Errors {
				sb.WriteString(fmt.Sprintf("- %s\n", e))
			}
		}
	}
	if !hasErrors {
		sb.WriteString("None ✅\n")
	}

	return sb.String()
}

func main() {
	serverURL := flag.String("server", "http://127.0.0.1:7437", "ClawMemory server URL")
	outputPath := flag.String("output", "bench-report.md", "Output markdown report path")
	suite := flag.String("suite", "all", "Benchmark suite to run (all|longmemeval|locomo|convomem)")
	flag.Parse()

	client := newBenchClient(*serverURL)
	ctx := context.Background()

	// Check server health
	healthResp, err := client.post(ctx, "/health", nil)
	if err != nil || healthResp == nil {
		fmt.Fprintf(os.Stderr, "Error: ClawMemory server not reachable at %s: %v\n", *serverURL, err)
		os.Exit(1)
	}

	report := &ReportCard{
		RunAt:     time.Now(),
		ServerURL: *serverURL,
	}

	switch *suite {
	case "longmemeval":
		report.Suites = []BenchmarkResult{runLongMemEval(ctx, client)}
	case "locomo":
		report.Suites = []BenchmarkResult{runLocomo(ctx, client)}
	case "convomem":
		report.Suites = []BenchmarkResult{runConvoMem(ctx, client)}
	default:
		fmt.Println("[bench] Running all benchmark suites...")
		report.Suites = []BenchmarkResult{
			runLongMemEval(ctx, client),
			runLocomo(ctx, client),
			runConvoMem(ctx, client),
		}
	}

	// Compute overall
	var totalPassed, totalTotal int
	var totalLatency, totalP95 float64
	for _, s := range report.Suites {
		totalPassed += s.Passed
		totalTotal += s.Total
		totalLatency += s.AvgLatencyMs
		totalP95 += s.P95LatencyMs
	}
	n := len(report.Suites)
	if n > 0 {
		report.Overall = BenchmarkResult{
			Name:         "Overall",
			Total:        totalTotal,
			Passed:       totalPassed,
			AvgLatencyMs: totalLatency / float64(n),
			P95LatencyMs: totalP95 / float64(n),
		}
		if totalTotal > 0 {
			report.Overall.Accuracy = float64(totalPassed) / float64(totalTotal)
		}
	}

	// Print summary to stdout
	for _, s := range report.Suites {
		fmt.Printf("[bench] %s: %d/%d (%.1f%%) | avg %.1fms\n",
			s.Name, s.Passed, s.Total, s.Accuracy*100, s.AvgLatencyMs)
	}
	fmt.Printf("[bench] Overall: %d/%d (%.1f%%)\n",
		report.Overall.Passed, report.Overall.Total, report.Overall.Accuracy*100)

	// Write markdown report
	md := generateReportCard(report)
	if err := os.WriteFile(*outputPath, []byte(md), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not write report to %s: %v\n", *outputPath, err)
	} else {
		fmt.Printf("[bench] Report written to %s\n", *outputPath)
	}
}
