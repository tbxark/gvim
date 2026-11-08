package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/list"
	"github.com/tbxark/gvim/internal/tui/prompt"
)

// fakeWorkspacePort records the TUI calls of the workspace UI.
type fakeWorkspacePort struct {
	entries                                  []WorkspaceEntry
	selected, created, closed, renamed, next string
	createRoot, createLabel                  string
	renameLabel                              string
}

func (f *fakeWorkspacePort) Entries() []WorkspaceEntry { return f.entries }
func (f *fakeWorkspacePort) Select(id string) (WorkspaceCmd, error) {
	f.selected = id
	return nil, nil
}
func (f *fakeWorkspacePort) Create(root, label string) (WorkspaceCmd, error) {
	f.created, f.createRoot, f.createLabel = "yes", root, label
	return nil, nil
}
func (f *fakeWorkspacePort) Close(id string) (WorkspaceCmd, error) {
	f.closed = id
	return nil, nil
}
func (f *fakeWorkspacePort) Rename(id, label string) error {
	f.renamed, f.renameLabel = id, label
	return nil
}
func (f *fakeWorkspacePort) Next() (WorkspaceCmd, error) {
	f.next = "yes"
	return nil, nil
}

// TestWorkspaceStatusSegment checks the active workspace label on the status row.
func TestWorkspaceStatusSegment(t *testing.T) {
	m := statusViewModel(t, nil)
	if _, ok := m.workspaceSegment(); ok {
		t.Fatal("a direct launch shows a workspace segment")
	}
	m.SetWorkspacePort(&fakeWorkspacePort{})
	m.SetWorkspaceLabel("gvim")
	if row := statusRowText(m); !strings.Contains(row, "WS gvim") {
		t.Fatalf("status row %q has no workspace segment", row)
	}
}

// TestWorkspacePaletteActions checks the workspace actions and the switcher overlay.
func TestWorkspacePaletteActions(t *testing.T) {
	m := statusViewModel(t, nil)
	m.SetWorkspacePort(&fakeWorkspacePort{entries: []WorkspaceEntry{
		{ID: "id1", Label: "one", Root: "/one", Materialized: true, Active: true},
		{ID: "id2", Label: "two", Root: "/two"},
	}})
	// No port: no action table row is claimed.
	local := statusViewModel(t, nil)
	if _, ok := local.workspaceAction("workspace_switch"); ok {
		t.Fatal("a direct launch claimed a workspace action")
	}
	for _, action := range []string{"workspace_switch", "workspace_new", "workspace_close", "workspace_rename"} {
		cmd, ok := m.workspaceAction(action)
		if !ok || cmd != nil {
			t.Fatalf("%s = %v, %v", action, cmd, ok)
		}
		if len(m.overlays) == 0 {
			t.Fatalf("%s opened no overlay", action)
		}
		if v, isList := m.overlays[len(m.overlays)-1].(*list.View); isList {
			if action == "workspace_new" {
				t.Fatalf("%s opened a list, want a prompt", action)
			}
			if v.Title == "" {
				t.Fatalf("%s list has no title", action)
			}
		} else if _, isPrompt := m.overlays[len(m.overlays)-1].(*prompt.Line); !isPrompt {
			t.Fatalf("%s opened %T", action, m.overlays[len(m.overlays)-1])
		}
		m.overlays = nil
	}
	// Next runs immediately.
	if cmd, ok := m.workspaceAction("workspace_next"); !ok || cmd != nil {
		t.Fatalf("workspace_next = %v, %v", cmd, ok)
	}
}

// TestWorkspaceNewPromptFlow checks the root → label prompt flow calls Create.
func TestWorkspaceNewPromptFlow(t *testing.T) {
	m := statusViewModel(t, nil)
	port := &fakeWorkspacePort{}
	m.SetWorkspacePort(port)
	m.openNewWorkspacePrompt()
	if _, ok := m.workspaceActionAnswer(prompt.Done{Tag: workspaceNewRootTag{}, Outcome: prompt.Submitted, Text: "/tmp/project"}); !ok {
		t.Fatal("the root answer was not a workspace answer")
	}
	if port.created != "" {
		t.Fatal("Create ran before the label answer")
	}
	if _, ok := m.workspaceActionAnswer(prompt.Done{Tag: workspaceNewLabelTag{root: "/tmp/project"}, Outcome: prompt.Submitted, Text: "mine"}); !ok {
		t.Fatal("the label answer was not a workspace answer")
	}
	if port.createRoot != "/tmp/project" || port.createLabel != "mine" {
		t.Fatalf("Create(%q, %q)", port.createRoot, port.createLabel)
	}
	// Rename.
	m.workspaceActionAnswer(prompt.Done{Tag: workspaceRenameTag{id: "id1"}, Outcome: prompt.Submitted, Text: "renamed"})
	if port.renamed != "id1" || port.renameLabel != "renamed" {
		t.Fatalf("Rename(%q, %q)", port.renamed, port.renameLabel)
	}
}

// TestWorkspaceHostSwap checks the daemon host: switching models keeps one program and
// routes later messages to the shown model; Do runs on the UI path.
func TestWorkspaceHostSwap(t *testing.T) {
	one := statusViewModel(t, nil)
	two := statusViewModel(t, nil)
	h := NewWorkspaceHost(one)
	if h.Active() != one {
		t.Fatal("the host starts on the wrong model")
	}
	if _, cmd := h.Update(tea.WindowSizeMsg{Width: 100, Height: 30}); cmd != nil {
		_ = cmd
	}
	if one.width != 100 {
		t.Fatalf("the first model width = %d, want 100", one.width)
	}
	// No program yet: Do reports false instead of blocking.
	if h.Do(func() { t.Fatal("unreachable") }) {
		t.Fatal("Do succeeded without a program")
	}
	// Show switches; the next Update batches the new model's Init.
	h.Show(two)
	next, cmd := h.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if host, ok := next.(*WorkspaceHost); !ok || host != h {
		t.Fatalf("Update returned %T, want the host", next)
	}
	if h.Active() != two || two.width != 120 {
		t.Fatalf("the switch did not route the message: active=%p two=%p width=%d", h.Active(), two, two.width)
	}
	_ = cmd // the batch carries the new model's Init
}

// TestStartWorkspaceLaunchStartsInDock checks the queued Import launch starts the attach
// program as an ephemeral dock tab named by the discovery row.
func TestStartWorkspaceLaunchStartsInDock(t *testing.T) {
	starter := newFakeStarter()
	m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: starter})
	before := m.dock.Count()
	launch := WorkspaceLaunch{
		Argv: []string{"tmux", "attach", "-t", "work"},
		Name: "tmux:work", Ephemeral: true, Source: "tmux:default/work",
	}
	cmd := m.StartWorkspaceLaunch(launch)
	if cmd == nil {
		t.Fatal("StartWorkspaceLaunch returned no command")
	}
	dockSettle(t, m, cmd)
	if m.dock.Count() != before+1 {
		t.Fatalf("dock tabs = %d, want %d", m.dock.Count(), before+1)
	}
	if len(starter.launches) != 1 {
		t.Fatalf("StartLaunch calls = %d, want 1", len(starter.launches))
	}
	got := starter.launches[0]
	if strings.Join(got.Argv, " ") != "tmux attach -t work" || !got.Ephemeral {
		t.Fatalf("launch = %+v", got)
	}
	if names := m.dock.TabNames(); len(names) == 0 || names[len(names)-1] != "tmux:work" {
		t.Fatalf("tab names = %v, want the imported name", names)
	}
	// An empty argv is a no-op.
	if cmd := m.StartWorkspaceLaunch(WorkspaceLaunch{Name: "x"}); cmd != nil {
		t.Fatal("an empty launch returned a command")
	}
}
