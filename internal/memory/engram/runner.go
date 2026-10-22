package engram

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/winnstorm/gabitotl/internal/procattr"
)

// Runner executes the engram binary. Tests use a fake so that no real Engram
// database is ever touched.
type Runner interface {
	// Run executes engram with args and returns stdout and stderr.
	Run(ctx context.Context, args ...string) (stdout, stderr []byte, err error)
}

// ExecRunner runs the real engram CLI.
type ExecRunner struct {
	Binary  string   // resolved binary path
	DataDir string   // optional ENGRAM_DATA_DIR override; empty inherits the environment
	Env     []string // base environment; nil uses os.Environ()
}

// FindBinary discovers the engram binary with exec.LookPath, falling back to
// ~/.local/bin/engram.
func FindBinary() (string, error) {
	if p, err := exec.LookPath("engram"); err == nil {
		return p, nil
	}
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".local", "bin", "engram")
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", errors.New("engram binary not found in PATH or ~/.local/bin")
}

// NewExecRunner resolves the binary and builds a runner. ENGRAM_DATA_DIR is
// honored through the inherited environment unless dataDir is set.
func NewExecRunner(dataDir string) (*ExecRunner, error) {
	bin, err := FindBinary()
	if err != nil {
		return nil, err
	}
	return &ExecRunner{Binary: bin, DataDir: dataDir}, nil
}

// Run implements Runner.
func (r *ExecRunner) Run(ctx context.Context, args ...string) ([]byte, []byte, error) {
	cmd := procattr.NoWindow(exec.CommandContext(ctx, r.Binary, args...))
	env := r.Env
	if env == nil {
		env = os.Environ()
	}
	if r.DataDir != "" {
		env = append(append([]string(nil), env...), "ENGRAM_DATA_DIR="+r.DataDir)
	}
	cmd.Env = env
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return out.Bytes(), errb.Bytes(), fmt.Errorf("engram %s: %w", firstArg(args), err)
	}
	return out.Bytes(), errb.Bytes(), nil
}

func firstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}
