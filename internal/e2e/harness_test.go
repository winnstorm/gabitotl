// Package e2e drives the real gabitotl binary against a fake machine: a fixture
// HOME, fake engram/gentle-ai/claude/opencode/kiro-cli/gh executables on a
// controlled PATH and temporary Git repositories. Nothing real is read or run.
//
// Capabilities the V1 binary does not expose as commands (commits, reverts,
// pull requests, the secret guard, the context-budget checkpoint, saving the
// project map) are exercised through the same internal packages the agent
// runtimes call, with the same fake machine installed in the test process
// environment.
package e2e

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

var binary string // the gabitotl binary built once by TestMain

func TestMain(m *testing.M) {
	if runtime.GOOS == "windows" {
		fmt.Println("e2e: skipped on windows (POSIX sh fakes); see .github/workflows/ci.yml")
		os.Exit(0)
	}
	dir, err := os.MkdirTemp("", "gabitotl-e2e-bin-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binary = filepath.Join(dir, "gabitotl")
	build := exec.Command("go", "build", "-buildvcs=false", "-o", binary, "github.com/winnstorm/gabitotl/cmd/gabitotl")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "building gabitotl: %v\n%s", err, out)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// Fake executables. Every call is appended to $FAKE_LOG as "<tool> <args>".
const fakeEngram = `#!/bin/sh
printf '%s\n' "engram $*" | tr '\n' ' ' >> "$FAKE_LOG"; echo >> "$FAKE_LOG"
case "$1" in
  doctor) echo '{"status":"ok","checks":[{"check_id":"db","result":"ok","message":"fine"}]}' ;;
  projects) echo "  shop  7 obs  1 sessions  0 prompts" ;;
  export) echo '{"sessions":[]}' > "$2"; echo "Exported to $2"; echo "  Sessions: 1"; echo "  Observations: 7"; echo "  Prompts: 0" ;;
  save)
    n=0; [ -f "$FAKE_DIR/engram.count" ] && read n < "$FAKE_DIR/engram.count"
    n=$((n+1)); echo "$n" > "$FAKE_DIR/engram.count"
    { echo "== $2"; echo "$3"; } >> "$FAKE_DIR/engram.saves"
    echo "Memory saved: #$n \"$2\" (memory)" ;;
  search) echo "No memories found" ;;
  *) echo "engram: unknown command $1" >&2; exit 2 ;;
esac
`

const fakeGentleAI = `#!/bin/sh
echo "gentle-ai $*" >> "$FAKE_LOG"
case "$1" in
  uninstall) if [ "$2" = "--all" ]; then echo "removed"; exit 0; fi; echo "usage: gentle-ai uninstall --all" >&2; exit 2 ;;
  *) exit 0 ;;
esac
`

const fakeAgent = `#!/bin/sh
echo "$(basename "$0") $*" >> "$FAKE_LOG"
echo "1.0.0"
`

const fakeGH = `#!/bin/sh
printf '%s\n' "gh $*" | tr '\n' ' ' >> "$FAKE_LOG"; echo >> "$FAKE_LOG"
echo "https://github.com/acme/shop/pull/7"
`

// machine is one hermetic fake computer.
type machine struct {
	t    *testing.T
	root string // everything of this machine lives here
	home string // fixture $HOME
	bin  string // controlled PATH
	dir  string // FAKE_DIR: state of the fake executables
	log  string // FAKE_LOG
	base string // where repositories are created
}

// newMachine builds a fixture machine and installs its environment into the
// test process too (t.Setenv), so library code that shells out to git, gh or
// engram uses the same fakes and never the real $HOME.
func newMachine(t *testing.T) *machine {
	return newMachineIn(t, t.TempDir())
}

// newMachineIn is newMachine with repositories created under base. Worktrees
// refuse temporary directories, so those tests pass a base outside /tmp.
func newMachineIn(t *testing.T, base string) *machine {
	t.Helper()
	root := t.TempDir()
	m := &machine{t: t, root: root, home: filepath.Join(root, "home"), bin: filepath.Join(root, "bin"),
		dir: filepath.Join(root, "fake"), log: filepath.Join(root, "fake", "calls.log"), base: base}
	for _, d := range []string{m.home, m.bin, m.dir, filepath.Join(root, "tmp")} {
		mustMkdir(t, d)
	}
	// Real tools the harness and git itself need, nothing else.
	for _, tool := range []string{"git", "sh", "cat", "mkdir", "date", "dirname", "basename", "rm", "cp", "tr"} {
		if p, err := exec.LookPath(tool); err == nil {
			if err := os.Symlink(p, filepath.Join(m.bin, tool)); err != nil {
				t.Fatal(err)
			}
		}
	}
	m.script("engram", fakeEngram)
	m.script("gentle-ai", fakeGentleAI)
	m.script("gh", fakeGH)
	for _, a := range []string{"claude", "opencode", "kiro-cli"} {
		m.script(a, fakeAgent)
	}
	m.write(filepath.Join(m.home, ".gitconfig"), "[user]\n\tname = Test Dev\n\temail = dev@example.invalid\n[init]\n\tdefaultBranch = main\n[commit]\n\tgpgsign = false\n")
	for k, v := range m.envMap() {
		t.Setenv(k, v)
	}
	return m
}

func (m *machine) envMap() map[string]string {
	return map[string]string{
		"HOME": m.home, "USERPROFILE": m.home, "PATH": m.bin,
		"TMPDIR":              filepath.Join(m.root, "tmp"),
		"FAKE_LOG":            m.log,
		"FAKE_DIR":            m.dir,
		"LANG":                "C",
		"GIT_CONFIG_GLOBAL":   filepath.Join(m.home, ".gitconfig"),
		"GIT_CONFIG_NOSYSTEM": "1",
		"XDG_CONFIG_HOME":     "", "ENGRAM_DATA_DIR": "", "OPENCODE_CONFIG": "", "OPENCODE_CONFIG_DIR": "",
	}
}

func (m *machine) env() []string {
	var out []string
	for k, v := range m.envMap() {
		if v != "" {
			out = append(out, k+"="+v)
		}
	}
	return out
}

func (m *machine) script(name, body string) {
	m.t.Helper()
	p := filepath.Join(m.bin, name)
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		m.t.Fatal(err)
	}
}

func (m *machine) write(path, content string) {
	m.t.Helper()
	mustMkdir(m.t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		m.t.Fatal(err)
	}
}

func mustMkdir(t *testing.T, d string) {
	t.Helper()
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
}

// result is one gabitotl run.
type result struct {
	code     int
	out, err string
}

// run executes the real binary in dir with the fixture environment and no stdin.
func (m *machine) run(dir string, args ...string) result {
	m.t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Dir = dir
	cmd.Env = m.env()
	var out, errb bytes.Buffer
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, &out, &errb
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		m.t.Fatalf("running gabitotl %v: %v", args, err)
	}
	return result{code, out.String(), errb.String()}
}

// ok runs and requires exit code 0.
func (m *machine) ok(dir string, args ...string) result {
	m.t.Helper()
	r := m.run(dir, args...)
	if r.code != 0 {
		m.t.Fatalf("gabitotl %v exited %d\nstdout:\n%s\nstderr:\n%s", args, r.code, r.out, r.err)
	}
	return r
}

// git runs the real git in dir with the fixture environment.
func (m *machine) git(dir string, args ...string) string {
	m.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = m.env()
	out, err := cmd.CombinedOutput()
	if err != nil {
		m.t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// repo creates a Git repository (with one commit unless empty) under base.
func (m *machine) repo(name string, files map[string]string) string {
	m.t.Helper()
	dir := filepath.Join(m.base, name)
	mustMkdir(m.t, dir)
	m.git(dir, "init", "-q")
	for p, c := range files {
		m.write(filepath.Join(dir, p), c)
	}
	if len(files) > 0 {
		m.git(dir, "add", "-A")
		m.git(dir, "commit", "-q", "-m", "chore: initial commit")
	}
	return dir
}

// calls returns the recorded fake invocations, in order.
func (m *machine) calls() []string {
	b, err := os.ReadFile(m.log)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

// index returns the position of the first call starting with prefix after from, or -1.
func index(calls []string, prefix string, from int) int {
	for i := from; i < len(calls); i++ {
		if strings.HasPrefix(calls[i], prefix) {
			return i
		}
	}
	return -1
}

func lastIndex(calls []string, prefix string) int {
	for i := len(calls) - 1; i >= 0; i-- {
		if strings.HasPrefix(calls[i], prefix) {
			return i
		}
	}
	return -1
}

func (m *machine) count(prefix string) int {
	n := 0
	for _, c := range m.calls() {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

// hash fingerprints every file under dirs (path and content), skipping .git
// and the fake call log, so "nothing changed" is a one-line assertion.
func hash(t *testing.T, skip []string, dirs ...string) string {
	t.Helper()
	h := sha256.New()
	for _, d := range dirs {
		var ps []string
		_ = filepath.WalkDir(d, func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if e.IsDir() && e.Name() == ".git" {
				return fs.SkipDir
			}
			for _, s := range skip {
				if p == s {
					return fs.SkipDir
				}
			}
			if !e.IsDir() {
				ps = append(ps, p)
			}
			return nil
		})
		sort.Strings(ps)
		for _, p := range ps {
			b, _ := os.ReadFile(p)
			h.Write([]byte(p + "\x00"))
			h.Write(b)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// outsideTemp returns a fresh directory that is not under a temporary
// directory (worktree commands refuse those). It lives next to the package
// sources and is removed when the test ends.
func outsideTemp(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	d, err := os.MkdirTemp(wd, "zz-scratch-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}

// allowVCS rewrites .gabitotl/vcs.md so every permission is "allow", the way a
// user opts in to autonomous Git work.
func (m *machine) allowVCS(root string) {
	m.t.Helper()
	p := filepath.Join(root, ".gabitotl", "vcs.md")
	m.write(p, strings.ReplaceAll(read(m.t, p), ": ask", ": allow"))
}
