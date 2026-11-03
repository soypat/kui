package kui2go

import (
	"unicode/utf8"

	"github.com/soypat/kui"
)

var _ kui.Measurer = TextMono{}

// TextMono is a [kui.Measurer] measuring text as if every rune had the same advance, i.e. for
// fixed width bitmap fonts and tests.
type TextMono struct {
	CharWidth  kui.Scalar // Advance of each rune in pixels.
	LineHeight kui.Scalar // Height of a line in pixels.
}

// Measure implements [kui.Measurer]: the rune count of s times the character advance plus
// letter spacing, and the line height.
func (m TextMono) Measure(s string, st *kui.TextStyle) (w, h float32) {
	n := kui.Scalar(utf8.RuneCountInString(s))
	return n * (m.CharWidth + kui.Scalar(st.LetterSpacing)), m.LineHeight
}
