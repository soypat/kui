package kuiscene

import (
	"fmt"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/soypat/kui"
	"github.com/soypat/kui/gesture"
	"github.com/soypat/kui/kui2go"
)

const ms = int64(1e6)

// events builds the events of tests, whose times, sources and modifiers are set with at, from
// and withMods so scripted input replays deterministically.
var events kui.EventBuilder

func init() {
	var t atomic.Int64
	events.Configure(kui.EventBuilderConfig{
		Source: 0,
		Nanotime: func() int64 {
			return t.Add(1)
		},
	})
}

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

// withMods returns e with key modifiers m.
func withMods(m kui.KeyMods, e kui.Event) kui.Event {
	e.KeyMods = m
	return e
}

type fixture struct {
	k                         *Scene
	eng                       *kui2go.LayoutBasic
	button, other, scroll, fl kui.Handle
	items                     [10]kui.Handle
	kind                      Kind
	reduced                   kindList // Events seen by the test kind's Reduce.
}

func newFixture(t testing.TB) *fixture {
	f := &fixture{k: new(Scene), eng: new(kui2go.LayoutBasic)}
	err := f.eng.Configure(kui2go.LayoutBasicConfig{MaxNodes: 64, Measurer: kui2go.TextMono{CharWidth: 8, LineHeight: 16}})
	if err != nil {
		t.Fatal(err)
	}
	mem := NewMemory(MemoryConfig{MaxNodes: 63, TextBytes: 256, MaxEvents: 64})
	err = f.k.Configure(Config{
		Memory:    mem,
		Engine:    f.eng,
		Viewport:  kui.Size{W: 300, H: 200},
		RootStyle: kui.LayoutStyle{Direction: kui.TopToBottom, Width: kui.Grow(0, 0), Height: kui.Grow(0, 0), Padding: kui.InsetsAll(10), Gap: 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	f.reduced = make([]kui.EventKind, 0, 256)
	f.kind, err = f.k.RegisterKind(KindSpec{
		Sense:  sensePress | gesture.SenseHover,
		Cursor: kui.CursorPointer,
		Reduce: func(k *Scene, n kui.Handle, e *WidgetEvent) {
			f.reduced = append(f.reduced, e.Kind)
			switch e.Kind {
			case gesture.Press:
				k.SetState(n, 1)
			case gesture.Release:
				k.SetState(n, 0)
			case gesture.Tap:
				k.Emit(WidgetEvent{Node: n, Kind: EventActivate})
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	b := f.k.Builder()
	b.SetPanic(true)
	row := b.Box(f.k.Root(), &kui.LayoutStyle{Width: kui.Grow(0, 0), Height: kui.Fixed(30), Gap: 10}, &Visual{})
	style := kui.LayoutStyle{Width: kui.Fixed(80), Height: kui.Grow(0, 0)}
	vis := Visual{Background: kui.RGB(1, 2, 3), Text: kui.Style{Size: 16, Color: kui.RGB(255, 255, 255)}}
	f.button = b.Node(row, f.kind, &style, &vis)
	f.k.SetText(f.button, "OK")
	f.other = b.Node(row, f.kind, &style, &vis)
	b.SetFlags(f.button, FlagFocusable)
	b.SetFlags(f.other, FlagFocusable)
	// Scroll container with 10 items of 20px in 50px.
	f.scroll = b.Box(f.k.Root(), &kui.LayoutStyle{Direction: kui.TopToBottom, Width: kui.Fixed(100), Height: kui.Fixed(50), ClipY: true}, &Visual{})
	b.SetFlags(f.scroll, FlagScroll)
	for i := range f.items {
		f.items[i] = b.Text(f.scroll, &kui.LayoutStyle{Width: kui.Grow(0, 0), Height: kui.Fixed(20)}, &vis, "item")
	}
	// Floating node above the other button.
	f.fl = b.Node(f.k.Root(), f.kind, &kui.LayoutStyle{
		Width: kui.Fixed(20), Height: kui.Fixed(20),
		Floating: kui.Floating{AttachTo: kui.AttachNode, Node: f.other, Element: kui.AttachLeftTop, Parent: kui.AttachLeftTop, Z: 1},
	}, &vis)
	return f
}

func (f *fixture) step(t testing.TB, now int64, evs ...kui.Event) []WidgetEvent {
	t.Helper()
	if err := f.k.Step(now, evs, nil); err != nil {
		t.Fatal(err)
	}
	return f.k.Events()
}

func kinds(evs []WidgetEvent) kindList {
	var out kindList
	for _, e := range evs {
		out = append(out, e.Kind)
	}
	return out
}

// kindList formats event kinds by name.
type kindList []kui.EventKind

func (l kindList) String() string {
	var b []byte
	for i, k := range l {
		if i > 0 {
			b = append(b, ' ')
		}
		b = append(b, KindString(k)...)
	}
	return "[" + string(b) + "]"
}

func center(r kui.Rect) (float32, float32) { return r.X + r.W/2, r.Y + r.H/2 }

func TestLayoutAndHit(t *testing.T) {
	f := newFixture(t)
	if err := f.k.Layout(); err != nil {
		t.Fatal(err)
	}
	if b := f.k.Box(f.button); b != (kui.Rect{X: 10, Y: 10, W: 80, H: 30}) {
		t.Fatalf("button box %+v", b)
	}
	x, y := center(f.k.Box(f.button))
	if h := f.k.HitTest(kui.Point{X: x, Y: y}, sensePress); h != f.button {
		t.Fatalf("hit test: want button, got %x", h)
	}
	// The floating node covers the top left of the other button and is on top.
	ob := f.k.Box(f.other)
	if h := f.k.HitTest(kui.Point{X: ob.X + 5, Y: ob.Y + 5}, sensePress); h != f.fl {
		t.Fatalf("hit test: want floating node, got %x", h)
	}
	if h := f.k.HitTest(kui.Point{X: ob.X + 50, Y: ob.Y + 5}, sensePress); h != f.other {
		t.Fatalf("hit test: want other, got %x", h)
	}
	// Clipped scroll items are not hit outside the scroll container.
	if h := f.k.HitTest(kui.Point{X: 20, Y: 150}, ^gesture.Sense(0)); h != 0 {
		t.Fatalf("hit test below scroll container: got %x", h)
	}
}

func TestTapDoubleTapAndHover(t *testing.T) {
	f := newFixture(t)
	f.k.Layout()
	x, y := center(f.k.Box(f.button))
	got := kinds(f.step(t, 1*ms, at(1*ms, events.PointerMove(x, y, 0))))
	if !slices.Equal(got, []kui.EventKind{gesture.HoverIn}) {
		t.Fatalf("hover: %v", got)
	}
	if r, _ := f.k.Requests(); len(r) != 1 || r[0].Cursor != kui.CursorPointer {
		t.Fatalf("cursor request: %+v", r)
	}
	got = kinds(f.step(t, 2*ms, at(2*ms, events.PointerDown(x, y, kui.ButtonPrimary))))
	if !slices.Equal(got, []kui.EventKind{gesture.Press, EventFocusIn}) || f.k.State(f.button) != 1 || f.k.Focus() != f.button {
		t.Fatalf("press: %v state=%d", got, f.k.State(f.button))
	}
	got = kinds(f.step(t, 3*ms, at(3*ms, events.PointerUp(x+1, y, kui.ButtonPrimary))))
	if !slices.Equal(got, []kui.EventKind{gesture.Tap, gesture.Release, EventActivate}) || f.k.State(f.button) != 0 {
		t.Fatalf("release: %v", got)
	}
	// Second tap within the double tap time.
	got = kinds(f.step(t, 4*ms,
		at(4*ms, events.PointerDown(x, y, kui.ButtonPrimary)),
		at(5*ms, events.PointerUp(x, y, kui.ButtonPrimary))))
	if !slices.Equal(got, []kui.EventKind{gesture.Press, gesture.DoubleTap, gesture.Release}) {
		t.Fatalf("double tap: %v", got)
	}
	// Secondary tap.
	got = kinds(f.step(t, 900*ms,
		at(900*ms, events.PointerDown(x, y, kui.ButtonSecondary)),
		at(901*ms, events.PointerUp(x, y, kui.ButtonSecondary))))
	if !slices.Equal(got, []kui.EventKind{gesture.Press, gesture.SecondaryTap, gesture.Release}) {
		t.Fatalf("secondary tap: %v", got)
	}
	got = kinds(f.step(t, 902*ms, kui.Event{Kind: kui.EventPointerLeave}))
	if !slices.Equal(got, []kui.EventKind{gesture.HoverOut}) {
		t.Fatalf("leave: %v", got)
	}
}

func TestDragAndLongPress(t *testing.T) {
	f := newFixture(t)
	f.k.Layout()
	x, y := center(f.k.Box(f.button))
	evs := f.step(t, 1*ms,
		at(1*ms, events.PointerDown(x, y, kui.ButtonPrimary)),
		at(2*ms, events.PointerMove(x+2, y, 0)),  // Within slop.
		at(3*ms, events.PointerMove(x+10, y, 0)), // Drag start.
		at(4*ms, events.PointerMove(x+15, y+1, 0)),
		at(5*ms, events.PointerUp(x+15, y+1, kui.ButtonPrimary)))
	got := kinds(evs)
	want := kindList{gesture.Press, EventFocusIn, gesture.HoverIn, gesture.DragStart, gesture.HoverMove, gesture.Drag, gesture.HoverMove, gesture.DragEnd, gesture.Release}
	if !slices.Equal(got, want) {
		t.Fatalf("drag:\nwant %v\ngot  %v", want, got)
	}
	if evs[5].DX != 5 || evs[5].DY != 1 {
		t.Fatalf("drag delta: %+v", evs[5])
	}
	// Long press: deadline reported while pressed, fires on a Step after it.
	f.step(t, 1000*ms, at(1000*ms, events.PointerDown(x, y, kui.ButtonPrimary)))
	deadline, ok := f.k.NextDeadline()
	if !ok || deadline != 1500*ms {
		t.Fatalf("deadline: %d %v", deadline, ok)
	}
	if got := kinds(f.step(t, 1499*ms)); len(got) != 0 {
		t.Fatalf("early long press: %v", got)
	}
	if got := kinds(f.step(t, 1500*ms)); !slices.Equal(got, []kui.EventKind{gesture.LongPress}) {
		t.Fatalf("long press: %v", got)
	}
	if got := kinds(f.step(t, 1600*ms, at(1600*ms, events.PointerUp(x, y, kui.ButtonPrimary)))); !slices.Equal(got, []kui.EventKind{gesture.Release}) {
		t.Fatalf("release after long press: %v", got)
	}
}

func TestFocusAndKeys(t *testing.T) {
	f := newFixture(t)
	tab := at(1, events.KeyDown(kui.KeyTab))
	shiftTab := at(1, withMods(kui.KeyModShift, events.KeyDown(kui.KeyTab)))
	f.step(t, 1, tab)
	if f.k.Focus() != f.button {
		t.Fatal("tab should focus first focusable")
	}
	f.step(t, 2, tab)
	if f.k.Focus() != f.other {
		t.Fatal("tab should focus second focusable")
	}
	f.step(t, 3, tab)
	if f.k.Focus() != f.button {
		t.Fatal("tab should wrap around")
	}
	f.step(t, 4, shiftTab)
	if f.k.Focus() != f.other {
		t.Fatal("shift tab should wrap backwards")
	}
	evs := f.step(t, 5, at(5, events.KeyDown(kui.KeyEnter)), at(6, events.Rune('x')))
	if len(evs) != 2 || evs[0].Kind != kui.EventKeyDown || evs[0].Node != f.other || evs[1].Kind != kui.EventText || evs[1].Code != 'x' {
		t.Fatalf("key events: %+v", evs)
	}
	f.k.SetFlags(f.other, FlagDisabled|FlagFocusable)
	if f.k.Focus() != 0 {
		t.Fatal("disabling the focused node should clear focus")
	}
}

func TestScroll(t *testing.T) {
	f := newFixture(t)
	f.k.Layout()
	box := f.k.Box(f.scroll)
	x, y := center(box)
	first := f.k.Box(f.items[0])
	f.step(t, 1, at(1, events.Scroll(x, y, 0, 30)))
	if s := f.k.Style(f.scroll).Scroll; s.Y != -30 {
		t.Fatalf("scroll offset %+v", s)
	}
	if moved := f.k.Box(f.items[0]); moved.Y != first.Y-30 {
		t.Fatalf("item did not move: %+v -> %+v", first, moved)
	}
	// Clamped to content: 10 items * 20px - 50px = 150px overflow.
	f.step(t, 2, at(2, events.Scroll(x, y, 0, 1000)))
	if s := f.k.Style(f.scroll).Scroll; s.Y != -150 {
		t.Fatalf("scroll not clamped: %+v", s)
	}
	f.step(t, 3, at(3, events.Scroll(x, y, 0, -1000)))
	if s := f.k.Style(f.scroll).Scroll; s.Y != 0 {
		t.Fatalf("scroll not clamped at top: %+v", s)
	}
}

func TestRemoveAndText(t *testing.T) {
	f := newFixture(t)
	if err := f.k.Remove(f.scroll); err != nil {
		t.Fatal(err)
	}
	if f.k.Valid(f.items[3]) || f.k.SetText(f.items[3], "x") != kui.ErrStaleHandle {
		t.Fatal("removed descendants should be stale")
	}
	if err := f.k.Remove(f.k.Root()); err == nil {
		t.Fatal("removing the root should fail")
	}
	// Reuse freed slots; handles differ by generation.
	h := f.k.Builder().Box(f.k.Root(), &kui.LayoutStyle{}, &Visual{})
	if h.Index() == 0 || h == f.items[0] {
		t.Fatal("bad reused handle")
	}
	// Fill the 256 byte arena forcing compaction.
	for i := range 40 {
		if err := f.k.SetText(f.button, fmt.Sprintf("text number %d", i)); err != nil {
			t.Fatalf("set text %d: %v", i, err)
		}
	}
	if f.k.Text(f.button) != "text number 39" || f.k.Text(f.other) != "" {
		t.Fatalf("text after compaction: %q", f.k.Text(f.button))
	}
	long := string(make([]byte, 300))
	if err := f.k.SetText(f.other, long); err != kui.ErrShortBuffer || f.k.TextArenaNeed() < 300 {
		t.Fatalf("want ErrShortBuffer, got %v need=%d", err, f.k.TextArenaNeed())
	}
}

func TestPaint(t *testing.T) {
	f := newFixture(t)
	var l kui.DrawList
	l.Reset(make([]kui.DrawCmd, 0, 64), make([]byte, 0, 256))
	if err := f.k.Paint(&l); err != nil {
		t.Fatal(err)
	}
	var ops []kui.DrawOp
	var lastZ int16
	for _, c := range l.Cmds() {
		ops = append(ops, c.Op)
		if c.Z < lastZ {
			t.Fatal("commands not in z order")
		}
		lastZ = c.Z
	}
	// Button and other (rect+text each), scroll clip with 2.5 visible items, floating node last.
	if ops[0] != kui.DrawRect || ops[1] != kui.DrawText || string(l.TextOf(&l.Cmds()[1])) != "OK" {
		t.Fatalf("unexpected first commands %v", ops)
	}
	if last := l.Cmds()[len(ops)-1]; last.Node != f.fl || last.Z != 1 {
		t.Fatalf("floating node should paint last: %+v", last)
	}
	if f.k.NeedsPaint() {
		t.Fatal("NeedsPaint after Paint")
	}
	var clipDepth int
	for _, op := range ops {
		switch op {
		case kui.DrawClipPush:
			clipDepth++
		case kui.DrawClipPop:
			clipDepth--
		}
	}
	if clipDepth != 0 {
		t.Fatal("unbalanced clip commands")
	}
}

func TestStepPaintAllocs(t *testing.T) {
	f := newFixture(t)
	f.k.Layout()
	var l kui.DrawList
	l.Reset(make([]kui.DrawCmd, 0, 64), make([]byte, 0, 256))
	x, y := center(f.k.Box(f.button))
	sx, sy := center(f.k.Box(f.scroll))
	evs := []kui.Event{
		at(1, events.PointerMove(x, y, 0)),
		at(2, events.PointerDown(x, y, kui.ButtonPrimary)),
		at(3, events.PointerMove(x+10, y, 0)),
		at(4, events.PointerUp(x+10, y, kui.ButtonPrimary)),
		at(5, events.Scroll(sx, sy, 0, 5)),
		at(6, events.KeyDown(kui.KeyTab)),
	}
	now := int64(0)
	frame := func() {
		now += 1000 * ms
		for i := range evs {
			evs[i].Time = now
		}
		f.k.Step(now, evs, nil)
		f.k.SetText(f.button, "OK")
		l.Clear()
		f.k.Paint(&l)
		f.reduced = f.reduced[:0]
	}
	frame()
	if allocs := testing.AllocsPerRun(100, frame); allocs != 0 {
		t.Fatal("allocations:", allocs)
	}
}

// script runs a deterministic sequence of events and returns all widget events and paint commands.
func script(t testing.TB, data []byte) ([]WidgetEvent, []kui.DrawCmd) {
	f := newFixture(t)
	modal, _ := f.addModal(t)
	var l kui.DrawList
	l.Reset(make([]kui.DrawCmd, 0, 256), make([]byte, 0, 1024))
	var all []WidgetEvent
	var cmds []kui.DrawCmd
	now := int64(0)
	for i := 0; i+4 <= len(data); i += 4 {
		b := data[i : i+4]
		now += int64(b[3]) * 10 * ms
		x, y := float32(b[1])*300/255, float32(b[2])*200/255
		var ev kui.Event
		switch b[0] % 8 {
		case 0:
			ev = at(now, from(b[3]%2, events.PointerMove(x, y, 0)))
		case 1:
			ev = at(now, from(b[3]%2, events.PointerDown(x, y, kui.PointerButton(b[1]%3))))
		case 2:
			ev = at(now, from(b[3]%2, events.PointerUp(x, y, kui.PointerButton(b[1]%3))))
		case 3:
			ev = at(now, events.Scroll(x, y, 0, float32(int8(b[1]))))
		case 4:
			ev = at(now, withMods(kui.KeyMods(b[2]), events.KeyDown(kui.Key(b[1]%0x53))))
		case 5:
			ev = at(now, events.Rune(rune(b[1])))
		case 6:
			ev = kui.Event{Time: now, Kind: kui.EventPointerCancel, Source: b[3] % 2}
		case 7:
			// Mutate the tree.
			switch b[1] % 6 {
			case 0:
				f.k.SetText(f.items[b[2]%10], string(rune('a'+b[3]%26)))
			case 1:
				f.k.Remove(f.items[b[2]%10])
			case 2:
				f.k.SetFlags(f.button, Flags(b[2])&(FlagHidden|FlagDisabled|FlagFocusable))
			case 3:
				f.k.SetViewport(kui.Size{W: 100 + x, H: 100 + y})
			case 4:
				f.k.SetFlags(modal, FlagModal|Flags(b[2])&FlagHidden)
			case 5:
				item := f.items[b[2]%10]
				n := len(f.k.Text(item))
				f.k.EditText(item, min(int(b[3]%4), n), n, "xy")
			}
		}
		if err := f.k.Step(now, []kui.Event{ev}, nil); err != nil {
			t.Fatal(err)
		}
		all = append(all, f.k.Events()...)
		l.Clear()
		if err := f.k.Paint(&l); err != nil {
			t.Fatal(err)
		}
		cmds = append(cmds, l.Cmds()...)
		checkInvariants(t, f, &l)
	}
	return all, cmds
}

func checkInvariants(t testing.TB, f *fixture, l *kui.DrawList) {
	t.Helper()
	depth := 0
	for _, c := range l.Cmds() {
		switch c.Op {
		case kui.DrawClipPush:
			depth++
		case kui.DrawClipPop:
			depth--
		}
		if depth < 0 {
			t.Fatal("clip pop without push")
		}
		if !c.Node.IsZero() && !f.k.Valid(c.Node) {
			t.Fatalf("paint command references invalid node %x", c.Node)
		}
	}
	if depth != 0 {
		t.Fatal("unbalanced clips")
	}
	for _, e := range f.k.Events() {
		if !f.k.Valid(e.Node) && e.Kind != EventFocusOut && e.Kind != gesture.HoverOut {
			t.Fatalf("event %v references invalid node", e.Kind)
		}
	}
	if fo := f.k.Focus(); fo != 0 && f.k.Flags(fo)&FlagFocusable == 0 {
		t.Fatal("focused node is not focusable")
	} else if i, err := f.k.resolve(fo); err == nil && !f.k.inModal(i) {
		t.Fatal("focused node is outside the active modal node")
	}
}

func TestDeterministicReplay(t *testing.T) {
	data := make([]byte, 4*500)
	seed := uint32(12345)
	for i := range data {
		seed = seed*1664525 + 1013904223
		data[i] = byte(seed >> 24)
	}
	ev1, cmd1 := script(t, data)
	ev2, cmd2 := script(t, data)
	if !slices.Equal(ev1, ev2) || !slices.Equal(cmd1, cmd2) {
		t.Fatal("scene is not deterministic")
	}
	if len(ev1) == 0 || len(cmd1) == 0 {
		t.Fatal("script produced no output")
	}
}

func FuzzSceneStep(f *testing.F) {
	f.Add([]byte{1, 30, 15, 1, 2, 30, 15, 1})
	f.Add([]byte{3, 20, 120, 5, 7, 1, 3, 1, 4, 0x2B, 0, 1})
	f.Fuzz(func(t *testing.T, data []byte) {
		script(t, data)
	})
}

func TestKindString(t *testing.T) {
	for k := EventFocusIn; k < EventExt0; k++ {
		if s := KindString(k); s == "" || s == k.String() {
			t.Errorf("kind %d has no name", k)
		}
	}
	if EventFocusIn != gesture.EventExt0 || EventExt0 != EventDismiss+1 {
		t.Error("scene kinds overlap others")
	}
	if KindString(gesture.Tap) != gesture.KindString(gesture.Tap) || KindString(EventExt0) != EventExt0.StringConst() {
		t.Error("KindString of a kind not of the scene")
	}
	if s := (kindList{gesture.Press, EventFocusIn, kui.EventKeyDown}).String(); s != "[press focus in keydown]" {
		t.Error("kind list:", s)
	}
	e := WidgetEvent{Kind: gesture.DragStart}
	if g, ok := e.Gesture(); !ok || g.Sense() != gesture.SenseDrag {
		t.Error("drag start not a drag gesture")
	}
	e.Kind = EventActivate
	if _, ok := e.Gesture(); ok {
		t.Error("activate taken for a gesture")
	}
}
