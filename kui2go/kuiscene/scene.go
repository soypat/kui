// Package kuiscene implements kui's retained node tree, layout replay, hit testing, input routing,
// focus and painting. Pointer gestures are recognized by package [gesture] and routed by hit
// testing. It does not allocate after [Scene.Configure]: all memory comes from a [Memory]
// sized up front.
//
// The kernel is push driven so any host loop can own control (desktop, mobile, web or a
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

// MaxKinds is the maximum number of kinds a kernel can register, including [KindBox].
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
	// FlagScroll makes the kernel scroll the node in response to scroll events by adjusting its
	// [kui.LayoutStyle.Scroll], clamped to its content. The node should clip (ClipX or ClipY).
	FlagScroll
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
	Text kui.Style
}

// KindSpec defines the behavior of a node kind.
type KindSpec struct {
	// Sense is the pointer input hit testing routes to nodes of this kind. Any of SensePress,
	// SenseDrag, SenseDoubleTap and SenseLongPress makes presses hit the node. Keys and text go
	// to the focused node regardless, see [FlagFocusable].
	Sense gesture.Sense
	// Reduce, if not nil, updates the state of node n in response to e. It is called for every
	// event targeting a node of this kind in the order events were produced, including events
	// emitted by other Reduce calls. It may emit further events with [Scene.Emit].
	Reduce func(k *Scene, n kui.Handle, e *WidgetEvent)
	// Paint, if not nil, draws node n before its children. Nil uses [Scene.PaintBox].
	// Nodes that clip (ClipX or ClipY) are clipped to their box while painting.
	Paint func(k *Scene, n kui.Handle, l *kui.DrawList)
	// PaintAfter, if not nil, draws over node n's children, outside of the node's own clip,
	// i.e. scroll bars and focus rings. The node's border is drawn by the kernel afterwards.
	PaintAfter func(k *Scene, n kui.Handle, l *kui.DrawList)
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

// Memory holds all memory used by a kernel. Create it with [NewMemory] or [Memory.Init].
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
	events []WidgetEvent
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

// NewMemory allocates memory for a kernel.
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
	m          *Memory
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
	stepEnd   int
	reqEnd    int
	modal     int32 // Active modal node, -1 for none.
	arenaNeed int
	layoutErr error
}

// Configure prepares the kernel. It creates the root node and registers [KindBox].
func (k *Scene) Configure(cfg Config) error {
	if cfg.Memory == nil || cfg.Engine == nil || cfg.Memory.maxNodes() < 2 || cfg.Memory.maxNodes()-1 > kui.MaxHandleIndex {
		return kui.ErrInvalidConfig
	}
	*k = Scene{
		m:          cfg.Memory,
		engine:     cfg.Engine,
		viewport:   cfg.Viewport,
		needLayout: true,
		needPaint:  true,
		modal:      -1,
	}
	k.gest.Configure(cfg.Gestures)
	m := k.m
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
	k.nkinds = 1
	k.kinds[KindBox] = KindSpec{}
	k.builder.k = k
	style := cfg.RootStyle
	if style == (kui.LayoutStyle{}) {
		style.Width, style.Height = kui.Grow(0, 0), kui.Grow(0, 0)
	}
	k.root = k.alloc(KindBox, &style, &cfg.RootVisual)
	return nil
}

// RegisterKind registers a node kind and returns its identifier.
func (k *Scene) RegisterKind(spec KindSpec) (Kind, error) {
	if k.nkinds == MaxKinds {
		return 0, kui.ErrCapacity
	}
	k.kinds[k.nkinds] = spec
	k.nkinds++
	return Kind(k.nkinds - 1), nil
}

// Root returns the root node, which spans the viewport.
func (k *Scene) Root() kui.Handle { return k.handle(k.root) }

// SetViewport sets the size of the viewport in logical pixels.
func (k *Scene) SetViewport(sz kui.Size) {
	if sz != k.viewport {
		k.viewport = sz
		k.invalidate()
	}
}

// Viewport returns the size of the viewport.
func (k *Scene) Viewport() kui.Size { return k.viewport }

func (k *Scene) handle(i int32) kui.Handle { return kui.MakeHandle(int(i), k.m.gen[i]) }

// resolve returns the node index of h.
func (k *Scene) resolve(h kui.Handle) (int32, error) {
	i := h.Index()
	if h.IsZero() || i == 0 || i >= k.m.maxNodes() {
		return -1, kui.ErrInvalidHandle
	} else if k.m.gen[i] != h.Gen() || k.m.flags[i]&flagAllocated == 0 {
		return -1, kui.ErrStaleHandle
	}
	return int32(i), nil
}

// Valid reports whether h refers to a live node.
func (k *Scene) Valid(h kui.Handle) bool {
	_, err := k.resolve(h)
	return err == nil
}

func (k *Scene) invalidate() {
	k.needLayout = true
	k.needPaint = true
}

// alloc takes a node from the free list. It returns -1 if there are none.
func (k *Scene) alloc(kind Kind, style *kui.LayoutStyle, vis *Visual) int32 {
	m := k.m
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
	k.invalidate()
	return i
}

// AddNode adds a node of the given kind as the last child of parent. Style and visual are copied.
func (k *Scene) AddNode(parent kui.Handle, kind Kind, style *kui.LayoutStyle, vis *Visual) (kui.Handle, error) {
	p, err := k.resolve(parent)
	if err != nil {
		return 0, err
	} else if int(kind) >= k.nkinds {
		return 0, kui.ErrInvalidConfig
	}
	i := k.alloc(kind, style, vis)
	if i < 0 {
		return 0, kui.ErrCapacity
	}
	m := k.m
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
	return k.handle(i), nil
}

// Remove removes node h and its descendants, invalidating their handles.
// The root node cannot be removed.
func (k *Scene) Remove(h kui.Handle) error {
	i, err := k.resolve(h)
	if err != nil {
		return err
	} else if i == k.root {
		return kui.ErrInvalidHandle
	}
	m := k.m
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
		if k.focus == k.handle(j) {
			k.focus = 0
		}
		m.flags[j] = 0
		m.gen[j]++
		if m.gen[j] == 0 {
			m.gen[j] = 1
		}
		m.free = append(m.free, j)
	}
	k.invalidate()
	return nil
}

// Parent returns the parent of h, or zero for the root node.
func (k *Scene) Parent(h kui.Handle) kui.Handle {
	if i, err := k.resolve(h); err == nil && k.m.links[i].parent >= 0 {
		return k.handle(k.m.links[i].parent)
	}
	return 0
}

// FirstChild returns the first child of h, or zero if it has none.
func (k *Scene) FirstChild(h kui.Handle) kui.Handle {
	if i, err := k.resolve(h); err == nil && k.m.links[i].first >= 0 {
		return k.handle(k.m.links[i].first)
	}
	return 0
}

// NextSibling returns the sibling after h, or zero if it is the last child.
func (k *Scene) NextSibling(h kui.Handle) kui.Handle {
	if i, err := k.resolve(h); err == nil && k.m.links[i].next >= 0 {
		return k.handle(k.m.links[i].next)
	}
	return 0
}

// Kind returns the kind of node h.
func (k *Scene) Kind(h kui.Handle) Kind {
	if i, err := k.resolve(h); err == nil {
		return k.m.kind[i]
	}
	return 0
}

// Flags returns the flags of node h.
func (k *Scene) Flags(h kui.Handle) Flags {
	if i, err := k.resolve(h); err == nil {
		return k.m.flags[i] &^ flagAllocated
	}
	return 0
}

// SetFlags sets the flags of node h.
func (k *Scene) SetFlags(h kui.Handle, f Flags) error {
	i, err := k.resolve(h)
	if err != nil {
		return err
	}
	f = f&^flagAllocated | flagAllocated
	if f != k.m.flags[i] {
		k.m.flags[i] = f
		if k.focus == h && f&(FlagHidden|FlagDisabled|FlagFocusable) != FlagFocusable {
			k.changeFocus(0)
		}
		k.invalidate()
	}
	return nil
}

// Style returns the layout style of node h. Modify it with [Scene.SetStyle].
func (k *Scene) Style(h kui.Handle) kui.LayoutStyle {
	if i, err := k.resolve(h); err == nil {
		return k.m.style[i]
	}
	return kui.LayoutStyle{}
}

// SetStyle sets the layout style of node h.
func (k *Scene) SetStyle(h kui.Handle, s *kui.LayoutStyle) error {
	i, err := k.resolve(h)
	if err != nil {
		return err
	}
	k.m.style[i] = *s
	k.invalidate()
	return nil
}

// Visual returns the visual style of node h.
func (k *Scene) Visual(h kui.Handle) *Visual {
	if i, err := k.resolve(h); err == nil {
		return &k.m.visual[i]
	}
	return &Visual{}
}

// SetVisual sets the visual style of node h.
func (k *Scene) SetVisual(h kui.Handle, v *Visual) error {
	i, err := k.resolve(h)
	if err != nil {
		return err
	}
	if v.Text != k.m.visual[i].Text {
		k.needLayout = true // Text metrics may change.
	}
	k.m.visual[i] = *v
	k.needPaint = true
	return nil
}

// State returns the widget state of node h. Its meaning is defined by the node's kind.
func (k *Scene) State(h kui.Handle) uint32 {
	if i, err := k.resolve(h); err == nil {
		return k.m.state[i]
	}
	return 0
}

// SetState sets the widget state of node h and schedules a repaint.
func (k *Scene) SetState(h kui.Handle, state uint32) error {
	i, err := k.resolve(h)
	if err != nil {
		return err
	}
	if k.m.state[i] != state {
		k.m.state[i] = state
		k.needPaint = true
	}
	return nil
}

// User returns the user value of node h.
func (k *Scene) User(h kui.Handle) uint32 {
	if i, err := k.resolve(h); err == nil {
		return k.m.user[i]
	}
	return 0
}

// SetUser sets a user defined value on node h.
func (k *Scene) SetUser(h kui.Handle, v uint32) error {
	i, err := k.resolve(h)
	if err != nil {
		return err
	}
	k.m.user[i] = v
	return nil
}

// Box returns the bounding box of node h computed by the last layout.
func (k *Scene) Box(h kui.Handle) kui.Rect {
	if i, err := k.resolve(h); err == nil {
		return k.m.box[i]
	}
	return kui.Rect{}
}

// Text returns the text of node h. It is valid until the node's text is modified.
func (k *Scene) Text(h kui.Handle) string {
	if i, err := k.resolve(h); err == nil {
		return k.textOf(i)
	}
	return ""
}

func (k *Scene) textOf(i int32) string {
	sp := k.m.text[i]
	if sp.len == 0 {
		return ""
	}
	return unsafe.String(&k.m.arena[sp.off], int(sp.len))
}

// SetText sets the text of node h, copying s into the text arena. See [Scene.EditText].
func (k *Scene) SetText(h kui.Handle, s string) error {
	i, err := k.resolve(h)
	if err != nil {
		return err
	} else if k.textOf(i) == s {
		return nil
	}
	return k.editText(i, 0, int(k.m.text[i].len), s)
}

// EditText replaces bytes [start, end) of node h's text with s. Edits are made in place while
// the text fits its capacity ([Scene.TextCap]); otherwise the text moves to the end of the
// arena with a capacity of its new length, compacting the arena if it is full. If it still
// does not fit [kui.ErrShortBuffer] is returned and [Scene.TextArenaNeed] reports the arena
// size required, leaving the text unchanged. s may be the text of another node, but not of h.
func (k *Scene) EditText(h kui.Handle, start, end int, s string) error {
	i, err := k.resolve(h)
	if err != nil {
		return err
	} else if start < 0 || end < start || end > int(k.m.text[i].len) {
		return kui.ErrOutOfRange
	}
	return k.editText(i, start, end, s)
}

func (k *Scene) editText(i int32, start, end int, s string) error {
	m := k.m
	sp := &m.text[i]
	n := int(sp.len) - (end - start) + len(s)
	if n > int(sp.cap) {
		// Moving may compact the arena: if s is another node's text, follow it.
		src, rel := k.textNode(s)
		if err := k.moveText(i, n); err != nil {
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
	k.invalidate()
	return nil
}

// ReserveText makes the capacity of node h's text at least n bytes, so that edits within it
// are made in place and never fail. Capacity is kept when the arena is compacted.
func (k *Scene) ReserveText(h kui.Handle, n int) error {
	i, err := k.resolve(h)
	if err != nil {
		return err
	} else if n <= int(k.m.text[i].cap) {
		return nil
	}
	return k.moveText(i, n)
}

// TextCap returns the capacity of node h's text: the length it can grow to without moving.
func (k *Scene) TextCap(h kui.Handle) int {
	if i, err := k.resolve(h); err == nil {
		return int(k.m.text[i].cap)
	}
	return 0
}

// textNode returns the node whose text s is part of and the offset of s within it, or -1.
func (k *Scene) textNode(s string) (node int32, rel uint32) {
	m := k.m
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
func (k *Scene) moveText(i int32, c int) error {
	m := k.m
	sp := &m.text[i]
	if c > cap(m.arena)-len(m.arena) {
		k.compactArena()
		if c > cap(m.arena)-len(m.arena) {
			k.arenaNeed = len(m.arena) + c
			return kui.ErrShortBuffer
		}
	}
	off := len(m.arena)
	m.arena = m.arena[:off+c]
	copy(m.arena[off:], m.arena[sp.off:sp.off+sp.len])
	sp.off, sp.cap = uint32(off), uint32(c)
	return nil
}

// TextArenaNeed returns the arena size that would have been required by the last text edit
// that returned [kui.ErrShortBuffer].
func (k *Scene) TextArenaNeed() int { return k.arenaNeed }

// compactArena moves live text spans to the start of the arena, in place, keeping their capacity.
func (k *Scene) compactArena() {
	m := k.m
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
func (k *Scene) Focus() kui.Handle { return k.focus }

// SetFocus focuses node h, which must be focusable. A zero h clears focus.
// FocusOut and FocusIn events are emitted on the next Step.
func (k *Scene) SetFocus(h kui.Handle) error {
	if h.IsZero() {
		k.changeFocus(0)
		return nil
	}
	i, err := k.resolve(h)
	if err != nil {
		return err
	} else if k.m.flags[i]&(FlagFocusable|FlagDisabled|FlagHidden) != FlagFocusable {
		return kui.ErrInvalidHandle
	}
	k.changeFocus(h)
	return nil
}

// Builder returns the kernel's builder. See [Builder].
func (k *Scene) Builder() *Builder { return &k.builder }

// NeedsPaint reports whether the UI changed since the last Paint.
func (k *Scene) NeedsPaint() bool { return k.needPaint || k.needLayout }

// Repaint makes [Scene.NeedsPaint] report true until the next Paint. Kinds call it when
// state they keep outside the kernel changes how they paint, i.e. a caret moving.
func (k *Scene) Repaint() { k.needPaint = true }

// DroppedEvents returns the number of widget events dropped because the event buffer was full.
func (k *Scene) DroppedEvents() int { return k.dropped }

// LayoutErr returns the error of the last layout, i.e. [kui.ErrCapacity] if the layout engine
// was configured with too few elements or [kui.ErrShortBuffer] if [MemoryConfig.MaxLines] lines
// could not hold the visible text.
func (k *Scene) LayoutErr() error { return k.layoutErr }
