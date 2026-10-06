package installer

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/winnstorm/gabitotl/internal/memory/mcp"
	"github.com/winnstorm/gabitotl/internal/memory/server"
	"github.com/winnstorm/gabitotl/internal/memory/store"
)

func activeTokens(t *testing.T, st store.Store, user string) []store.TokenRecord {
	t.Helper()
	all, err := st.ListTokens(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}
	var out []store.TokenRecord
	for _, tk := range all {
		if tk.Active() {
			out = append(out, tk)
		}
	}
	return out
}

func installHost(t *testing.T, r *teamRig, o Options, force bool) (Result, error) {
	t.Helper()
	inv, pl, err := r.plan(o)
	if err != nil {
		t.Fatal(err)
	}
	if force { // exercise the apply path even when the plan sees nothing to do
		pl.Team.Unchanged = false
	}
	return Run(context.Background(), r.env, inv, pl, nil, Hooks{})
}

func TestHostRerunKeepsExactlyOneActiveInstallerToken(t *testing.T) {
	r := newTeamRig(t)
	if _, err := installHost(t, r, hostOpts(), false); err != nil {
		t.Fatal(err)
	}
	first := storedToken(r.env, DefaultTokenEnv)
	if first == "" {
		t.Fatal("the first run must store a token")
	}
	secretsBefore := slurp(t, mcp.ClientSecretsPath(r.home))

	// A natural re-run is a no-op ...
	if res, err := installHost(t, r, hostOpts(), false); err != nil || !res.Skipped {
		t.Fatalf("%v skipped=%v", err, res.Skipped)
	}
	// ... and even a forced pass through the apply path reuses the valid token.
	for i := 0; i < 2; i++ {
		if _, err := installHost(t, r, hostOpts(), true); err != nil {
			t.Fatal(err)
		}
	}
	if got := storedToken(r.env, DefaultTokenEnv); got != first {
		t.Fatal("the valid token in the secrets file must be reused, not replaced")
	}
	if slurp(t, mcp.ClientSecretsPath(r.home)) != secretsBefore {
		t.Fatal("the secrets file must not change")
	}
	if act := activeTokens(t, r.st, "gabi"); len(act) != 1 || act[0].ID != tokenID(first) {
		t.Fatalf("exactly one active token, the original: %+v", act)
	}
	if all, _ := r.st.ListTokens(context.Background(), "gabi"); len(all) != 1 {
		t.Fatalf("no token may be minted on a re-run: %d", len(all))
	}
}

func TestLostSecretsFileMintsANewTokenAndRevokesTheOldOne(t *testing.T) {
	r := newTeamRig(t)
	if _, err := installHost(t, r, hostOpts(), false); err != nil {
		t.Fatal(err)
	}
	oldTok := storedToken(r.env, DefaultTokenEnv)
	if err := os.Remove(mcp.ClientSecretsPath(r.home)); err != nil {
		t.Fatal(err)
	}
	// A token a person created by hand for the same user is theirs: never touched.
	manual, _, err := server.NewScopedToken(context.Background(), r.st, "gabi", []string{"shop"}, false, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	res, err := installHost(t, r, hostOpts(), false)
	if err != nil || res.Skipped {
		t.Fatalf("a missing token must be repaired: %v skipped=%v", err, res.Skipped)
	}
	newTok := storedToken(r.env, DefaultTokenEnv)
	if newTok == "" || newTok == oldTok {
		t.Fatal("a new token must be stored")
	}
	ctx := context.Background()
	if _, err := server.VerifyToken(ctx, r.st, oldTok); err == nil {
		t.Fatal("the previous installer token must be revoked")
	}
	for name, tok := range map[string]string{"new": newTok, "manual": manual} {
		if u, err := server.VerifyToken(ctx, r.st, tok); err != nil || u != "gabi" {
			t.Fatalf("the %s token must stay valid: %v", name, err)
		}
	}
	if act := activeTokens(t, r.st, "gabi"); len(act) != 2 {
		t.Fatalf("the installer's new token and the manual one: %+v", act)
	}
	// And the new one is what the next run reuses.
	if _, err := installHost(t, r, hostOpts(), true); err != nil {
		t.Fatal(err)
	}
	if storedToken(r.env, DefaultTokenEnv) != newTok || len(activeTokens(t, r.st, "gabi")) != 2 {
		t.Fatal("the new token must be reused from now on")
	}
}

func TestRejectedStoredTokenIsReplacedNotAccumulated(t *testing.T) {
	r := newTeamRig(t)
	if _, err := installHost(t, r, hostOpts(), false); err != nil {
		t.Fatal(err)
	}
	oldTok := storedToken(r.env, DefaultTokenEnv)
	if err := r.st.RevokeToken(context.Background(), "gabi", tokenID(oldTok)); err != nil { // the server no longer accepts it
		t.Fatal(err)
	}
	if res, err := installHost(t, r, hostOpts(), false); err != nil || res.Skipped {
		t.Fatalf("an invalid stored token must trigger a repair: %v skipped=%v", err, res.Skipped)
	}
	newTok := storedToken(r.env, DefaultTokenEnv)
	if newTok == oldTok {
		t.Fatal("a new token is expected")
	}
	if act := activeTokens(t, r.st, "gabi"); len(act) != 1 || act[0].ID != tokenID(newTok) {
		t.Fatalf("%+v", act)
	}
}

func TestTokenLedgerHoldsIdsNeverSecrets(t *testing.T) {
	r := newTeamRig(t)
	if _, err := installHost(t, r, hostOpts(), false); err != nil {
		t.Fatal(err)
	}
	tok := storedToken(r.env, DefaultTokenEnv)
	b := slurp(t, tokenLedgerPath(r.env))
	secret := strings.SplitN(tok, ".", 2)[1]
	if strings.Contains(b, secret) || !strings.Contains(b, tokenID(tok)) {
		t.Fatalf("the ledger lists ids only:\n%s", b)
	}
	if st, err := os.Stat(tokenLedgerPath(r.env)); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("%v %v", st, err)
	}
}

func TestRollbackAfterRotationKeepsTheOldTokenAndDiscardsTheNewOne(t *testing.T) {
	r := newTeamRig(t)
	if _, err := installHost(t, r, hostOpts(), false); err != nil {
		t.Fatal(err)
	}
	oldTok := storedToken(r.env, DefaultTokenEnv)
	// Revoke it on the server so the next run has to mint, then make verification fail.
	// (A lost file would do too; this keeps the old file so the rollback has something to restore.)
	if err := r.st.RevokeToken(context.Background(), "gabi", tokenID(oldTok)); err != nil {
		t.Fatal(err)
	}
	inv, pl, err := r.plan(hostOpts())
	if err != nil {
		t.Fatal(err)
	}
	inner, calls := r.env.Team.OpenStore, 0
	r.env.Team.OpenStore = func(ctx context.Context, inst string) (store.Store, func(), error) {
		calls++
		if calls == 2 { // 1st open: tokens, 2nd open: verification fails; the rollback's own open works
			return nil, nil, os.ErrDeadlineExceeded
		}
		return inner(ctx, inst)
	}
	if _, err := Run(context.Background(), r.env, inv, pl, nil, Hooks{}); err == nil {
		t.Fatal("expected a failure")
	}
	if storedToken(r.env, DefaultTokenEnv) != oldTok {
		t.Fatal("the secrets file must be restored")
	}
	all, _ := r.st.ListTokens(context.Background(), "gabi")
	if len(all) != 2 {
		t.Fatalf("one original, one minted by the failed run: %d", len(all))
	}
	if n := len(activeTokens(t, r.st, "gabi")); n != 0 {
		t.Fatalf("the failed run's token must be revoked (the original was revoked above): %d active", n)
	}
}
