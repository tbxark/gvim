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

**File-query words.** The file mode splits a query on whitespace and every term must
match the candidate path, in any order, as Fresh's file finder documents (`feat group`
finds `features/groups/view.tsx`, `etc hosts` finds `/etc/hosts`, `save file` finds
`save_file.rs`). A legal path containing spaces still matches: `my file` finds
`my file.txt`, because whitespace separates terms rather than being a character the
candidate must contain. A `path:line[:col]` suffix is stripped before the terms are
matched. Buffer mode (`#`) applies the same matcher to tab labels (Fresh matches
buffers with its `FuzzyMatcher` too): `#file save` finds `save_file.go`, and a
fuzzy subsequence such as `#sflg` matches without being a contiguous substring; the
rows are ordered by score with tab order breaking ties. The terms are prepared once per
query and matched with a reusable matcher; an ASCII candidate that cannot contain a term
is rejected without being converted to runes or allocating. The finder keeps its bounded
behavior: 50,000 indexed files, 100 file-mode results, background enumeration
cancellation, and the palette's sequence guard that drops a ranking result for a query
the user already changed.

**Literal-path probe.** The file mode always asks the index's optional literal-path
probe for the path the query names, both while the index is enumerating and after it
has completed, as Fresh does (a typed `etc/hosts` finds the file before enumeration
reaches it; a completed/truncated index can still miss an exact file). The local probe
resolves the root and every candidate through symlinks and returns a path only when its
real target stays inside the real workspace (an entry symlink or a symlinked parent that
escapes is skipped). A local query naming an existing directory lists that directory's
visible regular files; otherwise it lists the parent and keeps the regular files whose
names start with the query's basename. Local directory work is bounded: at most 4096 entries are inspected in 128-entry
batches with cancellation, then sorted, and at most 50 paths are returned. A directory
listing follows the file explorer's Show Hidden Files setting for dot entries; the
parent-prefix branch includes hidden names when that setting is on or when the typed
basename itself starts with a dot (Fresh's shell-completion exception), and a typed dot
basename matches its dot names even when hidden files are otherwise off. The remote probe intentionally supports the
exact-path Stat slice only: it resolves the root and the literal candidate through the
authority's realpath capability and stats that one file, never the local disk, and
refuses when the backend cannot prove containment. It performs no remote directory
enumeration, because the authority ReadDir shell glob materializes the whole directory
before any client-side limit could trim it. Every probe runs only in a `tea.Cmd`, never in
Update/Draw; its context is canceled when the query changes or the palette closes. The
answer, including a negative, is cached in a bounded (16-path) cache keyed by the path
text, so a loading poll, a repeated key, a `:line[:col]` change or a mode-prefix escape
for the same path does not repeat the Stat; a changed path does probe again. The cache
belongs to one file-index generation: a finder Restart (manual refresh, reconnect or
workspace rebuild) bumps the generation and drops the whole cache, so a path that was
absent before the refresh is probed again and a newly created file can appear without
reopening the palette. A result is displayed only when both the query sequence and text
still match. Confirmed paths are merged before the normal rank, so fuzzy scoring, prefix
boost, the `:line[:col]` target and the 100-row file cap apply. Missing paths, escapes
through `..` or symlinks, unreadable paths produce no probe row; a probe never falls back
to another filesystem. Residuals: the remote parent-basename/directory branches are not
probed (they need a bounded directory-reader port); the local scan cap can miss matches
beyond the first 4096 entries of a directory larger than that (the inspected subset follows
filesystem order, so it is not a complete listing); the probe branch has no `.gitignore`
filtering; an external create/remove without an index refresh can still serve a cached
negative or positive until the next refresh or palette reopen (Fresh re-probes every query);
the Show Hidden Files setting is applied when the palette opens, so toggling it while the
palette is open applies on the next open; an exact local symlink is followed only while its
real target stays inside the workspace (a TOCTOU swap between realpath check and Stat
remains theoretically possible); hidden entries are not listed for a hidden directory
query.

- **File preview column** (the file mode, when a `Preview` reader is wired and the
  card's inner text width is at least 64 cells). The selected row's file preview, read in a
  `tea.Cmd` and cached for the palette's lifetime, is painted in a right-hand column
  after a muted `│` divider; the list columns are recomputed from the narrower width.
  With no line target it is the 16 KiB file head; a `path:line[:col]` query uses the
  optional window reader around that line (the same bounded window as Live Grep, 12 lines)
  so a target past the head is visible. The column has a right-aligned line-number gutter,
  highlights the named line, and shows a placeholder for a binary, empty or unreadable
  file; a file longer than the head or the twelve lines kept ends with `…`. A narrower
  frame drops the column and keeps the full-width list. The preview is plain text: it has
  no syntax colours and does not scroll (see §4.5 of `docs/parity.md`).
**Palette keyboard.** Up/Down move the selection, PageUp/PageDown move it one visible
page and clamp at the first/last row (Fresh's `prompt_page_up`/`prompt_page_down`; there is
no page before the first layout), Enter or Ctrl+F runs/opens the selected row (Fresh's
prompt_confirm; gvim intercepts Ctrl+F before the query widget moves the caret) and
Escape closes the palette. Tab accepts the selected
suggestion into the query instead of running it (Fresh's `prompt_accept_suggestion`): in
the file mode it fills the ranked path together with the `:line[:col]` target the query named,
so a jump target is not lost; a Windows drive or any other colon in the path is left intact
and the target is read from the end. A file whose own name ends in `:number` is kept verbatim
for that accepted query instead of being re-read as a jump target, and a name that begins
with a palette mode character (`>`, `#` or `:`) is written with a leading `./` escape so the query stays
in file mode (the ranker strips that escape). In command/buffer/line modes Tab fills the
command name, buffer name or line target, preserving the `>`, `#` or `:` mode prefix; the palette
stays open, the rows re-query, and Enter then runs/opens it. A message row, a disabled
command or no selection leaves the query unchanged. In Live Grep's floating overlay Tab
is a no-op, as in Fresh (its suggestion values are opaque).

**Find/replace bar keyboard.** F3 and Shift+F3 step through the matches without
closing the bar; Enter (or Ctrl+F, Fresh's prompt_confirm) jumps to the current match
and closes it; Tab/Shift+Tab show and
switch the replacement row; Esc restores the caret and selection from before the bar
opened. Alt+C/Alt+W/Alt+R/Alt+I toggle Case Sensitive, Whole Word, Regex and Confirm
Each. In either focused query field, `Ctrl+Delete` deletes the next word and
`Ctrl+Backspace`/`Ctrl+W` the previous word, as Fresh's `prompt_delete_word_forward`/
`prompt_delete_word_backward` (the Emacs `Alt+D` route resolves to the same forward delete);
these are widget chords, not editor actions, so the field keeps focus and the bar stays
open.

**One-line prompt keyboard.** Up/Down walk the prompt history; when the prompt shows a plugin
suggestion list, Up/Down/Tab/Shift+Tab move the highlight and fill the field, and PageUp/PageDown move
the highlight by the visible suggestion rows and clamp at the ends (Fresh prompt_page_up/prompt_page_down).
Ctrl+A selects the whole prompt text (Fresh prompt_select_all) as a reverse-video selection in the
default and VS Code presets; the macOS and Emacs presets keep their Ctrl+A prompt Home binding instead.
Shift+Left/Right extend the selection by one rune, Shift+Home/End extend to the ends, and
Ctrl+Shift+Left/Right select by word. Typing or pasting replaces the selected range, Backspace/Delete
deletes exactly it, and an unshifted arrow cancels the selection while keeping the text. Ctrl+C copies
the selected text, or the whole prompt when nothing is selected; Ctrl+X cuts the selected range, or the
whole prompt when nothing is selected, as Fresh's prompt_copy/prompt_cut do. A dialog that opens with
preselected prefill text still uses the older invisible replacement prefill; Ctrl+A is the path that
creates the reverse-video selection. Both actions wait for a native clipboard write to report its result: Copy shows Copied only on
success and Copy failed on an error, while Cut also waits before clearing. The OSC 52 write alone
is best-effort, so without a native writer the copy is reported as requested (not Copied) and the
prompt is kept; a failed cut keeps it too. A late result is applied only to the same
still-active prompt and only if it belongs to the latest copy/cut (same model generation);
a closed, switched or superseded prompt result is ignored. Copy/cut are single-flight: when a
native writer exists it is the only channel used (no OSC 52 beside it), and a process-wide permission
for that shared system clipboard refuses another workspace's copy/cut with a notice instead of queuing
it. CancelTransientWork, an owner-generation change and close mark a not-yet-started request canceled;
the command checks that after it gets the permission and skips ClipboardWriter.Write. A write already
in progress cannot be canceled (the writer API has no cancel): while it hangs the permission stays
held and new prompt copies/cuts are refused with a notice. OSC 52-only requests are not confirmable and
are reported as requested. Enter submits (Ctrl+F from the prompt keymap confirms it)
and Esc cancels.

**Settings page keyboard.** Typing filters the settings rows; `/` starts Fresh's settings search mode (clear filter, type to search, Enter ends the search keeping the current selection without editing it, Esc cancels the search without closing Settings); Up/Down move, PgUp/PgDn page, Home/End jump, Enter edits, Tab switches layer, Ctrl+S saves and Ctrl+R resets, Delete inherits/resets the row, and Ctrl+E edits the raw JSON. `?` opens Fresh's settings_help panel listing these keys; any key closes it, and Esc then closes the Settings page. The panel uses the existing overlay chrome and does not change focus or the cursor.

**Live Grep Resume.** Closing Live Grep captures the prior query, match modes and
scope toggles; it does not keep a reusable result set. Live Grep: Resume restores those
controls and always searches again. Cached hits are deliberately not restored: a hit's
file contents can change while the palette is closed and the search port has no
low-cost content fingerprint (the finder index tracks the file list, not content versions), so
showing an old hit could open the wrong line or miss a new match. The
capture and restore are in-memory, Update does no I/O; the re-run is the ordinary Live
Grep `tea.Cmd` with its sequence guard. A project-root or configuration change clears
the captured controls, so Resume starts a fresh session instead of carrying the old
root's query/modes/scopes. The command description says that it searches again.

- **Live Grep toolbar** (one row between the title border and the item rows, in Live
  Grep mode only). The match modes and the scopes are `Alt+<letter> Label:on|off`
  segments: `Alt+C` case, `Alt+G` regex, `Alt+O` whole word; `Alt+L` files,
  `Alt+U` buffers when a buffer snapshot is available, `Alt+T` terminals when the dock
  has an open terminal (on by default, matching Fresh's terminal-scrollback default),
  and `Alt+I` ignored and hidden files. The title border shows `Live Grep · N matches`
  (or `no matches`, `error`, `N+` at the 100-row cap, `· incomplete` when the remote
  adapter reports its result is truncated), and the footer carries the adapter's
  degradation/cap notice; an empty box says whether to type, turn a source on or that
  nothing matched. A late result from an older query cannot overwrite the current
  notice or rows. Live Grep's keys are answered by
  the overlay, as every overlay key is (parity §4.5), and its request carries the modes
  and scopes to the search port.
- **Live Grep row preview** (the selected match while the result rows have the focus) follows
  the palette's preview column: a file hit reads a bounded window around the match line
  (at most 16 KiB / 12 complete lines, the five lines before the hit included) in a
  `tea.Cmd` through the optional window-preview port, paints the line-number gutter and
  the selected target line, and never reads on Update or Draw. The read's context is
  canceled when the highlight moves to another hit, so a stale window cannot land on the
  new row; a port without the window seam falls back to the bounded file head. Only file
  hits preview: a buffer hit keeps the full-width rows (its unsaved text is already in the
  open-buffer snapshot), and a frame too narrow keeps the full-width list. Preview lines
  are display-sanitized: invalid UTF-8 and the Unicode Cc/Cf categories (C1 controls,
  bidi overrides, zero-width format characters, soft hyphen and BOM) are removed, while
  tabs keep their preview expansion and readable text is unchanged; the match highlight
  and syntax spans are remapped through the same rune map, so a removed control before
  the match cannot shift the bold run. Live Grep result rows follow the same rule: the
  displayed file/terminal label and line text are textsafe-cleaned, and `mcol`/`qlen` are
  remapped through CleanWithMap so the bold run stays on the visible query; the raw
  `rel`/`line`/`col`/index remain the open/focus target. A window that ends exactly at the
  12-line cap is marked truncated because the reader stops at the cap without probing for one
  more line, so the column may show its `…` marker at EOF (exact-maxLines truncated
  false-positive residual).
- **Live Grep quickfix export** (`Alt+Q` or `Alt+M` in Live Grep, Fresh's
  `live_grep_export_quickfix`): the current hits open in the shared, filterable list
  overlay as `Live Grep: <query> (N matches)`, grouped by one row per hit; Enter opens
  the file at the hit, activates the buffer's tab, or — for a terminal transcript hit —
  focuses that existing dock tab (showing the dock when hidden, starting no shell or
  file), and leaves the list open so the set can be walked; Esc closes it. A terminal row
  only focuses the tab: the search transcript line is not scrolled to (terminal-hit
  focus-only residual). The list's label/right text and title are textsafe-cleaned before
  painting, but its row Value keeps the raw path or dock index, so cleaning the display can
  never change which file/tab Enter opens or focuses. The export also carries the search's
  Notice/Truncated: its title gains `· incomplete` (or `· notice`) and a leading header row
  states the degradation/cap reason when the result is not complete. This is a list view, not a persistent quickfix stack: no
  gvim session state outlives it. The export is bounded independently of the palette's
  100-row result cap (at most 100 rows), and a capped export shows a notice naming the
  bound. A Live Grep search with no hits does not open an empty list.
- **Live Grep Terminals scope** (`Alt+T`): searches the open dock terminals' transcript
  windows, after the file and buffer hits (Fresh's order). The root snapshots the dock
  on the UI thread from the in-memory terminal state — no PTY read, no process start,
  no file IO — bounded to the newest 500 rows / 1 MiB per tab, 16 tabs and 4 MiB total;
  the palette re-applies those caps when it builds a request, so an unbudgeted caller
  cannot grow the search. A terminal hit row is labelled with the tab's name, its OSC
  title or its launch program (`terminal` when none), and Enter shows the dock and
  resolves that tab by its **stable runtime id** and reveals the matched line in the
  read-only scrollback pager (active main-screen terminals and exited live shells;
  a restored tab without a session or a running alternate-screen program has no
  pager and gets the honest "line cannot be located" notice). The stored snapshot row
  is validated, not trusted: the hit carries the expected line text, its snapshot
  neighbours and the scrollback length, so new output/scrollback eviction relocates
  the line only by a unique content match (an empty matched line is a real blank-line
  hit, validated/relocated by its known neighbours; a malformed snapshot payload is
  refused, not clamped); a missing, truncated or ambiguous line refuses with the same
  honest notice instead of jumping to the wrong row. A stale
  id — the tab closed since the search — leaves the active tab alone and shows "no
  longer open", so a close/reorder or duplicate labels cannot focus the wrong
  terminal. It opens no file and starts no shell; the Alt+Q export stores the same
  id+row+snapshot payload and validates/relocates identically while its list stays
  open. The scope starts on when a dock snapshot
  exists (Fresh's default) and can be turned off for the session with `Alt+T`.
  **Residual:** only the open dock's newest transcript window is searched. gvim has no
  Fresh-style per-terminal backing files (`<data>/terminals/*.txt`), so a closed
  terminal and scrollback older than the bounded window are not searchable; the
  terminals of other workspaces are out of scope (the workspaces provider needs its own
  cross-workspace seam). Diagnostics and all-workspaces sources remain out of scope for
  the same provider-protocol reason.

### Search panel details: project-wide Search and Replace, and Git Grep

Two more sheets share the card frame: a row of search, a row of file globs (both
sheets), and for Search and Replace one row of replacement, an option row and a scrollable
result region:

- **Project-wide Search and Replace** (started by "Search and Replace in Project"; the
  editor's Alt+A) searches the project files and the open buffers' unsaved text. The card
  is title border, result rows (up to clamp(h/2, 4, 18), shrinking only as far as the
  menu and tab rows allow; a frame with no room paints nothing), the Search row, the
  Replace row, the Files row, the options row and the hints. The result rows are a match
  tree: a file row ("[x] ▾ path (checked/total)", Enter folds and unfolds it) and a match
  row per matching line ("[x] line:col  text", the matched runes bold, Enter opens it).
  Space toggles the focused row's checkbox (a file row takes its matches with it), a
  checks every match and n unchecks every match; the title says how many are selected.
  The options are the scope chip (Alt+A cycles All Files, Open Buffers and Current File;
  Open Buffers searches only the open buffers' unsaved text, like Fresh's universal-search
  Buffers source, and the Files glob narrows the All Files walk), the Case (Alt+C), Regex
  (Alt+R) and Whole (Alt+W) checkboxes; Tab moves the focus between the search field, the
  replacement field, the Files field and the result rows. The Files field is a comma or
  space separated glob filter: a pattern without a slash matches the file's name at any
  depth (*.go) and a pattern with a slash matches the root-relative path (src/**,
  **/test_*.go), with * and ? staying inside one segment and ** crossing them; an invalid
  pattern blocks the search and says why. Up and Down in the search field walk the
  session's earlier patterns (20 deep); the searched pattern is remembered when a search
  finishes. The search streams: the app reports whole files as it walks them (one batch of
  at most 128 files or 1000 matches at a time), the panel appends them in the finder's
  order without moving the selected row, and the title and the empty result area say
  "searching N/M files" until the final batch. A provider that does not stream falls back
  to one asynchronous search that shows its result when it lands.
  Alt+Enter replaces every checked match, Shift+Enter replaces the focused row alone (a
  file row: its checked matches; a match row: that match), each after a confirmation and
  never at an offset the search did not report. The replacement is a deliberate
  divergence from Fresh: gvim plans edits and applies them to the open tabs (and opens
  the closed files as unsaved tabs) instead of writing to disk and bypassing undo; the
  notice "N files changed in M tabs; Save All to write" says so, and Save All does the
  writes through the normal save path. Search and replace never reuse the offsets of an
  older result: every file carries a content hash from the search, and a file whose text
  changed is skipped. Because the results stream, a replacement started before the search
  finishes plans from the rows that have arrived (the confirmation counts them); the fresh
  search that follows the replacement shows the rest. A closed file above the search read
  cap (or above the large-file threshold when the editor runs in large-file mode) is never
  searched and shows a skip reason; a file open in a buffer is searched from its snapshot
  whatever its size, so replacing in an open large buffer goes through the same
  unsaved-tab and hash model (Save All writes it), while a streamed writer for closed
  oversize files stays a residual.
- **Selected-match preview column** (while the result rows have the focus) follows the
  palette's file-preview conventions: a muted `│` divider at the right of the result
  region, a right-aligned line-number gutter, and a column only when the card's inner text
  width is at least 64 cells (the column takes 24..60 cells, the rows keep at least 30); a
  narrower card drops the column and keeps the full-width rows. The column's first row is
  the header `path:line` (with `–endLine:endCol` and `(N lines)` for a multi-line match,
  `(unsaved)` for an open buffer, and `· Lfirst–last/total` for the window's place in the
  file); the lines come from the open buffer's snapshot or, for a closed file, from a
  bounded read with the search caps (a large, binary or non-UTF-8 file shows the reason
  instead of lines). A remote closed file uses the window seam: the authority is scanned
  in bounded ReadRange chunks around the selected match (8 MiB scan budget), the header
  says `window only` when the window stops before EOF, a known total is shown when EOF
  was reached, and a hit beyond the scan budget is an explicit error (never the file head
  or a local read). The scan rejects NUL/binary content and invalid UTF-8 explicitly (the
  panel shows the reason/placeholder, never a sanitized partial excerpt). A first line
  exactly at the 256 KiB cap is shown as a complete line (window-truncated when more lines
  follow, the whole file when it ends there); a line above the cap is the explicit
  line-cap error, not an empty window. Syntax colour is kept only for a known-small total;
  the open-buffer path is unchanged. The match's line or lines take the selected style and the matched
  runes are bold; the rest of a line is syntax-coloured automatically when the port can
  highlight it (the highlighter is cached per file and bounded at 256 KiB or 4000 lines,
  and a grammarless or oversized file stays plain). The window scrolls while the result
  rows have the focus: PgUp/PgDn page it, Ctrl+Home/Ctrl+End jump it to the file start/end,
  and a wheel notch over the column scrolls three lines; plain Home/End still select the
  first/last result row, and without a column the scroll keys do nothing. A scrolled
  window is painted from its cache and only asks the port again at the cache edge. Enter
  still opens the match. A preview read takes the panel's cancellation context: changing the
  selected row or query, requesting another preview window, and closing the panel, cancel
  an in-flight local or remote read (the remote provider passes it into the authority's windowed read);
  a port that only implements the plain preview seam keeps working, with its stale answer
  dropped by sequence. Preview lines are untrusted display text: the palette and the
  search panel strip invalid UTF-8 and Unicode Cc/Cf (C1 controls, bidi overrides,
  zero-width format characters, soft hyphen and BOM) before painting, keep tabs'
  semantics, and remap the match highlight and syntax spans through the same rune map.
  File names are normalized but a legal Unicode Cf character in a path is displayed as
  named on disk (Cf path display residual).
- **Multi-line matches**: a regex match that spans lines is one row whose location reads
  `startLine:startCol–endLine:endCol` (the end exclusive); its text is the first line and
  the preview shows the whole range, while replace still applies the exact range. A match
  longer than the preview column is paged through with the same scroll keys; the header's
  `(N lines)` says how long it is, and paging never changes the replacement range.
- **Project search results as a list** (Alt+Q, or "Project Search Results in List" in
  the palette) is gvim's quickfix export: the result set opens in the shared list
  overlay, grouped by file, filterable, with Enter jumping to a match and the list
  staying open so the set can be walked. Opening the list cancels an in-flight
  selected-match preview read (the result view is being replaced). The list is capped
  independently of the search's 10000-match limit: at most 2000 match rows are built,
  with a final header row and a notice naming how many matches were not listed. gvim has no persistent quickfix
  stack and the results panel stays the source of truth; the next and previous
  project-match chords walk the same set from the editor.
- **Git Grep** (command palette row only) is the same card with a single search field and
  the Files row: the All Files/Case toggles collapse to Case (Alt+C), the replacement and
  scoped rows are absent, and Enter opens the match; its results can be listed with Alt+Q
  too. Tab walks the search field, the Files field and the result rows. The Files patterns
  are git pathspecs (git's own wildcard matching, so *.go matches at any depth and sub/**
  keeps a subtree), passed to git after "--"; the same validation as the project globs
  catches a bad bracket first, and an invalid pattern blocks the search. git grep runs in
  the work tree of the workspace root in one call (its 100-row cap makes a stream
  unnecessary); exit 0 and 1 both mean a finished search (1 is no matches), 2 or more is
  an error notice. The selected-match preview column, with its scrolling and syntax
  colours, works the same way while its result rows have the focus.
- **Residuals of the search surfaces**: replacing closed files above the search read cap
  (a streamed writer; open buffers are covered by the unsaved-tab path) and provider
  sources beyond files, open buffers and the current buffer. Recent files is a recorded
  decision rather than a half-landed source: gvim keeps recent folders for the welcome
  screen and the restored tabs, but no opened-file history to scope a search to, so a
  faithful source needs a new app-level history snapshot and its own streaming/replace
  semantics — deliberately out of this slice (diagnostics and all-workspaces sources
  remain out of scope for the same reason: they need provider-specific snapshot
  protocols; the open dock's terminals are covered by Live Grep's bounded Terminals
  scope above). The preview soft-wraps long lines (Alt+P, in the editor: on Linux the
  setting is per session; the session value is remembered while gvim runs) and its syntax
  colours are automatic — a colour toggle is a deliberate non-goal because the preview
  falls back to plain when the port cannot highlight.

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
| Workspace extract target picker | card, centred | `internal/tui/list` (a plain list) opened by `internal/tui/workspace_ui.go` |
| Extract-tabs multi-select picker | card, centred | `internal/tui/list` with its opt-in `Multi` mode (Space marks, Enter extracts the marked file tabs); the callbacks stay in `internal/tui/workspace_ui.go` |
| Plugin picker | card, anchored above the last row | `internal/tui/picker` |
| Menu-bar dropdown | menu | `internal/tui/menubar` |
| Context menu, "+" menu | menu | `internal/tui/ctxmenu` |
| Anchored popup (LSP hover, ...) | menu | `internal/tui/popup` |
| Sidebar column (File Explorer + Contents) | body, left column | `internal/tui/frame` (geometry and hit map), `internal/tui/sidebar` (section headers and the Contents body), `internal/tui/explorer` (tree body) |
| Plugin widget panel | card (centred float), body of a virtual-buffer pane, or a sidebar section | `internal/tui/pluginpanel` (layout, paint, hit testing), `internal/tui/plugin_panels.go` (placement, focus, routing); a floating panel is a card with the shared frame, a buffer panel is painted inside the pane's text rectangle, a sidebar section's body is painted by the column |

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
6. Plugin-owned surfaces follow the same rules: a floating widget panel is a card, a plugin
   sidebar section is a body of the column with the shared header, and a panel's focus goes to
   the widget under the pointer or in the Tab ring inside the panel, never straight to the
   editor. The declarative widget panel's layout, paint and hit testing live in
   `internal/tui/pluginpanel`.

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
| `{filename}` | Fresh's display name: the file path relative to the workspace root when it is under it, its absolute path otherwise, and the document's own name ("[No Name]", "Untitled-N", "*virtual*") when it has no path. Fresh's markers follow: " [+]" while the buffer is modified, " [RO]" while it is read-only (the same flag `{read_only}` shows). Not clickable, as in Fresh. |
| `{chord}` | the pending multi-key sequence as "[Ctrl+X]" (gvim's one label style), painted faint/muted, and shown only while a sequence waits: it appears on the first held key and is gone when the sequence resolves, misses or is cancelled. (Divergence: Fresh's reference paints the chord with the normal status foreground; the muted look is gvim's choice.) |
| `{read_only}` | "[RO]" for a read-only document; not clickable (gvim has no read-only menu) |
| `{line_ending}`, `{encoding}`, `{language}` | the indicator; click opens the chooser, as before |
| `{lsp}` | the language-server pill (see `docs/lsp.md`) |
| `{palette}` | "Palette: Ctrl+P", or the chord of the active keymap; click opens the command palette |
| `{clock}` | the local time as 24-hour zero-padded "HH:MM", Fresh's `chrono` format ("{:02}:{:02}") read from the model's clock when the row is built. A minute tick redraws the row while the element is configured and visible, so an idle editor still shows the new minute; a config change that adds it takes effect at the next launch (the row is not live-edited this round). The checked-out Fresh reference renders a static colon although its docs mention a blinking one; gvim matches the code. |
| `{warnings}` | nothing: gvim has no general-warning domain (Fresh's `[⚠ N]` counts its tracing warnings). gvim's equivalent list is Show Warnings, which keeps every notice, so a count of it would never reach zero. |
| `{update}` | Fresh's "Update: vX.Y.Z" when the release check found a newer version (`check_for_updates`, `docs/settings.md`); hidden otherwise. Click opens the Update details (the answer, the feed, when it ran, the release page) — gvim has no self-update, so the click never starts an install; Check for Updates and Show Update Output in the palette are the same state. |
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

`{filename}`, `{chord}` and `{clock}` are ordinary configured elements at that squeeze: they
keep no reserved slot and sit wherever the lists put them. `{chord}` only exists while a
sequence is pending, so the elements after it move when it appears and disappears (Fresh does
the same); `{filename}` elides at the left tail like any other element, its " [+]" / " [RO]"
markers being part of the same run.

### Cross-workspace buffer mirrors

When a daemon-hosted file is open in two workspaces, the non-owning workspace shows a
read-only mirror. The status message starts as "Mirror of <workspace>: read-only until
focused", the `[RO]` read-only chip and the `[+]` modified marker follow the shared
buffer's state, and focusing the mirror's workspace moves ownership there (the old
workspace becomes the mirror). A refused ownership request keeps the mirror read-only and
puts "Cross-workspace share conflict: <reason>; the mirror stays read-only" in the status
message; the message is visible on the status row/notice band like any other document
status. No new overlay or dialog is used for this: it is the existing tab status message
plus the existing read-only chip.

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
