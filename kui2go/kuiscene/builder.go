package kuiscene

import (
	"github.com/soypat/kui"
)

// Builder creates nodes. Instead of returning errors from each call it records the first
// error, returned by [Builder.Err], and returns zero handles once an error occurred so that
// trees can be declared without error checks. With SetPanic(true) errors panic instead.
type Builder struct {
	k          *Scene
	err        error
	panicOnErr bool
}

// Scene returns the scene the builder adds nodes to.
func (b *Builder) Scene() *Scene { return b.k }

// SetPanic makes the builder panic on errors instead of recording them.
func (b *Builder) SetPanic(panicOnErr bool) { b.panicOnErr = panicOnErr }

// Err returns the first error that occurred since the last ClearErr.
func (b *Builder) Err() error { return b.err }

// ClearErr clears the recorded error.
func (b *Builder) ClearErr() { b.err = nil }

// Fail records err as if a builder method had failed. Nil is ignored.
func (b *Builder) Fail(err error) {
	if err == nil {
		return
	} else if b.panicOnErr {
		panic(err)
	} else if b.err == nil {
		b.err = err
	}
}

// Node adds a node of the given kind as the last child of parent. Style and visual are copied.
func (b *Builder) Node(parent kui.Handle, kind Kind, style *kui.LayoutStyle, vis *Visual) kui.Handle {
	if b.err != nil {
		return 0
	}
	h, err := b.k.AddNode(parent, kind, style, vis)
	b.Fail(err)
	return h
}

// Box adds a container node.
func (b *Builder) Box(parent kui.Handle, style *kui.LayoutStyle, vis *Visual) kui.Handle {
	return b.Node(parent, KindBox, style, vis)
}

// Text adds a container node holding text.
func (b *Builder) Text(parent kui.Handle, style *kui.LayoutStyle, vis *Visual, text string) kui.Handle {
	h := b.Node(parent, KindBox, style, vis)
	if h != 0 {
		b.Fail(b.k.SetText(h, text))
	}
	return h
}

// SetFlags sets the flags of n, recording any error.
func (b *Builder) SetFlags(n kui.Handle, f Flags) {
	if n != 0 {
		b.Fail(b.k.SetFlags(n, f))
	}
}
