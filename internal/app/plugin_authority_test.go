package app

import (
	"strings"
	"testing"
)

// TestRemotePluginAuthorityNoticeStatesDegradation: a remote launch with plugins tells
// the user that only the workspace-backed APIs follow the host and that watching is the
// polled transport, not the local fsnotify.
func TestRemotePluginAuthorityNoticeStatesDegradation(t *testing.T) {
	t.Parallel()
	for _, want := range []string{"gvim.fs/gvim.proc", "remote SSH workspace", "getenv", "lookpath", "polled"} {
		if !strings.Contains(remotePluginAuthorityNotice, want) {
			t.Fatalf("remote plugin notice = %q, missing %q", remotePluginAuthorityNotice, want)
		}
	}
}
