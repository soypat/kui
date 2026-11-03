package kui

import "testing"

// newTree returns a tree of n nodes whose per node slices hold them all.
func newTree(n int, order, subtree []int32) *Tree {
	t := &Tree{
		Order:     order,
		Subtree:   make([]int32, n),
		Style:     make([]LayoutStyle, n),
		Text:      make([]Span, n),
		TextStyle: make([]TextStyle, n),
		Box:       make([]Rect, n),
		Content:   make([]Size, n),
		LineSpan:  make([]Span, n),
	}
	for p, v := range order {
		if v >= 0 {
			t.Subtree[v] = subtree[p]
		}
	}
	return t
}

func TestTreeValidate(t *testing.T) {
	// Node 5 is the root with children 2 (children 0 and 4) and 1; node 3 is not listed.
	order := []int32{5, 2, 0, 4, 1}
	sizes := []int32{5, 3, 1, 1, 1}
	tr := newTree(6, order, sizes)
	if err := tr.Validate(); err != nil {
		t.Fatal(err)
	}
	var children []int32
	for c := 1; c < tr.SubtreeEnd(0); c = tr.SubtreeEnd(c) {
		children = append(children, tr.Order[c])
	}
	if len(children) != 2 || children[0] != 2 || children[1] != 1 {
		t.Fatalf("root children %v", children)
	}
	if err := newTree(0, nil, nil).Validate(); err != nil {
		t.Fatal("empty tree:", err)
	}

	malformed := []struct {
		name  string
		order []int32
		sizes []int32
	}{
		{"root does not span", []int32{5, 2, 0, 4, 1}, []int32{4, 3, 1, 1, 1}},
		{"child overflows parent", []int32{5, 2, 0, 4, 1}, []int32{5, 2, 2, 1, 1}},
		{"child overflows tree", []int32{5, 2, 0, 4, 1}, []int32{5, 3, 1, 1, 2}},
		{"empty subtree", []int32{5, 2, 0, 4, 1}, []int32{5, 3, 0, 1, 1}},
		{"negative subtree", []int32{5, 2, 0, 4, 1}, []int32{5, 3, -1, 1, 1}},
	}
	for _, m := range malformed {
		if err := newTree(6, m.order, m.sizes).Validate(); err != ErrInvalidConfig {
			t.Errorf("%s: got %v", m.name, err)
		}
	}

	tr = newTree(6, order, sizes)
	tr.Box = tr.Box[:5]
	if err := tr.Validate(); err != ErrOutOfRange {
		t.Errorf("short output slice: got %v", err)
	}
	tr = newTree(6, []int32{-1}, []int32{1})
	if err := tr.Validate(); err != ErrOutOfRange {
		t.Errorf("negative node: got %v", err)
	}
	tr = newTree(6, order, sizes)
	tr.TextBytes = []byte("hello")
	tr.Text[4] = Span{Start: 2, Len: 3}
	if err := tr.Validate(); err != nil {
		t.Errorf("text in range: got %v", err)
	}
	tr.Text[4].Len = 4
	if err := tr.Validate(); err != ErrOutOfRange {
		t.Errorf("text out of range: got %v", err)
	}
	tr = newTree(6, order, sizes)
	tr.Style[1].Floating = Floating{AttachTo: AttachNode, Node: MakeHandle(6, 1)}
	if err := tr.Validate(); err != ErrOutOfRange {
		t.Errorf("attached to node out of range: got %v", err)
	}
}

func TestTreeNodeLines(t *testing.T) {
	tr := newTree(2, []int32{0, 1}, []int32{2, 1})
	tr.Lines = []TextLine{{Start: 0, Len: 3}, {Start: 3, Len: 2}}
	tr.LineSpan[1] = Span{Start: 0, Len: 2}
	if got := tr.NodeLines(1); len(got) != 2 || got[1].Start != 3 {
		t.Fatalf("lines %v", got)
	}
	if got := tr.NodeLines(0); len(got) != 0 {
		t.Fatalf("node without text has lines %v", got)
	}
	tr.LineSpan[1].Len = 3
	if got := tr.NodeLines(1); got != nil {
		t.Fatalf("span past Lines: %v", got)
	}
}
