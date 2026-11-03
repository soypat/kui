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
func (scene *Scene) Step(now int64, events []kui.Event, text TextSource) error {
	scene.now = now
	scene.textSrc = text
	m := scene.mem
	// Keep events and requests added since the last Step, dropping those already delivered.
	m.events = m.events[:copy(m.events, m.events[scene.stepEnd:])]
	m.reqs = m.reqs[:copy(m.reqs, m.reqs[scene.reqEnd:])]
	if len(m.reqs) == 0 {
		m.reqText = m.reqText[:0]
	}
	scene.stepEnd, scene.reqEnd = 0, 0
	scene.ensureLayout()
	for i := range events {
		scene.route(&events[i])
	}
	scene.checkTimers()
	// Let kinds react to events in order, including events they emit.
	for i := 0; i < len(m.events); i++ {
		e := &m.events[i]
		if j, err := scene.resolve(e.Node); err == nil {
			if reduce := scene.kinds[m.kind[j]].Update; reduce != nil {
				reduce(scene, e.Node, e)
			}
		}
	}
	scene.ensureLayout()
	scene.stepEnd = len(m.events)
	scene.reqEnd = len(m.reqs)
	return scene.layoutErr
}

// NextDeadline returns the time at which the scene next needs a Step without input, i.e. to
// fire a long press or a timer. Hosts can sleep until then to save power.
func (k *Scene) NextDeadline() (int64, bool) {
	deadline, ok := k.gest.Deadline((*scene)(k))
	for _, t := range k.mem.timers {
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
func (scene *Scene) SetTimer(n kui.Handle, code uint32, at int64) error {
	if _, err := scene.resolve(n); err != nil {
		return err
	}
	m := scene.mem
	for i := range m.timers {
		if t := &m.timers[i]; t.node == n && t.code == code {
			t.at = at
			return nil
		}
	}
	if len(m.timers) == cap(m.timers) {
		scene.pruneTimers()
		if len(m.timers) == cap(m.timers) {
			return kui.ErrCapacity
		}
	}
	m.timers = append(m.timers, timer{node: n, code: code, at: at})
	return nil
}

// CancelTimer cancels node n's pending timer with the given code, if any.
func (scene *Scene) CancelTimer(n kui.Handle, code uint32) {
	m := scene.mem
	for i, t := range m.timers {
		if t.node == n && t.code == code {
			m.timers = append(m.timers[:i], m.timers[i+1:]...)
			return
		}
	}
}

// pruneTimers removes the timers of removed nodes.
func (scene *Scene) pruneTimers() {
	m := scene.mem
	j := 0
	for _, t := range m.timers {
		if scene.Valid(t.node) {
			m.timers[j] = t
			j++
		}
	}
	m.timers = m.timers[:j]
}

// scene is the [Scene] as seen by its gesture recognizer.
type scene Scene

func (s *scene) Press(p kui.Point) (kui.Handle, bool) {
	k := (*Scene)(s)
	if i := k.mem.hits.query(p.X, p.Y, sensePress); i >= 0 && k.inModal(i) {
		return k.handle(i), true
	} else if k.modal >= 0 {
		return 0, false // Presses outside the modal node dismiss it and reach nothing else.
	} else if pan := k.mem.hits.query(p.X, p.Y, gesture.SenseScroll); pan >= 0 {
		return k.handle(pan), false // Dragging non interactive content may pan its container.
	}
	return 0, false
}

func (s *scene) Hover(p kui.Point) kui.Handle {
	k := (*Scene)(s)
	if i := k.mem.hits.query(p.X, p.Y, gesture.SenseHover); i >= 0 && k.inModal(i) {
		return k.handle(i)
	}
	return 0
}

// DragTarget returns the nearest of n and its ancestors whose kind senses drags or that has
// [FlagScrollable]. The search stops at the active modal node.
func (s *scene) DragTarget(n kui.Handle) kui.Handle {
	k := (*Scene)(s)
	m := k.mem
	i, _ := k.resolve(n)
	for ; i >= 0; i = m.links[i].parent {
		if m.flags[i]&FlagDisabled == 0 && (k.kinds[m.kind[i]].Sense&gesture.SenseDrag != 0 || m.flags[i]&FlagScrollable != 0) {
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
		return k.kinds[k.mem.kind[i]].Sense
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
				cursor = k.kinds[k.mem.kind[j]].Cursor
			}
		}
	}
	if cursor != k.cursor {
		k.cursor = cursor
		k.Request(kui.Request{Kind: kui.RequestCursor, Cursor: cursor}, "")
	}
}

// deliver emits gesture g to its node. Drags of nodes not sensing them pan their content
// ([FlagScrollable] nodes).
func (scene *Scene) deliver(g *gesture.Event) {
	i, err := scene.resolve(g.Node)
	if g.Kind == gesture.Press {
		scene.press(i, g)
		return
	} else if err != nil {
		return // The node was removed during the gesture.
	}
	switch g.Kind {
	case gesture.DragStart, gesture.Drag, gesture.DragEnd:
		if scene.kinds[scene.mem.kind[i]].Sense&gesture.SenseDrag == 0 {
			if g.Kind != gesture.DragEnd {
				scene.scrollBy(i, -g.DX, -g.DY)
			}
			return
		}
	}
	scene.emitAt(i, NodeEvent(*g))
}

// press delivers a press on node i, -1 for none.
func (scene *Scene) press(i int32, g *gesture.Event) {
	if !scene.inModal(i) {
		// Presses outside the modal node dismiss it and reach nothing else.
		e := NodeEvent(*g)
		e.Kind = EventDismiss
		scene.emitAt(scene.modal, e)
		return
	}
	// Pressing focuses focusable nodes and unfocuses otherwise, like most toolkits.
	focus := kui.Handle(0)
	if i >= 0 {
		scene.emitAt(i, NodeEvent(*g))
		if scene.mem.flags[i]&FlagFocusable != 0 {
			focus = g.Node
		}
	}
	scene.changeFocus(focus)
}

// emitAt emits e for node i with its position made relative to the node's box.
func (scene *Scene) emitAt(i int32, e NodeEvent) {
	box := scene.mem.box[i]
	e.Node, e.X, e.Y = scene.handle(i), e.X-box.X, e.Y-box.Y
	scene.Emit(e)
}

func (scene *Scene) route(ev *kui.Event) {
	switch ev.Kind {
	case kui.EventPointerDown, kui.EventPointerUp, kui.EventPointerMove, kui.EventPointerCancel,
		kui.EventPointerLeave, kui.EventBlur:
		scene.pointer(ev)
	case kui.EventScroll:
		scene.scroll(ev)
	case kui.EventKeyDown:
		switch key := ev.Key(); {
		case key == kui.KeyTab && ev.KeyMods&(kui.KeyModCtrl|kui.KeyModAlt|kui.KeyModSuper) == 0:
			scene.focusNext(ev.KeyMods&kui.KeyModShift != 0)
		case key == kui.KeyEscape && scene.modal >= 0:
			scene.Emit(NodeEvent{Time: ev.Time, Node: scene.handle(scene.modal), Kind: EventDismiss, Code: ev.Code, KeyMods: ev.KeyMods})
		default:
			scene.emitFocused(ev)
		}
	case kui.EventKeyUp, kui.EventText, kui.EventPreedit:
		scene.emitFocused(ev)
	case kui.EventResize:
		scene.SetViewport(kui.Size{W: ev.X, H: ev.Y})
		scene.ensureLayout()
	}
}

func (scene *Scene) emitFocused(ev *kui.Event) {
	if scene.Valid(scene.focus) {
		e := NodeEvent(*ev)
		e.Node = scene.focus
		scene.Emit(e)
	}
}

// inModal reports whether node i may receive pointer input: there is no active modal node,
// or i is the modal node or one of its descendants. It is false for i < 0 under a modal node.
func (scene *Scene) inModal(i int32) bool {
	if scene.modal < 0 {
		return true
	}
	for ; i >= 0; i = scene.mem.links[i].parent {
		if i == scene.modal {
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
	m := k.mem
	j := 0
	for _, t := range m.timers {
		if !k.Valid(t.node) {
			continue
		} else if t.at <= k.now {
			k.Emit(NodeEvent{Node: t.node, Kind: EventTimer, Code: t.code})
			continue
		}
		m.timers[j] = t
		j++
	}
	m.timers = m.timers[:j]
}

func (scene *Scene) scroll(ev *kui.Event) {
	target := scene.mem.hits.query(ev.X, ev.Y, gesture.SenseScroll)
	if target < 0 || !scene.inModal(target) {
		return
	}
	if scene.mem.flags[target]&FlagScrollable != 0 {
		scene.scrollBy(target, ev.DX, ev.DY)
	}
	if scene.kinds[scene.mem.kind[target]].Sense&gesture.SenseScroll != 0 {
		scene.emitAt(target, NodeEvent(*ev))
	}
}

// ScrollBy scrolls the content of clipping node h by dx, dy pixels along its clipped axes,
// clamped to its content size from the last layout. Positive values reveal content further
// right and down. It reports whether the scroll position, [kui.LayoutStyle.Scroll], changed.
func (scene *Scene) ScrollBy(h kui.Handle, dx, dy float32) bool {
	i, err := scene.resolve(h)
	return err == nil && scene.scrollBy(i, dx, dy)
}

func (scene *Scene) scrollBy(i int32, dx, dy float32) bool {
	m := scene.mem
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
	scene.invalidate()
	return true
}

// clampScroll clamps a child offset to [-overflow, 0].
func clampScroll(offset, overflow float32) float32 {
	return min(max(offset, -max(overflow, 0)), 0)
}

// ContentSize returns the size of the content of clipping node h, including its padding,
// computed by the last layout. Scrolling node h reveals the parts outside its box.
func (scene *Scene) ContentSize(h kui.Handle) kui.Size {
	if i, err := scene.resolve(h); err == nil && clips(&scene.mem.style[i]) {
		return scene.mem.tree.Content[i]
	}
	return kui.Size{}
}

func (scene *Scene) changeFocus(h kui.Handle) {
	if h == scene.focus {
		return
	}
	if _, err := scene.resolve(scene.focus); err == nil {
		scene.Emit(NodeEvent{Node: scene.focus, Kind: EventFocusOut})
	}
	scene.focus = h
	if _, err := scene.resolve(h); err == nil {
		scene.Emit(NodeEvent{Node: h, Kind: EventFocusIn})
	}
	scene.needPaint = true
}

// focusNext moves focus to the next (or previous) focusable node in pre-order, wrapping around.
// Under an active modal node only its subtree is traversed.
func (scene *Scene) focusNext(backward bool) {
	m := scene.mem
	current, _ := scene.resolve(scene.focus)
	var first, last, prev, next int32 = -1, -1, -1, -1
	seenCurrent := false
	start := scene.root
	if scene.modal >= 0 {
		start = scene.modal
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
		scene.changeFocus(scene.handle(target))
	}
}
