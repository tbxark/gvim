package editor

import (
	"strings"
	"testing"
)

func TestMemoValueKeepsValuesUntilTheTextChanges(t *testing.T) {
	t.Parallel()
	e := clipEditor(t, "abc")
	d := e.Active()
	calls := 0
	compute := func() any { calls++; return calls }
	if got := d.MemoValue("k", compute); got != 1 {
		t.Fatalf("first value = %v, want 1", got)
	}
	if got := d.MemoValue("k", compute); got != 1 || calls != 1 {
		t.Fatalf("second value = %v after %d calls, want the kept 1", got, calls)
	}
	if got := d.MemoValue("other", compute); got != 2 {
		t.Fatalf("another key = %v, want 2", got)
	}
	run(t, e, "move_line_end")
	if got := d.MemoValue("k", compute); got != 1 || calls != 2 {
		t.Fatalf("a caret move dropped the value: %v after %d calls", got, calls)
	}
	calls = 0
	e.Do("insert_char", map[string]any{"text": "x"})
	if got := d.MemoValue("k", compute); got != 1 {
		t.Fatalf("value after an edit = %v, want a new one", got)
	}
}

func TestHasLongLine(t *testing.T) {
	t.Parallel()
	e := clipEditor(t, strings.Repeat("é", 11)+"\nshort")
	d := e.Active()
	if !d.HasLongLine(10) {
		t.Error("a line of 11 runes is not longer than 10")
	}
	if d.HasLongLine(11) {
		t.Error("a line of 11 runes is longer than 11")
	}
	// The answer follows the text.
	run(t, e, "select_all", "delete_backward")
	if d.HasLongLine(10) {
		t.Error("an emptied document has a long line")
	}
}
