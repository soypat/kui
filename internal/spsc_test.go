package internal

import (
	"sync"
	"testing"
)

func TestSPSC(t *testing.T) {
	var r SPSC[uint16]
	if r.Reset(nil) || r.Reset(make([]uint16, 3)) || r.Push(1) || r.Write([]uint16{1}) != 0 {
		t.Fatal("unconfigured ring accepted a buffer or elements")
	}
	if !r.Reset(make([]uint16, 4)) {
		t.Fatal("Reset failed")
	}
	// Staged elements are invisible until published and may be modified until then.
	r.Push(1)
	r.Push(2)
	if r.Snapshot() != 0 {
		t.Fatal("snapshot holds unpublished elements")
	}
	*r.Last() = 20
	r.Publish()
	if r.Last() != nil {
		t.Fatal("Last returned a published element")
	}
	if r.Snapshot() != 2 || *r.At(0) != 1 || *r.At(1) != 20 {
		t.Fatalf("snapshot: len %d", r.Len())
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("At past the snapshot did not panic")
			}
		}()
		r.At(2)
	}()
	// Read slots are not reused until released.
	var dst [4]uint16
	if r.Read(dst[:1]) != 1 || dst[0] != 1 || r.Len() != 1 || *r.At(0) != 20 {
		t.Fatalf("read: %v len %d", dst[:1], r.Len())
	}
	if n := r.Write([]uint16{3, 4, 5}); n != 2 || r.Free() != 0 {
		t.Fatalf("wrote %d elements to a ring with 2 free slots", n)
	}
	r.Release()
	if r.Free() != 1 || !r.Push(6) || r.Push(7) {
		t.Fatal("released slot not reused")
	}
	r.Publish()
	// The snapshot is not refreshed until the next one; Drain takes one, wrapping around.
	if r.Len() != 1 {
		t.Fatalf("snapshot changed: len %d", r.Len())
	}
	if n := r.Drain(dst[:]); n != 4 || dst != [4]uint16{20, 3, 4, 6} {
		t.Fatalf("drained %v", dst[:n])
	}
	if r.Free() != 4 || r.Drain(dst[:]) != 0 {
		t.Fatal("drained ring not empty")
	}
}

func TestSPSCConcurrent(t *testing.T) {
	var r SPSC[uint16]
	r.Reset(make([]uint16, 8))
	const total = 1 << 15
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		var batch [3]uint16
		for i := 0; i < total; {
			if i%4 == 0 { // Stage one at a time, publishing every other.
				if r.Push(uint16(i)) {
					i++
				}
				if i%8 == 0 {
					r.Publish()
				}
				continue
			}
			n := min(len(batch), total-i)
			for j := range n {
				batch[j] = uint16(i + j)
			}
			i += r.Write(batch[:n])
			r.Publish()
		}
		r.Publish()
	}()
	var dst [5]uint16
	for next := 0; next < total; {
		if r.Snapshot() > 0 && *r.At(0) != uint16(next) {
			t.Fatalf("want %d, got %d", next, *r.At(0))
		}
		n := r.Read(dst[:])
		for _, v := range dst[:n] {
			if v != uint16(next) {
				t.Fatalf("want %d, got %d", next, v)
			}
			next++
		}
		r.Release()
	}
	wg.Wait()
}

func TestSPSCAllocs(t *testing.T) {
	var r SPSC[uint16]
	r.Reset(make([]uint16, 16))
	src := []uint16{1, 2, 3}
	var dst [8]uint16
	allocs := testing.AllocsPerRun(100, func() {
		r.Push(0)
		*r.Last() = 9
		r.Write(src)
		r.Publish()
		r.Drain(dst[:])
	})
	if allocs != 0 {
		t.Fatal("allocations:", allocs)
	}
}
