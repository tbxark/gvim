package palette

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestCtrlVReturnsTheClipboardRead checks that the command which reads the clipboard
// reaches the program, and that an ordinary key returns none.
func TestCtrlVReturnsTheClipboardRead(t *testing.T) {
	p := New(commandOptions(">", nil))
	if cmd := p.update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl}); cmd == nil {
		t.Error("Ctrl+V dropped the clipboard read")
	}
	if cmd := p.update(typed("a")); cmd != nil {
		t.Errorf("typing started a command: %v", cmd)
	}
}
