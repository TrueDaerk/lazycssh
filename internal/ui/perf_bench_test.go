package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The render-path benchmarks pin the cost of one frame against a fleet whose
// scrollbacks are full — the load the performance audit (issue #274) measured.
// bubbletea skips unchanged frames, so the case that matters is a *changing*
// pane: every new output chunk forces a full View, and View's cost per frame
// is what a chatty fleet multiplies.

// benchFleet builds n connected fakes, each with lines of scrollback already
// written, and an app resized to a realistic terminal.
func benchFleet(b *testing.B, n, lines int) (App, *fakeFleet, []string) {
	b.Helper()

	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("host-%02d", i+1)
	}
	fleet := newFakeFleet(names...)

	var flood strings.Builder
	for i := 0; i < lines; i++ {
		fmt.Fprintf(&flood, "load line %d: the quick brown fox jumps over the lazy dog\r\n", i)
	}
	for _, name := range names {
		s := fleet.sessions[name]
		if err := s.Start(context.Background()); err != nil {
			b.Fatalf("start %s: %v", name, err)
		}
		s.Emit(flood.String())
	}

	a := NewApp(Config{Hosts: names, Fleet: fleet, Theme: Options{Dark: true}})
	model, _ := a.Update(tea.WindowSizeMsg{Width: 220, Height: 60})
	a, ok := model.(App)
	if !ok {
		b.Fatalf("Update returned a %T, want App", model)
	}
	return a, fleet, names
}

// BenchmarkAppViewChattyFleet is one full frame: 12 panes on a 220x60
// terminal, every scrollback at the 10k-line retention cap.
func BenchmarkAppViewChattyFleet(b *testing.B) {
	a, _, _ := benchFleet(b, 12, 10000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = a.View()
	}
}

// BenchmarkAppViewQuietFleet is the same frame over near-empty scrollbacks,
// so the difference to the chatty benchmark is the cost of retained history
// alone.
func BenchmarkAppViewQuietFleet(b *testing.B) {
	a, _, _ := benchFleet(b, 12, 10)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = a.View()
	}
}

// BenchmarkOutputMsgUnderFilter is one output redraw hint while an output
// filter is active: the filter re-reads every host's recent output on every
// hint (issue #255), which under load happens once per event batch.
func BenchmarkOutputMsgUnderFilter(b *testing.B) {
	a, _, _ := benchFleet(b, 12, 10000)
	next, _ := a.applyOutputFilter("fox")
	a = next
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		model, _ := a.Update(SessionOutputMsg{ID: "host-01"})
		a = model.(App)
	}
}

// BenchmarkPaneScrollAtCap is one scroll keystroke in a pane whose history is
// at the retention cap: the scroll clamp walks the virtual line space to know
// its length.
func BenchmarkPaneScrollAtCap(b *testing.B) {
	a, _, names := benchFleet(b, 12, 10000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = a.maxScroll(names[0])
	}
}
