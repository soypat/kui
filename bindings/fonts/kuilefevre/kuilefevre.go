// Package kuilefevre provides a [kui.FontFace] for outline fonts such as TrueType, rasterized
// with github.com/soypat/lefevre. Measure faces with [kui2go.TextFaces].
package kuilefevre

import (
	"cmp"
	"math"
	"slices"

	"github.com/soypat/kui"
	"github.com/soypat/lefevre"
	"github.com/soypat/lefevre/raster"
)

// Source is a [kui.Font] with the vertical metrics and glyph outlines rasterizing needs.
// A *lefevre.Font implements it.
type Source interface {
	kui.Font
	raster.Outliner
	// VMetrics reports the vertical metrics in font units. descent is negative, below the baseline.
	VMetrics() (ascent, descent, lineGap, capHeight int16)
}

var _ Source = (*lefevre.Font)(nil)

// Config configures a [Face].
type Config struct {
	Font  Source   // Font rasterized.
	Sizes []uint16 // Pixel sizes rasterized.
	Runes string   // Runes rasterized at every size. Empty rasterizes printable ASCII.
}

// Face is an outline font face. [Face.Configure] rasterizes every size and rune once; Glyph and
// Metrics never allocate. Glyphs are placed one per rune with no kerning nor shaping.
type Face struct {
	font   Source
	glyphs []glyph // Sorted by key.
	masks  []byte
}

// glyph is one rasterized glyph: a w*h alpha mask at off in Face.masks.
type glyph struct {
	key                   uint32 // size<<16 | glyph ID.
	off                   int
	w, h, dx, dy, advance int32
}

var _ kui.FontFace = (*Face)(nil)

func glyphKey(size, id uint16) uint32 { return uint32(size)<<16 | uint32(id) }

// Configure rasterizes the glyphs of cfg.Runes at every size, reusing the buffers of a previous
// configuration when they are large enough.
func (f *Face) Configure(cfg Config) error {
	src := cfg.Font
	if src == nil || src.UnitsPerEm() <= 0 {
		return kui.ErrInvalidConfig
	}
	runes := []rune(cfg.Runes)
	if len(runes) == 0 {
		for r := ' '; r <= '~'; r++ {
			runes = append(runes, r)
		}
	}
	f.font = src
	f.glyphs = f.glyphs[:0]
	for _, size := range cfg.Sizes {
		scale := float32(size) / float32(src.UnitsPerEm())
		for _, r := range runes {
			id := src.GlyphID(r)
			if id == 0 {
				continue // The font has no glyph for r.
			}
			g := glyph{key: glyphKey(size, id), advance: int32(kui.FontAdvance(src, id, size))}
			w, h, dx, dy := raster.GlyphBox(src, id, scale)
			g.w, g.h, g.dx, g.dy = int32(w), int32(h), int32(dx), int32(dy)
			f.glyphs = append(f.glyphs, g)
		}
	}
	slices.SortFunc(f.glyphs, func(a, b glyph) int { return cmp.Compare(a.key, b.key) })
	// Repeated runes, sizes or runes sharing a glyph rasterize once.
	f.glyphs = slices.CompactFunc(f.glyphs, func(a, b glyph) bool { return a.key == b.key })
	n := 0
	for i := range f.glyphs {
		f.glyphs[i].off = n
		n += int(f.glyphs[i].w * f.glyphs[i].h)
	}
	if cap(f.masks) < n {
		f.masks = make([]byte, n)
	}
	f.masks = f.masks[:n]

	var rast raster.ScanlineRasterizer
	var segs []lefevre.Segment
	for _, g := range f.glyphs {
		if g.w == 0 || g.h == 0 {
			continue
		}
		size, id := uint16(g.key>>16), uint16(g.key)
		scale := float32(size) / float32(src.UnitsPerEm())
		segs = raster.RasterizeGlyph(&rast, f.masks[g.off:g.off+int(g.w*g.h)], int(g.w), src, id, scale, segs)
	}
	return nil
}

// Metrics returns the distance from the top of a line to the baseline and the line height at
// size pixels. They are defined for any size, rasterized or not.
func (f *Face) Metrics(size uint16) (ascent, lineHeight int) {
	if f.font == nil {
		return 0, 0
	}
	asc, desc, gap, _ := f.font.VMetrics()
	scale := float64(size) / float64(f.font.UnitsPerEm())
	return int(math.Round(float64(asc) * scale)), int(math.Round(float64(int(asc)-int(desc)+int(gap)) * scale))
}

// Glyph returns the rasterized mask of r. ok is false if r was not rasterized at size, in which
// case advance is still r's advance so the pen moves as [kui2go.TextFaces] measures.
func (f *Face) Glyph(size uint16, r rune) (mask []byte, w, h, dx, dy, advance int, ok bool) {
	if f.font == nil {
		return nil, 0, 0, 0, 0, 0, false
	}
	id := f.font.GlyphID(r)
	i, found := slices.BinarySearchFunc(f.glyphs, glyphKey(size, id), func(g glyph, key uint32) int {
		return cmp.Compare(g.key, key)
	})
	if !found {
		return nil, 0, 0, 0, 0, kui.FontAdvance(f.font, id, size), false
	}
	g := &f.glyphs[i]
	return f.masks[g.off : g.off+int(g.w*g.h)], int(g.w), int(g.h), int(g.dx), int(g.dy), int(g.advance), true
}
