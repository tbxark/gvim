package tui

import (
	"errors"
	"image/color"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/terminal"
	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/palette"
	"github.com/tbxark/gvim/theme"
)

// The dock's notices, for the tests that assert what the status row says.
const (
	noTerminalsNotice   = "Terminals are not available in this build."
	noClipboardNotice   = "The clipboard is not available in this build."
	unsupportedNotice   = "Terminals are not supported on this platform yet."
	startingShellNotice = "Starting a shell..."
	captureOnNotice     = "Keyboard capture ON - all keys go to terminal (F9 to toggle)"
	captureOffNotice    = "Keyboard capture OFF - UI bindings active (F9 to toggle)"
)

// fakeTerm implements Terminal. It records what it is asked to write, paste and
// resize. Its Updates channel starts with one pending signal, so a wait for output
// returns at once. Its Done channel stays open until a test closes it.
type fakeTerm struct {
	writes    []string
	pastes    []string
	resizes   [][2]int
	closed    bool
	writeErr  error
	screen    terminal.Screen
	appCursor bool // the shell's DECCKM mode, reported by AppCursorKeys
	updates   chan struct{}
	done      chan struct{}
}

func newFakeTerm() *fakeTerm {
	f := &fakeTerm{
		screen:  terminal.NewScreen(0, 0, nil),
		updates: make(chan struct{}, 1),
		done:    make(chan struct{}),
	}
	f.updates <- struct{}{}
	return f
}

func (f *fakeTerm) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	f.writes = append(f.writes, string(p))
	return len(p), nil
}

func (f *fakeTerm) Paste(text string) error {
	f.pastes = append(f.pastes, text)
	return nil
}

func (f *fakeTerm) Resize(cols, rows int) error {
	f.resizes = append(f.resizes, [2]int{cols, rows})
	return nil
}

func (f *fakeTerm) Screen() terminal.Screen  { return f.screen }
func (f *fakeTerm) Updates() <-chan struct{} { return f.updates }
func (f *fakeTerm) Done() <-chan struct{}    { return f.done }
func (f *fakeTerm) AppCursorKeys() bool      { return f.appCursor }
func (f *fakeTerm) Close() error             { f.closed = true; return nil }

// fakeStarter implements TerminalStarter. It hands out term, or fails with err, and
// it records the directory and size of each start.
type fakeStarter struct {
	term  *fakeTerm
	err   error
	dirs  []string
	sizes [][2]int
}

func newFakeStarter() *fakeStarter {
	return &fakeStarter{term: newFakeTerm()}
}

func (s *fakeStarter) Start(dir string, cols, rows int) (Terminal, error) {
	s.dirs = append(s.dirs, dir)
	s.sizes = append(s.sizes, [2]int{cols, rows})
	if s.err != nil {
		return nil, s.err
	}
	return s.term, nil
}

// fakeClipboard implements ClipboardReader with a fixed text or error.
type fakeClipboard struct {
	text string
	err  error
}

func (c fakeClipboard) Read() (string, error) { return c.text, c.err }

// dockKeys is testKeys with the bindings of the terminal dock. The terminal context
// binds the keys that stay with the editor while the dock has focus.
func dockKeys() fakeKeymap {
	k := testKeys()
	k["normal|`|alt"] = Command{Action: "open_terminal_in_dock"}
	k["normal|j|alt"] = Command{Action: "toggle_utility_dock"}
	k["normal|o|alt"] = Command{Action: "toggle_dock_focus"}
	k["terminal|o|alt"] = Command{Action: "toggle_dock_focus"}
	k["terminal| |ctrl"] = Command{Action: "terminal_escape"}
	k["terminal|v|ctrl"] = Command{Action: "terminal_paste"}
	k["terminal|F9|"] = Command{Action: "toggle_keyboard_capture"}
	k["terminal|\\|ctrl"] = Command{Action: "toggle_keyboard_capture"}
	k["normal|Escape|"] = Command{Action: "cancel_selection"}
	return k
}

// newDockModel returns a model over ed with the explorer tree rooted at /ws, sized
// 80 by 24, and the dock ports set.
func newDockModel(t *testing.T, ed EditorState, ports DockPorts) *Model {
	t.Helper()
	m := New(Deps{Editor: ed, Explorer: newFakeTree(3), Keys: dockKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.SetDock(ports)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return m
}

// dockPumps keeps the shell traffic of each model the tests settle. The dock's
// commands run concurrently, the way Bubble Tea runs them, and a wait for output or
// the exit parks until the test closes a shell channel; the pump carries that message
// into the next settle instead of losing it.
type dockPump struct {
	events chan tea.Msg
}

var dockPumps sync.Map // *Model -> *dockPump

// dockPumpFor returns the model's pump, making it the first time.
func dockPumpFor(m *Model) *dockPump {
	if p, ok := dockPumps.Load(m); ok {
		return p.(*dockPump)
	}
	p := &dockPump{events: make(chan tea.Msg, 512)}
	actual, _ := dockPumps.LoadOrStore(m, p)
	return actual.(*dockPump)
}

// start runs cmd in the background and sends what it reports to the pump.
func (p *dockPump) start(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() {
		msg := cmd()
		if msg == nil {
			return
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				p.start(c)
			}
			return
		}
		p.events <- msg
	}()
}

// dockSettle runs cmd and feeds the messages that come back into m, and it keeps going
// until the dock is quiet: nothing arrived for quiet. A wait for output or the exit is
// left running, so a test that closes a shell channel has its message delivered by the
// next settle.
func dockSettle(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	p := dockPumpFor(m)
	p.start(cmd)
	for {
		select {
		case msg := <-p.events:
			_, next := m.Update(msg)
			p.start(next)
		case <-time.After(40 * time.Millisecond):
			return
		}
	}
}

// dockRun runs an action the way the palette and the menu do: as a message that
// Update handles. Commands that Update adds after the action, such as a flush of the
// shell's input, are part of the run.
func dockRun(t *testing.T, m *Model, action string) {
	t.Helper()
	_, cmd := m.Update(palette.RunCommandMsg{Action: action})
	dockSettle(t, m, cmd)
}

// openDock shows the dock with a shell, as open_terminal_in_dock does.
func openDock(t *testing.T, m *Model) {
	t.Helper()
	dockSettle(t, m, m.runCommand("open_terminal_in_dock"))
}

// selectionDoc is the fake document with a selection between anchor and head.
type selectionDoc struct {
	*fakeDoc
	anchor, head buffer.Pos
	selected     bool
}

func (d selectionDoc) Selection() (a, h buffer.Pos, ok bool) {
	return d.anchor, d.head, d.selected
}

// selectionEditor is the fake editor whose active document is sel.
type selectionEditor struct {
	*fakeEditor
	sel selectionDoc
}

func (e selectionEditor) Active() Document { return e.sel }

func TestDockActionsShowAndHide(t *testing.T) {
	tests := []struct {
		name    string
		actions []string
		open    bool
		focus   bool
		starts  int
	}{
		{name: "open_terminal_in_dock shows and focuses the dock", actions: []string{"open_terminal_in_dock"}, open: true, focus: true, starts: 1},
		{name: "toggle_utility_dock shows a hidden dock", actions: []string{"toggle_utility_dock"}, open: true, focus: true, starts: 1},
		{name: "toggle_utility_dock hides a shown dock", actions: []string{"toggle_utility_dock", "toggle_utility_dock"}, open: false, focus: false, starts: 1},
		{name: "toggle_dock_focus opens a hidden dock with focus", actions: []string{"toggle_dock_focus"}, open: true, focus: true, starts: 1},
		{name: "toggle_dock_focus gives the focus to the editor", actions: []string{"open_terminal_in_dock", "toggle_dock_focus"}, open: true, focus: false, starts: 1},
		{name: "toggle_dock_focus takes the focus back to the dock", actions: []string{"open_terminal_in_dock", "focus_editor", "toggle_dock_focus"}, open: true, focus: true, starts: 1},
		{name: "terminal_escape gives the focus to the editor", actions: []string{"open_terminal_in_dock", "terminal_escape"}, open: true, focus: false, starts: 1},
		{name: "a running shell is shown again, not started again", actions: []string{"open_terminal_in_dock", "toggle_utility_dock", "open_terminal_in_dock"}, open: true, focus: true, starts: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			starter := newFakeStarter()
			m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: starter})
			for _, a := range tt.actions {
				dockSettle(t, m, m.runCommand(a))
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

func TestDockShellStartsInTheWorkspaceRoot(t *testing.T) {
	starter := newFakeStarter()
	m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: starter})
	openDock(t, m)
	if !slices.Equal(starter.dirs, []string{"/ws"}) {
		t.Fatalf("start directories = %q, want [/ws]", starter.dirs)
	}
	// The start is sized to the dock: 80 columns, and the rows under the title row.
	if want := [2]int{80, 5}; starter.sizes[0] != want {
		t.Fatalf("start size = %v, want %v", starter.sizes[0], want)
	}
}

func TestDockFocusIsExclusiveWithTheExplorer(t *testing.T) {
	tests := []struct {
		name          string
		actions       []string
		dockFocus     bool
		explorerFocus bool
	}{
		{name: "opening the dock takes the focus from the explorer", actions: []string{"focus_file_explorer", "open_terminal_in_dock"}, dockFocus: true, explorerFocus: false},
		{name: "focusing the explorer takes the focus from the dock", actions: []string{"open_terminal_in_dock", "focus_file_explorer"}, dockFocus: false, explorerFocus: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: newFakeStarter()})
			for _, a := range tt.actions {
				dockSettle(t, m, m.runCommand(a))
			}
			if m.dock.Focus() != tt.dockFocus || m.explorerFocus != tt.explorerFocus {
				t.Fatalf("dock focus, explorer focus = %v, %v; want %v, %v",
					m.dock.Focus(), m.explorerFocus, tt.dockFocus, tt.explorerFocus)
			}
		})
	}
}

func TestDockKeysRouteByFocus(t *testing.T) {
	tests := []struct {
		name   string
		setup  []string
		keys   []tea.KeyPressMsg
		writes []string
		editor []editor.Action
	}{
		{
			name:   "typed text goes to the shell while the dock has focus",
			setup:  []string{"open_terminal_in_dock"},
			keys:   []tea.KeyPressMsg{typed("a")},
			writes: []string{"a"},
		},
		{
			name:   "typed text goes to the editor while the editor has focus",
			setup:  []string{"open_terminal_in_dock", "focus_editor"},
			keys:   []tea.KeyPressMsg{typed("a")},
			editor: []editor.Action{"insert_char"},
		},
		{
			name:   "Enter and Backspace reach the shell as CR and DEL",
			setup:  []string{"open_terminal_in_dock"},
			keys:   []tea.KeyPressMsg{special(tea.KeyEnter), special(tea.KeyBackspace)},
			writes: []string{"\r", "\x7f"},
		},
		{
			name:   "arrow keys reach the shell, not the editor",
			setup:  []string{"open_terminal_in_dock"},
			keys:   []tea.KeyPressMsg{special(tea.KeyUp)},
			writes: []string{"\x1b[A"},
		},
		{
			name:   "Ctrl+S reaches the shell instead of saving",
			setup:  []string{"open_terminal_in_dock"},
			keys:   []tea.KeyPressMsg{ctrl('s')},
			writes: []string{"\x13"},
		},
		{
			name:   "Ctrl+Space gives the focus to the editor",
			setup:  []string{"open_terminal_in_dock"},
			keys:   []tea.KeyPressMsg{{Code: tea.KeySpace, Mod: tea.ModCtrl}, typed("a")},
			editor: []editor.Action{"insert_char"},
		},
		{
			name:   "Alt+O gives the focus to the editor",
			setup:  []string{"open_terminal_in_dock"},
			keys:   []tea.KeyPressMsg{{Code: 'o', Mod: tea.ModAlt}, typed("a")},
			editor: []editor.Action{"insert_char"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ed := newFakeEditor("hello")
			starter := newFakeStarter()
			m := newDockModel(t, ed, DockPorts{Starter: starter})
			for _, a := range tt.setup {
				dockSettle(t, m, m.runCommand(a))
			}
			for _, k := range tt.keys {
				dockSettle(t, m, press(m, k))
			}
			if !slices.Equal(starter.term.writes, tt.writes) {
				t.Fatalf("shell writes = %q, want %q", starter.term.writes, tt.writes)
			}
			if !slices.Equal(ed.actions, tt.editor) {
				t.Fatalf("editor actions = %v, want %v", ed.actions, tt.editor)
			}
		})
	}
}

func TestDockKeysWriteOnlyFromCommands(t *testing.T) {
	starter := newFakeStarter()
	m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: starter})
	openDock(t, m)

	cmds := []tea.Cmd{press(m, typed("a")), press(m, typed("b")), press(m, typed("c"))}
	if len(starter.term.writes) != 0 {
		t.Fatalf("keys wrote to the shell inside Update: %q", starter.term.writes)
	}
	for _, c := range cmds {
		dockSettle(t, m, c)
	}
	if got := strings.Join(starter.term.writes, ""); got != "abc" {
		t.Fatalf("shell received %q, want %q in key order", got, "abc")
	}
}

func TestDockPasteSendsClipboardText(t *testing.T) {
	tests := []struct {
		name      string
		shell     bool
		clipboard fakeClipboard
		pastes    []string
		notice    string
	}{
		{
			name:      "clipboard text is pasted with LF line breaks and no ESC",
			shell:     true,
			clipboard: fakeClipboard{text: "a\r\nb\x1b[201~"},
			pastes:    []string{"a\nb[201~"},
		},
		{
			name:      "no shell means nothing is pasted",
			shell:     false,
			clipboard: fakeClipboard{text: "a"},
			notice:    noTerminalNotice,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			starter := newFakeStarter()
			cb := tt.clipboard
			m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: starter, Clipboard: cb})
			if tt.shell {
				openDock(t, m)
			}
			if tt.shell {
				dockSettle(t, m, press(m, ctrl('v')))
			} else {
				// Without the dock open, Ctrl+V is the editor's key, so the action runs as the menu runs it.
				dockRun(t, m, "terminal_paste")
			}
			if tt.shell && !slices.Equal(starter.term.pastes, tt.pastes) {
				t.Fatalf("pastes = %q, want %q", starter.term.pastes, tt.pastes)
			}
			if !tt.shell && (m.notice != tt.notice || len(starter.dirs) != 0) {
				t.Fatalf("notice = %q, starts = %d; want notice %q and no start", m.notice, len(starter.dirs), tt.notice)
			}
		})
	}
}

func TestDockPasteReportsClipboardFailure(t *testing.T) {
	starter := newFakeStarter()
	m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: starter, Clipboard: fakeClipboard{err: errors.New("no tool")}})
	openDock(t, m)
	dockSettle(t, m, press(m, ctrl('v')))
	if len(starter.term.pastes) != 0 {
		t.Fatalf("pastes = %q, want none after a clipboard failure", starter.term.pastes)
	}
	if !strings.Contains(m.notice, "no tool") {
		t.Fatalf("notice = %q, want the clipboard error", m.notice)
	}
}

func TestSendSelectionToTerminal(t *testing.T) {
	tests := []struct {
		name     string
		lines    []string
		selected bool
		anchor   buffer.Pos
		head     buffer.Pos
		cursor   buffer.Pos
		want     string
	}{
		{
			name:     "selected text on one line",
			lines:    []string{"abcd"},
			selected: true,
			anchor:   buffer.Pos{Line: 0, Col: 1},
			head:     buffer.Pos{Line: 0, Col: 3},
			want:     "bc\n",
		},
		{
			name:     "selected text over two lines",
			lines:    []string{"abcd", "efgh"},
			selected: true,
			anchor:   buffer.Pos{Line: 0, Col: 2},
			head:     buffer.Pos{Line: 1, Col: 2},
			want:     "cd\nef\n",
		},
		{
			name:   "no selection sends the cursor line",
			lines:  []string{"abcd", "efgh"},
			cursor: buffer.Pos{Line: 1, Col: 0},
			want:   "efgh\n",
		},
		{
			name:     "a selection that ends at a line start keeps the line break once",
			lines:    []string{"ab", "cd"},
			selected: true,
			anchor:   buffer.Pos{Line: 0, Col: 0},
			head:     buffer.Pos{Line: 1, Col: 0},
			want:     "ab\n",
		},
		{
			name:   "ESC in the text is dropped, as a paste drops it",
			lines:  []string{"a\x1b[31mb"},
			cursor: buffer.Pos{Line: 0, Col: 0},
			want:   "a[31mb\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ed := newFakeEditor(tt.lines...)
			ed.doc.cursor = tt.cursor
			se := selectionEditor{fakeEditor: ed, sel: selectionDoc{fakeDoc: ed.doc, anchor: tt.anchor, head: tt.head, selected: tt.selected}}
			starter := newFakeStarter()
			m := newDockModel(t, se, DockPorts{Starter: starter})
			openDock(t, m)
			m.focusEditor()

			dockRun(t, m, "send_selection_to_terminal")
			if !slices.Equal(starter.term.writes, []string{tt.want}) {
				t.Fatalf("shell writes = %q, want [%q]", starter.term.writes, tt.want)
			}
			if !m.dock.Focus() {
				t.Fatal("the dock did not take the focus after the send")
			}
		})
	}
}

func TestSendSelectionWithoutShellNotifies(t *testing.T) {
	starter := newFakeStarter()
	m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: starter})
	dockRun(t, m, "send_selection_to_terminal")
	if m.notice != noTerminalNotice {
		t.Fatalf("notice = %q, want %q", m.notice, noTerminalNotice)
	}
	if len(starter.dirs) != 0 {
		t.Fatalf("send started %d shells, want none", len(starter.dirs))
	}
}

func TestDockStartFailureShowsNotice(t *testing.T) {
	tests := []struct {
		name  string
		ports func() DockPorts
		want  string
	}{
		{
			name:  "platform without terminals",
			ports: func() DockPorts { return DockPorts{Starter: &fakeStarter{err: terminal.ErrUnsupported}} },
			want:  "Terminals are not supported on this platform yet.",
		},
		{
			name:  "start error",
			ports: func() DockPorts { return DockPorts{Starter: &fakeStarter{err: errors.New("fork failed")}} },
			want:  "Could not start a shell: fork failed",
		},
		{
			name:  "no starter in this build",
			ports: func() DockPorts { return DockPorts{} },
			want:  "Terminals are not available in this build.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newDockModel(t, newFakeEditor("hello"), tt.ports())
			openDock(t, m)
			if m.dock.Running() {
				t.Fatal("a shell is running after a failed start")
			}
			if !m.dock.Open() || m.dock.Notice() != tt.want {
				t.Fatalf("open = %v, notice = %q; want open with notice %q", m.dock.Open(), m.dock.Notice(), tt.want)
			}
			top := m.layout().DockY + 1
			if row := m.paint().Plain()[top]; !strings.Contains(row, tt.want) {
				t.Fatalf("dock row %d = %q, want it to show %q", top, row, tt.want)
			}
		})
	}
}

func TestDockResizesWithTheLayout(t *testing.T) {
	starter := newFakeStarter()
	m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: starter})
	openDock(t, m)
	if n := len(starter.term.resizes); n != 0 {
		t.Fatalf("resizes after the start = %v, want none", starter.term.resizes)
	}
	_, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	dockSettle(t, m, cmd)
	// At 100 by 30 the dock takes 8 rows: the title row and 7 rows of shell.
	if want := [][2]int{{100, 7}}; !slices.Equal(starter.term.resizes, want) {
		t.Fatalf("resizes = %v, want %v", starter.term.resizes, want)
	}
}

func TestDockHideKeepsTheShellRunning(t *testing.T) {
	starter := newFakeStarter()
	m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: starter})
	openDock(t, m)
	dockSettle(t, m, m.runCommand("toggle_utility_dock"))
	if m.dock.Open() || starter.term.closed {
		t.Fatalf("open = %v, closed = %v; want hidden and still running", m.dock.Open(), starter.term.closed)
	}
	dockSettle(t, m, m.runCommand("toggle_utility_dock"))
	if len(starter.dirs) != 1 {
		t.Fatalf("shell starts = %d, want the same shell shown again", len(starter.dirs))
	}
}

func TestCloseDockEndsTheShell(t *testing.T) {
	starter := newFakeStarter()
	m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: starter})
	openDock(t, m)
	m.closeDock()
	if !starter.term.closed || m.dock.Running() {
		t.Fatalf("closed = %v, running = %v; want the shell closed and dropped", starter.term.closed, m.dock.Running())
	}
}

func TestDockPaintsScreenCells(t *testing.T) {
	red := color.RGBA{R: 255, A: 255}
	blue := color.RGBA{B: 255, A: 255}
	starter := newFakeStarter()
	// Two rows of six cells. Row 0 has three styled letters. Row 1 has a wide
	// character, which takes the next cell too, then a letter.
	starter.term.screen = terminal.NewScreen(2, 6, []terminal.Cell{
		{Rune: 'A', Style: terminal.Style{Fg: red, Bold: true}},
		{Rune: 'B', Style: terminal.Style{Reverse: true}},
		{Rune: 'C', Style: terminal.Style{Bg: blue, Underline: true}},
		{}, {}, {},
		{Rune: '漢'}, {}, {Rune: 'x'},
	})
	m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: starter})
	openDock(t, m)
	top := m.layout().DockY + 1

	g := m.paint()
	tests := []struct {
		name  string
		x, y  int
		text  string
		check func(s grid.Style) bool
	}{
		{name: "bold red foreground", x: 0, y: top, text: "A", check: func(s grid.Style) bool { return s.FG == red && s.Bold }},
		{name: "reverse video", x: 1, y: top, text: "B", check: func(s grid.Style) bool { return s.Reverse }},
		{name: "underlined blue background", x: 2, y: top, text: "C", check: func(s grid.Style) bool { return s.BG == blue && s.Underline }},
		{name: "default cell is blank with no colour", x: 3, y: top, text: " ", check: func(s grid.Style) bool { return s.FG == nil && s.BG == nil }},
		{name: "wide character takes two cells", x: 0, y: top + 1, text: "漢", check: func(grid.Style) bool { return true }},
		{name: "second half of a wide character is empty", x: 1, y: top + 1, text: "", check: func(grid.Style) bool { return true }},
		{name: "cell after a wide character", x: 2, y: top + 1, text: "x", check: func(grid.Style) bool { return true }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := g.At(tt.x, tt.y)
			if c.Text != tt.text {
				t.Fatalf("cell (%d, %d) text = %q, want %q", tt.x, tt.y, c.Text, tt.text)
			}
			if !tt.check(c.Style) {
				t.Fatalf("cell (%d, %d) style = %+v", tt.x, tt.y, c.Style)
			}
		})
	}
}

func TestDockCaretFollowsTheShellCursor(t *testing.T) {
	tests := []struct {
		name    string
		cursor  terminal.Cursor
		focus   bool
		wantOK  bool
		wantRow int // row offset from the dock's first shell row, when wantOK
		wantCol int
	}{
		{name: "visible cursor in the shell", cursor: terminal.Cursor{Row: 1, Col: 2, Visible: true}, focus: true, wantOK: true, wantRow: 1, wantCol: 2},
		{name: "hidden cursor is not drawn", cursor: terminal.Cursor{Row: 1, Col: 2, Visible: false}, focus: true, wantOK: false},
		{name: "editor focus uses the editor caret", cursor: terminal.Cursor{Row: 1, Col: 2, Visible: true}, focus: false, wantOK: true, wantRow: -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			starter := newFakeStarter()
			starter.term.screen = terminal.NewScreen(6, 80, nil)
			starter.term.screen.Cursor = tt.cursor
			m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: starter})
			openDock(t, m)
			if !tt.focus {
				m.focusEditor()
			}
			x, y, ok := m.caret()
			if ok != tt.wantOK {
				t.Fatalf("caret ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok || tt.wantRow < 0 {
				if ok && y >= m.layout().DockY {
					t.Fatalf("caret at row %d is inside the dock, want the editor's caret", y)
				}
				return
			}
			dockTop := m.layout().DockY + 1
			if x != tt.wantCol || y != dockTop+tt.wantRow {
				t.Fatalf("caret = (%d, %d), want (%d, %d)", x, y, tt.wantCol, dockTop+tt.wantRow)
			}
		})
	}
}

func TestDockLayoutRegion(t *testing.T) {
	tests := []struct {
		name       string
		w, h       int
		open       bool
		wantOn     bool
		wantY      int
		wantH      int
		wantBodyBt int
	}{
		{name: "80 by 24 with the dock takes 6 rows above the status row", w: 80, h: 24, open: true, wantOn: true, wantY: 17, wantH: 6, wantBodyBt: 16},
		{name: "80 by 24 without the dock keeps the body", w: 80, h: 24, open: false, wantOn: false, wantBodyBt: 22},
		{name: "80 by 11 takes the minimum dock", w: 80, h: 11, open: true, wantOn: true, wantY: 6, wantH: 4, wantBodyBt: 5},
		{name: "80 by 8 leaves no room for a dock", w: 80, h: 8, open: true, wantOn: false, wantBodyBt: 6},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := frame.ComputeLayout(tt.w, tt.h, layoutInput{DockOpen: tt.open})
			if l.DockOn != tt.wantOn {
				t.Fatalf("dockOn = %v, want %v", l.DockOn, tt.wantOn)
			}
			if tt.wantOn && (l.DockY != tt.wantY || l.DockH != tt.wantH) {
				t.Fatalf("dock rows start %d with %d rows, want %d with %d", l.DockY, l.DockH, tt.wantY, tt.wantH)
			}
			if l.BodyBottom != tt.wantBodyBt {
				t.Fatalf("bodyBottom = %d, want %d", l.BodyBottom, tt.wantBodyBt)
			}
		})
	}
}

func TestDockTooSmallForAShellKeepsKeysInTheEditor(t *testing.T) {
	ed := newFakeEditor("hello")
	starter := newFakeStarter()
	m := newDockModel(t, ed, DockPorts{Starter: starter})
	openDock(t, m)
	// At 80 by 8 the body has no room left for the dock, so the dock is not drawn.
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 8})
	if m.layout().DockOn {
		t.Fatal("the dock is laid out at 80 by 8, want it hidden")
	}
	dockSettle(t, m, press(m, typed("a")))
	if len(starter.term.writes) != 0 {
		t.Fatalf("a hidden dock took input: shell writes = %q", starter.term.writes)
	}
	if !slices.Equal(ed.actions, []editor.Action{"insert_char"}) {
		t.Fatalf("editor actions = %v, want the typed text in the editor", ed.actions)
	}
}

// runCmdWithin runs cmd as runCmd does and fails the test if it does not return within
// d, so a wait that never ends fails the test instead of hanging the run.
func runCmdWithin(t *testing.T, cmd tea.Cmd, d time.Duration) []tea.Msg {
	t.Helper()
	done := make(chan []tea.Msg, 1)
	go func() { done <- runCmd(cmd) }()
	select {
	case msgs := <-done:
		return msgs
	case <-time.After(d):
		t.Fatalf("command did not return within %v", d)
		return nil
	}
}

func TestDockTakesFocusOnlyWhileAShellRuns(t *testing.T) {
	tests := []struct {
		name      string
		ports     func() DockPorts
		wantFocus bool
	}{
		{
			name:      "a shell that started takes the focus",
			ports:     func() DockPorts { return DockPorts{Starter: newFakeStarter()} },
			wantFocus: true,
		},
		{
			name:      "a start that failed takes no focus",
			ports:     func() DockPorts { return DockPorts{Starter: &fakeStarter{err: terminal.ErrUnsupported}} },
			wantFocus: false,
		},
		{
			name:      "a dock with no starter takes no focus",
			ports:     func() DockPorts { return DockPorts{} },
			wantFocus: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newDockModel(t, newFakeEditor("hello"), tt.ports())
			openDock(t, m)
			if m.dock.Focus() != tt.wantFocus {
				t.Fatalf("dock focus = %v, want %v", m.dock.Focus(), tt.wantFocus)
			}
		})
	}
}

func TestDockTakesFocusWhenItsShellStarts(t *testing.T) {
	m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: newFakeStarter()})
	cmd := m.runCommand("open_terminal_in_dock")
	if m.dock.Focus() {
		t.Fatal("the dock took the focus before a shell ran")
	}
	dockSettle(t, m, cmd)
	if !m.dock.Focus() {
		t.Fatal("the dock did not take the focus when its shell started")
	}
}

func TestDockKeysWithoutAShellGoToTheEditor(t *testing.T) {
	tests := []struct {
		name   string
		keys   []tea.KeyPressMsg
		editor []editor.Action
	}{
		{name: "typed text is inserted", keys: []tea.KeyPressMsg{typed("a")}, editor: []editor.Action{"insert_char"}},
		{name: "a bound editor key runs", keys: []tea.KeyPressMsg{ctrl('s')}, editor: []editor.Action{"save"}},
		{name: "Escape reaches the keymap", keys: []tea.KeyPressMsg{special(tea.KeyEscape)}, editor: []editor.Action{"cancel_selection"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ed := newFakeEditor("hello")
			starter := &fakeStarter{err: errors.New("no shell")}
			m := newDockModel(t, ed, DockPorts{Starter: starter})
			openDock(t, m)
			for _, k := range tt.keys {
				dockSettle(t, m, press(m, k))
			}
			if !slices.Equal(ed.actions, tt.editor) {
				t.Fatalf("editor actions = %v, want %v", ed.actions, tt.editor)
			}
			if m.dock.Focus() {
				t.Fatal("the dock holds the focus with no shell running")
			}
		})
	}
}

func TestDockExitGivesTheFocusToTheEditor(t *testing.T) {
	ed := newFakeEditor("hello")
	starter := newFakeStarter()
	m := newDockModel(t, ed, DockPorts{Starter: starter})
	openDock(t, m)
	if !m.dock.Focus() {
		t.Fatal("precondition: the dock has the focus with its shell running")
	}
	close(starter.term.done)
	dockSettle(t, m, nil)
	if m.dock.Focus() {
		t.Fatal("the dock keeps the focus after its shell exited")
	}
	dockSettle(t, m, press(m, typed("a")))
	if !slices.Equal(ed.actions, []editor.Action{"insert_char"}) {
		t.Fatalf("editor actions = %v, want the typed text in the editor", ed.actions)
	}
	if len(starter.term.writes) != 0 {
		t.Fatalf("shell writes after exit = %q, want none", starter.term.writes)
	}
}

func TestDockPressFocusesTheDock(t *testing.T) {
	tests := []struct {
		name      string
		shell     bool
		row       func(l layout) int
		wantFocus bool
	}{
		{name: "a press on a shell row gives the dock the focus", shell: true, row: func(l layout) int { return l.DockY + 2 }, wantFocus: true},
		{name: "a press on the title row gives the dock the focus", shell: true, row: func(l layout) int { return l.DockY }, wantFocus: true},
		{name: "a press above the dock leaves the focus in the editor", shell: true, row: func(l layout) int { return l.DockY - 1 }, wantFocus: false},
		{name: "a press in the dock with no shell takes no focus", shell: false, row: func(l layout) int { return l.DockY + 2 }, wantFocus: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			starter := newFakeStarter()
			if !tt.shell {
				starter.err = errors.New("no shell")
			}
			m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: starter})
			openDock(t, m)
			m.focusEditor()
			m.Update(click(10, tt.row(m.layout()), tea.MouseLeft))
			if m.dock.Focus() != tt.wantFocus {
				t.Fatalf("dock focus = %v, want %v", m.dock.Focus(), tt.wantFocus)
			}
		})
	}
}

func TestDockKeyboardCaptureSendsBoundKeysToTheShell(t *testing.T) {
	tests := []struct {
		name   string
		keys   []tea.KeyPressMsg
		writes []string
		editor []editor.Action
	}{
		{
			name:   "F9 turns capture on, so a bound key goes to the shell",
			keys:   []tea.KeyPressMsg{special(tea.KeyF9), {Code: tea.KeySpace, Mod: tea.ModCtrl}},
			writes: []string{"\x00"},
		},
		{
			name:   "Ctrl+\\ toggles capture the same way",
			keys:   []tea.KeyPressMsg{ctrl('\\'), {Code: tea.KeySpace, Mod: tea.ModCtrl}},
			writes: []string{"\x00"},
		},
		{
			name:   "a second F9 turns capture off, so the binding runs again",
			keys:   []tea.KeyPressMsg{special(tea.KeyF9), special(tea.KeyF9), {Code: tea.KeySpace, Mod: tea.ModCtrl}, typed("a")},
			editor: []editor.Action{"insert_char"},
		},
		{
			name: "the toggle key itself is not sent to the shell",
			keys: []tea.KeyPressMsg{special(tea.KeyF9)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ed := newFakeEditor("hello")
			starter := newFakeStarter()
			m := newDockModel(t, ed, DockPorts{Starter: starter})
			openDock(t, m)
			for _, k := range tt.keys {
				dockSettle(t, m, press(m, k))
			}
			if !slices.Equal(starter.term.writes, tt.writes) {
				t.Fatalf("shell writes = %q, want %q", starter.term.writes, tt.writes)
			}
			if !slices.Equal(ed.actions, tt.editor) {
				t.Fatalf("editor actions = %v, want %v", ed.actions, tt.editor)
			}
		})
	}
}

func TestDockKeyboardCaptureNoticeFollowsTheToggle(t *testing.T) {
	const onNotice = "Keyboard capture ON - all keys go to terminal (F9 to toggle)"
	const offNotice = "Keyboard capture OFF - UI bindings active (F9 to toggle)"
	tests := []struct {
		name   string
		keys   []tea.KeyPressMsg
		notice string
	}{
		{name: "F9 turns capture on", keys: []tea.KeyPressMsg{special(tea.KeyF9)}, notice: onNotice},
		{name: "Ctrl+\\ turns capture on", keys: []tea.KeyPressMsg{ctrl('\\')}, notice: onNotice},
		{name: "a second F9 turns capture off", keys: []tea.KeyPressMsg{special(tea.KeyF9), special(tea.KeyF9)}, notice: offNotice},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: newFakeStarter()})
			openDock(t, m)
			for _, k := range tt.keys {
				dockSettle(t, m, press(m, k))
			}
			if m.notice != tt.notice {
				t.Fatalf("notice = %q, want %q", m.notice, tt.notice)
			}
		})
	}
}

func TestDockKeyboardCaptureNeedsARunningShell(t *testing.T) {
	ed := newFakeEditor("hello")
	m := newDockModel(t, ed, DockPorts{Starter: &fakeStarter{err: errors.New("no shell")}})
	openDock(t, m)
	dockSettle(t, m, m.runCommand("toggle_keyboard_capture"))
	if m.notice != "" {
		t.Fatalf("notice = %q, want none with no shell running", m.notice)
	}
}

func TestDockArrowKeysFollowTheShellMode(t *testing.T) {
	tests := []struct {
		name      string
		appCursor bool
		key       tea.KeyPressMsg
		want      string
	}{
		{name: "Up in normal cursor mode", key: special(tea.KeyUp), want: "\x1b[A"},
		{name: "Up in application cursor mode", appCursor: true, key: special(tea.KeyUp), want: "\x1bOA"},
		{name: "Home in application cursor mode", appCursor: true, key: special(tea.KeyHome), want: "\x1bOH"},
		{name: "End in normal cursor mode", key: special(tea.KeyEnd), want: "\x1b[F"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			starter := newFakeStarter()
			starter.term.appCursor = tt.appCursor
			m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: starter})
			openDock(t, m)
			dockSettle(t, m, press(m, tt.key))
			if !slices.Equal(starter.term.writes, []string{tt.want}) {
				t.Fatalf("shell writes = %q, want [%q]", starter.term.writes, tt.want)
			}
		})
	}
}

func TestDockKeyWithNoSequenceStaysInTheDock(t *testing.T) {
	tests := []struct {
		name string
		key  tea.KeyPressMsg
	}{
		{name: "a key with no sequence", key: special(tea.KeyBegin)},
		{name: "Ctrl with a letter that has no control byte", key: tea.KeyPressMsg{Code: 'é', Text: "é", Mod: tea.ModCtrl}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ed := newFakeEditor("hello")
			starter := newFakeStarter()
			m := newDockModel(t, ed, DockPorts{Starter: starter})
			openDock(t, m)
			dockSettle(t, m, press(m, tt.key))
			if len(starter.term.writes) != 0 {
				t.Fatalf("shell writes = %q, want none", starter.term.writes)
			}
			if len(ed.actions) != 0 {
				t.Fatalf("editor actions = %v, want none: the dock has the focus", ed.actions)
			}
			if !m.dock.Focus() {
				t.Fatal("the dock lost the focus to a key the shell cannot receive")
			}
		})
	}
}
