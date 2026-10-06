package gabitotl

import (
	"errors"
	"strings"
	"unicode"
)

// MaxIDLen is the maximum length of a slug used in file names.
const MaxIDLen = 64

var translit = map[rune]string{
	'á': "a", 'à': "a", 'â': "a", 'ä': "a", 'ã': "a", 'å': "a",
	'é': "e", 'è': "e", 'ê': "e", 'ë': "e",
	'í': "i", 'ì': "i", 'î': "i", 'ï': "i",
	'ó': "o", 'ò': "o", 'ô': "o", 'ö': "o", 'õ': "o",
	'ú': "u", 'ù': "u", 'û': "u", 'ü': "u",
	'ñ': "n", 'ç': "c", 'ß': "ss",
}

// Slug converts free text into a lowercase ASCII identifier safe for file
// names ("Gabi Pérez" -> "gabi-perez"). It returns "" when nothing usable remains.
func Slug(s string) string {
	var b strings.Builder
	lastDash := true
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if t, ok := translit[r]; ok {
			b.WriteString(t)
			lastDash = false
			continue
		}
		if r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > MaxIDLen {
		out = strings.Trim(out[:MaxIDLen], "-")
	}
	return out
}

// ErrInvalidID is returned for identifiers that are not already canonical slugs.
var ErrInvalidID = errors.New("invalid id: must be a lowercase slug (a-z, 0-9, '-')")

// ValidID reports whether id is a canonical slug, so it can never escape its directory.
func ValidID(id string) bool {
	return id != "" && Slug(id) == id
}

func checkID(id string) error {
	if !ValidID(id) {
		return ErrInvalidID
	}
	return nil
}
