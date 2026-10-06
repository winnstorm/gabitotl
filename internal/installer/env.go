// Package installer is the pure logic behind `gabitotl install`, `migrate`,
// `doctor`, `restore`, `uninstall` and `cleanup` (installer spec sections 3-5,
// 16-28, 38-43, 45-49). It never prints: every phase returns a typed result and
// reports progress through an Observer, so the CLI (or a future TUI) is only
// presentation.
//
// Hard invariants, each enforced by code and covered by a test:
//
//	(a) no destructive step runs before the global snapshot exists;
//	(b) Gentle-AI/legacy retirement runs only after every critical verify check passed;
//	(c) Engram is adopted, never copied, re-created or modified; its database
//	    file is never opened here, and `engram export` is only a safety backup
//	    bracketed by `engram doctor` before and after;
//	(d) anything already present is adopted, not installed again;
//	(e) nothing that cannot be attributed to Gentle-AI is removed; ~/.engram,
//	    .engram, user skills, secrets, credentials, repositories and
//	    ~/.gentle-ai/backups/ are never touched;
//	(f) reports and logs never carry secrets or environment values.
//
// Every external dependency (home, environment, PATH lookup, command runner,
// clock) is injected through Env, so tests use temp fixtures and fake runners
// and never read the real home.
package installer

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/memory"
	"github.com/winnstorm/gabitotl/internal/memory/engram"
	"github.com/winnstorm/gabitotl/internal/memory/migrate"
	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

// Runner executes external commands (git, engram, gentle-ai). Tests inject a
// fake that records every invocation.
type Runner interface {
	Run(ctx context.Context, dir, name string, args ...string) (stdout, stderr []byte, err error)
}

// ExecRunner runs real commands with the inherited environment.
type ExecRunner struct{}

// Run implements Runner.
func (ExecRunner) Run(ctx context.Context, dir, name string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	return out.Bytes(), errb.Bytes(), err
}

// Env carries every external dependency of the installer.
type Env struct {
	Home        string
	ProjectRoot string // "" when not inside a project
	GOOS        string
	GOARCH      string
	Getenv      func(string) string
	Environ     func() []string // names only are ever inspected, never values
	LookPath    func(string) (string, error)
	Runner      Runner
	Now         func() time.Time
	Runtimes    *rt.Registry
	Migrators   *migrate.Registry
	Version     string
	// OpenMemory builds the memory provider used to save imported knowledge.
	// Nil means imports are only recorded in imports.md.
	OpenMemory func(cfg gabitotl.MemoryConfig) (memory.Provider, error)
	// Team holds the team-memory hooks (managed server, team database, tokens).
	Team TeamEnv
	// RenderReport renders the migration report for imports.md (the CLI injects
	// the Spanish renderer). Nil uses Report.DefaultMarkdown.
	RenderReport func(Report) string
}

// StateDir is the GabitoTL global directory (~/.gabitotl).
func (e Env) StateDir() string { return filepath.Join(e.Home, ".gabitotl") }

// Var reads an environment variable through the injected lookup.
func (e Env) Var(k string) string {
	if e.Getenv == nil {
		return ""
	}
	return e.Getenv(k)
}

// Vars lists the process environment (name=value); only names are inspected.
func (e Env) vars() []string {
	if e.Environ == nil {
		return nil
	}
	return e.Environ()
}

// Look reports whether bin is on PATH.
func (e Env) Look(bin string) (string, bool) {
	if e.LookPath == nil {
		return "", false
	}
	p, err := e.LookPath(bin)
	return p, err == nil
}

// Clock returns the current time.
func (e Env) Clock() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

// RT builds the runtime-adapter Env.
func (e Env) RT() rt.Env {
	return rt.Env{
		Home: e.Home, ProjectRoot: e.ProjectRoot, StateDir: e.StateDir(), GOOS: e.GOOS,
		Getenv: e.Getenv, LookPath: e.LookPath, Now: e.Now,
	}
}

func (e Env) run(ctx context.Context, dir, name string, args ...string) ([]byte, []byte, error) {
	if e.Runner == nil {
		return nil, nil, os.ErrInvalid
	}
	return e.Runner.Run(ctx, dir, name, args...)
}

// engramRunner adapts the injected Runner to the T2 engram.Runner interface.
type engramRunner struct {
	env Env
	bin string
}

func (r engramRunner) Run(ctx context.Context, args ...string) ([]byte, []byte, error) {
	return r.env.run(ctx, "", r.bin, args...)
}

// Engram returns the T2 Engram CLI adapter over the injected Runner, or false
// when the binary is not installed. The database file is never touched: every
// operation is an official `engram` command.
func (e Env) Engram(project string) (*engram.Provider, string, bool) {
	bin, ok := e.Look("engram")
	if !ok {
		return nil, "", false
	}
	return engram.New(engram.Options{Runner: engramRunner{env: e, bin: bin}, Project: project}), bin, true
}
