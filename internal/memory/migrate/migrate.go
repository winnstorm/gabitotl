// Package migrate defines memory migration between providers. Per the
// installer/migration spec, memories are migrated only when a proven (tested)
// adapter exists for the exact source/target pair. None ships in V1: the
// registry returns ErrNoTestedMigrator and the official Engram export is kept
// untouched instead.
package migrate

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// ErrNoTestedMigrator means no proven migrator is registered for the pair.
var ErrNoTestedMigrator = errors.New("migrate: no tested memory migrator for this provider pair")

// Request describes one migration run.
type Request struct {
	ExportPath string // official export of the source (e.g. engram export)
	Project    string
	DryRun     bool
}

// Result summarizes a migration.
type Result struct {
	Migrated int
	Skipped  int
}

// MemoryMigrator moves memories from one provider to another.
type MemoryMigrator interface {
	From() string
	To() string
	Migrate(ctx context.Context, req Request) (Result, error)
}

// Registry maps (from, to) provider pairs to tested migrators.
type Registry struct {
	mu   sync.RWMutex
	byID map[string]MemoryMigrator
}

// NewRegistry returns an empty registry (V1 ships no concrete migrator).
func NewRegistry() *Registry { return &Registry{byID: map[string]MemoryMigrator{}} }

func key(from, to string) string { return from + "->" + to }

// Register adds a migrator; registering the same pair twice is an error.
func (r *Registry) Register(m MemoryMigrator) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(m.From(), m.To())
	if _, dup := r.byID[k]; dup {
		return fmt.Errorf("migrate: migrator %s already registered", k)
	}
	r.byID[k] = m
	return nil
}

// Lookup returns the migrator for the pair, or ErrNoTestedMigrator.
func (r *Registry) Lookup(from, to string) (MemoryMigrator, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if m, ok := r.byID[key(from, to)]; ok {
		return m, nil
	}
	return nil, fmt.Errorf("%w: %s -> %s", ErrNoTestedMigrator, from, to)
}
