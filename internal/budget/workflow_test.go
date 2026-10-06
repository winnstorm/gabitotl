package budget

import (
	"testing"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
)

func TestFromWorkflow(t *testing.T) {
	if c := FromWorkflow(gabitotl.DefaultWorkflow()); c != DefaultConfig() {
		t.Fatalf("%+v", c)
	}
	if c := FromWorkflow(gabitotl.WorkflowConfig{}); c != DefaultConfig() { // pre-budget configs
		t.Fatalf("zero values must fall back to defaults: %+v", c)
	}
	w := gabitotl.DefaultWorkflow()
	w.ContextLimitTokens, w.ContextThreshold = 1000, 0.5
	if c := FromWorkflow(w); c.Limit != 1000 || c.Threshold != 0.5 || c.Validate() != nil {
		t.Fatalf("%+v", c)
	}
}
