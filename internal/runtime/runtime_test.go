package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

func TestResolveRuntimeLocalOverridesProject(t *testing.T) {
	root := t.TempDir()
	l := gabitotl.NewLayout(root)
	if _, err := gabitotl.Init(root, gabitotl.InitOptions{Name: "p", Runtime: "claude-code"}); err != nil {
		t.Fatal(err)
	}
	if err := SetRuntime(l, "", "sonnet", false); err != nil {
		t.Fatal(err)
	}
	r, _ := ResolveRuntime(l)
	if r.Runtime != "claude-code" || r.Model != "sonnet" || r.ModelSource != "project" {
		t.Fatalf("%+v", r)
	}
	if err := SetRuntime(l, "opencode", "", true); err != nil {
		t.Fatal(err)
	}
	r, _ = ResolveRuntime(l)
	if r.Runtime != "opencode" || r.RuntimeSource != "local" || r.Model != "sonnet" {
		t.Fatalf("%+v", r)
	}
	if err := SetRuntime(l, "", "", false); err == nil {
		t.Fatal("empty set must fail")
	}
}

func TestSetModelTouchesOnlyRuntimeFiles(t *testing.T) {
	root := t.TempDir()
	l := gabitotl.NewLayout(root)
	if _, err := gabitotl.Init(root, gabitotl.InitOptions{Name: "p"}); err != nil {
		t.Fatal(err)
	}
	before := map[string][]byte{}
	for _, p := range []string{l.Project(), l.Memory(), l.Workflow(), l.VCS()} {
		before[p], _ = os.ReadFile(p)
	}
	if err := SetRuntime(l, "kiro-cli", "auto", false); err != nil {
		t.Fatal(err)
	}
	if err := SetRuntime(l, "", "other", true); err != nil {
		t.Fatal(err)
	}
	for p, b := range before {
		if now, _ := os.ReadFile(p); string(now) != string(b) {
			t.Fatalf("%s changed", p)
		}
	}
}

func TestModelSwitchCanBeForbidden(t *testing.T) {
	root := t.TempDir()
	l := gabitotl.NewLayout(root)
	if err := gabitotl.SaveRuntime(l, gabitotl.RuntimeConfig{Runtime: "x", AllowModelSwitch: false}); err != nil {
		t.Fatal(err)
	}
	if err := SetRuntime(l, "", "m", false); err == nil {
		t.Fatal("must refuse")
	}
}

// Core packages must never branch on runtime names (spec 32). Adapter
// packages (internal/runtime/*), importers and the installer wiring may.
func TestCoreNeverBranchesOnRuntimeNames(t *testing.T) {
	cond := regexp.MustCompile(`^\s*(\}\s*else\s+)?(if|case|switch)\b.*"(claude|claude-code|opencode|kiro|kiro-cli|kiro-ide)"`)
	root := ".."
	var bad []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if strings.HasPrefix(rel, "runtime/") || rel == "importer" || rel == "installer" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(data), "\n") {
			if cond.MatchString(line) {
				bad = append(bad, fmt.Sprintf("%s:%d: %s", rel, i+1, strings.TrimSpace(line)))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(bad) > 0 {
		t.Fatalf("core code branches on runtime names:\n%s", strings.Join(bad, "\n"))
	}
}

func TestRegistry(t *testing.T) {
	var r Registry
	r.Register(stub("a"))
	r.Register(stub("b"))
	if a, ok := r.Get("b"); !ok || a.Name() != "b" || len(r.Names()) != 2 {
		t.Fatal("registry lookup")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate must panic")
		}
	}()
	r.Register(stub("a"))
}

type stub string

func (s stub) Name() string                           { return string(s) }
func (stub) Label() string                            { return "" }
func (stub) Detect(Env) Detection                     { return Detection{} }
func (stub) Inspect(Env, Options) (Inspection, error) { return Inspection{}, nil }
func (stub) Backup(Env, *Snapshot, []Action) error    { return nil }
func (stub) Plan(Env, Options) ([]Action, error)      { return nil, nil }
func (stub) Apply(Env, []Action) (Result, error)      { return Result{}, nil }
func (stub) Verify(Env, Options) []Check              { return nil }
func (stub) RemoveGabitoConfig(Env) (Result, error)   { return Result{}, nil }
func (stub) ListModels(Env) ([]string, error)         { return nil, ErrUnsupported }
