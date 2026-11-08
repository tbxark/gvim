package terminal

// SavedStateVersion is the version of the terminal state the session directory
// keeps. A loader ignores a state from a version it does not know.
const SavedStateVersion = 1

// SavedState is the terminal dock state that survives an editor restart: the
// dock's open flag and directory, the pane width the transcript was captured at,
// the newest transcript rows as text, the launch record of the terminal that
// produced them (the agent metadata), and whether a process was running at the
// capture (Live). Running processes do not survive the restart. A tab that was
// live at the capture is restored read-only/dead and may be revived the first time
// the user dives into it (switches to or focuses the tab) when
// terminal.restore_live is on: Fresh's model starts a fresh PTY then, with the
// recorded agent-resume/launch argv precedence gated by terminal.resume_agents. A
// tab that was already exited at the capture (Live false — the default for an old
// record and for every tab that was not running) is never spawned automatically:
// it comes back read-only/dead with its exit marker, and only an explicit Restart
// starts anything.
type SavedState struct {
	Version int      `json:"version"`
	Open    bool     `json:"open,omitempty"`
	Cwd     string   `json:"cwd,omitempty"`
	Width   int      `json:"width,omitempty"`
	Rows    []string `json:"rows,omitempty"`
	// LaunchArgv is the command the terminal was started with, ResumeArgv the argv
	// that rejoins its agent session, Agent the configured agent name and
	// Ephemeral that it was a session terminal (Run Agent). Empty fields mean a
	// plain shell and keep the older records readable.
	LaunchArgv []string `json:"launch_argv,omitempty"`
	ResumeArgv []string `json:"resume_argv,omitempty"`
	Agent      string   `json:"agent,omitempty"`
	Ephemeral  bool     `json:"ephemeral,omitempty"`
	// LaunchDir is the directory the launched program was started in. Cwd is the
	// terminal's current directory (the shell's last OSC 7 report); an agent's
	// internal chdir must not move a later restart or resume away from the chosen
	// launch directory, so the two are kept apart. Additive: an older record has no
	// launch_dir and keeps its cwd as the launch directory on restore.
	LaunchDir string `json:"launch_dir,omitempty"`
	// Source is the durable external identity of a discovered session (for example
	// "tmux:default/work"), so a repeated import can find the tab that already
	// attaches it. Additive; empty for a configured agent or a plain shell.
	Source string `json:"source,omitempty"`
	// Live records whether a process was running when the state was captured. It is
	// additive and optional (an old record has no live flag and is treated as
	// exited, so it restores exactly as before); a live-at-capture tab may be
	// revived on the user's first dive into it, an exited one never is.
	Live bool `json:"live,omitempty"`
	// Screen is the bounded styled snapshot of the visible screen at the capture.
	// It is additive and optional (an old record has none and restores the plain
	// text transcript exactly as before); a restored tab paints the snapshot above
	// the new shell after a revive, giving the saved screen its styles back instead
	// of plain rows. See SavedScreen for the bounds.
	Screen *SavedScreen `json:"screen,omitempty"`
	// Tabs holds every dock terminal tab when the dock had more than the one the
	// top-level fields describe. The top-level fields mirror the active tab, so a
	// reader that does not know Tabs still restores that tab; a reader that does
	// uses Tabs and ActiveTab and ignores the mirror. The field is additive and
	// optional (an older record has no tabs and restores exactly as before).
	Tabs      []SavedTab `json:"tabs,omitempty"`
	ActiveTab int        `json:"active_tab,omitzero"`
}

// SavedTab is one terminal tab in a saved dock. Its fields mirror SavedState's
// transcript and launch record; a restored tab is read-only and dead until the
// user restarts it, exactly like a single saved terminal. Names are display-only.
type SavedTab struct {
	Name       string   `json:"name,omitempty"`
	Open       bool     `json:"open,omitempty"`
	Cwd        string   `json:"cwd,omitempty"`
	Width      int      `json:"width,omitempty"`
	Rows       []string `json:"rows,omitempty"`
	LaunchArgv []string `json:"launch_argv,omitempty"`
	ResumeArgv []string `json:"resume_argv,omitempty"`
	Agent      string   `json:"agent,omitempty"`
	Ephemeral  bool     `json:"ephemeral,omitempty"`
	LaunchDir  string   `json:"launch_dir,omitempty"`
	Source     string   `json:"source,omitempty"`
	// Live mirrors SavedState.Live for this tab: a process was running when the tab
	// was captured. Absent means exited (the old-record meaning).
	Live bool `json:"live,omitempty"`
	// Screen mirrors SavedState.Screen for this tab.
	Screen *SavedScreen `json:"screen,omitempty"`
}
