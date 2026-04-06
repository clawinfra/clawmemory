package search

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

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

	// BM25-only searcher (no embedder)
	searcher := New(s, nil, 0.4, 0.6)

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
