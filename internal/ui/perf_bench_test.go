package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
	"github.com/TrueDaerk/lazycssh/internal/workingset"
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

// benchKeystroke measures one keystroke into a focused pane at fleet scale n,
// to the frame containing the echo.
func benchKeystroke(b *testing.B, n int) {
	a, fleet, names := benchFleet(b, n, 1000)
	for _, name := range names {
		fleet.sessions[name].EchoInput = true
	}
	a.focus = AreaGrid
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		model, _ := a.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
		a = model.(App)
		model, _ = a.Update(SessionOutputMsg{ID: names[0]})
		a = model.(App)
		_ = a.View()
	}
}

// BenchmarkKeystrokeFleet12 is the focused-pane keystroke at the size the
// older render benchmarks use, so the fleet-scale cost is readable as a ratio.
func BenchmarkKeystrokeFleet12(b *testing.B) { benchKeystroke(b, 12) }

// BenchmarkKeystrokeBroadcast100 is one broadcast-bar keystroke fanned out to
// 100 connected fake sessions, then every echo's redraw hint, then the frame:
// the worst-case typing path at fleet scale (issue #291).
func BenchmarkKeystrokeBroadcast100(b *testing.B) {
	a, fleet, names := benchFleet(b, 100, 1000)
	for _, name := range names {
		fleet.sessions[name].EchoInput = true
	}
	ws := workingset.New(names)
	router, err := broadcast.NewRouter(ws)
	if err != nil {
		b.Fatalf("router: %v", err)
	}
	router.Attach(fleet)
	a.cfg.Sender = router
	a.cfg.Targets = router
	a.focus = AreaBroadcast
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		model, _ := a.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
		a = model.(App)
		for _, name := range names {
			model, _ = a.Update(SessionOutputMsg{ID: name})
			a = model.(App)
		}
		_ = a.View()
	}
}

// BenchmarkKeystrokeFleet100 is one keystroke into a focused pane with 100
// connected fake sessions, measured to the frame containing the echo: the key
// press Update, the echo's redraw hint, and the View that shows it — the
// keystroke-to-screen path issue #291 is about. The scrollbacks carry moderate
// history so the frame is a realistic one, not an empty grid.
func BenchmarkKeystrokeFleet100(b *testing.B) {
	a, fleet, names := benchFleet(b, 100, 1000)
	for _, name := range names {
		fleet.sessions[name].EchoInput = true
	}
	a.focus = AreaGrid
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		model, _ := a.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
		a = model.(App)
		model, _ = a.Update(SessionOutputMsg{ID: names[0]})
		a = model.(App)
		_ = a.View()
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
