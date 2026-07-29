package hosts

import (
	"strings"
	"testing"

	ssh_config "github.com/kevinburke/ssh_config"
)

func aliasResolver(t *testing.T, config string) *Resolver {
	t.Helper()
	cfg, err := ssh_config.Decode(strings.NewReader(config))
	if err != nil {
		t.Fatalf("decode config: %v", err)
	}
	return &Resolver{Config: cfg}
}

func TestAliasesListsConcreteHosts(t *testing.T) {
	r := aliasResolver(t, `
Host web-01
  HostName 10.0.0.1

Host db-01 db-02
  User admin

Host bastion
  Port 2222
`)
	got := strings.Join(r.Aliases(), ",")
	if got != "web-01,db-01,db-02,bastion" {
		t.Fatalf("Aliases() = %q, want file order", got)
	}
}

// Wildcards match rather than name a host, and negations exclude one; neither
// is a candidate to connect to.
func TestAliasesSkipsPatterns(t *testing.T) {
	r := aliasResolver(t, `
Host *
  ServerAliveInterval 60

Host web-*
  User deploy

Host web-? staging
  Port 22

Host !bastion prod-01
  User root
`)
	got := strings.Join(r.Aliases(), ",")
	if got != "staging,prod-01" {
		t.Fatalf("Aliases() = %q, want the concrete names only", got)
	}
}

func TestAliasesDeduplicates(t *testing.T) {
	r := aliasResolver(t, `
Host web-01
  User a

Host web-01
  Port 2200
`)
	got := r.Aliases()
	if len(got) != 1 || got[0] != "web-01" {
		t.Fatalf("Aliases() = %v, want one web-01", got)
	}
}

func TestAliasesWithoutAConfig(t *testing.T) {
	var nilResolver *Resolver
	if got := nilResolver.Aliases(); got != nil {
		t.Fatalf("nil resolver Aliases() = %v, want nil", got)
	}
	if got := (&Resolver{}).Aliases(); got != nil {
		t.Fatalf("zero resolver Aliases() = %v, want nil", got)
	}
}
