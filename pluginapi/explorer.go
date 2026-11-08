package pluginapi

// File explorer decorations and slots (round 10): the plugin-facing surface of Fresh's
// setFileExplorerDecorations/setFileExplorerSlots. A decoration gives a row a symbol and
// a colour; a slot entry overrides the leading/trailing slots and the filename colour of
// a row. Both are namespaced per plugin: different plugins may reuse a namespace label.
// Paths are absolute, or relative to the workspace root; paths outside the root are
// ignored. Positions are display rows of the explorer, not buffer lines.

// FileExplorerDecoration is one decorated explorer row.
type FileExplorerDecoration struct {
	// Path is the absolute path, or a path relative to the workspace root.
	Path string
	// Symbol is drawn in the trailing slot; only its first character is shown.
	Symbol string
	// Color is an RGB value, a theme key ("ui.file_status_added_fg") or a semantic name.
	Color Color
	// Priority orders decorations of one path; a higher value wins.
	Priority int
}

// FileExplorerSlotText is the content of a leading or trailing slot.
type FileExplorerSlotText struct {
	Text string
	// Color as an RGB value, a theme key or a semantic name.
	Color Color
	// MinWidth is the least display cells the slot reserves (leading slots only).
	MinWidth int
}

// FileExplorerSlot overrides the slots of one explorer row.
type FileExplorerSlot struct {
	Path string
	// Leading replaces the built-in leading icon slot; nil keeps the decoration/default.
	Leading *FileExplorerSlotText
	// SuppressLeading explicitly removes the leading slot (the built-in icon included).
	SuppressLeading bool
	// Trailing replaces the decoration/native trailing slot; nil keeps it.
	Trailing *FileExplorerSlotText
	// SuppressTrailing explicitly removes the trailing slot, native git marks included.
	SuppressTrailing bool
	// NameColor paints the row's name and compact-chain segments (unset keeps the default).
	NameColor Color
	// SuppressNameColor removes native name colouring (gvim has none today; kept for
	// Fresh's shape).
	SuppressNameColor bool
	Priority          int
}

// Caps of the explorer surface.
const (
	MaxExplorerEntries    = 10_000 // decorations or slots per namespace
	MaxExplorerNamespaces = 64     // namespaces per plugin
	MaxExplorerSymbol     = 8      // bytes of a decoration symbol
	MaxExplorerSlotText   = 32     // bytes of a slot text
	MaxExplorerMinWidth   = 8      // cells a leading slot may reserve
)

// ExplorerHost is the file-explorer part of the Host API.
type ExplorerHost interface {
	// SetExplorerDecorations replaces one namespace's decorated rows.
	SetExplorerDecorations(plugin, namespace string, decorations []FileExplorerDecoration) error
	ClearExplorerDecorations(plugin, namespace string)
	// SetExplorerSlots replaces one namespace's slot overrides.
	SetExplorerSlots(plugin, namespace string, slots []FileExplorerSlot) error
	ClearExplorerSlots(plugin, namespace string)
}
