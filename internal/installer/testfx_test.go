package installer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/memory"
	"github.com/winnstorm/gabitotl/internal/memory/migrate"
	"github.com/winnstorm/gabitotl/internal/runtime/all"
)

const (
	fxUserMD = "# My rules\n\nBe kind.\n"
	fxGentle = "<!-- gentle-ai:persona -->\nGentleman.\n<!-- /gentle-ai:persona -->\n"
	fxOC     = "{\n  \"provider\": {\"anthropic\": {}},\n  \"model\": \"anthropic/claude-sonnet-4-5\"\n}\n"
	fxState  = `{"version":"4.2.0","installed_agents":["claude-code","opencode","kiro-ide","windsurf"],"components":["persona","engram"]}`
	fxDoctor = `{"status":"ok","project":"shop","checks":[{"check_id":"db","result":"ok","message":"fine"}]}`
	fxList   = "Projects:\n  shop  7 obs  2 sessions  1 prompts\n"
	fxDB     = "ENGRAM-DB-BYTES"
)

// recMemory is a memory provider that records saves; it stands in for Engram.
type recMemory struct {
	mu    sync.Mutex
	saved []memory.Entry
}

func (r *recMemory) Search(context.Context, memory.Query) ([]memory.Entry, error) { return nil, nil }
func (r *recMemory) Save(_ context.Context, e memory.Entry) (memory.ID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.saved = append(r.saved, e)
	return "1", nil
}
func (r *recMemory) Update(context.Context, memory.ID, memory.EntryPatch) error {
	return memory.ErrUnsupported
}
func (r *recMemory) SessionSummary(context.Context, memory.Summary) error {
	return memory.ErrUnsupported
}
func (r *recMemory) CurrentProject(context.Context) (string, error) { return "shop", nil }
func (r *recMemory) Doctor(context.Context) memory.Report           { return memory.Report{Provider: "engram"} }

type fixture struct {
	t      *testing.T
	home   string
	root   string
	env    Env
	runner *FakeRunner
	mem    *recMemory
	vars   map[string]string
	bins   map[string]bool
	remote string
}

func writeFile(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func slurp(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// newFixture builds a fake machine: a home with Gentle-AI state, an Engram data
// dir, runtime configs with user content, a project, and a fake runner that
// logs every command. Nothing outside t.TempDir is ever read.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, home: t.TempDir(), root: t.TempDir(), mem: &recMemory{}, vars: map[string]string{"SHELL": "/bin/bash"},
		remote: "https://github.com/acme/shop.git",
		bins:   map[string]bool{"claude": true, "opencode": true, "kiro-cli": true, "git": true, "engram": true, "gentle-ai": true, "apt": true}}
	w := func(rel, s string) { writeFile(t, filepath.Join(f.home, rel), s) }
	w(".claude/CLAUDE.md", fxUserMD+"\n"+fxGentle)
	w(".config/opencode/opencode.json", fxOC)
	w(".kiro/steering/product.md", "# Product\n")
	w(".gentle-ai/state.json", fxState)
	w(".gentle-ai/backups/old.tgz", "backup")
	w(".engram/engram.db", fxDB)
	writeFile(t, filepath.Join(f.root, "go.mod"), "module shop\n")
	writeFile(t, filepath.Join(f.root, "main.go"), "package main\n")

	f.runner = &FakeRunner{Handlers: map[string]func([]string) (string, error){
		"git": func(a []string) (string, error) {
			if len(a) > 0 && a[0] == "rev-parse" {
				return "true\n", nil
			}
			return f.remote + "\n", nil
		},
		"engram doctor":   func([]string) (string, error) { return fxDoctor, nil },
		"engram projects": func([]string) (string, error) { return fxList, nil },
		"engram export": func(a []string) (string, error) {
			writeFile(t, a[1], `{"sessions":[],"observations":[]}`)
			return "Exported to " + a[1] + "\n  Sessions: 2\n  Observations: 7\n  Prompts: 1\n", nil
		},
		"engram save": func([]string) (string, error) { return "Memory saved: #9\n", nil },
		"gentle-ai uninstall": func([]string) (string, error) {
			writeFile(t, filepath.Join(f.home, ".gentle-ai", "uninstalled"), "yes")
			return "uninstalled\n", nil
		},
	}}
	f.env = Env{
		Home: f.home, ProjectRoot: f.root, GOOS: "linux", GOARCH: "amd64",
		Getenv: func(k string) string { return f.vars[k] },
		Environ: func() []string {
			var o []string
			for k, v := range f.vars {
				o = append(o, k+"="+v)
			}
			return o
		},
		LookPath: func(n string) (string, error) {
			if f.bins[n] {
				return "/fake/bin/" + n, nil
			}
			return "", os.ErrNotExist
		},
		Runner: f.runner, Runtimes: all.Registry(), Migrators: migrate.NewRegistry(), Version: "test",
		Now:        func() time.Time { return time.Date(2026, 10, 5, 21, 0, 0, 0, time.UTC) },
		OpenMemory: func(gabitotl.MemoryConfig) (memory.Provider, error) { return f.mem, nil },
	}
	return f
}

// treeHash hashes every file (path and content) under dirs, so tests can prove
// a command changed nothing.
func treeHash(t *testing.T, dirs ...string) string {
	t.Helper()
	h := sha256.New()
	for _, d := range dirs {
		var paths []string
		_ = filepath.WalkDir(d, func(p string, e fs.DirEntry, err error) error {
			if err == nil && !e.IsDir() {
				paths = append(paths, p)
			}
			return nil
		})
		sort.Strings(paths)
		for _, p := range paths {
			b, _ := os.ReadFile(p)
			h.Write([]byte(strings.TrimPrefix(p, d) + "\x00"))
			h.Write(b)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func removeAll(p string) error { return os.RemoveAll(p) }

// treeHashExcluding hashes dir without its top-level entry named skip.
func treeHashExcluding(t *testing.T, dir, skip string) string {
	t.Helper()
	es, _ := os.ReadDir(dir)
	var parts []string
	for _, e := range es {
		if e.Name() != skip {
			parts = append(parts, filepath.Join(dir, e.Name()))
		}
	}
	return treeHash(t, parts...)
}
