package plugin

import (
	"slices"

	"github.com/tbxark/gvim/pluginapi"
)

// SettingsSchema and SettingsValues expose a plugin's declared settings read-only to the
// host (the Settings page and tests). They never run Lua: the schema and the effective
// values are the state computeSettings keeps.

// SettingsSchema returns the settings a plugin declared with gvim.settings.define.
func (r *Runtime) SettingsSchema(name string) ([]pluginapi.PluginSettingSpec, bool) {
	p := r.plugins[name]
	if p == nil || p.dead || p.data.settings == nil {
		return nil, false
	}
	st := p.data.settings
	out := make([]pluginapi.PluginSettingSpec, 0, len(st.keys))
	for _, key := range st.keys {
		s := st.specs[key]
		if s == nil {
			continue
		}
		def := s.def
		if l, ok := def.([]string); ok {
			def = slices.Clone(l)
		}
		spec := pluginapi.PluginSettingSpec{
			Key: key, Description: s.desc, Type: s.typ, Default: def,
			Choices: slices.Clone(s.values), Min: s.min, Max: s.max,
			Project: s.project,
		}
		out = append(out, spec)
	}
	return out, true
}

// SettingsValues returns the effective value of each declared setting.
func (r *Runtime) SettingsValues(name string) ([]pluginapi.PluginSettingValue, bool) {
	p := r.plugins[name]
	if p == nil || p.dead || p.data.settings == nil {
		return nil, false
	}
	st := p.data.settings
	out := make([]pluginapi.PluginSettingValue, 0, len(st.keys))
	for _, key := range st.keys {
		v, ok := st.values[key]
		if !ok {
			continue
		}
		if l, ok := v.([]string); ok {
			v = slices.Clone(l)
		}
		out = append(out, pluginapi.PluginSettingValue{Key: key, Value: v, Project: st.projects[key]})
	}
	return out, true
}
