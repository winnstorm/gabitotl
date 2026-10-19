package gui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/credstore"
)

type credRig struct {
	*rig
	store *credstore.Fake
	ok    bool
	err   error
	saw   []string
}

func newCredRig(t *testing.T) *credRig {
	t.Helper()
	c := &credRig{rig: newRig(t), store: credstore.NewFake(), ok: true}
	c.svc.d.Credentials = func(string) credstore.Store { return c.store }
	c.svc.d.VerifyToken = func(_ context.Context, platform, host, _ string) (bool, string, error) {
		c.saw = append(c.saw, platform+"@"+host)
		return c.ok, "the token was rejected", c.err
	}
	return c
}

func TestGlobalDeliveryRoundTripWritesNoToken(t *testing.T) {
	r := newRig(t)
	g, err := r.svc.GlobalDelivery()
	if err != nil || g.Saved || g.Automation != "ask" || g.MergeMethod != "squash" {
		t.Fatalf("defaults: %+v %v", g, err)
	}
	f := false
	g, err = r.svc.SetGlobalDelivery(GlobalDeliveryAnswers{Automation: "until-pr", MergeMethod: "rebase", DeleteBranch: &f})
	if err != nil || !g.Saved || g.Automation != "until-pr" || g.MergeMethod != "rebase" || g.DeleteBranch {
		t.Fatalf("saved: %+v %v", g, err)
	}
	if again, _ := r.svc.GlobalDelivery(); again != g {
		t.Fatalf("reload %+v != %+v", again, g)
	}
	b, err := os.ReadFile(filepath.Join(r.home, ".gabitotl", "vcs.md"))
	if err != nil || strings.Contains(string(b), "auth") || strings.Contains(string(b), "configured") {
		t.Fatalf("global file must hold settings only:\n%s %v", b, err)
	}
	if _, err := r.svc.SetGlobalDelivery(GlobalDeliveryAnswers{Automation: "bogus"}); err == nil {
		t.Fatal("an invalid automation must be rejected")
	}
}

func TestSaveCredentialVerifiesThenStoresAndNeverReturnsTheToken(t *testing.T) {
	c := newCredRig(t)
	const token = "ghp_secret_value"
	res, err := c.svc.SaveCredential(context.Background(), "github", "", token)
	if err != nil || res.Status != CredSaved || !res.Stored || res.Host != "github.com" {
		t.Fatalf("%+v %v", res, err)
	}
	if got, _, _ := c.store.Get("github.com"); got != token {
		t.Fatal("not stored")
	}
	list, _ := c.svc.Credentials()
	if len(list) != 3 || !list[0].Stored || list[1].Stored {
		t.Fatalf("%+v", list)
	}
	for _, v := range []any{res, list} {
		if strings.Contains(strings.Join([]string{jsonOf(t, v)}, ""), token) {
			t.Fatal("the token leaked into a returned value")
		}
	}
	if err := c.svc.DeleteCredential("github.com"); err != nil {
		t.Fatal(err)
	}
	if list, _ := c.svc.Credentials(); list[0].Stored {
		t.Fatal("still stored")
	}
}

func TestSaveCredentialStoresNothingWhenRejectedOrUnreachableOrInvalid(t *testing.T) {
	c := newCredRig(t)
	ctx := context.Background()
	c.ok = false
	if res, _ := c.svc.SaveCredential(ctx, "gitlab", "", "tok"); res.Status != CredRejected || res.Stored {
		t.Fatalf("%+v", res)
	}
	c.err = errors.New("dial tcp: boom")
	if res, err := c.svc.SaveCredential(ctx, "gitlab", "gitlab.example.org", "tok"); err != nil || res.Status != CredUnreachable || strings.Contains(res.Detail, "boom") {
		t.Fatalf("%+v %v", res, err)
	}
	for _, tc := range [][3]string{{"svn", "", "tok"}, {"github", "bad host", "tok"}, {"github", "", "a\nb"}} {
		if res, _ := c.svc.SaveCredential(ctx, tc[0], tc[1], tc[2]); res.Status != CredInvalid {
			t.Fatalf("%v: %+v", tc, res)
		}
	}
	if len(c.store.M) != 0 {
		t.Fatalf("nothing may be stored: %v", c.store.M)
	}
	if len(c.saw) != 2 {
		t.Fatalf("invalid input must not reach the platform: %v", c.saw)
	}
}

func TestCredentialMethodsNeedTheirPorts(t *testing.T) {
	r := newRig(t)
	if _, err := r.svc.Credentials(); !errors.Is(err, ErrNotAvailable) {
		t.Fatal(err)
	}
	if _, err := r.svc.SaveCredential(context.Background(), "github", "", "t"); !errors.Is(err, ErrNotAvailable) {
		t.Fatal(err)
	}
}

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

type tooLongStore struct{ *credstore.Fake }

func (tooLongStore) Set(string, string) error { return credstore.ErrTokenTooLong }

func TestSaveCredentialMapsATooLongTokenToInvalid(t *testing.T) {
	c := newCredRig(t)
	c.svc.d.Credentials = func(string) credstore.Store { return tooLongStore{c.store} }
	res, err := c.svc.SaveCredential(context.Background(), "github", "", "tok")
	if err != nil || res.Status != CredInvalid || res.Stored {
		t.Fatalf("%+v %v", res, err)
	}
}
