package extractor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func mockLLMServer(t *testing.T, responseContent string, statusCode int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if statusCode != http.StatusOK {
			w.WriteHeader(statusCode)
			return
		}
		resp := chatResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Message: struct {
					Content string `json:"content"`
				}{Content: responseContent}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestExtract_BasicFacts(t *testing.T) {
	facts := []Fact{
		{Content: "User works at Anthropic", Category: "person", Container: "work", Importance: 0.8},
		{Content: "User prefers dark mode", Category: "preference", Container: "personal", Importance: 0.5},
		{Content: "User timezone is Australia/Sydney", Category: "preference", Container: "personal", Importance: 0.7},
	}
	factJSON, _ := json.Marshal(facts)

	srv := mockLLMServer(t, string(factJSON), http.StatusOK)
	defer srv.Close()

	e := New(srv.URL, "glm-4.7", "test-key")
	turns := []Turn{
		{Role: "user", Content: "I work at Anthropic and prefer dark mode. My timezone is Australia/Sydney."},
		{Role: "assistant", Content: "Got it! I'll remember your preferences."},
	}

	result, err := e.Extract(context.Background(), turns)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(result) != 3 {
		t.Errorf("expected 3 facts, got %d", len(result))
	}
	if result[0].Content != "User works at Anthropic" {
		t.Errorf("unexpected content: %q", result[0].Content)
	}
}

func TestExtract_NoFacts(t *testing.T) {
	srv := mockLLMServer(t, "[]", http.StatusOK)
	defer srv.Close()

	e := New(srv.URL, "glm-4.7", "")
	turns := []Turn{
		{Role: "user", Content: "Hi there"},
		{Role: "assistant", Content: "Hello!"},
	}

	result, err := e.Extract(context.Background(), turns)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 facts, got %d", len(result))
	}
}

func TestExtract_MaxFacts(t *testing.T) {
	// LLM returns 8 facts — should be capped at 5
	facts := make([]Fact, 8)
	for i := range facts {
		facts[i] = Fact{Content: "Fact " + strings.Repeat("a", i+1), Category: "general", Container: "general", Importance: 0.5}
	}
	factJSON, _ := json.Marshal(facts)

	srv := mockLLMServer(t, string(factJSON), http.StatusOK)
	defer srv.Close()

	e := New(srv.URL, "glm-4.7", "")
	turns := []Turn{{Role: "user", Content: "Many facts"}}

	result, err := e.Extract(context.Background(), turns)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(result) != 5 {
		t.Errorf("expected max 5 facts, got %d", len(result))
	}
}

func TestExtract_InvalidJSON(t *testing.T) {
	srv := mockLLMServer(t, "not valid json at all{{", http.StatusOK)
	defer srv.Close()

	e := New(srv.URL, "glm-4.7", "")
	turns := []Turn{{Role: "user", Content: "test"}}

	result, err := e.Extract(context.Background(), turns)
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
	if result != nil {
		t.Error("expected nil result for invalid JSON")
	}
}

func TestExtract_Categories(t *testing.T) {
	facts := []Fact{
		{Content: "Valid category", Category: "person", Container: "personal", Importance: 0.7},
		{Content: "Unknown category", Category: "unknown_category", Container: "personal", Importance: 0.5},
	}
	factJSON, _ := json.Marshal(facts)

	srv := mockLLMServer(t, string(factJSON), http.StatusOK)
	defer srv.Close()

	e := New(srv.URL, "glm-4.7", "")
	turns := []Turn{{Role: "user", Content: "test"}}

	result, err := e.Extract(context.Background(), turns)
	if err != nil {
		t.Fatal(err)
	}

	// First fact: valid category preserved
	if result[0].Category != "person" {
		t.Errorf("expected person, got %s", result[0].Category)
	}
	// Second fact: invalid category normalized to general
	if result[1].Category != "general" {
		t.Errorf("expected general for unknown category, got %s", result[1].Category)
	}
}

func TestExtract_Importance(t *testing.T) {
	facts := []Fact{
		{Content: "Normal importance", Category: "general", Container: "general", Importance: 0.7},
		{Content: "Too high importance", Category: "general", Container: "general", Importance: 1.5},
		{Content: "Too low importance", Category: "general", Container: "general", Importance: -0.5},
	}
	factJSON, _ := json.Marshal(facts)

	srv := mockLLMServer(t, string(factJSON), http.StatusOK)
	defer srv.Close()

	e := New(srv.URL, "glm-4.7", "")
	turns := []Turn{{Role: "user", Content: "test"}}

	result, err := e.Extract(context.Background(), turns)
	if err != nil {
		t.Fatal(err)
	}

	if result[1].Importance != 1.0 {
		t.Errorf("too high importance should be capped at 1.0, got %f", result[1].Importance)
	}
	if result[2].Importance != 0.0 {
		t.Errorf("too low importance should be clamped to 0.0, got %f", result[2].Importance)
	}
}

func TestBuildExtractionPrompt(t *testing.T) {
	turns := []Turn{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there"},
	}
	prompt := BuildExtractionPrompt(turns)

	if !strings.Contains(prompt, "USER: Hello") {
		t.Errorf("prompt should contain 'USER: Hello', got: %s", prompt)
	}
	if !strings.Contains(prompt, "ASSISTANT: Hi there") {
		t.Errorf("prompt should contain 'ASSISTANT: Hi there', got: %s", prompt)
	}
}

func TestExtract_HTTPError(t *testing.T) {
	srv := mockLLMServer(t, "", http.StatusInternalServerError)
	defer srv.Close()

	e := New(srv.URL, "glm-4.7", "")
	turns := []Turn{{Role: "user", Content: "test"}}

	_, err := e.Extract(context.Background(), turns)
	if err == nil {
		t.Error("expected error for HTTP 500, got nil")
	}
}

func TestExtract_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer srv.Close()

	e := New(srv.URL, "glm-4.7", "")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := e.Extract(ctx, []Turn{{Role: "user", Content: "test"}})
	if err == nil {
		t.Error("expected timeout error, got nil")
	}
}

func TestExtract_EmptyTurns(t *testing.T) {
	e := New("http://localhost:9999", "glm-4.7", "")
	result, err := e.Extract(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Error("expected nil for empty turns")
	}
}

func TestExtract_MarkdownCodeBlock(t *testing.T) {
	facts := []Fact{
		{Content: "User is a Go developer", Category: "person", Container: "work", Importance: 0.8},
	}
	factJSON, _ := json.Marshal(facts)
	// Wrap in markdown code block
	mdResponse := "```json\n" + string(factJSON) + "\n```"

	srv := mockLLMServer(t, mdResponse, http.StatusOK)
	defer srv.Close()

	e := New(srv.URL, "glm-4.7", "")
	result, err := e.Extract(context.Background(), []Turn{{Role: "user", Content: "I'm a Go developer"}})
	if err != nil {
		t.Fatalf("Extract with markdown wrapper: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("expected 1 fact, got %d", len(result))
	}
}

func TestExtract_SkipsEmptyContent(t *testing.T) {
	facts := []Fact{
		{Content: "", Category: "general", Container: "general", Importance: 0.5},
		{Content: "Real fact", Category: "general", Container: "general", Importance: 0.5},
	}
	factJSON, _ := json.Marshal(facts)

	srv := mockLLMServer(t, string(factJSON), http.StatusOK)
	defer srv.Close()

	e := New(srv.URL, "glm-4.7", "")
	result, err := e.Extract(context.Background(), []Turn{{Role: "user", Content: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 {
		t.Errorf("expected 1 fact (empty content skipped), got %d", len(result))
	}
}

func TestParseFacts_EmptyContent(t *testing.T) {
	result, err := parseFacts("")
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Error("expected nil for empty content")
	}
}

func TestParseFacts_NormalizeContainer(t *testing.T) {
	content := `[{"content":"test","category":"general","container":"unknown_cont","importance":0.5}]`
	facts, err := parseFacts(content)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 {
		t.Fatal("expected 1 fact")
	}
	if facts[0].Container != "general" {
		t.Errorf("expected container normalized to general, got %s", facts[0].Container)
	}
}
