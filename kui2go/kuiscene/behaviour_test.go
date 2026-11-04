package kuiscene

import (
	"fmt"
	"testing"

	"github.com/soypat/kui"
	"github.com/soypat/kui/gesture"
)

// behaviour is what a user expects of the widgets: given a state reached with some ops, when
// they perform more ops, then the expectations hold. Rows come from the widgets' documentation
// and from what users of other toolkits are used to.
type behaviour struct {
	name  string
	given []op
	when  []op
	then  []expect
}

// expect checks the UI after the when ops, which produced evs. It returns "" or what is wrong.
type expect func(u *widgetUI, evs []NodeEvent) string

func ops(o ...op) []op { return o }

const time1s = 1000 * ms

func then(e ...expect) []expect { return e }

var behaviours = []behaviour{
	// Buttons emit EventActivate when tapped or when Enter or Space is pressed while focused.
	{name: "click activates button", when: ops(click("btn")), then: then(emitsN("btn", EventActivate, 1))},
	{name: "two quick clicks activate button twice", when: ops(click("btn"), quick(click("btn"))), then: then(emitsN("btn", EventActivate, 2))},
	{name: "enter activates focused button", given: ops(focus("btn")), when: ops(key(kui.KeyEnter, 0)), then: then(emitsN("btn", EventActivate, 1))},
	{name: "space activates focused button", given: ops(focus("btn")), when: ops(key(kui.KeySpace, 0)), then: then(emitsN("btn", EventActivate, 1))},
	{name: "enter does nothing to unfocused button", when: ops(key(kui.KeyEnter, 0)), then: then(silent("btn"))},
	{name: "right click does not activate button", when: ops(rclick("btn")), then: then(emitsN("btn", EventActivate, 0))},
	{name: "sliding off button before release cancels click", when: ops(press("btn"), quick(hover("lbl")), quick(release("lbl"))), then: then(emitsN("btn", EventActivate, 0))},
	{name: "press and release on button without moving activates", when: ops(press("btn"), quick(release("btn"))), then: then(emitsN("btn", EventActivate, 1))},
	{name: "disabled button ignores click", given: ops(disable("btn")), when: ops(click("btn")), then: then(silent("btn"))},
	{name: "disabling focused button drops focus and keys", given: ops(focus("btn"), disable("btn")), when: ops(key(kui.KeyEnter, 0)), then: then(focused(""), silent("btn"))},
	{name: "re-enabled button works again", given: ops(disable("btn"), enable("btn")), when: ops(click("btn")), then: then(emitsN("btn", EventActivate, 1))},
	{name: "hidden button cannot be clicked where it was", given: ops(hide("btn")), when: ops(click("btn")), then: then(silent("btn"))},
	{name: "hovering button highlights it", when: ops(hover("btn")), then: then(emits("btn", gesture.HoverIn), stateHas("btn", stateHovered))},

	// Check boxes emit EventChanged with Code 1 when checked and 0 when unchecked, when tapped or
	// when Space is pressed while focused.
	{name: "click checks check box", when: ops(click("chk")), then: then(emitsCode("chk", EventChanged, 1), checked(true))},
	{name: "clicking check box label text toggles it", when: ops(clickAt("chk", 0.9, 0.5)), then: then(checked(true))},
	{name: "second click unchecks check box", given: ops(click("chk")), when: ops(click("chk")), then: then(emitsCode("chk", EventChanged, 0), checked(false))},
	{name: "space toggles focused check box", given: ops(focus("chk")), when: ops(key(kui.KeySpace, 0)), then: then(emitsCode("chk", EventChanged, 1), checked(true))},
	{name: "enter does not toggle check box", given: ops(focus("chk")), when: ops(key(kui.KeyEnter, 0)), then: then(emitsN("chk", EventChanged, 0), checked(false))},
	{name: "disabled check box keeps its state", given: ops(disable("chk")), when: ops(click("chk")), then: then(silent("chk"), checked(false))},

	// Focus.
	{name: "click focuses button", when: ops(click("btn")), then: then(focused("btn"), emits("btn", EventFocusIn))},
	{name: "click focuses entry", when: ops(click("entry")), then: then(focused("entry"))},
	{name: "click focuses slider", when: ops(click("slider")), then: then(focused("slider"))},
	{name: "click on label clears focus", given: ops(click("btn")), when: ops(click("lbl")), then: then(focused(""), emits("btn", EventFocusOut))},
	{name: "click on empty space clears focus", given: ops(click("btn")), when: ops(clickAt("scroll", 0.98, 0.98)), then: then(focused(""))},
	{name: "tab focuses first widget", when: ops(key(kui.KeyTab, 0)), then: then(focused("btn"))},
	{name: "tab moves focus to next widget", given: ops(focus("btn")), when: ops(key(kui.KeyTab, 0)), then: then(focused("chk"))},
	{name: "tab moves focus out of entry", given: ops(focus("entry")), when: ops(key(kui.KeyTab, 0)), then: then(focused("slider"), text("entry", "hello"))},
	{name: "tab wraps to first widget", given: ops(focus("slider")), when: ops(key(kui.KeyTab, 0)), then: then(focused("btn"))},
	{name: "shift tab moves focus back", given: ops(focus("chk")), when: ops(key(kui.KeyTab, kui.KeyModShift)), then: then(focused("btn"))},
	{name: "shift tab wraps to last widget", given: ops(focus("btn")), when: ops(key(kui.KeyTab, kui.KeyModShift)), then: then(focused("slider"))},
	{name: "tab skips disabled widget", given: ops(focus("btn"), disable("chk")), when: ops(key(kui.KeyTab, 0)), then: then(focused("entry"))},
	{name: "tab skips hidden widget", given: ops(focus("btn"), hide("chk")), when: ops(key(kui.KeyTab, 0)), then: then(focused("entry"))},
	{name: "hidden popup item cannot be focused", given: ops(focus("item")), when: ops(wait(10 * ms)), then: then(focused(""))},

	// Entries emit EventChanged when the user changes their text and EventActivate on Enter.
	{name: "focus puts caret at end and typing appends", given: ops(focus("entry")), when: ops(typ("word")), then: then(text("entry", "helloword"), emits("entry", EventChanged), selection(9, 9))},
	{name: "caret shows when entry is focused from code after idling", given: ops(wait(2500 * ms)), when: ops(after(time1s, focus("entry")), wait(10*ms)), then: then(stateHas("entry", stateCaret), emitsN("entry", EventTimer, 0))},
	{name: "typing into unfocused entry does nothing", when: ops(typ("word")), then: then(text("entry", "hello"), silent("entry"))},
	{name: "typing multibyte text", given: ops(focus("entry")), when: ops(typ("ünïcödé")), then: then(text("entry", "helloünïcödé"))},
	{name: "control characters are not inserted", given: ops(focus("entry")), when: ops(typ("\x01ctl\n")), then: then(text("entry", "helloctl"))},
	{name: "single control rune is ignored", given: ops(focus("entry")), when: ops(op{kind: opRune, arg: 0x08, dt: humanDT}), then: then(text("entry", "hello"), emitsN("entry", EventChanged, 0))},
	{name: "backspace deletes previous rune", given: ops(focus("entry"), typ("ñ")), when: ops(key(kui.KeyBackspace, 0)), then: then(text("entry", "hello"), emits("entry", EventChanged))},
	{name: "backspace at start does nothing", given: ops(focus("entry"), key(kui.KeyHome, 0)), when: ops(key(kui.KeyBackspace, 0)), then: then(text("entry", "hello"), emitsN("entry", EventChanged, 0))},
	{name: "delete at end does nothing", given: ops(focus("entry")), when: ops(key(kui.KeyDelete, 0)), then: then(text("entry", "hello"), emitsN("entry", EventChanged, 0))},
	{name: "home then delete removes first rune", given: ops(focus("entry"), key(kui.KeyHome, 0)), when: ops(key(kui.KeyDelete, 0)), then: then(text("entry", "ello"))},
	{name: "left arrow then typing inserts before last rune", given: ops(focus("entry"), key(kui.KeyArrowLeft, 0)), when: ops(typ("a")), then: then(text("entry", "hellao"))},
	{name: "right arrow at end stays", given: ops(focus("entry")), when: ops(key(kui.KeyArrowRight, 0)), then: then(selection(5, 5))},
	{name: "select all and type replaces text", given: ops(focus("entry"), key(kui.KeyA, kui.KeyModCtrl)), when: ops(typ("word")), then: then(text("entry", "word"))},
	{name: "select all with command key", given: ops(focus("entry")), when: ops(key(kui.KeyA, kui.KeyModSuper)), then: then(selection(0, 5))},
	{name: "shift home selects to start", given: ops(focus("entry")), when: ops(key(kui.KeyHome, kui.KeyModShift)), then: then(selection(0, 5))},
	{name: "backspace deletes selection", given: ops(focus("entry"), key(kui.KeyHome, kui.KeyModShift)), when: ops(key(kui.KeyBackspace, 0)), then: then(text("entry", ""))},
	{name: "left arrow collapses selection to its start", given: ops(focus("entry"), key(kui.KeyA, kui.KeyModCtrl)), when: ops(key(kui.KeyArrowLeft, 0)), then: then(selection(0, 0))},
	{name: "right arrow collapses selection to its end", given: ops(focus("entry"), key(kui.KeyHome, 0), key(kui.KeyEnd, kui.KeyModShift)), when: ops(key(kui.KeyArrowRight, 0)), then: then(selection(5, 5))},
	{name: "ctrl backspace deletes previous word", given: ops(focus("entry"), typ(" world")), when: ops(key(kui.KeyBackspace, kui.KeyModCtrl)), then: then(text("entry", "hello "))},
	{name: "ctrl left jumps to word start", given: ops(focus("entry"), typ(" world")), when: ops(key(kui.KeyArrowLeft, kui.KeyModCtrl)), then: then(selection(6, 6))},
	{name: "text beyond capacity is truncated at a rune", given: ops(focus("entry")), when: ops(typ("1234567890xñ")), then: then(text("entry", "hello1234567890x"))},
	{name: "full entry ignores typing", given: ops(focus("entry"), typ("1234567890xñ")), when: ops(typ("a")), then: then(text("entry", "hello1234567890x"), emitsN("entry", EventChanged, 0))},
	{name: "enter activates entry and keeps text", given: ops(focus("entry")), when: ops(key(kui.KeyEnter, 0)), then: then(emits("entry", EventActivate), text("entry", "hello"))},
	{name: "copy requests clipboard write of selection", given: ops(focus("entry"), key(kui.KeyA, kui.KeyModCtrl)), when: ops(key(kui.KeyC, kui.KeyModCtrl)), then: then(requested(kui.RequestClipboardWrite, "hello"), text("entry", "hello"))},
	{name: "copy without selection requests nothing", given: ops(focus("entry")), when: ops(key(kui.KeyC, kui.KeyModCtrl)), then: then(noRequest(kui.RequestClipboardWrite))},
	{name: "cut removes selection and requests clipboard write", given: ops(focus("entry"), key(kui.KeyA, kui.KeyModCtrl)), when: ops(key(kui.KeyX, kui.KeyModCtrl)), then: then(requested(kui.RequestClipboardWrite, "hello"), text("entry", ""))},
	{name: "paste requests clipboard read", given: ops(focus("entry")), when: ops(key(kui.KeyV, kui.KeyModCtrl)), then: then(requested(kui.RequestClipboardRead, ""))},
	{name: "focusing entry requests text input", when: ops(click("entry")), then: then(requested(kui.RequestTextInput, ""))},
	{name: "leaving entry ends editing", given: ops(focus("entry"), key(kui.KeyA, kui.KeyModCtrl)), when: ops(click("lbl")), then: then(focused(""), selection(0, 0), text("entry", "hello"))},
	{name: "double click selects word", given: ops(focus("entry"), typ(" world")), when: ops(clickAt("entry", 0.1, 0.5), quick(clickAt("entry", 0.1, 0.5))), then: then(selection(0, 5))},
	{name: "drag selects text", given: ops(focus("entry")), when: ops(dragFrom("entry", 0.15, 0.5, -30, 0)), then: then(selectionNonEmpty())},

	// Sliders emit EventChanged when the user changes their value. Arrow keys change it by 1/20,
	// Home and End go to the ends.
	{name: "click at slider start sets minimum", when: ops(clickAt("slider", 0, 0.5)), then: then(sliderAt(0), emitsCode("slider", EventChanged, 0))},
	{name: "click at slider end sets maximum", when: ops(clickAt("slider", 1, 0.5)), then: then(sliderAt(1), emitsCode("slider", EventChanged, SliderMax))},
	{name: "click at slider middle keeps value", when: ops(click("slider")), then: then(sliderNear(0.5, 0.01))},
	{name: "dragging slider right raises value", when: ops(drag("slider", 40, 0)), then: then(sliderAbove(0.55))},
	{name: "dragging slider past end clamps", when: ops(drag("slider", 126, 0)), then: then(sliderAt(1))},
	{name: "dragging slider vertically keeps value", when: ops(drag("slider", 0, 60)), then: then(sliderNear(0.5, 0.01))},
	{name: "right arrow raises focused slider", given: ops(focus("slider")), when: ops(key(kui.KeyArrowRight, 0)), then: then(sliderNear(0.55, 0.001), emits("slider", EventChanged))},
	{name: "up arrow raises focused slider", given: ops(focus("slider")), when: ops(key(kui.KeyArrowUp, 0)), then: then(sliderNear(0.55, 0.001))},
	{name: "left arrow lowers focused slider", given: ops(focus("slider")), when: ops(key(kui.KeyArrowLeft, 0)), then: then(sliderNear(0.45, 0.001))},
	{name: "home and end go to slider ends", given: ops(focus("slider"), key(kui.KeyHome, 0)), when: ops(key(kui.KeyEnd, 0)), then: then(sliderAt(1))},
	{name: "left at minimum changes nothing", given: ops(focus("slider"), key(kui.KeyHome, 0)), when: ops(key(kui.KeyArrowLeft, 0)), then: then(sliderAt(0), emitsN("slider", EventChanged, 0))},
	{name: "arrows do not move unfocused slider", when: ops(key(kui.KeyArrowRight, 0)), then: then(sliderNear(0.5, 0.01), silent("slider"))},
	{name: "disabling widget mid-drag stops the drag", given: ops(press("slider"), quick(hover("lbl")), disable("slider")), when: ops(quick(hover("chk"))), then: then(emitsN("slider", gesture.Drag, 0), emitsN("slider", EventChanged, 0))},
	{name: "disabled slider ignores drag", given: ops(disable("slider")), when: ops(drag("slider", 40, 0)), then: then(sliderNear(0.5, 0.01), silent("slider"))},

	// Scroll containers scroll with the wheel, by dragging their content or their scroll bar.
	{name: "wheel scrolls content", when: ops(wheel("scroll", 20)), then: then(scrollAt(-20))},
	{name: "wheel over an item scrolls its container", when: ops(wheel("row", 20)), then: then(scrollAt(-20))},
	{name: "wheel back returns", given: ops(wheel("scroll", 30)), when: ops(wheel("scroll", -30)), then: then(scrollAt(0))},
	{name: "wheel stops at top", when: ops(wheel("scroll", -50)), then: then(scrollAt(0))},
	{name: "wheel stops at bottom", when: ops(wheel("scroll", 127), wheel("scroll", 127), wheel("scroll", 127), wheel("scroll", 127)), then: then(scrolledToEnd())},
	{name: "wheel outside scroll container does nothing", when: ops(wheel("btn", 20)), then: then(scrollAt(0))},
	{name: "dragging content up scrolls down", given: ops(wheel("scroll", 40)), when: ops(drag("row", 0, -16)), then: then(scrollAt(-56))},
	{name: "dragging content down at top stays", when: ops(drag("row", 0, 30)), then: then(scrollAt(0))},
	{name: "scroll stays in range when its container grows", given: ops(wheel("scroll", 127), wheel("scroll", 127), wheel("scroll", 127)), when: ops(hide("btn")), then: then(scrolledToEnd())},
	{name: "dragging scroll bar down scrolls down", when: ops(dragThumb(20)), then: then(scrollBelow(-20))},

	// Popups block input outside them; pressing outside or Escape hides them.
	{name: "escape closes popup", given: ops(showPopup(true)), when: ops(key(kui.KeyEscape, 0)), then: then(shown("popup", false), emits("popup", EventDismiss))},
	{name: "click outside closes popup and is swallowed", given: ops(showPopup(true)), when: ops(click("entry")), then: then(shown("popup", false), emits("popup", EventDismiss), silent("entry"), focused(""))},
	{name: "click on popup item activates it and keeps popup", given: ops(showPopup(true)), when: ops(click("item")), then: then(emitsN("item", EventActivate, 1), shown("popup", true))},
	{name: "click on popup background keeps popup", given: ops(showPopup(true)), when: ops(clickAt("popup", 0.02, 0.02)), then: then(shown("popup", true), emitsN("popup", EventDismiss, 0))},
	{name: "tab stays inside open popup", given: ops(showPopup(true)), when: ops(key(kui.KeyTab, 0), key(kui.KeyTab, 0)), then: then(focused("item"))},
	{name: "keys do not reach widgets behind popup", given: ops(focus("chk"), showPopup(true)), when: ops(key(kui.KeySpace, 0)), then: then(checked(false))},
	{name: "popup shows below its button", given: ops(showPopup(true)), then: then(below("popup", "btn"))},
	{name: "disabling popup's button disables popup items", given: ops(showPopup(true), disable("btn")), when: ops(click("item")), then: then(silent("item"))},
	{name: "disabling popup's button drops focus inside popup", given: ops(showPopup(true), key(kui.KeyTab, 0), disable("btn")), when: ops(key(kui.KeyEnter, 0)), then: then(focused(""), silent("item"))},

	// Tooltips show after the pointer rests over their widget for the tooltip delay.
	{name: "tooltip hidden right after hover", when: ops(hover("btn")), then: then(shown("tip", false))},
	{name: "tooltip shows after resting", when: ops(hover("btn"), wait(700*ms)), then: then(shown("tip", true), below("tip", "btn"))},
	{name: "tooltip hides on leaving widget", given: ops(hover("btn"), wait(700*ms)), when: ops(hover("lbl")), then: then(shown("tip", false))},
	{name: "tooltip hides on press", given: ops(hover("btn"), wait(700*ms)), when: ops(press("btn")), then: then(shown("tip", false))},
	{name: "tooltip does not show after leaving early", when: ops(hover("btn"), quick(hover("lbl")), wait(700*ms)), then: then(shown("tip", false))},

	// Idle UIs are quiet so hosts can sleep.
	{name: "idle UI emits nothing", when: ops(wait(2500 * ms)), then: then(noEvents(), noRepaint())},
	{name: "hovering a label emits nothing", when: ops(hover("lbl")), then: then(noEvents())},
}

func TestBehaviour(t *testing.T) {
	for _, b := range behaviours {
		t.Run(b.name, func(t *testing.T) {
			u := newWidgetUI(t)
			u.label = b.name
			for _, o := range b.given {
				u.apply(t, o)
			}
			u.log("--- when")
			var evs []NodeEvent
			for _, o := range b.when {
				evs = append(evs, u.apply(t, o)...)
			}
			for _, e := range b.then {
				if msg := e(u, evs); msg != "" {
					u.errorf(t, "%s", msg)
				}
			}
		})
	}
}

// Expectations.

func count(evs []NodeEvent, n kui.Handle, kind kui.EventKind) int {
	c := 0
	for _, e := range evs {
		if e.Node == n && e.Kind == kind {
			c++
		}
	}
	return c
}

// emits expects at least one event of kind for the named widget.
func emits(name string, kind kui.EventKind) expect {
	return func(u *widgetUI, evs []NodeEvent) string {
		if count(evs, u.named[ref(name)], kind) == 0 {
			return fmt.Sprintf("want %s:%s in %s", name, EventKindString(kind), u.describe(evs))
		}
		return ""
	}
}

// emitsN expects exactly n events of kind for the named widget.
func emitsN(name string, kind kui.EventKind, n int) expect {
	return func(u *widgetUI, evs []NodeEvent) string {
		if c := count(evs, u.named[ref(name)], kind); c != n {
			return fmt.Sprintf("want %d %s:%s, got %d in %s", n, name, EventKindString(kind), c, u.describe(evs))
		}
		return ""
	}
}

// emitsCode expects the last event of kind for the named widget to have code.
func emitsCode(name string, kind kui.EventKind, code uint32) expect {
	return func(u *widgetUI, evs []NodeEvent) string {
		h := u.named[ref(name)]
		for i := len(evs) - 1; i >= 0; i-- {
			if e := evs[i]; e.Node == h && e.Kind == kind {
				if e.Code != code {
					return fmt.Sprintf("want %s:%s code %d, got %d", name, EventKindString(kind), code, e.Code)
				}
				return ""
			}
		}
		return fmt.Sprintf("want %s:%s in %s", name, EventKindString(kind), u.describe(evs))
	}
}

// silent expects no events for the named widget.
func silent(name string) expect {
	return func(u *widgetUI, evs []NodeEvent) string {
		h := u.named[ref(name)]
		for _, e := range evs {
			if e.Node == h {
				return fmt.Sprintf("want no events for %s, got %s", name, u.describe(evs))
			}
		}
		return ""
	}
}

func noEvents() expect {
	return func(u *widgetUI, evs []NodeEvent) string {
		if len(evs) != 0 {
			return "want no events, got " + u.describe(evs)
		}
		return ""
	}
}

// noRepaint expects the last op to not have needed a repaint.
func noRepaint() expect {
	return func(u *widgetUI, evs []NodeEvent) string {
		if u.painted {
			return "idle step needed a repaint"
		}
		return ""
	}
}

// focused expects the named widget, or nothing for "", to have focus.
func focused(name string) expect {
	return func(u *widgetUI, evs []NodeEvent) string {
		var want kui.Handle
		if name != "" {
			want = u.named[ref(name)]
		}
		if got := u.k.Focus(); got != want {
			return fmt.Sprintf("want focus on %s, got %s", u.name(want), u.name(got))
		}
		return ""
	}
}

func text(name, s string) expect {
	return func(u *widgetUI, evs []NodeEvent) string {
		if got := u.k.Text(u.named[ref(name)]); got != s {
			return fmt.Sprintf("want %s text %q, got %q", name, s, got)
		}
		return ""
	}
}

func selection(lo, hi int) expect {
	return func(u *widgetUI, evs []NodeEvent) string {
		if l, h := u.w.Selection(u.entry); l != lo || h != hi {
			return fmt.Sprintf("want selection [%d, %d), got [%d, %d) of %q", lo, hi, l, h, u.k.Text(u.entry))
		}
		return ""
	}
}

func selectionNonEmpty() expect {
	return func(u *widgetUI, evs []NodeEvent) string {
		if l, h := u.w.Selection(u.entry); l >= h {
			return fmt.Sprintf("want a selection, got [%d, %d)", l, h)
		}
		return ""
	}
}

func checked(want bool) expect {
	return func(u *widgetUI, evs []NodeEvent) string {
		if got := u.w.Checked(u.chk); got != want {
			return fmt.Sprintf("want checked %v, got %v", want, got)
		}
		return ""
	}
}

func stateHas(name string, bits uint32) expect {
	return func(u *widgetUI, evs []NodeEvent) string {
		if st := u.k.State(u.named[ref(name)]); st&bits != bits {
			return fmt.Sprintf("want %s state bits %b, got %b", name, bits, st)
		}
		return ""
	}
}

// shown expects the named widget to be shown, its ancestors included, or not.
func shown(name string, want bool) expect {
	return func(u *widgetUI, evs []NodeEvent) string {
		if got := u.shown(u.named[ref(name)]); got != want {
			return fmt.Sprintf("want %s shown %v, got %v", name, want, got)
		}
		return ""
	}
}

// below expects the named widget to be laid out below another.
func below(name, other string) expect {
	return func(u *widgetUI, evs []NodeEvent) string {
		u.k.Layout()
		a, b := u.k.Bounds(u.named[ref(name)]), u.k.Bounds(u.named[ref(other)])
		if a.Y < b.Y+b.H {
			return fmt.Sprintf("want %s %+v below %s %+v", name, a, other, b)
		}
		return ""
	}
}

func sliderAt(v float32) expect { return sliderNear(v, 0) }

func sliderNear(v, eps float32) expect {
	return func(u *widgetUI, evs []NodeEvent) string {
		if got := u.w.SliderValue(u.slider); got < v-eps || got > v+eps {
			return fmt.Sprintf("want slider %v±%v, got %v", v, eps, got)
		}
		return ""
	}
}

func sliderAbove(v float32) expect {
	return func(u *widgetUI, evs []NodeEvent) string {
		if got := u.w.SliderValue(u.slider); got <= v {
			return fmt.Sprintf("want slider above %v, got %v", v, got)
		}
		return ""
	}
}

func scrollAt(y float32) expect {
	return func(u *widgetUI, evs []NodeEvent) string {
		if got := u.k.Style(u.scroll).Scroll.Y; got != y {
			return fmt.Sprintf("want scroll %v, got %v", y, got)
		}
		return ""
	}
}

func scrollBelow(y float32) expect {
	return func(u *widgetUI, evs []NodeEvent) string {
		if got := u.k.Style(u.scroll).Scroll.Y; got >= y {
			return fmt.Sprintf("want scroll below %v, got %v", y, got)
		}
		return ""
	}
}

func scrolledToEnd() expect {
	return func(u *widgetUI, evs []NodeEvent) string {
		end := -(u.k.ContentSize(u.scroll).H - u.k.Bounds(u.scroll).H)
		if got := u.k.Style(u.scroll).Scroll.Y; end >= 0 || got != end {
			return fmt.Sprintf("want scroll at end %v, got %v", end, got)
		}
		return ""
	}
}

func requested(kind kui.RequestKind, s string) expect {
	return func(u *widgetUI, evs []NodeEvent) string {
		reqs, _ := u.k.Requests()
		for i := range reqs {
			if reqs[i].Kind == kind && u.k.RequestText(&reqs[i]) == s {
				return ""
			}
		}
		return fmt.Sprintf("want request %v %q, got %+v", kind, s, reqs)
	}
}

func noRequest(kind kui.RequestKind) expect {
	return func(u *widgetUI, evs []NodeEvent) string {
		reqs, _ := u.k.Requests()
		for i := range reqs {
			if reqs[i].Kind == kind {
				return fmt.Sprintf("want no %v request, got %+v", kind, reqs[i])
			}
		}
		return ""
	}
}

// dragThumb drags the scroll container's scroll bar thumb, at its top right corner while
// scrolled to the top, down by dy pixels.
func dragThumb(dy int) op { return dragFrom("scroll", 0.995, 0.01, 0, dy) }
