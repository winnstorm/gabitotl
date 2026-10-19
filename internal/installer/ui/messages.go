// Package ui is the presentation of the installer: Rioplatense Spanish or
// English wording (one embedded, replaceable messages file per persona language), state symbols and renderers. It
// holds no installer logic; package installer returns typed results and this
// package only formats them.
package ui

import (
	"bufio"
	_ "embed"
	"fmt"
	"sort"
	"strings"

	"github.com/winnstorm/gabitotl/internal/gabitotl"
	"github.com/winnstorm/gabitotl/internal/persona"
)

//go:embed messages_es-AR.txt
var defaultMessages string

//go:embed messages_en.txt
var englishMessages string

// State symbols (installer spec section 50).
const (
	SymOK    = "✓" // ready
	SymWarn  = "!" // needs attention
	SymTodo  = "→" // will be configured
	SymError = "×" // error
)

// Messages holds the replaceable user-facing templates.
type Messages map[string]string

func parse(src string) (Messages, error) {
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
	return m, nil
}

// ParseMessages reads "key = value" lines (blank lines and # comments ignored)
// and verifies every key of the embedded es-AR file is present, so a
// replacement file can never leave a gap.
func ParseMessages(src string) (Messages, error) {
	m, err := parse(src)
	if err != nil {
		return nil, err
	}
	def := Default()
	var missing []string
	for k := range def {
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
	m, err := parse(defaultMessages)
	if err != nil {
		panic(err) // the embedded file is covered by tests
	}
	return m
}

// For returns the embedded messages of the persona language lang (es-AR for an
// unknown one). It is the same catalogue mechanism, one file per language.
func For(lang persona.Lang) Messages {
	src := defaultMessages
	if lang == persona.LangEN {
		src = englishMessages
	}
	m, err := parse(src)
	if err != nil {
		panic(err) // the embedded files are covered by tests
	}
	return m
}

// Active returns the messages in the persona language the person running the
// command chose at install (home empty means os.UserHomeDir), else es-AR.
func Active(home string) Messages { return For(gabitotl.ActivePersonaLang(home)) }

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

// Keys referenced directly by renderers (the rest are built from codes).
const (
	KeyPlanTitle   = "plan.title"
	KeyPlanImport  = "plan.import.count"
	KeyPlanEmpty   = "plan.empty"
	KeyPlanDryDone = "plan.dry_run_done"
)
