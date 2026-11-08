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
3. **Body**: the sidebar column on the left and the editor panes on the right. The
   column is a stack of sections, each a header row over a body (the File Explorer,
   then the Markdown Contents section); adjacent sections share one border row, so a
   boundary costs one row. It is the explorer panel's box when there is one section.
   The section model and its row sharing are recorded in `docs/sidebar.md`.
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

### Palette details: the file preview and the Live Grep toolbar

The command palette keeps the sheet placement, and two optional regions live inside the
same card, between its borders:

- **File preview column** (the file mode, when a `Preview` reader is wired and the
  card's inner text width is at least 64 cells). The selected row's file head, read in a
  `tea.Cmd` and cached for the palette's lifetime, is painted in a right-hand column
  after a muted `│` divider; the list columns are recomputed from the narrower width.
  The column has a right-aligned line-number gutter, highlights the line a
  `path:line[:col]` query names, and shows a placeholder for a binary, empty or
  unreadable file; a file longer than the 16 KiB head or the twelve lines kept ends with
  `…`. A narrower frame drops the column and keeps the full-width list. The preview is
  a plain-text head: it has no syntax colours and does not scroll (see §4.5 of
  `docs/parity.md`).
- **Live Grep toolbar** (one row between the title border and the item rows, in Live
  Grep mode only). The match modes and the scopes are `Alt+<letter> Label:on|off`
  segments: `Alt+C` case, `Alt+G` regex, `Alt+O` whole word; `Alt+L` files,
  `Alt+U` buffers when a buffer snapshot is available, and `Alt+I` ignored and hidden
  files. The title border shows `Live Grep · N matches` (or `no matches`, `error`,
  `N+` at the 100-row cap), and an empty box says whether to type, turn a source on or
  that nothing matched. Live Grep's keys are answered by the overlay, as every overlay
  key is (parity §4.5), and its request carries the modes and scopes to the search port.

### Search panel details: project-wide Search and Replace, and Git Grep

Two more sheets share the card frame, one row of search plus (for Search and Replace)
one row of replacement, an option row and a scrollable result region:

- **Project-wide Search and Replace** (started by "Search and Replace in Project"; the
  editor's Alt+A) searches the project files and the open buffers' unsaved text. The card
  is title border, result rows (up to clamp(h/2, 4, 18), shrinking only as far as the
  menu and tab rows allow; a frame with no room paints nothing), the Search row, the
  Replace row, the options row and the hints. The result rows are grouped: a file row
  ("▾ path (count)", Enter folds and unfolds it) and a match row per matching line
  ("line:col  text", the matched runes bold); Enter on a match opens it. The options are
  the All Files/Current File scope (Alt+A), Case (Alt+C), Regex (Alt+R) and Whole
  (Alt+W) checkboxes; Tab moves the focus between the two fields and the result rows.
  Alt+Enter asks to replace every match of the result set. The replacement is a
  deliberate divergence from Fresh: gvim plans edits and applies them to the open tabs
  (and opens the closed files as unsaved tabs) instead of writing to disk and bypassing
  undo; the notice "N files changed in M tabs; Save All to write" says so, and Save All
  does the writes through the normal save path. Search and replace never reuse the
  offsets of an older result: every file carries a content hash from the search, and a
  file whose text changed is skipped.
- **Git Grep** (command palette row only) is the same card with a single search field:
  the All Files/Case toggles collapse to Case (Alt+C), the replacement row is absent, and
  Enter opens the match. It runs git grep in the work tree of the workspace root; exit 0
  and 1 both mean a finished search (1 is no matches), 2 or more is an error notice.

## 3. Component map

| Surface | Placement | Component |
|---|---|---|
| Command palette (Ctrl+P) | sheet | `internal/tui/palette` |
| Project search & replace, Git Grep | sheet | `internal/tui/searchpanel` |
| Find / replace bar | sheet | `internal/tui/find` |
| One-line prompt, confirm | sheet (input row) | `internal/tui/prompt` |
| File browser, switch project | card, above the status row | `internal/tui/browser` |
| Dialogs (confirm, error, save as, ...) | card, centred | `internal/tui/dialog` |
| Workspace-trust prompt | card, centred, exclusive | `internal/tui/trustdialog.go` |
| Lists (themes, cursor, ...) | card, centred | `internal/tui/list` |
| Plugin picker | card, anchored above the last row | `internal/tui/picker` |
| Menu-bar dropdown | menu | `internal/tui/menubar` |
| Context menu, "+" menu | menu | `internal/tui/ctxmenu` |
| Anchored popup (LSP hover, ...) | menu | `internal/tui/popup` |
| Sidebar column (File Explorer + Contents) | body, left column | `internal/tui/frame` (geometry and hit map), `internal/tui/sidebar` (section headers and the Contents body), `internal/tui/explorer` (tree body) |

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

## 5. The status bar

One row on the last row of the frame, laid out by `frame.StatusRuns`. Its content is
`editor.status_bar` as Fresh defines it: a left list and a right list of element names, and a
separator drawn between neighbours on the same side (default: none). The app passes the lists
through `tui.StatusBarOptions`; `internal/tui/frame` parses the names and lays the row out, and
painting and the hit map read the same runs. The TUI's fallback when no lists are passed is
Fresh's default element lists.

| Side | Default elements |
|------|------------------|
| left | `{trust}`, `{remote}`, `{terminal_restart}`, `{cursor}`, `{diagnostics}`, `{cursor_count}`, `{messages}` |
| right | `{read_only}`, `{line_ending}`, `{encoding}`, `{language}`, `{lsp}`, `{warnings}`, `{update}`, `{palette}` |

Elements can be moved between the sides or omitted. A name Fresh knows but gvim paints nothing
for still parses, so a Fresh config loads unchanged; a name neither knows is a config error, as
Fresh's `StatusBarElement::try_from` rejects it.

| Element | What gvim paints |
|---------|------------------|
| `{trust}` | the workspace-trust pill ("Trusted", "Restricted", "Blocked"), warning role for the restricted levels; click opens the Workspace Trust dialog |
| `{remote}` | "Local"; gvim is local-only, so the element is not clickable (there is no remote menu to open) |
| `{terminal_restart}` | "Restart terminal" while the terminal dock's shell has exited; click runs `restart_terminal` |
| `{cursor}` | the padded "Ln 1, Col 1"; click opens Go to Line |
| `{cursor:compact}` | the padded "12:3" form; click opens Go to Line |
| `{diagnostics}` | Fresh's split counts `[E:1 W:2 I:3]` (only the non-zero parts), a blank seven-cell slot while a server runs and no count is known yet; not clickable |
| `{cursor_count}` | "N cursors" when the active document has more than one caret, hidden at one |
| `{messages}` | the status message; click opens Show Warnings, gvim's status log; when the row cuts it the message wraps on the notice band above the row |
| `{read_only}` | "[RO]" for a read-only document; not clickable (gvim has no read-only menu) |
| `{line_ending}`, `{encoding}`, `{language}` | the indicator; click opens the chooser, as before |
| `{lsp}` | the language-server pill (see `docs/lsp.md`) |
| `{palette}` | "Palette: Ctrl+P", or the chord of the active keymap; click opens the command palette |
| `{warnings}` | nothing: gvim has no general-warning domain (Fresh's `[⚠ N]` counts its tracing warnings). gvim's equivalent list is Show Warnings, which keeps every notice, so a count of it would never reach zero. |
| `{update}` | nothing: gvim has no update checker |
| `{filename}` | nothing: the tab strip is gvim's filename surface |
| `{chord}`, `{clock}` | nothing yet (parsed; rendering is a follow-up) |
| `{plugin:token}` | nothing: gvim plugins contribute status items, not registered tokens (below) |

**Plugin items.** `gvim.ui.statusbar.item` items are placed by anchor, so they survive
configuration changes: left-aligned items go before the first `{messages}` element (or at the
end of the left group when `{messages}` is not configured), and right-aligned items before the
first configured of `{diagnostics}`, `{warnings}`, `{update}` and `{palette}` (or at the
end of the right group when none is). A clickable item keeps its hit target and `on_click`.
The large-file chip ("LARGE 42%") is a gvim extension, not a Fresh element: it leads the right
group.

**Squeeze rule (Fresh's).** The right group is sized first and dropped from its end, right-most
element first, but `min(floor(2W/5), 40)` cells stay for the left group and the first right
element survives while the row has at least 15 cells. The left group is elided at its tail: the
element that does not fit is cut and the rest drop. The `{remote}` label gives way as a whole
before the rest of the left group. When `{messages}` is cut or does not fit the row, the
message wraps on the notice band (section 1). The default left list starts with `{trust}`, so
the trust pill is the last left element to give way.

## 6. Menu accelerators

`editor.menu_bar_mnemonics` (default on) controls the menu-bar accelerators, as in Fresh:

- Each top-level label underlines the letter of the active keymap's Alt+letter `menu_open`
  binding for that menu — Fresh's `find_menu_mnemonic` rule. The default keymap binds File F,
  Edit E, View V, Selection S, Go G, Help H and Explorer X; gvim's own LSP menu (Fresh has no
  such menu) binds L. The letter is keymap data: a keymap with no `menu_open` chord for a
  label underlines nothing, and a keymap that binds a different letter moves the underline.
- The accelerator opens the menu through ordinary keymap resolution (the global `menu_open`
  binding); there is no second dispatch path. A normal-context binding shadows it, as Fresh's
  resolution does: on the macOS and Emacs keymaps Alt+F moves by word and Alt+L lower-cases
  text in the normal context, so those reach their menu only where no normal binding takes the
  chord, while the underline still marks the letter.
- F10 opens the menu bar (Fresh's `menu_activate`), and `editor.menu_bar_mnemonics` does not
  affect it: it is not a mnemonic.
- With the setting off, no label is underlined and an Alt+letter that the keymap only binds to
  `menu_open` does nothing; a user binding on the same chord still runs its own action.
- Dropdown rows carry no mnemonics; Fresh underlines bar labels only.
