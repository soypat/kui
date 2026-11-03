package kuiscene

import (
	"slices"
	"strings"
	"testing"

	"github.com/soypat/kui"
	"github.com/soypat/kui/gesture"
	"github.com/soypat/kui/kui2go"
)

func TestTimers(t *testing.T) {
	f := newFixture(t)
	if err := f.k.SetTimer(f.button, 7, 100*ms); err != nil {
		t.Fatal(err)
	}
	if err := f.k.SetTimer(f.other, 1, 50*ms); err != nil {
		t.Fatal(err)
	}
	f.k.CancelTimer(f.other, 1)
	if d, ok := f.k.NextDeadline(); !ok || d != 100*ms {
		t.Fatalf("deadline: %d %v", d, ok)
	}
	if evs := f.step(t, 99*ms); len(evs) != 0 {
		t.Fatalf("early timer: %v", kinds(evs))
	}
	evs := f.step(t, 100*ms)
	if len(evs) != 1 || evs[0].Kind != EventTimer || evs[0].Code != 7 || evs[0].Node != f.button {
		t.Fatalf("timer: %+v", evs)
	}
	if _, ok := f.k.NextDeadline(); ok {
		t.Fatal("fired timer still pending")
	}
	// Setting a pending timer again replaces it.
	f.k.SetTimer(f.button, 7, 300*ms)
	f.k.SetTimer(f.button, 7, 200*ms)
	if d, _ := f.k.NextDeadline(); d != 200*ms {
		t.Fatalf("replaced deadline: %d", d)
	}
	// Capacity, with timers of removed nodes reclaimed.
	for code := range uint32(7) {
		if err := f.k.SetTimer(f.items[code], code, 1e9); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.k.SetTimer(f.other, 0, 1e9); err != kui.ErrCapacity {
		t.Fatalf("want ErrCapacity, got %v", err)
	}
	f.k.Remove(f.items[0])
	if err := f.k.SetTimer(f.other, 0, 1e9); err != nil {
		t.Fatalf("stale timer not reclaimed: %v", err)
	}
}

// addModal adds a hidden modal node floating over the root with a focusable child.
func (f *fixture) addModal(t testing.TB) (modal, child kui.Handle) {
	b := f.k.Builder()
	modal = b.Node(f.k.Root(), f.kind, &kui.LayoutStyle{
		Width: kui.Fixed(100), Height: kui.Fixed(50), Padding: kui.InsetsAll(10),
		Floating: kui.Floating{AttachTo: kui.AttachRoot, Offset: kui.Point{X: 150, Y: 100}, Z: 5},
	}, &Visual{})
	child = b.Node(modal, f.kind, &kui.LayoutStyle{Width: kui.Grow(0, 0), Height: kui.Grow(0, 0)}, &Visual{})
	b.SetFlags(modal, FlagModal|FlagHidden)
	b.SetFlags(child, FlagFocusable)
	if err := b.Err(); err != nil {
		t.Fatal(err)
	}
	return modal, child
}

func TestModal(t *testing.T) {
	f := newFixture(t)
	modal, child := f.addModal(t)
	f.k.SetFocus(f.button)
	f.step(t, 1)
	// Showing the modal node moves focus out of the rest of the UI.
	f.k.SetFlags(modal, FlagModal)
	f.k.Layout()
	if f.k.Focus() != 0 {
		t.Fatal("focus outside the modal node was kept")
	}
	x, y := center(f.k.Box(f.button))
	evs := f.step(t, 2, at(2, events.PointerDown(x, y, kui.ButtonPrimary)), at(3, events.PointerUp(x, y, kui.ButtonPrimary)))
	if got := kinds(evs); !slices.Equal(got, []kui.EventKind{EventFocusOut, EventDismiss}) || evs[1].Node != modal {
		t.Fatalf("press outside modal: %v", got)
	}
	// Focus traversal stays inside the modal node.
	for i := range 3 {
		f.step(t, int64(10+i), at(1, events.KeyDown(kui.KeyTab)))
		if f.k.Focus() != child {
			t.Fatalf("tab %d left the modal node: %x", i, f.k.Focus())
		}
	}
	evs = f.step(t, 20, at(20, events.KeyDown(kui.KeyEscape)))
	if len(evs) != 1 || evs[0].Kind != EventDismiss || evs[0].Node != modal {
		t.Fatalf("escape: %+v", evs)
	}
	cx, cy := center(f.k.Box(child))
	evs = f.step(t, 30, at(30, events.PointerDown(cx, cy, kui.ButtonPrimary)))
	if len(evs) != 1 || evs[0].Kind != gesture.Press || evs[0].Node != child {
		t.Fatalf("press inside modal: %+v", evs)
	}
	f.step(t, 31, at(31, events.PointerUp(cx, cy, kui.ButtonPrimary)))
	// Hiding it clears focus from inside it and restores input to the rest of the UI.
	f.k.SetFlags(modal, FlagModal|FlagHidden)
	f.k.Layout()
	if f.k.Focus() != 0 {
		t.Fatal("focus inside hidden modal node was kept")
	}
	evs = f.step(t, 40, at(40, events.PointerDown(x, y, kui.ButtonPrimary)))
	if got := kinds(evs); !slices.Equal(got, []kui.EventKind{EventFocusOut, gesture.Press, EventFocusIn}) {
		t.Fatalf("press after modal hidden: %v", got)
	}
}

func TestDragPan(t *testing.T) {
	f := newFixture(t)
	tapKind, err := f.k.RegisterKind(KindSpec{Sense: gesture.SensePress})
	if err != nil {
		t.Fatal(err)
	}
	f.k.Remove(f.items[0])
	tappable := f.k.Builder().Node(f.scroll, tapKind, &kui.LayoutStyle{Width: kui.Grow(0, 0), Height: kui.Fixed(20)}, &Visual{})
	f.k.Layout()
	// Dragging non interactive content pans its scroll container, emitting nothing.
	x, _ := center(f.k.Box(f.scroll))
	evs := f.step(t, 1,
		at(1, from(1, events.PointerDown(x, 95, kui.ButtonPrimary))),
		at(2, from(1, events.PointerMove(x, 85, 0))),
		at(3, from(1, events.PointerMove(x, 65, 0))),
		at(4, from(1, events.PointerUp(x, 65, kui.ButtonPrimary))))
	if len(evs) != 0 || f.k.Style(f.scroll).Scroll.Y != -30 {
		t.Fatalf("pan: scroll=%v events=%v", f.k.Style(f.scroll).Scroll, kinds(evs))
	}
	// A press on a node that does not sense drags is released when its container takes over.
	f.k.ScrollBy(f.scroll, 0, 1000)
	f.k.Layout()
	x, y := center(f.k.Box(tappable))
	evs = f.step(t, 10,
		at(10, from(1, events.PointerDown(x, y, kui.ButtonPrimary))),
		at(11, from(1, events.PointerMove(x, y+20, 0))),
		at(12, from(1, events.PointerUp(x, y+20, kui.ButtonPrimary))))
	if got := kinds(evs); !slices.Equal(got, []kui.EventKind{gesture.Press, gesture.Release}) || f.k.Style(f.scroll).Scroll.Y != -130 {
		t.Fatalf("stolen drag: scroll=%v events=%v", f.k.Style(f.scroll).Scroll, got)
	}
	// ScrollBy clamps to the content.
	if !f.k.ScrollBy(f.scroll, 0, 1000) || f.k.Style(f.scroll).Scroll.Y != -150 {
		t.Fatalf("ScrollBy: %v", f.k.Style(f.scroll).Scroll)
	}
	if f.k.ScrollBy(f.scroll, 0, 10) {
		t.Fatal("ScrollBy past the end moved")
	}
	if sz := f.k.ContentSize(f.scroll); sz.H != 200 {
		t.Fatalf("content size %+v", sz)
	}
}

func TestRequests(t *testing.T) {
	f := newFixture(t)
	// Requests made outside Step are delivered with the next Step.
	if err := f.k.Request(kui.Request{Kind: kui.RequestClipboardWrite, Node: f.button}, "copied"); err != nil {
		t.Fatal(err)
	}
	if reqs, _ := f.k.Requests(); len(reqs) != 0 {
		t.Fatal("request delivered before Step")
	}
	f.step(t, 1)
	reqs, _ := f.k.Requests()
	if len(reqs) != 1 || reqs[0].Kind != kui.RequestClipboardWrite || f.k.RequestText(&reqs[0]) != "copied" {
		t.Fatalf("requests: %+v", reqs)
	}
	f.step(t, 2)
	if reqs, _ := f.k.Requests(); len(reqs) != 0 {
		t.Fatal("request delivered twice")
	}
	if err := f.k.Request(kui.Request{Kind: kui.RequestClipboardWrite}, strings.Repeat("x", 300)); err != kui.ErrShortBuffer {
		t.Fatalf("want ErrShortBuffer, got %v", err)
	}
	for range 8 {
		f.k.Request(kui.Request{Kind: kui.RequestClipboardRead}, "")
	}
	if err := f.k.Request(kui.Request{Kind: kui.RequestClipboardRead}, ""); err != kui.ErrBufferFull {
		t.Fatalf("want ErrBufferFull, got %v", err)
	}
}

func TestEditText(t *testing.T) {
	f := newFixture(t)
	k := f.k
	k.SetText(f.other, "hello")
	if err := k.EditText(f.other, 1, 4, "ipp"); err != nil || k.Text(f.other) != "hippo" {
		t.Fatalf("edit: %q %v", k.Text(f.other), err)
	}
	if err := k.EditText(f.other, 3, 6, ""); err != kui.ErrOutOfRange {
		t.Fatalf("want ErrOutOfRange, got %v", err)
	}
	if err := k.ReserveText(f.other, 32); err != nil || k.TextCap(f.other) != 32 || k.Text(f.other) != "hippo" {
		t.Fatalf("reserve: cap=%d %q %v", k.TextCap(f.other), k.Text(f.other), err)
	}
	k.EditText(f.other, 5, 5, " potamus")
	k.EditText(f.other, 0, 0, "big ")
	if k.Text(f.other) != "big hippo potamus" || k.TextCap(f.other) != 32 {
		t.Fatalf("edits in place: %q cap=%d", k.Text(f.other), k.TextCap(f.other))
	}
	// Churn the arena with another node, forcing compactions; the reservation is kept.
	for i := range 50 {
		if err := k.SetText(f.button, strings.Repeat("ab", i%20)); err != nil {
			t.Fatalf("set text %d: %v", i, err)
		}
	}
	if k.Text(f.other) != "big hippo potamus" || k.TextCap(f.other) != 32 {
		t.Fatalf("after compaction: %q cap=%d", k.Text(f.other), k.TextCap(f.other))
	}
	// A failed edit leaves the text unchanged.
	if err := k.EditText(f.other, 0, 0, strings.Repeat("x", 300)); err != kui.ErrShortBuffer || k.Text(f.other) != "big hippo potamus" {
		t.Fatalf("failed edit: %q %v", k.Text(f.other), err)
	}
}

func TestPaintAfterAndHiddenFocus(t *testing.T) {
	f := newFixture(t)
	afterKind, err := f.k.RegisterKind(KindSpec{
		Paint: func(k *Scene, n kui.Handle, l *kui.DrawList) { l.Custom(n, k.Box(n), 1, 0) },
		PaintAfter: func(k *Scene, n kui.Handle, l *kui.DrawList) {
			l.Custom(n, k.Box(n), 2, 0)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	clip := f.k.Builder().Node(f.k.Root(), afterKind, &kui.LayoutStyle{Width: kui.Fixed(50), Height: kui.Fixed(20), ClipX: true}, &Visual{})
	f.k.Builder().Box(clip, &kui.LayoutStyle{Width: kui.Fixed(100), Height: kui.Fixed(10)}, &Visual{Background: kui.RGB(1, 1, 1)})
	var l kui.DrawList
	l.Reset(make([]kui.DrawCmd, 0, 64), make([]byte, 0, 256))
	if err := f.k.Paint(&l); err != nil {
		t.Fatal(err)
	}
	var ops []kui.DrawOp
	for _, c := range l.Cmds() {
		if c.Node == clip || f.k.Parent(c.Node) == clip {
			ops = append(ops, c.Op)
		}
	}
	want := []kui.DrawOp{kui.DrawClipPush, kui.DrawCustom, kui.DrawRect, kui.DrawClipPop, kui.DrawCustom}
	if !slices.Equal(ops, want) {
		t.Fatalf("paint order:\nwant %v\ngot  %v", want, ops)
	}
	// Hiding an ancestor of the focused node clears focus.
	f.k.SetFocus(f.button)
	f.k.SetFlags(f.k.Parent(f.button), FlagHidden)
	f.k.Layout()
	if f.k.Focus() != 0 {
		t.Fatal("focus under hidden ancestor was kept")
	}
}

// TestSetTextAliasing copies one node's text to another while the arena compacts, which
// moves the source text.
func TestSetTextAliasing(t *testing.T) {
	var eng kui2go.LayoutBasic
	if err := eng.Configure(kui2go.LayoutBasicConfig{MaxNodes: 8, Measurer: kui2go.TextMono{CharWidth: 8, LineHeight: 16}}); err != nil {
		t.Fatal(err)
	}
	var k Scene
	if err := k.Configure(Config{Memory: NewMemory(MemoryConfig{MaxNodes: 7, TextBytes: 16, MaxEvents: 8}), Engine: &eng}); err != nil {
		t.Fatal(err)
	}
	b := k.Builder()
	b.SetPanic(true)
	hole := b.Text(k.Root(), &kui.LayoutStyle{}, &Visual{}, "xxxxx") // Arena [0, 5).
	src := b.Text(k.Root(), &kui.LayoutStyle{}, &Visual{}, "hello")  // [5, 10).
	dst := b.Text(k.Root(), &kui.LayoutStyle{}, &Visual{}, "ab")     // [10, 12).
	k.Remove(hole)
	// dst needs 5 bytes and 4 are free: compaction moves src to [0, 5), dst to [5, 7), then
	// dst moves to [7, 12) over where src was.
	if err := k.SetText(dst, k.Text(src)); err != nil {
		t.Fatal(err)
	}
	if k.Text(dst) != "hello" || k.Text(src) != "hello" {
		t.Fatalf("got %q and %q", k.Text(src), k.Text(dst))
	}
}
