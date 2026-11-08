# Fresh parity record

Status: live working record for the "full Fresh feature parity" goal, first written 2026-11-08 and
refreshed on the same date after the round-1 features landed (workspace trust, sidebar Contents,
status pills) on top of `588f4d7`. Every status below was checked in this tree or in
`refs/fresh`, not inferred from the old migration notes. Update the affected row when a round
lands instead of rewriting it.

Last verified: 2026-11-08, tree commit `588f4d7` plus the round-1 work (the tree is green
per the Lead's round gate). Fresh reference: the `refs/fresh` checkout as read on the same date.

**Round 1 outcome (2026-11-08):** workspace trust (levels/store/prompt/dialog/palette command/
config-layer + LSP-autostart gating/status pill), native sidebar sections with the Markdown
Contents section (the Lua `markdown_toc` plugin is retired: bundled plugins 9, 7 enabled by
default; runtime palette 217 rows), and the status bar's trust pill, LSP pill states and
`[E:1 W:2 I:3]` diagnostics badge. Residual gaps round 1 recorded are in §5.5.

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
| Menus / menu actions | 8 / 107 | 8 / 103 | generated from `translated_menus()`; gap is the dynamic "Copy with Formatting" submenu (empty) and its theme rows. |
| Keymap presets | 5 | 5 | same names: default, macos, macos-gui, emacs, vscode. Binding data is reduced; see the Keymaps row in §3.2. |
| Bundled plugins | 50 | 9 (10 before round 1) | 7 on by default (8 before round 1); `flash` and `vi_mode` off; round 1 retired the Lua `markdown_toc` plugin and folded the feature into the native Contents section (`docs/plugins/bundled.md` documents nine plugins). |
| Bundled plugin commands | ~180 | 13 after round 1 (15 before) | from `registerCommand(` / `gvim.command.register` counts; round 1 removed `markdown_toc`'s 2 registrations. |
| Themes | 10 built-ins | 10 built-ins | same names (`theme/data/` mirrors `fresh-editor-core/themes/`). |
| LSP server entries | built-in + plugin helpers | 62 | `lsp/manager/defaults.go`; gvim's table covers Fresh's documented table (`docs/lsp.md`). |
| Plugin API surface | ~372 methods | ~172 functions | `fresh.d.ts` vs `docs/plugins/gvim.d.lua`; runtime language differs by design, capability gaps do not. |
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
| Display & view options | line numbers/relative, wrap + wrap_column + wrap indent, page view, rulers, indent guides + rainbow, whitespace marks incl. newlines/CR, current line/column, occurrence, post-EOF tilde, scrollbars + markers, smooth scroll, animations, screensaver | **partial** | `internal/tui/view.go` (`ViewOptions`), `internal/tui/view_render.go`, `editor/viewsettings.go`, `config/editoropts.go`, tests `view_*_test.go`; Fresh `docs/features/editing.md` | Page view is parsed and stored but not drawn (`document/viewsettings.go:18`); virtual space is stored only (`document/viewsettings.go:16`); `diagnostics_inline_text`, `animations`, `cursor_jump_animation`, `viewport_edge_fade`, `screensaver_*` are parsed/plumbed but have no consumer; mouse hover is not built (`internal/tui/view.go` toggle notice); scroll sync skips compare views. |
| Folding | indent folds + LSP `foldingRange`, fold indicators, per-buffer toggle | **partial** | `editor/fold.go`, `document/fold.go`, `internal/tui/editorpane` gutter; Fresh `docs/features/editing.md` | No LSP folding ranges (grep for `foldingRange` returns nothing); indent folding, gutter arrows, toggle and per-buffer override are implemented. |
| Splits, tabs, panes, windows | splits (vertical/horizontal), tab strip, tab drag, preview tabs, buffer groups, multiple windows/workspaces | **partial** | `internal/tui/split*`, `internal/tui/tabs.go`, `internal/tui/tab_drag.go`, `internal/tui/tab_drop.go`, preview tabs in `internal/tui/model.go`; Fresh `docs/features/navigation.md` | No buffer groups, no second window/workspace model; "Move tab to other split" and "Extract Tab to New Workspace" are notices (`internal/tui/tab_drag.go:45`, `internal/tui/root_fix.go:40`); `next_window`/`prev_window` map to pane cycling (`internal/tui/split_tabs.go`). |
| Compare files | `fresh --diff A B`, editable side-by-side, change copy/take, next/prev difference, switch side, scrollbar marks | **implemented** | `internal/tui/compare*.go`, `docs/compare.md`, `compare_*_test.go`, `internal/cli` `--diff`; Fresh `docs/features/compare-files.md` | Presentation differs deliberately (two file tabs, §4). Fresh's plugin API `compareBuffers`/`replaceRange` for plugins is missing; large file mode is refused on both sides. |

### 3.2 Project, chrome and settings

| Area | Fresh features (evidence) | gvim status | gvim evidence | Notable sub-gaps |
|---|---|---|---|---|
| File explorer and sidebar | tree, file create/delete/rename/copy/cut/paste, hidden/gitignored toggles, git status marks, context menu, async reload, width/side, follow active buffer, compact chains, sticky parents, plugin rows/decorations, sidebar sections | **partial** (R1 landed) | `explorer/`, `internal/tui/explorer/`, `internal/tui/sidebar/` (sections + Contents widget), `outline/` (Markdown heading scan), `internal/tui/root_sidebar.go`, `commands/markdown_commands.go`, `docs/sidebar.md`, `docs/ui.md`; Fresh `docs/features/file-explorer.md`, `docs/internal/sidebar-sections-design.md` | Round 1 landed native sidebar sections: shared section borders, focus cycle, and the Markdown Contents section (`internal/tui/sidebar`, `outline/`, `docs/sidebar.md`), with palette rows Show Outline, Close Outline and Contents — Follow Cursor/Scroll as hand-written native commands (`commands/markdown_commands.go`). The Lua `markdown_toc` plugin is retired (deleted; bundled set 10 → 9, commands 15 → 13). Still open: follow-active-buffer, compact chains, sticky parents, reveal feedback, auto-open-on-last-close; explorer width/side fixed (`toggle_file_explorer_side` is a notice; `sidebar` config accepted and ignored); plugin explorer decorations/slots; Contents residual gaps per `docs/sidebar.md` §6 and §5.5 (per-heading fold ranges, section state persistence). |
| Finder, quick open, live grep | Ctrl+P modes (file/command/line/buffer), fuzzy ranking, streaming enumeration, finder preview, live grep with provider toggles (case/regex/word/files/buffers/ignored/terminals/workspaces), quickfix export, resume | **partial** | `finder/`, `fuzzy/`, `internal/tui/palette/` (4 modes, Live Grep); Fresh `docs/features/command-palette.md` | No file-content preview in the palette; Live Grep has no Fresh toggle set and no quickfix export/workspace scope (search string only). Resume Live Grep is implemented (`internal/app/live_grep.go`). |
| Command palette and command table | 206 core commands + plugin commands, contexts/availability, prefix modes, key hints | **implemented** core / **partial** plugin commands | `commands/` (206 generated + native rows), `internal/tui/palette/`; Fresh `docs/features/command-palette.md` | The generated core table is exact (206/206). The runtime palette has 217 rows: 206 generated from Fresh's table + 7 hand-written native rows (compare, markdown Contents, keys) + 4 plugin-management rows (`commands/palette_test.go`). Bundled plugin commands number 13 after round 1 (15 before), vs Fresh's ~180, so palettes differ by the missing plugins (the Bundled plugin feature set row, §3.3). |
| Menu bar, context menus, help, welcome | 8 menus, 107 actions, dynamic theme submenu, mnemonics/underlined accelerators, context menus, help overlay, welcome screen, dashboard | **partial** | `internal/tui/menubar/menu_table_gen.go`, `internal/tui/context_menu.go`, `internal/tui/help.go`, `internal/tui/welcome.go`; Fresh `docs/internal/editor-ux-features.md`, `docs/features/dashboard.md` | "Copy with Formatting" submenu is empty (`internal/tui/menubar/menu_table_gen.go:40`; Fresh fills it from installed themes at runtime). Menus have no mnemonics/F10/Alt+letter by design of this port (`docs/keybindings.md`). Dashboard plugin is missing; gvim has its own welcome/tour instead. |
| Status bar | configurable element lists (`{trust} {remote} {terminal_restart} {cursor} {diagnostics} {cursor_count} {messages}` left; `{read_only} {line_ending} {encoding} {language} {lsp} {warnings} {update} {palette}` right), per-element click actions, LSP pill with count badge | **partial**; trust pill, LSP pill states and `[E:1 W:2 I:3]` diagnostics badge **implemented** (R1) | `internal/tui/frame/frame.go` `StatusRuns`, `internal/tui/model.go` `statusView`, `internal/tui/lsp_detect.go` `lspPill`, `internal/tui/trust_state.go`, `internal/tui/statusbar_trust_test.go`, `docs/lsp.md`, `docs/trust.md`; Fresh `view/ui/status_bar.rs` (`Diagnostics` renders `E:1 W:2 I:3`, `Warnings` renders `[⚠ N]`) | The row is still hardcoded (trust pill, `Local`, position, plugin items, message; right: large, `[RO]`, EOL, encoding, language, LSP, plugin items, diagnostics badge, palette hint); `editor.status_bar` element lists are parsed and plumbed but not applied. Missing `{terminal_restart}`, `{cursor_count}` and `{update}`; the general `{warnings}` element is not painted (gvim's general warnings are the notices Show Warnings lists); `Local` stands in for Fresh's `{remote}`. Backlog #3. |
| Config layers and Settings UI | 4 layers (system/user/project/session), per-buffer overrides, config write from toggles, Settings UI (tree, widgets, layer switch, save), `init.ts`, config reload | **partial** (R1 added the trust gate on the project layer) | `config/config.go`, `config/editoropts.go`, `config/language.go`, `internal/app/settings.go`, `internal/tui/settings.go`, `docs/trust.md`; Fresh `docs/configuration/index.md` | Layers: user + project only; no session layer, no per-buffer pin persistence (toggles are per-buffer session state), no config-write path from the UI. Round 1: the project layer is skipped entirely unless the workspace is Trusted (`config.Options.Untrusted`), which is stricter than Fresh, which merges project config at any level (see `docs/trust.md`). Settings UI is a read-only list that opens `config.json` in a tab; no config reload (`docs/plugins/limits.md`). `gvim config show` still merges the project layer regardless of trust (R1-3 in §5.5). Many Fresh `editor.*` options are parsed; `hot_exit` and `restore_previous_session` are parsed but not consumed. Fresh-only top-level keys are accepted and ignored (`config/editoropts.go`). |
| Keymaps and keybinding editor | 5 presets, 276 default bindings across 13 contexts, user `keybindings`, unbind/noop, chords, custom keybinding maps, keybinding editor | **partial** | `keymap/*.json`, `internal/keybindings/`, `internal/tui/keybind_editor.go`; Fresh `docs/configuration/keyboard.md`, `docs/features/keybinding-editor.md` | Presets are hand-curated and much smaller: default 109 bindings / 74 actions vs Fresh 276 / 251 in the data files (Fresh's maps are larger and cover prompt/popup/menu/settings/completion contexts; gvim resolves overlay keys in code, `docs/keybindings.md`). Missing user-facing bindings include block select, line-end cursors, bookmark keys, LSP code actions/completion/implementation, shell commands, case transforms, expand-line-selection. Custom `keybinding_maps` are refused with a warning; the editor itself is implemented. |
| Themes and appearance | 10 built-in themes, theme files by name/path/URL/`builtin://`, `extends` inheritance, attributes on every color, theme editor, inspect-at-cursor, plugin theme overrides, background images | **partial** | `theme/`, `theme/data/*.json`, `internal/tui/style/`, `internal/tui/view.go`; Fresh `docs/features/themes.md` | All 10 built-in names and RGB/object+modifier values load; no named/default colors, no `extends`/inheritance, no file/URL theme forms. No theme editor or inspect-at-cursor (`internal/tui/notice_na.go:15`), no plugin theme override API, background image/blend is a not-applicable notice. |

### 3.3 Services

| Area | Fresh features (evidence) | gvim status | gvim evidence | Notable sub-gaps |
|---|---|---|---|---|
| LSP | diagnostics, hover, definition/implementation/references, completion, rename, code actions + server-initiated file ops, signature help, format (external + range), folding ranges, inlay hints, semantic tokens, diagnostics panel, inline diagnostics, multi-server with only/except features, universal servers, per-server status popup/mute, process limits, env/init options, root markers, C header routing, auto-read-only skip, large-file skip | **partial**; the status pill states + `[E:1 W:2 I:3]` diagnostics badge are **implemented** (R1) | `lsp/`, `lsp/manager/`, `internal/lsphost/`, `internal/tui/lsp*.go`, `docs/lsp.md`; Fresh `docs/features/lsp.md` | The pill follows Fresh's priority order (`LSP`, `LSP …`, `LSP (off)`, `LSP (missing)`, `LSP (failed)`, `LSP (n/a)`) and the badge renders the diagnostics element's `E:1 W:2 I:3` form with its blank slot. Still open: code-action application is a notice (`internal/tui/lsp.go`); inlay hints not requested; no semantic tokens, no `foldingRange`, no diagnostics panel, no end-of-line inline diagnostics, no multi-server merge (`lsp` list entries use the first enabled server), `only_features/except_features/env/initialization_options/process_limits/language_id_overrides` accepted but ignored (`config/config.go`), `universal_lsp` ignored, status is a notice not a per-server popup, no per-language mute, no remote-aware spawning. Core request/response, root markers, `.h` routing and server-failure reporting are implemented. Backlog #7. |
| Plugins runtime and host API | TypeScript on QuickJS, sandbox-lite with per-plugin contexts, ~372-method API, overlays/virtual text/conceals/soft breaks/folds, prompts/toolbars, modes/contexts, windows/splits, virtual+composite buffers, buffer groups, terminals, authority/remote, LSP requests, HTTP, widgets/floating/dock panels/sidebar sections, i18n, package manager, menus, events | **partial, runtime different** | `plugin/`, `pluginapi/`, `internal/pluginhost/`, `internal/tui/plugin_*.go`, `docs/plugins/` (7 API pages, 1111-line `gvim.d.lua`); `docs/plugins/porting.md:44-48` lists what is not provided | Lua 5.1 is the permitted runtime difference. Capability gaps (from `porting.md`): widgets/floating panels, sidebar-section API (the round-1 native section is the first slice), windows, authority/orchestrator, i18n, menus, package manager, conceal, virtual lines, plugin folds, terminal API, render_line, HTTP, mouse/focus events, composite buffers/buffer groups, themes, project-local plugins. Decoration/virtual-buffer/prompt/key-mode/LSP/process/fs/timer surface exists. |
| Bundled plugin feature set | 50 plugins: git_log, live_diff, git_grep, git_explorer, git_blame, git_gutter, git_statusbar, git_find_file, search_replace, diff_nav, diagnostics_panel, code-tour, dashboard, welcome*, theme_editor, markdown_toc, markdown_compose, markdown_source, path_complete, lsp_help, lsp_navigation, find_references, compare_files, flash, vi_mode, merge_conflict, tab_actions, goto_with_selection, env-manager, devcontainer, k8s-workspace, agent_*/orchestrator/live_sessions, pkg, audit_mode, language helpers | **partial** | `plugin/bundled/plugins/` (9 after round 1), `docs/plugins/bundled.md` (including its reduced/not-ported notes: git_blame reduced, merge_conflict reduced to in-buffer resolution, vi_mode limits; markdown_toc was retired in round 1 into the native Contents section and is not a port target); Fresh `plugins/*.ts` | Round 1 retired markdown_toc, leaving 9: goto_with_selection, tab_actions, git_gutter (with diff-nav commands), git_statusbar, git_find_file, git_blame (reduced), merge_conflict (reduced), flash, vi_mode. Missing plugin features: git_log, live_diff, git_grep, git_explorer, search_replace (project), diagnostics_panel, code-tour, dashboard, theme_editor, markdown_compose, markdown_source, path_complete, lsp_help, lsp_navigation, env-manager, devcontainer, k8s-workspace, orchestrator/agent sessions, live_sessions, pkg, audit_mode, compare_files (built-in in gvim instead), welcome (built-in), language-helper plugins. |
| Terminal | PTY terminal panes and utility dock, live/scrollback modes per split, keyboard capture, clickable paths (OSC 7), restart exited process + agent resume, shell override, send selection, mouse forwarding/drag-select, close_on_exit, terminal tabs, ConPTY on Windows | **partial** | `internal/terminal/`, `internal/tui/dock/`, `internal/tui/dock_*.go`, tests `dock_*_test.go`; Fresh `docs/features/terminal.md`, `docs/internal/terminal.md` | No scrollback model (`internal/terminal/term.go:13`), so no read-only scrollback mode, no terminal search/copy from history, no clickable paths/OSC 7 cwd tracking, no mouse forwarding/drag-select. No `terminal.shell` config (shell is `$SHELL`, `internal/uiadapter/terminal.go:42`), no agent-resume, no `close_on_exit`, no terminal tabs inside the dock. Unix only: Windows returns `ErrUnsupported`. Keyboard capture, restart, resize, selection copy, send-selection and split terminals exist. |
| Git surfaces | review diff (stage/unstage/discard/comments/export), range/stash reviews, git log with live diff, git blame panel with history, live diff baselines, diff chunk nav, git grep, git explorer, git find file, statusbar branch, gutter marks | **partial** | `gitstate/`, `plugin/bundled/plugins/git_gutter`, `git_statusbar`, `git_find_file`, `git_blame`, `merge_conflict`, `internal/tui/git.go`; Fresh `docs/features/git.md` | Missing: Review Diff (/range/stash), Git Log, Live Diff, Git Grep, Git Explorer, blame panel/history stepping (gvim has a reduced inline blame), merge view (reduced to in-buffer resolution), diff hunk sources beyond the HEAD gutter, review comments/export. Implemented: gutter hunk marks + next/prev change, branch statusbar, git find file, merge-conflict resolution, compare-as-difftool. |
| Session, restore, daemon | session restore/save, hot exit of unsaved buffers, crash recovery, ephemeral-file patterns, per-file pins, daemon mode with attach/detach and `--cmd daemon open-file --wait`, direct-vs-daemon launch modes | **partial** | `internal/restore/`, `session/`, `document/stash.go`, `editor/interrupted.go`, `internal/app/run.go`, tests `session_restore_test.go`, `files_recovery_test.go`; Fresh `docs/features/session-persistence.md` | Session save/restore, crash recovery and hot-exit stash exist. Missing: daemon mode (task "detach" is a documented not-applicable reason), `--restore`/`--no-restore`/`--simple` flags, daemon `open-file`/`--wait`, `hot_exit` and `restore_previous_session` settings are parsed but ignored (`internal/app/run.go` restores whenever no file arguments are given). |
| Workspaces, windows, orchestrator, agents | orchestration dock, multiple workspaces per daemon (one per git worktree), workspace switcher, New Workspace dialog, Run Agent (claude/codex/opencode/aider), agent session discovery/resume, live sessions, extract tab to workspace | **missing** | `grep -ri orchestrator --include=*.go` finds only command-table/capability strings; `internal/tui/remote.go` says remote workspaces unsupported; `extract_tab_to_new_workspace` is a notice; Fresh `docs/features/session-persistence.md`, `docs/features/scripting.md` | Whole area absent; gvim is one editor per process. Depends on daemon/workspace model. |
| Remote authority | SSH editing (scp/URL forms), devcontainers (CLI attach/rebuild/ports/logs), Kubernetes workspaces, remote-aware LSP/terminal/processes, reconnect, remote indicator popup | **missing** | No `ssh`/`authority` implementation in the tree; `internal/tui/remote.go:10` popup says "Remote workspaces are not supported"; `docs/plugins/porting.md:46` "authority ... not provided"; Fresh `docs/features/ssh.md`, `docs/features/devcontainer.md` | Entire area absent. Requires an authority abstraction under fs/proc/terminal/LSP first. |
| Workspace trust | per-folder Restricted/Trusted/Blocked, executable-marker detection, one-time prompt, status pill, dialog command, persisted per folder, gates LSP/build/env activation, env detectors | **partial** (R1 landed) | `trust/` (levels + Undecided, store at `config.Dir()/trust.json`, atomic writes, marker scan, Fresh startup policy), `internal/app/trust.go` and `internal/app/run.go` (`Options.Untrusted`, `lsp_enabled` forced off), `internal/tui/trust*.go` (dialog, port, notices, pill), `config/trust_test.go`, `docs/trust.md`; Fresh `docs/features/workspace-trust.md` | Implemented in round 1: the three levels plus Undecided, per-folder persistence, executable-marker scan and prompt (silent trust for plain folders, Fresh's policy), the open-time prompt, the `Workspace Trust…` palette command and dialog, the trust status pill with click routing, the project config layer skipped unless Trusted, and LSP auto-start forced off unless Trusted (manual Start/Restart still works). Still open: no process gate — terminal/shell/formatter/LSP/plugin spawns are not consulted against the level, and Blocked enforces the same as Restricted (`docs/trust.md` §3); env detection/activation and devcontainer/tasks are absent; the config gate is a startup decision (a mid-session trust change applies project tooling at the next start, documented divergence). Tracked as backlog #17. |
| Markdown | syntax highlighting, markdown_source smart list editing, compose/rendered/dual modes, conceal, tables, Contents sidebar section | **partial**; the native Contents section is **implemented** (R1) | `syntax/` markdown styling, `internal/tui/sidebar/contents.go`, `outline/`, `commands/markdown_commands.go`, `docs/sidebar.md`, `docs/ui.md`; Fresh `docs/features/editing.md` Markdown section, `refs/fresh/crates/fresh-editor/plugins/markdown_toc.ts` (retired in gvim) | Round 1 replaced the Lua virtual-buffer outline with the native sidebar Contents section and hand-written palette rows (Show Outline, Close Outline, Contents — Follow Cursor/Scroll); see the sidebar row and `docs/sidebar.md`. Still open: markdown_source (list continuation/checkbox/Tab bullet cycling), compose/rendered/dual view mode (page view is stored only), conceal/soft-break rendering, and tables (backlog #12). |
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
5. **Overlay keys are code-resolved.** Prompts, popups, menus, the find bar and the settings
   list answer their keys in code, so a user binding in those `when` contexts is skipped with a
   warning (`docs/keybindings.md`). Fresh exposes those as first-class keymap contexts. This is
   a deliberate port simplification, but it is why the keymap row is partial, not implemented.
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

## 5. Ranked backlog for the remaining rounds

Sizes are estimates: S ≈ a day or less, M ≈ 1–3 days, L ≈ 3–10 days, XL ≈ more (unvalidated).
The order follows the goal's dependency shape: user-visible TUI/editing gaps first, then the
plugin-API breadth that unblocks Fresh's plugin-era features, then the daemon/remote/agent
architecture. "Owner area" names the package that should own the work.

Round 1 closed the workspace-trust surface, native sidebar sections/Contents and the LSP pill
states + diagnostics badge; those are no longer backlog items. The ranked items below are the
remaining gaps, with the round-1 gaps listed as R1-1…R1-4 before them.

Round 1 opened four gaps to rank with the items below (they are new, so they carry R1 ids
instead of a rank; ranked item #17 already covers the spawn gate):

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

### 5.1 User-visible TUI and editing gaps (do first)

| # | Gap | Pri | Size | Owner area | Notes / depends on |
|---|---|---|---|---|---|
| 1 | Display/view: draw page view, honour virtual space, render end-of-line inline diagnostics; decide animations/cursor-jump/viewport-edge-fade/screensaver (implement or delete the parsed keys) | P0 | M | `document/viewsettings.go`, `internal/tui/view_render.go`, `editor` | parsed-but-inert keys documented in the Display & view matrix row (§3.1) |
| 2 | Keymap data parity: port the user-facing Fresh bindings gvim lacks (block select, add-cursors-to-line-ends, bookmark set/jump/list, LSP actions, shell commands, case transforms, expand-line-selection, query replace, delete-to-line-end, scroll actions, tour/macro keys) across all five presets | P0 | M | `keymap/*.json`, `commands/gen` if regenerated | the dead-action lists (`internal/uiadapter/dead_bindings_test.go`: `unhandledActions` = 1, `unhandledPaletteActions` = 0) are the dispatch floor, not the feature gap; pair each key with its owner feature |
| 3 | Status bar + menus: apply `editor.status_bar` element lists (the trust/LSP/diagnostics elements landed in round 1 but are still hardcoded), add the general `{warnings}` element and `{terminal_restart}`/`{cursor_count}`/`{update}`, add menu mnemonics (F10/Alt+letter/underlines), and fill the dynamic "Copy with Formatting" submenu from the built-in themes | P0 | S/M | `internal/tui/frame/frame.go`, `internal/tui/view.go`, `internal/tui/menubar`, `tools/gen_menus.py` | — |
| 4 | Settings UI editing + save + reload (the Settings list becomes an editor over the config schema: layer switch, write user/project file, reload config and plugins without restart) | P0 | M | `internal/tui/settings.go`, `config`, `internal/app` | `docs/plugins/limits.md` known limitation |
| 5 | Session settings + CLI: honour `hot_exit`, `restore_previous_session`, `skip_session_restore_when_files_passed`; add `--restore`/`--no-restore`/`--simple` | P0 | S | `internal/app/run.go`, `internal/cli`, `config` | — |
| 6 | Terminal: scrollback/read-only mode + terminal search/copy, clickable paths + OSC 7, `terminal.shell` config, mouse forwarding/drag-select, agent resume | P0 | L | `internal/terminal`, `internal/tui/dock`, `internal/app` | Fresh `docs/features/terminal.md`, `docs/internal/terminal.md` |
| 7 | LSP user-visible completion: code-action apply + workspace edits, inlay hints, semantic tokens, `foldingRange`, diagnostics panel + end-of-line inline diagnostics, multi-server merge, per-server status popup/mute, `universal_lsp`/feature filters/env/init options/process limits (the pill states and diagnostics badge are landed) | P1 | L | `lsp/`, `internal/lsphost`, `internal/tui/lsp*.go`, `config` | — |
| 8 | Project-wide Search & Replace + Git Grep (results panel, replace across files, provider toggles) | P1 | M | `search/`, `internal/tui`, bundled plugin | minimal results panel; plugin version depends on #13 |
| 9 | Finder/palette: file-content preview, Live Grep toggles (case/regex/word/files/buffers/ignored/terminals/workspaces), quickfix export, streaming provider model | P1 | M | `internal/tui/palette`, `finder`, `internal/app` | — |
| 10 | Explorer parity: follow active buffer, compact chains, sticky parents, reveal feedback, auto-open/close policy, sidebar width/side config, plugin explorer decorations/slots | P1 | M | `explorer/`, `internal/tui/explorer`, `internal/tui/root_sidebar.go` | #13 for plugin slots |
| 11 | Theme model/editor: `extends`/inheritance, named colors, file/URL/`builtin://` forms, theme editor + inspect-at-cursor, plugin theme overrides | P1 | M | `theme/`, `internal/tui`, `internal/configview` | #13 for plugin overrides |
| 12 | Markdown: compose/rendered/dual view, `markdown_source` smart editing, conceal/soft breaks, tables | P1 | L | `editor`, `syntax`, `internal/tui`, `plugin/bundled` | #1 and #13 |

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
| 18 | Daemon mode: client/server split, attach/detach, `open-file`/`--wait`, auto-attach, socket lifecycle, direct-vs-daemon launch modes | P1 | XL | new daemon package, `internal/cli`, `internal/app` | #5 |
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

1. **Round 2 — visible editing/view sweep.** #1 display/view, #2 keymap data, #3 status bar/menus,
   #4 settings UI, #5 session flags, #6 terminal scrollback/shell/paths, R1-3 `gvim config show`.
   Exit: every TUI surface gvim advertises behaves as Fresh does, and every parsed-but-inert
   setting is implemented or removed from the schema and docs.
2. **Round 3 — LSP completion and project search.** #7 LSP user-visible set, #8 project-wide
   search + git grep with a minimal results panel, #9 finder/palette preview and Live Grep toggles.
3. **Round 4 — plugin API tier 1.** #13 host capability tier, #15 diff/compare plugin API, then
   start #14 with git_log, live_diff, git_grep and diagnostics_panel.
4. **Round 5 — plugin-era feature set.** Finish #14 (search_replace, code-tour, dashboard,
   theme_editor, path_complete, lsp_help/lsp_navigation), #12 markdown compose/source, #10
   explorer parity, #11 theme model/editor, R1-1 Contents fold ranges.
5. **Round 6 — daemon and automation.** #18 daemon mode and its CLI/automation surface, then
   begin #19 workspaces/windows.
6. **Round 7 — workspaces, agents, remote.** Finish #19, #20 agent sessions, #21 remote
   authority, #17 trust process gate and env detection/activation.
7. **Round 8 — polish/optional.** #22 i18n, #23 Windows ConPTY, #24 update/telemetry decision,
   #25 platform input edges, #26 scope decision, R1-2 sidebar state persistence; a final sweep
   re-reads `refs/fresh` and refreshes this file. Keep direct mode and `--no-plugins` working
   throughout the daemon/remote rounds.


## 6. Tracking notes

- **Round 1 landed (2026-11-08).** The tree is green (build/vet/lint/tests per the Lead's round
  gate) and the round-1 features are in place; the rows read **partial (R1 landed)** where a
  Fresh area is only partly closed:
  - **Workspace trust** (`docs/trust.md`): levels + Undecided, per-folder store at
    `config.Dir()/trust.json` written through `atomicfile`, marker scan and Fresh's startup
    policy, open-time prompt, `Workspace Trust…` palette command + dialog, status pill (first
    left element, warning role for Restricted/Blocked, click opens the dialog), project config
    layer skipped unless Trusted, LSP auto-start forced off unless Trusted (manual start still
    works). Residual: per-spawn executable-path gate and env activation (ranked #17, recorded
    R1-4), `gvim config show` still merges the project layer (R1-3).
  - **Sidebar Contents** (`docs/sidebar.md`): native sidebar section stack (shared borders,
    focus cycle, frame/hit geometry) with the Markdown heading tree; palette rows Show/Close
    Outline and Contents — Follow Cursor/Scroll are hand-written native commands
    (`commands/markdown_commands.go`). The Lua `markdown_toc` plugin is retired (deleted;
    bundled plugins 9 with 7 enabled by default; runtime palette 217 rows). Residual:
    per-heading fold ranges for `fold_buffer` (R1-1), section state not persisted (R1-2).
  - **Status bar** (`docs/lsp.md`): the LSP pill state set and the `[E:1 W:2 I:3]` diagnostics
    badge with its blank slot, plus the trust pill. Residual: config-driven element lists, the
    general `{warnings}` element, `{terminal_restart}`, `{cursor_count}`, `{update}` (ranked #3).
- The gaps round 1 opened are listed at the top of §5 (R1-1…R1-4); §5.5 is the round-2 plan built from the ranked backlog
  minus the closed items.
- The command palette table is generated from Fresh; keep `commands/gen/generate.py` and
  `tools/gen_menus.py` in the parity loop. Regenerating after a Fresh update is the cheapest way
  to find newly added commands/menus; `tools/gen_menus.py --check` reports drift.
- When a Fresh release lands, re-run the counts in §2 (commands, keymaps, themes, plugins,
  plugin API) and update the matrix rows for the areas that changed.

## 7. Verification limits and risks

- All statuses are static-read evidence from the tree at the time of writing, including the
  round-1 work, which the Lead's round gate reports green (build/vet/lint/tests). This file's
  statuses were re-checked by reading the code and its design docs, not by re-running the gates,
  so a feature that exists but is broken would still read "implemented". Treat a status as a
  pointer to re-check, not a proof.
- Fresh is a moving reference checkout. Counts are pinned to the `refs/fresh` state read on
  2026-11-08; 413 e2e files and ~180 plugin command registrations are indexes, not exact feature
  counts.
- Keymap numbers compare raw data files; Fresh's presets inherit from one another and gvim
  resolves overlay keys in code, so effective user-visible coverage is closer than the raw
  counts suggest but still partial (the Keymaps matrix row).
- Some `editor.*` options are parsed to make Fresh configs load; this document only calls out
  options whose consumption was checked (page view, virtual space, inline diagnostics,
  animations/screensaver, status bar, `hot_exit`, `restore_previous_session`). Do not infer that
  unlisted options are wired.
- Fresh features that depend on its GUI/web/WASM builds or its update/install infrastructure are
  listed but flagged as a scope decision (§4 item 8, backlog #26).
- The repo's dispatch gap lists are close to empty (`unhandledActions` has one bound action,
  `unhandledPaletteActions` none). Do not read that as parity: a handler can dispatch and
  answer with a reduced-feature notice, and plugin-era features are whole commands that do not
  exist here yet.
- The round-1 refresh (same day) re-read the landed trust, sidebar and status-pill code.
  Their design records (`docs/trust.md`, `docs/sidebar.md`, `docs/lsp.md`) carry the detail and
  name the tests; this file still points rather than proves.
- The `migration/` docs, including GAP-v3's numbers, are historical (§1.4). Do not plan from
  them.

## 8. gvim-only additions (not parity requirements)

`gvim config show`/`config paths`, `gvim grammar`, the in-process `uitest` harness, the
`cmd/gvimdebug` PTY-served debug page, the screenshot sweep, and the config/UI-tree dump commands
are gvim tooling. They do not count toward Fresh parity, but they are the fastest way to verify
new feature work; keep them working.





