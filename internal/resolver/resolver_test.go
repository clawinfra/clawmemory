package resolver

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/clawinfra/clawmemory/internal/search"
	"github.com/clawinfra/clawmemory/internal/store"
)

func newTestStore(t *testing.T) store.Store {
	t.Helper()
	f, _ := os.CreateTemp("", "resolver_test_*.db")
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

func insertFact(t *testing.T, s store.Store, id, content string) *store.FactRecord {
	t.Helper()
	f := &store.FactRecord{
		ID:         id,
		Content:    content,
		Category:   "preference",
		Container:  "personal",
		Importance: 0.7,
		Confidence: 1.0,
		CreatedAt:  time.Now().UnixMilli(),
		UpdatedAt:  time.Now().UnixMilli(),
	}
	if err := s.InsertFact(context.Background(), f); err != nil {
		t.Fatalf("insertFact %s: %v", id, err)
	}
	return f
}

func makeSearcher(s store.Store) *search.Searcher {
	return search.New(s, nil, 0.4, 0.6)
}

func TestCheck_NoContradiction(t *testing.T) {
	s := newTestStore(t)

	// Insert a completely unrelated fact
	insertFact(t, s, "existing-001", "User prefers dark mode")

	searcher := makeSearcher(s)
	res := New(s, searcher)

	newFact := &store.FactRecord{
		ID:      "new-001",
		Content: "Blockchain is decentralized technology",
	}

	contradictions, err := res.Check(context.Background(), newFact)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	// Unrelated fact should not produce contradictions (BM25 won't match)
	t.Logf("Contradictions found: %d", len(contradictions))
}

func TestCheck_SameFactDuplicate(t *testing.T) {
	s := newTestStore(t)

	insertFact(t, s, "dup-001", "User prefers dark mode")

	searcher := makeSearcher(s)
	res := New(s, searcher)

	newFact := &store.FactRecord{
		ID:      "dup-001", // same ID
		Content: "User prefers dark mode",
	}

	contradictions, err := res.Check(context.Background(), newFact)
	if err != nil {
		t.Fatal(err)
	}
	// Same ID should be skipped
	for _, c := range contradictions {
		if c.ExistingFact.ID == "dup-001" && c.NewFact.ID == "dup-001" {
			t.Error("same fact should not be detected as contradiction with itself")
		}
	}
}

func TestResolve_Supersede(t *testing.T) {
	s := newTestStore(t)

	old := insertFact(t, s, "old-001", "User lives in Sydney")
	new_ := insertFact(t, s, "new-003", "User lives in Melbourne")

	searcher := makeSearcher(s)
	res := New(s, searcher)

	c := &Contradiction{
		ExistingFact: old,
		NewFact:      new_,
		Similarity:   0.9,
		Resolution:   "supersede",
	}

	if err := res.Resolve(context.Background(), c); err != nil {
		t.Fatalf("Resolve supersede: %v", err)
	}

	// Check old fact is superseded
	got, err := s.GetFact(context.Background(), "old-001")
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.SupersededBy == nil || *got.SupersededBy != "new-003" {
		t.Errorf("expected superseded_by=new-003, got %v", got.SupersededBy)
	}
	if got.Confidence != 0.3 {
		t.Errorf("expected confidence 0.3, got %f", got.Confidence)
	}
}

func TestResolve_Coexist(t *testing.T) {
	s := newTestStore(t)

	old := insertFact(t, s, "coex-old", "User prefers coffee")
	newF := insertFact(t, s, "coex-new", "User sometimes drinks tea")

	searcher := makeSearcher(s)
	res := New(s, searcher)

	c := &Contradiction{
		ExistingFact: old,
		NewFact:      newF,
		Similarity:   0.7,
		Resolution:   "coexist",
	}

	if err := res.Resolve(context.Background(), c); err != nil {
		t.Fatalf("Resolve coexist: %v", err)
	}

	// Both facts should remain unchanged
	got, err := s.GetFact(context.Background(), "coex-old")
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.SupersededBy != nil {
		t.Error("coexist should not supersede old fact")
	}
}

func TestResolve_DiscardNew(t *testing.T) {
	s := newTestStore(t)

	old := insertFact(t, s, "disc-old", "User likes blue")
	newF := &store.FactRecord{ID: "disc-new", Content: "User likes blue actually"}

	searcher := makeSearcher(s)
	res := New(s, searcher)

	c := &Contradiction{
		ExistingFact: old,
		NewFact:      newF,
		Similarity:   0.9,
		Resolution:   "discard_new",
	}

	err := res.Resolve(context.Background(), c)
	if err == nil {
		t.Error("expected error for discard_new resolution")
	}
}

func TestResolve_UnknownResolution(t *testing.T) {
	s := newTestStore(t)
	searcher := makeSearcher(s)
	res := New(s, searcher)

	c := &Contradiction{
		ExistingFact: &store.FactRecord{ID: "x"},
		NewFact:      &store.FactRecord{ID: "y"},
		Resolution:   "unknown_resolution",
	}
	if err := res.Resolve(context.Background(), c); err == nil {
		t.Error("expected error for unknown resolution")
	}
}

func TestCheck_NilSearcher(t *testing.T) {
	s := newTestStore(t)
	res := New(s, nil) // no searcher

	newFact := &store.FactRecord{
		ID:      "nil-srch",
		Content: "test",
	}

	contradictions, err := res.Check(context.Background(), newFact)
	if err != nil {
		t.Fatal(err)
	}
	if contradictions != nil {
		t.Error("expected nil contradictions when searcher is nil")
	}
}

func TestCheck_SupersededFactIgnored(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Insert old fact
	s.InsertFact(ctx, &store.FactRecord{
		ID:         "sup-old",
		Content:    "User prefers Python language",
		Category:   "preference",
		Container:  "personal",
		Importance: 0.7,
		Confidence: 1.0,
		CreatedAt:  time.Now().UnixMilli(),
		UpdatedAt:  time.Now().UnixMilli(),
	})
	// Insert new fact that supersedes old
	s.InsertFact(ctx, &store.FactRecord{
		ID:         "sup-new",
		Content:    "User prefers Go language",
		Category:   "preference",
		Container:  "personal",
		Importance: 0.7,
		Confidence: 1.0,
		CreatedAt:  time.Now().UnixMilli(),
		UpdatedAt:  time.Now().UnixMilli(),
	})
	// Supersede old
	s.SupersedeFact(ctx, "sup-old", "sup-new")

	searcher := makeSearcher(s)
	res := New(s, searcher)

	newFact := &store.FactRecord{
		ID:      "sup-newer",
		Content: "User now prefers Rust language",
	}

	contradictions, err := res.Check(ctx, newFact)
	if err != nil {
		t.Fatal(err)
	}
	// The superseded fact should be ignored
	for _, c := range contradictions {
		if c.ExistingFact.ID == "sup-old" {
			t.Error("superseded fact should not trigger contradiction")
		}
	}
}

func TestCheck_EmptyStore(t *testing.T) {
	s := newTestStore(t)
	searcher := makeSearcher(s)
	res := New(s, searcher)

	newFact := &store.FactRecord{
		ID:      "empty-store",
		Content: "Test fact",
	}

	contradictions, err := res.Check(context.Background(), newFact)
	if err != nil {
		t.Fatal(err)
	}
	if len(contradictions) != 0 {
		t.Errorf("expected 0 contradictions in empty store, got %d", len(contradictions))
	}
}

func TestNewWithThreshold(t *testing.T) {
	s := newTestStore(t)
	searcher := makeSearcher(s)
	res := NewWithThreshold(s, searcher, 0.75)
	if res == nil {
		t.Fatal("expected non-nil resolver")
	}
	if res.contradictionThreshold != 0.75 {
		t.Errorf("expected threshold 0.75, got %f", res.contradictionThreshold)
	}
}

func TestCheck_ExactDuplicateContent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Insert a fact with identical content
	s.InsertFact(ctx, &store.FactRecord{
		ID:         "dup-content-old",
		Content:    "User prefers dark mode interface settings",
		Category:   "preference",
		Container:  "personal",
		Importance: 0.7,
		Confidence: 1.0,
		CreatedAt:  1000,
		UpdatedAt:  1000,
	})

	searcher := makeSearcher(s)
	res := New(s, searcher)

	// New fact with same content but different ID
	newFact := &store.FactRecord{
		ID:      "dup-content-new",
		Content: "User prefers dark mode interface settings", // exact duplicate content
	}

	contradictions, err := res.Check(ctx, newFact)
	if err != nil {
		t.Fatal(err)
	}
	// Exact same content should NOT be flagged as contradiction
	for _, c := range contradictions {
		if c.ExistingFact.Content == newFact.Content {
			t.Error("exact duplicate content should not be a contradiction")
		}
	}
}

// Unused import protection
var _ = fmt.Sprintf
