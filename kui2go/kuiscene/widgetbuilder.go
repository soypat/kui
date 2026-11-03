package kuiscene

import (
	"github.com/soypat/kui"
	"github.com/soypat/kui/gesture"
)

// Theme holds the colors, metrics and timings of the widgets of a [WidgetBuilder].
type Theme struct {
	Text       kui.TextStyle // Font, size and color of text.
	Background kui.Color
	// Buttons.
	Button         kui.Color
	ButtonHover    kui.Color
	ButtonPressed  kui.Color
	ButtonDisabled kui.Color
	ButtonText     kui.Color
	Focus          kui.Color // Focus ring color.
	// Check boxes.
	CheckBox  kui.Color
	CheckMark kui.Color
	// Entries, sliders and scroll bars.
	Field     kui.Color // Background of entries and slider tracks.
	Accent    kui.Color // Filled part and thumb of sliders.
	Selection kui.Color // Background of selected text.
	Caret     kui.Color
	ScrollBar kui.Color
	// Popups and tooltips.
	Popup       kui.Color
	PopupBorder kui.Color
	Tooltip     kui.Color
	// Metrics in pixels.
	Padding       kui.Scalar
	Gap           kui.Scalar
	Radius        uint16
	CheckSize     kui.Scalar
	SliderSize    kui.Scalar // Height of sliders.
	ScrollBarSize kui.Scalar // Width of scroll bars.
	// Timings in nanoseconds. Zero disables caret blinking and tooltips, saving repaints.
	CaretBlink   int64 // Period of the caret's blink.
	TooltipDelay int64 // Time the pointer rests over a widget before its tooltip shows.
}

// DefaultTheme returns a dark theme.
func DefaultTheme() Theme {
	return Theme{
		Text:           kui.TextStyle{Size: 13, Color: kui.RGB(230, 230, 230)},
		Background:     kui.RGB(30, 30, 34),
		Button:         kui.RGB(60, 90, 160),
		ButtonHover:    kui.RGB(75, 110, 190),
		ButtonPressed:  kui.RGB(45, 70, 130),
		ButtonDisabled: kui.RGB(70, 70, 75),
		ButtonText:     kui.RGB(255, 255, 255),
		Focus:          kui.RGB(240, 200, 80),
		CheckBox:       kui.RGB(80, 80, 90),
		CheckMark:      kui.RGB(120, 200, 120),
		Field:          kui.RGB(48, 48, 56),
		Accent:         kui.RGB(90, 140, 230),
		Selection:      kui.RGB(60, 90, 160),
		Caret:          kui.RGB(240, 240, 240),
		ScrollBar:      kui.RGBA(255, 255, 255, 80),
		Popup:          kui.RGB(42, 42, 48),
		PopupBorder:    kui.RGB(85, 85, 95),
		Tooltip:        kui.RGB(64, 64, 72),
		Padding:        6,
		Gap:            6,
		Radius:         4,
		CheckSize:      13,
		SliderSize:     16,
		ScrollBarSize:  6,
		CaretBlink:     1e9,
		TooltipDelay:   600e6,
	}
}

// WidgetConfig configures a [WidgetBuilder].
type WidgetConfig struct {
	// Theme styles the widgets. The zero Theme uses [DefaultTheme].
	Theme Theme
	// Measurer measures text to place the caret of entries, and should be the layout engine's.
	// Required by [WidgetBuilder.Entry].
	Measurer kui.Measurer
	// InsertBytes is the size of the buffer text typed or pasted into entries is read into.
	// Longer insertions are dropped. Defaults to 256.
	InsertBytes int
}

// WidgetKinds holds the kinds of widgets, i.e. to tell widgets apart in events.
type WidgetKinds struct {
	Button, Check, Entry, Slider, Scroll, Popup, Tooltip Kind
}

// Node state bits of widgets. Sliders keep their value above them; see widget-slider.go.
const (
	statePressed = 1 << iota
	stateHovered
	stateChecked // Check boxes.
	stateCaret   // Entries: the caret is visible.
	stateThumb   // Scroll containers: the scroll bar thumb is being dragged.
)

// Timer codes of widgets, see [Scene.SetTimer].
const (
	timerTooltip uint32 = iota + 1
	timerBlink
)

// WidgetBuilder creates widgets on a [Scene]: labels, buttons, check boxes, text entries, sliders,
// scroll containers, popups and tooltips. Widgets are nodes whose kinds are registered by
// [WidgetBuilder.Configure]; like the scene they do not allocate after configuration.
//
// Widgets keep their state in their node: its text, its state bits and, for sliders, a value
// packed in the state. The only state outside nodes is the editing state of the focused entry,
// since only one entry is edited at a time.
//
// Widget creation errors are recorded by the scene's [Builder], see [WidgetBuilder.Err].
type WidgetBuilder struct {
	scene    *Scene
	theme    Theme
	measurer kui.Measurer
	kinds    WidgetKinds
	edit     edit
	insert   []byte
}

// Configure registers the widget kinds on scene, which must be configured. Call it once after
// each [Scene.Configure]: every call registers the kinds again. The WidgetBuilder must not be
// copied afterwards since the kinds refer to it.
func (w *WidgetBuilder) Configure(scene *Scene, cfg WidgetConfig) error {
	if scene == nil || scene.mem == nil {
		return kui.ErrInvalidConfig
	}
	n := cfg.InsertBytes
	if n <= 0 {
		n = 256
	}
	*w = WidgetBuilder{scene: scene, theme: cfg.Theme, measurer: cfg.Measurer, insert: w.insert[:0]}
	if w.theme == (Theme{}) {
		w.theme = DefaultTheme()
	}
	if cap(w.insert) < n {
		w.insert = make([]byte, 0, n)
	}
	press := gesture.SensePress | gesture.SenseHover
	specs := [...]struct {
		kind *Kind
		spec KindSpec
	}{
		{&w.kinds.Button, KindSpec{Sense: press, Update: w.updateButton, Paint: w.paintButton, Cursor: kui.CursorPointer}},
		{&w.kinds.Check, KindSpec{Sense: press, Update: w.updateCheck, Paint: w.paintCheck, Cursor: kui.CursorPointer}},
		{&w.kinds.Entry, KindSpec{
			Sense:  press | gesture.SenseDrag | gesture.SenseDoubleTap,
			Update: w.updateEntry, Paint: w.paintEntry, PaintOver: w.paintEntryOver, Cursor: kui.CursorText,
		}},
		{&w.kinds.Slider, KindSpec{Sense: press | gesture.SenseDrag, Update: w.updateSlider, Paint: w.paintSlider, Cursor: kui.CursorPointer}},
		{&w.kinds.Scroll, KindSpec{Sense: gesture.SensePress | gesture.SenseDrag, Update: w.updateScroll, PaintOver: w.paintScrollBar}},
		// Popups sense presses so those on their background do not dismiss them.
		{&w.kinds.Popup, KindSpec{Sense: gesture.SensePress, Update: w.updatePopup}},
		{&w.kinds.Tooltip, KindSpec{}},
	}
	for _, s := range specs {
		var err error
		if *s.kind, err = scene.RegisterKind(s.spec); err != nil {
			return err
		}
	}
	return nil
}

// Scene returns the scene widgets are created on.
func (w *WidgetBuilder) Scene() *Scene { return w.scene }

// Theme returns the widgets' theme.
func (w *WidgetBuilder) Theme() *Theme { return &w.theme }

// Kinds returns the kinds of the widgets.
func (w *WidgetBuilder) Kinds() WidgetKinds { return w.kinds }

// Err returns the first error that occurred creating widgets, recorded by the scene's [Builder].
func (w *WidgetBuilder) Err() error { return w.scene.builder.Err() }

func (w *WidgetBuilder) b() *Builder { return &w.scene.builder }

func (w *WidgetBuilder) textVisual(c kui.Color) Visual {
	st := w.theme.Text
	st.Color = c
	return Visual{Text: st}
}

// VBox adds a container laying out its children top to bottom, filling its parent's width.
func (w *WidgetBuilder) VBox(parent kui.Handle) kui.Handle {
	return w.b().Box(parent, &kui.LayoutStyle{Direction: kui.TopToBottom, Width: kui.Grow(0, 0), Gap: w.theme.Gap}, &Visual{})
}

// HBox adds a container laying out its children left to right, filling its parent's width.
func (w *WidgetBuilder) HBox(parent kui.Handle) kui.Handle {
	return w.b().Box(parent, &kui.LayoutStyle{Direction: kui.LeftToRight, Width: kui.Grow(0, 0), Gap: w.theme.Gap, AlignY: kui.AlignMiddle}, &Visual{})
}

// Label adds a text label.
func (w *WidgetBuilder) Label(parent kui.Handle, s string) kui.Handle {
	vis := w.textVisual(w.theme.Text.Color)
	return w.b().Text(parent, &kui.LayoutStyle{}, &vis, s)
}

// updateCommon updates the pressed and hovered state of n and shows its tooltip.
func (w *WidgetBuilder) updateCommon(n kui.Handle, e *NodeEvent) {
	k := w.scene
	st := k.State(n)
	switch e.Kind {
	case gesture.Press:
		st |= statePressed
		w.showTooltip(n, false)
	case gesture.Release:
		st &^= statePressed
	case gesture.HoverIn:
		st |= stateHovered
		if delay := w.theme.TooltipDelay; delay > 0 && w.tooltipOf(n) != 0 {
			k.SetTimer(n, timerTooltip, e.Time+delay)
		}
	case gesture.HoverOut:
		st &^= stateHovered
		k.CancelTimer(n, timerTooltip)
		w.showTooltip(n, false)
	case EventTimer:
		if e.Code == timerTooltip && st&stateHovered != 0 {
			w.showTooltip(n, true)
		}
	}
	k.SetState(n, st)
}

func isActivateKey(e *NodeEvent, enter bool) bool {
	if e.Kind != kui.EventKeyDown {
		return false
	}
	key := kui.Key(e.Code)
	return key == kui.KeySpace || (enter && key == kui.KeyEnter)
}

func (w *WidgetBuilder) paintFocus(n kui.Handle, l *kui.DrawList, r kui.Rect, radius kui.DrawRadii) {
	if w.scene.Focus() == n {
		l.Border(n, kui.Rect{X: r.X - 2, Y: r.Y - 2, W: r.W + 4, H: r.H + 4}, w.theme.Focus, kui.DrawBorders{Left: 1, Right: 1, Top: 1, Bottom: 1}, radius)
	}
}
