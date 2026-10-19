package gui

import (
	"context"
	"errors"
	"fmt"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/identity"
	"github.com/winnstorm/gabitotl/internal/installer"
	"github.com/winnstorm/gabitotl/internal/installer/ui"
	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

// Integration operations.
const (
	OpApply  = "apply"
	OpRemove = "remove"
)

// Integration is one coding agent as the Manager lists it.
type Integration struct {
	Name    string `json:"name"`
	Label   string `json:"label"`
	State   string `json:"state"`
	Usable  bool   `json:"usable"`  // installed or configured on this machine
	Managed bool   `json:"managed"` // GabitoTL has assets in it
	Healthy bool   `json:"healthy"` // every managed asset verifies
	Problem string `json:"problem,omitempty"`
}

// IntegrationRequest names one runtime and what to do with it.
type IntegrationRequest struct {
	Root           string `json:"root"`
	Runtime        string `json:"runtime"`
	Op             string `json:"op"`    // apply | remove
	Scope          string `json:"scope"` // global | project (apply only)
	Memory         string `json:"memory"`
	MigratePersona bool   `json:"migratePersona"`
}

// IntegrationResult is what an apply or remove did.
type IntegrationResult struct {
	Runtime string   `json:"runtime"`
	Applied []string `json:"applied"`
	Removed []string `json:"removed"`
	Manual  []string `json:"manual"` // things the user must do by hand
	Backup  string   `json:"backup,omitempty"`
	Checks  []Check  `json:"checks"`
}

// rtCtx is the resolved context of a runtime operation.
type rtCtx struct {
	env  installer.Env
	renv rt.Env
	opts rt.Options
	a    rt.Adapter
	m    ui.Messages
}

func (s *Service) runtimeCtx(req IntegrationRequest) (rtCtx, error) {
	env, err := s.env(req.Root)
	if err != nil {
		return rtCtx{}, err
	}
	a, ok := env.Runtimes.Get(req.Runtime)
	if !ok {
		return rtCtx{}, fmt.Errorf("%w %q (known: %v)", installer.ErrUnknownRuntime, req.Runtime, env.Runtimes.Names())
	}
	scope := rt.Scope(req.Scope)
	switch scope {
	case "":
		scope = rt.ScopeGlobal
	case rt.ScopeGlobal:
	case rt.ScopeProject:
		if req.Root == "" || !gabitotl.Exists(req.Root) {
			return rtCtx{}, errors.New("project scope needs a project: install GabitoTL in it first")
		}
	default:
		return rtCtx{}, fmt.Errorf("scope must be global or project, got %q", req.Scope)
	}
	renv := env.RT()
	// The doctor judges a project without memory.md as an Engram project, so a
	// connect or repair does too; otherwise it would leave the bridge out and
	// the doctor would report drift right after the fix.
	memory := req.Memory
	if memory == "" {
		memory = gabitotl.MemoryEngram
		if req.Root != "" {
			if m, err := gabitotl.LoadMemory(gabitotl.NewLayout(req.Root)); err == nil {
				memory = m.Provider
			}
		}
	}
	opts := installer.RuntimeOptions(renv, req.Root, scope, memory, req.MigratePersona)
	opts.Model = modelFor(req.Root, a)
	return rtCtx{env: env, renv: renv, opts: opts, a: a, m: msgs(env, "")}, nil
}

// modelFor returns the project model only for the runtime it was chosen for.
func modelFor(root string, a rt.Adapter) string {
	if root == "" {
		return ""
	}
	id := ""
	if u, err := identity.Active(root); err == nil {
		id = u.ID
	}
	r, err := rt.ResolveRuntime(gabitotl.NewLayout(root), id)
	if err != nil || r.Runtime != a.Name() {
		return ""
	}
	return r.Model
}

// Integrations lists every runtime with its state and health.
func (s *Service) Integrations(ctx context.Context, root string) ([]Integration, error) {
	env, err := s.env(root)
	if err != nil {
		return nil, err
	}
	inv := installer.BuildInventory(ctx, env, nil)
	st, _ := env.RT().Store().Load()
	out := []Integration{}
	for _, r := range inv.Runtimes {
		it := Integration{Name: r.Name, Label: r.Label, State: string(r.State), Usable: r.Usable(), Managed: len(st.ForRuntime(r.Name)) > 0}
		if it.Managed {
			it.Healthy = true
			rc, err := s.runtimeCtx(IntegrationRequest{Root: root, Runtime: r.Name})
			if err == nil {
				for _, c := range rc.a.Verify(rc.renv, rc.opts) {
					if !c.OK {
						it.Healthy, it.Problem = false, c.Name+": "+c.Detail
						break
					}
				}
			}
		}
		out = append(out, it)
	}
	return out, nil
}

var kindOrder = []rt.Kind{rt.Create, rt.Modify, rt.Preserve, rt.RemoveAfterVerify, rt.NoAction}

// actionGroups groups adapter actions by verb for display.
func actionGroups(acts []rt.Action) []PlanGroup {
	var out []PlanGroup
	for _, k := range kindOrder {
		g := PlanGroup{Kind: string(k), Label: string(k)}
		for _, a := range acts {
			if a.Kind == k {
				g.Lines = append(g.Lines, fmt.Sprintf("%s [%s]", a.Path, a.Target))
			}
		}
		if len(g.Lines) > 0 {
			out = append(out, g)
		}
	}
	return out
}

func mutating(acts []rt.Action) bool {
	for _, a := range acts {
		if a.Kind == rt.Create || a.Kind == rt.Modify || a.Kind == rt.RemoveAfterVerify {
			return true
		}
	}
	return false
}

// PlanIntegration shows what connecting (apply) or removing a runtime would do.
func (s *Service) PlanIntegration(ctx context.Context, req IntegrationRequest) (Plan, error) {
	rc, err := s.runtimeCtx(req)
	if err != nil {
		return Plan{}, err
	}
	switch req.Op {
	case OpApply, "":
		acts, err := rc.a.Plan(rc.renv, rc.opts)
		if err != nil {
			return Plan{}, wrap(rc.a.Name(), err)
		}
		return seal(Plan{Groups: actionGroups(acts), Notes: []Note{}, Steps: []PlanStep{}, Mutates: mutating(acts), Supported: true}), nil
	case OpRemove:
		return s.planRemove(rc, []string{req.Runtime})
	}
	return Plan{}, fmt.Errorf("op must be apply or remove, got %q", req.Op)
}

// planRemove is the uninstall plan of some runtimes (backup included by the run).
func (s *Service) planRemove(rc rtCtx, only []string) (Plan, error) {
	up, err := installer.PlanUninstall(rc.env, only)
	if err != nil {
		return Plan{}, err
	}
	g := PlanGroup{Kind: "REMOVE", Label: "REMOVE"}
	for _, n := range up.Runtimes {
		for _, f := range up.Files[n] {
			g.Lines = append(g.Lines, rc.m.Format("uninstall.remove.item", "name", n, "detail", f))
		}
	}
	p := Plan{Notes: []Note{}, Steps: []PlanStep{}, Mutates: !up.Empty(), Supported: true}
	if len(g.Lines) > 0 {
		p.Groups = append(p.Groups, g)
	}
	for _, k := range up.Preserved {
		p.Notes = append(p.Notes, Note{Code: "keep." + k, Text: rc.m.Format("uninstall.keep." + k)})
	}
	return seal(p), nil
}

// ApplyIntegration connects (apply) or removes one runtime. Both keep the
// existing backup behaviour: apply copies the touched files first; remove goes
// through the installer's uninstall, which saves them before deleting.
func (s *Service) ApplyIntegration(ctx context.Context, req IntegrationRequest, planID string, p Progress) (IntegrationResult, error) {
	fresh, err := s.PlanIntegration(ctx, req)
	if err != nil {
		return IntegrationResult{}, err
	}
	if err := checkPlan(planID, fresh); err != nil {
		return IntegrationResult{}, err
	}
	rc, err := s.runtimeCtx(req)
	if err != nil {
		return IntegrationResult{}, err
	}
	out := IntegrationResult{Runtime: req.Runtime, Applied: []string{}, Removed: []string{}, Manual: []string{}, Checks: []Check{}}
	p.emit(req.Runtime, "running", rc.a.Label(), 10)
	if req.Op == OpRemove {
		res, err := installer.Uninstall(rc.env, []string{req.Runtime}, nil)
		for _, files := range res.Removed {
			out.Removed = append(out.Removed, files...)
		}
		out.Manual, out.Backup = append(out.Manual, res.Manual...), res.Backup
		p.emit(req.Runtime, statusOf(err), rc.a.Label(), 100)
		return out, err
	}
	acts, err := rc.a.Plan(rc.renv, rc.opts)
	if err != nil {
		return out, wrap(rc.a.Name(), err)
	}
	snap, res, err := rt.BackupAndApply(rc.renv, rc.a, acts)
	if len(snap.Files) > 0 {
		out.Backup = snap.Dir
	}
	for _, a := range res.Applied {
		out.Applied = append(out.Applied, a.String())
	}
	for _, a := range res.Removed {
		out.Removed = append(out.Removed, a.Path)
	}
	for _, a := range res.Manual {
		out.Manual = append(out.Manual, a.String())
	}
	for _, c := range rc.a.Verify(rc.renv, rc.opts) {
		out.Checks = append(out.Checks, Check{Group: "runtime", Name: c.Name, OK: c.OK, Detail: c.Detail})
	}
	p.emit(req.Runtime, statusOf(err), rc.a.Label(), 100)
	return out, err
}

func statusOf(err error) string {
	if err != nil {
		return "error"
	}
	return "done"
}
