# Fresh parity record

This file is the feature parity matrix between gvim and Fresh (the Rust TUI editor checked out
read-only under `refs/fresh`), the list of deliberate differences, and the backlog of known gaps. It is
not a development log: what landed and when lives in `git log`, and each feature's behaviour lives
in its own doc (`docs/ui.md`, `docs/lsp.md`, `docs/terminal.md`, `docs/plugins/`, ...).

Scope is Fresh's TUI functionality. The Lua plugin runtime and gvim's visual style are the only
expressly flexible parts; the product boundary is recorded in section 6.

**How to update it.** When a feature lands or changes, edit the affected row of section 3 and the
matching entry of section 5 in place. Delete a backlog entry when its gap closes; do not leave a
"closed" note or a round narrative behind. Keep every path in backticks real: run the path check
in section 1 after an edit. Status words are the ones in section 1. "UNTESTED" means the code
exists and is unit-tested but has never been run against a real host (SSH server, tmux, Windows
console, OS keychain).

Last checked against the tree at `cb0fca5` plus the working-tree changes of 2026-11-13, and the
`refs/fresh` checkout of the same date.

## 1. Method

| Status | Meaning |
|---|---|
| **implemented** | The capability exists and is exercised by tests or docs at or above Fresh's level for that area. |
| **partial** | The core exists but named Fresh sub-features are absent or reduced; the row lists them. |
| **missing** | No implementation was found. |
| **different** | Deliberate and allowed divergence (section 4), not a gap. |

A status is backed by a package or file read, a test, or a doc that states the behaviour.
"Missing" means a grep of the tree (excluding `refs/`) found nothing. Cheap ways to re-check a row:

- **Feature exists:** `grep -rn "<symbol>" --include='*.go' . | grep -v refs`, then read the nearest
  file; tests are named after the feature, so `go test ./<pkg> -run <Name>` finds the contract.
- **Dispatch gaps:** a handler that only assigns `m.notice` is a stub. The dispatch floor is
  `unhandledActions` in `internal/uiadapter/dead_bindings_test.go` (today one bound action,
  `file_explorer_search_backspace`) and `notApplicable` in `internal/tui/notice_na.go`.
- **Generated tables:** `python3 commands/gen/generate.py refs/fresh` and
  `python3 tools/gen_menus.py --check`; keymap presets are plain JSON under `keymap/`.
- **Config claims:** grep the option name inside and outside `config/`; an option that is parsed
  but never consumed is listed in section 4.
- **Fresh side:** `refs/fresh/docs/features/<name>.md` for intent, the matching
  `crates/fresh-editor/tests/e2e/<name>.rs` for behaviour, and
  `crates/fresh-editor/plugins/<name>.ts` when it ships as a bundled plugin.
- **Renderer claims:** assert a few cells or rows in a `*_test.go` (no golden frames, see
  `CLAUDE.md`); `cmd/gvimdebug` is the visual check.
- **Path check for this file:** every backticked `*.go`/`*.md`/`*.json`/`*.lua` path in it should
  exist; a small script that walks the backtick spans and calls `os.path.exists` is enough.

## 2. Roll-up

| Measure | Fresh | gvim | Where to re-measure |
|---|---|---|---|
| Palette commands | 206 `CommandDef` rows | 243: the 206 generated rows plus 37 gvim rows (menu bar, live grep, project search, theme editor, compare, Contents, diagnostics, plugin management, agents, terminal tabs, hex view, update) | `commands/palette_test.go` (`TestCommandsFromBuiltinTable`) |
| Menus | 8 menus, 103 static actions, 31 separators, 2 static submenus + 1 dynamic | identical static table; the dynamic "Copy with Formatting" submenu lists the built-in themes; plugins add up to 32 rows | `internal/tui/menubar/menu_table_gen.go`, `python3 tools/gen_menus.py --check` |
| Keymap presets | 5 | 5 (default, macos, macos-gui, emacs, vscode) with Fresh's inheritance graph; own/in-effect bindings default 269/266, macos 61/289, macos-gui 52/340, emacs 98/331, vscode 11/273 | `keymap/common_set_test.go` |
| Themes | 10 built-in, files by name/path/URL | 10 built-in plus user themes, plugin theme files, URL themes (cached, validated); no packaged repository | `theme/data/*.json`, `docs/themes.md` |
| LSP server entries | built-in list plus universal servers | 62 built-in entries; `universal_lsp` is config only | `lsp/manager/defaults.go` |
| Bundled plugins | 50 | 37, of which 9 are on by default (`git_blame`, `diagnostics_panel`, `git_gutter`, `git_find_file`, `goto_with_selection`, `git_statusbar`, `lsp_help`, `path_complete`, `tab_actions`) | `plugin/bundled/plugins/*/plugin.json` (`default_enabled`), `docs/plugins/bundled.md` |
| Plugin API | about 372 methods (TypeScript) | about 250 Lua functions, API level 2 | `docs/plugins/gvim.d.lua` (checked against the runtime by `plugin/docs_test.go`) |
| Settings page | schema-driven page over every setting | 79 rows in 14 sections plus injected Languages, Language Servers, Plugins, Keybindings, Environment and Detectors pages | `config/settings_schema.go`, `docs/settings.md` |
| i18n catalogues | 15 locales | en (1203 keys) and zh-CN (581 keys); other locales are out of scope | `i18n/locales/*.json`, `docs/i18n.md` |
| Idle plugin heap | n/a | the 9 default-enabled plugins cost about 3.0 MiB against an 8 MiB test gate | `TestBundledPluginsIdleHeap`, `docs/plugins/bundled.md` |
| e2e feature index | 413 test files | not comparable; gvim uses Go unit tests | `refs/fresh/crates/fresh-editor/tests/e2e/` |

## 3. Matrix by area

### 3.1 Engine and editing

| Area | Fresh features | gvim status | gvim evidence | Open gaps |
|---|---|---|---|---|
| Text model, large files, encodings | rune text, undo/redo, two-mode large files, encoding detect and convert (UTF-8/16, Latin-1, CP125x, GBK/GB18030, Shift-JIS, EUC-KR), EOL detect | **implemented** | `buffer/`, `document/`, `encoding/`, `docs/architecture.md` | none found; detection heuristics may order differently |
| Binary, image and hex viewing | n/a (gvim additions) | **implemented** | `internal/tui/hexview.go`, `internal/tui/imageview.go`, `docs/binary.md`, `docs/images.md` | not a Fresh parity item |
| Editing core and smart editing | auto-indent/close/surround, smart home/backspace, bracket match, comment, case, transpose, sort/trim, shell command, Markdown list editing | **implemented** | `document/`, `editor/comment.go`, `editor/shell.go`, `editor/markdown.go`, `plugin/bundled/plugins/vi_mode/` | the `vi_mode` plugin has no replace mode (`R`) |
| Selection, multi-cursor, marks | Ctrl+D, cursors above/below/at line ends, block selection, mark mode, mouse word/line/drag selection | **implemented** | `document/block.go`, `editor/marks.go`, `internal/tui/motion_mouse.go`, `keymap/default.json` | `add_cursors_to_line_ends` has no default key (Fresh's preset binds it) |
| Undo, save pipeline, file metadata | one-undo-step edits, atomic saves, interrupted-save recovery, permissions/read-only, final newline and trim on save, auto-save, auto-revert | **implemented** | `atomicfile/`, `editor/files_recovery.go`, `editor/interrupted.go`, `internal/restore/`, `docs/files.md` | no sudo save prompt |
| In-buffer search and replace | find, replace, query replace, find in selection, case/word/regex toggles, history, match counts | **implemented** | `search/`, `internal/tui/find/`, `internal/tui/find_route.go` | none found |
| Navigation, bookmarks, macros | position history, go to line/bracket, bookmarks 0-9 with a list, macros with save-to-init and promote-to-command | **partial** | `editor/goto.go`, `editor/marks.go`, `internal/tui/root_bookmarks.go`, `internal/tui/bookmarks_picker.go`, `internal/tui/macro.go` | one macro slot (no registers 0-9); `Macro: Save to init` and `Promote to command` write an `init.lua` block where Fresh writes `init.ts` |
| Display and view options | line numbers, wrap and wrap column, page view, virtual space, rulers, indent guides, whitespace marks, current line/column, occurrence highlight, scrollbars with markers, inline diagnostics, inlay hints, semantic tokens, animations, screensaver | **partial** | `document/virtualspace.go`, `document/block.go`, `internal/tui/editorpane/`, `internal/tui/virtual_space.go`, `docs/markdown.md` | animations, edge fade and screensaver are parsed but never drawn (section 4); virtual space is off under wrap, multi-caret and large-file mode, and the per-buffer toggle is bool-only |
| Folding | indent folds, LSP `foldingRange`, outline/Contents folds, plugin namespace folds, fold-first toggle | **implemented** | `editor/fold.go`, `document/fold.go`, `editor/lsp_folds.go`, `editor/outline_folds.go`, `internal/tui/plugin_folds.go` | a source that changes ranges without an edit refreshes on the next edit or open; no fold command (as in Fresh) |
| Splits, tabs, panes | vertical/horizontal splits, tab strip and drag, preview tabs, buffer groups, split labels, per-buffer window state, line targets, multiple windows | **partial** | `internal/tui/split.go`, `internal/tui/tabs.go`, `internal/tui/tab_drag.go`, `internal/tui/plugin_groups.go`, `internal/tui/plugin_seams.go`, `internal/tui/plugin_linetargets.go` | no synthetic group tab or per-panel views; "Move tab to other split" is a notice |
| Compare files | `--diff A B`, editable side by side, copy/take change, next/prev difference, plugin compare API and the `compare_files` plugin | **implemented** (native view, plugin API, opt-in plugin port) | `internal/tui/compare.go`, `docs/compare.md`, `pluginapi/compare.go`, `internal/pluginhost/compare.go`, `plugin/bundled/plugins/compare_files/` | see "Plugin ports" in section 5 for the `compare_files` reductions |
| Markdown | highlighting, source smart editing, page view (compose), tables, code-fence frames, dual view, heading conceal and scrollbar marks, page-view persistence, Contents section | **partial** | `syntax/markdown*.go`, `editor/markdown.go`, `internal/tui/editorpane/compose*.go`, `internal/tui/dual.go`, `internal/tui/sidebar/contents.go`, `session/session.go`, `docs/markdown.md` | exact byte anchors for the dual view (a line anchor is used), a native Markdown menu row, wrapped-cell selection paints only the primary row |

### 3.2 Project, chrome and settings

| Area | Fresh features | gvim status | gvim evidence | Open gaps |
|---|---|---|---|---|
| File explorer and sidebar | tree, file create/delete/rename/copy/cut/paste, hidden/gitignored toggles, git marks, plugin decorations and slots, width/side/follow/compact options, sticky parents, sidebar sections with session state, remote tree | **partial** | `explorer/`, `internal/tui/explorer/`, `internal/tui/sidebar/`, `internal/tui/remote_tree.go`, `internal/app/remote_explorer*.go`, `docs/sidebar.md` | `respect_gitignore`, `custom_ignore_patterns`, `preview_tabs` and the indicator glyph keys are accepted and ignored; the side toggle is session-only; Fresh's rows-or-percent section sizing (gvim stores rows); explorer drag/move, undo, whole-subtree transactions, and mode/mtime/hardlink/xattr preservation on copy are not implemented (details in section 5) |
| Finder, quick open, live grep | Ctrl+P modes, fuzzy ranking, streaming enumeration (local and remote), file preview, live grep with provider toggles, quickfix export, resume | **partial** | `finder/`, `internal/tui/palette/`, `internal/app/live_grep.go`, `internal/app/remote_finder.go`, `docs/ui.md` | no workspaces provider; Resume re-runs the search with the saved controls instead of restoring cached matches; palette rows carry no git letters |
| Project search, replace, Git Grep | Search & Replace panel (match tree, scoped replace, globs, quickfix, preview, streaming), git grep finder | **implemented** core, **partial** against the plugins | `search/plan.go`, `internal/app/projectsearch.go`, `internal/app/gitgrep.go`, `internal/tui/searchpanel/`, `internal/tui/project_search.go`, `docs/ui.md`, `docs/keybindings.md` | replacing a closed file above the search read cap has no streamed writer; no recent-files, diagnostics or terminal provider; the quickfix list is a transient overlay, not a stack |
| Command palette | 206 core commands plus plugin commands, prefix modes, key hints | **implemented** core, **partial** plugin commands | `commands/`, `internal/tui/palette/` | `update_fresh`, `open_update_log` and `trigger_wave_animation` answer "not available in gvim" (`internal/tui/notice_na.go`) |
| Menu bar, context menus, help, welcome | 8 menus, mnemonics, context menus, help overlay, welcome screen, dashboard | **implemented**; the dashboard is a partial plugin equivalent | `internal/tui/menubar/`, `keymap/menu.go`, `internal/tui/context_menu.go`, `internal/tui/help.go`, `internal/tui/welcome.go`, `plugin/bundled/plugins/dashboard/`, `docs/ui.md` | Help's label row says "gvim" where Fresh writes its version; dashboard gaps are under "Plugin ports" in section 5 |
| Status bar | configurable element lists, separator, click actions, squeeze rule, `{filename}`/`{chord}`/`{clock}`/`{update}` | **implemented** | `internal/tui/frame/frame.go`, `internal/tui/status_elements.go`, `internal/tui/update_status.go`, `docs/ui.md` | `{warnings}` and `{plugin:token}` elements are parsed but not painted (plugin status items cover the use case) |
| Config layers and Settings UI | user and project layers, config write from toggles, Settings page, config reload, LSP/plugin/keybinding/env/locale pages | **partial** | `config/settings_schema.go`, `config/settings_structured.go`, `config/patch.go`, `internal/app/settingssvc.go`, `internal/app/settings_*.go`, `internal/tui/settings.go`, `docs/settings.md` | declared plugin settings are read-only on the page; LSP fields are edited as whole-array JSON; no in-page key capture; no per-field reset-vs-inherit display or full undo; Reload is a palette row only; Fresh's generic widget runtime is not ported |
| Keymaps and keybinding editor | 5 presets, user bindings, unbind, chords, keybinding editor, overlay contexts | **partial** | `keymap/`, `internal/keybindings/`, `internal/tui/overlay_keys.go`, `internal/tui/keybind_editor.go`, `docs/keybindings.md` | overlay contexts take single-key bindings only (the pending-chord state is root-only); the `completion`, `compositeBuffer` and text-field contexts are not keymap surfaces |
| Themes and appearance | built-in themes, theme files by name/path/URL, extends, colour picker, inspect with provenance, plugin theme overrides, background images | **partial** | `theme/`, `internal/tui/theme_editor.go`, `internal/tui/theme_picker.go`, `internal/tui/theme_inspect.go`, `internal/app/theme_url.go`, `docs/themes.md` | no packaged repository/marketplace form (`repo#fragment` is refused), URL fetch is startup-only, no background images (terminal cell grid), no drag-and-drop of theme values, inspect falls back to colour matching for untagged painters (compose, page view, plugin overlays) |

### 3.3 Services

| Area | Fresh features | gvim status | gvim evidence | Open gaps |
|---|---|---|---|---|
| LSP | diagnostics, hover, definition/references, completion (server and buffer words, snippets, auto-import, resolve, auto-popup, trigger characters), rename, code actions and file ops, signature help, format, folds, inlay hints, semantic tokens, diagnostics panel, multi-server routing, universal servers, status and mute, pull and workspace diagnostics, progress, process limits, remote LSP over SSH | **partial** | `lsp/`, `lsp/manager/`, `internal/lsphost/`, `internal/tui/lsp.go`, `internal/tui/diagpanel.go`, `internal/app/remote_lsp.go`, `docs/lsp.md` | no built-in universal server; `process_limits` is enforced on Linux only (cgroup v2, `ulimit -v` fallback), as in Fresh; workspace-edit application stops at the first refused file operation where Fresh continues (section 4); a remote server gets one restart attempt per reconnect |
| Plugins runtime and host API | TypeScript on QuickJS, about 372 methods, overlays, virtual text, folds, line targets, explorer decorations, project environment, prompts, windows, composite buffers, buffer groups, terminals, authority, HTTP, widgets and dock panels, i18n, package manager | **partial**, runtime **different** | `plugin/`, `pluginapi/`, `internal/pluginhost/`, `internal/tui/plugin_*.go`, `internal/tui/pluginpanel/`, `docs/plugins/` | composite buffers (native substitution only), dock and fullscreen placement, independent per-panel views, `windowEmbed` live panes, markdown-in-text, package manager, generic HTTP (`gvim.http.get` is a bounded HTTPS GET), project-local plugins, `render_line`; plugin `fs.remove` is unsupported on remote workspaces |
| Bundled plugins | 50 | **partial**: 37 ported or reduced, plus native equivalents where they genuinely replace one | `plugin/bundled/plugins/`, `plugin/bundled/lib/`, `docs/plugins/bundled.md`, `docs/plugins/porting.md` | see "Plugin ports" in section 5 |
| Terminal | PTY panes and dock, tabs, live and scrollback modes, OSC 7 paths, launch and agent records, restart, selection and copy, mouse forwarding, drag-select, link hover, ConPTY, SSH terminal, OSC 52, live restore | **partial** | `internal/terminal/`, `internal/tui/dock/`, `internal/restore/terminal.go`, `config/terminal.go`, `docs/terminal.md` | see "Terminal" in section 5 |
| Git surfaces | review diff, git log with live diff, blame, live diff baselines, diff chunk navigation, git grep, git explorer, git find file, statusbar branch, gutter marks | **partial** | `gitstate/`, `plugin/bundled/plugins/git_*`, `plugin/bundled/plugins/live_diff/`, `plugin/bundled/plugins/audit_mode/`, `plugin/bundled/plugins/merge_conflict/`, `internal/tui/git.go` | the review diff is read-only (no stage, discard, comments or export); no range or stash reviews; merge view is reduced to in-buffer resolution; blame has no history stepping beyond "previous revision" |
| Session, restore, daemon | session restore/save, hot exit, crash recovery, split-layout persistence, `--restore`/`--no-restore`/`--simple`, daemon with attach/detach and open-file forwarding, workspace registry, multi-client attach, auto-attach | **partial** | `internal/app/session_plan.go`, `internal/app/session_open.go`, `internal/app/daemon_runtime.go`, `internal/app/workspace_host.go`, `internal/daemon/`, `internal/cli/`, `session/`, `internal/restore/`, `docs/architecture.md` | see "Daemon and workspaces" in section 5 |
| Workspaces, agents | multiple workspaces per daemon, switcher, Run Agent, agent discovery and resume, extract to new workspace | **partial** | `internal/daemon/workspaces.go`, `internal/app/workspace_*.go`, `internal/agentdiscover/`, `internal/tui/agent_picker.go`, `internal/tui/workspace_host.go` | see "Daemon and workspaces" in section 5 |
| Remote authority | SSH editing, remote file system, processes and terminals, remote LSP, watch, finder, search, replace, explorer writes, env, SSH auth and config | **partial**, real hosts UNTESTED | `authority/`, `internal/app/remote_*.go`, `internal/pluginhost/authorityos.go`, `document/remote_large.go`, `docs/authority.md` | see "Remote authority" in section 5 |
| Workspace trust | per-folder Restricted/Trusted/Blocked, marker detection, open-time prompt, status pill, per-spawn executable gate, config-layer gating | **implemented** | `trust/`, `internal/app/trust.go`, `internal/app/config_trust.go`, `internal/tui/trust.go`, `docs/trust.md` | spawn-gate residuals under "Trust" in section 5 |
| Diagnostics surfaces | gutter marks, underline, inline text, panel, quickfix list, navigation, hover, pull/workspace diagnostics, progress | **implemented** | `internal/tui/diag.go`, `internal/tui/diagpanel.go`, `internal/tui/editorpane/inline_diag.go`, `commands/lsp_commands.go`, `docs/lsp.md` | plugin `DiagnosticInfo` carries no `code` field (native diagnostics do); publish-only servers report only what they choose |
| Scripting and CLI automation | `--cmd` script channel, `daemon open-file --wait`, stdin programs, `init.ts`, agent channels | **partial** | `internal/cli/`, `internal/app/stdin.go`, `internal/tui/plugin_init.go`, `config/` | gvim has `init.lua`, stdin-as-a-buffer, `gvim config show/paths`, `gvim grammar` and the daemon flags; Fresh's external `--cmd` script channel and `open-file --wait` are missing |
| Internationalization | 15 locales, locale detection and config, plugin i18n | **partial** by decision | `i18n/`, `tools/i18n_check.sh`, `internal/app/i18n_text.go`, `docs/i18n.md` | only en and zh-CN ship; rows without Fresh keys, structured schema names and some keybinding-help text stay English |
| Update | update check, status-bar element, self-update, update terminal | **partial** | `updatecheck/`, `config/config.go`, `internal/app/update_check.go`, `internal/tui/update_status.go`, `internal/tui/update_terminal.go`, `commands/update_commands.go` | check, indicator, and a user-configured update command in a dock tab only; no download/install engine |
| Platform input and OS integration | CSI-u disambiguation, AltGr/CapsLock quirks, key release, GPM mouse, suspend, ConPTY, OSC 52 (write and opt-in read), window title, focus events | **partial** | `internal/terminal/`, `internal/tui/input_edges_test.go`, `internal/tui/clip.go`, `internal/app/clipboard_osc52.go`, `config/clipboard.go`, `docs/keybindings.md` | no GPM console mouse, no kitty keyboard flags from the host side, key release only where the backend reports it, terminal-window focus used only for remote reconnect |

## 4. Deliberately different (allowed)

1. **Plugin runtime.** Lua 5.1 on gopher-lua, not TypeScript on QuickJS. `.js`/`.ts` plugins are
   never run and are reported once (`docs/plugins/porting.md`). The language difference is
   allowed; missing capabilities are gaps.
2. **TUI component style.** A custom cell grid on Bubble Tea v2 with the chrome described in
   `docs/ui.md`, not Fresh's retained-mode tree. Layout and paint differences are allowed;
   missing interactions are not.
3. **Pure-Go binary.** `tools/purity_check.sh` and the import rules replace Fresh's Rust/QuickJS
   architecture. Performance strategies (chroma vs syntect, Myers vs patience diff) may differ.
4. **Compare presentation.** Two ordinary tabs marked as a pair (`docs/compare.md`) instead of
   one composite-buffer tab; both sides stay editable.
5. **Overlay keymap contexts.** `prompt`, `searchPrompt`, `popup`, `menu`, `settings` and `dock`
   consult the keymap before their built-in handling and fall through when unbound
   (`docs/keybindings.md`); single-key bindings only.
6. **Project config is weaker and trust-gated.** `.gvim/config.json` cannot set `keymap`,
   `keybindings`, an LSP `command`, a `theme` URL, or the user-only keys (`auto_attach`, `ssh`,
   `check_for_updates`, `update_command`, `terminal.restore_live`, ...), and the whole project
   layer is skipped unless the workspace is Trusted. Fresh merges the project config at any level and relies on workspace
   trust for process execution.
7. **Trust store and timing.** One `<config.Dir()>/trust.json` for every workspace; the
   config-layer gate is a startup decision, so a folder trusted mid-session applies its project
   config at the next start (`docs/trust.md`).
8. **Not applicable to a cell grid.** Wave animation, background image and blend. Style-only
   options (`editor.animations`, `cursor_jump_animation`, `viewport_edge_fade`,
   `screensaver_*`) load without diagnostics for Fresh config compatibility and are not
   consumed.
9. **Session semantics.** A file launch (`gvim FILE...`) restores and saves no session;
   `--no-restore` still saves at quit because the session file is also the hot-exit store;
   `--simple`'s "no dock" is vacuous. The per-buffer hot-exit store that would lift this is
   designed and deferred (`docs/internal/hot-exit-store.md`).
10. **Settings write path.** `config` is a leaf package that validates and patches the JSONC
    surgically (`PatchBytes`); `internal/app/settingssvc.go` owns the atomic write. The page
    has an explicit layer target (User by default, Project only when Trusted) instead of
    "write to the most specific layer that defines it", and no session layer. Reset removes
    the key (inherit). Settings live-apply runs on the update loop.
11. **Explorer side is session-only**; `terminal.scrollback_lines` is a gvim-only key;
    Ctrl+] is the terminal exit chord and Ctrl+Space toggles live/pager mode in the dock, not
    keymap actions.
12. **Project-wide replace applies to tabs, not files.** Replacements land in editable tabs for
    review and are saved with Save All (Fresh's plugin writes files directly).
13. **LSP workspace edits follow server order and stop at a refused file operation**, reporting a
    partial apply (Fresh continues). Pull diagnostics only fill a gap: a server that publishes
    is never also pulled.
14. **Virtual-space block geometry is rune-based**, not Fresh's byte-clamped rule
    (`docs/internal/virtual-space-scoping.md`).
15. **Markdown page view is a native inline transform**, not Fresh's plugin; the dual view is a
    derived per-pane view of one document, not a buffer group; Contents is a native sidebar
    section with `contents.*` settings in the user layer. The `markdown_source` plugin pins a
    four-space list indent.
16. **Native equivalents of plugins**: the diagnostics sidebar panel, the theme editor and the
    project-search panel are native. The plugin buffer groups are "group-lite": a layout of
    real splits of the current tab. `compare_files` is a second, opt-in Compare surface and
    duplicates the native palette rows when enabled.
17. **Plugin action popups block the caller** until a choice is made; the plugin API level is 2.
18. **Bundled-plugin defaults are memory-bound.** Nine plugins are on by default; the rest are
    opt-in so the default set stays inside the 8 MiB idle-heap gate.
19. **Terminal persistence is per-tab read-only or dead state.** A tab that was live revives with
    a fresh PTY only on the first dive into it (`terminal.restore_live`, default on), never
    automatically; stored rows re-wrap as text on resize; reflow of the live screen is not done
    (the vt emulator has none).
20. **Remote `.gitignore` filtering, polled remote directory watching and the workspace extract
    variants (into an existing workspace, mixed file and terminal tabs, multi-select) are gvim
    extensions**, not Fresh parity items.
21. **Direct launches are refused while a daemon hosts the workspace** (single-writer rule);
    Fresh shares per-file buffers instead.
22. **Remote hot exit stages unsaved text locally** under the session directory (a privacy
    divergence from Fresh's server-side staging).
23. **Remote large files stream client-merged bytes** on save; Fresh uses a server-side patched
    recipe.

## 5. Open gaps

Sizes are not estimated here. Items are grouped by area; each names what is missing, not how it
was built. Where a reference doc has the detail it is named.

### Settings, keymaps, themes, display
- Declared plugin settings are read-only on the Plugins page; the Plugins page has no gallery or
  install UI (`docs/settings.md`). LSP fields are edited as whole-array JSON because
  `PatchBytes` cannot address one array element. No in-page key capture or second keybindings
  writer, no per-field reset-vs-inherit display, no full undo, Reload is a palette row only.
- Overlay keymap contexts: no multi-key bindings, and `completion`, `compositeBuffer` and the
  text-field contexts are not keymap surfaces (`docs/keybindings.md`).
- Explorer: `respect_gitignore`, `custom_ignore_patterns`, `preview_tabs` and the tree glyph keys
  are accepted and ignored; per-segment compact-row clicks and context menus; the side toggle is
  session-only.
- Finder and Live Grep: no workspaces provider; Resume restores controls only; the plain
  palette file preview is plain text and does not scroll (`docs/ui.md`).
- Themes: packaged repository and marketplace, in-session URL fetch and cache refresh, drag-and-drop
  of theme values, background images, user-theme inheritance beyond one level of `extends`,
  provenance for untagged painters (`docs/themes.md`).
- Virtual space: per-buffer block override, selecting virtual lines, wrap/multi-caret support.
- Status bar: `{warnings}` and `{plugin:token}` are not painted.
- Markdown: exact byte anchors for the dual view, native Markdown menu row, wrapped-cell
  selection beyond the primary row.

### Trust
- The spawn gate inspects only the first token of a shell `-c` command (compound, quoted or
  variable-built commands are not parsed); Windows `PATHEXT` and drive-letter case are not
  normalised; an environment injected below the authority, plugin OS and LSP seams is not
  resolved; a running process is not killed when its level drops; the gate covers only the local
  backend, so a remote workspace keeps the coarse app rule (untrusted remote means remote
  commands denied; the decision is recorded per remote target) (`docs/trust.md`).

### LSP
- No built-in universal language server (a user-configured `universal_lsp` works).
- Remote LSP gets one restart attempt per reconnect and has no remote install probe
  (`docs/authority.md`).
- Plugin `DiagnosticInfo` has no `code` field.

### Plugins
- Host API: composite buffers (native substitution only), dock and fullscreen placement,
  independent per-panel views, `windowEmbed` live panes, markdown-in-text, `render_line`, package
  manager, generic HTTP, project-local plugins. The shared daemon runtime has a 40-bit local-id
  cap; editor-scoped overlays are model-local; editor-scoped widget state is rebuilt, not cloned;
  a buffer-bearing mount cannot be editor-scoped; remote workspaces own their runtime.
- Plugin ports (`docs/plugins/porting.md`, `docs/plugins/bundled.md`):
  - `compare_files`: no synthetic group tab, no EOL-only hunks, fillers need the virtual-line
    paint capability, positional pairing fallback, `ui.input` instead of Fresh's picker, no
    `--diff` launch hook or script-facing export.
  - `path_complete`: no `prompt_changed` seam (exported engine plus one command).
  - `lsp_help`: payload carries no language, type or command; 16 install hints.
  - `live_diff`: plugin-side baselines, Myers diff with caps, large files refused.
  - `git_grep`: no prompt toolbar (state is in the picker footer), no preview band, superseded
    queries are discarded by generation rather than cancelled.
  - `git_explorer`: bounded nested-repository discovery; no hover tooltips or arbitrary icons.
  - `env_manager`: no widget panel, devcontainer or authority activation; one local environment;
    set-only overlay.
  - `dashboard`: no third-party section API beyond `register_section`, no dock placement, no
    mouse beyond clickable rows, no sessions section; GitHub, weather and network sections are
    opt-in and bounded.
  - `code_tour`: no dock placement, no per-step buffer modes, one tour at a time.
  - `lsp_navigation`: no initial selected index; `find_references` is command-driven (gvim has no
    reference events); the Finder library has no async search sources, embedded preview or
    utility-dock routing.
  - `diff_nav` (standalone, opt-in) and `git_gutter`'s merged navigation: targets are line-based
    (Fresh sorts by byte position); no real-terminal verification of the Compare Files source.
  - Language helpers `asm_lsp`, `csharp_support`, `clangd_support`, `rust_lsp`, `deno_lsp`,
    `slang_lsp` are reduced opt-in ports (no server-specific notifications, floating panels, or
    `--version` prechecks where Fresh has them); `live_sessions`, `audit_mode`, `devcontainer`,
    `k8s_workspace`, `pkg` are read-only (no attach, stage/discard, lifecycle, install).
  - Not ported: `orchestrator`, the `agent_*` family, the full `welcome_screen` page, and Fresh's
    remaining language helpers.

### Terminal
- Kitty keyboard: the child-side protocol answers presses only (no releases or event types;
  modifier-only, media and F13+ keys are not encoded); the host side requests no kitty flags.
- Live-screen reflow, per-tab splits and report-split `close_on_exit`.
- Live restore keeps a bounded styled screen and history window, cursor and alternate-screen
  flag, not Fresh's backing-file bytes; OSC titles, cwd, clipboard and hyperlinks are not
  replayed; reviving needs an explicit dive; remote launches restore no terminal state.
- Windows ConPTY is implemented and compile-verified but UNTESTED on a real console.
- Agents: no dock rows outside the picker, no other multiplexer or remote scanning, no
  git-worktree creation in Run Agent; same-root sessions share one workspace.

### Daemon and workspaces
- No fork or daemonize (the daemon is foreground), no Windows pipes, no workspace management
  CLI (the workspace frames exist in `internal/daemon/workspace_protocol.go`).
- Shared buffers are for LOCAL daemon workspaces only: sharing starts at `OpenFile` and
  `OpenLoaded`, not at plugin- or API-created tabs; large, virtual and over-8 MiB documents,
  and remote documents, stay independent; mirrors keep their own cursor and view; the Save As
  reservation is in-process only (a dropped plan leaks the claim until the editor exits).
- Extract variants: no PTY hand-over (a moved terminal revives on its first dive), co-tenant
  targets only, no undo or drag target.
- Remote workspaces are excluded from materialized-workspace eviction.

### Remote authority (real hosts UNTESTED)
- Explorer: no drag/move between directories, no undo or history, no whole-subtree transaction
  or cleanup UI, no mode/mtime/hardlink/xattr preservation, no atomic rename (bounded
  copy-and-remove), overwrite is remove-then-copy with no rollback, watch latency and rename
  approximation, Windows paths.
- Finder and search: the finder walks per directory (no server-side walk); the remote preview
  scans a bounded 8 MiB window (unknown total shows plain text, no semantic spans); the tool
  cap and the panel limit apply before candidate filtering; nested-repository inner ignore rules
  are not applied; replace has no cross-file rollback or undo.
- Large files: no host-side patched recipe, whole-file (non-windowed) transport cap, large-file
  syntax highlighting off.
- Plugin `gvim.fs.copy`: only the tree root has the no-overwrite guarantee, descendants use
  per-file atomic replace; metadata and modes are not preserved; a partial tree remains after
  failure; `fs.remove` is unsupported on remote workspaces.
- SSH: passphrases, 2FA and keyboard-interactive secrets are never keyring-stored; the keychain
  backends (macOS `security`, Linux `secret-tool`) are opt-in and tested only with fakes; the
  macOS backend briefly exposes the password in argv; ProxyJump is one hop.
- Env (R13-18): interactive-login capture supports bash and zsh only, other remote shells
  keep the `sh -c` fallback; the captured delta is cached until refresh or reconnect; no
  status-row indicator of the active remote env (Fresh has one) (`docs/authority.md`).
- Hot exit stages locally (section 4 item 22); saved-session conflict detection and restored
  terminal ownership are not implemented.

### Update, i18n, input
- No self-update download/install engine and no update terminal buffer.
- i18n: rows without Fresh keys, structured field and row schema names, and some
  keybinding-help and plugin-manager text stay English.
- Input: GPM console mouse, key-release reporting on legacy terminals, kitty keyboard flags.

### Scripting
- Fresh's external `--cmd` script channel, `open-file --wait`, watch and agent capability.

### Per-buffer hot exit
- A file launch cannot bring the launched files' own unsaved text back; the per-buffer store is
  designed and deferred (`docs/internal/hot-exit-store.md`).

## 6. Scope boundaries and gvim-only additions

**Product definition.** gvim's objective is a fast, keyboard-driven terminal code viewer:
viewing, navigation, search, syntax highlighting, diff and compare review, and view protection
(`-R` / `--view`). Outside that objective, by decision:

1. Plugin package management (git clone, install, update, remove, lockfile, registry sync).
2. Container and cluster lifecycle management (devcontainer and k8s workspace CLI attach).
3. External agent orchestration and interactive process injection (`orchestrator`, `agent_*`).
4. Remote daemon network clustering beyond the SSH jump-box support already present.
5. Editor locales other than English and Simplified Chinese.

**Other front ends.** Fresh's GUI/desktop build, the browser `--web` UI, the WASM build, packaging
matrices and telemetry are different front ends or distribution concerns, not TUI features.
Daemon mode itself is in scope.

**Genuine native equivalences.** A native gvim feature stands in for a Fresh plugin only when it
delivers the user-facing behaviour; its residual gaps stay in section 5. Accepted today: native
Compare for `compare_files`, the native theme editor, the native project-search panel, and the
native Markdown page view and dual view. `dashboard`, `live_grep` and `welcome_screen` are
partial equivalents at best, and `git_gutter`'s merged navigation is not the standalone
`diff_nav` plugin.

**gvim-only additions** (not parity requirements, but the fastest way to verify new work; keep
them working): `gvim config show` and `config paths` (show is trust-gated), `gvim grammar`,
`gvim rex install` / `uninstall`, the `--stdin-name` flag and bounded-snapshot stdin, the hex
and image viewers, read-only / pager mode (`-R`, `--view`), the in-process `internal/uitest`
harness, `cmd/gvimdebug`, the `GVIM_SHOTS` screenshot sweep, the gvim-only config keys
(`update_check_url`, `update_command`, `terminal.agents`, `terminal.scrollback_lines`, `ssh.*`,
`daemon.materialized_workspaces`, `auto_attach`), tmux and transcript session discovery, and the
`gvim.*` i18n keys.
