package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/dialog"
	"github.com/tbxark/gvim/theme"
)

// quitsWith reports whether the messages cmd produces include tea.QuitMsg.
func quitsWith(cmd tea.Cmd) bool {
	for _, msg := range runCmd(cmd) {
		if _, ok := msg.(tea.QuitMsg); ok {
			return true
		}
	}
	return false
}

// confirmModel is a model whose editor wants the quit confirmed.
func confirmModel(t *testing.T, confirm bool, d *fakeDialogs, names ...string) (*Model, *tabFake) {
	t.Helper()
	tf := newTabFake(names...)
	tf.confirmQuit = confirm
	return shellModel(t, tf, d), tf
}

func TestConfirmQuitAsksBeforeQuitting(t *testing.T) {
	t.Parallel()
	d := &fakeDialogs{}
	m, _ := confirmModel(t, true, d, "a.txt")
	_, cmd := m.Update(ctrl('q'))
	if quitsWith(cmd) {
		t.Fatal("Ctrl+Q quit without asking")
	}
	if last := d.last(t); last.kind != "quit" {
		t.Fatalf("dialog = %+v, want the quit confirmation", last)
	}
	// Cancel keeps the editor open and says so.
	if quitsWith(answer(m, d, dialog.ChoiceCancel, "")) {
		t.Fatal("Cancel quit")
	}
	if m.notice != "Quit cancelled" {
		t.Fatalf("notice = %q", m.notice)
	}
	// Quit leaves.
	_, _ = m.Update(ctrl('q'))
	if !quitsWith(answer(m, d, dialog.ChoiceQuit, "")) {
		t.Fatal("Quit did not quit")
	}
}

func TestConfirmQuitOffQuitsAtOnce(t *testing.T) {
	t.Parallel()
	d := &fakeDialogs{}
	m, _ := confirmModel(t, false, d, "a.txt")
	_, cmd := m.Update(ctrl('q'))
	if !quitsWith(cmd) || len(d.opened) != 0 {
		t.Fatalf("quit = %v, dialogs = %+v; want an immediate quit", quitsWith(cmd), d.opened)
	}
}

func TestConfirmQuitWithModifiedTabsAsksOnlyAboutThem(t *testing.T) {
	t.Parallel()
	d := &fakeDialogs{}
	m, tf := confirmModel(t, true, d, "a.txt", "b.txt")
	tf.dirty[1] = true
	tf.sync()
	m.Update(ctrl('q'))
	if len(d.opened) != 1 || d.opened[0].kind != "unsaved" {
		t.Fatalf("dialogs = %+v, want only the unsaved-changes one", d.opened)
	}
}

func TestConfirmQuitMenuAndPaletteUseTheSameGate(t *testing.T) {
	t.Parallel()
	for name, run := range map[string]func(m *Model) tea.Cmd{
		"menu row": func(m *Model) tea.Cmd {
			if err := m.Run("quit"); err != nil {
				t.Fatal(err)
			}
			cmd := m.pending
			m.pending = nil
			return cmd
		},
		"palette": func(m *Model) tea.Cmd { return m.runCommand("quit") },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			d := &fakeDialogs{}
			m, _ := confirmModel(t, true, d, "a.txt")
			if quitsWith(run(m)) {
				t.Fatal("quit without asking")
			}
			if d.last(t).kind != "quit" {
				t.Fatalf("dialog = %+v", d.last(t))
			}
		})
	}
}

func TestConfirmQuitWithoutDialogsRefusesWithANotice(t *testing.T) {
	t.Parallel()
	tf := newTabFake("a.txt")
	tf.confirmQuit = true
	tf.quitAction = "quit"
	m := New(Deps{Editor: tf, Explorer: newFakeTree(3), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	_, cmd := m.Update(ctrl('q'))
	if quitsWith(cmd) || m.notice == "" {
		t.Fatalf("quit = %v, notice = %q; want a refusal with a notice", quitsWith(cmd), m.notice)
	}
}

// TestConfirmQuitRealDialogDefaultsToCancel drives the real dialog: Enter (after the grace
// period) and Esc both cancel, and the Quit hotkey quits.
func TestConfirmQuitRealDialogDefaultsToCancel(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		key  tea.KeyPressMsg
		quit bool
	}{
		{"enter", special(tea.KeyEnter), false},
		{"esc", special(tea.KeyEscape), false},
		{"hotkey q", typed("q"), true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			now := testClock()
			tf := newTabFake("a.txt")
			tf.confirmQuit = true
			tf.quitAction = "quit"
			m := New(Deps{
				Editor: tf, Explorer: newFakeTree(3), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock,
				Dialogs: &realDialogs{clock: func() time.Time { return now }},
			})
			m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
			m.Update(ctrl('q'))
			now = now.Add(time.Second) // past the grace period
			_, cmd := m.Update(tt.key)
			var quit bool
			for _, msg := range runCmd(cmd) {
				if _, ok := msg.(dialog.Result); ok {
					_, c := m.Update(msg)
					quit = quitsWith(c)
				}
			}
			if quit != tt.quit {
				t.Fatalf("quit = %v, want %v", quit, tt.quit)
			}
			if !tt.quit && m.notice != "Quit cancelled" {
				t.Fatalf("notice = %q, want Quit cancelled", m.notice)
			}
		})
	}
}
