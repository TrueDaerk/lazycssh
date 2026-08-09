package ui

import (
	"strings"
	"testing"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
	"github.com/TrueDaerk/lazycssh/internal/commandlog"
	"github.com/TrueDaerk/lazycssh/internal/workingset"
)

// missingApp builds an app on the Command log panel over a live fleet, so the
// hosts that are up now can be driven per host - which is the whole question
// "send to missing" answers.
func missingApp(t *testing.T, names ...string) (App, *fakeFleet, *fakeSender, *commandlog.Log) {
	t.Helper()

	fleet := newFakeFleet(names...)
	ws := workingset.New(fleet.IDs())
	router, err := broadcast.NewRouter(ws)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	router.Attach(fleet)
	sender := &fakeSender{deliverTo: true}
	log := commandlog.New(0)

	a := resize(t, NewApp(Config{
		Fleet:      fleet,
		Targets:    router,
		WorkingSet: ws,
		Sender:     sender,
		Recorder:   log,
		Panes:      fleet,
		CommandLog: log,
		Theme:      Options{Dark: true},
	}), 120, 40)

	return pressKey(t, a, "4"), fleet, sender, log
}

// sendMissing presses the binding and feeds the message it emits back in, the
// way the event loop would.
func sendMissing(t *testing.T, a App) App {
	t.Helper()

	model, cmd := a.Update(keyMsgFor(t, "m"))
	next, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if cmd == nil {
		t.Fatal("m produced no command")
	}
	msg, ok := cmd().(CommandResendMissingMsg)
	if !ok {
		t.Fatalf("m produced a %T", cmd())
	}
	model, _ = next.Update(msg)
	return model.(App)
}

// The acceptance criterion: the host that reconnected after the fleet already
// ran the command gets it, and the hosts that already ran it do not.
func TestSendMissingReachesOnlyTheHostsThatMissedIt(t *testing.T) {
	a, fleet, sender, log := missingApp(t, "web-01", "web-02", "web-03")
	// The command went out while web-03 was down.
	log.Record("systemctl restart nginx", broadcast.ModeAll, []string{"web-01", "web-02"})

	for _, id := range []string{"web-01", "web-02", "web-03"} {
		fleet.connect(t, id)
	}
	a = syncFleet(t, a)

	a = sendMissing(t, a)

	if len(sender.sentTo) != 1 {
		t.Fatalf("SendTo was called %d times", len(sender.sentTo))
	}
	if got := strings.Join(sender.sentTo[0], ","); got != "web-03" {
		t.Fatalf("sent to %q, want web-03 alone", got)
	}
	if len(sender.sent) != 1 || sender.sent[0] != "systemctl restart nginx\n" {
		t.Fatalf("sent = %q", sender.sent)
	}
}

// Nothing missing is a no-op that says so, rather than a send to nobody or a
// second run everywhere.
func TestSendMissingWithNothingMissing(t *testing.T) {
	a, fleet, sender, log := missingApp(t, "web-01", "web-02")
	log.Record("uptime", broadcast.ModeAll, []string{"web-01", "web-02"})

	for _, id := range []string{"web-01", "web-02"} {
		fleet.connect(t, id)
	}
	a = syncFleet(t, a)

	a = sendMissing(t, a)

	if len(sender.sent) != 0 {
		t.Fatalf("something was sent: %q", sender.sent)
	}
	if got := a.LastDelivery(); got != "all hosts already received this" {
		t.Fatalf("LastDelivery() = %q", got)
	}
	if !strings.Contains(plain(a.View().Content), "all hosts already received this") {
		t.Fatalf("the status bar does not report the no-op:\n%s", plain(a.View().Content))
	}
}

// A host that is down cannot run anything, so it is not offered: only the
// hosts that are up now and missed it are targets.
func TestSendMissingSkipsHostsThatAreDown(t *testing.T) {
	a, fleet, sender, log := missingApp(t, "web-01", "web-02", "web-03")
	log.Record("uptime", broadcast.ModeAll, []string{"web-01"})

	fleet.connect(t, "web-01")
	fleet.connect(t, "web-02")
	fleet.fail(t, "web-03") // never received it, and still cannot
	a = syncFleet(t, a)

	a = sendMissing(t, a)

	if len(sender.sentTo) != 1 || strings.Join(sender.sentTo[0], ",") != "web-02" {
		t.Fatalf("sent to %v, want web-02 alone", sender.sentTo)
	}
}

// A clone has its own identifier and was never a target, so it counts as
// missing - which is the point: the second pane on a host has a fresh shell.
func TestSendMissingOffersAClone(t *testing.T) {
	a, fleet, sender, log := missingApp(t, "web-01", "web-01#2")
	log.Record("uptime", broadcast.ModeAll, []string{"web-01"})

	fleet.connect(t, "web-01")
	fleet.connect(t, "web-01#2")
	a = syncFleet(t, a)

	a = sendMissing(t, a)

	if len(sender.sentTo) != 1 || strings.Join(sender.sentTo[0], ",") != "web-01#2" {
		t.Fatalf("sent to %v, want the clone alone", sender.sentTo)
	}
}

// The resend leaves its own audit entry, recorded in the mode the original
// went out in: it repeats that decision, it does not make a new one.
func TestSendMissingIsRecordedWithTheOriginalMode(t *testing.T) {
	a, fleet, _, log := missingApp(t, "web-01", "web-02")
	log.Record("reboot", broadcast.ModeFleet, []string{"web-01"})

	fleet.connect(t, "web-01")
	fleet.connect(t, "web-02")
	a = syncFleet(t, a)

	a = sendMissing(t, a)

	entry, ok := log.Last()
	if !ok || entry.Command != "reboot" {
		t.Fatalf("log entry = %+v (%v)", entry, ok)
	}
	if entry.Mode != broadcast.ModeFleet {
		t.Fatalf("Mode = %v, want the original mode", entry.Mode)
	}
	if entry.Targets() != 1 || !entry.Received("web-02") {
		t.Fatalf("Hosts = %q, want the hosts that had missed it", entry.Hosts)
	}
}

// The count is on screen before the key is pressed: the preview resolves the
// target list, the same rule the broadcast label follows.
func TestPreviewShowsTheMissingHostsBeforeTheSend(t *testing.T) {
	a, fleet, _, log := missingApp(t, "web-01", "web-02", "web-03")
	log.Record("systemctl restart nginx", broadcast.ModeAll, []string{"web-01"})

	for _, id := range []string{"web-01", "web-02", "web-03"} {
		fleet.connect(t, id)
	}
	a = syncFleet(t, a)

	_, body, ok := a.panels.log.Preview(70, 20)
	if !ok {
		t.Fatal("the Command log panel has no preview")
	}
	view := plain(body)
	if !strings.Contains(view, "missing → 2 hosts") {
		t.Fatalf("the preview does not resolve the target count:\n%s", view)
	}
	if !strings.Contains(view, "web-02") || !strings.Contains(view, "web-03") {
		t.Fatalf("the preview does not name the targets:\n%s", view)
	}

	// And with nothing missing it says that instead of a count.
	log.Record("uptime", broadcast.ModeAll, []string{"web-01", "web-02", "web-03"})
	a = syncFleet(t, pressKey(t, a, "j"))
	_, body, _ = a.panels.log.Preview(70, 20)
	if !strings.Contains(plain(body), "all hosts already received this") {
		t.Fatalf("the preview does not report a complete fleet:\n%s", plain(body))
	}
}

// The status bar says how many machines the send was aimed at, at the send.
func TestSendMissingReportsTheTargetCount(t *testing.T) {
	a, fleet, _, log := missingApp(t, "web-01", "web-02", "web-03")
	log.Record("uptime", broadcast.ModeAll, []string{"web-01"})

	for _, id := range []string{"web-01", "web-02", "web-03"} {
		fleet.connect(t, id)
	}
	a = syncFleet(t, a)

	a = sendMissing(t, a)

	if got := a.LastDelivery(); !strings.Contains(got, "2/2 hosts") {
		t.Fatalf("LastDelivery() = %q, want the two hosts it was aimed at", got)
	}
}
