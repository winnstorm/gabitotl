// Package stats measures what GabitoTL costs and saves, per person. Everything
// is best-effort and an estimate: a missing or unreadable file counts as zero,
// and nothing here ever returns an error.
package stats

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/budget"
	"github.com/winnstorm/gabitotl/internal/codeindex"
	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/persona"
	"github.com/winnstorm/gabitotl/internal/skills"
	"github.com/winnstorm/gabitotl/internal/tokens"
)

// Context is the current session.
type Context struct {
	Used        int     `json:"used_tokens"`
	Limit       int     `json:"limit_tokens"`
	Percent     float64 `json:"percent"`
	Source      string  `json:"source"` // reported or estimated
	Checkpoints int     `json:"checkpoints"`
}

// Saving is the total of one savings kind (run, memory).
type Saving struct {
	Runs        int `json:"runs"`
	FullBytes   int `json:"full_bytes"`
	PrintedByte int `json:"printed_bytes"`
	SavedTokens int `json:"saved_tokens_estimated"`
}

// AlwaysOn is the fixed cost of every chat, in chars/4 tokens.
type AlwaysOn struct {
	Persona int `json:"persona_tokens"`
	Skills  int `json:"skill_description_tokens"`
	Total   int `json:"total_tokens"`
}

// Report is the whole output of `gabitotl stats`.
type Report struct {
	User     string            `json:"user"`
	Since    string            `json:"since,omitempty"`
	Context  Context           `json:"context"`
	Savings  map[string]Saving `json:"savings"`
	AlwaysOn AlwaysOn          `json:"always_on"`
	Index    string            `json:"code_index"`
}

// ParseSince accepts Nd, Nw (days, weeks) or any time.ParseDuration value.
// Empty or invalid input means "all time" (0).
func ParseSince(s string) time.Duration {
	s = strings.TrimSpace(s)
	if len(s) > 1 {
		unit := map[byte]time.Duration{'d': 24 * time.Hour, 'w': 7 * 24 * time.Hour}[s[len(s)-1]]
		if unit > 0 {
			if n, err := strconv.Atoi(s[:len(s)-1]); err == nil && n > 0 {
				return time.Duration(n) * unit
			}
			return 0
		}
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0
	}
	return d
}

// Collect builds the report for user. since > 0 limits the savings to events
// newer than now-since; the session and fixed costs are always current.
func Collect(layout gabitotl.Layout, user string, since time.Duration, now time.Time) Report {
	r := Report{User: user, Savings: map[string]Saving{}}
	if since > 0 {
		r.Since = since.String()
	}
	cfg := budget.DefaultConfig()
	if wf, err := gabitotl.LoadWorkflow(layout); err == nil {
		cfg = budget.FromWorkflow(wf)
	}
	r.Context = Context{Limit: cfg.Limit, Source: budget.SourceEstimated}
	if m, err := budget.New(layout, user, cfg); err == nil {
		d := m.Peek()
		r.Context = Context{Used: d.Used, Limit: d.Limit, Percent: d.Percent, Source: d.Source, Checkpoints: m.Checkpoints()}
	}
	if dir, err := layout.LocalUserDir(user); err == nil {
		r.Savings = readSavings(filepath.Join(dir, budget.SavingsFile), since, now)
	}
	r.AlwaysOn = fixedCost()
	r.Index = codeindex.Quick(layout.Root)
	return r
}

func readSavings(path string, since time.Duration, now time.Time) map[string]Saving {
	out := map[string]Saving{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		p := strings.Fields(sc.Text())
		if len(p) != 4 {
			continue
		}
		ts, e1 := strconv.ParseInt(p[0], 10, 64)
		full, e2 := strconv.Atoi(p[2])
		printed, e3 := strconv.Atoi(p[3])
		if e1 != nil || e2 != nil || e3 != nil || full < 0 || printed < 0 {
			continue
		}
		if since > 0 && time.Unix(ts, 0).Before(now.Add(-since)) {
			continue
		}
		s := out[p[1]]
		s.Runs++
		s.FullBytes += full
		s.PrintedByte += printed
		out[p[1]] = s
	}
	for k, s := range out {
		if s.FullBytes > s.PrintedByte {
			s.SavedTokens = tokens.FromBytes(s.FullBytes - s.PrintedByte)
		}
		out[k] = s
	}
	return out
}

func fixedCost() AlwaysOn {
	a := AlwaysOn{Persona: tokens.EstimateLegacy(persona.DefaultText())}
	for _, s := range skills.All() {
		a.Skills += tokens.EstimateLegacy(s.Name + ": " + description(s.Content))
	}
	a.Total = a.Persona + a.Skills
	return a
}

// description returns the front-matter description of a SKILL.md.
func description(content string) string {
	for _, line := range strings.Split(content, "\n") {
		if v, ok := strings.CutPrefix(line, "description:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// Kinds returns the savings kinds in stable order.
func (r Report) Kinds() []string {
	k := make([]string, 0, len(r.Savings))
	for n := range r.Savings {
		k = append(k, n)
	}
	sort.Strings(k)
	return k
}
