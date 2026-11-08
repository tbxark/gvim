package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/tui/find"
)

// multiWordFake is a wordFake whose selection text may span lines.
type multiWordFake struct{ wordFake }

func (w multiWordFake) SelectionOrWord() (string, buffer.Pos, buffer.Pos, bool, error) {
	if !w.sel {
		return w.wordFake.SelectionOrWord()
	}
	from, to := w.anchor, w.doc.cursor
	if to.Less(from) {
		from, to = to, from
	}
	var parts []string
	for l := from.Line; l <= to.Line; l++ {
		r := []rune(w.doc.lines[l])
		s, e := 0, len(r)
		if l == from.Line {
			s = from.Col
		}
		if l == to.Line {
			e = to.Col
		}
		parts = append(parts, string(r[s:e]))
	}
	return strings.Join(parts, "\n"), from, to, true, nil
}

func TestFindSelectionSearchesAMultiLineSelection(t *testing.T) {
	t.Parallel()
	sf := &searchFake{fileEditorFake: &fileEditorFake{fakeEditor: newFakeEditor("ab", "cd x ab", "cd")}}
	m := newRouteModelPreset(t, multiWordFake{wordFake{sf}}, "default")
	sf.SetCursor(buffer.Pos{}, false)
	sf.SetCursor(buffer.Pos{Line: 1, Col: 2}, true)
	if err := m.Run("find_selection_next"); err != nil {
		t.Fatal(err)
	}
	if !sf.sel || sf.anchor != (buffer.Pos{Line: 1, Col: 5}) {
		t.Fatalf("selection from %v (selected %v), want from 1:5; notice %q", sf.anchor, sf.sel, m.notice)
	}
	if got := m.findBar.Query().Text; got != "ab\ncd" {
		t.Fatalf("search text = %q", got)
	}
}

func TestEscAfterF3ThenEditPutsTheCaretBack(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		key  tea.KeyPressMsg
	}{
		{"typing", typed("x")},
		{"option toggle", tea.KeyPressMsg{Code: 'c', Mod: tea.ModAlt}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, sf := newDefaultSearchModel(t, "foo bar foo")
			sf.doc.cursor = buffer.Pos{Line: 0, Col: 4}
			openBarWith(m, "foo")
			press(m, special(tea.KeyF3))
			press(m, tt.key)
			press(m, special(tea.KeyEscape))
			if sf.sel || sf.doc.cursor != (buffer.Pos{Line: 0, Col: 4}) {
				t.Fatalf("caret %v selected %v after Esc", sf.doc.cursor, sf.sel)
			}
		})
	}
}

func TestFindSelectionContinuesOnlyFromAMatchStart(t *testing.T) {
	t.Parallel()
	m, sf := newWordModel(t, "foobar foo")
	sf.doc.cursor = buffer.Pos{Line: 0, Col: 8}
	if err := m.Run("find_selection_next"); err != nil {
		t.Fatal(err)
	}
	if got := m.findBar.Query().Text; got != "foo" {
		t.Fatalf("search text = %q, want foo", got)
	}
	sf.sel = false
	sf.doc.cursor = buffer.Pos{Line: 0, Col: 2}
	if err := m.Run("find_selection_next"); err != nil {
		t.Fatal(err)
	}
	if got := m.findBar.Query().Text; got != "foobar" {
		t.Fatalf("search text = %q, want the word foobar", got)
	}
}

func TestSearchStateSeedsTheFirstBarAndReportsTheLastOne(t *testing.T) {
	t.Parallel()
	m, _ := newDefaultSearchModel(t, "Foo foo")
	if _, ok := m.SearchState(); ok {
		t.Fatal("a model that never searched reports a search state")
	}
	m.SetSearchState(find.State{Find: []string{"old", "foo"}, Case: true})
	if st, ok := m.SearchState(); !ok || st.Find[1] != "foo" || !st.Case {
		t.Fatalf("SearchState before any bar = %+v (%v), want the seed", st, ok)
	}
	press(m, ctrl('f'))
	if got := m.findBar.Query(); got.Text != "foo" || !got.CaseSensitive {
		t.Fatalf("first bar holds %+v, want the seeded last search with Case on", got)
	}
	press(m, typed("x"))
	press(m, special(tea.KeyEnter))
	st, ok := m.SearchState()
	if !ok || len(st.Find) != 3 || st.Find[2] != "x" || !st.Case {
		t.Fatalf("SearchState after a search = %+v (%v)", st, ok)
	}
}
