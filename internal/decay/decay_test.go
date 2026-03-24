package decay

import (
	"context"
	"math"
	"os"
	"testing"
	"time"

	"github.com/clawinfra/clawmemory/internal/store"
)

func newTestStore(t *testing.T) store.Store {
	t.Helper()
	f, _ := os.CreateTemp("", "decay_test_*.db")
	path := f.Name()
	f.Close()
	s, err := store.NewSQLiteStore(path)
	if err != nil {
		os.Remove(path)
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s.Close()
		os.Remove(path)
	})
	return s
}

func insertFact(t *testing.T, s store.Store, id string, importance float64, createdAt int64) {
	t.Helper()
	f := &store.FactRecord{
		ID:         id,
		Content:    "Test fact " + id,
		Category:   "general",
		Container:  "general",
		Importance: importance,
		Confidence: 1.0,
		CreatedAt:  createdAt,
		UpdatedAt:  createdAt,
	}
	if err := s.InsertFact(context.Background(), f); err != nil {
		t.Fatalf("insertFact %s: %v", id, err)
	}
}

func TestDecayedImportance(t *testing.T) {
	// At t=0, should return original importance
	imp := DecayedImportance(0.7, 0, 30)
	if math.Abs(imp-0.7) > 1e-10 {
		t.Errorf("age 0: expected 0.7, got %f", imp)
	}

	// At half-life (30 days), should be halved
	imp = DecayedImportance(0.7, 30, 30)
	expected := 0.35
	if math.Abs(imp-expected) > 1e-10 {
		t.Errorf("at half-life: expected %f, got %f", expected, imp)
	}
}

func TestDecayedImportance_Zero(t *testing.T) {
	imp := DecayedImportance(0.8, 0, 30)
	if math.Abs(imp-0.8) > 1e-10 {
		t.Errorf("age 0 should not decay, got %f", imp)
	}
}

func TestDecayedImportance_VeryOld(t *testing.T) {
	// 365 days at 30-day half-life: 2^(-365/30) ≈ 2^(-12.17) ≈ 0.000216
	imp := DecayedImportance(1.0, 365, 30)
	if imp > 0.01 {
		t.Errorf("very old fact should have very low importance, got %f", imp)
	}
}

func TestDecayedImportance_ZeroHalfLife(t *testing.T) {
	imp := DecayedImportance(0.7, 10, 0)
	if imp != 0 {
		t.Errorf("zero half-life should return 0, got %f", imp)
	}
}

func TestRunOnce(t *testing.T) {
	s := newTestStore(t)

	// Insert old fact with low importance (will decay below threshold)
	oldTime := time.Now().Add(-365 * 24 * time.Hour).UnixMilli()
	insertFact(t, s, "old-low", 0.15, oldTime) // will decay to near 0

	// Insert old fact with high importance (won't decay below threshold)
	insertFact(t, s, "old-high", 1.0, oldTime) // will decay to ~0.000216 after 365 days... also pruned

	// Insert recent fact
	insertFact(t, s, "recent", 0.7, time.Now().UnixMilli())

	m := New(s, 30, 0.1, time.Hour)
	pruned, err := m.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	// Both old facts should be pruned (even high importance decays below 0.1 after 1 year)
	if pruned == 0 {
		t.Error("expected some facts to be pruned")
	}

	// Recent fact should remain
	ctx := context.Background()
	facts, _ := s.ListFacts(ctx, store.ListFactsOpts{})
	for _, f := range facts {
		if f.ID == "recent" {
			return // found, test passes
		}
	}
	t.Error("recent fact should not be pruned")
}

func TestRunOnce_TTLExpiry(t *testing.T) {
	s := newTestStore(t)

	// Insert fact with expired TTL
	expiredAt := time.Now().Add(-time.Hour).UnixMilli()
	expiresAt := time.Now().Add(-time.Minute).UnixMilli()
	f := &store.FactRecord{
		ID:         "ttl-expired",
		Content:    "Expired fact",
		Category:   "event",
		Container:  "personal",
		Importance: 0.9, // high importance, but TTL expired
		Confidence: 1.0,
		CreatedAt:  expiredAt,
		UpdatedAt:  expiredAt,
		ExpiresAt:  &expiresAt,
	}
	s.InsertFact(context.Background(), f)

	// Also insert a future TTL fact (should NOT be pruned)
	futureAt := time.Now().Add(time.Hour).UnixMilli()
	f2 := &store.FactRecord{
		ID:         "ttl-future",
		Content:    "Future TTL fact",
		Category:   "event",
		Container:  "personal",
		Importance: 0.9,
		Confidence: 1.0,
		CreatedAt:  time.Now().UnixMilli(),
		UpdatedAt:  time.Now().UnixMilli(),
		ExpiresAt:  &futureAt,
	}
	s.InsertFact(context.Background(), f2)

	m := New(s, 30, 0.1, time.Hour)
	pruned, err := m.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if pruned == 0 {
		t.Error("expected expired TTL fact to be pruned")
	}

	// Check future TTL fact still exists
	ctx := context.Background()
	got, _ := s.GetFact(ctx, "ttl-future")
	if got == nil || got.Deleted {
		t.Error("future TTL fact should not be pruned")
	}
}

func TestRunOnce_HighImportance(t *testing.T) {
	s := newTestStore(t)

	// Insert fact with very high importance, 1 day old — should NOT be pruned
	recentTime := time.Now().Add(-24 * time.Hour).UnixMilli()
	insertFact(t, s, "high-imp", 1.0, recentTime)

	m := New(s, 30, 0.1, time.Hour)
	pruned, err := m.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// importance after 1 day with 30-day half-life: 1.0 * 2^(-1/30) ≈ 0.977 — well above 0.1
	if pruned > 0 {
		t.Errorf("high importance recent fact should not be pruned, but %d were pruned", pruned)
	}
}

func TestRunOnce_NothingToPrune(t *testing.T) {
	s := newTestStore(t)

	// Insert recent high-importance facts
	for i := 0; i < 5; i++ {
		insertFact(t, s, "safe-"+string(rune('a'+i)), 0.9, time.Now().UnixMilli())
	}

	m := New(s, 30, 0.1, time.Hour)
	pruned, err := m.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if pruned != 0 {
		t.Errorf("expected 0 pruned, got %d", pruned)
	}
}

func TestStartStop(t *testing.T) {
	s := newTestStore(t)
	m := New(s, 30, 0.1, 50*time.Millisecond) // very short interval for testing

	m.Start()
	time.Sleep(150 * time.Millisecond) // let it run at least once
	m.Stop() // should not hang

	// Test passes if Stop() returns without deadlock
}
