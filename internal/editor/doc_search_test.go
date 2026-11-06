package editor

import (
	"errors"
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
)

// selectFoo returns an editor on "foo bar foo bar foo" with the first "foo" selected.
func selectFoo(t *testing.T) *Editor {
	t.Helper()
	e := docWith(Options{}, "foo bar foo bar foo")
	e.SelectRange(buffer.Pos{}, buffer.Pos{Col: 3})
	return e
}

func TestFindInSelectionNeedsASelection(t *testing.T) {
	t.Parallel()
	e := docWith(Options{}, "foo bar")
	if r := e.Do("find_in_selection", nil); !errors.Is(r.Err, ErrNothingSelected) {
		t.Fatalf("err = %v, want ErrNothingSelected", r.Err)
	}
	if _, ok := e.SearchRange(); ok {
		t.Fatal("a refused find_in_selection set a search range")
	}
}

func TestFindInSelectionSetsTheSearchRange(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		from buffer.Pos // selection anchor
		to   buffer.Pos // selection head
		want buffer.Selection
	}{
		{
			name: "the range is the selection",
			from: buffer.Pos{},
			to:   buffer.Pos{Col: 3},
			want: buffer.Selection{Anchor: buffer.Pos{}, Head: buffer.Pos{Col: 3}},
		},
		{
			name: "a backward selection gives the same range in document order",
			from: buffer.Pos{Col: 3},
			to:   buffer.Pos{},
			want: buffer.Selection{Anchor: buffer.Pos{}, Head: buffer.Pos{Col: 3}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := docWith(Options{}, "foo bar foo bar foo")
			e.SelectRange(tt.from, tt.to)
			run(t, e, "find_in_selection")
			got, ok := e.SearchRange()
			if !ok || got != tt.want {
				t.Fatalf("SearchRange() = %+v, %t, want %+v, true", got, ok, tt.want)
			}
			if want := "Search limited to the selection"; e.Status() != want {
				t.Fatalf("status = %q, want %q", e.Status(), want)
			}
		})
	}
}

func TestFindSelectionNextStaysInsideTheSearchRange(t *testing.T) {
	t.Parallel()
	e := selectFoo(t)
	run(t, e, "find_in_selection", "find_selection_next")
	if got := selectedText(e.Active()); got != "foo" {
		t.Fatalf("selected %q, want foo", got)
	}
	if got := e.Active().Sel.Anchor; got != (buffer.Pos{}) {
		t.Fatalf("selection moved to %+v, want it to stay at the range", got)
	}
}

func TestFindSelectionNextWithoutRangeLeavesTheSelection(t *testing.T) {
	t.Parallel()
	e := selectFoo(t)
	run(t, e, "find_selection_next")
	if got := e.Active().Sel.Anchor; got != (buffer.Pos{Col: 8}) {
		t.Fatalf("selection anchor = %+v, want the next foo at column 8", got)
	}
}

func TestClearSearchDropsTheRange(t *testing.T) {
	t.Parallel()
	e := selectFoo(t)
	run(t, e, "find_in_selection", "clear_search")
	if _, ok := e.SearchRange(); ok {
		t.Fatal("clear_search left a search range")
	}
	run(t, e, "find_selection_next")
	if got := e.Active().Sel.Anchor; got != (buffer.Pos{Col: 8}) {
		t.Fatalf("after clear the next find selected from %+v, want column 8", got)
	}
}

func TestClearSearchWithNoSearchIsQuiet(t *testing.T) {
	t.Parallel()
	e := docWith(Options{}, "foo")
	run(t, e, "clear_search")
	if want := "Search cleared"; e.Status() != want {
		t.Fatalf("status = %q, want %q", e.Status(), want)
	}
}

func TestFindInSelectionWorksOnReadOnlyDocument(t *testing.T) {
	t.Parallel()
	e := selectFoo(t)
	run(t, e, "toggle_read_only", "find_in_selection")
	if _, ok := e.SearchRange(); !ok {
		t.Fatal("find_in_selection on a read-only document set no range")
	}
}

func TestTextChangeEndsTheSearchRange(t *testing.T) {
	t.Parallel()
	e := selectFoo(t)
	run(t, e, "find_in_selection")
	typeText(t, e, "x")
	if _, ok := e.SearchRange(); ok {
		t.Fatal("a text change left the search range, whose positions are now stale")
	}
}
