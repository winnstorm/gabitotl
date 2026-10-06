// Package ui is the presentation of the installer: Rioplatense Spanish wording
// (one embedded, replaceable messages file), state symbols and renderers. It
// holds no installer logic; package installer returns typed results and this
// package only formats them.
package ui

import (
	"bufio"
	_ "embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed messages_es-AR.txt
var defaultMessages string

// State symbols (installer spec section 50).
const (
	SymOK    = "✓" // ready
	SymWarn  = "!" // needs attention
	SymTodo  = "→" // will be configured
	SymError = "×" // error
)

// Messages holds the replaceable user-facing templates.
type Messages map[string]string

// ParseMessages reads "key = value" lines (blank lines and # comments ignored)
// and verifies every key in RequiredKeys is present.
func ParseMessages(src string) (Messages, error) {
	m := Messages{}
	sc := bufio.NewScanner(strings.NewReader(src))
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("messages: line %d: expected key = value", n)
		}
		m[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	var missing []string
	for _, k := range RequiredKeys {
		if m[k] == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf("messages: missing keys: %s", strings.Join(missing, ", "))
	}
	return m, nil
}

// Default returns the embedded es-AR messages.
func Default() Messages {
	m, err := ParseMessages(defaultMessages)
	if err != nil {
		panic(err) // the embedded file is covered by tests
	}
	return m
}

// Format returns the template for key with {name} placeholders replaced by the
// given name/value pairs. An unknown key yields the key itself, so a gap in a
// replacement file is visible instead of silent.
func (m Messages) Format(key string, pairs ...string) string {
	s, ok := m[key]
	if !ok {
		return key
	}
	for i := 0; i+1 < len(pairs); i += 2 {
		s = strings.ReplaceAll(s, "{"+pairs[i]+"}", pairs[i+1])
	}
	return s
}

// Keys used by the renderers; RequiredKeys lists them for validation.
const (
	KeyPlanTitle   = "plan.title"
	KeyPlanImport  = "plan.import.count"
	KeyPlanEmpty   = "plan.empty"
	KeyPlanDryDone = "plan.dry_run_done"
)

// RequiredKeys must exist in any replacement messages file.
var RequiredKeys = []string{KeyPlanTitle, KeyPlanImport, KeyPlanEmpty, KeyPlanDryDone}
