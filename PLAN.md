# ClawMemory — Sovereign Agent Memory System
## Technical Specification v2.0 — 2026-03-24

---

## Vision

A **self-hosted, privacy-first memory engine** for OpenClaw agents — inspired by Supermemory's architecture but with zero third-party data exposure. All data stays in infrastructure we control (local SQLite + Turso cloud sync).

**The thesis:** Supermemory's benchmarks prove what good memory looks like. We build the same capabilities — fact extraction, contradiction resolution, temporal forgetting, hybrid RAG — but own every byte.

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────┐
│                  OpenClaw Plugin (TS)                │
│  Auto-capture (post-turn) + Auto-recall (pre-turn)  │
│  Manual: /remember /recall /profile /forget          │
└────────────────────┬────────────────────────────────┘
                     │ HTTP (localhost:7437)
┌────────────────────▼────────────────────────────────┐
│              ClawMemory Server (Go)                  │
│                                                      │
│  ┌──────────────┐  ┌───────────────┐  ┌──────────┐ │
│  │ Extractor    │  │ Resolver      │  │ Profile  │ │
│  │ (GLM-4.7)   │  │ (contradict.) │  │ Builder  │ │
│  └──────┬───────┘  └───────┬───────┘  └────┬─────┘ │
│         └──────────────────┼───────────────┘        │
│                    ┌───────▼──────┐                  │
│                    │  Store       │                  │
│                    │  (SQLite+    │                  │
│                    │   Turso)     │                  │
│                    └───────┬──────┘                  │
│                            │                         │
│  ┌──────────┐     ┌───────▼──────┐    ┌──────────┐ │
│  │  Search   │     │  Decay       │    │  Embed   │ │
│  │  (BM25+   │     │  (TTL +      │    │  (Ollama │ │
│  │   vector) │     │   importance) │    │  qwen2.5)│ │
│  └──────────┘     └──────────────┘    └──────────┘ │
└─────────────────────────────────────────────────────┘
```

### Storage Layers
- **Hot:** In-context (injected by OpenClaw plugin pre-turn as `[Memory context]` block)
- **Warm:** Local SQLite — fast reads, recent facts, BM25 full-text index
- **Cold:** Turso cloud (`libsql://agentmemory-bowen31337.aws-ap-northeast-1.turso.io`) — sync, backup, cross-device
- **Vector:** Ollama `qwen2.5:7b` embeddings on GPU server (`10.0.0.44:11434`) — 3584-dim vectors for semantic search

---

## 1. Go Package Structure

```
github.com/clawinfra/clawmemory

clawmemory/
├── cmd/
│   └── clawmemory/
│       └── main.go                 # CLI + HTTP server entry point
├── internal/
│   ├── config/
│   │   ├── config.go               # Configuration loading
│   │   └── config_test.go
│   ├── extractor/
│   │   ├── extractor.go            # LLM-based fact extraction
│   │   ├── prompt.go               # Extraction prompt templates
│   │   └── extractor_test.go
│   ├── store/
│   │   ├── store.go                # Store interface
│   │   ├── sqlite.go               # SQLite implementation
│   │   ├── turso.go                # Turso sync manager
│   │   ├── migrations.go           # Schema migrations
│   │   └── store_test.go
│   ├── search/
│   │   ├── search.go               # Search interface + hybrid combiner
│   │   ├── bm25.go                 # BM25 keyword search via FTS5
│   │   ├── vector.go               # Ollama embedding client + cosine similarity
│   │   └── search_test.go
│   ├── resolver/
│   │   ├── resolver.go             # Contradiction detection + resolution
│   │   └── resolver_test.go
│   ├── profile/
│   │   ├── profile.go              # User profile builder + updater
│   │   └── profile_test.go
│   ├── decay/
│   │   ├── decay.go                # Importance decay + TTL management
│   │   └── decay_test.go
│   ├── server/
│   │   ├── server.go               # HTTP API server (net/http)
│   │   ├── handlers.go             # Route handlers
│   │   ├── middleware.go            # Logging, CORS, auth
│   │   └── server_test.go
│   └── embed/
│       ├── client.go               # Ollama embedding HTTP client
│       └── client_test.go
├── plugin/                          # OpenClaw plugin (TypeScript)
│   ├── package.json
│   ├── tsconfig.json
│   ├── src/
│   │   ├── index.ts
│   │   ├── capture.ts
│   │   ├── recall.ts
│   │   └── tools.ts
│   └── manifest.json
├── bench/
│   ├── runner.go                    # Benchmark orchestrator
│   ├── longmemeval.go               # LongMemEval test harness
│   ├── locomo.go                    # LoCoMo test harness
│   ├── convomem.go                  # ConvoMem test harness
│   ├── report.go                    # Report card generator
│   ├── testdata/
│   │   ├── longmemeval_100.jsonl    # 100-question LongMemEval subset
│   │   ├── locomo_50.jsonl          # 50-conversation LoCoMo set
│   │   └── convomem_30.jsonl        # 30 contradiction scenarios
│   └── bench_test.go
├── docs/
│   ├── ARCHITECTURE.md
│   ├── API.md
│   ├── BENCHMARK.md
│   └── PLUGIN.md
├── scripts/
│   ├── setup.sh                     # One-command install
│   ├── bench.sh                     # Run full benchmark suite
│   └── seed.sh                      # Seed test data
├── .github/
│   └── workflows/
│       ├── ci.yml                   # Test + lint + build
│       └── bench.yml                # Weekly benchmark report
├── AGENTS.md
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

### Key Function Signatures

#### `internal/config/config.go`

```go
package config

// Config holds all ClawMemory configuration.
type Config struct {
    Server    ServerConfig    `json:"server"`
    Store     StoreConfig     `json:"store"`
    Embedding EmbeddingConfig `json:"embedding"`
    Extractor ExtractorConfig `json:"extractor"`
    Decay     DecayConfig     `json:"decay"`
    Turso     TursoConfig     `json:"turso"`
}

type ServerConfig struct {
    Host string `json:"host"` // default "127.0.0.1"
    Port int    `json:"port"` // default 7437
}

type StoreConfig struct {
    DBPath string `json:"db_path"` // default "~/.clawmemory/memory.db"
}

type EmbeddingConfig struct {
    OllamaURL string `json:"ollama_url"` // default "http://10.0.0.44:11434"
    Model     string `json:"model"`      // default "qwen2.5:7b"
    Dimension int    `json:"dimension"`  // default 3584
}

type ExtractorConfig struct {
    BaseURL string `json:"base_url"` // GLM-4.7 proxy URL
    Model   string `json:"model"`    // "glm-4.7"
    APIKey  string `json:"api_key"`  // proxy API key
}

type DecayConfig struct {
    HalfLifeDays  float64 `json:"half_life_days"`  // default 30
    MinImportance float64 `json:"min_importance"`  // default 0.1 — below this, auto-prune
    PruneInterval string  `json:"prune_interval"`  // default "1h"
}

type TursoConfig struct {
    URL       string `json:"url"`        // "libsql://agentmemory-bowen31337.aws-ap-northeast-1.turso.io"
    AuthToken string `json:"auth_token"` // from env: TURSO_AUTH_TOKEN
    SyncInterval string `json:"sync_interval"` // default "5m"
}

// Load reads config from path, falls back to defaults, then env overrides.
func Load(path string) (*Config, error)

// Default returns a Config with all defaults filled in.
func Default() *Config
```

#### `internal/extractor/extractor.go`

```go
package extractor

import "context"

// Fact represents a single extracted fact from conversation.
type Fact struct {
    Content    string  `json:"content"`    // "User's timezone is Australia/Sydney"
    Category   string  `json:"category"`   // person|project|preference|event|technical
    Container  string  `json:"container"`  // work|trading|clawchain|personal
    Importance float64 `json:"importance"` // 0.0-1.0
}

// Extractor calls an LLM to extract structured facts from conversation turns.
type Extractor struct {
    baseURL string
    model   string
    apiKey  string
    client  *http.Client
}

// New creates an Extractor targeting GLM-4.7 via proxy.
func New(baseURL, model, apiKey string) *Extractor

// Extract sends the last N turns to GLM-4.7 and returns 0-5 structured facts.
// The prompt instructs the LLM to output JSON array of facts.
// Returns empty slice if no extractable facts found.
func (e *Extractor) Extract(ctx context.Context, turns []Turn) ([]Fact, error)

// Turn represents a single conversation message.
type Turn struct {
    Role    string `json:"role"`    // "user" or "assistant"
    Content string `json:"content"`
}
```

#### `internal/extractor/prompt.go`

```go
package extractor

// extractionSystemPrompt is the system prompt for fact extraction.
// It instructs GLM-4.7 to:
// 1. Read the conversation turns
// 2. Extract 0-5 factual statements
// 3. Classify each fact by category and container
// 4. Rate importance 0.0-1.0
// 5. Output strictly as JSON array
const extractionSystemPrompt = `You are a fact extraction engine. Given conversation turns, extract factual information worth remembering long-term.

Rules:
- Extract 0-5 facts. If nothing is worth remembering, return empty array [].
- Each fact must be a single, atomic statement (e.g., "User prefers dark mode", not "User talked about preferences").
- Category must be one of: person, project, preference, event, technical.
- Container must be one of: work, trading, clawchain, personal, general.
- Importance: 1.0 = critical identity/preference, 0.7 = useful context, 0.3 = minor detail.
- Do NOT extract greetings, small talk, or meta-conversation.
- Do NOT extract information already obvious from the conversation role (e.g., "the user asked a question").

Output format (strict JSON, no markdown):
[{"content":"...","category":"...","container":"...","importance":0.7}]`

// BuildExtractionPrompt formats conversation turns into the user prompt.
func BuildExtractionPrompt(turns []Turn) string
```

#### `internal/store/store.go`

```go
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
    CreatedAt    int64     `json:"created_at"`    // unix timestamp
    UpdatedAt    int64     `json:"updated_at"`
    ExpiresAt    *int64    `json:"expires_at"`    // nil = never
    SupersededBy *string   `json:"superseded_by"` // FK to newer fact
    Embedding    []float32 `json:"embedding"`     // 3584-dim vector
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

// ListFactsOpts configures fact listing.
type ListFactsOpts struct {
    Container    string // filter by container ("" = all)
    Category     string // filter by category ("" = all)
    IncludeSuperseded bool // include superseded facts
    IncludeDeleted    bool // include soft-deleted facts
    Limit        int    // max results (default 100)
    Offset       int    // pagination offset
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
```

#### `internal/store/sqlite.go`

```go
package store

import (
    "context"
    "database/sql"
    "encoding/binary"
    "math"

    _ "github.com/tursodatabase/go-libsql" // CGO-based libsql driver
)

// SQLiteStore implements Store using local SQLite + libsql driver.
type SQLiteStore struct {
    db *sql.DB
}

// NewSQLiteStore opens (or creates) the SQLite database at dbPath.
// Runs migrations on first open. Enables WAL mode and FTS5.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error)

// encodeEmbedding converts []float32 to []byte (little-endian float32 array).
func encodeEmbedding(emb []float32) []byte

// decodeEmbedding converts []byte back to []float32.
func decodeEmbedding(data []byte) []float32

// cosineSimilarity computes cosine similarity between two float32 slices.
// Used for in-process vector search (no external vector DB needed).
func cosineSimilarity(a, b []float32) float64

// All Store interface methods implemented on *SQLiteStore...
```

#### `internal/store/turso.go`

```go
package store

import (
    "context"
    "database/sql"
    "sync"
    "time"

    libsql "github.com/tursodatabase/go-libsql"
)

// TursoSync manages background sync between local SQLite and Turso cloud.
type TursoSync struct {
    connector *libsql.Connector // embedded replica connector
    localDB   *sql.DB           // local SQLite handle
    remoteURL string
    authToken string
    interval  time.Duration
    mu        sync.Mutex
    stopCh    chan struct{}
}

// NewTursoSync creates a TursoSync that wraps an embedded replica.
// The local file at dbPath is the SQLite database; Turso is the remote primary.
// Uses go-libsql's embedded replica connector for automatic sync.
func NewTursoSync(dbPath, remoteURL, authToken string, syncInterval time.Duration) (*TursoSync, error)

// Start begins the background sync goroutine.
// Calls connector.Sync() every syncInterval.
func (t *TursoSync) Start()

// Stop halts the background sync goroutine. Blocks until stopped.
func (t *TursoSync) Stop()

// SyncNow triggers an immediate sync. Returns error if sync fails.
func (t *TursoSync) SyncNow(ctx context.Context) error

// DB returns the local database handle (reads are always local).
func (t *TursoSync) DB() *sql.DB
```

#### `internal/store/migrations.go`

```go
package store

// migrations is an ordered list of SQL migration statements.
// Each migration runs exactly once, tracked in a _migrations table.
var migrations = []string{
    migrationV1CreateTables,
    migrationV2AddFTS5,
    migrationV3AddIndexes,
}

// RunMigrations executes all pending migrations inside a transaction.
func RunMigrations(db *sql.DB) error
```

#### `internal/search/search.go`

```go
package search

import "context"

// Result is a single search result with relevance score.
type Result struct {
    FactID     string  `json:"fact_id"`
    Content    string  `json:"content"`
    Category   string  `json:"category"`
    Container  string  `json:"container"`
    Importance float64 `json:"importance"`
    Score      float64 `json:"score"`      // combined relevance score (0.0-1.0)
    BM25Score  float64 `json:"bm25_score"` // keyword match score
    VecScore   float64 `json:"vec_score"`  // semantic similarity score
}

// Searcher combines BM25 and vector search with reciprocal rank fusion.
type Searcher struct {
    store    store.Store
    embedder *embed.Client
    bm25Weight float64 // default 0.4
    vecWeight  float64 // default 0.6
}

// New creates a hybrid Searcher.
func New(store store.Store, embedder *embed.Client, bm25Weight, vecWeight float64) *Searcher

// Search performs hybrid BM25 + vector search, fuses results with RRF, returns top-k.
// Steps:
//   1. Run BM25 search via store.SearchFTS (SQLite FTS5)
//   2. Compute query embedding via embedder
//   3. Run vector search via store.SearchVector (brute-force cosine)
//   4. Reciprocal Rank Fusion: score_i = bm25Weight/(k+rank_bm25) + vecWeight/(k+rank_vec), k=60
//   5. Sort by fused score descending
//   6. Return top limit results
func (s *Searcher) Search(ctx context.Context, query string, opts SearchOpts) ([]Result, error)

// SearchOpts configures a search query.
type SearchOpts struct {
    Limit     int     // max results (default 10)
    Container string  // filter by container ("" = all)
    Threshold float64 // minimum score threshold (default 0.0)
}
```

#### `internal/search/bm25.go`

```go
package search

import "context"

// BM25Search wraps SQLite FTS5 for keyword-based search.
// FTS5 implements Okapi BM25 natively — we just query the virtual table.
type BM25Search struct {
    store store.Store
}

// NewBM25 creates a BM25 searcher backed by SQLite FTS5.
func NewBM25(store store.Store) *BM25Search

// Search executes an FTS5 query, returns results sorted by BM25 rank.
// Query is passed through FTS5 query syntax (supports AND, OR, NOT, phrase matching).
func (b *BM25Search) Search(ctx context.Context, query string, limit int) ([]Result, error)
```

#### `internal/search/vector.go`

```go
package search

import "context"

// VectorSearch wraps brute-force cosine similarity search over stored embeddings.
// For the expected data volume (<100K facts), brute-force is fast enough.
// If we exceed 100K facts, we can add HNSW indexing later.
type VectorSearch struct {
    store    store.Store
    embedder *embed.Client
}

// NewVector creates a vector searcher.
func NewVector(store store.Store, embedder *embed.Client) *VectorSearch

// Search computes the query embedding, then finds top-k facts by cosine similarity.
// Threshold filters out results below minimum similarity (default 0.3).
func (v *VectorSearch) Search(ctx context.Context, query string, limit int, threshold float64) ([]Result, error)
```

#### `internal/resolver/resolver.go`

```go
package resolver

import "context"

// Contradiction represents a detected conflict between facts.
type Contradiction struct {
    ExistingFact *store.FactRecord `json:"existing_fact"`
    NewFact      *store.FactRecord `json:"new_fact"`
    Similarity   float64           `json:"similarity"` // semantic similarity between the two
    Resolution   string            `json:"resolution"` // "supersede" | "coexist" | "discard_new"
}

// Resolver detects and resolves contradictions between facts.
type Resolver struct {
    store    store.Store
    searcher *search.Searcher
    embedder *embed.Client
    // Similarity threshold above which two facts are considered potentially contradictory.
    // Same-topic detection: cosine sim > 0.85 AND different content.
    contradictionThreshold float64 // default 0.85
}

// New creates a Resolver.
func New(store store.Store, searcher *search.Searcher, embedder *embed.Client) *Resolver

// Check examines a new fact against existing facts for contradictions.
// Algorithm:
//   1. Embed new fact content
//   2. Vector search for top 5 most similar existing facts
//   3. For each with similarity > contradictionThreshold:
//      a. If content differs meaningfully → contradiction detected
//      b. Resolution: newer fact wins (supersede), mark old fact.superseded_by = new.id
//   4. Return list of contradictions found (may be empty)
func (r *Resolver) Check(ctx context.Context, newFact *store.FactRecord) ([]Contradiction, error)

// Resolve applies the resolution strategy to a detected contradiction.
// For "supersede": sets old_fact.superseded_by = new_fact.id, lowers old_fact.confidence to 0.3.
// For "coexist": no change (both facts remain active).
// For "discard_new": returns error — caller should not insert the new fact.
func (r *Resolver) Resolve(ctx context.Context, contradiction *Contradiction) error
```

#### `internal/profile/profile.go`

```go
package profile

import "context"

// Profile represents the aggregated user profile.
type Profile struct {
    Entries   map[string]string `json:"entries"`
    Summary   string            `json:"summary"`    // LLM-generated summary
    UpdatedAt int64             `json:"updated_at"`
}

// Builder constructs and maintains the user profile from accumulated facts.
type Builder struct {
    store     store.Store
    extractor *extractor.Extractor
}

// New creates a profile Builder.
func New(store store.Store, extractor *extractor.Extractor) *Builder

// Build scans all active facts categorized as "person" or "preference"
// and synthesizes a user profile. Stores result in profile table.
// Called periodically (every 50 turns or on /profile command).
func (b *Builder) Build(ctx context.Context) (*Profile, error)

// Get returns the current profile from the store.
func (b *Builder) Get(ctx context.Context) (*Profile, error)

// Update merges new facts into the existing profile.
// Called after each extraction batch to incrementally update.
func (b *Builder) Update(ctx context.Context, facts []extractor.Fact) error

// Summarize generates a natural-language summary of the profile
// suitable for injection into system prompts.
// Uses GLM-4.7 to produce a 2-3 sentence summary.
func (b *Builder) Summarize(ctx context.Context) (string, error)
```

#### `internal/decay/decay.go`

```go
package decay

import (
    "context"
    "math"
    "time"
)

// Manager handles importance decay and TTL-based pruning of facts.
type Manager struct {
    store        store.Store
    halfLifeDays float64 // default 30
    minImportance float64 // default 0.1
    interval     time.Duration
    stopCh       chan struct{}
}

// New creates a decay Manager.
func New(store store.Store, halfLifeDays, minImportance float64, interval time.Duration) *Manager

// Start begins the background decay loop.
// Every interval:
//   1. Compute decayed importance for all facts: importance * 2^(-age_days/halfLife)
//   2. Any fact where decayed importance < minImportance → auto-prune (soft delete)
//   3. Any fact where expires_at < now → auto-prune
func (m *Manager) Start()

// Stop halts the background decay loop.
func (m *Manager) Stop()

// DecayedImportance calculates current importance after time decay.
// Formula: original_importance * 2^(-age_days / half_life_days)
func DecayedImportance(originalImportance float64, agedays float64, halfLifeDays float64) float64 {
    return originalImportance * math.Pow(2, -ageDays/halfLifeDays)
}

// RunOnce executes a single decay + prune cycle. Exported for testing.
func (m *Manager) RunOnce(ctx context.Context) (pruned int, err error)
```

#### `internal/server/server.go`

```go
package server

import (
    "context"
    "net/http"
)

// Server is the ClawMemory HTTP API server.
type Server struct {
    httpServer *http.Server
    store      store.Store
    searcher   *search.Searcher
    extractor  *extractor.Extractor
    resolver   *resolver.Resolver
    profile    *profile.Builder
    decay      *decay.Manager
    turso      *store.TursoSync
}

// New creates a Server with all dependencies wired.
func New(cfg *config.Config) (*Server, error)

// Start begins listening on the configured host:port.
func (s *Server) Start() error

// Shutdown gracefully shuts down the server, sync, and decay loops.
func (s *Server) Shutdown(ctx context.Context) error
```

#### `internal/server/handlers.go`

```go
package server

import "net/http"

// registerRoutes sets up all HTTP routes on the mux.
func (s *Server) registerRoutes(mux *http.ServeMux)

// handleIngest handles POST /api/v1/ingest — receives conversation turns, extracts facts.
func (s *Server) handleIngest(w http.ResponseWriter, r *http.Request)

// handleRemember handles POST /api/v1/remember — manual fact storage.
func (s *Server) handleRemember(w http.ResponseWriter, r *http.Request)

// handleRecall handles POST /api/v1/recall — hybrid search query.
func (s *Server) handleRecall(w http.ResponseWriter, r *http.Request)

// handleProfile handles GET /api/v1/profile — returns user profile.
func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request)

// handleForget handles POST /api/v1/forget — soft-delete matching facts.
func (s *Server) handleForget(w http.ResponseWriter, r *http.Request)

// handleStats handles GET /api/v1/stats — returns store statistics.
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request)

// handleHealth handles GET /health — liveness check.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request)

// handleSync handles POST /api/v1/sync — triggers immediate Turso sync.
func (s *Server) handleSync(w http.ResponseWriter, r *http.Request)

// handleFacts handles GET /api/v1/facts — list facts with filtering.
func (s *Server) handleFacts(w http.ResponseWriter, r *http.Request)

// handleFactByID handles GET /api/v1/facts/{id} — get single fact.
func (s *Server) handleFactByID(w http.ResponseWriter, r *http.Request)
```

#### `internal/embed/client.go`

```go
package embed

import (
    "context"
    "net/http"
)

// Client wraps the Ollama embedding API.
type Client struct {
    baseURL string // "http://10.0.0.44:11434"
    model   string // "qwen2.5:7b"
    dim     int    // 3584
    client  *http.Client
}

// New creates an Ollama embedding client.
func New(baseURL, model string, dim int) *Client

// Embed computes the embedding vector for a single text string.
// Calls POST http://10.0.0.44:11434/api/embeddings with body:
//   {"model":"qwen2.5:7b","prompt":"<text>"}
// Returns float32 slice of length 3584.
func (c *Client) Embed(ctx context.Context, text string) ([]float32, error)

// EmbedBatch computes embeddings for multiple texts.
// Calls Embed sequentially (Ollama doesn't support batch natively).
// For performance, limit batch size to 20 texts per call.
func (c *Client) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)

// Dimension returns the embedding dimension (3584 for qwen2.5:7b).
func (c *Client) Dimension() int
```

#### `cmd/clawmemory/main.go`

```go
package main

import (
    "flag"
    "log"
    "os"
    "os/signal"
)

func main() {
    // Subcommands:
    //   serve  — start HTTP server (default)
    //   ingest — ingest turns from stdin (JSON lines)
    //   recall — search from CLI
    //   stats  — print store stats
    //   sync   — trigger Turso sync
    //   bench  — run benchmark suite
}
```

---

## 2. Complete SQL Schema

```sql
-- ================================================================
-- ClawMemory Schema v1
-- ================================================================

-- Migration tracking
CREATE TABLE IF NOT EXISTS _migrations (
    version  INTEGER PRIMARY KEY,
    name     TEXT NOT NULL,
    applied_at INTEGER NOT NULL  -- unix timestamp
);

-- ================================================================
-- Migration V1: Core tables
-- ================================================================

-- Core facts table
CREATE TABLE facts (
    id            TEXT PRIMARY KEY,           -- UUIDv7 (time-sortable)
    content       TEXT NOT NULL,              -- "User's timezone is Australia/Sydney"
    category      TEXT NOT NULL               -- person|project|preference|event|technical
                  CHECK(category IN ('person','project','preference','event','technical')),
    container     TEXT NOT NULL DEFAULT 'general' -- work|trading|clawchain|personal|general
                  CHECK(container IN ('work','trading','clawchain','personal','general')),
    importance    REAL NOT NULL DEFAULT 0.7   -- 0.0-1.0
                  CHECK(importance >= 0.0 AND importance <= 1.0),
    confidence    REAL NOT NULL DEFAULT 1.0   -- 0.0-1.0 (lowered on contradiction)
                  CHECK(confidence >= 0.0 AND confidence <= 1.0),
    source        TEXT,                       -- turn ID or "manual"
    created_at    INTEGER NOT NULL,           -- unix timestamp millis
    updated_at    INTEGER NOT NULL,           -- unix timestamp millis
    expires_at    INTEGER,                    -- NULL = never expires
    superseded_by TEXT                        -- FK to newer fact ID
                  REFERENCES facts(id) ON DELETE SET NULL,
    embedding     BLOB,                       -- float32[3584] little-endian (14336 bytes)
    deleted       INTEGER NOT NULL DEFAULT 0  -- 0=active, 1=soft-deleted
);

-- Conversation turns (raw, for extraction context)
CREATE TABLE turns (
    id          TEXT PRIMARY KEY,            -- UUIDv7
    session_id  TEXT NOT NULL,               -- groups turns by conversation
    role        TEXT NOT NULL                 -- user|assistant
                CHECK(role IN ('user','assistant')),
    content     TEXT NOT NULL,
    created_at  INTEGER NOT NULL,            -- unix timestamp millis
    processed   INTEGER NOT NULL DEFAULT 0   -- 0=pending extraction, 1=done
);

-- User profile (stable key-value pairs)
CREATE TABLE profile (
    key         TEXT PRIMARY KEY,
    value       TEXT NOT NULL,
    updated_at  INTEGER NOT NULL             -- unix timestamp millis
);

-- Sync state tracking (for Turso sync coordination)
CREATE TABLE sync_state (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- ================================================================
-- Migration V2: FTS5 full-text search
-- ================================================================

-- FTS5 virtual table for BM25 keyword search on facts
CREATE VIRTUAL TABLE facts_fts USING fts5(
    content,          -- fact text (indexed)
    category,         -- filterable
    container,        -- filterable
    content=facts,    -- content table is facts
    content_rowid=rowid  -- auto-sync with facts rowid
);

-- Triggers to keep FTS5 in sync with facts table
CREATE TRIGGER facts_ai AFTER INSERT ON facts BEGIN
    INSERT INTO facts_fts(rowid, content, category, container)
    VALUES (new.rowid, new.content, new.category, new.container);
END;

CREATE TRIGGER facts_ad AFTER DELETE ON facts BEGIN
    INSERT INTO facts_fts(facts_fts, rowid, content, category, container)
    VALUES ('delete', old.rowid, old.content, old.category, old.container);
END;

CREATE TRIGGER facts_au AFTER UPDATE ON facts BEGIN
    INSERT INTO facts_fts(facts_fts, rowid, content, category, container)
    VALUES ('delete', old.rowid, old.content, old.category, old.container);
    INSERT INTO facts_fts(rowid, content, category, container)
    VALUES (new.rowid, new.content, new.category, new.container);
END;

-- ================================================================
-- Migration V3: Indexes
-- ================================================================

-- Fact lookup indexes
CREATE INDEX idx_facts_container ON facts(container) WHERE deleted = 0;
CREATE INDEX idx_facts_category ON facts(category) WHERE deleted = 0;
CREATE INDEX idx_facts_created_at ON facts(created_at) WHERE deleted = 0;
CREATE INDEX idx_facts_importance ON facts(importance) WHERE deleted = 0;
CREATE INDEX idx_facts_superseded_by ON facts(superseded_by) WHERE superseded_by IS NOT NULL;
CREATE INDEX idx_facts_expires_at ON facts(expires_at) WHERE expires_at IS NOT NULL AND deleted = 0;

-- Turn processing index
CREATE INDEX idx_turns_processed ON turns(processed, created_at) WHERE processed = 0;
CREATE INDEX idx_turns_session ON turns(session_id, created_at);
```

---

## 3. HTTP API Endpoints

Base URL: `http://127.0.0.1:7437`

All request/response bodies are JSON. Errors return `{"error": "message"}` with appropriate HTTP status.

### `GET /health`

Health check.

**Response 200:**
```json
{
  "status": "ok",
  "version": "0.1.0",
  "uptime_seconds": 3600,
  "store": {
    "active_facts": 1234,
    "db_size_bytes": 5242880
  }
}
```

### `POST /api/v1/ingest`

Ingest conversation turns for fact extraction. This is the primary auto-capture endpoint.

**Request:**
```json
{
  "session_id": "sess_abc123",
  "turns": [
    {"role": "user", "content": "I moved to Melbourne last week"},
    {"role": "assistant", "content": "Nice! How are you finding Melbourne?"}
  ]
}
```

**Response 200:**
```json
{
  "extracted_facts": [
    {
      "id": "01JABC123...",
      "content": "User moved to Melbourne recently",
      "category": "person",
      "container": "personal",
      "importance": 0.8
    }
  ],
  "contradictions": [
    {
      "existing_fact_id": "01JXYZ789...",
      "existing_content": "User lives in Sydney",
      "new_fact_id": "01JABC123...",
      "resolution": "supersede"
    }
  ],
  "turns_stored": 2
}
```

**Response 400:** `{"error": "turns array is required"}`

### `POST /api/v1/remember`

Manually store a fact (used by `/remember` command).

**Request:**
```json
{
  "content": "The project deadline is April 15th",
  "category": "event",
  "container": "work",
  "importance": 0.9,
  "expires_at": 1713139200
}
```

**Response 201:**
```json
{
  "id": "01JABC456...",
  "content": "The project deadline is April 15th",
  "category": "event",
  "container": "work",
  "importance": 0.9,
  "contradictions": []
}
```

### `POST /api/v1/recall`

Hybrid search (BM25 + vector). This is the primary auto-recall endpoint.

**Request:**
```json
{
  "query": "What timezone does the user prefer?",
  "limit": 10,
  "container": "",
  "threshold": 0.0,
  "include_profile": true
}
```

**Response 200:**
```json
{
  "results": [
    {
      "fact_id": "01JXYZ789...",
      "content": "User's timezone is Australia/Sydney",
      "category": "preference",
      "container": "personal",
      "importance": 0.9,
      "score": 0.87,
      "bm25_score": 0.72,
      "vec_score": 0.95
    }
  ],
  "profile_summary": "A technical founder based in Australia/Sydney who works on EvoClaw and ClawChain...",
  "total_results": 1,
  "search_latency_ms": 45
}
```

### `GET /api/v1/profile`

Get the current user profile.

**Response 200:**
```json
{
  "entries": {
    "timezone": "Australia/Sydney",
    "role": "Technical founder",
    "primary_projects": "EvoClaw, ClawChain",
    "communication_style": "Direct, skip filler"
  },
  "summary": "A technical founder based in Australia/Sydney who works on EvoClaw and ClawChain. Prefers direct communication without filler phrases.",
  "updated_at": 1711234567890
}
```

### `POST /api/v1/forget`

Soft-delete facts matching a query.

**Request:**
```json
{
  "query": "old project deadline",
  "max_delete": 5
}
```

**Response 200:**
```json
{
  "deleted_count": 2,
  "deleted_facts": [
    {"id": "01JABC...", "content": "Project deadline is March 1st"},
    {"id": "01JDEF...", "content": "Sprint ends March 15th"}
  ]
}
```

### `GET /api/v1/facts`

List facts with filtering and pagination.

**Query params:** `container`, `category`, `include_superseded` (bool), `include_deleted` (bool), `limit` (int, default 50), `offset` (int, default 0)

**Response 200:**
```json
{
  "facts": [
    {
      "id": "01JABC...",
      "content": "User's timezone is Australia/Sydney",
      "category": "preference",
      "container": "personal",
      "importance": 0.9,
      "confidence": 1.0,
      "created_at": 1711234567890,
      "updated_at": 1711234567890,
      "expires_at": null,
      "superseded_by": null,
      "deleted": false
    }
  ],
  "total": 1234,
  "limit": 50,
  "offset": 0
}
```

### `GET /api/v1/facts/{id}`

Get a single fact by ID.

**Response 200:** Single fact object (same shape as in list).
**Response 404:** `{"error": "fact not found"}`

### `GET /api/v1/stats`

Store statistics.

**Response 200:**
```json
{
  "total_facts": 1234,
  "active_facts": 1100,
  "superseded_facts": 100,
  "deleted_facts": 34,
  "total_turns": 5000,
  "unprocessed_turns": 12,
  "profile_entries": 8,
  "db_size_bytes": 5242880,
  "embedding_dimension": 3584,
  "last_sync_at": 1711234567890
}
```

### `POST /api/v1/sync`

Trigger immediate Turso sync.

**Response 200:**
```json
{
  "synced": true,
  "sync_latency_ms": 230
}
```

---

## 4. OpenClaw Plugin Specification

### `plugin/manifest.json`

```json
{
  "name": "clawmemory",
  "version": "0.1.0",
  "description": "Sovereign agent memory engine — auto-capture facts, auto-recall context",
  "author": "clawinfra",
  "homepage": "https://github.com/clawinfra/clawmemory",
  "type": "plugin",
  "main": "dist/index.js",
  "config": {
    "server_url": {
      "type": "string",
      "default": "http://127.0.0.1:7437",
      "description": "ClawMemory server URL"
    },
    "auto_capture": {
      "type": "boolean",
      "default": true,
      "description": "Automatically extract facts from every conversation turn"
    },
    "auto_recall": {
      "type": "boolean",
      "default": true,
      "description": "Automatically inject relevant memories before each turn"
    },
    "recall_limit": {
      "type": "number",
      "default": 10,
      "description": "Maximum memories to inject per turn"
    },
    "profile_interval": {
      "type": "number",
      "default": 50,
      "description": "Inject full profile summary every N turns"
    },
    "containers": {
      "type": "string",
      "default": "",
      "description": "Comma-separated container filter (empty = all)"
    }
  },
  "hooks": {
    "onTurnEnd": "dist/capture.js",
    "onTurnStart": "dist/recall.js"
  },
  "commands": [
    {
      "name": "remember",
      "description": "Manually store a fact in memory",
      "usage": "/remember <text>"
    },
    {
      "name": "recall",
      "description": "Search memory for relevant facts",
      "usage": "/recall <query>"
    },
    {
      "name": "profile",
      "description": "Show current user profile built from memories",
      "usage": "/profile"
    },
    {
      "name": "forget",
      "description": "Remove facts matching a query from memory",
      "usage": "/forget <query>"
    },
    {
      "name": "memory-stats",
      "description": "Show memory store statistics",
      "usage": "/memory-stats"
    }
  ]
}
```

### `plugin/src/index.ts`

```typescript
import type { Plugin, PluginContext, PluginConfig } from '@openclaw/sdk';
import { setupCapture } from './capture';
import { setupRecall } from './recall';
import { registerTools } from './tools';

interface ClawMemoryConfig extends PluginConfig {
  server_url: string;
  auto_capture: boolean;
  auto_recall: boolean;
  recall_limit: number;
  profile_interval: number;
  containers: string;
}

export default function clawmemoryPlugin(ctx: PluginContext<ClawMemoryConfig>): Plugin {
  const config = ctx.config;
  const serverUrl = config.server_url || 'http://127.0.0.1:7437';

  return {
    name: 'clawmemory',
    
    async onLoad() {
      // Health check: verify server is reachable
      const resp = await fetch(`${serverUrl}/health`);
      if (!resp.ok) throw new Error(`ClawMemory server not reachable at ${serverUrl}`);
      ctx.log.info('ClawMemory plugin loaded, server healthy');
    },

    hooks: {
      onTurnEnd: config.auto_capture ? setupCapture(serverUrl, config) : undefined,
      onTurnStart: config.auto_recall ? setupRecall(serverUrl, config) : undefined,
    },

    commands: registerTools(serverUrl, config),
  };
}
```

### `plugin/src/capture.ts`

```typescript
import type { TurnEndHook, Turn, ClawMemoryConfig } from './types';

/**
 * Auto-capture hook: after every conversation turn, send the last 2 turns
 * to ClawMemory for fact extraction.
 * 
 * Behaviour:
 * 1. Collect last 2 turns from the conversation
 * 2. POST to /api/v1/ingest with session_id + turns
 * 3. Log extracted facts count (no user-visible output)
 * 4. Runs async — does NOT block the conversation
 */
export function setupCapture(serverUrl: string, config: ClawMemoryConfig): TurnEndHook {
  let turnCount = 0;

  return async (turns: Turn[], sessionId: string) => {
    turnCount++;
    
    // Take last 2 turns (current user message + assistant response)
    const recentTurns = turns.slice(-2);
    if (recentTurns.length === 0) return;

    // Fire and forget — don't block conversation on extraction
    try {
      const resp = await fetch(`${serverUrl}/api/v1/ingest`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          session_id: sessionId,
          turns: recentTurns.map(t => ({
            role: t.role,
            content: t.content,
          })),
        }),
      });

      if (resp.ok) {
        const data = await resp.json();
        if (data.extracted_facts?.length > 0) {
          console.log(`[clawmemory] Extracted ${data.extracted_facts.length} facts`);
        }
        if (data.contradictions?.length > 0) {
          console.log(`[clawmemory] Resolved ${data.contradictions.length} contradictions`);
        }
      }
    } catch (err) {
      console.error('[clawmemory] Capture error:', err);
    }
  };
}
```

### `plugin/src/recall.ts`

```typescript
import type { TurnStartHook, ClawMemoryConfig } from './types';

/**
 * Auto-recall hook: before every turn, query ClawMemory for relevant context
 * and inject it into the system prompt as a [Memory context] block.
 *
 * Behaviour:
 * 1. Take the current user message as the search query
 * 2. POST to /api/v1/recall with query + config limits
 * 3. Format results as a [Memory context] block
 * 4. Inject into system prompt (prepend to existing)
 * 5. Every profile_interval turns, also inject full profile summary
 */
export function setupRecall(serverUrl: string, config: ClawMemoryConfig): TurnStartHook {
  let turnCount = 0;

  return async (userMessage: string, systemPrompt: string): Promise<string> => {
    turnCount++;

    try {
      const includeProfile = turnCount % config.profile_interval === 0;
      
      const resp = await fetch(`${serverUrl}/api/v1/recall`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          query: userMessage,
          limit: config.recall_limit,
          container: config.containers || '',
          threshold: 0.1,
          include_profile: includeProfile,
        }),
      });

      if (!resp.ok) return systemPrompt; // fail open

      const data = await resp.json();
      
      if (data.results?.length === 0 && !data.profile_summary) {
        return systemPrompt;
      }

      // Build memory context block
      let memoryBlock = '[Memory context]\n';
      
      if (data.profile_summary) {
        memoryBlock += `User profile: ${data.profile_summary}\n\n`;
      }

      if (data.results?.length > 0) {
        memoryBlock += 'Relevant memories:\n';
        for (const r of data.results) {
          memoryBlock += `- ${r.content} (${r.category}, score: ${r.score.toFixed(2)})\n`;
        }
      }

      memoryBlock += '[/Memory context]\n\n';

      return memoryBlock + systemPrompt;
    } catch (err) {
      console.error('[clawmemory] Recall error:', err);
      return systemPrompt; // fail open — never block conversation
    }
  };
}
```

### `plugin/src/tools.ts`

```typescript
import type { Command, ClawMemoryConfig } from './types';

/**
 * Register slash commands: /remember, /recall, /profile, /forget, /memory-stats
 */
export function registerTools(serverUrl: string, config: ClawMemoryConfig): Command[] {
  return [
    {
      name: 'remember',
      description: 'Store a fact in long-term memory',
      execute: async (args: string) => {
        const resp = await fetch(`${serverUrl}/api/v1/remember`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            content: args,
            category: 'general',      // auto-classified by server
            container: 'general',
            importance: 0.7,
          }),
        });
        const data = await resp.json();
        return `✅ Stored: "${data.content}" (id: ${data.id})`;
      },
    },
    {
      name: 'recall',
      description: 'Search memory for relevant facts',
      execute: async (args: string) => {
        const resp = await fetch(`${serverUrl}/api/v1/recall`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            query: args,
            limit: 10,
            include_profile: false,
          }),
        });
        const data = await resp.json();
        if (data.results?.length === 0) return '🔍 No matching memories found.';
        
        let output = `🧠 Found ${data.results.length} memories (${data.search_latency_ms}ms):\n`;
        for (const r of data.results) {
          output += `• [${r.score.toFixed(2)}] ${r.content} (${r.category}/${r.container})\n`;
        }
        return output;
      },
    },
    {
      name: 'profile',
      description: 'Show user profile built from memories',
      execute: async () => {
        const resp = await fetch(`${serverUrl}/api/v1/profile`);
        const data = await resp.json();
        
        let output = '👤 User Profile:\n';
        for (const [key, value] of Object.entries(data.entries || {})) {
          output += `  ${key}: ${value}\n`;
        }
        if (data.summary) {
          output += `\n📝 Summary: ${data.summary}`;
        }
        return output;
      },
    },
    {
      name: 'forget',
      description: 'Remove facts matching a query',
      execute: async (args: string) => {
        const resp = await fetch(`${serverUrl}/api/v1/forget`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ query: args, max_delete: 5 }),
        });
        const data = await resp.json();
        return `🗑️ Deleted ${data.deleted_count} memories.`;
      },
    },
    {
      name: 'memory-stats',
      description: 'Show memory store statistics',
      execute: async () => {
        const resp = await fetch(`${serverUrl}/api/v1/stats`);
        const data = await resp.json();
        
        return `📊 Memory Stats:
  Active facts: ${data.active_facts}
  Superseded: ${data.superseded_facts}
  Total turns: ${data.total_turns}
  Profile entries: ${data.profile_entries}
  DB size: ${(data.db_size_bytes / 1024 / 1024).toFixed(1)} MB
  Last sync: ${data.last_sync_at ? new Date(data.last_sync_at).toISOString() : 'never'}`;
      },
    },
  ];
}
```

### `plugin/package.json`

```json
{
  "name": "@clawinfra/clawmemory-plugin",
  "version": "0.1.0",
  "description": "OpenClaw plugin for ClawMemory — sovereign agent memory engine",
  "main": "dist/index.js",
  "types": "dist/index.d.ts",
  "scripts": {
    "build": "tsc",
    "dev": "tsc --watch",
    "lint": "eslint src/",
    "test": "vitest"
  },
  "dependencies": {},
  "devDependencies": {
    "@openclaw/sdk": "^0.1.0",
    "typescript": "^5.4.0",
    "vitest": "^1.4.0",
    "eslint": "^9.0.0"
  }
}
```

---

## 5. Ollama Embedding Integration

### Endpoint Details

- **URL:** `http://10.0.0.44:11434/api/embeddings`
- **Model:** `qwen2.5:7b`
- **Dimension:** 3584 (float32)
- **Method:** POST
- **Latency:** ~200-500ms per embedding (GPU server RTX 3090)

### Request Format

```
POST http://10.0.0.44:11434/api/embeddings
Content-Type: application/json

{
  "model": "qwen2.5:7b",
  "prompt": "User's timezone is Australia/Sydney"
}
```

### Response Format

```json
{
  "embedding": [0.123, -0.456, 0.789, ...]  // float64 array, length 3584
}
```

### Go Implementation (`internal/embed/client.go`)

```go
type embeddingRequest struct {
    Model  string `json:"model"`
    Prompt string `json:"prompt"`
}

type embeddingResponse struct {
    Embedding []float64 `json:"embedding"`
}

func (c *Client) Embed(ctx context.Context, text string) ([]float32, error) {
    reqBody := embeddingRequest{
        Model:  c.model,
        Prompt: text,
    }
    
    body, err := json.Marshal(reqBody)
    if err != nil {
        return nil, fmt.Errorf("marshal embedding request: %w", err)
    }
    
    req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/embeddings", bytes.NewReader(body))
    if err != nil {
        return nil, fmt.Errorf("create embedding request: %w", err)
    }
    req.Header.Set("Content-Type", "application/json")
    
    resp, err := c.client.Do(req)
    if err != nil {
        return nil, fmt.Errorf("embedding request: %w", err)
    }
    defer resp.Body.Close()
    
    if resp.StatusCode != http.StatusOK {
        respBody, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("embedding request failed (status %d): %s", resp.StatusCode, respBody)
    }
    
    var embResp embeddingResponse
    if err := json.NewDecoder(resp.Body).Decode(&embResp); err != nil {
        return nil, fmt.Errorf("decode embedding response: %w", err)
    }
    
    if len(embResp.Embedding) != c.dim {
        return nil, fmt.Errorf("unexpected embedding dimension: got %d, want %d", len(embResp.Embedding), c.dim)
    }
    
    // Convert float64 → float32 for storage efficiency
    result := make([]float32, len(embResp.Embedding))
    for i, v := range embResp.Embedding {
        result[i] = float32(v)
    }
    
    return result, nil
}
```

### Storage Format

Embeddings stored as BLOB in SQLite: little-endian float32 array, 3584 × 4 = **14,336 bytes per fact**.

At 10,000 facts: ~137 MB of embedding data. At 100,000 facts: ~1.37 GB. This is manageable for SQLite.

### Graceful Degradation

If Ollama is unreachable (GPU server down):
1. Facts are still stored without embeddings (embedding = NULL)
2. Search falls back to BM25-only (keyword search still works)
3. A background goroutine retries embedding generation for NULL-embedding facts every 5 minutes
4. Log a warning: `[clawmemory] Ollama unreachable — vector search degraded, BM25-only mode`

---

## 6. Turso Sync Strategy

### Architecture

Uses `go-libsql`'s **embedded replica** pattern:
- Local SQLite file is the primary read/write source (fast, zero latency)
- Turso cloud is the remote primary for cross-device sync and backup
- Writes go to local SQLite first, then sync to Turso in background

### Sync Implementation

```go
// In NewTursoSync:
connector, err := libsql.NewEmbeddedReplicaConnectorWithAutoSync(
    dbPath,      // local: ~/.clawmemory/memory.db
    remoteURL,   // remote: libsql://agentmemory-bowen31337.aws-ap-northeast-1.turso.io
    authToken,   // from env TURSO_AUTH_TOKEN
    syncInterval, // 5 minutes default
)
db := sql.OpenDB(connector)
```

### When Sync Happens

| Trigger | Mechanism |
|---------|-----------|
| Periodic (every 5 min) | `go-libsql` auto-sync via `EmbeddedReplicaConnectorWithAutoSync` |
| Manual (`POST /api/v1/sync`) | Calls `connector.Sync()` directly |
| On graceful shutdown | Final sync before exit |
| After batch ingest (>10 facts) | Immediate sync after large ingestion |

### Conflict Resolution

Since we use embedded replicas, Turso handles conflict resolution:

1. **Single-writer model:** Only one ClawMemory instance writes at a time (personal agent = single user). No concurrent writes in practice.
2. **If multi-device:** Turso's embedded replica model uses the remote as source of truth. On sync:
   - Local changes push to remote
   - Remote changes pull to local
   - If same row modified on both sides: remote wins (last-write-wins by Turso)
3. **Fact versioning:** Our superseded_by chain provides logical conflict resolution independent of storage-level conflicts. Even if Turso LWW picks the "wrong" write, the contradiction resolver will fix it on next fact extraction.

### Fallback: Turso Unavailable

If Turso is unreachable:
1. All reads/writes continue on local SQLite (zero degradation for local use)
2. Background sync retries every interval (5 min) with exponential backoff up to 30 min
3. Log: `[clawmemory] Turso sync failed — operating in local-only mode`
4. On reconnect, pending local changes sync automatically

### Configuration

```bash
# Environment variables
TURSO_AUTH_TOKEN=<token>  # Required for cloud sync
TURSO_URL=libsql://agentmemory-bowen31337.aws-ap-northeast-1.turso.io  # Or in config.json

# Config file (~/.clawmemory/config.json)
{
  "turso": {
    "url": "libsql://agentmemory-bowen31337.aws-ap-northeast-1.turso.io",
    "auth_token": "",  # prefer env var
    "sync_interval": "5m"
  }
}
```

---

## 7. Benchmark Harness Design

### Overview

Three benchmark suites, inspired by the benchmarks Supermemory tops. We create our own test data (not downloading full datasets — they're conversation-specific).

### Benchmark Data Format

Each benchmark uses JSONL files in `bench/testdata/`:

#### `longmemeval_100.jsonl` — 100 questions across 5 abilities

```jsonl
{"id":"lme_001","ability":"extraction","setup_turns":[{"role":"user","content":"I work at Anthropic as a research engineer"},{"role":"assistant","content":"That's great! Research engineering at Anthropic must be exciting."}],"question":"Where does the user work?","expected":"Anthropic","expected_keywords":["Anthropic"],"difficulty":"easy"}
{"id":"lme_002","ability":"multi_session","setup_turns":[...],"question":"What project was the user working on when they mentioned switching to Melbourne?","expected":"ClawChain","expected_keywords":["ClawChain","blockchain"],"difficulty":"hard"}
{"id":"lme_003","ability":"knowledge_update","setup_turns":[...],"question":"What is the user's current city?","expected":"Melbourne","expected_keywords":["Melbourne"],"contradicts":"lme_010","difficulty":"medium"}
{"id":"lme_004","ability":"temporal","setup_turns":[...],"question":"What did we discuss about the tax audit 3 sessions ago?","expected":"...","expected_keywords":["tax","audit"],"difficulty":"hard"}
{"id":"lme_005","ability":"abstention","setup_turns":[...],"question":"What is the user's mother's maiden name?","expected":"__UNKNOWN__","expected_keywords":[],"difficulty":"medium"}
```

Distribution: 20 extraction + 20 multi-session + 20 knowledge-update + 20 temporal + 20 abstention = **100 questions**.

#### `locomo_50.jsonl` — 50 multi-turn conversations with synthesis questions

```jsonl
{"id":"loc_001","conversation":[{"role":"user","content":"..."},{"role":"assistant","content":"..."},...50 turns...],"questions":[{"q":"What was the user's main concern about the deployment?","expected":"Latency spikes during peak hours","expected_keywords":["latency","peak"]}],"metadata":{"turns":50,"topics":["deployment","infrastructure"]}}
```

50 conversations, each 30-80 turns, with 1-3 synthesis questions per conversation.

#### `convomem_30.jsonl` — 30 contradiction scenarios

```jsonl
{"id":"con_001","initial_facts":[{"role":"user","content":"My favorite color is blue"}],"contradicting_facts":[{"role":"user","content":"Actually, I've started preferring green over blue"}],"question":"What is the user's favorite color?","expected":"green","expected_keywords":["green"],"should_not_contain":["blue"]}
```

30 scenarios testing: preference changes, location moves, job changes, project pivots, relationship updates.

### Runner Architecture (`bench/runner.go`)

```go
package bench

type BenchmarkRunner struct {
    serverURL string
    client    *http.Client
}

// RunAll executes all benchmark suites and produces a combined report.
func (r *BenchmarkRunner) RunAll(ctx context.Context) (*Report, error)

// RunLongMemEval runs the LongMemEval-inspired benchmark.
// For each question:
//   1. Ingest setup_turns via /api/v1/ingest
//   2. Wait for extraction (poll /api/v1/stats until unprocessed_turns = 0)
//   3. Query via /api/v1/recall with the question
//   4. Score: check if top-1 result contains expected_keywords
//   5. For abstention: check that system returns no confident results
func (r *BenchmarkRunner) RunLongMemEval(ctx context.Context, dataPath string) (*SuiteResult, error)

// RunLoCoMo runs the LoCoMo-inspired benchmark.
// For each conversation:
//   1. Ingest all turns sequentially via /api/v1/ingest
//   2. For each question: query via /api/v1/recall
//   3. Score: recall@1, recall@5, MRR
func (r *BenchmarkRunner) RunLoCoMo(ctx context.Context, dataPath string) (*SuiteResult, error)

// RunConvoMem runs the ConvoMem-inspired contradiction benchmark.
// For each scenario:
//   1. Ingest initial_facts
//   2. Ingest contradicting_facts
//   3. Query with the question
//   4. Score: top result should match expected, should_not_contain must be absent
func (r *BenchmarkRunner) RunConvoMem(ctx context.Context, dataPath string) (*SuiteResult, error)
```

### Report Card (`bench/report.go`)

```go
type Report struct {
    Timestamp     time.Time           `json:"timestamp"`
    Version       string              `json:"version"`
    Suites        map[string]*SuiteResult `json:"suites"`
    Aggregate     AggregateMetrics    `json:"aggregate"`
    LatencyStats  LatencyStats        `json:"latency"`
    SystemInfo    SystemInfo          `json:"system_info"`
}

type SuiteResult struct {
    Name           string  `json:"name"`
    TotalQuestions int     `json:"total_questions"`
    Correct        int     `json:"correct"`
    RecallAt1      float64 `json:"recall_at_1"`
    RecallAt5      float64 `json:"recall_at_5"`
    MRR            float64 `json:"mrr"`
    AbstentionF1   float64 `json:"abstention_f1,omitempty"`  // LongMemEval only
    ContradictionAcc float64 `json:"contradiction_acc,omitempty"` // ConvoMem only
}

type AggregateMetrics struct {
    OverallRecallAt1     float64 `json:"overall_recall_at_1"`
    OverallRecallAt5     float64 `json:"overall_recall_at_5"`
    OverallMRR           float64 `json:"overall_mrr"`
    ContradictionAccuracy float64 `json:"contradiction_accuracy"`
    AbstentionF1         float64 `json:"abstention_f1"`
}

type LatencyStats struct {
    IngestP50Ms  float64 `json:"ingest_p50_ms"`
    IngestP99Ms  float64 `json:"ingest_p99_ms"`
    RecallP50Ms  float64 `json:"recall_p50_ms"`
    RecallP99Ms  float64 `json:"recall_p99_ms"`
}

// GenerateMarkdown produces a markdown report card for README/CI.
func (r *Report) GenerateMarkdown() string
```

### Report Card Output (Markdown)

```markdown
## ClawMemory Benchmark Report — v0.1.0

| Suite | Recall@1 | Recall@5 | MRR | Special |
|-------|----------|----------|-----|---------|
| LongMemEval (100q) | 82.0% | 94.0% | 0.87 | Abstention F1: 0.91 |
| LoCoMo (50 convos) | 78.0% | 91.0% | 0.84 | — |
| ConvoMem (30 scenarios) | 90.0% | 96.7% | 0.93 | Contradiction Acc: 93.3% |
| **Aggregate** | **83.3%** | **93.9%** | **0.88** | — |

### Latency
| Operation | p50 | p99 |
|-----------|-----|-----|
| Ingest (extract + store) | 450ms | 1200ms |
| Recall (search + rank) | 35ms | 120ms |

### System
- Go 1.22+, SQLite 3.45+, Ollama qwen2.5:7b (3584-dim)
- GPU: RTX 3090 (embedding) | CPU: Intel i7 (search)
```

### Running Benchmarks

```bash
# From repo root
make bench

# Or directly
go run ./bench/... --server http://127.0.0.1:7437 --data bench/testdata/ --output bench/report.json

# Generate markdown report
go run ./bench/... --format markdown > docs/BENCHMARK.md
```

---

## 8. CI/CD — GitHub Actions

### `.github/workflows/ci.yml`

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

env:
  GO_VERSION: "1.22"

jobs:
  lint:
    name: Lint
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: ${{ env.GO_VERSION }}
      - name: golangci-lint
        uses: golangci/golangci-lint-action@v4
        with:
          version: latest
          args: --timeout=5m

  test:
    name: Test
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: ${{ env.GO_VERSION }}
      
      - name: Run tests with coverage
        run: |
          go test -v -race -coverprofile=coverage.out -covermode=atomic ./...
          go tool cover -func=coverage.out | tail -1
      
      - name: Check coverage threshold
        run: |
          COVERAGE=$(go tool cover -func=coverage.out | tail -1 | awk '{print $3}' | tr -d '%')
          echo "Total coverage: ${COVERAGE}%"
          if (( $(echo "$COVERAGE < 90" | bc -l) )); then
            echo "::error::Coverage ${COVERAGE}% is below 90% threshold"
            exit 1
          fi

      - name: Upload coverage
        uses: codecov/codecov-action@v4
        with:
          file: ./coverage.out

  build:
    name: Build
    runs-on: ubuntu-latest
    needs: [lint, test]
    strategy:
      matrix:
        goos: [linux, darwin]
        goarch: [amd64, arm64]
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: ${{ env.GO_VERSION }}

      - name: Build binary
        env:
          GOOS: ${{ matrix.goos }}
          GOARCH: ${{ matrix.goarch }}
          CGO_ENABLED: 1
        run: |
          go build -ldflags="-s -w -X main.version=$(git describe --tags --always)" \
            -o clawmemory-${{ matrix.goos }}-${{ matrix.goarch }} \
            ./cmd/clawmemory/

      - name: Upload artifact
        uses: actions/upload-artifact@v4
        with:
          name: clawmemory-${{ matrix.goos }}-${{ matrix.goarch }}
          path: clawmemory-${{ matrix.goos }}-${{ matrix.goarch }}

  plugin:
    name: Plugin (TypeScript)
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: plugin
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: "22"
      - run: npm ci
      - run: npm run lint
      - run: npm run build
      - run: npm test
```

### `.github/workflows/bench.yml`

```yaml
name: Benchmark

on:
  schedule:
    - cron: '0 3 * * 1'  # Every Monday 3am UTC
  workflow_dispatch:

jobs:
  benchmark:
    name: Run Benchmarks
    runs-on: self-hosted  # Needs Ollama GPU access
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"

      - name: Build and start server
        run: |
          go build -o clawmemory ./cmd/clawmemory/
          ./clawmemory serve --config bench/testconfig.json &
          sleep 5

      - name: Run benchmarks
        run: |
          go run ./bench/... \
            --server http://127.0.0.1:7437 \
            --data bench/testdata/ \
            --output bench/report.json \
            --format markdown > bench/REPORT.md

      - name: Upload report
        uses: actions/upload-artifact@v4
        with:
          name: benchmark-report
          path: |
            bench/report.json
            bench/REPORT.md

      - name: Comment on PR (if applicable)
        if: github.event_name == 'pull_request'
        uses: actions/github-script@v7
        with:
          script: |
            const fs = require('fs');
            const report = fs.readFileSync('bench/REPORT.md', 'utf8');
            github.rest.issues.createComment({
              issue_number: context.issue.number,
              owner: context.repo.owner,
              repo: context.repo.repo,
              body: `## Benchmark Report\n\n${report}`
            });

      - name: Cleanup
        if: always()
        run: kill %1 || true
```

---

## 9. Exact Test Plan

**Coverage target: ≥90% on all packages.**

### `internal/config/config_test.go`

| Test | Description |
|------|-------------|
| `TestDefault` | Default config has sensible values (port 7437, correct Ollama URL, etc.) |
| `TestLoadFromFile` | Reads a JSON config file and parses all fields correctly |
| `TestLoadMissingFile` | Returns default config when file doesn't exist |
| `TestEnvOverrides` | Environment variables override file config (TURSO_AUTH_TOKEN, etc.) |
| `TestValidation` | Invalid port / missing required fields return error |

### `internal/extractor/extractor_test.go`

| Test | Description |
|------|-------------|
| `TestExtract_BasicFacts` | Mock LLM returns 3 facts from simple conversation → parsed correctly |
| `TestExtract_NoFacts` | Conversation with no extractable info → empty slice, no error |
| `TestExtract_MaxFacts` | LLM returns >5 facts → truncated to 5 |
| `TestExtract_InvalidJSON` | LLM returns malformed JSON → error handled gracefully, returns empty |
| `TestExtract_Categories` | Extracted facts have valid categories (person/project/preference/event/technical) |
| `TestExtract_Importance` | Importance values are within [0.0, 1.0] |
| `TestBuildExtractionPrompt` | Prompt formatting includes all turns with correct role labels |
| `TestExtract_HTTPError` | LLM endpoint returns 500 → error propagated with context |
| `TestExtract_Timeout` | Context timeout → extraction cancelled, error returned |

### `internal/store/store_test.go`

| Test | Description |
|------|-------------|
| `TestNewSQLiteStore` | Opens in-memory DB, runs migrations, tables exist |
| `TestInsertFact` | Insert a fact → GetFact returns it with all fields |
| `TestInsertFact_DuplicateID` | Inserting same ID twice → error |
| `TestUpdateFact` | Update importance/confidence → reflected in GetFact |
| `TestSoftDeleteFact` | Soft-delete → fact.deleted=true, not in ListFacts default |
| `TestSoftDeleteFact_IncludeDeleted` | ListFacts with IncludeDeleted=true → shows deleted facts |
| `TestSupersedeF act` | Supersede old with new → old.superseded_by set, old.confidence lowered |
| `TestListFacts_FilterContainer` | Filter by container → only matching facts returned |
| `TestListFacts_FilterCategory` | Filter by category → only matching facts returned |
| `TestListFacts_Pagination` | Limit + Offset work correctly |
| `TestInsertTurn` | Insert turn → GetUnprocessedTurns returns it |
| `TestMarkTurnProcessed` | Mark processed → no longer in GetUnprocessedTurns |
| `TestSetProfile` | Set key-value → GetProfile returns it |
| `TestSetProfile_Update` | Set same key twice → value updated, updated_at changed |
| `TestListProfile` | Multiple entries → all returned |
| `TestDeleteProfile` | Delete key → GetProfile returns nil |
| `TestSearchFTS` | Insert 10 facts → FTS5 search returns relevant ones ranked by BM25 |
| `TestSearchFTS_NoResults` | Query with no matches → empty slice |
| `TestSearchFTS_Phrase` | Phrase matching works ("exact phrase") |
| `TestSearchVector` | Insert facts with embeddings → cosine search returns top-k |
| `TestSearchVector_Threshold` | Low-similarity facts filtered by threshold |
| `TestSearchVector_NullEmbedding` | Facts without embeddings excluded from vector search |
| `TestEncodeDecodeEmbedding` | Round-trip encode/decode preserves float32 values |
| `TestCosineSimilarity` | Known vectors → expected similarity value |
| `TestCosineSimilarity_Identical` | Same vector → similarity ≈ 1.0 |
| `TestCosineSimilarity_Orthogonal` | Orthogonal vectors → similarity ≈ 0.0 |
| `TestListDecayable` | Facts older than threshold with low importance → returned |
| `TestPruneFacts` | Prune by IDs → soft-deleted, count correct |
| `TestStats` | Insert various data → Stats returns correct counts |
| `TestMigrations` | Run migrations twice → idempotent, no error |
| `TestClose` | Close store → subsequent operations return error |

### `internal/search/search_test.go`

| Test | Description |
|------|-------------|
| `TestHybridSearch` | BM25 + vector results fused with RRF → correct ranking |
| `TestHybridSearch_BM25Only` | Ollama down → graceful fallback to BM25-only |
| `TestHybridSearch_VectorOnly` | Query matches semantically but not lexically → found via vector |
| `TestHybridSearch_ContainerFilter` | Filter by container → only matching results |
| `TestHybridSearch_Threshold` | Low-score results filtered out |
| `TestHybridSearch_EmptyStore` | No facts → empty results, no error |
| `TestRRF` | Reciprocal rank fusion formula correct for known inputs |
| `TestBM25Search_Ranking` | More relevant docs rank higher |
| `TestVectorSearch_Ranking` | Semantically closer docs rank higher |

### `internal/resolver/resolver_test.go`

| Test | Description |
|------|-------------|
| `TestCheck_NoContradiction` | New fact unrelated to existing → empty contradictions |
| `TestCheck_Contradiction` | New fact contradicts existing (same topic, different value) → detected |
| `TestCheck_SameFactDuplicate` | Exact same content → no contradiction (duplicate, not conflict) |
| `TestResolve_Supersede` | Supersede resolution → old fact superseded_by = new, confidence lowered |
| `TestResolve_Coexist` | Coexist resolution → both facts remain unchanged |
| `TestCheck_MultipleContradictions` | New fact contradicts 2 existing facts → both detected |
| `TestCheck_SupersededFactIgnored` | Already-superseded facts not checked → no false contradictions |

### `internal/profile/profile_test.go`

| Test | Description |
|------|-------------|
| `TestBuild_FromFacts` | Person + preference facts → profile entries extracted |
| `TestBuild_EmptyStore` | No facts → empty profile, no error |
| `TestGet` | After Build → Get returns same profile |
| `TestUpdate_IncrementalMerge` | New facts → profile updated without rebuilding |
| `TestUpdate_Overwrite` | Updated fact → profile entry updated |
| `TestSummarize` | Profile entries → natural language summary via LLM |

### `internal/decay/decay_test.go`

| Test | Description |
|------|-------------|
| `TestDecayedImportance` | 30 days at half-life 30 → importance halved |
| `TestDecayedImportance_Zero` | Age 0 → original importance unchanged |
| `TestDecayedImportance_VeryOld` | 365 days → importance very low |
| `TestRunOnce` | Facts below threshold → pruned, count returned |
| `TestRunOnce_TTLExpiry` | Facts past expires_at → pruned |
| `TestRunOnce_HighImportance` | High-importance facts survive longer |
| `TestRunOnce_NothingToPrune` | All facts above threshold → 0 pruned |
| `TestStartStop` | Start → runs periodically → Stop → no more runs |

### `internal/server/server_test.go`

| Test | Description |
|------|-------------|
| `TestHealth` | GET /health → 200 with status ok |
| `TestIngest_Success` | POST /api/v1/ingest with valid turns → 200, facts extracted |
| `TestIngest_EmptyTurns` | POST with empty turns → 400 error |
| `TestIngest_InvalidJSON` | POST with malformed JSON → 400 |
| `TestRemember_Success` | POST /api/v1/remember → 201, fact stored |
| `TestRemember_MissingContent` | POST without content → 400 |
| `TestRecall_Success` | POST /api/v1/recall → 200 with results |
| `TestRecall_EmptyQuery` | POST with empty query → 400 |
| `TestProfile_Success` | GET /api/v1/profile → 200 with profile |
| `TestForget_Success` | POST /api/v1/forget → 200, facts deleted |
| `TestFacts_List` | GET /api/v1/facts → 200 with paginated list |
| `TestFacts_Filter` | GET /api/v1/facts?container=work → filtered results |
| `TestFactByID_Found` | GET /api/v1/facts/{id} → 200 with fact |
| `TestFactByID_NotFound` | GET /api/v1/facts/{invalid} → 404 |
| `TestStats` | GET /api/v1/stats → 200 with statistics |
| `TestSync` | POST /api/v1/sync → 200 |
| `TestCORS` | OPTIONS request → correct CORS headers |
| `TestContentType` | All responses have Content-Type: application/json |

### `internal/embed/client_test.go`

| Test | Description |
|------|-------------|
| `TestEmbed_Success` | Mock Ollama returns 3584-dim vector → parsed correctly |
| `TestEmbed_WrongDimension` | Mock returns wrong dimension → error |
| `TestEmbed_ServerError` | Mock returns 500 → error with context |
| `TestEmbed_Timeout` | Context cancelled → error |
| `TestEmbedBatch` | 5 texts → 5 embeddings returned |
| `TestEmbedBatch_Empty` | Empty input → empty output |
| `TestDimension` | Returns configured dimension |

### `bench/bench_test.go`

| Test | Description |
|------|-------------|
| `TestLongMemEval_Parse` | Parse JSONL test data → correct structure |
| `TestLoCoMo_Parse` | Parse JSONL test data → correct structure |
| `TestConvoMem_Parse` | Parse JSONL test data → correct structure |
| `TestReportGenerate` | Generate markdown report → valid format |
| `TestScoring_RecallAt1` | Known results → correct recall@1 |
| `TestScoring_MRR` | Known ranks → correct MRR |
| `TestScoring_AbstentionF1` | Known abstention results → correct F1 |

### Test Infrastructure

```go
// testutil package provides shared test helpers

// NewTestStore creates an in-memory SQLite store for testing.
func NewTestStore(t *testing.T) store.Store

// NewMockEmbedder returns a mock that produces deterministic embeddings.
func NewMockEmbedder(t *testing.T) *embed.Client

// NewMockExtractor returns a mock that returns predefined facts.
func NewMockExtractor(t *testing.T, facts []extractor.Fact) *extractor.Extractor

// SeedFacts inserts N test facts into a store.
func SeedFacts(t *testing.T, s store.Store, n int)
```

### Running Tests

```bash
# All tests with coverage
go test -v -race -coverprofile=coverage.out ./...

# Single package
go test -v -race ./internal/store/...

# With coverage report
go tool cover -html=coverage.out -o coverage.html
```

---

## Makefile

```makefile
.PHONY: build test lint bench clean serve

VERSION := $(shell git describe --tags --always --dirty)
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"

build:
	CGO_ENABLED=1 go build $(LDFLAGS) -o bin/clawmemory ./cmd/clawmemory/

test:
	go test -v -race -coverprofile=coverage.out -covermode=atomic ./...
	@echo "Coverage:"
	@go tool cover -func=coverage.out | tail -1

lint:
	golangci-lint run --timeout=5m

bench:
	go run ./bench/... --server http://127.0.0.1:7437 --data bench/testdata/ --output bench/report.json

serve:
	go run ./cmd/clawmemory/ serve

clean:
	rm -rf bin/ coverage.out

plugin-build:
	cd plugin && npm ci && npm run build

plugin-test:
	cd plugin && npm test

all: lint test build plugin-build plugin-test
```

---

## Build Phases (Implementation Order)

### Phase 1 — Core Engine (Builder Iteration 1)
1. `go.mod` with dependencies: `go-libsql`, `google/uuid` (for UUIDv7)
2. `internal/config/` — config loading with defaults + env overrides
3. `internal/embed/client.go` — Ollama HTTP client
4. `internal/store/` — SQLite store with migrations, FTS5, CRUD, vector search
5. `internal/search/` — BM25 + vector + RRF hybrid
6. `internal/server/` — HTTP server with /health, /recall, /remember, /stats, /facts
7. `cmd/clawmemory/main.go` — serve subcommand
8. All corresponding `*_test.go` files
9. ≥90% coverage on store + search + embed

### Phase 2 — Intelligence (Builder Iteration 2)
1. `internal/extractor/` — GLM-4.7 fact extraction
2. `internal/resolver/` — Contradiction detection
3. `internal/profile/` — Profile builder
4. `internal/decay/` — Importance decay + TTL
5. `internal/store/turso.go` — Turso sync
6. Wire extraction + resolution into `/api/v1/ingest` endpoint
7. All corresponding `*_test.go` files
8. ≥90% coverage on extractor + resolver + profile + decay

### Phase 3 — Plugin + Benchmarks (Builder Iteration 3)
1. `plugin/` — OpenClaw TypeScript plugin
2. `bench/testdata/` — Generate benchmark datasets (100 + 50 + 30)
3. `bench/` — Runner, scorers, report generator
4. `docs/` — ARCHITECTURE.md, API.md, BENCHMARK.md, PLUGIN.md
5. `AGENTS.md` — repo navigation TOC
6. `README.md` with benchmark results
7. CI/CD workflows
8. v0.1.0 tag

---

## Tech Stack (Final)

| Component | Choice | Import Path / URL |
|-----------|--------|-------------------|
| Core engine | Go 1.22+ | — |
| SQLite driver | go-libsql (CGO, embedded replicas) | `github.com/tursodatabase/go-libsql` |
| Cloud sync | Turso | `libsql://agentmemory-bowen31337.aws-ap-northeast-1.turso.io` |
| Embeddings | Ollama qwen2.5:7b | `http://10.0.0.44:11434/api/embeddings` |
| Embedding dim | 3584 (float32) | — |
| LLM extraction | GLM-4.7 via proxy | `anthropic-proxy-6` (OpenAI-compatible) |
| HTTP server | net/http (stdlib) | — |
| UUID | UUIDv7 (time-sortable) | `github.com/google/uuid` |
| JSON | encoding/json (stdlib) | — |
| Plugin | TypeScript | `@clawinfra/clawmemory-plugin` |
| Lint | golangci-lint | — |
| CI | GitHub Actions | — |

---

## Privacy Model

- All data stored locally first (SQLite file at `~/.clawmemory/memory.db`)
- Turso sync is encrypted in transit (libsql TLS)
- Turso is the only external service — and we control the database
- Embeddings computed on our GPU server (10.0.0.44) — never leave our infra
- LLM extraction calls go to GLM-4.7 proxy — only extracted text (2-3 sentences per turn), not raw conversation
- No telemetry, no analytics, no third-party SDKs
- Soft-delete, not hard-delete — full audit trail

---

## Default Port

**7437** — `M-E-M-S` on a phone keypad (for "MEMS" / memory system).

---

## Next Step

Builder implements Phase 1 using this spec exactly as written.
