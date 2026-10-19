// Package tokens holds the one token estimator of GabitoTL. It is a leaf
// package (standard library only) so every other package can use it without
// import cycles. Both formulas are estimates; real counts come from the runtime.
package tokens

import "unicode/utf8"

// Estimate estimates tokens as ceil(characters / 3.6), counting runes. It is the
// default for live text, memory bundles and the project map: Spanish and
// non-ASCII text tokenize worse than the chars/4 rule, so it leans higher and
// budgets fire earlier rather than late.
func Estimate(s string) int {
	return (utf8.RuneCountInString(s)*5 + 17) / 18
}

// EstimateLegacy estimates tokens as ceil(characters / 4), counting runes. It is
// the yardstick of the always-on persona budget, kept so that number stays
// comparable over time.
func EstimateLegacy(s string) int {
	return (utf8.RuneCountInString(s) + 3) / 4
}

// FromBytes converts a byte count to an estimated token count with the same
// ratio as Estimate (about 3.6 characters per token, one byte per character).
func FromBytes(n int) int {
	if n <= 0 {
		return 0
	}
	return (n*5 + 17) / 18
}
