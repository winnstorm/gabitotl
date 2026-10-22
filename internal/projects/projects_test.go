package projects

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fake struct {
	name  string
	cands []Candidate
	err   error
}

func (f fake) Name() string { return f.name }
func (f fake) Discover(context.Context) ([]Candidate, error) {
	return f.cands, f.err
}

func TestNormalizeWindows(t *testing.T) {
	cases := []struct{ in, display, key string }{
		{`c:\Users\Me\Repo\`, `C:\Users\Me\Repo`, `c:\users\me\repo`},
		{`C:/Users/me/repo/../repo`, `C:\Users\me\repo`, `c:\users\me\repo`},
		{`\\?\D:\Work`, `D:\Work`, `d:\work`},
	}
	for _, c := range cases {
		d, k, ok := Normalize(c.in, true)
		if !ok || d != c.display || k != c.key {
			t.Errorf("%q -> %q %q %v", c.in, d, k, ok)
		}
	}
	for _, bad := range []string{"", "relative\\x", "/unix/path"} {
		if _, _, ok := Normalize(bad, true); ok {
			t.Errorf("%q must be rejected on Windows", bad)
		}
	}
	if !isRoot(`C:\`, true) || !isRoot("C:", true) || isRoot(`C:\x`, true) || !isRoot("/", false) {
		t.Error("root detection")
	}
	if _, _, ok := Normalize("rel/x", false); ok {
		t.Error("relative path must be rejected")
	}
}

func TestFileURIPath(t *testing.T) {
	cases := map[string]string{
		"file:///c%3A/Users/x/my%20repo": "c:/Users/x/my repo",
		"file:///home/x/repo":            "/home/x/repo",
	}
	for in, want := range cases {
		if got, ok := FileURIPath(in); !ok || got != want {
			t.Errorf("%s -> %q %v", in, got, ok)
		}
	}
	for _, bad := range []string{"vscode-remote://ssh/x", "https://x/y", "file://server/share"} {
		if _, ok := FileURIPath(bad); ok {
			t.Errorf("%s must be rejected", bad)
		}
	}
}

func TestScanMergesFiltersAndSorts(t *testing.T) {
	home := t.TempDir()
	a := filepath.Join(home, "a")
	b := filepath.Join(home, "b")
	for _, d := range []string{filepath.Join(a, ".git"), filepath.Join(b, ".gabitotl")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	old, newer := time.Unix(1000, 0), time.Unix(2000, 0)
	srcs := []Source{
		fake{cands: []Candidate{
			{Path: a, Source: SrcClaudeCode, LastSeen: old},
			{Path: b + string(filepath.Separator), Source: SrcGabitoTL, LastSeen: time.Unix(500, 0)},
			{Path: home, Source: SrcClaudeCode},                  // home itself
			{Path: "/", Source: SrcClaudeCode},                   // root
			{Path: filepath.Join(home, "gone"), Source: SrcKiro}, // missing
			{Path: "relative/dir", Source: SrcKiro},              // not absolute
		}},
		fake{err: os.ErrPermission},
		fake{cands: []Candidate{{Path: a, Source: SrcOpenCode, LastSeen: newer}}},
	}
	got := Scan(context.Background(), srcs, Options{Home: home})
	if len(got) != 2 {
		t.Fatalf("want 2 projects, got %+v", got)
	}
	if got[0].Path != a || got[0].Name != "a" || !got[0].IsGit || got[0].HasGabitoTL ||
		!got[0].LastSeen.Equal(newer) || len(got[0].Sources) != 2 || got[0].Sources[0] != SrcClaudeCode || got[0].Sources[1] != SrcOpenCode {
		t.Fatalf("a = %+v", got[0])
	}
	if got[1].Path != b || !got[1].HasGabitoTL || got[1].IsGit || got[1].LastSeen.IsZero() {
		t.Fatalf("b = %+v", got[1])
	}
}

func TestScanHonoursMax(t *testing.T) {
	home := t.TempDir()
	var cands []Candidate
	for _, n := range []string{"x", "y", "z"} {
		d := filepath.Join(home, n)
		_ = os.Mkdir(d, 0o755)
		cands = append(cands, Candidate{Path: d, Source: SrcKiro})
	}
	if got := Scan(context.Background(), []Source{fake{cands: cands}}, Options{Home: home, Max: 2}); len(got) != 2 {
		t.Fatalf("got %d", len(got))
	}
}
