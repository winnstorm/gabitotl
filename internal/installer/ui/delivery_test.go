package ui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

func askDelivery(in string, a gabitotl.DeliveryAnswers, n Need) (gabitotl.DeliveryAnswers, string) {
	var out bytes.Buffer
	NewPrompter(strings.NewReader(in), &out, Default()).Delivery(&a, n)
	return a, out.String()
}

var fullNeed = Need{Delivery: true, DeliveryFull: true}

func TestDeliveryDefaultsAreTheMiddleOptionAndNothingRisky(t *testing.T) {
	// Platform "none yet", automation until the PR, normal PRs.
	a, out := askDelivery("\n\n\n", gabitotl.DeliveryAnswers{}, fullNeed)
	if a.Provider != "" || a.Automation != gabitotl.AutomationUntilPR || a.Draft == nil || *a.Draft {
		t.Fatalf("%+v", a)
	}
	if a.MergeMethod != "" || a.DeleteBranch != nil {
		t.Fatalf("merge details are only asked when merge is automatic: %+v", a)
	}
	if strings.Contains(out, "squash") || strings.Contains(out, "Squash") {
		t.Fatalf("no merge question when the person merges:\n%s", out)
	}
}

func TestDeliveryPlatformOffersTheInferredOneFirst(t *testing.T) {
	a, out := askDelivery("\n2\n\n", gabitotl.DeliveryAnswers{}, Need{Delivery: true, DeliveryFull: true, Inferred: "gitlab"})
	if a.Provider != "gitlab" || a.Automation != gabitotl.AutomationUntilPR {
		t.Fatalf("the inferred platform is the default: %+v", a)
	}
	if !strings.Contains(out, "Tu repositorio está en GitLab") || !strings.Contains(out, "[2]") {
		t.Fatalf("default highlighted on the found platform:\n%s", out)
	}
	a, _ = askDelivery("4\n\n\n", gabitotl.DeliveryAnswers{}, Need{Delivery: true, DeliveryFull: true, Inferred: "gitlab"})
	if a.Provider != "" {
		t.Fatalf("choosing none keeps the provider that is already set: %+v", a)
	}
}

func TestDeliveryEverythingAutomaticAsksMergeMethodBranchAndDraft(t *testing.T) {
	// platform GitHub | all automatic | rebase | keep the branch | draft PRs
	a, out := askDelivery("1\n1\n3\n2\n2\n", gabitotl.DeliveryAnswers{}, fullNeed)
	if a.Provider != "github" || a.Automation != gabitotl.AutomationAll || a.MergeMethod != gabitotl.MergeRebase {
		t.Fatalf("%+v", a)
	}
	if a.DeleteBranch == nil || *a.DeleteBranch || a.Draft == nil || !*a.Draft {
		t.Fatalf("%+v", a)
	}
	if n := strings.Count(out, "Elegí una opción"); n != 5 {
		t.Fatalf("exactly five questions, got %d:\n%s", n, out)
	}
}

func TestDeliveryAskEveryTimeNeedsNoMoreQuestions(t *testing.T) {
	a, out := askDelivery("4\n3\n", gabitotl.DeliveryAnswers{}, fullNeed)
	if a.Automation != gabitotl.AutomationAsk || a.Draft != nil || a.MergeMethod != "" {
		t.Fatalf("%+v", a)
	}
	if strings.Count(out, "Elegí una opción") != 2 {
		t.Fatalf("two questions:\n%s", out)
	}
}

func TestDeliveryNeverAsksWhatAFlagAnswered(t *testing.T) {
	yes := true
	given := gabitotl.DeliveryAnswers{Provider: "github", Automation: gabitotl.AutomationAll, MergeMethod: gabitotl.MergeSquash, DeleteBranch: &yes, Draft: &yes}
	a, out := askDelivery("", given, fullNeed)
	if out != "" || a.Automation != gabitotl.AutomationAll {
		t.Fatalf("nothing left to ask:\n%s", out)
	}
	_, out = askDelivery("", gabitotl.DeliveryAnswers{}, Need{})
	if out != "" {
		t.Fatalf("a configured project is never asked:\n%s", out)
	}
}

func TestDeliveryOnAConfiguredProjectAsksOnlyThePersonsAutomation(t *testing.T) {
	a, out := askDelivery("3\n", gabitotl.DeliveryAnswers{}, Need{Delivery: true})
	if a.Automation != gabitotl.AutomationAsk || a.Provider != "" || a.Draft != nil {
		t.Fatalf("%+v", a)
	}
	if strings.Count(out, "Elegí una opción") != 1 || !strings.Contains(out, "no se comparte") {
		t.Fatalf("one question and the note that it stays personal:\n%s", out)
	}
}

func TestDeliveryEndedInputNeverSettlesTheQuestions(t *testing.T) {
	a, _ := askDelivery("", gabitotl.DeliveryAnswers{}, fullNeed)
	if !a.Empty() {
		t.Fatalf("no input, no answers: %+v", a)
	}
	a, _ = askDelivery("2\n", gabitotl.DeliveryAnswers{}, fullNeed)
	if a.Provider != "gitlab" || a.Automation != "" {
		t.Fatalf("the automation stays unanswered, so the project is not marked configured: %+v", a)
	}
}

func TestDeliveryNeverMentionsPRSizeOrExceptions(t *testing.T) {
	forbidden := []string{"exce", "tamaño", "tamano", "presupuesto", "líneas", "lineas", "encaden", "apilad", "budget", "size", "chain", "stack", "400"}
	for k, v := range Default() {
		if !strings.HasPrefix(k, "ask.vcs") && !strings.HasPrefix(k, "vcs.") {
			continue
		}
		low := strings.ToLower(k + " " + v)
		for _, w := range forbidden {
			if strings.Contains(low, w) {
				t.Errorf("%s mentions %q: %s", k, w, v)
			}
		}
	}
	_, out := askDelivery("1\n1\n1\n1\n1\n", gabitotl.DeliveryAnswers{}, fullNeed)
	low := strings.ToLower(out)
	for _, w := range forbidden {
		if strings.Contains(low, w) {
			t.Errorf("the prompts mention %q:\n%s", w, out)
		}
	}
}

func TestNextDeliveryStepOrder(t *testing.T) {
	var done []DeliveryStep
	var a gabitotl.DeliveryAnswers
	var got []DeliveryStep
	for {
		s := NextDeliveryStep(fullNeed, a, done)
		if s == "" {
			break
		}
		got = append(got, s)
		done = append(done, s)
		switch s {
		case StepAutomation:
			ApplyDeliveryAnswer(&a, s, gabitotl.AutomationAll)
		default:
			ApplyDeliveryAnswer(&a, s, "no")
		}
	}
	want := []DeliveryStep{StepPlatform, StepAutomation, StepMerge, StepDelete, StepDraft}
	if len(got) != len(want) {
		t.Fatalf("%v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%v", got)
		}
	}
}
