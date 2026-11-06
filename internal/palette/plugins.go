package palette

import "strings"

// SourcePlugin is the Source of a command that a plugin registered.
const SourcePlugin = "plugin"

// PluginActionPrefix starts the Action of a plugin command. The rest of the
// Action is the command name the plugin host knows it by.
const PluginActionPrefix = "plugin:"

// PluginEntry returns the palette entry of the plugin command called name, shown
// with label.
func PluginEntry(name, label string) Entry {
	return Entry{
		Name:        label,
		Description: "Command from a plugin",
		Action:      PluginActionPrefix + name,
		Source:      SourcePlugin,
	}
}

// PluginName returns the plugin command name that action runs. It returns false
// when action is not a plugin command.
func PluginName(action string) (string, bool) {
	name, ok := strings.CutPrefix(action, PluginActionPrefix)
	if !ok {
		return "", false
	}
	return name, true
}
