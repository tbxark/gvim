package app

// Remote workspace trust records (task-143). A remote workspace has no local filesystem
// root to canonicalize, so its decision is stored under an explicit record key derived
// from the remote target: the spec's display (user@host[:port]) plus the workspace root.
// The same target therefore keeps its decision across restarts and reconnects, and two
// different remote workspaces do not share one (even on the same host).
//
// Initial policy: the executable-content marker scan is local-only today, so an
// undecided remote target is opened with no markers (like a markerless local folder):
// it starts trusted and the decision is recorded, which means the trust prompt does not
// reappear on reconnect. A remote marker probe needs an early remote connection and is a
// recorded residual; a user can still restrict a remote workspace through the dialog and
// that decision persists by target.

import (
	"path"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/trust"
)

// remoteTrustTarget is the stable human-readable target of a remote workspace:
// user@host[:port]:workspace-root. The line/column suffix never affects the workspace.
func remoteTrustTarget(spec *authority.RemoteSpec) string {
	if spec == nil {
		return ""
	}
	workspace := path.Clean(spec.Workspace())
	if workspace == "" || workspace == "." {
		workspace = spec.Workspace()
	}
	return spec.Display() + ":" + workspace
}

// remoteTrustKey is the trust-store record key of a remote workspace.
func remoteTrustKey(spec *authority.RemoteSpec) string {
	return trust.RecordKey(remoteTrustTarget(spec))
}

// openRemoteTrust resolves the trust record of a remote workspace. The caller receives
// the store (nil when trust is unavailable), the state and a warning for the status row.
// It uses the same store file as local decisions and the explicit record key, so a
// reconnect with the same target finds the recorded level and does not prompt again.
func openRemoteTrust(spec *authority.RemoteSpec) (*trust.Store, trust.State, string) {
	target := "SSH:" + remoteTrustTarget(spec)
	store, err := newTrustStore()
	if err != nil {
		return nil, trust.State{Root: target, Level: trust.Trusted, Decided: true}, "workspace trust: " + err.Error()
	}
	state, err := store.OpenRecord(remoteTrustKey(spec), nil)
	state.Root = target
	if err != nil {
		return store, state, "workspace trust: " + err.Error()
	}
	return store, state, ""
}
