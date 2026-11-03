package kuiscene

import (
	"unicode/utf8"
	"unsafe"

	"github.com/soypat/kui"
	"github.com/soypat/kui/gesture"
)

// edit is the editing state of the focused entry. Entries keep nothing but their text while
// not focused, so a single edit serves them all, like in immediate mode UIs.
type edit struct {
	node      kui.Handle // Entry being edited, zero for none.
	caret     int        // Byte offset of the caret in the text.
	anchor    int        // Other end of the selection; equal to caret when nothing is selected.
	scroll    float32    // Horizontal scroll of the text in pixels.
	selecting bool       // The pointer is dragging out a selection.
}

// selection returns the selected byte range.
func (ed *edit) selection() (lo, hi int) { return min(ed.caret, ed.anchor), max(ed.caret, ed.anchor) }

// Entry adds a single line text input holding at most maxLen bytes of text. The bytes are
// reserved in the scene's text arena when the entry is created, so editing never fails.
// It emits [EventChanged] when the user changes its text, which [Scene.Text] returns, and
// [EventActivate] when Enter is pressed.
//
// Entries support selection with the pointer and the keyboard, word movement with Ctrl or Alt,
// and the clipboard through [kui.RequestClipboardWrite] and [kui.RequestClipboardRead].
// Input method composition ([kui.EventPreedit]) is not displayed.
func (w *WidgetBuilder) Entry(parent kui.Handle, s string, maxLen int) kui.Handle {
	b := w.b()
	if w.measurer == nil {
		b.Fail(kui.ErrInvalidConfig)
		return 0
	}
	vis := w.textVisual(w.theme.Text.Color)
	vis.Text.Wrap = kui.WrapNone
	vis.Background = w.theme.Field
	vis.Radius = kui.Radius(w.theme.Radius)
	p := w.theme.Padding
	_, lineHeight := w.measurer.Measure("", &vis.Text)
	style := kui.LayoutStyle{
		Width: kui.Grow(0, 0), Height: kui.Fixed(lineHeight + 2*p),
		Padding: kui.InsetsAll(p), AlignY: kui.AlignMiddle, ClipX: true,
	}
	h := b.Node(parent, w.kinds.Entry, &style, &vis)
	if h != 0 {
		b.Fail(w.scene.ReserveText(h, maxLen))
		b.Fail(w.scene.SetText(h, s[:runeFloor(s, maxLen)]))
		b.SetFlags(h, FlagFocusable)
	}
	return h
}

// Selection returns the selected byte range of entry h's text. It is empty unless h has focus.
func (w *WidgetBuilder) Selection(h kui.Handle) (start, end int) {
	if w.edit.node != h {
		return 0, 0
	}
	return w.edit.selection()
}

func (w *WidgetBuilder) updateEntry(k *Scene, n kui.Handle, e *NodeEvent) {
	w.updateCommon(n, e)
	ed := &w.edit
	switch e.Kind {
	case EventFocusOut:
		w.endEdit(n)
		return
	case EventFocusIn, gesture.Press:
		// A press arrives before the focus it gives: start editing to place the caret.
		w.beginEdit(n)
	}
	if ed.node != n {
		return // The remaining events only matter while editing.
	}
	switch e.Kind {
	case EventFocusIn: // The caret is at the end of the text.
	case gesture.Press:
		ed.caret, ed.selecting = w.offsetAt(n, e.X), true
		if e.KeyMods&kui.KeyModShift == 0 {
			ed.anchor = ed.caret
		}
	case gesture.DragStart, gesture.Drag:
		if !ed.selecting {
			return
		}
		ed.caret = w.offsetAt(n, e.X)
	case gesture.DragEnd, gesture.Release:
		ed.selecting = false
		return
	case gesture.DoubleTap:
		s := k.Text(n)
		ed.anchor, ed.caret = wordStart(s, ed.caret), wordEnd(s, ed.caret)
	case kui.EventKeyDown:
		w.editKey(n, e)
	case kui.EventText:
		w.insertText(n, e)
	case EventTimer:
		if blink := w.theme.CaretBlink; e.Code == timerBlink && blink > 0 {
			k.SetState(n, k.State(n)^stateCaret)
			k.SetTimer(n, timerBlink, e.Time+blink/2)
		}
		return
	default:
		return
	}
	w.caretMoved(n, e.Time)
}

// beginEdit makes n the edited entry with the caret at the end of its text.
func (w *WidgetBuilder) beginEdit(n kui.Handle) {
	if w.edit.node == n {
		return
	}
	w.endEdit(w.edit.node)
	end := len(w.scene.Text(n))
	w.edit = edit{node: n, caret: end, anchor: end}
	w.scene.Request(kui.Request{Kind: kui.RequestTextInput, Node: n, Code: 1, Rect: w.scene.Bounds(n)}, "")
}

// endEdit stops editing entry n, scrolling it back to the start of its text.
func (w *WidgetBuilder) endEdit(n kui.Handle) {
	k := w.scene
	if w.edit.node != n || n == 0 {
		return
	}
	w.edit = edit{}
	k.CancelTimer(n, timerBlink)
	k.SetState(n, k.State(n)&^stateCaret)
	if style := k.Style(n); style.Scroll.X != 0 {
		style.Scroll.X = 0
		k.SetStyle(n, &style)
	}
	k.Request(kui.Request{Kind: kui.RequestTextInput, Node: n, Code: 0}, "")
}

func (w *WidgetBuilder) editKey(n kui.Handle, e *NodeEvent) {
	k := w.scene
	ed := &w.edit
	s := k.Text(n)
	extend := e.KeyMods&kui.KeyModShift != 0
	word := e.KeyMods&(kui.KeyModCtrl|kui.KeyModAlt) != 0       // Ctrl, or Option on macOS.
	shortcut := e.KeyMods&(kui.KeyModCtrl|kui.KeyModSuper) != 0 // Ctrl, or Command on macOS.
	move := func(to int) {
		ed.caret = to
		if !extend {
			ed.anchor = to
		}
	}
	lo, hi := ed.selection()
	switch key := kui.Key(e.Code); {
	case key == kui.KeyArrowLeft && lo < hi && !extend:
		move(lo)
	case key == kui.KeyArrowLeft:
		move(prevStop(s, ed.caret, word))
	case key == kui.KeyArrowRight && lo < hi && !extend:
		move(hi)
	case key == kui.KeyArrowRight:
		move(nextStop(s, ed.caret, word))
	case key == kui.KeyHome:
		move(0)
	case key == kui.KeyEnd:
		move(len(s))
	case key == kui.KeyBackspace:
		if lo == hi {
			lo = prevStop(s, lo, word)
		}
		w.replace(n, lo, hi, "")
	case key == kui.KeyDelete:
		if lo == hi {
			hi = nextStop(s, hi, word)
		}
		w.replace(n, lo, hi, "")
	case key == kui.KeyEnter:
		k.Emit(NodeEvent{Node: n, Kind: EventActivate, Time: e.Time})
	case shortcut && key == kui.KeyA:
		ed.anchor, ed.caret = 0, len(s)
	case shortcut && (key == kui.KeyC || key == kui.KeyX) && lo < hi:
		k.Request(kui.Request{Kind: kui.RequestClipboardWrite, Node: n}, s[lo:hi])
		if key == kui.KeyX {
			w.replace(n, lo, hi, "")
		}
	case shortcut && key == kui.KeyV:
		k.Request(kui.Request{Kind: kui.RequestClipboardRead, Node: n}, "")
	}
}

// insertText replaces the selection with the text of event e, dropping control characters and
// the text that does not fit the insertion buffer.
func (w *WidgetBuilder) insertText(n kui.Handle, e *NodeEvent) {
	buf := w.insert[:w.scene.EventText(w.insert[:cap(w.insert)], e)]
	j := 0
	for _, c := range buf {
		if c >= 0x20 && c != 0x7f { // UTF-8 continuation bytes are >= 0x80.
			buf[j] = c
			j++
		}
	}
	lo, hi := w.edit.selection()
	w.replace(n, lo, hi, unsafe.String(unsafe.SliceData(buf), j))
}

// replace replaces bytes [lo, hi) of entry n's text with s, truncating s to the entry's
// capacity, and places the caret after it.
func (w *WidgetBuilder) replace(n kui.Handle, lo, hi int, s string) {
	k := w.scene
	room := k.TextCap(n) - len(k.Text(n)) + hi - lo
	s = s[:runeFloor(s, room)]
	if lo == hi && s == "" {
		return
	}
	if k.EditText(n, lo, hi, s) != nil {
		return
	}
	w.edit.caret = lo + len(s)
	w.edit.anchor = w.edit.caret
	k.Emit(NodeEvent{Node: n, Kind: EventChanged})
}

// caretMoved shows the caret, restarting its blink, and scrolls the text to keep it visible.
func (w *WidgetBuilder) caretMoved(n kui.Handle, now int64) {
	k := w.scene
	ed := &w.edit
	k.SetState(n, k.State(n)|stateCaret)
	if blink := w.theme.CaretBlink; blink > 0 {
		k.SetTimer(n, timerBlink, now+blink/2)
	}
	k.Repaint()
	style := k.Style(n)
	view := k.Bounds(n).W - style.Padding.Left - style.Padding.Right - 1 // Room for the caret.
	s := k.Text(n)
	st := &k.Visual(n).Text
	x := w.measure(s[:ed.caret], st)
	if x-ed.scroll > view {
		ed.scroll = x - view
	} else if x < ed.scroll {
		ed.scroll = x
	}
	ed.scroll = max(min(ed.scroll, w.measure(s, st)-view), 0)
	if style.Scroll.X != -ed.scroll {
		style.Scroll.X = -ed.scroll
		k.SetStyle(n, &style)
	}
}

func (w *WidgetBuilder) measure(s string, st *kui.TextStyle) float32 {
	width, _ := w.measurer.Measure(s, st)
	return width
}

// offsetAt returns the byte offset of the rune boundary of entry n's text nearest to x,
// relative to n's box.
func (w *WidgetBuilder) offsetAt(n kui.Handle, x float32) int {
	k := w.scene
	s := k.Text(n)
	st := &k.Visual(n).Text
	x += w.edit.scroll - k.Style(n).Padding.Left
	best, bestDist := 0, abs(x)
	try := func(i int) {
		if d := abs(w.measure(s[:i], st) - x); d < bestDist {
			best, bestDist = i, d
		}
	}
	for i := range s {
		try(i)
	}
	try(len(s))
	return best
}

func abs(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func (w *WidgetBuilder) paintEntry(k *Scene, n kui.Handle, l *kui.DrawList) {
	box := k.Bounds(n)
	vis := k.Visual(n)
	l.Rect(n, box, vis.Background, vis.Radius)
	ed := &w.edit
	if ed.node != n {
		k.PaintText(n, l, vis.Text.Color)
		return
	}
	style := k.Style(n)
	s := k.Text(n)
	x0 := box.X + style.Padding.Left - ed.scroll
	inner := kui.Rect{Y: box.Y + style.Padding.Top, H: box.H - style.Padding.Top - style.Padding.Bottom}
	if lo, hi := ed.selection(); lo < hi {
		a, b := w.measure(s[:lo], &vis.Text), w.measure(s[:hi], &vis.Text)
		inner.X, inner.W = x0+a, b-a
		l.Rect(n, inner, w.theme.Selection, kui.DrawRadii{})
	}
	k.PaintText(n, l, vis.Text.Color)
	if k.State(n)&stateCaret != 0 {
		inner.X, inner.W = x0+w.measure(s[:ed.caret], &vis.Text), 1
		l.Rect(n, inner, w.theme.Caret, kui.DrawRadii{})
	}
}

// paintEntryOver draws the focus ring outside the entry's clip.
func (w *WidgetBuilder) paintEntryOver(k *Scene, n kui.Handle, l *kui.DrawList) {
	w.paintFocus(n, l, k.Bounds(n), k.Visual(n).Radius)
}

// runeFloor returns the largest rune boundary of s not after n.
func runeFloor(s string, n int) int {
	if n >= len(s) {
		return len(s)
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return max(n, 0)
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' }

// prevStop returns the caret position before i: the previous rune, or with word the start of
// the previous word.
func prevStop(s string, i int, word bool) int {
	if !word {
		_, size := utf8.DecodeLastRuneInString(s[:i])
		return i - size
	}
	for i > 0 && isSpace(s[i-1]) {
		i--
	}
	return wordStart(s, i)
}

// nextStop returns the caret position after i: the next rune, or with word the end of the
// next word.
func nextStop(s string, i int, word bool) int {
	if !word {
		_, size := utf8.DecodeRuneInString(s[i:])
		return i + size
	}
	for i < len(s) && isSpace(s[i]) {
		i++
	}
	return wordEnd(s, i)
}

// wordStart returns the start of the word containing or ending at i.
func wordStart(s string, i int) int {
	for i > 0 && !isSpace(s[i-1]) {
		i--
	}
	return i
}

// wordEnd returns the end of the word containing or starting at i.
func wordEnd(s string, i int) int {
	for i < len(s) && !isSpace(s[i]) {
		i++
	}
	return i
}
