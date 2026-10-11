package coordination

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/memory"
	"github.com/winnstorm/gabitotl/internal/vcs"
)

var gitEnv = []string{
	"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@example.com",
	"GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@example.com",
	"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
}

func newRepo(t *testing.T) *vcs.Repo {
	t.Helper()
	base := t.TempDir()
	dir := filepath.Join(base, "app")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := gabitotl.DefaultVCS("git")
	cfg.Permissions = gabitotl.VCSPermissions{Branches: "allow", Commits: "allow", Push: "allow", PullRequests: "allow"}
	r := &vcs.Repo{Dir: dir, Runner: vcs.ExecRunner{Env: gitEnv}, Config: cfg}
	ctx := context.Background()
	if _, err := r.Git(ctx, "init", "-q", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Commit(ctx, "chore: init", "a.txt"); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestWorktreeLifecycle(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	w := &Worktrees{Repo: r, AllowTemp: true}
	wt, err := w.Add(ctx, "gabi", "auth-login", "feature/auth-login")
	if err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(r.Dir)
	want := filepath.Join(filepath.Dir(real), "app-worktrees", "auth-login")
	if wt.Path != want || wt.Branch != "feature/auth-login" || wt.Head == "" {
		t.Fatalf("worktree = %+v, want path %s", wt, want)
	}
	if _, err := w.Add(ctx, "gabi", "auth-login", "feature/other"); !errors.Is(err, ErrWorktreeExists) {
		t.Fatalf("want ErrWorktreeExists, got %v", err)
	}
	// Existing branch is checked out as is; falls back to the user name.
	if _, err := r.Git(ctx, "branch", "feature/pay"); err != nil {
		t.Fatal(err)
	}
	if wt2, err := w.Add(ctx, "gonza", "", "feature/pay"); err != nil || filepath.Base(wt2.Path) != "gonza" {
		t.Fatalf("add by user: %+v %v", wt2, err)
	}
	list, err := w.List(ctx)
	if err != nil || len(list) != 3 || !list[0].Main || list[1].Main {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if err := w.Remove(ctx, "app"); !errors.Is(err, ErrNoWorktree) && !errors.Is(err, ErrMainWorktree) {
		t.Fatalf("main worktree must not be removable, got %v", err)
	}
	if err := w.Remove(ctx, "auth-login"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Fatalf("worktree dir should be gone: %v", err)
	}
	if err := w.Remove(ctx, "missing"); !errors.Is(err, ErrNoWorktree) {
		t.Fatalf("want ErrNoWorktree, got %v", err)
	}
}

func TestWorktreeRefusesTempAndBadInput(t *testing.T) {
	r := newRepo(t) // t.TempDir lives under the OS temp dir
	w := NewWorktrees(r)
	if _, err := w.Add(context.Background(), "gabi", "t", "feature/t"); !errors.Is(err, ErrTempLocation) {
		t.Fatalf("want ErrTempLocation, got %v", err)
	}
	w.AllowTemp = true
	if _, err := w.Add(context.Background(), "gabi", "../x", "b"); !errors.Is(err, gabitotl.ErrInvalidID) {
		t.Fatalf("want ErrInvalidID, got %v", err)
	}
	if _, err := w.Add(context.Background(), "gabi", "t", "-bad"); err == nil {
		t.Fatal("option-looking branch must be rejected")
	}
	r.Config.Permissions.Branches = gabitotl.PermDeny
	if _, err := w.Add(context.Background(), "gabi", "t", "feature/new"); !errors.Is(err, vcs.ErrDenied) {
		t.Fatalf("want ErrDenied, got %v", err)
	}
}

func TestRealPidAlive(t *testing.T) {
	if !processAlive(os.Getpid()) {
		t.Fatal("own pid must be alive")
	}
	if processAlive(2147483646) {
		t.Fatal("unlikely pid must not be alive")
	}
}

func TestTaskStateRoundTrip(t *testing.T) {
	layout := gabitotl.NewLayout(t.TempDir())
	in := Task{
		ActiveWork: ActiveWork{ID: "auth-login", Owner: "gabi", Status: StatusInProgress, Branch: "feature/auth-login", Areas: []string{"src/auth"}},
		State: TaskState{
			Objective: "Completar autenticación básica.",
			Done:      []string{"Login", "errores principales"},
			Todo:      []string{"Logout", "recuperación de contraseña"},
			Next:      "Implementar logout.",
		},
	}
	if err := WriteTask(layout, in); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(layout.Dir(), "work", "gabi", "auth-login.md"))
	for _, want := range []string{"id: auth-login", "owner: gabi", "status: in-progress", "# Objetivo", "## Completado", "## Falta", "## Próximo paso"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("task file missing %q:\n%s", want, raw)
		}
	}
	out, err := ReadTask(layout, "gabi", "auth-login")
	if err != nil {
		t.Fatal(err)
	}
	if out.State.Objective != in.State.Objective || len(out.State.Done) != 2 || out.State.Todo[1] != "recuperación de contraseña" || out.State.Next != "Implementar logout." || out.Branch != in.Branch {
		t.Fatalf("round trip = %+v", out)
	}
	if err := WriteTask(layout, Task{ActiveWork: ActiveWork{ID: "x", Owner: "gabi", Status: "weird"}}); err == nil {
		t.Fatal("invalid status must be rejected")
	}
	if err := WriteTask(layout, Task{ActiveWork: ActiveWork{ID: "../x", Owner: "gabi", Status: StatusDone}}); !errors.Is(err, gabitotl.ErrInvalidID) {
		t.Fatalf("want ErrInvalidID, got %v", err)
	}
	if all, err := ListTasks(layout, ""); err != nil || len(all) != 1 {
		t.Fatalf("list = %v %v", all, err)
	}
	if none, err := ListTasks(layout, "nobody"); err != nil || len(none) != 0 {
		t.Fatalf("list nobody = %v %v", none, err)
	}
}

type fakeMem struct {
	entries []memory.Entry
	err     error
}

func (f fakeMem) Search(context.Context, memory.Query) ([]memory.Entry, error) {
	return f.entries, f.err
}
func (fakeMem) Save(context.Context, memory.Entry) (memory.ID, error)      { return "", nil }
func (fakeMem) Update(context.Context, memory.ID, memory.EntryPatch) error { return nil }
func (fakeMem) SessionSummary(context.Context, memory.Summary) error       { return nil }
func (fakeMem) CurrentProject(context.Context) (string, error)             { return "p", nil }
func (fakeMem) Doctor(context.Context) memory.Report                       { return memory.Report{} }

func TestCheckOverlap(t *testing.T) {
	layout := gabitotl.NewLayout(t.TempDir())
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(WriteTask(layout, Task{ActiveWork: ActiveWork{ID: "users-refactor", Owner: "gonza", Status: StatusInProgress, Branch: "feature/users", Areas: []string{"src/users"}}}))
	must(WriteTask(layout, Task{ActiveWork: ActiveWork{ID: "billing", Owner: "lau", Status: StatusInProgress, Areas: []string{"src/billing"}}}))
	must(WriteTask(layout, Task{ActiveWork: ActiveWork{ID: "old-users", Owner: "pepe", Status: StatusDone, Areas: []string{"src/users"}}}))
	must(WriteTask(layout, Task{ActiveWork: ActiveWork{ID: "mine", Owner: "gabi", Status: StatusInProgress, Areas: []string{"src/users"}}}))

	rep := CheckOverlap(context.Background(), layout, nil, Probe{User: "gabi", Task: "mfa", Areas: []string{"src/users/profile"}, Files: []string{"src/auth/mfa.go"}})
	if len(rep.Findings) != 1 {
		t.Fatalf("findings = %+v", rep.Findings)
	}
	f := rep.Findings[0]
	if f.Who != "gonza" || f.Task != "users-refactor" || f.Branch != "feature/users" || f.Source != "work" || len(f.SharedAreas) != 1 || f.SharedAreas[0] != "src/users/profile" {
		t.Fatalf("finding = %+v", f)
	}

	// Segment boundary: src/user must not match src/users.
	if rep := CheckOverlap(context.Background(), layout, nil, Probe{User: "gabi", Areas: []string{"src/user"}}); len(rep.Findings) != 0 {
		t.Fatalf("prefix without boundary matched: %+v", rep.Findings)
	}

	// Memory adds a person with no work file, and dedupes known ones.
	mem := fakeMem{entries: []memory.Entry{
		{Type: memory.TypeProgress, Actor: "nico", Task: "payments", Branch: "feature/pay", Files: []string{"src/payments/api.go"}},
		{Type: memory.TypeProgress, Actor: "gonza", Title: "working on src/users", Content: "x"},
		{Type: memory.TypeProgress, Actor: "gabi", Files: []string{"src/payments/x.go"}},
		{Type: memory.TypeProgress, Actor: memory.ActorLegacy, Files: []string{"src/payments/x.go"}},
		{Type: memory.TypeDecision, Actor: "zoe", Files: []string{"src/payments/x.go"}},
	}}
	rep = CheckOverlap(context.Background(), layout, mem, Probe{User: "gabi", Areas: []string{"src/users", "src/payments"}})
	who := map[string]string{}
	for _, f := range rep.Findings {
		who[f.Who] = f.Source
	}
	if len(rep.Findings) != 2 || who["nico"] != "memory" || who["gonza"] != "work" {
		t.Fatalf("findings = %+v", rep.Findings)
	}

	// Memory failure is a warning, never an error or a block.
	rep = CheckOverlap(context.Background(), layout, fakeMem{err: errors.New("down")}, Probe{User: "gabi", Areas: []string{"src/users"}})
	if len(rep.Findings) != 1 || len(rep.Warnings) != 1 {
		t.Fatalf("report = %+v", rep)
	}
	if rep := CheckOverlap(context.Background(), layout, nil, Probe{User: "gabi"}); len(rep.Findings) != 0 {
		t.Fatal("no areas means no findings")
	}
}
