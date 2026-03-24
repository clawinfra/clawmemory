// Package decay provides importance decay and TTL-based pruning for ClawMemory facts.
// Facts that haven't been accessed recently lose importance over time according to
// an exponential half-life model.
package decay

import (
	"context"
	"log"
	"math"
	"time"

	"github.com/clawinfra/clawmemory/internal/store"
)

// Manager handles importance decay and TTL-based pruning of facts.
type Manager struct {
	st            store.Store
	halfLifeDays  float64       // default 30
	minImportance float64       // default 0.1
	interval      time.Duration // how often to run decay
	stopCh        chan struct{}
	stopped       chan struct{}
}

// New creates a decay Manager.
func New(s store.Store, halfLifeDays, minImportance float64, interval time.Duration) *Manager {
	return &Manager{
		st:            s,
		halfLifeDays:  halfLifeDays,
		minImportance: minImportance,
		interval:      interval,
		stopCh:        make(chan struct{}),
		stopped:       make(chan struct{}),
	}
}

// Start begins the background decay loop.
// Every interval:
//  1. Compute decayed importance for all facts
//  2. Any fact where decayed importance < minImportance → auto-prune (soft delete)
//  3. Any fact where expires_at < now → auto-prune
func (m *Manager) Start() {
	go func() {
		defer close(m.stopped)
		ticker := time.NewTicker(m.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				ctx := context.Background()
				pruned, err := m.RunOnce(ctx)
				if err != nil {
					log.Printf("[clawmemory] decay error: %v", err)
				} else if pruned > 0 {
					log.Printf("[clawmemory] decay pruned %d facts", pruned)
				}
			case <-m.stopCh:
				return
			}
		}
	}()
}

// Stop halts the background decay loop.
func (m *Manager) Stop() {
	close(m.stopCh)
	<-m.stopped
}

// RunOnce executes a single decay + prune cycle.
// Returns the number of facts pruned.
func (m *Manager) RunOnce(ctx context.Context) (int, error) {
	now := time.Now()
	nowMillis := now.UnixMilli()

	// List all decayable facts (older than now, or with expiry)
	facts, err := m.st.ListDecayable(ctx, nowMillis, m.minImportance)
	if err != nil {
		return 0, err
	}

	var toPrune []string
	for _, f := range facts {
		// Check TTL expiry
		if f.ExpiresAt != nil && *f.ExpiresAt < nowMillis {
			toPrune = append(toPrune, f.ID)
			continue
		}

		// Compute age in days
		ageMs := nowMillis - f.CreatedAt
		ageDays := float64(ageMs) / (1000 * 60 * 60 * 24)

		// Compute decayed importance
		decayed := DecayedImportance(f.Importance, ageDays, m.halfLifeDays)
		if decayed < m.minImportance {
			toPrune = append(toPrune, f.ID)
		}
	}

	if len(toPrune) == 0 {
		return 0, nil
	}

	return m.st.PruneFacts(ctx, toPrune)
}

// DecayedImportance calculates current importance after time decay.
// Formula: original_importance * 2^(-age_days / half_life_days)
func DecayedImportance(originalImportance float64, ageDays float64, halfLifeDays float64) float64 {
	if halfLifeDays <= 0 {
		return 0
	}
	return originalImportance * math.Pow(2, -ageDays/halfLifeDays)
}
