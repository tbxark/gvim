// Package tui is the Bubble Tea v2 root model of gvim. It is one tea.Model that
// composes sub-models. Each sub-model has an Update(msg) (Model, tea.Cmd) and a
// View, and it is a value of its own package. The root routes input, holds the
// focus, and computes the layout. Painting is into the cell grid in
// internal/tui/grid, and the colors come from internal/tui/style.
//
// Layout and hit testing. frame.ComputeLayout (internal/tui/frame) is the one
// geometry source.
// With every bar shown, row 0 is the menu bar, row 1 the tab strip, rows 2 to
// h-2 the body, and row h-1 the status row. The menu bar, tab strip and status
// row can each be hidden (the body takes the row), and a chrome border style
// adds a frame row under the menu bar and above the status row. The sidebar
// column (the explorer and its sections) takes the columns on its configured
// side, and the editor takes the rest. A frame smaller than 8x3 shows only the
// gate message. The hit map (internal/tui/frame) reads the same layout as the
// painter; the menu bar's own labels and dropdowns are hit-tested by
// internal/tui/menubar.
//
// Input. The root resolves key presses through the Keymap in the focused
// context, and inserts text when no binding claims it and the editor has focus.
// Mouse presses and wheel notches go through the hit map. View sets the mouse
// to all-motion mode, so hover and drag reach the model.
//
// The parts of the frame, each a band in the layout and a region in the hit map:
//
//   - Editor panes: internal/tui/editorpane paints the visible lines of a
//     Document with the gutter, the text, the selection, the change marks, the
//     syntax spans and the scrollbar; internal/tui/split owns the split tree.
//   - Menu bar: internal/tui/menubar, over the generated menu table.
//   - Tab strip: internal/tui/tabstrip paints it from the layout runs.
//   - Status row: built from frame.StatusView by statusView.
//   - Sidebar column: the explorer panel and the Contents, Diagnostics and plugin
//     sections (internal/tui/explorer, internal/tui/sidebar).
//   - Dock: the terminal tabs under the editor (internal/tui/dock).
//   - Overlays: the palette, find bar, prompts, browser, lists and popups are on
//     the root's stack (see package overlay). An overlay takes the key and mouse
//     input while it is on top, and Esc closes it.
//
// Sub-packages never import this package. The root never imports the concrete
// domain packages. internal/app wires them in through ports.go.
package tui
