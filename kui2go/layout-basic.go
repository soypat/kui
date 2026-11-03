package kui2go

import (
	"math"
	"unsafe"

	"github.com/soypat/kui"
)

var _ kui.LayoutEngine = (*LayoutBasic)(nil)

// LayoutBasicConfig configures a [LayoutBasic].
type LayoutBasicConfig struct {
	// MaxNodes is the maximum node index plus one of the trees laid out.
	MaxNodes int
	// Measurer measures text. Required to lay out text.
	Measurer kui.Measurer
}

// LayoutBasic is a [kui.LayoutEngine] running clay's layout algorithm directly over a
// [kui.Tree]: passes over its slices instead of clay's element declarations, hash map and render
// commands. It gives the geometry clay does, but for padding and gaps not being rounded to whole
// pixels. A floating node attached to a node not laid out is placed relative to the origin.
//
// Configure it before use. It does not allocate afterwards.
type LayoutBasic struct {
	measurer kui.Measurer
	nodes    []basicNode
	kids     []int32 // Children of the node being sized: node indices, and ^n for node n's text.
	resize   []int32 // Children of the node being sized that may be resized.
	roots    []int32 // Positions in Order of the floating nodes.
	stack    []int32
	mark     uint32 // Marks the nodes listed by the current layout.
}

// basicNode is the state of a node during a layout. Its own size is held in Tree.Box.
type basicNode struct {
	min       kui.Size // Size below which the node is not compressed.
	text      kui.Rect // Box of the node's text element.
	textMin   float32  // Width of the text's widest word, below which it is not compressed.
	natural   kui.Size // Size of the text laid out on a single line.
	newlines  bool     // The text contains newlines.
	lineH     float32  // Height of a line of the text.
	aspectMax float32  // Height cap set by an aspect ratio after sizing widths, if capped.
	capped    bool
	parent    int32
	mark      uint32 // Equal to LayoutBasic.mark if the node is laid out.
	placed    uint32 // Equal to LayoutBasic.mark once the node is positioned.
}

// Configure allocates the engine's memory. It may be called again to resize it.
func (e *LayoutBasic) Configure(cfg LayoutBasicConfig) error {
	if cfg.MaxNodes <= 0 {
		return kui.ErrInvalidConfig
	}
	e.measurer = cfg.Measurer
	e.nodes = resizeSlice(e.nodes, cfg.MaxNodes)[:cfg.MaxNodes]
	e.kids = resizeSlice(e.kids, cfg.MaxNodes+1)
	e.resize = resizeSlice(e.resize, cfg.MaxNodes+1)
	e.roots = resizeSlice(e.roots, cfg.MaxNodes)
	e.stack = resizeSlice(e.stack, 2*cfg.MaxNodes)
	return nil
}

func resizeSlice[T any](s []T, n int) []T {
	if cap(s) >= n {
		return s[:0:n]
	}
	return make([]T, 0, n)
}

const (
	basicEps      = 0.01
	basicMaxFloat = math.MaxFloat32
)

func floatEq(a, b float32) bool { return a-b < basicEps && a-b > -basicEps }

func floating(s *kui.LayoutStyle) bool { return s.Floating.AttachTo != kui.AttachNone }

// Layout implements [kui.LayoutEngine]. It returns the error of [kui.Tree.Validate] for a
// malformed tree and [kui.ErrCapacity] for a node index of at least [LayoutBasicConfig.MaxNodes],
// writing nothing.
func (e *LayoutBasic) Layout(t *kui.Tree, viewport kui.Size) error {
	if err := t.Validate(); err != nil {
		return err
	} else if len(t.Order) == 0 {
		return nil
	}
	for _, n := range t.Order {
		if int(n) >= len(e.nodes) {
			return kui.ErrCapacity
		}
	}
	e.mark++
	e.link(t)
	e.fit(t)
	e.sizeAxis(t, viewport, true)
	err := e.wrap(t)
	// Like clay, aspect ratios scale nodes laid out in a flow, not floating ones.
	for p, n := range t.Order {
		if ar := t.Style[n].AspectRatio; ar != 0 && (p == 0 || !floating(&t.Style[n])) {
			t.Box[n].H = t.Box[n].W / ar
			e.nodes[n].aspectMax, e.nodes[n].capped = t.Box[n].H, true
		}
	}
	e.propagateHeights(t)
	e.sizeAxis(t, viewport, false)
	for p, n := range t.Order {
		if ar := t.Style[n].AspectRatio; ar != 0 && (p == 0 || !floating(&t.Style[n])) {
			t.Box[n].W = ar * t.Box[n].H
		}
	}
	e.place(t, viewport)
	return err
}

// link records the parent of each listed node and marks it listed.
func (e *LayoutBasic) link(t *kui.Tree) {
	ends := e.stack[:0] // Pairs of subtree end and node.
	e.roots = e.roots[:0]
	for p, n := range t.Order {
		for len(ends) > 0 && int(ends[len(ends)-2]) <= p {
			ends = ends[:len(ends)-2]
		}
		nd := &e.nodes[n]
		*nd = basicNode{parent: -1, mark: e.mark}
		if len(ends) > 0 {
			nd.parent = ends[len(ends)-1]
		}
		if p > 0 && floating(&t.Style[n]) {
			e.roots = append(e.roots, int32(p))
		}
		ends = append(ends, int32(t.SubtreeEnd(p)), n)
	}
}

// children sets e.kids to the children of the node at position p laid out in its flow: its
// text, then its children that do not float.
func (e *LayoutBasic) children(t *kui.Tree, p int) []int32 {
	n := t.Order[p]
	kids := e.kids[:0]
	if t.Text[n].Len > 0 {
		kids = append(kids, ^n)
	}
	for c := p + 1; c < t.SubtreeEnd(p); c = t.SubtreeEnd(c) {
		if m := t.Order[c]; !floating(&t.Style[m]) {
			kids = append(kids, m)
		}
	}
	e.kids = kids
	return kids
}

// size returns the size of child c along an axis: a node, or the text of node ^c.
func (e *LayoutBasic) size(t *kui.Tree, c int32, x bool) *float32 {
	var r *kui.Rect
	if c < 0 {
		r = &e.nodes[^c].text
	} else {
		r = &t.Box[c]
	}
	if x {
		return &r.W
	}
	return &r.H
}

func (e *LayoutBasic) minSize(c int32, x bool) float32 {
	switch {
	case c < 0 && x:
		return e.nodes[^c].textMin
	case c < 0:
		return e.nodes[^c].lineH // Wrapped text compresses down to a line.
	case x:
		return e.nodes[c].min.W
	}
	return e.nodes[c].min.H
}

// sizing returns the sizing of child c along an axis. Text is sized to fit.
func sizing(t *kui.Tree, c int32, x bool) kui.Sizing {
	if c < 0 {
		return kui.Sizing{}
	} else if x {
		return t.Style[c].Width
	}
	return t.Style[c].Height
}

// maxOf returns the maximum size of child c along an axis, which an aspect ratio may lower.
func (e *LayoutBasic) maxOf(t *kui.Tree, c int32, x bool) float32 {
	if !x && c >= 0 && e.nodes[c].capped {
		return e.nodes[c].aspectMax
	}
	return maxSize(sizing(t, c, x))
}

// maxSize returns the maximum size of a sizing that is not a percentage: unbounded for 0, but
// Min for a fixed sizing, so that Fixed(0) is 0.
func maxSize(s kui.Sizing) float32 {
	if s.Kind == kui.SizeFixed {
		return max(s.Min, 0)
	} else if s.Max <= 0 {
		return basicMaxFloat
	}
	return s.Max
}

func clamp(v float32, s kui.Sizing) float32 { return min(max(v, s.Min), maxSize(s)) }

func resizable(t *kui.Tree, c int32, x bool) bool {
	if c < 0 {
		return t.TextStyle[^c].Wrap == kui.WrapWords
	}
	k := sizing(t, c, x).Kind
	return k != kui.SizePercent && k != kui.SizeFixed
}

func fixAspect(t *kui.Tree, c int32) {
	if c < 0 {
		return
	}
	b := &t.Box[c]
	s := &t.Style[c]
	// A fixed axis is not derived, even when its size is 0.
	if ar := s.AspectRatio; ar != 0 {
		if b.W == 0 && b.H != 0 && s.Width.Kind != kui.SizeFixed {
			b.W = b.H * ar
		} else if b.W != 0 && b.H == 0 && s.Height.Kind != kui.SizeFixed {
			b.H = b.W * (1 / ar)
		}
	}
}

// fit sizes every node to its content, bottom up, as clay does when closing elements.
func (e *LayoutBasic) fit(t *kui.Tree) {
	for p := len(t.Order) - 1; p >= 0; p-- {
		n := t.Order[p]
		s := &t.Style[n]
		nd := &e.nodes[n]
		if t.Text[n].Len > 0 {
			e.measure(t, n)
		}
		padX, padY := s.Padding.Left+s.Padding.Right, s.Padding.Top+s.Padding.Bottom
		var size, min kui.Size
		kids := e.children(t, p)
		gaps := float32(max(len(kids)-1, 0)) * s.Gap
		if s.Direction == kui.LeftToRight {
			size.W, min.W = padX, padX
			for _, c := range kids {
				size.W += *e.size(t, c, true)
				size.H = max(size.H, *e.size(t, c, false)+padY)
				if !s.ClipX {
					min.W += e.minSize(c, true)
				}
				if !s.ClipY {
					min.H = max(min.H, e.minSize(c, false)+padY)
				}
			}
			size.W += gaps
			if !s.ClipX {
				min.W += gaps
			}
		} else {
			size.H, min.H = padY, padY
			for _, c := range kids {
				size.H += *e.size(t, c, false)
				size.W = max(size.W, *e.size(t, c, true)+padX)
				if !s.ClipY {
					min.H += e.minSize(c, false)
				}
				if !s.ClipX {
					min.W = max(min.W, e.minSize(c, true)+padX)
				}
			}
			size.H += gaps
			if !s.ClipY {
				min.H += gaps
			}
		}
		if s.Width.Kind != kui.SizePercent {
			size.W, min.W = clamp(size.W, s.Width), clamp(min.W, s.Width)
		} else {
			size.W = 0
		}
		if s.Height.Kind != kui.SizePercent {
			size.H, min.H = clamp(size.H, s.Height), clamp(min.H, s.Height)
		} else {
			size.H = 0
		}
		t.Box[n].W, t.Box[n].H = size.W, size.H
		nd.min = min
		fixAspect(t, n)
	}
}

// word returns the next word of s from start, as clay measures them: a word and the space
// after it, a word ending the text or a line, or the empty word of a newline (length 0) after
// which the next word starts at next. width excludes newlines.
func (e *LayoutBasic) word(s string, st *kui.TextStyle, start int, spaceW float32) (wordStart, length, next int, width, height float32, ok bool) {
	for end := start; end < len(s); end++ {
		if s[end] != ' ' && s[end] != '\n' {
			continue
		}
		if end > start {
			width, height = e.measurer.Measure(s[start:end], st)
		}
		if s[end] == ' ' {
			return start, end - start + 1, end + 1, width + spaceW, height, true
		} else if end > start {
			return start, end - start, end, width, height, true // The newline is the next word.
		}
		return end + 1, 0, end + 1, 0, 0, true
	}
	if start < len(s) {
		width, height = e.measurer.Measure(s[start:], st)
		return start, len(s) - start, len(s), width, height, true
	}
	return 0, 0, 0, 0, 0, false
}

// measure sizes node n's text on a single line.
func (e *LayoutBasic) measure(t *kui.Tree, n int32) {
	nd := &e.nodes[n]
	st := &t.TextStyle[n]
	if e.measurer == nil {
		nd.text, nd.natural, nd.textMin, nd.lineH = kui.Rect{}, kui.Size{}, 0, 0
		return
	}
	s := unsafe.String(&t.TextBytes[t.Text[n].Start], int(t.Text[n].Len))
	spaceW, _ := e.measurer.Measure(" ", st)
	var lineW, width, height, minW float32
	for i := 0; ; {
		start, length, next, w, h, ok := e.word(s, st, i, spaceW)
		if !ok {
			break
		}
		i = next
		height = max(height, h)
		if length == 0 { // Newline.
			width = max(width, lineW)
			lineW = 0
			nd.newlines = true
			continue
		}
		wordW := w
		if s[start+length-1] == ' ' {
			wordW -= spaceW
		}
		minW = max(minW, wordW)
		lineW += w
	}
	nd.natural = kui.Size{W: max(width, lineW) - float32(st.LetterSpacing), H: height}
	nd.lineH = height
	if st.LineHeight > 0 {
		nd.lineH = float32(st.LineHeight)
	}
	nd.text = kui.Rect{W: nd.natural.W, H: nd.lineH}
	nd.textMin = minW
}

// sizeAxis sizes the nodes along an axis top down, growing and compressing children to their
// parents: the tree, then the floating nodes.
func (e *LayoutBasic) sizeAxis(t *kui.Tree, viewport kui.Size, x bool) {
	// The root is the only child of a container spanning the viewport, laying out left to right.
	parentSize := viewport.H
	if x {
		parentSize = viewport.W
	}
	e.kids = append(e.kids[:0], t.Order[0])
	e.distribute(t, x, parentSize, 0, 0, x, false, e.kids)
	e.sizeSubtree(t, 0, x)
	for _, fp := range e.roots {
		// Like clay, size floating nodes along both axes in either pass, so a target resized
		// by an aspect ratio in between is followed.
		n := t.Order[fp]
		s := &t.Style[n]
		b := &t.Box[n]
		target, ok := e.attachTarget(t, n, viewport)
		b.W = sizeFloating(b.W, target.W, s.Width, ok)
		b.H = sizeFloating(b.H, target.H, s.Height, ok)
		e.sizeSubtree(t, int(fp), x)
	}
}

// sizeFloating returns the size along an axis of a floating node of the given sizing, size and
// target size, if it has a target.
func sizeFloating(size, target float32, sz kui.Sizing, hasTarget bool) float32 {
	if hasTarget {
		switch sz.Kind {
		case kui.SizeGrow:
			size = target
		case kui.SizePercent:
			size = target * sz.Percent
		}
	}
	if sz.Kind != kui.SizePercent {
		size = clamp(size, sz)
	}
	return size
}

// attachTarget returns the box floating node n is positioned relative to.
func (e *LayoutBasic) attachTarget(t *kui.Tree, n int32, viewport kui.Size) (kui.Rect, bool) {
	f := &t.Style[n].Floating
	switch f.AttachTo {
	case kui.AttachParent:
		if p := e.nodes[n].parent; p >= 0 {
			return t.Box[p], true
		}
	case kui.AttachRoot:
		return kui.Rect{W: viewport.W, H: viewport.H}, true
	case kui.AttachNode:
		if i := f.Node.Index(); i < len(e.nodes) && e.nodes[i].mark == e.mark {
			return t.Box[i], true
		}
	}
	return kui.Rect{}, false
}

// sizeSubtree sizes the children of the nodes of the subtree at position p along an axis,
// excluding the subtrees of floating nodes in it.
func (e *LayoutBasic) sizeSubtree(t *kui.Tree, p int, x bool) {
	for q, end := p, t.SubtreeEnd(p); q < end; q++ {
		n := t.Order[q]
		s := &t.Style[n]
		if q != p && floating(s) {
			q = t.SubtreeEnd(q) - 1
			continue
		}
		size, pad, clip := t.Box[n].H, s.Padding.Top+s.Padding.Bottom, s.ClipY
		if x {
			size, pad, clip = t.Box[n].W, s.Padding.Left+s.Padding.Right, s.ClipX
		}
		along := x == (s.Direction == kui.LeftToRight)
		e.distribute(t, x, size, pad, s.Gap, along, clip, e.children(t, q))
	}
}

// distribute sizes the children kids of a node along an axis, as clay does: percentages of the
// node, then growing children sharing the free space along its layout axis or compressing
// children down to their minimum when they overflow it, and across it filling or fitting it.
func (e *LayoutBasic) distribute(t *kui.Tree, x bool, parentSize, padding, gap float32, along, clip bool, kids []int32) {
	var inner float32
	paddingAndGaps := padding
	growers := 0
	res := e.resize[:0]
	for i, c := range kids {
		sz := sizing(t, c, x)
		if resizable(t, c, x) {
			res = append(res, c)
		}
		if along {
			if sz.Kind != kui.SizePercent {
				inner += *e.size(t, c, x)
			}
			if sz.Kind == kui.SizeGrow {
				growers++
			}
			if i > 0 {
				inner += gap
				paddingAndGaps += gap
			}
		} else {
			inner = max(inner, *e.size(t, c, x))
		}
	}
	for _, c := range kids {
		if sz := sizing(t, c, x); sz.Kind == kui.SizePercent {
			*e.size(t, c, x) = (parentSize - paddingAndGaps) * sz.Percent
			if along {
				inner += *e.size(t, c, x)
			}
			fixAspect(t, c)
		}
	}
	if !along {
		for _, c := range res {
			sz := sizing(t, c, x)
			limit := parentSize - padding
			if clip {
				limit = max(limit, inner) // Children of scroll containers grow to the content.
			}
			size := e.size(t, c, x)
			if sz.Kind == kui.SizeGrow {
				*size = min(limit, e.maxOf(t, c, x))
			}
			*size = max(e.minSize(c, x), min(*size, limit))
		}
		return
	}
	free := parentSize - padding - inner
	if free < 0 {
		if clip {
			return // Scroll containers do not compress their content.
		}
		e.compress(t, x, free, res)
	} else if free > 0 && growers > 0 {
		n := 0
		for _, c := range res {
			if sizing(t, c, x).Kind == kui.SizeGrow {
				res[n] = c
				n++
			}
		}
		e.grow(t, x, free, res[:n])
	}
}

// compress shrinks the largest of children res by -free in total, down to their minimum sizes.
func (e *LayoutBasic) compress(t *kui.Tree, x bool, free float32, res []int32) {
	for free < -basicEps && len(res) > 0 {
		var largest, second float32
		add := free
		for _, c := range res {
			size := *e.size(t, c, x)
			if floatEq(size, largest) {
				continue
			}
			if size > largest {
				second, largest = largest, size
			}
			if size < largest {
				second = max(second, size)
				add = second - largest
			}
		}
		add = max(add, free/float32(len(res)))
		progress := false
		for i := 0; i < len(res); i++ {
			size := e.size(t, res[i], x)
			minSize, prev := e.minSize(res[i], x), *size
			if !floatEq(*size, largest) {
				continue
			}
			*size += add
			if *size <= minSize {
				*size = minSize
				res[i] = res[len(res)-1]
				res = res[:len(res)-1]
				i--
				progress = true
			}
			free -= *size - prev
			progress = progress || *size != prev
		}
		if !progress {
			return
		}
	}
}

// grow grows the smallest of children res, up to their maximum sizes, until they take free.
func (e *LayoutBasic) grow(t *kui.Tree, x bool, free float32, res []int32) {
	for free > basicEps && len(res) > 0 {
		smallest, second := float32(basicMaxFloat), float32(basicMaxFloat)
		add := free
		for _, c := range res {
			size := *e.size(t, c, x)
			if floatEq(size, smallest) {
				continue
			}
			if size < smallest {
				second, smallest = smallest, size
			}
			if size > smallest {
				second = min(second, size)
				add = second - smallest
			}
		}
		add = min(add, free/float32(len(res)))
		progress := false
		for i := 0; i < len(res); i++ {
			size := e.size(t, res[i], x)
			maxS, prev := e.maxOf(t, res[i], x), *size
			if !floatEq(*size, smallest) {
				continue
			}
			*size += add
			if *size >= maxS {
				*size = maxS
				res[i] = res[len(res)-1]
				res = res[:len(res)-1]
				i--
				progress = true
			}
			free -= *size - prev
			progress = progress || *size != prev
		}
		if !progress {
			return
		}
	}
}

// wrap breaks text into lines at the width of its text element, as clay does, writing the lines
// to t.Lines with positions relative to the text element and sizing its height to them.
func (e *LayoutBasic) wrap(t *kui.Tree) error {
	t.Lines = t.Lines[:0]
	var err error
	for _, n := range t.Order {
		t.LineSpan[n] = kui.Span{Start: uint32(len(t.Lines))}
		if t.Text[n].Len == 0 || e.measurer == nil {
			continue
		}
		nd := &e.nodes[n]
		st := &t.TextStyle[n]
		s := unsafe.String(&t.TextBytes[t.Text[n].Start], int(t.Text[n].Len))
		lw := &lineWriter{t: t, n: n, nd: nd, st: st}
		if !nd.newlines && nd.natural.W <= nd.text.W {
			lw.add(0, len(s), nd.text.W, nd.text.H)
		} else {
			e.wrapWords(s, st, nd, lw)
			nd.text.H = nd.lineH * float32(lw.count)
		}
		if lw.dropped {
			err = kui.ErrShortBuffer
		}
	}
	return err
}

func (e *LayoutBasic) wrapWords(s string, st *kui.TextStyle, nd *basicNode, lw *lineWriter) {
	spaceW, _ := e.measurer.Measure(" ", st)
	spacing := float32(st.LetterSpacing)
	limit := nd.text.W
	var lineW float32
	var lineLen, lineStart int
	for i := 0; ; {
		start, length, next, w, _, ok := e.word(s, st, i, spaceW)
		if !ok {
			break
		}
		switch {
		case lineLen == 0 && lineW+w > limit:
			// A word too wide for a line of its own still gets one.
			lw.add(start, length, w, nd.lineH)
			i = next
			lineStart = start + length
		case length == 0 || lineW+w > limit:
			// End the line at a newline or before the word not fitting it.
			wl, n := lineW, lineLen
			if i := lineStart + lineLen - 1; n > 0 && i < len(s) && s[max(i, 0)] == ' ' {
				wl -= spaceW
				n--
			}
			lw.add(lineStart, n, wl, nd.lineH)
			if lineLen == 0 || length == 0 {
				i = next
			}
			lineW, lineLen, lineStart = 0, 0, start
		default:
			lineW += w + spacing
			lineLen += length
			i = next
		}
	}
	if lineLen > 0 {
		lw.add(lineStart, lineLen, lineW-spacing, nd.lineH)
	}
}

// lineWriter appends the lines of node n's text to t.Lines.
type lineWriter struct {
	t       *kui.Tree
	n       int32
	nd      *basicNode
	st      *kui.TextStyle
	count   int // Lines of the text, including empty and dropped ones.
	dropped bool
}

func (lw *lineWriter) add(start, length int, w, h float32) {
	y := (lw.nd.lineH-lw.nd.natural.H)/2 + float32(lw.count)*lw.nd.lineH
	lw.count++
	t := lw.t
	if length == 0 {
		return // Empty lines take room but are not written.
	} else if len(t.Lines) == cap(t.Lines) {
		lw.dropped = true
		return
	}
	var x float32
	switch lw.st.Align {
	case kui.AlignCenter:
		x = (lw.nd.text.W - w) / 2
	case kui.AlignRight:
		x = lw.nd.text.W - w
	}
	t.Lines = append(t.Lines, kui.TextLine{Rect: kui.Rect{X: x, Y: y, W: w, H: h}, Start: uint32(start), Len: uint32(length)})
	t.LineSpan[lw.n].Len++
}

// propagateHeights grows nodes to the height of their children after text wrapping, bottom up.
func (e *LayoutBasic) propagateHeights(t *kui.Tree) {
	for p := len(t.Order) - 1; p >= 0; p-- {
		n := t.Order[p]
		kids := e.children(t, p)
		if len(kids) == 0 {
			continue
		}
		s := &t.Style[n]
		lo, hi := s.Height.Min, s.Height.Max // A percentage, sized later, has a maximum of 0.
		if s.Height.Kind != kui.SizePercent {
			hi = maxSize(s.Height)
		}
		if e.nodes[n].capped {
			hi = e.nodes[n].aspectMax
		}
		pad := s.Padding.Top + s.Padding.Bottom
		h := &t.Box[n].H
		if s.Direction == kui.LeftToRight {
			for _, c := range kids {
				*h = min(max(max(*e.size(t, c, false)+pad, *h), lo), hi)
			}
		} else {
			content := pad + float32(len(kids)-1)*s.Gap
			for _, c := range kids {
				content += *e.size(t, c, false)
			}
			*h = min(max(content, lo), hi)
		}
	}
}

// place positions the nodes top down: the tree, then the floating nodes in ascending Z order,
// equal Z in tree order, each after the node it attaches to. It also writes the content sizes
// of clipping nodes and positions lines.
func (e *LayoutBasic) place(t *kui.Tree, viewport kui.Size) {
	root := t.Order[0]
	t.Box[root].X, t.Box[root].Y = 0, 0
	e.placeSubtree(t, 0)
	roots := e.roots
	for i := 1; i < len(roots); i++ { // Stable insertion sort by Z.
		for j := i; j > 0 && t.Style[t.Order[roots[j]]].Floating.Z < t.Style[t.Order[roots[j-1]]].Floating.Z; j-- {
			roots[j], roots[j-1] = roots[j-1], roots[j]
		}
	}
	// Place the first floating node whose target is placed until all are; clay places them
	// in Z order only, reading targets placed after them from the previous layout.
	for len(roots) > 0 {
		i := 0
		for i < len(roots) && !e.targetPlaced(t, t.Order[roots[i]]) {
			i++
		}
		if i == len(roots) {
			i = 0 // Attached in a cycle: place in Z order.
		}
		fp := roots[i]
		copy(roots[i:], roots[i+1:])
		roots = roots[:len(roots)-1]
		n := t.Order[fp]
		f := &t.Style[n].Floating
		target, _ := e.attachTarget(t, n, viewport)
		b := &t.Box[n]
		var pos kui.Point
		switch f.Parent {
		case kui.AttachLeftTop, kui.AttachLeftCenter, kui.AttachLeftBottom:
			pos.X = target.X
		case kui.AttachCenterTop, kui.AttachCenterCenter, kui.AttachCenterBottom:
			pos.X = target.X + target.W/2
		default:
			pos.X = target.X + target.W
		}
		switch f.Element {
		case kui.AttachCenterTop, kui.AttachCenterCenter, kui.AttachCenterBottom:
			pos.X -= b.W / 2
		case kui.AttachRightTop, kui.AttachRightCenter, kui.AttachRightBottom:
			pos.X -= b.W
		}
		switch f.Parent {
		case kui.AttachLeftTop, kui.AttachCenterTop, kui.AttachRightTop:
			pos.Y = target.Y
		case kui.AttachLeftCenter, kui.AttachCenterCenter, kui.AttachRightCenter:
			pos.Y = target.Y + target.H/2
		default:
			pos.Y = target.Y + target.H
		}
		switch f.Element {
		case kui.AttachLeftCenter, kui.AttachCenterCenter, kui.AttachRightCenter:
			pos.Y -= b.H / 2
		case kui.AttachLeftBottom, kui.AttachCenterBottom, kui.AttachRightBottom:
			pos.Y -= b.H
		}
		b.X, b.Y = pos.X+f.Offset.X, pos.Y+f.Offset.Y
		e.placeSubtree(t, int(fp))
	}
}

// targetPlaced reports whether the node floating node n attaches to is positioned, or does
// not need to be.
func (e *LayoutBasic) targetPlaced(t *kui.Tree, n int32) bool {
	f := &t.Style[n].Floating
	switch f.AttachTo {
	case kui.AttachParent:
		return e.nodes[e.nodes[n].parent].placed == e.mark
	case kui.AttachNode:
		i := f.Node.Index()
		return i >= len(e.nodes) || e.nodes[i].mark != e.mark || e.nodes[i].placed == e.mark
	}
	return true
}

// placeSubtree positions the children of the nodes of the subtree at position p, whose own
// position is set, excluding the subtrees of floating nodes in it.
func (e *LayoutBasic) placeSubtree(t *kui.Tree, p int) {
	for q, end := p, t.SubtreeEnd(p); q < end; q++ {
		n := t.Order[q]
		s := &t.Style[n]
		if q != p && floating(s) {
			q = t.SubtreeEnd(q) - 1
			continue
		}
		e.nodes[n].placed = e.mark
		b := &t.Box[n]
		size := b.Size() // Children are aligned within the size before expansion.
		if floating(s) && q != 0 {
			ex := s.Floating.Expand
			b.X, b.Y, b.W, b.H = b.X-ex.W, b.Y-ex.H, b.W+2*ex.W, b.H+2*ex.H
		}
		var scroll kui.Point
		if s.ClipX || s.ClipY {
			scroll = s.Scroll
		}
		kids := e.children(t, q)
		var content kui.Size
		ltr := s.Direction == kui.LeftToRight
		for _, c := range kids {
			w, h := *e.size(t, c, true), *e.size(t, c, false)
			if ltr {
				content.W += w
				content.H = max(content.H, h)
			} else {
				content.W = max(content.W, w)
				content.H += h
			}
		}
		gaps := float32(max(len(kids)-1, 0)) * s.Gap
		padX, padY := s.Padding.Left+s.Padding.Right, s.Padding.Top+s.Padding.Bottom
		next := kui.Point{X: s.Padding.Left, Y: s.Padding.Top}
		if ltr {
			content.W += gaps
			next.X += alignOffset(size.W-padX-content.W, s.AlignX == kui.AlignCenter, s.AlignX == kui.AlignRight)
		} else {
			content.H += gaps
			next.Y += alignOffset(size.H-padY-content.H, s.AlignY == kui.AlignMiddle, s.AlignY == kui.AlignBottom)
		}
		if s.ClipX || s.ClipY {
			t.Content[n] = kui.Size{W: content.W + padX, H: content.H + padY}
		}
		for _, c := range kids {
			w, h := *e.size(t, c, true), *e.size(t, c, false)
			if ltr {
				next.Y = s.Padding.Top + crossOffset(size.H-padY-h, s.AlignY == kui.AlignMiddle, s.AlignY == kui.AlignBottom)
			} else {
				next.X = s.Padding.Left + crossOffset(size.W-padX-w, s.AlignX == kui.AlignCenter, s.AlignX == kui.AlignRight)
			}
			x, y := b.X+next.X+scroll.X, b.Y+next.Y+scroll.Y
			if c < 0 {
				e.placeText(t, ^c, x, y)
			} else {
				t.Box[c].X, t.Box[c].Y = x, y
			}
			if ltr {
				next.X += w + s.Gap
			} else {
				next.Y += h + s.Gap
			}
		}
	}
}

// alignOffset returns the offset of children taking less room than extra along a layout axis.
func alignOffset(extra float32, center, end bool) float32 {
	switch {
	case center:
		extra /= 2
	case !end:
		extra = 0
	}
	return max(0, extra)
}

// crossOffset returns the offset of a child across a layout axis, with whitespace around it.
func crossOffset(whitespace float32, center, end bool) float32 {
	switch {
	case center:
		return whitespace / 2
	case end:
		return whitespace
	}
	return 0
}

// placeText positions node n's text element and its lines at x, y.
func (e *LayoutBasic) placeText(t *kui.Tree, n int32, x, y float32) {
	nd := &e.nodes[n]
	nd.text.X, nd.text.Y = x, y
	sp := t.LineSpan[n]
	for i := sp.Start; i < sp.End(); i++ {
		t.Lines[i].Rect.X += x
		t.Lines[i].Rect.Y += y
	}
}
