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
  trust/ (workspace trust: levels, decision store, policy, executable-content scan, the spawn gate DecideSpawn)
  updatecheck/ (the release check behind the status row element {update})
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
| `internal/agentdiscover` | Read-only discovery of terminal sessions that already exist outside gvim: the tmux socket walk, bounded `list-sessions`/`list-panes` parsing, agent pane name matches and qualified attach argv. TUI-free; the runner and an `AllowCommands` trust gate are injected. | stdlib only; a scan never starts or attaches a session; see `docs/terminal.md` Agent discovery |
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
| `trust` | Workspace trust: the three levels and the undecided state, the per-user decision store, Fresh's open-time policy, executable-content marker scan and the spawn gate (`DecideSpawn`, docs/trust.md §3.1). | TUI-free; store at `config.Dir()/trust.json` through `atomicfile`; the app gates the config project layer on the level and wires the same live level into the authority, plugin OS and LSP manager spawn paths. |
| `updatecheck` | The release check behind the status element `{update}`: feed fetch with a transport policy (https, or http on loopback), version comparison (semver-ish, prereleases), a 64 KiB body/8 s bound and a daily debounce stamp. | Leaf, stdlib only; the app runs it in a `tea.Cmd` at startup and the frame paints `Update: vX.Y.Z`; gvim has no self-update (docs/settings.md). |
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
  unlinked. While a daemon (the per-root default or any explicitly named one) hosts a
  workspace, EVERY direct editor launch in it refuses — bare, FILE…, `--diff`, `--simple` and
  stdin — because the daemon's unsaved buffers and a direct editor's writes to the same
  files would race both ways (the direct editor writes the file under the daemon's
  buffer, then the daemon's save overwrites the direct edits). The error names the daemon
  and the way out: attach with `gvim -a` (then the files, e.g. `gvim -a FILE...`), or
  `gvim --attach=NAME` (`-a=NAME` also works) for a named daemon, or quit the daemon first.
  The optional NAME is only accepted in the = form: `gvim -a NAME` would make NAME a file
  argument, not attach to that daemon. A named daemon is
  discovered through a per-root host mark (`daemon_hosts.go`: one JSON file per
  daemon name under the daemon directory, written before serving and removed only after
  the checked exit save; stale marks are ignored and removed on read), with the default
  socket probe as the fallback. A failed host mark write refuses the daemon start
  (listener closed and socket removed): without the mark a named daemon would be
  invisible to direct launches, which is exactly the race this rule prevents. Fresh can keep `fresh FILE` a direct launch because it
  enforces one buffer per file across editors (a second editor gets a read-only buffer);
  gvim has no cross-process per-file ownership yet, so refusing the direct launch is a
  deliberate, documented divergence until that Fresh-style open-file/read-only
  ownership surface exists. The refusal keys on the launch root: a direct launch from a
  different root that opens a file the daemon's workspace holds is not covered until
  per-file ownership exists (a named daemon hosting another root is unrelated by design).
  Tests cover the default and named daemon, the no-lost-work
  case (refused launch leaves the daemon's unsaved buffer and the file untouched), and
  the host mark lifecycle.
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

### 8.1 Workspace registry (#19a, landed round 13)

The registry is the workspace store on top of the round-12 daemon ownership model. It
mirrors Fresh's workspace cache: one durable file per workspace, no central list.

- **Location and name.** `<user data dir>/gvim/workspaces/<encoded-canonical-root>.<stable-id>.json`
  (`GVIM_WORKSPACE_DIR` overrides the directory for tests). The canonical root is
  absolute, cleaned and symlink-resolved, so `./x`, `x/` and a symlinked spelling are one
  workspace; one workspace exists per canonical root. The encoded root is one percent-encoded
  file-name component (a deterministic sha256 name when it would exceed one filename); the
  `root` recorded inside the file stays authoritative.
- **Schema v1** (`daemon.WorkspaceFile`): `version`, `stable_id` (minted once with crypto/rand
  at creation, adopted from the file on restore, used as the per-workspace session directory
  name), `label` (defaults to the root basename and survives renames), `root`,
  `last_focused_at` (unix milliseconds) and `layout` (the existing `session.Session` layout;
  nil until captured). The per-workspace session directory `workspaces/sessions/<stable-id>/`
  holds the stashes and the terminal record (`terminal.SavedState`, its own versioned schema,
  owned by the terminal/dock layer: saved and loaded with `restore.SaveTerminal`/
  `restore.LoadTerminal`). The workspace file never embeds the terminal record, and old
  workspace files load unchanged; `authority_spec` is a future additive field.
- **Registry API** (`internal/daemon/workspaces.go`): `WorkspaceDir`, `OpenRegistry(dir)`,
  `CanonicalRoot`, `List` (deduped per canonical root, freshest `last_focused_at` first; a
  stable-id-bearing file wins a tie, then the lexicographically first path), `Get`, `Create`
  (returns the existing workspace for a known root), `Save`, `Rename`, `Touch`,
  `UpdateLayout`, `Remove` (deletes only the registry file — session data is kept),
  `RemoveSessionData` (explicit discard) and `SessionDir` (creates the per-workspace session
  directory). `Workspace.Materialized`/`Active` are runtime flags, never persisted.
- **Durability.** Saves are atomic: temp file in the same directory (0600), write, fsync,
  close, rename over the target, then a directory fsync; a failed save leaves the previous
  file intact and no temp file behind. A file whose `version` is newer than the binary is
  refused on read and on overwrite (`ErrWorkspaceVersionTooNew`), skipped by scans with a
  diagnostic and never modified or deleted; corrupt files are likewise skipped, not deleted.
  The registry tests cover CRUD, stable-id durability across reopen, canonical-root and
  symlink dedup, freshest-wins arbitration, future-version refusal/no-overwrite, corrupt-file
  skipping, atomicity/no temp leftovers, rename/touch/layout persistence, close keeping
  session data, safe encoded names and the directory override.
- **Workspace protocol (wire + server dispatch + app handlers, landed).** The client
  frames `workspace_list`, `workspace_select`, `workspace_new`, `workspace_close` and
  `workspace_rename` carry JSON payloads; the server answers `workspaces` (the list with
  the active id and each entry's id/label/root/materialized/active) or a notice. The server
  exposes them through `Server.OnWorkspaceRequest`; a daemon without a workspace layer answers
  with a notice, a malformed request is a notice too, and the connection survives either.
  `Hello.Workspace` names the workspace to attach to (empty means the current
  foreground one; the round-12 attach path still ignores it). `Registry.Discover(root)` is
  the daemon-start view: the launch root is the foreground workspace (created/reused) and
  every other registry entry is an inert descriptor. The app handlers drive the same
  manager as the TUI port (list/select/new/close/rename against live runtimes; see the
  switching lifecycle below). Wire types, validation, frame round trips, the server
  dispatch and the end-to-end protocol are tested.
- **Switching lifecycle (landed).** One tea program runs a `tui.WorkspaceHost` that swaps the active
  workspace model; one `workspaceManager` (internal/app/workspace_host.go) owns the registry
  and the materialized runtimes. The daemon start materializes the launch root's workspace
  eagerly and returns the rest as inert descriptors. A switch captures the current
  workspace and builds a dormant target OFF the UI goroutine (a tea command over a
  `buildRuntime` in a worker, with a request sequence and a cancellable context: a newer
  switch cancels the previous build and a stale result is closed instead of landing; build
  failures leave the editor on the current workspace and show a notice). Warm runtimes are
  kept (LSP, plugins, dock shells); switching back is a model swap and does not re-run the
  model's Init. The TUI exposes Workspace: Switch/New/Close/Rename/Next (daemon only, added
  to the model's command table at materialization; the static command table is unchanged),
  a switcher list, root-then-label prompts, and a hand-written `WS <label>` status item.
  The workspace protocol frames drive the same manager (workspace_list/select/new/close/
  rename answer with the list and the active id; errors are notices). An open-file request
  carries the client's cwd and either the legacy bare `paths` or the positional `files`
  (path + 1-based line/column): relative paths resolve against the client's cwd, and the
  position reaches the editor. A path open in a materialized workspace switches to that
  workspace; a path open in a *dormant* workspace is detected from its saved layout, then
  that workspace is materialized and activated asynchronously with the file focused (a
  saved position is kept when the request has none, a stale layout entry whose file is
  gone and has no stash is ignored, and stash text restores the file). A detected owner
  that fails to materialize is an error with the current workspace kept - it is never
  re-opened in another workspace (single-writer v1; shared buffers are #19b). A path open
  nowhere opens in the active workspace. Discovery Import uses the additive
  `WorkspaceLauncher.CreateWithLaunch(root, label, launch)` port: the manager creates or
  reuses the workspace, remembers the discovered session's attach argv keyed by stable id,
  and starts it in that workspace's dock once materialization + Show install the model
  (through the host's command queue, never from a goroutine touching the editor). A
  repeat import of the same root starts its own attach tab in the one workspace of that
  root: the workspace layout/files are shared, the selected session survives as a terminal
  tab (the model asks the dock to find a tab by the launch's Source and focuses it instead
  of duplicating), because registry v1 has no per-session workspace identity or
  initial-launch record yet.
- **Remote file finder (landed).** A remote launch's quick-open file mode enumerates the
  remote workspace through `authority.Files().ReadDir` (streaming into the ordinary
  `finder.Index`, so the palette's poll-while-loading and query-sequence staleness guards
  are shared); the local file index is never started and a local project switch is refused
  (it would enumerate the local disk). The walk is bounded (50k files, 20k directories,
  depth 64), skips the walker's skip dirs/hidden entries, rejects unsafe remote entry names
  (separators, `..`, NUL) and never traverses symlinks; ctx cancellation ends the index
  with `context.Canceled`. A root read failure is the index error and is shown as a
  `File search failed: ...` row; a truncated list shows an incomplete-list row instead of
  posing as the whole project; subdirectory failures keep the rest (partial list). A `~`
  workspace root is expanded remotely (one bounded shell command) so the palette root and
  the walk use the remote absolute path; a failed expansion leaves the root unchanged and
  surfaces the enumeration error, never a local fallback. Remote rows join with slashes and
  the local file preview is disabled (nil preview) for a remote file mode. Gaps: remote
  git `ls-files` (Fresh prefers git through the backend spawner) is not wired, remote file
  preview and Live Grep-over-remote are follow-ups (Live Grep is refused with a notice in a
  remote workspace instead of silently searching the local project, the palette file
  preview is cleared, and the local git-status adapter is replaced by an inert one: local
  git must not look up remote paths), and the remote workspace's plugin/agent project root
  still comes from the local process root. Project Search and Git Grep now run on the
  remote host through the same authority one-shot surface (the spawn/trust gate applies),
  in the session's remote captured environment delta (`env.Remote`; the local overlay is
  never forwarded, and the same delta drives the remote terminal, LSP and git grep):
  the search probes the host for rg then grep (a missing tool is a clear "no search tool
  on the remote host" notice, never a local fallback), runs with the remote workspace cwd
  without a shell, streams whole-file batches (the panel's cancel channel cancels the
  remote command and the stream closes without a Done batch), caps matches and flags
  truncation, and validates the query locally before any remote run. Git Grep resolves the
  repository remotely (git rev-parse --show-toplevel), then runs git grep -n --column in
  the remote repo root and returns absolute remote paths, so a hit opens through the
  remote document access; the local git state is never consulted. Project Replace stays
  refused with a remote-specific notice: a safe remote replace needs verified remote
  reads/writes and one backup/atomic strategy. Remaining remote gaps: the current-file and
  open-buffers search scopes and the panel preview column are not wired. The remote
  ~/~user root is expanded by the remote shell; authority.QuoteDir keeps the tilde word
  expandable and quotes only the tail ("$HOME"/'proj', ~user/'proj'), because ~'/proj' is
  not a tilde expansion in sh/bash.
- **Read-only remote explorer (landed).** A remote launch now shows the remote workspace tree
  in the explorer: a lazy, read-only `tui.RemoteExplorerTree` over a `RemoteDirLoader` port
  (app adapter `authorityDirLoader`) lists each directory through `authority.Files().ReadDir`,
  with no local filesystem fallback. Every listing runs in a command, never on the update loop;
  the command's message carries a generation and the root drops it when a newer reload or the
  tree changed (stale-response protection), keeps the expansion of directories that remain and
  prunes entries the remote side removed. The workspace trust gate (`authGate`) runs before any
  listing; a disconnected authority is the explicit `remote workspace is disconnected; activate
  the window to reconnect` notice and the tree keeps its cached rows until the existing
  authority-reconnect path reloads it. Listings are bounded (5000 entries per directory, the
  authority's own cap, over it a clear error naming the directory), hidden entries are filtered
  by the explorer's show-hidden toggle, ordering matches the local tree (directories first,
  natural, case-insensitive), and a trailing slash-free remote root is shown as given. Opening
  a file uses the existing row->`OpenFileMsg` path and therefore the remote `FileAccess`
  (save/revert already route to the authority). The tree is read-only: the local file-operation
  adapter is cleared for a remote launch, the panel refuses new-file/new-directory/rename/
  delete/cut/paste with one explicit notice, and the explorer context menu keeps only the path
  copies; the local project opener, preview, project search/replace and git-status ports stay
  refused/replaced as before. Reveal/follow and re-expansion of unloaded ancestors work by
  reloading the waypoint directories and revealing the path in the new listing. Gaps vs
  Fresh's remote explorer (which runs the full file tree over the authority): remote
  rename/delete/create, drag/move and cut/copy/paste are not wired, remote .gitignore filtering
  is not implemented (the toggle is a no-op and no entry is marked ignored), remote git marks
  stay inert, the tree is not watched for remote directory changes (the file watch covers open
  documents' directories only), a directory symlink is shown as a directory (its target is
  listed through the symlink path), and the refresh/again cadence is request-driven rather than
  backed by a remote directory watcher; Windows clients still share the root's `filepath`
  assumptions for remote POSIX paths. Tests: `internal/tui/remote_tree_test.go` (lazy
  expand/no-read-before-command, natural order, hidden filter, stale toggle drop,
  prune/keep-expansion, error+retry, reveal waypoints, read-only/hidden capabilities) and
  `remote_tree_model_test.go` (Update does no remote read; initial load and reconnect reload
  run in commands; disconnected notice keeps rows), `internal/tui/explorer/async_test.go`
  (async toggle is a command and the read-only refusals/notices), and
  `internal/app/remote_explorer_test.go` (UTF-16-independent entry mapping, trust gate before
  reads, disconnected refusal, remote-only listing with a local decoy present, 5000-entry
  clear error).
- **Split persistence (landed).** The workspace file's `Layout` (the embedded
  `session.Session`) also carries the editor's pane split tree as an optional
  `layout` object: inner nodes direction/ratio, leaves file index + path and per-pane
  top/topRow, plus a focus index in reading order. It is additive/omitempty at session
  version 1, bounded (16 panes, depth 16) and validated on load; a layout that no longer
  resolves (missing/mismatched/ambiguous pane file) falls back to one pane and never a
  wrong file. Capture is a pure model read (`m.SplitState()`) from the snapshot step,
  restore happens right after `tui.New` in the shared build path (so daemon workspace
  materialization and restart restore panes), legacy files load unchanged, hot-exit restore
  skips it, and remote launches keep the empty session plan (no local/remote path mapping
  yet, #21).
- **Capture is snapshot-and-write (landed).** The UI goroutine takes a consistent in-memory
  snapshot (layout, unsaved buffer texts, large-file overlays, terminal record, find
  history) and a per-workspace ordered writer queue writes the stash files, session JSON,
  terminal record, search history and the registry layout/touch into the workspace's
  session directory: two delayed snapshots of one workspace are written in snapshot order,
  so an older write can never overwrite a newer one, and no disk work runs on the UI
  goroutine (the registry itself is concurrency-safe for the UI, socket and writer
  goroutines). Ordering: a close waits for that workspace's queued writes, and daemon quit
  snapshots every materialized workspace, waits for every write before closing the
  runtimes (active first). The exit path is checked (`closeAllChecked`, #19a review): all
  capture writes share ONE deadline (the old closeAll waited per write); a failed or
  timed-out capture is retried with the runtime's synchronous save (the same fallback a
  workspace close uses); when that also fails the runtime's crash-recovery copy is written
  so the text survives for the next start, and the error names the recovery path. The
  runtimes are closed only after those attempts, and any capture failure is returned, so
  the daemon exits non-zero instead of reporting a clean quit (the compatibility closeAll
  wrapper only prints the error; tests and deferred cleanup still use it). This matches
  Fresh's hot-exit promise that a daemon shutdown never drops unsaved work quietly (Fresh
  flushes its per-buffer recovery store for every workspace at normal shutdown and keeps
  the still-modified entries); gvim's last resort is the recovery copy plus a named error.
  Overwrite protection (delayed-writer follow-up): each workspace has a write lock and a
  snapshot sequence/commit token (`captureWriter`). `runWriter` holds the lock across the
  write and the registry update and commits its sequence; the exit fallback only runs when
  it can TryLock, and then sets `stopped` and `committed = seq`, so a delayed queued older
  snapshot skips its write instead of overwriting the confirmed newer state (regression:
  a gated writer plus a fallback commit). If TryLock fails, a write is in flight holding
  the lock with the NEWEST snapshot: the exit runs no fallback (that would be the
  overwrite race), marks that runtime `skipSave` so its own synchronous exit save cannot
  race it either, keeps the crash-recovery copy as the last resort, and the daemon exits
  non-zero while the in-flight write is left to finish. Limits, honestly: a writer stuck
  inside the filesystem cannot be interrupted or waited out (the recovery copy plus the
  non-zero exit is the best effort, and a hard kill before it finishes loses that last
  capture while the previous session file stays intact); the fallback save does not
  rewrite the registry layout (the last captured layout stays; the session file is
  authoritative for content); and a workspace without a session directory cannot persist
  (the registry always gives daemon workspaces one). Switching itself does NOT wait for the disk write (that is the point
  of the writer); a crash between snapshot and write loses that last capture, while the
  previous session file stays intact — this is the documented limitation of the slice, and
  a failed write is reported (tested with a read-only session directory and an injected
  failing writer). The capture is field-for-field compared against `restore.WriteSession` in a
  regression test (layout, explorer/sidebar, cursor/top, active tab, unsaved stash; the
  session format has no split record, so no split state exists to capture yet).
  Per-workspace plugin runtimes are kept materialized per workspace (memory for
  isolation): only materialized workspaces cost a runtime; the registry keeps the rest as
  descriptors.
- **Extract tab to a new workspace (first slice, landed).** The tab context menu's
  "Extract to New Workspace" (and the palette row "Extract tab to new workspace") moves the
  active file tab out of the daemon's workspace. Fresh moves a tab into a *co-tenant*
  workspace over the same project root; registry v1 keeps one workspace per canonical root,
  so this slice roots the new workspace at the **file's folder** instead: the UI goroutine
  captures the tab (path, cursor/top, page-view flags, virtual column, unsaved text) and
  returns a command; the command canonicalizes the folder (disk work stays off Update),
  refuses a folder that is already the current root or any registered workspace with a
  clear notice while the tab stays open, then creates the workspace, writes its session
  JSON with the tab's `FileState` and a stash of the unsaved text, sets the registry layout, and
  marshals back to the UI goroutine to force-close the source tab and switch (the
  materialization itself stays async). The report is "Extracted <name> into workspace
  <label>"; a failure before that close leaves the tab and its text open and reports why.
  The close-workspace flow asks first when the workspace still runs terminals: the
  confirmation lists every one of them and its agent (the dock's `RunningTabs` snapshot), and
  the close report and protocol close answer say exactly what was stopped; a dormant close
  reports that nothing was stopped. Rename reports the new label on the status row (the
  protocol answer stays the list only). Fresh's separate diff_nav plugin is unrelated; the
  per-tab "Extract to New Workspace" item also appears in Fresh's tab context menu.
- **Remainder of #19a (not shipped).** A remote workspace's `authority_spec` field (registry
  v1 has no authority), the initial terminal-launch record in a workspace descriptor (the
  discovery Import can create a workspace but not attach an argv to it yet), shared
  buffers/daemon-level open-path map beyond the switch-to-existing-workspace rule (#19b),
  a workspace switcher that shows live runtime details (the list shows label/root/state
  only), and making the capture write itself cancellable/ordered under a slow disk beyond
  the quit wait. The extract first slice leaves these open: co-tenant workspaces over one
  root (Fresh's model for the extracted tab), extracting into an existing workspace,
  terminal-tab extraction (Fresh moves the live PTY; gvim's dock tabs cannot be extracted
  yet), large-file overlay extraction, extracting more than the active tab at once, and the
  cross-workspace shared-buffer model (#19b). The round-12 daemon still hosts exactly one
  attached client.
