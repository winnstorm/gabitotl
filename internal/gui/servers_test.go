package gui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/vcs"
	"github.com/winnstorm/gabitotl/internal/vcshosts"
)

func newServerRig(t *testing.T) *credRig {
	c := newCredRig(t)
	c.svc.d.Servers = func(home string) vcshosts.Registry { return vcshosts.NewFile(home) }
	return c
}

func TestSaveServerVerifiesAtTheURLThenRegistersAndStores(t *testing.T) {
	c := newServerRig(t)
	c.user = "jdoe"
	const token = "dc-secret-token"
	res, err := c.svc.SaveServer(context.Background(), "bitbucket-server", "https://Bitbucket.Personal.Corp:8443/bb/", token, nil, false)
	if err != nil || res.Status != CredSaved || !res.Stored || res.Host != "bitbucket.personal.corp" || res.User != "jdoe" {
		t.Fatalf("%+v %v", res, err)
	}
	if got := c.saw[0]; got != "bitbucket-server@bitbucket.personal.corp|https://Bitbucket.Personal.Corp:8443/bb" && !strings.HasPrefix(got, "bitbucket-server@bitbucket.personal.corp|https://") {
		t.Fatalf("verified at %q", got)
	}
	if tok, ok, _ := c.store.Get("bitbucket.personal.corp"); !ok || tok != token {
		t.Fatal("token not stored under the host without port")
	}
	list, err := c.svc.Servers()
	if err != nil || len(list) != 1 || !list[0].Stored || list[0].Platform != "bitbucket-server" || !strings.HasSuffix(list[0].URL, ":8443/bb") {
		t.Fatalf("%+v %v", list, err)
	}
	if strings.Contains(jsonOf(t, res)+jsonOf(t, list), token) {
		t.Fatal("the token leaked into a returned value")
	}
	if err := c.svc.DeleteServer("bitbucket.personal.corp"); err != nil {
		t.Fatal(err)
	}
	if list, _ := c.svc.Servers(); len(list) != 0 {
		t.Fatalf("still registered: %+v", list)
	}
	if _, ok, _ := c.store.Get("bitbucket.personal.corp"); ok {
		t.Fatal("token still stored")
	}
}

func TestSaveServerWritesNothingOnFailure(t *testing.T) {
	c := newServerRig(t)
	ctx := context.Background()
	c.ok = false
	res, _ := c.svc.SaveServer(ctx, "bitbucket-server", "https://bb.corp", "bad-token", nil, false)
	if res.Status != CredRejected || res.Stored {
		t.Fatalf("rejected: %+v", res)
	}
	c.ok, c.err = true, errors.New("bb.corp: certificate not trusted by this system")
	res, _ = c.svc.SaveServer(ctx, "bitbucket-server", "https://bb.corp", "tok", nil, false)
	if res.Status != CredUnreachable || !strings.Contains(res.Detail, "certificate not trusted") {
		t.Fatalf("unreachable: %+v", res)
	}
	c.err = nil
	n := len(c.saw)
	for _, in := range [][3]string{
		{"svn", "https://bb.corp", "tok"},
		{"bitbucket-server", "https://u:p@bb.corp", "tok"},
		{"bitbucket-server", "ftp://bb.corp", "tok"},
		{"bitbucket-server", "https://bb.corp", "a\nb"},
		{"bitbucket-server", "https://bb.corp", ""}, // nothing stored to reuse
	} {
		if res, _ := c.svc.SaveServer(ctx, in[0], in[1], in[2], nil, false); res.Status != CredInvalid {
			t.Fatalf("%v: %+v", in, res)
		}
	}
	if len(c.saw) != n {
		t.Fatal("invalid input must not reach the server")
	}
	if list, _ := c.svc.Servers(); len(list) != 0 || len(c.store.M) != 0 {
		t.Fatalf("nothing may be written: %+v %v", list, c.store.M)
	}
}

func TestSaveServerWithoutATokenChecksTheStoredOne(t *testing.T) {
	c := newServerRig(t)
	_ = c.store.Set("bb.corp", "stored-token")
	res, err := c.svc.SaveServer(context.Background(), "bitbucket-server", "https://bb.corp:8443", "", nil, false)
	if err != nil || res.Status != CredSaved {
		t.Fatalf("%+v %v", res, err)
	}
	if tok, _, _ := c.store.Get("bb.corp"); tok != "stored-token" {
		t.Fatal("the stored token must be kept")
	}
	if list, _ := c.svc.Servers(); len(list) != 1 {
		t.Fatalf("%+v", list)
	}
}

func TestTestCredentialUsesTheStoredToken(t *testing.T) {
	c := newServerRig(t)
	ctx := context.Background()
	if res, _ := c.svc.TestCredential(ctx, "github", ""); res.Status != CredInvalid || res.Host != "github.com" {
		t.Fatalf("no token: %+v", res)
	}
	_ = c.store.Set("github.com", "ghp_x")
	c.user = "octo"
	if res, _ := c.svc.TestCredential(ctx, "github", "github.com"); res.Status != CredOK || res.User != "octo" || !res.Stored {
		t.Fatalf("public: %+v", res)
	}
	if _, err := c.svc.SaveServer(ctx, "bitbucket-server", "https://bb.corp/ctx", "tok", nil, false); err != nil {
		t.Fatal(err)
	}
	c.saw = nil
	if res, _ := c.svc.TestCredential(ctx, "bitbucket-server", "bb.corp:7999"); res.Status != CredOK || res.Host != "bb.corp" || len(c.saw) != 1 || !strings.HasSuffix(c.saw[0], "|https://bb.corp/ctx") {
		t.Fatalf("server: %+v %v", res, c.saw)
	}
	c.ok = false
	if res, _ := c.svc.TestCredential(ctx, "bitbucket-server", "bb.corp"); res.Status != CredRejected {
		t.Fatalf("rejected: %+v", res)
	}
	if res, _ := c.svc.TestCredential(ctx, "bitbucket-server", "other.corp"); res.Status != CredInvalid {
		t.Fatalf("unknown: %+v", res)
	}
	if res, _ := c.svc.TestCredential(ctx, "gitlab", "bb.corp"); res.Status != CredInvalid {
		t.Fatalf("platform mismatch: %+v", res)
	}
}

func TestProjectOnARegisteredServer(t *testing.T) {
	c := newServerRig(t)
	ctx := context.Background()
	base := c.runner.Handlers["git"]
	c.runner.Handlers["git"] = func(a []string) (string, error) {
		if len(a) > 1 && a[0] == "remote" {
			return "ssh://git@bb.corp:7999/proj/app.git\n", nil
		}
		return base(a)
	}
	if d, _ := c.svc.Project(ctx, c.root); d.Platform != "git" || d.Credential.Server {
		t.Fatalf("unregistered host: %+v", d.Credential)
	}
	if _, err := c.svc.SaveServer(ctx, "bitbucket-server", "https://bb.corp:8443/bb", "tok", nil, false); err != nil {
		t.Fatal(err)
	}
	d, err := c.svc.Project(ctx, c.root)
	if err != nil || d.Platform != "bitbucket-server" || d.Host != "bb.corp" || !d.Credential.Server || !d.Credential.Stored || d.Credential.ServerURL != "https://bb.corp:8443/bb" {
		t.Fatalf("%+v %v", d, err)
	}
}

func TestServerMethodsNeedTheirPorts(t *testing.T) {
	c := newCredRig(t) // no Servers dependency
	if _, err := c.svc.Servers(); !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("got %v", err)
	}
	if _, err := c.svc.SaveServer(context.Background(), "bitbucket-server", "https://bb.corp", "t", nil, false); !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("got %v", err)
	}
}

func TestSaveServerRefusesPlainHTTPExceptLoopback(t *testing.T) {
	c := newServerRig(t)
	ctx := context.Background()
	res, err := c.svc.SaveServer(ctx, "bitbucket-server", "http://10.0.0.5:7990", "tok", nil, true)
	if err != nil || res.Status != CredInvalid || res.Code != "insecure_http" || len(c.saw) != 0 {
		t.Fatalf("%+v %v verified=%v", res, err, c.saw)
	}
	if res, _ := c.svc.SaveServer(ctx, "bitbucket-server", "http://localhost:7990", "tok", nil, false); res.Status != CredSaved {
		t.Fatalf("loopback http: %+v", res)
	}
	if res, _ := c.svc.SaveServer(ctx, "bitbucket-server", "ftp://x.corp", "tok", nil, false); res.Code != "invalid_url" {
		t.Fatalf("bad url: %+v", res)
	}
}

func TestSaveServerHostConflictNeedsReplaceAndANewToken(t *testing.T) {
	c := newServerRig(t)
	ctx := context.Background()
	if res, _ := c.svc.SaveServer(ctx, "bitbucket-server", "https://bb.corp:8443/bb", "old-token", nil, false); res.Status != CredSaved {
		t.Fatalf("first: %+v", res)
	}
	c.saw = nil
	// Same host name, another port: refused before anything is sent to it.
	res, err := c.svc.SaveServer(ctx, "bitbucket-server", "https://bb.corp:9443/bb", "new-token", nil, false)
	if err != nil || res.Status != CredConflict || res.Code != "host_conflict" || len(c.saw) != 0 {
		t.Fatalf("conflict: %+v %v verified=%v", res, err, c.saw)
	}
	if tok, _, _ := c.store.Get("bb.corp"); tok != "old-token" {
		t.Fatal("a refused conflict must keep the old token")
	}
	// Replacing without a new token never reuses the old server's token.
	if res, _ := c.svc.SaveServer(ctx, "bitbucket-server", "https://bb.corp:9443/bb", "", nil, true); res.Status != CredInvalid || res.Code != "not_stored" || len(c.saw) != 0 {
		t.Fatalf("replace without token: %+v", res)
	}
	res, err = c.svc.SaveServer(ctx, "bitbucket-server", "https://bb.corp:9443/bb", "new-token", nil, true)
	if err != nil || res.Status != CredSaved {
		t.Fatalf("replace: %+v %v", res, err)
	}
	if tok, _, _ := c.store.Get("bb.corp"); tok != "new-token" {
		t.Fatal("token not replaced")
	}
	if list, _ := c.svc.Servers(); len(list) != 1 || !strings.Contains(list[0].URL, ":9443") {
		t.Fatalf("%+v", list)
	}
}

func TestSaveServerSSHHostsAndCorruptRegistry(t *testing.T) {
	c := newServerRig(t)
	ctx := context.Background()
	res, err := c.svc.SaveServer(ctx, "bitbucket-server", "https://bb.corp", "tok", []string{"SSH.bb.corp"}, false)
	if err != nil || res.Status != CredSaved {
		t.Fatalf("%+v %v", res, err)
	}
	if list, _ := c.svc.Servers(); len(list) != 1 || len(list[0].SSHHosts) != 1 || list[0].SSHHosts[0] != "ssh.bb.corp" {
		t.Fatalf("%+v", list)
	}
	if res, _ := c.svc.SaveServer(ctx, "bitbucket-server", "https://bb.corp", "tok", []string{"a/b"}, false); res.Code != "invalid_url" {
		t.Fatalf("bad ssh host: %+v", res)
	}
	p := vcshosts.FilePath(c.home)
	if err := os.WriteFile(p, []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := c.svc.Servers(); err == nil || !strings.Contains(err.Error(), p) {
		t.Fatalf("a corrupt registry must be an error naming its file: %v", err)
	}
	if _, err := c.svc.TestCredential(ctx, "bitbucket-server", "bb.corp"); err == nil {
		t.Fatal("TestCredential must not say 'unknown server' for a corrupt registry")
	}
}

func TestFailureCodesComeFromTypedErrors(t *testing.T) {
	c := newServerRig(t)
	ctx := context.Background()
	for code, err := range map[string]error{
		"cert_untrusted": fmt.Errorf("wrapped: %w", vcs.ErrCertificate),
		"no_permission":  fmt.Errorf("wrapped: %w", vcs.ErrNoPermission),
		"timeout":        fmt.Errorf("wrapped: %w", vcs.ErrTimeout),
		"unreachable":    errors.New("connection refused"),
	} {
		c.err = err
		res, _ := c.svc.SaveServer(ctx, "bitbucket-server", "https://bb.corp", "tok", nil, false)
		if res.Status != CredUnreachable || res.Code != code {
			t.Errorf("%s: %+v", code, res)
		}
	}
	c.err, c.ok = nil, false
	if res, _ := c.svc.SaveServer(ctx, "bitbucket-server", "https://bb.corp", "tok", nil, false); res.Status != CredRejected || res.Code != "rejected" {
		t.Fatalf("rejected: %+v", res)
	}
	if res, _ := c.svc.TestCredential(ctx, "bitbucket-server", "other.corp"); res.Code != "unknown_host" {
		t.Fatalf("unknown host: %+v", res)
	}
	_ = c.store.Set("github.com", "x")
	_ = c.store.Delete("github.com")
	if res, _ := c.svc.TestCredential(ctx, "github", ""); res.Code != "not_stored" {
		t.Fatalf("not stored: %+v", res)
	}
}
