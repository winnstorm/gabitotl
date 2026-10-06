package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/identity"
	"github.com/winnstorm/gabitotl/internal/vcs"
)

// subcommands dispatches "gabitotl <group> <sub> ..." to its handler.
func subcommands(env Env, group string, args []string, subs map[string]func(Env, []string) int) int {
	names := make([]string, 0, len(subs))
	for n := range subs {
		names = append(names, n)
	}
	sort.Strings(names)
	usage := "<" + strings.Join(names, "|") + ">"
	if len(args) == 0 {
		fmt.Fprintf(env.Stderr, "Usage: gabitotl %s %s [flags]\n", group, usage)
		return ExitUsage
	}
	if fn, ok := subs[args[0]]; ok {
		return fn(env, args[1:])
	}
	fmt.Fprintf(env.Stderr, "gabitotl %s: unknown subcommand %q (want one of %s)\n", group, args[0], strings.Join(names, ", "))
	return ExitUsage
}

// userID returns the --user flag or the active local identity.
func userID(root, flagUser string) (string, error) {
	if flagUser != "" {
		if !gabitotl.ValidID(flagUser) {
			return "", gabitotl.ErrInvalidID
		}
		return flagUser, nil
	}
	u, err := identity.Active(root)
	if err != nil {
		return "", fmt.Errorf("%w (pass --user or run `gabitotl init`)", err)
	}
	return u.ID, nil
}

// vcsConfig loads vcs.md, falling back to conservative defaults (every
// operation asks) when the project has not been initialized.
func vcsConfig(layout gabitotl.Layout) (gabitotl.VCSConfig, error) {
	c, err := gabitotl.LoadVCS(layout)
	if errors.Is(err, fs.ErrNotExist) {
		return gabitotl.DefaultVCS("git"), nil
	}
	return c, err
}

// workflowConfig loads workflow.md, falling back to the V1 defaults.
func workflowConfig(layout gabitotl.Layout) (gabitotl.WorkflowConfig, error) {
	c, err := gabitotl.LoadWorkflow(layout)
	if errors.Is(err, fs.ErrNotExist) {
		return gabitotl.DefaultWorkflow(), nil
	}
	return c, err
}

// confirmFunc answers "ask" permissions on an interactive terminal. Without a
// TTY it never confirms, so unattended runs cannot perform "ask" operations.
func confirmFunc(env Env) vcs.Confirm {
	if !env.IsTTY {
		return nil
	}
	p := &prompter{r: bufio.NewReader(env.Stdin), w: env.Stderr}
	return func(op, detail string) bool {
		a := strings.ToLower(p.ask("vcs.md says \"ask\" for "+op+": "+detail+". Proceed? (y/N)", "n"))
		return a == "y" || a == "yes"
	}
}

// openRepo returns the repo of root with vcs.md permissions and confirmation.
func openRepo(env Env, root string) (*vcs.Repo, error) {
	cfg, err := vcsConfig(gabitotl.NewLayout(root))
	if err != nil {
		return nil, err
	}
	r := vcs.New(root, cfg)
	r.Confirm = confirmFunc(env)
	if wf, err := workflowConfig(gabitotl.NewLayout(root)); err == nil {
		r.Branding = vcs.PolicyFromWorkflow(wf)
	}
	r.OnBranding = reportBranding(env)
	return r, nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
