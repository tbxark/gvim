// Package uitest drives a Bubble Tea v2 program in process and checks the screen
// it produces, cell by cell, without golden files.
//
// Messages (keys, mouse, and the sync points that Settle uses) go through the
// real program loop, so Update runs exactly as in the application. The screen
// is the model's own View after each update, written into a terminal emulator
// (github.com/charmbracelet/x/vt). The renderer's diff output is not replayed:
// a test checks what the model draws, and renderer correctness is left to
// Bubble Tea. Key and mouse decoding in the terminal driver is not exercised.
package uitest

import (
	"fmt"
	"image/color"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
)

// waitLimit bounds Settle and WaitFor so a broken test fails instead of
// hanging. pollEvery is the pause between checks.
const (
	waitLimit = 3 * time.Second
	pollEvery = 5 * time.Millisecond
)

// Session is one running program. Create it with Start; Close releases it, and
// Start also registers Close with t.Cleanup.
type Session struct {
	tb   testing.TB
	prog *tea.Program
	in   *io.PipeWriter
	w, h int

	mu  sync.Mutex // guards emu and final
	emu *vt.Emulator

	done      chan struct{}
	closeOnce sync.Once
	final     tea.Model
}

// dropEmptySize removes the 0x0 window-size messages that Bubble Tea sends
// when its output is not a terminal. The size the harness sets is kept.
func dropEmptySize(_ tea.Model, msg tea.Msg) tea.Msg {
	if ws, ok := msg.(tea.WindowSizeMsg); ok && ws.Width == 0 && ws.Height == 0 {
		return nil
	}
	return msg
}

// syncMsg asks the program to acknowledge once every earlier message has been
// handled. Settle uses it as a barrier.
type syncMsg struct{ ack chan struct{} }

// recorder wraps the model. After each update it draws the model's view into
// the emulator, so the screen is always the latest frame.
type recorder struct {
	inner tea.Model
	s     *Session
}

// Init implements tea.Model.
func (r *recorder) Init() tea.Cmd {
	cmd := r.inner.Init()
	r.s.draw(r.inner.View())
	return cmd
}

// Update implements tea.Model. Sync messages are acknowledged here and never
// reach the wrapped model.
func (r *recorder) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if sm, ok := msg.(syncMsg); ok {
		close(sm.ack)
		return r, nil
	}
	next, cmd := r.inner.Update(msg)
	r.inner = next
	r.s.draw(next.View())
	return r, cmd
}

// View implements tea.Model. The screen comes from the emulator, so this only
// has to exist; the renderer's output is discarded.
func (r *recorder) View() tea.View { return tea.NewView("") }

// Start runs m in a program of w columns and h rows. It returns once the first
// frame is drawn.
func Start(tb testing.TB, m tea.Model, w, h int) *Session {
	tb.Helper()
	pr, pw := io.Pipe()
	s := &Session{
		tb:   tb,
		in:   pw,
		w:    w,
		h:    h,
		emu:  vt.NewEmulator(w, h),
		done: make(chan struct{}),
	}
	rec := &recorder{inner: m, s: s}
	s.prog = tea.NewProgram(rec,
		tea.WithInput(pr),
		tea.WithOutput(io.Discard),
		// Bubble Tea reports a 0x0 size when its output is not a terminal. That
		// message would blank the model after the harness sets the real size.
		tea.WithFilter(dropEmptySize),
		tea.WithoutSignalHandler(),
	)
	go func() {
		final, _ := s.prog.Run()
		s.mu.Lock()
		if r, ok := final.(*recorder); ok {
			s.final = r.inner
		}
		s.mu.Unlock()
		close(s.done)
	}()
	tb.Cleanup(func() { s.Close() })
	// The size is sent as a message, not by a program option, so that the model
	// has it before the first assertion.
	s.Send(tea.WindowSizeMsg{Width: w, Height: h})
	s.Settle()
	return s
}

// draw replays one frame into the emulator: clear the screen, then the view's
// content with CRLF line ends, as a terminal expects.
func (s *Session) draw(v tea.View) {
	content := strings.ReplaceAll(v.Content, "\n", "\r\n")
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.emu.Write([]byte("\x1b[H\x1b[2J" + content))
}

// Send delivers a message to the program.
func (s *Session) Send(msg tea.Msg) {
	s.tb.Helper()
	s.prog.Send(msg)
}

// Key sends one key given in the notation of the keymap files: a single
// character ("x"), or modifiers joined to a key name with '+', as in
// "ctrl+s", "alt+f", "shift+left", "f10" or "ctrl+pgdown". Key names are
// enter, esc, tab, backspace, delete, space, up, down, left, right, home, end,
// pgup, pgdown, and f1 to f12.
func (s *Session) Key(spec string) {
	s.tb.Helper()
	msg, err := ParseKey(spec)
	if err != nil {
		s.tb.Fatalf("uitest: %v", err)
	}
	s.Send(msg)
}

// Type sends each rune of text as one key press.
func (s *Session) Type(text string) {
	s.tb.Helper()
	for _, r := range text {
		s.Send(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// Click presses and releases the left button at column x, row y (zero-based).
func (s *Session) Click(x, y int) {
	s.tb.Helper()
	s.Press(x, y)
	s.Release(x, y)
}

// Press presses the left button at column x, row y.
func (s *Session) Press(x, y int) {
	s.tb.Helper()
	s.Send(tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft}))
}

// Release releases the left button at column x, row y.
func (s *Session) Release(x, y int) {
	s.tb.Helper()
	s.Send(tea.MouseReleaseMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft}))
}

// Drag moves the pointer to column x, row y while the left button is held.
func (s *Session) Drag(x, y int) {
	s.tb.Helper()
	s.Send(tea.MouseMotionMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft}))
}

// Hover moves the pointer to column x, row y with no button held.
func (s *Session) Hover(x, y int) {
	s.tb.Helper()
	s.Send(tea.MouseMotionMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseNone}))
}

// Wheel scrolls one notch up or down at column x, row y.
func (s *Session) Wheel(x, y int, up bool) {
	s.tb.Helper()
	b := tea.MouseWheelDown
	if up {
		b = tea.MouseWheelUp
	}
	s.Send(tea.MouseWheelMsg(tea.Mouse{X: x, Y: y, Button: b}))
}

// Settle waits until every message sent so far has been handled and drawn. It
// sends a sync message through the program and waits for its acknowledgement.
func (s *Session) Settle() {
	s.tb.Helper()
	ack := make(chan struct{})
	s.prog.Send(syncMsg{ack: ack})
	select {
	case <-ack:
	case <-s.done:
	case <-time.After(waitLimit):
		s.tb.Fatalf("uitest: program did not handle a message within %v\n%s", waitLimit, s.Screen())
	}
}

// WaitFor settles, then waits until cond holds for the screen. Work that
// finishes later (a command's result) shows up on a later check. On timeout it
// fails the test and prints the last screen.
func (s *Session) WaitFor(cond func(Screen) bool) Screen {
	s.tb.Helper()
	deadline := time.Now().Add(waitLimit)
	for {
		s.Settle()
		sc := s.Screen()
		if cond(sc) {
			return sc
		}
		if time.Now().After(deadline) {
			s.tb.Fatalf("uitest: condition not met within %v\n%s", waitLimit, sc)
		}
		time.Sleep(pollEvery)
	}
}

// Screen returns a snapshot of the emulated screen.
func (s *Session) Screen() Screen {
	s.mu.Lock()
	defer s.mu.Unlock()
	sc := Screen{w: s.w, h: s.h, cells: make([][]Cell, s.h)}
	for y := 0; y < s.h; y++ {
		row := make([]Cell, s.w)
		for x := 0; x < s.w; x++ {
			row[x] = cellAt(s.emu, x, y)
		}
		sc.cells[y] = row
	}
	return sc
}

// cellAt converts one emulator cell. A missing cell is a blank.
func cellAt(e *vt.Emulator, x, y int) Cell {
	c := e.CellAt(x, y)
	if c == nil {
		return blankCell()
	}
	ch := c.Content
	if ch == "" {
		ch = " "
	}
	cell := Cell{
		Char:      ch,
		Underline: c.Style.Underline != uv.UnderlineNone,
		Bold:      c.Style.Attrs&uv.AttrBold != 0,
	}
	cell.Fg, cell.DefaultFg = resolveColor(c.Style.Fg, defaultFg)
	cell.Bg, cell.DefaultBg = resolveColor(c.Style.Bg, defaultBg)
	if c.Style.Attrs&uv.AttrReverse != 0 {
		cell.Fg, cell.Bg = cell.Bg, cell.Fg
		cell.DefaultFg, cell.DefaultBg = cell.DefaultBg, cell.DefaultFg
	}
	return cell
}

// resolveColor converts an emulator colour to RGBA. A nil colour is the
// terminal default, which is def; the flag reports that case.
func resolveColor(c color.Color, def color.RGBA) (color.RGBA, bool) {
	if c == nil {
		return def, true
	}
	return color.RGBAModel.Convert(c).(color.RGBA), false
}

// Screenshot writes the current frame to a PNG file at path. The image is
// Screen.PNG of the same snapshot that Screen returns.
func (s *Session) Screenshot(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := s.Screen().PNG(f); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// Close quits the program and returns its final model. Close is safe to call
// more than once. A program that does not quit within the limit is killed.
func (s *Session) Close() tea.Model {
	s.closeOnce.Do(func() {
		s.prog.Send(tea.QuitMsg{})
		select {
		case <-s.done:
		case <-time.After(waitLimit):
			s.prog.Kill()
			<-s.done
		}
		_ = s.in.Close()
	})
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.final
}

// String formats a screen for failure messages: one line per row, trailing
// blanks removed, and the row number in front.
func (sc Screen) String() string {
	var b strings.Builder
	for y := range sc.cells {
		fmt.Fprintf(&b, "%2d|%s\n", y, strings.TrimRight(sc.Row(y), " "))
	}
	return b.String()
}
