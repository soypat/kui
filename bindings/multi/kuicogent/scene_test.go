package kuicogent

import (
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"cogentcore.org/core/math32"
	"cogentcore.org/core/paint"
	"github.com/soypat/kui"
	"github.com/soypat/kui/kui2go"
	"github.com/soypat/kui/kui2go/kuiscene"
)

// TestScene lays out widgets with the Measurer and paints them with the Painter at scale 2,
// checking text stays in the boxes layout gave it. Set KUI_SCENE_DIR to write scene.png there.
func TestScene(t *testing.T) {
	const w, h, scale = 260, 220, 2
	m := NewMeasurer()
	var eng kui2go.LayoutBasic
	if err := eng.Configure(kui2go.LayoutBasicConfig{MaxNodes: 64, Measurer: m}); err != nil {
		t.Fatal(err)
	}
	theme := kuiscene.DefaultTheme()
	theme.CaretBlink = 0
	var k kuiscene.Scene
	err := k.Configure(kuiscene.Config{
		Memory:     kuiscene.NewMemory(kuiscene.MemoryConfig{MaxNodes: 63, TextBytes: 1024, MaxEvents: 64}),
		Engine:     &eng,
		Viewport:   kui.Size{W: w, H: h},
		RootStyle:  kui.LayoutStyle{Direction: kui.TopToBottom, Width: kui.Grow(0, 0), Height: kui.Grow(0, 0), Padding: kui.InsetsAll(8), Gap: 6},
		RootVisual: kuiscene.Visual{Background: theme.Background},
	})
	if err != nil {
		t.Fatal(err)
	}
	var wb kuiscene.WidgetBuilder
	if err := wb.Configure(&k, kuiscene.WidgetConfig{Theme: theme, Measurer: m}); err != nil {
		t.Fatal(err)
	}
	root := k.Root()
	wb.Label(root, "kui on Cogent Core")
	row := wb.HBox(root)
	wb.Button(row, "Increment")
	wb.Button(row, "Reset")
	wb.Check(root, "Checked", true)
	entry := wb.Entry(root, "hello kui", 32)
	wb.Slider(root, 0.3, 0)
	list := wb.Scroll(root, kui.TopToBottom)
	for i := range 8 {
		wb.Label(list, "Scrollable item "+strconv.Itoa(i))
	}
	if err := wb.Err(); err != nil {
		t.Fatal(err)
	}
	k.SetFocus(entry)
	if err := k.Layout(); err != nil {
		t.Fatal(err)
	}
	var l kui.DrawList
	l.Reset(make([]kui.DrawCmd, 0, 256), make([]byte, 0, 2048))
	if err := k.Paint(&l); err != nil {
		t.Fatal(err)
	}
	var p Painter
	p.Configure(PainterConfig{})
	pc := paint.NewPainter(math32.Vec2(w*scale, h*scale))
	p.Paint(pc, &l, scale)
	img := paint.RenderToImage(pc)
	if dir := os.Getenv("KUI_SCENE_DIR"); dir != "" {
		f, err := os.Create(filepath.Join(dir, "scene.png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
	}
	// Text measured by the Measurer fits the width layout gave it.
	texts := 0
	for _, c := range l.Cmds() {
		if c.Op != kui.DrawText {
			continue
		}
		texts++
		_, size, _, _ := c.TextStyle()
		if mw, _ := m.Measure(string(l.TextOf(&c)), &kui.TextStyle{Size: size}); mw > c.Rect.W+0.5 {
			t.Errorf("%q measures %v, laid out in %v", l.TextOf(&c), mw, c.Rect.W)
		}
	}
	if texts < 8 { // Labels, buttons, check, entry and the visible list items.
		t.Fatalf("%d text commands", texts)
	}
}
