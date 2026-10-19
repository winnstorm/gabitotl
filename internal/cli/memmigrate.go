package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/installer/ui"
	"github.com/winnstorm/gabitotl/internal/memory/migrate"
)

const migrateTimeout = 30 * time.Minute

func runMemoryMigrate(env Env, args []string) int {
	if len(args) == 0 || args[0] != "engram" {
		fmt.Fprintln(env.Stderr, "Usage: gabitotl memory migrate engram (--project NAME | --all-projects) [--export FILE] [--backup FILE] [--dry-run | --yes]")
		return ExitUsage
	}
	fs := flag.NewFlagSet("memory migrate engram", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	export := fs.String("export", "", "official `engram export` JSON (default: make the safety export with the engram CLI)")
	backup := fs.String("backup", "", "safety export that must exist and be non-empty (default: the export itself)")
	project := fs.String("project", "", "Engram project to import")
	allProjects := fs.Bool("all-projects", false, "import every project of the export (explicit opt-in)")
	target := fs.String("target-project", "", "project key to use in the team memory (default: the same name; needs --project)")
	personal := fs.Bool("include-personal", false, "also import personal-scope observations (off: they would become visible to the whole team)")
	instance := fs.String("instance", "", "named managed instance that holds the team database")
	reportPath := fs.String("report", "", "where to write the JSON report (default: a new folder under ~/.gabitotl/migrations)")
	dry := fs.Bool("dry-run", false, "count what would be imported and change nothing")
	yes := fs.Bool("yes", false, "do not ask for confirmation")
	root := fs.String("root", "", "project root (optional)")
	if err := fs.Parse(args[1:]); err != nil {
		return flagExit(err)
	}
	m := ui.Active("")
	if *project == "" && !*allProjects {
		fmt.Fprintf(env.Stderr, "%s %s\n", ui.SymError, m.Format("migrate.refuse_scope"))
		return ExitUsage
	}
	if *project != "" && *allProjects {
		fmt.Fprintln(env.Stderr, "gabitotl memory migrate engram: choose --project or --all-projects, not both")
		return ExitUsage
	}
	if *dry && *export == "" {
		fmt.Fprintf(env.Stderr, "%s %s\n", ui.SymError, m.Format("migrate.refuse_export"))
		return ExitUsage
	}
	ienv, err := installerEnvFn(projectRoot(env, *root))
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl memory migrate engram: %v\n", err)
		return ExitError
	}
	mig, err := ienv.Migrators.Lookup("engram", "memory-mcp")
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl memory migrate engram: %v\n", err)
		return ExitError
	}
	if !*dry {
		ok, refused := confirmed(env, m, *yes, m.Format("migrate.confirm"))
		if refused {
			return ExitError
		}
		if !ok {
			return ExitOK
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), migrateTimeout)
	defer cancel()

	dir := filepath.Join(ienv.StateDir(), "migrations", ienv.Clock().Format("2006-01-02_1504")+"_memory-import")
	exportPath := *export
	if exportPath == "" { // the official export, made by the engram CLI: the only thing run against Engram
		p, _, found := ienv.Engram(*project)
		if !found {
			fmt.Fprintf(env.Stderr, "%s %s\n", ui.SymError, m.Format("migrate.needs_engram"))
			return ExitError
		}
		if err := os.MkdirAll(filepath.Join(dir, "memory"), 0o700); err != nil {
			fmt.Fprintf(env.Stderr, "gabitotl memory migrate engram: %v\n", err)
			return ExitError
		}
		exportPath = filepath.Join(dir, "memory", "engram-export.json")
		if _, err := p.Export(ctx, exportPath, *project); err != nil {
			fmt.Fprintf(env.Stderr, "gabitotl memory migrate engram: the safety export failed, nothing was imported: %v\n", err)
			return ExitError
		}
	}
	backupPath := *backup
	if backupPath == "" {
		backupPath = exportPath
	}
	rp := *reportPath
	if rp == "" && !*dry {
		rp = filepath.Join(dir, "import-report.json")
	}
	req := migrate.Request{ExportPath: exportPath, BackupPath: backupPath, ReportPath: rp, Project: *project,
		IncludePersonal: *personal, DryRun: *dry, StoreInstance: *instance}
	if *project != "" {
		req.TargetProject = *target
	} else if *target != "" {
		fmt.Fprintln(env.Stderr, "gabitotl memory migrate engram: --target-project needs --project")
		return ExitUsage
	}
	res, err := mig.Migrate(ctx, req)
	renderMigration(env, m, res.Report, rp)
	if err != nil {
		fmt.Fprintf(env.Stderr, "%s gabitotl memory migrate engram: %v\n", ui.SymError, err)
		return ExitError
	}
	if !*dry && !res.Report.Verified {
		return ExitError
	}
	return ExitOK
}

func renderMigration(env Env, m ui.Messages, rep migrate.Report, reportPath string) {
	w := env.Stdout
	skipped := 0
	for _, n := range rep.Skipped {
		skipped += n
	}
	fmt.Fprintf(w, "%s\n", m.Format("migrate.title"))
	fmt.Fprintf(w, "%s %s\n", ui.SymOK, m.Format("migrate.counts", "observations", strconv.Itoa(rep.Observations), "eligible", strconv.Itoa(rep.Eligible), "skipped", strconv.Itoa(skipped)))
	if len(rep.Skipped) > 0 {
		fmt.Fprintf(w, "%s %s\n", ui.SymTodo, m.Format("migrate.skipped", "detail", kv(rep.Skipped)))
	}
	if len(rep.ByProject) > 0 {
		fmt.Fprintf(w, "%s %s\n", ui.SymTodo, m.Format("migrate.by_project", "detail", kv(rep.ByProject)))
	}
	if len(rep.ByType) > 0 {
		fmt.Fprintf(w, "%s %s\n", ui.SymTodo, m.Format("migrate.by_type", "detail", kv(rep.ByType)))
	}
	if len(rep.MappedTypes) > 0 {
		fmt.Fprintf(w, "%s %s\n", ui.SymWarn, m.Format("migrate.mapped", "detail", kv(rep.MappedTypes)))
	}
	fmt.Fprintf(w, "%s %s\n", ui.SymTodo, m.Format("migrate.extras", "summaries", strconv.Itoa(rep.Summaries), "relations", strconv.Itoa(rep.Relations), "prompts", strconv.Itoa(rep.Prompts)))
	fmt.Fprintf(w, "%s %s\n", ui.SymTodo, m.Format("migrate.actors", "known", strconv.Itoa(rep.KnownActors), "legacy", strconv.Itoa(rep.LegacyActors)))
	for _, wn := range rep.Warnings {
		fmt.Fprintf(w, "%s %s\n", ui.SymWarn, wn)
	}
	if rep.DryRun {
		fmt.Fprintf(w, "\n%s\n", m.Format("migrate.dry"))
		return
	}
	if rep.Status == "" {
		return
	}
	fmt.Fprintf(w, "\n%s %s\n", ui.SymOK, m.Format("migrate.done", "imported", strconv.Itoa(rep.Imported), "already", strconv.Itoa(rep.AlreadyPresent)))
	if rep.TopicKeysDropped > 0 {
		fmt.Fprintf(w, "%s %s\n", ui.SymWarn, m.Format("migrate.dropped", "count", strconv.Itoa(rep.TopicKeysDropped)))
	}
	if rep.Verified {
		fmt.Fprintf(w, "%s %s\n", ui.SymOK, m.Format("migrate.verified", "ok", strconv.Itoa(rep.SampleOK), "checked", strconv.Itoa(rep.SampleChecked)))
	} else if rep.Status == "complete" {
		fmt.Fprintf(w, "%s %s\n", ui.SymWarn, m.Format("migrate.unverified", "ok", strconv.Itoa(rep.SampleOK), "checked", strconv.Itoa(rep.SampleChecked)))
	}
	fmt.Fprintf(w, "%s %s\n", ui.SymOK, m.Format("migrate.untouched"))
	if reportPath != "" {
		fmt.Fprintf(w, "%s %s\n", ui.SymOK, m.Format("migrate.report", "path", reportPath))
	}
}

func kv(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + " " + strconv.Itoa(m[k])
	}
	return strings.Join(parts, ", ")
}
