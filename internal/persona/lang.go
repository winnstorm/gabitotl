package persona

import (
	"fmt"
	"strings"
)

// Lang is the language of Gabito's chat voice. It never touches artifacts.
type Lang string

// The supported persona languages.
const (
	LangES Lang = "es" // Argentine Spanish (es-AR), the default
	LangEN Lang = "en" // plain English
)

// Langs lists every language, default first.
var Langs = []Lang{LangES, LangEN}

// ParseLang validates a user-provided language (case and spaces ignored). "es-AR"
// and "en-US" style tags are accepted for convenience.
func ParseLang(s string) (Lang, error) {
	v := strings.ToLower(strings.TrimSpace(s))
	if base, _, ok := strings.Cut(v, "-"); ok {
		v = base
	}
	for _, l := range Langs {
		if Lang(v) == l {
			return l, nil
		}
	}
	return "", fmt.Errorf("unknown persona language %q (want es or en)", s)
}

// normLang maps an unknown language to the default.
func normLang(l Lang) Lang {
	if l == LangEN {
		return LangEN
	}
	return LangES
}
