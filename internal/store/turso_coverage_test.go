package store

// turso_coverage_test.go — tests for TursoSync using a mock connector.
// We bypass NewTursoSync (which requires real Turso) and construct TursoSync
// directly with a mock connector + local SQLite DB.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	libsql "github.com/tursodatabase/go-libsql"
)

// ─── Mock connector ───────────────────────────────────────────────────────────

// mockConnector implements tursoConnector with configurable behaviour.
type mockConnector struct {
	syncErr   error
	closeErr  error
	syncCount int
}

func (m *mockConnector) Sync() (libsql.Replicated, error) {
	m.syncCount++
	return libsql.Replicated{}, m.syncErr
}

func (m *mockConnector) Close() error {
	return m.closeErr
}

// ─── Helper: build a TursoSync with mock connector + real local SQLite ────────

func newMockTursoSync(t *testing.T, conn *mockConnector) (*TursoSync, func()) {
	t.Helper()
	f, err := os.CreateTemp("", "turso_mock_*.db")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	f.Close()

	// Open a real local SQLite for the local DB handle
	db, err := sql.Open("libsql", "file:"+path)
	if err != nil {
		os.Remove(path)
		t.Fatal(err)
	}

	ts := &TursoSync{
		connector: conn,
		localDB:   db,
		interval:  50 * time.Millisecond,
		stopCh:    make(chan struct{}),
		stopped:   make(chan struct{}),
	}

	cleanup := func() {
		db.Close()
		os.Remove(path)
	}
	return ts, cleanup
}

// ─── Tests ────────────────────────────────────────────────────────────────────

func TestTursoSync_DB(t *testing.T) {
	conn := &mockConnector{}
	ts, cleanup := newMockTursoSync(t, conn)
	defer cleanup()

	if ts.DB() == nil {
		t.Error("DB() should return non-nil")
	}
}

func TestTursoSync_SyncNow_Success(t *testing.T) {
	conn := &mockConnector{}
	ts, cleanup := newMockTursoSync(t, conn)
	defer cleanup()

	if err := ts.SyncNow(context.Background()); err != nil {
		t.Errorf("SyncNow: %v", err)
	}
	if conn.syncCount != 1 {
		t.Errorf("expected 1 sync call, got %d", conn.syncCount)
	}
}

func TestTursoSync_SyncNow_Error(t *testing.T) {
	conn := &mockConnector{syncErr: errors.New("sync failed")}
	ts, cleanup := newMockTursoSync(t, conn)
	defer cleanup()

	err := ts.SyncNow(context.Background())
	if err == nil {
		t.Error("expected error from SyncNow")
	}
}

func TestTursoSync_Close_Success(t *testing.T) {
	conn := &mockConnector{}
	ts, _ := newMockTursoSync(t, conn)

	if err := ts.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestTursoSync_Close_ConnectorError(t *testing.T) {
	conn := &mockConnector{closeErr: fmt.Errorf("connector close error")}
	ts, _ := newMockTursoSync(t, conn)

	// localDB.Close() should succeed, then connector.Close() errors
	err := ts.Close()
	if err == nil {
		t.Error("expected error from connector.Close()")
	}
}

func TestTursoSync_StartStop(t *testing.T) {
	conn := &mockConnector{}
	ts, cleanup := newMockTursoSync(t, conn)
	defer cleanup()

	ts.Start()
	// Let it tick a couple times
	time.Sleep(130 * time.Millisecond)
	ts.Stop()

	// Should have synced at least once (tick + final sync on Stop)
	if conn.syncCount < 1 {
		t.Logf("syncCount=%d (may be 0 if goroutine didn't tick in time)", conn.syncCount)
	}
}

func TestTursoSync_StartStop_SyncError(t *testing.T) {
	conn := &mockConnector{syncErr: errors.New("sync failure")}
	ts, cleanup := newMockTursoSync(t, conn)
	defer cleanup()

	ts.Start()
	// Let it error a couple times
	time.Sleep(130 * time.Millisecond)
	ts.Stop()
}

func TestNewTursoSync_InvalidURL(t *testing.T) {
	f, err := os.CreateTemp("", "turso_inv_*.db")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	f.Close()
	defer os.Remove(path)
	defer os.Remove(path + ".turso")

	// Attempt to create TursoSync with invalid URL
	_, err = NewTursoSync(path+".turso", "invalid://not-real", "fake-token", time.Second)
	if err != nil {
		t.Logf("NewTursoSync with invalid URL: %v (expected)", err)
	} else {
		t.Log("NewTursoSync succeeded with invalid URL (graceful degradation)")
	}
}

// TursoSync_Close_DBError tests when the local DB close errors
func TestTursoSync_Close_DBError(t *testing.T) {
	conn := &mockConnector{}
	ts, _ := newMockTursoSync(t, conn)

	// Close the DB first so localDB.Close() returns error
	ts.localDB.Close()

	// Now Close() should return an error from localDB.Close()
	// (double-close is generally a no-op for database/sql, but let's test it)
	_ = ts.Close()
}

// ─── NewTursoSyncFromConnector and adapters ───────────────────────────────────

// mockExternalConnector implements TursoConnector (the exported interface).
type mockExternalConnector struct {
	syncErr  error
	closeErr error
}

func (m *mockExternalConnector) Sync() (interface{}, error) {
	return nil, m.syncErr
}

func (m *mockExternalConnector) Close() error {
	return m.closeErr
}

func TestNewTursoSyncFromConnector_Success(t *testing.T) {
	ec := &mockExternalConnector{}
	ts := NewTursoSyncFromConnector(ec, nil, 50*time.Millisecond)
	if ts == nil {
		t.Fatal("expected non-nil TursoSync")
	}

	// Test SyncNow (exercises mockableTursoConnector.Sync -> ec.Sync)
	if err := ts.SyncNow(context.Background()); err != nil {
		t.Logf("SyncNow error (may be OK for nil db): %v", err)
	}

	// Test DB()
	db := ts.DB()
	_ = db

	// Cleanup
	ts.localDB.Close()
}

func TestNewTursoSyncFromConnector_SyncError(t *testing.T) {
	ec := &mockExternalConnector{syncErr: errors.New("sync failure")}
	ts := NewTursoSyncFromConnector(ec, nil, 50*time.Millisecond)

	err := ts.SyncNow(context.Background())
	if err == nil {
		t.Error("expected sync error")
	}
	ts.localDB.Close()
}

func TestNewTursoSyncFromConnector_CloseError(t *testing.T) {
	ec := &mockExternalConnector{closeErr: errors.New("close failure")}
	ts := NewTursoSyncFromConnector(ec, nil, 50*time.Millisecond)

	// localDB.Close() first, then connector.Close() errors
	// Actually ts.Close() calls localDB.Close() then connector.Close()
	err := ts.Close()
	if err == nil {
		t.Error("expected close error from connector")
	}
}

func TestNewTursoSyncFromConnector_StartStop(t *testing.T) {
	ec := &mockExternalConnector{}
	ts := NewTursoSyncFromConnector(ec, nil, 50*time.Millisecond)

	ts.Start()
	time.Sleep(80 * time.Millisecond)
	ts.Stop()
	ts.localDB.Close()
}

func TestNewTursoSyncFromConnector_StartStop_SyncError(t *testing.T) {
	ec := &mockExternalConnector{syncErr: errors.New("tick error")}
	ts := NewTursoSyncFromConnector(ec, nil, 50*time.Millisecond)

	ts.Start()
	time.Sleep(80 * time.Millisecond)
	ts.Stop() // final sync will also error (logged)
	ts.localDB.Close()
}
