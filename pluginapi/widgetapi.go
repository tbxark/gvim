package pluginapi

// Declarative widget panels (plugin API tier 1b). A plugin builds a Widget tree, the ui
// host lays it out on the cell grid, paints it, hit-tests it, keeps the focus and reports
// what the user did as a widget_event. The tree is plain data: pluginapi imports the
// standard library only, and the host validates and copies everything it keeps.
//
// The kinds are the interactive ones of Fresh's widget API: the layout containers row and
// col, the display kinds label, text (an editable field), raw, hintBar, spacer, divider,
// the controls button, toggle, list, tree, dropdown, number, radio, dualList and
// labeledSection, and the wrappers component, overlay and popup; windowEmbed draws a
// placeholder because a panel cannot embed a live editor pane.

// PanelID names one widget panel a plugin mounted. 0 means none. Ids are chosen by the
// runtime (not reused while the plugin runs) and the host keys every panel by it.
type PanelID uint64

// WidgetKind names one node of a widget spec.
type WidgetKind string

// The widget kinds of v1.
const (
	WidgetRow     WidgetKind = "row"
	WidgetCol     WidgetKind = "col"
	WidgetLabel   WidgetKind = "label"
	WidgetText    WidgetKind = "text"
	WidgetRaw     WidgetKind = "raw"
	WidgetHintBar WidgetKind = "hintBar"
	WidgetSpacer  WidgetKind = "spacer"
	WidgetDivider WidgetKind = "divider"
	WidgetButton  WidgetKind = "button"
	WidgetToggle  WidgetKind = "toggle"
	WidgetList    WidgetKind = "list"
	WidgetTree    WidgetKind = "tree"

	// Interactive kinds (tier 1b).
	WidgetDropdown       WidgetKind = "dropdown"
	WidgetNumber         WidgetKind = "number"
	WidgetRadio          WidgetKind = "radio"
	WidgetDualList       WidgetKind = "dualList"
	WidgetLabeledSection WidgetKind = "labeledSection"
	// Wrappers: the first child is the wrapped widget.
	WidgetComponent   WidgetKind = "component"
	WidgetOverlay     WidgetKind = "overlay"
	WidgetPopup       WidgetKind = "popup"
	WidgetWindowEmbed WidgetKind = "windowEmbed"
)

// WidgetRun is one styled run of a label, a raw row or a hint entry.
type WidgetRun struct {
	Text  string
	Style Style
}

// WidgetItem is one row of a list.
type WidgetItem struct {
	Key, Text, Detail string
	Checked, Disabled bool
}

// WidgetNode is one node of a tree. Depth is the nesting level (0 for a root row); a node
// with children is a node with a following node of a greater depth. Expanded is the seed
// state of that node's children; the host keeps the state after the first draw.
type WidgetNode struct {
	Key, Text, Detail string
	Depth             int
	Checked, Expanded bool
	// Leaf says the node has no children even when the next node is deeper (a column of
	// flat rows with an indent). It is only needed for odd shapes; depth is enough
	// otherwise.
	Leaf bool
	// Draggable makes the row pickable with the pointer: the host reports drag, drop and
	// dragend widget events, and the plugin moves the row with a previewTreeMove mutation.
	Draggable bool
	// Flush starts a leaf's text at its indent instead of after the disclosure gutter.
	Flush bool
	// ExtraLines are additional styled rows under a card node (item_height > 1).
	ExtraLines []WidgetRun
}

// Widget is one node of a declarative widget tree.
type Widget struct {
	Kind WidgetKind
	// Key is the stable identity of a node across updates: focus, selection, checked
	// state and scroll position are kept for matching keys.
	Key string

	// Children are the row and col layout children.
	Children []Widget

	// Text is the label text, the read-only text block's value (split on newline, no
	// trailing empty row), the divider glyph (one cell, default "-") or a hint bar's
	// shortcut text.
	Text string
	// Runs are the styled segments of a label (when non-empty it replaces Text), one
	// row per entry of raw, and the entries of hintBar ("key: label" runs).
	Runs []WidgetRun

	// Focusable control state.
	Checked       bool   // toggle, tree node seed via Nodes
	Indeterminate bool   // toggle: neither on nor off
	Focused       bool   // initial focus hint; the panel's FocusKey wins
	Disabled      bool   // button: drawn muted and skipped by the focus ring
	Intent        string // button: "primary", "secondary" or ""
	Label         string // button/toggle text

	// List and tree state.
	Items       []WidgetItem
	Nodes       []WidgetNode
	Selected    int  // 0-based index of the selected item/node; -1 for none
	VisibleRows int  // list/tree window; 0 shares the available height
	Checkable   bool // tree: draw checkboxes and report toggles
	Columns     int  // spacer: blank cells in a row (default 1)

	// Interactive control state (tier 1b). The host owns it for a keyed widget after the
	// first input; the spec values are the seeds.
	Options          []string // dropdown and radio: the selectable values
	Number           float64  // number: the seed value
	Min, Max, Step   float64  // number bounds and step (Step 0 means 1)
	Integer, Percent bool     // number: render as an integer / a percentage
	Combo            bool     // dropdown: also accept typed text
	// Text field: the seed value is Text; these shape the editing region.
	Placeholder string
	FieldWidth  int  // visible columns; 0 auto-fits the content or the panel width
	FullWidth   bool // stretch the field to the enclosing row/col width
	Rows        int  // visible editing rows (0/1 single line, >=2 multi-line)
	MinRows     int  // grows with its text from this many rows
	MaxRows     int  // grows with its text up to this many rows (0: fixed Rows)
	ReadOnly    bool // text: caret/selection only, no edits
	Markdown    bool // text: accepted and rendered as plain text in this slice
	// Dropdown/dual list.
	Open         bool     // dropdown: the option list is expanded
	ScrollOffset int      // dropdown: first visible option when open
	Completions  []string // text: host-drawn candidate list (completion_request events)
	Included     []string // dualList: the values initially included
	// labeledSection and wrappers.
	Title     string // labeledSection: the legend on the box
	WidthPct  int    // labeledSection in a row: percentage of the row width
	WidthCols int    // labeledSection in a row: exact columns (wins over WidthPct)
	Window    WinID  // windowEmbed: the window id, 0 draws placeholders

	// Style is the optional styling of a label, text, button or hint row.
	Style Style

	// Flex makes a spacer take the free width of a row.
	Flex bool
}

// WidgetPanel is the content of a panel: the root widget and the key of the widget that
// starts focused ("" means no focus).
type WidgetPanel struct {
	Root     Widget
	FocusKey string
}

// WidgetPlacement is where a mounted panel sits.
type WidgetPlacement string

// The placements of v1.
const (
	// PlacementFloat is a centred floating card over the frame.
	PlacementFloat WidgetPlacement = "float"
	// PlacementBuffer paints the panel over the pane of the window showing the plugin's
	// virtual buffer (Buffer names it).
	PlacementBuffer WidgetPlacement = "buffer"
	// PlacementSidebar is a section of the sidebar column, under the explorer.
	PlacementSidebar WidgetPlacement = "sidebar"
)

// WidgetScope is how far a mounted panel/section reaches (Fresh's chrome scope).
type WidgetScope string

const (
	// ScopeWindow is the default: the mount belongs to the workspace window that mounted
	// it. It is shown while that workspace is active and is not carried to other
	// workspaces.
	ScopeWindow WidgetScope = "window"
	// ScopeEditor belongs to the whole editor: the host carries the registration across
	// daemon workspace switches, so it is visible in every workspace.
	ScopeEditor WidgetScope = "editor"
	// ScopeBuffer belongs to one buffer (the mount's Buffer, or the active buffer at mount
	// time). It is shown only while that buffer is active in its workspace and is dropped
	// with the buffer.
	ScopeBuffer WidgetScope = "buffer"
)

// WidgetMount is everything a mount call passes.
type WidgetMount struct {
	Panel     WidgetPanel
	Title     string
	Placement WidgetPlacement
	// Buffer is the virtual buffer whose window shows a buffer placement, or the buffer a
	// buffer-scoped sidebar section belongs to (0 = the active buffer at mount time).
	Buffer BufferID
	// Scope is the chrome scope of the mount ("" = ScopeWindow). A daemon's shared
	// plugin runtime carries editor-scoped mounts across workspace switches; window
	// mounts stay with the workspace and buffer mounts follow their buffer.
	Scope WidgetScope
	// WidthPct and HeightPct size a floating card, as a percentage of the frame (0: the
	// host default). Rows is the body height a sidebar section asks for (0 shares the
	// column).
	WidthPct, HeightPct int
	Rows                int
	// Focus gives the panel the keyboard at once; Closable puts an Escape/close path on
	// it that unmounts the panel and raises a cancel event.
	Focus, Closable bool
	// Page makes the whole panel content scroll as one window (a page) when no focused
	// widget takes the movement keys. AutoFocusFirst (nil: true) says whether a panel
	// without a FocusKey starts focused on its first tabbable widget.
	Page           bool
	AutoFocusFirst *bool
}

// WidgetAction is an action the plugin asks the host to perform on the focused widget of a
// panel (gvim.ui.widget_command). "textInputKey" feeds one editing key (Key, a canonical
// name: "backspace", "left", "home", ...), "textInputChar" types Text into a text field or
// combo, and "key" is the generic form the host treats like the matching smart key.
type WidgetAction struct {
	Kind  string // "focusAdvance" | "activate" | "selectMove" | "textInputKey" | "textInputChar" | "key"
	Delta int    // focusAdvance and selectMove
	Key   string // textInputKey and key
	Text  string // textInputChar
}

// WidgetMutation is a targeted change of one mounted panel (gvim.ui.widget_mutate). It is
// a faster path than re-sending the whole spec; the instance state of other widgets is
// kept.
type WidgetMutation struct {
	Kind      string // see the kinds below
	WidgetKey string
	Checked   bool
	Index     int // 0-based; setSelectedIndex, setDropdown
	Value     string
	// CursorByte is the caret byte offset for setValue; HasCursor says it was given.
	CursorByte int
	HasCursor  bool
	// Number is the new value for setNumber.
	Number float64
	// Included is the new included list for setDualIncluded.
	Included []string
	// Items, ItemKeys and NewNodes replace or append list/tree rows:
	// setItems replaces, appendTreeNodes appends.
	Items    []WidgetItem
	ItemKeys []string
	NewNodes []WidgetNode
	// Keys is the set for setExpandedKeys and setCheckedKeys; Checked is the state for
	// setCheckedKeys.
	Keys []string
	// Entries is the new content for setRawEntries.
	Entries []WidgetRun
	// The tree drag preview: previewTreeMove moves a row in the painted preview
	// (SourceKey after AfterKey, labelled Label); endTreeMovePreview ends it (Commit
	// keeps the move the plugin then applies with a spec update; false reverts).
	SourceKey string
	AfterKey  string
	HasAfter  bool
	Label     string
	Commit    bool
	// FocusKey is the setFocusKey target (widgetKey is accepted for the same call).
	FocusKey string
}

// WidgetEvent is the payload the host reports to plugins as a widget_event. It is a plain
// struct for the fake host and for tests; the runtime converts it to a Lua table. Panel is
// the mounted panel id; Widget is the key of the widget the event is about ("" for a
// panel-level event); Index is 0-based (-1 when the event carries none); Checked, Value
// and Mouse carry the rest.
type WidgetEvent struct {
	Panel   uint64
	Event   string // "activate" | "toggle" | "select" | "focus" | "value" | "close" | "cancel" | "mouse_click" | "mouse_move" | "mouse_wheel"
	Widget  string
	Index   int
	Checked bool
	Value   string
	// CursorByte is the caret byte offset a text change/completion event reports (-1 when
	// the event carries none).
	CursorByte int
	// Included is the included set of a dualList change.
	Included []string
	// FocusKey is the widget that holds the panel focus after the event ("" for none).
	FocusKey string
	// Drag target for the tree drag/drop events: TargetKey is the row under the pointer
	// ("" off every row) and TargetIndex its 0-based index (-1 for none).
	TargetKey   string
	TargetIndex int
	// Mouse fields for the mouse events: the frame cell, the button ("left", "right",
	// "middle") and the wheel delta in rows (positive down).
	X, Y   int
	Button string
	Wheel  int
}
