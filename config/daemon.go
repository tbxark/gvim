package config

import (
	"encoding/json"
	"fmt"
)

// The daemon bounds how many LOCAL workspaces it keeps materialized in memory
// (daemon.materialized_workspaces). Only the user config may set it: a repository cannot
// decide how much memory the daemon spends on other workspaces.
const (
	// DefaultMaterializedWorkspaces is the cap when no file sets it. Eight warm local
	// workspaces is enough for ordinary use and bounds the daemon's editor/model memory;
	// each warm workspace costs one runtime plus its documents and terminal screens.
	DefaultMaterializedWorkspaces = 8
	// MinMaterializedWorkspaces is the smallest accepted cap: the active workspace alone
	// must stay materialized.
	MinMaterializedWorkspaces = 1
	// MaxMaterializedWorkspaces bounds the cap: more warm workspaces than this is not a
	// daemon any more (the registry holds the rest as dormant descriptors).
	MaxMaterializedWorkspaces = 64
)

// DaemonConfig is the daemon section of the config: gvim's memory bound for the materialized
// workspaces of a daemon host. It is a gvim extension (Fresh's warm-set cap is not a user
// setting there yet) and only the user layer may set it.
type DaemonConfig struct {
	// MaterializedWorkspaces is daemon.materialized_workspaces: the most local workspaces
	// the daemon keeps materialized at once. Exceeding it evicts the least-recently-focused
	// non-active workspace after capturing its session. Remote/authority workspaces are not
	// evicted (and do not count) in this slice. Default DefaultMaterializedWorkspaces.
	MaterializedWorkspaces int
}

// DefaultDaemonConfig returns the daemon defaults.
func DefaultDaemonConfig() DaemonConfig {
	return DaemonConfig{MaterializedWorkspaces: DefaultMaterializedWorkspaces}
}

// setDaemon applies the object of the "daemon" section from the user layer. A project file
// never comes here: applyObject refuses the section for the project layer with a diagnostic.
func (c *Config) setDaemon(raw json.RawMessage) (diags []Diagnostic) {
	diag := func(key, message string) {
		diags = append(diags, Diagnostic{Key: "daemon." + key, Message: message})
	}
	entries, err := decodeObject(raw)
	if err != nil {
		return []Diagnostic{{Key: "daemon", Message: err.Error()}}
	}
	for _, k := range sortedKeys(entries) {
		v := entries[k]
		if isNull(v) {
			diag(k, "null is not a valid value")
			continue
		}
		switch k {
		case "materialized_workspaces":
			var n int
			if err := json.Unmarshal(v, &n); err != nil {
				diag(k, "must be an integer")
				continue
			}
			if err := validateMaterializedWorkspaces(n); err != nil {
				diag(k, err.Error())
				continue
			}
			c.Daemon.MaterializedWorkspaces = n
		default:
			diag(k, "unknown setting")
		}
	}
	return diags
}

// validateMaterializedWorkspaces checks one daemon.materialized_workspaces value.
func validateMaterializedWorkspaces(n int) error {
	if n < MinMaterializedWorkspaces || n > MaxMaterializedWorkspaces {
		return fmt.Errorf("must be between %d and %d, got %d", MinMaterializedWorkspaces, MaxMaterializedWorkspaces, n)
	}
	return nil
}
