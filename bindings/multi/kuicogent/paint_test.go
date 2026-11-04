package kuicogent

import (
	"image"
	"image/color"
	"image/draw"
	"testing"

	"cogentcore.org/core/math32"
	"cogentcore.org/core/paint"
	"github.com/soypat/kui"
)

const width, height = 64, 48

var (
	bg    = kui.RGB(10, 20, 30)
	red   = kui.RGB(255, 0, 0)
	green = kui.RGB(0, 255, 0)
	white = kui.RGB(255, 255, 255)
)

// quads is a 2 by 2 image: red, green, blue and white pixels.
type quads struct{}

func (quads) Image(id uint32) (pix []byte, stride, w, h int, ok bool) {
	return []byte{255, 0, 0, 255, 0, 255, 0, 255, 0, 0, 255, 255, 255, 255, 255, 255}, 8, 2, 2, id == 1
}

func newList() *kui.DrawList {
	l := new(kui.DrawList)
	l.Reset(make([]kui.DrawCmd, 0, 32), make([]byte, 0, 256))
	return l
}

// paintList paints l over the background at scale with a new Painter and returns the pixels.
func paintList(t *testing.T, l *kui.DrawList, scale float32) *image.RGBA {
	t.Helper()
	var p Painter
	if err := p.Configure(PainterConfig{Images: quads{}}); err != nil {
		t.Fatal(err)
	}
	pc := paint.NewPainter(math32.Vec2(width, height))
	fill(pc, bg)
	pc.Rectangle(0, 0, width, height)
	pc.Draw()
	p.Paint(pc, l, scale)
	img := paint.RenderToImage(pc)
	if rgba, ok := img.(*image.RGBA); ok {
		return rgba
	}
	rgba := image.NewRGBA(img.Bounds())
	draw.Draw(rgba, rgba.Bounds(), img, img.Bounds().Min, draw.Src)
	return rgba
}

func at(img *image.RGBA, x, y int) kui.Color {
	c := img.RGBAAt(x, y)
	return kui.Color(color.NRGBAModel.Convert(c).(color.NRGBA))
}

func check(t *testing.T, img *image.RGBA, x, y int, want kui.Color) {
	t.Helper()
	if got := at(img, x, y); got != want {
		t.Errorf("pixel %d,%d is %v, want %v", x, y, got, want)
	}
}

func TestPaintShapes(t *testing.T) {
	l := newList()
	l.Rect(1, kui.Rect{X: 10, Y: 10, W: 20, H: 10}, red, kui.DrawRadii{})
	l.Rect(1, kui.Rect{X: 2, Y: 2, W: 6, H: 6}, kui.RGBA(255, 0, 0, 0), kui.DrawRadii{}) // Transparent.
	l.PushClip(1, kui.Rect{X: 34, Y: 4, W: 10, H: 10})
	l.Rect(1, kui.Rect{W: width, H: height}, green, kui.DrawRadii{})
	l.PopClip(1)
	l.Border(1, kui.Rect{X: 2, Y: 24, W: 20, H: 20}, white, kui.DrawBorders{Left: 3, Right: 3, Top: 3, Bottom: 3}, kui.Radius(4))
	l.Circle(1, kui.Point{X: 54, Y: 34}, 6, red)
	l.Line(1, kui.Point{X: 26, Y: 40}, kui.Point{X: 44, Y: 40}, 2, white)
	img := paintList(t, l, 1)

	check(t, img, 15, 15, red)
	check(t, img, 5, 5, bg)
	check(t, img, 9, 15, bg)
	check(t, img, 38, 8, green) // Inside the clip.
	check(t, img, 30, 8, bg)    // Outside it.
	check(t, img, 47, 8, bg)
	check(t, img, 38, 20, bg)
	check(t, img, 3, 34, white) // Border.
	check(t, img, 12, 34, bg)   // Its hole.
	check(t, img, 54, 34, red)  // Circle center.
	check(t, img, 54, 26, bg)
	check(t, img, 35, 39, white) // Line.
	check(t, img, 35, 44, bg)
}

func TestPaintScale(t *testing.T) {
	l := newList()
	l.Rect(1, kui.Rect{X: 5, Y: 5, W: 10, H: 5}, red, kui.DrawRadii{})
	img := paintList(t, l, 2)
	check(t, img, 10, 10, red)
	check(t, img, 29, 19, red)
	check(t, img, 30, 10, bg)
	check(t, img, 10, 20, bg)
	check(t, img, 9, 10, bg)
}

func TestPaintImage(t *testing.T) {
	l := newList()
	l.Image(1, kui.Rect{X: 8, Y: 8, W: 16, H: 16}, 1, kui.Color{}, kui.DrawRadii{})
	l.Image(1, kui.Rect{X: 30, Y: 8, W: 16, H: 16}, 2, kui.Color{}, kui.DrawRadii{}) // No image 2.
	img := paintList(t, l, 1)
	near := func(x, y int, r, g, b uint8) {
		t.Helper()
		c := at(img, x, y)
		if d := max(diff(c.R, r), diff(c.G, g), diff(c.B, b)); d > 40 {
			t.Errorf("pixel %d,%d is %v, want about %d,%d,%d", x, y, c, r, g, b)
		}
	}
	near(11, 11, 255, 0, 0)
	near(20, 11, 0, 255, 0)
	near(11, 20, 0, 0, 255)
	near(20, 20, 255, 255, 255)
	check(t, img, 38, 16, bg)
}

func diff(a, b uint8) int { return max(int(a)-int(b), int(b)-int(a)) }

// TestPaintText checks text is painted within the width the Measurer gives it.
func TestPaintText(t *testing.T) {
	m := NewMeasurer()
	st := kui.TextStyle{Size: 16}
	const s = "Hello kui"
	mw, mh := m.Measure(s, &st)
	if mw <= 0 || mh <= 0 {
		t.Fatalf("measured %vx%v", mw, mh)
	}
	l := newList()
	r := kui.Rect{X: 4, Y: 10, W: mw, H: mh}
	l.Text(1, r, white, s, 0, st.Size, 0, 0)
	img := paintList(t, l, 1)
	ink, beyond := 0, 0
	for y := range height {
		for x := range width {
			if at(img, x, y) == bg {
				continue
			}
			ink++
			if x > int(r.X+mw)+1 || y < int(r.Y)-1 || y > int(r.Y+r.H)+1 {
				beyond++
			}
		}
	}
	if ink == 0 || beyond != 0 {
		t.Fatalf("%d ink pixels, %d outside the measured %vx%v box", ink, beyond, mw, mh)
	}
}

func TestMeasure(t *testing.T) {
	m := NewMeasurer()
	st := kui.TextStyle{Size: 13}
	w1, h1 := m.Measure("Hello", &st)
	w2, h2 := m.Measure("Hello, world", &st)
	we, he := m.Measure("", &st)
	if !(w1 > 0 && w2 > w1 && h1 > 0 && h2 == h1 && we == 0 && he == h1) {
		t.Fatalf("Hello %vx%v, Hello, world %vx%v, empty %vx%v", w1, h1, w2, h2, we, he)
	}
	big := kui.TextStyle{Size: 26}
	if w, h := m.Measure("Hello", &big); w <= w1 || h <= h1 {
		t.Fatalf("double size %vx%v", w, h)
	}
}
