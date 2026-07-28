// Package secret holds credential material in memory and keeps it out of
// everything else: logs, error strings, rendered views and files on disk.
//
// The type here is deliberately awkward to print. A [Value] renders as
// "[redacted]" through every formatting verb and refuses to be serialised, so
// the accident that leaks a password - a `%v` in an error, a struct dumped into
// a log line - cannot happen by omission. Reading the secret is an explicit call.
package secret

import (
	"errors"
	"fmt"
)

// redacted is what a secret renders as, wherever it is rendered.
const redacted = "[redacted]"

// ErrNotSerialisable is returned when something tries to write a secret to disk
// or over the wire as data.
var ErrNotSerialisable = errors.New("secret: credentials are never serialised")

// Value is a credential held in memory only.
//
// The zero value is an empty secret. Values are not safe for concurrent
// mutation; they are created, read and wiped by one owner.
type Value struct {
	b []byte
}

// New takes ownership of a byte slice holding a credential. The caller must not
// keep using it: [Value.Wipe] overwrites this memory.
func New(b []byte) *Value { return &Value{b: b} }

// FromString copies a credential out of a string.
//
// A string cannot be wiped - it is immutable and the runtime may keep copies -
// so this constructor is a compromise for the paths that already hand around
// strings, not a recommendation.
func FromString(s string) *Value { return &Value{b: []byte(s)} }

// Reveal returns the credential. Every call site is a place a secret leaves this
// package, which is why it is a method with a conspicuous name rather than a
// field.
func (v *Value) Reveal() string {
	if v == nil {
		return ""
	}
	return string(v.b)
}

// Bytes returns the underlying buffer, which stays live until [Value.Wipe].
func (v *Value) Bytes() []byte {
	if v == nil {
		return nil
	}
	return v.b
}

// Len is the length of the credential, which is safe to report: it is what a
// masked input already shows.
func (v *Value) Len() int {
	if v == nil {
		return 0
	}
	return len(v.b)
}

// Empty reports whether there is no credential.
func (v *Value) Empty() bool { return v.Len() == 0 }

// Wipe overwrites the buffer and empties the value. It is best effort: Go
// cannot promise the memory was never copied by the garbage collector, and a
// credential that was ever a string cannot be reached at all. It removes the
// obvious copy, which is the one that outlives the run in a core dump.
func (v *Value) Wipe() {
	if v == nil {
		return
	}
	for i := range v.b {
		v.b[i] = 0
	}
	v.b = nil
}

// String renders the secret as "[redacted]".
func (v *Value) String() string { return redacted }

// GoString renders the secret as "[redacted]" under %#v.
func (v *Value) GoString() string { return redacted }

// Format renders the secret as "[redacted]" under every verb, including %s, %q
// and %x, so no format string can print it by accident.
func (v *Value) Format(f fmt.State, verb rune) {
	switch verb {
	case 'q':
		fmt.Fprintf(f, "%q", redacted)
	default:
		fmt.Fprint(f, redacted)
	}
}

// MarshalYAML refuses to serialise a secret.
func (v *Value) MarshalYAML() (any, error) { return nil, ErrNotSerialisable }

// MarshalJSON refuses to serialise a secret.
func (v *Value) MarshalJSON() ([]byte, error) { return nil, ErrNotSerialisable }

// MarshalText refuses to serialise a secret, which also covers the encoders that
// reach for [encoding.TextMarshaler].
func (v *Value) MarshalText() ([]byte, error) { return nil, ErrNotSerialisable }
