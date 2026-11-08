package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/pluginapi"
)

// The history of an input prompt is walked with Up and Down; the text typed before the first
// Up comes back after the last Down.
func TestPluginInputHistory(t *testing.T) {
	t.Parallel()
	hist := []string{"one", "two", "three"}
	tests := []struct {
		name string
		keys []tea.KeyPressMsg
		want string
	}{
		{"up recalls the latest", []tea.KeyPressMsg{special(tea.KeyUp)}, "three"},
		{"up twice", []tea.KeyPressMsg{special(tea.KeyUp), special(tea.KeyUp)}, "two"},
		{"up stops at the oldest", []tea.KeyPressMsg{special(tea.KeyUp), special(tea.KeyUp), special(tea.KeyUp), special(tea.KeyUp), special(tea.KeyUp)}, "one"},
		{"down walks back", []tea.KeyPressMsg{special(tea.KeyUp), special(tea.KeyUp), special(tea.KeyDown)}, "three"},
		{"down past the newest restores the draft", []tea.KeyPressMsg{special(tea.KeyUp), special(tea.KeyDown)}, "dr"},
		{"down without up does nothing", []tea.KeyPressMsg{special(tea.KeyDown)}, "dr"},
		{"ctrl+p and ctrl+n", []tea.KeyPressMsg{{Code: 'p', Mod: tea.ModCtrl}, {Code: 'p', Mod: tea.ModCtrl}, {Code: 'n', Mod: tea.ModCtrl}}, "three"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, rt := newPromptModel(t)
			openVia(t, m, rt, func(h pluginapi.Host) {
				if err := h.Input(7, pluginapi.InputSpec{Prompt: ":", History: hist}); err != nil {
					t.Error(err)
				}
			})
			typeInto(m, "dr")
			for _, k := range tt.keys {
				feed(m, runKey(m, k))
			}
			feed(m, runKey(m, special(tea.KeyEnter)))
			if got := lastResolved(t, rt); len(got.Values) != 1 || got.Values[0] != tt.want {
				t.Fatalf("resolved %+v, want %q", got, tt.want)
			}
		})
	}
}

func TestPluginInputCtrlCCancels(t *testing.T) {
	t.Parallel()
	m, rt := newPromptModel(t)
	openVia(t, m, rt, func(h pluginapi.Host) { _ = h.Input(7, pluginapi.InputSpec{Prompt: ":"}) })
	typeInto(m, "ab")
	feed(m, runKey(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}))
	got := lastResolved(t, rt)
	if got.Token != 7 || got.Err != "" || len(got.Values) != 1 || got.Values[0] != nil {
		t.Fatalf("resolved %+v, want a nil answer", got)
	}
	if len(m.overlays) != 0 {
		t.Fatalf("%d overlays after ctrl+c", len(m.overlays))
	}
}
