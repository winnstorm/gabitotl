package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/budget"
	"github.com/winnstorm/gabitotl/internal/digest"
	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/procattr"
)

// runRun executes a command without a shell, keeps its full output in a log
// file and prints a compact digest. Its exit code is the command's.
func runRun(env Env, args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	maxLines := fs.Int("max-lines", digest.DefaultMaxLines, "cap on failure lines printed")
	tail := fs.Int("tail", digest.DefaultTail, "trailing output lines printed")
	keep := fs.Int("keep", defaultKeepLogs, "run logs to keep (0 = keep all)")
	asJSON := fs.Bool("json", false, "print JSON")
	root := fs.String("root", "", "project root (default: Git top-level of the current directory)")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	cmdArgs := fs.Args()
	if len(cmdArgs) == 0 {
		fmt.Fprintln(env.Stderr, "usage: gabitotl run [--max-lines N] [--tail N] [--json] -- <command> [args...]")
		return ExitUsage
	}
	name := strings.Join(cmdArgs, " ")

	var buf bytes.Buffer
	c := procattr.NoWindow(exec.Command(cmdArgs[0], cmdArgs[1:]...))
	c.Stdout, c.Stderr = &buf, &buf
	if wd, err := env.Getwd(); err == nil {
		c.Dir = wd
	}
	start := time.Now()
	err := c.Run()
	secs := time.Since(start).Seconds()

	exit := 0
	if err != nil {
		var ee *exec.ExitError
		switch {
		case errors.As(err, &ee):
			exit = ee.ExitCode()
			if exit < 0 {
				exit = 1
			}
		case errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist):
			fmt.Fprintf(env.Stderr, "run: command not found: %s\n", cmdArgs[0])
			return 127
		default:
			fmt.Fprintf(env.Stderr, "run: %v\n", err)
			return 127
		}
	}

	full := buf.String()
	if !*asJSON && len(full) <= smallOutputBytes && countLines(full) <= *tail+*maxLines {
		// Small output: the digest would cost more than the output itself.
		fmt.Fprint(env.Stdout, full)
		if full != "" && !strings.HasSuffix(full, "\n") {
			fmt.Fprintln(env.Stdout)
		}
		fmt.Fprintf(env.Stdout, "run: exit %d in %.1fs\n", exit, secs)
		return exit
	}
	logDir, layout, haveProject := runLogDir(env, *root)
	logPath := writeRunLog(logDir, start, buf.Bytes())
	if logPath != "-" {
		pruneRunLogs(logDir, *keep)
		if haveProject {
			if rel, err := filepath.Rel(layout.Root, logPath); err == nil && !strings.HasPrefix(rel, "..") {
				logPath = filepath.ToSlash(rel)
			}
		}
	}

	res := digest.Digest(full, digest.Options{MaxLines: *maxLines, Tail: *tail, OK: exit == 0})
	header := fmt.Sprintf("run: %s → exit %d in %.1fs, %d lines (log: %s)", name, exit, secs, res.Lines, logPath)

	var printed string
	if *asJSON {
		data, _ := json.MarshalIndent(map[string]any{
			"cmd": name, "exit": exit, "seconds": round1(secs), "lines": res.Lines,
			"log": logPath, "highlights": res.Highlights, "tail": res.Tail,
		}, "", "  ")
		printed = string(data) + "\n"
	} else {
		printed = digest.Render(header, res)
	}
	fmt.Fprint(env.Stdout, printed)

	if haveProject && len(full) > len(printed) {
		user := gabitotl.ActiveUserID()
		if user == "" {
			user = activeID(layout.Root)
		}
		_ = budget.RecordSavings(layout, user, "run", len(full), len(printed))
	}
	return exit
}

func round1(f float64) float64 { return float64(int(f*10+0.5)) / 10 }

// runLogDir returns where logs go: .gabitotl/tmp in a project, else the OS temp dir.
func runLogDir(env Env, flagRoot string) (string, gabitotl.Layout, bool) {
	root, err := resolveRoot(env, flagRoot)
	if err != nil {
		return os.TempDir(), gabitotl.Layout{}, false
	}
	layout := gabitotl.NewLayout(root)
	return filepath.Join(layout.Dir(), "tmp"), layout, true
}

const (
	defaultKeepLogs = 20
	// smallOutputBytes is the largest output printed verbatim instead of digested.
	smallOutputBytes = 2048
)

func countLines(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}

// pruneRunLogs deletes all but the newest keep files named run-*.log (best-effort).
// Names embed a sortable timestamp, so name order is age order.
func pruneRunLogs(dir string, keep int) {
	if keep <= 0 {
		return
	}
	logs, err := filepath.Glob(filepath.Join(dir, "run-*.log"))
	if err != nil || len(logs) <= keep {
		return
	}
	sort.Strings(logs)
	for _, p := range logs[:len(logs)-keep] {
		_ = os.Remove(p)
	}
}

// writeRunLog saves the full output and returns its path ("-" if it cannot be saved).
func writeRunLog(dir string, t time.Time, data []byte) string {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "-"
	}
	p := filepath.Join(dir, "run-"+t.Format("20060102-150405.000")+".log")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return "-"
	}
	return p
}
