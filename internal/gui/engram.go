package gui

import (
	"context"
	"errors"

	"github.com/winnstorm/gabitotl/internal/engraminstall"
	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/installer"
	"github.com/winnstorm/gabitotl/internal/installer/ui"
	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

// EngramStatus is the Engram component as the Manager shows it.
type EngramStatus struct {
	Supported bool   `json:"supported"` // a pinned release exists for this system
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	Path      string `json:"path,omitempty"`
	Managed   bool   `json:"managed"` // the copy GabitoTL installed
	OnPath    bool   `json:"onPath"`
	Pinned    string `json:"pinned"`
	Dir       string `json:"dir,omitempty"`
	Source    string `json:"source"`    // official repository (information only)
	NeedsWork bool   `json:"needsWork"` // missing, or ours but not on the user PATH
}

func (s *Service) engram() *engraminstall.Installer {
	if s.d.Engram != nil {
		return s.d.Engram
	}
	return engraminstall.Default()
}

// engramPlan is what installing Engram would do, or false when nothing is
// needed (present, or no pinned release for this system).
func (s *Service) engramPlan(ctx context.Context) (engraminstall.Plan, bool) {
	en := s.engram()
	if !en.Supported() {
		return engraminstall.Plan{}, false
	}
	pl := en.Plan(ctx)
	return pl, pl.Action == engraminstall.ActionInstall || len(pl.Steps) > 0
}

func engramStepOf(m ui.Messages, pl engraminstall.Plan) PlanStep {
	key := "gui.step.engram-install"
	if pl.Action == engraminstall.ActionSkip {
		key = "gui.step.engram-path"
	}
	return PlanStep{ID: "engram", Title: m.Format(key, "version", engraminstall.Version)}
}

// engramStep is the wizard plan entry: present only when the chosen memory is
// Engram and it must be installed.
func (s *Service) engramStep(ctx context.Context, m ui.Messages, p prep) *PlanStep {
	if p.plan.MemoryChoice != gabitotl.MemoryEngram {
		return nil
	}
	pl, need := s.engramPlan(ctx)
	if !need {
		return nil
	}
	st := engramStepOf(m, pl)
	return &st
}

// applyEngram installs Engram, reporting each step between from and to percent.
// A failure is reported, never fatal: the rest of the install does not depend on
// the binary being there.
func (s *Service) applyEngram(ctx context.Context, m ui.Messages, p Progress, from, to int) StepResult {
	total := len(s.engram().Plan(ctx).Steps)
	if total == 0 {
		total = 1
	}
	n := 0
	res, err := s.engram().Install(ctx, func(id string) {
		p.emit("engram."+id, "running", m.Format("gui.engram."+id), from+(to-from)*n/total)
		n++
	})
	switch {
	case errors.Is(err, engraminstall.ErrUnsupported):
		return StepResult{ID: "engram", Status: StatusUnsupported, Message: m.Format("gui.engram.unsupported", "url", engraminstall.SourceURL)}
	case err != nil:
		p.emit("engram", "error", err.Error(), to)
		return StepResult{ID: "engram", Status: StatusError, Message: err.Error()}
	case res.Action == engraminstall.ActionSkip && n == 0:
		p.emit("engram", "done", m.Format("gui.step.engram-skip"), to)
		return StepResult{ID: "engram", Status: StatusSkipped}
	}
	msg := m.Format("gui.step.engram-done", "version", res.Version)
	out := StepResult{ID: "engram", Status: StatusOK}
	out.Message = m.Format("gui.engram.restart")
	msg += " " + out.Message
	if res.NotOnPath {
		n := m.Format("gui.engram.notonpath", "dir", s.engram().Dir)
		out.Message += " " + n
		msg += " " + n
	}
	p.emit("engram", "done", msg, to)
	return out
}

// ensureEngram installs Engram before the installer run when the plan needs it
// and returns the refreshed context (the inventory and the agent entries now see
// the binary). Nothing happens on a dry run.
func (s *Service) ensureEngram(ctx context.Context, dryRun bool, refresh func() (prep, error), pr prep, p Progress, from, to int) (prep, []StepResult, error) {
	if dryRun || s.engramStep(ctx, pr.m, pr) == nil {
		return pr, nil, nil
	}
	r := s.applyEngram(ctx, pr.m, p, from, to)
	if r.Status != StatusOK {
		return pr, []StepResult{r}, nil
	}
	fresh, err := refresh()
	if err != nil {
		return pr, []StepResult{r}, err
	}
	return fresh, []StepResult{r}, nil
}

// Engram reports the installed Engram.
func (s *Service) Engram(ctx context.Context) (EngramStatus, error) {
	st := s.engram().Status(ctx)
	_, need := s.engramPlan(ctx)
	return EngramStatus{
		Supported: st.Supported, Installed: st.Installed, Version: st.Version, Path: st.Path, Managed: st.Managed,
		OnPath: st.OnPath, Pinned: st.Pinned, Dir: st.Dir, Source: st.Source, NeedsWork: need,
	}, nil
}

// PlanEngram shows what installing Engram would do.
func (s *Service) PlanEngram(ctx context.Context) (Plan, error) {
	env, err := s.env("")
	if err != nil {
		return Plan{}, err
	}
	m := msgs(env, "")
	p := Plan{Groups: []PlanGroup{}, Notes: []Note{}, Steps: []PlanStep{}, Supported: true}
	if !s.engram().Supported() {
		p.Supported, p.Unsupports = false, []string{"engram"}
		return seal(p), nil
	}
	if pl, need := s.engramPlan(ctx); need {
		p.Steps = append(p.Steps, engramStepOf(m, pl))
		p.Mutates = true
	}
	return seal(p), nil
}

// ApplyEngram runs the plan PlanEngram returned: it installs Engram, then adds
// its MCP server to each managed agent through the runtime adapters (backup
// first, idempotent).
func (s *Service) ApplyEngram(ctx context.Context, planID string, p Progress) ([]StepResult, error) {
	fresh, err := s.PlanEngram(ctx)
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
		r := s.applyEngram(ctx, m, p, 1, 60)
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
			continue // GabitoTL does not manage this agent
		}
		rc, err := s.runtimeCtx(IntegrationRequest{Runtime: r.Name})
		if err == nil {
			var acts []rt.Action
			if acts, err = s.engramActions(rc); err == nil && mutating(acts) {
				_, _, err = rt.BackupAndApply(rc.renv, rc.a, acts)
			}
		}
		res := StepResult{ID: "engram." + r.Name, Status: StatusOK}
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

// engramActions plans only the Engram server entry of one agent.
func (s *Service) engramActions(rc rtCtx) ([]rt.Action, error) {
	acts, err := rc.a.Plan(rc.renv, rc.opts)
	if err != nil {
		return nil, wrap(rc.a.Name(), err)
	}
	var out []rt.Action
	for _, a := range acts {
		if a.Edit.Op == rt.OpSetKey && len(a.Edit.Key) > 0 && a.Edit.Key[len(a.Edit.Key)-1] == rt.EntryEngram || a.Target == "server "+rt.EntryEngram {
			out = append(out, a)
		}
	}
	return out, nil
}
