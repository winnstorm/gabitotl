package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/winnstorm/gabitotl/internal/memory"
	"github.com/winnstorm/gabitotl/internal/memory/store"
)

// EntryOut is an entry as returned to clients. Its JSON is a superset of
// memory.Entry, so the client provider decodes it unchanged.
type EntryOut struct {
	ID         string   `json:"id"`
	Type       string   `json:"type"`
	Title      string   `json:"title"`
	Content    string   `json:"content"`
	Project    string   `json:"project"`
	TopicKey   string   `json:"topic_key,omitempty"`
	Actor      string   `json:"actor"`
	Task       string   `json:"task,omitempty"`
	Branch     string   `json:"branch,omitempty"`
	Commit     string   `json:"commit,omitempty"`
	Files      []string `json:"files,omitempty"`
	Validation string   `json:"validation,omitempty"`
	NextStep   string   `json:"next_step,omitempty"`
	Ojo        string   `json:"ojo,omitempty"`
	Timestamp  string   `json:"timestamp,omitempty"`
	Source     string   `json:"source,omitempty"`
	Scope      string   `json:"scope,omitempty"`
	Revision   int      `json:"revision"`
	CreatedBy  string   `json:"created_by,omitempty"`
	CreatedAt  string   `json:"created_at,omitempty"`
	UpdatedAt  string   `json:"updated_at,omitempty"`
	Score      float64  `json:"score,omitempty"`
}

func entryOut(r store.Record) EntryOut {
	ts := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.UTC().Format(time.RFC3339)
	}
	return EntryOut{
		ID: string(r.ID), Type: string(r.Type), Title: r.Title, Content: r.Content, Project: r.Project, TopicKey: r.TopicKey,
		Actor: r.Actor, Task: r.Task, Branch: r.Branch, Commit: r.Commit, Files: r.Files, Validation: r.Validation,
		NextStep: r.NextStep, Ojo: r.Ojo, Timestamp: ts(r.Timestamp), Source: r.Source, Scope: r.Scope, Revision: r.Revision,
		CreatedBy: r.CreatedBy, CreatedAt: ts(r.CreatedAt), UpdatedAt: ts(r.UpdatedAt), Score: r.Score,
	}
}

func entriesOut(rs []store.Record) EntriesOut {
	out := EntriesOut{Entries: make([]EntryOut, 0, len(rs)), Count: len(rs)}
	for _, r := range rs {
		out.Entries = append(out.Entries, entryOut(r))
	}
	return out
}

// EntriesOut is the result of memory_search and memory_active_work.
type EntriesOut struct {
	Entries []EntryOut `json:"entries"`
	Count   int        `json:"count"`
}

// SearchIn is the input of memory_search (a memory.Query).
type SearchIn struct {
	Text     string `json:"text" jsonschema:"search terms; matched by prefix, ranked by relevance (title first)"`
	Project  string `json:"project,omitempty" jsonschema:"project key; searches are always scoped to one project"`
	Type     string `json:"type,omitempty" jsonschema:"restrict to an entry type"`
	Limit    int    `json:"limit,omitempty" jsonschema:"maximum results, default 8, capped at 20"`
	MatchAny bool   `json:"match_any,omitempty" jsonschema:"true: any term matches; false: every term must match"`
}

// SaveIn is the input of memory_save (a memory.Entry).
type SaveIn struct {
	Type       string   `json:"type" jsonschema:"decision, bugfix, discovery, convention, config, preference, pending, risk, progress or project-map"`
	Title      string   `json:"title" jsonschema:"short title"`
	Content    string   `json:"content" jsonschema:"the memory itself"`
	Project    string   `json:"project,omitempty" jsonschema:"project key"`
	TopicKey   string   `json:"topic_key,omitempty" jsonschema:"stable key: saving it again revises the same entry instead of duplicating it"`
	Actor      string   `json:"actor,omitempty" jsonschema:"ignored unless it equals your authenticated user; leave it out"`
	Task       string   `json:"task,omitempty" jsonschema:"task id"`
	Branch     string   `json:"branch,omitempty"`
	Commit     string   `json:"commit,omitempty"`
	Files      []string `json:"files,omitempty"`
	Validation string   `json:"validation,omitempty"`
	NextStep   string   `json:"next_step,omitempty"`
	Ojo        string   `json:"ojo,omitempty"`
	Source     string   `json:"source,omitempty"`
	Timestamp  string   `json:"timestamp,omitempty" jsonschema:"ignored: the server stamps the time"`
}

// SaveOut is the result of memory_save.
type SaveOut struct {
	ID       string `json:"id"`
	Revision int    `json:"revision"`
	Created  bool   `json:"created" jsonschema:"false when an existing entry with the same topic_key was revised"`
}

// PatchIn selects the fields memory_update changes; omitted fields are kept.
type PatchIn struct {
	Title      *string  `json:"title,omitempty"`
	Content    *string  `json:"content,omitempty"`
	Type       *string  `json:"type,omitempty"`
	Task       *string  `json:"task,omitempty"`
	Branch     *string  `json:"branch,omitempty"`
	Commit     *string  `json:"commit,omitempty"`
	Files      []string `json:"files,omitempty"`
	Validation *string  `json:"validation,omitempty"`
	NextStep   *string  `json:"next_step,omitempty"`
	Ojo        *string  `json:"ojo,omitempty"`
}

// UpdateIn is the input of memory_update.
type UpdateIn struct {
	ID               string  `json:"id"`
	Project          string  `json:"project,omitempty"`
	ExpectedRevision int     `json:"expected_revision,omitempty" jsonschema:"optimistic lock: fail if the entry is no longer at this revision"`
	Patch            PatchIn `json:"patch"`
}

// UpdateOut is the result of memory_update.
type UpdateOut struct {
	ID       string `json:"id"`
	Revision int    `json:"revision"`
}

// SummaryIn is the input of memory_session_summary (a memory.Summary).
type SummaryIn struct {
	Project      string   `json:"project,omitempty"`
	Actor        string   `json:"actor,omitempty" jsonschema:"ignored unless it equals your authenticated user; leave it out"`
	Task         string   `json:"task,omitempty"`
	Branch       string   `json:"branch,omitempty"`
	Goal         string   `json:"goal,omitempty"`
	Discoveries  string   `json:"discoveries,omitempty"`
	Accomplished string   `json:"accomplished,omitempty"`
	NextSteps    string   `json:"next_steps,omitempty"`
	Files        []string `json:"files,omitempty"`
	Timestamp    string   `json:"timestamp,omitempty" jsonschema:"ignored: the server stamps the time"`
}

// SummaryOut is the result of memory_session_summary.
type SummaryOut struct {
	OK      bool   `json:"ok"`
	Project string `json:"project"`
}

// CurrentIn is the (empty) input of memory_current_project.
type CurrentIn struct{}

// CurrentOut is the result of memory_current_project.
type CurrentOut struct {
	Project string `json:"project"`
	Actor   string `json:"actor"`
}

// DoctorOut is the result of memory_doctor (a memory.Report).
type DoctorOut struct {
	Provider string     `json:"provider"`
	Project  string     `json:"project,omitempty"`
	Checks   []CheckOut `json:"checks"`
	OK       bool       `json:"ok"`
}

// CheckOut is one diagnostic.
type CheckOut struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// ActiveIn is the input of memory_active_work.
type ActiveIn struct {
	Project      string `json:"project,omitempty"`
	Actor        string `json:"actor,omitempty" jsonschema:"only entries written by this actor"`
	Task         string `json:"task,omitempty" jsonschema:"only entries of this task"`
	SinceMinutes int    `json:"since_minutes,omitempty" jsonschema:"only entries updated in the last N minutes"`
	Limit        int    `json:"limit,omitempty" jsonschema:"maximum results, default 8, capped at 20"`
}

// descriptor returns the MCP tool definition (name, description, hints) shared
// by the server and by the stdio bridge, so the two can never diverge.
func descriptor(name string) *mcp.Tool {
	ro := &mcp.ToolAnnotations{ReadOnlyHint: true}
	rw := func(title string) *mcp.ToolAnnotations {
		f := false
		return &mcp.ToolAnnotations{Title: title, DestructiveHint: &f}
	}
	switch name {
	case ToolSearch:
		return &mcp.Tool{Name: ToolSearch, Description: "Search team memory by relevance. Always scoped to one project; at most 20 results.", Annotations: ro}
	case ToolSave:
		return &mcp.Tool{Name: ToolSave, Description: "Save a memory entry attributed to you. With a topic_key, a live entry with the same key is revised in place.", Annotations: rw("Save memory")}
	case ToolUpdate:
		return &mcp.Tool{Name: ToolUpdate, Description: "Update fields of an existing entry. Pass expected_revision for an optimistic lock.", Annotations: rw("Update memory")}
	case ToolSessionSummary:
		return &mcp.Tool{Name: ToolSessionSummary, Description: "Store the summary that closes a working session.", Annotations: rw("Session summary")}
	case ToolCurrentProject:
		return &mcp.Tool{Name: ToolCurrentProject, Description: "The server's default project (if any) and the authenticated actor.", Annotations: ro}
	case ToolDoctor:
		return &mcp.Tool{Name: ToolDoctor, Description: "Read-only health report of the memory server and its database.", Annotations: ro}
	case ToolActiveWork:
		return &mcp.Tool{Name: ToolActiveWork, Description: "Recently updated entries, newest first, by actor and/or task: who is working on what.", Annotations: ro}
	}
	panic("server: unknown tool " + name)
}

func (s *Server) register() {
	mcp.AddTool(s.mcp, descriptor(ToolSearch), s.search)
	mcp.AddTool(s.mcp, descriptor(ToolSave), s.save)
	mcp.AddTool(s.mcp, descriptor(ToolUpdate), s.update)
	mcp.AddTool(s.mcp, descriptor(ToolSessionSummary), s.sessionSummary)
	mcp.AddTool(s.mcp, descriptor(ToolCurrentProject), s.currentProject)
	mcp.AddTool(s.mcp, descriptor(ToolDoctor), s.doctor)
	mcp.AddTool(s.mcp, descriptor(ToolActiveWork), s.activeWork)
}

func tooLong(name, v string, max int) error {
	if len(v) > max {
		return fmt.Errorf("%s is longer than %d bytes", name, max)
	}
	return nil
}

func checkFiles(files []string) error {
	if len(files) > MaxFiles {
		return fmt.Errorf("more than %d files", MaxFiles)
	}
	for _, f := range files {
		if err := tooLong("file path", f, MaxFileBytes); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) search(ctx context.Context, req *mcp.CallToolRequest, in SearchIn) (*mcp.CallToolResult, EntriesOut, error) {
	if err := tooLong("text", in.Text, MaxQueryBytes); err != nil {
		return nil, EntriesOut{}, err
	}
	project, err := s.project(in.Project)
	if err != nil {
		return nil, EntriesOut{}, err
	}
	if err := s.allow(req, project); err != nil {
		return nil, EntriesOut{}, err
	}
	typ := memory.EntryType(in.Type)
	if typ != "" && !validType(typ) {
		return nil, EntriesOut{}, fmt.Errorf("invalid entry type %q", in.Type)
	}
	limit := in.Limit
	switch {
	case limit <= 0:
		limit = memory.DefaultPerQuery
	case limit > MaxSearchLimit:
		limit = MaxSearchLimit
	}
	ctx, cancel := s.callCtx(ctx)
	defer cancel()
	rs, err := s.cfg.Store.Search(ctx, memory.Query{Text: in.Text, Project: project, Type: typ, Limit: limit, MatchAny: in.MatchAny})
	if err != nil {
		return nil, EntriesOut{}, s.toolErr("search", err)
	}
	return nil, entriesOut(rs), nil
}

func validType(t memory.EntryType) bool {
	for _, v := range memory.EntryTypes {
		if v == t {
			return true
		}
	}
	return false
}

func (s *Server) save(ctx context.Context, req *mcp.CallToolRequest, in SaveIn) (*mcp.CallToolResult, SaveOut, error) {
	actor, err := s.resolveActor(req, in.Actor)
	if err != nil {
		return nil, SaveOut{}, err
	}
	project, err := s.project(in.Project)
	if err != nil {
		return nil, SaveOut{}, err
	}
	if err := s.allow(req, project); err != nil {
		return nil, SaveOut{}, err
	}
	for _, c := range []struct {
		n, v string
		max  int
	}{
		{"title", in.Title, MaxTitleBytes}, {"content", in.Content, MaxContentBytes}, {"topic_key", in.TopicKey, MaxTopicBytes},
		{"task", in.Task, MaxFieldBytes}, {"branch", in.Branch, MaxFieldBytes}, {"commit", in.Commit, MaxFieldBytes},
		{"validation", in.Validation, MaxFieldBytes}, {"next_step", in.NextStep, MaxFieldBytes}, {"ojo", in.Ojo, MaxFieldBytes},
		{"source", in.Source, MaxFieldBytes},
	} {
		if err := tooLong(c.n, c.v, c.max); err != nil {
			return nil, SaveOut{}, err
		}
	}
	if err := checkFiles(in.Files); err != nil {
		return nil, SaveOut{}, err
	}
	e := memory.Entry{
		Type: memory.EntryType(in.Type), Title: in.Title, Content: in.Content, Project: project, TopicKey: strings.TrimSpace(in.TopicKey),
		Actor: actor, Task: in.Task, Branch: in.Branch, Commit: in.Commit, Files: in.Files, Validation: in.Validation,
		NextStep: in.NextStep, Ojo: in.Ojo, Source: in.Source,
	}
	e, _, _, err = memory.Policy{}.Sanitize(e)
	if err != nil {
		return nil, SaveOut{}, err
	}
	if err := e.Validate("team"); err != nil {
		return nil, SaveOut{}, err
	}
	ctx, cancel := s.callCtx(ctx)
	defer cancel()
	res, err := s.cfg.Store.Save(ctx, e)
	if err != nil {
		return nil, SaveOut{}, s.toolErr("save", err)
	}
	return nil, SaveOut{ID: string(res.ID), Revision: res.Revision, Created: res.Created}, nil
}

func (s *Server) update(ctx context.Context, req *mcp.CallToolRequest, in UpdateIn) (*mcp.CallToolResult, UpdateOut, error) {
	actor, err := s.resolveActor(req, "")
	if err != nil {
		return nil, UpdateOut{}, err
	}
	if strings.TrimSpace(in.ID) == "" {
		return nil, UpdateOut{}, errors.New("id is required")
	}
	project := strings.TrimSpace(in.Project)
	if project == "" {
		project = s.cfg.DefaultProject
	}
	if len(project) > MaxProjectBytes {
		return nil, UpdateOut{}, fmt.Errorf("project is longer than %d bytes", MaxProjectBytes)
	}
	if project == "" && !scopeFor(req).all {
		// The store would match an entry in any project: a scoped token must name one.
		return nil, UpdateOut{}, errors.New("project is required for a project-scoped token")
	}
	if project != "" {
		if err := s.allow(req, project); err != nil {
			return nil, UpdateOut{}, err
		}
	}
	p := in.Patch
	for _, c := range []struct {
		n string
		v *string
		m int
	}{
		{"title", p.Title, MaxTitleBytes}, {"content", p.Content, MaxContentBytes}, {"task", p.Task, MaxFieldBytes},
		{"branch", p.Branch, MaxFieldBytes}, {"commit", p.Commit, MaxFieldBytes}, {"validation", p.Validation, MaxFieldBytes},
		{"next_step", p.NextStep, MaxFieldBytes}, {"ojo", p.Ojo, MaxFieldBytes},
	} {
		if c.v != nil {
			if err := tooLong(c.n, *c.v, c.m); err != nil {
				return nil, UpdateOut{}, err
			}
		}
	}
	if err := checkFiles(p.Files); err != nil {
		return nil, UpdateOut{}, err
	}
	patch := memory.EntryPatch{
		Title: p.Title, Content: p.Content, Task: p.Task, Branch: p.Branch, Commit: p.Commit, Files: p.Files,
		Validation: p.Validation, NextStep: p.NextStep, Ojo: p.Ojo,
	}
	if p.Type != nil {
		t := memory.EntryType(*p.Type)
		patch.Type = &t
	}
	patch, _, _, err = memory.Policy{}.SanitizePatch(patch)
	if err != nil {
		return nil, UpdateOut{}, err
	}
	ctx, cancel := s.callCtx(ctx)
	defer cancel()
	rec, err := s.cfg.Store.Update(ctx, project, memory.ID(in.ID), patch, actor, in.ExpectedRevision)
	if err != nil {
		return nil, UpdateOut{}, s.toolErr("update", err)
	}
	return nil, UpdateOut{ID: string(rec.ID), Revision: rec.Revision}, nil
}

func (s *Server) sessionSummary(ctx context.Context, req *mcp.CallToolRequest, in SummaryIn) (*mcp.CallToolResult, SummaryOut, error) {
	actor, err := s.resolveActor(req, in.Actor)
	if err != nil {
		return nil, SummaryOut{}, err
	}
	project, err := s.project(in.Project)
	if err != nil {
		return nil, SummaryOut{}, err
	}
	if err := s.allow(req, project); err != nil {
		return nil, SummaryOut{}, err
	}
	for _, c := range []struct{ n, v string }{
		{"task", in.Task}, {"branch", in.Branch}, {"goal", in.Goal}, {"discoveries", in.Discoveries},
		{"accomplished", in.Accomplished}, {"next_steps", in.NextSteps},
	} {
		if err := tooLong(c.n, c.v, MaxContentBytes); err != nil {
			return nil, SummaryOut{}, err
		}
	}
	if err := checkFiles(in.Files); err != nil {
		return nil, SummaryOut{}, err
	}
	sum := memory.Summary{Project: project, Actor: actor, Task: in.Task, Branch: in.Branch, Goal: in.Goal,
		Discoveries: in.Discoveries, Accomplished: in.Accomplished, NextSteps: in.NextSteps, Files: in.Files}
	sum, _, _, err = memory.Policy{}.SanitizeSummary(sum)
	if err != nil {
		return nil, SummaryOut{}, err
	}
	ctx, cancel := s.callCtx(ctx)
	defer cancel()
	if err := s.cfg.Store.SaveSummary(ctx, sum); err != nil {
		return nil, SummaryOut{}, s.toolErr("summary", err)
	}
	// Also keep the latest summary of each actor searchable as a progress entry.
	// It is a convenience: the summary row above is the record.
	if e := memory.SessionEntry(sum); e.Content != "" {
		e.TopicKey = "session/summary/" + actor
		if _, err := s.cfg.Store.Save(ctx, e); err != nil {
			s.log.Warn("session summary entry not saved", "error", err.Error())
		}
	}
	return nil, SummaryOut{OK: true, Project: project}, nil
}

func (s *Server) currentProject(_ context.Context, req *mcp.CallToolRequest, _ CurrentIn) (*mcp.CallToolResult, CurrentOut, error) {
	project := s.cfg.DefaultProject
	if s.allow(req, project) != nil {
		project = "" // do not reveal a project the token cannot use
	}
	return nil, CurrentOut{Project: project, Actor: s.actorOf(req)}, nil
}

func (s *Server) doctor(ctx context.Context, req *mcp.CallToolRequest, _ CurrentIn) (*mcp.CallToolResult, DoctorOut, error) {
	ctx, cancel := s.callCtx(ctx)
	defer cancel()
	rep := s.Doctor(ctx)
	out := DoctorOut{Provider: rep.Provider, Project: s.cfg.DefaultProject, Checks: make([]CheckOut, 0, len(rep.Checks)), OK: rep.OK()}
	scoped := !scopeFor(req).all
	for _, c := range rep.Checks {
		if scoped && c.Name == "data" {
			continue // aggregate counts span every project
		}
		out.Checks = append(out.Checks, CheckOut{Name: c.Name, Status: c.Status, Message: c.Message})
	}
	if scoped {
		out.Project = ""
	}
	return nil, out, nil
}

func (s *Server) activeWork(ctx context.Context, req *mcp.CallToolRequest, in ActiveIn) (*mcp.CallToolResult, EntriesOut, error) {
	project, err := s.project(in.Project)
	if err != nil {
		return nil, EntriesOut{}, err
	}
	if err := s.allow(req, project); err != nil {
		return nil, EntriesOut{}, err
	}
	q := store.ActiveQuery{Project: project, Actor: strings.TrimSpace(in.Actor), Task: strings.TrimSpace(in.Task), Limit: in.Limit}
	switch {
	case q.Limit <= 0:
		q.Limit = memory.DefaultPerQuery
	case q.Limit > MaxSearchLimit:
		q.Limit = MaxSearchLimit
	}
	if err := tooLong("task", q.Task, MaxFieldBytes); err != nil {
		return nil, EntriesOut{}, err
	}
	if in.SinceMinutes > 0 {
		if in.SinceMinutes > 60*24*365 {
			in.SinceMinutes = 60 * 24 * 365
		}
		q.Since = s.now().Add(-time.Duration(in.SinceMinutes) * time.Minute)
	}
	ctx, cancel := s.callCtx(ctx)
	defer cancel()
	rs, err := s.cfg.Store.Active(ctx, q)
	if err != nil {
		return nil, EntriesOut{}, s.toolErr("active", err)
	}
	return nil, entriesOut(rs), nil
}
