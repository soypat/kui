package kui

// Direction is the axis along which a node lays out its children.
type Direction uint8

const (
	LeftToRight Direction = iota // left-to-right
	TopToBottom                  // top-to-bottom
)

// SizeKind selects how a node is sized along an axis.
type SizeKind uint8

const (
	// SizeFit wraps the node's content, clamped to [Min, Max] (Max 0 means unbounded).
	SizeFit SizeKind = iota // fit
	// SizeGrow expands to fill available space shared with other growing siblings, clamped to [Min, Max].
	SizeGrow // grow
	// SizeFixed sizes the node to exactly Min.
	SizeFixed // fixed
	// SizePercent sizes the node to Percent (0..1) of its parent's size minus padding and gaps.
	SizePercent // percent
)

// Sizing controls the size of a node along one axis.
type Sizing struct {
	Kind     SizeKind
	Min, Max Scalar
	Percent  Scalar
}

// Fit returns a fit to content sizing clamped to [min, max]. A max of 0 is unbounded.
func Fit(min, max Scalar) Sizing { return Sizing{Kind: SizeFit, Min: min, Max: max} }

// Grow returns a sizing that fills available space, clamped to [min, max]. A max of 0 is unbounded.
func Grow(min, max Scalar) Sizing { return Sizing{Kind: SizeGrow, Min: min, Max: max} }

// Fixed returns a fixed sizing.
func Fixed(size Scalar) Sizing { return Sizing{Kind: SizeFixed, Min: size, Max: size} }

// Percent returns a sizing relative to the parent's size.
func Percent(p Scalar) Sizing { return Sizing{Kind: SizePercent, Percent: p} }

// VAlign is the vertical alignment of children within a node. Their horizontal alignment is an [Align].
type VAlign uint8

const (
	AlignTop    VAlign = iota // top
	AlignMiddle               // middle
	AlignBottom               // bottom
)

// AttachTo selects what a floating node is positioned relative to.
type AttachTo uint8

const (
	AttachNone   AttachTo = iota // none
	AttachParent                 // parent
	AttachRoot                   // root
	AttachNode                   // node
)

// AttachPoint is a point on a node's bounding box.
type AttachPoint uint8

const (
	AttachLeftTop      AttachPoint = iota // left-top
	AttachLeftCenter                      // left-center
	AttachLeftBottom                      // left-bottom
	AttachCenterTop                       // center-top
	AttachCenterCenter                    // center-center
	AttachCenterBottom                    // center-bottom
	AttachRightTop                        // right-top
	AttachRightCenter                     // right-center
	AttachRightBottom                     // right-bottom
)

// Floating makes a node float above others without affecting the layout of its siblings or
// parent, i.e. for tooltips, menus and dialogs.
type Floating struct {
	AttachTo AttachTo
	// Node is the node to attach to when AttachTo is AttachNode. Layout engines identify it by its
	// index ([Handle.Index]), so it must refer to a live node.
	Node Handle
	// Element is the point on the floating node placed on point Parent of the node it attaches to.
	Element, Parent AttachPoint
	Offset          Point
	Expand          Size // Grows the floating node's box without affecting its children.
	Z               int16
	// Clip clips the floating node to the clip rectangle of the node it attaches to.
	Clip bool
}

// LayoutStyle holds the layout properties of a node, following clay's element declarations.
type LayoutStyle struct {
	Direction Direction
	Width     Sizing
	Height    Sizing
	Padding   Insets
	Gap       Scalar // Space between children along Direction.
	AlignX    Align  // Horizontal alignment of children.
	AlignY    VAlign // Vertical alignment of children.
	// ClipX and ClipY clip children overflowing the node, making it a scroll container.
	ClipX, ClipY bool
	// Scroll offsets the children of a clipping node, i.e. its scroll position (usually negative).
	Scroll      Point
	Floating    Floating
	AspectRatio Scalar // Width/height ratio, 0 for none.
}

// Span is a range of Len elements starting at Start, i.e. of bytes or lines.
type Span struct {
	Start, Len uint32
}

// End returns Start+Len.
func (s Span) End() uint32 { return s.Start + s.Len }

// Tree is a node tree for a [LayoutEngine] to lay out, held in parallel slices. Every slice but
// Order is indexed by node, so a UI kernel storing its nodes in columns passes them as they are.
//
// Order lists the nodes to lay out in pre-order: engines size bottom up by iterating it
// backwards and position top down by iterating it forwards. Subtree gives the tree's shape: the
// children of the node at position p of Order follow it, each followed by its own subtree, up to
// [Tree.SubtreeEnd](p). Nodes not listed, i.e. hidden ones, take no space and their outputs are
// left unchanged.
type Tree struct {
	// Order lists the indices of the nodes to lay out in pre-order, starting with the root.
	Order []int32
	// Subtree is the number of nodes listed in each node's subtree, itself included.
	Subtree []int32
	// Style is the layout style of each node.
	Style []LayoutStyle
	// Text is the byte range of each node's text in TextBytes, empty for none, and TextStyle
	// its style.
	Text      []Span
	TextStyle []Style
	TextBytes []byte

	// Box receives the bounding box of each node.
	Box []Rect
	// Content receives the size of the content of each clipping node, including its padding.
	// Scrolling reveals the content outside the node's box.
	Content []Size
	// Lines receives the visible lines of text, up to its capacity, and LineSpan the range of
	// each node's lines in it.
	LineSpan []Span
	Lines    []Line
}

// SubtreeEnd returns the position in Order just past the subtree of the node at position p.
// Its children are found iterating c from p+1 while c < SubtreeEnd(p), with c = SubtreeEnd(c).
func (t *Tree) SubtreeEnd(p int) int { return p + int(t.Subtree[t.Order[p]]) }

// NodeLines returns the visible lines of text of node n written by the last layout.
func (t *Tree) NodeLines(n int) []Line {
	sp := t.LineSpan[n]
	if sp.End() > uint32(len(t.Lines)) {
		return nil
	}
	return t.Lines[sp.Start:sp.End()]
}

// Validate checks that t is a well formed tree whose per node slices hold every node listed
// and whose text lies within TextBytes. It returns [ErrOutOfRange] for a node or text outside
// its slices and [ErrInvalidConfig] for a malformed Subtree. Nodes listed twice are not detected.
func (t *Tree) Validate() error {
	n := len(t.Order)
	if n == 0 {
		return nil
	}
	nodes := min(len(t.Subtree), len(t.Style), len(t.Text), len(t.TextStyle), len(t.Box), len(t.Content), len(t.LineSpan))
	for _, v := range t.Order {
		if v < 0 || int(v) >= nodes {
			return ErrOutOfRange
		}
		if t.Text[v].Len > 0 && uint64(t.Text[v].Start)+uint64(t.Text[v].Len) > uint64(len(t.TextBytes)) {
			return ErrOutOfRange
		}
		if f := &t.Style[v].Floating; f.AttachTo == AttachNode && f.Node.Index() >= nodes {
			return ErrOutOfRange
		}
	}
	if t.Subtree[t.Order[0]] != int32(n) {
		return ErrInvalidConfig
	}
	// Every subtree is at least its node and the subtrees of its children tile the rest of it.
	for p := range n {
		end := t.SubtreeEnd(p)
		if end <= p || end > n {
			return ErrInvalidConfig
		}
		for c := p + 1; c < end; c = t.SubtreeEnd(c) {
			if e := t.SubtreeEnd(c); e <= c || e > end {
				return ErrInvalidConfig
			}
		}
	}
	return nil
}

// LayoutEngine computes the geometry of a [Tree]. Implementations must not allocate after
// configuration nor retain the tree; they may keep state between layouts, i.e. caches.
type LayoutEngine interface {
	// Layout lays out t within a viewport of the given size, writing the outputs of the nodes
	// it lists. It returns [ErrCapacity] if t has more nodes than the engine is configured for
	// and [ErrShortBuffer] if the visible lines of text did not fit t.Lines, the layout being
	// otherwise complete.
	Layout(t *Tree, viewport Size) error
}
