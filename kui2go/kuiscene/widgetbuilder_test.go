package kuiscene

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/soypat/kui"
	"github.com/soypat/kui/kui2go"
)

// widgetFont measures like a 7x13 bitmap font.
var widgetFont = kui2go.TextMono{CharWidth: 7, LineHeight: 13}

type widgetUI struct {
	k             *Scene
	w             *WidgetBuilder
	btn, chk, lbl kui.Handle
	entry, slider kui.Handle
	popup, item   kui.Handle
	tip           kui.Handle
	scroll        kui.Handle
	items         [20]kui.Handle
	queue         kui2go.EventSPSC // Text source for pasted text.
}

func newWidgetUI(t testing.TB) *widgetUI {
	eng := new(kui2go.LayoutBasic)
	if err := eng.Configure(kui2go.LayoutBasicConfig{MaxNodes: 64, Measurer: widgetFont}); err != nil {
		t.Fatal(err)
	}
	u := &widgetUI{k: new(Scene), w: new(WidgetBuilder)}
	err := u.k.Configure(Config{
		Memory:    NewMemory(MemoryConfig{MaxNodes: 63, TextBytes: 512, MaxEvents: 64}),
		Engine:    eng,
		Viewport:  kui.Size{W: 240, H: 320},
		RootStyle: kui.LayoutStyle{Direction: kui.TopToBottom, Width: kui.Grow(0, 0), Height: kui.Grow(0, 0), Gap: 4},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = u.w.Configure(u.k, WidgetConfig{Measurer: widgetFont}); err != nil {
		t.Fatal(err)
	}
	u.queue.Reset(make([]kui.Event, 4), make([]byte, 64))
	w, root := u.w, u.k.Root()
	u.lbl = w.Label(root, "label")
	u.btn = w.Button(root, "button")
	u.tip = w.Tooltip(u.btn, "tip")
	u.popup = w.Popup(u.btn)
	u.item = w.Button(u.popup, "item")
	u.chk = w.Check(root, "check", false)
	u.entry = w.Entry(root, "hello", 16)
	u.slider = w.Slider(root, 0.5, 0)
	u.scroll = w.Scroll(root, kui.TopToBottom)
	for i := range u.items {
		u.items[i] = w.Label(u.scroll, "item")
	}
	if err := w.Err(); err != nil {
		t.Fatal(err)
	}
	if err := u.k.Layout(); err != nil {
		t.Fatal(err)
	}
	return u
}

func (u *widgetUI) step(t testing.TB, now int64, evs ...kui.Event) []NodeEvent {
	t.Helper()
	if err := u.k.Step(now, evs, &u.queue); err != nil {
		t.Fatal(err)
	}
	return u.k.Events()
}

func (u *widgetUI) clickAt(t testing.TB, now int64, x, y float32) []NodeEvent {
	return u.step(t, now, at(now, events.PointerDown(x, y, kui.ButtonPrimary)), at(now, events.PointerUp(x, y, kui.ButtonPrimary)))
}

func (u *widgetUI) click(t testing.TB, now int64, h kui.Handle) []NodeEvent {
	b := u.k.Bounds(h)
	return u.clickAt(t, now, b.X+b.W/2, b.Y+b.H/2)
}

func (u *widgetUI) key(t testing.TB, now int64, key kui.Key, mods kui.KeyMods) []NodeEvent {
	return u.step(t, now, at(now, withMods(mods, events.KeyDown(key))))
}

func (u *widgetUI) typeText(t testing.TB, now int64, s string) []NodeEvent {
	u.queue.WriteTextEvent(kui.Event{Time: now, Kind: kui.EventText}, []byte(s))
	var evs [4]kui.Event
	n := u.queue.DrainEvents(evs[:])
	return u.step(t, now, evs[:n]...)
}

func hasEvent(evs []NodeEvent, n kui.Handle, kind kui.EventKind) (NodeEvent, bool) {
	for _, e := range evs {
		if e.Node == n && e.Kind == kind {
			return e, true
		}
	}
	return NodeEvent{}, false
}

func TestWidgets(t *testing.T) {
	u := newWidgetUI(t)
	if _, ok := hasEvent(u.click(t, 1e9, u.btn), u.btn, EventActivate); !ok {
		t.Fatal("button click did not activate")
	}
	e, ok := hasEvent(u.click(t, 2e9, u.chk), u.chk, EventChanged)
	if !ok || e.Code != 1 || !u.w.Checked(u.chk) {
		t.Fatalf("check box did not check: %+v", e)
	}
	// Space toggles the focused check box back.
	if u.k.Focus() != u.chk {
		t.Fatal("check box should be focused after click")
	}
	if e, ok := hasEvent(u.key(t, 3e9, kui.KeySpace, 0), u.chk, EventChanged); !ok || e.Code != 0 || u.w.Checked(u.chk) {
		t.Fatal("space did not uncheck")
	}
	// Clicking a label does nothing and clears focus.
	if evs := u.click(t, 4e9, u.lbl); len(evs) != 1 || evs[0].Kind != EventFocusOut {
		t.Fatalf("label click: %+v", evs)
	}
	// Disabled buttons receive no input.
	u.k.SetFlags(u.btn, FlagDisabled)
	if evs := u.click(t, 5e9, u.btn); len(evs) != 0 {
		t.Fatalf("disabled button got events: %+v", evs)
	}
}

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

func TestWidgetSlider(t *testing.T) {
	u := newWidgetUI(t)
	k, n := u.k, u.slider
	if v := u.w.SliderValue(n); v < 0.49 || v > 0.51 {
		t.Fatalf("initial value %v", v)
	}
	box := k.Bounds(n)
	y := box.Y + box.H/2
	e, ok := hasEvent(u.clickAt(t, 1*ms, box.X+1, y), n, EventChanged)
	if !ok || e.Code != 0 || u.w.SliderValue(n) != 0 {
		t.Fatalf("click at start: %+v %v", e, u.w.SliderValue(n))
	}
	evs := u.step(t, 2*ms,
		at(2*ms, events.PointerDown(box.X+box.W/2, y, kui.ButtonPrimary)),
		at(3*ms, events.PointerMove(box.X+box.W+50, y, 0)),
		at(4*ms, events.PointerUp(box.X+box.W+50, y, kui.ButtonPrimary)))
	if u.w.SliderValue(n) != 1 {
		t.Fatalf("drag to end: %v %v", u.w.SliderValue(n), evs)
	}
	u.key(t, 5*ms, kui.KeyArrowLeft, 0)
	if v := u.w.SliderValue(n); v < 0.94 || v > 0.96 {
		t.Fatalf("arrow key: %v", v)
	}
	// Stepped sliders snap.
	s := u.w.Slider(u.k.Root(), 0.3, 4)
	if v := u.w.SliderValue(s); v < 0.249 || v > 0.251 {
		t.Fatalf("snapped value %v", v)
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

func TestWidgetPopupAndTooltip(t *testing.T) {
	u := newWidgetUI(t)
	k := u.k
	u.w.ShowPopup(u.popup, true)
	k.Layout()
	// Clicking an item inside the popup works; clicking outside dismisses it.
	if _, ok := hasEvent(u.click(t, 1*ms, u.item), u.item, EventActivate); !ok {
		t.Fatal("popup item did not activate")
	}
	eb := k.Bounds(u.entry)
	evs := u.clickAt(t, 2*ms, eb.X+eb.W-5, eb.Y+5)
	if _, ok := hasEvent(evs, u.popup, EventDismiss); !ok || k.Flags(u.popup)&FlagHidden == 0 {
		t.Fatalf("press outside did not dismiss: %+v", evs)
	}
	if k.Focus() == u.entry {
		t.Fatal("press outside reached the entry")
	}
	u.w.ShowPopup(u.popup, true)
	u.key(t, 3*ms, kui.KeyEscape, 0)
	if k.Flags(u.popup)&FlagHidden == 0 {
		t.Fatal("escape did not dismiss")
	}
	// Tooltips show after resting over their widget and hide when leaving it.
	b := k.Bounds(u.btn)
	u.step(t, 10*ms, at(10*ms, events.PointerMove(b.X+2, b.Y+2, 0)))
	if k.Flags(u.tip)&FlagHidden == 0 {
		t.Fatal("tooltip shown early")
	}
	u.step(t, 10*ms+u.w.theme.TooltipDelay)
	if k.Flags(u.tip)&FlagHidden != 0 {
		t.Fatal("tooltip not shown")
	}
	k.Layout()
	if tb := k.Bounds(u.tip); tb.Y < b.Y+b.H {
		t.Fatalf("tooltip %+v not below button %+v", tb, b)
	}
	u.step(t, 20*ms+u.w.theme.TooltipDelay, at(0, events.PointerMove(1, 1, 0)))
	if k.Flags(u.tip)&FlagHidden == 0 {
		t.Fatal("tooltip not hidden")
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

// FuzzWidgets drives the widgets with random input, checking invariants after every step.
func FuzzWidgets(f *testing.F) {
	f.Add([]byte{1, 100, 140, 0, 5, 'a', 0, 0, 6, 3, 0, 0, 4, 2, 1, 0, 2, 100, 140, 0})
	f.Add([]byte{1, 30, 30, 0, 2, 30, 30, 0, 7, 0, 0, 0, 3, 120, 250, 9, 4, 6, 2, 0})
	keys := [...]kui.Key{kui.KeyArrowLeft, kui.KeyArrowRight, kui.KeyHome, kui.KeyEnd, kui.KeyBackspace,
		kui.KeyDelete, kui.KeyEnter, kui.KeyEscape, kui.KeyTab, kui.KeySpace, kui.KeyA, kui.KeyC, kui.KeyX, kui.KeyV}
	texts := [...]string{"", "a", "ñ", "word", "two words", "\x01ctl\n", "ünïcödé"}
	f.Fuzz(func(t *testing.T, data []byte) {
		u := newWidgetUI(t)
		k, w := u.k, u.w
		now := int64(0)
		for i := 0; i+4 <= len(data); i += 4 {
			b := data[i : i+4]
			now += int64(b[3]) * 5 * ms
			x, y := float32(b[1])*240/255, float32(b[2])*320/255
			src := b[3] % 2
			var evs []kui.Event
			switch b[0] % 8 {
			case 0:
				evs = append(evs, at(now, from(src, events.PointerMove(x, y, 0))))
			case 1:
				evs = append(evs, at(now, from(src, withMods(kui.KeyMods(b[3]>>4), events.PointerDown(x, y, kui.PointerButton(1+b[3]%2))))))
			case 2:
				evs = append(evs, at(now, from(src, events.PointerUp(x, y, kui.PointerButton(1+b[3]%2)))))
			case 3:
				evs = append(evs, at(now, events.Scroll(x, y, 0, float32(int8(b[1])))))
			case 4:
				evs = append(evs, at(now, withMods(kui.KeyMods(b[2])&(kui.KeyModShift|kui.KeyModCtrl|kui.KeyModAlt), events.KeyDown(keys[int(b[1])%len(keys)]))))
			case 5:
				evs = append(evs, at(now, events.Rune(rune(b[1]))))
			case 6:
				u.typeText(t, now, texts[int(b[1])%len(texts)])
			case 7:
				w.ShowPopup(u.popup, b[1]%2 == 0)
			}
			u.step(t, now, evs...)
			// Invariants.
			s := k.Text(u.entry)
			if len(s) > k.TextCap(u.entry) || k.TextCap(u.entry) != 16 {
				t.Fatalf("entry text %q exceeds capacity %d", s, k.TextCap(u.entry))
			}
			if lo, hi := w.Selection(u.entry); lo < 0 || lo > hi || hi > len(s) || !runeBoundary(s, lo) || !runeBoundary(s, hi) {
				t.Fatalf("selection [%d, %d) invalid in %q", lo, hi, s)
			}
			if f := k.Focus(); f != 0 && k.Flags(f)&FlagFocusable == 0 {
				t.Fatal("unfocusable node focused")
			}
			if v := w.SliderValue(u.slider); v < 0 || v > 1 {
				t.Fatalf("slider value %v", v)
			}
			if sc, content, box := k.Style(u.scroll).Scroll.Y, k.ContentSize(u.scroll), k.Bounds(u.scroll); sc > 0 || -sc > max(content.H-box.H, 0)+0.01 {
				t.Fatalf("scroll %v out of range", sc)
			}
		}
	})
}

func runeBoundary(s string, i int) bool { return i == len(s) || utf8.RuneStart(s[i]) }
