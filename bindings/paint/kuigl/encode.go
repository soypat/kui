package kuigl

import (
	"math"
	"unicode/utf8"
	"unsafe"

	"github.com/soypat/kui"
)

// Instance kinds, as tested by the fragment shader.
const (
	kindRect = iota
	kindBorder
	kindCircle
	kindLine
	kindGlyph
	kindImage
)

// instance is a shape or glyph, drawn as one instanced quad. Geometry is in framebuffer pixels
// with the origin at the top left corner.
type instance struct {
	Rect  [4]float32 // Shape: x, y, width, height. Lines: start x, y and extent x, y.
	Quad  [4]float32 // Pixels drawn: x0, y0, x1, y1, integers within the clip and the command's bounds.
	Radii [4]float32 // Corner radii: top left, top right, bottom left, bottom right.
	// Param is the border widths (left, right, top, bottom) of borders, the half width of lines,
	// the offset from framebuffer to atlas pixels of glyphs and the pixel size of images.
	Param [4]float32
	Color [4]uint8 // Non premultiplied RGBA.
	Kind  float32
}

const instanceSize = int(unsafe.Sizeof(instance{}))

// batch is a run of instances drawn with one call.
type batch struct {
	first, count int
	image        int // Image slot sampled, or -1.
}

// irect is a rectangle of pixels [x0, x1) by [y0, y1).
type irect struct{ x0, y0, x1, y1 int32 }

func (r irect) empty() bool { return r.x0 >= r.x1 || r.y0 >= r.y1 }

func (r irect) intersect(s irect) irect {
	r.x0, r.y0 = max(r.x0, s.x0), max(r.y0, s.y0)
	r.x1, r.y1 = min(r.x1, s.x1), min(r.y1, s.y1)
	return r
}

func (r irect) union(s irect) irect {
	return irect{min(r.x0, s.x0), min(r.y0, s.y0), max(r.x1, s.x1), max(r.y1, s.y1)}
}

func (r irect) f32() [4]float32 {
	return [4]float32{float32(r.x0), float32(r.y0), float32(r.x1), float32(r.y1)}
}

// stop is why encoding a chunk ended.
type stop uint8

const (
	stopEnd    stop = iota // No commands left.
	stopFull               // Instance buffer full.
	stopAtlas              // Glyph atlas full.
	stopImages             // Every image slot is used by the chunk.
	stopCustom             // Custom command, painted by the user.
)

// imageSlot is an image texture.
type imageSlot struct {
	id     uint32
	w, h   int
	used   bool
	upload bool   // Pixels must be uploaded before the chunk is drawn.
	chunk  uint32 // Last chunk drawing the image.
}

// sink draws encoded chunks.
type sink interface {
	// submit uploads the glyphs and images of e's chunk and draws its batches.
	submit(e *encoder)
	// custom paints custom command c, clipped to clip.
	custom(c *kui.DrawCmd, clip irect)
}

// encoder turns draw commands into instances, in chunks that fit its buffers. A chunk ends
// when a buffer is full or a custom command comes; resources drawn by a chunk are not
// replaced before it is drawn.
type encoder struct {
	faces     []kui.FontFace
	src       kui.ImageSource
	scale     float32
	inst      []instance
	batches   []batch
	clip      [32]irect
	depth     int // Clip pushes, which may exceed len(clip).
	full      bool
	atlas     atlas
	images    []imageSlot
	nextImage int
	chunk     uint32
	freshAt   int // Command the atlas was last emptied for, or -1.
	dropped   int
	resets    int
}

func (e *encoder) init(faces []kui.FontFace, src kui.ImageSource, maxInstances, atlasSide, maxGlyphs, maxImages int) {
	*e = encoder{
		faces:   faces,
		src:     src,
		inst:    make([]instance, 0, maxInstances),
		batches: make([]batch, 0, maxInstances),
		images:  make([]imageSlot, maxImages),
		chunk:   1,
	}
	e.atlas.init(atlasSide, maxGlyphs)
}

// run encodes l for a framebuffer of width by height pixels, scale pixels per logical pixel,
// handing each chunk to s.
func (e *encoder) run(l *kui.DrawList, width, height int, scale float32, s sink) {
	e.scale = scale
	e.clip[0] = irect{0, 0, int32(width), int32(height)}
	e.depth = 0
	e.freshAt = -1
	cmds := l.Cmds()
	for i := 0; i < len(cmds); {
		next, why := e.encode(l, i)
		e.flush(s)
		switch why {
		case stopAtlas:
			e.atlas.reset()
			e.resets++
			e.freshAt = next
		case stopCustom:
			s.custom(&cmds[next], e.top())
			next++
		}
		i = next
	}
}

// flush submits the chunk, if it draws anything, and starts the next one.
func (e *encoder) flush(s sink) {
	if len(e.inst) > 0 {
		s.submit(e)
	}
	e.inst, e.batches = e.inst[:0], e.batches[:0]
	e.full = false
	e.chunk++
}

// encode adds the instances of commands from i on to the chunk until it must end, returning
// the command to continue with.
func (e *encoder) encode(l *kui.DrawList, i int) (next int, why stop) {
	cmds := l.Cmds()
	for ; i < len(cmds); i++ {
		c := &cmds[i]
		n, nb := len(e.inst), len(e.batches)
		lastImage := -1
		if nb > 0 {
			lastImage = e.batches[nb-1].image
		}
		why = stopEnd
		switch c.Op {
		case kui.DrawRect:
			e.shape(c, kindRect)
		case kui.DrawBorder:
			e.shape(c, kindBorder)
		case kui.DrawCircle:
			e.shape(c, kindCircle)
		case kui.DrawLine:
			e.shape(c, kindLine)
		case kui.DrawText:
			why = e.text(c, l.TextOf(c), i)
		case kui.DrawImage:
			why = e.image(c)
		case kui.DrawClipPush:
			e.pushClip(c.Rect)
		case kui.DrawClipPop:
			if e.depth > 0 {
				e.depth--
			}
		case kui.DrawCustom:
			return i, stopCustom
		}
		if e.full {
			why = stopFull
		}
		switch {
		case why == stopEnd:
			continue
		case why == stopFull && n == 0:
			// The command alone does not fit an empty chunk: keep what fit.
			e.dropped++
			return i + 1, why
		}
		// Encode the command again in the next chunk.
		e.inst, e.batches = e.inst[:n], e.batches[:nb]
		if nb > 0 {
			b := &e.batches[nb-1]
			b.count, b.image = n-b.first, lastImage
		}
		return i, why
	}
	return i, stopEnd
}

// add appends instance in, sampling image slot image if not negative. It reports false if
// the chunk is full.
func (e *encoder) add(in *instance, image int) bool {
	if len(e.inst) == cap(e.inst) {
		e.full = true
		return false
	}
	nb := len(e.batches)
	if nb == 0 || (image >= 0 && e.batches[nb-1].image >= 0 && e.batches[nb-1].image != image) {
		e.batches = append(e.batches, batch{first: len(e.inst), image: image})
		nb++
	} else if image >= 0 {
		e.batches[nb-1].image = image
	}
	e.batches[nb-1].count++
	e.inst = append(e.inst, *in)
	return true
}

// top returns the current clip.
func (e *encoder) top() irect { return e.clip[min(e.depth, len(e.clip)-1)] }

// pushClip intersects the clip with r. Clips nested deeper than the stack keep the deepest
// one stored.
func (e *encoder) pushClip(r kui.Rect) {
	if e.depth+1 < len(e.clip) {
		e.clip[e.depth+1] = e.pixels(r).intersect(e.clip[e.depth])
	}
	e.depth++
}

// pixels returns the framebuffer pixels touched by r, in logical pixels.
func (e *encoder) pixels(r kui.Rect) irect {
	s := e.scale
	return irect{floor(r.X * s), floor(r.Y * s), ceil((r.X + r.W) * s), ceil((r.Y + r.H) * s)}
}

// coordLimit bounds pixel coordinates so they convert to int32 exactly.
const coordLimit = 1 << 24

func floor(v float32) int32 {
	return int32(math.Floor(math.Max(math.Min(float64(v), coordLimit), -coordLimit)))
}

func ceil(v float32) int32 {
	return int32(math.Ceil(math.Max(math.Min(float64(v), coordLimit), -coordLimit)))
}

func rgba(c kui.Color) [4]uint8 { return [4]uint8{c.R, c.G, c.B, c.A} }

func (e *encoder) rect(r kui.Rect) [4]float32 {
	s := e.scale
	return [4]float32{r.X * s, r.Y * s, r.W * s, r.H * s}
}

func (e *encoder) radii(r kui.DrawRadii) [4]float32 {
	s := e.scale
	return [4]float32{float32(r[0]) * s, float32(r[1]) * s, float32(r[2]) * s, float32(r[3]) * s}
}

// shape adds a rectangle, border, circle or line.
func (e *encoder) shape(c *kui.DrawCmd, kind int) {
	if c.Color.A == 0 {
		return
	}
	q := e.pixels(c.Bounds()).intersect(e.top())
	if q.empty() {
		return
	}
	in := instance{Rect: e.rect(c.Rect), Quad: q.f32(), Color: rgba(c.Color), Kind: float32(kind)}
	s := e.scale
	switch kind {
	case kindRect:
		in.Radii = e.radii(c.Radii)
	case kindBorder:
		in.Radii = e.radii(c.Radii)
		w := c.BorderWidths()
		in.Param = [4]float32{float32(w.Left) * s, float32(w.Right) * s, float32(w.Top) * s, float32(w.Bottom) * s}
	case kindLine:
		in.Param[0] = max(c.LineWidth(), 1) / 2 * s
	}
	e.add(&in, -1)
}

// text adds the glyphs of text command c with text s, command i of the list.
func (e *encoder) text(c *kui.DrawCmd, s []byte, i int) stop {
	font, size, spacing, _ := c.TextStyle()
	if int(font) >= len(e.faces) || e.faces[font] == nil || c.Color.A == 0 {
		return stopEnd
	}
	face := e.faces[font]
	// Glyphs may extend past their line box, but never past the command's bounds.
	bounds := e.pixels(c.Bounds()).intersect(e.top())
	if bounds.empty() {
		return stopEnd
	}
	sc := e.scale
	psize := size
	if sc != 1 {
		psize = uint16(min(math.Round(float64(size)*float64(sc)), math.MaxUint16))
	}
	ascent, lineHeight := face.Metrics(psize)
	// Center the face's line within the command's line box.
	y, h := c.Rect.Y*sc, c.Rect.H*sc
	baseline := int32(math.Round(float64(y+(h-float32(lineHeight))/2))) + int32(ascent)
	col := rgba(c.Color)
	penX := c.Rect.X // In logical pixels, like the advances measured by layout.
	for len(s) > 0 {
		r, n := utf8.DecodeRune(s)
		s = s[n:]
		g, full := e.atlas.glyph(face, font, size, psize, r)
		if full {
			if e.freshAt != i {
				return stopAtlas
			}
			// Even the empty atlas cannot hold every glyph of c: skip those that do not fit.
			_, _, _, _, _, adv, _ := face.Glyph(size, r)
			penX += float32(adv + int(spacing))
			e.dropped++
			continue
		}
		if g.w > 0 {
			gx := int32(math.Round(float64(penX*sc))) + int32(g.dx)
			gy := baseline + int32(g.dy)
			q := irect{gx, gy, gx + int32(g.w), gy + int32(g.h)}.intersect(bounds)
			if !q.empty() {
				in := instance{
					Quad:  q.f32(),
					Param: [4]float32{float32(int32(g.x) - gx), float32(int32(g.y) - gy)},
					Color: col,
					Kind:  kindGlyph,
				}
				if !e.add(&in, -1) {
					return stopFull
				}
			}
		}
		penX += float32(int(g.adv) + int(spacing))
	}
	return stopEnd
}

// image adds image command c.
func (e *encoder) image(c *kui.DrawCmd) stop {
	tint := c.Color
	if tint == (kui.Color{}) {
		tint = kui.RGB(255, 255, 255) // Zero tint means untinted.
	}
	if e.src == nil || tint.A == 0 || c.Rect.W <= 0 || c.Rect.H <= 0 {
		return stopEnd
	}
	q := e.pixels(c.Bounds()).intersect(e.top())
	if q.empty() {
		return stopEnd
	}
	id := c.ImageID()
	slot := -1
	for j := range e.images {
		if e.images[j].used && e.images[j].id == id {
			slot = j
			break
		}
	}
	if slot < 0 {
		_, _, w, h, ok := e.lookImage(id)
		if !ok {
			return stopEnd
		}
		if slot = e.evict(); slot < 0 {
			return stopImages
		}
		e.images[slot] = imageSlot{id: id, w: w, h: h, used: true, upload: true}
	}
	img := &e.images[slot]
	img.chunk = e.chunk
	in := instance{
		Rect:  e.rect(c.Rect),
		Quad:  q.f32(),
		Radii: e.radii(c.Radii),
		Param: [4]float32{float32(img.w), float32(img.h)},
		Color: rgba(tint),
		Kind:  kindImage,
	}
	e.add(&in, slot)
	return stopEnd
}

// lookImage returns the pixels of image id if they are valid RGBA8 rows that can be uploaded:
// stride must be a multiple of 4.
func (e *encoder) lookImage(id uint32) (pix []byte, stride, w, h int, ok bool) {
	pix, stride, w, h, ok = e.src.Image(id)
	if !ok || w <= 0 || h <= 0 || stride < 4*w || stride%4 != 0 || len(pix) < (h-1)*stride+4*w {
		return nil, 0, 0, 0, false
	}
	return pix, stride, w, h, true
}

// evict returns an image slot not used by the chunk, or -1.
func (e *encoder) evict() int {
	for k := range e.images {
		j := (e.nextImage + k) % len(e.images)
		if !e.images[j].used || e.images[j].chunk != e.chunk {
			e.nextImage = j + 1
			return j
		}
	}
	return -1
}
