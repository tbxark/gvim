package tui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/list"
	"github.com/tbxark/gvim/theme"
)

// fakeBindings is a Keymap that also lists its bindings, as the app adapter does.
// primary holds the binding Primary returns for each action.
type fakeBindings struct {
	fakeKeymap
	list    []KeyBinding
	primary map[string]KeyBinding
}

func (k fakeBindings) Bindings() []KeyBinding { return k.list }

func (k fakeBindings) Primary(action string) (KeyBinding, bool) {
	b, ok := k.primary[action]
	return b, ok
}

// fileFake is a fakeEditor that also opens files, as the app adapter does. It
// records the paths it was asked to open.
type fileFake struct {
	*fakeEditor
	opened []string
}

func (f *fileFake) OpenFile(path string) error {
	f.opened = append(f.opened, path)
	return nil
}

func (f *fileFake) OpenPreview(string) error { return nil }
func (f *fileFake) GotoLine(int, int) error  { return nil }

// newUtilityModel returns a 80 by 24 model over ed with the keymap keys, the
// settings s and the keymap switch sw. A nil sw leaves switching off.
func newUtilityModel(t *testing.T, ed EditorState, keys Keymap, s Settings, sw func(string) (Keymap, error)) *Model {
	t.Helper()
	m := New(Deps{Editor: ed, Keys: keys, Theme: theme.Theme{}, Clock: testClock, Settings: s, SwitchKeymap: sw})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return m
}

// frameText returns the rows of the frame as plain text.
func frameText(m *Model) []string {
	return m.paint().Plain()
}

// frameHas reports whether some row of the frame contains s.
func frameHas(m *Model, s string) bool {
	for _, row := range frameText(m) {
		if strings.Contains(row, s) {
			return true
		}
	}
	return false
}

// runAction runs action on m and fails the test when the action is not handled.
func runAction(t *testing.T, m *Model, action string) {
	t.Helper()
	if err := m.Run(action); err != nil {
		t.Fatalf("Run(%s) = %v, want nil", action, err)
	}
}

// frameRowHas reports whether one row of the frame contains every part.
func frameRowHas(m *Model, parts ...string) bool {
	for _, row := range frameText(m) {
		ok := true
		for _, p := range parts {
			ok = ok && strings.Contains(row, p)
		}
		if ok {
			return true
		}
	}
	return false
}

// rowsOf renders list rows as "# header" for a group and "left=right" for an item.
func rowsOf(rows []list.Row) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		if r.Header {
			out[i] = "# " + r.Left
			continue
		}
		out[i] = r.Left + "=" + r.Right
	}
	return out
}

// sameLines reports whether got and want are the same lists, with a readable failure.
func sameLines(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("%s:\n got: %q\nwant: %q", what, got, want)
	}
}

// errSwitch is the error a keymap switch returns in the failure tests.
var errSwitch = errors.New("no such keymap")

// actionNames returns the names of the actions the fake editor ran, in order.
func (f *fakeEditor) actionNames() []string {
	out := make([]string, len(f.actions))
	for i, a := range f.actions {
		out[i] = string(a)
	}
	return out
}
