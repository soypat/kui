package kuiscene

import (
	"github.com/soypat/kui"
	"github.com/soypat/kui/gesture"
)

// Step processes input events at time now (monotonic nanoseconds), producing widget events
// readable with [Scene.Events] and host requests readable with [Scene.Requests].
// text resolves the payload of multi-byte text events and may be nil.
// The layout is recomputed first if the tree changed, so events are routed to what is on screen
// after the last Paint, and again afterwards if event handling changed the tree.
func (k *Scene) Step(now int64, events []kui.Event, text TextSource) error {
	k.now = now
	k.textSrc = text
	m := k.m
	// Keep events and requests added since the last Step, dropping those already delivered.
	m.events = m.events[:copy(m.events, m.events[k.stepEnd:])]
	m.reqs = m.reqs[:copy(m.reqs, m.reqs[k.reqEnd:])]
	if len(m.reqs) == 0 {
		m.reqText = m.reqText[:0]
	}
	k.stepEnd, k.reqEnd = 0, 0
	k.ensureLayout()
	for i := range events {
		k.route(&events[i])
	}
	k.checkTimers()
	// Let kinds react to events in order, including events they emit.
	for i := 0; i < len(m.events); i++ {
		e := &m.events[i]
		if j, err := k.resolve(e.Node); err == nil {
			if reduce := k.kinds[m.kind[j]].Reduce; reduce != nil {
				reduce(k, e.Node, e)
			}
		}
	}
	k.ensureLayout()
	k.stepEnd = len(m.events)
	k.reqEnd = len(m.reqs)
	return k.layoutErr
}

// NextDeadline returns the time at which the kernel next needs a Step without input, i.e. to
// fire a long press or a timer. Hosts can sleep until then to save power.
func (k *Scene) NextDeadline() (int64, bool) {
	deadline, ok := k.gest.Deadline((*scene)(k))
	for _, t := range k.m.timers {
		if k.Valid(t.node) && (!ok || t.at < deadline) {
			deadline, ok = t.at, true
		}
	}
	return deadline, ok
}

// SetTimer schedules an [EventTimer] with the given code for node n at time at, replacing n's
// pending timer with the same code. The first Step at or after at delivers it, and
// [Scene.NextDeadline] reports it so hosts can sleep until then. It returns
// [kui.ErrCapacity] if [MemoryConfig.MaxTimers] timers are pending.
func (k *Scene) SetTimer(n kui.Handle, code uint32, at int64) error {
	if _, err := k.resolve(n); err != nil {
		return err
	}
	m := k.m
	for i := range m.timers {
		if t := &m.timers[i]; t.node == n && t.code == code {
			t.at = at
			return nil
		}
	}
	if len(m.timers) == cap(m.timers) {
		k.pruneTimers()
		if len(m.timers) == cap(m.timers) {
			return kui.ErrCapacity
		}
	}
	m.timers = append(m.timers, timer{node: n, code: code, at: at})
	return nil
}

// CancelTimer cancels node n's pending timer with the given code, if any.
func (k *Scene) CancelTimer(n kui.Handle, code uint32) {
	m := k.m
	for i, t := range m.timers {
		if t.node == n && t.code == code {
			m.timers = append(m.timers[:i], m.timers[i+1:]...)
			return
		}
	}
}

// pruneTimers removes the timers of removed nodes.
func (k *Scene) pruneTimers() {
	m := k.m
	j := 0
	for _, t := range m.timers {
		if k.Valid(t.node) {
			m.timers[j] = t
			j++
		}
	}
	m.timers = m.timers[:j]
}

// scene is the kernel as seen by its gesture recognizer.
type scene Scene

func (s *scene) Press(p kui.Point) (kui.Handle, bool) {
	k := (*Scene)(s)
	if i := k.m.hits.query(p.X, p.Y, sensePress); i >= 0 && k.inModal(i) {
		return k.handle(i), true
	} else if k.modal >= 0 {
		return 0, false // Presses outside the modal node dismiss it and reach nothing else.
	} else if pan := k.m.hits.query(p.X, p.Y, gesture.SenseScroll); pan >= 0 {
		return k.handle(pan), false // Dragging non interactive content may pan its container.
	}
	return 0, false
}

func (s *scene) Hover(p kui.Point) kui.Handle {
	k := (*Scene)(s)
	if i := k.m.hits.query(p.X, p.Y, gesture.SenseHover); i >= 0 && k.inModal(i) {
		return k.handle(i)
	}
	return 0
}

// DragTarget returns the nearest of n and its ancestors whose kind senses drags or that has
// [FlagScroll]. The search stops at the active modal node.
func (s *scene) DragTarget(n kui.Handle) kui.Handle {
	k := (*Scene)(s)
	m := k.m
	i, _ := k.resolve(n)
	for ; i >= 0; i = m.links[i].parent {
		if m.flags[i]&FlagDisabled == 0 && (k.kinds[m.kind[i]].Sense&gesture.SenseDrag != 0 || m.flags[i]&FlagScroll != 0) {
			return k.handle(i)
		} else if i == k.modal {
			break
		}
	}
	return 0
}

func (s *scene) Sense(n kui.Handle) gesture.Sense {
	k := (*Scene)(s)
	if i, err := k.resolve(n); err == nil {
		return k.kinds[k.m.kind[i]].Sense
	}
	return 0
}

// pointer recognizes the gestures of pointer event ev and delivers them.
func (k *Scene) pointer(ev *kui.Event) {
	cursor := k.cursor
	gs := k.gest.Feed((*scene)(k), ev, k.gestures[:0])
	for i := range gs {
		g := &gs[i]
		k.deliver(g)
		if g.Source == 0 && (g.Kind == gesture.HoverIn || g.Kind == gesture.HoverOut) {
			cursor = kui.CursorDefault
			if j, err := k.resolve(g.Node); err == nil && g.Kind == gesture.HoverIn {
				cursor = k.kinds[k.m.kind[j]].Cursor
			}
		}
	}
	if cursor != k.cursor {
		k.cursor = cursor
		k.Request(kui.Request{Kind: kui.RequestCursor, Cursor: cursor}, "")
	}
}

// deliver emits gesture g to its node. Drags of nodes not sensing them pan their content
// ([FlagScroll] nodes).
func (k *Scene) deliver(g *gesture.Event) {
	i, err := k.resolve(g.Node)
	if g.Kind == gesture.Press {
		k.press(i, g)
		return
	} else if err != nil {
		return // The node was removed during the gesture.
	}
	switch g.Kind {
	case gesture.DragStart, gesture.Drag, gesture.DragEnd:
		if k.kinds[k.m.kind[i]].Sense&gesture.SenseDrag == 0 {
			if g.Kind != gesture.DragEnd {
				k.scrollBy(i, -g.DX, -g.DY)
			}
			return
		}
	}
	k.emitAt(i, WidgetEvent(*g))
}

// press delivers a press on node i, -1 for none.
func (k *Scene) press(i int32, g *gesture.Event) {
	if !k.inModal(i) {
		// Presses outside the modal node dismiss it and reach nothing else.
		e := WidgetEvent(*g)
		e.Kind = EventDismiss
		k.emitAt(k.modal, e)
		return
	}
	// Pressing focuses focusable nodes and unfocuses otherwise, like most toolkits.
	focus := kui.Handle(0)
	if i >= 0 {
		k.emitAt(i, WidgetEvent(*g))
		if k.m.flags[i]&FlagFocusable != 0 {
			focus = g.Node
		}
	}
	k.changeFocus(focus)
}

// emitAt emits e for node i with its position made relative to the node's box.
func (k *Scene) emitAt(i int32, e WidgetEvent) {
	box := k.m.box[i]
	e.Node, e.X, e.Y = k.handle(i), e.X-box.X, e.Y-box.Y
	k.Emit(e)
}

func (k *Scene) route(ev *kui.Event) {
	switch ev.Kind {
	case kui.EventPointerDown, kui.EventPointerUp, kui.EventPointerMove, kui.EventPointerCancel,
		kui.EventPointerLeave, kui.EventBlur:
		k.pointer(ev)
	case kui.EventScroll:
		k.scroll(ev)
	case kui.EventKeyDown:
		switch key := ev.Key(); {
		case key == kui.KeyTab && ev.KeyMods&(kui.KeyModCtrl|kui.KeyModAlt|kui.KeyModSuper) == 0:
			k.focusNext(ev.KeyMods&kui.KeyModShift != 0)
		case key == kui.KeyEscape && k.modal >= 0:
			k.Emit(WidgetEvent{Time: ev.Time, Node: k.handle(k.modal), Kind: EventDismiss, Code: ev.Code, KeyMods: ev.KeyMods})
		default:
			k.emitFocused(ev)
		}
	case kui.EventKeyUp, kui.EventText, kui.EventPreedit:
		k.emitFocused(ev)
	case kui.EventResize:
		k.SetViewport(kui.Size{W: ev.X, H: ev.Y})
		k.ensureLayout()
	}
}

func (k *Scene) emitFocused(ev *kui.Event) {
	if k.Valid(k.focus) {
		e := WidgetEvent(*ev)
		e.Node = k.focus
		k.Emit(e)
	}
}

// inModal reports whether node i may receive pointer input: there is no active modal node,
// or i is the modal node or one of its descendants. It is false for i < 0 under a modal node.
func (k *Scene) inModal(i int32) bool {
	if k.modal < 0 {
		return true
	}
	for ; i >= 0; i = k.m.links[i].parent {
		if i == k.modal {
			return true
		}
	}
	return false
}

// checkTimers fires long presses and due timers.
func (k *Scene) checkTimers() {
	gs := k.gest.Tick((*scene)(k), k.now, k.gestures[:0])
	for i := range gs {
		k.deliver(&gs[i])
	}
	// Fire due timers in the order they were set, keeping the others.
	m := k.m
	j := 0
	for _, t := range m.timers {
		if !k.Valid(t.node) {
			continue
		} else if t.at <= k.now {
			k.Emit(WidgetEvent{Node: t.node, Kind: EventTimer, Code: t.code})
			continue
		}
		m.timers[j] = t
		j++
	}
	m.timers = m.timers[:j]
}

func (k *Scene) scroll(ev *kui.Event) {
	target := k.m.hits.query(ev.X, ev.Y, gesture.SenseScroll)
	if target < 0 || !k.inModal(target) {
		return
	}
	if k.m.flags[target]&FlagScroll != 0 {
		k.scrollBy(target, ev.DX, ev.DY)
	}
	if k.kinds[k.m.kind[target]].Sense&gesture.SenseScroll != 0 {
		k.emitAt(target, WidgetEvent(*ev))
	}
}

// ScrollBy scrolls the content of clipping node h by dx, dy pixels along its clipped axes,
// clamped to its content size from the last layout. Positive values reveal content further
// right and down. It reports whether the scroll position, [kui.LayoutStyle.Scroll], changed.
func (k *Scene) ScrollBy(h kui.Handle, dx, dy float32) bool {
	i, err := k.resolve(h)
	return err == nil && k.scrollBy(i, dx, dy)
}

func (k *Scene) scrollBy(i int32, dx, dy float32) bool {
	m := k.m
	content := m.tree.Content[i]
	style := &m.style[i]
	box := m.box[i]
	prev := style.Scroll
	if style.ClipX {
		style.Scroll.X = clampScroll(style.Scroll.X-dx, content.W-box.W)
	}
	if style.ClipY {
		style.Scroll.Y = clampScroll(style.Scroll.Y-dy, content.H-box.H)
	}
	if style.Scroll == prev {
		return false
	}
	k.invalidate()
	return true
}

// clampScroll clamps a child offset to [-overflow, 0].
func clampScroll(offset, overflow float32) float32 {
	return min(max(offset, -max(overflow, 0)), 0)
}

// ContentSize returns the size of the content of clipping node h, including its padding,
// computed by the last layout. Scrolling node h reveals the parts outside its box.
func (k *Scene) ContentSize(h kui.Handle) kui.Size {
	if i, err := k.resolve(h); err == nil && clips(&k.m.style[i]) {
		return k.m.tree.Content[i]
	}
	return kui.Size{}
}

func (k *Scene) changeFocus(h kui.Handle) {
	if h == k.focus {
		return
	}
	if _, err := k.resolve(k.focus); err == nil {
		k.Emit(WidgetEvent{Node: k.focus, Kind: EventFocusOut})
	}
	k.focus = h
	if _, err := k.resolve(h); err == nil {
		k.Emit(WidgetEvent{Node: h, Kind: EventFocusIn})
	}
	k.needPaint = true
}

// focusNext moves focus to the next (or previous) focusable node in pre-order, wrapping around.
// Under an active modal node only its subtree is traversed.
func (k *Scene) focusNext(backward bool) {
	m := k.m
	current, _ := k.resolve(k.focus)
	var first, last, prev, next int32 = -1, -1, -1, -1
	seenCurrent := false
	start := k.root
	if k.modal >= 0 {
		start = k.modal
	}
	stack := append(m.stack[:0], start)
	for len(stack) > 0 {
		i := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		f := m.flags[i]
		if f&(FlagHidden|FlagDisabled) != 0 {
			continue
		}
		if f&FlagFocusable != 0 && i != current {
			if first < 0 {
				first = i
			}
			last = i
			if !seenCurrent {
				prev = i
			} else if next < 0 {
				next = i
			}
		}
		if i == current {
			seenCurrent = true
		}
		for c := m.links[i].last; c >= 0; c = m.links[c].prev {
			stack = append(stack, c)
		}
	}
	// Wrap around when there is no focusable node after (or before) the current one.
	target := next
	if !seenCurrent || next < 0 {
		target = first
	}
	if backward {
		target = prev
		if !seenCurrent || prev < 0 {
			target = last
		}
	}
	if target >= 0 {
		k.changeFocus(k.handle(target))
	}
}
