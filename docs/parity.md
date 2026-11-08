# Fresh parity record

Status: live working record for the "full Fresh feature parity" goal, first written 2026-11-08 and
refreshed after rounds 1–8 landed (round 1: workspace trust, sidebar Contents, status pills;
round 2: virtual space, keymaps, status bar/menus, sessions; round 3: settings page, terminal
scrollback/links/mouse, explorer options, palette preview/toggles; round 4: LSP router/edits,
project-wide search/replace + Git Grep, settings coverage, virtual-space block/vertical, markdown
smart editing; round 5: LSP 4b data rendering, plugin API tier 1a, theme model/editor, terminal
slice B, project-search residuals, status-bar elements; round 6: LSP 4b E diagnostics panel/
quickfix/progress, plugin tier 1b, the diagnostics_panel port, the Markdown compose core,
Contents fold_buffer, search preview/multi-line; round 7: LSP R6-5, plugin tier 1c-A store half,
git_log/live_diff ports, markdown fences/tables, overlay keymap contexts, a settings R4-3 slice;
round 8: plugin tier 1c-B/C rendering+seams, #15 compare API + compare_files port, markdown dual
+ table borders, settings R7-3, path_complete/lsp_help ports, live_diff/git_log full paths,
streaming project search + Git Grep globs). Every status below was checked in this tree or in
`refs/fresh`, not inferred from the old migration notes. Update the affected row when a round
lands instead of rewriting it.

Last verified: 2026-11-08, tree commit `81f1eca` plus the uncommitted round-8 work. Round 8's gate
set was run by this audit: go build ./..., CGO_ENABLED=0 go build ./..., go vet, golangci-lint 0
issues, gofmt clean, purity and import rules green; go test ./... -count=1 had one load-sensitive
timing failure on the loaded run (plugin `TestHeatBusyTimerIsThrottled`), which passes repeatedly
when re-run isolated. Fresh reference: the `refs/fresh` checkout as read on the same date.

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
LSP events, all-files diagnostics, theme overrides; `gvim.api_version` is 2 (191 functions then; 210 after round 6, in
`docs/plugins/gvim.d.lua`). **Theme model/editor.** `theme/` registry with Fresh's forms/extends/
fallbacks/named colors/hot reload, `docs/themes.md`, inspect-at-cursor, native editor with
save-as/delete and plugin overrides; the R4-3 settings mutex fix landed. **Terminal slice B.**
Selection/copy, `jump_to_end_on_output`/`mouse_drag_selects`, regex/case scrollback search,
dead-shell scrollback, restart-into-transcript and `terminal.json` persistence (read-only, no
re-run, no reflow) — R3-2 closed. **Project-search R4-2.** Match-tree checkboxes, Replace Scoped,
file globs, Alt+Q list export and 20-deep search history landed (streaming etc. deferred).
**Status bar R2-3.** `{filename}`, `{chord}` and `{clock}` paint now (R2-3 closed).

**Round 6 outcome (2026-11-08).** Six tracks landed; the LSP 4b E increment completes #7:
**LSP 4b A–E.** Inlay hints (EOL tier), semantic tokens, foldingRange external fold source and
pull diagnostics (round 5) plus a workspace diagnostics store, the native sidebar Diagnostics
panel (grouped file→severity, counts header, selection/jump/scroll, progress/empty states), the
quickfix Diagnostics List overlay and progress waiting states (round 6); task-22/29 complete.
**Plugin API tier 1b.** Interactive widget kinds (text/number/dropdown/radio/dualList, tree
editing/drag) with host-owned instance state and page mode; group-lite buffer groups
(splits of the current tab), setPanelContent styled entries, panel visibility/focus/title and
scroll-sync groups; theme-file APIs (schema/data/list/save/delete/reload/user dir) and
text_properties_at_cursor; API level stays 2. **diagnostics_panel port.** The first Fresh plugin
port (10 bundled plugins, 8 on by default; reduced Finder subset, no default key binding, Enter
closes the v1 widget panel; ~0.9 MiB, default set about 6.6 MiB of the 8 MiB gate then (6.75 after round 7). **Markdown
compose core** (`docs/markdown.md`): per-buffer page width resolution and centered margin,
hidden gutter, wrap at the measure with list/quote hanging indents, inline conceal of
emphasis/strong/code/link/strike with caret-line reveal, rules/quote bars, and source↔composed
mapping for caret, hit, selection, search highlights, diagnostics, folds and soft rows;
deferred tables, fence frames, dual view, heading polish and view-mode persistence. **Contents
fold_buffer R1-1.** The native `contents.fold_buffer` (user layer, restart; plugin key fallback)
publishes heading fold ranges through a version-guarded outline fold source; folding a Contents
row folds its section in the buffer (no fold command: Fresh's markdown_toc registers none).
**Search extras.** Selected-match preview column (palette preview conventions, bounded plain
text) and multi-line regex rows with exact replace ranges. Palette 222→224. No Lead code fixes
this round: the two integration fixes are in the Lead's harness/README, and the apparent
page-view-then-Contents-fold issue was a harness test with a single heading (no bug).

**Round 7 outcome (2026-11-08).** Six tracks landed, plugin tier 1c is partial and settings R4-3
is a slice:
**LSP R6-5.** Workspace edits now apply a server's `documentChanges` strictly in server order,
interleaving resource ops and text edits (created-file edits land on the created file; each open-
doc step re-checks the protocol/editor version at the step; stale steps skipped; closed files
opened as unsaved tabs then the previous tab restored) and a refused/failed file operation STOPS
the remaining changes with a partial-applied notice. This is deliberately stricter than Fresh
(which continues); docs/lsp.md records it. Workspace diagnostics for unopened files are pulled
from workspace-capable servers (5 s throttle, capability-revision/open-count re-ask, published
sets win, workspace-only rows marked `(not open)`, no `$/cancelRequest`), and `$/progress` text
(window/workDoneProgress/create + begin/report/end, cap 8) shows in the panel header/empty
state; `LSPSession.StartLanguage` was added for the settings page. **Plugin tier 1c-A (partial).**
The decorations store and Lua surface for virtual lines (above/below with gutter glyphs/text
overlays), conceals (activation/scope/composeOnly), soft breaks (indent/prefix), cursor-line
overlays, ranged scrollbar markers and namespace-scoped plugin folds landed with caps/docs; the
pane rendering (row-insertion/source↔display transform) and the 1c-C seams (split labels,
per-buffer window state, path/focus/active-window/group events, fold pane integration, line
targets) are deferred to round 8, so the API is present-but-unrendered and `gvim.has("decor_virtual_paint")` is false.
**Ports.** `git_log` (group-lite log/detail/hint splits,
streaming detail with a disk cache, Enter-to-file, 4 palette + 13 hidden actions, no default
bindings; reduced: no synthetic group tab, no HEAD watcher, hint-row toolbar, plugin-side diff
decorations) and `live_diff` (reduced rendering until 1c-B: line backgrounds/word emphasis/
gutter/scrollbar; the full virtual-line old-content path is written and gated on the missing
capability; baselines plugin-side via `git show`/`fs.read` + Myers diff, no host baseline API;
large-file refused) are in; 12 bundled plugins, 8 default-enabled (`flash`/`vi_mode` off,
`live_diff` opt-in, `git_log` off: with it the 9-plugin idle heap read 8.68 MiB over the 8 MiB
gate; the 8-plugin default set is 6.75 MiB). **Markdown R6-2.** Code-fence frames (language
label, rails/padding, syntax kinds inside, delimiter reveal, nesting fallback) and table
box-drawing (mid border from the source separator, version-stable widths, alignment, per-row
reveal, exact mapping) landed; table top/bottom border rows and multi-visual-row cells remained
blocked on the page-view extra-row hook, and dual remained a native per-pane view-mode task.
**Keymap R2-4 closed.** The palette/find/prompt/menu/settings/dock
overlays consult the keymap in their Fresh contexts before built-ins with fall-through; presets
grew (+48 default, +7 macos, +20 emacs, +2 macos-gui; own/in-effect default 245/243, macos
60/266, macos-gui 52/317, emacs 98/309, vscode 9/248), the loader accepts the overlay contexts
and warns+skips multi-key bindings there. Residual R7-4. **Settings R4-3 (partial).** The
Language Servers page (effective builtin/user/universal rows, live manager state, start/restart/
stop via `SettingsPort.Act`, whole-array validated JSON editing, project-layer locks) and the
read-only Environment facts landed on new page plumbing (info/locked/inherited/action/filter/
list-path/JSON); per-language structured values/reset-vs-inherit, the Plugins and Keybindings
pages, docs and env detection remain (R7-3). The Lead fixed four wrap-up lint findings in the
new diagpanel/settings files (no semantic change); task-36 and task-41 stay in progress.

**Round 8 outcome (2026-11-08).** Seven tracks landed; plugin tier 1c is complete except for its
documented remainder (R8-1) and settings R7-3 is closed:
**Plugin tier 1c-B/C.** One shared source↔display row transform (`rows_extra.go`: extra rows and
soft breaks, cached per line and used by every row/caret/hit/selection/scroll calculation;
composition order wrap+folds → markdown compose → conceal → soft breaks → virtual lines →
cursor-line overlay → selection) now paints virtual lines above/below (gutter glyph, text
overlays, priority, 64-row cap), conceal (replacement/activation/scope/composeOnly without
changing columns), soft breaks (indent/prefix), the cursor-line overlay and plugin folds (fold
pane integration + fold-first toggle); the same hook carries the markdown page-view table
top/bottom borders (`Pane.PageExtraRows`). 1c-C seams: split labels, per-buffer window state
(`gvim.buf.get/set_window_state`), open/focus/close split wrappers, path_changed/
focus_gained/active_window_changed/buffer_group_closed events; `gvim.has
("decor_virtual_paint")` is true (FakeHost features switch). A group-layout ratio inversion
(first-child share vs SplitOptions' second-window share) was found and fixed with a
regression test. **#15 compare API + compare_files.** pluginapi CompareResult/
DiffSource/DiffHost with a pure compare-geometry package (UTF-8-safe spans, jump-at-gap,
positional pairing), Lua `gvim.diff.compare_buffers/replace_range` (byte model, one-edit/one-
undo, false-not-error refusals, `eol_only` always false because gvim normalises EOLs), and the
compare_files port (8 Fresh commands/no default keys, group-lite splits, hunk-anchored scroll
sync, Fresh theme decorations, copy/take via replace_range, binary/large refusal, opt-in
default_enabled=false). Native Compare remains the default surface; enabling the plugin adds
duplicate palette rows (documented). **Markdown dual.** A native 50/50 source+compose split of
the same document through a derived per-pane page-view override (document settings stay the
single-pane default), focus on source, line-anchored compare-style scroll sync, shared
document/caret with compose-pane hit/selection mapping, five palette rows (Source/Rendered/Dual/
Toggle Dual/Cycle Mode; palette 224→229), mode notices; closing/compare/tab change clears it.
Table top/bottom borders landed via the page-view extra-row hook. **Settings R7-3 closed.**
Per-language structured values with inherited display/reset-vs-inherit (whole-object writes),
Languages entries pages, Plugins page (inventory, user-layer enable/disable with rollback,
reload, read-only settings), read-only Keybindings page + Open Keybinding Editor action;
docs/settings.md and the docs/lsp settings paragraph updated. **Small ports.** path_complete
(faithful completion engine, exported `complete()`, one prompt command; no `prompt_changed` seam —
documented residual) and lsp_help (event-driven install hints: status row on
`lsp_server_error`, action popup on `lsp_status_clicked` with 16 language hints + native quick
actions; payload limitations documented); the real-host palette investigation closed as a
harness/order artefact with a permanent real-host palette test. Bundled set 15 (10 default-
enabled), idle heap ~7.9 MiB of the 8 MiB gate (headroom ~0.08); the five off are flash,
vi_mode, live_diff, git_log and compare_files. **R7-2 follow-ups.** live_diff's full virtual-
line rendering is active (capability true; both modes tested; real-host test), git_log folds +
per-commit detail scroll restore via window state (real-host from palette), group ratio defect
fixed; the round-7 palette finding is a harness/order artefact. **Search R6-3 slice.**
Streaming/batched project search (SearchBatch/BatchSearcher, ≤128 files/≤1000 matches per
batch, one goroutine, cancel on query change/close, one-shot fallback; panel appends
progressively with "searching N/M files", selection preserved, partial-replace plans from the
shown rows) and a Git Grep Files field (git pathspecs, validation/invalid-block, passed after
"--"). The Lead's integration edits: one lint fix in internal/app/projectsearch.go and
temp-harness deletions; the plugin fixed the reload-picker regression (Ranked query/rowsQuery)
that was the last red test.

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

Round-6 evidence adds `docs/markdown.md`, the updated `docs/lsp.md`/`docs/sidebar.md`/
`docs/settings.md`/`docs/ui.md`/`docs/keybindings.md` and plugin docs (`api-ui.md`,
`api-buffers.md`, `api-data.md`, `bundled.md`, `limits.md`, `gvim.d.lua`/`events.md`), and the new
code: `internal/tui/pluginpanel/{interactive,interactive_state}.go`, `internal/tui/plugin_groups.go`,
`pluginapi/buffergroup.go`, `plugin/api_groups.go`, `internal/tui/plugin_themes.go`,
`internal/tui/diagpanel.go` + `commands/lsp_commands.go`, `internal/tui/editorpane/
{compose,compose_layout,pageview}.go` + `syntax/markdown_markers.go`, `config/contents.go` +
`editor/outline_folds.go` + `internal/uiadapter/outline_folds.go`, `plugin/bundled/plugins/
diagnostics_panel/` and `internal/tui/searchpanel/` preview/multi-line, with the round-6 tests
named in the rows.

Round-7 evidence adds `docs/lsp.md`, `docs/markdown.md`, `docs/keybindings.md`,
`docs/settings.md` and the plugin docs (`api-ui.md` virtual-text surface, `bundled.md`
git_log/live_diff/memory, `porting.md` baseline/streaming patterns, `gvim.d.lua` 221
functions), and the new code: `decorations/{decor,line,batch}.go` + the tier-1c kinds,
`plugin/api_decor.go` (697 lines) + fold namespaces, `internal/tui/plugin_folds.go`,
`plugin/bundled/plugins/{git_log,live_diff}/`, `internal/tui/editorpane/compose_{fence,
table}.go` + `syntax/markdown_{fences,tables}.go`, `internal/tui/overlay_keys.go` +
`keymap/*.json`, `internal/tui/workspace_edit.go` + `lsp/{progress,requests}.go` +
`internal/lsphost/lsp_data.go`, `internal/app/settings_{lsp,plugins,runtime}.go` +
`internal/tui/{settings_actions,settings_structure}.go`, with the round-7 tests named below.

Round-8 evidence adds the updated `docs/markdown.md` (dual), `docs/compare.md` (port),
`docs/settings.md`/`docs/lsp.md` (R7-3 page), `docs/ui.md`/`docs/keybindings.md` (streaming
search, Git Grep paths), the plugin docs (api-ui virtual surface painted, api-system gvim.diff,
events new names, limits caps, bundled 15/10/5, porting path_complete/compare residuals,
gvim.d.lua 231 functions) and the new code: `internal/tui/editorpane/{rows_extra,conceal}.go`,
`internal/tui/{dual,plugin_seams,plugin_folds}.go`, `pluginapi/compare.go` +
`plugin/compare_geom.go` + `internal/pluginhost/compare.go`, the compare_files/path_complete/
lsp_help plugin dirs, `internal/app/settings_{languages,plugins,keybindings}.go`,
`internal/tui/{searchpanel,project_search}.go` streaming, plus the round-8 tests named below.

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
| Palette commands (runtime) | 206 core (Fresh's `CommandDef` table) | 229 | 206 generated + 7 hand-written native rows + 4 plugin-management + 3 round-4 + 2 round-5 + 2 round-6 + 5 round-8 Markdown mode rows (Source, Rendered, Dual, Toggle Dual, Cycle Mode); `commands/palette_test.go` pins 229. Plugin-command rows (compare_files, path_complete, git_log, ...) appear at run time only when their plugin is enabled. All rows have a handler or a documented reason; the only bound-action gap is `file_explorer_search_backspace`. |
| Documented "not applicable" commands | n/a | 4 | `internal/tui/notice_na.go` (detach, update, update log, wave). Inspect Theme now runs (round 5), and the round-1 trust command runs. |
| Menus / menu actions | 8 / 103 static actions, 31 separators, 2 static submenus + 1 dynamic | 8 / 103 static actions, 31 separators, 2 static submenus + the dynamic theme submenu | Static parity is exact against Fresh's `translated_menus()` (103 actions / 31 separators / 2 static submenus); the dynamic "Copy with Formatting" submenu is filled at run time from the theme catalog (`internal/tui/menubar/dynamic.go`). Round 5 adds plugin-contributed rows (`gvim.menu.add_item`, `internal/tui/menubar/plugins.go`, at most 32 rows, owner-cleaned). Help's label row says "gvim" where Fresh writes its version. |
| Keymap presets | 5 | 5 | Same names and inheritance graph. Round 7 closed R2-4: the overlay contexts (`prompt`, `searchPrompt`, `popup`, `menu`, `settings`, `dock`) consult the keymap before built-ins with fall-through; own/in-effect counts are now default 245/243, macos 60/266, macos-gui 52/317, emacs 98/309, vscode 9/248 (`keymap/common_set_test.go`, `docs/keybindings.md`). `completion`/`compositeBuffer` are not emitted; residual R7-4. |
| Bundled plugins | 50 | 15 (10 on by default) | Round 8 added `compare_files` (15th, off), `path_complete` and `lsp_help` (both on); off are flash, vi_mode, live_diff, git_log and compare_files (reasons: memory/heap gate, Fresh-opt-in, native-compare duplication). `docs/plugins/bundled.md` documents the fifteen; `markdown_toc` was retired in round 1. |
| Bundled plugin commands | ~180 | 30 registration sites | from `gvim.command.register` call sites: compare_files registers its 8 commands through one table, path_complete 1, lsp_help 0 (event-driven); earlier ports keep their rows. |
| Themes | 10 built-in themes, theme files by name/path/URL/`builtin://`, `extends` inheritance, attributes on every color, theme editor, inspect-at-cursor, plugin theme overrides, background images | 10 built-ins + user themes (round 5); plugin theme-file APIs (round 6) | Round 5 landed the registry/forms/extends/fallbacks/named colors/inspect/native editor/overrides (`docs/themes.md`); round 6 added plugin theme-file APIs (schema/data/all/list/user-dir/file-exists/save/delete/reload, ≤1 MiB validated) so a theme_editor port is unblocked. Residuals R5-3. |
| LSP server entries | built-in + plugin helpers + universal servers | 62 built-in entries | `lsp/manager/defaults.go`; rounds 4–6 route lists/universal entries and draw hints/tokens/folds/diagnostics, but no built-in universal server ships (Fresh's quicklsp) and `process_limits` is parsed and unenforced (R6-5). |
| Bundled plugin idle heap | n/a | about 7.9 MiB of the 8 MiB gate (10 default-enabled; ~9.8 MiB session baseline) | `TestBundledPluginsIdleHeap`, `docs/plugins/bundled.md` memory table; headroom is ~0.08 MiB, which is why compare_files/git_log ship off. |
| Plugin API surface | ~372 methods | 231 functions | `docs/plugins/gvim.d.lua` is 1441 lines, API level 2; round 8 completed the tier-1c painted surface (virtual lines/conceal/soft breaks/cursor-line/plugin folds), added the compare/diff API (`gvim.diff`) and 1c-C seams (split labels, window state, four events). Remaining gaps are R8-1 (and the recorded composite substitution). |
| Settings page | schema-driven page (~14k LOC in refs/fresh) over every setting | 69 settings in 14 sections + injected Languages entries/Language Servers/Plugins/Keybindings/Environment pages | Rounds 3–6 built the page/structured editors/reset/Reload/Contents; round 7 added the non-scalar plumbing + Language Servers + Environment; round 8 closed R7-3 with per-language structured values + reset-vs-inherit display, the Plugins page (enable/disable with rollback, reload, read-only settings) and the read-only Keybindings page + editor action (`internal/app/settings_{languages,plugins,keybindings}.go`, `docs/settings.md`). Residuals R8-3. |
| Terminal | PTY terminal panes and utility dock, live/scrollback modes per split, keyboard capture, clickable paths (OSC 7), restart exited process + agent resume, shell override, send selection, mouse forwarding/drag-select, close_on_exit, terminal tabs, ConPTY on Windows | **partial** (slices A+B; R3-2 closed) | `config/terminal.go`, `internal/terminal/{scrollback,mouse,saved}.go`, `internal/tui/dock/{selection,transcript,scrollback,input,panel,render,ports}.go` + `dock_glue.go`, `internal/restore/terminal.go`, `internal/uiadapter/terminal.go`, `docs/terminal.md`, tests `internal/terminal/*_test.go`, `internal/tui/dock/*_test.go`, `internal/restore/terminal_test.go` | Slices A+B: `terminal.shell`/`close_on_exit`/`mouse_forwarding`/`jump_to_end_on_output`/`mouse_drag_selects`, gvim-only `scrollback_lines`; scrollback pager + Ctrl+F search with regex/case; OSC 7 + Ctrl+Click links; read-only text selection (live-drag into scrollback, Shift+drag bypassing forwarding, word/line clicks in scrollback only, painted + copied, selection holds the view), dead-shell scrollback, restart-into-transcript (bounded prior rows, last OSC 7 cwd), `terminal.json` transcript persistence (read-only/dead on restore; no re-run — §4 item 26). Ctrl+Space is the dock's key (removed from the keymap, §4 item 25). Residuals R5-4: reflow (x/vt has none; saved width recorded only), automatic agent re-exec (`resume_agents` accepted-ignored), terminal tabs, child OSC 52/kitty/`pass_ctrl_v_without_text`, Windows ConPTY, drag auto-scroll and word-wise drag extension, hover underline/`file:line:col` links, split-terminal `close_on_exit`. |
| Explorer options | `file_explorer.width/side/follow_active_buffer/auto_open_on_last_buffer_close/compact_directories/preview_tabs/…` + sticky parents | 5 options + sticky parents implemented | width percent/columns with grip, left/right side geometry, follow + reveal, auto-open on last close, compact chains, sticky band; side toggle session-only and 4 keys accepted-ignored (R3-3). |
| Finder / Live Grep | file preview, Live Grep toolbar (case/word/regex, files/buffers/ignored/terminals/workspaces), quickfix, streaming | preview column + 6 Live Grep toggles | `internal/tui/palette/{preview,grep}.go`; deferred quickfix, terminals/workspaces providers, streaming, Live Grep row preview, Resume cache (R3-4). |
| Project search / replace | plugin Search & Replace panel (match tree, scoped replace, globs, quickfix) + Git Grep | native panel + Git Grep (rounds 4–6) | Rounds 4–5 landed the panel/ports/checkboxes/scoped replace/globs/list export/history; round 6 added the selected-match preview column and multi-line match rows (exact ranges kept in replace): `internal/tui/searchpanel/{draw,panel,types}.go`, `docs/ui.md`. Residuals R6-3. |
| e2e feature index | 413 test files | not comparable | gvim uses Go unit tests; Fresh's file names are the feature index used below. |

## 3. Matrix by area

### 3.1 Engine and editing

| Area | Fresh features (evidence) | gvim status | gvim evidence | Notable sub-gaps |
|---|---|---|---|---|
| Text model, large files, encodings | rune text, undo/redo, two-mode large-file support, encoding detect/convert (UTF-8/16, Latin-1, CP125x, GBK/GB18030, Shift-JIS, EUC-KR), EOL detect | **implemented** | `buffer/`, `document/`, `encoding/`, `editor/large_*`, `editor/files_lineending*`, `docs/architecture.md`; Fresh `docs/features/encoding.md` | None found. Detection heuristics may order differently; per-file reload uses the same palette command. |
| Editing core, smart editing, vi | auto-indent/close/surround, smart home/backspace, bracket match, comment, case, transpose, sort/trim, shell command, Markdown source smart editing + page-view compose/fences/tables, vi-mode plugin | **implemented** | `document/smart*.go`, `editor/comment.go`, `document/case.go`, `editor/shell.go`, `editor/markdown.go`, `internal/tui/editorpane/{compose,compose_fence,compose_table,pageview}.go`, `plugin/bundled/plugins/vi_mode`; Fresh `docs/features/editing.md` | Markdown compose/fences/tables landed rounds 6–7 (see Display row, `docs/markdown.md`); remaining Markdown gaps are R7-2/R6-2 (dual, borders/multi-row, heading polish, page-view persistence). Fresh's paragraph motions and "insert/open line" exist; replacement-mode (`R`) is documented as unsupported in the vi plugin. |
| Selection, multi-cursor, marks | multi-cursor (Ctrl+D, above/below, line ends), block selection, mark mode, mouse word/line/drag selection | **implemented** | `document/cursors*.go`, `document/block.go`, `editor/marks.go`, `internal/tui/motion_mouse.go`, keys in `keymap/default.json`; Fresh `docs/features/editing.md` | `add_cursors_to_line_ends` is not bound by default (present in Fresh's preset); Fresh's double-click word drag is implemented. |
| Undo, save pipeline, file metadata | one-undo-step edits, atomic saves, interrupted-save recovery, permissions/read-only, final-newline/trim on save, auto-save, auto-revert | **implemented** | `document/save_plan.go`, `atomicfile/`, `editor/files_recovery.go`, `editor/interrupted.go`, `internal/restore`, `docs/architecture.md` | Fresh's `sudo save prompt` is missing (no `sudo` path found); permission/ownership preservation is present. |
| Search & replace (in-buffer) | find, replace, query replace (y/n/!/q), find in selection, selection next/prev, case/whole-word/regex toggles, confirm-each, search history, match counts | **implemented** | `search/`, `internal/tui/find/`, `internal/tui/find_route.go`, tests `find_*_test.go`; Fresh `docs/features/search-replace.md` | Project-wide search/replace and Git Grep landed rounds 4–8 (next row); remaining search-panel residuals are preview scrolling/syntax colours, a multi-line match longer than the preview, large-file replace and provider scopes (R8-5). |
| Navigation, bookmarks, macros | position history, go to line/bracket, bookmarks 0-9 (set/jump/list), macros 0-9 + save-to-init/promote-to-command | **partial** | `editor/nav*.go`, `editor/goto.go`, `editor/marks.go`, `internal/tui/root_bookmarks.go`, `internal/tui/macro.go`; Fresh `docs/features/editing.md` | `List Bookmarks` is a stub ("editor does not list its bookmark slots", `internal/tui/root_commands.go:16`). One macro slot only: record/play/list work, `Macro: Save to init.ts`/`Promote to command` write an `init.lua` block (Fresh writes `init.ts`), no macro registers 0-9. |
| Display & view options | line numbers/relative, wrap + wrap_column + wrap indent, page view, virtual space (block/vertical), rulers, indent guides + rainbow, whitespace marks incl. newlines/CR, current line/column, occurrence, post-EOF tilde, scrollbars + markers, smooth scroll, inline diagnostics + staleness, inlay hints, semantic tokens, Markdown page view (compose, fences, tables, dual split), animations, screensaver | **partial** (R2+R4+R5+R6+R7+R8 landed the user-visible subset) | `document/{virtualspace,block}.go`, `editor/{virtualspace,block,smart}.go`, `internal/tui/editorpane/{inline_diag,virtual_block,virtual_lines,compose,compose_layout,compose_fence,compose_table,pageview,rows_extra,conceal}.go` + `syntax/markdown_{markers,fences,tables}.go`, `internal/tui/{virtual_space,pageview,dual}.go`, `docs/markdown.md`; Fresh `docs/features/editing.md` | Round 6 landed the compose core, round 7 fences/tables, round 8 the native source+compose dual split (derived per-pane view mode, line-anchored sync, five mode commands) and the table top/bottom border rows through the 1c-B page-view extra-row hook. Still open (R8-2/R8-1): multi-visual-row table cells and nested fences, heading scrollbar markers/marker conceal, page-view session persistence + free per-split mode, mapped occurrences/rulers/guides/whitespace and composed plugin overlays, task/image/HTML conceal, exact row/byte dual scroll anchors + a native Markdown menu row + multiple duals; R4-4 virtual-space limits; style-only keys out of `ViewOptions` (§4 item 9). |
| Folding | indent folds + LSP `foldingRange` + outline/Contents folds + plugin namespace folds, fold indicators, fold-first toggle, per-buffer toggle | **implemented** core; all four sources | `editor/fold.go`, `document/fold.go`, `editor/{lsp_folds,outline_folds}.go` + `internal/uiadapter/{lsp,outline}_folds.go`, `internal/tui/plugin_folds.go` (namespace ranges, pane integration, fold-first toggle, 2000/namespace cap), `internal/tui/editorpane/` gutter; tests `decorations/tier1c_test.go`, `plugin/decor_tier1c_test.go`, `editor/{lsp,outline}_folds_test.go` | Round 5 merged LSP ranges, round 6 the Contents outline source, round 7 the plugin namespace store; round 8 integrated plugin folds into the pane (drawn/toggled like any fold, fold-first toggle from the command/plugin API). Residuals: a source that changes ranges without an edit refreshes on the next edit/open; Contents publishes only while its section is open (§4 item 34). |
| Splits, tabs, panes, windows | splits (vertical/horizontal), tab strip, tab drag, preview tabs, buffer groups, split labels, per-buffer window state, multiple windows/workspaces | **partial** | `internal/tui/split*`, `internal/tui/tabs.go`, `internal/tui/tab_drag.go`, `internal/tui/tab_drop.go`, preview tabs in `internal/tui/model.go`, plugin group-lite in `internal/tui/plugin_groups.go` + `pluginapi/buffergroup.go`, 1c-C seams in `internal/tui/{plugin_seams,plugin_events}.go`; Fresh `docs/features/navigation.md` | Round 6 added plugin groups (splits of the current tab, panel buffers/content/title/visibility/focus, ownership cleanup, scroll sync); round 8 added 1c-C split labels, per-buffer window state, open/focus/close wrappers and the path_changed/focus_gained/active_window_changed/buffer_group_closed events, and fixed the layout ratio inversion (first-child share vs SplitOptions second-window share) with a regression test. Still open (R8-1): no synthetic group tab/per-panel views/fixed-height leaves, no second window/workspace model; "Move tab to other split"/"Extract Tab to New Workspace" are notices. |
| Compare files | `fresh --diff A B`, editable side-by-side, change copy/take, next/prev difference, switch side, scrollbar marks, plugin compare API (compareBuffers/replaceRange) and the compare_files plugin | **implemented** (native view + plugin API + opt-in plugin port) | `internal/tui/compare*.go`, `docs/compare.md`, `compare_*_test.go`, `internal/cli` `--diff`, `pluginapi/compare.go` + `internal/pluginhost/compare.go` (CompareResult/DiffSource/DiffHost, one-edit/one-undo replaceRange), `plugin/compare_geom.go` + tests, Lua `gvim.diff.compare_buffers/replace_range`, `plugin/bundled/plugins/compare_files/` (8 commands, group-lite splits, hunk-anchored scroll sync, copy/take, opt-in); Fresh `docs/features/compare-files.md` | Native Compare is gvim's default surface for Fresh's Compare Files (two file tabs, fillers, hunks, take/copy, synced scroll, large-file refusal; §4 item 1 for presentation). Round 8 added the plugin API and the port: group-lite splits of the two file buffers (opened as ordinary tabs when needed; Close closes only ones it opened when unmodified), hunk-anchored scroll sync, Fresh theme decorations, filler rows gated on `decor_virtual_paint`, copy/take through replace_range (one undo per merge), binary/large refusal, 8 Fresh commands/no default keys, opt-in for the heap gate (enabling duplicates the native palette rows). Residuals R8-4: no synthetic group tab, no EOL-only hunks (EOL normalisation), filler rows need the paint capability, positional pairing fallback, ui.input instead of Fresh's picker, no `--diff` launch hook/script-facing export. |

### 3.2 Project, chrome and settings

| Area | Fresh features (evidence) | gvim status | gvim evidence | Notable sub-gaps |
|---|---|---|---|---|
| File explorer and sidebar | tree, file create/delete/rename/copy/cut/paste, hidden/gitignored toggles, git status marks, context menu, async reload, width/side, follow active buffer, compact chains, sticky parents, plugin rows/decorations, sidebar sections, Contents fold_buffer | **partial** (R3 options; R1 Contents; R6 fold_buffer) | `explorer/{options,sticky}.go`, `internal/tui/explorer/`, `internal/tui/{root_explorer,markdown_toc}.go`, `internal/tui/sidebar/` (content sections + `KindPlugin`/`KindDiagnostics`), `config/contents.go`, `outline/`, `docs/sidebar.md` §9 | Round 3 landed the explorer options; round 6 completed R1-1: the native `contents.fold_buffer` (user layer, restart; plugin-key fallback) folds the buffer by heading through a version-guarded outline fold source (`editor/outline_folds.go`, `internal/uiadapter/outline_folds.go`, tests `editor/outline_folds_test.go`, `internal/tui/markdown_toc_fold_test.go`, `config/contents_test.go`); the section publishes ranges only while open and re-applies folded headings by byte offset after a rescan. Still open: plugin explorer rows/decorations/slots; R1-2 sidebar persistence, rows/divider session state; `respect_gitignore`/`custom_ignore_patterns`/`preview_tabs`/glyph keys accepted-ignored; side toggle session-only; Contents: no fold command (Fresh parity). |
| Finder, quick open, live grep | Ctrl+P modes (file/command/line/buffer), fuzzy ranking, streaming enumeration, finder preview, live grep with provider toggles (case/regex/word/files/buffers/ignored/terminals/workspaces), quickfix export, resume | **partial** (R3 landed the preview and toggles) | `finder/finder.go` (`EnumerateAll`, hidden/ignored walk), `internal/tui/palette/{preview,grep,palette,draw}.go`, `internal/app/{preview,live_grep}.go`, `docs/ui.md` palette details, tests `palette/{preview,grep,palette}_test.go`, `internal/app/live_grep_test.go`; Fresh `docs/features/command-palette.md`, `search-replace.md` | Round 3 landed: the file-mode preview column (head of the selected file, 16 KiB / 12 lines, `path:line:col` parsing, target line highlighted, binary/empty/unreadable placeholders, dropped under 64 inner cells) and Live Grep's toolbar — case (seeded by `editor.search.case_sensitive`), regex (on by default as Fresh's), whole word, files, buffers (1 MiB snapshot per buffer) and ignored/hidden sources, with match counts and error rows. Still open: quickfix export, terminals/workspaces providers (need the plugin/results-panel work), streaming provider model, preview for Live Grep rows (file mode only), syntax colours/scrolling in the preview, and Resume re-runs the search instead of restoring cached matches (R3-4). |
| Project-wide search, replace and Git Grep | plugin Search & Replace panel: grouped match tree with per-file/per-match selection, scoped replace, Case/Whole/Regex toggles, current-file/all-files, file globs, 10k cap, unsaved buffers, quickfix export, search history, selected-match preview, multi-line matches, streaming results; git grep finder with case toggle and pathspec field | **implemented** core; **partial** against the plugin (R4–R8) | `search/plan.go`, `internal/app/{projectsearch,gitgrep}.go` (streaming batches, cancellation, Git Grep pathspecs), `internal/tui/searchpanel/{glob,types,panel,draw}.go` + `internal/tui/project_search.go`, shared apply helper `internal/tui/workspace_edit.go`, `commands/search_commands.go`, `docs/keybindings.md`, `docs/ui.md`; Fresh `plugins/search_replace.ts`, `plugins/git_grep.ts` | Round 8 added streaming/batched results (whole-file batches ≤128 files/≤1000 matches, one goroutine per request with cancel on query change/close, one-shot fallback; the panel appends progressively with "searching N/M files", selection preserved and partial-replace plans from the shown rows) and a Git Grep Files field (git pathspecs, validated, invalid blocks; passed after "--"; one git grep call with its 100-row cap). Residuals R8-5: preview that scrolls/syntax-colours, a multi-line match longer than the preview (first lines), large-file replace, provider scopes beyond files/current buffer. |
| Command palette and command table | 206 core commands + plugin commands, contexts/availability, prefix modes, key hints | **implemented** core / **partial** plugin commands | `commands/` (206 generated + native rows), `internal/tui/palette/`; Fresh `docs/features/command-palette.md` | The generated core table is exact (206/206). The runtime palette has 229 rows: 206 generated + 7 native + 4 plugin-management + 3 round-4 + 2 round-5 + 2 round-6 + 5 round-8 Markdown mode rows — `commands/palette_test.go`. Bundled plugin commands add rows only while their plugins are enabled (30 registration sites across the fifteen). |
| Menu bar, context menus, help, welcome | 8 menus, 103 static actions, dynamic theme submenu, mnemonics/underlined accelerators, context menus, help overlay, welcome screen, dashboard | **implemented** menus/help/welcome; dashboard **missing** (plugin) | `internal/tui/menubar/{menu_table_gen,menubar,dynamic}.go`, `keymap/menu.go` (`Map.MenuMnemonic`), `internal/tui/menu_mnemonic_test.go`, `internal/tui/menubar/mnemonic_test.go`, `internal/tui/context_menu.go`, `internal/tui/help.go`, `internal/tui/welcome.go`, `docs/ui.md` §6; Fresh `docs/internal/editor-ux-features.md`, `docs/features/dashboard.md` | Menus match Fresh's static table exactly (103 actions, 31 separators, 2 static submenus), and the dynamic "Copy with Formatting" submenu lists the 10 bundled themes (`menubar/dynamic.go`). Round 2 added the accelerators: underlined keymap-assigned letters and Alt+letter through ordinary keymap resolution, F10 for the bar, `editor.menu_bar_mnemonics` off removes both; dropdown rows carry no mnemonics (as in Fresh). Help's label says "gvim" instead of Fresh's version string. Residual: the dashboard plugin (backlog #14), not the menu system. |
| Status bar | configurable element lists (Fresh's defaults: left `{trust} {remote} {terminal_restart} {cursor} {diagnostics} {cursor_count} {messages}`; right `{read_only} {line_ending} {encoding} {language} {lsp} {warnings} {update} {palette}`), separator, per-element click actions, squeeze rule, `{filename}`/`{chord}`/`{clock}` | **implemented** (R2; parsed-but-unpainted elements documented) | `internal/tui/frame/frame.go` `StatusRuns`/`DefaultStatusLeft/Right`, `internal/tui/model.go` `statusView`/`statusRow`, `internal/tui/status_elements.go` (filename/chord/clock/tick), tests `statusbar_elements_test.go` + `statusbar_residual_test.go`, `docs/ui.md` §5; Fresh `config.rs` `StatusBarConfig`, `view/ui/status_bar.rs` | R2-3 closed in round 5: `{filename}` (workspace-relative or absolute path, document name when pathless, Fresh's " [+]" / " [RO]" markers), `{chord}` (shown only while a multi-key sequence waits, gvim's label style, muted — §4 item 22) and `{clock}` (24-hour `HH:MM`, static colon as Fresh's code — §4 item 23; a minute tick redraws while configured, a config change takes effect next launch). The filename resolves the root and the document through canonicalFilePath (the /private/var vs /var class) and falls back to the opened spelling outside the root (integration fix, statusbar_residual_test + status_filename_app_test). Still unpainted: plugin tokens `{plugin:token}`; `{warnings}`/`{update}` are deliberately empty (no warning-count domain/updater). Plugin status items keep their anchors. |
| Config layers and Settings UI | 4 layers (system/user/project/session), per-buffer overrides, config write from toggles, Settings UI (tree, widgets, layer switch, save), `init.ts`, config reload, LSP/plugin/keybinding/env sections | **partial** (R3+R4 built the page; R6 Contents; R7 Language Servers/Environment; R8 closed R7-3) | `config/settings_schema.go` (69 scalar rows, 14 sections), `config/settings_structured.go`, `config/contents.go`, `config/patch.go`, `internal/app/settingssvc.go` (Snapshot/Validate/Save/Reload/Apply/Act), `internal/app/settings_{lsp,runtime,plugins,languages,keybindings}.go`, `internal/tui/{settings,settings_structure,settings_actions}.go`, `commands/settings_commands.go`, `docs/settings.md` | Rounds 3–7 built the page/structured editors/reset/Reload/Contents, the non-scalar plumbing and the Language Servers/Environment pages. Round 8 closed R7-3: per-language structured values with inherited display and reset-vs-inherit (whole-object writes), Languages entries pages, Plugins page (runtime inventory, user-layer enable/disable with file rollback, reload, read-only settings), read-only Keybindings page + Open Keybinding Editor action. Residuals R8-3: plugin declared-settings schema/values API (plugin settings read-only keys today), plugin gallery/install page, no in-page key capture (deliberate), env detection (`env.detectors`), per-field LSP dialogs; `gvim config show` trust (R1-3). |
| Keymaps and keybinding editor | 5 presets, user keybindings, unbind/noop, chords, custom keybinding maps, keybinding editor, overlay contexts | **partial** (R2 landed the data; R7 closed the overlay contexts) | `keymap/*.json` (inheritance graph), `keymap/common_set_test.go` (own/in-effect counts), `keymap/menu.go`, `internal/keybindings/`, `internal/tui/overlay_keys.go` (overlay router), `internal/tui/keybind_editor.go`, `docs/keybindings.md`, `docs/architecture.md` | Round 2 rebuilt the presets; round 7 closed R2-4: the prompt/searchPrompt/popup/menu/settings/dock overlays consult the keymap before built-ins with fall-through; presets own/in-effect are now default 245/243, macos 60/266, macos-gui 52/317, emacs 98/309, vscode 9/248; the loader accepts the overlay contexts and warns+skips multi-key bindings there. Residuals R7-4: single-key overlay bindings only, unbind cannot disable a built-in handler, text-field selection/clipboard/suggestion actions are not surfaces, and completion/compositeBuffer contexts, custom settings/theme dialogs and plugin panels are outside the router. Custom `keybinding_maps` are still refused with a warning. |
| Themes and appearance | 10 built-in themes, theme files by name/path/URL/`builtin://`, `extends` inheritance, attributes on every color, theme editor, inspect-at-cursor, plugin theme overrides, background images | **partial** (R5 model/editor; R6 plugin theme-file APIs) | `theme/` (keys table/registry/resolve/themefile/overrides), `internal/tui/style/`, `internal/tui/{theme_inspect,theme_editor}.go`, `internal/app/theme_registry.go`, `internal/tui/plugin_themes.go`, `docs/themes.md` | Round 5 landed the registry/forms/extends/fallbacks/named colors/inspect/native editor/overrides; round 6 added the plugin theme-file APIs (schema, data, list, user dir, file exists/save/delete/reload, ≤1 MiB write validated) used by the future theme_editor port. Residuals R5-3: URL/packaged themes, colour picker/mouse editing, per-cell provenance, Fresh themes' accent/popup colors, settings preview, background images. |

### 3.3 Services

| Area | Fresh features (evidence) | gvim status | gvim evidence | Notable sub-gaps |
|---|---|---|---|---|
| LSP | diagnostics, hover, definition/implementation/references, completion, rename, code actions + server-initiated file ops, signature help, format, folding ranges, inlay hints, semantic tokens, diagnostics panel, quickfix list, inline diagnostics + staleness, multi-server with only/except, universal servers, per-server status popup/mute, pull diagnostics, workspace diagnostics for unopened files, progress, process limits, env/init options, root markers, C header routing | **partial** (#7 A–E complete; R6-5 three items landed) | `lsp/{client,handler,requests,capabilities,progress}.go`, `lsp/manager/{manager,router}.go`, `internal/lsphost/{lsp,lsp_data}.go`, `internal/tui/{lsp,lsp_data,workspace_edit,diagpanel}.go`, `editor/{lsp,outline}_folds.go`, `internal/uiadapter/lsp_workspace.go`, `commands/lsp_commands.go`, `docs/lsp.md` | Rounds 4–6 landed the router/edits/status/staleness, hints/tokens/folds/pull diagnostics and the diagnostics panel/quickfix/progress states. Round 7 landed: interleaved workspace edits in server order with stop-on-refused partial reporting (stricter than Fresh, §4 item 18), workspace diagnostics for unopened files (capability-gated `workspace/diagnostic`, 5 s throttle, capability/open-count re-ask, published-set precedence, workspace-only rows marked `(not open)`, 50/file cap, no `$/cancelRequest`), `$/progress` text in the panel (cap 8, no animation) and `LSPSession.StartLanguage`. Residuals R7-5: inline-hint placement tier 2, no built-in universal server, `process_limits` unenforced, diagnostics capability-gated/publish-only servers report only what they choose, no cancel request for pulls, resource-op failure model documented stricter than Fresh. |
| Plugins runtime and host API | TypeScript on QuickJS, sandbox-lite with per-plugin contexts, ~372-method API, overlays/virtual text/conceals/soft breaks/folds, prompts/toolbars, modes/contexts, windows/splits, virtual+composite buffers, buffer groups, terminals, authority/remote, LSP requests, HTTP, widgets/floating/dock panels/sidebar sections, i18n, package manager, menus, events | **partial, runtime different; API level 2 (tiers 1a+1b+1c) | `plugin/`, `pluginapi/` (incl. `widgetapi.go`, `buffergroup.go`, `compare.go`, tier-1c decoration types), `internal/pluginhost/` (incl. `compare.go`), `internal/tui/plugin*.go` + `pluginpanel/`, `plugin_groups.go`, `plugin_themes.go`, `plugin_folds.go`, `plugin_seams.go`, `decorations/`, `docs/plugins/` (6 API pages + `events.md`, 1441-line `gvim.d.lua`) | Lua 5.1 is the permitted runtime difference; API level 2. Rounds 5–7 added tiers 1a/1b/1c-A (widgets, groups, theme files, store/Lua surface); round 8 completed tier 1c-B/C: the shared `rows_extra` transform paints virtual lines/soft breaks, conceal, cursor-line overlay and plugin folds with one source↔display mapping (`gvim.has("decor_virtual_paint")` true), plus split labels, per-buffer window state, wrappers and four events, and the compare/diff API (`gvim.diff`). Remaining (R8-1): setLineTargets, virtual rows cut-not-wrapped/line-based scrollbar thumb, fixed-height group leaves, synthetic group tab/per-panel views, windowEmbed live panes, markdown-in-text, dock/fullscreen placement, plugin explorer rows/slots, composite buffers (recorded native substitution), package manager, HTTP, project-local plugins, render_line. |
| Bundled plugin feature set | 50 plugins: git_log, live_diff, git_grep, git_explorer, git_blame, git_gutter, git_statusbar, git_find_file, search_replace, diff_nav, diagnostics_panel, code-tour, dashboard, welcome*, theme_editor, markdown_toc, markdown_compose, markdown_source, path_complete, lsp_help, lsp_navigation, find_references, compare_files, flash, vi_mode, merge_conflict, tab_actions, goto_with_selection, env-manager, devcontainer, k8s-workspace, agent_*/orchestrator/live_sessions, pkg, audit_mode, language helpers | **partial** | `plugin/bundled/plugins/` (15; 10 default-enabled), `docs/plugins/bundled.md`; Fresh `plugins/*.ts` | Ported: diagnostics_panel (round 6), git_log + live_diff (round 7, reduced; round 8 full paths), compare_files (round 8, opt-in), path_complete + lsp_help (round 8, reduced). Missing: git_grep, git_explorer, search_replace (native substitution), code-tour, dashboard, theme_editor (native substitution), markdown_compose/source (native), lsp_navigation, find_references, env-manager, devcontainer, k8s-workspace, orchestrator/agent sessions, live_sessions, pkg, audit_mode; language helpers. Native substitutions recorded for theme_editor/search_replace/markdown_compose/source/composite. |
| Terminal | PTY terminal panes and utility dock, live/scrollback modes per split, keyboard capture, clickable paths (OSC 7), restart exited process + agent resume, shell override, send selection, mouse forwarding/drag-select, close_on_exit, terminal tabs, ConPTY on Windows | **partial** (R3 landed slice A) | `config/terminal.go`, `internal/terminal/{scrollback,mouse,session,term}.go`, `internal/tui/dock/{scrollback,input,panel,render,ports}.go` + `dock_glue.go`, `internal/uiadapter/terminal.go`, `docs/terminal.md`, tests `internal/terminal/*_test.go`, `internal/tui/dock/*_test.go` | Round 3 landed: `terminal.shell` (command+args; project layer may not set it), `close_on_exit`, `mouse_forwarding` (`requested`/`alt_screen` with Shift bypass and the alternate-scroll rule), gvim-only `scrollback_lines`; a read-only scrollback pager (Ctrl+Space toggle with terminal_escape fallback, Esc, PgUp/PgDn/Home/End, `/` case-insensitive search with n/N, y copy row, Ctrl+C copy screen, title rows), OSC 7 working-directory tracking with Ctrl+Click path links (absolute/~ → OSC 7 cwd → workspace root, exists check in a tea.Cmd, off on the alternate screen), and mouse forwarding from the child's DECSET modes. Round 5 (slice B) closed R3-2's main items: read-only text selection in the dock (drag on the live grid drops into scrollback and selects, Shift+drag selects while a program owns the mouse, double/triple click select a word/row in scrollback only, selection painted and copied through the clipboard path, selection holds the view against automatic scroll); `jump_to_end_on_output` and `mouse_drag_selects` parsed and applied; scrollback search with Ctrl+F plus Ctrl+R regex and Ctrl+A case toggles (prior transcript + history + screen); dead-shell focus and scrollback navigation; restart-into-the-same-transcript (bounded prior rows above the new shell, restarted in the last OSC 7 cwd) and session-directory persistence of the transcript (`terminal.json`, bounded rows with control characters stripped, restored read-only and dead, nothing re-run). Still open: reflow (x/vt has no reflow; saved width recorded only), automatic agent re-exec (`terminal.resume_agents` accepted-ignored: no tracked launch/agent argv), terminal tabs, child OSC 52 (x/vt exposes no payload) / kitty keyboard / `pass_ctrl_v_without_text`, Windows ConPTY, drag auto-scroll and word-wise drag extension, hover underline and `file:line:col` links, split-terminal `close_on_exit`. |
| Git surfaces | review diff (stage/unstage/discard/comments/export), range/stash reviews, git log with live diff, git blame panel with history, live diff baselines, diff chunk nav, git grep, git explorer, git find file, statusbar branch, gutter marks | **partial** | `gitstate/`, `plugin/bundled/plugins/git_gutter`, `git_statusbar`, `git_find_file`, `git_blame`, `merge_conflict`, `internal/tui/git.go`; Fresh `docs/features/git.md` | Missing: Review Diff (/range/stash), Git Log, Live Diff, Git Grep, Git Explorer, blame panel/history stepping (gvim has a reduced inline blame), merge view (reduced to in-buffer resolution), diff hunk sources beyond the HEAD gutter, review comments/export. Implemented: gutter hunk marks + next/prev change, branch statusbar, git find file, merge-conflict resolution, compare-as-difftool. |
| Session, restore, daemon | session restore/save, hot exit of unsaved buffers, crash recovery, ephemeral-file patterns, per-file pins, `--restore`/`--no-restore`/`--simple`, daemon mode with attach/detach and `--cmd daemon open-file --wait`, direct-vs-daemon launch modes | **partial** (R2 landed hot exit, the restore settings and the CLI flags) | `internal/app/{session_plan,session_open,hotexit}.go`, `internal/app/run.go`, `internal/restore/session.go` (`RestoreHotExit`), `internal/cli/root.go`, session/ files, tests `session_plan_test.go`, `session_launch_test.go`, `restore/session_hot_test.go`, `internal/cli/root_test.go`, README sessions/flags; Fresh `docs/features/session-persistence.md` | Round 2 landed: `hot_exit` (quit without asking, the session save stashes the text; off asks per dirty tab), `restore_previous_session` (tabs/layout vs the unsaved text only), `skip_session_restore_when_files_passed` (file launch, or restore then append the given files), and the one-shot `--restore`/`--no-restore` (alias `--no-session`)/`--simple` with mutual-exclusion errors. Still open: daemon mode and its CLI, no per-buffer hot-exit store (a file launch cannot keep/restore its files' unsaved text), `--no-restore` still saves the session (deliberate; §4 item 10, R2-5), and `--simple`'s "no dock" is vacuous. |
| Workspaces, windows, orchestrator, agents | orchestration dock, multiple workspaces per daemon (one per git worktree), workspace switcher, New Workspace dialog, Run Agent (claude/codex/opencode/aider), agent session discovery/resume, live sessions, extract tab to workspace | **missing** | `grep -ri orchestrator --include=*.go` finds only command-table/capability strings; `internal/tui/remote.go` says remote workspaces unsupported; `extract_tab_to_new_workspace` is a notice; Fresh `docs/features/session-persistence.md`, `docs/features/scripting.md` | Whole area absent; gvim is one editor per process. Depends on daemon/workspace model. |
| Remote authority | SSH editing (scp/URL forms), devcontainers (CLI attach/rebuild/ports/logs), Kubernetes workspaces, remote-aware LSP/terminal/processes, reconnect, remote indicator popup | **missing** | No `ssh`/`authority` implementation in the tree; `internal/tui/remote.go:10` popup says "Remote workspaces are not supported"; `docs/plugins/porting.md:46` "authority ... not provided"; Fresh `docs/features/ssh.md`, `docs/features/devcontainer.md` | Entire area absent. Requires an authority abstraction under fs/proc/terminal/LSP first. |
| Workspace trust | per-folder Restricted/Trusted/Blocked, executable-marker detection, one-time prompt, status pill, dialog command, persisted per folder, gates LSP/build/env activation, env detectors | **partial** (R1 landed) | `trust/` (levels + Undecided, store at `config.Dir()/trust.json`, atomic writes, marker scan, Fresh startup policy), `internal/app/trust.go` and `internal/app/run.go` (`Options.Untrusted`, `lsp_enabled` forced off), `internal/tui/trust*.go` (dialog, port, notices, pill), `config/trust_test.go`, `docs/trust.md`; Fresh `docs/features/workspace-trust.md` | Implemented in round 1: the three levels plus Undecided, per-folder persistence, executable-marker scan and prompt (silent trust for plain folders, Fresh's policy), the open-time prompt, the `Workspace Trust…` palette command and dialog, the trust status pill with click routing, the project config layer skipped unless Trusted, and LSP auto-start forced off unless Trusted (manual Start/Restart still works). Still open: no process gate — terminal/shell/formatter/LSP/plugin spawns are not consulted against the level, and Blocked enforces the same as Restricted (`docs/trust.md` §3); env detection/activation and devcontainer/tasks are absent; the config gate is a startup decision (a mid-session trust change applies project tooling at the next start, documented divergence). Tracked as backlog #17. |
| Markdown | syntax highlighting, markdown_source smart list editing, page view/compose, tables, code-fence frames, dual view, Contents section | **partial** (R1 Contents; R4 smart editing; R6 compose core; R7 fences/tables; R8 dual+borders) | `syntax/{markdown,markdown_inline,markdown_markers,markdown_fences,markdown_tables}.go`, `editor/markdown.go`, `internal/tui/editorpane/{compose,compose_layout,compose_fence,compose_table,pageview}.go` + `internal/tui/{pageview,dual}.go`, `internal/tui/sidebar/contents.go`, `outline/`, `docs/markdown.md`; Fresh `plugins/markdown_{toc,source,compose}.ts` | Round 8 added the native dual view (source+compose split, derived per-pane mode, line-anchored sync, five mode commands) and the table top/bottom border rows via the page-view extra-row hook; the compose/fence/table core landed rounds 6–7. Residuals R8-2: multi-visual-row cells/escaped pipes/nested tables, nested fences, heading scrollbar markers/marker conceal/heading polish, page-view mode+width persistence, free per-split mode, mapped occurrences/rulers/guides/whitespace and composed plugin overlays, task/image/HTML conceal, exact row/byte dual anchors + native Markdown menu row + multiple duals. |
| Diagnostics surfaces | gutter marks, underline, inline end-of-line text, diagnostics panel, quickfix list, error navigation, hover fusion, pull/workspace diagnostics, progress | **partial** (rounds 4–7) | `internal/tui/{diag,diagpanel,lsp}.go`, `internal/tui/editorpane/{decor,inline_diag}.go`, `commands/lsp_commands.go`, `docs/lsp.md`; Fresh `docs/features/lsp.md` | Gutter dots, underline, inline end-of-line diagnostics text with version staleness (rounds 2/5), status count, F8 jumps and hover diagnostics exist. Round 6 added the native sidebar Diagnostics panel (file→severity grouping, counts header, jump/scroll/focus, progress/empty states) and the quickfix Diagnostics List; round 7 added workspace diagnostics for unopened files (`(not open)` rows that open on jump) and `$/progress` text in the panel. Residuals R7-5: inline-hint placement tier 2, no built-in universal server, `process_limits` unenforced, capability-gated workspace diagnostics/no cancel, plugin `DiagnosticInfo` carries no code (native ones do). |
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
5. **Overlay keymap contexts are resolved, with narrowings (R2-4 closed, round 7).** gvim
   resolves `global`, `normal`, `file_explorer`, `terminal` and, since round 7, `prompt`,
   `searchPrompt`, `popup`, `menu`, `settings` and `dock`: the top overlay consults the keymap
   before its built-in handling and falls through to the built-in when unbound (`docs/keybindings.md`
   "How overlay keys work"). Remaining narrowings (R7-4): overlay contexts take single-key user
   bindings only, `unbind` cannot disable a surface's built-in handler, and
   `completion`/`compositeBuffer`/the custom settings/theme dialogs/plugin panels are not keymap
   surfaces yet.
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
18. **LSP workspace edits follow server order, and a refused file operation stops the edit.** A
    server's `documentChanges` apply strictly in the order sent, interleaving create/rename/delete
    with text edits (a created file's text edit lands on the created file; a rename happens before
    edits of the renamed path); each open-document step checks the version the server computed
    against and the editor version at that step, so a stale step is skipped and the rest still
    apply; closed files are read, opened as unsaved tabs, edited, and the previous tab restored;
    deletes and create/rename onto an existing target are refused (Fresh's rule). A refused or
    failed file operation STOPS the remaining documentChanges, and the notice reports the partial
    state with the applied counts — deliberately stricter than Fresh's `apply_workspace_edit`,
    which continues after a refusal and only counts it. Nothing is reported as success when part
    of the edit was refused. `docs/lsp.md`.
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

33. **The Markdown page view is a native inline transform, not Fresh's plugin.** The view mode
    is per document (Fresh: per split), the compose rules are the shipped subset (emphasis/
    strong/inline-code/link/strike conceal and cursor-line reveal, rules, quote bars, list/quote
    hanging indents; no tables/fence frames/heading polish yet), and the pane maps source runes
    to composed runes for caret/hit/selection/search/diagnostics/folds. Non-Markdown documents
    get the page geometry only, and large files stay full width. `docs/markdown.md`; R6-2.
34. **Contents fold_buffer is a native setting.** gvim reads `contents.fold_buffer` (user layer,
    restart; the retired `plugins.markdown_toc.settings.fold_buffer` is the fallback and the native
    key wins). Ranges are published through a version-guarded outline fold source only while the
    Contents section is open (Fresh's plugin publishes while it tracks a Markdown buffer even with
    the panel hidden), stale ranges are dropped on edit and folded headings re-applied by heading
    byte offset. Folding is the section's own disclosure gesture: there is no fold command or menu
    row because Fresh's markdown_toc registers none. `docs/sidebar.md` §6.
35. **The diagnostics panel is native, not Fresh's plugin panel.** gvim shows a native sidebar
    section (`sidebar.KindDiagnostics`) and a native quickfix Diagnostics List overlay; Fresh ships
    a plugin widget panel and no native list. The builtin palette title "Toggle Diagnostics
    Sidebar" keeps the Fresh plugin's title reachable. Enter jumps and closes the panel (a v1
    widget panel would hold the keyboard), grouping is file then severity, the header shows
    workspace counts/progress, and there is no workspace-wide request for files no server has
    reported on. Native diagnostics carry a `code`; plugin API diagnostics do not. `docs/lsp.md`.
36. **Plugin buffer groups are group-lite.** A group is a layout of real splits of the current tab
    (no synthetic group tab): read-only panel buffers the host makes or existing buffers shown in
    their own windows, styled panel content, panel title/visibility/focus, ownership cleanup, and
    scroll-sync groups with anchor interpolation. Fixed-height leaves are accepted and ignored
    (split ratios place panels); group titles are stored for the port API but not painted;
    independent per-panel views of one buffer are deferred to tier 1c. `docs/plugins/api-buffers.md`.
37. **The plugin API stays at level 2.** Round 6 added the interactive widget/instance-state
    surface, group-lite buffer groups + scroll-sync, theme-file APIs and
    `text_properties_at_cursor` without a level bump; a manifest may still require `"api": 2` and
    level-1 plugins keep working. `docs/plugins/README.md`.
38. **Search previews are bounded plain text.** The selected-match column is a plain-text window
    (no scrolling, no syntax colours) with 24..60 cells and palette conventions, hidden below a
    64-cell card; a multi-line match longer than the window shows its first lines, and a single
    multi-line match row is one row carrying the exact source range. Replace still applies the
    exact range. `docs/ui.md`.
39. **The first plugin port is reduced, and its host surfaces are honest about it.**
    `diagnostics_panel` ports the Fresh plugin's live-panel behavior with the Finder subset (no
    prompt mode, preview pane, utility-dock placement or severity colours; the `[E]/[W]/[I]/[H]`
    tag carries severity), at most 500 rows with bounded strings, no default key binding, and
    Enter closing the v1 widget panel so the keyboard returns to the editor. Costs about
    0.9 MiB; the default set is about 6.75 MiB of the 8 MiB idle gate after round 7. `docs/plugins/bundled.md`.

40. **The plugin virtual-text surface is painted through one shared row transform.** Round 7's
    store/Lua surface (virtual lines above/below, conceal, soft breaks, cursor-line overlay,
    ranged scrollbars, plugin folds) is now rendered by round 8's tier-1c-B/C: `rows_extra.go`
    inserts display rows and the pane keeps one cached source↔display mapping for painting,
    caret, hit, selection, search and diagnostics, in the fixed order wrap+folds → markdown
    compose → conceal → soft breaks → virtual lines → cursor-line overlay → selection.
    `gvim.has("decor_virtual_paint")` is true on this host; older/reduced hosts fall back (the
    persisted ports feature-detect it). Limits recorded in R8-1 (virtual rows are cut, not
    wrapped; the scrollbar thumb is line-based). `docs/plugins/api-ui.md`.
41. **live_diff renders its full diff but has no host baseline.** With tier-1c-B the plugin's
    full path is active: deleted lines above the edits as virtual lines (with their `-` glyph and
    removed-word text overlays), line backgrounds, word emphasis, gutter and scrollbar marks;
    the reduced path remains for hosts without `decor_virtual_paint`. The old side is still
    fetched by the plugin (`git show <rev>:./<file>` with the buffer's encoding, or
    `gvim.fs.read`+`gvim.text.decode` for disk mode) and diffed with `gvim.diff.buffer` (Myers;
    hunk boundaries can differ from Fresh's patience baseline, and the word refinement has a
    DP-cell budget); gvim decorations have no `fgOnCollisionOnly`, so only the
    `editor.diff_*_bg` theme keys are used, and large-file mode is refused rather than diffed.
    `docs/plugins/porting.md`.
42. **git_log is reduced to group-lite.** The log/detail/hint layout is real splits of the
    current tab (not Fresh's synthetic single-tab group) and its panel buffers are also visible
    as tabs; the toolbar is a hint row, not clickable buttons; the log is a styled buffer whose
    cursor is the selection (a line decoration tints it), not a described List widget; the
    detail is raw `git show` text carrying plugin decorations, not a host diff-background layer;
    there is no HEAD watcher (`r` refreshes). The diff streams with disk caching and the Fresh
    caps; the port has no default key bindings. `docs/plugins/bundled.md`.
43. **Bundled-plugin default policy is memory-bound.** 10 of the 15 bundled plugins are
    default-enabled; off are flash and vi_mode (size/heap), live_diff (opt-in like Fresh's),
    git_log and compare_files (heap; compare_files also duplicates native Compare). The default
    set costs about 7.9 MiB of the firm 8 MiB gate (headroom ~0.08; ~9.8 MiB session baseline),
    enforced by `TestBundledPluginsIdleHeap`; the historical git_log-on measurement (8.68 MiB)
    is why git_log ships off. `docs/plugins/bundled.md`.
44. **Markdown framing is single-visual-row, now with the outer table borders.** Table rows use
    the mid border (from the source separator), padded/aligned cells and per-row caret reveal,
    and round 8 wires the top/bottom border rows through the shared page-view extra-row hook
    (`Pane.PageExtraRows`) that tier-1c-B added. Still residual: cells needing more than one
    visual row, escaped pipes/nested tables, a frame's long body line continuing unframed
    (first visual row only) and fences nested in blockquotes/lists (left raw).
    `docs/markdown.md`.
45. **Overlay keymap contexts are narrowed.** The overlay surfaces consult the keymap in Fresh's
    context names before their built-ins with fall-through, but only single-key bindings apply
    there (a sequence is warned+skipped), `unbind` removes the preset row while the built-in
    handler still runs on the freed chord, a prompt text field has no selection/clipboard/
    suggestion actions to bind, and the `completion`/`compositeBuffer` contexts, custom
    settings/theme dialogs and plugin panels are outside the router. Presets carry Fresh's rows
    only where gvim's surfaces understand them. `docs/keybindings.md`.
46. **Settings coverage is complete for the page surface, with three documented residuals.**
    The Language Servers page edits each language's whole server array as validated JSON (array
    elements are not individually addressable), shows parsed servers read-only and accepts
    `process_limits` as shown-but-unapplied; the Environment rows are read-only facts; the
    per-language structured values show the inherited effective value and reset/inherit via
    whole-object writes; the Plugins page toggles `plugins.<name>.enabled` in the user layer
    with file rollback and reloads, listing declared settings read-only; the Keybindings page is
    read-only and opens the existing Keybinding Editor (no in-page capture, deliberately).
    Residuals (R8-3): no plugin declared-settings schema/values API, no plugin gallery/install
    page, no in-page key capture, `env.detectors`/env activation, per-field LSP dialogs.
    `docs/settings.md`.

47. **The Markdown dual view is native, not Fresh's buffer group.** gvim uses a derived per-pane
    page-view override (document view settings remain the single-pane default), an ordinary
    50/50 split over the same tab/document (source left, composed right, focus on source),
    compare-style line-anchored scroll sync, shared document/caret with the compose pane's
    caret/hit/selection mapping, and five hand-written palette rows. Residual differences: no
    group tab/context, exact row/byte anchors, a native Markdown menu row (the menu table is
    generated from Fresh), split persistence, more than one dual per document, free per-split
    mode. `docs/markdown.md`.
48. **compare_files is a second, opt-in Compare surface.** gvim's native Compare view is the
    default implementation of Fresh's Compare Files; the bundled Lua port duplicates it as
    group-lite splits of the two file tabs (opened as ordinary tabs when needed; Close closes
    only ones it opened when unmodified), with hunk-anchored scroll sync, Fresh theme
    decorations, copy/take through `replace_range` (one undo per merge), no EOL-only hunks,
    filler rows gated on the virtual-line capability, positional pairing, ui.input paths, no
    `--diff` hook/export. It ships off by default for the heap gate; enabling it adds palette
    rows next to the native ones (palette count stays 229 without it). `docs/compare.md`.
49. **path_complete and lsp_help are reduced ports.** Fresh's path_complete answers the built-in
    prompt's `prompt_changed` event, which gvim has no seam for (Open File/Save As are native
    browser/dialog overlays), so gvim keeps the faithful completion engine as an exported
    `complete()` plus one “Path Completion: Complete Path” prompt command (Fresh registers none
    and no key; the command is the documented deviation). lsp_help's event payload carries only
    the error text (no language/type/command), so the language is the active buffer's and “not
    found” is inferred from the text; 16 language hints, native quick actions in the action
    popup, no workspace-symbol action, English hints. `docs/plugins/bundled.md`.
50. **The inserted-row mechanism has documented display limits.** Virtual rows are cut to the
    pane width (not wrapped); the scrollbar thumb stays line-based; group fixed-height leaves
    are accepted and ignored (split ratios place panels); there is no synthetic group tab or
    independent per-panel views, no live `windowEmbed` panes, no markdown-in-text widget and no
    dock/fullscreen placement; `setLineTargets` is deferred with them (R8-1). Composition order
    is fixed (see item 40). `docs/plugins/api-ui.md`.
51. **Bundled defaults are memory-bound.** 10 of the 15 bundled plugins are default-enabled;
    off are flash, vi_mode (size/heap), live_diff (opt-in like Fresh's), git_log and
    compare_files (heap; compare also duplicates native Compare). The default set costs about
    7.9 MiB of the firm 8 MiB gate (headroom ~0.08), enforced by
    `TestBundledPluginsIdleHeap`. `docs/plugins/bundled.md`.
52. **Palette count 229.** Of the 229 rows 23 are non-generated: 7 hand-written native rows, 4
    (project search/Git Grep/Reload, Edit Theme/Project Search Results, diagnostics rows, and
    the five Markdown mode rows); plugin commands appear only when the plugin loads, and the
    opt-in compare_files port duplicates the native Compare rows without changing the count.
53. **Group layout ratios are fixed.** BufferGroupLayout.Ratio is the first child's share;
    SplitOptions.Ratio is the new (second) window's share. Round 8 found and fixed the inversion
    with a regression test (`TestBufferGroupLayoutRatioIsFirstChildShare`); fixed-height leaves
    are still accepted and ignored. `docs/plugins/api-buffers.md`.

## 5. Ranked backlog for the remaining rounds

Sizes are estimates: S ≈ a day or less, M ≈ 1–3 days, L ≈ 3–10 days, XL ≈ more (unvalidated).
The order follows the goal's dependency shape: user-visible TUI/editing gaps first, then the
plugin-API breadth that unblocks Fresh's plugin-era features, then the daemon/remote/agent
architecture. "Owner area" names the package that should own the work.

Rounds 1–8 closed the workspace-trust surface, native sidebar sections/Contents and the LSP pill
+ diagnostics badge (round 1); virtual space (`on`), inline diagnostics, Fresh's keymap data, the
config-driven status bar + menu accelerators, and the session settings/CLI (round 2); the editable
Settings page, terminal slice A, the explorer options + sticky parents, and the palette preview +
Live Grep toggles (round 3); LSP 4a, project-wide Search & Replace + Git Grep, settings coverage,
virtual space R2-1 and markdown smart editing (round 4); LSP 4b data rendering A–D, plugin host
tier 1a, theme model/editor, terminal slice B, project-search R4-2 and status bars R2-3 (round 5);
LSP 4b E, plugin tier 1b, the diagnostics_panel port, the markdown compose core, Contents
fold_buffer (R1-1) and search preview/multi-line (round 6); LSP R6-5, plugin tier 1c-A's
store/Lua half, git_log/live_diff ports, markdown fences/tables, overlay keymap contexts (R2-4)
and a settings R4-3 slice (round 7); and plugin tier 1c-B/C, the #15 compare API + compare_files
port, the native markdown dual view + table borders, settings R7-3, the path_complete/lsp_help
ports, live_diff/git_log full paths and streaming search/Git Grep globs (round 8). Retired ids:
#7, #8, #11 and #15 are closed; §5.1 now holds #12 only; R1-1, R2-4, R5-1, R6-5 and
R7-1/R7-2/R7-3 are closed, and the slices of earlier rounds are in. The ranked items below are
the remaining gaps, with the gaps the rounds opened listed first as R1-*, R2-*, R3-*, R4-*,
R5-*, R6-*, R7-* and R8-*.

Round 1 recorded three remaining gaps (R1-1 closed in round 6; the spawn gate is also ranked as #17):

- **R1-1 (CLOSED round 6)** — Contents `fold_buffer`: the native setting (user layer, restart;
  plugin-key fallback) publishes per-heading ranges through the version-guarded outline fold
  source; folding a Contents row folds its section (no fold command, Fresh parity). See the
  File explorer/sidebar row and R6-6 for the small residual differences.
- **R1-2 (P2, S)** — persist sidebar section state in the session (section list/order, Contents
  visibility and follow mode, heights; `internal/tui/sidebar`, `internal/restore`, `session`).
- **R1-3 (P2, S)** — `gvim config show` still merges the project config layer regardless of
  trust; gate it by the trust level or label the layers (`internal/configview`, `config`;
  `docs/trust.md` §3 "Config viewer").
- **R1-4 (P1, L)** — per-spawn executable-path gate and Blocked spawn denial
  (terminal/shell/formatter/LSP/plugin processes); tracked as ranked item #17
  (`trust/` and the spawn sites; `docs/trust.md` §3).

Round 2 recorded two remaining gaps (R2-1/R2-2 closed in round 4, R2-3 in round 5):

- **R2-4 (CLOSED round 7)** — the overlay keymap contexts landed: the palette/find bar/prompt/
  menu bar/Settings page/dock consult the keymap in Fresh's context names before their built-in
  handlers, with fall-through; presets carry Fresh's overlay rows where gvim's surfaces
  understand them; the loader accepts the contexts and warns+skips multi-key bindings there.
  Residual limits moved to R7-4 (single-key only, unbind semantics, text-field actions,
  completion/compositeBuffer/dialog/plugin-panel surfaces). `docs/keybindings.md`.
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

Round 4 recorded five gaps (R4-1 closed in round 6; R4-2 partially closed in rounds 5–6):

- **R4-1 (CLOSED round 6)** — LSP 4b A–E landed across rounds 5–6 (hints/tokens/folds/pull
  diagnostics + diagnostics store/panel/quickfix list/progress); task-22/29 complete. The small
  leftovers moved to R6-5 (no workspace-wide request for never-reported files, inline-hint tier
  2, no built-in universal server, `process_limits` unenforced, resource-op ordering/optimistic
  applyEdit). `docs/lsp.md`.
- **R4-2 (P2, M, mostly closed round 8)** — project search: rounds 5–8 landed match-tree
  checkboxes, Replace Scoped, file globs, Alt+Q list export, 20-deep history, the selected-match
  preview, multi-line rows, streaming/batched results with cancel/one-shot fallback and the Git
  Grep Files pathspec field. Remaining is R8-5 (preview scroll/colours, long multi-line,
  large-file replace, provider scopes); replacements stay unsaved for review (§4 item 16).
- **R4-3 (CLOSED round 8, residuals R8-3)** — the non-scalar page plumbing, Language Servers
  page, Environment facts (round 7) plus per-language structured values/reset-vs-inherit, the
  Plugins page and the read-only Keybindings page (round 8) close the page surface. Residuals
  are R8-3 (plugin settings schema API, plugin gallery, no in-page key capture, `env.detectors`,
  per-field LSP dialogs); the page's deliberate divergences are §4 items 46 and 17.
- **R4-4 (P2, S)** — virtual-space limits: linear selections byte-clamped, block columns
  rune-based (§4 item 19), no virtual space with wrap/multi-caret/large files, vertical lines
  clamp at the viewport/folded end and cannot be selected, the per-buffer toggle cannot allow
  block, positions are not session-persisted. `docs/internal/virtual-space-scoping.md`.
- **R4-5 (P2, M, partly closed round 6)** — the markdown_source subset landed in round 4 and the
  compose core in round 6; what remains of the Markdown work is R6-2 (tables, fence frames, dual
  view, heading polish, task/image/HTML conceal, page-view persistence and per-split mode,
  mapped occurrences/rulers/guides/whitespace and composed plugin overlays). `docs/markdown.md`.

Round 5 recorded six gaps (R5-1 closed in round 6):

- **R5-1 (CLOSED round 6)** — LSP 4b A–E: hints/tokens/folds/pull diagnostics (round 5) and the
  workspace diagnostics store, native Diagnostics sidebar panel, quickfix Diagnostics List and
  progress waiting states (round 6) landed; task-22/29 complete. The small leftovers moved to
  R6-5 (no workspace-wide request for never-reported files, inline-hint tier 2, no built-in
  universal server, `process_limits` unenforced, resource-op ordering/optimistic applyEdit).
- **R5-2 (P1, L/XL, tier 1c complete except R8-1)** — rounds 5–8 landed tiers 1a/1b and 1c
  (painted virtual-text surface, group-lite groups/scroll sync, interactive widgets, theme-file
  APIs, split labels/window state/events, compare API). Remaining plugin-host gaps are R8-1
  (setLineTargets, virtual-row wrapping/scrollbar thumb, fixed-height leaves, synthetic group
  tab/per-panel views, windowEmbed live panes, markdown-in-text, dock placement, explorer rows/
  slots) plus the recorded composite-buffer native substitution.
- **R5-3 (P2, M)** — theme residuals: URL/packaged themes; user-theme inheritance (Fresh's own
  limit); colour picker/mouse editing; per-cell painted-key provenance (inspect matches rendered
  colours + context, §4 item 30); Fresh themes' accent/popup selection colours; theme preview in
  the Settings page; background images. `docs/themes.md` §8.
- **R5-4 (P2, M/L)** — terminal residuals: reflow (x/vt has none; saved width recorded only),
  automatic agent re-exec (`resume_agents` accepted-ignored), terminal tabs, child OSC 52/kitty
  keyboard/`pass_ctrl_v_without_text`, Windows ConPTY, drag auto-scroll and word-wise drag
  extension, hover underline and `file:line:col` links, split-terminal `close_on_exit`.
  `docs/terminal.md` Deferred.
- **R5-5 (P2, M, mostly closed round 8)** — preview column and multi-line rows (round 6), then
  streaming/batched results and Git Grep pathspecs (round 8) landed. Remaining is R8-5 (preview
  scroll/syntax colours, a multi-line match longer than the preview, large-file replace,
  provider scopes; no persistent quickfix stack — §4 item 31).
- **R5-6 (P2, S)** — status bar residuals: `{plugin:token}` still unpainted (needs a
  register/paint API; plugin status items cover the use case), and `{warnings}`/`{update}` stay
  deliberately empty (no warning-count domain/updater).

Round 6 recorded six gaps:

- **R6-1 (P2, L, 1c store+render landed round 8; remaining R8-1)** — the painted virtual-text
  surface and 1c-C seams landed; what remains of tier 1c is R8-1: `setLineTargets`, virtual rows
  cut not wrapped, line-based scrollbar thumb, fixed-height group leaves, synthetic group tab/
  per-panel views, windowEmbed live panes, markdown-in-text, dock/fullscreen placement, plugin
  explorer rows/slots, and the composite API (recorded native substitution).
- **R6-2 (P2, L, fences/tables round 7, dual+borders round 8; remaining R8-2)** — the Markdown
  renderer and dual view are in place; residuals are R8-2: multi-visual-row cells/escaped pipes/
  nested tables, nested-fence framing, heading scrollbar markers/marker conceal/heading polish,
  page-view mode/width persistence, free per-split mode, mapped occurrences/rulers/indent
  guides/whitespace, composed plugin overlays, task/image/HTML conceal, and dual exact anchors/
  native menu row/split persistence/multiple duals.
- **R6-3 (P2, M, streaming/globs closed round 8; remaining R8-5)** — streaming/batched project
  results (≤128 files/≤1000 matches per batch, cancel, progress, one-shot fallback) and Git
  Grep files/pathspecs landed. Remaining: preview scrolling/syntax colours, a long multi-line
  match, large-file replace, provider scopes; no persistent quickfix stack (§4 item 31).
- **R6-4 (P2, M, partly closed round 7)** — workspace diagnostics for files no server has
  reported on now come from a capability-gated `workspace/diagnostic` pull when the server
  advertises one (task-42); publish-only servers still report only what they choose, there is no
  cancel request, and the diagnostics-panel port remains reduced (no Finder prompt/preview/dock,
  severity tags instead of colours, no default key binding) with plugin diagnostics carrying no
  `code` field. Port reductions tracked as R7-2/R7-3.
- **R6-5 (CLOSED round 7)** — the three picked items landed (interleaved workspace edits with
  stop-on-refused partial reporting, workspace diagnostics for unopened files, $/progress text in
  the panel) and are recorded in the LSP row/§4 item 18. The small leftovers moved to R7-5
  (inline-hint tier 2, no built-in universal server, `process_limits` unenforced, diagnostics
  capability-gated/publish-only/no-cancel, resource-op failure model stricter than Fresh).
- **R6-6 (P2, S)** — Contents/sidebar residuals: R1-2 sidebar section persistence (list/order/
  heights; rows/divider drag are session state), the fold source publishes only while the
  section is open (Fresh tracks with the panel hidden), `contents.fold_buffer` applies on restart
  (Fresh re-reads plugin settings per event), no right-side sidebar or resize below the minimum,
  and plugin explorer rows/decorations/slots wait on a later plugin surface (with R6-1).
  `docs/sidebar.md`.

Round 7 recorded five gaps:

- **R7-1 (CLOSED round 8, remainder R8-1)** — the shared pane row transform (`rows_extra.go`)
  landed: virtual lines above/below (gutter glyph/text overlays/priority/caps), conceal
  (replacement/activation/scope/composeOnly, columns unchanged), soft breaks (indent/prefix),
  cursor-line overlay, plugin folds with pane integration + fold-first toggle, and one cached
  source↔display mapping used by paint/caret/hit/selection/search/scroll; the 1c-C seams
  (split labels, per-buffer window state, open/focus/close wrappers, path_changed/focus_gained/
  active_window_changed/buffer_group_closed) and `gvim.has("decor_virtual_paint")` also landed.
  `setLineTargets` and the remaining limits moved to R8-1.
- **R7-2 (CLOSED round 8, reductions R8-4)** — live_diff's full virtual-line rendering is active
  (capability true; reduced path for old hosts; both tested, real-host vs HEAD), git_log folds
  and per-commit detail scroll restore use the new seams (real-host from palette), and the
  group-layout ratio inversion found by these ports is fixed with a regression test. What
  remains (baseline patience/caps, compare/path ports' reductions) is R8-4.
- **R7-3 (CLOSED round 8, residuals R8-3)** — the page surface is complete: per-language
  structured values with inherited display/reset-vs-inherit (whole-object writes), Languages
  entries pages, the Plugins page (inventory, user-layer enable/disable with file rollback,
  reload, read-only settings), the read-only Keybindings page + Open Keybinding Editor action,
  docs/settings.md and the docs/lsp settings paragraph. Residuals (plugin settings schema API,
  plugin gallery, in-page capture, env detection) moved to R8-3.
- **R7-4 (P2, S)** — keymap residual limits: overlay contexts accept only single-key bindings;
  `unbind` cannot disable a built-in fallback handler; text-field selection/clipboard/suggestion
  actions are not surfaces; `completion`/`compositeBuffer` contexts, custom settings/theme dialogs
  and plugin panels are outside the router. `docs/keybindings.md`.
- **R7-5 (P3, S/M)** — small LSP residuals: inline-hint placement tier 2, no built-in universal
  server (Fresh's quicklsp), `process_limits` parsed/unenforced, workspace diagnostics
  capability-gated (publish-only servers report their own set), no `$/cancelRequest` for pulls,
  and the documented stricter-than-Fresh stop-on-refused workspace-edit failure model.
  `docs/lsp.md`.

Round 8 recorded five gaps:

- **R8-1 (P2, L)** — the remainder of plugin tier 1c: `setLineTargets`; virtual inserted rows are
  cut to the pane (not wrapped) and the scrollbar thumb stays line-based; fixed-height group
  leaves accepted-ignored; no synthetic group tab or independent per-panel views; no live
  `windowEmbed` panes; no markdown-in-text widget; no dock/fullscreen placement; plugin
  explorer rows/decorations/slots; composite buffers recorded as a native substitution (API
  surface only). `docs/plugins/api-ui.md`.
- **R8-2 (P2, M)** — Markdown residual: multi-visual-row cells/escaped pipes/nested tables,
  nested-fence framing, heading scrollbar markers/marker conceal/heading polish, page-view mode
  + width session persistence, free per-split mode, mapped occurrences/rulers/indent guides/
  whitespace and composed plugin overlays, task/image/HTML conceal; dual exact row/byte anchors,
  a native Markdown menu row, split persistence and multiple duals. `docs/markdown.md`.
- **R8-3 (P2, M)** — settings residual: the plugin runtime exposes no declared-settings schema/
  values API (plugin settings are read-only keys, edited in the raw file), no plugin gallery/
  install page, no in-page key capture (deliberate — the editor overlay is the writer), env
  detection (`env.detectors`)/activation, per-field LSP dialogs. `docs/settings.md` §6.
- **R8-4 (P2, S)** — port reductions: compare_files (no synthetic group tab, no EOL-only hunks,
  fillers need virtual-line paint, positional pairing, ui.input paths, no `--diff` hook/export,
  opt-in duplicate palette rows), path_complete (no `prompt_changed` seam; exported engine + one
  command), lsp_help (payload lacks language/type/command, 16 hints, no workspace-symbol action),
  live_diff (plugin-side baselines, Myers vs patience caps, no collision-fg, large-file refused).
  `docs/plugins/porting.md`.
- **R8-5 (P2, M)** — search residual: preview that scrolls/syntax-colours, a multi-line match
  longer than the preview (first lines), replacing in very large files (skipped with a reason),
  provider scopes beyond files/current buffer; no persistent quickfix stack (Alt+Q overlay).
  `docs/ui.md`.

### 5.1 User-visible TUI and editing gaps (do first)

| # | Gap | Pri | Size | Owner area | Notes / depends on |
|---|---|---|---|---|---|
| 12 | Markdown residual: multi-visual-row cells/escaped pipes/nested tables, nested-fence framing, heading scrollbar markers/marker conceal/heading polish, page-view mode+width persistence, free per-split mode, mapped occurrences/rulers/guides/whitespace and composed plugin overlays, task/image/HTML conceal; dual exact row/byte anchors, native Markdown menu row, split persistence, multiple duals. The renderer (compose/fences/tables/borders/dual) landed rounds 6–8 | P1 | L | `editor`, `syntax`, `internal/tui/editorpane`, `internal/tui`, `session` | R8-2/R6-2 |

### 5.2 Plugin-API breadth (unblocks Fresh's plugin-era features)

| # | Gap | Pri | Size | Owner area | Notes / depends on |
|---|---|---|---|---|---|
| 13 | Plugin host capability tier 1c remainder (tiers 1a–1c landed rounds 5–8): `setLineTargets`, inserted-row wrapping/scrollbar thumb, fixed-height group leaves, synthetic group tab + independent per-panel views, windowEmbed live panes, markdown-in-text, dock/fullscreen placement, plugin explorer rows/slots; composite buffers recorded as a native substitution (API-surface only) | P2 | L | `pluginapi`, `plugin`, `decorations`, `internal/tui/pluginpanel`, `internal/tui/plugin_*.go` | R8-1; #15 closed round 8; composite decision §4 item 50/§5 |
| 14 | Port the remaining bundled plugins. Landed: diagnostics_panel (round 6), git_log/live_diff (round 7; full paths round 8), compare_files (round 8, opt-in), path_complete/lsp_help (round 8, reduced). Next: shared Finder lib + lsp_navigation + find_references, dashboard, code-tour; then git_grep (toolbar adaptation) + git_explorer (explorer slots API) + env-manager after env detection; daemon-era plugins (devcontainer, k8s-workspace, orchestrator/agent sessions, live_sessions, audit_mode, pkg) follow the architecture rounds. Native substitutions recorded: theme_editor, search_replace, markdown_compose/source, composite buffers | P1 | L each; XL total | `plugin/bundled/plugins`, `plugin/bundled/lib` | #13/R8-1; §5.5 round 9 |
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

1. **Round 9 — the remaining ports bundle and the Markdown residual slice.** Shared
   `plugin/bundled/lib/finder.lua` + lsp_navigation + find_references (one Finder prerequisite,
   S/M each), dashboard (M/L; existing host APIs, small wrappers), code-tour (M/L; 1c-B/C seams
   now landed; documented reductions for markdown-in-text/Utility Dock/describeWorkspace); plus
   the Markdown residuals most visible after dual: multi-visual-row table cells/escaped pipes/
   nested tables, heading scrollbar markers/marker conceal, page-view mode+width persistence
   (session fields), mapped occurrences/rulers/guides/whitespace and composed plugin overlays
   (R8-2). Start the terminal/search/theme sweeps or carry them to round 10.
2. **Round 10 — sweeps and the second port wave.** Terminal R5-4 (ConPTY first; then drag
   auto-scroll/word-wise drag, hover links, OSC 52/kitty/pass_ctrl_v; reflow/tabs/agent re-exec
   pair with the daemon/agent rounds); search R8-5 (preview scroll/colours, long multi-line,
   large-file replace, provider scopes); theme R5-3 (URL/packaged themes + package decision,
   accent/popup colours, colour picker/mouse editing, per-cell provenance, settings preview);
   git_grep (toolbar adaptation) and git_explorer (explorer slots API) ports; R7-4 keymap limits,
   R7-5 LSP leftovers, R8-3 settings residuals, R6-6 Contents.
3. **Round 11 — daemon, workspaces, agents, remote.** #18 daemon mode, #19 workspaces/windows,
   #20 agent sessions, #21 remote authority, #17 trust spawn gate + env detection; keep direct
   mode and `--no-plugins` working; env-manager/devcontainer/k8s-workspace/orchestrator/agent
   sessions/live_sessions/pkg/audit_mode ports and the composite API (if kept) follow the
   architecture.
4. **Round 12 — i18n and platform polish.** #22 i18n, #23 ConPTY completion, #24 update/telemetry
   decision, #25 platform input edges, #26 scope decision; R1-2/R1-3/R2-5/R4-4/R8-1 sweeps;
   real-host verification passes for the newer ports.
5. **Round 13 — final parity pass.** Re-read `refs/fresh`, re-run the §2 counts and the e2e
   feature index, refresh every partial row, record the deliberate endgame gaps (composite API,
   R8-1 renderer limits, native substitutions) and close the book on the goal.

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
  staleness), project-wide search/Git Grep, settings coverage R3-1 (68 settings in 13 sections then; 69/14 after round 6,
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
- **Round 6 landed (2026-11-08).** Six tracks complete (LSP 4b E — task-22/29 complete; plugin tier
  1b; the diagnostics_panel port; the Markdown compose core; Contents fold_buffer R1-1; search
  preview/multi-line); this audit ran the gates and reviewed the diff (no scope creep; §7):
  - **LSP 4b E** (`docs/lsp.md`): workspace diagnostics store (replaced by every 300 ms poll,
    cleared on stop, on-demand workspace poll; `internal/tui/lsp.go`), native sidebar Diagnostics
    panel (`internal/tui/diagpanel.go`: `sidebar.KindDiagnostics`, file→severity grouping, counts
    header/progress, selection/jump/scroll/empty states, F6 stage), quickfix Diagnostics List
    overlay that stays open, two hand-written palette rows (`commands/lsp_commands.go`, palette
    222→224), progress via header counts/"Diagnostics …"/empty-state text (no animation). Tests
    `internal/tui/diagpanel_test.go`, `commands/lsp_commands_test.go`. Residuals R6-5.
  - **Plugin API tier 1b** (`docs/plugins/api-ui.md`, `api-buffers.md`, `api-data.md`): interactive
    widget kinds (text/number/dropdown/radio/dualList, tree edit/drag) with host-owned instance
    state, `WidgetAction`/`WidgetMutation`/`widget_event` extensions and page mode
    (`internal/tui/pluginpanel/{interactive,interactive_state}.go`); group-lite buffer groups as
    real splits of the current tab (layout JSON, panel buffers/content/title/visibility/focus,
    ownership cleanup; `pluginapi/buffergroup.go`, `plugin/api_groups.go`, `internal/tui/
    plugin_groups.go`) with scroll-sync groups (anchors, interpolation); theme-file APIs
    (schema/data/list/user dir/file exists/save/delete/reload, ≤1 MiB validated) and
    `text_properties_at_cursor` (`internal/tui/plugin_themes.go`). API level stays 2
    (`gvim.d.lua` 210 functions/1316 lines then; 221/1376 after round 7). Residual remainder is R7-1.
  - **diagnostics_panel port** (`docs/plugins/bundled.md`): `plugin/bundled/plugins/
    diagnostics_panel/` (init.lua 473 lines + i18n + manifest, api 2, default on) built on the
    tier-1a widget panel and `gvim.lsp.all_diagnostics`; reduced Finder subset and v1-panel UX
    documented; no default key binding; 10 bundled/8 enabled and 15 bundled commands; idle heap
    ≈6.63 MiB of the 8 MiB gate. Tests `plugin/bundled/diagnostics_panel_test.go`.
  - **Markdown compose core** (`docs/markdown.md`): `internal/tui/editorpane/{compose,
    compose_layout,pageview}.go` + `syntax/markdown_markers.go` + `internal/tui/pageview.go`;
    page-width resolution/centering/hidden gutter, wrap at the measure with list/quote hanging
    indents, inline conceal with caret-line reveal, rules/quote bars, source↔composed mapping for
    caret/hit/selection/search/diagnostics/folds, large-file fallback, non-Markdown margins only.
    Tests `internal/tui/editorpane/{compose,compose_marks,pageview}_test.go`,
    `internal/tui/pageview_test.go`, `editor/markdown_pageview_test.go`. Deferred R6-2.
  - **Contents fold_buffer R1-1** (`docs/sidebar.md` §6): native `contents.fold_buffer` (user
    layer/restart, plugin-key fallback; `config/contents.go` + settings row), version-guarded
    outline fold source (`editor/outline_folds.go`, `internal/uiadapter/outline_folds.go`,
    `document/fold.go`), publish/re-apply/clear in `internal/tui/markdown_toc.go`; no fold
    command (Fresh parity). Tests `editor/outline_folds_test.go`,
    `internal/tui/markdown_toc_fold_test.go`, `config/contents_test.go`. Residual R6-6.
  - **Search extras R5-5 slice** (`docs/ui.md`, `docs/keybindings.md`): selected-match preview
    column (palette conventions, 64-cell threshold, 24..60 cols, ≥30 rows kept, bounded plain
    text) and multi-line regex rows with exact ranges (EndLine/EndCol carried through the port;
    replace applies the exact From/To). Tests listed by the keymap owner (panel_test preview/
    multi-line, app projectsearch multi-line/preview, e2e, root conversion). Residuals R6-3.
- **Round 6 integration note.** The Lead's two integration fixes are in his harness/README only
  (no code fixes found this round): the apparent page-view-then-Contents-fold issue was a harness
  test with a single heading, so it is not a bug; the memory figure and bundled 10/8 counts are in
  `docs/plugins/bundled.md` (README memory figure pending the Lead's doc fix).
- **Round 7 landed (2026-11-08).** Six tracks: LSP R6-5 complete, git_log/live_diff ports complete,
  markdown fences/tables complete, R2-4 closed; plugin tier 1c-A and settings R4-3 are partial
  (tasks 36/41 stay in progress). This audit ran the gates and reviewed the diff (no scope
  creep; §7):
  - **LSP R6-5** (`docs/lsp.md`): workspace edits now interleave `documentChanges` in server order
    with a version check per step; a refused/failed file op stops the rest and reports partial
    state + applied counts (stricter than Fresh, §4 item 18); workspace diagnostics for unopened
    files (capability-gated `workspace/diagnostic`, 5 s throttle, capability/open-count re-ask,
    published-set precedence, workspace-only `(not open)` rows capped 50/file, no
    `$/cancelRequest`) and `$/progress` text (create ack, cap 8, panel header/empty states, no
    animation); `LSPSession.StartLanguage` added for the settings page. Files
    `internal/tui/{workspace_edit,lsp}.go`, `lsp/{progress,requests,capabilities,handler}.go`,
    `internal/lsphost/lsp_data.go`, `internal/tui/diagpanel.go`; tests include
    `TestWorkspaceEditInterleavesResourceOpsAndTextEdits`, `…StopsAfterARefusedOperation`,
    `…PartialSuccessBeforeARefusal`, `TestWorkspaceEditRefusesStaleVersion`,
    `TestWorkspaceDiagnostics`, `TestCapWorkspaceDiagnostics`,
    `TestMergeWorkspaceDiagnosticsPrecedence`, `TestProgressBeginReportEnd`,
    `TestWorkspaceDiagnosticsRowMarksAndOpensAnUnopenedFile`. Residuals R7-5.
  - **Plugin tier 1c-A (partial)** (`docs/plugins/api-ui.md` virtual-text surface): new decoration
    store kinds (virtual lines above/below with gutter glyphs and text overlays, conceal with
    activation/scope/composeOnly, soft breaks with indent/prefix, cursor-line overlay, ranged
    scrollbar markers) + pluginapi/Lua surface (697-line `plugin/api_decor.go`, the `ns:set` kinds
    and convenience functions) + namespace-scoped plugin folds (`internal/tui/plugin_folds.go`),
    caps/docs: 64 virtual rows and 64 soft breaks per line, 256 conceal spans per line, 2000 fold
    ranges per namespace, plus the existing quotas. Painting (pane row insertion/source↔display)
    and the 1c-C seams are deferred, so the API is present-but-unrendered;
    `gvim.has("decor_virtual_paint")` is false and the ports feature-detect it. Tests
    `decorations/tier1c_test.go`, `plugin/decor_tier1c_test.go`. R7-1.
  - **git_log port** (`docs/plugins/bundled.md`): group-lite log/detail/hint splits over the
    current tab, streaming `git show --stat --patch` detail with a disk cache, Enter-to-file,
    `j/k`/Page/Home/End/Tab/q/Esc/r/y modes, 4 palette + 13 hidden actions, no default bindings;
    reductions: no synthetic group tab, no HEAD watcher, hint-row toolbar, plugin-side detail
    decorations, folds/window-state feature-detected inert; nothing runs until opened; off by
    default for the idle gate. Files `plugin/bundled/plugins/git_log/` (1601-line init.lua),
    tests `plugin/bundled/git_log_test.go`. R7-2.
  - **live_diff port** (`docs/plugins/porting.md`): per-buffer modes HEAD/disk/branch/off with
    defaults + overrides, plugin-side baseline fetch + `gvim.diff.buffer` (Myers caps), reduced
    rendering (line backgrounds/word emphasis/gutter/scrollbar marks; full virtual-line old-
    content path written and gated on `decor_virtual_paint`), debounced refresh on edits/
    switches/reverts/HEAD+disk watches, large-file refusal, opt-in. Files
    `plugin/bundled/plugins/live_diff/` (1465-line init.lua), tests
    `plugin/bundled/live_diff_test.go`. R7-2.
  - **Markdown fences/tables** (`docs/markdown.md`): `syntax/markdown_fences.go` +
    `internal/tui/editorpane/compose_fence.go` (label/rails/padding/syntax kinds/reveal/nesting
    fallback) and `syntax/markdown_tables.go` + `compose_table.go` (classification, natural
    widths per text version, proportional shrink, colon alignment, padded cells, mid border from
    the source separator, per-row reveal, exact caret/hit/selection mapping; tables in fences
    literal). Tests `compose_fence_test.go`/`compose_table_test.go`. Residual: top/bottom border
    rows need the page-view extra-row hook (1c-B); multi-visual-row cells and nested fences also
    residual (R7-2).
  - **Keymap R2-4 closed** (`docs/keybindings.md`): the prompt/searchPrompt/popup/menu/settings/
    dock overlays consult the keymap in Fresh's contexts before built-ins with fall-through;
    presets own/in-effect default 245/243, macos 60/266, macos-gui 52/317, emacs 98/309, vscode
    9/248; the loader accepts the contexts and warns+skips multi-key bindings. Files
    `internal/tui/overlay_keys.go`, `keymap/*.json`, `internal/keybindings/`; tests
    `overlay_keys_test.go`, `keymap/overlay_contexts_test.go`. Residual R7-4.
  - **Settings R4-3 (partial)** (`docs/settings.md`): non-scalar page plumbing (info/locked/
    inherited/action/filter/list-path/whole-array JSON/JSON validation/`SettingsPort.Act`), the
    Language Servers page (effective built-in/user/universal rows with live manager state,
    start/restart/stop action rows, whole-array validated JSON per language, project-layer locks)
    and read-only Environment facts (shell + source, TERM, icon set, OS/arch, config files,
    trust). Files `internal/app/settings_{lsp,runtime,plugins}.go`, `internal/tui/
    {settings_actions,settings_structure}.go`; tests `internal/app/settings_lsp_test.go`,
    `internal/tui/settings_plumbing_test.go`. Remainder R7-3.
- **Round 7 integration note.** The Lead fixed four wrap-up lint findings in the new
  diagpanel/settings_structure/settings_runtime files (no semantic change); the leftover
  verification harness was removed before the commit. `gvim.d.lua` is 221 functions/1376 lines,
  API level 2; palette stays 224; bundled plugins 12 (8 default-enabled, idle heap 6.75 MiB of
  the 8 MiB gate; git_log off: 9-enabled 8.68 MiB); bundled commands 28.
- **Round 7 Lead verification.** The Markdown code-fence/table rendering and the Settings
  Language Servers page were confirmed on screenshots of the real app. The two Lua ports were
  exercised only through their plugintest/fake-host end-to-end tests: a real-host harness run
  with `plugins.git_log.enabled`/`plugins.live_diff.enabled` showed both plugins loading (their
  init logs) but their palette command rows did not appear in that harness, so opening them from
  the real palette is unverified. This is a round-8 verification item (a candidate real defect in
  the config-locked enable → command-registration path, or a harness timing artefact), and it is
  part of why the ports are recorded as landed-with-documented-reductions.
- **Round 8 landed (2026-11-08).** Seven tracks complete (tier 1c-B/C, #15 + compare_files,
  markdown dual, settings R7-3, small ports + palette verification, R7-2 follow-ups, search
  R6-3 slice); this audit ran the gates and reviewed the diff (no scope creep; §7):
  - **Plugin tier 1c-B/C** (`docs/plugins/api-ui.md`): `internal/tui/editorpane/rows_extra.go`
    (one source↔display row transform: extra rows + soft breaks cached per line and used by
    row/caret/hit/selection/scroll; documented composition order), `conceal.go`, virtual lines
    above/below (gutter glyph/text overlays/priority/64-row cap), conceal
    (activation/scope/composeOnly), soft breaks (indent/prefix), cursor-line overlay, plugin
    folds with pane integration + fold-first toggle (`plugin_folds.go`), `Pane.PageExtraRows`
    shared with the page-view table borders; 1c-C seams (`plugin_seams.go`: split labels,
    per-buffer window state, open/focus/close wrappers; `plugin_events.go`: path_changed/
    focus_gained/active_window_changed/buffer_group_closed); `gvim.has("decor_virtual_paint")`
    true via FakeHost SetFeature. Group-layout ratio inversion found and fixed (first-child
    share conversion) with `plugin_groups_ratio_test.go`. Tests `rows_extra_test.go`,
    `plugin_decor_*`, `plugin_seams_test.go`, `decor_capability_test.go`. Residual R8-1.
  - **#15 compare/diff API + compare_files** (`docs/compare.md`, api-system#gvimdiff):
    `pluginapi/compare.go` (CompareResult/CompareSide/CompareHunk/DiffSource/DiffHost),
    `internal/pluginhost/compare.go`, `plugin/compare_geom.go` (UTF-8-safe spans, jump-at-gap,
    positional pairing), Lua `gvim.diff.compare_buffers/replace_range` (byte model, one-edit/
    one-undo, false-not-error refusals, `eol_only` always false), the `plugins/compare_files/`
    port (8 commands/no default keys, group-lite splits, hunk-anchored scroll sync, Fresh
    decorations, copy/take via replace_range, opt-in default_enabled=false); docs rows, limits
    caps, porting reductions. Tests `compare_geom_test.go`, `data_compare_test.go`,
    `internal/pluginhost/compare_test.go`, `compare_files_test.go`. Residual R8-4.
  - **Markdown dual view** (`docs/markdown.md`): `internal/tui/dual.go` (derived per-pane page-
    view override, 50/50 split of the same document, source focus, line-anchored compare-style
    scroll sync, shared doc/caret + compose hit/selection mapping, validity clearing), five
    hand-written palette rows (224→229) and mode notices; table top/bottom borders landed
    through `Pane.PageExtraRows` (`pageview_table_borders.go`). Tests `dual_test.go`,
    `pageview_table_border_test.go`. Residual R8-2.
  - **Settings R7-3 closed** (`docs/settings.md`): `internal/app/settings_{languages,plugins,
    keybindings}.go` + TUI sides (per-language structured values with inherited display/reset-
    vs-inherit whole-object writes, Plugins page with user-layer toggle/rollback/reload and
    read-only settings, read-only Keybindings page + Open Keybinding Editor action), docs/lsp
    settings paragraph. Tests as above. Residuals R8-3.
  - **Small ports + palette verification** (`docs/plugins/bundled.md`): path_complete (faithful
    completion engine + exported `complete()` + one prompt command; no prompt_changed seam —
    porting residual) and lsp_help (status hint on lsp_server_error, action popup on
    lsp_status_clicked with 16 language hints + native quick actions; payload limitations
    documented); the real-host palette investigation closed as a harness/order artefact, with
    `internal/app/plugins_ports_palette_test.go`/`plugins_round8_test.go` as permanent real-host
    tests. Bundled 15 (10 on), heap ≈7.9 MiB of the 8 MiB gate.
  - **R7-2 follow-ups** (`docs/plugins/porting.md`): live_diff full virtual-line rendering active
    (capability true; reduced path for old hosts; real-host vs HEAD test), git_log folds +
    per-commit detail scroll restore via window state (real-host from palette), group ratio
    defect fixed; round-7 palette finding concluded a harness/order artefact.
  - **Search R6-3 slice** (`docs/ui.md`, `docs/keybindings.md`): streaming/batched project
    search (SearchBatch/BatchSearcher contract, ≤128 files/≤1000 matches per batch, one
    goroutine, cancel, one-shot fallback; panel progressive append/progress/partial-replace)
    and the Git Grep Files pathspec field (git semantics, validation/invalid-block, after
    "--"). Tests `panel_test.go` streaming set, `projectsearch_test.go` batch set. Residual
    R8-5.
- **Round 8 integration notes.** The Lead fixed one lint finding (QF1007) in
  `internal/app/projectsearch.go` (semantic-neutral, inside the streaming change) and deleted the
  temporary harnesses; the plugin fixed the reload-picker regression (`Ranked` now carries its
  query and `rowsQuery`, Enter ranks synchronously while a ranking is in flight) that was the
  last red test. Metrics: palette 229; `gvim.d.lua` 231 functions/1441 lines; bundled 15/10/5;
  bundled command registrations 30 call sites; idle heap ≈7.9 MiB of the 8 MiB gate; API level 2.
- The gaps the rounds opened are at the top of §5 (R1-2…R1-4, R2-5, R3-3…R3-4,
  R4-4…R4-5, R5-2…R5-6, R6-1…R6-6, R7-4…R7-5, R8-1…R8-5; #7/#8/#11/#15, R1-1, R2-4,
  R5-1, R6-5 and R7-1/R7-2/R7-3 are closed, and the slice closures of earlier rounds are in);
  §5.5 is the round-9 plan built from the ranked backlog minus the closed items.

- The command palette table is generated from Fresh; keep `commands/gen/generate.py` and
  `tools/gen_menus.py` in the parity loop. Regenerating after a Fresh update is the cheapest way
  to find newly added commands/menus; `tools/gen_menus.py --check` reports drift.
- When a Fresh release lands, re-run the counts in §2 (commands, keymaps, themes, plugins,
  plugin API) and update the matrix rows for the areas that changed.

## 7. Verification limits and risks

- Statuses are read evidence from the tree at the time of writing. Rounds 2–8 had their full
  gate sets run by this audit on their trees: build ×2, vet, golangci-lint 0 issues, gofmt clean,
  purity and import rules green in every round. Round 8's go test ./... -count=1 had one
  load-sensitive timing failure on the loaded run — `plugin`
  `TestHeatBusyTimerIsThrottled` (the 12 ms/10 ms timer was not found while the package ran under
  load); it passes repeatedly when re-run isolated (2×), so it is recorded as a load flake, not
  a regression. Round-1 statuses were read-code only (no gates were run by this audit then). A
  feature that exists but is broken would still read "implemented": treat a status as a pointer,
  not proof.
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
- Round-2/3/4/5/6/7/8 diff reviews (this audit): no scope creep outside the declared tracks.
  Shared files stayed in scope (round 8: `decorations/`, `pluginapi/` + `pluginapi/compare.go`,
  `internal/tui/{model,split,split/*,view,pageview,motion_mouse,plugin_*,editorpane}`,
  `internal/tui/picker`, `internal/pluginhost/`, `internal/app/settings_*` + search/ports,
  `plugin/{compare_geom,data_compare,hostcall}.go`, the three port dirs and the docs records),
  and the generated tables, `refs/` and `_attic/` were not touched; no golden tests were added.
  The Lead's QF1007 lint fix in projectsearch.go is semantic-neutral and the temporary harnesses
  were deleted; the plugin's reload-picker fix (Ranked query/rowsQuery) is covered by
  `picker_test.go`. Round-8 findings (recorded, not fixed by the audit):
  (a) `docs/plugins/bundled.md` still contains the older “8.68 MiB” git_log-on measurement
  sentence beside the current 7.9/8 gate table — port/Lead docs fix (the current gate figure is
  ≈7.9 MiB of 8 for the 10 default-enabled plugins; compare_files never passed the gate enabled);
  (b) no code bugs found in spot checks: rows_extra ordering/cache mappings, conceal/soft-break
  activation, dual per-pane page view/scroll sync/hit mapping, compare geometry/spans/replace
  semantics, group ratio conversion, settings languages inherited display/plugin rollback/
  keybindings list, streaming batches/cancel/one-shot fallback, Git Grep pathspecs, path_complete
  engine, lsp_help events, live_diff/git_log real-host paths — all match docs/tests;
  (c) palette 229, API level 2, bundled 15/10/5 and ≈7.9 MiB verified from the tree;
  (d) the deliberate divergences are §4 items 18, 40–53, including the native dual/compare
  substitutions and the recorded composite-buffer native substitution.
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
