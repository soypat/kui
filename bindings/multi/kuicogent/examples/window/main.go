// Command window shows kui widgets in a Cogent Core window: Cogent provides the window, its
// events and the painting; kui the widgets, their layout and their behaviour.
//
// Run: go run ./examples/window (from the kuicogent directory).
package main

import (
	"fmt"
	"image"
	"log"
	"strconv"
	"time"

	"cogentcore.org/core/events"
	"cogentcore.org/core/math32"
	"cogentcore.org/core/paint"
	"cogentcore.org/core/paint/render"
	"cogentcore.org/core/system"
	_ "cogentcore.org/core/system/driver"
	"github.com/soypat/kui"
	"github.com/soypat/kui/bindings/multi/kuicogent"
	"github.com/soypat/kui/kui2go"
	"github.com/soypat/kui/kui2go/kuiscene"
)

func main() {
	win, err := system.TheApp.NewWindow(&system.NewWindowOptions{
		Size:      image.Pt(520, 420),
		StdPixels: true,
		Title:     "kui on Cogent Core",
	})
	if err != nil {
		log.Fatal(err)
	}
	go run(win)
	system.TheApp.MainLoop()
}

// run builds the UI and drives it with the window's events. Cogent sends a paint event at the
// screen's refresh rate: the UI steps and repaints on those.
func run(win system.Window) {
	start := time.Now()
	nanotime := func() int64 { return int64(time.Since(start)) }

	var queue kui2go.EventSPSC
	queue.Reset(make([]kui.Event, 256), make([]byte, 1024))
	var in kuicogent.Input
	check(in.Configure(kuicogent.InputConfig{Events: &queue, Window: win, Nanotime: nanotime}))

	measurer := kuicogent.NewMeasurer()
	var eng kui2go.LayoutBasic
	check(eng.Configure(kui2go.LayoutBasicConfig{MaxNodes: 128, Measurer: measurer}))
	theme := kuiscene.DefaultTheme()
	dots := win.Size()
	var k kuiscene.Scene
	check(k.Configure(kuiscene.Config{
		Memory:     kuiscene.NewMemory(kuiscene.MemoryConfig{MaxNodes: 127, TextBytes: 4096, MaxEvents: 128}),
		Engine:     &eng,
		Viewport:   kui.Size{W: float32(dots.X) / in.Scale(), H: float32(dots.Y) / in.Scale()},
		RootStyle:  kui.LayoutStyle{Direction: kui.TopToBottom, Width: kui.Grow(0, 0), Height: kui.Grow(0, 0), Padding: kui.InsetsAll(16), Gap: 10},
		RootVisual: kuiscene.Visual{Background: theme.Background},
	}))
	var w kuiscene.WidgetBuilder
	check(w.Configure(&k, kuiscene.WidgetConfig{Theme: theme, Measurer: measurer}))
	root := k.Root()
	w.Label(root, "kui widgets, Cogent Core window, events and painting.")
	counter := w.Label(root, "clicks: 0")
	row := w.HBox(root)
	inc := w.Button(row, "Increment")
	w.Tooltip(inc, "Adds one")
	reset := w.Button(row, "Reset")
	chk := w.Check(root, "Check me", true)
	entry := w.Entry(root, "Type here", 64)
	echo := w.Label(root, "")
	slider := w.Slider(root, 0.5, 0)
	list := w.Scroll(root, kui.TopToBottom)
	for i := range 30 {
		w.Label(list, "Scrollable item "+strconv.Itoa(i))
	}
	if err := w.Err(); err != nil {
		log.Fatal(err)
	}

	var painter kuicogent.Painter
	painter.Configure(kuicogent.PainterConfig{})
	var drawList kui.DrawList
	drawList.Reset(make([]kui.DrawCmd, 0, 1024), make([]byte, 0, 8192))
	evs := make([]kui.Event, 256)
	var pc *paint.Painter
	var renderer render.Renderer
	clicks := 0
	for {
		ev := win.Events().Deque.NextEvent()
		in.Handle(ev)
		switch ev.Type() {
		case events.Window:
			if we, ok := ev.(*events.WindowEvent); ok && we.Action == events.WinClose {
				system.TheApp.Quit()
				return
			}
			continue
		case events.WindowPaint:
		default:
			continue
		}
		now := nanotime()
		n := queue.DrainEvents(evs)
		deadline, timer := k.NextDeadline()
		if n > 0 || (timer && now >= deadline) {
			if err := k.Step(now, evs[:n], &queue); err != nil {
				log.Println("step:", err)
			}
			for _, e := range k.Events() {
				switch {
				case e.Kind == kuiscene.EventActivate && e.Node == inc:
					clicks++
				case e.Kind == kuiscene.EventActivate && e.Node == reset:
					clicks = 0
				case e.Kind == kuiscene.EventChanged && e.Node == chk:
					fmt.Println("checked:", e.Code == 1)
				case e.Kind == kuiscene.EventChanged && e.Node == entry:
					k.SetText(echo, k.Text(entry))
				case e.Kind == kuiscene.EventChanged && e.Node == slider:
					fmt.Printf("slider: %.2f\n", w.SliderValue(slider))
				}
			}
			k.SetText(counter, "clicks: "+strconv.Itoa(clicks))
		}
		if !k.NeedsPaint() {
			continue
		}
		drawList.Clear()
		if err := k.Paint(&drawList); err != nil {
			log.Println("paint:", err)
		}
		dots := win.Size()
		if dots.X <= 0 || dots.Y <= 0 {
			continue
		}
		size := math32.FromPoint(dots)
		if pc == nil || pc.Size != size {
			pc = paint.NewPainter(size)
			renderer = paint.NewSourceRenderer(size)
		}
		painter.Paint(pc, &drawList, in.Scale())
		if !kuicogent.Present(win, renderer, pc.RenderDone()) {
			return
		}
	}
}

func check(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
