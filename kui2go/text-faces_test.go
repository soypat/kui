package kui2go

import (
	"testing"

	"github.com/soypat/kui"
)

// fixedFace has 8 pixel advances and 13 pixel lines, and glyphs for ASCII only.
type fixedFace struct{}

func (fixedFace) Metrics(uint16) (ascent, lineHeight int) { return 10, 13 }

func (fixedFace) Glyph(_ uint16, r rune) (mask []byte, w, h, dx, dy, advance int, ok bool) {
	return nil, 0, 0, 0, 0, 8, r < 128
}

func TestTextFaces(t *testing.T) {
	faces := TextFaces{fixedFace{}}
	// Runes outside the font still advance.
	if w, h := faces.Measure("héllo世", &kui.TextStyle{LetterSpacing: 1}); w != 6*9 || h != 13 {
		t.Fatalf("measure: %vx%v", w, h)
	}
	if w, h := faces.Measure("hello", &kui.TextStyle{FontID: 1}); w != 0 || h != 0 {
		t.Fatalf("unknown font measured %vx%v", w, h)
	}
	if faces.Face(0) == nil || faces.Face(1) != nil {
		t.Fatal("Face")
	}
	st := kui.TextStyle{}
	if allocs := testing.AllocsPerRun(100, func() { faces.Measure("hello", &st) }); allocs != 0 {
		t.Fatal("allocations:", allocs)
	}
}
