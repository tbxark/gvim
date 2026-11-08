package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/theme"
)

// macroKeys binds the macro keys the way the default keymap does, so a test can
// play the macro with a key press as well as with the action.
func macroKeys() fakeKeymap {
	k := testKeys()
	k["normal|F4|"] = Command{Action: "play_last_macro"}
	k["normal|F5|"] = Command{Action: "stop_macro_recording"}
	return k
}

// actionsOf returns the editor actions the fake editor ran, in order.
func actionsOf(ed *fakeEditor) []string {
	out := make([]string, len(ed.actions))
	for i, a := range ed.actions {
		out[i] = string(a)
	}
	return out
}

// TestMacroRecordsTypingAndCommandsAndPlaysThemBack records a save and a typed
// letter, then plays the macro by action. The editor sees the same actions again.
func TestMacroRecordsTypingAndCommandsAndPlaysThemBack(t *testing.T) {
	t.Parallel()
	ed := newFakeEditor("x")
	m := newTestModel(t, ed, nil, 80, 20)
	m.keys = macroKeys()

	must(t, m.Run("prompt_record_macro"))
	press(m, ctrl('s'))
	press(m, typed("a"))
	must(t, m.Run("stop_macro_recording"))
	if got, want := actionsOf(ed), []string{"save", "insert_char"}; !slices.Equal(got, want) {
		t.Fatalf("recording ran %v, want %v", got, want)
	}

	ed.actions, ed.args = nil, nil
	must(t, m.Run("play_last_macro"))
	if got, want := actionsOf(ed), []string{"save", "insert_char"}; !slices.Equal(got, want) {
		t.Fatalf("playback ran %v, want %v", got, want)
	}
	if got := ed.args[1]["text"]; got != "a" {
		t.Fatalf("playback typed %v, want a", got)
	}
}

// TestMacroPlayByKeyUsesTheKeymap checks that the macro key of the keymap plays the
// last macro, as typing it would.
func TestMacroPlayByKeyUsesTheKeymap(t *testing.T) {
	t.Parallel()
	ed := newFakeEditor("x")
	m := newTestModel(t, ed, nil, 80, 20)
	m.keys = macroKeys()

	must(t, m.Run("prompt_record_macro"))
	press(m, typed("q"))
	press(m, special(tea.KeyF5))
	ed.actions, ed.args = nil, nil
	press(m, special(tea.KeyF4))
	if got, want := actionsOf(ed), []string{"insert_char"}; !slices.Equal(got, want) {
		t.Fatalf("F4 ran %v, want %v", got, want)
	}
}

// TestMacroControlActionsAreNotRecorded checks that the macro's own actions are not
// part of the recording: starting, stopping and playing leave only the typing.
func TestMacroControlActionsAreNotRecorded(t *testing.T) {
	t.Parallel()
	ed := newFakeEditor("x")
	m := newTestModel(t, ed, nil, 80, 20)
	m.keys = macroKeys()

	must(t, m.Run("prompt_record_macro"))
	press(m, typed("a"))
	must(t, m.Run("play_last_macro")) // plays nothing yet, and must not record itself
	press(m, special(tea.KeyF5))
	if got := m.root.macro.last; len(got) != 1 || got[0].cmd.Action != "insert_char" {
		t.Fatalf("recorded %v, want one insert_char", got)
	}
}

// TestMacroToInitActionsAreNotRecorded checks that saving or promoting the recorded macro
// while a recording is on is not a step of that recording: replaying it would open the
// name prompt again, and the saved command would contain the save action.
func TestMacroToInitActionsAreNotRecorded(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"prompt_save_macro_to_init", "prompt_promote_macro"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			ed := newFakeEditor("x")
			m := newTestModel(t, ed, nil, 80, 20)
			m.keys = macroKeys()

			must(t, m.Run("prompt_record_macro"))
			press(m, typed("a"))
			must(t, m.Run("stop_macro_recording"))
			must(t, m.Run("prompt_record_macro"))
			press(m, typed("b"))
			must(t, m.Run(action))
			press(m, special(tea.KeyEscape)) // the name prompt
			must(t, m.Run("stop_macro_recording"))
			got := m.root.macro.last
			if len(got) != 1 || got[0].cmd.Action != "insert_char" {
				t.Fatalf("recorded %v, want one insert_char", got)
			}
		})
	}
}

// TestMacroPaletteOpeningIsNotRecorded checks that opening the palette to stop the
// recording (Ctrl+P, then Record Macro) is not the last step of the macro: replaying
// it would open the palette at the end.
func TestMacroPaletteOpeningIsNotRecorded(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"quick_open", "command_palette", "quick_open_files", "quick_open_buffers"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			ed := newFakeEditor("x")
			m := newTestModel(t, ed, nil, 80, 20)
			m.keys = macroKeys()

			must(t, m.Run("prompt_record_macro"))
			press(m, typed("a"))
			must(t, m.Run(action))
			must(t, m.Run("stop_macro_recording"))
			got := m.root.macro.last
			if len(got) != 1 || got[0].cmd.Action != "insert_char" {
				t.Fatalf("recorded %v, want one insert_char", got)
			}
		})
	}
}

// TestMacroPlayWhileRecordingIsNotRecorded checks that a playback made while the
// recorder is on does not add its own actions to the recording.
func TestMacroPlayWhileRecordingIsNotRecorded(t *testing.T) {
	t.Parallel()
	ed := newFakeEditor("x")
	m := newTestModel(t, ed, nil, 80, 20)
	m.keys = macroKeys()

	must(t, m.Run("prompt_record_macro"))
	press(m, typed("a"))
	must(t, m.Run("stop_macro_recording"))
	must(t, m.Run("prompt_record_macro"))
	must(t, m.Run("play_last_macro"))
	must(t, m.Run("stop_macro_recording"))
	if got := m.root.macro.last; len(got) != 1 {
		t.Fatalf("recording while playing kept %d actions, want 1", len(got))
	}
}

// TestMacroPlayWithoutRecordingNotices checks that playing before any recording
// runs nothing and says so.
func TestMacroPlayWithoutRecordingNotices(t *testing.T) {
	t.Parallel()
	ed := newFakeEditor("x")
	m := newTestModel(t, ed, nil, 80, 20)
	must(t, m.Run("play_last_macro"))
	if len(ed.actions) != 0 {
		t.Fatalf("playback with no macro ran %v", actionsOf(ed))
	}
	if got := m.statusView().message; got != "No macro recorded" {
		t.Fatalf("status = %q, want the no-macro notice", got)
	}
}

// TestMacroStopWithoutRecordingNotices checks that stopping when nothing is being
// recorded says so and keeps the last macro.
func TestMacroStopWithoutRecordingNotices(t *testing.T) {
	t.Parallel()
	ed := newFakeEditor("x")
	m := newTestModel(t, ed, nil, 80, 20)
	must(t, m.Run("stop_macro_recording"))
	if got := m.statusView().message; got != "Not recording a macro" {
		t.Fatalf("status = %q, want the not-recording notice", got)
	}
}

// TestMacroRecordAgainReplacesTheLast checks that a second recording replaces the
// last macro rather than appending to it.
func TestMacroRecordAgainReplacesTheLast(t *testing.T) {
	t.Parallel()
	ed := newFakeEditor("x")
	m := newTestModel(t, ed, nil, 80, 20)
	m.keys = macroKeys()

	must(t, m.Run("prompt_record_macro"))
	press(m, typed("a"))
	must(t, m.Run("stop_macro_recording"))
	must(t, m.Run("prompt_record_macro"))
	press(m, typed("b"))
	must(t, m.Run("stop_macro_recording"))
	if got := m.root.macro.last; len(got) != 1 || got[0].cmd.Args["text"] != "b" {
		t.Fatalf("last macro = %v, want only the second recording", got)
	}
}

// hookEditor is a fake editor that runs hook before each action, so a test can act
// in the middle of a replay.
type hookEditor struct {
	*fakeEditor
	hook func(a editor.Action)
}

func (h *hookEditor) Do(a editor.Action, args map[string]any) editor.Result {
	if h.hook != nil {
		h.hook(a)
	}
	return h.fakeEditor.Do(a, args)
}

// TestMacroRecordsEachCommandWithItsFocus checks that a recording keeps every command
// with the focus it ran in: an overlay on top, the explorer, or the tab strip. A
// command run by name, as the palette and the menu run theirs, is recorded too.
func TestMacroRecordsEachCommandWithItsFocus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		focus  func(m *Model) // the focus set before the recording starts
		act    func(m *Model) // the command under test, run while recording
		action string
		ctx    macroCtx
	}{
		{
			name:   "editor",
			act:    func(m *Model) { m.dispatch(Command{Action: "save"}) },
			action: "save",
		},
		{
			name:   "explorer",
			focus:  func(m *Model) { m.takeExplorerFocus() },
			act:    func(m *Model) { press(m, special(tea.KeyDown)) },
			action: "file_explorer_down",
			ctx:    macroCtx{explorer: true},
		},
		{
			name:   "tab strip",
			focus:  func(m *Model) { m.setStage(stageTabs) },
			act:    func(m *Model) { press(m, special(tea.KeyRight)) },
			action: "next_buffer",
			ctx:    macroCtx{tabs: true},
		},
		{
			name:   "overlay",
			focus:  func(m *Model) { m.dispatch(Command{Action: "command_palette"}) },
			act:    func(m *Model) { m.dispatch(Command{Action: "save"}) },
			action: "save",
			ctx:    macroCtx{overlay: true},
		},
		{
			name:   "run by name",
			act:    func(m *Model) { _ = m.Run("save") },
			action: "save",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := newTestModel(t, newFakeEditor("x"), newFakeTree(3), 80, 24)
			m.keys = macroKeys()
			if tc.focus != nil {
				tc.focus(m)
			}
			must(t, m.Run("prompt_record_macro"))
			tc.act(m)
			must(t, m.Run("stop_macro_recording"))
			got := m.root.macro.last
			if len(got) != 1 || got[0].cmd.Action != tc.action || got[0].ctx != tc.ctx {
				t.Fatalf("recorded %+v, want %s in %+v", got, tc.action, tc.ctx)
			}
		})
	}
}

// TestMacroReplaySkipsWhatTheFocusCannotRun records a macro in one focus and replays
// it in another. A command the focus at its step cannot run is skipped, the notice
// counts the skipped commands, and only the commands that run reach the editor.
func TestMacroReplaySkipsWhatTheFocusCannotRun(t *testing.T) {
	t.Parallel()
	insert := func(m *Model) { m.dispatch(Command{Action: "insert_char", Args: map[string]any{"text": "a"}}) }
	save := func(m *Model) { m.dispatch(Command{Action: "save"}) }
	explorer := func(m *Model) { m.takeExplorerFocus() }
	tests := []struct {
		name         string
		record       func(m *Model) // the commands of the recording, in the focus each ran in
		replay       func(m *Model) // the focus before the replay; the replay starts in the editor
		wantEditor   []string       // the editor actions the replay ran
		wantSkipped  int            // the commands the replay skipped
		wantOverlay  bool           // an overlay is open after the replay
		wantExplorer bool           // the explorer has focus after the replay
	}{
		{
			name:         "save runs from the explorer",
			record:       save,
			replay:       explorer,
			wantEditor:   []string{"save"},
			wantExplorer: true,
		},
		{
			name:         "typed text is skipped in the explorer",
			record:       insert,
			replay:       explorer,
			wantSkipped:  1,
			wantExplorer: true,
		},
		{
			name: "explorer keys are skipped in the editor",
			record: func(m *Model) {
				explorer(m)
				m.dispatch(Command{Action: "file_explorer_down"})
			},
			wantSkipped: 1,
		},
		{
			name:        "copy from the editor is skipped in the explorer",
			record:      func(m *Model) { m.dispatch(Command{Action: "copy"}) },
			replay:      explorer,
			wantSkipped: 1, wantExplorer: true,
		},
		{
			name: "copy from the explorer is skipped in the editor",
			record: func(m *Model) {
				explorer(m)
				m.dispatch(Command{Action: "copy"})
			},
			wantSkipped: 1,
		},
		{
			name: "a focus change in the macro gives the explorer its keys",
			record: func(m *Model) {
				m.dispatch(Command{Action: "focus_file_explorer"})
				m.dispatch(Command{Action: "file_explorer_down"})
			},
			wantExplorer: true,
		},
		{
			name: "an overlay stops the rest of the macro",
			record: func(m *Model) {
				m.dispatch(Command{Action: "goto_line"})
				press(m, special(tea.KeyEscape)) // closes the overlay before the next step
				save(m)
			},
			wantSkipped: 1,
			wantOverlay: true,
		},
		{
			name:        "typed text is skipped on the tab strip",
			record:      insert,
			replay:      func(m *Model) { m.setStage(stageTabs) },
			wantSkipped: 1,
		},
		{
			name:       "a buffer switch runs from the tab strip",
			record:     func(m *Model) { m.dispatch(Command{Action: "next_buffer"}) },
			replay:     func(m *Model) { m.setStage(stageTabs) },
			wantEditor: []string{"next_buffer"},
		},
		{
			name: "skipped commands are counted",
			record: func(m *Model) {
				save(m)
				insert(m)
				save(m)
				insert(m)
			},
			replay:       explorer,
			wantEditor:   []string{"save", "save"},
			wantSkipped:  2,
			wantExplorer: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ed := newFakeEditor("x")
			m := newTestModel(t, ed, newFakeTree(3), 80, 24)
			m.keys = macroKeys()
			must(t, m.Run("prompt_record_macro"))
			tc.record(m)
			must(t, m.Run("stop_macro_recording"))

			m.focusEditor()
			if tc.replay != nil {
				tc.replay(m)
			}
			ed.actions, ed.args = nil, nil
			must(t, m.Run("play_last_macro"))
			if got := actionsOf(ed); !slices.Equal(got, tc.wantEditor) {
				t.Fatalf("replay ran %v, want %v", got, tc.wantEditor)
			}
			if tc.wantSkipped > 0 {
				want := fmt.Sprintf("Macro skipped %d of %d actions that cannot run here", tc.wantSkipped, len(m.root.macro.last))
				if m.notice != want {
					t.Fatalf("notice %q, want %q", m.notice, want)
				}
			} else if strings.Contains(m.notice, "Macro skipped") {
				t.Fatalf("notice %q on a replay that skipped nothing", m.notice)
			}
			if got := len(m.overlays) > 0; got != tc.wantOverlay {
				t.Fatalf("overlay open %v after the replay, want %v", got, tc.wantOverlay)
			}
			if m.explorerFocus != tc.wantExplorer {
				t.Fatalf("explorer focus %v after the replay, want %v", m.explorerFocus, tc.wantExplorer)
			}
		})
	}
}

// TestMacroReplayDoesNotRecordItselfOrNest runs a replay in which one step asks for
// another playback or a recording. The inner request does nothing, the replay runs
// each recorded step once, and the replayed steps are not recorded.
func TestMacroReplayDoesNotRecordItselfOrNest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		action string // the action the first replayed step runs
	}{
		{name: "a play from a step is ignored", action: "play_last_macro"},
		{name: "a record from a step records nothing", action: "prompt_record_macro"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := &hookEditor{fakeEditor: newFakeEditor("x")}
			m := New(Deps{Editor: h, Keys: macroKeys(), Theme: theme.Theme{}, Clock: testClock})
			m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			must(t, m.Run("prompt_record_macro"))
			press(m, typed("a"))
			must(t, m.Run("stop_macro_recording"))
			before := slices.Clone(m.root.macro.last)

			fired := false
			h.hook = func(editor.Action) {
				if !fired {
					fired = true
					_ = m.Run(tc.action)
				}
			}
			h.actions, h.args = nil, nil
			must(t, m.Run("play_last_macro"))
			if got, want := actionsOf(h.fakeEditor), []string{"insert_char"}; !slices.Equal(got, want) {
				t.Fatalf("replay ran %v, want %v once", got, want)
			}
			if got := m.root.macro.last; !slices.EqualFunc(got, before, func(a, b macroStep) bool { return a.cmd.Action == b.cmd.Action && a.ctx == b.ctx }) {
				t.Fatalf("the replay changed the macro to %+v, want %+v", got, before)
			}
			if got := len(m.root.macro.cmds); got != 0 {
				t.Fatalf("the replay recorded %d commands, want none", got)
			}
		})
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
