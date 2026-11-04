package kuiglay

import (
	"testing"

	"github.com/soypat/kui"
	"github.com/soypat/kui/kui2go"
)

func newEngine(t testing.TB) *Engine {
	var e Engine
	err := e.Configure(Config{MaxElements: 64, Measurer: kui2go.TextMono{CharWidth: 8, LineHeight: 16}})
	if err != nil {
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

func newTree() *kui.Tree {
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

func TestEngine(t *testing.T) {
	e := newEngine(t)
	tr := newTree()
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
	if len(lines) != 1 || lines[0].Start != 0 || lines[0].Len != 5 || lines[0].Rect.X != 80 {
		t.Errorf("label lines: %+v", lines)
	}
	// Scroll container wraps its text at 100px into "one two" and "three four", offset by -16.
	lines = tr.NodeLines(scroll)
	if len(lines) != 2 || lines[0].Rect.Y != 45-16 || lines[1].Start != 8 || lines[1].Len != 10 {
		t.Errorf("scroll lines: %+v", lines)
	}
	if sz := tr.Content[scroll]; sz.H != 32 {
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

func TestEngineErrors(t *testing.T) {
	var e Engine
	if err := e.Configure(Config{MaxElements: 6, Measurer: kui2go.TextMono{CharWidth: 8, LineHeight: 16}}); err != nil {
		t.Fatal(err)
	}
	tr := newTree()
	if err := e.Layout(tr, kui.Size{W: 200, H: 200}); err != kui.ErrCapacity {
		t.Fatalf("want ErrCapacity, got %v", err)
	}
	if n := e.NeededElements(); n != 7 {
		t.Fatalf("needed elements: %d", n)
	}
	e = Engine{}
	e.Configure(Config{MaxElements: 7, Measurer: kui2go.TextMono{CharWidth: 8, LineHeight: 16}})
	tr.Lines = tr.Lines[:0:2]
	if err := e.Layout(tr, kui.Size{W: 200, H: 200}); err != kui.ErrShortBuffer {
		t.Fatalf("want ErrShortBuffer, got %v", err)
	}
	if len(tr.Lines) != 2 || len(tr.NodeLines(label)) != 1 || len(tr.NodeLines(scroll)) != 1 || tr.Box[popup].W != 20 {
		t.Fatalf("partial lines %+v, popup %+v", tr.Lines, tr.Box[popup])
	}
	tr.Subtree[header] = 5 // Past the end of the tree.
	if err := e.Layout(tr, kui.Size{W: 200, H: 200}); err != kui.ErrInvalidConfig {
		t.Fatalf("malformed tree: got %v", err)
	}
}

func TestEngineAllocs(t *testing.T) {
	e := newEngine(t)
	tr := newTree()
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
