package kuiscene

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/soypat/kui"
	"github.com/soypat/kui/gesture"
	"github.com/soypat/kui/kui2go"
)

// The harness drives a widgetUI with ops: user actions on named widgets that table tests
// write with constructors (click("btn"), key(kui.KeyEnd, kui.KeyModShift)...) and that random
// tests and fuzzers decode from bytes. After every op it checks the invariants of the scene and
// of the widgets, and it keeps a trace of the last ops so failures read like a script.

// widgetFont measures like a 7x13 bitmap font.
var widgetFont = kui2go.TextMono{CharWidth: 7, LineHeight: 13}

// Named widgets of widgetUI that ops target, like the finders of Flutter's WidgetTester or the
// item references of Dear ImGui's test engine.
const (
	refLabel = iota
	refButton
	refTip
	refPopup
	refItem
	refCheck
	refEntry
	refSlider
	refScroll
	refRow // An item of the scroll container.
	numRefs
)

var refNames = [numRefs]string{"lbl", "btn", "tip", "popup", "item", "chk", "entry", "slider", "scroll", "row"}

// ref returns the index of the widget named name.
func ref(name string) uint8 {
	for i, n := range refNames {
		if n == name {
			return uint8(i)
		}
	}
	panic("unknown widget " + strconv.Quote(name))
}

const traceLen = 32

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

	named   [numRefs]kui.Handle
	now     int64   // Time of the last op.
	down    [2]bool // Pointers held down by ops, per source.
	painted bool    // NeedsPaint reported before the last op's paint.
	paint   kui.DrawList
	label   string // Names the test case in failures, i.e. a seed.
	trace   [traceLen]string
	ntrace  int
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
	u.named = [numRefs]kui.Handle{u.lbl, u.btn, u.tip, u.popup, u.item, u.chk, u.entry, u.slider, u.scroll, u.items[3]}
	u.paint.Reset(make([]kui.DrawCmd, 0, 256), make([]byte, 0, 1024))
	if err := u.k.Paint(&u.paint); err != nil { // The host's first frame.
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

func runeBoundary(s string, i int) bool { return i == len(s) || utf8.RuneStart(s[i]) }

// Ops.

type opKind uint8

const (
	opMove     opKind = iota // The pointer moves to the target point.
	opPress                  // Pointer press at the target point.
	opRelease                // Pointer release at the target point.
	opClick                  // Press and release at the target point in one Step.
	opDrag                   // Press, move by a delta and release in one Step.
	opWheel                  // Wheel scroll of int8(arg) at the target point.
	opKey                    // Key opKeys[arg&15] with modifiers opMods[arg>>4&7].
	opType                   // Text opTexts[arg%len], through the text queue like a paste.
	opRune                   // A single rune event of rune(arg): maybe a control character.
	opWait                   // No input: time passes.
	opLeave                  // The pointer leaves the window.
	opCancel                 // The pointer is cancelled.
	opPopup                  // Show the popup if arg is odd, else hide it.
	opFlags                  // Enable and show (arg%3 == 0), disable (1) or hide (2) the target.
	opFocus                  // Focus the target from code.
	opClickAbs               // Click at x, y as fractions of the viewport: also between widgets.
	numOps
)

// Op kinds and targets share an encoded op's first byte.
var _ [numOps - 16]struct{}
var _ [16 - numOps]struct{}
var _ [16 - numRefs]struct{}

// Pointer op arguments.
const (
	argSecondary = 1 << 0 // Secondary button.
	argShift     = 1 << 1 // Press and click with shift held.
	argSource1   = 1 << 7 // From pointer source 1.
)

// op is one user action on a widgetUI. Ops encode to opBytes bytes.
type op struct {
	kind   opKind
	target uint8 // Index in refNames.
	x, y   uint8 // Point in the target's box, as fractions (x+0.5)/256 of its size.
	arg    uint8 // Kind specific, see opKind.
	dt     uint8 // Time since the previous op in opUnits.
}

const (
	opBytes = 5
	opUnit  = 10 * ms
	humanDT = 40 // 400ms between ops of table tests: slower than a double tap.
)

var opKeys = [16]kui.Key{
	kui.KeyArrowLeft, kui.KeyArrowRight, kui.KeyArrowUp, kui.KeyArrowDown, kui.KeyHome, kui.KeyEnd,
	kui.KeyBackspace, kui.KeyDelete, kui.KeyEnter, kui.KeyEscape, kui.KeyTab, kui.KeySpace,
	kui.KeyA, kui.KeyC, kui.KeyX, kui.KeyV,
}

var opMods = [8]kui.KeyMods{
	0, kui.KeyModShift, kui.KeyModCtrl, kui.KeyModCtrl | kui.KeyModShift,
	kui.KeyModAlt, kui.KeyModSuper, kui.KeyModAlt | kui.KeyModShift, kui.KeyModSuper | kui.KeyModShift,
}

// opTexts is the text typed by ops. Table tests type only these, so their ops stay encodable.
var opTexts = [...]string{"", "a", "ñ", "word", " world", "two words", "\x01ctl\n", "ünïcödé", "1234567890xñ"}

func decodeOps(data []byte) []op {
	ops := make([]op, 0, len(data)/opBytes)
	for ; len(data) >= opBytes; data = data[opBytes:] {
		ops = append(ops, op{
			kind: opKind(data[0] % uint8(numOps)), target: (data[0] >> 4) % numRefs,
			x: data[1], y: data[2], arg: data[3], dt: data[4],
		})
	}
	return ops
}

func encodeOps(ops []op) []byte {
	b := make([]byte, 0, len(ops)*opBytes)
	for _, o := range ops {
		b = append(b, uint8(o.kind)|o.target<<4, o.x, o.y, o.arg, o.dt)
	}
	return b
}

func frac(v uint8) float32 { return (float32(v) + 0.5) / 256 }

// dragDelta returns the motion of a drag op: arg's high bit selects the axis and its low 7
// bits the distance in pixels, halved and offset by 64.
func (o op) dragDelta() (dx, dy float32) {
	d := float32(2 * (int(o.arg&0x7f) - 64))
	if o.arg&0x80 != 0 {
		return 0, d
	}
	return d, 0
}

func (o op) String() string {
	var b strings.Builder
	where := refNames[o.target]
	if o.x != 128 || o.y != 128 {
		where += fmt.Sprintf("@(%.2f,%.2f)", frac(o.x), frac(o.y))
	}
	pointer := func(verb string) {
		if o.arg&argShift != 0 && (o.kind == opPress || o.kind == opClick) {
			b.WriteString("shift+")
		}
		if o.arg&argSecondary != 0 && o.kind != opMove {
			b.WriteString("r")
		}
		catspace(&b, verb, where)
		if o.arg&argSource1 != 0 {
			b.WriteString(" src1")
		}
	}
	switch o.kind {
	case opMove:
		pointer("move")
	case opPress:
		pointer("press")
	case opRelease:
		pointer("release")
	case opClick:
		pointer("click")
	case opDrag:
		dx, dy := o.dragDelta()
		fmt.Fprintf(&b, "drag %s by (%+g,%+g)", where, dx, dy)
	case opWheel:
		fmt.Fprintf(&b, "wheel %s by %+d", where, int8(o.arg))
	case opKey:
		catspace(&b, "key", modsString(opMods[o.arg>>4&7]), opKeys[o.arg&15].String())
	case opType:
		catspace(&b, "type", strconv.Quote(opTexts[int(o.arg)%len(opTexts)]))
	case opRune:
		catspace(&b, "rune", strconv.QuoteRune(rune(o.arg)))
	case opWait:
		b.WriteString("wait")
	case opLeave, opCancel:
		b.WriteString([...]string{opLeave: "leave", opCancel: "cancel"}[o.kind])
		if o.arg&argSource1 != 0 {
			b.WriteString(" src1")
		}
	case opPopup:
		catspace(&b, "popup", strconv.FormatBool(o.arg&1 != 0))
	case opFlags:
		catspace(&b, [...]string{"enable", "disable", "hide"}[o.arg%3], refNames[o.target])
	case opFocus:
		catspace(&b, "focus", refNames[o.target])
	case opClickAbs:
		fmt.Fprintf(&b, "click at (%.2f,%.2f) of viewport", frac(o.x), frac(o.y))
	}
	fmt.Fprintf(&b, " +%dms", int(o.dt)*int(opUnit/ms))
	return b.String()
}

func cat(b *strings.Builder, strs ...string) {
	for i := range strs {
		b.WriteString(strs[i])
	}
}

func catspace(b *strings.Builder, strs ...string) {
	for i := range strs {
		if i != 0 {
			b.WriteByte(' ')
		}
		b.WriteString(strs[i])
	}
}

func modsString(m kui.KeyMods) string {
	var s string
	for _, mod := range [...]struct {
		m    kui.KeyMods
		name string
	}{{kui.KeyModCtrl, "ctrl+"}, {kui.KeyModAlt, "alt+"}, {kui.KeyModSuper, "super+"}, {kui.KeyModShift, "shift+"}} {
		if m&mod.m != 0 {
			s += mod.name
		}
	}
	return s
}

// Op constructors for table tests. They act on the named widget's center at human pace.

func pointerOp(kind opKind, name string) op {
	return op{kind: kind, target: ref(name), x: 128, y: 128, dt: humanDT}
}

func fracByte(f float32) uint8 { return uint8(min(max(math.Round(float64(f*256-0.5)), 0), 255)) }

func click(name string) op   { return pointerOp(opClick, name) }
func press(name string) op   { return pointerOp(opPress, name) }
func release(name string) op { return pointerOp(opRelease, name) }
func hover(name string) op   { return pointerOp(opMove, name) }
func rclick(name string) op  { o := click(name); o.arg = argSecondary; return o }

// clickAt clicks at fx, fy of the named widget's box.
func clickAt(name string, fx, fy float32) op {
	o := click(name)
	o.x, o.y = fracByte(fx), fracByte(fy)
	return o
}

// drag drags from the named widget's center by dx or dy pixels: even, in [-128, 126].
func drag(name string, dx, dy int) op {
	d, axis := dx, uint8(0)
	if dy != 0 {
		d, axis = dy, 0x80
	}
	if dx != 0 && dy != 0 || d%2 != 0 || d < -128 || d > 126 {
		panic("drag delta not encodable")
	}
	o := pointerOp(opDrag, name)
	o.arg = axis | uint8(d/2+64)
	return o
}

// dragFrom drags from fx, fy of the named widget's box by dx or dy pixels.
func dragFrom(name string, fx, fy float32, dx, dy int) op {
	o := drag(name, dx, dy)
	o.x, o.y = fracByte(fx), fracByte(fy)
	return o
}

func wheel(name string, dy int8) op { o := pointerOp(opWheel, name); o.arg = uint8(dy); return o }

func key(k kui.Key, m kui.KeyMods) op {
	o := op{kind: opKey, dt: humanDT}
	ki, mi := -1, -1
	for i := range opKeys {
		if opKeys[i] == k {
			ki = i
		}
	}
	for i := range opMods {
		if opMods[i] == m {
			mi = i
		}
	}
	if ki < 0 || mi < 0 {
		panic("key not encodable: " + modsString(m) + k.String())
	}
	o.arg = uint8(ki | mi<<4)
	return o
}

func typ(s string) op {
	for i := range opTexts {
		if opTexts[i] == s {
			return op{kind: opType, arg: uint8(i), dt: humanDT}
		}
	}
	panic("text not in opTexts: " + strconv.Quote(s))
}

// wait lets d pass without input, at most 2.55s.
func wait(d int64) op {
	if d%opUnit != 0 || d/opUnit > 255 {
		panic("wait not encodable")
	}
	return op{kind: opWait, dt: uint8(d / opUnit)}
}

func showPopup(show bool) op {
	o := op{kind: opPopup, dt: humanDT}
	if show {
		o.arg = 1
	}
	return o
}

func focus(name string) op   { return op{kind: opFocus, target: ref(name), dt: humanDT} }
func enable(name string) op  { return op{kind: opFlags, target: ref(name), dt: humanDT} }
func disable(name string) op { return op{kind: opFlags, target: ref(name), arg: 1, dt: humanDT} }
func hide(name string) op    { return op{kind: opFlags, target: ref(name), arg: 2, dt: humanDT} }

// quick makes o follow the previous op after 10ms.
func quick(o op) op { o.dt = 1; return o }

// after makes o follow the previous op after d, at most 2.55s.
func after(d int64, o op) op { o.dt = wait(d).dt; return o }

// point returns where pointer op o acts.
func (u *widgetUI) point(o op) (x, y float32) {
	if o.kind == opClickAbs {
		vp := u.k.Viewport()
		return vp.W * frac(o.x), vp.H * frac(o.y)
	}
	b := u.k.Bounds(u.named[o.target])
	return b.X + b.W*frac(o.x), b.Y + b.H*frac(o.y)
}

// apply performs o, checks invariants and returns the events of the Steps it took.
func (u *widgetUI) apply(t testing.TB, o op) []NodeEvent {
	t.Helper()
	u.now += int64(o.dt) * opUnit
	now := u.now
	x, y := u.point(o)
	var src uint8
	if o.arg&argSource1 != 0 {
		src = 1
	}
	button := kui.ButtonPrimary
	if o.arg&argSecondary != 0 {
		button = kui.ButtonSecondary
	}
	var mods kui.KeyMods
	if o.arg&argShift != 0 && (o.kind == opPress || o.kind == opClick) {
		mods = kui.KeyModShift
	}
	pe := func(e kui.Event) kui.Event { return at(now, from(src, withMods(mods, e))) }
	var buf [3]kui.Event
	in := buf[:0]
	switch o.kind {
	case opMove:
		in = append(in, pe(events.PointerMove(x, y, 0)))
	case opPress:
		in = append(in, pe(events.PointerDown(x, y, button)))
		u.down[src] = true
	case opRelease:
		in = append(in, pe(events.PointerUp(x, y, button)))
		u.down[src] = false
	case opClick, opClickAbs:
		in = append(in, pe(events.PointerDown(x, y, button)), pe(events.PointerUp(x, y, button)))
		u.down[src] = false
	case opDrag:
		dx, dy := o.dragDelta()
		in = append(in, pe(events.PointerDown(x, y, button)), pe(events.PointerMove(x+dx, y+dy, 0)), pe(events.PointerUp(x+dx, y+dy, button)))
		u.down[src] = false
	case opWheel:
		in = append(in, at(now, events.Scroll(x, y, 0, float32(int8(o.arg)))))
	case opKey:
		in = append(in, at(now, withMods(opMods[o.arg>>4&7], events.KeyDown(opKeys[o.arg&15]))))
	case opType:
		return u.finish(t, o, u.typeText(t, now, opTexts[int(o.arg)%len(opTexts)]))
	case opRune:
		in = append(in, at(now, events.Rune(rune(o.arg))))
	case opLeave:
		in = append(in, kui.Event{Time: now, Kind: kui.EventPointerLeave, Source: src})
	case opCancel:
		in = append(in, kui.Event{Time: now, Kind: kui.EventPointerCancel, Source: src})
		u.down[src] = false
	case opPopup:
		u.w.ShowPopup(u.popup, o.arg&1 != 0)
	case opFlags:
		h := u.named[o.target]
		f := u.k.Flags(h) &^ (FlagDisabled | FlagHidden)
		f |= [...]Flags{0, FlagDisabled, FlagHidden}[o.arg%3]
		u.k.SetFlags(h, f)
	case opFocus:
		u.k.SetFocus(u.named[o.target])
	}
	return u.finish(t, o, u.step(t, now, in...))
}

func (u *widgetUI) finish(t testing.TB, o op, evs []NodeEvent) []NodeEvent {
	t.Helper()
	u.log(o.String() + " -> " + u.describe(evs))
	u.painted = u.k.NeedsPaint()
	u.check(t, evs)
	return evs
}

// name returns the name of node h.
func (u *widgetUI) name(h kui.Handle) string {
	for i, n := range u.named {
		if n == h {
			return refNames[i]
		}
	}
	switch h {
	case 0:
		return "none"
	case u.k.Root():
		return "root"
	}
	return "#" + strconv.Itoa(h.Index())
}

// describe formats evs as "[btn:press btn:tap btn:activate]".
func (u *widgetUI) describe(evs []NodeEvent) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, e := range evs {
		if i > 0 {
			b.WriteByte(' ')
		}
		cat(&b, u.name(e.Node), ":", EventKindString(e.Kind))
		if e.Kind == EventChanged && e.Code != 0 {
			cat(&b, "=", strconv.Itoa(int(e.Code)))
		}
	}
	b.WriteByte(']')
	return b.String()
}

func (u *widgetUI) log(s string) {
	u.trace[u.ntrace%traceLen] = s
	u.ntrace++
}

func (u *widgetUI) traceString() string {
	var b strings.Builder
	first := max(u.ntrace-traceLen, 0)
	if first > 0 {
		fmt.Fprintf(&b, "  (%d earlier ops)\n", first)
	}
	for i := first; i < u.ntrace; i++ {
		cat(&b, " ", u.trace[i%traceLen], "\n")
	}
	return b.String()
}

// fatalf fails the test with the trace of the last ops.
func (u *widgetUI) fatalf(t testing.TB, format string, args ...any) {
	t.Helper()
	t.Fatalf("%s: %s\nops:\n%s", u.label, fmt.Sprintf(format, args...), u.traceString())
}

func (u *widgetUI) errorf(t testing.TB, format string, args ...any) {
	t.Helper()
	t.Errorf("%s: %s\nops:\n%s", u.label, fmt.Sprintf(format, args...), u.traceString())
}

// shown reports whether h and its ancestors are valid and not hidden.
func (u *widgetUI) shown(h kui.Handle) bool {
	for ; h != 0; h = u.k.Parent(h) {
		if !u.k.Valid(h) || u.k.Flags(h)&FlagHidden != 0 {
			return false
		}
	}
	return true
}

// enabled reports whether h and its ancestors are not disabled.
func (u *widgetUI) enabled(h kui.Handle) bool {
	for ; h != 0; h = u.k.Parent(h) {
		if u.k.Flags(h)&FlagDisabled != 0 {
			return false
		}
	}
	return true
}

// check fails if the scene or the widgets are in an invalid state after a Step that produced
// evs, painting the scene to check its draw list.
func (u *widgetUI) check(t testing.TB, evs []NodeEvent) {
	t.Helper()
	k, w := u.k, u.w
	u.paint.Clear()
	if err := k.Paint(&u.paint); err != nil {
		u.fatalf(t, "paint: %v", err)
	}
	if msg := sceneProblem(k, &u.paint, evs); msg != "" {
		u.fatalf(t, "%s", msg)
	}
	if f := k.Focus(); f != 0 && (!u.shown(f) || !u.enabled(f)) {
		u.fatalf(t, "focused %s is hidden or disabled", u.name(f))
	}
	s := k.Text(u.entry)
	if len(s) > k.TextCap(u.entry) || k.TextCap(u.entry) != 16 {
		u.fatalf(t, "entry text %q exceeds capacity %d", s, k.TextCap(u.entry))
	}
	if !utf8.ValidString(s) {
		u.fatalf(t, "entry text %q is not UTF-8", s)
	}
	if lo, hi := w.Selection(u.entry); lo < 0 || lo > hi || hi > len(s) || !runeBoundary(s, lo) || !runeBoundary(s, hi) {
		u.fatalf(t, "selection [%d, %d) invalid in %q", lo, hi, s)
	}
	if v := w.SliderValue(u.slider); v < 0 || v > 1 {
		u.fatalf(t, "slider value %v", v)
	}
	if sc, content, box := k.Style(u.scroll).Scroll.Y, k.ContentSize(u.scroll), k.Bounds(u.scroll); sc > 0 || -sc > max(content.H-box.H, 0)+0.01 {
		u.fatalf(t, "scroll %v out of range", sc)
	}
}

// sceneProblem describes what is wrong with scene k after a Step that produced evs and a Paint
// into l, or returns "".
func sceneProblem(k *Scene, l *kui.DrawList, evs []NodeEvent) string {
	depth := 0
	var z int16
	for i, c := range l.Cmds() {
		switch c.Op {
		case kui.DrawClipPush:
			depth++
		case kui.DrawClipPop:
			depth--
		}
		if depth < 0 {
			return "clip pop without push"
		}
		if !c.Node.IsZero() && !k.Valid(c.Node) {
			return fmt.Sprintf("paint command references invalid node %x", c.Node)
		}
		if i > 0 && c.Z < z {
			return "paint commands not in z order"
		}
		z = c.Z
	}
	if depth != 0 {
		return "unbalanced clips"
	}
	if k.NeedsPaint() {
		return "NeedsPaint after Paint"
	}
	for _, e := range evs {
		if !k.Valid(e.Node) && e.Kind != EventFocusOut && e.Kind != gesture.HoverOut {
			return fmt.Sprintf("event %v references invalid node", EventKindString(e.Kind))
		}
	}
	if fo := k.Focus(); fo != 0 && k.Flags(fo)&FlagFocusable == 0 {
		return "focused node is not focusable"
	} else if i, err := k.resolve(fo); err == nil && !k.inModal(i) {
		return "focused node is outside the active modal node"
	}
	return ""
}
