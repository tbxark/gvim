package pluginapi

// Line targets (tier-1c remainder, Fresh's setLineTargets): a plugin makes lines of a
// buffer clickable. The host owns the behaviour: pressing Enter on a target line, or a
// single left click on it, opens Path at line Target, into the split labelled Into when
// that window is live, else beside the buffer that holds the targets.

// LineTarget is one clickable line of a buffer.
type LineTarget struct {
	Line int // 0-based line in the buffer that carries the target
	// Path is the file to open; a relative path resolves against the workspace root.
	Path string
	// Target is the 0-based line to land on in Path. 0 means the top line.
	Target int
	// Into names a split by its plugin label (gvim.win.set_split_label). When the label
	// names no live window the target opens beside the pane holding the targets.
	Into string
}

// LineTargetHost is the line-target part of the Host API.
type LineTargetHost interface {
	// SetLineTargets replaces every target the plugin's call sets on the buffer; an
	// empty list clears them. Another plugin's later call replaces the set as well.
	SetLineTargets(plugin string, id BufferID, targets []LineTarget) error
	// ClearLineTargets removes the targets of the buffer when the calling plugin owns
	// them.
	ClearLineTargets(plugin string, id BufferID)
}

// Caps of one buffer's line targets.
const (
	MaxLineTargets    = 10_000
	MaxLineTargetLen  = 4096 // path bytes
	MaxLineTargetInto = 64   // split label bytes
)
