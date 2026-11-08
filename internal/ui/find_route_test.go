package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/keymap"
	"github.com/tbxark/gvim/internal/search"
	"github.com/tbxark/gvim/internal/theme"
)

// searchFake is a fakeEditor that tracks a selection and searches the document
// text with internal/search. It is the EditorState the find tests drive.
type searchFake struct {
	*fileEditorFake
	anchor buffer.Pos
	sel    bool
}

// SetCursor moves the caret. A move with extend grows the selection from its
// anchor, or from the old caret when there is no selection, and any other move
// clears it.
func (f *searchFake) SetCursor(p buffer.Pos, extend bool) {
	if extend {
		if !f.sel {
			f.anchor = f.doc.cursor
			f.sel = true
		}
	} else {
		f.sel = false
	}
	f.doc.cursor = p
}

// Active returns the document with the selection of the fake.
func (f *searchFake) Active() Document { return selDoc{fakeDoc: f.doc, s: f} }

// FindMatches returns the matches of q in the fake text.
func (f *searchFake) FindMatches(q search.Query) ([]search.Match, error) {
	return search.Find(buffer.New(strings.Join(f.doc.lines, "\n")), q)
}

// ReplaceMatch replaces m in the fake text with repl, as the editor does, and
// returns the inserted text.
func (f *searchFake) ReplaceMatch(q search.Query, repl string, m search.Match) (string, error) {
	b := buffer.New(strings.Join(f.doc.lines, "\n"))
	text, err := search.Replace(b, q, repl, m)
	if err != nil {
		return "", err
	}
	f.doc.lines = strings.Split(b.Text(), "\n")
	return text, nil
}

// ReplaceMatches replaces every match of q in the fake text with repl.
func (f *searchFake) ReplaceMatches(q search.Query, repl string) (int, error) {
	b := buffer.New(strings.Join(f.doc.lines, "\n"))
	n, err := search.ReplaceAll(b, q, repl)
	if err != nil {
		return 0, err
	}
	f.doc.lines = strings.Split(b.Text(), "\n")
	return n, nil
}

// text returns the fake document text.
func (f *searchFake) text() string { return strings.Join(f.doc.lines, "\n") }

// selDoc is the fake document with the selection of its searchFake.
type selDoc struct {
	*fakeDoc
	s *searchFake
}

// Selection reports the selection of the fake, anchor first.
func (d selDoc) Selection() (a, h buffer.Pos, ok bool) {
	if !d.s.sel {
		return buffer.Pos{}, buffer.Pos{}, false
	}
	return d.s.anchor, d.cursor, true
}

// newRouteModel returns a macOS-keymap model over ed, sized to 100 by 30.
func newRouteModel(t *testing.T, ed EditorState) *Model {
	t.Helper()
	return newRouteModelPreset(t, ed, "macos")
}

// newRouteModelPreset returns a model over ed with the named built-in keymap,
// sized to 100 by 30.
func newRouteModelPreset(t *testing.T, ed EditorState, preset string) *Model {
	t.Helper()
	km, err := keymap.ByName(preset)
	if err != nil {
		t.Fatal(err)
	}
	m := New(Deps{Editor: ed, Keys: presetKeymap{km: km}, Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return m
}

// newSearchModel returns a model over a searchFake of lines.
func newSearchModel(t *testing.T, lines ...string) (*Model, *searchFake) {
	t.Helper()
	sf := &searchFake{fileEditorFake: &fileEditorFake{fakeEditor: newFakeEditor(lines...)}}
	return newRouteModel(t, sf), sf
}

// newDefaultSearchModel is newSearchModel on the default keymap. The macOS keymap
// binds Ctrl+Alt+R to lsp_rename, so the query_replace tests use the default one.
func newDefaultSearchModel(t *testing.T, lines ...string) (*Model, *searchFake) {
	t.Helper()
	sf := &searchFake{fileEditorFake: &fileEditorFake{fakeEditor: newFakeEditor(lines...)}}
	return newRouteModelPreset(t, sf, "default"), sf
}

// noDocSearchFake is a searchFake with no tab open.
type noDocSearchFake struct{ *searchFake }

// Active reports that no document is open.
func (noDocSearchFake) Active() Document { return nil }

// runCmds runs cmd and feeds its message back to m, as the program does.
func runCmds(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	m.Update(cmd())
}

func TestCtrlFOpensTheFindBarThroughTheRoot(t *testing.T) {
	m := newRouteModel(t, newFakeEditor("hello world", "second line", "third"))
	press(m, ctrl('f'))

	if !m.findOpen() || len(m.overlays) != 1 {
		t.Fatalf("overlay stack has %d entries after Ctrl+F, want the find bar", len(m.overlays))
	}
	if _, ok := m.overlays[0].(findOverlay); !ok {
		t.Fatalf("top overlay = %T, want the find bar", m.overlays[0])
	}
	rows := screenRows(m)
	h := len(rows)
	if got := strings.TrimSpace(rows[h-1]); got != "Search:" {
		t.Errorf("last row = %q, want the search row", got)
	}
	if !strings.Contains(rows[h-2], "[ ] Case Sensitive (Alt+C)") || !strings.Contains(rows[h-2], "[ ] Regex (Alt+R)") {
		t.Errorf("row %d = %q, want the option toggles", h-2, rows[h-2])
	}
	if !strings.Contains(rows[h-3], "Local") {
		t.Errorf("row %d = %q, want the status row above the bar", h-3, rows[h-3])
	}
	if l := m.layout(); l.statusY != h-3 || l.bodyBottom != h-4 {
		t.Errorf("layout status %d and body bottom %d, want %d and %d", l.statusY, l.bodyBottom, h-3, h-4)
	}
}

func TestEscClosesTheFindBarAndGivesBackTheBody(t *testing.T) {
	m := newRouteModel(t, newFakeEditor("hello"))
	press(m, ctrl('f'))
	press(m, special(tea.KeyEscape))
	if len(m.overlays) != 0 || m.findOpen() {
		t.Fatalf("overlay stack has %d entries after Esc, want none", len(m.overlays))
	}
	if l := m.layout(); l.statusY != 29 || l.bodyBottom != 28 {
		t.Errorf("layout status %d and body bottom %d after the bar closed, want 29 and 28", l.statusY, l.bodyBottom)
	}
}

func TestCtrlGWithoutASearchSaysHowToStart(t *testing.T) {
	m := newRouteModel(t, newFakeEditor("hello world"))
	press(m, ctrl('g'))
	if m.notice != noActiveSearch {
		t.Fatalf("notice = %q, want %q", m.notice, noActiveSearch)
	}
	if !strings.Contains(strings.Join(screenRows(m), "\n"), "No active search. Use Edit > Find") {
		t.Error("the status row does not show the message")
	}
	if len(m.overlays) != 0 {
		t.Errorf("Ctrl+G opened %d overlays, want none", len(m.overlays))
	}
}

func TestFindBarNavigatesAndCtrlGContinuesAfterClose(t *testing.T) {
	m, sf := newSearchModel(t, "foo bar foo", "x foo")
	press(m, ctrl('f'))
	for _, r := range "foo" {
		press(m, typed(string(r)))
	}
	// Typing only searches: the caret and the selection stay.
	if sf.sel || sf.doc.cursor != (buffer.Pos{Line: 0, Col: 0}) {
		t.Fatalf("after typing, selection anchor %+v head %+v (selected %v)", sf.anchor, sf.doc.cursor, sf.sel)
	}
	// Enter jumps to the first match and closes the bar.
	press(m, special(tea.KeyEnter))
	if m.findOpen() || !sf.sel || sf.anchor != (buffer.Pos{Line: 0, Col: 0}) || sf.doc.cursor != (buffer.Pos{Line: 0, Col: 3}) {
		t.Fatalf("after Enter, open %v, selection anchor %+v head %+v (selected %v)", m.findOpen(), sf.anchor, sf.doc.cursor, sf.sel)
	}
	press(m, special(tea.KeyF3))
	if sf.anchor != (buffer.Pos{Line: 0, Col: 8}) {
		t.Fatalf("F3 selected from %+v, want the second match at 0:8", sf.anchor)
	}

	// The bar is closed, but its search text still drives Ctrl+G.
	press(m, ctrl('g'))
	if sf.anchor != (buffer.Pos{Line: 1, Col: 2}) {
		t.Fatalf("Ctrl+G after close selected from %+v, want the match at 1:2", sf.anchor)
	}
	if m.notice != "Match 3 of 3" {
		t.Errorf("notice = %q, want the match count", m.notice)
	}
}

// TestPaletteColonJumpsOnTheFirstEnter: Fresh's palette ":" mode jumps to the line the
// query names (docs/features/command-palette.md), so one Enter is enough and no second
// prompt opens. The previous test pinned the extra go-to-line prompt that PC54 reported.
func TestPaletteColonJumpsOnTheFirstEnter(t *testing.T) {
	for _, tc := range []struct {
		query     string
		line, col int
	}{
		{":2", 2, 1},
		{":3:2", 3, 2},
	} {
		ed := &fileEditorFake{fakeEditor: newFakeEditor("one", "two", "three")}
		m := newRouteModel(t, ed)
		press(m, ctrl('p'))
		press(m, special(tea.KeyBackspace)) // the ">" prefix goes, leaving an empty query
		for _, r := range tc.query {
			press(m, typed(string(r)))
		}
		runCmds(m, press(m, special(tea.KeyEnter)))
		if ed.line != tc.line || ed.col != tc.col {
			t.Errorf("%q: GotoLine(%d, %d) after one Enter, want (%d, %d)", tc.query, ed.line, ed.col, tc.line, tc.col)
		}
		for _, o := range m.overlays {
			if _, ok := o.(lineOverlay); ok {
				t.Errorf("%q: a go-to-line prompt opened after the jump", tc.query)
			}
		}
	}
}

func TestGoToLineKeyOpensTheGotoPrompt(t *testing.T) {
	ed := &fileEditorFake{fakeEditor: newFakeEditor("one", "two")}
	m := newRouteModel(t, ed)
	press(m, ctrl('l'))
	if _, ok := m.overlays[len(m.overlays)-1].(lineOverlay); !ok {
		t.Fatalf("Ctrl+L opened %T, want the go-to-line prompt", m.overlays[len(m.overlays)-1])
	}
	for _, r := range "2:2" {
		press(m, typed(string(r)))
	}
	runCmds(m, press(m, special(tea.KeyEnter)))
	if ed.line != 2 || ed.col != 2 {
		t.Fatalf("GotoLine(%d, %d), want (2, 2)", ed.line, ed.col)
	}
}

func TestGotoPromptRejectsWhatIsNotALine(t *testing.T) {
	ed := &fileEditorFake{fakeEditor: newFakeEditor("one")}
	m := newRouteModel(t, ed)
	press(m, ctrl('l'))
	for _, r := range "x" {
		press(m, typed(string(r)))
	}
	runCmds(m, press(m, special(tea.KeyEnter)))
	if len(m.overlays) != 1 {
		t.Fatalf("the prompt closed on a bad line, overlays = %d", len(m.overlays))
	}
	if ed.line != 0 {
		t.Errorf("GotoLine ran on a bad line: %d", ed.line)
	}
}

// Encoding satisfies Document for the selection fake.
func (selDoc) Encoding() string { return "ASCII" }

// LineEnding satisfies Document for the selection fake.
func (selDoc) LineEnding() string { return "LF" }

func TestStartSearchReplaceOpensTheFindBarWithTheReplaceRow(t *testing.T) {
	m, _ := newDefaultSearchModel(t, "hello world")
	doAction(m, "start_search_replace")

	if !m.findOpen() {
		t.Fatalf("overlay stack has %d entries after start_search_replace, want the find bar", len(m.overlays))
	}
	if rows := m.findBar.Rows(); rows != 3 {
		t.Fatalf("bar covers %d rows, want 3 with the replacement row", rows)
	}
	rows := screenRows(m)
	if got := strings.TrimSpace(rows[len(rows)-1]); !strings.HasPrefix(got, "Replace:") {
		t.Errorf("last row = %q, want the replacement row", got)
	}
}

func TestSearchReplaceNextAndPrevStepThroughMatches(t *testing.T) {
	m, sf := newDefaultSearchModel(t, "foo bar foo", "x foo")
	press(m, ctrl('f'))
	for _, r := range "foo" {
		press(m, typed(string(r)))
	}
	press(m, special(tea.KeyEnter))
	// The bar is closed and its search text is held. The selection is 0:0 to 0:3.

	doAction(m, "search_replace_next_match")
	if sf.anchor != (buffer.Pos{Line: 0, Col: 8}) || m.notice != "Match 2 of 3" {
		t.Fatalf("next selected from %+v with notice %q, want the match at 0:8 and %q", sf.anchor, m.notice, "Match 2 of 3")
	}
	doAction(m, "search_replace_next_match")
	if sf.anchor != (buffer.Pos{Line: 1, Col: 2}) {
		t.Fatalf("second next selected from %+v, want the match at 1:2", sf.anchor)
	}
	doAction(m, "search_replace_prev_match")
	if sf.anchor != (buffer.Pos{Line: 0, Col: 8}) || m.notice != "Match 2 of 3" {
		t.Fatalf("prev selected from %+v with notice %q, want the match at 0:8", sf.anchor, m.notice)
	}
}

func TestSearchReplaceNavigationWithoutASearchSaysHowToStart(t *testing.T) {
	m, _ := newDefaultSearchModel(t, "foo")
	doAction(m, "search_replace_next_match")
	if m.notice != noActiveSearch {
		t.Fatalf("notice = %q, want %q", m.notice, noActiveSearch)
	}
	if len(m.overlays) != 0 {
		t.Errorf("the key opened %d overlays, want none", len(m.overlays))
	}
}

// heldReplaceModel types x into the find bar with z as the replacement, then
// closes the bar with Esc, which leaves the search and replacement held.
func heldReplaceModel(t *testing.T, lines ...string) (*Model, *searchFake) {
	t.Helper()
	m, sf := newDefaultSearchModel(t, lines...)
	press(m, ctrl('f'))
	press(m, typed("x"))
	press(m, special(tea.KeyTab))
	press(m, typed("z"))
	press(m, special(tea.KeyEscape))
	if m.findOpen() {
		t.Fatal("bar still open after Esc")
	}
	return m, sf
}

func TestQueryReplaceAnswersEachMatchFromTheHeldBar(t *testing.T) {
	m, sf := heldReplaceModel(t, "x x x x")
	doAction(m, "query_replace")
	if !m.findOpen() {
		t.Fatalf("overlay stack has %d entries after Ctrl+Alt+R, want the find bar", len(m.overlays))
	}
	rows := screenRows(m)
	h := len(rows)
	if !strings.Contains(rows[h-2], "Replace? (y)es (n)o (a)ll (c)ancel") {
		t.Fatalf("row %d = %q, want Fresh's query-replace question", h-2, rows[h-2])
	}

	press(m, typed("y")) // replace 0:0
	if got := sf.text(); got != "z x x x" {
		t.Fatalf("after y: text = %q, want %q", got, "z x x x")
	}
	press(m, typed("n")) // skip 0:2
	if got := sf.text(); got != "z x x x" {
		t.Fatalf("after n: text = %q, want no change", got)
	}
	press(m, typed("q"))
	if got := sf.text(); got != "z x x x" {
		t.Fatalf("after q: text = %q, want no change", got)
	}
	if rows := screenRows(m); !strings.Contains(rows[len(rows)-2], "Replaced 1 occurrence(s)") {
		t.Fatalf("row %d = %q, want the count after q", len(rows)-2, rows[len(rows)-2])
	}
}

func TestEscDuringQueryReplaceStopsTheRun(t *testing.T) {
	m, sf := heldReplaceModel(t, "x x x")
	doAction(m, "query_replace")
	press(m, typed("y"))
	press(m, special(tea.KeyEscape))

	if m.findOpen() {
		t.Fatal("Esc left the find bar on the overlay stack")
	}
	if got := sf.text(); got != "z x x" {
		t.Fatalf("text = %q, want the one replacement made before Esc", got)
	}
}

func TestQueryReplaceWithoutASearchKeepsTheBarOpenAndSaysSo(t *testing.T) {
	m, sf := newDefaultSearchModel(t, "x x")
	doAction(m, "query_replace")
	if !m.findOpen() {
		t.Fatal("Ctrl+Alt+R without a search did not open the bar")
	}
	rows := screenRows(m)
	if !strings.Contains(rows[len(rows)-2], "Query replace: empty search query.") {
		t.Fatalf("row %d = %q, want the empty-query message", len(rows)-2, rows[len(rows)-2])
	}
	if got := sf.text(); got != "x x" {
		t.Fatalf("text = %q, want no change", got)
	}
}

func TestSearchReplaceActionsNeedADocument(t *testing.T) {
	for _, action := range []string{"start_search_replace", "search_replace_next_match", "search_replace_prev_match", "query_replace"} {
		t.Run(action, func(t *testing.T) {
			sf := &searchFake{fileEditorFake: &fileEditorFake{fakeEditor: newFakeEditor("x")}}
			m := newRouteModel(t, noDocSearchFake{sf})
			if err := m.Run(action); err != nil {
				t.Fatalf("Run(%q) = %v, want it handled", action, err)
			}
			if m.notice != noDocumentNotice {
				t.Errorf("notice = %q, want %q", m.notice, noDocumentNotice)
			}
			if m.findOpen() {
				t.Error("the find bar opened with no document")
			}
		})
	}
}

// selectingEditorFake records which of the two jumps ran.
type selectingEditorFake struct {
	*fileEditorFake
	selecting bool
}

func (f *selectingEditorFake) GotoLineSelecting(line, col int) error {
	f.selecting = true
	f.line, f.col = line, col
	return nil
}

func TestGotoPromptSelectsAndOtherJumpsDoNot(t *testing.T) {
	ed := &selectingEditorFake{fileEditorFake: &fileEditorFake{fakeEditor: newFakeEditor("one", "two")}}
	m := newRouteModel(t, ed)
	m.openAt("x.txt", false, 2, 1)
	if ed.selecting || ed.line != 2 {
		t.Fatalf("openAt: selecting = %v, line = %d; a jump must use GotoLine", ed.selecting, ed.line)
	}
	press(m, ctrl('l'))
	press(m, typed("1"))
	runCmds(m, press(m, special(tea.KeyEnter)))
	if !ed.selecting || ed.line != 1 {
		t.Fatalf("prompt: selecting = %v, line = %d; the prompt must use GotoLineSelecting", ed.selecting, ed.line)
	}
}
