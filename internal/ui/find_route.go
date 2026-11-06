package ui

import (
	"errors"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/search"
	"github.com/tbxark/gvim/internal/ui/find"
	"github.com/tbxark/gvim/internal/ui/grid"
)

// noActiveSearch is the status text of find_next and find_previous when no search
// text is held. It is the legacy text, and the legacy screen shows it.
const noActiveSearch = "No active search. Press Ctrl+F to search."

// errSearchUnsupported is what the find bar reports when the editor the root
// wraps cannot search. The bar shows it in place of the match count.
var errSearchUnsupported = errors.New("search is not supported in this port")

// searchPort is the search surface of the editor over the active document. The
// app adapter implements it, and the root asserts it on EditorState, as it does
// for fileEditor. The matches come from internal/search over the document's
// buffer. Each replacement is one undo step.
type searchPort interface {
	FindMatches(q search.Query) ([]search.Match, error)
	ReplaceMatch(q search.Query, repl string, m search.Match) (string, error)
	ReplaceMatches(q search.Query, repl string) (int, error)
}

// findOverlay adapts *find.Bar to Overlay. The bar cannot name Overlay, because
// that would import this package. The bar stays on the stack until it closes
// itself or Esc pops it.
type findOverlay struct{ b *find.Bar }

// Update forwards the message to the bar. It returns nil once the bar is closed,
// so the stack pops it.
func (o findOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	b, cmd := o.b.Update(msg)
	if !b.Active() {
		return nil, cmd
	}
	return o, cmd
}

// Draw paints the bar over the bottom rows of the frame.
func (o findOverlay) Draw(g *grid.Grid) { o.b.Draw(g) }

// Active reports whether the bar is still open.
func (o findOverlay) Active() bool { return o.b.Active() }

// findHost is the find.Editor the bar works through. It reads the active document
// and moves the caret and the selection through the EditorState port, so every
// change of text and selection goes through the editor.
type findHost struct{ m *Model }

// Cursor is the caret of the active document.
func (h findHost) Cursor() buffer.Pos {
	if doc := h.m.ed.Active(); doc != nil {
		return doc.Cursor()
	}
	return buffer.Pos{}
}

// Selection returns the ordered ends of the selection, and ok is false when
// nothing is selected.
func (h findHost) Selection() (start, end buffer.Pos, ok bool) {
	doc := h.m.ed.Active()
	if doc == nil {
		return buffer.Pos{}, buffer.Pos{}, false
	}
	a, b, ok := doc.Selection()
	if !ok {
		return buffer.Pos{}, buffer.Pos{}, false
	}
	if b.Less(a) {
		a, b = b, a
	}
	return a, b, true
}

// Find returns the matches of q in the active document. An editor without search
// gives an error.
func (h findHost) Find(q search.Query) ([]search.Match, error) {
	s, ok := h.m.ed.(searchPort)
	if !ok {
		return nil, errSearchUnsupported
	}
	matches, err := s.FindMatches(q)
	if err != nil {
		return nil, err
	}
	// find_in_selection limits the find bar to the editor's search range.
	if r, ok := h.m.ed.(searchRanger); ok {
		if sel, limited := r.SearchRange(); limited {
			matches = withinRange(matches, sel)
		}
	}
	return matches, nil
}

// searchRanger is the optional view of the search range the editor keeps for
// find_in_selection (see editor.SearchRange). The app adapter forwards it.
type searchRanger interface {
	SearchRange() (buffer.Selection, bool)
}

// withinRange keeps the matches that lie inside sel, in place.
func withinRange(matches []search.Match, sel buffer.Selection) []search.Match {
	from, to := sel.Ordered()
	return slices.DeleteFunc(matches, func(m search.Match) bool {
		return m.Start.Less(from) || to.Less(m.End)
	})
}

// Select selects m. The anchor is the start of the match and the caret goes to
// its end, as SetCursor with extend does.
func (h findHost) Select(m search.Match) {
	h.m.ed.SetCursor(m.Start, false)
	h.m.ed.SetCursor(m.End, true)
}

// Place moves the caret to p and clears the selection.
func (h findHost) Place(p buffer.Pos) {
	h.m.ed.SetCursor(p, false)
}

// Replace replaces the match m of q with repl through the editor.
func (h findHost) Replace(q search.Query, repl string, m search.Match) (string, error) {
	s, ok := h.m.ed.(searchPort)
	if !ok {
		return "", errSearchUnsupported
	}
	return s.ReplaceMatch(q, repl, m)
}

// ReplaceAll replaces every match of q with repl through the editor.
func (h findHost) ReplaceAll(q search.Query, repl string) (int, error) {
	s, ok := h.m.ed.(searchPort)
	if !ok {
		return 0, errSearchUnsupported
	}
	return s.ReplaceMatches(q, repl)
}

// findAction runs the search and replace actions: start_search_replace opens the
// find bar with its replacement row, search_replace_next_match and
// search_replace_prev_match step through the matches, and query_replace asks
// about each match. It reports whether it handled the action. The uiAction
// switch falls through to it.
func (m *Model) findAction(action string) (tea.Cmd, bool) {
	switch action {
	case "start_search_replace", "search_replace_next_match", "search_replace_prev_match", "query_replace":
	default:
		return nil, false
	}
	if !m.hasDocument() {
		m.notice = noDocumentNotice
		return nil, true
	}
	switch action {
	case "start_search_replace":
		m.openFind(true)
	case "search_replace_next_match":
		m.findNavigate(true)
	case "search_replace_prev_match":
		m.findNavigate(false)
	case "query_replace":
		m.queryReplace()
	}
	return nil, true
}

// queryReplace starts query-replace from the bar's anchor, which is the start of
// the selection or the caret. The bar's search and replacement texts are the ones
// it holds, so a bar that Esc popped is shown again with them. With no search
// text the bar says so and stays open.
func (m *Model) queryReplace() {
	switch {
	case m.findBar == nil:
		m.openFind(true)
	case !m.findOpen():
		m.findBar.Reopen()
		m.showFind()
	}
	m.findBar.ShowReplace()
	m.findBar.StartQueryReplace()
}

// findOpen reports whether a find bar is on the overlay stack. A closed bar is
// popped, so the editor body gets its rows back.
func (m *Model) findOpen() bool {
	for _, o := range m.overlays {
		if _, ok := o.(findOverlay); ok {
			return true
		}
	}
	return false
}

// openFind shows the find bar, with its replacement row when withReplace is set.
// A second bar is not opened over the first.
func (m *Model) openFind(withReplace bool) {
	if m.findOpen() {
		return
	}
	m.findBar = find.Open(findHost{m: m}, m.styles, withReplace)
	m.showFind()
}

// showFind puts the find bar on top of the overlay stack. The bar takes rows from
// the editor body, so the body is resized and the caret is kept on screen.
func (m *Model) showFind() {
	m.pushOverlay(findOverlay{b: m.findBar})
	m.syncViewport()
	m.revealCaret()
}

// findNavigate runs find_next or find_previous. With no search text it sets the
// legacy status. Otherwise it selects the next match after the selection end, or
// the previous one before the selection start, and wraps around the document.
// The search text is the one the find bar holds, open or closed.
func (m *Model) findNavigate(forward bool) {
	if m.findBar == nil {
		m.notice = noActiveSearch
		return
	}
	q := m.findBar.Query()
	if q.Text == "" {
		m.notice = noActiveSearch
		return
	}
	h := findHost{m: m}
	matches, err := h.Find(q)
	if err != nil {
		m.notice = err.Error()
		return
	}
	if len(matches) == 0 {
		m.notice = search.StatusText(0, 0)
		return
	}
	start, end, ok := h.Selection()
	if !ok {
		start, end = h.Cursor(), h.Cursor()
	}
	var idx int
	if forward {
		idx, _ = search.Next(matches, end, true)
	} else {
		idx, _ = search.Prev(matches, start, true)
	}
	h.Select(matches[idx])
	m.revealCaret()
	m.notice = search.StatusText(idx, len(matches))
}

// revealCaret scrolls the editor body so the caret line is on screen. It moves
// the viewport only when the caret is outside it, and the caret itself stays put.
func (m *Model) revealCaret() {
	doc := m.ed.Active()
	rows := m.bodyRows()
	if doc == nil || rows <= 0 {
		return
	}
	line, top := doc.Cursor().Line, doc.Top()
	switch {
	case line < top:
		m.ed.ScrollTo(line)
	case line >= top+rows:
		m.ed.ScrollTo(line - rows + 1)
	}
}
