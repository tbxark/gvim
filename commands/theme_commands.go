package commands

// The theme rows gvim's own native editor adds to the palette. They are written
// by hand, like the other native rows, so the generated table stays Fresh's.
func init() {
	pluginCommands = append([]builtinCommand{
		{Name: "Edit Theme", Description: "Open the native theme editor over the active theme", Action: "edit_theme", Contexts: []string{"Normal", "FileExplorer", "Terminal"}},
	}, pluginCommands...)
}
