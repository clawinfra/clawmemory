// Package store provides the storage interface and implementations for ClawMemory.
// Facts, turns, profile entries, and sync state are all managed here.
package store

import "context"

// FactRecord is the full database representation of a fact.
type FactRecord struct {
	ID           string    `json:"id"`
	Content      string    `json:"content"`
	Category     string    `json:"category"`
	Container    string    `json:"container"`
	Importance   float64   `json:"importance"`
	Confidence   float64   `json:"confidence"`
	Source       string    `json:"source"`        // conversation turn ID or "manual"
	CreatedAt    int64     `json:"created_at"`    // unix timestamp millis
	UpdatedAt    int64     `json:"updated_at"`
	ExpiresAt    *int64    `json:"expires_at"`    // nil = never
	SupersededBy *string   `json:"superseded_by"` // FK to newer fact
	Embedding    []float32 `json:"embedding"`     // 3584-dim vector (may be nil)
	Deleted      bool      `json:"deleted"`       // soft delete
}

// TurnRecord stores raw conversation turns for extraction.
type TurnRecord struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt int64  `json:"created_at"`
	Processed bool   `json:"processed"`
}

// ProfileEntry is a key-value pair in the user profile.
type ProfileEntry struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	UpdatedAt int64  `json:"updated_at"`
}

// ListFactsOpts configures fact listing.
type ListFactsOpts struct {
	Container         string // filter by container ("" = all)
	Category          string // filter by category ("" = all)
	IncludeSuperseded bool   // include superseded facts
	IncludeDeleted    bool   // include soft-deleted facts
	Limit             int    // max results (default 100)
	Offset            int    // pagination offset
}

// StoreStats returns database statistics.
type StoreStats struct {
	TotalFacts       int   `json:"total_facts"`
	ActiveFacts      int   `json:"active_facts"`
	SupersededFacts  int   `json:"superseded_facts"`
	DeletedFacts     int   `json:"deleted_facts"`
	TotalTurns       int   `json:"total_turns"`
	UnprocessedTurns int   `json:"unprocessed_turns"`
	ProfileEntries   int   `json:"profile_entries"`
	DBSizeBytes      int64 `json:"db_size_bytes"`
}

// Store defines the storage interface for ClawMemory.
type Store interface {
	// Facts
	InsertFact(ctx context.Context, fact *FactRecord) error
	GetFact(ctx context.Context, id string) (*FactRecord, error)
	UpdateFact(ctx context.Context, fact *FactRecord) error
	SoftDeleteFact(ctx context.Context, id string) error
	ListFacts(ctx context.Context, opts ListFactsOpts) ([]*FactRecord, error)
	SupersedeFact(ctx context.Context, oldID, newID string) error

	// Turns
	InsertTurn(ctx context.Context, turn *TurnRecord) error
	GetUnprocessedTurns(ctx context.Context, limit int) ([]*TurnRecord, error)
	MarkTurnProcessed(ctx context.Context, id string) error

	// Profile
	SetProfile(ctx context.Context, key, value string) error
	GetProfile(ctx context.Context, key string) (*ProfileEntry, error)
	ListProfile(ctx context.Context) ([]*ProfileEntry, error)
	DeleteProfile(ctx context.Context, key string) error

	// Search (BM25 via FTS5)
	SearchFTS(ctx context.Context, query string, limit int) ([]*FactRecord, error)

	// Vector search (facts with embeddings)
	SearchVector(ctx context.Context, queryEmbedding []float32, limit int, threshold float64) ([]*FactRecord, error)

	// Decay
	ListDecayable(ctx context.Context, before int64, minImportance float64) ([]*FactRecord, error)
	PruneFacts(ctx context.Context, ids []string) (int, error)

	// Sync
	LastSyncTimestamp(ctx context.Context) (int64, error)
	SetLastSyncTimestamp(ctx context.Context, ts int64) error

	// Maintenance
	Close() error
	Stats(ctx context.Context) (*StoreStats, error)
}
