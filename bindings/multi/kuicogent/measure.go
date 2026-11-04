package kuicogent

import "github.com/soypat/kui"

var _ kui.Measurer = (*Measurer)(nil)

// Measurer is a [kui.Measurer] shaping text with Cogent's shaper, as [Painter] draws it:
// Cogent's default font family at the style's size, ignoring the font and letter spacing.
// Unlike the kui.Measurer contract asks, it allocates.
type Measurer struct {
	text shaper
}

// NewMeasurer returns a Measurer.
func NewMeasurer() *Measurer {
	m := new(Measurer)
	m.text.init()
	return m
}

// Measure implements [kui.Measurer]: the width of s on a single line and the line height.
func (m *Measurer) Measure(s string, st *kui.TextStyle) (w, h float32) {
	lines := m.text.lines(s, float32(st.Size), nil)
	return lines.Bounds.Size().X, lines.LineHeight
}
