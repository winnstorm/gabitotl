package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/credstore"
	"github.com/winnstorm/gabitotl/internal/gabitotl"
	rt "github.com/winnstorm/gabitotl/internal/runtime"
	"github.com/winnstorm/gabitotl/internal/vcs"
)

func TestLoadVCSOrDefaultLayersGlobalUnderProject(t *testing.T) {
	root := t.TempDir() // no .gabitotl yet
	home := withPlatform(t, &platformFake{}, nil)
	f := false
	if _, err := gabitotl.SaveGlobalDelivery(home, gabitotl.DeliveryAnswers{Automation: gabitotl.AutomationUntilPR, MergeMethod: gabitotl.MergeRebase, Draft: &f}); err != nil {
		t.Fatal(err)
	}
	l := gabitotl.NewLayout(root)

	// No project vcs.md: the global defaults apply.
	cfg, err := loadVCSOrDefault(l, "")
	if err != nil || cfg.MergeMethod != gabitotl.MergeRebase || cfg.AutomationLevel() != gabitotl.AutomationUntilPR {
		t.Fatalf("global only: %+v %v", cfg, err)
	}

	// A project that already has vcs.md keeps winning for what it sets.
	if err := os.MkdirAll(l.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.VCS(), []byte("---\nprovider: gitlab\nmerge_method: merge\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = loadVCSOrDefault(l, "")
	if err != nil || cfg.MergeMethod != gabitotl.MergeMerge || cfg.Provider != "gitlab" || cfg.AutomationLevel() != gabitotl.AutomationUntilPR {
		t.Fatalf("project over global: %+v %v", cfg, err)
	}
}

func TestCredentialLookupOrderEnvThenReferenceThenStore(t *testing.T) {
	root := project(t)
	withPlatform(t, &platformFake{}, map[string]string{"FROM_ENV": "env-token"})
	store := fakeCreds(t)
	if err := store.Set("bitbucket.org", "stored-token"); err != nil {
		t.Fatal(err)
	}
	cfg := gabitotl.DefaultVCS("bitbucket")

	cfg.Auth = "env:FROM_ENV"
	if c, err := credentialFor(root, cfg); err != nil || c.Secret != "env-token" {
		t.Fatalf("the environment reference wins over the store: %+v %v", c, err)
	}
	cfg.Auth = "env:NOT_SET"
	if c, err := credentialFor(root, cfg); err != nil || c.Secret != "stored-token" {
		t.Fatalf("an unset reference falls back to the store: %+v %v", c, err)
	}
	if err := store.Delete("bitbucket.org"); err != nil {
		t.Fatal(err)
	}
	if _, err := credentialFor(root, cfg); err == nil || strings.Contains(err.Error(), "stored-token") {
		t.Fatalf("nothing anywhere is a not-logged-in error: %v", err)
	}
}

func TestStoreTokenWithNoHomeFailsInsteadOfWritingUnderTheWorkingDirectory(t *testing.T) {
	wd := t.TempDir()
	t.Chdir(wd)
	withPlatform(t, &platformFake{}, nil)
	oldEnv := runtimeEnvFn
	runtimeEnvFn = func(string) (rt.Env, error) { return rt.Env{}, nil } // unknown home
	old := credStoreFn
	credStoreFn = func(home string) credstore.Store { return credstore.New(home) }
	t.Cleanup(func() { credStoreFn, runtimeEnvFn = old, oldEnv })
	if err := credStoreFn("").Set("bitbucket.org", "tok"); err == nil {
		t.Fatal("an empty home must be refused")
	}
	if tok, ok := vcs.DefaultTokens("github.com"); ok || tok != "" {
		t.Fatal("no token without a home")
	}
	if entries, _ := os.ReadDir(wd); len(entries) != 0 {
		t.Fatalf("wrote under cwd: %v", entries)
	}
}
