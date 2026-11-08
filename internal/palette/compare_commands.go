package palette

// compareCommands are the rows that start a comparison of two files, as Fresh's compare_files
// plugin registers them: they work with no comparison open. They are written by hand and are
// not part of the generated table. The rows that work on an open comparison (next and
// previous difference and the rest) are listed by the root while a compare view is open.
// Commands puts these rows after the generated ones and before the plugin manager's.
var compareCommands = []builtinCommand{
	{Name: "Compare Files...", Description: "Show two files side by side, both editable, with their differences aligned and highlighted", Action: "compare_files", Contexts: []string{"Normal", "FileExplorer", "Terminal"}},
	{Name: "Compare Active File With...", Description: "Show the active file side by side with another file", Action: "compare_with_file", Contexts: []string{"Normal", "FileExplorer", "Terminal"}},
}
