package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fake implements every port in memory and records the calls in order.
type fake struct {
	calls   []string
	files   map[string]bool
	path    string
	reg     *Registration
	links   map[string]Shortcut
	failOn  string // call name that returns an error
	copied  int64
	removed []string
}

func newFake() *fake {
	return &fake{files: map[string]bool{}, links: map[string]Shortcut{}, copied: 2048}
}

func (f *fake) rec(name string) error {
	f.calls = append(f.calls, name)
	if f.failOn == name {
		return errors.New("boom")
	}
	return nil
}

func (f *fake) CopyFile(src, dst string) (int64, error) {
	if err := f.rec("copy"); err != nil {
		return 0, err
	}
	f.files[dst] = true
	return f.copied, nil
}
func (f *fake) Exists(p string) bool { return f.files[p] }
func (f *fake) RemoveAll(p string) error {
	f.removed = append(f.removed, p)
	for k := range f.files {
		if k == p || strings.HasPrefix(k, p+string(filepath.Separator)) {
			delete(f.files, k)
		}
	}
	for k := range f.links {
		if strings.HasPrefix(k, p+string(filepath.Separator)) {
			delete(f.links, k)
			delete(f.files, k)
		}
	}
	return f.rec("removeall")
}
func (f *fake) ScheduleRemoval(string) error { return f.rec("schedule") }
func (f *fake) Get() (string, error)         { return f.path, f.rec("path.get") }
func (f *fake) Set(v string) error {
	if err := f.rec("path.set"); err != nil {
		return err
	}
	f.path = v
	return nil
}
func (f *fake) Create(p string, s Shortcut) error {
	if err := f.rec("link:" + filepath.Base(filepath.Dir(p)) + "/" + filepath.Base(p)); err != nil {
		return err
	}
	f.links[p] = s
	f.files[p] = true
	return nil
}
func (f *fake) Remove(p string) error {
	delete(f.links, p)
	delete(f.files, p)
	return f.rec("unlink")
}
func (f *fake) Register(r Registration) error {
	if err := f.rec("register"); err != nil {
		return err
	}
	f.reg = &r
	return nil
}
func (f *fake) Unregister() error {
	f.reg = nil
	return f.rec("unregister")
}
func (f *fake) Registered() (string, bool) {
	if f.reg == nil {
		return "", false
	}
	return f.reg.DisplayVersion, true
}

func newInstaller(f *fake) *Installer {
	return &Installer{
		Env: Env{
			ExePath:           filepath.Join("src", ManagerExe),
			DefaultInstallDir: filepath.Join("local", "Programs", "GabitoTL"),
			StartMenuPrograms: "start",
			DesktopDir:        "desk",
			Today:             func() string { return "20261019" },
		},
		FS: f, Path: f, Links: f, Reg: f,
	}
}

func ids(ss []Step) []string {
	var out []string
	for _, s := range ss {
		out = append(out, s.ID)
	}
	return out
}

func TestPlanContents(t *testing.T) {
	in := newInstaller(newFake())
	cases := []struct {
		name string
		o    Options
		want []string
	}{
		{"minimal", Options{}, []string{StepCopy, StepRegister}},
		{"path", Options{AddToPath: true}, []string{StepCopy, StepPath, StepRegister}},
		{"start menu", Options{StartMenu: true}, []string{StepCopy, StepStartMenu, StepRegister}},
		{"everything", Options{AddToPath: true, StartMenu: true, DesktopShortcut: true},
			[]string{StepCopy, StepPath, StepStartMenu, StepDesktop, StepRegister}},
	}
	for _, c := range cases {
		if got := ids(in.Plan(c.o)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: plan %v, want %v", c.name, got, c.want)
		}
	}
	for _, s := range in.Plan(Options{}) {
		if s.Title == "" {
			t.Errorf("step %s has no title", s.ID)
		}
	}
}

func TestApplyOrderAndRegistration(t *testing.T) {
	f := newFake()
	in := newInstaller(f)
	o := Options{Version: "1.2.3", AddToPath: true, StartMenu: true, DesktopShortcut: true}
	var seen []string
	if err := in.Apply(o, func(s Step) { seen = append(seen, s.ID) }); err != nil {
		t.Fatal(err)
	}
	if want := ids(in.Plan(o)); !reflect.DeepEqual(seen, want) {
		t.Fatalf("progress %v, want %v", seen, want)
	}
	dir := in.Env.DefaultInstallDir
	if !HasPathEntry(f.path, dir) {
		t.Fatalf("PATH %q lacks %q", f.path, dir)
	}
	r := f.reg
	if r == nil || r.DisplayVersion != "1.2.3" || r.Publisher != "GabitoTL" || r.InstallDate != "20261019" {
		t.Fatalf("bad registration: %+v", r)
	}
	if r.EstimatedSizeKB != 2 {
		t.Fatalf("size %d KB, want 2", r.EstimatedSizeKB)
	}
	if r.InstallLocation != dir || r.DisplayIcon != filepath.Join(dir, ManagerExe) {
		t.Fatalf("bad paths: %+v", r)
	}
	sc := f.links[filepath.Join("start", "GabitoTL", "GabitoTL Manager.lnk")]
	if sc.Target != filepath.Join(dir, ManagerExe) || sc.WorkDir != dir {
		t.Fatalf("bad start menu shortcut: %+v", sc)
	}
	if _, ok := f.links[filepath.Join("desk", "GabitoTL Manager.lnk")]; !ok {
		t.Fatal("desktop shortcut missing")
	}
}

func TestApplyCopiesCLIWhenPresent(t *testing.T) {
	f := newFake()
	f.files[filepath.Join("src", CLIExe)] = true
	in := newInstaller(f)
	if err := in.Apply(Options{}, nil); err != nil {
		t.Fatal(err)
	}
	if !f.files[filepath.Join(in.Env.DefaultInstallDir, CLIExe)] {
		t.Fatal("CLI was not copied")
	}
	if f.reg.EstimatedSizeKB != 4 {
		t.Fatalf("size %d KB, want 4 (two files)", f.reg.EstimatedSizeKB)
	}
}

func TestApplyIdempotentWhenRunningFromInstallDir(t *testing.T) {
	f := newFake()
	in := newInstaller(f)
	in.Env.ExePath = filepath.Join(in.Env.DefaultInstallDir, ManagerExe)
	for i := 0; i < 2; i++ {
		if err := in.Apply(Options{AddToPath: true}, nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range f.calls {
		if c == "copy" {
			t.Fatal("must not copy the exe onto itself")
		}
	}
	if n := strings.Count(f.path, "GabitoTL"); n != 1 {
		t.Fatalf("PATH has %d entries after two installs: %q", n, f.path)
	}
}

func TestApplyRollsBackOnFailure(t *testing.T) {
	f := newFake()
	f.path = `C:\Existing`
	f.failOn = "register"
	in := newInstaller(f)
	err := in.Apply(Options{AddToPath: true, StartMenu: true, DesktopShortcut: true}, nil)
	if err == nil || !strings.Contains(err.Error(), "Apps & features") {
		t.Fatalf("want wrapped register error, got %v", err)
	}
	if f.path != `C:\Existing` {
		t.Fatalf("PATH not restored: %q", f.path)
	}
	if len(f.links) != 0 {
		t.Fatalf("shortcuts left behind: %v", f.links)
	}
	if f.files[filepath.Join(in.Env.DefaultInstallDir, ManagerExe)] {
		t.Fatal("fresh install dir not removed")
	}
}

func TestRollbackKeepsPreexistingInstallDir(t *testing.T) {
	f := newFake()
	dir := newInstaller(f).Env.DefaultInstallDir
	f.files[dir] = true
	f.failOn = "path.set"
	in := newInstaller(f)
	if err := in.Apply(Options{AddToPath: true}, nil); err == nil {
		t.Fatal("want error")
	}
	for _, r := range f.removed {
		if r == dir {
			t.Fatal("rollback deleted a directory that existed before the install")
		}
	}
}

func TestApplyStopsAtFirstFailure(t *testing.T) {
	f := newFake()
	f.failOn = "copy"
	in := newInstaller(f)
	if err := in.Apply(Options{AddToPath: true}, nil); err == nil {
		t.Fatal("want error")
	}
	for _, c := range f.calls {
		if strings.HasPrefix(c, "path") || c == "register" {
			t.Fatalf("step ran after failure: %v", f.calls)
		}
	}
}

func TestUninstall(t *testing.T) {
	f := newFake()
	in := newInstaller(f)
	o := Options{AddToPath: true, StartMenu: true, DesktopShortcut: true}
	if err := in.Apply(o, nil); err != nil {
		t.Fatal(err)
	}
	f.calls = nil
	var seen []string
	if err := in.Uninstall(Options{}, func(s Step) { seen = append(seen, s.ID) }); err != nil {
		t.Fatal(err)
	}
	if want := ids(in.UninstallPlan(Options{})); !reflect.DeepEqual(seen, want) {
		t.Fatalf("progress %v, want %v", seen, want)
	}
	if HasPathEntry(f.path, in.Env.DefaultInstallDir) || f.reg != nil || len(f.links) != 0 {
		t.Fatalf("leftovers: path=%q reg=%v links=%v", f.path, f.reg, f.links)
	}
	if f.calls[len(f.calls)-1] != "schedule" {
		t.Fatalf("directory removal must come last: %v", f.calls)
	}
}

func TestUninstallIsBestEffort(t *testing.T) {
	f := newFake()
	f.failOn = "path.get"
	in := newInstaller(f)
	f.reg = &Registration{}
	err := in.Uninstall(Options{}, nil)
	if err == nil {
		t.Fatal("want the PATH error reported")
	}
	if f.reg != nil {
		t.Fatal("later steps must still run after a failure")
	}
	if f.calls[len(f.calls)-1] != "schedule" {
		t.Fatalf("removal not scheduled: %v", f.calls)
	}
}

func TestStatus(t *testing.T) {
	f := newFake()
	in := newInstaller(f)
	if s := in.Status(Options{}); s.Installed() || s.OnPath || s.StartMenu || s.DesktopLink {
		t.Fatalf("empty machine reported as installed: %+v", s)
	}
	if err := in.Apply(Options{Version: "9", AddToPath: true, StartMenu: true}, nil); err != nil {
		t.Fatal(err)
	}
	s := in.Status(Options{})
	want := Status{InstallDir: in.Env.DefaultInstallDir, DirExists: true, Registered: true, Version: "9", OnPath: true, StartMenu: true}
	if s != want {
		t.Fatalf("got %+v, want %+v", s, want)
	}
	if !s.Installed() {
		t.Fatal("Installed() false")
	}
}

func TestUninstallStringQuoting(t *testing.T) {
	dir := filepath.Join("C:", "Users", "Ana Perez", "GabitoTL")
	want := `"` + filepath.Join(dir, ManagerExe) + `" --uninstall`
	if got := UninstallString(dir); got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	if got := ModifyString(dir); !strings.HasPrefix(got, `"`) || !strings.HasSuffix(got, `" --manage`) {
		t.Fatalf("bad modify string %s", got)
	}
}

func TestOSFSCopyAtomicAndReplace(t *testing.T) {
	d := t.TempDir()
	src := filepath.Join(d, "src.exe")
	dst := filepath.Join(d, "out", "app.exe")
	if err := os.WriteFile(src, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	var fs osFS
	for _, body := range []string{"v1", "version-two"} {
		if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		n, err := fs.CopyFile(src, dst)
		if err != nil || n != int64(len(body)) {
			t.Fatalf("copy: n=%d err=%v", n, err)
		}
		if got, _ := os.ReadFile(dst); string(got) != body {
			t.Fatalf("dst = %q", got)
		}
	}
	entries, _ := os.ReadDir(filepath.Dir(dst))
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}
	if _, err := fs.CopyFile(filepath.Join(d, "missing"), dst); err == nil {
		t.Fatal("want error for a missing source")
	}
}
