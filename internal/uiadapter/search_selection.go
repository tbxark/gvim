package uiadapter

import "github.com/tbxark/gvim/buffer"

// SelectionOrWord is the text that find_selection_next and find_selection_previous
// search for, and where it lies. The find bar reads it through the ui port.
func (a *Editor) SelectionOrWord() (text string, from, to buffer.Pos, ok bool, err error) {
	return a.ed.SelectionOrWord()
}
