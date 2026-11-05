package find

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/search"
	"github.com/tbxark/gvim/internal/theme"
	"github.com/tbxark/gvim/internal/ui/grid"
	"github.com/tbxark/gvim/internal/ui/style"
)

// fakeEditor is an in-memory Editor over a real buffer, so the search results
// come from the search package.
type fakeEditor struct {
	buf    *buffer.Buffer
	cursor buffer.Pos
	sel    *search.Match
}

func newFake(text string) *fakeEditor {
	return &fakeEditor{buf: buffer.New(text)}
}

func (f *fakeEditor) Cursor() buffer.Pos { return f.cursor }

func (f *fakeEditor) Selection() (start, end buffer.Pos, ok bool) {
	if f.sel == nil {
		return f.cursor, f.cursor, false
	}
	return f.sel.Start, f.sel.End, true
}

func (f *fakeEditor) Find(q search.Query) ([]search.Match, error) {
	return search.Find(f.buf, q)
}

func (f *fakeEditor) Select(m search.Match) {
	f.sel = &m
	f.cursor = m.End
}

func (f *fakeEditor) Place(p buffer.Pos) {
	f.sel = nil
	f.cursor = f.buf.Clamp(p)
}

func (f *fakeEditor) Replace(q search.Query, repl string, m search.Match) (string, error) {
	return search.Replace(f.buf, q, repl, m)
}

func (f *fakeEditor) ReplaceAll(q search.Query, repl string) (int, error) {
	return search.ReplaceAll(f.buf, q, repl)
}

// text returns the document text.
func (f *fakeEditor) text() string { return f.buf.Text() }

func testStyles() style.Styles { return style.New(theme.Theme{}) }

// key returns a key press for a keystroke name such as "enter", "alt+c" or
// "shift+f3", or for a single character typed as text.
func key(name string) tea.KeyPressMsg {
	switch name {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "alt+enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "f3":
		return tea.KeyPressMsg{Code: tea.KeyF3}
	case "shift+f3":
		return tea.KeyPressMsg{Code: tea.KeyF3, Mod: tea.ModShift}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "alt+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModAlt}
	case "alt+w":
		return tea.KeyPressMsg{Code: 'w', Mod: tea.ModAlt}
	case "alt+r":
		return tea.KeyPressMsg{Code: 'r', Mod: tea.ModAlt}
	}
	r := []rune(name)[0]
	return tea.KeyPressMsg{Code: r, Text: name}
}

// typeText sends each character of s as a key press.
func typeText(b *Bar, s string) {
	for _, r := range s {
		b.Update(key(string(r)))
	}
}

// press sends one key press, by keystroke name, to the bar.
func press(b *Bar, name string) {
	b.Update(key(name))
}

// frame draws the bar into a w by h grid and returns its plain rows.
func frame(b *Bar, w, h int) []string {
	g := grid.New(w, h)
	b.Draw(g)
	return g.Plain()
}

func TestTypingSelectsFirstMatchAfterAnchor(t *testing.T) {
	t.Parallel()
	f := newFake("foo bar foo\nfoo")
	f.cursor = buffer.Pos{Line: 0, Col: 4}
	b := Open(f, testStyles(), false)
	typeText(b, "foo")

	want := search.Match{Start: buffer.Pos{Line: 0, Col: 8}, End: buffer.Pos{Line: 0, Col: 11}}
	if f.sel == nil || *f.sel != want {
		t.Fatalf("selection = %v, want %v", f.sel, want)
	}
	if got := b.statusText(); got != "Match 2 of 3" {
		t.Fatalf("status = %q, want %q", got, "Match 2 of 3")
	}
}

func TestEnterAndF3StepAndWrap(t *testing.T) {
	t.Parallel()
	f := newFake("foo bar foo\nfoo")
	f.cursor = buffer.Pos{Line: 0, Col: 4}
	b := Open(f, testStyles(), false)
	typeText(b, "foo")

	tests := []struct {
		key    string
		status string
		start  buffer.Pos
	}{
		{"enter", "Match 3 of 3", buffer.Pos{Line: 1, Col: 0}},
		{"enter", "Match 1 of 3", buffer.Pos{Line: 0, Col: 0}},
		{"shift+f3", "Match 3 of 3", buffer.Pos{Line: 1, Col: 0}},
		{"f3", "Match 1 of 3", buffer.Pos{Line: 0, Col: 0}},
	}
	for _, tt := range tests {
		press(b, tt.key)
		if got := b.statusText(); got != tt.status {
			t.Fatalf("after %s: status = %q, want %q", tt.key, got, tt.status)
		}
		if f.sel == nil || f.sel.Start != tt.start {
			t.Fatalf("after %s: selection = %v, want start %v", tt.key, f.sel, tt.start)
		}
	}
}

func TestToggleCaseRefindsAndReports(t *testing.T) {
	t.Parallel()
	f := newFake("Foo foo")
	b := Open(f, testStyles(), false)
	typeText(b, "foo")
	if got := b.statusText(); got != "Match 1 of 2" {
		t.Fatalf("before toggle: status = %q", got)
	}

	press(b, "alt+c")
	if got := b.statusText(); got != "Case-sensitive search enabled" {
		t.Fatalf("notice = %q", got)
	}
	if !b.opts[optCase] {
		t.Fatal("case option not set")
	}
	press(b, "backspace")
	typeText(b, "o")
	if got := b.statusText(); got != "Match 1 of 1" {
		t.Fatalf("after case toggle and edit: status = %q, want %q", got, "Match 1 of 1")
	}
	rows := frame(b, 60, 6)
	if !strings.Contains(rows[4], "[x] Case Sensitive (⌥C)") {
		t.Fatalf("options row %q does not show the checked case option", rows[4])
	}
	if !strings.Contains(rows[4], "[ ] Whole Word (⌥W)") {
		t.Fatalf("options row %q does not show the unchecked whole-word option", rows[4])
	}
}

func TestWholeWordOption(t *testing.T) {
	t.Parallel()
	f := newFake("cat concat cat")
	b := Open(f, testStyles(), false)
	press(b, "alt+w")
	typeText(b, "cat")
	if got := b.statusText(); got != "Match 1 of 2" {
		t.Fatalf("status = %q, want %q", got, "Match 1 of 2")
	}
}

func TestInvalidRegexShowsError(t *testing.T) {
	t.Parallel()
	f := newFake("a(b")
	b := Open(f, testStyles(), false)
	press(b, "alt+r")
	typeText(b, "(")
	if !strings.Contains(b.statusText(), "invalid pattern") {
		t.Fatalf("status = %q, want the pattern error", b.statusText())
	}
	if f.sel != nil {
		t.Fatalf("an invalid pattern selected %v", f.sel)
	}
}

func TestReplaceCurrentReplacesSelectionAndAdvances(t *testing.T) {
	t.Parallel()
	f := newFake("a a a")
	b := Open(f, testStyles(), false)
	typeText(b, "a")
	press(b, "tab")
	typeText(b, "b")
	press(b, "enter")

	if got := f.text(); got != "b a a" {
		t.Fatalf("text = %q, want %q", got, "b a a")
	}
	if got := b.statusText(); got != "Match 1 of 2" {
		t.Fatalf("status = %q, want %q", got, "Match 1 of 2")
	}
	if f.sel == nil || f.sel.Start != (buffer.Pos{Line: 0, Col: 2}) {
		t.Fatalf("selection = %v, want the match at column 2", f.sel)
	}
}

func TestEnterInReplaceWithoutMatchChangesNothing(t *testing.T) {
	t.Parallel()
	f := newFake("a b")
	b := Open(f, testStyles(), false)
	typeText(b, "a")
	typeText(b, "x") // the query "ax" has no match; the "a" selection stays
	press(b, "tab")
	typeText(b, "Q")
	press(b, "enter")

	if got := f.text(); got != "a b" {
		t.Fatalf("text = %q, want no change", got)
	}
	if got := b.statusText(); got != search.StatusText(0, 0) {
		t.Fatalf("status = %q, want %q", got, search.StatusText(0, 0))
	}
}

func TestReplaceAllReplacesEveryMatch(t *testing.T) {
	t.Parallel()
	f := newFake("a a a")
	b := Open(f, testStyles(), false)
	typeText(b, "a")
	press(b, "tab")
	typeText(b, "bb")
	press(b, "alt+enter")

	if got := f.text(); got != "bb bb bb" {
		t.Fatalf("text = %q, want %q", got, "bb bb bb")
	}
	if got := b.statusText(); got != "Replaced 3" {
		t.Fatalf("status = %q, want %q", got, "Replaced 3")
	}
	if f.sel != nil {
		t.Fatalf("selection %v left after replace all", f.sel)
	}
}

func TestQueryReplaceAnswers(t *testing.T) {
	t.Parallel()
	f := newFake("x x x x")
	b := Open(f, testStyles(), false)
	typeText(b, "x")
	press(b, "tab")
	typeText(b, "y")
	b.StartQueryReplace()
	if got := b.statusText(); got != queryPrompt {
		t.Fatalf("status = %q, want the question", got)
	}

	typeText(b, "y") // replace the first x
	if got := f.text(); got != "y x x x" {
		t.Fatalf("after y: text = %q", got)
	}
	typeText(b, "n") // skip the second x
	if got := f.text(); got != "y x x x" {
		t.Fatalf("after n: text = %q", got)
	}
	typeText(b, "!") // replace the rest
	if got := f.text(); got != "y x y y" {
		t.Fatalf("after !: text = %q, want %q", got, "y x y y")
	}
	if got := b.statusText(); got != "Replaced 3" {
		t.Fatalf("status = %q, want %q", got, "Replaced 3")
	}
	if b.qr != nil {
		t.Fatal("query-replace still running after !")
	}
}

func TestQueryReplaceQuitKeepsEarlierReplacements(t *testing.T) {
	t.Parallel()
	f := newFake("x x x")
	b := Open(f, testStyles(), false)
	typeText(b, "x")
	press(b, "tab")
	typeText(b, "z")
	b.StartQueryReplace()
	typeText(b, "y")
	typeText(b, "q")

	if got := f.text(); got != "z x x" {
		t.Fatalf("text = %q, want %q", got, "z x x")
	}
	if got := b.statusText(); got != "Replaced 1" {
		t.Fatalf("status = %q, want %q", got, "Replaced 1")
	}
}

func TestQueryReplaceIgnoresOtherKeys(t *testing.T) {
	t.Parallel()
	f := newFake("x")
	b := Open(f, testStyles(), false)
	typeText(b, "x")
	b.StartQueryReplace()
	typeText(b, "k")
	if got := f.text(); got != "x" {
		t.Fatalf("text = %q, want no change", got)
	}
	if b.qr == nil {
		t.Fatal("an unknown key ended query-replace")
	}
}

func TestStartQueryReplaceWithoutSearchText(t *testing.T) {
	t.Parallel()
	b := Open(newFake("x"), testStyles(), false)
	b.StartQueryReplace()
	if b.qr != nil {
		t.Fatal("query-replace started without search text")
	}
	if got := b.statusText(); got != noSearchText {
		t.Fatalf("status = %q, want %q", got, noSearchText)
	}
}

func TestEscClosesTheBar(t *testing.T) {
	t.Parallel()
	b := Open(newFake("x"), testStyles(), false)
	if !b.Active() {
		t.Fatal("new bar is not active")
	}
	press(b, "esc")
	if b.Active() {
		t.Fatal("bar still active after Esc")
	}
}

func TestTabShowsReplaceRowAndSwitches(t *testing.T) {
	t.Parallel()
	f := newFake("x")
	b := Open(f, testStyles(), false)
	press(b, "tab")
	if !b.replaceOpen || !b.inReplace {
		t.Fatal("first Tab did not show and focus the replacement row")
	}
	press(b, "tab")
	if b.inReplace {
		t.Fatal("second Tab did not switch back to the search row")
	}
	press(b, "shift+tab")
	if !b.inReplace {
		t.Fatal("Shift+Tab did not switch to the replacement row")
	}
}

func TestDrawRowsWithAndWithoutReplace(t *testing.T) {
	t.Parallel()
	b := Open(newFake("x"), testStyles(), false)
	typeText(b, "x")
	rows := frame(b, 60, 10)
	if !strings.HasPrefix(rows[8], " [ ] Case Sensitive (⌥C)") {
		t.Fatalf("options row = %q", rows[8])
	}
	if !strings.HasPrefix(rows[9], "Search: x") {
		t.Fatalf("search row = %q", rows[9])
	}
	if !strings.Contains(rows[9], "Match 1 of 1") {
		t.Fatalf("search row %q has no match status", rows[9])
	}

	press(b, "tab")
	rows = frame(b, 60, 10)
	if !strings.HasPrefix(rows[7], " [ ] Case Sensitive") {
		t.Fatalf("with replace: options row = %q", rows[7])
	}
	if !strings.HasPrefix(rows[8], "Search: x") {
		t.Fatalf("with replace: search row = %q", rows[8])
	}
	if !strings.HasPrefix(rows[9], "Replace:") {
		t.Fatalf("with replace: replace row = %q", rows[9])
	}
}

func TestCursorFollowsTheFocusedField(t *testing.T) {
	t.Parallel()
	b := Open(newFake("x"), testStyles(), false)
	typeText(b, "fo")
	x, y, ok := b.Cursor(60, 10)
	if !ok || x != len("Search: ")+2 || y != 9 {
		t.Fatalf("Cursor = (%d, %d, %v), want (%d, 9, true)", x, y, ok, len("Search: ")+2)
	}

	press(b, "tab")
	typeText(b, "r")
	x, y, ok = b.Cursor(60, 10)
	if !ok || x != len("Replace: ")+1 || y != 9 {
		t.Fatalf("replace Cursor = (%d, %d, %v)", x, y, ok)
	}
}

func TestCursorOffGridReportsNoCaret(t *testing.T) {
	t.Parallel()
	b := Open(newFake("x"), testStyles(), false)
	typeText(b, strings.Repeat("a", 30))
	if _, _, ok := b.Cursor(10, 10); ok {
		t.Fatal("caret reported inside a 10-cell row it does not fit in")
	}
}

func TestRowsForKeepsHeaderRows(t *testing.T) {
	t.Parallel()
	tests := []struct {
		h       int
		replace bool
		want    rows
	}{
		{h: 10, want: rows{opt: 8, search: 9, repl: -1}},
		{h: 10, replace: true, want: rows{opt: 7, search: 8, repl: 9}},
		{h: 3, want: rows{opt: 2, search: -1, repl: -1}},
	}
	for _, tt := range tests {
		if got := rowsFor(tt.h, tt.replace); got != tt.want {
			t.Errorf("rowsFor(%d, %v) = %+v, want %+v", tt.h, tt.replace, got, tt.want)
		}
	}
}

func TestAdvancePos(t *testing.T) {
	t.Parallel()
	start := buffer.Pos{Line: 2, Col: 3}
	if got := advancePos(start, "héé"); got != (buffer.Pos{Line: 2, Col: 6}) {
		t.Errorf("single line: got %v", got)
	}
	if got := advancePos(start, "a\nbc"); got != (buffer.Pos{Line: 3, Col: 2}) {
		t.Errorf("two lines: got %v", got)
	}
}
