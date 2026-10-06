// Package migrate defines memory migration between providers. Per the
// installer/migration spec, memories are migrated only when a proven (tested)
// adapter exists for the exact source/target pair. The shipped one is
// engrammcp (Engram export to the GabitoTL Memory MCP server); every other
// pair returns ErrNoTestedMigrator and the official export is kept untouched.
package migrate

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// ErrNoTestedMigrator means no proven migrator is registered for the pair.
var ErrNoTestedMigrator = errors.New("migrate: no tested memory migrator for this provider pair")

// ErrNoBackup means the safety backup the migration requires is missing or empty.
var ErrNoBackup = errors.New("migrate: refusing to run without a safety backup: the backup export must exist and be non-empty")

// Request describes one migration run.
type Request struct {
	ExportPath string // official export of the source (e.g. engram export)
	// BackupPath is the safety export. The migration refuses to run unless it
	// exists, is a regular non-empty file and looks like an export. It may be the
	// same file as ExportPath (the snapshot's export is both the backup and the input).
	BackupPath string
	// ReportPath receives the JSON report, written at the start and updated while
	// the run progresses (the checkpoint a resumed run reports against).
	ReportPath string
	Project    string // only this source project (empty: every project)
	// TargetProject renames the project in the target (requires Project).
	TargetProject string
	// IncludePersonal also migrates personal-scope observations. Off by default:
	// personal notes must not land in memory the whole team can read.
	IncludePersonal bool
	DryRun          bool
}

// Result summarizes a migration.
type Result struct {
	Migrated int
	Skipped  int
	Report   Report
}

// Report is the migration report: counts, what was skipped and why, and what
// was verified. It never contains entry content.
type Report struct {
	Source       string `json:"source"`
	Target       string `json:"target"`
	Status       string `json:"status"` // dry-run, in-progress, complete, interrupted, failed
	DryRun       bool   `json:"dry_run"`
	Resumed      bool   `json:"resumed,omitempty"`
	ExportSHA256 string `json:"export_sha256"`
	BackupPath   string `json:"backup_path"`
	// Observations in the export, what was eligible, and what happened to them.
	Observations     int            `json:"observations"`
	Eligible         int            `json:"eligible"`
	Imported         int            `json:"imported"`
	AlreadyPresent   int            `json:"already_present"`
	Skipped          map[string]int `json:"skipped,omitempty"`
	ByType           map[string]int `json:"by_type,omitempty"`    // eligible observations per ENGRAM type
	ByProject        map[string]int `json:"by_project,omitempty"` // eligible observations per target project
	MappedTypes      map[string]int `json:"mapped_types,omitempty"`
	TopicKeysDropped int            `json:"topic_keys_dropped,omitempty"`
	KnownActors      int            `json:"known_actors"`
	LegacyActors     int            `json:"legacy_actors"`
	Relations        int            `json:"relations,omitempty"`
	Summaries        int            `json:"summaries,omitempty"`
	SummariesDone    int            `json:"summaries_imported,omitempty"`
	Prompts          int            `json:"prompts_not_imported,omitempty"`
	Verified         bool           `json:"verified"`
	SampleChecked    int            `json:"sample_checked"`
	SampleOK         int            `json:"sample_ok"`
	Warnings         []string       `json:"warnings,omitempty"`
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
