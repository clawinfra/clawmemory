package store

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/tursodatabase/go-libsql" // CGO-based libsql driver
)

// SQLiteStore implements Store using local SQLite + libsql driver.
type SQLiteStore struct {
	db   *sql.DB
	path string
}

// NewSQLiteStore opens (or creates) the SQLite database at dbPath.
// Runs migrations on first open. Enables WAL mode and FTS5.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	db, err := sql.Open("libsql", "file:"+dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Enable WAL mode for better concurrency
	// Note: PRAGMA journal_mode=WAL returns a row, use QueryRow instead of Exec
	var journalMode string
	if err := db.QueryRow("PRAGMA journal_mode=WAL").Scan(&journalMode); err != nil {
		// Not fatal — some libsql configurations (e.g. Turso) don't support WAL pragma.
		// Intentionally ignored: WAL mode is a performance optimisation, not a correctness requirement.
		journalMode = "unknown"
		_ = journalMode // suppress unused warning
		_ = err         //nolint:errcheck // graceful degradation
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	if err := RunMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return &SQLiteStore{db: db, path: dbPath}, nil
}

// encodeEmbedding converts []float32 to []byte (little-endian float32 array).
func encodeEmbedding(emb []float32) []byte {
	if len(emb) == 0 {
		return nil
	}
	buf := make([]byte, len(emb)*4)
	for i, v := range emb {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

// decodeEmbedding converts []byte back to []float32.
func decodeEmbedding(data []byte) []float32 {
	if len(data) == 0 {
		return nil
	}
	n := len(data) / 4
	result := make([]float32, n)
	for i := range result {
		bits := binary.LittleEndian.Uint32(data[i*4:])
		result[i] = math.Float32frombits(bits)
	}
	return result
}

// cosineSimilarity computes cosine similarity between two float32 slices.
// Returns 0 if either slice is empty or their norms are zero.
func cosineSimilarity(a, b []float32) float64 {
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
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// InsertFact inserts a new fact record into the database.
func (s *SQLiteStore) InsertFact(ctx context.Context, fact *FactRecord) error {
	var expiresAt interface{}
	if fact.ExpiresAt != nil {
		expiresAt = *fact.ExpiresAt
	}
	var supersededBy interface{}
	if fact.SupersededBy != nil {
		supersededBy = *fact.SupersededBy
	}
	var embBlob interface{}
	if len(fact.Embedding) > 0 {
		embBlob = encodeEmbedding(fact.Embedding)
	}

	now := time.Now().UnixMilli()
	if fact.CreatedAt == 0 {
		fact.CreatedAt = now
	}
	if fact.UpdatedAt == 0 {
		fact.UpdatedAt = now
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO facts (id, content, category, container, importance, confidence, source, created_at, updated_at, expires_at, superseded_by, embedding, deleted)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		fact.ID, fact.Content, fact.Category, fact.Container,
		fact.Importance, fact.Confidence, fact.Source,
		fact.CreatedAt, fact.UpdatedAt, expiresAt, supersededBy,
		embBlob, boolToInt(fact.Deleted),
	)
	if err != nil {
		return fmt.Errorf("insert fact: %w", err)
	}
	return nil
}

// GetFact retrieves a single fact by ID.
func (s *SQLiteStore) GetFact(ctx context.Context, id string) (*FactRecord, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, content, category, container, importance, confidence, source,
		        created_at, updated_at, expires_at, superseded_by, embedding, deleted
		 FROM facts WHERE id = ?`, id)
	return scanFact(row)
}

// UpdateFact updates an existing fact record.
func (s *SQLiteStore) UpdateFact(ctx context.Context, fact *FactRecord) error {
	fact.UpdatedAt = time.Now().UnixMilli()

	var expiresAt interface{}
	if fact.ExpiresAt != nil {
		expiresAt = *fact.ExpiresAt
	}
	var supersededBy interface{}
	if fact.SupersededBy != nil {
		supersededBy = *fact.SupersededBy
	}
	var embBlob interface{}
	if len(fact.Embedding) > 0 {
		embBlob = encodeEmbedding(fact.Embedding)
	}

	result, err := s.db.ExecContext(ctx,
		`UPDATE facts SET content=?, category=?, container=?, importance=?, confidence=?,
		        source=?, updated_at=?, expires_at=?, superseded_by=?, embedding=?, deleted=?
		 WHERE id=?`,
		fact.Content, fact.Category, fact.Container, fact.Importance, fact.Confidence,
		fact.Source, fact.UpdatedAt, expiresAt, supersededBy, embBlob,
		boolToInt(fact.Deleted), fact.ID,
	)
	if err != nil {
		return fmt.Errorf("update fact: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("fact not found: %s", fact.ID)
	}
	return nil
}

// SoftDeleteFact marks a fact as deleted without removing it from the database.
func (s *SQLiteStore) SoftDeleteFact(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE facts SET deleted=1, updated_at=? WHERE id=? AND deleted=0`,
		time.Now().UnixMilli(), id)
	if err != nil {
		return fmt.Errorf("soft delete fact: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("fact not found or already deleted: %s", id)
	}
	return nil
}

// ListFacts retrieves facts with optional filtering.
func (s *SQLiteStore) ListFacts(ctx context.Context, opts ListFactsOpts) ([]*FactRecord, error) {
	if opts.Limit <= 0 {
		opts.Limit = 100
	}

	var conditions []string
	var args []interface{}

	if !opts.IncludeDeleted {
		conditions = append(conditions, "deleted = 0")
	}
	if !opts.IncludeSuperseded {
		conditions = append(conditions, "superseded_by IS NULL")
	}
	if opts.Container != "" {
		conditions = append(conditions, "container = ?")
		args = append(args, opts.Container)
	}
	if opts.Category != "" {
		conditions = append(conditions, "category = ?")
		args = append(args, opts.Category)
	}

	query := `SELECT id, content, category, container, importance, confidence, source,
	                 created_at, updated_at, expires_at, superseded_by, embedding, deleted
	          FROM facts`
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	args = append(args, opts.Limit, opts.Offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list facts: %w", err)
	}
	defer rows.Close()

	return scanFacts(rows)
}

// SupersedeFact marks oldID as superseded by newID and lowers its confidence.
func (s *SQLiteStore) SupersedeFact(ctx context.Context, oldID, newID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE facts SET superseded_by=?, confidence=0.3, updated_at=? WHERE id=?`,
		newID, time.Now().UnixMilli(), oldID)
	if err != nil {
		return fmt.Errorf("supersede fact: %w", err)
	}
	return nil
}

// InsertTurn inserts a conversation turn record.
func (s *SQLiteStore) InsertTurn(ctx context.Context, turn *TurnRecord) error {
	if turn.CreatedAt == 0 {
		turn.CreatedAt = time.Now().UnixMilli()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO turns (id, session_id, role, content, created_at, processed)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		turn.ID, turn.SessionID, turn.Role, turn.Content,
		turn.CreatedAt, boolToInt(turn.Processed),
	)
	if err != nil {
		return fmt.Errorf("insert turn: %w", err)
	}
	return nil
}

// GetUnprocessedTurns returns turns that haven't been processed for extraction yet.
func (s *SQLiteStore) GetUnprocessedTurns(ctx context.Context, limit int) ([]*TurnRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, role, content, created_at, processed
		 FROM turns WHERE processed=0 ORDER BY created_at ASC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("get unprocessed turns: %w", err)
	}
	defer rows.Close()
	return scanTurns(rows)
}

// MarkTurnProcessed marks a turn as processed.
func (s *SQLiteStore) MarkTurnProcessed(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE turns SET processed=1 WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("mark turn processed: %w", err)
	}
	return nil
}

// SetProfile upserts a profile key-value pair.
func (s *SQLiteStore) SetProfile(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO profile (key, value, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		key, value, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("set profile: %w", err)
	}
	return nil
}

// GetProfile retrieves a profile entry by key.
func (s *SQLiteStore) GetProfile(ctx context.Context, key string) (*ProfileEntry, error) {
	var entry ProfileEntry
	err := s.db.QueryRowContext(ctx,
		`SELECT key, value, updated_at FROM profile WHERE key=?`, key).
		Scan(&entry.Key, &entry.Value, &entry.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get profile: %w", err)
	}
	return &entry, nil
}

// ListProfile returns all profile entries.
func (s *SQLiteStore) ListProfile(ctx context.Context) ([]*ProfileEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT key, value, updated_at FROM profile ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("list profile: %w", err)
	}
	defer rows.Close()

	var entries []*ProfileEntry
	for rows.Next() {
		var e ProfileEntry
		if err := rows.Scan(&e.Key, &e.Value, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan profile entry: %w", err)
		}
		entries = append(entries, &e)
	}
	return entries, rows.Err()
}

// DeleteProfile removes a profile entry by key.
func (s *SQLiteStore) DeleteProfile(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM profile WHERE key=?`, key)
	if err != nil {
		return fmt.Errorf("delete profile: %w", err)
	}
	return nil
}

// SearchFTS performs BM25 full-text search using SQLite FTS5.
func (s *SQLiteStore) SearchFTS(ctx context.Context, query string, limit int) ([]*FactRecord, error) {
	if limit <= 0 {
		limit = 10
	}
	// Use FTS5 via rowid match — query the virtual table and join back to facts
	// The correct FTS5 approach: query facts_fts, use rowid to join to facts
	rows, err := s.db.QueryContext(ctx,
		`SELECT f.id, f.content, f.category, f.container, f.importance, f.confidence, f.source,
		        f.created_at, f.updated_at, f.expires_at, f.superseded_by, f.embedding, f.deleted
		 FROM facts f
		 WHERE f.rowid IN (
		     SELECT rowid FROM facts_fts WHERE facts_fts MATCH ?
		 )
		 AND f.deleted=0 AND f.superseded_by IS NULL
		 ORDER BY f.importance DESC
		 LIMIT ?`,
		query, limit)
	if err != nil {
		return nil, fmt.Errorf("FTS search: %w", err)
	}
	defer rows.Close()
	return scanFacts(rows)
}

// SearchVector performs brute-force cosine similarity search over stored embeddings.
// Returns facts sorted by cosine similarity descending, filtered by threshold.
func (s *SQLiteStore) SearchVector(ctx context.Context, queryEmbedding []float32, limit int, threshold float64) ([]*FactRecord, error) {
	if limit <= 0 {
		limit = 10
	}

	// Load all facts with embeddings (active, non-superseded)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, content, category, container, importance, confidence, source,
		        created_at, updated_at, expires_at, superseded_by, embedding, deleted
		 FROM facts
		 WHERE embedding IS NOT NULL AND deleted=0 AND superseded_by IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("vector search load facts: %w", err)
	}
	defer rows.Close()

	allFacts, err := scanFacts(rows)
	if err != nil {
		return nil, err
	}

	// Compute cosine similarity for all facts
	type scoredFact struct {
		fact  *FactRecord
		score float64
	}

	var scored []scoredFact
	for _, f := range allFacts {
		if len(f.Embedding) == 0 {
			continue
		}
		sim := cosineSimilarity(queryEmbedding, f.Embedding)
		if sim >= threshold {
			scored = append(scored, scoredFact{fact: f, score: sim})
		}
	}

	// Sort by score descending (insertion sort)
	for i := 1; i < len(scored); i++ {
		for j := i; j > 0 && scored[j].score > scored[j-1].score; j-- {
			scored[j], scored[j-1] = scored[j-1], scored[j]
		}
	}

	// Return top-k
	if len(scored) > limit {
		scored = scored[:limit]
	}

	result := make([]*FactRecord, len(scored))
	for i, sf := range scored {
		result[i] = sf.fact
	}
	return result, nil
}

// ListDecayable returns facts that may need importance decay processing.
// before is a unix timestamp in millis; minImportance is the pruning threshold.
func (s *SQLiteStore) ListDecayable(ctx context.Context, before int64, minImportance float64) ([]*FactRecord, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, content, category, container, importance, confidence, source,
		        created_at, updated_at, expires_at, superseded_by, embedding, deleted
		 FROM facts
		 WHERE deleted=0 AND (created_at < ? OR (expires_at IS NOT NULL AND expires_at < ?))
		 AND importance > 0`,
		before, time.Now().UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("list decayable: %w", err)
	}
	defer rows.Close()
	return scanFacts(rows)
}

// PruneFacts soft-deletes a list of facts by ID.
// Returns the count of facts actually deleted.
func (s *SQLiteStore) PruneFacts(ctx context.Context, ids []string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	now := time.Now().UnixMilli()
	total := 0
	for _, id := range ids {
		result, err := s.db.ExecContext(ctx,
			`UPDATE facts SET deleted=1, updated_at=? WHERE id=? AND deleted=0`,
			now, id)
		if err != nil {
			return total, fmt.Errorf("prune fact %s: %w", id, err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return total, fmt.Errorf("rows affected: %w", err)
		}
		total += int(rows)
	}
	return total, nil
}

// LastSyncTimestamp returns the last Turso sync timestamp (unix millis).
// Returns 0 if never synced.
func (s *SQLiteStore) LastSyncTimestamp(ctx context.Context) (int64, error) {
	var value string
	err := s.db.QueryRowContext(ctx,
		`SELECT value FROM sync_state WHERE key='last_sync_at'`).Scan(&value)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get last sync timestamp: %w", err)
	}
	var ts int64
	if _, err := fmt.Sscanf(value, "%d", &ts); err != nil {
		return 0, fmt.Errorf("parse sync timestamp: %w", err)
	}
	return ts, nil
}

// SetLastSyncTimestamp records the last Turso sync timestamp.
func (s *SQLiteStore) SetLastSyncTimestamp(ctx context.Context, ts int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sync_state (key, value) VALUES ('last_sync_at', ?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		fmt.Sprintf("%d", ts))
	if err != nil {
		return fmt.Errorf("set last sync timestamp: %w", err)
	}
	return nil
}

// Close closes the database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// Stats returns database statistics.
func (s *SQLiteStore) Stats(ctx context.Context) (*StoreStats, error) {
	var stats StoreStats

	// Total facts
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM facts`).Scan(&stats.TotalFacts); err != nil {
		return nil, fmt.Errorf("count total facts: %w", err)
	}
	// Active facts (not deleted, not superseded)
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM facts WHERE deleted=0 AND superseded_by IS NULL`).Scan(&stats.ActiveFacts); err != nil {
		return nil, fmt.Errorf("count active facts: %w", err)
	}
	// Superseded facts
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM facts WHERE superseded_by IS NOT NULL AND deleted=0`).Scan(&stats.SupersededFacts); err != nil {
		return nil, fmt.Errorf("count superseded facts: %w", err)
	}
	// Deleted facts
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM facts WHERE deleted=1`).Scan(&stats.DeletedFacts); err != nil {
		return nil, fmt.Errorf("count deleted facts: %w", err)
	}
	// Total turns
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM turns`).Scan(&stats.TotalTurns); err != nil {
		return nil, fmt.Errorf("count total turns: %w", err)
	}
	// Unprocessed turns
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM turns WHERE processed=0`).Scan(&stats.UnprocessedTurns); err != nil {
		return nil, fmt.Errorf("count unprocessed turns: %w", err)
	}
	// Profile entries
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM profile`).Scan(&stats.ProfileEntries); err != nil {
		return nil, fmt.Errorf("count profile entries: %w", err)
	}

	// DB size
	if s.path != "" {
		fi, err := os.Stat(s.path)
		if err == nil {
			stats.DBSizeBytes = fi.Size()
		}
	}

	return &stats, nil
}

// --- helpers ---

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanFact(row rowScanner) (*FactRecord, error) {
	var f FactRecord
	var deleted int
	var embBlob []byte
	var expiresAt sql.NullInt64
	var supersededBy sql.NullString

	err := row.Scan(
		&f.ID, &f.Content, &f.Category, &f.Container,
		&f.Importance, &f.Confidence, &f.Source,
		&f.CreatedAt, &f.UpdatedAt,
		&expiresAt, &supersededBy,
		&embBlob, &deleted,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan fact: %w", err)
	}

	f.Deleted = deleted != 0
	if expiresAt.Valid {
		v := expiresAt.Int64
		f.ExpiresAt = &v
	}
	if supersededBy.Valid {
		v := supersededBy.String
		f.SupersededBy = &v
	}
	if len(embBlob) > 0 {
		f.Embedding = decodeEmbedding(embBlob)
	}
	return &f, nil
}

func scanFacts(rows *sql.Rows) ([]*FactRecord, error) {
	var facts []*FactRecord
	for rows.Next() {
		f, err := scanFact(rows)
		if err != nil {
			return nil, err
		}
		facts = append(facts, f)
	}
	return facts, rows.Err()
}

func scanTurns(rows *sql.Rows) ([]*TurnRecord, error) {
	var turns []*TurnRecord
	for rows.Next() {
		var t TurnRecord
		var processed int
		if err := rows.Scan(&t.ID, &t.SessionID, &t.Role, &t.Content, &t.CreatedAt, &processed); err != nil {
			return nil, fmt.Errorf("scan turn: %w", err)
		}
		t.Processed = processed != 0
		turns = append(turns, &t)
	}
	return turns, rows.Err()
}
