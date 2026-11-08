package commands

// searchCommands are the project-wide search and replace rows. Fresh ships the panel as
// the bundled plugin search_replace.ts and the git grep finder as git_grep.ts, which
// register their commands as plugin commands; gvim builds both into the TUI, so their
// entry points are hand-written rows like the compare and markdown ones.
func init() {
	pluginCommands = append([]builtinCommand{
		{Name: "Search and Replace in Project", Description: "Search every project file and replace across files, with a grouped result list", Action: "start_search_replace", Contexts: []string{"Normal", "FileExplorer", "Terminal"}},
		{Name: "Git Grep", Description: "Search the tracked files of the git work tree with git grep, with a result list and a case toggle", Action: "start_git_grep", Contexts: []string{"Normal", "FileExplorer", "Terminal"}},
		{Name: "Project Search Results in List", Description: "Show the last project search's matches as a filterable list to jump through", Action: "project_search_results", Contexts: []string{"Normal", "FileExplorer", "Terminal"}},
	}, pluginCommands...)
}
