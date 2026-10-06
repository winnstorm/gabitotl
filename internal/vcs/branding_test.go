package vcs

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/branding"
	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

// Branding strings are built at runtime so this repository passes its own guard.
var (
	llm       = "Cla" + "ude"
	aiTrailer = "Co-Authored-By: " + llm + " Sonnet 5.5 <noreply@" + "anthropic" + ".com>"
	aiFooter  = "🤖 " + "Generated " + "with [" + llm + " Code](https://" + "claude.com/claude-" + "code)"
)

func TestCommitStripsAttribution(t *testing.T) {
	r := newRepo(t)
	r.Branding.Mode = gabitotl.GuardStrip
	var op string
	var removed []branding.Finding
	r.OnBranding = func(o string, rem, _ []branding.Finding) { op, removed = o, rem }
	write(t, r.Dir, "b.txt", "b")
	msg := "feat(b): add b file\n\nWhy it matters.\n\n" + aiFooter + "\n\n" + aiTrailer + "\n"
	if _, err := r.Commit(context.Background(), msg, "b.txt"); err != nil {
		t.Fatal(err)
	}
	body, _ := r.Git(context.Background(), "log", "-1", "--format=%B")
	if strings.TrimSpace(body) != "feat(b): add b file\n\nWhy it matters." {
		t.Fatalf("commit body = %q", body)
	}
	if op != OpCommit || len(removed) != 2 {
		t.Fatalf("what was removed must be reported, got op=%q removed=%+v", op, removed)
	}
}

func TestCommitBlockMode(t *testing.T) {
	r := newRepo(t)
	r.Branding.Mode = gabitotl.GuardBlock
	write(t, r.Dir, "b.txt", "b")
	_, err := r.Commit(context.Background(), "feat: b\n\n"+aiTrailer, "b.txt")
	be, ok := IsBranding(err)
	if !ok || !errors.Is(err, ErrAttribution) || len(be.Findings) != 1 {
		t.Fatalf("want BrandingError, got %v", err)
	}
	head, _ := r.Git(context.Background(), "log", "--format=%s")
	if strings.Contains(head, "feat: b") {
		t.Fatal("nothing must be committed")
	}
}

func TestCommitStripRefusesWhenLineHasContent(t *testing.T) {
	r := newRepo(t)
	r.Branding.Mode = gabitotl.GuardStrip
	write(t, r.Dir, "b.txt", "b")
	_, err := r.Commit(context.Background(), "feat: b\n\nThanks, generated "+"with "+llm, "b.txt")
	if _, ok := IsBranding(err); !ok {
		t.Fatalf("a line mixing content and attribution cannot be stripped safely: %v", err)
	}
}

func TestCommitWarnAndOffModes(t *testing.T) {
	for _, mode := range []string{gabitotl.GuardWarn, gabitotl.GuardOff} {
		r := newRepo(t)
		r.Branding.Mode = mode
		var warned []branding.Finding
		r.OnBranding = func(_ string, _, w []branding.Finding) { warned = w }
		write(t, r.Dir, "b.txt", "b")
		// Text only the new detector knows about (the legacy trailer rule never relaxes).
		msg := "feat: b\n\nAssisted by " + llm
		if _, err := r.Commit(context.Background(), msg, "b.txt"); err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		if (mode == gabitotl.GuardWarn) != (len(warned) == 1) {
			t.Fatalf("%s: warned = %+v", mode, warned)
		}
	}
}

func TestCommitExtraTerms(t *testing.T) {
	r := newRepo(t)
	r.Branding.Mode = gabitotl.GuardStrip
	write(t, r.Dir, "b.txt", "b")
	msg := "feat: b\n\nCreated with AcmeLLM"
	if _, err := r.Commit(context.Background(), msg, "b.txt"); err != nil {
		t.Fatalf("unknown brand passes by default: %v", err)
	}
	write(t, r.Dir, "c.txt", "c")
	r.Branding.ExtraTerms = []string{"acmellm"}
	if _, err := r.Commit(context.Background(), msg, "c.txt"); err != nil {
		t.Fatal(err)
	}
	body, _ := r.Git(context.Background(), "log", "-1", "--format=%B")
	if strings.Contains(body, "AcmeLLM") {
		t.Fatalf("extra term must be stripped: %q", body)
	}
}

func TestPolicyFromWorkflow(t *testing.T) {
	wf := gabitotl.DefaultWorkflow()
	wf.BrandingGuard, wf.BrandingExtraTerms = gabitotl.GuardBlock, []string{"x"}
	p := PolicyFromWorkflow(wf)
	if p.Mode != gabitotl.GuardBlock || len(p.ExtraTerms) != 1 {
		t.Fatalf("policy = %+v", p)
	}
	if (BrandingPolicy{}).mode() != gabitotl.GuardBlock {
		t.Fatal("zero policy must keep the library behavior: refuse")
	}
}

func TestOpenPRStripsAndBlocks(t *testing.T) {
	fr := &fakeRunner{out: "https://github.com/acme/app/pull/9"}
	r := &Repo{Dir: t.TempDir(), Config: allowAll(), Branding: BrandingPolicy{Mode: gabitotl.GuardStrip}}
	var seen string
	r.OnBranding = func(op string, rem, _ []branding.Finding) { seen = op + ":" + string(rune('0'+len(rem))) }
	prov, _ := ProviderFor("github", fr)
	if _, err := r.OpenPR(context.Background(), prov, PRRequest{Title: "feat: login", Body: "Adds login.\n\n" + aiFooter}); err != nil {
		t.Fatal(err)
	}
	call := strings.Join(fr.calls[0], " ")
	if strings.Contains(call, llm) || !strings.Contains(call, "--body Adds login.") || seen != OpPullRequest+":1" {
		t.Fatalf("call = %q seen = %q", call, seen)
	}
	r.Branding.Mode = gabitotl.GuardBlock
	fr.calls = nil
	if _, err := r.OpenPR(context.Background(), prov, PRRequest{Title: "feat: x", Body: aiFooter}); !errors.Is(err, ErrAttribution) {
		t.Fatalf("block mode must refuse, got %v", err)
	}
	if len(fr.calls) != 0 {
		t.Fatal("gh must not run when the guard refuses")
	}
}
