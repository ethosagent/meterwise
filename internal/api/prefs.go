package api

import (
	"context"
	"sync"

	"github.com/ethosagent/meterwise/internal/contract"
)

// PreferenceStore keeps per-user UI preferences. Plan 01 backs it with memory;
// Plan 02 replaces it with a table.
type PreferenceStore interface {
	Get(ctx context.Context, userID string) (contract.Preferences, error)
	Update(ctx context.Context, userID string, patch contract.PreferencesPatch) (contract.Preferences, error)
}

// DefaultPreferences is what a user who never changed anything gets.
func DefaultPreferences() contract.Preferences {
	return contract.Preferences{Theme: contract.ThemeSystem}
}

// MemoryPreferences is the stub-backed PreferenceStore: process-local, lost on restart.
type MemoryPreferences struct {
	mu   sync.Mutex
	byID map[string]contract.Preferences
}

// NewMemoryPreferences returns an empty store.
func NewMemoryPreferences() *MemoryPreferences {
	return &MemoryPreferences{byID: map[string]contract.Preferences{}}
}

// Get returns the user's preferences, or the defaults.
func (m *MemoryPreferences) Get(_ context.Context, userID string) (contract.Preferences, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.byID[userID]; ok {
		return p, nil
	}
	return DefaultPreferences(), nil
}

// Update applies the fields present in patch and returns the result.
func (m *MemoryPreferences) Update(_ context.Context, userID string, patch contract.PreferencesPatch) (contract.Preferences, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.byID[userID]
	if !ok {
		p = DefaultPreferences()
	}
	if patch.Theme != nil {
		p.Theme = *patch.Theme
	}
	if patch.ReducedMotion != nil {
		p.ReducedMotion = *patch.ReducedMotion
	}
	m.byID[userID] = p
	return p, nil
}
