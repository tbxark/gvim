# Settings

Status: design record of the editable Settings page (round 3).

gvim reads its settings from JSONC files with three layers: the built-in
defaults, the user file (`~/.config/gvim/config.json`, or `--config`) and the
project file (`.gvim/config.json` at or above the workspace root). The
**Settings** page (Edit ▸ Settings…, the palette command **Open Settings**) edits
the scalar settings of the user or project layer, validates each value through
the same option tables a real config load uses, writes the file without
rewriting it, and applies what it can to the running editor. This file records
the design: the schema, the layers, the write path, the live-versus-restart
boundary, and what is deferred.

## 1. The schema

`config/settings_schema.go` holds the descriptor table the page is built from:
53 scalar settings, each a `config.Setting` with

- `Path`: where the value lives in the file, such as `{"editor", "tab_size"}`;
- `Kind`: `bool`, `int`, `enum` or `string`;
- `Choices` (or `ChoicesFrom`: `themes`/`keymaps`, which the app fills
  from `theme.Names()` and the built-in keymap names, because `config` is a
  leaf package and must not import them);
- `Min`/`Max` for an int;
- `Layers`: `LayerUser` and/or `LayerProject` (the keymap is user-only,
  because the project layer cannot change what keys do);
- `Apply`: `ApplyLive` or `ApplyRestart` (section 5);
- `Value(Config)` and `Encode(text)`: the current value as the page shows
  it, and the JSON to write for a value the page produced.

The settings are grouped in sections: Interface, Display, Whitespace, Editing,
Search, Explorer, Session and Mouse. The table covers the scalar settings gvim
reads today; the sections with structured values (languages, lsp, formatters,
plugins, status_bar elements, keybindings) are not in the table yet.

`config.ValidateEdits(edits, project)` applies the edits to a fresh default
config through the existing option tables and returns the diagnostics, using the
same `applyObject` path a real config file goes through, so a value the editor
could not read is refused before it reaches the file and the project-layer rules
(the C11 rules: no keymap, no plugin enablement, no LSP command) still hold.

## 2. The layers

The page writes one layer at a time. The footer shows the target; **Tab**
switches it.

- **User** (the default): the user config file, `--config` when given, else
  `~/.config/gvim/config.json` (created on save).
- **Project**: the project config file, and only for a workspace that is
  **Trusted** (see `docs/trust.md`). The page writes the nearest existing
  `.gvim/config.json` at or above the workspace root; when there is none it
  writes `<root>/.gvim/config.json`. A project row whose setting is user-only
  (the keymap) is shown as "`<name> (user layer)`" and cannot be edited there.

There is no session layer: gvim's runtime toggles are session state, but they are
not a config layer the page writes. There is no "most specific layer that
already defines the setting" auto-targeting either (Fresh's rule): the layer is
an explicit choice. The project layer keeps its trust gate from round 1: the
running editor reads the project layer only in a trusted workspace, so a write
trusted now is read at the next start, and one written while untrusted is
refused with a notice.

## 3. The write path

Saving is one patch of the chosen layer's file:

1. the page builds one `SettingEdit` per changed row (the value in the page's
   display form);
2. `internal/app/settingssvc.go` validates every edit: the schema's bounds and
   choices, the app-side checks a theme (a bundled theme) and a keymap (a
   built-in preset) need, then `config.ValidateEdits` through the option
   tables and the layer rules;
3. `config.PatchBytes` changes the parsed JSONC tree and returns the new file:
   it edits the hujson tree directly instead of unmarshalling and re-marshalling,
   so comments, the layout and indentation, the trailing-comma style, and every
   key the patch does not name are kept; an absent parent object is created; a
   file that is not valid JSONC is refused and left alone. The service
   (`patchConfigFile`) replaces the file at once with `atomicfile`, so a
   failed write leaves the old one. The split is the import rule: `config` is a
   leaf package and may not import `atomicfile`; it owns the schema and the
   patch, the app owns the file write;
4. the service reloads the config with `config.Load` (same `Options.Untrusted`
   gate as the start), which is the value the page and the running editor see as
   `"saved"`.

A save that writes the file but fails to reload is reported as an error; the
file keeps the value, and the next start reads it.

## 4. Reading the page

The page (`internal/tui/settings.go`) is a centred card in the style of
`docs/ui.md`: a title with the layer, a filter row, section headers and value
rows, and the hints on the bottom border. It reads plain rows from the
`tui.SettingsPort` (`internal/tui/ports.go`) and never sees the config
schema or a config type; `internal/app/settingssvc.go` implements the port over
`config.Load`, `config.PatchFile` and the editor's live setters.

Keys:

| Key | Action |
|---|---|
| type | filter the rows by name, key, section or value |
| Up / Down, PageUp / PageDown, Home / End | move the highlight |
| Enter | edit the highlighted row: a bool toggles in place, an enum opens its picker, an int or a string opens a one-line prompt (validated while it is typed) |
| Tab | switch the layer, User ⇄ Project (Project only when trusted) |
| Ctrl+S | save the pending values to the layer |
| Ctrl+R | reset the pending values |
| Ctrl+E | close the page and open that layer's raw config file in a tab |
| Esc | close the page; with pending values it asks first |

Changed rows carry a `*` marker and the title gets a ` *` too. Nothing is
written until Ctrl+S; a value the port refuses keeps the prompt open with the
reason, and a save the port refuses keeps the page dirty with a status-row
notice.

**Edit raw** is the escape hatch to the old behavior: it opens the chosen
layer's config file as a tab, for the settings the page does not edit (languages,
LSP, formatters, plugins, keybindings, status-bar element lists, …).

## 5. Live versus restart

The page marks each row with whether a saved value applies at once. On save the
service returns a `tui.SettingsChange`: the display options, the effective
settings, the icon resolution and two key lists. The root applies the `Live`
keys through the existing setters and shows one notice for the `Restart` keys:

`Restart gvim to apply: editor.hot_exit, editor.virtual_space`

The restart keys are read only while the editor starts: the editor's option
struct is built once, the session and recovery settings are read before the TUI
exists, and the terminal reports its capabilities once. The live keys are pushed
into the running editor by the service and the root:

- the root calls `SettingsPort.Apply` on the update loop (the editor belongs to
  that goroutine; the Save command only reads and writes files), and the service
  re-registers the editor's language-settings resolver (`languageSettings(cfg)`),
  formatters, rulers and ephemeral patterns, so the editing options that flow
  through the per-language settings (tab size, use tabs, auto indent, auto
  close, auto surround, line wrap and the whitespace flags) take effect for
  every open document;
- the root applies the display options (tab bar, status bar, prompt line,
  scrollbars, line numbers, line wrap, current-line/occurrence/column
  highlights, whitespace, cursor style, indent guides, rainbow brackets, smooth
  scroll, mouse wheel/double-click/hover delays, search defaults, explorer
  hidden/ignored, menu-bar mnemonics, show menu bar), the theme, the keymap and
  the icon set, rebuilding the styles and the menu bar as those need.

**Restart settings** in this slice: `editor.hot_exit`,
`editor.restore_previous_session`, `editor.auto_save_enabled` and its
interval, `editor.recovery_enabled`, `editor.virtual_space`,
`editor.scroll_offset`, the default line ending, trim-trailing-whitespace and
ensure-final-newline on save, `editor.auto_read_only`, and
`mouse_capture`. The LSP, plugin and large-file settings are not in the page
at all yet (deferred), and they keep their own restart boundary when they are
edited by hand.

**Command-line flags win over the file** for the running editor, as they already
do: setting the theme or the keymap while `--theme`/`--keymap` is given
writes the file but does not apply the value, and the notice says the flag wins.

**Reload.** Save always reloads. `SettingsPort.Reload()` re-reads the config
without writing and returns the same change (every live key); it is exposed for
a later **Reload Config** command and is not bound to a key in this slice.

## 6. What is deferred

- the generic widget runtime of Fresh's page (dual-list editors for status-bar
  elements, map editors for languages/lsp/plugins, text and file pickers);
- the sections with structured values: languages, LSP, formatters, plugins,
  keybindings, status-bar element lists, env detectors, terminal, and the newer
  `file_explorer` / sidebar width+side options (they have no scalar row yet);
- the session layer and per-buffer pins;
- Fresh's "write to the most specific layer that already defines the setting";
  the page has an explicit layer target;
- undo in the page, per-setting reset (Ctrl+R resets every pending value), and
  search scopes;
- a config **Reload** command/keybinding, and hot reload of the restart set;
- plugin settings and theme preview in the page.

The read-only Settings list remains as the fallback when the app wires no
`SettingsPort` (the TUI in a test, or a host without a config directory): it
lists the effective settings and opens the config file on Enter.

## 7. Files

| File | Role |
|---|---|
| `config/settings_schema.go` | the descriptor table, `Setting`, `Settings()`, `SettingByKey` |
| `config/patch.go` | `Edit`, `PatchBytes` (surgical hujson patch), `ValidateEdits` |
| `config/config.go` | `applyObject`, the shared key-application path |
| `internal/tui/ports.go` | `SettingsPort`, `SettingsSnapshot`, `SettingsItem`, `SettingEdit`, `SettingsChange` |
| `internal/tui/settings.go` | the editable page, `applySettingsChange`, the settings message |
| `internal/app/settingssvc.go` | the port implementation over the config files and the editor's live setters |
| `internal/app/run.go` | wires the port into the TUI |

## 8. Tests

- `config/patch_test.go`: comments/layout/trailing-comma style kept, unknown
  keys kept, parents created, missing file created, invalid file untouched,
  pointer escaping, no-op patches, validation through the option tables
  (bounds, enums, null, project C11 rules), schema coherence (every setting's
  current value encodes, validates, and is valid in the project layer when the
  setting allows that layer).
- `internal/app/settingssvc_test.go`: the snapshot, the trust gate on the
  project layer, a user-layer save with comments preserved and the editor's
  language settings applied live, the restart boundary, a trusted project-layer
  save and reload, a refused value leaving the file alone, Reload reading a file
  changed on disk, and the command-line flag winning.
- `internal/tui/settings_page_test.go`: opening the page and its sections,
  toggling and saving a bool through a fake port, the restart notice, filtering
  and reset, the layer switch (trusted and untrusted), the prompt validation,
  the discard confirmation, live value application, and Edit raw without a file.
