package editor

import (
	"errors"
	"slices"
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
)

// selectedText returns the text of the selection of d, or "" when nothing is selected.
func selectedText(d *Document) string {
	if !d.HasSelection() {
		return ""
	}
	from, to := d.Sel.Ordered()
	return d.Buffer.Range(from, to)
}

// newActions are the actions the editor text, document and tab commands add.
var newActions = []Action{
	"sort_lines", "expand_selection", "duplicate_line_above", "trim_trailing_whitespace",
	"ensure_final_newline", "copy_with_theme", "copy_file_path", "copy_relative_file_path",
	"revert", "reset_buffer_settings", "clear_search", "find_in_selection",
	"switch_to_previous_tab", "switch_to_tab_by_name", "format_buffer", "scan_line_index",
}

func TestNewActionsAreRegistered(t *testing.T) {
	t.Parallel()
	for _, name := range newActions {
		if !slices.Contains(Actions(), name) {
			t.Errorf("%s is not registered", name)
			continue
		}
		e := docWith(Options{}, "b\na")
		if r := e.Do(name, nil); errors.Is(r.Err, ErrUnknownAction) {
			t.Errorf("%s returned ErrUnknownAction", name)
		}
	}
}

func TestSortLines(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		text     string
		at       placed
		wantText string
		wantSel  *buffer.Selection
		wantErr  error
		readOnly bool
	}{
		{
			name:     "selected lines are sorted",
			text:     "c\nb\na\nd",
			at:       placed{sel: &buffer.Selection{Anchor: buffer.Pos{}, Head: buffer.Pos{Line: 2, Col: 1}}},
			wantText: "a\nb\nc\nd",
			wantSel:  &buffer.Selection{Anchor: buffer.Pos{}, Head: buffer.Pos{Line: 2, Col: 1}},
		},
		{
			name:     "a selection ending at column 0 leaves that line out",
			text:     "c\nb\na\nd",
			at:       placed{sel: &buffer.Selection{Anchor: buffer.Pos{}, Head: buffer.Pos{Line: 3}}},
			wantText: "a\nb\nc\nd",
			wantSel:  &buffer.Selection{Anchor: buffer.Pos{}, Head: buffer.Pos{Line: 2, Col: 1}},
		},
		{
			name:     "without a selection the whole document is sorted and keeps its final newline",
			text:     "c\nb\na\n",
			at:       placed{caret: buffer.Pos{}},
			wantText: "a\nb\nc\n",
		},
		{
			name:     "sorting is case-sensitive, uppercase first",
			text:     "b\nB\na\nA",
			at:       placed{caret: buffer.Pos{}},
			wantText: "A\nB\na\nb",
		},
		{
			name:     "sorted lines are left alone",
			text:     "a\nb",
			at:       placed{caret: buffer.Pos{}},
			wantText: "a\nb",
		},
		{
			name:     "a single line is left alone",
			text:     "a",
			at:       placed{caret: buffer.Pos{}},
			wantText: "a",
		},
		{
			name:     "a read-only document is refused",
			text:     "c\nb",
			at:       placed{caret: buffer.Pos{}},
			wantText: "c\nb",
			wantErr:  ErrReadOnly,
			readOnly: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := docWith(Options{}, tt.text)
			tt.at.apply(e)
			if tt.readOnly {
				run(t, e, "toggle_read_only")
			}
			r := e.Do("sort_lines", nil)
			if !errors.Is(r.Err, tt.wantErr) {
				t.Fatalf("sort_lines err = %v, want %v", r.Err, tt.wantErr)
			}
			d := e.Active()
			if got := d.Buffer.Text(); got != tt.wantText {
				t.Fatalf("text = %q, want %q", got, tt.wantText)
			}
			if !selectionEqual(d.Sel, tt.wantSel) {
				t.Fatalf("selection = %+v, want %+v", d.Sel, tt.wantSel)
			}
			if tt.wantText == tt.text && d.Modified() {
				t.Fatal("an unchanged text left the document modified")
			}
		})
	}
}

func TestSortLinesIsOneUndoStep(t *testing.T) {
	t.Parallel()
	e := docWith(Options{}, "c\nb\na")
	run(t, e, "sort_lines")
	if got := e.Active().Buffer.Text(); got != "a\nb\nc" {
		t.Fatalf("after sort text = %q", got)
	}
	run(t, e, "undo")
	if got := e.Active().Buffer.Text(); got != "c\nb\na" {
		t.Fatalf("after undo text = %q, want the original", got)
	}
}

func TestExpandSelectionGrowsWordLineThenBuffer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		text  string
		caret buffer.Pos
		want  []string // the selected text after each press
	}{
		{
			name:  "word, then line, then whole buffer, then no further change",
			text:  "foo bar\nbaz",
			caret: buffer.Pos{Line: 0, Col: 1},
			want:  []string{"foo", "foo bar\n", "foo bar\nbaz", "foo bar\nbaz"},
		},
		{
			name:  "a caret just after a word selects that word",
			text:  "one two",
			caret: buffer.Pos{Line: 0, Col: 3},
			want:  []string{"one", "one two", "one two"},
		},
		{
			name:  "a caret on a blank with no word selects the line",
			text:  "a  b\nx",
			caret: buffer.Pos{Line: 0, Col: 2},
			want:  []string{"a  b\n", "a  b\nx"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := docWith(Options{}, tt.text)
			e.SetCursor(tt.caret, false)
			for i, want := range tt.want {
				run(t, e, "expand_selection")
				if got := selectedText(e.Active()); got != want {
					t.Fatalf("press %d selected %q, want %q", i+1, got, want)
				}
			}
		})
	}
}

func TestDuplicateLineAbove(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		text       string
		at         placed
		wantText   string
		wantCursor buffer.Pos
		wantSel    *buffer.Selection
	}{
		{
			name:       "the caret line is copied above and the caret stays on the copy",
			text:       "a\nb\nc",
			at:         placed{caret: buffer.Pos{Line: 1, Col: 1}},
			wantText:   "a\nb\nb\nc",
			wantCursor: buffer.Pos{Line: 1, Col: 1},
		},
		{
			name:       "the first line",
			text:       "a\nb",
			at:         placed{caret: buffer.Pos{}},
			wantText:   "a\na\nb",
			wantCursor: buffer.Pos{},
		},
		{
			name:       "the last line without a newline",
			text:       "a\nb",
			at:         placed{caret: buffer.Pos{Line: 1}},
			wantText:   "a\nb\nb",
			wantCursor: buffer.Pos{Line: 1},
		},
		{
			name:       "a selection copies its whole lines as one block",
			text:       "a\nb\nc",
			at:         placed{sel: &buffer.Selection{Anchor: buffer.Pos{Line: 1}, Head: buffer.Pos{Line: 2, Col: 1}}},
			wantText:   "a\nb\nc\nb\nc",
			wantCursor: buffer.Pos{Line: 2, Col: 1},
			wantSel:    &buffer.Selection{Anchor: buffer.Pos{Line: 1}, Head: buffer.Pos{Line: 2, Col: 1}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := docWith(Options{}, tt.text)
			tt.at.apply(e)
			run(t, e, "duplicate_line_above")
			d := e.Active()
			if got := d.Buffer.Text(); got != tt.wantText {
				t.Fatalf("text = %q, want %q", got, tt.wantText)
			}
			if d.Cursor != tt.wantCursor {
				t.Fatalf("cursor = %+v, want %+v", d.Cursor, tt.wantCursor)
			}
			if !selectionEqual(d.Sel, tt.wantSel) {
				t.Fatalf("selection = %+v, want %+v", d.Sel, tt.wantSel)
			}
		})
	}
}

func TestDuplicateLineAboveIsOneUndoStep(t *testing.T) {
	t.Parallel()
	e := docWith(Options{}, "a\nb")
	e.SetCursor(buffer.Pos{Line: 1}, false)
	run(t, e, "duplicate_line_above", "undo")
	if got := e.Active().Buffer.Text(); got != "a\nb" {
		t.Fatalf("after undo text = %q, want the original", got)
	}
}

func TestTrimTrailingWhitespace(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		text       string
		caret      buffer.Pos
		wantText   string
		wantCursor buffer.Pos
		wantStatus string
	}{
		{
			name:       "trailing spaces and tabs go from every line",
			text:       "a  \nb\t\n c \t",
			wantText:   "a\nb\n c",
			wantStatus: "Trimmed trailing whitespace",
		},
		{
			name:       "text without trailing blanks is unchanged",
			text:       "a\n b",
			wantText:   "a\n b",
			wantStatus: "No trailing whitespace to remove",
		},
		{
			name:       "a caret past the trimmed end moves back into the text",
			text:       "a  \nb",
			caret:      buffer.Pos{Line: 0, Col: 3},
			wantText:   "a\nb",
			wantCursor: buffer.Pos{Line: 0, Col: 1},
			wantStatus: "Trimmed trailing whitespace",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := docWith(Options{}, tt.text)
			e.SetCursor(tt.caret, false)
			run(t, e, "trim_trailing_whitespace")
			d := e.Active()
			if got := d.Buffer.Text(); got != tt.wantText {
				t.Fatalf("text = %q, want %q", got, tt.wantText)
			}
			if tt.wantCursor != (buffer.Pos{}) && d.Cursor != tt.wantCursor {
				t.Fatalf("cursor = %+v, want %+v", d.Cursor, tt.wantCursor)
			}
			if e.Status() != tt.wantStatus {
				t.Fatalf("status = %q, want %q", e.Status(), tt.wantStatus)
			}
		})
	}
}

func TestTrimTrailingWhitespaceIsOneUndoStep(t *testing.T) {
	t.Parallel()
	e := docWith(Options{}, "a  \nb  ")
	run(t, e, "trim_trailing_whitespace", "undo")
	if got := e.Active().Buffer.Text(); got != "a  \nb  " {
		t.Fatalf("after undo text = %q, want the original", got)
	}
}

func TestEnsureFinalNewline(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		text       string
		wantText   string
		wantStatus string
	}{
		{name: "a missing break is added", text: "a\nb", wantText: "a\nb\n", wantStatus: "Added final newline"},
		{name: "a text that ends with a break is unchanged", text: "a\n", wantText: "a\n", wantStatus: "File already ends with newline"},
		{name: "an empty text stays empty", text: "", wantText: "", wantStatus: "File already ends with newline"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := docWith(Options{}, tt.text)
			run(t, e, "ensure_final_newline")
			if got := e.Active().Buffer.Text(); got != tt.wantText {
				t.Fatalf("text = %q, want %q", got, tt.wantText)
			}
			if e.Status() != tt.wantStatus {
				t.Fatalf("status = %q, want %q", e.Status(), tt.wantStatus)
			}
		})
	}
}

func TestEnsureFinalNewlineIsOneUndoStep(t *testing.T) {
	t.Parallel()
	e := docWith(Options{}, "a")
	run(t, e, "ensure_final_newline", "undo")
	if got := e.Active().Buffer.Text(); got != "a" {
		t.Fatalf("after undo text = %q, want the original", got)
	}
}

// TestNewTextChangesRefuseReadOnly runs each new text-changing action on a read-only
// document, with and without a selection. The text must not change.
func TestNewTextChangesRefuseReadOnly(t *testing.T) {
	t.Parallel()
	actions := []Action{"sort_lines", "duplicate_line_above", "trim_trailing_whitespace", "ensure_final_newline"}
	for _, a := range actions {
		t.Run(string(a), func(t *testing.T) {
			t.Parallel()
			for _, selected := range []bool{false, true} {
				e := docWith(Options{}, "b  \na")
				if selected {
					e.SelectRange(buffer.Pos{}, buffer.Pos{Line: 1, Col: 1})
				}
				run(t, e, "toggle_read_only")
				if r := e.Do(a, nil); !errors.Is(r.Err, ErrReadOnly) {
					t.Fatalf("%s (selected %t) err = %v, want ErrReadOnly", a, selected, r.Err)
				}
				if got := e.Active().Buffer.Text(); got != "b  \na" {
					t.Fatalf("%s (selected %t) changed a read-only text to %q", a, selected, got)
				}
			}
		})
	}
}

func TestCopyWithThemeCopiesPlainSelection(t *testing.T) {
	t.Parallel()
	e := docWith(Options{}, "hello world")
	e.SelectRange(buffer.Pos{}, buffer.Pos{Col: 5})
	run(t, e, "copy_with_theme")
	if e.clipboard != "hello" {
		t.Fatalf("clipboard = %q, want %q", e.clipboard, "hello")
	}
	if want := "Copied as plain text"; e.Status() != want {
		t.Fatalf("status = %q, want %q", e.Status(), want)
	}
	if got := e.Active().Buffer.Text(); got != "hello world" {
		t.Fatalf("copy changed the text to %q", got)
	}
}

func TestCopyWithThemeWithoutSelectionFails(t *testing.T) {
	t.Parallel()
	e := docWith(Options{}, "hello")
	e.clipboard = "keep"
	if r := e.Do("copy_with_theme", nil); !errors.Is(r.Err, ErrNothingSelected) {
		t.Fatalf("err = %v, want ErrNothingSelected", r.Err)
	}
	if e.clipboard != "keep" {
		t.Fatalf("clipboard = %q, want it unchanged", e.clipboard)
	}
}
