package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/terminal"
	"github.com/tbxark/gvim/internal/tui/list"
)

// extractTargetFake records the extract-into-existing calls.
func (f *fakeWorkspacePort) ExtractTargets() []WorkspaceEntry { return f.targets }
func (f *fakeWorkspacePort) ExtractTabTo(id string) (WorkspaceCmd, error) {
	f.extractTo = id
	return nil, f.extractErr
}
func (f *fakeWorkspacePort) ExtractTerminalTabTo(id string) (WorkspaceCmd, error) {
	f.extractTerminalTo = id
	return nil, f.extractErr
}
func (f *fakeWorkspacePort) ExtractTabs(paths []string) (WorkspaceCmd, error) {
	f.extractMany = append([]string(nil), paths...)
	return nil, f.extractErr
}
func (f *fakeWorkspacePort) ExtractMixed(sel ExtractSelection, targetID string, replace bool) (WorkspaceCmd, error) {
	f.mixSelection = sel
	f.mixTarget = targetID
	f.mixReplace = replace
	return f.mixCmd, f.extractErr
}

// TestWorkspaceExtractPaletteRows checks the new extract rows appear only when the
// port supports the matching surface.
func TestWorkspaceExtractPaletteRows(t *testing.T) {
	t.Parallel()
	m := statusViewModel(t, nil)
	m.SetWorkspacePort(&fakeWorkspacePort{
		targets: []WorkspaceEntry{{ID: "id2", Label: "two", Root: "/root"}},
	})
	m.AddWorkspaceCommands()
	actions := map[string]bool{}
	for _, c := range m.allCommands() {
		actions[c.Action] = true
	}
	for _, action := range []string{"workspace_extract_tab", "workspace_extract_tab_to", "workspace_extract_terminal_to", "workspace_extract_tabs"} {
		if !actions[action] {
			t.Errorf("palette has no %s row", action)
		}
	}
	// A port with only ExtractTab keeps the old single row: drop the target/many
	// methods by using a port type that only implements WorkspacePort+ExtractTab.
	var old workspaceExtractOnly
	old.entries = []WorkspaceEntry{{ID: "id1", Label: "one", Active: true}}
	m2 := statusViewModel(t, nil)
	m2.SetWorkspacePort(&old)
	m2.AddWorkspaceCommands()
	for _, c := range m2.allCommands() {
		switch c.Action {
		case "workspace_extract_tab_to", "workspace_extract_terminal_to", "workspace_extract_tabs":
			t.Fatalf("a port without the target surfaces has %s", c.Action)
		}
	}
}

// workspaceExtractOnly is a workspace port with only the round-16 extract surface.
type workspaceExtractOnly struct {
	entries []WorkspaceEntry
}

func (w *workspaceExtractOnly) Entries() []WorkspaceEntry           { return w.entries }
func (w *workspaceExtractOnly) Select(string) (WorkspaceCmd, error) { return nil, nil }
func (w *workspaceExtractOnly) Create(string, string) (WorkspaceCmd, error) {
	return nil, nil
}
func (w *workspaceExtractOnly) Close(string) (WorkspaceCmd, error) { return nil, nil }
func (w *workspaceExtractOnly) Rename(string, string) error        { return nil }
func (w *workspaceExtractOnly) Next() (WorkspaceCmd, error)        { return nil, nil }
func (w *workspaceExtractOnly) ExtractTab() (WorkspaceCmd, error)  { return nil, nil }

// TestWorkspaceExtractToExistingPicker checks the picker lists the target
// workspaces and calls the target command with the chosen id.
func TestWorkspaceExtractToExistingPicker(t *testing.T) {
	t.Parallel()
	m := statusViewModel(t, nil)
	port := &fakeWorkspacePort{targets: []WorkspaceEntry{
		{ID: "id2", Label: "two", Root: "/root", Materialized: true},
		{ID: "id3", Label: "three", Root: "/root"},
	}}
	m.SetWorkspacePort(port)
	if cmd, ok := m.workspaceAction("workspace_extract_tab_to"); !ok || cmd != nil {
		t.Fatalf("workspace_extract_tab_to = %v, %v", cmd, ok)
	}
	if len(m.overlays) != 1 {
		t.Fatalf("target picker overlays = %T", m.overlays)
	}
	v, ok := m.overlays[0].(*list.View)
	if !ok || v.Title != "Extract Tab to Workspace" {
		t.Fatalf("picker = %#v", m.overlays[0])
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if port.extractTo != "id3" {
		t.Fatalf("ExtractTabTo = %q, want id3", port.extractTo)
	}

	// No targets: a notice, no empty picker.
	empty := &fakeWorkspacePort{}
	m2 := statusViewModel(t, nil)
	m2.SetWorkspacePort(empty)
	m2.workspaceAction("workspace_extract_tab_to")
	if len(m2.overlays) != 0 || !strings.Contains(m2.notice, "No other workspace over this root") {
		t.Fatalf("empty picker notice = %q overlays %d", m2.notice, len(m2.overlays))
	}
}

// TestWorkspaceExtractTerminalNeedsTab: with no dock tab the action explains instead
// of opening the target picker.
func TestWorkspaceExtractTerminalNeedsTab(t *testing.T) {
	t.Parallel()
	m := statusViewModel(t, nil)
	m.SetWorkspacePort(&fakeWorkspacePort{targets: []WorkspaceEntry{{ID: "id2"}}})
	m.workspaceAction("workspace_extract_terminal_to")
	if len(m.overlays) != 0 {
		t.Fatalf("terminal extract opened a picker without a dock tab")
	}
	if m.notice == "" {
		t.Fatal("terminal extract without a tab said nothing")
	}
}

// answerKey presses a confirm answer and delivers the Done command it returns, so
// the tag handler runs through the same path the program uses.
func answerKey(m *Model, code rune, text string) {
	_, cmd := m.Update(tea.KeyPressMsg{Code: code, Text: text})
	if cmd != nil {
		m.Update(cmd())
	}
}

// mixedTabModel has two file tabs and one restored terminal tab.
func mixedTabModel(t *testing.T) (*Model, *fakeEditor) {
	t.Helper()
	m := statusViewModel(t, nil)
	ed := m.ed.(*fakeEditor)
	ed.tabs = []TabInfo{
		{Name: "a.txt", Path: "/ws/a.txt", Modified: true},
		{Name: "welcome", Path: ""},
		{Name: "b.txt", Path: "/ws/b.txt"},
	}
	m.RestoreTerminal(terminal.SavedState{Version: terminal.SavedStateVersion, Tabs: []terminal.SavedTab{{Name: "shell", Open: true, Rows: []string{"out"}}}})
	return m, ed
}

// TestWorkspaceExtractMixedPicker: the picker lists file AND terminal tabs, and the
// marked set is handed to the mixed port; with no existing targets it goes straight
// to a new co-tenant.
func TestWorkspaceExtractMixedPicker(t *testing.T) {
	t.Parallel()
	m, _ := mixedTabModel(t)
	port := &fakeWorkspacePort{}
	m.SetWorkspacePort(port)
	if cmd, ok := m.workspaceAction("workspace_extract_tabs"); !ok || cmd != nil {
		t.Fatalf("workspace_extract_tabs = %v, %v", cmd, ok)
	}
	if len(m.overlays) != 1 {
		t.Fatalf("mixed picker overlays = %T", m.overlays)
	}
	v, ok := m.overlays[0].(*list.View)
	if !ok || !v.Multi || len(v.Rows) != 3 {
		t.Fatalf("mixed picker = %#v", m.overlays[0])
	}
	// Space marks a.txt, Down skips to b.txt (the welcome row is not offered), Space
	// marks it; the terminal row stays unmarked.
	m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	if _, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		m.Update(cmd())
	}
	if port.mixTarget != "" || port.mixReplace {
		t.Fatalf("new-target call = %q replace=%v", port.mixTarget, port.mixReplace)
	}
	if len(port.mixSelection.Files) != 2 || port.mixSelection.Files[0] != "/ws/a.txt" || port.mixSelection.Files[1] != "/ws/b.txt" {
		t.Fatalf("mixed files = %v", port.mixSelection.Files)
	}
}

// TestWorkspaceExtractMixedTargetChooser: with existing workspaces the Enter asks for
// the target; the first row is the new co-tenant, the rest are the existing entries.
func TestWorkspaceExtractMixedTargetChooser(t *testing.T) {
	t.Parallel()
	m, _ := mixedTabModel(t)
	port := &fakeWorkspacePort{targets: []WorkspaceEntry{
		{ID: "id2", Label: "two", Root: "/root", Materialized: true},
		{ID: "id3", Label: "three", Root: "/root"},
	}}
	m.SetWorkspacePort(port)
	m.workspaceAction("workspace_extract_tabs")
	// Mark the terminal row (third row: a.txt, b.txt, shell) and choose the second
	// target row (New workspace, two, three).
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	// The picker hands the selection back through a command so it has popped before
	// the target chooser is pushed.
	if _, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		m.Update(cmd())
	}
	if len(m.overlays) != 1 {
		t.Fatalf("target chooser overlays = %T", m.overlays)
	}
	chooser, ok := m.overlays[0].(*list.View)
	if !ok || chooser.Title != "Extract Tabs To" || len(chooser.Rows) != 3 {
		t.Fatalf("chooser = %#v", m.overlays[0])
	}
	if chooser.Rows[0].Value != "" || !strings.Contains(chooser.Rows[0].Left, "New workspace") {
		t.Fatalf("first chooser row = %+v, want the new co-tenant", chooser.Rows[0])
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // two
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if port.mixTarget != "id2" || port.mixReplace {
		t.Fatalf("target call = %q replace=%v", port.mixTarget, port.mixReplace)
	}
	if len(port.mixSelection.Terminals) != 1 || port.mixSelection.Terminals[0].Name != "shell" {
		t.Fatalf("mixed terminals = %+v", port.mixSelection.Terminals)
	}
}

// TestWorkspaceExtractMixedConflictPrompt: a conflict report from the command asks
// first; No changes nothing, Yes retries with replace=true.
func TestWorkspaceExtractMixedConflictPrompt(t *testing.T) {
	t.Parallel()
	for _, yes := range []bool{false, true} {
		m, _ := mixedTabModel(t)
		port := &fakeWorkspacePort{targets: []WorkspaceEntry{{ID: "id2", Label: "two", Root: "/root"}}}
		port.mixCmd = func() tea.Msg {
			return ExtractConflictMsg{
				Selection: ExtractSelection{Files: []string{"/ws/a.txt"}}, TargetID: "id2", TargetLabel: "two",
				Conflicts: []string{"/ws/a.txt", "terminal \"shell\""},
			}
		}
		m.SetWorkspacePort(port)
		m.workspaceAction("workspace_extract_tabs")
		m.Update(tea.KeyPressMsg{Code: tea.KeySpace}) // mark a.txt
		if _, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
			m.Update(cmd()) // picker -> target chooser
		}
		// Choose the existing target (first row is New workspace, second is id2).
		m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if cmd != nil {
			m.Update(cmd()) // chooser -> command -> conflict message -> confirmation
		}
		if len(m.overlays) != 1 {
			t.Fatalf("no conflict confirmation: %T", m.overlays)
		}
		if yes {
			answerKey(m, 'y', "y")
			if !port.mixReplace {
				t.Fatal("Yes did not retry with replace")
			}
		} else {
			answerKey(m, 'n', "n")
			if port.mixReplace {
				t.Fatal("No retried with replace")
			}
			if !strings.Contains(m.notice, "cancelled; nothing was moved") {
				t.Fatalf("cancel notice = %q", m.notice)
			}
		}
	}
}
