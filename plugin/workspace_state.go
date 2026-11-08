package plugin

// Per-workspace plugin state (#19b slice). A direct launch keeps the editor-wide plugin
// state: one gvim.state file per plugin under the config directory. A daemon workspace
// instead gives its plugin runtime one workspace-scoped state file in the workspace's
// session directory (sessions/<stable-id>/plugin-state.json); a workspace switch to another
// materialized runtime therefore talks to that workspace's own state, and a fresh
// materialization seeds from it. The file is versioned (a newer version is never
// overwritten), bounded (per-plugin maxState, whole file maxStateFile) and atomic. The
// editor-wide parts - plugin enablement/settings and the plugin manager's toggles - stay in
// their shared files. The shared-runtime/chrome-scope model of Fresh's
// plugin-window-scope-plan is a follow-up: this slice namespaces the state a runtime writes,
// it does not merge the runtimes.

import (
	"encoding/json"
	"fmt"
)

// WorkspaceStateVersion is the version of the per-workspace plugin-state file.
const WorkspaceStateVersion = 1

// WorkspaceState is one workspace's gvim.state content: plugin name -> key -> encoded JSON
// value, exactly the shape a plugin's editor-wide state file has.
type WorkspaceState struct {
	Version int
	Plugins map[string]map[string]json.RawMessage
}

// ParseWorkspaceState reads a workspace plugin-state file. Empty data is an empty state.
// A version newer than this binary reports readOnly=true with no error: the app passes that
// to the runtime, which refuses writes and never overwrites the file. Damaged JSON is an
// error; the caller treats it as an empty state (and may say so once).
func ParseWorkspaceState(data []byte) (*WorkspaceState, bool, error) {
	if len(data) == 0 {
		return nil, false, nil
	}
	var f struct {
		Version int                                   `json:"version"`
		Plugins map[string]map[string]json.RawMessage `json:"plugins"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, false, fmt.Errorf("workspace plugin state: %w", err)
	}
	switch {
	case f.Version > WorkspaceStateVersion:
		return nil, true, nil
	case f.Version < 1:
		return nil, false, fmt.Errorf("workspace plugin state: invalid version %d", f.Version)
	}
	if f.Plugins == nil {
		f.Plugins = map[string]map[string]json.RawMessage{}
	}
	return &WorkspaceState{Version: f.Version, Plugins: f.Plugins}, false, nil
}
