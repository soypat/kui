package kuiscene

import (
	"github.com/soypat/kui"
)

// Popup adds a hidden modal container floating below anchor, i.e. a menu. Show it with
// [WidgetBuilder.ShowPopup] and add children to it. While shown, input outside it is blocked
// and Tab cycles focus within it. Pressing outside it or pressing Escape hides it; applications
// can observe this as [EventDismiss] on the popup.
//
// The popup is a child of anchor, so it is removed, hidden and disabled with it.
func (w *WidgetBuilder) Popup(anchor kui.Handle) kui.Handle {
	p := w.theme.Padding
	style := kui.LayoutStyle{
		Direction: kui.TopToBottom, Padding: kui.InsetsAll(p), Gap: w.theme.Gap,
		Floating: kui.Floating{AttachTo: kui.AttachParent, Element: kui.AttachLeftTop, Parent: kui.AttachLeftBottom, Offset: kui.Point{Y: 4}, Z: 100},
	}
	vis := Visual{
		Background: w.theme.Popup, BorderColor: w.theme.PopupBorder,
		Border: kui.DrawBorders{Left: 1, Right: 1, Top: 1, Bottom: 1}, Radius: kui.Radius(w.theme.Radius),
	}
	b := w.b()
	h := b.Node(anchor, w.kinds.Popup, &style, &vis)
	b.SetFlags(h, FlagModal|FlagHidden)
	return h
}

// ShowPopup shows or hides popup h.
func (w *WidgetBuilder) ShowPopup(h kui.Handle, show bool) {
	f := w.scene.Flags(h) | FlagHidden
	if show {
		f &^= FlagHidden
	}
	w.scene.SetFlags(h, f)
}

func (w *WidgetBuilder) updatePopup(k *Scene, n kui.Handle, e *NodeEvent) {
	if e.Kind == EventDismiss {
		w.ShowPopup(n, false)
	}
}

// Tooltip adds a tooltip showing s below widget n after the pointer rests over n for
// [Theme.TooltipDelay]. n's kind must sense hover ([gesture.SenseHover]) and update on hover
// events like the widgets of [WidgetBuilder] do: buttons, check boxes, entries and sliders.
func (w *WidgetBuilder) Tooltip(n kui.Handle, s string) kui.Handle {
	vis := w.textVisual(w.theme.Text.Color)
	vis.Background = w.theme.Tooltip
	vis.Radius = kui.Radius(w.theme.Radius)
	p := w.theme.Padding / 2
	style := kui.LayoutStyle{
		Padding:  kui.Insets{Left: 2 * p, Right: 2 * p, Top: p, Bottom: p},
		Floating: kui.Floating{AttachTo: kui.AttachParent, Element: kui.AttachCenterTop, Parent: kui.AttachCenterBottom, Offset: kui.Point{Y: 6}, Z: 101},
	}
	b := w.b()
	h := b.Node(n, w.kinds.Tooltip, &style, &vis)
	if h != 0 {
		b.Fail(w.scene.SetText(h, s))
		b.SetFlags(h, FlagHidden)
	}
	return h
}

// tooltipOf returns the tooltip of widget n, or zero.
func (w *WidgetBuilder) tooltipOf(n kui.Handle) kui.Handle {
	k := w.scene
	for c := k.FirstChild(n); c != 0; c = k.NextSibling(c) {
		if k.Kind(c) == w.kinds.Tooltip {
			return c
		}
	}
	return 0
}

func (w *WidgetBuilder) showTooltip(n kui.Handle, show bool) {
	if t := w.tooltipOf(n); t != 0 {
		f := FlagHidden
		if show {
			f = 0
		}
		w.scene.SetFlags(t, f)
	}
}
