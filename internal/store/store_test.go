package store

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// newTestStore creates an in-memory SQLite store for testing.
func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	// Use a temp file for tests since go-libsql doesn't support :memory:
	f, err := os.CreateTemp("", "clawmemory_test_*.db")
	if err != nil {
		t.Fatalf("create temp db file: %v", err)
	}
	path := f.Name()
	f.Close()

	s, err := NewSQLiteStore(path)
	if err != nil {
		os.Remove(path)
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() {
		s.Close()
		os.Remove(path)
	})
	return s
}

func testFact(id string) *FactRecord {
	now := time.Now().UnixMilli()
	return &FactRecord{
		ID:         id,
		Content:    fmt.Sprintf("Test fact %s content", id),
		Category:   "person",
		Container:  "personal",
		Importance: 0.7,
		Confidence: 1.0,
		Source:     "test",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

func TestNewSQLiteStore(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Tables should exist
	stats, err := s.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.TotalFacts != 0 {
		t.Errorf("expected 0 facts, got %d", stats.TotalFacts)
	}
}

func TestInsertFact(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	fact := testFact("fact-001")
	if err := s.InsertFact(ctx, fact); err != nil {
		t.Fatalf("InsertFact: %v", err)
	}

	got, err := s.GetFact(ctx, "fact-001")
	if err != nil {
		t.Fatalf("GetFact: %v", err)
	}
	if got == nil {
		t.Fatal("GetFact returned nil")
	}
	if got.Content != fact.Content {
		t.Errorf("content mismatch: got %q, want %q", got.Content, fact.Content)
	}
	if got.Category != "person" {
		t.Errorf("category mismatch: got %q", got.Category)
	}
	if got.Importance != 0.7 {
		t.Errorf("importance mismatch: got %f", got.Importance)
	}
	if got.Deleted {
		t.Error("expected not deleted")
	}
}

func TestInsertFact_DuplicateID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	fact := testFact("dup-001")
	if err := s.InsertFact(ctx, fact); err != nil {
		t.Fatalf("first InsertFact: %v", err)
	}
	if err := s.InsertFact(ctx, fact); err == nil {
		t.Error("expected error on duplicate insert, got nil")
	}
}

func TestUpdateFact(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	fact := testFact("upd-001")
	if err := s.InsertFact(ctx, fact); err != nil {
		t.Fatalf("InsertFact: %v", err)
	}

	fact.Importance = 0.9
	fact.Confidence = 0.5
	if err := s.UpdateFact(ctx, fact); err != nil {
		t.Fatalf("UpdateFact: %v", err)
	}

	got, err := s.GetFact(ctx, "upd-001")
	if err != nil || got == nil {
		t.Fatalf("GetFact: %v", err)
	}
	if got.Importance != 0.9 {
		t.Errorf("importance not updated: got %f", got.Importance)
	}
	if got.Confidence != 0.5 {
		t.Errorf("confidence not updated: got %f", got.Confidence)
	}
}

func TestSoftDeleteFact(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	fact := testFact("del-001")
	if err := s.InsertFact(ctx, fact); err != nil {
		t.Fatalf("InsertFact: %v", err)
	}

	if err := s.SoftDeleteFact(ctx, "del-001"); err != nil {
		t.Fatalf("SoftDeleteFact: %v", err)
	}

	// Default list should not include deleted
	facts, err := s.ListFacts(ctx, ListFactsOpts{})
	if err != nil {
		t.Fatalf("ListFacts: %v", err)
	}
	for _, f := range facts {
		if f.ID == "del-001" {
			t.Error("deleted fact should not appear in default list")
		}
	}
}

func TestSoftDeleteFact_IncludeDeleted(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	fact := testFact("del-002")
	if err := s.InsertFact(ctx, fact); err != nil {
		t.Fatalf("InsertFact: %v", err)
	}
	if err := s.SoftDeleteFact(ctx, "del-002"); err != nil {
		t.Fatalf("SoftDeleteFact: %v", err)
	}

	facts, err := s.ListFacts(ctx, ListFactsOpts{IncludeDeleted: true})
	if err != nil {
		t.Fatalf("ListFacts: %v", err)
	}
	found := false
	for _, f := range facts {
		if f.ID == "del-002" && f.Deleted {
			found = true
		}
	}
	if !found {
		t.Error("deleted fact should appear in list when IncludeDeleted=true")
	}
}

func TestSoftDeleteFact_NotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.SoftDeleteFact(ctx, "nonexistent"); err == nil {
		t.Error("expected error for nonexistent fact")
	}
}

func TestSupersedeFact(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	old := testFact("sup-old")
	newFact := testFact("sup-new")
	if err := s.InsertFact(ctx, old); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertFact(ctx, newFact); err != nil {
		t.Fatal(err)
	}

	if err := s.SupersedeFact(ctx, "sup-old", "sup-new"); err != nil {
		t.Fatalf("SupersedeFact: %v", err)
	}

	got, err := s.GetFact(ctx, "sup-old")
	if err != nil || got == nil {
		t.Fatalf("GetFact: %v", err)
	}
	if got.SupersededBy == nil || *got.SupersededBy != "sup-new" {
		t.Errorf("superseded_by not set correctly: got %v", got.SupersededBy)
	}
	if got.Confidence != 0.3 {
		t.Errorf("confidence should be 0.3 after supersede, got %f", got.Confidence)
	}
}

func TestListFacts_FilterContainer(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	f1 := testFact("cf-001")
	f1.Container = "work"
	f2 := testFact("cf-002")
	f2.Container = "personal"

	if err := s.InsertFact(ctx, f1); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertFact(ctx, f2); err != nil {
		t.Fatal(err)
	}

	facts, err := s.ListFacts(ctx, ListFactsOpts{Container: "work"})
	if err != nil {
		t.Fatalf("ListFacts: %v", err)
	}
	if len(facts) != 1 || facts[0].ID != "cf-001" {
		t.Errorf("expected 1 work fact, got %d", len(facts))
	}
}

func TestListFacts_FilterCategory(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	f1 := testFact("cat-001")
	f1.Category = "project"
	f2 := testFact("cat-002")
	f2.Category = "preference"

	s.InsertFact(ctx, f1)
	s.InsertFact(ctx, f2)

	facts, err := s.ListFacts(ctx, ListFactsOpts{Category: "project"})
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 || facts[0].ID != "cat-001" {
		t.Errorf("expected 1 project fact, got %d", len(facts))
	}
}

func TestListFacts_Pagination(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		f := testFact(fmt.Sprintf("pag-%03d", i))
		time.Sleep(time.Millisecond) // ensure distinct timestamps
		if err := s.InsertFact(ctx, f); err != nil {
			t.Fatal(err)
		}
	}

	page1, err := s.ListFacts(ctx, ListFactsOpts{Limit: 3, Offset: 0})
	if err != nil {
		t.Fatalf("ListFacts page1: %v", err)
	}
	if len(page1) != 3 {
		t.Errorf("expected 3 facts on page1, got %d", len(page1))
	}

	page2, err := s.ListFacts(ctx, ListFactsOpts{Limit: 3, Offset: 3})
	if err != nil {
		t.Fatalf("ListFacts page2: %v", err)
	}
	if len(page2) != 3 {
		t.Errorf("expected 3 facts on page2, got %d", len(page2))
	}

	// Ensure no overlap
	ids1 := map[string]bool{}
	for _, f := range page1 {
		ids1[f.ID] = true
	}
	for _, f := range page2 {
		if ids1[f.ID] {
			t.Errorf("fact %s appears in both pages", f.ID)
		}
	}
}

func TestInsertTurn(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	turn := &TurnRecord{
		ID:        "turn-001",
		SessionID: "sess-001",
		Role:      "user",
		Content:   "Hello world",
		CreatedAt: time.Now().UnixMilli(),
	}
	if err := s.InsertTurn(ctx, turn); err != nil {
		t.Fatalf("InsertTurn: %v", err)
	}

	turns, err := s.GetUnprocessedTurns(ctx, 10)
	if err != nil {
		t.Fatalf("GetUnprocessedTurns: %v", err)
	}
	if len(turns) != 1 || turns[0].ID != "turn-001" {
		t.Errorf("expected 1 unprocessed turn, got %d", len(turns))
	}
}

func TestMarkTurnProcessed(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	turn := &TurnRecord{
		ID:        "turn-002",
		SessionID: "sess-001",
		Role:      "assistant",
		Content:   "Response",
		CreatedAt: time.Now().UnixMilli(),
	}
	s.InsertTurn(ctx, turn)

	if err := s.MarkTurnProcessed(ctx, "turn-002"); err != nil {
		t.Fatalf("MarkTurnProcessed: %v", err)
	}

	turns, err := s.GetUnprocessedTurns(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, t2 := range turns {
		if t2.ID == "turn-002" {
			t.Error("processed turn should not appear in unprocessed list")
		}
	}
}

func TestSetProfile(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.SetProfile(ctx, "timezone", "Australia/Sydney"); err != nil {
		t.Fatalf("SetProfile: %v", err)
	}

	got, err := s.GetProfile(ctx, "timezone")
	if err != nil || got == nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if got.Value != "Australia/Sydney" {
		t.Errorf("expected 'Australia/Sydney', got %q", got.Value)
	}
}

func TestSetProfile_Update(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	s.SetProfile(ctx, "city", "Sydney")
	time.Sleep(time.Millisecond)
	s.SetProfile(ctx, "city", "Melbourne")

	got, err := s.GetProfile(ctx, "city")
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.Value != "Melbourne" {
		t.Errorf("expected Melbourne after update, got %q", got.Value)
	}
}

func TestListProfile(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	s.SetProfile(ctx, "key1", "val1")
	s.SetProfile(ctx, "key2", "val2")
	s.SetProfile(ctx, "key3", "val3")

	entries, err := s.ListProfile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(entries))
	}
}

func TestDeleteProfile(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	s.SetProfile(ctx, "toDelete", "value")
	if err := s.DeleteProfile(ctx, "toDelete"); err != nil {
		t.Fatalf("DeleteProfile: %v", err)
	}
	got, err := s.GetProfile(ctx, "toDelete")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Error("expected nil after delete")
	}
}

func TestSearchFTS(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	facts := []*FactRecord{
		{ID: "fts-001", Content: "User prefers dark mode", Category: "preference", Container: "personal", Importance: 0.7, Confidence: 1.0, CreatedAt: time.Now().UnixMilli(), UpdatedAt: time.Now().UnixMilli()},
		{ID: "fts-002", Content: "User lives in Sydney Australia", Category: "person", Container: "personal", Importance: 0.8, Confidence: 1.0, CreatedAt: time.Now().UnixMilli(), UpdatedAt: time.Now().UnixMilli()},
		{ID: "fts-003", Content: "Project ClawChain uses Go language", Category: "project", Container: "work", Importance: 0.6, Confidence: 1.0, CreatedAt: time.Now().UnixMilli(), UpdatedAt: time.Now().UnixMilli()},
		{ID: "fts-004", Content: "User timezone is AEST", Category: "preference", Container: "personal", Importance: 0.9, Confidence: 1.0, CreatedAt: time.Now().UnixMilli(), UpdatedAt: time.Now().UnixMilli()},
		{ID: "fts-005", Content: "Coffee preferred over tea", Category: "preference", Container: "personal", Importance: 0.3, Confidence: 1.0, CreatedAt: time.Now().UnixMilli(), UpdatedAt: time.Now().UnixMilli()},
	}
	for _, f := range facts {
		if err := s.InsertFact(ctx, f); err != nil {
			t.Fatalf("InsertFact %s: %v", f.ID, err)
		}
	}

	results, err := s.SearchFTS(ctx, "User", 10)
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected some results for 'User' query")
	}
}

func TestSearchFTS_NoResults(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	results, err := s.SearchFTS(ctx, "xyzzy_not_found_abc", 10)
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestSearchFTS_Phrase(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	f := &FactRecord{
		ID:        "phrase-001",
		Content:   "User prefers dark mode interface",
		Category:  "preference",
		Container: "personal",
		Importance: 0.7,
		Confidence: 1.0,
		CreatedAt: time.Now().UnixMilli(),
		UpdatedAt: time.Now().UnixMilli(),
	}
	s.InsertFact(ctx, f)

	results, err := s.SearchFTS(ctx, `"dark mode"`, 10)
	if err != nil {
		t.Fatalf("SearchFTS phrase: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected phrase match result")
	}
}

func TestListDecayable(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Old fact
	old := testFact("decay-old")
	old.CreatedAt = time.Now().Add(-48*time.Hour).UnixMilli()
	old.UpdatedAt = old.CreatedAt
	old.Importance = 0.3
	s.InsertFact(ctx, old)

	// Recent fact
	recent := testFact("decay-recent")
	s.InsertFact(ctx, recent)

	// List facts older than 1 hour
	before := time.Now().Add(-time.Hour).UnixMilli()
	facts, err := s.ListDecayable(ctx, before, 0.0)
	if err != nil {
		t.Fatalf("ListDecayable: %v", err)
	}

	found := false
	for _, f := range facts {
		if f.ID == "decay-old" {
			found = true
		}
		if f.ID == "decay-recent" {
			t.Error("recent fact should not be in decayable list")
		}
	}
	if !found {
		t.Error("old fact should be in decayable list")
	}
}

func TestPruneFacts(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	ids := []string{"prune-001", "prune-002", "prune-003"}
	for _, id := range ids {
		f := testFact(id)
		s.InsertFact(ctx, f)
	}

	count, err := s.PruneFacts(ctx, ids[:2])
	if err != nil {
		t.Fatalf("PruneFacts: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 pruned, got %d", count)
	}

	// Verify they're soft-deleted
	facts, _ := s.ListFacts(ctx, ListFactsOpts{IncludeDeleted: true})
	deletedCount := 0
	for _, f := range facts {
		if f.Deleted {
			deletedCount++
		}
	}
	if deletedCount != 2 {
		t.Errorf("expected 2 deleted facts, got %d", deletedCount)
	}
}

func TestPruneFacts_Empty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	count, err := s.PruneFacts(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("expected 0 pruned for empty ids, got %d", count)
	}
}

func TestStats(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Insert some data
	for i := 0; i < 5; i++ {
		f := testFact(fmt.Sprintf("stats-%03d", i))
		s.InsertFact(ctx, f)
	}
	s.SoftDeleteFact(ctx, "stats-000")
	s.SetProfile(ctx, "k1", "v1")
	s.SetProfile(ctx, "k2", "v2")

	stats, err := s.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.TotalFacts != 5 {
		t.Errorf("expected 5 total facts, got %d", stats.TotalFacts)
	}
	if stats.ActiveFacts != 4 {
		t.Errorf("expected 4 active facts, got %d", stats.ActiveFacts)
	}
	if stats.DeletedFacts != 1 {
		t.Errorf("expected 1 deleted fact, got %d", stats.DeletedFacts)
	}
	if stats.ProfileEntries != 2 {
		t.Errorf("expected 2 profile entries, got %d", stats.ProfileEntries)
	}
}

func TestMigrations(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Run again — should be idempotent
	if err := RunMigrations(s.db); err != nil {
		t.Fatalf("second RunMigrations: %v", err)
	}

	stats, err := s.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.TotalFacts != 0 {
		t.Error("expected empty store after idempotent migration")
	}
}

func TestClose(t *testing.T) {
	f, err := os.CreateTemp("", "close_test_*.db")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	f.Close()
	defer os.Remove(path)

	s, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Subsequent operations should fail
	_, err = s.Stats(context.Background())
	if err == nil {
		t.Error("expected error after close")
	}
}

func TestLastSyncTimestamp(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	ts, err := s.LastSyncTimestamp(ctx)
	if err != nil {
		t.Fatalf("LastSyncTimestamp: %v", err)
	}
	if ts != 0 {
		t.Errorf("expected 0 for never synced, got %d", ts)
	}

	now := time.Now().UnixMilli()
	if err := s.SetLastSyncTimestamp(ctx, now); err != nil {
		t.Fatalf("SetLastSyncTimestamp: %v", err)
	}

	ts2, err := s.LastSyncTimestamp(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ts2 != now {
		t.Errorf("expected %d, got %d", now, ts2)
	}
}

func TestGetFact_NotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	got, err := s.GetFact(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetFact nonexistent: %v", err)
	}
	if got != nil {
		t.Error("expected nil for nonexistent fact")
	}
}

func TestUpdateFact_NotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	f := testFact("no-such-fact")
	if err := s.UpdateFact(ctx, f); err == nil {
		t.Error("expected error for updating nonexistent fact")
	}
}

func TestInsertFact_WithExpiry(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	future := time.Now().Add(time.Hour).UnixMilli()
	f := testFact("exp-001")
	f.ExpiresAt = &future
	if err := s.InsertFact(ctx, f); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetFact(ctx, "exp-001")
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.ExpiresAt == nil || *got.ExpiresAt != future {
		t.Errorf("expires_at not preserved: got %v", got.ExpiresAt)
	}
}

func TestInsertFact_DefaultTimestamps(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	f := &FactRecord{
		ID:         "ts-001",
		Content:    "Timestamp test",
		Category:   "general",
		Container:  "general",
		Importance: 0.5,
		Confidence: 1.0,
		// No CreatedAt/UpdatedAt — should auto-fill
	}

	if err := s.InsertFact(ctx, f); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetFact(ctx, "ts-001")
	if got.CreatedAt == 0 {
		t.Error("expected auto-filled CreatedAt")
	}
}

func TestListFacts_IncludeSuperseded(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	old := testFact("is-old")
	newF := testFact("is-new")
	s.InsertFact(ctx, old)
	s.InsertFact(ctx, newF)
	s.SupersedeFact(ctx, "is-old", "is-new")

	// Default: superseded excluded
	facts, _ := s.ListFacts(ctx, ListFactsOpts{})
	for _, f := range facts {
		if f.ID == "is-old" {
			t.Error("superseded fact should not appear in default list")
		}
	}

	// With IncludeSuperseded: should appear
	facts2, _ := s.ListFacts(ctx, ListFactsOpts{IncludeSuperseded: true, IncludeDeleted: false})
	found := false
	for _, f := range facts2 {
		if f.ID == "is-old" {
			found = true
		}
	}
	if !found {
		t.Error("superseded fact should appear with IncludeSuperseded=true")
	}
}

func TestGetProfile_Missing(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	got, err := s.GetProfile(ctx, "nonexistent-key")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Error("expected nil for missing profile key")
	}
}

func TestInsertTurn_DefaultTimestamp(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	turn := &TurnRecord{
		ID:        "t-ts-001",
		SessionID: "sess",
		Role:      "user",
		Content:   "Hello",
		// No CreatedAt
	}
	if err := s.InsertTurn(ctx, turn); err != nil {
		t.Fatal(err)
	}
	turns, _ := s.GetUnprocessedTurns(ctx, 10)
	if len(turns) == 0 {
		t.Fatal("expected turn")
	}
	if turns[0].CreatedAt == 0 {
		t.Error("expected auto-filled CreatedAt")
	}
}

func TestPruneFacts_AlreadyDeleted(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	f := testFact("prune-dup")
	s.InsertFact(ctx, f)
	s.SoftDeleteFact(ctx, "prune-dup")

	// Pruning again should return 0 (already deleted)
	count, err := s.PruneFacts(ctx, []string{"prune-dup"})
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("expected 0 for already-deleted fact, got %d", count)
	}
}

func TestNewSQLiteStore_CreatesDir(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/nested/dir/test.db"

	s, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore with nested path: %v", err)
	}
	defer func() {
		s.Close()
	}()

	// Verify store works
	stats, err := s.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.TotalFacts != 0 {
		t.Error("expected 0 facts")
	}
}

func TestBoolToInt(t *testing.T) {
	if boolToInt(true) != 1 {
		t.Error("boolToInt(true) should be 1")
	}
	if boolToInt(false) != 0 {
		t.Error("boolToInt(false) should be 0")
	}
}

func TestSupersedeFactRowsAffected(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	f1 := testFact("sup-af-old")
	f2 := testFact("sup-af-new")
	s.InsertFact(ctx, f1)
	s.InsertFact(ctx, f2)

	// Supersede should work without error
	if err := s.SupersedeFact(ctx, "sup-af-old", "sup-af-new"); err != nil {
		t.Fatalf("SupersedeFact: %v", err)
	}

	// Verify relationship
	got, _ := s.GetFact(ctx, "sup-af-old")
	if got == nil || got.SupersededBy == nil {
		t.Error("expected superseded_by to be set")
	}
}

func TestSearchFTS_Limit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Insert 5 facts about "technology"
	for i := 0; i < 5; i++ {
		f := &FactRecord{
			ID:         fmt.Sprintf("tech-%d", i),
			Content:    fmt.Sprintf("technology topic %d discussion", i),
			Category:   "technical",
			Container:  "general",
			Importance: 0.7,
			Confidence: 1.0,
			CreatedAt:  time.Now().UnixMilli(),
			UpdatedAt:  time.Now().UnixMilli(),
		}
		s.InsertFact(ctx, f)
	}

	// Limit to 2
	results, err := s.SearchFTS(ctx, "technology", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) > 2 {
		t.Errorf("expected at most 2 results, got %d", len(results))
	}
}

func TestListFacts_DefaultLimit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Insert 3 facts
	for i := 0; i < 3; i++ {
		f := testFact(fmt.Sprintf("dl-%d", i))
		s.InsertFact(ctx, f)
	}

	// Default limit (0 → 100)
	facts, err := s.ListFacts(ctx, ListFactsOpts{Limit: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 3 {
		t.Errorf("expected 3 facts, got %d", len(facts))
	}
}

func TestGetUnprocessedTurns_DefaultLimit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Insert a turn
	s.InsertTurn(ctx, &TurnRecord{
		ID:        "gup-001",
		SessionID: "sess",
		Role:      "user",
		Content:   "Hello",
		CreatedAt: time.Now().UnixMilli(),
	})

	// Default limit (0 → 100)
	turns, err := s.GetUnprocessedTurns(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 {
		t.Errorf("expected 1 turn, got %d", len(turns))
	}
}

func TestSearchFTS_DefaultLimit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	f := &FactRecord{
		ID:         "fts-def",
		Content:    "default limit test fact",
		Category:   "general",
		Container:  "general",
		Importance: 0.7,
		Confidence: 1.0,
		CreatedAt:  time.Now().UnixMilli(),
		UpdatedAt:  time.Now().UnixMilli(),
	}
	s.InsertFact(ctx, f)

	// Default limit (0 → 10)
	results, err := s.SearchFTS(ctx, "default limit test", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Error("expected results with default limit")
	}
}
