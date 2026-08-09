package ui

import (
	"slices"
	"strings"
)

// The host picker's item sources (issues #246, #254). The picker itself knows
// only [HostSource]; what a row *is* - an ssh-config alias, a saved group, a
// host this machine reached before - lives here, so a fourth source is a type
// in this file and nothing else.

// HostKind is where a picker row came from. It is what the tag column shows,
// and it is the merge order: an alias that is also a recent host stays the
// alias, because that is the row the user can reason about.
type HostKind int

const (
	// KindConfig is a concrete alias of ~/.ssh/config.
	KindConfig HostKind = iota
	// KindGroup is a saved group; picking it connects all of its hosts.
	KindGroup
	// KindRecent is a host connected to in an earlier run.
	KindRecent
)

// Tag is the three-letter origin marker the picker draws in front of a row.
// Three letters, one column, no colour needed: the tag survives a terminal
// without styling.
func (k HostKind) Tag() string {
	switch k {
	case KindGroup:
		return "grp"
	case KindRecent:
		return "rec"
	default:
		return "cfg"
	}
}

// PickerItem is one row of the host picker: what it is called, where it came
// from, and what connecting it means.
type PickerItem struct {
	// Name is both the row's label and its identity - two sources offering
	// the same Name are the same row. Group names carry the `@` prefix the
	// command line uses, so a group and a host of the same name are two rows
	// rather than a collision.
	Name string
	// Kind is the origin tag.
	Kind HostKind
	// Patterns are the host patterns connecting this row sends. Empty means
	// the name itself, which is every case but a group.
	Patterns []string
}

// connect is what enter sends for this row.
func (i PickerItem) connect() []string {
	if len(i.Patterns) > 0 {
		return slices.Clone(i.Patterns)
	}
	return []string{i.Name}
}

// HostSource supplies the host picker's candidates. It is an interface, not a
// slice, so the picker's item source can grow - known hosts, a discovery
// backend - without the picker changing.
type HostSource interface {
	// Items returns the rows to offer, in display order. The picker calls it
	// when it opens, not while rendering, so an implementation may do real
	// work: reading the group directory is one.
	Items() []PickerItem
}

// RecentHosts is the recent-host list as the picker needs it: read only, most
// recent first. It is the subset of [recent.Store] the interface uses,
// declared here so this package stays off the disk in tests.
type RecentHosts interface {
	// Load returns the hosts, most recent first.
	Load() ([]string, error)
}

// aliasSource offers the concrete ssh-config aliases the program already
// resolved for the new-host prompt's completion.
type aliasSource []string

// Items tags every alias `cfg`.
func (s aliasSource) Items() []PickerItem {
	items := make([]PickerItem, 0, len(s))
	for _, alias := range s {
		items = append(items, PickerItem{Name: alias, Kind: KindConfig})
	}
	return items
}

// groupSource offers the saved groups. Picking one connects every host in it,
// which is why the row carries the group's patterns rather than its name.
type groupSource struct {
	store SessionStore
}

// Items reads the group directory. A group whose file cannot be read is
// skipped rather than offered as a row that would fail on enter - the Groups
// panel is where an unreadable file is reported.
func (s groupSource) Items() []PickerItem {
	if s.store == nil {
		return nil
	}
	names, err := s.store.List()
	if err != nil {
		return nil
	}
	items := make([]PickerItem, 0, len(names))
	for _, name := range names {
		sess, err := s.store.Load(name)
		if err != nil || sess == nil {
			continue
		}
		patterns := sess.Patterns()
		if len(patterns) == 0 {
			continue
		}
		items = append(items, PickerItem{
			Name:     "@" + name,
			Kind:     KindGroup,
			Patterns: patterns,
		})
	}
	return items
}

// recentSource offers the hosts earlier runs connected to, most recent first.
type recentSource struct {
	store RecentHosts
}

// Items reads the recent list. An unreadable list is no rows: the picker is
// still usable, and the Groups panel is not the place this would be reported.
func (s recentSource) Items() []PickerItem {
	if s.store == nil {
		return nil
	}
	hosts, err := s.store.Load()
	if err != nil {
		return nil
	}
	items := make([]PickerItem, 0, len(hosts))
	for _, host := range hosts {
		items = append(items, PickerItem{Name: host, Kind: KindRecent})
	}
	return items
}

// mergedSource is several sources as one, in order.
type mergedSource []HostSource

// MergeHostSources merges sources into one, first source wins on a repeated
// name. Nil sources are skipped, so a run without a group store passes one in
// without a nil check at every call site.
//
// The order is the preference: the program passes ssh-config first, so a host
// that is both an alias and a recent connect is one `cfg` row. Deduplication
// is by [PickerItem.Name], which is why groups carry their `@` prefix.
func MergeHostSources(sources ...HostSource) HostSource {
	out := make(mergedSource, 0, len(sources))
	for _, src := range sources {
		if src != nil {
			out = append(out, src)
		}
	}
	return out
}

// Items concatenates the sources' items and drops the repeats.
func (s mergedSource) Items() []PickerItem {
	var items []PickerItem
	seen := make(map[string]bool)
	for _, src := range s {
		for _, item := range src.Items() {
			name := strings.TrimSpace(item.Name)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			item.Name = name
			items = append(items, item)
		}
	}
	return items
}
