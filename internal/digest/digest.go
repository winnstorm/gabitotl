// Package digest condenses the output of a noisy command (tests, builds,
// linters) into the few lines an agent needs: failures with a little context
// and the tail. It is pure: no I/O, no clock.
package digest

import (
	"fmt"
	"regexp"
	"strings"
)

// Defaults for Options.
const (
	DefaultMaxLines = 60
	DefaultTail     = 15
	// QuietTail is the tail kept when the command succeeded without failure lines.
	QuietTail = 5
	// contextAfter is how many lines after a failure line are kept.
	contextAfter = 2
	maxLookBack  = 12
)

// Options bound the digest.
type Options struct {
	MaxLines int  // cap on highlight lines
	Tail     int  // trailing lines kept
	OK       bool // the command exited 0
}

// Result is the digest of one output.
type Result struct {
	Lines      int      `json:"lines"`
	Highlights []string `json:"highlights"`
	Tail       []string `json:"tail"`
}

var (
	signal = regexp.MustCompile(`(?i)(\bfail(ed|ure|ures)?\b|\berrors?\b|panic:|traceback|exception|assertionerror|undefined:|✗|npm err!)`)
	// compiler/linter positions: file.ext:12:5: or file.ext(12,5): message.
	position = regexp.MustCompile(`^\s*[\w./\\@-]+\.\w+(:\d+:\d+:|\(\d+,\d+\):)`)
	// summary lines of a passing run such as "0 failed" or "errors: 0".
	zero = regexp.MustCompile(`(?i)\b0 (failed|failures?|errors?)\b|\b(failed|failures?|errors?)[:=] ?0\b`)
	run  = regexp.MustCompile(`^=== (RUN|NAME|CONT)`)
)

func isSignal(line string) bool {
	if position.MatchString(line) {
		return true
	}
	if !signal.MatchString(line) {
		return false
	}
	return !zero.MatchString(line)
}

// Digest builds the digest of out.
func Digest(out string, o Options) Result {
	if o.MaxLines <= 0 {
		o.MaxLines = DefaultMaxLines
	}
	if o.Tail < 0 {
		o.Tail = 0
	}
	lines := splitLines(out)
	r := Result{Lines: len(lines), Highlights: []string{}, Tail: []string{}}

	picked := make([]bool, len(lines))
	seen := map[string]bool{}
	var hl []string
	truncated := false
	for i := 0; i < len(lines); i++ {
		if len(hl) >= o.MaxLines {
			truncated = truncated || isSignal(lines[i])
			continue
		}
		if !isSignal(lines[i]) {
			continue
		}
		key := strings.TrimSpace(lines[i])
		if seen[key] {
			continue
		}
		seen[key] = true
		if name := failName(lines[i]); name != "" {
			// Go prints the test's own output between "=== RUN name" and "--- FAIL: name".
			for k := i - 1; k >= 0 && k >= i-maxLookBack; k-- {
				if run.MatchString(lines[k]) && strings.Contains(lines[k], name) {
					for m := k; m < i; m++ {
						if !picked[m] {
							picked[m] = true
							hl = append(hl, lines[m])
						}
					}
					break
				}
			}
		}
		for j := i; j <= i+contextAfter && j < len(lines); j++ {
			if picked[j] || (j > i && isSignal(lines[j])) {
				continue
			}
			picked[j] = true
			hl = append(hl, lines[j])
		}
	}
	r.Highlights = collapse(trimBlank(hl))
	if truncated {
		r.Highlights = append(r.Highlights, "… (more failure lines in the log)")
	}

	tail := o.Tail
	if o.OK && len(hl) == 0 {
		tail = QuietTail
	}
	start := len(lines) - tail
	if start < 0 {
		start = 0
	}
	var t []string
	for i := start; i < len(lines); i++ {
		if !picked[i] {
			t = append(t, lines[i])
		}
	}
	r.Tail = collapse(trimBlank(t))
	return r
}

// Render prints the digest as text: the header, highlights, then the tail.
func Render(header string, r Result) string {
	var b strings.Builder
	b.WriteString(header + "\n")
	for _, l := range r.Highlights {
		b.WriteString(l + "\n")
	}
	if len(r.Highlights) > 0 && len(r.Tail) > 0 {
		b.WriteString("--- tail ---\n")
	}
	for _, l := range r.Tail {
		b.WriteString(l + "\n")
	}
	return b.String()
}

func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func trimBlank(in []string) []string {
	out := in[:0:0]
	for _, l := range in {
		if strings.TrimSpace(l) != "" {
			out = append(out, strings.TrimRight(l, " \t"))
		}
	}
	return out
}

// collapse folds runs of identical lines into "line (×N)".
func collapse(in []string) []string {
	var out []string
	for i := 0; i < len(in); {
		j := i
		for j < len(in) && in[j] == in[i] {
			j++
		}
		if n := j - i; n > 1 {
			out = append(out, fmt.Sprintf("%s (×%d)", in[i], n))
		} else {
			out = append(out, in[i])
		}
		i = j
	}
	return out
}

// failName returns the test name of a "--- FAIL: name (0.00s)" line.
func failName(line string) string {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "--- FAIL:") {
		return ""
	}
	f := strings.Fields(strings.TrimPrefix(t, "--- FAIL:"))
	if len(f) == 0 {
		return ""
	}
	return f[0]
}
