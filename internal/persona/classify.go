package persona

import (
	"strings"
	"unicode"
)

// Kind is the internal work type of a request (spec section 12). It is never
// shown to the user and never asked for.
type Kind string

// Work kinds.
const (
	NewProject    Kind = "new-project"
	Feature       Kind = "feature"
	Bugfix        Kind = "bugfix"
	Refactor      Kind = "refactor"
	Maintenance   Kind = "maintenance"
	Configuration Kind = "configuration"
	Research      Kind = "research"
)

var accents = strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n")

// rule maps keywords to a kind. A keyword without a space matches a word that
// starts with it (a stem); a keyword with a space matches as a phrase. Rules
// are tried in order and the first match wins, so more specific kinds come first.
type rule struct {
	kind     Kind
	keywords []string
}

var rules = []rule{
	{NewProject, []string{"nuevo proyecto", "proyecto nuevo", "new project", "desde cero", "from scratch", "scaffold", "bootstrap", "crear un proyecto", "create a project", "arrancar un proyecto"}},
	{Bugfix, []string{"bug", "error", "falla", "fallo", "fix", "arregl", "corregi", "corrige", "corregir", "no funciona", "se rompe", "roto", "crash", "excepcion", "exception", "regresion", "regression", "broken"}},
	{Refactor, []string{"refactor", "reorganiz", "reestructur", "simplific", "extraer", "extract", "renombr", "rename", "desacopl", "limpiar el codigo", "clean up"}},
	{Research, []string{"investig", "research", "analiz", "analyze", "explica", "explain", "como funciona", "how does", "por que", "why does", "compar", "que opinas", "que te parece", "diferencia entre", "audit"}},
	{Configuration, []string{"config", "instal", "install", "setup", "pipeline", "deploy", "variable de entorno", "env var", "dockerfile", "linter", "workflow de", "settings"}},
	{Maintenance, []string{"actualiz", "update", "upgrade", "bump", "dependenc", "deprec", "mantenimiento", "maintenance", "limpieza", "migrar a la version", "vulnerab"}},
	{Feature, []string{"agreg", "anad", "add", "implement", "nueva funcionalidad", "new feature", "feature", "soporte para", "support for", "crear", "create", "permitir", "habilitar"}},
}

// Classify returns the work kind of a request with simple deterministic
// heuristics (Spanish and English keywords, no model call). Unknown requests
// that read as a question are research; anything else is a feature.
func Classify(request string) Kind {
	norm := accents.Replace(strings.ToLower(request))
	words := strings.FieldsFunc(norm, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	padded := " " + strings.Join(words, " ") + " "
	for _, r := range rules {
		for _, kw := range r.keywords {
			if strings.Contains(kw, " ") {
				if strings.Contains(padded, " "+kw+" ") || strings.Contains(padded, " "+kw) {
					return r.kind
				}
				continue
			}
			for _, w := range words {
				if strings.HasPrefix(w, kw) {
					return r.kind
				}
			}
		}
	}
	if strings.Contains(request, "?") {
		return Research
	}
	return Feature
}
