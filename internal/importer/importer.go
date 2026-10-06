// Package importer reads other harnesses' project knowledge and synthesizes it
// into Knowledge items (spec section 23, installer spec sections 21 and 36).
//
// Importing means read and synthesize. Importers NEVER modify another
// harness' files, never copy configuration blindly, and skip anything that
// looks like a secret. What was imported is recorded in .gabitotl/imports.md
// (origin, date, content hash, what, last review) so an unchanged source is
// not imported again. Gentle-AI and Engram are detected independently:
// uninstalling one never implies touching the other.
package importer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

// Kind classifies a Knowledge item.
type Kind string

const (
	KindDecision     Kind = "decision"
	KindConvention   Kind = "convention"
	KindArchitecture Kind = "architecture"
	KindTask         Kind = "task"
	KindCommand      Kind = "command"
	KindConstraint   Kind = "constraint"
	KindTooling      Kind = "tooling"
)

// Knowledge is one useful fact extracted from another harness.
type Knowledge struct {
	Kind   Kind   `json:"kind"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	Origin string `json:"origin"`
	Path   string `json:"path,omitempty"` // source file, relative to the project or home
}

// ProjectInfo is one Engram project row (observation counts when available).
type ProjectInfo struct {
	Name         string
	Observations int
}

// Env carries every external dependency so tests use fixtures only.
type Env struct {
	Home        string
	ProjectRoot string
	ProjectKey  string // memory.md project_key, used to match Engram projects
	Getenv      func(string) string
	LookPath    func(string) (string, error)
	// Projects lists Engram projects (production: the T2 engram adapter's
	// Projects). Nil means unavailable.
	Projects func(context.Context) ([]ProjectInfo, error)
	Now      func() time.Time
}

func (e Env) getenv(k string) string {
	if e.Getenv == nil {
		return ""
	}
	return e.Getenv(k)
}

func (e Env) look(bin string) (string, bool) {
	if e.LookPath == nil {
		return "", false
	}
	p, err := e.LookPath(bin)
	return p, err == nil
}

func (e Env) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

// Signal is one piece of detection evidence.
type Signal struct {
	Name   string `json:"name"`
	Found  bool   `json:"found"`
	Detail string `json:"detail,omitempty"`
}

// Detection reports whether a source is present.
type Detection struct {
	Source  string   `json:"source"`
	Found   bool     `json:"found"`
	Signals []Signal `json:"signals"`
}

// Collected is what a source produced.
type Collected struct {
	Items   []Knowledge
	Skipped []string // "path: reason" (secret-looking, too large, unreadable)
}

// Source is one importer. Detect and Collect are read-only.
type Source interface {
	Name() string
	Detect(env Env) Detection
	Collect(ctx context.Context, env Env) (Collected, error)
}

// Sources returns the V1 importers in a stable order.
func Sources() []Source {
	return []Source{kiroSource{}, gentleAISource{}, engramSource{}, instructionsSource{}}
}

// Names lists the source names.
func Names() []string {
	var out []string
	for _, s := range Sources() {
		out = append(out, s.Name())
	}
	return out
}

// Hash is the content hash of a set of items.
func Hash(items []Knowledge) string {
	b, _ := json.Marshal(items)
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])[:16]
}

// Entry is one origin's record in imports.md.
type Entry struct {
	Origin     string   `yaml:"origin"`
	Date       string   `yaml:"date"`
	Hash       string   `yaml:"hash"`
	Version    string   `yaml:"version,omitempty"`
	Imported   []string `yaml:"imported,omitempty"`
	LastReview string   `yaml:"last_review"`
	Memory     bool     `yaml:"memory"` // the items were also saved to the memory provider
}

// Registry is the front matter of .gabitotl/imports.md.
type Registry struct {
	Imports []Entry `yaml:"imports"`
}

// LoadRegistry reads imports.md; a missing file is an empty registry.
func LoadRegistry(l gabitotl.Layout) (Registry, error) {
	var r Registry
	d, err := gabitotl.ReadDoc(l.Imports())
	if errors.Is(err, fs.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return r, err
	}
	if !d.HasFront {
		return r, nil
	}
	return r, d.Decode(&r)
}

// SaveRegistry writes imports.md keeping its Markdown body.
func SaveRegistry(l gabitotl.Layout, r Registry) error {
	if r.Imports == nil {
		r.Imports = []Entry{}
	}
	body := "\n# Imports\n\nRecords of material imported from other harnesses or tools. Imported content\nis historical context and never overrides current code or this configuration.\n"
	if d, err := gabitotl.ReadDoc(l.Imports()); err == nil {
		body = d.Body
	}
	return gabitotl.WriteDoc(l.Imports(), r, body)
}

func (r Registry) find(origin string) int {
	for i, e := range r.Imports {
		if e.Origin == origin {
			return i
		}
	}
	return -1
}

// Status of one source after Run.
const (
	StatusNotFound   = "not-found"
	StatusUnchanged  = "unchanged"
	StatusImported   = "imported"
	StatusWouldApply = "would-import"
	StatusFailed     = "failed"
)

// Outcome is the result for one source.
type Outcome struct {
	Source  string
	Status  string
	Hash    string
	Items   []Knowledge
	Skipped []string
	Err     string
}

// Options drive Run.
type Options struct {
	Only   []string // source names; empty means all
	DryRun bool
	Force  bool // import again even when the hash is unchanged
	// Sink saves one item to memory. Nil means registry only.
	Sink func(context.Context, Knowledge) error
}

// Run imports from the selected sources. Dry runs write nothing. A source whose
// hash is unchanged is skipped (only its last_review advances).
func Run(ctx context.Context, l gabitotl.Layout, env Env, opts Options) ([]Outcome, error) {
	reg, err := LoadRegistry(l)
	if err != nil {
		return nil, err
	}
	var out []Outcome
	dirty := false
	for _, s := range Sources() {
		if len(opts.Only) > 0 && !contains(opts.Only, s.Name()) {
			continue
		}
		o := Outcome{Source: s.Name()}
		if !s.Detect(env).Found {
			o.Status = StatusNotFound
			out = append(out, o)
			continue
		}
		c, err := s.Collect(ctx, env)
		if err != nil {
			o.Status, o.Err = StatusFailed, err.Error()
			out = append(out, o)
			continue
		}
		o.Items, o.Skipped, o.Hash = c.Items, c.Skipped, Hash(c.Items)
		today := env.now().Format("2006-01-02")
		i := reg.find(s.Name())
		switch {
		case i >= 0 && reg.Imports[i].Hash == o.Hash && !opts.Force:
			o.Status = StatusUnchanged
			if !opts.DryRun {
				reg.Imports[i].LastReview, dirty = today, true
			}
		case opts.DryRun:
			o.Status = StatusWouldApply
		default:
			memOK := opts.Sink != nil
			for _, k := range c.Items {
				if opts.Sink == nil {
					break
				}
				if err := opts.Sink(ctx, k); err != nil {
					o.Err = fmt.Sprintf("saving %q to memory: %v", k.Title, err)
					memOK = false
					break
				}
			}
			if o.Err != "" { // not recorded: the next run retries
				o.Status = StatusFailed
				break
			}
			e := Entry{Origin: s.Name(), Date: today, Hash: o.Hash, LastReview: today, Memory: memOK}
			for _, k := range c.Items {
				e.Imported = append(e.Imported, string(k.Kind)+": "+k.Title)
			}
			if i >= 0 {
				reg.Imports[i] = e
			} else {
				reg.Imports = append(reg.Imports, e)
			}
			o.Status, dirty = StatusImported, true
		}
		out = append(out, o)
	}
	if dirty {
		if err := SaveRegistry(l, reg); err != nil {
			return out, err
		}
	}
	return out, nil
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// --- shared extraction helpers -------------------------------------------

const (
	maxFileBytes = 256 << 10
	maxBodyChars = 1500
)

// readText reads a bounded text file. ok is false (with a reason) for
// unreadable, oversized or secret-looking files, which are skipped whole.
func readText(path string) (text, skip string, ok bool) {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return "", "unreadable", false
	}
	if st.Size() > maxFileBytes {
		return "", "too large", false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", "unreadable", false
	}
	if gabitotl.ValidateNoSecrets(b) != nil {
		return "", "looks like it contains a secret", false
	}
	return string(b), "", true
}

func excerpt(s string, max int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	cut := string(r[:max])
	if i := strings.LastIndexAny(cut, "\n. "); i > max/2 {
		cut = cut[:i]
	}
	return strings.TrimSpace(cut) + " ..."
}

func rel(base, p string) string {
	if r, err := filepath.Rel(base, p); err == nil {
		return filepath.ToSlash(r)
	}
	return p
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }
