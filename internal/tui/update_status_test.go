package tui

import (
	"context"
	"testing"

	"github.com/tbxark/gvim/theme"
)

// updateModel builds a model whose release check returns info (or nothing when the
// hook is nil).
func updateModel(check func(context.Context) *UpdateInfo) *Model {
	return New(Deps{
		Editor:  newFakeEditor("doc"),
		Theme:   theme.Theme{},
		Clock:   testClock,
		Keys:    fakeKeymap{},
		Updates: UpdateHooks{Check: check},
	})
}

// TestUpdateElementShowsTheNewerRelease checks the {update} status element: absent
// until the background check answers, then Fresh's "Update: vX.Y.Z".
func TestUpdateElementShowsTheNewerRelease(t *testing.T) {
	m := updateModel(func(context.Context) *UpdateInfo { return nil })
	if m.statusView().Update != "" {
		t.Fatalf("the element painted %q before the check", m.statusView().Update)
	}
	cmd := m.updateCheckCmd()
	if cmd == nil {
		t.Fatal("no release-check command was scheduled")
	}
	// The check returns a newer release: the element paints it.
	m.Update(cmd())
	if got := m.statusView().Update; got != "" {
		// The first model has a nil answer; paint a found release instead.
		t.Fatalf("a nil answer painted %q", got)
	}
	found := updateModel(func(context.Context) *UpdateInfo { return &UpdateInfo{Version: "0.4.2", URL: "https://example.com"} })
	found.Update(found.updateCheckCmd()())
	if got := found.statusView().Update; got != "Update: v0.4.2" {
		t.Fatalf("the element = %q, want Update: v0.4.2", got)
	}
}

// TestUpdateElementAbsentWithoutAHook checks that a launch without the check (a
// test model, or check_for_updates off) schedules nothing and paints nothing.
func TestUpdateElementAbsentWithoutAHook(t *testing.T) {
	m := New(Deps{Editor: newFakeEditor("doc"), Theme: theme.Theme{}, Clock: testClock, Keys: fakeKeymap{}})
	if m.updateCheckCmd() != nil {
		t.Fatal("a model without update hooks scheduled a check")
	}
	if got := m.statusView().Update; got != "" {
		t.Fatalf("the element painted %q without hooks", got)
	}
	// A check that fails later (or a reconnect) never paints a stale release.
	m.Update(updateCheckedMsg{info: nil})
	if got := m.statusView().Update; got != "" {
		t.Fatalf("an empty answer painted %q", got)
	}
}
