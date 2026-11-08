package tui

import (
	"testing"

	"github.com/tbxark/gvim/pluginapi"
)

// The mode of a buffer shows a label in the status row, and says how to leave when it does not
// bind Esc.
func TestBufferModeHasAStatusLabel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		info pluginapi.ModeInfo
		want string
	}{
		{"its own label", pluginapi.ModeInfo{Name: "outline", Status: "-- OUTLINE --"}, "-- OUTLINE -- (Esc x3 leaves)"},
		{"no label", pluginapi.ModeInfo{Name: "outline"}, "-- OUTLINE -- (Esc x3 leaves)"},
		{"binds Esc", pluginapi.ModeInfo{Name: "outline", Bindings: map[string]string{"escape": "p.fn"}}, "-- OUTLINE --"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newKeyEnv(t)
			tt.info.Plugin = "p"
			e.m.plug.host.SetModes("p", []pluginapi.ModeInfo{tt.info})
			e.pe.bufs[0].Mode = "outline"
			items := e.m.plugStatusItems()
			if len(items) != 1 || items[0].Text != tt.want || items[0].Plugin != "p" {
				t.Fatalf("status items = %+v, want one with %q", items, tt.want)
			}
			// the panic chord leaves it, and the label goes with it
			e.m.plugPanic()
			if items := e.m.plugStatusItems(); len(items) != 0 {
				t.Fatalf("the label stayed after the mode was cleared: %+v", items)
			}
		})
	}
}
