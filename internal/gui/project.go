package gui

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/winnstorm/gabitotl/internal/credstore"
	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/installer"
	"github.com/winnstorm/gabitotl/internal/vcs"
)

// The per-project half of the Manager (D59). The install is global; a project is
// a folder the person opens to change what belongs to it: its delivery override,
// its code index, its guard hooks, its GabitoTL files.

// Where the effective delivery settings of a project come from.
const (
	SourceProject = "project" // the project (or the person in it) set them
	SourceGlobal  = "global"  // the user's global defaults
	SourceDefault = "default" // the built-in defaults: nothing saved anywhere
)

// EffectiveDelivery is what a project really uses, with its provenance.
type EffectiveDelivery struct {
	Automation   string         `json:"automation"`
	MergeMethod  string         `json:"mergeMethod"`
	DeleteBranch bool           `json:"deleteBranch"`
	Draft        bool           `json:"draft"`
	Source       string         `json:"source"` // project | global | default
	Global       GlobalDelivery `json:"global"` // the defaults it falls back to
}

// ProjectCredential says whether the token of the project's Git host is stored.
// Known is false when the project has no remote to read a host from.
type ProjectCredential struct {
	Known    bool   `json:"known"`
	Platform string `json:"platform,omitempty"`
	Host     string `json:"host,omitempty"`
	Stored   bool   `json:"stored"`
}

// ProjectDetail is the overview of one project folder.
type ProjectDetail struct {
	Path        string            `json:"path"`
	Name        string            `json:"name"`
	HasGabitoTL bool              `json:"hasGabitotl"`
	IsGit       bool              `json:"isGit"`
	Platform    string            `json:"platform,omitempty"` // inferred from the remote: github | gitlab | bitbucket | ...
	Host        string            `json:"host,omitempty"`
	Delivery    EffectiveDelivery `json:"delivery"`
	Credential  ProjectCredential `json:"credential"`
	Index       []IndexBackend    `json:"index"` // code index tools (works without .gabitotl)
	Guard       GuardSettings     `json:"guard"`
}

// Project describes root. It only reads; a remote URL is reduced to its host and
// never returned (it can embed credentials).
func (s *Service) Project(ctx context.Context, root string) (ProjectDetail, error) {
	abs, err := checkRoot(root)
	if err != nil {
		return ProjectDetail{}, err
	}
	env, err := s.env(abs)
	if err != nil {
		return ProjectDetail{}, err
	}
	p := installer.DetectProject(ctx, env)
	out := ProjectDetail{
		Path: abs, Name: p.Name, HasGabitoTL: p.Exists, IsGit: p.GitRepo, Index: s.codeIndex().Backends(abs),
		Guard: GuardSettings{Mode: gabitotl.DefaultWorkflow().BrandingGuard, HooksAnswer: gabitotl.HooksAsk},
	}
	if out.Index == nil {
		out.Index = []IndexBackend{}
	}
	if p.Remote != "" {
		out.Platform, out.Host = p.Provider, vcs.RemoteHost(p.Remote)
	}
	if out.Delivery, err = s.effectiveDelivery(env, abs, p.Exists); err != nil {
		return out, err
	}
	if out.Host != "" {
		out.Credential = ProjectCredential{Known: true, Platform: out.Platform, Host: credstore.NormalizeHost(out.Host)}
		if st, err := s.credStore(); err == nil {
			_, out.Credential.Stored, _ = st.Get(out.Credential.Host)
		}
	}
	if out.HasGabitoTL {
		if w, err := gabitotl.LoadWorkflow(gabitotl.NewLayout(abs)); err == nil {
			out.Guard = GuardSettings{Mode: w.BrandingGuard, HooksAnswer: w.GuardHooks, HooksInstalled: w.GuardHooks == gabitotl.HooksInstalled}
		}
	}
	return out, nil
}

// effectiveDelivery layers the settings the way the CLI does (built-in, global,
// project, person) and names where they come from.
func (s *Service) effectiveDelivery(env installer.Env, root string, hasProject bool) (EffectiveDelivery, error) {
	base, globalSaved, err := gabitotl.LoadGlobalVCS(env.Home)
	if err != nil {
		return EffectiveDelivery{}, err
	}
	cfg, source := base, SourceDefault
	if globalSaved {
		source = SourceGlobal
	}
	if hasProject {
		l, user := gabitotl.NewLayout(root), gabitotl.ActiveUserID()
		switch c, err := gabitotl.LoadVCSLayered(l, user, base); {
		case err == nil:
			cfg = c
			if c.Configured || len(gabitotl.InspectVCS(l, user).Overrides) > 0 {
				source = SourceProject
			}
		case errors.Is(err, fs.ErrNotExist):
		default:
			return EffectiveDelivery{}, err
		}
	}
	return EffectiveDelivery{
		Automation: cfg.AutomationLevel(), MergeMethod: cfg.EffectiveMergeMethod(), DeleteBranch: cfg.DeleteBranch, Draft: cfg.DraftPRs,
		Source: source, Global: globalDeliveryOf(base, globalSaved),
	}, nil
}

// ResetProjectDelivery makes the project follow the global defaults again.
func (s *Service) ResetProjectDelivery(root string) (ChangeResult, error) {
	root, err := checkRoot(root)
	if err != nil {
		return ChangeResult{}, err
	}
	if !gabitotl.Exists(root) {
		return ChangeResult{}, errors.New("there is no .gabitotl here: set GabitoTL up in the project first")
	}
	env, err := s.env(root)
	if err != nil {
		return ChangeResult{}, err
	}
	l := gabitotl.NewLayout(root)
	if err := gabitotl.ResetDeliveryToGlobal(l, gabitotl.ActiveUserID(), env.Home); err != nil {
		return ChangeResult{}, err
	}
	return ChangeResult{Files: []string{l.VCS()}, Notes: []Note{}}, nil
}

// projectFiles are the files `gabitotl init` creates, relative to the root.
func projectFiles(l gabitotl.Layout, root string) []string {
	var out []string
	for _, p := range []string{l.Project(), l.Memory(), l.Runtime(), l.VCS(), l.Workflow(), l.Imports(), l.Gitignore()} {
		if rel, err := filepath.Rel(root, p); err == nil {
			out = append(out, filepath.ToSlash(rel))
		}
	}
	return out
}

// PlanProjectSetup shows what setting GabitoTL up in root creates. It is the same
// `gabitotl init` the CLI runs (solo memory, nothing overwritten).
func (s *Service) PlanProjectSetup(_ context.Context, root string) (Plan, error) {
	abs, err := checkRoot(root)
	if err != nil {
		return Plan{}, err
	}
	env, err := s.env(abs)
	if err != nil {
		return Plan{}, err
	}
	m := msgs(env, "")
	p := Plan{Groups: []PlanGroup{}, Notes: []Note{}, Steps: []PlanStep{}, Supported: true}
	if gabitotl.Exists(abs) {
		p.Notes = append(p.Notes, Note{Code: "project.exists", Text: m.Format("gui.project.setup.exists")})
		return seal(p), nil
	}
	p.Mutates = true
	p.Groups = append(p.Groups, PlanGroup{Kind: "CREATE", Label: m.Format("gui.project.setup.create"), Lines: projectFiles(gabitotl.NewLayout(abs), abs)})
	p.Notes = append(p.Notes, Note{Code: "project.defaults", Text: m.Format("gui.project.setup.defaults")})
	return seal(p), nil
}

// ApplyProjectSetup creates .gabitotl in root with the installer's own Init, seeds
// the project's delivery from the global defaults and remembers the folder.
func (s *Service) ApplyProjectSetup(ctx context.Context, root, planID string, p Progress) (ChangeResult, error) {
	fresh, err := s.PlanProjectSetup(ctx, root)
	if err != nil {
		return ChangeResult{}, err
	}
	if err := checkPlan(planID, fresh); err != nil {
		return ChangeResult{}, err
	}
	abs, err := checkRoot(root)
	if err != nil {
		return ChangeResult{}, err
	}
	env, err := s.env(abs)
	if err != nil {
		return ChangeResult{}, err
	}
	p.emit("project", "running", abs, 20)
	res, err := gabitotl.Init(abs, gabitotl.InitOptions{})
	if err != nil {
		p.emit("project", "error", err.Error(), 100)
		return ChangeResult{}, err
	}
	out := ChangeResult{Files: append([]string{}, res.Created...), Notes: []Note{}}
	if len(res.Created) > 0 {
		// A new vcs.md must not mask the user's global defaults with built-in ones.
		if err := gabitotl.ResetDeliveryToGlobal(gabitotl.NewLayout(abs), gabitotl.ActiveUserID(), env.Home); err != nil {
			p.emit("project", "error", err.Error(), 100)
			return out, err
		}
	}
	if s.d.RememberProject != nil {
		_ = s.d.RememberProject(abs)
	}
	p.emit("project", "done", abs, 100)
	return out, nil
}

// IndexProject indexes root with the named code index tool, reporting progress.
// Cancelling ctx stops the tool.
func (s *Service) IndexProject(ctx context.Context, root, tool string, p Progress) error {
	if _, err := checkRoot(root); err != nil {
		return err
	}
	if !s.beginOp() {
		env, err := s.env("")
		if err != nil {
			return ErrBusy
		}
		return errors.New(msgs(env, "").Format("gui.error.busy"))
	}
	defer s.endOp()
	p.emit("index", "running", tool, 10)
	if err := s.SetCodeIndex(ctx, root, tool); err != nil {
		p.emit("index", "error", err.Error(), 100)
		return err
	}
	p.emit("index", "done", tool, 100)
	return nil
}

// checkRoot is the gate of every project-scoped entry point: a non-empty,
// absolute path to an existing directory that may be a project root (never the
// home folder, a temporary folder, a filesystem or drive root or a UNC share).
func checkRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", errors.New("choose a project folder first")
	}
	if !filepath.IsAbs(root) && !gabitotl.IsFilesystemRoot(root) {
		return "", errors.New("the project folder must be an absolute path: " + root)
	}
	if err := gabitotl.ValidateRoot(root); err != nil {
		return "", err
	}
	abs := filepath.Clean(root)
	if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
		return "", errors.New("not an existing folder: " + abs)
	}
	return abs, nil
}

// beginOp claims the single long-running operation slot; endOp frees it.
func (s *Service) beginOp() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return false
	}
	s.running = true
	return true
}

func (s *Service) endOp() {
	s.mu.Lock()
	s.running = false
	s.mu.Unlock()
}
