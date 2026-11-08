# Themes

Status: design record of the theme model and the theme UI (round 5).

gvim paints from a color theme. The bundled themes are Fresh's (copied under
`theme/data`); user themes live in `config.Dir()/themes` and may extend a built-in theme. The model lives in `theme/`: the key table, parsing, inheritance, fallbacks, the registry that finds themes, and the in-memory layer the plugin API and the theme editor use. `internal/tui/style` is the one place colors become styles, so every consumer goes through the same resolution.

## 1. Theme files and config forms

A theme file is Fresh's JSON format: a `name`, an optional `extends`, and the sections `editor`, `ui`, `search`, `diagnostic` and `syntax`. A value is an `[r,g,b]` array, a color name (`"Yellow"`, `"Default"`), a `{"color": ..., "modifier": ["bold", ...]}` bundle, or (for the three attribute-only keys) a list of attributes. Unknown color keys are warnings, not errors; a value the format cannot read rejects the theme with the key named.

Config forms (seen in `config/config.go` and validated in `theme/form.go`):

```jsonc
{ "theme": "dark" }                              // a bundled theme
{ "theme": "builtin://light" }                   // the explicit builtin form
{ "theme": "my-theme.json" }                     // relative to config.Dir()/themes
{ "theme": "subdir/dark.json" }                  // nested user theme
{ "theme": "file://$HOME/themes/x.json" }        // absolute; $HOME and
                                                  // $XDG_CONFIG_HOME expand
```

URL/packaged themes (`https://...` forms) are recognized and refused with a clear error naming the value; that is the recorded decision: gvim keeps the refusal and has no network code or fetch slice until packaging and network policy are decided (remote packs stay out). The command-line `--theme` takes the same forms; a plain name resolves against the bundled themes then the user theme dir, and a typo is one line that names the choices. The Settings page's theme row lists the catalog refs, validates through the same loader (so a URL form is refused there with the same message) and shows the four-colour preview (§6).

## 2. Inheritance and fallbacks

The model is a port of Fresh's `theme/types.rs` (source revision `2338640274915a37786810bd8b59439c6633ba64`, recorded in `theme/keys_table.go`):

- `theme/keys_table.go` is the `theme_color_keys!` table: 143 color keys (49 required, 91 fallback, 3 optional) plus gvim's two `ui.accent_*` extension keys, the three attribute-only keys, and the `ui.popup_fg` alias. The checked-table test (`theme/keys_table_test.go`) asserts the counts, the exact required list, that every fallback chain terminates at a required or optional key without cycles, and that every bundled theme resolves all required keys.
- `Resolve` chooses the base: an explicit `extends` must name a built-in theme (chaining user themes is not supported, as in Fresh); a complete standalone theme (all required keys named) has no base; otherwise the implicit base is `builtin://light` when `editor.bg`'s relative luminance is above 0.5 and `builtin://dark` otherwise (the terminal default and an unreadable background count as dark).
- With a base, a key the theme does not name is the base's resolved value - the fallback chain is not re-derived (Fresh's rule: `extends` wins over fallbacks). A standalone theme fills each omitted fallback key from the first key its chain names, taking that key's color and attributes.
- The two gvim extension keys `ui.accent_bg`/`ui.accent_fg` fall back to `ui.popup_selection_bg`/`ui.popup_selection_fg` (they are `FallbackKey`s in the table). Every gvim accent surface reads them: the `style.Accent` role (status highlights, the palette and menu selections), the `style.Overlay` highlight/border/focus, and the explorer/diagnostics marker bars. The bundled themes either leave the extension keys to the fallback or set them to exactly the popup selection colours, so every bundled theme's accents are Fresh's popup selection colours (`theme/accent_audit_test.go` and `internal/tui/style/accent_audit_test.go` pin the equality and the painted roles); a user theme may set `accent_bg`/`accent_fg` explicitly to choose a different accent (a theme with a base inherits the base's accent unless it names them, and a standalone theme fills them through the chain — the same tests pin those cases).
- Attributes apply to the foreground AND the background of a value: an attribute on `editor.selection_bg`, for example, reaches the selected text, and the legacy `editor.selection_modifier` / `ui.semantic_highlight_modifier` keys overwrite the attributes of their color key when a theme gives both. `editor.bracket_rainbow_match_modifier` defaults to bold+underlined.

## 3. Named colors and Reset

Named colors (`Black`, `Red`, ... `White`) render through the terminal's own ANSI palette, as Fresh's ratatui named colors do - this was checked in the reference source (`TokenColor::to_ratatui` returns the ratatui named color; the fixed-RGB table is used only for luminance, inspect/animation math and the theme editor). gvim therefore keeps the user's terminal palette for such themes and the bundled `terminal` theme stays fully host-adaptive. Fresh's fixed-RGB table (`Red 205,0,0` etc., `color_to_rgb`) is ported and used for: the light/dark choice and the `dark` flag, the inspect popup's RGB text and swatches, the theme editor's swatches, parsing/validation, and `plugin ThemeColor` (a named color now answers with its fixed RGB; it used to have no RGB). `"Default"` / `"Reset"` mean the terminal default (no color). Rendered colors are compared in the inspect popup by their rendered form (ANSI name or fixed RGB), never by approximate value.

## 4. The registry and hot reload

`theme.Registry` (`theme/registry.go`) owns discovery and resolution: `NewRegistry(config.Dir()/themes)`, `Reload()` (rescan + clear the resolved cache), `Infos()` (name, portable ref, builtin flag, path), `Load(ref)`, `Canonical(ref)` (the portable ref a config writes), `Warnings()` and `ThemePath`. A broken user theme is never a startup failure: it is listed and `Load` reports it where it is used; scanning problems are warnings. The package-level `theme.Names()`/`theme.Load()` wrappers stay for the bundled themes and for importers that predate the registry.

The app builds one registry in `buildRuntime` and passes it as `tui.Deps.ThemeCatalog`; `runTheme`, `Options.Validate` (which reads the user theme dir), the settings service and `configview` use it. The TUI keeps `m.themeRef` (the portable ref) and `m.theme`; `applyTheme(ref)` resolves through the catalog and calls `rebuildTheme()`, which applies `theme.WithOverrides` (plugin/editor layer), rebuilds `style.New`/`editorpane.NewStyles`/menubar/explorer and raises `theme_changed` with `{name, dark}`. Nil catalog falls back to the bundled themes (tests).

Hot reload: the **Reload Configuration** command re-reads the config and calls `Registry.Reload`, so a theme file edited by hand is read again; the theme is always in the change's live set. Saving from the theme editor does the same after its atomic write.

## 5. Inspect at cursor

**Inspect Theme at Cursor** (palette) and **Ctrl+Right-Click** on a cell open the theme inspector (`internal/tui/theme_inspect.go`). The cell is read from a repaint of the frame, so its foreground, background and attributes are the ones actually drawn.

**Per-cell painted-key provenance (round 11).** The paint path records which theme key each cell's colours came from, Fresh's `setFg`/`setBg`-style attribution: `grid.Style` carries two compact ids (`FGKey`/`BGKey`, 0 = unknown) and `grid.Grid` has an opt-in per-cell capture buffer. The ids are dense 1..N indices into a role table built from `theme.Keys()` in `internal/tui/style/roles.go` (canonical name and aliases resolve through `theme.KeyByName`); the table is the only place `section.key` becomes an id. Tagging happens where styles are constructed, not in the paint loops: `style.TaggedCell`/`TaggedStyle` are used by `editorpane.NewStyles` (text, selection, gutter, scrollbar, tab/current-line/ruler/whitespace/occurrence/diagnostic roles and every syntax kind with `editor.bg` as its background), by `style.NewOverlay` (box/title/muted/idle/hover, accent selection and focus, border, error), by the status bar and menu band, and by the explorer/sidebar/diagnostics chrome. That covers the first-surface subset: editor text and syntax, gutter/line numbers, selection and current line, status bar (including the accent palette segment and the warning run), popup/list/palette/Settings rows and the menu bar.

The inspector repaints once through `Model.paintInto` with capture enabled (`paintCaptured`), reads the captured ids for the inspected cell and prints the exact keys first: `Painted by (fg): syntax.keyword #C586C0`, `Painted by (bg): editor.bg #0D1117`, plus the pair line `Painted by: syntax.keyword on editor.bg` when both sides are attributed. Where a surface did not attribute a side (styles built ad hoc, compose/page-view/plugin painters), the popup falls back to the previous behaviour and says so explicitly: `Foreground (matched by colour): …` lists the resolved keys whose rendered colour equals the cell's, capped at eight plus a summary, and the context facts pick the role among them: the screen and buffer position, the current line, the selection, the active find text, the diagnostic covering the position (severity and message), and the syntax category of the character with its theme key. Capture is off everywhere else: a normal frame allocates no buffer, records nothing and paints the same cells (tests pin both). The popup dismisses on any key or a click outside; the click or key then goes on to the editor, as Fresh's popup does. A plain right click keeps the context menu.

## 6. Theme editor

The native editor (`internal/tui/theme_editor.go`) is a list-driven overlay over the resolved theme: section headers and the color keys with a swatch, value (`#RRGGBB`, ANSI name or Default) and attributes. Enter edits the color (hex, Fresh named color, Default/Reset; validated by `theme.ParseColorText`), `Ctrl+A` edits the attributes (comma list), and every edit live-previews by rebuilding the styles from an in-memory theme layered under the plugin overrides. Esc restores the theme that was applied when the editor opened (asking first when there are unsaved edits). `Ctrl+S` on a bundled theme routes to Save As; a user theme saves to `config.Dir()/themes/<name>.json` through `atomicfile` (the catalog's `SaveTheme` writes the full resolved theme, so the file is standalone and portable), then the registry reloads and the theme is applied. `Ctrl+Shift+S` is Save As, `Ctrl+D` deletes a user theme (confirm + atomic remove). The palette rows are **Select Theme** (already generated) and the native **Edit Theme**.

**Colour picker.** `p` on a colour row — or a left click on the row's swatch (a click elsewhere only selects the row, as before) — opens the swatch-grid picker (`internal/tui/theme_picker.go`). Its candidates are the working theme's distinct colours in key-table order followed by the named ANSI colours, eight chips per row, labelled with their `#RRGGBB`/name in the selected line. Arrow keys/Home/End/PageUp/PageDown move the selection; a mouse click selects a chip and a drag keeps moving the selection; moving live-previews the candidate on the edited key through the same in-memory working theme (so the whole frame and the editor rows repaint). **Enter** applies it as a normal colour edit — the editor becomes dirty and can save — while **Esc**, or a click outside the grid, restores the value the editor had and closes. Mouse selection never commits on its own, so the keyboard stays the confirm step. The existing Enter hex/name prompt and `Ctrl+A` attributes prompt are unchanged; the editor footer names `p picker`.

## 7. Plugin overrides and the API

The plugin API `gvim.theme.override(colors)` / `gvim.theme.clear()` (owned by the plugin host layer) reaches the canonical layering `theme.WithOverrides(base, map[string]string)`: keys are `section.key` theme keys (aliases accepted), values are `#RRGGBB`, a Fresh color name, or `Default`; a color override keeps the base value's attributes. The TUI owns the map and applies it in `rebuildTheme`; `SetThemeOverrides`/`ClearThemeOverrides` on the root model back `pluginHost.ThemeOverrides`/`SetThemeOverrides`. Overrides are cleared on every theme change. `theme_changed` fires with `{name, dark}`; the existing `Theme()`/`ThemeColor()` host reads now use the enriched model (named colors answer with their fixed RGB). `gvim.theme.get_color` keeps working.

## 8. What is deferred

- URL/packaged themes and remote packs: **decision recorded** — keep the clear refusal, no network code and no fetch slice until packaging/network policy exists (see §1);
- inheriting from another *user* theme: **decision** — one level of `extends` to a built-in
  theme is the model (a user theme extends a built-in theme; Fresh's own limit, kept).
- background images in themes: **decision** — out of the colour theme model (theme keys are
  colours and attributes; Fresh's editor background support is a separate feature), recorded
  as a deliberate refusal rather than a missing key.
- drag-and-drop of theme values or swatches: **decision** — not added. The colour picker's
  drag moves the selection; nothing reorders theme values, and no colour-picker entry is
  bolted onto the Settings theme row (the picker edits theme values and belongs to the theme
  editor, while the Settings row's Enter/click opens Select Theme);
- untagged painters remain a fallback: compose/page-view/plugin/custom-overlay cells report `matched by colour` instead of provenance (the first-surface subset — editor text/syntax, gutter, selection/current line, status bar, popup/palette/Settings rows, menu — is attributed);
- accent keys beyond gvim's own `ui.accent_*` extension; a Fresh theme and every bundled theme get the popup selection colors as their accent (§2).

The Settings page's theme preview landed (four colour chips on the theme row; mechanics and keys in `docs/settings.md` §4), so it is no longer deferred.

## 9. Files

| File | Role |
|---|---|
| `theme/keys_table.go` | the ported color key table, aliases, modifier-only keys |
| `theme/theme.go` / `theme/color.go` | Theme/Color types, fixed-RGB names, reset, attributes, luminance |
| `theme/themefile.go` | JSON theme file parsing and warnings |
| `theme/resolve.go` | base choice, extends, fallback filling, modifier-only keys |
| `theme/registry.go` | builtin+user discovery, config forms, reload, save/delete paths |
| `theme/overrides.go` | `WithOverrides` and the theme editor's save format |
| `theme/form.go` | config/CLI theme form validation |
| `internal/tui/style` | the single theme-to-styles conversion (fg+bg attributes); `style/roles.go` is the theme-key id role table and the tagging helpers |
| `internal/tui/grid` | the cell grid; `grid.Style.FGKey/BGKey` and the opt-in `CaptureKeys`/`KeysAt` buffer are the provenance capture |
| `internal/tui/theme_inspect.go` | inspect popup and Ctrl+Right-Click |
| `internal/tui/theme_editor.go` | the native theme editor (list, hex/attributes prompts, Save/Save As/Delete/Open) |
| `internal/tui/theme_picker.go` | the editor's colour picker: swatch grid, arrow/mouse selection, live preview, Enter/Esc |
| `internal/tui/plugin_host.go` | plugin theme reads and overrides |
| `internal/app/theme_registry.go` | the ThemeCatalog over the registry, atomic saves |
| `internal/app/settings_theme_preview.go` | the Settings theme row's cached colour previews (background/foreground/accent/popup selection) |
| `internal/app/run.go` | wires the registry/catalog into the TUI |

## 10. Tests

- `theme/keys_table_test.go`: counts, required list, chain invariants, bundled themes; the accent extension keys.
- `theme/theme_test.go` / `theme/model_test.go`: parsing warnings and errors, base-by-luminance, extends, standalone fallbacks, attribute bundles and the legacy modifier keys, named/Reset colors, overrides (incl. dark flag), save round-trip, registry forms `file://`/env/user dir/reload/delete, save.
- `internal/tui/style/decor_test.go` / `editorpane/styles_test.go`: partial themes use the base's values (Fresh's rule), attributes reach cells.
- `internal/tui/theme_tui_test.go`: inspect command and Ctrl+Right-Click, the catalog picker, apply through the catalog, overrides layering and clearing.
- `internal/tui/theme_inspect_provenance_test.go`: exact `Painted by` output for editor text, syntax, gutter, status bar, menu bar and a palette/Settings selection; the colour+context fallback for an unattributed theme-editor swatch; a normal paint with no capture and an identical captured repaint.
- `internal/tui/style/roles_test.go`: role ids dense/unique/round-trip/aliases, `TaggedCell` colour equality, overlay role ids. `internal/tui/editorpane/styles_role_test.go`: pane roles and syntax kinds carry their keys. `internal/tui/grid/keys_test.go`: capture off/on, all write paths, release, and rendering unchanged.
- `internal/tui/theme_editor_test.go`: editor list/preview/save/attributes; `theme_picker_test.go`: both picker entry points, click/drag preview, Enter apply, Esc/outside restore, palette contents.
- `internal/app/theme_test.go`: config theme forms, the atomic Save/Delete path, hot reload of an edited user theme; `app_test.go`: `--theme` forms.
- `theme/accent_audit_test.go`: every bundled theme's accent equals its popup selection, a standalone theme fills the accent through the fallback chain, and the accent keys are editor rows.
- `internal/app/settingssvc_test.go`: Reload re-applies the theme; the R4-3 lock test (Snapshot answers while a Save's file write is blocked).