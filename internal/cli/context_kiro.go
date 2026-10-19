package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/budget"
)

// sourceKiro selects Kiro's own context usage figure (--source kiro).
const sourceKiro = "kiro"

// kiroTailBytes is how much of messages.jsonl is scanned first: the current
// usage is the last contextUsage line, which is almost always near the end.
const kiroTailBytes = 256 << 10

// measureFromSource sets the monitor from a runtime's own usage figure.
func measureFromSource(source string, m *budget.Monitor, root string) error {
	switch source {
	case sourceKiro:
		pct, ok := KiroUsage(kiroHome(), root)
		if !ok {
			return fmt.Errorf("no Kiro session with a context usage figure found for %s", root)
		}
		return m.SetPercent(pct)
	}
	return fmt.Errorf("unknown --source %q (want %s)", source, sourceKiro)
}

// runSourceHook is the silent, never-failing hook path for --source.
func runSourceHook(env Env, format, source, cwd string) int {
	if cwd == "" && env.Getwd != nil {
		cwd, _ = env.Getwd()
	}
	m, _, root, err := openMonitor(env, cwd)
	if err != nil || measureFromSource(source, m, root) != nil {
		return ExitOK
	}
	d, err := m.Check()
	if err != nil || !d.ShouldSave {
		return ExitOK
	}
	emitHook(env, format, "UserPromptSubmit", checkpointMessage(d))
	return ExitOK
}

func kiroHome() string {
	if h := os.Getenv("KIRO_HOME"); h != "" {
		return h
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(h, ".kiro")
}

type kiroSession struct {
	WorkspacePaths []string `json:"workspacePaths"`
	RootPaths      []string `json:"rootPaths"`
	LastModifiedAt string   `json:"lastModifiedAt"`
}

func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func (s kiroSession) covers(root string) bool {
	for _, p := range append(append([]string{}, s.WorkspacePaths...), s.RootPaths...) {
		if p != "" && samePath(p, root) {
			return true
		}
	}
	return false
}

// KiroUsage returns the context usage percentage (0-100) Kiro recorded in the
// newest session whose workspace is root. ok is false when no such session or
// figure exists. Layout: <home>/sessions/<id>/sess_<uuid>/{session.json,messages.jsonl}.
func KiroUsage(home, root string) (float64, bool) {
	if home == "" || root == "" {
		return 0, false
	}
	root, _ = filepath.Abs(root)
	dirs, _ := filepath.Glob(filepath.Join(home, "sessions", "*", "sess_*"))
	var best string
	var bestAt time.Time
	for _, d := range dirs {
		raw, err := os.ReadFile(filepath.Join(d, "session.json"))
		if err != nil {
			continue
		}
		var s kiroSession
		if json.Unmarshal(raw, &s) != nil || !s.covers(root) {
			continue
		}
		at, _ := time.Parse(time.RFC3339, s.LastModifiedAt)
		if best == "" || at.After(bestAt) {
			best, bestAt = d, at
		}
	}
	if best == "" {
		return 0, false
	}
	return lastContextUsage(filepath.Join(best, "messages.jsonl"))
}

// lastContextUsage finds the last contextUsage metadata line: the tail of the
// file first, the whole file only when the tail has none.
func lastContextUsage(path string) (float64, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0, false
	}
	if st.Size() > kiroTailBytes {
		buf := make([]byte, kiroTailBytes)
		if n, err := f.ReadAt(buf, st.Size()-kiroTailBytes); err == nil || err == io.EOF {
			if pct, ok := scanUsage(bytes.NewReader(buf[:n])); ok {
				return pct, true
			}
		}
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return 0, false
	}
	return scanUsage(f)
}

func scanUsage(r io.Reader) (pct float64, found bool) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 16<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.Contains(line, []byte("contextUsage")) {
			continue
		}
		var rec struct {
			Payload struct {
				Type  string `json:"type"`
				Key   string `json:"key"`
				Value struct {
					Pct *float64 `json:"usagePercentage"`
				} `json:"value"`
			} `json:"payload"`
		}
		if json.Unmarshal(line, &rec) != nil || rec.Payload.Type != "session_metadata" ||
			rec.Payload.Key != "contextUsage" || rec.Payload.Value.Pct == nil {
			continue // malformed or a truncated tail line
		}
		pct, found = *rec.Payload.Value.Pct, true
	}
	return
}
