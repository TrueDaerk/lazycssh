package ssh

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"github.com/TrueDaerk/lazycssh/internal/hosts"
)

// Method names as reported by [Credentials.LastMethod].
const (
	MethodAgent               = "agent"
	MethodPublicKey           = "publickey"
	MethodPassword            = "password"
	MethodKeyboardInteractive = "keyboard-interactive"
)

// Prompter asks the user for a secret.
//
// Implementations must never write what they receive anywhere but back to the
// caller: no logging, no error strings, no session transcript. The UI
// implementation reads into a masked text input and keeps the value in memory
// only.
type Prompter interface {
	// Passphrase asks for the passphrase protecting a private key file. The
	// host is the one whose dial hit the encrypted key - the answer is cached
	// per key file, but the question belongs to a host the user can see.
	Passphrase(ctx context.Context, host hosts.Host, keyPath string) (string, error)
	// Password asks for the login password of a host.
	Password(ctx context.Context, host hosts.Host) (string, error)
	// Question asks a free-form keyboard-interactive question. echo reports
	// whether the answer may be shown while it is typed.
	Question(ctx context.Context, host hosts.Host, question string, echo bool) (string, error)
}

// ErrNoPrompter is returned when a secret is needed but nothing can ask for it,
// which is the case for a non-interactive run.
var ErrNoPrompter = errors.New("ssh: a secret is required but no prompter is available")

// Credentials builds the authentication methods offered to a host, and caches
// the secrets it had to ask for.
//
// The cache is what makes forty hosts bearable: a passphrase is requested once
// per key file and a password once per login user, however many hosts need it.
//
// Nothing here is ever written to disk. A Credentials value holds secrets in
// memory for the lifetime of the run.
type Credentials struct {
	// Prompter asks the user for secrets. Nil means secrets cannot be
	// obtained, which fails the methods that need one rather than hanging.
	Prompter Prompter
	// AgentSocket overrides SSH_AUTH_SOCK. Empty means read the environment.
	AgentSocket string
	// DisableAgent skips the agent even when one is available.
	DisableAgent bool

	mu          sync.Mutex
	passphrases map[string]string // key file path -> passphrase
	passwords   map[string]string // login user -> password
	lastMethod  map[string]string // session id -> the method last attempted
}

// Methods returns the authentication methods to offer a host, in the order they
// should be tried: the agent first, then identity files, then interactive
// secrets.
//
// The callbacks are lazy on purpose. Nothing prompts, reads a key file or talks
// to the agent until the server actually asks for that method, so connecting to
// hosts that accept the agent never produces a passphrase prompt.
func (c *Credentials) Methods(ctx context.Context, sessionID string, host hosts.Host) []ssh.AuthMethod {
	chain := c.chain(ctx, sessionID, host)

	methods := make([]ssh.AuthMethod, len(chain))
	for i, m := range chain {
		methods[i] = m.method
	}
	return methods
}

// MethodNames returns the names of the methods that would be offered to a host,
// in order. The Hosts panel uses it to explain what will be tried.
func (c *Credentials) MethodNames(host hosts.Host) []string {
	chain := c.chain(context.Background(), "", host)

	names := make([]string, len(chain))
	for i, m := range chain {
		names[i] = m.name
	}
	return names
}

// authMethod pairs a method with the name reported for it. The ssh package
// keeps [ssh.AuthMethod] closed - its only method is unexported - so a method
// cannot be wrapped from outside. The name is therefore carried alongside, and
// each callback records its own attempt.
type authMethod struct {
	name   string
	method ssh.AuthMethod
}

func (c *Credentials) chain(ctx context.Context, sessionID string, host hosts.Host) []authMethod {
	var chain []authMethod

	if signers, ok := c.agentSigners(); ok {
		chain = append(chain, authMethod{MethodAgent, ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
			c.note(sessionID, MethodAgent)
			return signers()
		})})
	}

	if len(host.IdentityFiles) > 0 {
		chain = append(chain, authMethod{MethodPublicKey, ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
			c.note(sessionID, MethodPublicKey)
			return c.identitySigners(ctx, host)
		})})
	}

	chain = append(chain, authMethod{MethodPassword, ssh.PasswordCallback(func() (string, error) {
		c.note(sessionID, MethodPassword)
		return c.password(ctx, host)
	})})

	chain = append(chain, authMethod{MethodKeyboardInteractive,
		ssh.KeyboardInteractive(func(name, instruction string, questions []string, echos []bool) ([]string, error) {
			c.note(sessionID, MethodKeyboardInteractive)
			return c.answer(ctx, host, questions, echos)
		})})

	return chain
}

// LastMethod reports the authentication method most recently attempted for a
// session. After a successful handshake that is the method that worked, which is
// what the Hosts panel shows.
//
// It is an observation, not a guarantee from the protocol: the ssh library
// offers no hook for "this method succeeded", and methods are attempted in
// order, so the last one attempted is the one that got in.
func (c *Credentials) LastMethod(sessionID string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastMethod[sessionID]
}

func (c *Credentials) note(sessionID, name string) {
	if sessionID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lastMethod == nil {
		c.lastMethod = make(map[string]string)
	}
	c.lastMethod[sessionID] = name
}

// agentSigners returns a callback reading signers from a running ssh-agent.
func (c *Credentials) agentSigners() (func() ([]ssh.Signer, error), bool) {
	if c.DisableAgent {
		return nil, false
	}

	socket := c.AgentSocket
	if socket == "" {
		socket = os.Getenv("SSH_AUTH_SOCK")
	}
	if socket == "" {
		return nil, false
	}

	return func() ([]ssh.Signer, error) {
		conn, err := net.Dial("unix", socket)
		if err != nil {
			return nil, fmt.Errorf("connect to ssh-agent: %w", err)
		}
		// The connection is deliberately not closed here: the returned signers
		// use it for every signature. It is released when the process exits,
		// which for a session-scoped tool is the right lifetime.
		signers, err := agent.NewClient(conn).Signers()
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("read identities from ssh-agent: %w", err)
		}
		return signers, nil
	}, true
}

// identitySigners loads the host's identity files, prompting once per encrypted
// key however many hosts use it.
func (c *Credentials) identitySigners(ctx context.Context, host hosts.Host) ([]ssh.Signer, error) {
	var (
		signers []ssh.Signer
		errs    []error
	)

	for _, path := range host.IdentityFiles {
		signer, err := c.loadIdentity(ctx, host, path)
		if err != nil {
			// A missing or unusable key is not fatal on its own: ssh config
			// files routinely list keys that do not exist on every machine.
			errs = append(errs, err)
			continue
		}
		signers = append(signers, signer)
	}

	if len(signers) == 0 && len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return signers, nil
}

// loadIdentity reads one private key, asking for a passphrase if it is
// encrypted.
func (c *Credentials) loadIdentity(ctx context.Context, host hosts.Host, path string) (ssh.Signer, error) {
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read identity %s: %w", path, err)
	}

	signer, err := ssh.ParsePrivateKey(pem)
	if err == nil {
		return signer, nil
	}

	var missing *ssh.PassphraseMissingError
	if !errors.As(err, &missing) {
		return nil, fmt.Errorf("parse identity %s: %w", path, err)
	}

	passphrase, err := c.passphrase(ctx, host, path)
	if err != nil {
		return nil, err
	}

	signer, err = ssh.ParsePrivateKeyWithPassphrase(pem, []byte(passphrase))
	if err != nil {
		// The passphrase was wrong; forget it so the next attempt can ask
		// again rather than failing every remaining host silently.
		c.forgetPassphrase(path)
		return nil, fmt.Errorf("decrypt identity %s: %w", path, err)
	}
	return signer, nil
}

// passphrase returns the cached passphrase for a key file, asking for it once.
func (c *Credentials) passphrase(ctx context.Context, host hosts.Host, path string) (string, error) {
	c.mu.Lock()
	if v, ok := c.passphrases[path]; ok {
		c.mu.Unlock()
		return v, nil
	}
	prompter := c.Prompter
	c.mu.Unlock()

	if prompter == nil {
		return "", fmt.Errorf("identity %s is encrypted: %w", path, ErrNoPrompter)
	}

	v, err := prompter.Passphrase(ctx, host, path)
	if err != nil {
		return "", fmt.Errorf("passphrase for %s: %w", path, err)
	}

	c.mu.Lock()
	if c.passphrases == nil {
		c.passphrases = make(map[string]string)
	}
	c.passphrases[path] = v
	c.mu.Unlock()

	return v, nil
}

func (c *Credentials) forgetPassphrase(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.passphrases, path)
}

// password returns the cached password for a login user, asking for it once.
//
// The cache is keyed by user rather than by host: the whole point of a cluster
// tool is that the same account exists on every machine, and asking forty times
// for one password would make the tool unusable.
func (c *Credentials) password(ctx context.Context, host hosts.Host) (string, error) {
	c.mu.Lock()
	if v, ok := c.passwords[host.User]; ok {
		c.mu.Unlock()
		return v, nil
	}
	prompter := c.Prompter
	c.mu.Unlock()

	if prompter == nil {
		return "", fmt.Errorf("password for %s: %w", host.Alias, ErrNoPrompter)
	}

	v, err := prompter.Password(ctx, host)
	if err != nil {
		return "", fmt.Errorf("password for %s: %w", host.Alias, err)
	}

	c.mu.Lock()
	if c.passwords == nil {
		c.passwords = make(map[string]string)
	}
	c.passwords[host.User] = v
	c.mu.Unlock()

	return v, nil
}

// ForgetPassword drops the cached password for a user, so a wrong answer can be
// corrected instead of failing every remaining host.
func (c *Credentials) ForgetPassword(user string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.passwords, user)
}

// answer responds to keyboard-interactive questions. A single question that
// looks like a password prompt is served from the password cache, which is what
// makes PAM-based servers behave like password servers.
func (c *Credentials) answer(ctx context.Context, host hosts.Host, questions []string, echos []bool) ([]string, error) {
	if len(questions) == 0 {
		return nil, nil
	}

	if len(questions) == 1 && !echoAt(echos, 0) && looksLikePasswordPrompt(questions[0]) {
		v, err := c.password(ctx, host)
		if err != nil {
			return nil, err
		}
		return []string{v}, nil
	}

	if c.Prompter == nil {
		return nil, fmt.Errorf("keyboard-interactive for %s: %w", host.Alias, ErrNoPrompter)
	}

	answers := make([]string, len(questions))
	for i, q := range questions {
		v, err := c.Prompter.Question(ctx, host, q, echoAt(echos, i))
		if err != nil {
			return nil, fmt.Errorf("keyboard-interactive for %s: %w", host.Alias, err)
		}
		answers[i] = v
	}
	return answers, nil
}

func echoAt(echos []bool, i int) bool {
	if i < len(echos) {
		return echos[i]
	}
	return false
}

// looksLikePasswordPrompt recognises the usual PAM wording.
func looksLikePasswordPrompt(question string) bool {
	return strings.Contains(strings.ToLower(question), "password")
}

// FuncPrompter adapts plain functions to [Prompter], which is convenient for
// tests and for a non-interactive front end.
type FuncPrompter struct {
	PassphraseFunc func(ctx context.Context, host hosts.Host, keyPath string) (string, error)
	PasswordFunc   func(ctx context.Context, host hosts.Host) (string, error)
	QuestionFunc   func(ctx context.Context, host hosts.Host, question string, echo bool) (string, error)
}

func (p FuncPrompter) Passphrase(ctx context.Context, host hosts.Host, keyPath string) (string, error) {
	if p.PassphraseFunc == nil {
		return "", ErrNoPrompter
	}
	return p.PassphraseFunc(ctx, host, keyPath)
}

func (p FuncPrompter) Password(ctx context.Context, host hosts.Host) (string, error) {
	if p.PasswordFunc == nil {
		return "", ErrNoPrompter
	}
	return p.PasswordFunc(ctx, host)
}

func (p FuncPrompter) Question(ctx context.Context, host hosts.Host, question string, echo bool) (string, error) {
	if p.QuestionFunc == nil {
		return "", ErrNoPrompter
	}
	return p.QuestionFunc(ctx, host, question, echo)
}
