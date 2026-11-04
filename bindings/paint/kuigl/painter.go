// Package kuigl paints kui draw lists with OpenGL: a GPU batch painter for [kui.DrawList]s.
//
// Shapes and glyphs become instanced quads drawn by one shader; text and shapes share draw
// calls, which are split only when an image texture changes or a buffer fills. Glyphs are
// rasterized by the [kui.FontFace]s of the configuration into a texture atlas on first use.
// Coverage and blending follow the formulas of kui-use's CPU painter, so both paint the same
// pixels give or take a unit of rounding, and nothing is drawn outside a command's
// [kui.DrawCmd.Bounds], keeping damage tracking exact.
//
// The painter uses the common subset of OpenGL 3.3 core and OpenGL ES 3.0. It knows nothing of
// windows: it draws with the context current on the calling thread, created by the caller,
// i.e. with GLFW. It does not allocate after [Painter.Configure].
package kuigl

import "github.com/soypat/kui"

// Config configures a [Painter].
type Config struct {
	// Faces rasterize the text of fonts, indexed by font.
	Faces []kui.FontFace
	// Images resolves image commands. May be nil. An image is uploaded the first time it is
	// drawn: call [Painter.InvalidateImage] when its pixels change. Image rows must be a
	// multiple of 4 bytes apart.
	Images kui.ImageSource
	// Custom, if set, paints custom commands with GL, clipped by the scissor test. The
	// painter's GL state is restored after it returns.
	Custom func(c *kui.DrawCmd)
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
	Instances   int // Shapes and glyphs drawn.
	AtlasResets int // Times the glyph atlas filled up and was emptied.
	Dropped     int // Commands or glyphs not drawn in full for lack of room.
}

// Painter paints draw lists with OpenGL. Configure it with [Painter.Configure].
type Painter struct {
	enc         encoder
	gpu         gpu
	paintCustom func(c *kui.DrawCmd)
	stats       Stats
	ok          bool
}

var _ sink = (*Painter)(nil)

// Configure creates the painter's GL resources with the context current on the calling
// thread, initializing the GL function pointers. It is the only method that allocates.
func (p *Painter) Configure(cfg Config) error {
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
	if p.ok {
		p.Close()
	}
	*p = Painter{paintCustom: cfg.Custom}
	p.enc.init(cfg.Faces, cfg.Images, cfg.MaxInstances, cfg.AtlasSize, cfg.MaxGlyphs, cfg.MaxImages)
	if err := p.gpu.init(cfg.MaxInstances, cfg.AtlasSize, cfg.MaxImages); err != nil {
		return err
	}
	p.ok = true
	return nil
}

// Clear fills the bound framebuffer with c.
func (p *Painter) Clear(c kui.Color) {
	if p.ok {
		p.gpu.clear(c)
	}
}

// Paint draws l over the bound framebuffer of width by height pixels, with scale framebuffer
// pixels per logical pixel of l. It leaves the GL state set for its own drawing.
func (p *Painter) Paint(l *kui.DrawList, width, height int, scale float32) error {
	if !p.ok {
		return kui.ErrInvalidConfig
	}
	if width <= 0 || height <= 0 {
		return nil
	}
	if !(scale > 0) {
		scale = 1
	}
	p.gpu.begin(width, height)
	p.enc.run(l, width, height, scale, p)
	p.stats.Frames++
	p.stats.AtlasResets = p.enc.resets
	p.stats.Dropped = p.enc.dropped
	return nil
}

// InvalidateImage discards the texture of image id, so its pixels are uploaded again the
// next time it is drawn.
func (p *Painter) InvalidateImage(id uint32) {
	for i := range p.enc.images {
		if s := &p.enc.images[i]; s.used && s.id == id {
			s.used = false
		}
	}
}

// Stats returns the counters of the painter's work.
func (p *Painter) Stats() Stats { return p.stats }

// Close deletes the painter's GL resources. The painter must be configured again to be used.
func (p *Painter) Close() {
	if p.ok {
		p.gpu.close()
		p.ok = false
	}
}

func (p *Painter) submit(e *encoder) {
	p.stats.Draws += p.gpu.draw(e)
	p.stats.Instances += len(e.inst)
}

func (p *Painter) custom(c *kui.DrawCmd, clip irect) {
	if p.paintCustom == nil || clip.empty() {
		return
	}
	p.gpu.scissor(clip)
	p.paintCustom(c)
	p.gpu.begin(p.gpu.width, p.gpu.height)
}
