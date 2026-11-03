package kui

import "math"

// DrawOp is the kind of a [DrawCmd].
type DrawOp uint8

const (
	DrawNone DrawOp = iota // none
	// DrawRect fills Rect with Color and corner Radius.
	DrawRect // rect
	// DrawBorder strokes the inside of Rect with Color, per side widths ([DrawCmd.BorderWidths])
	// and corner Radius.
	DrawBorder // border
	// DrawText draws the command's text ([DrawList.TextOf]) with its top left corner at Rect's
	// origin. See [DrawCmd.TextStyle] for font information.
	DrawText // text
	// DrawImage draws image [DrawCmd.ImageID] into Rect, tinted by Color, with corner Radius.
	DrawImage // image
	// DrawClipPush intersects the clip rectangle with Rect until the matching DrawClipPop.
	DrawClipPush // clip-push
	// DrawClipPop restores the clip rectangle before the matching DrawClipPush.
	DrawClipPop // clip-pop
	// DrawLine strokes a line from Rect's origin to Rect's origin plus (W, H) with width
	// [DrawCmd.LineWidth].
	DrawLine // line
	// DrawCircle fills the ellipse inscribed in Rect.
	DrawCircle // circle
	// DrawCustom is passed to a user supplied painter. See [DrawCmd.CustomData].
	DrawCustom // custom
)

// Radii holds corner radii in pixels: top left, top right, bottom left and bottom right.
type Radii [4]uint16

// Radius returns a radius of r for all corners.
func Radius(r uint16) Radii { return Radii{r, r, r, r} }

// DrawBorders holds the border width of each side in pixels.
type DrawBorders struct {
	Left, Right, Top, Bottom uint8
}

// DrawCmd is a single draw command. Its size is 44 bytes and it contains no pointers.
// The meaning of A and B depends on Op; use the accessor methods.
type DrawCmd struct {
	Rect   Rect
	Color  Color
	Radius Radii
	// Node is the node that generated the command, if any.
	Node Handle
	A, B uint32
	// Z is the layer of the command. Commands in a list are in ascending Z order.
	Z     int16
	Op    DrawOp
	Flags uint8
}

// BorderWidths returns the side widths of a DrawBorder command.
func (c *DrawCmd) BorderWidths() DrawBorders {
	return DrawBorders{Left: uint8(c.A), Right: uint8(c.A >> 8), Top: uint8(c.A >> 16), Bottom: uint8(c.A >> 24)}
}

// TextStyle returns the font, size, letter spacing and line height of a DrawText command.
func (c *DrawCmd) TextStyle() (font, size, letterSpacing, lineHeight uint16) {
	return c.Radius[0], c.Radius[1], c.Radius[2], c.Radius[3]
}

// LineWidth returns the stroke width of a DrawLine command.
func (c *DrawCmd) LineWidth() float32 { return math.Float32frombits(c.A) }

// ImageID returns the user defined image identifier of a DrawImage command.
func (c *DrawCmd) ImageID() uint32 { return c.A }

// CustomData returns the user defined values of a DrawCustom command.
func (c *DrawCmd) CustomData() (id, data uint32) { return c.A, c.B }

// TextOverhang is the distance in pixels glyphs may draw outside the box of their text
// command, i.e. italic or accented glyphs. [DrawCmd.Bounds] grows text boxes by it.
const TextOverhang = 2

// Bounds returns the region command c may draw to, ignoring clipping. Painters must not draw
// outside it, so damage tracking is exact. A clip push's bounds are its clip rectangle, since
// changing it changes what is drawn inside it. Clip pops have empty bounds.
func (c *DrawCmd) Bounds() Rect {
	r := c.Rect
	switch c.Op {
	case DrawNone, DrawClipPop:
		return Rect{}
	case DrawText:
		return grow(r, TextOverhang)
	case DrawLine:
		x0, x1 := min(r.X, r.X+r.W), max(r.X, r.X+r.W)
		y0, y1 := min(r.Y, r.Y+r.H), max(r.Y, r.Y+r.H)
		return grow(Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}, max(c.LineWidth(), 1)/2+1)
	}
	return r
}

func grow(r Rect, d float32) Rect {
	return Rect{X: r.X - d, Y: r.Y - d, W: r.W + 2*d, H: r.H + 2*d}
}

// ImageSource resolves the image identifiers of [DrawImage] commands for painters.
// Implementations must not allocate.
type ImageSource interface {
	// Image returns the pixels of image id: h rows of w non premultiplied RGBA8 pixels, each
	// row stride bytes after the previous one.
	Image(id uint32) (pix []byte, stride, w, h int, ok bool)
}

// DrawList is a sequence of draw commands with a byte arena for their text: what a UI paints,
// for painters (CPU or GPU) to draw in order. Commands are in ascending Z order and clip
// commands are balanced, unless commands were dropped ([DrawList.Err]).
//
// All memory is supplied by the user with [DrawList.Reset]; DrawList never allocates. When a
// buffer is too small commands are dropped, Err returns [ErrShortBuffer] and [DrawList.Need]
// reports the sizes required to hold everything that was added, so the caller can grow its
// buffers and paint again.
type DrawList struct {
	cmds     []DrawCmd
	text     []byte
	needCmds int
	needText int
	z        int16
	err      error
}

// Reset sets the list's buffers and clears it. A nil buffer keeps the current one.
func (l *DrawList) Reset(cmds []DrawCmd, textCap []byte) {
	if cmds != nil {
		l.cmds = cmds[:0]
	}
	if textCap != nil {
		l.text = textCap[:0]
	}
	l.Clear()
}

// Clear removes all commands keeping the buffers.
func (l *DrawList) Clear() {
	l.cmds = l.cmds[:0]
	l.text = l.text[:0]
	l.needCmds, l.needText = 0, 0
	l.z = 0
	l.err = nil
}

// CopyFrom replaces l's contents with those of src. If they do not fit l's buffers, l is left
// empty, and [ErrShortBuffer] is returned and reported by l's Err and Need.
func (l *DrawList) CopyFrom(src *DrawList) error {
	l.Clear()
	l.needCmds, l.needText, l.z = src.needCmds, src.needText, src.z
	if len(src.cmds) > cap(l.cmds) || len(src.text) > cap(l.text) {
		l.err = ErrShortBuffer
		return l.err
	}
	l.cmds = append(l.cmds, src.cmds...)
	l.text = append(l.text, src.text...)
	l.err = src.err
	return nil
}

// Cmds returns the commands in the list. Valid until the list is modified.
func (l *DrawList) Cmds() []DrawCmd { return l.cmds }

// Len returns the number of commands in the list.
func (l *DrawList) Len() int { return len(l.cmds) }

// Err returns [ErrShortBuffer] if commands or text were dropped since the last Clear.
func (l *DrawList) Err() error { return l.err }

// Need returns the number of commands and text bytes that were added since the last Clear,
// including those dropped for lack of space.
func (l *DrawList) Need() (cmds, textBytes int) { return l.needCmds, l.needText }

// SetZ sets the Z layer of subsequently added commands.
func (l *DrawList) SetZ(z int16) { l.z = z }

// TextOf returns the text of a DrawText command in this list.
func (l *DrawList) TextOf(c *DrawCmd) []byte {
	end := uint64(c.A) + uint64(c.B)
	if c.Op != DrawText || end > uint64(len(l.text)) {
		return nil
	}
	return l.text[c.A:end]
}

// Add appends c with the list's current Z layer. Its text operands are not validated.
func (l *DrawList) Add(c DrawCmd) {
	l.needCmds++
	if len(l.cmds) == cap(l.cmds) {
		l.err = ErrShortBuffer
		return
	}
	c.Z = l.z
	l.cmds = append(l.cmds, c)
}

// Rect adds a filled rectangle.
func (l *DrawList) Rect(node Handle, r Rect, c Color, radius Radii) {
	l.Add(DrawCmd{Op: DrawRect, Node: node, Rect: r, Color: c, Radius: radius})
}

// Border adds a border stroked inside r.
func (l *DrawList) Border(node Handle, r Rect, c Color, w DrawBorders, radius Radii) {
	a := uint32(w.Left) | uint32(w.Right)<<8 | uint32(w.Top)<<16 | uint32(w.Bottom)<<24
	l.Add(DrawCmd{Op: DrawBorder, Node: node, Rect: r, Color: c, Radius: radius, A: a})
}

// Text adds a text command, copying s into the list's text arena.
func (l *DrawList) Text(node Handle, r Rect, c Color, s string, font, size, letterSpacing, lineHeight uint16) {
	l.needText += len(s)
	if len(s) > cap(l.text)-len(l.text) {
		l.needCmds++
		l.err = ErrShortBuffer
		return
	}
	off := len(l.text)
	l.text = append(l.text, s...)
	before := len(l.cmds)
	l.Add(DrawCmd{Op: DrawText, Node: node, Rect: r, Color: c, A: uint32(off), B: uint32(len(s)), Radius: Radii{font, size, letterSpacing, lineHeight}})
	if len(l.cmds) == before {
		l.text = l.text[:off] // Command dropped, release its text.
	}
}

// Image adds an image command. image is a user defined identifier resolved by the painter.
func (l *DrawList) Image(node Handle, r Rect, image uint32, tint Color, radius Radii) {
	l.Add(DrawCmd{Op: DrawImage, Node: node, Rect: r, Color: tint, Radius: radius, A: image})
}

// PushClip intersects the clip rectangle with r until the matching PopClip.
func (l *DrawList) PushClip(node Handle, r Rect) {
	l.Add(DrawCmd{Op: DrawClipPush, Node: node, Rect: r})
}

// PopClip restores the clip rectangle before the matching PushClip.
func (l *DrawList) PopClip(node Handle) {
	l.Add(DrawCmd{Op: DrawClipPop, Node: node})
}

// Line adds a line from p0 to p1 with the given stroke width.
func (l *DrawList) Line(node Handle, p0, p1 Point, width float32, c Color) {
	l.Add(DrawCmd{Op: DrawLine, Node: node, Rect: Rect{X: p0.X, Y: p0.Y, W: p1.X - p0.X, H: p1.Y - p0.Y}, Color: c, A: math.Float32bits(width)})
}

// Circle adds a filled circle.
func (l *DrawList) Circle(node Handle, center Point, radius float32, c Color) {
	l.Add(DrawCmd{Op: DrawCircle, Node: node, Rect: Rect{X: center.X - radius, Y: center.Y - radius, W: 2 * radius, H: 2 * radius}, Color: c})
}

// Custom adds a command for a user supplied painter.
func (l *DrawList) Custom(node Handle, r Rect, id, data uint32) {
	l.Add(DrawCmd{Op: DrawCustom, Node: node, Rect: r, A: id, B: data})
}

// DrawMark records the state of a [DrawList] for [DrawList.Rewind].
type DrawMark struct {
	cmds, text, needCmds, needText int
	z                              int16
	err                            error
}

// Mark returns the current state of the list.
func (l *DrawList) Mark() DrawMark {
	return DrawMark{cmds: len(l.cmds), text: len(l.text), needCmds: l.needCmds, needText: l.needText, z: l.z, err: l.err}
}

// Rewind removes all commands added after m was taken.
func (l *DrawList) Rewind(m DrawMark) {
	l.cmds = l.cmds[:m.cmds]
	l.text = l.text[:m.text]
	l.needCmds, l.needText, l.z, l.err = m.needCmds, m.needText, m.z, m.err
}
