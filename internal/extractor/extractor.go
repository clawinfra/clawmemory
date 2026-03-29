// Package extractor provides LLM-based fact extraction from conversation turns.
// It supports both OpenAI-compatible (/v1/chat/completions) and Anthropic-compatible
// (/v1/messages) API formats, auto-detected from the base URL or overridden via APIFormat.
package extractor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// APIFormat selects which HTTP wire format to use when calling the LLM.
type APIFormat string

const (
	FormatOpenAI    APIFormat = "openai"    // POST /chat/completions
	FormatAnthropic APIFormat = "anthropic" // POST /messages
)

// Fact represents a single extracted fact from conversation.
type Fact struct {
	Content    string  `json:"content"`    // "User's timezone is Australia/Sydney"
	Category   string  `json:"category"`   // person|project|preference|event|technical|general
	Container  string  `json:"container"`  // work|trading|clawchain|personal|general
	Importance float64 `json:"importance"` // 0.0-1.0
}

// Turn represents a single conversation message.
type Turn struct {
	Role    string `json:"role"`    // "user" or "assistant"
	Content string `json:"content"`
}

// Extractor calls an LLM to extract structured facts from conversation turns.
type Extractor struct {
	baseURL string
	model   string
	apiKey  string
	format  APIFormat
	client  *http.Client
}

// --- OpenAI wire types ---

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Temperature float64         `json:"temperature"`
	MaxTokens   int             `json:"max_tokens"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// --- Anthropic wire types ---

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// validCategories is the set of valid fact categories.
var validCategories = map[string]bool{
	"person": true, "project": true, "preference": true,
	"event": true, "technical": true, "general": true,
}

// validContainers is the set of valid containers.
var validContainers = map[string]bool{
	"work": true, "trading": true, "clawchain": true,
	"personal": true, "general": true,
}

// detectFormat infers API format from base URL if not explicitly set.
// URLs containing "anthropic" default to Anthropic format; everything else OpenAI.
func detectFormat(baseURL string) APIFormat {
	lower := strings.ToLower(baseURL)
	if strings.Contains(lower, "anthropic") {
		return FormatAnthropic
	}
	return FormatOpenAI
}

// New creates an Extractor. APIFormat is auto-detected from baseURL.
func New(baseURL, model, apiKey string) *Extractor {
	return &Extractor{
		baseURL: baseURL,
		model:   model,
		apiKey:  apiKey,
		format:  detectFormat(baseURL),
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

// NewWithFormat creates an Extractor with an explicit API format override.
func NewWithFormat(baseURL, model, apiKey string, format APIFormat) *Extractor {
	e := New(baseURL, model, apiKey)
	e.format = format
	return e
}

// Extract sends the last N turns to the LLM and returns 0-5 structured facts.
func (e *Extractor) Extract(ctx context.Context, turns []Turn) ([]Fact, error) {
	if len(turns) == 0 {
		return nil, nil
	}
	userPrompt := BuildExtractionPrompt(turns)
	switch e.format {
	case FormatAnthropic:
		return e.extractAnthropic(ctx, userPrompt)
	default:
		return e.extractOpenAI(ctx, userPrompt)
	}
}

// extractOpenAI calls POST {baseURL}/chat/completions.
func (e *Extractor) extractOpenAI(ctx context.Context, userPrompt string) ([]Fact, error) {
	reqBody := chatRequest{
		Model: e.model,
		Messages: []openAIMessage{
			{Role: "system", Content: extractionSystemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Temperature: 0.1,
		MaxTokens:   512,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal openai request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		e.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create openai request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}
	return e.doRequest(req, e.parseOpenAI)
}

// extractAnthropic calls POST {baseURL}/messages with Anthropic wire format.
func (e *Extractor) extractAnthropic(ctx context.Context, userPrompt string) ([]Fact, error) {
	reqBody := anthropicRequest{
		Model:     e.model,
		MaxTokens: 512,
		System:    extractionSystemPrompt,
		Messages:  []anthropicMessage{{Role: "user", Content: userPrompt}},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		e.baseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create anthropic request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	if e.apiKey != "" {
		req.Header.Set("x-api-key", e.apiKey)
	}
	return e.doRequest(req, e.parseAnthropic)
}

// doRequest executes an HTTP request and parses the response via the provided parser.
func (e *Extractor) doRequest(req *http.Request, parse func([]byte) ([]Fact, error)) ([]Fact, error) {
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("extractor request: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read extractor response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("extractor request failed (status %d): %s", resp.StatusCode, respBody)
	}
	return parse(respBody)
}

// parseOpenAI extracts the text content from an OpenAI chat completion response.
func (e *Extractor) parseOpenAI(body []byte) ([]Fact, error) {
	var r chatResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("decode openai response: %w", err)
	}
	if len(r.Choices) == 0 {
		return nil, nil
	}
	return parseFacts(r.Choices[0].Message.Content)
}

// parseAnthropic extracts the text content from an Anthropic messages response.
func (e *Extractor) parseAnthropic(body []byte) ([]Fact, error) {
	var r anthropicResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("decode anthropic response: %w", err)
	}
	for _, block := range r.Content {
		if block.Type == "text" {
			return parseFacts(block.Text)
		}
	}
	return nil, nil
}

// parseFacts parses the JSON array of facts from the LLM response.
func parseFacts(content string) ([]Fact, error) {
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)
	if content == "" || content == "[]" {
		return nil, nil
	}
	var facts []Fact
	if err := json.Unmarshal([]byte(content), &facts); err != nil {
		return nil, fmt.Errorf("parse facts JSON: %w", err)
	}
	validated := make([]Fact, 0, len(facts))
	for _, f := range facts {
		if f.Content == "" {
			continue
		}
		if !validCategories[f.Category] {
			f.Category = "general"
		}
		if !validContainers[f.Container] {
			f.Container = "general"
		}
		if f.Importance < 0 {
			f.Importance = 0
		}
		if f.Importance > 1 {
			f.Importance = 1
		}
		validated = append(validated, f)
		if len(validated) >= 5 {
			break
		}
	}
	return validated, nil
}
