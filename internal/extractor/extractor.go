// Package extractor provides LLM-based fact extraction from conversation turns.
// It uses GLM-4.7 via an OpenAI-compatible HTTP proxy to identify and classify
// facts worth remembering from raw conversation text.
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
	client  *http.Client
}

// openAIMessage is a single message in the OpenAI chat format.
type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatRequest is the OpenAI-compatible chat completion request.
type chatRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Temperature float64         `json:"temperature"`
	MaxTokens   int             `json:"max_tokens"`
}

// chatResponse is the OpenAI-compatible chat completion response.
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
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

// New creates an Extractor targeting GLM-4.7 via proxy.
func New(baseURL, model, apiKey string) *Extractor {
	return &Extractor{
		baseURL: baseURL,
		model:   model,
		apiKey:  apiKey,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// Extract sends the last N turns to GLM-4.7 and returns 0-5 structured facts.
// The prompt instructs the LLM to output a JSON array of facts.
// Returns empty slice if no extractable facts found.
func (e *Extractor) Extract(ctx context.Context, turns []Turn) ([]Fact, error) {
	if len(turns) == 0 {
		return nil, nil
	}

	userPrompt := BuildExtractionPrompt(turns)

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
		return nil, fmt.Errorf("marshal extractor request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		e.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create extractor request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("extractor request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("extractor request failed (status %d): %s", resp.StatusCode, respBody)
	}

	var chatResp chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("decode extractor response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return nil, nil
	}

	content := strings.TrimSpace(chatResp.Choices[0].Message.Content)
	return parseFacts(content)
}

// parseFacts parses the JSON array of facts from the LLM response.
// Handles edge cases like markdown code blocks, invalid JSON, etc.
func parseFacts(content string) ([]Fact, error) {
	// Strip markdown code blocks if present
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

	// Validate and cap at 5 facts
	validated := make([]Fact, 0, len(facts))
	for _, f := range facts {
		if f.Content == "" {
			continue
		}
		// Normalize category
		if !validCategories[f.Category] {
			f.Category = "general"
		}
		// Normalize container
		if !validContainers[f.Container] {
			f.Container = "general"
		}
		// Clamp importance
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
