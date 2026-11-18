package commands

// Additional native gvim palette commands that do not belong in the legacy
// Fresh table or the plugin manager tail block. Prepended in init() so the
// plugin_manager..plugin_reload_all block stays contiguous at the end.
func init() {
	pluginCommands = append([]builtinCommand{
		{
			Name:        "View: Toggle Sticky Scroll",
			Description: "Pin the enclosing function or block header at the top of the editor while scrolling",
			Action:      "toggle_sticky_scroll",
			Contexts:    []string{"Normal"},
		},
		{
			Name:        "Help: About gvim",
			Description: "Show gvim version, Go runtime, platform, and architecture details",
			Action:      "about_gvim",
			Contexts:    []string{"Normal", "FileExplorer", "Terminal"},
		},
	}, pluginCommands...)
}
