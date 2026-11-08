package app

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/internal/tui/palette"
	"github.com/tbxark/gvim/keymap"
	"github.com/tbxark/gvim/theme"
)

func TestTerminalKeyContextMapping(t *testing.T) {
	tests := []struct {
		name string
		ctx  tui.KeyContext
		want keymap.Context
	}{
		{name: "terminal dock", ctx: tui.KeyContextTerminal, want: keymap.ContextTerminal},
		{name: "file explorer", ctx: tui.KeyContextFileExplorer, want: keymap.ContextFileExplorer},
		{name: "normal editor", ctx: tui.KeyContextNormal, want: keymap.ContextNormal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := keymapContext(tt.ctx); got != tt.want {
				t.Fatalf("keymapContext(%q) = %q, want %q", tt.ctx, got, tt.want)
			}
		})
	}
}

func TestClipboardCommands(t *testing.T) {
	tests := []struct {
		goos string
		want [][]string
	}{
		{goos: "darwin", want: [][]string{{"pbpaste"}}},
		{goos: "linux", want: [][]string{
			{"wl-paste", "--no-newline"},
			{"xclip", "-selection", "clipboard", "-out"},
			{"xsel", "--clipboard", "--output"},
		}},
		{goos: "windows", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			if got := clipboardCommands(tt.goos); !slices.EqualFunc(got, tt.want, slices.Equal[[]string]) {
				t.Fatalf("clipboardCommands(%q) = %q, want %q", tt.goos, got, tt.want)
			}
		})
	}
}

func TestTermStarterReturnsNilTerminalOnError(t *testing.T) {
	// A width no PTY can hold is refused before any process starts, on every platform.
	sh := termStarter{ctx: context.Background()}
	got, err := sh.Start(t.TempDir(), 70000, 24)
	if err == nil {
		t.Fatal("Start with an invalid size error = nil, want an error")
	}
	if got != nil {
		t.Fatalf("Start returned %v with the error, want a nil Terminal", got)
	}
}

// cmdPump runs the commands the model returns the way the program does: each one in
// its own goroutine, with every message it produces sent back to the test loop.
type cmdPump struct {
	ctx  context.Context
	msgs chan tea.Msg
}

func newCmdPump(ctx context.Context) cmdPump {
	return cmdPump{ctx: ctx, msgs: make(chan tea.Msg, 64)}
}

// start runs cmd in the background. A batch runs each of its commands.
func (p cmdPump) start(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, sub := range batch {
				p.start(sub)
			}
			return
		}
		if msg == nil {
			return
		}
		select {
		case p.msgs <- msg:
		case <-p.ctx.Done():
		}
	}()
}

// until feeds messages to m until the frame text satisfies ok. It fails the test at
// the deadline, and it shows the last frame.
func (p cmdPump) until(t *testing.T, m *tui.Model, ok func(frame string) bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		frame := ansi.Strip(m.View().Content)
		if ok(frame) {
			return
		}
		select {
		case msg := <-p.msgs:
			_, cmd := m.Update(msg)
			p.start(cmd)
		case <-deadline:
			t.Fatalf("the frame did not reach the expected state; last frame:\n%s", frame)
		}
	}
}

// TestDockRunsAShellAndShowsItsOutput drives the dock the way the program does. A
// command typed into the dock reaches the shell, and the shell's output shows in the
// dock. It runs /bin/sh with $SHELL set to it, so it is skipped without /bin/sh.
func TestDockRunsAShellAndShowsItsOutput(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("needs /bin/sh:", err)
	}
	t.Setenv("SHELL", "/bin/sh")
	ctx := t.Context()

	tree, err := explorer.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := tui.New(newUIDeps(editor.New(editor.Options{}), keymap.Default(), theme.Theme{}, tree, nil))
	m.SetDock(tui.DockPorts{Starter: termStarter{ctx: ctx}})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	p := newCmdPump(ctx)
	_, cmd := m.Update(palette.RunCommandMsg{Action: "open_terminal_in_dock"})
	p.start(cmd)
	p.until(t, m, func(frame string) bool { return !strings.Contains(frame, "Starting a shell") })

	// The command echoes as typed, so the marker is built by the shell: the typed text
	// has "got%s" and the output has "gotok".
	for _, r := range "printf 'got%s\\n' ok" {
		_, cmd := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		p.start(cmd)
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	p.start(cmd)
	p.until(t, m, func(frame string) bool { return strings.Contains(frame, "gotok") })
}
