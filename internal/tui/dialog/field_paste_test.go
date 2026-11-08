package dialog

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestFieldCtrlVReturnsTheClipboardRead(t *testing.T) {
	t.Parallel()
	f := newField("")
	if cmd := f.key(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl}); cmd == nil {
		t.Error("Ctrl+V dropped the clipboard read")
	}
	if cmd := f.key(key("a")); cmd != nil {
		t.Errorf("typing started a command: %v", cmd)
	}
}
