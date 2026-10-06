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

func TestSaveActiveLoad(t *testing.T) {
	root := t.TempDir()
	if _, err := Active(root); !errors.Is(err, ErrNoActive) {
		t.Fatalf("Active before save = %v", err)
	}
	gabi, _ := New("gabi", "Gabriela", "Gabi")
	gonza, _ := New("gonza", "Gonzalo", "")
	for _, u := range []User{gabi, gonza} {
		if err := Save(root, u); err != nil {
			t.Fatal(err)
		}
	}
	// Last saved is active; both shared profiles exist.
	if got, err := Active(root); err != nil || got != gonza {
		t.Fatalf("Active = %+v, %v", got, err)
	}
	if got, err := Load(root, "gabi"); err != nil || got != gabi {
		t.Fatalf("Load = %+v, %v", got, err)
	}
	l := gabitotl.NewLayout(root)
	for _, p := range []string{filepath.Join(l.Users(), "gabi.md"), filepath.Join(l.Users(), "gonza.md"), l.LocalUser()} {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "@") {
			t.Errorf("%s must not contain an email", p)
		}
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

func TestSuggestFromGit(t *testing.T) {
	t.Chdir(t.TempDir()) // avoid picking up repository-local git config
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(cfg, []byte("[user]\n\tname = Gabi Pérez\n\temail = secret@example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	u, ok := SuggestFromGit()
	if !ok || u.ID != "gabi-perez" || u.Name != "Gabi Pérez" {
		t.Fatalf("suggest = %+v, %v", u, ok)
	}

	empty := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", empty)
	if _, ok := SuggestFromGit(); ok {
		t.Error("no git user.name must yield ok=false")
	}
}
