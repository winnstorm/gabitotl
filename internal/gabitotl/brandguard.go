package gabitotl

import (
	"fmt"

	"github.com/winnstorm/gabitotl/internal/branding"
)

// BrandingError reports LLM attribution in content headed for .gabitotl/.
// Authorship belongs to the person who asked for the work, so the write is
// refused instead of silently rewritten.
type BrandingError struct {
	Line  int
	Class branding.Class
}

func (e *BrandingError) Error() string {
	return fmt.Sprintf("refusing to write: line %d credits an AI tool as author (%s); authorship belongs to the person who asked for the work, remove that line", e.Line, e.Class)
}

// ValidateNoBranding returns a *BrandingError for the first line of content
// that attributes the work to an LLM. Plain technical mentions of a tool or
// runtime are fine. It never echoes the offending text.
func ValidateNoBranding(content []byte) error {
	if branding.IsBinary(content) {
		return nil
	}
	if fs := branding.Default().Scan("", string(content), branding.ContextProse); len(fs) > 0 {
		return &BrandingError{Line: fs[0].Line, Class: fs[0].Class}
	}
	return nil
}
