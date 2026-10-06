package engrammcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/memory"
	"github.com/winnstorm/gabitotl/internal/memory/engram"
	"github.com/winnstorm/gabitotl/internal/memory/migrate"
	"github.com/winnstorm/gabitotl/internal/memory/store"
)

// Provider names of the pair. To is the provider name of memory.md: the team
// memory server of this repository.
const (
	From = gabitotl.MemoryEngram
	To   = gabitotl.MemoryMemoryMCP
	// SourceLabel is stamped on every imported entry (spec section 54).
	SourceLabel = "engram/gentle-ai"
)

// Skip reasons of the report.
const (
	SkipDeleted  = "deleted"
	SkipPersonal = "personal-scope"
	SkipOther    = "other-project"
	SkipEmpty    = "empty"
	SkipNoProj   = "no-project"
	SkipRejected = "rejected-by-store"
)

const (
	maxExportBytes = 1 << 30
	sampleSize     = 25
	maxWarnings    = 20
	checkpointEach = 200
)

// Opener opens the import side of the team memory store. The returned func
// closes it. It is only called for a real run (never for a dry run).
type Opener func(ctx context.Context) (store.Importer, func(), error)

// Migrator implements migrate.MemoryMigrator for Engram to the team memory.
type Migrator struct {
	Open Opener
	Now  func() time.Time
}

var _ migrate.MemoryMigrator = (*Migrator)(nil)

// New returns the migrator over a store opener.
func New(open Opener) *Migrator { return &Migrator{Open: open} }

// From implements migrate.MemoryMigrator.
func (*Migrator) From() string { return From }

// To implements migrate.MemoryMigrator.
func (*Migrator) To() string { return To }

func (m *Migrator) now() time.Time {
	if m.Now != nil {
		return m.Now().UTC()
	}
	return time.Now().UTC()
}

// typeMap maps Engram's free-form types to ours. Everything not listed and not
// already one of ours becomes "discovery"; the original type is kept in Meta.
var typeMap = map[string]memory.EntryType{
	"architecture": memory.TypeDecision, "pattern": memory.TypeConvention, "learning": memory.TypeDiscovery,
	"manual": memory.TypeDiscovery, "session_summary": memory.TypeProgress, "summary": memory.TypeProgress,
}

func mapType(t string) (memory.EntryType, bool) {
	et := memory.EntryType(strings.ToLower(strings.TrimSpace(t)))
	if slices.Contains(memory.EntryTypes, et) {
		return et, false
	}
	if v, ok := typeMap[string(et)]; ok {
		return v, true
	}
	return memory.TypeDiscovery, true
}

type item struct {
	rec        store.ImportRecord
	engramType string
	wantHash   string // sha256 of the content that must be stored
}

type builder struct {
	rep      *migrate.Report
	req      migrate.Request
	items    []item
	sessions map[string]session
	rels     map[string][]relation
}

func (b *builder) warn(format string, a ...any) {
	if len(b.rep.Warnings) < maxWarnings {
		b.rep.Warnings = append(b.rep.Warnings, fmt.Sprintf(format, a...))
	} else if len(b.rep.Warnings) == maxWarnings {
		b.rep.Warnings = append(b.rep.Warnings, "more warnings omitted")
	}
}

func (b *builder) skip(reason string) {
	if b.rep.Skipped == nil {
		b.rep.Skipped = map[string]int{}
	}
	b.rep.Skipped[reason]++
}

func inc(m *map[string]int, k string) {
	if *m == nil {
		*m = map[string]int{}
	}
	(*m)[k]++
}

func hashStr(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func (b *builder) add(o observation) {
	switch {
	case o.DeletedAt != nil && *o.DeletedAt != "":
		b.skip(SkipDeleted)
		return
	case strings.EqualFold(o.Scope, "personal") && !b.req.IncludePersonal:
		b.skip(SkipPersonal)
		return
	}
	project := str(o.Project)
	if project == "" {
		project = b.sessions[o.SessionID].Project
	}
	switch {
	case project == "":
		b.skip(SkipNoProj)
		return
	case b.req.Project != "" && project != b.req.Project:
		b.skip(SkipOther)
		return
	}
	if b.req.Project != "" && b.req.TargetProject != "" {
		project = b.req.TargetProject
	}
	body, tr, found := engram.ExtractTrailer(o.Content)
	if strings.TrimSpace(o.Title) == "" || strings.TrimSpace(body) == "" {
		b.skip(SkipEmpty)
		return
	}
	et, mapped := mapType(o.Type)
	if mapped {
		inc(&b.rep.MappedTypes, strings.TrimSpace(o.Type)+" -> "+string(et))
	}
	// Actor: the original author when the content says so (entries GabitoTL saved
	// carry an **Actor** trailer); Engram itself records no author, so the rest is
	// "legacy". An actor is never invented.
	actor := memory.ActorLegacy
	if found && tr.Actor != memory.ActorLegacy && gabitotl.ValidID(tr.Actor) {
		actor = tr.Actor
		b.rep.KnownActors++
	} else {
		b.rep.LegacyActors++
	}
	e := memory.Entry{Type: et, Title: o.Title, Content: body, Project: project, TopicKey: strings.TrimSpace(str(o.TopicKey)),
		Actor: actor, Source: SourceLabel}
	if found {
		e.Task, e.Branch, e.Commit, e.Files, e.Validation, e.NextStep, e.Ojo = tr.Task, tr.Branch, tr.Commit, tr.Files, tr.Validation, tr.NextStep, tr.Ojo
	}
	created, cerr := parseTime(o.CreatedAt)
	updated, uerr := parseTime(o.UpdatedAt)
	switch {
	case cerr != nil && uerr != nil:
		b.warn("observation %q: no readable timestamp, the import time is used", o.SyncID)
	case cerr != nil:
		created = updated
	case uerr != nil:
		updated = created
	}
	e.Timestamp = updated
	ref := "engram:" + o.SyncID
	if o.SyncID == "" { // legacy export: a content-derived reference keeps re-runs idempotent
		ref = "engram-content:" + hashStr(project + "\x00" + o.CreatedAt + "\x00" + o.Title + "\x00" + o.Content)[:32]
		b.warn("observation %d has no sync_id: a content-derived reference is used", o.ID)
	}
	meta := map[string]string{"engram_type": strings.TrimSpace(o.Type)}
	if o.SyncID != "" {
		meta["engram_sync_id"] = o.SyncID
	}
	if o.SessionID != "" {
		meta["engram_session"] = o.SessionID
	}
	if o.Scope != "" {
		meta["engram_scope"] = o.Scope
	}
	if o.Revisions > 1 {
		meta["engram_revision_count"] = fmt.Sprint(o.Revisions)
	}
	if rs := b.rels[o.SyncID]; o.SyncID != "" && len(rs) > 0 {
		var parts []string
		for _, r := range rs {
			p := r.Relation + "->engram:" + r.TargetID
			if r.Status != "" {
				p += " [" + r.Status + "]"
			}
			parts = append(parts, p)
		}
		sort.Strings(parts)
		meta["engram_relations"] = strings.Join(parts, "; ")
		b.rep.Relations += len(rs)
	}
	b.rep.Eligible++
	inc(&b.rep.ByType, strings.TrimSpace(o.Type))
	inc(&b.rep.ByProject, project)
	b.items = append(b.items, item{rec: store.ImportRecord{Entry: e, SourceRef: ref, CreatedAt: created, UpdatedAt: updated, Meta: meta},
		engramType: o.Type, wantHash: hashStr(body)})
}

// checkBackup enforces the safety rule: no migration without a safety export.
func checkBackup(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("%w (no backup path given)", migrate.ErrNoBackup)
	}
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() || st.Size() == 0 {
		return "", fmt.Errorf("%w (%s)", migrate.ErrNoBackup, path)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("%w (%s)", migrate.ErrNoBackup, path)
	}
	defer f.Close()
	h := sha256.New()
	var probe map[string]json.RawMessage
	if err := json.NewDecoder(io.TeeReader(io.LimitReader(f, maxExportBytes), h)).Decode(&probe); err != nil {
		return "", fmt.Errorf("%w: %s is not a JSON export", migrate.ErrNoBackup, path)
	}
	if _, ok := probe["observations"]; !ok {
		return "", fmt.Errorf("%w: %s does not look like an engram export", migrate.ErrNoBackup, path)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func readExport(path string) (exportDoc, string, error) {
	f, err := os.Open(path) // read-only: the export is never modified
	if err != nil {
		return exportDoc{}, "", err
	}
	defer f.Close()
	h := sha256.New()
	var doc exportDoc
	if err := json.NewDecoder(io.TeeReader(io.LimitReader(f, maxExportBytes), h)).Decode(&doc); err != nil {
		return exportDoc{}, "", fmt.Errorf("%s is not a readable engram export: %w", path, err)
	}
	if doc.Observations == nil && doc.Sessions == nil {
		return exportDoc{}, "", fmt.Errorf("%s does not look like an engram export (no observations or sessions)", path)
	}
	// Hash the whole file (the decoder may stop before trailing whitespace).
	if _, err := io.Copy(h, io.LimitReader(f, maxExportBytes)); err != nil {
		return exportDoc{}, "", err
	}
	return doc, hex.EncodeToString(h.Sum(nil)), nil
}

// Migrate implements migrate.MemoryMigrator.
func (m *Migrator) Migrate(ctx context.Context, req migrate.Request) (migrate.Result, error) {
	rep := migrate.Report{Source: From, Target: To, DryRun: req.DryRun, BackupPath: req.BackupPath}
	res := migrate.Result{}
	if req.TargetProject != "" && req.Project == "" {
		return res, errors.New("engram migration: --target-project needs --project")
	}
	if _, err := checkBackup(req.BackupPath); err != nil {
		return res, err
	}
	doc, sum, err := readExport(req.ExportPath)
	if err != nil {
		return res, err
	}
	rep.ExportSHA256 = sum
	rep.Observations, rep.Prompts = len(doc.Observations), len(doc.Prompts)
	b := &builder{rep: &rep, req: req, sessions: map[string]session{}, rels: map[string][]relation{}}
	for _, s := range doc.Sessions {
		b.sessions[s.ID] = s
	}
	for _, r := range doc.Relations {
		b.rels[r.SourceID] = append(b.rels[r.SourceID], r)
	}
	for _, o := range doc.Observations {
		b.add(o)
	}
	sums := b.summaries(doc)
	rep.Summaries = len(sums)
	skipped := 0
	for _, n := range rep.Skipped {
		skipped += n
	}
	res.Skipped = skipped

	if req.DryRun {
		rep.Status = "dry-run"
		res.Report = rep
		return res, nil
	}
	if m.Open == nil {
		return res, errors.New("engram migration: no memory store is available (the team server database must be reachable from this machine)")
	}
	imp, closeFn, err := m.Open(ctx)
	if err != nil {
		return res, fmt.Errorf("engram migration: cannot open the team memory store: %w", err)
	}
	defer closeFn()

	if prev, ok := readReport(req.ReportPath); ok && prev.ExportSHA256 == sum && (prev.Status == "in-progress" || prev.Status == "interrupted") {
		rep.Resumed = true
	}
	rep.Status = "in-progress"
	m.saveReport(req.ReportPath, rep)

	for i, it := range b.items {
		if err := ctx.Err(); err != nil {
			return m.interrupted(req, rep, res, err)
		}
		r, err := imp.Import(ctx, it.rec)
		switch {
		case err == nil && r.Created:
			rep.Imported++
			if r.TopicKeyDropped {
				rep.TopicKeysDropped++
			}
		case err == nil:
			rep.AlreadyPresent++
		case errors.Is(err, store.ErrInvalid):
			if rep.Skipped == nil {
				rep.Skipped = map[string]int{}
			}
			rep.Skipped[SkipRejected]++
			res.Skipped++
			b.warn("an observation was rejected by the store (%v)", err)
		default: // the store is unavailable: stop, the run can be resumed
			return m.interrupted(req, rep, res, err)
		}
		if (i+1)%checkpointEach == 0 {
			m.saveReport(req.ReportPath, rep)
		}
	}
	for _, s := range sums {
		if err := ctx.Err(); err != nil {
			return m.interrupted(req, rep, res, err)
		}
		created, err := imp.ImportSummary(ctx, s.ref, s.sum)
		switch {
		case err == nil && created:
			rep.SummariesDone++
		case err == nil, errors.Is(err, store.ErrInvalid):
		default:
			return m.interrupted(req, rep, res, err)
		}
	}
	m.verify(ctx, imp, b, &rep)
	rep.Status = "complete"
	res.Migrated = rep.Imported
	res.Report = rep
	m.saveReport(req.ReportPath, rep)
	return res, nil
}

func (m *Migrator) interrupted(req migrate.Request, rep migrate.Report, res migrate.Result, cause error) (migrate.Result, error) {
	rep.Status = "interrupted"
	res.Migrated, res.Report = rep.Imported, rep
	m.saveReport(req.ReportPath, rep)
	return res, fmt.Errorf("engram migration interrupted (re-run the same command to resume; nothing is duplicated): %w", cause)
}

// verify checks counts and a deterministic sample of content hashes.
func (m *Migrator) verify(ctx context.Context, imp store.Importer, b *builder, rep *migrate.Report) {
	rejected := rep.Skipped[SkipRejected]
	countsOK := rep.Imported+rep.AlreadyPresent == rep.Eligible-rejected
	n := len(b.items)
	var idx []int
	if n <= sampleSize {
		for i := range n {
			idx = append(idx, i)
		}
	} else {
		for k := range sampleSize {
			idx = append(idx, k*(n-1)/(sampleSize-1))
		}
	}
	for _, i := range idx {
		it := b.items[i]
		rep.SampleChecked++
		got, err := imp.BySourceRef(ctx, it.rec.SourceRef)
		if err != nil {
			continue
		}
		if hashStr(got.Content) == it.wantHash && got.Project == it.rec.Entry.Project && got.Type == it.rec.Entry.Type &&
			got.CreatedAt.Equal(it.rec.CreatedAt) && got.Meta["engram_type"] == strings.TrimSpace(it.engramType) {
			rep.SampleOK++
		}
	}
	rep.Verified = countsOK && rep.SampleOK == rep.SampleChecked && rejected == 0
	if !countsOK {
		b.warn("count mismatch: %d imported + %d already present, expected %d", rep.Imported, rep.AlreadyPresent, rep.Eligible-rejected)
	}
}

type summaryItem struct {
	ref string
	sum memory.Summary
}

// summaries turns Engram session summaries into session summaries of the team
// store, attributed to "legacy".
func (b *builder) summaries(doc exportDoc) []summaryItem {
	var out []summaryItem
	for _, s := range doc.Sessions {
		text := strings.TrimSpace(str(s.Summary))
		if text == "" || s.Project == "" || (b.req.Project != "" && s.Project != b.req.Project) {
			continue
		}
		project := s.Project
		if b.req.Project != "" && b.req.TargetProject != "" {
			project = b.req.TargetProject
		}
		ts, err := parseTime(str(s.EndedAt))
		if err != nil {
			ts, _ = parseTime(s.StartedAt)
		}
		out = append(out, summaryItem{ref: "engram-session:" + s.ID, sum: memory.Summary{
			Project: project, Actor: memory.ActorLegacy, Goal: "Engram session " + s.ID, Accomplished: text, Timestamp: ts}})
	}
	return out
}

// readReport loads a previous report, if any.
func readReport(path string) (migrate.Report, bool) {
	if path == "" {
		return migrate.Report{}, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return migrate.Report{}, false
	}
	var r migrate.Report
	if json.Unmarshal(b, &r) != nil {
		return migrate.Report{}, false
	}
	return r, true
}

// saveReport writes the report atomically (mode 0600). It holds counts and
// reasons only, never entry content. A failure to write it never fails the run.
func (m *Migrator) saveReport(path string, rep migrate.Report) {
	if path == "" {
		return
	}
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return
	}
	dir := filepath.Dir(path)
	if os.MkdirAll(dir, 0o700) != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, ".import-report-*")
	if err != nil {
		return
	}
	defer os.Remove(tmp.Name())
	if tmp.Chmod(0o600) != nil {
		tmp.Close()
		return
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return
	}
	if tmp.Close() == nil {
		_ = os.Rename(tmp.Name(), path)
	}
}
