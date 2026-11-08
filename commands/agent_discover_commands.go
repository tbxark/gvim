package commands

// The agent-discovery rows gvim's own native editor adds to the palette. They are
// written by hand, like the other native rows, so the generated table stays
// Fresh's.
//
// The title is gvim's own English: Fresh's agent-discovery command is
// "Orchestrator: Import sessions" and its Import opens a workspace, while this
// reduction opens the discovered tmux session in a terminal tab, so it names what
// it does. When Import-as-workspace lands the row can take the Fresh title.
func init() {
	pluginCommands = append([]builtinCommand{
		{Name: "Discover Agent Sessions...", Description: "Find tmux sessions and agents running outside gvim and open one in a terminal tab", Action: "discover_agent_sessions", Contexts: []string{"Normal", "FileExplorer", "Terminal"}},
	}, pluginCommands...)
}
