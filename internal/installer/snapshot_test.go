package installer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func snapshotFor(t *testing.T, f *fixture, o Options) (*Snapshot, Inventory, Plan, error) {
	t.Helper()
	inv, pl := buildPlan(t, f, o)
	s, err := CreateSnapshot(context.Background(), f.env, inv, pl, nil)
	return s, inv, pl, err
}

func TestSnapshotContentsAndEngramOrder(t *testing.T) {
	f := newFixture(t)
	dbBefore := treeHash(t, f.home+"/.engram")
	cfgBefore := slurp(t, f.home+"/.claude/CLAUDE.md")
	f.runner.Calls = nil
	s, _, _, err := snapshotFor(t, f, Options{Source: SourceGentleAI})
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"migration.md", "inventory.json", "manifest.json", "memory/engram-export.json", "gentle-ai/state.json", "gabitotl-state.json"} {
		if rel == "gabitotl-state.json" {
			continue // no ownership state exists yet on a fresh machine
		}
		if _, err := os.Stat(filepath.Join(s.Dir, rel)); err != nil {
			t.Errorf("snapshot lacks %s: %v", rel, err)
		}
	}
	if slurp(t, s.Dir+"/gentle-ai/state.json") != fxState {
		t.Error("Gentle-AI state must be copied byte for byte")
	}
	var claude *ManifestFile
	for i, mf := range s.Manifest.Files {
		if mf.Runtime == "claude-code" && strings.HasSuffix(mf.Path, ".claude/CLAUDE.md") {
			claude = &s.Manifest.Files[i]
		}
	}
	if claude == nil || !claude.Existed || slurp(t, filepath.Join(s.Dir, claude.Copy)) != cfgBefore {
		t.Fatalf("Claude config copy missing or different: %+v", claude)
	}
	// Doctor runs first, then the export; nothing else touches Engram.
	var engramCalls []string
	for _, c := range f.runner.Log() {
		if strings.HasPrefix(c, "engram") {
			engramCalls = append(engramCalls, strings.Fields(c)[1])
		}
	}
	if got := strings.Join(engramCalls, ","); !strings.HasPrefix(got, "doctor") || !strings.Contains(got, "export") || strings.Index(got, "export") < strings.Index(got, "doctor") {
		t.Fatalf("engram calls = %v", engramCalls)
	}
	if treeHash(t, f.home+"/.engram") != dbBefore {
		t.Fatal("invariant c: the Engram data dir must be untouched")
	}
	if !s.Manifest.Engram.Done || s.Manifest.Engram.Observations != 7 {
		t.Fatalf("%+v", s.Manifest.Engram)
	}
	if st, _ := os.Stat(s.Dir); st.Mode().Perm() != 0o700 {
		t.Errorf("snapshot dir is private, got %v", st.Mode().Perm())
	}
	md := slurp(t, s.Dir+"/migration.md")
	if strings.Contains(md, "Gentleman.") || strings.Contains(md, "anthropic") {
		t.Errorf("migration.md must list paths, never file content:\n%s", md)
	}
}

func TestSnapshotRefusesUnhealthyEngramBeforeAnyMutation(t *testing.T) {
	f := newFixture(t)
	f.runner.Handlers["engram doctor"] = func([]string) (string, error) {
		return `{"status":"error","checks":[{"check_id":"db","result":"error","message":"corrupt"}]}`, nil
	}
	homeBefore, rootBefore := treeHashExcluding(t, f.home, ".gabitotl"), treeHash(t, f.root)
	f.runner.Calls = nil
	_, _, _, err := snapshotFor(t, f, Options{})
	if !errors.Is(err, ErrMemoryUnhealthy) {
		t.Fatalf("err = %v", err)
	}
	if f.runner.Count("engram export") != 0 {
		t.Fatal("no export from an unhealthy Engram")
	}
	// Only ~/.gabitotl (GabitoTL's own dir, holding the failed snapshot) may differ.
	if treeHashExcluding(t, f.home, ".gabitotl") != homeBefore || treeHash(t, f.root) != rootBefore {
		t.Fatal("a refused run must not change anything outside ~/.gabitotl")
	}
}

func TestSnapshotFailsClosedWhenExportFails(t *testing.T) {
	f := newFixture(t)
	f.runner.Handlers["engram export"] = func([]string) (string, error) { return "", errors.New("disk full") }
	if _, _, _, err := snapshotFor(t, f, Options{}); !errors.Is(err, ErrExportFailed) {
		t.Fatalf("err = %v", err)
	}
}

func TestRestoreFilesRevertsEditsAndRemovesCreatedFiles(t *testing.T) {
	f := newFixture(t)
	s, _, pl, err := snapshotFor(t, f, Options{})
	if err != nil {
		t.Fatal(err)
	}
	md := f.home + "/.claude/CLAUDE.md"
	orig := slurp(t, md)
	writeFile(t, md, "clobbered")
	created := ""
	for _, mf := range s.Manifest.Files {
		if !mf.Existed {
			created = mf.Path
			writeFile(t, created, "made by a failed run")
			break
		}
	}
	if created == "" {
		t.Fatalf("expected at least one file that did not exist (plan: %d items)", len(pl.Items))
	}
	changed, err := s.RestoreFiles()
	if err != nil || len(changed) < 2 {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if slurp(t, md) != orig {
		t.Fatal("edited file not restored")
	}
	if _, err := os.Stat(created); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("file created by the run must be removed")
	}
}

func TestLoadSnapshotRejectsPathTraversal(t *testing.T) {
	f := newFixture(t)
	for _, id := range []string{"..", "../x", "a/b", ""} {
		if _, err := LoadSnapshot(f.env, id); err == nil {
			t.Errorf("id %q must be refused", id)
		}
	}
}
