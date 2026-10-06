package cli

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/winnstorm/gabitotl/internal/coordination"
)

func runWorktree(env Env, args []string) int {
	return subcommands(env, "worktree", args, map[string]func(Env, []string) int{
		"add":    runWorktreeAdd,
		"list":   runWorktreeList,
		"remove": runWorktreeRemove,
	})
}

func worktrees(env Env, root string) (*coordination.Worktrees, string, error) {
	dir, err := resolveRoot(env, root)
	if err != nil {
		return nil, "", err
	}
	repo, err := openRepo(env, dir)
	if err != nil {
		return nil, "", err
	}
	return coordination.NewWorktrees(repo), dir, nil
}

func runWorktreeAdd(env Env, args []string) int {
	fs := flag.NewFlagSet("worktree add", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	root := fs.String("root", "", "project root")
	user := fs.String("user", "", "developer id (default: active identity)")
	task := fs.String("task", "", "task id; names the worktree directory (default: the user id)")
	branch := fs.String("branch", "", "branch to check out or create (default: feature/<task>)")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	w, dir, err := worktrees(env, *root)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl worktree add: %v\n", err)
		return ExitError
	}
	u, err := userID(dir, *user)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl worktree add: %v\n", err)
		return ExitError
	}
	b := *branch
	if b == "" {
		if *task == "" {
			fmt.Fprintln(env.Stderr, "gabitotl worktree add: --branch or --task is required")
			return ExitUsage
		}
		if b, err = w.Repo.TaskBranchName(*task); err != nil {
			fmt.Fprintf(env.Stderr, "gabitotl worktree add: %v\n", err)
			return ExitError
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	wt, err := w.Add(ctx, u, *task, b)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl worktree add: %v\n", err)
		return ExitError
	}
	fmt.Fprintf(env.Stdout, "worktree: %s\nbranch: %s\nnext: cd %s && gabitotl init (own .gabitotl/local identity)\n", wt.Path, wt.Branch, wt.Path)
	return ExitOK
}

func runWorktreeList(env Env, args []string) int {
	fs := flag.NewFlagSet("worktree list", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	root := fs.String("root", "", "project root")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	w, _, err := worktrees(env, *root)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl worktree list: %v\n", err)
		return ExitError
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	list, err := w.List(ctx)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl worktree list: %v\n", err)
		return ExitError
	}
	for _, wt := range list {
		kind := "linked"
		if wt.Main {
			kind = "main"
		}
		fmt.Fprintf(env.Stdout, "%-6s %-24s %s\n", kind, dash(wt.Branch), wt.Path)
	}
	return ExitOK
}

func runWorktreeRemove(env Env, args []string) int {
	fs := flag.NewFlagSet("worktree remove", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	root := fs.String("root", "", "project root")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(env.Stderr, "Usage: gabitotl worktree remove [--root DIR] <name|path>")
		return ExitUsage
	}
	w, _, err := worktrees(env, *root)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl worktree remove: %v\n", err)
		return ExitError
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := w.Remove(ctx, fs.Arg(0)); err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl worktree remove: %v\n", err)
		return ExitError
	}
	fmt.Fprintf(env.Stdout, "removed worktree %s\n", fs.Arg(0))
	return ExitOK
}
