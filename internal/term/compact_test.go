package term

import (
	"fmt"
	"testing"
)

// ringWants asserts the ring holds exactly want, oldest first.
func ringWants(t *testing.T, r *stringRing, want ...string) {
	t.Helper()
	if r.len() != len(want) {
		t.Fatalf("ring len = %d, want %d", r.len(), len(want))
	}
	for i, w := range want {
		if got := r.at(i); got != w {
			t.Fatalf("ring at(%d) = %q, want %q", i, got, w)
		}
	}
}

// The ring evicts oldest-first through growth, wrap-around, mid-life
// evictions and cap changes — the sequences the drain and trim paths drive.
func TestStringRingWrapEvictRebound(t *testing.T) {
	var r stringRing
	r.setMax(3)

	for i := 0; i < 5; i++ {
		r.push(fmt.Sprint(i))
	}
	ringWants(t, &r, "2", "3", "4")

	r.evictOldest()
	ringWants(t, &r, "3", "4")

	// Push after an eviction must land behind the survivors, not over them.
	r.push("5")
	r.push("6")
	ringWants(t, &r, "4", "5", "6")

	r.setMax(2)
	ringWants(t, &r, "5", "6")

	r.setMax(4)
	r.push("7")
	r.push("8")
	ringWants(t, &r, "5", "6", "7", "8")
	r.push("9")
	ringWants(t, &r, "6", "7", "8", "9")
}

// A zero cap drops everything and evicting an empty ring is a no-op.
func TestStringRingZeroAndEmpty(t *testing.T) {
	var r stringRing
	r.evictOldest()
	r.push("dropped")
	if r.len() != 0 {
		t.Fatalf("ring with max 0 retained %d lines", r.len())
	}
	r.setMax(-1)
	if r.max != 0 {
		t.Fatalf("negative max = %d, want clamped to 0", r.max)
	}
}
