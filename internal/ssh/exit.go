// Exit code tracking. A PTY carries one byte stream: there is no out-of-band
// channel that says how the last command ended, so the shell has to be asked
// to say it in-band. lazycssh injects a prompt hook that prints an OSC 133;D
// sequence - the FinalTerm / shell-integration convention - carrying $? before
// every prompt. The sequence is invisible: terminals ignore unknown OSC
// sequences, so the marker never shows in a pane.
//
// Degradation is graceful by design. A shell that does not run the hook - a
// plain POSIX sh, a restricted shell, a user profile that overwrites the
// variables - simply never emits the marker, and the session reports "no exit
// status known" rather than a wrong one.

package ssh

// exitMarkerPrefix is the start of the marker: OSC 133 ("semantic prompt")
// subcommand D, "command finished", followed by the exit code and a BEL.
const exitMarkerPrefix = "\x1b]133;D;"

// ExitSetupCommand is the line written to the remote shell's stdin right after
// it starts. It arms both bash (PROMPT_COMMAND) and zsh (precmd); each shell
// ignores the mechanism of the other. The leading space keeps the line out of
// the history of shells configured with HISTCONTROL=ignorespace.
// The line ends in a carriage return because that is what "enter" is on a PTY.
const ExitSetupCommand = " PROMPT_COMMAND='printf \"\\033]133;D;%d\\007\" \"$?\"'; " +
	"precmd() { printf \"\\033]133;D;%d\\007\" \"$?\"; }\r"

// ExitEvent says a command finished on a session and how.
type ExitEvent struct {
	// ID names the session.
	ID string
	// Code is the exit status the shell reported.
	Code int
	// Seq is how many markers this session has reported, this one included.
	// It is what lets a consumer tell "the command I sent has answered" from
	// "the answer that was already there"; see [Session.LastExit].
	Seq uint64
}

// SessionID implements [Event].
func (e ExitEvent) SessionID() string { return e.ID }

// exitScanner watches a session's output stream for exit markers. It is a byte
// state machine, because a marker can be split across any read boundary. It
// never blocks and never fails: a malformed marker is abandoned, not reported.
//
// The scanner is not safe for concurrent use; each session feeds it from one
// goroutine only.
type exitScanner struct {
	// onExit is called with each completed marker's code.
	onExit func(code int)

	matched int  // bytes of exitMarkerPrefix matched so far
	inCode  bool // the prefix matched; digits are being collected
	code    int
	digits  int
}

// maxExitDigits bounds the number: an exit status has at most three digits,
// and a stream that keeps sending digits is not a marker.
const maxExitDigits = 3

// Scan consumes one chunk of session output. The chunk is only inspected,
// never modified; the caller writes the same bytes to the scrollback.
func (e *exitScanner) Scan(p []byte) {
	for _, c := range p {
		if e.inCode {
			switch {
			case c >= '0' && c <= '9' && e.digits < maxExitDigits:
				e.code = e.code*10 + int(c-'0')
				e.digits++
			case c == '\a' && e.digits > 0:
				e.onExit(e.code)
				e.reset()
			default:
				e.reset()
			}
			continue
		}

		if c == exitMarkerPrefix[e.matched] {
			e.matched++
			if e.matched == len(exitMarkerPrefix) {
				e.inCode = true
			}
			continue
		}
		e.reset()
		// The mismatched byte could itself start a marker.
		if c == exitMarkerPrefix[0] {
			e.matched = 1
		}
	}
}

// reset abandons any partial match.
func (e *exitScanner) reset() {
	e.matched = 0
	e.inCode = false
	e.code = 0
	e.digits = 0
}

// recordExit stores a session's newest exit status and reports it as an event.
func (s *sshSession) recordExit(code int) {
	s.mu.Lock()
	s.lastExit = code
	s.exitSeq++
	seq := s.exitSeq
	s.mu.Unlock()
	s.emit(ExitEvent{ID: s.id, Code: code, Seq: seq})
}

// LastExit returns the exit status of the last command the shell reported and
// how many markers have arrived on this session. A zero sequence means nothing
// has been reported - a shell without the prompt hook never reports, and
// saying "unknown" is the honest answer there.
//
// The two values come out of one lock on purpose. A consumer decides whether
// an exit belongs to the command it sent by comparing the sequence against the
// one it recorded at the send, and a code read a moment apart from its
// sequence would let it attribute the previous command's status to the new
// one.
func (s *sshSession) LastExit() (int, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastExit, s.exitSeq
}
