package cli

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/memory/mcp"
	rt "github.com/winnstorm/gabitotl/internal/runtime"
	"github.com/winnstorm/gabitotl/internal/vcs"
)

// platformFake stands for gh and glab: `<tool> auth status` succeeds only while
// loggedIn, and the scripted terminal login flips it.
type platformFake struct {
	mu       sync.Mutex
	loggedIn bool
	missing  bool
	loginErr error
	calls    []string
	tty      []string
}

func (p *platformFake) Run(_ context.Context, _ string, name string, args ...string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, name+" "+strings.Join(args, " "))
	if p.missing {
		return "", &vcs.CommandError{Cmd: name, Err: exec.ErrNotFound}
	}
	if name == "git" { // the real git answers for origin in these tests
		out, err := vcs.ExecRunner{}.Run(context.Background(), "", name, args...)
		return out, err
	}
	if !p.loggedIn {
		return "", &vcs.CommandError{Cmd: name, Err: errors.New("exit 1"), Stderr: "You are not logged in"}
	}
	return "Logged in", nil
}

func (p *platformFake) RunTTY(_ context.Context, _ string, name string, args ...string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tty = append(p.tty, name+" "+strings.Join(args, " "))
	if p.loginErr == nil {
		p.loggedIn = true
	}
	return p.loginErr
}

// withPlatform installs the fake gh/glab and a private home for the secrets file.
func withPlatform(t *testing.T, p *platformFake, vars map[string]string) (home string) {
	t.Helper()
	home = t.TempDir()
	oldRun, oldTTY, oldEnv := vcsRunnerFn, vcsTTYFn, runtimeEnvFn
	vcsRunnerFn = func() vcs.Runner { return gitThrough{p} }
	vcsTTYFn = func() vcs.TTYRunner { return p }
	runtimeEnvFn = func(root string) (rt.Env, error) {
		return rt.Env{Home: home, ProjectRoot: root, GOOS: "linux", Getenv: func(k string) string { return vars[k] }}, nil
	}
	t.Cleanup(func() { vcsRunnerFn, vcsTTYFn, runtimeEnvFn = oldRun, oldTTY, oldEnv })
	return home
}

// gitThrough runs git for real (the repositories are temp ones) and sends every
// other command to the fake.
type gitThrough struct{ p *platformFake }

func (g gitThrough) Run(ctx context.Context, dir, name string, args ...string) (string, error) {
	if name == "git" {
		return vcs.ExecRunner{}.Run(ctx, dir, name, args...)
	}
	return g.p.Run(ctx, dir, name, args...)
}

func setOrigin(t *testing.T, root, url string) {
	t.Helper()
	if _, err := vcs.New(root, gabitotl.VCSConfig{}).Git(context.Background(), "remote", "add", "origin", url); err != nil {
		t.Fatal(err)
	}
}

func TestLoginWhenAlreadyLoggedInSaysSoAndRunsNoLogin(t *testing.T) {
	root := project(t)
	setOrigin(t, root, "git@github.com:acme/app.git")
	p := &platformFake{loggedIn: true}
	withPlatform(t, p, nil)
	r := runIn(t, "", true, root, "vcs", "login")
	if r.code != 0 || !strings.Contains(r.out, "Ya tenés sesión iniciada en GitHub") || len(p.tty) != 0 {
		t.Fatalf("%+v tty=%v", r, p.tty)
	}
}

func TestLoginWithoutATerminalPrintsTheFixAndNeverPrompts(t *testing.T) {
	root := project(t)
	setOrigin(t, root, "git@github.com:acme/app.git")
	p := &platformFake{}
	withPlatform(t, p, nil)
	r := runIn(t, "", false, root, "vcs", "login")
	if r.code == 0 || !strings.Contains(r.err, "gh auth login") || !strings.Contains(r.err, "Sin terminal") || len(p.tty) != 0 {
		t.Fatalf("%+v tty=%v", r, p.tty)
	}
}

func TestLoginOnATerminalRunsTheToolsOwnLoginThenRechecks(t *testing.T) {
	root := project(t)
	setOrigin(t, root, "https://gitlab.com/acme/app.git")
	p := &platformFake{}
	withPlatform(t, p, nil)
	r := runIn(t, "", true, root, "vcs", "login")
	if r.code != 0 || !strings.Contains(r.out, "Listo, ya tenés sesión en GitLab") {
		t.Fatalf("%+v", r)
	}
	if len(p.tty) != 1 || p.tty[0] != "glab auth login" {
		t.Fatalf("tty = %v", p.tty)
	}
	// Two `glab auth status`: before the login and the re-check after it.
	n := 0
	for _, c := range p.calls {
		if c == "glab auth status" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("calls = %v", p.calls)
	}
}

func TestLoginFailsWhenTheToolsLoginFailsOrDoesNotStick(t *testing.T) {
	root := project(t)
	setOrigin(t, root, "git@github.com:acme/app.git")
	p := &platformFake{loginErr: errors.New("cancelled")}
	withPlatform(t, p, nil)
	if r := runIn(t, "", true, root, "vcs", "login"); r.code == 0 || !strings.Contains(r.err, "gh auth login") {
		t.Fatalf("%+v", r)
	}
}

func TestLoginCheckOnlyVerifiesAndNeverLogsIn(t *testing.T) {
	root := project(t)
	setOrigin(t, root, "git@github.com:acme/app.git")
	p := &platformFake{}
	withPlatform(t, p, nil)
	r := runIn(t, "", true, root, "vcs", "login", "--check")
	if r.code == 0 || len(p.tty) != 0 || !strings.Contains(r.err, "gh auth login") {
		t.Fatalf("%+v tty=%v", r, p.tty)
	}
	p.loggedIn = true
	if r := runIn(t, "", true, root, "vcs", "login", "--check"); r.code != 0 {
		t.Fatalf("%+v", r)
	}
}

func TestLoginWithAMissingToolSaysHowToInstallIt(t *testing.T) {
	root := project(t)
	setOrigin(t, root, "git@github.com:acme/app.git")
	p := &platformFake{missing: true}
	withPlatform(t, p, nil)
	r := runIn(t, "", true, root, "vcs", "login")
	if r.code == 0 || !strings.Contains(r.err, "Falta la herramienta gh") || !strings.Contains(r.err, "https://cli.github.com/") || len(p.tty) != 0 {
		t.Fatalf("%+v", r)
	}
}

func TestLoginOnAnUnknownPlatformIsExplained(t *testing.T) {
	root := project(t)
	setOrigin(t, root, "ssh://git@git.example.org/app.git")
	withPlatform(t, &platformFake{}, nil)
	if r := runIn(t, "", true, root, "vcs", "login"); r.code == 0 || !strings.Contains(r.err, "GitHub, GitLab y Bitbucket Cloud") {
		t.Fatalf("%+v", r)
	}
}

// bitbucketAPI answers /2.0/user for one token only.
func bitbucketAPI(t *testing.T, token string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/2.0/user" || r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"message":"Bad credentials"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"username":"ana"}`)
	}))
	t.Cleanup(srv.Close)
	old := vcsAPIFn
	vcsAPIFn = func() string { return srv.URL }
	t.Cleanup(func() { vcsAPIFn = old })
	return srv
}

func TestLoginBitbucketVerifiesTheTokenStoresItAndSavesOnlyTheReference(t *testing.T) {
	t.Setenv("GABITOTL_USER", "ana")
	root := project(t)
	setOrigin(t, root, "git@bitbucket.org:acme/app.git")
	const token = "bb-token-123456"
	bitbucketAPI(t, token)
	home := withPlatform(t, &platformFake{}, map[string]string{"MY_BB": token})

	r := runIn(t, "", false, root, "vcs", "login", "--token-env", "MY_BB", "--store-token")
	if r.code != 0 || !strings.Contains(r.out, "Sesión en Bitbucket verificada con la variable MY_BB") || !strings.Contains(r.out, "permisos 0600") {
		t.Fatalf("%+v", r)
	}
	secrets := mcp.ClientSecretsPath(home)
	st, err := os.Stat(secrets)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("the secrets file must be 0600: %v %v", st, err)
	}
	if b, _ := os.ReadFile(secrets); !strings.Contains(string(b), "MY_BB="+token) {
		t.Fatalf("token not stored:\n%s", b)
	}
	cfg, err := gabitotl.LoadVCSFor(gabitotl.NewLayout(root), "ana")
	if err != nil || cfg.Auth != "env:MY_BB" {
		t.Fatalf("the reference is saved: %+v %v", cfg, err)
	}
	local, _ := gabitotl.NewLayout(root).LocalVCS("ana")
	shared := gabitotl.NewLayout(root).VCS()
	if b, _ := os.ReadFile(local); !strings.Contains(string(b), "auth: env:MY_BB") {
		t.Fatalf("auth lives in the personal file:\n%s", b)
	}
	for _, p := range []string{local, shared} {
		if b, _ := os.ReadFile(p); strings.Contains(string(b), token) {
			t.Fatalf("the token must never be written to %s", p)
		}
	}
	if strings.Contains(r.out+r.err, token) {
		t.Fatal("the token must never be printed")
	}
	// Later the variable is gone from the shell: the stored token still resolves.
	cred, err := vcs.ResolveCredential(cfg.Auth, mcp.Resolver{Getenv: func(string) string { return "" }, Home: home}.Lookup)
	if err != nil || cred.Secret != token {
		t.Fatalf("the stored token resolves without the variable: %v", err)
	}
}

func TestLoginBitbucketRejectedTokenSavesNothing(t *testing.T) {
	t.Setenv("GABITOTL_USER", "ana")
	root := project(t)
	setOrigin(t, root, "git@bitbucket.org:acme/app.git")
	bitbucketAPI(t, "the-right-token")
	home := withPlatform(t, &platformFake{}, map[string]string{"BITBUCKET_TOKEN": "a-wrong-token"})
	r := runIn(t, "", false, root, "vcs", "login", "--store-token")
	if r.code == 0 || !strings.Contains(r.err, "Bitbucket no aceptó el token de BITBUCKET_TOKEN") {
		t.Fatalf("%+v", r)
	}
	if _, err := os.Stat(mcp.ClientSecretsPath(home)); err == nil {
		t.Fatal("a rejected token is not stored")
	}
	if cfg, _ := gabitotl.LoadVCSFor(gabitotl.NewLayout(root), "ana"); cfg.Auth != "" {
		t.Fatalf("a rejected token saves no reference: %+v", cfg)
	}
	if strings.Contains(r.out+r.err, "a-wrong-token") {
		t.Fatal("the token must never be printed")
	}
}

func TestLoginBitbucketWithoutTheVariableExplainsWhatToDo(t *testing.T) {
	root := project(t)
	setOrigin(t, root, "git@bitbucket.org:acme/app.git")
	withPlatform(t, &platformFake{}, nil)
	r := runIn(t, "", false, root, "vcs", "login")
	if r.code == 0 || !strings.Contains(r.err, "BITBUCKET_TOKEN no está definida") || !strings.Contains(r.err, "export BITBUCKET_TOKEN=") {
		t.Fatalf("%+v", r)
	}
	if r := runIn(t, "", false, root, "vcs", "login", "--token-env", "1bad"); r.code == 0 || !strings.Contains(r.err, "no es un nombre de variable") {
		t.Fatalf("%+v", r)
	}
}

func TestLoginBitbucketInteractiveAsksTheNameAndWhetherToStore(t *testing.T) {
	t.Setenv("GABITOTL_USER", "ana")
	root := project(t)
	setOrigin(t, root, "https://bitbucket.org/acme/app.git")
	const token = "bb-token-abcdef"
	bitbucketAPI(t, token)
	home := withPlatform(t, &platformFake{}, map[string]string{"TEAM_BB": token})
	r := runIn(t, "TEAM_BB\ns\n", true, root, "vcs", "login")
	if r.code != 0 || !strings.Contains(r.out, "¿En qué variable de entorno está tu token de Bitbucket?") || !strings.Contains(r.out, "Guardé el token en") {
		t.Fatalf("%+v", r)
	}
	if b, _ := os.ReadFile(mcp.ClientSecretsPath(home)); !strings.Contains(string(b), "TEAM_BB="+token) {
		t.Fatalf("stored:\n%s", b)
	}
	// Asked once: with the reference saved, a second login asks nothing and --check changes nothing.
	r = runIn(t, "", true, root, "vcs", "login", "--check")
	if r.code != 0 || strings.Contains(r.out, "¿En qué variable") {
		t.Fatalf("%+v", r)
	}
}

func TestLoginBitbucketServerIsUnsupported(t *testing.T) {
	root := project(t)
	setOrigin(t, root, "ssh://git@bitbucket.acme.io:7999/p/app.git")
	// vcs.md names bitbucket explicitly: the host is what is wrong.
	if r := runIn(t, "", false, root, "vcs", "setup", "--vcs-provider=bitbucket", "--vcs-automation=ask"); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	withPlatform(t, &platformFake{}, map[string]string{"BITBUCKET_TOKEN": "t"})
	bitbucketAPI(t, "t")
	r := runIn(t, "", false, root, "vcs", "login")
	if r.code == 0 || !strings.Contains(r.err, "Server ni Data Center") {
		t.Fatalf("%+v", r)
	}
}

func TestStatusShowsTheAuthLine(t *testing.T) {
	root := project(t)
	setOrigin(t, root, "git@github.com:acme/app.git")
	p := &platformFake{}
	withPlatform(t, p, nil)
	if r := runIn(t, "", false, root, "vcs", "status"); !strings.Contains(r.out, "auth: not logged in; Log in with: gh auth login") {
		t.Fatalf("%s", r.out)
	}
	p.loggedIn = true
	if r := runIn(t, "", false, root, "vcs", "status"); !strings.Contains(r.out, "auth: ok (github)") {
		t.Fatalf("%s", r.out)
	}
	if r := runIn(t, "", false, root, "vcs", "status", "--json"); !strings.Contains(r.out, `"status": "logged-in"`) {
		t.Fatalf("%s", r.out)
	}
	p.missing = true
	if r := runIn(t, "", false, root, "vcs", "status"); !strings.Contains(r.out, "auth: tool missing; Install gh from https://cli.github.com/") {
		t.Fatalf("%s", r.out)
	}
}

func TestStatusAuthIsNotApplicableOffTheThreePlatforms(t *testing.T) {
	root := project(t) // no remote: local
	withPlatform(t, &platformFake{}, nil)
	if r := runIn(t, "", false, root, "vcs", "status"); !strings.Contains(r.out, "auth: n/a") {
		t.Fatalf("%s", r.out)
	}
}

func TestSetupAdoptsTheInferredPlatformUnlessYouChoseOne(t *testing.T) {
	t.Setenv("GABITOTL_USER", "ana")
	root := project(t)
	setOrigin(t, root, "https://gitlab.com/acme/app.git")
	l := gabitotl.NewLayout(root)
	if v, _ := gabitotl.LoadVCS(l); v.Provider != "git" {
		t.Fatalf("the project starts with the generic provider: %+v", v)
	}
	// A bare --yes changes nothing, not even the provider.
	if r := runIn(t, "", false, root, "vcs", "setup", "--yes"); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if v, _ := gabitotl.LoadVCS(l); v.Provider != "git" {
		t.Fatalf("--yes alone writes nothing: %+v", v)
	}
	r := runIn(t, "", false, root, "vcs", "setup", "--yes", "--vcs-automation=all")
	if v, _ := gabitotl.LoadVCS(l); r.code != 0 || v.Provider != "gitlab" || !strings.Contains(r.out, "Plataforma: gitlab") {
		t.Fatalf("the platform origin points at is adopted: %+v %+v", v, r)
	}

	// An explicit choice stays, even when origin disagrees.
	root2 := project(t)
	setOrigin(t, root2, "https://gitlab.com/acme/app.git")
	runIn(t, "", false, root2, "vcs", "setup", "--vcs-provider=github", "--vcs-automation=until-pr")
	runIn(t, "", false, root2, "vcs", "setup", "--vcs-automation=ask")
	if v, _ := gabitotl.LoadVCS(gabitotl.NewLayout(root2)); v.Provider != "github" {
		t.Fatalf("an explicit choice is never overwritten: %+v", v)
	}
	// Without a remote nothing is invented.
	root3 := project(t)
	runIn(t, "", false, root3, "vcs", "setup", "--yes", "--vcs-automation=all")
	if v, _ := gabitotl.LoadVCS(gabitotl.NewLayout(root3)); v.Provider != "git" {
		t.Fatalf("no remote, no adoption: %+v", v)
	}
}
