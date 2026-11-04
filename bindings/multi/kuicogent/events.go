package kuicogent

import (
	"image"
	"unicode"

	"cogentcore.org/core/events"
	"cogentcore.org/core/events/key"
	"cogentcore.org/core/system"
	"github.com/soypat/kui"
)

// InputConfig configures an [Input].
type InputConfig struct {
	// Events receives the translated events. Required.
	Events kui.EventWriter
	// Window is the window whose events are translated, for its size and pixel ratio. Required.
	Window system.Window
	// Nanotime stamps events, in monotonic nanoseconds. Defaults to the time since Configure.
	Nanotime func() int64
}

// Input translates the events of a Cogent window to kui events. Positions and sizes are in
// logical pixels: Cogent's dots divided by the screen's device pixel ratio.
type Input struct {
	w       kui.EventWriter
	events  kui.EventBuilder // Stamps events and holds their modifiers.
	size    func() (dots image.Point, scale float32)
	scale   float32
	buf     [1]kui.Event // A field so writing does not allocate.
	dropped int
}

// Configure sets up in. Feed it every event of the window with [Input.Handle].
func (in *Input) Configure(cfg InputConfig) error {
	win := cfg.Window
	if win == nil {
		return kui.ErrInvalidConfig
	}
	return in.configure(cfg.Events, cfg.Nanotime, func() (image.Point, float32) {
		var scale float32
		if sc := win.Screen(); sc != nil {
			scale = sc.DevicePixelRatio
		}
		return win.Size(), scale
	})
}

func (in *Input) configure(w kui.EventWriter, nanotime func() int64, size func() (image.Point, float32)) error {
	if w == nil || size == nil {
		return kui.ErrInvalidConfig
	}
	*in = Input{w: w, size: size, scale: 1}
	in.events.Configure(kui.EventBuilderConfig{Nanotime: nanotime})
	if _, scale := size(); scale > 0 {
		in.scale = scale
	}
	return nil
}

// Scale returns the number of dots (framebuffer pixels) per logical pixel.
func (in *Input) Scale() float32 { return in.scale }

// Dropped returns the number of events dropped because they did not fit the writer.
func (in *Input) Dropped() int { return in.dropped }

// Handle translates ev and writes it. Events kui has no counterpart for are ignored, as are
// the events Cogent derives from others (clicks, chords of pressed keys).
func (in *Input) Handle(ev events.Event) {
	in.events.SetKeyMods(ToMods(ev.Modifiers()))
	switch typ := ev.Type(); typ {
	case events.MouseDown, events.MouseUp:
		b := ToButton(ev.MouseButton())
		if b == kui.ButtonNone {
			return
		}
		x, y := in.pos(ev)
		if typ == events.MouseDown {
			in.write(in.events.PointerDown(x, y, b))
		} else {
			in.write(in.events.PointerUp(x, y, b))
		}
	case events.MouseMove, events.MouseDrag:
		x, y := in.pos(ev)
		in.write(in.events.PointerMove(x, y, kui.ButtonNone))
	case events.Scroll:
		if s, ok := ev.(*events.MouseScroll); ok {
			// Cogent's deltas, in dots, are positive scrolling down like kui's.
			x, y := in.pos(ev)
			in.write(in.events.Scroll(x, y, s.Delta.X/in.scale, s.Delta.Y/in.scale))
		}
	case events.KeyDown, events.KeyUp:
		k := ToKey(ev.KeyCode())
		if k == 0 {
			return
		}
		if typ == events.KeyDown {
			in.write(in.events.KeyDown(k))
		} else {
			in.write(in.events.KeyUp(k))
		}
	case events.KeyChord:
		// Typed text comes as chords without a key code; chords with one repeat key presses.
		r := ev.KeyRune()
		shortcut := ToMods(ev.Modifiers())&(kui.KeyModCtrl|kui.KeyModSuper) != 0
		if ev.KeyCode() == key.CodeUnknown && unicode.IsPrint(r) && !shortcut {
			in.write(in.events.Rune(r))
		}
	case events.WindowResize:
		in.resize()
	case events.Window:
		we, ok := ev.(*events.WindowEvent)
		if !ok {
			return
		}
		switch we.Action {
		case events.WinShow:
			in.resize()
		case events.WinFocus:
			in.write(in.events.NewEvent(kui.EventFocus))
		case events.WinFocusLost:
			in.write(in.events.NewEvent(kui.EventBlur))
		}
	}
}

func (in *Input) pos(ev events.Event) (x, y float32) {
	p := ev.WindowPos()
	return float32(p.X) / in.scale, float32(p.Y) / in.scale
}

// resize writes the window's size, which Cogent's resize events do not carry.
func (in *Input) resize() {
	dots, scale := in.size()
	if dots.X <= 0 || dots.Y <= 0 {
		return
	}
	if !(scale > 0) {
		scale = 1
	}
	in.scale = scale
	in.write(in.events.Resize(float32(dots.X)/scale, float32(dots.Y)/scale, scale))
}

func (in *Input) write(e kui.Event) {
	in.buf[0] = e
	in.dropped += 1 - in.w.WriteEvents(in.buf[:])
}

// ToKey returns the kui key of Cogent key code c, or zero if kui has none. Cogent's codes are
// USB HID usages like kui's; keypad keys other than Enter have no kui key.
func ToKey(c key.Codes) kui.Key {
	switch {
	case c >= key.CodeA && c <= key.CodeUpArrow, c >= key.CodeLeftControl && c <= key.CodeRightMeta:
		return kui.Key(c)
	case c == key.CodeKeypadEnter:
		return kui.KeyEnter
	}
	return 0
}

// ToMods returns the kui modifiers of Cogent modifiers m. Meta, the command or Windows key,
// is kui's Super.
func ToMods(m key.Modifiers) kui.KeyMods {
	var mods kui.KeyMods
	for _, p := range [...]struct {
		c key.Modifiers
		k kui.KeyMods
	}{
		{key.Shift, kui.KeyModShift},
		{key.Control, kui.KeyModCtrl},
		{key.Alt, kui.KeyModAlt},
		{key.Meta, kui.KeyModSuper},
		{key.CapsLock, kui.KeyModCapsLock},
		{key.NumLock, kui.KeyModNumLock},
	} {
		if m&(1<<p.c) != 0 { // Modifiers holds bits indexed by the constants.
			mods |= p.k
		}
	}
	return mods
}

// ToButton returns the kui button of Cogent button b.
func ToButton(b events.Buttons) kui.PointerButton {
	switch b {
	case events.Left:
		return kui.ButtonPrimary
	case events.Right:
		return kui.ButtonSecondary
	case events.Middle:
		return kui.ButtonTertiary
	}
	return kui.ButtonNone
}
