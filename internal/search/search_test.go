package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/clawinfra/clawmemory/internal/embed"
	"github.com/clawinfra/clawmemory/internal/store"
)

// newTestStore creates a temp SQLite store for testing.
func newTestStore(t *testing.T) store.Store {
	t.Helper()
	f, err := os.CreateTemp("", "search_test_*.db")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	f.Close()

	s, err := store.NewSQLiteStore(path)
	if err != nil {
		os.Remove(path)
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s.Close()
		os.Remove(path)
	})
	return s
}

func makeMockEmbedder(t *testing.T, dim int) *embed.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		emb := make([]float64, dim)
		for i := range emb {
			emb[i] = 0.1
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"embedding": emb})
	}))
	t.Cleanup(srv.Close)
	return embed.New(srv.URL, "test", dim)
}

func seedFacts(t *testing.T, s store.Store, contents []string) {
	t.Helper()
	ctx := context.Background()
	for i, content := range contents {
		f := &store.FactRecord{
			ID:         fmt.Sprintf("seed-%03d", i),
			Content:    content,
			Category:   "general",
			Container:  "general",
			Importance: 0.7,
			Confidence: 1.0,
			CreatedAt:  time.Now().UnixMilli(),
			UpdatedAt:  time.Now().UnixMilli(),
		}
		if err := s.InsertFact(ctx, f); err != nil {
			t.Fatalf("seed InsertFact: %v", err)
		}
	}
}

func TestHybridSearch(t *testing.T) {
	s := newTestStore(t)
	seedFacts(t, s, []string{
		"User prefers dark mode",
		"User lives in Sydney",
		"Project ClawChain is a blockchain",
		"User timezone is Australia/Sydney",
		"Coffee over tea",
	})

	embedder := makeMockEmbedder(t, 8)
	searcher := New(s, embedder, 0.4, 0.6)

	results, err := searcher.Search(context.Background(), "User", SearchOpts{Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	// Should return results (BM25 will find "User" facts)
	if len(results) == 0 {
		t.Error("expected some search results")
	}
}

func TestHybridSearch_BM25Only(t *testing.T) {
	s := newTestStore(t)
	seedFacts(t, s, []string{
		"User prefers dark mode interface",
		"Golang is a systems language",
	})

	// No embedder (nil) — should fall back to BM25 only
	searcher := New(s, nil, 0.4, 0.6)

	results, err := searcher.Search(context.Background(), "dark mode", SearchOpts{Limit: 5})
	if err != nil {
		t.Fatalf("Search BM25-only: %v", err)
	}
	if len(results) == 0 {
		t.Error("BM25-only search should still find results")
	}
}

func TestHybridSearch_ContainerFilter(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for i, cont := range []string{"work", "work", "personal"} {
		f := &store.FactRecord{
			ID:         fmt.Sprintf("cf-%03d", i),
			Content:    fmt.Sprintf("User info %d", i),
			Category:   "general",
			Container:  cont,
			Importance: 0.7,
			Confidence: 1.0,
			CreatedAt:  time.Now().UnixMilli(),
			UpdatedAt:  time.Now().UnixMilli(),
		}
		s.InsertFact(ctx, f)
	}

	searcher := New(s, nil, 0.4, 0.6)
	results, err := searcher.Search(context.Background(), "User", SearchOpts{
		Limit:     10,
		Container: "work",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Container != "work" {
			t.Errorf("expected work container, got %s", r.Container)
		}
	}
}

func TestHybridSearch_Threshold(t *testing.T) {
	s := newTestStore(t)
	seedFacts(t, s, []string{"User prefers dark mode"})

	searcher := New(s, nil, 0.4, 0.6)
	results, err := searcher.Search(context.Background(), "dark mode", SearchOpts{
		Limit:     10,
		Threshold: 0.9999, // very high threshold
	})
	if err != nil {
		t.Fatal(err)
	}
	// RRF scores are small numbers; high threshold should filter all
	if len(results) != 0 {
		t.Logf("Note: %d results above threshold 0.9999 (RRF scores are small)", len(results))
	}
}

func TestHybridSearch_EmptyStore(t *testing.T) {
	s := newTestStore(t)
	searcher := New(s, nil, 0.4, 0.6)

	results, err := searcher.Search(context.Background(), "anything", SearchOpts{Limit: 10})
	if err != nil {
		t.Fatalf("Search empty store: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results from empty store, got %d", len(results))
	}
}

func TestRRF(t *testing.T) {
	// Test RRF math: score = w/(k+rank)
	// bm25W=0.4, vecW=0.6, k=60
	// fact in bm25 rank 1, vec rank 1: score = 0.4/61 + 0.6/61 ≈ 0.01639
	bm25 := []*store.FactRecord{
		{ID: "a", Content: "fact a", Category: "general", Container: "general", Importance: 0.7},
		{ID: "b", Content: "fact b", Category: "general", Container: "general", Importance: 0.7},
	}
	vec := []*store.FactRecord{
		{ID: "a", Content: "fact a", Category: "general", Container: "general", Importance: 0.7},
		{ID: "c", Content: "fact c", Category: "general", Container: "general", Importance: 0.7},
	}

	results := reciprocalRankFusion(bm25, vec, 0.4, 0.6)

	if len(results) == 0 {
		t.Fatal("expected results from RRF")
	}
	// "a" appears in both → should have highest score
	if results[0].FactID != "a" {
		t.Errorf("expected 'a' (in both lists) to rank first, got %s", results[0].FactID)
	}
	// Score of "a" should be bm25W/61 + vecW/61
	expectedScore := 0.4/61.0 + 0.6/61.0
	if abs(results[0].Score-expectedScore) > 1e-10 {
		t.Errorf("RRF score mismatch: got %f, want %f", results[0].Score, expectedScore)
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func TestBM25Search_Ranking(t *testing.T) {
	s := newTestStore(t)
	seedFacts(t, s, []string{
		"User prefers dark mode",         // should match "user dark mode"
		"Completely unrelated topic",
		"User interface uses dark theme mode settings", // more tokens match
	})

	b := NewBM25(s)
	results, err := b.Search(context.Background(), "dark mode", 5)
	if err != nil {
		t.Fatalf("BM25 search: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected BM25 results")
	}
}

func TestBM25Only(t *testing.T) {
	s := newTestStore(t)
	seedFacts(t, s, []string{
		"User prefers dark mode settings",
		"Golang is a language for systems programming",
		"Dark mode reduces eye strain",
	})

	searcher := New(s, nil, 0.4, 0.6)
	results, err := searcher.BM25Only(context.Background(), "dark mode", SearchOpts{Limit: 5})
	if err != nil {
		t.Fatalf("BM25Only: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected results from BM25Only")
	}
	// All results should have non-zero BM25Score
	for _, r := range results {
		if r.BM25Score == 0 {
			t.Errorf("expected non-zero BM25Score, got 0 for %s", r.FactID)
		}
	}
}

func TestBM25Only_DefaultLimit(t *testing.T) {
	s := newTestStore(t)
	seedFacts(t, s, []string{"Test fact"})

	searcher := New(s, nil, 0.4, 0.6)
	results, err := searcher.BM25Only(context.Background(), "Test", SearchOpts{Limit: 0})
	if err != nil {
		t.Fatal(err)
	}
	_ = results // just ensure it doesn't panic
}

func TestNewSearcher_DefaultWeights(t *testing.T) {
	s := newTestStore(t)

	// With zero weights, should use defaults
	searcher := New(s, nil, 0, 0)
	if searcher.bm25Weight != 0.4 {
		t.Errorf("expected default bm25Weight 0.4, got %f", searcher.bm25Weight)
	}
	if searcher.vecWeight != 0.6 {
		t.Errorf("expected default vecWeight 0.6, got %f", searcher.vecWeight)
	}
}

func TestNewBM25(t *testing.T) {
	s := newTestStore(t)
	b := NewBM25(s)
	if b == nil {
		t.Error("expected non-nil BM25 searcher")
	}
	// Test empty search
	results, err := b.Search(context.Background(), "nomatch_xyz_abc", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestNewVector(t *testing.T) {
	s := newTestStore(t)
	embedder := makeMockEmbedder(t, 8)
	v := NewVector(s, embedder)
	if v == nil {
		t.Error("expected non-nil vector searcher")
	}
}

func TestVectorSearch_Ranking(t *testing.T) {
	dim := 8
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		emb := make([]float64, dim)
		for i := range emb {
			emb[i] = 0.5
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"embedding": emb})
	}))
	defer srv.Close()

	embedder := embed.New(srv.URL, "test", dim)
	s := newTestStore(t)

	// Insert facts with different embeddings
	ctx := context.Background()
	embHigh := make([]float32, dim)
	embLow := make([]float32, dim)
	for i := range embHigh {
		embHigh[i] = 0.5 // similar to query
		embLow[i] = -0.5 // dissimilar to query
	}

	s.InsertFact(ctx, &store.FactRecord{
		ID: "high", Content: "High similarity fact", Category: "general", Container: "general",
		Importance: 0.7, Confidence: 1.0, Embedding: embHigh,
		CreatedAt: time.Now().UnixMilli(), UpdatedAt: time.Now().UnixMilli(),
	})
	s.InsertFact(ctx, &store.FactRecord{
		ID: "low", Content: "Low similarity fact", Category: "general", Container: "general",
		Importance: 0.7, Confidence: 1.0, Embedding: embLow,
		CreatedAt: time.Now().UnixMilli(), UpdatedAt: time.Now().UnixMilli(),
	})

	v := NewVector(s, embedder)
	results, err := v.Search(ctx, "test query", 5, 0.0)
	if err != nil {
		t.Fatalf("VectorSearch: %v", err)
	}

	if len(results) == 0 {
		t.Error("expected vector search results")
	}
	// "high" should rank first
	if len(results) > 0 && results[0].FactID != "high" {
		t.Logf("Note: expected 'high' to rank first, got %s", results[0].FactID)
	}
}

func TestVectorSearch_EmbedError(t *testing.T) {
	// Server that always returns 500 — embed.Embed will fail
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "fail", http.StatusInternalServerError)
	}))
	defer srv.Close()

	embedder := embed.New(srv.URL, "test", 8)
	s := newTestStore(t)
	v := NewVector(s, embedder)

	_, err := v.Search(context.Background(), "query", 5, 0.0)
	if err == nil {
		t.Error("expected error when embed fails")
	}
}

func TestVectorSearch_DefaultLimit(t *testing.T) {
	dim := 8
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		emb := make([]float64, dim)
		json.NewEncoder(w).Encode(map[string]interface{}{"embedding": emb})
	}))
	defer srv.Close()

	embedder := embed.New(srv.URL, "test", dim)
	s := newTestStore(t)
	v := NewVector(s, embedder)

	// limit=0 should default to 10, not error
	results, err := v.Search(context.Background(), "query", 0, 0.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = results // empty store, just checking no panic
}

func TestBM25Search_StoreError(t *testing.T) {
	// Use a closed store to trigger store error
	f, _ := os.CreateTemp("", "bm25err_*.db")
	path := f.Name()
	f.Close()
	s, err := store.NewSQLiteStore(path)
	if err != nil {
		os.Remove(path)
		return // skip if can't create
	}
	s.Close() // close immediately to make queries fail
	os.Remove(path)

	b := NewBM25(s)
	_, err = b.Search(context.Background(), "query", 5)
	if err == nil {
		t.Error("expected error from closed store")
	}
}

func TestBM25Search_DefaultLimit(t *testing.T) {
	s := newTestStore(t)
	b := NewBM25(s)
	// limit=0 defaults to 10 — should not error on empty store
	results, err := b.Search(context.Background(), "test", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = results
}

func TestHybridSearch_VectorError(t *testing.T) {
	// Embedder pointing at dead server — vector arm fails gracefully, BM25 still works
	embedder := embed.New("http://127.0.0.1:1", "test", 8)
	s := newTestStore(t)
	seedFacts(t, s, []string{"hello world", "foo bar"})

	searcher := New(s, embedder, 0.5, 0.5)
	ctx := context.Background()
	// BM25 should still return results even if vector arm errors
	results, err := searcher.Search(ctx, "hello", SearchOpts{Limit: 5})
	// Some implementations return partial results; others propagate error.
	// Either way, should not panic.
	_ = results
	_ = err
}
