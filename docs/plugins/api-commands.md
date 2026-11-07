# Commands, keys, modes and key input

## gvim.command

```text
gvim.command.register(spec) -> Command | nil, err
gvim.command.unregister(name) -> true | nil, err
gvim.command.list(filter?) -> { {id, title, plugin, builtin}, ... }
gvim.command.run(action, args?) -> true | nil, err
gvim.command.run_all(steps) -> true | nil, err
gvim.command.actions() -> string[]
gvim.command.palette(query?) -> true
cmd:unregister() -> true | nil, err
```

### register

```lua
gvim.command.register {
  name = "toggle",                       -- [a-z][a-z0-9_]*, unique in the plugin; the id is "<plugin>.<name>"
  title = "Git: Toggle Blame",           -- the palette text (default: name)
  description = "...",                   -- palette detail
  when = { "has_buffer", "!read_only" }, -- all must hold; "!" negates
  keys = { "ctrl+alt+b" },               -- sugar for gvim.keymap.set(key, id)
  record = true,                         -- may a macro record it (default true)
  hidden = false,                        -- keep it out of the palette
  run = function(ctx) end,
}
```

* `run` receives `ctx = { args = table, count = 1, buf = Buffer | nil, source = ... }`. `source` is
  `"palette"`, `"key"`, `"menu"`, `"api"` (another plugin or `command.run`) or `"mode"`. `count`
  is the repeat count given by `command.run(..., {count = 3})`.
* `register` **raises** when the plugin already has a command of that name (`unregister` it
  first), and when a field has the wrong type. An invalid name, a bad `when` or a bad chord is
  `nil, "invalid: ..."`. At most 512 commands per plugin.
* The action id `<plugin>.<name>` is valid in the `keybindings` of `config.json` (an object
  `{"bindings": [{"key", "modifiers", "action", "when"}]}`, not a map of chords; see
  [bundled.md](bundled.md#bundled-plugins) for an example), in macros and in
  `gvim.command.run`. Built-in action names have no dot, so ids never collide.
* A command starts as a task and may wait. It has the command budget, `plugin_timeout_ms`
  ([limits.md](limits.md#time-budgets)).
* **`when`** is evaluated by the editor each time: the palette hides the row; a key press whose
  `when` fails shows `"<title> is not available now"` and does nothing. Names are everything the
  menus know (`has_buffer`, `has_selection`, `can_save`, `modified`, `read_only`, `large`,
  `file_explorer`, `file_explorer_focused`, ...), `language:<id>` (for example `language:go`), and
  the contexts your plugins set with [`gvim.context`](#gvimcontext).
* A macro that records a plugin command records the command id once, not what the command does
  through `command.run` (no double recording).

### unregister, list, palette

`unregister(name)` returns `nil, "not_found: ..."` for an unknown name, and removes the key bindings
that ran the command. `list` returns the built-in commands first (title = id) and then plugin
commands; hidden commands are not listed. `filter` is a query string or a table
`{ query = , plugin = , builtin = }`. `command.palette(query)` opens the palette with the query typed.

### run and run_all

```lua
gvim.command.run("move_down")
gvim.command.run("delete_line", { count = 3 })
gvim.command.run("insert_char", { text = "x" })
gvim.command.run("hello.hello")        -- another plugin's command: a new task, not awaited

gvim.command.run_all {
  "move_down",
  { "insert_char", { text = "x" } },                 -- {action, args}
  { action = "delete_line", count = 3 },             -- or a table with fields
}
```

`run` accepts any editor action (see `gvim.command.actions()`, about 270 editor and ui actions), any ui action,
or a plugin command id. `args.count` repeats it. An editor or ui action's own error comes back as
`nil, err` (`read_only`, `unsupported: action "x" is not supported`, ...). `run_all` makes all its
steps **one undo group** and stops at the first error.

* They act on the **active document** only.
* Some actions are **deferred** until your slice has ended (save, quit, macro replay, plugin
  management, plugin commands): see [conventions.md](conventions.md#tasks-and-the--calls).
* Repeat counts are capped, because the editor cannot be interrupted while it repeats:
  10 000 per action, 100 000 in all and 10 000 steps for `run_all`; above that is `limit`.
* A call also has a time budget of 250 ms in the editor, read between repetitions and between
  steps. Past it the call stops with `timeout` and says how far it got; what already ran is not
  undone. The first repetition always runs.

## gvim.keymap

```text
gvim.keymap.set(chord, action_or_fn, opts?) -> Binding | nil, err
gvim.keymap.del(chord, opts?) -> true | nil, err
gvim.keymap.label(action) -> string | nil
binding:active() -> boolean
binding:del() -> true | nil, err
```

```lua
gvim.keymap.set("ctrl+alt+h", "hello.hello", { desc = "Say hello" })
gvim.keymap.set("alt+j", function(ctx) gvim.command.run("move_down", { count = 5 }) end)
gvim.keymap.set("ctrl+shift+f", "hello.hello", { when = { "has_buffer" } })
```

* **Chords** use gvim's key names: `ctrl+shift+f`, `alt+f`, `enter`, `F10`, `ctrl+alt+b`.
  Spell an upper-case letter as `shift+a` (chords are case-folded). An invalid chord is
  `nil, "invalid: ..."`. Chords longer than four steps are refused.
* **Sequences** (`"g g"`, space-separated) work only inside a [mode](#gvimmode): the editor
  resolves single keys in the normal context. `keymap.set` with a sequence and no `mode` is
  `invalid`.
* `opts`: `mode` (nil: the normal editing context; any other name: a plugin mode), `desc` (palette
  title for a function), `args` (a table passed as `ctx.args`), `when` (names as for commands;
  normal context only).
* A **function** as the action becomes a hidden command `<plugin>.__fn<N>`; the editor only ever
  sees action ids.
* **Layering**: the preset keymap, then plugin bindings, then the user's `keybindings` in the
  config. The user always wins. A later binding of the same chord by the same plugin replaces an
  earlier one. At most 4096 bindings per plugin.
* The key resolution order in the editor is: panic chords, a pending `next_key` wait, the editor
  input mode, the buffer mode, then plugin bindings and the keymap.
* **The panic chords** (`ctrl+^` and `ctrl+alt+shift+esc` always; `ctrl+shift+6`, which an enhanced
  keyboard reports for `ctrl+^`, and three quick `Esc` only while plugin input is active) are handled
  first and cannot be rebound. Every shipped preset binds `ctrl+shift+6` to *Set Bookmark 6* (Fresh
  parity), so that key clears plugin input while a plugin mode, key wait or overlay is active and sets
  bookmark 6 otherwise.
* `keymap.label(action)` returns the formatted primary binding of an action (`"Ctrl+S"`), or `nil`.
* A command's `keys = {...}` is sugar for `keymap.set`. Key bindings with a `mode` never enter the
  keymap: they live in the mode table.

## gvim.mode

A **mode** captures keys before the keymap sees them: modal editing, or a transient key map.

```text
gvim.mode.define(spec) -> Mode | nil, err
gvim.mode.enter(name) -> true | nil, err
gvim.mode.leave() -> true
gvim.mode.current() -> name, plugin | nil
mode:name() -> string
mode:enter() -> true | nil, err
```

```lua
gvim.mode.define {
  name = "resolve",
  status = "-- RESOLVE --",                  -- shown first in the status bar while the mode is on
  bindings = {
    n = function() goto_next() end,
    ["u"] = "my_plugin.use_ours",            -- an action name works too
    ["g g"] = function() gvim.command.run("goto_start") end,   -- sequences are fine here
    q = function() gvim.mode.leave() end,
  },
  text = "swallow",                          -- what unbound printable keys do
  paste = "swallow",
  fallthrough = true,                        -- unbound non-text keys reach the keymap
}
gvim.mode.enter("resolve")
```

* `text`: `"pass"` (default; the key types as usual), `"swallow"` (dropped), or a function that
  receives `ctx.args.text` (what vi uses to read counts and characters).
* `paste`: `"pass"` (default), `"swallow"`, or `"text"` (the `text` function runs with
  `ctx.args = {text =, paste = true}`).
* `fallthrough` (default true): a key the mode does not bind and that types no text goes on to the
  normal keymap, so `ctrl+s` still saves in vi's normal mode.
* A **sequence prefix** that is also a binding waits up to one second for the next key, then runs
  the shorter binding; a key that does not continue the sequence runs the shorter binding first and
  is then handled again.
* The editor has **one** input mode at a time: entering a mode ends the previous one, whoever owned
  it. Modes are scoped to their plugin (two plugins may use the same name). `gvim.mode.leave()`
  only ends a mode the calling plugin owns. `gvim.mode.current()` asks the editor, so it is right
  even after the panic chord cleared the mode. A suspended plugin cannot enter a mode.
* Raises on a missing `name`; `mode.define` of an existing name is `conflict`.
* A mode `status` label is cut at the status-cell limit and always comes first among the plugin's
  status items. `mode_changed` is raised on every change ([events.md](events.md)).
* A buffer can have a **buffer-local mode** with `b:set_mode(name)` (virtual buffers); it is found by
  the bare name.

## gvim.context

```text
gvim.context.set(name, on) -> true | nil, err
gvim.context.get(name) -> boolean
```

A **context** is a named flag the `when` clauses of commands and bindings can test. It is
plugin-scoped and cleared when the plugin unloads. Names starting with `mode:` belong to the editor
and cannot be set.

## gvim.input

```text
gvim.input.next_key(opts?) ⏳ -> Key | nil, err
```

Consumes the next key press before the keymap sees it. `opts.timeout_ms` is 10 000 by default,
at most 60 000. Esc is delivered like any key. The error is `timeout` or `cancelled` (the panic
chord, an unload). Only one key wait is pending at a time (`busy`).

```lua
-- read a count and a motion, vi style
local key = gvim.input.next_key { timeout_ms = 3000 }
if key and key.text then
  gvim.notify("you typed " .. key.text)
end
```
