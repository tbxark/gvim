# Conventions

Read this page once before the API pages. Everything here applies to every function.

## Entry

```lua
local gvim = require "gvim"
```

`require "gvim.buf"` and `gvim.buf` are the same table; every area is both a field and a
module. `gvim.git` is a Lua module that ships with gvim (`require "gvim.git"`), loaded on
first use. `gvim.api_version` is `1`.

`json`, `re`, `utf8` and `bit` are also globals (the same tables as `gvim.json` and so on).
There are no other globals beyond the Lua standard library that gvim leaves in the sandbox
([api-data.md](api-data.md#the-standard-library)).

## Types

```text
Pos     = { line = 0, col = 0 }               -- see the bases table below
Range   = { from = Pos, to = Pos }            -- half open: `to` is not included
Edit    = { range = Range, text = "..." }
Color   = "#rrggbb" | {r, g, b} | "editor.selection_bg" | "error"
Style   = { fg = Color, bg = Color, bold = true, italic = true, underline = true,
            strike = true, reverse = true, extend = true }   -- all optional
Key     = { key = "a", text = "a" | nil, ctrl = false, alt = false, shift = false, super = false }
Item    = "text" | { text = "text", detail = "...", keybinding = "...", disabled = true, ... }
```

* `Color` forms: a hex string, a table `{r, g, b}` (0 to 255), a **theme key** `"section.key"`
  (any key of the active theme, for example `"editor.selection_bg"`), or a **semantic name**:
  `error`, `warning`, `info`, `hint`, `add`, `change`, `delete`, `match`, `accent`, `dim`.
  Theme keys and semantic names are resolved when the screen is painted, so they follow
  `select_theme`.
* `Style.strike` is accepted and stored, but gvim's cell grid has no strike attribute yet, so it
  is not drawn.
* An `Item` given to a picker or popup may be a string or a table with any extra fields; the
  functions that return the chosen item give you back **your own table**.
* Helpers: `gvim.pos(line, col)`, `gvim.range(l1, c1, l2, c2)`, `gvim.pos_cmp(a, b)` (-1, 0, 1)
  and `gvim.range_eq(a, b)`.

## Bases and ranges

These are binding and tested (`TestBasesTable`).

| Where | Base |
|---|---|
| Every `line` and `col` returned or accepted (`Pos`, `Range`, hunk starts, decorations, grep hits, diagnostics, events) | 0-based. `col` is a **byte** offset into the string `b:line(n)` returns (the decoded line text; an invalid UTF-8 byte appears as U+FFFD, 3 bytes) |
| Lua array indexes, `string.find` and `re.find` results, `utf8.*` positions | 1-based |
| `gvim.ui.select`, second result | the 1-based position in the `items` array (the host's own item id stays 0-based Go; the runtime adds 1) |
| `gvim.workspace.grep` | `{path, line, col}` with 0-based line and 0-based byte col (the app converts the 1-based rune values of live grep, using the hit text) |
| `gvim.lsp.diagnostics` | ranges converted from LSP UTF-16 columns to byte columns, using the line text. The **parameters and results of `gvim.lsp.request` are not converted**: they keep the protocol's UTF-16 columns |
| `b:offset_of`, `b:pos_of`, `b:byte_length` | 0-based bytes of the LF-normalised text (a CRLF file counts `\n`, one byte per line break; `b:info().line_ending` tells which it is) |

The idiom for the text of a line from a column on is `line_text:sub(col + 1)`.

Ranges are clamped and never an error. A column inside a UTF-8 sequence rounds down to the start
of the sequence. More exactly:

* `b:text{from = Pos(l, 0), to = Pos(l + 1, 0)}` returns line `l` **including** its `"\n"`.
* A `to.col` past the end of a line clamps to the end of the line (before the `"\n"`).
* `to.line >= line_count` clamps to the end of the document; no newline is added.
* `b:delete(range)` uses the same rules.
* `b:set_lines(first, last, lines)` works on the array of lines `L[0..n)` (`n = line_count >= 1`):
  `first` and `last` are clamped into `[0, n]`, `first > last` is `invalid`; the new array is
  `L[0..first) ++ lines ++ L[last..n)`; an empty result is `{""}`; the text is the array joined
  with `"\n"`. So `set_lines(n, n, {"x"})` appends a line and `set_lines(0, n, {})` empties the
  document.
* In large-file mode the byte columns count in the *decoded* line text that `b:line(n)`
  returns, not in the raw bytes of the file.

## Version tokens

`b:version()`, `b:info().version` and the `version` of events are **strings**
(`"<generation>.<version>"`). Compare them for equality and nothing else: an undo returns the
earlier token together with the earlier text, so equal tokens mean equal text, and a buffer that is
replaced as a whole (revert, reload, encoding change) gets a new generation. The option
`version = tok` of `b:edit` and of decoration calls makes the call fail with `stale` when the
buffer is not in that state any more. The zero token never names a real state.

## Errors

A programming error (a wrong argument type, a missing required field) **raises**
(`bad argument #1 to get (number expected, got string)`; the function name is the one Lua knows
the callee by, so it can be a short or local name). An *expected* failure returns two
values:

```lua
local ok, err = buf:insert({ line = 0, col = 0 }, "x")
if not ok then
  -- err is "<code>: <message>", for example "read_only: buffer is read only"
  gvim.log.warn("insert failed: %s", err)
end
```

Codes: `read_only closed too_large invalid not_found busy timeout cancelled io exec
unsupported conflict limit stale`. `gvim.err_code(err)` returns the code of an error string (or
`nil`).

* A function whose host capability does not exist (a stage the editor does not implement, no
  language server configured) returns `nil, "unsupported: <name>"`. `gvim.has(feature)` reports
  features: `decor`, `windows`, `virtual_buffers`, `input`, `lsp` (a language server is
  configured) and `git` (a `git` binary is on the path; checked at every call).
* A method of a handle whose object is gone (a closed buffer, a closed window) returns
  `nil, "closed: ..."`, and `h:valid()` is `false`.

## Tasks and the ⏳ calls

Every entry point of a plugin runs as a **task**: the main chunk, a command, an event handler, a
timer or process callback, a prompt callback, and the function you pass to
`gvim.async.spawn`. All Lua runs on the editor's update goroutine, one slice at a time, so there
are no data races and no locks.

A function marked **⏳** in these pages may suspend the task: it reads like a blocking call, and
the editor goes on while the file is read, the process runs or the user types an answer.

```lua
local text = gvim.fs.read(path)                    -- ⏳ the task waits; the editor does not
local name = gvim.ui.input { prompt = "Name:" }    -- ⏳ waits for the user
local r = gvim.proc.run { "git", "status", "-s" }  -- ⏳
```

* A ⏳ function works only inside a task. Called from the main thread of a state it raises
  `async function called outside a task`.
* A task that waits costs nothing, and its time budget starts again when it resumes.
* `gvim.async.spawn`, `gvim.async.all` and `gvim.async.suspend` give you concurrency; see
  [api-system.md](api-system.md#gvimasync).
* At most 64 tasks of one plugin may be parked at once. The call that would be the 65th returns
  `nil, "limit: parked task quota (64)"` and logs a warning; the plugin is not killed.

**Where you must not wait.** A ⏳ call inside a function that gvim calls from Go cannot work, and
the task fails with `async call inside a Go callback`. That covers: a `table.sort` comparator, a
`string.gsub` callback, a metamethod (`__index` function, `__tostring` called by `tostring`,
`print`, `string.format` or `gvim.log`, `__lt` called by `table.sort`, and so on), a `for ... in` iterator
function that awaits, the body of `b:undo_group`, a `validate` callback of `gvim.ui.input`, a
`before_file_save` handler, an `on_unload` function and a `shutdown` handler. Write
`while true do local v = gen() ... end` instead of an iterator that awaits. A generator that only
yields (never awaits) works in `for ... in`.

`pcall` and `xpcall` are yield-safe (they run the function in a coroutine of their own), so an
await inside `pcall(function() ... end)` works. Each `pcall` costs one coroutine, about 3 µs and
5.6 KB: avoid it in a hot loop. Nesting is limited to 190 levels. `coroutine.wrap` and
`coroutine.resume` forward awaits to the task as well.

**Deferred calls.** Some functions cannot act before they return, because the action could
re-enter the plugin runtime while your slice is on the stack. `b:save()`, `b:revert()` and
`gvim.buf.open()` are ⏳ and resolve when the work is done. `gvim.command.run` and `run_all`
**defer** these actions: `save`, `save_as`, `save_all`, `quit`, `quit_discard`, macro replay
(`play_last_macro`, `prompt_play_macro`), the plugin management and `init` commands, and any
plugin command (`<plugin>.<name>`). Such a `command.run` returns `true`, and the action happens
after your slice. If you need the effect, use the ⏳ function (`b:save()`), not
`command.run("save")` followed by a read. Every other action runs at once. `gvim.command.run`
and `run_all` act on the **active document** only.

## Handles

`Buffer`, `Window`, `Subscription`, `Timer`, `Process`, `Task`, `Namespace`, `Picker`,
`Popup`, `StatusItem`, `Watcher`, `Command`, `Binding`, `Mode` and `Regex` are userdata with
locked metatables. You cannot add fields to them or read their metatable. `tostring(h)` is
`Buffer(7)`, `h:valid()` tells whether the object is alive (where it applies), and `==` works.
`Buffer` and `Window` handles are cached per id, so `a == b` and using them as table keys work.
A handle you keep after its buffer closed keeps working as a dead handle. Handles belong to
the state that made them: they cannot be passed to another plugin.

## Callbacks

Callbacks are always Lua functions, never names. A callback that raises is logged with its
line; it does not stop other handlers. Five failures in a row unsubscribe an event handler
(with a log line). A timeout, a Go panic or a heap-guard cancel is a **strike**; three strikes in
60 seconds suspend the plugin (its commands still run; events, timers and key layers stop).

## Lua 5.1 notes

gopher-lua is Lua 5.1 with `goto`. There is no integer subtype, no bit operators (use
`bit.*`), no `utf8` library in the box (gvim provides one, see [api-data.md](api-data.md#gvimutf8)),
and `#` on a table with holes is the usual border.

**A known miscompile.** gopher-lua v1.1.2 compiles a swap of two **local** variables,
`a, b = b, a`, as two assignments one after the other, so the result is `b, b`. Fields,
upvalues and globals are fine. Write the swap through a temporary, `local t = a; a = b; b = t`,
or swap table fields. The bundled plugins avoid the pattern and say so where they do.
