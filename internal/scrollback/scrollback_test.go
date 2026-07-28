package scrollback

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestWriteSplitsLines(t *testing.T) {
	tests := []struct {
		name   string
		writes []string
		want   []string
	}{
		{
			name:   "no output at all",
			writes: nil,
			want:   nil,
		},
		{
			name:   "one terminated line",
			writes: []string{"hello\n"},
			want:   []string{"hello"},
		},
		{
			name:   "several lines in one write",
			writes: []string{"a\nb\nc\n"},
			want:   []string{"a", "b", "c"},
		},
		{
			name:   "an unterminated line is visible immediately",
			writes: []string{"user@host:~$ "},
			want:   []string{"user@host:~$ "},
		},
		{
			name:   "a line split across writes is assembled",
			writes: []string{"hel", "lo", " world\n"},
			want:   []string{"hello world"},
		},
		{
			name:   "crlf is treated as one line ending",
			writes: []string{"a\r\nb\r\n"},
			want:   []string{"a", "b"},
		},
		{
			name:   "crlf split across writes",
			writes: []string{"a\r", "\nb\r\n"},
			want:   []string{"a", "b"},
		},
		{
			name:   "a bare carriage return redraws the line",
			writes: []string{"10%\r20%\r100%\n"},
			want:   []string{"100%"},
		},
		{
			name:   "a bare carriage return split across writes",
			writes: []string{"10%\r", "99%\n"},
			want:   []string{"99%"},
		},
		{
			name:   "an empty line is kept",
			writes: []string{"a\n\nb\n"},
			want:   []string{"a", "", "b"},
		},
		{
			name:   "trailing newline does not create an empty last line",
			writes: []string{"a\n"},
			want:   []string{"a"},
		},
		{
			name:   "ansi escapes are preserved verbatim",
			writes: []string{"\x1b[31mred\x1b[0m\n"},
			want:   []string{"\x1b[31mred\x1b[0m"},
		},
		{
			name:   "tabs and spaces are preserved",
			writes: []string{"a\t b  \n"},
			want:   []string{"a\t b  "},
		},
		{
			name:   "utf8 survives being split mid rune across writes",
			writes: []string{"\xc3", "\xa4\n"},
			want:   []string{"ä"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := New(100)
			for _, w := range tt.writes {
				n, err := b.Write([]byte(w))
				if err != nil {
					t.Fatalf("Write(%q) returned error: %v", w, err)
				}
				if n != len(w) {
					t.Errorf("Write(%q) = %d, want %d", w, n, len(w))
				}
			}
			assertLines(t, b.Lines(), tt.want)
		})
	}
}

func TestEviction(t *testing.T) {
	const capacity = 5
	b := New(capacity)

	for i := 1; i <= 12; i++ {
		fmt.Fprintf(b, "line %d\n", i)
	}

	assertLines(t, b.Lines(), []string{"line 8", "line 9", "line 10", "line 11", "line 12"})

	if got := b.Len(); got != capacity {
		t.Errorf("Len() = %d, want %d", got, capacity)
	}
	if got, want := b.Dropped(), 7; got != want {
		t.Errorf("Dropped() = %d, want %d", got, want)
	}
}

func TestEvictionKeepsOrderAcrossManyWraps(t *testing.T) {
	const capacity = 3
	b := New(capacity)

	for i := 0; i < 1000; i++ {
		fmt.Fprintf(b, "%d\n", i)
	}

	assertLines(t, b.Lines(), []string{"997", "998", "999"})
}

func TestPendingLineIsNotCounted(t *testing.T) {
	b := New(10)
	b.Write([]byte("done\nin progress"))

	if got, want := b.Len(), 1; got != want {
		t.Errorf("Len() = %d, want %d: the pending line is not a complete line", got, want)
	}
	assertLines(t, b.Lines(), []string{"done", "in progress"})
}

func TestOverlongLineIsCommitted(t *testing.T) {
	b := New(10)

	// A remote process dumping a binary with no newline in sight.
	b.Write([]byte(strings.Repeat("x", maxLineLength+100)))

	lines := b.Lines()
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2 (one forced commit plus the remainder)", len(lines))
	}
	if len(lines[0]) != maxLineLength {
		t.Errorf("first line is %d bytes, want the cap of %d", len(lines[0]), maxLineLength)
	}
	if len(lines[1]) != 100 {
		t.Errorf("remainder is %d bytes, want 100", len(lines[1]))
	}
}

func TestNewClampsCapacity(t *testing.T) {
	for _, capacity := range []int{0, -1} {
		if got := New(capacity).Capacity(); got != DefaultCapacity {
			t.Errorf("New(%d).Capacity() = %d, want %d: a zero config field must not discard all output",
				capacity, got, DefaultCapacity)
		}
	}
	if got := New(7).Capacity(); got != 7 {
		t.Errorf("New(7).Capacity() = %d, want 7", got)
	}
}

func TestWrittenCountsEverything(t *testing.T) {
	b := New(2)
	for i := 0; i < 10; i++ {
		b.Write([]byte("abc\n"))
	}
	if got, want := b.Written(), 40; got != want {
		t.Errorf("Written() = %d, want %d: dropped output still counts as written", got, want)
	}
}

func TestReset(t *testing.T) {
	b := New(2)
	b.Write([]byte("a\nb\nc\npartial"))

	b.Reset()

	if got := b.Lines(); len(got) != 0 {
		t.Errorf("Lines() after Reset = %q, want nothing", got)
	}
	if got := b.Dropped(); got != 0 {
		t.Errorf("Dropped() after Reset = %d, want 0", got)
	}
	if got := b.Written(); got != 0 {
		t.Errorf("Written() after Reset = %d, want 0", got)
	}
	if got := b.Capacity(); got != 2 {
		t.Errorf("Capacity() after Reset = %d, want it unchanged at 2", got)
	}
}

func TestString(t *testing.T) {
	b := New(10)
	b.Write([]byte("a\nb\nprompt$ "))

	if got, want := b.String(), "a\nb\nprompt$ "; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// TestConcurrentWriteAndRead is the backpressure guarantee under -race: a
// session goroutine writing flat out while the UI takes snapshots must never
// race and must never block.
func TestConcurrentWriteAndRead(t *testing.T) {
	b := New(64)

	var wg sync.WaitGroup
	const writers = 4
	const perWriter = 2000

	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				fmt.Fprintf(b, "writer %d line %d\n", w, i)
			}
		}(w)
	}

	stop := make(chan struct{})
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = b.Lines()
			_ = b.Dropped()
			runtime.Gosched()
		}
	}()

	wg.Wait()
	close(stop)
	<-readerDone

	if got, want := b.Len(), 64; got != want {
		t.Errorf("Len() = %d, want the capacity %d", got, want)
	}
	if b.Dropped() == 0 {
		t.Error("Dropped() = 0, want the overflow to have been counted")
	}
}

// TestMemoryIsBounded is the substantive backpressure claim: a host emitting
// continuously must not grow the process without bound.
func TestMemoryIsBounded(t *testing.T) {
	b := New(1000)

	line := []byte(strings.Repeat("x", 200) + "\n")
	for i := 0; i < 200_000; i++ {
		b.Write(line)
	}

	if got := b.Len(); got != 1000 {
		t.Fatalf("Len() = %d, want 1000", got)
	}

	var stats runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&stats)

	// 1000 lines of 200 bytes is 200 KiB of payload. Allow generous overhead
	// for the runtime itself; the point is that 40 MB of input did not stay.
	const limit = 16 << 20
	if stats.HeapAlloc > limit {
		t.Errorf("heap is %d bytes after writing 40 MB through a 1000 line buffer, want below %d",
			stats.HeapAlloc, limit)
	}
}

func BenchmarkWrite(b *testing.B) {
	buf := New(DefaultCapacity)
	line := []byte("2026-07-28 12:00:00 some fairly typical log line from a remote host\n")

	b.SetBytes(int64(len(line)))
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		buf.Write(line)
	}
}

func BenchmarkLines(b *testing.B) {
	buf := New(DefaultCapacity)
	for i := 0; i < DefaultCapacity; i++ {
		fmt.Fprintf(buf, "line %d\n", i)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = buf.Lines()
	}
}

func assertLines(t *testing.T, got, want []string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d\ngot:  %q\nwant: %q", len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}
