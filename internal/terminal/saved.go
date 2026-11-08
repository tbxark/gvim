package terminal

// SavedStateVersion is the version of the terminal state the session directory
// keeps. A loader ignores a state from a version it does not know.
const SavedStateVersion = 1

// SavedState is the terminal dock state that survives an editor restart: the
// dock's open flag and directory, the pane width the transcript was captured at,
// and the newest transcript rows as text. Running processes do not survive: a
// restored terminal comes back read-only and dead until the user restarts it.
type SavedState struct {
	Version int      `json:"version"`
	Open    bool     `json:"open,omitempty"`
	Cwd     string   `json:"cwd,omitempty"`
	Width   int      `json:"width,omitempty"`
	Rows    []string `json:"rows,omitempty"`
}
