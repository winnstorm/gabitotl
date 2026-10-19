// Package codeindex detects an optional code index (a pre-built graph of the
// code) so the agent can query it instead of reading files. Detection is local
// only: a binary on PATH plus an index directory in the project. Nothing here
// installs a tool or touches the network.
package codeindex

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Status is what Detect found for one backend in one project.
type Status struct {
	Installed bool   `json:"installed"`
	Indexed   bool   `json:"indexed"`
	Version   string `json:"version,omitempty"`
}

// Backend is one code-index tool.
type Backend interface {
	// Name is the stable identifier and the executable name.
	Name() string
	// Detect reports whether the tool is on PATH and the project is indexed.
	Detect(root string) Status
	// SetupCommand is the argument list that indexes root (no shell).
	SetupCommand(root string) []string
	// InstallHint says how the user installs the tool themselves.
	InstallHint() string
	// AgentHint is one short line telling an agent how to query the index.
	AgentHint() string
}

type tool struct {
	name, indexDir, install, hint string
	setup                         func(root string) []string
}

func (t tool) Name() string                      { return t.name }
func (t tool) SetupCommand(root string) []string { return t.setup(root) }
func (t tool) InstallHint() string               { return t.install }
func (t tool) AgentHint() string                 { return t.hint }

func (t tool) Detect(root string) Status {
	bin, err := exec.LookPath(t.name)
	if err != nil {
		return Status{}
	}
	s := Status{Installed: true, Version: version(bin)}
	if fi, err := os.Stat(filepath.Join(root, t.indexDir)); err == nil && fi.IsDir() {
		s.Indexed = true
	}
	return s
}

// version is best effort: a tool that does not answer simply has no version.
func version(bin string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return strings.TrimSpace(line)
}

// CodeGraph is the default backend: one MCP tool (codegraph_explore), a CLI
// with the same output and a small context cost.
func CodeGraph() Backend {
	return tool{
		name:     "codegraph",
		indexDir: ".codegraph",
		install:  "install CodeGraph (https://github.com/colbymchenry/codegraph), then run `gabitotl index setup`",
		hint:     "CodeGraph: `codegraph_explore` (one call) or CLI `codegraph explore|query|callers|impact`.",
		setup: func(root string) []string {
			return []string{"codegraph", "init", "--yes", root}
		},
	}
}

// CodeReviewGraph is the optional second backend: impact-first, Python-based,
// about 30 MCP tools (more context cost).
func CodeReviewGraph() Backend {
	return tool{
		name:     "code-review-graph",
		indexDir: ".code-review-graph",
		install:  "install it with `pipx install code-review-graph` (or uvx), then run `gabitotl index setup`",
		hint:     "code-review-graph: `get_minimal_context`, then `get_impact_radius`.",
		setup: func(string) []string {
			return []string{"code-review-graph", "build"}
		},
	}
}

// All lists the backends in preference order (the first installed one is the
// default).
func All() []Backend { return []Backend{CodeGraph(), CodeReviewGraph()} }

// ByName returns the backend called name.
func ByName(name string) (Backend, bool) {
	for _, b := range All() {
		if b.Name() == name {
			return b, true
		}
	}
	return nil, false
}

// Describe renders a status as "installed, indexed" / "not installed".
func Describe(s Status) string {
	switch {
	case !s.Installed:
		return "not installed"
	case s.Indexed:
		return "installed, indexed"
	default:
		return "installed, not indexed"
	}
}

// Default picks the backend to set up: the first installed one, or nil.
func Default(root string) Backend {
	for _, b := range All() {
		if b.Detect(root).Installed {
			return b
		}
	}
	return nil
}

// NextStep is the recommended action given what is installed and indexed.
func NextStep(root string) string {
	var installedNotIndexed Backend
	for _, b := range All() {
		s := b.Detect(root)
		if s.Installed && s.Indexed {
			return "ready: " + b.AgentHint()
		}
		if s.Installed && installedNotIndexed == nil {
			installedNotIndexed = b
		}
	}
	if installedNotIndexed != nil {
		return "run `gabitotl index setup` to index this project with " + installedNotIndexed.Name()
	}
	return "optional: " + CodeGraph().InstallHint()
}

// Quick is a one-line status without running any tool (no --version call):
// the first installed backend and whether the project is indexed, or "none".
func Quick(root string) string {
	for _, b := range All() {
		t, ok := b.(tool)
		if !ok {
			continue
		}
		if _, err := exec.LookPath(t.name); err != nil {
			continue
		}
		s := Status{Installed: true}
		if fi, err := os.Stat(filepath.Join(root, t.indexDir)); err == nil && fi.IsDir() {
			s.Indexed = true
		}
		return t.name + ": " + Describe(s)
	}
	return "none installed (optional)"
}
