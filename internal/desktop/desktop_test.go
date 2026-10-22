package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
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
	removal Removal
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
func (f *fake) ScheduleRemoval(r Removal) error {
	f.sched, f.removal = r.Files, r
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
	f.files[in.Env.ExePath] = true
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
	data := `C:\Users\Ana Perez\AppData\Local\GabitoTL`
	r := Removal{
		Dir: dir, Files: []string{dir + `\gabitotl-manager.exe`, dir + `\gabitotl.exe`},
		DataDir: data, Folders: []string{data + `\webview`, data + `\webview-setup`, data + `\logs`}, WaitPID: 4242,
	}
	got, err := RemovalCommandLine(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`cmd /d /s /c "`,
		`tasklist /fi "PID eq 4242"`,
		`find " 4242 "`,
		`del /f /q "` + r.Files[0] + `" "` + r.Files[1] + `"`,
		`rmdir /s /q "` + data + `\webview"`,
		`rmdir /s /q "` + data + `\webview-setup"`,
		`rmdir /s /q "` + data + `\logs"`,
		`rmdir "` + dir + `" >nul`,
		`rmdir "` + data + `" >nul`,
		`for /l %i in (1,1,60)`,
		`gabitotl-uninstall.log`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	for _, bad := range []string{`rmdir /s /q "` + dir + `"`, `rmdir /s /q "` + data + `"`} {
		if strings.Contains(got, bad) {
			t.Fatalf("install and data dirs must never be removed recursively: %s", got)
		}
	}
	if !strings.HasSuffix(got, `"`) || strings.Count(got, `"`)%2 != 0 {
		t.Fatalf("unbalanced quotes: %s", got)
	}
	if nw, err := RemovalCommandLine(Removal{Dir: dir, Files: r.Files}); err != nil || strings.Contains(nw, "tasklist") {
		t.Fatal("no PID, no wait")
	}
}

func TestRemovalOnlyNamedFoldersInsideDataDir(t *testing.T) {
	f := newFake()
	in := newInstaller(f)
	in.Env.DataDir = abs(filepath.Join("local", "GabitoTL"))
	if err := in.Uninstall(Options{}, nil); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range f.removal.Folders {
		names = append(names, filepath.Base(p))
		if !sameOrInside(p, in.Env.DataDir) {
			t.Fatalf("%s outside the data dir", p)
		}
	}
	if !reflect.DeepEqual(names, dataFolders) || f.removal.DataDir != in.Env.DataDir {
		t.Fatalf("removal = %+v", f.removal)
	}
	in.Env.DataDir = abs(filepath.Join("local", "Gabito&TL"))
	f.removal = Removal{}
	_ = in.Uninstall(Options{}, nil)
	if len(f.removal.Folders) != 0 || f.removal.DataDir != "" {
		t.Fatalf("a data dir with cmd metacharacters must be skipped: %+v", f.removal)
	}
}

// fakeProcs lists a fixed set of copies and records Stop in the fake's call log.
type fakeProcs struct {
	f       *fake
	procs   []Proc
	failPID uint32
}

func (p *fakeProcs) Running(string, []string) ([]Proc, error) { return p.procs, nil }
func (p *fakeProcs) Stop(pr Proc, _ time.Duration) error {
	p.f.calls = append(p.f.calls, "stop")
	if pr.PID == p.failPID {
		return errors.New("access denied")
	}
	return nil
}

func withProcs(f *fake, pids ...uint32) (*Installer, *fakeProcs) {
	in := newInstaller(f)
	fp := &fakeProcs{f: f}
	for _, id := range pids {
		fp.procs = append(fp.procs, Proc{PID: id, Path: filepath.Join(in.Env.DefaultInstallDir, ManagerExe)})
	}
	in.Procs = fp
	return in, fp
}

func TestPlansIncludeCloseStepOnlyWhenCopiesRun(t *testing.T) {
	f := newFake()
	in, fp := withProcs(f)
	if got := ids(mustPlan(t, in, Options{})); got[0] == StepClose {
		t.Fatalf("no copies, no close step: %v", got)
	}
	if got := ids(mustUPlan(t, in)); got[0] == StepClose {
		t.Fatalf("no copies, no close step: %v", got)
	}
	fp.procs = []Proc{{PID: 1}, {PID: 2}, {PID: 3}}
	pl := mustPlan(t, in, Options{})
	if pl[0].ID != StepClose || !strings.Contains(pl[0].Title, "3 running copies") {
		t.Fatalf("plan = %v", pl)
	}
	if up := mustUPlan(t, in); up[0].ID != StepClose {
		t.Fatalf("uninstall plan = %v", up)
	}
}

func TestUninstallStopsCopiesBeforeScheduling(t *testing.T) {
	f := newFake()
	in, _ := withProcs(f, 10, 11)
	if err := in.Uninstall(Options{}, nil); err != nil {
		t.Fatal(err)
	}
	var seq []string
	for _, c := range f.calls {
		if c == "stop" || c == "schedule" {
			seq = append(seq, c)
		}
	}
	if !reflect.DeepEqual(seq, []string{"stop", "stop", "schedule"}) {
		t.Fatalf("order = %v", seq)
	}
}

func TestUninstallFailureToStopStillRunsTheRest(t *testing.T) {
	f := newFake()
	in, fp := withProcs(f, 10, 11)
	fp.failPID = 10
	f.reg = &Registration{}
	err := in.Uninstall(Options{}, nil)
	if err == nil || !strings.Contains(err.Error(), "process 10") || strings.Contains(err.Error(), "process 11") {
		t.Fatalf("want only pid 10 reported, got %v", err)
	}
	if f.reg != nil || f.calls[len(f.calls)-1] != "schedule" {
		t.Fatalf("later steps must run: reg=%v calls=%v", f.reg, f.calls)
	}
}

func TestApplyRefusesToCopyWhenCopiesCannotBeStopped(t *testing.T) {
	f := newFake()
	in, fp := withProcs(f, 10)
	fp.failPID = 10
	err := in.Apply(Options{}, nil)
	if err == nil || !strings.Contains(err.Error(), "nothing was copied") {
		t.Fatalf("err = %v", err)
	}
	for _, c := range f.calls {
		if c == "copy" || c == "write" {
			t.Fatalf("copied despite a running copy: %v", f.calls)
		}
	}
	fp.failPID = 0
	f.calls = nil
	if err := in.Apply(Options{}, nil); err != nil {
		t.Fatal(err)
	}
	if f.calls[0] != "stop" {
		t.Fatalf("stop must precede the copy: %v", f.calls)
	}
}

func TestHungCopies(t *testing.T) {
	now := time.Now()
	ps := []Proc{
		{PID: 1, Started: now.Add(-2 * time.Minute)},                  // hung
		{PID: 2, Started: now.Add(-2 * time.Minute), HasWindow: true}, // fine
		{PID: 3, Started: now.Add(-5 * time.Second)},                  // young
		{PID: 4}, // unknown start
	}
	if got := HungCopies(ps, now); len(got) != 1 || got[0].PID != 1 {
		t.Fatalf("hung = %v", got)
	}
}

func TestOwned(t *testing.T) {
	dir := abs(filepath.Join("Programs", "GabitoTL"))
	exes := ownedFiles(dir)
	if !Owned(strings.ToUpper(exes[0]), dir, exes) || Owned(filepath.Join(dir, "other.exe"), dir, exes) ||
		Owned(filepath.Join(abs("elsewhere"), ManagerExe), dir, exes) {
		t.Fatal("Owned must match only the owned exes inside dir, ignoring case")
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

func TestApplyPrefersEmbeddedCLIOverStaleSibling(t *testing.T) {
	f := newFake()
	in := newInstaller(f)
	in.EmbeddedCLI = []byte("cli")
	f.files[filepath.Join(filepath.Dir(in.Env.ExePath), CLIExe)] = true
	if err := in.Apply(Options{}, nil); err != nil {
		t.Fatal(err)
	}
	wrote := false
	for _, c := range f.calls {
		wrote = wrote || c == "write"
	}
	if !wrote {
		t.Fatalf("the embedded CLI must win over a sibling: %v", f.calls)
	}
}

func TestApplyUsesSiblingCLIWhenNothingIsEmbedded(t *testing.T) {
	f := newFake()
	in := newInstaller(f)
	f.files[filepath.Join(filepath.Dir(in.Env.ExePath), CLIExe)] = true
	if err := in.Apply(Options{}, nil); err != nil {
		t.Fatal(err)
	}
	if !f.files[filepath.Join(in.Env.DefaultInstallDir, CLIExe)] {
		t.Fatalf("sibling CLI was not copied: %v", f.calls)
	}
}

// diskInstaller runs the real file adapter in a temp tree; the other ports are fakes.
type failingFS struct {
	osFS
	failWrite bool
}

func (f failingFS) WriteFile(dst string, data []byte) (int64, error) {
	if f.failWrite {
		return 0, errors.New("disk full")
	}
	return f.osFS.WriteFile(dst, data)
}
func (failingFS) ScheduleRemoval(Removal) error { return nil }

func diskInstaller(t *testing.T, fs FileSystem) (*Installer, string, string) {
	t.Helper()
	root := t.TempDir()
	src := filepath.Join(root, "dl")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, ManagerExe), []byte("new-manager"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := newFake()
	dir := filepath.Join(root, "Programs", "GabitoTL")
	return &Installer{
		Env: Env{
			ExePath: filepath.Join(src, ManagerExe), DefaultInstallDir: dir,
			StartMenuPrograms: filepath.Join(root, "start"), DesktopDir: filepath.Join(root, "desk"),
			Protected: []string{filepath.Join(root, "home")},
		},
		FS: fs, Path: f, Links: f, Reg: f, EmbeddedCLI: []byte("new-cli"),
	}, dir, root
}

func TestEmbeddedCLIBytesAreWrittenExactly(t *testing.T) {
	in, dir, _ := diskInstaller(t, failingFS{})
	if err := in.Apply(Options{}, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, CLIExe))
	if err != nil || string(got) != "new-cli" {
		t.Fatalf("cli = %q (%v)", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, CLIExe+".bak")); err == nil {
		t.Fatal("no backup may remain after a successful install")
	}
}

func TestUpgradeFailureRestoresThePreviousFiles(t *testing.T) {
	in, dir, _ := diskInstaller(t, failingFS{failWrite: true})
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{ManagerExe: "old-manager", CLIExe: "old-cli"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := in.Apply(Options{}, nil); err == nil {
		t.Fatal("want the write error")
	}
	for name, want := range map[string]string{ManagerExe: "old-manager", CLIExe: "old-cli"} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q (%v), want %q", name, got, err, want)
		}
		if _, err := os.Stat(filepath.Join(dir, name+".bak")); err == nil {
			t.Fatalf("%s.bak must be cleaned up", name)
		}
	}
}

func TestFreshInstallFailureRemovesTheDir(t *testing.T) {
	in, dir, _ := diskInstaller(t, failingFS{failWrite: true})
	if err := in.Apply(Options{}, nil); err == nil {
		t.Fatal("want the write error")
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("a failed first install must not leave the directory behind")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func TestWriteAtomicLeavesNoTempFile(t *testing.T) {
	d := t.TempDir()
	dst := filepath.Join(d, "x.exe")
	if _, err := writeAtomic(dst, strings.NewReader("ok")); err != nil {
		t.Fatal(err)
	}
	if _, err := writeAtomic(filepath.Join(d, "y.exe"), errReader{}); err == nil {
		t.Fatal("want the read error")
	}
	blocked := filepath.Join(d, "blocked")
	if err := os.MkdirAll(filepath.Join(blocked, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := writeAtomic(blocked, strings.NewReader("z")); err == nil {
		t.Fatal("renaming over a non-empty directory must fail")
	}
	ents, _ := os.ReadDir(d)
	for _, e := range ents {
		if strings.Contains(e.Name(), ".tmp") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}

func TestUninstallOwnsTheCLIToo(t *testing.T) {
	f := newFake()
	in := newInstaller(f)
	if err := in.Uninstall(Options{}, nil); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range f.sched {
		found = found || filepath.Base(p) == CLIExe
	}
	if !found {
		t.Fatalf("scheduled removal %v must include %s", f.sched, CLIExe)
	}
}

func TestRemovalCommandLineRejectsUnsafePaths(t *testing.T) {
	dir := `C:\Users\Ana\AppData\Local\Programs\GabitoTL`
	data := `C:\Users\Ana\AppData\Local\GabitoTL`
	for name, r := range map[string]Removal{
		"empty datadir file": {Dir: dir, Files: []string{""}},
		"relative datadir":   {Dir: dir, DataDir: `local\GabitoTL`},
		"relative dir":       {Dir: `Programs\GabitoTL`},
		"percent datadir":    {Dir: dir, DataDir: `C:\Users\%USERNAME%\GabitoTL`},
		"caret datadir":      {Dir: dir, DataDir: `C:\a^b`},
		"quote datadir":      {Dir: dir, DataDir: `C:\a"b`},
		"amp datadir":        {Dir: dir, DataDir: `C:\a&calc`},
		"percent dir":        {Dir: `C:\%TEMP%\x`},
		"amp dir":            {Dir: `C:\a&b`},
		"amp file":           {Dir: dir, Files: []string{dir + `\a&b.exe`}},
		"folder meta":        {Dir: dir, DataDir: data, Folders: []string{data + `\w^`}},
		"dotdot to root":     {Dir: dir, DataDir: `C:\x\..\..`},
		"root datadir":       {Dir: dir, DataDir: `C:\`},
		"root dir":           {Dir: `D:/`},
		"unc share root":     {Dir: dir, DataDir: `\\srv\share`},
		"folder is root":     {Dir: dir, Folders: []string{`C:\x\..`}},
	} {
		if got, err := RemovalCommandLine(r); err == nil {
			t.Errorf("%s: accepted: %s", name, got)
		}
	}
}

func TestRemovalCommandLineCleansAndQuotes(t *testing.T) {
	got, err := RemovalCommandLine(Removal{Dir: `C:\x\GabitoTL\..`})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `rmdir "C:\x" >nul`) || strings.Contains(got, `..`) {
		t.Fatalf("Dir not cleaned: %s", got)
	}
	data := `C:\Users\Ana (a)b\Local\.\GabitoTL\\`
	got, err = RemovalCommandLine(Removal{Dir: `C:\p\GabitoTL`, DataDir: data, Folders: []string{data + `/webview`}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`rmdir "C:\Users\Ana (a)b\Local\GabitoTL" >nul`,
		`rmdir /s /q "C:\Users\Ana (a)b\Local\GabitoTL\webview"`,
		`if exist "C:\Users\Ana (a)b\Local\GabitoTL\webview" (`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	got, err = RemovalCommandLine(Removal{Dir: `\\?\C:\p\GabitoTL`, Files: []string{`\\?\C:\p\GabitoTL\a.exe`}})
	if err != nil || !strings.Contains(got, `del /f /q "C:\p\GabitoTL\a.exe"`) {
		t.Fatalf("extended prefix not normalized: %v %s", err, got)
	}
}

type fakeHandle struct {
	image   string
	started time.Time
	idErr   error
	exited  bool
	visible bool
	exitsOn string // "close" or "terminate": action after which it exits
	calls   []string
}

func (f *fakeHandle) Identity() (string, time.Time, error) {
	f.calls = append(f.calls, "identity")
	return f.image, f.started, f.idErr
}
func (f *fakeHandle) CloseWindows() bool {
	f.calls = append(f.calls, "close")
	if f.exitsOn == "close" {
		f.exited = true
	}
	return f.visible
}
func (f *fakeHandle) Wait(time.Duration) bool { return f.exited }
func (f *fakeHandle) Terminate() error {
	f.calls = append(f.calls, "terminate")
	if f.exitsOn == "terminate" {
		f.exited = true
	}
	return nil
}

func TestStopProcessIdentity(t *testing.T) {
	dir := abs(filepath.Join("Programs", "GabitoTL"))
	exe := filepath.Join(dir, ManagerExe)
	t0 := time.Unix(1_700_000_000, 0)
	p := Proc{PID: 7, Path: exe, Started: t0, HasWindow: true}
	cases := []struct {
		name  string
		h     *fakeHandle
		proc  Proc
		calls string
	}{
		{"same image and start, closes itself", &fakeHandle{image: exe, started: t0, visible: true, exitsOn: "close"}, p, "identity close"},
		{"same, terminated", &fakeHandle{image: strings.ToUpper(exe), started: t0, exitsOn: "terminate"}, p, "identity close terminate"},
		{"pid reused by another image", &fakeHandle{image: filepath.Join(abs("elsewhere"), "notepad.exe"), started: t0}, p, "identity"},
		{"same image, different start", &fakeHandle{image: exe, started: t0.Add(time.Second)}, p, "identity"},
		{"unknown listed start", &fakeHandle{image: exe, started: t0}, Proc{PID: 7, Path: exe}, "identity"},
	}
	for _, c := range cases {
		if err := stopProcess(c.proc, time.Millisecond, c.h); err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
		if got := strings.Join(c.h.calls, " "); got != c.calls {
			t.Errorf("%s: calls = %q, want %q", c.name, got, c.calls)
		}
	}
	// A process that never exits is an error only after terminate was tried.
	if err := stopProcess(p, time.Millisecond, &fakeHandle{image: exe, started: t0}); err == nil {
		t.Error("expected an error when the process survives terminate")
	}
	// Identity unreadable but the process already exited: gone, not an error.
	if err := stopProcess(p, time.Millisecond, &fakeHandle{idErr: errors.New("x"), exited: true}); err != nil {
		t.Errorf("exited process: %v", err)
	}
}

func TestOwnedExtendedPrefixAndCase(t *testing.T) {
	dir := abs(filepath.Join("Users", "Ana", "GabitoTL"))
	exe := filepath.Join(dir, ManagerExe)
	for _, p := range []string{`\\?\` + exe, strings.ToUpper(exe), `\\?\` + strings.ToLower(exe)} {
		if !Owned(p, dir, []string{exe}) {
			t.Errorf("Owned(%q) = false", p)
		}
	}
	if Owned(`\\?\`+filepath.Join(abs("elsewhere"), ManagerExe), dir, []string{exe}) {
		t.Error("foreign path owned")
	}
}

func TestStripExtended(t *testing.T) {
	for in, want := range map[string]string{
		`\\?\C:\a\b`:       `C:\a\b`,
		`\\?\UNC\srv\sh\x`: `\\srv\sh\x`,
		`\\?\unc\srv\sh\x`: `\\srv\sh\x`,
		`C:\a`:             `C:\a`,
		`/tmp/a`:           `/tmp/a`,
	} {
		if got := stripExtended(in); got != want {
			t.Errorf("stripExtended(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRemovalCommandLineGuardsEveryDeleteWithMarker(t *testing.T) {
	dir := `C:\Users\Ana\AppData\Local\Programs\GabitoTL`
	data := `C:\Users\Ana\AppData\Local\GabitoTL`
	marker := data + `\uninstall.pending`
	got, err := RemovalCommandLine(Removal{
		Dir: dir, Files: []string{dir + `\gabitotl-manager.exe`, dir + `\gabitotl.exe`},
		DataDir: data, Folders: []string{data + `\webview`, data + `\logs`}, WaitPID: 7, Marker: marker,
	})
	if err != nil {
		t.Fatal(err)
	}
	guard := `if exist "` + marker + `" `
	// every del / rmdir sits in a command that is guarded by the marker
	for _, cmd := range []string{"del /f /q", "rmdir /s /q", `rmdir "` + dir + `"`, `rmdir "` + data + `"`} {
		for from := 0; ; {
			i := strings.Index(got[from:], cmd)
			if i < 0 {
				break
			}
			i += from
			from = i + len(cmd)
			// the guard must open the enclosing segment: look back to the previous " & " or "do ("
			seg := got[:i]
			if j := strings.LastIndex(seg, guard); j < 0 {
				t.Fatalf("%q at %d has no marker guard before it:\n%s", cmd, i, got)
			}
		}
	}
	if n := strings.Count(got, `del /f /q "`+marker+`"`); n != 1 {
		t.Fatalf("marker must be deleted once at the end, got %d:\n%s", n, got)
	}
	if strings.Count(got, guard) < 4 {
		t.Fatalf("expected a guard on every round and cleanup step:\n%s", got)
	}
	if _, err := RemovalCommandLine(Removal{Dir: dir, Marker: `C:\x&y`}); err == nil {
		t.Fatal("marker with cmd metacharacters accepted")
	}
}

func TestUninstallWritesMarkerInstallRemovesIt(t *testing.T) {
	f := newFake()
	in := newInstaller(f)
	in.Env.DataDir = abs(filepath.Join("local", "GabitoTL"))
	marker := filepath.Join(in.Env.DataDir, PendingMarkerName)
	var slept []time.Duration
	in.Sleep = func(d time.Duration) { slept = append(slept, d) }
	if err := in.Uninstall(Options{}, nil); err != nil {
		t.Fatal(err)
	}
	if f.removal.Marker != marker || !f.files[marker] {
		t.Fatalf("uninstall must write the marker and hand it to the cleanup: %+v", f.removal)
	}
	f.calls, f.removed = nil, nil
	if err := in.Apply(Options{}, nil); err != nil {
		t.Fatal(err)
	}
	if f.files[marker] || len(f.removed) == 0 || f.removed[0] != marker {
		t.Fatalf("install must remove the marker first: removed=%v", f.removed)
	}
	if len(slept) != 1 {
		t.Fatalf("install must wait out an in-flight round once, slept %v", slept)
	}
	if f.calls[0] != "removeall" || f.calls[1] != "copy" {
		t.Fatalf("marker removal must precede the copy: %v", f.calls)
	}
	// no marker: no wait
	slept = nil
	_ = in.Apply(Options{}, nil)
	if len(slept) != 0 {
		t.Fatalf("no marker, no wait: %v", slept)
	}
}

// vanishFS loses the manager after it is copied, like a cleanup deleting it.
type vanishFS struct{ *fake }

func (v vanishFS) Exists(p string) bool {
	return v.fake.Exists(p) && filepath.Base(p) != ManagerExe
}

func TestApplyFailsWhenCopiedExeIsMissing(t *testing.T) {
	f := newFake()
	in := newInstaller(f)
	in.FS = vanishFS{f}
	in.Env.ExePath = abs(filepath.Join("src", ManagerExe))
	err := in.Apply(Options{}, nil)
	if err == nil || !strings.Contains(err.Error(), "incomplete") || !strings.Contains(err.Error(), ManagerExe) {
		t.Fatalf("err = %v", err)
	}
}

func TestRemovalCommandLineTargetsAreIndependentStatements(t *testing.T) {
	dir := `C:\Users\Ana\AppData\Local\Programs\GabitoTL`
	got, err := RemovalCommandLine(Removal{Dir: dir, Files: []string{dir + `\a.exe`, dir + `\b.exe`}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "else") {
		t.Fatalf("no else chain allowed:\n%s", got)
	}
	a, b := strings.Index(got, `if exist "`+dir+`\a.exe"`), strings.Index(got, `if exist "`+dir+`\b.exe"`)
	if a < 0 || b < a || !strings.Contains(got[a:b], ") & (") {
		t.Fatalf("each target must be its own parenthesized statement joined by &:\n%s", got)
	}
}

type failMarkerFS struct{ *fake }

func (f failMarkerFS) WriteFile(string, []byte) (int64, error) { return 0, errors.New("disk full") }

func TestUninstallMarkerWriteFailureSchedulesNothing(t *testing.T) {
	f := newFake()
	in := newInstaller(f)
	in.Env.DataDir = abs(filepath.Join("local", "GabitoTL"))
	in.FS = failMarkerFS{f}
	f.removal = Removal{}
	err := in.Uninstall(Options{}, nil)
	if err == nil || !strings.Contains(err.Error(), "marker") {
		t.Fatalf("want the marker error, got %v", err)
	}
	if f.removal.Dir != "" {
		t.Fatalf("no cleanup may be scheduled: %+v", f.removal)
	}
}
