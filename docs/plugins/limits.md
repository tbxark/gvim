# Sandbox, limits and known gaps

## What the sandbox is for

A plugin is the user's own code: it runs with the user's file and process rights, through
`gvim.fs` and `gvim.proc`, exactly as a Fresh plugin does. gvim has no trust store and no permission
model, and **project folders can never ship a plugin** (the only thing a repository controls is
`plugins.<name>.settings` and `enabled: false`, below). The limits on this page protect the *editor*
from plugin **bugs**: a loop that never ends, a pattern that backtracks forever, an event storm, memory
that grows without bound. They are not a security boundary against a hostile plugin, and the Go
builtins that the sandbox did not replace can still hold the editor until they return. Do not install a
plugin you would not run as a shell script. If a plugin does make the editor unusable, start gvim with
`gvim --no-plugins`.

What a plugin can reach: its own Lua state; the `gvim` API; files and processes with the user's rights;
`json re utf8 bit`; its own directory and the bundled `gvim.*` modules through `require`. Nothing else:
no `io`, no `os.execute`, no `debug`, no `package`, no `load`, no other plugin's state (exports are
marshalled copies), no Go objects, no goroutines or channels.

## Time budgets

All Lua runs on the editor's update goroutine, in **slices**. The clock runs only while Lua runs: a
task that waits (⏳) costs nothing and gets a fresh budget when it resumes, and a slow *editor*
operation that you called (an edit, a big read, a search) is not charged to the plugin.

| Slice | Budget | On overrun |
|---|---|---|
| main chunk, `init.lua` | `min(4 x plugin_timeout_ms, 5 s)` (default 2 s) | the plugin is `failed`, everything it registered is rolled back |
| a command (palette, key, menu, `command.run`, a mode key handler) and a callback after user interaction (prompt, picker, popup, `next_key`) | `plugin_timeout_ms` (default 500 ms) | the task is aborted with `context deadline exceeded` at the line it was on; a strike |
| an event handler, a timer, a process, watch or status-click callback | `min(50 ms, plugin_timeout_ms)` | the same |
| a `before_file_save` handler | 25 ms, must not wait | the veto is ignored; warning and strike |
| `on_unload` | 200 ms for all the plugin's functions together, must not wait | abandoned; the remaining functions are skipped |
| `shutdown` handlers | 200 ms for all plugins together, 50 ms each, no waiting | abandoned |

* All event and timer slices that start in one editor update share **100 ms**; the rest waits for the
  next update (fair: plugins take turns). User-triggered commands are not subject to this cap.
  A chain of tasks that keeps re-queueing itself (`gvim.async.spawn`, `command.run`) is cut after twice
  that per update; if it still has work queued after 10 such updates in a row it gets a strike (a "task
  storm"), and the strike that suspends the plugin drops the work it has queued.
* A slice that has not ended after **four times** its budget is killed at once, even inside a slow host
  call.
* **Strikes**: a timeout, a Go panic or a heap-guard cancel is a strike; three within 60 seconds suspend
  the plugin (`suspended` in the manager; its commands still run, its events, timers and key layers
  stop). Ordinary Lua errors are not strikes.
* **Heap guard**: the heap is read when a slice begins and sampled every millisecond once the slice has
  run for 2 ms; if the allocated objects grew by more than 128 MiB, a forced collection confirms that the
  *live* heap grew by that much, and the slice is cancelled (at once, without waiting for the collection,
  when the growth passes the limit plus the live heap that was there). It measures the whole process, so a
  large file loading at the same time could cancel an innocent plugin; it is a circuit breaker, not an
  accountant. A single Lua instruction cannot be interrupted, so the peak is the limit plus what the
  instruction that crossed it allocated: see [the heap guard and a single concatenation](#the-heap-guard-and-a-single-concatenation) below.
* **Timer pace**: a plugin that loops on `gvim.timer.sleep(0)` (or `after`/`every` with a delay under 10 ms),
  or whose timer callbacks keep the editor busy (400 ms of every second), never trips a slice budget but
  can burn a core. gvim counts the arms of a second and the busy time of every task slice of the plugin
  (timers, sleeps, commands and events alike). Two hot seconds in a row throttle the plugin: its timers and
  sleeps wait at least 100 ms (or four times its longest callback), a warning goes to the log and the
  status row **once**, and the loop goes on at a pace the editor can afford. While it is throttled only the
  busy time counts: a second with 150 ms or more of task time is hot, and five hot seconds in a row give a
  strike (three suspend the plugin, as for any runaway). A loop that waits out the floor between its turns
  is calm, so a bare `sleep(0)` spin is throttled and warned, but not struck; a callback that keeps the core
  busy through the floor is struck. Three calm seconds lift the throttle. A plugin that waits for real
  intervals is never affected, and a cooperative job that does a little work between its turns is only
  throttled (`TestCooperativeJobIsNotSuspended`).
* `plugin_timeout_ms` is read from the config (default 500, 1 to 60000); the project config can only
  lower it. The load budget is capped at 5 s even for 60000.

## Quotas

Counted per plugin; going over returns `nil, "limit: ..."` (or raises, where noted).

| Resource | Limit |
|---|---|
| event subscriptions | 256 |
| commands | 512 |
| timers | 64 (`debounce` raises) |
| watchers | 32 |
| live processes | 16 |
| decoration namespaces | 64 |
| key bindings (normal context and modes together) | 4096 |
| status items | 8 |
| live pickers and popups | 8 |
| in-flight background jobs (`Work`) | 32 |
| parked tasks | 64 (the awaiting call returns the error) |
| live tasks | 10 000 |
| registered `on_unload` functions | 16 |
| decorations | 20 000 per (plugin, namespace, buffer) and 50 000 per plugin; at most 256 drawn per kind and line |
| virtual buffers | 64 per plugin, 256 in all; 64 MiB of text per plugin, 256 MiB in all |
| queued `gvim.emit` events | 1000; payload 64 KiB |
| `gvim.state` | 1 MiB of keys and encoded values per plugin |
| log message | 4 KiB; the log keeps 2000 entries |
| cached `Buffer` and `Window` handles | 10 000 per state (invalid ones are evicted first) |
| files in a plugin | 64 `.lua` files, 256 KiB each (`plugin.json`: 1 MiB), 5 MiB in all; the registry of a Lua state 1 048 576 slots |
| call depth | 254 Lua frames (`stack overflow`); `pcall` nesting 190 |

## The cap table

Every Go function that allocates in proportion to its arguments computes the final size *before*
allocating and raises `resulting string too large` (or the stated error) beyond the cap. Sizes use
64-bit arithmetic and division, never an unchecked multiplication. Constants live in
`plugin/limits.go`; each row has a test (`TestStdlibCaps`).

| Function | Rule |
|---|---|
| a string crossing the API, `string.rep`, `table.concat` result | at most 64 MiB (`MaxString`) |
| `string.rep(s, n)` | `n <= 0` gives `""`; `n >= 2^53` or `#s * n > 64 MiB` raises, with no multiplication |
| `table.concat(t, sep, i, j)` | sums the element lengths first (a non-string element raises); over 64 MiB raises |
| `string.gsub` (luapat) and `re.gsub` | the output builder fails beyond 64 MiB, for string, table and function replacements; `$1` expansion is counted before it is appended |
| `re.split`, `gvim.text.split` | more than 1 000 000 pieces raises |
| `string.format` | result at most 1 MiB; width and precision at most 99; `%s` of a longer value raises |
| `gvim.text.pad` and `truncate` | width over 8192 raises `width too large` |
| `os.date`, `gvim.time.format` | a format over 1024 bytes raises |
| `json.decode` | input at most 64 MiB, at most 1 000 000 nodes and depth 200, checked while parsing |
| `json.encode` | output at most 64 MiB, the same node and depth caps, cycles raise |
| tables passed to the host | at most 1 000 000 nodes and depth 32; a cycle is an error |
| `gvim.text.fuzzy` | query at most 256 bytes (`too_large`); items clipped to 4096 bytes; the background job stops when the plugin is unloaded |
| `b:set_language`, `gvim.buf.create{language=}` | a name over 32 bytes is `invalid` (no lexer is that long) |
| integer table keys | a sparse key above 1 048 576 lives in the hash part (`t[2^26] = 1` costs nothing); a sparse key below it still pads the array with nils (`t[1000000] = 1` is about 16 MiB live, 80 MiB allocated and 35 ms in one VM instruction), so do not key a table by an absolute line number: key it by an offset from the first line you use, or by chunks of lines |
| `table.sort` | more than 500 000 elements raises; the default comparator polls the time budget every 4096 comparisons |
| `unpack`, `string.byte`, `select('#')` | at most 100 000 / 64 K results |
| buffer reads | 200 000 lines and 16 MiB per call |
| `b:edit` | 50 000 edits and 8 MiB inserted per call |
| `fs.read` | 16 MiB (option `max`); `fs.list` 100 000 entries; `fs.walk` 100 000 entries |
| `proc` | argv 1024 entries and 1 MiB, env 256 entries, output 8 MiB per stream, stdin queue 8 MiB |
| `s .. s` (concatenation) | **cannot be capped** (it is a VM instruction); the heap guard is the net |
| patterns (`string.*`) | 2 000 000 steps plus 8 per subject byte, pattern at most 256 bytes, subject at most 16 MiB |

### The regex work cap

RE2 (`gvim.re`) and the host's regex search (`b:find` with `regex = true`) cannot be interrupted, so they
are bounded up front by the size of the compiled program: a program over 2000 instructions is refused,
and a search of a subject of `n` bytes with a program of `m` instructions needs `n * m <= 40 000 000`
(about a quarter of a second). A pattern that is a plain literal is exempt, and only the 16 MiB subject
cap applies. The error is `limit: subject too large for this pattern; use b:find_lines/b:find or scan
line by line`. `b:find` has no such help from a subject limit: ask for a range with `within`, or use
`b:find_lines`, which scans whole lines in time-boxed chunks. `b:find_lines` and `b:find` bound CPU by
these caps and by time boxes, not by cancellation: a pathological regex over 16 MiB is refused up
front, not interrupted.

## Settings are untrusted input

`plugins.<name>.settings` comes from the user's config **and, for keys the plugin marked
`project = true`, from the project's `.gvim/config.json`**, which a repository can ship. The project
layer can also switch a plugin off (`enabled: false`); it can never switch one on, raise
`plugin_timeout_ms` (a larger project value is ignored with a diagnostic), or set a key that is not
`project = true` (dropped with one log line per key).

So: a setting value is data from a stranger. **Never** use one as a command line, as a path you delete or
overwrite, as a URL or as shell text without validating it, and declare `project = true` only for harmless
presentation keys (labels, depth, a delay, a colour). The bundled plugins do exactly that.

### The heap guard and a single concatenation

gvim runs the unmodified gopher-lua, and one `..` is a single VM instruction that nothing can interrupt:
the heap guard can cancel a slice only *between* instructions, so the peak is the guard's limit plus
what the instruction that crossed it allocated. `runtime/metrics` is read between instructions (every
millisecond, from the heap as it was when the slice began), and the limit is 128 MiB, so that the usual
overshoot stays near twice the guard. Measured peak resident size, one process per case, on the
default guard (128 MiB; the process itself is about 10 MiB; `plugin/membomb_unix_test.go`
repeats these and fails when one grows past its bound):

| Plugin code | Peak | Times the guard |
|---|---|---|
| `s = s .. s` from one byte, in a loop | 268 MiB (a 256 MiB guard: 525 MiB, a 64 MiB guard: 140 MiB) | 2.1 |
| the same from a 60 MiB string, from a 33 MiB string | 192 MiB, 243 MiB | 1.5, 1.9 |
| keeping a new 1 MiB string in a table, in a loop | 150 MiB | 1.2 |
| `s = s .. s .. s .. s` from 1 MiB (four operands) | 354 MiB | 2.8 |
| one `..` over 8 operands of 60 MiB (256 MiB guard) | 553 MiB | 2.2 |
| one `..` over **100** operands of 60 MiB (256 MiB guard) | **5.2 GiB** | 20 |
| one `..` over 20 operands of 16 MiB (default 128 MiB guard, measured by the review) | 416 MiB, about 57 MiB idle | 3.3 |

The rows marked "256 MiB guard" were measured with a 256 MiB guard; the others with the default 128 MiB.

The last two rows are the part that stays unbounded: a chain `a .. b .. c ...` is one instruction that
allocates every operand at once, so its peak is the number of operands (at most about 250, the register
limit of a Lua function) times the largest string the plugin holds (at most 64 MiB from the API, and
whatever earlier steps left under the guard). A plugin has to write such an expression on purpose with
huge operands; recovery is `--no-plugins`. Bounding it would take a rewrite of long chains into pairs at
compile time, or a patch of the VM. gvim does neither, and the residual is open (decision recorded in the
follow-up review, not in the code):

* **Left open: a long chain is not bounded.** Rewriting the syntax tree into left-nested pairs does not
  work with gopher-lua's compiler, which flattens right-nested concatenations and removes the instructions
  of the operand it compiled before it emits its own. A bytecode rewrite (split each multi-operand
  `OP_CONCAT` into pairwise steps that reuse the operand registers, then move the result) needs the jumps,
  `LOADBOOL` skips and line information relocated after the insertion, which is fragile across gopher-lua
  versions. Rejecting long chains at load time would refuse ordinary code, and a cap on the operand count
  still allows the cap times the operand size.
* A dozen short pieces copy about six times their length in one instruction, which is negligible; the
  cost is only for chains of large operands.

Every other single call that could make a large string or a large stack is capped by the runtime, by
gopher-lua's own options, or by the guard, and is measured above the same way:

| Call | Bound |
|---|---|
| `string.rep`, `string.format`, `table.concat`, `gsub`, `json.encode` | the result is at most 64 MiB (`MaxString`; `string.format` 1 MiB): `resulting string too large`, 71 MiB peak |
| `("x"):byte(1, -1)`, `unpack(t, 1, 1e8)`, `string.char(unpack(t))` | `string slice too long` / `too many results to unpack`, from the registry limit `RegistryMaxSize` of 1 048 576 slots (about 16 MiB), no growth |
| a table with a far integer key, `t[1e9] = 1` | `lua.MaxArrayIndex` is 1 048 576: keys above it live in the hash part, nothing is padded |
| recursion | `CallStackSize` 256 frames, 190 nested coroutine resumes |
| a table that keeps growing, strings kept in a table | the heap guard, between instructions: 150 MiB peak for 1 MiB elements |

## Residual risks

1. **`..` cannot be interrupted or capped.** See [above](#the-heap-guard-and-a-single-concatenation): a doubling string peaks at
   about twice the heap guard, and one concatenation of many large operands is the part that stays
   unbounded. Recovery is `--no-plugins`.
2. **The heap guard's forced collection costs CPU** on very large heaps (hundreds of MiB); it runs at
   most once per slice and only on a candidate.
3. **The heap guard measures the process**, not the plugin (above).
4. `pcall` is yield-safe, which costs one coroutine (about 3 µs and 5.6 KB) per call: avoid it in tight
   loops.
5. **Event cost grows with subscribers.** Delivering one hot event to ten subscribed plugins costs
   about 12 to 14 µs per handler call (0.4 to 0.6 ms per editor update when every hot event is
   subscribed by every plugin); `viewport_changed` makes the editor compute the pane layout twice per
   update while someone listens (about 17 µs). Prefer `debounce`, `opts.buf` and the narrowest event.
6. **Byte columns and offsets are over the LF-normalised text**: a CRLF document counts `\n` as one
   byte, unlike Fresh's raw offsets (`b:info().line_ending` tells which).
7. A long synchronous `Host` edit (up to 8 MiB) can take longer than a frame; use `b:set_text` once for
   a rewrite rather than many edits.
8. **Windows is only compile-checked**: the process-group kill and `os.OpenRoot` symlink behaviour are
   specified but were not run there.
9. **Deferral makes `command.run` of some actions asynchronous** (save, quit, macro replay, plugin
   commands): wait on `b:save()` instead of reading after `command.run("save")`.
10. **A replayed event can double-initialise** a plugin that also scans `gvim.buf.list()` at load: make
    initialisation idempotent.
11. `FindLines` and `FindRange` are bounded by work caps and time boxes, not cancellation.
12. Intentional divergences from Fresh: writes are synchronous (no `flush`); hunks come from Myers'
    diff, not patience, so gutter hunk boundaries can differ; `git_blame` and `merge_conflict` are
    reduced ports; there is no widget runtime, no orchestrator, no `render_line`. The Markdown
    Contents section is built into the TUI, not a plugin (`docs/sidebar.md`).

## Known limitations

Things that are *not* implemented or work differently from what you might expect. None is a bug the
plugin system will fix without a new stage.

* **No config reload.** gvim reads `config.json` at startup. A changed `plugins.<name>.settings` or
  `enabled` needs a restart (`Runtime.UpdateSettings` exists for a future reload but has no caller).
  Opening another project does not reload its project config either.
* **Key sequences work only inside modes.** The editor resolves single keys in the normal context, so
  `keymap.set("g g", ...)` without a `mode` is `invalid`, and a sequence in a command's `keys` is dropped
  with a warning notice when the keymap is rebuilt.
* **One editor input mode at a time.** Entering a mode ends the previous one. Pending-sequence state of a
  buffer mode is not reset by `b:set_mode`, and `mode_changed` is raised for the editor-wide mode only.
* **The panic chord shadows `ctrl+shift+6` while plugin input is active** (a plugin mode, key wait or
  overlay); every shipped preset binds it to *Set Bookmark 6*, which works as usual otherwise. Three
  quick `Esc` presses are the chord only while the input is stuck (a plugin mode that does not bind
  `Esc`, a key wait, a plugin prompt), so a mode that uses `Esc` itself, like vi_mode, is not switched
  off by Esc mashing.
* **`Style.strike` is not drawn** (the cell grid has no strike attribute).
* **Decorations do not survive undo** of the edit that removed their anchors, and are dropped on a
  whole-buffer replace; set them again on `buffer_changed`. Those of a closed buffer are freed at the next
  decoration call or on unload.
* **Picker details**: `p:query()` follows the user only for a live picker; a popup has no in-place update
  (`set_lines` closes and shows it again); a close issued from inside a `validate` callback can leave a
  dead overlay that swallows one key, so do not do it.
* **Virtual buffers**: a content write gives the buffer a new generation, so it resets `Buffer.Version`
  and drops decorations. Caches keyed only on the version (string-context highlighting, the occurrence
  tracker) can briefly show stale data after `b:set_content`. `gvim.has("virtual_buffers")` is
  unconditional.
* **Windows**: windows are laid out per call, so `w:valid()` and `w:visible()` cost O(panes x visible
  rows); the text range of a long line in a large file reports columns as the window offset plus bytes
  inside the window (an approximation). Opening a compare view creates new window ids.
* **Startup**: several plugins can each use a full load slice in one update, so loading is bounded per
  plugin, not by the 100 ms cap. A main chunk that waits forever (`gvim.async.suspend` without a
  timeout) holds back the plugins that `require` it and `plugins_loaded`: give such a wait a
  `timeout_ms`.
* **Plugin state is flushed within 500 ms**; a hard crash can lose the last half second of it.
* **`git_gutter` keeps change tracking on** for every buffer for as long as it is enabled (that is what
  live gutter marks need).
* **A plugin cannot read another plugin's handles**; `Subscription` and `Command` handles cannot be
  passed through `gvim.plugins` at all.
* **`.js` and `.ts` plugins are never run.** See [porting.md](porting.md).
* **Lua 5.1 miscompile**: `a, b = b, a` on two locals ([conventions.md](conventions.md#lua-51-notes)).
