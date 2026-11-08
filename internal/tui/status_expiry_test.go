package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/theme"
)

// statusEditor is a fakeEditor that keeps a status message the way the real editor
// does: an action sets it, and ClearStatus drops it. A "save" sets "Saved", and an
// action named "fail" sets an error text.
type statusEditor struct{ *fakeEditor }

func (s statusEditor) Do(a editor.Action, args map[string]any) editor.Result {
	res := s.fakeEditor.Do(a, args)
	switch a {
	case "save":
		s.status = "Saved"
	case "fail":
		s.status = "save /tmp/x/y.txt: encoding: cannot represent U+4E2D"
	}
	return res
}

func (s statusEditor) ClearStatus() { s.status = "" }

func newStatusModel(t *testing.T) (*Model, statusEditor) {
	t.Helper()
	ed := statusEditor{newFakeEditor("hello")}
	keys := testKeys()
	keys["normal|f|ctrl"] = Command{Action: "fail"}
	m := New(Deps{Editor: ed, Keys: keys, Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	return m, ed
}

func TestSavedDoesNotOutliveTheNextKey(t *testing.T) {
	t.Parallel()
	m, ed := newStatusModel(t)
	press(m, ctrl('s'))
	if got := m.statusView().message; got != "Saved" {
		t.Fatalf("status after Ctrl+S = %q, want Saved", got)
	}
	press(m, typed("x"))
	if got := m.statusView().message; got != "" {
		t.Errorf("status after typing = %q, want the stale Saved gone", got)
	}
	if ed.status != "" {
		t.Errorf("editor status %q was not cleared", ed.status)
	}
}

func TestAnActionKeepsTheStatusItSets(t *testing.T) {
	t.Parallel()
	m, _ := newStatusModel(t)
	press(m, typed("x"))
	press(m, ctrl('s'))
	press(m, ctrl('s'))
	if got := m.statusView().message; got != "Saved" {
		t.Errorf("status after a second save = %q, want Saved shown again", got)
	}
}

func TestAClickClearsTheStatus(t *testing.T) {
	t.Parallel()
	m, ed := newStatusModel(t)
	press(m, ctrl('s'))
	m.Update(click(30, 5, tea.MouseLeft))
	if ed.status != "" || m.statusView().message != "" {
		t.Errorf("status after a click = %q, want none", m.statusView().message)
	}
}

func TestEditorStatusesJoinTheWarningsLog(t *testing.T) {
	t.Parallel()
	m, _ := newStatusModel(t)
	press(m, ctrl('f'))
	press(m, typed("x")) // the error is cleared from the row, but kept in the log
	if !slices.ContainsFunc(m.notes.items, func(s string) bool { return strings.Contains(s, "cannot represent U+4E2D") }) {
		t.Fatalf("warnings %q lack the save error", m.notes.items)
	}
}

// failEditor is a statusEditor that tells failures from notes, as the app adapter does.
type failEditor struct{ statusEditor }

func (f failEditor) StatusIsError() bool { return strings.Contains(f.status, "cannot represent") }

func TestWarningsLogKeepsFailuresAndNotNotes(t *testing.T) {
	t.Parallel()
	ed := failEditor{statusEditor{newFakeEditor("hello")}}
	keys := testKeys()
	keys["normal|f|ctrl"] = Command{Action: "fail"}
	m := New(Deps{Editor: ed, Keys: keys, Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	press(m, ctrl('s'))
	if got := m.statusView().message; got != "Saved" {
		t.Fatalf("setup: status = %q, want Saved", got)
	}
	press(m, ctrl('f'))
	if want := []string{"save /tmp/x/y.txt: encoding: cannot represent U+4E2D"}; !slices.Equal(m.notes.items, want) {
		t.Errorf("warnings log = %q, want only the failure %q", m.notes.items, want)
	}
}

// A wheel notch is navigation, like a key or a click, so it ends a message too.
func TestAWheelNotchClearsTheStatus(t *testing.T) {
	t.Parallel()
	m, ed := newStatusModel(t)
	press(m, ctrl('s'))
	if got := m.statusView().message; got != "Saved" {
		t.Fatalf("setup: status = %q, want Saved", got)
	}
	m.Update(tea.MouseWheelMsg(tea.Mouse{X: 40, Y: 10, Button: tea.MouseWheelDown}))
	if ed.status != "" || m.statusView().message != "" {
		t.Errorf("status after a wheel notch = %q, want none", m.statusView().message)
	}
}
