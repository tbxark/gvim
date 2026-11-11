package dock

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/terminal"
	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/style"
)

// fakeTerm implements Terminal. It records what it is asked to write, paste and
// resize. Its Updates channel starts with one pending signal, so a wait for output
// returns at once. Its Done channel stays open until a test closes it.
type fakeTerm struct {
	writes     []string
	pastes     []string
	resizes    [][2]int
	closed     bool
	writeErr   error
	screen     terminal.Screen
	appCursor  bool  // the shell's DECCKM mode, reported by AppCursorKeys
	kittyFlags uint8 // the child's kitty keyboard flags, reported by KittyKeyboardFlags
	updates    chan struct{}
	done       chan struct{}

	// The scrollback, OSC 7 and mouse state the dock reads.
	history       []string              // the history rows, oldest first
	styledHistory []terminal.Line       // styled history rows, when set
	cwd           string                // OSC 7 working directory
	alt           bool                  // the alternate screen
	exitCode      int                   // the status ExitStatus reports
	exitOK        bool                  // whether the fake reports an exit status
	title         string                // the OSC 0/2 title the fake reports
	focusCalls    []bool                // the DECSET 1004 focus reports the dock forwarded
	fgSupported   bool                  // whether ForegroundProcessSupported reports a local PTY
	fgName        string                // the foreground process name ForegroundProcessName returns
	fgOK          bool                  // whether ForegroundProcessName read a name
	fgCalls       int                   // the calls to ForegroundProcessName
	mouse         bool                  // the shell asked for mouse reporting
	drag          bool                  // the shell asked for drag motion (1002)
	allMotion     bool                  // the shell asked for all motion (1003)
	mice          []terminal.MouseEvent // the mouse events the dock forwarded
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

func (f *fakeTerm) Screen() terminal.Screen          { return f.screen }
func (f *fakeTerm) Updates() <-chan struct{}         { return f.updates }
func (f *fakeTerm) Done() <-chan struct{}            { return f.done }
func (f *fakeTerm) AppCursorKeys() bool              { return f.appCursor }
func (f *fakeTerm) KittyKeyboardFlags() uint8        { return f.kittyFlags }
func (f *fakeTerm) Title() string                    { return f.title }
func (f *fakeTerm) Focus(focused bool)               { f.focusCalls = append(f.focusCalls, focused) }
func (f *fakeTerm) ForegroundProcessSupported() bool { return f.fgSupported }
func (f *fakeTerm) ForegroundProcessName() (string, bool) {
	f.fgCalls++
	return f.fgName, f.fgOK
}
func (f *fakeTerm) Close() error { f.closed = true; return nil }

// Scrollback returns the history window the dock asks for.
func (f *fakeTerm) Scrollback(from, count int) (lines []terminal.Line, total int) {
	if len(f.styledHistory) > 0 {
		total = len(f.styledHistory)
		from = min(max(from, 0), total)
		count = min(max(count, 0), total-from)
		return append([]terminal.Line(nil), f.styledHistory[from:from+count]...), total
	}
	total = len(f.history)
	from = min(max(from, 0), total)
	count = min(max(count, 0), total-from)
	for _, text := range f.history[from : from+count] {
		lines = append(lines, fakeLine(text))
	}
	return lines, total
}

func (f *fakeTerm) ScrollbackLen() int {
	if len(f.styledHistory) > 0 {
		return len(f.styledHistory)
	}
	return len(f.history)
}

// SearchScrollback finds the history rows that hold query, case-insensitively.
func (f *fakeTerm) SearchScrollback(query string, limit int) (matches []int, total int) {
	query = strings.ToLower(query)
	for i, text := range f.history {
		if strings.Contains(strings.ToLower(text), query) {
			matches = append(matches, i)
			if len(matches) >= limit {
				break
			}
		}
	}
	return matches, len(f.history)
}

// SearchScrollbackMode finds the history rows that match query with the search
// options. The fake's query is matched as plain text.
func (f *fakeTerm) SearchScrollbackMode(query string, regex, matchCase bool, limit int) (matches []int, total int, err error) {
	matches, total = f.SearchScrollback(query, limit)
	return matches, total, nil
}

// ExitStatus reports the exit status a test armed (the fake is not a real child).
func (f *fakeTerm) ExitStatus() (int, bool) { return f.exitCode, f.exitOK }

func (f *fakeTerm) WorkingDirectory() string { return f.cwd }
func (f *fakeTerm) AltScreen() bool          { return f.alt }
func (f *fakeTerm) MouseReporting() (reporting, drag, allMotion bool) {
	return f.mouse, f.drag, f.allMotion
}

func (f *fakeTerm) SendMouse(ev terminal.MouseEvent) { f.mice = append(f.mice, ev) }

// fakeLine is a history row of width 80 cells.
func fakeLine(text string) terminal.Line {
	cells := make([]terminal.Cell, 80)
	for i, r := range []rune(text) {
		if i >= len(cells) {
			break
		}
		cells[i] = terminal.Cell{Rune: r}
	}
	return terminal.Line{Cells: cells}
}

// fakeStarter implements TerminalStarter. It hands out term, or fails with err.
type fakeStarter struct {
	term       *fakeTerm
	err        error
	dirs       []string
	launches   []terminal.Launch // the StartLaunch calls, in order
	launchDirs []string          // the directories of the StartLaunch calls
}

func (s *fakeStarter) Start(dir string, cols, rows int) (Terminal, error) {
	s.dirs = append(s.dirs, dir)
	if s.err != nil {
		return nil, s.err
	}
	return s.term, nil
}

// StartLaunch records an agent/resume start, the way the real starter runs it.
func (s *fakeStarter) StartLaunch(launch terminal.Launch, dir string, cols, rows int) (Terminal, error) {
	s.launches = append(s.launches, launch)
	s.launchDirs = append(s.launchDirs, dir)
	if s.err != nil {
		return nil, s.err
	}
	return s.term, nil
}

// newTestPanel returns a shown panel whose starter started a shell, sized 80 by 4.
func newTestPanel(t *testing.T, starter *fakeStarter) *Panel {
	t.Helper()
	var p Panel
	p.SetHost(Host{
		RootDir:     func() string { return "/ws" },
		Notice:      func(string) {},
		ContentSize: func() (int, int) { return 80, 4 },
		Focus:       func() {},
	})
	p.SetPorts(Ports{Starter: starter})
	cmd := p.Show()
	if cmd == nil {
		t.Fatal("Show did not start a shell")
	}
	if _, ok := p.Update(cmd()); !ok {
		t.Fatal("the start message was not taken")
	}
	if !p.Running() {
		t.Fatal("the shell did not start")
	}
	return &p
}

// messages runs cmd and returns the messages it produces, looking into batches.
func messages(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if msg == nil {
		return nil
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, messages(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// runWithin runs cmd and fails the test if it does not return within d.
func runWithin(t *testing.T, cmd tea.Cmd, d time.Duration) []tea.Msg {
	t.Helper()
	done := make(chan []tea.Msg, 1)
	go func() { done <- messages(cmd) }()
	select {
	case msgs := <-done:
		return msgs
	case <-time.After(d):
		t.Fatalf("command did not return within %v", d)
		return nil
	}
}

func TestWaitEventReportsWhatHappened(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(f *fakeTerm)
		live      bool
		watchExit bool
		want      tea.Msg
	}{
		{
			name:      "a pending signal is output",
			setup:     func(f *fakeTerm) {},
			live:      true,
			watchExit: true,
			want:      outputMsg{gen: 3, ended: false},
		},
		{
			name: "a closed Updates channel ends the output",
			setup: func(f *fakeTerm) {
				<-f.updates
				close(f.updates)
			},
			live:      true,
			watchExit: true,
			want:      outputMsg{gen: 3, ended: true},
		},
		{
			name: "once the output has ended the wait is for the exit",
			setup: func(f *fakeTerm) {
				close(f.done)
			},
			live:      false,
			watchExit: true,
			want:      exitMsg{gen: 3},
		},
		{
			name: "a background job keeps the output open, and the exit is still seen",
			setup: func(f *fakeTerm) {
				<-f.updates
				close(f.done)
			},
			live:      true,
			watchExit: true,
			want:      exitMsg{gen: 3},
		},
		{
			name: "after the exit only the output is waited for",
			setup: func(f *fakeTerm) {
				close(f.done)
			},
			live:      true,
			watchExit: false,
			want:      outputMsg{gen: 3, ended: false},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeTerm()
			tt.setup(f)
			msgs := runWithin(t, waitEvent(f, 3, tt.live, tt.watchExit), 2*time.Second)
			if len(msgs) != 1 || msgs[0] != tt.want {
				t.Fatalf("messages = %#v, want [%#v]", msgs, tt.want)
			}
		})
	}
}

func TestOutputKeepsWaitingUntilTheExit(t *testing.T) {
	starter := &fakeStarter{term: newFakeTerm()}
	p := newTestPanel(t, starter)
	gen := p.gen

	// Output arrives: the wait is armed again.
	p.Update(outputMsg{gen: gen})
	if cmd := p.Sync(); cmd == nil || !p.armed {
		t.Fatal("the output wait was not armed again after output")
	}

	// The output ends with the PTY: the wait for the exit is armed.
	p.Update(outputMsg{gen: gen, ended: true})
	cmd := p.Sync()
	if cmd == nil || p.live {
		t.Fatal("the exit wait was not armed after the output ended")
	}
	close(starter.term.done)
	for _, msg := range messages(cmd) {
		p.Update(msg)
	}

	if !p.Exited() || p.armed {
		t.Fatalf("exited = %v, armed = %v; want exited and no wait", p.Exited(), p.armed)
	}
	g := grid.New(80, 6)
	p.Draw(g, frame.Layout{DockOn: true, W: 80, DockX: 0, DockY: 0, DockH: 6, EditorW: 80}, style.Styles{})
	if top := g.Plain()[0]; !strings.Contains(top, "(exited)") {
		t.Fatalf("title row = %q, want it to say the shell exited", top)
	}
	// Keys after the exit do not reach the dead shell.
	p.SendKey(tea.Key{Code: 'a', Text: "a"})
	if len(p.queue) != 0 || len(starter.term.writes) != 0 {
		t.Fatalf("queue = %d, writes = %q; want the dead shell to get nothing", len(p.queue), starter.term.writes)
	}
}

// TestDrawStaysInTheDockColumns checks the dock paints its own cells only: with a
// column beside it, every dock row leaves the cells left of DockX untouched and
// starts its title at DockX.
func TestDrawStaysInTheDockColumns(t *testing.T) {
	t.Parallel()
	p := newTestPanel(t, &fakeStarter{term: newFakeTerm()})
	const dockX, dockW, rows = 10, 40, 6
	l := frame.Layout{DockOn: true, W: dockX + dockW, DockX: dockX, DockY: 1, DockH: 4, EditorW: dockW}
	g := grid.New(l.W, rows)
	for y := range rows {
		for x := range dockX {
			g.Set(x, y, "#", grid.Style{})
		}
	}
	p.Draw(g, l, style.Styles{})
	plain := g.Plain()
	for y, row := range plain {
		if got := row[:dockX]; got != strings.Repeat("#", dockX) {
			t.Fatalf("row %d left of the dock = %q, want it untouched", y, got)
		}
	}
	if got := plain[l.DockY][dockX:]; !strings.HasPrefix(got, " Terminal") {
		t.Fatalf("dock title row = %q, want the title starting at column %d", got, dockX+1)
	}
}

// TestExitIsSeenWhileOutputStaysOpen checks the dock shows an exited shell at once,
// even while its output stays open.
func TestExitIsSeenWhileOutputStaysOpen(t *testing.T) {
	starter := &fakeStarter{term: newFakeTerm()}
	p := newTestPanel(t, starter)
	gen := p.gen

	// A background job still holds the PTY, so the output does not end. The shell
	// itself has exited, and the dock must show that.
	close(starter.term.done)
	p.Update(outputMsg{gen: gen})
	cmd := p.Sync()
	for range 8 {
		if p.Exited() {
			break
		}
		var next []tea.Cmd
		for _, msg := range runWithin(t, cmd, 2*time.Second) {
			if c, ok := p.Update(msg); ok {
				next = append(next, c)
			}
		}
		cmd = tea.Batch(next...)
		if cmd == nil {
			cmd = p.Sync()
		}
	}
	if !p.Exited() {
		t.Fatal("the dock does not show the shell as exited while its output stays open")
	}
	g := grid.New(80, 6)
	p.Draw(g, frame.Layout{DockOn: true, W: 80, DockX: 0, DockY: 0, DockH: 6, EditorW: 80}, style.Styles{})
	if top := g.Plain()[0]; !strings.Contains(top, "(exited)") {
		t.Fatalf("title row = %q, want it to say the shell exited", top)
	}
}

// flushInput runs the dock's queued input against the shell.
func flushInput(t *testing.T, p *Panel) {
	t.Helper()
	cmd := p.Sync()
	if cmd == nil {
		return
	}
	for _, msg := range messages(cmd) {
		if next, taken := p.Update(msg); taken && next != nil {
			for _, m := range messages(next) {
				p.Update(m)
			}
		}
	}
}

func TestCtrlVPasteFollowsPassCtrlVWithoutText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		pass  bool // terminal.pass_ctrl_v_without_text
		text  string
		err   error
		yank  string
		paste []string
		write []string
	}{
		{"text pastes", true, "clip", nil, "Y", []string{"clip"}, nil},
		{"no text passes the key", true, "", nil, "Y", nil, []string{"\x16"}},
		{"no text with the flag off pastes the yank", false, "", nil, "Y", []string{"Y"}, nil},
		{"no text with the flag off and no yank does nothing", false, "", nil, "", nil, nil},
		{"an unreadable clipboard pastes the yank", true, "", errors.New("no display"), "Y", []string{"Y"}, nil},
		{"an unreadable clipboard with the flag off and no yank does nothing", false, "", errors.New("no display"), "", nil, nil},
		{"an unreadable clipboard without a yank passes the key", true, "", errors.New("no display"), "", nil, []string{"\x16"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, term := livePanel(t, "hi")
			p.SetEffects(Effects{PassCtrlVWithoutText: tt.pass})
			p.host.LastYank = func() string { return tt.yank }
			if _, ok := p.Update(clipboardMsg{gen: p.gen, text: tt.text, err: tt.err}); !ok {
				t.Fatal("the clipboard message was not taken")
			}
			flushInput(t, p)
			if !slices.Equal(pastes(term), tt.paste) || !slices.Equal(term.writes, tt.write) {
				t.Fatalf("pastes %q writes %q, want %q %q", pastes(term), term.writes, tt.paste, tt.write)
			}
		})
	}
}

// pastes returns the shell's paste texts.
func pastes(term *fakeTerm) []string { return term.pastes }
