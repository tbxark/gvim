package editor

import (
	"errors"
	"fmt"
	"slices"

	"github.com/tbxark/gvim/internal/buffer"
)

// Guards for a document in large file mode. The text of such a document stays on disk,
// so an action must not read or compare the whole text, and must not hand a part of it
// to the clipboard or another process when only a cut piece of it can be read. The
// helpers here are what the actions use instead of Buffer.Text and Buffer.Range. A
// document that is not large goes through the same helpers and gets what it always did.

// maxLargeCarets is the most carets that an action creates in a large document. A
// caret per line of a selection would otherwise take memory and time in proportion to
// the size of the file.
const maxLargeCarets = 10_000

// contentMark identifies the content of a document, to find out whether an action
// changed it. A document in memory is marked by its text. A large document is marked by
// the version of its buffer, because its text cannot be held: an edit that restores the
// text still counts as a change, which is the safe side.
type contentMark struct {
	text string
	ver  uint64
}

// contentMark returns the mark of the document as it is now. Marks compare with ==.
func (d *Document) contentMark() contentMark {
	if d.large != nil {
		return contentMark{ver: d.Buffer.Version()}
	}
	return contentMark{text: d.Buffer.Text()}
}

// largeError is a refusal with the words the status row shows. It matches each error
// of errs with errors.Is.
type largeError struct {
	msg  string
	errs []error
}

func (e *largeError) Error() string   { return e.msg }
func (e *largeError) Unwrap() []error { return e.errs }

// errLargeAction is the refusal of an action that cannot work on a large document. It
// matches ErrLargeFile.
func errLargeAction(action Action) error {
	return fmt.Errorf("%s: %w", action, ErrLargeFile)
}

// errSelectionTooLarge is the refusal of an action that needs the text of a range that
// is over the size a large document hands out. It matches ErrLargeFile and
// buffer.ErrOverCap.
func errSelectionTooLarge() error {
	return &largeError{
		msg:  fmt.Sprintf("selection too large for large file mode (%d MiB)", buffer.MaxMaterialize>>20),
		errs: []error{ErrLargeFile, buffer.ErrOverCap},
	}
}

// errTooManyCarets is the refusal of an action that would create more carets than a
// large document allows. It matches ErrLargeFile.
func errTooManyCarets() error {
	return &largeError{
		msg:  fmt.Sprintf("more than %d carets are not available in large file mode", maxLargeCarets),
		errs: []error{ErrLargeFile},
	}
}

// errLineCut is the refusal of an action that needs the whole text of a line that is
// longer than a large document reads. line is 0-based.
func errLineCut(line int, what string) error {
	return &largeError{
		msg:  fmt.Sprintf("line %d is longer than %d MiB: not %s", line+1, buffer.DefaultMaxLine>>20, what),
		errs: []error{ErrLargeFile, buffer.ErrLineTooLong},
	}
}

// cutLineIn returns the first line of the range from..to whose text can only be read in part,
// or -1. A line over the line cap is read to its end by Range, so only the first line of a
// source whose index is not complete is cut short, and it matters when the range takes it
// whole: when it continues past the line break, or when it reaches the end of the part of the
// line that can be read. The cost is one look per line, and none when the file has no line
// over the cap.
func (d *Document) cutLineIn(from, to buffer.Pos) int {
	if d.large == nil || !d.Index().HugeLine {
		return -1
	}
	if to.Less(from) {
		from, to = to, from
	}
	for line := from.Line; line < to.Line; line++ {
		if d.Buffer.LineUnfinished(line) {
			return line
		}
	}
	if d.Buffer.LineUnfinished(to.Line) && to.Col >= d.Buffer.LineLen(to.Line) {
		return to.Line
	}
	return -1
}

// checkLines returns the refusal of an action that reads the lines first through last
// whole, to copy them or write them back: a line of them is cut short, or together they
// are over the size a large document hands out. It looks at the length of each line in
// the index and reads none. It is nil for a document in memory. what is the verb of the
// message, such as "copied".
func (d *Document) checkLines(first, last int, what string) error {
	if d.large == nil {
		return nil
	}
	huge := d.Index().HugeLine
	total := 0
	for line := first; line <= last; line++ {
		if huge && d.Buffer.LineCut(line) {
			return errLineCut(line, what)
		}
		total += d.Buffer.LineBytes(line) + 1
		if total > buffer.MaxMaterialize {
			return errSelectionTooLarge()
		}
	}
	return nil
}

// readRange returns the text between from and to for an action that copies it, filters
// it or writes it back changed. A document in memory returns it as Buffer.Range does.
// A large document returns the text of a range up to 16 MiB. A longer range, and a
// range that takes a line that is cut short whole, are refused, so a copy or a rewrite
// never holds less text than the range names.
func (d *Document) readRange(from, to buffer.Pos) (string, error) {
	if d.large == nil {
		return d.Buffer.Range(from, to), nil
	}
	if line := d.cutLineIn(from, to); line >= 0 {
		return "", errLineCut(line, "copied")
	}
	text, err := d.Buffer.RangeChecked(from, to)
	switch {
	case errors.Is(err, buffer.ErrOverCap):
		return "", errSelectionTooLarge()
	case err != nil:
		return "", err
	}
	return text, nil
}

// planRange is readRange for the planning of an edit. Where the edit planner has no
// way to return an error, a failure is remembered in the document and the planner gets
// no text. The edit that follows is not made (see failPlan), and the action ends with
// the error.
func (d *Document) planRange(from, to buffer.Pos) string {
	text, err := d.readRange(from, to)
	d.failPlan(err)
	return text
}

// failPlan remembers err as the reason the edit being planned must not be made. A nil
// err changes nothing. The first reason is kept.
func (d *Document) failPlan(err error) {
	if err != nil && d.planErr == nil {
		d.planErr = err
	}
}

// editRefused reports whether the edits just made were not made, because planning them
// failed or because the buffer refused one of them. The reason is kept in the document
// for the end of the action (takeRefusal). The callers that make carets from the ends of
// their edits stop here: the positions of edits that were rolled back mean nothing.
func (d *Document) editRefused() bool {
	if d.large == nil {
		return false
	}
	if err := d.Buffer.Refusal(); err != nil {
		d.failPlan(err)
	}
	return d.planErr != nil
}

// cursorSnapshot is the caret state of a document, to put back when an action is refused.
type cursorSnapshot struct {
	cursor    buffer.Pos
	sel       *buffer.Selection
	extra     []Caret
	block     *blockState
	lastMulti *multiEditRecord
	want      int
}

// snapCursors copies the caret state of d.
func (d *Document) snapCursors() *cursorSnapshot {
	s := &cursorSnapshot{cursor: d.Cursor, block: d.block, lastMulti: d.lastMulti, want: d.want}
	if d.Sel != nil {
		sel := *d.Sel
		s.sel = &sel
	}
	s.extra = slices.Clone(d.extra)
	for i, x := range s.extra {
		if x.Sel != nil {
			sel := *x.Sel
			s.extra[i].Sel = &sel
		}
	}
	return s
}

// restoreCursors puts the caret state of s back into d.
func (d *Document) restoreCursors(s *cursorSnapshot) {
	d.Cursor, d.Sel, d.extra, d.block, d.lastMulti, d.want = s.cursor, s.sel, s.extra, s.block, s.lastMulti, s.want
}

// takeRefusal returns the reason the last action on d was refused, as an error with the
// words for the status row, and clears it. A planning failure comes first. It is nil
// when nothing was refused, and always nil for a document in memory.
func (d *Document) takeRefusal() error {
	err := d.planErr
	d.planErr = nil
	if refused := d.Buffer.Refusal(); err == nil {
		err = refused
	}
	if err == nil {
		return nil
	}
	return d.refusalError(err)
}

// refusalError words a refusal of the buffer for the status row. The caret line stands
// for the line that was refused when there is one caret.
func (d *Document) refusalError(err error) error {
	which := "a line"
	if len(d.extra) == 0 {
		which = fmt.Sprintf("line %d", d.Cursor.Line+1)
	}
	var msg string
	switch {
	case errors.Is(err, ErrLargeFile):
		return err
	case errors.Is(err, buffer.ErrLineTooLong):
		msg = fmt.Sprintf("%s is longer than %d MiB: not editable", which, buffer.DefaultMaxLine>>20)
	case errors.Is(err, buffer.ErrInvalidUTF8):
		msg = which + " is not valid UTF-8: not editable"
	case errors.Is(err, buffer.ErrOverlayFull):
		msg = "too many unsaved changes: save first"
	case errors.Is(err, buffer.ErrLineOverBudget):
		msg = which + " is too big to edit in large file mode"
	case errors.Is(err, buffer.ErrSourceChanged):
		msg = "file changed on disk: reload or Save As after reloading"
	case errors.Is(err, buffer.ErrOverCap):
		return errSelectionTooLarge()
	default:
		return err
	}
	return &largeError{msg: msg, errs: []error{err}}
}

// refusedInLargeMode lists the actions that need the whole text of the document and
// are refused whatever the document holds. Actions that are refused only in some
// cases check in their own handler.
var refusedInLargeMode = map[Action]bool{
	"format_buffer":            true,
	"trim_trailing_whitespace": true,
	"set_line_ending":          true,
	"find_selection_next":      true,
	"find_selection_previous":  true,
}

// frontierSensitive lists the actions that treat the last line of the document
// differently: it has no line break to cut or move past. While the index of a large
// document is being built, the last line known is not the last line, so the action
// would act on a line that has a break. They return ErrIndexing for a caret on that line.
var frontierSensitive = map[Action]bool{
	"delete_line":    true,
	"cut":            true,
	"copy":           true,
	"duplicate_line": true,
	"move_line_down": true,
}

// selectionOrLines lists the frontierSensitive actions that act on the text of a
// selection when there is one, and on the caret line only without one.
var selectionOrLines = map[Action]bool{
	"cut":  true,
	"copy": true,
}

// lineWise lists the actions that edit each line of a selection one by one. A large
// document refuses them for a selection of more than maxLargeLines lines.
var lineWise = map[Action]bool{
	"insert_tab":           true,
	"dedent_selection":     true,
	"toggle_comment":       true,
	"sort_lines":           true,
	"duplicate_line":       true,
	"duplicate_line_above": true,
	"move_line_up":         true,
	"move_line_down":       true,
	"delete_line":          true,
}

// maxLargeLines is the most lines a line-by-line action works on in a large document.
const maxLargeLines = 100_000

// refuseInLargeMode returns the refusal of action a, when d is a large document and a
// cannot work on it: the actions of refusedInLargeMode, and a line-by-line action on a
// selection of too many lines. It is nil for any other document and action.
func (d *Document) refuseInLargeMode(a Action) error {
	if d.large == nil {
		return nil
	}
	if err := d.refuseWhileRestoring(a); err != nil {
		return err
	}
	if refusedInLargeMode[a] {
		return errLargeAction(a)
	}
	if lineWise[a] && d.selectedLineCount() > maxLargeLines {
		return &largeError{
			msg:  fmt.Sprintf("%s: selection of more than %d lines is too large for large file mode", a, maxLargeLines),
			errs: []error{ErrLargeFile},
		}
	}
	return nil
}

// selectedLineCount is the number of lines the carets and selections of d touch,
// counting a line once for each caret that touches it.
func (d *Document) selectedLineCount() int {
	n := 0
	for _, c := range d.allCarets() {
		lo, hi := c.bounds()
		n += hi.Line - lo.Line + 1
	}
	return n
}

// onFrontier reports whether a caret of d is on the last line the index knows, while
// the index is incomplete.
func (d *Document) onFrontier() bool {
	if d.large == nil || d.Index().Complete {
		return false
	}
	last := d.Buffer.Lines() - 1
	if d.Cursor.Line >= last || (d.Sel != nil && d.Sel.Anchor.Line >= last) {
		return true
	}
	for _, x := range d.extra {
		if x.Cursor.Line >= last {
			return true
		}
	}
	return false
}

// joinsNextLine lists the deletes that, at the end of a line, remove its line break and
// so join the next line to it. While the index is incomplete, the next line of the last
// line known may exist, so such a delete at the end of that line waits (ErrIndexing).
var joinsNextLine = map[Action]bool{
	"delete_forward":      true,
	"delete_to_line_end":  true,
	"delete_word_forward": true,
}

// atFrontierEnd reports whether a caret of d with no selection is at the end of the last
// line the index knows, while the index is incomplete.
func (d *Document) atFrontierEnd() bool {
	if d.large == nil || d.Index().Complete {
		return false
	}
	last := d.Buffer.Lines() - 1
	for _, c := range d.allCarets() {
		if lo, hi := c.bounds(); lo == hi && lo.Line == last && lo.Col >= d.Buffer.LineLen(last) {
			return true
		}
	}
	return false
}
