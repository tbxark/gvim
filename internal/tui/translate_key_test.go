package tui

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestTranslateKeyModifiers(t *testing.T) {
	tests := []struct {
		name     string
		key      tea.Key
		wantName string
		wantMods []string
		wantText string
	}{
		{"plain letter", tea.Key{Code: 's', Text: "s"}, "s", nil, "s"},
		{"ctrl", tea.Key{Code: 's', Mod: tea.ModCtrl}, "s", []string{"ctrl"}, ""},
		{"super letter", tea.Key{Code: 's', Mod: tea.ModSuper}, "s", []string{"super"}, ""},
		{"super with text is not typed", tea.Key{Code: 's', Text: "s", Mod: tea.ModSuper}, "s", []string{"super"}, ""},
		{"super shift letter", tea.Key{Code: 'p', Mod: tea.ModSuper | tea.ModShift}, "p", []string{"shift", "super"}, ""},
		{"ctrl super", tea.Key{Code: 'k', Mod: tea.ModCtrl | tea.ModSuper}, "k", []string{"ctrl", "super"}, ""},
		{"super arrow", tea.Key{Code: tea.KeyUp, Mod: tea.ModSuper}, "Up", []string{"super"}, ""},
		// xterm modifier 9 (Cmd+Up on macOS terminals) is decoded as Meta.
		{"meta arrow stands for super", tea.Key{Code: tea.KeyUp, Mod: tea.ModMeta}, "Up", []string{"super"}, ""},
		{"alt is not super", tea.Key{Code: 'g', Mod: tea.ModAlt}, "g", []string{"alt"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kp, text := translateKey(tt.key)
			if kp.Name != tt.wantName || !slices.Equal(kp.Mods, tt.wantMods) || text != tt.wantText {
				t.Errorf("translateKey = %+v, %q; want %q %v, %q", kp, text, tt.wantName, tt.wantMods, tt.wantText)
			}
		})
	}
}
