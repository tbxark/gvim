# gvim UI: spacing and overlay style

Status: the rules the chrome and the overlays follow (2026-11-08).

This file is the design record for the terminal UI's spacing and for the overlay
("popup") components. `docs/architecture.md` records the package tree; this file
records how the pieces look and where they sit, so a new component has one place to
look for the padding, the margins and the frame of a box.

## 1. Spacing scale

All spacing is whole terminal cells and comes from `internal/tui/space`:

- `PadX` (1): horizontal padding inside a bar, a card or a menu, on both sides of
  the text. Text never touches a border or a screen edge.
- `Margin(w)` (and `ListMargin(w)`, which is the same rule): the gap between a
  floating surface and the left and right screen edge. It is 0 below 60 columns, 1
  up to 139, and 2 from 140. A floating surface never docks to an edge when the
  frame has room for the gap.
- Vertical spacing is made of whole rows: a band that needs air gets a padding row of
  its own, a floating card keeps at least one row from the top and bottom edge (a card
  that docks to the bottom row, such as the picker without a query line, gives up the
  bottom row), and a sheet is docked to the last row of the frame.

The chrome rows, from the top:

1. **Menu bar**, two rows: the labels on row 0 and one padding row of the menu
   background below them. The padding row is what keeps the labels from sitting
   against the tab strip; a second padding row above the labels is not used, since
   the top edge of the frame is its own boundary and three rows read as too tall.
2. **Tab strip**, one row, with the explorer's title row aligned with it when the menu
   bar is shown (with the menu bar hidden the strip is row 0 and the panel keeps its
   title on row 1). The new-tab button is a three-cell button (" + ") pinned to the
   right edge, so the glyph keeps one padding cell on each side and the hit target
   covers the whole button.
3. **Body**: the explorer panel on the left and the editor panes on the right.
4. **Status bar**, one row, on the last row of the frame. With the `show_prompt_line`
   setting on, an open prompt takes the row below the status bar: the status row moves
   up one row **only while a prompt is open**, so when nothing is open the status bar
   keeps the last row and the space above and below it stays the same. With the setting
   off (the default) a prompt paints over the status row, as Fresh's auto-hide
   prompt line does.

## 2. Floats: one frame for every surface

Every surface that is drawn over the frame is one of three placements:

- **Sheet**: docks to the bottom of the frame, full width less the margin, and holds
  a command input. The command palette and the find/replace bar use it: a card whose
  bottom border is on the frame's last row, with the title on the top border, the rows,
  the input row directly above the bottom border, and the key hints on the bottom
  border. The prompt line is the input row of a sheet without the card frame: when it
  covers the status row it is one row, with the same muted label and raised field.
- **Card**: a floating box, centred (dialogs, lists, text boxes) or centred
  horizontally and anchored above the status row (the file browser, the picker). Cards
  keep `Margin(w)` from the left and right edges and at least one row from the top and
  bottom; a card with no query line puts its bottom border on the last row (the
  picker).
- **Menu**: a box anchored to a cell or to a label: the menu-bar dropdowns, the
  context menus and the anchored popups. A menu keeps `space.Margin(w)` from the
  left and right edge (the popup never covers the menu bar's label row) and stays
  inside the frame on both axes.

The three placements share one visual frame, painted by `style.Overlay.PaintBox` and
`style.Overlay.PaintFooter`:

- a rounded border in the theme's accent colour;
- the title on the top border, after one blank cell, on the plain rounded rule (the
  title is passed bare; `PaintBox` trims it and leaves the rest of the border plain);
- the fill of the popup background, with `PadX` between the border and the text;
- the key hints of the surface on the bottom border, starting after the same
  one-cell padding, in the muted style;
- no hand-drawn borders (`╭─╮` built by a component) and no second border style.

Inside a frame the roles are fixed:

- `Overlay.Selected` marks the highlighted row; `Overlay.Focus` marks the focused
  button or label;
- `Overlay.Muted` carries secondary text: hints, labels, shortcuts and details;
- `Overlay.Box` is the text of a box, and the fill of its rows;
- a disabled row is the box style with faint set;
- an input row (a sheet query, a prompt, a picker query) uses the raised field fill
  and the field text (`Overlay.Idle`) for the field, a muted label before the field
  where the surface has one (find, prompt), and the caret cell reversed. A list's
  filter row stays on the box fill: it is a row of the list, not a field.

## 3. Component map

| Surface | Placement | Component |
|---|---|---|
| Command palette (Ctrl+P) | sheet | `internal/tui/palette` |
| Find / replace bar | sheet | `internal/tui/find` |
| One-line prompt, confirm | sheet (input row) | `internal/tui/prompt` |
| File browser, switch project | card, above the status row | `internal/tui/browser` |
| Dialogs (confirm, error, save as, ...) | card, centred | `internal/tui/dialog` |
| Lists (themes, cursor, ...) | card, centred | `internal/tui/list` |
| Plugin picker | card, anchored above the last row | `internal/tui/picker` |
| Menu-bar dropdown | menu | `internal/tui/menubar` |
| Context menu, "+" menu | menu | `internal/tui/ctxmenu` |
| Anchored popup (LSP hover, ...) | menu | `internal/tui/popup` |

## 4. Rules for a new component

1. Decide the placement first: sheet, card or menu. Do not invent a fourth one.
2. Paint the frame with `PaintBox`; put the hints on the bottom border with
   `PaintFooter`. Do not build border glyphs or title text by hand.
3. Use `space.Margin(w)` for the edge gap and `space.PadX` for the inside
   padding. Do not use a literal column or a different margin.
4. The title is bare text ("Open File", not " Open File "). Keep it short enough for
   the box; `PaintBox` cuts a title that cannot fit.
5. Keep the placement geometry in one pure function that painting and hit testing
   both call, so a box and its click targets cannot drift apart.
