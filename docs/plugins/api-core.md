# Core API

The `gvim` table itself, logging and notices, the plugin's own metadata, settings, editor
config, persistent state, directories, the environment and calls between plugins. See
[conventions.md](conventions.md) for coordinates, errors and `⏳`.

## gvim

```text
gvim.api_version        -- 2
gvim.version            -- the gvim version, or "dev" for a build from a work tree
gvim.platform           -- "darwin" | "linux" | "windows"
gvim.has(feature) -> boolean
gvim.err_code(err) -> string|nil
gvim.pos(line, col?) -> Pos
gvim.range(l1, c1, l2, c2) -> Range
gvim.pos_cmp(a, b) -> -1|0|1
gvim.range_eq(a, b) -> boolean
```

`gvim.has(feature)`: `"decor"`, `"windows"`, `"virtual_buffers"`, `"input"` are always
true in the shipped editor. `"lsp"` is true when a language server is configured (see
[`gvim.lsp`](api-system.md#gvimlsp)); `"git"` is true when a `git` binary is on the path (it is looked
up at every call, because the binary can appear or vanish while gvim runs). An unknown feature is
`false`.

## gvim.log and gvim.notify

```text
gvim.log.debug(fmt, ...)    gvim.log.info(fmt, ...)
gvim.log.warn(fmt, ...)     gvim.log.error(fmt, ...)
gvim.notify(msg, level?)    -- level "info" (default) | "warn" | "error"; returns true
gvim.status(msg)            -- the same as gvim.notify(msg, "info")
```

* `log.*` take `string.format` arguments when the first argument is a string, otherwise the
  arguments joined with spaces through `tostring`. `print(...)` logs at info level its arguments joined with tabs through `tostring`, so unlike
  `gvim.log.info("a", 1)` it never treats the first argument as a format. A line is cut at
  4 KiB. The log keeps the last 2000 entries (**Plugin: Show Log**).
* `warn` and `error` also go to the user's warnings list (**Show Warnings**). There is no status-bar counter
  for them: the `[E:N W:N I:N]` counter on the status row counts language server diagnostics only.
* `notify` shows the text on the status row (newlines become spaces, 512 bytes at most);
  `warn` and `error` also go to the warnings log.
* Formatting runs inside your slice, so a `__tostring` metamethod is budgeted like any Lua.

## gvim.plugin

```text
gvim.plugin.name        -- "hello"
gvim.plugin.dir         -- "" for single-file and buffer plugins
gvim.plugin.version     -- from the manifest
gvim.plugin.source      -- "bundled" | "user" | "cli" | "init" | "buffer"
gvim.plugin.on_unload(fn) -> true | nil, err
```

`on_unload` functions run when the plugin is unloaded or reloaded, in the order given. They must
not wait, get 200 ms in all, and are abandoned after that. At most 16 can be registered.
Everything the plugin registered is closed by the runtime anyway; use `on_unload` for what only you
know about (a state you keep). An `on_unload` function cannot touch files: every `gvim.fs` function
waits, so `gvim.fs.write` or `gvim.fs.remove` in it (or in a `shutdown` handler) is refused with
`async call inside a Go callback: on_unload must not yield` in the log and does nothing. To remove a
file you created, delete it when you are done with it, or on the next load.

## gvim.settings

```text
local settings = gvim.settings.define {
  labels = { type = "string", default = "asdf", description = "Jump labels" },
  depth  = { type = "integer", default = 3, minimum = 1, maximum = 6, project = true },
  style  = { type = "enum", values = { "dark", "light" }, default = "dark" },
  words  = { type = "string_list", default = { "TODO" } },
}
print(settings.depth)
gvim.settings.get() -> table     -- a plain copy of every value
```

`define` declares the keys of `plugins.<name>.settings` in the config. Types: `boolean`,
`integer`, `number`, `string`, `enum` (with `values`) and `string_list`; `minimum` and `maximum`
apply to numbers. The effective value is the default, overridden by the user's config, and
overridden again by the project's `.gvim/config.json` **only for keys declared `project = true`**.
A value of the wrong type or out of range keeps the one below it and logs a warning. Project
settings for keys that are not `project = true` are dropped with one warning each.

`settings` is a read-only live table: reading `settings.depth` always gives the current value. It
does not iterate with `pairs`; use `gvim.settings.get()` for a plain copy. At most 128 keys.

> Setting values are **untrusted input** (a repository can ship a project config). Never use one
> as a command line, as a path to delete, as a URL or as shell text without validating it. Declare
> `project = true` only for harmless presentation keys (labels, depth, a delay). See
> [limits.md](limits.md#settings-are-untrusted-input).

gvim reads the config when it starts, so a changed setting needs a restart.

## gvim.config

```text
gvim.config.get(key?) -> value | table
```

A read-only snapshot of some effective editor settings. With a key, its value (or `nil`); without,
a table of all of them. The keys are: `theme`, `tab_size`, `insert_spaces`, `line_ending`,
`trim_trailing_whitespace`, `ensure_final_newline`, `auto_indent`, `line_numbers`, `line_wrap`,
`highlight_current_line`, `show_whitespace`, `cursor_style`, `auto_save_enabled`,
`auto_save_interval_secs` (the names are those of the config file). Other keys are not readable. The values are what the editor
uses now: `line_wrap`, `highlight_current_line`, `show_whitespace` and `cursor_style` follow the view toggles,
and `auto_save_enabled` is `false` with `auto_save_interval_secs` `0` while auto-save is off.

## gvim.state

```text
gvim.state.get(key, default?) -> value
gvim.state.set(key, value) -> true | nil, err
gvim.state.delete(key) -> true
gvim.state.keys() -> string[]
```

A JSON key-value store per plugin. Values must be representable as JSON (strings,
numbers, booleans, tables of those); the whole store is capped at 1 MiB (`limit`).
Writes are visible to the next read at once and are flushed to disk in the background within
500 ms and when gvim exits; a crash can lose the last half second. State survives reloads, not
an uninstall.

**Scope.** A direct launch keeps the editor-wide store in `plugin-data/<name>/state.json`,
shared by every session of that gvim. A DAEMON workspace scopes it to the workspace: the
state is kept in `sessions/<stable-id>/plugin-state.json` in the workspace's session
directory, one versioned file (`{"version":1,"plugins":{name:{key:value}}}`) for
all of its plugins, restored when the workspace is materialized and flushed when it is closed
or the daemon exits. Switching workspaces therefore reads and writes that workspace's state;
two workspaces (co-tenants included) never see each other's keys. The file is bounded per
plugin (1 MiB) and in all (8 MiB; an over-bound save is skipped with a warning, never
truncated). A file written by a newer gvim is read-only: `set` returns an invalid
error and the file is never overwritten. Plugin enablement/settings and the plugin manager's
toggles stay editor-wide in the daemon too.

## gvim.dirs and gvim.env

```text
gvim.dirs.config()  gvim.dirs.data()  gvim.dirs.plugin()  gvim.dirs.root()
gvim.dirs.cwd()     gvim.dirs.home()  gvim.dirs.temp()      -- strings, no IO
gvim.env.get(name) -> string|nil

-- The project environment (config env.detectors; host-owned detection and gate).
gvim.env.detected() -> {name, kind, snippet}|nil
gvim.env.active() -> {name, kind, snippet}|nil
gvim.env.trust_level() -> "trusted"|"restricted"|"blocked"
gvim.env.set(snippet, opts?) -> true|nil, err   -- opts {dir?, kind?}; "path-only"|"shell"
gvim.env.clear() -> true|nil, err
```

`dirs.data()` is `<config>/plugin-data/<name>`; `dirs.plugin()` is the plugin's directory (empty
for single files); `dirs.root()` is the workspace root *now* (it changes when the user opens
another project). Plugins never change the environment; pass `env` to `gvim.proc.run` instead.

## gvim.plugins

```text
gvim.plugins.list() -> { {name, status, version, source}, ... }
gvim.plugins.export(tbl) -> true | nil, err
gvim.plugins.get(name) -> proxy | nil
```

Plugins call each other through exports.

```lua
-- plugin "git_gutter"
gvim.plugins.export {
  hunks = function(buf_id) return compute(buf_id) end,
}

-- another plugin
local gutter = gvim.plugins.get "git_gutter"
if gutter then
  local hunks, err = gutter.hunks(buf.id)
end
```

* `export` exposes the **functions** of the table; any other value must be plain data and is
  copied.
* Every call through a proxy is **⏳**. Arguments are deep-copied plain data (no functions, no
  userdata, so no `Buffer` or `Window` handle: pass an id), the function runs as a task in the
  *exporting* plugin's state, and the results are copied back. No Lua value is ever shared between
  two states. Depth is limited to 4.
* The proxy holds only the plugin's name; the exporter is looked up at every call. A plugin that is
  not loaded gives `nil, "not_found: plugin 'x' is not loaded"`; a reload of the exporter resolves
  calls in flight with `nil, "closed: plugin reloaded"`; an error inside the exported function gives
  `nil, "plugin 'x': <message>"`.
* `Subscription` and `Command` handles cannot cross.

## gvim.i18n

Plugins can ship their own translations in a `<name>.i18n.json` next to the plugin (the
editor finds `plugins/foo.lua` + `plugins/foo.i18n.json`, or `plugins/foo/foo.i18n.json` for a
directory plugin). The file is Fresh's plugin i18n format: a JSON object of locale → key →
text, with `%{name}` placeholders:

```json
{
  "en": { "cmd.do_thing": "My Plugin: Do Thing", "status.found": "Found %{count} items" },
  "bg": { "cmd.do_thing": "Моят плъгин: Направи" }
}
```

```text
gvim.i18n.t(key, vars?) -> string
gvim.i18n.translate(key, vars?) -> string    the same function
gvim.i18n.locale() -> string
```

`t(key)` returns the text of the editor's locale, then English, then the key itself, so a
missing translation reads as a key and an English fallback always exists. `vars` fills the
`%{name}` placeholders (`string`, number or boolean values; an unknown placeholder is left as
it is). `locale()` is the editor's resolved locale: the config `locale` (Settings →
Interface → locale, or the top-level `locale` key) when set, otherwise
`LC_ALL`/`LC_MESSAGES`/`LANG`, otherwise `"en"`; a live Settings change reaches the
plugin at its next call. The editor's own strings use the same resolution through the
TUI-free `i18n` package (see [i18n.md](../i18n.md)). A plugin ships its own
`.i18n.json`; the embedded editor catalogues are the editor's own translation layer and
are not injected into plugins, so `gvim.i18n.t` only sees the plugin's file (plus its
English entries) for the editor's locale.

A command `title` or `description` that starts with `%` is a translation key and is resolved
when the command is registered: `title = "%cmd.do_thing"` becomes `t("cmd.do_thing")`. Menu
labels accept the same prefix.


## Plugin declared settings (host view)

A plugin declares its settings with `gvim.settings.define` (see
[api-core.md](api-core.md#gvimsettings)). The runtime also exposes that schema, read-only, to
the host: `SettingsSchema(name)` returns one `PluginSettingSpec` per declared key (key,
description, type, default, enum choices, numeric min/max, project flag) and
`SettingsValues(name)` returns the effective `PluginSettingValue` of each key with the layer
that supplied it (a project override is marked; a user-config value wins over the default).
The Settings page lists these rows read-only (type, default and the effective value in the
note); editing still goes through `plugins.<name>.settings` in the config. Declaring a
setting in a plugin therefore makes it visible to the page with no extra host code.
