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
	if err := SetRuntime(l, "ana", "", "sonnet", false); err != nil {
		t.Fatal(err)
	}
	r, _ := ResolveRuntime(l, "ana")
	if r.Runtime != "claude-code" || r.Model != "sonnet" || r.ModelSource != "project" {
		t.Fatalf("%+v", r)
	}
	if err := SetRuntime(l, "ana", "opencode", "", true); err != nil {
		t.Fatal(err)
	}
	r, _ = ResolveRuntime(l, "ana")
	if r.Runtime != "opencode" || r.RuntimeSource != "local" || r.Model != "sonnet" {
		t.Fatalf("%+v", r)
	}
	if err := SetRuntime(l, "ana", "", "", false); err == nil {
		t.Fatal("empty set must fail")
	}
}

func TestLocalRuntimeIsPerPersonWithLegacyFallback(t *testing.T) {
	root := t.TempDir()
	l := gabitotl.NewLayout(root)
	if _, err := gabitotl.Init(root, gabitotl.InitOptions{Name: "p", Runtime: "claude-code"}); err != nil {
		t.Fatal(err)
	}
	// A legacy shared local/runtime.md is read when the person has no file.
	if err := gabitotl.WriteDoc(l.LegacyLocalRuntime(), localRuntime{Runtime: "kiro-cli"}, bodyLocal); err != nil {
		t.Fatal(err)
	}
	if r, _ := ResolveRuntime(l, "ana"); r.Runtime != "kiro-cli" || r.RuntimeSource != "local" {
		t.Fatalf("legacy fallback: %+v", r)
	}
	// A per-person file wins and is isolated from other people.
	if err := SetRuntime(l, "ana", "opencode", "", true); err != nil {
		t.Fatal(err)
	}
	if r, _ := ResolveRuntime(l, "ana"); r.Runtime != "opencode" {
		t.Fatalf("ana: %+v", r)
	}
	if r, _ := ResolveRuntime(l, "bob"); r.Runtime != "kiro-cli" {
		t.Fatalf("bob must not see ana's override: %+v", r)
	}
	if p, _ := l.LocalRuntime("ana"); !strings.HasSuffix(p, "local/users/ana/runtime.md") {
		t.Fatalf("path = %s", p)
	}
	if _, err := os.Stat(l.LegacyLocalRuntime()); err != nil {
		t.Errorf("legacy file must be left in place: %v", err)
	}
	// No user: no override, and a local set is refused.
	if r, _ := ResolveRuntime(l, ""); r.Runtime != "claude-code" {
		t.Fatalf("no user: %+v", r)
	}
	if err := SetRuntime(l, "", "x", "", true); err == nil {
		t.Fatal("local set without a user must fail")
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
	if err := SetRuntime(l, "ana", "kiro-cli", "auto", false); err != nil {
		t.Fatal(err)
	}
	if err := SetRuntime(l, "ana", "", "other", true); err != nil {
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
	if err := SetRuntime(l, "ana", "", "m", false); err == nil {
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

func TestMemoryForIsTheSameForBothProviders(t *testing.T) {
	for _, p := range []string{gabitotl.MemoryEngram, gabitotl.MemoryMemoryMCP} {
		main, extra, retire := MemoryFor(p, true)
		if main.Name != EntryBridge || len(extra) != 1 || extra[0].Name != EntryEngram || len(retire) != 0 {
			t.Fatalf("%s with engram: %+v %+v %v", p, main, extra, retire)
		}
		main, extra, retire = MemoryFor(p, false)
		if main.Name != EntryBridge || len(extra) != 0 || len(retire) != 1 || retire[0] != EntryEngram {
			t.Fatalf("%s without engram: %+v %+v %v", p, main, extra, retire)
		}
	}
	if main, extra, retire := MemoryFor("none", true); main.Command != "" || extra != nil || retire != nil {
		t.Fatalf("none configures nothing: %+v %+v %v", main, extra, retire)
	}
}
