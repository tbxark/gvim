package ui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/keymap"
	"github.com/tbxark/gvim/internal/pluginapi"
	"github.com/tbxark/gvim/internal/theme"
)

// fakeStore is a KeybindingStore over an in-memory list. Preview returns a keymap whose
// bindings are the base ones with a list applied, and Save records the list as well: an
// unbind removes the binding of its chord and context, any other entry adds one.
type fakeStore struct {
	base   []KeyBinding
	custom []CustomBinding
	saves  int
	err    error

	previewErr error
}

func (s *fakeStore) Custom() []CustomBinding { return slices.Clone(s.custom) }

func (s *fakeStore) Save(list []CustomBinding, preset string) (Keymap, error) {
	s.saves++
	if s.err != nil {
		return nil, s.err
	}
	s.custom = slices.Clone(list)
	return fakeBindings{fakeKeymap: testKeys(), list: s.keymap()}, nil
}

func (s *fakeStore) Preview(list []CustomBinding, preset string) (Keymap, error) {
	if s.previewErr != nil {
		return nil, s.previewErr
	}
	return fakeBindings{fakeKeymap: testKeys(), list: s.keymapOf(list)}, nil
}

func (s *fakeStore) keymap() []KeyBinding { return s.keymapOf(s.custom) }

func (s *fakeStore) keymapOf(custom []CustomBinding) []KeyBinding {
	var out []KeyBinding
	for _, b := range s.base {
		removed := false
		for _, c := range custom {
			if c.Action == "unbind" && c.Chord == b.Chord && c.Context == b.Context {
				removed = true
			}
		}
		if !removed {
			out = append(out, b)
		}
	}
	for _, c := range custom {
		if c.Action != "unbind" {
			out = append(out, KeyBinding{Action: c.Action, Chord: c.Chord, Context: c.Context})
		}
	}
	return out
}

var editorBase = []KeyBinding{
	{Action: "save", Chord: "ctrl+s", Context: "normal"},
	{Action: "quit", Chord: "ctrl+q", Context: "normal"},
	{Action: "move_left", Chord: "left", Context: "normal"},
	{Action: "quick_open", Chord: "ctrl+p", Context: "global"},
}

// newKeybindModel returns a model with the keybinding editor open over editorBase and a
// store that holds custom.
func newKeybindModel(t *testing.T, custom ...CustomBinding) (*Model, *fakeStore) {
	t.Helper()
	st := &fakeStore{base: editorBase, custom: custom}
	keys := fakeBindings{fakeKeymap: testKeys(), list: st.keymap()}
	m := New(Deps{Editor: newFakeEditor("hello"), Keys: keys, Theme: theme.Theme{}, Clock: testClock,
		Settings: Settings{Keymap: "default", ConfigFile: "/home/u/.config/gvim/config.json"},
		Root:     RootPorts{Keybindings: st}})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	runAction(t, m, "open_keybinding_editor")
	if _, ok := m.overlays[len(m.overlays)-1].(*keybindEditor); !ok {
		t.Fatalf("top overlay = %T, want the keybinding editor", m.overlays[len(m.overlays)-1])
	}
	return m, st
}

// selectRowOf moves the highlight to the row of action.
func selectRowOf(t *testing.T, m *Model, action string) {
	t.Helper()
	e := m.overlays[len(m.overlays)-1].(*keybindEditor)
	for k, i := range e.shown {
		if e.rows[i].action == action {
			e.sel = k
			return
		}
	}
	t.Fatalf("no row for %s", action)
}

// finish runs the command a key press returned and feeds its message back, as the program does.
func finish(m *Model, cmd tea.Cmd) {
	if cmd != nil {
		m.Update(cmd())
	}
}

// editorOf returns the keybinding editor on top of the stack.
func editorOf(m *Model) *keybindEditor { return m.overlays[len(m.overlays)-1].(*keybindEditor) }

// saveAllNow presses Ctrl+S in the table and feeds the result of the write back.
func saveAllNow(t *testing.T, m *Model) {
	t.Helper()
	cmd := press(m, ctrl('s'))
	if cmd == nil {
		t.Fatal("Ctrl+S in the table started no save")
	}
	finish(m, cmd)
}

func rowText(m *Model, parts ...string) bool { return frameRowHas(m, parts...) }

func TestKeybindingEditorListsKeyActionContextAndSource(t *testing.T) {
	m, _ := newKeybindModel(t, CustomBinding{Chord: "ctrl+q", Action: "quit", Context: "normal"})
	if !rowText(m, "Ctrl+S", "Save", "normal", "keymap") {
		t.Errorf("the Save row lacks its key, description, context and source:\n%s", strings.Join(frameText(m), "\n"))
	}
	if !rowText(m, "Ctrl+Q", "Quit", "normal", "custom") {
		t.Error("the custom Quit row is not marked custom")
	}
	if !rowText(m, "Ctrl+P", "global") {
		t.Error("the global row is missing")
	}
	// The Action column is the machine name, as in Fresh, so only the key must not be raw.
	for _, raw := range []string{"ctrl+s", "ctrl+q"} {
		if frameHas(m, raw) {
			t.Errorf("the table shows the raw %q", raw)
		}
	}
}

func TestKeybindingEditorFooterOnlyAdvertisesWhatWorks(t *testing.T) {
	m, _ := newKeybindModel(t)
	foot := frameText(m)
	var line string
	for _, r := range foot {
		if strings.Contains(r, "Esc close") {
			line = r
		}
	}
	if line == "" {
		t.Fatalf("no footer:\n%s", strings.Join(foot, "\n"))
	}
	// Each advertised key must do what the footer says.
	for _, want := range []string{"/ filter", "r key", "c ctx", "s src", "Enter edit", "a add", "d unbind", "x disable", "Ctrl+S save", "Esc close"} {
		if !strings.Contains(line, want) {
			t.Errorf("footer %q lacks %q", line, want)
		}
	}
	if strings.Contains(line, "Enter run") {
		t.Errorf("footer %q advertises Enter run, which does nothing here", line)
	}
	press(m, special(tea.KeyEnter))
	if m.overlays[len(m.overlays)-1].(*keybindEditor).mode != kbEdit {
		t.Error("Enter did not open the edit dialog the footer promises")
	}
	press(m, special(tea.KeyEscape))
	press(m, typed("a"))
	if m.overlays[len(m.overlays)-1].(*keybindEditor).mode != kbEdit || m.overlays[len(m.overlays)-1].(*keybindEditor).form.orig != nil {
		t.Error("a did not open the add dialog")
	}
}

func TestKeybindingEditorRebindsAKeyByPressingIt(t *testing.T) {
	m, st := newKeybindModel(t)
	selectRowOf(t, m, "save")
	press(m, special(tea.KeyEnter))
	press(m, ctrl('k')) // the dialog starts listening for the new key
	if !frameHas(m, "Ctrl+K") {
		t.Fatalf("the dialog does not show the recorded key:\n%s", strings.Join(frameText(m), "\n"))
	}
	press(m, special(tea.KeyTab)) // to the action
	press(m, ctrl('s'))           // Ctrl+S applies the dialog from there
	if st.saves != 0 {
		t.Fatal("the dialog wrote the config; the change must stay pending until Ctrl+S in the table")
	}
	if !rowText(m, "Ctrl+K", "Save", "custom") || !frameHas(m, "not saved yet") {
		t.Errorf("the pending change is not shown as pending:\n%s", strings.Join(frameText(m), "\n"))
	}
	cmd := press(m, ctrl('s')) // Ctrl+S in the table writes it
	if cmd == nil {
		t.Fatal("saving returned no command")
	}
	if st.saves != 0 {
		t.Fatal("the config was written inside Update; it must run in a command")
	}
	finish(m, cmd)
	want := []CustomBinding{
		{Chord: "ctrl+s", Action: "unbind", Context: "normal"},
		{Chord: "ctrl+k", Action: "save", Context: "normal"},
	}
	if !slices.EqualFunc(st.custom, want, func(a, b CustomBinding) bool {
		return a.Chord == b.Chord && a.Action == b.Action && a.Context == b.Context
	}) {
		t.Fatalf("saved list = %+v, want %+v", st.custom, want)
	}
	if !rowText(m, "Ctrl+K", "Save", "custom") {
		t.Errorf("the table does not show the new key:\n%s", strings.Join(frameText(m), "\n"))
	}
	if rowText(m, "Ctrl+S", "Save") {
		t.Error("the old key is still listed for Save")
	}
	if !frameHas(m, "config.json") || !strings.Contains(m.notice, "Saved") {
		t.Errorf("no confirmation: notice %q", m.notice)
	}
	if _, ok := m.keys.(fakeBindings); !ok || len(m.keys.(fakeBindings).list) != len(st.keymap()) {
		t.Error("the keymap of the next key press was not replaced")
	}
}

func TestKeybindingEditorWarnsOfAConflict(t *testing.T) {
	m, _ := newKeybindModel(t)
	selectRowOf(t, m, "quit")
	press(m, special(tea.KeyEnter))
	press(m, ctrl('s')) // on the key field Ctrl+S is a key to bind, not Save
	if !frameHas(m, "Conflict: Ctrl+S is bound to save in normal") {
		t.Fatalf("no conflict warning:\n%s", strings.Join(frameText(m), "\n"))
	}
	// A key that nothing holds shows none.
	press(m, ctrl('k'))
	if frameHas(m, "Conflict") {
		t.Error("a free key shows a conflict")
	}
	// A global binding conflicts with the same key in another context.
	press(m, ctrl('p'))
	if !frameHas(m, "Conflict: Ctrl+P is bound to quick_open in global") {
		t.Error("no conflict with the global binding")
	}
}

func TestKeybindingEditorResetsACustomBinding(t *testing.T) {
	m, st := newKeybindModel(t, CustomBinding{Chord: "ctrl+k", Action: "save", Context: "normal"})
	selectRowOf(t, m, "save")
	e := m.overlays[len(m.overlays)-1].(*keybindEditor)
	r, _ := e.selected()
	if r.chord != "ctrl+k" && r.chord != "ctrl+s" {
		t.Fatalf("selected %+v", r)
	}
	// Pick the custom row.
	for k, i := range e.shown {
		if e.rows[i].custom >= 0 {
			e.sel = k
		}
	}
	if !frameHas(m, "d reset") {
		t.Error("the footer does not offer d reset on a custom row")
	}
	press(m, typed("d"))
	if len(st.custom) != 1 {
		t.Fatalf("d wrote the config: %+v", st.custom)
	}
	if !frameHas(m, "Reset Ctrl+K") {
		t.Errorf("no note of the reset:\n%s", strings.Join(frameText(m), "\n"))
	}
	saveAllNow(t, m)
	if len(st.custom) != 0 {
		t.Fatalf("custom list after reset = %+v, want empty", st.custom)
	}
	if !strings.Contains(m.notice, "Saved") {
		t.Errorf("notice = %q", m.notice)
	}
}

func TestKeybindingEditorUnbindsAndDisablesKeymapBindings(t *testing.T) {
	m, st := newKeybindModel(t)
	selectRowOf(t, m, "move_left")
	press(m, typed("d"))
	if rowText(m, "Left", "Move left") {
		t.Error("the unbound row is still listed")
	}
	selectRowOf(t, m, "quit")
	press(m, typed("x"))
	if !rowText(m, "Ctrl+Q", "(disabled)") {
		t.Error("the disabled row is not shown as disabled")
	}
	saveAllNow(t, m)
	if len(st.custom) != 2 || st.custom[0].Action != "unbind" || st.custom[0].Chord != "left" || st.custom[0].Context != "normal" {
		t.Fatalf("after d: %+v, want an unbind of left in normal", st.custom)
	}
	last := st.custom[len(st.custom)-1]
	if last.Action != "noop" || last.Chord != "ctrl+q" {
		t.Fatalf("after x: %+v, want a noop of ctrl+q", last)
	}
}

func TestKeybindingEditorAddsABinding(t *testing.T) {
	m, st := newKeybindModel(t)
	press(m, typed("a"))
	press(m, tea.KeyPressMsg{Code: 'u', Mod: tea.ModAlt}) // the key
	press(m, special(tea.KeyTab))
	for _, r := range "goto_l" {
		press(m, typed(string(r)))
	}
	if !frameHas(m, "goto_line") {
		t.Errorf("the dialog lists no suggestion:\n%s", strings.Join(frameText(m), "\n"))
	}
	press(m, special(tea.KeyTab)) // accepts the suggestion
	press(m, special(tea.KeyTab)) // to the context
	press(m, special(tea.KeyRight))
	press(m, special(tea.KeyEnter)) // applies the dialog
	saveAllNow(t, m)
	want := CustomBinding{Chord: "alt+u", Action: "goto_line", Context: "global"}
	if len(st.custom) != 1 || st.custom[0].Chord != want.Chord || st.custom[0].Action != want.Action || st.custom[0].Context != want.Context {
		t.Fatalf("saved %+v, want %+v", st.custom, want)
	}
}

func TestKeybindingEditorRefusesAnUnknownAction(t *testing.T) {
	m, st := newKeybindModel(t)
	press(m, typed("a"))
	press(m, ctrl('k'))
	press(m, special(tea.KeyTab))
	for _, r := range "no_such_action" {
		press(m, typed(string(r)))
	}
	press(m, ctrl('s'))
	if st.saves != 0 || len(editorOf(m).custom) != 0 {
		t.Fatal("an unknown action was accepted")
	}
	if !frameHas(m, `"no_such_action" is not an action`) {
		t.Errorf("no explanation:\n%s", strings.Join(frameText(m), "\n"))
	}
}

func TestKeybindingEditorShowsASaveError(t *testing.T) {
	m, st := newKeybindModel(t)
	st.err = errors.New("disk full")
	selectRowOf(t, m, "move_left")
	press(m, typed("d"))
	saveAllNow(t, m)
	if !frameHas(m, "Not saved: disk full") {
		t.Errorf("the failure is not shown:\n%s", strings.Join(frameText(m), "\n"))
	}
}

func TestKeybindingEditorEscClosesOneLevelAtATime(t *testing.T) {
	m, _ := newKeybindModel(t)
	press(m, typed("/"))
	press(m, typed("q"))
	if frameHas(m, "Move left") {
		t.Fatal("the filter did not narrow the rows")
	}
	press(m, special(tea.KeyEscape)) // clears the filter
	if len(m.overlays) != 1 || !frameHas(m, "Move left") {
		t.Fatal("Esc did not clear the filter first")
	}
	press(m, special(tea.KeyEnter))
	press(m, special(tea.KeyEscape)) // closes the dialog
	if len(m.overlays) != 1 || m.overlays[0].(*keybindEditor).mode != kbList {
		t.Fatal("Esc did not close the dialog only")
	}
	press(m, special(tea.KeyEscape)) // closes the editor
	if len(m.overlays) != 0 {
		t.Fatal("Esc in the table did not close the editor")
	}
}

func TestKeybindingEditorCanRecordSpecialKeys(t *testing.T) {
	m, _ := newKeybindModel(t)
	press(m, typed("a"))
	press(m, special(tea.KeyEnter)) // Enter on the key field: record any key
	press(m, special(tea.KeyEscape))
	e := m.overlays[len(m.overlays)-1].(*keybindEditor)
	if e.form.chord != "escape" || e.mode != kbEdit {
		t.Fatalf("recorded %q in mode %d, want escape in the dialog", e.form.chord, e.mode)
	}
	press(m, special(tea.KeyEnter))
	press(m, special(tea.KeyTab))
	if e.form.chord != "tab" {
		t.Fatalf("recorded %q, want tab", e.form.chord)
	}
}

func TestKeybindingEditorWithoutAStoreIsReadOnly(t *testing.T) {
	keys := fakeBindings{fakeKeymap: testKeys(), list: editorBase}
	m := New(Deps{Editor: newFakeEditor("hello"), Keys: keys, Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	runAction(t, m, "open_keybinding_editor")
	if !frameHas(m, "Read only") {
		t.Error("no read-only note")
	}
	if frameHas(m, "Enter edit") || frameHas(m, "a add") {
		t.Error("the footer advertises keys that do nothing without a store")
	}
	press(m, special(tea.KeyEnter))
	if m.overlays[len(m.overlays)-1].(*keybindEditor).mode != kbList {
		t.Error("Enter opened a dialog that cannot save")
	}
}

func TestKeybindingEditorWithoutAListerSaysSo(t *testing.T) {
	m := New(Deps{Editor: newFakeEditor("hello"), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	runAction(t, m, "open_keybinding_editor")
	if !frameHas(m, noBindingsText) {
		t.Error("no note that the keymap cannot list its bindings")
	}
	if frameHas(m, "Enter run") {
		t.Error("a list that runs nothing advertises Enter run")
	}
}

// Binding a key that the user unbound earlier leaves one entry for it, not an unbind and a
// binding of the same chord.
func TestKeybindingEditorRebindingAnUnboundKeyKeepsOneEntry(t *testing.T) {
	m, st := newKeybindModel(t, CustomBinding{Chord: "ctrl+k", Action: "unbind", Context: "normal"})
	press(m, typed("a"))
	press(m, ctrl('k'))
	press(m, special(tea.KeyTab))
	for _, r := range "save" {
		press(m, typed(string(r)))
	}
	press(m, special(tea.KeyTab)) // takes the first suggestion
	press(m, ctrl('s'))
	saveAllNow(t, m)
	if len(st.custom) != 1 || st.custom[0].Action != "save" || st.custom[0].Chord != "ctrl+k" {
		t.Fatalf("saved %+v, want only the binding of ctrl+k", st.custom)
	}
}

// A global binding hides the normal one of the same key, so it is a conflict, but the keys of
// the modal contexts are their own.
func TestKeybindingEditorConflictsStayInTheEditorContexts(t *testing.T) {
	st := &fakeStore{base: append(slices.Clone(editorBase), KeyBinding{Action: "prompt_delete_to_line_end", Chord: "ctrl+k", Context: "prompt"})}
	keys := fakeBindings{fakeKeymap: testKeys(), list: st.keymap()}
	m := New(Deps{Editor: newFakeEditor("hello"), Keys: keys, Theme: theme.Theme{}, Clock: testClock,
		Settings: Settings{Keymap: "default"}, Root: RootPorts{Keybindings: st}})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	runAction(t, m, "open_keybinding_editor")
	press(m, typed("a"))
	press(m, ctrl('k'))
	press(m, special(tea.KeyTab))
	press(m, special(tea.KeyTab))
	press(m, special(tea.KeyRight)) // global
	if frameHas(m, "Conflict") {
		t.Errorf("a global key clashes with a prompt key:\n%s", strings.Join(frameText(m), "\n"))
	}
}

// resize gives the model a new terminal size.
func resize(m *Model, w, h int) { m.Update(tea.WindowSizeMsg{Width: w, Height: h}) }

// On a short terminal the dialog is drawn in a compact form, and on one too small for it the
// editor refuses to open it, so no key ever goes to a dialog nobody sees.
func TestKeybindingEditorDialogOnShortTerminals(t *testing.T) {
	m, st := newKeybindModel(t)
	resize(m, 80, 12)
	selectRowOf(t, m, "quick_open")
	press(m, special(tea.KeyEnter))
	if editorOf(m).mode != kbEdit || !frameHas(m, "Edit binding") || !frameHas(m, "Context:") {
		t.Fatalf("no dialog on a 12-row terminal:\n%s", strings.Join(frameText(m), "\n"))
	}
	press(m, special(tea.KeyEscape))

	resize(m, 80, 8)
	frameText(m) // the program paints after every message, which tells the editor its size
	press(m, special(tea.KeyEnter))
	e := editorOf(m)
	if e.mode != kbList || !frameHas(m, "Terminal too small") {
		t.Fatalf("mode %d, the status does not say the terminal is too small:\n%s", e.mode, strings.Join(frameText(m), "\n"))
	}
	press(m, special(tea.KeyTab))
	press(m, special(tea.KeyTab))
	press(m, special(tea.KeyRight))
	press(m, special(tea.KeyEnter))
	if len(e.custom) != 0 || st.saves != 0 {
		t.Errorf("keys changed a binding behind a dialog that is not drawn: %+v", e.custom)
	}

	// Shrinking while the dialog is open closes it.
	resize(m, 120, 30)
	frameText(m)
	press(m, special(tea.KeyEnter))
	if e.mode != kbEdit {
		t.Fatal("the dialog did not open on a large terminal")
	}
	resize(m, 80, 8)
	frameText(m)
	if e.mode != kbList {
		t.Error("the dialog stayed in charge after the terminal became too small to draw it")
	}
}

func TestKeybindingEditorNarrowBoxKeepsColumnsAndTheCloseKey(t *testing.T) {
	for _, w := range []int{60, 70, 80, 120} {
		m, _ := newKeybindModel(t)
		resize(m, w, 22)
		lines := frameText(m)
		var head, foot string
		for _, l := range lines {
			if strings.Contains(l, "Key ") && strings.Contains(l, "Action") {
				head = l
			}
			if strings.Contains(l, "╰") {
				foot = l
			}
		}
		for _, want := range []string{"Context", "Source"} {
			if !strings.Contains(head, want) {
				t.Errorf("width %d: header %q lacks %q", w, head, want)
			}
		}
		if !strings.Contains(foot, "Esc close") {
			t.Errorf("width %d: the footer %q does not advertise the closing key", w, foot)
		}
		if !rowText(m, "Ctrl+S", "save", "normal", "keymap") {
			t.Errorf("width %d: the Save row lacks a column:\n%s", w, strings.Join(lines, "\n"))
		}
	}
}

func TestKeybindingEditorShowsDescriptionAndActionColumns(t *testing.T) {
	m, _ := newKeybindModel(t)
	if !rowText(m, "Ctrl+S", "save", "Save", "normal") {
		t.Errorf("the row lacks the action id or its description:\n%s", strings.Join(frameText(m), "\n"))
	}
}

// Changes are pending until Ctrl+S, and closing with some asks what to do with them.
func TestKeybindingEditorPendingChangesAndClosePrompt(t *testing.T) {
	tests := []struct {
		name      string
		answer    tea.KeyPressMsg
		wantOpen  bool
		wantSaved int // length of the list in the store afterwards
	}{
		{"discard closes without writing", typed("d"), false, 0},
		{"save writes and closes", typed("s"), false, 1},
		{"cancel keeps editing", special(tea.KeyEscape), true, 0},
		{"c cancels too", typed("c"), true, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, st := newKeybindModel(t)
			selectRowOf(t, m, "move_left")
			press(m, typed("d"))
			if st.saves != 0 {
				t.Fatal("d wrote the config")
			}
			press(m, special(tea.KeyEscape))
			if len(m.overlays) != 1 || editorOf(m).mode != kbConfirm || !frameHas(m, "discard") {
				t.Fatalf("Esc with pending changes did not ask:\n%s", strings.Join(frameText(m), "\n"))
			}
			finish(m, press(m, tc.answer))
			if open := len(m.overlays) == 1; open != tc.wantOpen {
				t.Errorf("editor open = %v, want %v", open, tc.wantOpen)
			}
			if len(st.custom) != tc.wantSaved {
				t.Errorf("store holds %+v, want %d entries", st.custom, tc.wantSaved)
			}
		})
	}
}

func TestKeybindingEditorCleanCloseDoesNotAsk(t *testing.T) {
	m, st := newKeybindModel(t)
	selectRowOf(t, m, "move_left")
	press(m, typed("d"))
	saveAllNow(t, m)
	press(m, special(tea.KeyEscape))
	if len(m.overlays) != 0 || st.saves != 1 {
		t.Errorf("overlays %d, saves %d: a saved editor closes at once", len(m.overlays), st.saves)
	}
}

func TestKeybindingEditorPreviewErrorLeavesThePendingListAlone(t *testing.T) {
	m, st := newKeybindModel(t)
	st.previewErr = errors.New("bad list")
	selectRowOf(t, m, "move_left")
	press(m, typed("d"))
	e := editorOf(m)
	if len(e.custom) != 0 || !frameHas(m, "Not applied: bad list") {
		t.Errorf("pending %+v, status %q", e.custom, e.status)
	}
}

func TestKeybindingEditorContextAndSourceFilters(t *testing.T) {
	m, _ := newKeybindModel(t, CustomBinding{Chord: "ctrl+k", Action: "save", Context: "normal"})
	e := editorOf(m)
	count := func() int { return len(e.shown) }
	all := count()
	press(m, typed("c")) // global first, as in Fresh's order
	if e.ctxFilter != "global" || count() != 1 || !rowText(m, "Ctrl+P") || !frameHas(m, "Context: global") {
		t.Fatalf("context filter %q shows %d rows", e.ctxFilter, count())
	}
	press(m, typed("c"))
	if e.ctxFilter != "normal" || count() != all-1 {
		t.Fatalf("context filter %q shows %d rows", e.ctxFilter, count())
	}
	press(m, typed("c"))
	if e.ctxFilter != "" || count() != all {
		t.Fatalf("the context filter did not return to All: %q, %d rows", e.ctxFilter, count())
	}
	press(m, typed("s"))
	if e.srcFilter != "custom" || count() != 1 || !rowText(m, "Ctrl+K") || !frameHas(m, "Source: custom") {
		t.Fatalf("source filter %q shows %d rows", e.srcFilter, count())
	}
	press(m, typed("s"))
	if e.srcFilter != "keymap" || count() != all-1 {
		t.Fatalf("source filter %q shows %d rows", e.srcFilter, count())
	}
	press(m, typed("s"))
	if e.srcFilter != "" || count() != all {
		t.Fatalf("the source filter did not return to All")
	}
}

func TestKeybindingEditorKeyRecordingSearch(t *testing.T) {
	m, _ := newKeybindModel(t)
	e := editorOf(m)
	press(m, typed("r"))
	if !e.searching || !e.searchKey || !frameHas(m, "press the key to find") {
		t.Fatal("r did not start the key search")
	}
	press(m, typed("q")) // a plain letter is the key searched for, not text
	if e.keyFilter != "q" || e.searching || len(e.shown) != 0 {
		t.Fatalf("key filter %q, searching %v, %d rows", e.keyFilter, e.searching, len(e.shown))
	}
	press(m, special(tea.KeyEscape)) // clears the search
	if e.keyFilter != "" || len(e.shown) != len(e.rows) || len(m.overlays) != 1 {
		t.Fatal("Esc did not clear the key search")
	}
	press(m, typed("r"))
	press(m, ctrl('q'))
	if len(e.shown) != 1 || e.rows[e.shown[0]].action != "quit" || !frameHas(m, "Key: Ctrl+Q") {
		t.Fatalf("searching for Ctrl+Q shows %d rows", len(e.shown))
	}
	// Tab switches between the search by key and the search by text.
	press(m, special(tea.KeyEscape))
	press(m, typed("r"))
	press(m, special(tea.KeyTab))
	if e.searchKey || !e.searching {
		t.Fatal("Tab did not switch to the text search")
	}
	press(m, special(tea.KeyTab))
	if !e.searchKey {
		t.Fatal("Tab did not switch back to the key search")
	}
}

// TestEditorFooterKeepsSaveAtSmallWidths: the footer always names Ctrl+S, the way to save, at
// the sizes a narrow terminal gives. A footer that drops it hides the one key that writes changes.
func TestEditorFooterKeepsSaveAtSmallWidths(t *testing.T) {
	for _, sz := range [][2]int{{60, 20}, {50, 14}, {44, 12}} {
		m, _ := newKeybindModel(t)
		resize(m, sz[0], sz[1])
		fr := frameText(m)
		if footer := fr[len(fr)-2]; !strings.Contains(footer, "Ctrl+S save") {
			t.Errorf("%dx%d: the footer %q does not name Ctrl+S save", sz[0], sz[1], footer)
		}
	}
}

// TestEditorDialogStaysInsideTheTable: the Add binding dialog never paints over the table's
// border. At 50x14 it used to take the top border row, so the title read "╭─╭─ Add binding".
func TestEditorDialogStaysInsideTheTable(t *testing.T) {
	for _, sz := range [][2]int{{60, 20}, {50, 14}} {
		m, _ := newKeybindModel(t)
		resize(m, sz[0], sz[1])
		press(m, typed("a"))
		var dialog string
		for _, l := range frameText(m) {
			if strings.Contains(l, "Add binding") {
				dialog = l
			}
		}
		if dialog == "" {
			t.Fatalf("%dx%d: the dialog is not drawn:\n%s", sz[0], sz[1], strings.Join(frameText(m), "\n"))
		}
		if strings.Contains(dialog, "╭─╭─") || !strings.Contains(dialog, "│ ╭─ Add binding") {
			t.Errorf("%dx%d: the dialog title %q is over the table border", sz[0], sz[1], dialog)
		}
	}
}

// TestEditorListsOnlyContextsGvimResolves: a binding in a context gvim never resolves (a prompt
// or a popup answers its keys in code) is not a row, so the editor does not offer a key that
// does nothing. The rows are the ones the root can run.
func TestEditorListsOnlyContextsGvimResolves(t *testing.T) {
	base := append(slices.Clone(editorBase),
		KeyBinding{Action: "prompt_move_start", Chord: "ctrl+a", Context: "prompt"},
		KeyBinding{Action: "toggle_search_case_sensitive", Chord: "alt+z", Context: "searchPrompt"},
		KeyBinding{Action: "move_up", Chord: "up", Context: "file_explorer"})
	st := &fakeStore{base: base}
	keys := fakeBindings{fakeKeymap: testKeys(), list: st.keymap()}
	m := New(Deps{Editor: newFakeEditor("hello"), Keys: keys, Theme: theme.Theme{}, Clock: testClock,
		Settings: Settings{Keymap: "default", ConfigFile: "/home/u/.config/gvim/config.json"},
		Root:     RootPorts{Keybindings: st}})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	runAction(t, m, "open_keybinding_editor")
	e := m.overlays[len(m.overlays)-1].(*keybindEditor)
	for _, r := range e.rows {
		if !keymap.Consulted(keymap.Context(r.context)) {
			t.Errorf("the editor lists %s in %s, a context gvim does not resolve", r.action, r.context)
		}
	}
	if len(e.rows) != 5 {
		t.Errorf("rows = %d, want the four editorBase rows and the file-explorer row", len(e.rows))
	}
}

// layeringStore is a store that also previews over the plugin layer. It counts the previews
// that take the layer.
type layeringStore struct {
	*fakeStore
	layered int
}

func (s *layeringStore) PreviewWithPlugins(list []CustomBinding, preset string, _ []pluginapi.Binding) (Keymap, error) {
	s.layered++
	return s.Preview(list, preset)
}

// TestEditorPreviewsOverThePluginLayer: a pending change is previewed through the store's plugin
// layer when it has one, so the plugins' rows stay in the table while the list changes.
func TestEditorPreviewsOverThePluginLayer(t *testing.T) {
	st := &layeringStore{fakeStore: &fakeStore{base: editorBase}}
	keys := fakeBindings{fakeKeymap: testKeys(), list: st.keymap()}
	m := New(Deps{Editor: newFakeEditor("hello"), Keys: keys, Theme: theme.Theme{}, Clock: testClock,
		Settings: Settings{Keymap: "default", ConfigFile: "/home/u/.config/gvim/config.json"},
		Root:     RootPorts{Keybindings: st}})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	runAction(t, m, "open_keybinding_editor")
	press(m, typed("d"))
	if st.layered == 0 {
		t.Fatal("the pending change was previewed without the plugin layer")
	}
}
