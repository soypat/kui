package kui2go

import (
	"math"
	"sync/atomic"
	"unicode/utf8"

	"github.com/soypat/kui"
	"github.com/soypat/kui/internal"
)

var (
	_ kui.EventReader = (*EventSPSC)(nil)
	_ kui.EventWriter = (*EventSPSC)(nil)
	_ kui.EventReader = (*EventFan)(nil)
)

// EventSPSC is a lock free single producer single consumer event queue with a buffer for text. Only
// one goroutine may write to it and only one may drain it, which may be the same one, i.e. a
// host pushing input from callbacks and draining it each frame; producers running on another
// goroutine or in an interrupt handler each need a queue of their own, merged by an [EventFan].
// The text of drained events stays valid until the next drain.
//
// Events move in batches: publishing or draining any number of them costs two atomic
// operations, which disable interrupts on microcontrollers without atomic instructions.
// High frequency input written with PushCoalesce is merged and published by Flush.
type EventSPSC struct {
	ev    internal.SPSC[kui.Event]
	text  []byte
	tmask uint32        // len(text)-1.
	thead atomic.Uint32 // Text bytes freed. Written by the consumer.

	// Producer state.
	wttail uint32 // Text bytes written.

	// Consumer state.
	release uint32 // Text position the next drain frees, the end of the last drained text.
	freed   uint32 // Copy of thead.
	last    int64  // Time of the last drained event.
}

// Reset sets the buffers and empties the queue. len(events) must be a power of two and so must
// len(text), unless it is zero for single rune text only. Text is freed by the drain after the
// one returning its event, so text must hold the text written between two drains plus that of
// the last drained events. Reset must not be called concurrently with writes or drains.
func (q *EventSPSC) Reset(events []kui.Event, text []byte) error {
	if n := len(text); n > 0 && (n&(n-1) != 0 || n > 1<<30) {
		return kui.ErrInvalidConfig
	}
	*q = EventSPSC{text: text, last: math.MinInt64}
	if len(text) > 0 {
		q.tmask = uint32(len(text) - 1)
	}
	if !q.ev.Reset(events) {
		return kui.ErrInvalidConfig
	}
	return nil
}

// Push writes and publishes e, along with the events written by PushCoalesce before it.
// It returns false if the queue is full or e is a text event whose text is held elsewhere
// ([kui.Event.IsString]); write text with WriteTextEvent. Producer only.
func (q *EventSPSC) Push(e kui.Event) bool {
	if e.IsString() || !q.ev.Push(e) {
		return false
	}
	q.ev.Publish()
	return true
}

// PushCoalesce writes e, merging it into the last written event if that one is not published
// yet and absorbs it ([kui.Event.Coalesce]): consecutive moves of a pointer and consecutive
// scrolls, so high frequency input does not fill the queue. It does not publish e: the next
// Push, WriteTextEvent, WriteEvents or Flush does. It returns false like Push. Producer only.
func (q *EventSPSC) PushCoalesce(e kui.Event) bool {
	if last := q.ev.Last(); last != nil && last.Coalesce(e) {
		return true
	}
	return !e.IsString() && q.ev.Push(e)
}

// Flush publishes the events written by PushCoalesce. Producer only.
func (q *EventSPSC) Flush() { q.ev.Publish() }

// WriteEvents implements [kui.EventWriter], publishing the events along with those written by
// PushCoalesce before them. It stops before the first text event whose text is held elsewhere
// ([kui.Event.IsString]); write text with WriteTextEvent. Producer only.
func (q *EventSPSC) WriteEvents(src []kui.Event) int {
	for i := range src {
		if src[i].IsString() {
			src = src[:i]
			break
		}
	}
	n := q.ev.Write(src)
	q.ev.Publish()
	return n
}

// WriteTextEvent implements [kui.EventWriter], writing and publishing e, of kind
// [kui.EventText] or [kui.EventPreedit], along with the events written by PushCoalesce before
// it. Single runes are encoded in e.Code; longer text is copied to the text buffer. It returns
// [kui.ErrBufferFull] if the queue is full and [kui.ErrShortBuffer] if the text does not fit,
// writing nothing. Producer only.
func (q *EventSPSC) WriteTextEvent(e kui.Event, text []byte) error {
	if e.Kind != kui.EventText && e.Kind != kui.EventPreedit {
		return kui.ErrInvalidConfig
	}
	if q.ev.Free() == 0 {
		return kui.ErrBufferFull
	}
	e.Code, e.Len = 0, 0
	if r, size := utf8.DecodeRune(text); size == len(text) && r != utf8.RuneError {
		e.Code = uint32(r)
	} else if len(text) > 0 {
		if len(text) > 0xffff || len(text) > len(q.text)-int(q.wttail-q.thead.Load()) {
			return kui.ErrShortBuffer
		}
		i := int(q.wttail & q.tmask)
		c := copy(q.text[i:], text)
		copy(q.text, text[c:])
		e.Code, e.Len = q.wttail, uint16(len(text))
		q.wttail += uint32(len(text))
	}
	q.ev.Push(e) // Fits: Free reported room.
	q.ev.Publish()
	return nil
}

// DrainEvents implements [kui.EventReader]. Times are raised to the previous drained time if
// the producer's clock went back. Consumer only.
func (q *EventSPSC) DrainEvents(dst []kui.Event) int {
	q.begin()
	n := q.ev.Read(dst)
	q.took(dst[:n], &q.last, 0)
	q.ev.Release()
	return n
}

// EventTextWrite implements [kui.EventReader]. Consumer only.
func (q *EventSPSC) EventTextWrite(dst []byte, e *kui.Event) int {
	if !e.IsString() {
		return writeRune(dst, e)
	}
	if int(e.Len) > len(q.text) {
		return 0
	}
	i := int(e.Code & q.tmask)
	n := min(int(e.Len), len(dst))
	if n < int(e.Len) {
		// Drop the rune cut by the end of dst.
		for n > 0 && !utf8.RuneStart(q.text[(i+n)&int(q.tmask)]) {
			n--
		}
	}
	c := copy(dst[:n], q.text[i:])
	copy(dst[c:n], q.text)
	return n
}

// begin starts a drain: it frees the text of the events drained last and takes a snapshot of
// the events published so far.
func (q *EventSPSC) begin() {
	if q.release != q.freed {
		q.freed = q.release
		q.thead.Store(q.freed)
	}
	q.ev.Snapshot()
}

// took processes events just read into evs, raising their times to at least *last so they
// never decrease, and holding their text until the next drain. The Code of text events becomes
// tag plus the position of their text in the text buffer.
func (q *EventSPSC) took(evs []kui.Event, last *int64, tag uint32) {
	t := *last
	for i := range evs {
		e := &evs[i]
		t = max(t, e.Time)
		e.Time = t
		if e.IsString() {
			q.release = e.Code + uint32(e.Len)
			e.Code = tag | e.Code&q.tmask
		}
	}
	*last = t
}

// writeRune writes the rune of a text event that holds it in its Code to dst, if it fits.
func writeRune(dst []byte, e *kui.Event) int {
	if (e.Kind != kui.EventText && e.Kind != kui.EventPreedit) || e.Code == 0 {
		return 0
	}
	if n := utf8.RuneLen(rune(e.Code)); n < 0 || n > len(dst) {
		return 0
	}
	return utf8.EncodeRune(dst, rune(e.Code))
}

// EventFan merges the events of several producers, i.e. input devices, interrupt handlers and
// goroutines, each writing to an [EventSPSC] of its own. Producers never wait nor contend, a
// flooding producer only fills its own queue, and each producer's events stay in order.
// Each source must have a single producer and EventFan a single consumer; sources are drained
// only through the fan.
type EventFan struct {
	srcs []EventSPSC
	last int64 // Time of the last drained event.
}

// Reset sets the sources the fan drains, one per producer, each already [EventSPSC.Reset] with its
// buffers. At most 256 sources are supported, with text buffers of at most 16 MiB. Queued
// events are kept. Reset must not be called concurrently with DrainEvents.
func (f *EventFan) Reset(srcs []EventSPSC) error {
	if len(srcs) == 0 || len(srcs) > 256 {
		return kui.ErrInvalidConfig
	}
	for i := range srcs {
		if srcs[i].ev.Cap() == 0 || len(srcs[i].text) > 1<<24 {
			return kui.ErrInvalidConfig
		}
	}
	f.srcs = srcs
	f.last = math.MinInt64
	return nil
}

// Source returns the queue of producer i, the only one it may write to.
func (f *EventFan) Source(i int) *EventSPSC { return &f.srcs[i] }

// DrainEvents implements [kui.EventReader]: it moves the earliest events of all sources into
// dst, taking equal times from the source of lowest index first. An event written after a later
// one was drained has its time raised to that one's, so drained times never decrease and
// durations between them, i.e. of gestures, are never negative. Producers must stamp events
// with the same clock.
//
// Each source is read and released with one atomic operation per drain, and runs of events of
// a source preceding all other sources' are copied at once.
func (f *EventFan) DrainEvents(dst []kui.Event) int {
	for i := range f.srcs {
		f.srcs[i].begin()
	}
	n := 0
	for n < len(dst) {
		// Pick the source with the earliest event.
		s := -1
		var t int64
		for i := range f.srcs {
			ev := &f.srcs[i].ev
			if ev.Len() == 0 {
				continue
			}
			if et := ev.At(0).Time; s < 0 || et < t {
				s, t = i, et
			}
		}
		if s < 0 {
			break
		}
		// Its run continues while its events precede the earliest event of every other
		// source, equal times going to the source of lowest index.
		ev := &f.srcs[s].ev
		end := min(ev.Len(), len(dst)-n)
		for i := range f.srcs {
			o := &f.srcs[i].ev
			if i == s || o.Len() == 0 {
				continue
			}
			ot := o.At(0).Time
			h := 1
			for h < end {
				ht := ev.At(h).Time
				if ht > ot || (ht == ot && i < s) {
					break
				}
				h++
			}
			end = h
		}
		m := ev.Read(dst[n : n+end])
		f.srcs[s].took(dst[n:n+m], &f.last, uint32(s)<<24)
		n += m
	}
	for i := range f.srcs {
		f.srcs[i].ev.Release()
	}
	return n
}

// EventTextWrite implements [kui.EventReader].
func (f *EventFan) EventTextWrite(dst []byte, e *kui.Event) int {
	if !e.IsString() {
		return writeRune(dst, e)
	}
	if i := int(e.Code >> 24); i < len(f.srcs) {
		return f.srcs[i].EventTextWrite(dst, e) // The source's text mask drops the index.
	}
	return 0
}
