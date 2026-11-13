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

// TestFieldCtrlVReplacesAnUntouchedPrefill checks that the clipboard text of Ctrl+V
// replaces an untouched prefill as a bracketed paste does, and appends once the
// field was edited. An empty read keeps the prefill.
func TestFieldCtrlVReplacesAnUntouchedPrefill(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		touch bool
		raw   tea.Msg
		want  string
	}{
		{"untouched prefill is replaced", false, tea.PasteMsg{Content: "new"}, "new"},
		{"an empty read keeps the prefill", false, tea.PasteMsg{Content: ""}, "prefill"},
		{"an edited field appends", true, tea.PasteMsg{Content: "new"}, "prefillnew"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newField("prefill")
			f.selectAll()
			if tt.touch {
				f.untouched = false
			}
			f.update(fieldPasteMsg{raw: tt.raw})
			if got := f.value(); got != tt.want {
				t.Fatalf("value = %q, want %q", got, tt.want)
			}
		})
	}
}
