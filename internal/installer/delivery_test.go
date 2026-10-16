package installer

import (
	"context"
	"testing"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

func gitLines(t *testing.T, f *fixture) map[string]string {
	t.Helper()
	d := Doctor(context.Background(), f.env)
	if d.Errors() != 0 {
		t.Fatalf("delivery findings are never failures: %+v", d)
	}
	return lineCodes(d, "git")
}

func TestApplyStoresTheDeliveryAnswersAndDoctorReportsThem(t *testing.T) {
	f := newFixture(t)
	installed(t, f, Options{User: "gabi", UserName: "Gabi", Delivery: gabitotl.DeliveryAnswers{
		Automation: gabitotl.AutomationAll, MergeMethod: gabitotl.MergeRebase,
	}})
	l := gabitotl.NewLayout(f.root)
	c, err := gabitotl.LoadVCS(l)
	if err != nil || !c.Configured || c.AutomationLevel() != gabitotl.AutomationAll || c.MergeMethod != gabitotl.MergeRebase {
		t.Fatalf("%+v %v", c, err)
	}
	lines := gitLines(t, f)
	if lines[DocGitDelivery] != LineOK || lines[DocGitDeliveryTodo] != "" {
		t.Fatalf("configured: %v", lines)
	}
}

func TestDoctorPointsAtVCSSetupUntilTheQuestionsAreAnswered(t *testing.T) {
	f := newFixture(t)
	installed(t, f, Options{User: "gabi", UserName: "Gabi"})
	lines := gitLines(t, f)
	if lines[DocGitDeliveryTodo] != LineWarn || lines[DocGitDelivery] != "" {
		t.Fatalf("not configured yet is a warning: %v", lines)
	}
	if _, err := gabitotl.SaveDelivery(gabitotl.NewLayout(f.root), gabitotl.ActiveUserID(), gabitotl.DeliveryAnswers{Automation: gabitotl.AutomationAsk}); err != nil {
		t.Fatal(err)
	}
	lines = gitLines(t, f)
	if lines[DocGitDelivery] != LineOK || lines[DocGitDeliveryTodo] != "" {
		t.Fatalf("answering ask settles it: %v", lines)
	}
}

func TestDoctorWarnsWhenTheConfiguredPlatformDiffersFromOrigin(t *testing.T) {
	f := newFixture(t) // origin is on GitHub
	installed(t, f, Options{User: "gabi", UserName: "Gabi"})
	if lines := gitLines(t, f); lines[DocGitProviderDiffer] != "" {
		t.Fatalf("vcs.md was created from origin, so they agree: %v", lines)
	}
	if _, err := gabitotl.SaveDelivery(gabitotl.NewLayout(f.root), gabitotl.ActiveUserID(), gabitotl.DeliveryAnswers{Provider: "gitlab"}); err != nil {
		t.Fatal(err)
	}
	if lines := gitLines(t, f); lines[DocGitProviderDiffer] != LineWarn {
		t.Fatalf("a mismatch is a warning: %v", lines)
	}
}
