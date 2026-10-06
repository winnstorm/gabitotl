// Package server is the GabitoTL Memory MCP server. It exposes the
// memory.Provider surface as MCP tools over stdio or streamable HTTP, on top of
// a store.Store (PostgreSQL in production, memstore in tests).
//
// Identity rules (the reason this server exists):
//
//   - Over HTTP the actor of every write is the owner of the bearer token. A
//     request that names a different actor is rejected, never rewritten, so a
//     client cannot write as someone else. Tokens are created from the CLI,
//     stored hashed, and shown once.
//   - Over stdio there is one trusted local client; the actor is fixed when the
//     server starts (Config.FixedActor).
//   - "legacy" is never an authenticated actor; it exists only for imports,
//     which go through the store directly.
//
// Logs are structured and never contain entry content, tokens or connection
// strings.
package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/winnstorm/gabitotl/internal/memory"
	"github.com/winnstorm/gabitotl/internal/memory/store"
	"github.com/winnstorm/gabitotl/internal/version"
)

// Tool names. They are the wire contract with internal/memory/mcp.
const (
	ToolSearch         = "memory_search"
	ToolSave           = "memory_save"
	ToolUpdate         = "memory_update"
	ToolSessionSummary = "memory_session_summary"
	ToolCurrentProject = "memory_current_project"
	ToolDoctor         = "memory_doctor"
	ToolActiveWork     = "memory_active_work"
)

// Input and output limits, enforced before anything reaches the store.
const (
	MaxQueryBytes   = 500
	MaxProjectBytes = 120
	MaxTitleBytes   = 300
	MaxTopicBytes   = 200
	MaxContentBytes = 64 << 10
	MaxFieldBytes   = 8 << 10 // task, branch, commit, validation, next step, ojo, source
	MaxFiles        = 200
	MaxFileBytes    = 512
	MaxSearchLimit  = memory.DefaultTotal
)

// ErrActorMismatch is returned when a request names an actor other than the
// authenticated one.
var ErrActorMismatch = errors.New("actor mismatch: writes are always attributed to the authenticated user; omit the actor or use your own")

// Config configures a Server.
type Config struct {
	Store store.Store
	// FixedActor is the actor for stdio (no token). It must be empty for HTTP.
	FixedActor string
	// DefaultProject answers memory_current_project and fills a missing project.
	DefaultProject string
	Logger         *slog.Logger
	// MaxBodyBytes bounds an HTTP request body (default 1 MiB).
	MaxBodyBytes int64
	// CallTimeout bounds one tool call (default 20s).
	CallTimeout time.Duration
	// UserRate / UserBurst limit requests per authenticated user (default 20/s, burst 40).
	UserRate  float64
	UserBurst float64
	// AuthFailRate / AuthFailBurst limit failed authentications per client
	// address (default 10 per minute).
	AuthFailPerMinute int
	// TrustedProxyHeader, when set (for example "X-Forwarded-For"), makes the
	// per-address authentication throttle and the access log use the address in
	// that header instead of the TCP peer. Off by default: a client can forge the
	// header unless every request really passes through a proxy you control.
	// For a comma-separated list the LAST entry is used (the one the nearest
	// trusted proxy appended).
	TrustedProxyHeader string
	Now                func() time.Time
}

// Server is a Memory MCP server.
type Server struct {
	cfg   Config
	mcp   *mcp.Server
	log   *slog.Logger
	now   func() time.Time
	users *limiter
	fails *limiter
}

// New builds the server and registers the tools.
func New(cfg Config) (*Server, error) {
	if cfg.Store == nil {
		return nil, errors.New("server: a store is required")
	}
	if cfg.FixedActor != "" {
		if err := CheckUserID(cfg.FixedActor); err != nil {
			return nil, fmt.Errorf("server: actor: %w", err)
		}
	}
	if h := cfg.TrustedProxyHeader; h != "" && !validHeaderName(h) {
		return nil, fmt.Errorf("server: %q is not a valid header name", h)
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = 1 << 20
	}
	if cfg.CallTimeout <= 0 {
		cfg.CallTimeout = 20 * time.Second
	}
	if cfg.UserRate <= 0 {
		cfg.UserRate = 20
	}
	if cfg.UserBurst <= 0 {
		cfg.UserBurst = 40
	}
	if cfg.AuthFailPerMinute <= 0 {
		cfg.AuthFailPerMinute = 10
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	s := &Server{cfg: cfg, log: cfg.Logger, now: cfg.Now}
	s.users = newLimiter(cfg.UserRate, cfg.UserBurst, cfg.Now)
	s.fails = newLimiter(float64(cfg.AuthFailPerMinute)/60, float64(cfg.AuthFailPerMinute), cfg.Now)
	s.mcp = mcp.NewServer(&mcp.Implementation{Name: "gabitotl-memory", Title: "GabitoTL Memory", Version: version.Version},
		&mcp.ServerOptions{Instructions: instructions})
	s.mcp.AddReceivingMiddleware(s.logCalls)
	s.register()
	return s, nil
}

const instructions = "GabitoTL team memory. Search before working (memory_search, always scoped to a project), save decisions, " +
	"bugfixes, conventions and progress with memory_save, and close a session with memory_session_summary. " +
	"Your actor is your authenticated user: never send another one."

// MCP exposes the underlying MCP server (tests connect to it in memory).
func (s *Server) MCP() *mcp.Server { return s.mcp }

// logCalls logs every tool call: name, actor, duration and outcome, never arguments.
func (s *Server) logCalls(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		if method != "tools/call" {
			return next(ctx, method, req)
		}
		start := time.Now()
		res, err := next(ctx, method, req)
		attrs := []any{"tool", toolName(req), "duration_ms", time.Since(start).Milliseconds()}
		if ctr, ok := req.(*mcp.CallToolRequest); ok {
			attrs = append(attrs, "actor", s.actorOf(ctr))
		}
		switch r := res.(type) {
		case *mcp.CallToolResult:
			attrs = append(attrs, "is_error", r != nil && r.IsError)
		}
		if err != nil {
			attrs = append(attrs, "error", err.Error())
		}
		s.log.Info("tool call", attrs...)
		return res, err
	}
}

func toolName(req mcp.Request) string {
	if r, ok := req.(*mcp.CallToolRequest); ok && r.Params != nil {
		return r.Params.Name
	}
	return ""
}

// actorOf returns the authenticated actor of a request ("" when none).
func (s *Server) actorOf(req *mcp.CallToolRequest) string {
	if req != nil && req.Extra != nil && req.Extra.TokenInfo != nil && req.Extra.TokenInfo.UserID != "" {
		return req.Extra.TokenInfo.UserID
	}
	return s.cfg.FixedActor
}

// resolveActor enforces the identity rules for one call.
func (s *Server) resolveActor(req *mcp.CallToolRequest, claimed string) (string, error) {
	actor := s.actorOf(req)
	if actor == "" {
		return "", errors.New("unauthenticated: no actor for this request")
	}
	if claimed = strings.TrimSpace(claimed); claimed != "" && claimed != actor {
		s.log.Warn("actor spoof rejected", "actor", actor, "claimed", claimed, "tool", toolName(req))
		return "", ErrActorMismatch
	}
	return actor, nil
}

// extraScope is the key under which the verified token scope travels in TokenInfo.Extra.
const extraScope = "gabitotl.scope"

// scope is what a token may reach. The zero value (stdio, no token) is "all",
// because stdio serves one trusted local client.
type scope struct {
	all      bool
	projects map[string]bool
}

func scopeOf(r store.TokenRecord) scope {
	sc := scope{all: r.AllProjects, projects: map[string]bool{}}
	for _, p := range r.Projects {
		sc.projects[p] = true
	}
	return sc
}

// ErrOutOfScope is returned when a token is used on a project outside its allowlist.
type ErrOutOfScope struct{ Project string }

func (e ErrOutOfScope) Error() string {
	return fmt.Sprintf("this token is not allowed to use project %q", e.Project)
}

// scopeFor returns the scope of a request. A request that carries a token but
// no readable scope is treated as having no access at all (fail closed).
func scopeFor(req *mcp.CallToolRequest) scope {
	if req == nil || req.Extra == nil || req.Extra.TokenInfo == nil {
		return scope{all: true} // stdio: no token, one trusted client
	}
	if sc, ok := req.Extra.TokenInfo.Extra[extraScope].(scope); ok {
		return sc
	}
	return scope{}
}

// allow reports whether the request may use project.
func (s *Server) allow(req *mcp.CallToolRequest, project string) error {
	sc := scopeFor(req)
	if sc.all || sc.projects[project] {
		return nil
	}
	s.log.Warn("project out of scope", "actor", s.actorOf(req), "project", project, "tool", toolName(req))
	return ErrOutOfScope{Project: project}
}

func (s *Server) project(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		p = s.cfg.DefaultProject
	}
	if p == "" {
		return "", errors.New("project is required")
	}
	if len(p) > MaxProjectBytes {
		return "", fmt.Errorf("project is longer than %d bytes", MaxProjectBytes)
	}
	return p, nil
}

// callCtx bounds a tool call.
func (s *Server) callCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, s.cfg.CallTimeout)
}

// toolErr converts a store error into a message that is safe to show a client.
// Anything unexpected is logged here and reported generically.
func (s *Server) toolErr(op string, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, store.ErrNotFound):
		return errors.New("not found")
	case errors.Is(err, store.ErrConflict):
		return errors.New("revision conflict: the entry changed since you read it; fetch it again and retry")
	case errors.Is(err, store.ErrInvalid):
		return err
	case errors.Is(err, context.DeadlineExceeded):
		s.log.Error("tool timed out", "op", op)
		return errors.New("the memory store did not answer in time; try again")
	}
	s.log.Error("store error", "op", op, "error", err.Error())
	return errors.New("internal error (details are in the server log)")
}

// Doctor reports the health of the server and its store.
func (s *Server) Doctor(ctx context.Context) memory.Report {
	rep := s.cfg.Store.Doctor(ctx)
	rep.Checks = append(rep.Checks, memory.Check{Name: "server", Status: memory.StatusOK, Message: "gabitotl-memory " + version.Version})
	return rep
}

// ServeStdio serves one trusted local client over stdin/stdout. It needs a fixed actor.
func (s *Server) ServeStdio(ctx context.Context) error {
	if s.cfg.FixedActor == "" {
		return errors.New("server: stdio needs an actor (--actor or an active identity)")
	}
	return s.mcp.Run(ctx, &mcp.StdioTransport{})
}

func validHeaderName(h string) bool {
	for _, r := range h {
		if !(r == '-' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return h != ""
}
