package pluginapi

// Buffer groups (plugin API tier 1b, the "group-lite" form). A group is a named layout of
// panels shown as splits of the current tab; each panel is either a virtual buffer the host
// creates (a read-only panel buffer whose content the plugin sets with SetPanelContent) or an
// existing buffer shown in its own window. gvim has no synthetic group tab: the group owns the
// windows it splits off and the panel buffers it creates, and closes them when it closes; the
// plugin-facing title segments are kept but there is no group tab to paint them in (tier 1c).
//
// Scroll-sync groups tie two split windows together: the follower's top line follows the
// leader through the anchor pairs the plugin sets.

// BufferGroupID names one buffer group. 0 means none.
type BufferGroupID uint64

// BufferGroupLeaf is one panel of a group layout.
type BufferGroupLeaf struct {
	Name string // the panel name the group API uses
	// Buffer shows an existing buffer; 0 makes the host create a read-only panel buffer.
	Buffer BufferID
	// Fixed asks for a fixed display height of Height rows (Fresh's {type="fixed",
	// height=N} leaf). The host re-applies it on every layout change; split borders can
	// leave it one row off, and a fixed leaf inside a ratio split is approximate.
	Fixed  bool
	Height int
}

// BufferGroupLayout is one node of a parsed layout: a leaf, or a split of two children.
// Dir is "h" (side by side, first left) or "v" (first above).
type BufferGroupLayout struct {
	Leaf *BufferGroupLeaf
	Dir  string
	// Ratio is the share of the first child (0 means an even split). The host maps it to
	// the split option, whose ratio is the share of the second window.
	Ratio         float64
	First, Second *BufferGroupLayout
}

// BufferGroupTitleSegment is one piece of a group title: literal text, or the file a panel
// shows (Panel names the panel; a modified buffer is marked by the host).
type BufferGroupTitleSegment struct {
	Text  string
	Panel string
}

// BufferGroupSpec is a createBufferGroup call: the group name and its layout.
type BufferGroupSpec struct {
	Name   string
	Layout BufferGroupLayout
}

// BufferGroupPanel is one panel of a mounted group.
type BufferGroupPanel struct {
	Name    string
	Buffer  BufferID
	Window  WinID
	Visible bool
}

// ScrollAnchor maps a line of the leader window to the follower window.
type ScrollAnchor struct {
	Left, Right int // 0-based line numbers
}
