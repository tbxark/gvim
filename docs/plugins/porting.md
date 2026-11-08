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
| `prompt`, `startPrompt` with `setPromptSuggestions` | `gvim.ui.input`, `gvim.ui.picker` |
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
| `t(...)`, windows, orchestrator, authority, widgets, themes, packages | not provided |

Not provided at all (and not stubbed): HTTP, i18n, menus, project-local plugins, a trust store,
conceal, virtual lines, plugin folds, a terminal API, Fresh's window, authority and widget runtimes,
the package manager, `render_line`, and mouse and focus events.

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
