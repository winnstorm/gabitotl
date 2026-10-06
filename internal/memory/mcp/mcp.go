// Package mcp is an abstract Memory MCP provider. The concrete Memory MCP
// server (backed by PostgreSQL) is not selected yet, so this package defines
// only a thin Transport (call a named MCP tool with JSON arguments) and
// implements memory.Provider over it. No PostgreSQL access lives here.
//
// Tool mapping is configurable through Tools; the defaults mirror the common
// "mem_*" naming. Wire contract per tool (JSON):
//
//	Search         args: memory.Query            -> {"entries":[memory.Entry...]}
//	Save           args: memory.Entry            -> {"id":"..."}
//	Update         args: {"id":"...","patch":{memory.EntryPatch}} -> {}
//	SessionSummary args: memory.Summary          -> {}
//	CurrentProject args: {}                      -> {"project":"..."}
//	Health         args: {}                      -> {} (optional; empty name skips)
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

// ErrNoTransport means no concrete Memory MCP server has been selected.
var ErrNoTransport = errors.New("memory-mcp: no Memory MCP transport configured (concrete server not selected yet)")

// Transport calls a named MCP tool with JSON arguments and returns the JSON result.
type Transport interface {
	Call(ctx context.Context, tool string, args any) (json.RawMessage, error)
}

// Tools maps provider operations to MCP tool names.
type Tools struct {
	Search, Save, Update, SessionSummary, CurrentProject, Health string
}

// DefaultTools is the default tool-name mapping.
func DefaultTools() Tools {
	return Tools{
		Search: "mem_search", Save: "mem_save", Update: "mem_update",
		SessionSummary: "mem_session_summary", CurrentProject: "mem_current_project",
		Health: "mem_health",
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
		CurrentProject: pick(t.CurrentProject, d.CurrentProject), Health: t.Health,
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
		return nil, ErrNoTransport
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
	return p.call(ctx, p.tools.Update, map[string]any{"id": id, "patch": patch}, nil)
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
	return res.Project, nil
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
	probe, name := p.tools.Health, "health"
	if probe == "" {
		probe, name = p.tools.CurrentProject, "server"
	}
	if err := p.call(ctx, probe, struct{}{}, nil); err != nil {
		rep.Checks = append(rep.Checks, memory.Check{Name: name, Status: memory.StatusError, Message: err.Error()})
	} else {
		rep.Checks = append(rep.Checks, memory.Check{Name: name, Status: memory.StatusOK, Message: "server answered"})
	}
	return rep
}

// FromConfig is the factory constructor. Until a concrete Memory MCP server is
// selected there is no Transport to bind, so it returns ErrNoTransport after
// validating the configuration.
func FromConfig(cfg gabitotl.MemoryConfig) (memory.Provider, error) {
	if cfg.PostgresConnection != "" && !envRefRe.MatchString(cfg.PostgresConnection) {
		return nil, fmt.Errorf("memory-mcp: postgres_connection must be an env:NAME reference")
	}
	return nil, ErrNoTransport
}
