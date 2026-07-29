package hosts

import "strings"

// Aliases returns the concrete host aliases the resolver's OpenSSH config
// declares, in file order, deduplicated.
//
// Only patterns that name one host are candidates: wildcard patterns (`*`,
// `web-?`) match rather than name, and negated patterns (`!bastion`) exclude.
// There is no reverse expansion of a glob into the hosts it would match - the
// config does not know them either.
//
// A nil resolver or a resolver without a config returns nil, which is what a
// machine without ~/.ssh/config has to offer.
func (r *Resolver) Aliases() []string {
	if r == nil || r.Config == nil {
		return nil
	}

	var out []string
	seen := make(map[string]bool)
	for _, host := range r.Config.Hosts {
		for _, pattern := range host.Patterns {
			alias := pattern.String()
			if alias == "" || seen[alias] {
				continue
			}
			if strings.ContainsAny(alias, "*?") || strings.HasPrefix(alias, "!") {
				continue
			}
			seen[alias] = true
			out = append(out, alias)
		}
	}
	return out
}
