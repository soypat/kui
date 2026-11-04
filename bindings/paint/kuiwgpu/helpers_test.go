package kuiwgpu

import "github.com/soypat/kui"

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
