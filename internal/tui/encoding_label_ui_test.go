package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestStatusRowEncodingFollowsTypedCJK types non-ASCII text into an ASCII file. The
// status row shows the encoding of the text as it is now, before any save, and goes
// back to ASCII when the typed text is deleted.
func TestStatusRowEncodingFollowsTypedCJK(t *testing.T) {
	m, ed := viewModel(t, DefaultViewOptions(), "hello\n", 80, 24)
	status := func() string { return screenRow(m, 23) }
	if !strings.Contains(status(), "ASCII") {
		t.Fatalf("setup: status row %q does not show ASCII", status())
	}
	m.Update(tea.KeyPressMsg{Code: '中', Text: "中"})
	if s := status(); !strings.Contains(s, "UTF-8") || strings.Contains(s, "ASCII") {
		t.Fatalf("after typing CJK, before save, the status row is %q, want UTF-8", s)
	}
	// The test keymap binds no backspace, so the deletion is the editor's own action.
	if r := ed.Do("delete_backward", nil); r.Err != nil {
		t.Fatalf("delete_backward: %v", r.Err)
	}
	if s := status(); !strings.Contains(s, "ASCII") {
		t.Fatalf("after deleting the typed text, the status row is %q, want ASCII", s)
	}
}
