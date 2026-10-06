package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/coordination"
	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/vcs"
)

// project builds a temp git repo with a committed file and an initialized
// .gabitotl (user gabi, solo mode).
func project(t *testing.T) string {
	t.Helper()
	for k, v := range map[string]string{
		"GIT_AUTHOR_NAME": "T", "GIT_AUTHOR_EMAIL": "t@example.com",
		"GIT_COMMITTER_NAME": "T", "GIT_COMMITTER_EMAIL": "t@example.com",
		"GIT_CONFIG_GLOBAL": "/dev/null", "GIT_CONFIG_SYSTEM": "/dev/null",
	} {
		t.Setenv(k, v)
	}
	root := t.TempDir()
	repo := vcs.New(root, gabitotl.VCSConfig{Permissions: gabitotl.VCSPermissions{Branches: "allow", Commits: "allow", Push: "allow", PullRequests: "allow"}})
	ctx := context.Background()
	if _, err := repo.Git(ctx, "init", "-q", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app.txt"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Commit(ctx, "chore: init", "app.txt"); err != nil {
		t.Fatal(err)
	}
	r := runIn(t, "", false, root, "init", "--root", root, "--name", "Shop", "--kind", "existing", "--user-id", "gabi", "--user-name", "Gabi", "--mode", "solo", "--provider", "engram", "--vcs", "git", "--yes")
	if r.code != 0 {
		t.Fatalf("init: %s", r.err)
	}
	return root
}

func TestVCSStatusCommand(t *testing.T) {
	root := project(t)
	r := runIn(t, "", false, root, "vcs", "status")
	if r.code != 0 || !strings.Contains(r.out, "branch: main") || !strings.Contains(r.out, "provider: local") || !strings.Contains(r.out, "commits=ask") {
		t.Fatalf("status = %+v", r)
	}
	if r := runIn(t, "", false, root, "vcs"); r.code != ExitUsage {
		t.Fatalf("missing subcommand: %+v", r)
	}
	if r := runIn(t, "", false, root, "vcs", "nope"); r.code != ExitUsage {
		t.Fatalf("unknown subcommand: %+v", r)
	}
}

func TestBackupCommands(t *testing.T) {
	root := project(t)
	r := runIn(t, "", false, root, "backup", "create", "--task", "auth-login", "--reason", "rework", "app.txt", "new.txt", ".env")
	if r.code != 0 || !strings.Contains(r.out, "snapshot: gabi/") || !strings.Contains(r.out, "files: app.txt") || !strings.Contains(r.err, "skipped .env") {
		t.Fatalf("create = %+v", r)
	}
	if err := os.WriteFile(filepath.Join(root, "app.txt"), []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := runIn(t, "", false, root, "backup", "list"); r.code != 0 || !strings.Contains(r.out, "auth-login") || !strings.Contains(r.out, "rework") {
		t.Fatalf("list = %+v", r)
	}
	if r := runIn(t, "", false, root, "backup", "restore", "latest", "app.txt"); r.code != 0 || !strings.Contains(r.out, "restored") {
		t.Fatalf("restore = %+v", r)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "app.txt")); string(b) != "v1" {
		t.Fatalf("app.txt = %q", b)
	}
	if r := runIn(t, "", false, root, "backup", "restore", "latest", "../etc/passwd"); r.code == 0 {
		t.Fatalf("traversal must fail: %+v", r)
	}
	if r := runIn(t, "", false, root, "backup", "create", "--task", "x", "../outside"); r.code == 0 {
		t.Fatalf("outside root must fail: %+v", r)
	}
	if r := runIn(t, "", false, root, "backup", "prune"); r.code != 0 || !strings.Contains(r.out, "pruned 0") {
		t.Fatalf("prune = %+v", r)
	}
	if r := runIn(t, "", false, root, "backup", "create", "app.txt"); r.code != ExitUsage {
		t.Fatalf("missing --task: %+v", r)
	}
}

func TestWorktreeCommandRefusesTemp(t *testing.T) {
	root := project(t) // lives under the OS temp dir
	r := runIn(t, "", false, root, "worktree", "add", "--task", "auth", "--branch", "feature/auth")
	if r.code == 0 || !strings.Contains(r.err, "temporary directory") {
		t.Fatalf("add = %+v", r)
	}
	r = runIn(t, "", false, root, "worktree", "list")
	if r.code != 0 || !strings.Contains(r.out, "main") {
		t.Fatalf("list = %+v", r)
	}
	if r := runIn(t, "", false, root, "worktree", "remove", "nope"); r.code == 0 {
		t.Fatalf("remove unknown: %+v", r)
	}
}

func TestWorkOverlapCommand(t *testing.T) {
	root := project(t)
	layout := gabitotl.NewLayout(root)
	for _, w := range []coordination.ActiveWork{
		{ID: "users-refactor", Owner: "gonza", Status: "in-progress", Branch: "feature/users", Areas: []string{"src/users"}},
		{ID: "mine", Owner: "gabi", Status: "in-progress", Areas: []string{"src/users"}},
	} {
		if err := coordination.WriteTask(layout, coordination.Task{ActiveWork: w}); err != nil {
			t.Fatal(err)
		}
	}
	r := runIn(t, "", false, root, "work", "overlap", "--no-memory", "--areas", "src/users/profile")
	if r.code != 0 || !strings.Contains(r.out, "overlap with gonza") || !strings.Contains(r.out, "Gonza") && !strings.Contains(r.out, "gonza está trabajando") {
		t.Fatalf("overlap = %+v", r)
	}
	if strings.Contains(r.out, "overlap with gabi") {
		t.Fatalf("own work must not count: %s", r.out)
	}
	if r := runIn(t, "", false, root, "work", "overlap", "--no-memory", "--areas", "src/billing"); !strings.Contains(r.out, "no overlap found") {
		t.Fatalf("expected none: %+v", r)
	}
	if r := runIn(t, "", false, root, "work", "overlap", "--no-memory", "--json", "--areas", "src/users"); r.code != 0 || !strings.Contains(r.out, `"who": "gonza"`) {
		t.Fatalf("json = %+v", r)
	}
	if r := runIn(t, "", false, root, "work", "overlap"); r.code != ExitUsage {
		t.Fatalf("usage = %+v", r)
	}
	r = runIn(t, "", false, root, "work", "status")
	if r.code != 0 || !strings.Contains(r.out, "gonza/users-refactor [in-progress]") {
		t.Fatalf("status = %+v", r)
	}
}

func TestPersonaPrintCommand(t *testing.T) {
	r := runIn(t, "", false, t.TempDir(), "persona", "print", "--stats")
	if r.code != 0 || !strings.Contains(r.out, "# Gabito TL") || !strings.Contains(r.out, "**Qué hice:**") || !strings.Contains(r.err, "tokens") {
		t.Fatalf("persona = %+v", r)
	}
}
