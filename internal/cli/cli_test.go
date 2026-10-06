package cli

import (
	"bytes"
	"strings"
	"testing"
)

func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(args, Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errb, Getwd: func() (string, error) { return t.TempDir(), nil }})
	return code, out.String(), errb.String()
}

func TestDispatch(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		code   int
		outHas string
		errHas string
	}{
		{"no args prints help", nil, 0, "Usage: gabitotl", ""},
		{"help", []string{"help"}, 0, "init", ""},
		{"--help", []string{"--help"}, 0, "doctor", ""},
		{"version", []string{"version"}, 0, "gabitotl ", ""},
		{"--version", []string{"--version"}, 0, "gabitotl ", ""},
		{"unknown", []string{"nope"}, 2, "", "unknown command"},
		{"doctor stub", []string{"doctor"}, 2, "", "not implemented yet"},
		{"install stub", []string{"install"}, 2, "", "not implemented yet"},
		{"migrate stub", []string{"migrate"}, 2, "", "not implemented yet"},
		{"restore stub", []string{"restore"}, 2, "", "not implemented yet"},
		{"uninstall stub", []string{"uninstall"}, 2, "", "not implemented yet"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, out, errs := run(t, tc.args...)
			if code != tc.code {
				t.Fatalf("exit = %d, want %d", code, tc.code)
			}
			if !strings.Contains(out, tc.outHas) || !strings.Contains(errs, tc.errHas) {
				t.Fatalf("stdout=%q stderr=%q", out, errs)
			}
		})
	}
}
