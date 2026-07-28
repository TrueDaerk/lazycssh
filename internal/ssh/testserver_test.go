package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
)

// testServer is an in-process SSH server on the loopback interface. It exists so
// that the real session implementation is exercised for real - handshake, PTY
// request, window change, shell, exit - without any test reaching the network.
type testServer struct {
	t        *testing.T
	listener net.Listener
	signer   ssh.Signer

	// Password is accepted for any user. Empty means password auth is refused.
	Password string
	// AuthorizedKey is accepted for any user. Nil means public key auth is
	// refused.
	AuthorizedKey ssh.PublicKey

	mu         sync.Mutex
	ptyTerm    string
	windowSize [2]int // width, height, latest window-change or pty-req
	sessions   int

	wg     sync.WaitGroup
	closed chan struct{}
}

// newTestServer starts a server and stops it when the test ends.
func newTestServer(t *testing.T) *testServer {
	t.Helper()

	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatalf("build signer: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	s := &testServer{
		t:        t,
		listener: listener,
		signer:   signer,
		Password: "hunter2",
		closed:   make(chan struct{}),
	}

	s.wg.Add(1)
	go s.acceptLoop()

	t.Cleanup(s.Close)
	return s
}

// Addr is the host the server listens on.
func (s *testServer) Addr() (host string, port int) {
	addr := s.listener.Addr().(*net.TCPAddr)
	return addr.IP.String(), addr.Port
}

// HostKeyCallback verifies exactly this server's key, which is what a client
// with a correct known_hosts entry would do.
func (s *testServer) HostKeyCallback() ssh.HostKeyCallback {
	return ssh.FixedHostKey(s.signer.PublicKey())
}

// WindowSize returns the size the server last saw.
func (s *testServer) WindowSize() (width, height int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.windowSize[0], s.windowSize[1]
}

// PtyTerm returns the TERM value of the last pty-req.
func (s *testServer) PtyTerm() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ptyTerm
}

func (s *testServer) Close() {
	select {
	case <-s.closed:
		return
	default:
	}
	close(s.closed)
	s.listener.Close()
	s.wg.Wait()
}

func (s *testServer) acceptLoop() {
	defer s.wg.Done()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConn(conn)
		}()
	}
}

func (s *testServer) handleConn(conn net.Conn) {
	defer conn.Close()

	config := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if s.Password == "" || string(password) != s.Password {
				return nil, errors.New("password rejected")
			}
			return nil, nil
		},
		PublicKeyCallback: func(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			s.mu.Lock()
			authorized := s.AuthorizedKey
			s.mu.Unlock()

			if authorized == nil {
				return nil, errors.New("public key auth disabled")
			}
			if string(key.Marshal()) != string(authorized.Marshal()) {
				return nil, errors.New("unknown public key")
			}
			return nil, nil
		},
	}
	config.AddHostKey(s.signer)

	serverConn, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return // Expected for the auth-failure and bad-host-key tests.
	}
	defer serverConn.Close()

	go ssh.DiscardRequests(reqs)

	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			newChannel.Reject(ssh.UnknownChannelType, "only sessions are supported")
			continue
		}
		channel, requests, err := newChannel.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.sessions++
		s.mu.Unlock()

		go s.handleSession(channel, requests)
	}
}

func (s *testServer) handleSession(channel ssh.Channel, requests <-chan *ssh.Request) {
	for req := range requests {
		switch req.Type {
		case "pty-req":
			var payload struct {
				Term                  string
				Width, Height         uint32
				WidthPixel, HeightPix uint32
				Modes                 string
			}
			if err := ssh.Unmarshal(req.Payload, &payload); err == nil {
				s.mu.Lock()
				s.ptyTerm = payload.Term
				s.windowSize = [2]int{int(payload.Width), int(payload.Height)}
				s.mu.Unlock()
			}
			req.Reply(true, nil)

		case "window-change":
			var payload struct {
				Width, Height         uint32
				WidthPixel, HeightPix uint32
			}
			if err := ssh.Unmarshal(req.Payload, &payload); err == nil {
				s.mu.Lock()
				s.windowSize = [2]int{int(payload.Width), int(payload.Height)}
				s.mu.Unlock()
			}
			req.Reply(true, nil)

		case "shell":
			req.Reply(true, nil)
			go s.runShell(channel)

		default:
			req.Reply(false, nil)
		}
	}
}

// runShell is a deliberately tiny shell: it echoes what it receives, answers a
// few commands the tests need, and exits on "exit".
func (s *testServer) runShell(channel ssh.Channel) {
	defer channel.Close()

	io.WriteString(channel, "welcome\r\n")

	buf := make([]byte, 1024)
	var line []byte
	for {
		n, err := channel.Read(buf)
		if n > 0 {
			line = append(line, buf[:n]...)
			// Echo the way a PTY does, translating the carriage return the
			// terminal sends into the CRLF the terminal displays.
			channel.Write(crlf(buf[:n]))
		}
		if err != nil {
			return
		}

		for {
			i := indexByte(line, '\r')
			if i < 0 {
				i = indexByte(line, '\n')
			}
			if i < 0 {
				break
			}
			command := string(line[:i])
			line = line[i+1:]

			switch command {
			case "exit":
				channel.Write([]byte("bye\r\n"))
				channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
				return
			case "fail":
				channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{3}))
				return
			case "stderr":
				channel.Stderr().Write([]byte("to stderr\r\n"))
			case "flood":
				for i := 0; i < 5000; i++ {
					channel.Write([]byte("flood line\r\n"))
				}
			default:
				channel.Write([]byte("\r\n"))
			}
		}
	}
}

// crlf expands a bare carriage return into CRLF, which is what the ONLCR
// terminal setting does on a real PTY.
func crlf(b []byte) []byte {
	out := make([]byte, 0, len(b)+8)
	for i := 0; i < len(b); i++ {
		out = append(out, b[i])
		if b[i] == '\r' && (i+1 >= len(b) || b[i+1] != '\n') {
			out = append(out, '\n')
		}
	}
	return out
}

func indexByte(b []byte, c byte) int {
	for i := range b {
		if b[i] == c {
			return i
		}
	}
	return -1
}

// passwordAuth is the auth method matching the test server's password.
func passwordAuth(s *testServer) []ssh.AuthMethod {
	return []ssh.AuthMethod{ssh.Password(s.Password)}
}
