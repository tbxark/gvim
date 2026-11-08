package app

import (
	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/env"
	"github.com/tbxark/gvim/trust"
)

// workspaceEnv is the environment provider surface of one session. The local session
// mounts env.Provider (local detection and local capture); a remote session mounts
// env.Remote, whose detection and activation run on the remote host (#R13-13), so the
// local overlay never crosses the wire. The rest of the app (plugin env port, Settings
// env rows, spawn paths) reads this interface and does not know which host answers.
type workspaceEnv interface {
	// Detected is the environment detected at the workspace root, if any.
	Detected() (trust.DetectedEnv, bool)
	// Active is the activated environment, if a recipe is active.
	Active() (env.Info, bool)
	// Trusted reports the live trust gate: activation runs project shell code.
	Trusted() bool
	// Detectors are the configured detectors.
	Detectors() []config.EnvDetector
	// Overlay is the set-only delta entries (KEY=VALUE) of the active environment.
	Overlay() []string
	// Unset is the keys the activation removed.
	Unset() []string
	// LastError is the last capture error, if the last capture failed.
	LastError() error
	// Set activates a recipe (the plugin env-manager path); only trusted workspaces.
	Set(snippet, dir string, kind config.EnvKind) error
	// Clear deactivates the environment.
	Clear()
	// Refresh drops the capture cache (Env: Reload).
	Refresh()
}

// localEnvOverlay is the overlay a LOCAL child process sees: the session env provider's
// overlay for a local launch, and none for a remote launch (its local children - the
// plugin host - must not inherit an environment captured for the remote host, and the
// remote host must never receive the local overlay).
func localEnvOverlay(provider workspaceEnv, remote bool) func() []string {
	if remote || provider == nil {
		return nil
	}
	return provider.Overlay
}
