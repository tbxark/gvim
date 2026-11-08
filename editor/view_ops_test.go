package editor

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
)

// withDoc returns a 5-row editor whose only tab holds text.
func withDoc(text string) *Editor {
	e := New(Options{Rows: 5, Cols: 80})
	e.docs = []*Document{newDocument("", text)}
	e.switchTo(0)
	return e
}

// withTabs returns a 5-row editor with one empty tab per name, the first one
// active. The paths are fake; nothing is read from disk.
func withTabs(names ...string) *Editor {
	e := New(Options{Rows: 5, Cols: 80})
	e.docs = nil
	for _, n := range names {
		e.docs = append(e.docs, newDocument("/fake/"+n, ""))
	}
	e.switchTo(0)
	return e
}

// numbered returns n lines, "line 0" through "line n-1", joined by newlines.
func numbered(n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i)
	}
	return strings.Join(lines, "\n")
}

// tabNames returns the labels of the open tabs, in order.
func tabNames(e *Editor) []string {
	var names []string
	for _, t := range e.Tabs() {
		names = append(names, t.Name)
	}
	return names
}

func TestSetCursorClampsWithoutScrolling(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		p    buffer.Pos
		want buffer.Pos
	}{
		{"inside text", buffer.Pos{Line: 30, Col: 2}, buffer.Pos{Line: 30, Col: 2}},
		{"column past line end", buffer.Pos{Line: 30, Col: 99}, buffer.Pos{Line: 30, Col: len("line 30")}},
		{"line past document end", buffer.Pos{Line: 500}, buffer.Pos{Line: 99}},
		{"negative clamps to origin", buffer.Pos{Line: -4, Col: -4}, buffer.Pos{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := withDoc(numbered(100))
			e.ScrollTo(20)
			e.SetCursor(tt.p, false)
			d := e.Active()
			if d.Cursor != tt.want {
				t.Fatalf("Cursor = %+v, want %+v", d.Cursor, tt.want)
			}
			if d.Top != 20 {
				t.Fatalf("SetCursor scrolled: Top = %d, want 20", d.Top)
			}
		})
	}
}

func TestSetCursorExtendAndClear(t *testing.T) {
	t.Parallel()
	e := withDoc("alpha beta")
	e.SetCursor(buffer.Pos{Col: 1}, false)
	e.SetCursor(buffer.Pos{Col: 6}, true)
	d := e.Active()
	if !d.HasSelection() || d.Sel.Anchor != (buffer.Pos{Col: 1}) || d.Sel.Head != (buffer.Pos{Col: 6}) {
		t.Fatalf("extend: Sel = %+v", d.Sel)
	}
	e.SetCursor(buffer.Pos{Col: 3}, false)
	if d.Sel != nil || d.Cursor != (buffer.Pos{Col: 3}) {
		t.Fatalf("plain move should clear selection: Sel=%+v Cursor=%+v", d.Sel, d.Cursor)
	}
}

func TestSelectRange(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		anchor     buffer.Pos
		head       buffer.Pos
		wantSel    bool
		wantCursor buffer.Pos
	}{
		{"forward range puts caret at head", buffer.Pos{Col: 0}, buffer.Pos{Col: 5}, true, buffer.Pos{Col: 5}},
		{"backward range keeps anchor after head", buffer.Pos{Col: 5}, buffer.Pos{Col: 0}, true, buffer.Pos{Col: 0}},
		{"equal positions clear selection", buffer.Pos{Col: 2}, buffer.Pos{Col: 2}, false, buffer.Pos{Col: 2}},
		{"positions clamp into document", buffer.Pos{Col: 0}, buffer.Pos{Line: 9, Col: 9}, true, buffer.Pos{Line: 0, Col: 5}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := withDoc("hello")
			e.SelectRange(tt.anchor, tt.head)
			d := e.Active()
			if d.HasSelection() != tt.wantSel {
				t.Fatalf("HasSelection = %v, want %v", d.HasSelection(), tt.wantSel)
			}
			if d.Cursor != tt.wantCursor {
				t.Fatalf("Cursor = %+v, want %+v", d.Cursor, tt.wantCursor)
			}
			if tt.wantSel && d.Sel.Head != d.Cursor {
				t.Fatalf("Sel.Head %+v must equal Cursor %+v", d.Sel.Head, d.Cursor)
			}
		})
	}
}

func TestScrollByAndScrollToClamp(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		text  string
		start int
		call  func(e *Editor)
		want  int
	}{
		{"ScrollBy down", numbered(10), 0, func(e *Editor) { e.ScrollBy(3) }, 3},
		{"ScrollBy up past top clamps to zero", numbered(10), 2, func(e *Editor) { e.ScrollBy(-5) }, 0},
		{"ScrollBy past end clamps to last line", numbered(10), 0, func(e *Editor) { e.ScrollBy(100) }, 9},
		{"ScrollBy on one line stays at zero", "solo", 0, func(e *Editor) { e.ScrollBy(3) }, 0},
		{"ScrollTo negative clamps to zero", numbered(10), 4, func(e *Editor) { e.ScrollTo(-1) }, 0},
		{"ScrollTo inside document", numbered(10), 0, func(e *Editor) { e.ScrollTo(4) }, 4},
		{"ScrollTo past end clamps to last line", numbered(10), 0, func(e *Editor) { e.ScrollTo(50) }, 9},
		{"ScrollTo on empty buffer stays at zero", "", 0, func(e *Editor) { e.ScrollTo(7) }, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := withDoc(tt.text)
			e.ScrollTo(tt.start)
			e.SetCursor(buffer.Pos{Line: 0, Col: 1}, false)
			before := e.Active().Cursor
			tt.call(e)
			d := e.Active()
			if d.Top != tt.want {
				t.Fatalf("Top = %d, want %d", d.Top, tt.want)
			}
			if d.Cursor != before {
				t.Fatalf("scrolling moved the caret from %+v to %+v", before, d.Cursor)
			}
		})
	}
}

func TestActivateTab(t *testing.T) {
	t.Parallel()
	e := withTabs("a", "b", "c")
	e.ActivateTab(2)
	if !e.Tabs()[2].Active {
		t.Fatalf("ActivateTab(2) did not activate the third tab: %+v", e.Tabs())
	}
	e.ActivateTab(-1)
	e.ActivateTab(3)
	if e.Active().Name() != "c" {
		t.Fatalf("out-of-range ActivateTab changed the active tab to %q", e.Active().Name())
	}
	e.ActivateTab(0)
	if e.Active().Name() != "a" {
		t.Fatalf("ActivateTab(0) active = %q", e.Active().Name())
	}
}

func TestCloseTabKeepsActiveDocument(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		tabs       []string
		active     int
		close      int
		wantTabs   []string
		wantActive string
	}{
		{"close before active keeps active document", []string{"a", "b", "c"}, 2, 0, []string{"b", "c"}, "c"},
		{"close after active keeps active document", []string{"a", "b", "c"}, 0, 2, []string{"a", "b"}, "a"},
		{"close the active middle tab", []string{"a", "b", "c"}, 1, 1, []string{"a", "c"}, "c"},
		{"close the active last tab", []string{"a", "b", "c"}, 2, 2, []string{"a", "b"}, "b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := withTabs(tt.tabs...)
			e.switchTo(tt.active)
			if err := e.CloseTab(tt.close, false); err != nil {
				t.Fatalf("CloseTab(%d) = %v", tt.close, err)
			}
			if got := tabNames(e); !equalStrings(got, tt.wantTabs) {
				t.Fatalf("tabs = %v, want %v", got, tt.wantTabs)
			}
			if got := e.Active().Name(); got != tt.wantActive {
				t.Fatalf("active = %q, want %q", got, tt.wantActive)
			}
		})
	}
}

func TestCloseTabUnsavedAndBadIndex(t *testing.T) {
	t.Parallel()
	e := withTabs("a", "b")
	// Edit tab 1 so it is modified without changing the active tab.
	e.docs[1].Buffer.Insert(buffer.Pos{}, "x")

	err := e.CloseTab(1, false)
	if !errors.Is(err, ErrUnsaved) {
		t.Fatalf("CloseTab of modified tab = %v, want ErrUnsaved", err)
	}
	if e.Status() != err.Error() {
		t.Fatalf("status = %q, want %q", e.Status(), err.Error())
	}
	if n := len(e.Tabs()); n != 2 {
		t.Fatalf("modified tab was closed without force: %d tabs", n)
	}

	if err := e.CloseTab(1, true); err != nil {
		t.Fatalf("forced CloseTab = %v", err)
	}
	if got := tabNames(e); !equalStrings(got, []string{"a"}) {
		t.Fatalf("tabs after forced close = %v", got)
	}

	for _, i := range []int{-1, 1, 7} {
		err := e.CloseTab(i, true)
		if !errors.Is(err, ErrNoTab) {
			t.Fatalf("CloseTab(%d) = %v, want ErrNoTab", i, err)
		}
	}
	if n := len(e.Tabs()); n != 1 {
		t.Fatalf("bad index changed the tab count to %d", n)
	}
}

func TestCloseTabAfterDoClosesActive(t *testing.T) {
	t.Parallel()
	e := withTabs("a", "b", "c")
	e.switchTo(1)
	// The close_tab action and CloseTab must agree on the active tab.
	if r := e.Do("close_tab", nil); r.Err != nil {
		t.Fatalf("close_tab = %v", r.Err)
	}
	if got := tabNames(e); !equalStrings(got, []string{"a", "c"}) || e.Active().Name() != "c" {
		t.Fatalf("tabs = %v active = %q", got, e.Active().Name())
	}
}

// equalStrings reports whether a and b hold the same strings in order.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
