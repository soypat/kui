package kuigl

import (
	"slices"
	"testing"

	"github.com/soypat/kui"
)

// testFace has solid glyphs of size/2 by size pixels whose coverage is the rune plus the size,
// advancing size/2+1. Spaces have no glyph. It reuses its mask buffer like faces may.
type testFace struct {
	buf   [64 * 128]byte
	calls int
}

func (f *testFace) Metrics(size uint16) (ascent, lineHeight int) {
	return int(size) * 3 / 4, int(size)
}

func (f *testFace) Glyph(size uint16, r rune) (mask []byte, w, h, dx, dy, advance int, ok bool) {
	f.calls++
	w, h = int(size)/2, int(size)
	if r == ' ' || w*h > len(f.buf) {
		return nil, 0, 0, 0, 0, w + 1, false
	}
	for i := range w * h {
		f.buf[i] = byte(r) + byte(size)
	}
	return f.buf[:w*h], w, h, 0, -int(size) * 3 / 4, w + 1, true
}

// testImages holds 2 by 2 images: red, green, blue and white pixels, tinted by id.
type testImages struct{ pix [4][16]byte }

func (im *testImages) Image(id uint32) (pix []byte, stride, w, h int, ok bool) {
	if id == 0 || int(id) > len(im.pix) {
		return nil, 0, 0, 0, false
	}
	p := &im.pix[id-1]
	copy(p[:], []byte{255, 0, 0, 255, 0, 255, 0, 255, 0, 0, 255, 255, 255, 255, 255, uint8(id * 50)})
	return p[:], 8, 2, 2, true
}

// recSink records the chunks submitted, doing the bookkeeping of the GL sink.
type recSink struct {
	chunks  [][]instance
	batches [][]batch
	uploads []int // Image slots uploaded.
	atlas   int   // Atlas uploads.
	customs []irect
}

func (r *recSink) submit(e *encoder) {
	r.chunks = append(r.chunks, slices.Clone(e.inst))
	r.batches = append(r.batches, slices.Clone(e.batches))
	if !e.atlas.dirty.empty() {
		r.atlas++
		e.atlas.dirty = irect{}
	}
	for i := range e.images {
		if e.images[i].upload {
			e.images[i].upload = false
			r.uploads = append(r.uploads, i)
		}
	}
}

func (r *recSink) custom(c *kui.DrawCmd, clip irect) { r.customs = append(r.customs, clip) }

func (r *recSink) all() []instance { return slices.Concat(r.chunks...) }

type encConfig struct {
	maxInstances, atlasSide, maxGlyphs, maxImages int
}

func newEncoder(cfg encConfig) (*encoder, *testFace) {
	if cfg.maxInstances == 0 {
		cfg.maxInstances = 256
	}
	if cfg.atlasSide == 0 {
		cfg.atlasSide = 256
	}
	if cfg.maxGlyphs == 0 {
		cfg.maxGlyphs = 64
	}
	if cfg.maxImages == 0 {
		cfg.maxImages = 4
	}
	face := new(testFace)
	e := new(encoder)
	e.init([]kui.FontFace{face}, new(testImages), cfg.maxInstances, cfg.atlasSide, cfg.maxGlyphs, cfg.maxImages)
	return e, face
}

func newList() *kui.DrawList {
	l := new(kui.DrawList)
	l.Reset(make([]kui.DrawCmd, 0, 64), make([]byte, 0, 512))
	return l
}

var (
	white = kui.RGB(255, 255, 255)
	red   = kui.RGB(255, 0, 0)
)

// singles adds one command of each drawing op to l.
var singles = []func(l *kui.DrawList){
	func(l *kui.DrawList) { l.Rect(1, kui.Rect{X: 3.5, Y: 4.25, W: 20, H: 10.5}, red, kui.Radius(4)) },
	func(l *kui.DrawList) {
		l.Border(1, kui.Rect{X: 2, Y: 3, W: 30, H: 12}, red, kui.DrawBorders{Left: 1, Right: 2, Top: 3, Bottom: 1}, kui.DrawRadii{6, 0, 2, 9})
	},
	func(l *kui.DrawList) { l.Circle(1, kui.Point{X: 20.5, Y: 15}, 7.25, red) },
	func(l *kui.DrawList) { l.Line(1, kui.Point{X: 40, Y: 2}, kui.Point{X: 5, Y: 30}, 3, red) },
	func(l *kui.DrawList) { l.Text(1, kui.Rect{X: 1.4, Y: 2, W: 40, H: 13}, white, "Hi g", 0, 13, 1, 0) },
	func(l *kui.DrawList) { l.Image(1, kui.Rect{X: 5, Y: 6, W: 9.5, H: 7}, 1, kui.Color{}, kui.Radius(2)) },
}

// TestBounds checks no quad leaves its command's bounds, at several scales and clips.
func TestBounds(t *testing.T) {
	for _, scale := range []float32{1, 1.5, 2} {
		for _, clip := range []kui.Rect{{}, {X: 6, Y: 5, W: 13, H: 7}} {
			for i, add := range singles {
				e, _ := newEncoder(encConfig{})
				l := newList()
				if clip != (kui.Rect{}) {
					l.PushClip(0, clip)
				}
				add(l)
				cmd := l.Cmds()[len(l.Cmds())-1]
				var s recSink
				const width, height = 100, 80
				e.run(l, width, height, scale, &s)
				insts := s.all()
				if len(insts) == 0 {
					t.Fatalf("scale %v command %d: nothing drawn", scale, i)
				}
				e.scale = scale
				want := e.pixels(cmd.Bounds()).intersect(irect{0, 0, width, height})
				if clip != (kui.Rect{}) {
					want = want.intersect(e.pixels(clip))
				}
				for _, in := range insts {
					q := irect{int32(in.Quad[0]), int32(in.Quad[1]), int32(in.Quad[2]), int32(in.Quad[3])}
					if q.empty() || q.intersect(want) != q {
						t.Errorf("scale %v clip %v command %d (%v): quad %v outside %v", scale, clip, i, cmd.Op, q, want)
					}
				}
			}
		}
	}
}

func TestShapes(t *testing.T) {
	e, _ := newEncoder(encConfig{})
	l := newList()
	l.Rect(1, kui.Rect{X: 1, Y: 2, W: 3, H: 4}, red, kui.DrawRadii{1, 2, 3, 4})
	l.Border(1, kui.Rect{X: 1, Y: 2, W: 3, H: 4}, red, kui.DrawBorders{Left: 1, Right: 2, Top: 3, Bottom: 4}, kui.Radius(1))
	l.Line(1, kui.Point{X: 1, Y: 2}, kui.Point{X: 4, Y: 6}, 0.5, red)
	var s recSink
	e.run(l, 50, 50, 2, &s)
	in := s.all()
	if len(in) != 3 {
		t.Fatalf("%d instances, want 3", len(in))
	}
	if in[0].Rect != [4]float32{2, 4, 6, 8} || in[0].Radii != [4]float32{2, 4, 6, 8} || in[0].Quad != [4]float32{2, 4, 8, 12} || in[0].Kind != kindRect {
		t.Errorf("rect %+v", in[0])
	}
	if in[0].Color != [4]uint8{255, 0, 0, 255} {
		t.Errorf("rect color %v", in[0].Color)
	}
	if in[1].Param != [4]float32{2, 4, 6, 8} || in[1].Kind != kindBorder {
		t.Errorf("border %+v", in[1])
	}
	if in[2].Rect != [4]float32{2, 4, 6, 8} || in[2].Param[0] != 1 || in[2].Kind != kindLine { // Width at least 1.
		t.Errorf("line %+v", in[2])
	}
}

func TestSkip(t *testing.T) {
	e, _ := newEncoder(encConfig{})
	l := newList()
	l.Rect(1, kui.Rect{X: 1, Y: 1, W: 5, H: 5}, kui.RGBA(255, 0, 0, 0), kui.DrawRadii{}) // Transparent.
	l.Rect(1, kui.Rect{X: 60, Y: 1, W: 5, H: 5}, red, kui.DrawRadii{})                   // Off screen.
	l.Rect(1, kui.Rect{X: 1, Y: 1, W: 0, H: 5}, red, kui.DrawRadii{})                    // Empty.
	l.Text(1, kui.Rect{X: 1, Y: 1, W: 20, H: 13}, white, "x", 1, 13, 0, 0)               // No font 1.
	l.Image(1, kui.Rect{X: 1, Y: 1, W: 5, H: 5}, 99, white, kui.DrawRadii{})             // No image 99.
	l.PushClip(1, kui.Rect{X: 30, Y: 30, W: 5, H: 5})
	l.Rect(1, kui.Rect{X: 1, Y: 1, W: 5, H: 5}, red, kui.DrawRadii{}) // Clipped out.
	l.PopClip(1)
	var s recSink
	e.run(l, 50, 50, 1, &s)
	if len(s.chunks) != 0 {
		t.Fatalf("drew %+v", s.all())
	}
}

func TestClip(t *testing.T) {
	e, _ := newEncoder(encConfig{})
	l := newList()
	full := kui.Rect{W: 100, H: 100}
	l.PushClip(1, kui.Rect{X: 10.5, Y: 10, W: 20, H: 20})
	l.Rect(1, full, red, kui.DrawRadii{}) // [10, 31) x [10, 30).
	l.PushClip(1, kui.Rect{X: 20, Y: 0, W: 50, H: 15})
	l.Rect(1, full, red, kui.DrawRadii{}) // [20, 31) x [10, 15).
	l.PopClip(1)
	l.Rect(1, full, red, kui.DrawRadii{}) // [10, 31) x [10, 30).
	l.PopClip(1)
	l.PopClip(1)                          // Unbalanced.
	l.Rect(1, full, red, kui.DrawRadii{}) // Screen.
	var s recSink
	e.run(l, 64, 48, 1, &s)
	want := [][4]float32{{10, 10, 31, 30}, {20, 10, 31, 15}, {10, 10, 31, 30}, {0, 0, 64, 48}}
	got := s.all()
	if len(got) != len(want) {
		t.Fatalf("%d instances, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Quad != want[i] {
			t.Errorf("quad %d: %v, want %v", i, got[i].Quad, want[i])
		}
	}

	// Clips nested deeper than the stack keep the deepest stored one.
	l = newList()
	l.Reset(make([]kui.DrawCmd, 0, 128), nil)
	for i := range 40 {
		l.PushClip(1, kui.Rect{X: float32(i), Y: 0, W: 100, H: 100})
	}
	l.Rect(1, full, red, kui.DrawRadii{})
	for range 40 {
		l.PopClip(1)
	}
	l.Rect(1, full, red, kui.DrawRadii{})
	s = recSink{}
	e.run(l, 64, 48, 1, &s)
	got = s.all()
	if len(got) != 2 || got[0].Quad != [4]float32{30, 0, 64, 48} || got[1].Quad != [4]float32{0, 0, 64, 48} {
		t.Fatalf("deep clips: %+v", got)
	}
}

func TestText(t *testing.T) {
	e, face := newEncoder(encConfig{})
	l := newList()
	l.Text(1, kui.Rect{X: 1.4, Y: 2, W: 40, H: 15}, white, "ab a", 0, 12, 2, 0)
	var s recSink
	e.run(l, 64, 48, 1, &s)
	got := s.all()
	if len(got) != 3 { // The space has no glyph.
		t.Fatalf("%d glyphs, want 3", len(got))
	}
	// Baseline: round(2 + (15-12)/2) + 9 = 13; glyphs are 6 by 12, 9 above the baseline.
	// Pens advance 7 plus 2 of spacing from 1.4, rounded: 1, 10 and, after the space, 28.
	wantX := []float32{1, 10, 28}
	for i, in := range got {
		if in.Kind != kindGlyph || in.Quad[0] != wantX[i] || in.Quad[1] != 4 || in.Quad[2] != wantX[i]+6 || in.Quad[3] != 16 {
			t.Errorf("glyph %d: %+v", i, in)
		}
	}
	// The atlas holds a, b and the space once, a's mask its coverage.
	if face.calls != 3 || e.atlas.n != 3 {
		t.Fatalf("face calls %d, atlas glyphs %d, want 3 and 3", face.calls, e.atlas.n)
	}
	a, _ := e.atlas.glyph(face, 0, 12, 12, 'a')
	if px := e.atlas.pix[int(a.y)*e.atlas.side+int(a.x)]; px != 'a'+12 {
		t.Fatalf("atlas pixel %d", px)
	}
	// Atlas offsets map the glyph's pixels to its mask.
	if got[0].Param[0] != float32(a.x)-1 || got[0].Param[1] != float32(a.y)-4 {
		t.Fatalf("offset %v, glyph at %d,%d", got[0].Param, a.x, a.y)
	}
	if s.atlas != 1 {
		t.Fatalf("%d atlas uploads, want 1", s.atlas)
	}

	// At scale 2 glyphs are rasterized at twice the size, advancing by the logical advances.
	face.calls = 0
	s = recSink{}
	e.run(l, 128, 96, 2, &s)
	got = s.all()
	if len(got) != 3 || got[0].Quad[2]-got[0].Quad[0] != 12 || got[1].Quad[0] != 2*10+1 {
		t.Fatalf("scale 2: %+v", got)
	}
	if face.calls != 6 { // Each rune at both sizes.
		t.Fatalf("face calls %d, want 6", face.calls)
	}
	// The mask is copied before the advance call reuses the face's buffer.
	b, _ := e.atlas.glyph(face, 0, 12, 24, 'b')
	if px := e.atlas.pix[int(b.y)*e.atlas.side+int(b.x)]; px != 'b'+24 || b.adv != 7 {
		t.Fatalf("scaled glyph pixel %d advance %d", px, b.adv)
	}
}

func TestBatches(t *testing.T) {
	e, _ := newEncoder(encConfig{})
	l := newList()
	r := kui.Rect{X: 1, Y: 1, W: 4, H: 4}
	l.Rect(1, r, red, kui.DrawRadii{})
	l.Image(1, r, 1, white, kui.DrawRadii{})
	l.Text(1, kui.Rect{X: 1, Y: 1, W: 20, H: 13}, white, "x", 0, 12, 0, 0)
	l.Image(1, r, 2, white, kui.DrawRadii{})
	l.Image(1, r, 2, white, kui.DrawRadii{})
	l.Rect(1, r, red, kui.DrawRadii{})
	l.Image(1, r, 1, white, kui.DrawRadii{})
	var s recSink
	e.run(l, 64, 48, 1, &s)
	if len(s.batches) != 1 {
		t.Fatalf("%d chunks, want 1", len(s.batches))
	}
	b := s.batches[0]
	if len(b) != 3 || b[0].count != 3 || b[1].count != 3 || b[2].count != 1 ||
		b[0].image != b[2].image || b[0].image == b[1].image || b[1].image < 0 {
		t.Fatalf("batches %+v", b)
	}
	if !slices.Equal(s.uploads, []int{b[0].image, b[1].image}) {
		t.Fatalf("uploads %v", s.uploads)
	}
	// Images stay uploaded.
	s = recSink{}
	e.run(l, 64, 48, 1, &s)
	if len(s.uploads) != 0 {
		t.Fatalf("uploads %v again", s.uploads)
	}
}

func TestChunks(t *testing.T) {
	e, _ := newEncoder(encConfig{maxInstances: 8})
	l := newList()
	for i := range 20 {
		l.Rect(1, kui.Rect{X: float32(i), Y: 1, W: 1, H: 1}, red, kui.DrawRadii{})
	}
	var s recSink
	e.run(l, 64, 48, 1, &s)
	if n := []int{len(s.chunks[0]), len(s.chunks[1]), len(s.chunks[2])}; len(s.chunks) != 3 || !slices.Equal(n, []int{8, 8, 4}) {
		t.Fatalf("chunks of %d", n)
	}

	// Text that does not fit after other commands moves to the next chunk whole.
	l = newList()
	for i := range 5 {
		l.Rect(1, kui.Rect{X: float32(i), Y: 1, W: 1, H: 1}, red, kui.DrawRadii{})
	}
	l.Text(1, kui.Rect{X: 1, Y: 1, W: 60, H: 13}, white, "abcdef", 0, 8, 0, 0)
	s = recSink{}
	e.run(l, 64, 48, 1, &s)
	if len(s.chunks) != 2 || len(s.chunks[0]) != 5 || len(s.chunks[1]) != 6 || e.dropped != 0 {
		t.Fatalf("chunks %d dropped %d", len(s.chunks), e.dropped)
	}

	// Text with more glyphs than a chunk holds is cut.
	l = newList()
	l.Text(1, kui.Rect{X: 0, Y: 1, W: 64, H: 13}, white, "abcdefghijkl", 0, 8, 0, 0)
	l.Rect(1, kui.Rect{X: 1, Y: 1, W: 1, H: 1}, red, kui.DrawRadii{})
	s = recSink{}
	e.run(l, 64, 48, 1, &s)
	if len(s.chunks) != 2 || len(s.chunks[0]) != 8 || len(s.chunks[1]) != 1 || e.dropped != 1 {
		t.Fatalf("chunks %d dropped %d", len(s.chunks), e.dropped)
	}
}

func TestAtlasFull(t *testing.T) {
	// Glyphs of size 8 are 4 by 8: a 16 pixel atlas holds 8.
	e, _ := newEncoder(encConfig{atlasSide: 16})
	l := newList()
	l.Text(1, kui.Rect{X: 0, Y: 0, W: 64, H: 13}, white, "abcdef", 0, 8, 0, 0)
	l.Text(1, kui.Rect{X: 0, Y: 20, W: 64, H: 13}, white, "ghijkl", 0, 8, 0, 0)
	var s recSink
	e.run(l, 64, 48, 1, &s)
	if len(s.chunks) != 2 || len(s.chunks[0]) != 6 || len(s.chunks[1]) != 6 || e.resets != 1 || s.atlas != 2 {
		t.Fatalf("chunks %d, resets %d, uploads %d", len(s.chunks), e.resets, s.atlas)
	}

	// A command needing more than the empty atlas holds draws what fits.
	e, _ = newEncoder(encConfig{atlasSide: 16})
	l = newList()
	l.Text(1, kui.Rect{X: 0, Y: 0, W: 64, H: 13}, white, "abcdefghij", 0, 8, 0, 0)
	s = recSink{}
	e.run(l, 64, 48, 1, &s)
	if got := len(s.all()); got != 8 || e.dropped != 2 {
		t.Fatalf("%d glyphs, %d dropped", got, e.dropped)
	}
	if x := s.all()[7].Quad[0]; x != 7*5 { // Pens still advance over dropped glyphs.
		t.Fatalf("last glyph at %v", x)
	}

	// The glyph table fills too.
	e, _ = newEncoder(encConfig{maxGlyphs: 3})
	l = newList()
	l.Text(1, kui.Rect{X: 0, Y: 0, W: 64, H: 13}, white, "abc", 0, 8, 0, 0)
	l.Text(1, kui.Rect{X: 0, Y: 20, W: 64, H: 13}, white, "d", 0, 8, 0, 0)
	s = recSink{}
	e.run(l, 64, 48, 1, &s)
	if len(s.chunks) != 2 || e.resets != 1 {
		t.Fatalf("chunks %d, resets %d", len(s.chunks), e.resets)
	}
}

func TestImageSlots(t *testing.T) {
	e, _ := newEncoder(encConfig{maxImages: 2})
	l := newList()
	r := kui.Rect{X: 1, Y: 1, W: 4, H: 4}
	for id := uint32(1); id <= 3; id++ {
		l.Image(1, r, id, white, kui.DrawRadii{})
	}
	var s recSink
	e.run(l, 64, 48, 1, &s)
	if len(s.chunks) != 2 || len(s.chunks[0]) != 2 || len(s.chunks[1]) != 1 || len(s.uploads) != 3 {
		t.Fatalf("chunks %d, uploads %v", len(s.chunks), s.uploads)
	}
	if img := s.chunks[0][0]; img.Kind != kindImage || img.Param[0] != 2 || img.Param[1] != 2 || img.Color != [4]uint8{255, 255, 255, 255} {
		t.Fatalf("image %+v", img)
	}
	// Invalidated images are uploaded again.
	l = newList()
	l.Image(1, r, 3, kui.Color{}, kui.DrawRadii{})
	s = recSink{}
	e.run(l, 64, 48, 1, &s)
	if len(s.uploads) != 0 {
		t.Fatalf("uploads %v", s.uploads)
	}
	var p Painter
	p.enc = *e
	p.InvalidateImage(3)
	s = recSink{}
	p.enc.run(l, 64, 48, 1, &s)
	if len(s.uploads) != 1 {
		t.Fatalf("uploads %v after invalidating", s.uploads)
	}
}

func TestCustom(t *testing.T) {
	e, _ := newEncoder(encConfig{})
	l := newList()
	r := kui.Rect{X: 1, Y: 1, W: 4, H: 4}
	l.Rect(1, r, red, kui.DrawRadii{})
	l.PushClip(1, kui.Rect{X: 2, Y: 3, W: 10, H: 10})
	l.Custom(1, r, 7, 8)
	l.PopClip(1)
	l.Rect(1, r, red, kui.DrawRadii{})
	var s recSink
	e.run(l, 64, 48, 1, &s)
	if len(s.chunks) != 2 || len(s.customs) != 1 || s.customs[0] != (irect{2, 3, 12, 13}) {
		t.Fatalf("chunks %d customs %v", len(s.chunks), s.customs)
	}
}

// countSink counts instances without allocating.
type countSink struct{ n int }

func (c *countSink) submit(e *encoder) {
	c.n += len(e.inst)
	e.atlas.dirty = irect{}
	for i := range e.images {
		e.images[i].upload = false
	}
}

func (c *countSink) custom(*kui.DrawCmd, irect) {}

func TestEncodeAllocs(t *testing.T) {
	e, _ := newEncoder(encConfig{maxInstances: 16, atlasSide: 32, maxImages: 1})
	l := newList()
	for _, add := range singles {
		add(l)
	}
	l.Image(1, kui.Rect{X: 5, Y: 6, W: 9.5, H: 7}, 2, kui.Color{}, kui.Radius(2)) // Swaps the image slot.
	l.Text(1, kui.Rect{X: 1, Y: 20, W: 60, H: 13}, white, "the quick brown fox", 0, 13, 0, 0)
	var s countSink
	run := func() { e.run(l, 100, 80, 1.5, &s) }
	run()
	if allocs := testing.AllocsPerRun(100, run); allocs != 0 {
		t.Fatal("allocations:", allocs)
	}
	if s.n == 0 || e.resets == 0 {
		t.Fatalf("drew %d instances with %d atlas resets: want both", s.n, e.resets)
	}
}
