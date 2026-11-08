package tui

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/pluginapi"
)

// modeItem returns the first status item of the mode, "" when there is none.
func modeItem(m *Model) string {
	for _, it := range m.plugStatusItems() {
		if strings.HasPrefix(it.id, "mode:") {
			return it.text
		}
	}
	return ""
}

// A mode that can trap the keyboard says how to leave it; a mode of one buffer shows a label
// at all.
func TestModeLabelsSayHowToLeave(t *testing.T) {
	t.Parallel()
	t.Run("an editor mode that swallows keys", func(t *testing.T) {
		t.Parallel()
		e := newKeyEnv(t)
		e.mode(t, pluginapi.ModeInfo{Name: "m", Status: "-- OUTLINE --", Text: pluginapi.TextSwallow})
		if got := modeItem(e.m); got != "-- OUTLINE --" {
			t.Errorf("label = %q, want the plugin's own label: an editor mode draws it", got)
		}
	})
	t.Run("an editor mode that passes keys keeps its label", func(t *testing.T) {
		t.Parallel()
		e := newKeyEnv(t)
		e.mode(t, pluginapi.ModeInfo{Name: "m", Status: "-- EDIT --"})
		if got := modeItem(e.m); got != "-- EDIT --" {
			t.Errorf("label = %q", got)
		}
	})
	t.Run("a mode that uses Esc needs no hint", func(t *testing.T) {
		t.Parallel()
		e := newKeyEnv(t)
		e.mode(t, pluginapi.ModeInfo{Name: "m", Status: "-- NORMAL --", Text: pluginapi.TextSwallow})
		if got := modeItem(e.m); got != "-- NORMAL --" {
			t.Errorf("label = %q", got)
		}
	})
	t.Run("a mode of one buffer", func(t *testing.T) {
		t.Parallel()
		e := newKeyEnv(t)
		e.m.plug.host.SetModes("p", []pluginapi.ModeInfo{{Name: "outline", Plugin: "p", Status: "-- OUTLINE --", Text: pluginapi.TextSwallow}})
		e.pe.bufs[0].Mode = "outline"
		if got := modeItem(e.m); got != "-- OUTLINE -- (Esc x3 leaves)" {
			t.Errorf("label = %q", got)
		}
	})
	t.Run("a mode of one buffer without a label is named", func(t *testing.T) {
		t.Parallel()
		e := newKeyEnv(t)
		e.m.plug.host.SetModes("p", []pluginapi.ModeInfo{{Name: "outline", Plugin: "p", Text: pluginapi.TextSwallow}})
		e.pe.bufs[0].Mode = "outline"
		if got := modeItem(e.m); got != "-- OUTLINE -- (Esc x3 leaves)" {
			t.Errorf("label = %q", got)
		}
	})
	t.Run("once the chord switched it off the label goes", func(t *testing.T) {
		t.Parallel()
		e := newKeyEnv(t)
		e.m.plug.host.SetModes("p", []pluginapi.ModeInfo{{Name: "outline", Plugin: "p", Text: pluginapi.TextSwallow}})
		e.pe.bufs[0].Mode = "outline"
		e.m.Update(decodeKey(t, "\x1e"))
		if got := modeItem(e.m); got != "" {
			t.Errorf("label = %q after the panic chord", got)
		}
	})
}
