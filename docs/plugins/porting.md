# Porting

## From gvim's old JavaScript plugins

The goja/esbuild system is removed. A `.js` or `.ts` file in the plugin folder is **never run**: gvim
shows one notice (`N JavaScript plugin(s) ignored (gvim plugins are Lua now): a.js, b.ts`) and a
`legacy` row per file in **Plugin: Manager**. The old API was five functions:

| Old (JavaScript) | New (Lua) |
|---|---|
| `gvim.registerCommand(name, label, fn)` | `gvim.command.register{ name = name, title = label, run = fn }` |
| `gvim.notify(msg)`, `gvim.setStatus(text)` | `gvim.notify(msg)`, `gvim.status(text)` |
| `gvim.insertText(text)` | `gvim.command.run("insert_char", { text = text })` |
| `gvim.activePath()` | `gvim.buf.current():path()` |
| a `Promise` | not needed: `⏳` functions read like blocking calls inside a task |

## From Fresh's TypeScript plugins

gvim's API is **native Lua, not a Fresh shim**: names follow Fresh's events and concepts, but a port is
a rewrite in idiomatic Lua. The bundled plugins are examples of the technique (see
[bundled.md](bundled.md)). The map:

| Fresh (`editor.*`) | gvim |
|---|---|
| `getActiveBufferId`, `getBufferInfo`, `listBuffers`, `findBufferByPath` | `gvim.buf.current()`, `b:info()`, `gvim.buf.list()`, `gvim.buf.find(path)` |
| `getBufferText(id, start, end)`, `getBufferLength`, `getLineStartPosition(n)` | `b:text(range)`, `b:byte_length()`, `b:offset_of({line = n, col = 0})` |
| `compareBuffers(left, right)` | `gvim.diff.compare_buffers(left, right)`: same CompareResult fields in snake_case (`left_lines`, `left_len`, `left_version` = version token, `eol_only` always false) |
| `replaceRange(buf, start, end, text, expectedVersion)` | `gvim.diff.replace_range(buf, start, end, text, expected_version?)`: one edit and one undo step; `false` for the read-only/binary/out-of-range/stale refusals, exactly as Fresh |
| `getCursorPosition`, `getPrimaryCursor`, `getAllCursors`, `setBufferCursor` | `b:offset_of(b:cursor())`, `b:cursors()`, `b:set_cursor(pos)` |
| `insertText`, `deleteRange`, `replaceRange`, `insertAtCursor` | `b:insert`, `b:delete`, `b:replace`; typing semantics: `gvim.command.run("insert_char", {text = ...})` |
| `executeAction`, `executeActions` | `gvim.command.run`, `gvim.command.run_all` |
| `registerCommand`, `unregisterCommand`, `defineMode`, `setEditorMode`, `setContext` | `gvim.command.register/unregister`, `gvim.mode.define/enter`, `gvim.context.set` |
| `on("after_file_save", handler)` | `gvim.on("after_file_save", fn)`: the names are Fresh's; `buffer_id` becomes `ev.buf`, offsets become `Pos` |
| `setStatus`, `info`, `warn`, `error`, `debug` | `gvim.notify`, `gvim.log.*` |
| `prompt`, `startPrompt` with `setPromptSuggestions` | `gvim.ui.input` and `gvim.ui.set_prompt_suggestions` in its `on_change`, or `gvim.ui.picker` |
| `import { Finder, defaultFuzzyFilter, getRelativePath } from "./lib/finder.ts"` | `require "gvim.finder"`: `Finder.new`/`open`/`close`, `filter`/`score`, `tree`, `open_locations`, `read_snippets`, `relative_path` (see [The shared Finder](#the-shared-finder-port-residual)) |
| `showActionPopup` | `gvim.ui.action_popup` (`action_popup_result` event) |
| `addMenuItem` | `gvim.menu.add_item` / `remove_item` / `clear` |
| `t(...)`, plugin i18n files | `gvim.i18n.t` (`t` alias) + `<name>.i18n.json`; %-prefixed command titles are translated |
| `overrideThemeColors` | `gvim.theme.override` / `clear` / `overrides` |
| `getAllDiagnostics` | `gvim.lsp.all_diagnostics` |
| `addOverlay`, `clearNamespace`, `setLineIndicator(s)`, `setScrollbarMarkers`, `addVirtualText` | `ns:set` with `highlight`, `gutter`, `scroll`, `eol` items |
| `runOnTarget`, `spawnHostProcess` | `gvim.proc.run`, `gvim.proc.spawn` |
| `readFile`, `writeFile`, `readDir`, `fileExists`, `walkTree`, `watchPath` | `gvim.fs.*` |
| `delay`, `setTimeout`, `setInterval` | `gvim.timer.sleep/after/every` |
| `Promise.all` | `gvim.async.all` |
| `getGlobalState`, `setGlobalState`; `defineConfig*`, `getPluginConfig` | `gvim.state`; `gvim.settings.define` |
| `exportPluginApi`, `getPluginApi` | `gvim.plugins.export`, `gvim.plugins.get` |
| `setFileExplorerDecorations(ns, …)`, `setFileExplorerSlots(ns, …)`, the two clears | `gvim.explorer.set_decorations(ns, items)`, `set_slots`, `clear_decorations`, `clear_slots`: snake_case items (`min_width`, `suppress_leading`, `suppress_trailing`, `name_color`, `suppress_name_color`), per-plugin namespaces as in Fresh, 10k entries/namespace and 64 namespaces/plugin; `FileExplorerTooltip` is not supported (no hover tooltips) |
| `computeLineDiff` | `gvim.diff.lines` (or `diff.buffer`) |
| `createVirtualBuffer*`, `setVirtualBufferContent`, `getTextPropertiesAtCursor` | `gvim.buf.create` plus `w:split` plus `b:set_content`, `b:props_at` |
| `beginKeyCapture`, `getNextKey`, `endKeyCapture` | `gvim.input.next_key` (no begin or end) |
| windows, orchestrator, authority, packages | not provided |

Widgets: Fresh's declarative widget API has a read-mostly v1 here (see
[api-ui.md](api-ui.md#widget-panels-and-sidebar-sections)): `mountWidgetPanel`/`updateWidgetPanel`/
`unmountWidgetPanel`, `widgetCommand`, `widgetMutate` and `mountSidebarSection` are
`gvim.ui.mount_widget_panel`, `update_widget_panel`, `unmount_widget_panel`, `widget_command`,
`widget_mutate` and `mount_sidebar_section`; the widget kinds are the interactive tier-1b set
(text, number, dropdown, radio, dual list and wrappers) and there are no floating/dock
placement controls.

Not provided at all (and not stubbed): the translated editor catalogues (the `locale`
setting, resolution and the English catalogue landed; plugin texts are translated; see
[api-core.md](api-core.md#gvimi18n) and [i18n.md](../i18n.md)), project-local plugins, a trust store,
synthetic buffer-group tabs and per-panel views of one buffer, a terminal API, Fresh's window and
authority runtimes, the package manager, the HTTP client API and `render_line`. The tier-1c
decoration surface is provided and painted (virtual lines, conceal, soft breaks, cursor-line
overlay, ranged scrollbar marks and plugin folds), gated by `gvim.has("decor_virtual_paint")`;
the 1c-C seams are provided too (split labels, per-plugin window state, open/focus/close split
wrappers and the path/focus/window/group events), as are `setLineTargets`
(`gvim.buf.set_line_targets`) and fixed-height group layout leaves (`{type="fixed",height=n}`,
height 1..100, re-applied on layout changes; the split border can leave it one row off).
Declared plugin settings (`gvim.settings.define`) are exposed read-only to the host and the
Settings page with their schema and effective values.

### What changes when you port

* **No Promises.** A ⏳ function suspends the calling task. `await foo()` becomes `foo()`;
  `Promise.all([a(), b()])` becomes `gvim.async.all{ a, b }`; `new Promise(resolve => ...)` becomes
  `gvim.async.suspend(function(resume) ... resume(v) end)`. Handlers are Lua functions, not names.
* **Positions.** Fresh uses byte *offsets* into the buffer; gvim uses `Pos{line, col}` (0-based line, 0-based
  byte column). `b:offset_of` and `b:pos_of` convert, on the LF-normalised text (CRLF files differ from
  Fresh's raw offsets). `line_text:sub(col + 1)` is the idiom.
* **Writes are synchronous** (no `flush`): the next read sees the edit. Every `b:edit` call is one undo
  step.
* **Errors are values**: `nil, "<code>: <message>"`, not exceptions.
* **Diff algorithm**: gvim diffs with Myers', not patience, so hunk boundaries can differ from Fresh's gutter.

### Live diff: no host baseline (port residual)

Fresh's `live_diff` diffs against a host-registered baseline (`registerDiffBaseline` plus a
patience `diffAgainstBaseline`, with `getBaselineLines` for the old text). gvim has no baseline
API and this port does not ask for one. Recorded differences:

* The old side is fetched by the plugin: `git show <rev>:./<file>` (HEAD/branch, decoded with the
  buffer's encoding) or `gvim.fs.read` + `gvim.text.decode` for disk mode. No repository, an
  untracked file or a missing file leaves the buffer unmarked; large-file mode is refused with one
  notice instead of diffed.
* The diff is computed with `gvim.diff.buffer` (Myers). Hunk boundaries can differ from Fresh's
  patience baseline, and the word-level refinement (char-LCS similarity, word LCS, caps) runs in Lua
  with a DP-cell budget instead of natively.
* gvim decorations have no `fgOnCollisionOnly` mode, so the diff line backgrounds use the theme's
  `editor.diff_*_bg` keys only (no `editor.diff_*_collision_fg`).
* Virtual rows with the deleted old content and their word emphasis use tier-1c virtual-line
  painting. The shipped TUI reports `gvim.has("decor_virtual_paint")` (the paint surface is
  provided), so the full Fresh view is
  what users get: one `virtual_line_above` row per deleted line with the `-` gutter glyph and
  bold+underlined removed-word `text_overlays`, in Fresh's priority order and cut to the host's
  64-rows-per-anchor cap plus an "… N more deleted lines" summary row. On a host without the
  capability (an older host or the reduced test path) it falls back to line backgrounds,
  new-side word emphasis, gutter indicators and scrollbar marks.
* Rendering caps are Fresh's, made host-aware: full → no-vlines (more than
  `max_virtual_lines` deleted lines) → outline (more than `max_overlay_lines` changed lines), and
  a deletion block longer than the host's 64-virtual-rows-per-anchor cap is summarised by an
  "`… N more deleted lines`“ row.

### Streaming a process into a buffer

A view that streams a command's output (a patch, a log, a diff) does not need a file-backed buffer
and `b:revert()`; a virtual buffer the plugin owns can be appended to as the data arrives. `git_log`
is the reference port:

* Create the panel buffer with `gvim.buf.create` (read-only), put it in the layout/tab where it
  belongs, and start the command with `gvim.proc.spawn` (not `run`, which waits for the whole
  output). Arguments are validated argv arrays; pin the format flags and the environment as the
  bundled `gvim.git` module does, so repository configuration cannot change the format the parser
  reads (or run a program).
* In `on_stdout`, append the chunk: `buf:set_read_only(false); buf:append(chunk);
  buf:set_read_only(true)` (a read-only virtual buffer refuses the append; the flag is
  plugin-facing). Do the incremental parse on the same chunk, carrying the partial last line across
  chunks and capping a single line's parse cost.
* Style rows as they arrive with one `ns:add` per run, not one per line: a `highlight` decoration
  whose range spans N equal-kind lines costs one host call for the whole run.
* **Ordering.** The process's exit callback may arrive before the queued `on_stdout` tasks of the
  same process. Finish one tick after `on_exit` (`gvim.timer.after(1, finish)`), or the last chunks
  would be dropped and a cache marked complete too early.
* Bound it: a byte cap and a line cap, kill the process when either is passed and append a footer;
  a generation token bumped on every new selection/refresh/close makes late callbacks from the
  killed stream no-ops. No whole-buffer read is needed to fill the view.
* Cache by content identity (a commit sha, a baseline id): stream into a file under
  `gvim.dirs.data()` while appending to the buffer, and write a small `.done` marker last, only on
  a clean exit. A cache hit becomes one bounded `gvim.fs.read` plus `buf:set_text`; an interrupted
  stream has no marker and is regenerated.
* `b:revert()` remains the right tool for a **file-backed** buffer (open the path, rewrite the
  file, revert); it costs a tab and a disk round-trip, so the append pattern above is preferred for
  a live view that is not a real file.

### Verifying a default-off bundled port in the real app

A bundled plugin with `default_enabled: false` is exercised by the fake host in its own
package tests, but its real-host path is worth one check because the enable path is config
driven. The procedure (and why it matters):

1. Isolate the config, then enable the plugin in the **user** config:
   `{"plugins": {"<name>": {"enabled": true}}}`.
2. Start the app session and wait for the plugin manager to report the plugin `loaded`; then
   let the session settle.
3. Open a buffer, then open the palette and query the plugin's command titles. The palette
   table is built from the loaded-plugin command table **when the palette opens**, so a
   palette opened before the load settles has no plugin rows and does not gain them while it
   stays open. The plugin's init log can appear while the command table is still settling,
   which makes "init log present, palette row missing" the signature of querying too early,
   not of a config-locked enable defect.
4. Run one of the rows and assert its real effect (the flash, git_log and live_diff ports
   each have such a smoke test; `internal/app/plugins_ports_palette_test.go` covers the
   config-locked enable → palette → command-run chain end to end).

### Path completion needs a built-in-prompt seam (port residual)

Fresh's `path_complete` is passive: it answers the editor's `prompt_changed` event for the
built-in Open File and Save File As prompts with directory suggestions. gvim has neither a
`prompt_changed`-style event nor a way to attach `gvim.ui.set_prompt_suggestions` to a prompt the
plugin did not open: Open File/Switch Project use the native file-browser overlay and Save As uses
the Save-As dialog. The bundled port keeps the completion engine (the exported `complete(input)`
plus a Path Completion prompt command that exercises it) and documents the gap; a future host seam
would emit a prompt-changed event for the browser/dialog inputs, and route the plugin's suggestion
list into those overlays, after which the port can drop its command and match Fresh exactly.

### Compare files: group-lite splits (port residual)

`compare_files` (port of Fresh's `compare_files.ts`) compares through group-lite splits of the
current tab showing the two existing file buffers — there is no synthetic group tab, so the title
segments are stored but not painted. The two files are opened as ordinary tabs when they were not
open (gvim has no tabless view of a real file buffer); **Compare: Close** closes the ones it opened
when they are unmodified, while a file that was already open keeps its tab. Line-ending-only hunks
do not exist because gvim normalises a buffer's line endings (`eol_only` is always false, so there
are no CRLF/missing-newline notes). Filler rows need the host's virtual-line painting
(`gvim.has("decor_virtual_paint")`); without it the panes are level only where both sides have lines.
Lines are paired positionally inside a hunk (Fresh's documented fallback), the command bodies ask
for paths with `gvim.ui.input` instead of Fresh's file picker, and there is no `fresh --diff` launch
hook or script-facing export (the native Compare view handles `--diff`). Enabling the plugin adds
palette rows that duplicate the native Compare commands, which is why it is off by default.

### The shared Finder (port residual)

Fresh's `plugins/lib/finder.ts` (1862 lines) is split in three in the bundled `gvim.finder` library
(`plugin/bundled/lib/finder.lua`): Fresh's fuzzy ranking with hard caps; a prompt frontend over
`gvim.ui.picker` (`filter = "none"`, the library ranks in Lua so custom orderings such as symbols by
source line survive, and `on_close` runs exactly once with `selected` / `cancelled` / `programmatic`);
and a panel backend (`tree`) plus a locations finder (`open_locations`: relative `path:line` rows,
snippet descriptions from open buffers or one bounded read, preview tabs on move, commit on Enter,
cancel closes the preview). The filter source loads once and is ranked per query; a **search
source** (`opts.search`, Fresh's `mode = "search"`) is called from the picker's `on_query` callback
task with the debounced query (so it may wait on a process, as `git_grep` does) and its rows are
capped, with `min_query`, an `on_rows(shown, total)` status hook and `set_footer`/`set_status`/
`refresh` helpers. `diagnostics_panel` uses the tree backend and requires the library lazily on its
first build, so the idle default bundle does not pay for it (the 8 MiB gate).

Reduced or omitted from Fresh's Finder: search sources have no cancellation handle (a superseded
query's `set_items` is discarded by the picker generation guard instead of killing the process), no
per-query process spawn management, the embedded preview pane and utility-dock routing, `syncWithEditor`,
`navigateOnCursorMove`, `panelKeys` and the panel-mode prompt-type events; the panel backend covers
the grouped/deduped tree use only. `lsp_navigation` and `find_references` add the LSP edges:
`gvim.finder.lsp_locations` normalizes `Location`/`Location[]`/`LocationLink[]` and converts UTF-16
columns to bytes when the target line is open. Both are command ports because gvim has no
`lsp_references`/`lsp_implementation` plugin events (the native Find References action has its own
UI); `lsp_navigation` also offers definition/implementation/type-definition list commands that are
gvim additions over Fresh's core direct-jump actions, and the picker has no initial selected index, so
Fresh's enclosing-symbol preselection is not reproduced.

### Git grep: no prompt toolbar (port residual)

Fresh's `git_grep` uses a prompt **toolbar** (`setPromptToolbar` with a Case toggle widget) and drives
the toggle from `widget_event` / `toggleOverlayToolbarWidget`; gvim's prompt is `gvim.ui.picker` and has
no toolbar or in-prompt key surface. The port therefore keeps Fresh's module-level `case_sensitive`
choice (insensitive by default, like Fresh's effective config preset — gvim plugins cannot read
`editor.search.case_sensitive`), shows it in the picker footer/hint line, and flips it with Fresh's
second palette command (`Git Grep: Toggle Case Sensitive`), which refreshes an open prompt and
otherwise applies to the next search. Fresh binds no chord for the command, so the port registers none.
Fresh also disables the Finder's split preview because its floating overlay draws a preview band;
gvim has no overlay preview band, and the port opens no preview tabs either (Enter opens the hit).
The `git grep` process runs through the hardened `gvim.git` gateway (`grep -n --column -I [-i] -e`,
15 s timeout, 2 MiB output cap, first 100 rows, per-line caps); exit 0 and 1 are both success (git
grep exits 1 for no matches), >= 2 is the error status, and paths with colons still parse because the
line/column numbers anchor the match from the left.

### Environment activation is host-managed (port residual)

Fresh's `env_manager` plugin owns detection and activation. In gvim both are host features
(`env.detectors` in the user config, the `gvim.env` runtime API, the trust gate in the core trust
dialog), and the bundled plugin is the thin command/status layer: Activate a detected environment,
Reload after a `.envrc`/`mise.toml` change, Use System, and Status (the `env` status-bar pill shows
`system` / `<name>` / `<name> ✓` / `<name> (locked)`). Reductions: no widget-panel UI yet, no file
watcher (Reload is explicit), no devcontainer deferral/authority handling, no automatic
shell-environment activation (the command is explicit), one local environment, and no persisted
recipe; a bare `.venv` without an interpreter does not count and first match wins, as in Fresh.

### Line targets: what they can and cannot express

`gvim.buf.set_line_targets` maps a *buffer line* to a file path and 0-based line: pressing plain
Enter there (normal mode, count 1, no selection or extra carets) or a single left click opens that
location, in the split labelled `into` when one is live, else beside the pane holding the targets.
Targets cannot open commands, virtual buffers or a historical revision, and a widget-panel row is not
a buffer line. Three port decisions follow (round 11):

* `dashboard` uses targets: its recent-file rows are painted buffer lines, so each paint publishes
  `{line, path}` targets for them and `clear_line_targets` runs on close/unload. The rows remain
  keyboard-navigable through the buffer-local mode (its Enter binding runs first), so the visible win
  is a mouse click on a recent file. Command rows cannot be targets and stay keyboard-only.
* `code_tour` cannot use targets: its steps are a widget-panel card, not lines of a buffer, and its
  step highlight is decoration anchoring in the source file. It keeps local line anchors.
* `git_log` should not use targets: its detail rows' Enter opens the commit's historical blob
  (`git show <sha>:<file>`) as a read-only virtual buffer, exactly like Fresh; a target opens the
  working-tree file, which is wrong for any commit that is not HEAD. The Enter mapping and hunk
  navigation stay local.

### Notes for porters (learned from the bundled ports)

* **Never read a whole buffer.** Scanning a 150 000-line file line by line in Lua blows the time budget.
  Use `b:find` and `b:find_lines` (host side, chunked with `gvim.timer.sleep(0)`), and read single lines with
  `b:line(n)` only for the candidates. `merge_conflict` does this.
* **Use `w:visible()`** for "what is on screen" (soft wrap and folds make `top_line + height` wrong).
* **Debounce** hot events (`buffer_changed`, `cursor_moved`) with `gvim.on(..., {debounce = ms})`, and keep
  handlers under 50 ms: yield to the editor with `gvim.timer.sleep(0)` every few thousand items.
* **Diff on the host**: `gvim.diff.buffer(buf, old_text)` compares without building line arrays in Lua.
* **Pass `version`** to decoration and edit calls computed from text you read earlier, so a change in
  between is `stale` instead of a mis-placed mark.
* **Make initialisation idempotent.** A plugin that loads late is replayed `after_file_open` and
  `buffer_activated` for the open buffers, and you probably also scan `gvim.buf.list()`.
* **No `a, b = b, a` on locals** ([conventions.md](conventions.md#lua-51-notes)); no ⏳ call inside a
  `table.sort` comparator, a `gsub` callback, a metamethod, a `validate` callback or an iterator function.
* **Do not trust settings.** See [limits.md](limits.md#settings-are-untrusted-input).
