package persona

// Risk holds the signals of spec section 19 that make Gabito ask before
// working. Each field is a fact the caller has established, not a guess.
type Risk struct {
	DataLoss                      bool // risk of losing data
	HardToRevert                  bool // the decision is difficult to undo
	ImpactfulAlternatives         bool // options with materially different impact
	Security                      bool // security is involved
	UnclearIntent                 bool // the real goal is not understood, or two very different readings exist
	ContradictsDecision           bool // the request contradicts a previous decision
	WrongDecisionForLackOfContext bool // the user may be deciding wrongly for lack of context
}

// AskDecision is the outcome of the question policy.
type AskDecision struct {
	Ask     bool
	Reasons []string // stable identifiers of the signals that fired
}

// ShouldAsk applies the question policy: any risk signal means ask before
// modifying anything; no signal means Gabito decides alone (minor detail,
// clear project convention, decision already in memory, or a standard and
// reversible option) and records the decision in memory if it may matter later.
func ShouldAsk(r Risk) AskDecision {
	var d AskDecision
	add := func(on bool, reason string) {
		if on {
			d.Reasons = append(d.Reasons, reason)
		}
	}
	add(r.DataLoss, "data-loss")
	add(r.HardToRevert, "hard-to-revert")
	add(r.ImpactfulAlternatives, "impactful-alternatives")
	add(r.Security, "security")
	add(r.UnclearIntent, "unclear-intent")
	add(r.ContradictsDecision, "contradicts-previous-decision")
	add(r.WrongDecisionForLackOfContext, "wrong-decision-for-lack-of-context")
	d.Ask = len(d.Reasons) > 0
	return d
}

// DecideAlone is the complement of ShouldAsk.
func DecideAlone(r Risk) bool { return !ShouldAsk(r).Ask }
