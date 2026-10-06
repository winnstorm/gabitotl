// Package engrammcp is the tested Engram to GabitoTL Memory MCP migrator: it
// reads the OFFICIAL `engram export` JSON and imports it into the team memory
// store through the store-level import path (store.Importer).
//
// # Export schema: VERIFIED vs ASSUMPTION
//
// VERIFIED (read from the upstream Go source, Gentleman-Programming/engram
// internal/store/store.go on the main branch, through WebFetch, which returns a
// summary of the page rather than the raw file; the installed engram 2.0.0
// binary was deliberately NOT executed and not inspected):
//
//   - document: {version, exported_at, sessions[], observations[], prompts[],
//     relations[]?, prompt_tombstones[]?}; version "0.2.0" (legacy "0.1.0").
//   - session: id, project, directory, started_at, ended_at?, summary?
//   - observation: id (int), sync_id, session_id, type, title, content,
//     tool_name?, project?, scope, topic_key?, revision_count, duplicate_count,
//     last_seen_at?, review_after?, created_at, updated_at, deleted_at?
//     (soft-deleted observations ARE exported).
//   - prompt: id, sync_id, session_id, content, project?, created_at.
//   - relation: sync_id, source_id, target_id, relation, reason?, evidence?,
//     confidence?, judgment_status, ..., created_at, updated_at.
//   - observations carry NO author field.
//
// ASSUMPTION (not verified; the parser tolerates their absence or variation):
//
//   - relation source_id/target_id are observation sync_ids;
//   - timestamps are RFC 3339 or SQLite "YYYY-MM-DD HH:MM:SS", both in UTC;
//   - observation types are free-form strings (the common ones are mapped,
//     unknown ones become "discovery" and the original is kept in Meta);
//   - scope is "project" or "personal";
//   - a 0.1.0 export may lack sync_id (a content-derived reference is used);
//   - unknown fields are ignored, so a newer export version still parses.
//
// The migrator is covered by hand-built fixtures that follow the VERIFIED part.
package engrammcp

import (
	"encoding/json"
	"fmt"
	"time"
)

// exportDoc is the part of the official export this migrator reads.
type exportDoc struct {
	Version      string            `json:"version"`
	ExportedAt   string            `json:"exported_at"`
	Sessions     []session         `json:"sessions"`
	Observations []observation     `json:"observations"`
	Prompts      []json.RawMessage `json:"prompts"`
	Relations    []relation        `json:"relations"`
}

type session struct {
	ID        string  `json:"id"`
	Project   string  `json:"project"`
	StartedAt string  `json:"started_at"`
	EndedAt   *string `json:"ended_at"`
	Summary   *string `json:"summary"`
}

type observation struct {
	ID        int64   `json:"id"`
	SyncID    string  `json:"sync_id"`
	SessionID string  `json:"session_id"`
	Type      string  `json:"type"`
	Title     string  `json:"title"`
	Content   string  `json:"content"`
	Project   *string `json:"project"`
	Scope     string  `json:"scope"`
	TopicKey  *string `json:"topic_key"`
	Revisions int     `json:"revision_count"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
	DeletedAt *string `json:"deleted_at"`
}

type relation struct {
	SyncID   string `json:"sync_id"`
	SourceID string `json:"source_id"`
	TargetID string `json:"target_id"`
	Relation string `json:"relation"`
	Status   string `json:"judgment_status"`
}

var timeLayouts = []string{
	time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05", "2006-01-02",
}

// parseTime accepts the layouts above; a zone-less value is read as UTC.
func parseTime(v string) (time.Time, error) {
	for _, l := range timeLayouts {
		if t, err := time.ParseInLocation(l, v, time.UTC); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized timestamp %q", v)
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
