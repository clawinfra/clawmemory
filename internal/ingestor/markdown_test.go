package ingestor

import (
	"testing"
)

func TestChunkMarkdown_Normal(t *testing.T) {
	content := `# Title

Some intro text.

## Section One

Content of section one.

## Section Two

Content of section two.
`
	chunks := ChunkMarkdown(content)

	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}

	// First chunk: content before any ## heading (includes the # Title line)
	if chunks[0].Title != "" {
		t.Errorf("expected empty title for pre-heading chunk, got %q", chunks[0].Title)
	}

	if chunks[1].Title != "Section One" {
		t.Errorf("expected title %q, got %q", "Section One", chunks[1].Title)
	}
	if chunks[2].Title != "Section Two" {
		t.Errorf("expected title %q, got %q", "Section Two", chunks[2].Title)
	}
}

func TestChunkMarkdown_NoHeaders(t *testing.T) {
	content := `Just some plain text.

No headers here.
`
	chunks := ChunkMarkdown(content)

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Title != "" {
		t.Errorf("expected empty title, got %q", chunks[0].Title)
	}
}

func TestChunkMarkdown_Empty(t *testing.T) {
	chunks := ChunkMarkdown("")
	if len(chunks) != 0 {
		t.Fatalf("expected 0 chunks for empty content, got %d", len(chunks))
	}
}

func TestChunkMarkdown_WhitespaceOnly(t *testing.T) {
	chunks := ChunkMarkdown("   \n\n\t  ")
	if len(chunks) != 0 {
		t.Fatalf("expected 0 chunks for whitespace-only content, got %d", len(chunks))
	}
}

func TestChunkMarkdown_OnlyHeaders(t *testing.T) {
	content := `## Alpha

## Beta

## Gamma
`
	chunks := ChunkMarkdown(content)
	// Alpha and Beta have no content, Gamma has no content either → 0 chunks
	if len(chunks) != 0 {
		t.Fatalf("expected 0 chunks (all sections empty), got %d", len(chunks))
	}
}

func TestChunkMarkdown_HeadersWithContent(t *testing.T) {
	content := `## Alpha

alpha content

## Beta

beta content
`
	chunks := ChunkMarkdown(content)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0].Title != "Alpha" {
		t.Errorf("expected Alpha, got %q", chunks[0].Title)
	}
	if chunks[1].Title != "Beta" {
		t.Errorf("expected Beta, got %q", chunks[1].Title)
	}
}
