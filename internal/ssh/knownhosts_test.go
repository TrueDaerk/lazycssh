package ssh

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/TrueDaerk/lazycssh/internal/hosts"
)

// promptFunc adapts a function to HostKeyPrompter.
type promptFunc func(ctx context.Context, sessionID string, host hosts.Host, keyType, fingerprint string) (bool, error)

func (f promptFunc) ConfirmHostKey(ctx context.Context, sessionID string, host hosts.Host, keyType, fingerprint string) (bool, error) {
	return f(ctx, sessionID, host, keyType, fingerprint)
}

// knownHostsFor returns a path to a known_hosts file containing the server's key
// for its own address.
func knownHostsFor(t *testing.T, srv *testServer, dir string) string {
	t.Helper()

	addr, port := srv.Addr()
	line := knownhosts.Line(
		[]string{knownhosts.Normalize(net.JoinHostPort(addr, strconv.Itoa(port)))},
		srv.signer.PublicKey())

	path := filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}
	return path
}

// serverHost is the resolved host describing the test server.
func serverHost(srv *testServer) hosts.Host {
	addr, port := srv.Addr()
	return hosts.Host{Alias: "test-host", Addr: addr, User: "tester", Port: port}
}

// startWith dials the test server using the given host key callback.
func startWith(t *testing.T, srv *testServer, callback ssh.HostKeyCallback) (Session, error) {
	t.Helper()

	host := serverHost(srv)
	s := New("s1", Config{
		Host:            host,
		Auth:            []ssh.AuthMethod{ssh.Password(srv.Password)},
		HostKeyCallback: callback,
		Timeout:         5 * time.Second,
	}, nil)
	t.Cleanup(func() { s.Close() })

	return s, s.Start(t.Context())
}

func TestKnownHostsAcceptsARecordedKey(t *testing.T) {
	srv := newTestServer(t)
	path := knownHostsFor(t, srv, t.TempDir())

	kh, err := NewKnownHosts([]string{path}, promptFunc(func(context.Context, string, hosts.Host, string, string) (bool, error) {
		t.Error("a recorded key must not produce a prompt")
		return false, nil
	}))
	if err != nil {
		t.Fatalf("NewKnownHosts: %v", err)
	}

	s, err := startWith(t, srv, kh.Callback(t.Context(), "s1", serverHost(srv)))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForOutput(t, s, "welcome")
}

func TestKnownHostsPromptsForAnUnknownKeyAndRemembersIt(t *testing.T) {
	srv := newTestServer(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "known_hosts")

	var (
		prompts         atomic.Int32
		seenFingerprint string
		seenKeyType     string
	)
	kh, err := NewKnownHosts([]string{path}, promptFunc(
		func(_ context.Context, _ string, _ hosts.Host, keyType, fingerprint string) (bool, error) {
			prompts.Add(1)
			seenKeyType, seenFingerprint = keyType, fingerprint
			return true, nil
		}))
	if err != nil {
		t.Fatalf("NewKnownHosts: %v", err)
	}

	s, err := startWith(t, srv, kh.Callback(t.Context(), "s1", serverHost(srv)))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForOutput(t, s, "welcome")

	if got := prompts.Load(); got != 1 {
		t.Errorf("prompted %d times, want 1", got)
	}
	// The user must be shown what ssh shows, not an opaque blob.
	if want := ssh.FingerprintSHA256(srv.signer.PublicKey()); seenFingerprint != want {
		t.Errorf("fingerprint = %q, want %q", seenFingerprint, want)
	}
	if seenKeyType != srv.signer.PublicKey().Type() {
		t.Errorf("key type = %q, want %q", seenKeyType, srv.signer.PublicKey().Type())
	}

	// Accepting must persist, so the next run does not ask again.
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read known_hosts: %v", err)
	}
	if !strings.Contains(string(contents), srv.signer.PublicKey().Type()) {
		t.Errorf("known_hosts = %q, want the accepted key appended", contents)
	}

	// And it must be effective immediately for further sessions this run.
	prompts.Store(0)
	s2, err := startWith(t, srv, kh.Callback(t.Context(), "s1", serverHost(srv)))
	if err != nil {
		t.Fatalf("second Start: %v", err)
	}
	waitForOutput(t, s2, "welcome")
	if got := prompts.Load(); got != 0 {
		t.Errorf("prompted %d times for an already accepted key", got)
	}
}

func TestKnownHostsRejectionFailsOnlyThatSession(t *testing.T) {
	srv := newTestServer(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "known_hosts")

	kh, err := NewKnownHosts([]string{path}, promptFunc(
		func(context.Context, string, hosts.Host, string, string) (bool, error) { return false, nil }))
	if err != nil {
		t.Fatalf("NewKnownHosts: %v", err)
	}

	s, err := startWith(t, srv, kh.Callback(t.Context(), "s1", serverHost(srv)))
	if err == nil {
		t.Fatal("Start succeeded with a rejected host key")
	}
	if !errors.Is(err, ErrHostKeyRejected) {
		t.Errorf("error = %v, want ErrHostKeyRejected", err)
	}
	if got := s.State(); got != StateFailed {
		t.Errorf("State() = %s, want %s", got, StateFailed)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("a rejected key was written to known_hosts")
	}
}

func TestKnownHostsChangedKeyIsAHardFailure(t *testing.T) {
	srv := newTestServer(t)
	other := newTestServer(t)
	dir := t.TempDir()

	// Record the *other* server's key under this server's address.
	addr, port := srv.Addr()
	line := knownhosts.Line(
		[]string{knownhosts.Normalize(net.JoinHostPort(addr, strconv.Itoa(port)))},
		other.signer.PublicKey())
	path := filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}

	var prompts atomic.Int32
	kh, err := NewKnownHosts([]string{path}, promptFunc(
		func(context.Context, string, hosts.Host, string, string) (bool, error) {
			prompts.Add(1)
			return true, nil // would accept, and must never be asked
		}))
	if err != nil {
		t.Fatalf("NewKnownHosts: %v", err)
	}

	_, err = startWith(t, srv, kh.Callback(t.Context(), "s1", serverHost(srv)))
	if err == nil {
		t.Fatal("Start succeeded against a host whose key changed")
	}

	if got := prompts.Load(); got != 0 {
		t.Errorf("the user was asked %d times about a changed key; it must never be a question", got)
	}

	var changed *HostKeyChangedError
	if !errors.As(err, &changed) {
		t.Fatalf("error = %v (%T), want *HostKeyChangedError", err, err)
	}
	for _, want := range []string{"IDENTIFICATION HAS CHANGED", ssh.FingerprintSHA256(srv.signer.PublicKey()), path} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err, want)
		}
	}
	if changed.KnownLine != 1 {
		t.Errorf("KnownLine = %d, want 1: the user needs to know which line to fix", changed.KnownLine)
	}

	// The file must be untouched.
	contents, _ := os.ReadFile(path)
	if strings.Count(string(contents), "\n") != 1 {
		t.Errorf("known_hosts = %q, want it unchanged", contents)
	}
}

func TestKnownHostsWithoutPrompterRefuses(t *testing.T) {
	srv := newTestServer(t)
	path := filepath.Join(t.TempDir(), "known_hosts")

	kh, err := NewKnownHosts([]string{path}, nil)
	if err != nil {
		t.Fatalf("NewKnownHosts: %v", err)
	}

	_, err = startWith(t, srv, kh.Callback(t.Context(), "s1", serverHost(srv)))
	if err == nil {
		t.Fatal("Start succeeded with an unknown key and no prompter")
	}
	if !errors.Is(err, ErrNoHostKeyPrompter) {
		t.Errorf("error = %v, want ErrNoHostKeyPrompter: an unknown key must never be accepted silently", err)
	}
	if !strings.Contains(err.Error(), ssh.FingerprintSHA256(srv.signer.PublicKey())) {
		t.Errorf("error = %q, want it to name the fingerprint so the user can act on it", err)
	}
}

func TestKnownHostsPrompterErrorFailsTheSession(t *testing.T) {
	srv := newTestServer(t)
	path := filepath.Join(t.TempDir(), "known_hosts")

	kh, err := NewKnownHosts([]string{path}, promptFunc(
		func(context.Context, string, hosts.Host, string, string) (bool, error) {
			return false, errors.New("the user closed the prompt")
		}))
	if err != nil {
		t.Fatalf("NewKnownHosts: %v", err)
	}

	if _, err := startWith(t, srv, kh.Callback(t.Context(), "s1", serverHost(srv))); err == nil {
		t.Fatal("Start succeeded although the prompt failed")
	}
}

func TestKnownHostsReadsHashedEntries(t *testing.T) {
	srv := newTestServer(t)
	dir := t.TempDir()

	// knownhosts.HashHostname produces the |1|salt|hash form ssh writes with
	// HashKnownHosts=yes.
	addr, port := srv.Addr()
	normalized := knownhosts.Normalize(net.JoinHostPort(addr, strconv.Itoa(port)))
	line := knownhosts.Line([]string{knownhosts.HashHostname(normalized)}, srv.signer.PublicKey())

	path := filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}

	kh, err := NewKnownHosts([]string{path}, promptFunc(
		func(context.Context, string, hosts.Host, string, string) (bool, error) {
			t.Error("a hashed but recorded key must not produce a prompt")
			return false, nil
		}))
	if err != nil {
		t.Fatalf("NewKnownHosts: %v", err)
	}

	s, err := startWith(t, srv, kh.Callback(t.Context(), "s1", serverHost(srv)))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForOutput(t, s, "welcome")
}

func TestKnownHostsSkipsMissingFilesButUsesTheOnesThatExist(t *testing.T) {
	srv := newTestServer(t)
	dir := t.TempDir()
	present := knownHostsFor(t, srv, dir)
	absent := filepath.Join(dir, "known_hosts2")

	// The writable file is listed first and does not exist; the second does.
	kh, err := NewKnownHosts([]string{absent, present}, promptFunc(
		func(context.Context, string, hosts.Host, string, string) (bool, error) {
			t.Error("a key recorded in the second file must not produce a prompt")
			return false, nil
		}))
	if err != nil {
		t.Fatalf("NewKnownHosts: %v", err)
	}

	s, err := startWith(t, srv, kh.Callback(t.Context(), "s1", serverHost(srv)))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForOutput(t, s, "welcome")
}

func TestKnownHostsReloadsWhenAFileChangesAfterConstruction(t *testing.T) {
	srv := newTestServer(t)
	other := newTestServer(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "known_hosts")

	addr, port := srv.Addr()
	normalized := knownhosts.Normalize(net.JoinHostPort(addr, strconv.Itoa(port)))

	// Start with the *wrong* key recorded, as if the host had since been
	// rebuilt with a new one.
	staleLine := knownhosts.Line([]string{normalized}, other.signer.PublicKey())
	if err := os.WriteFile(path, []byte(staleLine+"\n"), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}

	kh, err := NewKnownHosts([]string{path}, promptFunc(
		func(context.Context, string, hosts.Host, string, string) (bool, error) {
			t.Error("the corrected key must not produce a prompt")
			return false, nil
		}))
	if err != nil {
		t.Fatalf("NewKnownHosts: %v", err)
	}

	// Simulate `ssh-keygen -R` plus re-adding the real key, as the issue's
	// repro does, from outside this process while lazycssh keeps running.
	freshLine := knownhosts.Line([]string{normalized}, srv.signer.PublicKey())
	if err := os.WriteFile(path, []byte(freshLine+"\n"), 0o600); err != nil {
		t.Fatalf("rewrite known_hosts: %v", err)
	}
	// Force the mtime forward so the change is detected even on filesystems
	// with coarse (e.g. 1s) mtime resolution.
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	cb := kh.Callback(t.Context(), "s1", serverHost(srv))
	if err := cb(normalized, &net.TCPAddr{}, srv.signer.PublicKey()); err != nil {
		t.Fatalf("verify after known_hosts was corrected: %v", err)
	}
}

func TestKnownHostsPicksUpAFileCreatedAfterConstruction(t *testing.T) {
	srv := newTestServer(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "known_hosts")
	// path does not exist yet: NewKnownHosts must succeed anyway.

	kh, err := NewKnownHosts([]string{path}, promptFunc(
		func(context.Context, string, hosts.Host, string, string) (bool, error) {
			t.Error("a key recorded in a file created after construction must not produce a prompt")
			return false, nil
		}))
	if err != nil {
		t.Fatalf("NewKnownHosts: %v", err)
	}

	addr, port := srv.Addr()
	normalized := knownhosts.Normalize(net.JoinHostPort(addr, strconv.Itoa(port)))
	line := knownhosts.Line([]string{normalized}, srv.signer.PublicKey())
	if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
		t.Fatalf("create known_hosts: %v", err)
	}

	cb := kh.Callback(t.Context(), "s1", serverHost(srv))
	if err := cb(normalized, &net.TCPAddr{}, srv.signer.PublicKey()); err != nil {
		t.Fatalf("verify after known_hosts was created: %v", err)
	}
}

func TestKnownHostsCreatesTheFileAndItsDirectory(t *testing.T) {
	srv := newTestServer(t)
	// A path two levels below an empty temp dir, as on a machine with no ~/.ssh.
	path := filepath.Join(t.TempDir(), "dot-ssh", "known_hosts")

	kh, err := NewKnownHosts([]string{path}, promptFunc(
		func(context.Context, string, hosts.Host, string, string) (bool, error) { return true, nil }))
	if err != nil {
		t.Fatalf("NewKnownHosts: %v", err)
	}

	if _, err := startWith(t, srv, kh.Callback(t.Context(), "s1", serverHost(srv))); err != nil {
		t.Fatalf("Start: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("known_hosts was not created: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("known_hosts mode = %v, want 0600", perm)
	}
}

func TestNewKnownHostsRequiresAFile(t *testing.T) {
	if _, err := NewKnownHosts(nil, nil); err == nil {
		t.Error("NewKnownHosts(nil) returned no error")
	}
}

func TestInsecureIgnoreHostKeyConnectsToAnything(t *testing.T) {
	srv := newTestServer(t)

	// The opt-out must work - it is a supported flag - but it has to be chosen
	// explicitly. Session.Start refuses a nil callback rather than defaulting
	// to this; TestSessionRefusesToStartWithoutHostKeyVerification covers that.
	s, err := startWith(t, srv, InsecureIgnoreHostKey())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForOutput(t, s, "welcome")
}

func TestHostAddress(t *testing.T) {
	tests := []struct {
		name string
		host hosts.Host
		want string
	}{
		{
			name: "the default port is written bare, as ssh does",
			host: hosts.Host{Addr: "srv1.example.com", Port: 22},
			want: "srv1.example.com",
		},
		{
			name: "a custom port is bracketed",
			host: hosts.Host{Addr: "srv1.example.com", Port: 2222},
			want: "[srv1.example.com]:2222",
		},
		{
			name: "an IPv6 address with a custom port",
			host: hosts.Host{Addr: "2001:db8::1", Port: 2222},
			want: "[2001:db8::1]:2222",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HostAddress(tt.host); got != tt.want {
				t.Errorf("HostAddress = %q, want %q", got, tt.want)
			}
		})
	}
}
