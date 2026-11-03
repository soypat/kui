package internal

import "sync/atomic"

// SPSC is a lock free single producer single consumer FIFO over a user supplied buffer whose
// length is a power of two. One goroutine or interrupt handler may produce and one may
// consume, which may be the same one.
//
// Both ends work in batches, so moving any number of elements costs a couple of atomic
// operations, which disable interrupts on microcontrollers without atomic instructions:
//   - The producer stages elements with Push and Write and makes them visible to the consumer
//     with Publish. Staged elements may still be modified, see Last.
//   - The consumer takes the published elements with Snapshot, accesses them with At and Read,
//     and hands their slots back to the producer with Release.
type SPSC[T any] struct {
	buf  []T
	mask uint32        // len(buf)-1.
	head atomic.Uint32 // Elements released by the consumer.
	tail atomic.Uint32 // Elements published by the producer.

	// Producer state.
	wtail uint32 // Elements written, staged or published.
	wpub  uint32 // Copy of tail.
	whead uint32 // Last loaded head: at least len(buf)-(wtail-whead) slots are free.

	// Consumer state.
	rhead uint32 // Elements read.
	rrel  uint32 // Copy of head.
	rend  uint32 // tail at the last Snapshot.
}

// Reset sets the buffer and empties the ring. It returns false, leaving the ring unchanged, if
// len(buf) is not a power of two of at most 1<<30. Reset must not be called concurrently with
// other methods.
func (r *SPSC[T]) Reset(buf []T) bool {
	if n := len(buf); n == 0 || n&(n-1) != 0 || n > 1<<30 {
		return false
	}
	*r = SPSC[T]{buf: buf, mask: uint32(len(buf) - 1)}
	return true
}

// Cap returns the capacity of the ring.
func (r *SPSC[T]) Cap() int { return len(r.buf) }

// Free returns the number of elements that can be staged. Producer only.
func (r *SPSC[T]) Free() int {
	r.whead = r.head.Load()
	return len(r.buf) - int(r.wtail-r.whead)
}

// Push stages v. It returns false if the ring is full. Producer only.
func (r *SPSC[T]) Push(v T) bool {
	if r.wtail-r.whead == uint32(len(r.buf)) && r.Free() == 0 {
		return false
	}
	r.buf[r.wtail&r.mask] = v
	r.wtail++
	return true
}

// Write stages the longest prefix of src that fits and returns its length. Producer only.
func (r *SPSC[T]) Write(src []T) int {
	n := len(r.buf) - int(r.wtail-r.whead)
	if n < len(src) {
		n = r.Free()
	}
	n = min(n, len(src))
	i := int(r.wtail & r.mask)
	c := copy(r.buf[i:], src[:n])
	copy(r.buf, src[c:n])
	r.wtail += uint32(n)
	return n
}

// Last returns the last staged element, or nil if every element written is published. The
// consumer cannot see it yet, so it may be modified, i.e. to merge another element into it.
// Producer only.
func (r *SPSC[T]) Last() *T {
	if r.wtail == r.wpub {
		return nil
	}
	return &r.buf[(r.wtail-1)&r.mask]
}

// Publish makes the staged elements visible to the consumer, along with every memory write
// the producer made before calling it. Producer only.
func (r *SPSC[T]) Publish() {
	if r.wpub != r.wtail {
		r.wpub = r.wtail
		r.tail.Store(r.wpub)
	}
}

// Snapshot takes the elements published so far for At and Read, along with every memory write
// the producer made before publishing them, and returns how many are unread. Consumer only.
func (r *SPSC[T]) Snapshot() int {
	r.rend = r.tail.Load()
	return r.Len()
}

// Len returns the number of unread elements of the last snapshot. Consumer only.
func (r *SPSC[T]) Len() int { return int(r.rend - r.rhead) }

// At returns unread element i of the last snapshot, 0 being the oldest. It panics if i is not
// less than Len. Consumer only.
func (r *SPSC[T]) At(i int) *T {
	if uint(i) >= uint(r.Len()) {
		panic("ring: index out of range")
	}
	return &r.buf[(r.rhead+uint32(i))&r.mask]
}

// Read moves up to len(dst) unread elements of the last snapshot into dst and returns how many
// it moved. Their slots are not reused until Release. Consumer only.
func (r *SPSC[T]) Read(dst []T) int {
	n := min(r.Len(), len(dst))
	i := int(r.rhead & r.mask)
	c := copy(dst[:n], r.buf[i:])
	copy(dst[c:n], r.buf)
	r.rhead += uint32(n)
	return n
}

// Release hands the slots of the elements read back to the producer. Consumer only.
func (r *SPSC[T]) Release() {
	if r.rrel != r.rhead {
		r.rrel = r.rhead
		r.head.Store(r.rrel)
	}
}

// Drain moves up to len(dst) of the oldest published elements into dst and releases them,
// returning how many it moved. Consumer only.
func (r *SPSC[T]) Drain(dst []T) int {
	r.Snapshot()
	n := r.Read(dst)
	r.Release()
	return n
}
