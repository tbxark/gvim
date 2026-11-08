package document

import (
	"errors"
)

// ErrReadOnly is returned by an action that would change a read-only document,
// and by Save, which would change its file. The status shows "read-only".
var ErrReadOnly = errors.New("read-only")

// textChanging lists the actions that change the text of a document. A read-only
// document refuses each of them with ErrReadOnly before the action runs. Save is
// refused by Editor.Save, and save_as is allowed, because it writes a new file.
// TestEveryActionLeavesReadOnlyTextAlone fails for a text-changing action that is
// missing from this list.
var textChanging = map[Action]bool{
	"insert_char":              true,
	"insert_newline":           true,
	"insert_tab":               true,
	"dedent_selection":         true,
	"duplicate_line":           true,
	"delete_line":              true,
	"open_line":                true,
	"to_upper_case":            true,
	"to_lower_case":            true,
	"transpose_chars":          true,
	"toggle_case":              true,
	"toggle_comment":           true,
	"delete_backward":          true,
	"delete_forward":           true,
	"delete_word_backward":     true,
	"delete_word_forward":      true,
	"delete_to_line_start":     true,
	"delete_to_line_end":       true,
	"move_line_up":             true,
	"move_line_down":           true,
	"cut":                      true,
	"paste":                    true,
	"undo":                     true,
	"redo":                     true,
	"reload_with_encoding":     true,
	"dabbrev_expand":           true,
	"shell_command_replace":    true,
	"sort_lines":               true,
	"duplicate_line_above":     true,
	"trim_trailing_whitespace": true,
	"ensure_final_newline":     true,
	"revert":                   true,
	"format_buffer":            true,
}

// ReadOnly reports whether the document refuses changes to its text and to its
// file. Movement, selection, copy, save_as and the encoding actions still work.
func (d *Document) ReadOnly() bool {
	return d.readOnly
}
