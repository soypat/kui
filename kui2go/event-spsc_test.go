package kui2go

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/soypat/kui"
)

// events builds the events of tests, whose times and sources are set with at and from.
var events kui.EventBuilder

// at returns e with time t.
func at(t int64, e kui.Event) kui.Event {
	e.Time = t
	return e
}

// from returns e with source s.
func from(s uint8, e kui.Event) kui.Event {
	e.Source = s
	return e
}

func TestEventSPSC(t *testing.T) {
	var q EventSPSC
	if q.Reset(make([]kui.Event, 3), nil) == nil || q.Reset(make([]kui.Event, 4), make([]byte, 3)) == nil {
		t.Fatal("expected error for non power of two buffers")
	}
	if err := q.Reset(make([]kui.Event, 8), nil); err != nil {
		t.Fatal(err)
	}
	const total = 10000
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		var batch [3]kui.Event
		for i := 0; i < total; {
			if i%2 == 0 {
				if q.Push(kui.Event{Time: int64(i)}) {
					i++
				}
				continue
			}
			n := min(len(batch), total-i)
			for j := range n {
				batch[j] = kui.Event{Time: int64(i + j)}
			}
			i += q.WriteEvents(batch[:n])
		}
	}()
	dst := make([]kui.Event, 5)
	next := int64(0)
	for next < total {
		n := q.DrainEvents(dst)
		for _, e := range dst[:n] {
			if e.Time != next {
				t.Fatalf("want event %d, got %d", next, e.Time)
			}
			next++
		}
	}
	wg.Wait()
}

func TestEventSPSCText(t *testing.T) {
	var q EventSPSC
	if err := q.Reset(make([]kui.Event, 4), make([]byte, 8)); err != nil {
		t.Fatal(err)
	}
	text := func(e *kui.Event) string { return eventText(&q, e) }
	for _, s := range []string{"ab", "ñ", "", "xyz"} {
		if err := q.WriteTextEvent(kui.Event{Time: 1, Kind: kui.EventText}, []byte(s)); err != nil {
			t.Fatalf("push %q: %v", s, err)
		}
	}
	if err := q.WriteTextEvent(kui.Event{Time: 2, Kind: kui.EventText}, []byte("fits")); err != kui.ErrBufferFull {
		t.Fatalf("full queue: want ErrBufferFull, got %v", err)
	}
	dst := make([]kui.Event, 8)
	if n := q.DrainEvents(dst); n != 4 {
		t.Fatalf("drained %d events", n)
	}
	if text(&dst[0]) != "ab" || dst[1].Len != 0 || text(&dst[1]) != "ñ" || text(&dst[2]) != "" || text(&dst[3]) != "xyz" {
		t.Fatalf("texts %q %q %q %q", text(&dst[0]), text(&dst[1]), text(&dst[2]), text(&dst[3]))
	}
	// Drained text is held until the next drain: 5 of 8 bytes are in use.
	if err := q.WriteTextEvent(kui.Event{Time: 3, Kind: kui.EventText}, []byte("1234")); err != kui.ErrShortBuffer {
		t.Fatalf("want ErrShortBuffer, got %v", err)
	}
	if text(&dst[0]) != "ab" || text(&dst[3]) != "xyz" {
		t.Fatal("text of drained events changed before the next drain")
	}
	if n := q.DrainEvents(dst); n != 0 { // Frees the text.
		t.Fatalf("drained %d events", n)
	}
	if err := q.WriteTextEvent(kui.Event{Time: 4, Kind: kui.EventPreedit}, []byte("123456")); err != nil { // Wraps around the text buffer.
		t.Fatal(err)
	}
	if n := q.DrainEvents(dst); n != 1 || dst[0].Kind != kui.EventPreedit || text(&dst[0]) != "123456" {
		t.Fatalf("wrapped text: %d events, %q", n, text(&dst[0]))
	}
	// Text whose bytes are held elsewhere is refused, as are text pushes of other kinds.
	foreign := kui.Event{Kind: kui.EventText, Code: 2, Len: 3}
	src := []kui.Event{at(5, events.KeyDown(kui.KeyA)), foreign, at(6, events.KeyUp(kui.KeyA))}
	if q.WriteEvents(src) != 1 || q.Push(foreign) {
		t.Fatal("accepted a text event with its text elsewhere")
	}
	if err := q.WriteTextEvent(kui.Event{Time: 7, Kind: kui.EventKeyDown}, []byte("ab")); err != kui.ErrInvalidConfig {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
	// Times never decrease.
	q.Push(kui.Event{Time: 3})
	if n := q.DrainEvents(dst); n != 2 || dst[0].Time != 5 || dst[1].Time != 5 {
		t.Fatalf("times: %+v", dst[:n])
	}
	// A partial drain frees only the text of the events it returned.
	q.Reset(make([]kui.Event, 4), make([]byte, 8))
	q.WriteTextEvent(kui.Event{Time: 8, Kind: kui.EventText}, []byte("abc"))
	q.WriteTextEvent(kui.Event{Time: 9, Kind: kui.EventText}, []byte("def"))
	if n := q.DrainEvents(dst[:1]); n != 1 || text(&dst[0]) != "abc" {
		t.Fatalf("partial drain: %d events, %q", n, text(&dst[0]))
	}
	q.DrainEvents(dst[:0]) // Frees "abc" but not "def": 5 bytes free.
	if err := q.WriteTextEvent(kui.Event{Time: 10, Kind: kui.EventText}, []byte("123456")); err != kui.ErrShortBuffer {
		t.Fatalf("want ErrShortBuffer, got %v", err)
	}
	if err := q.WriteTextEvent(kui.Event{Time: 10, Kind: kui.EventText}, []byte("12345")); err != nil {
		t.Fatal(err)
	}
	if n := q.DrainEvents(dst); n != 2 || text(&dst[0]) != "def" || text(&dst[1]) != "12345" {
		t.Fatalf("after partial drain: %d events, %q %q", n, text(&dst[0]), text(&dst[1]))
	}
}

// textOf returns the text of producer p's event i.
func textOf(p, i int) string {
	return "p" + strconv.Itoa(p) + ":" + strconv.Itoa(i)
}

// produce writes total events stamped by clock to q as producer p: every third is a text event,
// the rest are written in batches.
func produce(q *EventSPSC, p, total int, clock *atomic.Int64) {
	var batch [4]kui.Event
	for i := 0; i < total; {
		if i%3 == 0 {
			if q.WriteTextEvent(kui.Event{Time: clock.Add(1), Kind: kui.EventText}, []byte(textOf(p, i))) == nil {
				i++
			}
			continue
		}
		n := 0
		for n < len(batch) && i+n < total && (i+n)%3 != 0 {
			batch[n] = kui.Event{Time: clock.Add(1), Kind: kui.EventKeyDown, Source: uint8(p), Code: uint32(i + n)}
			n++
		}
		i += q.WriteEvents(batch[:n])
	}
}

// consume drains r until it returns total events from each of producers, checking each
// producer's events arrive in order with their text and times never decrease.
func consume(t *testing.T, r kui.EventReader, producers, total int) {
	t.Helper()
	next := make([]int, producers)
	dst := make([]kui.Event, 5)
	var buf [16]byte
	last := int64(0)
	for got := 0; got < producers*total; {
		n := r.DrainEvents(dst)
		for i := range dst[:n] {
			e := &dst[i]
			if e.Time < last {
				t.Fatalf("time went back from %d to %d", last, e.Time)
			}
			last = e.Time
			p := int(e.Source)
			if e.Kind == kui.EventText {
				text := buf[:r.EventTextWrite(buf[:], e)]
				p = int(text[1] - '0')
				if want := textOf(p, next[p]); string(text) != want {
					t.Fatalf("producer %d: want text %q, got %q", p, want, text)
				}
			} else if int(e.Code) != next[p] {
				t.Fatalf("producer %d: want event %d, got %d", p, next[p], e.Code)
			}
			next[p]++
		}
		got += n
	}
}

func TestEventSPSCConcurrentText(t *testing.T) {
	var q EventSPSC
	if err := q.Reset(make([]kui.Event, 8), make([]byte, 16)); err != nil {
		t.Fatal(err)
	}
	const total = 10000
	var clock atomic.Int64
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		produce(&q, 0, total, &clock)
	}()
	consume(t, &q, 1, total)
	wg.Wait()
}

// TestEventSPSCConcurrentCoalesce checks merging into unpublished events races with no drain: a
// producer moves a pointer right one pixel at a time, coalescing and flushing every few moves.
func TestEventSPSCConcurrentCoalesce(t *testing.T) {
	var q EventSPSC
	if err := q.Reset(make([]kui.Event, 4), nil); err != nil {
		t.Fatal(err)
	}
	const total = 20000
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for x := 1; x <= total; {
			if q.PushCoalesce(at(int64(x), events.PointerMove(float32(x), 0, 0))) {
				x++
			}
			if x%3 == 0 || x > total {
				q.Flush()
			}
		}
	}()
	dst := make([]kui.Event, 3)
	last := float32(0)
	for last < total {
		n := q.DrainEvents(dst)
		for _, e := range dst[:n] {
			if e.X <= last || float32(e.Time) != e.X {
				t.Fatalf("move to %v after %v, time %d", e.X, last, e.Time)
			}
			last = e.X
		}
	}
	wg.Wait()
}

func TestEventFan(t *testing.T) {
	var f EventFan
	if f.Reset(nil) == nil || f.Reset(make([]EventSPSC, 2)) == nil {
		t.Fatal("expected error for no sources or sources without buffers")
	}
	srcs := make([]EventSPSC, 3)
	for i := range srcs {
		if err := srcs[i].Reset(make([]kui.Event, 4), make([]byte, 16)); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Reset(srcs); err != nil {
		t.Fatal(err)
	}
	push := func(src int, tm int64) {
		if !f.Source(src).Push(kui.Event{Time: tm, Code: uint32(src)}) {
			t.Fatalf("source %d full", src)
		}
	}
	push(0, 30)
	push(0, 40)
	push(1, 10)
	push(1, 30) // Ties with source 0, which goes first.
	push(2, 20)
	dst := make([]kui.Event, 3)
	n := f.DrainEvents(dst) // A partial drain takes the earliest events.
	if n != 3 || dst[0].Time != 10 || dst[1].Time != 20 || dst[2].Time != 30 || dst[2].Code != 0 {
		t.Fatalf("first drain: %+v", dst[:n])
	}
	push(2, 5) // Written after later events were drained: still first, its time is raised.
	n = f.DrainEvents(dst)
	if n != 3 || dst[0].Time != 30 || dst[0].Code != 2 || dst[1].Time != 30 || dst[1].Code != 1 || dst[2].Time != 40 {
		t.Fatalf("second drain: %+v", dst[:n])
	}
	// Text is read from the source it was written to.
	f.Source(2).WriteTextEvent(kui.Event{Time: 50, Kind: kui.EventText}, []byte("two"))
	f.Source(1).WriteTextEvent(kui.Event{Time: 60, Kind: kui.EventText}, []byte("one"))
	if n = f.DrainEvents(dst); n != 2 {
		t.Fatalf("drained %d text events", n)
	}
	if a, b := eventText(&f, &dst[0]), eventText(&f, &dst[1]); a != "two" || b != "one" {
		t.Fatalf("texts %q %q", a, b)
	}
	if n = f.DrainEvents(dst); n != 0 {
		t.Fatalf("drained %d events from empty sources", n)
	}
}

// TestFanMerge checks the batched merge against draining one earliest event at a time.
func TestEventFanMerge(t *testing.T) {
	const k = 3
	for seed := range 300 {
		r := uint32(seed)*2654435761 + 1
		rand := func(n int) int { r ^= r << 13; r ^= r >> 17; r ^= r << 5; return int(r % uint32(n)) }
		srcs := make([]EventSPSC, k)
		for i := range srcs {
			srcs[i].Reset(make([]kui.Event, 16), nil)
		}
		var f EventFan
		f.Reset(srcs)
		var model [k][]kui.Event
		last := int64(0)
		for round := range 30 {
			for range rand(8) {
				s := rand(k)
				e := kui.Event{Time: int64(round*4 + rand(6)), Code: uint32(rand(1 << 20)), Source: uint8(s)}
				if f.Source(s).Push(e) {
					model[s] = append(model[s], e)
				}
			}
			dst := make([]kui.Event, 1+rand(7))
			n := f.DrainEvents(dst)
			for i := range dst[:n] {
				best := -1
				for s := range model {
					if len(model[s]) > 0 && (best < 0 || model[s][0].Time < model[best][0].Time) {
						best = s
					}
				}
				if best < 0 {
					t.Fatalf("seed %d round %d: drained %d events, model has %d", seed, round, n, i)
				}
				want := model[best][0]
				model[best] = model[best][1:]
				last = max(last, want.Time)
				want.Time = last
				if dst[i] != want {
					t.Fatalf("seed %d round %d event %d: want %+v, got %+v", seed, round, i, want, dst[i])
				}
			}
			if n < len(dst) && (len(model[0])+len(model[1])+len(model[2])) > 0 {
				t.Fatalf("seed %d round %d: short drain with events left", seed, round)
			}
		}
	}
}

func TestEventFanConcurrent(t *testing.T) {
	const producers, total = 4, 5000
	srcs := make([]EventSPSC, producers)
	for i := range srcs {
		srcs[i].Reset(make([]kui.Event, 8), make([]byte, 32))
	}
	var f EventFan
	if err := f.Reset(srcs); err != nil {
		t.Fatal(err)
	}
	var clock atomic.Int64 // Shared clock all producers stamp events with.
	var wg sync.WaitGroup
	for p := range producers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			produce(f.Source(p), p, total, &clock)
		}()
	}
	consume(t, &f, producers, total)
	wg.Wait()
}

// TestWriteTextEvent forwards events from one queue to another through the kui interfaces.
func TestWriteTextEvent(t *testing.T) {
	var src, dst EventSPSC
	src.Reset(make([]kui.Event, 4), make([]byte, 16))
	dst.Reset(make([]kui.Event, 4), make([]byte, 16))
	// Fields of the template are kept but its text is replaced.
	e := kui.Event{Time: 1, Kind: kui.EventText, Source: 2, KeyMods: kui.KeyModShift, Code: 99, Len: 9}
	src.Push(at(1, events.KeyDown(kui.KeyA)))
	if err := src.WriteTextEvent(e, []byte("hey")); err != nil {
		t.Fatal(err)
	}
	e.Kind = kui.EventPreedit
	if err := src.WriteTextEvent(e, nil); err != nil { // Ends composition.
		t.Fatal(err)
	}
	var r kui.EventReader = &src
	var w kui.EventWriter = &dst
	var evs [4]kui.Event
	var buf [16]byte
	n := r.DrainEvents(evs[:])
	for i := 0; i < n; {
		if !evs[i].IsString() {
			i += w.WriteEvents(evs[i:n])
			continue
		}
		text := buf[:r.EventTextWrite(buf[:], &evs[i])]
		if err := w.WriteTextEvent(evs[i], text); err != nil {
			t.Fatal(err)
		}
		i++
	}
	n = dst.DrainEvents(evs[:])
	key, txt, pre := evs[0], evs[1], evs[2]
	if n != 3 || key.Kind != kui.EventKeyDown || txt.Kind != kui.EventText || txt.Source != 2 ||
		txt.KeyMods != kui.KeyModShift || eventText(&dst, &txt) != "hey" {
		t.Fatalf("forwarded %d events: %+v", n, evs[:n])
	}
	if pre.Kind != kui.EventPreedit || pre.Code != 0 || pre.Len != 0 {
		t.Fatalf("empty preedit: %+v", pre)
	}
}

func TestEventSPSCAllocs(t *testing.T) {
	var q EventSPSC
	q.Reset(make([]kui.Event, 16), make([]byte, 64))
	src := []kui.Event{from(1, at(1, events.PointerMove(2, 3, 0))), at(2, events.KeyDown(kui.KeyA))}
	dst := make([]kui.Event, 16)
	buf := make([]byte, 32)
	var w kui.EventWriter = &q
	text := []byte("text")
	allocs := testing.AllocsPerRun(100, func() {
		w.WriteEvents(src)
		q.PushCoalesce(from(1, at(3, events.PointerMove(4, 5, 0))))
		q.PushCoalesce(from(1, at(3, events.PointerMove(5, 6, 0))))
		q.Flush()
		w.WriteTextEvent(kui.Event{Time: 3, Kind: kui.EventText}, text)
		n := q.DrainEvents(dst)
		for i := range dst[:n] {
			q.EventTextWrite(buf, &dst[i])
		}
	})
	if allocs != 0 {
		t.Fatal("allocations:", allocs)
	}
}

func TestEventFanAllocs(t *testing.T) {
	srcs := make([]EventSPSC, 3)
	for i := range srcs {
		srcs[i].Reset(make([]kui.Event, 8), make([]byte, 16))
	}
	var f EventFan
	f.Reset(srcs)
	dst := make([]kui.Event, 8)
	buf := make([]byte, 32)
	var r kui.EventReader = &f
	allocs := testing.AllocsPerRun(100, func() {
		f.Source(0).Push(from(1, at(1, events.PointerMove(2, 3, 0))))
		f.Source(2).Push(at(2, events.KeyDown(kui.KeyA)))
		f.Source(1).WriteTextEvent(kui.Event{Time: 3, Kind: kui.EventText}, []byte("text"))
		n := r.DrainEvents(dst)
		for i := range dst[:n] {
			r.EventTextWrite(buf, &dst[i])
		}
	})
	if allocs != 0 {
		t.Fatal("allocations:", allocs)
	}
}

// eventText returns the text of e read from r.
func eventText(r kui.EventReader, e *kui.Event) string {
	var buf [64]byte
	return string(buf[:r.EventTextWrite(buf[:], e)])
}

func TestEventTextWrite(t *testing.T) {
	var q EventSPSC
	q.Reset(make([]kui.Event, 4), make([]byte, 8))
	var evs [4]kui.Event
	// Move the text position so the next text wraps around the buffer.
	q.WriteTextEvent(kui.Event{Kind: kui.EventText}, []byte("12345"))
	q.DrainEvents(evs[:])
	q.DrainEvents(evs[:]) // Frees the text.
	if err := q.WriteTextEvent(kui.Event{Kind: kui.EventText}, []byte("aé€b")); err != nil {
		t.Fatal(err)
	}
	if q.DrainEvents(evs[:]) != 1 {
		t.Fatal("text event not drained")
	}
	// Text that does not fit is dropped at a rune boundary.
	var dst [8]byte
	for size, want := range []string{"", "a", "a", "aé", "aé", "aé", "aé€", "aé€b"} {
		if got := string(dst[:q.EventTextWrite(dst[:size], &evs[0])]); got != want {
			t.Errorf("%d bytes: got %q, want %q", size, got, want)
		}
	}
	// Single runes are dropped whole, as are invalid ones.
	e := kui.Event{Kind: kui.EventText, Code: '€'}
	if q.EventTextWrite(dst[:2], &e) != 0 || q.EventTextWrite(dst[:3], &e) != 3 || string(dst[:3]) != "€" {
		t.Error("single rune")
	}
	e.Code = 0xd800
	if q.EventTextWrite(dst[:], &e) != 0 {
		t.Error("invalid rune written")
	}
}

func TestEventSPSCCoalesce(t *testing.T) {
	var q EventSPSC
	q.Reset(make([]kui.Event, 4), make([]byte, 16))
	q.PushCoalesce(at(1, events.PointerMove(1, 1, 0)))
	q.PushCoalesce(at(2, events.PointerMove(5, 6, 0)))
	q.PushCoalesce(from(1, at(3, events.PointerMove(7, 7, 0)))) // Different pointer, not merged.
	q.PushCoalesce(at(4, events.Scroll(0, 0, 1, 2)))
	q.PushCoalesce(at(5, events.Scroll(0, 0, 3, 4)))
	dst := make([]kui.Event, 8)
	if n := q.DrainEvents(dst); n != 0 {
		t.Fatalf("drained %d unpublished events", n)
	}
	q.Flush()
	n := q.DrainEvents(dst)
	if n != 3 || dst[0].Time != 2 || dst[0].X != 5 || dst[0].Y != 6 || dst[1].Source != 1 || dst[2].DX != 4 || dst[2].DY != 6 {
		t.Fatalf("coalesced events: %+v", dst[:n])
	}
	// Published events are not merged into, and other writes publish pending ones in order.
	q.PushCoalesce(at(6, events.PointerMove(1, 1, 0)))
	q.Flush()
	q.PushCoalesce(at(7, events.PointerMove(2, 2, 0)))
	q.PushCoalesce(at(8, events.PointerMove(3, 3, 0)))
	if err := q.WriteTextEvent(kui.Event{Time: 9, Kind: kui.EventText}, []byte("héllo")); err != nil {
		t.Fatal(err)
	}
	// The queue of 4 holds 3 events: the move takes the last slot and the key does not fit.
	if !q.PushCoalesce(at(10, events.PointerMove(4, 4, 0))) || q.Push(at(11, events.KeyDown(kui.KeyA))) {
		t.Fatal("want the move written and the key refused")
	}
	n = q.DrainEvents(dst) // The pending move is not published.
	if n != 3 || dst[0].X != 1 || dst[1].X != 3 || dst[2].Kind != kui.EventText || eventText(&q, &dst[2]) != "héllo" {
		t.Fatalf("events: %+v", dst[:n])
	}
	q.Push(at(12, events.KeyDown(kui.KeyA))) // Publishes the pending move first.
	if n = q.DrainEvents(dst); n != 2 || dst[0].X != 4 || dst[1].Kind != kui.EventKeyDown {
		t.Fatalf("pending event not published in order: %+v", dst[:n])
	}
}
