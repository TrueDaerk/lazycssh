package ssh

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/TrueDaerk/lazycssh/internal/hosts"
)

// The secrets used throughout these tests. They are distinctive so that a leak
// into an error string or a rendered view is unmistakable.
const (
	testPassword   = "correct-horse-battery-staple"
	testPassphrase = "unlock-me-please-42"
)

func authHost(alias string, identityFiles ...string) hosts.Host {
	return hosts.Host{
		Alias: alias, Addr: alias + ".example.com", User: "deploy", Port: 22,
		IdentityFiles: identityFiles,
	}
}

// writeKey writes an ed25519 private key, optionally encrypted, and returns its
// path and public key.
func writeKey(t *testing.T, passphrase string) (path string, pub ssh.PublicKey) {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	var block *pem.Block
	if passphrase == "" {
		block, err = ssh.MarshalPrivateKey(priv, "")
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte(passphrase))
	}
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}

	path = filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("build signer: %v", err)
	}
	return path, signer.PublicKey()
}

func TestCredentialsPromptsOncePerUser(t *testing.T) {
	var calls atomic.Int32
	creds := &Credentials{
		DisableAgent: true,
		Prompter: FuncPrompter{
			PasswordFunc: func(context.Context, hosts.Host) (string, error) {
				calls.Add(1)
				return testPassword, nil
			},
		},
	}

	// Forty hosts, one account: the user must be asked exactly once.
	for i := 0; i < 40; i++ {
		host := authHost("srv" + string(rune('a'+i%26)))
		got, err := creds.password(t.Context(), host)
		if err != nil {
			t.Fatalf("password for %s: %v", host.Alias, err)
		}
		if got != testPassword {
			t.Fatalf("password = %q, want the answered value", got)
		}
	}

	if got := calls.Load(); got != 1 {
		t.Errorf("the user was asked %d times, want exactly 1: forty prompts for one password makes the tool unusable", got)
	}
}

func TestCredentialsPasswordCacheIsPerUser(t *testing.T) {
	var asked []string
	var mu sync.Mutex

	creds := &Credentials{
		DisableAgent: true,
		Prompter: FuncPrompter{
			PasswordFunc: func(_ context.Context, h hosts.Host) (string, error) {
				mu.Lock()
				defer mu.Unlock()
				asked = append(asked, h.User)
				return "secret-for-" + h.User, nil
			},
		},
	}

	root := hosts.Host{Alias: "a", User: "root"}
	deploy := hosts.Host{Alias: "b", User: "deploy"}

	for _, h := range []hosts.Host{root, deploy, root, deploy} {
		if _, err := creds.password(t.Context(), h); err != nil {
			t.Fatalf("password: %v", err)
		}
	}

	if len(asked) != 2 {
		t.Errorf("asked %v, want one prompt per distinct user", asked)
	}
}

func TestCredentialsPromptsConcurrentlyWithoutDuplicating(t *testing.T) {
	var calls atomic.Int32
	creds := &Credentials{
		DisableAgent: true,
		Prompter: FuncPrompter{
			PasswordFunc: func(context.Context, hosts.Host) (string, error) {
				calls.Add(1)
				time.Sleep(10 * time.Millisecond)
				return testPassword, nil
			},
		},
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			creds.password(context.Background(), authHost("srv"))
		}()
	}
	wg.Wait()

	// Twenty sessions dialling at once may race into the prompt, but the cache
	// must converge: every later caller is served from it.
	if got := calls.Load(); got > 20 {
		t.Errorf("prompted %d times for 20 concurrent sessions", got)
	}
	if _, err := creds.password(t.Context(), authHost("srv")); err != nil {
		t.Fatalf("password after the race: %v", err)
	}
	before := calls.Load()
	creds.password(t.Context(), authHost("srv"))
	if got := calls.Load(); got != before {
		t.Errorf("a settled cache still prompted: %d -> %d", before, got)
	}
}

func TestCredentialsForgetPassword(t *testing.T) {
	var calls atomic.Int32
	creds := &Credentials{
		DisableAgent: true,
		Prompter: FuncPrompter{
			PasswordFunc: func(context.Context, hosts.Host) (string, error) {
				calls.Add(1)
				return testPassword, nil
			},
		},
	}
	host := authHost("srv")

	creds.password(t.Context(), host)
	creds.ForgetPassword(host.User)
	creds.password(t.Context(), host)

	if got := calls.Load(); got != 2 {
		t.Errorf("prompted %d times, want 2: a wrong answer must be correctable", got)
	}
}

func TestCredentialsLoadsEncryptedIdentityOncePerFile(t *testing.T) {
	keyPath, _ := writeKey(t, testPassphrase)

	var calls atomic.Int32
	creds := &Credentials{
		DisableAgent: true,
		Prompter: FuncPrompter{
			PassphraseFunc: func(_ context.Context, _ hosts.Host, path string) (string, error) {
				calls.Add(1)
				if path != keyPath {
					t.Errorf("asked for %q, want the key path %q", path, keyPath)
				}
				return testPassphrase, nil
			},
		},
	}

	for i := 0; i < 5; i++ {
		signers, err := creds.identitySigners(t.Context(), authHost("srv", keyPath))
		if err != nil {
			t.Fatalf("identitySigners: %v", err)
		}
		if len(signers) != 1 {
			t.Fatalf("got %d signers, want 1", len(signers))
		}
	}

	if got := calls.Load(); got != 1 {
		t.Errorf("asked for the passphrase %d times, want once per key file", got)
	}
}

func TestCredentialsUnencryptedIdentityNeedsNoPrompt(t *testing.T) {
	keyPath, _ := writeKey(t, "")

	creds := &Credentials{
		DisableAgent: true,
		Prompter: FuncPrompter{
			PassphraseFunc: func(context.Context, hosts.Host, string) (string, error) {
				t.Error("an unencrypted key must not produce a passphrase prompt")
				return "", errors.New("unexpected prompt")
			},
		},
	}

	signers, err := creds.identitySigners(t.Context(), authHost("srv", keyPath))
	if err != nil {
		t.Fatalf("identitySigners: %v", err)
	}
	if len(signers) != 1 {
		t.Errorf("got %d signers, want 1", len(signers))
	}
}

func TestCredentialsWrongPassphraseIsForgotten(t *testing.T) {
	keyPath, _ := writeKey(t, testPassphrase)

	var calls atomic.Int32
	creds := &Credentials{
		DisableAgent: true,
		Prompter: FuncPrompter{
			PassphraseFunc: func(context.Context, hosts.Host, string) (string, error) {
				if calls.Add(1) == 1 {
					return "wrong-on-purpose", nil
				}
				return testPassphrase, nil
			},
		},
	}
	host := authHost("srv", keyPath)

	if _, err := creds.identitySigners(t.Context(), host); err == nil {
		t.Fatal("a wrong passphrase produced no error")
	}

	// The second attempt must ask again rather than reuse the wrong answer for
	// every remaining host.
	signers, err := creds.identitySigners(t.Context(), host)
	if err != nil {
		t.Fatalf("second attempt: %v", err)
	}
	if len(signers) != 1 {
		t.Fatalf("got %d signers, want 1", len(signers))
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("prompted %d times, want 2", got)
	}
}

func TestCredentialsMissingIdentityIsNotFatalWhenAnotherWorks(t *testing.T) {
	keyPath, _ := writeKey(t, "")
	creds := &Credentials{DisableAgent: true}

	host := authHost("srv", filepath.Join(t.TempDir(), "absent"), keyPath)

	signers, err := creds.identitySigners(t.Context(), host)
	if err != nil {
		t.Fatalf("identitySigners: %v", err)
	}
	if len(signers) != 1 {
		t.Errorf("got %d signers, want the one usable key", len(signers))
	}
}

func TestCredentialsWithoutPrompter(t *testing.T) {
	keyPath, _ := writeKey(t, testPassphrase)
	creds := &Credentials{DisableAgent: true}

	if _, err := creds.password(t.Context(), authHost("srv")); !errors.Is(err, ErrNoPrompter) {
		t.Errorf("password error = %v, want ErrNoPrompter", err)
	}
	if _, err := creds.identitySigners(t.Context(), authHost("srv", keyPath)); !errors.Is(err, ErrNoPrompter) {
		t.Errorf("identity error = %v, want ErrNoPrompter", err)
	}
}

func TestCredentialsKeyboardInteractiveReusesThePassword(t *testing.T) {
	var passwordCalls, questionCalls atomic.Int32
	creds := &Credentials{
		DisableAgent: true,
		Prompter: FuncPrompter{
			PasswordFunc: func(context.Context, hosts.Host) (string, error) {
				passwordCalls.Add(1)
				return testPassword, nil
			},
			QuestionFunc: func(_ context.Context, _ hosts.Host, q string, _ bool) (string, error) {
				questionCalls.Add(1)
				return "answer to " + q, nil
			},
		},
	}
	host := authHost("srv")

	// A PAM server asking the usual single question is served from the password
	// cache, so a password host and a PAM host behave the same.
	answers, err := creds.answer(t.Context(), host, []string{"Password: "}, []bool{false})
	if err != nil {
		t.Fatalf("answer: %v", err)
	}
	if len(answers) != 1 || answers[0] != testPassword {
		t.Errorf("answers = %q, want the cached password", answers)
	}
	if got := questionCalls.Load(); got != 0 {
		t.Errorf("the generic question prompt was used %d times for a password question", got)
	}

	// Anything else is a real question.
	answers, err = creds.answer(t.Context(), host, []string{"Verification code: "}, []bool{false})
	if err != nil {
		t.Fatalf("answer: %v", err)
	}
	if len(answers) != 1 || !strings.Contains(answers[0], "Verification code") {
		t.Errorf("answers = %q, want the question to have been asked", answers)
	}
	if got := passwordCalls.Load(); got != 1 {
		t.Errorf("password prompted %d times, want 1", got)
	}
}

func TestCredentialsAnswersEveryQuestion(t *testing.T) {
	creds := &Credentials{
		DisableAgent: true,
		Prompter: FuncPrompter{
			QuestionFunc: func(_ context.Context, _ hosts.Host, q string, echo bool) (string, error) {
				if q == "Username: " && !echo {
					t.Error("an echoing question was reported as hidden")
				}
				return "a:" + q, nil
			},
		},
	}

	answers, err := creds.answer(t.Context(), authHost("srv"),
		[]string{"Username: ", "Token: "}, []bool{true, false})
	if err != nil {
		t.Fatalf("answer: %v", err)
	}
	if len(answers) != 2 {
		t.Fatalf("got %d answers for 2 questions", len(answers))
	}
}

func TestMethodOrder(t *testing.T) {
	keyPath, _ := writeKey(t, "")

	t.Run("agent comes first when one is available", func(t *testing.T) {
		creds := &Credentials{AgentSocket: filepath.Join(t.TempDir(), "agent.sock")}
		assertMethodOrder(t, creds, authHost("srv", keyPath),
			MethodAgent, MethodPublicKey, MethodPassword, MethodKeyboardInteractive)
	})

	t.Run("no agent, no agent method", func(t *testing.T) {
		creds := &Credentials{DisableAgent: true}
		assertMethodOrder(t, creds, authHost("srv", keyPath),
			MethodPublicKey, MethodPassword, MethodKeyboardInteractive)
	})

	t.Run("no identity files, no public key method", func(t *testing.T) {
		creds := &Credentials{DisableAgent: true}
		assertMethodOrder(t, creds, authHost("srv"), MethodPassword, MethodKeyboardInteractive)
	})
}

// assertMethodOrder checks both the reported names and that the same number of
// real methods is produced, so the two cannot drift apart.
func assertMethodOrder(t *testing.T, creds *Credentials, host hosts.Host, want ...string) {
	t.Helper()

	names := creds.MethodNames(host)
	if len(names) != len(want) {
		t.Fatalf("MethodNames = %q, want %q", names, want)
	}
	for i := range names {
		if names[i] != want[i] {
			t.Errorf("method %d is %q, want %q", i, names[i], want[i])
		}
	}
	if got := len(creds.Methods(t.Context(), "s1", host)); got != len(want) {
		t.Errorf("Methods returned %d methods but MethodNames reported %d", got, len(want))
	}
}

// TestNoSecretEverAppearsInAnError is the security guarantee: a credential must
// not reach a log line, an error string or a rendered view.
func TestNoSecretEverAppearsInAnError(t *testing.T) {
	keyPath, _ := writeKey(t, testPassphrase)

	creds := &Credentials{
		DisableAgent: true,
		Prompter: FuncPrompter{
			PassphraseFunc: func(context.Context, hosts.Host, string) (string, error) { return "wrong-" + testPassphrase, nil },
			PasswordFunc:   func(context.Context, hosts.Host) (string, error) { return testPassword, nil },
			QuestionFunc: func(context.Context, hosts.Host, string, bool) (string, error) {
				return testPassword, nil
			},
		},
	}
	host := authHost("srv", keyPath)

	var messages []string

	// Every path that can fail after a secret has been handled.
	if _, err := creds.identitySigners(t.Context(), host); err != nil {
		messages = append(messages, err.Error())
	}
	if _, err := creds.identitySigners(t.Context(), authHost("srv", filepath.Join(t.TempDir(), "absent"))); err != nil {
		messages = append(messages, err.Error())
	}

	// A real handshake that fails because the password is wrong for this server.
	srv := newTestServer(t)
	srv.Password = "something-else"
	addr, port := srv.Addr()
	h := hosts.Host{Alias: "test-host", Addr: addr, User: "deploy", Port: port}

	s := New("s1", Config{
		Host:            h,
		Auth:            creds.Methods(t.Context(), "s1", h),
		HostKeyCallback: srv.HostKeyCallback(),
		Timeout:         5 * time.Second,
	}, nil)
	t.Cleanup(func() { s.Close() })

	if err := s.Start(t.Context()); err != nil {
		messages = append(messages, err.Error())
	}
	if err := s.Err(); err != nil {
		messages = append(messages, err.Error())
	}
	messages = append(messages, s.Scrollback().String())

	for _, secret := range []string{testPassword, testPassphrase} {
		for _, msg := range messages {
			if strings.Contains(msg, secret) {
				t.Errorf("a secret leaked into %q", msg)
			}
		}
	}
	if len(messages) < 3 {
		t.Fatalf("only %d messages were collected; the test is not exercising the failure paths", len(messages))
	}
}

// TestAuthAgainstServer exercises the whole chain against the in-process server.
func TestAuthAgainstServer(t *testing.T) {
	t.Run("password", func(t *testing.T) {
		srv := newTestServer(t)
		creds := &Credentials{
			DisableAgent: true,
			Prompter: FuncPrompter{
				PasswordFunc: func(context.Context, hosts.Host) (string, error) { return srv.Password, nil },
			},
		}
		connectWith(t, srv, creds, nil)

		if got := creds.LastMethod("s1"); got != MethodPassword {
			t.Errorf("LastMethod = %q, want %q", got, MethodPassword)
		}
	})

	t.Run("encrypted identity file", func(t *testing.T) {
		srv := newTestServer(t)
		keyPath, pub := writeKey(t, testPassphrase)
		srv.mu.Lock()
		srv.AuthorizedKey = pub
		srv.mu.Unlock()
		srv.Password = "" // only the key may work

		var prompts atomic.Int32
		creds := &Credentials{
			DisableAgent: true,
			Prompter: FuncPrompter{
				PassphraseFunc: func(context.Context, hosts.Host, string) (string, error) {
					prompts.Add(1)
					return testPassphrase, nil
				},
			},
		}
		connectWith(t, srv, creds, []string{keyPath})

		if got := prompts.Load(); got != 1 {
			t.Errorf("asked for the passphrase %d times, want 1", got)
		}
		if got := creds.LastMethod("s1"); got != MethodPublicKey {
			t.Errorf("LastMethod = %q, want %q", got, MethodPublicKey)
		}
	})
}

// connectWith dials the test server using the given credentials and asserts the
// session reaches a running shell.
func connectWith(t *testing.T, srv *testServer, creds *Credentials, identityFiles []string) {
	t.Helper()

	addr, port := srv.Addr()
	host := hosts.Host{
		Alias: "test-host", Addr: addr, User: "deploy", Port: port,
		IdentityFiles: identityFiles,
	}

	s := New("s1", Config{
		Host:            host,
		Auth:            creds.Methods(t.Context(), "s1", host),
		HostKeyCallback: srv.HostKeyCallback(),
		Timeout:         5 * time.Second,
	}, nil)
	t.Cleanup(func() { s.Close() })

	if err := s.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForOutput(t, s, "welcome")
}
