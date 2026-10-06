// Package branding detects LLM branding that claims authorship or attribution.
//
// Authorship of the code always belongs to the person who asked for it, so a
// line that credits an AI tool (a co-author trailer, a "generated with" footer,
// an author header, assistant chatter, a share link) is a defect. A plain
// technical mention is not: documenting that GabitoTL supports a given runtime,
// naming a model id or a config directory never trips the detector, because
// every pattern needs an authorship or attribution cue next to the brand.
//
// The package is a leaf: deterministic, offline, no dependency on the rest of
// GabitoTL, so every layer (commits, hooks, memory, file writes) can use it.
package branding

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Class is the pattern family a finding belongs to.
type Class string

// Pattern classes.
const (
	ClassTrailer     Class = "trailer"     // A: co-author/sign-off/author identities
	ClassGenerated   Class = "generated"   // B: "generated with/by ..." lines and markers
	ClassHeader      Class = "header"      // C: @author, __author__, copyright fields
	ClassBoilerplate Class = "boilerplate" // D: leaked assistant voice
	ClassFooter      Class = "footer"      // E: badges, share links, signatures
)

// Action is the suggested fix for a finding.
type Action string

// Fix actions.
const (
	// ActionRemoveLine means the whole line is attribution and can be deleted.
	ActionRemoveLine Action = "remove-line"
	// ActionManual means the match shares the line with code or prose that must
	// be kept, so a human decides.
	ActionManual Action = "manual-review"
)

// Context says what kind of text is being scanned; it decides which lines are
// safe to remove automatically.
type Context int

// Scan contexts.
const (
	// ContextMessage is a commit message, PR text or memory entry: every line
	// that is only attribution is removable.
	ContextMessage Context = iota
	// ContextProse is a documentation file (Markdown, text, HTML).
	ContextProse
	// ContextCode is source or configuration: only whole comment lines are
	// removable, anything that may be program logic needs manual review.
	ContextCode
)

// Finding is one detected attribution line.
type Finding struct {
	File   string `json:"file,omitempty"`
	Line   int    `json:"line"`
	Class  Class  `json:"class"`
	Text   string `json:"text"` // the offending line, trimmed and truncated
	Action Action `json:"action"`
	Fix    string `json:"fix"`
}

// Removable reports whether the finding can be fixed by deleting its line.
func (f Finding) Removable() bool { return f.Action == ActionRemoveLine }

const (
	sep      = `(?:[\s_+\-]|%20)+`
	maxText  = 100
	maxLine  = 4000
	maxExtra = 64
)

// baseBrands is the built-in brand list (lower case regex fragments).
var baseBrands = []string{
	`github copilot`, `copilot`, `claude`, `anthropic`, `chatgpt`, `gpt-?\d[\w.\-]*`, `gpt`, `openai`, `codex`,
	`gemini`, `bard`, `qwen`, `deepseek`, `kimi`, `moonshot`, `llama`, `meta ai`, `mistral`, `grok`, `xai`,
	`x\.ai`, `cursor`, `windsurf`, `devin`, `cline`, `aider`, `opencode`, `kiro`, `gentle-?ai`, `amazon q`,
	`perplexity`,
}

// aiMailDomains are vendor domains whose addresses identify an AI identity.
var aiMailDomains = []string{
	"anthropic.com", "openai.com", "x.ai", "deepseek.com", "mistral.ai", "perplexity.ai", "cursor.com",
	"cursor.sh", "codeium.com", "windsurf.com", "cognition.ai", "cognition-labs.com", "aider.chat",
	"moonshot.cn", "moonshot.ai", "qwen.ai", "kiro.dev",
}

type rule struct {
	class    Class
	re       *regexp.Regexp
	identity int // capture group holding an identity that must look like an AI tool (0: none)
}

// Detector finds LLM attribution. Build one with New; it is safe for concurrent use.
type Detector struct {
	brand *regexp.Regexp // any brand, word-bounded
	rules []rule
	name  *regexp.Regexp // tokens allowed in an AI identity name
}

// New builds a Detector. extraTerms are additional brand names (case-insensitive,
// matched literally).
func New(extraTerms ...string) *Detector {
	alts := append([]string(nil), baseBrands...)
	for _, t := range extraTerms {
		t = strings.TrimSpace(t)
		if t == "" || utf8.RuneCountInString(t) > maxExtra {
			continue
		}
		alts = append(alts, regexp.QuoteMeta(strings.ToLower(t)))
	}
	b := `(?:` + strings.Join(alts, "|") + `)`
	// Brands that are unambiguous when someone claims to BE them.
	self := `(?:chatgpt|gpt-?\d[\w.\-]*|gemini|bard|qwen|deepseek|llama|mistral|grok|copilot)`
	verbs := `(?:generated|created|written|authored|assisted|made|co-?developed|co-?authored|co-?created|drafted|produced|vibe-?coded|translated)`
	opener := `[\[(*_` + "`" + `"'<]*`
	mk := func(c Class, id int, pat string) rule { return rule{c, regexp.MustCompile(`(?i)` + pat), id} }
	d := &Detector{
		brand: regexp.MustCompile(`(?i)\b` + b + `\b`),
		name:  regexp.MustCompile(`(?i)\b` + b + `\b`),
		rules: []rule{
			// A: git trailers and identity lines.
			mk(ClassTrailer, 2, `^\s*(?:>\s*)*(co-authored-by|co-authored|signed-off-by|reviewed-by|acked-by|tested-by|suggested-by|assisted-by|generated-by|authored-by|reported-by|helped-by)\s*:\s*(.+?)\s*$`),
			// C: header and author fields.
			mk(ClassHeader, 1, `@author\s+(.+?)\s*(?:\*/|-->)?\s*$`),
			mk(ClassHeader, 1, `(?:__author__|\bauthors?|\bmaintainers?|\bcreated[_ -]by|\bwritten[_ -]by)["']?\s*[:=]\s*["'\[]*\s*(.+?)\s*$`),
			mk(ClassHeader, 1, `copyright\s*(?:\(c\)|©)?\s*(?:\d{4}(?:\s*[-–,]\s*\d{4})*)?\s*[,:]?\s*(.+?)\s*$`),
			// B: generated-by/with lines and markers.
			mk(ClassGenerated, 0, `\b`+verbs+sep+`(?:with|by|using|via|from|through)`+sep+`(?:the`+sep+`help`+sep+`of`+sep+`)?`+opener+`(?:an?`+sep+`)?(?:ai|llm|artificial`+sep+`intelligence|`+b+`)\b`),
			mk(ClassGenerated, 0, `\bwith`+sep+`the`+sep+`help`+sep+`of`+sep+opener+`(?:ai|llm|`+b+`)\b`),
			mk(ClassGenerated, 0, `\b(?:ai|llm|gpt|chatgpt|claude|copilot)[- ](?:generated|assisted|authored|written|created|co-?authored)\b`),
			mk(ClassGenerated, 0, `🤖\s*(?:generated|created|made|written|authored|co-?authored|assisted)\b`),
			// E: badges, signatures and share links.
			mk(ClassFooter, 0, `https?://(?:www\.)?(?:claude\.com/claude-code|claude\.ai/(?:code|share|chat|public)|chatgpt\.com/(?:share|c/|g/)|chat\.openai\.com/(?:share|c/)|gemini\.google\.com/(?:share|app/)|g\.co/gemini/share|chat\.deepseek\.com|copilot\.microsoft\.com|perplexity\.ai/search|grok\.com/share|chat\.mistral\.ai|kimi\.com/share|chat\.qwen\.ai)`),
			mk(ClassFooter, 0, `!?\[[^\]]*\]\([^)]*(?:made|built|generated|powered|assisted|co-?authored|created|written)`+sep+`(?:with|by)`+sep+`(?:ai|llm|`+b+`)`),
			mk(ClassFooter, 0, `^\W*(?:sent|posted)`+sep+`from`+sep+b+`\b`),
			mk(ClassFooter, 0, `^\s*(?:[—–]|--)\s*`+b+`(?:\s+(?:code|ai))?\s*$`),
			// D: assistant voice.
			mk(ClassBoilerplate, 0, `\bas\s+an?\s+(?:ai|llm|large\s+language\s+model|language\s+model|ai\s+(?:language\s+model|assistant))\b`),
			mk(ClassBoilerplate, 0, `\bI(?:'m|’m|\s+am)\s+(?:`+self+`\b|an?\s+(?:ai\b|llm\b|(?:large\s+)?language\s+model)|claude\s*,?\s*(?:an?\s+ai|made\s+by|created\s+by|trained\s+by|developed\s+by|built\s+by|from\s+anthropic))`),
			mk(ClassBoilerplate, 0, `^\W*(?:(?:sure|certainly|absolutely|of\s+course)[,!.]?\s*)?here(?:'s|\s+is|\s+are)\s+(?:the|an?|your)\s+(?:updated|modified|revised|complete|full|final|refactored|fixed|corrected|new)\s+(?:code|version|implementation|file|function|script|snippet)\b`),
			mk(ClassBoilerplate, 0, `^\W*i\s+hope\s+this\s+helps\b`),
		},
	}
	return d
}

var defaultDetector = New()

// Default returns a shared Detector with the built-in brand list.
func Default() *Detector { return defaultDetector }

// suffixWords may follow a brand inside an AI identity name ("Claude Code",
// "Claude Opus 4.5", "Anthropic, PBC").
var suffixWords = regexp.MustCompile(`(?i)\b(?:code|ai|opus|sonnet|haiku|bot|agent|assistant|pbc|inc|llc|ltd|labs?|team|chat|co|cli|swe|integration|all\s+rights\s+reserved|v?\d[\w.\-]*)\b`)

// IsAIIdentity reports whether value (a name, optionally with <email>) names an
// AI tool: an AI vendor address, a bot account of one, or a name made only of
// brand words ("Claude", "Claude Sonnet 5.5", "GitHub Copilot"). A person who
// merely shares a brand word ("Claude Smith") is not an AI identity.
func (d *Detector) IsAIIdentity(value string) bool {
	v := strings.ToLower(strings.TrimSpace(value))
	name := v
	if i := strings.IndexByte(v, '<'); i >= 0 {
		name = v[:i]
		addr := v[i+1:]
		if j := strings.IndexByte(addr, '>'); j >= 0 {
			addr = addr[:j]
		}
		if at := strings.LastIndexByte(addr, '@'); at >= 0 {
			dom := addr[at+1:]
			for _, m := range aiMailDomains {
				if dom == m || strings.HasSuffix(dom, "."+m) {
					return true
				}
			}
			local := addr[:at]
			if strings.HasSuffix(local, "[bot]") && d.brand.MatchString(local) {
				return true
			}
		}
	}
	name = strings.Trim(name, " \t\"'`[](),;*_.")
	if name == "" {
		return false
	}
	if strings.HasSuffix(name, "[bot]") && d.brand.MatchString(name) {
		return true
	}
	if !d.brand.MatchString(name) {
		return false
	}
	rest := d.name.ReplaceAllString(name, " ")
	rest = suffixWords.ReplaceAllString(rest, " ")
	rest = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, rest)
	return rest == ""
}

// Scan checks text line by line. file is only copied into the findings.
func (d *Detector) Scan(file, text string, ctx Context) []Finding {
	var out []Finding
	for i, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if f, ok := d.scanLine(line, ctx); ok {
			f.File, f.Line = file, i+1
			out = append(out, f)
		}
	}
	return out
}

func (d *Detector) scanLine(line string, ctx Context) (Finding, bool) {
	if len(line) > maxLine {
		line = line[:maxLine]
	}
	for _, r := range d.rules {
		loc := r.re.FindStringSubmatchIndex(line)
		if loc == nil {
			continue
		}
		if r.identity > 0 {
			a, b := loc[2*r.identity], loc[2*r.identity+1]
			if a < 0 || !d.IsAIIdentity(line[a:b]) {
				continue
			}
		}
		f := Finding{Class: r.class, Text: truncate(strings.TrimSpace(line))}
		if removable(line, loc[0], ctx) {
			f.Action, f.Fix = ActionRemoveLine, "remove this line"
		} else {
			f.Action, f.Fix = ActionManual, "review by hand: the attribution shares the line with content to keep"
		}
		return f, true
	}
	return Finding{}, false
}

// removable decides whether the whole line is attribution that can be deleted.
func removable(line string, start int, ctx Context) bool {
	for _, r := range line[:start] {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	if ctx != ContextCode {
		return true
	}
	t := strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(t, "/*"):
		return strings.Contains(t, "*/")
	case strings.HasPrefix(t, "<!--"):
		return strings.Contains(t, "-->")
	case strings.HasPrefix(t, `"""`), strings.HasPrefix(t, "'''"):
		return strings.Count(t, t[:3]) >= 2
	}
	for _, p := range []string{"//", "#", "--", "*", ";", "%", "rem ", "REM "} {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}

func truncate(s string) string {
	if utf8.RuneCountInString(s) <= maxText {
		return s
	}
	r := []rune(s)
	return string(r[:maxText]) + "…"
}

// Strip removes every line that is only attribution from a message-like text
// (commit message, PR title or body, memory entry). removed lists what was
// taken out; remaining lists findings that share a line with content and need
// a human. Runs of blank lines left behind collapse and the text is trimmed.
func (d *Detector) Strip(text string) (clean string, removed, remaining []Finding) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var kept []string
	for i, line := range lines {
		f, ok := d.scanLine(line, ContextMessage)
		if !ok {
			kept = append(kept, line)
			continue
		}
		f.Line = i + 1
		if f.Removable() {
			removed = append(removed, f)
			continue
		}
		remaining = append(remaining, f)
		kept = append(kept, line)
	}
	if len(removed) == 0 {
		return text, nil, remaining
	}
	var out []string
	blank := false
	for _, l := range kept {
		if strings.TrimSpace(l) == "" {
			if blank {
				continue
			}
			blank = true
		} else {
			blank = false
		}
		out = append(out, l)
	}
	return strings.TrimSpace(strings.Join(out, "\n")), removed, remaining
}

// Has reports whether text contains any attribution.
func (d *Detector) Has(text string) bool {
	return len(d.Scan("", text, ContextMessage)) > 0
}
