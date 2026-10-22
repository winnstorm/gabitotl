package vcs

import (
	"errors"
	"fmt"
)

// Typed errors callers can match with errors.Is.
var (
	// ErrNotRepo means the directory is not inside a Git work tree.
	ErrNotRepo = errors.New("vcs: not a git repository")
	// ErrDenied means vcs.md denies the operation.
	ErrDenied = errors.New("vcs: operation denied by .gabitotl/vcs.md")
	// ErrNeedsConfirmation means vcs.md says "ask" and no confirmation was given.
	ErrNeedsConfirmation = errors.New("vcs: operation requires confirmation (permission is \"ask\")")
	// ErrHistoryRewrite is returned for any attempt to rewrite history:
	// force-push, reset --hard, rebase, amend, filter-branch.
	ErrHistoryRewrite = errors.New("vcs: rewriting history is never allowed; use a revert commit")
	// ErrInvalidCommitMessage means the message is not a Conventional Commit.
	ErrInvalidCommitMessage = errors.New("vcs: commit message must follow Conventional Commits (type(scope): subject)")
	// ErrAttribution means a message carries AI or co-author attribution.
	ErrAttribution = errors.New("vcs: attribution trailers (Co-Authored-By, generated-with) are not allowed")
	// ErrNothingToCommit means nothing is staged.
	ErrNothingToCommit = errors.New("vcs: nothing to commit")
	// ErrBranchExists means the task branch already exists.
	ErrBranchExists = errors.New("vcs: branch already exists")
	// ErrNotImplemented marks provider operations that are documented stubs.
	ErrNotImplemented = errors.New("vcs: not implemented")
	// ErrUnsupported means the provider, host or operation is not supported.
	ErrUnsupported = errors.New("vcs: unsupported")
	// ErrNotLoggedIn means no usable credential exists for the platform.
	ErrNotLoggedIn = errors.New("vcs: not logged in to the platform")
	// ErrCertificate means the server's TLS certificate is not trusted or not usable.
	ErrCertificate = errors.New("vcs: certificate problem")
	// ErrNoPermission means the server knows the token but refuses the operation (HTTP 403).
	ErrNoPermission = errors.New("vcs: the token lacks permission")
	// ErrTimeout means the server did not answer in time.
	ErrTimeout = errors.New("vcs: the server did not answer in time")
	// ErrNoRemote means the operation needs an origin remote.
	ErrNoRemote = errors.New("vcs: no origin remote configured")
)

// PermissionError carries which operation was gated.
type PermissionError struct {
	Op  string
	Err error // ErrDenied or ErrNeedsConfirmation
}

func (e *PermissionError) Error() string { return fmt.Sprintf("%s: %v", e.Op, e.Err) }
func (e *PermissionError) Unwrap() error { return e.Err }

// kindErr is an error that keeps its own (secret-free) message and also matches a
// sentinel, so callers branch with errors.Is instead of reading English text.
type kindErr struct {
	kind error
	err  error
}

func (e *kindErr) Error() string   { return e.err.Error() }
func (e *kindErr) Unwrap() []error { return []error{e.kind, e.err} }

func tagged(kind, err error) error { return &kindErr{kind: kind, err: err} }
