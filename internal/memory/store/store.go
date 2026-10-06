// Package store is the persistence contract of the GabitoTL Memory MCP server.
// Two implementations exist: pgstore (PostgreSQL, the real one) and memstore
// (in memory, used by unit tests and as the reference the conformance suite
// holds pgstore to).
//
// Rules every implementation enforces:
//   - an actor is required on every write; "legacy" is accepted only because
//     imports need it, and the server never lets a network client choose it;
//   - deletes are soft: a deleted entry disappears from reads and searches but
//     its row is kept;
//   - an entry with a topic_key is an upsert target: saving the same
//     (project, topic_key) again revises the live entry instead of duplicating it;
//   - updates are optimistic: a caller that read revision N can demand N.
package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/memory"
)

// Errors shared by every Store.
var (
	// ErrNotFound means no live entry (or token) matches.
	ErrNotFound = errors.New("store: not found")
	// ErrConflict means the expected revision no longer matches the stored one.
	ErrConflict = errors.New("store: revision conflict")
	// ErrInvalid wraps a rejected input (missing project, actor, bad limits...).
	ErrInvalid = errors.New("store: invalid input")
)

// Limits shared by implementations and the server.
const (
	DefaultLimit = memory.DefaultPerQuery
	MaxLimit     = 50
	// DefaultScope is stamped on entries that do not name a scope.
	DefaultScope = "project"
)

// Record is a stored entry with its bookkeeping fields. Entry.Timestamp is the
// moment the entry was last written.
type Record struct {
	memory.Entry
	Scope     string    `json:"scope,omitempty"`
	Revision  int       `json:"revision"`
	CreatedBy string    `json:"created_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// Score is the relevance of a search hit (higher is better); zero elsewhere.
	Score float64 `json:"score,omitempty"`
	// SourceRef and Meta are set on imported entries (see Importer).
	SourceRef string            `json:"source_ref,omitempty"`
	Meta      map[string]string `json:"meta,omitempty"`
}

// SaveResult describes what Save did.
type SaveResult struct {
	ID       memory.ID
	Revision int
	// Created is false when a live entry with the same topic_key was revised.
	Created bool
}

// ActiveQuery lists recently touched entries, newest first. It backs the
// soft-lock question "who is working on what".
type ActiveQuery struct {
	Project string
	Actor   string
	Task    string
	// Since keeps entries updated at or after it; zero means no lower bound.
	Since time.Time
	Limit int
}

// Stats are cheap counters for diagnostics.
type Stats struct {
	SchemaVersion int
	Observations  int // live
	Deleted       int
	Summaries     int
	Projects      int
	Actors        int
	Tokens        int // not revoked
}

// TokenRecord is a stored API token. Only a hash of the secret is kept.
type TokenRecord struct {
	ID        string
	User      string
	Hash      []byte
	CreatedAt time.Time
	RevokedAt time.Time // zero when active
	// AllProjects is true for a token that may use every project. Tokens created
	// before project scopes existed are all-project tokens. Otherwise Projects is
	// the allowlist (exact project keys).
	AllProjects bool
	Projects    []string
}

// Active reports whether the token can still authenticate.
func (t TokenRecord) Active() bool { return t.RevokedAt.IsZero() }

// Allows reports whether the token may use project.
func (t TokenRecord) Allows(project string) bool {
	if t.AllProjects {
		return true
	}
	for _, p := range t.Projects {
		if p == project {
			return true
		}
	}
	return false
}

// ScopeLabel describes the scope for listings: "all projects" or "projects: a, b".
func (t TokenRecord) ScopeLabel() string {
	if t.AllProjects {
		return "all projects"
	}
	return "projects: " + strings.Join(t.Projects, ", ")
}

// TokenStore persists API tokens (hashed). Both implementations also implement it.
type TokenStore interface {
	CreateToken(ctx context.Context, t TokenRecord) error
	// TokenByID returns ErrNotFound for an unknown id.
	TokenByID(ctx context.Context, id string) (TokenRecord, error)
	// ListTokens returns the tokens of a user (all users when user is empty), oldest first.
	ListTokens(ctx context.Context, user string) ([]TokenRecord, error)
	// RevokeToken revokes an active token of the user; ErrNotFound when none matches.
	RevokeToken(ctx context.Context, user, id string) error
}

// Store is the memory persistence. Implementations are safe for concurrent use.
type Store interface {
	// Save inserts the entry, or revises the live entry with the same
	// (project, topic_key). Project and Actor are required.
	Save(ctx context.Context, e memory.Entry) (SaveResult, error)
	// Get returns a live entry. An empty project matches any project.
	Get(ctx context.Context, project string, id memory.ID) (Record, error)
	// Update applies the patch and bumps the revision. expectRevision 0 skips
	// the optimistic check; otherwise a mismatch returns ErrConflict. The actor
	// becomes the author of the new revision. An empty project matches any.
	Update(ctx context.Context, project string, id memory.ID, patch memory.EntryPatch, actor string, expectRevision int) (Record, error)
	// Delete soft-deletes a live entry.
	Delete(ctx context.Context, project string, id memory.ID, actor string) error
	// Search returns live entries ranked by relevance, newest first on ties,
	// capped by q.Limit (default DefaultLimit, never above MaxLimit).
	Search(ctx context.Context, q memory.Query) ([]Record, error)
	// Active lists recently updated live entries.
	Active(ctx context.Context, q ActiveQuery) ([]Record, error)
	// SaveSummary stores a session summary. Project and Actor are required.
	SaveSummary(ctx context.Context, s memory.Summary) error
	// Summaries returns the newest summaries of a project.
	Summaries(ctx context.Context, project string, limit int) ([]memory.Summary, error)
	// Doctor is read-only and never includes connection strings or secrets.
	Doctor(ctx context.Context) memory.Report
	Stats(ctx context.Context) (Stats, error)
	TokenStore
	Close()
}

// ClampLimit applies the default and the maximum to a requested limit.
func ClampLimit(n int) int {
	switch {
	case n <= 0:
		return DefaultLimit
	case n > MaxLimit:
		return MaxLimit
	}
	return n
}
