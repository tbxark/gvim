package ui

import (
	"errors"
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

// ReplaceMatch is not used by the navigation tests.
func (f *searchFake) ReplaceMatch(search.Query, string, search.Match) (string, error) {
	return "", errors.New("replace is not in the fake")
}

// ReplaceMatches is not used by the navigation tests.
func (f *searchFake) ReplaceMatches(search.Query, string) (int, error) {
	return 0, errors.New("replace is not in the fake")
}

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
	km, err := keymap.ByName("macos")
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
	if !strings.Contains(rows[h-2], "[ ] Case Sensitive (⌥C)") || !strings.Contains(rows[h-2], "[ ] Regex (⌥R)") {
		t.Errorf("row %d = %q, want the option toggles", h-2, rows[h-2])
	}
	if !strings.Contains(rows[h-3], "Trusted") {
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
	if !strings.Contains(strings.Join(screenRows(m), "\n"), "No active search. Press Ctrl+F") {
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
	// Typing selects the first match at or after the caret.
	if !sf.sel || sf.anchor != (buffer.Pos{Line: 0, Col: 0}) || sf.doc.cursor != (buffer.Pos{Line: 0, Col: 3}) {
		t.Fatalf("after typing, selection anchor %+v head %+v (selected %v)", sf.anchor, sf.doc.cursor, sf.sel)
	}
	press(m, special(tea.KeyF3))
	if sf.anchor != (buffer.Pos{Line: 0, Col: 8}) {
		t.Fatalf("F3 selected from %+v, want the second match at 0:8", sf.anchor)
	}
	press(m, special(tea.KeyEscape))

	// The bar is closed, but its search text still drives Ctrl+G.
	press(m, ctrl('g'))
	if sf.anchor != (buffer.Pos{Line: 1, Col: 2}) {
		t.Fatalf("Ctrl+G after close selected from %+v, want the match at 1:2", sf.anchor)
	}
	if m.notice != "Match 3 of 3" {
		t.Errorf("notice = %q, want the match count", m.notice)
	}
}

func TestPaletteColonOpensTheGotoPrompt(t *testing.T) {
	ed := &fileEditorFake{fakeEditor: newFakeEditor("one", "two", "three")}
	m := newRouteModel(t, ed)
	press(m, ctrl('p'))
	press(m, special(tea.KeyBackspace)) // the ">" prefix goes, leaving an empty query
	press(m, typed(":"))
	press(m, typed("2"))
	runCmds(m, press(m, special(tea.KeyEnter)))

	o, ok := m.overlays[len(m.overlays)-1].(lineOverlay)
	if !ok {
		t.Fatalf("top overlay = %T after the ':' row, want the go-to-line prompt", m.overlays[len(m.overlays)-1])
	}
	if got := o.l.Value(); got != "2" {
		t.Fatalf("prompt text = %q, want the chosen line 2", got)
	}
	runCmds(m, press(m, special(tea.KeyEnter)))
	if ed.line != 2 || ed.col != 1 {
		t.Fatalf("GotoLine(%d, %d) after the prompt, want (2, 1)", ed.line, ed.col)
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
