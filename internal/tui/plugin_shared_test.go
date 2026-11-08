package tui

import (
	"testing"

	"github.com/tbxark/gvim/pluginapi"
)

// TestPluginSharedCarriesEditorWideRegistrations: the daemon's shared host keeps plugin
// commands/menus/status items/key bindings when a workspace switch installs them into the
// entering model, and per-workspace chrome (panels) is not carried.
func TestPluginSharedCarriesEditorWideRegistrations(t *testing.T) {
	a := statusViewModel(t, nil)
	b := statusViewModel(t, nil)
	shared := NewPluginShared()
	shared.Bind(a)
	if shared.Active() != a {
		t.Fatal("Bind did not make the model active")
	}
	if got := shared.host.model(); got != a {
		t.Fatal("the host mux does not route to the active model")
	}

	// A registers an editor-wide command; B must see it after the switch.
	a.plug.commands = map[string][]pluginapi.CommandInfo{
		"plugin_a": {{ID: "a.run", Title: "Plugin A", Plugin: "plugin_a"}},
	}
	a.plug.order = []string{"plugin_a"}
	shared.Bind(b)
	if len(b.plug.commands["plugin_a"]) != 1 || b.plug.order[0] != "plugin_a" {
		t.Fatalf("B did not inherit A's command registration: %+v %+v", b.plug.commands, b.plug.order)
	}

	// B registers another command, a status item and a menu; going back gives A all of
	// them and nothing of B's workspace chrome.
	b.plug.commands["plugin_b"] = []pluginapi.CommandInfo{{ID: "b.run", Title: "Plugin B", Plugin: "plugin_b"}}
	b.plug.order = append(b.plug.order, "plugin_b")
	shared.host.SetStatusItems("plugin_b", []pluginapi.StatusItem{{ID: "item", Plugin: "plugin_b", Text: "ok"}})
	shared.host.SetMenuItems("plugin_b", []pluginapi.MenuItem{{Menu: "File", Label: "Plugin B", Action: "b.run"}})
	b.plug.panels.order = []pluginapi.PanelID{7}
	shared.Bind(a)
	if len(a.plug.commands["plugin_b"]) != 1 {
		t.Fatalf("A did not inherit B's command: %+v", a.plug.commands)
	}
	if got := a.plug.keys.status.items["plugin_b"]; len(got) != 1 || got[0].Text != "ok" {
		t.Fatalf("A did not inherit the status item: %+v", a.plug.keys.status.items)
	}
	if got := a.plug.menus.byPlugin["plugin_b"]; len(got) != 1 || got[0].Label != "Plugin B" {
		t.Fatalf("A did not inherit the menu row: %+v", a.plug.menus.byPlugin)
	}
	if len(a.plug.panels.order) != 0 {
		t.Fatalf("workspace-scoped panel state leaked across the switch: %+v", a.plug.panels.order)
	}
	if shared.host.model() != a || shared.Active() != a {
		t.Fatal("the host mux did not retarget to A")
	}
}

// TestPluginSharedSkipsRuntimeStop: a workspace model bound into the shared runtime must
// not stop it when its workspace closes; the daemon owns the lifecycle.
func TestPluginSharedSkipsRuntimeStop(t *testing.T) {
	m := statusViewModel(t, nil)
	shared := NewPluginShared()
	m.plug.shared = shared
	m.plug.host = shared.host
	// No runtime is installed; a wrong path would panic on Stop. It must simply return.
	m.pluginStop()
}
