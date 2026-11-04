package kuiscene

import (
	"flag"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/soypat/kui"
	"github.com/soypat/kui/gesture"
)

// Laws are behaviours users expect from any state the UI can reach, not just a fresh one:
// TestWidgetLaws and FuzzWidgets drive the UI into random states with random ops, then check
// every law whose precondition holds there.

var (
	flagSeed = flag.Uint64("seed", 1, "first seed of TestWidgetLaws")
	flagN    = flag.Int("states", 300, "number of random UI states TestWidgetLaws checks laws from")
)

// timeShift is the clock offset of the replica UI checking that absolute time does not matter.
const timeShift = 1e12 + 12345

type law struct {
	name string
	pre  func(u *widgetUI) bool // Whether the law applies to the current state.
	run  func(t testing.TB, u *widgetUI, r *rand.Rand)
}

// focusables are the widgets of widgetUI taking focus outside the popup, in tree order.
var focusables = []string{"btn", "chk", "entry", "slider"}

// plainTexts are the texts of opTexts without control characters.
var plainTexts = []string{"a", "ñ", "word", " world", "two words", "ünïcödé"}

// interactions are the events of a widget the user interacts with.
var interactions = []kui.EventKind{
	gesture.Press, gesture.Tap, gesture.DoubleTap, gesture.SecondaryTap, gesture.LongPress, gesture.DragStart,
	gesture.Drag, gesture.HoverIn, EventActivate, EventChanged, EventFocusIn, kui.EventKeyDown, kui.EventText, kui.EventScroll,
}

const sliderStep = SliderMax / 20 // Arrow key step of sliders without steps.

var laws = []law{
	{
		name: "click activates button",
		pre:  func(u *widgetUI) bool { return u.reachable("btn") },
		run: func(t testing.TB, u *widgetUI, r *rand.Rand) {
			if n := count(u.apply(t, click("btn")), u.btn, EventActivate); n != 1 {
				u.fatalf(t, "click on button activated it %d times", n)
			}
		},
	},
	{
		name: "click focuses widget",
		pre:  func(u *widgetUI) bool { return slices.ContainsFunc(focusables, u.reachable) },
		run: func(t testing.TB, u *widgetUI, r *rand.Rand) {
			name := pick(r, filter(focusables, u.reachable))
			u.apply(t, click(name))
			if f := u.k.Focus(); f != u.named[ref(name)] {
				u.fatalf(t, "click on %s focused %s", name, u.name(f))
			}
		},
	},
	{
		name: "check box clicked twice is unchanged",
		pre:  func(u *widgetUI) bool { return u.reachable("chk") },
		run: func(t testing.TB, u *widgetUI, r *rand.Rand) {
			before := u.w.Checked(u.chk)
			for i, want := range [2]bool{!before, before} {
				e, ok := hasEvent(u.apply(t, click("chk")), u.chk, EventChanged)
				if !ok || u.w.Checked(u.chk) != want || (e.Code == 1) != want {
					u.fatalf(t, "click %d on check box: checked %v, event %v %+v, want %v", i+1, u.w.Checked(u.chk), ok, e, want)
				}
			}
		},
	},
	{
		name: "disabled widget ignores input",
		pre:  func(u *widgetUI) bool { return true },
		run: func(t testing.TB, u *widgetUI, r *rand.Rand) {
			name := pick(r, focusables)
			h := u.named[ref(name)]
			before := u.value(name)
			u.apply(t, disable(name))
			for range 12 {
				o := randOp(r)
				if o.kind == opFlags || o.kind == opPopup {
					o.kind = opWait // Keep the widget disabled.
				}
				for _, e := range u.apply(t, o) {
					if e.Node == h && slices.Contains(interactions, e.Kind) {
						u.fatalf(t, "disabled %s got %v", name, EventKindString(e.Kind))
					}
				}
			}
			if v := u.value(name); v != before {
				u.fatalf(t, "disabled %s changed from %s to %s", name, before, v)
			}
			u.apply(t, enable(name))
		},
	},
	{
		name: "tab cycles through widgets in order",
		pre: func(u *widgetUI) bool {
			return !u.popupOpen() && slices.Contains(u.tabbable(), u.k.Focus())
		},
		run: func(t testing.TB, u *widgetUI, r *rand.Rand) {
			want := u.tabbable()
			n := len(want)
			start := u.k.Focus()
			p := slices.Index(want, start)
			for i := 1; i <= n; i++ {
				u.apply(t, quick(key(kui.KeyTab, 0)))
				if f := u.k.Focus(); f != want[(p+i)%n] {
					u.fatalf(t, "tab %d focused %s, want %s", i, u.name(f), u.name(want[(p+i)%n]))
				}
			}
			for i := 1; i <= n; i++ {
				u.apply(t, quick(key(kui.KeyTab, kui.KeyModShift)))
				if f := u.k.Focus(); f != want[(p-i+n)%n] {
					u.fatalf(t, "shift tab %d focused %s, want %s", i, u.name(f), u.name(want[(p-i+n)%n]))
				}
			}
		},
	},
	{
		name: "typing then backspacing restores entry",
		pre: func(u *widgetUI) bool {
			return u.reachable("entry") && len(u.k.Text(u.entry)) < u.k.TextCap(u.entry)
		},
		run: func(t testing.TB, u *widgetUI, r *rand.Rand) {
			u.apply(t, click("entry"))
			u.apply(t, quick(key(kui.KeyEnd, 0)))
			before := strings.Clone(u.k.Text(u.entry)) // Text aliases the scene's text arena.
			fits := filter(plainTexts, func(s string) bool { return len(before)+len(s) <= u.k.TextCap(u.entry) })
			s := pick(r, fits)
			u.apply(t, quick(typ(s)))
			if got := u.k.Text(u.entry); got != before+s {
				u.fatalf(t, "typing %q after %q gave %q", s, before, got)
			}
			for range utf8.RuneCountInString(s) {
				u.apply(t, quick(key(kui.KeyBackspace, 0)))
			}
			if got := u.k.Text(u.entry); got != before {
				u.fatalf(t, "backspacing %q gave %q, want %q", s, got, before)
			}
		},
	},
	{
		name: "entry edits agree with reference editor",
		pre: func(u *widgetUI) bool {
			return !u.popupOpen() && u.shown(u.entry) && u.enabled(u.entry)
		},
		run: func(t testing.TB, u *widgetUI, r *rand.Rand) {
			if u.k.Focus() != u.entry {
				u.apply(t, focus("entry"))
			}
			keys := []kui.Key{kui.KeyArrowLeft, kui.KeyArrowRight, kui.KeyHome, kui.KeyEnd, kui.KeyBackspace, kui.KeyDelete}
			for range 16 {
				m := u.editor()
				var o op
				var edited bool
				switch c := r.IntN(10); {
				case c < 6:
					k, shift := pick(r, keys), r.IntN(2) == 0
					var mods kui.KeyMods
					if shift {
						mods = kui.KeyModShift
					}
					o, edited = quick(key(k, mods)), m.key(k, shift)
				case c < 7:
					o = quick(key(kui.KeyA, kui.KeyModCtrl))
					m.anchor, m.caret = 0, len(m.s)
				default:
					s := pick(r, plainTexts)
					o, edited = quick(typ(s)), m.typ(s)
				}
				evs := u.apply(t, o)
				if got := u.editor(); got != m {
					u.fatalf(t, "%v: entry %+v, reference editor %+v", o, got, m)
				}
				if changed := count(evs, u.entry, EventChanged) > 0; changed != edited {
					u.fatalf(t, "%v: EventChanged %v, reference editor edited %v", o, changed, edited)
				}
			}
		},
	},
	{
		name: "slider arrows are reversible",
		pre: func(u *widgetUI) bool {
			v := u.sliderRaw()
			return !u.popupOpen() && u.shown(u.slider) && u.enabled(u.slider) && v >= sliderStep && v <= SliderMax-sliderStep
		},
		run: func(t testing.TB, u *widgetUI, r *rand.Rand) {
			if u.k.Focus() != u.slider {
				u.apply(t, focus("slider"))
			}
			v0 := u.sliderRaw()
			up, down := pick(r, []kui.Key{kui.KeyArrowRight, kui.KeyArrowUp}), pick(r, []kui.Key{kui.KeyArrowLeft, kui.KeyArrowDown})
			e, ok := hasEvent(u.apply(t, quick(key(up, 0))), u.slider, EventChanged)
			if v := u.sliderRaw(); v != v0+sliderStep || !ok || e.Code != uint32(v) {
				u.fatalf(t, "%v from %d: value %d, event %v %+v", up, v0, v, ok, e)
			}
			u.apply(t, quick(key(down, 0)))
			if v := u.sliderRaw(); v != v0 {
				u.fatalf(t, "%v then %v from %d gave %d", up, down, v0, v)
			}
		},
	},
	{
		name: "wheel scroll is reversible",
		pre: func(u *widgetUI) bool {
			down, upRoom := u.scrollRoom()
			x, y := u.point(wheel("scroll", 0))
			return !u.popupOpen() && max(down, upRoom) >= 1 && u.k.HitTest(kui.Point{X: x, Y: y}, gesture.SenseScroll) == u.scroll
		},
		run: func(t testing.TB, u *widgetUI, r *rand.Rand) {
			down, upRoom := u.scrollRoom()
			d := 1 + r.IntN(int(min(max(down, upRoom), 100)))
			if down < float32(d) {
				d = -d
			}
			y0 := u.k.Style(u.scroll).Scroll.Y
			u.apply(t, quick(wheel("scroll", int8(d))))
			near := func(a, b float32) bool { return a-b < 1e-3 && b-a < 1e-3 } // Offsets may be fractional.
			if y := u.k.Style(u.scroll).Scroll.Y; !near(y, y0-float32(d)) {
				u.fatalf(t, "wheel %+d from %v scrolled to %v", d, y0, y)
			}
			u.apply(t, quick(wheel("scroll", int8(-d))))
			if y := u.k.Style(u.scroll).Scroll.Y; !near(y, y0) {
				u.fatalf(t, "wheel %+d and back from %v scrolled to %v", d, y0, y)
			}
		},
	},
	{
		name: "escape closes popup",
		pre:  func(u *widgetUI) bool { return u.canPopup() },
		run: func(t testing.TB, u *widgetUI, r *rand.Rand) {
			u.apply(t, showPopup(true))
			if !u.popupOpen() {
				u.fatalf(t, "popup did not show")
			}
			evs := u.apply(t, quick(key(kui.KeyEscape, 0)))
			if count(evs, u.popup, EventDismiss) != 1 || u.popupOpen() {
				u.fatalf(t, "escape did not close popup")
			}
		},
	},
	{
		name: "popup item click keeps popup and outside click closes it",
		pre: func(u *widgetUI) bool {
			return u.canPopup() && u.k.Flags(u.item)&(FlagHidden|FlagDisabled) == 0
		},
		run: func(t testing.TB, u *widgetUI, r *rand.Rand) {
			u.apply(t, showPopup(true))
			if n := count(u.apply(t, click("item")), u.item, EventActivate); n != 1 || !u.popupOpen() {
				u.fatalf(t, "popup item click: %d activations, popup open %v", n, u.popupOpen())
			}
			pb := u.k.Bounds(u.popup)
			outside := filter([]string{"entry", "slider", "chk"}, func(name string) bool {
				x, y := u.point(click(name))
				return u.shown(u.named[ref(name)]) && !pb.Contains(kui.Point{X: x, Y: y})
			})
			if len(outside) == 0 {
				return
			}
			name := pick(r, outside)
			h := u.named[ref(name)]
			evs := u.apply(t, click(name))
			if count(evs, u.popup, EventDismiss) != 1 || u.popupOpen() {
				u.fatalf(t, "click on %s outside popup did not close it", name)
			}
			if _, ok := hasEvent(evs, h, gesture.Press); ok || u.k.Focus() == h {
				u.fatalf(t, "click outside popup reached %s", name)
			}
		},
	},
	{
		name: "idle UI stays quiet",
		pre: func(u *widgetUI) bool {
			_, pending := u.k.NextDeadline()
			return u.idle() && !pending
		},
		run: func(t testing.TB, u *widgetUI, r *rand.Rand) {
			if evs := u.apply(t, wait(2500*ms)); len(evs) != 0 || u.painted {
				u.fatalf(t, "idle step: events %s, repaint %v", u.describe(evs), u.painted)
			}
		},
	},
}

// checkLaws drives a UI with the ops of data, checking that a replica with its clock shifted
// behaves the same, then checks every law that applies, counting them in ran and the events
// seen in seen if not nil.
func checkLaws(t testing.TB, label string, data []byte, r *rand.Rand, ran []int, seen map[kui.EventKind]int) {
	u, shifted := newWidgetUI(t), newWidgetUI(t)
	u.label, shifted.label = label, label+" (clock shifted)"
	shifted.now = timeShift
	for _, o := range decodeOps(data) {
		evs := u.apply(t, o)
		if a, b := u.describe(evs), shifted.describe(shifted.apply(t, o)); a != b {
			u.fatalf(t, "shifting the clock changed the events of %v to %s", o, b)
		}
		if seen != nil {
			for _, e := range evs {
				seen[e.Kind]++
			}
		}
	}
	for i, l := range laws {
		if l.pre(u) {
			u.log("--- law: " + l.name)
			l.run(t, u, r)
			if ran != nil {
				ran[i]++
			}
		}
	}
}

func TestWidgetLaws(t *testing.T) {
	ran := make([]int, len(laws))
	seen := make(map[kui.EventKind]int)
	for i := range *flagN {
		seed := *flagSeed + uint64(i)
		r := rand.New(rand.NewPCG(seed, 0))
		data := make([]byte, opBytes*r.IntN(61))
		for j := 0; j < len(data); j += opBytes {
			o := data[j : j+opBytes]
			if j > 0 && r.IntN(6) == 0 {
				// Repeat the previous op quickly, as in double clicks and key repeat.
				copy(o, data[j-opBytes:j])
				o[4] = byte(r.IntN(25))
				continue
			}
			for k := range o {
				o[k] = byte(r.Uint32())
			}
		}
		checkLaws(t, fmt.Sprintf("seed %d", seed), data, r, ran, seen)
	}
	// Guard the generator: random states must reach every law and every widget event.
	for i, l := range laws {
		if ran[i] == 0 {
			t.Errorf("law %q never applied", l.name)
		}
		t.Logf("%4d %s", ran[i], l.name)
	}
	for _, k := range []kui.EventKind{EventActivate, EventChanged, EventDismiss, EventFocusIn, EventFocusOut, EventTimer, gesture.Tap, gesture.DoubleTap, gesture.DragStart, gesture.HoverIn} {
		if seen[k] == 0 {
			t.Errorf("random ops never produced %v", EventKindString(k))
		}
	}
}

// FuzzWidgets drives the widgets with fuzzed ops, checking invariants after every op and the
// laws that apply to the state reached. Behaviour table rows seed it.
func FuzzWidgets(f *testing.F) {
	for _, b := range behaviours {
		f.Add(encodeOps(slices.Concat(b.given, b.when)))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		h := fnv.New64a()
		h.Write(data)
		checkLaws(t, "fuzz", data, rand.New(rand.NewPCG(h.Sum64(), 0)), nil, nil)
	})
}

func TestOpsRoundTrip(t *testing.T) {
	for _, b := range behaviours {
		o := slices.Concat(b.given, b.when)
		if got := decodeOps(encodeOps(o)); !slices.Equal(got, o) {
			t.Errorf("%s: ops %v decode to %v", b.name, o, got)
		}
	}
	if s := (op{kind: opClick, target: ref("btn"), x: 128, y: 128, arg: argShift | argSecondary, dt: humanDT}).String(); s != "shift+rclick btn +400ms" {
		t.Error("op string:", s)
	}
	if s := key(kui.KeyArrowLeft, kui.KeyModCtrl|kui.KeyModShift).String(); s != "key ctrl+shift+left +400ms" {
		t.Error("key op string:", s)
	}
}

// Helpers of laws.

func pick[T any](r *rand.Rand, s []T) T { return s[r.IntN(len(s))] }

func filter[T any](s []T, keep func(T) bool) []T {
	var out []T
	for _, v := range s {
		if keep(v) {
			out = append(out, v)
		}
	}
	return out
}

func randOp(r *rand.Rand) op {
	var b [opBytes]byte
	for i := range b {
		b[i] = byte(r.Uint32())
	}
	return decodeOps(b[:])[0]
}

// idle reports whether no pointer is held down.
func (u *widgetUI) idle() bool { return !u.down[0] && !u.down[1] }

func (u *widgetUI) popupOpen() bool { return u.shown(u.popup) }

// canPopup reports whether the popup can be shown and used.
func (u *widgetUI) canPopup() bool {
	return u.idle() && !u.popupOpen() && u.shown(u.btn) && u.enabled(u.popup)
}

// reachable reports whether a click on the named widget's center reaches it: no pointer is
// held, no popup blocks input and the widget is shown, enabled and topmost there.
func (u *widgetUI) reachable(name string) bool {
	h := u.named[ref(name)]
	x, y := u.point(click(name))
	return u.idle() && !u.popupOpen() && u.shown(h) && u.enabled(h) &&
		u.k.HitTest(kui.Point{X: x, Y: y}, gesture.SensePress) == h
}

// tabbable returns the widgets Tab can focus outside the popup, in order.
func (u *widgetUI) tabbable() []kui.Handle {
	var hs []kui.Handle
	for _, name := range focusables {
		if h := u.named[ref(name)]; u.shown(h) && u.enabled(h) {
			hs = append(hs, h)
		}
	}
	return hs
}

// value returns the value of the named widget a user can change.
func (u *widgetUI) value(name string) string {
	switch name {
	case "chk":
		return fmt.Sprint(u.w.Checked(u.chk))
	case "entry":
		return strings.Clone(u.k.Text(u.entry))
	case "slider":
		return fmt.Sprint(u.sliderRaw())
	}
	return ""
}

func (u *widgetUI) sliderRaw() int { return int(u.k.State(u.slider) >> sliderValueShift) }

// scrollRoom returns how far the scroll container can scroll down and up.
func (u *widgetUI) scrollRoom() (down, up float32) {
	overflow := u.k.ContentSize(u.scroll).H - u.k.Bounds(u.scroll).H
	off := -u.k.Style(u.scroll).Scroll.Y
	return max(overflow-off, 0), off
}

// editor returns the editing state of the entry.
func (u *widgetUI) editor() editModel {
	return editModel{s: strings.Clone(u.k.Text(u.entry)), caret: u.w.edit.caret, anchor: u.w.edit.anchor, cap: u.k.TextCap(u.entry)}
}

// editModel is a reference single line editor: what users expect of an entry's keys.
type editModel struct {
	s             string
	caret, anchor int
	cap           int
}

func (m *editModel) sel() (lo, hi int) { return min(m.caret, m.anchor), max(m.caret, m.anchor) }

// key applies key k, extending the selection with shift. It reports whether the text was edited.
func (m *editModel) key(k kui.Key, shift bool) bool {
	lo, hi := m.sel()
	move := func(to int) {
		m.caret = to
		if !shift {
			m.anchor = to
		}
	}
	prev := func(i int) int { _, n := utf8.DecodeLastRuneInString(m.s[:i]); return i - n }
	next := func(i int) int { _, n := utf8.DecodeRuneInString(m.s[i:]); return i + n }
	switch k {
	case kui.KeyArrowLeft:
		if lo < hi && !shift {
			move(lo) // Collapse the selection to its start.
		} else {
			move(prev(m.caret))
		}
	case kui.KeyArrowRight:
		if lo < hi && !shift {
			move(hi)
		} else {
			move(next(m.caret))
		}
	case kui.KeyHome:
		move(0)
	case kui.KeyEnd:
		move(len(m.s))
	case kui.KeyBackspace:
		if lo == hi {
			lo = prev(lo)
		}
		return m.replace(lo, hi, "")
	case kui.KeyDelete:
		if lo == hi {
			hi = next(hi)
		}
		return m.replace(lo, hi, "")
	}
	return false
}

// typ replaces the selection with s.
func (m *editModel) typ(s string) bool {
	lo, hi := m.sel()
	return m.replace(lo, hi, s)
}

// replace replaces m.s[lo:hi] with as many runes of s as fit the capacity.
func (m *editModel) replace(lo, hi int, s string) bool {
	for len(m.s)-(hi-lo)+len(s) > m.cap {
		_, n := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-n]
	}
	if lo == hi && s == "" {
		return false
	}
	m.s = m.s[:lo] + s + m.s[hi:]
	m.caret = lo + len(s)
	m.anchor = m.caret
	return true
}
