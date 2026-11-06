package ui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/keymap"
	"github.com/tbxark/gvim/internal/theme"
	"github.com/tbxark/gvim/internal/ui/style"
)

// messageDialogs is the fake dialogs with the message dialog of the app's port.
type messageDialogs struct {
	fakeDialogs
	messages []string
}

func (d *messageDialogs) Error(message string, tag any, _ style.Overlay) Overlay {
	d.messages = append(d.messages, message)
	d.opened = append(d.opened, dialogCall{kind: "error", text: message, tag: tag})
	return &dialogOverlay{}
}

func newQuitStashModel(t *testing.T, dialogs Dialogs) *Model {
	t.Helper()
	le := newLargeEditor(t)
	km, err := keymap.ByName("default")
	if err != nil {
		t.Fatal(err)
	}
	m := New(Deps{Editor: le, Keys: presetKeymap{km: km}, Theme: theme.Theme{}, Clock: testClock, Dialogs: dialogs})
	start(m)
	return m
}

func isQuitCmd(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func TestQuitBlockedWhenStashFails(t *testing.T) {
	t.Parallel()
	d := &messageDialogs{}
	m := newQuitStashModel(t, d)
	calls := 0
	failure := errors.New("disk full")
	m.SetQuitStash(func() error {
		calls++
		return failure
	})

	if cmd := m.quit(); cmd != nil {
		t.Fatalf("quit() returned a command although the stash failed")
	}
	if calls != 1 {
		t.Fatalf("the stash ran %d times, want 1", calls)
	}
	if len(d.messages) != 1 || !strings.Contains(d.messages[0], "disk full") || !strings.Contains(d.messages[0], "Quit cancelled") {
		t.Errorf("dialog messages = %q, want one that names the cancelled quit and the error", d.messages)
	}
	if len(m.overlays) != 1 {
		t.Errorf("%d overlays, want the error dialog", len(m.overlays))
	}
	if !strings.Contains(m.notice, "disk full") {
		t.Errorf("notice = %q, want the error", m.notice)
	}

	// Once the stash can be written the quit goes through, and the stash ran again.
	m.overlays = nil
	m.SetQuitStash(func() error {
		calls++
		return nil
	})
	if !isQuitCmd(m.quit()) {
		t.Errorf("quit() did not quit after the stash worked")
	}
	if calls != 2 {
		t.Errorf("the stash ran %d times, want 2", calls)
	}
}

func TestQuitActionIsBlockedByAFailedStashToo(t *testing.T) {
	t.Parallel()
	m := newQuitStashModel(t, &messageDialogs{})
	m.SetQuitStash(func() error { return errors.New("read-only file system") })
	cmd := m.quitLoop()
	if cmd != nil {
		t.Fatalf("the quit action quit although the stash failed")
	}
	if !strings.Contains(m.notice, "read-only file system") {
		t.Errorf("notice = %q", m.notice)
	}
}

func TestFailedStashShowsTheNoticeWithoutDialogs(t *testing.T) {
	t.Parallel()
	m := newQuitStashModel(t, nil)
	m.SetQuitStash(func() error { return errors.New("no space") })
	if cmd := m.quit(); cmd != nil {
		t.Fatalf("quit() returned a command although the stash failed")
	}
	if !strings.Contains(m.notice, "no space") || len(m.overlays) != 0 {
		t.Errorf("notice = %q, overlays %d: want the notice only", m.notice, len(m.overlays))
	}
}

func TestQuitWithoutAStashFunctionQuitsAtOnce(t *testing.T) {
	t.Parallel()
	m := newQuitStashModel(t, &messageDialogs{})
	if !isQuitCmd(m.quit()) {
		t.Errorf("quit() did not quit with no stash function")
	}
	m.SetQuitStash(nil)
	if !isQuitCmd(m.quit()) {
		t.Errorf("quit() did not quit after the stash function was removed")
	}
}
