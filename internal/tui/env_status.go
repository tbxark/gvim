package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/textsafe"
	"github.com/tbxark/gvim/trust"
)

// envStatusSegment is the status row's active-environment indicator, the gvim
// counterpart of Fresh's env-manager "env" status pill (the plugin registers a
// status-bar element whose value is the environment name/state). It reads only
// the cached EnvPort: no detection, capture, probe, spawn or trust decision runs
// here, so the render path never blocks or touches project shell code. The value
// follows Fresh's statusbar.* text: an active environment shows "<name> ✓" (or
// "active" when the detector had no name), a detected but not activated
// environment shows its name, "(locked)" while the workspace is untrusted, and
// nothing is shown when no environment is detected. Unlike Fresh the segment is
// always on rather than an opt-in status-bar element, so the neutral "system"
// value is omitted to keep the row quiet; docs/parity R13-18 records that.
func (m *Model) envStatusSegment() (statusItem, bool) {
	if m.env == nil {
		return statusItem{}, false
	}
	active, detected, hasActive, hasDetected, trustLevel := m.env.CachedEnvStatus()
	name := ""
	switch {
	case hasActive:
		name = active.Name
		if name == "" && hasDetected {
			name = detected.Name
		}
	case hasDetected:
		name = detected.Name
	}
	// Detector names are configuration/project data: strip Cc/Cf controls and invalid
	// UTF-8 before they reach the terminal (bidi overrides/zero-width spoofing included).
	name = textsafe.Clean(name)
	switch {
	case hasActive:
		// An already-active environment stays shown as active even if workspace trust was
		// revoked later: the indicator is informational, it never re-gates or un-applies an
		// environment (Fresh's envActive branch behaves the same; deactivation is explicit).
		if name != "" {
			return statusItem{ID: "env", Text: uiTextVars("gvim.status.env_active", "%{name} ✓", map[string]any{"name": name}), Prio: 1<<20 - 2, Clickable: true}, true
		}
		return statusItem{ID: "env", Text: uiText("gvim.status.env_active_unknown", "active"), Prio: 1<<20 - 2, Clickable: true}, true
	case hasDetected && name != "":
		if trustLevel == string(trust.Trusted) {
			return statusItem{ID: "env", Text: uiTextVars("gvim.status.env_detected", "%{name}", map[string]any{"name": name}), Prio: 1<<20 - 2, Clickable: true}, true
		}
		return statusItem{ID: "env", Text: uiTextVars("gvim.status.env_locked", "%{name} (locked)", map[string]any{"name": name}), Prio: 1<<20 - 2, Clickable: true}, true
	}
	return statusItem{}, false
}

// envStatusClick is the safe click entry of the active-environment segment, the gvim
// counterpart of Fresh's env-manager status pill click. It never changes trust itself:
// an untrusted workspace opens the existing cancellable Workspace Trust dialog (the same
// surface the trust pill and command use); a trusted workspace dispatches the env-manager
// plugin's own Env: Activate/Reload action (reusing its trust gate, capture and restart
// path — no activation logic lives in the status row); a workspace with no detected env
// just says so. A trust port that is unavailable reports its usual notice. Everything is
// a tea.Cmd/overlay, so Update never blocks; a missing env-manager plugin gets an explicit
// notice instead of an unknown-action fallthrough.
func (m *Model) envStatusClick() tea.Cmd {
	if m.env == nil {
		return nil
	}
	_, _, hasActive, hasDetected, trustLevel := m.env.CachedEnvStatus()
	if !hasActive && !hasDetected {
		m.notice = uiText("gvim.notice.env_none", "No environment manager detected in this project")
		return nil
	}
	if trustLevel != string(trust.Trusted) {
		return m.openWorkspaceTrust()
	}
	action, label := "env_manager.activate", "Activate"
	if hasActive {
		action, label = "env_manager.reload", "Reload"
	}
	if m.plug.rt == nil || !m.plug.rt.Owns(action) {
		if label == "Activate" {
			m.notice = uiText("gvim.notice.env_activate_unavailable", "Env: Activate is unavailable — enable the env-manager plugin")
		} else {
			m.notice = uiText("gvim.notice.env_reload_unavailable", "Env: Reload is unavailable — enable the env-manager plugin")
		}
		return nil
	}
	return m.dispatch(Command{Action: action})
}
