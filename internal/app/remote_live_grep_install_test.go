package app

import (
	"testing"

	"github.com/tbxark/gvim/internal/tui"
)

// TestInstallRemoteSearchPortsInstallsRemoteLiveGrep: the remote composition installs the
// authority-backed Grep port alongside project search; no local live-grep adapter remains.
func TestInstallRemoteSearchPortsInstallsRemoteLiveGrep(t *testing.T) {
	ports := tui.RootPorts{}
	installRemoteSearchPorts(&ports, &fakeOneShot{}, nil, nil, "/remote/w", "SSH:u@h", nil)
	if _, ok := ports.Grep.(*remoteLiveGrep); !ok {
		t.Fatalf("Grep port = %T, want the remote live grep", ports.Grep)
	}
	if _, ok := ports.ProjectSearch.(*remoteSearch); !ok {
		t.Fatalf("ProjectSearch port = %T", ports.ProjectSearch)
	}
}

// TestRemoteRootPortsClearsLocalGrepper: a remote launch clears the local Grepper before the
// remote branch installs the authority-backed one, so no local file reader can serve a
// remote-looking row.
func TestRemoteRootPortsClearsLocalGrepper(t *testing.T) {
	ports := tui.RootPorts{Grep: &liveGrep{}}
	remoteRootPorts(&ports)
	if ports.Grep != nil {
		t.Fatalf("Grep port = %T, want nil after remoteRootPorts", ports.Grep)
	}
}
