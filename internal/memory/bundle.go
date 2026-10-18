package memory

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/winnstorm/gabitotl/internal/persona"
)

// Bundle limits, in estimated tokens (estimateTokens).
const (
	DefaultBundleTokens   = 1500
	DefaultPerEntryTokens = 250
	charsPerToken         = 3.6
)

// estimateTokens mirrors budget.EstimateTextTokens (ceil(runes/3.6)); budget imports
// this package, so the formula is repeated here instead of imported.
func estimateTokens(s string) int {
	return int(math.Ceil(float64(utf8.RuneCountInString(s)) / charsPerToken))
}

// BundleItem is one packed entry: a header line and a capped body.
type BundleItem struct {
	ID     ID        `json:"id,omitempty"`
	Type   EntryType `json:"type"`
	Title  string    `json:"title"`
	Body   string    `json:"body"`
	Tokens int       `json:"tokens"`
}

// Bundle is the capped result handed to the agent.
type Bundle struct {
	Items   []BundleItem `json:"entries"`
	Tokens  int          `json:"tokens"`
	Omitted int          `json:"omitted"`
}

// Header renders the single header line of an item.
func (i BundleItem) Header() string {
	h := fmt.Sprintf("%s · %s", i.Type, i.Title)
	if i.ID != "" {
		h += " · #" + string(i.ID)
	}
	return h
}

func (i BundleItem) text() string {
	if i.Body == "" {
		return i.Header()
	}
	return i.Header() + "\n" + i.Body
}

// Summary is the final line of a rendered bundle.
func (b Bundle) Summary() string {
	return fmt.Sprintf("memory: %d entries, ~%d tokens (%d omitted)", len(b.Items), b.Tokens, b.Omitted)
}

// Render returns the compact text form: per entry a header and a body, then the summary.
func (b Bundle) Render() string {
	var sb strings.Builder
	for _, it := range b.Items {
		sb.WriteString(it.text())
		sb.WriteString("\n")
	}
	sb.WriteString(b.Summary())
	return sb.String()
}

// PackBundle packs entries, in the given (relevance) order, under a per-entry and a
// total token cap. Bodies are cut at a sentence or word boundary. Entries that do not
// fit in the remaining total are counted as omitted, and the first one that does not
// fit ends the packing so lower-ranked entries never jump the queue.
func PackBundle(entries []Entry, maxTokens, perEntryTokens int) Bundle {
	if maxTokens <= 0 {
		maxTokens = DefaultBundleTokens
	}
	if perEntryTokens <= 0 {
		perEntryTokens = DefaultPerEntryTokens
	}
	var b Bundle
	for n, e := range entries {
		it := BundleItem{ID: e.ID, Type: e.Type, Title: strings.Join(strings.Fields(e.Title), " ")}
		head := estimateTokens(it.Header()) + 1
		bodyRunes := int(float64(perEntryTokens-head) * charsPerToken)
		if bodyRunes > 0 {
			it.Body = persona.Truncate(e.Content, bodyRunes)
		}
		it.Tokens = estimateTokens(it.text())
		if b.Tokens+it.Tokens > maxTokens {
			b.Omitted = len(entries) - n
			break
		}
		b.Items = append(b.Items, it)
		b.Tokens += it.Tokens
	}
	return b
}
