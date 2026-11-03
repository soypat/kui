// Package kuiscene implements kui's retained node tree, layout replay, hit testing, input routing,
// focus and painting. Pointer gestures are recognized by package [gesture] and routed by hit
// testing. It does not allocate after [Scene.Configure]: all memory comes from a [Memory]
// sized up front.
//
// The scene is push driven so any host loop can own control (desktop, mobile, web or a
// microcontroller super loop): the host feeds input with [Scene.Step], reacts to the
// resulting [Scene.Events], and draws [Scene.Paint]'s draw list. Time only enters
// through events and Step's now argument.
//
// Widget behavior is table driven: each node has a [Kind] whose [KindSpec] declares which
// pointer input it senses, how its state reacts to events and how it is painted.
package kuiscene

import (
	"unsafe"

	"github.com/soypat/kui"
	"github.com/soypat/kui/gesture"
)

// Kind identifies a node kind registered with [Scene.RegisterKind].
type Kind uint8

// KindBox is the built-in container kind. It paints its background, text and border and senses no input.
const KindBox Kind = 0

// MaxKinds is the maximum number of kinds a scene can register, including [KindBox].
const MaxKinds = 32

// Flags are per node flags.
type Flags uint8

const (
	// FlagHidden excludes the node and its descendants from layout, painting and input.
	FlagHidden Flags = 1 << iota
	// FlagDisabled excludes the node from input and focus. Its kind may paint it differently.
	FlagDisabled
	// FlagFocusable makes the node reachable by keyboard focus traversal.
	FlagFocusable
	// FlagScrollable makes the scene scroll the node in response to scroll events by adjusting its
	// [kui.LayoutStyle.Scroll], clamped to its content. The node should clip (ClipX or ClipY).
	FlagScrollable
	// FlagModal makes the node capture input while it is visible, i.e. a popup or dialog. Pointer
	// input outside its subtree is discarded, focus traversal stays inside it, and pressing
	// outside it or pressing Escape emits [EventDismiss] to it. Only the last modal node in paint
	// order is active.
	FlagModal
	flagAllocated
)

// Visual holds the visual style of a node, used by kind painters.
type Visual struct {
	Background  kui.Color
	BorderColor kui.Color
	Border      kui.DrawBorders
	Radius      kui.DrawRadii
	// Text styles the node's text. Text.Color is the text color.
	Text kui.TextStyle
}

// KindSpec defines the behavior of a node kind.
type KindSpec struct {
	// Sense is the pointer input hit testing routes to nodes of this kind. Any of SensePress,
	// SenseDrag, SenseDoubleTap and SenseLongPress makes presses hit the node. Keys and text go
	// to the focused node regardless, see [FlagFocusable].
	Sense gesture.Sense
	// Update, if not nil, updates the state of node n in response to e. It is called for every
	// event targeting a node of this kind in the order events were produced, including events
	// emitted by other Update calls. It may emit further events with [Scene.Emit].
	Update func(k *Scene, n kui.Handle, e *NodeEvent)
	// Paint, if not nil, draws node n before its children. Nil uses [Scene.PaintBox].
	// Nodes that clip (ClipX or ClipY) are clipped to their box while painting.
	Paint func(k *Scene, n kui.Handle, l *kui.DrawList)
	// PaintOver, if not nil, draws over node n's children, outside of the node's own clip,
	// i.e. scroll bars and focus rings. The node's border is drawn by the scene afterwards.
	PaintOver func(k *Scene, n kui.Handle, l *kui.DrawList)
	// Cursor is the cursor requested while the pointer hovers a node of this kind.
	Cursor kui.Cursor
}

type links struct {
	parent, first, last, next, prev int32 // -1 for none.
}

type span struct {
	off, len, cap uint32
}

type timer struct {
	node kui.Handle
	code uint32
	at   int64
}

// Memory holds all memory used by a [Scene]. Create it with [NewMemory] or [Memory.Init].
type Memory struct {
	links  []links
	gen    []uint8
	kind   []Kind
	flags  []Flags
	style  []kui.LayoutStyle
	visual []Visual
	text   []span
	state  []uint32
	user   []uint32
	box    []kui.Rect // Layout output.
	clip   []kui.Rect // Region where the node is visible.
	// tree is the visible tree laid out by the layout engine. Its Style and Box are the node
	// columns style and box.
	tree   kui.Tree
	free   []int32
	stack  []int32
	roots  []root
	hits   hitIndex
	arena  []byte
	events []NodeEvent
	timers []timer
	reqs   []kui.Request
	// reqText holds the text of requests, i.e. text copied to the clipboard.
	reqText []byte
}

type root struct {
	node int32
	z    int16
}

// MemoryConfig sizes a [Memory].
type MemoryConfig struct {
	MaxNodes  int // Maximum number of nodes, including the root node.
	TextBytes int // Size of the arena holding node text.
	MaxEvents int // Maximum number of widget events produced by a single Step.
	// MaxLines is the maximum number of visible text lines, of all nodes. Defaults to the larger
	// of 64 and MaxNodes.
	MaxLines  int
	MaxTimers int // Maximum number of pending timers, see [Scene.SetTimer]. Defaults to 8.
	// MaxRequests is the maximum number of host requests produced by a single Step. Defaults to 8.
	MaxRequests int
	// RequestTextBytes is the size of the buffer holding request text, i.e. text copied to the
	// clipboard. Defaults to 256.
	RequestTextBytes int
}

// NewMemory allocates memory for a [Scene].
func NewMemory(cfg MemoryConfig) *Memory {
	var m Memory
	m.Init(cfg)
	return &m
}

// Init allocates m's buffers, reusing existing ones with enough capacity.
func (m *Memory) Init(cfg MemoryConfig) {
	n := cfg.MaxNodes + 1 // Index 0 is never used so zero handles never resolve.
	cfg.MaxLines = positiveOr(cfg.MaxLines, max(64, cfg.MaxNodes))
	cfg.MaxTimers = positiveOr(cfg.MaxTimers, 8)
	cfg.MaxRequests = positiveOr(cfg.MaxRequests, 8)
	cfg.RequestTextBytes = positiveOr(cfg.RequestTextBytes, 256)
	m.links = resize(m.links, n)
	m.gen = resize(m.gen, n)
	m.kind = resize(m.kind, n)
	m.flags = resize(m.flags, n)
	m.style = resize(m.style, n)
	m.visual = resize(m.visual, n)
	m.text = resize(m.text, n)
	m.state = resize(m.state, n)
	m.user = resize(m.user, n)
	m.box = resize(m.box, n)
	m.clip = resize(m.clip, n)
	m.free = resize(m.free, n)[:0]
	m.stack = resize(m.stack, 2*n)[:0]
	m.roots = resize(m.roots, n)[:0]
	m.hits.init(n)
	m.arena = resize(m.arena, cfg.TextBytes)[:0]
	m.events = resize(m.events, cfg.MaxEvents)[:0]
	m.timers = resize(m.timers, cfg.MaxTimers)[:0]
	m.reqs = resize(m.reqs, cfg.MaxRequests)[:0]
	m.reqText = resize(m.reqText, cfg.RequestTextBytes)[:0]
	t := &m.tree
	t.Order = resize(t.Order, n)[:0]
	t.Subtree = resize(t.Subtree, n)
	t.Style = m.style
	t.Text = resize(t.Text, n)
	t.TextStyle = resize(t.TextStyle, n)
	t.Box = m.box
	t.Content = resize(t.Content, n)
	t.LineSpan = resize(t.LineSpan, n)
	t.Lines = resize(t.Lines, cfg.MaxLines)[:0]
}

func positiveOr(v, def int) int {
	if v > 0 {
		return v
	}
	return def
}

func resize[T any](s []T, n int) []T {
	if cap(s) >= n {
		s = s[:n:n]
		clear(s)
		return s
	}
	return make([]T, n)
}

func (m *Memory) maxNodes() int { return len(m.links) }

// Config configures a [Scene].
type Config struct {
	Memory   *Memory
	Engine   kui.LayoutEngine
	Viewport kui.Size
	Gestures gesture.Config
	// RootStyle and RootVisual style the root node. A zero RootStyle makes the root fill the viewport.
	RootStyle  kui.LayoutStyle
	RootVisual Visual
}

// TextSource resolves the text of [kui.EventText] and [kui.EventPreedit] events whose text is
// held by the queue they were drained from ([kui.Event.IsString]). Every [kui.EventReader], i.e.
// the SPSC and Fan of package event, implements it.
type TextSource interface {
	EventTextWrite(dst []byte, e *kui.Event) int
}

// Scene is a retained UI tree with input handling and painting. See the package documentation.
type Scene struct {
	mem        *Memory
	engine     kui.LayoutEngine
	viewport   kui.Size
	root       int32
	kinds      [MaxKinds]KindSpec
	nkinds     int
	gest       gesture.Recognizer
	gestures   [gesture.MaxEvents]gesture.Event // Gestures of the event being routed.
	focus      kui.Handle
	needLayout bool
	needPaint  bool
	now        int64
	textSrc    TextSource
	builder    Builder
	cursor     kui.Cursor
	dropped    int
	// stepEnd and reqEnd are the number of events and requests at the end of the last Step.
	// Those added after them, outside Step (i.e. by SetFocus), are delivered with the next Step.
	stepEnd     int
	reqEnd      int
	modal       int32 // Active modal node, -1 for none.
	bytesNeeded int
	layoutErr   error
}

// Configure prepares the scene. It creates the root node and registers [KindBox].
func (scene *Scene) Configure(cfg Config) error {
	if cfg.Memory == nil || cfg.Engine == nil || cfg.Memory.maxNodes() < 2 || cfg.Memory.maxNodes()-1 > kui.MaxHandleIndex {
		return kui.ErrInvalidConfig
	}
	*scene = Scene{
		mem:        cfg.Memory,
		engine:     cfg.Engine,
		viewport:   cfg.Viewport,
		needLayout: true,
		needPaint:  true,
		modal:      -1,
	}
	scene.gest.Configure(cfg.Gestures)
	m := scene.mem
	for i := range m.links {
		m.links[i] = links{parent: -1, first: -1, last: -1, next: -1, prev: -1}
		m.gen[i] = 1
	}
	m.free = m.free[:0]
	for i := m.maxNodes() - 1; i >= 1; i-- {
		m.free = append(m.free, int32(i))
	}
	m.arena = m.arena[:0]
	m.events = m.events[:0]
	m.timers = m.timers[:0]
	m.reqs = m.reqs[:0]
	m.reqText = m.reqText[:0]
	scene.nkinds = 1
	scene.kinds[KindBox] = KindSpec{}
	scene.builder.k = scene
	style := cfg.RootStyle
	if style == (kui.LayoutStyle{}) {
		style.Width, style.Height = kui.Grow(0, 0), kui.Grow(0, 0)
	}
	scene.root = scene.alloc(KindBox, &style, &cfg.RootVisual)
	return nil
}

// RegisterKind registers a node kind and returns its identifier.
func (scene *Scene) RegisterKind(spec KindSpec) (Kind, error) {
	if scene.nkinds == MaxKinds {
		return 0, kui.ErrCapacity
	}
	scene.kinds[scene.nkinds] = spec
	scene.nkinds++
	return Kind(scene.nkinds - 1), nil
}

// Root returns the root node, which spans the viewport.
func (scene *Scene) Root() kui.Handle { return scene.handle(scene.root) }

// SetViewport sets the size of the viewport in logical pixels.
func (scene *Scene) SetViewport(sz kui.Size) {
	if sz != scene.viewport {
		scene.viewport = sz
		scene.invalidate()
	}
}

// Viewport returns the size of the viewport.
func (scene *Scene) Viewport() kui.Size { return scene.viewport }

func (scene *Scene) handle(i int32) kui.Handle { return kui.MakeHandle(int(i), scene.mem.gen[i]) }

// resolve returns the node index of h.
func (scene *Scene) resolve(h kui.Handle) (int32, error) {
	i := h.Index()
	if h.IsZero() || i == 0 || i >= scene.mem.maxNodes() {
		return -1, kui.ErrInvalidHandle
	} else if scene.mem.gen[i] != h.Gen() || scene.mem.flags[i]&flagAllocated == 0 {
		return -1, kui.ErrStaleHandle
	}
	return int32(i), nil
}

// Valid reports whether h refers to a live node.
func (scene *Scene) Valid(h kui.Handle) bool {
	_, err := scene.resolve(h)
	return err == nil
}

func (scene *Scene) invalidate() {
	scene.needLayout = true
	scene.needPaint = true
}

// alloc takes a node from the free list. It returns -1 if there are none.
func (scene *Scene) alloc(kind Kind, style *kui.LayoutStyle, vis *Visual) int32 {
	m := scene.mem
	if len(m.free) == 0 {
		return -1
	}
	i := m.free[len(m.free)-1]
	m.free = m.free[:len(m.free)-1]
	m.links[i] = links{parent: -1, first: -1, last: -1, next: -1, prev: -1}
	m.kind[i] = kind
	m.flags[i] = flagAllocated
	m.style[i] = *style
	m.visual[i] = *vis
	m.text[i] = span{}
	m.state[i] = 0
	m.user[i] = 0
	m.box[i] = kui.Rect{}
	m.clip[i] = kui.Rect{}
	scene.invalidate()
	return i
}

// AddNode adds a node of the given kind as the last child of parent. Style and visual are copied.
func (scene *Scene) AddNode(parent kui.Handle, kind Kind, style *kui.LayoutStyle, vis *Visual) (kui.Handle, error) {
	p, err := scene.resolve(parent)
	if err != nil {
		return 0, err
	} else if int(kind) >= scene.nkinds {
		return 0, kui.ErrInvalidConfig
	}
	i := scene.alloc(kind, style, vis)
	if i < 0 {
		return 0, kui.ErrCapacity
	}
	m := scene.mem
	l := &m.links[i]
	l.parent = p
	pl := &m.links[p]
	if pl.last >= 0 {
		m.links[pl.last].next = i
		l.prev = pl.last
	} else {
		pl.first = i
	}
	pl.last = i
	return scene.handle(i), nil
}

// RemoveNode removes node h and its descendants, invalidating their handles.
// The root node cannot be removed.
func (scene *Scene) RemoveNode(h kui.Handle) error {
	i, err := scene.resolve(h)
	if err != nil {
		return err
	} else if i == scene.root {
		return kui.ErrInvalidHandle
	}
	m := scene.mem
	// Unlink from parent.
	l := m.links[i]
	if l.prev >= 0 {
		m.links[l.prev].next = l.next
	} else {
		m.links[l.parent].first = l.next
	}
	if l.next >= 0 {
		m.links[l.next].prev = l.prev
	} else {
		m.links[l.parent].last = l.prev
	}
	// Free the subtree.
	stack := m.stack[:0]
	stack = append(stack, i)
	for len(stack) > 0 {
		j := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for c := m.links[j].first; c >= 0; c = m.links[c].next {
			stack = append(stack, c)
		}
		if scene.focus == scene.handle(j) {
			scene.focus = 0
		}
		m.flags[j] = 0
		m.gen[j]++
		if m.gen[j] == 0 {
			m.gen[j] = 1
		}
		m.free = append(m.free, j)
	}
	scene.invalidate()
	return nil
}

// Parent returns the parent of h, or zero for the root node.
func (scene *Scene) Parent(h kui.Handle) kui.Handle {
	if i, err := scene.resolve(h); err == nil && scene.mem.links[i].parent >= 0 {
		return scene.handle(scene.mem.links[i].parent)
	}
	return 0
}

// FirstChild returns the first child of h, or zero if it has none.
func (scene *Scene) FirstChild(h kui.Handle) kui.Handle {
	if i, err := scene.resolve(h); err == nil && scene.mem.links[i].first >= 0 {
		return scene.handle(scene.mem.links[i].first)
	}
	return 0
}

// NextSibling returns the sibling after h, or zero if it is the last child.
func (scene *Scene) NextSibling(h kui.Handle) kui.Handle {
	if i, err := scene.resolve(h); err == nil && scene.mem.links[i].next >= 0 {
		return scene.handle(scene.mem.links[i].next)
	}
	return 0
}

// Kind returns the kind of node h.
func (scene *Scene) Kind(h kui.Handle) Kind {
	if i, err := scene.resolve(h); err == nil {
		return scene.mem.kind[i]
	}
	return 0
}

// Flags returns the flags of node h.
func (scene *Scene) Flags(h kui.Handle) Flags {
	if i, err := scene.resolve(h); err == nil {
		return scene.mem.flags[i] &^ flagAllocated
	}
	return 0
}

// SetFlags sets the flags of node h.
func (scene *Scene) SetFlags(h kui.Handle, f Flags) error {
	i, err := scene.resolve(h)
	if err != nil {
		return err
	}
	f = f&^flagAllocated | flagAllocated
	if f != scene.mem.flags[i] {
		scene.mem.flags[i] = f
		if scene.focus == h && f&(FlagHidden|FlagDisabled|FlagFocusable) != FlagFocusable {
			scene.changeFocus(0)
		}
		scene.invalidate()
	}
	return nil
}

// Style returns the layout style of node h. Modify it with [Scene.SetStyle].
func (scene *Scene) Style(h kui.Handle) kui.LayoutStyle {
	if i, err := scene.resolve(h); err == nil {
		return scene.mem.style[i]
	}
	return kui.LayoutStyle{}
}

// SetStyle sets the layout style of node h.
func (scene *Scene) SetStyle(h kui.Handle, s *kui.LayoutStyle) error {
	i, err := scene.resolve(h)
	if err != nil {
		return err
	}
	scene.mem.style[i] = *s
	scene.invalidate()
	return nil
}

// Visual returns the visual style of node h.
func (scene *Scene) Visual(h kui.Handle) *Visual {
	if i, err := scene.resolve(h); err == nil {
		return &scene.mem.visual[i]
	}
	return &Visual{}
}

// SetVisual sets the visual style of node h.
func (scene *Scene) SetVisual(h kui.Handle, v *Visual) error {
	i, err := scene.resolve(h)
	if err != nil {
		return err
	}
	if v.Text != scene.mem.visual[i].Text {
		scene.needLayout = true // Text metrics may change.
	}
	scene.mem.visual[i] = *v
	scene.needPaint = true
	return nil
}

// State returns the widget state of node h. Its meaning is defined by the node's kind.
func (scene *Scene) State(h kui.Handle) uint32 {
	if i, err := scene.resolve(h); err == nil {
		return scene.mem.state[i]
	}
	return 0
}

// SetState sets the widget state of node h and schedules a repaint.
func (scene *Scene) SetState(h kui.Handle, state uint32) error {
	i, err := scene.resolve(h)
	if err != nil {
		return err
	}
	if scene.mem.state[i] != state {
		scene.mem.state[i] = state
		scene.needPaint = true
	}
	return nil
}

// UserData returns the user value of node h.
func (scene *Scene) UserData(h kui.Handle) uint32 {
	if i, err := scene.resolve(h); err == nil {
		return scene.mem.user[i]
	}
	return 0
}

// SetUser sets a user defined value on node h.
func (scene *Scene) SetUser(h kui.Handle, v uint32) error {
	i, err := scene.resolve(h)
	if err != nil {
		return err
	}
	scene.mem.user[i] = v
	return nil
}

// Bounds returns the bounding box of node h computed by the last layout.
func (scene *Scene) Bounds(h kui.Handle) kui.Rect {
	if i, err := scene.resolve(h); err == nil {
		return scene.mem.box[i]
	}
	return kui.Rect{}
}

// Text returns the text of node h. It is valid until the node's text is modified.
func (scene *Scene) Text(h kui.Handle) string {
	if i, err := scene.resolve(h); err == nil {
		return scene.textOf(i)
	}
	return ""
}

func (scene *Scene) textOf(i int32) string {
	sp := scene.mem.text[i]
	if sp.len == 0 {
		return ""
	}
	return unsafe.String(&scene.mem.arena[sp.off], int(sp.len))
}

// SetText sets the text of node h, copying s into the text arena. See [Scene.EditText].
func (scene *Scene) SetText(h kui.Handle, s string) error {
	i, err := scene.resolve(h)
	if err != nil {
		return err
	} else if scene.textOf(i) == s {
		return nil
	}
	return scene.editText(i, 0, int(scene.mem.text[i].len), s)
}

// EditText replaces bytes [start, end) of node h's text with s. Edits are made in place while
// the text fits its capacity ([Scene.TextCap]); otherwise the text moves to the end of the
// arena with a capacity of its new length, compacting the arena if it is full. If it still
// does not fit [kui.ErrShortBuffer] is returned and [Scene.TextBytesNeeded] reports the arena
// size required, leaving the text unchanged. s may be the text of another node, but not of h.
func (scene *Scene) EditText(h kui.Handle, start, end int, s string) error {
	i, err := scene.resolve(h)
	if err != nil {
		return err
	} else if start < 0 || end < start || end > int(scene.mem.text[i].len) {
		return kui.ErrOutOfRange
	}
	return scene.editText(i, start, end, s)
}

func (scene *Scene) editText(i int32, start, end int, s string) error {
	m := scene.mem
	sp := &m.text[i]
	n := int(sp.len) - (end - start) + len(s)
	if n > int(sp.cap) {
		// Moving may compact the arena: if s is another node's text, follow it.
		src, rel := scene.textNode(s)
		if err := scene.moveText(i, n); err != nil {
			return err
		}
		if src >= 0 {
			s = unsafe.String(&m.arena[m.text[src].off+rel], len(s))
		}
	}
	t := m.arena[sp.off : sp.off+sp.cap]
	copy(t[start+len(s):], t[end:sp.len]) // Shift the tail, possibly overlapping.
	copy(t[start:], s)
	sp.len = uint32(n)
	scene.invalidate()
	return nil
}

// ReserveText makes the capacity of node h's text at least n bytes, so that edits within it
// are made in place and never fail. Capacity is kept when the arena is compacted.
func (scene *Scene) ReserveText(h kui.Handle, n int) error {
	i, err := scene.resolve(h)
	if err != nil {
		return err
	} else if n <= int(scene.mem.text[i].cap) {
		return nil
	}
	return scene.moveText(i, n)
}

// TextCap returns the capacity of node h's text: the length it can grow to without moving.
func (scene *Scene) TextCap(h kui.Handle) int {
	if i, err := scene.resolve(h); err == nil {
		return int(scene.mem.text[i].cap)
	}
	return 0
}

// textNode returns the node whose text s is part of and the offset of s within it, or -1.
func (scene *Scene) textNode(s string) (node int32, rel uint32) {
	m := scene.mem
	if len(s) == 0 || cap(m.arena) == 0 {
		return -1, 0
	}
	p := uintptr(unsafe.Pointer(unsafe.StringData(s)))
	base := uintptr(unsafe.Pointer(unsafe.SliceData(m.arena)))
	if p < base || p >= base+uintptr(cap(m.arena)) {
		return -1, 0
	}
	off := uint32(p - base)
	for j := range m.text {
		if sp := m.text[j]; m.flags[j]&flagAllocated != 0 && off >= sp.off && off < sp.off+sp.cap {
			return int32(j), off - sp.off
		}
	}
	return -1, 0
}

// moveText moves node i's text to the end of the arena with capacity c >= its length.
func (scene *Scene) moveText(i int32, c int) error {
	m := scene.mem
	sp := &m.text[i]
	if c > cap(m.arena)-len(m.arena) {
		scene.compactArena()
		if c > cap(m.arena)-len(m.arena) {
			scene.bytesNeeded = len(m.arena) + c
			return kui.ErrShortBuffer
		}
	}
	off := len(m.arena)
	m.arena = m.arena[:off+c]
	copy(m.arena[off:], m.arena[sp.off:sp.off+sp.len])
	sp.off, sp.cap = uint32(off), uint32(c)
	return nil
}

// TextBytesNeeded returns the arena size that would have been required by the last text edit
// that returned [kui.ErrShortBuffer].
func (scene *Scene) TextBytesNeeded() int { return scene.bytesNeeded }

// compactArena moves live text spans to the start of the arena, in place, keeping their capacity.
func (scene *Scene) compactArena() {
	m := scene.mem
	cursor := uint32(0)
	for {
		// Find the live span with the lowest offset at or after cursor. Quadratic but
		// allocation free; compaction only happens when the arena fills up.
		best := int32(-1)
		for i := range m.text {
			sp := &m.text[i]
			if sp.cap == 0 || m.flags[i]&flagAllocated == 0 || sp.off < cursor {
				continue
			}
			if best < 0 || sp.off < m.text[best].off {
				best = int32(i)
			}
		}
		if best < 0 {
			break
		}
		sp := &m.text[best]
		copy(m.arena[cursor:cursor+sp.len], m.arena[sp.off:sp.off+sp.len])
		sp.off = cursor
		cursor += sp.cap
	}
	m.arena = m.arena[:cursor]
}

// Focus returns the focused node, or zero.
func (scene *Scene) Focus() kui.Handle { return scene.focus }

// SetFocus focuses node h, which must be focusable. A zero h clears focus.
// FocusOut and FocusIn events are emitted on the next Step.
func (scene *Scene) SetFocus(h kui.Handle) error {
	if h.IsZero() {
		scene.changeFocus(0)
		return nil
	}
	i, err := scene.resolve(h)
	if err != nil {
		return err
	} else if scene.mem.flags[i]&(FlagFocusable|FlagDisabled|FlagHidden) != FlagFocusable {
		return kui.ErrInvalidHandle
	}
	scene.changeFocus(h)
	return nil
}

// Builder returns the scene's builder. See [Builder].
func (scene *Scene) Builder() *Builder { return &scene.builder }

// NeedsPaint reports whether the UI changed since the last Paint.
func (scene *Scene) NeedsPaint() bool { return scene.needPaint || scene.needLayout }

// Repaint makes [Scene.NeedsPaint] report true until the next Paint. Kinds call it when
// state they keep outside the scene changes how they paint, i.e. a caret moving.
func (scene *Scene) Repaint() { scene.needPaint = true }

// DroppedEvents returns the number of widget events dropped because the event buffer was full.
func (scene *Scene) DroppedEvents() int { return scene.dropped }

// LayoutErr returns the error of the last layout, i.e. [kui.ErrCapacity] if the layout engine
// was configured with too few elements or [kui.ErrShortBuffer] if [MemoryConfig.MaxLines] lines
// could not hold the visible text.
func (scene *Scene) LayoutErr() error { return scene.layoutErr }
