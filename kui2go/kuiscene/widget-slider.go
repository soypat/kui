package kuiscene

import (
	"github.com/soypat/kui"
	"github.com/soypat/kui/gesture"
)

// Sliders pack their state in the node state: flags in the low byte, the number of steps in
// the next and the value, scaled to [0, SliderMax], in the high 16 bits.
const (
	sliderStepsShift = 8
	sliderValueShift = 16
	// SliderMax is the value of a slider at its maximum, as reported in [EventChanged].
	SliderMax = 1<<16 - 1
)

// Slider adds a horizontal slider for a value in [0, 1], initially value. With steps > 0 the
// value snaps to multiples of 1/steps, at most 255 steps. It emits [EventChanged] when
// the user changes the value, with Code holding it scaled to [0, SliderMax]. Arrow keys
// change the value by a step, or by 1/20 without steps; Home and End go to the ends.
func (w *WidgetBuilder) Slider(parent kui.Handle, value float32, steps int) kui.Handle {
	b := w.b()
	if steps < 0 || steps > 255 {
		b.Fail(kui.ErrOutOfRange)
		return 0
	}
	style := kui.LayoutStyle{Width: kui.Grow(4*w.theme.SliderSize, 0), Height: kui.Fixed(w.theme.SliderSize)}
	h := b.Node(parent, w.kinds.Slider, &style, &Visual{})
	if h != 0 {
		w.scene.SetState(h, uint32(steps)<<sliderStepsShift)
		w.SetSliderValue(h, value)
		b.SetFlags(h, FlagFocusable)
	}
	return h
}

// SliderValue returns the value of slider h in [0, 1].
func (w *WidgetBuilder) SliderValue(h kui.Handle) float32 {
	return float32(w.scene.State(h)>>sliderValueShift) / SliderMax
}

// SetSliderValue sets the value of slider h, clamped to [0, 1] and snapped to its steps,
// without emitting events.
func (w *WidgetBuilder) SetSliderValue(h kui.Handle, v float32) {
	w.setSlider(h, int(min(max(v, 0), 1)*SliderMax+0.5))
}

// setSlider sets slider h's value scaled to [0, SliderMax], clamping and snapping it. It
// reports whether the value changed.
func (w *WidgetBuilder) setSlider(h kui.Handle, v int) bool {
	st := w.scene.State(h)
	v = min(max(v, 0), SliderMax)
	if steps := int(st >> sliderStepsShift & 0xff); steps > 0 {
		v = (v*steps + SliderMax/2) / SliderMax * SliderMax / steps
	}
	next := st&(1<<sliderValueShift-1) | uint32(v)<<sliderValueShift
	w.scene.SetState(h, next)
	return next != st
}

// sliderTrack returns the horizontal range the center of slider n's thumb moves along.
func (w *WidgetBuilder) sliderTrack(n kui.Handle) (x0, x1 float32) {
	box := w.scene.Bounds(n)
	r := box.H / 2
	return box.X + r, box.X + box.W - r
}

func (w *WidgetBuilder) updateSlider(k *Scene, n kui.Handle, e *NodeEvent) {
	w.updateCommon(n, e)
	st := k.State(n)
	v := int(st >> sliderValueShift)
	step := SliderMax / 20
	if steps := int(st >> sliderStepsShift & 0xff); steps > 0 {
		step = (SliderMax + steps - 1) / steps
	}
	switch e.Kind {
	case gesture.Press, gesture.DragStart, gesture.Drag:
		x0, x1 := w.sliderTrack(n)
		x := k.Bounds(n).X + e.X
		v = int((x - x0) / max(x1-x0, 1) * SliderMax)
	case kui.EventKeyDown:
		switch kui.Key(e.Code) {
		case kui.KeyArrowLeft, kui.KeyArrowDown:
			v -= step
		case kui.KeyArrowRight, kui.KeyArrowUp:
			v += step
		case kui.KeyHome:
			v = 0
		case kui.KeyEnd:
			v = SliderMax
		default:
			return
		}
	default:
		return
	}
	if w.setSlider(n, v) {
		k.Emit(NodeEvent{Node: n, Kind: EventChanged, Code: k.State(n) >> sliderValueShift, Time: e.Time})
	}
}

func (w *WidgetBuilder) paintSlider(k *Scene, n kui.Handle, l *kui.DrawList) {
	box := k.Bounds(n)
	x0, x1 := w.sliderTrack(n)
	thumbX := x0 + w.SliderValue(n)*(x1-x0)
	h := box.H / 3
	track := kui.Rect{X: x0, Y: box.Y + (box.H-h)/2, W: x1 - x0, H: h}
	radius := kui.Radius(uint16(h / 2))
	l.Rect(n, track, w.theme.Field, radius)
	track.W = thumbX - x0
	l.Rect(n, track, w.theme.Accent, radius)
	c := w.theme.Accent
	if k.State(n)&(stateHovered|statePressed) != 0 {
		c = w.theme.ButtonHover
	}
	l.Circle(n, kui.Point{X: thumbX, Y: box.Y + box.H/2}, box.H/2, c)
	w.paintFocus(n, l, box, kui.Radius(uint16(box.H/2)))
}
