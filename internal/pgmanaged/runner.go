package pgmanaged

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Call is one invocation of the engine CLI.
type Call struct {
	Bin  string
	Args []string
	// Env is appended to the (filtered) process environment. Secrets travel
	// here, never in Args.
	Env []string
	// Stdout, when set, receives standard output instead of Result.Stdout.
	Stdout io.Writer
}

// Result is what a Call produced.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// Runner runs engine commands. A non-zero exit is returned as *ExitError.
type Runner interface {
	Run(ctx context.Context, c Call) (Result, error)
}

// ExitError is a command that ran and failed.
type ExitError struct {
	Code   int
	Stderr string
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("exit status %d: %s", e.Code, strings.TrimSpace(e.Stderr))
}

// ExecRunner is the real Runner. It never inherits the user's PostgreSQL
// environment.
type ExecRunner struct {
	// Environ returns the base environment (os.Environ when nil).
	Environ func() []string
}

// foreignEnv reports whether a variable belongs to a PostgreSQL the user may
// already run (libpq PG*, DATABASE_URL) or to the image's own variables that
// docker could otherwise inherit by name.
func foreignEnv(kv string) bool {
	k, _, _ := strings.Cut(kv, "=")
	return strings.HasPrefix(k, "PG") || strings.HasPrefix(k, "POSTGRES_") || k == "DATABASE_URL"
}

// FilteredEnv drops every PostgreSQL-related variable from base.
func FilteredEnv(base []string) []string {
	out := make([]string, 0, len(base))
	for _, kv := range base {
		if !foreignEnv(kv) {
			out = append(out, kv)
		}
	}
	return out
}

// Run implements Runner.
func (r ExecRunner) Run(ctx context.Context, c Call) (Result, error) {
	environ := os.Environ
	if r.Environ != nil {
		environ = r.Environ
	}
	cmd := exec.CommandContext(ctx, c.Bin, c.Args...)
	cmd.Env = append(FilteredEnv(environ()), c.Env...)
	var out, errb bytes.Buffer
	if c.Stdout != nil {
		cmd.Stdout = c.Stdout
	} else {
		cmd.Stdout = &out
	}
	cmd.Stderr = &errb
	err := cmd.Run()
	res := Result{Stdout: out.Bytes(), Stderr: errb.Bytes()}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		res.ExitCode = ee.ExitCode()
		return res, &ExitError{Code: res.ExitCode, Stderr: errb.String()}
	}
	return res, err
}
