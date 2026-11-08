package tui

import (
	"slices"
	"testing"

	"github.com/tbxark/gvim/pluginapi"
)

// TestSettingsPluginActRunsControlFromUpdate checks the split the plugin runtime
// needs: Act (SettingsPort) only wrote and reloaded the config file; the root runs
// the runtime's Control from the update goroutine when the Act result arrives.
func TestSettingsPluginActRunsControlFromUpdate(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		action, value, wantOp, wantName string
	}{
		{"plugin-enable", "git", "enable", "git"},
		{"plugin-disable", "flash", "disable", "flash"},
		{"plugin-reload", "", "reload_all", ""},
	} {
		t.Run(tt.action, func(t *testing.T) {
			t.Parallel()
			m, rt, _ := newMgrFixture(t)
			_, cmd := m.Update(settingsActDoneMsg{action: tt.action, value: tt.value, message: "done"})
			feed(m, cmd)
			want := pluginapi.Control{Op: tt.wantOp, Name: tt.wantName}
			if !slices.Contains(rt.controls, want) {
				t.Fatalf("controls = %+v, want %+v", rt.controls, want)
			}
		})
	}
}

// TestSettingsPluginActionUnknownRunsNoControl checks that a non-plugin action does
// not touch the runtime.
func TestSettingsPluginActionUnknownRunsNoControl(t *testing.T) {
	t.Parallel()
	m, rt, _ := newMgrFixture(t)
	if cmd := m.settingsPluginControl("lsp-restart", "go"); cmd != nil {
		t.Error("an LSP action produced a plugin Control command")
	}
	if len(rt.controls) != 0 {
		t.Errorf("controls = %+v, want none", rt.controls)
	}
}
