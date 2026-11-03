package kui

import (
	"slices"
	"testing"
	"unsafe"
)

func TestDrawCmdSize(t *testing.T) {
	if sz := unsafe.Sizeof(DrawCmd{}); sz != 44 {
		t.Fatalf("DrawCmd size changed: %d bytes", sz)
	}
}

func TestDrawListShortBufferNeed(t *testing.T) {
	var l DrawList
	l.Reset(make([]DrawCmd, 0, 2), make([]byte, 0, 8))
	r := Rect{W: 10, H: 10}
	l.Rect(1, r, RGB(1, 2, 3), Radius(2))
	l.Text(2, r, RGB(0, 0, 0), "hello", 1, 16, 0, 0)
	l.Text(3, r, RGB(0, 0, 0), "world", 1, 16, 0, 0) // No room for text.
	l.Border(4, r, RGB(9, 9, 9), DrawBorders{1, 2, 3, 4}, DrawRadii{})
	if l.Err() != ErrShortBuffer {
		t.Fatalf("want ErrShortBuffer, got %v", l.Err())
	}
	cmds, text := l.Need()
	if cmds != 4 || text != 10 {
		t.Fatalf("need: want 4 commands and 10 bytes, got %d and %d", cmds, text)
	}
	if l.Len() != 2 || string(l.TextOf(&l.Cmds()[1])) != "hello" {
		t.Fatalf("unexpected list contents")
	}
	// Grow to the needed size and paint again.
	l.Reset(make([]DrawCmd, 0, cmds), make([]byte, 0, text))
	l.Rect(1, r, RGB(1, 2, 3), Radius(2))
	l.Text(2, r, RGB(0, 0, 0), "hello", 1, 16, 0, 0)
	l.Text(3, r, RGB(0, 0, 0), "world", 1, 16, 2, 20)
	l.Border(4, r, RGB(9, 9, 9), DrawBorders{1, 2, 3, 4}, DrawRadii{})
	if l.Err() != nil || l.Len() != 4 {
		t.Fatalf("after growing: err=%v len=%d", l.Err(), l.Len())
	}
	c := l.Cmds()
	if bw := c[3].BorderWidths(); bw != (DrawBorders{1, 2, 3, 4}) {
		t.Errorf("border widths %+v", bw)
	}
	if f, s, ls, lh := c[2].TextStyle(); f != 1 || s != 16 || ls != 2 || lh != 20 || string(l.TextOf(&c[2])) != "world" {
		t.Errorf("text command %+v", c[2])
	}
}

func TestDrawListMarkRewind(t *testing.T) {
	var l DrawList
	l.Reset(make([]DrawCmd, 0, 16), make([]byte, 0, 16))
	r := Rect{W: 1, H: 1}
	l.Rect(0, r, Color{}, DrawRadii{})
	l.PushClip(0, r)
	m := l.Mark()
	l.SetZ(3)
	l.Text(0, r, Color{}, "abc", 0, 0, 0, 0)
	l.Rewind(m)
	if l.Len() != 2 || len(l.text) != 0 {
		t.Fatal("rewind did not restore state")
	}
	l.PopClip(0)
	l.Line(0, Point{}, Point{X: 3, Y: 4}, 1.5, Color{})
	cmds := l.Cmds()
	if last := cmds[len(cmds)-1]; last.LineWidth() != 1.5 || last.Z != 0 {
		t.Fatalf("line %+v", last)
	}
}

func TestDrawCmdBounds(t *testing.T) {
	r := Rect{X: 10, Y: 20, W: 30, H: 40}
	tests := []struct {
		c    DrawCmd
		want Rect
	}{
		{DrawCmd{Op: DrawRect, Rect: r}, r},
		{DrawCmd{Op: DrawClipPush, Rect: r}, r},
		{DrawCmd{Op: DrawClipPop, Rect: r}, Rect{}},
		{DrawCmd{Op: DrawText, Rect: r}, Rect{X: 8, Y: 18, W: 34, H: 44}},
		// A line from (10,20) to (0,0) of width 4 grows by 4/2+1.
		{DrawCmd{Op: DrawLine, Rect: Rect{X: 10, Y: 20, W: -10, H: -20}, A: 0x40800000}, Rect{X: -3, Y: -3, W: 16, H: 26}},
	}
	for _, tc := range tests {
		if got := tc.c.Bounds(); got != tc.want {
			t.Errorf("%v bounds: want %+v, got %+v", tc.c.Op, tc.want, got)
		}
	}
}

func TestDrawListCopyFrom(t *testing.T) {
	var src DrawList
	src.Reset(make([]DrawCmd, 0, 4), make([]byte, 0, 16))
	src.Text(1, Rect{W: 10, H: 10}, RGB(1, 1, 1), "hello", 0, 13, 0, 0)
	src.Rect(2, Rect{W: 5, H: 5}, RGB(2, 2, 2), DrawRadii{})
	var dst DrawList
	dst.Reset(make([]DrawCmd, 0, 1), make([]byte, 0, 16))
	if err := dst.CopyFrom(&src); err != ErrShortBuffer || dst.Len() != 0 {
		t.Fatalf("want ErrShortBuffer and empty list, got %v len=%d", err, dst.Len())
	}
	if cmds, text := dst.Need(); cmds != 2 || text != 5 {
		t.Fatalf("need %d %d", cmds, text)
	}
	dst.Reset(make([]DrawCmd, 0, 2), nil)
	if err := dst.CopyFrom(&src); err != nil || !slices.Equal(dst.Cmds(), src.Cmds()) || string(dst.TextOf(&dst.Cmds()[0])) != "hello" {
		t.Fatalf("copy: %v", err)
	}
}

func TestDrawListAllocs(t *testing.T) {
	var l DrawList
	l.Reset(make([]DrawCmd, 0, 64), make([]byte, 0, 256))
	r := Rect{W: 10, H: 10}
	allocs := testing.AllocsPerRun(100, func() {
		l.Clear()
		l.Rect(1, r, RGB(1, 2, 3), Radius(2))
		l.Text(2, r, RGB(0, 0, 0), "hello", 1, 16, 0, 0)
		l.PushClip(3, r)
		l.Circle(4, Point{X: 5, Y: 5}, 3, RGB(1, 1, 1))
		l.PopClip(3)
	})
	if allocs != 0 {
		t.Fatal("allocations:", allocs)
	}
}
