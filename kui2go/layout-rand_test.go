package kui2go

import (
	"encoding/binary"
	"flag"
	"fmt"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/soypat/kui"
)

// Random tree tests of LayoutBasic. The generator is the one of the differential test against
// glay in bindings/layout/kuiglay; here, with no reference engine, the oracles are invariants
// and relations every layout must satisfy.

var (
	flagSeed  = flag.Uint64("seed", 1, "first seed of TestLayoutBasicRandom")
	flagTrees = flag.Int("trees", 2000, "number of random trees TestLayoutBasicRandom lays out")
)

const randNodes = 24

var randWords = []string{"a", "bb", "ccc", "dddd", "eeeee", "ffffffffff"}

type randGen struct{ r *rand.Rand }

// n returns a choice in [0, k). Modulo, unlike rand's IntN, returns on any source: the zeros
// of an exhausted bytesSource choose 0.
func (g randGen) n(k int) int { return int(g.r.Uint64() % uint64(k)) }

func (g randGen) sizing() kui.Sizing {
	switch g.n(5) {
	case 0:
		return kui.Fixed(float32(g.n(80)))
	case 1:
		return kui.Grow(float32(g.n(3)*10), float32(g.n(3)*40))
	case 2:
		return kui.Percent(float32(g.n(11)) / 10)
	}
	return kui.Fit(float32(g.n(3)*10), float32(g.n(3)*60))
}

func (g randGen) text() string {
	var b []byte
	for i := range g.n(8) {
		if i > 0 {
			if g.n(6) == 0 {
				b = append(b, '\n')
			} else {
				b = append(b, ' ')
			}
		}
		b = append(b, randWords[g.n(len(randWords))]...)
	}
	return string(b)
}

// randTree returns a random tree of the nodes first to first+n-1, all listed, with every layout
// feature. Floating nodes attach to any node they do not depend on: possibly declared after them,
// in floating subtrees of higher Z or with negative Z.
func randTree(g randGen, first, n int, aspect bool) *kui.Tree {
	size := first + n
	t := &kui.Tree{
		Subtree:   make([]int32, size),
		Style:     make([]kui.LayoutStyle, size),
		Text:      make([]kui.Span, size),
		TextStyle: make([]kui.TextStyle, size),
		Box:       make([]kui.Rect, size),
		Content:   make([]kui.Size, size),
		LineSpan:  make([]kui.Span, size),
		Lines:     make([]kui.TextLine, 0, 512),
	}
	// Node first+i's parent is a node before it.
	parent := make([]int, size)
	children := make([][]int, size)
	parent[first] = -1
	for i := first + 1; i < size; i++ {
		parent[i] = first + g.n(i-first)
		children[parent[i]] = append(children[parent[i]], i)
	}
	var visit func(v int)
	visit = func(v int) {
		pos := len(t.Order)
		t.Order = append(t.Order, int32(v))
		for _, c := range children[v] {
			visit(c)
		}
		t.Subtree[v] = int32(len(t.Order) - pos)
	}
	visit(first)
	for i := first; i < size; i++ {
		s := &t.Style[i]
		s.Direction = kui.Direction(g.n(2))
		s.Width, s.Height = g.sizing(), g.sizing()
		p := float32(g.n(3) * 3)
		s.Padding = kui.Insets{Left: p, Right: float32(g.n(2) * 4), Top: float32(g.n(2) * 2), Bottom: p}
		s.Gap = float32(g.n(3) * 5)
		s.AlignX, s.AlignY = kui.AlignH(g.n(3)), kui.AlignV(g.n(3))
		if g.n(4) == 0 {
			s.ClipX, s.ClipY = g.n(2) == 0, true
			s.Scroll = kui.Point{X: -float32(g.n(3) * 7), Y: -float32(g.n(3) * 9)}
		}
		if aspect && g.n(8) == 0 {
			s.AspectRatio = float32(1+g.n(3)) / 2
		}
		if i > first && g.n(6) == 0 {
			f := &s.Floating
			f.AttachTo = kui.AttachTo(1 + g.n(3))
			f.Element, f.Parent = kui.AttachPoint(g.n(9)), kui.AttachPoint(g.n(9))
			f.Offset = kui.Point{X: float32(g.n(5)), Y: float32(g.n(5))}
			f.Expand = kui.Size{W: float32(g.n(2) * 2)}
			f.Z = int16(g.n(4) - 1)
		}
	}
	// Floating nodes form a dependency DAG: each gets a rank above the rank of the floating
	// subtree holding it (the main tree has rank 0), and attaches only to nodes of lower rank.
	rank := make([]int, size)
	rootRank := func(v int) int {
		for ; v >= first; v = parent[v] {
			if v != first && t.Style[v].Floating.AttachTo != kui.AttachNone {
				return rank[v]
			}
		}
		return 0
	}
	for _, v := range t.Order {
		if f := &t.Style[v].Floating; int(v) != first && f.AttachTo != kui.AttachNone {
			rank[v] = rootRank(parent[v]) + 1 + g.n(n)
		}
	}
	for _, v := range t.Order {
		f := &t.Style[v].Floating
		if f.AttachTo != kui.AttachNode {
			continue
		}
		var cands []int
		for _, c := range t.Order {
			if rootRank(int(c)) < rank[v] {
				cands = append(cands, int(c))
			}
		}
		f.Node = kui.MakeHandle(cands[g.n(len(cands))], 1)
	}
	var arena []byte
	for i := first; i < size; i++ {
		if g.n(2) == 0 {
			continue
		}
		s := g.text()
		if s == "" {
			continue
		}
		t.Text[i] = kui.Span{Start: uint32(len(arena)), Len: uint32(len(s))}
		arena = append(arena, s...)
		ts := &t.TextStyle[i]
		ts.Size = 16
		ts.Wrap = kui.TextWrap(g.n(3))
		ts.Align = kui.AlignH(g.n(3))
		if g.n(4) == 0 {
			ts.LineHeight = 20
		}
	}
	t.TextBytes = arena
	return t
}

// cloneOutputs returns a copy of t sharing its inputs, with outputs of its own.
func cloneOutputs(t *kui.Tree) *kui.Tree {
	c := *t
	c.Box = make([]kui.Rect, len(t.Box))
	c.Content = make([]kui.Size, len(t.Content))
	c.LineSpan = make([]kui.Span, len(t.LineSpan))
	c.Lines = make([]kui.TextLine, 0, cap(t.Lines))
	return &c
}

// diffOutputs describes the first difference between the outputs of a and b, or returns "".
func diffOutputs(a, b *kui.Tree) string {
	for _, n := range a.Order {
		if a.Box[n] != b.Box[n] {
			return fmt.Sprintf("box[%d] %+v vs %+v", n, a.Box[n], b.Box[n])
		} else if a.Content[n] != b.Content[n] {
			return fmt.Sprintf("content[%d] %+v vs %+v", n, a.Content[n], b.Content[n])
		}
		la, lb := a.NodeLines(int(n)), b.NodeLines(int(n))
		if len(la) != len(lb) {
			return fmt.Sprintf("node %d has %d vs %d lines", n, len(la), len(lb))
		}
		for i := range la {
			if la[i] != lb[i] {
				return fmt.Sprintf("line %d of node %d %+v vs %+v", i, n, la[i], lb[i])
			}
		}
	}
	return ""
}

// negativePercent reports whether a node of t has a negative percent size.
func negativePercent(t *kui.Tree) bool {
	for _, n := range t.Order {
		s, b := &t.Style[n], t.Box[n]
		if s.Width.Kind == kui.SizePercent && b.W < 0 || s.Height.Kind == kui.SizePercent && b.H < 0 {
			return true
		}
	}
	return false
}

// layoutProblem describes an output of t's layout that no layout should give, or returns "".
func layoutProblem(t *kui.Tree) string {
	finite := func(vs ...float32) bool {
		for _, v := range vs {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				return false
			}
		}
		return true
	}
	// Percent sizes are fractions of the parent's size less its padding and gaps, negative in
	// parents smaller than those as in clay, and spread to aspect ratios and content sizes.
	// Trees with such sizes skip the sign checks.
	signs := !negativePercent(t)
	for _, n := range t.Order {
		b, c, s := t.Box[n], t.Content[n], &t.Style[n]
		if !finite(b.X, b.Y, b.W, b.H, c.W, c.H) {
			return fmt.Sprintf("node %d box %+v content %+v not finite", n, b, c)
		}
		if signs && (b.W < 0 || b.H < 0 || c.W < 0 || c.H < 0) {
			return fmt.Sprintf("node %d box %+v content %+v negative", n, b, c)
		}
		// Sizes of nodes laid out by their parent, unaffected by aspect ratios and Expand.
		if s.AspectRatio == 0 && (n == t.Order[0] || s.Floating.AttachTo == kui.AttachNone) {
			for _, ax := range [2]struct {
				name string
				sz   kui.Sizing
				got  float32
			}{{"width", s.Width, b.W}, {"height", s.Height, b.H}} {
				switch sz := ax.sz; {
				case sz.Kind == kui.SizeFixed && ax.got != sz.Min:
					return fmt.Sprintf("node %d %s %v not fixed %v", n, ax.name, ax.got, sz.Min)
				case (sz.Kind == kui.SizeFit || sz.Kind == kui.SizeGrow) && (ax.got < sz.Min || sz.Max > 0 && ax.got > sz.Max):
					return fmt.Sprintf("node %d %v %s %v outside [%v, %v]", n, sz.Kind, ax.name, ax.got, sz.Min, sz.Max)
				}
			}
		}
		// Lines are in order and hold all the text but the spaces and newlines between them.
		sp := t.Text[n]
		text := t.TextBytes[sp.Start : sp.Start+sp.Len]
		end := 0
		blank := func(s []byte) bool {
			for _, c := range s {
				if c != ' ' && c != '\n' {
					return false
				}
			}
			return true
		}
		for i, l := range t.NodeLines(int(n)) {
			lo, hi := int(l.Start), int(l.Start+l.Len)
			if l.Len == 0 || lo < end || hi > len(text) {
				return fmt.Sprintf("line %d [%d, %d) of node %d out of order in %q", i, lo, hi, n, text)
			} else if !blank(text[end:lo]) {
				return fmt.Sprintf("node %d lost %q before line %d of %q", n, text[end:lo], i, text)
			}
			end = hi
		}
		if !blank(text[end:]) {
			return fmt.Sprintf("node %d lost %q at the end of %q", n, text[end:], text)
		}
	}
	return ""
}

// dumpTree formats the inputs and boxes of t's nodes, indented by depth.
func dumpTree(t *kui.Tree) string {
	var b []byte
	var stack []int // Positions in Order where open subtrees end.
	for pos, n := range t.Order {
		for len(stack) > 0 && stack[len(stack)-1] <= pos {
			stack = stack[:len(stack)-1]
		}
		sp := t.Text[n]
		b = fmt.Appendf(b, "%*s%d: box %+v content %+v text %q\n%*s   style %+v\n", 2*len(stack), "", n, t.Box[n], t.Content[n],
			t.TextBytes[sp.Start:sp.Start+sp.Len], 2*len(stack), "", t.Style[n])
		stack = append(stack, pos+int(t.Subtree[n]))
	}
	return string(b)
}

// checkRandomLayout lays out a random tree, after another one on the same engine, checking the
// invariants of its outputs and that they equal those of a fresh engine and of a second layout.
func checkRandomLayout(t testing.TB, label string, r *rand.Rand) {
	g := randGen{r}
	vp := kui.Size{W: float32(50 + g.n(400)), H: float32(50 + g.n(400))}
	prev := randTree(g, 1, 1+g.n(randNodes), g.n(2) == 0)
	// Node 0 is not listed and must keep its outputs.
	tr := randTree(g, 1, 1+g.n(randNodes), g.n(2) == 0)
	tr.Box[0], tr.Content[0], tr.LineSpan[0] = kui.Rect{X: 1, Y: 2, W: 3, H: 4}, kui.Size{W: 5, H: 6}, kui.Span{Start: 7, Len: 8}
	fail := func(format string, args ...any) {
		t.Helper()
		t.Fatalf("%s, viewport %+v: %s", label, vp, fmt.Sprintf(format, args...))
	}
	reused := newLayoutBasic(t, randNodes+1)
	if err := reused.Layout(prev, vp); err != nil {
		fail("previous tree: %v", err)
	}
	if err := reused.Layout(tr, vp); err != nil {
		fail("%v", err)
	}
	if msg := layoutProblem(tr); msg != "" {
		fail("%s\n%s", msg, dumpTree(tr))
	}
	if tr.Box[0] != (kui.Rect{X: 1, Y: 2, W: 3, H: 4}) || tr.Content[0] != (kui.Size{W: 5, H: 6}) || tr.LineSpan[0] != (kui.Span{Start: 7, Len: 8}) {
		fail("unlisted node outputs changed: %+v %+v %+v", tr.Box[0], tr.Content[0], tr.LineSpan[0])
	}
	fresh := cloneOutputs(tr)
	if err := newLayoutBasic(t, randNodes+1).Layout(fresh, vp); err != nil {
		fail("fresh engine: %v", err)
	}
	if d := diffOutputs(tr, fresh); d != "" {
		fail("engine reused after another tree differs from a fresh one: %s", d)
	}
	again := cloneOutputs(tr)
	if err := reused.Layout(again, vp); err != nil {
		fail("second layout: %v", err)
	}
	if d := diffOutputs(tr, again); d != "" {
		fail("second layout differs: %s", d)
	}
}

func TestLayoutBasicRandom(t *testing.T) {
	for i := range *flagTrees {
		seed := *flagSeed + uint64(i)
		checkRandomLayout(t, fmt.Sprintf("seed %d", seed), rand.New(rand.NewPCG(seed, 0)))
	}
}

// FuzzLayoutBasic feeds the fuzzer's bytes to the tree generator as its randomness, so the
// fuzzer mutates trees directly. Once the bytes run out every choice is the first one.
func FuzzLayoutBasic(f *testing.F) {
	for seed := range uint64(8) {
		b := make([]byte, 512)
		r := rand.New(rand.NewPCG(seed, 0))
		for i := range b {
			b[i] = byte(r.Uint32())
		}
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		checkRandomLayout(t, "fuzz", rand.New(&bytesSource{data}))
	})
}

// bytesSource is a rand.Source reading its values from bytes, then zeros.
type bytesSource struct{ b []byte }

func (s *bytesSource) Uint64() uint64 {
	var v [8]byte
	s.b = s.b[copy(v[:], s.b):]
	return binary.LittleEndian.Uint64(v[:])
}
