package kuigl

import (
	"fmt"
	"os"
	"runtime"
	"testing"
	"unsafe"

	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/go-gl/glfw/v3.4/glfw"
	"github.com/soypat/kui"
	"github.com/soypat/kui/kui2go/kuibatch"
)

// GL calls run on the main OS thread, as GLFW requires, through onMain.

func init() { runtime.LockOSThread() }

var mainQueue = make(chan func())

func TestMain(m *testing.M) {
	done := make(chan int)
	go func() { done <- m.Run() }()
	for {
		select {
		case f := <-mainQueue:
			f()
		case code := <-done:
			if glWin != nil {
				glWin.Destroy()
				glfw.Terminate()
			}
			os.Exit(code)
		}
	}
}

// onMain runs f on the main thread. f must not call t.Fatal or t.Skip.
func onMain(f func()) {
	done := make(chan struct{})
	mainQueue <- func() { defer close(done); f() }
	<-done
}

const fbWidth, fbHeight = 64, 48

var (
	glWin   *glfw.Window
	glErr   error
	glFB    uint32
	glReady bool
	bg      = kui.RGB(10, 20, 30)
)

// glSetup creates a hidden window with a GL 3.3 core context and a framebuffer to paint into,
// or skips the test without a display.
func glSetup(t *testing.T) {
	t.Helper()
	onMain(func() {
		if glReady || glErr != nil {
			return
		}
		if glErr = glfw.Init(); glErr != nil {
			return
		}
		glfw.WindowHint(glfw.Visible, glfw.False)
		glfw.WindowHint(glfw.ContextVersionMajor, 3)
		glfw.WindowHint(glfw.ContextVersionMinor, 3)
		glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
		glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)
		if glWin, glErr = glfw.CreateWindow(16, 16, "kuigl test", nil, nil); glErr != nil {
			glfw.Terminate()
			return
		}
		glWin.MakeContextCurrent()
		if glErr = gl.Init(); glErr != nil {
			return
		}
		var tex uint32
		gl.GenTextures(1, &tex)
		gl.BindTexture(gl.TEXTURE_2D, tex)
		gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, fbWidth, fbHeight, 0, gl.RGBA, gl.UNSIGNED_BYTE, nil)
		gl.GenFramebuffers(1, &glFB)
		gl.BindFramebuffer(gl.FRAMEBUFFER, glFB)
		gl.FramebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, tex, 0)
		if s := gl.CheckFramebufferStatus(gl.FRAMEBUFFER); s != gl.FRAMEBUFFER_COMPLETE {
			glErr = fmt.Errorf("framebuffer status %#x", s)
			return
		}
		glReady = true
	})
	if glErr != nil {
		t.Skip("no GL context:", glErr)
	}
}

func newPainter(t *testing.T, cfg Config) *Painter {
	t.Helper()
	glSetup(t)
	if cfg.Faces == nil {
		cfg.Faces = []kui.FontFace{new(testFace)}
	}
	if cfg.Images == nil {
		cfg.Images = new(testImages)
	}
	p := new(Painter)
	var err error
	onMain(func() { err = p.Configure(cfg) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { onMain(p.Close) })
	return p
}

// frame is a painted framebuffer, rows top first.
type frame [fbHeight][fbWidth][4]uint8

func paint(t *testing.T, p *Painter, l *kui.DrawList, scale float32) *frame {
	t.Helper()
	var f frame
	var err error
	var glerr uint32
	onMain(func() {
		gl.BindFramebuffer(gl.FRAMEBUFFER, glFB)
		p.Clear(bg)
		err = p.Paint(l, fbWidth, fbHeight, scale)
		var rows frame
		gl.ReadPixels(0, 0, fbWidth, fbHeight, gl.RGBA, gl.UNSIGNED_BYTE, unsafe.Pointer(&rows[0][0][0]))
		for y := range fbHeight {
			f[y] = rows[fbHeight-1-y] // GL rows are bottom first.
		}
		glerr = gl.GetError()
	})
	if err != nil {
		t.Fatal(err)
	}
	if glerr != gl.NO_ERROR {
		t.Fatalf("GL error %#x", glerr)
	}
	return &f
}

func rgbaOf(c kui.Color) [4]uint8 { return [4]uint8{c.R, c.G, c.B, c.A} }

// near reports whether a and b differ by at most 1 per channel.
func near(a, b [4]uint8) bool {
	for i := range a {
		if d := int(a[i]) - int(b[i]); d < -1 || d > 1 {
			return false
		}
	}
	return true
}

// lerp8 is kui-use's CPU blending of s over d with alpha a.
func lerp8(d, s, a uint8) uint8 {
	return uint8((uint16(s)*uint16(a) + uint16(d)*uint16(255-a) + 127) / 255)
}

func over(d [4]uint8, c kui.Color, a uint8) [4]uint8 {
	return [4]uint8{lerp8(d[0], c.R, a), lerp8(d[1], c.G, a), lerp8(d[2], c.B, a), a + uint8((uint16(d[3])*uint16(255-a)+127)/255)}
}

func TestGLRect(t *testing.T) {
	p := newPainter(t, Config{})
	l := newList()
	l.Rect(1, kui.Rect{X: 10, Y: 10, W: 20.5, H: 10}, red, kui.DrawRadii{})
	f := paint(t, p, l, 1)
	if got := f[15][20]; got != rgbaOf(red) {
		t.Fatalf("inside %v", got)
	}
	if got := f[5][5]; got != rgbaOf(bg) {
		t.Fatalf("outside %v", got)
	}
	// Half covered pixel: coverage 128 blended like the CPU painter.
	if got, want := f[15][30], over(rgbaOf(bg), red, 128); !near(got, want) {
		t.Fatalf("edge %v, want %v", got, want)
	}

	// Scale 2 doubles geometry.
	l = newList()
	l.Rect(1, kui.Rect{X: 5, Y: 5, W: 10, H: 5}, red, kui.DrawRadii{})
	f = paint(t, p, l, 2)
	for y := range fbHeight {
		for x := range fbWidth {
			want := rgbaOf(bg)
			if x >= 10 && x < 30 && y >= 10 && y < 20 {
				want = rgbaOf(red)
			}
			if f[y][x] != want {
				t.Fatalf("scale 2: pixel %d,%d is %v, want %v", x, y, f[y][x], want)
			}
		}
	}
}

// TestGLBounds checks commands draw only within their bounds, and draw something.
func TestGLBounds(t *testing.T) {
	p := newPainter(t, Config{})
	for i, add := range singles {
		l := newList()
		add(l)
		cmd := l.Cmds()[0]
		f := paint(t, p, l, 1)
		b := kuibatch.PixelsOf(cmd.Bounds(), 1)
		drawn := false
		for y := range fbHeight {
			for x := range fbWidth {
				inside := int32(x) >= b.X0 && int32(x) < b.X1 && int32(y) >= b.Y0 && int32(y) < b.Y1
				if f[y][x] != rgbaOf(bg) {
					drawn = true
					if !inside {
						t.Fatalf("command %d (%v) drew %v at %d,%d outside %v", i, cmd.Op, f[y][x], x, y, b)
					}
				}
			}
		}
		if !drawn {
			t.Errorf("command %d (%v) drew nothing", i, cmd.Op)
		}
	}
}

func TestGLText(t *testing.T) {
	p := newPainter(t, Config{})
	l := newList()
	l.Text(1, kui.Rect{X: 2, Y: 2, W: 40, H: 13}, white, "Hi", 0, 13, 0, 0)
	f := paint(t, p, l, 1)
	// H is 6 by 13 at the pen (2, baseline 11 - 9 = 2), coverage 'H'+13.
	cov := uint8('H' + 13)
	if got, want := f[8][4], over(rgbaOf(bg), white, cov); !near(got, want) {
		t.Fatalf("glyph pixel %v, want %v", got, want)
	}
	if got := f[1][4]; got != rgbaOf(bg) {
		t.Fatalf("above glyph %v", got)
	}
	if got, want := f[8][11], over(rgbaOf(bg), white, uint8('i'+13)); !near(got, want) {
		t.Fatalf("second glyph pixel %v, want %v", got, want)
	}
}

func TestGLImage(t *testing.T) {
	p := newPainter(t, Config{})
	l := newList()
	l.Image(1, kui.Rect{X: 4, Y: 4, W: 8, H: 8}, 1, kui.Color{}, kui.DrawRadii{})
	l.Image(1, kui.Rect{X: 20, Y: 4, W: 8, H: 8}, 2, kui.RGBA(255, 255, 255, 128), kui.DrawRadii{})
	f := paint(t, p, l, 1)
	for _, c := range []struct {
		x, y int
		want [4]uint8
	}{
		{5, 5, rgbaOf(red)},
		{9, 5, [4]uint8{0, 255, 0, 255}},
		{5, 9, [4]uint8{0, 0, 255, 255}},
		{9, 9, over(rgbaOf(bg), white, 50)}, // White pixel with alpha 50.
		{21, 5, over(rgbaOf(bg), red, 128)}, // Tint alpha 128.
	} {
		if got := f[c.y][c.x]; !near(got, c.want) {
			t.Errorf("pixel %d,%d is %v, want %v", c.x, c.y, got, c.want)
		}
	}
}

func TestGLCustom(t *testing.T) {
	var calls int
	p := newPainter(t, Config{Custom: func(c *kui.DrawCmd) {
		calls++
		gl.ClearColor(0, 1, 0, 1)
		gl.Clear(gl.COLOR_BUFFER_BIT) // Clipped by the scissor test.
	}})
	l := newList()
	l.PushClip(1, kui.Rect{X: 2, Y: 3, W: 4, H: 5})
	l.Custom(1, kui.Rect{W: 64, H: 48}, 0, 0)
	l.PopClip(1)
	l.Rect(1, kui.Rect{X: 40, Y: 40, W: 4, H: 4}, red, kui.DrawRadii{}) // Drawn after the GL state is restored.
	f := paint(t, p, l, 1)
	if calls != 1 {
		t.Fatalf("%d custom calls", calls)
	}
	green := [4]uint8{0, 255, 0, 255}
	if f[3][2] != green || f[7][5] != green || f[8][5] != rgbaOf(bg) || f[3][6] != rgbaOf(bg) {
		t.Fatalf("custom clip: %v %v %v %v", f[3][2], f[7][5], f[8][5], f[3][6])
	}
	if f[41][41] != rgbaOf(red) {
		t.Fatalf("rect after custom %v", f[41][41])
	}
}

func TestGLAllocs(t *testing.T) {
	p := newPainter(t, Config{MaxInstances: 16, AtlasSize: 32, MaxImages: 1})
	l := newList()
	for _, add := range singles {
		add(l)
	}
	l.Image(1, kui.Rect{X: 5, Y: 6, W: 9.5, H: 7}, 2, kui.Color{}, kui.Radius(2))
	l.Text(1, kui.Rect{X: 1, Y: 20, W: 60, H: 13}, white, "the quick brown fox", 0, 13, 0, 0)
	paint(t, p, l, 1.5)
	var allocs float64
	onMain(func() {
		allocs = testing.AllocsPerRun(20, func() {
			p.Clear(bg)
			p.Paint(l, fbWidth, fbHeight, 1.5)
		})
	})
	if allocs != 0 {
		t.Fatal("allocations:", allocs)
	}
	if s := p.Stats(); s.Draws == 0 || s.AtlasResets == 0 {
		t.Fatalf("stats %+v", s)
	}
}
