package tui

import "testing"

// TestPluginHostFileLaunchIsPerModel: the direct host reports its model's immutable
// property, and the daemon shared host follows the active workspace model (the bind
// switch), never a value captured from the runtime's first workspace.
func TestPluginHostFileLaunchIsPerModel(t *testing.T) {
	direct := &pluginHost{m: New(Deps{FileLaunch: true})}
	if !direct.FileLaunch() {
		t.Fatal("direct host did not report the model's file launch")
	}
	shared := NewPluginShared()
	first := New(Deps{})
	second := New(Deps{FileLaunch: true})
	shared.Bind(first)
	if shared.host.FileLaunch() {
		t.Fatal("shared host reported a file launch before the daemon bound one")
	}
	shared.Bind(second)
	if !shared.host.FileLaunch() {
		t.Fatal("shared host did not follow the active workspace's file launch")
	}
	shared.Bind(first)
	if shared.host.FileLaunch() {
		t.Fatal("shared host kept the previous workspace's file launch after a switch")
	}
}
