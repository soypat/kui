package kui2go

import (
	"testing"

	"github.com/soypat/kui"
)

func newLayoutBasic(t testing.TB, maxNodes int) *LayoutBasic {
	var e LayoutBasic
	if err := e.Configure(LayoutBasicConfig{MaxNodes: maxNodes, Measurer: TextMono{CharWidth: 8, LineHeight: 16}}); err != nil {
		t.Fatal(err)
	}
	return &e
}

// Node indices of the test tree. Index 0 and 6 are unused; 6 is never listed.
const (
	root   = 1
	header = 2
	label  = 3
	scroll = 4
	popup  = 5
	nodes  = 7
)

// newLayoutTree returns the tree of glaylayout's tests, whose results come from clay.
func newLayoutTree() *kui.Tree {
	t := &kui.Tree{
		// root: header (label), scroll, popup.
		Order:     []int32{root, header, label, scroll, popup},
		Subtree:   make([]int32, nodes),
		Style:     make([]kui.LayoutStyle, nodes),
		Text:      make([]kui.Span, nodes),
		TextStyle: make([]kui.TextStyle, nodes),
		TextBytes: []byte("hello one two three four"),
		Box:       make([]kui.Rect, nodes),
		Content:   make([]kui.Size, nodes),
		LineSpan:  make([]kui.Span, nodes),
		Lines:     make([]kui.TextLine, 0, 8),
	}
	t.Subtree[root], t.Subtree[header], t.Subtree[label], t.Subtree[scroll], t.Subtree[popup] = 5, 2, 1, 1, 1
	t.Style[root] = kui.LayoutStyle{Direction: kui.TopToBottom, Width: kui.Grow(0, 0), Height: kui.Grow(0, 0), Padding: kui.InsetsAll(10), Gap: 5}
	t.Style[header] = kui.LayoutStyle{Width: kui.Grow(0, 0), Height: kui.Fixed(30), AlignX: kui.AlignCenter, AlignY: kui.AlignMiddle}
	t.Style[label] = kui.LayoutStyle{Width: kui.Fit(0, 0)}
	t.Style[scroll] = kui.LayoutStyle{Direction: kui.TopToBottom, Width: kui.Fixed(100), Height: kui.Fixed(40), ClipY: true, Scroll: kui.Point{Y: -16}}
	t.Style[popup] = kui.LayoutStyle{Width: kui.Fixed(20), Height: kui.Fixed(20), Floating: kui.Floating{AttachTo: kui.AttachNode, Node: kui.MakeHandle(header, 3), Element: kui.AttachLeftTop, Parent: kui.AttachLeftBottom, Z: 1}}
	t.Text[label] = kui.Span{Start: 0, Len: 5}
	t.Text[scroll] = kui.Span{Start: 6, Len: 18}
	t.TextStyle[label] = kui.TextStyle{Size: 16}
	t.TextStyle[scroll] = kui.TextStyle{Size: 16}
	t.Box[6] = kui.Rect{X: 1, Y: 2, W: 3, H: 4} // Unlisted nodes keep their outputs.
	return t
}

func TestLayoutBasic(t *testing.T) {
	e := newLayoutBasic(t, nodes)
	tr := newLayoutTree()
	if err := e.Layout(tr, kui.Size{W: 200, H: 200}); err != nil {
		t.Fatal(err)
	}
	if r := tr.Box[root]; r != (kui.Rect{W: 200, H: 200}) {
		t.Errorf("root: %+v", r)
	}
	if r := tr.Box[header]; r != (kui.Rect{X: 10, Y: 10, W: 180, H: 30}) {
		t.Errorf("header: %+v", r)
	}
	// Label is centered in header: 5 chars * 8px = 40 wide, 16 tall.
	if r := tr.Box[label]; r != (kui.Rect{X: 80, Y: 17, W: 40, H: 16}) {
		t.Errorf("label: %+v", r)
	}
	lines := tr.NodeLines(label)
	if len(lines) != 1 || lines[0].Start != 0 || lines[0].Len != 5 || lines[0].Rect != (kui.Rect{X: 80, Y: 17, W: 40, H: 16}) {
		t.Errorf("label lines: %+v", lines)
	}
	// Scroll container wraps its text at 100px into "one two" and "three four", offset by -16.
	lines = tr.NodeLines(scroll)
	if len(lines) != 2 || lines[0].Rect != (kui.Rect{X: 10, Y: 45 - 16, W: 56, H: 16}) ||
		lines[1].Start != 8 || lines[1].Len != 10 || lines[1].Rect.Y != 45 {
		t.Errorf("scroll lines: %+v", lines)
	}
	if sz := tr.Content[scroll]; sz != (kui.Size{W: 100, H: 32}) {
		t.Errorf("scroll content size: %+v", sz)
	}
	if r := tr.Box[popup]; r != (kui.Rect{X: 10, Y: 40, W: 20, H: 20}) {
		t.Errorf("popup: %+v", r)
	}
	if lines := tr.NodeLines(popup); len(lines) != 0 {
		t.Errorf("popup has lines %+v", lines)
	}
	if r := tr.Box[6]; r != (kui.Rect{X: 1, Y: 2, W: 3, H: 4}) {
		t.Errorf("unlisted node box changed: %+v", r)
	}

	// Hiding the header, i.e. not listing it, moves the scroll container up.
	tr.Order = []int32{root, scroll, popup}
	tr.Subtree[root] = 3
	tr.Style[popup].Floating.AttachTo = kui.AttachParent
	if err := e.Layout(tr, kui.Size{W: 200, H: 200}); err != nil {
		t.Fatal(err)
	}
	if r := tr.Box[scroll]; r.Y != 10 {
		t.Errorf("scroll after hiding header: %+v", r)
	}
	if len(tr.NodeLines(scroll)) != 2 || len(tr.Lines) != 2 {
		t.Errorf("lines after hiding header: %+v", tr.Lines)
	}
}

func TestLayoutBasicSizing(t *testing.T) {
	// A row of a fixed child, two growing children sharing the rest, one capped, and a
	// wrapped text compressed to fit.
	tr := &kui.Tree{
		Order:     []int32{0, 1, 2, 3, 4},
		Subtree:   []int32{5, 1, 1, 1, 1},
		Style:     make([]kui.LayoutStyle, 5),
		Text:      make([]kui.Span, 5),
		TextStyle: make([]kui.TextStyle, 5),
		TextBytes: []byte("aa bb"),
		Box:       make([]kui.Rect, 5),
		Content:   make([]kui.Size, 5),
		LineSpan:  make([]kui.Span, 5),
		Lines:     make([]kui.TextLine, 0, 4),
	}
	tr.Style[0] = kui.LayoutStyle{Width: kui.Fixed(130), Height: kui.Fit(0, 0), Padding: kui.Insets{Left: 5, Right: 5}, Gap: 10, AlignY: kui.AlignBottom}
	tr.Style[1] = kui.LayoutStyle{Width: kui.Fixed(20), Height: kui.Fixed(10)}
	tr.Style[2] = kui.LayoutStyle{Width: kui.Grow(0, 0), Height: kui.Fixed(40)}
	tr.Style[3] = kui.LayoutStyle{Width: kui.Grow(0, 15), Height: kui.Grow(0, 0)}
	tr.Style[4] = kui.LayoutStyle{Width: kui.Fit(0, 0)}
	tr.Text[4] = kui.Span{Len: 5}
	e := newLayoutBasic(t, 5)
	if err := e.Layout(tr, kui.Size{W: 500, H: 500}); err != nil {
		t.Fatal(err)
	}
	// 120 inner pixels minus 20 fixed, 30 of gaps and 40 of text ("aa bb") leave 30 to grow:
	// 15 each. Text is 16 tall, so the row is 40 tall and the second grower fills it.
	want := []kui.Rect{
		{X: 0, Y: 0, W: 130, H: 40},
		{X: 5, Y: 30, W: 20, H: 10},
		{X: 35, Y: 0, W: 15, H: 40},
		{X: 60, Y: 0, W: 15, H: 40},
		{X: 85, Y: 24, W: 40, H: 16},
	}
	for i, w := range want {
		if tr.Box[i] != w {
			t.Errorf("box %d: want %+v, got %+v", i, w, tr.Box[i])
		}
	}
	// Narrowed by 10 pixels too many, the row compresses its largest child, the text, which
	// wraps into two lines. Growers have nothing left.
	tr.Style[0].Width = kui.Fixed(90)
	if err := e.Layout(tr, kui.Size{W: 500, H: 500}); err != nil {
		t.Fatal(err)
	}
	lines := tr.NodeLines(4)
	if b := tr.Box[4]; b.W < 29.95 || b.W > 30.05 || b.H != 32 || len(lines) != 2 || lines[1].Start != 3 || tr.Box[2].W != 0 {
		t.Errorf("compressed text %+v, lines %+v, grower %+v", b, lines, tr.Box[2])
	}
}

func TestLayoutBasicErrors(t *testing.T) {
	tr := newLayoutTree()
	e := newLayoutBasic(t, nodes-1)
	if err := e.Layout(tr, kui.Size{W: 200, H: 200}); err != nil {
		t.Fatalf("listed nodes fit: %v", err)
	}
	tr.Order = append(tr.Order, 6)
	tr.Subtree[6], tr.Subtree[root] = 1, 6
	if err := e.Layout(tr, kui.Size{W: 200, H: 200}); err != kui.ErrCapacity {
		t.Fatalf("want ErrCapacity, got %v", err)
	}
	tr = newLayoutTree()
	tr.Lines = tr.Lines[:0:2]
	if err := newLayoutBasic(t, nodes).Layout(tr, kui.Size{W: 200, H: 200}); err != kui.ErrShortBuffer {
		t.Fatalf("want ErrShortBuffer, got %v", err)
	}
	if len(tr.Lines) != 2 || len(tr.NodeLines(label)) != 1 || len(tr.NodeLines(scroll)) != 1 || tr.Box[scroll].H != 40 {
		t.Fatalf("partial lines %+v, scroll %+v", tr.Lines, tr.Box[scroll])
	}
	tr.Subtree[header] = 5 // Past the end of the tree.
	if err := newLayoutBasic(t, nodes).Layout(tr, kui.Size{W: 200, H: 200}); err != kui.ErrInvalidConfig {
		t.Fatalf("malformed tree: got %v", err)
	}
}

func TestLayoutBasicAllocs(t *testing.T) {
	e := newLayoutBasic(t, nodes)
	tr := newLayoutTree()
	frame := func() {
		if err := e.Layout(tr, kui.Size{W: 200, H: 200}); err != nil {
			t.Fatal(err)
		}
	}
	frame()
	if allocs := testing.AllocsPerRun(100, frame); allocs != 0 {
		t.Fatal("allocations:", allocs)
	}
}
