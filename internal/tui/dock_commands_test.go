package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/terminal"
	"github.com/tbxark/gvim/theme"
)

// shellsStarter hands out a new fake shell for each start, so a test can tell the
// shell a restart closes from the shell it starts. A start fails with err when it is set.
type shellsStarter struct {
	terms      []*fakeTerm
	dirs       []string
	launches   []terminal.Launch
	launchDirs []string
	err        error
	// makeTerm builds each started shell; nil starts a plain newFakeTerm().
	makeTerm func() *fakeTerm
}

func (s *shellsStarter) Start(dir string, cols, rows int) (Terminal, error) {
	s.dirs = append(s.dirs, dir)
	if s.err != nil {
		return nil, s.err
	}
	t := newFakeTerm()
	if s.makeTerm != nil {
		t = s.makeTerm()
	}
	s.terms = append(s.terms, t)
	return t, nil
}

// StartLaunch starts a recorded program (the restart/resume path).
func (s *shellsStarter) StartLaunch(launch terminal.Launch, dir string, cols, rows int) (Terminal, error) {
	s.launches = append(s.launches, launch)
	s.launchDirs = append(s.launchDirs, dir)
	return s.Start(dir, cols, rows)
}

// rootTree is the fake tree with a root the test can move, as a project switch does.
type rootTree struct {
	*fakeTree
	root string
}

func (t *rootTree) Root() string { return t.root }

// terminalKeys is dockKeys with a key bound to each terminal command, in the editor
// context and in the terminal context, so a test can press them.
func terminalKeys() fakeKeymap {
	k := dockKeys()
	k["normal|t|alt"] = Command{Action: "open_terminal"}
	k["normal|b|alt"] = Command{Action: "open_terminal_below"}
	k["normal|r|alt"] = Command{Action: "open_terminal_right"}
	k["normal|f|alt"] = Command{Action: "focus_terminal"}
	k["normal|k|alt"] = Command{Action: "restart_terminal"}
	k["terminal|k|alt"] = Command{Action: "restart_terminal"}
	return k
}

// newTerminalModel returns a model over a fake editor, with tree, keys and the shell
// starter, sized 80 by 24.
func newTerminalModel(t *testing.T, tree ExplorerTree, keys Keymap, starter TerminalStarter) *Model {
	t.Helper()
	m := New(Deps{Editor: newFakeEditor("hello"), Explorer: tree, Keys: keys, Theme: theme.Theme{}, Clock: testClock})
	m.SetDock(DockPorts{Starter: starter})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return m
}

func TestTerminalCommandsRunFromThePalette(t *testing.T) {
	tests := []struct {
		name      string
		setup     []string
		action    string
		open      bool
		focus     bool
		starts    int
		noticeHas string // a substring of the status notice; empty means no notice
	}{
		{name: "open_terminal shows and focuses a shell", action: "open_terminal", open: true, focus: true, starts: 1, noticeHas: "Terminal opened below."},
		{name: "open_terminal_below shows and focuses the dock below", action: "open_terminal_below", open: true, focus: true, starts: 1, noticeHas: "Terminal opened below."},
		{name: "open_terminal_right falls back to the dock below and says so", action: "open_terminal_right", open: true, focus: true, starts: 1, noticeHas: "Terminal stays below"},
		{name: "focus_terminal opens a hidden dock", action: "focus_terminal", open: true, focus: true, starts: 1},
		{name: "focus_terminal gives the focus back to a shown dock", setup: []string{"open_terminal", "focus_editor"}, action: "focus_terminal", open: true, focus: true, starts: 1},
		{name: "focus_terminal with a shell running keeps that shell", setup: []string{"open_terminal"}, action: "focus_terminal", open: true, focus: true, starts: 1},
		{name: "restart_terminal starts a shell when none ran", action: "restart_terminal", open: true, focus: true, starts: 1},
		{name: "restart_terminal replaces the running shell", setup: []string{"open_terminal"}, action: "restart_terminal", open: true, focus: true, starts: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			starter := &shellsStarter{}
			m := newTerminalModel(t, newFakeTree(3), dockKeys(), starter)
			for _, a := range tt.setup {
				dockRun(t, m, a)
			}
			// The notice the setup left is not the action's, so the action starts with none.
			m.notice = ""
			dockRun(t, m, tt.action)
			if tt.noticeHas == "" && m.notice != "" {
				t.Fatalf("notice = %q, want none", m.notice)
			}
			if tt.noticeHas != "" && !strings.Contains(m.notice, tt.noticeHas) {
				t.Fatalf("notice = %q, want it to contain %q", m.notice, tt.noticeHas)
			}
			if m.dock.Open() != tt.open || m.dock.Focus() != tt.focus {
				t.Fatalf("open, focus = %v, %v; want %v, %v", m.dock.Open(), m.dock.Focus(), tt.open, tt.focus)
			}
			if len(starter.dirs) != tt.starts {
				t.Fatalf("shell starts = %d, want %d", len(starter.dirs), tt.starts)
			}
		})
	}
}

func TestTerminalCommandsRunFromTheKeymap(t *testing.T) {
	tests := []struct {
		name          string
		setup         []string
		focusExplorer bool
		key           tea.KeyPressMsg
		open          bool
		focus         bool
		starts        int
	}{
		{name: "Alt+T opens the dock with a shell", key: altKey('t'), open: true, focus: true, starts: 1},
		{name: "Alt+B opens the dock below", key: altKey('b'), open: true, focus: true, starts: 1},
		{name: "Alt+R opens the dock below, as the right fallback", key: altKey('r'), open: true, focus: true, starts: 1},
		{name: "Alt+F opens a hidden dock", key: altKey('f'), open: true, focus: true, starts: 1},
		{name: "Alt+F gives the focus back to a shown dock", setup: []string{"open_terminal", "focus_editor"}, key: altKey('f'), open: true, focus: true, starts: 1},
		{name: "Alt+K restarts the shell from the editor", setup: []string{"open_terminal", "focus_editor"}, key: altKey('k'), open: true, focus: true, starts: 2},
		{name: "Alt+K restarts the shell from the dock", setup: []string{"open_terminal"}, key: altKey('k'), open: true, focus: true, starts: 2},
		{name: "Alt+T from the explorer opens the dock", setup: []string{"focus_file_explorer"}, key: altKey('t'), open: true, focus: true, starts: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			starter := &shellsStarter{}
			m := newTerminalModel(t, newFakeTree(3), terminalKeys(), starter)
			for _, a := range tt.setup {
				dockRun(t, m, a)
			}
			dockSettle(t, m, press(m, tt.key))
			if m.dock.Open() != tt.open || m.dock.Focus() != tt.focus {
				t.Fatalf("open, focus = %v, %v; want %v, %v", m.dock.Open(), m.dock.Focus(), tt.open, tt.focus)
			}
			if len(starter.dirs) != tt.starts {
				t.Fatalf("shell starts = %d, want %d", len(starter.dirs), tt.starts)
			}
		})
	}
}

func TestTerminalCommandsStartInTheWorkspaceRoot(t *testing.T) {
	for _, action := range []string{"open_terminal", "open_terminal_below", "open_terminal_right", "focus_terminal", "restart_terminal"} {
		t.Run(action, func(t *testing.T) {
			starter := &shellsStarter{}
			m := newTerminalModel(t, newFakeTree(3), dockKeys(), starter)
			dockRun(t, m, action)
			if !slices.Equal(starter.dirs, []string{"/ws"}) {
				t.Fatalf("start directories = %q, want [/ws]", starter.dirs)
			}
		})
	}
}

func TestTerminalCommandsLayOutTheDockBelowTheBody(t *testing.T) {
	// At 80 by 24 the dock takes the layout's rows at the bottom: the title row
	// at row 17 and 5 shell rows, as open_terminal_in_dock does. open_terminal_below
	// and open_terminal_right use the same rows, so they keep the layout's split.
	for _, action := range []string{"open_terminal_in_dock", "open_terminal", "open_terminal_below", "open_terminal_right"} {
		t.Run(action, func(t *testing.T) {
			m := newTerminalModel(t, newFakeTree(3), dockKeys(), &shellsStarter{})
			dockRun(t, m, action)
			l := m.layout()
			if !l.DockOn || l.DockY != 17 || l.DockH != 6 || l.W != 80 {
				t.Fatalf("dock on = %v at row %d with %d rows over %d columns; want on at row 17 with 6 rows over 80 columns",
					l.DockOn, l.DockY, l.DockH, l.W)
			}
			if l.BodyBottom != 16 {
				t.Fatalf("bodyBottom = %d, want 16: the editor body stops above the dock", l.BodyBottom)
			}
		})
	}
}

func TestFocusTerminalWithNoRunningShellNotifies(t *testing.T) {
	tests := []struct {
		name  string
		fail  bool // the shell cannot start
		setup func(t *testing.T, m *Model, s *shellsStarter)
	}{
		{
			name:  "a start that failed",
			fail:  true,
			setup: func(t *testing.T, m *Model, s *shellsStarter) { openDock(t, m) },
		},
		{
			name: "a shell that exited",
			setup: func(t *testing.T, m *Model, s *shellsStarter) {
				openDock(t, m)
				close(s.terms[0].done)
				dockSettle(t, m, nil)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			starter := &shellsStarter{}
			if tt.fail {
				starter.err = errors.New("no shell")
			}
			m := newTerminalModel(t, newFakeTree(3), dockKeys(), starter)
			tt.setup(t, m, starter)
			starts := len(starter.dirs)
			dockRun(t, m, "focus_terminal")
			if m.dock.Focus() {
				t.Fatal("the dock took the focus with no shell running")
			}
			if len(starter.dirs) != starts {
				t.Fatalf("shell starts = %d, want %d: focus_terminal must not start a shell", len(starter.dirs), starts)
			}
			if m.notice != noTerminalNotice() {
				t.Fatalf("notice = %q, want %q", m.notice, noTerminalNotice())
			}
		})
	}
}

func TestRestartTerminalClosesTheShellAndStartsAnotherInItsDirectory(t *testing.T) {
	tree := &rootTree{fakeTree: newFakeTree(3), root: "/ws"}
	starter := &shellsStarter{}
	m := newTerminalModel(t, tree, dockKeys(), starter)
	openDock(t, m)

	// The workspace root moves on. The restart keeps the directory the shell started in.
	tree.root = "/other"
	dockRun(t, m, "restart_terminal")

	if !slices.Equal(starter.dirs, []string{"/ws", "/ws"}) {
		t.Fatalf("start directories = %q, want the first shell's directory twice", starter.dirs)
	}
	if len(starter.terms) != 2 {
		t.Fatalf("shells started = %d, want 2", len(starter.terms))
	}
	if !starter.terms[0].closed {
		t.Fatal("the running shell was not closed by the restart")
	}
	if starter.terms[1].closed {
		t.Fatal("the new shell was closed")
	}
	if m.dock.Exited() || !m.dock.Running() {
		t.Fatal("the dock does not run the new shell")
	}
	if !m.dock.Open() || !m.dock.Focus() {
		t.Fatalf("open, focus = %v, %v; want the dock shown with the focus", m.dock.Open(), m.dock.Focus())
	}
}

func TestRestartTerminalReplacesAnExitedShell(t *testing.T) {
	starter := &shellsStarter{}
	m := newTerminalModel(t, newFakeTree(3), dockKeys(), starter)
	openDock(t, m)
	close(starter.terms[0].done)
	dockSettle(t, m, nil)
	if !m.dock.Exited() {
		t.Fatal("precondition: the shell has exited")
	}

	dockRun(t, m, "restart_terminal")
	if len(starter.terms) != 2 || m.dock.Exited() || !m.dock.Running() {
		t.Fatalf("shells = %d, exited = %v; want a new shell running in place of the exited one",
			len(starter.terms), m.dock.Exited())
	}
}

func TestRestartTerminalWhileAStartIsInFlightStartsNothingElse(t *testing.T) {
	starter := &shellsStarter{}
	m := newTerminalModel(t, newFakeTree(3), dockKeys(), starter)
	pending := m.runCommand("open_terminal")
	dockRun(t, m, "restart_terminal")
	dockSettle(t, m, pending)
	if len(starter.dirs) != 1 {
		t.Fatalf("shell starts = %d, want 1: a restart must not start a second shell over one in flight", len(starter.dirs))
	}
}

func TestRestartTerminalWithoutAStarterShowsTheNotice(t *testing.T) {
	m := newTerminalModel(t, newFakeTree(3), dockKeys(), nil)
	dockRun(t, m, "restart_terminal")
	if m.dock.Running() {
		t.Fatal("a shell runs in a build with no starter")
	}
	if m.dock.Notice() != noTerminalsNotice {
		t.Fatalf("dock notice = %q, want %q", m.dock.Notice(), noTerminalsNotice)
	}
}

func TestOpenTerminalCommandsKeepTheRunningShell(t *testing.T) {
	tests := []struct {
		name   string
		action string
		hide   bool   // the dock is hidden before the command; its shell keeps running
		notice string // the status row's notice after the command
	}{
		{name: "Open Terminal focuses the running shell", action: "open_terminal", notice: "Shell kept; terminal below."},
		{name: "Open Terminal shows a hidden dock with its shell", action: "open_terminal", hide: true, notice: "Shell kept; terminal below."},
		{name: "Open Terminal Below keeps the shell below the editor", action: "open_terminal_below", notice: "Shell kept; terminal below."},
		{name: "Open Terminal Below shows a hidden dock with its shell", action: "open_terminal_below", hide: true, notice: "Shell kept; terminal below."},
		{name: "Open Terminal to the Right keeps the bottom position", action: "open_terminal_right", notice: "Terminal stays below: no right column yet."},
		{name: "Open Terminal to the Right shows a hidden dock below", action: "open_terminal_right", hide: true, notice: "Terminal stays below: no right column yet."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			starter := &shellsStarter{}
			m := newTerminalModel(t, newFakeTree(3), dockKeys(), starter)
			openDock(t, m)
			shell := m.dock.Running()
			if tt.hide {
				dockRun(t, m, "toggle_utility_dock")
			} else {
				dockRun(t, m, "focus_editor")
			}

			dockRun(t, m, tt.action)

			if m.dock.Running() != shell {
				t.Fatal("the command replaced the running shell")
			}
			if len(starter.dirs) != 1 {
				t.Fatalf("shell starts = %d, want 1", len(starter.dirs))
			}
			if !m.dock.Open() || !m.dock.Focus() {
				t.Fatalf("open, focus = %v, %v; want the dock shown with the focus", m.dock.Open(), m.dock.Focus())
			}
			if l := m.layout(); !l.DockOn || l.DockY != 17 || l.DockH != 6 || l.DockY+l.DockH != l.StatusY {
				t.Fatalf("dock on = %v at row %d with %d rows; want on at row 17 with 6 rows, directly above the status row",
					l.DockOn, l.DockY, l.DockH)
			}
			if m.notice != tt.notice {
				t.Fatalf("notice = %q, want %q", m.notice, tt.notice)
			}
		})
	}
}

func TestOpenTerminalCommandsSayWhenTheFrameHasNoRoom(t *testing.T) {
	for _, action := range []string{"open_terminal", "open_terminal_below"} {
		t.Run(action, func(t *testing.T) {
			m := newTerminalModel(t, newFakeTree(3), dockKeys(), &shellsStarter{})
			// At 9 rows the editor body would keep only 2 rows under the dock, so the dock is not laid out.
			m.Update(tea.WindowSizeMsg{Width: 80, Height: 9})
			dockRun(t, m, action)
			if m.layout().DockOn {
				t.Fatal("the dock is laid out in a 9-row frame")
			}
			if m.notice != "No room for the terminal." {
				t.Fatalf("notice = %q, want the no-room notice", m.notice)
			}
		})
	}
}

func TestOpenTerminalWithoutAStarterShowsTheNotice(t *testing.T) {
	for _, action := range []string{"open_terminal", "open_terminal_below", "open_terminal_right"} {
		t.Run(action, func(t *testing.T) {
			m := newTerminalModel(t, newFakeTree(3), dockKeys(), nil)
			dockRun(t, m, action)
			if m.notice != noTerminalsNotice {
				t.Fatalf("notice = %q, want %q: the dock cannot start a shell", m.notice, noTerminalsNotice)
			}
		})
	}
}

func TestOpenTerminalNoticeSurvivesTheKeyPress(t *testing.T) {
	m := newTerminalModel(t, newFakeTree(3), terminalKeys(), &shellsStarter{})
	openDock(t, m)
	dockRun(t, m, "focus_editor")
	dockSettle(t, m, press(m, altKey('t')))
	if m.notice != "Shell kept; terminal below." {
		t.Fatalf("notice = %q, want the shell-kept notice after the key press", m.notice)
	}
}

func TestOpenTerminalNoticeShowsInTheStatusRow(t *testing.T) {
	tests := []struct {
		name   string
		setup  []string
		action string
		want   string // the start of the notice, which the status row shows at 80 columns
	}{
		{name: "a new shell", action: "open_terminal", want: "Terminal opened below."},
		{name: "a running shell", setup: []string{"open_terminal_in_dock"}, action: "open_terminal", want: "Shell kept"},
		{name: "Open Terminal to the Right", action: "open_terminal_right", want: "Terminal stays below"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTerminalModel(t, newFakeTree(3), dockKeys(), &shellsStarter{})
			for _, a := range tt.setup {
				dockRun(t, m, a)
			}
			dockRun(t, m, tt.action)
			if !frameRowHas(m, tt.want) {
				t.Fatalf("no row of the frame shows %q", tt.want)
			}
		})
	}
}
