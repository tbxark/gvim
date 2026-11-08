package commands

// The terminal rows gvim's own native editor adds to the palette. They are written
// by hand, like the other native rows, so the generated table stays Fresh's.
//
// "Run Agent..." has no Fresh CommandDef (Fresh's agent sessions are plugin data,
// not a command row), so it is gvim's own English title.
func init() {
	pluginCommands = append([]builtinCommand{
		{Name: "Run Agent...", Description: "Start a configured terminal agent in the dock (terminal.agents)", Action: "run_agent", Contexts: []string{"Normal", "FileExplorer", "Terminal"}},
	}, pluginCommands...)
}

// terminalCommands are the dock terminal tab rows. Fresh has no terminal-tab
// commands (its tab commands are for editor buffers), so the titles are
// hand-written English, per docs/i18n.md's boundary for hand-written command
// titles; the quick-switch keys are documented in docs/terminal.md rather than
// added to a keymap preset.
var terminalCommands = []builtinCommand{
	{Name: "New Terminal Tab", Description: "Start another shell in a new dock tab", Action: "terminal_tab_new", Contexts: []string{"Normal", "FileExplorer", "Terminal"}},
	{Name: "Close Terminal Tab", Description: "Close the dock tab and its shell", Action: "terminal_tab_close", Contexts: []string{"Normal", "FileExplorer", "Terminal"}},
	{Name: "Next Terminal Tab", Description: "Show the next dock terminal tab", Action: "terminal_tab_next", Contexts: []string{"Normal", "FileExplorer", "Terminal"}},
	{Name: "Previous Terminal Tab", Description: "Show the previous dock terminal tab", Action: "terminal_tab_previous", Contexts: []string{"Normal", "FileExplorer", "Terminal"}},
	{Name: "Rename Terminal Tab", Description: "Name the active dock terminal tab", Action: "terminal_tab_rename", Contexts: []string{"Normal", "FileExplorer", "Terminal"}},
	{Name: "Move Tab To Left", Description: "Move the active terminal tab left", Action: "terminal_tab_move_left", Contexts: []string{"Normal", "FileExplorer", "Terminal"}},
	{Name: "Move Tab To Right", Description: "Move the active terminal tab right", Action: "terminal_tab_move_right", Contexts: []string{"Normal", "FileExplorer", "Terminal"}},
}
