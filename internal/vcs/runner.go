// Package vcs is the Git layer: repository inspection, gated branch/commit/push
// operations, safe rollback by revert, and pull request creation behind a
// provider interface. Every external command goes through a Runner so tests
// never touch a real remote.
package vcs

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/winnstorm/gabitotl/internal/procattr"
)

// Runner executes an external command in dir and returns its trimmed stdout.
type Runner interface {
	Run(ctx context.Context, dir, name string, args ...string) (string, error)
}

// CommandError is returned by ExecRunner for a failed command.
type CommandError struct {
	Cmd    string
	Stderr string
	Err    error
}

func (e *CommandError) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if msg == "" {
		return fmt.Sprintf("%s: %v", e.Cmd, e.Err)
	}
	return fmt.Sprintf("%s: %v: %s", e.Cmd, e.Err, msg)
}

func (e *CommandError) Unwrap() error { return e.Err }

// ExecRunner runs real commands with os/exec.
type ExecRunner struct {
	// Env holds extra KEY=VALUE pairs appended to the process environment.
	Env []string
}

// Run implements Runner. Git never prompts for credentials.
func (r ExecRunner) Run(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := procattr.NoWindow(exec.CommandContext(ctx, name, args...))
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	cmd.Env = append(cmd.Env, r.Env...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return strings.TrimSpace(out.String()), &CommandError{
			Cmd: name + " " + strings.Join(args, " "), Stderr: errb.String(), Err: err,
		}
	}
	return strings.TrimSpace(out.String()), nil
}
