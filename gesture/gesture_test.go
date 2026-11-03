package gesture

import (
	"slices"
	"testing"

	"github.com/soypat/kui"
)

const ms = int64(1e6)

// Nodes of the test scene, by index.
const (
	list   = iota // Scroll container: panned by drags, takes no presses.
	item          // In list: double taps and long presses, drags taken over by list.
	slider        // In list: takes its own drags.
	button        // Outside list: hover, no drags, double taps nor long presses.
)

type node struct {
	name    string
	r       kui.Rect
	parent  int // -1 for none.
	sense   Sense
	removed bool
}

// scene is a [Scene] of nodes in paint order. The handle of node i has index i+1.
type scene struct{ nodes []node }

func newScene() *scene {
	return &scene{nodes: []node{
		list:   {name: "list", r: kui.Rect{W: 100, H: 100}, parent: -1, sense: SenseDrag},
		item:   {name: "item", r: kui.Rect{W: 100, H: 20}, parent: list, sense: SensePress | SenseDoubleTap | SenseLongPress},
		slider: {name: "slider", r: kui.Rect{Y: 40, W: 100, H: 20}, parent: list, sense: SensePress | SenseDrag},
		button: {name: "button", r: kui.Rect{X: 120, W: 60, H: 20}, parent: -1, sense: SensePress | SenseHover},
	}}
}

func setup() (*scene, *Recognizer) {
	var r Recognizer
	r.Configure(Config{})
	return newScene(), &r
}

func (s *scene) handle(i int) kui.Handle { return kui.MakeHandle(i+1, 1) }

func (s *scene) index(h kui.Handle) int {
	i := h.Index() - 1
	if h.IsZero() || i >= len(s.nodes) || s.nodes[i].removed {
		return -1
	}
	return i
}

// hit returns the topmost node at p sensing any of sense, or -1.
func (s *scene) hit(p kui.Point, sense Sense) int {
	for i := len(s.nodes) - 1; i >= 0; i-- {
		if !s.nodes[i].removed && s.nodes[i].sense&sense != 0 && s.nodes[i].r.Contains(p) {
			return i
		}
	}
	return -1
}

func (s *scene) Press(p kui.Point) (kui.Handle, bool) {
	if i := s.hit(p, SensePress); i >= 0 {
		return s.handle(i), true
	} else if i := s.hit(p, SenseDrag); i >= 0 {
		return s.handle(i), false
	}
	return 0, false
}

func (s *scene) Hover(p kui.Point) kui.Handle {
	if i := s.hit(p, SenseHover); i >= 0 {
		return s.handle(i)
	}
	return 0
}

func (s *scene) DragTarget(n kui.Handle) kui.Handle {
	for i := s.index(n); i >= 0; i = s.nodes[i].parent {
		if s.nodes[i].sense&SenseDrag != 0 {
			return s.handle(i)
		}
	}
	return 0
}

func (s *scene) Sense(n kui.Handle) Sense {
	if i := s.index(n); i >= 0 {
		return s.nodes[i].sense
	}
	return 0
}

func (s *scene) center(i int) (float32, float32) {
	r := s.nodes[i].r
	return r.X + r.W/2, r.Y + r.H/2
}

// names returns gestures as "Kind node" strings, "-" naming no node.
func (s *scene) names(gs []Event) []string {
	var out []string
	for _, g := range gs {
		name := "-"
		if i := g.Node.Index() - 1; !g.Node.IsZero() && i < len(s.nodes) {
			name = s.nodes[i].name
		}
		out = append(out, KindString(g.Kind)+" "+name)
	}
	return out
}

func feed(r *Recognizer, s Scene, evs ...kui.Event) []Event {
	var out []Event
	for i := range evs {
		out = r.Feed(s, &evs[i], out)
	}
	return out
}

// events builds the events of tests, whose times and sources are set with at and from so
// scripted input replays deterministically.
var events kui.EventBuilder

// at returns e with time t.
func at(t int64, e kui.Event) kui.Event {
	e.Time = t
	return e
}

// from returns e with source s.
func from(s uint8, e kui.Event) kui.Event {
	e.Source = s
	return e
}

func down(t int64, src uint8, x, y float32) kui.Event {
	return at(t, from(src, events.PointerDown(x, y, kui.ButtonPrimary)))
}

func up(t int64, src uint8, x, y float32) kui.Event {
	return at(t, from(src, events.PointerUp(x, y, kui.ButtonPrimary)))
}

func move(t int64, src uint8, x, y float32) kui.Event {
	return at(t, from(src, events.PointerMove(x, y, 0)))
}

func check(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("%s:\nwant %q\ngot  %q", what, want, got)
	}
}

func TestTap(t *testing.T) {
	s, r := setup()
	x, y := s.center(item)
	gs := feed(r, s, down(0, 0, x, y), up(ms, 0, x+2, y))
	check(t, "tap", s.names(gs), "press item", "tap item", "release item")
	if g := gs[1]; g.X != x+2 || g.Y != y || g.Time != ms || g.Button != kui.ButtonPrimary {
		t.Fatalf("tap fields: %+v", g)
	}
	gs = feed(r, s,
		at(900*ms, events.PointerDown(x, y, kui.ButtonSecondary)),
		at(901*ms, events.PointerUp(x, y, kui.ButtonSecondary)))
	check(t, "secondary tap", s.names(gs), "press item", "secondary tap item", "release item")
	// Releasing outside the pressed node is not a tap. The button does not drag, so moving
	// beyond the slop does not start a drag either.
	bx, by := s.center(button)
	gs = feed(r, s, down(2000*ms, 0, bx, by), move(2001*ms, 0, bx, by+50), up(2002*ms, 0, bx, by+50))
	check(t, "release outside", s.names(gs), "press button", "release button")
	// Pressing nothing reports a press on no node and nothing else.
	gs = feed(r, s, down(3000*ms, 0, 190, 90), move(3001*ms, 0, 150, 90), up(3002*ms, 0, 150, 90))
	check(t, "press on nothing", s.names(gs), "press -")
}

func TestDoubleTap(t *testing.T) {
	s, r := setup()
	x, y := s.center(item)
	tap := func(at int64, x float32) []string {
		return s.names(feed(r, s, down(at, 0, x, y), up(at+ms, 0, x, y)))[1:2]
	}
	check(t, "first", tap(0, x), "tap item")
	check(t, "second", tap(100*ms, x), "double tap item")
	check(t, "third starts over", tap(200*ms, x), "tap item")
	check(t, "too late", tap(600*ms, x), "tap item")
	check(t, "too far", tap(700*ms, x+20), "tap item")
	check(t, "close enough", tap(800*ms, x+15), "double tap item")
	// Nodes not sensing double taps get taps.
	bx, by := s.center(button)
	gs := feed(r, s, down(2000*ms, 0, bx, by), up(2001*ms, 0, bx, by), down(2050*ms, 0, bx, by), up(2051*ms, 0, bx, by))
	check(t, "button", s.names(gs), "press button", "tap button", "release button", "press button", "tap button", "release button")
}

func TestLongPress(t *testing.T) {
	s, r := setup()
	x, y := s.center(item)
	feed(r, s, down(1000*ms, 0, x, y))
	if d, ok := r.Deadline(s); !ok || d != 1500*ms {
		t.Fatalf("deadline: %d %v", d, ok)
	}
	if gs := r.Tick(s, 1499*ms, nil); len(gs) != 0 {
		t.Fatalf("early long press: %q", s.names(gs))
	}
	gs := r.Tick(s, 1500*ms, nil)
	check(t, "long press", s.names(gs), "long press item")
	if g := gs[0]; g.X != x || g.Y != y || g.Time != 1500*ms || g.Button != kui.ButtonPrimary {
		t.Fatalf("long press fields: %+v", g)
	}
	if _, ok := r.Deadline(s); ok {
		t.Fatal("deadline after long press")
	}
	check(t, "tick after long press", s.names(r.Tick(s, 2000*ms, nil)))
	check(t, "release after long press", s.names(feed(r, s, up(2000*ms, 0, x, y))), "release item")
	// Nodes not sensing long presses get a tap however long they are held.
	bx, by := s.center(button)
	feed(r, s, down(3000*ms, 0, bx, by))
	if _, ok := r.Deadline(s); ok {
		t.Fatal("deadline for button")
	}
	check(t, "tick button", s.names(r.Tick(s, 9000*ms, nil)))
	check(t, "held tap", s.names(feed(r, s, up(9000*ms, 0, bx, by))), "tap button", "release button")
}

func TestDrag(t *testing.T) {
	s, r := setup()
	x, y := s.center(slider)
	gs := feed(r, s,
		down(0, 0, x, y),
		move(1*ms, 0, x+2, y),  // Within slop.
		move(2*ms, 0, x+10, y), // Drag start.
		move(3*ms, 0, x+15, y+1),
		up(4*ms, 0, x+16, y+1))
	check(t, "drag", s.names(gs), "press slider", "drag start slider", "drag slider", "drag end slider", "release slider")
	for i, want := range []kui.Point{{X: 10}, {X: 5, Y: 1}, {X: 1}} {
		if g := gs[i+1]; g.DX != want.X || g.DY != want.Y {
			t.Fatalf("%v delta: want %v, got %v,%v", KindString(g.Kind), want, g.DX, g.DY)
		}
	}
}

func TestDragTakeover(t *testing.T) {
	s, r := setup()
	x, y := s.center(item)
	gs := feed(r, s, down(0, 0, x, y), move(ms, 0, x, y+20))
	check(t, "takeover", s.names(gs), "press item", "release item", "drag start list")
	if g := gs[2]; g.DY != 20 {
		t.Fatalf("drag start delta: %+v", g)
	}
	if _, ok := r.Deadline(s); ok {
		t.Fatal("dragged press can still long press")
	}
	gs = feed(r, s, move(2*ms, 0, x, y+25), up(3*ms, 0, x, y+25))
	check(t, "pan", s.names(gs), "drag list", "drag end list")
	// Dragging content taking no presses pans its container without pressing anything.
	gs = feed(r, s, down(10*ms, 0, x, 80), move(11*ms, 0, x, 60), up(12*ms, 0, x, 60))
	check(t, "pan background", s.names(gs), "press -", "drag start list", "drag end list")
}

func TestCancel(t *testing.T) {
	s, r := setup()
	x, y := s.center(item)
	gs := feed(r, s, down(0, 0, x, y), kui.Event{Time: ms, Kind: kui.EventPointerCancel})
	check(t, "cancel", s.names(gs), "press item", "release item")
	check(t, "up after cancel", s.names(feed(r, s, up(2*ms, 0, x, y))))
	// Blur cancels every pointer at its last position.
	sx, sy := s.center(slider)
	feed(r, s, down(10*ms, 1, sx, sy), move(11*ms, 1, sx+10, sy), down(12*ms, 2, x, y))
	gs = feed(r, s, kui.Event{Time: 20 * ms, Kind: kui.EventBlur})
	check(t, "blur", s.names(gs), "drag end slider", "release slider", "release item")
	want := []struct {
		src  uint8
		x, y float32
	}{{1, sx + 10, sy}, {1, sx + 10, sy}, {2, x, y}}
	for i, w := range want {
		if g := gs[i]; g.Source != w.src || g.X != w.x || g.Y != w.y || g.Time != 20*ms {
			t.Fatalf("blur gesture %d: %+v", i, g)
		}
	}
}

func TestMultiPointer(t *testing.T) {
	s, r := setup()
	x, y := s.center(item)
	bx, by := s.center(button)
	sx, sy := s.center(slider)
	gs := feed(r, s,
		down(0, 1, x, y),
		down(1*ms, 2, sx, sy),
		down(2*ms, 3, bx, by),
		move(3*ms, 2, sx+20, sy),
		up(4*ms, 1, x, y),
		up(5*ms, 3, bx, by),
		up(6*ms, 2, sx+20, sy))
	check(t, "pointers", s.names(gs),
		"press item", "press slider", "press button", "drag start slider",
		"tap item", "release item", "tap button", "release button", "drag end slider", "release slider")
	sources := []uint8{1, 2, 3, 2, 1, 1, 3, 3, 2, 2}
	for i, g := range gs {
		if g.Source != sources[i] {
			t.Fatalf("gesture %d %v: source %d, want %d", i, KindString(g.Kind), g.Source, sources[i])
		}
	}
}

func TestHover(t *testing.T) {
	s, r := setup()
	bx, by := s.center(button)
	gs := feed(r, s,
		move(0, 0, bx, by),
		move(1*ms, 0, bx+1, by),
		move(2*ms, 0, bx, by+50),
		move(3*ms, 0, bx, by),
		kui.Event{Time: 4 * ms, Kind: kui.EventPointerLeave},
		kui.Event{Time: 5 * ms, Kind: kui.EventPointerLeave})
	check(t, "hover", s.names(gs), "hover in button", "hover move button", "hover out button", "hover in button", "hover out button")
	// Each pointer hovers on its own.
	gs = feed(r, s, move(10*ms, 1, bx, by), move(11*ms, 2, bx, by), move(12*ms, 1, 0, 0))
	check(t, "pointers", s.names(gs), "hover in button", "hover in button", "hover out button")
	if gs[2].Source != 1 {
		t.Fatalf("hover out source: %d", gs[2].Source)
	}
}

func TestRemovedNode(t *testing.T) {
	s, r := setup()
	x, y := s.center(item)
	feed(r, s, down(0, 0, x, y))
	s.nodes[item].removed = true
	if _, ok := r.Deadline(s); ok {
		t.Fatal("deadline for removed node")
	}
	// The press still ends with a release, which is not a tap.
	check(t, "release", s.names(feed(r, s, up(ms, 0, x, y))), "release item")
	// Hovering a node that is removed reports leaving it.
	bx, by := s.center(button)
	feed(r, s, move(10*ms, 0, bx, by))
	s.nodes[button].removed = true
	check(t, "hover", s.names(feed(r, s, move(11*ms, 0, bx, by))), "hover out button")
}

func TestAllocs(t *testing.T) {
	s, r := setup()
	x, y := s.center(item)
	sx, sy := s.center(slider)
	evs := []kui.Event{
		down(0, 0, x, y), up(0, 0, x, y),
		down(0, 1, sx, sy), move(0, 1, sx+10, sy), up(0, 1, sx+10, sy),
		move(0, 0, 150, 10), {Kind: kui.EventPointerLeave},
	}
	dst := make([]Event, 0, MaxEvents)
	now := int64(0)
	allocs := testing.AllocsPerRun(100, func() {
		now += 1000 * ms
		for i := range evs {
			evs[i].Time = now
			dst = r.Feed(s, &evs[i], dst[:0])
		}
		dst = r.Tick(s, now, dst[:0])
	})
	if allocs != 0 {
		t.Fatal("allocations:", allocs)
	}
}

// TestInvariants feeds random input and checks that gestures nest properly per pointer: presses
// release once, drags end, hovers leave, and taps and long presses happen while pressed.
func TestInvariants(t *testing.T) {
	s, r := setup()
	const sources = 3
	var pressed, dragging, hovered [sources]kui.Handle
	var isDown [sources]bool
	var seen [HoverOut + 1]int
	seed := uint32(1)
	rnd := func(n int) int {
		seed = seed*1664525 + 1013904223
		return int(seed>>16) % n
	}
	fail := func(i int, g Event, what string) {
		t.Helper()
		t.Fatalf("step %d: %v of node %d from source %d: %s", i, KindString(g.Kind), g.Node.Index(), g.Source, what)
	}
	now := int64(0)
	var gs []Event
	var last [sources]kui.Point
	for i := range 20000 {
		now += int64(rnd(40)) * ms
		if rnd(10) == 0 {
			now += 600 * ms // Long enough for long presses.
		}
		src := uint8(rnd(sources))
		// Pointers mostly stay put, so taps and double taps happen.
		p := &last[src]
		if rnd(8) == 0 {
			*p = kui.Point{X: float32(rnd(200)), Y: float32(rnd(100))}
		}
		var ev kui.Event
		switch op := rnd(20); {
		case op < 1:
			ev = kui.Event{Time: now, Kind: kui.EventBlur}
			isDown = [sources]bool{}
		case op < 2:
			ev = kui.Event{Time: now, Kind: kui.EventPointerLeave, Source: src}
		case op < 3:
			ev = kui.Event{Time: now, Kind: kui.EventPointerCancel, Source: src}
			isDown[src] = false
		case op < 11 && !isDown[src]:
			ev = at(now, from(src, events.PointerDown(p.X, p.Y, kui.PointerButton(rnd(3)))))
			isDown[src] = true
		case op < 11:
			ev = up(now, src, p.X, p.Y)
			isDown[src] = false
		default:
			p.X += float32(rnd(3) - 1)
			ev = move(now, src, p.X, p.Y)
		}
		gs = r.Feed(s, &ev, gs[:0])
		if len(gs) > MaxEvents {
			t.Fatalf("step %d: %d gestures", i, len(gs))
		}
		gs = r.Tick(s, now, gs)
		for _, g := range gs {
			seen[g.Kind]++
			p, d, h := &pressed[g.Source], &dragging[g.Source], &hovered[g.Source]
			switch g.Kind {
			case Press:
				if *p != 0 {
					fail(i, g, "already pressed")
				}
				*p = g.Node
			case Release:
				if *p != g.Node {
					fail(i, g, "not pressed")
				}
				*p = 0
			case Tap, SecondaryTap, DoubleTap, LongPress:
				if *p != g.Node {
					fail(i, g, "not pressed")
				}
			case DragStart:
				if *d != 0 || *p != 0 && *p != g.Node {
					fail(i, g, "already dragging or pressing another node")
				}
				*d = g.Node
			case Drag, DragEnd:
				if *d != g.Node {
					fail(i, g, "not dragging")
				}
				if g.Kind == DragEnd {
					*d = 0
				}
			case HoverIn:
				if *h != 0 {
					fail(i, g, "already hovering")
				}
				*h = g.Node
			case HoverMove, HoverOut:
				if *h != g.Node {
					fail(i, g, "not hovering")
				}
				if g.Kind == HoverOut {
					*h = 0
				}
			}
		}
	}
	for k := Press; k <= HoverOut; k++ {
		if seen[k] == 0 {
			t.Errorf("no %v gestures", KindString(k))
		}
	}
	t.Logf("gestures seen: %v", seen[Press:])
}

func TestKind(t *testing.T) {
	if EventExt0 != HoverOut+1 {
		t.Error("EventExt0 overlaps gesture kinds")
	}
	for k := Press; k < EventExt0; k++ {
		g, ok := KindOf(k)
		if !ok || kui.EventKind(g) != k || g.Sense() == 0 || KindString(k) != g.String() || g.String() == k.String() {
			t.Errorf("kind %d: %v %v sense %v", k, g, ok, g.Sense())
		}
	}
	for _, k := range []kui.EventKind{0, kui.EventKeyDown, Press - 1, EventExt0} {
		if g, ok := KindOf(k); ok || g != 0 || Kind(k).Sense() != 0 || KindString(k) != k.StringConst() {
			t.Errorf("kind %d taken for a gesture", k)
		}
	}
	senses := map[kui.EventKind]Sense{
		Press: SensePress, SecondaryTap: SensePress, DoubleTap: SenseDoubleTap, LongPress: SenseLongPress,
		DragStart: SenseDrag, DragEnd: SenseDrag, HoverIn: SenseHover, HoverOut: SenseHover,
	}
	for k, want := range senses {
		if g, _ := KindOf(k); g.Sense() != want {
			t.Errorf("%v: sense %v, want %v", g, g.Sense(), want)
		}
	}
}
