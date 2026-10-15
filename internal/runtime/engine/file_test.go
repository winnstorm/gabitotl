package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/winnstorm/gabitotl/internal/ownership"
	rt "github.com/winnstorm/gabitotl/internal/runtime"
)

func fileEnv(t *testing.T) (rt.Env, string) {
	t.Helper()
	home := t.TempDir()
	return rt.Env{Home: home, StateDir: filepath.Join(home, ".gabitotl"), GOOS: "linux"}, home
}

func mustNil(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func readStr(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	mustNil(t, err)
	return string(b)
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func applyFile(t *testing.T, env rt.Env, runtime, path, content string) rt.Action {
	t.Helper()
	a := PlanFile(env, runtime, path, content)
	if _, err := Apply(env, runtime, "", []rt.Action{a}); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestPlanFileCreateThenIdempotent(t *testing.T) {
	env, home := fileEnv(t)
	path := filepath.Join(home, "skills", "demo", "SKILL.md")
	a := PlanFile(env, "r1", path, "v1\n")
	if a.Kind != rt.Create || a.Edit.Op != rt.OpWriteFile {
		t.Fatalf("absent file: %+v", a)
	}
	res, err := Apply(env, "r1", "", []rt.Action{a})
	mustNil(t, err)
	if len(res.Applied) != 1 || len(res.Checks) != 1 || !res.Checks[0].OK {
		t.Fatalf("apply result: %+v", res)
	}
	if readStr(t, path) != "v1\n" {
		t.Fatal("content not written")
	}
	if st, err := os.Stat(path); err != nil || st.Mode().Perm() != 0o644 {
		t.Fatalf("file mode: %v %v", st, err)
	}
	if st, err := os.Stat(filepath.Dir(path)); err != nil || st.Mode().Perm() != 0o755 {
		t.Fatalf("dir mode: %v %v", st, err)
	}
	if b := PlanFile(env, "r1", path, "v1\n"); b.Kind != rt.NoAction {
		t.Fatalf("same content must be NO-ACTION: %+v", b)
	}
	for _, c := range Pending([]rt.Action{PlanFile(env, "r1", path, "v1\n")}) {
		if !c.OK {
			t.Fatalf("verify failed: %+v", c)
		}
	}
	st, _ := env.Store().Load()
	i, ok := st.Find(path)
	if !ok || !st.Entries[i].Whole || !st.Entries[i].CreatedFile || st.Entries[i].HashAfter != ownership.Hash([]byte("v1\n")) {
		t.Fatalf("ownership entry: %+v", st.Entries)
	}
	if len(st.Entries[i].Dirs) != 2 {
		t.Fatalf("created dirs should be skills and skills/demo: %v", st.Entries[i].Dirs)
	}
}

func TestPlanFileKeepsUserFile(t *testing.T) {
	env, home := fileEnv(t)
	path := filepath.Join(home, "skills", "demo", "SKILL.md")
	mustNil(t, os.MkdirAll(filepath.Dir(path), 0o755))
	mustNil(t, os.WriteFile(path, []byte("mine\n"), 0o644))
	a := PlanFile(env, "r1", path, "v1\n")
	if a.Kind != rt.Preserve || a.Edit.Op != rt.OpManual {
		t.Fatalf("a user file must be preserved: %+v", a)
	}
	res, err := Apply(env, "r1", "", []rt.Action{a})
	mustNil(t, err)
	if len(res.Manual) != 1 || readStr(t, path) != "mine\n" {
		t.Fatalf("user file touched: %+v", res)
	}
	if _, err := Remove(env, "r1"); err != nil {
		t.Fatal(err)
	}
	if readStr(t, path) != "mine\n" {
		t.Fatal("uninstall removed a user file")
	}
}

func TestIdenticalUserFileIsNotClaimed(t *testing.T) {
	env, home := fileEnv(t)
	path := filepath.Join(home, "skills", "demo", "SKILL.md")
	mustNil(t, os.MkdirAll(filepath.Dir(path), 0o755))
	mustNil(t, os.WriteFile(path, []byte("v1\n"), 0o644))
	// A stale Create plan (the file appeared after planning) must not claim it.
	stale := rt.Action{Kind: rt.Create, Runtime: "r1", Path: path, Target: "file", Edit: rt.Edit{Op: rt.OpWriteFile, Body: "v1\n"}}
	_, err := Apply(env, "r1", "", []rt.Action{stale})
	mustNil(t, err)
	if _, err := Remove(env, "r1"); err != nil {
		t.Fatal(err)
	}
	if !exists(path) {
		t.Fatal("an identical file GabitoTL did not write must survive uninstall")
	}
}

func TestPlanFileUpdatesOurOwnFileAndBacksItUp(t *testing.T) {
	env, home := fileEnv(t)
	path := filepath.Join(home, "skills", "demo", "SKILL.md")
	applyFile(t, env, "r1", path, "v1\n")
	a := PlanFile(env, "r1", path, "v2\n")
	if a.Kind != rt.Modify {
		t.Fatalf("our unchanged file with new content must be MODIFY: %+v", a)
	}
	snap := &rt.Snapshot{Dir: filepath.Join(home, "snap")}
	mustNil(t, Backup(env, snap, []rt.Action{a}))
	if len(snap.Files) != 1 || readStr(t, snap.CopyPath(path)) != "v1\n" {
		t.Fatalf("existing target was not backed up: %+v", snap)
	}
	_, err := Apply(env, "r1", "", []rt.Action{a})
	mustNil(t, err)
	if readStr(t, path) != "v2\n" {
		t.Fatal("not updated")
	}
	st, _ := env.Store().Load()
	if e := st.Entries[0]; e.HashAfter != ownership.Hash([]byte("v2\n")) || !e.CreatedFile {
		t.Fatalf("entry after update: %+v", e)
	}
}

func TestEditedSkillIsKeptOnUpdateAndUninstall(t *testing.T) {
	env, home := fileEnv(t)
	path := filepath.Join(home, "skills", "demo", "SKILL.md")
	applyFile(t, env, "r1", path, "v1\n")
	mustNil(t, os.WriteFile(path, []byte("edited by me\n"), 0o644))
	if a := PlanFile(env, "r1", path, "v2\n"); a.Kind != rt.Preserve || a.Edit.Op != rt.OpManual {
		t.Fatalf("an edited file must not be overwritten: %+v", a)
	}
	res, err := Remove(env, "r1")
	mustNil(t, err)
	if len(res.Removed) != 0 || len(res.Manual) != 1 {
		t.Fatalf("an edited file must be reported, not removed: %+v", res)
	}
	if readStr(t, path) != "edited by me\n" {
		t.Fatal("edited file lost")
	}
	st, _ := env.Store().Load()
	if _, ok := st.Find(path); !ok {
		t.Fatal("the entry must stay so a later uninstall reports it again")
	}
}

func TestRemoveDeletesFileAndOnlyEmptyCreatedDirs(t *testing.T) {
	env, home := fileEnv(t)
	skills := filepath.Join(home, ".claude", "skills")
	a := filepath.Join(skills, "one", "SKILL.md")
	b := filepath.Join(skills, "two", "SKILL.md")
	applyFile(t, env, "r1", a, "one\n")
	applyFile(t, env, "r1", b, "two\n")
	extra := filepath.Join(skills, "two", "notes.txt") // the user added a file next to ours
	mustNil(t, os.WriteFile(extra, []byte("keep"), 0o644))
	res, err := Remove(env, "r1")
	mustNil(t, err)
	if len(res.Removed) != 2 {
		t.Fatalf("removed: %+v", res)
	}
	if exists(a) || exists(b) || exists(filepath.Dir(a)) {
		t.Fatal("our files and the emptied skill directory must be gone")
	}
	if !exists(extra) || !exists(filepath.Dir(b)) {
		t.Fatal("a directory holding a user file must stay")
	}
	st, _ := env.Store().Load()
	if len(st.Entries) != 0 {
		t.Fatalf("state not cleared: %+v", st.Entries)
	}

	// Everything ours: the shared skills directory we created goes away too.
	env2, home2 := fileEnv(t)
	p := filepath.Join(home2, ".claude", "skills", "one", "SKILL.md")
	q := filepath.Join(home2, ".claude", "skills", "two", "SKILL.md")
	applyFile(t, env2, "r1", p, "one\n")
	applyFile(t, env2, "r1", q, "two\n")
	_, err = Remove(env2, "r1")
	mustNil(t, err)
	if exists(filepath.Join(home2, ".claude")) {
		t.Fatal("directories created by GabitoTL must be removed once empty")
	}
	if !exists(home2) {
		t.Fatal("a directory GabitoTL did not create must stay")
	}
}

func TestSharedFileWaitsForLastOwner(t *testing.T) {
	env, home := fileEnv(t)
	path := filepath.Join(home, ".kiro", "skills", "demo", "SKILL.md")
	applyFile(t, env, "kiro-cli", path, "v1\n")
	b := PlanFile(env, "kiro-ide", path, "v1\n")
	if b.Kind != rt.NoAction {
		t.Fatalf("the second variant must find it in place: %+v", b)
	}
	_, err := Apply(env, "kiro-ide", "", []rt.Action{b})
	mustNil(t, err)
	st, _ := env.Store().Load()
	if len(st.Entries) != 1 || len(st.Entries[0].Runtimes) != 2 {
		t.Fatalf("one entry, two owners expected: %+v", st.Entries)
	}
	_, err = Remove(env, "kiro-cli")
	mustNil(t, err)
	if !exists(path) {
		t.Fatal("removing one owner must keep the shared file")
	}
	_, err = Remove(env, "kiro-ide")
	mustNil(t, err)
	if exists(path) {
		t.Fatal("the last owner must remove the file")
	}
}

func TestOldStateWithoutWholeFieldLoads(t *testing.T) {
	env, home := fileEnv(t)
	mustNil(t, os.MkdirAll(env.StateDir, 0o755))
	old := `{"version":1,"entries":[{"runtimes":["claude-code"],"path":"` + filepath.ToSlash(filepath.Join(home, "CLAUDE.md")) +
		`","sections":["persona"],"created_file":true,"hash_before":"","hash_after":"abc","version":"1"}]}`
	mustNil(t, os.WriteFile(filepath.Join(env.StateDir, "state.json"), []byte(old), 0o644))
	st, err := env.Store().Load()
	mustNil(t, err)
	if len(st.Entries) != 1 || st.Entries[0].Whole || len(st.Entries[0].Dirs) != 0 || st.Entries[0].Sections[0] != "persona" {
		t.Fatalf("old state misread: %+v", st.Entries)
	}
}
