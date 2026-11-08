package session

// SidebarState is the sidebar's per-session UI state, saved beside the explorer
// state: which native sections stand where, their sizes, the explorer's width form
// and side, and the scroll positions of the explorer and Contents trees. It is a
// bounded snapshot of numbers and booleans (no file contents, no diagnostics).
//
// The format is migration-safe: the field is optional (omitempty) and every zero
// value means the default, so a session file from an older release decodes with no
// sidebar state and keeps the config defaults, and a reader that does not know the
// field ignores it. Session.Version does not change: this is an additive optional
// object, not a format revision.
type SidebarState struct {
	ExplorerCollapsed bool `json:"explorerCollapsed,omitzero"`
	// WidthMode and WidthValue are the explorer width form: 0 automatic, 1 percent
	// of the frame, 2 columns. Side is "left" or "right".
	WidthMode        int    `json:"widthMode,omitzero"`
	WidthValue       int    `json:"widthValue,omitzero"`
	Side             string `json:"side,omitempty"`
	ExplorerSelected string `json:"explorerSelected,omitempty"`
	ExplorerScroll   int    `json:"explorerScroll,omitzero"`
	// The Contents section's open/collapsed state and its rows and tree position.
	ContentsOpen      bool `json:"contentsOpen,omitzero"`
	ContentsCollapsed bool `json:"contentsCollapsed,omitzero"`
	ContentsRows      int  `json:"contentsRows,omitzero"`
	ContentsScroll    int  `json:"contentsScroll,omitzero"`
	// The Diagnostics section's open/collapsed state and its rows.
	DiagnosticsOpen      bool `json:"diagnosticsOpen,omitzero"`
	DiagnosticsCollapsed bool `json:"diagnosticsCollapsed,omitzero"`
	DiagnosticsRows      int  `json:"diagnosticsRows,omitzero"`
	// PluginOrder, PluginRows and PluginCollapsed are the plugin sections' stack
	// order, requested body rows and folded state, by the plugin panel id. They are
	// optional and bounded (at most 64 sections, rows clamped, unknown ids ignored
	// on load), so an older session keeps the mount order and the defaults, and a
	// reader that does not know them ignores them. Section body contents are never
	// stored.
	PluginOrder     []string        `json:"pluginOrder,omitempty"`
	PluginRows      map[string]int  `json:"pluginRows,omitempty"`
	PluginCollapsed map[string]bool `json:"pluginCollapsed,omitempty"`
}
