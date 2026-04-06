// Package search provides BM25 full-text search for ClawMemory facts.
// It uses SQLite FTS5 for keyword-based search with ranking.
package search

import (
	"context"
	"sort"

	"github.com/clawinfra/clawmemory/internal/store"
)

// Result is a single search result with relevance score.
type Result struct {
	FactID     string  `json:"fact_id"`
	Content    string  `json:"content"`
	Category   string  `json:"category"`
	Container  string  `json:"container"`
	Importance float64 `json:"importance"`
	Score      float64 `json:"score"`      // combined relevance score (0.0-1.0)
	BM25Score  float64 `json:"bm25_score"` // keyword match score
	VecScore   float64 `json:"vec_score"`  // kept for API compatibility (always 0)
}

// SearchOpts configures a search query.
type SearchOpts struct {
	Limit     int     // max results (default 10)
	Container string  // filter by container ("" = all)
	Threshold float64 // minimum score threshold (default 0.0)
}

// Searcher performs BM25 search via SQLite FTS5.
type Searcher struct {
	store      store.Store
	bm25Weight float64 // default 0.4
}

// New creates a BM25 Searcher. The embedder and vecWeight parameters are ignored
// and kept for backward-compatible call sites during migration.
func New(s store.Store, _ interface{}, bm25Weight, _ float64) *Searcher {
	if bm25Weight <= 0 {
		bm25Weight = 0.4
	}
	return &Searcher{
		store:      s,
		bm25Weight: bm25Weight,
	}
}

// Search performs BM25 full-text search via SQLite FTS5, returns top-k results.
// Steps:
//  1. Run BM25 search via store.SearchFTS (SQLite FTS5)
//  2. Filter by container if specified
//  3. Score via RRF-style ranking
//  4. Filter by threshold
//  5. Return top limit results
func (s *Searcher) Search(ctx context.Context, query string, opts SearchOpts) ([]Result, error) {
	if opts.Limit <= 0 {
		opts.Limit = 10
	}

	// Fetch more results to filter well.
	fetchLimit := opts.Limit * 3
	if fetchLimit < 20 {
		fetchLimit = 20
	}
	if opts.Container != "" {
		// Increase fetch limit to ensure container-specific facts appear in candidates
		fetchLimit = opts.Limit * 20
		if fetchLimit < 200 {
			fetchLimit = 200
		}
	}

	// BM25 search
	bm25Facts, err := s.store.SearchFTS(ctx, query, fetchLimit)
	if err != nil {
		// FTS may fail on special chars — fallback gracefully
		bm25Facts = nil
	}

	// Filter by container
	if opts.Container != "" {
		bm25Facts = filterFactsByContainer(bm25Facts, opts.Container)
	}

	// Score using RRF-style ranking
	results := rankBM25(bm25Facts, s.bm25Weight)

	// Filter by threshold
	if opts.Threshold > 0 {
		filtered := results[:0]
		for _, r := range results {
			if r.Score >= opts.Threshold {
				filtered = append(filtered, r)
			}
		}
		results = filtered
	}

	// Return top-k
	if len(results) > opts.Limit {
		results = results[:opts.Limit]
	}

	return results, nil
}

// rankBM25 converts a slice of BM25-ordered facts into scored Results.
func rankBM25(facts []*store.FactRecord, bm25W float64) []Result {
	const k = 60.0

	type scored struct {
		id        string
		score     float64
		bm25Score float64
	}

	scores := make([]scored, len(facts))
	for i, f := range facts {
		bm25Score := bm25W / (k + float64(i+1))
		scores[i] = scored{id: f.ID, score: bm25Score, bm25Score: bm25Score}
	}

	// Sort by score descending (already sorted by FTS5, but keep for consistency)
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})

	// Build fact map for lookup
	factMap := make(map[string]*store.FactRecord, len(facts))
	for _, f := range facts {
		factMap[f.ID] = f
	}

	results := make([]Result, 0, len(scores))
	for _, sc := range scores {
		f, ok := factMap[sc.id]
		if !ok {
			continue
		}
		results = append(results, Result{
			FactID:    f.ID,
			Content:   f.Content,
			Category:  f.Category,
			Container: f.Container,
			Importance: f.Importance,
			Score:     sc.score,
			BM25Score: sc.bm25Score,
			VecScore:  0,
		})
	}
	return results
}

// filterFactsByContainer returns only facts matching the given container.
func filterFactsByContainer(facts []*store.FactRecord, container string) []*store.FactRecord {
	if container == "" || len(facts) == 0 {
		return facts
	}
	filtered := make([]*store.FactRecord, 0, len(facts)/2)
	for _, f := range facts {
		if f.Container == container {
			filtered = append(filtered, f)
		}
	}
	return filtered
}

// BM25Only searches using only BM25 (keyword) search.
func (s *Searcher) BM25Only(ctx context.Context, query string, opts SearchOpts) ([]Result, error) {
	if opts.Limit <= 0 {
		opts.Limit = 10
	}

	facts, err := s.store.SearchFTS(ctx, query, opts.Limit)
	if err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(facts))
	for i, f := range facts {
		score := 1.0 / (60.0 + float64(i+1)) * s.bm25Weight
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
