package term

import "testing"

// Seq is the render cache's validity signal (issue #293): it must move on
// every mutation a render could observe, and must not move on reads — a
// reader polling it between frames must not invalidate itself.
func TestSeqMovesOnWritesAndGeometryOnly(t *testing.T) {
	e := New(80, 24)
	defer e.Close()

	s0 := e.Seq()

	if _, err := e.Write([]byte("hello\r\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	s1 := e.Seq()
	if s1 <= s0 {
		t.Fatalf("a write did not move Seq: %d -> %d", s0, s1)
	}

	// Reads leave it alone.
	_ = e.Render()
	_ = e.HistoryLen()
	_ = e.HistoryLine(0)
	_ = e.Text()
	if got := e.Seq(); got != s1 {
		t.Fatalf("a read moved Seq: %d -> %d", s1, got)
	}

	e.Resize(100, 30)
	s2 := e.Seq()
	if s2 <= s1 {
		t.Fatalf("a resize did not move Seq: %d -> %d", s1, s2)
	}

	// A no-op resize is no mutation.
	e.Resize(100, 30)
	if got := e.Seq(); got != s2 {
		t.Fatalf("a same-size resize moved Seq: %d -> %d", s2, got)
	}

	e.SetHistorySize(500)
	if got := e.Seq(); got <= s2 {
		t.Fatalf("a retention change did not move Seq: %d -> %d", s2, got)
	}
}
