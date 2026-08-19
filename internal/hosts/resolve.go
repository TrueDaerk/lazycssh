package hosts

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kevinburke/ssh_config"
)

// defaultPort is the SSH port used when neither the argument nor the config
// names one.
const defaultPort = 22

// Host is one machine to connect to, with everything needed to dial it already
// resolved.
type Host struct {
	// Alias is the name the user typed, after brace expansion and with any
	// user@ and :port stripped. It is what the UI displays, because that is
	// what the user recognises.
	Alias string
	// Addr is the address to dial, taken from a HostName directive when one
	// matched and from Alias otherwise.
	Addr string
	// User is the login user.
	User string
	// Port is the TCP port.
	Port int
	// IdentityFiles are the private keys to offer, in order, with ~ already
	// expanded. Empty means "let the auth layer decide".
	IdentityFiles []string
	// ProxyJump is the jump host specification, empty when there is none.
	ProxyJump string
}

// String renders the host the way ssh command lines do, which is what error
// messages and the Hosts panel should show.
func (h Host) String() string {
	s := h.Alias
	if h.User != "" {
		s = h.User + "@" + s
	}
	if h.Port != defaultPort {
		s += ":" + strconv.Itoa(h.Port)
	}
	return s
}

// Resolver turns host arguments into [Host] values, consulting an OpenSSH
// client config for anything the argument does not state.
//
// The zero Resolver is usable: it resolves nothing from a config and falls back
// to the process defaults.
type Resolver struct {
	// Config is the parsed OpenSSH client config, or nil when none was found.
	Config *ssh_config.Config
	// DefaultUser is used when neither the argument nor the config names one.
	// Empty means the current OS user, looked up on first use.
	DefaultUser string
}

// LoadConfig parses an OpenSSH client config. A missing file is not an error:
// most users have no ~/.ssh/config and lazycssh works without one.
func LoadConfig(path string) (*ssh_config.Config, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open ssh config %s: %w", path, err)
	}
	defer f.Close()

	cfg, err := ssh_config.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("parse ssh config %s: %w", path, err)
	}
	return cfg, nil
}

// UserConfigPath returns the path of the current user's OpenSSH client config.
func UserConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}
	return filepath.Join(home, ".ssh", "config"), nil
}

// NewResolver loads the current user's ~/.ssh/config and returns a Resolver
// using it. A missing config yields a working Resolver, not an error.
func NewResolver() (*Resolver, error) {
	path, err := UserConfigPath()
	if err != nil {
		return nil, err
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		return nil, err
	}
	return &Resolver{Config: cfg}, nil
}

// ResolveAll expands every argument and resolves the results, preserving order.
func (r *Resolver) ResolveAll(args []string) ([]Host, error) {
	expanded, err := ExpandAll(args)
	if err != nil {
		return nil, err
	}

	out := make([]Host, 0, len(expanded))
	for _, arg := range expanded {
		h, err := r.Resolve(arg)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, nil
}

// Resolve turns a single expanded argument into a [Host].
//
// The argument may carry an explicit user and port, `root@srv1.example.com:2222`.
// Anything stated on the command line wins over the config, which in turn wins
// over the built-in defaults — the same precedence ssh itself applies.
func (r *Resolver) Resolve(arg string) (Host, error) {
	alias, cliUser, cliPort, err := splitTarget(arg)
	if err != nil {
		return Host{}, err
	}

	h := Host{Alias: alias, User: cliUser, Port: cliPort}

	h.Addr = r.lookup(alias, "HostName")
	if h.Addr == "" {
		h.Addr = alias
	} else {
		h.Addr, err = expandHostNameTokens(h.Addr, alias)
		if err != nil {
			return Host{}, err
		}
	}

	if h.User == "" {
		h.User = r.lookup(alias, "User")
	}
	if h.User == "" {
		h.User, err = r.defaultUser()
		if err != nil {
			return Host{}, err
		}
	}

	if h.Port == 0 {
		port := r.lookup(alias, "Port")
		if port != "" {
			n, err := strconv.Atoi(port)
			if err != nil || n < 1 || n > 65535 {
				return Host{}, fmt.Errorf("ssh config for %q: Port %q is not a valid port number", alias, port)
			}
			h.Port = n
		}
	}
	if h.Port == 0 {
		h.Port = defaultPort
	}

	h.IdentityFiles = r.lookupAll(alias, "IdentityFile")
	for i, path := range h.IdentityFiles {
		h.IdentityFiles[i] = expandTilde(path)
	}

	h.ProxyJump = r.lookup(alias, "ProxyJump")
	if strings.EqualFold(h.ProxyJump, "none") {
		h.ProxyJump = ""
	}

	return h, nil
}

// lookup reads one key for an alias from the config, ignoring the library's
// built-in defaults: those are applied here, so that "unset" stays
// distinguishable from "set to the default".
func (r *Resolver) lookup(alias, key string) string {
	if r == nil || r.Config == nil {
		return ""
	}
	v, err := r.Config.Get(alias, key)
	if err != nil {
		return ""
	}
	return v
}

// lookupAll reads a repeatable key, such as IdentityFile.
func (r *Resolver) lookupAll(alias, key string) []string {
	if r == nil || r.Config == nil {
		return nil
	}
	vs, err := r.Config.GetAll(alias, key)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		if v != "" {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// defaultUser returns the configured fallback user, or the current OS user.
func (r *Resolver) defaultUser() (string, error) {
	if r != nil && r.DefaultUser != "" {
		return r.DefaultUser, nil
	}
	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("determine the current user: %w", err)
	}
	return u.Username, nil
}

// splitTarget parses the [user@]host[:port] form. The host part may be a
// bracketed IPv6 address, [::1]:22, which is the only way a colon in the host
// itself is unambiguous.
func splitTarget(arg string) (host, user string, port int, err error) {
	if arg == "" {
		return "", "", 0, fmt.Errorf("empty host argument")
	}

	rest := arg
	if i := strings.LastIndex(rest, "@"); i >= 0 {
		user, rest = rest[:i], rest[i+1:]
		if user == "" {
			return "", "", 0, fmt.Errorf("host %q: empty user before '@'", arg)
		}
		if rest == "" {
			return "", "", 0, fmt.Errorf("host %q: nothing after '@'", arg)
		}
	}

	if strings.HasPrefix(rest, "[") {
		end := strings.Index(rest, "]")
		if end < 0 {
			return "", "", 0, fmt.Errorf("host %q: missing ']' after the IPv6 address", arg)
		}
		host = rest[1:end]
		tail := rest[end+1:]
		switch {
		case tail == "":
			return host, user, 0, nil
		case strings.HasPrefix(tail, ":"):
			port, err = parsePort(arg, tail[1:])
			return host, user, port, err
		default:
			return "", "", 0, fmt.Errorf("host %q: unexpected %q after the IPv6 address", arg, tail)
		}
	}

	if i := strings.LastIndex(rest, ":"); i >= 0 {
		// More than one colon and no brackets means a bare IPv6 address, which
		// cannot carry a port.
		if strings.Count(rest, ":") > 1 {
			return rest, user, 0, nil
		}
		host = rest[:i]
		if host == "" {
			return "", "", 0, fmt.Errorf("host %q: empty host before the port", arg)
		}
		port, err = parsePort(arg, rest[i+1:])
		return host, user, port, err
	}

	return rest, user, 0, nil
}

func parsePort(arg, s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 65535 {
		return 0, fmt.Errorf("host %q: %q is not a valid port number", arg, s)
	}
	return n, nil
}

// expandHostNameTokens expands the percent tokens ssh_config(5) allows in a
// HostName directive: %h is the alias as given before any substitution, and
// %% is a literal percent sign. Any other %-token is an error, matching
// OpenSSH's own "percent_expand: unknown key" behaviour rather than passing
// the raw token through to a failing DNS lookup.
func expandHostNameTokens(hostname, alias string) (string, error) {
	if !strings.Contains(hostname, "%") {
		return hostname, nil
	}

	var b strings.Builder
	for i := 0; i < len(hostname); i++ {
		c := hostname[i]
		if c != '%' {
			b.WriteByte(c)
			continue
		}
		i++
		if i >= len(hostname) {
			return "", fmt.Errorf("ssh config for %q: HostName %q ends with a dangling '%%'", alias, hostname)
		}
		switch hostname[i] {
		case 'h':
			b.WriteString(alias)
		case '%':
			b.WriteByte('%')
		default:
			return "", fmt.Errorf("ssh config for %q: HostName %q has unknown token %%%c", alias, hostname, hostname[i])
		}
	}
	return b.String(), nil
}

// expandTilde resolves a leading ~/ against the current home directory, which
// ssh config files use routinely. A path that cannot be resolved is returned
// unchanged rather than failing the whole run.
func expandTilde(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, path[2:])
}
