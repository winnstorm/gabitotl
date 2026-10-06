package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"strings"

	"github.com/winnstorm/gabitotl/internal/coordination"
	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/memory"
	"github.com/winnstorm/gabitotl/internal/persona"
)

func layoutOf(root string) gabitotl.Layout { return gabitotl.NewLayout(root) }

func runWork(env Env, args []string) int {
	return subcommands(env, "work", args, map[string]func(Env, []string) int{
		"start":   func(e Env, a []string) int { return runWorkWrite(e, a, true) },
		"update":  func(e Env, a []string) int { return runWorkWrite(e, a, false) },
		"status":  runWorkStatus,
		"overlap": runWorkOverlap,
	})
}

func runWorkStatus(env Env, args []string) int {
	fs := flag.NewFlagSet("work status", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	root := fs.String("root", "", "project root")
	user := fs.String("user", "", "only this developer's tasks (default: everyone)")
	all := fs.Bool("all", false, "include finished tasks")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	dir, err := resolveRoot(env, *root)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl work status: %v\n", err)
		return ExitError
	}
	layout := layoutOf(dir)
	if h, ok := coordination.Inspect(layout); ok {
		fmt.Fprintf(env.Stdout, "session: %s (pid %d) holds this working directory\n", h.User, h.PID)
	}
	tasks, err := coordination.ListTasks(layout, *user)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl work status: %v\n", err)
		return ExitError
	}
	shown := 0
	for _, t := range tasks {
		if !*all && !t.Active() {
			continue
		}
		shown++
		fmt.Fprintf(env.Stdout, "%s/%s [%s] branch=%s\n", t.Owner, t.ID, t.Status, dash(t.Branch))
		if t.State.Next != "" {
			fmt.Fprintf(env.Stdout, "    next: %s\n", t.State.Next)
		}
		if len(t.State.Todo) > 0 {
			fmt.Fprintf(env.Stdout, "    todo: %s\n", strings.Join(t.State.Todo, "; "))
		}
	}
	if shown == 0 {
		fmt.Fprintln(env.Stdout, "no active work")
	}
	return ExitOK
}

func runWorkOverlap(env Env, args []string) int {
	fs := flag.NewFlagSet("work overlap", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	root := fs.String("root", "", "project root")
	user := fs.String("user", "", "developer id (default: active identity)")
	task := fs.String("task", "", "your task id")
	areas := fs.String("areas", "", "comma-separated areas (modules or paths) you are about to touch")
	files := fs.String("files", "", "comma-separated files you are about to touch")
	noMemory := fs.Bool("no-memory", false, "only check active-work files, skip memory")
	asJSON := fs.Bool("json", false, "print as JSON")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	if *areas == "" && *files == "" {
		fmt.Fprintln(env.Stderr, "Usage: gabitotl work overlap [--user ID] [--task ID] [--root DIR] [--no-memory] [--json] --areas a,b [--files x,y]")
		return ExitUsage
	}
	dir, err := resolveRoot(env, *root)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl work overlap: %v\n", err)
		return ExitError
	}
	u, err := userID(dir, *user)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl work overlap: %v\n", err)
		return ExitError
	}
	probe := coordination.Probe{User: u, Task: *task, Areas: splitList(*areas), Files: splitList(*files)}
	var mem memory.Provider
	var warnings []string
	if !*noMemory {
		p, cfg, err := openMemory(env, dir)
		if err != nil {
			warnings = append(warnings, "memory unavailable: "+err.Error())
		} else {
			mem, probe.Project = p, cfg.ProjectKey
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), memoryTimeout)
	defer cancel()
	rep := coordination.CheckOverlap(ctx, layoutOf(dir), mem, probe)
	rep.Warnings = append(warnings, rep.Warnings...)
	if *asJSON {
		enc := json.NewEncoder(env.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rep)
		return ExitOK
	}
	msgs := persona.DefaultMessages()
	if len(rep.Findings) == 0 {
		fmt.Fprintln(env.Stdout, "no overlap found")
	}
	for _, f := range rep.Findings {
		fmt.Fprintf(env.Stdout, "overlap with %s (task %s, branch %s, via %s): %s\n",
			f.Who, dash(f.Task), dash(f.Branch), f.Source, strings.Join(f.SharedAreas, ", "))
		fmt.Fprintf(env.Stdout, "  **%s:** %s\n", msgs[persona.KeyBlockOjo], msgs.OverlapOjo(f.Who, strings.Join(f.SharedAreas, ", ")))
	}
	for _, w := range rep.Warnings {
		fmt.Fprintf(env.Stderr, "warning: %s\n", w)
	}
	return ExitOK // advisory: never fails because of an overlap
}

// runWorkWrite implements `work start` (create) and `work update` (merge) for
// the caller's own active-work file, so soft locks see what they are touching.
func runWorkWrite(env Env, args []string, start bool) int {
	name := "work update"
	if start {
		name = "work start"
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	root := fs.String("root", "", "project root")
	user := fs.String("user", "", "developer id (default: active identity)")
	task := fs.String("task", "", "task id (required)")
	areas := fs.String("areas", "", "comma-separated areas (modules or paths) you touch")
	files := fs.String("files", "", "comma-separated files you touch")
	branch := fs.String("branch", "", "task branch")
	status := fs.String("status", "", "in-progress, blocked, paused or done (default for start: in-progress)")
	objective := fs.String("objective", "", "one-line objective")
	next := fs.String("next", "", "next step")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	if *task == "" {
		fmt.Fprintf(env.Stderr, "Usage: gabitotl %s --task ID [--areas a,b] [--files x,y] [--branch B] [--status S] [--objective T] [--next T] [--user ID] [--root DIR]\n", name)
		return ExitUsage
	}
	dir, err := resolveRoot(env, *root)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl %s: %v\n", name, err)
		return ExitError
	}
	u, err := userID(dir, *user)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl %s: %v\n", name, err)
		return ExitError
	}
	layout := layoutOf(dir)
	t, rerr := coordination.ReadTask(layout, u, *task)
	switch {
	case start && rerr != nil:
		t = coordination.Task{ActiveWork: coordination.ActiveWork{ID: *task, Owner: u, Status: coordination.StatusInProgress}}
	case rerr != nil:
		fmt.Fprintf(env.Stderr, "gabitotl %s: no task %s/%s (use `work start`): %v\n", name, u, *task, rerr)
		return ExitError
	}
	if *areas != "" {
		t.Areas = splitList(*areas)
	}
	if *files != "" {
		t.Files = splitList(*files)
	}
	if *branch != "" {
		t.Branch = *branch
	}
	if *status != "" {
		t.Status = *status
	}
	if *objective != "" {
		t.State.Objective = *objective
	}
	if *next != "" {
		t.State.Next = *next
	}
	if err := coordination.WriteTask(layout, t); err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl %s: %v\n", name, err)
		return ExitError
	}
	fmt.Fprintf(env.Stdout, "%s/%s [%s] branch=%s areas=%s\n", t.Owner, t.ID, t.Status, dash(t.Branch), dash(strings.Join(t.Areas, ",")))
	return ExitOK
}
