package kuicogent

import (
	"image"
	"testing"

	"cogentcore.org/core/events"
	"cogentcore.org/core/events/key"
	"cogentcore.org/core/math32"
	"github.com/soypat/kui"
	"github.com/soypat/kui/kui2go"
)

// recorder is an EventWriter keeping what is written.
type recorder struct{ evs []kui.Event }

func (r *recorder) WriteEvents(src []kui.Event) int { r.evs = append(r.evs, src...); return len(src) }

func (r *recorder) WriteTextEvent(e kui.Event, text []byte) error {
	r.evs = append(r.evs, e)
	return nil
}

func newInput(t *testing.T, w kui.EventWriter, dots image.Point, scale float32) *Input {
	t.Helper()
	var now int64
	in := new(Input)
	err := in.configure(w, func() int64 { now++; return now }, func() (image.Point, float32) { return dots, scale })
	if err != nil {
		t.Fatal(err)
	}
	return in
}

func mods(ms ...key.Modifiers) key.Modifiers {
	var m key.Modifiers
	for _, x := range ms {
		m.SetFlag(true, x)
	}
	return m
}

func TestInput(t *testing.T) {
	r := &recorder{}
	in := newInput(t, r, image.Pt(400, 200), 2)
	in.Handle(events.NewMouse(events.MouseDown, events.Left, image.Pt(20, 40), mods(key.Shift)))
	in.Handle(events.NewMouse(events.MouseUp, events.Right, image.Pt(20, 40), 0))
	in.Handle(events.NewMouseMove(events.NoButton, image.Pt(30, 50), image.Pt(20, 40), 0))
	in.Handle(events.NewMouse(events.MouseDown, events.NoButton, image.Pt(1, 1), 0)) // Dropped.
	in.Handle(events.NewScroll(image.Pt(10, 10), math32.Vec2(4, 40), 0))
	in.Handle(events.NewKey(events.KeyDown, 'a', key.CodeA, mods(key.Control, key.Meta)))
	in.Handle(events.NewKey(events.KeyUp, 0, key.CodeKeypadEnter, 0))
	in.Handle(events.NewKey(events.KeyDown, '5', key.CodeKeypad5, 0)) // No kui key: dropped.
	in.Handle(events.NewKey(events.KeyChord, 'é', key.CodeUnknown, mods(key.Shift)))
	in.Handle(events.NewKey(events.KeyChord, 'a', key.CodeA, mods(key.Control)))       // Repeats a press.
	in.Handle(events.NewKey(events.KeyChord, 'c', key.CodeUnknown, mods(key.Control))) // Shortcut.
	in.Handle(events.NewWindow(events.WinFocusLost))
	in.Handle(events.NewWindow(events.WinFocus))
	in.Handle(events.NewWindowResize())
	in.Handle(events.NewWindowPaint()) // Ignored.

	want := []kui.EventKind{kui.EventPointerDown, kui.EventPointerUp, kui.EventPointerMove, kui.EventScroll,
		kui.EventKeyDown, kui.EventKeyUp, kui.EventText, kui.EventBlur, kui.EventFocus, kui.EventResize}
	if len(r.evs) != len(want) {
		t.Fatalf("%d events, want %d: %+v", len(r.evs), len(want), r.evs)
	}
	for i, k := range want {
		if r.evs[i].Kind != k {
			t.Fatalf("event %d is %v, want %v", i, r.evs[i].Kind, k)
		}
	}
	e := r.evs
	if e[0].X != 10 || e[0].Y != 20 || e[0].Button != kui.ButtonPrimary || e[0].KeyMods != kui.KeyModShift {
		t.Errorf("press %+v", e[0])
	}
	if e[1].Button != kui.ButtonSecondary || e[1].KeyMods != 0 {
		t.Errorf("release %+v", e[1])
	}
	if e[2].X != 15 || e[2].Y != 25 {
		t.Errorf("move %+v", e[2])
	}
	if e[3].DX != 2 || e[3].DY != 20 || e[3].X != 5 {
		t.Errorf("scroll %+v", e[3])
	}
	if e[4].Key() != kui.KeyA || e[4].KeyMods != kui.KeyModCtrl|kui.KeyModSuper {
		t.Errorf("key %v mods %v", e[4].Key(), e[4].KeyMods)
	}
	if e[5].Key() != kui.KeyEnter {
		t.Errorf("keypad enter %v", e[5].Key())
	}
	if e[6].Rune() != 'é' || e[6].IsString() {
		t.Errorf("text %q", e[6].Rune())
	}
	if w, h, s := e[9].Resize(); w != 200 || h != 100 || s != 2 {
		t.Errorf("resize %v %v %v", w, h, s)
	}
	for i := 1; i < len(e); i++ {
		if e[i].Time <= e[i-1].Time {
			t.Fatal("times do not increase")
		}
	}
}

func TestKeys(t *testing.T) {
	for c := key.Codes(0); c < 300; c++ {
		got := ToKey(c)
		var want kui.Key
		switch {
		case c >= 0x04 && c <= 0x52, c >= 0xE0 && c <= 0xE7:
			want = kui.Key(c)
		case c == key.CodeKeypadEnter:
			want = kui.KeyEnter
		}
		if got != want {
			t.Errorf("ToKey(%d) = %v, want %v", c, got, want)
		}
	}
	// HID usages agree on both sides.
	for _, p := range []struct {
		c key.Codes
		k kui.Key
	}{
		{key.CodeA, kui.KeyA}, {key.Code0, kui.Key0}, {key.CodeReturnEnter, kui.KeyEnter}, {key.CodeSpacebar, kui.KeySpace},
		{key.CodeF12, kui.KeyF12}, {key.CodeUpArrow, kui.KeyArrowUp}, {key.CodeLeftShift, kui.KeyLeftShift}, {key.CodeRightMeta, kui.KeyRightSuper},
	} {
		if got := ToKey(p.c); got != p.k {
			t.Errorf("ToKey(%v) = %v, want %v", p.c, got, p.k)
		}
	}
	all := mods(key.Shift, key.Control, key.Alt, key.Meta, key.CapsLock, key.NumLock)
	want := kui.KeyModShift | kui.KeyModCtrl | kui.KeyModAlt | kui.KeyModSuper | kui.KeyModCapsLock | kui.KeyModNumLock
	if got := ToMods(all); got != want {
		t.Fatalf("ToMods = %v, want %v", got, want)
	}
	if ToButton(events.Middle) != kui.ButtonTertiary || ToButton(events.NoButton) != kui.ButtonNone {
		t.Fatal("ToButton")
	}
}

func TestInputAllocs(t *testing.T) {
	var q kui2go.EventSPSC
	if err := q.Reset(make([]kui.Event, 8), make([]byte, 16)); err != nil {
		t.Fatal(err)
	}
	in := newInput(t, &q, image.Pt(100, 100), 1)
	evs := []events.Event{
		events.NewMouse(events.MouseDown, events.Left, image.Pt(5, 5), 0),
		events.NewMouseMove(events.NoButton, image.Pt(6, 6), image.Pt(5, 5), mods(key.Shift)),
		events.NewScroll(image.Pt(6, 6), math32.Vec2(0, 3), 0),
		events.NewKey(events.KeyDown, 'a', key.CodeA, 0),
		events.NewKey(events.KeyChord, 'a', key.CodeUnknown, 0),
	}
	dst := make([]kui.Event, 8)
	frame := func() {
		for _, ev := range evs {
			in.Handle(ev)
		}
		q.DrainEvents(dst)
	}
	frame()
	if allocs := testing.AllocsPerRun(100, frame); allocs != 0 {
		t.Fatal("allocations:", allocs)
	}
}
