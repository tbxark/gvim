package terminal

// SavedStateVersion is the version of the terminal state the session directory
// keeps. A loader ignores a state from a version it does not know.
const SavedStateVersion = 1

// Bounds on the additive search-query history in a saved terminal state. They are
// exported so the dock's in-memory history and the restore loader apply the same
// limits (Fresh's InputHistory cap is 100).
const (
	MaxSearchHistoryEntries = 100
	MaxSearchQueryBytes     = 512
)

// SavedState is the terminal dock state that survives an editor restart: every dock
// tab as a SavedTab, the active tab's index and the dock's scrollback-search query
// history. Running processes do not survive the restart. A tab that was live at the
// capture (SavedTab.Live) is restored read-only/dead and may be revived the first time
// the user dives into it (switches to or focuses the tab) when terminal.restore_live
// is on: Fresh's model starts a fresh PTY then, with the recorded agent-resume/launch
// argv precedence gated by terminal.resume_agents. A tab that was already exited at the
// capture (Live false) is never spawned automatically: it comes back read-only/dead with
// its exit marker, and only an explicit Restart starts anything.
type SavedState struct {
	Version int `json:"version"`
	// SearchHistory is the dock's scrollback-search query history (oldest first,
	// newest last): Fresh keeps its search prompt history per window and persists it
	// with the workspace, and gvim's dock mirrors that for its pager. It is
	// bounded/cleaned on load, and it never starts a process; it only restores what
	// the search input recalls.
	SearchHistory []string `json:"search_history,omitempty"`
	// Tabs holds every dock terminal tab, and ActiveTab is the index of the active one.
	Tabs      []SavedTab `json:"tabs,omitempty"`
	ActiveTab int        `json:"active_tab,omitzero"`
}

// SavedTab is one terminal tab in a saved dock: its open flag and directory, the pane
// width the transcript was captured at, the newest transcript rows as text, the launch
// record of the terminal that produced them (the agent metadata) and how its process
// ended. A restored tab is read-only and dead until the user restarts it. Name is
// display-only.
type SavedTab struct {
	Name  string   `json:"name,omitempty"`
	Open  bool     `json:"open,omitempty"`
	Cwd   string   `json:"cwd,omitempty"`
	Width int      `json:"width,omitempty"`
	Rows  []string `json:"rows,omitempty"`
	// LaunchArgv is the command the terminal was started with, ResumeArgv the argv
	// that rejoins its agent session, Agent the configured agent name and Ephemeral
	// that it was a session terminal (Run Agent). Empty fields mean a plain shell.
	LaunchArgv []string `json:"launch_argv,omitempty"`
	ResumeArgv []string `json:"resume_argv,omitempty"`
	Agent      string   `json:"agent,omitempty"`
	Ephemeral  bool     `json:"ephemeral,omitempty"`
	// LaunchDir is the directory the launched program was started in. Cwd is the
	// terminal's current directory (the shell's last OSC 7 report); an agent's
	// internal chdir must not move a later restart or resume away from the chosen
	// launch directory, so the two are kept apart.
	LaunchDir string `json:"launch_dir,omitempty"`
	// Source is the durable external identity of a discovered session (for example
	// "tmux:default/work"), so a repeated import can find the tab that already
	// attaches it. Empty for a configured agent or a plain shell.
	Source string `json:"source,omitempty"`
	// Live records whether a process was running when the tab was captured. A
	// live-at-capture tab may be revived on the user's first dive into it, an exited
	// one never is.
	Live bool `json:"live,omitempty"`
	// Screen is the bounded styled snapshot of the visible screen at the capture; a
	// restored tab paints it above the new shell after a revive, giving the saved
	// screen its styles back instead of plain rows. See SavedScreen for the bounds.
	Screen *SavedScreen `json:"screen,omitempty"`
	// Title is the terminal title the child last set with OSC 0/2 (cleaned and
	// bounded); empty for a child that never set one.
	Title string `json:"title,omitempty"`
	// ExitCode is the child's exit code when the tab was captured from a terminal
	// whose process had exited (ExitKnown says the code was observed).
	ExitCode  int  `json:"exit_code,omitempty"`
	ExitKnown bool `json:"exit_known,omitempty"`
}

// MergeSearchHistory merges a migrated dock's search-query history into a target
// history: target entries stay first (oldest to newest), then source entries are
// appended unless an identical query is already present, and only the newest
// MaxSearchHistoryEntries survive. It keeps the data untrusted: callers clean each
// query (controls/invalid UTF-8) before/after merging.
func MergeSearchHistory(target, source []string) []string {
	if len(target) == 0 && len(source) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(target)+len(source))
	out := make([]string, 0, len(target)+len(source))
	for _, query := range target {
		if query == "" || seen[query] {
			continue
		}
		seen[query] = true
		out = append(out, query)
	}
	for _, query := range source {
		if query == "" || seen[query] {
			continue
		}
		seen[query] = true
		out = append(out, query)
	}
	if over := len(out) - MaxSearchHistoryEntries; over > 0 {
		out = append([]string(nil), out[over:]...)
	}
	return out
}
