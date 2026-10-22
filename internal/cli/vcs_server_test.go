package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/vcshosts"
)

const dcToken = "dc-token-987654"

// dataCenter is a Bitbucket Data Center that accepts dcToken as a bearer.
func dataCenter(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bitbucket/rest/api/latest/profile/recent/repos" || r.Header.Get("Authorization") != "Bearer "+dcToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("X-AUSERNAME", "jdoe")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// fakeServers swaps the registry for an in-memory one.
func fakeServers(t *testing.T) *vcshosts.Fake {
	t.Helper()
	f := &vcshosts.Fake{}
	old := serversFn
	serversFn = func(string) vcshosts.Registry { return f }
	t.Cleanup(func() { serversFn = old })
	return f
}

func TestLoginServerVerifiesRegistersAndStores(t *testing.T) {
	srv := dataCenter(t)
	reg, creds := fakeServers(t), fakeCreds(t)
	withPlatform(t, &platformFake{}, map[string]string{"BITBUCKET_SERVER_TOKEN": dcToken})
	r := runIn(t, "", false, t.TempDir(), "vcs", "login", "--url", srv.URL+"/bitbucket/")
	if r.code != 0 || !strings.Contains(r.out, "aceptó el token (sesión como jdoe)") || !strings.Contains(r.out, "Registré 127.0.0.1") {
		t.Fatalf("%+v", r)
	}
	if strings.Contains(r.out+r.err, dcToken) {
		t.Fatal("the token was printed")
	}
	if tok, ok, _ := creds.Get("127.0.0.1"); !ok || tok != dcToken {
		t.Fatal("token not stored")
	}
	if e, ok := vcshosts.Lookup(reg, "127.0.0.1"); !ok || e.URL != srv.URL+"/bitbucket" || e.Platform != vcshosts.BitbucketServer {
		t.Fatalf("registry: %+v", reg.Entries)
	}
	// Later, without the variable, the stored token is verified again; --check writes nothing.
	reg.Entries, creds.M = nil, map[string]string{"127.0.0.1": dcToken}
	withPlatform(t, &platformFake{}, nil)
	if r := runIn(t, "", false, t.TempDir(), "vcs", "login", "--url", srv.URL+"/bitbucket", "--check"); r.code != 0 || len(reg.Entries) != 0 {
		t.Fatalf("%+v %v", r, reg.Entries)
	}
}

func TestLoginServerStdinTokenAndRejection(t *testing.T) {
	srv := dataCenter(t)
	reg, creds := fakeServers(t), fakeCreds(t)
	withPlatform(t, &platformFake{}, nil)
	r := runIn(t, "wrong-secret-token\n", false, t.TempDir(), "vcs", "login", "--url", srv.URL+"/bitbucket", "--token-stdin")
	if r.code == 0 || !strings.Contains(r.err, "no aceptó el token") || strings.Contains(r.out+r.err, "wrong-secret-token") {
		t.Fatalf("%+v", r)
	}
	if len(creds.M) != 0 || len(reg.Entries) != 0 {
		t.Fatal("a rejected token must store and register nothing")
	}
	if r := runIn(t, dcToken+"\n", false, t.TempDir(), "vcs", "login", "--url", srv.URL+"/bitbucket", "--token-stdin"); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if len(creds.M) != 1 || len(reg.Entries) != 1 {
		t.Fatal("accepted token must be stored and registered")
	}
}

func TestLoginServerRefusesBadInput(t *testing.T) {
	reg, creds := fakeServers(t), fakeCreds(t)
	withPlatform(t, &platformFake{}, nil)
	for _, args := range [][]string{
		{"--url", "https://user:pw@bb.corp"},
		{"--url", "ftp://bb.corp"},
		{"--url", "https://bb.corp", "--platform", "svn"},
		{"--url", "https://bb.corp"}, // no token anywhere
		{"--url", "https://bb.corp", "--token-env", "bad name"},
	} {
		r := runIn(t, "", false, t.TempDir(), append([]string{"vcs", "login"}, args...)...)
		if r.code == 0 {
			t.Fatalf("%v: %+v", args, r)
		}
	}
	if len(reg.Entries) != 0 || len(creds.M) != 0 {
		t.Fatal("nothing may be written")
	}
}

func TestLoginServerUnreachableAndCertificateMessage(t *testing.T) {
	fakeServers(t)
	fakeCreds(t)
	withPlatform(t, &platformFake{}, map[string]string{"BITBUCKET_SERVER_TOKEN": dcToken})
	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer tlsSrv.Close()
	r := runIn(t, "", false, t.TempDir(), "vcs", "login", "--url", tlsSrv.URL)
	if r.code == 0 || !strings.Contains(r.err, "no considera confiable") || strings.Contains(r.err, dcToken) {
		t.Fatalf("%+v", r)
	}
}

func TestVCSTestChecksTheStoredToken(t *testing.T) {
	srv := dataCenter(t)
	reg, creds := fakeServers(t), fakeCreds(t)
	withPlatform(t, &platformFake{}, nil)
	_ = reg.Put(vcshosts.Entry{Platform: vcshosts.BitbucketServer, URL: srv.URL + "/bitbucket"}, vcshosts.PutOptions{})
	if r := runIn(t, "", false, t.TempDir(), "vcs", "test", "--host", "127.0.0.1:7999"); r.code == 0 || !strings.Contains(r.err, "No hay un token guardado") {
		t.Fatalf("no token: %+v", r)
	}
	_ = creds.Set("127.0.0.1", dcToken)
	if r := runIn(t, "", false, t.TempDir(), "vcs", "test", "--host", "127.0.0.1"); r.code != 0 || !strings.Contains(r.out, "sesión como jdoe") || strings.Contains(r.out+r.err, dcToken) {
		t.Fatalf("ok: %+v", r)
	}
	_ = creds.Set("127.0.0.1", "stale-token-value")
	if r := runIn(t, "", false, t.TempDir(), "vcs", "test", "--host", "127.0.0.1"); r.code == 0 || !strings.Contains(r.err, "no aceptó el token") || strings.Contains(r.err, "stale-token-value") {
		t.Fatalf("rejected: %+v", r)
	}
	if r := runIn(t, "", false, t.TempDir(), "vcs", "test", "--host", "unknown.corp"); r.code == 0 || !strings.Contains(r.err, "no está registrado") {
		t.Fatalf("unregistered: %+v", r)
	}
}

func TestProjectOnARegisteredServerUsesBitbucketServer(t *testing.T) {
	srv := dataCenter(t)
	reg, creds := fakeServers(t), fakeCreds(t)
	_ = reg.Put(vcshosts.Entry{Platform: vcshosts.BitbucketServer, URL: srv.URL + "/bitbucket"}, vcshosts.PutOptions{})
	_ = creds.Set("127.0.0.1", dcToken)
	withPlatform(t, &platformFake{}, nil)
	root := project(t)
	setOrigin(t, root, "ssh://git@127.0.0.1:7999/proj/app.git")
	if r := runIn(t, "", false, root, "vcs", "status"); r.code != 0 || !strings.Contains(r.out, "provider: bitbucket-server") || !strings.Contains(r.out, "auth: ok (bitbucket-server)") {
		t.Fatalf("%+v", r)
	}
	if r := runIn(t, "", false, root, "vcs", "login"); r.code != 0 || !strings.Contains(r.out, "Bitbucket Server") {
		t.Fatalf("%+v", r)
	}
	if r := runIn(t, "", false, root, "vcs", "test"); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	delete(creds.M, "127.0.0.1")
	if r := runIn(t, "", false, root, "vcs", "login"); r.code == 0 || !strings.Contains(r.err, "--url") {
		t.Fatalf("%+v", r)
	}
}

func TestLoginServerPlainHTTPNeedsTheExplicitFlag(t *testing.T) {
	reg, creds := fakeServers(t), fakeCreds(t)
	withPlatform(t, &platformFake{}, map[string]string{"BITBUCKET_SERVER_TOKEN": dcToken})
	r := runIn(t, "", false, t.TempDir(), "vcs", "login", "--url", "http://10.0.0.5:7990")
	if r.code == 0 || !strings.Contains(r.err, "http sin cifrar") || strings.Contains(r.err, "texto plano: ") {
		t.Fatalf("%+v", r)
	}
	// With the override it warns about clear text and goes on to verify (the host does not resolve).
	r = runIn(t, "", false, t.TempDir(), "vcs", "login", "--url", "http://bb.invalid:7990", "--allow-insecure-http")
	if r.code == 0 || !strings.Contains(r.err, "texto plano") || strings.Contains(r.err, "Usar https://") {
		t.Fatalf("%+v", r)
	}
	if len(reg.Entries) != 0 || len(creds.M) != 0 {
		t.Fatal("nothing may be written")
	}
}

func TestLoginServerHostConflictNeedsReplaceAndSSHHostIsKept(t *testing.T) {
	srv := dataCenter(t)
	reg, creds := fakeServers(t), fakeCreds(t)
	withPlatform(t, &platformFake{}, map[string]string{"BITBUCKET_SERVER_TOKEN": dcToken})
	if r := runIn(t, "", false, t.TempDir(), "vcs", "login", "--url", srv.URL+"/bitbucket", "--ssh-host", "git-ssh.corp", "--ssh-host", "git2.corp"); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if e, ok := vcshosts.Lookup(reg, "git-ssh.corp"); !ok || e.Host != "127.0.0.1" || len(e.SSHHosts) != 2 {
		t.Fatalf("ssh alias: %+v", reg.Entries)
	}
	// Same host name, another context path: a different server.
	r := runIn(t, "", false, t.TempDir(), "vcs", "login", "--url", srv.URL+"/other")
	if r.code == 0 || !strings.Contains(r.err, "--replace") || reg.Entries[0].URL != srv.URL+"/bitbucket" {
		t.Fatalf("%+v %+v", r, reg.Entries)
	}
	if r := runIn(t, "", false, t.TempDir(), "vcs", "login", "--url", srv.URL+"/bitbucket", "--ssh-host", "bad/host"); r.code == 0 || !strings.Contains(r.err, "SSH") {
		t.Fatalf("bad ssh host: %+v", r)
	}
	_ = creds
}
