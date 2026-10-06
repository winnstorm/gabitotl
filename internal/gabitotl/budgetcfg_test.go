package gabitotl

import (
	"os"
	"strings"
	"testing"
)

func TestWorkflowBudgetKeysDefaultAndRoundTrip(t *testing.T) {
	root := t.TempDir()
	l := NewLayout(root)
	// A workflow.md written before the budget keys existed keeps working.
	old := "---\noutput: compact\nlanguage: es-AR\npersona: gabito\nbackup_before_write: true\nmemory_before_work: true\nmemory_after_work: true\nbackup:\n  max_snapshots: 20\n  max_age_days: 7\n---\n\n# Workflow\n"
	if err := os.MkdirAll(l.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.Workflow(), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := LoadWorkflow(l)
	if err != nil || c.ContextLimitTokens != 200000 || c.ContextThreshold != 0.75 {
		t.Fatalf("%v %+v", err, c)
	}
	c.ContextLimitTokens, c.ContextThreshold = 100000, 0.5
	if err := SaveWorkflow(l, c); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(l.Workflow())
	if !strings.Contains(string(b), "context_limit_tokens: 100000") || !strings.Contains(string(b), "# Workflow") {
		t.Fatalf("%s", b)
	}
	got, _ := LoadWorkflow(l)
	if got.ContextLimitTokens != 100000 || got.ContextThreshold != 0.5 {
		t.Fatalf("%+v", got)
	}
	for _, bad := range []func(*WorkflowConfig){
		func(c *WorkflowConfig) { c.ContextLimitTokens = 0 },
		func(c *WorkflowConfig) { c.ContextThreshold = 1.5 },
	} {
		w := DefaultWorkflow()
		bad(&w)
		if w.Validate() == nil {
			t.Fatal("invalid budget accepted")
		}
	}
}
