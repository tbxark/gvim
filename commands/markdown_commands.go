package commands

// markdownCommands are the Contents sidebar section's command rows. Fresh ships
// the section as the bundled plugin markdown_toc.ts, which registers its
// commands as plugin commands; gvim builds the section into the TUI, so its
// entry points are hand-written rows like the compare ones. They keep the
// palette reachable when auto_open is off, which is the only way to show the
// section then.
var markdownCommands = []builtinCommand{
	{Name: "Markdown: Show Outline", Description: "Show the Contents section in the sidebar for the active Markdown buffer", Action: "markdown_toc_show", Contexts: []string{"Normal", "FileExplorer", "Terminal"}},
	{Name: "Markdown: Close Outline", Description: "Hide the Contents section for the active Markdown buffer", Action: "markdown_toc_close", Contexts: []string{"Normal", "FileExplorer", "Terminal"}},
	{Name: "Markdown: Contents — Follow Cursor/Scroll", Description: "Toggle whether the selected heading follows the cursor or the top of the viewport", Action: "markdown_toc_toggle_follow", Contexts: []string{"Normal", "FileExplorer", "Terminal"}},
}
