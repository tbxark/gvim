package pluginapi

// Plugin declared-settings schema and effective values (round-9 R8-3): a read-only view
// of a plugin's gvim.settings.define schema for the host, the Settings page and tests.
// The plugin still reads the live values through gvim.settings; the runtime owns them.

// PluginSettingSpec is one declared setting.
type PluginSettingSpec struct {
	Key         string
	Description string // the define description, may be ""
	Type        string // "boolean" | "integer" | "number" | "string" | "enum" | "string_list"
	Default     any
	Choices     []string // enum values
	Min, Max    *float64 // integer/number bounds, nil when unbounded
	Project     bool     // project config may override it
}

// PluginSettingValue is the effective value of one declared setting.
type PluginSettingValue struct {
	Key   string
	Value any // bool, float64, string or []string
	// Project says the project config layer supplied the value (when the setting is
	// project = true); a user-config value otherwise.
	Project bool
}
