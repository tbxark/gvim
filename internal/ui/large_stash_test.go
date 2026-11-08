package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/editor"
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

// quickSaver is a largeEditor whose write ends at once, where the largeEditor's waits to
// be cancelled.
type quickSaver struct{ *largeEditor }

func (e *quickSaver) WriteSaveContext(ctx context.Context, p editor.SavePlan) []editor.SaveResult {
	return p.RunContext(ctx)
}

// TestQuitDuringASaveWaitsForTheWriteInsteadOfAskingAboutTheTabItSaves checks that a
// quit pressed while a write is in flight opens no dialog, which would still be open
// over a clean buffer once the write ended, and quits when the write has been applied.
func TestQuitDuringASaveWaitsForTheWriteInsteadOfAskingAboutTheTabItSaves(t *testing.T) {
	t.Parallel()
	path := writeLines(t, t.TempDir(), "a.txt", bigFile, 40, 0)
	le := &quickSaver{newLargeEditor(t, path)}
	km, err := keymap.ByName("default")
	if err != nil {
		t.Fatal(err)
	}
	d := &fakeDialogs{}
	m := New(Deps{Editor: le, Keys: presetKeymap{km: km}, Theme: theme.Theme{}, Clock: testClock, Dialogs: d})
	feedAll(t, m, start(m), 10)
	le.ed.Do("insert_char", map[string]any{"text": "Z"})
	save := press(m, ctrl('s'))
	if len(m.large.saves) != 1 {
		t.Fatalf("%d writes in flight, want 1", len(m.large.saves))
	}

	if cmd := press(m, ctrl('q')); cmd != nil {
		t.Fatalf("a quit during a save returned a command")
	}
	if len(d.opened) != 0 || len(m.overlays) != 0 {
		t.Fatalf("a quit during a save asked: dialogs %v, overlays %d", d.opened, len(m.overlays))
	}
	saved := ofType[saveDoneMsg](runCmd(save))
	if len(saved) != 1 {
		t.Fatalf("%d save results, want 1", len(saved))
	}
	_, cmd := m.Update(saved[0])
	var quit bool
	for _, msg := range runCmd(cmd) {
		_, ok := msg.(tea.QuitMsg)
		quit = quit || ok
	}
	if !quit {
		t.Errorf("the program did not quit when the write ended; dialogs %v", d.opened)
	}
	if len(d.opened) != 0 {
		t.Errorf("the quit asked after the write: %v", d.opened)
	}
}

// TestSecondQuitDuringASaveAsksAtOnce checks the way out of a write that does not end.
func TestSecondQuitDuringASaveAsksAtOnce(t *testing.T) {
	t.Parallel()
	path := writeLines(t, t.TempDir(), "a.txt", bigFile, 40, 0)
	le := newLargeEditor(t, path)
	km, err := keymap.ByName("default")
	if err != nil {
		t.Fatal(err)
	}
	d := &fakeDialogs{}
	m := New(Deps{Editor: le, Keys: presetKeymap{km: km}, Theme: theme.Theme{}, Clock: testClock, Dialogs: d})
	feedAll(t, m, start(m), 10)
	le.ed.Do("insert_char", map[string]any{"text": "Z"})
	press(m, ctrl('s'))
	press(m, ctrl('q'))
	press(m, ctrl('q'))
	if len(d.opened) != 1 || d.opened[0].kind != "unsaved" {
		t.Errorf("the second quit opened %v, want the unsaved dialog", d.opened)
	}
}
