package plugin_test

import (
	"testing"

	"github.com/tbxark/gvim/plugin/plugintest"
)

// gvim.workspace.file_launch is the fake host's immutable invocation property.
func TestWorkspaceFileLaunch(t *testing.T) {
	host := plugintest.NewFakeHost()
	host.Workspace().FileLaunch = true
	luaRows(t, gvimRows([]row{
		{"file launch", `T.spy(gvim.workspace.file_launch())`, "true"},
	}), plugintest.Options{Host: host})
	host.Workspace().FileLaunch = false
	luaRows(t, gvimRows([]row{
		{"session launch", `T.spy(gvim.workspace.file_launch())`, "false"},
	}), plugintest.Options{Host: host})
}
