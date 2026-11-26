// Package reviewdiff implements the state, layout, diff-row model, inline word
// diff, context expansion, search, hit-testing and cell-grid rendering for
// gvim's built-in Review Diff Mode.
//
// Like editorpane and sidebar, this package is pure: it performs no file or git
// I/O and holds no Bubble Tea dependency. The root TUI model feeds it file
// lists and file diffs loaded asynchronously through ReviewGitPort, routes key
// and mouse events to it, and paints its three regions (the left Changes
// sidebar, the 1-row Diff header bar, and the main Unified/Split diff pane)
// directly into the shared frame grid.
package reviewdiff
