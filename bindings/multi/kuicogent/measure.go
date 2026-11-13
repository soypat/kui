package kuicogent

import "github.com/soypat/kui"

var _ kui.FontMeasurer = (*FontMeasurer)(nil)

// FontMeasurer is a [kui.FontMeasurer] shaping text with Cogent's shaper, as [Painter] draws it:
// Cogent's default font family at the style's size, ignoring the font and letter spacing.
// Unlike the kui.FontMeasurer contract asks, it allocates.
type FontMeasurer struct {
	text shaper
}

// NewFontMeasurer returns a Measurer.
func NewFontMeasurer() *FontMeasurer {
	m := new(FontMeasurer)
	m.text.init()
	return m
}

// Measure implements [kui.FontMeasurer]: the width of s on a single line and the line height.
func (m *FontMeasurer) Measure(s string, st *kui.TextStyle) (w, h float32) {
	lines := m.text.lines(s, float32(st.Size), nil)
	return lines.Bounds.Size().X, lines.LineHeight
}
