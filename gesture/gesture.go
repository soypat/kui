// Package gesture recognizes pointer gestures (presses, taps, double taps, long presses, drags
// and hovering) from the pointer events of up to [MaxPointers] pointers. It allocates nothing.
//
// A [Recognizer] knows nodes only by their handles: hit testing, and which nodes take which
// gestures, are asked of a [Scene]. Time only enters through events and [Recognizer.Tick], so
// recognition is deterministic and replayable.
package gesture

import "github.com/soypat/kui"

// MaxPointers is the maximum number of simultaneously tracked pointers (mouse and touches).
const MaxPointers = 8

// MaxEvents is the maximum number of gestures produced by a single [Recognizer.Feed] or
// [Recognizer.Tick].
const MaxEvents = 2 * MaxPointers

//go:generate stringer -type=Kind -linecomment -output=stringers.go

// Kind is a gesture kind, [Press] to [HoverOut], typed for its methods. Events hold kinds as
// [kui.EventKind]s, so that one switch covers gestures and other kinds; see [KindOf].
type Kind kui.EventKind

// KindOf returns k as a gesture Kind, or false if k is not a gesture kind.
func KindOf(k kui.EventKind) (Kind, bool) {
	if g := Kind(k); g >= evPress && g < evEventExt0 {
		return g, true
	}
	return 0, false
}

// KindString returns the name of event kind k: a gesture kind's or [kui.EventKind.StringConst].
func KindString(k kui.EventKind) string {
	if g, ok := KindOf(k); ok {
		return g.String()
	}
	return k.StringConst()
}

// Sense returns the group of gestures k belongs to, named by the sense nodes take it with:
// SensePress for Press, Release, Tap and SecondaryTap, SenseDrag for drags, SenseHover for
// hovering, and SenseDoubleTap and SenseLongPress. Zero if k is not a gesture kind.
func (k Kind) Sense() Sense {
	switch k {
	case evPress, evRelease, evTap, evSecondaryTap:
		return SensePress
	case evDoubleTap:
		return SenseDoubleTap
	case evLongPress:
		return SenseLongPress
	case evDragStart, evDrag, evDragEnd:
		return SenseDrag
	case evHoverIn, evHoverMove, evHoverOut:
		return SenseHover
	}
	return 0
}

const (
	evPress        = Kind(kui.EventExt0 + iota) // press
	evRelease                                   // release
	evTap                                       // tap
	evSecondaryTap                              // secondary tap
	evDoubleTap                                 // double tap
	evLongPress                                 // long press
	evDragStart                                 // drag start
	evDrag                                      // drag
	evDragEnd                                   // drag end
	evHoverIn                                   // hover in
	evHoverMove                                 // hover move
	evHoverOut                                  // hover out
	evEventExt0
)

// Gesture kinds, numbered from [kui.EventExt0].
const (
	// Press reports a pointer going down. Node is the pressed node, or zero if the press reached
	// none. Pressed nodes receive a Release when the press ends.
	Press = kui.EventKind(evPress)
	// Release reports the end of a press: the pointer went up or was canceled, or another node
	// took over the press's drag.
	Release = kui.EventKind(evRelease)
	// Tap is a primary press and release over the same node without dragging or long pressing.
	Tap = kui.EventKind(evTap)
	// SecondaryTap is a tap with a non primary button.
	SecondaryTap = kui.EventKind(evSecondaryTap)
	// DoubleTap replaces the second of two taps close in time and space on nodes sensing it.
	DoubleTap = kui.EventKind(evDoubleTap)
	// LongPress is a press held without dragging for [Config.LongPressTime] on nodes sensing it.
	LongPress = kui.EventKind(evLongPress)
	// Drag gestures report pointer motion after a press moved beyond [Config.DragSlop]. DX, DY
	// hold the motion since the previous drag gesture, or since the press for DragStart.
	DragStart = kui.EventKind(evDragStart)
	Drag      = kui.EventKind(evDrag)
	DragEnd   = kui.EventKind(evDragEnd)
	// Hover gestures report the pointer entering, moving over and leaving a node.
	HoverIn   = kui.EventKind(evHoverIn)
	HoverMove = kui.EventKind(evHoverMove)
	HoverOut  = kui.EventKind(evHoverOut)
	// EventExt0 is the start of the event kinds of routers built on gestures, i.e. UI scene.
	EventExt0 = kui.EventKind(evEventExt0)
)

// Event is a gesture of one pointer: Node is the node the gesture is for, X, Y the pointer
// position, DX, DY the drag motion and Source the pointer, see [kui.Event].
type Event kui.Event

// KuiEvent returns e as a [kui.Event], for its methods.
func (e *Event) KuiEvent() *kui.Event { return (*kui.Event)(e) }

func (e *Event) Gesture() (Kind, bool) { return KindOf(e.Kind) }

// Sense is the set of pointer input a node takes. It does not depend on kind values, so routers
// can number their events freely.
type Sense uint8

const (
	// SensePress makes presses hit the node: Press, Release, Tap and SecondaryTap.
	SensePress Sense = 1 << iota
	// SenseDrag makes the node receive the drags of presses on it, and of presses on descendants
	// not sensing drags (see [Scene.DragTarget]).
	SenseDrag
	// SenseDoubleTap makes a quick second tap on the node a DoubleTap.
	SenseDoubleTap
	// SenseLongPress makes a press held on the node a LongPress. Its release is not a tap.
	SenseLongPress
	// SenseHover makes the node receive HoverIn, HoverMove and HoverOut.
	SenseHover
	// SenseScroll makes the node receive wheel scrolls. Not a gesture: routers hit test
	// [kui.EventScroll] with it.
	SenseScroll
)

// Scene is what a [Recognizer] needs to know about the nodes under the pointers. Removed nodes
// must not be hit, sense input nor take drags.
type Scene interface {
	// Press returns the node a press at p goes to and whether it is pressed. A node that is not
	// pressed only receives the press's drags, i.e. a scroll container panned by dragging
	// content that takes no presses. Zero for none.
	Press(p kui.Point) (node kui.Handle, pressed bool)
	// Hover returns the node hovered at p, or zero for none.
	Hover(p kui.Point) kui.Handle
	// DragTarget returns the node receiving the drags of a press on n: n itself, a node taking
	// the press over, or zero if the press does not drag.
	DragTarget(n kui.Handle) kui.Handle
	// Sense returns the input node n takes, zero for none. Recognizers ask it about
	// SenseDoubleTap and SenseLongPress: nodes without them get taps instead.
	Sense(n kui.Handle) Sense
}

// Config configures gesture recognition. Zero fields take defaults.
type Config struct {
	DragSlop      float32 // Pointer travel in pixels before a press becomes a drag. Default 4.
	DoubleTapTime int64   // Maximum nanoseconds between taps of a double tap. Default 300ms.
	LongPressTime int64   // Nanoseconds a press must be held to become a long press. Default 500ms.
}

// Recognizer turns pointer events into gestures. Configure it before use.
type Recognizer struct {
	cfg      Config
	pointers [MaxPointers]pointer
}

// pointer is the gesture state of one pointer.
type pointer struct {
	used   bool
	source uint8
	down   bool
	button kui.PointerButton
	// target is the node that received the press, the node panned by a press that reached
	// none, or the node that took over the press's drag.
	target   kui.Handle
	pressed  bool // target received Press, so it gets Release.
	downPos  kui.Point
	lastPos  kui.Point
	downTime int64
	dragging bool
	longDone bool // Long press fired, so release is not a tap.
	hover    kui.Handle
	// Last tap, for double tap detection.
	tapTime int64
	tapPos  kui.Point
	tapNode kui.Handle
}

// Configure sets the configuration of r and forgets all pointers.
func (r *Recognizer) Configure(cfg Config) {
	if cfg.DragSlop <= 0 {
		cfg.DragSlop = 4
	}
	if cfg.DoubleTapTime <= 0 {
		cfg.DoubleTapTime = 300e6
	}
	if cfg.LongPressTime <= 0 {
		cfg.LongPressTime = 500e6
	}
	*r = Recognizer{cfg: cfg}
}

// Feed recognizes the gestures of event ev, appending them to dst. It handles pointer down,
// up, move, cancel and leave events; blur cancels the presses of all pointers. Other events
// are ignored. At most [MaxEvents] gestures are appended.
func (r *Recognizer) Feed(s Scene, ev *kui.Event, dst []Event) []Event {
	switch ev.Kind {
	case kui.EventPointerDown:
		return r.down(s, ev, dst)
	case kui.EventPointerUp:
		return r.up(s, r.pointer(ev.Source), ev, false, dst)
	case kui.EventPointerCancel:
		return r.up(s, r.pointer(ev.Source), ev, true, dst)
	case kui.EventPointerMove:
		return r.move(s, ev, dst)
	case kui.EventPointerLeave:
		return r.hover(r.pointer(ev.Source), 0, ev, dst)
	case kui.EventBlur:
		for i := range r.pointers {
			p := &r.pointers[i]
			if p.used && p.down {
				cancel := *ev
				cancel.Source = p.source
				cancel.X, cancel.Y = p.lastPos.X, p.lastPos.Y
				dst = r.up(s, p, &cancel, true, dst)
			}
		}
	}
	return dst
}

// Tick fires the long presses due at time now, appending them to dst.
func (r *Recognizer) Tick(s Scene, now int64, dst []Event) []Event {
	for i := range r.pointers {
		p := &r.pointers[i]
		if !p.longPending() || now-p.downTime < r.cfg.LongPressTime || s.Sense(p.target)&SenseLongPress == 0 {
			continue
		}
		p.longDone = true
		dst = append(dst, Event{
			Time: now, Node: p.target, X: p.lastPos.X, Y: p.lastPos.Y,
			Kind: LongPress, Source: p.source, Button: p.button,
		})
	}
	return dst
}

// Deadline returns the time of the next long press, at which [Recognizer.Tick] should be called.
func (r *Recognizer) Deadline(s Scene) (deadline int64, ok bool) {
	for i := range r.pointers {
		p := &r.pointers[i]
		if !p.longPending() || s.Sense(p.target)&SenseLongPress == 0 {
			continue
		}
		if t := p.downTime + r.cfg.LongPressTime; !ok || t < deadline {
			deadline, ok = t, true
		}
	}
	return deadline, ok
}

// longPending reports whether p is pressed and may still become a long press.
func (p *pointer) longPending() bool { return p.used && p.pressed && !p.dragging && !p.longDone }

func (r *Recognizer) pointer(source uint8) *pointer {
	var free *pointer
	for i := range r.pointers {
		p := &r.pointers[i]
		if p.used && p.source == source {
			return p
		} else if !p.used && free == nil {
			free = p
		}
	}
	if free == nil {
		free = &r.pointers[0] // Too many pointers, reuse a slot.
	}
	*free = pointer{used: true, source: source}
	return free
}

// newEvent returns a gesture of the given kind for node n made by pointer event ev.
func newEvent(kind kui.EventKind, n kui.Handle, ev *kui.Event) Event {
	return Event{Time: ev.Time, Node: n, X: ev.X, Y: ev.Y, Kind: kind, Source: ev.Source, Button: ev.Button, KeyMods: ev.KeyMods}
}

// dragEvent returns a drag gesture for node n with the motion from position from to ev's.
func dragEvent(kind kui.EventKind, n kui.Handle, ev *kui.Event, from kui.Point) Event {
	e := newEvent(kind, n, ev)
	e.DX, e.DY = ev.X-from.X, ev.Y-from.Y
	return e
}

func dist2(a, b kui.Point) float32 {
	dx, dy := a.X-b.X, a.Y-b.Y
	return dx*dx + dy*dy
}

func (r *Recognizer) down(s Scene, ev *kui.Event, dst []Event) []Event {
	p := r.pointer(ev.Source)
	pos := kui.Point{X: ev.X, Y: ev.Y}
	target, pressed := s.Press(pos)
	p.down = true
	p.button = ev.Button
	p.target, p.pressed = target, pressed && target != 0
	p.downPos, p.lastPos = pos, pos
	p.downTime = ev.Time
	p.dragging, p.longDone = false, false
	if !p.pressed {
		target = 0
	}
	return append(dst, newEvent(Press, target, ev))
}

func (r *Recognizer) move(s Scene, ev *kui.Event, dst []Event) []Event {
	p := r.pointer(ev.Source)
	pos := kui.Point{X: ev.X, Y: ev.Y}
	slop := r.cfg.DragSlop
	if p.down && p.target != 0 {
		if p.dragging {
			dst = append(dst, dragEvent(Drag, p.target, ev, p.lastPos))
			p.lastPos = pos
		} else if dist2(pos, p.downPos) > slop*slop {
			dst = r.startDrag(s, p, ev, dst)
		}
	}
	return r.hover(p, s.Hover(pos), ev, dst)
}

// startDrag makes p's press a drag of the node taking it, if any.
func (r *Recognizer) startDrag(s Scene, p *pointer, ev *kui.Event, dst []Event) []Event {
	d := s.DragTarget(p.target)
	if d == 0 {
		return dst
	} else if d != p.target {
		// Another node takes over the press, i.e. a scroll container panning its content.
		if p.pressed {
			dst = append(dst, newEvent(Release, p.target, ev))
		}
		p.target, p.pressed = d, false
	}
	p.dragging = true
	p.lastPos = kui.Point{X: ev.X, Y: ev.Y}
	return append(dst, dragEvent(DragStart, d, ev, p.downPos))
}

func (r *Recognizer) up(s Scene, p *pointer, ev *kui.Event, cancel bool, dst []Event) []Event {
	if !p.down {
		return dst
	}
	p.down = false
	target, pressed := p.target, p.pressed
	p.target, p.pressed = 0, false
	if p.dragging {
		dst = append(dst, dragEvent(DragEnd, target, ev, p.lastPos))
	} else if pressed && !cancel && !p.longDone {
		if hit, ok := s.Press(kui.Point{X: ev.X, Y: ev.Y}); ok && hit == target {
			dst = append(dst, r.tap(s, p, target, ev))
		}
	}
	if pressed {
		dst = append(dst, newEvent(Release, target, ev))
	}
	return dst
}

// tap returns the tap gesture of p's press on node n ending with event ev.
func (r *Recognizer) tap(s Scene, p *pointer, n kui.Handle, ev *kui.Event) Event {
	pos := kui.Point{X: ev.X, Y: ev.Y}
	slop := r.cfg.DragSlop
	switch {
	case p.button != kui.ButtonPrimary && p.button != kui.ButtonNone:
		return newEvent(SecondaryTap, n, ev)
	case p.tapNode == n && ev.Time-p.tapTime <= r.cfg.DoubleTapTime &&
		dist2(pos, p.tapPos) <= 4*slop*slop && s.Sense(n)&SenseDoubleTap != 0:
		p.tapNode = 0
		return newEvent(DoubleTap, n, ev)
	}
	p.tapNode, p.tapTime, p.tapPos = n, ev.Time, pos
	return newEvent(Tap, n, ev)
}

// hover moves p's hover to node n, zero for none.
func (r *Recognizer) hover(p *pointer, n kui.Handle, ev *kui.Event, dst []Event) []Event {
	if n == p.hover {
		if n != 0 {
			dst = append(dst, newEvent(HoverMove, n, ev))
		}
		return dst
	}
	if p.hover != 0 {
		dst = append(dst, newEvent(HoverOut, p.hover, ev))
	}
	p.hover = n
	if n != 0 {
		dst = append(dst, newEvent(HoverIn, n, ev))
	}
	return dst
}
