// Package profile provides user profile building from accumulated facts.
// It synthesizes person and preference facts into a structured profile
// with an LLM-generated natural language summary.
package profile

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/clawinfra/clawmemory/internal/extractor"
	"github.com/clawinfra/clawmemory/internal/store"
)

// Profile represents the aggregated user profile.
type Profile struct {
	Entries   map[string]string `json:"entries"`
	Summary   string            `json:"summary"`    // LLM-generated summary
	UpdatedAt int64             `json:"updated_at"`
}

// Builder constructs and maintains the user profile from accumulated facts.
type Builder struct {
	st        store.Store
	extractor *extractor.Extractor
}

// New creates a profile Builder.
func New(s store.Store, ext *extractor.Extractor) *Builder {
	return &Builder{st: s, extractor: ext}
}

// Build scans all active facts categorized as "person" or "preference"
// and synthesizes a user profile. Stores result in profile table.
func (b *Builder) Build(ctx context.Context) (*Profile, error) {
	// Get person facts
	personFacts, err := b.st.ListFacts(ctx, store.ListFactsOpts{
		Category: "person",
		Limit:    100,
	})
	if err != nil {
		return nil, fmt.Errorf("list person facts: %w", err)
	}

	prefFacts, err := b.st.ListFacts(ctx, store.ListFactsOpts{
		Category: "preference",
		Limit:    100,
	})
	if err != nil {
		return nil, fmt.Errorf("list preference facts: %w", err)
	}

	allFacts := append(personFacts, prefFacts...)

	entries := make(map[string]string)
	for _, f := range allFacts {
		// Extract key-value pairs from fact content
		key, value := inferProfileKey(f.Content)
		if key != "" && value != "" {
			entries[key] = value
		}
	}

	// Store each entry
	for k, v := range entries {
		if err := b.st.SetProfile(ctx, k, v); err != nil {
			return nil, fmt.Errorf("set profile entry %s: %w", k, err)
		}
	}

	profile := &Profile{
		Entries:   entries,
		UpdatedAt: time.Now().UnixMilli(),
	}

	// Generate summary
	summary, err := b.generateSummary(ctx, allFacts)
	if err == nil {
		profile.Summary = summary
		_ = b.st.SetProfile(ctx, "_summary", summary)
	}

	return profile, nil
}

// Get returns the current profile from the store.
func (b *Builder) Get(ctx context.Context) (*Profile, error) {
	entries, err := b.st.ListProfile(ctx)
	if err != nil {
		return nil, fmt.Errorf("list profile: %w", err)
	}

	profile := &Profile{
		Entries:   make(map[string]string),
		UpdatedAt: time.Now().UnixMilli(),
	}

	for _, e := range entries {
		if e.Key == "_summary" {
			profile.Summary = e.Value
			if e.UpdatedAt > profile.UpdatedAt {
				profile.UpdatedAt = e.UpdatedAt
			}
		} else {
			profile.Entries[e.Key] = e.Value
		}
	}

	return profile, nil
}

// Update merges new facts into the existing profile incrementally.
// Called after each extraction batch to incrementally update.
func (b *Builder) Update(ctx context.Context, facts []extractor.Fact) error {
	for _, f := range facts {
		if f.Category != "person" && f.Category != "preference" {
			continue
		}
		key, value := inferProfileKey(f.Content)
		if key != "" && value != "" {
			if err := b.st.SetProfile(ctx, key, value); err != nil {
				return fmt.Errorf("update profile entry %s: %w", key, err)
			}
		}
	}
	return nil
}

// Summarize generates a natural-language summary of the profile
// suitable for injection into system prompts.
func (b *Builder) Summarize(ctx context.Context) (string, error) {
	entries, err := b.st.ListProfile(ctx)
	if err != nil {
		return "", fmt.Errorf("list profile for summary: %w", err)
	}

	if len(entries) == 0 {
		return "", nil
	}

	// Build a simple summary from profile entries
	var parts []string
	for _, e := range entries {
		if e.Key == "_summary" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %s", e.Key, e.Value))
	}

	if len(parts) == 0 {
		return "", nil
	}

	summary := "User profile — " + strings.Join(parts, "; ")
	return summary, nil
}

// generateSummary produces a simple summary from facts.
// Uses the extractor if available, otherwise builds a simple text summary.
func (b *Builder) generateSummary(ctx context.Context, facts []*store.FactRecord) (string, error) {
	if len(facts) == 0 {
		return "", nil
	}

	var parts []string
	for _, f := range facts {
		parts = append(parts, f.Content)
	}

	return strings.Join(parts[:min(len(parts), 5)], ". "), nil
}

// inferProfileKey extracts a structured key-value pair from fact content.
// Uses simple heuristics for common patterns.
func inferProfileKey(content string) (key, value string) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", ""
	}

	lc := strings.ToLower(content)

	// Pattern: "User's X is Y" or "User X is Y"
	patterns := []struct {
		prefix  string
		keyName string
	}{
		{"user's timezone is ", "timezone"},
		{"user timezone is ", "timezone"},
		{"user lives in ", "location"},
		{"user moved to ", "location"},
		{"user is based in ", "location"},
		{"user works at ", "employer"},
		{"user works for ", "employer"},
		{"user is a ", "role"},
		{"user's role is ", "role"},
		{"user prefers ", "preference"},
		{"user's name is ", "name"},
		{"user speaks ", "language"},
		{"user's primary project is ", "primary_project"},
		{"user's project is ", "primary_project"},
	}

	for _, p := range patterns {
		if strings.HasPrefix(lc, p.prefix) {
			value := content[len(p.prefix):]
			return p.keyName, value
		}
	}

	// Fall back: use the whole content as a generic entry with a hash-based key
	return "", ""
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
