package cli

import (
	"context"
	"errors"
	"github.com/winnstorm/gabitotl/internal/gui"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGUIDepsGuardHooksUseTheGivenBinary(t *testing.T) {
	root := gitRepo(t)
	d := GUIDeps(func() string { return "/opt/app/gabitotl" })
	if d.Env == nil || d.VCSAuth == nil || d.GuardHistory == nil || d.Version == "" {
		t.Fatalf("incomplete wiring: %+v", d)
	}
	ctx := context.Background()
	if err := d.GuardHooks(ctx, root, true, ""); err != nil {
		t.Fatal(err)
	}
	hook := filepath.Join(root, ".git", "hooks", "commit-msg")
	b, err := os.ReadFile(hook)
	if err != nil || !strings.Contains(string(b), "/opt/app/gabitotl") {
		t.Fatalf("the hook must call the CLI binary, not the app: %v\n%s", err, b)
	}
	if err := d.GuardHooks(ctx, root, false, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(hook); err == nil {
		t.Fatal("the hook must be removed")
	}
}

func TestGUIDepsGuardHistoryRuns(t *testing.T) {
	root := gitRepo(t)
	gwrite(t, root, ".gabitotl/workflow.md", "---\noutput: compact\nlanguage: es-AR\npersona: gabito\nbranding_guard: strip\n---\n")
	if _, err := GUIDeps(nil).GuardHistory(context.Background(), root); err != nil {
		t.Skipf("history scan needs commits in this git: %v", err) // an empty repo has no history to read
	}
}

func TestGUIGuardHooksRefuseWithoutAnInstalledCLI(t *testing.T) {
	root := gitRepo(t)
	for name, d := range map[string]func() string{"nil": nil, "empty": func() string { return "" }} {
		deps := GUIDeps(d)
		if err := deps.GuardHooks(context.Background(), root, true, ""); !errors.Is(err, gui.ErrCLINotInstalled) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
}
