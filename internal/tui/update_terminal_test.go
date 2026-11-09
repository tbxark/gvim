package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	commands "github.com/tbxark/gvim/commands"
)

// hasUpdateRow reports whether the palette command list has action.
func hasUpdateRow(m *Model, action string) bool {
	return slices.ContainsFunc(m.allCommands(), func(e commands.Entry) bool { return e.Action == action })
}

func TestUpdateRowsHiddenWithoutACommand(t *testing.T) {
	m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: newFakeStarter()})
	if hasUpdateRow(m, "update_fresh") || hasUpdateRow(m, "open_update_log") {
		t.Fatal("the updater rows are listed without an update command")
	}
	if _, ok := m.updateAction("update_fresh"); ok {
		t.Fatal("Update fresh was handled without a command")
	}
	if _, ok := m.updateAction("open_update_log"); ok {
		t.Fatal("Show update output was handled without a command")
	}
}

func TestUpdateRowsShownWithACommand(t *testing.T) {
	m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: newFakeStarter()})
	m.SetUpdateHooks(UpdateHooks{Command: []string{"echo", "hi"}})
	if !hasUpdateRow(m, "update_fresh") || !hasUpdateRow(m, "open_update_log") {
		t.Fatal("the updater rows are missing with an update command")
	}
}

// TestUpdateCommandRunsInTheDock: Update fresh starts the configured argv in a dock
// tab named Update, keeps the output tab, and a second run restarts that one tab
// instead of opening a duplicate.
func TestUpdateCommandRunsInTheDock(t *testing.T) {
	starter := newFakeStarter()
	m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: starter})
	m.SetUpdateHooks(UpdateHooks{Command: []string{"echo", "update"}})
	cmd, ok := m.updateAction("update_fresh")
	if !ok || cmd == nil {
		t.Fatalf("Update fresh = %v, %v", cmd, ok)
	}
	dockSettle(t, m, cmd)
	if len(starter.launches) != 1 || !slices.Equal(starter.launches[0].Argv, []string{"echo", "update"}) {
		t.Fatalf("launches = %+v, want the configured argv", starter.launches)
	}
	if !slices.Contains(m.dock.TabNames(), "Update") {
		t.Fatalf("dock tabs = %v, want Update", m.dock.TabNames())
	}
	before := m.dock.Count()
	cmd, ok = m.updateAction("update_fresh")
	if !ok || cmd == nil {
		t.Fatalf("second Update fresh = %v, %v", cmd, ok)
	}
	dockSettle(t, m, cmd)
	if m.dock.Count() != before {
		t.Fatalf("a second run opened a new tab: %d -> %d", before, m.dock.Count())
	}
	// Show update output comes back to the same tab.
	m.dock.Hide()
	cmd, ok = m.updateAction("open_update_log")
	if !ok {
		t.Fatal("Show update output was not handled")
	}
	if cmd != nil {
		dockSettle(t, m, cmd)
	}
	if !m.dock.Open() {
		t.Fatal("Show update output did not show the dock")
	}
}

func TestUpdateOutputWithoutARun(t *testing.T) {
	m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: newFakeStarter()})
	m.SetUpdateHooks(UpdateHooks{Command: []string{"echo", "hi"}})
	if cmd, ok := m.updateAction("open_update_log"); !ok || cmd != nil {
		t.Fatalf("Show update output before a run = %v, %v", cmd, ok)
	}
	if !strings.Contains(m.notice, "No update output yet") {
		t.Fatalf("notice = %q", m.notice)
	}
}

func TestUpdateCommandStartErrorIsClear(t *testing.T) {
	starter := newFakeStarter()
	starter.err = errTermStart
	m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: starter})
	m.SetUpdateHooks(UpdateHooks{Command: []string{"echo", "hi"}})
	cmd, ok := m.updateAction("update_fresh")
	if !ok || cmd == nil {
		t.Fatalf("Update fresh = %v, %v", cmd, ok)
	}
	dockSettle(t, m, cmd)
	notice := m.notice
	if n := m.dock.Notice(); n != "" {
		notice = n
	}
	if !strings.Contains(strings.ToLower(notice), "start") && !strings.Contains(notice, errTermStart.Error()) {
		t.Fatalf("notice = %q, want a clear start error", notice)
	}
}

var errTermStart = errors.New("cannot start the terminal")
