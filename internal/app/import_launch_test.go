package app

import (
	"slices"
	"testing"

	"github.com/tbxark/gvim/internal/terminal"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/internal/tui/dock"
)

// importFakeTerminal is the dock Terminal the imported attach tab runs under.
type importFakeTerminal struct{}

func (importFakeTerminal) Write(p []byte) (int, error) { return len(p), nil }
func (importFakeTerminal) Paste(string) error          { return nil }
func (importFakeTerminal) Resize(int, int) error       { return nil }
func (importFakeTerminal) Screen() terminal.Screen     { return terminal.NewScreen(0, 0, nil) }
func (importFakeTerminal) AppCursorKeys() bool         { return false }
func (importFakeTerminal) Updates() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}
func (importFakeTerminal) Done() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}
func (importFakeTerminal) Close() error { return nil }
func (importFakeTerminal) Scrollback(int, int) ([]terminal.Line, int) {
	return nil, 0
}
func (importFakeTerminal) ScrollbackLen() int { return 0 }
func (importFakeTerminal) SearchScrollback(string, int) ([]int, int) {
	return nil, 0
}
func (importFakeTerminal) SearchScrollbackMode(string, bool, bool, int) ([]int, int, error) {
	return nil, 0, nil
}
func (importFakeTerminal) WorkingDirectory() string { return "" }
func (importFakeTerminal) AltScreen() bool          { return false }
func (importFakeTerminal) MouseReporting() (bool, bool, bool) {
	return false, false, false
}
func (importFakeTerminal) SendMouse(terminal.MouseEvent) {}

// importFakeStarter records the launches the dock starts.
type importFakeStarter struct {
	launches []terminal.Launch
	dirs     []string
}

func (s *importFakeStarter) Start(dir string, _, _ int) (dock.Terminal, error) {
	s.dirs = append(s.dirs, dir)
	return importFakeTerminal{}, nil
}

func (s *importFakeStarter) StartLaunch(launch terminal.Launch, dir string, _, _ int) (dock.Terminal, error) {
	s.launches = append(s.launches, launch)
	s.dirs = append(s.dirs, dir)
	return importFakeTerminal{}, nil
}

// TestImportedWorkspaceStartsTheAttachTerminal is the end-to-end check of the
// discovery Import: CreateWithLaunch materializes a real workspace runtime, and the
// launch the workspace layer applies to that runtime's model (the same call
// applyCommit queues) starts the selected session's attach terminal with its whole
// record. A repeated same-source import does not duplicate the tab; a second tmux
// session in the same root gets its own attach tab.
func TestImportedWorkspaceStartsTheAttachTerminal(t *testing.T) {
	starter := &importFakeStarter{}
	m, _ := newWorkspaceManagerTest(t)
	root := t.TempDir()
	attachA := []string{"tmux", "-S", "/tmp/tmux-501/default", "attach", "-t", "a"}
	launchA := tui.WorkspaceLaunch{
		Argv: attachA, Name: "tmux:a", Agent: "claude", ResumeArgv: attachA,
		Ephemeral: true, Dir: root, Source: "tmux:s/a",
	}
	cmd, err := m.CreateWithLaunch(root, "tmux:a", launchA)
	if err != nil || cmd == nil {
		t.Fatalf("CreateWithLaunch = %v, %v", cmd, err)
	}
	cmd() // create + materialize + switch (the real runtime build)
	target := m.ForegroundModel()
	if target == nil {
		t.Fatal("the imported workspace has no foreground model")
	}
	// Install a recording dock starter on the real materialized model: the dock
	// ports built by buildRuntime would spawn a real process, which a unit test
	// must not do. This is the same SetDock the app calls; everything below runs
	// the dock's real start path.
	target.SetDock(tui.DockPorts{Starter: starter})
	if len(m.Entries()) != 2 {
		t.Fatalf("entries = %+v, want the foreground and the imported workspace", m.Entries())
	}
	// The workspace layer applies the launch to the materialized model's dock.
	start := target.StartWorkspaceLaunch(launchA)
	if start == nil {
		t.Fatal("StartWorkspaceLaunch on the imported workspace returned no command")
	}
	start()
	if len(starter.launches) != 1 {
		t.Fatalf("attach launches = %+v, want one", starter.launches)
	}
	if got := starter.launches[0]; !slices.Equal(got.Argv, attachA) || !slices.Equal(got.ResumeArgv, attachA) ||
		got.Agent != "claude" || !got.Ephemeral || got.Source != "tmux:s/a" {
		t.Fatalf("attach record = %+v", got)
	}
	if got := starter.dirs[0]; got != root {
		t.Fatalf("attach dir = %q, want the imported session directory", got)
	}
	// A repeated import of the same source focuses the existing tab: no duplicate.
	if cmd := target.StartWorkspaceLaunch(launchA); cmd != nil {
		t.Fatalf("repeated import started a command: %v", cmd)
	}
	if len(starter.launches) != 1 {
		t.Fatalf("repeated import duplicated the attach tab: %+v", starter.launches)
	}
	// A second tmux session in the same root is not suppressed: its own attach tab.
	attachB := []string{"tmux", "-S", "/tmp/tmux-501/default", "attach", "-t", "b"}
	launchB := tui.WorkspaceLaunch{
		Argv: attachB, Name: "tmux:b", ResumeArgv: attachB, Ephemeral: true,
		Dir: root, Source: "tmux:s/b",
	}
	cmd, err = m.CreateWithLaunch(root, "tmux:b", launchB)
	if err != nil || cmd == nil {
		t.Fatalf("same-root CreateWithLaunch = %v, %v", cmd, err)
	}
	cmd() // the workspace already exists: this reuses and switches to it
	if len(m.Entries()) != 2 {
		t.Fatalf("same-root import created a workspace: %+v", m.Entries())
	}
	start = m.ForegroundModel().StartWorkspaceLaunch(launchB)
	if start == nil {
		t.Fatal("the second same-root launch returned no command")
	}
	start()
	if len(starter.launches) != 2 || !slices.Equal(starter.launches[1].Argv, attachB) {
		t.Fatalf("second same-root attach = %+v", starter.launches)
	}
	if got := starter.launches[1].Source; got != "tmux:s/b" {
		t.Fatalf("second attach source = %q", got)
	}
}
