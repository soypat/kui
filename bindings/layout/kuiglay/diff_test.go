package kuiglay

import (
	"flag"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/soypat/kui"
	"github.com/soypat/kui/kui2go"
)

// Differential tests of Engine against kui2go.LayoutBasic, which runs clay's algorithm directly
// over a kui.Tree. Both must give the same geometry, also when one Engine is reused across trees
// as an application would.

var flagDiffN = flag.Int("diffn", 2000, "number of random trees laid out by TestDiffLayoutBasic")

const diffNodes = 24

var diffWords = []string{"a", "bb", "ccc", "dddd", "eeeee", "ffffffffff"}

type diffGen struct{ r *rand.Rand }

func (g diffGen) n(k int) int { return g.r.IntN(k) }

func (g diffGen) sizing() kui.Sizing {
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

func (g diffGen) text() string {
	var b []byte
	for i := range g.n(8) {
		if i > 0 {
			if g.n(6) == 0 {
				b = append(b, '\n')
			} else {
				b = append(b, ' ')
			}
		}
		b = append(b, diffWords[g.n(len(diffWords))]...)
	}
	return string(b)
}

// diffTree returns a random tree of the nodes first to first+n-1, all listed, with every layout
// feature. Floating nodes attach to any node they do not depend on: possibly declared after them,
// in floating subtrees of higher Z or with negative Z.
func diffTree(g diffGen, first, n int, aspect bool) *kui.Tree {
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

func near(a, b float32) bool { return a-b < 0.02 && b-a < 0.02 }

func nearRect(a, b kui.Rect) bool {
	return near(a.X, b.X) && near(a.Y, b.Y) && near(a.W, b.W) && near(a.H, b.H)
}

// diffOutputs describes the differences between the outputs of a and b, empty for none.
func diffOutputs(a, b *kui.Tree) string {
	var d strings.Builder
	for _, n := range a.Order {
		if !nearRect(a.Box[n], b.Box[n]) {
			fmt.Fprintf(&d, " box[%d] glay %+v basic %+v;", n, a.Box[n], b.Box[n])
		}
		if s := &a.Style[n]; s.ClipX || s.ClipY {
			if !near(a.Content[n].W, b.Content[n].W) || !near(a.Content[n].H, b.Content[n].H) {
				fmt.Fprintf(&d, " content[%d] glay %+v basic %+v;", n, a.Content[n], b.Content[n])
			}
		}
		la, lb := a.NodeLines(int(n)), b.NodeLines(int(n))
		if len(la) != len(lb) {
			fmt.Fprintf(&d, " lines[%d] glay %d basic %d;", n, len(la), len(lb))
			continue
		}
		for i := range la {
			if !nearRect(la[i].Rect, lb[i].Rect) || la[i].Start != lb[i].Start || la[i].Len != lb[i].Len {
				fmt.Fprintf(&d, " line[%d][%d] glay %+v basic %+v;", n, i, la[i], lb[i])
			}
		}
	}
	return d.String()
}

// dumpTree describes t's nodes and both engines' boxes, for debugging differences.
func dumpTree(a, b *kui.Tree) string {
	var d strings.Builder
	for p, n := range a.Order {
		st := &a.Style[n]
		sp := a.Text[n]
		fmt.Fprintf(&d, "p%d n%d sub%d dir%d W%+v H%+v pad%+v gap%v clip%v,%v scroll%v float%+v ar%v text%q\n",
			p, n, a.Subtree[n], st.Direction, st.Width, st.Height, st.Padding, st.Gap, st.ClipX, st.ClipY,
			st.Scroll, st.Floating, st.AspectRatio, a.TextBytes[sp.Start:sp.Start+sp.Len])
		fmt.Fprintf(&d, "    glay %+v basic %+v\n", a.Box[n], b.Box[n])
	}
	return d.String()
}

type diffEngines struct {
	glay  Engine
	basic kui2go.LayoutBasic
}

func newDiffEngines(t testing.TB, maxElements, maxNodes int) *diffEngines {
	m := kui2go.TextMono{CharWidth: 8, LineHeight: 16}
	var e diffEngines
	if err := e.glay.Configure(Config{MaxElements: maxElements, MaxLines: 512, MaxMeasureWords: 32 * maxElements, Measurer: m}); err != nil {
		t.Fatal(err)
	}
	if err := e.basic.Configure(kui2go.LayoutBasicConfig{MaxNodes: maxNodes, Measurer: m}); err != nil {
		t.Fatal(err)
	}
	return &e
}

// layout lays out t with both engines and returns the differences, or the error of glay's engine.
func (e *diffEngines) layout(t *kui.Tree, vp kui.Size) (string, error) {
	tb := cloneOutputs(t)
	if err := e.glay.Layout(t, vp); err != nil && err != kui.ErrShortBuffer {
		return "", err
	}
	e.basic.Layout(tb, vp)
	if d := diffOutputs(t, tb); d != "" {
		return d + "\n" + dumpTree(t, tb), nil
	}
	return "", nil
}

// TestDiffLayoutBasic lays out random trees with one Engine, as an application reusing it
// across changing trees would, and compares them with LayoutBasic's.
func TestDiffLayoutBasic(t *testing.T) {
	for _, aspect := range []bool{false, true} {
		t.Run(fmt.Sprint("aspect=", aspect), func(t *testing.T) {
			e := newDiffEngines(t, 2*diffNodes, diffNodes)
			bad := 0
			for seed := range *flagDiffN {
				g := diffGen{rand.New(rand.NewPCG(uint64(seed), 7))}
				tr := diffTree(g, 0, diffNodes, aspect)
				vp := kui.Size{W: float32(100 + g.n(300)), H: float32(100 + g.n(300))}
				d, err := e.layout(tr, vp)
				if err != nil {
					t.Fatalf("seed %d: %v", seed, err)
				}
				if d == "" {
					continue
				}
				bad++
				if bad <= 3 {
					t.Errorf("seed %d vp %v:%s", seed, vp, d)
				}
			}
			if bad > 0 {
				t.Errorf("%d/%d trees differ", bad, *flagDiffN)
			}
		})
	}
}

// TestDiffPageSwitch alternates between two pages with distinct node indices, each fitting the
// engine on its own but not together, as switching between allocated pages does.
func TestDiffPageSwitch(t *testing.T) {
	const pageNodes = 16
	e := newDiffEngines(t, 2*pageNodes, 3*pageNodes)
	for i := range 200 {
		g := diffGen{rand.New(rand.NewPCG(uint64(i), 9))}
		first := 1 + (i%2)*pageNodes
		tr := diffTree(g, first, pageNodes, false)
		d, err := e.layout(tr, kui.Size{W: 300, H: 300})
		if err != nil {
			t.Fatalf("layout %d: %v", i, err)
		}
		if d != "" {
			t.Fatalf("layout %d:%s", i, d)
		}
	}
}

// TestScrollSlotReuse removes a scroll container while another takes its place in the
// declaration order: the new one's content size must not come from the removed one.
func TestScrollSlotReuse(t *testing.T) {
	e := newDiffEngines(t, 8, 8)
	tr := &kui.Tree{
		Order:     []int32{1, 2, 4},
		Subtree:   make([]int32, 8),
		Style:     make([]kui.LayoutStyle, 8),
		Text:      make([]kui.Span, 8),
		TextStyle: make([]kui.TextStyle, 8),
		Box:       make([]kui.Rect, 8),
		Content:   make([]kui.Size, 8),
		LineSpan:  make([]kui.Span, 8),
		Lines:     make([]kui.TextLine, 0, 8),
	}
	tr.Subtree[1], tr.Subtree[2], tr.Subtree[4] = 3, 2, 1
	clip := kui.LayoutStyle{Width: kui.Fixed(50), Height: kui.Fixed(50), ClipY: true}
	tr.Style[1] = kui.LayoutStyle{Width: kui.Grow(0, 0), Height: kui.Grow(0, 0)}
	tr.Style[2] = clip
	tr.Style[3] = clip
	tr.Style[4] = kui.LayoutStyle{Width: kui.Fixed(10), Height: kui.Fixed(80)}
	tr.Style[5] = kui.LayoutStyle{Width: kui.Fixed(30), Height: kui.Fixed(120)}
	vp := kui.Size{W: 200, H: 200}
	if d, err := e.layout(tr, vp); err != nil || d != "" {
		t.Fatalf("first layout: %v%s", err, d)
	}
	// Node 3 replaces node 2, at the same position, with different content.
	tr.Order = []int32{1, 3, 5}
	tr.Subtree[1], tr.Subtree[3], tr.Subtree[5] = 3, 2, 1
	if d, err := e.layout(tr, vp); err != nil || d != "" {
		t.Fatalf("second layout: %v%s", err, d)
	}
	if c := tr.Content[3]; c != (kui.Size{W: 30, H: 120}) {
		t.Fatalf("content of node 3: %+v", c)
	}
}

// TestFixedZero checks that Fixed(0) sizes a node to zero, not to its content.
func TestFixedZero(t *testing.T) {
	e := newDiffEngines(t, 8, 8)
	tr := &kui.Tree{
		Order:     []int32{1, 2, 3},
		Subtree:   []int32{0, 3, 2, 1},
		Style:     make([]kui.LayoutStyle, 4),
		Text:      make([]kui.Span, 4),
		TextStyle: make([]kui.TextStyle, 4),
		TextBytes: []byte("hello"),
		Box:       make([]kui.Rect, 4),
		Content:   make([]kui.Size, 4),
		LineSpan:  make([]kui.Span, 4),
		Lines:     make([]kui.TextLine, 0, 8),
	}
	tr.Style[1] = kui.LayoutStyle{Width: kui.Grow(0, 0), Height: kui.Grow(0, 0)}
	tr.Style[2] = kui.LayoutStyle{Width: kui.Fixed(0), Height: kui.Fixed(0)}
	tr.Style[3] = kui.LayoutStyle{Width: kui.Fixed(40), Height: kui.Fixed(40)}
	tr.Text[2] = kui.Span{Len: 5}
	tr.TextStyle[2] = kui.TextStyle{Size: 16}
	if d, err := e.layout(tr, kui.Size{W: 200, H: 200}); err != nil || d != "" {
		t.Fatalf("%v%s", err, d)
	}
	if b := tr.Box[2]; b.W != 0 || b.H != 0 {
		t.Fatalf("Fixed(0) box: %+v", b)
	}
}

// TestRelayoutAllocs checks that re-initializing glay and laying out more than once do not
// allocate: alternating pages fills glay's hash map, and a floating node with negative Z
// attached to its parent needs a second layout.
func TestRelayoutAllocs(t *testing.T) {
	const pageNodes = 16
	e := newDiffEngines(t, 2*pageNodes, 3*pageNodes)
	var pages [2]*kui.Tree
	elements := 0
	for i := range pages {
		g := diffGen{rand.New(rand.NewPCG(uint64(i), 11))}
		pages[i] = diffTree(g, 1+i*pageNodes, pageNodes, false)
		elements += len(pages[i].Order)
		for _, n := range pages[i].Order {
			if pages[i].Text[n].Len > 0 {
				elements++
			}
		}
	}
	if elements <= 2*pageNodes+1 {
		t.Fatalf("pages of %d elements fit glay's hash map together", elements)
	}
	tr := pages[0]
	last := tr.Order[len(tr.Order)-1]
	tr.Style[last].Floating = kui.Floating{AttachTo: kui.AttachParent, Z: -1}
	if p := e.glay.passes(tr); p < 2 {
		t.Fatalf("want at least 2 passes, got %d", p)
	}
	i := 0
	layout := func() {
		i++
		if err := e.glay.Layout(pages[i%2], kui.Size{W: 300, H: 300}); err != nil && err != kui.ErrShortBuffer {
			t.Fatal(err)
		}
	}
	layout()
	layout()
	if allocs := testing.AllocsPerRun(50, layout); allocs != 0 {
		t.Fatal("allocations:", allocs)
	}
	if d, err := e.layout(tr, kui.Size{W: 300, H: 300}); err != nil || d != "" {
		t.Fatalf("%v%s", err, d)
	}
}

// TestNeeds checks that reconfiguring with the needs reported after kui.ErrCapacity suffices.
func TestNeeds(t *testing.T) {
	text := []byte(strings.Repeat("word ", 40) + "\n\nend")
	tr := &kui.Tree{
		Order:     []int32{0, 1},
		Subtree:   []int32{2, 1},
		Style:     []kui.LayoutStyle{{Width: kui.Grow(0, 0)}, {Width: kui.Fixed(50)}},
		Text:      []kui.Span{{}, {Len: uint32(len(text))}},
		TextStyle: make([]kui.TextStyle, 2),
		TextBytes: text,
		Box:       make([]kui.Rect, 2),
		Content:   make([]kui.Size, 2),
		LineSpan:  make([]kui.Span, 2),
		Lines:     make([]kui.TextLine, 0, 64),
	}
	m := kui2go.TextMono{CharWidth: 8, LineHeight: 16}
	var e Engine
	vp := kui.Size{W: 200, H: 900}
	// Each limit in turn is too small, then set to the need reported.
	steps := []struct {
		name string
		cfg  Config
		set  func(*Config)
	}{
		{"elements", Config{MaxElements: 2, MaxLines: 64, MaxMeasureWords: 96}, func(c *Config) { c.MaxElements = e.NeededElements() }},
		{"words", Config{MaxElements: 3, MaxLines: 64, MaxMeasureWords: 32}, func(c *Config) { c.MaxMeasureWords = e.NeededMeasureWords() }},
		{"lines", Config{MaxElements: 3, MaxLines: 4, MaxMeasureWords: 96}, func(c *Config) { c.MaxLines = e.NeededLines() }},
	}
	for _, step := range steps {
		cfg := step.cfg
		cfg.Measurer = m
		if err := e.Configure(cfg); err != nil {
			t.Fatal(step.name, err)
		}
		if err := e.Layout(tr, vp); err != kui.ErrCapacity {
			t.Fatalf("%s: want ErrCapacity, got %v", step.name, err)
		}
		step.set(&cfg)
		if err := e.Configure(cfg); err != nil {
			t.Fatal(step.name, err)
		}
		if err := e.Layout(tr, vp); err != nil {
			t.Fatalf("%s: after reconfiguring with %+v: %v", step.name, cfg, err)
		}
	}
	if n := len(tr.NodeLines(1)); n != 41 {
		t.Fatalf("want 41 lines, got %d", n)
	}
}
