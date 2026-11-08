package pluginhost

import (
	"strings"
	"testing"
)

// TestMergeEnvFromAppliesOverlayAndProcessEnv: the active project environment's
// overlay is inherited by plugin processes, and an explicit process env wins.
func TestMergeEnvFromAppliesOverlayAndProcessEnv(t *testing.T) {
	env := mergeEnvFrom([]string{"GVIM_OVERLAY=on", "GVIM_OVERRIDE=overlay"}, map[string]string{"GVIM_OVERRIDE": "plugin"})
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "GVIM_OVERLAY=on") {
		t.Errorf("overlay missing from %v", env)
	}
	if !strings.Contains(joined, "GVIM_OVERRIDE=plugin") {
		t.Errorf("process env did not win in %v", env)
	}
	for _, entry := range env {
		if strings.HasPrefix(entry, "GVIM_OVERRIDE=") && entry != "GVIM_OVERRIDE=plugin" {
			t.Errorf("duplicate/overridden entry %q", entry)
		}
	}
}
