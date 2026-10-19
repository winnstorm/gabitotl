package persona

import (
	"fmt"
	"strings"
)

// Mode is how short Gabito's chat replies are. It never touches artifacts.
type Mode string

// The chat concision modes.
const (
	ModeNormal Mode = "normal"
	ModeLite   Mode = "lite"
	ModeFull   Mode = "full"
)

// Modes lists every mode, from the default to the shortest.
var Modes = []Mode{ModeNormal, ModeLite, ModeFull}

// ParseMode validates a user-provided mode name (case and surrounding spaces ignored).
func ParseMode(s string) (Mode, error) {
	m := Mode(strings.ToLower(strings.TrimSpace(s)))
	for _, k := range Modes {
		if m == k {
			return m, nil
		}
	}
	return "", fmt.Errorf("unknown persona mode %q (want one of %s)", s, modeNames())
}

func modeNames() string {
	names := make([]string, len(Modes))
	for i, m := range Modes {
		names[i] = string(m)
	}
	return strings.Join(names, ", ")
}

const rulesHeader = `The mode applies ONLY to what you say in chat. Never apply it to code, comments, commit messages, PR text, docs, memory entries or UI strings.
Always write full, clear sentences for: security warnings, confirmations of destructive or hard-to-revert actions, and questions to the user.
Quote error messages verbatim.
Keep the Gabito voice (Argentine Spanish, voseo) and the block format (Qué hice / Progreso / Qué sigue, OJO, TIP): only the wording gets shorter.
`

var modeRules = map[Mode]string{
	ModeNormal: "Mode normal: no change, follow the persona defaults.\n",
	ModeLite: `Mode lite:
- Drop filler, greetings, restating the question and closing offers.
- One idea per sentence.
- No hedging.
- Keep articles and grammar intact.
`,
	ModeFull: `Mode full (telegraphic):
- Fragments are allowed. No filler or pleasantries.
- Drop articles and connectors where the meaning stays clear.
- Prefer symbols (→, =) and short words.
- One line per fact.
- Still readable by a human.
`,
}

// Rules returns the instructions the agent follows in chat for m. An unknown
// mode gets the rules of ModeNormal.
func Rules(m Mode) string {
	r, ok := modeRules[m]
	if !ok {
		r = modeRules[ModeNormal]
	}
	return rulesHeader + r
}
