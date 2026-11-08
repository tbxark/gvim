package tui

import (
	"context"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/tui/list"
	"github.com/tbxark/gvim/theme"
)

// updateStatusModel builds a model with the given update hooks.
func updateStatusModel(h UpdateHooks) *Model {
	return New(Deps{
		Editor:  newFakeEditor("doc"),
		Theme:   theme.Theme{},
		Clock:   testClock,
		Keys:    fakeKeymap{},
		Updates: h,
	})
}

// TestCheckForUpdatesCommand drives the Check for Updates command: it runs the
// check in a command (never on the update goroutine), reports the found release
// and paints the element; an up-to-date answer clears the element.
func TestCheckForUpdatesCommand(t *testing.T) {
	status := UpdateStatus{Current: "0.4.1", Feed: "https://example.com/feed", CheckedAt: time.Now()}
	var forced int
	m := updateStatusModel(UpdateHooks{
		Force: func(context.Context) UpdateStatus {
			forced++
			return status
		},
		Status: func() UpdateStatus { return status },
	})
	cmd, ok := m.updateAction("check_for_updates")
	if !ok || cmd == nil {
		t.Fatalf("check_for_updates = %v, %v; want a command", cmd, ok)
	}
	if forced != 0 {
		t.Fatal("the check ran on the update goroutine")
	}
	// A found release: element painted, notice names it.
	status.Available, status.Latest, status.URL = true, "0.4.2", "https://example.com/releases/0.4.2"
	m.Update(cmd())
	if forced != 1 {
		t.Fatalf("forced checks = %d, want 1", forced)
	}
	if got := m.statusView().Update; got != "Update: v0.4.2" {
		t.Fatalf("element = %q, want Update: v0.4.2", got)
	}
	if m.notice == "" || !contains(m.notice, "0.4.2 is available") || !contains(m.notice, "releases/0.4.2") {
		t.Fatalf("notice = %q, want the release and its page", m.notice)
	}
	// Up to date: element cleared, notice says so.
	status = UpdateStatus{Current: "0.4.2", CheckedAt: time.Now()}
	cmd, _ = m.updateAction("check_for_updates")
	m.Update(cmd())
	if got := m.statusView().Update; got != "" {
		t.Fatalf("element = %q after an up-to-date answer", got)
	}
	if !contains(m.notice, "up to date") {
		t.Fatalf("notice = %q, want the up-to-date answer", m.notice)
	}
}

// TestCheckForUpdatesFailureAndDisabled checks the two failure paths: a failed
// check is a notice, and a launch with the check off says so instead of doing
// nothing.
func TestCheckForUpdatesFailureAndDisabled(t *testing.T) {
	m := updateStatusModel(UpdateHooks{
		Force: func(context.Context) UpdateStatus {
			return UpdateStatus{Current: "0.4.1", Err: "the feed answered 404 Not Found"}
		},
	})
	cmd, ok := m.updateAction("check_for_updates")
	if !ok || cmd == nil {
		t.Fatal("check_for_updates did not run")
	}
	m.Update(cmd())
	if !contains(m.notice, "Update check failed") || !contains(m.notice, "404") {
		t.Fatalf("notice = %q, want the failure reason", m.notice)
	}
	if got := m.statusView().Update; got != "" {
		t.Fatalf("a failed check painted %q", got)
	}
	// No hooks at all: the command explains the check is off.
	off := updateStatusModel(UpdateHooks{})
	if cmd, ok := off.updateAction("check_for_updates"); !ok || cmd != nil {
		t.Fatalf("a disabled check returned %v, %v", cmd, ok)
	}
	if !contains(off.notice, "off") {
		t.Fatalf("notice = %q, want the disabled explanation", off.notice)
	}
}

// TestShowUpdateOutput opens the details list with the check state.
func TestShowUpdateOutput(t *testing.T) {
	checked := time.Date(2026, 3, 4, 9, 5, 0, 0, time.UTC)
	st := UpdateStatus{
		Current: "0.4.1", Latest: "0.4.2", Available: true,
		URL: "https://example.com/releases/0.4.2", Feed: "https://example.com/feed",
		CheckedAt: checked,
	}
	m := updateStatusModel(UpdateHooks{Status: func() UpdateStatus { return st }})
	if _, ok := m.updateAction("show_update_output"); !ok {
		t.Fatal("show_update_output was not handled")
	}
	if len(m.overlays) == 0 {
		t.Fatal("no details list opened")
	}
	v, ok := m.overlays[len(m.overlays)-1].(*list.View)
	if !ok {
		t.Fatalf("top overlay = %T, want the update list", m.overlays[len(m.overlays)-1])
	}
	rows := v.Rows
	want := map[string]string{
		"Feed":            "https://example.com/feed",
		"Current version": "0.4.1",
		"Latest release":  "0.4.2",
		"Status":          "update available",
		"Last checked":    "2026-03-04 09:05",
		"Release page":    "https://example.com/releases/0.4.2",
	}
	for _, r := range rows {
		if v, ok := want[r.Left]; ok {
			if r.Right != v {
				t.Errorf("%s = %q, want %q", r.Left, r.Right, v)
			}
			delete(want, r.Left)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing rows: %v", want)
	}
}

// TestUpdateSegmentClick checks the {update} element is clickable: the click shows
// the details instead of starting an install (gvim has no self-update).
func TestUpdateSegmentClick(t *testing.T) {
	m := updateStatusModel(UpdateHooks{Status: func() UpdateStatus {
		return UpdateStatus{Current: "0.4.1", Latest: "0.4.2", Available: true}
	}})
	m.Update(updateCheckedMsg{info: &UpdateInfo{Version: "0.4.2"}})
	if cmd := m.statusSegmentClick(statusSegmentUpdate); cmd != nil {
		t.Fatalf("the update click returned a command: %v", cmd)
	}
	if len(m.overlays) == 0 {
		t.Fatal("the update click opened no details")
	}
}

// contains reports whether s has sub.
func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

// indexOf is strings.Index without pulling the import into this small test helper.
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
