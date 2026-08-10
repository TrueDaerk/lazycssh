package ui

import (
	"testing"
	"unsafe"
)

// The App model travels by value through every method call, and Go heap-
// allocates implicit copies larger than 64KiB (the compiler's
// maxImplicitStackVarSize) - at 255KiB, every non-inlined method call
// allocated a quarter megabyte of garbage, tens of megabytes per rendered
// frame (issue #274). The fat singletons (Theme, KeyMap) are held by pointer
// now; this pins the size so a future field cannot quietly push the struct
// back over the cliff.
func TestAppStaysCheapToCopy(t *testing.T) {
	const limit = 80 * 1024
	if size := unsafe.Sizeof(App{}); size > limit {
		t.Fatalf("App is %d bytes by value, over the %d budget: hold big new fields by pointer, "+
			"or every method call pays for the copy", size, limit)
	}
}
