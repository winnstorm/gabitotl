package gabitotl_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/persona"
)

func personaProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if _, err := gabitotl.Init(root, gabitotl.InitOptions{}); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestPersonaModeDefaultsToNormalAndReadsWriteNothing(t *testing.T) {
	root, home := personaProject(t), t.TempDir()
	if m := gabitotl.LoadPersonaMode(gabitotl.NewLayout(root), "ana", home); m != persona.ModeNormal {
		t.Fatalf("default = %s", m)
	}
	if _, err := os.Stat(filepath.Join(root, ".gabitotl", "local", "users", "ana", "persona.md")); !os.IsNotExist(err) {
		t.Fatalf("reading must not create persona.md: %v", err)
	}
	if entries, _ := os.ReadDir(home); len(entries) != 0 {
		t.Fatalf("reading must not write in home: %v", entries)
	}
}

func TestPersonaModeRoundTripPerPerson(t *testing.T) {
	root, home := personaProject(t), t.TempDir()
	l := gabitotl.NewLayout(root)
	p, err := gabitotl.SavePersonaMode(root, "ana", home, persona.ModeLite)
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := l.LocalPersona("ana"); p != want {
		t.Fatalf("saved in %s, want %s", p, want)
	}
	if _, err := gabitotl.SavePersonaMode(root, "beto", home, persona.ModeFull); err != nil {
		t.Fatal(err)
	}
	if m := gabitotl.LoadPersonaMode(l, "ana", home); m != persona.ModeLite {
		t.Errorf("ana = %s", m)
	}
	if m := gabitotl.LoadPersonaMode(l, "beto", home); m != persona.ModeFull {
		t.Errorf("beto = %s", m)
	}
	if m := gabitotl.LoadPersonaMode(l, "carla", home); m != persona.ModeNormal {
		t.Errorf("carla = %s", m)
	}
	if entries, _ := os.ReadDir(home); len(entries) != 0 {
		t.Errorf("a project save must not touch home: %v", entries)
	}
	ign := strings.Join(gabitotl.GitignoreEntries, " ")
	if !strings.Contains(ign, "local/") || !strings.HasPrefix(p, filepath.Join(root, ".gabitotl", "local")+string(filepath.Separator)) {
		t.Errorf("persona file %s is not under the git-ignored local/", p)
	}
}

func TestPersonaModeProjectWinsOverHome(t *testing.T) {
	root, home := personaProject(t), t.TempDir()
	l := gabitotl.NewLayout(root)
	if p, err := gabitotl.SavePersonaMode("", "ana", home, persona.ModeFull); err != nil || p != gabitotl.HomePersonaPath(home) {
		t.Fatalf("home save = %s, %v", p, err)
	}
	if m := gabitotl.LoadPersonaMode(l, "ana", home); m != persona.ModeFull {
		t.Fatalf("home fallback = %s", m)
	}
	if _, err := gabitotl.SavePersonaMode(root, "ana", home, persona.ModeLite); err != nil {
		t.Fatal(err)
	}
	if m := gabitotl.LoadPersonaMode(l, "ana", home); m != persona.ModeLite {
		t.Fatalf("project must win, got %s", m)
	}
	if m := gabitotl.LoadPersonaMode(gabitotl.NewLayout(""), "ana", home); m != persona.ModeFull {
		t.Fatalf("outside a project the home file applies, got %s", m)
	}
}

func TestPersonaModeInvalidFileReadsAsNormal(t *testing.T) {
	root, home := personaProject(t), t.TempDir()
	l := gabitotl.NewLayout(root)
	p, _ := l.LocalPersona("ana")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{"---\nmode: caveman\n---\n", "---\nmode: [\n---\n", "no front matter", "---\nmode: lite\n"} {
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if m := gabitotl.LoadPersonaMode(l, "ana", home); m != persona.ModeNormal {
			t.Errorf("%q read as %s", content, m)
		}
	}
	if _, err := gabitotl.SavePersonaMode(root, "ana", home, persona.Mode("bogus")); err == nil {
		t.Error("an invalid mode must not be saved")
	}
}
