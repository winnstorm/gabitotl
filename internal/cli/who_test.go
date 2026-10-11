package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/winnstorm/gabitotl/internal/coordination"
	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/memory"
	"github.com/winnstorm/gabitotl/internal/memory/mcp"
)

// activeProvider is a stub memory-mcp provider: it also answers ActiveWork.
type activeProvider struct {
	stubProvider
	entries []memory.Entry
	err     error
	q       mcp.ActiveQuery
}

func (a *activeProvider) ActiveWork(_ context.Context, q mcp.ActiveQuery) ([]memory.Entry, error) {
	a.q = q
	return a.entries, a.err
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.name=T", "-c", "user.email=t@example.com"}, args...)
	if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// whoProject is a real git repository with an initialized project (gabi is the
// active identity) and a second worktree on feature/pay.
func whoProject(t *testing.T) (root, other string, layout gabitotl.Layout) {
	t.Helper()
	t.Setenv("GABITOTL_USER", "gabi")
	base := t.TempDir()
	root = filepath.Join(base, "app")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, root, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, root, "add", "a.txt")
	gitIn(t, root, "commit", "-q", "-m", "chore: init")
	if r := runIn(t, "", false, t.TempDir(), "init", "--root", root, "--name", "Shop", "--kind", "new",
		"--user-id", "gabi", "--user-name", "Gabi", "--yes"); r.code != 0 {
		t.Fatalf("init: %s", r.err)
	}
	other = filepath.Join(base, "app-wt")
	gitIn(t, root, "worktree", "add", "-q", "-b", "feature/pay", other)
	return root, other, gabitotl.NewLayout(root)
}

func TestWhoMergesScopesAndDeduplicates(t *testing.T) {
	root, other, layout := whoProject(t)
	pid := os.Getpid()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	must(coordination.Register(layout, coordination.Session{User: "gabi", PID: pid, Task: "auth"}, now.Add(-5*time.Minute)))
	must(coordination.Register(layout, coordination.Session{User: "carol", PID: pid}, now.Add(-9*time.Minute)))
	// Bob's session lives in the other worktree's own sessions directory.
	must(coordination.Register(gabitotl.NewLayout(other), coordination.Session{User: "bob", PID: pid, Task: "pay"}, now.Add(-20*time.Minute)))
	// The same person and task also shows up as pushed state.
	must(coordination.WriteTask(layout, coordination.Task{ActiveWork: coordination.ActiveWork{
		ID: "pay", Owner: "bob", Status: coordination.StatusInProgress, Branch: "feature/pay", Areas: []string{"src/pay"}}}))
	must(coordination.WriteTask(layout, coordination.Task{ActiveWork: coordination.ActiveWork{
		ID: "old", Owner: "bob", Status: coordination.StatusDone}}))
	ap := &activeProvider{entries: []memory.Entry{
		{Actor: "bob", Task: "pay", Branch: "feature/pay", Timestamp: now},
		{Actor: "dana", Task: "ui", Branch: "feature/ui", Files: []string{"web"}, Timestamp: now.Add(-time.Hour)},
		{Actor: memory.ActorLegacy, Task: "x"},
	}}
	withProvider(t, ap)

	r := runIn(t, "", false, root, "who", "--json")
	if r.code != 0 || r.err != "" {
		t.Fatalf("%+v", r)
	}
	var got struct {
		Self   string                  `json:"self"`
		People []coordination.Presence `json:"people"`
	}
	if err := json.Unmarshal([]byte(r.out), &got); err != nil {
		t.Fatalf("%v\n%s", err, r.out)
	}
	byKey := map[string]coordination.Presence{}
	for _, p := range got.People {
		if _, dup := byKey[p.User+"/"+p.Task]; dup {
			t.Fatalf("duplicate row for %s/%s: %+v", p.User, p.Task, got.People)
		}
		byKey[p.User+"/"+p.Task] = p
	}
	if got.Self != "gabi" || len(got.People) != 4 {
		t.Fatalf("self=%q people=%+v", got.Self, got.People)
	}
	if p := byKey["gabi/auth"]; !p.Own || p.Scope != coordination.ScopeDirectory || p.Branch != "main" {
		t.Fatalf("own row (branch comes from the worktree list): %+v", p)
	}
	if p := byKey["carol/"]; p.Scope != coordination.ScopeDirectory || p.Source != coordination.SourceSession {
		t.Fatalf("carol: %+v", p)
	}
	// Bob: the worktree session wins, the work file only contributes areas.
	if p := byKey["bob/pay"]; p.Scope != coordination.ScopeWorktree || p.Source != coordination.SourceSession ||
		p.Worktree != other || p.Branch != "feature/pay" || strings.Join(p.Areas, ",") != "src/pay" {
		t.Fatalf("bob: %+v", p)
	}
	if p := byKey["dana/ui"]; p.Scope != coordination.ScopeRemote || p.Source != coordination.SourceMemory || p.LastSeen.IsZero() {
		t.Fatalf("dana: %+v", p)
	}
	if ap.q.SinceMinutes != 1440 || ap.q.Limit != 20 {
		t.Fatalf("query = %+v", ap.q)
	}
}

func TestWhoTableAndNoMemory(t *testing.T) {
	root, _, layout := whoProject(t)
	if err := coordination.Register(layout, coordination.Session{User: "carol", PID: os.Getpid(), Task: "auth"}, time.Now().Add(-3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	ap := &activeProvider{err: errors.New("boom")}
	withProvider(t, ap)
	r := runIn(t, "", false, root, "who", "--no-memory")
	if r.code != 0 || r.err != "" || !strings.Contains(r.out, "USER") || !strings.Contains(r.out, "carol") ||
		!strings.Contains(r.out, "directory") || !strings.Contains(r.out, "session") || !strings.Contains(r.out, "3 min ago") {
		t.Fatalf("%+v", r)
	}
	if ap.q != (mcp.ActiveQuery{}) {
		t.Fatal("--no-memory must not query memory")
	}
}

func TestWhoMemoryFailureIsAWarning(t *testing.T) {
	root, _, layout := whoProject(t)
	if err := coordination.Register(layout, coordination.Session{User: "carol", PID: os.Getpid()}, time.Now()); err != nil {
		t.Fatal(err)
	}
	withProvider(t, &activeProvider{err: errors.New("server unreachable")})
	r := runIn(t, "", false, root, "who")
	if r.code != 0 || !strings.Contains(r.err, "warning: shared memory unavailable: server unreachable") || !strings.Contains(r.out, "carol") {
		t.Fatalf("a failing source must only warn: %+v", r)
	}
	// Engram has no ActiveWork: skipped without a word.
	withProvider(t, &stubProvider{})
	r = runIn(t, "", false, root, "who")
	if r.code != 0 || r.err != "" || !strings.Contains(r.out, "carol") {
		t.Fatalf("%+v", r)
	}
}

func TestWhoOutsideGitStillWorks(t *testing.T) {
	t.Setenv("GABITOTL_USER", "gabi")
	root := initProject(t) // not a Git repository: worktrees become a warning
	withProvider(t, &stubProvider{})
	r := runIn(t, "", false, root, "who", "--json")
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	var got struct {
		People   []any    `json:"people"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(r.out), &got); err != nil || got.People == nil {
		t.Fatalf("people must be an array, not null: %v\n%s", err, r.out)
	}
}
