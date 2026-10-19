// Package compact plans and applies memory compaction: old operational
// entries (progress) are folded into one monthly digest per project, and exact
// duplicates are dropped. Knowledge entries (decisions, bugfixes, conventions,
// discoveries, ...) are never touched. Originals are soft-deleted, and a JSON
// backup of every affected entry is written before anything changes.
package compact

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/memory"
	"github.com/winnstorm/gabitotl/internal/memory/store"
	"github.com/winnstorm/gabitotl/internal/persona"
)

// DigestTopicPrefix marks digest entries; they are never folded again.
const DigestTopicPrefix = "digest/progress/"

const (
	pageSize     = store.MaxLimit
	previewRunes = 100
	digestRunes  = 3000
)

// DefaultOlderThan is the age after which an operational entry is foldable.
const DefaultOlderThan = 30 * 24 * time.Hour

// ParseAge parses "30d" or "4w" (days and weeks) into a duration.
func ParseAge(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid age %q (want a number and d or w, for example 30d)", s)
	}
	n, err := strconv.Atoi(s[:len(s)-1])
	if err != nil || n < 1 {
		return 0, fmt.Errorf("invalid age %q (want a positive number and d or w, for example 30d)", s)
	}
	switch s[len(s)-1] {
	case 'd':
		return time.Duration(n) * 24 * time.Hour, nil
	case 'w':
		return time.Duration(n) * 7 * 24 * time.Hour, nil
	}
	return 0, fmt.Errorf("invalid age %q (unit must be d or w)", s)
}

// Group is one fold: Records are replaced by Digest (nil for duplicates).
type Group struct {
	Kind    string         `json:"kind"` // "digest" or "duplicate"
	Project string         `json:"project"`
	Month   string         `json:"month,omitempty"`
	Records []store.Record `json:"records"`
	Bytes   int            `json:"bytes"`
	Digest  *memory.Entry  `json:"digest,omitempty"`
}

// Plan is the result of Build.
type Plan struct {
	Cutoff  time.Time `json:"cutoff"`
	Groups  []Group   `json:"groups"`
	Scanned int       `json:"scanned"`
	// Kept counts entries left untouched (knowledge, recent, digests, unique).
	Kept int `json:"kept"`
}

// Folded is the number of entries the plan would soft-delete.
func (p Plan) Folded() (entries, bytes int) {
	for _, g := range p.Groups {
		entries += len(g.Records)
		bytes += g.Bytes
	}
	return
}

func size(r store.Record) int { return len(r.Title) + len(r.Content) }

func dupKey(r store.Record) string {
	n := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	return r.Project + "\x00" + n(r.Title) + "\x00" + n(r.Content)
}

// List returns every live entry of project, newest first, paging through Active.
func List(ctx context.Context, s store.Store, project string) ([]store.Record, error) {
	var all []store.Record
	seen := map[memory.ID]bool{}
	var before time.Time
	for {
		page, err := s.Active(ctx, store.ActiveQuery{Project: project, Before: before, Limit: pageSize})
		if err != nil {
			return nil, err
		}
		fresh := 0
		for _, r := range page {
			if !seen[r.ID] {
				seen[r.ID] = true
				all = append(all, r)
				fresh++
			}
		}
		if len(page) < pageSize || fresh == 0 {
			return all, nil
		}
		// +1us keeps ties on the boundary timestamp; seen drops the repeats.
		before = page[len(page)-1].UpdatedAt.Add(time.Microsecond)
	}
}

// Build plans a compaction over records (any order). Only progress entries are
// candidates; a topic-keyed entry is already versioned by the store (upsert)
// and is never a fold target on its own.
func Build(records []store.Record, now time.Time, olderThan time.Duration) Plan {
	p := Plan{Cutoff: now.Add(-olderThan), Scanned: len(records)}
	recs := append([]store.Record(nil), records...)
	sort.SliceStable(recs, func(i, j int) bool { return recs[i].UpdatedAt.After(recs[j].UpdatedAt) })

	gone := map[memory.ID]bool{}
	// 1. exact duplicates (any age): keep the newest.
	byKey := map[string][]store.Record{}
	var keys []string
	for _, r := range recs {
		if r.Type != memory.TypeProgress || strings.HasPrefix(r.TopicKey, DigestTopicPrefix) {
			continue
		}
		k := dupKey(r)
		if _, ok := byKey[k]; !ok {
			keys = append(keys, k)
		}
		byKey[k] = append(byKey[k], r)
	}
	for _, k := range keys {
		if g := byKey[k]; len(g) > 1 {
			grp := Group{Kind: "duplicate", Project: g[0].Project, Records: g[1:]}
			for _, r := range grp.Records {
				grp.Bytes += size(r)
				gone[r.ID] = true
			}
			p.Groups = append(p.Groups, grp)
		}
	}
	// 2. old progress entries without a topic key, one digest per project+month.
	type pm struct{ project, month string }
	byMonth := map[pm][]store.Record{}
	var order []pm
	for _, r := range recs {
		if r.Type != memory.TypeProgress || gone[r.ID] || r.TopicKey != "" || !r.UpdatedAt.Before(p.Cutoff) {
			continue
		}
		k := pm{r.Project, r.UpdatedAt.UTC().Format("2006-01")}
		if _, ok := byMonth[k]; !ok {
			order = append(order, k)
		}
		byMonth[k] = append(byMonth[k], r)
	}
	for _, k := range order {
		g := byMonth[k]
		if len(g) < 2 {
			continue
		}
		grp := Group{Kind: "digest", Project: k.project, Month: k.month, Records: g}
		for _, r := range g {
			grp.Bytes += size(r)
			gone[r.ID] = true
		}
		d := Digest(k.project, k.month, g)
		grp.Digest = &d
		p.Groups = append(p.Groups, grp)
	}
	p.Kept = len(recs) - len(gone)
	return p
}

// Digest renders the monthly digest: one bullet per original (title and a short
// preview), capped so it never outgrows a memory bundle entry.
func Digest(project, month string, rs []store.Record) memory.Entry {
	sorted := append([]store.Record(nil), rs...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].UpdatedAt.Before(sorted[j].UpdatedAt) })
	var b strings.Builder
	for i, r := range sorted {
		line := "- " + persona.Truncate(r.Title, previewRunes)
		if pv := persona.Truncate(r.Content, previewRunes); pv != "" && pv != strings.TrimSpace(r.Title) {
			line += ": " + pv
		}
		if b.Len()+len(line) > digestRunes {
			fmt.Fprintf(&b, "- ... and %d more (see the compaction backup)\n", len(sorted)-i)
			break
		}
		b.WriteString(line + "\n")
	}
	return memory.Entry{
		Type: memory.TypeProgress, Project: project, TopicKey: DigestTopicPrefix + month,
		Title:   "Progress digest " + month,
		Content: strings.TrimSpace(b.String()),
	}
}

// Backup writes the affected entries to dir/memory-compact-<timestamp>.json
// (mode 0600) and returns its path.
func Backup(dir string, now time.Time, p Plan) (string, error) {
	var recs []store.Record
	for _, g := range p.Groups {
		recs = append(recs, g.Records...)
	}
	data, err := json.MarshalIndent(map[string]any{"created_at": now.UTC(), "cutoff": p.Cutoff, "entries": recs}, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "memory-compact-"+now.UTC().Format("20060102T150405Z")+".json")
	return path, os.WriteFile(path, data, 0o600)
}

// Writer is the part of store.Store that Apply needs.
type Writer interface {
	Save(ctx context.Context, e memory.Entry) (store.SaveResult, error)
	Delete(ctx context.Context, project string, id memory.ID, actor string) error
}

// Apply writes each digest first and then soft-deletes the originals. It stops
// at the first error, so a failure never deletes an entry that has no digest.
func Apply(ctx context.Context, w Writer, p Plan, actor string) (digests, deleted int, err error) {
	for _, g := range p.Groups {
		if g.Digest != nil {
			d := *g.Digest
			d.Actor = actor
			if _, err := w.Save(ctx, d); err != nil {
				return digests, deleted, fmt.Errorf("save digest %s: %w", g.Month, err)
			}
			digests++
		}
		for _, r := range g.Records {
			if err := w.Delete(ctx, r.Project, r.ID, actor); err != nil {
				return digests, deleted, fmt.Errorf("delete #%s: %w", r.ID, err)
			}
			deleted++
		}
	}
	return digests, deleted, nil
}
