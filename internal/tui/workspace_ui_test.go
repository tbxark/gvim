package tui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

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
	port := &fakeWorkspacePort{entries: []WorkspaceEntry{
		{ID: "id1", Label: "one", Root: "/one", Materialized: true, Active: true},
		{ID: "id2", Label: "two", Root: "/two"},
	}}
	m.SetWorkspacePort(port)
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
	// Next runs in a command: the port may wait on the UI goroutine, so Update must not call it.
	cmd, ok := m.workspaceAction("workspace_next")
	if !ok || cmd == nil {
		t.Fatalf("workspace_next = %v, %v", cmd, ok)
	}
	if port.next != "" {
		t.Fatal("Next ran inside Update, on the UI goroutine")
	}
	cmd()
	if port.next != "yes" {
		t.Fatal("the Next command did not call the port")
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
	cmd, _ := m.workspaceActionAnswer(prompt.Done{Tag: workspaceRenameTag{id: "id1"}, Outcome: prompt.Submitted, Text: "renamed"})
	if port.renamed != "" {
		t.Fatal("Rename ran inside Update, on the UI goroutine")
	}
	if cmd == nil {
		t.Fatal("Rename returned no command")
	}
	cmd()
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
	if cmd := m.answer(prompt.Done{Tag: workspaceCloseRunningTag{id: "id1"}, Outcome: prompt.Yes}); cmd != nil {
		cmd()
	}
	if port.closed != "id1" {
		t.Fatalf("Yes did not close: %q", port.closed)
	}

	// Without running terminals the close goes straight through.
	quiet := &fakeWorkspacePort{entries: []WorkspaceEntry{{ID: "id2", Label: "two"}}}
	m.SetWorkspacePort(quiet)
	if cmd := m.workspaceCloseEntry(WorkspaceEntry{ID: "id2", Label: "two"}); cmd != nil {
		cmd()
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

type pathMultiFake struct {
	*multiFake
	paths []string
}

func (f *pathMultiFake) Tabs() []TabInfo {
	tabs := f.multiFake.Tabs()
	for i := range tabs {
		if i < len(f.paths) {
			tabs[i].Path = f.paths[i]
		}
	}
	return tabs
}

func (f *pathMultiFake) CloseTab(i int, force bool) error {
	if err := f.multiFake.CloseTab(i, force); err != nil {
		return err
	}
	f.paths = append(f.paths[:i], f.paths[i+1:]...)
	return nil
}

func TestCloseTabForWorkspaceExtractUpdatesSplitIndicesAndNilActiveSafe(t *testing.T) {
	t.Parallel()
	ed := &pathMultiFake{
		multiFake: newMultiFake(3, 2),
		paths:     []string{"/ws/0.txt", "/ws/1.txt", "/ws/2.txt"},
	}
	m := New(Deps{Editor: ed, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	runAction(t, m, "split_vertical")
	// Put tab 2 in the right leaf while focused left leaf A holds tab 0, then extract-close tab 0.
	m.focusLeaf(m.splits.Root.A)
	m.splits.Root.B.Tab = 2
	if !m.CloseTabForWorkspaceExtract("/ws/0.txt") {
		t.Fatal("CloseTabForWorkspaceExtract returned false")
	}
	if m.splits.Root.B.Tab != 1 {
		t.Fatalf("right leaf Tab = %d after closing tab 0, want 1", m.splits.Root.B.Tab)
	}
	for i, leaf := range m.splits.Root.Leaves() {
		if leaf.Tab < 0 || leaf.Tab >= len(ed.Tabs()) {
			t.Fatalf("leaf %d has out-of-range Tab %d (len %d)", i, leaf.Tab, len(ed.Tabs()))
		}
	}
	if !m.CloseTabForWorkspaceExtractAt("/ws/2.txt") {
		t.Fatal("CloseTabForWorkspaceExtractAt returned false")
	}
	var empty WorkspaceHost
	if next, cmd := empty.Update(tea.WindowSizeMsg{Width: 80, Height: 24}); next != &empty || cmd != nil {
		t.Fatalf("nil-active WorkspaceHost.Update = %v, %v", next, cmd)
	}
	_ = empty.View()
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestWorkspaceHostIgnoresZeroWindowSize(t *testing.T) {
	t.Parallel()
	m := statusViewModel(t, nil)
	h := NewWorkspaceHost(m)
	h.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if h.width != 100 || h.height != 30 || m.width != 100 || m.height != 30 {
		t.Fatalf("initial size = host %dx%d model %dx%d, want 100x30", h.width, h.height, m.width, m.height)
	}

	cases := []struct {
		name  string
		msg   tea.Msg
		keep  bool
		wantW int
		wantH int
	}{
		{name: "zero both", msg: tea.WindowSizeMsg{Width: 0, Height: 0}, keep: false, wantW: 100, wantH: 30},
		{name: "zero width", msg: tea.WindowSizeMsg{Width: 0, Height: 30}, keep: false, wantW: 100, wantH: 30},
		{name: "zero height", msg: tea.WindowSizeMsg{Width: 100, Height: 0}, keep: false, wantW: 100, wantH: 30},
		{name: "negative width", msg: tea.WindowSizeMsg{Width: -1, Height: 30}, keep: false, wantW: 100, wantH: 30},
		{name: "positive size", msg: tea.WindowSizeMsg{Width: 120, Height: 40}, keep: true, wantW: 120, wantH: 40},
	}

	if got := ignoreZeroWindowSize(h, struct{}{}); got == nil {
		t.Fatal("ignoreZeroWindowSize dropped a non-size message")
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			filtered := ignoreZeroWindowSize(h, tc.msg)
			if tc.keep && filtered != tc.msg {
				t.Fatalf("ignoreZeroWindowSize(%#v) = %#v, want passthrough", tc.msg, filtered)
			}
			if !tc.keep && filtered != nil {
				t.Fatalf("ignoreZeroWindowSize(%#v) = %#v, want nil", tc.msg, filtered)
			}
			h.Update(tc.msg)
			if h.width != tc.wantW || h.height != tc.wantH || m.width != tc.wantW || m.height != tc.wantH {
				t.Fatalf("after %#v: host=%dx%d model=%dx%d, want %dx%d",
					tc.msg, h.width, h.height, m.width, m.height, tc.wantW, tc.wantH)
			}
		})
	}
}

func TestRunWorkspaceHostDropsZeroWindowSize(t *testing.T) {
	t.Parallel()
	m := New(Deps{Editor: newFakeEditor("ZERO-SIZE-GUARD"), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	h := NewWorkspaceHost(m)
	inR, inW := io.Pipe()
	out := &syncBuffer{}
	ctx, cancel := context.WithCancel(context.Background())
	progCh := make(chan *tea.Program, 1)
	done := make(chan error, 1)
	go func() {
		done <- RunWorkspaceHost(ctx, h, inR, out, func(p *tea.Program) { progCh <- p })
	}()

	var p *tea.Program
	select {
	case p = <-progCh:
	case <-time.After(5 * time.Second):
		cancel()
		_ = inW.Close()
		_ = inR.Close()
		t.Fatal("RunWorkspaceHost did not emit program")
	}

	t.Cleanup(func() {
		cancel()
		_ = inW.Close()
		_ = inR.Close()
		select {
		case err := <-done:
			_ = err
		case <-time.After(5 * time.Second):
		}
	})

	p.Send(tea.WindowSizeMsg{Width: 80, Height: 24})
	p.Send(tea.WindowSizeMsg{Width: 0, Height: 0})

	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), "ZERO-SIZE-GUARD") {
		if time.Now().After(deadline) {
			t.Fatalf("rendered output did not contain ZERO-SIZE-GUARD after 0x0 size message: %q", out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}

	p.Quit()
	cancel()
	_ = inW.Close()
	_ = inR.Close()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("RunWorkspaceHost: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunWorkspaceHost did not exit after Quit")
	}
	if m.width != 80 || m.height != 24 {
		t.Fatalf("model size = %dx%d, want 80x24", m.width, m.height)
	}
}

// TestWorkspaceOpReportsFailureOffTheUIGoroutine: Select, Next, Close and Rename run in a
// command, because the app's port waits on the UI goroutine; a failure comes back as a message
// that Update puts on the status row.
func TestWorkspaceOpReportsFailureOffTheUIGoroutine(t *testing.T) {
	t.Parallel()
	m := statusViewModel(t, nil)
	port := &fakeWorkspacePort{entries: []WorkspaceEntry{{ID: "id1", Label: "one"}, {ID: "id2", Label: "two"}}}
	m.SetWorkspacePort(port)
	m.openWorkspaceSwitcher()
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if cmd != nil {
		cmd()
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if port.selected != "" {
		t.Fatal("Select ran inside Update, on the UI goroutine")
	}
	if cmd == nil {
		t.Fatal("choosing a workspace returned no command")
	}
	cmd()
	if port.selected != "id2" {
		t.Fatalf("Select(%q), want id2", port.selected)
	}

	msg := workspaceOp(func() (WorkspaceCmd, error) { return nil, errors.New("workspace: the daemon is not ready") })()
	if _, ok := msg.(workspaceOpErrMsg); !ok {
		t.Fatalf("a failed operation returned %T, want workspaceOpErrMsg", msg)
	}
	m.Update(msg)
	if m.notice != "workspace: the daemon is not ready" {
		t.Fatalf("notice = %q", m.notice)
	}
}
