package ui

import (
	"errors"
	"slices"
	"testing"

	"github.com/TrueDaerk/lazycssh/internal/sessions"
)

// stubRecent is a recent-host list without a disk.
type stubRecent struct {
	hosts []string
	err   error
}

func (s stubRecent) Load() ([]string, error) { return s.hosts, s.err }

// names is the item names, which is what most of these assertions compare.
func names(items []PickerItem) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Name)
	}
	return out
}

// tags is the item origin tags in row order.
func tags(items []PickerItem) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Kind.Tag())
	}
	return out
}

// The three sources become one list, in source order, each row tagged with
// where it came from.
func TestMergeHostSourcesTagsEverySource(t *testing.T) {
	store := storeWith(t, savedGroup("prod", "web-{01..02}"))
	src := MergeHostSources(
		aliasSource{"web-01", "db-01"},
		groupSource{store: store},
		recentSource{store: stubRecent{hosts: []string{"cache-7"}}},
	)

	items := src.Items()
	if got, want := names(items), []string{"web-01", "db-01", "@prod", "cache-7"}; !slices.Equal(got, want) {
		t.Fatalf("Items() = %v, want %v", got, want)
	}
	if got, want := tags(items), []string{"cfg", "cfg", "grp", "rec"}; !slices.Equal(got, want) {
		t.Fatalf("tags = %v, want %v", got, want)
	}
}

// A host that is both an ssh-config alias and a recent connect is one row, and
// it is the alias: the merge order is the preference.
func TestMergeHostSourcesPrefersTheEarlierSource(t *testing.T) {
	src := MergeHostSources(
		aliasSource{"web-01"},
		recentSource{store: stubRecent{hosts: []string{"web-01", "cache-7"}}},
	)

	items := src.Items()
	if got, want := names(items), []string{"web-01", "cache-7"}; !slices.Equal(got, want) {
		t.Fatalf("Items() = %v, want %v", got, want)
	}
	if items[0].Kind != KindConfig {
		t.Fatalf("the repeated row is tagged %q, want cfg", items[0].Kind.Tag())
	}
}

// A source that repeats itself - a hand-edited recent file, a config listing
// an alias twice - still produces one row.
func TestMergeHostSourcesDropsRepeatsWithinASource(t *testing.T) {
	src := MergeHostSources(aliasSource{"web-01", "web-01", " ", "db-01"})

	if got, want := names(src.Items()), []string{"web-01", "db-01"}; !slices.Equal(got, want) {
		t.Fatalf("Items() = %v, want %v", got, want)
	}
}

// A group's row carries the group's patterns, so picking it connects the whole
// group rather than a host named after it.
func TestGroupSourceCarriesThePatterns(t *testing.T) {
	store := storeWith(t, savedGroup("prod", "web-{01..02}", "db-01"))

	items := groupSource{store: store}.Items()
	if len(items) != 1 {
		t.Fatalf("Items() = %v, want one group row", names(items))
	}
	if got, want := items[0].connect(), []string{"web-{01..02}", "db-01"}; !slices.Equal(got, want) {
		t.Fatalf("connect() = %v, want %v", got, want)
	}
}

// A host row connects itself: no patterns means the name.
func TestPickerItemWithoutPatternsConnectsItsName(t *testing.T) {
	item := PickerItem{Name: "web-01", Kind: KindConfig}
	if got, want := item.connect(), []string{"web-01"}; !slices.Equal(got, want) {
		t.Fatalf("connect() = %v, want %v", got, want)
	}
}

// A store that cannot be listed costs the picker its group rows, not its
// usability: the Groups panel is where an unreadable directory is reported.
func TestGroupSourceSurvivesAnUnreadableStore(t *testing.T) {
	if items := (groupSource{store: brokenDirStore{}}).Items(); len(items) != 0 {
		t.Fatalf("Items() = %v, want none", names(items))
	}
	if items := (groupSource{}).Items(); len(items) != 0 {
		t.Fatalf("Items() without a store = %v, want none", names(items))
	}
}

// Same for the recent list: an unreadable file is no rows, not a failure.
func TestRecentSourceSurvivesAnUnreadableList(t *testing.T) {
	src := recentSource{store: stubRecent{err: errors.New("permission denied")}}
	if items := src.Items(); len(items) != 0 {
		t.Fatalf("Items() = %v, want none", names(items))
	}
	if items := (recentSource{}).Items(); len(items) != 0 {
		t.Fatalf("Items() without a store = %v, want none", names(items))
	}
}

// A nil source in the merge is skipped, so a run without a group store passes
// one in without a nil check at the call site.
func TestMergeHostSourcesSkipsNilSources(t *testing.T) {
	src := MergeHostSources(nil, aliasSource{"web-01"}, nil)
	if got, want := names(src.Items()), []string{"web-01"}; !slices.Equal(got, want) {
		t.Fatalf("Items() = %v, want %v", got, want)
	}
}

// storeWith builds a group store in a temporary directory holding the given
// groups.
func storeWith(t *testing.T, saved ...*sessions.Session) *sessions.Store {
	t.Helper()

	store, err := sessions.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	for _, s := range saved {
		if err := store.Save(s); err != nil {
			t.Fatalf("Save %s: %v", s.Name, err)
		}
	}
	return store
}
