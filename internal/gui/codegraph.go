package gui

import (
	"context"
	"errors"

	"github.com/winnstorm/gabitotl/internal/codegraph"
	"github.com/winnstorm/gabitotl/internal/installer"
	"github.com/winnstorm/gabitotl/internal/installer/ui"
	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

// CodeGraphAgent says whether one agent has the CodeGraph MCP server.
type CodeGraphAgent struct {
	Runtime    string `json:"runtime"`
	Label      string `json:"label"`
	Registered bool   `json:"registered"`
}

// CodeGraphStatus is the required CodeGraph component as the Manager shows it.
type CodeGraphStatus struct {
	Supported bool             `json:"supported"` // a managed install exists for this OS
	Installed bool             `json:"installed"`
	Version   string           `json:"version,omitempty"`
	Path      string           `json:"path,omitempty"`
	Pinned    string           `json:"pinned"`
	OnPath    bool             `json:"onPath"`
	Shadow    string           `json:"shadow,omitempty"` // another codegraph ahead on PATH (never modified)
	NeedsWork bool             `json:"needsWork"`        // missing, older than pinned, or an agent lacks the MCP server
	Hint      string           `json:"hint,omitempty"`   // how to install it where there is no managed install
	Agents    []CodeGraphAgent `json:"agents"`
}

func (s *Service) codegraph() *codegraph.Installer {
	if s.d.CodeGraph != nil {
		return s.d.CodeGraph
	}
	return codegraph.Default()
}

// codegraphStep is the wizard plan entry of the required component.
func (s *Service) codegraphStep(m ui.Messages) *PlanStep {
	cg := s.codegraph()
	if !cg.Supported() {
		return nil
	}
	pl := cg.Plan(context.Background())
	return &PlanStep{ID: "codegraph", Title: m.Format("gui.step.codegraph-"+string(pl.Action), "version", codegraph.Version, "installed", pl.Installed)}
}

// applyCodeGraph installs or upgrades CodeGraph, reporting each step between
// the from and to percent.
func (s *Service) applyCodeGraph(ctx context.Context, m ui.Messages, p Progress, from, to int) StepResult {
	n := 0
	res, err := s.codegraph().Install(ctx, func(id string) {
		p.emit("codegraph."+id, "running", m.Format("gui.codegraph."+id), from+(to-from)*n/5)
		n++
	})
	switch {
	case errors.Is(err, codegraph.ErrUnsupported):
		return StepResult{ID: "codegraph", Status: StatusUnsupported}
	case err != nil:
		p.emit("codegraph", "error", err.Error(), to)
		return StepResult{ID: "codegraph", Status: StatusError, Message: err.Error()}
	case res.Action == codegraph.ActionSkip && n == 0:
		p.emit("codegraph", "done", m.Format("gui.step.codegraph-skip", "version", res.Version), to)
		return StepResult{ID: "codegraph", Status: StatusSkipped}
	}
	p.emit("codegraph", "done", m.Format("gui.step.codegraph-done", "version", res.Version), to)
	return StepResult{ID: "codegraph", Status: StatusOK}
}

// CodeGraph reports the installed CodeGraph and which agents can start it.
func (s *Service) CodeGraph(ctx context.Context) (CodeGraphStatus, error) {
	st := s.codegraph().Status(ctx)
	out := CodeGraphStatus{
		Supported: st.Supported, Installed: st.Installed, Version: st.Version, Path: st.Path, Pinned: st.Pinned,
		OnPath: st.OnPath, Shadow: st.Shadow, Agents: []CodeGraphAgent{},
	}
	if !st.Supported {
		out.Hint = codegraph.Hint
	}
	out.NeedsWork = st.Supported && (!st.Installed || codegraph.Compare(st.Version, st.Pinned) < 0 || !st.OnPath)
	env, err := s.env("")
	if err != nil {
		return out, err
	}
	inv := installer.BuildInventory(ctx, env, nil)
	stored, _ := env.RT().Store().Load()
	for _, r := range inv.Runtimes {
		if len(stored.ForRuntime(r.Name)) == 0 {
			continue // GabitoTL does not manage this agent
		}
		rc, err := s.runtimeCtx(IntegrationRequest{Runtime: r.Name})
		if err != nil {
			continue
		}
		reg := s.codegraphRegistered(rc)
		out.Agents = append(out.Agents, CodeGraphAgent{Runtime: r.Name, Label: r.Label, Registered: reg})
		out.NeedsWork = out.NeedsWork || (st.Supported && !reg)
	}
	return out, nil
}

// codegraphActions plans only the CodeGraph server entry of one agent.
func (s *Service) codegraphActions(rc rtCtx) ([]rt.Action, error) {
	rc.opts.Tools = installer.CodeGraphTools(rc.renv, true)
	acts, err := rc.a.Plan(rc.renv, rc.opts)
	if err != nil {
		return nil, wrap(rc.a.Name(), err)
	}
	var out []rt.Action
	for _, a := range acts {
		if a.Edit.Op == rt.OpSetKey && len(a.Edit.Key) > 0 && a.Edit.Key[len(a.Edit.Key)-1] == rt.EntryCodeGraph || a.Target == "server "+rt.EntryCodeGraph {
			out = append(out, a)
		}
	}
	return out, nil
}

func (s *Service) codegraphRegistered(rc rtCtx) bool {
	acts, err := s.codegraphActions(rc)
	if err != nil || len(acts) == 0 {
		return false
	}
	return !mutating(acts)
}

// PlanCodeGraph shows what repairing or upgrading CodeGraph would do: the
// component itself and the MCP entry of each agent GabitoTL manages.
func (s *Service) PlanCodeGraph(ctx context.Context) (Plan, error) {
	cg := s.codegraph()
	if !cg.Supported() {
		return seal(Plan{Groups: []PlanGroup{}, Notes: []Note{}, Steps: []PlanStep{}, Unsupports: []string{"codegraph"}}), nil
	}
	env, err := s.env("")
	if err != nil {
		return Plan{}, err
	}
	m := msgs(env, "")
	p := Plan{Groups: []PlanGroup{}, Notes: []Note{}, Steps: []PlanStep{}, Supported: true}
	if cgp := cg.Plan(ctx); cgp.Action != codegraph.ActionSkip || len(cgp.Steps) > 0 {
		p.Steps = append(p.Steps, PlanStep{ID: "codegraph", Title: m.Format("gui.step.codegraph-"+string(cgp.Action), "version", codegraph.Version, "installed", cgp.Installed)})
		p.Mutates = true
	}
	inv := installer.BuildInventory(ctx, env, nil)
	stored, _ := env.RT().Store().Load()
	for _, r := range inv.Runtimes {
		if len(stored.ForRuntime(r.Name)) == 0 {
			continue
		}
		rc, err := s.runtimeCtx(IntegrationRequest{Runtime: r.Name})
		if err != nil {
			return Plan{}, err
		}
		acts, err := s.codegraphActions(rc)
		if err != nil {
			return Plan{}, err
		}
		p.Groups = append(p.Groups, actionGroups(acts)...)
		p.Mutates = p.Mutates || mutating(acts)
	}
	return seal(p), nil
}

// ApplyCodeGraph runs the plan PlanCodeGraph returned: it installs or upgrades
// CodeGraph, then adds its MCP server to each managed agent through the runtime
// adapters (backup first, idempotent). It never runs `codegraph install`.
func (s *Service) ApplyCodeGraph(ctx context.Context, planID string, p Progress) ([]StepResult, error) {
	fresh, err := s.PlanCodeGraph(ctx)
	if err != nil {
		return nil, err
	}
	if err := checkPlan(planID, fresh); err != nil {
		return nil, err
	}
	if !fresh.Supported {
		return nil, ErrUnsupportedOS
	}
	env, err := s.env("")
	if err != nil {
		return nil, err
	}
	m := msgs(env, "")
	out := []StepResult{}
	if len(fresh.Steps) > 0 {
		r := s.applyCodeGraph(ctx, m, p, 1, 60)
		out = append(out, r)
		if r.Status == StatusError {
			return out, errors.New(r.Message)
		}
	}
	inv := installer.BuildInventory(ctx, env, nil)
	stored, _ := env.RT().Store().Load()
	var firstErr error
	for _, r := range inv.Runtimes {
		if len(stored.ForRuntime(r.Name)) == 0 {
			continue
		}
		rc, err := s.runtimeCtx(IntegrationRequest{Runtime: r.Name})
		if err == nil {
			var acts []rt.Action
			if acts, err = s.codegraphActions(rc); err == nil && mutating(acts) {
				_, _, err = rt.BackupAndApply(rc.renv, rc.a, acts)
			}
		}
		res := StepResult{ID: "codegraph." + r.Name, Status: StatusOK}
		if err != nil {
			res.Status, res.Message = StatusError, err.Error()
			firstErr = errors.Join(firstErr, err)
		}
		p.emit(res.ID, statusOf(err), r.Label, 90)
		out = append(out, res)
	}
	p.emit("done", statusOf(firstErr), "", 100)
	return out, firstErr
}
