package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/memory"
)

// Import path. History from another system (an Engram export) cannot be saved
// through the MCP tools: those derive the actor from a token and stamp the time,
// and "legacy" can never authenticate. Importers therefore write through the
// store directly, with these guarantees:
//
//   - idempotent: a SourceRef is imported at most once, ever. Re-running never
//     duplicates, never overwrites, and never resurrects an entry deleted since;
//   - faithful: timestamps, actor, type, project and content are kept as given;
//   - non-destructive: an imported entry never revises an existing live entry,
//     so a topic_key that is already taken is dropped from the import (and
//     recorded in Meta) instead of overwriting team memory.

// MaxSourceRefBytes bounds a SourceRef.
const MaxSourceRefBytes = 200

// ImportRecord is one entry to import.
type ImportRecord struct {
	// Entry carries the content. Actor is required ("legacy" when the author is
	// unknown) and Source should name the origin (for example "engram/gentle-ai").
	Entry memory.Entry
	// SourceRef identifies the entry in its origin ("engram:<sync_id>"). It is the
	// idempotency key.
	SourceRef string
	// CreatedAt and UpdatedAt are the original timestamps (zero: the import time).
	CreatedAt, UpdatedAt time.Time
	// Meta keeps origin details that have no column (original type, relations).
	Meta map[string]string
}

// ImportResult describes what Import did.
type ImportResult struct {
	ID memory.ID
	// Created is false when SourceRef had already been imported (nothing changed).
	Created bool
	// TopicKeyDropped is true when the entry's topic_key was already used by a
	// live entry in the project, so it was imported without one.
	TopicKeyDropped bool
}

// Importer is implemented by every Store (memstore and pgstore).
type Importer interface {
	// Import stores rec unless its SourceRef already exists.
	Import(ctx context.Context, rec ImportRecord) (ImportResult, error)
	// ImportSummary stores a session summary unless ref already exists. It
	// returns whether a row was created.
	ImportSummary(ctx context.Context, ref string, s memory.Summary) (bool, error)
	// BySourceRef returns the imported entry (deleted ones included), or ErrNotFound.
	BySourceRef(ctx context.Context, ref string) (Record, error)
}

// CheckImport validates an import record.
func CheckImport(rec ImportRecord) error {
	ref := rec.SourceRef
	switch {
	case strings.TrimSpace(ref) == "" || len(ref) > MaxSourceRefBytes || strings.ContainsRune(ref, 0):
		return fmt.Errorf("%w: a source reference of 1 to %d bytes is required", ErrInvalid, MaxSourceRefBytes)
	}
	return CheckEntry(rec.Entry)
}

// CheckSummaryImport validates a summary import.
func CheckSummaryImport(ref string, s memory.Summary) error {
	if strings.TrimSpace(ref) == "" || len(ref) > MaxSourceRefBytes || strings.ContainsRune(ref, 0) {
		return fmt.Errorf("%w: a source reference of 1 to %d bytes is required", ErrInvalid, MaxSourceRefBytes)
	}
	return CheckSummary(s)
}
