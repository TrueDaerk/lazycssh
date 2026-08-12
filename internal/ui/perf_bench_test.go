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

// BenchmarkAppViewLiveSearch is the chatty-fleet frame with a live search
// term: the status bar counts the focused pane's matches every frame. The
// acceptance criterion of issue #278 is that this costs the same order as
// BenchmarkAppViewChattyFleet, not the full-scrollback walk it used to.
// The term is sparse — a dozen hits in the 10k lines — like a real hunt for
// an error; a term matching every visible line measures the highlight
// restyle, which is window-bounded and predates the cache.
func BenchmarkAppViewLiveSearch(b *testing.B) {
	a, _, names := benchFleet(b, 12, 10000)
	a.searchTerm = "line 999"
	_ = a.matchLines(names[0]) // the first walk, paid once at the term keystroke
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = a.View()
	}
}

// BenchmarkAppViewLiveSearchChattyFocus adds output arriving on the focused
// pane between frames — every line shifts the whole match space at the cap,
// which is the case the incremental cache exists for.
func BenchmarkAppViewLiveSearchChattyFocus(b *testing.B) {
	a, fleet, names := benchFleet(b, 12, 10000)
	a.searchTerm = "line 999"
	_ = a.matchLines(names[0])
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fleet.sessions[names[0]].Emit("one more quick brown fox\r\n")
		_ = a.View()
	}
}

// BenchmarkAppViewFullScreenChatty is full-screen mode's worst case: one pane
// fills the whole main area and new output lands on it between frames, so
// every frame re-renders the largest possible pane content (issue #293).
func BenchmarkAppViewFullScreenChatty(b *testing.B) {
	a, fleet, names := benchFleet(b, 12, 10000)
	a.screen = ScreenFull
	a.focus = AreaGrid
	a = a.syncLayout()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fleet.sessions[names[0]].Emit("one more quick brown fox\r\n")
		_ = a.View()
	}
}

// BenchmarkAppViewFullScreenHiddenOutput is the frame issue #293 exists for:
// full-screen mode, and the output that forced the redraw landed on a pane
// that is not even on screen. The visible pane did not change, so with the
// cross-frame cache this frame must not pay for re-rendering it.
func BenchmarkAppViewFullScreenHiddenOutput(b *testing.B) {
	a, fleet, names := benchFleet(b, 12, 10000)
	a.screen = ScreenFull
	a.focus = AreaGrid
	a = a.syncLayout()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fleet.sessions[names[1]].Emit("hidden host output\r\n")
		_ = a.View()
	}
}

// BenchmarkAppViewLargeFleetOneChatty is the fleet-scale redraw: 100 hosts,
// output arriving on exactly one of them between frames. The frame's honest
// cost is that one pane; every other pane's content is unchanged and, with
// the cross-frame cache, unpaid for (issue #293).
func BenchmarkAppViewLargeFleetOneChatty(b *testing.B) {
	a, fleet, names := benchFleet(b, 100, 2000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fleet.sessions[names[0]].Emit("one more quick brown fox\r\n")
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
