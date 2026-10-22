// Package gui is the headless backend of the GabitoTL desktop app (the Windows
// setup wizard and the Manager). It is a facade over the packages that already
// do the work (installer, doctor, runtime adapters, lifecycle, guard, code
// index, desktop): it calls them in-process, never shells out to the gabitotl
// CLI and holds no install logic of its own.
//
// Every method takes and returns plain JSON-serialisable structs, so a shell
// (Wails) can bind the Service to JavaScript. Every mutating operation is
// plan-first: a Plan call returns what would change plus an id, and the apply
// call only runs when that id still matches the machine. Nothing prompts; the
// GUI collects all answers up front.
package gui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/winnstorm/gabitotl/internal/codegraph"
	"github.com/winnstorm/gabitotl/internal/credstore"
	"github.com/winnstorm/gabitotl/internal/desktop"
	"github.com/winnstorm/gabitotl/internal/engraminstall"
	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/installer"
	"github.com/winnstorm/gabitotl/internal/installer/ui"
	"github.com/winnstorm/gabitotl/internal/persona"
	"github.com/winnstorm/gabitotl/internal/projects"
	"github.com/winnstorm/gabitotl/internal/vcshosts"
)

// Deps are the ports of the Service. Env is required; every other field has a
// safe default so a test only fills what it exercises.
type Deps struct {
	// Env builds the installer environment for a project root ("" means none).
	Env func(root string) (installer.Env, error)
	// VCSAuth checks the login to the Git platform of the project.
	VCSAuth func(ctx context.Context, root string) (SignIn, error)
	// GuardHistory audits existing commits for AI attribution (read-only).
	GuardHistory func(ctx context.Context, root string) (GuardScan, error)
	// GuardHooks installs or removes the authorship guard Git hooks. cli is the
	// gabitotl executable the hooks call when the Service knows it (it was just
	// installed into the chosen folder); "" lets the composition root decide.
	GuardHooks func(ctx context.Context, root string, install bool, cli string) error
	// CodeIndex detects and sets up the optional code index; nil uses the real tools.
	CodeIndex CodeIndex
	// Desktop is the Windows integration; nil means this OS has none.
	Desktop *desktop.Installer
	// Credentials is the OS credential store of the user at home; nil disables the
	// credential methods. VerifyToken asks the platform whether a token works with
	// no project involved (ok, a secret-free detail, a transport error).
	Credentials func(home string) credstore.Store
	// baseURL is the registry URL of a self-hosted server ("" for a public host);
	// user is the authenticated account name when the platform tells.
	VerifyToken func(ctx context.Context, platform, host, baseURL, token string) (ok bool, detail, user string, err error)
	// Servers is the registry of self-hosted servers (Bitbucket Data Center, GitHub
	// Enterprise, self-managed GitLab) of the user at home; nil disables them.
	Servers func(home string) vcshosts.Registry
	// CodeGraph installs the required CodeGraph component; nil uses the real
	// installer of this machine (Windows only, unsupported elsewhere).
	CodeGraph *codegraph.Installer
	// Engram installs Engram when a project uses it and it is missing; nil uses
	// the real installer of this machine.
	Engram *engraminstall.Installer
	// Projects discovers the projects on this machine (read-only); nil means none.
	// RememberProject / ForgetProject keep the Manager's own list; nil disables them.
	Projects        func(ctx context.Context) ([]projects.Project, error)
	RememberProject func(path string) error
	ForgetProject   func(path string) error
	// Version is shown in Apps & features.
	Version string
}

// Service is the facade the shell binds to the page.
type Service struct {
	d Deps

	mu  sync.Mutex
	cli string // gabitotl.exe installed by the last successful desktop apply

	running bool // a long-running operation (project indexing) holds the slot
}

func (s *Service) setCLI(p string) {
	s.mu.Lock()
	s.cli = p
	s.mu.Unlock()
}

func (s *Service) installedCLI() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cli
}

// New returns a Service over deps.
func New(d Deps) *Service { return &Service{d: d} }

func (s *Service) env(root string) (installer.Env, error) {
	if s.d.Env == nil {
		return installer.Env{}, errors.New("gui: no installer environment configured")
	}
	return s.d.Env(root)
}

// msgs is the catalogue in the language the person saved; a non-empty lang
// (a wizard choice not saved yet) wins.
func msgs(env installer.Env, lang string) ui.Messages {
	if l, err := persona.ParseLang(lang); lang != "" && err == nil {
		return ui.For(l)
	}
	return ui.Active(env.Home)
}

// Messages returns the whole text catalogue (key to text) of lang, "es" or
// "en". The page renders every string from it.
func (s *Service) Messages(lang string) (map[string]string, error) {
	l, err := persona.ParseLang(lang)
	if err != nil {
		return nil, err
	}
	return ui.For(l), nil
}

// Language is the saved language of the person: "es" or "en".
func (s *Service) Language() (string, error) {
	env, err := s.env("")
	if err != nil {
		return "", err
	}
	return string(gabitotl.ActivePersonaLang(env.Home)), nil
}

// SetLanguage saves the language. Runtimes keep their previous persona text
// until the doctor's repair fix re-applies them.
func (s *Service) SetLanguage(lang string) error {
	l, err := persona.ParseLang(lang)
	if err != nil {
		return err
	}
	env, err := s.env("")
	if err != nil {
		return err
	}
	_, err = gabitotl.SavePersonaLang(env.Home, l)
	return err
}

// planID fingerprints a plan so an apply can prove the user saw it. The
// fingerprint covers what changes, never timestamps.
func planID(parts ...any) string {
	h := sha256.New()
	for _, p := range parts {
		b, _ := json.Marshal(p)
		h.Write(b)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// checkPlan verifies the id an apply call received against the fresh plan.
func checkPlan(given string, fresh Plan) error {
	switch {
	case given == "":
		return ErrPlanRequired
	case given != fresh.ID:
		return ErrPlanChanged
	}
	return nil
}

// seal fills the plan id from its content.
func seal(p Plan) Plan {
	// The snapshot name carries the minute, in the field and in the plan lines
	// that create its folder: both are left out of the fingerprint, or a plan
	// reviewed before the minute turns would never apply.
	h := p
	h.Snapshot = ""
	if p.Snapshot != "" {
		h.Groups = make([]PlanGroup, len(p.Groups))
		for i, g := range p.Groups {
			g.Lines = append([]string(nil), g.Lines...)
			for j, l := range g.Lines {
				g.Lines[j] = strings.ReplaceAll(l, p.Snapshot, "<snapshot>")
			}
			h.Groups[i] = g
		}
	}
	p.ID = planID(h)
	return p
}

// planGroups renders installer plan items with the shared presenter.
func planGroups(m ui.Messages, env installer.Env, pl installer.Plan) []PlanGroup {
	var out []PlanGroup
	for _, g := range ui.PlanGroups(m, pl, ui.Paths{Home: env.Home, Root: env.ProjectRoot}) {
		out = append(out, PlanGroup{Kind: string(g.Kind), Label: g.Label, Lines: g.Lines})
	}
	return out
}

func noteList(m ui.Messages, notes []installer.Note) []Note {
	out := []Note{}
	for _, n := range notes {
		out = append(out, Note{Code: n.Code, Text: m.Format("note."+n.Code, "detail", n.Detail)})
	}
	return out
}

// isUnsupported reports an error that only means "not on this OS".
func isUnsupported(err error) bool {
	return errors.Is(err, desktop.ErrUnsupported) || errors.Is(err, ErrUnsupportedOS)
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func wrap(what string, err error) error { return fmt.Errorf("%s: %w", what, err) }
