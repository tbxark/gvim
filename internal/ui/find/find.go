// Package find is the find bar of gvim: the search field, the replacement
// field, the three option toggles, the match status, and the find, replace,
// replace-all and query-replace commands. The root model gives the bar the keys
// while it is open and paints it over the frame.
//
// The bar never edits the document itself. It asks the root through Editor, and
// it keeps only its own state: the two fields, the options, the anchor it
// searches from, and the selection it made. The root implements Editor over the
// editor, so every change of text and selection goes through the editor.
//
// The bar does not import the ui root. The root wraps *Bar in its Overlay type
// (see internal/ui/ports.go), because an interface method cannot name a type
// from a package that imports this one.
package find

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/search"
	"github.com/tbxark/gvim/internal/ui/grid"
	"github.com/tbxark/gvim/internal/ui/style"
)

// Labels of the bar. The search and replace prompts are the legacy texts.
const (
	searchLabel  = "Search:"
	replaceLabel = "Replace:"
	labelGap     = 3 // blank cells between two option labels
	queryPrompt  = "Replace? (y)es (n)o (a)ll (c)ancel"
	queryEmpty   = "Query replace: empty search query."
)

// The option toggles, in the order the options row shows them.
const (
	optCase = iota
	optWord
	optRegex
	numOptions
)

// optionLabels are the toggle labels, and optionNotes the status messages that
// report a change, indexed like the options.
var (
	optionLabels = [numOptions]string{"Case Sensitive (⌥C)", "Whole Word (⌥W)", "Regex (⌥R)"}
	optionNotes  = [numOptions]string{"Case-sensitive search", "Whole-word search", "Regex search"}
)

// Editor is what the find bar asks of the active document. The root model
// implements it over the editor and its buffer. Every method runs inside Update,
// on the editor's goroutine, so the bar sees the document as the last edit left
// it.
type Editor interface {
	// Cursor returns the caret.
	Cursor() buffer.Pos
	// Selection returns the ordered ends of the selection. ok is false when
	// nothing is selected.
	Selection() (start, end buffer.Pos, ok bool)
	// Find returns the matches of q in the document, in document order. It does
	// not change the document.
	Find(q search.Query) ([]search.Match, error)
	// Select selects m and puts the caret at its end.
	Select(m search.Match)
	// Place moves the caret to p, clamped to the document, and clears the
	// selection.
	Place(p buffer.Pos)
	// Replace replaces the match m of q with repl as one undo step and returns
	// the text it inserted. It does not move the caret. It returns
	// search.ErrNoMatch when m is not a current match of q.
	Replace(q search.Query, repl string, m search.Match) (string, error)
	// ReplaceAll replaces every match of q with repl as one undo step and
	// returns how many it replaced.
	ReplaceAll(q search.Query, repl string) (int, error)
}

// Bar is the find bar. It is an overlay: while it is open, the root sends it
// the key presses, and it is drawn over the frame. Update returns the bar
// itself. Active turns false once the bar is closed.
type Bar struct {
	ed     Editor
	styles styles

	search textinput.Model
	repl   textinput.Model

	opts        [numOptions]bool // the toggles: case, whole word, regex
	replaceOpen bool             // the replacement row is shown
	inReplace   bool             // the keys go to the replacement row

	anchor   buffer.Pos // the caret when the bar opened; the first match is at or after it
	hasSel   bool       // the bar's selection: a match it selected, or the selection it found
	selStart buffer.Pos
	selEnd   buffer.Pos

	status string    // the match count, or the error of the pattern, at the right of the search row
	notice string    // the last option change; shown until the next key
	qr     *queryRun // non-nil while query-replace asks about each match
	closed bool
}

// queryRun is one query-replace session. from is where the next match is
// looked for, and count is the number of replacements made so far.
type queryRun struct {
	from  buffer.Pos
	count int
}

// Open returns a find bar for the document of ed, anchored at its caret. With
// withReplace the replacement row is shown and focused. The search field is
// focused otherwise.
func Open(ed Editor, st style.Styles, withReplace bool) *Bar {
	b := &Bar{
		ed:     ed,
		styles: newStyles(st),
		search: newField(),
		repl:   newField(),
		anchor: ed.Cursor(),
	}
	if start, end, ok := ed.Selection(); ok {
		b.hasSel, b.selStart, b.selEnd = true, start, end
	}
	b.focusRow()
	if withReplace {
		b.ShowReplace()
	}
	return b
}

// Reopen shows the bar again with the fields and options it held, for a bar that
// is not on screen: one that Esc popped, or one that Esc closed. The bar searches
// from the start of the selection when there is one, and from the caret
// otherwise. A query-replace the bar left running is dropped. The root pushes the
// bar back onto its overlay stack.
func (b *Bar) Reopen() {
	b.closed = false
	b.qr = nil
	b.anchor = b.ed.Cursor()
	b.hasSel = false
	if start, end, ok := b.ed.Selection(); ok {
		b.anchor = start
		b.hasSel, b.selStart, b.selEnd = true, start, end
	}
	b.focusRow()
}

// newField returns a text field with no prompt, a real caret, and no width
// limit. Without a width the field never scrolls, so the caret column is the
// cell count of the text before it.
func newField() textinput.Model {
	m := textinput.New()
	m.Prompt = ""
	m.SetVirtualCursor(false)
	m.SetWidth(0)
	return m
}

// Active reports whether the bar is open.
func (b *Bar) Active() bool { return !b.closed }

// Rows is the number of frame rows the bar covers at the bottom: the options row
// and the search row, plus the replacement row when it is shown. The root takes
// these rows out of the editor body while the bar is on the stack.
func (b *Bar) Rows() int {
	if b.replaceOpen {
		return 3
	}
	return 2
}

// ShowReplace shows the replacement row and moves the keys to it. It does
// nothing more when the row is already shown.
func (b *Bar) ShowReplace() {
	b.replaceOpen = true
	b.inReplace = true
	b.focusRow()
}

// StartQueryReplace begins asking about each match from the anchor, where the bar
// opened or was last reopened. Each answer is y to replace, n to skip, ! or a to
// replace the rest, and q or c to stop; Esc also stops. With no search text it
// only reports that.
func (b *Bar) StartQueryReplace() {
	b.notice = ""
	if b.search.Value() == "" {
		b.status = queryEmpty
		return
	}
	b.qr = &queryRun{from: b.anchor}
	b.queryStep()
}

// Update handles one message. Key presses edit the bar. Other messages are
// ignored. The returned command is always nil, because the bar answers every
// change through Editor.
func (b *Bar) Update(msg tea.Msg) (*Bar, tea.Cmd) {
	kp, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return b, nil
	}
	if b.qr != nil {
		b.queryKey(kp)
		return b, nil
	}
	b.keyPress(kp)
	return b, nil
}

// keyPress runs one key press outside query-replace. Esc closes the bar. F3 and
// Shift+F3 step to the next and previous match, Tab shows the replacement row
// and switches rows, Enter finds the next match, or replaces in the replacement
// row, and Alt+Enter replaces all. Alt+C, Alt+W and Alt+R toggle the options.
// Other keys edit the focused field.
func (b *Bar) keyPress(kp tea.KeyPressMsg) {
	name := tea.Key(kp).Keystroke()
	if _, toggle := toggleOf(name); !toggle {
		b.notice = ""
	}
	switch name {
	case "esc":
		b.closed = true
	case "f3":
		b.step(true)
	case "shift+f3":
		b.step(false)
	case "tab", "shift+tab":
		if !b.replaceOpen {
			b.ShowReplace()
		} else {
			b.inReplace = !b.inReplace
			b.focusRow()
		}
	case "enter":
		if b.inReplace {
			b.replaceCurrent()
		} else {
			b.step(true)
		}
	case "alt+enter":
		if b.inReplace {
			b.replaceAll()
		} else {
			b.step(true)
		}
	default:
		if i, ok := toggleOf(name); ok {
			b.toggle(i)
			return
		}
		b.edit(kp)
	}
}

// toggleOf maps an Alt+letter key name to its option.
func toggleOf(name string) (int, bool) {
	switch name {
	case "alt+c":
		return optCase, true
	case "alt+w":
		return optWord, true
	case "alt+r":
		return optRegex, true
	}
	return 0, false
}

// edit gives a key to the focused field. A change to the search text searches
// again and moves the selection to the first match. A change to the replacement
// text changes no match.
func (b *Bar) edit(kp tea.KeyPressMsg) {
	if b.inReplace {
		b.repl, _ = b.repl.Update(kp)
		return
	}
	before := b.search.Value()
	b.search, _ = b.search.Update(kp)
	if b.search.Value() != before {
		b.refresh(true)
	}
}

// toggle flips option i, searches again under the new option, and reports the
// new state in place of the match status.
func (b *Bar) toggle(i int) {
	b.opts[i] = !b.opts[i]
	b.refresh(true)
	state := " disabled"
	if b.opts[i] {
		state = " enabled"
	}
	b.notice = optionNotes[i] + state
}

// focusRow gives the keyboard to the focused field and takes it from the other.
func (b *Bar) focusRow() {
	if b.inReplace {
		b.search.Blur()
		b.repl.Focus()
		return
	}
	b.repl.Blur()
	b.search.Focus()
}

// query returns the search of the bar: the search text under the options.
func (b *Bar) query() search.Query {
	return search.Query{
		Text:          b.search.Value(),
		CaseSensitive: b.opts[optCase],
		WholeWord:     b.opts[optWord],
		Regex:         b.opts[optRegex],
	}
}

// Query returns the search the bar holds, even after the bar is closed. The root
// runs find_next and find_previous on it when the bar is not open.
func (b *Bar) Query() search.Query { return b.query() }

// Clear empties the search text and the match status, as clear_search does. The
// options and the replacement text stay, and the highlights go with the text.
func (b *Bar) Clear() {
	b.search.SetValue("")
	b.refresh(false)
}

// refresh recomputes the match status for the search text. With move set, the
// selection goes to the first match at or after the anchor. An empty search
// text clears the status, and an invalid pattern shows its error.
func (b *Bar) refresh(move bool) {
	if b.qr != nil {
		return
	}
	if b.search.Value() == "" {
		b.status = ""
		return
	}
	matches, err := b.ed.Find(b.query())
	if err != nil {
		b.status = err.Error()
		return
	}
	if len(matches) == 0 {
		b.status = search.StatusText(0, 0)
		return
	}
	idx, _ := search.Next(matches, b.anchor, true)
	if move {
		b.selectMatch(matches[idx])
	}
	b.status = search.StatusText(idx, len(matches))
}

// step selects the match after the selection end when forward is set, or the
// one before the selection start otherwise. Both wrap around the document.
func (b *Bar) step(forward bool) {
	if b.search.Value() == "" {
		return
	}
	matches, err := b.ed.Find(b.query())
	if err != nil {
		b.status = err.Error()
		return
	}
	if len(matches) == 0 {
		b.status = search.StatusText(0, 0)
		return
	}
	start, end := b.bounds()
	var idx int
	if forward {
		idx, _ = search.Next(matches, end, true)
	} else {
		idx, _ = search.Prev(matches, start, true)
	}
	b.selectMatch(matches[idx])
	b.status = search.StatusText(idx, len(matches))
}

// bounds returns the ordered ends of the selection, or the caret twice when
// nothing is selected.
func (b *Bar) bounds() (start, end buffer.Pos) {
	if b.hasSel {
		return b.selStart, b.selEnd
	}
	c := b.ed.Cursor()
	return c, c
}

// selectMatch selects m in the editor and remembers the selection.
func (b *Bar) selectMatch(m search.Match) {
	b.ed.Select(m)
	b.hasSel = true
	b.selStart, b.selEnd = m.Start, m.End
}

// clearSelection drops the selection in the editor and in the bar. The caret
// stays where it is.
func (b *Bar) clearSelection() {
	b.hasSel = false
	b.ed.Place(b.ed.Cursor())
}

// replaceCurrent replaces the selection when it is exactly one match of the
// search, puts the caret after the new text, and finds the next match. Otherwise
// it only finds the next match.
func (b *Bar) replaceCurrent() {
	if b.search.Value() == "" {
		return
	}
	q := b.query()
	matches, err := b.ed.Find(q)
	if err != nil {
		b.status = err.Error()
		return
	}
	start, end := b.bounds()
	for _, m := range matches {
		if !b.hasSel || m.Start != start || m.End != end {
			continue
		}
		text, err := b.ed.Replace(q, b.repl.Value(), m)
		if err != nil {
			b.status = err.Error()
			return
		}
		b.ed.Place(advancePos(m.Start, text))
		b.hasSel = false
		b.refresh(false)
		b.step(true)
		return
	}
	b.step(true)
}

// replaceAll replaces every match of the search and reports the count.
func (b *Bar) replaceAll() {
	if b.search.Value() == "" {
		return
	}
	n, err := b.ed.ReplaceAll(b.query(), b.repl.Value())
	b.clearSelection()
	if err != nil {
		b.status = err.Error()
		return
	}
	b.status = fmt.Sprintf("Replaced %d", n)
}

// queryCurrent returns the first match at or after the run's position. It
// reports false when there is none.
func (b *Bar) queryCurrent() (search.Match, bool, error) {
	matches, err := b.ed.Find(b.query())
	if err != nil {
		return search.Match{}, false, err
	}
	idx, ok := search.Next(matches, b.qr.from, false)
	if !ok {
		return search.Match{}, false, nil
	}
	return matches[idx], true, nil
}

// queryStep selects the next match of the run, or ends the run when there is
// none.
func (b *Bar) queryStep() {
	cur, found, err := b.queryCurrent()
	if err != nil {
		b.abortQuery(err)
		return
	}
	if !found {
		b.finishQuery()
		return
	}
	b.selectMatch(cur)
	b.status = queryPrompt
}

// queryReplace replaces the match cur and moves the run past the new text.
func (b *Bar) queryReplace(cur search.Match) error {
	text, err := b.ed.Replace(b.query(), b.repl.Value(), cur)
	if err != nil {
		return err
	}
	b.qr.count++
	b.qr.from = advancePos(cur.Start, text)
	b.hasSel = false
	return nil
}

// queryKey answers the query-replace question. Escape ends the run, and each
// key that is not an answer is ignored. y replaces the match, n skips it, ! or a
// replaces it and every later match, and q or c stops.
func (b *Bar) queryKey(kp tea.KeyPressMsg) {
	if tea.Key(kp).Keystroke() == "esc" {
		b.finishQuery()
		return
	}
	if utf8.RuneCountInString(kp.Text) != 1 {
		return
	}
	r, _ := utf8.DecodeRuneInString(kp.Text)
	decision, ok := search.DecisionFromKey(r)
	if !ok {
		return
	}
	cur, found, err := b.queryCurrent()
	if err != nil {
		b.abortQuery(err)
		return
	}
	switch decision {
	case search.DecisionQuit:
		b.finishQuery()
	case search.DecisionSkip:
		if found {
			b.qr.from = cur.End
		}
		b.queryStep()
	case search.DecisionReplace:
		if found {
			if err := b.queryReplace(cur); err != nil {
				b.abortQuery(err)
				return
			}
		}
		b.queryStep()
	case search.DecisionReplaceAll:
		for found {
			if err := b.queryReplace(cur); err != nil {
				b.abortQuery(err)
				return
			}
			cur, found, err = b.queryCurrent()
			if err != nil {
				b.abortQuery(err)
				return
			}
		}
		b.finishQuery()
	}
}

// finishQuery ends a query-replace run and reports how many replacements it made.
func (b *Bar) finishQuery() {
	if b.qr == nil {
		return
	}
	b.status = fmt.Sprintf("Replaced %d occurrence(s)", b.qr.count)
	b.qr = nil
	b.clearSelection()
}

// abortQuery ends a query-replace run on an error and shows the error.
func (b *Bar) abortQuery(err error) {
	b.qr = nil
	b.status = err.Error()
	b.clearSelection()
}

// advancePos returns the position just after text inserted at start.
func advancePos(start buffer.Pos, text string) buffer.Pos {
	lines := strings.Split(text, "\n")
	if len(lines) == 1 {
		return buffer.Pos{Line: start.Line, Col: start.Col + utf8.RuneCountInString(text)}
	}
	last := lines[len(lines)-1]
	return buffer.Pos{Line: start.Line + len(lines) - 1, Col: utf8.RuneCountInString(last)}
}

// styles are the grid styles of the bar, built from the chrome styles.
type styles struct {
	bar, label, active, field grid.Style
}

// newStyles derives the bar styles: the menu colors for the bar and its labels,
// the editor colors for the fields, and the selected overlay color for the label
// of the focused row.
func newStyles(st style.Styles) styles {
	return styles{
		bar:    style.Cell(st.Menu),
		label:  style.Cell(st.Menu),
		active: st.Overlay.Selected,
		field:  style.Cell(st.Editor),
	}
}
