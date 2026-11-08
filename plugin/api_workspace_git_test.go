package plugin_test

import (
	"testing"

	"github.com/tbxark/gvim/plugin"
	"github.com/tbxark/gvim/plugin/plugintest"
)

// gvim.workspace.git_root and git_branch are what gvim.git falls back to when the git
// binary is missing (AMENDMENTS C28), and gvim.has("git") says whether the binary exists.
func TestWorkspaceGitFallbacks(t *testing.T) {
	svc := &plugintest.FakeServices{GitRoots: map[string]string{"/repo": "/repo"}, Branches: map[string]string{"/repo": "trunk"}}
	luaRows(t, gvimRows([]row{
		{"root of a subdirectory", `T.spy(gvim.workspace.git_root("/repo/a/b"))`, "/repo"},
		{"root of a stranger", `T.spy(gvim.workspace.git_root("/other"))`, "nil"},
		{"branch", `T.spy(gvim.workspace.git_branch("/repo"))`, "trunk"},
		{"branch of a stranger", `T.spy(gvim.workspace.git_branch("/other"))`, "~nil not_found: not a repository: /other"},
		{"bad arguments", `T.spy(pcall(gvim.workspace.git_root, 5)) T.spy(pcall(gvim.workspace.git_branch, ""))`, "~bad argument"},
	}), plugintest.Options{Services: svc})
}

func TestWorkspaceGitWithoutServices(t *testing.T) {
	luaRows(t, gvimRows([]row{
		{"root is not found", `T.spy(gvim.workspace.git_root("/repo"))`, "nil"},
		{"branch is unsupported", `T.spy(gvim.workspace.git_branch("/repo"))`, "~nil unsupported"},
	}), plugintest.Options{Services: plugin.NoServices()})
}

func TestHasGitFollowsTheBinary(t *testing.T) {
	for _, installed := range []bool{false, true} {
		o := plugintest.NewFakeOS()
		if installed {
			o.Bin["git"] = "/usr/bin/git"
		}
		want := "false"
		if installed {
			want = "true"
		}
		h := start(t, `local gvim = require "gvim" T.spy(gvim.has("git"), gvim.has("decor"), gvim.has("nope"))`, plugintest.Options{OS: o})
		if got := spyText(h); got != want+" true false" {
			t.Errorf("git installed %v: has = %q", installed, got)
		}
		if n := o.UICalls(); n != 0 {
			t.Errorf("has(git) made %d IO calls on the ui goroutine", n)
		}
	}
}
