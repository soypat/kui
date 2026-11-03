package kui

import (
	"image/color"
)

// Font is a typeface described in its own terms: a design grid, a character map and advances.
// Every metric is in font units. Its method set is a subset of piudf's piupage.Font so one
// font, such as a *lefevre.Font, serves both a kui interface and a PDF document.
type Font interface {
	// UnitsPerEm is the design grid the advances below are expressed in.
	UnitsPerEm() int
	// GlyphID is the glyph the font draws for r, or 0 if it has none.
	GlyphID(r rune) uint16
	// GlyphAdvance is glyph's horizontal advance in font units.
	GlyphAdvance(glyph uint16) int32
}

// Advance returns glyph's pen advance at size pixels, rounded to whole pixels. A [FontFace] built on f
// reports it and its [Measurer] sums it, so measured and painted widths agree.
func FontAdvance(f Font, glyph uint16, size uint16) int {
	upem := int64(f.UnitsPerEm())
	if upem <= 0 {
		return 0
	}
	n := int64(f.GlyphAdvance(glyph)) * int64(size)
	if n < 0 {
		return -int((-n + upem/2) / upem)
	}
	return int((n + upem/2) / upem)
}

// Face provides glyph alpha masks for CPU rasterization. Implementations must not allocate.
type FontFace interface {
	// Metrics returns the distance from the top of a line to the baseline and the line height, in pixels.
	Metrics(size uint16) (ascent, lineHeight int)
	// Glyph returns the alpha coverage mask of r: w*h bytes in row major order. (dx, dy) is the
	// offset of the mask's top left corner from the pen position on the baseline and advance is
	// the horizontal pen advance. ok is false if the face has no glyph for r.
	Glyph(size uint16, r rune) (mask []byte, w, h, dx, dy, advance int, ok bool)
}

// Measurer measures text. Implementations must not allocate nor retain s.
type Measurer interface {
	// Measure returns the width and height of s laid out on a single line. The height of
	// the empty string is the height of a line.
	Measure(s string, st *TextStyle) (w, h float32)
}

// TextLine is a laid out line of a node's text, see [Tree].
type TextLine struct {
	Rect Rect
	// Start and Len are the byte range of the line within the node's text.
	Start, Len uint32
}

// Color is an alias of Go's standard library non-alpha-premultiplied 32-bit color [color.NRGBA].
type Color = color.NRGBA

type TextStyle struct {
	FontID        uint16 //
	Size          uint16 // Font size in pixels.
	LetterSpacing uint16 // Extra space between characters in pixels.
	LineHeight    uint16 // Height of each line in pixels. Zero uses the measured height.
	Color         Color
	Wrap          TextWrap
	Align         AlignH
}

// TextWrap controls how text is broken into lines.
type TextWrap uint8

const (
	// Break on whitespace and newlines.
	WrapWords TextWrap = iota // words
	// Break only on newlines.
	WrapNewlines // newlines
	// Never break.
	WrapNone // none
)

// AlignH is a horizontal alignment: of wrapped lines within their box, and of children within a
// node ([LayoutStyle.AlignX]).
type AlignH uint8

const (
	AlignLeft   AlignH = iota // left
	AlignCenter               // center
	AlignRight                // right
)
