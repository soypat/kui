// Package kuiglay implements [kui.LayoutEngine] with glay, a Go port of the clay layout library.
//
// Clay is built for an application that lays out its whole UI every frame, with capacities
// sized far above what it uses. A kui kernel instead lays out only when its tree changes, with
// capacities sized to its needs. The [Engine] bridges the two, so that its layouts do not depend
// on the layouts before them:
//   - glay's text measurement cache and element ID hash map keep entries from earlier layouts
//     and cannot recover once full. When either fills, the engine re-initializes glay, reusing
//     its memory, and lays out again: only a tree too large on its own fails with [kui.ErrCapacity].
//   - glay places floating nodes in ascending Z, the main tree at Z 0, and a floating node
//     attached to a node placed after it is positioned against that node's box from the previous
//     layout. The engine finds such nodes in the tree and lays out again as many times as needed
//     for every node to be placed against boxes of the same tree.
//   - glay keeps the scroll state of each clipping node across layouts, which kui does not use.
//     The engine discards it before each layout.
//   - glay takes a size of 0 as unbounded. The engine sizes a [kui.Fixed](0) axis to the
//     smallest float32 instead, which an aspect ratio never overrides, as for any other fixed
//     size, and writes sizes that small as 0. An aspect ratio derives an axis only when it is
//     exactly 0, so one next to or inside a Fixed(0) node may still be applied where it would
//     not be for a size of exactly 0, or the other way around.
//
// glay's geometry is clay's, see [kui.LayoutEngine]. glay also rounds padding and gaps to whole
// pixels, clamping negative ones to 0, and supports at most 100 clipping nodes per tree.
package kuiglay

import (
	"math"
	"unsafe"

	"github.com/soypat/glay"
	"github.com/soypat/kui"
)

const (
	// maxClippingNodes is the number of scroll containers glay keeps, fixed as in clay.
	maxClippingNodes = 100
	// renderCommandCount sizes glay's render commands, which the engine does not read.
	renderCommandCount = 16
)

// Config configures an [Engine].
type Config struct {
	// MaxElements is the maximum number of nodes plus nodes with text per layout.
	MaxElements int
	// MaxLines is the maximum number of lines of text per layout, which wrapping text must fit
	// in before its lines are written to [kui.Tree.Lines]. Defaults to MaxElements.
	MaxLines int
	// MaxMeasureWords sizes glay's text measurement cache, which must hold the words of all
	// texts of a layout. See [glay.Config].
	MaxMeasureWords int
	// Measurer measures text. Required to lay out text.
	Measurer kui.Measurer
}

// Engine is a [kui.LayoutEngine] backed by a [glay.Context], which it declares each tree to.
// It must not be copied after Configure.
type Engine struct {
	ctx      glay.Context
	cfg      glay.Config // glay's configuration, to re-initialize it.
	measurer kui.Measurer
	decl     glay.ElementDeclaration
	style    kui.TextStyle
	// Scratch indexed by position in Order.
	textID []uint32 // glay ID of the node's text element, zero for none.
	root   []int32  // Position of the floating node holding the node, 0 for the main tree.
	parent []int32  // Position of the node's parent, -1 for the root.
	target []int32  // Root holding the node a floating node attaches to, -1 for none.
	need   []int32  // Layouts needed to place the floating node at this position.
	stack  []int32
	boxes  []glay.BoundingBox // Line boxes of a text.
	// Needs of the last layout, see NeededElements.
	needElements, needWords int
}

var _ kui.LayoutEngine = (*Engine)(nil)

// Configure allocates the engine's memory. It may be called again to resize it.
func (e *Engine) Configure(cfg Config) error {
	maxLines := cfg.MaxLines
	if maxLines == 0 {
		maxLines = cfg.MaxElements
	}
	if cfg.MaxElements <= 0 || maxLines < 0 || cfg.MaxMeasureWords < 0 {
		return kui.ErrInvalidConfig
	}
	e.measurer = cfg.Measurer
	e.cfg = glay.Config{
		MaxElementCount:              cfg.MaxElements + 2, // Root element and clay's reserved slot.
		MaxMeasureTextCacheWordCount: cfg.MaxMeasureWords,
		MaxRenderCommandCount:        renderCommandCount,
		MaxWrappedTextLineCount:      maxLines,
		MeasureText:                  e.measure,
	}
	if err := e.ctx.Initialize(e.cfg); err != nil {
		return kui.ErrInvalidConfig
	}
	e.textID = resize(e.textID, cfg.MaxElements)
	e.root = resize(e.root, cfg.MaxElements)
	e.parent = resize(e.parent, cfg.MaxElements)
	e.target = resize(e.target, cfg.MaxElements)
	e.need = resize(e.need, cfg.MaxElements)
	e.stack = resize(e.stack, cfg.MaxElements)
	e.boxes = resize(e.boxes, maxLines)
	return nil
}

func resize[T any](s []T, n int) []T {
	if cap(s) >= n {
		return s[:0:n]
	}
	return make([]T, 0, n)
}

// Glay returns the underlying glay context.
func (e *Engine) Glay() *glay.Context { return &e.ctx }

// NeededElements returns the [Config.MaxElements] the last layout needed. Reconfigure with it
// when Layout returns [kui.ErrCapacity].
func (e *Engine) NeededElements() int { return e.needElements }

// NeededMeasureWords returns a [Config.MaxMeasureWords] sufficient for the last layout, an
// upper bound valid after Layout returns [kui.ErrCapacity].
func (e *Engine) NeededMeasureWords() int { return e.needWords }

// NeededLines returns a [Config.MaxLines] sufficient for the last layout, an upper bound valid
// after Layout returns [kui.ErrCapacity]: every line holds a word or ends at a newline.
func (e *Engine) NeededLines() int { return e.needWords }

func (e *Engine) measure(s string, cfg *glay.TextElementConfig, _ any) glay.Dimensions {
	if e.measurer == nil {
		return glay.Dimensions{}
	}
	e.style.FontID = cfg.FontID
	e.style.Size = cfg.FontSize
	e.style.LetterSpacing = cfg.LetterSpacing
	e.style.LineHeight = cfg.LineHeight
	w, h := e.measurer.Measure(s, &e.style)
	return glay.Dimensions{Width: w, Height: h}
}

// id returns the glay element ID of node n. IDs are never zero.
func id(n int32) glay.ElementID { return glay.IDFromUint(uint32(n) + 1) }

func floating(s *kui.LayoutStyle) bool { return s.Floating.AttachTo != kui.AttachNone }

// Layout implements [kui.LayoutEngine]. It returns the error of [kui.Tree.Validate] for a
// malformed tree and [kui.ErrCapacity] for one exceeding the engine's configuration or glay's
// 100 clipping nodes, writing nothing. glay's other errors, i.e. a floating node attached to a
// node not listed, leave a valid layout and are not reported.
func (e *Engine) Layout(t *kui.Tree, viewport kui.Size) error {
	if err := t.Validate(); err != nil {
		return err
	}
	e.needElements = len(t.Order)
	clipping := 0
	for _, n := range t.Order {
		if t.Text[n].Len > 0 {
			e.needElements++
		}
		if s := &t.Style[n]; s.ClipX || s.ClipY {
			clipping++
		}
	}
	if e.needElements > cap(e.textID) || clipping > maxClippingNodes {
		e.computeNeeds(t)
		return kui.ErrCapacity
	}
	passes := e.passes(t)
	stale, err := e.run(t, viewport, passes)
	if stale {
		// Entries of earlier layouts filled glay's text cache or hash map: start afresh.
		if e.ctx.Initialize(e.cfg) != nil {
			return kui.ErrInvalidConfig // Unreachable: the configuration was valid.
		}
		stale, err = e.run(t, viewport, passes)
		if stale {
			err = kui.ErrCapacity
		}
	}
	if err != nil {
		e.computeNeeds(t)
		return err
	}
	return e.write(t)
}

// run lays out t the given number of times. It reports whether glay's text cache or hash map
// filled, which entries of earlier layouts may cause, and returns [kui.ErrCapacity] if
// elements or wrapped lines did not fit.
func (e *Engine) run(t *kui.Tree, viewport kui.Size, passes int) (stale bool, err error) {
	for range passes {
		e.declare(t, viewport)
		// EndLayout's errors are told apart below; other errors leave the layout valid, and
		// render commands, which the engine does not read, may exceed their capacity.
		e.ctx.EndLayout()
		w := e.ctx.Warnings()
		switch {
		case w.MaxElementsExceeded, w.MaxWrappedTextLinesExceeded:
			return false, kui.ErrCapacity
		case w.MaxTextMeasureCacheExceeded, w.HashMapCapacityExceeded:
			return true, nil
		}
	}
	return false, nil
}

// declare declares t to glay, closing each node at the end of its subtree.
func (e *Engine) declare(t *kui.Tree, viewport kui.Size) {
	// Discard the scroll state of clipping nodes, rebuilt as they are declared: glay finds it by
	// declaration order, so the state of a node gone could be taken for another's.
	e.ctx.ResetScrollContainers()
	e.ctx.SetLayoutDimensions(glay.Dimensions{Width: viewport.W, Height: viewport.H})
	e.ctx.BeginLayout()
	textID := e.textID[:len(t.Order)]
	open := e.stack[:0]
	for p, n := range t.Order {
		for len(open) > 0 && t.SubtreeEnd(int(open[len(open)-1])) <= p {
			open = open[:len(open)-1]
			e.ctx.CloseElement()
		}
		e.ctx.OpenElementWithID(id(n))
		toDecl(&e.decl, &t.Style[n])
		if p == 0 {
			e.decl.Floating = glay.FloatingElementConfig{} // The root fills the viewport, as in LayoutBasic.
		}
		e.ctx.ConfigureOpenElement(&e.decl)
		textID[p] = 0
		if sp := t.Text[n]; sp.Len > 0 {
			textID[p] = e.ctx.OpenTextElement(unsafe.String(&t.TextBytes[sp.Start], int(sp.Len)), textConfig(&t.TextStyle[n])).ID
		}
		open = append(open, int32(p))
	}
	for range open {
		e.ctx.CloseElement()
	}
}

// write writes the outputs of the last layout to t. It returns [kui.ErrShortBuffer] if lines
// did not fit t.Lines.
func (e *Engine) write(t *kui.Tree) error {
	for _, n := range t.Order {
		bb, _ := e.ctx.GetElementData(id(n))
		t.Box[n] = kui.Rect{X: bb.X, Y: bb.Y, W: collapsed(bb.Width), H: collapsed(bb.Height)}
		t.LineSpan[n] = kui.Span{}
		if s := &t.Style[n]; s.ClipX || s.ClipY {
			cd := e.ctx.GetScrollContainerData(id(n)).ContentDimensions
			t.Content[n] = kui.Size{W: collapsed(cd.Width), H: collapsed(cd.Height)}
		}
	}
	lines := t.Lines[:0]
	textID := e.textID[:len(t.Order)]
	var err error
	for p, n := range t.Order {
		if textID[p] == 0 {
			continue
		}
		tid := glay.IDFromUint(textID[p])
		wrapped := e.ctx.GetTextLines(tid)
		boxes := e.ctx.AppendTextLineBoxes(e.boxes[:0], tid)
		sp := &t.LineSpan[n]
		sp.Start = uint32(len(lines))
		for i := range wrapped {
			if len(wrapped[i].Line) == 0 {
				continue // Empty lines take room but are not written, as in LayoutBasic.
			} else if len(lines) == cap(lines) {
				err = kui.ErrShortBuffer
				break
			}
			bb := boxes[i]
			lines = append(lines, kui.TextLine{
				Rect:  kui.Rect{X: bb.X, Y: bb.Y, W: bb.Width, H: bb.Height},
				Start: uint32(wrapped[i].Offset),
				Len:   uint32(len(wrapped[i].Line)),
			})
			sp.Len++
		}
		if sp.Len == 0 {
			*sp = kui.Span{}
		}
		if err != nil {
			break
		}
	}
	t.Lines = lines
	return err
}

// computeNeeds sets the needs reported after a layout of t fails.
func (e *Engine) computeNeeds(t *kui.Tree) {
	words := 1 // Clay reserves a word slot.
	for _, n := range t.Order {
		sp := t.Text[n]
		if sp.Len == 0 {
			continue
		}
		// glay keeps a word per space-terminated word, and up to two per newline: the word
		// before it and an empty one for the newline itself.
		words++
		for _, c := range t.TextBytes[sp.Start : sp.Start+sp.Len] {
			switch c {
			case ' ':
				words++
			case '\n':
				words += 2
			}
		}
	}
	e.needWords = words
}

// passes returns the number of glay layouts of t needed for each floating node to be placed
// against the box of its target in the same tree. glay places the main tree, then floating
// subtrees in ascending Z with ties in declaration order, and positions a floating node against
// the box its target was last given: by the previous layout if the target's subtree is placed
// after the floating node. Each further layout resolves one more such link of a chain.
func (e *Engine) passes(t *kui.Tree) int {
	n := len(t.Order)
	root, parent, target, need := e.root[:n], e.parent[:n], e.target[:n], e.need[:n]
	open := e.stack[:0]
	floats := 0
	for p, v := range t.Order {
		for len(open) > 0 && t.SubtreeEnd(int(open[len(open)-1])) <= p {
			open = open[:len(open)-1]
		}
		parent[p] = -1
		root[p] = 0
		if len(open) > 0 {
			parent[p] = open[len(open)-1]
			root[p] = root[parent[p]]
		}
		if p > 0 && floating(&t.Style[v]) {
			root[p] = int32(p)
			floats++
		}
		open = append(open, int32(p))
	}
	if floats == 0 {
		return 1
	}
	// Find the root holding the target of each floating node.
	for p, v := range t.Order {
		target[p] = -1
		need[p] = 0
		if root[p] != int32(p) || p == 0 {
			continue
		}
		f := &t.Style[v].Floating
		switch f.AttachTo {
		case kui.AttachParent:
			target[p] = root[parent[p]]
		case kui.AttachRoot:
			target[p] = 0
		case kui.AttachNode:
			for q, m := range t.Order {
				if int(m) == f.Node.Index() {
					target[p] = root[q]
					break
				}
			}
		}
		if target[p] == int32(p) {
			target[p] = -1 // Attached to a node in its own subtree: more layouts do not help.
		}
	}
	// Each floating node attaches to at most one root, so following targets from a floating node
	// yields a chain ending at the main tree, at a node without a target or in a cycle. Compute
	// the needs of a chain's nodes from its end back. need is 0 for nodes not visited yet and -1
	// for nodes of the chain being followed.
	need[0] = 1
	passes := 1
	for p := range t.Order {
		if root[p] != int32(p) || need[p] != 0 {
			continue
		}
		chain := e.stack[:0]
		for q := int32(p); q >= 0 && need[q] == 0; q = target[q] {
			need[q] = -1
			chain = append(chain, q)
		}
		for i := len(chain) - 1; i >= 0; i-- {
			q := chain[i]
			nq := int32(1)
			if tq := target[q]; tq >= 0 && need[tq] > 0 {
				nq = need[tq]
				if !e.placedBefore(t, tq, q) {
					nq++
				}
			} // Otherwise no target, or the link closing a cycle, which more layouts do not resolve.
			need[q] = nq
			passes = max(passes, int(nq))
		}
	}
	return passes
}

// placedBefore reports whether glay places the root at position a before the one at position b.
func (e *Engine) placedBefore(t *kui.Tree, a, b int32) bool {
	za, zb := e.rootZ(t, a), e.rootZ(t, b)
	return za < zb || (za == zb && a < b)
}

// rootZ returns the Z of the root at position p: 0 for the main tree.
func (e *Engine) rootZ(t *kui.Tree, p int32) int16 {
	if p == 0 {
		return 0
	}
	return t.Style[t.Order[p]].Floating.Z
}

func textConfig(st *kui.TextStyle) glay.TextElementConfig {
	return glay.TextElementConfig{
		TextColor:     glay.Color{R: float32(st.Color.R), G: float32(st.Color.G), B: float32(st.Color.B), A: float32(st.Color.A)},
		FontID:        st.FontID,
		FontSize:      st.Size,
		LetterSpacing: st.LetterSpacing,
		LineHeight:    st.LineHeight,
		WrapMode:      glay.TextElementConfigWrapMode(st.Wrap),
		TextAlignment: glay.TextAlignment(st.Align),
	}
}

func pixels(v kui.Scalar) uint16 {
	return uint16(min(max(math.Round(float64(v)), 0), math.MaxUint16))
}

// collapsed returns size, or 0 for the sizes toAxis gives Fixed(0) axes and those derived
// from them, far below a pixel.
func collapsed(size float32) float32 {
	if size > -1e-30 && size < 1e-30 {
		return 0
	}
	return size
}

func toAxis(s kui.Sizing) glay.SizingAxis {
	switch s.Kind {
	case kui.SizeGrow:
		return glay.SizingAxisGrow(s.Min, s.Max)
	case kui.SizeFixed:
		if s.Min <= 0 {
			// glay takes a maximum of 0 as unbounded, which would size the node to fit, and
			// derives a size of exactly 0 from an aspect ratio.
			return glay.SizingAxis{MinMax: glay.SizingMinMax{Min: math.SmallestNonzeroFloat32, Max: math.SmallestNonzeroFloat32}, Type: glay.SizingFixed}
		}
		return glay.SizingAxisFixed(s.Min)
	case kui.SizePercent:
		return glay.SizingAxisPercent(s.Percent)
	}
	return glay.SizingAxisFit(s.Min, s.Max)
}

var alignX = [...]glay.LayoutAlignmentX{kui.AlignLeft: glay.AlignXLeft, kui.AlignCenter: glay.AlignXCenter, kui.AlignRight: glay.AlignXRight}
var alignY = [...]glay.LayoutAlignmentY{kui.AlignTop: glay.AlignYTop, kui.AlignMiddle: glay.AlignYCenter, kui.AlignBottom: glay.AlignYBottom}

func toDecl(d *glay.ElementDeclaration, s *kui.LayoutStyle) {
	*d = glay.ElementDeclaration{
		Layout: glay.LayoutConfig{
			Sizing:   glay.Sizing{Width: toAxis(s.Width), Height: toAxis(s.Height)},
			Padding:  glay.Padding{Left: pixels(s.Padding.Left), Right: pixels(s.Padding.Right), Top: pixels(s.Padding.Top), Bottom: pixels(s.Padding.Bottom)},
			ChildGap: pixels(s.Gap),
		},
		AspectRatio: glay.AspectRatioElementConfig{AspectRatio: s.AspectRatio},
		Clip:        glay.ClipElementConfig{Horizontal: s.ClipX, Vertical: s.ClipY, ChildOffset: glay.Vector2{X: s.Scroll.X, Y: s.Scroll.Y}},
	}
	if s.Direction == kui.TopToBottom {
		d.Layout.LayoutDirection = glay.TopToBottom
	}
	if int(s.AlignX) < len(alignX) {
		d.Layout.ChildAlignment.X = alignX[s.AlignX]
	}
	if int(s.AlignY) < len(alignY) {
		d.Layout.ChildAlignment.Y = alignY[s.AlignY]
	}
	if f := &s.Floating; f.AttachTo != kui.AttachNone {
		fl := &d.Floating
		fl.Offset = glay.Vector2{X: f.Offset.X, Y: f.Offset.Y}
		fl.Expand = glay.Dimensions{Width: f.Expand.W, Height: f.Expand.H}
		fl.ZIndex = f.Z
		fl.AttachPoints = glay.FloatingAttachPoints{Element: glay.FloatingAttachPointType(f.Element), Parent: glay.FloatingAttachPointType(f.Parent)}
		// Pointer capture is handled by the kernel's hit testing, not glay.
		fl.PointerCaptureMode = glay.PointerCaptureModePassthrough
		switch f.AttachTo {
		case kui.AttachParent:
			fl.AttachTo = glay.AttachToParent
		case kui.AttachRoot:
			fl.AttachTo = glay.AttachToRoot
		case kui.AttachNode:
			fl.AttachTo = glay.AttachToElementWithID
			fl.ParentID = id(int32(f.Node.Index())).ID
		}
		if f.Clip {
			fl.ClipTo = glay.ClipToAttachedParent
		}
	}
}
