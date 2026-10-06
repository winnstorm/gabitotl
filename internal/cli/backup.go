package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/winnstorm/gabitotl/internal/backup"
)

func runBackup(env Env, args []string) int {
	return subcommands(env, "backup", args, map[string]func(Env, []string) int{
		"create":  runBackupCreate,
		"list":    runBackupList,
		"restore": runBackupRestore,
		"prune":   runBackupPrune,
	})
}

// backupSetup resolves root, user and the manager shared by backup subcommands.
func backupSetup(env Env, root, user *string, name string) (*backup.Manager, string, string, bool) {
	dir, err := resolveRoot(env, *root)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl %s: %v\n", name, err)
		return nil, "", "", false
	}
	u, err := userID(dir, *user)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl %s: %v\n", name, err)
		return nil, "", "", false
	}
	wf, err := workflowConfig(layoutOf(dir))
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl %s: %v\n", name, err)
		return nil, "", "", false
	}
	return backup.New(dir, wf.Backup), dir, u, true
}

func runBackupCreate(env Env, args []string) int {
	fs := flag.NewFlagSet("backup create", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	root := fs.String("root", "", "project root")
	user := fs.String("user", "", "developer id (default: active identity)")
	task := fs.String("task", "", "task id (required)")
	reason := fs.String("reason", "", "why these files will change")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	if *task == "" || fs.NArg() == 0 {
		fmt.Fprintln(env.Stderr, "Usage: gabitotl backup create --task ID [--user ID] [--reason TEXT] [--root DIR] <file>...")
		return ExitUsage
	}
	m, dir, u, ok := backupSetup(env, root, user, "backup create")
	if !ok {
		return ExitError
	}
	meta := backup.Meta{User: u, Task: *task, Reason: *reason}
	if repo, err := openRepo(env, dir); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if st, err := repo.Status(ctx); err == nil && st.IsRepo {
			meta.Branch, meta.BaseCommit = st.Branch, st.Head
		}
	}
	res, err := m.Create(meta, fs.Args())
	for _, s := range res.Skipped {
		fmt.Fprintf(env.Stderr, "skipped %s: %s\n", s.Path, s.Reason)
	}
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl backup create: %v\n", err)
		return ExitError
	}
	fmt.Fprintf(env.Stdout, "snapshot: %s/%s\nfiles: %s\n", u, res.Snapshot.ID, strings.Join(res.Snapshot.Manifest.Files, ", "))
	return ExitOK
}

func runBackupList(env Env, args []string) int {
	fs := flag.NewFlagSet("backup list", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	root := fs.String("root", "", "project root")
	user := fs.String("user", "", "developer id (default: active identity)")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	m, _, u, ok := backupSetup(env, root, user, "backup list")
	if !ok {
		return ExitError
	}
	snaps, err := m.List(u)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl backup list: %v\n", err)
		return ExitError
	}
	if len(snaps) == 0 {
		fmt.Fprintf(env.Stdout, "no snapshots for %s\n", u)
		return ExitOK
	}
	for _, s := range snaps {
		fmt.Fprintf(env.Stdout, "%s  %d file(s)  %s\n", s.ID, len(s.Manifest.Files), s.Manifest.Reason)
	}
	return ExitOK
}

func runBackupRestore(env Env, args []string) int {
	fs := flag.NewFlagSet("backup restore", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	root := fs.String("root", "", "project root")
	user := fs.String("user", "", "developer id (default: active identity)")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	if fs.NArg() == 0 {
		fmt.Fprintln(env.Stderr, "Usage: gabitotl backup restore [--user ID] [--root DIR] <snapshot-id|latest> [file...]")
		return ExitUsage
	}
	m, _, u, ok := backupSetup(env, root, user, "backup restore")
	if !ok {
		return ExitError
	}
	snap, err := m.Find(u, fs.Arg(0))
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl backup restore: %v\n", err)
		return ExitError
	}
	restored, err := m.Restore(snap, fs.Args()[1:]...)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl backup restore: %v\n", err)
		if errors.Is(err, backup.ErrOutsideRoot) || errors.Is(err, backup.ErrNotInSnapshot) {
			return ExitUsage
		}
		return ExitError
	}
	fmt.Fprintf(env.Stdout, "restored from %s: %s\n", snap.ID, strings.Join(restored, ", "))
	return ExitOK
}

func runBackupPrune(env Env, args []string) int {
	fs := flag.NewFlagSet("backup prune", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	root := fs.String("root", "", "project root")
	user := fs.String("user", "", "developer id (default: active identity)")
	if err := fs.Parse(args); err != nil {
		return flagExit(err)
	}
	m, _, u, ok := backupSetup(env, root, user, "backup prune")
	if !ok {
		return ExitError
	}
	removed, err := m.Prune(u)
	if err != nil {
		fmt.Fprintf(env.Stderr, "gabitotl backup prune: %v\n", err)
		return ExitError
	}
	fmt.Fprintf(env.Stdout, "pruned %d snapshot(s) (keep newest %d or last %d days)\n", len(removed), m.Policy.MaxSnapshots, m.Policy.MaxAgeDays)
	return ExitOK
}
