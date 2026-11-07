package palette

// pluginCommands are the rows for the Lua plugin manager. Fresh has no such rows (its
// plugins have a different manager), so they are written by hand and are not part of the
// generated table. Commands appends them after the generated rows.
var pluginCommands = []builtinCommand{
	{Name: "Plugin: Manager", Description: "List the Lua plugins; enable, disable, reload or inspect one", Action: "plugin_manager", Contexts: []string{"Normal", "FileExplorer", "Terminal"}},
	{Name: "Plugin: Show Log", Description: "Open the plugin log in a new buffer, newest line last", Action: "plugin_show_log", Contexts: []string{"Normal", "FileExplorer", "Terminal"}},
	{Name: "Plugin: Reload...", Description: "Pick a plugin and reload it from its source", Action: "plugin_reload", Contexts: []string{"Normal", "FileExplorer", "Terminal"}},
	{Name: "Plugin: Reload All", Description: "Read every plugin again and reload it", Action: "plugin_reload_all", Contexts: []string{"Normal", "FileExplorer", "Terminal"}},
}

// gvimRename is the text a generated row gets in gvim.
type gvimRename struct{ Name, Description string }

// gvimRenames rewrites the generated rows whose text names Fresh's init.ts, so the palette
// says init.lua, which is gvim's script. An empty Name keeps the generated name. The
// legacy palette tests compare against the generated text (freshCommands), so renaming a
// row does not touch testdata/legacy_lists.json.
var gvimRenames = map[string]gvimRename{
	"init_check":                {"init: Check init.lua", "Syntax-check init.lua in gvim's config directory and report errors"},
	"init_edit":                 {"init: Edit init.lua", "Open init.lua in gvim's config directory (creates it from a template if missing)"},
	"init_reload":               {"init: Reload init.lua", "Reload init.lua via the plugin pipeline"},
	"prompt_promote_macro":      {"Macro: Promote to command", "Turn a recorded macro into an editable init.lua command you can extend with arbitrary code"},
	"prompt_save_macro_to_init": {"Macro: Save to init.lua", "Persist a recorded macro to init.lua as a command that replays its steps"},
}
