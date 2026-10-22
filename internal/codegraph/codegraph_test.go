package codegraph

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

type fakePath struct {
	val  string
	sets int
}

func (f *fakePath) Get() (string, error) { return f.val, nil }
func (f *fakePath) Set(v string) error   { f.val = v; f.sets++; return nil }

type fakeDL struct {
	body  []byte
	err   error
	calls int
	url   string
}

func (f *fakeDL) Download(_ context.Context, url string, dst io.Writer, _ int64) error {
	f.calls++
	f.url = url
	if f.err != nil {
		return f.err
	}
	_, err := dst.Write(f.body)
	return err
}

func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	zw.Close()
	return buf.Bytes()
}

func goodBundle(t *testing.T) []byte {
	return makeZip(t, map[string]string{
		"codegraph-win32-x64/node.exe":                  "node",
		"codegraph-win32-x64/bin/codegraph.cmd":         "@echo off",
		"codegraph-win32-x64/lib/dist/bin/codegraph.js": "js",
	})
}

type rig struct {
	in   *Installer
	dl   *fakeDL
	path *fakePath
	root string
	ver  string // what `codegraph --version` prints; "" means not installed
}

func newRig(t *testing.T, zipBytes []byte) *rig {
	t.Helper()
	r := &rig{dl: &fakeDL{body: zipBytes}, path: &fakePath{val: `C:\Windows`}, root: filepath.Join(t.TempDir(), "codegraph")}
	sum := sha256.Sum256(zipBytes)
	Assets["windows/amd64"] = Asset{Name: "codegraph-win32-x64.zip", SHA256: hex.EncodeToString(sum[:])}
	t.Cleanup(func() { Assets["windows/amd64"] = realAsset })
	r.in = &Installer{
		Ports: Ports{
			Download: r.dl,
			Look:     func(string) (string, error) { return "", os.ErrNotExist },
			Run: func(_ context.Context, bin string, _ ...string) (string, error) {
				if r.ver == "" {
					return "", errors.New("not found")
				}
				return "codegraph " + r.ver + "\n", nil
			},
			Path: r.path,
		},
		GOOS: "windows", GOARCH: "amd64", Root: r.root, Token: func() string { return "t1" },
	}
	return r
}

var realAsset = Assets["windows/amd64"]

func TestPinnedHashesLookRight(t *testing.T) {
	hexRe := regexp.MustCompile(`^[0-9a-f]{64}$`)
	for k, a := range Assets {
		if !hexRe.MatchString(a.SHA256) || !strings.HasSuffix(a.Name, ".zip") || !strings.HasPrefix(k, "windows/") {
			t.Errorf("bad pinned asset %s: %+v", k, a)
		}
	}
	if !strings.HasPrefix(BaseURL, "https://") || !strings.Contains(BaseURL, "v"+Version+"/") {
		t.Errorf("BaseURL %q", BaseURL)
	}
}

func TestInstallFresh(t *testing.T) {
	r := newRig(t, goodBundle(t))
	var steps []string
	res, err := r.in.Install(context.Background(), func(id string) { steps = append(steps, id) })
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != ActionInstall || res.Version != Version {
		t.Fatalf("res %+v", res)
	}
	if got := strings.Join(steps, ","); got != "download,verify,extract,activate,path" {
		t.Errorf("steps %s", got)
	}
	for _, f := range []string{"node.exe", `bin/codegraph.cmd`, `lib/dist/bin/codegraph.js`} {
		if _, err := os.Stat(filepath.Join(r.in.Current(), filepath.FromSlash(f))); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}
	if !strings.HasSuffix(r.dl.url, "/v"+Version+"/codegraph-win32-x64.zip") {
		t.Errorf("url %s", r.dl.url)
	}
	if !strings.Contains(r.path.val, r.in.BinDir()) || r.path.sets != 1 {
		t.Errorf("path %q sets=%d", r.path.val, r.path.sets)
	}
	if _, err := os.Stat(filepath.Join(r.root, Version)); !os.IsNotExist(err) {
		t.Error("stage dir left behind")
	}
}

func TestChecksumMismatchFailsClosed(t *testing.T) {
	r := newRig(t, goodBundle(t))
	r.dl.body = append(r.dl.body, 'x') // tampered after pinning
	_, err := r.in.Install(context.Background(), nil)
	if !errors.Is(err, ErrChecksum) {
		t.Fatalf("err = %v", err)
	}
	if _, e := os.Stat(r.in.Current()); !os.IsNotExist(e) {
		t.Error("current exists after a failed verify")
	}
	if r.path.sets != 0 {
		t.Error("PATH changed after a failed verify")
	}
	ents, _ := os.ReadDir(r.root)
	if len(ents) != 0 {
		t.Errorf("leftovers: %v", ents)
	}
}

func TestZipSlipRejected(t *testing.T) {
	for _, name := range []string{"top/../../evil.txt", "/abs/evil", `top\..\..\evil`, "C:/evil"} {
		r := newRig(t, makeZip(t, map[string]string{name: "x", "top/bin/codegraph.cmd": "x"}))
		if _, err := r.in.Install(context.Background(), nil); err == nil || errors.Is(err, ErrChecksum) {
			t.Errorf("%q: err = %v, want unsafe path", name, err)
		}
		if _, e := os.Stat(r.in.Current()); !os.IsNotExist(e) {
			t.Errorf("%q: installed anyway", name)
		}
		if _, e := os.Stat(filepath.Join(filepath.Dir(r.root), "evil.txt")); e == nil {
			t.Errorf("%q: escaped the stage", name)
		}
	}
}

func TestBundleWithoutLauncherRejected(t *testing.T) {
	r := newRig(t, makeZip(t, map[string]string{"top/node.exe": "x"}))
	if _, err := r.in.Install(context.Background(), nil); err == nil {
		t.Fatal("expected an error")
	}
	if _, e := os.Stat(r.in.Current()); !os.IsNotExist(e) {
		t.Error("installed without a launcher")
	}
}

func TestSkipWhenAlreadyInstalled(t *testing.T) {
	for _, v := range []string{Version, "1.9.0"} {
		r := newRig(t, goodBundle(t))
		r.ver = v
		r.in.Look = func(string) (string, error) { return `C:\tools\codegraph.cmd`, nil }
		res, err := r.in.Install(context.Background(), nil)
		if err != nil || res.Action != ActionSkip || r.dl.calls != 0 || r.path.sets != 0 {
			t.Fatalf("v=%s res=%+v err=%v calls=%d sets=%d", v, res, err, r.dl.calls, r.path.sets)
		}
	}
}

func TestSkipStillAddsMissingPathEntry(t *testing.T) {
	r := newRig(t, goodBundle(t))
	if _, err := r.in.Install(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	r.ver = Version
	r.path.val, r.path.sets = `C:\Windows`, 0 // the user removed it
	res, err := r.in.Install(context.Background(), nil)
	if err != nil || res.Action != ActionSkip || r.path.sets != 1 || r.dl.calls != 1 {
		t.Fatalf("res=%+v err=%v sets=%d calls=%d", res, err, r.path.sets, r.dl.calls)
	}
}

func TestPathAddedOnce(t *testing.T) {
	r := newRig(t, goodBundle(t))
	if _, err := r.in.Install(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	r.ver = "1.0.0" // forces an upgrade
	if _, err := r.in.Install(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if r.path.sets != 1 || strings.Count(r.path.val, r.in.BinDir()) != 1 {
		t.Errorf("path %q sets=%d", r.path.val, r.path.sets)
	}
}

func TestUpgradeReplacesAndKeepsLockedFileAside(t *testing.T) {
	r := newRig(t, goodBundle(t))
	cur := r.in.Current()
	os.MkdirAll(filepath.Join(cur, "bin"), 0o755)
	os.WriteFile(filepath.Join(cur, "bin", "codegraph.cmd"), []byte("OLD"), 0o755)
	os.WriteFile(filepath.Join(cur, "stale.dll"), []byte("old"), 0o755)
	r.ver = "1.0.0"
	// A running MCP server locks the directory: renaming it fails.
	r.in.rename = func(a, b string) error {
		if a == cur {
			return errors.New("The process cannot access the file")
		}
		return os.Rename(a, b)
	}
	res, err := r.in.Install(context.Background(), nil)
	if err != nil || res.Action != ActionUpgrade {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	b, _ := os.ReadFile(filepath.Join(cur, "bin", "codegraph.cmd"))
	if string(b) != "@echo off" {
		t.Errorf("launcher = %q", b)
	}
	if _, err := os.Stat(filepath.Join(cur, "stale.dll")); !os.IsNotExist(err) {
		t.Error("old file still in place")
	}
}

func TestUpgradeFailureRestoresPrevious(t *testing.T) {
	r := newRig(t, goodBundle(t))
	cur := r.in.Current()
	os.MkdirAll(cur, 0o755)
	os.WriteFile(filepath.Join(cur, "keep.txt"), []byte("old"), 0o755)
	r.ver = "1.0.0"
	r.in.rename = func(a, b string) error {
		if strings.HasSuffix(a, Version) { // staging into current fails
			return errors.New("boom")
		}
		return os.Rename(a, b)
	}
	if _, err := r.in.Install(context.Background(), nil); err == nil {
		t.Fatal("expected an error")
	}
	if b, err := os.ReadFile(filepath.Join(cur, "keep.txt")); err != nil || string(b) != "old" {
		t.Errorf("previous install not restored: %v %q", err, b)
	}
}

func TestUnsupportedOS(t *testing.T) {
	in := &Installer{GOOS: "linux", GOARCH: "amd64", Root: t.TempDir()}
	if in.Supported() || in.Plan(context.Background()).Action != ActionUnsupported {
		t.Fatal("linux must be unsupported")
	}
	if _, err := in.Install(context.Background(), nil); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v", err)
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{{"1.6.2", "1.6.2", 0}, {"1.6.10", "1.6.2", 1}, {"1.5.9", "1.6.0", -1}, {"2.0.0", "1.99.99", 1}}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%s,%s)=%d", c.a, c.b, got)
		}
	}
	if v, ok := ParseVersion("codegraph v1.6.2 (win32)"); !ok || v != "1.6.2" {
		t.Errorf("ParseVersion = %q", v)
	}
}

func TestHTTPDownloader(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.Write([]byte("hello"))
		case "/big":
			w.Write(make([]byte, 100))
		case "/redir":
			http.Redirect(w, r, "http://example.invalid/x", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	d := NewHTTPDownloaderWith(srv.Client())
	var buf bytes.Buffer
	if err := d.Download(context.Background(), srv.URL+"/ok", &buf, 10); err != nil || buf.String() != "hello" {
		t.Fatalf("ok: %v %q", err, buf.String())
	}
	for _, p := range []string{"/big", "/redir", "/missing"} {
		if err := d.Download(context.Background(), srv.URL+p, io.Discard, 10); err == nil {
			t.Errorf("%s: expected an error", p)
		}
	}
	if err := d.Download(context.Background(), "http://example.com/x", io.Discard, 10); err == nil {
		t.Error("plain http must be refused")
	}
}

func TestManagedInstallWinsOverOlderNpmOnPath(t *testing.T) {
	r := newRig(t, goodBundle(t))
	os.MkdirAll(filepath.Dir(r.in.Launcher()), 0o755)
	os.WriteFile(r.in.Launcher(), []byte("x"), 0o755)
	npm := `C:\npm\codegraph.cmd`
	r.in.Look = func(string) (string, error) { return npm, nil }
	r.in.Run = func(_ context.Context, bin string, _ ...string) (string, error) {
		if bin == npm {
			return "codegraph 1.0.0", nil
		}
		return "1.6.2", nil
	}
	st := r.in.Status(context.Background())
	if st.Version != "1.6.2" || st.Path != r.in.Launcher() || st.Shadow != npm {
		t.Fatalf("%+v", st)
	}
	if pl := r.in.Plan(context.Background()); pl.Action != ActionSkip {
		t.Fatalf("plan %+v", pl)
	}
}

func TestVersionParsingAndPrerelease(t *testing.T) {
	if v, _ := ParseVersion("node v24.1.0 bundled\ncodegraph 1.6.2\n"); v != "24.1.0" && v != "1.6.2" {
		t.Errorf("v=%q", v)
	}
	if v, _ := ParseVersion("CodeGraph (build 2.3.4.5) 1.6.2"); v != "1.6.2" {
		t.Errorf("bare token should win, got %q", v)
	}
	if Compare("1.6.2-rc.1", "1.6.2") != -1 || Compare("1.6.2", "1.6.2-rc.1") != 1 || Compare("1.6.2-rc.1", "1.6.2-rc.1") != 0 {
		t.Error("prerelease must be lower than its release")
	}
}

func TestExtractRejectsWindowsTraps(t *testing.T) {
	cases := []map[string]string{
		{"top/bin/codegraph.cmd": "x", "top/CON.txt": "x"},
		{"top/bin/codegraph.cmd": "x", "top/lpt1": "x"},
		{"top/bin/codegraph.cmd": "x", "top/trail.": "x"},
		{"top/bin/codegraph.cmd": "x", "top/a.TXT ": "x"},
		{"top/bin/codegraph.cmd": "x", "top/Dup.js": "1", "top/dup.JS": "2"},
	}
	for i, c := range cases {
		r := newRig(t, makeZip(t, c))
		if _, err := r.in.Install(context.Background(), nil); err == nil || errors.Is(err, ErrChecksum) {
			t.Errorf("case %d: err=%v", i, err)
		}
		if _, e := os.Stat(r.in.Current()); !os.IsNotExist(e) {
			t.Errorf("case %d: installed", i)
		}
	}
}

func TestSweepAndRollbackLeaveNoStrays(t *testing.T) {
	r := newRig(t, goodBundle(t))
	cur := r.in.Current()
	os.MkdirAll(cur, 0o755)
	os.WriteFile(filepath.Join(cur, "keep.txt"), []byte("old"), 0o755)
	os.WriteFile(filepath.Join(cur, "locked.dll.old-zzz"), []byte("x"), 0o755)
	os.MkdirAll(r.root+"/current.old-prev", 0o755)
	r.ver = "1.0.0"
	moved := 0
	r.in.rename = func(a, b string) error {
		if a == cur {
			return errors.New("locked") // force the merge path
		}
		if strings.Contains(a, Version) && !strings.Contains(b, ".old-") {
			moved++
			if moved == 2 { // fail midway through moving the new files in
				return errors.New("boom")
			}
		}
		return os.Rename(a, b)
	}
	if _, err := r.in.Install(context.Background(), nil); err == nil {
		t.Fatal("expected failure")
	}
	if b, _ := os.ReadFile(filepath.Join(cur, "keep.txt")); string(b) != "old" {
		t.Error("previous file not restored")
	}
	if _, err := os.Stat(r.root + "/current.old-prev"); !os.IsNotExist(err) {
		t.Error("old aside dir not swept")
	}
	if _, err := os.Stat(filepath.Join(cur, "node.exe")); !os.IsNotExist(err) {
		t.Error("stray new file left after rollback")
	}
}
