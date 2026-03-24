package store

import (
	"database/sql"
	"fmt"
	"time"
)

// migration represents a single database migration.
type migration struct {
	name       string
	statements []string
}

// migrations is an ordered list of SQL migrations.
// Each migration contains individual SQL statements (go-libsql can't handle multi-statement SQL).
var migrations = []migration{
	{
		name: "v1_create_tables",
		statements: []string{
			`CREATE TABLE IF NOT EXISTS _migrations (
				version    INTEGER PRIMARY KEY,
				name       TEXT NOT NULL,
				applied_at INTEGER NOT NULL
			)`,
			`CREATE TABLE IF NOT EXISTS facts (
				id            TEXT PRIMARY KEY,
				content       TEXT NOT NULL,
				category      TEXT NOT NULL
				              CHECK(category IN ('person','project','preference','event','technical','general')),
				container     TEXT NOT NULL DEFAULT 'general'
				              CHECK(container IN ('work','trading','clawchain','personal','general')),
				importance    REAL NOT NULL DEFAULT 0.7
				              CHECK(importance >= 0.0 AND importance <= 1.0),
				confidence    REAL NOT NULL DEFAULT 1.0
				              CHECK(confidence >= 0.0 AND confidence <= 1.0),
				source        TEXT,
				created_at    INTEGER NOT NULL,
				updated_at    INTEGER NOT NULL,
				expires_at    INTEGER,
				superseded_by TEXT REFERENCES facts(id) ON DELETE SET NULL,
				embedding     BLOB,
				deleted       INTEGER NOT NULL DEFAULT 0
			)`,
			`CREATE TABLE IF NOT EXISTS turns (
				id          TEXT PRIMARY KEY,
				session_id  TEXT NOT NULL,
				role        TEXT NOT NULL CHECK(role IN ('user','assistant')),
				content     TEXT NOT NULL,
				created_at  INTEGER NOT NULL,
				processed   INTEGER NOT NULL DEFAULT 0
			)`,
			`CREATE TABLE IF NOT EXISTS profile (
				key         TEXT PRIMARY KEY,
				value       TEXT NOT NULL,
				updated_at  INTEGER NOT NULL
			)`,
			`CREATE TABLE IF NOT EXISTS sync_state (
				key   TEXT PRIMARY KEY,
				value TEXT NOT NULL
			)`,
		},
	},
	{
		name: "v2_add_fts5",
		statements: []string{
			`CREATE VIRTUAL TABLE IF NOT EXISTS facts_fts USING fts5(
				content,
				category,
				container,
				content=facts,
				content_rowid=rowid
			)`,
			`CREATE TRIGGER IF NOT EXISTS facts_ai AFTER INSERT ON facts BEGIN
				INSERT INTO facts_fts(rowid, content, category, container)
				VALUES (new.rowid, new.content, new.category, new.container);
			END`,
			`CREATE TRIGGER IF NOT EXISTS facts_ad AFTER DELETE ON facts BEGIN
				INSERT INTO facts_fts(facts_fts, rowid, content, category, container)
				VALUES ('delete', old.rowid, old.content, old.category, old.container);
			END`,
			`CREATE TRIGGER IF NOT EXISTS facts_au AFTER UPDATE ON facts BEGIN
				INSERT INTO facts_fts(facts_fts, rowid, content, category, container)
				VALUES ('delete', old.rowid, old.content, old.category, old.container);
				INSERT INTO facts_fts(rowid, content, category, container)
				VALUES (new.rowid, new.content, new.category, new.container);
			END`,
		},
	},
	{
		name: "v3_add_indexes",
		statements: []string{
			`CREATE INDEX IF NOT EXISTS idx_facts_container ON facts(container) WHERE deleted = 0`,
			`CREATE INDEX IF NOT EXISTS idx_facts_category ON facts(category) WHERE deleted = 0`,
			`CREATE INDEX IF NOT EXISTS idx_facts_created_at ON facts(created_at) WHERE deleted = 0`,
			`CREATE INDEX IF NOT EXISTS idx_facts_importance ON facts(importance) WHERE deleted = 0`,
			`CREATE INDEX IF NOT EXISTS idx_facts_superseded_by ON facts(superseded_by) WHERE superseded_by IS NOT NULL`,
			`CREATE INDEX IF NOT EXISTS idx_facts_expires_at ON facts(expires_at) WHERE expires_at IS NOT NULL AND deleted = 0`,
			`CREATE INDEX IF NOT EXISTS idx_turns_processed ON turns(processed, created_at) WHERE processed = 0`,
			`CREATE INDEX IF NOT EXISTS idx_turns_session ON turns(session_id, created_at)`,
		},
	},
}

// RunMigrations executes all pending migrations.
// Migrations are tracked in the _migrations table and run exactly once.
func RunMigrations(db *sql.DB) error {
	// Ensure migrations table exists first
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS _migrations (
		version    INTEGER PRIMARY KEY,
		name       TEXT NOT NULL,
		applied_at INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	for i, m := range migrations {
		version := i + 1

		// Check if already applied
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM _migrations WHERE version = ?", version).Scan(&count); err != nil {
			return fmt.Errorf("check migration %d: %w", version, err)
		}
		if count > 0 {
			continue // already applied
		}

		// Execute each statement individually
		for j, stmt := range m.statements {
			if _, err := db.Exec(stmt); err != nil {
				return fmt.Errorf("execute migration %d (%s) stmt %d: %w", version, m.name, j+1, err)
			}
		}

		// Record it
		if _, err := db.Exec(
			"INSERT INTO _migrations (version, name, applied_at) VALUES (?, ?, ?)",
			version, m.name, time.Now().Unix(),
		); err != nil {
			return fmt.Errorf("record migration %d: %w", version, err)
		}
	}

	return nil
}
