package kuiscene

import (
	"unicode/utf8"
	"unsafe"

	"github.com/soypat/kui"
	"github.com/soypat/kui/gesture"
)

//go:generate stringer -type=eventkind -linecomment -output=eventkind_string.go

// Event kinds of the kernel and of kinds, numbered from [gesture.EventExt0]. Nodes also receive,
// as [WidgetEvent]s:
//   - The gestures of package [gesture] (Press, Tap, DoubleTap, LongPress, Drag*, Hover*...),
//     routed by hit testing to nodes sensing them, see [KindSpec.Sense].
//   - [kui.EventScroll], a wheel scroll of DX, DY over nodes sensing [gesture.SenseScroll].
//   - [kui.EventKeyDown], [kui.EventKeyUp], [kui.EventText] and [kui.EventPreedit], delivered to
//     the focused node. Read text with [Scene.EventText].
//
// They map onto Fyne's input interfaces (Tappable, DoubleTappable, SecondaryTappable, Draggable,
// Scrollable, desktop.Hoverable, Focusable, desktop.Keyable) so a Fyne-like tier can be built on
// top.
const (
	// EventFocusIn and EventFocusOut report keyboard focus changes.
	EventFocusIn  = kui.EventKind(evFocusIn)
	EventFocusOut = kui.EventKind(evFocusOut)
	// EventActivate is emitted by kinds when the user activates the node, i.e. a button click.
	EventActivate = kui.EventKind(evActivate)
	// EventChanged is emitted by kinds when their value changes. Code holds the new value.
	EventChanged = kui.EventKind(evChanged)
	// EventTimer is a timer set with [Scene.SetTimer] firing. Code is the timer's code.
	EventTimer = kui.EventKind(evTimer)
	// EventDismiss asks the active modal node (see [FlagModal]) to close: the user pressed
	// outside it or pressed Escape. The kernel does not hide the node itself.
	EventDismiss = kui.EventKind(evDismiss)
	// EventExt0 is the start of application defined event kinds, emitted with [Scene.Emit].
	EventExt0 = kui.EventKind(evEventExt0)
)

type eventkind kui.EventKind

const (
	evFocusIn  = eventkind(gesture.EventExt0 + iota) // focus in
	evFocusOut                                       // focus out
	evActivate                                       // activate
	evChanged                                        // changed
	evTimer                                          // timer
	evDismiss                                        // dismiss
	evEventExt0
)

// KindString returns the name of event kind k: a kernel kind's or [gesture.KindString].
func KindString(k kui.EventKind) string {
	if e := eventkind(k); e >= evFocusIn && e < evEventExt0 {
		return e.String()
	}
	return gesture.KindString(k)
}

// sensePress is the input that makes presses hit a node.
const sensePress = gesture.SensePress | gesture.SenseDrag | gesture.SenseDoubleTap | gesture.SenseLongPress

// WidgetEvent is an event routed to a node: Node is set and pointer positions are relative to
// the node's box. DX, DY is the drag motion or scroll amount. Code is the key of key events, the
// value of EventChanged or the text of text events.
type WidgetEvent kui.Event

// KuiEvent returns e as a [kui.Event], for its methods.
func (e *WidgetEvent) KuiEvent() *kui.Event { return (*kui.Event)(e) }

// Gesture returns the gesture kind of e, or false if e is not a gesture.
func (e *WidgetEvent) Gesture() (gesture.Kind, bool) { return gesture.KindOf(e.Kind) }

// Emit appends an event to the output of the current Step. Its Reduce is called after those of
// the events already emitted. Events emitted outside Step are delivered with the next Step.
// Events are dropped if the event buffer is full; see [Scene.DroppedEvents].
func (k *Scene) Emit(e WidgetEvent) {
	if len(k.m.events) == cap(k.m.events) {
		k.dropped++
		return
	}
	if e.Time == 0 {
		e.Time = k.now
	}
	k.m.events = append(k.m.events, e)
}

// Events returns the events produced by the last Step. Valid until the next Step.
func (k *Scene) Events() []WidgetEvent { return k.m.events[:k.stepEnd] }

// Request queues a request to the host. text, if not empty, is copied to the buffer returned by
// [Scene.Requests] and located by the request's Text. Requests made outside Step are delivered
// with the next Step. It returns [kui.ErrBufferFull] if [MemoryConfig.MaxRequests] requests are
// queued and [kui.ErrShortBuffer] if text does not fit [MemoryConfig.RequestTextBytes].
func (k *Scene) Request(r kui.Request, text string) error {
	m := k.m
	if len(m.reqs) == cap(m.reqs) {
		return kui.ErrBufferFull
	} else if len(text) > cap(m.reqText)-len(m.reqText) {
		return kui.ErrShortBuffer
	}
	r.Text = kui.Span{Start: uint32(len(m.reqText)), Len: uint32(len(text))}
	m.reqText = append(m.reqText, text...)
	m.reqs = append(m.reqs, r)
	return nil
}

// Requests returns the host requests produced by the last Step and the buffer holding their
// text ([kui.Request.Text]). Both are valid until the next Step.
func (k *Scene) Requests() (reqs []kui.Request, text []byte) {
	return k.m.reqs[:k.reqEnd], k.m.reqText
}

// RequestText returns the text of request r. Valid until the next Step.
func (k *Scene) RequestText(r *kui.Request) string {
	if r.Text.Len == 0 || r.Text.End() > uint32(len(k.m.reqText)) {
		return ""
	}
	return unsafe.String(&k.m.reqText[r.Text.Start], int(r.Text.Len))
}

// EventText writes the text of a [kui.EventText] or [kui.EventPreedit] event to dst and returns
// its length. Text that does not fit dst is dropped at a rune boundary. Single rune events are
// decoded directly; longer text is read from the Step's [TextSource].
func (k *Scene) EventText(dst []byte, e *WidgetEvent) int {
	switch r := rune(e.Code); {
	case e.Kind != kui.EventText && e.Kind != kui.EventPreedit:
		return 0
	case e.Len == 0 && utf8.RuneLen(r) > 0 && utf8.RuneLen(r) <= len(dst) && r != 0:
		return utf8.EncodeRune(dst, r)
	case e.Len == 0 || k.textSrc == nil:
		return 0
	}
	return k.textSrc.EventTextWrite(dst, e.KuiEvent())
}

// hitIndex holds the hit testable boxes of a layout in paint order, as parallel arrays.
type hitIndex struct {
	x0, y0, x1, y1 []float32
	sense          []gesture.Sense
	node           []int32
	n              int
}

func (h *hitIndex) init(n int) {
	h.x0 = resize(h.x0, n)
	h.y0 = resize(h.y0, n)
	h.x1 = resize(h.x1, n)
	h.y1 = resize(h.y1, n)
	h.sense = resize(h.sense, n)
	h.node = resize(h.node, n)
	h.n = 0
}

func (h *hitIndex) add(r kui.Rect, sense gesture.Sense, node int32) {
	if h.n == len(h.node) || r.Empty() {
		return
	}
	i := h.n
	h.x0[i], h.y0[i], h.x1[i], h.y1[i] = r.X, r.Y, r.X+r.W, r.Y+r.H
	h.sense[i] = sense
	h.node[i] = node
	h.n++
}

// query returns the topmost node at x, y sensing any of sense, or -1.
func (h *hitIndex) query(x, y float32, sense gesture.Sense) int32 {
	x0, y0, x1, y1, s := h.x0[:h.n], h.y0[:h.n], h.x1[:h.n], h.y1[:h.n], h.sense[:h.n]
	for i := h.n - 1; i >= 0; i-- {
		if x >= x0[i] && x < x1[i] && y >= y0[i] && y < y1[i] && s[i]&sense != 0 {
			return h.node[i]
		}
	}
	return -1
}

// HitTest returns the topmost node at p sensing any of sense, or zero. Nodes with [FlagScroll]
// sense [gesture.SenseScroll].
func (k *Scene) HitTest(p kui.Point, sense gesture.Sense) kui.Handle {
	k.ensureLayout()
	if i := k.m.hits.query(p.X, p.Y, sense); i >= 0 {
		return k.handle(i)
	}
	return 0
}
