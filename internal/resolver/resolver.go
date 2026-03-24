// Package resolver provides contradiction detection and resolution for ClawMemory facts.
// When a new fact is added that conflicts with existing facts (same topic, different value),
// the resolver detects the contradiction and applies resolution strategies.
package resolver

import (
	"context"
	"fmt"

	"github.com/clawinfra/clawmemory/internal/embed"
	"github.com/clawinfra/clawmemory/internal/search"
	"github.com/clawinfra/clawmemory/internal/store"
)

// Contradiction represents a detected conflict between facts.
type Contradiction struct {
	ExistingFact *store.FactRecord `json:"existing_fact"`
	NewFact      *store.FactRecord `json:"new_fact"`
	Similarity   float64           `json:"similarity"` // semantic similarity between the two
	Resolution   string            `json:"resolution"` // "supersede" | "coexist" | "discard_new"
}

// Resolver detects and resolves contradictions between facts.
type Resolver struct {
	store                  store.Store
	searcher               *search.Searcher
	embedder               *embed.Client
	contradictionThreshold float64 // default 0.85
}

// New creates a Resolver.
func New(s store.Store, searcher *search.Searcher, embedder *embed.Client) *Resolver {
	return &Resolver{
		store:                  s,
		searcher:               searcher,
		embedder:               embedder,
		contradictionThreshold: 0.85,
	}
}

// NewWithThreshold creates a Resolver with a custom contradiction threshold.
func NewWithThreshold(s store.Store, searcher *search.Searcher, embedder *embed.Client, threshold float64) *Resolver {
	return &Resolver{
		store:                  s,
		searcher:               searcher,
		embedder:               embedder,
		contradictionThreshold: threshold,
	}
}

// Check examines a new fact against existing facts for contradictions.
// Algorithm:
//  1. Embed new fact content
//  2. Vector search for top 5 most similar existing facts
//  3. For each with similarity > contradictionThreshold:
//     a. If content differs meaningfully → contradiction detected
//     b. Resolution: newer fact wins (supersede)
//  4. Return list of contradictions found (may be empty)
func (r *Resolver) Check(ctx context.Context, newFact *store.FactRecord) ([]Contradiction, error) {
	if r.embedder == nil {
		return nil, nil // Can't check without embedder
	}

	emb, err := r.embedder.Embed(ctx, newFact.Content)
	if err != nil {
		// Graceful degradation: can't embed, skip contradiction check
		return nil, nil
	}

	// Search for top 5 similar existing facts
	similarFacts, err := r.store.SearchVector(ctx, emb, 5, r.contradictionThreshold)
	if err != nil {
		return nil, fmt.Errorf("vector search for contradiction check: %w", err)
	}

	var contradictions []Contradiction
	for _, existing := range similarFacts {
		// Skip if it's the same fact
		if existing.ID == newFact.ID {
			continue
		}
		// Skip if already superseded
		if existing.SupersededBy != nil {
			continue
		}

		// Compute actual similarity
		sim := computeSimilarity(newFact.Embedding, existing.Embedding)
		if sim < r.contradictionThreshold {
			sim = r.contradictionThreshold // at minimum we already know it's above threshold
		}

		// Check if content differs meaningfully (not just a duplicate)
		if existing.Content == newFact.Content {
			continue // exact duplicate, not a contradiction
		}

		contradictions = append(contradictions, Contradiction{
			ExistingFact: existing,
			NewFact:      newFact,
			Similarity:   sim,
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

// computeSimilarity computes cosine similarity between two float32 slices.
func computeSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		av := float64(a[i])
		bv := float64(b[i])
		dot += av * bv
		normA += av * av
		normB += bv * bv
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	sqrtAB := sqrt64(normA) * sqrt64(normB)
	if sqrtAB == 0 {
		return 0
	}
	return dot / sqrtAB
}

// sqrt64 is a simple square root for float64.
func sqrt64(x float64) float64 {
	if x <= 0 {
		return 0
	}
	// Newton's method
	z := x
	for i := 0; i < 50; i++ {
		z -= (z*z - x) / (2 * z)
	}
	return z
}
