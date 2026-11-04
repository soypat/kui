// Package kuibatch batches [kui.DrawList]s for GPU painters: shapes and glyphs become
// instanced quads, one [Instance] each, which a painter draws with one shader whatever its
// graphics API.
//
// An [Encoder] turns a draw list into chunks that fit its buffers. Text and shapes share
// draws, split into [Batch]es only when an image texture changes; a chunk ends when the
// instance buffer fills, the glyph atlas fills or a custom command comes, and is handed to the
// painter's [Sink], which uploads the chunk's glyphs and images and draws its batches. Glyphs
// are rasterized by the [kui.FontFace]s of the configuration into an atlas on first use.
// Resources drawn by a chunk are not replaced before it is submitted.
//
// Quads cover only pixels within the clip and the command's [kui.DrawCmd.Bounds], so painters
// never draw outside them, keeping damage tracking exact. Coverage and blending are computed
// by the painters' shaders, from the instance fields documented on [Instance].
//
// The encoder does not allocate after [Encoder.Configure].
package kuibatch

import (
	"math"
	"unicode/utf8"
	"unsafe"

	"github.com/soypat/kui"
)

// Instance kinds, the value of [Instance.Kind].
const (
	KindRect = iota
	KindBorder
	KindCircle
	KindLine
	KindGlyph
	KindImage
)

// Instance is a shape or glyph, drawn as one instanced quad. Geometry is in framebuffer pixels
// with the origin at the top left corner. Its layout is shared with the painters' shaders.
type Instance struct {
	Rect  [4]float32 // Shape: x, y, width, height. Lines: start x, y and extent x, y.
	Quad  [4]float32 // Pixels drawn: x0, y0, x1, y1, integers within the clip and the command's bounds.
	Radii [4]float32 // Corner radii: top left, top right, bottom left, bottom right.
	// Param is the border widths (left, right, top, bottom) of borders, the half width of lines,
	// the offset from framebuffer to atlas pixels of glyphs and the pixel size of images.
	Param [4]float32
	Color [4]uint8 // Non premultiplied RGBA.
	Kind  float32
}

// InstanceSize is the size in bytes of an [Instance].
const InstanceSize = int(unsafe.Sizeof(Instance{}))

// Batch is a run of instances drawn with one call.
type Batch struct {
	First, Count int
	Image        int // Image slot sampled, or -1.
}

// Pixels is a rectangle of framebuffer pixels [X0, X1) by [Y0, Y1).
type Pixels struct{ X0, Y0, X1, Y1 int32 }

// Empty reports whether r holds no pixels.
func (r Pixels) Empty() bool { return r.X0 >= r.X1 || r.Y0 >= r.Y1 }

// Intersect returns the pixels in both r and s.
func (r Pixels) Intersect(s Pixels) Pixels {
	r.X0, r.Y0 = max(r.X0, s.X0), max(r.Y0, s.Y0)
	r.X1, r.Y1 = min(r.X1, s.X1), min(r.Y1, s.Y1)
	return r
}

func (r Pixels) union(s Pixels) Pixels {
	return Pixels{min(r.X0, s.X0), min(r.Y0, s.Y0), max(r.X1, s.X1), max(r.Y1, s.Y1)}
}

func (r Pixels) f32() [4]float32 {
	return [4]float32{float32(r.X0), float32(r.Y0), float32(r.X1), float32(r.Y1)}
}

// PixelsOf returns the framebuffer pixels touched by r, in logical pixels, at scale
// framebuffer pixels per logical pixel.
func PixelsOf(r kui.Rect, scale float32) Pixels {
	s := scale
	return Pixels{floor(r.X * s), floor(r.Y * s), ceil((r.X + r.W) * s), ceil((r.Y + r.H) * s)}
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

// Sink draws encoded chunks for an [Encoder].
type Sink interface {
	// Submit uploads the glyphs and images of e's chunk, with [Encoder.AtlasUpload] and
	// [Encoder.ImageUpload], and draws its [Encoder.Batches] of [Encoder.Instances].
	Submit(e *Encoder)
	// Custom paints custom command c, clipped to clip.
	Custom(c *kui.DrawCmd, clip Pixels)
}

// Config configures an [Encoder].
type Config struct {
	// Faces rasterize the text of fonts, indexed by font.
	Faces []kui.FontFace
	// Images resolves image commands. May be nil. An image is uploaded the first time it is
	// drawn: call [Encoder.InvalidateImage] when its pixels change. Image rows must be a
	// multiple of 4 bytes apart.
	Images kui.ImageSource
	// MaxInstances is the number of shapes and glyphs of a chunk. Lists with more are drawn in
	// several. Defaults to 4096.
	MaxInstances int
	// AtlasSize is the side in pixels of the single channel glyph atlas. Defaults to 1024.
	AtlasSize int
	// MaxGlyphs is the number of glyphs the atlas holds. Defaults to 2048. When the atlas fills
	// it is emptied and glyphs are rasterized again.
	MaxGlyphs int
	// MaxImages is the number of image texture slots. Defaults to 16.
	MaxImages int
}

// Encoder turns draw commands into instances, in chunks that fit its buffers. Configure it
// with [Encoder.Configure].
type Encoder struct {
	faces     []kui.FontFace
	src       kui.ImageSource
	scale     float32
	inst      []Instance
	batches   []Batch
	clip      [32]Pixels
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

// Configure allocates the buffers of e, applying the defaults of cfg's zero fields. It
// returns cfg with them applied, for painters to size their GPU resources.
func (e *Encoder) Configure(cfg Config) Config {
	if cfg.MaxInstances <= 0 {
		cfg.MaxInstances = 4096
	}
	if cfg.AtlasSize <= 0 {
		cfg.AtlasSize = 1024
	}
	if cfg.MaxGlyphs <= 0 {
		cfg.MaxGlyphs = 2048
	}
	if cfg.MaxImages <= 0 {
		cfg.MaxImages = 16
	}
	*e = Encoder{
		faces:   cfg.Faces,
		src:     cfg.Images,
		inst:    make([]Instance, 0, cfg.MaxInstances),
		batches: make([]Batch, 0, cfg.MaxInstances),
		images:  make([]imageSlot, cfg.MaxImages),
		chunk:   1,
	}
	e.atlas.init(cfg.AtlasSize, cfg.MaxGlyphs)
	return cfg
}

// Instances returns the instances of the chunk being submitted.
func (e *Encoder) Instances() []Instance { return e.inst }

// Batches returns the batches of the chunk being submitted.
func (e *Encoder) Batches() []Batch { return e.batches }

// AtlasSide returns the side in pixels of the glyph atlas.
func (e *Encoder) AtlasSide() int { return e.atlas.side }

// AtlasUpload returns the region of the atlas changed since the last call, empty if none, and
// the atlas pixels: one byte of coverage each, rows [AtlasSide] bytes apart. The region is
// then considered uploaded.
func (e *Encoder) AtlasUpload() (r Pixels, pix []byte) {
	r = e.atlas.dirty
	e.atlas.dirty = Pixels{}
	return r, e.atlas.pix
}

// ImageUpload returns an image the chunk draws whose pixels must be uploaded to its slot:
// non premultiplied RGBA rows, stride bytes apart, of w by h pixels. The image is then
// considered uploaded. It returns ok false when no uploads are left. Images whose source
// changed since they were encoded are skipped, and uploaded again when next drawn.
func (e *Encoder) ImageUpload() (slot int, pix []byte, stride, w, h int, ok bool) {
	for i := range e.images {
		s := &e.images[i]
		if !s.upload {
			continue
		}
		s.upload = false
		pix, stride, w, h, ok = e.lookImage(s.id)
		if !ok || w != s.w || h != s.h {
			s.used = false // The source changed it since it was encoded.
			continue
		}
		return i, pix, stride, w, h, true
	}
	return -1, nil, 0, 0, 0, false
}

// InvalidateImage discards the texture of image id, so its pixels are uploaded again the
// next time it is drawn.
func (e *Encoder) InvalidateImage(id uint32) {
	for i := range e.images {
		if s := &e.images[i]; s.used && s.id == id {
			s.used = false
		}
	}
}

// Resets returns the number of times the glyph atlas filled up and was emptied.
func (e *Encoder) Resets() int { return e.resets }

// Dropped returns the number of commands or glyphs not drawn in full for lack of room.
func (e *Encoder) Dropped() int { return e.dropped }

// Run encodes l for a framebuffer of width by height pixels, scale pixels per logical pixel,
// handing each chunk to s.
func (e *Encoder) Run(l *kui.DrawList, width, height int, scale float32, s Sink) {
	e.scale = scale
	e.clip[0] = Pixels{0, 0, int32(width), int32(height)}
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
			s.Custom(&cmds[next], e.top())
			next++
		}
		i = next
	}
}

// flush submits the chunk, if it draws anything, and starts the next one.
func (e *Encoder) flush(s Sink) {
	if len(e.inst) > 0 {
		s.Submit(e)
	}
	e.inst, e.batches = e.inst[:0], e.batches[:0]
	e.full = false
	e.chunk++
}

// encode adds the instances of commands from i on to the chunk until it must end, returning
// the command to continue with.
func (e *Encoder) encode(l *kui.DrawList, i int) (next int, why stop) {
	cmds := l.Cmds()
	for ; i < len(cmds); i++ {
		c := &cmds[i]
		n, nb := len(e.inst), len(e.batches)
		lastImage := -1
		if nb > 0 {
			lastImage = e.batches[nb-1].Image
		}
		why = stopEnd
		switch c.Op {
		case kui.DrawRect:
			e.shape(c, KindRect)
		case kui.DrawBorder:
			e.shape(c, KindBorder)
		case kui.DrawCircle:
			e.shape(c, KindCircle)
		case kui.DrawLine:
			e.shape(c, KindLine)
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
			b.Count, b.Image = n-b.First, lastImage
		}
		return i, why
	}
	return i, stopEnd
}

// add appends instance in, sampling image slot image if not negative. It reports false if
// the chunk is full.
func (e *Encoder) add(in *Instance, image int) bool {
	if len(e.inst) == cap(e.inst) {
		e.full = true
		return false
	}
	nb := len(e.batches)
	if nb == 0 || (image >= 0 && e.batches[nb-1].Image >= 0 && e.batches[nb-1].Image != image) {
		e.batches = append(e.batches, Batch{First: len(e.inst), Image: image})
		nb++
	} else if image >= 0 {
		e.batches[nb-1].Image = image
	}
	e.batches[nb-1].Count++
	e.inst = append(e.inst, *in)
	return true
}

// top returns the current clip.
func (e *Encoder) top() Pixels { return e.clip[min(e.depth, len(e.clip)-1)] }

// pushClip intersects the clip with r. Clips nested deeper than the stack keep the deepest
// one stored.
func (e *Encoder) pushClip(r kui.Rect) {
	if e.depth+1 < len(e.clip) {
		e.clip[e.depth+1] = e.pixels(r).Intersect(e.clip[e.depth])
	}
	e.depth++
}

// pixels returns the framebuffer pixels touched by r, in logical pixels.
func (e *Encoder) pixels(r kui.Rect) Pixels { return PixelsOf(r, e.scale) }

// coordLimit bounds pixel coordinates so they convert to int32 exactly.
const coordLimit = 1 << 24

func floor(v float32) int32 {
	return int32(math.Floor(math.Max(math.Min(float64(v), coordLimit), -coordLimit)))
}

func ceil(v float32) int32 {
	return int32(math.Ceil(math.Max(math.Min(float64(v), coordLimit), -coordLimit)))
}

func rgba(c kui.Color) [4]uint8 { return [4]uint8{c.R, c.G, c.B, c.A} }

func (e *Encoder) rect(r kui.Rect) [4]float32 {
	s := e.scale
	return [4]float32{r.X * s, r.Y * s, r.W * s, r.H * s}
}

func (e *Encoder) radii(r kui.DrawRadii) [4]float32 {
	s := e.scale
	return [4]float32{float32(r[0]) * s, float32(r[1]) * s, float32(r[2]) * s, float32(r[3]) * s}
}

// shape adds a rectangle, border, circle or line.
func (e *Encoder) shape(c *kui.DrawCmd, kind int) {
	if c.Color.A == 0 {
		return
	}
	q := e.pixels(c.Bounds()).Intersect(e.top())
	if q.Empty() {
		return
	}
	in := Instance{Rect: e.rect(c.Rect), Quad: q.f32(), Color: rgba(c.Color), Kind: float32(kind)}
	s := e.scale
	switch kind {
	case KindRect:
		in.Radii = e.radii(c.Radii)
	case KindBorder:
		in.Radii = e.radii(c.Radii)
		w := c.BorderWidths()
		in.Param = [4]float32{float32(w.Left) * s, float32(w.Right) * s, float32(w.Top) * s, float32(w.Bottom) * s}
	case KindLine:
		in.Param[0] = max(c.LineWidth(), 1) / 2 * s
	}
	e.add(&in, -1)
}

// text adds the glyphs of text command c with text s, command i of the list.
func (e *Encoder) text(c *kui.DrawCmd, s []byte, i int) stop {
	font, size, spacing, _ := c.TextStyle()
	if int(font) >= len(e.faces) || e.faces[font] == nil || c.Color.A == 0 {
		return stopEnd
	}
	face := e.faces[font]
	// Glyphs may extend past their line box, but never past the command's bounds.
	bounds := e.pixels(c.Bounds()).Intersect(e.top())
	if bounds.Empty() {
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
			q := Pixels{gx, gy, gx + int32(g.w), gy + int32(g.h)}.Intersect(bounds)
			if !q.Empty() {
				in := Instance{
					Quad:  q.f32(),
					Param: [4]float32{float32(int32(g.x) - gx), float32(int32(g.y) - gy)},
					Color: col,
					Kind:  KindGlyph,
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
func (e *Encoder) image(c *kui.DrawCmd) stop {
	tint := c.Color
	if tint == (kui.Color{}) {
		tint = kui.RGB(255, 255, 255) // Zero tint means untinted.
	}
	if e.src == nil || tint.A == 0 || c.Rect.W <= 0 || c.Rect.H <= 0 {
		return stopEnd
	}
	q := e.pixels(c.Bounds()).Intersect(e.top())
	if q.Empty() {
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
	in := Instance{
		Rect:  e.rect(c.Rect),
		Quad:  q.f32(),
		Radii: e.radii(c.Radii),
		Param: [4]float32{float32(img.w), float32(img.h)},
		Color: rgba(tint),
		Kind:  KindImage,
	}
	e.add(&in, slot)
	return stopEnd
}

// lookImage returns the pixels of image id if they are valid RGBA8 rows that can be uploaded:
// stride must be a multiple of 4.
func (e *Encoder) lookImage(id uint32) (pix []byte, stride, w, h int, ok bool) {
	pix, stride, w, h, ok = e.src.Image(id)
	if !ok || w <= 0 || h <= 0 || stride < 4*w || stride%4 != 0 || len(pix) < (h-1)*stride+4*w {
		return nil, 0, 0, 0, false
	}
	return pix, stride, w, h, true
}

// evict returns an image slot not used by the chunk, or -1.
func (e *Encoder) evict() int {
	for k := range e.images {
		j := (e.nextImage + k) % len(e.images)
		if !e.images[j].used || e.images[j].chunk != e.chunk {
			e.nextImage = j + 1
			return j
		}
	}
	return -1
}
