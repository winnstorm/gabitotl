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
	fs.StringVar(&f.userID, "user-id", "", "developer id (default: slug of the user name)")
	fs.StringVar(&f.userName, "user-name", "", "developer name")
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
	_, activeErr := identity.Active(root)
	hasActive := activeErr == nil

	if env.IsTTY && !f.yes {
		p := &prompter{r: bufio.NewReader(env.Stdin), w: env.Stdout}
		if f.mode == "" {
			f.mode = p.ask("Memory mode (solo/team)", gabitotl.ModeSolo)
		}
		if f.userName == "" && !hasActive {
			def := ""
			if s, ok := identity.SuggestFromGit(); ok {
				def = s.Name
			}
			f.userName = p.ask("Your name", def)
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
	switch {
	case user != nil:
		fmt.Fprintf(env.Stdout, "  identity: %s (%s)\n", user.ID, user.Name)
	case hasActive:
		fmt.Fprintln(env.Stdout, "  identity: unchanged")
	default:
		fmt.Fprintln(env.Stdout, "  identity: not set (use --user-name)")
	}
	return nil
}

// resolveUser returns the identity to save, or nil when none should change.
// The git user.name is only offered as a prompt default, never applied silently.
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
	_, dir, err := hooksDirFor(env, root)
	if err != nil {
		return // not a Git project: nothing to hook
	}
	install := !f.noHooks
	if install && env.IsTTY && !f.yes {
		p := &prompter{r: bufio.NewReader(env.Stdin), w: env.Stdout}
		a := strings.ToLower(p.ask(persona.Msg("guard.hooks.ask"), "s"))
		install = a != "n" && a != "no"
	}
	if !install {
		rememberHooks(root, gabitotl.HooksDeclined)
		fmt.Fprintln(env.Stdout, persona.Msg("guard.hooks.declined"))
		return
	}
	if err := installHooks(env, root, dir); err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl init: guard hooks: %v\n", err)
	}
}
