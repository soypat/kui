package kuiscene

import (
	"github.com/soypat/kui"
	"github.com/soypat/kui/gesture"
)

// Layout recomputes the layout if the tree changed. It is called automatically by Step,
// Paint and HitTest; calling it directly is only needed to query boxes after modifying the tree.
func (scene *Scene) Layout() error {
	scene.ensureLayout()
	return scene.layoutErr
}

func (scene *Scene) ensureLayout() {
	if !scene.needLayout {
		return
	}
	scene.needLayout = false
	scene.needPaint = true
	scene.layoutErr = scene.replay()
	scene.computeGeometry()
}

func clips(s *kui.LayoutStyle) bool { return s.ClipX || s.ClipY }

func floating(s *kui.LayoutStyle) bool { return s.Floating.AttachTo != kui.AttachNone }

// replay lists the visible tree in pre-order for the layout engine, lays it out and collects
// the paint roots: the root node and floating nodes, stable sorted by z.
func (scene *Scene) replay() error {
	m := scene.mem
	t := &m.tree
	t.Order = t.Order[:0]
	t.TextBytes = m.arena
	m.roots = append(m.roots[:0], root{node: scene.root})
	stack := append(m.stack[:0], scene.root)
	for len(stack) > 0 {
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if v < 0 {
			v = ^v
			t.Subtree[v] = int32(len(t.Order)) - t.Subtree[v] // Subtree held v's position.
			continue
		}
		style := &m.style[v]
		if f := &style.Floating; f.AttachTo == kui.AttachNode && !scene.Valid(f.Node) {
			continue // Attached to a removed node, whose index may now be another node's.
		}
		t.Subtree[v] = int32(len(t.Order))
		t.Order = append(t.Order, v)
		sp := m.text[v]
		t.Text[v] = kui.Span{Start: sp.off, Len: sp.len}
		if sp.len > 0 {
			t.TextStyle[v] = m.visual[v].Text
		}
		if v != scene.root && floating(style) && len(m.roots) < cap(m.roots) {
			m.roots = append(m.roots, root{node: v, z: style.Floating.Z})
		}
		stack = append(stack, ^v)
		for c := m.links[v].last; c >= 0; c = m.links[c].prev {
			if m.flags[c]&FlagHidden == 0 {
				stack = append(stack, c)
			}
		}
	}
	err := scene.engine.Layout(t, scene.viewport)
	// Stable sort roots by z.
	roots := m.roots
	for i := 1; i < len(roots); i++ {
		for j := i; j > 0 && roots[j].z < roots[j-1].z; j-- {
			roots[j], roots[j-1] = roots[j-1], roots[j]
		}
	}
	return err
}

// childClip returns the clip rectangle node i imposes on its children.
func (scene *Scene) childClip(i int32) kui.Rect {
	if clips(&scene.mem.style[i]) {
		return scene.mem.clip[i].Intersect(scene.mem.box[i])
	}
	return scene.mem.clip[i]
}

func (scene *Scene) computeGeometry() {
	m := scene.mem
	viewport := kui.Rect{W: scene.viewport.W, H: scene.viewport.H}
	// Clip rectangles in pre-order, so parents' are known.
	for _, i := range m.tree.Order {
		style := &m.style[i]
		p := m.links[i].parent
		switch {
		case i == scene.root:
			m.clip[i] = viewport
		case floating(style):
			m.clip[i] = viewport
			if style.Floating.Clip {
				switch style.Floating.AttachTo {
				case kui.AttachParent:
					m.clip[i] = scene.childClip(p)
				case kui.AttachNode:
					if t, err := scene.resolve(style.Floating.Node); err == nil {
						m.clip[i] = m.clip[t]
					}
				}
			}
		default:
			m.clip[i] = scene.childClip(p)
		}
	}
	// Hit index in paint order. The last modal node visited becomes the active one.
	hits := &m.hits
	hits.n = 0
	scene.modal = -1
	focusable := false
	stack := m.stack[:0]
	for _, r := range m.roots {
		stack = append(stack[:0], r.node)
		for len(stack) > 0 {
			i := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			f := m.flags[i]
			if f&FlagDisabled != 0 {
				continue // Disabled subtrees receive no input.
			}
			if f&FlagModal != 0 {
				scene.modal = i
			}
			focusable = focusable || scene.focus == scene.handle(i)
			sense := scene.kinds[m.kind[i]].Sense
			if f&FlagScrollable != 0 {
				sense |= gesture.SenseScroll
			}
			if sense != 0 {
				hits.add(m.box[i].Intersect(m.clip[i]), sense, i)
			}
			for c := m.links[i].last; c >= 0; c = m.links[c].prev {
				if m.flags[c]&FlagHidden == 0 && !floating(&m.style[c]) {
					stack = append(stack, c)
				}
			}
		}
	}
	// Focus can only stay on nodes that can receive input: not under a hidden or disabled
	// ancestor, nor outside the modal node.
	if f, err := scene.resolve(scene.focus); err == nil && (!focusable || !scene.inModal(f)) {
		scene.changeFocus(0)
	}
}

// Paint appends the draw commands of the UI to l, laying it out first if needed.
// It returns l's error, i.e. [kui.ErrShortBuffer] if l's buffers were too small.
func (scene *Scene) Paint(l *kui.DrawList) error {
	scene.ensureLayout()
	m := scene.mem
	for _, r := range m.roots {
		l.SetZ(r.z)
		rootClipped := r.node != scene.root && m.style[r.node].Floating.Clip
		if rootClipped {
			l.PushClip(scene.handle(r.node), m.clip[r.node])
		}
		stack := append(m.stack[:0], r.node)
		for len(stack) > 0 {
			v := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if v < 0 { // Exit node.
				scene.paintExit(^v, l)
				continue
			}
			i := v
			isClip := clips(&m.style[i])
			if scene.visible(i) {
				if isClip {
					l.PushClip(scene.handle(i), m.box[i])
				}
				h := scene.handle(i)
				if paint := scene.kinds[m.kind[i]].Paint; paint != nil {
					paint(scene, h, l)
				} else {
					scene.PaintBox(h, l)
				}
			} else if isClip {
				continue // Node and all its children are clipped away.
			}
			stack = append(stack, ^i)
			for c := m.links[i].last; c >= 0; c = m.links[c].prev {
				if m.flags[c]&FlagHidden == 0 && !floating(&m.style[c]) {
					stack = append(stack, c)
				}
			}
		}
		if rootClipped {
			l.PopClip(scene.handle(r.node))
		}
	}
	l.SetZ(0)
	scene.needPaint = false
	return l.Err()
}

// paintExit draws what goes over node i's children.
func (scene *Scene) paintExit(i int32, l *kui.DrawList) {
	m := scene.mem
	h := scene.handle(i)
	if clips(&m.style[i]) {
		l.PopClip(h)
	}
	if !scene.visible(i) {
		return
	}
	if after := scene.kinds[m.kind[i]].PaintOver; after != nil {
		after(scene, h, l)
	}
	vis := &m.visual[i]
	if vis.Border != (kui.DrawBorders{}) && vis.BorderColor.A > 0 {
		l.Border(h, m.box[i], vis.BorderColor, vis.Border, vis.Radius)
	}
}

// visible reports whether some of node i's box is inside its clip rectangle.
func (scene *Scene) visible(i int32) bool {
	return !scene.mem.box[i].Intersect(scene.mem.clip[i]).Empty()
}

// PaintBox draws node n's background and text. It is the default painter of kinds without one.
func (scene *Scene) PaintBox(n kui.Handle, l *kui.DrawList) {
	i, err := scene.resolve(n)
	if err != nil {
		return
	}
	vis := &scene.mem.visual[i]
	if vis.Background.A > 0 {
		l.Rect(n, scene.mem.box[i], vis.Background, vis.Radius)
	}
	scene.PaintText(n, l, vis.Text.Color)
}

// PaintText draws the text of node n with the given color.
func (scene *Scene) PaintText(n kui.Handle, l *kui.DrawList, c kui.Color) {
	i, err := scene.resolve(n)
	if err != nil || scene.mem.text[i].len == 0 {
		return
	}
	s := scene.textOf(i)
	st := &scene.mem.visual[i].Text
	for _, line := range scene.mem.tree.NodeLines(int(i)) {
		end := line.Start + line.Len
		if end > uint32(len(s)) {
			continue
		}
		l.Text(n, line.Rect, c, s[line.Start:end], st.FontID, st.Size, st.LetterSpacing, st.LineHeight)
	}
}
