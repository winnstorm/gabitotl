package vcs

import (
	"context"
	"fmt"
	"strings"
)

func errBadOutput(cmd string) error { return fmt.Errorf("vcs: could not read the output of %s", cmd) }

// combineChecks folds the per-check results: any failure fails, else any pending
// check keeps it pending, no checks at all is "none".
func combineChecks(total int, failing, pending []string) Checks {
	switch {
	case len(failing) > 0:
		return Checks{State: ChecksFailing, Detail: strings.Join(failing, ", ")}
	case len(pending) > 0:
		return Checks{State: ChecksPending, Detail: strings.Join(pending, ", ")}
	case total == 0:
		return Checks{State: ChecksNone}
	}
	return Checks{State: ChecksPassing}
}

// cliAuth asks a platform CLI (`<tool> auth status`) whether the person is logged
// in. A missing tool, a logged-out tool and a logged-in one are three states, each
// with the exact fix.
func cliAuth(ctx context.Context, run Runner, dir, provider, tool, installURL string) (AuthState, error) {
	st := AuthState{Provider: provider, Command: tool + " auth login"}
	_, err := runnerOr(run).Run(ctx, dir, tool, "auth", "status")
	switch {
	case err == nil:
		st.Status, st.Command = AuthLoggedIn, ""
	case toolMissing(err):
		st.Status, st.Command, st.InstallURL = AuthToolMissing, "", installURL
		st.Fix = fmt.Sprintf("Install %s from %s and then run: %s auth login", tool, installURL, tool)
	default:
		st.Status = AuthNotLoggedIn
		st.Fix = "Log in with: " + st.Command
	}
	return st, nil
}
