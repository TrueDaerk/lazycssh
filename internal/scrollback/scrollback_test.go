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

// The line discipline: a cursor on the pending logical line, with overwrite
// semantics — exactly what a remote readline needs to redraw a recalled
// command, single- or multi-row (issue #178). What it does not interpret
// passes through for the render-time sanitizer.
func TestLineDiscipline(t *testing.T) {
	tests := []struct {
		name   string
		writes []string
		want   []string
	}{
		{
			name:   "backspace moves the cursor left and the next rune overwrites",
			writes: []string{"abc\b\bX\n"},
			want:   []string{"aXc"},
		},
		{
			name:   "backspace on an empty line does nothing",
			writes: []string{"\bhi\n"},
			want:   []string{"hi"},
		},
		{
			name:   "backspace never reaches a committed line",
			writes: []string{"a\n\b\bb\n"},
			want:   []string{"a", "b"},
		},
		{
			name:   "overwriting a multi-byte rune replaces it whole",
			writes: []string{"ä\bx\n"},
			want:   []string{"x"},
		},
		{
			name:   "erase right of the cursor at the end is a no-op",
			writes: []string{"hi\x1b[K!\n"},
			want:   []string{"hi!"},
		},
		{
			name:   "erase right with an explicit zero parameter",
			writes: []string{"hi\x1b[0K!\n"},
			want:   []string{"hi!"},
		},
		{
			name:   "erase right truncates at the cursor",
			writes: []string{"$ ls -la", "\b\b\b\b\b\b\x1b[K\n"},
			want:   []string{"$ "},
		},
		{
			name:   "erase the whole line blanks it but keeps the cursor column",
			writes: []string{"secret\x1b[2Knew\n"},
			want:   []string{"      new"},
		},
		{
			name:   "erase left of the cursor blanks what was there",
			writes: []string{"old\x1b[1Knew\n"},
			want:   []string{"   new"},
		},
		{
			name:   "a readline history recall replaces the line",
			writes: []string{"$ ls -la", "\b\b\b\b\b\b\x1b[Kecho hi\n"},
			want:   []string{"$ echo hi"},
		},
		{
			name:   "an escape sequence split across writes",
			writes: []string{"gone\x1b", "[2K\rkept\n"},
			want:   []string{"kept"},
		},
		{
			name:   "an escape sequence split inside its parameters",
			writes: []string{"gone\x1b[2", "K\rkept\n"},
			want:   []string{"kept"},
		},
		{
			name:   "sgr split across writes is preserved verbatim",
			writes: []string{"\x1b[3", "1mred\n"},
			want:   []string{"\x1b[31mred"},
		},
		{
			name:   "cursor movement without a known width is dropped, not text",
			writes: []string{"a\x1b[Ab\n"},
			want:   []string{"ab"},
		},
		{
			name:   "cursor forward pads and cursor back overwrites",
			writes: []string{"ab\x1b[3Cx\x1b[4Dy\n"},
			want:   []string{"aby  x"},
		},
		{
			name:   "cursor to column overwrites in place",
			writes: []string{"hello\x1b[1GJ\n"},
			want:   []string{"Jello"},
		},
		{
			name:   "a multi-parameter movement sequence is not half-understood",
			writes: []string{"a\x1b[1;2Cb\n"},
			want:   []string{"a\x1b[1;2Cb"},
		},
		{
			name:   "non-csi escapes pass through verbatim",
			writes: []string{"\x1bMx\n"},
			want:   []string{"\x1bMx"},
		},
		{
			name:   "an osc sequence is consumed, its payload never becomes text",
			writes: []string{"\x1b]133;D;0\aok\n"},
			want:   []string{"ok"},
		},
		{
			name:   "an osc sequence terminated by ST is consumed too",
			writes: []string{"\x1b]0;title\x1b\\ok\n"},
			want:   []string{"ok"},
		},
		{
			name:   "an osc sequence split across writes stays invisible",
			writes: []string{"\x1b]133;D", ";0\aok\n"},
			want:   []string{"ok"},
		},
		{
			name:   "a line break aborts an unfinished sequence",
			writes: []string{"a\x1b[\nb\n"},
			want:   []string{"a\x1b[", "b"},
		},
		{
			name:   "a carriage return aborts an unfinished sequence and redraws",
			writes: []string{"a\x1b[\rb\n"},
			want:   []string{"b\x1b["},
		},
		{
			name:   "erase interacts with the bare carriage return reset",
			writes: []string{"10%\r\x1b[K100%\n"},
			want:   []string{"100%"},
		},
		{
			name:   "a progress bar leaves only its last frame",
			writes: []string{"10%\r20%\r100%\n"},
			want:   []string{"100%"},
		},
		{
			name:   "a shorter redraw leaves the stale tail a terminal would show",
			writes: []string{"100%\r5%\n"},
			want:   []string{"5%0%"},
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

// The issue-178 regression: recalling a history entry that wraps over several
// screen rows, then stepping past it, must not leave the redraw's intermediate
// states in the scrollback. The byte streams are what bash 3.2 actually emits
// on a 51-column PTY, captured for the issue's repro (arrow-up to the recalled
// multi-row entry, arrow-down to a shorter one, enter).
func TestMultiRowReadlineRedraw(t *testing.T) {
	const width = 51
	const hook = ` PROMPT_COMMAND='printf "\033]133;D;%d\007"`             // wraps after this
	const hookRest = `" "$?"'; precmd() { printf "\033]133;D;%d\007" "$?"` // wraps again
	const hookEnd = `; }`

	b := New(100)
	b.SetWidth(width)

	// The prompt, a recalled `ls -lh`, then arrow-up to the multi-row entry:
	// readline backspaces over the old text and echoes the long entry, with a
	// bare "\r" at each row boundary after the forced wrap.
	b.Write([]byte("ssh06:~$ ls -lh"))
	b.Write([]byte("\b\b\b\b\b\b" + hook + "\r" + hookRest + "\r" + hookEnd))

	// Arrow-down: readline repositions to the first row, types the shorter
	// entry over it, erases the rest of that row, walks down clearing the two
	// rows below, and parks the cursor back after the new text.
	b.Write([]byte("\x1b[A\x1b[A\x1b[C\x1b[C\x1b[C\x1b[C\x1b[C\x1b[Cls -lh\x1b[K"))
	b.Write([]byte("\r\n\r\x1b[K\r\n\r\x1b[K"))
	b.Write([]byte("\x1b[A\x1b[A" + strings.Repeat("\x1b[C", 15)))

	// The pending line is the redrawn command and nothing else.
	assertLines(t, b.Lines(), []string{"ssh06:~$ ls -lh"})

	// Enter: the command commits once, and its output follows.
	b.Write([]byte("\r\ntotal 8\r\n"))
	assertLines(t, b.Lines(), []string{"ssh06:~$ ls -lh", "total 8"})
}

// The same dance without the bare "\r" at the wrap boundaries — the variant
// where the terminal's auto-wrap does the wrapping. The whole recalled entry
// is then one logical pending line spanning three screen rows.
func TestMultiRowRedrawWithAutoWrap(t *testing.T) {
	const width = 20
	b := New(100)
	b.SetWidth(width)

	// A 50-cell recalled entry on a 20-column terminal: rows are
	// "$ 123456789012345678", "90123456789012345678", "9012345678".
	b.Write([]byte("$ " + "123456789012345678901234567890123456789012345678"))

	// Arrow-down to "ls": cursor to row 0 column 3, overwrite, erase right,
	// then clear the two rows below and come back.
	b.Write([]byte("\x1b[A\x1b[A\x1b[3Gls\x1b[K"))
	b.Write([]byte("\r\n\r\x1b[K\r\n\r\x1b[K"))
	b.Write([]byte("\x1b[A\x1b[A\x1b[4C"))

	assertLines(t, b.Lines(), []string{"$ ls"})

	b.Write([]byte("\r\nfile\r\n"))
	assertLines(t, b.Lines(), []string{"$ ls", "file"})
}

// An escape sequence longer than the interpreter's bound is handed to the line
// verbatim instead of being buffered forever.
func TestOverlongEscapeSequenceIsFlushed(t *testing.T) {
	b := New(10)
	seq := "\x1b[" + strings.Repeat("1;", 40)
	if _, err := b.Write([]byte(seq)); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	lines := b.Lines()
	if len(lines) != 1 || !strings.Contains(lines[0], "1;1;") {
		t.Fatalf("the overlong sequence was not flushed into the line: %q", lines)
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

// The clear-screen family: erase-display and alternate-screen switches plant
// a [ClearMark] where the visible area restarted, and preserve the history
// around it - the issue-131 behavior.
func TestClearScreenPlantsAMarker(t *testing.T) {
	tests := []struct {
		name   string
		writes []string
		want   []string
	}{
		{
			name:   "erase whole screen drops the pending line and marks",
			writes: []string{"old\ntyp\x1b[2J$ \n"},
			want:   []string{"old", ClearMark, "$ "},
		},
		{
			name:   "clear's usual sequence: home then erase",
			writes: []string{"old\n\x1b[H\x1b[2J$ \n"},
			want:   []string{"old", ClearMark, "$ "},
		},
		{
			name:   "erase scrollback too is treated as a clear, not a wipe",
			writes: []string{"old\n\x1b[3J\x1b[H\x1b[2J$ \n"},
			want:   []string{"old", ClearMark, "$ "},
		},
		{
			name:   "erase above keeps the line the cursor is on",
			writes: []string{"old\nkept\x1b[1J!\n"},
			want:   []string{"old", ClearMark, "kept!"},
		},
		{
			name:   "erase below is a silent no-op at the tail",
			writes: []string{"hi\x1b[J!\n\x1b[0J"},
			want:   []string{"hi!"},
		},
		{
			name:   "entering the alternate screen clears",
			writes: []string{"old\n\x1b[?1049hscreen frame\n"},
			want:   []string{"old", ClearMark, "screen frame"},
		},
		{
			name:   "leaving the alternate screen clears too",
			writes: []string{"vim ui\n\x1b[?1049l$ \n"},
			want:   []string{"vim ui", ClearMark, "$ "},
		},
		{
			name:   "the legacy alternate-screen switches clear as well",
			writes: []string{"a\n\x1b[?47hb\n\x1b[?1047lc\n"},
			want:   []string{"a", ClearMark, "b", ClearMark, "c"},
		},
		{
			name:   "consecutive clears collapse into one marker",
			writes: []string{"old\n\x1b[2J\x1b[2J\x1b[?1049h$ \n"},
			want:   []string{"old", ClearMark, "$ "},
		},
		{
			name:   "other private-mode switches pass through verbatim",
			writes: []string{"a\x1b[?25lb\n"},
			want:   []string{"a\x1b[?25lb"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := New(100)
			for _, w := range tt.writes {
				if _, err := b.Write([]byte(w)); err != nil {
					t.Fatalf("Write(%q) returned error: %v", w, err)
				}
			}
			assertLines(t, b.Lines(), tt.want)
		})
	}
}
