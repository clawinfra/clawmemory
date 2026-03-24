package search

import (
	"context"
	"fmt"

	"github.com/clawinfra/clawmemory/internal/embed"
	"github.com/clawinfra/clawmemory/internal/store"
)

// VectorSearch wraps brute-force cosine similarity search over stored embeddings.
// For the expected data volume (<100K facts), brute-force is fast enough.
type VectorSearch struct {
	store    store.Store
	embedder *embed.Client
}

// NewVector creates a vector searcher.
func NewVector(s store.Store, embedder *embed.Client) *VectorSearch {
	return &VectorSearch{store: s, embedder: embedder}
}

// Search computes the query embedding, then finds top-k facts by cosine similarity.
// Threshold filters out results below minimum similarity (default 0.3).
func (v *VectorSearch) Search(ctx context.Context, query string, limit int, threshold float64) ([]Result, error) {
	if limit <= 0 {
		limit = 10
	}

	emb, err := v.embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	facts, err := v.store.SearchVector(ctx, emb, limit, threshold)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}

	results := make([]Result, 0, len(facts))
	for i, f := range facts {
		score := 1.0 / (60.0 + float64(i+1))
		results = append(results, Result{
			FactID:    f.ID,
			Content:   f.Content,
			Category:  f.Category,
			Container: f.Container,
			Importance: f.Importance,
			Score:     score,
			VecScore:  score,
		})
	}
	return results, nil
}
