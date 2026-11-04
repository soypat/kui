package kuibatch

import "github.com/soypat/kui"

// glyphKey identifies a glyph of the atlas: rune r of a font at a size, rasterized at psize
// framebuffer pixels.
type glyphKey struct {
	font, size, psize uint16
	r                 rune
}

func (k glyphKey) hash() uint32 {
	h := uint64(k.font) | uint64(k.size)<<16 | uint64(k.psize)<<32
	h ^= uint64(uint32(k.r)) * 0x9e3779b97f4a7c15
	h ^= h >> 29
	h *= 0xbf58476d1ce4e5b9
	h ^= h >> 32
	return uint32(h)
}

// glyph is a glyph of the atlas.
type glyph struct {
	key        glyphKey
	x, y, w, h uint16 // Mask in the atlas. Empty for glyphs without pixels.
	dx, dy     int16  // Offset of the mask from the pen on the baseline, in framebuffer pixels.
	adv        int32  // Pen advance at key.size, in logical pixels.
	used       bool
}

// atlas caches glyph masks in a square single channel texture, packed in shelves. Its pixels
// are kept in memory and the region changed since the last upload is tracked, so a texture
// is updated by uploading that region only.
type atlas struct {
	pix   []byte
	side  int
	x, y  int // Packing position.
	shelf int // Height of the current shelf.
	dirty Pixels
	table []glyph // Open addressing hash table, at least twice max long.
	n     int
	max   int
}

func (a *atlas) init(side, maxGlyphs int) {
	n := 1
	for n < 2*maxGlyphs {
		n *= 2
	}
	*a = atlas{pix: make([]byte, side*side), side: side, table: make([]glyph, n), max: maxGlyphs}
}

// reset empties the atlas.
func (a *atlas) reset() {
	clear(a.table)
	a.n, a.x, a.y, a.shelf = 0, 0, 0, 0
	a.dirty = Pixels{}
}

// glyph returns the glyph of rune r of face at size, rasterizing it at psize if it is not in
// the atlas. full reports the atlas has no room for it.
func (a *atlas) glyph(face kui.FontFace, font, size, psize uint16, r rune) (g *glyph, full bool) {
	k := glyphKey{font: font, size: size, psize: psize, r: r}
	mask := uint32(len(a.table) - 1)
	for i := k.hash() & mask; ; i = (i + 1) & mask {
		g := &a.table[i]
		if !g.used {
			if a.n >= a.max {
				return nil, true
			}
			return g, !a.insert(g, face, k)
		}
		if g.key == k {
			return g, false
		}
	}
}

// insert rasterizes the glyph of k into g, reporting whether its mask fit.
func (a *atlas) insert(g *glyph, face kui.FontFace, k glyphKey) bool {
	mask, w, h, dx, dy, adv, ok := face.Glyph(k.psize, k.r)
	if !ok || w <= 0 || h <= 0 || len(mask) < w*h || w > a.side || h > a.side {
		w, h = 0, 0 // Nothing to draw, or more than any atlas holds: keep the advance only.
	}
	var x, y int
	if w > 0 {
		var fits bool
		if x, y, fits = a.place(w, h); !fits {
			return false
		}
		for row := range h {
			copy(a.pix[(y+row)*a.side+x:], mask[row*w:(row+1)*w])
		}
		r := Pixels{int32(x), int32(y), int32(x + w), int32(y + h)}
		if a.dirty.Empty() {
			a.dirty = r
		} else {
			a.dirty = a.dirty.union(r)
		}
	}
	if k.psize != k.size {
		_, _, _, _, _, adv, _ = face.Glyph(k.size, k.r) // After copying the mask, which it may reuse.
	}
	*g = glyph{key: k, x: uint16(x), y: uint16(y), w: uint16(w), h: uint16(h), dx: int16(dx), dy: int16(dy), adv: int32(adv), used: true}
	a.n++
	return true
}

// place reserves a w by h region.
func (a *atlas) place(w, h int) (x, y int, ok bool) {
	if a.x+w > a.side {
		a.x, a.y, a.shelf = 0, a.y+a.shelf, 0
	}
	if a.y+h > a.side {
		return 0, 0, false
	}
	x, y = a.x, a.y
	a.x += w
	a.shelf = max(a.shelf, h)
	return x, y, true
}
