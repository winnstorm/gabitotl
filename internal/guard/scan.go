package guard

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/winnstorm/gabitotl/internal/branding"
	"github.com/winnstorm/gabitotl/internal/vcs"
)

// Settings is the guard configuration taken from workflow.md.
type Settings struct {
	Mode       string
	ExtraTerms []string
	AllowPaths []string
}

// Detector builds the detector for the configured extra terms.
func (s Settings) Detector() *branding.Detector { return branding.New(s.ExtraTerms...) }

// AddedLine is one line added by a diff.
type AddedLine struct {
	File string
	Line int
	Text string
}

// ParseAdded extracts the added lines of a unified diff (-U0 or any context).
// Deleted lines, context lines and binary files yield nothing.
func ParseAdded(diff string) []AddedLine {
	var out []AddedLine
	file, line := "", 0
	for _, l := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(l, "+++ "):
			file = diffPath(strings.TrimPrefix(l, "+++ "))
		case strings.HasPrefix(l, "--- "):
			// old name: ignore
		case strings.HasPrefix(l, "@@"):
			line = hunkStart(l)
		case strings.HasPrefix(l, "+") && file != "":
			out = append(out, AddedLine{File: file, Line: line, Text: l[1:]})
			line++
		case strings.HasPrefix(l, " ") && file != "":
			line++
		}
	}
	return out
}

func diffPath(s string) string {
	s = strings.TrimSpace(s)
	if s == "/dev/null" {
		return ""
	}
	if u, err := strconv.Unquote(s); err == nil {
		s = u
	}
	return strings.TrimPrefix(s, "b/")
}

// hunkStart reads the new-file start line of "@@ -a,b +c,d @@".
func hunkStart(h string) int {
	i := strings.Index(h, "+")
	if i < 0 {
		return 0
	}
	rest := h[i+1:]
	end := strings.IndexAny(rest, ", ")
	if end < 0 {
		end = len(rest)
	}
	n, _ := strconv.Atoi(rest[:end])
	return n
}

// ScanAdded scans added lines only, honoring the skip rules and the allowlist.
func ScanAdded(d *branding.Detector, allow []string, added []AddedLine) []branding.Finding {
	var out []branding.Finding
	for _, a := range added {
		if branding.Skipped(a.File) || branding.Allowed(a.File, allow) {
			continue
		}
		if f := d.Scan(a.File, a.Text, branding.ContextFor(a.File)); len(f) > 0 {
			f[0].Line = a.Line
			out = append(out, f[0])
		}
	}
	return out
}

const diffArgs = "--no-color --no-ext-diff --no-renames --src-prefix=a/ --dst-prefix=b/ -U0"

func diffCmd(extra ...string) []string {
	return append(append([]string{"-c", "core.quotepath=false", "diff"}, strings.Fields(diffArgs)...), extra...)
}

// ScanStaged scans the lines the next commit adds (git diff --cached).
func ScanStaged(ctx context.Context, run vcs.Runner, dir string, s Settings) ([]branding.Finding, error) {
	out, err := run.Run(ctx, dir, "git", diffCmd("--cached", "--")...)
	if err != nil {
		return nil, err
	}
	return ScanAdded(s.Detector(), s.AllowPaths, ParseAdded(out)), nil
}

// ScanDiff scans the lines added in the work tree relative to ref.
func ScanDiff(ctx context.Context, run vcs.Runner, dir, ref string, s Settings) ([]branding.Finding, error) {
	if ref == "" || strings.HasPrefix(ref, "-") {
		return nil, vcs.ErrNotImplemented
	}
	out, err := run.Run(ctx, dir, "git", diffCmd(ref, "--")...)
	if err != nil {
		return nil, err
	}
	return ScanAdded(s.Detector(), s.AllowPaths, ParseAdded(out)), nil
}

// ListFiles returns the project files the guard looks at: tracked and untracked
// but not ignored files in a Git work tree, a plain walk otherwise.
func ListFiles(ctx context.Context, run vcs.Runner, dir string) ([]string, error) {
	out, err := run.Run(ctx, dir, "git", "-c", "core.quotepath=false", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return branding.WalkFiles(dir)
	}
	seen := map[string]bool{}
	var files []string
	for _, f := range strings.Split(out, "\x00") {
		if f = strings.TrimSpace(f); f != "" && !seen[f] {
			seen[f] = true
			files = append(files, f)
		}
	}
	return files, nil
}

// ScanAll scans the whole work tree.
func ScanAll(ctx context.Context, run vcs.Runner, dir string, s Settings) ([]branding.Finding, error) {
	files, err := ListFiles(ctx, run, dir)
	if err != nil {
		return nil, err
	}
	return s.Detector().ScanPaths(dir, files, s.AllowPaths), nil
}

// HistoryFinding is attribution found in a commit that already exists. History
// is never rewritten: these are reported for the people who own it to decide.
type HistoryFinding struct {
	Commit  string         `json:"commit"`
	Subject string         `json:"subject"`
	Where   string         `json:"where"` // message, author or committer
	Class   branding.Class `json:"class"`
	Text    string         `json:"text"`
}

// ScanHistory reads every commit reachable from any ref (read-only) and reports
// attribution in messages and AI identities as author or committer.
func ScanHistory(ctx context.Context, run vcs.Runner, dir string, s Settings) ([]HistoryFinding, error) {
	res, err := ScanHistoryIncremental(ctx, run, dir, s, "", true)
	return res.Findings, err
}

// HistoryResult is a history scan outcome plus how many commits it read.
type HistoryResult struct {
	Findings []HistoryFinding
	Scanned  int
}

// ScanHistoryIncremental is ScanHistory limited to commits not yet scanned
// clean. store is a file of ref tips whose whole ancestry was scanned without
// findings; "" disables remembering. full ignores it and rescans everything.
// Tips are recorded only after a scan with no findings, so findings are
// reported again until fixed. A stored tip whose object no longer exists is
// ignored; one that still exists but is no longer reachable is still valid,
// because its ancestry was scanned when it was recorded.
func ScanHistoryIncremental(ctx context.Context, run vcs.Runner, dir string, s Settings, store string, full bool) (HistoryResult, error) {
	const rs, us = "\x1e", "\x1f"
	tips := currentTips(ctx, run, dir)
	args := []string{"log", "--all", "--format=%H" + "%x1f" + "%an" + "%x1f" + "%ae" + "%x1f" + "%cn" + "%x1f" + "%ce" + "%x1f" + "%B" + "%x1e"}
	if store != "" && !full {
		var known []string
		for _, t := range readTips(store) {
			if _, err := run.Run(ctx, dir, "git", "rev-parse", "--verify", "-q", t+"^{commit}"); err == nil {
				known = append(known, t)
			}
		}
		if len(known) > 0 {
			args = append(args, "--not")
			args = append(args, known...)
		}
	}
	out, err := run.Run(ctx, dir, "git", args...)
	if err != nil {
		return HistoryResult{}, err
	}
	d := s.Detector()
	var res HistoryResult
	for _, rec := range strings.Split(out, rs) {
		rec = strings.TrimLeft(rec, "\n")
		f := strings.SplitN(rec, us, 6)
		if len(f) < 6 {
			continue
		}
		res.Scanned++
		sha, an, ae, cn, ce, msg := f[0], f[1], f[2], f[3], f[4], f[5]
		subject, _, _ := strings.Cut(strings.TrimSpace(msg), "\n")
		add := func(where string, c branding.Class, text string) {
			res.Findings = append(res.Findings, HistoryFinding{Commit: sha, Subject: subject, Where: where, Class: c, Text: text})
		}
		if d.IsAIIdentity(an + " <" + ae + ">") {
			add("author", branding.ClassTrailer, an+" <"+ae+">")
		}
		if d.IsAIIdentity(cn + " <" + ce + ">") {
			add("committer", branding.ClassTrailer, cn+" <"+ce+">")
		}
		for _, fd := range d.Scan("", msg, branding.ContextMessage) {
			add("message", fd.Class, fd.Text)
		}
	}
	if store != "" && len(res.Findings) == 0 && len(tips) > 0 {
		_ = writeTips(store, tips) // best effort: a lost cache only costs a full scan
	}
	return res, nil
}

// currentTips lists the objects every ref (and HEAD) points at.
func currentTips(ctx context.Context, run vcs.Runner, dir string) []string {
	out, err := run.Run(ctx, dir, "git", "for-each-ref", "--format=%(objectname)")
	if err != nil {
		return nil
	}
	if h, err := run.Run(ctx, dir, "git", "rev-parse", "-q", "--verify", "HEAD"); err == nil {
		out += "\n" + h
	}
	seen := map[string]bool{}
	var tips []string
	for _, t := range strings.Fields(out) {
		if !seen[t] {
			seen[t] = true
			tips = append(tips, t)
		}
	}
	return tips
}

func readTips(store string) []string {
	b, err := os.ReadFile(store)
	if err != nil {
		return nil
	}
	var tips []string
	for _, t := range strings.Fields(string(b)) {
		if len(t) >= 40 && len(t) <= 64 && strings.Trim(t, "0123456789abcdef") == "" {
			tips = append(tips, t)
		}
	}
	return tips
}

func writeTips(store string, tips []string) error {
	if err := os.MkdirAll(filepath.Dir(store), 0o755); err != nil {
		return err
	}
	return os.WriteFile(store, []byte(strings.Join(tips, "\n")+"\n"), 0o644)
}
