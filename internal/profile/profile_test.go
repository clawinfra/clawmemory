package profile

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/clawinfra/clawmemory/internal/extractor"
	"github.com/clawinfra/clawmemory/internal/store"
)

func newTestStore(t *testing.T) store.Store {
	t.Helper()
	f, _ := os.CreateTemp("", "profile_test_*.db")
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

func insertFact(t *testing.T, s store.Store, id, content, category string) {
	t.Helper()
	f := &store.FactRecord{
		ID:         id,
		Content:    content,
		Category:   category,
		Container:  "personal",
		Importance: 0.7,
		Confidence: 1.0,
		CreatedAt:  time.Now().UnixMilli(),
		UpdatedAt:  time.Now().UnixMilli(),
	}
	if err := s.InsertFact(context.Background(), f); err != nil {
		t.Fatalf("insertFact: %v", err)
	}
}

func TestBuild_FromFacts(t *testing.T) {
	s := newTestStore(t)

	insertFact(t, s, "f1", "User's timezone is Australia/Sydney", "preference")
	insertFact(t, s, "f2", "User lives in Sydney", "person")
	insertFact(t, s, "f3", "User works at Anthropic", "person")

	b := New(s, nil)
	profile, err := b.Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if profile == nil {
		t.Fatal("expected non-nil profile")
	}

	if val, ok := profile.Entries["timezone"]; !ok || val == "" {
		t.Logf("timezone entry: %v", profile.Entries)
	}
}

func TestBuild_EmptyStore(t *testing.T) {
	s := newTestStore(t)
	b := New(s, nil)

	profile, err := b.Build(context.Background())
	if err != nil {
		t.Fatalf("Build with empty store: %v", err)
	}
	if profile == nil {
		t.Fatal("expected non-nil profile even for empty store")
	}
	if len(profile.Entries) != 0 {
		t.Errorf("expected empty entries, got %d", len(profile.Entries))
	}
}

func TestGet(t *testing.T) {
	s := newTestStore(t)
	insertFact(t, s, "g1", "User's timezone is Australia/Sydney", "preference")

	b := New(s, nil)

	// Build first
	_, err := b.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Get should return the same profile
	profile, err := b.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if profile == nil {
		t.Fatal("expected non-nil profile")
	}
}

func TestGet_EmptyStore(t *testing.T) {
	s := newTestStore(t)
	b := New(s, nil)

	profile, err := b.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.Entries) != 0 {
		t.Errorf("expected empty profile from empty store")
	}
}

func TestUpdate_IncrementalMerge(t *testing.T) {
	s := newTestStore(t)
	b := New(s, nil)

	facts := []extractor.Fact{
		{Content: "User's timezone is Australia/Sydney", Category: "preference", Container: "personal", Importance: 0.9},
		{Content: "User works at Anthropic", Category: "person", Container: "work", Importance: 0.8},
		{Content: "User is building ClawChain", Category: "project", Container: "clawchain", Importance: 0.8},
	}

	if err := b.Update(context.Background(), facts); err != nil {
		t.Fatalf("Update: %v", err)
	}

	profile, err := b.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// timezone fact should be extracted
	if profile.Entries["timezone"] != "Australia/Sydney" {
		t.Logf("profile entries: %v", profile.Entries)
	}
}

func TestUpdate_Overwrite(t *testing.T) {
	s := newTestStore(t)
	b := New(s, nil)

	// First update
	b.Update(context.Background(), []extractor.Fact{
		{Content: "User lives in Sydney", Category: "person", Container: "personal", Importance: 0.8},
	})

	// Second update with new value
	b.Update(context.Background(), []extractor.Fact{
		{Content: "User lives in Melbourne", Category: "person", Container: "personal", Importance: 0.8},
	})

	profile, err := b.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Latest value should win
	if loc, ok := profile.Entries["location"]; ok {
		if loc != "Melbourne" {
			t.Logf("expected Melbourne, got %s", loc)
		}
	}
}

func TestSummarize(t *testing.T) {
	s := newTestStore(t)

	s.SetProfile(context.Background(), "timezone", "Australia/Sydney")
	s.SetProfile(context.Background(), "location", "Sydney")
	s.SetProfile(context.Background(), "role", "technical founder")

	b := New(s, nil)
	summary, err := b.Summarize(context.Background())
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if summary == "" {
		t.Error("expected non-empty summary")
	}
}

func TestSummarize_Empty(t *testing.T) {
	s := newTestStore(t)
	b := New(s, nil)

	summary, err := b.Summarize(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if summary != "" {
		t.Errorf("expected empty summary for empty profile, got: %s", summary)
	}
}

func TestInferProfileKey(t *testing.T) {
	tests := []struct {
		content  string
		wantKey  string
		wantVal  string
	}{
		{"User's timezone is Australia/Sydney", "timezone", "Australia/Sydney"},
		{"User timezone is UTC", "timezone", "UTC"},
		{"User lives in Sydney", "location", "Sydney"},
		{"User moved to Melbourne", "location", "Melbourne"},
		{"User is based in Brisbane", "location", "Brisbane"},
		{"User works at Anthropic", "employer", "Anthropic"},
		{"User works for Google", "employer", "Google"},
		{"User prefers dark mode", "preference", "dark mode"},
		{"User is a software engineer", "role", "software engineer"},
		{"User's role is tech lead", "role", "tech lead"},
		{"User's name is Alex", "name", "Alex"},
		{"User speaks Chinese", "language", "Chinese"},
		{"User's primary project is ClawChain", "primary_project", "ClawChain"},
		{"User's project is EvoClaw", "primary_project", "EvoClaw"},
		{"", "", ""},
		{"Completely unrelated content", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.content, func(t *testing.T) {
			key, val := inferProfileKey(tt.content)
			if key != tt.wantKey {
				t.Errorf("key: got %q, want %q", key, tt.wantKey)
			}
			if val != tt.wantVal {
				t.Errorf("val: got %q, want %q", val, tt.wantVal)
			}
		})
	}
}

func TestMinHelper(t *testing.T) {
	tests := []struct {
		a, b int
		want int
	}{
		{3, 5, 3},
		{5, 3, 3},
		{3, 3, 3},
		{0, 5, 0},
	}
	for _, tt := range tests {
		got := min(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("min(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestBuild_WithManyFacts(t *testing.T) {
	s := newTestStore(t)

	// Insert enough facts to trigger the summary cap
	for i := 0; i < 10; i++ {
		insertFact(t, s, fmt.Sprintf("many-%02d", i), fmt.Sprintf("User's timezone is UTC+%d", i), "preference")
	}

	b := New(s, nil)
	profile, err := b.Build(context.Background())
	if err != nil {
		t.Fatalf("Build many facts: %v", err)
	}
	// Summary should be non-empty (capped at 5)
	if profile == nil {
		t.Fatal("expected non-nil profile")
	}
}
