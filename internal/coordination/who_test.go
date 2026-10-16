package coordination

import (
	"testing"
	"time"
)

func TestMergePresenceRules(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	rows := []Presence{
		{User: "bob", Scope: ScopeRemote, Source: SourceMemory, Task: "pay", Areas: []string{"src/pay"}, LastSeen: t0.Add(time.Hour)},
		{User: "bob", Scope: ScopeDirectory, Source: SourceSession, Task: "pay", Started: t0, LastSeen: t0},
		{User: "bob", Scope: ScopeRemote, Source: SourceWork, Task: "ui", Branch: "feature/ui"},
		// A shell with a branch and no task joins the task on the same branch.
		{User: "bob", Scope: ScopeDirectory, Source: SourceSession, Branch: "feature/ui", Started: t0.Add(-time.Hour)},
		// Bare shells in the same place are one row; another worktree is another.
		{User: "ana", Scope: ScopeDirectory, Source: SourceSession},
		{User: "ana", Scope: ScopeDirectory, Source: SourceSession},
		{User: "ana", Scope: ScopeWorktree, Source: SourceSession, Worktree: "/w"},
		// Different tasks of the same person stay apart.
		{User: "ana", Scope: ScopeRemote, Source: SourceWork, Task: "a"},
		{User: "ana", Scope: ScopeRemote, Source: SourceWork, Task: "b"},
	}
	got := mergePresence(rows)
	if len(got) != 6 {
		t.Fatalf("rows = %+v", got)
	}
	// Sorted by user, then scope, then task.
	if got[0].User != "ana" || got[0].Scope != ScopeDirectory || got[1].Scope != ScopeWorktree || got[2].Task != "a" || got[3].Task != "b" {
		t.Fatalf("order: %+v", got)
	}
	var pay, ui Presence
	for _, p := range got {
		switch {
		case p.User == "bob" && p.Task == "pay":
			pay = p
		case p.User == "bob" && p.Task == "ui":
			ui = p
		}
	}
	if pay.Source != SourceSession || pay.Scope != ScopeDirectory || len(pay.Areas) != 1 || !pay.LastSeen.Equal(t0.Add(time.Hour)) {
		t.Fatalf("most specific source wins and is completed by the others: %+v", pay)
	}
	if ui.Scope != ScopeDirectory || ui.Source != SourceSession || ui.Task != "ui" || !ui.Started.Equal(t0.Add(-time.Hour)) {
		t.Fatalf("a taskless session on the same branch joins the task: %+v", ui)
	}
}
