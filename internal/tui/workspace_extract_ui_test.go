package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

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

// TestWorkspaceExtractTabsPicker: the multi-select picker offers only file tabs, Space
// marks them and Enter extracts the marked set.
func TestWorkspaceExtractTabsPicker(t *testing.T) {
	t.Parallel()
	m := statusViewModel(t, nil)
	ed := m.ed.(*fakeEditor)
	ed.tabs = []TabInfo{
		{Name: "a.txt", Path: "/ws/a.txt"},
		{Name: "welcome", Path: ""},
		{Name: "b.txt", Path: "/ws/b.txt", Modified: true},
	}
	port := &fakeWorkspacePort{targets: []WorkspaceEntry{{ID: "id2"}}}
	m.SetWorkspacePort(port)
	if cmd, ok := m.workspaceAction("workspace_extract_tabs"); !ok || cmd != nil {
		t.Fatalf("workspace_extract_tabs = %v, %v", cmd, ok)
	}
	if len(m.overlays) != 1 {
		t.Fatalf("multi picker overlays = %T", m.overlays)
	}
	v, ok := m.overlays[0].(*list.View)
	if !ok || !v.Multi || len(v.Rows) != 2 {
		t.Fatalf("multi picker = %#v", m.overlays[0])
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(port.extractMany) != 2 || port.extractMany[0] != "/ws/a.txt" || port.extractMany[1] != "/ws/b.txt" {
		t.Fatalf("ExtractTabs = %v", port.extractMany)
	}
}
