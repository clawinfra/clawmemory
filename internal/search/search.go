// Package search provides hybrid BM25 + vector search for ClawMemory facts.
// It combines keyword-based FTS5 search with semantic vector similarity using
// Reciprocal Rank Fusion (RRF) to produce ranked results.
package search

import (
	"context"
	"sort"

	"github.com/clawinfra/clawmemory/internal/embed"
	"github.com/clawinfra/clawmemory/internal/store"
)

// Result is a single search result with relevance score.
type Result struct {
	FactID    string  `json:"fact_id"`
	Content   string  `json:"content"`
	Category  string  `json:"category"`
	Container string  `json:"container"`
	Importance float64 `json:"importance"`
	Score     float64 `json:"score"`      // combined relevance score (0.0-1.0)
	BM25Score float64 `json:"bm25_score"` // keyword match score
	VecScore  float64 `json:"vec_score"`  // semantic similarity score
}

// SearchOpts configures a search query.
type SearchOpts struct {
	Limit     int     // max results (default 10)
	Container string  // filter by container ("" = all)
	Threshold float64 // minimum score threshold (default 0.0)
}

// Searcher combines BM25 and vector search with reciprocal rank fusion.
type Searcher struct {
	store      store.Store
	embedder   *embed.Client
	bm25Weight float64 // default 0.4
	vecWeight  float64 // default 0.6
}

// New creates a hybrid Searcher.
func New(s store.Store, embedder *embed.Client, bm25Weight, vecWeight float64) *Searcher {
	if bm25Weight <= 0 {
		bm25Weight = 0.4
	}
	if vecWeight <= 0 {
		vecWeight = 0.6
	}
	return &Searcher{
		store:      s,
		embedder:   embedder,
		bm25Weight: bm25Weight,
		vecWeight:  vecWeight,
	}
}

// Search performs hybrid BM25 + vector search, fuses results with RRF, returns top-k.
// Steps:
//  1. Run BM25 search via store.SearchFTS (SQLite FTS5)
//  2. Compute query embedding via embedder (if available)
//  3. Run vector search via store.SearchVector (brute-force cosine)
//  4. Reciprocal Rank Fusion: score_i = bm25Weight/(k+rank_bm25) + vecWeight/(k+rank_vec), k=60
//  5. Sort by fused score descending
//  6. Return top limit results
func (s *Searcher) Search(ctx context.Context, query string, opts SearchOpts) ([]Result, error) {
	if opts.Limit <= 0 {
		opts.Limit = 10
	}

	// Fetch more results from each source to fuse well
	fetchLimit := opts.Limit * 3
	if fetchLimit < 20 {
		fetchLimit = 20
	}

	// 1. BM25 search
	bm25Facts, err := s.store.SearchFTS(ctx, query, fetchLimit)
	if err != nil {
		// FTS may fail on special chars — fallback gracefully
		bm25Facts = nil
	}

	// 2. Vector search (best-effort — may not be available if Ollama is down)
	var vecFacts []*store.FactRecord
	if s.embedder != nil {
		emb, embErr := s.embedder.Embed(ctx, query)
		if embErr == nil {
			vecFacts, _ = s.store.SearchVector(ctx, emb, fetchLimit, 0.0)
		}
		// If embedder fails, gracefully degrade to BM25-only
	}

	// 3. Reciprocal Rank Fusion
	results := reciprocalRankFusion(bm25Facts, vecFacts, s.bm25Weight, s.vecWeight)

	// 4. Filter by container
	if opts.Container != "" {
		filtered := results[:0]
		for _, r := range results {
			if r.Container == opts.Container {
				filtered = append(filtered, r)
			}
		}
		results = filtered
	}

	// 5. Filter by threshold
	if opts.Threshold > 0 {
		filtered := results[:0]
		for _, r := range results {
			if r.Score >= opts.Threshold {
				filtered = append(filtered, r)
			}
		}
		results = filtered
	}

	// 6. Return top-k
	if len(results) > opts.Limit {
		results = results[:opts.Limit]
	}

	return results, nil
}

// reciprocalRankFusion combines BM25 and vector results using RRF.
// k=60 is the standard RRF constant.
func reciprocalRankFusion(bm25Facts, vecFacts []*store.FactRecord, bm25W, vecW float64) []Result {
	const k = 60.0

	// Build rank maps: factID -> rank (1-indexed)
	bm25Ranks := make(map[string]int, len(bm25Facts))
	for i, f := range bm25Facts {
		bm25Ranks[f.ID] = i + 1
	}

	vecRanks := make(map[string]int, len(vecFacts))
	for i, f := range vecFacts {
		vecRanks[f.ID] = i + 1
	}

	// Build combined fact map
	allFacts := make(map[string]*store.FactRecord, len(bm25Facts)+len(vecFacts))
	for _, f := range bm25Facts {
		allFacts[f.ID] = f
	}
	for _, f := range vecFacts {
		allFacts[f.ID] = f
	}

	// Compute fused scores
	type scored struct {
		id        string
		score     float64
		bm25Score float64
		vecScore  float64
	}

	scores := make(map[string]*scored, len(allFacts))
	for id := range allFacts {
		sc := &scored{id: id}

		bm25Rank, hasBM25 := bm25Ranks[id]
		vecRank, hasVec := vecRanks[id]

		if hasBM25 {
			sc.bm25Score = bm25W / (k + float64(bm25Rank))
		}
		if hasVec {
			sc.vecScore = vecW / (k + float64(vecRank))
		}
		sc.score = sc.bm25Score + sc.vecScore
		scores[id] = sc
	}

	// Sort by score descending
	sortedScores := make([]*scored, 0, len(scores))
	for _, sc := range scores {
		sortedScores = append(sortedScores, sc)
	}
	sort.Slice(sortedScores, func(i, j int) bool {
		return sortedScores[i].score > sortedScores[j].score
	})

	// Build result slice
	results := make([]Result, 0, len(sortedScores))
	for _, sc := range sortedScores {
		f := allFacts[sc.id]
		results = append(results, Result{
			FactID:    f.ID,
			Content:   f.Content,
			Category:  f.Category,
			Container: f.Container,
			Importance: f.Importance,
			Score:     sc.score,
			BM25Score: sc.bm25Score,
			VecScore:  sc.vecScore,
		})
	}
	return results
}

// BM25Only searches using only BM25 (keyword) search.
// Used as fallback when vector search is unavailable.
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
