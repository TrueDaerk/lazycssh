package term

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
)

// Output-path benchmarks for the performance audit (issue #274): what one
// chatty host costs to ingest, and what reading a full pane back costs.

// floodChunk is a realistic read chunk: complete lines, CRLF terminated, the
// shape a remote `yes`/`seq` produces through a PTY.
func floodChunk(lines int) []byte {
	var b strings.Builder
	for i := 0; i < lines; i++ {
		fmt.Fprintf(&b, "load line %d: the quick brown fox jumps over the lazy dog\r\n", i)
	}
	return []byte(b.String())
}

// BenchmarkEmulatorWrite is ingest throughput: how fast session output can be
// parsed into the emulator. b.SetBytes makes the result read as MB/s.
func BenchmarkEmulatorWrite(b *testing.B) {
	e := New(120, 40)
	defer e.Close()
	chunk := floodChunk(100)
	b.SetBytes(int64(len(chunk)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = e.Write(chunk)
	}
}

// BenchmarkEmulatorText reads the whole pane content — retained history plus
// screen — as plain text, with the history at the 10k-line retention cap.
// This is what the output filter and the diff panel called per evaluation.
func BenchmarkEmulatorText(b *testing.B) {
	e := New(120, 40)
	defer e.Close()
	_, _ = e.Write(floodChunk(11000))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = e.Text()
	}
}

// TestRetainedMemoryAtCap pins the retention win of issue #277: one emulator
// at the 10k-line cap held ~75 MB when every scrolled-off line stayed a cell
// grid; the compact store must keep it an order of magnitude below that. The
// measurement mirrors the #274 audit: MemStats around New plus a flood.
func TestRetainedMemoryAtCap(t *testing.T) {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	e := New(120, 40)
	defer e.Close()
	_, _ = e.Write(floodChunk(11000))

	runtime.GC()
	runtime.ReadMemStats(&after)
	retained := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	t.Logf("retained at cap: %.1f MB", float64(retained)/(1<<20))
	if retained > 20<<20 {
		t.Errorf("retained %d bytes at the cap, want well under 20 MB (was ~75 MB before #277)", retained)
	}
}

// BenchmarkEmulatorHistoryWalk renders every retained history line once, the
// way a pane render walks the scrollback, with the history at the cap.
func BenchmarkEmulatorHistoryWalk(b *testing.B) {
	e := New(120, 40)
	defer e.Close()
	_, _ = e.Write(floodChunk(11000))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j, n := 0, e.HistoryLen(); j < n; j++ {
			_ = e.HistoryLine(j)
		}
	}
}
