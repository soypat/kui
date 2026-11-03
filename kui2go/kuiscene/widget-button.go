package kuiscene

import (
	"github.com/soypat/kui"
	"github.com/soypat/kui/gesture"
)

// Button adds a button. It emits [EventActivate] when tapped or when Enter or Space is
// pressed while it has focus.
func (w *WidgetBuilder) Button(parent kui.Handle, s string) kui.Handle {
	vis := w.textVisual(w.theme.ButtonText)
	vis.Background = w.theme.Button
	vis.Radius = kui.Radius(w.theme.Radius)
	p := w.theme.Padding
	style := kui.LayoutStyle{Padding: kui.Insets{Left: 2 * p, Right: 2 * p, Top: p, Bottom: p}, AlignX: kui.AlignCenter, AlignY: kui.AlignMiddle}
	b := w.b()
	h := b.Node(parent, w.kinds.Button, &style, &vis)
	if h != 0 {
		b.Fail(w.scene.SetText(h, s))
		b.SetFlags(h, FlagFocusable)
	}
	return h
}

func (w *WidgetBuilder) updateButton(k *Scene, n kui.Handle, e *NodeEvent) {
	w.updateCommon(n, e)
	if e.Kind == gesture.Tap || isActivateKey(e, true) {
		k.Emit(NodeEvent{Node: n, Kind: EventActivate, Time: e.Time})
	}
}

func (w *WidgetBuilder) paintButton(k *Scene, n kui.Handle, l *kui.DrawList) {
	st := k.State(n)
	c := w.theme.Button
	switch {
	case k.Flags(n)&FlagDisabled != 0:
		c = w.theme.ButtonDisabled
	case st&statePressed != 0:
		c = w.theme.ButtonPressed
	case st&stateHovered != 0:
		c = w.theme.ButtonHover
	}
	box := k.Bounds(n)
	radius := kui.Radius(w.theme.Radius)
	l.Rect(n, box, c, radius)
	k.PaintText(n, l, w.theme.ButtonText)
	w.paintFocus(n, l, box, radius)
}
