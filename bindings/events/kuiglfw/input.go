// Package kuiglfw binds the input of a GLFW window to kui: [Input] translates the window's
// callbacks to [kui.Event]s written to any [kui.EventWriter], and carries out the cursor and
// clipboard [kui.Request]s of a UI.
//
// The window and its graphics context are created by the caller, so the binding pairs with
// any renderer. GLFW runs callbacks inside glfw.PollEvents and glfw.WaitEvents on the main OS
// thread: the Input is then the single producer of its EventWriter, and must be used from
// that thread only.
//
// Input does not allocate after [Input.Configure], except reading the clipboard.
package kuiglfw

import (
	"time"

	"github.com/go-gl/glfw/v3.4/glfw"
	"github.com/soypat/kui"
)

// Config configures an [Input].
type Config struct {
	// Events receives the window's input. Required.
	Events kui.EventWriter
	// Nanotime is the clock events are stamped with, in monotonic nanoseconds. Defaults to the
	// time since Configure.
	Nanotime func() int64
	// ScrollStep is the scroll distance of a wheel notch in logical pixels. Defaults to 20.
	ScrollStep float32
}

// Input translates the input of a GLFW window to kui events. Positions and sizes are in
// logical pixels, GLFW's screen coordinates; [Input.Scale] converts them to framebuffer pixels.
//
// Pointer moves and scrolls are coalesced: consecutive ones are merged and written when
// another event arrives or on [Input.Flush], which should be called after polling GLFW.
// Events that do not fit the writer are dropped and counted by [Input.Dropped].
type Input struct {
	win        *glfw.Window
	w          kui.EventWriter
	events     kui.EventBuilder // Stamps events and tracks modifiers.
	scrollStep float32
	x, y       float32 // Last pointer position.
	width      int     // Last size written, in logical pixels.
	height     int
	fbWidth    int // Framebuffer size in pixels.
	fbHeight   int
	scale      float32 // Framebuffer pixels per logical pixel.
	pending    kui.Event
	hasPending bool
	buf        [2]kui.Event // Events being written. A field so writing does not allocate.
	dropped    int
	cursor     kui.Cursor // Current cursor shape.
	cursors    [numCursors]*glfw.Cursor
}

const numCursors = int(kui.CursorResize) + 1

// Configure binds in to win, replacing win's input callbacks, and writes a
// [kui.EventResize] with win's size and scale.
func (in *Input) Configure(win *glfw.Window, cfg Config) error {
	if win == nil {
		return kui.ErrInvalidConfig
	}
	if err := in.configure(cfg); err != nil {
		return err
	}
	in.win = win
	shapes := [numCursors]glfw.StandardCursor{
		kui.CursorDefault: glfw.ArrowCursor,
		kui.CursorPointer: glfw.HandCursor,
		kui.CursorText:    glfw.IBeamCursor,
		kui.CursorGrab:    glfw.HandCursor,
		kui.CursorResize:  glfw.HResizeCursor,
	}
	for i, shape := range shapes {
		if in.cursors[i] == nil {
			in.cursors[i] = glfw.CreateStandardCursor(shape)
		}
	}
	win.SetCursor(nil)
	win.SetCursorPosCallback(func(_ *glfw.Window, x, y float64) { in.cursorPos(x, y) })
	win.SetCursorEnterCallback(func(_ *glfw.Window, entered bool) { in.cursorEnter(entered) })
	win.SetMouseButtonCallback(func(_ *glfw.Window, b glfw.MouseButton, action glfw.Action, mods glfw.ModifierKey) {
		in.mouseButton(b, action, mods)
	})
	win.SetScrollCallback(func(_ *glfw.Window, dx, dy float64) { in.scroll(dx, dy) })
	win.SetKeyCallback(func(_ *glfw.Window, key glfw.Key, _ int, action glfw.Action, mods glfw.ModifierKey) {
		in.key(key, action, mods)
	})
	win.SetCharCallback(func(_ *glfw.Window, r rune) { in.char(r) })
	win.SetSizeCallback(func(w *glfw.Window, width, height int) {
		fw, fh := w.GetFramebufferSize()
		in.resize(width, height, fw, fh)
	})
	win.SetFramebufferSizeCallback(func(w *glfw.Window, fw, fh int) {
		width, height := w.GetSize()
		in.resize(width, height, fw, fh)
	})
	win.SetFocusCallback(func(_ *glfw.Window, focused bool) { in.focus(focused) })
	width, height := win.GetSize()
	fw, fh := win.GetFramebufferSize()
	in.resize(width, height, fw, fh)
	return nil
}

// configure sets the state that does not involve a window.
func (in *Input) configure(cfg Config) error {
	if cfg.Events == nil {
		return kui.ErrInvalidConfig
	}
	if cfg.Nanotime == nil {
		start := time.Now()
		cfg.Nanotime = func() int64 { return int64(time.Since(start)) }
	}
	if cfg.ScrollStep <= 0 {
		cfg.ScrollStep = 20
	}
	*in = Input{w: cfg.Events, scrollStep: cfg.ScrollStep, scale: 1, cursors: in.cursors}
	in.events.Configure(kui.EventBuilderConfig{Nanotime: cfg.Nanotime})
	return nil
}

// Flush writes the pending coalesced pointer move or scroll, if any. Call it after
// glfw.PollEvents and glfw.WaitEvents so the last move of a poll is not held back.
func (in *Input) Flush() {
	if in.hasPending {
		in.hasPending = false
		in.buf[0] = in.pending
		in.dropped += 1 - in.w.WriteEvents(in.buf[:1])
	}
}

// Scale returns the number of framebuffer pixels per logical pixel, as in the last
// [kui.EventResize] written.
func (in *Input) Scale() float32 { return in.scale }

// FramebufferSize returns the size in pixels of the window's framebuffer, tracked from GLFW's
// callbacks. Unlike asking GLFW, it does not allocate.
func (in *Input) FramebufferSize() (width, height int) { return in.fbWidth, in.fbHeight }

// Dropped returns the number of events dropped because they did not fit the writer.
func (in *Input) Dropped() int { return in.dropped }

// push writes the pending event, if any, followed by e.
func (in *Input) push(e kui.Event) {
	n := 0
	if in.hasPending {
		in.hasPending = false
		in.buf[0] = in.pending
		n = 1
	}
	in.buf[n] = e
	n++
	in.dropped += n - in.w.WriteEvents(in.buf[:n])
}

// coalesce merges e into the pending event, or writes the pending event and holds e.
func (in *Input) coalesce(e kui.Event) {
	if in.hasPending && in.pending.Coalesce(e) {
		return
	}
	in.Flush()
	in.pending, in.hasPending = e, true
}

func (in *Input) cursorPos(x, y float64) {
	in.x, in.y = float32(x), float32(y)
	in.coalesce(in.events.PointerMove(in.x, in.y, kui.ButtonNone))
}

func (in *Input) cursorEnter(entered bool) {
	if !entered {
		in.push(in.events.NewEvent(kui.EventPointerLeave))
	}
}

func (in *Input) mouseButton(b glfw.MouseButton, action glfw.Action, mods glfw.ModifierKey) {
	in.events.SetKeyMods(ToMods(mods))
	btn := ToButton(b)
	if btn == kui.ButtonNone {
		return
	}
	switch action {
	case glfw.Press:
		in.push(in.events.PointerDown(in.x, in.y, btn))
	case glfw.Release:
		in.push(in.events.PointerUp(in.x, in.y, btn))
	}
}

// scroll writes a scroll of GLFW's wheel offsets, positive when scrolling up or left. kui's
// scroll amounts are positive when content scrolls down, so they are negated.
func (in *Input) scroll(dx, dy float64) {
	step := float64(in.scrollStep)
	in.coalesce(in.events.Scroll(in.x, in.y, float32(-dx*step), float32(-dy*step)))
}

func (in *Input) key(key glfw.Key, action glfw.Action, mods glfw.ModifierKey) {
	in.events.SetKeyMods(ToMods(mods))
	k := ToKey(key)
	if k == 0 {
		return
	}
	switch action {
	case glfw.Press, glfw.Repeat:
		in.push(in.events.KeyDown(k))
	case glfw.Release:
		in.push(in.events.KeyUp(k))
	}
}

func (in *Input) char(r rune) { in.push(in.events.Rune(r)) }

// resize writes a resize for a window of width by height logical pixels whose framebuffer is
// fbWidth by fbHeight pixels, unless nothing changed. Minimized windows, of zero size, are
// ignored.
func (in *Input) resize(width, height, fbWidth, fbHeight int) {
	in.fbWidth, in.fbHeight = fbWidth, fbHeight
	if width <= 0 || height <= 0 {
		return
	}
	scale := float32(1)
	if fbWidth > 0 {
		scale = float32(fbWidth) / float32(width)
	}
	if width == in.width && height == in.height && scale == in.scale {
		return
	}
	in.width, in.height, in.scale = width, height, scale
	in.push(in.events.Resize(float32(width), float32(height), scale))
}

func (in *Input) focus(focused bool) {
	kind := kui.EventBlur
	if focused {
		kind = kui.EventFocus
	}
	in.push(in.events.NewEvent(kind))
}
