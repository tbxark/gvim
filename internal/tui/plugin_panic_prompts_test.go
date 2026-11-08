package tui

import (
	"slices"
	"testing"

	"github.com/tbxark/gvim/pluginapi"
)

// The panic chord (plugin_keys.go) must close the prompts, pickers and popups of the prompt
// stage and resolve each wait once, as cancelled, and a key wait with them.
func TestPanicChordClosesPluginPrompts(t *testing.T) {
	t.Parallel()
	m, rt := newPromptModel(t)
	openVia(t, m, rt, func(h pluginapi.Host) {
		_ = h.Input(1, pluginapi.InputSpec{})
		_ = h.OpenPicker(2, 9, pluginapi.PickerSpec{})
		_ = h.ShowPopup(3, 10, pluginapi.PopupSpec{Lines: []string{"x"}})
	})
	m.plug.waits[99] = "key"
	if !m.pluginInputActive() {
		t.Fatal("an open plugin prompt does not count as plugin input")
	}
	feed(m, m.plugPanic())
	if len(m.overlays) != 0 {
		t.Fatalf("%d overlays left", len(m.overlays))
	}
	var toks []pluginapi.Token
	for _, c := range rt.resolved {
		if c.Err != "cancelled: plugin input cleared" {
			t.Fatalf("resolved %+v, want cancelled", c)
		}
		toks = append(toks, c.Token)
	}
	slices.Sort(toks)
	if !slices.Equal(toks, []pluginapi.Token{1, 2, 3, 99}) {
		t.Fatalf("cancelled waits %v, want 1 2 3 99 once each", toks)
	}
	if len(m.plug.waits) != 0 || m.pluginInputActive() {
		t.Fatalf("waits %v, input active %v", m.plug.waits, m.pluginInputActive())
	}
}
