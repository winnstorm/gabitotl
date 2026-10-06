package engram

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/memory"
)

// Engram stores free-form content only (title, content, type, project, topic).
// The traceability fields are written as a trailer of "**Label**: value" lines
// after the body and parsed back from search results. Labels are fixed.
var trailerLabels = []string{"Actor", "Task", "Branch", "Commit", "Files", "Validation", "Next step", "Ojo", "Timestamp", "Source"}

var trailerRe = regexp.MustCompile(`^\*\*(Actor|Task|Branch|Commit|Files|Validation|Next step|Ojo|Timestamp|Source)\*\*: ?(.*)$`)

// renderContent returns the body followed by the traceability trailer.
func renderContent(e memory.Entry) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(e.Content))
	var lines []string
	add := func(label, v string) {
		if strings.TrimSpace(v) != "" {
			lines = append(lines, fmt.Sprintf("**%s**: %s", label, strings.TrimSpace(v)))
		}
	}
	add("Actor", e.Actor)
	add("Task", e.Task)
	add("Branch", e.Branch)
	add("Commit", e.Commit)
	add("Files", strings.Join(e.Files, ", "))
	add("Validation", e.Validation)
	add("Next step", e.NextStep)
	add("Ojo", e.Ojo)
	if !e.Timestamp.IsZero() {
		add("Timestamp", e.Timestamp.UTC().Format(time.RFC3339))
	}
	add("Source", e.Source)
	if len(lines) > 0 {
		b.WriteString("\n\n")
		b.WriteString(strings.Join(lines, "\n"))
	}
	return b.String()
}

// parseContent strips the trailer from content and fills the entry fields.
func parseContent(content string, e *memory.Entry) {
	var body []string
	for _, ln := range strings.Split(content, "\n") {
		m := trailerRe.FindStringSubmatch(strings.TrimRight(ln, "\r"))
		if m == nil {
			body = append(body, ln)
			continue
		}
		v := strings.TrimSpace(m[2])
		switch m[1] {
		case "Actor":
			e.Actor = v
		case "Task":
			e.Task = v
		case "Branch":
			e.Branch = v
		case "Commit":
			e.Commit = v
		case "Files":
			for _, f := range strings.Split(v, ",") {
				if f = strings.TrimSpace(f); f != "" {
					e.Files = append(e.Files, f)
				}
			}
		case "Validation":
			e.Validation = v
		case "Next step":
			e.NextStep = v
		case "Ojo":
			e.Ojo = v
		case "Timestamp":
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				e.Timestamp = t
			}
		case "Source":
			e.Source = v
		}
	}
	e.Content = strings.TrimSpace(strings.Join(body, "\n"))
}

var (
	headerRe = regexp.MustCompile(`^\[\d+\] #(\d+) \(([^)]*)\) — (.*?)(?: \((\d{4}-\d{2}-\d{2})\))?$`)
	metaRe   = regexp.MustCompile(`^\s+(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}) \| project: (\S+) \| scope: (\S+)\s*$`)
)

// parseSearch parses `engram search` human output. Search results carry a
// truncated content preview; the full text needs a direct observation lookup,
// which the CLI does not offer (see package doc).
func parseSearch(out string) []memory.Entry {
	var res []memory.Entry
	var cur *memory.Entry
	var body []string
	flush := func() {
		if cur != nil {
			parseContent(strings.Join(body, "\n"), cur)
			res = append(res, *cur)
		}
		cur, body = nil, nil
	}
	for _, ln := range strings.Split(out, "\n") {
		if m := headerRe.FindStringSubmatch(ln); m != nil {
			flush()
			cur = &memory.Entry{ID: memory.ID(m[1]), Type: memory.EntryType(m[2]), Title: m[3], Source: ""}
			continue
		}
		if cur == nil {
			continue
		}
		if m := metaRe.FindStringSubmatch(ln); m != nil {
			if t, err := time.Parse("2006-01-02 15:04:05", m[1]); err == nil {
				cur.Timestamp = t.UTC()
			}
			cur.Project = m[2]
			flush()
			continue
		}
		body = append(body, strings.TrimPrefix(ln, "    "))
	}
	flush()
	return res
}
