package kuicogent

import (
	"image"
	"image/color"
	"image/draw"

	"cogentcore.org/core/colors"
	"cogentcore.org/core/math32"
	"cogentcore.org/core/paint"
	"cogentcore.org/core/paint/ppath"
	"cogentcore.org/core/paint/render"
	"cogentcore.org/core/styles/sides"
	"cogentcore.org/core/styles/units"
	"cogentcore.org/core/system"
	"cogentcore.org/core/system/composer"
	"cogentcore.org/core/text/rich"
	"cogentcore.org/core/text/shaped"
	"cogentcore.org/core/text/text"
	"github.com/soypat/kui"

	_ "cogentcore.org/core/paint/renderers" // Sets paint.NewSourceRenderer and NewImageRenderer.
)

// PainterConfig configures a [Painter].
type PainterConfig struct {
	// Images resolves image commands. May be nil.
	Images kui.ImageSource
	// Custom, if set, paints custom commands, scale dots per logical pixel.
	Custom func(c *kui.DrawCmd, pc *paint.Painter, scale float32)
}

// Painter paints kui draw lists with a Cogent [paint.Painter].
type Painter struct {
	cfg  PainterConfig
	text shaper
}

// Configure sets up p.
func (p *Painter) Configure(cfg PainterConfig) error {
	p.cfg = cfg
	p.text.init()
	return nil
}

// Paint records the commands of l on pc, with scale dots per logical pixel of l. It leaves
// pc's fill and stroke styles changed.
func (p *Painter) Paint(pc *paint.Painter, l *kui.DrawList, scale float32) {
	if !(scale > 0) {
		scale = 1
	}
	depth := 0
	cmds := l.Cmds()
	for i := range cmds {
		c := &cmds[i]
		x, y, w, h := c.Rect.X*scale, c.Rect.Y*scale, c.Rect.W*scale, c.Rect.H*scale
		switch c.Op {
		case kui.DrawRect:
			if c.Color.A == 0 || w <= 0 || h <= 0 {
				continue
			}
			fill(pc, c.Color)
			rect(pc, x, y, w, h, radii(c.Radii, scale))
			pc.Draw()
		case kui.DrawBorder:
			if c.Color.A == 0 || w <= 0 || h <= 0 {
				continue
			}
			// The outer box minus the inner one, inset by each side's width, like kui-use's CPU
			// painter: the inner radii shrink by the narrowest side.
			b := c.BorderWidths()
			lw, rw, tw, bw := float32(b.Left)*scale, float32(b.Right)*scale, float32(b.Top)*scale, float32(b.Bottom)*scale
			minW := float32(min(b.Left, b.Right, b.Top, b.Bottom)) * scale
			fill(pc, c.Color)
			pc.Fill.Rule = ppath.EvenOdd
			outer := radii(c.Radii, scale)
			rect(pc, x, y, w, h, outer)
			if iw, ih := w-lw-rw, h-tw-bw; iw > 0 && ih > 0 {
				inner := sides.NewFloats(max(outer.Top-minW, 0), max(outer.Right-minW, 0), max(outer.Bottom-minW, 0), max(outer.Left-minW, 0))
				rect(pc, x+lw, y+tw, iw, ih, inner)
			}
			pc.Draw()
			pc.Fill.Rule = ppath.NonZero
		case kui.DrawCircle:
			if c.Color.A == 0 {
				continue
			}
			fill(pc, c.Color)
			pc.Circle(x+w/2, y+h/2, min(w, h)/2)
			pc.Draw()
		case kui.DrawLine:
			if c.Color.A == 0 {
				continue
			}
			pc.Fill.Color = nil
			pc.Stroke.Color = colors.Uniform(c.Color)
			pc.Stroke.Width.Dot(max(c.LineWidth(), 1) * scale)
			pc.Stroke.Cap = ppath.CapRound
			pc.Line(x, y, x+w, y+h)
			pc.Draw()
			pc.Stroke.Color = nil
		case kui.DrawText:
			_, size, _, _ := c.TextStyle()
			s := l.TextOf(c)
			if c.Color.A == 0 || len(s) == 0 {
				continue
			}
			lines := p.text.lines(string(s), float32(size)*scale, c.Color)
			// Center the line in the command's line box, as kui-use's CPU painter does.
			pc.DrawText(lines, math32.Vec2(x, y+(h-lines.LineHeight)/2))
		case kui.DrawImage:
			p.image(pc, c, x, y, w, h, scale)
		case kui.DrawClipPush:
			pc.PushContext(pc.Paint, render.NewBounds(x, y, w, h, sides.Floats{}))
			depth++
		case kui.DrawClipPop:
			if depth > 0 {
				pc.PopContext()
				depth--
			}
		case kui.DrawCustom:
			if p.cfg.Custom != nil {
				p.cfg.Custom(c, pc, scale)
			}
		}
	}
	for ; depth > 0; depth-- { // Lists that dropped commands may be unbalanced.
		pc.PopContext()
	}
}

func (p *Painter) image(pc *paint.Painter, c *kui.DrawCmd, x, y, w, h, scale float32) {
	if p.cfg.Images == nil || w <= 0 || h <= 0 {
		return
	}
	pix, stride, iw, ih, ok := p.cfg.Images.Image(c.ImageID())
	if !ok || iw <= 0 || ih <= 0 || stride < 4*iw || len(pix) < (ih-1)*stride+4*iw {
		return
	}
	// kui images are non premultiplied RGBA rows: the layout of image.NRGBA.
	img := &image.NRGBA{Pix: pix, Stride: stride, Rect: image.Rect(0, 0, iw, ih)}
	rounded := c.Radii != (kui.DrawRadii{})
	if rounded {
		pc.PushContext(pc.Paint, render.NewBounds(x, y, w, h, radii(c.Radii, scale)))
	}
	pc.DrawImageScaled(img, img, x, y, w, h)
	if rounded {
		pc.PopContext()
	}
}

func fill(pc *paint.Painter, c kui.Color) {
	pc.Fill.Color = colors.Uniform(c)
	pc.Stroke.Color = nil
}

// radii returns kui corner radii (top left, top right, bottom left, bottom right) in dots as
// Cogent's (top left, top right, bottom right, bottom left).
func radii(r kui.DrawRadii, scale float32) sides.Floats {
	return sides.NewFloats(float32(r[0])*scale, float32(r[1])*scale, float32(r[3])*scale, float32(r[2])*scale)
}

func rect(pc *paint.Painter, x, y, w, h float32, r sides.Floats) {
	if r == (sides.Floats{}) {
		pc.Rectangle(x, y, w, h)
	} else {
		pc.RoundedRectangleSides(x, y, w, h, r)
	}
}

// shaper shapes single lines of text, for both [Painter] and [Measurer] so they agree.
type shaper struct {
	sh shaped.Shaper
	uc units.Context
}

func (s *shaper) init() {
	s.sh = shaped.NewShaper()
	s.uc.Defaults()
}

// lines shapes str on a single line at size dots, in color c.
func (s *shaper) lines(str string, size float32, c color.Color) *shaped.Lines {
	ts := text.NewStyle()
	ts.FontSize.Dot(size)
	ts.WhiteSpace = text.WrapNever
	if c != nil {
		ts.Color = c
	}
	ts.ToDots(&s.uc)
	rs := rich.NewStyle()
	return s.sh.WrapLines(rich.NewText(rs, []rune(str)), rs, ts, math32.Vec2(1e6, 1e6))
}

// Present shows r, the render of a [paint.Painter] the size of win (see [paint.State.RenderDone]),
// on win, rasterized by rd, a renderer made by [paint.NewSourceRenderer] of the same size. It
// reports false if the window is closed.
func Present(win system.Window, rd render.Renderer, r render.Render) bool {
	if !win.Lock() {
		return false
	}
	defer win.Unlock()
	cp := win.Composer()
	cp.Start()
	cp.Add(&source{rd: rd, r: r}, nil)
	cp.Compose()
	return true
}

// source is a composer source drawing a render, as Cogent's core package does for scenes.
type source struct {
	rd render.Renderer
	r  render.Render
}

func (s *source) Draw(c composer.Composer) {
	cd, ok := c.(*composer.ComposerDrawer)
	if !ok {
		return
	}
	img := s.rd.Render(s.r).Image()
	if img == nil {
		return
	}
	cd.Drawer.Copy(image.Point{}, img, img.Bounds(), draw.Src, false)
}
