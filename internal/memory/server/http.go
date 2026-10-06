package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// HTTPConfig configures ListenAndServe.
type HTTPConfig struct {
	// Addr is host:port. Loopback addresses are the safe default.
	Addr string
	// TLSCert and TLSKey are PEM files; when both are set the server speaks TLS.
	TLSCert, TLSKey string
	// AllowInsecureRemote permits plain HTTP on a non-loopback address. Only
	// for deployments where a TLS-terminating proxy sits in front.
	AllowInsecureRemote bool
}

// EndpointPath is where the MCP endpoint lives.
const EndpointPath = "/mcp"

type ctxKey struct{}

// reqInfo carries per-request facts from inner middleware to the access log.
type reqInfo struct{ user string }

// Handler returns the HTTP handler: /healthz (public, shallow) and /mcp
// (bearer token, rate limited, size limited).
func (s *Server) Handler() (http.Handler, error) {
	if s.cfg.FixedActor != "" {
		return nil, errors.New("server: a fixed actor is for stdio only; HTTP derives the actor from the token")
	}
	mcpH := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s.mcp }, &mcp.StreamableHTTPOptions{
		// Every tool is a plain request/response, so no sessions and no
		// server-sent streams: simpler to scale and to put behind a proxy.
		Stateless:           true,
		JSONResponse:        true,
		MaxRequestBodyBytes: s.cfg.MaxBodyBytes,
		Logger:              s.log,
	})
	bearer := auth.RequireBearerToken(s.verify, &auth.RequireBearerTokenOptions{AllowMissingExpiration: true})
	protected := s.accessLog(s.authGate(bearer(s.userLimit(mcpH))))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.Handle(EndpointPath, protected)
	return s.recoverer(secureHeaders(mux)), nil
}

func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				s.log.Error("panic in handler", "panic", fmt.Sprint(v))
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// statusWriter records the response status.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// Flush keeps streaming responses working through the wrapper.
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func peerIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// clientIP is the address the throttle and the log attribute a request to: the
// TCP peer, or, when a trusted proxy header is configured and carries a valid
// address, the last entry of that header.
func (s *Server) clientIP(r *http.Request) string {
	if h := s.cfg.TrustedProxyHeader; h != "" {
		if v := r.Header.Get(h); v != "" {
			parts := strings.Split(v, ",")
			if ip := net.ParseIP(strings.TrimSpace(parts[len(parts)-1])); ip != nil {
				return ip.String()
			}
		}
	}
	return peerIP(r)
}

// accessLog logs method, path, status, duration, client address and user. No
// headers, no bodies, no query strings.
func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info := &reqInfo{}
		sw := &statusWriter{ResponseWriter: w}
		start := time.Now()
		next.ServeHTTP(sw, r.WithContext(context.WithValue(r.Context(), ctxKey{}, info)))
		s.log.Info("http request", "method", r.Method, "path", r.URL.Path, "status", sw.status,
			"duration_ms", time.Since(start).Milliseconds(), "remote", s.clientIP(r), "user", info.user)
	})
}

// authGate throttles clients that keep failing to authenticate: every 401 costs
// the client address a token, and an exhausted address gets 429 before any
// token is even looked at.
func (s *Server) authGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := s.clientIP(r)
		if !s.fails.peek(ip) {
			tooMany(w, time.Minute)
			return
		}
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		if sw.status == http.StatusUnauthorized {
			s.fails.allow(ip)
		}
	})
}

func tooMany(w http.ResponseWriter, retry time.Duration) {
	secs := int(retry.Seconds()) + 1
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
}

// verify is the bearer verifier. Failures are indistinguishable to the client.
func (s *Server) verify(ctx context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
	rec, err := VerifyTokenRecord(ctx, s.cfg.Store, token)
	switch {
	case errors.Is(err, ErrBadToken):
		return nil, auth.ErrInvalidToken
	case err != nil:
		s.log.Error("token store error", "error", err.Error())
		return nil, errors.New("authentication unavailable")
	}
	return &auth.TokenInfo{UserID: rec.User, Extra: map[string]any{extraScope: scopeOf(rec)}}, nil
}

// userLimit applies the per-user rate limit after authentication.
func (s *Server) userLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ti := auth.TokenInfoFromContext(r.Context())
		if ti == nil {
			http.Error(w, "unauthenticated", http.StatusUnauthorized)
			return
		}
		if info, ok := r.Context().Value(ctxKey{}).(*reqInfo); ok {
			info.user = ti.UserID
		}
		if ok, retry := s.users.allow(ti.UserID); !ok {
			tooMany(w, retry)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

// CheckBind refuses plain HTTP on a non-loopback address unless explicitly allowed.
func (c HTTPConfig) CheckBind() error {
	host, _, err := net.SplitHostPort(c.Addr)
	if err != nil {
		return fmt.Errorf("server: invalid address %q: %w", c.Addr, err)
	}
	if (c.TLSCert == "") != (c.TLSKey == "") {
		return errors.New("server: --tls-cert and --tls-key go together")
	}
	if c.TLSCert == "" && !isLoopbackHost(host) && !c.AllowInsecureRemote {
		return fmt.Errorf("server: refusing plain HTTP on %q: bearer tokens would cross the network unencrypted. "+
			"Use --tls-cert/--tls-key, put the server behind a TLS proxy and pass --allow-insecure-remote, or bind to 127.0.0.1", c.Addr)
	}
	return nil
}

// ListenAndServe serves until ctx is cancelled, then shuts down gracefully.
func (s *Server) ListenAndServe(ctx context.Context, hc HTTPConfig) error {
	if err := hc.CheckBind(); err != nil {
		return err
	}
	ln, err := net.Listen("tcp", hc.Addr)
	if err != nil {
		return err
	}
	return s.Serve(ctx, ln, hc)
}

// Serve serves on ln until ctx is cancelled.
func (s *Server) Serve(ctx context.Context, ln net.Listener, hc HTTPConfig) error {
	h, err := s.Handler()
	if err != nil {
		_ = ln.Close()
		return err
	}
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    16 << 10,
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12},
	}
	done := make(chan error, 1)
	go func() {
		if hc.TLSCert != "" {
			done <- srv.ServeTLS(ln, hc.TLSCert, hc.TLSKey)
		} else {
			done <- srv.Serve(ln)
		}
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		err := srv.Shutdown(sctx)
		<-done
		return err
	}
}
