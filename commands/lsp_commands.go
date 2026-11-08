package commands

// lspCommands are the diagnostics rows. Fresh ships the diagnostics panel as the
// bundled plugin diagnostics_panel.ts, which registers its command as a plugin
// command; gvim builds the panel into the TUI, so its two entry points are
// hand-written rows like the compare and markdown ones. The list row is the
// native quickfix view of the same workspace diagnostics.
func init() {
	pluginCommands = append([]builtinCommand{
		{Name: "Toggle Diagnostics Sidebar", Description: "Show or hide the native diagnostics section of the sidebar", Action: "toggle_diagnostics_panel", Contexts: []string{"Normal", "FileExplorer", "Terminal"}},
		{Name: "Diagnostics List", Description: "List the workspace diagnostics in a filterable list to jump through", Action: "open_diagnostics_list", Contexts: []string{"Normal", "FileExplorer", "Terminal"}},
	}, pluginCommands...)
}
