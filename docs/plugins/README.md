# gvim plugins

gvim runs plugins written in **Lua 5.1** (on [gopher-lua](https://github.com/yuin/gopher-lua),
pure Go). A plugin can add commands to the palette, bind keys, react to editor events, edit
buffers, draw decorations, open prompts and pickers, run processes and talk to language
servers. gvim ships a few plugins of its own (see [bundled.md](bundled.md)), and you write the
rest in `~/.config/gvim/plugins/` or in a single `init.lua`.

The earlier JavaScript plugin system is gone. `.js` and `.ts` files in the plugin folder are
never run: gvim shows one notice that lists them and a `legacy` row in the plugin manager.
The status row holds one line: when plugins also fail to load at startup, the notices are joined
(errors first) and cut at the width of the row. Each one is in **Show Warnings**, and the plugin
log (**Plugin: Show Log**) has the full text.
[porting.md](porting.md) maps the old API and Fresh's TypeScript API to the new one.

| Document | What is in it |
|---|---|
| this file | quick start, where plugins live, configuration, the manager, `init.lua`, command line |
| [conventions.md](conventions.md) | coordinates (the bases table), errors, `⏳` calls and tasks, handles, Lua 5.1 notes |
| [api-core.md](api-core.md) | `gvim` root, `log`, `plugin`, `settings`, `config`, `state`, `dirs`, `env`, `plugins` |
| [api-buffers.md](api-buffers.md) | `gvim.buf`, the `Buffer` handle, virtual buffers, `gvim.win` |
| [api-commands.md](api-commands.md) | `gvim.command`, `keymap`, `mode`, `context`, `input` |
| [events.md](events.md) | `gvim.on` and every event with its payload |
| [api-ui.md](api-ui.md) | prompts, pickers, popups, the status bar, the clipboard, decorations |
| [api-system.md](api-system.md) | `path`, `fs`, `proc`, `timer`, `async`, `workspace`, `git`, `lsp`, `diff` |
| [api-data.md](api-data.md) | the standard library as gvim leaves it, `json`, `re`, `text`, `utf8`, `bit`, `time`, `theme` |
| [limits.md](limits.md) | sandbox, time budgets, quotas, the cap table, `re` work cap, untrusted settings, risks, known limitations |
| [bundled.md](bundled.md) | the plugins that ship with gvim, their commands, settings and memory cost |
| [porting.md](porting.md) | Fresh to gvim and old gvim JS API to Lua |
| [gvim.d.lua](gvim.d.lua) | type stubs for editor completion (checked against the runtime by a test) |
| [examples/](examples/) | four complete plugins, loaded and run by the tests |

## Quick start

Save this as `~/.config/gvim/plugins/hello.lua`. The folder is `plugins` inside gvim's config
directory: `$XDG_CONFIG_HOME/gvim` when that is set, `~/.config/gvim` otherwise (macOS included),
and the user config directory of the OS on Windows:

```lua
local gvim = require "gvim"

gvim.command.register {
  name = "hello",
  title = "Hello: Say Hello",
  run = function(ctx)
    local buf = gvim.buf.current()
    gvim.notify("Hello from " .. (buf and buf:name() or "nobody"))
  end,
}
```

Start gvim, open the command palette and run **Hello: Say Hello**. The status row shows the
greeting. While you develop, `gvim --plugin ./hello.lua` loads a file without installing it,
and **Plugin: Reload...** (palette) reads the file again after you edit it. **Plugin: Show
Log** opens everything the plugin printed, with the line of every error.

Everything the editor offers is in the `gvim` table, which you get with
`local gvim = require "gvim"`. There are no other injected globals beyond the standard library
(see [api-data.md](api-data.md)) and `json`, `re`, `utf8` and `bit`.

## API level

The runtime reports `gvim.api_version`; this gvim is **level 2**. A plugin manifest may ask for a minimum level with `"api": 2`. Level 2 adds the read-mostly plugin host surfaces: declarative widget panels and sidebar sections (`gvim.ui.mount_widget_panel` and friends), `gvim.i18n`, `gvim.menu` row hooks, `gvim.ui.action_popup`, prompt suggestions (`gvim.ui.set_prompt_suggestions`), the all-files diagnostics `gvim.lsp.all_diagnostics`, the `lsp_status_clicked`/`lsp_server_error` and `widget_event` events, and the interactive widget kinds and host-owned instance state (`text` fields, dropdown, number,
radio, dual list, labeledSection, wrappers, page mode, tree drag), the theme-file APIs
(`gvim.theme.all/data/save_file/delete_file/reload/schema`), `gvim.buf.text_properties_at_cursor`, and the
`gvim.theme.override`/`gvim.theme.clear` session overrides. Everything of level 1 keeps working.

## Where plugins live

| Source | Location | Notes |
|---|---|---|
| `bundled` | inside the gvim binary | alphabetical; the manifest's `default_enabled` decides whether one starts |
| `user` | `~/.config/gvim/plugins/` | `name.lua`, or a directory `name/` with an entry file; alphabetical |
| `cli` | each `--plugin PATH` | a `.lua` file or a plugin directory; always enabled; for development |
| `init` | `~/.config/gvim/init.lua` | the plugin named `init`; loaded last |
| `buffer` | **Load Plugin from Buffer** | the current buffer is loaded as a plugin; not remembered |

A name collision is resolved as `cli` over `user` over `bundled`. When a file and a directory
of the same name are both in the user folder, the directory wins and a notice says so.
Plugins load in dependency order (`requires`, `after`), then by source, then by name, and
`init` is always last.

```text
~/.config/gvim/
  init.lua                    your own script, the plugin "init"
  plugins/
    hello.lua                 a single-file plugin, named "hello"
    git_blame/                a directory plugin: the directory name is the plugin name
      plugin.json             optional manifest (JSON with comments)
      init.lua                the entry file ("main" in the manifest changes it)
      lua/blame/util.lua      require "blame.util" finds it (<dir>/lua first, then <dir>)
  plugin-data/<name>/state.json    what gvim.state keeps for the plugin
  plugin-state.json           the manager's enable/disable switches
```

Plugin names match `^[a-z][a-z0-9_]{0,39}$`; a file that does not is skipped with a notice. A
plugin may have at most 64 files, 5 MiB in all, 256 KiB per `.lua` file and 1 MiB for `plugin.json`, and its files are read
through `os.OpenRoot`, so a symlink cannot lead out of the plugin directory.

### The manifest `plugin.json`

All fields are optional; unknown fields are ignored.

```jsonc
{
  "name": "git_gutter",            // default: the directory or file name
  "version": "1.0.0",              // shown by the manager, and gvim.plugin.version
  "description": "Git change markers in the gutter",
  "api": 1,                        // the lowest plugin API level it needs; a newer one fails with a notice
  "main": "init.lua",              // the entry file; it must stay inside the plugin directory
  "default_enabled": true,         // bundled plugins only: false means "off until the user turns it on"
  "requires": ["lib_x"],           // hard dependency: missing, disabled or failed means this one does not load
  "after": ["flash"]               // ordering only
}
```

A plugin that `requires` another starts after the other's main chunk has *finished*. `require`
inside a plugin loads, in this order, host modules (`gvim`, `gvim.<area>`, `gvim.git`; they
cannot be replaced), then the plugin's own files (`lua/a/b.lua`, `lua/a/b/init.lua`, `a/b.lua`,
`a/b/init.lua`). There is no `package.path` and no `require` of another plugin: use
[`gvim.plugins`](api-core.md#gvimplugins).

## Configuration

Plugins are configured in `config.json` (the same file as every other gvim setting):

```jsonc
{
  "plugins": {
    "git_blame": { "enabled": false },
    "flash": { "settings": { "labels": "asdfghjkl" } },
    "vi_mode": { "enabled": true }
  },
  "plugin_timeout_ms": 500
}
```

* **Enabled or not**, first match wins: `plugins.<name>.enabled` in the config; then the manager's
  `plugin-state.json`; then the manifest's `default_enabled` (bundled plugins); otherwise on.
  The config always beats the manager: a plugin switched in the config shows `Locked: set in
  config` and the manager refuses to toggle it.
* **Settings** are `plugins.<name>.settings.<key>`; a plugin declares its keys with
  [`gvim.settings.define`](api-core.md#gvimsettings). A value of the wrong type is ignored with a
  warning in the log. Settings values are **untrusted input**, see [limits.md](limits.md#settings-are-untrusted-input).
* **Project config** (`.gvim/config.json`) may switch a plugin *off* and may set a setting only for
  keys that the plugin marked `project = true`. It can never turn a plugin on: opening a
  repository never runs code from it. Project plugin folders do not exist.
* `plugin_timeout_ms` (default 500, 1 to 60000) is the time a command may run (see
  [limits.md](limits.md#time-budgets)). A project config can only lower it.
* Settings are read when gvim starts. gvim has no config reload yet, so editing the config needs
  a restart; **Plugin: Reload** re-reads a plugin's source, not the config.

## The plugin manager and commands

| Palette command | What it does |
|---|---|
| **Plugin: Manager** | lists every plugin with its status (`loaded`, `disabled`, `failed`, `suspended`, `legacy`); Enter opens the actions of a row: enable, disable, reload, open source |
| **Plugin: Reload...** / **Plugin: Reload All** | read the source again and reload; a syntax error keeps the old version running |
| **Plugin: Show Log** | the plugin log (2000 entries) in a read-only buffer, newest last |
| **Load Plugin from Buffer** | loads the current buffer as a plugin named after the file (default `buffer`) |
| **init: Edit init.lua** | opens `init.lua`, creating it from a template when it does not exist |
| **init: Check init.lua** | a syntax check of `init.lua` that reports the first error line |
| **init: Reload init.lua** | reloads the plugin `init` |
| **Macro: Save to init.lua** | appends the recorded macro to `init.lua` as a command that replays its steps (one undo step). It does **not** reload `init.lua`: run **init: Reload init.lua**. If `init.lua` is open in a tab with unsaved changes, the append goes to disk under it and the tab is stale |
| **Macro: Promote to command** | the same, as an editable function of `gvim.command.run` calls |

Reload keeps `gvim.state`, runs `on_unload`, closes everything the plugin registered (commands,
bindings, modes, status items, decorations, overlays, timers, watchers, processes, windows it
split) and loads the new source. Plugins that `requires` it reload after it.

## Command line

```text
gvim --no-plugins          do not discover or load any plugin, not even init.lua
gvim --plugin PATH         load the .lua file or plugin directory at PATH; repeatable
gvim --log-file PATH       append the status-row notices and the plugin log to PATH
```

The plugin log in memory holds the last 2000 entries (**Plugin: Show Log**) and is gone when gvim
exits; `--log-file` keeps it (below).

`--plugin` still loads when combined with `--no-plugins`. `--no-plugins` is the way out when a
plugin makes the editor unusable. Pressing `ctrl+^` or `ctrl+alt+shift+esc` at any time, or
`ctrl+shift+6` while a plugin holds the keyboard, or `Esc` three times within 600 ms while the plugin
input is *stuck*, clears every plugin input mode and key capture and closes plugin prompts: the *panic
chords*. The input is stuck when each of the three presses finds a plugin waiting for a key
(`gvim.input.next_key`), a plugin prompt, picker or popup open, or an active mode that does not bind
`Esc` itself. A mode that binds `Esc` (vi_mode does) answers every press, so Esc mashing never counts.

`--log-file PATH` appends the notices of the status row (what **Show Warnings** lists) and the plugin
log (what **Plugin: Show Log** shows, every level) to the file, one line each with a time stamp, and
writes them as they happen: the file is current while gvim runs and has what a `shutdown` handler or a
crash left. The file is opened for appending and created when missing. A path that cannot be written
is a notice and gvim runs without a log.

## What a plugin is allowed to do

A plugin is the user's own code and runs with the user's file and process rights, exactly like a
Fresh plugin. The sandbox protects the editor from *bugs* (loops, memory, runaway events),
not from a hostile plugin: do not install a plugin you would not run as a shell script. The
details are in [limits.md](limits.md).
