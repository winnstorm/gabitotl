package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/winnstorm/gabitotl/internal/procattr"
	"github.com/winnstorm/gabitotl/internal/version"
)

// Client-side errors. Callers branch on them with errors.Is; the message is
// already written for a person.
var (
	// ErrAuth means the server rejected the token, or the token reference does not resolve.
	ErrAuth = errors.New("authentication failed: the token was rejected or is not set")
	// ErrUnreachable means the server could not be reached (network, DNS, refused, timeout).
	ErrUnreachable = errors.New("the memory server is unreachable")
	// ErrSchema means the server does not speak the contract this client expects
	// (unknown tool, malformed result): usually a version mismatch.
	ErrSchema = errors.New("the memory server does not match this client's contract (version mismatch?)")
	// ErrRateLimited means the server answered 429.
	ErrRateLimited = errors.New("the memory server is rate limiting this client; retry in a moment")
	// ErrForbidden means the token is valid but not allowed to use that project.
	ErrForbidden = errors.New("this token is not allowed to use that project")
	// ErrInsecureURL means a plain http:// URL to a non-loopback host.
	ErrInsecureURL = errors.New("refusing plain http to a non-loopback server: the bearer token would cross the network unencrypted (use https, or tunnel to 127.0.0.1)")
)

// ToolError is a failure the server reported for a tool call (validation,
// not found, revision conflict, ...). It is not retried.
type ToolError struct{ Tool, Message string }

func (e *ToolError) Error() string { return e.Tool + ": " + e.Message }

// DefaultCallTimeout bounds one tool call, retries included per attempt.
const DefaultCallTimeout = 30 * time.Second

// idempotent tools may be retried after a transport failure; writes never are.
func idempotent(tool string) bool {
	switch tool {
	case "memory_search", "memory_current_project", "memory_doctor", "memory_active_work":
		return true
	}
	return false
}

// SDKTransport is the real Transport: it keeps one MCP client session (opened
// lazily, reopened after a transport failure) over a connector.
type SDKTransport struct {
	connect func(ctx context.Context) (*sdk.ClientSession, error)
	status  *atomic.Int32 // last HTTP status seen (HTTP only)
	timeout time.Duration
	retries int
	backoff time.Duration

	mu   sync.Mutex
	sess *sdk.ClientSession
}

var _ Transport = (*SDKTransport)(nil)

// HTTPOptions configure NewHTTPTransport.
type HTTPOptions struct {
	URL string
	// TokenRef is the env:NAME reference holding the bearer token. It is
	// resolved on every request, so a rotated token is picked up without a restart.
	TokenRef string
	Getenv   func(string) string
	// AllowInsecure permits http:// to a non-loopback host (only through a tunnel you trust).
	AllowInsecure bool
	Timeout       time.Duration
	// HTTPClient is optional (tests, custom TLS roots); its transport is wrapped.
	HTTPClient *http.Client
}

// CheckURL validates a server URL: http(s), a host, and no plain http to a remote host.
func CheckURL(raw string, allowInsecure bool) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("server url %q must look like https://host:port/mcp", raw)
	}
	if u.User != nil {
		return errors.New("server url must not carry credentials; use a token reference")
	}
	if u.Scheme == "http" && !allowInsecure && !isLoopback(u.Hostname()) {
		return ErrInsecureURL
	}
	return nil
}

func isLoopback(h string) bool {
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(h, "[]"))
	return ip != nil && ip.IsLoopback()
}

type bearerRT struct {
	base   http.RoundTripper
	ref    string
	getenv func(string) string
	status *atomic.Int32
}

func (b *bearerRT) RoundTrip(r *http.Request) (*http.Response, error) {
	tok, err := ResolveRef(b.ref, b.getenv)
	if err != nil {
		b.status.Store(http.StatusUnauthorized)
		return nil, fmt.Errorf("%w (%v)", ErrAuth, err)
	}
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+tok)
	b.status.Store(0)
	resp, err := b.base.RoundTrip(r)
	if resp != nil {
		b.status.Store(int32(resp.StatusCode))
	}
	return resp, err
}

// NewHTTPTransport builds a streamable-HTTP client transport with a bearer
// token read from an env:NAME reference at call time.
func NewHTTPTransport(o HTTPOptions) (*SDKTransport, error) {
	if err := CheckURL(o.URL, o.AllowInsecure); err != nil {
		return nil, err
	}
	if !envRefRe.MatchString(o.TokenRef) {
		return nil, errors.New("token must be an env:NAME reference, never the token itself")
	}
	status := &atomic.Int32{}
	base := http.DefaultTransport
	hc := &http.Client{}
	if o.HTTPClient != nil {
		hc = &http.Client{Transport: o.HTTPClient.Transport, Timeout: o.HTTPClient.Timeout}
		if hc.Transport != nil {
			base = hc.Transport
		}
	}
	hc.Transport = &bearerRT{base: base, ref: o.TokenRef, getenv: o.Getenv, status: status}
	tr := &sdk.StreamableClientTransport{Endpoint: o.URL, HTTPClient: hc, MaxRetries: -1, DisableStandaloneSSE: true}
	t := newSDK(func(ctx context.Context) (*sdk.ClientSession, error) {
		return sdk.NewClient(&sdk.Implementation{Name: "gabitotl", Version: version.Version}, nil).Connect(ctx, tr, nil)
	}, o.Timeout)
	t.status = status
	return t, nil
}

// StdioOptions configure NewStdioTransport.
type StdioOptions struct {
	Command string
	Args    []string
	Env     []string // full environment of the child (nil: inherit)
	Timeout time.Duration
}

// NewStdioTransport talks to a local `gabitotl memory serve --stdio` (or any
// MCP server) as a child process.
func NewStdioTransport(o StdioOptions) (*SDKTransport, error) {
	if strings.TrimSpace(o.Command) == "" {
		return nil, errors.New("stdio transport needs a command")
	}
	return newSDK(func(ctx context.Context) (*sdk.ClientSession, error) {
		cmd := procattr.NoWindow(exec.Command(o.Command, o.Args...))
		cmd.Env = o.Env
		c := sdk.NewClient(&sdk.Implementation{Name: "gabitotl", Version: version.Version}, nil)
		return c.Connect(ctx, &sdk.CommandTransport{Command: cmd}, nil)
	}, o.Timeout), nil
}

func newSDK(connect func(context.Context) (*sdk.ClientSession, error), timeout time.Duration) *SDKTransport {
	if timeout <= 0 {
		timeout = DefaultCallTimeout
	}
	return &SDKTransport{connect: connect, timeout: timeout, retries: 1, backoff: 200 * time.Millisecond}
}

// Close ends the session (and the child process for stdio).
func (t *SDKTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.sess == nil {
		return nil
	}
	err := t.sess.Close()
	t.sess = nil
	return err
}

func (t *SDKTransport) session(ctx context.Context) (*sdk.ClientSession, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.sess != nil {
		return t.sess, nil
	}
	s, err := t.connect(ctx)
	if err != nil {
		return nil, err
	}
	t.sess = s
	return s, nil
}

func (t *SDKTransport) drop(s *sdk.ClientSession) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.sess == s {
		t.sess = nil
		go func() { _ = s.Close() }()
	}
}

// Call implements Transport. Idempotent reads are retried once on a transport
// failure; writes never are.
func (t *SDKTransport) Call(ctx context.Context, tool string, args any) (json.RawMessage, error) {
	attempts := 1
	if idempotent(tool) {
		attempts += t.retries
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			select {
			case <-time.After(t.backoff):
			case <-ctx.Done():
				return nil, mapErr(ctx.Err(), 0)
			}
		}
		raw, retryable, err := t.callOnce(ctx, tool, args)
		if err == nil {
			return raw, nil
		}
		lastErr = err
		if !retryable {
			break
		}
	}
	return nil, lastErr
}

func (t *SDKTransport) callOnce(ctx context.Context, tool string, args any) (json.RawMessage, bool, error) {
	cctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	s, err := t.session(cctx)
	if err != nil {
		return nil, true, t.mapped(err)
	}
	res, err := s.CallTool(cctx, &sdk.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.drop(s)
		me := t.mapped(err)
		return nil, errors.Is(me, ErrUnreachable), me
	}
	if res.IsError {
		return nil, false, toolError(tool, res)
	}
	if res.StructuredContent != nil {
		raw, err := json.Marshal(res.StructuredContent)
		if err != nil {
			return nil, false, fmt.Errorf("%w: %v", ErrSchema, err)
		}
		return raw, false, nil
	}
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok && json.Valid([]byte(tc.Text)) {
			return json.RawMessage(tc.Text), false, nil
		}
	}
	return nil, false, fmt.Errorf("%w: the result carries no structured content", ErrSchema)
}

func (t *SDKTransport) mapped(err error) error {
	st := 0
	if t.status != nil {
		st = int(t.status.Load())
	}
	return mapErr(err, st)
}

func toolError(tool string, res *sdk.CallToolResult) error {
	var msg string
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			msg = tc.Text
			break
		}
	}
	if msg == "" {
		msg = "the server reported an error"
	}
	switch {
	case strings.Contains(msg, "not allowed to use project"):
		return fmt.Errorf("%w: %s", ErrForbidden, msg)
	case strings.Contains(msg, "unauthenticated"):
		return fmt.Errorf("%w: %s", ErrAuth, msg)
	}
	return &ToolError{Tool: tool, Message: msg}
}

// mapErr turns an SDK, network or HTTP failure into one of the package errors.
func mapErr(err error, httpStatus int) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrAuth), errors.Is(err, ErrRateLimited), errors.Is(err, ErrSchema):
		return err
	}
	switch httpStatus {
	case http.StatusUnauthorized:
		return fmt.Errorf("%w (HTTP 401)", ErrAuth)
	case http.StatusForbidden:
		return fmt.Errorf("%w (HTTP 403)", ErrAuth)
	case http.StatusTooManyRequests:
		return ErrRateLimited
	case http.StatusNotFound:
		return fmt.Errorf("%w: HTTP 404 (is the URL the /mcp endpoint?)", ErrSchema)
	}
	msg := err.Error()
	var ne net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled),
		errors.Is(err, syscall.ECONNREFUSED), errors.Is(err, syscall.ECONNRESET), errors.As(err, &ne),
		strings.Contains(msg, "connection refused"), strings.Contains(msg, "no such host"), strings.Contains(msg, "EOF"),
		strings.Contains(msg, "connection closed"), strings.Contains(msg, "broken pipe"):
		return fmt.Errorf("%w: %s", ErrUnreachable, sanitize(msg))
	case strings.Contains(msg, "unknown tool"), strings.Contains(msg, "Method not found"), strings.Contains(msg, "-32601"),
		strings.Contains(msg, "-32602"), strings.Contains(msg, "invalid params"), strings.Contains(msg, "unsupported protocol"):
		return fmt.Errorf("%w: %s", ErrSchema, sanitize(msg))
	}
	return fmt.Errorf("memory server error: %s", sanitize(msg))
}

// sanitize keeps messages to one short line; URLs may carry host names but never tokens.
func sanitize(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 300 {
		s = s[:300] + "..."
	}
	return s
}
