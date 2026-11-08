package app

import "fmt"

// pluginAct runs one Plugins page action. The page's first increment is the
// inventory and the enabled toggle; per-plugin settings editing arrives with the
// plugin runtime's settings schema seam (docs/settings.md §6).
func (s *settingsService) pluginAct(action, value string) (string, error) {
	return "", fmt.Errorf("settings: plugin action %q is not available yet", action)
}
