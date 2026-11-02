/*
Package kui holds low-level types common to all UI implementations including:
  - geometry
  - node handles
  - font interfaces
  - event logic: keys, pointer, resize, lifecycle etc.

kui notably missing important logic which is implementation dependent such as layout engine, text
processing implementations, event queue logic(SPSC or multi-threaded) and rendering.
*/
package kui

type errGeneric uint8

// Errors returned by kui packages. They do not allocate.
const (
	_                errGeneric = iota
	ErrShortBuffer              // short buffer
	ErrBufferFull               // buffer full
	ErrInvalidConfig            // invalid configuration
	ErrStaleHandle              // stale handle
	ErrInvalidHandle            // invalid handle
	ErrCapacity                 // capacity exceeded
	ErrUnbalanced               // unbalanced open/close
	ErrOutOfRange               // out of range
)

func (e errGeneric) Error() string { return e.String() }

// Handle refers to a node in UI tree. Lower bits hold node index/id and higher bits hold generation to detect stale handles.
// Zero handle refers to no node, which is to say generation is always non-zero.
type Handle uint32 // TODO: maybe provide 64 bit handles on large non-mcu systems.

const (
	handleIndexBits = 24
	handleIndexMask = 1<<handleIndexBits - 1
	// MaxHandleIndex is the largest node index a Handle can address.
	MaxHandleIndex = handleIndexMask
)

// MakeHandle creates a new [Handle] ready for use. gen must be non-zero.
func MakeHandle(index int, gen uint8) Handle {
	if gen == 0 {
		panic("invalid generation")
	}
	return Handle(uint32(gen)<<handleIndexBits | uint32(index)&handleIndexMask)
}

// Index returns the node index of h.
func (h Handle) Index() int { return int(h & handleIndexMask) }

// Gen returns the generation of h.
func (h Handle) Gen() uint8 { return uint8(h >> handleIndexBits) }

// IsZero reports whether h refers to no node.
func (h Handle) IsZero() bool { return h == 0 }

// RGB returns an opaque color.
func RGB(r, g, b uint8) Color { return Color{R: r, G: g, B: b, A: 255} }

// RGBA returns a color with the given alpha.
func RGBA(r, g, b, a uint8) Color { return Color{R: r, G: g, B: b, A: a} }
