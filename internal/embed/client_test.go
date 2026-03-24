package embed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func makeMockOllama(t *testing.T, dim int, statusCode int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embeddings" {
			http.NotFound(w, r)
			return
		}
		if statusCode != http.StatusOK {
			w.WriteHeader(statusCode)
			return
		}
		emb := make([]float64, dim)
		for i := range emb {
			emb[i] = float64(i) * 0.001
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(embeddingResponse{Embedding: emb})
	}))
}

func TestEmbed_Success(t *testing.T) {
	dim := 3584
	srv := makeMockOllama(t, dim, http.StatusOK)
	defer srv.Close()

	c := New(srv.URL, "qwen2.5:7b", dim)
	emb, err := c.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(emb) != dim {
		t.Errorf("expected %d dimensions, got %d", dim, len(emb))
	}
	// Check first value
	if emb[0] != float32(0.0) {
		t.Errorf("expected emb[0]=0.0, got %f", emb[0])
	}
}

func TestEmbed_WrongDimension(t *testing.T) {
	// Server returns 10-dim embedding but client expects 3584
	srv := makeMockOllama(t, 10, http.StatusOK)
	defer srv.Close()

	c := New(srv.URL, "qwen2.5:7b", 3584)
	_, err := c.Embed(context.Background(), "hello")
	if err == nil {
		t.Error("expected error for wrong dimension, got nil")
	}
}

func TestEmbed_ServerError(t *testing.T) {
	srv := makeMockOllama(t, 3584, http.StatusInternalServerError)
	defer srv.Close()

	c := New(srv.URL, "qwen2.5:7b", 3584)
	_, err := c.Embed(context.Background(), "hello")
	if err == nil {
		t.Error("expected error for server 500, got nil")
	}
}

func TestEmbed_Timeout(t *testing.T) {
	// Server that hangs
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer srv.Close()

	c := New(srv.URL, "qwen2.5:7b", 3584)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := c.Embed(ctx, "hello")
	if err == nil {
		t.Error("expected timeout error, got nil")
	}
}

func TestEmbed_UnreachableServer(t *testing.T) {
	c := New("http://127.0.0.1:1", "qwen2.5:7b", 3584)
	_, err := c.Embed(context.Background(), "hello")
	if err == nil {
		t.Error("expected error for unreachable server, got nil")
	}
}

func TestEmbedBatch(t *testing.T) {
	dim := 3584
	srv := makeMockOllama(t, dim, http.StatusOK)
	defer srv.Close()

	c := New(srv.URL, "qwen2.5:7b", dim)
	texts := []string{"hello", "world", "foo", "bar", "baz"}

	results, err := c.EmbedBatch(context.Background(), texts)
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	if len(results) != len(texts) {
		t.Errorf("expected %d results, got %d", len(texts), len(results))
	}
	for i, emb := range results {
		if len(emb) != dim {
			t.Errorf("result %d: expected %d dims, got %d", i, dim, len(emb))
		}
	}
}

func TestEmbedBatch_Empty(t *testing.T) {
	c := New("http://localhost:11434", "qwen2.5:7b", 3584)
	results, err := c.EmbedBatch(context.Background(), nil)
	if err != nil {
		t.Fatalf("EmbedBatch empty: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results for empty input, got %v", results)
	}
}

func TestDimension(t *testing.T) {
	c := New("http://localhost:11434", "qwen2.5:7b", 3584)
	if c.Dimension() != 3584 {
		t.Errorf("expected dimension 3584, got %d", c.Dimension())
	}

	c2 := New("http://localhost:11434", "nomic-embed", 768)
	if c2.Dimension() != 768 {
		t.Errorf("expected dimension 768, got %d", c2.Dimension())
	}
}

func TestEmbedBatch_ErrorMidway(t *testing.T) {
	dim := 3584
	count := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		if count >= 2 {
			// Fail on second request
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		emb := make([]float64, dim)
		json.NewEncoder(w).Encode(embeddingResponse{Embedding: emb})
	}))
	defer srv.Close()

	c := New(srv.URL, "qwen2.5:7b", dim)
	texts := []string{"text1", "text2", "text3"}

	_, err := c.EmbedBatch(context.Background(), texts)
	if err == nil {
		t.Error("expected error when batch fails midway")
	}
}

func TestEmbed_BadURL(t *testing.T) {
	c := New("http://invalid-host-12345.example.com:11434", "qwen2.5:7b", 3584)
	_, err := c.Embed(context.Background(), "hello")
	if err == nil {
		t.Error("expected error for invalid host")
	}
}

func TestEmbed_InvalidResponseJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not valid json"))
	}))
	defer srv.Close()

	c := New(srv.URL, "qwen2.5:7b", 3584)
	_, err := c.Embed(context.Background(), "hello")
	if err == nil {
		t.Error("expected error for invalid JSON response")
	}
}
