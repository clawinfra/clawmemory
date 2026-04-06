package ingestor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Chunk represents a section of a markdown document.
type Chunk struct {
	// Title is the ## heading that introduced this section, or "" for content
	// that appears before the first ## heading.
	Title   string
	Content string
}

// ChunkMarkdown splits markdown content into chunks by ## headings.
// Content before the first ## heading is returned as a single chunk with an
// empty Title. Each ## heading starts a new chunk whose Title is the heading
// text (without the leading "## ").
func ChunkMarkdown(content string) []Chunk {
	if strings.TrimSpace(content) == "" {
		return nil
	}

	lines := strings.Split(content, "\n")
	var chunks []Chunk
	var currentTitle string
	var currentLines []string

	flush := func() {
		text := strings.TrimSpace(strings.Join(currentLines, "\n"))
		if text != "" {
			chunks = append(chunks, Chunk{Title: currentTitle, Content: text})
		}
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			flush()
			currentTitle = strings.TrimPrefix(line, "## ")
			currentLines = nil
		} else {
			currentLines = append(currentLines, line)
		}
	}
	flush()

	return chunks
}

// ingestRequest mirrors the body expected by POST /api/v1/ingest.
type ingestRequest struct {
	SessionID string    `json:"session_id"`
	Turns     []turn    `json:"turns"`
}

type turn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// IngestResult holds the outcome of a single chunk ingest call.
type IngestResult struct {
	Chunk  Chunk
	Status int
	Err    error
}

// Ingest reads markdown from src, splits it into chunks, and POSTs each chunk
// to serverURL/api/v1/ingest with the given agentID as the session_id.
// If dryRun is true, no HTTP calls are made and the chunks are returned with
// Status 0.
func Ingest(src *MarkdownSource, agentID, serverURL string, dryRun bool) ([]IngestResult, error) {
	content, err := src.Read()
	if err != nil {
		return nil, fmt.Errorf("read source: %w", err)
	}

	chunks := ChunkMarkdown(content)
	if len(chunks) == 0 {
		return nil, nil
	}

	results := make([]IngestResult, 0, len(chunks))

	client := &http.Client{Timeout: 30 * time.Second}

	for _, chunk := range chunks {
		result := IngestResult{Chunk: chunk}

		if dryRun {
			results = append(results, result)
			continue
		}

		// Build the chunk text. Include the title as a heading if present.
		chunkText := chunk.Content
		if chunk.Title != "" {
			chunkText = "## " + chunk.Title + "\n\n" + chunk.Content
		}

		reqBody := ingestRequest{
			SessionID: agentID,
			Turns: []turn{
				{Role: "user", Content: chunkText},
			},
		}

		bodyBytes, err := json.Marshal(reqBody)
		if err != nil {
			result.Err = fmt.Errorf("marshal request: %w", err)
			results = append(results, result)
			continue
		}

		resp, err := client.Post(
			serverURL+"/api/v1/ingest",
			"application/json",
			bytes.NewReader(bodyBytes),
		)
		if err != nil {
			result.Err = fmt.Errorf("post ingest: %w", err)
			results = append(results, result)
			continue
		}
		resp.Body.Close()
		result.Status = resp.StatusCode

		if resp.StatusCode != http.StatusOK {
			result.Err = fmt.Errorf("server returned status %d", resp.StatusCode)
		}

		results = append(results, result)
	}

	return results, nil
}
