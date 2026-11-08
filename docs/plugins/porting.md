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
| `getCursorPosition`, `getPrimaryCursor`, `getAllCursors`, `setBufferCursor` | `b:offset_of(b:cursor())`, `b:cursors()`, `b:set_cursor(pos)` |
| `insertText`, `deleteRange`, `replaceRange`, `insertAtCursor` | `b:insert`, `b:delete`, `b:replace`; typing semantics: `gvim.command.run("insert_char", {text = ...})` |
| `executeAction`, `executeActions` | `gvim.command.run`, `gvim.command.run_all` |
| `registerCommand`, `unregisterCommand`, `defineMode`, `setEditorMode`, `setContext` | `gvim.command.register/unregister`, `gvim.mode.define/enter`, `gvim.context.set` |
| `on("after_file_save", handler)` | `gvim.on("after_file_save", fn)`: the names are Fresh's; `buffer_id` becomes `ev.buf`, offsets become `Pos` |
| `setStatus`, `info`, `warn`, `error`, `debug` | `gvim.notify`, `gvim.log.*` |
| `prompt`, `startPrompt` with `setPromptSuggestions` | `gvim.ui.input` and `gvim.ui.set_prompt_suggestions` in its `on_change`, or `gvim.ui.picker` |
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
| `computeLineDiff` | `gvim.diff.lines` (or `diff.buffer`) |
| `createVirtualBuffer*`, `setVirtualBufferContent`, `getTextPropertiesAtCursor` | `gvim.buf.create` plus `w:split` plus `b:set_content`, `b:props_at` |
| `beginKeyCapture`, `getNextKey`, `endKeyCapture` | `gvim.input.next_key` (no begin or end) |
| windows, orchestrator, authority, packages | not provided |

Widgets: Fresh's declarative widget API has a read-mostly v1 here (see
[api-ui.md](api-ui.md#widget-panels-and-sidebar-sections)): `mountWidgetPanel`/`updateWidgetPanel`/
`unmountWidgetPanel`, `widgetCommand`, `widgetMutate` and `mountSidebarSection` are
`gvim.ui.mount_widget_panel`, `update_widget_panel`, `unmount_widget_panel`, `widget_command`,
`widget_mutate` and `mount_sidebar_section`; the widget kinds are the read-only subset (no
`textInput`, dropdown, radio or dual list yet) and there are no floating/dock placement
controls.

Not provided at all (and not stubbed): the editor-wide i18n (plugin texts are translated, see [api-core.md](api-core.md#gvimi18n)), project-local plugins, a trust store,
painted conceal/virtual lines/plugin folds (the tier-1c store and Lua names landed; painting is the
remaining host increment, see the live_diff residual below), a terminal API, Fresh's window and
authority runtimes, the package manager, the HTTP client API and `render_line`.

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
* Virtual rows with the deleted old content and their word emphasis need tier-1c virtual-line
  *painting*. The store/API landed in 1c-A; until the host reports `gvim.has("decor_virtual_paint")`
  the plugin shows the reduced Fresh view: line backgrounds, word emphasis on the new side, gutter
  indicators and scrollbar marks. The old-content rows, their `-` gutter glyph and the removed-word
  emphasis are the remainder; the full path is already written and activates when the capability
  appears.
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
