// Package engram adapts the real `engram` CLI (v2.x) to memory.Provider.
//
// Real command surface used (verified against `engram` 2.0.0):
//
//	engram search <query> [--type T] [--project P|--all] [--scope S] [--limit N] [--match all|any]
//	engram save <title> <content> [--type T] [--project P] [--scope S] [--topic KEY]
//	engram doctor [--json] [--project P]            (read-only)
//	engram projects list
//	engram export <file> [--project P|--all]
//
// Gaps (no CLI equivalent, so typed ErrUnsupported):
//   - Update: the CLI has no update command. Saving again with the same topic
//     key upserts the observation; callers that need updates should use topic keys.
//   - SessionSummary: only the MCP server exposes mem_session_summary. The
//     memory.Recorder falls back to saving a progress entry.
//
// The Engram database file is never opened or modified directly; every
// operation goes through the CLI. ENGRAM_DATA_DIR is honored via the inherited
// environment (or Options.DataDir).
package engram

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/memory"
)

// Name is the provider name in memory.md.
const Name = gabitotl.MemoryEngram

// ErrUnsupported aliases memory.ErrUnsupported for callers of this package.
var ErrUnsupported = memory.ErrUnsupported

// Options configure the adapter.
type Options struct {
	Runner  Runner
	Project string // default project (memory.md project_key)
	Scope   string // optional engram scope, default "project"
}

// Provider is the Engram CLI adapter.
type Provider struct{ opt Options }

var _ memory.Provider = (*Provider)(nil)

// New builds a Provider over a Runner.
func New(opt Options) *Provider { return &Provider{opt: opt} }

// FromConfig discovers the real binary and builds a Provider for cfg.
func FromConfig(cfg gabitotl.MemoryConfig) (memory.Provider, error) {
	r, err := NewExecRunner("")
	if err != nil {
		return nil, err
	}
	return New(Options{Runner: r, Project: cfg.ProjectKey}), nil
}

func (p *Provider) project(q string) string {
	if q != "" {
		return q
	}
	return p.opt.Project
}

// Search runs `engram search`.
func (p *Provider) Search(ctx context.Context, q memory.Query) ([]memory.Entry, error) {
	text := strings.TrimLeft(strings.TrimSpace(q.Text), "-")
	if text == "" {
		return nil, errors.New("engram: empty search text")
	}
	args := []string{"search", text}
	if q.Type != "" {
		args = append(args, "--type", string(q.Type))
	}
	if pr := p.project(q.Project); pr != "" {
		args = append(args, "--project", pr)
	}
	if q.Limit > 0 {
		args = append(args, "--limit", strconv.Itoa(q.Limit))
	}
	if q.MatchAny {
		args = append(args, "--match", "any")
	}
	out, _, err := p.opt.Runner.Run(ctx, args...)
	if err != nil {
		return nil, err
	}
	return parseSearch(string(out)), nil
}

var savedRe = regexp.MustCompile(`Memory saved: #(\d+)`)

// Save runs `engram save`. Traceability fields travel in a content trailer.
func (p *Provider) Save(ctx context.Context, e memory.Entry) (memory.ID, error) {
	args := []string{"save", e.Title, renderContent(e), "--type", string(e.Type)}
	if pr := p.project(e.Project); pr != "" {
		args = append(args, "--project", pr)
	}
	if p.opt.Scope != "" {
		args = append(args, "--scope", p.opt.Scope)
	}
	if e.TopicKey != "" {
		args = append(args, "--topic", e.TopicKey)
	}
	out, _, err := p.opt.Runner.Run(ctx, args...)
	if err != nil {
		return "", err
	}
	m := savedRe.FindStringSubmatch(string(out))
	if m == nil {
		return "", fmt.Errorf("engram save: unexpected output %q", strings.TrimSpace(string(out)))
	}
	return memory.ID(m[1]), nil
}

// Update is unsupported: the engram CLI has no update command. Save with the
// same topic key to upsert.
func (p *Provider) Update(context.Context, memory.ID, memory.EntryPatch) error {
	return fmt.Errorf("engram update: %w (use Save with the same topic key to upsert)", ErrUnsupported)
}

// SessionSummary is unsupported over the CLI (MCP-only in engram).
func (p *Provider) SessionSummary(context.Context, memory.Summary) error {
	return fmt.Errorf("engram session summary: %w (MCP-only; record a progress entry instead)", ErrUnsupported)
}

// CurrentProject returns the configured project key. The CLI does not expose
// working-directory project detection (MCP-only), so without a configured key
// it returns ErrUnsupported.
func (p *Provider) CurrentProject(context.Context) (string, error) {
	if p.opt.Project != "" {
		return p.opt.Project, nil
	}
	return "", fmt.Errorf("engram current project: %w (no project_key configured)", ErrUnsupported)
}

type doctorJSON struct {
	Status  string `json:"status"`
	Project string `json:"project"`
	Checks  []struct {
		CheckID  string `json:"check_id"`
		Result   string `json:"result"`
		Message  string `json:"message"`
		NextStep string `json:"safe_next_step"`
	} `json:"checks"`
}

// Doctor runs the read-only `engram doctor --json`, scoped to the project.
func (p *Provider) Doctor(ctx context.Context) memory.Report {
	rep := memory.Report{Provider: Name, Project: p.opt.Project}
	args := []string{"doctor", "--json"}
	if p.opt.Project != "" {
		args = append(args, "--project", p.opt.Project)
	}
	out, _, err := p.opt.Runner.Run(ctx, args...)
	var d doctorJSON
	// doctor exits non-zero when it finds errors but still prints the report.
	if jerr := json.Unmarshal(out, &d); jerr != nil {
		msg := "engram doctor produced no readable report"
		if err != nil {
			msg = err.Error()
		}
		rep.Checks = append(rep.Checks, memory.Check{Name: "engram", Status: memory.StatusError, Message: msg})
		return rep
	}
	rep.Checks = append(rep.Checks, memory.Check{Name: "engram", Status: memory.StatusOK, Message: "CLI reachable"})
	for _, c := range d.Checks {
		st := memory.StatusOK
		switch c.Result {
		case "ok":
		case "warning":
			st = memory.StatusWarn
		default: // blocked, error and anything unknown fail closed
			st = memory.StatusError
		}
		rep.Checks = append(rep.Checks, memory.Check{Name: c.CheckID, Status: st, Message: c.Message})
	}
	return rep
}

// ProjectInfo is one row of `engram projects list`.
type ProjectInfo struct {
	Name         string `json:"name"`
	Observations int    `json:"observations"`
	Sessions     int    `json:"sessions"`
	Prompts      int    `json:"prompts"`
}

var projectRe = regexp.MustCompile(`^\s+(\S+)\s+(\d+) obs\s+(\d+) sessions?\s+(\d+) prompts?`)

// Projects runs `engram projects list` (read-only).
func (p *Provider) Projects(ctx context.Context) ([]ProjectInfo, error) {
	out, _, err := p.opt.Runner.Run(ctx, "projects", "list")
	if err != nil {
		return nil, err
	}
	var res []ProjectInfo
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		if m := projectRe.FindStringSubmatch(sc.Text()); m != nil {
			o, _ := strconv.Atoi(m[2])
			s, _ := strconv.Atoi(m[3])
			pr, _ := strconv.Atoi(m[4])
			res = append(res, ProjectInfo{Name: m[1], Observations: o, Sessions: s, Prompts: pr})
		}
	}
	return res, sc.Err()
}

// ExportResult summarizes an official `engram export`.
type ExportResult struct {
	Path         string `json:"path"`
	Sessions     int    `json:"sessions"`
	Observations int    `json:"observations"`
	Prompts      int    `json:"prompts"`
}

var exportCountRe = regexp.MustCompile(`(Sessions|Observations|Prompts):\s+(\d+)`)

// Export runs the official `engram export <path> [--project P]`. An empty
// project exports the CLI default scope (see `engram export`); pass the project
// name for a scoped export. path must be explicit and must not look like a flag.
func (p *Provider) Export(ctx context.Context, path, project string) (ExportResult, error) {
	if path == "" || strings.HasPrefix(path, "-") {
		return ExportResult{}, fmt.Errorf("engram export: invalid path %q", path)
	}
	args := []string{"export", path}
	if project != "" {
		args = append(args, "--project", project)
	}
	out, _, err := p.opt.Runner.Run(ctx, args...)
	if err != nil {
		return ExportResult{}, err
	}
	res := ExportResult{Path: path}
	for _, m := range exportCountRe.FindAllStringSubmatch(string(out), -1) {
		n, _ := strconv.Atoi(m[2])
		switch m[1] {
		case "Sessions":
			res.Sessions = n
		case "Observations":
			res.Observations = n
		case "Prompts":
			res.Prompts = n
		}
	}
	if !strings.Contains(string(out), "Exported to") {
		return res, fmt.Errorf("engram export: unexpected output %q", strings.TrimSpace(string(out)))
	}
	return res, nil
}
