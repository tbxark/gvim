package tui

import "github.com/tbxark/gvim/pluginapi"

// pluginHost's EnvHost implementation: the plugin-visible gvim.env API over the
// app's EnvPort. Detection and the activation trust gate live in the host; the
// methods here only report and forward, then raise env_changed for listeners.

func (h *pluginHost) DetectedEnv() (pluginapi.EnvInfo, bool) {
	if h.model().env == nil {
		return pluginapi.EnvInfo{}, false
	}
	return h.model().env.DetectedEnv()
}

func (h *pluginHost) ActiveEnv() (pluginapi.EnvInfo, bool) {
	if h.model().env == nil {
		return pluginapi.EnvInfo{}, false
	}
	return h.model().env.ActiveEnv()
}

func (h *pluginHost) EnvTrustLevel() string {
	if h.model().env == nil {
		return "restricted"
	}
	return h.model().env.TrustLevel()
}

func (h *pluginHost) SetEnv(plugin, snippet, dir, kind string) error {
	if h.model().env == nil {
		return pluginapi.Errf(pluginapi.CodeUnsupported, "env activation is not supported here")
	}
	if err := h.model().env.SetEnv(snippet, dir, kind); err != nil {
		return err
	}
	h.model().emitEnvChanged()
	return nil
}

func (h *pluginHost) ClearEnv(plugin string) error {
	if h.model().env == nil {
		return pluginapi.Errf(pluginapi.CodeUnsupported, "env activation is not supported here")
	}
	if err := h.model().env.ClearEnv(); err != nil {
		return err
	}
	h.model().emitEnvChanged()
	return nil
}

// emitEnvChanged raises env_changed after the active environment changed. The
// payload carries the detected and active environment so a status pill can
// re-render without another host call.
func (m *Model) emitEnvChanged() {
	if !m.plugWants("env_changed") {
		return
	}
	payload := map[string]any{"detected": false, "active": false, "name": "", "kind": ""}
	if m.env != nil {
		if info, ok := m.env.DetectedEnv(); ok {
			payload["detected"] = true
			payload["name"] = info.Name
			payload["kind"] = info.Kind
		}
		if info, ok := m.env.ActiveEnv(); ok {
			payload["active"] = true
			payload["name"] = info.Name
			payload["kind"] = info.Kind
		}
	}
	m.plugEmit("env_changed", payload)
}

// emitTrustChanged raises trust_changed after a recorded workspace trust decision
// (the env-manager plugin activates on the user's single Trust decision).
func (m *Model) emitTrustChanged(level string) {
	if !m.plugWants("trust_changed") {
		return
	}
	m.plugEmit("trust_changed", map[string]any{"level": level})
}
