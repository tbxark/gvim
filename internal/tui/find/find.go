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
// *Bar implements overlay.Overlay directly, so the root stacks it without an
// adapter.
package find

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/overlay"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/search"
)

// The bar is an overlay of the root's stack.
var (
	_ overlay.Overlay = (*Bar)(nil)
	_ overlay.Caret   = (*Bar)(nil)
)

// ExclusiveOverlay marks the find bar as a transient interaction: opening it closes
// the other exclusive overlays (the palette, the browser, a picker list). See
// overlay.Exclusive.
func (*Bar) ExclusiveOverlay() {}

// Labels of the bar. The search and replace prompts are the legacy texts.
const (
	searchLabel  = "Search:"
	replaceLabel = "Replace:"
	labelGap     = 3 // blank cells between two option labels
	queryPrompt  = "Replace? (y)es (n)o (a)ll (c)ancel"
	queryWrapped = "[Wrapped] " + queryPrompt
	queryEmpty   = "Query replace: empty search query."
)

// The option toggles, in the order the options row shows them.
const (
	optCase = iota
	optWord
	optRegex
	optConfirm // ask before each replacement; not part of the search query
	numOptions
)

// optionLabels are the toggle labels, and optionNotes the status messages that
// report a change, indexed like the options.
var (
	optionLabels = [numOptions]string{"Case Sensitive (Alt+C)", "Whole Word (Alt+W)", "Regex (Alt+R)", "Confirm Each (Alt+I)"}
	optionNotes  = [numOptions]string{"Case-sensitive search", "Whole-word search", "Regex search", "Confirm each"}
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
	// Text returns the text from start to end.
	Text(start, end buffer.Pos) string
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

// Streamer is the optional part of an Editor whose active document is too large to
// list all its matches at once (a document in large file mode). While Streaming
// reports true, the bar does not call Find, ReplaceAll or Text on it: it asks Next
// for the match to go to, and the root reports the progress of the scan that
// counts the matches through SetStream.
type Streamer interface {
	// Streaming reports whether the active document is searched in a stream.
	Streaming() bool
	// Next returns the first match that starts at or after from when forward is
	// set, and the last match that starts before from otherwise. Both wrap around the
	// document. found is false when there is no match, and the error says so
	// when the search was cut short.
	Next(q search.Query, from buffer.Pos, forward bool) (m search.Match, found bool, err error)
}

// Bar is the find bar. It is an overlay: while it is open, the root sends it
// the key presses, and it is drawn over the frame. Update returns the bar
// itself. Active turns false once the bar is closed.
type Bar struct {
	ed     Editor
	styles styles

	search textinput.Model
	repl   textinput.Model

	opts        [numOptions]bool // the toggles: case, whole word, regex, confirm each
	replaceOpen bool             // the replacement row is shown
	inReplace   bool             // the keys go to the replacement row

	anchor   buffer.Pos // the caret when the bar opened; the first match is at or after it
	hasSel   bool       // the bar's selection: a match it selected, or the selection it found
	selStart buffer.Pos
	selEnd   buffer.Pos

	orig origin // the caret and selection when the bar opened; Esc puts them back

	matched   bool   // the current selection is a match of the current search
	moved     bool   // the bar moved the editor selection; Esc puts it back
	committed bool   // Enter kept the search: the matches stay highlighted and F3 goes on
	prefilled bool   // the search text was filled in and is "selected", so typing replaces it
	armed     bool   // Ctrl+Alt+R opened the bar: Enter in the replacement row asks about each match
	held      string // the search text of the bar this one inherited from

	// multi is search text with line breaks, which the single-line field shows
	// with spaces. It is the query text while the field still holds multiShown.
	multi, multiShown string

	searchHist, replHist *history

	stream *StreamInfo // the last progress of the scan that counts the matches of a large document

	status string    // the match count, or the error of the pattern, at the right of the search row
	notice string    // the last option change; shown until the next key
	qr     *queryRun // non-nil while query-replace asks about each match
	closed bool
}

// origin is the caret and selection of the document at one moment.
type origin struct {
	cursor     buffer.Pos
	hasSel     bool
	start, end buffer.Pos
}

// queryRun is one query-replace session. from is where the next match is
// looked for, start is where the run began, and count is the number of
// replacements made so far. Once the run has gone past the end of the document
// it wraps to the top and wrapped is set; it stops again at start.
type queryRun struct {
	from    buffer.Pos
	start   buffer.Pos
	wrapped bool
	count   int
}

// Open returns a find bar for the document of ed, anchored at its caret. With
// withReplace the replacement row is shown too. The search field is focused
// either way, as in Fresh, so the search is typed first and Enter moves on to
// the replacement.
func Open(ed Editor, st style.Styles, withReplace bool) *Bar {
	b := &Bar{
		ed:     ed,
		styles: newStyles(st),
		search: newField(),
		repl:   newField(),
		anchor: ed.Cursor(),

		searchHist: &history{},
		replHist:   &history{},
	}
	if start, end, ok := ed.Selection(); ok {
		b.hasSel, b.selStart, b.selEnd = true, start, end
	}
	b.orig = b.snapshot()
	b.focusRow()
	b.replaceOpen = withReplace
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
	b.matched, b.moved, b.committed, b.prefilled, b.armed = false, false, false, false, false
	b.anchor = b.ed.Cursor()
	b.hasSel = false
	if start, end, ok := b.ed.Selection(); ok {
		b.anchor = start
		b.hasSel, b.selStart, b.selEnd = true, start, end
	}
	b.orig = b.snapshot()
	b.focusRow()
}

// snapshot returns the caret and selection of the document now.
func (b *Bar) snapshot() origin {
	o := origin{cursor: b.ed.Cursor()}
	o.start, o.end, o.hasSel = b.ed.Selection()
	return o
}

// SetOptions sets the toggles: case sensitive, whole word, regex and confirm each.
func (b *Bar) SetOptions(caseSensitive, wholeWord, regex, confirmEach bool) {
	b.opts = [numOptions]bool{caseSensitive, wholeWord, regex, confirmEach}
}

// Inherit takes the toggles and the two histories of prev, the bar that was last
// open, so they outlive one Ctrl+F. The search text of prev is kept to tell, in
// Prefill, a selection that is its current match.
func (b *Bar) Inherit(prev *Bar) {
	b.opts = prev.opts
	b.searchHist, b.replHist = prev.searchHist, prev.replHist
	b.held = prev.query().Text
}

// State is what outlives the process: the two histories, oldest first, and the
// option toggles.
type State struct {
	Find, Replace                  []string
	Case, Word, Regex, ConfirmEach bool
}

// State returns the histories and toggles of the bar.
func (b *Bar) State() State {
	return State{
		Find:        slices.Clone(b.searchHist.items),
		Replace:     slices.Clone(b.replHist.items),
		Case:        b.opts[optCase],
		Word:        b.opts[optWord],
		Regex:       b.opts[optRegex],
		ConfirmEach: b.opts[optConfirm],
	}
}

// Load sets the histories and toggles from st, which State returned in an earlier
// run. The entries are kept as they are, up to the history cap.
func (b *Bar) Load(st State) {
	b.SetOptions(st.Case, st.Word, st.Regex, st.ConfirmEach)
	b.searchHist.load(st.Find)
	b.replHist.load(st.Replace)
}

// Prefill fills the search text as Fresh does on Ctrl+F: the single-line
// selection, or the search that found it when the selection is its current match,
// or else the last search of the history. The text counts as selected, so typing
// replaces it. It does not move the caret.
func (b *Bar) Prefill() {
	text := b.searchHist.last()
	if start, end, ok := b.ed.Selection(); ok && start.Line == end.Line {
		if sel := b.ed.Text(start, end); sel != "" {
			text = sel
			if b.held != "" && b.isMatch(b.held, start, end) {
				text = b.held
			}
		}
	}
	if text == "" {
		return
	}
	b.search.SetValue(text)
	b.prefilled = true
	b.refresh()
}

// isMatch reports whether start..end is a match of text under the toggles.
func (b *Bar) isMatch(text string, start, end buffer.Pos) bool {
	q := b.query()
	q.Text = text
	if st, ok := b.streamer(); ok {
		m, found, err := st.Next(q, start, true)
		return err == nil && found && m == search.Match{Start: start, End: end}
	}
	matches, err := b.ed.Find(q)
	if err != nil {
		return false
	}
	return slices.Contains(matches, search.Match{Start: start, End: end})
}

// SetSearch makes text the search, as find_selection_next does: the toggles stay,
// the text is kept in the history, and the search is committed so its matches show
// and F3 goes on from it.
func (b *Bar) SetSearch(text string) {
	b.setSearchText(text)
	b.prefilled, b.matched = false, false
	b.searchHist.push(text)
	b.committed = true
	b.refresh()
}

// setSearchText puts text in the search field. The field is one line, so line
// breaks show as spaces, but the search keeps the text as it is until the field
// is edited.
func (b *Bar) setSearchText(text string) {
	b.search.SetValue(text)
	b.multi, b.multiShown = "", ""
	if strings.Contains(text, "\n") {
		b.multi, b.multiShown = text, b.search.Value()
	}
}

// Committed reports whether Enter, or SetSearch, kept the search after the bar
// closed. The root shows the matches and lets F3 go on while it holds.
func (b *Bar) Committed() bool { return b.committed }

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

// Rows reports the rows the bar takes at the bottom of a frame h rows high: the card
// with its title border, the options row, the search row and, when it is shown, the
// replacement row, plus the bottom border with the hints. It is zero when the frame has
// no room for that card above the menu band and the tab strip, so the layout reserves
// nothing for a bar that would not be painted.
func (b *Bar) Rows(h int) int {
	if rowsFor(h, b.replaceOpen).top < 0 {
		return 0
	}
	if b.replaceOpen {
		return 5
	}
	return 4
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
		b.armed = true
		b.inReplace = false
		b.focusRow()
		return
	}
	b.armed = false
	b.searchHist.push(b.query().Text)
	b.replHist.push(b.repl.Value())
	b.qr = &queryRun{from: b.anchor, start: b.anchor}
	b.queryStep()
}

// Update handles one message and returns the bar itself, so the root can stack it as an
// overlay. Key presses edit the bar. A paste, and the clipboard text that Ctrl+V asked
// for, go to the focused field, except in query-replace. The returned command is the
// clipboard read of Ctrl+V, and nil otherwise, because the bar answers every change
// through Editor.
func (b *Bar) Update(msg tea.Msg) (overlay.Overlay, tea.Cmd) {
	kp, ok := msg.(tea.KeyPressMsg)
	if !ok {
		if b.qr == nil {
			b.edit(msg)
		}
		return b, nil
	}
	if b.qr != nil {
		b.queryKey(kp)
		return b, nil
	}
	return b, b.keyPress(kp)
}

// keyPress runs one key press outside query-replace. Esc closes the bar and puts
// the caret and selection back. F3 and Shift+F3 step to the next and previous
// match, Tab shows the replacement row and switches rows, and Up and Down walk
// the history of the focused field. Enter (and Ctrl+F) in the search row jumps to
// the match and closes the bar, keeping the search; with the replacement row
// shown it moves to that row instead. Enter in the replacement row replaces all,
// or asks about each match under Confirm Each, and Alt+Enter does the same.
// Ctrl+Alt+R starts query-replace. Alt+C, Alt+W, Alt+R and Alt+I toggle the
// options. Other keys edit the focused field.
func (b *Bar) keyPress(kp tea.KeyPressMsg) (cmd tea.Cmd) {
	name := tea.Key(kp).Keystroke()
	if _, toggle := toggleOf(name); !toggle {
		b.notice = ""
	}
	switch name {
	case "esc":
		b.Cancel()
	case "up", "down":
		b.walkHistory(name == "up")
	case "ctrl+alt+r":
		b.ShowReplace()
		b.StartQueryReplace()
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
	case "enter", "ctrl+f":
		b.enter()
	case "alt+enter":
		if b.inReplace {
			b.enter()
		} else {
			b.step(true)
		}
	case "ctrl+delete":
		// Fresh's prompt context maps Ctrl+Delete (and Emacs Alt+D) to
		// prompt_delete_word_forward. The root overlay router forwards that as the
		// synthetic Ctrl+Delete, which the text input does not bind; translate it to
		// the widget's own DeleteWordForward chord. The focused field keeps the keys.
		cmd = b.edit(tea.KeyPressMsg{Code: tea.KeyDelete, Mod: tea.ModAlt})
	case "ctrl+backspace":
		// A direct Ctrl+Backspace is the same Fresh action; the router normally maps
		// it to Ctrl+W, but keep the bar correct if a caller delivers it directly.
		cmd = b.edit(tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl})
	default:
		if i, ok := toggleOf(name); ok {
			b.toggle(i)
			return
		}
		cmd = b.edit(kp)
	}
	return cmd
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
	case "alt+i":
		return optConfirm, true
	}
	return 0, false
}

// enter confirms the focused row. The search row jumps to the match and closes the
// bar, or hands the keys to the replacement row when that is shown. The
// replacement row replaces every match, or asks about each when Confirm Each is on
// or Ctrl+Alt+R opened the bar.
func (b *Bar) enter() {
	b.searchHist.push(b.query().Text)
	if !b.inReplace {
		if b.replaceOpen {
			b.inReplace = true
			b.focusRow()
			return
		}
		b.commit()
		return
	}
	b.replHist.push(b.repl.Value())
	if b.opts[optConfirm] || b.armed {
		b.StartQueryReplace()
		return
	}
	b.replaceAll()
}

// commit selects the first match at or after the anchor, unless F3 already
// selected one, closes the bar and keeps the search for the highlights and F3. An
// empty search only closes the bar, and a search with no match or a bad pattern
// leaves it open with the reason.
func (b *Bar) commit() {
	if b.search.Value() == "" {
		b.closed = true
		return
	}
	if st, ok := b.streamer(); ok {
		b.commitStream(st)
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
	if !b.matched {
		idx, _ := search.Next(matches, b.anchor, true)
		b.selectMatch(matches[idx])
		b.status = search.StatusText(idx, len(matches))
	}
	b.committed = true
	b.closed = true
}

// Cancel closes the bar as Esc does: it ends a running query-replace and puts the
// caret and selection back where they were when the bar opened, if the bar moved
// them to a match. The root calls it when it pops the bar on Esc.
// Dismiss cancels the bar, as Esc does. The root calls it when it pops an
// overlay on Esc.
func (b *Bar) Dismiss() { b.Cancel() }

func (b *Bar) Cancel() {
	if b.qr != nil {
		b.finishQuery()
	}
	b.restore()
	b.closed = true
}

// restore puts the caret and selection back where they were when the bar opened,
// when the bar moved them to a match.
func (b *Bar) restore() {
	if !b.moved {
		return
	}
	b.moved, b.matched = false, false
	if b.orig.hasSel {
		b.ed.Select(search.Match{Start: b.orig.start, End: b.orig.end})
		return
	}
	b.ed.Place(b.orig.cursor)
}

// walkHistory replaces the focused field with an older (up) or newer entry of its
// history.
func (b *Bar) walkHistory(up bool) {
	f, h := &b.search, b.searchHist
	if b.inReplace {
		f, h = &b.repl, b.replHist
	}
	var text string
	var ok bool
	if up {
		text, ok = h.prev(f.Value())
	} else {
		text, ok = h.next()
	}
	if !ok {
		return
	}
	if b.inReplace {
		f.SetValue(text)
	} else {
		b.setSearchText(text)
	}
	b.prefilled = false
	if !b.inReplace {
		b.matched = false
		b.refresh()
	}
}

// edit gives a message, a key press or a paste, to the focused field. A change to the
// search text searches again and updates the match status; the caret and selection
// stay. A change to the replacement text changes no match. Line breaks of a paste become
// spaces, as the field makes them. Filled-in search text counts as selected: a typed
// character or a paste replaces it, and Backspace or Delete clears it. It returns the
// clipboard read that Ctrl+V starts.
func (b *Bar) edit(msg tea.Msg) tea.Cmd {
	if p, ok := msg.(tea.PasteMsg); ok {
		p.Content = strings.ReplaceAll(strings.ReplaceAll(p.Content, "\r\n", "\n"), "\r", "\n")
		msg = p
	}
	var cmd tea.Cmd
	if b.inReplace {
		b.repl, cmd = b.repl.Update(msg)
	} else {
		before := b.search.Value()
		if b.prefilled {
			switch m := msg.(type) {
			case tea.KeyPressMsg:
				b.prefilled = false
				name := tea.Key(m).Keystroke()
				switch {
				case name == "backspace" || name == "delete":
					b.search.SetValue("")
					msg = nil
				case m.Text != "" || name == "ctrl+v":
					b.search.SetValue("")
				}
			case tea.PasteMsg:
				b.prefilled = false
				b.search.SetValue("")
			}
		}
		if msg != nil {
			b.search, cmd = b.search.Update(msg)
		}
		if b.search.Value() != before {
			b.multi, b.multiShown = "", ""
			b.matched = false
			b.refresh()
		}
	}
	// Any other command is the blink of the caret, which the bar does not draw.
	if kp, ok := msg.(tea.KeyPressMsg); ok && tea.Key(kp).Keystroke() == "ctrl+v" {
		return cmd
	}
	return nil
}

// toggle flips option i, searches again under the new option, and reports the
// new state in place of the match status.
func (b *Bar) toggle(i int) {
	b.opts[i] = !b.opts[i]
	b.matched = false
	b.refresh()
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
	text := b.search.Value()
	if b.multi != "" && text == b.multiShown {
		text = b.multi
	}
	return search.Query{
		Text:          text,
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
	b.setSearchText("")
	b.committed = false
	b.refresh()
}

// refresh recomputes the match status for the search text, which counts the
// matches and numbers the first one at or after the anchor. It moves neither the
// caret nor the selection. An empty search text clears the status, and an invalid
// pattern shows its error.
func (b *Bar) refresh() {
	if b.qr != nil {
		return
	}
	if b.search.Value() == "" {
		b.status = ""
		return
	}
	if _, ok := b.streamer(); ok {
		// The root starts a scan for the new query and reports its progress.
		b.stream = nil
		b.status = b.streamStatus()
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
	b.status = search.StatusText(idx, len(matches))
}

// step selects the match after the selection end when forward is set, or the
// one before the selection start otherwise. Both wrap around the document.
func (b *Bar) step(forward bool) {
	if b.search.Value() == "" {
		return
	}
	if st, ok := b.streamer(); ok {
		b.stepStream(st, forward)
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
		from := end
		if b.matched && start == end {
			// The selected match is empty, so the one that starts here is the
			// current one.
			from.Col++
		}
		idx, _ = search.Next(matches, from, true)
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
	b.matched, b.moved = true, true
	b.hasSel = true
	b.selStart, b.selEnd = m.Start, m.End
}

// clearSelection drops the selection in the editor and in the bar. The caret
// stays where it is.
func (b *Bar) clearSelection() {
	b.hasSel = false
	b.matched, b.moved = false, false
	b.ed.Place(b.ed.Cursor())
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
	if n == 0 {
		b.status = search.StatusText(0, 0)
		return
	}
	b.status = fmt.Sprintf("Replaced %d", n)
}

// queryCurrent returns the first match at or after the run's position. Past the
// last match the run wraps to the top of the document and goes on up to the place
// it began. It reports false when there is none.
func (b *Bar) queryCurrent() (search.Match, bool, error) {
	if st, ok := b.streamer(); ok {
		return b.queryCurrentStream(st)
	}
	matches, err := b.ed.Find(b.query())
	if err != nil {
		return search.Match{}, false, err
	}
	qr := b.qr
	if !qr.wrapped {
		if idx, ok := search.Next(matches, qr.from, false); ok {
			return matches[idx], true, nil
		}
		qr.wrapped = true
		qr.from = buffer.Pos{}
	}
	idx, ok := search.Next(matches, qr.from, false)
	if !ok || !matches[idx].Start.Less(qr.start) {
		return search.Match{}, false, nil
	}
	return matches[idx], true, nil
}

// pastMatch returns the position a run looks from once it has dealt with m, which
// ends at end. An empty match stands before a character, so the run goes one
// column further, or it would find the same match again.
func pastMatch(m search.Match, end buffer.Pos) buffer.Pos {
	if m.Start == m.End {
		end.Col++
	}
	return end
}

// shiftPos maps p, a position at or after oldEnd, to its place once the text
// that ended at oldEnd has been replaced by text ending at newEnd. A p inside the
// replaced text goes to newEnd.
func shiftPos(p, oldEnd, newEnd buffer.Pos) buffer.Pos {
	switch {
	case p.Less(oldEnd):
		return newEnd
	case p.Line == oldEnd.Line:
		return buffer.Pos{Line: newEnd.Line, Col: newEnd.Col + p.Col - oldEnd.Col}
	}
	return buffer.Pos{Line: p.Line + newEnd.Line - oldEnd.Line, Col: p.Col}
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
	if b.qr.wrapped {
		b.status = queryWrapped
	}
}

// queryReplace replaces the match cur and moves the run past the new text.
func (b *Bar) queryReplace(cur search.Match) error {
	text, err := b.ed.Replace(b.query(), b.repl.Value(), cur)
	if err != nil {
		return err
	}
	b.qr.count++
	end := advancePos(cur.Start, text)
	if b.qr.wrapped {
		b.qr.start = shiftPos(b.qr.start, cur.End, end)
	}
	b.qr.from = pastMatch(cur, end)
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
			b.qr.from = pastMatch(cur, cur.End)
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

// styles are the styles of the bar, built from the chrome styles.
type styles struct {
	ov     style.Overlay // the card frame: border, fill, title and hints
	label  grid.Style    // the option labels
	active grid.Style    // the label of the focused row
	field  grid.Style    // the input fields
	status grid.Style    // the match status at the right of the search row
}

// newStyles derives the sheet styles from the overlay palette: the card frame, the
// muted labels, the focused label of the selected overlay, and the raised input
// fields of the overlay.
func newStyles(st style.Styles) styles {
	ov := st.Overlay
	return styles{
		ov:     ov,
		label:  ov.Muted,
		active: ov.Focus,
		field:  ov.Idle,
		status: ov.Muted,
	}
}
