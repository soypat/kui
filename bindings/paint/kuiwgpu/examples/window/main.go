// Command window shows kui widgets in a GLFW window painted with WebGPU: GLFW provides the
// window and its input (kuiglfw), WebGPU the painting (kuiwgpu), lefevre the font
// (kuilefevre); kui the widgets, their layout and their behaviour.
//
// Run: go run ./paint/kuiwgpu/examples/window (from the bindings directory).
package main

import (
	"fmt"
	"log"
	"math"
	"runtime"
	"strconv"
	"time"

	"github.com/go-gl/glfw/v3.4/glfw"
	"github.com/oliverbestmann/webgpu/wgpu"
	"github.com/oliverbestmann/webgpu/wgpuglfw"
	"github.com/soypat/kui"
	"github.com/soypat/kui/bindings/events/kuiglfw"
	"github.com/soypat/kui/bindings/fonts/kuilefevre"
	"github.com/soypat/kui/bindings/paint/kuiwgpu"
	"github.com/soypat/kui/kui2go"
	"github.com/soypat/kui/kui2go/kuiscene"
	"github.com/soypat/lefevre"
	"golang.org/x/image/font/gofont/goregular"
)

// GLFW must be used from the main OS thread.
func init() { runtime.LockOSThread() }

func main() {
	check(glfw.Init())
	defer glfw.Terminate()
	glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI) // WebGPU, not GL, draws the window.
	win, err := glfw.CreateWindow(520, 420, "kui on WebGPU", nil, nil)
	check(err)
	defer win.Destroy()

	start := time.Now()
	nanotime := func() int64 { return int64(time.Since(start)) }
	var queue kui2go.EventSPSC
	queue.Reset(make([]kui.Event, 256), make([]byte, 1024))
	var in kuiglfw.Input
	check(in.Configure(win, kuiglfw.Config{Events: &queue, Nanotime: nanotime}))

	// WebGPU device presenting to the window.
	instance := wgpu.CreateInstance(nil)
	defer instance.Release()
	surface := instance.CreateSurface(wgpuglfw.GetSurfaceDescriptor(win))
	defer surface.Release()
	adapter, err := instance.RequestAdapter(&wgpu.RequestAdapterOptions{CompatibleSurface: surface})
	check(err)
	defer adapter.Release()
	device, err := adapter.RequestDevice(nil)
	check(err)
	defer device.Release()
	gpuQueue := device.GetQueue()
	defer gpuQueue.Release()
	caps := surface.GetCapabilities(adapter)
	surfaceCfg := wgpu.SurfaceConfiguration{
		Usage:       wgpu.TextureUsageRenderAttachment,
		Format:      pickFormat(caps.Formats),
		PresentMode: wgpu.PresentModeFifo,
		AlphaMode:   caps.AlphaModes[0],
	}

	// Text: Go Regular rasterized at the theme's size, in logical and framebuffer pixels.
	theme := kuiscene.DefaultTheme()
	font := new(lefevre.Font)
	check(font.LoadBytes(goregular.TTF, 0))
	size := theme.Text.Size
	var face kuilefevre.Face
	check(face.Configure(kuilefevre.Config{Font: font, Sizes: []uint16{size, uint16(math.Round(float64(size) * float64(in.Scale())))}}))
	faces := kui2go.TextFaces{&face}

	var painter kuiwgpu.Painter
	check(painter.Configure(kuiwgpu.Config{Device: device, Queue: gpuQueue, Format: surfaceCfg.Format, Faces: faces}))
	defer painter.Close()

	// The UI.
	var eng kui2go.LayoutBasic
	check(eng.Configure(kui2go.LayoutBasicConfig{MaxNodes: 128, Measurer: faces}))
	ww, wh := win.GetSize()
	var k kuiscene.Scene
	check(k.Configure(kuiscene.Config{
		Memory:     kuiscene.NewMemory(kuiscene.MemoryConfig{MaxNodes: 127, TextBytes: 4096, MaxEvents: 128}),
		Engine:     &eng,
		Viewport:   kui.Size{W: float32(ww), H: float32(wh)},
		RootStyle:  kui.LayoutStyle{Direction: kui.TopToBottom, Width: kui.Grow(0, 0), Height: kui.Grow(0, 0), Padding: kui.InsetsAll(16), Gap: 10},
		RootVisual: kuiscene.Visual{Background: theme.Background},
	}))
	var w kuiscene.WidgetBuilder
	check(w.Configure(&k, kuiscene.WidgetConfig{Theme: theme, Measurer: faces}))
	root := k.Root()
	w.Label(root, "kui widgets, GLFW window and events, WebGPU painting.")
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
	check(w.Err())

	var drawList kui.DrawList
	drawList.Reset(make([]kui.DrawCmd, 0, 1024), make([]byte, 0, 8192))
	evs := make([]kui.Event, 256)
	clicks := 0
	// Each iteration steps and paints, then sleeps until input comes or a timer of the UI
	// (tooltips, caret blinks) is due. The first one paints before waiting: Wayland shows
	// windows, and sends their events, only once something was presented.
	for ; !win.ShouldClose(); wait(&k, nanotime) {
		in.Flush()
		now := nanotime()
		n := queue.DrainEvents(evs)
		deadline, timer := k.NextDeadline()
		if n > 0 || (timer && now >= deadline) {
			if err := k.Step(now, evs[:n], &queue); err != nil {
				log.Println("step:", err)
			}
			in.Apply(k.Requests())
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
		fbw, fbh := in.FramebufferSize()
		if fbw <= 0 || fbh <= 0 {
			continue // Minimized.
		}
		if surfaceCfg.Width != uint32(fbw) || surfaceCfg.Height != uint32(fbh) {
			surfaceCfg.Width, surfaceCfg.Height = uint32(fbw), uint32(fbh)
			surface.Configure(device, &surfaceCfg)
		}
		if err := present(surface, &painter, &drawList, fbw, fbh, in.Scale()); err != nil {
			log.Println("present:", err)
			surface.Configure(device, &surfaceCfg) // Outdated or lost: try again next frame.
			k.Repaint()
		}
	}
}

// wait sleeps until GLFW has events or the next timer of k is due.
func wait(k *kuiscene.Scene, nanotime func() int64) {
	if deadline, timer := k.NextDeadline(); timer {
		glfw.WaitEventsTimeout(max(float64(deadline-nanotime())/1e9, 0))
	} else {
		glfw.WaitEvents()
	}
}

// present paints l on the surface's next texture and shows it.
func present(surface *wgpu.Surface, painter *kuiwgpu.Painter, l *kui.DrawList, width, height int, scale float32) error {
	st, err := surface.TryGetCurrentTexture()
	if err != nil {
		return err
	}
	tex, ok := st.Get()
	if !ok {
		if tex != nil {
			tex.Release()
		}
		return fmt.Errorf("surface texture status %v", st.Status)
	}
	defer tex.Release() // After Present.
	view, err := tex.TryCreateView(nil)
	if err != nil {
		return err
	}
	defer view.Release()
	painter.Clear(kui.RGB(0, 0, 0))
	if err := painter.Paint(view, l, width, height, scale); err != nil {
		return err
	}
	surface.Present()
	return nil
}

// pickFormat returns a surface format storing kui's colors as they are, if there is one:
// sRGB formats blend in linear light.
func pickFormat(formats []wgpu.TextureFormat) wgpu.TextureFormat {
	for _, f := range formats {
		if f == wgpu.TextureFormatBGRA8Unorm || f == wgpu.TextureFormatRGBA8Unorm {
			return f
		}
	}
	return formats[0]
}

func check(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
