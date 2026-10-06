package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"time"
)

func runVCS(env Env, args []string) int {
	return subcommands(env, "vcs", args, map[string]func(Env, []string) int{"status": runVCSStatus, "branch": runVCSBranch})
}

func runVCSStatus(env Env, args []string) int {
	fs := flag.NewFlagSet("vcs status", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	root := fs.String("root", "", "project root (default: Git top-level of the current directory)")
	asJSON := fs.Bool("json", false, "print as JSON")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	dir, err := resolveRoot(env, *root)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl vcs status: %v\n", err)
		return ExitError
	}
	repo, err := openRepo(env, dir)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl vcs status: %v\n", err)
		return ExitError
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	st, err := repo.Status(ctx)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl vcs status: %v\n", err)
		return ExitError
	}
	if *asJSON {
		enc := json.NewEncoder(env.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(struct {
			Status      any `json:"status"`
			Configured  any `json:"configured_provider"`
			Permissions any `json:"permissions"`
		}{st, repo.Config.Provider, repo.Config.Permissions})
		return ExitOK
	}
	if !st.IsRepo {
		fmt.Fprintf(env.Stdout, "not a git repository: %s\n", dir)
		return ExitError
	}
	p := repo.Config.Permissions
	fmt.Fprintf(env.Stdout, "root: %s\nbranch: %s\nhead: %s\nremote: %s\nprovider: %s (vcs.md: %s)\ndirty: %t\n",
		st.Root, dash(st.Branch), dash(st.Head), dash(st.Remote), st.Provider, repo.Config.Provider, st.Dirty)
	fmt.Fprintf(env.Stdout, "permissions: branches=%s commits=%s push=%s pull_requests=%s\n", p.Branches, p.Commits, p.Push, p.PullRequests)
	return ExitOK
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func runVCSBranch(env Env, args []string) int {
	fs := flag.NewFlagSet("vcs branch", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	root := fs.String("root", "", "project root (default: Git top-level of the current directory)")
	task := fs.String("task", "", "task id; the branch is feature/<task> (required)")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	if *task == "" {
		fmt.Fprintln(env.Stderr, "Usage: gabitotl vcs branch --task ID [--root DIR]")
		return ExitUsage
	}
	dir, err := resolveRoot(env, *root)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl vcs branch: %v\n", err)
		return ExitError
	}
	repo, err := openRepo(env, dir)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl vcs branch: %v\n", err)
		return ExitError
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	name, err := repo.CreateTaskBranch(ctx, *task)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl vcs branch: %v\n", err)
		return ExitError
	}
	fmt.Fprintf(env.Stdout, "switched to new branch %s\n", name)
	return ExitOK
}
