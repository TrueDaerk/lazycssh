package ssh

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/TrueDaerk/lazycssh/internal/hosts"
)

// HostKeyPrompter asks the user what to do about a host key that is not in
// known_hosts.
//
// It is only ever consulted for an *unknown* key. A key that is known and
// different is never a question - see [HostKeyChangedError].
type HostKeyPrompter interface {
	// ConfirmHostKey reports whether the key should be accepted and remembered.
	// The fingerprint is the SHA256 form ssh itself prints. The session id
	// names the dialling pane the question belongs to (issue #182).
	ConfirmHostKey(ctx context.Context, sessionID string, host hosts.Host, keyType, fingerprint string) (bool, error)
}

// ErrHostKeyRejected is returned when the user declines an unknown host key.
var ErrHostKeyRejected = errors.New("ssh: host key rejected")

// ErrNoHostKeyPrompter is returned when an unknown host key is encountered and
// nothing can ask the user about it. It is a refusal, never a silent accept.
var ErrNoHostKeyPrompter = errors.New("ssh: unknown host key and no way to ask about it")

// HostKeyChangedError reports that a host presented a different key than the one
// recorded in known_hosts.
//
// This is deliberately not a prompt. An unknown key is a first meeting; a
// changed key is either a rebuilt machine or someone sitting in the middle of
// the connection, and a tool that offers to click that away is worse than one
// that refuses.
type HostKeyChangedError struct {
	Host        hosts.Host
	KeyType     string
	Fingerprint string
	KnownFile   string
	KnownLine   int
}

func (e *HostKeyChangedError) Error() string {
	return fmt.Sprintf(
		"REMOTE HOST IDENTIFICATION HAS CHANGED for %s: it offered a %s key with fingerprint %s, "+
			"which does not match the key recorded in %s line %d. "+
			"Remove that line only if you know the host was rebuilt",
		e.Host.Alias, e.KeyType, e.Fingerprint, e.KnownFile, e.KnownLine)
}

// KnownHosts verifies server keys against OpenSSH known_hosts files, and can
// append a key the user accepted.
//
// Verification is on by default and cannot be weakened by a config file. The
// only way out is [InsecureIgnoreHostKey], which the caller has to choose
// explicitly.
type KnownHosts struct {
	// Prompter is asked about unknown keys. Nil means unknown keys are refused
	// rather than accepted.
	Prompter HostKeyPrompter

	files    []string
	writable string // where accepted keys are appended

	mu        sync.Mutex
	verify    ssh.HostKeyCallback
	snapshots map[string]fileSnapshot // last-loaded mtime/size per file, for change detection
	accepted  map[string]struct{}     // normalized address + key, accepted this run
}

// fileSnapshot is the state a known_hosts file was in when last loaded. It is
// deliberately coarse (mtime + size, not a hash): known_hosts files are
// rewritten wholesale by ssh-keygen and friends, so a changed mtime or size is
// enough to know the cached verifier is stale, without reading the file twice
// per check.
type fileSnapshot struct {
	exists  bool
	modTime time.Time
	size    int64
}

// snapshotFiles stats each file. A file that cannot be stat'd (missing or
// otherwise inaccessible) is recorded as not existing, mirroring reload's
// existing-file filter.
func snapshotFiles(files []string) map[string]fileSnapshot {
	snaps := make(map[string]fileSnapshot, len(files))
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			snaps[f] = fileSnapshot{}
			continue
		}
		snaps[f] = fileSnapshot{exists: true, modTime: info.ModTime(), size: info.Size()}
	}
	return snaps
}

// DefaultKnownHostsFiles returns the files OpenSSH consults, whether or not they
// exist.
func DefaultKnownHostsFiles() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("locate home directory: %w", err)
	}
	return []string{
		filepath.Join(home, ".ssh", "known_hosts"),
		filepath.Join(home, ".ssh", "known_hosts2"),
	}, nil
}

// NewKnownHosts builds a verifier over the given files. Files that do not exist
// are skipped for reading; the first one listed is where accepted keys are
// written, and it is created on demand.
//
// A user with no known_hosts file at all is therefore prompted for every host,
// which is exactly what ssh does on a fresh machine.
func NewKnownHosts(files []string, prompter HostKeyPrompter) (*KnownHosts, error) {
	if len(files) == 0 {
		return nil, errors.New("ssh: no known_hosts files given")
	}

	k := &KnownHosts{
		Prompter: prompter,
		files:    files,
		writable: files[0],
		accepted: make(map[string]struct{}),
	}
	if err := k.reload(); err != nil {
		return nil, err
	}
	return k, nil
}

// reload rebuilds the verifier from the files that currently exist.
func (k *KnownHosts) reload() error {
	snaps := snapshotFiles(k.files)

	var existing []string
	for _, f := range k.files {
		if snaps[f].exists {
			existing = append(existing, f)
		}
	}

	k.mu.Lock()
	defer k.mu.Unlock()

	if len(existing) == 0 {
		// Nothing recorded yet: every key is unknown, none is a mismatch.
		k.verify = func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			return &knownhosts.KeyError{}
		}
		k.snapshots = snaps
		return nil
	}

	verify, err := knownhosts.New(existing...)
	if err != nil {
		return fmt.Errorf("read known_hosts: %w", err)
	}
	k.verify = verify
	k.snapshots = snaps
	return nil
}

// refreshIfChanged reloads the verifier when any known_hosts file's mtime or
// size differs from what was last loaded - including a file that appeared or
// disappeared since. It runs before every verification: connection setup is
// rare and expensive compared to a handful of stat(2) calls, so checking on
// every dial keeps a reconnect (issue #309) from missing an external edit
// made while lazycssh was already running, without needing a filesystem
// watcher.
func (k *KnownHosts) refreshIfChanged() error {
	current := snapshotFiles(k.files)

	k.mu.Lock()
	changed := !snapshotsEqual(k.snapshots, current)
	k.mu.Unlock()

	if !changed {
		return nil
	}
	return k.reload()
}

// snapshotsEqual reports whether two snapshot sets describe the same files.
func snapshotsEqual(a, b map[string]fileSnapshot) bool {
	if len(a) != len(b) {
		return false
	}
	for f, sa := range a {
		if b[f] != sa {
			return false
		}
	}
	return true
}

// Callback returns the [ssh.HostKeyCallback] for one session. The context is the
// one the prompt runs under, so a session being cancelled also cancels its
// question; the session id names the pane the question belongs to.
func (k *KnownHosts) Callback(ctx context.Context, sessionID string, host hosts.Host) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		if err := k.refreshIfChanged(); err != nil {
			return fmt.Errorf("refresh known_hosts for %s: %w", host.Alias, err)
		}

		k.mu.Lock()
		verify := k.verify
		_, alreadyAccepted := k.accepted[acceptKey(hostname, key)]
		k.mu.Unlock()

		if alreadyAccepted {
			return nil
		}

		err := verify(hostname, remote, key)
		if err == nil {
			return nil
		}

		var keyErr *knownhosts.KeyError
		if !errors.As(err, &keyErr) {
			return fmt.Errorf("verify host key for %s: %w", host.Alias, err)
		}

		if len(keyErr.Want) > 0 {
			// The host is known and the key does not match.
			known := keyErr.Want[0]
			return &HostKeyChangedError{
				Host:        host,
				KeyType:     key.Type(),
				Fingerprint: ssh.FingerprintSHA256(key),
				KnownFile:   known.Filename,
				KnownLine:   known.Line,
			}
		}

		return k.confirm(ctx, sessionID, host, hostname, key)
	}
}

// confirm asks about an unknown key and remembers it if the user accepts.
func (k *KnownHosts) confirm(ctx context.Context, sessionID string, host hosts.Host, hostname string, key ssh.PublicKey) error {
	if k.Prompter == nil {
		return fmt.Errorf("%s offered an unknown %s key with fingerprint %s: %w",
			host.Alias, key.Type(), ssh.FingerprintSHA256(key), ErrNoHostKeyPrompter)
	}

	accept, err := k.Prompter.ConfirmHostKey(ctx, sessionID, host, key.Type(), ssh.FingerprintSHA256(key))
	if err != nil {
		return fmt.Errorf("confirm host key for %s: %w", host.Alias, err)
	}
	if !accept {
		return fmt.Errorf("%s (%s): %w", host.Alias, ssh.FingerprintSHA256(key), ErrHostKeyRejected)
	}

	if err := k.Add(hostname, key); err != nil {
		return fmt.Errorf("remember host key for %s: %w", host.Alias, err)
	}
	return nil
}

// Add appends a host key to the writable known_hosts file and makes it
// immediately effective for the rest of the run.
func (k *KnownHosts) Add(hostname string, key ssh.PublicKey) error {
	line := knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key)

	if err := os.MkdirAll(filepath.Dir(k.writable), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(k.writable), err)
	}

	f, err := os.OpenFile(k.writable, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open %s: %w", k.writable, err)
	}
	defer f.Close()

	if _, err := f.WriteString(line + "\n"); err != nil {
		return fmt.Errorf("write %s: %w", k.writable, err)
	}

	k.mu.Lock()
	k.accepted[acceptKey(hostname, key)] = struct{}{}
	k.mu.Unlock()

	// Reload so that other sessions dialling the same host in this run are
	// verified from the file rather than asked again.
	return k.reload()
}

// acceptKey identifies one accepted (host, key) pair for this run.
func acceptKey(hostname string, key ssh.PublicKey) string {
	return knownhosts.Normalize(hostname) + " " + string(key.Marshal())
}

// InsecureIgnoreHostKey accepts any host key.
//
// It exists only for --insecure-ignore-host-key. The flag is explicit, the
// status bar shows it for the whole run, and nothing else in the program may
// select this: a missing callback is refused rather than defaulted to it.
func InsecureIgnoreHostKey() ssh.HostKeyCallback {
	return ssh.InsecureIgnoreHostKey()
}

// HostAddress renders the address a host key is recorded under, which is the
// hostname for port 22 and the bracketed form otherwise, exactly as ssh writes
// it.
func HostAddress(host hosts.Host) string {
	if host.Port == defaultSSHPort {
		return knownhosts.Normalize(host.Addr)
	}
	return knownhosts.Normalize(net.JoinHostPort(host.Addr, strconv.Itoa(host.Port)))
}

// defaultSSHPort mirrors the port hosts resolve to by default.
const defaultSSHPort = 22
