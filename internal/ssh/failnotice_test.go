package ssh

import (
	"errors"
	"strings"
	"testing"

	"github.com/TrueDaerk/lazycssh/internal/hosts"
)

// A failed session prints its error into its own scrollback, like a terminal
// would (issue #180) - once, however often the state machine is poked after.
func TestFailureIsWrittenToTheScrollback(t *testing.T) {
	f := NewFake("h1", hosts.Host{Alias: "h1"}, nil)
	f.Emit("output\r\n")
	f.Disconnect(errors.New("session on h1 ended: connection reset"))
	f.Disconnect(errors.New("a second failure"))

	got := f.Scrollback().String()
	if !strings.Contains(got, "connection reset") {
		t.Fatalf("the failure is not in the scrollback:\n%s", got)
	}
	if !strings.Contains(got, "output") {
		t.Fatalf("the failure replaced the output:\n%s", got)
	}
	if strings.Contains(got, "second failure") {
		t.Fatalf("a done session wrote another failure:\n%s", got)
	}
}
