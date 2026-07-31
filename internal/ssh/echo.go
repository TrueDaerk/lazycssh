// Suppression of the exit-hook echo. The line [ExitSetupCommand] typed into
// the remote shell comes straight back: the PTY line discipline echoes the
// bytes as they arrive, and a line-editing shell (readline, zle) redraws them
// once more next to its first prompt. Both copies are noise the user never
// typed, so the stdout pump removes them before the bytes reach the scrollback
// or the terminal emulator.
//
// The filter is deliberately narrow: it matches the exact bytes of the setup
// line and nothing else, suppresses at most two occurrences, and stands down
// for good the moment the first exit marker arrives — after the hook has
// printed once, every byte on the stream is the user's business.

package ssh

import (
	"bytes"
	"strings"
)

// echoedSetup is what the remote side echoes back: the setup line without the
// carriage return that ends it, and without the leading space — the space is
// for HISTCONTROL, and leaving it visible costs one blank character while
// letting the match start on an uncommon byte.
var echoedSetup = []byte(strings.TrimSuffix(strings.TrimPrefix(ExitSetupCommand, " "), "\r"))

// maxSetupEchoes is how many copies of the line can come back: one from the
// PTY echo, one from a line-editing shell redrawing its input.
const maxSetupEchoes = 2

// setupEchoFilter removes the echoed setup line from a session's output
// stream. It is a streaming matcher: an echo can be split across any read
// boundary, so a stream suffix that could still grow into the pattern is held
// back until the next chunk decides.
//
// Like the exit scanner it is not safe for concurrent use; the stdout pump
// feeds it from one goroutine only.
type setupEchoFilter struct {
	held      []byte // stream suffix that is a prefix of the pattern
	remaining int    // suppressions left before the filter stands down
	stopped   bool
}

// newSetupEchoFilter returns a filter armed for the copies one connect can
// produce.
func newSetupEchoFilter() *setupEchoFilter {
	return &setupEchoFilter{remaining: maxSetupEchoes}
}

// active reports whether the filter still inspects the stream.
func (f *setupEchoFilter) active() bool {
	return !f.stopped && f.remaining > 0
}

// Filter consumes one chunk and returns what may pass. The result can be
// shorter than the input (an echo was removed, or a possible echo start is
// held back) or longer (held bytes turned out not to be an echo).
func (f *setupEchoFilter) Filter(p []byte) []byte {
	if !f.active() {
		if len(f.held) == 0 {
			return p
		}
		out := append(f.held, p...)
		f.held = nil
		return out
	}

	data := append(f.held, p...)
	f.held = nil

	var out []byte
	for f.remaining > 0 {
		i := bytes.Index(data, echoedSetup)
		if i < 0 {
			break
		}
		out = append(out, data[:i]...)
		data = data[i+len(echoedSetup):]
		f.remaining--
	}
	if !f.active() {
		return append(out, data...)
	}

	// Hold back the longest stream suffix that is still a prefix of the
	// pattern; everything before it can no longer become an echo.
	keep := f.prefixSuffix(data)
	out = append(out, data[:len(data)-keep]...)
	f.held = append([]byte(nil), data[len(data)-keep:]...)
	return out
}

// Stop stands the filter down and returns whatever it was still holding back.
// It is idempotent; after the first call Filter passes everything through.
func (f *setupEchoFilter) Stop() []byte {
	f.stopped = true
	held := f.held
	f.held = nil
	return held
}

// prefixSuffix returns the length of the longest suffix of data that is a
// proper prefix of the pattern.
func (f *setupEchoFilter) prefixSuffix(data []byte) int {
	max := len(echoedSetup) - 1
	if len(data) < max {
		max = len(data)
	}
	for k := max; k > 0; k-- {
		if bytes.Equal(data[len(data)-k:], echoedSetup[:k]) {
			return k
		}
	}
	return 0
}
