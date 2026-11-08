package commands

// The update rows gvim's native update check adds to the palette. Fresh has
// "Update fresh" and "Show update output" for its self-updater; gvim does not
// install releases, so its rows only ask the feed and show what it answered
// (hand-written English, like the other native rows; see docs/ui.md).
func init() {
	pluginCommands = append([]builtinCommand{
		{Name: "Check for Updates", Description: "Ask the release feed whether a newer gvim is published", Action: "check_for_updates", Contexts: []string{"Normal", "FileExplorer", "Terminal"}},
		{Name: "Show Update Output", Description: "Show the release check details", Action: "show_update_output", Contexts: []string{"Normal", "FileExplorer", "Terminal"}},
	}, pluginCommands...)
}
