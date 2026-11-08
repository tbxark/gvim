# gvim package architecture

Status: design record for the package reorganization (`master`, after the layout move).
This file describes the package tree, what each package is for, and the import rules the
compiler and `tools/import_rules_check.sh` enforce. `migration/ARCH-v4.md` is the
earlier proposal-only record and is superseded by this file where they disagree.

The goal of the layout is that a reader (or a coding agent) can tell from the import path
what a package is responsible for, where a new file belongs, and which direction a
dependency may point.

## 1. Layers

```
main.go                         the executable: main → cli → app (root package, so
                                `go install` / `go build .` work without a path)
cmd/gvimdebug/                  developer page: runs gvim in a PTY, serves the screen

internal/                       private executable shell (no top-level package imports it)
  app/                          composition root: Options, Run, buildRuntime
  uiadapter/                    engine → TUI ports: editor/document views, dialogs, files,
                                git status, projects, terminal/clipboard, auto-save gate
  pluginhost/                   the Lua runtime host: real plugin.OS, editor, services, virtual
                                buffers, decorations
  lsphost/                      lsp client → TUI language-server port
  restore/                      session save/restore and crash-recovery policy
  keybindings/                  keymap adapter, user bindings store, plugin binding overlay
  highlight/                    syntax-span cache for the TUI
  apptest/                      test-only shared helpers; never in a production tree
  configview/                   the `gvim config show` / `config paths` backend
  cli/                          cobra command tree, flags, DIR / FILE:LINE:COL
  tui/                          Bubble Tea v2 root model + widgets
    overlay/ list/              shared kit: the Overlay/Caret contract and the list/box widget
    frame/ sidebar/ split/      geometry kit: layout + hit map, section stack,
    tabstrip/                   split tree, tab strip
    dock/                       terminal dock panel (the root keeps the glue)
    browser/ ctxmenu/ dialog/ editorpane/ explorer/ find/ grid/ icons/ menubar/
    palette/ picker/ pluginpanel/ popup/ prompt/ space/ style/
  terminal/                     PTY + screen model behind the terminal dock
  filewatch/                    fsnotify watcher (explorer reloads, project switches)
  uitest/                       test-only in-process UI harness

<top level>                     TUI-free engine, shared libraries and services
  buffer/ document/ editor/ search/ syntax/ fold/ diff/ encoding/ session/
  decorations/ textlayout/ outline/
  atomicfile/ shellcmd/
  workspace/ explorer/ finder/ fuzzy/ fsops/ gitstate/
  config/ keymap/ theme/
  trust/ (workspace trust: levels, decision store, policy, executable-content scan)
  lsp/ (+lsp/manager, lsp/lsptest) pluginapi/ plugin/ (+hostos, bundled, plugintest,
  pattern) commands/
```

Dependency direction:

```
stdlib / third party
      │
      ▼
engine leaves  (buffer, textlayout, diff, encoding, fuzzy, theme, fsops, gitstate, lsp,
                pluginapi, decorations, atomicfile, shellcmd, i18n)
      ▼
document          (per-file model: buffer/search/syntax/textlayout/encoding/diff/atomicfile)
      ▼
engine (editor, search, syntax, session, commands)
      ▼
internal/tui  (depends on engine value types and the plugin contract, never on the shell)
      ▼
internal/{uiadapter,keybindings,lsphost,restore,highlight}   (host adapters)
      ▼
internal/app  (composition root: the only package that imports the host packages)
      ▼
internal/cli → main (root package)
```

## 2. Import rules

`tools/import_rules_check.sh` reads the direct imports of every package
(`go list`) and fails with a rule id and the offending edge. The rules:

| Rule | Statement |
|---|---|
| R1 | No top-level package (not `internal/…`, not `cmd/…`) imports `internal/…`. |
| R2 | `charm.land/*` (Bubble Tea, bubbles, lipgloss) is imported only by `internal/tui/…`, `internal/terminal` and `internal/uitest`. The plain `github.com/charmbracelet/x/*` helper libraries (`ansi`, `term`, `vt`) are not the UI framework and stay free. |
| R3 | Leaf packages import no package of this module: `buffer`, `textlayout`, `diff`, `encoding`, `fuzzy`, `pluginapi`, `decorations`, `theme`, `fsops`, `gitstate`, `lsp`, `lsp/lsptest`, `atomicfile`, `shellcmd`, `config`, `explorer`, `keymap`, `i18n`, `plugin/bundled`, `plugin/pattern`, `outline`. (`lsp/manager` sits above `lsp` and may import it.) |
| R4 | Engine direction. `editor` does not import the UI, app, cli, plugin runtime/contract, lsp, finder, fsops, workspace, keymap, theme, config or commands. `search` does not import `editor`. `syntax` imports only `theme` among module packages. `session` imports only `buffer`. `commands` imports only `fuzzy` and `keymap`. `workspace` imports only `config`. `trust` imports only `config` and `atomicfile`. `finder` imports only `explorer` and `fuzzy`. |
| R5 | The TUI tree does not import `internal/app`, `internal/cli`, the plugin runtime, `lsp`, `finder` or `fsops`; a TUI sub-package does not import its parent `internal/tui`. |
| R6 | `internal/cli` imports only `internal/app`, `internal/configview`, `authority` (the top-level remote-spec parser it classifies targets with) and `syntax` (and cobra). |
| R7 | Only the root main package and `internal/cli` import `internal/app`; only the root main package imports `internal/cli`; only `internal/app`, `internal/pluginhost` and `plugin/plugintest` import the plugin runtime; only `internal/app` and `internal/uiadapter` import TUI sub-packages. |
| R8 | No package in the shipped binary imports test support: `buffer/buffertest`, `plugin/plugintest`, `lsp/lsptest`, `internal/uitest`, `internal/apptest`. |
| R9 | `pluginapi` and `decorations` import no module package and no charm package. |
| R10 | The plugin tree imports only the standard library, `gopher-lua`, `hujson`, `pluginapi`, `plugin/bundled`, `plugin/pattern`, `plugin/hostos` (plugintest may also import the runtime and other plugin sub-packages). No charm. |
| R11 | `editor`, `document`, `buffer` and `decorations` do not import `pluginapi`. |
| R12 | Only `internal/tui` (its root and its own sub-packages), `internal/app` and `internal/uiadapter` import TUI sub-packages. |
| R13 | The private host packages: only `internal/app` imports `uiadapter`, `pluginhost`, `lsphost` and `restore`; `keybindings` may also be imported by `uiadapter`/`pluginhost`/`configview`; `highlight` by `uiadapter`. |
| R14 | `internal/configview` imports only `config`, `internal/keybindings`, `keymap` and `theme` among module packages; it never imports `internal/app`, `internal/cli` or `internal/tui`. |
| R15 | The TUI root contract (`internal/tui`) is imported only by `internal/app`, the host packages (`uiadapter`, `keybindings`, `lsphost`, `restore`) and `internal/uitest`; TUI sub-packages never import the root (R5). |
| R16 | No top-level package's test files import `internal/`; tests follow the same layer rule as the package they test. |

Run the gates:

```sh
go build ./... && CGO_ENABLED=0 go build ./...
go vet ./...
golangci-lint run ./...
go test ./...
bash tools/purity_check.sh        # PURE_GO_OK
bash tools/import_rules_check.sh  # IMPORT_RULES_OK
```

## 3. Package catalogue

### 3.1 Executables

| Package | Role |
|---|---|
| root `main` | `main`: signal context, `cli.Execute(ctx, args, stdout, stderr, app.Run)`, exit code. No logic. Stays at the module root so `go install` works. |
| `cmd/gvimdebug` | Developer page: runs gvim in a PTY, forwards keys and serves the screen over HTTP for scripted screenshots. |

### 3.2 Private shell

| Package | Role | May import |
|---|---|---|
| `internal/cli` | cobra command tree, flags, target classification (DIR vs FILE:LINE:COL through an injected `stat`), exit codes. | `internal/app` (Options/Run), `internal/configview`, `syntax`, cobra, stdlib |
| `internal/configview` | Backs `gvim config show`, `gvim config paths`: renders the effective config as JSON and reports config file locations. | `config`, `internal/keybindings`, `keymap`, `theme` |
| `internal/app` | Composition root: `Options`, `Validate`, `Run`, `buildRuntime`; wires engine + host packages + TUI. | engine packages, host packages, TUI root and sub-packages, plugin runtime |
| `internal/daemon` | The daemon MVP (#18a): the socket protocol and lifecycle, the hosted editor's socket IO, and the attach client that forwards this terminal to a daemon. | `internal/tui` (message types only), charm, stdlib |
| `internal/uiadapter` | Adapts engine to the TUI ports: editor/document views, dialogs, ranker, file ops, git status, projects, terminal/clipboard, auto-save gate. | engine packages, `tui` sub-packages, `highlight`, `keybindings`, `terminal` |
| `internal/pluginhost` | The real `plugin.OS`, the editor as plugins see it, plugin services, virtual buffers and decorations. | engine packages, `plugin`, `pluginapi`, `keybindings` |
| `internal/lsphost` | Language-server port: managers, document sync, diagnostics polling, server catalogue. | `lsp`, `lsp/manager`, engine packages, `pluginapi`, `tui` |
| `internal/restore` | Session save/restore and crash-recovery copies (session file, stash files, recovery dir). | `session`, `editor`, `buffer`, `config`, `tui` |
| `internal/keybindings` | Built-in presets, user bindings and persistence, `keymap.Map` → TUI `Keymap` adapter, plugin binding overlay. | `keymap`, `config`, `commands`, `pluginapi`, `tui` |
| `internal/highlight` | Syntax-span cache for the TUI: windowed lexing, generations, string-context queries. | `buffer`, `editor`, `syntax` |
| `internal/apptest` | Test-only shared helpers (config isolation, temp roots, session editor, big files). | `editor` (and test-only imports elsewhere) |
| `internal/tui` | The Bubble Tea v2 root model: routing, focus, layout, hit map, painting; interfaces in `ports.go`. | engine value types, `pluginapi`, `decorations`, its own sub-packages, charm |
| `internal/tui/*` | Widgets and panels (see 4.2). | kits (`grid`, `space`, `style`, `icons`), engine value types, charm |
| `internal/terminal` | Runs a program in a PTY and keeps a model of its screen (`Screen`, `Cell`, styles, cursor, paste). Backend of the terminal dock. | `creack/pty`, `x/vt`, `ultraviolet`, stdlib |
| `internal/filewatch` | fsnotify watcher with debounce and changed-directory coalescing. | fsnotify, stdlib |
| `internal/uitest` | Test-only in-process Bubble Tea harness (keys, mouse, screen cells, PNG). | charm, stdlib |

### 3.3 Engine and libraries

| Package | Role | Key facts |
|---|---|---|
| `buffer` | The text model: lines of runes, positions, edits, undo/redo, observer, large-file mode. | Two storage modes in one package: in-memory lines, and a disk-backed `bigStore` for large files (`OpenLarge`, `Snapshot`, `IndexScan`, `Rebase`). A split was considered and rejected: the mode is part of `Buffer`, not a layer on it (see `buffer/doc.go`). |
| `document` | One open file: text, cursor/carets, selection, folds, view settings, saved/on-disk/encoding/EOL state, large-file mode, text commands and motions, document-level load/save planning, and the per-document plugin seam data. | The model half of the old `editor` god package; imports `buffer`/`search`/`syntax`/`textlayout`/`fold`/`diff`/`encoding`/`atomicfile`, never `editor`, `session` or `internal/`. |
| `editor` | The editing session: tabs, active document, action registry and `Do` dispatch, statuses, save/revert/autosave jobs, disk polling, shell jobs, settings resolution, marks/jumps, and the plugin seam's session half. | Imports `document`, `atomicfile`, `shellcmd` and the engine leaves; no UI. `editor/bridge.go` still aliases the moved names for consumers. |
| `search` | Find, scan, replace and range search over `buffer` lines. | `visible.go` is the visible-window search (renamed from a misleading name). |
| `syntax` | Chroma lexer resolution, token spans for a visible window, markdown inline styling, style table from a theme. | |
| `fold` | Indentation fold ranges. | Small leaf used by `editor`. |
| `diff` | Line diff (go-diff) into hunks; used by the compare view and plugin services. | |
| `encoding` | Decoding/encoding detection and conversion (UTF-8/16, charsets via `x/text`). | |
| `session` | Session file format and stash files (open tabs, cursors, unsaved overlays). | The restore *policy* lives in `internal/restore`. Session and search JSON go through `atomicfile`; the overlay-stash payload is `document.StashOverlay`. |
| `decorations` | Plugin decoration store: namespaced items per buffer, anchors that follow edits, per-line query. | Pure data, stdlib only. |
| `authority` | The workspace backend slot: four routing surfaces (file I/O, one-shot process, long-running stdio spawn, terminal wrapper), the local implementation over the plugin OS port, Fresh-compatible scp/ssh:// target parsing with path translation, and the first SSH backend (pure-Go x/crypto/ssh) for terminal and one-shot exec. | TUI-free, no internal imports; remote file I/O is the SFTP/remote-helper follow-up; the app/TUI wiring lands after the daemon work (docs/authority.md). |
| `textlayout` | Terminal text geometry: indent width, indent guides, word occurrences, match columns. | Leaf; used by `editor`/`fold` and the editor pane. |
| `outline` | Markdown heading scan for the sidebar's Contents section: ATX and setext headings, fenced code skipped, bounded to 1 MiB. | Leaf, TUI-free; the widget and the model half are in `internal/tui/sidebar` and `internal/tui/markdown_toc.go` (docs/sidebar.md). |
| `atomicfile` | Atomic file replacement: temp+sync+rename, mode/owner/xattr, symlink following. | Leaf; used by `editor` (saves) and `session` (session and search JSON). |
| `shellcmd` | Runs one external program in a directory with a timeout; the exec half of `editor/shell.go`. | Leaf; `editor` wraps it for shell-command actions. |
| `workspace` | Project root switching and the recent-folders list. | |
| `explorer` | File-tree model, `ListDir` with options, gitignore matching, async reload. | |
| `finder` | Project file index (`git ls-files`/walk) and usage ranking. | |
| `fuzzy` | fzf-style scorer shared by the finder, the palette and the pickers. | |
| `fsops` | Create, mkdir, rename, move, copy, trash with conflict policy. | |
| `gitstate` | Git work-tree root, branch and per-file status with go-git. | |
| `config` | Layered JSONC configuration (defaults, user, project), languages, formatters, plugins, and the top-level `locale`. | `hujson`. The project layer is skipped when `Options.Untrusted` is set (workspace trust; see docs/trust.md); a project `locale` is refused (a user preference). |
| `i18n` | Translation layer: embedded English catalogue, `T`/`TN` with `%{name}` interpolation, CLDR plurals, locale resolution (`auto` from the environment or a configured code) and the plugin runtime's locale. | Leaf, stdlib only; embedded locale JSON in `i18n/locales/` (English source of truth, Fresh import); see `docs/i18n.md`. |
| `trust` | Workspace trust: the three levels and the undecided state, the per-user decision store, Fresh's open-time policy and executable-content marker scan. | TUI-free; store at `config.Dir()/trust.json` through `atomicfile`; the app gates the config project layer on it (docs/trust.md). |
| `keymap` | Embedded presets, chord parsing and canonicalization, resolve, user overrides. | Data files `keymap/*.json`. |
| `theme` | Theme data (colors, syntax keys) from embedded JSON. | Data files `theme/data/*.json`. |
| `lsp` | Pure-Go LSP client/protocol layer: JSON-RPC, transport, positions, requests, diagnostics, signature help. | |
| `lsp/manager` | The multi-server manager: one client per workspace root, server catalogue (`Spec`, `Defaults`, `DefaultFor`). | imports `lsp`. |
| `lsp/lsptest` | Test-only fake language server and pipes shared by the client and manager tests. | imports no module package. |
| `pluginapi` | The plugin contract: plain types (`api.go`, the tier-1a `widgetapi.go` spec types, action popups, prompt suggestions, the all-files diagnostics shape), `Host`/`Runtime` interfaces, error codes, `Unsupported`. | stdlib only; imported by `internal/tui`, `internal/app` and `plugin`. |
| `plugin` | Lua plugin runtime: sandbox, tasks and the yield protocol, budgets/strikes, `gvim.*` API installers, data libraries, loader/lifecycle. `doc.go` holds the file map. | `plugin/bundled` (embedded Lua), `plugin/plugintest` (fake host harness), `plugin/pattern` (Lua pattern matcher), `plugin/hostos` (OS port). |
| `plugin/hostos` | The plugin OS port: files, processes and watchers (`OS`, `Dirs`, `Proc*`, `Watch*`, `NopOS`). | Implemented by `internal/pluginhost`, which is why it is a separate package from the runtime. |
| `commands` | The command registry and palette model: generated Fresh command table, palette query modes, availability rules, filtering. | Feeds the command palette; keymap-aware. |

## 4. Inside the big packages

The layout moved code between packages. The following maps cover the packages that are
still large internally, so a reader can find a feature without grepping by memory.

### 4.1 `internal/tui` root (file clusters)

| Cluster | Files | Responsibility |
|---|---|---|
| core / dispatch | `model.go`, `root.go`, `ports.go`, `doc.go`, `dialogs.go`, `key_hints.go`, `keybind_known.go`, `chord_keys.go`, `overlay_keys.go`, notices, `status_expiry.go`, `cont_*` | `Model` + `Deps`, `New/Run/Close/Init/Update/View`, update pipeline, key translation/dispatch, overlay-context keymap routing, overlay stack, painting, ports |
| splits / tabs / compare | `split.go`, `split_resize.go`, `split_drag.go`, `split_tabs.go`, `tabs.go`, `tab_drag.go`, `tab_drop.go`, `compare*.go` | pane controllers and drag/drop over the `tui/split` tree, tab strip glue over `tui/tabstrip`, side-by-side compare |
| terminal dock | `dock_glue.go`, `dock_commands.go`, `dock_text.go` (`shell.go` is editor shell commands) | root glue over `tui/dock`: port aliases, action routing, paint calls, selection helpers |
| LSP UI | `lsp.go`, `lsp_detect.go`, `diag.go`, `root_signature.go` | language-server UI controller, diagnostics, goto/hover/completion/code actions/rename, signature help |
| plugin host | `plugin_*.go`, plus the plugin host files `plugin_panels.go` (widget panels and sidebar sections), `plugin_menus.go` (menu bar rows), `plugin_actionpopup.go` (action popups) and `plugin_themes.go` (theme-file APIs), `plugin_groups.go` (buffer groups and scroll sync), `plugin_folds.go` (plugin folding ranges), `plugin_seams.go` (split labels and window state) | the UI side of the plugin contract: pump, events, keys/modes, prompts, windows, virtual buffers, decorations, manager, init.lua, the declarative panels/sections, menus, action popups and prompt suggestions |
| overlays / utilities | `keybind_editor.go`, `help_utility.go`, `notice_warnings.go`, `help.go`, `settings.go`, `icon_preview.go`, `tour.go`, `root_stash.go`, `language_pick.go`, `files_encoding.go`, `keymapeditor.go`, `keybind_ports.go` | keybinding editor, help/settings/tour/notices/pickers; the list/box machinery lives in `tui/list` |
| frame glue | `frame_alias.go`, `welcome.go`, `footer.go` | aliases into the `tui/frame` geometry kit, welcome screen, footer hints |
| view / display | `view.go`, `view_render.go` | display settings and per-document view settings |
| find glue | `find_route.go`, `find_highlight.go`, `find_mouse.go` | controller between the find bar and the editor |
| save / files / watch | `root_revert_fix.go`, `save.go`, `watch_reload.go`, `autosave.go`, `files_revert.go`, `files_recovery.go`, `files_overwrite.go` | save/save-as planning, auto-save tick, disk polling, reload, recovery copies |
| root features | `macro.go`, `root_dump.go`, `root_ports.go`, `root_commands.go`, `root_project.go`, `root_watch.go`, `root_fix.go`, `root_sidebar.go`, `root_bookmarks.go`, `git.go`, `live.go`, `root_*_fix.go` | project switch/recents, watcher glue, git marks, macros, bookmarks, live grep, sidebar focus cycle, root ports |
| sidebar / Contents | `markdown_toc.go`, `root_sidebar.go` | the Contents section's buffer lifecycle, settings, follow, keys and mouse; the sidebar focus cycle |
| editor body | `motion_mouse.go`, `motion_rows.go`, `viewrow.go`, `occurrence.go` | click/drag/wheel, row-mapped motion, top-row memo, occurrence highlight |
| large-file mode | `large.go` | large-file UI: index progress, full-load prompt, streamed save/search, error dialogs |
| clipboard / popup | `clip.go`, `context_menu.go`, `popup.go` | OSC 52/HTML clipboard flows, right-click menus, popup adapter |

**Overlay key routing.** `overlay_keys.go` holds the root's side of the overlay keymap contexts.
An overlay that takes its keys in code declares one of the Fresh contexts the root resolves
(`prompt`, `searchPrompt`, `popup`, `menu`, `settings`, `dock`) through the root's type switch;
before the overlay's own handler, `routeOverlay` looks the key up in that context, runs the
surface's operation or the model's normal dispatch for the bound action, and falls through to the
built-in handler when the key is unbound. The menu bar and the dock hook into the same router from
`routeMenu` and `dockKey`; plugin panels and the custom settings/theme dialogs are outside it
(the plugin host owns its key layers). Text-field operations are forwarded as the widget's own
keys, so the built-in behaviour is preserved and only the chord moves.

### 4.2 `internal/tui` sub-packages

`overlay` (the Overlay/Caret contract every overlay implements), `list` (the list/box
overlay kit: `View`, `TextBox`, centring and scroll helpers), `frame` (the single geometry
source: `ComputeLayout`, hit map, tab-strip and welcome placement), `sidebar` (the explorer
column's section stack: `Layout`, header rows, the Contents widget; docs/sidebar.md), `split` (the split tree
and pure pane geometry), `tabstrip` (strip painting and pointer mapping), `grid` (cell
surface on `x/ansi`), `space` (padding, text fitting and `Rect`), `style` (theme → lipgloss
styles; the only place that happens), `icons` (Nerd Font glyphs), `editorpane`
(paints the text area; pure renderer, no tea), `menubar` (menu bar + dropdowns; generated
menu table), `explorer` (file-tree panel), `palette` (command palette overlay), `find`
(find bar), `prompt` (bottom-row prompts), `browser` (file/folder browser), `picker`
(plugin list picker), `popup` (anchored list popup), `ctxmenu` (right-click menu),
`dialog` (modal dialogs), `dock` (the terminal dock panel: PTY lifecycle, input/paste,
resize/exit, capture, paint), `pluginpanel` (the declarative widget panel of the plugin
API: layout, paint, hit-testing, focus and key/mouse state, the interactive widget kinds and
their host-owned instance state and page mode; the root owns the placements and routes input;
docs/plugins/api-ui.md).

### 4.3 `document` and `editor`

`document` is the model; `editor` is the session around it. The `Document` type, its
fields and every method on it live in `document`; `editor` holds `Editor`, the action
registry and the job/polling orchestration. A temporary `editor/bridge.go` aliases the
moved names (`Document`, `Caret`, `ViewSettings`, ...) so the rest of the module keeps
compiling until consumers are retargeted.

`document` clusters:

| Cluster | Files (examples) | Responsibility |
|---|---|---|
| document core | `document.go`, `docid.go`, `changes.go`, `diskstate.go`, `settings.go`, `accessors.go` | `Document` struct and state, identity/generation, change ring, disk stamps, the options it reads |
| cursor & editing | `cursors*.go`, `edit.go`, `edit_api.go`, `block.go`, `smart*.go`, `indent*.go`, `indent_rules.go`, `case.go`, `comment.go`, `bracket.go`, `textcmd.go`, `textops.go`, `vi_actions.go`, `dabbrev.go`, `scanbudget.go` | carets and selections, edit primitives, typing/auto-close/auto-indent, line/selection commands, case/comment/bracket, vi, dabbrev |
| motions & clusters | `motion*.go`, `fold.go`, `viewsettings.go` | grapheme/word/line movement helpers and row mapping inputs, folds, per-document view settings |
| files & state | `files_*.go`, `encoding.go`, `binary.go`, `paths.go`, `readonly.go`, `pending.go`, `large*.go`, `save_large.go`, `autosave_async.go`, `stash.go` | load/decode, encoding/EOL, disk state, large mode and its index, save planning and jobs, overlay stash format |
| plugin data | `virtual.go`, `docops_id.go`, `lspedit.go`, `save_info.go` | virtual-document state, id-based ops data, LSP edit value, save result payload |

`editor` clusters:

| Cluster | Files (examples) | Responsibility |
|---|---|---|
| session shell | `editor.go`, `handlers.go`, `doc.go`, `tabs*.go`, `settings.go` | `Editor`, action registry, tabs and active document, statuses, settings resolution |
| jobs & polling | `save_plan.go`, `autosave.go`, `files_diskcheck.go`, `doc_revert.go`, `doc_index.go`, `load.go`, `interrupted.go`, `paths.go` | save/revert/index jobs, disk polling, load functions, crash-interruption queries, save-as/rename |
| commands | `findsel.go`, `goto.go`, `marks.go`, `nav*.go`, `mouse_ops.go`, `view_ops.go`, `motion_carets.go`, `motion_wrap.go`, `replace.go`, `doc_format.go`, `shell.go`, `doc_search.go`, `doc_path.go` | action handlers bound to keys: find, goto, marks/jumps, viewport, replace, format, shell, search range, path copy |
| plugin seam | `lspedit.go`, `virtual.go`, `docops_id.go`, `changes.go`, `docid.go`, `edit_api.go` | the session side of the plugin contract: doc lookup, listeners, virtual documents, LSP edit application |
| state guards | `readonly.go`, `pending.go`, `large_stash.go`, `large.go` | read-only guards, pending-index gates, stash restore/replace, large-path policy |

### 4.4 `buffer`

One package with two storage modes: an in-memory model (`buffer.go`, `history.go`,
`snapshot.go`, edits and observers) and a disk-backed large mode (`lines_tree.go` and the
`large_*.go` files: source, index, pages, overlay, writes, rebase, budget). `Buffer.big`
selects the mode; every public method handles both. The split into two packages was
considered and rejected (see `migration/ARCH-v4.md` history and `buffer/doc.go`).

### 4.5 `plugin`

| Cluster | Files (examples) | Responsibility |
|---|---|---|
| runtime kernel | `runtime.go`, `plugin.go`, `task.go`, `sandbox.go`, `slice.go`, `handles.go`, `convert.go`, `limits.go`, `log.go`, `hostcall.go`, `ioruntime.go`, `require.go`, `timerpace.go`, `errfmt.go`, `bindings.go`, `lint_bindings.go` | `Runtime`/plugin state, Lua entry and yield protocol, tasks, budgets/strikes, heap guard, handle registry, Go↔Lua conversion, sandbox, `require` |
| `gvim.*` API installers | `api_*.go` (one per area), `bindings.go`, `api_modules.go` | the Lua surface; `api_modules.go` lists the installers in order. Tier 1a adds `api_panel.go` (gvim.ui widget panels and sidebar sections), `api_i18n.go`, `api_menu.go`, the action popup and prompt suggestions in `api_ui.go`, the theme override in `data_time.go`, and the all-files diagnostics in `api_lsp.go` |
| data libraries | `data_*.go`, `stdlib*.go` | `gvim.json`, `gvim.re`, `gvim.settings`, `gvim.text`, `gvim.diff`, `gvim.time`, `gvim.utf8`, and the repaired Lua 5.1 stdlib |
| loader | `discover.go`, `manifest.go`, `order.go`, `lifecycle.go`, `control.go` | discovery, `plugin.json`, dependency order, enable/disable/reload, `plugin-state.json` |
| OS port | `os.go`, `nopos.go` | file/process/watch interface used by `gvim.fs` and `gvim.proc`; the real implementation is in `internal/app` |

The `gvim.*` installers intentionally stay in the runtime package: they call 120 kernel
symbols directly, so a separate package would only fake a boundary. The OS port is the
exception: it is an interface the shell implements, so it lives in `plugin/hostos`.

## 5. Naming decisions

Renamed during the reorganization (no compatibility constraints were kept):

| Old | New | Why |
|---|---|---|
| `internal/ui` (package `ui`) | `internal/tui` (package `tui`) | It is the terminal UI root model of an executable, and "ui" did not distinguish the widget sub-packages from the root. |
| `internal/term` | `internal/terminal` | The package runs a PTY and models a screen; "terminal" says so. |
| `internal/gitx` | `gitstate` | "x" suffix said nothing; the package reports git work-tree state. |
| `internal/display` | `textlayout` | Not a display abstraction: terminal text geometry (indent, guides, columns). |
| `internal/decor` | `decorations` | Unabbreviated; the store's items are called decorations everywhere. |
| `internal/palette` | `commands` | It is the command registry that feeds the palette; it collided by name with `internal/tui/palette`, the overlay. |
| `internal/explorer/watch` | `internal/filewatch` | It is a generic fsnotify watcher used by `app` and the TUI, not by `explorer`. |
| `internal/luapat` | `plugin/pattern` | It is the Lua pattern matcher, used only by the plugin runtime. |
| root `main.go` | kept at the module root | Yes, it is the idiomatic `cmd/` layout to move it, but the module root main keeps `go install github.com/tbxark/gvim@latest` / `go build .` working without a path; only auxiliary commands (gvimdebug) live under `cmd/`. |
| engine packages under `internal/` | top level | The engine is TUI-free and is the stable core: `buffer`, `editor`, `search`, `syntax`, `fold`, `diff`, `encoding`, `session`, `decorations`, `textlayout`, `workspace`, `explorer`, `finder`, `fuzzy`, `fsops`, `gitstate`, `config`, `keymap`, `theme`, `lsp`, `pluginapi`, `plugin`, `commands`. The executable shell stays private. |

Rejected renames: `buffer` → `text` (2,028 `buffer.Pos` references, no functional gain);
`internal/app` (the conventional name for a composition root is good).

## 6. Big-package status and work ahead

The reorganization is staged; each step keeps `go build ./... && go test ./...` green.

Done:

1. Top-of-tree layout move and package renames (§5).
2. `main` stays at the module root (easy `go install` / `go build .`); only auxiliary
   commands live under `cmd/`.
3. Import gate rewritten for the new tree (R1-R14) and generator paths updated.
4. `internal/app` split into host packages (`uiadapter`, `pluginhost`, `lsphost`,
   `restore`, `keybindings`, `highlight`, plus test-only `apptest`); `app` is now six
   files (Options/Run/buildRuntime, settings, logfile, live grep, formatters), and the
   `gvim config` backend moved to `internal/configview` with `syntax.GrammarNames`.
5. TUI kits: `overlay` (contract + Caret), `list` (list/box kit), `space` (`Rect` and text
   fitting), removing ten forwarding adapters (~36 methods) and the duplicated fit helpers;
   then `split` (tree + geometry), `frame` (layout + hit map) and `tabstrip` (strip paint),
   taking the root from ~23.9k to ~21.7k source lines with all tests green.
6. Engine leaves: `atomicfile`, `shellcmd`, the `buffer` two-mode doc, and file renames.
7. `editor` split into top-level `document/` (per-file model, 61 files/7.0k LOC) and a
   slimmed `editor/` (session, 65 files/6.6k LOC), with a temporary alias bridge so no
   consumer package changed; the import gate gained the `document` direction (R4/R11).
8. Terminal dock extracted to `internal/tui/dock` (panel + tests), root keeps the glue;
   the zero-importer `internal/tui/statusbar` package (status row primitives duplicated by
   `tui/frame.StatusView`) was deleted.

Next (tracked as follow-up work; each keeps build + tests green):

1. Remaining TUI extraction. The package moves for the keybinding editor
   (`tui/keybindeditor`) and the terminal dock (`tui/dock`) are mechanical on top of the
   kits, but their tests are model-driven (`keybind_editor_test.go` 743 LOC,
   `dock_test.go` 1,090 LOC plus four dock tests) and need a dedicated harness rewrite
   against the new types; do that in one focused pass. The plugin host and LSP UI remain
   last.
3. `editor` → `document` + `editor` split (the two god objects), with an alias bridge.
4. `lsp` split into client + `lsp/manager` + `lsp/lsptest`; `plugin/hostos` extracted from
   the runtime, and `plugin/doc.go` now holds the file map.
5. Finish the editor split tail: move the `editor` model tests into `document`, then delete
   the alias bridge (`editor/bridge.go` + `document/export.go`, ~413 lines) by retargeting
   the ~165 consumer references; tighten `document`'s exported surface to accessors.
6. Fold `fold` into `textlayout`; optional `plugin/luastdlib` extraction; optional keybinding
   editor extraction (`tui/keybindeditor`, needs its test harness rewritten).

The analysis these steps build on (file clusters, symbol-level boundaries, risk
ordering) is in the session's analysis notes; the durable conclusions are kept in this
file when each step lands.

## 7. What enforces the layout

- The compiler: import cycles and illegal `internal/` imports fail the build.
- `tools/import_rules_check.sh`: R1-R16, run in every gate.
- `tools/purity_check.sh`: the shipped binary stays pure Go (no cgo).
- `go vet ./...`, `golangci-lint run ./...`, `go test ./...`.
- Generated code stays generated: `commands/commands_gen.go`
  (`python3 commands/gen/generate.py refs/fresh`) and
  `internal/tui/menubar/menu_table_gen.go` (`python3 tools/gen_menus.py --check`).

## 8. Daemon mode (#18a MVP)

Fresh can host its editor in a daemon and attach terminals to it; gvim's first
slice (round 12) implements the lifecycle and the client transport, not yet the
workspace model (#19).

- **Launch.** `gvim --daemon [NAME]` runs the ordinary editor runtime in the
  foreground (the MVP does not fork) and listens on a Unix socket under the user
  cache directory, mode 0600 in a 0700 directory. The socket name defaults to a
  per-root name (`slug(basename)+8-hex hash(root)`); `NAME` overrides it. A
  live daemon for the same name refuses a second listener; a stale socket file is
  unlinked. While a daemon hosts a workspace, a bare launch in it refuses instead
  of opening a second editor of the same session; file launches, `--simple`,
  `--diff` and stdin launches stay direct launches.
- **Attach.** `gvim -a [NAME]` (or `--attach`) dials the socket, sends a hello
  with the terminal size (and any named files as an open-file frame), puts the
  terminal in raw mode, and forwards input bytes, output frames, resize signals
  and SIGWINCH. A detached/exit frame restores the terminal. Only one client is
  attached at a time: a second attach gets a notice and is dropped, never a silent
  takeover.
- **Hosting.** The daemon runs the normal Bubble Tea program through `tui.RunWithIO`
  with the server as its input and output: input frames block the program's reader
  while no client is attached (the editor stays alive), output frames are written
  only to the attached client (discarded otherwise), and an attach or resize sends
  a window-size message so the frame is repainted at the client's size. The hosted
  editor restores the session on daemon start and saves it on daemon exit; a
  detach saves nothing and does not stop any process.
- **Detach.** The hosted editor lists one extra palette row, "Detach from Daemon"
  (only under `--daemon`). It sends the detached frame to the client and drops
  the connection; the editor keeps running and the next attach repaints it.
- **Protocol.** Length-prefixed frames: a 4-byte big-endian length (at most 1 MiB),
  a type byte and the payload. Client frames: hello (JSON cols/rows/args), input
  (raw terminal bytes), resize (JSON cols/rows), detach, open-file (JSON paths),
  ping. Server frames: output (raw ANSI), notice (text), detached, pong, exit.
  Unknown or malformed frames drop only that client.
- **Plan.** The rest of Fresh's daemon/workspace model is the #19a slice: a
  workspace registry of per-root files (stable id, label, layout), lazy
  materialization with inert placeholders, protocol workspace-list/select/new/
  close/rename, a dock/session switcher and per-workspace save/restore with daemon
  exit ordering. Today the daemon hosts one workspace, the launch directory.
