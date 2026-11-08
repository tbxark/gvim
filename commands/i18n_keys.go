package commands

// The extraction table for command titles whose generated Fresh key cannot be used:
// hand-written gvim-only rows (terminal tabs, Run Agent, plugin manager, compare/
// markdown rows, ...) and the generated rows gvim renames (their Fresh translation
// names Fresh's file). The generated table keeps Name/Description as the English
// source and fallback; Commands() attaches these keys, and the palette resolves them
// through i18n with the explicit English fallback (never the bare key).
//
// Regenerate the generated half with commands/gen/generate.py (never hand-edit
// commands_gen.go); this table is hand-maintained and grows when a new gvim-only row
// is added.
var gvimCommandKeys = map[string]string{
	"compare_files":              "gvim.command.compare_files",
	"compare_with_file":          "gvim.command.compare_with_file",
	"edit_theme":                 "gvim.command.edit_theme",
	"init_check":                 "gvim.command.init_check",
	"init_edit":                  "gvim.command.init_edit",
	"init_reload":                "gvim.command.init_reload",
	"markdown_cycle_mode":        "gvim.command.markdown_cycle_mode",
	"markdown_mode_dual":         "gvim.command.markdown_mode_dual",
	"markdown_mode_rendered":     "gvim.command.markdown_mode_rendered",
	"markdown_mode_source":       "gvim.command.markdown_mode_source",
	"markdown_toc_close":         "gvim.command.markdown_toc_close",
	"markdown_toc_show":          "gvim.command.markdown_toc_show",
	"markdown_toc_toggle_follow": "gvim.command.markdown_toc_toggle_follow",
	"markdown_toggle_dual":       "gvim.command.markdown_toggle_dual",
	"menu_activate":              "gvim.command.menu_activate",
	"open_diagnostics_list":      "gvim.command.open_diagnostics_list",
	"open_live_grep":             "gvim.command.open_live_grep",
	"plugin_manager":             "gvim.command.plugin_manager",
	"plugin_reload":              "gvim.command.plugin_reload",
	"plugin_reload_all":          "gvim.command.plugin_reload_all",
	"plugin_show_log":            "gvim.command.plugin_show_log",
	"project_search_results":     "gvim.command.project_search_results",
	"prompt_promote_macro":       "gvim.command.prompt_promote_macro",
	"prompt_save_macro_to_init":  "gvim.command.prompt_save_macro_to_init",
	"reload_config":              "gvim.command.reload_config",
	"run_agent":                  "gvim.command.run_agent",
	"start_git_grep":             "gvim.command.start_git_grep",
	"start_search_replace":       "gvim.command.start_search_replace",
	"terminal_tab_close":         "gvim.command.terminal_tab_close",
	"terminal_tab_new":           "gvim.command.terminal_tab_new",
	"terminal_tab_next":          "gvim.command.terminal_tab_next",
	"terminal_tab_previous":      "gvim.command.terminal_tab_previous",
	"terminal_tab_rename":        "gvim.command.terminal_tab_rename",
	"toggle_diagnostics_panel":   "gvim.command.toggle_diagnostics_panel",
}

// gvimCommandDescKeys is the description half of the table; descriptions are not
// extracted yet, so it is empty and kept for the next slice.
var gvimCommandDescKeys = map[string]string{}
