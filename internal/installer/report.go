package installer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/importer"
	"github.com/winnstorm/gabitotl/internal/ownership"
)

// Report is the typed migration report (spec 22: Memoria / Configuración
// importada / No importado / Retirado / Preservado). Presenters render it; the
// installer only decides what goes in it. It never carries secrets.
type Report struct {
	Source      string
	ImportedAt  string
	Status      string // complete | partial
	Engram      bool   // Engram detected and reused
	Project     string // Engram project
	Observation int
	Backup      string   // snapshot path of the export, display form
	MemoryNote  string   // "" or NoteNoMigrator when team memory was requested without a tested migrator
	Imported    []string // "kind: title"
	NotImported []string
	Retired     []string
	Preserved   []string
}

const reportSection = "migration-"

// buildReport assembles the report from what actually happened.
func (r *run) buildReport(status string) Report {
	rep := Report{Source: r.pl.Options.Source, ImportedAt: r.env.Clock().Format("2006-01-02T15:04:05Z07:00"), Status: status}
	m := r.inv.Memory
	if m.Present() {
		rep.Engram, rep.Project, rep.Observation = true, m.ProjectMatch, m.Observations
		if r.snap != nil && r.snap.Manifest.Engram.Done {
			rep.Backup = tilde(r.env.Home, filepath.Join(r.snap.Dir, r.snap.Manifest.Engram.File))
		}
	}
	if r.pl.HasNote(NoteNoMigrator) {
		rep.MemoryNote = NoteNoMigrator
	}
	for _, o := range r.res.Imports {
		if o.Status == importer.StatusImported {
			for _, k := range o.Items {
				rep.Imported = append(rep.Imported, string(k.Kind)+": "+k.Title)
			}
		}
	}
	g := r.inv.Other.GentleAI
	if g.Found {
		rep.NotImported = append(rep.NotImported, "persona")
		if len(g.Components) > 0 {
			rep.NotImported = append(rep.NotImported, "components: "+strings.Join(g.Components, ", "))
		}
	}
	for _, a := range r.res.Retired.Personas {
		rep.Retired = append(rep.Retired, "persona: "+a.Runtime)
	}
	if len(r.res.Retired.GentleArgs) > 0 {
		rep.Retired = append(rep.Retired, "gentle-ai: "+strings.Join(r.res.Retired.GentleArgs, " "))
	}
	if rep.Engram {
		rep.Preserved = append(rep.Preserved, "engram")
	}
	rep.Preserved = append(rep.Preserved, "user-files", "unmanaged-config")
	if g.HasBackups {
		rep.Preserved = append(rep.Preserved, "gentle-ai-backups")
	}
	return rep
}

// DefaultMarkdown renders the report in English; the CLI injects the Spanish
// renderer (package ui) through Env.RenderReport.
func (rep Report) DefaultMarkdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "source: %s\nimported_at: %s\nstatus: %s\n\n## Memory\n\n", rep.Source, rep.ImportedAt, rep.Status)
	if rep.Engram {
		fmt.Fprintf(&b, "Engram detected and reused (project %q, %d observations). Backup: %s\n", rep.Project, rep.Observation, rep.Backup)
	} else {
		b.WriteString("Engram not found.\n")
	}
	list := func(title string, items []string) {
		fmt.Fprintf(&b, "\n## %s\n\n", title)
		for _, i := range items {
			fmt.Fprintf(&b, "- %s\n", i)
		}
		if len(items) == 0 {
			b.WriteString("- none\n")
		}
	}
	list("Imported configuration", rep.Imported)
	list("Not imported", rep.NotImported)
	list("Retired", rep.Retired)
	list("Preserved", rep.Preserved)
	return b.String()
}

// writeReport records the migration in .gabitotl/imports.md (inside a marked
// section, so the registry front matter and any text of the user stay intact)
// and an installation record in ~/.gabitotl/installations/.
func (r *run) writeReport(ctx context.Context, status string) error {
	r.phase(PhaseReport)
	rep := r.buildReport(status)
	if r.env.ProjectRoot != "" && r.pl.Options.Source == SourceGentleAI {
		l := gabitotl.NewLayout(r.env.ProjectRoot)
		d, err := gabitotl.ReadDoc(l.Imports())
		if err != nil {
			return err
		}
		reg, err := importer.LoadRegistry(l)
		if err != nil {
			return err
		}
		text := rep.DefaultMarkdown()
		if r.env.RenderReport != nil {
			text = r.env.RenderReport(rep)
		}
		body, err := ownership.UpsertSection(d.Body, reportSection+rep.Source, "\n"+text)
		if err != nil {
			return err
		}
		if err := gabitotl.WriteDoc(l.Imports(), reg, body); err != nil {
			return err
		}
		r.res.ReportPath = l.Imports()
	}
	rec := map[string]any{
		"id": r.snap.ID, "source": r.pl.Options.Source, "date": rep.ImportedAt, "status": status,
		"gabitotl_version": r.env.Version, "runtimes": r.res.Applied, "memory": r.pl.MemoryChoice,
		"snapshot": r.snap.ID, "gentle_ai_retired": len(r.res.Retired.GentleArgs) > 0,
	}
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(r.env.StateDir(), "installations", r.snap.ID+".json"), append(b, '\n'), 0o600)
}
