// Package ui is the Bubble Tea v2 root model of gvim. It is one tea.Model that
// composes sub-models. Each sub-model has an Update(msg) (Model, tea.Cmd) and a
// View, and it is a value of its own package. The root routes input, holds the
// focus, and computes the layout. Painting is into the cell grid in ui/grid,
// and the colors come from ui/style.
//
// Layout and hit testing. computeLayout (layout.go) is the one geometry source.
// Row 0 is the menu bar, row 1 the tab strip, rows 2 to h-2 the body, and row
// h-1 the status row. The explorer panel takes the left columns of rows 1 to
// h-2, and the editor takes the rest. A frame smaller than 8x3 shows only the
// gate message. The hit map (hit.go) reads the same layout as the painter.
//
// Input. The root resolves key presses through the Keymap in the focused
// context, and inserts text when no binding claims it and the editor has focus.
// Mouse presses and wheel notches go through the hit map. View sets the mouse
// to all-motion mode, the mouse-first setting of ARCH-v4 decision 1.
//
// Stubs. Each part below is a band in the layout and a region in the hit map.
// The painted parts show their text now. The rest of each part arrives in the
// phase named, and it fills the existing band without moving the geometry.
//
//   - Editor pane (P3): the body paints the visible lines from the Document,
//     with the gutter, the text, the selection, the change marks, the syntax
//     spans and the scrollbar. The terminal caret follows the editor caret. A
//     press places the caret, a double press selects the word, a triple press
//     the line, and a drag extends the selection. The wheel and the scrollbar
//     move the viewport.
//   - Menu bar (P3): row 0 shows the labels now. The dropdowns, the generated
//     menu table, and the Menu.Toggle and Menu.Hover hits come in P3.
//   - Tab strip (P3): row 1 shows the labels now. Switching, closing and the
//     "+" button are routed in P3.
//   - Status row (P3): position and message are shown now. The encoding,
//     language and warning-counter segments are blank until P3.
//   - Footer (P3): the keybinding footer, through bubbles/help over the keymap
//     adapter. It takes no rows in this geometry yet.
//   - Explorer panel (P4, P6): the title is shown now. The tree rows, borders,
//     grip drag and file operations come later. The key and wheel routing for
//     focus, visibility and tree movement is in the root now.
//   - Palette, find bar, prompts and browser (P4, P5): overlays on the root's
//     stack (see Overlay in ports.go). An overlay takes the key and mouse input
//     while it is on top, and Esc closes it.
//
// Sub-packages never import this package. The root never imports the concrete
// domain packages. internal/app wires them in through ports.go.
package ui
