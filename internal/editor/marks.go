package editor

import (
	"fmt"

	"github.com/tbxark/gvim/internal/buffer"
)

// extraState holds the state of the marks, navigation, dabbrev and tab
// actions. Editor keeps one value of it, and each action file owns its part.
type extraState struct {
	// slots are the bookmarks, keyed by the digit that names them.
	slots map[string]markSlot
	// marked is the set of tabs in mark mode, where plain moves extend the
	// selection from its anchor (see set_mark).
	marked map[*Document]bool
	// jumps is the navigation history (nav.go).
	jumps jumpHistory
	// dabbrev is the open dabbrev cycle, if any (dabbrev.go).
	dabbrev dabbrevCycle
}

// markSlot is a bookmark: the tab it was set in and the caret position there.
type markSlot struct {
	doc *Document
	pos buffer.Pos
}

// registerMarksHandlers registers the handlers of this track. It is the one
// call buildHandlers makes, and it registers the handlers of the other files.
func registerMarksHandlers(h map[Action]handler) {
	h["set_mark"] = setMark
	h["cancel_mark"] = cancelMark
	h["clear_mark"] = clearMark
	h["set_bookmark"] = setBookmark
	h["jump_to_bookmark"] = jumpToBookmark
	registerNavHandlers(h)
	registerBracketHandlers(h)
	registerDabbrevHandlers(h)
	registerFindSelHandlers(h)
	registerShellHandlers(h)
	registerTabsExtraHandlers(h)
}

// marking reports whether d is in mark mode.
func (e *Editor) marking(d *Document) bool {
	return e.extras.marked[d]
}

// setMark starts mark mode at the caret. The selection is anchored there, and
// plain moves extend it until cancel_mark or clear_mark.
func setMark(e *Editor, _ map[string]any) error {
	d := e.Active()
	d.Sel = &buffer.Selection{Anchor: d.Cursor, Head: d.Cursor}
	if e.extras.marked == nil {
		e.extras.marked = map[*Document]bool{}
	}
	e.extras.marked[d] = true
	return nil
}

// cancelMark leaves mark mode and keeps the selection. The next plain move
// drops it, as Fresh's soft exit does.
func cancelMark(e *Editor, _ map[string]any) error {
	delete(e.extras.marked, e.Active())
	return nil
}

// clearMark leaves mark mode and drops the selection at once.
func clearMark(e *Editor, _ map[string]any) error {
	d := e.Active()
	delete(e.extras.marked, d)
	d.Sel = nil
	return nil
}

// bookmarkSlot reads the "char" argument of the bookmark actions. It must be
// one digit, from 0 to 9.
func bookmarkSlot(action Action, args map[string]any) (string, error) {
	s, ok := args["char"].(string)
	if !ok {
		return "", fmt.Errorf("%s: missing string argument \"char\"", action)
	}
	if len(s) != 1 || s[0] < '0' || s[0] > '9' {
		return "", fmt.Errorf("%s: bookmark slot %q is not a digit from 0 to 9", action, s)
	}
	return s, nil
}

// setBookmark stores the caret of the active tab in a bookmark slot. A slot that
// is already set moves to the new place.
func setBookmark(e *Editor, args map[string]any) error {
	slot, err := bookmarkSlot("set_bookmark", args)
	if err != nil {
		return err
	}
	d := e.Active()
	if e.extras.slots == nil {
		e.extras.slots = map[string]markSlot{}
	}
	e.extras.slots[slot] = markSlot{doc: d, pos: d.Cursor}
	e.setStatus(fmt.Sprintf("Bookmark '%s' set", slot))
	return nil
}

// jumpToBookmark moves to a bookmark, switching tabs if needed. The place it
// leaves is recorded in the navigation history. A bookmark whose tab was closed
// is forgotten and reported as an error.
func jumpToBookmark(e *Editor, args map[string]any) error {
	slot, err := bookmarkSlot("jump_to_bookmark", args)
	if err != nil {
		return err
	}
	mark, ok := e.extras.slots[slot]
	if !ok {
		return fmt.Errorf("bookmark '%s' not set", slot)
	}
	if e.indexOfDoc(mark.doc) < 0 {
		delete(e.extras.slots, slot)
		return fmt.Errorf("bookmark '%s': buffer no longer exists", slot)
	}
	e.pushJump()
	e.goToJump(jumpEntry(mark))
	e.setStatus(fmt.Sprintf("Jumped to bookmark '%s'", slot))
	return nil
}

// indexOfDoc returns the index of d in the open tabs, or -1.
func (e *Editor) indexOfDoc(d *Document) int {
	for i, x := range e.docs {
		if x == d {
			return i
		}
	}
	return -1
}
