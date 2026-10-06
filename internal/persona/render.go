package persona

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Length limits (runes). Compact output is a product rule: every block short.
const (
	MaxBlockLen = 280 // each block of a long response
	MaxSmallLen = 240 // a small-work response (1-2 sentences)
)

// Output errors.
var (
	ErrMissingBlock  = errors.New("persona: a long response needs Qué hice, Progreso and Qué sigue")
	ErrTooLong       = errors.New("persona: block is longer than the allowed maximum")
	ErrTooManySents  = errors.New("persona: a small response must be one or two sentences")
	ErrEmptyOptional = errors.New("persona: Ojo and TIP must not be present when empty")
	ErrBadFormat     = errors.New("persona: response does not follow the compact format")
)

// Truncate collapses whitespace and shortens s to at most max runes. The rule:
// keep whole sentences when at least half of max survives; otherwise cut at the
// last word boundary and end with an ellipsis. It never exceeds max.
func Truncate(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	head := runes[:max]
	for i := len(head) - 1; i >= max/2; i-- {
		if r := head[i]; (r == '.' || r == '!' || r == '?') && (i+1 == len(runes) || runes[i+1] == ' ') {
			return string(head[:i+1])
		}
	}
	cut := max - 1 // room for the ellipsis
	h := runes[:cut]
	for i := len(h) - 1; i > 0; i-- {
		if h[i] == ' ' {
			h = h[:i]
			break
		}
	}
	return strings.TrimRight(string(h), " ,;:-") + "…"
}

// Renderer produces compact responses with the configured message templates.
type Renderer struct {
	Msgs Messages
}

// NewRenderer returns a Renderer with the embedded es-AR templates.
func NewRenderer() Renderer { return Renderer{Msgs: DefaultMessages()} }

func (r Renderer) label(key string) string { return "**" + r.Msgs[key] + ":**" }

// Small renders small work: one or two sentences, plus an optional TIP. Text
// longer than MaxSmallLen is truncated with Truncate.
func (r Renderer) Small(text, tip string) (string, error) {
	text = Truncate(text, MaxSmallLen)
	if text == "" {
		text = r.Msgs[KeySmallDone]
	}
	if countSentences(text) > 2 {
		return "", ErrTooManySents
	}
	out := text
	if tip = strings.TrimSpace(tip); tip != "" {
		out += "\n" + r.label(KeyBlockTip) + " " + Truncate(tip, MaxBlockLen)
	}
	return out, nil
}

// Long is a long-work response. Did, Progress and Next are mandatory; Ojo and
// Tip are emitted only when non-empty, never by obligation.
type Long struct {
	Did, Progress, Next string
	Ojo, Tip            string
}

// Long renders the mandatory blocks and the optional Ojo/TIP, truncating each
// block to MaxBlockLen.
func (r Renderer) Long(l Long) (string, error) {
	if strings.TrimSpace(l.Did) == "" || strings.TrimSpace(l.Progress) == "" || strings.TrimSpace(l.Next) == "" {
		return "", ErrMissingBlock
	}
	var lines []string
	add := func(key, text string) {
		lines = append(lines, r.label(key)+" "+Truncate(text, MaxBlockLen))
	}
	add(KeyBlockDid, l.Did)
	add(KeyBlockProgress, l.Progress)
	add(KeyBlockNext, l.Next)
	if strings.TrimSpace(l.Ojo) != "" {
		add(KeyBlockOjo, l.Ojo)
	}
	if strings.TrimSpace(l.Tip) != "" {
		add(KeyBlockTip, l.Tip)
	}
	return strings.Join(lines, "\n"), nil
}

// countSentences counts sentence terminators, treating text without one as a
// single sentence.
func countSentences(s string) int {
	n := 0
	rs := []rune(strings.TrimSpace(s))
	for i, c := range rs {
		if (c == '.' || c == '!' || c == '?') && (i+1 == len(rs) || rs[i+1] == ' ' || rs[i+1] == '\n') {
			n++
		}
	}
	if n == 0 && len(rs) > 0 {
		return 1
	}
	return n
}

// ValidateLong checks a rendered long response: the three mandatory blocks in
// order, then optional Ojo and TIP in that order, each non-empty and within
// MaxBlockLen.
func (r Renderer) ValidateLong(out string) error {
	order := []string{KeyBlockDid, KeyBlockProgress, KeyBlockNext, KeyBlockOjo, KeyBlockTip}
	pos := 0
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		matched := false
		for i := pos; i < len(order); i++ {
			prefix := r.label(order[i])
			if strings.HasPrefix(line, prefix) {
				body := strings.TrimSpace(strings.TrimPrefix(line, prefix))
				if body == "" {
					return fmt.Errorf("%w: %s", ErrEmptyOptional, r.Msgs[order[i]])
				}
				if utf8.RuneCountInString(body) > MaxBlockLen {
					return fmt.Errorf("%w: %s", ErrTooLong, r.Msgs[order[i]])
				}
				seen[order[i]] = true
				pos = i + 1
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("%w: unexpected line %q", ErrBadFormat, Truncate(line, 40))
		}
	}
	if !seen[KeyBlockDid] || !seen[KeyBlockProgress] || !seen[KeyBlockNext] {
		return ErrMissingBlock
	}
	return nil
}

// ValidateSmall checks a rendered small response: at most two sentences within
// MaxSmallLen, with an optional trailing TIP line.
func (r Renderer) ValidateSmall(out string) error {
	main, tip, hasTip := strings.Cut(strings.TrimSpace(out), "\n")
	if utf8.RuneCountInString(main) > MaxSmallLen {
		return ErrTooLong
	}
	if countSentences(main) > 2 || strings.TrimSpace(main) == "" {
		return ErrTooManySents
	}
	if hasTip {
		prefix := r.label(KeyBlockTip)
		if !strings.HasPrefix(tip, prefix) || strings.TrimSpace(strings.TrimPrefix(tip, prefix)) == "" {
			return ErrBadFormat
		}
		if utf8.RuneCountInString(tip) > MaxBlockLen+utf8.RuneCountInString(prefix)+1 {
			return ErrTooLong
		}
	}
	return nil
}

// TipLine renders a TIP block, truncated like every other block.
func (r Renderer) TipLine(text string) string {
	return r.label(KeyBlockTip) + " " + Truncate(text, MaxBlockLen*2)
}
