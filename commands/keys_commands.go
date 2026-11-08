package commands

// The keymap presets keep only the common shortcuts, so the commands that lost their key
// need a palette row, and the rows that Fresh does not generate for gvim's own features
// are written here. They go in front of pluginCommands, which Commands lists after the
// generated rows, so this file does not touch the generated table or the legacy lists and
// the plugin manager rows stay last.
func init() {
	pluginCommands = append([]builtinCommand{
		{Name: "Open Menu Bar", Description: "Open the menu bar on its first menu; Left and Right switch menus, Esc closes (no default key)", Action: "menu_activate", Contexts: []string{"Normal", "FileExplorer", "Terminal"}},
		{Name: "Live Grep", Description: "Search the project's files with a result list that updates as you type (search only: Replace works on one buffer)", Action: "open_live_grep", Contexts: []string{"Normal"}},
	}, pluginCommands...)
}
