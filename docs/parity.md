# Fresh parity record

Status: live working record for the "full Fresh feature parity" goal, first written 2026-11-08 and
refreshed after rounds 1–5 landed (round 1: workspace trust, sidebar Contents, status pills;
round 2: virtual space, keymaps, status bar/menus, sessions; round 3: settings page, terminal
scrollback/links/mouse, explorer options, palette preview/toggles; round 4: LSP router/edits,
project-wide search/replace + Git Grep, settings coverage, virtual-space block/vertical, markdown
smart editing; round 5: LSP 4b data rendering, plugin API tier 1a, theme model/editor, terminal
slice B, project-search residuals, status-bar elements). Every status below was checked in this
tree or in `refs/fresh`, not inferred from the old migration notes. Update the affected row when a
round lands instead of rewriting it.

Last verified: 2026-11-08, tree commit `b373639` plus the uncommitted round-5 work. Round 5's full
gate set was run by this audit (go build ./..., CGO_ENABLED=0 go build ./..., go vet,
golangci-lint 0 issues, gofmt clean, purity, import rules, go test ./... -count=1: all green, 67
packages). Fresh reference: the `refs/fresh` checkout as read on the same date.

**Round 1 outcome (2026-11-08):** workspace trust (levels/store/prompt/dialog/palette command/
config-layer + LSP-autostart gating/status pill), native sidebar sections with the Markdown
Contents section (the Lua `markdown_toc` plugin is retired: bundled plugins 9, 7 enabled by
default; runtime palette 217 rows at the time), and the status bar's trust pill, LSP pill states
and `[E:1 W:2 I:3]` diagnostics badge. Residual gaps round 1 recorded are R1-1…R1-4 in §5.

**Round 2 outcome (2026-11-08):** virtual space (the `on` subset) and end-of-line inline
diagnostics; the five keymaps rebuilt on Fresh's inheritance graph with Fresh's bindings for the
four contexts gvim resolves (default 198 own/196 in effect, macos 53/212, macos-gui 50/261,
emacs 78/243, vscode 9/201); config-driven status bar element lists (Fresh's defaults,
separator, squeeze rule) plus menu accelerators (`editor.menu_bar_mnemonics`); and session
settings/CLI (`hot_exit`, `restore_previous_session`, `skip_session_restore_when_files_passed`,
`--restore`/`--no-restore`/`--no-session`/`--simple`). Residuals were R2-1…R2-5; R2-1/R2-2
closed in round 4 and R2-3 in round 5 (§5).

**Round 3 outcome (2026-11-08):** the editable Settings page (53 scalar settings in 8 sections,
User/Project layers, comment-preserving JSONC patch writes, live apply plus an explicit restart
set) — `docs/settings.md`; terminal slice A (`terminal.shell`, `close_on_exit`,
`scrollback_lines`, `mouse_forwarding`, a read-only scrollback pager with search/copy, OSC 7 +
Ctrl+Click links, mouse forwarding) — `docs/terminal.md`; explorer parity (`file_explorer` width/
side/follow/auto-open/compact options, sticky parents, reveal) — `docs/sidebar.md` §9; and the
finder/palette slice (file-preview column, Live Grep match modes and files/buffers/ignored
scopes, match counts) — `docs/ui.md`. Residuals were R3-1…R3-4; R3-1 is closed in round 4, R3-2 in
round 5 (§5).

**Round 4 outcome (2026-11-08):** LSP 4a — multi-server/universal router with only/except
filters, capability intersection and dynamic registration; typed code actions with
resolve/executeCommand/applyEdit (version checks, resource ops), three-way apply; per-server
status popup/mute; inline-diagnostic version staleness — `docs/lsp.md`. Project-wide Search &
Replace + Git Grep — a native results panel and ports, replace across files through the shared
multi-file apply helper, and a git grep provider — `docs/keybindings.md`, `docs/ui.md`. Settings
coverage R3-1 — structured editors for languages/formatters/status-bar elements/file-explorer
options/terminal shell, per-key and structured reset, and Reload Configuration —
`docs/settings.md`. Virtual space R2-1 — true-rectangle block selections past EOL and vertical
virtual lines below the last line. Markdown-source smart editing — list continuation/numbering/
checkboxes, Tab/Shift+Tab bullet cycling and indentation, auto-close handling —
`editor/markdown.go`. Residuals are R4-1…R4-5 in §5.

**Round 5 outcome (2026-11-08).** Six tracks landed, the LSP one partially:
**LSP 4b (partial, increments A–D).** Inlay hints drawn at the end of their line as faint virtual
text (tier-1 placement; toggle command), semantic tokens painted as an overlay through the
existing syntax theme keys (full document + delta, 20-line padding), LSP `foldingRange` merged into
the document's fold set with the indentation folds (external fold-source API), and pull diagnostics
(`textDocument/diagnostic` when no server published, cached per version, no double reporting).
Inlay hints were confirmed against a real gopls session. Increment E, the diagnostics sidebar panel/quickfix, is deferred; task-22 is still in progress.
**Plugin API tier 1a.** Declarative widget panels (v1 kinds row/col/label/text/raw/hintBar/spacer/
divider/button/toggle/list/tree, focus/key/mouse routing, actions/mutations, caps) in the new
`internal/tui/pluginpanel` sub-package, floating/buffer panels, public sidebar sections, plugin
i18n, menu-bar rows, blocking action popups + result event, prompt suggestions, mouse/focus and
LSP events, all-files diagnostics, theme overrides; `gvim.api_version` is 2 (191 functions in
`docs/plugins/gvim.d.lua`). **Theme model/editor.** `theme/` registry with Fresh's forms/extends/
fallbacks/named colors/hot reload, `docs/themes.md`, inspect-at-cursor, native editor with
save-as/delete and plugin overrides; the R4-3 settings mutex fix landed. **Terminal slice B.**
Selection/copy, `jump_to_end_on_output`/`mouse_drag_selects`, regex/case scrollback search,
dead-shell scrollback, restart-into-transcript and `terminal.json` persistence (read-only, no
re-run, no reflow) — R3-2 closed. **Project-search R4-2.** Match-tree checkboxes, Replace Scoped,
file globs, Alt+Q list export and 20-deep search history landed (streaming etc. deferred).
**Status bar R2-3.** `{filename}`, `{chord}` and `{clock}` paint now (R2-3 closed).

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

Round-4 evidence adds `lsp/manager/router.go` + `lsp/capabilities.go` (feature routing),
`internal/lsphost/lsp.go` workspace edits, `internal/uiadapter/lsp_workspace.go`,
`internal/tui/{workspace_edit,project_search,lsp.go}` + `internal/tui/searchpanel/`,
`internal/app/{projectsearch,gitgrep}.go`, `search/plan.go`, `config/settings_structured.go`,
`document/{block,virtualspace}.go` + `internal/tui/editorpane/virtual_{block,lines}.go`,
`editor/markdown.go`, and the updated records (`docs/lsp.md`, `docs/settings.md`,
`docs/keybindings.md`, `docs/ui.md`).

Round-5 evidence adds `docs/themes.md`, `docs/lsp.md`, `docs/terminal.md`, `docs/ui.md` and
`docs/keybindings.md` updates, the plugin API docs (`docs/plugins/api-*.md`, `events.md`,
`gvim.d.lua`) and the new code: `internal/tui/pluginpanel/` + `plugin_panels.go`/
`plugin_menus.go`/`plugin_actionpopup.go`, `theme/{keys_table,registry,resolve,themefile,
overrides}.go`, `internal/tui/theme_{inspect,editor}.go`, `internal/tui/dock/{selection,
transcript}.go` + `internal/restore/terminal.go`, `internal/tui/lsp_data.go` +
`editor/lsp_folds.go`, `internal/lsphost/lsp_data.go`, `internal/tui/searchpanel/glob.go`,
`internal/tui/status_elements.go` and the round-5 plugin/theme/terminal/search tests.

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
| Palette commands (runtime) | 206 core (Fresh's `CommandDef` table) | 222 | 206 generated from Fresh's `CommandDef` table + 7 hand-written native rows (compare, markdown Contents, keys) + 4 plugin-management rows + 3 round-4 rows (Search and Replace in Project, Git Grep, Reload Configuration) + 2 round-5 rows (Edit Theme, Project Search Results in List); `commands/palette_test.go` pins 222. All have a handler or a documented reason (`unhandledPaletteActions` is empty, `internal/uiadapter/dead_bindings_test.go`); the bound-action gap list (`unhandledActions`) holds only `file_explorer_search_backspace`. |
| Documented "not applicable" commands | n/a | 4 | `internal/tui/notice_na.go` (detach, update, update log, wave). Inspect Theme now runs (round 5), and the round-1 trust command runs. |
| Menus / menu actions | 8 / 103 static actions, 31 separators, 2 static submenus + 1 dynamic | 8 / 103 static actions, 31 separators, 2 static submenus + the dynamic theme submenu | Static parity is exact against Fresh's `translated_menus()` (103 actions / 31 separators / 2 static submenus); the dynamic "Copy with Formatting" submenu is filled at run time from the theme catalog (`internal/tui/menubar/dynamic.go`). Round 5 adds plugin-contributed rows (`gvim.menu.add_item`, `internal/tui/menubar/plugins.go`, at most 32 rows, owner-cleaned). Help's label row says "gvim" where Fresh writes its version. |
| Keymap presets | 5 | 5 | Same names and inheritance graph (default base; macos/emacs/vscode inherit default; macos-gui inherits macos). Own / in-effect bindings after round 2: default 198/196, macos 53/212, macos-gui 50/261, emacs 78/243, vscode 9/201 (`keymap/common_set_test.go`); the presets are Fresh's own binding data for the four contexts gvim resolves (`docs/keybindings.md`). |
| Bundled plugins | 50 | 9 (10 before round 1) | 7 on by default (8 before round 1); `flash` and `vi_mode` off; round 1 retired the Lua `markdown_toc` plugin and folded the feature into the native Contents section (`docs/plugins/bundled.md` documents nine plugins). |
| Bundled plugin commands | ~180 | 13 after round 1 (15 before) | from `registerCommand(` / `gvim.command.register` counts; round 1 removed `markdown_toc`'s 2 registrations. |
| Themes | 10 built-in themes, theme files by name/path/URL/`builtin://`, `extends` inheritance, attributes on every color, theme editor, inspect-at-cursor, plugin theme overrides, background images | 10 built-ins + user themes (round 5) | Fresh's forms `name`/`builtin://`/user-dir-relative/`file://` with env expansion; URL/packaged themes are refused with a clear error (deferred). Registry, `extends` (built-in base only, as Fresh), auto-base by luminance, a 143-key table with Fresh's 49 required/91 fallback chains, named/Default colors, attributes on fg+bg, hot reload, inspect-at-cursor, native editor with Save As/Delete, plugin overrides (`docs/themes.md`). Residuals R5-3. |
| LSP server entries | built-in + plugin helpers + universal servers | 62 built-in entries | `lsp/manager/defaults.go`; round 4 routes lists/universal entries (Fresh's `LspLanguageConfig::Multi` + `universal_lsp`) but ships no built-in universal server (Fresh's quicklsp), and `process_limits` is parsed and unenforced (R4-1). |
| Plugin API surface | ~372 methods | 191 functions | `docs/plugins/gvim.d.lua` is 1220 lines; API level 2 (`gvim.api_version = 2`) with Fresh's names/contracts; the runtime language differs by design, capability gaps do not. Remaining gaps are the plugin row below and R5-2. |
| Settings page | schema-driven page (~14k LOC in refs/fresh) over every setting | 68 settings in 13 sections (8 scalar, 5 structured pages), User+Project layers | Rounds 3–4 land the page, the structured editors, reset/removal and Reload Configuration: `config/{settings_schema,settings_structured,patch}.go`, `internal/app/settingssvc.go`, `internal/tui/{settings.go,settings_structure.go}` (`docs/settings.md`). Residual coverage R4-3. |
| Terminal | PTY terminal panes and utility dock, live/scrollback modes per split, keyboard capture, clickable paths (OSC 7), restart exited process + agent resume, shell override, send selection, mouse forwarding/drag-select, close_on_exit, terminal tabs, ConPTY on Windows | **partial** (slices A+B; R3-2 closed) | `config/terminal.go`, `internal/terminal/{scrollback,mouse,saved}.go`, `internal/tui/dock/{selection,transcript,scrollback,input,panel,render,ports}.go` + `dock_glue.go`, `internal/restore/terminal.go`, `internal/uiadapter/terminal.go`, `docs/terminal.md`, tests `internal/terminal/*_test.go`, `internal/tui/dock/*_test.go`, `internal/restore/terminal_test.go` | Slices A+B: `terminal.shell`/`close_on_exit`/`mouse_forwarding`/`jump_to_end_on_output`/`mouse_drag_selects`, gvim-only `scrollback_lines`; scrollback pager + Ctrl+F search with regex/case; OSC 7 + Ctrl+Click links; read-only text selection (live-drag into scrollback, Shift+drag bypassing forwarding, word/line clicks in scrollback only, painted + copied, selection holds the view), dead-shell scrollback, restart-into-transcript (bounded prior rows, last OSC 7 cwd), `terminal.json` transcript persistence (read-only/dead on restore; no re-run — §4 item 26). Ctrl+Space is the dock's key (removed from the keymap, §4 item 25). Residuals R5-4: reflow (x/vt has none; saved width recorded only), automatic agent re-exec (`resume_agents` accepted-ignored), terminal tabs, child OSC 52/kitty/`pass_ctrl_v_without_text`, Windows ConPTY, drag auto-scroll and word-wise drag extension, hover underline/`file:line:col` links, split-terminal `close_on_exit`. |
| Explorer options | `file_explorer.width/side/follow_active_buffer/auto_open_on_last_buffer_close/compact_directories/preview_tabs/…` + sticky parents | 5 options + sticky parents implemented | width percent/columns with grip, left/right side geometry, follow + reveal, auto-open on last close, compact chains, sticky band; side toggle session-only and 4 keys accepted-ignored (R3-3). |
| Finder / Live Grep | file preview, Live Grep toolbar (case/word/regex, files/buffers/ignored/terminals/workspaces), quickfix, streaming | preview column + 6 Live Grep toggles | `internal/tui/palette/{preview,grep}.go`; deferred quickfix, terminals/workspaces providers, streaming, Live Grep row preview, Resume cache (R3-4). |
| Project search / replace | plugin Search & Replace panel (match tree, scoped replace, globs, quickfix) + Git Grep | native panel + Git Grep, R4-2 landed (round 5) | `search/plan.go`, `internal/app/{projectsearch,gitgrep}.go`, `internal/tui/searchpanel/` (glob, checkbox tree, scoped replace, history), `internal/tui/project_search.go`, `commands/search_commands.go`; residuals R5-5. |
| e2e feature index | 413 test files | not comparable | gvim uses Go unit tests; Fresh's file names are the feature index used below. |

## 3. Matrix by area

### 3.1 Engine and editing

| Area | Fresh features (evidence) | gvim status | gvim evidence | Notable sub-gaps |
|---|---|---|---|---|
| Text model, large files, encodings | rune text, undo/redo, two-mode large-file support, encoding detect/convert (UTF-8/16, Latin-1, CP125x, GBK/GB18030, Shift-JIS, EUC-KR), EOL detect | **implemented** | `buffer/`, `document/`, `encoding/`, `editor/large_*`, `editor/files_lineending*`, `docs/architecture.md`; Fresh `docs/features/encoding.md` | None found. Detection heuristics may order differently; per-file reload uses the same palette command. |
| Editing core, smart editing, vi | auto-indent/close/surround, smart home/backspace, bracket match, comment, case, transpose, sort/trim, shell command, vi-mode plugin | **implemented** | `document/smart*.go`, `editor/comment.go`, `document/case.go`, `editor/shell.go`, `plugin/bundled/plugins/vi_mode`, tests per action; Fresh `docs/features/editing.md` | Markdown compose mode is missing (#12/R4-5). Fresh's paragraph motions and "insert/open line" exist; replacement-mode (`R`) is documented as unsupported in the vi plugin. |
| Selection, multi-cursor, marks | multi-cursor (Ctrl+D, above/below, line ends), block selection, mark mode, mouse word/line/drag selection | **implemented** | `document/cursors*.go`, `document/block.go`, `editor/marks.go`, `internal/tui/motion_mouse.go`, keys in `keymap/default.json`; Fresh `docs/features/editing.md` | `add_cursors_to_line_ends` is not bound by default (present in Fresh's preset); Fresh's double-click word drag is implemented. |
| Undo, save pipeline, file metadata | one-undo-step edits, atomic saves, interrupted-save recovery, permissions/read-only, final-newline/trim on save, auto-save, auto-revert | **implemented** | `document/save_plan.go`, `atomicfile/`, `editor/files_recovery.go`, `editor/interrupted.go`, `internal/restore`, `docs/architecture.md` | Fresh's `sudo save prompt` is missing (no `sudo` path found); permission/ownership preservation is present. |
| Search & replace (in-buffer) | find, replace, query replace (y/n/!/q), find in selection, selection next/prev, case/whole-word/regex toggles, confirm-each, search history, match counts | **implemented** | `search/`, `internal/tui/find/`, `internal/tui/find_route.go`, tests `find_*_test.go`; Fresh `docs/features/search-replace.md` | Project-wide Search & Replace and Git Grep landed in rounds 4–5 (the next row); remaining search-panel residuals are streaming/preview/multi-line/large-file/provider scopes (R5-5). |
| Navigation, bookmarks, macros | position history, go to line/bracket, bookmarks 0-9 (set/jump/list), macros 0-9 + save-to-init/promote-to-command | **partial** | `editor/nav*.go`, `editor/goto.go`, `editor/marks.go`, `internal/tui/root_bookmarks.go`, `internal/tui/macro.go`; Fresh `docs/features/editing.md` | `List Bookmarks` is a stub ("editor does not list its bookmark slots", `internal/tui/root_commands.go:16`). One macro slot only: record/play/list work, `Macro: Save to init.ts`/`Promote to command` write an `init.lua` block (Fresh writes `init.ts`), no macro registers 0-9. |
| Display & view options | line numbers/relative, wrap + wrap_column + wrap indent, page view, virtual space (block/vertical), rulers, indent guides + rainbow, whitespace marks incl. newlines/CR, current line/column, occurrence, post-EOF tilde, scrollbars + markers, smooth scroll, inline diagnostics + staleness, inlay hints, semantic tokens, animations, screensaver | **partial** (R2+R4+R5 landed the user-visible subset) | `document/{virtualspace,block}.go`, `editor/{virtualspace,block,smart}.go`, `internal/tui/editorpane/{inline_diag,virtual_block,virtual_lines}.go`, `internal/tui/virtual_space.go`, `internal/tui/lsp_data.go` (hints/tokens), tests `editor/virtualspace_{block,lines}_test.go`, `internal/tui/virtual_space*_test.go`, `internal/tui/lsp_data_test.go`; Fresh `docs/features/editing.md`, `docs/internal/virtual-space-scoping.md` | Round 2 closed the horizontal `on` subset and inline diagnostics; round 4 closed `block` rectangles, vertical virtual lines and version-aware staleness; round 5 draws LSP inlay hints as end-of-line faint virtual text and semantic tokens as a style overlay (LSP row). Limits kept (§4 item 19): linear selections byte-clamped, block columns rune columns, virtual space off with wrap/multi-caret/large files, vertical lines clamp at the viewport/folded end, per-buffer toggle cannot select block, no persistence (R4-4). Page view is stored only and belongs to Markdown (#12); mouse hover is not built; scroll sync skips compare views; style-only keys stay out of `ViewOptions` (§4 item 9). |
| Folding | indent folds + LSP `foldingRange`, fold indicators, per-buffer toggle | **implemented** core; LSP source round 5 | `editor/fold.go`, `document/fold.go`, `editor/lsp_folds.go` + `internal/uiadapter/lsp_folds.go` (external fold source), `internal/tui/editorpane/` gutter, `lsp/requests.go`; tests `editor/lsp_folds_test.go`, fold tests | Round 5 merges the server's `foldingRange` results with the indentation folds (one fold set for the gutter, fold actions and hidden-line logic; ranges past EOF cut; refreshed debounced after edits and on open). Residual: the Contents `fold_buffer` setting (R1-1) can now consume the external fold source but the Contents side is not wired; a server that changes ranges without an edit refreshes on the next edit/open. |
| Splits, tabs, panes, windows | splits (vertical/horizontal), tab strip, tab drag, preview tabs, buffer groups, multiple windows/workspaces | **partial** | `internal/tui/split*`, `internal/tui/tabs.go`, `internal/tui/tab_drag.go`, `internal/tui/tab_drop.go`, preview tabs in `internal/tui/model.go`; Fresh `docs/features/navigation.md` | No buffer groups (plugin tier 1b), no second window/workspace model; "Move tab to other split" and "Extract Tab to New Workspace" are notices; `next_window`/`prev_window` map to pane cycling. |
| Compare files | `fresh --diff A B`, editable side-by-side, change copy/take, next/prev difference, switch side, scrollbar marks | **implemented** | `internal/tui/compare*.go`, `docs/compare.md`, `compare_*_test.go`, `internal/cli` `--diff`; Fresh `docs/features/compare-files.md` | Presentation differs deliberately (two file tabs, §4). Fresh's plugin API `compareBuffers`/`replaceRange` for plugins is missing; large file mode is refused on both sides. |

### 3.2 Project, chrome and settings

| Area | Fresh features (evidence) | gvim status | gvim evidence | Notable sub-gaps |
|---|---|---|---|---|
| File explorer and sidebar | tree, file create/delete/rename/copy/cut/paste, hidden/gitignored toggles, git status marks, context menu, async reload, width/side, follow active buffer, compact chains, sticky parents, plugin rows/decorations, sidebar sections | **partial** (R3 landed the options) | `explorer/{options,sticky}.go`, `internal/tui/explorer/` (panel/context), `internal/tui/root_explorer.go` (reveal/follow/auto-open), `internal/tui/frame/frame.go` (side geometry), `config/fileexplorer.go`, `internal/app/explorer.go`, `internal/tui/sidebar/`, `docs/sidebar.md` §9, tests `explorer/*_test.go`, `internal/tui/explorer_parity_test.go`; Fresh `docs/features/file-explorer.md` | Round 3 landed Fresh's explorer options: `width` (percent or columns, grip keeps the form, frame clamps [16,60]), `side` left/right with full frame geometry and hit map, `follow_active_buffer` (reveal on tab change, silent), `auto_open_on_last_buffer_close`, `compact_directories` (one row per single-child chain), sticky-parent band (≤7, never the whole body) and explicit reveal feedback on show/focus. Still open: plugin explorer rows/decorations/slots; `respect_gitignore`, `custom_ignore_patterns`, `preview_tabs` and the tree-indicator glyph keys are accepted-ignored; the side toggle is session-only (Fresh persists it); per-segment compact-row clicks/context menus; Contents residual gaps (R1-1, R1-2). |
| Finder, quick open, live grep | Ctrl+P modes (file/command/line/buffer), fuzzy ranking, streaming enumeration, finder preview, live grep with provider toggles (case/regex/word/files/buffers/ignored/terminals/workspaces), quickfix export, resume | **partial** (R3 landed the preview and toggles) | `finder/finder.go` (`EnumerateAll`, hidden/ignored walk), `internal/tui/palette/{preview,grep,palette,draw}.go`, `internal/app/{preview,live_grep}.go`, `docs/ui.md` palette details, tests `palette/{preview,grep,palette}_test.go`, `internal/app/live_grep_test.go`; Fresh `docs/features/command-palette.md`, `search-replace.md` | Round 3 landed: the file-mode preview column (head of the selected file, 16 KiB / 12 lines, `path:line:col` parsing, target line highlighted, binary/empty/unreadable placeholders, dropped under 64 inner cells) and Live Grep's toolbar — case (seeded by `editor.search.case_sensitive`), regex (on by default as Fresh's), whole word, files, buffers (1 MiB snapshot per buffer) and ignored/hidden sources, with match counts and error rows. Still open: quickfix export, terminals/workspaces providers (need the plugin/results-panel work), streaming provider model, preview for Live Grep rows (file mode only), syntax colours/scrolling in the preview, and Resume re-runs the search instead of restoring cached matches (R3-4). |
| Project-wide search, replace and Git Grep | plugin Search & Replace panel: grouped match tree with per-file/per-match selection, scoped replace, Case/Whole/Regex toggles, current-file/all-files, file globs, 10k cap, unsaved buffers, quickfix export, search history; git grep finder with a case toggle and provider scopes | **implemented** core; **partial** against the plugin (R4+R5) | `search/plan.go` (per-file edits planned, no stale offsets), `internal/app/{projectsearch,gitgrep}.go` (open dirty buffers, caps/skips, Git Grep through `shellcmd.RunArgs`), `internal/tui/searchpanel/` (`glob.go` filter, checkbox match tree, scoped replace, history, list export) + `internal/tui/project_search.go`, the shared apply helper in `internal/tui/workspace_edit.go`, `commands/search_commands.go`, `docs/keybindings.md`, `docs/ui.md`; Fresh `plugins/search_replace.ts`, `plugins/git_grep.ts` | Round 4 landed the panel, ports and unsaved-tab apply; round 5 added match-tree checkboxes (Space/a/n), Replace Scoped (Shift+Enter), the file-glob field (`*`/`?` per segment, `**` across, invalid pattern blocks), the Alt+Q filterable list export (quickfix substitute, §4 item 31) and 20-deep search history. Residuals R5-5: no streaming/batched results (one async search, "searching…" until it lands), no split preview pane, multi-line matches as one start-line row, no large-file replace, provider scopes beyond files/current buffer, no Git Grep globs; replacements stay unsaved for review (§4 item 16). |
| Command palette and command table | 206 core commands + plugin commands, contexts/availability, prefix modes, key hints | **implemented** core / **partial** plugin commands | `commands/` (206 generated + native rows), `internal/tui/palette/`; Fresh `docs/features/command-palette.md` | The generated core table is exact (206/206). The runtime palette has 222 rows: 206 generated + 7 hand-written native rows (compare, markdown Contents, keys) + 4 plugin-management rows + 3 round-4 rows + 2 round-5 rows (Edit Theme, Project Search Results in List) — `commands/palette_test.go`. Bundled plugin commands number 13 vs Fresh's ~180, so palettes differ by the missing plugins. |
| Menu bar, context menus, help, welcome | 8 menus, 103 static actions, dynamic theme submenu, mnemonics/underlined accelerators, context menus, help overlay, welcome screen, dashboard | **implemented** menus/help/welcome; dashboard **missing** (plugin) | `internal/tui/menubar/{menu_table_gen,menubar,dynamic}.go`, `keymap/menu.go` (`Map.MenuMnemonic`), `internal/tui/menu_mnemonic_test.go`, `internal/tui/menubar/mnemonic_test.go`, `internal/tui/context_menu.go`, `internal/tui/help.go`, `internal/tui/welcome.go`, `docs/ui.md` §6; Fresh `docs/internal/editor-ux-features.md`, `docs/features/dashboard.md` | Menus match Fresh's static table exactly (103 actions, 31 separators, 2 static submenus), and the dynamic "Copy with Formatting" submenu lists the 10 bundled themes (`menubar/dynamic.go`). Round 2 added the accelerators: underlined keymap-assigned letters and Alt+letter through ordinary keymap resolution, F10 for the bar, `editor.menu_bar_mnemonics` off removes both; dropdown rows carry no mnemonics (as in Fresh). Help's label says "gvim" instead of Fresh's version string. Residual: the dashboard plugin (backlog #14), not the menu system. |
| Status bar | configurable element lists (Fresh's defaults: left `{trust} {remote} {terminal_restart} {cursor} {diagnostics} {cursor_count} {messages}`; right `{read_only} {line_ending} {encoding} {language} {lsp} {warnings} {update} {palette}`), separator, per-element click actions, squeeze rule, `{filename}`/`{chord}`/`{clock}` | **implemented** (R2; parsed-but-unpainted elements documented) | `internal/tui/frame/frame.go` `StatusRuns`/`DefaultStatusLeft/Right`, `internal/tui/model.go` `statusView`/`statusRow`, `internal/tui/status_elements.go` (filename/chord/clock/tick), tests `statusbar_elements_test.go` + `statusbar_residual_test.go`, `docs/ui.md` §5; Fresh `config.rs` `StatusBarConfig`, `view/ui/status_bar.rs` | R2-3 closed in round 5: `{filename}` (workspace-relative or absolute path, document name when pathless, Fresh's " [+]" / " [RO]" markers), `{chord}` (shown only while a multi-key sequence waits, gvim's label style, muted — §4 item 22) and `{clock}` (24-hour `HH:MM`, static colon as Fresh's code — §4 item 23; a minute tick redraws while configured, a config change takes effect next launch). The filename resolves the root and the document through canonicalFilePath (the /private/var vs /var class) and falls back to the opened spelling outside the root (integration fix, statusbar_residual_test + status_filename_app_test). Still unpainted: plugin tokens `{plugin:token}`; `{warnings}`/`{update}` are deliberately empty (no warning-count domain/updater). Plugin status items keep their anchors. |
| Config layers and Settings UI | 4 layers (system/user/project/session), per-buffer overrides, config write from toggles, Settings UI (tree, widgets, layer switch, save), `init.ts`, config reload | **partial** (R3 + R4 landed the editable page and its structured coverage) | `config/settings_schema.go` (68 settings in 13 sections; 8 scalar + the Languages/Formatters/Status Bar/File Explorer/Terminal pages), `config/settings_structured.go`, `config/patch.go` (set, remove, cascade), `internal/app/settingssvc.go`, `internal/tui/{settings.go,settings_structure.go}` + `SettingsPort`, `commands/settings_commands.go` (Reload Configuration), `docs/settings.md`, tests `config/patch*_test.go`, `internal/app/settingssvc_test.go`, `internal/tui/settings_page_test.go` | Round 3 landed the editable scalar page, User/Project layers (Project trusted-only), comment-preserving JSONC patch + atomicfile write, validation through the real option tables, live apply on the update loop with an explicit restart set, and `SettingsPort.Reload`. Round 4 added the structured editors (16 scalar per-language overrides, formatters, status-bar dual list, file-explorer options, terminal shell/args), per-key and structured reset/removal with parent cleanup, and the Reload Configuration command. Still open (R4-3): the LSP section, plugins/keybindings/env-detector sections, per-language structured values (extensions, filenames, formatter args/stdin/timeout, on_save, indent), Fresh's reset-to-default vs inherit distinction (§4 item 17), per-change undo, a Reload keybinding, and plugin settings/theme preview. `gvim config show` still merges the project layer regardless of trust (R1-3). |
| Keymaps and keybinding editor | 5 presets (276/104/64/54/4 own bindings across 13 contexts), user `keybindings`, unbind/noop, chords, custom keybinding maps, keybinding editor | **partial** (R2 landed the data for the four resolvable contexts) | `keymap/*.json` (inheritance graph), `keymap/common_set_test.go` (own/in-effect counts), `keymap/menu.go`, `internal/keybindings/`, `internal/tui/keybind_editor.go`, `docs/keybindings.md`; Fresh `docs/configuration/keyboard.md`, `docs/features/keybinding-editor.md` | Round 2 rebuilt the presets on Fresh's inheritance graph with Fresh's own bindings for the four contexts gvim resolves (`global`, `normal`, `file_explorer`, `terminal`): own/in-effect default 198/196, macos 53/212, macos-gui 50/261, emacs 78/243, vscode 9/201. Missing keys were added (block select, line-end cursors, marks/bookmarks, LSP actions, shell, case/query-replace, scroll, tour/macro), gvim-specific keys kept where Fresh has none, conflicts resolved to Fresh's data; `docs/keybindings.md` rewritten. Residual: the eight contexts gvim resolves in code are not keymap-addressable and a user binding there is skipped with a warning (§4 item 5, R2-4). Custom `keybinding_maps` are refused with a warning; the editor itself is implemented. |
| Themes and appearance | 10 built-in themes, theme files by name/path/URL/`builtin://`, `extends` inheritance, attributes on every color, theme editor, inspect-at-cursor, plugin theme overrides, background images | **partial** (R5 landed the model and editor) | `theme/` (`keys_table`, `registry`, `resolve`, `themefile`, `overrides`, `color`), `internal/tui/style/` (the only theme→style place), `internal/tui/{theme_inspect,theme_editor}.go`, `internal/app/theme_registry.go`, `docs/themes.md` | Round 5 landed: registry (built-in + `config.Dir()/themes` user dir), config forms (`name`/`builtin://`/relative/`file://` with env expansion; URL/packs refused), `extends` with built-in base, auto-base by luminance, Fresh's 143-key table (49 required/91 fallback/3 optional + gvim's 2 `ui.accent_*` + attribute keys), named/Default colors (ANSI-palette rendering verified equal to Fresh; fixed RGB only for luminance/inspect/editor/`ThemeColor`), attributes on fg+bg + legacy modifiers, hot reload, inspect-at-cursor (palette + Ctrl+Right-Click; rendered-color matching + context facts, §4 item 30), native editor (list-driven, live preview, Save/Save As/Save-as for built-ins, Delete, atomic write), plugin overrides. Residuals R5-3: URL/packaged themes, user-theme inheritance (Fresh's limit), color picker/mouse editing, per-cell provenance, Fresh themes' accent/popup colors, settings preview, background images. |

### 3.3 Services

| Area | Fresh features (evidence) | gvim status | gvim evidence | Notable sub-gaps |
|---|---|---|---|---|
| LSP | diagnostics, hover, definition/implementation/references, completion, rename, code actions + server-initiated file ops, signature help, format, folding ranges, inlay hints, semantic tokens, diagnostics panel, inline diagnostics + staleness, multi-server with only/except, universal servers, per-server status popup/mute, pull diagnostics, process limits, env/init options, root markers, C header routing | **partial** (R1–R4 routing/edits/status; R5 data rendering A–D; E pending) | `lsp/{client,handler,requests,capabilities}.go`, `lsp/manager/{manager,router}.go`, `internal/lsphost/{lsp,lsp_data}.go`, `internal/tui/{lsp,lsp_data,workspace_edit}.go`, `editor/lsp_folds.go`, `internal/uiadapter/lsp_workspace.go`, `docs/lsp.md`, tests `lsp/manager/router_test.go`, `internal/tui/lsp_{status,workspace,data}_test.go` | Rounds 4–5 landed: multi-server/universal router (capability intersection, only/except, dynamic registration), code actions with resolve/executeCommand/applyEdit (version checks, create/rename, delete refused), per-server status popup/mute, inline staleness; then inlay hints (end-of-line faint virtual text, toggle command, debounced refresh — tier-1 placement, §4 item 27), semantic tokens (full+delta, 20-line padding, existing syntax theme keys, overlay), foldingRange merged into the editor folds via the external fold source, and pull diagnostics (only when no server published; cached per version; §4 item 28). A real gopls session (initialization_options with gopls.hints.parameterNames etc.) verified hints end-to-end; the Toggle Inlay Hints state now starts from editor.enable_inlay_hints (ViewOptions.EnableInlayHints) instead of default-off (integration fix, [TestInlayHintsStartFromTheViewOptions]). R5-1/task-22 E still open: the diagnostics sidebar panel/quickfix list (the `WorkspaceDiagnostics` port data exists), progress indicators, inline-hint placement tier 2; carried 4a residuals: no built-in universal server, `process_limits` unenforced, resource-ops-first ordering/partial apply (§4 item 18), optimistic applyEdit. |
| Plugins runtime and host API | TypeScript on QuickJS, sandbox-lite with per-plugin contexts, ~372-method API, overlays/virtual text/conceals/soft breaks/folds, prompts/toolbars, modes/contexts, windows/splits, virtual+composite buffers, buffer groups, terminals, authority/remote, LSP requests, HTTP, widgets/floating/dock panels/sidebar sections, i18n, package manager, menus, events | **partial, runtime different; API level 2** | `plugin/`, `pluginapi/` (incl. `widgetapi.go`), `internal/pluginhost/`, `internal/tui/plugin*.go` + `internal/tui/pluginpanel/`, `docs/plugins/` (6 API pages + `events.md`, 1220-line `gvim.d.lua`); `docs/plugins/porting.md` lists what is not provided | Lua 5.1 is the permitted runtime difference. Round 5 (tier 1a, API level 2) added declarative widget panels/sidebar sections (read-only v1: row/col/label/text/raw/hintBar/spacer/divider/button/toggle/list/tree; actions focusAdvance/activate/selectMove; mutations setChecked/setSelectedIndex/setValue; placements float/buffer/sidebar section; caps 8 panels/2000 widgets/etc.), plugin i18n (`<name>.i18n.json`, `t()`, %-prefixed command/menu titles), menu-bar rows, blocking action popups + result event, prompt suggestions, mouse/focus + LSP status/error events, all-files diagnostics, theme override hooks/`builtin_themes`. Remaining (R5-2): editable widget kinds (text/dropdown/radio/dualList/tree editing), floating/dock placement controls, buffer groups + `setPanelContent`, composite buffers + scroll sync, virtual lines/conceal/soft breaks/layout hints/folds/line targets, terminal API, windows/authority/orchestrator, package manager, HTTP, project-local plugins, `render_line`; sidebar sections are append-only (no F6 cycle/drag-resize/reorder). |
| Bundled plugin feature set | 50 plugins: git_log, live_diff, git_grep, git_explorer, git_blame, git_gutter, git_statusbar, git_find_file, search_replace, diff_nav, diagnostics_panel, code-tour, dashboard, welcome*, theme_editor, markdown_toc, markdown_compose, markdown_source, path_complete, lsp_help, lsp_navigation, find_references, compare_files, flash, vi_mode, merge_conflict, tab_actions, goto_with_selection, env-manager, devcontainer, k8s-workspace, agent_*/orchestrator/live_sessions, pkg, audit_mode, language helpers | **partial** | `plugin/bundled/plugins/` (9), `docs/plugins/bundled.md`; Fresh `plugins/*.ts` | Still 9 bundled plugins (round 1 retired `markdown_toc`; `git_blame`/`merge_conflict` reduced, `vi_mode` limited). Round 5 built the host tier 1a but ported no plugin; #14 is the next track (diagnostics_panel first, then git_log/live_diff/theme_editor/search_replace; see the round-6 plan in §5.5). |
| Terminal | PTY terminal panes and utility dock, live/scrollback modes per split, keyboard capture, clickable paths (OSC 7), restart exited process + agent resume, shell override, send selection, mouse forwarding/drag-select, close_on_exit, terminal tabs, ConPTY on Windows | **partial** (R3 landed slice A) | `config/terminal.go`, `internal/terminal/{scrollback,mouse,session,term}.go`, `internal/tui/dock/{scrollback,input,panel,render,ports}.go` + `dock_glue.go`, `internal/uiadapter/terminal.go`, `docs/terminal.md`, tests `internal/terminal/*_test.go`, `internal/tui/dock/*_test.go` | Round 3 landed: `terminal.shell` (command+args; project layer may not set it), `close_on_exit`, `mouse_forwarding` (`requested`/`alt_screen` with Shift bypass and the alternate-scroll rule), gvim-only `scrollback_lines`; a read-only scrollback pager (Ctrl+Space toggle with terminal_escape fallback, Esc, PgUp/PgDn/Home/End, `/` case-insensitive search with n/N, y copy row, Ctrl+C copy screen, title rows), OSC 7 working-directory tracking with Ctrl+Click path links (absolute/~ → OSC 7 cwd → workspace root, exists check in a tea.Cmd, off on the alternate screen), and mouse forwarding from the child's DECSET modes. Round 5 (slice B) closed R3-2's main items: read-only text selection in the dock (drag on the live grid drops into scrollback and selects, Shift+drag selects while a program owns the mouse, double/triple click select a word/row in scrollback only, selection painted and copied through the clipboard path, selection holds the view against automatic scroll); `jump_to_end_on_output` and `mouse_drag_selects` parsed and applied; scrollback search with Ctrl+F plus Ctrl+R regex and Ctrl+A case toggles (prior transcript + history + screen); dead-shell focus and scrollback navigation; restart-into-the-same-transcript (bounded prior rows above the new shell, restarted in the last OSC 7 cwd) and session-directory persistence of the transcript (`terminal.json`, bounded rows with control characters stripped, restored read-only and dead, nothing re-run). Still open: reflow (x/vt has no reflow; saved width recorded only), automatic agent re-exec (`terminal.resume_agents` accepted-ignored: no tracked launch/agent argv), terminal tabs, child OSC 52 (x/vt exposes no payload) / kitty keyboard / `pass_ctrl_v_without_text`, Windows ConPTY, drag auto-scroll and word-wise drag extension, hover underline and `file:line:col` links, split-terminal `close_on_exit`. |
| Git surfaces | review diff (stage/unstage/discard/comments/export), range/stash reviews, git log with live diff, git blame panel with history, live diff baselines, diff chunk nav, git grep, git explorer, git find file, statusbar branch, gutter marks | **partial** | `gitstate/`, `plugin/bundled/plugins/git_gutter`, `git_statusbar`, `git_find_file`, `git_blame`, `merge_conflict`, `internal/tui/git.go`; Fresh `docs/features/git.md` | Missing: Review Diff (/range/stash), Git Log, Live Diff, Git Grep, Git Explorer, blame panel/history stepping (gvim has a reduced inline blame), merge view (reduced to in-buffer resolution), diff hunk sources beyond the HEAD gutter, review comments/export. Implemented: gutter hunk marks + next/prev change, branch statusbar, git find file, merge-conflict resolution, compare-as-difftool. |
| Session, restore, daemon | session restore/save, hot exit of unsaved buffers, crash recovery, ephemeral-file patterns, per-file pins, `--restore`/`--no-restore`/`--simple`, daemon mode with attach/detach and `--cmd daemon open-file --wait`, direct-vs-daemon launch modes | **partial** (R2 landed hot exit, the restore settings and the CLI flags) | `internal/app/{session_plan,session_open,hotexit}.go`, `internal/app/run.go`, `internal/restore/session.go` (`RestoreHotExit`), `internal/cli/root.go`, session/ files, tests `session_plan_test.go`, `session_launch_test.go`, `restore/session_hot_test.go`, `internal/cli/root_test.go`, README sessions/flags; Fresh `docs/features/session-persistence.md` | Round 2 landed: `hot_exit` (quit without asking, the session save stashes the text; off asks per dirty tab), `restore_previous_session` (tabs/layout vs the unsaved text only), `skip_session_restore_when_files_passed` (file launch, or restore then append the given files), and the one-shot `--restore`/`--no-restore` (alias `--no-session`)/`--simple` with mutual-exclusion errors. Still open: daemon mode and its CLI, no per-buffer hot-exit store (a file launch cannot keep/restore its files' unsaved text), `--no-restore` still saves the session (deliberate; §4 item 10, R2-5), and `--simple`'s "no dock" is vacuous. |
| Workspaces, windows, orchestrator, agents | orchestration dock, multiple workspaces per daemon (one per git worktree), workspace switcher, New Workspace dialog, Run Agent (claude/codex/opencode/aider), agent session discovery/resume, live sessions, extract tab to workspace | **missing** | `grep -ri orchestrator --include=*.go` finds only command-table/capability strings; `internal/tui/remote.go` says remote workspaces unsupported; `extract_tab_to_new_workspace` is a notice; Fresh `docs/features/session-persistence.md`, `docs/features/scripting.md` | Whole area absent; gvim is one editor per process. Depends on daemon/workspace model. |
| Remote authority | SSH editing (scp/URL forms), devcontainers (CLI attach/rebuild/ports/logs), Kubernetes workspaces, remote-aware LSP/terminal/processes, reconnect, remote indicator popup | **missing** | No `ssh`/`authority` implementation in the tree; `internal/tui/remote.go:10` popup says "Remote workspaces are not supported"; `docs/plugins/porting.md:46` "authority ... not provided"; Fresh `docs/features/ssh.md`, `docs/features/devcontainer.md` | Entire area absent. Requires an authority abstraction under fs/proc/terminal/LSP first. |
| Workspace trust | per-folder Restricted/Trusted/Blocked, executable-marker detection, one-time prompt, status pill, dialog command, persisted per folder, gates LSP/build/env activation, env detectors | **partial** (R1 landed) | `trust/` (levels + Undecided, store at `config.Dir()/trust.json`, atomic writes, marker scan, Fresh startup policy), `internal/app/trust.go` and `internal/app/run.go` (`Options.Untrusted`, `lsp_enabled` forced off), `internal/tui/trust*.go` (dialog, port, notices, pill), `config/trust_test.go`, `docs/trust.md`; Fresh `docs/features/workspace-trust.md` | Implemented in round 1: the three levels plus Undecided, per-folder persistence, executable-marker scan and prompt (silent trust for plain folders, Fresh's policy), the open-time prompt, the `Workspace Trust…` palette command and dialog, the trust status pill with click routing, the project config layer skipped unless Trusted, and LSP auto-start forced off unless Trusted (manual Start/Restart still works). Still open: no process gate — terminal/shell/formatter/LSP/plugin spawns are not consulted against the level, and Blocked enforces the same as Restricted (`docs/trust.md` §3); env detection/activation and devcontainer/tasks are absent; the config gate is a startup decision (a mid-session trust change applies project tooling at the next start, documented divergence). Tracked as backlog #17. |
| Markdown | syntax highlighting, markdown_source smart list editing, compose/rendered/dual modes, conceal, tables, Contents section | **partial** (R1 Contents; R4 smart-editing subset) | `editor/markdown.go` + `editor/markdown_test.go`, `syntax/`, `internal/tui/sidebar/contents.go`, `outline/`, `commands/markdown_commands.go`, `docs/sidebar.md`; Fresh `plugins/markdown_toc.ts`, `plugins/markdown_source.ts`, `plugins/markdown_compose.ts` | Round 4 landed a `markdown_source` subset: list continuation (indent + same bullet/number+1/checkbox), empty-item removal, Tab/Shift+Tab indentation and bullet cycling, auto-close handling, gated to `.md`/`.markdown`/`.mdx` or the markdown/multimarkdown language; primary caret only, hardcoded 4-space indent as Fresh's plugin does (§4 item 20). Still open (R4-5): the rest of the plugin surface, compose/rendered/dual view (and page view), conceal/soft breaks, tables. |
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
    the Settings page (round 4) covers it for the file, but the session toggle does not write it (R3-3). `docs/sidebar.md` §9.
14. **`terminal.scrollback_lines` is a gvim-only extension.** It is not a Fresh key; the
    remaining Fresh `terminal` keys (`resume_agents`, `pass_ctrl_v_without_text`,
    `skip_app_execution_alias`) are accepted and ignored until their features land (round 5
    implemented `jump_to_end_on_output` and `mouse_drag_selects`). `docs/terminal.md` Config.
15. **Ctrl+] is the only terminal exit chord (gvim addition).** Round 3 kept Fresh's
    `terminal_escape` fallback on Ctrl+Space; round 5 moved Ctrl+Space to the dock itself
    (item 25), so Ctrl+] (and the dock escaping) is the only chord that gives the keyboard
    back to the editor. `docs/terminal.md`.
16. **Project-wide replace applies to tabs, not files.** Fresh's `search_replace` plugin writes
    replacements straight to disk; gvim plans per-file edits and applies them through the editor:
    open buffers change in memory (one undo step per file, Save All shows the usual dialog) and
    files that are not open are opened as unsaved tabs. Deliberate: it avoids Fresh's
    stale-offset/undo bugs (`docs/internal/search-and-diff.md` 2.x) and never writes a file the user
    has not seen. `internal/tui/project_search.go`, `docs/keybindings.md`.
17. **Settings reset removes the key (inherit) only.** Fresh's editor has per-field "reset to
    default" and "inherit" actions; gvim's page has one reset, which deletes the key from the
    edited layer so a lower layer or the built-in default applies (structured entries are removed
    the same way). A reset to the built-in default while a lower layer sets a value cannot be
    expressed yet (R4-3).
18. **LSP workspace edits are applied resource-ops-first.** A server's `documentChanges` are split
    into file operations (create/rename/delete) and text edits: gvim applies every file operation
    first, in its `documentChanges` order, then applies the text edits; a refused file operation is
    reported but does not stop the remaining text edits. Fresh applies `documentChanges` in order.
    gvim refuses delete-file operations and a create/rename onto an existing target (Fresh's rule)
    and replies to `workspace/applyEdit` optimistically (`applied: true` before the UI applies it).
19. **Virtual-space block geometry is rune-based.** Block corners are stored as rune columns
    (byte-addressable edit ranges converted at the editor boundary), so the rectangle is exact for
    the ASCII spaces padding adds; tabs/wide characters at or past EOL can shift a block edge from
    Fresh's visual-cell columns. Linear selections stay byte-clamped and virtual space is off under
    wrap/large-file/multi-caret, as Fresh's scoping doc records.
20. **Markdown source editing hardcodes a four-space indent.** The `markdown_source` plugin pins
    `TAB_SIZE = 4` and ignores `tab_size`/`use_tabs` for Markdown list indentation, and gvim matches
    it (`editor/markdown.go`); the smart editing runs only in source view (gvim has no other view)
    and only for the primary caret.

21. **Named colors were verified equal, and are kept that way.** Fresh's named theme colors
    render through the terminal's ANSI palette (its ratatui named colors), with the fixed-RGB
    table used only for luminance, inspect/animation math, the theme editor and `ThemeColor`.
    gvim does the same (verified in the reference source), so the bundled `terminal` theme stays
    host-adaptive; the earlier concern that gvim diverged was wrong. `docs/themes.md` §3.
22. **The status-bar chord is muted.** Fresh paints a pending multi-key sequence with the normal
    status foreground; gvim paints it faint (`frame.StyleMuted`). The label form is also gvim's
    ("[Ctrl+X]"). `docs/ui.md` §5.
23. **The clock's colon does not blink.** Fresh's docs mention a blinking colon, but the checked
    reference renders a static one; gvim matches the code (24-hour zero-padded `HH:MM`) and
    redraws on the minute while the element is configured and visible.
24. **The plugin API is level 2.** `gvim.api_version` is 2 and a manifest may require it with
    `"api": 2`; level 1 plugins keep working. The level covers the tier-1a host surfaces (widget
    panels/sections, i18n, menus, action popups, prompt suggestions, events, all-diagnostics,
    theme overrides), not the whole Fresh API. `docs/plugins/README.md`.
25. **Ctrl+Space belongs to the terminal dock, not the keymap.** Round 5 removed the
    `terminal_escape` binding from the terminal context: the dock reads Ctrl+Space itself to
    toggle live/read-only scrollback (Shift+PageUp also enters it), and Ctrl+] is the only chord
    that returns the keyboard to the editor (gvim addition). A user keymap cannot rebind it.
    `docs/terminal.md`, `docs/keybindings.md`.
26. **Terminal persistence is a read-only transcript, not a live session.** `terminal.json` keeps a
    bounded transcript of the newest rows (500 rows / 256 KiB, control-stripped); restore brings
    it back read-only and dead, re-runs nothing, and there is no reflow (x/vt keeps the widths
    rows had when they scrolled off; the pane clips, the saved width is recorded only). Fresh
    keeps live terminals and can resume tracked agents; gvim's `resume_agents` stays
    accepted-ignored and restart starts a shell in the last OSC 7 directory. `docs/terminal.md`.
27. **Inlay hints use the tier-1 end-of-line placement.** Fresh inserts a hint at its own
    character position; gvim renders the line's hints as faint virtual text after the last
    character (right-aligned inline diagnostics keep their own slot). Toggle Inlay Hints turns
    them off and on; the document text is never changed. `docs/lsp.md`.
28. **Pull diagnostics only fill a gap.** A server that publishes is never also pulled from, so
    one problem is reported once; pulled sets are cached per document version. Fresh's fusion
    rules are richer (hover fusion/per-server raw sets), which round 6's panel work may need.
    `docs/lsp.md`.
29. **The theme editor is a native list-driven overlay.** Fresh's editor is a plugin using the
    widget runtime; gvim's is a sheet over the resolved theme (swatch/value/attribute rows,
    prompt-based colour+attribute edits, live preview, Save/Save As/Delete, atomic write) so it
    needs no widget-runtime dependency. There is no colour picker or drag editing (R5-3).
30. **Inspect-at-cursor matches rendered colours plus context, not cell provenance.** Fresh
    records the theme key each painter used; gvim repaints the frame, compares the cell's
    rendered fg/bg against the resolved theme (listing every matching key, capped) and uses
    context facts (current line, selection, find, diagnostic, syntax kind) to pick the role.
    Ambiguous cells can list several keys. `docs/themes.md` §5.
31. **Project-search "quickfix" is a transient list.** Alt+Q opens the result set in the shared
    list overlay (filterable, Enter jumps, list stays open); gvim has no persistent quickfix
    stack and the results panel remains the source of truth. `docs/ui.md`.
32. **The plugin action popup blocks the caller.** `gvim.ui.action_popup` waits for the choice
    and returns its id (and also emits `action_popup_result`); Fresh's `showActionPopup` returns a
    boolean and reports only through the hook. Both share the card/frame and the caps.

## 5. Ranked backlog for the remaining rounds

Sizes are estimates: S ≈ a day or less, M ≈ 1–3 days, L ≈ 3–10 days, XL ≈ more (unvalidated).
The order follows the goal's dependency shape: user-visible TUI/editing gaps first, then the
plugin-API breadth that unblocks Fresh's plugin-era features, then the daemon/remote/agent
architecture. "Owner area" names the package that should own the work.

Rounds 1–5 closed the workspace-trust surface, native sidebar sections/Contents and the LSP
pill + diagnostics badge (round 1); virtual space (`on`), inline diagnostics, Fresh's keymap
data, the config-driven status bar + menu accelerators, and the session settings/CLI (round 2);
the editable Settings page, terminal slice A, the explorer options + sticky parents, and the
palette preview + Live Grep toggles (round 3); LSP 4a, project-wide Search & Replace + Git Grep,
settings coverage, virtual space R2-1 and markdown smart editing (round 4); and LSP 4b data
rendering A–D, the plugin host tier 1a (API level 2), the theme model/editor, terminal slice B,
the project-search R4-2 additions and the status-bar elements R2-3 (round 5). Those are no
longer backlog items (retired ids: #8 and #11 are closed; §5.1 now holds #7 as its remaining
LSP diagnostics-panel/quickfix slice and #12; R2-1, R2-2, R2-3, R3-1 and R3-2 are closed). The
ranked items below are the remaining gaps, with the gaps the rounds opened listed first as
R1-*, R2-*, R3-*, R4-* and R5-*.

Round 1 recorded four gaps (R1-1…R1-4; the spawn gate is also ranked as #17):

- **R1-1 (P1, M)** — per-heading fold ranges for the Contents `fold_buffer` setting: the setting
  is read and plumbed and the editor now accepts external fold ranges (`SetExternalFolds`, round
  5), but the Contents side does not feed its heading ranges yet, so the setting still does
  nothing user-visible (`internal/tui/sidebar`; `docs/sidebar.md` §6).
- **R1-2 (P2, S)** — persist sidebar section state in the session (section list/order, Contents
  visibility and follow mode, heights; `internal/tui/sidebar`, `internal/restore`, `session`).
- **R1-3 (P2, S)** — `gvim config show` still merges the project config layer regardless of
  trust; gate it by the trust level or label the layers (`internal/configview`, `config`;
  `docs/trust.md` §3 "Config viewer").
- **R1-4 (P1, L)** — per-spawn executable-path gate and Blocked spawn denial
  (terminal/shell/formatter/LSP/plugin processes); tracked as ranked item #17
  (`trust/` and the spawn sites; `docs/trust.md` §3).

Round 2 recorded two remaining gaps (R2-1/R2-2 closed in round 4, R2-3 in round 5):

- **R2-4 (P2, M)** — the eight code-resolved keymap contexts: expose Fresh's
  prompt/searchPrompt/popup/completion/settings/menu/compositeBuffer/dock contexts to user
  bindings once the overlays take keymap bindings; today such bindings are skipped with a
  warning (`docs/keybindings.md`).
- **R2-5 (P2, M)** — per-buffer hot-exit store and file-launch unsaved-text restore: one
  session file means a file launch cannot keep its files' unsaved text and `--no-restore` must
  still save (§4 item 10); a per-buffer store would close this.

Round 3 recorded three remaining gaps (R3-1 closed in round 4):

- **R3-2 (CLOSED round 5)** — terminal slice B landed selection/copy, `jump_to_end_on_output` +
  `mouse_drag_selects`, regex/case scrollback search, dead-shell scrollback, restart-into-the-
  transcript and the saved `terminal.json` transcript (read-only/dead on restore, no process
  re-run). Residuals moved to R5-4 (reflow, automatic agent re-exec, tabs, OSC 52/kitty, ConPTY,
  drag auto-scroll/word-wise drag, hover links). `docs/terminal.md` Deferred.
- **R3-3 (P2, M)** — explorer residuals: `respect_gitignore`, `custom_ignore_patterns`,
  `preview_tabs` and the tree-indicator glyph keys are accepted-ignored; the side toggle is
  session-only (Fresh writes the user config); per-segment compact-row clicks/context menus;
  plugin explorer rows/decorations/slots (with #13). `docs/sidebar.md` §9.
- **R3-4 (P2, M)** — finder/palette residuals: quickfix export, terminals/workspaces providers,
  streaming provider model, preview for Live Grep rows (file mode only today), syntax colours
  or scrolling in the file preview, and Resume re-runs the search instead of restoring cached
  matches. `docs/ui.md` palette details.

Round 4 recorded five gaps (R4-1 and R4-2 partially closed in round 5):

- **R4-1 (P1, M remaining)** — LSP 4b: round 5 landed inlay hints (end-of-line tier), semantic
  tokens, foldingRange and one-source pull diagnostics; what remains is increment E — the
  diagnostics sidebar panel, quickfix list and progress indicators — plus the tier-2 inline-hint
  placement. Carried 4a residuals: no built-in universal server, `process_limits` parsed but
  unenforced, resource-ops-first ordering/partial apply (§4 item 18), optimistic `applyEdit`
  acknowledgement. `docs/lsp.md` "What is not here yet".
- **R4-2 (P2, M remaining)** — project search: round 5 landed match-tree checkboxes, Replace
  Scoped, the file-glob field, the Alt+Q list export and 20-deep search history; remaining are
  streaming/batched results, a split preview pane, multi-line matches as rows, large-file
  replace, provider scopes beyond files/current buffer, and Git Grep globs. Unsaved-tab apply is
  deliberate (§4 item 16); `docs/keybindings.md`, `docs/ui.md`.
- **R4-3 (P2, M)** — settings residuals: the LSP section, the plugins/keybindings/env-detector
  sections, per-language structured values (extensions, filenames, formatter args/stdin/timeout,
  `on_save`, `indent`), Fresh's reset-to-default vs inherit distinction (§4 item 17), per-change
  undo, a Reload keybinding, plugin settings and theme preview. `docs/settings.md` §6.
- **R4-4 (P2, S)** — virtual-space limits: linear selections byte-clamped, block columns
  rune-based (§4 item 19), no virtual space with wrap/multi-caret/large files, vertical lines
  clamp at the viewport/folded end and cannot be selected, the per-buffer toggle cannot allow
  block, positions are not session-persisted. `docs/internal/virtual-space-scoping.md`.
- **R4-5 (P2, M)** — markdown_source residual: the rest of the plugin surface and the
  compose/page-view modes (Fresh's "rendered" is the inline compose view, "dual" a source+compose
  buffer group — see #12), tables and code-fence frames; the smart editing is source-view-only
  with a fixed four-space indent (§4 item 20). `editor/markdown.go`.

Round 5 recorded six gaps:

- **R5-1 (P1, M)** — LSP 4b increment E: a native diagnostics sidebar panel (grouped by file and
  severity from the multi-server store, Enter jumps, palette toggle), the quickfix list and
  progress indicators; task-22 stays in progress until it lands. The port data exists
  (`WorkspaceDiagnostics`); `docs/lsp.md`.
- **R5-2 (P1, L/XL)** — plugin host tier 1b/1c: editable widget kinds (text/dropdown/radio/
  dualList/tree editing), buffer groups + setPanelContent + scroll-sync groups, composite
  buffers, virtual lines/conceal/soft breaks/layout hints/folds/line targets, floating/dock
  placement controls, theme-file APIs, window/authority/terminal seams. Also the v1 sidebar
  limits (append-only, no F6 cycle, no drag-resize/reorder) and HTTP still not provided
  (the porting doc's not-provided line lost it this round; see §7). `docs/plugins/api-ui.md`.
- **R5-3 (P2, M)** — theme residuals: URL/packaged themes; user-theme inheritance (Fresh's own
  limit); colour picker/mouse editing; per-cell painted-key provenance (inspect matches rendered
  colours + context, §4 item 30); Fresh themes' accent/popup selection colours; theme preview in
  the Settings page; background images. `docs/themes.md` §8.
- **R5-4 (P2, M/L)** — terminal residuals: reflow (x/vt has none; saved width recorded only),
  automatic agent re-exec (`resume_agents` accepted-ignored), terminal tabs, child OSC 52/kitty
  keyboard/`pass_ctrl_v_without_text`, Windows ConPTY, drag auto-scroll and word-wise drag
  extension, hover underline and `file:line:col` links, split-terminal `close_on_exit`.
  `docs/terminal.md` Deferred.
- **R5-5 (P2, M)** — project-search residuals: streaming/batched results, split preview pane,
  multi-line match rows, large-file replace, provider scopes beyond files/current buffer, Git
  Grep globs. `docs/ui.md` search residuals.
- **R5-6 (P2, S)** — status bar residuals: `{plugin:token}` still unpainted (needs a
  register/paint API; plugin status items cover the use case), and `{warnings}`/`{update}` stay
  deliberately empty (no warning-count domain/updater).

### 5.1 User-visible TUI and editing gaps (do first)

| # | Gap | Pri | Size | Owner area | Notes / depends on |
|---|---|---|---|---|---|
| 7 | LSP 4b increment E: native diagnostics sidebar panel (grouped by file/severity, Enter jumps, palette toggle), quickfix list, progress indicators; inline-hint placement tier 2 (4a residuals: no built-in universal server, process_limits unenforced, resource-op ordering, optimistic applyEdit) | P1 | M | `lsp/`, `internal/lsphost`, `internal/tui/lsp*.go`, `internal/tui/sidebar/` | R5-1; A–D landed in round 5 (inlay hints/semantic tokens/foldingRange/pull diagnostics) |
| 12 | Markdown compose/page view: centered page width, marker conceal with cursor-aware reveal, soft wrap + hanging list/quote indents, then table box-drawing and code-fence frames; "dual" = a source+compose buffer group (needs tier 1b). The markdown_source subset landed in round 4 | P1 | L | `editor`, `syntax`, `internal/tui/editorpane`, `internal/tui` | R4-5; planned round 6 core per the Phase-1 spec |

### 5.2 Plugin-API breadth (unblocks Fresh's plugin-era features)

| # | Gap | Pri | Size | Owner area | Notes / depends on |
|---|---|---|---|---|---|
| 13 | Plugin host capability tier 1b/1c (tier 1a landed round 5, API level 2): interactive widget kinds; buffer groups + setPanelContent + scroll-sync groups; composite buffers; virtual lines/conceal/soft breaks/layout hints/folds/line targets; floating/dock placement controls; plugin theme-file APIs; window/authority/terminal seams | P1 | L/XL | `pluginapi`, `plugin`, `internal/tui/pluginpanel`, `internal/tui/plugin_*.go` | R5-2; Phase-1 spec splits 1b (widgets+groups+theme APIs) from 1c (virtual surface+composite+deferred seams) |
| 14 | Port the missing bundled plugins on the tier 1–2 host. First: diagnostics_panel (ready after 1a), git_log (needs 1b groups), theme_editor (1b + theme APIs), live_diff (1c virtual surface), search_replace (lowest: native project search already covers its user features; consider a deliberate native substitution). Then code-tour, dashboard, path_complete, lsp_help/lsp_navigation, markdown_compose/source | P1 | L each; XL total | `plugin/bundled/plugins`, `plugin/bundled/lib` | #13; Phase-1 port plan with the Lead |
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

1. **Round 6 — LSP E, plugin tier 1b, the first plugin port and the markdown compose core (the
   Phase-1 specs).** #7 remainder (diagnostics sidebar panel + quickfix + progress) as the M;
   #13 tier 1b (interactive widgets, buffer groups/panel content + scroll sync, theme-file APIs)
   as the L; #14 diagnostics_panel (the first port, ready after tier 1a) as the M; #12 markdown
   compose core (page width/margins/conceal/wrap + caret mapping) as the L.
2. **Round 7 — plugin tier 1c and the heavier ports.** #13 tier 1c (virtual lines/conceal/soft
   breaks/folds/line targets, composite buffers, window/authority seams); #14 git_log, live_diff,
   theme_editor; #12 markdown dual (buffer group) + tables/code-fence frames; R1-1 Contents
   fold_buffer (the fold source landed in round 5).
3. **Round 8 — remaining ports, diff API and trust gate.** Finish #14 (search_replace or a
   recorded native substitution, code-tour/dashboard/path_complete/lsp helpers), #15 diff/compare
   plugin API, #17 trust spawn gate + env detection, #22 i18n groundwork; sweep R5-3/R5-5.
4. **Round 9 — daemon, workspaces, agents, remote.** #18 daemon mode, #19 workspaces/windows,
   #20 agent sessions, #21 remote authority; keep direct mode and `--no-plugins` working.
5. **Round 10 — remaining polish/optional.** #23 Windows ConPTY, #24 update/telemetry decision,
   #25 platform input edges, #26 scope decision, terminal reflow/tabs (R5-4), then the
   R1-2/R1-3, R2-4/R2-5, R4-3/R4-4/R4-5 and R5 sweep; a final pass re-reads `refs/fresh` and
   refreshes this file.

## 6. Tracking notes

- **Rounds 1–4 landed (2026-11-08).** Each tree was gated by this audit (build/vet/lint/purity/
  import rules/tests green) and its diff reviewed with no scope creep (§7). One line per track:
  round 1 — workspace trust (levels/store/prompt/dialog/command/pill, Trusted-only project layer,
  LSP autostart gating), native sidebar sections + Markdown Contents (Lua `markdown_toc` retired:
  9 bundled plugins, 7 on), LSP/trust status pills and the diagnostics badge; residual R1-1…R1-4.
  Round 2 — virtual space `on` + inline diagnostics, the five keymaps on Fresh's inheritance graph
  (default 198/196, macos 53/212, macos-gui 50/261, emacs 78/243, vscode 9/201), config-driven
  status bar + menu accelerators, session settings/CLI (`hot_exit`, restore, `--restore`/
  `--no-restore`/`--simple`); R2-1/R2-2 later closed in round 4, R2-3 in round 5, R2-4/R2-5 open.
  Round 3 — editable Settings page (53 settings in 8 sections then; JSONC patch + atomicfile,
  live/restart sets), terminal slice A (shell/close_on_exit/scrollback_lines/mouse forwarding,
  pager/links), explorer options (width/side/follow/auto-open/compact/sticky), finder preview +
  Live Grep toggles; R3-1 closed in round 4, R3-2 in round 5, R3-3/R3-4 open.
  Round 4 — LSP 4a (multi-server router + code actions/applyEdit + per-server status + inline
  staleness), project-wide search/Git Grep, settings coverage R3-1 (68 settings in 13 sections,
  structured editors/reset/Reload), virtual space R2-1 (block rectangles + vertical lines) and
  markdown-source smart editing; R4-1/R4-2 partly closed in round 5, R4-3…R4-5 open. The Lead's
  canonical-path integration fix for project replace (tab spellings via `canonicalFilePath`/
  `openTabPath`, `project_search_canonical_test.go`) landed with round 4.
- **Round 5 landed (2026-11-08).** Six tracks complete except LSP 4b increment E (task-22 stays
  in progress); this audit ran the gates and reviewed the diff (no scope creep; §7):
  - **LSP 4b A–D** (`docs/lsp.md`): `internal/tui/lsp_data.go` (debounced hint/token/fold
    refreshes, per-version caches), EOL inlay-hint virtual text + Toggle Inlay Hints,
    semantic-token overlay via `semanticTokenStyle`, `editor/lsp_folds.go` +
    `internal/uiadapter/lsp_folds.go` external fold source (merged by `document/fold.go`), and
    `internal/lsphost/lsp_data.go` pull diagnostics (one-source, cached) + `WorkspaceDiagnostics`
    port data. Increment E (diagnostics panel/quickfix) deferred; R5-1.
  - **Plugin API tier 1a** (`docs/plugins/`, API level 2): `internal/tui/pluginpanel/`
    (layout/paint/hit/focus of the v1 widget spec), `internal/tui/plugin_panels.go`
    (placement/lifecycle/input; floating, buffer and sidebar-section placements;
    caps 8 panels/2000 widgets), `plugin_menus.go` + `menubar/plugins.go`,
    `plugin_actionpopup.go`, `plugin/api_{panel,menu,i18n}.go`, the prompt-suggestion and
    mouse/focus/LSP event paths, `plugin/api_lsp.go` all-files diagnostics, theme overrides
    (`internal/tui/view.go` `SetThemeOverrides`), and 191-function `gvim.d.lua`/docs coverage.
    R5-2.
  - **Theme model/editor** (`docs/themes.md`): `theme/{keys_table,themefile,resolve,registry,
    overrides,color,form}.go` (143-key table, forms, extends/fallbacks, fixed-RGB named table for
    luminance/inspect), `internal/app/theme_registry.go` (atomic save/delete, catalog),
    `internal/tui/{theme_inspect,theme_editor}.go` (Ctrl+Right-Click inspector, native editor),
    hot reload through Reload Configuration, plugin overrides; the R4-3 settings mutex fix
    (`prepareSave` + `ioMu`, `TestSaveDoesNotHoldTheStateLockAcrossFileIO`) landed. R5-3.
  - **Terminal slice B** (`docs/terminal.md`): `internal/tui/dock/{selection,transcript}.go`,
    `internal/terminal/saved.go`, `internal/restore/terminal.go`, the two new config keys,
    Ctrl+F/Ctrl+R/Ctrl+A scrollback search, dead-shell scrollback, restart-into-transcript;
    R3-2 closed; R5-4.
  - **Project-search R4-2** (`docs/keybindings.md`, `docs/ui.md`): `internal/tui/searchpanel/
    {glob.go,types.go,panel.go}` (glob filter, checkbox match tree, Replace Scoped, history),
    Alt+Q list export, `internal/tui/project_search.go` wiring. R4-2 partially closed; R5-5.
  - **Status bar R2-3** (`docs/ui.md` §5): `internal/tui/status_elements.go` (`{filename}`,
    `{chord}`, `{clock}` + minute tick), frame paint; R2-3 closed, R5-6 for plugin tokens.
- **Integration fixes after the round-5 tracks (Lead, both green).** (a) LSP 4b visual
  verification with a real gopls server found the Toggle Inlay Hints state starting default-off:
  `m.lsp.inlay` was never initialised from `editor.enable_inlay_hints`. `New()` now sets it from
  `ViewOptions.EnableInlayHints` (`TestInlayHintsStartFromTheViewOptions`; three tests updated),
  and hints paint end-to-end with gopls `parameterNames`/inlay settings (real-server evidence for
  the LSP row). (b) `{filename}` showed an absolute path when the workspace root was
  canonicalised (`/private/var`) while the document kept its opened spelling (`/var`);
  `statusFilename` now resolves both through `canonicalFilePath` and falls back to the original
  spelling outside the root (`internal/tui/statusbar_residual_test.go`,
  `internal/app/status_filename_app_test.go`). Reviewed; `go test ./internal/tui ./internal/app`
  green after the fixes.
- The gaps the rounds opened are at the top of §5 (R1-1…R1-4, R2-4…R2-5, R3-3…R3-4,
  R4-1…R4-5, R5-1…R5-6; #8/#11, R2-1, R2-2, R2-3, R3-1 and R3-2 are closed); §5.5 is the
  round-6 plan built from the ranked backlog minus the closed items.
- The command palette table is generated from Fresh; keep `commands/gen/generate.py` and
  `tools/gen_menus.py` in the parity loop. Regenerating after a Fresh update is the cheapest way
  to find newly added commands/menus; `tools/gen_menus.py --check` reports drift.
- When a Fresh release lands, re-run the counts in §2 (commands, keymaps, themes, plugins,
  plugin API) and update the matrix rows for the areas that changed.

## 7. Verification limits and risks

- Statuses are read evidence from the tree at the time of writing. Rounds 2–5 had their full
  gate sets run by this audit on their trees, all green: go build ./..., CGO_ENABLED=0 go build
  ./..., go vet, golangci-lint 0 issues, gofmt clean, purity, import rules, go test ./...
  -count=1. Each diff was reviewed; round-1 statuses were verified by reading code and docs
  only. A feature that exists but is broken would still read "implemented": treat a status as a
  pointer, not proof.
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
- Round-2/3/4/5 diff reviews (this audit): no scope creep outside the declared tracks. Shared
  files stayed in scope (round 2 `keymap/menu.go`; round 3 `internal/tui/{model,view,frame,
  ports}.go`; round 4 `internal/tui/{model,ports,root_ports}.go` + `config/config.go`; round 5
  `pluginapi/*`, `plugin/limits.go`, `internal/tui/{model,view,frame,ports,sidebar}`,
  `config/config.go`, `tools/import_rules_check.sh` and the design records), and the generated
  tables, `refs/` and `_attic/` were not touched; no golden tests were added. Round-5 findings:
  (a) `internal/app/zz_lead_round5_shots_test.go` is a leftover temporary Lead visual-verification
  harness — delete it before committing; (b) `docs/plugins/porting.md` dropped HTTP from its
  "not provided" list although no HTTP API exists in the runtime (grep finds none) — restore it
  (R5-2); (c) the action-popup API blocks its caller where Fresh's `showActionPopup` returns a
  boolean and reports only through the hook (§4 item 32); (d) named colors were verified equal
  to Fresh in the reference source (§4 item 21); (e) the R4-3 settings-lock standing
  nit is fixed and tested
  (`TestSaveDoesNotHoldTheStateLockAcrossFileIO`); (f) two post-track integration fixes, both
  reviewed green and regression-tested: the inlay-hints toggle starts from
  `editor.enable_inlay_hints` (real-gopls evidence for LSP 4b) and `{filename}` resolves the
  canonical root/opened spelling (`canonicalFilePath`; status tests). Carried from round 4:
  resource-ops-first workspace-edit ordering/partial apply (§4 item 18, now R5-1), Git Grep's
  basic-regex query semantics (same as Fresh's plugin; R5-5), virtual-space rune-column block
  geometry (R4-4). Round-2's earlier notes still stand: `--restore --diff` is accepted and
  silently ineffective, and `hotExitEditor` is a documented temporary wrapper.
- The round-2 formatting nit (an ATX heading appended directly after a list item without a
  blank line) no longer reproduces in `docs/ui.md`, `docs/sidebar.md`, `docs/lsp.md`,
  `docs/settings.md` or `docs/keybindings.md` as checked for this refresh.
- The `migration/` docs, including GAP-v3's numbers, are historical (§1.4). Do not plan from
  them.

## 8. gvim-only additions (not parity requirements)

`gvim config show`/`config paths`, `gvim grammar`, the in-process `uitest` harness, the
`cmd/gvimdebug` PTY-served debug page, the screenshot sweep, and the config/UI-tree dump commands
are gvim tooling. They do not count toward Fresh parity, but they are the fastest way to verify
new feature work; keep them working.







