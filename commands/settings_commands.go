package commands

// The settings rows that gvim's own Settings page adds to the palette. They are
// written by hand, so the generated table stays Fresh's; Commands lists them with
// the other native rows, after the generated ones and before the plugin manager's.
func init() {
	pluginCommands = append([]builtinCommand{
		{Name: "Reload Configuration", Description: "Re-read the config files from disk and apply the settings that can change without a restart", Action: "reload_config", Contexts: []string{"Normal", "FileExplorer", "Terminal"}},
	}, pluginCommands...)
}
