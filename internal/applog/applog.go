// Package applog is the small diagnostics log of the desktop app: a size-rotated
// text file under the user's data folder. The GUI executable has no console, so
// this file (and the message box for fatal errors) is the only way to learn why
// something failed on a machine we cannot see. It is plain Go and tested on every
// platform.
//
// Nothing secret goes in: every line passes through Scrub, which masks token-like
// values, and callers log paths, versions and error text, never credentials or
// environment values.
package applog

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strconv"
	"sync"
	"time"
)

const (
	// DefaultMax is the size at which the log file rotates.
	DefaultMax = 1 << 20
	// FileName is the log file inside its folder.
	FileName = "manager.log"
)

// PathIn is the log file for a given local application data folder:
// <localAppData>\GabitoTL\logs\manager.log.
func PathIn(localAppData string) string {
	return filepath.Join(localAppData, "GabitoTL", "logs", FileName)
}

// Logger appends timestamped lines to a file and rotates it. A nil *Logger is
// valid and discards everything, so callers never need a nil check.
type Logger struct {
	mu   sync.Mutex
	path string
	max  int64
	size int64
	f    *os.File
}

// Open prepares the log at path. The file rotates to path+".1" when it would
// pass max bytes; only the current file and one previous one are kept.
func Open(path string, max int64) (*Logger, error) {
	if max <= 0 {
		max = DefaultMax
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	l := &Logger{path: path, max: max}
	if err := l.openLocked(); err != nil {
		return nil, err
	}
	return l, nil
}

// Path is the current log file ("" for a nil logger).
func (l *Logger) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// Dir is the folder holding the log files.
func (l *Logger) Dir() string {
	if l == nil {
		return ""
	}
	return filepath.Dir(l.path)
}

func (l *Logger) openLocked() error {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if st, err := f.Stat(); err == nil {
		l.size = st.Size()
	}
	l.f = f
	return nil
}

func (l *Logger) rotateLocked() {
	if l.f != nil {
		_ = l.f.Close()
		l.f = nil
	}
	_ = os.Remove(l.path + ".1")
	_ = os.Rename(l.path, l.path+".1")
	l.size = 0
	_ = l.openLocked()
}

// Infof writes one line.
func (l *Logger) Infof(format string, a ...any) { l.write("INFO", fmt.Sprintf(format, a...)) }

// Errorf writes one error line.
func (l *Logger) Errorf(format string, a ...any) { l.write("ERROR", fmt.Sprintf(format, a...)) }

// Panic logs a recovered panic value with the stack of the goroutine.
func (l *Logger) Panic(where string, v any) {
	l.write("PANIC", fmt.Sprintf("%s: %v\n%s", where, v, debug.Stack()))
}

// Close releases the file.
func (l *Logger) Close() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil {
		_ = l.f.Close()
		l.f = nil
	}
}

func (l *Logger) write(level, msg string) {
	if l == nil {
		return
	}
	line := time.Now().Format("2006-01-02 15:04:05.000") + " pid=" + strconv.Itoa(os.Getpid()) + " " + level + " " + Scrub(msg) + "\n"
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		if l.openLocked() != nil {
			return
		}
	}
	if l.size > 0 && l.size+int64(len(line)) > l.max {
		l.rotateLocked()
		if l.f == nil {
			return
		}
	}
	n, _ := l.f.WriteString(line)
	l.size += int64(n)
}

var (
	// key=value / key: value pairs whose key names a secret.
	secretPair = regexp.MustCompile(`(?i)\b([a-z0-9_.-]*(?:token|secret|password|passwd|authorization|api[_-]?key)[a-z0-9_.-]*)\s*([=:])\s*("[^"]*"|'[^']*'|\S+)`)
	// well-known token shapes, wherever they appear.
	secretShape = regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{16,}|github_pat_[A-Za-z0-9_]{20,}|glpat-[A-Za-z0-9_-]{16,}|xox[abp]-[A-Za-z0-9-]{10,}|ATBB[A-Za-z0-9]{16,})\b`)
	bearer      = regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=-]{8,}`)
)

// Scrub masks token-like values in s.
func Scrub(s string) string {
	s = bearer.ReplaceAllString(s, "$1 ***")
	s = secretPair.ReplaceAllString(s, "$1$2***")
	return secretShape.ReplaceAllString(s, "***")
}

// SafeArgs renders a command line for the log: flags are kept, any other
// argument is dropped (it could be a path with a user name or a value).
func SafeArgs(args []string) string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if len(a) > 1 && a[0] == '-' && !containsEq(a) {
			out = append(out, a)
		} else {
			out = append(out, "<arg>")
		}
	}
	return fmt.Sprint(out)
}

func containsEq(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			return true
		}
	}
	return false
}
