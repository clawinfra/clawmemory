package store

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	libsql "github.com/tursodatabase/go-libsql"
)

// TursoSync manages background sync between local SQLite and Turso cloud.
// It uses go-libsql's embedded replica connector for automatic sync.
type TursoSync struct {
	connector *libsql.Connector
	localDB   *sql.DB
	interval  time.Duration
	mu        sync.Mutex
	stopCh    chan struct{}
	stopped   chan struct{}
}

// NewTursoSync creates a TursoSync that wraps an embedded replica.
// The local file at dbPath is the SQLite database; Turso is the remote primary.
// Uses go-libsql's embedded replica connector for automatic sync.
func NewTursoSync(dbPath, remoteURL, authToken string, syncInterval time.Duration) (*TursoSync, error) {
	connector, err := libsql.NewEmbeddedReplicaConnector(dbPath, remoteURL,
		libsql.WithAuthToken(authToken),
	)
	if err != nil {
		return nil, fmt.Errorf("create turso connector: %w", err)
	}

	db := sql.OpenDB(connector)
	if err := db.Ping(); err != nil {
		// Don't fail hard — Turso may be unavailable, we fall back gracefully
		log.Printf("[clawmemory] Turso sync failed — operating in local-only mode: %v", err)
	}

	return &TursoSync{
		connector: connector,
		localDB:   db,
		interval:  syncInterval,
		stopCh:    make(chan struct{}),
		stopped:   make(chan struct{}),
	}, nil
}

// Start begins the background sync goroutine.
// Calls connector.Sync() every syncInterval.
func (t *TursoSync) Start() {
	go func() {
		defer close(t.stopped)
		ticker := time.NewTicker(t.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := t.syncOnce(); err != nil {
					log.Printf("[clawmemory] Turso sync error: %v", err)
				}
			case <-t.stopCh:
				// Final sync before exit
				if err := t.syncOnce(); err != nil {
					log.Printf("[clawmemory] Turso final sync error: %v", err)
				}
				return
			}
		}
	}()
}

// Stop halts the background sync goroutine. Blocks until stopped.
func (t *TursoSync) Stop() {
	close(t.stopCh)
	<-t.stopped
}

// SyncNow triggers an immediate sync. Returns error if sync fails.
func (t *TursoSync) SyncNow(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, err := t.connector.Sync()
	return err
}

// syncOnce is the internal sync call (holds the mutex).
func (t *TursoSync) syncOnce() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, err := t.connector.Sync()
	return err
}

// DB returns the local database handle (reads are always local).
func (t *TursoSync) DB() *sql.DB {
	return t.localDB
}

// Close closes the database connection and connector.
func (t *TursoSync) Close() error {
	if err := t.localDB.Close(); err != nil {
		return err
	}
	return t.connector.Close()
}
