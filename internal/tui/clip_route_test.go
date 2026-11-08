package tui

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/popup"
)

// recClipboard is a system clipboard that holds text, fails reads with err and
// records what is written to it.
type recClipboard struct {
	text   string
	err    error
	writes *[]string
}

func (c recClipboard) Read() (string, error) { return c.text, c.err }

func (c recClipboard) Write(s string) error {
	*c.writes = append(*c.writes, s)
	return nil
}

// clipModel returns a model over ed with cb as the system clipboard and Ctrl+V bound to paste.
func clipModel(t *testing.T, ed EditorState, cb ClipboardReader) *Model {
	t.Helper()
	keys := dockKeys()
	keys["normal|v|ctrl"] = Command{Action: "paste"}
	keys["normal|f|ctrl"] = Command{Action: "find"}
	m := New(Deps{Editor: ed, Explorer: newFakeTree(3), Keys: keys, Clock: testClock})
	m.SetDock(DockPorts{Clipboard: cb})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return m
}

func TestCopyAndCutReachTheSystemClipboard(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		action string
		text   string
		err    error
		want   []string
	}{
		{name: "copy", action: "copy", text: "abc", want: []string{"abc"}},
		{name: "cut", action: "cut", text: "a\nb\n", want: []string{"a\nb\n"}},
		{name: "copy with theme", action: "copy_with_theme", text: "abc", want: []string{"abc"}},
		{name: "an empty result leaves the clipboard alone", action: "copy", text: ""},
		{name: "a refused copy leaves the clipboard alone", action: "copy", text: "abc", err: editor.ErrNothingSelected},
		{name: "other actions leave the clipboard alone", action: "select_all", text: "/x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var native []string
			ed := &copyEditor{docEditor: newDocEditor("/ws/a.txt", false), text: tt.text, err: tt.err}
			m := clipModel(t, ed, recClipboard{writes: &native})
			cmd := m.dispatch(Command{Action: tt.action})
			osc := clipboardWrites(cmd)
			if !slices.Equal(osc, tt.want) {
				t.Fatalf("OSC 52 writes = %q, want %q", osc, tt.want)
			}
			// The native write runs when the batch runs; clipboardWrites already ran it.
			if !slices.Equal(native, tt.want) {
				t.Fatalf("native writes = %q, want %q", native, tt.want)
			}
		})
	}
}

func TestPasteReadsTheSystemClipboard(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cb   ClipboardReader
		want map[string]any // the arguments of the paste action
	}{
		{name: "system text is passed to the editor", cb: recClipboard{text: "from outside", writes: new([]string)}, want: map[string]any{"text": "from outside"}},
		{name: "an unreadable clipboard falls back to the register", cb: recClipboard{err: errors.New("no tool"), writes: new([]string)}},
		{name: "an empty clipboard falls back to the register", cb: recClipboard{writes: new([]string)}},
		{name: "no clipboard falls back to the register"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ed := newFakeEditor("hello")
			m := clipModel(t, ed, tt.cb)
			cmd := press(m, ctrl('v'))
			// Update does no IO: the clipboard is read by the command.
			if tt.cb != nil && len(ed.actions) != 0 {
				t.Fatalf("the editor ran %q before the clipboard was read", ed.actions)
			}
			feedMessages(m, cmd)
			if !slices.Equal(ed.actions, []editor.Action{"paste"}) {
				t.Fatalf("actions = %q, want one paste", ed.actions)
			}
			if !reflect.DeepEqual(ed.args[0], tt.want) {
				t.Fatalf("args = %v, want %v", ed.args[0], tt.want)
			}
		})
	}
}

func TestBracketedPasteGoesToTheEditor(t *testing.T) {
	t.Parallel()
	ed := newFakeEditor("hello")
	m := clipModel(t, ed, nil)
	m.Update(tea.PasteMsg{Content: "a\r\nb"})
	if !slices.Equal(ed.actions, []editor.Action{"paste"}) {
		t.Fatalf("actions = %q, want one paste", ed.actions)
	}
	if got := ed.args[0]["text"]; got != "a\r\nb" {
		t.Fatalf("text = %q, want the pasted text as it came", got)
	}
}

func TestBracketedPasteStaysWithTheFocusedPart(t *testing.T) {
	t.Parallel()
	t.Run("an open overlay takes it", func(t *testing.T) {
		t.Parallel()
		ed := newFakeEditor("hello")
		m := clipModel(t, ed, nil)
		press(m, ctrl('f'))
		if !m.findOpen() {
			t.Fatal("Ctrl+F did not open the find bar")
		}
		m.Update(tea.PasteMsg{Content: "needle"})
		if len(ed.actions) != 0 {
			t.Fatalf("the editor ran %q, want nothing", ed.actions)
		}
	})
	t.Run("the explorer ignores it", func(t *testing.T) {
		t.Parallel()
		ed := newFakeEditor("hello")
		m := clipModel(t, ed, nil)
		press(m, ctrl('e'))
		m.Update(tea.PasteMsg{Content: "x"})
		if len(ed.actions) != 0 {
			t.Fatalf("the editor ran %q, want nothing", ed.actions)
		}
	})
	t.Run("the dock gets it", func(t *testing.T) {
		t.Parallel()
		starter := newFakeStarter()
		ed := newFakeEditor("hello")
		m := newDockModel(t, ed, DockPorts{Starter: starter})
		openDock(t, m)
		_, cmd := m.Update(tea.PasteMsg{Content: "ls\r\n"})
		dockSettle(t, m, cmd)
		if !slices.Equal(starter.term.pastes, []string{"ls\n"}) {
			t.Fatalf("pastes = %q", starter.term.pastes)
		}
		if len(ed.actions) != 0 {
			t.Fatalf("the editor ran %q, want nothing", ed.actions)
		}
	})
}

// TestMacroPasteKeepsItsPlaceInTheReplay checks that a recorded paste runs at its turn in
// the replay, not after the steps that follow it, and does not read the system clipboard.
func TestMacroPasteKeepsItsPlaceInTheReplay(t *testing.T) {
	t.Parallel()
	ed := newFakeEditor("hello")
	m := clipModel(t, ed, recClipboard{text: "SYS", writes: new([]string)})
	if err := m.Run("prompt_record_macro"); err != nil {
		t.Fatal(err)
	}
	feedMessages(m, m.dispatch(Command{Action: "paste"}))
	press(m, typed("a"))
	if err := m.Run("stop_macro_recording"); err != nil {
		t.Fatal(err)
	}
	ed.actions, ed.args = nil, nil
	feedMessages(m, m.playMacro())
	want := []editor.Action{"paste", "insert_char"}
	if !slices.Equal(ed.actions, want) {
		t.Fatalf("actions = %q, want %q", ed.actions, want)
	}
	if ed.args[0]["text"] != nil {
		t.Fatalf("the replayed paste carried %v, want the editor's own register", ed.args[0]["text"])
	}
}

func TestBracketedPasteClosesAPopupAndReachesTheEditor(t *testing.T) {
	t.Parallel()
	ed := newFakeEditor("hello")
	m := clipModel(t, ed, nil)
	m.ShowPopup(popup.Point{X: 1, Y: 1}, "t", []popup.Item{{Label: "a"}})
	m.Update(tea.PasteMsg{Content: "needle"})
	if !slices.Equal(ed.actions, []editor.Action{"paste"}) {
		t.Fatalf("actions = %q, want one paste", ed.actions)
	}
	if len(m.overlays) != 0 {
		t.Fatalf("%d overlays left open, want the popup closed", len(m.overlays))
	}
}
