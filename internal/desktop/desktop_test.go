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
	sched   []string
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
func (f *fake) WriteFile(dst string, data []byte) (int64, error) {
	if err := f.rec("write"); err != nil {
		return 0, err
	}
	f.files[dst] = true
	return int64(len(data)), nil
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
func (f *fake) ScheduleRemoval(_ string, files []string) error {
	f.sched = files
	return f.rec("schedule")
}
func (f *fake) Get() (string, error) { return f.path, f.rec("path.get") }
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

func abs(p string) string {
	a, _ := filepath.Abs(p)
	return a
}

func mustPlan(t *testing.T, in *Installer, o Options) []Step {
	t.Helper()
	ss, err := in.Plan(o)
	if err != nil {
		t.Fatal(err)
	}
	return ss
}

func mustUPlan(t *testing.T, in *Installer) []Step {
	t.Helper()
	ss, err := in.UninstallPlan(Options{})
	if err != nil {
		t.Fatal(err)
	}
	return ss
}

func newInstaller(f *fake) *Installer {
	return &Installer{
		Env: Env{
			ExePath:           abs(filepath.Join("src", ManagerExe)),
			DefaultInstallDir: abs(filepath.Join("local", "Programs", "GabitoTL")),
			StartMenuPrograms: abs("start"),
			DesktopDir:        abs("desk"),
			Protected:         []string{abs("home"), abs("local")},
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
		if got := ids(mustPlan(t, in, c.o)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: plan %v, want %v", c.name, got, c.want)
		}
	}
	for _, s := range mustPlan(t, in, Options{}) {
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
	if want := ids(mustPlan(t, in, o)); !reflect.DeepEqual(seen, want) {
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
	sc := f.links[abs(filepath.Join("start", "GabitoTL", "GabitoTL Manager.lnk"))]
	if sc.Target != filepath.Join(dir, ManagerExe) || sc.WorkDir != dir {
		t.Fatalf("bad start menu shortcut: %+v", sc)
	}
	if _, ok := f.links[abs(filepath.Join("desk", "GabitoTL Manager.lnk"))]; !ok {
		t.Fatal("desktop shortcut missing")
	}
}

func TestApplyCopiesCLIWhenPresent(t *testing.T) {
	f := newFake()
	f.files[abs(filepath.Join("src", CLIExe))] = true
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
	if want := ids(mustUPlan(t, in)); !reflect.DeepEqual(seen, want) {
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

func TestPathRollbackSkippedWhenNothingChanged(t *testing.T) {
	f := newFake()
	in := newInstaller(f)
	f.path = in.Env.DefaultInstallDir // already on PATH: add is a no-op
	f.failOn = "register"
	if err := in.Apply(Options{AddToPath: true}, nil); err == nil {
		t.Fatal("want error")
	}
	for _, c := range f.calls {
		if c == "path.set" {
			t.Fatalf("PATH was written although nothing changed: %v", f.calls)
		}
	}
}

func TestUninstallLeavesForeignFilesAlone(t *testing.T) {
	f := newFake()
	in := newInstaller(f)
	if err := in.Apply(Options{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := in.Uninstall(Options{}, nil); err != nil {
		t.Fatal(err)
	}
	dir := in.Env.DefaultInstallDir
	want := []string{filepath.Join(dir, ManagerExe), filepath.Join(dir, CLIExe)}
	if !reflect.DeepEqual(f.sched, want) {
		t.Fatalf("scheduled %v, want only %v", f.sched, want)
	}
	for _, r := range f.removed {
		if r == dir {
			t.Fatal("uninstall must not RemoveAll the install dir")
		}
	}
}

func TestUnsafeDirsAreRefused(t *testing.T) {
	f := newFake()
	in := newInstaller(f)
	cases := map[string]string{
		"empty-ish":     "  ",
		"relative":      "rel/dir",
		"root":          abs(string(filepath.Separator)),
		"home":          abs("home"),
		"home case":     strings.ToUpper(abs("home")),
		"home trailing": abs("home") + string(filepath.Separator),
		"local":         abs("local"),
		"ancestor":      filepath.Dir(abs("home")),
		"quote":         abs(`x"y`),
		"percent":       abs("x%PATH%"),
		"ampersand":     abs("a&b"),
		"pipe":          abs("a|b"),
		"lt":            abs("a<b"),
		"gt":            abs("a>b"),
		"caret":         abs("a^b"),
	}
	for name, dir := range cases {
		o := Options{InstallDir: dir}
		var ue *UnsafeDirError
		if err := in.Apply(o, nil); !errors.As(err, &ue) {
			t.Errorf("%s: Apply err %v, want UnsafeDirError", name, err)
		}
		if err := in.Uninstall(o, nil); !errors.As(err, &ue) {
			t.Errorf("%s: Uninstall err %v, want UnsafeDirError", name, err)
		}
		if _, err := in.Plan(o); err == nil {
			t.Errorf("%s: Plan accepted", name)
		}
	}
	if len(f.calls) != 0 {
		t.Fatalf("an unsafe dir touched the system: %v", f.calls)
	}
	if err := in.Apply(Options{InstallDir: abs("home/GabitoTL")}, nil); err != nil {
		t.Fatalf("a subfolder of a protected dir must be allowed: %v", err)
	}
}

func TestEnvValidate(t *testing.T) {
	in := newInstaller(newFake())
	in.Env.DefaultInstallDir = filepath.Join("Programs", "GabitoTL") // empty LOCALAPPDATA
	if _, err := in.Plan(Options{}); err == nil {
		t.Fatal("Plan accepted a relative default dir")
	}
	if err := in.Apply(Options{}, nil); err == nil {
		t.Fatal("Apply accepted a relative default dir")
	}
	if err := in.Uninstall(Options{}, nil); err == nil {
		t.Fatal("Uninstall accepted a relative default dir")
	}
	in = newInstaller(newFake())
	in.Env.DesktopDir = ""
	if _, err := in.Plan(Options{}); err == nil {
		t.Fatal("Plan accepted an empty desktop dir")
	}
}

func TestRemovalCommandLine(t *testing.T) {
	dir := `C:\Users\Ana Perez\AppData\Local\Programs\GabitoTL`
	files := []string{dir + `\gabitotl-manager.exe`, dir + `\gabitotl.exe`}
	got := RemovalCommandLine(dir, files)
	for _, want := range []string{
		`cmd /d /s /c "`,
		`del /f /q "` + files[0] + `" "` + files[1] + `"`,
		`rmdir "` + dir + `"`,
		`for /l %i in (1,1,5)`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "rmdir /s") || strings.Contains(got, "rd /s") {
		t.Fatalf("must not recurse: %s", got)
	}
	if !strings.HasSuffix(got, `"`) || strings.Count(got, `"`)%2 != 0 {
		t.Fatalf("unbalanced quotes: %s", got)
	}
}

func TestApplyWritesEmbeddedCLIWhenNoSibling(t *testing.T) {
	f := newFake()
	in := newInstaller(f)
	in.EmbeddedCLI = []byte("cli")
	if err := in.Apply(Options{}, nil); err != nil {
		t.Fatal(err)
	}
	if !f.files[filepath.Join(in.Env.DefaultInstallDir, CLIExe)] {
		t.Fatal("embedded CLI was not written to the install dir")
	}
}

func TestApplyPrefersSiblingCLIOverEmbedded(t *testing.T) {
	f := newFake()
	in := newInstaller(f)
	in.EmbeddedCLI = []byte("cli")
	f.files[filepath.Join(filepath.Dir(in.Env.ExePath), CLIExe)] = true
	if err := in.Apply(Options{}, nil); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.calls {
		if c == "write" {
			t.Fatal("embedded CLI written although a sibling exists")
		}
	}
}
