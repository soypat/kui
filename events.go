package kui

//go:generate stringer -linecomment -type=EventKind,keymod,PointerButton,errGeneric,Key,Wrap,Align -output=stringers.go

import (
	"github.com/soypat/kui/internal"
)

// EventReader is the consumer end of an event queue. A Reader has a single consumer.
// Usually a UI context will have a single EventReader instance that handles events from one or many producers.
type EventReader interface {
	// DrainEvents moves up to len(dst) of the oldest events into dst and returns how many it moved.
	// Drained times never decrease so it is the EventReader's responsability to order events written by [Event.Time].
	// The text of drained events stays valid until the next Drain.
	DrainEvents(dst []Event) int
	// EventTextAppend appends the text of e, an event returned by the last Drain, to dst. Valid when [Event.Len]>0.
	EventTextAppend(dst []byte, e *Event) []byte
}

// EventWriter is the producer end of an event queue. A Writer has a single producer.
type EventWriter interface {
	// WriteEvents queues the longest prefix of src that fits and returns its length.
	WriteEvents(src []Event) int
}

// Event is a single input event. It contains no pointers. Field meaning depends on Kind, see [EventKind].
// Event defines raw device input before being processed by the UI. Event does not offer gesture/sense events.
// These need to happen at the UI construction level since widgets define their interaction with gestures.
type Event struct {
	Time         int64         // Monotonic time of event in nanoseconds, arbitrary epoch.
	Node         Handle        // Target node. Zero until the router resolves it.
	X, Y, DX, DY float32       // Geometry in logical pixels, e.g. pointer position (X,Y), scroll amount (DX,DY).
	Code         uint32        // [Key] for key events, rune for single-rune text. Opaque text offset when Len>0.
	Len          uint16        // Byte length of multi-rune text/preedit (e.g. paste, IME). Zero otherwise.
	Kind         EventKind     // Kind provides interpretation for [Event] fields. See list of possible [EventKind] e.g. [EventPointerDown]
	Source       uint8         // Pointer ID: 0 is the mouse, touch points use 1 and up e.g. User dragging five fingers across screen has 5 distinct Source values 1..5.
	Button       PointerButton // Button pressed or released on pointer down/up events.
	KeyMods      KeyMods       // Modifier state (shift, ctrl, caps lock...) when the event was created.
}

// Key returns the key of a [KindKeyUp] or [KindKeyDown].
func (e *Event) Key() Key { return Key(e.Code) }

// Rune returns the rune of a single rune text event.
func (e *Event) Rune() rune { return rune(e.Code) }

// IsString returns true if the event does not contain full text of event and requires side-channel data.
func (e *Event) IsString() bool {
	return e.Len > 0 && (e.Kind == EventText || e.Kind == EventPreedit)
}

// IsRouted checks if [Event.Node] field has been set and event ready to be routed to UI node.
func (e *Event) IsRouted() bool { return !e.Node.IsZero() }

// Resize returns the width, height and scale values a EventResize was created with. See [EventBuilder.Resize].
func (e *Event) Resize() (w, h, scale float32) {
	if e.Kind != EventResize {
		panic("expected resize")
	}
	return e.X, e.Y, e.DX
}

// Coalesce checks if an incoming new event can be merged into a preexisting event e.
// It returns true if the event was succesfully merged into e, in which case incoming may be discarded and replaced by e.
func (e *Event) Coalesce(incoming Event) (coalesced bool) {
	if e.Kind == incoming.Kind && e.Source == incoming.Source && e.KeyMods == incoming.KeyMods {
		switch e.Kind {
		case EventPointerMove:
			e.Time, e.X, e.Y = incoming.Time, incoming.X, incoming.Y
			coalesced = true
		case EventScroll:
			e.Time, e.X, e.Y = incoming.Time, incoming.X, incoming.Y
			e.DX += incoming.DX
			e.DY += incoming.DY
			coalesced = true
		}
	}
	return coalesced
}

// EventBuilder is a convenience data structure for creating events. It serves mainly
// as an example for users to understand the semantics of [Event] since not everyone will find its API
// a perfect fit for their use case. The zero EventBuilder is valid and ready for use.
type EventBuilder struct {
	mono    internal.MonotonicTime
	source  uint8
	keyMods KeyMods
}

// EventBuilderConfig configures EventBuilder. EventBuilder zero value is valid so this is optional configuration.
type EventBuilderConfig struct {
	Source   uint8        // Sets [Event.Source].
	Nanotime func() int64 // monotonic time source.
}

// Configure is called to customize EventBuilder output. Zeros the [KeyMods] state.
// Non-zero sources typically refers to touch points for Pointer events but can serve as device identifier as well.
func (events *EventBuilder) Configure(cfg EventBuilderConfig) {
	events.mono.Config(cfg.Nanotime)
	events.source = cfg.Source
	events.keyMods = 0
}

// KeyMods returns currently set [KeyMods].
func (events *EventBuilder) KeyMods() (mods KeyMods) { return events.keyMods }

// SetKeyMods clears keymods and sets mods as new event output [KeyMods].
func (events *EventBuilder) SetKeyMods(mods KeyMods) { events.keyMods = mods }

// NewEvent creates a [EventKind] of [Event] from the EventBuilder source setting the time, source and [KeyMods] of the event.
func (events *EventBuilder) NewEvent(kind EventKind) Event {
	return Event{Time: events.mono.Nanotime(), Source: events.source, Kind: kind, KeyMods: events.keyMods}
}

// PointerUp returns a [EventPointerUp] event.
func (events *EventBuilder) PointerUp(x, y float32, b PointerButton) Event {
	return events.pointer(EventPointerUp, x, y, b)
}

// PointerMove returns a [PointerMove] event.
func (events *EventBuilder) PointerMove(x, y float32, b PointerButton) Event {
	return events.pointer(EventPointerMove, x, y, b)
}

// PointerDown returns a [PointerDown] event.
func (events *EventBuilder) PointerDown(x, y float32, b PointerButton) Event {
	return events.pointer(EventPointerDown, x, y, b)
}
func (events *EventBuilder) pointer(kind EventKind, x, y float32, b PointerButton) Event {
	ev := events.NewEvent(kind)
	ev.X, ev.Y, ev.Button = x, y, b
	return ev
}

// Scroll returns a scroll event at x, y.
func (events *EventBuilder) Scroll(x, y, dx, dy float32) Event {
	ev := events.NewEvent(EventScroll)
	ev.X, ev.Y, ev.DX, ev.DY = x, y, dx, dy
	return ev
}

// KeyDown returns a key down event.
func (events *EventBuilder) KeyDown(k Key) Event {
	return events.code(EventKeyDown, uint32(k))
}

// KeyUp returns a key up event.
func (events *EventBuilder) KeyUp(k Key) Event {
	return events.code(EventKeyUp, uint32(k))
}

// Rune returns a single rune text event.
func (events *EventBuilder) Rune(r rune) Event {
	return events.code(EventText, uint32(r))
}

func (events *EventBuilder) code(kind EventKind, code uint32) Event {
	ev := events.NewEvent(kind)
	ev.Code = code
	return ev
}

// Resize returns a resize event for a window of w by h logical pixels with the given scale factor.
func (events *EventBuilder) Resize(w, h, scale float32) Event {
	ev := events.NewEvent(EventResize)
	ev.X, ev.Y, ev.DX = w, h, scale
	return ev
}

// PointerButton is a pointer button, used primarily for defining pointer [Event]s.
type PointerButton uint8

const (
	ButtonNone      PointerButton = iota // none
	ButtonPrimary                        // primary
	ButtonSecondary                      // secondary
	ButtonTertiary                       // tertiary
	ButtonBack                           // back
	ButtonForward                        // forward
)

// EventKind identifies the type of an [Event] and how its fields are interpreted.
type EventKind uint8

const (
	eventNone EventKind = iota // none
	// EventPointer events. Source identifies the pointer: 0 is the mouse, touch points use 1 and up.
	// X, Y is the position in logical pixels. Button is set for down and up events.
	EventPointerDown // pointer down
	EventPointerUp   // pointer up
	EventPointerMove // pointer move
	// EventPointerCancel aborts any gesture in progress for Source, i.e. the OS took over a touch.
	EventPointerCancel // pointer cancel
	// EventPointerLeave reports the pointer left the window.
	EventPointerLeave // pointer leave
	// EventScroll is a wheel or touchpad scroll at X, Y. DX, DY is the scroll amount in
	// logical pixels; positive DY scrolls content down.
	EventScroll // scroll
	// Key events. Code is the [Key] (USB HID usage), Mods the modifier state.
	EventKeyDown // keydown
	EventKeyUp   // keyup
	// EventText is committed text input. If Len is zero Code is a single rune,
	// otherwise Code and Len are the offset and length of UTF-8 text in the queue's text buffer.
	EventText // text
	// EventPreedit is IME composition text, encoded like KindText. An empty preedit ends composition.
	EventPreedit // preedit
	// EventResize reports new window dimensions X, Y in logical pixels and DX the scale
	// factor from logical to physical pixels.
	EventResize // resize
	// EventInsets reports safe area insets: X left, Y top, DX right, DY bottom.
	EventInsets // insets
	// EventFocus and KindBlur report the window gained or lost input focus.
	EventFocus // focus
	// EventBlur reports lost input focus. See [EventFocus].
	EventBlur // blur
	// EventLifecycle reports an application lifecycle change in Code, see [Lifecycle].
	EventLifecycle // lifecycle
	// EventWake is posted by user code, i.e. from another goroutine, to wake the UI loop. Code is user defined.
	EventWake // wake
	// EventFrame is a display refresh tick.
	EventFrame // frame
	numEventKinds

	// EventExt0 is the start of externally/user defined event kinds.
	EventExt0 EventKind = 32 // ext0
)

var _ = [EventExt0]struct{}{}[numEventKinds] // compile time guarantee numEventKinds<EventExt0

// StringConst returns a constant value on unknown or external event kinds without allocating.
func (ev EventKind) StringConst() string {
	if ev < numEventKinds {
		return ev.String()
	} else if ev < EventExt0 {
		return "(undefined)"
	}
	return "(external)"
}

// KeyMods is a bit set of keyboard modifiers used in [Event]s.
type KeyMods keymod

const (
	KeyModShift    = KeyMods(kmShift)
	KeyModCtrl     = KeyMods(kmCtrl)
	KeyModAlt      = KeyMods(kmAlt)
	KeyModSuper    = KeyMods(kmSuper)
	KeyModCapsLock = KeyMods(kmCapsLock)
	KeyModNumLock  = KeyMods(kmNumLock)
)

type keymod uint8 // facilitates autogen of bitset enum stringifying. See [KeyMods.AppendString]

const (
	kmShift    keymod = 1 << iota // shift
	kmCtrl                        // ctrl
	kmAlt                         // alt
	kmSuper                       // super
	kmCapsLock                    // capsL
	kmNumLock                     // numL

	kmMax = iota
)

// AppendString appends all keyboard modifier string representations, each separated by space.
func (km KeyMods) AppendString(dst []byte) []byte {
	i := 0
	appended := false
	for km != 0 && i < kmMax {
		if km&1 != 0 {
			if appended {
				dst = append(dst, ' ')
			}
			dst = append(dst, keymod(1<<i).String()...)
			appended = true
		}
		km >>= 1
	}
	return dst
}

// IsUpper returns true if the text ouput is in upper-case mode. If both shift and caps lock set IsUpper is negated (false).
func (km KeyMods) IsUpper() bool {
	return (km&KeyModShift != 0) != (km&KeyModCapsLock != 0)
}

// Key is a physical key identified by its USB HID keyboard usage ID (usage page 0x07).
// HID usages are portable across platforms and are what USB keyboards attached to
// microcontrollers report natively.
type Key uint16

// Keys by HID usage ID.
const (
	keyNone         Key = 0x00            // none
	KeyA            Key = 0x04 + iota - 1 // A
	KeyB                                  // B
	KeyC                                  // C
	KeyD                                  // D
	KeyE                                  // E
	KeyF                                  // F
	KeyG                                  // G
	KeyH                                  // H
	KeyI                                  // I
	KeyJ                                  // J
	KeyK                                  // K
	KeyL                                  // L
	KeyM                                  // M
	KeyN                                  // N
	KeyO                                  // O
	KeyP                                  // P
	KeyQ                                  // Q
	KeyR                                  // R
	KeyS                                  // S
	KeyT                                  // T
	KeyU                                  // U
	KeyV                                  // V
	KeyW                                  // W
	KeyX                                  // X
	KeyY                                  // Y
	KeyZ                                  // Z
	Key1                                  // 1
	Key2                                  // 2
	Key3                                  // 3
	Key4                                  // 4
	Key5                                  // 5
	Key6                                  // 6
	Key7                                  // 7
	Key8                                  // 8
	Key9                                  // 9
	Key0                                  // 0
	KeyEnter                              // enter
	KeyEscape                             // esc
	KeyBackspace                          // backspace
	KeyTab                                // tab
	KeySpace                              // space
	KeyMinus                              // -
	KeyEqual                              // =
	KeyLeftBracket                        // [
	KeyRightBracket                       // ]
	KeyBackslash                          // \
	KeyNonUSHash                          // #
	KeySemicolon                          // ;
	KeyApostrophe                         // '
	KeyGrave                              // `
	KeyComma                              // ,
	KeyPeriod                             // .
	KeySlash                              // /
	KeyCapsLock                           // capslock
	KeyF1                                 // F1
	KeyF2                                 // F2
	KeyF3                                 // F3
	KeyF4                                 // F4
	KeyF5                                 // F5
	KeyF6                                 // F6
	KeyF7                                 // F7
	KeyF8                                 // F8
	KeyF9                                 // F9
	KeyF10                                // F10
	KeyF11                                // F11
	KeyF12                                // F12
	KeyPrintScreen                        // printscreen
	KeyScrollLock                         // scrolllock
	KeyPause                              // pause
	KeyInsert                             // insert
	KeyHome                               // home
	KeyPageUp                             // pageup
	KeyDelete                             // delete
	KeyEnd                                // end
	KeyPageDown                           // pagedown
	KeyArrowRight                         // right
	KeyArrowLeft                          // left
	KeyArrowDown                          // down
	KeyArrowUp                            // up
)

// Modifier keys.
const (
	KeyLeftCtrl   Key = 0xE0 // lctrl
	KeyLeftShift  Key = 0xE1 // lshift
	KeyLeftAlt    Key = 0xE2 // lalt
	KeyLeftSuper  Key = 0xE3 // lsuper
	KeyRightCtrl  Key = 0xE4 // rctrl
	KeyRightShift Key = 0xE5 // rshift
	KeyRightAlt   Key = 0xE6 // ralt
	KeyRightSuper Key = 0xE7 // rsuper
)

// IsModifier returns true if key is one of known key modifiers e.g. shift, capslock, ctrl, alt.
func (key Key) IsModifier() bool {
	return (key >= 0xE0 && key <= 0xE7) || key == KeyCapsLock
}
