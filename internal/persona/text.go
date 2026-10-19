package persona

import (
	_ "embed"
	"regexp"
)

//go:embed persona.md
var personaTemplate string

var placeholder = regexp.MustCompile(`\{\{([a-z._]+)\}\}`)

// Text returns the compact persona (system prompt) that runtime adapters
// install, with the user-facing wording taken from msgs.
func Text(msgs Messages) string {
	return placeholder.ReplaceAllStringFunc(personaTemplate, func(m string) string {
		if v, ok := msgs[placeholder.FindStringSubmatch(m)[1]]; ok {
			return v
		}
		return m
	})
}

// DefaultText is Text with the embedded es-AR templates.
func DefaultText() string { return Text(DefaultMessages()) }

// TextFor is Text with the embedded templates of lang.
func TextFor(lang Lang) string { return Text(MessagesFor(lang)) }
