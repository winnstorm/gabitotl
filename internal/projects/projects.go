// Package projects discovers the projects on this machine for the Manager. A
// Source port yields candidate folders (Claude Code, OpenCode, Kiro, the
// folders GabitoTL remembers); Scan merges them. Every source is read-only and
// best effort: an absent or unreadable source is skipped, never an error.
package projects

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Source names, as they appear in Project.Sources.
const (
	SrcGabitoTL   = "gabitotl"
	SrcClaudeCode = "claude-code"
	SrcOpenCode   = "opencode"
	SrcKiro       = "kiro"
)

// Project is one discovered folder.
type Project struct {
	Path        string
	Name        string
	Sources     []string
	HasGabitoTL bool
	IsGit       bool
	LastSeen    time.Time // best effort; zero when unknown
}

// Candidate is what a source reports before merging.
type Candidate struct {
	Path     string
	Source   string
	LastSeen time.Time
}

// Source is the discovery port. Discover must not modify anything.
type Source interface {
	Name() string
	Discover(ctx context.Context) ([]Candidate, error)
}

// Options bound a scan.
type Options struct {
	Home    string
	Windows bool          // path rules; defaults to the running OS via DefaultOptions
	Max     int           // max projects returned (default 500)
	Timeout time.Duration // total budget (default 5s)
}

// DefaultOptions are the rules of the running OS.
func DefaultOptions(home string) Options {
	return Options{Home: home, Windows: os.PathSeparator == '\\'}
}

// Normalize returns the display path and the dedupe key of p, or ok=false when
// p is not an absolute path. It is pure so Windows rules are testable anywhere.
func Normalize(p string, windows bool) (display, key string, ok bool) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", "", false
	}
	if !windows {
		if !strings.HasPrefix(p, "/") {
			return "", "", false
		}
		c := filepath.Clean(p)
		return c, c, true
	}
	s := path.Clean(strings.ReplaceAll(strings.TrimPrefix(p, `\\?\`), `\`, "/"))
	if len(s) < 2 || s[1] != ':' || !isLetter(s[0]) {
		return "", "", false
	}
	s = strings.ToUpper(s[:1]) + s[1:]
	display = strings.ReplaceAll(s, "/", `\`)
	return display, strings.ToLower(display), true
}

func isLetter(b byte) bool { return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' }

// isRoot reports a filesystem root ("/" or "C:\").
func isRoot(display string, windows bool) bool {
	if windows {
		return len(display) <= 3 && len(display) >= 2 && display[1] == ':'
	}
	return display == "/"
}

// Scan queries every source and merges the answers: paths are normalised,
// missing folders, the home folder and filesystem roots are dropped, duplicates
// merge their sources, and the result is sorted by LastSeen (newest first) then
// name. A failing source is skipped.
func Scan(ctx context.Context, srcs []Source, o Options) []Project {
	if o.Max <= 0 {
		o.Max = 500
	}
	if o.Timeout <= 0 {
		o.Timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()

	_, homeKey, _ := Normalize(o.Home, o.Windows)
	byKey := map[string]*Project{}
	mtime := map[string]time.Time{}
	for _, s := range srcs {
		cands, err := s.Discover(ctx)
		if err != nil {
			continue
		}
		for _, c := range cands {
			if ctx.Err() != nil {
				break
			}
			display, key, ok := Normalize(c.Path, o.Windows)
			if !ok || key == homeKey || isRoot(display, o.Windows) {
				continue
			}
			p, seen := byKey[key]
			if !seen {
				fi, err := os.Stat(display)
				if err != nil || !fi.IsDir() {
					continue
				}
				p = &Project{Path: display, Name: baseName(display, o.Windows)}
				mtime[key] = fi.ModTime()
				p.HasGabitoTL = isDir(filepath.Join(display, ".gabitotl"))
				_, gerr := os.Stat(filepath.Join(display, ".git"))
				p.IsGit = gerr == nil
				byKey[key] = p
				if len(byKey) >= o.Max {
					break
				}
			}
			p.addSource(c.Source)
			if c.LastSeen.After(p.LastSeen) {
				p.LastSeen = c.LastSeen
			}
		}
	}
	out := make([]Project, 0, len(byKey))
	for k, p := range byKey {
		if p.LastSeen.IsZero() {
			p.LastSeen = mtime[k] // folder mtime: a rough "last touched" when no source knows
		}
		sort.Strings(p.Sources)
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].LastSeen.Equal(out[j].LastSeen) {
			return out[i].LastSeen.After(out[j].LastSeen)
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func (p *Project) addSource(s string) {
	if s == "" {
		return
	}
	for _, have := range p.Sources {
		if have == s {
			return
		}
	}
	p.Sources = append(p.Sources, s)
}

func baseName(display string, windows bool) string {
	if windows {
		return display[strings.LastIndexByte(display, '\\')+1:]
	}
	return filepath.Base(display)
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
