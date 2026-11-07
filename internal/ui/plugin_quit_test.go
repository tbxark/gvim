package ui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// quitMsgs answers a key to the open confirm and returns the messages the answer produced,
// feeding the first round back into the model as the program would.
func quitMsgs(m *Model, k tea.KeyPressMsg) []tea.Msg {
	var out []tea.Msg
	for _, msg := range runCmd(press(m, k)) {
		out = append(out, msg)
		_, cmd := m.Update(msg)
		out = append(out, runCmd(cmd)...)
	}
	return out
}

func quitted(msgs []tea.Msg) bool {
	for _, msg := range msgs {
		if _, ok := msg.(tea.QuitMsg); ok {
			return true
		}
	}
	return false
}

// TestQuitDiscardRequiresConfirm: the hidden quit_discard action never quits or closes a
// tab by itself. It opens a question that names the tabs with unsaved changes, and only
// Yes discards them, each tab with force, last tab first, and quits.
func TestQuitDiscardRequiresConfirm(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		dirty      []bool
		key        tea.KeyPressMsg
		wantAsk    []string // substrings of the question
		wantClosed []int
		wantQuit   bool
	}{
		{"yes discards the dirty tabs and quits", []bool{false, true, true}, typed("y"), []string{"b.txt, c.txt", "and quit?"}, []int{2, 1}, true},
		{"no keeps everything", []bool{false, true, true}, typed("n"), []string{"b.txt, c.txt"}, nil, false},
		{"esc keeps everything", []bool{true, false, false}, special(tea.KeyEscape), []string{"a.txt"}, nil, false},
		{"clean tabs still ask, and yes only quits", []bool{false, false, false}, typed("y"), []string{"Quit without saving?"}, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tf := newTabFake("a.txt", "b.txt", "c.txt")
			copy(tf.dirty, tt.dirty)
			tf.sync()
			m := shellModel(t, tf, &fakeDialogs{})

			if cmd := m.dispatch(Command{Action: "quit_discard"}); cmd != nil {
				t.Fatalf("quit_discard returned a command before the answer: %v", runCmd(cmd))
			}
			if len(m.overlays) != 1 {
				t.Fatalf("overlays = %d, want the question", len(m.overlays))
			}
			if _, ok := m.overlays[0].(confirmOverlay); !ok {
				t.Fatalf("overlay = %T, want a confirm", m.overlays[0])
			}
			if len(tf.closed) != 0 || tf.quit {
				t.Fatalf("before the answer: closed %v, quit %v", tf.closed, tf.quit)
			}
			asked := strings.Join(screenRows(m), "\n")
			for _, want := range tt.wantAsk {
				if !strings.Contains(asked, want) {
					t.Errorf("the question does not contain %q:\n%s", want, asked)
				}
			}
			msgs := quitMsgs(m, tt.key)
			if !slices.Equal(tf.closed, tt.wantClosed) {
				t.Errorf("closed tabs = %v, want %v", tf.closed, tt.wantClosed)
			}
			if got := quitted(msgs); got != tt.wantQuit {
				t.Errorf("quit = %v, want %v (messages %v)", got, tt.wantQuit, msgs)
			}
			if len(m.overlays) != 0 {
				t.Errorf("overlays after the answer = %d", len(m.overlays))
			}
		})
	}
}

// TestQuitDiscardThroughHostDoWaitsAndAsks: a plugin that runs quit_discard with
// gvim.command.run reaches the same question, after its slice has returned.
func TestQuitDiscardThroughHostDoWaitsAndAsks(t *testing.T) {
	t.Parallel()
	rt := newFakeRuntime(t, "x.cmd")
	m, _ := newPluginModel(t, rt)
	rt.onRun = func(string, map[string]any) {
		if err := rt.host.Do("quit_discard", nil, 1); err != nil {
			t.Error(err)
		}
		if len(m.overlays) != 0 {
			t.Error("the question opened inside the slice")
		}
	}
	must(t, m.Run("x.cmd"))
	if len(m.overlays) != 1 {
		t.Fatalf("overlays = %d, want the question", len(m.overlays))
	}
	if _, ok := m.overlays[0].(confirmOverlay); !ok {
		t.Fatalf("overlay = %T, want a confirm", m.overlays[0])
	}
	if msgs := quitMsgs(m, typed("n")); quitted(msgs) {
		t.Fatal("No quit the editor")
	}
}
