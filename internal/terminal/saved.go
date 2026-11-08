package terminal

// SavedStateVersion is the version of the terminal state the session directory
// keeps. A loader ignores a state from a version it does not know.
const SavedStateVersion = 1

// SavedState is the terminal dock state that survives an editor restart: the
// dock's open flag and directory, the pane width the transcript was captured at,
// the newest transcript rows as text, and the launch record of the terminal that
// produced them (the agent metadata). Running processes do not survive: a
// restored terminal comes back read-only and dead until the user restarts it, and
// the restored launch record never starts anything on its own (Fresh's rule: no
// auto-resume on restore).
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
}
