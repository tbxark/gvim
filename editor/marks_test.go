package editor

import (
	"slices"
	"testing"

	"github.com/tbxark/gvim/buffer"
)

// marksCaret puts the caret of the active document at line, col.
func marksCaret(e *Editor, line, col int) {
	e.Active().Cursor = buffer.Pos{Line: line, Col: col}
}

// marksSelected returns the selected text of the active document, or "".
func marksSelected(e *Editor) string {
	d := e.Active()
	if !d.HasSelection() {
		return ""
	}
	from, to := d.Sel.Ordered()
	return d.Buffer.Range(from, to)
}

// slotArgs is the argument map of set_bookmark and jump_to_bookmark.
func slotArgs(slot string) map[string]any {
	return map[string]any{"char": slot}
}

func TestBookmarkSlotsRoundTrip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		set      string // char argument of set_bookmark
		jump     string // char argument of jump_to_bookmark
		wantSet  bool   // set_bookmark must fail
		wantJump bool   // jump_to_bookmark must fail
		wantLine int    // caret line after the jump attempt
	}{
		{name: "slot 1 returns to the saved line", set: "1", jump: "1", wantLine: 2},
		{name: "slot 0 is valid", set: "0", jump: "0", wantLine: 2},
		{name: "slot 9 is valid", set: "9", jump: "9", wantLine: 2},
		{name: "a letter is not a slot", set: "a", jump: "a", wantSet: true, wantJump: true, wantLine: 0},
		{name: "an empty slot is refused", set: "", jump: "", wantSet: true, wantJump: true, wantLine: 0},
		{name: "an unset slot cannot be jumped to", set: "1", jump: "2", wantJump: true, wantLine: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := withDoc(numbered(4))
			marksCaret(e, 2, 0)
			if err := e.Do("set_bookmark", slotArgs(tt.set)).Err; (err != nil) != tt.wantSet {
				t.Fatalf("set_bookmark(%q) err = %v, want error %v", tt.set, err, tt.wantSet)
			}
			marksCaret(e, 0, 0)
			if err := e.Do("jump_to_bookmark", slotArgs(tt.jump)).Err; (err != nil) != tt.wantJump {
				t.Fatalf("jump_to_bookmark(%q) err = %v, want error %v", tt.jump, err, tt.wantJump)
			}
			if got := e.Active().Cursor.Line; got != tt.wantLine {
				t.Fatalf("caret line = %d, want %d", got, tt.wantLine)
			}
		})
	}
}

func TestBookmarkJumpReturnsToItsTab(t *testing.T) {
	t.Parallel()
	e := New(Options{Rows: 5, Cols: 80})
	e.docs = []*Document{
		newDocument("/fake/a.txt", "one\ntwo\nthree"),
		newDocument("/fake/b.txt", "x\ny"),
	}
	e.switchTo(0)
	marksCaret(e, 1, 1)
	if err := e.Do("set_bookmark", slotArgs("2")).Err; err != nil {
		t.Fatalf("set_bookmark: %v", err)
	}
	e.switchTo(1)
	if err := e.Do("jump_to_bookmark", slotArgs("2")).Err; err != nil {
		t.Fatalf("jump_to_bookmark: %v", err)
	}
	if got := e.Active().Name(); got != "a.txt" {
		t.Fatalf("active tab = %q, want a.txt", got)
	}
	if got := e.Active().Cursor; got != (buffer.Pos{Line: 1, Col: 1}) {
		t.Fatalf("caret = %+v, want {1 1}", got)
	}
}

func TestBookmarkOfClosedTabCannotBeJumpedTo(t *testing.T) {
	t.Parallel()
	e := New(Options{Rows: 5, Cols: 80})
	e.docs = []*Document{
		newDocument("/fake/a.txt", "one\ntwo"),
		newDocument("/fake/b.txt", "x"),
	}
	e.switchTo(0)
	marksCaret(e, 1, 0)
	if err := e.Do("set_bookmark", slotArgs("3")).Err; err != nil {
		t.Fatalf("set_bookmark: %v", err)
	}
	if err := e.Do("close_tab", nil).Err; err != nil {
		t.Fatalf("close_tab: %v", err)
	}
	if err := e.Do("jump_to_bookmark", slotArgs("3")).Err; err == nil {
		t.Fatal("jump_to_bookmark to a closed tab returned nil, want an error")
	}
	if got := e.Active().Name(); got != "b.txt" {
		t.Fatalf("active tab = %q, want b.txt", got)
	}
}

func TestMarkModeExtendsSelectionUntilCancelled(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		steps   []Action
		wantSel string
		wantCol int
	}{
		{"set_mark then moves select", []Action{"set_mark", "move_right", "move_right"}, "bc", 3},
		{"moves without a mark select nothing", []Action{"move_right", "move_right"}, "", 3},
		{"mark mode lasts over several moves", []Action{"set_mark", "move_right", "move_right", "move_right"}, "bcd", 4},
		{"cancel_mark keeps the selection", []Action{"set_mark", "move_right", "cancel_mark"}, "b", 2},
		{"a move after cancel_mark collapses the selection to its right edge", []Action{"set_mark", "move_right", "cancel_mark", "move_right"}, "", 2},
		{"clear_mark drops the selection at once", []Action{"set_mark", "move_right", "clear_mark"}, "", 2},
		{"clear_mark leaves mark mode off", []Action{"set_mark", "clear_mark", "move_right", "move_right"}, "", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := withDoc("abcdef")
			marksCaret(e, 0, 1)
			for _, a := range tt.steps {
				if r := e.Do(a, nil); r.Err != nil {
					t.Fatalf("Do(%s): %v", a, r.Err)
				}
			}
			if got := marksSelected(e); got != tt.wantSel {
				t.Fatalf("selection = %q, want %q", got, tt.wantSel)
			}
			if got := e.Active().Cursor.Col; got != tt.wantCol {
				t.Fatalf("caret col = %d, want %d", got, tt.wantCol)
			}
		})
	}
}

func TestMarkAndBookmarkActionsNeedADocument(t *testing.T) {
	t.Parallel()
	for _, a := range []Action{"set_mark", "cancel_mark", "clear_mark", "set_bookmark", "jump_to_bookmark"} {
		e := New(Options{Empty: true})
		if r := e.Do(a, slotArgs("1")); r.Err != ErrNoDocument {
			t.Errorf("Do(%s) with no tab: err = %v, want ErrNoDocument", a, r.Err)
		}
	}
}

func TestE2ActionsAreRegistered(t *testing.T) {
	t.Parallel()
	names := Actions()
	for _, want := range []Action{
		"set_bookmark", "jump_to_bookmark", "set_mark", "cancel_mark", "clear_mark",
		"navigate_back", "navigate_forward", "goto_matching_bracket", "dabbrev_expand",
		"find_selection_next", "find_selection_previous", "close", "save_all",
		"shell_command", "shell_command_replace",
	} {
		if !slices.Contains(names, want) {
			t.Errorf("Actions() missing %q", want)
		}
	}
}
