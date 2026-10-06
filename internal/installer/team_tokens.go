package installer

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/winnstorm/gabitotl/internal/memory/mcp"
	"github.com/winnstorm/gabitotl/internal/memory/server"
	"github.com/winnstorm/gabitotl/internal/memory/store"
)

// The host's own token is minted by the installer. Re-running the installer must
// not pile up active tokens, so it
//
//   - reuses the token already in the 0600 secrets file while the server still
//     accepts it for this user and project, and otherwise
//   - mints a new one and revokes the previous tokens THIS installer issued for
//     that user (tracked by id under the label installer:<user>; the ids are not
//     secrets, the plaintext is never recorded anywhere but the secrets file).
//
// Tokens a person created with `gabitotl memory token create` are never tracked,
// so they are never revoked here.

// tokenLedgerName is the file (in the GabitoTL state dir) that lists the ids of
// the tokens the installer issued. Ids only: no secret ever goes into it.
const tokenLedgerName = "memory-tokens.json"

type tokenLedger struct {
	Version int                 `json:"version"`
	Labels  map[string][]string `json:"labels"`
}

func installerLabel(instance, user string) string {
	if instance == "" {
		instance = "default"
	}
	return instance + "/installer:" + user
}

func tokenLedgerPath(env Env) string { return filepath.Join(env.StateDir(), tokenLedgerName) }

func loadTokenLedger(env Env) tokenLedger {
	l := tokenLedger{Version: 1, Labels: map[string][]string{}}
	if b, err := os.ReadFile(tokenLedgerPath(env)); err == nil {
		var in tokenLedger
		if json.Unmarshal(b, &in) == nil && in.Labels != nil {
			l.Labels = in.Labels
		}
	}
	return l
}

func (l tokenLedger) save(env Env) error {
	if err := os.MkdirAll(env.StateDir(), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(env.StateDir(), ".memory-tokens-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), tokenLedgerPath(env))
}

// storedToken is the token in the 0600 secrets file ("" when there is none). The
// process environment is deliberately not consulted: this is the installer's own.
func storedToken(env Env, tokenEnv string) string {
	return (mcp.Resolver{Getenv: func(string) string { return "" }, Home: env.Home}).Lookup(tokenEnv)
}

// tokenID extracts the public id of a token (gbt_<id>.<secret>), "" if malformed.
func tokenID(tok string) string {
	rest, ok := strings.CutPrefix(tok, server.TokenPrefix)
	if !ok {
		return ""
	}
	id, _, _ := strings.Cut(rest, ".")
	return id
}

// reusableToken reports whether the stored token still authenticates as user and
// is scoped to project alone; it returns its record when so.
func reusableToken(ctx context.Context, env Env, st store.TokenStore, tokenEnv, user, project string) (store.TokenRecord, bool) {
	tok := storedToken(env, tokenEnv)
	if tok == "" {
		return store.TokenRecord{}, false
	}
	rec, err := server.VerifyTokenRecord(ctx, st, tok)
	if err != nil || rec.User != user || rec.AllProjects || !rec.Allows(project) {
		return store.TokenRecord{}, false
	}
	return rec, true
}

// hostToken makes sure this machine's user has a valid token in the secrets file:
// the existing one when it still validates, otherwise a new one. Tokens to revoke
// are only remembered here; they are revoked once the run verified, so a
// rollback (which restores the old secrets file) never loses a working token.
func (r *run) hostToken(ctx context.Context, st store.Store, instance, user string, projects []string) error {
	if rec, ok := reusableToken(ctx, r.env, st, r.pl.Team.TokenEnv, user, projects[0]); ok {
		r.tokenID = rec.ID
		r.emit(PhaseServer, "ok", "your token", "reused: the one in the 0600 secrets file is still valid")
		return nil
	}
	old := tokenID(storedToken(r.env, r.pl.Team.TokenEnv))
	tok, err := r.env.Team.NewToken(ctx, st, user, projects)
	if err != nil {
		return err
	}
	if err := r.storeToken(tok); err != nil {
		return err
	}
	r.tokenID, r.tokenNew = tokenID(tok), true
	r.tokenStale = slices.Clone(loadTokenLedger(r.env).Labels[installerLabel(instance, user)])
	if old != "" && !slices.Contains(r.tokenStale, old) {
		// A token of this very user that was in the file but no longer validates
		// for this setup (wrong scope, say) is ours to retire as well; RevokeToken
		// is per user, so it can never touch somebody else's.
		r.tokenStale = append(r.tokenStale, old)
	}
	r.emit(PhaseServer, "ok", "your token", "stored in the 0600 secrets file")
	return nil
}

// commitTokens revokes what the new token replaced and records the tokens now
// issued by the installer. Failures warn: they never fail a verified install.
func (r *run) commitTokens(ctx context.Context) {
	t := r.pl.Team
	if t == nil || t.Server == nil || r.tokenID == "" {
		return
	}
	user, label := r.pl.Options.User, installerLabel(t.Server.Instance, r.pl.Options.User)
	remaining := []string{}
	if len(r.tokenStale) > 0 {
		st, closeFn, err := r.env.Team.OpenStore(ctx, t.Server.Instance)
		if err != nil {
			r.warn("the previous token of %s could not be revoked (database unreachable): %v", user, err)
			remaining = append(remaining, r.tokenStale...)
		} else {
			defer closeFn()
			for _, id := range r.tokenStale {
				if err := st.RevokeToken(ctx, user, id); err != nil && !errors.Is(err, store.ErrNotFound) {
					r.warn("the previous token %s of %s could not be revoked: %v", id, user, err)
					remaining = append(remaining, id)
				}
			}
		}
	}
	led := loadTokenLedger(r.env)
	if !slices.Contains(remaining, r.tokenID) {
		remaining = append(remaining, r.tokenID)
	}
	led.Labels[label] = remaining
	if err := led.save(r.env); err != nil {
		r.warn("could not record the issued token id: %v", err)
	}
}

// discardNewToken revokes the token a failed run minted (the rollback put the
// previous secrets file back, so the new one would only be an orphan).
func (r *run) discardNewToken(ctx context.Context) {
	t := r.pl.Team
	if t == nil || t.Server == nil || !r.tokenNew || r.tokenID == "" || r.env.Team.OpenStore == nil {
		return
	}
	st, closeFn, err := r.env.Team.OpenStore(ctx, t.Server.Instance)
	if err != nil {
		r.warn("rollback: the token minted by this run could not be revoked: %v", err)
		return
	}
	defer closeFn()
	if err := st.RevokeToken(ctx, r.pl.Options.User, r.tokenID); err != nil && !errors.Is(err, store.ErrNotFound) {
		r.warn("rollback: the token minted by this run could not be revoked: %v", err)
	}
}
