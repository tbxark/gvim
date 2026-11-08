# Bundled plugins

gvim ships nineteen plugins inside the binary, written in Lua like any other (their sources are under
`plugin/bundled/plugins/<name>/` in the repository; **Plugin: Manager** shows them, and
`config.json` can override them). Nine are on by default; `flash`, `vi_mode`, `live_diff`, `git_log`,
`compare_files`, `merge_conflict`, `dashboard`, `code_tour`, `lsp_navigation` and `find_references`
are off until you turn them on:

```jsonc
{ "plugins": { "vi_mode": { "enabled": true }, "git_blame": { "enabled": false } } }
```

A plugin you put in `~/.config/gvim/plugins/` with the same name replaces a bundled one.

| Plugin | Default | Commands (palette) | Settings (`plugins.<name>.settings`) |
|---|---|---|---|
| `goto_with_selection` | on | Go to Line with Selection | none |
| `tab_actions` | on | Close Other Tabs, Close All Tabs, Close Tabs To Right, Close Tabs To Left, Move Tab To Left, Move Tab To Right | none |
| `git_gutter` | on | Git Gutter: Refresh, Next Change, Previous Change | `debounce_ms` (300, 50 to 5000, project) |
| `git_statusbar` | on | none (a status bar item) | none |
| `git_find_file` | on | Git Find File, Git Find File: Reload | none |
| `git_log` | **off** | Git Log, Git Log: Current File, Git Log: Close, Git Log: Refresh | none |
| `git_blame` | on (idle until toggled) | Git: Toggle Blame, Git: Show Commit, Git: Copy Commit Hash | `debounce_ms` (150, 50 to 2000, project), `summary_width` (60, 10 to 200, project) |
| `merge_conflict` | **off** | Merge: Start Resolution, Next Conflict, Previous Conflict, Use Ours, Take Theirs, Use Both, Resolve Conflict... | none |
| `diagnostics_panel` | on | Show Diagnostics Panel, Toggle Diagnostics Panel | none |
| `path_complete` | on | Path Completion: Complete Path | none |
| `lsp_help` | on | none (event driven: an LSP server error puts an install hint on the status row, and the LSP status click opens the help popup) | none |
| `lsp_navigation` | **off** | Go to LSP Symbol, LSP: Show Definitions, LSP: Show Implementations, LSP: Show Type Definitions | none |
| `find_references` | **off** | Show References Panel (and the hidden Hide References Panel close command) | none |
| `dashboard` | **off** | Show Dashboard | `show_on_empty` (true), `refresh_seconds` (5) |
| `code_tour` | **off** | Tour: Load Definition..., Tour: Open Workspace Tour..., Tour: Next Step, Tour: Previous Step, Tour: Exit, Tour: Create from Current Location... | none |
| `compare_files` | **off** | Compare Files..., Compare Active File With..., Compare: Next Difference, Compare: Previous Difference, Compare: Switch Side, Compare: Copy Change to Other Side, Compare: Take Change from Other Side, Compare: Close | none |
| `live_diff` | **off** | Live Diff: Toggle (Global), Live Diff: Toggle (Buffer), Live Diff: vs HEAD, Live Diff: vs Disk (unsaved changes), Live Diff: vs Branch..., Live Diff: vs Default Branch, Live Diff: Refresh, Live Diff: Set Default Mode... | `default_mode` (head), `default_branch` (main), `debounce_ms` (75, 10 to 5000, project), `max_virtual_lines` (2000), `max_overlay_lines` (20000) |
| `flash` | **off** | Flash: Jump | `labels`, `skip_rule` (true), `case_sensitive` |
| `vi_mode` | **off** | Vi Mode: Toggle | `auto_start` (true), `arrow_keys`, `ctrl_keys`, `ignore_case`, `smart_case`, `shift_width`, `clipboard_yank`, `search_word_under_cursor`, `hlsearch` |

None of them binds a default key (Fresh's presets use most chords); bind one yourself, with
`gvim.keymap.set("ctrl+alt+j", "flash.jump")` in your `init.lua`, or in `config.json`:

```json
{ "keybindings": { "bindings": [
  { "key": "j", "modifiers": ["ctrl", "alt"], "action": "flash.jump", "when": "normal" }
] } }
```

A plugin command's action id is `<plugin>.<name>`.

## What each one does

**goto_with_selection**: runs `set_mark`, then asks for a line and goes there, so the text between the
caret and the line is selected.

**tab_actions**: closes the other tabs, the tabs to the right or left, or all tabs; modified tabs are
skipped and counted in the status message. Moves the active tab left or right.

**git_gutter** (with Fresh's `diff_nav`): compares the open buffer with the file at `HEAD` and draws a
bar in the gutter for added and modified lines, a triangle where lines were deleted, and marks on the
scrollbar. **Next Change** and **Previous Change** jump between changed regions. Unlike Fresh it compares
the *buffer*, so the marks follow unsaved typing after a short pause. The HEAD text is read once per
file with `git show` and compared in the host (no line arrays in Lua); a file outside a repository or
untracked is left alone, and so is a buffer in large-file mode: it gets no marks, and **Next Change** and
**Previous Change** say that git changes are not tracked there. Exports `hunks(buf_id)` for other plugins (`{type = "added"|"modified"|"deleted",
line, count}`).

**git_statusbar**: the current branch (the first seven digits of the commit for a detached HEAD) on
the right of the status row. It refreshes when a file opens, a tab is activated, a file is saved and when
`HEAD` changes (it watches the directory that holds it).

**git_find_file**: a fuzzy picker over the files git tracks (up to 50 000), opened relative to the
repository root; the list is read again each time.

**git_blame** (reduced; Fresh's panel with history stepping is not ported): **Toggle Blame** shows
author, age and summary of the commit that last changed the cursor line, after the line; only 40 lines
around the cursor are blamed (`git blame -L`), an edited buffer is blamed by its text, answers are kept
for 30 seconds. **Show Commit** opens the commit in a popup, **Copy Commit Hash** puts its hash on the
clipboard.

**merge_conflict** (reduced to in-buffer resolution; the three-panel merge view is not ported): marks
`<<<<<<<` / `|||||||` / `=======` / `>>>>>>>` blocks with highlights, jumps between them, and resolves the
one under the caret as ours, theirs or both in one undo step. **Start Resolution** turns on a key mode
(`n`/`p` next and previous, `u` ours, `t` theirs, `b` both, `q` leaves). It never reads a whole buffer:
it asks the host for the marker lines and rescans only what an edit touched.

**diagnostics_panel** (the first Fresh plugin port; reduced): lists every diagnostic the language
servers have published, grouped by file the way Fresh's panel groups them (a file header, then
`[E] line:col message` rows with the source in the right column; `E`/`W`/`I`/`H` are error, warning,
info and hint). **Show Diagnostics Panel** opens it filtered to the current file, **Toggle
Diagnostics Panel** opens or closes it, and the **All files** toggle inside the panel switches the
filter to every open document (Fresh's `a` key). Enter jumps to the diagnostic and closes the panel
so the keyboard goes back to the editor (a v1 widget panel holds it while it is focused; Fresh's dock
panel stays open), Esc closes it, `diagnostics_updated` rebuilds the rows live, and activating
another file re-filters them. At most 500 diagnostics are listed (the API can return 50 000); a
message is cut to its first line and 120 cells, and paths and sources are bounded too. Ported from
`refs/fresh/crates/fresh-editor/plugins/diagnostics_panel.ts` (275 lines) on the live-panel part of
its `plugins/lib/finder.ts` (1862 lines). Not ported from the Finder library: prompt mode and search
sources, the preview pane, `syncWithEditor`/`navigateOnCursorMove`, the utility-dock placement, the
`q`/`a`/`r` panel keys (the filter is a widget toggle, refresh is automatic, Esc closes) and Fresh's
severity colours. The diagnostic *code* is not shown because gvim's LSP API carries the source but
no code. Like every bundled plugin it binds no key; bind `diagnostics_panel.toggle_diagnostics_panel`
yourself.

**git_log** (reduced to group-lite; Fresh's synthetic single-tab group is not ported): a magit-style
history view built as a layout of three split panels -- a one-line hint strip, the commit log, and the
selected commit's `git show --stat --patch`. The log rows carry Fresh's columns (short hash, relative
date, author initials, subject, [refs]) and theme colours. Moving the log cursor (j/k, arrows,
Page/Home/End) updates the detail after a 60 ms debounce; Enter on the log focuses the detail, Tab
switches pane, Enter on a detail row opens that file at its new-side line (a read-only
`git show <sha>:<path>` view), q/Esc closes, r refreshes and y copies the hash. The diff streams from
`git show --stat --patch` and is killed and footer-truncated at 4 MiB / 20 000 lines; per-file diffs
over 2000 changed lines are excluded with Fresh's footer, and every sha's diff is cached under
`<data>/git_log/` with a completion marker, so revisiting a commit is zero-git. Fresh's folds are
ported: the file and hunk folding ranges are published with `gvim.decor.set_folding_ranges`, the fold
marker shows in the gutter, a collapsed range hides its body behind the placeholder and the normal fold
action toggles it. The detail scroll position is remembered per commit via
`gvim.buf.get/set_window_state` (in-session, cleared with the buffer). Reductions (also in init.lua and
porting.md): the group is a layout of splits of the current tab and the panel buffers are also visible
as tabs; the layout supports fixed-height leaves (`{type="fixed",height=1}`), so Fresh's one-row
toolbar renders as one row (the split border can leave it a row off); the log is a styled buffer
whose cursor is the selection (a line decoration tints it), not a described List widget; the detail is raw git-show text carrying plugin decorations,
not a host `.diff` background layer; there is no HEAD watcher (r refreshes, as Fresh's view). It is
**off by default** for the idle-heap gate (see Memory).

**live_diff** (with Fresh's diff_nav navigation surface): shows the open buffer against a baseline -- the
file at git HEAD (default), the file currently on disk, or a chosen git branch/ref -- with per-buffer
mode toggles and Fresh's eight Live Diff commands (no default keys). Added/modified new-side lines get
a background in the theme's `editor.diff_*_bg` colours and + / ~ gutter glyphs, word-level changes are
bold+underlined, and every hunk gets a scrollbar mark. The old side is fetched by the plugin
(`git show` for HEAD/branch, an encoding-aware disk read for disk mode) and diffed with
`gvim.diff`/Myers; word refinement runs in Lua under Fresh's caps. Large-file mode is refused with one
notice, and the plugin is idle outside a repository. It watches the git directory and, in disk mode,
the file itself, so commits and external writes refresh the diff. Deleted lines' old content is drawn
as tier-1c virtual rows above the seam, with the `-` gutter glyph and bold+underlined removed words, in
Fresh's priority order and capped at the host's 64 virtual rows per anchor plus an "… N more deleted
lines" summary row (the capability is live on the shipped host; an older host falls back to the line
backgrounds and new-side emphasis). It is opt-in like Fresh's live_diff.

**path_complete** (reduced; Fresh's passive prompt hook has no gvim seam): Fresh's plugin answers the
editor's built-in Open File / Save File As prompts with directory suggestions. gvim's Open File and
Switch Project are the native file browser and Save As is the Save-As dialog, and
`gvim.ui.set_prompt_suggestions` only fills a prompt a plugin opened, so there is no built-in prompt
event to answer; the completion engine is ported faithfully (directory/pattern parsing, hidden-file
rule, case-insensitive prefix, directories first then alphabetical, 100-row cap, and the *new file*
row for a path that does not exist yet) and exposed two ways: a `complete(input)` export for other
plugins, and one command, **Path Completion: Complete Path**, that opens a path prompt with live
suggestions (Enter on a directory descends, Enter on a file opens it, Esc cancels). Fresh's plugin
registers no command and no key; the gained command is the documented deviation, and binding it is
left to the user (no default key is added).

**lsp_help** (reduced): Fresh's per-language install hints for when a language server is missing or
fails. On `lsp_server_error` the plugin puts a Fresh-style line on the status row (*<Language> LSP
server not found — click the LSP status for install help*) and remembers the failure per language;
the next LSP-status click opens an action popup with the entry's explanation, copyable install
commands and gvim-native quick actions (Start/Restart LSP Server, Stop LSP Server, Toggle LSP for
This Buffer, Disable <Language> LSP, Go to Definition, Find References, Dismiss). Install hints are
carried for the languages a gvim user is likely to configure (Go, Rust, Python, TypeScript/
JavaScript, C/C++, Java, JSON, YAML, TOML, Markdown, Ruby, Bash, Lua, HTML, CSS, SQL). Reductions:
gvim's `lsp_server_error` payload carries only the error text (no language, error type or server
command), so the language is the active buffer's, the server command in the message is the entry's
documented default and *not found* is inferred from the error text; gvim's LSP status click also
opens the native server list, so the help popup stacks on it; there is no workspace-symbol quick
action in gvim's command table. Fresh has no i18n file for this plugin; the generic strings are
translated here and the per-server hints stay English. Like Fresh's, the plugin registers no
commands and no keys.

**compare_files** (port of Fresh's own compare plugin; reduced): **Compare Files...** (or **Compare
Active File With...**) opens the two paths you type and shows them as two editable splits of the
current tab — changed lines get a background, in-line changes a highlighted span, a gutter glyph and
a scrollbar mark show each change, the panes scroll together anchored at every change, and
**Next/Previous Difference** walk the hunks. **Copy Change to Other Side** (`vimdiff` `dp`) and **Take
Change from Other Side** (`do`) merge the hunk under the caret through the host's compare offsets and
`replace_range`, one edit and one undo step per merge. The diff comes from
`gvim.diff.compare_buffers`, so no text crosses into Lua; a binary or large-file side is refused with
a status message. It is opt-in (`default_enabled: false`) for the bundled idle-heap budget and
registers no default keybindings, matching Fresh; the native **Compare** view stays the default
surface.

**lsp_navigation** (with the shared finder): a port of Fresh's plugin on the shared `gvim.finder`
library. **Go to LSP Symbol** asks the server for `textDocument/documentSymbol`, lists the symbols as
`[kind] name` rows with the source line as the detail, filters them with Fresh's fuzzy algorithm
(re-sorted by source line) and previews the selection in the buffer: moving the highlight moves the
cursor onto the symbol and paints it in the search-match style, Enter commits the jump, Esc restores
the cursor and clears the highlight. Symbol names are refined against their line (servers report the
declaration start), and the kinds and both response shapes (DocumentSymbol/SymbolInformation) follow
Fresh. The three **LSP: Show Definitions / Show Implementations / Show Type Definitions** list
commands are gvim additions (in Fresh those are core actions that jump directly; gvim keeps its
native Go to Definition rows) and share the references location list: one result jumps, several open
the filterable list with preview tabs. Reductions (also in porting.md): the picker has no initial
selected index, so the enclosing symbol is not preselected as in Fresh, and the highlight is a
decoration, not an overlay namespace. No default key binding (Fresh's plugin registers none).

**find_references** (reduced; command driven): Fresh's plugin answers the host's `lsp_references` /
`lsp_implementation` events with a finder list; gvim has no such plugin events (its native Find
References action opens its own UI), so the port exposes Fresh's own command keys — **Show References
Panel** asks the server for `textDocument/references` for the symbol under the cursor (UTF-16 column,
`includeDeclaration = true`, results capped at 100 rows) and shows a filterable location list:
`relative path:line` rows, the source line as the detail (open buffers read in place, other files via
one bounded read), a preview tab as the selection moves, Enter commits the jump and the hidden **Hide
References Panel** command closes the list. Empty results, a missing symbol/language and server errors
keep the Fresh status-line messages. Reductions: no reference/implementation events (commands
instead), no implementation listener (that list lives in lsp_navigation), and no source-split reuse
(preview tabs do). No default key binding (Fresh's core action has its own chord; the plugin
registers none).

**dashboard** (reduced Fresh port): a read-only start-screen tab shown when the editor has no file
open — a centred frame with the repository state (branch, ahead/behind the default branch,
working-tree counts, origin), recently visited files, keyboard-navigable quick actions (settings,
explorer, live grep, theme, Git Log, diagnostics) and best-effort `df` disk rows. Tab/Down/j and
Shift-Tab/Up/k move, Enter activates (a buffer-local mode); **Show Dashboard** reopens it. It
auto-opens on an empty editor and after the last file closes (`show_on_empty`) and closes when a file
opens; a timer refreshes it (`refresh_seconds`). Reductions: no weather/GitHub/network sections, no
third-party section API or Utility Dock placement, no mouse clicks on rows and no sessions section.

**code_tour** (reduced Fresh port): loads a Fresh `.fresh-tour.json` or a VS Code Edit CodeTour
`.tour` (auto-detected; bounded at 1 MiB / 200 steps), shows the steps in a floating widget card (list,
step counter and location, prose as styled plain text, Previous/Next/Jump/Exit buttons, hint bar) and
walks them: each step opens its file at the line range with `gvim.buf.open` and highlights it with
anchored decorations (the plugin keeps local line anchors; `gvim.buf.set_line_targets` is now
available for a future version). One tour at a time; the last tour and step are
restored from `gvim.state` on the next load. **Tour: Create from Current Location...** writes a
VS Code-compatible starter `.tour` under `.tours/`, atomically. Load/schema/step errors stay in
notices. No default bindings (Fresh's keys live in its dock-buffer mode; here the panel owns the
keyboard).

**The Markdown outline is not a plugin in gvim.** Fresh ships the Contents section as the bundled
plugin `markdown_toc.ts`; gvim builds it into the TUI as the sidebar section under the File Explorer
(`docs/sidebar.md`), so there is no Lua plugin to load, and the palette rows **Markdown: Show
Outline**, **Markdown: Close Outline** and **Markdown: Contents — Follow Cursor/Scroll** are built-in
commands like the compare rows. The config keys stay under the plugin's old name, so a Fresh config
keeps working: `plugins.markdown_toc.enabled` and `plugins.markdown_toc.settings` with
`max_depth` (6, 1 to 6), `follow` (`cursor` or `scroll`), `fold_buffer` (false), `rows` (10,
1 to 200) and `auto_open` (true). The one residual gap against Fresh is `fold_buffer`: gvim's editor
folds are indentation folds, so a heading usually opens none to fold.

**flash**: label-based jump. **Flash: Jump** starts a session; every character you type extends a literal
pattern, every visible match in every window gets a label, and typing a label jumps there (Backspace
shortens the pattern, Enter jumps to the nearest match, Esc or any other key ends it). Labels follow
flash.nvim: the nearest matches get the most comfortable keys, and a letter that is also the next
character after a match is not used.

**vi_mode**: vi-style modal editing: normal, insert, visual (character, line, block) and operator-pending
modes, counts, operators with motions and text objects, registers, marks, `.` repeat, and `:` commands.
It starts in normal mode (`auto_start`); **Vi Mode: Toggle** switches it off and on. The panic chord
`ctrl+^` clears every plugin input mode, vi's included: vi says so, and **Vi Mode: Toggle** brings it back.
Three quick `Esc` presses do **not** (vi binds `Esc`, so mashing it is normal). `ctrl+c` is an `Esc`: it
leaves insert and visual mode and cancels a pending command and the `:` and `/` prompts. Keys vi does not
bind (`ctrl+s`, `alt+...`, function keys) still reach the editor. A selection made with the mouse (a drag,
a double click) or with shift and the arrow keys is the visual mode, so `d`, `y`, `c` and the rest act on
it; a paste puts the text before the caret in normal mode (like `P`), replaces the selection in visual
mode and is inserted in insert mode. A Vim command vi does not have (`gn`, `gq`, `=`, `R`, `@`, ...)
says `vi: ... is not supported`, and the keys that belong to it (the motion of `gq`, the text typed
after `cgn` or `R` until `Esc`, the register of `q`) are dropped instead of running as commands.
The `:` and `/` prompts keep a history (Up and Down), the matches of a search are highlighted in the
window (`:noh` hides them, `:set nohls` or the `hlsearch` setting turns it off), a search that wraps says
so, and `:set` changes `hls`, `et`, `ts` and `sw` for real and refuses what the editor decides (`ai`, `is`).
Exports `toggle`, `enable`, `disable` and `is_enabled`. Its README
(`plugin/bundled/plugins/vi_mode/README.md`) lists the keys; the differences from Vim are:

* Vim's insert-mode ctrl keys (`ctrl+w`, `ctrl+u`, `ctrl+o`, `ctrl+r`, `ctrl+d`, ...) and the `ctrl+w`
  window prefix are not provided: they run the keymap's own binding (`ctrl+o` opens the file picker,
  `ctrl+r` Replace, and on the macOS keymap `ctrl+w` closes the tab);
* no macros (`q`, `@`; use the editor's own), jump list, replace mode (`R` outside visual), sentences,
  folds, `:!` filters, `:normal`, `gn`/`cgn`, `gq`/`gw`/`=`/`!`, `g;`, and the marks `'.` `` `[ `` `']` `^`
  (each says it is not supported);
* marks do not move when the text changes;
* a count insert (`3ix<Esc>`) or `.` repeat is one more undo step; a change operator and the text typed
  after it (`cw foo<Esc>`) are two undo steps, because the editor has no undo group that stays open
  while keys are typed;
* `j` and `k` are the editor's own moves (by display rows on a wrapped line); `d3j` and the visual mode
  count logical lines; the caret column after `u`, `guu`, `gUU`, `g~~`, `Vy`, `yip` can differ from Vim's;
* only the primary caret is used; block mode counts characters, not display cells;
* a file still being indexed (large-file mode) refuses commands with a message until the index is ready.
* search (`/`, `?`, `n`, `N`, `*`, `#`, `:g`) is not available on a file in large-file mode: vi says
  "no search in large-file mode"; use the editor's own find.

## Memory

Plugins are loaded eagerly and cost heap while idle (about 18 KB of bytecode per Lua function plus
tables). `TestBundledPluginsIdleHeap` in `internal/app` starts the **real runtime with the default-enabled
bundled set** in a real editor session with a file open, forces a full collection, and compares the live
heap with the same session under `--no-plugins`. The measurement runs in a **fresh process** (the test
starts the test binary again and runs only itself there), because the live heap after the whole package
is about 88 MiB and moves by several MiB between two measurements; it also fails when the cost reads
under 1 MiB, so a polluted baseline cannot pass unnoticed. The gate is 8 MiB: if the default set ever costs more,
the largest plugins must become `default_enabled: false` in their `plugin.json` and this page must say so.

Measured with the plugin set of this release (macOS arm64, Go 1.27, pure heap in use after a full GC):

| | Heap |
|---|---|
| the editor session without plugins | 1.86 MiB |
| the same session with the 9 default-enabled plugins loaded and idle | about 8.9 MiB |
| **cost of the default set** | **about 7.0 MiB** (gate: 8 MiB) |

The gate passes with the nine default-enabled plugins (about 1 MiB of headroom). Round 9 added host
API surface and four new ports; the ten-plugin set first measured 8.07–8.10 MiB, over the gate, so
`merge_conflict` (about 1.35 MiB) ships `default_enabled: false` as well. The shared Finder library is
not part of the idle cost: `diagnostics_panel` requires `gvim.finder` lazily on its first build
(requiring it at load measured 8.7 MiB with the round-8 set, over the gate).
The opt-in plugins stay off for the heap budget: a trial measurement with `git_log` loaded too read
about 8.7 MiB of idle heap (over the gate; that configuration never passed), `live_diff` is opt-in
like Fresh's, and `compare_files` ships off as well. Per plugin, in
a bare runtime that holds only that plugin (each figure includes about 0.3 MiB for the runtime itself, and
the first one loaded pays for the shared prelude): `goto_with_selection` 0.6 MiB, `git_find_file` 0.7,
`git_statusbar` 0.8, `tab_actions` 0.85, `diagnostics_panel` about 0.9 (measured as the rise of
the default set when it joined it, not in a bare runtime), `git_log` about 1.9 (the same kind of rise;
that is why it is off), `path_complete` and `lsp_help` together about 1.2 (the same rise; both are
small passive plugins, so they stay on), `git_gutter` 1.0, `git_blame` 1.15,
`merge_conflict` 1.35 (flipped off in round 9 when the ten-plugin set measured 8.07–8.10 MiB),
`flash` 1.55, and **`vi_mode` 6.9 MiB**. `vi_mode` is the one that would bust the
budget (the default set plus vi would be about 14 MiB), which is why it is off by default; `flash` is off by
default as well. If you do not use the git plugins, switching them off in the config saves about
4 MiB.
