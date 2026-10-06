// Package mcp is the Memory MCP provider: a memory.Provider that talks to the
// GabitoTL Memory MCP server (internal/memory/server) through a thin Transport
// (call a named MCP tool with JSON arguments). No PostgreSQL access lives here;
// the real transport (SDKTransport: streamable HTTP with a bearer token, or a
// stdio child process) is built by FromConfig, and Transport stays abstract so
// the provider is testable with FakeTransport.
//
// Tool names and wire contract (JSON) match the server's tools:
//
//	memory_search           args: memory.Query            -> {"entries":[memory.Entry...],"count":n}
//	memory_save             args: memory.Entry            -> {"id":"...","revision":n,"created":bool}
//	memory_update           args: {"id","project","patch":{memory.EntryPatch},"expected_revision"} -> {"id":"...","revision":n}
//	memory_session_summary  args: memory.Summary          -> {"ok":true,"project":"..."}
//	memory_current_project  args: {}                      -> {"project":"...","actor":"..."}
//	memory_doctor           args: {}                      -> {"provider","checks":[...],"ok":bool}
//	memory_active_work      args: ActiveQuery             -> {"entries":[...],"count":n}
//
// The server derives the actor from the credential and rejects a different one,
// so the client sends the actor it believes it is and never needs to be trusted.
//
// The connection is held by REFERENCE only (env:NAME). It is resolved at run
// time by the Transport implementation via ResolveRef and never persisted or
// logged by this package.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/memory"
)

// Name is the provider name in memory.md.
const Name = gabitotl.MemoryMemoryMCP

// Transport calls a named MCP tool with JSON arguments and returns the JSON result.
type Transport interface {
	Call(ctx context.Context, tool string, args any) (json.RawMessage, error)
}

// Tools maps provider operations to MCP tool names.
type Tools struct {
	Search, Save, Update, SessionSummary, CurrentProject, Health, ActiveWork string
}

// DefaultTools is the tool-name mapping of the GabitoTL Memory MCP server.
func DefaultTools() Tools {
	return Tools{
		Search: "memory_search", Save: "memory_save", Update: "memory_update",
		SessionSummary: "memory_session_summary", CurrentProject: "memory_current_project",
		Health: "memory_doctor", ActiveWork: "memory_active_work",
	}
}

func (t Tools) withDefaults() Tools {
	d := DefaultTools()
	pick := func(v, def string) string {
		if v == "" {
			return def
		}
		return v
	}
	return Tools{
		Search: pick(t.Search, d.Search), Save: pick(t.Save, d.Save), Update: pick(t.Update, d.Update),
		SessionSummary: pick(t.SessionSummary, d.SessionSummary),
		CurrentProject: pick(t.CurrentProject, d.CurrentProject), Health: pick(t.Health, d.Health),
		ActiveWork: pick(t.ActiveWork, d.ActiveWork),
	}
}

var envRefRe = regexp.MustCompile(`^env:([A-Za-z_][A-Za-z0-9_]*)$`)

// ResolveRef resolves an env:NAME reference through getenv (os.Getenv when nil).
// The value is returned to the caller only; it must never be persisted.
func ResolveRef(ref string, getenv func(string) string) (string, error) {
	m := envRefRe.FindStringSubmatch(ref)
	if m == nil {
		return "", fmt.Errorf("connection reference %q must be of the form env:NAME", ref)
	}
	if getenv == nil {
		getenv = os.Getenv
	}
	v := getenv(m[1])
	if v == "" {
		return "", fmt.Errorf("environment variable %s is not set", m[1])
	}
	return v, nil
}

// Options configure the provider.
type Options struct {
	Transport     Transport
	Tools         Tools
	ConnectionRef string // env:NAME reference, never a value
	Project       string
	Getenv        func(string) string
}

// Provider implements memory.Provider over a Transport.
type Provider struct {
	opt   Options
	tools Tools
}

var _ memory.Provider = (*Provider)(nil)

// New builds a Provider. It validates that the connection (if any) is a
// reference but does not resolve it.
func New(opt Options) (*Provider, error) {
	if opt.Transport == nil {
		return nil, errors.New("memory-mcp: a transport is required")
	}
	if opt.ConnectionRef != "" && !envRefRe.MatchString(opt.ConnectionRef) {
		return nil, fmt.Errorf("memory-mcp: connection must be an env:NAME reference")
	}
	return &Provider{opt: opt, tools: opt.Tools.withDefaults()}, nil
}

func (p *Provider) call(ctx context.Context, tool string, args, out any) error {
	raw, err := p.opt.Transport.Call(ctx, tool, args)
	if err != nil {
		return fmt.Errorf("memory-mcp %s: %w", tool, err)
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("memory-mcp %s: bad result: %w", tool, err)
	}
	return nil
}

func (p *Provider) project(v string) string {
	if v != "" {
		return v
	}
	return p.opt.Project
}

// Search calls the search tool.
func (p *Provider) Search(ctx context.Context, q memory.Query) ([]memory.Entry, error) {
	q.Project = p.project(q.Project)
	var res struct {
		Entries []memory.Entry `json:"entries"`
	}
	if err := p.call(ctx, p.tools.Search, q, &res); err != nil {
		return nil, err
	}
	if q.Limit > 0 && len(res.Entries) > q.Limit {
		res.Entries = res.Entries[:q.Limit]
	}
	return res.Entries, nil
}

// Save calls the save tool.
func (p *Provider) Save(ctx context.Context, e memory.Entry) (memory.ID, error) {
	e.Project = p.project(e.Project)
	var res struct {
		ID memory.ID `json:"id"`
	}
	if err := p.call(ctx, p.tools.Save, e, &res); err != nil {
		return "", err
	}
	if res.ID == "" {
		return "", fmt.Errorf("memory-mcp %s: result carries no id", p.tools.Save)
	}
	return res.ID, nil
}

// Update calls the update tool.
func (p *Provider) Update(ctx context.Context, id memory.ID, patch memory.EntryPatch) error {
	if strings.TrimSpace(string(id)) == "" {
		return errors.New("memory-mcp: update needs an id")
	}
	args := map[string]any{"id": id, "patch": patch}
	if p.opt.Project != "" {
		args["project"] = p.opt.Project
	}
	return p.call(ctx, p.tools.Update, args, nil)
}

// SessionSummary calls the session-summary tool.
func (p *Provider) SessionSummary(ctx context.Context, s memory.Summary) error {
	s.Project = p.project(s.Project)
	return p.call(ctx, p.tools.SessionSummary, s, nil)
}

// CurrentProject calls the current-project tool, falling back to the key.
func (p *Provider) CurrentProject(ctx context.Context) (string, error) {
	var res struct {
		Project string `json:"project"`
	}
	if err := p.call(ctx, p.tools.CurrentProject, struct{}{}, &res); err != nil {
		if p.opt.Project != "" {
			return p.opt.Project, nil
		}
		return "", err
	}
	if res.Project == "" {
		return p.opt.Project, nil
	}
	return res.Project, nil
}

// ActiveQuery asks which entries were recently updated, by actor and/or task.
type ActiveQuery struct {
	Project      string `json:"project,omitempty"`
	Actor        string `json:"actor,omitempty"`
	Task         string `json:"task,omitempty"`
	SinceMinutes int    `json:"since_minutes,omitempty"`
	Limit        int    `json:"limit,omitempty"`
}

// ActiveWork lists recently updated entries (newest first): who is working on
// what. It backs soft-lock checks; it is not part of memory.Provider.
func (p *Provider) ActiveWork(ctx context.Context, q ActiveQuery) ([]memory.Entry, error) {
	q.Project = p.project(q.Project)
	var res struct {
		Entries []memory.Entry `json:"entries"`
	}
	if err := p.call(ctx, p.tools.ActiveWork, q, &res); err != nil {
		return nil, err
	}
	return res.Entries, nil
}

// Doctor is read-only: it checks that the connection reference resolves (the
// value is never printed) and that the server answers.
func (p *Provider) Doctor(ctx context.Context) memory.Report {
	rep := memory.Report{Provider: Name, Project: p.opt.Project}
	if p.opt.ConnectionRef == "" {
		rep.Checks = append(rep.Checks, memory.Check{Name: "connection", Status: memory.StatusWarn, Message: "no connection reference configured"})
	} else if _, err := ResolveRef(p.opt.ConnectionRef, p.opt.Getenv); err != nil {
		rep.Checks = append(rep.Checks, memory.Check{Name: "connection", Status: memory.StatusError, Message: err.Error()})
	} else {
		rep.Checks = append(rep.Checks, memory.Check{Name: "connection", Status: memory.StatusOK, Message: "reference " + p.opt.ConnectionRef + " resolves"})
	}
	var remote struct {
		Checks []memory.Check `json:"checks"`
	}
	if err := p.call(ctx, p.tools.Health, struct{}{}, &remote); err != nil {
		switch {
		case errors.Is(err, ErrAuth):
			rep.Checks = append(rep.Checks, memory.Check{Name: "authentication", Status: memory.StatusError, Message: err.Error()})
		case errors.Is(err, ErrUnreachable):
			rep.Checks = append(rep.Checks, memory.Check{Name: "server", Status: memory.StatusError, Message: err.Error()})
		case errors.Is(err, ErrSchema):
			rep.Checks = append(rep.Checks, memory.Check{Name: "schema", Status: memory.StatusError, Message: err.Error()})
		default:
			rep.Checks = append(rep.Checks, memory.Check{Name: "server", Status: memory.StatusError, Message: err.Error()})
		}
		return rep
	}
	rep.Checks = append(rep.Checks,
		memory.Check{Name: "server", Status: memory.StatusOK, Message: "server answered"},
		memory.Check{Name: "authentication", Status: memory.StatusOK, Message: "the token was accepted"})
	for _, c := range remote.Checks {
		c.Name = "server " + c.Name
		rep.Checks = append(rep.Checks, c)
	}
	// The project key must exist and the token must be allowed to use it. One
	// read-only search of a single entry tells both.
	if p.opt.Project == "" {
		rep.Checks = append(rep.Checks, memory.Check{Name: "project", Status: memory.StatusWarn, Message: "no project_key in .gabitotl/memory.md"})
		return rep
	}
	if _, err := p.Search(ctx, memory.Query{Text: "", Project: p.opt.Project, Limit: 1}); err != nil {
		st, msg := memory.StatusWarn, err.Error()
		if errors.Is(err, ErrForbidden) {
			st, msg = memory.StatusError, "this token is not allowed to use project "+p.opt.Project+"; ask the server owner for a token that includes it"
		}
		rep.Checks = append(rep.Checks, memory.Check{Name: "project", Status: st, Message: msg})
		return rep
	}
	rep.Checks = append(rep.Checks, memory.Check{Name: "project", Status: memory.StatusOK, Message: "project " + p.opt.Project + " is readable"})
	return rep
}
