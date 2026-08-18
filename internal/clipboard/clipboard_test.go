package clipboard

import "testing"

// env builds a lookup over a fixed map, so nothing here reads the machine's
// real environment.
func env(pairs map[string]string) func(string) string {
	return func(k string) string { return pairs[k] }
}

// The variables sshd sets each mean the same thing on their own: this process
// is running on the far end of an SSH connection.
func TestRemoteSession(t *testing.T) {
	tests := []struct {
		name string
		vars map[string]string
		want bool
	}{
		{name: "a local shell has none of them", vars: map[string]string{}},
		{name: "SSH_CONNECTION", vars: map[string]string{"SSH_CONNECTION": "10.0.0.1 5 10.0.0.2 22"}, want: true},
		{name: "SSH_CLIENT", vars: map[string]string{"SSH_CLIENT": "10.0.0.1 5 22"}, want: true},
		{name: "SSH_TTY", vars: map[string]string{"SSH_TTY": "/dev/pts/3"}, want: true},
		{name: "an unrelated variable is not one", vars: map[string]string{"TERM": "xterm-256color"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RemoteSession(env(tt.vars)); got != tt.want {
				t.Fatalf("RemoteSession = %v, want %v", got, tt.want)
			}
		})
	}
}

// A run inside an SSH session installs no local writer: the clipboard the OS
// tools would reach belongs to the far machine, and OSC 52 is the only path
// that can reach the user's own.
func TestNewInstallsNoWriterInsideAnSSHSession(t *testing.T) {
	if w := New(env(map[string]string{"SSH_CONNECTION": "10.0.0.1 5 10.0.0.2 22"})); w != nil {
		t.Fatalf("New = %v, want nil inside an SSH session", w)
	}
}

// A nil return is a nil interface, not a typed nil: the caller assigns it
// into an interface field, where a typed nil would read as "there is a
// clipboard" and every copy would then shell out on a machine with no tool.
func TestNewReturnsAnUntypedNil(t *testing.T) {
	var w Writer = New(env(map[string]string{"SSH_TTY": "/dev/pts/3"}))
	if w != nil {
		t.Fatal("New returned a typed nil")
	}
}
