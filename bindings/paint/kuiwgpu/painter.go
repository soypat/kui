// Package kuiwgpu paints kui draw lists with WebGPU (github.com/oliverbestmann/webgpu): a GPU
// batch painter for [kui.DrawList]s running on Vulkan, Metal, Direct3D 12 and OpenGL ES
// through wgpu-native, or on the browser's WebGPU with GOOS=js.
//
// Shapes and glyphs become instanced quads drawn by one shader, batched by [kuibatch.Encoder];
// text and shapes share draw calls, which are split only when an image texture changes or a
// buffer fills. Glyphs are rasterized by the [kui.FontFace]s of the configuration into a
// texture atlas on first use. Coverage and blending follow the formulas of kui-use's CPU
// painter, as kuigl's do, so both paint the same pixels give or take a unit of rounding, and
// nothing is drawn outside a command's [kui.DrawCmd.Bounds], keeping damage tracking exact.
// On sRGB targets blending happens in linear light: opaque pixels are the same, edges may
// differ by a few units.
//
// The painter knows nothing of windows: it draws into texture views of the device given,
// created by the caller, i.e. from a surface of a GLFW window (see the wgpuglfw package).
// Unlike kuigl it allocates while painting, as WebGPU's command encoders and passes are
// objects, and when it uploads an image of a new size.
package kuiwgpu

import (
	"github.com/oliverbestmann/webgpu/wgpu"
	"github.com/soypat/kui"
	"github.com/soypat/kui/kui2go/kuibatch"
)

// Config configures a [Painter].
type Config struct {
	// Device creates the painter's resources and draws. Required.
	Device *wgpu.Device
	// Queue submits the painter's work. Defaults to the device's queue.
	Queue *wgpu.Queue
	// Format is the format of the texture views painted. Required. With sRGB formats, colors
	// are decoded before being stored, so the bytes stored are kui's colors.
	Format wgpu.TextureFormat
	// Faces rasterize the text of fonts, indexed by font.
	Faces []kui.FontFace
	// Images resolves image commands. May be nil. An image is uploaded the first time it is
	// drawn: call [Painter.InvalidateImage] when its pixels change. Image rows must be a
	// multiple of 4 bytes apart. Images larger than the device's textures are not drawn.
	Images kui.ImageSource
	// Custom, if set, paints custom commands in a render pass of their own on the painted
	// view, clipped by its scissor rectangle.
	Custom func(c *kui.DrawCmd, pass *wgpu.RenderPassEncoder)
	// MaxInstances is the number of shapes and glyphs drawn per buffer upload. Lists with more
	// are drawn in several. Defaults to 4096.
	MaxInstances int
	// AtlasSize is the side in pixels of the glyph atlas texture. Defaults to 1024.
	AtlasSize int
	// MaxGlyphs is the number of glyphs the atlas holds. Defaults to 2048. When the atlas fills
	// it is emptied and glyphs are rasterized again.
	MaxGlyphs int
	// MaxImages is the number of image textures. Defaults to 16.
	MaxImages int
}

// Stats counts the work of a [Painter] since it was configured.
type Stats struct {
	Frames      int // Paint calls.
	Draws       int // Draw calls.
	Submits     int // Command buffers submitted: one per chunk, custom command or lone clear.
	Instances   int // Shapes and glyphs drawn.
	AtlasResets int // Times the glyph atlas filled up and was emptied.
	Dropped     int // Commands or glyphs not drawn in full for lack of room.
}

// Painter paints draw lists with WebGPU. Configure it with [Painter.Configure].
type Painter struct {
	enc         kuibatch.Encoder
	gpu         gpu
	paintCustom func(c *kui.DrawCmd, pass *wgpu.RenderPassEncoder)
	stats       Stats
	ok          bool
}

var _ kuibatch.Sink = sink{}

// Configure creates the painter's resources on cfg.Device.
func (p *Painter) Configure(cfg Config) error {
	if cfg.Device == nil || cfg.Format == wgpu.TextureFormatUndefined {
		return kui.ErrInvalidConfig
	}
	if p.ok {
		p.Close()
	}
	*p = Painter{paintCustom: cfg.Custom}
	bc := p.enc.Configure(kuibatch.Config{
		Faces:        cfg.Faces,
		Images:       cfg.Images,
		MaxInstances: cfg.MaxInstances,
		AtlasSize:    cfg.AtlasSize,
		MaxGlyphs:    cfg.MaxGlyphs,
		MaxImages:    cfg.MaxImages,
	})
	if err := p.gpu.init(cfg.Device, cfg.Queue, cfg.Format, bc.MaxInstances, bc.AtlasSize, bc.MaxImages); err != nil {
		return err
	}
	p.ok = true
	return nil
}

// Clear makes the next [Painter.Paint] fill its target with c before drawing.
func (p *Painter) Clear(c kui.Color) {
	p.gpu.clear, p.gpu.clearColor = true, c
}

// Paint draws l over target, a view of width by height pixels of the configured format, with
// scale pixels per logical pixel of l. Its work is submitted to the queue when it returns.
func (p *Painter) Paint(target *wgpu.TextureView, l *kui.DrawList, width, height int, scale float32) error {
	if !p.ok || target == nil {
		return kui.ErrInvalidConfig
	}
	if width <= 0 || height <= 0 {
		return nil
	}
	if !(scale > 0) {
		scale = 1
	}
	g := &p.gpu
	g.begin(target, width, height)
	p.enc.Run(l, width, height, scale, sink{p})
	if g.clear && g.err == nil {
		g.endPass(g.beginPass()) // Nothing was drawn: clear alone.
	}
	g.clear = false
	p.stats.Frames++
	p.stats.Submits = g.submits
	p.stats.AtlasResets = p.enc.Resets()
	p.stats.Dropped = p.enc.Dropped()
	err := g.err
	g.target, g.err = nil, nil
	return err
}

// InvalidateImage discards the texture of image id, so its pixels are uploaded again the
// next time it is drawn.
func (p *Painter) InvalidateImage(id uint32) { p.enc.InvalidateImage(id) }

// Stats returns the counters of the painter's work.
func (p *Painter) Stats() Stats { return p.stats }

// Close releases the painter's resources. The painter must be configured again to be used.
func (p *Painter) Close() {
	if p.ok {
		p.gpu.close()
		p.ok = false
	}
}

// sink draws the chunks of a painter's encoder.
type sink struct{ p *Painter }

func (s sink) Submit(e *kuibatch.Encoder) {
	s.p.stats.Draws += s.p.gpu.draw(e)
	s.p.stats.Instances += len(e.Instances())
}

func (s sink) Custom(c *kui.DrawCmd, clip kuibatch.Pixels) {
	p := s.p
	if p.paintCustom == nil || clip.Empty() || p.gpu.err != nil {
		return
	}
	pass, enc := p.gpu.beginPass()
	if pass == nil {
		return
	}
	pass.SetScissorRect(uint32(clip.X0), uint32(clip.Y0), uint32(clip.X1-clip.X0), uint32(clip.Y1-clip.Y0))
	p.paintCustom(c, pass)
	p.gpu.endPass(pass, enc)
}
