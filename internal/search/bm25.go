package search

import (
	"context"
	"fmt"

	"github.com/clawinfra/clawmemory/internal/store"
)

// BM25Search wraps SQLite FTS5 for keyword-based search.
// FTS5 implements Okapi BM25 natively — we just query the virtual table.
type BM25Search struct {
	store store.Store
}

// NewBM25 creates a BM25 searcher backed by SQLite FTS5.
func NewBM25(s store.Store) *BM25Search {
	return &BM25Search{store: s}
}

// Search executes an FTS5 query and returns results sorted by BM25 rank.
// Query is passed through FTS5 query syntax (supports AND, OR, NOT, phrase matching).
func (b *BM25Search) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	if limit <= 0 {
		limit = 10
	}

	facts, err := b.store.SearchFTS(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("BM25 search: %w", err)
	}

	results := make([]Result, 0, len(facts))
	for i, f := range facts {
		// Approximate BM25 score from rank position
		score := 1.0 / (60.0 + float64(i+1))
		results = append(results, Result{
			FactID:    f.ID,
			Content:   f.Content,
			Category:  f.Category,
			Container: f.Container,
			Importance: f.Importance,
			Score:     score,
			BM25Score: score,
		})
	}
	return results, nil
}
