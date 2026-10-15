package vcs

import (
	"regexp"
	"strings"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

// Operation names used for permission gating and error messages.
const (
	OpBranch      = "branches"
	OpCommit      = "commits"
	OpPush        = "push"
	OpPullRequest = "pull_requests"
	OpMerge       = "merge"
)

// Confirm is asked when a permission is "ask". It returns true to proceed.
// A nil Confirm means "ask" never proceeds (ErrNeedsConfirmation).
type Confirm func(op, detail string) bool

// permission returns the configured permission for op.
func permission(p gabitotl.VCSPermissions, op string) string {
	switch op {
	case OpBranch:
		return p.Branches
	case OpCommit:
		return p.Commits
	case OpPush:
		return p.Push
	case OpPullRequest:
		return p.PullRequests
	case OpMerge:
		return p.MergePermission()
	}
	return gabitotl.PermDeny
}

// Gate applies a VCS permission: allow proceeds, deny fails, ask consults confirm.
func Gate(p gabitotl.VCSPermissions, op, detail string, confirm Confirm) error {
	switch permission(p, op) {
	case gabitotl.PermAllow:
		return nil
	case gabitotl.PermAsk:
		if confirm != nil && confirm(op, detail) {
			return nil
		}
		return &PermissionError{Op: op, Err: ErrNeedsConfirmation}
	default:
		return &PermissionError{Op: op, Err: ErrDenied}
	}
}

// CheckHistorySafe rejects Git invocations that rewrite history. It is applied
// to every command the Repo runs, so no code path can force-push, reset --hard,
// amend, rebase or filter history.
func CheckHistorySafe(args []string) error {
	// Skip leading global options such as -C <dir> or -c k=v.
	i := 0
	for i < len(args) {
		switch {
		case args[i] == "-C" || args[i] == "-c":
			i += 2
		case strings.HasPrefix(args[i], "-"):
			i++
		default:
			goto found
		}
	}
	return nil
found:
	if i >= len(args) {
		return nil
	}
	sub, rest := args[i], args[i+1:]
	has := func(flags ...string) bool {
		for _, a := range rest {
			for _, f := range flags {
				if a == f || strings.HasPrefix(a, f+"=") {
					return true
				}
			}
		}
		return false
	}
	switch sub {
	case "push":
		if has("-f", "--force", "--force-with-lease", "--force-if-includes", "--mirror", "--delete", "-d") {
			return ErrHistoryRewrite
		}
		for _, a := range rest {
			if strings.HasPrefix(a, "+") || strings.HasPrefix(a, ":") {
				return ErrHistoryRewrite // +refspec forces; :ref deletes
			}
		}
	case "reset":
		if has("--hard", "--merge", "--keep") {
			return ErrHistoryRewrite
		}
	case "commit":
		if has("--amend") {
			return ErrHistoryRewrite
		}
	case "rebase", "filter-branch", "filter-repo", "replace":
		return ErrHistoryRewrite
	case "update-ref":
		return ErrHistoryRewrite
	}
	return nil
}

var conventional = regexp.MustCompile(`^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)(\([a-z0-9._/-]+\))?!?: \S.*`)

var attribution = regexp.MustCompile(`(?i)co-authored-by:|generated\swith|generated\sby\s(claude|ai)|noreply@anthropic`)

// ValidateCommitMessage requires a Conventional Commit subject and rejects any
// attribution trailer. GabitoTL never adds trailers of its own.
func ValidateCommitMessage(msg string) error {
	msg = strings.TrimSpace(msg)
	subject, _, _ := strings.Cut(msg, "\n")
	if !conventional.MatchString(subject) {
		return ErrInvalidCommitMessage
	}
	if attribution.MatchString(msg) {
		return ErrAttribution
	}
	return nil
}

// ValidateNoAttribution checks free text (for example a PR body).
func ValidateNoAttribution(text string) error {
	if attribution.MatchString(text) {
		return ErrAttribution
	}
	return nil
}
