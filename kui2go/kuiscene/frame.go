package kuiscene

import (
	"github.com/soypat/kui"
	"github.com/soypat/kui/gesture"
)

// Layout recomputes the layout if the tree changed. It is called automatically by Step,
// Paint and HitTest; calling it directly is only needed to query boxes after modifying the tree.
func (k *Scene) Layout() error {
	k.ensureLayout()
	return k.layoutErr
}

func (k *Scene) ensureLayout() {
	if !k.needLayout {
		return
	}
	k.needLayout = false
	k.needPaint = true
	k.layoutErr = k.replay()
	k.computeGeometry()
}

func clips(s *kui.LayoutStyle) bool { return s.ClipX || s.ClipY }

func floating(s *kui.LayoutStyle) bool { return s.Floating.AttachTo != kui.AttachNone }

// replay lists the visible tree in pre-order for the layout engine, lays it out and collects
// the paint roots: the root node and floating nodes, stable sorted by z.
func (k *Scene) replay() error {
	m := k.m
	t := &m.tree
	t.Order = t.Order[:0]
	t.TextBytes = m.arena
	m.roots = append(m.roots[:0], root{node: k.root})
	stack := append(m.stack[:0], k.root)
	for len(stack) > 0 {
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if v < 0 {
			v = ^v
			t.Subtree[v] = int32(len(t.Order)) - t.Subtree[v] // Subtree held v's position.
			continue
		}
		style := &m.style[v]
		if f := &style.Floating; f.AttachTo == kui.AttachNode && !k.Valid(f.Node) {
			continue // Attached to a removed node, whose index may now be another node's.
		}
		t.Subtree[v] = int32(len(t.Order))
		t.Order = append(t.Order, v)
		sp := m.text[v]
		t.Text[v] = kui.Span{Start: sp.off, Len: sp.len}
		if sp.len > 0 {
			t.TextStyle[v] = m.visual[v].Text
		}
		if v != k.root && floating(style) && len(m.roots) < cap(m.roots) {
			m.roots = append(m.roots, root{node: v, z: style.Floating.Z})
		}
		stack = append(stack, ^v)
		for c := m.links[v].last; c >= 0; c = m.links[c].prev {
			if m.flags[c]&FlagHidden == 0 {
				stack = append(stack, c)
			}
		}
	}
	err := k.engine.Layout(t, k.viewport)
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
func (k *Scene) childClip(i int32) kui.Rect {
	if clips(&k.m.style[i]) {
		return k.m.clip[i].Intersect(k.m.box[i])
	}
	return k.m.clip[i]
}

func (k *Scene) computeGeometry() {
	m := k.m
	viewport := kui.Rect{W: k.viewport.W, H: k.viewport.H}
	// Clip rectangles in pre-order, so parents' are known.
	for _, i := range m.tree.Order {
		style := &m.style[i]
		p := m.links[i].parent
		switch {
		case i == k.root:
			m.clip[i] = viewport
		case floating(style):
			m.clip[i] = viewport
			if style.Floating.Clip {
				switch style.Floating.AttachTo {
				case kui.AttachParent:
					m.clip[i] = k.childClip(p)
				case kui.AttachNode:
					if t, err := k.resolve(style.Floating.Node); err == nil {
						m.clip[i] = m.clip[t]
					}
				}
			}
		default:
			m.clip[i] = k.childClip(p)
		}
	}
	// Hit index in paint order. The last modal node visited becomes the active one.
	hits := &m.hits
	hits.n = 0
	k.modal = -1
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
				k.modal = i
			}
			focusable = focusable || k.focus == k.handle(i)
			sense := k.kinds[m.kind[i]].Sense
			if f&FlagScroll != 0 {
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
	if f, err := k.resolve(k.focus); err == nil && (!focusable || !k.inModal(f)) {
		k.changeFocus(0)
	}
}

// Paint appends the draw commands of the UI to l, laying it out first if needed.
// It returns l's error, i.e. [kui.ErrShortBuffer] if l's buffers were too small.
func (k *Scene) Paint(l *kui.DrawList) error {
	k.ensureLayout()
	m := k.m
	for _, r := range m.roots {
		l.SetZ(r.z)
		rootClipped := r.node != k.root && m.style[r.node].Floating.Clip
		if rootClipped {
			l.PushClip(k.handle(r.node), m.clip[r.node])
		}
		stack := append(m.stack[:0], r.node)
		for len(stack) > 0 {
			v := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if v < 0 { // Exit node.
				k.paintExit(^v, l)
				continue
			}
			i := v
			isClip := clips(&m.style[i])
			if k.visible(i) {
				if isClip {
					l.PushClip(k.handle(i), m.box[i])
				}
				h := k.handle(i)
				if paint := k.kinds[m.kind[i]].Paint; paint != nil {
					paint(k, h, l)
				} else {
					k.PaintBox(h, l)
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
			l.PopClip(k.handle(r.node))
		}
	}
	l.SetZ(0)
	k.needPaint = false
	return l.Err()
}

// paintExit draws what goes over node i's children.
func (k *Scene) paintExit(i int32, l *kui.DrawList) {
	m := k.m
	h := k.handle(i)
	if clips(&m.style[i]) {
		l.PopClip(h)
	}
	if !k.visible(i) {
		return
	}
	if after := k.kinds[m.kind[i]].PaintAfter; after != nil {
		after(k, h, l)
	}
	vis := &m.visual[i]
	if vis.Border != (kui.DrawBorders{}) && vis.BorderColor.A > 0 {
		l.Border(h, m.box[i], vis.BorderColor, vis.Border, vis.Radius)
	}
}

// visible reports whether some of node i's box is inside its clip rectangle.
func (k *Scene) visible(i int32) bool { return !k.m.box[i].Intersect(k.m.clip[i]).Empty() }

// PaintBox draws node n's background and text. It is the default painter of kinds without one.
func (k *Scene) PaintBox(n kui.Handle, l *kui.DrawList) {
	i, err := k.resolve(n)
	if err != nil {
		return
	}
	vis := &k.m.visual[i]
	if vis.Background.A > 0 {
		l.Rect(n, k.m.box[i], vis.Background, vis.Radius)
	}
	k.PaintText(n, l, vis.Text.Color)
}

// PaintText draws the text of node n with the given color.
func (k *Scene) PaintText(n kui.Handle, l *kui.DrawList, c kui.Color) {
	i, err := k.resolve(n)
	if err != nil || k.m.text[i].len == 0 {
		return
	}
	s := k.textOf(i)
	st := &k.m.visual[i].Text
	for _, line := range k.m.tree.NodeLines(int(i)) {
		end := line.Start + line.Len
		if end > uint32(len(s)) {
			continue
		}
		l.Text(n, line.Rect, c, s[line.Start:end], st.FontID, st.Size, st.LetterSpacing, st.LineHeight)
	}
}
