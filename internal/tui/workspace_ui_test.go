package tui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/ctxmenu"
	"github.com/tbxark/gvim/internal/tui/list"
	"github.com/tbxark/gvim/internal/tui/prompt"
	"github.com/tbxark/gvim/theme"
)

// fakeWorkspacePort records the TUI calls of the workspace UI.
type fakeWorkspacePort struct {
	entries                                  []WorkspaceEntry
	selected, created, closed, renamed, next string
	createRoot, createLabel                  string
	renameLabel                              string
	extracted                                bool
	extractErr                               error
	targets                                  []WorkspaceEntry
	extractTo, extractTerminalTo             string
	extractMany                              []string
	mixSelection                             ExtractSelection
	mixTarget                                string
	mixReplace                               bool
	mixCmd                                   WorkspaceCmd
	running                                  []WorkspaceRunningTerminal
	runningQueried                           string
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
func (f *fakeWorkspacePort) ExtractTab() (WorkspaceCmd, error) {
	f.extracted = true
	return nil, f.extractErr
}
func (f *fakeWorkspacePort) RunningTerminals(id string) []WorkspaceRunningTerminal {
	f.runningQueried = id
	return f.running
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

// TestWorkspaceHostReplaysSizeOnSwitch checks a shown model gets the last known window
// size (a daemon host has no terminal to ask): without the replay the new workspace paints
// at width 0 after a switch.
func TestWorkspaceHostReplaysSizeOnSwitch(t *testing.T) {
	one := statusViewModel(t, nil)
	two := statusViewModel(t, nil)
	h := NewWorkspaceHost(one)
	h.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	h.Show(two)
	_, cmd := h.Update(struct{}{})
	// Run the pending batch as the program loop would and feed the messages back.
	var run func(tea.Cmd)
	var msgs []tea.Msg
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		msg := c()
		switch m := msg.(type) {
		case nil:
		case tea.BatchMsg:
			for _, sub := range m {
				run(sub)
			}
		default:
			msgs = append(msgs, msg)
		}
	}
	run(cmd)
	for _, msg := range msgs {
		_, next := h.Update(msg)
		run(next)
	}
	if two.width != 100 || two.height != 30 {
		t.Fatalf("the shown model size = %dx%d, want 100x30", two.width, two.height)
	}
}

// TestFileRootMarksThePaletteRemote checks the app's remote file root reaches the model
// (the palette then uses it and skips the local preview).
func TestFileRootMarksThePaletteRemote(t *testing.T) {
	m := New(Deps{Editor: newFakeEditor("hello"), Keys: testKeys(), Theme: theme.Theme{}, FileRoot: "/remote/w"})
	if m.fileRoot != "/remote/w" {
		t.Fatalf("fileRoot = %q", m.fileRoot)
	}
	local := New(Deps{Editor: newFakeEditor("hello"), Keys: testKeys(), Theme: theme.Theme{}})
	if local.fileRoot != "" {
		t.Fatalf("local fileRoot = %q", local.fileRoot)
	}
}

// TestWorkspaceExtractAction checks the extract palette row and action: it exists only
// when the workspace layer supports extracting, calls the port, and shows the layer's
// refusal without closing anything.
func TestWorkspaceExtractAction(t *testing.T) {
	m := statusViewModel(t, nil)
	// A layer without the optional extract surface gets no row and a notice.
	m.SetWorkspacePort(&fakeWorkspaces{})
	m.AddWorkspaceCommands()
	for _, c := range m.commands {
		if c.Action == "workspace_extract_tab" {
			t.Fatal("a layer without ExtractTab got the extract row")
		}
	}
	port := &fakeWorkspacePort{}
	m = statusViewModel(t, nil)
	m.SetWorkspacePort(port)
	m.AddWorkspaceCommands()
	found := false
	for _, c := range m.commands {
		if c.Action == "workspace_extract_tab" {
			found = true
			if c.Name != "Extract tab to new workspace" {
				t.Errorf("extract row name = %q", c.Name)
			}
		}
	}
	if !found {
		t.Fatal("the extract row is missing")
	}
	if cmd, ok := m.workspaceAction("workspace_extract_tab"); !ok || cmd != nil {
		t.Fatalf("workspace_extract_tab = %v, %v", cmd, ok)
	}
	if !port.extracted {
		t.Fatal("the extract action did not call the port")
	}
	// A refusal (no file path, large file, same root) comes back as an error the status
	// row shows; the action closes nothing.
	port.extractErr = errors.New("Cannot extract: buffer has no file path")
	m.workspaceAction("workspace_extract_tab")
	if !strings.Contains(m.notice, "no file path") {
		t.Fatalf("notice = %q", m.notice)
	}
}

// TestWorkspaceCloseAsksBeforeStoppingTerminals checks the close confirmation: a workspace
// with running terminals does not close until Yes, and the question names every terminal
// and agent. A workspace with none closes at once.
func TestWorkspaceCloseAsksBeforeStoppingTerminals(t *testing.T) {
	m := statusViewModel(t, nil)
	port := &fakeWorkspacePort{
		entries: []WorkspaceEntry{{ID: "id1", Label: "one"}},
		running: []WorkspaceRunningTerminal{{Name: "bash"}, {Name: "claude", Agent: "claude"}},
	}
	m.SetWorkspacePort(port)
	entry := WorkspaceEntry{ID: "id1", Label: "one"}
	if cmd := m.workspaceCloseEntry(entry); cmd != nil {
		t.Fatalf("close with running terminals returned a command instead of asking")
	}
	if port.closed != "" {
		t.Fatal("the workspace closed before the question was answered")
	}
	if len(m.overlays) == 0 {
		t.Fatal("no confirmation overlay")
	}
	if _, ok := m.overlays[len(m.overlays)-1].(*prompt.Confirm); !ok {
		t.Fatalf("overlay = %T, want the confirmation prompt", m.overlays[len(m.overlays)-1])
	}
	summary := workspaceRunningSummary(port.running)
	if summary != "2 running terminals: bash, claude (agent claude)" {
		t.Fatalf("summary = %q", summary)
	}
	// No: nothing closes.
	m.answer(prompt.Done{Tag: workspaceCloseRunningTag{id: "id1"}, Outcome: prompt.No})
	if port.closed != "" {
		t.Fatalf("No closed the workspace: %q", port.closed)
	}
	// Yes: the port closes the workspace.
	m.answer(prompt.Done{Tag: workspaceCloseRunningTag{id: "id1"}, Outcome: prompt.Yes})
	if port.closed != "id1" {
		t.Fatalf("Yes did not close: %q", port.closed)
	}

	// Without running terminals the close goes straight through.
	quiet := &fakeWorkspacePort{entries: []WorkspaceEntry{{ID: "id2", Label: "two"}}}
	m.SetWorkspacePort(quiet)
	if cmd := m.workspaceCloseEntry(WorkspaceEntry{ID: "id2", Label: "two"}); cmd != nil {
		t.Fatal("the direct close returned a command")
	}
	if quiet.closed != "id2" {
		t.Fatalf("the quiet workspace did not close: %q", quiet.closed)
	}
}

// TestTabMenuExtractRow checks Fresh's tab context menu item: present only for a workspace
// layer that extracts, disabled for a tab with no file, and activating the right-clicked
// tab before extracting.
func TestTabMenuExtractRow(t *testing.T) {
	m, ed := newCtxModel(t, "a.txt", "b.txt")
	m.SetWorkspacePort(&fakeWorkspacePort{})
	var row *ctxmenu.Item
	for i, it := range m.tabMenuItems(1) {
		if it.Label == "Extract to New Workspace" {
			row = &m.tabMenuItems(1)[i]
		}
	}
	if row == nil {
		t.Fatal("the tab menu has no extract row")
	}
	if row.Disabled {
		t.Fatal("the extract row is disabled for a file tab")
	}
	m.showContextMenu(tea.Mouse{X: 5, Y: 1}, m.tabMenuItems(1))
	chooseRow(t, m, "Extract to New Workspace")
	if !ed.tabs[1].Active {
		t.Fatal("the extracted tab was not activated")
	}
	// A tab with no path has the row disabled.
	m2, ed2 := newCtxModel(t, "a.txt")
	ed2.tabs[0].Path = ""
	m2.SetWorkspacePort(&fakeWorkspacePort{})
	for _, it := range m2.tabMenuItems(0) {
		if it.Label == "Extract to New Workspace" && !it.Disabled {
			t.Fatal("the extract row is enabled for a tab with no file")
		}
	}
}
