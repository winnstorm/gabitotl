package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/memory"
	"github.com/winnstorm/gabitotl/internal/memory/mcp"
	"github.com/winnstorm/gabitotl/internal/memory/server"
)

// errNoMemoryProvider is what a tool call reports when the project the bridge
// runs in has no usable memory configuration.
var errNoMemoryProvider = errors.New("no memory provider configured for this project")

// memoryRouter is the Forwarder of `gabitotl memory mcp` when no --url is given.
// It resolves the provider on every call from the .gabitotl/memory.md of the
// project the process runs in, so ONE runtime registration serves projects
// that use different providers: memory-mcp projects are forwarded to the team
// server, Engram projects are served through the engram CLI. Reading the file
// per call means a provider change takes effect without restarting the agent.
type memoryRouter struct {
	env  Env
	root string // --root, empty: the Git top-level of the working directory
	// tokenRef and project override memory.md for memory-mcp projects (--token-env, --project).
	tokenRef, project string
	insecure          bool

	mu  sync.Mutex
	trs map[string]*mcp.SDKTransport
}

func newMemoryRouter(env Env, root, tokenRef, project string, insecure bool) *memoryRouter {
	return &memoryRouter{env: env, root: root, tokenRef: tokenRef, project: project, insecure: insecure, trs: map[string]*mcp.SDKTransport{}}
}

// Close releases the cached team-server connections.
func (r *memoryRouter) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, t := range r.trs {
		_ = t.Close()
	}
}

// Forward implements server.Forwarder.
func (r *memoryRouter) Forward(ctx context.Context, tool string, args any) (json.RawMessage, error) {
	root, err := resolveRoot(r.env, r.root)
	if err != nil {
		return nil, fmt.Errorf("%w (not inside a GabitoTL project: %v)", errNoMemoryProvider, err)
	}
	cfg, err := gabitotl.LoadMemory(gabitotl.NewLayout(root))
	if err != nil {
		return nil, fmt.Errorf("%w (cannot read .gabitotl/memory.md: %v)", errNoMemoryProvider, err)
	}
	m := map[string]any{}
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	project := cfg.ProjectKey
	if r.project != "" && cfg.Provider == gabitotl.MemoryMemoryMCP {
		project = r.project
	}
	switch cfg.Provider {
	case gabitotl.MemoryMemoryMCP:
		return r.team(ctx, cfg, project, tool, m)
	case gabitotl.MemoryEngram:
		return r.engram(ctx, root, cfg, tool, m)
	}
	return nil, fmt.Errorf("%w (memory.md names provider %q)", errNoMemoryProvider, cfg.Provider)
}

// team forwards one call to the memory-mcp server of the project.
func (r *memoryRouter) team(ctx context.Context, cfg gabitotl.MemoryConfig, project, tool string, m map[string]any) (json.RawMessage, error) {
	tokenRef := cfg.Token
	if r.tokenRef != "" {
		tokenRef = r.tokenRef
	}
	if cfg.URL == "" || tokenRef == "" {
		return nil, fmt.Errorf("%w (memory-mcp needs url and token in .gabitotl/memory.md)", errNoMemoryProvider)
	}
	tr, err := r.transport(cfg.URL, tokenRef)
	if err != nil {
		return nil, err
	}
	if p, _ := m["project"].(string); p == "" && project != "" && tool != server.ToolCurrentProject && tool != server.ToolDoctor {
		m["project"] = project
	}
	out, err := tr.Call(ctx, tool, m)
	if err != nil || tool != server.ToolCurrentProject || project == "" {
		return out, err
	}
	res := map[string]any{}
	if json.Unmarshal(out, &res) != nil {
		return out, nil
	}
	if p, _ := res["project"].(string); p == "" {
		res["project"] = project
	}
	return json.Marshal(res)
}

func (r *memoryRouter) transport(url, tokenRef string) (*mcp.SDKTransport, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := url + "\x00" + tokenRef
	if t, ok := r.trs[key]; ok {
		return t, nil
	}
	renv, err := runtimeEnvFn("")
	if err != nil {
		return nil, err
	}
	t, err := mcp.NewHTTPTransport(mcp.HTTPOptions{URL: url, TokenRef: tokenRef, AllowInsecure: r.insecure,
		Getenv: mcp.Resolver{Getenv: renv.Getenv, Home: renv.Home}.Lookup})
	if err != nil {
		return nil, err
	}
	r.trs[key] = t
	return t, nil
}

// engram serves one tool call for a project that uses Engram, over the engram
// CLI. The CLI has no update and no session summary: update is reported as
// unsupported (save again with the same topic_key), and a summary is stored as
// a progress entry, exactly like memory.Recorder does.
func (r *memoryRouter) engram(ctx context.Context, root string, cfg gabitotl.MemoryConfig, tool string, m map[string]any) (json.RawMessage, error) {
	p, err := newProvider(cfg)
	if err != nil {
		return nil, fmt.Errorf("engram is not available: %w (install it from https://github.com/Gentleman-Programming/engram; GabitoTL never installs it)", err)
	}
	actor := activeID(root)
	ctx, cancel := context.WithTimeout(ctx, memoryTimeout)
	defer cancel()
	project := func(v string) string {
		if v != "" {
			return v
		}
		return cfg.ProjectKey
	}
	decode := func(in any) error {
		raw, err := json.Marshal(m)
		if err != nil {
			return err
		}
		return json.Unmarshal(raw, in)
	}
	switch tool {
	case server.ToolSearch:
		var in server.SearchIn
		if err := decode(&in); err != nil {
			return nil, err
		}
		es, err := p.Search(ctx, memory.Query{Text: in.Text, Project: project(in.Project), Type: memory.EntryType(in.Type), Limit: in.Limit, MatchAny: in.MatchAny})
		if err != nil {
			return nil, err
		}
		if es == nil {
			es = []memory.Entry{}
		}
		return json.Marshal(map[string]any{"entries": es, "count": len(es)})
	case server.ToolSave:
		var in server.SaveIn
		if err := decode(&in); err != nil {
			return nil, err
		}
		e := memory.Entry{Type: memory.EntryType(in.Type), Title: in.Title, Content: in.Content, Project: project(in.Project), TopicKey: in.TopicKey,
			Actor: actor, Task: in.Task, Branch: in.Branch, Commit: in.Commit, Files: in.Files, Validation: in.Validation,
			NextStep: in.NextStep, Ojo: in.Ojo, Source: in.Source}
		id, err := p.Save(ctx, e)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"id": id})
	case server.ToolUpdate:
		var in server.UpdateIn
		if err := decode(&in); err != nil {
			return nil, err
		}
		patch := memory.EntryPatch{Title: in.Patch.Title, Content: in.Patch.Content, Task: in.Patch.Task, Branch: in.Patch.Branch, Commit: in.Patch.Commit,
			Files: in.Patch.Files, Validation: in.Patch.Validation, NextStep: in.Patch.NextStep, Ojo: in.Patch.Ojo}
		if in.Patch.Type != nil {
			t := memory.EntryType(*in.Patch.Type)
			patch.Type = &t
		}
		if err := p.Update(ctx, memory.ID(in.ID), patch); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"id": in.ID})
	case server.ToolSessionSummary:
		var in server.SummaryIn
		if err := decode(&in); err != nil {
			return nil, err
		}
		s := memory.Summary{Project: project(in.Project), Actor: actor, Task: in.Task, Branch: in.Branch, Goal: in.Goal,
			Discoveries: in.Discoveries, Accomplished: in.Accomplished, NextSteps: in.NextSteps, Files: in.Files}
		err := p.SessionSummary(ctx, s)
		if errors.Is(err, memory.ErrUnsupported) {
			e := memory.SessionEntry(s)
			if e.Content == "" {
				return nil, errors.New("the session summary is empty")
			}
			e.TopicKey += "/" + actor
			_, err = p.Save(ctx, e)
		}
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"ok": true, "project": s.Project})
	case server.ToolCurrentProject:
		return json.Marshal(map[string]any{"project": cfg.ProjectKey, "actor": actor})
	case server.ToolDoctor:
		rep := p.Doctor(ctx)
		checks := make([]map[string]string, 0, len(rep.Checks))
		for _, c := range rep.Checks {
			checks = append(checks, map[string]string{"name": c.Name, "status": c.Status, "message": c.Message})
		}
		return json.Marshal(map[string]any{"provider": rep.Provider, "project": rep.Project, "checks": checks, "ok": rep.OK()})
	}
	return nil, fmt.Errorf("%s is only available with team memory (memory-mcp); this project uses engram", tool)
}
