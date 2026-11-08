package pluginapi

// Environment detection and activation (the env-manager host surface).
//
// Detection, the trust gate and spawn application live in the app (see the env
// package); this interface is the plugin-visible seam. A plugin can read the
// detected and active environment, ask the live trust level and activate or
// deactivate an environment. SetEnv is host-gated: outside a trusted workspace it
// returns an error and never runs the snippet.

// EnvInfo describes one detected or active environment.
type EnvInfo struct {
	// Name is the detector label, e.g. ".venv" or "direnv".
	Name string
	// Kind is "path-only" or "shell".
	Kind string
	// Snippet is the activation command (with {dir} already expanded).
	Snippet string
}

// EnvHost is the environment part of Host.
type EnvHost interface {
	// DetectedEnv returns the environment detected at the workspace root.
	DetectedEnv() (EnvInfo, bool)
	// ActiveEnv returns the active environment, if one is applied.
	ActiveEnv() (EnvInfo, bool)
	// EnvTrustLevel returns the live workspace trust level: "trusted",
	// "restricted" or "blocked".
	EnvTrustLevel() string
	// SetEnv activates snippet in dir. kind is "path-only" or "shell"; an empty
	// kind means "shell". It fails with CodeInvalid outside a trusted
	// workspace and with CodeInvalid for an empty or oversized snippet.
	SetEnv(plugin, snippet, dir, kind string) error
	// ClearEnv deactivates the plugin's environment.
	ClearEnv(plugin string) error
}
