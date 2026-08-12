package term

import "testing"

// The change mark backs the UI's cross-frame pane cache (issue #291): equal
// marks promise equal renders, so every mutation path must move it, and an
// idle emulator must not.

func TestChangeMarkMovesOnEveryMutation(t *testing.T) {
	e := New(80, 24)
	defer e.Close()

	mark := e.Change()
	if again := e.Change(); again != mark {
		t.Fatal("the mark moved without a mutation")
	}

	steps := []struct {
		name   string
		mutate func()
	}{
		{"a write", func() { _, _ = e.Write([]byte("hello\r\n")) }},
		{"a resize", func() { e.Resize(100, 30) }},
		{"a retention change", func() { e.SetHistorySize(500) }},
	}
	for _, step := range steps {
		step.mutate()
		next := e.Change()
		if next == mark {
			t.Fatalf("the mark did not move on %s", step.name)
		}
		mark = next
	}

	// A resize to the same size changes nothing a render reads.
	e.Resize(100, 30)
	if e.Change() != mark {
		t.Fatal("the mark moved on a same-size resize")
	}
}
