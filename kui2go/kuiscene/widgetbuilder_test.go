package kuiscene

import (
	"strings"
	"testing"

	"github.com/soypat/kui"
)

func TestWidgetEntry(t *testing.T) {
	u := newWidgetUI(t)
	k, n := u.k, u.entry
	style := k.Style(n)
	style.Width = kui.Fixed(60) // Narrow, so text overflows and scrolls.
	k.SetStyle(n, &style)
	k.Layout()
	box := k.Bounds(n)
	pad := style.Padding.Left
	// Clicking between "he" and "llo" (7 pixel glyphs) places the caret there.
	u.clickAt(t, 1*ms, box.X+pad+2*7+2, box.Y+box.H/2)
	if k.Focus() != n {
		t.Fatal("entry not focused")
	}
	if lo, hi := u.w.Selection(n); lo != 2 || hi != 2 {
		t.Fatalf("caret %d %d", lo, hi)
	}
	if r, _ := k.Requests(); len(r) == 0 || r[len(r)-1].Kind != kui.RequestTextInput || r[len(r)-1].Code != 1 {
		t.Fatalf("text input request: %+v", r)
	}
	evs := u.typeText(t, 2*ms, "y")
	if _, ok := hasEvent(evs, n, EventChanged); !ok || k.Text(n) != "heyllo" {
		t.Fatalf("typing: %q", k.Text(n))
	}
	// Select everything after the caret and replace it with pasted text.
	u.key(t, 3*ms, kui.KeyEnd, kui.KeyModShift)
	u.typeText(t, 4*ms, ", world\n")
	if k.Text(n) != "hey, world" {
		t.Fatalf("paste: %q", k.Text(n))
	}
	// Word movement and deletion.
	u.key(t, 5*ms, kui.KeyArrowLeft, kui.KeyModCtrl)
	u.key(t, 6*ms, kui.KeyBackspace, kui.KeyModCtrl)
	if k.Text(n) != "world" {
		t.Fatalf("word delete: %q", k.Text(n))
	}
	u.key(t, 7*ms, kui.KeyDelete, 0)
	if k.Text(n) != "orld" {
		t.Fatalf("delete: %q", k.Text(n))
	}
	// Capacity is 16 bytes, so insertions are truncated at a rune boundary.
	u.key(t, 8*ms, kui.KeyEnd, 0)
	u.typeText(t, 9*ms, "1234567890xñ")
	if k.Text(n) != "orld1234567890x" {
		t.Fatalf("truncated insert: %q", k.Text(n))
	}
	// The text overflows the entry, which scrolls to keep the caret visible.
	if got, want := k.Style(n).Scroll.X, -float32(15*7-(60-2*pad-1)); got != want {
		t.Fatalf("entry scroll %v, want %v", got, want)
	}
	// Clipboard: select all, copy, cut and paste requests.
	u.key(t, 10*ms, kui.KeyA, kui.KeyModCtrl)
	u.key(t, 11*ms, kui.KeyC, kui.KeyModCtrl)
	r, _ := k.Requests()
	if len(r) != 1 || r[0].Kind != kui.RequestClipboardWrite || k.RequestText(&r[0]) != "orld1234567890x" {
		t.Fatalf("copy: %+v", r)
	}
	u.key(t, 12*ms, kui.KeyX, kui.KeyModSuper)
	if k.Text(n) != "" {
		t.Fatalf("cut: %q", k.Text(n))
	}
	u.key(t, 13*ms, kui.KeyV, kui.KeyModCtrl)
	if r, _ := k.Requests(); len(r) != 1 || r[0].Kind != kui.RequestClipboardRead {
		t.Fatalf("paste request: %+v", r)
	}
	if _, ok := hasEvent(u.key(t, 14*ms, kui.KeyEnter, 0), n, EventActivate); !ok {
		t.Fatal("enter did not activate")
	}
	// Leaving the entry ends editing.
	k.SetText(n, strings.Repeat("long text ", 4)[:16])
	u.click(t, 15*ms, u.lbl)
	if k.Focus() != 0 || k.Style(n).Scroll.X != 0 || k.State(n)&stateCaret != 0 {
		t.Fatal("editing did not end")
	}
	if r, _ := k.Requests(); len(r) != 1 || r[0].Kind != kui.RequestTextInput || r[0].Code != 0 {
		t.Fatalf("stop text input request: %+v", r)
	}
}

func TestWidgetCaretBlink(t *testing.T) {
	u := newWidgetUI(t)
	k, n := u.k, u.entry
	k.SetFocus(n)
	u.step(t, 0)
	if k.State(n)&stateCaret == 0 {
		t.Fatal("caret hidden after focus")
	}
	half := u.w.theme.CaretBlink / 2
	if d, ok := k.NextDeadline(); !ok || d != half {
		t.Fatalf("blink deadline %d %v", d, ok)
	}
	u.step(t, half)
	if k.State(n)&stateCaret != 0 || !k.NeedsPaint() {
		t.Fatal("caret did not blink")
	}
	u.step(t, 2*half)
	if k.State(n)&stateCaret == 0 {
		t.Fatal("caret did not blink back")
	}
}

// TestWidgetSlider checks stepped sliders, which the behaviour table's slider has none of.
func TestWidgetSlider(t *testing.T) {
	u := newWidgetUI(t)
	s := u.w.Slider(u.k.Root(), 0.3, 4)
	if v := u.w.SliderValue(s); v < 0.249 || v > 0.251 {
		t.Fatalf("snapped value %v", v)
	}
	u.k.SetFocus(s)
	if e, ok := hasEvent(u.key(t, 1*ms, kui.KeyArrowRight, 0), s, EventChanged); !ok || e.Code != SliderMax/2 || u.w.SliderValue(s) < 0.499 || u.w.SliderValue(s) > 0.501 {
		t.Fatalf("step: %v %+v", u.w.SliderValue(s), e)
	}
	if u.w.Slider(u.k.Root(), 0, 256) != 0 || u.w.Err() != kui.ErrOutOfRange {
		t.Fatal("too many steps accepted")
	}
}

func TestWidgetScroll(t *testing.T) {
	u := newWidgetUI(t)
	k, n := u.k, u.scroll
	box := k.Bounds(n)
	thumb, scale, ok := u.w.scrollThumb(n)
	if !ok || thumb.H >= box.H || thumb.Y != box.Y {
		t.Fatalf("thumb %+v of %+v", thumb, box)
	}
	u.step(t, 1*ms, at(1*ms, events.Scroll(box.X+10, box.Y+10, 0, 20)))
	if k.Style(n).Scroll.Y != -20 {
		t.Fatalf("wheel: %v", k.Style(n).Scroll)
	}
	// Dragging the thumb scrolls proportionally.
	k.Layout()
	thumb, _, _ = u.w.scrollThumb(n)
	tx, ty := thumb.X+thumb.W/2, thumb.Y+thumb.H/2
	u.step(t, 2*ms,
		at(2*ms, events.PointerDown(tx, ty, kui.ButtonPrimary)),
		at(3*ms, events.PointerMove(tx, ty+10, 0)),
		at(4*ms, events.PointerUp(tx, ty+10, kui.ButtonPrimary)))
	if got, want := k.Style(n).Scroll.Y, -20-10*scale; got < want-0.01 || got > want+0.01 {
		t.Fatalf("thumb drag: scroll %v, want %v", got, want)
	}
	// Dragging content (a label) pans it back.
	k.Layout()
	before := k.Style(n).Scroll.Y
	lb := k.Bounds(u.items[3])
	x, y := lb.X+5, lb.Y+5
	u.step(t, 5*ms,
		at(5*ms, from(1, events.PointerDown(x, y, kui.ButtonPrimary))),
		at(6*ms, from(1, events.PointerMove(x, y+15, 0))),
		at(7*ms, from(1, events.PointerUp(x, y+15, kui.ButtonPrimary))))
	if got := k.Style(n).Scroll.Y; got != before+15 {
		t.Fatalf("pan: scroll %v, want %v", got, before+15)
	}
}

func TestWidgetFrameAllocs(t *testing.T) {
	u := newWidgetUI(t)
	var l kui.DrawList
	l.Reset(make([]kui.DrawCmd, 0, 128), make([]byte, 0, 512))
	sb := u.k.Bounds(u.slider)
	scroll := u.k.Bounds(u.scroll)
	now := int64(0)
	frame := func() {
		now += 1e9
		u.click(t, now, u.btn)
		u.click(t, now+1*ms, u.chk)
		u.click(t, now+2*ms, u.entry)
		u.typeText(t, now+3*ms, "abc")
		u.key(t, now+4*ms, kui.KeyBackspace, kui.KeyModCtrl)
		u.step(t, now+5*ms,
			at(now+5*ms, events.PointerDown(sb.X+5, sb.Y+5, kui.ButtonPrimary)),
			at(now+6*ms, events.PointerMove(sb.X+sb.W/2, sb.Y+5, 0)),
			at(now+7*ms, events.PointerUp(sb.X+sb.W/2, sb.Y+5, kui.ButtonPrimary)),
			at(now+8*ms, events.Scroll(scroll.X+5, scroll.Y+5, 0, 7)))
		u.w.ShowPopup(u.popup, true)
		u.key(t, now+9*ms, kui.KeyEscape, 0)
		l.Clear()
		if err := u.k.Paint(&l); err != nil {
			t.Fatal(err)
		}
	}
	frame()
	if allocs := testing.AllocsPerRun(50, frame); allocs != 0 {
		t.Fatal("allocations:", allocs)
	}
}
