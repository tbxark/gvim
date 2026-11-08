# Virtual-space scoping

The record for the `editor.virtual_space` limits and the decisions behind them. The setting is
`off` (default), `on` (the caret may sit past the end of lines and on empty rows below the last
line) or `block` (block selections may also use those cells). The engine lives in
`document/virtualspace.go`, `document/block.go` and `editor/virtualspace.go`, the pane in
`internal/tui/editorpane/virtual_{block,lines}.go`, and the view toggles in
`internal/tui/virtual_space.go`.

## Landed surface

- Horizontal virtual space: the caret can park past the end of a line and the line is padded
  with spaces for editing, copying and selection; `document.VirtualLines`/`SetVirtualLines`
  and `editor.VirtualSpaceEnabled`/`VirtualSpaceBlockEnabled` are the engine APIs.
- Vertical virtual space (`on`): the caret can sit up to `document.MaxVirtualLines` (10000)
  empty display rows below the last line; the pane draws the caret there, clamped to the body's
  last row, and the view never scrolls past the text (Fresh's known limit).
- Block selections: `document.BlockRect` stores the rectangle in rune columns, with
  `BlockStep`, `setBlock`, `blockPad` and the editor-boundary conversion; `editor` pads lines
  with spaces for a block edit and writes the same bytes a normal edit would.
- R4-4 landed this round: session persistence of the virtual caret position — the saved file
  state carries the caret's vertical position (virtual rows and column) and a restore parks the
  caret there again; older sessions without the fields decode unchanged (zero means "on text"),
  and block/off modes are unaffected. Tests: `internal/restore` round-trip and old-session
  tests, `session` decode test.

## Recorded limits (deliberate)

1. **Block geometry is rune-based while linear selections stay byte-clamped.** This is the
   §4 item 19 divergence: block corners are stored as rune columns (byte-addressable edit
   ranges converted at the editor boundary) so the rectangle is exact for the ASCII spaces
   padding adds; tabs and wide characters at or past EOL can shift a block edge from Fresh's
   visual-cell columns. Making it visual-cell exact would need cell-width-aware block
   arithmetic through the engine and is recorded, not silently approximated.
2. **Virtual space is off under line wrap, multi-caret and large files.** Fresh's scoping doc
   records the same: the pane's row and visual-column arithmetic does not carry virtual columns
   through wrapped rows, several carets would each need their own virtual position, and the
   windowed large-file mode does not keep the whole line text. The editor ignores the setting
   in those modes and the status shows the persisted setting, not an effective one.
3. **The per-buffer switch is a bool, so it can enable `on` but not `block`.**
   `document.ViewSettings.VirtualSpace` (and the generic toggle in
   `editor/viewsettings.go`) is `*bool`; `block` stays an editor-level
   `editor.virtual_space` mode. A per-buffer tri-state override would have to go through the
   generic bool override boundary; recorded as a deliberate limitation rather than a hidden
   one.
4. **Vertical virtual lines are caret rows only and cannot be selected.** No buffer cells
   exist below the last line, so a selection or drag that reaches a virtual row clamps to the
   last text line; the caret row itself is clamped to the body's last row (viewport and folded
   end), which is the documented "view does not scroll past the text" limit.
5. **No pane scroll past the last line**: the virtual rows are drawn in the viewport that
   exists, so a caret parked deeper than the body height is shown on the last visible row.
6. **Session persistence is the only state carried across restarts.** There is no
   per-workspace or per-buffer virtual-space setting beyond the editor setting and the
   per-buffer bool; the caret's virtual position is the only per-session state (item above).

## Tests

`document/virtualspace_test.go`, `document/block_test.go`, `editor/virtualspace_test.go`,
`editor/virtualspace_block_test.go`, `editor/virtualspace_lines_test.go`,
`editor/block_test.go`, `internal/tui/editorpane/virtual_lines_test.go`,
`internal/tui/editorpane/virtual_inline_test.go`, `internal/tui/editorpane/virtual_block_test.go`,
`internal/tui/virtual_space_test.go`, and the session round-trip tests named above.
