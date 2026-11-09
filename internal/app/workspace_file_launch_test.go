package app

import (
	"testing"

	"github.com/tbxark/gvim/config"
)

// TestDaemonWorkspaceIsNotAFileLaunch: a daemon/shared workspace is built from session
// state, so workspaceOptions clears every launch field (even if the daemon's own options
// carried them) and the plugin-facing file-launch property is false. This is the
// provable per-workspace behavior the shared host routes to the active model.
func TestDaemonWorkspaceIsNotAFileLaunch(t *testing.T) {
	m, _ := newWorkspaceManagerTest(t)
	ws := createDormant(t, m)
	m.opts.Files = []FileTarget{{Path: "launch.txt"}}
	m.opts.Simple = true
	m.opts.Diff = true
	m.opts.Restore = true
	m.opts.Stdin = []byte("piped\n")
	opts, err := m.workspaceOptions(ws)
	if err != nil {
		t.Fatal(err)
	}
	if len(opts.Files) != 0 || opts.Simple || opts.Diff || opts.Restore || opts.StdinLaunch() {
		t.Fatalf("daemon workspace options kept launch fields: %+v", opts)
	}
	if plan := planSession(opts, config.Defaults()); plan.fileLaunch {
		t.Fatalf("daemon workspace plan = %+v, want no file launch", plan)
	}
}
