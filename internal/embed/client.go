// Package embed provides an Ollama embedding HTTP client for ClawMemory.
// It computes vector embeddings for text using the Ollama API,
// with graceful degradation when the server is unreachable.
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// embeddingRequest is the JSON body for the Ollama embedding API.
type embeddingRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

// embeddingResponse is the JSON response from the Ollama embedding API.
type embeddingResponse struct {
	Embedding []float64 `json:"embedding"`
}

// Client wraps the Ollama embedding API.
type Client struct {
	baseURL string // "http://10.0.0.44:11434"
	model   string // "qwen2.5:7b"
	dim     int    // 3584
	client  *http.Client
}

// New creates an Ollama embedding client.
func New(baseURL, model string, dim int) *Client {
	return &Client{
		baseURL: baseURL,
		model:   model,
		dim:     dim,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Embed computes the embedding vector for a single text string.
// Calls POST {baseURL}/api/embeddings with model and prompt.
// Returns a float32 slice of length dim.
// Returns an error if the Ollama server is unreachable or returns an unexpected response.
func (c *Client) Embed(ctx context.Context, text string) ([]float32, error) {
	reqBody := embeddingRequest{
		Model:  c.model,
		Prompt: text,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal embedding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create embedding request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embedding request failed (status %d): %s", resp.StatusCode, respBody)
	}

	var embResp embeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&embResp); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}

	if len(embResp.Embedding) != c.dim {
		return nil, fmt.Errorf("unexpected embedding dimension: got %d, want %d", len(embResp.Embedding), c.dim)
	}

	// Convert float64 → float32 for storage efficiency
	result := make([]float32, len(embResp.Embedding))
	for i, v := range embResp.Embedding {
		result[i] = float32(v)
	}

	return result, nil
}

// EmbedBatch computes embeddings for multiple texts.
// Calls Embed sequentially since Ollama doesn't support batch natively.
// For performance, limit batch size to 20 texts per call.
func (c *Client) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	results := make([][]float32, len(texts))
	for i, text := range texts {
		emb, err := c.Embed(ctx, text)
		if err != nil {
			return nil, fmt.Errorf("embedding text %d: %w", i, err)
		}
		results[i] = emb
	}

	return results, nil
}

// Dimension returns the embedding dimension configured for this client.
func (c *Client) Dimension() int {
	return c.dim
}
