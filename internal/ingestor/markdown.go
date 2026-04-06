// Package ingestor provides markdown ingestion support for ClawMemory.
// It reads markdown content from local files or remote URLs, splits it
// into chunks and sends each chunk to the ClawMemory ingest API.
package ingestor

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// MarkdownSource reads markdown content from a local file path or a remote URL.
type MarkdownSource struct {
	// Source is either a local file path or an http/https URL.
	Source string
}

// NewMarkdownSource creates a new MarkdownSource for the given path or URL.
func NewMarkdownSource(source string) *MarkdownSource {
	return &MarkdownSource{Source: source}
}

// Read fetches the markdown content from the source.
// If the source starts with "http://" or "https://", it performs an HTTP GET.
// Otherwise, it reads the file from the local filesystem.
func (m *MarkdownSource) Read() (string, error) {
	if strings.HasPrefix(m.Source, "http://") || strings.HasPrefix(m.Source, "https://") {
		return m.readURL()
	}
	return m.readFile()
}

func (m *MarkdownSource) readFile() (string, error) {
	data, err := os.ReadFile(m.Source)
	if err != nil {
		return "", fmt.Errorf("read file %q: %w", m.Source, err)
	}
	return string(data), nil
}

func (m *MarkdownSource) readURL() (string, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(m.Source)
	if err != nil {
		return "", fmt.Errorf("fetch url %q: %w", m.Source, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch url %q: unexpected status %d", m.Source, resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read url body %q: %w", m.Source, err)
	}
	return string(data), nil
}
