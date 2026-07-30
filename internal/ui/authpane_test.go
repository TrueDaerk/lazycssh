package ui

import (
	"strings"
	"testing"
)

// A duplicated alias dials as alias#2; the question about the alias lands on
// the first matching pane rather than nowhere.
func TestPaneIndexForMatchesDuplicateAliases(t *testing.T) {
	a := NewApp(Config{Hosts: []string{"db-01#2", "web-01"}, Theme: Options{Dark: true}})

	if i, ok := a.paneIndexFor("web-01"); !ok || i != 1 {
		t.Fatalf("paneIndexFor(web-01) = %d, %v, want 1, true", i, ok)
	}
	if i, ok := a.paneIndexFor("db-01"); !ok || i != 0 {
		t.Fatalf("paneIndexFor(db-01) = %d, %v, want 0, true", i, ok)
	}
	if _, ok := a.paneIndexFor("gone"); ok {
		t.Fatal("paneIndexFor(gone) found a pane")
	}
	if _, ok := a.paneIndexFor(""); ok {
		t.Fatal("paneIndexFor(\"\") found a pane")
	}
}

// A host leaving mid-question sends the question back to the Status panel
// rather than nowhere.
func TestQuestionFollowsADepartedHostToTheStatusPanel(t *testing.T) {
	a := questionApp(t)
	model, _ := a.Update(HostsChangedMsg{Hosts: []string{"web-02", "web-03"}})
	a = model.(App)

	if a.questionPaneVisible() {
		t.Fatal("the question still claims a pane its host left")
	}
	if lines := a.statusPanel(60); !strings.Contains(plain(lines), "unknown") {
		t.Fatalf("the Status panel did not pick up the orphaned question:\n%s", plain(lines))
	}
}
