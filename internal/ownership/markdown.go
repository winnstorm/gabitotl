// Package ownership marks and tracks every asset GabitoTL writes into files it
// does not own (spec section 38), so it can be updated idempotently and removed
// without touching user content.
//
//   - Markdown carries <!-- gabitotl:start NAME --> / <!-- gabitotl:end NAME -->
//     sections (this file).
//   - JSON/JSONC/TOML-like configs cannot carry our markers, so the keys we wrote
//     are tracked in the global state file (state.go) and edited with a splice
//     editor that never re-serializes the document (jsonedit.go).
package ownership

import (
	"fmt"
	"regexp"
	"strings"
)

// StartMarker and EndMarker delimit a GabitoTL section in Markdown.
func StartMarker(name string) string { return "<!-- gabitotl:start " + name + " -->" }
func EndMarker(name string) string   { return "<!-- gabitotl:end " + name + " -->" }

// Block is a marked region of a document: Content[Start:End] includes both
// marker lines and the newline that ends the closing one.
type Block struct {
	Name       string
	Start, End int
}

// FindBlocks returns every block opened by startRe (its first capture group is
// the block name) and closed by the pattern endTmpl, where %s stands for the
// quoted name. Unclosed starts are ignored.
func FindBlocks(content string, startRe *regexp.Regexp, endTmpl string) []Block {
	var out []Block
	for _, m := range startRe.FindAllStringSubmatchIndex(content, -1) {
		name := content[m[2]:m[3]]
		endRe, err := regexp.Compile(fmt.Sprintf(endTmpl, regexp.QuoteMeta(name)))
		if err != nil {
			continue
		}
		loc := endRe.FindStringIndex(content[m[1]:])
		if loc == nil {
			continue
		}
		end := m[1] + loc[1]
		if end < len(content) && content[end] == '\r' {
			end++
		}
		if end < len(content) && content[end] == '\n' {
			end++
		}
		// The start marker owns its whole line when it is the first thing on it.
		out = append(out, Block{Name: name, Start: m[0], End: end})
	}
	return out
}

var ownStart = regexp.MustCompile(`<!-- gabitotl:start ([A-Za-z0-9._-]+) -->`)

// Sections returns the GabitoTL sections of content.
func Sections(content string) []Block {
	return FindBlocks(content, ownStart, `<!-- gabitotl:end %s -->`)
}

func findSection(content, name string) (Block, bool) {
	for _, b := range Sections(content) {
		if b.Name == name {
			return b, true
		}
	}
	return Block{}, false
}

func render(name, body string) string {
	return StartMarker(name) + "\n" + strings.TrimRight(body, "\r\n") + "\n" + EndMarker(name) + "\n"
}

// SectionBody returns the body currently stored in the named section.
func SectionBody(content, name string) (string, bool) {
	b, ok := findSection(content, name)
	if !ok {
		return "", false
	}
	inner := content[b.Start:b.End]
	inner = strings.TrimPrefix(inner, StartMarker(name)+"\n")
	if i := strings.LastIndex(inner, EndMarker(name)); i >= 0 {
		inner = inner[:i]
	}
	return strings.TrimRight(inner, "\r\n"), true
}

// UpsertSection inserts or replaces the named section. Content outside the
// markers is preserved byte for byte, and applying it twice is a no-op.
func UpsertSection(content, name, body string) (string, error) {
	if strings.Contains(body, "gabitotl:start") || strings.Contains(body, "gabitotl:end") {
		return "", fmt.Errorf("ownership: section body must not contain GabitoTL markers")
	}
	block := render(name, body)
	if b, ok := findSection(content, name); ok {
		return content[:b.Start] + block + content[b.End:], nil
	}
	switch {
	case content == "":
		return block, nil
	case !strings.HasSuffix(content, "\n"):
		return content + "\n\n" + block, nil
	}
	return content + "\n" + block, nil
}

// RemoveBlock deletes b from content. A blank separator line left directly
// before a trailing block is removed with it, so appending then removing a
// section restores the original text.
func RemoveBlock(content string, b Block) string {
	before, after := content[:b.Start], content[b.End:]
	if after == "" && strings.HasSuffix(before, "\n\n") {
		before = before[:len(before)-1]
	}
	return before + after
}

// RemoveSection deletes the named section and reports whether it existed.
func RemoveSection(content, name string) (string, bool) {
	b, ok := findSection(content, name)
	if !ok {
		return content, false
	}
	return RemoveBlock(content, b), true
}
