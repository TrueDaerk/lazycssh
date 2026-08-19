package hosts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestResolver builds a resolver over the fixture config, never over the
// developer's real ~/.ssh/config.
func newTestResolver(t *testing.T) *Resolver {
	t.Helper()

	cfg, err := LoadConfig(filepath.Join("testdata", "ssh_config"))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadConfig returned no config for the fixture file")
	}
	return &Resolver{Config: cfg, DefaultUser: "testuser"}
}

func TestResolve(t *testing.T) {
	r := newTestResolver(t)

	tests := []struct {
		name string
		arg  string
		want Host
	}{
		{
			name: "alias resolves HostName, User and Port",
			arg:  "bastion",
			want: Host{
				Alias: "bastion", Addr: "bastion.example.com", User: "ops", Port: 2222,
				IdentityFiles: []string{expandTilde("~/.ssh/id_bastion")},
			},
		},
		{
			name: "glob pattern matches",
			arg:  "web-2",
			want: Host{Alias: "web-2", Addr: "web-2", User: "deploy", Port: 2200, ProxyJump: "bastion"},
		},
		{
			name: "first matching block wins, later blocks still contribute",
			arg:  "web-1",
			want: Host{Alias: "web-1", Addr: "10.0.0.1", User: "deploy", Port: 2200, ProxyJump: "bastion"},
		},
		{
			name: "host with no HostName dials its own name",
			arg:  "db.example.com",
			want: Host{Alias: "db.example.com", Addr: "db.example.com", User: "postgres", Port: 22},
		},
		{
			name: "unknown host falls back to the Host * block",
			arg:  "unknown.example.com",
			want: Host{Alias: "unknown.example.com", Addr: "unknown.example.com", User: "fallback", Port: 22},
		},
		{
			name: "explicit user beats the config",
			arg:  "root@bastion",
			want: Host{
				Alias: "bastion", Addr: "bastion.example.com", User: "root", Port: 2222,
				IdentityFiles: []string{expandTilde("~/.ssh/id_bastion")},
			},
		},
		{
			name: "explicit port beats the config",
			arg:  "bastion:22",
			want: Host{
				Alias: "bastion", Addr: "bastion.example.com", User: "ops", Port: 22,
				IdentityFiles: []string{expandTilde("~/.ssh/id_bastion")},
			},
		},
		{
			name: "explicit user and port together",
			arg:  "root@web-2:2022",
			want: Host{Alias: "web-2", Addr: "web-2", User: "root", Port: 2022, ProxyJump: "bastion"},
		},
		{
			name: "repeated IdentityFile keeps order and expands ~",
			arg:  "multikey",
			want: Host{
				Alias: "multikey", Addr: "multikey", User: "fallback", Port: 22,
				IdentityFiles: []string{expandTilde("~/.ssh/id_one"), "/etc/ssh/id_two"},
			},
		},
		{
			name: "ProxyJump none means no jump host",
			arg:  "noproxy",
			want: Host{Alias: "noproxy", Addr: "noproxy", User: "fallback", Port: 22},
		},
		{
			name: "bare IPv4 address",
			arg:  "10.1.2.3",
			want: Host{Alias: "10.1.2.3", Addr: "10.1.2.3", User: "fallback", Port: 22},
		},
		{
			name: "bracketed IPv6 address with a port",
			arg:  "[2001:db8::1]:2222",
			want: Host{Alias: "2001:db8::1", Addr: "2001:db8::1", User: "fallback", Port: 2222},
		},
		{
			name: "bare IPv6 address without a port",
			arg:  "2001:db8::1",
			want: Host{Alias: "2001:db8::1", Addr: "2001:db8::1", User: "fallback", Port: 22},
		},
		{
			name: "%h token expands to the alias before substitution",
			arg:  "fs01.x",
			want: Host{Alias: "fs01.x", Addr: "fs01.x.example.com", User: "fallback", Port: 22},
		},
		{
			name: "%% expands to a literal percent sign",
			arg:  "literalpercent",
			want: Host{Alias: "literalpercent", Addr: "host%name.example.com", User: "fallback", Port: 22},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.Resolve(tt.arg)
			if err != nil {
				t.Fatalf("Resolve(%q) returned error: %v", tt.arg, err)
			}
			assertHost(t, tt.arg, got, tt.want)
		})
	}
}

func TestResolveRejectsMalformedTargets(t *testing.T) {
	r := newTestResolver(t)

	tests := []struct {
		name    string
		arg     string
		wantMsg string
	}{
		{name: "empty argument", arg: "", wantMsg: "empty host argument"},
		{name: "empty user", arg: "@host", wantMsg: "empty user"},
		{name: "nothing after at", arg: "user@", wantMsg: "nothing after"},
		{name: "empty host before port", arg: ":22", wantMsg: "empty host"},
		{name: "non numeric port", arg: "host:ssh", wantMsg: "not a valid port"},
		{name: "port out of range", arg: "host:70000", wantMsg: "not a valid port"},
		{name: "zero port", arg: "host:0", wantMsg: "not a valid port"},
		{name: "unclosed IPv6 bracket", arg: "[2001:db8::1", wantMsg: "missing ']'"},
		{name: "junk after IPv6 bracket", arg: "[2001:db8::1]x", wantMsg: "unexpected"},
		{name: "invalid port in the config", arg: "badport", wantMsg: "not a valid port number"},
		{name: "unknown percent token in HostName", arg: "badtoken", wantMsg: "unknown token %x"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.Resolve(tt.arg)
			if err == nil {
				t.Fatalf("Resolve(%q) = %+v, want an error", tt.arg, got)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantMsg)
			}
		})
	}
}

func TestResolveWithoutConfig(t *testing.T) {
	// A user with no ~/.ssh/config must still be able to connect.
	r := &Resolver{DefaultUser: "testuser"}

	got, err := r.Resolve("srv1.example.com")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	assertHost(t, "srv1.example.com", got, Host{
		Alias: "srv1.example.com", Addr: "srv1.example.com", User: "testuser", Port: 22,
	})
}

func TestLoadConfig(t *testing.T) {
	t.Run("a missing file is not an error", func(t *testing.T) {
		cfg, err := LoadConfig(filepath.Join(t.TempDir(), "does-not-exist"))
		if err != nil {
			t.Fatalf("LoadConfig on a missing file returned error: %v", err)
		}
		if cfg != nil {
			t.Errorf("LoadConfig on a missing file returned a config, want nil")
		}
	})

	t.Run("an unreadable file is an error naming the path", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("running as root, which ignores file permissions")
		}

		path := filepath.Join(t.TempDir(), "unreadable")
		if err := os.WriteFile(path, []byte("Host x\n"), 0o000); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		if _, err := LoadConfig(path); err == nil {
			t.Error("LoadConfig on an unreadable file returned no error")
		} else if !strings.Contains(err.Error(), path) {
			t.Errorf("error = %q, want it to name the path %q", err, path)
		}
	})

	t.Run("an empty file yields a usable config", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "empty")
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		cfg, err := LoadConfig(path)
		if err != nil {
			t.Fatalf("LoadConfig on an empty file returned error: %v", err)
		}
		r := &Resolver{Config: cfg, DefaultUser: "testuser"}
		got, err := r.Resolve("srv1")
		if err != nil {
			t.Fatalf("Resolve returned error: %v", err)
		}
		assertHost(t, "srv1", got, Host{Alias: "srv1", Addr: "srv1", User: "testuser", Port: 22})
	})
}

func TestResolveAll(t *testing.T) {
	r := newTestResolver(t)

	t.Run("expands and resolves in order", func(t *testing.T) {
		got, err := r.ResolveAll([]string{"web-{1..2}", "bastion"})
		if err != nil {
			t.Fatalf("ResolveAll returned error: %v", err)
		}
		wantAliases := []string{"web-1", "web-2", "bastion"}
		if len(got) != len(wantAliases) {
			t.Fatalf("ResolveAll returned %d hosts, want %d", len(got), len(wantAliases))
		}
		for i, want := range wantAliases {
			if got[i].Alias != want {
				t.Errorf("host %d alias = %q, want %q", i, got[i].Alias, want)
			}
		}
		// The alias is what the user typed; the address is what gets dialled.
		if got[0].Addr != "10.0.0.1" {
			t.Errorf("web-1 Addr = %q, want the HostName from the config", got[0].Addr)
		}
	})

	t.Run("a bad pattern fails before anything is resolved", func(t *testing.T) {
		if got, err := r.ResolveAll([]string{"web-1", "bad{"}); err == nil {
			t.Fatalf("ResolveAll = %+v, want an error", got)
		}
	})
}

func TestHostString(t *testing.T) {
	tests := []struct {
		name string
		host Host
		want string
	}{
		{
			name: "default port is omitted",
			host: Host{Alias: "srv1", User: "root", Port: 22},
			want: "root@srv1",
		},
		{
			name: "non default port is shown",
			host: Host{Alias: "srv1", User: "root", Port: 2222},
			want: "root@srv1:2222",
		},
		{
			name: "the alias is shown, not the address",
			host: Host{Alias: "bastion", Addr: "10.0.0.1", User: "ops", Port: 22},
			want: "ops@bastion",
		},
		{
			name: "no user",
			host: Host{Alias: "srv1", Port: 22},
			want: "srv1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.host.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func assertHost(t *testing.T, arg string, got, want Host) {
	t.Helper()

	if got.Alias != want.Alias {
		t.Errorf("Resolve(%q).Alias = %q, want %q", arg, got.Alias, want.Alias)
	}
	if got.Addr != want.Addr {
		t.Errorf("Resolve(%q).Addr = %q, want %q", arg, got.Addr, want.Addr)
	}
	if got.User != want.User {
		t.Errorf("Resolve(%q).User = %q, want %q", arg, got.User, want.User)
	}
	if got.Port != want.Port {
		t.Errorf("Resolve(%q).Port = %d, want %d", arg, got.Port, want.Port)
	}
	if got.ProxyJump != want.ProxyJump {
		t.Errorf("Resolve(%q).ProxyJump = %q, want %q", arg, got.ProxyJump, want.ProxyJump)
	}
	if len(got.IdentityFiles) != len(want.IdentityFiles) {
		t.Fatalf("Resolve(%q).IdentityFiles = %q, want %q", arg, got.IdentityFiles, want.IdentityFiles)
	}
	for i := range got.IdentityFiles {
		if got.IdentityFiles[i] != want.IdentityFiles[i] {
			t.Errorf("Resolve(%q).IdentityFiles[%d] = %q, want %q", arg, i, got.IdentityFiles[i], want.IdentityFiles[i])
		}
	}
}
