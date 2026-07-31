// The exit-hook setup line is typed into the remote shell, and a PTY echoes
// what is typed: once by the kernel while the line waits in the input queue,
// and once more by the shell's line editor when it redisplays the pending
// input at the first prompt. Both copies would land in the scrollback as
// visible noise at the top of every pane, so the stdout stream is passed
// through this filter, which removes the known setup text before anything
// else sees it.
//
// The filter is deliberately narrow: it matches the exact bytes of
// [ExitSetupCommand] and nothing else, removes at most two occurrences, and
// then becomes a no-op for the rest of the session. A shell whose echo looks
// different - syntax highlighting, a line editor that rewrites the text -
// simply keeps its echo; that is the same graceful degradation the exit hook
// itself promises.

package ssh

// setupEchoCopies is how many echoed copies of the setup line a session can
// produce: the kernel echo and the line editor's redisplay.
const setupEchoCopies = 2

// echoFilter removes echoed copies of the exit-hook setup line from a byte
// stream. Like [exitScanner] it is a byte state machine, because an echo can
// be split across any read boundary; unlike the scanner it rewrites the
// stream, so bytes that might be the start of an echo are withheld until the
// match completes or fails.
//
// The filter is not safe for concurrent use; each session feeds it from one
// goroutine only.
type echoFilter struct {
	target    []byte // the setup line without its trailing carriage return
	matched   int    // bytes of target matched so far, all withheld
	swallowNL bool   // a copy was removed; its line break is being dropped too
	remaining int    // copies still to remove; 0 means pass-through
	out       []byte // scratch output buffer, reused across calls
}

// newEchoFilter returns a filter armed for one session's startup.
func newEchoFilter() *echoFilter {
	return &echoFilter{
		// The trailing \r is "enter" on the way in; on the way back the echo
		// ends in whatever line break the terminal produced, so the match
		// stops before it and swallowNL drops it.
		target:    []byte(ExitSetupCommand[:len(ExitSetupCommand)-1]),
		remaining: setupEchoCopies,
	}
}

// Filter consumes one chunk of stream bytes and returns the bytes to keep.
// The returned slice is only valid until the next call. Bytes that form a
// partial match are withheld across calls; Flush releases them at stream end.
func (f *echoFilter) Filter(p []byte) []byte {
	if f.remaining == 0 && !f.swallowNL {
		return p
	}
	f.out = f.out[:0]
	for _, c := range p {
		f.feed(c)
	}
	return f.out
}

// Flush returns any withheld partial match. Call it when the stream ends, so
// a stream that stops mid-match still shows every byte it carried.
func (f *echoFilter) Flush() []byte {
	n := f.matched
	f.matched = 0
	return f.target[:n]
}

// feed advances the state machine by one byte.
func (f *echoFilter) feed(c byte) {
	if f.swallowNL {
		if c == '\r' || c == '\n' {
			return
		}
		f.swallowNL = false
	}

	if f.remaining == 0 {
		f.out = append(f.out, c)
		return
	}

	if c == f.target[f.matched] {
		f.matched++
		if f.matched == len(f.target) {
			f.matched = 0
			f.remaining--
			f.swallowNL = true
		}
		return
	}

	if f.matched == 0 {
		f.out = append(f.out, c)
		return
	}

	// The match failed partway. The first withheld byte is definitely not
	// part of an echo, but the rest might overlap the start of one, so they
	// are run through the machine again rather than emitted wholesale.
	n := f.matched
	f.matched = 0
	f.out = append(f.out, f.target[0])
	for _, b := range f.target[1:n] {
		f.feed(b)
	}
	f.feed(c)
}
