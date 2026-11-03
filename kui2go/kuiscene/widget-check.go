package kuiscene

import (
	"github.com/soypat/kui"
	"github.com/soypat/kui/gesture"
)

// Check adds a check box with a label. It emits [EventChanged] with Code 1 when checked
// and 0 when unchecked, when tapped or when Space is pressed while it has focus.
func (w *WidgetBuilder) Check(parent kui.Handle, s string, checked bool) kui.Handle {
	vis := w.textVisual(w.theme.Text.Color)
	style := kui.LayoutStyle{Padding: kui.Insets{Left: w.theme.CheckSize + w.theme.Gap}, Height: kui.Fit(w.theme.CheckSize, 0), AlignY: kui.AlignMiddle}
	b := w.b()
	h := b.Node(parent, w.kinds.Check, &style, &vis)
	if h != 0 {
		b.Fail(w.scene.SetText(h, s))
		b.SetFlags(h, FlagFocusable)
		w.SetChecked(h, checked)
	}
	return h
}

// Checked reports whether check box h is checked.
func (w *WidgetBuilder) Checked(h kui.Handle) bool { return w.scene.State(h)&stateChecked != 0 }

// SetChecked sets the checked state of check box h without emitting events.
func (w *WidgetBuilder) SetChecked(h kui.Handle, checked bool) {
	st := w.scene.State(h) &^ stateChecked
	if checked {
		st |= stateChecked
	}
	w.scene.SetState(h, st)
}

func (w *WidgetBuilder) updateCheck(k *Scene, n kui.Handle, e *NodeEvent) {
	w.updateCommon(n, e)
	if e.Kind == gesture.Tap || isActivateKey(e, false) {
		checked := !w.Checked(n)
		w.SetChecked(n, checked)
		var code uint32
		if checked {
			code = 1
		}
		k.Emit(NodeEvent{Node: n, Kind: EventChanged, Code: code, Time: e.Time})
	}
}

func (w *WidgetBuilder) paintCheck(k *Scene, n kui.Handle, l *kui.DrawList) {
	box := k.Bounds(n)
	size := w.theme.CheckSize
	sq := kui.Rect{X: box.X, Y: box.Y + (box.H-size)/2, W: size, H: size}
	c := w.theme.CheckBox
	if k.State(n)&stateHovered != 0 {
		c = w.theme.ButtonHover
	}
	radius := kui.Radius(w.theme.Radius / 2)
	l.Rect(n, sq, c, radius)
	if w.Checked(n) {
		inset := size / 4
		l.Rect(n, kui.Rect{X: sq.X + inset, Y: sq.Y + inset, W: size - 2*inset, H: size - 2*inset}, w.theme.CheckMark, radius)
	}
	k.PaintText(n, l, w.theme.Text.Color)
	w.paintFocus(n, l, sq, radius)
}
