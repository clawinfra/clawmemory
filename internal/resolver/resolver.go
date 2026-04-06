// Package resolver provides contradiction detection and resolution for ClawMemory facts.
// When a new fact is added that conflicts with existing facts (same topic, different value),
// the resolver detects the contradiction and applies resolution strategies.
// v0.2.0: Uses BM25 keyword search for candidate retrieval (no vector embeddings).
package resolver

import (
	"context"
	"fmt"

	"github.com/clawinfra/clawmemory/internal/search"
	"github.com/clawinfra/clawmemory/internal/store"
)

// Contradiction represents a detected conflict between facts.
type Contradiction struct {
	ExistingFact *store.FactRecord `json:"existing_fact"`
	NewFact      *store.FactRecord `json:"new_fact"`
	Similarity   float64           `json:"similarity"` // BM25 rank-based score
	Resolution   string            `json:"resolution"` // "supersede" | "coexist" | "discard_new"
}

// Resolver detects and resolves contradictions between facts.
type Resolver struct {
	store                  store.Store
	searcher               *search.Searcher
	contradictionThreshold float64 // default 0.85 (kept for API compat)
}

// New creates a Resolver with BM25-based candidate retrieval.
func New(s store.Store, searcher *search.Searcher) *Resolver {
	return &Resolver{
		store:                  s,
		searcher:               searcher,
		contradictionThreshold: 0.85,
	}
}

// NewWithThreshold creates a Resolver with a custom contradiction threshold.
func NewWithThreshold(s store.Store, searcher *search.Searcher, threshold float64) *Resolver {
	return &Resolver{
		store:                  s,
		searcher:               searcher,
		contradictionThreshold: threshold,
	}
}

// Check examines a new fact against existing facts for potential contradictions.
// Uses BM25 search to find keyword-similar facts, then checks for content differences.
// Returns an empty slice (no contradictions) when the store is empty or search fails.
func (r *Resolver) Check(ctx context.Context, newFact *store.FactRecord) ([]Contradiction, error) {
	if r.searcher == nil {
		return nil, nil
	}

	// Search for top 5 similar existing facts by keyword
	results, err := r.searcher.Search(ctx, newFact.Content, search.SearchOpts{Limit: 5})
	if err != nil {
		// Graceful degradation
		return nil, nil
	}

	var contradictions []Contradiction
	for _, result := range results {
		// Skip if it's the same fact
		if result.FactID == newFact.ID {
			continue
		}

		// Fetch full fact record
		existing, err := r.store.GetFact(ctx, result.FactID)
		if err != nil || existing == nil {
			continue
		}

		// Skip if already superseded
		if existing.SupersededBy != nil {
			continue
		}

		// Check if content differs meaningfully (not just a duplicate)
		if existing.Content == newFact.Content {
			continue // exact duplicate, not a contradiction
		}

		contradictions = append(contradictions, Contradiction{
			ExistingFact: existing,
			NewFact:      newFact,
			Similarity:   result.Score,
			Resolution:   "supersede", // newer fact wins by default
		})
	}

	return contradictions, nil
}

// Resolve applies the resolution strategy to a detected contradiction.
// For "supersede": sets old_fact.superseded_by = new_fact.id, lowers old_fact.confidence to 0.3.
// For "coexist": no change (both facts remain active).
// For "discard_new": returns an error — caller should not insert the new fact.
func (r *Resolver) Resolve(ctx context.Context, contradiction *Contradiction) error {
	switch contradiction.Resolution {
	case "supersede":
		return r.store.SupersedeFact(ctx, contradiction.ExistingFact.ID, contradiction.NewFact.ID)
	case "coexist":
		// No change needed
		return nil
	case "discard_new":
		return fmt.Errorf("new fact discarded: conflicts with existing fact %s", contradiction.ExistingFact.ID)
	default:
		return fmt.Errorf("unknown resolution strategy: %s", contradiction.Resolution)
	}
}
