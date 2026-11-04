package kui2go

import "github.com/soypat/kui"

var _ kui.Measurer = TextFaces(nil)

// TextFaces maps the font identifiers of [kui.TextStyle] and of text draw commands to faces:
// font i is drawn and measured with TextFaces[i]. It implements [kui.Measurer] with the glyph
// advances painters draw with, so measured and painted widths agree.
type TextFaces []kui.FontFace

// Face returns the face of font id, or nil if there is none.
func (fs TextFaces) Face(id uint16) kui.FontFace {
	if int(id) >= len(fs) {
		return nil
	}
	return fs[id]
}

// Measure implements [kui.Measurer]. The width of s is the sum of its glyph advances, including
// those of glyphs the face does not have, plus letter spacing; its height the font's line height.
func (fs TextFaces) Measure(s string, st *kui.TextStyle) (w, h float32) {
	f := fs.Face(st.FontID)
	if f == nil {
		return 0, 0
	}
	width := 0
	for _, r := range s {
		_, _, _, _, _, advance, _ := f.Glyph(st.Size, r)
		width += advance + int(st.LetterSpacing)
	}
	_, lineHeight := f.Metrics(st.Size)
	return float32(width), float32(lineHeight)
}
