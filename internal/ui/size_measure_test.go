package ui

import (
	"testing"
	"unsafe"
)

// The App model travels by value through every method call, and Go heap-
// allocates implicit copies of 64KiB or more (the compiler's
// maxImplicitStackVarSize) - at 255KiB, every non-inlined method call
// allocated a quarter megabyte of garbage, tens of megabytes per rendered
// frame (issue #274). The fat singletons (Theme, KeyMap) are held by pointer
// and the text inputs and the help model are boxed (issue #279), which put
// App at ~3.2KiB; this pins the size under the cliff so a future field
// cannot quietly push the struct back over it.
func TestAppStaysCheapToCopy(t *testing.T) {
	const limit = 64 * 1024
	if size := unsafe.Sizeof(App{}); size >= limit {
		t.Fatalf("App is %d bytes by value, at or over the %d-byte implicit-heap threshold: "+
			"hold big new fields by pointer, or every method call pays a heap allocation "+
			"for the copy", size, limit)
	}
}
