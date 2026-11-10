package tui

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/tbxark/gvim/editor"
)

func TestReadOnlyPagerNavigationKeys(t *testing.T) {
	t.Parallel()
	ed := newFakeEditor("line 1", "line 2", "line 3")
	m := newTestModel(t, ed, nil, 80, 24)

	// Editable document: typing 'x' dispatches insert_char
	press(m, typed("x"))
	if !slices.Contains(ed.actions, editor.Action("insert_char")) {
		t.Fatalf("actions = %v, want insert_char", ed.actions)
	}

	// Make document read-only
	ed.doc.readOnly = true
	ed.actions = nil

	// In read-only mode:
	// 'j' -> move_down
	press(m, typed("j"))
	// 'k' -> move_up
	press(m, typed("k"))
	// ' ' -> move_page_down
	press(m, typed(" "))
	// 'b' -> move_page_up
	press(m, typed("b"))
	// 'g' -> move_document_start
	press(m, typed("g"))
	// 'G' -> move_document_end
	press(m, typed("G"))

	wantActions := []editor.Action{
		"move_down",
		"move_up",
		"move_page_down",
		"move_page_up",
		"move_document_start",
		"move_document_end",
	}
	if !slices.Equal(ed.actions, wantActions) {
		t.Fatalf("read-only actions = %v, want %v", ed.actions, wantActions)
	}

	// 'q' in pager mode exits
	cmd := press(m, typed("q"))
	if cmd == nil {
		t.Fatal("pressing 'q' in read-only mode should return quit cmd, got nil")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("msg = %T (%v), want tea.QuitMsg", msg, msg)
	}
}
