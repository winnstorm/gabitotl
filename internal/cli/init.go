package cli

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/identity"
	"github.com/winnstorm/gabitotl/internal/installer"
	"github.com/winnstorm/gabitotl/internal/installer/ui"
	"github.com/winnstorm/gabitotl/internal/persona"
)

type initFlags struct {
	root, name, kind, userID, userName, mode, provider, vcs string
	yes, noHooks                                            bool
}

func runInit(env Env, args []string) int {
	var f initFlags
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	fs.StringVar(&f.root, "root", "", "project root (default: Git top-level of the current directory)")
	fs.StringVar(&f.name, "name", "", "project name (default: directory name)")
	fs.StringVar(&f.kind, "kind", "", "project kind: new|existing (default: detected)")
	fs.StringVar(&f.userID, "user-id", "", "explicit developer profile id (default: slug of the user name)")
	fs.StringVar(&f.userName, "user-name", "", "explicit developer profile name (the active identity comes from the OS user)")
	fs.StringVar(&f.mode, "mode", "", "memory mode: solo|team (default: solo)")
	fs.StringVar(&f.provider, "provider", "", "memory provider: engram|memory-mcp (default: by mode)")
	fs.StringVar(&f.vcs, "vcs", "", "VCS provider: github|bitbucket|gitlab|azure-devops|git|local (default: git)")
	fs.BoolVar(&f.yes, "yes", false, "non-interactive: accept defaults, never prompt")
	fs.BoolVar(&f.noHooks, "no-hooks", false, "do not install the Git hooks of the authorship guard (remembered in workflow.md)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitOK
		}
		return ExitUsage
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(env.Stderr, "gabitotl init: unexpected argument %q\n", fs.Arg(0))
		return ExitUsage
	}
	if err := doInit(env, f); err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl init: %v\n", err)
		return ExitError
	}
	return ExitOK
}

func doInit(env Env, f initFlags) error {
	root, err := resolveRoot(env, f.root)
	if err != nil {
		return err
	}
	l := gabitotl.NewLayout(root)

	existed := gabitotl.Exists(root)
	interactive := env.IsTTY && !f.yes
	if interactive {
		// One reader for every question of this run, including the chained team
		// setup: separate readers would swallow each other's buffered input.
		env.Stdin = bufio.NewReader(env.Stdin)
	}
	// Alone or with other people picks the memory provider. Flags (--mode,
	// --provider) answer it, and so does a project that already exists.
	chain := false
	if interactive && !existed && f.mode == "" && f.provider == "" {
		chain = askWorksWithOthers(env, ui.Active(""))
		if chain {
			f.mode = gabitotl.ModeTeam
		}
	}

	res, err := gabitotl.Init(root, gabitotl.InitOptions{
		Name: f.name, Kind: f.kind, Mode: f.mode, Provider: f.provider, VCSProvider: f.vcs,
	})
	if err != nil {
		return err
	}

	user, err := resolveUser(f)
	if err != nil {
		return err
	}
	if user != nil {
		if err := identity.Save(root, *user); err != nil {
			return err
		}
	}

	fmt.Fprintf(env.Stdout, "GabitoTL project at %s\n", l.Dir())
	fmt.Fprintf(env.Stdout, "  created: %d, already present: %d\n", len(res.Created), len(res.Skipped))
	for _, p := range res.Created {
		fmt.Fprintf(env.Stdout, "  + %s\n", p)
	}
	offerHooks(env, root, f)
	if user != nil {
		fmt.Fprintf(env.Stdout, "  profile: %s (%s)\n", user.ID, user.Name)
	}
	if a, err := identity.Active(root); err == nil {
		fmt.Fprintf(env.Stdout, "  identity: %s (%s)\n", a.ID, a.Name)
	}
	switch {
	case chain: // team memory chosen for a new project: finish it with the existing team setup
		return chainInstall(env, root, "")
	case interactive && existed && f.mode == "" && f.provider == "":
		if cfg, err := gabitotl.LoadMemory(l); err == nil && cfg.Provider == gabitotl.MemoryEngram {
			m := ui.Active("")
			if i, ok := ui.NewPrompter(env.Stdin, env.Stdout, m).Choose(m.Format("ask.change"),
				[]string{m.Format("ask.change.no"), m.Format("ask.change.yes")}, 0); ok && i == 1 {
				return chainInstall(env, root, gabitotl.MemoryMemoryMCP)
			}
		}
	}
	return nil
}

// askWorksWithOthers is the one question that picks the memory provider of a new
// project: alone means local Engram, with other people means shared team memory.
// Ended input answers "alone", the safe default.
func askWorksWithOthers(env Env, m ui.Messages) bool {
	i, ok := ui.NewPrompter(env.Stdin, env.Stdout, m).Choose(m.Format("ask.mode"),
		[]string{m.Format("ask.mode.solo"), m.Format("ask.mode.team")}, 0)
	return ok && i == 1
}

// chainInstall continues with the install flow of the same project, which already
// knows the team setup (host or client, server URL, token variable, Engram
// import) and applies it with a snapshot, verification and rollback.
func chainInstall(env Env, root, memory string) error {
	if code := execute(env, installFlags{root: root, memory: memory, noTUI: true, memoryEngine: "auto"}, installer.SourceInstall); code != ExitOK {
		return errors.New("the team memory setup did not complete (the project itself was created; run `gabitotl install` to retry)")
	}
	return nil
}

// resolveUser returns the explicit profile to save, or nil when the flags give
// none. The active identity itself always comes from the OS user.
func resolveUser(f initFlags) (*identity.User, error) {
	if f.userName == "" {
		if f.userID != "" {
			return nil, errors.New("--user-id requires --user-name")
		}
		return nil, nil
	}
	u, err := identity.New(f.userID, f.userName, "")
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func resolveRoot(env Env, flagRoot string) (string, error) {
	if flagRoot != "" {
		abs, err := filepath.Abs(flagRoot)
		if err != nil {
			return "", err
		}
		if err := gabitotl.ValidateRoot(abs); err != nil {
			return "", err
		}
		return abs, nil
	}
	wd, err := env.Getwd()
	if err != nil {
		return "", err
	}
	return gabitotl.FindRoot(wd)
}

type prompter struct {
	r *bufio.Reader
	w io.Writer
}

// ask prints a one-line question and returns the trimmed answer, or def when
// the answer is empty or input ends.
func (p *prompter) ask(question, def string) string {
	if def != "" {
		fmt.Fprintf(p.w, "%s [%s]: ", question, def)
	} else {
		fmt.Fprintf(p.w, "%s: ", question)
	}
	line, _ := p.r.ReadString('\n')
	if line = strings.TrimSpace(line); line != "" {
		return line
	}
	return def
}

// offerHooks installs the authorship guard hooks in a Git project unless the
// answer is already remembered in workflow.md (guard_hooks). Installing is the
// default; the user declines with --no-hooks or by answering "n", and the answer
// is remembered so it is asked once.
func offerHooks(env Env, root string, f initFlags) {
	wf, err := gabitotl.LoadWorkflow(gabitotl.NewLayout(root))
	if err != nil || wf.GuardHooks != gabitotl.HooksAsk {
		return
	}
	if _, _, err := hooksDirFor(env, root); err != nil {
		return // not a Git project: nothing to hook
	}
	decline := f.noHooks
	if !decline && env.IsTTY && !f.yes {
		p := &prompter{r: bufio.NewReader(env.Stdin), w: env.Stdout}
		a := strings.ToLower(p.ask(persona.Msg("guard.hooks.ask"), "s"))
		decline = a == "n" || a == "no"
	}
	if decline {
		fmt.Fprintln(env.Stdout, persona.Msg("guard.hooks.declined"))
	}
	installed, err := ensureHooks(env.Stdout, root, decline)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl init: guard hooks: %v\n", err)
	}
	_ = installed
}
