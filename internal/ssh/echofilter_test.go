package ssh

import (
	"bytes"
	"testing"
)

// setupEcho is what a PTY echoes back for the setup line: the typed text with
// the carriage return translated to a line break.
var setupEcho = ExitSetupCommand[:len(ExitSetupCommand)-1] + "\r\n"

// filterAll feeds the chunks through one filter and returns everything it let
// through, including the final flush.
func filterAll(chunks ...string) string {
	f := newEchoFilter()
	var out bytes.Buffer
	for _, c := range chunks {
		out.Write(f.Filter([]byte(c)))
	}
	out.Write(f.Flush())
	return out.String()
}

func TestEchoFilterRemovesTheKernelEcho(t *testing.T) {
	got := filterAll("Last login: today\r\n" + setupEcho + "user@host:~$ ")
	want := "Last login: today\r\nuser@host:~$ "
	if got != want {
		t.Fatalf("Filter left %q, want %q", got, want)
	}
}

func TestEchoFilterRemovesBothCopies(t *testing.T) {
	// Kernel echo first, then the line editor redisplaying the pending input
	// after the prompt.
	in := "motd\r\n" + setupEcho + "user@host:~$ " + setupEcho + "user@host:~$ "
	got := filterAll(in)
	want := "motd\r\nuser@host:~$ user@host:~$ "
	if got != want {
		t.Fatalf("Filter left %q, want %q", got, want)
	}
}

// An echo can be split across any read boundary, so the filter must work fed
// one byte at a time.
func TestEchoFilterSurvivesAnySplit(t *testing.T) {
	in := "a\r\n" + setupEcho + "$ "
	chunks := make([]string, 0, len(in))
	for i := range in {
		chunks = append(chunks, in[i:i+1])
	}
	if got := filterAll(chunks...); got != "a\r\n$ " {
		t.Fatalf("byte-by-byte: Filter left %q, want %q", got, "a\r\n$ ")
	}
}

// A partial match that fails must release every withheld byte unchanged.
func TestEchoFilterReleasesFailedMatches(t *testing.T) {
	in := " PROMPT_COMMAND is a bash variable\r\n"
	if got := filterAll(in); got != in {
		t.Fatalf("Filter mangled a near-miss: %q, want %q", got, in)
	}
}

// The withheld prefix of a failed match can itself contain the start of a real
// echo; the filter must find it.
func TestEchoFilterFindsEchoAfterOverlap(t *testing.T) {
	// Two leading spaces: the first starts a match that fails on the second,
	// and the second starts the real echo. The first space is ordinary text
	// and survives; the echo does not.
	got := filterAll(" " + setupEcho + "$ ")
	if got != " $ " {
		t.Fatalf("Filter left %q, want %q", got, " $ ")
	}
}

// After both copies are gone the filter is a no-op: a user who broadcasts the
// setup line later sees its echo like any other text.
func TestEchoFilterStopsAfterTwoCopies(t *testing.T) {
	got := filterAll(setupEcho + setupEcho + setupEcho)
	if got != setupEcho {
		t.Fatalf("Filter left %q, want one surviving copy %q", got, setupEcho)
	}
}

// A stream that ends mid-match still shows every byte it carried.
func TestEchoFilterFlushReleasesPartialMatch(t *testing.T) {
	partial := ExitSetupCommand[:20]
	if got := filterAll(partial); got != partial {
		t.Fatalf("Flush released %q, want %q", got, partial)
	}
}

// Line breaks directly after a removed echo are swallowed too; text is not.
func TestEchoFilterSwallowsOnlyTheLineBreak(t *testing.T) {
	got := filterAll(ExitSetupCommand[:len(ExitSetupCommand)-1] + "\r\n\r\nnext")
	if got != "next" {
		t.Fatalf("Filter left %q, want %q", got, "next")
	}
}
