# Fresh parity record

Status: live working record for the "full Fresh feature parity" goal, first written 2026-11-08 and
refreshed after rounds 1–3 landed (round 1: workspace trust, sidebar Contents, status pills;
round 2: virtual space, keymaps, status bar/menus, sessions; round 3: settings page, terminal
scrollback/links/mouse, explorer options, palette preview/toggles). Every status below was
checked in this tree or in `refs/fresh`, not inferred from the old migration notes. Update the
affected row when a round lands instead of rewriting it.

Last verified: 2026-11-08, tree commit `8865c28` plus the uncommitted round-3 work. Round 3's
full gate set was run by this audit (go build ./..., CGO_ENABLED=0 go build ./..., go vet,
golangci-lint 0 issues, purity, import rules, go test ./... -count=1: all green).
Fresh reference: the `refs/fresh` checkout as read on the same date.

**Round 1 outcome (2026-11-08):** workspace trust (levels/store/prompt/dialog/palette command/
config-layer + LSP-autostart gating/status pill), native sidebar sections with the Markdown
Contents section (the Lua `markdown_toc` plugin is retired: bundled plugins 9, 7 enabled by
default; runtime palette 217 rows), and the status bar's trust pill, LSP pill states and
`[E:1 W:2 I:3]` diagnostics badge. Residual gaps round 1 recorded are R1-1…R1-4 in §5.

**Round 2 outcome (2026-11-08):** virtual space (the `on` subset) and end-of-line inline
diagnostics; the five keymaps rebuilt on Fresh's inheritance graph with Fresh's bindings for the
four contexts gvim resolves (default 198 own/196 in effect, macos 53/212, macos-gui 50/261,
emacs 78/243, vscode 9/201); config-driven status bar element lists (Fresh's defaults,
separator, squeeze rule) plus menu accelerators (`editor.menu_bar_mnemonics`); and session
settings/CLI (`hot_exit`, `restore_previous_session`, `skip_session_restore_when_files_passed`,
`--restore`/`--no-restore`/`--no-session`/`--simple`). Residuals are R2-1…R2-5 in §5.

**Round 3 outcome (2026-11-08):** the editable Settings page (53 scalar settings in 8 sections,
User/Project layers, comment-preserving JSONC patch writes, live apply plus an explicit restart
set) — `docs/settings.md`; terminal slice A (`terminal.shell`, `close_on_exit`,
`scrollback_lines`, `mouse_forwarding`, a read-only scrollback pager with search/copy, OSC 7 +
Ctrl+Click links, mouse forwarding) — `docs/terminal.md`; explorer parity (`file_explorer` width/
side/follow/auto-open/compact options, sticky parents, reveal) — `docs/sidebar.md` §9; and the
finder/palette slice (file-preview column, Live Grep match modes and files/buffers/ignored
scopes, match counts) — `docs/ui.md`. Residuals are R3-1…R3-4 in §5.

The reference editor is the checkout in `refs/fresh` (read-only). The allowed differences are
listed in §4; everything else that Fresh has and gvim lacks is a gap, ranked in §5.

## 1. Method and evidence

### 1.1 Fresh inventory sources

- `refs/fresh/docs/features/*.md` (20 pages: editing, search-replace, file-explorer, terminal,
  lsp, git, compare-files, themes, encoding, keybinding-editor, session-persistence, ssh,
  devcontainer, dashboard, code-tours, navigation, command-palette, languages, emacs-keymap,
  scripting), `docs/configuration/` (layers, init.ts, keyboard, actions),
  `docs/internal/` design notes (sidebar sections, terminal, LSP, retained UI, plugin system).
- `refs/fresh/crates/fresh-editor/tests/e2e/*.rs`: 413 test files used as a feature index.
- `refs/fresh/crates/fresh-editor/plugins/`: 50 bundled `.ts` plugins; ~180
  `registerCommand(` call sites (approximate count, used as a breadth signal).
- `refs/fresh/crates/fresh-editor/src/input/commands.rs`: 206 `CommandDef` palette commands;
  `input/keybindings.rs`: 374 action names; `src/app/`, `src/services/`, `src/view/` for the
  feature implementations.
- `refs/fresh/crates/fresh-editor-core/src/config.rs`: top-level `Config` (20 keys),
  `translated_menus()` (8 menus), per-section config structs; `keymaps/*.json` (5 presets).
- Fresh's plugin declaration `plugins/lib/fresh.d.ts`: 9368 lines, ~372 API methods.

### 1.2 How a gvim status was determined

Status is backed by one or more of: a package/file read, a test name, a doc that states the
behaviour, or a generated table. "Missing" means a grep of the tree (excluding `refs/`,
`_attic/`, `.git/`) found no implementation and no doc claiming it. Each row names the evidence
in the fourth column; absence claims that are easy to re-check name the command or file.

gvim's own release-grade sources used here: `README.md`, `docs/architecture.md`, `docs/ui.md`,
`docs/lsp.md`, `docs/compare.md`, `docs/keybindings.md`, `docs/sidebar.md`, `docs/trust.md`,
`docs/plugins/*`, `CLAUDE.md`, `commands/commands_gen.go` (206 rows, generated from Fresh's
`CommandDef` table), `internal/uiadapter/dead_bindings_test.go`, `internal/tui/notice_na.go`,
`config/*` (settings schema and layers), `keymap/*.json`, `theme/data/*.json`, the bundled
plugin tree, the round-1 packages and their design docs (`trust/`, `outline/`,
`internal/tui/sidebar/`), and the per-feature `*_test.go` files.

Round-2 evidence adds `document/virtualspace.go`, `editor/virtualspace.go`,
`internal/tui/editorpane/inline_diag.go`, `keymap/menu.go`,
`internal/app/{hotexit,session_open,session_plan}.go`, the status-row code in
`internal/tui/frame/frame.go`, and the updated design records (`docs/ui.md` status bar and
menu accelerators, `docs/keybindings.md`, `docs/lsp.md`, README sessions and flags).

Round-3 evidence adds `docs/settings.md` and `docs/terminal.md`, `config/{settings_schema,patch}.go`,
`internal/app/settingssvc.go`, `internal/terminal/{scrollback,mouse}.go` and the session callbacks,
`internal/tui/dock/{scrollback,input}.go`, `explorer/{options,sticky}.go` with `internal/app/explorer.go`,
`internal/tui/palette/{preview,grep}.go` with `internal/app/{preview,live_grep}.go`, and the updated
records (`docs/sidebar.md` §9, `docs/ui.md` palette details, README).

### 1.3 Status vocabulary

| Status | Meaning |
|---|---|
| **implemented** | The capability exists in gvim and is exercised by tests/docs at or above Fresh's level for that area. |
| **partial** | The core exists but named Fresh sub-features are absent or reduced; the row lists them. |
| **missing** | No implementation was found. |
| **different** | Deliberate and allowed divergence (§4), not a gap. |
| **partial (R1 landed)** | The round-1 work is in the working tree and closes part of the Fresh area; the remaining sub-gaps are listed. The commit and the full gates are the Lead's round gate. |

### 1.4 Historical docs: do not use for status

`migration/GAP-v3.md`, `migration/ROUND-PLAN.md`, `migration/PHASE1-EDITOR.md`,
`migration/BLUEPRINT.md` and `migration/ARCH-v4.md` are historical records. Their package paths
predate the layout move, their plugin plan predates the Lua rewrite, and GAP-v3's matrix (for
example "syntax highlighting: 0", "palette commands implemented: 22") describes the tree before
rounds P1–P5 landed. They are useful only for rationale and dependency history. The current
design records are `docs/architecture.md` and `docs/ui.md`; this file supersedes those docs
for parity status.

### 1.5 How to verify or refresh a row cheaply

- **Feature exists at all:** `grep -rn "<symbol>" --include='*.go' . | grep -v refs`, then read
  the nearest file; tests are named after the feature, so `go test ./<pkg> -run <Name>` usually
  finds the contract.
- **Command or notice gaps:** `grep -n "<action>" internal/tui/*.go`; a handler that only
  assigns `m.notice` is a stub. The repo's dispatch gap lists are `unhandledActions` and
  `unhandledPaletteActions` in `internal/uiadapter/dead_bindings_test.go` (today: one bound
  action, no palette actions). They are the dispatch floor, not the feature-gap list: most
  remaining gaps are reduced handlers that dispatch but answer with a notice.
- **Menu, command and keymap data:** `python3 commands/gen/generate.py refs/fresh` and
  `python3 tools/gen_menus.py --check` regenerate/check the tables; keymap bindings are plain
  JSON under `keymap/`.
- **Config claims:** grep the option name inside and outside `config/`; parsed-but-unused is a
  common pattern (`StatusBar`, `HotExit`, `RestorePreviousSession`, `DiagnosticsInlineText`,
  `ScreensaverEnabled`, `Animations`).
- **Fresh side:** read `refs/fresh/docs/features/<name>.md` for intent, the matching
  `crates/fresh-editor/tests/e2e/<name>.rs` for behaviour, and the plugin source when the
  feature ships as a bundled plugin (`refs/fresh/crates/fresh-editor/plugins/<name>.ts`).
- **Renderer claims:** assert a few cells/rows in a `*_test.go` (no golden frames, CLAUDE.md).
  `GVIM_SHOTS=/tmp/gvim-shots go test -run TestWriteScreenshots -count=1 ./internal/app/` and
  `cmd/gvimdebug` are the visual check.
- **Close the loop:** record the date and tree commit you checked in the row, and delete the
  backlog entry instead of moving it.

## 2. Roll-up

| Measure | Fresh | gvim | Note |
|---|---|---|---|
| Palette commands (runtime) | 206 core (Fresh's `CommandDef` table) | 217 | 206 generated from Fresh's `CommandDef` table + 7 hand-written native rows (compare, markdown Contents, keys) + 4 plugin-management rows; `commands/palette_test.go` pins 217. All have a handler or a documented reason (`unhandledPaletteActions` is empty, `internal/uiadapter/dead_bindings_test.go`). The bound-action gap list (`unhandledActions`) now holds only `file_explorer_search_backspace`. |
| Documented "not applicable" commands | n/a | 5 | `internal/tui/notice_na.go:9` (detach, update, update log, wave, inspect theme). The round-1 trust command is no longer here: `workspace_trust_prompt` now runs. |
| Menus / menu actions | 8 / 103 static actions, 31 separators, 2 static submenus + 1 dynamic | 8 / 103 static actions, 31 separators, 2 static submenus + the dynamic one filled from 10 themes | Static parity is exact against Fresh's `translated_menus()` (103 actions / 31 separators / 2 static submenus); the dynamic "Copy with Formatting" submenu is filled at run time from the bundled themes (`internal/tui/menubar/dynamic.go`, present before round 1 — the round-1 note that it was empty was wrong). Help's label row says "gvim" where Fresh writes its version. Round 2 added the `editor.menu_bar_mnemonics` accelerators. |
| Keymap presets | 5 | 5 | Same names and inheritance graph (default base; macos/emacs/vscode inherit default; macos-gui inherits macos). Own / in-effect bindings after round 2: default 198/196, macos 53/212, macos-gui 50/261, emacs 78/243, vscode 9/201 (`keymap/common_set_test.go`); the presets are Fresh's own binding data for the four contexts gvim resolves (`docs/keybindings.md`). |
| Bundled plugins | 50 | 9 (10 before round 1) | 7 on by default (8 before round 1); `flash` and `vi_mode` off; round 1 retired the Lua `markdown_toc` plugin and folded the feature into the native Contents section (`docs/plugins/bundled.md` documents nine plugins). |
| Bundled plugin commands | ~180 | 13 after round 1 (15 before) | from `registerCommand(` / `gvim.command.register` counts; round 1 removed `markdown_toc`'s 2 registrations. |
| Themes | 10 built-ins | 10 built-ins | same names (`theme/data/` mirrors `fresh-editor-core/themes/`). |
| LSP server entries | built-in + plugin helpers | 62 | `lsp/manager/defaults.go`; gvim's table covers Fresh's documented table (`docs/lsp.md`). |
| Plugin API surface | ~372 methods | ~172 functions | `fresh.d.ts` vs `docs/plugins/gvim.d.lua`; runtime language differs by design, capability gaps do not. |
| Settings page | schema-driven page (~14k LOC in refs/fresh) over every setting | 53 scalar settings / 8 sections, User+Project layers | `config/settings_schema.go` + `internal/tui/settings.go` + `internal/app/settingssvc.go`; structured sections are Edit-raw only (R3-1). |
| Terminal | live + scrollback buffer, links, mouse, shell override, agent resume, persistence | slice A: 4 config keys, scrollback pager, links, mouse forwarding | `terminal.shell`/`close_on_exit`/`mouse_forwarding` implemented, gvim-only `scrollback_lines`; deferred drag-select/reflow/persistence/agent resume/Windows/tabs (R3-2). |
| Explorer options | `file_explorer.width/side/follow_active_buffer/auto_open_on_last_buffer_close/compact_directories/preview_tabs/…` + sticky parents | 5 options + sticky parents implemented | width percent/columns with grip, left/right side geometry, follow + reveal, auto-open on last close, compact chains, sticky band; side toggle session-only and 4 keys accepted-ignored (R3-3). |
| Finder / Live Grep | file preview, Live Grep toolbar (case/word/regex, files/buffers/ignored/terminals/workspaces), quickfix, streaming | preview column + 6 Live Grep toggles | `internal/tui/palette/{preview,grep}.go`; deferred quickfix, terminals/workspaces providers, streaming, Live Grep row preview, Resume cache (R3-4). |

| e2e feature index | 413 test files | not comparable | gvim uses Go unit tests; Fresh's file names are the feature index used below. |

## 3. Matrix by area

### 3.1 Engine and editing

| Area | Fresh features (evidence) | gvim status | gvim evidence | Notable sub-gaps |
|---|---|---|---|---|
| Text model, large files, encodings | rune text, undo/redo, two-mode large-file support, encoding detect/convert (UTF-8/16, Latin-1, CP125x, GBK/GB18030, Shift-JIS, EUC-KR), EOL detect | **implemented** | `buffer/`, `document/`, `encoding/`, `editor/large_*`, `editor/files_lineending*`, `docs/architecture.md`; Fresh `docs/features/encoding.md` | None found. Detection heuristics may order differently; per-file reload uses the same palette command. |
| Editing core, smart editing, vi | auto-indent/close/surround, smart home/backspace, bracket match, comment, case, transpose, sort/trim, shell command, vi-mode plugin | **implemented** | `document/smart*.go`, `editor/comment.go`, `document/case.go`, `editor/shell.go`, `plugin/bundled/plugins/vi_mode`, tests per action; Fresh `docs/features/editing.md` | Markdown compose mode is missing (row 24). Fresh's paragraph motions and "insert/open line" exist; replacement-mode (`R`) is documented as unsupported in the vi plugin. |
| Selection, multi-cursor, marks | multi-cursor (Ctrl+D, above/below, line ends), block selection, mark mode, mouse word/line/drag selection | **implemented** | `document/cursors*.go`, `document/block.go`, `editor/marks.go`, `internal/tui/motion_mouse.go`, keys in `keymap/default.json`; Fresh `docs/features/editing.md` | `add_cursors_to_line_ends` is not bound by default (present in Fresh's preset); Fresh's double-click word drag is implemented. |
| Undo, save pipeline, file metadata | one-undo-step edits, atomic saves, interrupted-save recovery, permissions/read-only, final-newline/trim on save, auto-save, auto-revert | **implemented** | `document/save_plan.go`, `atomicfile/`, `editor/files_recovery.go`, `editor/interrupted.go`, `internal/restore`, `docs/architecture.md` | Fresh's `sudo save prompt` is missing (no `sudo` path found); permission/ownership preservation is present. |
| Search & replace (in-buffer) | find, replace, query replace (y/n/!/q), find in selection, selection next/prev, case/whole-word/regex toggles, confirm-each, search history, match counts | **implemented** | `search/`, `internal/tui/find/`, `internal/tui/find_route.go`, tests `find_*_test.go`; Fresh `docs/features/search-replace.md` | Project-wide Search & Replace is missing (row 21); Git Grep plugin is missing. |
| Navigation, bookmarks, macros | position history, go to line/bracket, bookmarks 0-9 (set/jump/list), macros 0-9 + save-to-init/promote-to-command | **partial** | `editor/nav*.go`, `editor/goto.go`, `editor/marks.go`, `internal/tui/root_bookmarks.go`, `internal/tui/macro.go`; Fresh `docs/features/editing.md` | `List Bookmarks` is a stub ("editor does not list its bookmark slots", `internal/tui/root_commands.go:16`). One macro slot only: record/play/list work, `Macro: Save to init.ts`/`Promote to command` write an `init.lua` block (Fresh writes `init.ts`), no macro registers 0-9. |
| Display & view options | line numbers/relative, wrap + wrap_column + wrap indent, page view, virtual space, rulers, indent guides + rainbow, whitespace marks incl. newlines/CR, current line/column, occurrence, post-EOF tilde, scrollbars + markers, smooth scroll, inline diagnostics, animations, screensaver | **partial** (R2 landed the user-visible subset) | `document/virtualspace.go`, `editor/virtualspace.go`, `internal/tui/editorpane/{inline_diag,pane}.go`, `internal/tui/virtual_space_test.go`, `editor/virtualspace_test.go`, `internal/tui/editorpane/virtual_inline_test.go`, `docs/lsp.md`; Fresh `docs/features/editing.md`, `docs/internal/virtual-space-scoping.md` | Round 2 closed: virtual space for the horizontal `on` subset (caret past EOL, typing pads the gap, up/down keep the column, clicks, per-buffer toggle; off when wrapped, multi-caret or large file) and end-of-line inline diagnostics (most severe message, right-aligned, severity colour, dropped when narrow). Still open: virtual space `block` mode and Fresh's vertical virtual lines (R2-1); no version-aware staleness for inline diagnostics (R2-2); page view is stored only and belongs to the Markdown row/backlog #12; mouse hover is not built; scroll sync skips compare views. The style-only keys (`animations`, `cursor_jump_animation`, `viewport_edge_fade`, `screensaver_*`) are accepted config keys but deliberately not carried into the TUI (§4 item 9). |
| Folding | indent folds + LSP `foldingRange`, fold indicators, per-buffer toggle | **partial** | `editor/fold.go`, `document/fold.go`, `internal/tui/editorpane` gutter; Fresh `docs/features/editing.md` | No LSP folding ranges (grep for `foldingRange` returns nothing); indent folding, gutter arrows, toggle and per-buffer override are implemented. |
| Splits, tabs, panes, windows | splits (vertical/horizontal), tab strip, tab drag, preview tabs, buffer groups, multiple windows/workspaces | **partial** | `internal/tui/split*`, `internal/tui/tabs.go`, `internal/tui/tab_drag.go`, `internal/tui/tab_drop.go`, preview tabs in `internal/tui/model.go`; Fresh `docs/features/navigation.md` | No buffer groups, no second window/workspace model; "Move tab to other split" and "Extract Tab to New Workspace" are notices (`internal/tui/tab_drag.go:45`, `internal/tui/root_fix.go:40`); `next_window`/`prev_window` map to pane cycling (`internal/tui/split_tabs.go`). |
| Compare files | `fresh --diff A B`, editable side-by-side, change copy/take, next/prev difference, switch side, scrollbar marks | **implemented** | `internal/tui/compare*.go`, `docs/compare.md`, `compare_*_test.go`, `internal/cli` `--diff`; Fresh `docs/features/compare-files.md` | Presentation differs deliberately (two file tabs, §4). Fresh's plugin API `compareBuffers`/`replaceRange` for plugins is missing; large file mode is refused on both sides. |

### 3.2 Project, chrome and settings

| Area | Fresh features (evidence) | gvim status | gvim evidence | Notable sub-gaps |
|---|---|---|---|---|
| File explorer and sidebar | tree, file create/delete/rename/copy/cut/paste, hidden/gitignored toggles, git status marks, context menu, async reload, width/side, follow active buffer, compact chains, sticky parents, plugin rows/decorations, sidebar sections | **partial** (R3 landed the options) | `explorer/{options,sticky}.go`, `internal/tui/explorer/` (panel/context), `internal/tui/root_explorer.go` (reveal/follow/auto-open), `internal/tui/frame/frame.go` (side geometry), `config/fileexplorer.go`, `internal/app/explorer.go`, `internal/tui/sidebar/`, `docs/sidebar.md` §9, tests `explorer/*_test.go`, `internal/tui/explorer_parity_test.go`; Fresh `docs/features/file-explorer.md` | Round 3 landed Fresh's explorer options: `width` (percent or columns, grip keeps the form, frame clamps [16,60]), `side` left/right with full frame geometry and hit map, `follow_active_buffer` (reveal on tab change, silent), `auto_open_on_last_buffer_close`, `compact_directories` (one row per single-child chain), sticky-parent band (≤7, never the whole body) and explicit reveal feedback on show/focus. Still open: plugin explorer rows/decorations/slots; `respect_gitignore`, `custom_ignore_patterns`, `preview_tabs` and the tree-indicator glyph keys are accepted-ignored; the side toggle is session-only (Fresh persists it); per-segment compact-row clicks/context menus; Contents residual gaps (R1-1, R1-2). |
| Finder, quick open, live grep | Ctrl+P modes (file/command/line/buffer), fuzzy ranking, streaming enumeration, finder preview, live grep with provider toggles (case/regex/word/files/buffers/ignored/terminals/workspaces), quickfix export, resume | **partial** (R3 landed the preview and toggles) | `finder/finder.go` (`EnumerateAll`, hidden/ignored walk), `internal/tui/palette/{preview,grep,palette,draw}.go`, `internal/app/{preview,live_grep}.go`, `docs/ui.md` palette details, tests `palette/{preview,grep,palette}_test.go`, `internal/app/live_grep_test.go`; Fresh `docs/features/command-palette.md`, `search-replace.md` | Round 3 landed: the file-mode preview column (head of the selected file, 16 KiB / 12 lines, `path:line:col` parsing, target line highlighted, binary/empty/unreadable placeholders, dropped under 64 inner cells) and Live Grep's toolbar — case (seeded by `editor.search.case_sensitive`), regex (on by default as Fresh's), whole word, files, buffers (1 MiB snapshot per buffer) and ignored/hidden sources, with match counts and error rows. Still open: quickfix export, terminals/workspaces providers (need the plugin/results-panel work), streaming provider model, preview for Live Grep rows (file mode only), syntax colours/scrolling in the preview, and Resume re-runs the search instead of restoring cached matches (R3-4). |
| Command palette and command table | 206 core commands + plugin commands, contexts/availability, prefix modes, key hints | **implemented** core / **partial** plugin commands | `commands/` (206 generated + native rows), `internal/tui/palette/`; Fresh `docs/features/command-palette.md` | The generated core table is exact (206/206). The runtime palette has 217 rows: 206 generated from Fresh's table + 7 hand-written native rows (compare, markdown Contents, keys) + 4 plugin-management rows (`commands/palette_test.go`). Bundled plugin commands number 13 after round 1 (15 before), vs Fresh's ~180, so palettes differ by the missing plugins (the Bundled plugin feature set row, §3.3). |
| Menu bar, context menus, help, welcome | 8 menus, 103 static actions, dynamic theme submenu, mnemonics/underlined accelerators, context menus, help overlay, welcome screen, dashboard | **implemented** menus/help/welcome; dashboard **missing** (plugin) | `internal/tui/menubar/{menu_table_gen,menubar,dynamic}.go`, `keymap/menu.go` (`Map.MenuMnemonic`), `internal/tui/menu_mnemonic_test.go`, `internal/tui/menubar/mnemonic_test.go`, `internal/tui/context_menu.go`, `internal/tui/help.go`, `internal/tui/welcome.go`, `docs/ui.md` §6; Fresh `docs/internal/editor-ux-features.md`, `docs/features/dashboard.md` | Menus match Fresh's static table exactly (103 actions, 31 separators, 2 static submenus), and the dynamic "Copy with Formatting" submenu lists the 10 bundled themes (`menubar/dynamic.go`). Round 2 added the accelerators: underlined keymap-assigned letters and Alt+letter through ordinary keymap resolution, F10 for the bar, `editor.menu_bar_mnemonics` off removes both; dropdown rows carry no mnemonics (as in Fresh). Help's label says "gvim" instead of Fresh's version string. Residual: the dashboard plugin (backlog #14), not the menu system. |
| Status bar | configurable element lists (Fresh's defaults: left `{trust} {remote} {terminal_restart} {cursor} {diagnostics} {cursor_count} {messages}`; right `{read_only} {line_ending} {encoding} {language} {lsp} {warnings} {update} {palette}`), separator, per-element click actions, squeeze rule | **implemented** (R2; parsed-but-unpainted elements documented) | `internal/tui/frame/frame.go` `StatusRuns`/`ParseStatusElement`/`DefaultStatusLeft/Right`, `internal/tui/model.go` `statusView`/`statusRow`, `internal/tui/statusbar_elements_test.go`, `config/editoropts.go` status-element validation, `config/statusbar_test.go`, `docs/ui.md` §5; Fresh `config.rs` `StatusBarConfig` and `view/ui/status_bar.rs` | Round 2 replaced the hardcoded row with the configured lists (Fresh's defaults when unset, an explicitly empty list paints nothing), the configured separator, Fresh's squeeze/drop order, and the element semantics: trust, remote ("Local", not clickable), cursor + cursor:compact, diagnostics `[E:1 W:2 I:3]` with its blank slot, cursor_count, messages, read_only, line_ending/encoding/language pickers, lsp, terminal_restart (when the dock shell exited), palette. Parsed but painting nothing: `{filename}`, `{chord}`, `{clock}` and plugin tokens (R2-3); `{warnings}` and `{update}` are deliberately hidden (no warning-count domain, no updater). Plugin status items keep their anchors around messages/diagnostics/warnings/update/palette; the large-file chip is a gvim extension leading the right group. |
| Config layers and Settings UI | 4 layers (system/user/project/session), per-buffer overrides, config write from toggles, Settings UI (tree, widgets, layer switch, save), `init.ts`, config reload | **partial** (R3 landed the editable page) | `config/settings_schema.go` (53 settings, 8 sections), `config/patch.go` (`Edit`/`PatchBytes`/`ValidateEdits`), `config/config.go` `applyObject`, `internal/app/settingssvc.go`, `internal/tui/settings.go` + `internal/tui/ports.go` `SettingsPort`/`SettingsChange`, `docs/settings.md`, tests `config/patch_test.go`, `internal/app/settingssvc_test.go`, `internal/tui/settings_page_test.go` | Round 3 landed: an editable Settings page (filter, sections, bool/enum/int/string rows, dirty marker, Edit raw) over User (default) and Project (trusted-only) layers; validation through the real option tables plus the C11 layer rules; one comment/layout-preserving JSONC patch per save written atomically by the app; live apply of the display/editor/search/explorer/theme/keymap/icon set on the update loop, with an explicit restart set (`hot_exit`, `restore_previous_session`, auto-save/recovery, `virtual_space`, `scroll_offset`, default line ending/newline-on-save, `auto_read_only`, `mouse_capture`); `SettingsPort.Reload` reads a file changed on disk; the old read-only list remains the no-port fallback. Still open: structured-value sections (languages/LSP/formatters/plugins/keybindings/status_bar/env detectors/terminal/file_explorer/sidebar sections) are Edit-raw only, no session layer or per-buffer pins, no Fresh most-specific-layer targeting, no page undo/per-setting reset, no Reload command/hot reload of the restart set, no plugin settings/theme preview (R3-1); `gvim config show` still merges the project layer regardless of trust (R1-3). |
| Keymaps and keybinding editor | 5 presets (276/104/64/54/4 own bindings across 13 contexts), user `keybindings`, unbind/noop, chords, custom keybinding maps, keybinding editor | **partial** (R2 landed the data for the four resolvable contexts) | `keymap/*.json` (inheritance graph), `keymap/common_set_test.go` (own/in-effect counts), `keymap/menu.go`, `internal/keybindings/`, `internal/tui/keybind_editor.go`, `docs/keybindings.md`; Fresh `docs/configuration/keyboard.md`, `docs/features/keybinding-editor.md` | Round 2 rebuilt the presets on Fresh's inheritance graph with Fresh's own bindings for the four contexts gvim resolves (`global`, `normal`, `file_explorer`, `terminal`): own/in-effect default 198/196, macos 53/212, macos-gui 50/261, emacs 78/243, vscode 9/201. Missing keys were added (block select, line-end cursors, marks/bookmarks, LSP actions, shell, case/query-replace, scroll, tour/macro), gvim-specific keys kept where Fresh has none, conflicts resolved to Fresh's data; `docs/keybindings.md` rewritten. Residual: the eight contexts gvim resolves in code are not keymap-addressable and a user binding there is skipped with a warning (§4 item 5, R2-4). Custom `keybinding_maps` are refused with a warning; the editor itself is implemented. |
| Themes and appearance | 10 built-in themes, theme files by name/path/URL/`builtin://`, `extends` inheritance, attributes on every color, theme editor, inspect-at-cursor, plugin theme overrides, background images | **partial** | `theme/`, `theme/data/*.json`, `internal/tui/style/`, `internal/tui/view.go`; Fresh `docs/features/themes.md` | All 10 built-in names and RGB/object+modifier values load; no named/default colors, no `extends`/inheritance, no file/URL theme forms. No theme editor or inspect-at-cursor (`internal/tui/notice_na.go:15`), no plugin theme override API, background image/blend is a not-applicable notice. |

### 3.3 Services

| Area | Fresh features (evidence) | gvim status | gvim evidence | Notable sub-gaps |
|---|---|---|---|---|
| LSP | diagnostics, hover, definition/implementation/references, completion, rename, code actions + server-initiated file ops, signature help, format (external + range), folding ranges, inlay hints, semantic tokens, diagnostics panel, inline diagnostics, multi-server with only/except features, universal servers, per-server status popup/mute, process limits, env/init options, root markers, C header routing, auto-read-only skip, large-file skip | **partial**; the status pill states + `[E:1 W:2 I:3]` diagnostics badge are **implemented** (R1) | `lsp/`, `lsp/manager/`, `internal/lsphost/`, `internal/tui/lsp*.go`, `docs/lsp.md`; Fresh `docs/features/lsp.md` | The pill follows Fresh's priority order (`LSP`, `LSP …`, `LSP (off)`, `LSP (missing)`, `LSP (failed)`, `LSP (n/a)`) and the badge renders the diagnostics element's `E:1 W:2 I:3` form with its blank slot. Still open: code-action application is a notice (`internal/tui/lsp.go`); inlay hints not requested; no semantic tokens, no `foldingRange`, no diagnostics panel, no end-of-line inline diagnostics, no multi-server merge (`lsp` list entries use the first enabled server), `only_features/except_features/env/initialization_options/process_limits/language_id_overrides` accepted but ignored (`config/config.go`), `universal_lsp` ignored, status is a notice not a per-server popup, no per-language mute, no remote-aware spawning. Core request/response, root markers, `.h` routing and server-failure reporting are implemented. Backlog #7. |
| Plugins runtime and host API | TypeScript on QuickJS, sandbox-lite with per-plugin contexts, ~372-method API, overlays/virtual text/conceals/soft breaks/folds, prompts/toolbars, modes/contexts, windows/splits, virtual+composite buffers, buffer groups, terminals, authority/remote, LSP requests, HTTP, widgets/floating/dock panels/sidebar sections, i18n, package manager, menus, events | **partial, runtime different** | `plugin/`, `pluginapi/`, `internal/pluginhost/`, `internal/tui/plugin_*.go`, `docs/plugins/` (7 API pages, 1111-line `gvim.d.lua`); `docs/plugins/porting.md:44-48` lists what is not provided | Lua 5.1 is the permitted runtime difference. Capability gaps (from `porting.md`): widgets/floating panels, sidebar-section API (the round-1 native section is the first slice), windows, authority/orchestrator, i18n, menus, package manager, conceal, virtual lines, plugin folds, terminal API, render_line, HTTP, mouse/focus events, composite buffers/buffer groups, themes, project-local plugins. Decoration/virtual-buffer/prompt/key-mode/LSP/process/fs/timer surface exists. |
| Bundled plugin feature set | 50 plugins: git_log, live_diff, git_grep, git_explorer, git_blame, git_gutter, git_statusbar, git_find_file, search_replace, diff_nav, diagnostics_panel, code-tour, dashboard, welcome*, theme_editor, markdown_toc, markdown_compose, markdown_source, path_complete, lsp_help, lsp_navigation, find_references, compare_files, flash, vi_mode, merge_conflict, tab_actions, goto_with_selection, env-manager, devcontainer, k8s-workspace, agent_*/orchestrator/live_sessions, pkg, audit_mode, language helpers | **partial** | `plugin/bundled/plugins/` (9 after round 1), `docs/plugins/bundled.md` (including its reduced/not-ported notes: git_blame reduced, merge_conflict reduced to in-buffer resolution, vi_mode limits; markdown_toc was retired in round 1 into the native Contents section and is not a port target); Fresh `plugins/*.ts` | Round 1 retired markdown_toc, leaving 9: goto_with_selection, tab_actions, git_gutter (with diff-nav commands), git_statusbar, git_find_file, git_blame (reduced), merge_conflict (reduced), flash, vi_mode. Missing plugin features: git_log, live_diff, git_grep, git_explorer, search_replace (project), diagnostics_panel, code-tour, dashboard, theme_editor, markdown_compose, markdown_source, path_complete, lsp_help, lsp_navigation, env-manager, devcontainer, k8s-workspace, orchestrator/agent sessions, live_sessions, pkg, audit_mode, compare_files (built-in in gvim instead), welcome (built-in), language-helper plugins. |
| Terminal | PTY terminal panes and utility dock, live/scrollback modes per split, keyboard capture, clickable paths (OSC 7), restart exited process + agent resume, shell override, send selection, mouse forwarding/drag-select, close_on_exit, terminal tabs, ConPTY on Windows | **partial** (R3 landed slice A) | `config/terminal.go`, `internal/terminal/{scrollback,mouse,session,term}.go`, `internal/tui/dock/{scrollback,input,panel,render,ports}.go` + `dock_glue.go`, `internal/uiadapter/terminal.go`, `docs/terminal.md`, tests `internal/terminal/*_test.go`, `internal/tui/dock/*_test.go` | Round 3 landed: `terminal.shell` (command+args; project layer may not set it), `close_on_exit`, `mouse_forwarding` (`requested`/`alt_screen` with Shift bypass and the alternate-scroll rule), gvim-only `scrollback_lines`; a read-only scrollback pager (Ctrl+Space toggle with terminal_escape fallback, Esc, PgUp/PgDn/Home/End, `/` case-insensitive search with n/N, y copy row, Ctrl+C copy screen, title rows), OSC 7 working-directory tracking with Ctrl+Click path links (absolute/~ → OSC 7 cwd → workspace root, exists check in a tea.Cmd, off on the alternate screen), and mouse forwarding from the child's DECSET modes. Still open: drag-select/text selection (Shift+drag under a mouse-hungry program), reflow and persisted scrollback/session restore, agent resume and restart-into-same-transcript, terminal tabs, scrollback navigation for a dead shell, `jump_to_end_on_output`, child OSC 52/kitty keyboard, regex/case search options, hover underline and `file:line:col` links, Windows ConPTY (R3-2). |
| Git surfaces | review diff (stage/unstage/discard/comments/export), range/stash reviews, git log with live diff, git blame panel with history, live diff baselines, diff chunk nav, git grep, git explorer, git find file, statusbar branch, gutter marks | **partial** | `gitstate/`, `plugin/bundled/plugins/git_gutter`, `git_statusbar`, `git_find_file`, `git_blame`, `merge_conflict`, `internal/tui/git.go`; Fresh `docs/features/git.md` | Missing: Review Diff (/range/stash), Git Log, Live Diff, Git Grep, Git Explorer, blame panel/history stepping (gvim has a reduced inline blame), merge view (reduced to in-buffer resolution), diff hunk sources beyond the HEAD gutter, review comments/export. Implemented: gutter hunk marks + next/prev change, branch statusbar, git find file, merge-conflict resolution, compare-as-difftool. |
| Session, restore, daemon | session restore/save, hot exit of unsaved buffers, crash recovery, ephemeral-file patterns, per-file pins, `--restore`/`--no-restore`/`--simple`, daemon mode with attach/detach and `--cmd daemon open-file --wait`, direct-vs-daemon launch modes | **partial** (R2 landed hot exit, the restore settings and the CLI flags) | `internal/app/{session_plan,session_open,hotexit}.go`, `internal/app/run.go`, `internal/restore/session.go` (`RestoreHotExit`), `internal/cli/root.go`, session/ files, tests `session_plan_test.go`, `session_launch_test.go`, `restore/session_hot_test.go`, `internal/cli/root_test.go`, README sessions/flags; Fresh `docs/features/session-persistence.md` | Round 2 landed: `hot_exit` (quit without asking, the session save stashes the text; off asks per dirty tab), `restore_previous_session` (tabs/layout vs the unsaved text only), `skip_session_restore_when_files_passed` (file launch, or restore then append the given files), and the one-shot `--restore`/`--no-restore` (alias `--no-session`)/`--simple` with mutual-exclusion errors. Still open: daemon mode and its CLI, no per-buffer hot-exit store (a file launch cannot keep/restore its files' unsaved text), `--no-restore` still saves the session (deliberate; §4 item 10, R2-5), and `--simple`'s "no dock" is vacuous. |
| Workspaces, windows, orchestrator, agents | orchestration dock, multiple workspaces per daemon (one per git worktree), workspace switcher, New Workspace dialog, Run Agent (claude/codex/opencode/aider), agent session discovery/resume, live sessions, extract tab to workspace | **missing** | `grep -ri orchestrator --include=*.go` finds only command-table/capability strings; `internal/tui/remote.go` says remote workspaces unsupported; `extract_tab_to_new_workspace` is a notice; Fresh `docs/features/session-persistence.md`, `docs/features/scripting.md` | Whole area absent; gvim is one editor per process. Depends on daemon/workspace model. |
| Remote authority | SSH editing (scp/URL forms), devcontainers (CLI attach/rebuild/ports/logs), Kubernetes workspaces, remote-aware LSP/terminal/processes, reconnect, remote indicator popup | **missing** | No `ssh`/`authority` implementation in the tree; `internal/tui/remote.go:10` popup says "Remote workspaces are not supported"; `docs/plugins/porting.md:46` "authority ... not provided"; Fresh `docs/features/ssh.md`, `docs/features/devcontainer.md` | Entire area absent. Requires an authority abstraction under fs/proc/terminal/LSP first. |
| Workspace trust | per-folder Restricted/Trusted/Blocked, executable-marker detection, one-time prompt, status pill, dialog command, persisted per folder, gates LSP/build/env activation, env detectors | **partial** (R1 landed) | `trust/` (levels + Undecided, store at `config.Dir()/trust.json`, atomic writes, marker scan, Fresh startup policy), `internal/app/trust.go` and `internal/app/run.go` (`Options.Untrusted`, `lsp_enabled` forced off), `internal/tui/trust*.go` (dialog, port, notices, pill), `config/trust_test.go`, `docs/trust.md`; Fresh `docs/features/workspace-trust.md` | Implemented in round 1: the three levels plus Undecided, per-folder persistence, executable-marker scan and prompt (silent trust for plain folders, Fresh's policy), the open-time prompt, the `Workspace Trust…` palette command and dialog, the trust status pill with click routing, the project config layer skipped unless Trusted, and LSP auto-start forced off unless Trusted (manual Start/Restart still works). Still open: no process gate — terminal/shell/formatter/LSP/plugin spawns are not consulted against the level, and Blocked enforces the same as Restricted (`docs/trust.md` §3); env detection/activation and devcontainer/tasks are absent; the config gate is a startup decision (a mid-session trust change applies project tooling at the next start, documented divergence). Tracked as backlog #17. |
| Markdown | syntax highlighting, markdown_source smart list editing, compose/rendered/dual modes, conceal, tables, Contents sidebar section | **partial**; the native Contents section is **implemented** (R1) | `syntax/` markdown styling, `internal/tui/sidebar/contents.go`, `outline/`, `commands/markdown_commands.go`, `docs/sidebar.md`, `docs/ui.md`; Fresh `docs/features/editing.md` Markdown section, `refs/fresh/crates/fresh-editor/plugins/markdown_toc.ts` (retired in gvim) | Round 1 replaced the Lua virtual-buffer outline with the native sidebar Contents section and hand-written palette rows (Show Outline, Close Outline, Contents — Follow Cursor/Scroll); see the sidebar row and `docs/sidebar.md`. Still open: `markdown_source` (list continuation/checkbox/Tab bullet cycling), compose/rendered/dual view mode — **page view is stored only and belongs to this item**, not the display row — plus conceal/soft-break rendering and tables (backlog #12). |
| Diagnostics surfaces | gutter marks, underline, inline end-of-line text, diagnostics panel, quickfix list, error navigation, hover fusion | **partial** | `internal/tui/diag.go`, `internal/tui/lsp.go`, `internal/tui/editorpane/decor.go`, `docs/lsp.md`; Fresh `docs/features/lsp.md` | Gutter dots, underline, status count, F8 jumps and hover diagnostics exist. End-of-line inline text is parsed but unused; diagnostics panel/dock and quickfix list are missing. |
| Scripting and CLI automation | `--cmd script`, `--cmd daemon open-file --wait`, stdin programs, `init.ts` startup script, agent channels, watch mode, CLI help for the API | **partial** | `internal/cli/` (flags: config/keymap/theme/log-file/diff/no-plugins/plugin), `plugin_init.go` (`init.lua`); Fresh `docs/features/scripting.md` | gvim has `init.lua` and the Lua API, plus gvim-only `gvim config show/paths` and `gvim grammar` subcommands. Missing: Fresh's external `--cmd` script channel, `open-file --wait`, daemon modes, stdin piping, `--restore/--no-restore/--simple/--web`, watch/agent capability. |
| Internationalization | 15 locales, locale detection, config locale, plugin i18n (`t()`, locale JSON, translated menus/commands/prompts) | **missing** | English only: `internal/tui/view.go` select_locale notice; no locale data in tree; Fresh `docs/i18n.md`, locales dir 15 files | Whole area missing; menus/labels are hardcoded English. |
| Update, packaging, telemetry | update check, in-editor self-update prompt, `--no-upgrade-check`, telemetry toggle, packaging matrix (deb/rpm/aur/nix/npm/brew/appimage/flatpak/winget), daemon packaging | **missing / deliberately not planned?** | `internal/tui/notice_na.go:12-13` (update/update log not applicable); `internal/cli/` has no update flags; Fresh `README.md` | Decide with the Lead: these are distribution features rather than editor features for a pure-Go terminal binary. If in scope, the in-editor update indicator belongs to the status bar (`{update}`). |
| Platform input and OS integration | CSI-u/modifier disambiguation, AltGr/CapsLock quirks, key release, GPM mouse on Linux console, suspend, stdin streaming, ConPTY, OSC 52 clipboard, Window title | **partial** | `internal/terminal/` (x/vt), Bubble Tea v2 input, `internal/tui/clip.go` (OSC 52/HTML), `internal/tui/root_terminal.go` suspend; Fresh `docs/internal/terminal.md` | Keyboard protocol settings are parsed but the code uses Bubble Tea's input as-is; no GPM, no stdin streaming/piping, no Windows PTY, no focus/mouse events for plugins, no terminal mouse forwarding. Suspend and OSC 52 clipboard exist. |

## 4. Deliberately different (allowed)

1. **Plugin runtime.** Lua 5.1 on gopher-lua, not TypeScript on QuickJS. `.js`/`.ts` files are
   never run and are reported once (`docs/plugins/porting.md`, `docs/plugins/README.md`). The
   *language* difference is allowed; missing plugin capabilities are gaps (the Plugins matrix row, §3.3).
2. **TUI component style.** Rendering is a custom cell grid on Bubble Tea v2 and the chrome
   follows `docs/ui.md` (spacing scale, sheet/card/menu placements, one shared card frame), not
   Fresh's retained-mode `fresh-ui` tree. Layout/paint differences are allowed; missing
   *interactions and modalities* (menus, palette, popups) are not.
3. **Go + Bubble Tea v2, pure-Go binary.** `tools/purity_check.sh` and import rules replace
   Fresh's Rust/QuickJS architecture; performance strategies (chroma vs syntect, Myers vs
   patience diff) may differ.
4. **Compare presentation.** gvim opens the two files in their own tabs marked `⇄`
   (`docs/compare.md`); Fresh uses one composite-buffer tab. Both sides remain editable and
   diff-equivalent.
5. **Keymap contexts are narrowed to four.** gvim resolves `global`, `normal`,
   `file_explorer` and `terminal`; since round 2 the five presets are Fresh's own binding data for
   exactly those contexts, with Fresh's inheritance graph. Fresh's other contexts (`prompt`,
   `searchPrompt`, `popup`, `completion`, `settings`, `menu`, `compositeBuffer`, `dock`) are not
   emitted: gvim's overlays answer their keys in code, and the loader skips a user binding there
   with a warning (`docs/keybindings.md`). This is the remaining deliberate narrowing; closing it
   is backlog R2-4.
6. **Project config is deliberately weaker, and trust-gated.** The project `.gvim/config.json`
   cannot set `keymap`, `keybindings` or an LSP `command` (C11; `config/config.go` package doc and
   `setProjectLSP`), and since round 1 the whole project layer is skipped unless the workspace is
   Trusted (`config.Options.Untrusted`). Trusting a folder opens its configuration, not the
   ability to choose programs: the C11 rules still hold under Trusted. Fresh merges the project
   config at any level and relies on workspace trust for process execution instead.
7. **Trust store and decision timing.** gvim keeps one JSON store, `<config.Dir()>/trust.json`,
   for every workspace, while Fresh keeps per-project files under its data directory. gvim's
   config-layer gate is a startup decision: a workspace trusted mid-session applies its project
   config at the next start (the pill and the next workspace load change immediately); Fresh
   gates live spawns instead. Both differences, and the reason, are recorded in `docs/trust.md`.
8. **Not applicable to a terminal cell grid.** Wave animation, background image/blend and
   inspect-theme-at-cursor are documented as not applicable (`internal/tui/notice_na.go`,
   `internal/tui/view.go` `backgroundNotice`). Fresh's GUI/desktop builds, browser `--web` UI and
   WASM build are outside a terminal-only pure-Go binary; this is a port decision the Lead
   should confirm rather than a silent omission (see §5 P2).
9. **Style-only display options are parsed but never consumed.** `editor.animations`,
   `cursor_jump_animation`, `viewport_edge_fade` and `screensaver_enabled`/`screensaver_idle_minutes`
   load without diagnostics for Fresh config compatibility but are not carried into the TUI: the
   cell-grid component style (`docs/ui.md`) draws no animations, fading edges or screensaver.
   Round 2 made this an explicit decision and removed the inert plumbing from `ViewOptions`.
10. **Session semantics differ where gvim has no daemon and one session file.** A file launch
    (`gvim FILE…`) restores and saves no session and cannot bring the launched files' own
    hot-exit text back, where Fresh does; `--no-restore`/`restore_previous_session=false` still
    saves the session at quit (only the restore is skipped), because gvim's session file is also
    its hot-exit store and skipping the save would lose in-progress work; and `--simple`'s
    "no dock" is vacuous because gvim has no dock. README documents all three; round 2 delivers
    them deliberately.

11. **Settings write path and layer targeting.** `config` is a leaf package: it owns the
    schema, validation through the real option tables and `PatchBytes` (a surgical hujson edit
    that keeps comments, layout and unknown keys); `internal/app/settingssvc.go` owns the atomic
    file write and the reload. The page has an explicit layer target (User by default, Project
    only when the workspace is Trusted) instead of Fresh's "write to the most specific layer
    that already defines the setting", and it has no session layer. Structured settings
    (languages, LSP, formatters, plugins, keybindings, status-bar lists) are Edit-raw only.
    `docs/settings.md` §2–3.
12. **Settings live apply runs on the update loop.** The editor belongs to the TUI goroutine, so
    the service pushes the editor-side live values (language-settings resolver, formatters,
    rulers, ephemeral patterns) through `SettingsPort.Apply` on that loop; Save and Reload are
    tea.Cmd file IO only. A named restart set (`hot_exit`, `restore_previous_session`,
    auto-save/recovery, `virtual_space`, `scroll_offset`, line-ending/newline-on-save,
    `auto_read_only`, `mouse_capture`) applies at the next start — Fresh applies most settings
    live. `docs/settings.md` §5.
13. **Explorer side is session-only.** `toggle_file_explorer_side` flips the side for the session;
    Fresh persists the choice to the user config. The config file still decides the next launch
    until the Settings page covers `file_explorer.side` (R3-1/R3-3). `docs/sidebar.md` §9.
14. **`terminal.scrollback_lines` is a gvim-only extension.** It is not a Fresh key; the other
    Fresh `terminal` keys (`jump_to_end_on_output`, `resume_agents`, `mouse_drag_selects`,
    `pass_ctrl_v_without_text`, `skip_app_execution_alias`) are accepted and ignored until their
    features land. `docs/terminal.md` Config.
15. **Ctrl+Space is the scrollback switch and the escape fallback.** gvim's terminal context keeps
    Fresh's `terminal_escape` binding on Ctrl+Space; the dock takes the key first and enters
    scrollback when the shell has history, otherwise it falls through to the keymap's escape.
    Ctrl+] always exits (gvim addition). Recorded in `docs/terminal.md` and the keymap comment.

## 5. Ranked backlog for the remaining rounds

Sizes are estimates: S ≈ a day or less, M ≈ 1–3 days, L ≈ 3–10 days, XL ≈ more (unvalidated).
The order follows the goal's dependency shape: user-visible TUI/editing gaps first, then the
plugin-API breadth that unblocks Fresh's plugin-era features, then the daemon/remote/agent
architecture. "Owner area" names the package that should own the work.

Rounds 1–3 closed the workspace-trust surface, native sidebar sections/Contents and the LSP
pill + diagnostics badge (round 1); virtual space (`on`), inline diagnostics, Fresh's keymap
data, the config-driven status bar + menu accelerators, and the session settings/CLI (round 2);
the editable Settings page, terminal slice A, the explorer options + sticky parents, and the
palette preview + Live Grep toggles (round 3). Those are no longer backlog items (their numbers
are retired ids: §5.1 now holds #7, #8, #11 and #12). The ranked items below are the remaining
gaps, with the gaps the rounds opened listed first as R1-*, R2-* and R3-*.

Round 1 recorded four gaps (R1-1…R1-4; the spawn gate is also ranked as #17):

- **R1-1 (P1, M)** — per-heading fold ranges for the Contents `fold_buffer` setting: the
  setting is read and plumbed but does nothing until the editor accepts fold ranges from
  outside the document (`editor` fold-range port, `internal/tui/sidebar`; `docs/sidebar.md` §6).
- **R1-2 (P2, S)** — persist sidebar section state in the session (section list/order, Contents
  visibility and follow mode, heights; `internal/tui/sidebar`, `internal/restore`, `session`).
- **R1-3 (P2, S)** — `gvim config show` still merges the project config layer regardless of
  trust; gate it by the trust level or label the layers (`internal/configview`, `config`;
  `docs/trust.md` §3 "Config viewer").
- **R1-4 (P1, L)** — per-spawn executable-path gate and Blocked spawn denial
  (terminal/shell/formatter/LSP/plugin processes); tracked as ranked item #17
  (`trust/` and the spawn sites; `docs/trust.md` §3).

Round 2 recorded five gaps:

- **R2-1 (P1, M)** — virtual space `block` mode and vertical virtual space: gvim implements the
  horizontal `on` subset; `block` (rectangular selection geometry, block copy/paste past EOL) is
  accepted but clamped, and Fresh's vertical virtual lines below the last line are not
  implemented (`editor`/`document`/`editorpane`; `docs/internal/virtual-space-scoping.md`).
- **R2-2 (P2, S)** — version-aware inline-diagnostic staleness: the port carries no diagnostic
  version, so inline text is always the latest server reply and is never dimmed; Fresh dims
  stale versions (`internal/tui/editorpane/inline_diag.go`, `internal/tui/ports.go`).
- **R2-3 (P2, S)** — status-bar elements parsed but unpainted: `{filename}`, `{chord}`,
  `{clock}` and plugin tokens (`{plugin:token}` needs a register/paint API); `{warnings}` and
  `{update}` stay hidden deliberately (no warning-count domain, no updater) — `docs/ui.md`.
- **R2-4 (P2, M)** — the eight code-resolved keymap contexts: expose Fresh's
  prompt/searchPrompt/popup/completion/settings/menu/compositeBuffer/dock contexts to user
  bindings once the overlays take keymap bindings; today such bindings are skipped with a
  warning (`docs/keybindings.md`).
- **R2-5 (P2, M)** — per-buffer hot-exit store and file-launch unsaved-text restore: one
  session file means a file launch cannot keep its files' unsaved text and `--no-restore` must
  still save (§4 item 10); a per-buffer store would close this.

Round 3 recorded four gaps:

- **R3-1 (P1, M)** — Settings page coverage: the structured sections (languages, LSP,
  formatters, plugins, keybindings, status-bar element lists, env detectors, terminal,
  file_explorer and sidebar sections) are Edit-raw only; no session layer, no per-buffer pins,
  no Fresh most-specific-layer targeting, no undo or per-setting reset, no plugin settings or
  theme preview, and no Reload command/keybinding for the live set (`SettingsPort.Reload` exists
  but is unbound; the restart set cannot hot-reload). `docs/settings.md` §6.
- **R3-2 (P1, L)** — terminal slice B: drag-select/text selection (including Shift+drag under a
  mouse-hungry program), reflow + persisted scrollback/session restore, agent resume and
  restart-into-the-same-transcript, terminal tabs, scrollback navigation for a dead shell,
  `jump_to_end_on_output`, child OSC 52/kitty keyboard, regex/case scrollback search, hover
  underline and `file:line:col` links, Windows ConPTY (`docs/terminal.md` Deferred).
- **R3-3 (P2, M)** — explorer residuals: `respect_gitignore`, `custom_ignore_patterns`,
  `preview_tabs` and the tree-indicator glyph keys are accepted-ignored; the side toggle is
  session-only (Fresh writes the user config); per-segment compact-row clicks/context menus;
  plugin explorer rows/decorations/slots (with #13). `docs/sidebar.md` §9.
- **R3-4 (P2, M)** — finder/palette residuals: quickfix export, terminals/workspaces providers,
  streaming provider model, preview for Live Grep rows (file mode only today), syntax colours
  or scrolling in the file preview, and Resume re-runs the search instead of restoring cached
  matches. `docs/ui.md` palette details.

### 5.1 User-visible TUI and editing gaps (do first)

| # | Gap | Pri | Size | Owner area | Notes / depends on |
|---|---|---|---|---|---|
| 7 | LSP user-visible completion: code-action apply + workspace edits, inlay hints, semantic tokens, `foldingRange`, diagnostics panel + end-of-line inline diagnostics, multi-server merge, per-server status popup/mute, `universal_lsp`/feature filters/env/init options/process limits | P1 | L | `lsp/`, `internal/lsphost`, `internal/tui/lsp*.go`, `config` | the pill states, diagnostics badge and status-bar element wiring landed in rounds 1–2 |
| 8 | Project-wide Search & Replace + Git Grep (results panel, replace across files with confirm, Git Grep provider; match-tree/scoped replace/streaming deferred) | P1 | M | `search/`, `internal/tui`, `internal/app`, bundled plugin | Phase-1 spec with the Lead; reuses finder index + editor multi-file edit flow |
| 11 | Theme model/editor: `extends`/inheritance, named colors, file/URL/`builtin://` forms, theme editor + inspect-at-cursor, plugin theme overrides | P1 | M | `theme/`, `internal/tui`, `internal/configview` | #13 for plugin overrides |
| 12 | Markdown: compose/rendered/dual view (including page view), `markdown_source` smart editing, conceal/soft breaks, tables | P1 | L | `editor`, `syntax`, `internal/tui`, `plugin/bundled` | #13; page view belongs to this item |

### 5.2 Plugin-API breadth (unblocks Fresh's plugin-era features)

| # | Gap | Pri | Size | Owner area | Notes / depends on |
|---|---|---|---|---|---|
| 13 | Plugin host capability tier 1: public sidebar-section API (generalise the landed native section stack), full plugin overlay/virtual-text surface, dock/floating panels, composite buffers/buffer groups, plugin i18n/menu hooks, window/authority seams | P1 | L | `pluginapi`, `plugin`, `internal/tui/plugin_*.go` | the round-1 native section is the first consumer |
| 14 | Port the missing bundled plugins on tier 1: git_log, live_diff, git_grep, git_explorer, search_replace, diagnostics_panel, code-tour, dashboard, theme_editor, path_complete, lsp_help/lsp_navigation, markdown_compose/source (where not in #12); `markdown_toc` is retired in round 1 and is not a port target | P1 | L each; XL total | `plugin/bundled/plugins`, `plugin/bundled/lib` | #13 |
| 15 | Compare/diff plugin API: `compareBuffers`, `replaceRange`, buffer groups, scroll-sync groups, so bundled diff/compare plugins are portable | P1 | M | `pluginapi`, `plugin`, `diff/` | #13 |
| 16 | Plugin package manager/registry (Fresh `pkg`: install/update/remove from git, registry sync, consent dialog) | P2 | L | `plugin/`, `shellcmd` | #13 |
| 17 | Workspace-trust enforcement + env: per-spawn gate for in-workspace executables (terminal/shell/formatter/LSP/plugin processes; Blocked denies all spawns), env detection/activation (.venv, direnv, mise, pipenv, poetry) and the env-manager plugin | P1 | L | `trust/`, spawn sites in `editor`/`internal/lsphost`/`internal/terminal`/`plugin/hostos`, new env package, `internal/app` | round-1 trust model; `docs/trust.md` §3 |

### 5.3 Daemon, workspaces, remote, agents (architecture)

| # | Gap | Pri | Size | Owner area | Notes / depends on |
|---|---|---|---|---|---|
| 18 | Daemon mode: client/server split, attach/detach, `open-file`/`--wait`, auto-attach, socket lifecycle, direct-vs-daemon launch modes | P1 | XL | new daemon package, `internal/cli`, `internal/app` | session settings landed in round 2; daemon build starts here |
| 19 | Workspaces/windows: workspace model, dock switcher, New Workspace, extract-tab-to-workspace, buffer groups | P1 | XL | new workspace model, `internal/tui` | #18 |
| 20 | Agent sessions: Run Agent dialog, agent resume metadata, session discovery/resume, dock integration | P1 | L | workspace/terminal/plugin packages | #18/#19 |
| 21 | Remote authority: SSH file/process/terminal/LSP backend, devcontainer attach, Kubernetes workspaces, remote indicator, reconnect | P1 | XL | new authority package; `plugin/hostos` | #13, #18 |

### 5.4 Polish and optional (P2)

| # | Gap | Pri | Size | Owner area | Notes / depends on |
|---|---|---|---|---|---|
| 22 | i18n: message catalogue, locale detection/config, the 15 Fresh locales, plugin `t()` and translated menus/commands/prompts | P1 | L infra; XL translation | new i18n package, `internal/tui`, `commands/gen` | needed for full parity but costly; can follow the first rounds |
| 23 | Terminal Windows support (ConPTY) | P2 | M | `internal/terminal` | — |
| 24 | Update check/self-update/telemetry: decide scope with the Lead; if in scope, status-bar `{update}` indicator + `--no-upgrade-check` | P2 | S/M | `internal/app`, `internal/cli`, status bar | Lead decision |
| 25 | Platform edges: GPM mouse on the Linux console, stdin streaming/piping, key-release/AltGr/CapsLock edge cases, plugin focus events | P2 | M | `internal/terminal`, input layer, `internal/cli` | — |
| 26 | Fresh GUI/desktop, browser `--web` UI, WASM build, packaging matrix, daemon packaging | P2 (confirm out of scope) | XL | n/a | Lead decision |

### 5.5 Suggested round order for the remaining goal

1. **Round 4 — LSP routing/edits and project search (the two Phase-1 specs).** #7 LSP 4a
   (multi-server/universal router + feature filters + env/init/limits config; full code-action
   apply with resolve/executeCommand/applyEdit incl. resource ops and version checks; per-server
   status popup/mute; inline-diagnostic staleness) as the L, and #8 project-wide Search &
   Replace + Git Grep (minimal results panel + replace-across-files with confirm) as the M.
2. **Round 5 — LSP views and settings coverage.** #7 LSP 4b (semantic tokens, `foldingRange` +
   external fold source, inlay hints tier 1, diagnostics sidebar panel, pull diagnostics) and
   R3-1 settings structured editors (languages/LSP first, since the LSP round needs them).
3. **Round 6 — plugin API tier 1.** #13 host capability tier, #15 diff/compare plugin API, then
   start #14 with git_log, live_diff, git_grep and diagnostics_panel.
4. **Round 7 — plugin-era features and polish.** Finish #14 (search_replace, code-tour,
   dashboard, theme_editor, path_complete, lsp helpers), #12 markdown compose/source (and page
   view), #11 theme model/editor, R3-2 terminal slice B, R1-1 Contents fold ranges (the LSP
   folding source makes it cheap).
5. **Round 8 — daemon, workspaces, agents, remote.** #18 daemon mode, #19 workspaces/windows,
   #20 agent sessions, #21 remote authority, #17 trust process gate + env detection; keep direct
   mode and `--no-plugins` working throughout.
6. **Round 9 — remaining polish/optional.** #22 i18n, #23 Windows ConPTY, #24 update/telemetry
   decision, #25 platform input edges, #26 scope decision, then the R1-2/R1-3, R2-1…R2-5 and
   R3-3/R3-4 sweep; a final pass re-reads `refs/fresh` and refreshes this file.

## 6. Tracking notes

- **Round 1 landed (2026-11-08).** The tree is green (build/vet/lint/tests per the Lead's round
  gate); the rows read **partial (R1 landed)** where a Fresh area is only partly closed:
  - **Workspace trust** (`docs/trust.md`): levels + Undecided, per-folder store at
    `config.Dir()/trust.json`, marker scan and Fresh's startup policy, open-time prompt,
    `Workspace Trust…` command + dialog, status pill, project config layer skipped unless
    Trusted, LSP auto-start forced off unless Trusted. Residuals R1-4/#17 (spawn gate, env) and
    R1-3 (`gvim config show`).
  - **Sidebar Contents** (`docs/sidebar.md`): native section stack + Markdown heading tree,
    palette rows from `commands/markdown_commands.go`; Lua `markdown_toc` retired (bundled 9,
    7 on by default; runtime palette 217). Residuals R1-1, R1-2.
  - **Status bar** (`docs/lsp.md`): LSP pill states + `[E:1 W:2 I:3]` badge and trust pill;
    round 2 closed the config-driven element-list item.
- **Round 2 landed (2026-11-08).** All four tasks complete; this audit ran the gates and
  reviewed the diff (no scope creep; §7):
  - **Display**: virtual space `on` subset and end-of-line inline diagnostics (no version
    staleness), style-only keys left out of `ViewOptions` (§4 item 9); page view stayed under
    #12. Residuals R2-1, R2-2.
  - **Keymap**: five presets on Fresh's inheritance graph with Fresh's data for the four
    resolvable contexts (default 198/196, macos 53/212, macos-gui 50/261, emacs 78/243,
    vscode 9/201; `keymap/common_set_test.go`); `docs/keybindings.md` rewritten. Residual R2-4.
  - **Status bar + menus**: config-driven element lists, Fresh defaults/separator/squeeze
    (`frame.StatusRuns`), menu accelerators via `keymap.MenuMnemonic`; `docs/ui.md` §5–6; the
    dynamic "Copy with Formatting" submenu was already theme-filled (round-1 correction).
    Residual R2-3.
  - **Session + CLI**: `planSession` with `hot_exit`/`restore_previous_session`/
    `skip_session_restore_when_files_passed`, `--restore`/`--no-restore`/`--no-session`/
    `--simple`, `RestoreHotExit`, README sessions/flags; tests `session_plan_test.go`,
    `session_launch_test.go`, `restore/session_hot_test.go`. Residual R2-5 and §4 item 10.
- **Round 3 landed (2026-11-08).** All four tracks complete; this audit ran the gates and
  reviewed the diff (no scope creep; §7):
  - **Settings page** (`docs/settings.md`): `config/settings_schema.go` (53 scalar settings, 8
    sections), `config/patch.go` (`PatchBytes` keeps comments/layout/unknown keys;
    `ValidateEdits` through the real option tables + C11 rules), `internal/app/settingssvc.go`
    (Snapshot/Validate/Save/Reload/Apply, Trusted-only project layer, atomicfile write),
    `internal/tui/settings.go` (filter, sections, editors, dirty marker, layer switch, Edit raw,
    discard confirmation) and `SettingsPort`; live apply on the update loop + restart notice.
    Residual R3-1; deliberate differences §4 items 11–12.
  - **Terminal slice A** (`docs/terminal.md`): `config/terminal.go` (`shell`, `close_on_exit`,
    `scrollback_lines`, `mouse_forwarding`), `internal/terminal/{scrollback,mouse}.go` + session
    callbacks (history, OSC 7 cwd, alt-screen, mouse modes, `SendMouse`),
    `internal/tui/dock/{scrollback,input}.go` (pager/search/copy, link clicks, forwarding),
    starter options in `internal/uiadapter/terminal.go`. Residual R3-2; §4 items 14–15.
  - **Explorer options** (`docs/sidebar.md` §9): `config/fileexplorer.go` + `explorer/options.go`
    (width, side, follow, auto-open, compact), `explorer/sticky.go`, `internal/app/explorer.go`,
    `internal/tui/root_explorer.go` (reveal/follow/auto-open), side geometry in
    `internal/tui/frame/frame.go`, panel compact/sticky/grip. Residual R3-3; §4 item 13.
  - **Finder/palette** (`docs/ui.md` palette details): `finder/finder.go` `EnumerateAll` +
    hidden/ignored options, `internal/app/{preview,live_grep}.go`, `internal/tui/palette/`
    preview column (`path:line:col`) and Live Grep toolbar (case/regex/whole; files/buffers/
    ignored; counts/errors; buffer snapshots). Residual R3-4.
- The gaps the rounds opened are at the top of §5 (R1-1…R1-4, R2-1…R2-5, R3-1…R3-4); §5.5 is
  the round-4 plan built from the ranked backlog minus the closed items.
- The command palette table is generated from Fresh; keep `commands/gen/generate.py` and
  `tools/gen_menus.py` in the parity loop. Regenerating after a Fresh update is the cheapest way
  to find newly added commands/menus; `tools/gen_menus.py --check` reports drift.
- When a Fresh release lands, re-run the counts in §2 (commands, keymaps, themes, plugins,
  plugin API) and update the matrix rows for the areas that changed.

## 7. Verification limits and risks

- Statuses are read evidence from the tree at the time of writing. Rounds 2 and 3 had their full
  gate sets run by this audit on their trees, both green: go build ./..., CGO_ENABLED=0 go build
  ./..., go vet, golangci-lint 0 issues, purity, import rules, go test ./... -count=1. Each diff
  was reviewed; round-1 statuses were verified by reading code and docs only. A feature that
  exists but is broken would still read "implemented": treat a status as a pointer, not proof.
- Fresh is a moving reference checkout. Counts are pinned to the `refs/fresh` state read on
  2026-11-08; 413 e2e files and ~180 plugin command registrations are indexes, not exact feature
  counts.
- Keymap counts are from the preset files and the inheritance graph
  (`keymap/common_set_test.go`); what remains partial is the eight contexts gvim resolves in
  code (§4 item 5, R2-4), not the data.
- Display options that are parsed but deliberately unconsumed are listed in §4 item 9 (the
  style-only keys); the status bar and the session keys are consumed as of rounds 1–2. Do not
  infer that other unlisted options are wired.
- Fresh features that depend on its GUI/web/WASM builds or its update/install infrastructure are
  listed but flagged as a scope decision (§4 item 8, backlog #26).
- The repo's dispatch gap lists are close to empty (`unhandledActions` has one bound action,
  `unhandledPaletteActions` none). Do not read that as parity: a handler can dispatch and
  answer with a reduced-feature notice, and plugin-era features are whole commands that do not
  exist here yet.
- The round-1 refresh re-read the trust/sidebar/status-pill code, the round-2 refresh the
  display/keymap/status/session code, and the round-3 refresh the settings/terminal/explorer/
  palette code; each ran the gates. The design records carry the detail and name the tests;
  this file points rather than proves.
- Round-2/3 diff reviews (this audit): no scope creep outside the four tasks of each round. The
  round-2 cross-owner helper (`keymap/menu.go`) and the round-3 shared files (`internal/tui/
  model.go`, `view.go`, `frame/frame.go`, `ports.go`) stay inside the declared write scopes; the
  generated tables, `refs/` and `_attic/` were not touched. Two nits, not bugs: `internal/app/
  live_grep.go` lost the `"\r"` in its `strings.TrimRight` when the buffer/file paths were
  refactored, so CRLF lines keep a trailing carriage return in Live Grep row text (restore the
  argument); and `internal/app/settingssvc.go` holds its mutex across the save's file IO, so
  page reads/validation from the update loop can briefly wait on disk while a save is in
  flight (release the lock around the write in a follow-up). Round-2's earlier notes still
  stand: `--restore --diff` is accepted and silently ineffective, and `hotExitEditor` is a
  documented temporary wrapper. The Lead's in-progress fixes to `docs/plugins/limits.md` and
  the `keymap/default.json` Ctrl+Space comment were ignored in this review.
- See also the formatting nit from round 2: newly appended sections in `docs/ui.md` and
  `docs/sidebar.md` put an ATX heading directly after a list item without a blank line;
  Markdown renders it, but the style is inconsistent with the rest of the docs.
- The `migration/` docs, including GAP-v3's numbers, are historical (§1.4). Do not plan from
  them.

## 8. gvim-only additions (not parity requirements)

`gvim config show`/`config paths`, `gvim grammar`, the in-process `uitest` harness, the
`cmd/gvimdebug` PTY-served debug page, the screenshot sweep, and the config/UI-tree dump commands
are gvim tooling. They do not count toward Fresh parity, but they are the fastest way to verify
new feature work; keep them working.






