package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/installer"
	"github.com/winnstorm/gabitotl/internal/tui"
)

// runOn runs the CLI with explicit terminal flags and a stubbed full-screen UI.
func (f *cfix) runOn(stdinTTY, stdoutTTY bool, args ...string) (r result, tuiCalls int) {
	f.t.Helper()
	old := tuiRunFn
	tuiRunFn = func(cfg tui.Config, _ io.Reader, _ io.Writer) (tui.Outcome, error) {
		tuiCalls++
		return tui.Outcome{}, nil
	}
	f.t.Cleanup(func() { tuiRunFn = old })
	var out, errb bytes.Buffer
	code := Run(args, Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errb, Getwd: func() (string, error) { return f.root, nil }, IsTTY: stdinTTY, IsOutTTY: stdoutTTY})
	return result{code, out.String(), errb.String()}, tuiCalls
}

func TestTUIOpensOnlyWhenStdinAndStdoutAreTerminals(t *testing.T) {
	for _, tc := range []struct {
		name      string
		in, out   bool
		args      []string
		wantTUI   bool
		wantPlain string
	}{
		{"install on a terminal", true, true, []string{"install"}, true, ""},
		{"bare command on a terminal", true, true, nil, true, ""},
		{"stdout piped", true, false, []string{"install", "--dry-run"}, false, "Plan:"},
		{"stdin piped", false, true, []string{"install", "--dry-run"}, false, "Plan:"},
		{"--no-tui forces plain", true, true, []string{"install", "--no-tui", "--dry-run"}, false, "Plan:"},
		{"--dry-run stays plain", true, true, []string{"install", "--dry-run"}, false, "Plan:"},
		{"--yes stays plain", true, true, []string{"install", "--yes", "--dry-run"}, false, "Plan:"},
		{"migrate stays plain", true, true, []string{"migrate", "gentle-ai", "--dry-run"}, false, "Plan:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCfix(t)
			before := chash(t, f.home, f.root)
			r, calls := f.runOn(tc.in, tc.out, tc.args...)
			if (calls == 1) != tc.wantTUI {
				t.Fatalf("tui calls = %d, want TUI=%v\n%+v", calls, tc.wantTUI, r)
			}
			if tc.wantTUI && (r.code != ExitOK || !strings.Contains(r.out, "No cambié nada")) {
				t.Fatalf("quitting the TUI must change nothing and say so: %+v", r)
			}
			if tc.wantPlain != "" && !strings.Contains(r.out, tc.wantPlain) {
				t.Fatalf("plain flow output missing %q:\n%+v", tc.wantPlain, r)
			}
			if chash(t, f.home, f.root) != before {
				t.Fatal("nothing may change here")
			}
		})
	}
}

func TestTUIConfigCarriesFlagsAndNeverAsksWhatWasGiven(t *testing.T) {
	f := newCfix(t)
	old := tuiRunFn
	var got tui.Config
	tuiRunFn = func(cfg tui.Config, _ io.Reader, _ io.Writer) (tui.Outcome, error) {
		got = cfg
		return tui.Outcome{}, nil
	}
	t.Cleanup(func() { tuiRunFn = old })
	var out, errb bytes.Buffer
	Run([]string{"install", "--runtime", "claude-code", "--user", "gabi", "--name", "Gabi", "--memory", "engram"},
		Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errb, Getwd: func() (string, error) { return f.root, nil }, IsTTY: true, IsOutTTY: true})
	o := got.Options
	if len(o.Runtimes) != 1 || o.Runtimes[0] != "claude-code" || o.User != "gabi" || o.UserName != "Gabi" || o.Memory != "engram" {
		t.Fatalf("flags were not carried: %+v", o)
	}
	need := got.Need(installer.Inventory{}, o)
	if need.Name || need.Mode {
		t.Fatalf("name and memory were given, so they are not asked: %+v", need)
	}
}
