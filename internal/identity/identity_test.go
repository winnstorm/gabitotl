package identity

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

func TestSlug(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Gabi", "gabi"},
		{"Gabriela Pérez", "gabriela-perez"},
		{"  Gonza_López  ", "gonza-lopez"},
		{"J. R. R. Tolkien", "j-r-r-tolkien"},
		{"../../etc/passwd", "etc-passwd"},
		{"!!!", ""},
	}
	for _, tc := range tests {
		if got := Slug(tc.in); got != tc.want {
			t.Errorf("Slug(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNew(t *testing.T) {
	tests := []struct {
		name, id, in, display string
		want                  User
		wantErr               bool
	}{
		{"derive id", "", "Gabi Pérez", "", User{"gabi-perez", "Gabi Pérez", "Gabi Pérez"}, false},
		{"explicit id", "Gabi", "Gabriela", "Gabi", User{"gabi", "Gabriela", "Gabi"}, false},
		{"empty name", "x", "  ", "", User{}, true},
		{"bad id", "!!!", "Name", "", User{}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := New(tc.id, tc.in, tc.display)
			if (err != nil) != tc.wantErr || got != tc.want {
				t.Fatalf("got %+v, %v", got, err)
			}
		})
	}
}

func TestSaveAndLoad(t *testing.T) {
	root := t.TempDir()
	gabi, _ := New("gabi", "Gabriela", "Gabi")
	gonza, _ := New("gonza", "Gonzalo", "")
	for _, u := range []User{gabi, gonza} {
		if err := Save(root, u); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := Load(root, "gabi"); err != nil || got != gabi {
		t.Fatalf("Load = %+v, %v", got, err)
	}
	l := gabitotl.NewLayout(root)
	for _, p := range []string{filepath.Join(l.Users(), "gabi.md"), filepath.Join(l.Users(), "gonza.md")} {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "@") {
			t.Errorf("%s must not contain an email", p)
		}
	}
	// Save no longer writes a local identity file.
	if _, err := os.Stat(filepath.Join(l.Local(), "user.md")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("local/user.md must not be written: %v", err)
	}
}

func hermeticGit(t *testing.T, body string) {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

func TestSlugOSName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"gabi", "gabi"},
		{`CORP\Gabi`, "gabi"},
		{"Gabi@corp.example", "gabi"},
		{`DOM\Gabi.Perez`, "gabi-perez"},
		{"  ", ""},
	}
	for _, tc := range tests {
		if got := slugOSName(tc.in); got != tc.want {
			t.Errorf("slugOSName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestActiveFromEnvAutoCreatesProfile(t *testing.T) {
	hermeticGit(t, "[user]\n\tname = Gabi Pérez\n")
	t.Setenv(EnvUser, "Gabi")
	root := t.TempDir()
	if err := os.MkdirAll(gabitotl.NewLayout(root).Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	u, err := Active(root)
	if err != nil || u.ID != "gabi" || u.Name != "Gabi Pérez" {
		t.Fatalf("Active = %+v, %v", u, err)
	}
	if got, err := Load(root, "gabi"); err != nil || got != u {
		t.Fatalf("profile not created: %+v, %v", got, err)
	}
	// Idempotent: a hand-edited profile and its body are left alone.
	p, _ := gabitotl.NewLayout(root).UserFile("gabi")
	d, _ := gabitotl.ReadDoc(p)
	custom := gabitotl.Doc{Front: d.Front, HasFront: true, Body: "\nNotes by hand\n"}
	if err := os.WriteFile(p, custom.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Active(root); err != nil {
		t.Fatal(err)
	}
	if d, _ := gabitotl.ReadDoc(p); d.Body != "\nNotes by hand\n" {
		t.Errorf("profile rewritten: %q", d.Body)
	}
}

func TestActiveNameFallsBackToID(t *testing.T) {
	hermeticGit(t, "")
	t.Setenv(EnvUser, "ana")
	root := t.TempDir()
	u, err := Active(root) // not an initialized project: nothing is created
	if err != nil || u.ID != "ana" || u.Name != "ana" {
		t.Fatalf("Active = %+v, %v", u, err)
	}
	if _, err := os.Stat(gabitotl.NewLayout(root).Dir()); err == nil {
		t.Error("Active must not create .gabitotl in a non-project")
	}
}

func TestActiveIgnoresLegacyLocalUser(t *testing.T) {
	hermeticGit(t, "")
	t.Setenv(EnvUser, "ana")
	root := t.TempDir()
	l := gabitotl.NewLayout(root)
	legacy := User{ID: "bob", Name: "Bob"}
	if err := gabitotl.WriteDoc(filepath.Join(l.Local(), "user.md"), legacy, "\nold\n"); err != nil {
		t.Fatal(err)
	}
	if u, err := Active(root); err != nil || u.ID != "ana" {
		t.Fatalf("Active = %+v, %v", u, err)
	}
}

func TestActiveNoIDDerivable(t *testing.T) {
	t.Setenv(EnvUser, "")
	t.Setenv("USER", "")
	t.Setenv("USERNAME", "")
	t.Setenv("LOGNAME", "")
	if OSUserID() != "" {
		t.Skip("os/user resolves a name on this platform")
	}
	if _, err := Active(t.TempDir()); !errors.Is(err, ErrNoActive) {
		t.Fatalf("Active = %v", err)
	}
}

func TestSavePreservesBodyAndRejectsInvalid(t *testing.T) {
	root := t.TempDir()
	u, _ := New("gabi", "Gabriela", "")
	if err := Save(root, u); err != nil {
		t.Fatal(err)
	}
	p, _ := gabitotl.NewLayout(root).UserFile("gabi")
	d, _ := gabitotl.ReadDoc(p)
	custom := gabitotl.Doc{Front: d.Front, HasFront: true, Body: "\nNotes by hand\n"}
	if err := os.WriteFile(p, custom.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	u.DisplayName = "Gabi"
	if err := Save(root, u); err != nil {
		t.Fatal(err)
	}
	if d, _ := gabitotl.ReadDoc(p); d.Body != "\nNotes by hand\n" {
		t.Errorf("body lost: %q", d.Body)
	}
	for _, bad := range []User{{ID: "Bad ID", Name: "x"}, {ID: "../x", Name: "x"}, {ID: "ok", Name: ""}} {
		if err := Save(root, bad); err == nil {
			t.Errorf("Save(%+v) should fail", bad)
		}
	}
	if _, err := Load(root, "../x"); err == nil {
		t.Error("Load with traversal must fail")
	}
}
