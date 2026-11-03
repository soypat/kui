package kuiscene

import (
	"github.com/soypat/kui"
	"github.com/soypat/kui/gesture"
)

// Scroll adds a container filling its parent that lays out its children along dir and
// scrolls them when they overflow it: with the mouse wheel, by dragging its content (a drag
// starting on a child that does not take drags itself, like a button) or by dragging its scroll
// bar. Add children to the returned node.
func (w *WidgetBuilder) Scroll(parent kui.Handle, dir kui.Direction) kui.Handle {
	style := kui.LayoutStyle{Direction: dir, Width: kui.Grow(0, 0), Height: kui.Grow(0, 0), Gap: w.theme.Gap}
	// Leave room for the scroll bar so it does not cover content.
	if dir == kui.TopToBottom {
		style.ClipY = true
		style.Padding.Right = w.theme.ScrollBarSize
	} else {
		style.ClipX = true
		style.Padding.Bottom = w.theme.ScrollBarSize
	}
	b := w.b()
	h := b.Node(parent, w.kinds.Scroll, &style, &Visual{})
	b.SetFlags(h, FlagScrollable)
	return h
}

// scrollThumb returns the scroll bar thumb of scroll container n and how many pixels of
// content moving it by one pixel scrolls. ok is false if the content does not overflow.
func (w *WidgetBuilder) scrollThumb(n kui.Handle) (thumb kui.Rect, scale float32, ok bool) {
	k := w.scene
	box, content, style := k.Bounds(n), k.ContentSize(n), k.Style(n)
	size := w.theme.ScrollBarSize
	// Work along the scroll axis: length is the box's, extent the content's, offset the scroll.
	length, extent, offset := box.H, content.H, -style.Scroll.Y
	if style.Direction == kui.LeftToRight {
		length, extent, offset = box.W, content.W, -style.Scroll.X
	}
	overflow := extent - length
	if overflow <= 0 || length <= 0 {
		return kui.Rect{}, 0, false
	}
	thumbLen := min(max(length*length/extent, 2*size), length)
	travel := length - thumbLen
	pos := offset / overflow * travel
	if style.Direction == kui.LeftToRight {
		thumb = kui.Rect{X: box.X + pos, Y: box.Y + box.H - size, W: thumbLen, H: size}
	} else {
		thumb = kui.Rect{X: box.X + box.W - size, Y: box.Y + pos, W: size, H: thumbLen}
	}
	return thumb, overflow / max(travel, 1), true
}

func (w *WidgetBuilder) updateScroll(k *Scene, n kui.Handle, e *NodeEvent) {
	st := k.State(n)
	switch e.Kind {
	case gesture.Press:
		box := k.Bounds(n)
		if thumb, _, ok := w.scrollThumb(n); ok && thumb.Contains(kui.Point{X: box.X + e.X, Y: box.Y + e.Y}) {
			st |= stateThumb
		}
	case gesture.DragStart, gesture.Drag:
		if st&stateThumb == 0 {
			k.ScrollBy(n, -e.DX, -e.DY) // Pan: content follows the pointer.
		} else if _, scale, ok := w.scrollThumb(n); ok {
			k.ScrollBy(n, e.DX*scale, e.DY*scale)
		}
	case gesture.DragEnd, gesture.Release:
		st &^= stateThumb
	}
	k.SetState(n, st)
}

func (w *WidgetBuilder) paintScrollBar(k *Scene, n kui.Handle, l *kui.DrawList) {
	if thumb, _, ok := w.scrollThumb(n); ok {
		l.Rect(n, thumb, w.theme.ScrollBar, kui.Radius(uint16(w.theme.ScrollBarSize/2)))
	}
}
