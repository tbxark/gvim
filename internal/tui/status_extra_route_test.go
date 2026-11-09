package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/pluginapi"
)

// TestPluginStatusItemNamedEnvStaysPlugin is the structural-routing regression: a plugin
// status item whose ID is also "env" and Clickable must run the plugin's status-click
// action, not the native environment action. The model's env port says "no environment
// detected", so a misroute would paint the env_none notice instead of running the plugin.
func TestPluginStatusItemNamedEnvStaysPlugin(t *testing.T) {
	e := newKeyEnv(t)
	e.m.env = &fakeEnvPort{trust: "trusted"}
	e.m.plug.host.SetStatusItems("p", []pluginapi.StatusItem{
		{ID: "env", Plugin: "p", Text: "plugin env", Clickable: true},
	})
	l := e.m.layout()
	x := -1
	for _, r := range l.Status {
		if r.Kind == hitStatusPlugin && strings.Contains(r.Text, "plugin env") {
			x = r.X
		}
	}
	if x < 0 {
		t.Fatalf("the plugin env item is not on the status row: %+v", l.Status)
	}
	// Structural source: the plugin entry (before the native appended segments) is a
	// plugin item even though its ID matches the native segment's ID.
	index := -1
	for i, r := range l.Status {
		if r.Kind == hitStatusPlugin && strings.Contains(r.Text, "plugin env") {
			index = r.Index
		}
		_ = i
	}
	if entry, ok := e.m.statusExtraEntryAt(index); !ok || entry.source != statusExtraPlugin {
		t.Fatalf("plugin item source = %+v, want statusExtraPlugin", entry)
	}
	e.m.Update(click(x, l.StatusY, tea.MouseLeft))
	if !slices.Equal(e.rt.runs, []string{"p.__status_env"}) {
		t.Fatalf("runs = %v, want the plugin status-click action", e.rt.runs)
	}
	if e.m.notice == "No environment manager detected in this project" {
		t.Fatal("the plugin env item was misrouted to the native environment action")
	}
}
