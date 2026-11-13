package session

// SidebarState is the sidebar's per-session UI state, saved beside the explorer
// state: which native sections stand where, their sizes, the explorer's width form
// and side, and the scroll positions of the explorer and Contents trees. It is a
// bounded snapshot of numbers and booleans (no file contents, no diagnostics).
//
// The field is optional (omitempty) and every zero value means the default, so a
// session without sidebar state keeps the config defaults.
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
	ContentsScroll    int  `json:"contentsScroll,omitzero"`
	// The Diagnostics section's open/collapsed state and its rows.
	DiagnosticsOpen      bool `json:"diagnosticsOpen,omitzero"`
	DiagnosticsCollapsed bool `json:"diagnosticsCollapsed,omitzero"`
	// SectionRows is the per-section explicit body height map (0 or absent = flex; keys explorer, contents,
	// diagnostics and plugin:<id>), bounded on collect and clamped on restore.
	SectionRows map[string]int `json:"sectionRows,omitempty"`
	// PluginOrder and PluginCollapsed are the plugin sections' stack order and folded
	// state, by the plugin panel id (their rows are in SectionRows as plugin:<id>). They are
	// optional and bounded (at most 64 sections, rows clamped, unknown ids ignored
	// on load). Section body contents are never stored.
	PluginOrder     []string        `json:"pluginOrder,omitempty"`
	PluginCollapsed map[string]bool `json:"pluginCollapsed,omitempty"`
}
