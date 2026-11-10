package pluginapi

// AgentSession is one discovered agent/terminal session exposed to plugins. It is a
// read-only view: no process handle, no resume or attach argv. Path is the file a UI can
// open for a transcript row, or the working directory of a tmux row; Dir is the working
// directory. Kind distinguishes the two row shapes ("tmux" or "history").
type AgentSession struct {
	Kind       string   // "tmux" or "history"
	Tool       string   // "tmux", "claude-code" or "codex-cli"
	ID         string   // stable source/name identity
	Title      string   // display title
	Path       string   // open target: the transcript file, or the tmux working directory
	Dir        string   // recorded working directory, when it is known
	Agent      string   // argv0 basename of a transcript's agent, or ""
	Agents     []string // probable tmux agent pane names
	Attached   bool     // a tmux client is attached to the session
	SocketOnly bool     // the tmux server was not queried; its list is unknown
	Exact      bool     // whether a history resume would name this exact session
	ModTime    int64    // transcript modification time, Unix seconds (0 when unknown)
}

// AgentReport is one bounded agent-discover scan. Supported is false when the session has
// no discovery capability at all; a supported scan can still be empty. Problems are bounded
// readable reasons; Truncated says a scan bound stopped the walk.
type AgentReport struct {
	Supported bool
	Installed bool
	Truncated bool
	Sessions  []AgentSession
	Problems  []string
}
