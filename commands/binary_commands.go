package commands

// binaryCommands are the hand-written rows for inspecting non-text files. A
// binary file opens as a read-only summary; Toggle Hex View switches it to the
// bounded, read-only hex dump and back. The command is offered in the editor
// contexts because it also explains itself on a text tab.
var binaryCommands = []builtinCommand{
	{
		Name:        "Toggle Hex View",
		Description: "Switch a binary file between its summary and a read-only hex dump",
		Action:      "toggle_hex_view",
		Contexts:    []string{"Normal", "FileExplorer", "Terminal"},
	},
}
