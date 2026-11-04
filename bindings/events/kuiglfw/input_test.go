package kuiglfw

import (
	"testing"

	"github.com/go-gl/glfw/v3.4/glfw"
	"github.com/soypat/kui"
	"github.com/soypat/kui/kui2go"
)

// recorder is an EventWriter keeping what is written, holding at most max events.
type recorder struct {
	evs  []kui.Event
	text []string
	max  int
}

func (r *recorder) WriteEvents(src []kui.Event) int {
	n := min(len(src), r.max-len(r.evs))
	r.evs = append(r.evs, src[:n]...)
	return n
}

func (r *recorder) WriteTextEvent(e kui.Event, text []byte) error {
	if len(r.evs) >= r.max {
		return kui.ErrBufferFull
	}
	r.evs = append(r.evs, e)
	r.text = append(r.text, string(text))
	return nil
}

func newInput(t *testing.T, w kui.EventWriter) *Input {
	t.Helper()
	var now int64
	in := new(Input)
	err := in.configure(Config{Events: w, Nanotime: func() int64 { now++; return now }})
	if err != nil {
		t.Fatal(err)
	}
	return in
}

func kinds(evs []kui.Event) []kui.EventKind {
	ks := make([]kui.EventKind, len(evs))
	for i := range evs {
		ks[i] = evs[i].Kind
	}
	return ks
}

func TestConfigure(t *testing.T) {
	var in Input
	if err := in.configure(Config{}); err != kui.ErrInvalidConfig {
		t.Fatal("no writer:", err)
	}
	if err := in.Configure(nil, Config{Events: &recorder{max: 1}}); err != kui.ErrInvalidConfig {
		t.Fatal("no window:", err)
	}
	in = *newInput(t, &recorder{max: 1})
	if in.scrollStep != 20 || in.Scale() != 1 {
		t.Fatal("defaults:", in.scrollStep, in.Scale())
	}
}

// oldToKey is the key mapping of kui-use's driver/glfw before this package.
func oldToKey(k glfw.Key) kui.Key {
	switch {
	case k >= glfw.KeyA && k <= glfw.KeyZ:
		return kui.KeyA + kui.Key(k-glfw.KeyA)
	case k >= glfw.Key1 && k <= glfw.Key9:
		return kui.Key1 + kui.Key(k-glfw.Key1)
	case k == glfw.Key0:
		return kui.Key0
	case k >= glfw.KeyF1 && k <= glfw.KeyF12:
		return kui.KeyF1 + kui.Key(k-glfw.KeyF1)
	}
	switch k {
	case glfw.KeyEnter, glfw.KeyKPEnter:
		return kui.KeyEnter
	case glfw.KeyEscape:
		return kui.KeyEscape
	case glfw.KeyBackspace:
		return kui.KeyBackspace
	case glfw.KeyTab:
		return kui.KeyTab
	case glfw.KeySpace:
		return kui.KeySpace
	case glfw.KeyMinus:
		return kui.KeyMinus
	case glfw.KeyEqual:
		return kui.KeyEqual
	case glfw.KeyLeftBracket:
		return kui.KeyLeftBracket
	case glfw.KeyRightBracket:
		return kui.KeyRightBracket
	case glfw.KeyBackslash:
		return kui.KeyBackslash
	case glfw.KeySemicolon:
		return kui.KeySemicolon
	case glfw.KeyApostrophe:
		return kui.KeyApostrophe
	case glfw.KeyGraveAccent:
		return kui.KeyGrave
	case glfw.KeyComma:
		return kui.KeyComma
	case glfw.KeyPeriod:
		return kui.KeyPeriod
	case glfw.KeySlash:
		return kui.KeySlash
	case glfw.KeyCapsLock:
		return kui.KeyCapsLock
	case glfw.KeyInsert:
		return kui.KeyInsert
	case glfw.KeyHome:
		return kui.KeyHome
	case glfw.KeyPageUp:
		return kui.KeyPageUp
	case glfw.KeyDelete:
		return kui.KeyDelete
	case glfw.KeyEnd:
		return kui.KeyEnd
	case glfw.KeyPageDown:
		return kui.KeyPageDown
	case glfw.KeyRight:
		return kui.KeyArrowRight
	case glfw.KeyLeft:
		return kui.KeyArrowLeft
	case glfw.KeyDown:
		return kui.KeyArrowDown
	case glfw.KeyUp:
		return kui.KeyArrowUp
	case glfw.KeyLeftControl:
		return kui.KeyLeftCtrl
	case glfw.KeyLeftShift:
		return kui.KeyLeftShift
	case glfw.KeyLeftAlt:
		return kui.KeyLeftAlt
	case glfw.KeyLeftSuper:
		return kui.KeyLeftSuper
	case glfw.KeyRightControl:
		return kui.KeyRightCtrl
	case glfw.KeyRightShift:
		return kui.KeyRightShift
	case glfw.KeyRightAlt:
		return kui.KeyRightAlt
	case glfw.KeyRightSuper:
		return kui.KeyRightSuper
	}
	return 0
}

func TestKeys(t *testing.T) {
	added := map[glfw.Key]kui.Key{
		glfw.KeyPrintScreen: kui.KeyPrintScreen,
		glfw.KeyScrollLock:  kui.KeyScrollLock,
		glfw.KeyPause:       kui.KeyPause,
	}
	for k := glfw.KeyUnknown - 1; k <= glfw.KeyLast+1; k++ {
		want, ok := added[k]
		if !ok {
			want = oldToKey(k)
		}
		if got := ToKey(k); got != want {
			t.Errorf("ToKey(%d) = %v, want %v", k, got, want)
		}
	}
	for _, c := range []struct {
		g glfw.Key
		k kui.Key
	}{
		{glfw.KeyA, kui.KeyA}, {glfw.KeyZ, kui.KeyZ}, {glfw.Key0, kui.Key0}, {glfw.Key9, kui.Key9},
		{glfw.KeyF12, kui.KeyF12}, {glfw.KeyKPEnter, kui.KeyEnter}, {glfw.KeyRightSuper, kui.KeyRightSuper},
		{glfw.KeyKP5, 0}, {glfw.KeyF13, 0}, {glfw.KeyMenu, 0}, {glfw.KeyUnknown, 0},
	} {
		if got := ToKey(c.g); got != c.k {
			t.Errorf("ToKey(%d) = %v, want %v", c.g, got, c.k)
		}
	}
}

func TestModsAndButtons(t *testing.T) {
	all := glfw.ModShift | glfw.ModControl | glfw.ModAlt | glfw.ModSuper | glfw.ModCapsLock | glfw.ModNumLock
	want := kui.KeyModShift | kui.KeyModCtrl | kui.KeyModAlt | kui.KeyModSuper | kui.KeyModCapsLock | kui.KeyModNumLock
	if got := ToMods(all); got != want {
		t.Fatalf("ToMods(all) = %v, want %v", got, want)
	}
	if got := ToMods(glfw.ModControl); got != kui.KeyModCtrl {
		t.Fatalf("ToMods(ctrl) = %v", got)
	}
	buttons := []kui.PointerButton{kui.ButtonPrimary, kui.ButtonSecondary, kui.ButtonTertiary, kui.ButtonBack, kui.ButtonForward}
	for b := glfw.MouseButton1; b <= glfw.MouseButtonLast; b++ {
		want := kui.ButtonNone
		if int(b) < len(buttons) {
			want = buttons[b]
		}
		if got := ToButton(b); got != want {
			t.Errorf("ToButton(%d) = %v, want %v", b, got, want)
		}
	}
}

func TestCoalesce(t *testing.T) {
	r := &recorder{max: 100}
	in := newInput(t, r)
	for i := range 100 {
		in.cursorPos(float64(i), 2*float64(i))
	}
	if len(r.evs) != 0 {
		t.Fatal("moves written before flush:", len(r.evs))
	}
	in.Flush()
	in.Flush() // Nothing pending.
	if len(r.evs) != 1 || r.evs[0].Kind != kui.EventPointerMove || r.evs[0].X != 99 || r.evs[0].Y != 198 {
		t.Fatalf("moves: %+v", r.evs)
	}

	// Scrolls sum; a key writes the pending scroll first; the press is at the last position.
	r.evs = r.evs[:0]
	in.scroll(0, 1)
	in.scroll(0.5, 2)
	in.key(glfw.KeyA, glfw.Press, glfw.ModShift)
	in.cursorPos(5, 6)
	in.mouseButton(glfw.MouseButtonLeft, glfw.Press, 0)
	in.mouseButton(glfw.MouseButtonLeft, glfw.Release, 0)
	got := kinds(r.evs)
	want := []kui.EventKind{kui.EventScroll, kui.EventKeyDown, kui.EventPointerMove, kui.EventPointerDown, kui.EventPointerUp}
	if len(got) != len(want) {
		t.Fatalf("kinds %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("kinds %v, want %v", got, want)
		}
	}
	if s := r.evs[0]; s.DX != -10 || s.DY != -60 {
		t.Fatalf("scroll %v %v, want -10 -60", s.DX, s.DY)
	}
	if k := r.evs[1]; k.Key() != kui.KeyA || k.KeyMods != kui.KeyModShift {
		t.Fatalf("key %v mods %v", k.Key(), k.KeyMods)
	}
	if d := r.evs[3]; d.X != 5 || d.Y != 6 || d.Button != kui.ButtonPrimary || d.KeyMods != 0 {
		t.Fatalf("press %+v", d)
	}
	for i := 1; i < len(r.evs); i++ {
		if r.evs[i].Time < r.evs[i-1].Time {
			t.Fatal("times decrease")
		}
	}

	// A modifier press splits moves; moves after it carry the modifier.
	r.evs = r.evs[:0]
	in.cursorPos(1, 1)
	in.key(glfw.KeyLeftControl, glfw.Press, glfw.ModControl)
	in.cursorPos(2, 2)
	in.cursorPos(3, 3)
	in.Flush()
	if got := kinds(r.evs); len(got) != 3 || r.evs[2].X != 3 || r.evs[2].KeyMods != kui.KeyModCtrl {
		t.Fatalf("events %+v", r.evs)
	}
}

func TestEvents(t *testing.T) {
	r := &recorder{max: 100}
	in := newInput(t, r)
	in.key(glfw.KeyEnter, glfw.Repeat, 0)
	in.key(glfw.KeyEnter, glfw.Release, 0)
	in.key(glfw.KeyKP5, glfw.Press, 0)               // Unmapped: dropped.
	in.mouseButton(glfw.MouseButton8, glfw.Press, 0) // Unmapped: dropped.
	in.char('ñ')
	in.focus(false)
	in.focus(true)
	in.cursorEnter(true) // Moves follow; nothing to write.
	in.cursorEnter(false)
	want := []kui.EventKind{kui.EventKeyDown, kui.EventKeyUp, kui.EventText, kui.EventBlur, kui.EventFocus, kui.EventPointerLeave}
	got := kinds(r.evs)
	if len(got) != len(want) {
		t.Fatalf("kinds %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("kinds %v, want %v", got, want)
		}
	}
	if r.evs[2].Rune() != 'ñ' || r.evs[2].IsString() {
		t.Fatalf("rune %q", r.evs[2].Rune())
	}
}

func TestResize(t *testing.T) {
	r := &recorder{max: 100}
	in := newInput(t, r)
	in.resize(400, 300, 800, 600) // HiDPI.
	in.resize(400, 300, 800, 600) // Size and framebuffer callbacks both report a change.
	in.resize(0, 0, 0, 0)         // Minimized.
	in.resize(500, 300, 500, 300)
	if len(r.evs) != 2 {
		t.Fatalf("%d resizes, want 2", len(r.evs))
	}
	if w, h, s := r.evs[0].Resize(); w != 400 || h != 300 || s != 2 {
		t.Fatalf("resize %v %v %v", w, h, s)
	}
	if w, h, s := r.evs[1].Resize(); w != 500 || h != 300 || s != 1 || in.Scale() != 1 {
		t.Fatalf("resize %v %v %v", w, h, s)
	}
	if w, h := in.FramebufferSize(); w != 500 || h != 300 {
		t.Fatalf("framebuffer %dx%d", w, h)
	}
}

func TestDropped(t *testing.T) {
	r := &recorder{max: 2}
	in := newInput(t, r)
	in.cursorPos(1, 1)
	in.char('a') // Writes the move and the rune.
	in.char('b')
	in.cursorPos(2, 2)
	in.Flush()
	if len(r.evs) != 2 || in.Dropped() != 2 {
		t.Fatalf("written %d dropped %d, want 2 and 2", len(r.evs), in.Dropped())
	}
}

// TestSPSC drives an input writing to a kui2go queue, checking it does not allocate.
func TestSPSC(t *testing.T) {
	var q kui2go.EventSPSC
	if err := q.Reset(make([]kui.Event, 16), make([]byte, 64)); err != nil {
		t.Fatal(err)
	}
	in := newInput(t, &q)
	dst := make([]kui.Event, 16)
	x := 0.0
	frame := func() {
		for range 10 {
			x++
			in.cursorPos(x, x)
		}
		in.mouseButton(glfw.MouseButtonLeft, glfw.Press, 0)
		in.mouseButton(glfw.MouseButtonLeft, glfw.Release, 0)
		in.scroll(0, 1)
		in.scroll(0, 1)
		in.key(glfw.KeyA, glfw.Press, glfw.ModShift)
		in.char('A')
		in.key(glfw.KeyA, glfw.Release, glfw.ModShift)
		in.Flush()
		if n := q.DrainEvents(dst); n != 7 {
			t.Fatalf("drained %d events, want 7", n)
		}
	}
	frame()
	if allocs := testing.AllocsPerRun(100, frame); allocs != 0 {
		t.Fatal("allocations:", allocs)
	}
	if in.Dropped() != 0 {
		t.Fatal("dropped:", in.Dropped())
	}
}
