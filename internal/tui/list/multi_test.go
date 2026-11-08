package list

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/style"
)

func TestMultiSelectMarksAndReturnsRows(t *testing.T) {
	t.Parallel()
	rows := []Row{{Left: "a", Value: "a"}, {Header: true, Left: "group"}, {Left: "b", Value: "b"}}
	var got []Row
	v := New("T", rows, style.Overlay{}, nil)
	v.Multi = true
	v.ChooseMany = func(_ *View, marked []Row) tea.Cmd {
		got = append([]Row(nil), marked...)
		return nil
	}
	v.update(tea.KeyPressMsg{Code: tea.KeySpace}) // mark a
	v.update(tea.KeyPressMsg{Code: tea.KeyDown})  // header is skipped
	v.update(tea.KeyPressMsg{Code: tea.KeySpace}) // mark b
	v.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(got) != 2 || got[0].Value != "a" || got[1].Value != "b" {
		t.Fatalf("marked rows = %+v", got)
	}
	// A second Space unmarks b; the remaining marked a is still returned.
	got = nil
	v.update(tea.KeyPressMsg{Code: tea.KeySpace}) // unmark b (highlight is b)
	v.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(got) != 1 || got[0].Value != "a" {
		t.Fatalf("remaining marked rows = %+v", got)
	}
	v.update(tea.KeyPressMsg{Code: tea.KeyUp})    // back to a
	v.update(tea.KeyPressMsg{Code: tea.KeySpace}) // unmark a: no marks left
	got = nil
	v.update(tea.KeyPressMsg{Code: tea.KeyEnter}) // falls back to run(nil) = nothing
	if len(got) != 0 {
		t.Fatalf("Enter with no marks ran ChooseMany: %+v", got)
	}
}

func TestMultiSelectEnterWithoutMarksRunsHighlighted(t *testing.T) {
	t.Parallel()
	var ran string
	v := New("T", []Row{{Left: "a", Value: "a"}, {Left: "b", Value: "b"}}, style.Overlay{}, func(_ *View, r Row) tea.Cmd {
		ran = r.Value
		return nil
	})
	v.Multi = true
	v.ChooseMany = func(*View, []Row) tea.Cmd { t.Fatal("ChooseMany ran with no marks"); return nil }
	v.update(tea.KeyPressMsg{Code: tea.KeyDown})
	v.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if ran != "b" {
		t.Fatalf("ran = %q", ran)
	}
}

func TestMultiSelectDrawsCheckboxes(t *testing.T) {
	t.Parallel()
	v := New("T", []Row{{Left: "alpha"}, {Left: "beta"}}, style.Overlay{}, nil)
	v.Multi = true
	g := grid.New(80, 24)
	v.Draw(g)
	v.update(tea.KeyPressMsg{Code: tea.KeySpace})
	g = grid.New(80, 24)
	v.Draw(g)
	rows := g.Plain()
	var marked, unmarked string
	for _, row := range rows {
		if strings.Contains(row, "[x] alpha") {
			marked = row
		}
		if strings.Contains(row, "[ ] beta") {
			unmarked = row
		}
	}
	if marked == "" || unmarked == "" {
		t.Fatalf("checkbox markers not painted: %q", rows)
	}
}
