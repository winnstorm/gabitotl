// Package memstore is the in-memory store.Store. It backs the server unit tests
// and is the reference behavior the conformance suite holds pgstore to. Nothing
// is persisted.
package memstore

import (
	"context"
	"crypto/rand"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/winnstorm/gabitotl/internal/memory"
	"github.com/winnstorm/gabitotl/internal/memory/store"
)

// Store is a concurrency-safe in-memory store.Store.
type Store struct {
	// Now is the clock; nil uses time.Now.
	Now func() time.Time

	mu        sync.Mutex
	rows      []*row
	summaries []memory.Summary
	sumRefs   map[string]bool
	tokens    map[string]store.TokenRecord
	order     []string // token ids in creation order
}

type row struct {
	rec     store.Record
	deleted bool
}

var (
	_ store.Store    = (*Store)(nil)
	_ store.Importer = (*Store)(nil)
)

// New returns an empty store.
func New() *Store { return &Store{tokens: map[string]store.TokenRecord{}} }

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func newID() memory.ID {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return memory.ID(fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]))
}

// Close is a no-op.
func (s *Store) Close() {}

// Save implements store.Store.
func (s *Store) Save(_ context.Context, e memory.Entry) (store.SaveResult, error) {
	if err := store.CheckEntry(e); err != nil {
		return store.SaveResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if e.TopicKey != "" {
		for _, r := range s.rows {
			if !r.deleted && r.rec.Project == e.Project && r.rec.TopicKey == e.TopicKey {
				r.rec.Entry = merge(e, r.rec.Entry)
				r.rec.Revision++
				r.rec.UpdatedAt = now
				r.rec.Timestamp = now
				return store.SaveResult{ID: r.rec.ID, Revision: r.rec.Revision}, nil
			}
		}
	}
	e.ID = newID()
	e.Timestamp = now
	e.Files = slices.Clone(e.Files)
	if e.Source == "" {
		e.Source = memory.DefaultSource
	}
	s.rows = append(s.rows, &row{rec: store.Record{
		Entry: e, Scope: store.DefaultScope, Revision: 1, CreatedBy: e.Actor, CreatedAt: now, UpdatedAt: now,
	}})
	return store.SaveResult{ID: e.ID, Revision: 1, Created: true}, nil
}

// merge keeps identity fields of the stored entry and takes the content of the new one.
func merge(in, old memory.Entry) memory.Entry {
	in.ID, in.Timestamp = old.ID, old.Timestamp
	in.Files = slices.Clone(in.Files)
	if in.Source == "" {
		in.Source = old.Source
	}
	return in
}

func (s *Store) live(project string, id memory.ID) *row {
	for _, r := range s.rows {
		if !r.deleted && r.rec.ID == id && (project == "" || r.rec.Project == project) {
			return r
		}
	}
	return nil
}

func clone(r store.Record) store.Record {
	r.Files = slices.Clone(r.Files)
	return r
}

// Get implements store.Store.
func (s *Store) Get(_ context.Context, project string, id memory.ID) (store.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r := s.live(project, id); r != nil {
		return clone(r.rec), nil
	}
	return store.Record{}, store.ErrNotFound
}

// Update implements store.Store.
func (s *Store) Update(_ context.Context, project string, id memory.ID, p memory.EntryPatch, actor string, expect int) (store.Record, error) {
	if strings.TrimSpace(actor) == "" {
		return store.Record{}, fmt.Errorf("%w: %v", store.ErrInvalid, memory.ErrActorRequired)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.live(project, id)
	if r == nil {
		return store.Record{}, store.ErrNotFound
	}
	if expect > 0 && r.rec.Revision != expect {
		return store.Record{}, store.ErrConflict
	}
	next, err := store.ApplyPatch(r.rec.Entry, p)
	if err != nil {
		return store.Record{}, err
	}
	now := s.now()
	next.Actor = actor
	next.Timestamp = now
	r.rec.Entry = next
	r.rec.Revision++
	r.rec.UpdatedAt = now
	return clone(r.rec), nil
}

// Delete implements store.Store.
func (s *Store) Delete(_ context.Context, project string, id memory.ID, actor string) error {
	if strings.TrimSpace(actor) == "" {
		return fmt.Errorf("%w: %v", store.ErrInvalid, memory.ErrActorRequired)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.live(project, id)
	if r == nil {
		return store.ErrNotFound
	}
	r.deleted = true
	r.rec.UpdatedAt = s.now()
	return nil
}

func tokens(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

func hasPrefix(words []string, term string) bool {
	for _, w := range words {
		if strings.HasPrefix(w, term) {
			return true
		}
	}
	return false
}

// score weighs a term by where it matches: title 4, topic key / task 2, body 1.
func score(r store.Record, term string) float64 {
	switch {
	case hasPrefix(tokens(r.Title), term):
		return 4
	case hasPrefix(tokens(r.TopicKey+" "+r.Task), term):
		return 2
	case hasPrefix(tokens(r.Content), term):
		return 1
	}
	return 0
}

// Search implements store.Store.
func (s *Store) Search(_ context.Context, q memory.Query) ([]store.Record, error) {
	terms := store.Terms(q.Text)
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.Record
	for _, r := range s.rows {
		if r.deleted || (q.Project != "" && r.rec.Project != q.Project) || (q.Type != "" && r.rec.Type != q.Type) {
			continue
		}
		rec := clone(r.rec)
		if len(terms) > 0 {
			hit, total := 0, 0.0
			for _, t := range terms {
				if sc := score(rec, t); sc > 0 {
					hit++
					total += sc
				}
			}
			if hit == 0 || (!q.MatchAny && hit < len(terms)) {
				continue
			}
			rec.Score = total
		}
		out = append(out, rec)
	}
	slices.SortStableFunc(out, func(a, b store.Record) int {
		switch {
		case a.Score != b.Score:
			if a.Score > b.Score {
				return -1
			}
			return 1
		case !a.UpdatedAt.Equal(b.UpdatedAt):
			if a.UpdatedAt.After(b.UpdatedAt) {
				return -1
			}
			return 1
		}
		return strings.Compare(string(a.ID), string(b.ID))
	})
	if n := store.ClampLimit(q.Limit); len(out) > n {
		out = out[:n]
	}
	return out, nil
}

// Active implements store.Store.
func (s *Store) Active(_ context.Context, q store.ActiveQuery) ([]store.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.Record
	for _, r := range s.rows {
		if r.deleted || (q.Project != "" && r.rec.Project != q.Project) ||
			(q.Actor != "" && r.rec.Actor != q.Actor) || (q.Task != "" && r.rec.Task != q.Task) ||
			(!q.Since.IsZero() && r.rec.UpdatedAt.Before(q.Since)) {
			continue
		}
		out = append(out, clone(r.rec))
	}
	slices.SortStableFunc(out, func(a, b store.Record) int { return b.UpdatedAt.Compare(a.UpdatedAt) })
	if n := store.ClampLimit(q.Limit); len(out) > n {
		out = out[:n]
	}
	return out, nil
}

// SaveSummary implements store.Store.
func (s *Store) SaveSummary(_ context.Context, sum memory.Summary) error {
	if err := store.CheckSummary(sum); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sum.Timestamp = s.now()
	sum.Files = slices.Clone(sum.Files)
	s.summaries = append(s.summaries, sum)
	return nil
}

// Summaries implements store.Store.
func (s *Store) Summaries(_ context.Context, project string, limit int) ([]memory.Summary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []memory.Summary
	for i := len(s.summaries) - 1; i >= 0; i-- {
		if project == "" || s.summaries[i].Project == project {
			out = append(out, s.summaries[i])
		}
	}
	if n := store.ClampLimit(limit); len(out) > n {
		out = out[:n]
	}
	return out, nil
}

// Doctor implements store.Store.
func (s *Store) Doctor(context.Context) memory.Report {
	return memory.Report{Provider: "memstore", Checks: []memory.Check{{Name: "store", Status: memory.StatusOK, Message: "in-memory store (not persistent)"}}}
}

// Stats implements store.Store.
func (s *Store) Stats(context.Context) (store.Stats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := store.Stats{Summaries: len(s.summaries)}
	projects, actors := map[string]bool{}, map[string]bool{}
	for _, r := range s.rows {
		if r.deleted {
			st.Deleted++
			continue
		}
		st.Observations++
		projects[r.rec.Project] = true
		actors[r.rec.Actor] = true
	}
	st.Projects, st.Actors = len(projects), len(actors)
	for _, t := range s.tokens {
		if t.Active() {
			st.Tokens++
		}
	}
	return st, nil
}

// CreateToken implements store.TokenStore.
func (s *Store) CreateToken(_ context.Context, t store.TokenRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, dup := s.tokens[t.ID]; dup {
		return fmt.Errorf("%w: token id exists", store.ErrInvalid)
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = s.now()
	}
	t.Hash = slices.Clone(t.Hash)
	t.Projects = slices.Clone(t.Projects)
	s.tokens[t.ID] = t
	s.order = append(s.order, t.ID)
	return nil
}

// TokenByID implements store.TokenStore.
func (s *Store) TokenByID(_ context.Context, id string) (store.TokenRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tokens[id]
	if !ok {
		return store.TokenRecord{}, store.ErrNotFound
	}
	t.Hash = slices.Clone(t.Hash)
	t.Projects = slices.Clone(t.Projects)
	return t, nil
}

// ListTokens implements store.TokenStore.
func (s *Store) ListTokens(_ context.Context, user string) ([]store.TokenRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.TokenRecord
	for _, id := range s.order {
		if t := s.tokens[id]; user == "" || t.User == user {
			t.Hash = nil
			t.Projects = slices.Clone(t.Projects)
			out = append(out, t)
		}
	}
	return out, nil
}

// RevokeToken implements store.TokenStore.
func (s *Store) RevokeToken(_ context.Context, user, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tokens[id]
	if !ok || t.User != user || !t.Active() {
		return store.ErrNotFound
	}
	t.RevokedAt = s.now()
	s.tokens[id] = t
	return nil
}

// Import implements store.Importer.
func (s *Store) Import(_ context.Context, rec store.ImportRecord) (store.ImportResult, error) {
	if err := store.CheckImport(rec); err != nil {
		return store.ImportResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.rows { // deleted rows count: an import is never resurrected
		if r.rec.SourceRef == rec.SourceRef {
			return store.ImportResult{ID: r.rec.ID}, nil
		}
	}
	e := rec.Entry
	res := store.ImportResult{Created: true}
	meta := map[string]string{}
	for k, v := range rec.Meta {
		meta[k] = v
	}
	if e.TopicKey != "" {
		for _, r := range s.rows {
			if !r.deleted && r.rec.Project == e.Project && r.rec.TopicKey == e.TopicKey {
				meta["dropped_topic_key"] = e.TopicKey
				e.TopicKey, res.TopicKeyDropped = "", true
				break
			}
		}
	}
	now := s.now()
	created, updated := rec.CreatedAt, rec.UpdatedAt
	if created.IsZero() {
		created = now
	}
	if updated.IsZero() {
		updated = created
	}
	e.ID = newID()
	e.Timestamp = updated.UTC()
	e.Files = slices.Clone(e.Files)
	if e.Source == "" {
		e.Source = memory.DefaultSource
	}
	if len(meta) == 0 {
		meta = nil
	}
	s.rows = append(s.rows, &row{rec: store.Record{
		Entry: e, Scope: store.DefaultScope, Revision: 1, CreatedBy: e.Actor, CreatedAt: created.UTC(), UpdatedAt: updated.UTC(),
		SourceRef: rec.SourceRef, Meta: meta,
	}})
	res.ID = e.ID
	return res, nil
}

// ImportSummary implements store.Importer.
func (s *Store) ImportSummary(_ context.Context, ref string, sum memory.Summary) (bool, error) {
	if err := store.CheckSummaryImport(ref, sum); err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sumRefs == nil {
		s.sumRefs = map[string]bool{}
	}
	if s.sumRefs[ref] {
		return false, nil
	}
	s.sumRefs[ref] = true
	if sum.Timestamp.IsZero() {
		sum.Timestamp = s.now()
	}
	sum.Files = slices.Clone(sum.Files)
	s.summaries = append(s.summaries, sum)
	return true, nil
}

// BySourceRef implements store.Importer.
func (s *Store) BySourceRef(_ context.Context, ref string) (store.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.rows {
		if r.rec.SourceRef == ref && ref != "" {
			return clone(r.rec), nil
		}
	}
	return store.Record{}, store.ErrNotFound
}
