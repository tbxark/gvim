// Package buffer implements the text model behind the editor: a list of lines
// addressed by rune columns, with edits that can be undone and redone.
//
// A Buffer is not safe for concurrent use. Every method works on positions
// that are clamped into the document instead of failing, so callers never
// need to pre-validate cursor coordinates.
//
// # Two storage modes
//
// A Buffer holds its text in one of two ways, and callers use the same methods
// either way:
//
//   - New returns an in-memory buffer. Its lines are a [][]rune (buffer.go),
//     edited in place, with undo groups and observers (history.go, observer.go)
//     and snapshots that share the lines (snapshot.go).
//   - OpenLarge returns a buffer backed by a Source, for a file too large to
//     hold in memory. The lines are found through a chunk index over the source
//     (large_grid.go, lines_tree.go), reads go through a bounded page cache
//     (large_pages.go), and unsaved edits are an overlay of literal lines over
//     the file's own (large_overlay.go). Snapshots read the source directly
//     (large_snapshot.go), and Buffer.Rebase moves the buffer onto the file
//     after a save.
//
// Buffer.Large reports the mode. A large buffer can refuse a read or an edit it
// cannot make cheaply or correctly, and Buffer.Refusal says why; a memory buffer
// never refuses. Buffer.Stats and Buffer.IndexInfo report the rest of what the
// large mode does for its caller.
//
// The two modes are one type on purpose: the mode is part of Buffer's identity,
// not a layer above it. Buffer.big is read by thirteen files, and the history,
// observer and seam methods all branch on it. A split into two packages was
// considered and rejected: it would need a new operation and notification
// boundary, and the mode would still be part of what a Buffer is.
package buffer
