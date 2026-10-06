package tui

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/winnstorm/gabitotl/internal/installer"
	"github.com/winnstorm/gabitotl/internal/installer/ui"
	"github.com/winnstorm/gabitotl/internal/memory/migrate"
	"github.com/winnstorm/gabitotl/internal/runtime/all"
)

const (
	fxGentlePersona = "<!-- gentle-ai:persona -->\nGentleman.\n<!-- /gentle-ai:persona -->\n"
	fxStateAll      = `{"version":"4.2.0","installed_agents":["claude-code","opencode"],"components":["persona","engram"]}`
	fxStateExtra    = `{"version":"4.2.0","installed_agents":["claude-code","opencode","windsurf"],"components":["persona","engram"]}`
	fxDoctor        = `{"status":"ok","project":"shop","checks":[{"check_id":"db","result":"ok","message":"fine"}]}`
	fxList          = "Projects:\n  shop  7 obs  2 sessions  1 prompts\n"
)

// machine is a fake computer: a fixture HOME and project, fake binaries and a
// recording runner. Nothing outside t.TempDir is ever read or written.
type machine struct {
	t      *testing.T
	home   string
	root   string
	runner *installer.FakeRunner
	bins   map[string]bool
	env    installer.Env
}

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newMachine builds a machine with Claude Code, OpenCode, Engram (7 memories)
// and Gentle-AI managing claude-code and opencode.
func newMachine(t *testing.T) *machine {
	t.Helper()
	mc := &machine{t: t, home: t.TempDir(), root: filepath.Join(t.TempDir(), "shop"),
		bins: map[string]bool{"claude": true, "opencode": true, "git": true, "engram": true, "gentle-ai": true}}
	write(t, filepath.Join(mc.home, ".claude/CLAUDE.md"), "# My rules\n\n"+fxGentlePersona)
	write(t, filepath.Join(mc.home, ".config/opencode/opencode.json"), "{\n  \"model\": \"anthropic/claude-sonnet-4-5\"\n}\n")
	write(t, filepath.Join(mc.home, ".gentle-ai/state.json"), fxStateAll)
	write(t, filepath.Join(mc.home, ".gentle-ai/backups/old.tgz"), "backup")
	write(t, filepath.Join(mc.home, ".engram/engram.db"), "ENGRAM-DB-BYTES")
	write(t, filepath.Join(mc.root, "go.mod"), "module shop\n")
	write(t, filepath.Join(mc.root, "main.go"), "package main\n")
	mc.runner = &installer.FakeRunner{Handlers: map[string]func([]string) (string, error){
		"git": func(a []string) (string, error) {
			if len(a) > 0 && a[0] == "rev-parse" {
				return "true\n", nil
			}
			return "https://github.com/acme/shop.git\n", nil
		},
		"engram doctor":   func([]string) (string, error) { return fxDoctor, nil },
		"engram projects": func([]string) (string, error) { return fxList, nil },
		"engram export": func(a []string) (string, error) {
			write(t, a[1], `{"sessions":[],"observations":[]}`)
			return "Exported to " + a[1] + "\n  Sessions: 2\n  Observations: 7\n  Prompts: 1\n", nil
		},
		"engram save":         func([]string) (string, error) { return "Memory saved: #9\n", nil },
		"gentle-ai uninstall": func([]string) (string, error) { return "uninstalled\n", nil },
	}}
	mc.env = installer.Env{
		Home: mc.home, ProjectRoot: mc.root, GOOS: "linux", GOARCH: "amd64",
		Getenv:  func(k string) string { return map[string]string{"SHELL": "/bin/bash"}[k] },
		Environ: func() []string { return []string{"SHELL=/bin/bash"} },
		LookPath: func(n string) (string, error) {
			if mc.bins[n] {
				return "/fake/bin/" + n, nil
			}
			return "", os.ErrNotExist
		},
		Runner: mc.runner, Runtimes: all.Registry(), Migrators: migrate.NewRegistry(), Version: "test",
		Now: func() time.Time { return time.Date(2026, 10, 5, 21, 0, 0, 0, time.UTC) },
	}
	return mc
}

func (mc *machine) config(o installer.Options) Config {
	return Config{
		Backend: NewBackend(mc.env), Messages: ui.Default(), Paths: ui.Paths{Home: mc.home, Root: mc.root},
		Options: o, NoColor: true, SuggestedName: "Test Dev", Timeout: time.Minute,
	}
}

// hash fingerprints every file under dirs, skipping the given absolute paths.
func hash(t *testing.T, skip []string, dirs ...string) string {
	t.Helper()
	h := sha256.New()
	for _, d := range dirs {
		var ps []string
		_ = filepath.WalkDir(d, func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				return nil
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

// state is the machine fingerprint that must not change when nothing is applied.
func (mc *machine) state() string {
	return hash(mc.t, nil, mc.home, mc.root)
}

// driver feeds messages to a Model and runs the commands it returns, the way
// Bubble Tea's runtime does, without a terminal.
type driver struct {
	t  *testing.T
	m  *Model
	in chan tea.Msg
}

func newDriver(t *testing.T, cfg Config) *driver {
	t.Helper()
	d := &driver{t: t, m: New(cfg), in: make(chan tea.Msg, 4096)}
	d.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	d.run(d.m.Init())
	d.settle()
	return d
}

func (d *driver) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() {
		msg := cmd()
		if msg == nil {
			return
		}
		// tea.Batch and tea.Sequence return slices of commands.
		if v := reflect.ValueOf(msg); v.Kind() == reflect.Slice && v.Type().Elem() == reflect.TypeOf(cmd) {
			for i := 0; i < v.Len(); i++ {
				d.run(v.Index(i).Interface().(tea.Cmd))
			}
			return
		}
		d.in <- msg
	}()
}

func (d *driver) send(msg tea.Msg) {
	_, cmd := d.m.Update(msg)
	d.run(cmd)
}

// settle delivers command results until nothing arrives for a short while.
func (d *driver) settle() {
	for {
		select {
		case msg := <-d.in:
			d.send(msg)
		case <-time.After(80 * time.Millisecond):
			return
		}
	}
}

// until delivers messages until cond holds or the deadline passes.
func (d *driver) until(what string, cond func() bool) {
	d.t.Helper()
	deadline := time.After(20 * time.Second)
	for !cond() {
		select {
		case msg := <-d.in:
			d.send(msg)
		case <-deadline:
			d.t.Fatalf("timed out waiting for %s\n%s", what, d.view())
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func (d *driver) key(names ...string) {
	for _, n := range names {
		var k tea.KeyPressMsg
		switch n {
		case "enter":
			k = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "esc":
			k = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "down":
			k = tea.KeyPressMsg{Code: tea.KeyDown}
		case "up":
			k = tea.KeyPressMsg{Code: tea.KeyUp}
		case "pgdown":
			k = tea.KeyPressMsg{Code: tea.KeyPgDown}
		case "space":
			k = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
		case "ctrl+c":
			k = tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
		default:
			k = tea.KeyPressMsg{Code: rune(n[0]), Text: n}
		}
		d.send(k)
		d.settle()
	}
}

func (d *driver) typeText(s string) {
	for _, r := range s {
		d.send(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	d.settle()
}

func (d *driver) view() string { return d.m.render() }

// flat collapses whitespace so assertions do not depend on where lines wrap.
func flat(s string) string { return strings.Join(strings.Fields(s), " ") }

func (d *driver) has(sub string) bool { return strings.Contains(flat(d.view()), flat(sub)) }

func (d *driver) require(sub string) {
	d.t.Helper()
	if !d.has(sub) {
		d.t.Fatalf("screen does not show %q:\n%s", sub, d.view())
	}
}

// pick moves the cursor to the menu item with the label and presses enter.
func (d *driver) pick(label string) {
	d.t.Helper()
	for i, it := range d.m.items() {
		if it.label == label {
			d.m.cur = i
			d.key("enter")
			return
		}
	}
	d.t.Fatalf("no menu item %q in %+v\n%s", label, d.m.items(), d.view())
}

func sizeMsg(w, h int) tea.WindowSizeMsg { return tea.WindowSizeMsg{Width: w, Height: h} }
