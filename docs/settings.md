# Settings

Status: design record of the editable Settings page (rounds 3 and 4).

gvim reads its settings from JSONC files with three layers: the built-in
defaults, the user file (`~/.config/gvim/config.json`, or `--config`) and the
project file (`.gvim/config.json` at or above the workspace root). The
**Settings** page (Edit ▸ Settings…, the palette command **Open Settings**) edits
the user or project layer, validates each value through the same option tables a
real config load uses, writes the file without rewriting it, and applies what it
can to the running editor. This file records the design: the schema, the layers,
the write path, the structured sub-pages, the live-versus-restart boundary, the
Reload command, per-setting reset, and what is deferred.

## 1. The schema

`config/settings_schema.go` holds the descriptor table the page is built from:
69 settings, each a `config.Setting` with

- `Path`: where the value lives in the file, such as `{"editor", "tab_size"}`;
- `Kind`: `bool`, `int`, `enum`, `string`, or `page` (a row that opens
  a structured sub-page and has no single text value);
- `Choices` (or `ChoicesFrom`: `themes`/`keymaps`, which the app fills
  from `theme.Names()` and the built-in keymap names, because `config` is a
  leaf package and must not import them);
- `Min`/`Max` for an int;
- `Layers`: `LayerUser` and/or `LayerProject` (the keymap, the terminal
  shell and its arguments, and `contents.fold_buffer` — the retired
  markdown_toc plugin's setting — are user-only, because a project must not
  change what keys do, what program gvim executes, or how the reader's outline
  behaves);
- `Apply`: `ApplyLive` or `ApplyRestart` (section 5);
- `Value(Config)` and `Encode(text)`: the current value as the page shows
  it, and the JSON to write for a value the page produced. A page row's
  `Value` is its summary ("2 commands"), and `Encode` refuses.

The sections are Interface, Display, Whitespace, Editing, Search, Explorer,
Contents, Session, Mouse, Languages, Formatters, Status Bar, File Explorer and
Terminal.
The scalar rows cover the settings gvim reads today; the structured rows open
the sub-pages of section 4. The app adds the non-scalar rows the config schema
cannot describe: the **Language Servers** page row (the LSP page: effective
built-in/user/universal servers with their live manager state, manual
start/stop/restart action rows, and each language's whole server array as
validated JSON) and the read-only **Environment** rows (effective shell and its
source, TERM/TERM_PROGRAM, the resolved icon set and why, OS/arch, the config
files in use and the trust state). Those rows are injected by
`internal/app/settingssvc.go`, never into `config.Settings()`, so the schema's
reset and validation paths still describe scalar config keys only.

Rows can be read-only or inherited: an `info` item is a fact that Enter/Del only
explain; a structure row marked *locked* says why the layer cannot change it
(Enter/Del show the reason), and a row marked *inherited* shows the value it
inherits while Enter writes an override and Del is a no-op. Action rows run
instead of editing: `ui:` closes the page and runs a TUI action (the Keybinding
Editor), `act:` calls `SettingsPort.Act` from a command (LSP start/stop/restart).
A list structure names its whole-array path so an empty list can still take its
first item, and a JSON field validates with `json.Valid` and writes the raw JSON
(not a quoted string). Structured pages that own arrays (on_save, an LSP server
list) edit the whole array as JSON, because `config.PatchBytes` addresses objects
and map keys, not array elements.

Three more app-injected pages follow the same rule (their rows are not scalar
config keys):

- **Languages · one entry** shows the 16 scalar overrides with the value the
  layer inherits when it does not set one (`(inherit: 8)`, note "inherit"):
  Enter writes an override, Del removes it and the row goes back to inheriting.
  After the scalars come the structured children — `extensions` and `filenames`
  (whole string lists), `formatter` (command, stdin, timeout_ms and an args
  list; the object is written whole), `on_save` (a JSON array page: actions are
  objects, so the whole array is edited and validated as JSON) and `indent`
  (the five pattern strings).
- **Plugins** lists the runtime inventory (`pluginapi.Runtime.Plugins`: name,
  source, status, enabled, locked, and the settings keys the user config sets,
  read-only). Enter on a plugin writes `plugins.<name>.enabled` to the user
  config and reloads it — with the previous file bytes restored if the write or
  reload fails — and the root then calls the runtime's `Control` from the update
  goroutine (the plugin runtime is single-goroutine; Act only does the file IO in
  its command). The project layer shows the same rows locked: plugin enablement
  is the user config's. **Reload plugins** calls `Control{Op:"reload_all"}`.
- **Keybindings** is a read-only effective list: the preset's bindings with the
  user's own entries marked, grouped by `keymap.ConsultedContexts` order and
  filterable; the plugin bindings the root's keymap layers are appended
  read-only by the TUI side (`settingsKeybindingRows`, note "plugin: <name>").
  There is no in-page capture: Enter on the action row closes Settings and opens
  the existing validated Keybinding Editor, which writes the keybindings file
  through the loader. That split is deliberate (docs §6).

`config/settings_structured.go` owns the structured knowledge: the 16 scalar
per-language override fields (`LanguageOverrideFields`: `tab_size`,
`use_tabs`, `auto_indent`, `auto_close`, `auto_surround`, `line_wrap`,
`wrap_column`, `page_view`, `page_width`, `show_whitespace_tabs`,
`format_on_save`, `comment_prefix`, `word_characters`, `grammar`,
`textmate_grammar`, `indentation_guide`, with their kinds and bounds), the
known language ids (`KnownLanguageIDs`), the status-bar element names
(`StatusBarElementNames`), the list of paths the page may reset
(`RemovablePath`: the schema keys plus `formatters.<key>`,
`languages.<id>[.<field>]`, the per-language structured values
`extensions`, `filenames`, `formatter[.<field>]`, `on_save` and
`indent[.<pattern>]`, and `terminal.shell.command|args`), the inherited display
of a per-language scalar (`LanguageInheritedValue`: the built-in language entry
applied over the global editor option, e.g. go `tab_size` 8), the page
summaries, and the `file_explorer.width` display/encode rules ("30%" for a
percentage, "24 cols" for a column count, a bare number 0-100 is a percentage as
the config file reads it). `DecodeLayer` decodes one layer file for the app's
structured reads.

`config.ValidateEdits(edits, project)` applies the edits to a fresh default
config through the existing option tables and returns the diagnostics, using the
same `applyObject` path a real config file goes through, so a value the editor
could not read is refused before it reaches the file and the project-layer rules
(the C11 rules: no keymap, no plugin enablement, no LSP command, no terminal
shell) still hold. A structured set edit (an entry of `formatters`, a
`languages` field, a status-bar list, `terminal.shell.args`) is validated the
same way; a reset needs no validation.

## 2. The layers

The page writes one layer at a time. The footer shows the target; **Tab**
switches it.

- **User** (the default): the user config file, `--config` when given, else
  `~/.config/gvim/config.json` (created on save).
- **Project**: the project config file, and only for a workspace that is
  **Trusted** (see `docs/trust.md`). The page writes the nearest existing
  `.gvim/config.json` at or above the workspace root; when there is none it
  writes `<root>/.gvim/config.json`. A row whose setting is user-only (the
  keymap, the terminal shell and its arguments) is shown as
  "`<name> (user layer)`" and cannot be edited there.

There is no session layer: gvim's runtime toggles are session state, but they are
not a config layer the page writes. There is no "most specific layer that
already defines the setting" auto-targeting either (Fresh's rule): the layer is
an explicit choice. The project layer keeps its trust gate from round 1: the
running editor reads the project layer only in a trusted workspace, so a write
trusted now is read at the next start, and one written while untrusted is
refused with a notice.

The structured sub-pages read the keys the chosen layer's file itself sets, not
the merged config, so the page shows what a Reset would remove. A status-bar list
the layer does not set starts from the effective list (the default or the lower
layer), and the first change writes both sides as whole arrays; nothing else
under `editor.status_bar` (the separator) is touched.

## 3. The write path

Saving is one patch of the chosen layer's file:

1. the page builds one `tui.SettingEdit` per change: a scalar row's key and
   display value, or a structured row's explicit config path and JSON value, or
   a reset (a path to remove);
2. `internal/app/settingssvc.go` validates every set: the schema's bounds and
   choices, the app-side checks a theme (a bundled theme) and a keymap (a
   built-in preset) need, then `config.ValidateEdits` through the option tables
   and the layer rules (a reset only needs `RemovablePath`, so it is
   idempotent);
3. `config.PatchBytes` changes the parsed JSONC tree and returns the new file.
   It sets or removes explicit paths; a reset deletes the key and cascades only
   objects this removal emptied (a parent that still has a member is never
   touched, and the root stays). Comments, layout, indentation, the
   trailing-comma style and every key the patch does not name are kept; an
   absent key on a reset is a no-op; a file that is not valid JSONC is refused
   and left alone;
4. `patchConfigFile` (in the app, because `config` is a leaf package and may
   not import `atomicfile`) replaces the file atomically, creating its
   directory and the file when missing; a failed write leaves the old file;
5. the service reloads the config with `config.Load` (same
   `Options.Untrusted` gate as the start) and returns what to apply.

A save that writes the file but fails to reload is reported as an error; the
file keeps the value, and the next start reads it. A reset's effective value then
comes from the lower layer, or gvim's built-in default when no layer sets it.

## 4. Reading the page

The page (`internal/tui/settings.go`) is a centred card in the style of
`docs/ui.md`: a title with the layer, a filter row, section headers and value
rows, and the hints on the bottom border. It reads plain rows from the
`tui.SettingsPort` (`internal/tui/ports.go`) and never sees the config
schema or a config type; `internal/app/settingssvc.go` implements the port over
`config.Load`, `config.PatchBytes`, `config.DecodeLayer` and the editor's
live setters.

Main page keys:

| Key | Action |
|---|---|
| type | filter the rows by name, key, section or value |
| Up / Down, PageUp / PageDown, Home / End | move the highlight |
| Enter | edit the highlighted row: a bool toggles in place, an enum opens its picker, an int or a string opens a one-line prompt (validated while it is typed), and a page row opens its structured sub-page |
| Del | reset the highlighted setting (see below); Del again undoes it |
| Tab | switch the layer, User ⇄ Project (Project only when trusted) |
| Ctrl+S | save the pending changes to the layer |
| Ctrl+R | discard all pending changes (the undo for the page) |
| Ctrl+E | close the page and open that layer's raw config file in a tab |
| Esc | close the page; with pending changes it asks first |

Changed rows carry a `*` marker and the title gets a ` *` too. Nothing is
written until Ctrl+S; a value the port refuses keeps the prompt open with the
reason, and a save the port refuses keeps the page dirty with a status-row
notice.

**Reset.** Del on a row marks it for reset in the chosen layer: the save removes
the key from that layer's file (with the empty-parent cascade of section 3), so
the effective value falls back to the lower layer or the built-in default. The
row shows `(default)` while the reset is pending; Del again drops it. This is
the "inherit" side of Fresh's Reset/Inherit pair: the page has one reset, "remove
from this layer", not a second "set the built-in default explicitly" button.
Ctrl+R discards every pending change instead (there is no per-change undo stack).

**Structured sub-pages.** A page row (Languages, Formatters, Status Bar left and
right, Terminal shell arguments, Language Servers, Plugins, Keybindings) opens a sub-page
(`internal/tui/settings_structure.go`) built from the port's
`SettingsStructure`. The sub-page edits the parent page's pending changes as
they are made; the main page's Ctrl+S writes them all, and Ctrl+S in a sub-page
closes it and saves. Esc goes back. The port reads the layer's file for the
sub-page from a `tea.Cmd`, with a generation guard, so no IO runs on the update
loop and a fast back-and-forth cannot show a stale page.

- **map** (Formatters): one row per entry, `key │ command`; Enter edits the
  command, Ctrl+N adds an entry (key, then command), Del resets the entry.
- **list** (Terminal shell arguments): one row per argument; Enter edits, Ctrl+N
  adds, Del removes, Alt+Up/Down reorders; the whole array is written.
- **fields** (one language's overrides, the formatter, the indent patterns): the
  scalar rows show the layer's value or the inherited one marked `(inherit: …)`
  with the "inherit" note; Enter edits by kind, Del resets the override/pattern.
  Rows that open a child page (the structured language values, the formatter's
  args) keep their own summary value and open that page on Enter.
- **languages**: one row per language entry with its override count; Enter opens
  that language's fields sub-page, Ctrl+N adds a language from the known ids,
  Del resets the whole entry.
- **entries** and **JSON** rows: an entries page (added for future object lists)
  opens a fields child per entry and writes the whole array; a JSON row (the LSP
  server arrays, `languages.<id>.on_save`) validates the text with `json.Valid`
  and writes it as raw JSON at the parent path.
- **bindings** (Keybindings): context headers and read-only binding rows (chord,
  action, source note), the typed filter, and the action row that opens the
  Keybinding Editor; rows are locked, so Enter explains instead of editing.
- **dual** (Status Bar): Left, Right and Available groups. Tab chooses the side a
  new element joins; Enter on a Left/Right entry moves it to the other side (the
  sibling cross-exclusion: an element is on one side at most), Enter on an
  Available element adds it to the chosen side, Del moves an entry off to
  Available, Alt+Up/Down reorders within the side. Both element lists are
  written as whole arrays and no other `editor.status_bar` key is touched.

The sub-pages have no filter row (they are short and ordered); the main page's
filter covers finding the section.

## 5. Live versus restart

The page marks each row with whether a saved value applies at once. On save the
service returns a `tui.SettingsChange`: the display options, the effective
settings, the icon resolution and two key lists. The root applies the `Live`
keys through the existing setters and shows one notice for the `Restart` keys:

`Restart gvim to apply: terminal.close_on_exit, terminal.shell.command`

The restart keys are read only while the editor starts: the editor's option
struct is built once, the session and recovery settings are read before the TUI
exists, the terminal dock is built from its options, and the terminal reports
its capabilities once. The live keys are pushed into the running editor by the
service and the root:

- the root calls `SettingsPort.Apply` on the update loop (the editor belongs to
  that goroutine; the Save command only reads and writes files), and the service
  re-registers the editor's language-settings resolver (`languageSettings(cfg)`),
  formatters, rulers and ephemeral patterns, so the editing options that flow
  through the per-language settings (tab size, use tabs, auto indent, auto
  close, auto surround, line wrap and the whitespace flags) and the
  `languages`/`formatters` sections take effect for every open document;
- the root applies the display options (tab bar, status bar, prompt line,
  scrollbars, line numbers, line wrap, current-line/occurrence/column
  highlights, whitespace, cursor style, indent guides, rainbow brackets, smooth
  scroll, mouse wheel/double-click/hover delays, search defaults, explorer
  hidden/ignored, menu-bar mnemonics, show menu bar), the status-bar element
  lists and separator, the file-explorer options (width, side, follow, auto-open,
  compact directories), the theme, the keymap and the icon set, rebuilding the
  styles and the menu bar as those need.

**Restart settings** in this slice: `editor.hot_exit`,
`editor.restore_previous_session`, `editor.auto_save_enabled` and its
interval, `editor.recovery_enabled`, `editor.virtual_space`,
`editor.scroll_offset`, the default line ending, trim-trailing-whitespace and
ensure-final-newline on save, `editor.auto_read_only`, `mouse_capture`, and
the whole `terminal` section (the dock is built at startup), and
`contents.fold_buffer` (the Contents section reads its settings when it
starts). The LSP, plugin and large-file settings are not in the page at all yet
(deferred), and they keep their own restart boundary when edited by hand.

**Command-line flags win over the file** for the running editor, as they already
do: setting the theme or the keymap while `--theme`/`--keymap` is given
writes the file but does not apply the value, and the notice says the flag wins.

**Live page actions.** The Language Servers page's start/stop/restart rows run
through `SettingsPort.Act` from a command, not from a Save: they start or stop
servers at once and then refresh the page, so the notice reports the outcome.
They are not config edits. The Plugins page splits differently: Act writes and
reloads the user config in its command (with rollback to the previous bytes on a
failed write or reload), and the root runs the runtime's `Control` from the
update goroutine when the Act result arrives, because the plugin runtime is
documented single-goroutine. The Environment rows are facts, not settings.

**Reload.** Save always reloads. The palette row **Reload Configuration**
(`reload_config`, native row in `commands/settings_commands.go`; no default
key) re-reads the config files from disk through the same path and applies the
live set, so a config file edited by hand takes effect without a restart. A
Reload compares the previous and the new config and reports only the settings
whose effective value changed: the live ones are applied, the changed restart
ones go into the restart notice, and no change at all says "Config reloaded: no
changes". The command does not write any file, and an open Settings page keeps
its unsaved changes over the new snapshot. `SettingsPort.Reload()` stays the port
method behind it.

## 6. What is deferred

- **R7-3 coverage is closed for the page surface:** the Language Servers page,
  the read-only Environment section, the per-language structured values with the
  reset-vs-inherit display, the Plugins page and the read-only Keybindings page
  all ship. The exact residuals are: the plugin runtime exposes no
  declared-settings schema/values API yet, so a plugin's settings are listed
  read-only (keys only) and edited in the raw config file; there is no plugin
  gallery/install page; keybinding editing deliberately stays in the existing
  Keybinding Editor (no in-page capture, no second writer of the keybindings
  file);
- project-environment detection (`env.detectors`: venv/direnv/mise activation)
  is not implemented; the Environment section says so;
- an LSP server's fields are edited as the whole language array in JSON, not as
  per-field dialogs (array elements cannot be patched one by one); the parsed
  servers are shown read-only beside it, and `process_limits` is accepted,
  shown, and not applied;
- Fresh's two-button per-field distinction between "reset to the built-in
  default" and "inherit/unset", and a full per-change undo stack (the page has
  one reset per setting and Ctrl+R to discard everything);
- a config Reload keybinding (the palette row is the only entry point);
- plugin settings editing and theme preview in the page;
- Fresh's generic widget runtime (dual-list drag and drop, map entry dialogs
  with nested fields).

The read-only Settings list remains as the fallback when the app wires no
`SettingsPort` (the TUI in a test, or a host without a config directory): it
lists the effective settings and opens the config file on Enter (the ui_icons row
opens the icon preview).

## 7. Files

| File | Role |
|---|---|
| `config/settings_schema.go` | the descriptor table, `Setting`, `Settings()`, `SettingByKey`, the section names |
| `config/settings_structured.go` | the structured knowledge: language override fields, language ids, status-bar element names, removable paths, page summaries, explorer width, `DecodeLayer` |
| `config/patch.go` | `Edit` (set or remove), `PatchBytes` (surgical hujson patch with the reset cascade), `ValidateEdits` |
| `config/config.go` | `applyObject`, the shared key-application path |
| `config/contents.go` | the native `contents.fold_buffer` key, its project-layer rule (ignored: the retired markdown_toc plugin's settings are the user's) and `ContentsFoldBuffer`, the effective value with the plugin-key fallback |
| `internal/tui/ports.go` | `SettingsPort`, `SettingsSnapshot`, `SettingsItem`, `SettingsStructure`, `SettingEdit`, `SettingsChange` |
| `internal/tui/settings.go` | the main page, reset, reload action, `applySettingsChange`, the settings messages |
| `internal/tui/settings_structure.go` | the generic structured sub-page (map, list, fields, languages, dual) |
| `internal/app/settingssvc.go` | the port implementation over the config files, the layer reader of the sub-pages, and the editor's live setters; `SettingsPort.Act` dispatch |
| `internal/app/settings_runtime.go` | the page's optional live parts (LSP port, plugin runtime, effective keymap, icon resolution), the injected Language Servers/Environment rows and the Environment facts |
| `internal/app/settings_lsp.go` | the Language Servers structures: effective servers from the built-in table + `lsp` + `universal_lsp`, live status, actions, whole-array JSON editing, project-layer locks |
| `internal/app/settings_languages.go` | the per-language page: inherited scalar rows and the extensions/filenames/formatter/on_save/indent child structures |
| `internal/app/settings_plugins.go` | the Plugins page: runtime inventory, user-layer enable/disable with file rollback, reload; declared settings read-only |
| `internal/app/settings_keybindings.go` | the read-only Keybindings list: effective preset+user bindings grouped by consulted context, and the editor action row |
| `internal/tui/settings_actions.go` | the settings page's deferred TUI-action message and the Settings-overlay close helper |
| `internal/app/run.go` | wires the port into the TUI |
| `commands/settings_commands.go` | the native **Reload Configuration** palette row |

## 8. Tests

`internal/tui/settings_plumbing_test.go` pins the new row/page machinery (info,
locked, inherited, action, filter, empty-list path, JSON validation);
`internal/app/settings_languages_test.go` pins the inherited rows and the
structured child pages; `internal/app/settings_plugins_test.go` pins the plugin
inventory, the user-layer toggle and both rollback paths;
`internal/app/settings_keybindings_test.go` pins the grouped effective list and the
editor action; `internal/tui/settings_bindings_test.go` and
`internal/tui/settings_plugins_test.go` pin the TUI side (bindings layout, plugin
rows, Control from Update). `internal/app/settings_lsp_test.go` pins the Language Servers page (effective
builtin/user/universal rows with live state, project-layer listing/locking and
the config validation that refuses a project program but accepts a mute, and the
start/restart/stop actions).

- `config/patch_test.go` and `config/patch_remove_test.go`: comments/layout/
  trailing-comma style kept, unknown keys kept, parents created, missing file
  created, invalid file untouched, pointer escaping, no-op patches, resets
  (sibling kept, empty-parent cascade, absent-key no-op, mixed set+remove in one
  patch, trailing-comma style), validation through the option tables (bounds,
  enums, null, project C11 rules, the structured sections), `RemovablePath`,
  and schema coherence (every scalar's current value encodes and validates; the
  page rows, helpers and layers are declared).
- `internal/app/settingssvc_test.go`: the snapshot, the trust gate on the
  project layer, a user-layer save with comments preserved and the editor's
  language settings applied live, the restart boundary, a trusted project-layer
  save and reload, a refused value leaving the file alone, Reload reading a file
  changed on disk and reporting only the changed keys, the no-change Reload, the
  structured sub-pages (formatters, languages and one language's fields, the
  status-bar dual list, the shell arguments), a structured save patching map,
  language and list entries at once, the dual list leaving the separator alone,
  the unknown/wrong structured write guard, and the trust-gated project
  structure.
- `internal/tui/settings_page_test.go` and
  `internal/tui/settings_structure_test.go`: opening the page and its sections,
  toggling and saving a bool through a fake port, the restart notice, filtering
  and discard-all, the layer switch (trusted and untrusted), the prompt
  validation, the discard confirmation, live value application, Edit raw, the
  map edit/add/reset/save flow, the dual list moves and both-side writes, the
  languages list and fields pages, the list edits/reorders, per-setting reset and
  its undo, the stale-structure guard, and the Reload command's notices.