package vcs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"

	"github.com/winnstorm/gabitotl/internal/vcshosts"
)

// MergeOptions say how a pull request is merged. Method is one of
// gabitotl.MergeMethods (squash, merge, rebase).
type MergeOptions struct {
	Method       string
	DeleteBranch bool
}

// Check states ChecksState reports.
const (
	ChecksPassing = "passing"
	ChecksFailing = "failing"
	ChecksPending = "pending"
	ChecksNone    = "none"
)

// Checks is the combined state of the CI checks of a pull request.
type Checks struct {
	State  string // ChecksPassing, ChecksFailing, ChecksPending or ChecksNone
	Detail string // short, secret-free note (for example the failing check names)
}

// Auth states AuthStatus reports.
const (
	AuthLoggedIn    = "logged-in"
	AuthNotLoggedIn = "not-logged-in"
	AuthToolMissing = "tool-missing"
	AuthUnsupported = "unsupported"
)

// AuthState says whether GabitoTL can act on a platform as the person.
type AuthState struct {
	Provider string
	Status   string // one of the Auth* constants
	// Fix is a human-readable instruction that contains the exact command.
	Fix string
	// Command is the exact command alone ("" when none applies) and InstallURL
	// where to get the missing tool; presenters word their own messages with them.
	Command    string
	InstallURL string
	// Detail is a short secret-free note about what was observed.
	Detail string
	// User is the authenticated account name when the platform tells (secret-free).
	User string
}

// OK reports whether the person is logged in.
func (a AuthState) OK() bool { return a.Status == AuthLoggedIn }

// Host is a code-hosting platform GabitoTL can drive: it opens, merges and
// retargets pull requests (merge requests on GitLab), reads their checks and
// tells whether the person is logged in. It extends PRProvider.
type Host interface {
	PRProvider
	// MergePR merges pr. Callers go through Repo.MergePR, which gates it.
	MergePR(ctx context.Context, dir string, pr PR, opts MergeOptions) error
	// SetBase retargets pr to newBase (a stacked PR whose parent was merged).
	SetBase(ctx context.Context, dir string, pr PR, newBase string) error
	// ChecksState combines the CI checks of pr.
	ChecksState(ctx context.Context, dir string, pr PR) (Checks, error)
	// AuthStatus reports the login state without changing anything.
	AuthStatus(ctx context.Context, dir string) (AuthState, error)
}

// HostOptions are the injectable pieces behind the hosts.
type HostOptions struct {
	// Runner runs gh, glab and git; nil means ExecRunner.
	Runner Runner
	// HTTP and BitbucketAPI serve the Bitbucket Cloud REST API; nil and "" mean
	// a client with a timeout and https://api.bitbucket.org.
	HTTP         *http.Client
	BitbucketAPI string
	// Credentials returns the Bitbucket credential; nil means not logged in.
	Credentials func() (Credential, error)
	// Tokens returns the stored token of a host, handed to gh and glab in their own
	// child environment when the person has not set one; nil uses DefaultTokens.
	Tokens func(host string) (string, bool)
	// Servers is the registry of self-hosted servers and ServerToken returns the
	// token of one (Bitbucket Data Center); nil Servers means none are registered.
	Servers     vcshosts.Registry
	ServerToken func(host string) (string, error)
}

// HostFor returns the host of a vcs.md provider value: github, gitlab,
// bitbucket (Bitbucket Cloud) or bitbucket-server (Data Center). Anything else fails with ErrUnsupported.
func HostFor(name string, o HostOptions) (Host, error) {
	tokens := o.Tokens
	if tokens == nil {
		tokens = DefaultTokens
	}
	servers := o.Servers
	if servers == nil {
		servers = defaultServers()
	}
	serverToken := o.ServerToken
	if serverToken == nil {
		serverToken = func(host string) (string, error) {
			if tokens == nil {
				return "", nil
			}
			t, _ := tokens(host)
			return t, nil
		}
	}
	switch name {
	case "github":
		return GitHub{Runner: withStoredTokens(o.Runner, tokens)}, nil
	case "gitlab":
		return GitLab{Runner: withStoredTokens(o.Runner, tokens)}, nil
	case "bitbucket":
		return Bitbucket{Runner: o.Runner, HTTP: o.HTTP, BaseURL: o.BitbucketAPI, Creds: o.Credentials}, nil
	case "bitbucket-server":
		return BitbucketServer{Runner: o.Runner, HTTP: o.HTTP, Servers: servers, Token: serverToken}, nil
	}
	return nil, fmt.Errorf("%w: no pull request support for provider %q (supported: github, gitlab, bitbucket, bitbucket-server)", ErrUnsupported, name)
}

// TTYRunner runs an interactive command attached to the terminal (stdin, stdout
// and stderr pass through), for example `gh auth login`.
type TTYRunner interface {
	RunTTY(ctx context.Context, dir, name string, args ...string) error
}

// ExecTTY runs real commands attached to the process terminal.
type ExecTTY struct{}

// RunTTY implements TTYRunner.
func (ExecTTY) RunTTY(ctx context.Context, dir, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func runnerOr(r Runner) Runner {
	if r == nil {
		return ExecRunner{}
	}
	return r
}

// toolMissing reports a command that is not installed.
func toolMissing(err error) bool { return errors.Is(err, exec.ErrNotFound) }

var trailingNumber = regexp.MustCompile(`/(\d+)/?$`)

// prNumber extracts the numeric id at the end of a PR URL (0 when absent).
func prNumber(url string) int {
	if m := trailingNumber.FindStringSubmatch(url); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

// newPR builds a PR from its URL, deriving the id from it.
func newPR(url string) PR {
	pr := PR{URL: url}
	if n := prNumber(url); n > 0 {
		pr.Number, pr.ID = n, strconv.Itoa(n)
	}
	return pr
}

// prID returns the platform id of pr, from ID, Number or the URL.
func prID(pr PR) (string, error) {
	switch {
	case pr.ID != "":
		return pr.ID, nil
	case pr.Number > 0:
		return strconv.Itoa(pr.Number), nil
	}
	if n := prNumber(pr.URL); n > 0 {
		return strconv.Itoa(n), nil
	}
	return "", errors.New("vcs: the pull request has no id or URL")
}

// prRef is what gh accepts to name a PR: the URL, else the number.
func prRef(pr PR) (string, error) {
	if pr.URL != "" {
		return pr.URL, nil
	}
	return prID(pr)
}
