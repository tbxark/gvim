package tui

import (
	"errors"
	"strings"
	"testing"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/keymap"
	"github.com/tbxark/gvim/theme"
)

// wordFake is a searchFake that also gives find_selection_next its text: the
// selection, or the word at the caret.
type wordFake struct{ *searchFake }

func (w wordFake) SelectionOrWord() (string, buffer.Pos, buffer.Pos, bool, error) {
	c := w.doc.cursor
	if w.sel {
		from, to := w.anchor, c
		if to.Less(from) {
			from, to = to, from
		}
		return w.doc.lines[from.Line][from.Col:to.Col], from, to, true, nil
	}
	line := []rune(w.doc.lines[c.Line])
	from, to := c.Col, c.Col
	for from > 0 && isWordRune(line[from-1]) {
		from--
	}
	for to < len(line) && isWordRune(line[to]) {
		to++
	}
	if from == to {
		return "", buffer.Pos{}, buffer.Pos{}, false, nil
	}
	return string(line[from:to]), buffer.Pos{Line: c.Line, Col: from}, buffer.Pos{Line: c.Line, Col: to}, true, nil
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// newWordModel returns a default-keymap model over a wordFake of lines.
func newWordModel(t *testing.T, lines ...string) (*Model, *searchFake) {
	t.Helper()
	sf := &searchFake{fileEditorFake: &fileEditorFake{fakeEditor: newFakeEditor(lines...)}}
	return newRouteModelPreset(t, wordFake{sf}, "default"), sf
}

func TestEscAfterTypingLeavesTheCaretAlone(t *testing.T) {
	t.Parallel()
	m, sf := newDefaultSearchModel(t, "foo bar foo")
	sf.doc.cursor = buffer.Pos{Line: 0, Col: 4}
	openBarWith(m, "foo")
	press(m, special(tea.KeyEscape))
	if sf.sel || sf.doc.cursor != (buffer.Pos{Line: 0, Col: 4}) {
		t.Fatalf("caret %v selected %v after typing and Esc", sf.doc.cursor, sf.sel)
	}
	if got := m.findLineHighlights()(0); got != nil {
		t.Fatalf("Esc kept the highlights %v", got)
	}
}

func TestEscAfterF3PutsTheCaretBack(t *testing.T) {
	t.Parallel()
	m, sf := newDefaultSearchModel(t, "foo bar foo")
	sf.doc.cursor = buffer.Pos{Line: 0, Col: 4}
	openBarWith(m, "foo")
	press(m, special(tea.KeyF3))
	if !sf.sel {
		t.Fatal("F3 selected nothing")
	}
	press(m, special(tea.KeyEscape))
	if sf.sel || sf.doc.cursor != (buffer.Pos{Line: 0, Col: 4}) {
		t.Fatalf("caret %v selected %v after Esc", sf.doc.cursor, sf.sel)
	}
}

func TestEnterRevealsTheMatchOffScreen(t *testing.T) {
	t.Parallel()
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = "line"
	}
	lines[90] = "needle"
	m, sf := newDefaultSearchModel(t, lines...)
	openBarWith(m, "needle")
	press(m, special(tea.KeyEnter))
	if m.findOpen() {
		t.Fatal("bar still open")
	}
	if top := sf.doc.top; top == 0 || sf.doc.cursor.Line != 90 || top > 90 {
		t.Fatalf("top = %d, caret line %d: the match is not on screen", top, sf.doc.cursor.Line)
	}
}

func TestSearchDefaultsComeFromTheConfig(t *testing.T) {
	t.Parallel()
	km, err := keymap.ByName("default")
	if err != nil {
		t.Fatal(err)
	}
	view := DefaultViewOptions()
	view.Search = SearchOptions{CaseSensitive: true, Regex: true, ConfirmEach: true}
	sf := &searchFake{fileEditorFake: &fileEditorFake{fakeEditor: newFakeEditor("a")}}
	m := New(Deps{Editor: sf, Keys: presetKeymap{km: km}, Theme: theme.Theme{}, Clock: testClock, View: &view})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	press(m, ctrl('f'))
	rows := screenRows(m)
	opts := rows[len(rows)-2]
	for _, want := range []string{"[x] Case Sensitive", "[ ] Whole Word", "[x] Regex", "[x] Confirm Each"} {
		if !strings.Contains(opts, want) {
			t.Errorf("options row %q lacks %q", opts, want)
		}
	}
}

func TestReopeningKeepsTogglesAndPrefillsTheLastSearch(t *testing.T) {
	t.Parallel()
	m, sf := newDefaultSearchModel(t, "Foo foo")
	press(m, ctrl('f'))
	press(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModAlt})
	for _, r := range "foo" {
		press(m, typed(string(r)))
	}
	press(m, special(tea.KeyEnter))
	sf.doc.cursor = buffer.Pos{}
	sf.sel = false

	press(m, ctrl('f'))
	if got := m.findBar.Query(); got.Text != "foo" || !got.CaseSensitive {
		t.Fatalf("reopened bar holds %+v, want foo with case sensitivity kept", got)
	}
	press(m, typed("b")) // the pre-filled text is selected
	if got := m.findBar.Query().Text; got != "b" {
		t.Fatalf("typing over the prefill gave %q", got)
	}
	press(m, special(tea.KeyUp))
	if got := m.findBar.Query().Text; got != "foo" {
		t.Fatalf("Up gave %q, want the history entry foo", got)
	}
}

func TestCtrlFPrefillsFromTheSelection(t *testing.T) {
	t.Parallel()
	m, sf := newDefaultSearchModel(t, "hello world")
	sf.SetCursor(buffer.Pos{Line: 0, Col: 6}, false)
	sf.SetCursor(buffer.Pos{Line: 0, Col: 11}, true)
	press(m, ctrl('f'))
	if got := m.findBar.Query().Text; got != "world" {
		t.Fatalf("search text = %q, want the selection", got)
	}
}

func TestFindNextWalksZeroWidthMatches(t *testing.T) {
	t.Parallel()
	m, sf := newDefaultSearchModel(t, "a", "b", "c")
	press(m, ctrl('f'))
	press(m, tea.KeyPressMsg{Code: 'r', Mod: tea.ModAlt})
	press(m, typed("^"))
	press(m, special(tea.KeyEnter)) // the empty match at 0:0
	var lines []int
	for range 3 {
		press(m, special(tea.KeyF3))
		lines = append(lines, sf.doc.cursor.Line)
	}
	if lines[0] != 1 || lines[1] != 2 || lines[2] != 0 {
		t.Fatalf("F3 visited lines %v, want 1 2 0", lines)
	}
}

func TestFindSelectionStartsASearchWithTheBarFlags(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		caseSens  bool
		forward   bool
		wantStart buffer.Pos
		wantNote  string
	}{
		{name: "case-insensitive by default", forward: true, wantStart: buffer.Pos{Line: 0, Col: 5}, wantNote: "Match 2 of 3"},
		{name: "case-sensitive skips Todo", caseSens: true, forward: true, wantStart: buffer.Pos{Line: 1, Col: 0}, wantNote: "Match 2 of 2"},
		{name: "previous wraps to the last", forward: false, wantStart: buffer.Pos{Line: 1, Col: 0}, wantNote: "Match 3 of 3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, sf := newWordModel(t, "todo Todo x", "todo")
			if tt.caseSens {
				press(m, ctrl('f'))
				press(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModAlt})
				press(m, special(tea.KeyEscape))
			}
			sf.doc.cursor = buffer.Pos{Line: 0, Col: 2} // inside the first "todo"
			if err := m.Run(map[bool]string{true: "find_selection_next", false: "find_selection_previous"}[tt.forward]); err != nil {
				t.Fatal(err)
			}
			if !sf.sel || sf.anchor != tt.wantStart {
				t.Fatalf("selection from %v (selected %v), want %v", sf.anchor, sf.sel, tt.wantStart)
			}
			if m.notice != tt.wantNote {
				t.Fatalf("notice = %q, want %q", m.notice, tt.wantNote)
			}
			if got := m.findBar.Query().Text; got != "todo" {
				t.Fatalf("search text = %q", got)
			}
			if !m.findShown() || m.findLineHighlights()(0) == nil {
				t.Fatal("the matches are not highlighted")
			}
		})
	}
}

func TestFindSelectionThenF3ContinuesTheSearch(t *testing.T) {
	t.Parallel()
	m, sf := newWordModel(t, "ab x ab", "ab")
	sf.doc.cursor = buffer.Pos{Line: 0, Col: 1}
	if err := m.Run("find_selection_next"); err != nil {
		t.Fatal(err)
	}
	if sf.anchor != (buffer.Pos{Line: 0, Col: 5}) {
		t.Fatalf("first press selected from %v, want 0:5", sf.anchor)
	}
	// The selection is now a match of the held search, so a second press continues it.
	if err := m.Run("find_selection_next"); err != nil {
		t.Fatal(err)
	}
	if sf.anchor != (buffer.Pos{Line: 1, Col: 0}) {
		t.Fatalf("second press selected from %v, want 1:0", sf.anchor)
	}
	press(m, special(tea.KeyF3))
	if sf.anchor != (buffer.Pos{Line: 0, Col: 0}) {
		t.Fatalf("F3 selected from %v, want 0:0", sf.anchor)
	}
}

func TestFindSelectionWithoutTextSaysSo(t *testing.T) {
	t.Parallel()
	m, sf := newWordModel(t, " ; ")
	sf.doc.cursor = buffer.Pos{Line: 0, Col: 1}
	if err := m.Run("find_selection_next"); err != nil {
		t.Fatal(err)
	}
	if m.notice != noTextToSearch {
		t.Fatalf("notice = %q", m.notice)
	}
}

// refusingFake is a wordFake whose selection cannot be the search text.
type refusingFake struct {
	wordFake
	err error
}

func (r refusingFake) SelectionOrWord() (string, buffer.Pos, buffer.Pos, bool, error) {
	return "", buffer.Pos{}, buffer.Pos{}, false, r.err
}

// TestFindSelectionSaysWhyItRefusesASelection is the regression test for a refused
// selection being reported as "No text to search".
func TestFindSelectionSaysWhyItRefusesASelection(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"find_selection_next", "find_selection_previous"} {
		sf := &searchFake{fileEditorFake: &fileEditorFake{fakeEditor: newFakeEditor("foo bar")}}
		why := errors.New("selection too large to search for in large file mode (over 64 KiB)")
		m := newRouteModelPreset(t, refusingFake{wordFake: wordFake{sf}, err: why}, "default")
		if err := m.Run(action); err != nil {
			t.Fatal(err)
		}
		if m.notice != why.Error() {
			t.Errorf("%s: notice = %q, want %q", action, m.notice, why)
		}
		if m.findBar != nil {
			t.Errorf("%s: a refused selection opened a search", action)
		}
	}
}

func TestReplaceActionTypesIntoTheSearchRowFirst(t *testing.T) {
	t.Parallel()
	m, sf := newDefaultSearchModel(t, "x x")
	if err := m.Run("replace"); err != nil {
		t.Fatal(err)
	}
	press(m, typed("x"))
	press(m, special(tea.KeyEnter)) // to the replacement row
	press(m, typed("y"))
	press(m, special(tea.KeyEnter)) // replaces all
	if got := sf.text(); got != "y y" {
		t.Fatalf("text = %q, want the search typed first and the replacement second", got)
	}
}

func TestQueryReplaceFromAFreshBar(t *testing.T) {
	t.Parallel()
	m, sf := newDefaultSearchModel(t, "x x")
	doAction(m, "query_replace")
	press(m, typed("x"))
	press(m, special(tea.KeyEnter)) // to the replacement row
	press(m, typed("y"))
	press(m, special(tea.KeyEnter)) // asks about each match
	press(m, typed("!"))
	if got := sf.text(); got != "y y" {
		t.Fatalf("text = %q", got)
	}
}
