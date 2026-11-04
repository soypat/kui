package kuiwgpu

import (
	"errors"
	"os"
	"sync"
	"testing"
	"unsafe"

	"github.com/oliverbestmann/webgpu/wgpu"
	"github.com/soypat/kui"
	"github.com/soypat/kui/kui2go/kuibatch"
)

// The tests paint into offscreen textures of a headless device, read back. They are skipped
// without a WebGPU adapter. WGPU_FORCE_FALLBACK_ADAPTER=1 asks for a software adapter.

var (
	devOnce sync.Once
	dev     *wgpu.Device
	queue   *wgpu.Queue
	devErr  error
)

func device(t *testing.T) (*wgpu.Device, *wgpu.Queue) {
	t.Helper()
	devOnce.Do(func() {
		inst := wgpu.CreateInstance(nil)
		adapter, err := inst.RequestAdapter(&wgpu.RequestAdapterOptions{
			ForceFallbackAdapter: os.Getenv("WGPU_FORCE_FALLBACK_ADAPTER") == "1",
		})
		if err != nil {
			devErr = err
			return
		}
		if dev, devErr = adapter.RequestDevice(nil); devErr == nil {
			queue = dev.GetQueue()
		}
	})
	if devErr != nil {
		t.Skip("no WebGPU device:", devErr)
	}
	return dev, queue
}

// fbWidth makes rows of 256 bytes, the alignment of texture to buffer copies.
const fbWidth, fbHeight = 64, 48

var bg = kui.RGB(10, 20, 30)

// target is a texture painted and the buffer it is read back through.
type target struct {
	tex  *wgpu.Texture
	view *wgpu.TextureView
	buf  *wgpu.Buffer
}

func newTarget(t *testing.T, format wgpu.TextureFormat) *target {
	t.Helper()
	d, _ := device(t)
	tex, err := d.TryCreateTexture(&wgpu.TextureDescriptor{
		Usage:         wgpu.TextureUsageRenderAttachment | wgpu.TextureUsageCopySrc,
		Dimension:     wgpu.TextureDimension2D,
		Size:          wgpu.Extent3D{Width: fbWidth, Height: fbHeight, DepthOrArrayLayers: 1},
		Format:        format,
		MipLevelCount: 1,
		SampleCount:   1,
	})
	if err != nil {
		t.Fatal(err)
	}
	view, err := tex.TryCreateView(nil)
	if err != nil {
		t.Fatal(err)
	}
	buf, err := d.TryCreateBuffer(&wgpu.BufferDescriptor{
		Usage: wgpu.BufferUsageMapRead | wgpu.BufferUsageCopyDst,
		Size:  fbWidth * fbHeight * 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		buf.Release()
		view.Release()
		tex.Release()
	})
	return &target{tex: tex, view: view, buf: buf}
}

// frame is a painted texture, rows top first.
type frame [fbHeight][fbWidth][4]uint8

func (tg *target) read(t *testing.T) *frame {
	t.Helper()
	d, q := device(t)
	enc := d.CreateCommandEncoder(nil)
	enc.CopyTextureToBuffer(
		&wgpu.TexelCopyTextureInfo{Texture: tg.tex, Aspect: wgpu.TextureAspectAll},
		&wgpu.TexelCopyBufferInfo{Buffer: tg.buf, Layout: wgpu.TexelCopyBufferLayout{BytesPerRow: fbWidth * 4, RowsPerImage: fbHeight}},
		&wgpu.Extent3D{Width: fbWidth, Height: fbHeight, DepthOrArrayLayers: 1},
	)
	cmds := enc.Finish(nil)
	q.Submit(cmds)
	cmds.Release()
	enc.Release()
	var status wgpu.MapAsyncStatus
	const size = fbWidth * fbHeight * 4
	tg.buf.MapAsync(wgpu.MapModeRead, 0, size, func(s wgpu.MapAsyncStatus) { status = s })
	d.Poll(true, nil)
	if status != wgpu.MapAsyncStatusSuccess {
		t.Fatalf("mapping status %v", status)
	}
	var f frame
	copy(unsafe.Slice(&f[0][0][0], size), tg.buf.GetMappedRange(0, size))
	tg.buf.Unmap()
	return &f
}

func newPainter(t *testing.T, cfg Config) (*Painter, *target) {
	t.Helper()
	cfg.Device, cfg.Queue = device(t)
	if cfg.Format == wgpu.TextureFormatUndefined {
		cfg.Format = wgpu.TextureFormatRGBA8Unorm
	}
	if cfg.Faces == nil {
		cfg.Faces = []kui.FontFace{new(testFace)}
	}
	if cfg.Images == nil {
		cfg.Images = new(testImages)
	}
	p := new(Painter)
	if err := p.Configure(cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p, newTarget(t, cfg.Format)
}

func paint(t *testing.T, p *Painter, tg *target, l *kui.DrawList, scale float32) *frame {
	t.Helper()
	p.Clear(bg)
	if err := p.Paint(tg.view, l, fbWidth, fbHeight, scale); err != nil {
		t.Fatal(err)
	}
	return tg.read(t)
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

func TestConfigure(t *testing.T) {
	var p Painter
	if err := p.Configure(Config{Format: wgpu.TextureFormatRGBA8Unorm}); !errors.Is(err, kui.ErrInvalidConfig) {
		t.Fatalf("no device: %v", err)
	}
	d, _ := device(t)
	if err := p.Configure(Config{Device: d}); !errors.Is(err, kui.ErrInvalidConfig) {
		t.Fatalf("no format: %v", err)
	}
	big := int(d.GetLimits().MaxTextureDimension2D) + 1
	if err := p.Configure(Config{Device: d, Format: wgpu.TextureFormatRGBA8Unorm, AtlasSize: big}); !errors.Is(err, kui.ErrInvalidConfig) {
		t.Fatalf("atlas of %d: %v", big, err)
	}
	if err := p.Paint(nil, newList(), 1, 1, 1); !errors.Is(err, kui.ErrInvalidConfig) {
		t.Fatalf("unconfigured paint: %v", err)
	}
}

func TestRect(t *testing.T) {
	p, tg := newPainter(t, Config{})
	l := newList()
	l.Rect(1, kui.Rect{X: 10, Y: 10, W: 20.5, H: 10}, red, kui.DrawRadii{})
	f := paint(t, p, tg, l, 1)
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
	f = paint(t, p, tg, l, 2)
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

// TestBounds checks commands draw only within their bounds, and draw something.
func TestBounds(t *testing.T) {
	p, tg := newPainter(t, Config{})
	for i, add := range singles {
		l := newList()
		add(l)
		cmd := l.Cmds()[0]
		f := paint(t, p, tg, l, 1)
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

func TestText(t *testing.T) {
	p, tg := newPainter(t, Config{})
	l := newList()
	l.Text(1, kui.Rect{X: 2, Y: 2, W: 40, H: 13}, white, "Hi", 0, 13, 0, 0)
	f := paint(t, p, tg, l, 1)
	// H is 6 by 13 at the pen (2, baseline 11 - 9 = 2), coverage 'H'+13.
	if got, want := f[8][4], over(rgbaOf(bg), white, uint8('H'+13)); !near(got, want) {
		t.Fatalf("glyph pixel %v, want %v", got, want)
	}
	if got := f[1][4]; got != rgbaOf(bg) {
		t.Fatalf("above glyph %v", got)
	}
	if got, want := f[8][11], over(rgbaOf(bg), white, uint8('i'+13)); !near(got, want) {
		t.Fatalf("second glyph pixel %v, want %v", got, want)
	}
}

func TestImage(t *testing.T) {
	p, tg := newPainter(t, Config{})
	l := newList()
	l.Image(1, kui.Rect{X: 4, Y: 4, W: 8, H: 8}, 1, kui.Color{}, kui.DrawRadii{})
	l.Image(1, kui.Rect{X: 20, Y: 4, W: 8, H: 8}, 2, kui.RGBA(255, 255, 255, 128), kui.DrawRadii{})
	f := paint(t, p, tg, l, 1)
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

// greenPipeline returns a pipeline filling the target with opaque green.
func greenPipeline(t *testing.T) *wgpu.RenderPipeline {
	t.Helper()
	d, _ := device(t)
	module, err := d.TryCreateShaderModule(&wgpu.ShaderModuleDescriptor{WGSLSource: &wgpu.ShaderSourceWGSL{Code: `
@vertex fn vs(@builtin(vertex_index) i: u32) -> @builtin(position) vec4f {
	let p = vec2f(f32((i << 1u) & 2u), f32(i & 2u));
	return vec4f(p*2.0 - 1.0, 0.0, 1.0);
}
@fragment fn fs() -> @location(0) vec4f { return vec4f(0.0, 1.0, 0.0, 1.0); }
`}})
	if err != nil {
		t.Fatal(err)
	}
	defer module.Release()
	pl, err := d.TryCreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Vertex:      wgpu.VertexState{Module: module, EntryPoint: "vs"},
		Primitive:   wgpu.PrimitiveState{Topology: wgpu.PrimitiveTopologyTriangleList, CullMode: wgpu.CullModeNone},
		Multisample: wgpu.MultisampleState{Count: 1, Mask: 0xFFFFFFFF},
		Fragment: &wgpu.FragmentState{Module: module, EntryPoint: "fs", Targets: []wgpu.ColorTargetState{{
			Format: wgpu.TextureFormatRGBA8Unorm, WriteMask: wgpu.ColorWriteMaskAll,
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pl.Release)
	return pl
}

func TestCustom(t *testing.T) {
	green := greenPipeline(t)
	var calls int
	p, tg := newPainter(t, Config{Custom: func(c *kui.DrawCmd, pass *wgpu.RenderPassEncoder) {
		calls++
		pass.SetPipeline(green)
		pass.Draw(3, 1, 0, 0) // Clipped by the scissor rectangle.
	}})
	l := newList()
	l.PushClip(1, kui.Rect{X: 2, Y: 3, W: 4, H: 5})
	l.Custom(1, kui.Rect{W: 64, H: 48}, 0, 0)
	l.PopClip(1)
	l.Rect(1, kui.Rect{X: 40, Y: 40, W: 4, H: 4}, red, kui.DrawRadii{}) // Drawn in a pass after the custom one.
	f := paint(t, p, tg, l, 1)
	if calls != 1 {
		t.Fatalf("%d custom calls", calls)
	}
	g := [4]uint8{0, 255, 0, 255}
	if f[3][2] != g || f[7][5] != g || f[8][5] != rgbaOf(bg) || f[3][6] != rgbaOf(bg) {
		t.Fatalf("custom clip: %v %v %v %v", f[3][2], f[7][5], f[8][5], f[3][6])
	}
	if f[41][41] != rgbaOf(red) {
		t.Fatalf("rect after custom %v", f[41][41])
	}
}

// TestClear checks a clear with nothing drawn still clears, and painting without a clear
// draws over the target's pixels.
func TestClear(t *testing.T) {
	p, tg := newPainter(t, Config{})
	l := newList()
	l.Rect(1, kui.Rect{X: 0, Y: 0, W: 10, H: 10}, red, kui.DrawRadii{})
	paint(t, p, tg, l, 1)
	if err := p.Paint(tg.view, newList(), fbWidth, fbHeight, 1); err != nil {
		t.Fatal(err)
	}
	if f := tg.read(t); f[5][5] != rgbaOf(red) {
		t.Fatalf("painting nothing changed %v", f[5][5])
	}
	f := paint(t, p, tg, newList(), 1)
	if f[5][5] != rgbaOf(bg) {
		t.Fatalf("clear alone left %v", f[5][5])
	}
}

// TestChunks checks lists painted in many chunks, with the atlas emptied and image textures
// replaced midway, paint as when painted in one.
func TestChunks(t *testing.T) {
	l := newList()
	for i := range 30 {
		c := kui.RGBA(uint8(i*8), uint8(255-i*8), 128, 200)
		l.Rect(1, kui.Rect{X: float32(i * 2), Y: float32(i%5) * 3, W: 3, H: 3}, c, kui.Radius(1))
	}
	l.Text(1, kui.Rect{X: 0, Y: 20, W: 64, H: 13}, white, "abcdef", 0, 8, 0, 0)
	l.Text(1, kui.Rect{X: 0, Y: 32, W: 64, H: 13}, red, "ghijkl", 0, 8, 0, 0)
	for id := uint32(1); id <= 3; id++ {
		l.Image(1, kui.Rect{X: 30 + float32(id)*8, Y: 16, W: 7, H: 7}, id, kui.Color{}, kui.DrawRadii{})
	}
	one, tg1 := newPainter(t, Config{})
	want := paint(t, one, tg1, l, 1)
	// Chunks of 6 hold the longest text. Glyphs of size 8 are 4 by 8: a 16 pixel atlas holds 8
	// of the 12.
	many, tg2 := newPainter(t, Config{MaxInstances: 6, AtlasSize: 16, MaxImages: 1})
	got := paint(t, many, tg2, l, 1)
	if s := many.Stats(); s.Submits < 8 || s.AtlasResets == 0 || s.Dropped != 0 {
		t.Fatalf("stats %+v: want many submits, an atlas reset and nothing dropped", s)
	}
	for y := range fbHeight {
		for x := range fbWidth {
			if got[y][x] != want[y][x] {
				t.Fatalf("pixel %d,%d is %v in chunks, %v in one", x, y, got[y][x], want[y][x])
			}
		}
	}
}

// TestSRGB checks sRGB targets store kui's colors.
func TestSRGB(t *testing.T) {
	p, tg := newPainter(t, Config{Format: wgpu.TextureFormatRGBA8UnormSrgb})
	l := newList()
	c := kui.RGB(200, 100, 50)
	l.Rect(1, kui.Rect{X: 10, Y: 10, W: 20, H: 10}, c, kui.DrawRadii{})
	f := paint(t, p, tg, l, 1)
	if got := f[15][20]; !near(got, rgbaOf(c)) {
		t.Fatalf("inside %v, want %v", got, rgbaOf(c))
	}
	if got := f[5][5]; !near(got, rgbaOf(bg)) {
		t.Fatalf("outside %v, want %v", got, rgbaOf(bg))
	}
}
