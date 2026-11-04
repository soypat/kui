package kuilefevre

import (
	"testing"

	"github.com/soypat/kui"
	"github.com/soypat/kui/kui2go"
	"github.com/soypat/lefevre"
	"golang.org/x/image/font/gofont/goregular"
)

func goRegular(t *testing.T) *Face {
	t.Helper()
	fnt := new(lefevre.Font)
	if err := fnt.LoadBytes(goregular.TTF, 0); err != nil {
		t.Fatal(err)
	}
	var face Face
	if err := face.Configure(Config{Font: fnt, Sizes: []uint16{13, 24}}); err != nil {
		t.Fatal(err)
	}
	return &face
}

func TestConfigure(t *testing.T) {
	var face Face
	if err := face.Configure(Config{}); err != kui.ErrInvalidConfig {
		t.Fatal("want invalid config without a font, got", err)
	}
}

func TestGlyph(t *testing.T) {
	face := goRegular(t)
	mask, w, h, _, dy, adv, ok := face.Glyph(24, 'A')
	if !ok || w <= 0 || h <= 0 || len(mask) != w*h || dy >= 0 || adv <= 0 {
		t.Fatalf("unexpected glyph w=%d h=%d dy=%d adv=%d ok=%v len=%d", w, h, dy, adv, ok, len(mask))
	}
	var ink int
	for _, a := range mask {
		if a != 0 {
			ink++
		}
	}
	if ink == 0 {
		t.Fatal("glyph 'A' has no ink")
	}
	if mask, _, _, _, _, adv, ok := face.Glyph(24, ' '); !ok || len(mask) != 0 || adv <= 0 {
		t.Fatalf("space: ok=%v len=%d adv=%d", ok, len(mask), adv)
	}
	// Runes and sizes not rasterized still advance the pen.
	if _, _, _, _, _, adv, ok := face.Glyph(24, 'é'); ok || adv <= 0 {
		t.Fatalf("unrasterized rune: ok=%v adv=%d", ok, adv)
	}
	if _, _, _, _, _, adv, ok := face.Glyph(30, 'A'); ok || adv <= 0 {
		t.Fatalf("unrasterized size: ok=%v adv=%d", ok, adv)
	}
}

func TestMetrics(t *testing.T) {
	face := goRegular(t)
	a13, lh13 := face.Metrics(13)
	a24, lh24 := face.Metrics(24)
	if a13 <= 0 || a13 > lh13 || a24 <= a13 || lh24 <= lh13 {
		t.Fatalf("metrics: 13px ascent=%d line=%d, 24px ascent=%d line=%d", a13, lh13, a24, lh24)
	}
}

// TestAdvances checks glyphs advance by the font's advance whether they were rasterized or not,
// so text measured by kui2go.TextFaces fits where it is painted.
func TestAdvances(t *testing.T) {
	face := goRegular(t)
	for _, size := range []uint16{13, 30} {
		for _, r := range "Hello, wörld!" {
			_, _, _, _, _, adv, _ := face.Glyph(size, r)
			if want := kui.FontAdvance(face.font, face.font.GlyphID(r), size); adv != want {
				t.Errorf("%q at %d: advance %d, want %d", r, size, adv, want)
			}
		}
	}
}

func TestAllocs(t *testing.T) {
	face := goRegular(t)
	faces := kui2go.TextFaces{face}
	st := kui.TextStyle{Size: 13}
	allocs := testing.AllocsPerRun(100, func() {
		face.Glyph(13, 'g')
		face.Metrics(13)
		faces.Measure("hello", &st)
	})
	if allocs != 0 {
		t.Fatal("allocations:", allocs)
	}
}
