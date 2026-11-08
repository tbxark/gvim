package commands

import "slices"

// SourcePlugin is the Source of a command that a plugin registered.
const SourcePlugin = "plugin"

// PluginEntry returns the palette entry of a plugin command. action is the id the
// plugin runtime owns ("<plugin>.<name>"), title is the label shown in the list and
// description is the second line; an empty description says which plugin the
// command is from. when lists the Holds-style predicates the command needs
// ("has_buffer", "!read_only"); it is copied into Entry.When for the ui to
// evaluate and is not a key-context scope, so it never disables the row here.
func PluginEntry(action, title, description, plugin string, when []string) Entry {
	if description == "" {
		description = "Command from a plugin"
		if plugin != "" {
			description = "Command from plugin " + plugin
		}
	}
	return Entry{
		Name:        title,
		Description: description,
		Action:      action,
		Source:      SourcePlugin,
		When:        slices.Clone(when),
	}
}
