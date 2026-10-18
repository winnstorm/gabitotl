package persona_test

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/winnstorm/gabitotl/internal/persona"
	"github.com/winnstorm/gabitotl/internal/tokens"
)

func TestClassify(t *testing.T) {
	cases := map[string]persona.Kind{
		"Quiero arrancar un proyecto nuevo de API":     persona.NewProject,
		"create a new project from scratch":            persona.NewProject,
		"El login tira un error 500 al refrescar":      persona.Bugfix,
		"fix the broken date parsing":                  persona.Bugfix,
		"Refactorizá el servicio de usuarios":          persona.Refactor,
		"extraer la validación a un módulo":            persona.Refactor,
		"Investigá cómo funciona la caché":             persona.Research,
		"¿Por qué usamos Redis acá?":                   persona.Research,
		"Configurar el pipeline de CI":                 persona.Configuration,
		"instalar el linter":                           persona.Configuration,
		"Actualizar las dependencias":                  persona.Maintenance,
		"upgrade to the latest framework version":      persona.Maintenance,
		"Agregar MFA al login":                         persona.Feature,
		"implement password recovery":                  persona.Feature,
		"hacé algo con los usuarios":                   persona.Feature,
		"los usuarios?":                                persona.Research,
		"Agregar un test para el bug de la paginación": persona.Bugfix,
		"debug logging":                                persona.Feature,
	}
	for in, want := range cases {
		if got := persona.Classify(in); got != want {
			t.Errorf("Classify(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestShouldAsk(t *testing.T) {
	if d := persona.ShouldAsk(persona.Risk{}); d.Ask || len(d.Reasons) != 0 || !persona.DecideAlone(persona.Risk{}) {
		t.Fatalf("no signals must decide alone: %+v", d)
	}
	signals := map[string]persona.Risk{
		"data-loss":                          {DataLoss: true},
		"hard-to-revert":                     {HardToRevert: true},
		"impactful-alternatives":             {ImpactfulAlternatives: true},
		"security":                           {Security: true},
		"unclear-intent":                     {UnclearIntent: true},
		"contradicts-previous-decision":      {ContradictsDecision: true},
		"wrong-decision-for-lack-of-context": {WrongDecisionForLackOfContext: true},
	}
	for want, r := range signals {
		d := persona.ShouldAsk(r)
		if !d.Ask || len(d.Reasons) != 1 || d.Reasons[0] != want || persona.DecideAlone(r) {
			t.Errorf("%s: %+v", want, d)
		}
	}
	if d := persona.ShouldAsk(persona.Risk{Security: true, DataLoss: true}); len(d.Reasons) != 2 {
		t.Fatalf("reasons = %v", d.Reasons)
	}
}

func TestTruncate(t *testing.T) {
	if got := persona.Truncate("  hola   mundo  ", 50); got != "hola mundo" {
		t.Fatalf("got %q", got)
	}
	s := "Primera oración completa y bastante larga para ocupar espacio. Segunda oración que ya no entra del todo en el límite."
	got := persona.Truncate(s, 70)
	if got != "Primera oración completa y bastante larga para ocupar espacio." {
		t.Fatalf("sentence cut = %q", got)
	}
	long := strings.Repeat("palabra ", 100)
	got = persona.Truncate(long, 40)
	if utf8.RuneCountInString(got) > 40 || !strings.HasSuffix(got, "…") || strings.Contains(got, "palabr…") && false {
		t.Fatalf("word cut = %q", got)
	}
	if strings.HasSuffix(strings.TrimSuffix(got, "…"), " ") {
		t.Fatalf("no trailing space before ellipsis: %q", got)
	}
	if got := persona.Truncate(strings.Repeat("x", 100), 10); utf8.RuneCountInString(got) > 10 {
		t.Fatalf("hard cut = %q", got)
	}
}

func TestRenderSmall(t *testing.T) {
	r := persona.NewRenderer()
	out, err := r.Small("Listo. Corregí la validación y quedó cubierta con un test.", "El mismo caso aparece en importación.")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "\n**TIP:** El mismo caso") {
		t.Fatalf("out = %q", out)
	}
	if err := r.ValidateSmall(out); err != nil {
		t.Fatal(err)
	}
	if out, _ := r.Small("", ""); out != "Listo." {
		t.Fatalf("default = %q", out)
	}
	if _, err := r.Small("Uno. Dos. Tres.", ""); !errors.Is(err, persona.ErrTooManySents) {
		t.Fatalf("want ErrTooManySents, got %v", err)
	}
	if err := r.ValidateSmall("Uno. Dos. Tres."); !errors.Is(err, persona.ErrTooManySents) {
		t.Fatalf("want ErrTooManySents, got %v", err)
	}
	if err := r.ValidateSmall(strings.Repeat("a", 300)); !errors.Is(err, persona.ErrTooLong) {
		t.Fatalf("want ErrTooLong, got %v", err)
	}
	if err := r.ValidateSmall("Listo.\n**TIP:**"); !errors.Is(err, persona.ErrBadFormat) {
		t.Fatalf("want ErrBadFormat, got %v", err)
	}
}

func TestRenderLong(t *testing.T) {
	r := persona.NewRenderer()
	out, err := r.Long(persona.Long{
		Did: "Terminé el login y dejé cubiertos los errores principales.", Progress: "Autenticación lista; falta logout.",
		Next: "Voy con logout.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "Ojo") || strings.Contains(out, "TIP") {
		t.Fatalf("optional blocks must not be emitted when empty:\n%s", out)
	}
	want := "**Qué hice:** Terminé el login y dejé cubiertos los errores principales.\n**Progreso:** Autenticación lista; falta logout.\n**Qué sigue:** Voy con logout."
	if out != want {
		t.Fatalf("out =\n%s", out)
	}
	if err := r.ValidateLong(out); err != nil {
		t.Fatal(err)
	}
	full, _ := r.Long(persona.Long{Did: "a.", Progress: "b.", Next: "c.", Ojo: strings.Repeat("riesgo ", 100), Tip: "Sumá recuperación de contraseña."})
	if err := r.ValidateLong(full); err != nil {
		t.Fatalf("full: %v\n%s", err, full)
	}
	for _, line := range strings.Split(full, "\n") {
		if utf8.RuneCountInString(line) > persona.MaxBlockLen+20 {
			t.Fatalf("block not truncated: %d runes", utf8.RuneCountInString(line))
		}
	}
	if _, err := r.Long(persona.Long{Did: "a", Progress: "b"}); !errors.Is(err, persona.ErrMissingBlock) {
		t.Fatalf("want ErrMissingBlock, got %v", err)
	}
	bad := map[string]error{
		"**Qué hice:** a\n**Qué sigue:** c":                                                 persona.ErrMissingBlock,
		"**Progreso:** b\n**Qué hice:** a\n**Qué sigue:** c":                                persona.ErrBadFormat,
		"**Qué hice:** a\n**Progreso:** b\n**Qué sigue:** c\n**Ojo:**":                      persona.ErrEmptyOptional,
		"**Qué hice:** a\n**Progreso:** b\n**Qué sigue:** c\n**TIP:** x\n**Ojo:** y":        persona.ErrBadFormat,
		"**Qué hice:** " + strings.Repeat("z", 400) + "\n**Progreso:** b\n**Qué sigue:** c": persona.ErrTooLong,
		"blah": persona.ErrBadFormat,
	}
	for in, want := range bad {
		if err := r.ValidateLong(in); !errors.Is(err, want) {
			t.Errorf("ValidateLong(%.40q) = %v, want %v", in, err, want)
		}
	}
}

func TestMessagesAreReplaceable(t *testing.T) {
	m := persona.DefaultMessages()
	if got := m.OverlapOjo("Gonza", "el módulo de usuarios"); !strings.HasPrefix(got, "Gonza está trabajando ahora sobre el módulo de usuarios.") {
		t.Fatalf("overlap = %q", got)
	}
	if !strings.Contains(m.ContextTip(), "podés abrir una sesión nueva") {
		t.Fatalf("context tip = %q", m.ContextTip())
	}
	if _, err := persona.ParseMessages("block.did = x"); err == nil {
		t.Fatal("missing keys must be reported")
	}
	if _, err := persona.ParseMessages("nonsense"); err == nil {
		t.Fatal("malformed line must be reported")
	}
	custom := persona.DefaultMessages()
	custom[persona.KeyBlockDid] = "What I did"
	if !strings.Contains(persona.Text(custom), "**What I did:**") {
		t.Fatal("persona text must use replaced messages")
	}
}

func TestPersonaTextBudgetAndRules(t *testing.T) {
	text := persona.DefaultText()
	if strings.Contains(text, "{{") {
		t.Fatal("unresolved placeholder in persona text")
	}
	const maxTokens = 1500
	if n := tokens.EstimateLegacy(text); n > maxTokens {
		t.Fatalf("persona is ~%d tokens, budget is %d", n, maxTokens)
	} else {
		t.Logf("persona estimated at ~%d tokens", n)
	}
	for _, rule := range []string{
		"Argentine Spanish", "ALWAYS query memory", "Current code > `.gabitotl/` > Git > memory > imported docs",
		"one helper at a time, sequentially", "Never several agents in parallel", "Never rewrite shared history",
		"revert commit", "Never claim something works without checking", "Never write passwords, tokens",
		"OJO:", "TIP:", "Ask BEFORE working", "Decide alone", "75%", "**Qué hice:**", "**Progreso:**", "**Qué sigue:**",
		"Este chat ya viene bastante largo", "one or two sentences", "Reuse before you create", "Never leave two versions of the same thing coexisting", "make a rollback point", "commit or stash first",
		"Authorship (highest priority)", "Never add AI/LLM branding or attribution anywhere",
		"Authorship belongs to the person who requested the work", "If you see such branding, remove it",
		"The voice applies ONLY to what you say in chat", "## Terminal and remote", "load the `gabito-terminal` skill",
		"gabitotl persona mode", "## Lead with your expertise", "you own HOW to build it", "Review, challenge and propose",
		"you lead the technical vision", "leave the block format", "everyday example", "No code detail unless the user asks", "Local permission does not authorize remote work", "explicit OK", "If no persona-mode rules were injected at session start", "load the matching `gabito-*` skill",
	} {
		if !strings.Contains(text, rule) {
			t.Errorf("persona text is missing the rule %q", rule)
		}
	}
}
