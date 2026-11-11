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
    palette/ picker/ pluginpanel/ popup/ prompt/ searchpanel/ space/ style/ textsafe/
  terminal/                     PTY + screen model behind the terminal dock
  filewatch/                    fsnotify watcher (explorer reloads, project switches)
  daemon/                       attach/detach socket host and the workspace registry
  agentdiscover/                read-only tmux and agent-transcript session discovery
  uitest/                       test-only in-process UI harness

<top level>                     TUI-free engine, shared libraries and services
  buffer/ document/ editor/ search/ syntax/ fold/ diff/ encoding/ session/
  decorations/ textlayout/ outline/
  atomicfile/ shellcmd/
  workspace/ explorer/ finder/ fuzzy/ fsops/ gitstate/
  config/ keymap/ theme/ i18n/
  authority/ (the workspace backend slot: local or SSH, +authoritytest)
  env/ (project environment detection and activation)
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
| R2 | `charm.land/*` (Bubble Tea, bubbles, lipgloss) is imported only by `internal/tui/…`, `internal/terminal`, `internal/daemon` (it runs the TUI program over a client socket) and `internal/uitest`. The plain `github.com/charmbracelet/x/*` helper libraries (`ansi`, `term`, `vt`) are not the UI framework and stay free. |
| R3 | Leaf packages import no package of this module: `buffer`, `textlayout`, `diff`, `encoding`, `fuzzy`, `pluginapi`, `decorations`, `theme`, `fsops`, `gitstate`, `lsp`, `lsp/lsptest`, `atomicfile`, `shellcmd`, `config`, `explorer`, `keymap`, `i18n`, `plugin/bundled`, `plugin/pattern`, `outline`, `updatecheck`, `internal/agentdiscover`, `internal/tui/textsafe`. (`lsp/manager` sits above `lsp` and may import it.) |
| R4 | Engine direction. `editor` does not import the UI, app, cli, plugin runtime/contract, lsp, finder, fsops, workspace, keymap, theme, config or commands. `search` does not import `editor`. `syntax` imports only `theme` among module packages. `session` imports only `buffer`, `document` and `atomicfile`. `commands` imports only `fuzzy` and `keymap`. `workspace` imports only `config`. `trust` imports only `config` and `atomicfile`. `finder` imports only `explorer` and `fuzzy`. `authority` imports only `trust` and `plugin/hostos`. `env` imports only `config` and `trust`. `internal/daemon` imports only `config` and `session`. |
| R5 | The TUI tree does not import `internal/app`, `internal/cli`, the plugin runtime, `lsp`, `finder` or `fsops`; a TUI sub-package does not import its parent `internal/tui`. |
| R6 | `internal/cli` imports only `internal/app`, `internal/configview`, `authority` (the top-level remote-spec parser it classifies targets with) and `syntax` (and cobra). |
| R7 | Only the root main package and `internal/cli` import `internal/app`; only the root main package imports `internal/cli`; only `internal/app`, `internal/pluginhost` and `plugin/plugintest` import the plugin runtime; only `internal/app` and `internal/uiadapter` import TUI sub-packages. |
| R8 | No package in the shipped binary imports test support: `buffer/buffertest`, `plugin/plugintest`, `lsp/lsptest`, `internal/uitest`, `internal/apptest`. |
| R9 | `pluginapi` and `decorations` import no module package and no charm package. |
| R10 | The plugin tree imports only the standard library, `gopher-lua`, `hujson`, `pluginapi`, `plugin/bundled`, `plugin/pattern`, `plugin/hostos` (plugintest may also import the runtime and other plugin sub-packages). No charm. |
| R11 | `editor`, `document`, `buffer` and `decorations` do not import `pluginapi`. |
| R12 | Only `internal/tui` (its root and its own sub-packages), `internal/app` and `internal/uiadapter` import TUI sub-packages. `internal/tui/pluginpanel` imports only `grid`, `style`, `space` and `pluginapi`. |
| R13 | The private host packages: only `internal/app` imports `uiadapter`, `pluginhost` and `lsphost`; `restore` and `highlight` may also be imported by `uiadapter`; `keybindings` by `uiadapter`/`pluginhost`/`configview`. |
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
| `internal/cli` | cobra command tree, flags, target classification (DIR vs FILE:LINE:COL through an injected `stat`, remote targets through `authority`), exit codes. | `internal/app` (Options/Run), `internal/configview`, `syntax`, `authority`, cobra, stdlib |
| `internal/configview` | Backs `gvim config show`, `gvim config paths`: renders the effective config as JSON and reports config file locations. | `config`, `internal/keybindings`, `keymap`, `theme` |
| `internal/app` | Composition root: `Options`, `Validate`, `Run`, `buildRuntime`; wires engine + host packages + TUI. | engine packages, host packages, TUI root and sub-packages, plugin runtime |
| `internal/agentdiscover` | Read-only discovery of terminal sessions that already exist outside gvim: the tmux socket walk, bounded `list-sessions`/`list-panes` parsing, agent pane name matches and qualified attach argv. TUI-free; the runner and an `AllowCommands` trust gate are injected. | stdlib only; a scan never starts or attaches a session; see `docs/terminal.md` Agent discovery |
| `internal/daemon` | The daemon MVP (#18a): the socket protocol and lifecycle, the hosted editor's socket IO, the attach client that forwards this terminal to a daemon, and the workspace registry. | `config`, `session`, charm, stdlib |
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
| `buffer` | The text model: lines of runes, positions, edits, undo/redo, observer, large-file mode. | Two storage modes in one package: in-memory lines, and a disk-backed `bigStore` for large files (`OpenLarge`, `Snapshot`, `IndexScan`, `Rebase`). A split was considered and rejected: the mode is part of `Buffer`, not a layer on it (see `buffer/doc.go`). The source can be an optional asynchronous `WindowSource` (remote large files): `ReadAt` is cache-only and a miss returns `ErrSourcePending` at once while recording the window (`TakePendingWindow`), so the paint path does not block; the root loads the window in a command (`LoadWindow`) and repaints. Local `fileSource` behavior is unchanged. |
| `document` | One open file: text, cursor/carets, selection, folds, view settings, saved/on-disk/encoding/EOL state, large-file mode, text commands and motions, document-level load/save planning, and the per-document plugin seam data. Remote large files load through `remote_large.go`: a 1 MiB-aligned, 8-window LRU `buffer.WindowSource` over the authority's `RangeAccess`, read-only, with `TakePendingLargeWindow`/`LoadLargeWindow` for the root. | The model half of the old `editor` god package; imports `buffer`/`search`/`syntax`/`textlayout`/`fold`/`diff`/`encoding`/`atomicfile`, never `editor`, `session` or `internal/`. |
| `editor` | The editing session: tabs, active document, action registry and `Do` dispatch, statuses, save/revert/autosave jobs, disk polling, shell jobs, settings resolution, marks/jumps, and the plugin seam's session half. | Imports `document`, `atomicfile`, `shellcmd` and the engine leaves; no UI. `editor/bridge.go` still aliases the moved names for consumers. |
| `search` | Find, scan, replace and range search over `buffer` lines. | `visible.go` is the visible-window search (renamed from a misleading name). |
| `syntax` | Chroma lexer resolution, token spans for a visible window, markdown inline styling, style table from a theme. | |
| `fold` | Indentation fold ranges. | Small leaf used by `editor`. |
| `diff` | Line diff (go-diff) into hunks; used by the compare view and plugin services. | |
| `encoding` | Decoding/encoding detection and conversion (UTF-8/16, charsets via `x/text`). | |
| `session` | Session file format and stash files (open tabs, cursors, unsaved overlays). | The restore *policy* lives in `internal/restore`. Session and search JSON go through `atomicfile`; the overlay-stash payload is `document.StashOverlay`. |
| `decorations` | Plugin decoration store: namespaced items per buffer, anchors that follow edits, per-line query. | Pure data, stdlib only. |
| `authority` | The workspace backend slot: four routing surfaces (file I/O, one-shot process, long-running stdio spawn, terminal wrapper), the local implementation over the plugin OS port, Fresh-compatible scp/ssh:// target parsing with path translation, and the first SSH backend (pure-Go x/crypto/ssh) for terminal and one-shot exec. | TUI-free, no internal imports; `internal/app` wires it for a remote launch (docs/authority.md). |
| `env` | Project environment detection (detectors, markers) and activation: the shell snippet's environment delta, captured once per input fingerprint and applied to every spawn path. | Imports `config` and `trust`; a capture runs its shells without holding the provider lock. |
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
| `trust` | Workspace trust: the three levels and the undecided state, the per-user decision store, Fresh's open-time policy, executable-content marker scan and the spawn gate (`DecideSpawn`, docs/trust.md §3.1). | TUI-free; store at `config.StateDir()/trust.json` through `atomicfile` (never inside a workspace; one-time move from the config dir; see [files.md](files.md)); the app gates the config project layer on the level and wires the same live level into the authority, plugin OS and LSP manager spawn paths. |
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
source: `ComputeLayout`, the `Style` chrome a look is built from, hit map, tab-strip and
welcome placement), `sidebar` (the explorer
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
| OS port | `hostos/os.go`, `hostos/nopos.go` | file/process/watch interface used by `gvim.fs` and `gvim.proc`; the real implementation is in `internal/pluginhost` |

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
3. Import gate rewritten for the new tree (now R1-R16) and generator paths updated.
4. `internal/app` split into host packages (`uiadapter`, `pluginhost`, `lsphost`,
   `restore`, `keybindings`, `highlight`, plus test-only `apptest`), and the
   `gvim config` backend moved to `internal/configview` with `syntax.GrammarNames`.
   (`app` has since grown again with the remote, daemon-workspace and settings wiring.)
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
9. `lsp` split into the client, `lsp/manager` and `lsp/lsptest`; the plugin OS port
   extracted to `plugin/hostos`, and `plugin/doc.go` holds the plugin file map.

Next (tracked as follow-up work; each keeps build + tests green):

1. Remaining TUI extraction: the keybinding editor (`tui/keybindeditor`); its tests are
   model-driven (`keybind_editor_test.go`) and need a harness rewrite against the new
   types. The plugin host and LSP UI remain last.
2. Finish the editor split tail: move the `editor` model tests into `document`, then delete
   the alias bridge (`editor/bridge.go` + `document/export.go`, ~413 lines) by retargeting
   the ~165 consumer references; tighten `document`'s exported surface to accessors.
3. Fold `fold` into `textlayout`; optional `plugin/luastdlib` extraction; optional keybinding
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
  files would race both ways (one exception: a bare launch with `auto_attach` on joins the
  running daemon instead of refusing; see Auto attach below) (the direct editor writes the file under the daemon's
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
- **Remote target / jump box (landed).** A daemon start may take a remote target:
  gvim --daemon=NAME user@host:/path starts the daemon locally (this machine needs gvim;
  the host needs only SSH) and hosts that remote workspace over the SSH authority, so the
  editor, terminal, LSP, project search, remote explorer, git marks and quick-open all
  address the remote host. The target is consumed only at daemon start and its canonical
  text is persisted in the workspace registry (authority_spec), so a restart with the same
  target reuses the same workspace identity and reconnects instead of minting a duplicate;
  a reattach (gvim --attach=NAME) ignores the target in its arguments because the daemon
  owns SSH. Before the socket hosts, the daemon connects its foreground remote workspace on
  the daemon's own terminal and answers password, encrypted-key passphrase and 2FA prompts
  there; a start without an interactive terminal fails with an actionable error instead of
  prompting on a stdin nobody answers. A workspace binds one backend: local workspaces stay
  local when switched to, and a dormant remote workspace materializes its own authority
  lazily from its stored spec. Tests use an in-process fake authority (no real SSH): start
  with a remote target, detach/reattach, restart reuse of the persisted spec/identity, lazy
  dormant switch, a stored spec that no longer parses keeping the current workspace, and the
  prompt/non-interactive paths. Daemon-hosted remote workspaces also capture and restore
  their workspace session across a restart: the existing capture writes the layout, file
  states and stashes into the session directory keyed by stable id, and the next start
  (after the authority connects) reopens the files through the remote document access and
  restores tabs, active tab, cursor, pane layout and unsaved text; a vanished file keeps its
  stash and is reported by the session notice, while a disconnected start restores nothing
  and skips its capture so the saved session survives for the next start. Direct remote
  launches still keep no local session.
- **Attach.** `gvim -a [NAME]` (or `--attach`) dials the socket, sends a hello
  with the protocol version, the terminal size and any named files as an open-file
  frame, puts the terminal in raw mode, and forwards input bytes, output frames,
  resize signals and SIGWINCH. A detached/exit frame restores the terminal. Several
  clients may be attached to one session (Fresh's shared-view model): every server frame
  is broadcast to all of them, input from any client reaches the program, and the
  program is told the **smallest attached client's size**, so a larger terminal
  letterboxes instead of cropping the shared view (a resize or an attach renegotiates
  it). Detach or a dropped connection removes only that client; the others keep
  receiving frames and the session keeps running. A slow client is detached after one
  bounded write; an unknown frame drops only that client.
- **Protocol version.** The hello carries `version` (`ProtocolVersion = 1`). A client
  speaking another version is sent a dedicated version-rejected frame naming both
  versions (the client prints that as an error and exits nonzero) and dropped before any
  state change: it cannot resize, type into, or disturb a session hosted by an
  incompatible daemon. The server never guesses compatibility.
- **Hosting.** The daemon runs the normal Bubble Tea program through `tui.RunWithIO`
  with the server as its input and output: input from any attached client feeds the
  program's reader (which blocks while none is attached, so the editor stays alive),
  output frames are broadcast to every attached client (and discarded when none is),
  and an attach or resize sends a window-size message with the smallest attached size so
  every client's frame is repainted for the shared view. The hosted editor restores the
  session on daemon start and saves it on daemon exit; a detach saves nothing and does
  not stop any process.
- **Detach.** The hosted editor lists one extra palette row, "Detach from Daemon"
  (only under `--daemon`). It sends the detached frame to every currently attached
  client and drops them; the editor keeps running and the next attach repaints it.
- **Auto attach.** With the user-config knob `auto_attach` on (top-level boolean,
  default **off**; Fresh defaults it on), a bare launch — no files, no direct-mode flags
  (`--diff`/`--simple`/`--stdin`/`--restore`/`--no-restore`), a terminal on stdin, and no
  explicit `--daemon`/`--attach` — joins a daemon that already hosts the same workspace
  instead of being refused as a second writer. It prints a one-line notice naming the
  daemon before proxying the terminal. It never starts a daemon, never applies to a
  remote-target launch (explicit `-a` is required there), never applies to an explicitly
  named ambiguous set (several named daemons for one root and no default daemon keep the
  refusal), and the preference is read from the user layer only. Fresh's
  one-shared-daemon/"any flag is direct" nuances are not copied; gvim keeps its per-root
  named daemons. Tests cover the decision (off, no daemon, direct modes, notice) and the
  multi-client protocol.

- **Protocol.** Length-prefixed frames: a 4-byte big-endian length (at most 1 MiB),
  a type byte and the payload. Client frames: hello (JSON cols/rows/args), input
  (raw terminal bytes), resize (JSON cols/rows), detach, open-file (JSON paths),
  ping. Server frames: output (raw ANSI), notice (text), detached, pong, exit.
  Unknown or malformed frames drop only that client.

### 8.1 Workspace registry (#19a, landed round 13)

The registry is the workspace store on top of the round-12 daemon ownership model. It
mirrors Fresh's workspace cache: one durable file per workspace, no central list. Registry
v2 allows several CO-TENANT workspaces over one canonical root (Fresh's extracted-tab
windows, labels "proj", "proj (2)", ...); a registry v1 file (one workspace per root)
still loads unchanged.

- **Location and name.** `<user data dir>/gvim/workspaces/<encoded-canonical-root>.<stable-id>.json`
  (`GVIM_WORKSPACE_DIR` overrides the directory for tests). The canonical root is
  absolute, cleaned and symlink-resolved, so `./x`, `x/` and a symlinked spelling are one
  workspace; a workspace is keyed by (canonical root, stable id), so v2 allows several to
  share one root. The encoded root is one percent-encoded
  file-name component (a deterministic sha256 name when it would exceed one filename); the
  `root` recorded inside the file stays authoritative.
- **Schema v1/v2** (`daemon.WorkspaceFile`): `version`, `stable_id` (minted once with crypto/rand
  at creation, adopted from the file on restore, used as the per-workspace session directory
  name - so a v1 -> v2 migration never moves or loses the session directory), `label`,
  `root`, `last_focused_at` (unix milliseconds) and `layout` (the existing
  `session.Session` layout; nil until captured). Version 2 is the current format (co-tenant
  workspaces); version 1 files load as before and a later save rewrites them as version 2.
  A co-tenant's empty label gets Fresh's precedence - the root basename, then "basename (2)",
  "basename (3)", ... counted from the workspaces already at the root, skipping a numbered
  label a rename took. The per-workspace session directory `workspaces/sessions/<stable-id>/`
  holds the stashes and the terminal record (`terminal.SavedState`, its own versioned schema,
  owned by the terminal/dock layer: saved and loaded with `restore.SaveTerminal`/
  `restore.LoadTerminal`). The workspace file never embeds the terminal record, and old
  workspace files load unchanged. Version 2 gained the additive authority_spec field: the
  canonical remote target text of a workspace bound to a remote authority (empty = local);
  old files load as local, a file whose spec contains control characters is refused by the
  scan with a diagnostic, and a remote workspace's root is normalized remotely (cleaned
  POSIX path, never stat'ed on this host).
- **Registry API** (`internal/daemon/workspaces.go`): `WorkspaceDir`, `OpenRegistry(dir)`,
  `CanonicalRoot`, `List` (every workspace; dedup only an identical `stable_id`, keeping
  that identity's freshest file; ordered by root group - roots by their freshest
  `last_focused_at` first, then root path, and freshest first inside a root), `Get`,
  `Create` (returns the freshest existing workspace of a known root), `CreateCoTenant`
  (always mints a new workspace over the root - extraction's path), `CreateWithAuthority` and `DiscoverWithAuthority` (the daemon jump-box foreground: a workspace keyed by remote root and canonical spec, reused on restart; the root is normalized without local IO), `Save`, `Rename`,
  `Touch`, `UpdateLayout`, `Remove` (deletes only the registry file — session data
  is kept), `RemoveSessionData` (explicit discard) and `SessionDir` (creates the
  per-workspace session directory). `Workspace.Materialized`/`Active` are runtime flags, never
  persisted.
- **Durability.** Saves are atomic: temp file in the same directory (0600), write, fsync,
  close, rename over the target, then a directory fsync; a failed save leaves the previous
  file intact and no temp file behind. A file whose `version` is newer than the binary is
  refused on read and on overwrite (`ErrWorkspaceVersionTooNew`), skipped by scans with a
  diagnostic and never modified or deleted; corrupt files are likewise skipped, not deleted.
  The registry tests cover CRUD, stable-id durability across reopen, canonical-root and
  symlink keys, co-tenants sharing a root with independent layouts and session directories,
  Fresh label precedence, root-grouped ordering, duplicate-stable-id dedup, v1-file loading
  and migration on save (stable id and session directory kept), discovery of a co-tenanted
  root (freshest foreground, all others dormant), future-version refusal/no-overwrite,
  corrupt-file skipping, atomicity/no temp leftovers, rename/touch/layout persistence, close
  keeping session data, safe encoded names and the directory override, plus authority-spec
  round-trip/reuse-by-(root,spec), local discovery ignoring spec-bearing entries, and
  old-file/new-field tolerance.
- **Workspace protocol (wire + server dispatch + app handlers, landed).** The client
  frames `workspace_list`, `workspace_select`, `workspace_new`, `workspace_close` and
  `workspace_rename` carry JSON payloads; the server answers `workspaces` (the list with
  the active id and each entry's id/label/root/materialized/active) or a notice. The server
  exposes them through `Server.OnWorkspaceRequest`; a daemon without a workspace layer answers
  with a notice, a malformed request is a notice too, and the connection survives either.
  `Hello.Workspace` names the workspace to attach to (empty means the current
  foreground one; the round-12 attach path still ignores it). `Registry.Discover(root)` is
  the daemon-start view: the launch root's freshest workspace is the foreground one (created
  only when the root has none) and every other registry entry - including the other
  co-tenants of the same root - is an inert descriptor. Registry.DiscoverWithAuthority is
  the same view for a daemon started with a remote target: the foreground workspace is
  keyed by (remote root, canonical authority spec), created only when absent, so a restart
  reconnects the same identity; local discovery never adopts a spec-bearing entry, and a
  local daemon keeps its remote workspaces dormant until a switch materializes them. The
  app handlers drive the same
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
  model's Init. A workspace build parses its stored authority_spec and sets that
  workspace's remote options, so a dormant remote workspace sets up its own SSH authority
  (trust gate, failure notice) while local workspaces keep the local backend; the daemon's
  foreground remote workspace connects on the daemon terminal before the socket hosts. The TUI exposes Workspace: Switch/New/Close/Rename/Next (daemon only, added
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
  re-opened in another workspace. A path open nowhere opens in the active workspace.
  A path already open in another MATERIALIZED local workspace is shared instead of
  switching away: see the cross-workspace shared buffers bullet below. Discovery Import uses the additive
  `WorkspaceLauncher.CreateWithLaunch(root, label, launch)` port: the manager creates or
  reuses the workspace, remembers the discovered session's attach argv keyed by stable id,
  and starts it in that workspace's dock once materialization + Show install the model
  (through the host's command queue, never from a goroutine touching the editor). A
  repeat import of the same root starts its own attach tab in the one workspace of that
  root: the workspace layout/files are shared, the selected session survives as a terminal
  tab (the model asks the dock to find a tab by the launch's Source and focuses it instead
  of duplicating), because registry v1 has no per-session workspace identity or
  initial-launch record yet.
- **Workspace-scoped async ownership (landed safety slice).** Every built workspace model
  gets a palette.Owner {workspace stable id, model generation}; the app's workspace
  manager increments the generation for every build (a re-created workspace after eviction
  gets a new one, a warm switch keeps its own). Palette async results/open actions and
  project-search async/root-facing messages carry the owner; the active model drops a
  mismatched nonzero owner before any overlay/action sees it, and the search panel also
  owner-checks its own result messages. This prevents stale GrepResult/preview/fileTick/
  OpenTerminalMsg/searchpanel results from colliding with the newly shown model's seq or
  model-local dock tab id. A zero owner means a direct launch (one model) or a hand-built
  legacy/test action; generated messages are never zero in a daemon model. The manager calls
  Model.CancelTransientWork on the UI goroutine before a switch (beginSelect), for a
  non-active close and for eviction; the palette cancels its probe, Live Grep context and
  preview read, the project-search panel cancels its stream/preview (Panel.CancelWork) and
  the root cancels its kept replacement. Dock PTY waits are never canceled: a warm
  workspace must keep receiving output/exit.
  The WorkspaceHost source-route wraps commands returned by a model's Update/Init and by
  QueueFor at enqueue with {owner, source model}; a BatchMsg is re-wrapped per subcommand,
  and the post-Show WindowSize replay is bound to the model that was shown. A hidden warm
  model's result is delivered back to that model's Update on the UI goroutine (active model
  untouched; commands it starts are re-tagged with the same source); a closed model's
  result is discarded, and a wrapped command checks the source's atomic closed flag BEFORE
  running. Program-level results (tea.Quit/ClearScreen/Suspend/Interrupt, clipboard, raw/
  print, terminal queries) are handed to the Program for the active source; a hidden
  source's program effect is dropped. hostCallMsg/Do is a separate cancelable handshake:
  a timeout before the UI claims the call marks it canceled so the queued fn can never run
  later; if the call already started, Do waits for it to finish rather than returning
  while it mutates state. Its bool is a completion guarantee: false means the callback
  never ran, true means it ran to completion (DoWithTimeout lets tests use a short bound).
  Calls go through one host-owned sender goroutine and a bounded queue (64), never a
  goroutine per call: a blocked UI costs one sender plus the queue, and a full queue makes
  Do fail fast (callback never ran). A timed-out queued call is canceled and skipped when
  the UI eventually drains it; a call already started is waited for. PostOpenFileAt uses
  the same bounded queue with a 2 s default bound (PostOpenFileAtWithTimeout for other
  bounds): it returns false instead of blocking a socket goroutine, and the app reports
  that the open was not delivered. A true PostOpenFileAt only means ACCEPTED into the
  bounded queue, not delivered or opened: the program may stop first. The sender prefers
  the stop channel when both it and the queue are ready, so a stopped host does not start
  another send. The WorkspaceHost is single-use: one program/sender for its lifetime; after
  stop, Do/PostOpenFileAt/QueueFor cannot deliver (QueueFor requires the host and a source
  model, rejects a stopped host or a closed source, and is capped at 64 pending commands;
  pending work is drained only by a later Update/Show). Rejected imported-terminal launches
  and rejected eviction passes are reported with a visible notice (the eviction pass retries
  on the next switch) rather than silently dropped. host.Notify is safe from any goroutine:
  it queues a bounded notifyMsg through the same outbound sender (never Do or a model write
  from the caller); host Update applies it on the UI goroutine, and a full/stopped queue
  makes Notify return false (notices are best-effort). UI callbacks that need a guaranteed
  notice use manager notifyOnUI, which writes the active model directly. DoWithTimeout false
  guarantees the callback never ran; true guarantees it completed. A callback that already
  started has no hard timeout: Do waits for it even past the nominal bound.
  Teardown uses ONE UI closure: quiesceModelForTeardown runs MarkClosed +
  CancelTransientWork + shared-buffer DropEditor atomically through host.Do, BEFORE the
  off-UI runtime teardown. If the UI does not answer, the cancelable host call never runs,
  the workspace stays fully materialized with its ownership intact, and close/evict report
  the failure (tests cover both). An already-starting command can finish its side effect,
  but its result is dropped by the closed check. UI callbacks never use the blocking
  notify path: applyCommit's eviction-refusal notice and enqueueCapture's snapshot-failure
  notice call notifyOnUI (direct active-model notice), avoiding a self-Do freeze.
  Explicit exceptions: tea.Sequence is unsupported (its unexported sequenceMsg runs
  subcommands outside the envelope; gvim has no usage, and hidden subcommands would lose
  lifecycle work, so no unsafe reflection is used); tea.RequestCapability is unsupported
  (its response is untagged and cannot be correlated to a hidden request); tea.Exec's
  request is program-level and active-source-only (hidden callbacks are dropped);
  hostCallMsg is app-internal UI work and is never model-routed. Untagged external
  program.Send/program-delivered resize/open messages stay active-targeted by design; the
  manager's internal PostOpenFileAt is target-bound. LSP/watcher routing is structural
  rather than covered by a dedicated regression; arbitrary plugin async messages are not
  individually audited. This is not cross-workspace Live Grep: there is still no read-only
  WorkspaceSearch port, workspace-scoped hit identity for other workspaces, or
  remote-authority-aware open routing, and those remain prerequisites for the
  All-workspaces scope.

- **Cross-workspace shared buffers (landed #19b bounded slice).** A daemon's local
  workspaces share one `editor.SharedBuffers` registry (`internal/app/workspace_shared.go`, created once by
  the workspace manager and copied into every workspace build's editor options; a direct
  launch and a remote/authority workspace get nil and keep independent buffers - remote
  files live on another host). A file is owned by one workspace at a time. Opening a path
  already open in another materialized workspace in the focused workspace gives that
  workspace a read-only mirror (`document.NewMirrorDocument`: text, saved state and encoding copied;
  large, virtual and over-8 MiB documents fall back to the ordinary independent open) and
  immediately requests ownership; opening it while a non-focused workspace holds it leaves
  the mirror read-only until that workspace is focused, and the daemon open-file routing
  keeps the request in the focused workspace instead of switching to the owner when the
  path is shared. A daemon open-file REQUEST for the path still switches to the owning
  workspace (the established #19a routing and its parity floor); sharing starts on an
  OpenFile inside another workspace (finder/explorer). Ownership moves the CANONICAL document (text, undo history, unsaved
  state, disk baseline) to the entering workspace's editor and leaves a fresh read-only
  snapshot in the old one, so a workspace switch is the ownership request; a refused
  request (the manager's switch path can refuse, tested with a hook) leaves the mirror
  read-only with a conflict in its status row. Owner edits reach every mirror through an
  explicit change path (`editor.SharedBuffers.onOwnerChange`): the mirror applies the `buffer.Change`
  only when its recorded owner version is exactly the version before it, and reloads the
  whole text under the document stale-version guard otherwise (coarse edits, undo/redo,
  a stale version). A focus also syncs the mirrors' saved/disk state, so a save updates
  the other workspace's dirty markers. Closing or evicting a workspace calls
  `DropEditor`: an owner hands the canonical document to a mirror, a mirror is dropped, and a
  close therefore keeps the file and its unsaved state in the other workspace. A TAB close
  follows Fresh's view-vs-buffer rule: closing a mirror tab never asks the unsaved question
  (the owner keeps the text) and simply detaches the mirror; closing an owner tab while a
  mirror exists also asks nothing and promotes the canonical document to that mirror first;
  only the final view of a buffer (no mirror left) keeps the ordinary modified
  save/discard/cancel prompt, and a forced close then removes the registry entry. The bulk
  CloseByPath path (a deleted file or folder) detaches all shared victims up front, so a
  promoted owner leaves no lost text and a refused transfer leaves the tabs untouched.
  Disabled: close prompts are skipped because the buffer survives in the other workspace,
  not because unsaved text is discarded.

  Startup restore uses the same registry: `OpenLoaded` for a path another
  materialized workspace owns adopts the owner's live document instead of opening an
  independent copy - an unmodified restored copy becomes a read-only mirror, and a
  restored UNSAVED copy whose text differs from the live owner is kept as a separate
  untitled `[shared conflict]` tab (the restored text is preserved for Save As; the file
  path is removed from it, so it can never overwrite the owner) while the restored
  workspace's normal tab mirrors the owner; the disk file is untouched. Adoption is
  deferred to the workspace's focus because the restore pipeline applies the stash text
  after opening: a workspace that is being materialized while another is focused loads
  independently until Focus resolves it, exactly when the restored unsaved text is final.
  The daemon tests this with a dormant workspace whose saved session restores the same
  file with a stale stash. The conflict copy is pathless and labelled, so its later save
  and close cannot silently overwrite the owner: Save As to a path another materialized
  workspace owns is refused at plan time even with force (closing the local mirror tab
  does not bypass it), Save As on a read-only mirror is refused instead of forking under
  the registry, and Save As to a NEW path registers that file as the saving workspace's
  canonical shared buffer, so another workspace opening it shares/requests ownership
  rather than getting an independent copy that could overwrite it. Save As on a SHARED
  OWNER rekeys the buffer: the entry and its mirrors follow the renamed buffer to the new
  path, the old path stops being shared and reloads independently from its untouched file.
  A Save As plan holds a revocable EXCLUSIVE reservation on its canonical target from the
  UI plan step (`SharedBuffers.ReserveSaveAs`) until `ApplySavePlan`: another workspace
  cannot open, register or Save As the path meanwhile (`Open` reports it is being saved,
  `PlanSaveAs` refuses a second claim, the same editor gets an in-progress refusal), so the
  planner's ownership check cannot be overtaken by the asynchronous write that runs off the
  UI goroutine. `ApplySavePlan` releases the claim on every outcome - success, write failure
  and cancellation - `SavePlan.Release` is the hook for a caller that drops a plan without
  applying it, and `DropEditor` releases the claims of a closed/evicted workspace. This
  extends Fresh's one-buffer-per-file rule (Fresh refuses a Save As onto a path another
  editor has open) across gvim's plan-to-write window. Tests: the target is refused to the
  other workspace until apply, a failed write and a cancelled/dropped plan release it, a
  dropped editor releases it, concurrent plans produce exactly one winner, and the
  plan-/write-/apply window creates no independent copy of the target.  The conflict tab's label is persisted with the session (`session.FileState.Label` for a
  pathless buffer, written by both session writers and restored by `RestoreSession`), so an
  eviction/rematerialize or daemon restart keeps the preserved copy identifiable as
  `[shared conflict] <name>` instead of a plain `Untitled-N` tab; its text and pathless
  safety survive repeated restores, and the owner's live text and the disk file are never
  replaced by a stale restored copy. Tests also cover
  open in two workspaces, mirror read-only-until-ownership, edit propagation, ownership
  back-and-forth with undo/unsaved state, conflict notice, close-owner promotion, owner-tab
  close promotion, mirror-tab close without a prompt, the last-view prompt, CloseByPath
  promotion, and a remote launch keeping independent buffers. Residuals: the Save As
  reservation is in-process (a caller that drops a plan without Release or DropEditor leaks
  it until the editor goes away) and only guards Open/Register/PlanSaveAs - an
  OpenLoaded/plugin-created tab for a reserved path is not blocked from loading (only from
  registering as an owner); disk exists/force semantics are unchanged. Sharing starts on
  `OpenFile` and startup `OpenLoaded` but not on a plugin/API-created tab for a path
  already shared (that still opens an independent copy), and a daemon open-file request
  for an already-owned path switches to the owner rather than opening a mirror;
  the editor-level Close() used by an exiting runtime does
  not promote (the daemon's workspace close/evict path calls DropEditor first), the two
  workspaces do not become two TUI splits or panes of one workspace in this slice, and
  they share the entry's view position only when ownership moves (each keeps its own
  cursor while mirrored); large/virtual buffers and remote workspaces are independent,
  and the mirror's plugin seams see a new document id after a handoff.
- **Remote file finder (landed).** A remote launch's quick-open file mode fills the ordinary `finder.Index` (the palette's
  poll-while-loading and query-sequence staleness guards are shared); the local file index
  is never started and a local project switch is refused (it would enumerate the local
  disk). In a git work tree it uses Fresh's project-file list: one bounded authority
  one-shot `git rev-parse --is-inside-work-tree`, then one bounded
  `git ls-files -z -s --cached --others --exclude-standard` (8 MiB / 20 s) fed through the
  additive `finder.StartFuncNotify`. That single listing traversal replaces the recursive
  per-directory ReadDir for the git path and needs no client-side .gitignore reader:
  tracked files (including tracked files that match .gitignore) and untracked non-ignored
  files are emitted. The listing is filtered before emit — no .git internals, hidden path
  segments, intermediate skip directories, invalid or non-UTF-8 names, or paths beyond the
  finder's remote depth bound — while the Index keeps the 50k file cap, truncation and
  cancellation semantics, and every Restart/refresh re-runs the remote git listing (a new
  carrier or a changed work tree is picked up). Cases the one pass cannot represent — no
  git work tree, git missing/failing, a truncated listing, or a nested repository/submodule
  that git lists by name only — fall back to the previous recursive authority walk (bounded
  directories/depth, skip dirs/hidden, no symlink traversal, remote .gitignore reader)
  instead of silently dropping files. A git probe/listing failure that forces this fallback
  is not silent: the index carries one non-fatal `Notice` (via `StartFuncNotify`) and the
  Quick Open file mode shows it as one informative row after the results, without hiding or
  disabling them. A plain non-git workspace and the nested-repository fallback are not
  degradations and carry no notice; Restart/refresh clears the notice, so a reconnect does
  not show a stale one. A root read failure of the fallback walk is the index
  error and is shown as a
  `File search failed: ...` row; a truncated list shows an incomplete-list row instead of
  posing as the whole project; subdirectory failures keep the rest (partial list). A `~`
  workspace root is expanded remotely (one bounded shell command) so the palette root and
  the walk use the remote absolute path; a failed expansion leaves the root unchanged and
  surfaces the enumeration error, never a local fallback. Remote rows join with slashes and
  the local file preview is never used for a remote file mode (the remote data bullet below wires an authority-backed previewer). Gaps: Quick Open's default hidden filter still leaves hidden tracked files out (the git
  listing is filtered like the walk), the fallback walk paths still use the client-side
  remote ignore reader, nested-repository/submodule contents appear only through the
  fallback path, non-UTF-8 paths are omitted (Fresh leaves them out too), a truncated git
  listing falls back to the recursive walk rather than keeping a partial one, and the
  fallback notice's English literal awaits the next i18n extraction. The palette file
  preview is the authority-backed remote reader, and Live Grep-over-remote is wired
  (Files reuses this project-search candidate scope; Buffers/Terminals stay in-memory;
  Ignored uses the widened remote scan; the local Grep port is cleared and replaced), and
  the local git-status adapter is replaced by an inert one: local git must not look up
  remote paths. The remote workspace's plugin/agent project root still comes from the
  local process root. Project Search and Git Grep now run on the
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
  remote document access; the local git state is never consulted.
  Project search (Live Grep) has an explicit remote scope rather than "whatever the tool
  walks": in a git work tree it reads Fresh's project-file listing once (`git rev-parse
  --is-inside-work-tree`, then one bounded `git ls-files -z -s --cached --others
  --exclude-standard`) and passes the candidate FILES explicitly to the tool in bounded
  batches (400 paths / 48 KiB of paths per command, at most 64 commands). The candidates
  are tracked files (including tracked files that match .gitignore, and tracked dotfiles)
  plus untracked non-hidden files; the tool therefore never scans ignored or hidden trees,
  so ignored matches cannot consume the output cap or the result limit before candidates.
  Paths follow `--` (a name starting with a dash is a path, not an option), the query stays
  a single `-e` argument, cancellation and the search timeout stop between batches, and each
  tool command's output is capped at 8 MiB (truncation flagged); one search may start at most 64
  batched candidate commands, all within the 20 s search deadline, so the cap is per command and
  not an 8 MiB total. The existing rg/grep regex engine is
  unchanged: there is no POSIX-ERE prefilter, and every returned line is re-checked by the
  local search engine as before. The candidate filter still drops anything outside the
  scope (untracked hidden files, ignored entries). Nested repositories/submodules are listed
  by name only, so a candidate list containing one cannot be mapped losslessly to explicit
  files: that search keeps the widened whole-tree scan (which reaches the contents through
  the prefix rule) and carries the notice that ignored entries were scanned. When the
  complete list would need more than 64 commands the search keeps the
  previous whole-tree widened rg scan (`--no-ignore --hidden -g !.git`) filtered by the
  candidate scope, and the panel receives a non-fatal notice that ignored entries were
  scanned and results may be capped before every candidate is searched. When the candidate
  listing itself is unavailable (git cannot run, the listing fails, times out or is
  truncated), the tool keeps its previous defaults for the workspace (rg's own rules, or
  the hidden-only grep fallback) and the panel receives the non-fatal notice "remote ignore
  scope unavailable: ... tracked files that match .gitignore may be omitted". A non-git
  workspace is not a degradation (Fresh's walk does not consult .gitignore there) and gets
  no notice. The notice is a `Notice` field on the search batches/result, shown in the
  panel's hint row and cleared by a new search; it never fails the search. When the panel
  supplies a finder file snapshot that snapshot (already ignore-filtered) is authoritative
  and this path is not used. No local file is read anywhere in this path.
  Project Replace now runs
  on the remote host through the authority file system (see the remote-project-replace
  bullet below). The current-file and open-buffers scopes now search the editor's remote
  document snapshots (the authority loaded that text; unsaved edits included) with the same
  match caps, batching and query validation as locally, and start no remote process; no local
  disk is read. The panel preview column is wired for remote results: an open buffer's text is
  used as the panel already has it; otherwise the remote adapter's
  `ContextWindowPreviewer` scans the file through authority `ReadRange` in 64 KiB chunks
  until the selected match's surrounding window (8 MiB scan budget, 256 KiB output/line
  cap, the panel's cancellation context and stale sequence). EOF makes the total line count
  known; stopping at the window reports `Window`+`Truncated` with an unknown total
  (`window only`); a match past the scan budget is an explicit error, never a file head or
  local read. The window scan validates NUL/binary and UTF-8 (with a carried partial rune)
  and returns the explicit binary/not-UTF-8 reason instead of sanitizing a partial excerpt.
  An exactly-at-cap 256 KiB line is kept as a complete line (window-truncated or whole-file
  at EOF); a longer line gets the explicit line-cap error, not an empty scanner failure. The preview carries syntax spans from the shared highlighter
  (`internal/app/preview_highlight.go`) only when the total is known and within its 4000-line
  cap, so an unknown-total deep window stays plain; an open-buffer preview is highlighted from
  the panel's text. Remaining remote gaps: an unknown-total window is plain text and the scan
  cannot reach a hit beyond 8 MiB; there are no semantic/LSP spans. Fresh renders its overlay
  preview as a real buffer through the text pipeline, while gvim has no remote preview
  buffer and only this bounded window; the explicit-candidate path needs a complete remote
  git listing (a failed/truncated listing keeps the tool's defaults, can omit tracked files
  matching .gitignore, and is reported with the non-fatal notice); a candidate list too
  large for the batched path falls back to the widened whole-tree scan (reported with the
  notice) so ignored entries can still consume the cap/time; a candidate list containing a
  nested repository/submodule also forces the widened scan (notified), because its contents
  cannot be enumerated as explicit candidates; and the match limit is applied after merging
  the batch output, so the result count can be below Limit. The remote
  ~/~user root is expanded by the remote shell; authority.QuoteDir keeps the tilde word
  expandable and quotes only the tail ("$HOME"/'proj', ~user/'proj'), because ~'/proj' is
  not a tilde expansion in sh/bash.
- **Remote Live Grep (landed).** The remote workspace's Live Grep port is the
  authority-backed `remoteLiveGrep` installed with the remote project search; the local
  Grepper is cleared in `remoteRootPorts`, so no remote row can read the local project.
  Its Files scope calls the remote project-search adapter (the same candidate scope,
  one-shot trust/spawn gate, remote cwd/env, NUL parser, per-command 8 MiB cap and 20 s
  deadline; the quick-open snapshot bounds it when present). The Ignored scope runs the
  same wide scan with `--no-ignore --hidden` because no complete candidate list names
  untracked ignored files; Buffers and Terminals are the editor's in-memory snapshots
  and are searched after files with no remote command. The palette's optional
  ContextGrepper gives the search a cancellable context; superseding the query, toggling
  a scope, dismissing, choosing a row or exporting cancels the streaming project search
  and the authority command. Row preview uses the remote previewer's cancellable
  `ReadRange` window (64 KiB chunks, 8 MiB scan budget, 5 lines before, 12 lines
  maximum, 16 KiB output) and the local head reader is never used for a remote row; a
  hit opens the remote absolute path through the existing remote document access. The
  project-search degradation/cap Notice and Truncated flag travel with the successful
  hits through `GrepResult`/`GrepResultMsg`; a tool exit>1 keeps the stdout candidates
  parsed before the failure and marks the result incomplete (the Done error still says
  why); the palette title marks `incomplete` and its
  footer shows the notice, the Alt+Q export list carries the same signal in its title and
  a leading header row, and the seq guard drops a late result from an older query so it
  cannot overwrite the current notice/rows. No remote result cache survives Resume:
  the palette restores controls only and always re-searches, and a reconnect/root change
  rebuilds the remote index/ports (a disconnected authority reports its error).
  Residuals: the notice is a footer/title chrome string, not a separate row (a long
  notice can be truncated by the frame); the Ignored wide scan is bounded but can hit
  the per-command cap; the remote preview window is a bounded line scan (deep hits can be
  unavailable past the 8 MiB prefix budget), not a remote buffer; and Buffers/Terminals
  are client-side text, not remote host state.
- **Remote data surfaces: git marks, quick-open preview, finder refresh (landed).**
  The remote explorer's git letters come from a porcelain-v1 NUL-separated git status run
  through the authority one-shot with the remote workspace cwd and the remote captured
  environment delta (8 s and 8 MiB bounds, a 2 s cache coalescing save/watch/reload bursts,
  invalidated on reconnect and re-read by the TUI reconnect command); the local git state
  is never consulted. The letters match the local port (A/M/D/U/R/C); the parser records
  the approximations: a rename/copy marks its new path R and its original path is not
  marked, an unmerged pair shows C, a typechange shows M, and ignored entries are not
  shown. The quick-open palette reads the selected remote file's head through the
  authority's windowed read (offset 0, at most min(requested, 64 KiB) plus one byte): a
  short answer is the whole file, the extra byte is trimmed and reported as truncated, so
  a large file shows a real head with an honest truncation flag. A backend without the
  windowed read falls back to the bounded whole-file read (a complete small file, an error
  for a larger one, never a partial head presented as complete); a disconnected authority
  is an error and never a local read. The finder index Restart cancels and waits for the
  old run and re-enumerates in place with a generation guard (a stale run cannot inject
  files, the old done channel is always closed, and Snapshot reports loading from the
  restart on so the palette loading poll stays coherent); the remote adapter resolves the
  slot's current file system per read, so the same palette pointer follows a reconnected
  carrier. The reconnect hook restarts the remote index, and the existing File Explorer:
  Refresh action re-enumerates it on demand (a full re-walk, not incremental; a palette
  already open during the refresh keeps its previous rows until it re-ranks or reopens).
  Residuals: palette file rows carry no git letters (the local palette rows do not either;
  marks are the explorer's), and the remote project-search panel's preview column now reads
  through this same authority windowed window path (window-only for a deep hit, known-total syntax colour).
- **Remote explorer (landed).** A remote launch shows the remote workspace tree in the
  explorer: a lazy `tui.RemoteExplorerTree` over a `RemoteDirLoader` port
  (app adapter `authorityDirLoader`) lists each directory through `authority.Files().ReadDir`,
  with no local filesystem fallback. Every listing runs in a command, never on the update loop;
  the command's message carries a generation and the root drops it when a newer reload or the
  tree changed (stale-response protection), keeps the expansion of directories that remain and
  prunes entries the remote side removed. The workspace trust gate (`authGate`) runs before any
  listing; a disconnected authority is the explicit `remote workspace is disconnected; activate
  the window to reconnect` notice and the tree keeps its cached rows until the existing
  authority-reconnect path reloads it. Listings are bounded (5000 entries per directory, the
  authority's own cap, over it a clear error naming the directory), hidden entries are filtered
  by the explorer's show-hidden toggle, the repository metadata directory (`.git`) is never
  cached and has no row (the local tree's `explorer.ListDir` drops it the same way), ordering
  matches the local tree (directories first, natural, case-insensitive), and a trailing
  slash-free remote root is shown as given.
  Remote .gitignore filtering uses the same parser and go-git matcher as the local
  explorer: the workspace root's and every ancestor/nested .gitignore are read through
  `authority.Files()` (never the local disk), bounded to 64 directory levels, 64 KiB per
  file and 4096 accumulated rules; a missing file is normal, while an oversized or
  unreadable one (or a depth/rule overflow) keeps the listing working with ignore
  filtering incomplete and one explicit notice. Ignored entries are hidden by default
  and revealed by the existing show-gitignored toggle (rows carry the loader's Ignored
  flag); the remote finder index filters through the same ignore batch, so the palette
  stops listing ignored files and directories. Remote directory watching reuses the
  existing authority-backed `remoteWatch` bridge: the explorer loader adds each listed
  directory to `RootPorts.WatchDirs`, so the polled authority Watch refreshes the listing
  through the normal watchWait -> reloadTreeAsync path with its debounce/coalescing, off
  the update loop, and its existing cancellation/reconnect behavior (Reconnected restarts
  the watches, Close cancels them). The loader probes watch support once: a backend that
  refuses Watch (or a launch without the bridge) gets one 'remote directory watching is
  not supported; refresh manually (Ctrl+R)' notice and degrades to manual refresh
  without failing the listing.
  Opening
  a file uses the existing row->`OpenFileMsg` path and therefore the remote `FileAccess`
  (save/revert already route to the authority). Writes on the remote tree: New File, New
  Directory, Rename and Delete are wired through `internal/app/remote_explorer_ops.go` over
  `authority.Files()`/`authority.Mutations(fs)` — an empty file is created with an atomic
  empty write after an existence check, mkdir/rename/delete go through the mutation helpers (a
  target that exists is refused, a remote delete is permanent because there is no remote
  trash), every operation runs the workspace trust gate first, a disconnected authority is the
  explicit "activate the window to reconnect" error, names use the same `fsops.Validate` rules
  and must stay inside the remote root, and the existing file-job refresh reloads the remote
  tree through its reload command. Confirmations state the remote target: the new-file/folder
  and rename prompt labels name it, and the delete confirmation says "Permanently delete ...
  on SSH:user@host:/path? Remote deletes have no trash." The clipboard is wired over the same
  authority surface (`internal/app/remote_explorer_copy.go`): cut (`Move`) and copy
  (`Copy`) walk the subtree through `Files().ReadDir`/`ReadFile` and write it through
  `WriteFile(Atomic: true)` with `Mutations.Mkdir`, bounded to 10000 entries, 256 MiB,
  64 directory levels and a 2-minute timeout; symbolic links, invalid names, paths outside
  the remote root, the workspace root as source and a destination inside the source are
  refused before anything is created. Copies are binary-safe; a cancellation or failure in
  the write phase keeps the partial destination and the notice says where it is (no
  rollback, no remote data deletion). Cut is copy-then-remove: the source is removed only
  after the copy fully succeeded (a failed removal keeps both and says so). A same-directory
  copy/duplicate gets Fresh's free name ("name copy.ext", "name copy 2.ext", the exact
  `fsops.CopyName` rule); a paste into another directory is REFUSED when the name exists
  (remote paste never overwrites, and the overwrite prompt is not offered). Every clipboard
  action asks first on a remote workspace and names the target ("Cut a.go on
  SSH:user@host:/path?"; paste names the destination and count); a local workspace keeps the
  old no-confirmation behavior. Every operation runs the workspace trust gate first, a
  disconnected authority is the explicit reconnect error, and the existing file-job refresh
  reloads the tree. The local project opener, preview and git-status ports stay
  refused/replaced as before (project search runs through the authority and project replace
  writes through it too: see the remote-project-replace bullet). Reveal/follow and
  re-expansion of unloaded ancestors work by reloading the waypoint directories and
  revealing the path in the new listing. Gaps vs Fresh's remote explorer (which runs the
  full file tree over the authority): drag/move (a drag between directories) is not wired,
  the clipboard has no undo/partial-cleanup UI, remote git marks stay inert, a
  directory symlink is shown as a directory (its target is listed through the symlink path),
  and a create/rename existence check has a check-to-write race (the FileMutations surface
  has no exclusive create/rename). Remote .gitignore files are read once per listing (the
  explorer applies an edit on the next reload triggered by the watch/refresh; the finder
  keeps its ignore batch for one index lifetime, so an edited .gitignore reaches the palette
  on the next index restart/reconnect). The directory watch is the polled authority watch
  (~500 ms plus debounce), so latency and the rename-as-remove+create approximation remain,
  and a watch the backend cannot provide degrades to manual refresh with one notice.
  Windows clients still share the root's `filepath` assumptions for remote POSIX paths.
  Tests: `internal/app/remote_ignore_test.go` (root/nested/negation/anchored rules mark
  explorer entries, the finder drops ignored entries, size/depth bounds degrade with one
  notice, a supported watch adds the loaded directories, an unsupported watch notices and
  adds none, a disconnected authority touches nothing, local decoy untouched),
  `internal/tui/remote_tree_test.go` (lazy
  expand/no-read-before-command, natural order, hidden filter, stale toggle drop,
  prune/keep-expansion, error+retry, reveal waypoints, read-only/hidden capabilities) and
  `remote_tree_model_test.go` (Update does no remote read; initial load and reconnect reload
  run in commands; disconnected notice keeps rows), `internal/tui/explorer/async_test.go`
  (async toggle is a command and the clipboard refusals/notices), `internal/tui/remote_ops_ui_test.go`
  (create/rename/delete available while cut/paste refused, remote context menu keeps writes +
  path copies and drops clipboard rows, remote confirmations name the target and the permanent
  delete, a successful write queues a remote refresh without reading in the handler),
  `internal/app/remote_explorer_test.go` (UTF-16-independent entry mapping, trust gate before
  reads, disconnected refusal, remote-only listing with a local decoy present, 5000-entry
  clear error), and `internal/app/remote_explorer_ops_test.go` (each create/mkdir/rename/delete
  success and failure, name validation, target-exists refusal, recursive directory delete,
  disconnected refusal touching nothing, trust gate before any mutation, unsupported
  move/copy, outside-workspace refusal, local decoy untouched).
- **MRU persistence in crash recovery and workspace extraction (landed).** The editor's
  document-identity MRU is already saved by the normal session writer (`Session.MRU`, additive
  `[]MruFile {Path, Label}`). Two paths wrote a session without it and now carry it too: the
  crash-recovery copy (`restore.RecoveryWriter`) records the active tab and the MRU order limited
  to the unsaved documents it stashes, plus the label of a pathless buffer, so `recoverCrash`
  restores the pre-crash completion/dabbrev order through the same `applySessionMru` mapping; and
  every workspace-extraction path (single/multi-tab/mixed into a new co-tenant, and a transfer
  into a dormant existing workspace) writes the source editor's MRU order restricted to the moved
  documents, newest first, with `mergeExtractMru` putting them before the target's existing refs
  and dropping a stale ref for the same path. A materialized target keeps the normal capture
  path. Old sessions and old recovery copies without the fields restore exactly as before
  (activation-derived order); stale/missing refs are skipped and the remaining order is kept.
  Tests: `internal/restore/recovery_test.go` (MRU+active round trip, pathless label, old copy
  without the fields) and `internal/app/workspace_extract_mru_test.go` plus the extract
  session/MRU assertions (new single/multi/mixed co-tenant, dormant merge, materialized capture).
- **Remote workspace recovery, trust and reconnect (landed).** Direct remote launches now
  have bounded hot-exit/crash recovery: the ordinary `restore.RecoveryWriter` stages the
  unsaved text of remote documents under a local staging session directory keyed by the
  remote target (`<cache>/gvim/remote-sessions/<hash>/session`, or the workspace session
  directory of a daemon-hosted remote workspace), and the staged session records the
  REMOTE paths. It is installed after `setupRemoteEditing`, so the next launch detects
  the copy and reopens those remote paths through the authority's `FileAccess` with the
  staged text; the notice names the remote target and a clean exit clears the staging
  directory. This is a deliberate local-staging divergence from Fresh, whose recovery
  routes through the active authority; no local file named by the workspace is ever read
  or written as remote content, and the writer is driven from the root's recovery command
  (no update-loop I/O). Remote workspace trust is a record in the same atomic trust store
  keyed by the remote target (user@host[:port]:workspace root) through the explicit
  record-key API, so a decision persists across restarts and a reconnect does not prompt
  again; local root keying is unchanged. Reconnect resilience reuses the existing
  invalidations/refreshes (authority watch Reconnected, remote finder Restart, git cache
  Invalidate, explorer reload and startup-open retry, LSP restart in a command) and now
  reports them as ONE notice: `SSH reconnected: remote workspace refreshed`, with the
  restarted-server count or the refresh failure appended. Tests:
  `internal/app/remote_recovery_test.go` (target-keyed dir, staged copy records the remote
  path, restart restores the remote tab/text through a fake authority, no local file, clean
  path), `internal/app/remote_trust_test.go` + `trust/record_test.go` (record keyed by
  target, restart/reconnect reads the decision, other target separate, local root keys
  unchanged, markerless/marked policy), `internal/tui/authority_restart_test.go` (one
  refreshed-workspace notice, no-hook fallback, failure named). Residuals: remote
  executable-content marker probing needs an early remote connection (not wired, so an
  undecided remote target starts markerless-trusted and records that), recovery copies are
  bounded by the existing per-document writer (a large document's unstaged overlay follows
  the existing cap), and a remote recovery copy is local text (the documented divergence).
- **Remote project replace (landed).** "Search and Replace in Project" now writes over a
  remote workspace through the authority instead of refusing: the root keeps the existing
  confirmation (the question names the remote target, e.g. `SSH:user@host:/path`) and runs
  the replacement in a command. `internal/app/remote_replace.go` reads each candidate file
  with `authority.Files()` (never the local disk), applies the same `search.Query`/
  `search.PlanReplace` semantics as the local replacer (case/whole-word/regex/capture
  groups, the panel's checked matches, `req.Limit`), and writes the result back through
  `FileSystem.WriteFile` with `Atomic` (temporary file in the same remote directory +
  rename, mode preserved). Safety: the workspace trust gate runs before the first remote
  operation; a file whose search-time hash no longer matches is skipped as stale, and a
  file whose size/mtime changed between the read and the write is skipped as changed
  (never written at a stale snapshot); per-file 4 MiB, per-run 32 MiB, 512-file and 20 s
  budgets bound the run, with context cancellation checked between files and passed into
  every authority call. Any read/write error aborts with a precise file-naming error and
  the count of files already atomically written; there is no cross-file rollback and no
  undo history for the direct writes (undo across a remote run is a recorded residual).
  Files open in the editor (dirty buffers) are skipped: their buffer text is not the remote
  file's text and writing under a live buffer is never safe; remote large-file filtering
  stays out of the candidate list (the remote search caps it first), and candidate ignore
  filtering follows the search-list scope above (including its tracked-file difference on
  the rg path). The local
  project-search preview and the local git-status adapter stay cleared for a remote launch.
  Tests: `internal/app/remote_replace_test.go` (remote multi-file write with a local decoy
  untouched, stale-hash skip, changed-mtime skip from the pre-write stat, write failure
  abort with the partial count, cancellation mid-run, trust refusal before any I/O, size
  and out-of-workspace bounds) and `internal/tui/project_replace_remote_test.go` (the
  remote confirmation names the target, the remote result applies no local edits, the
  notice carries written/skipped counts and a failure keeps the partial count).
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
- **Per-workspace plugin state (landed #19b slice; the shared runtime landed in the next bullet).** A
  daemon workspace's plugin runtime scopes `gvim.state` to that workspace: the state lives in
  `sessions/<stable-id>/plugin-state.json` in its session directory, one versioned (v1)
  JSON object `{"version":1,"plugins":{name:{key:value}}}` for all of its plugins. The app
  reads it in `buildRuntime` (never on the update loop) and the runtime seeds it when it
  starts; writes coalesce behind the existing 500 ms debounce into one atomic write and are
  flushed synchronously on Stop, i.e. when the workspace is closed or the daemon exits.
  Bounds: 1 MiB per plugin (same `maxState` as a direct launch) and 8 MiB for the whole file
  (an over-bound save is skipped with a warning, never truncated); the key cap stays 256
  bytes. A file whose version is newer than this binary is read-only: `gvim.state.set` returns
  an invalid error and the file is never overwritten (the runtime logs the reason once).
  Switching to another materialized workspace talks to that workspace's file; a fresh
  materialization restores it; closing a workspace flushes its own file and leaks nothing
  into another. Direct launches keep the editor-wide `plugin-data/<name>/state.json` files
  byte-identically, and the editor-wide parts (plugin settings/enablement and the manager's
  `plugin-state.json` toggles) stay shared for daemon workspaces. This slice namespaces the
  state a runtime writes; it does NOT merge the per-workspace runtimes. The editor-wide
  shared runtime plus Fresh's plugin-window-scope chrome model
  (refs/fresh/docs/internal/plugin-window-scope-plan.md: one editor-wide runtime,
  per-window `session_plugin_state`, chrome scope and the focus announcer that retargets the
  active window/split/buffer) remains the recorded follow-up residual.
- **Materialized-workspace bound and eviction (landed #19b follow-up).** A daemon keeps at
  most `daemon.materialized_workspaces` LOCAL workspaces warm (user-config only, default 8,
  accepted range 1..64; a project config that sets it is refused with a diagnostic because a
  repository must not decide the daemon's memory). The cap lives in `config.DaemonConfig`,
  reaches the manager through the runtime built for the foreground workspace (`buildRuntime`
  copies the config's value), and is enforced after a switch materializes a workspace: an
  eviction pass, scheduled as a command (never on the UI goroutine), picks the
  least-recently-focused non-active local workspace. It captures the workspace session first
  (the existing ordered capture writer; the snapshot runs on the UI goroutine), and if that
  write fails it confirms durability with the synchronous fallback save. Only then does it
  tear the runtime down (plugin wait cancellation via `Model.Close`, terminal/scrollback
  state, document handles, LSP/dock resources) and drop it from the materialized set - the
  registry entry, session directory and `sessions/<stable-id>/plugin-state.json` stay, so the
  next select materializes it from the saved session. The active workspace is never evicted
  (rechecked after the capture), and a workspace whose capture AND fallback save both fail is
  kept materialized, over the cap, and the failure is reported; the eviction pass then tries
  the next candidate. A successful eviction posts a status-row notice naming the workspace and
  the cap. A workspace being evicted refuses concurrent select/close instead of racing the
  teardown, and the exit save serializes with an eviction pass. Remote/authority workspaces are
  excluded from this slice: they own an SSH runtime, a rematerialize would have to reconnect,
  so they neither count toward nor are chosen by the local cap (documented residual). Tests:
  `internal/app/workspace_evict_test.go` (LRU order, active never evicted, unsaved workspace
  protected when capture and fallback fail, fallback rescue, reopen restores the unsaved tab,
  remote exclusions, plugin-state file preserved across eviction/reopen, cap from config);
  `config/daemon_test.go` (default, user layer values/bounds, project refusal).
- **Shared daemon plugin runtime and host mux (landed #19b follow-up).** One
  `plugin.Runtime` serves every LOCAL daemon workspace (`pluginhost.Daemon`): the first workspace
  build creates it from that build's config; every later workspace contributes only its
  plugin OS, editor adapter, keymap builder and state namespace (`pluginhost.Workspace`). A
  `pluginhost.MuxOS` routes the runtime's file/process/watch calls to the ACTIVE workspace's OS,
  so plugin spawns use that workspace's root, env and trust gate. The runtime's host is a
  daemon-wide mux (`tui.PluginShared`): one `pluginHost` whose `model()` reads the bound active
  model, so plugin UI/buffer calls route to the active workspace. On a switch the manager
  rebinds host and OS and calls `Runtime.SwitchWorkspaceState` (the leaving namespace is flushed
  as Work, the entering workspace's `sessions/<stable-id>/plugin-state.json` becomes the store).
  Editor-wide registrations - plugin commands, menus, key bindings/modes/contexts and status
  items - are snapshotted from the leaving model and installed into the entering one (with a
  keymap rebuild), so a plugin loaded once keeps its commands/keys/menus across workspaces.
  Chrome now has a per-mount scope (Fresh's model): a widget panel/sidebar mount declares
  "window" (the default - the workspace model that mounted it), "editor" (carried in the
  same leaving-model snapshot and installed into every entering model, where its pane is a
  fresh instance rebuilt from the mount spec) or "buffer" (the optional buf or the active
  buffer at mount time; parked while another buffer is focused and dropped when the buffer
  closes). The column, hit-test and panel focus only see mounts whose scope belongs on
  screen. The prompt/picker/popup overlays, per-buffer decorations/folds/groups/view state
  stay with the model they were created in. One focus announcer now owns
  active_window_changed and focus_gained: the paths that change focus call it, and it diffs
  (workspace label, focused split window id, active buffer id) so a split change reports
  exactly one transition. The buffer diff emits buffer_activated/deactivated and seeds the
  announcer from the same transition, so a window change never repeats the buffer pair.
  PluginShared.Bind seeds the leaving model's announcement so a workspace switch forces the
  entering workspace's window/buffer announcement. Widget focus remains widget_event. A
  workspace close does not stop the shared runtime (models skip pluginStop when bound to it);
  the daemon stops it once after closeAll, which flushes the active state namespace and shuts
  down every registered workspace OS. Direct launches keep one runtime per session
  byte-identically. Remote/authority-spec workspaces are excluded from sharing this slice
  (they keep their own runtime).
  Tests: `internal/tui/plugin_shared_test.go` (editor-wide registrations carried, chrome
  not, host retarget, no workspace stop), `internal/pluginhost/shared_test.go` (OS mux
  follows the active workspace, one runtime per daemon), `internal/app/workspace_plugin_shared_test.go`
  (one runtime for two daemon workspaces, host/state rebind on switch/back).
  Plugin window and buffer ids are editor-global across the shared runtime: on Bind a model
  gets a sequence in the shared namespace (direct launches stay sequence 0), the API exposes
  seq<<40|local, every host entry point decodes and refuses a foreign or released id with
  CodeClosed, returns/events use global ids, and PluginShared.ReleaseWorkspace frees an
  evicted/closed model's sequence so a rematerialized workspace gets a fresh one (plugins
  re-read ids after a remount; payloads still carry the workspace label). Residuals: the
  global namespace keeps 40 low bits for a workspace-local id and a per-mux sequence (far
  beyond the editor's/manager's real bounds, but a hard cap), editor-scoped overlays
  (prompts/pickers/popups) remain model-local (only widget panels/sidebar sections are
  carried), editor-scoped widget instance state is not cloned into inactive workspaces (the
  copy is rebuilt from the mount spec when entered, live changes reach it through an update
  after the switch), and a buffer-bearing mount cannot be editor-scoped across workspaces
  (its buffer id is workspace-local, so the carry skips it).
  The per-workspace PROJECT plugin config is
  applied on a switch: `pluginhost.NewWorkspace` snapshots the workspace config's
  `plugins.<name>.enabled`/`plugins.<name>.settings` layer (project layer included) with a
  canonical key; `Daemon.Activate` compares it with the runtime's current layer and, when it
  differs, calls `plugin.Runtime.ApplyWorkspacePluginConfig`, which re-decides only the known
  bundled/user candidates and schedules rescan Work only for the plugins whose enabled state
  or settings changed: a plugin the workspace's project config disables is unloaded (with its
  dependants), one it stops disabling loads again, and a loaded plugin with changed settings
  is reloaded; every other plugin keeps running. The reads/compiles are Work, the switch
  asks the entering model for one plugin pump (`RequestPluginPump` runs at the end of
  Update), and a project config NEVER writes the editor-wide `plugin-state.json` toggles
  (the project layer is a view, not a user choice). Bounded: one reload per changed plugin,
  no reload-all; the unload itself runs the plugin's shutdown handlers on the UI goroutine
  (the runtime's existing bounded unload path, <=200 ms total for all plugins).
  Cross-workspace plugin UI waits: `tui.PluginShared` keeps a token -> owning-model registry
  for shared-runtime daemons. A wait/prompt opened in workspace A stays registered to A
  across a switch to B (its overlay and wait table live in A's model); B does not see it and
  cannot reuse the runtime-global token (a duplicate is an invalid error), and the wait
  resumes/answers in A when A is shown again. Closing A explicitly resolves every wait it
  still owns with a `cancelled` completion and a warning notice, so a plugin task cannot
  wait forever on a prompt whose workspace is gone; direct launches have no registry (one
  model owns everything). Remote/authority workspaces still run their own runtime (while one
  is active the shared daemon runtime has no pump and is paused - queued plugin events/tasks
  resume when a local workspace is active again); the old namespace's pending state is flushed
  as Work on a switch, so a hard kill immediately after a switch can lose the last debounce
  window exactly like the direct-launch state file. Tests: `plugin/workspace_config_test.go`
  (only affected plugin reloads/unloads/loads, no global toggle writes),
  `internal/pluginhost/shared_test.go` (config-key diff asks for exactly one pump),
  `internal/tui/plugin_waits_shared_test.go` (wait stays with A across a switch, duplicate
  token refused, close cancels with a notice, B's waits untouched).
- **Extract tab to a co-tenant workspace (landed with registry v2).** The tab context
  menu's "Extract to New Workspace" (and the palette row "Extract tab to new workspace")
  moves the active file tab into a NEW workspace over the SAME project root, matching
  Fresh's `extract_tab_to_new_workspace`: registry v2's `CreateCoTenant` mints a fresh stable id
  and Fresh's numbered label (`proj`, `proj (2)`, ...), so the source and the extracted
  workspace are independent runtimes and session directories over one root. The UI
  goroutine captures the tab (path, cursor/top, page-view flags, virtual column, unsaved
  text) and returns a command; the command canonicalizes the root, creates the co-tenant,
  writes its session JSON with the tab's `FileState` and a stash of the unsaved text, and
  sets the registry layout. Back on the UI goroutine the switch is queued FIRST and the
  source tab is closed only after the switch was accepted: a refused switch leaves the
  tab (and its unsaved text) in place and drops the added co-tenant, while a failure after
  the queue keeps the new workspace (its session holds the text) and reports exactly that -
  the tab is never closed and then discarded. A second source capture after the close
  records the source workspace without the extracted tab, so switching back never
  resurrects a duplicate. The report is "Extracted <name> into workspace <label>".
  Only the large-file overlay and the no-file-path (welcome screen) cases are still refused;
  the tab is left untouched on every refusal. Round 16 (this slice) adds three variants on
  top of that path, with the same transfer-first, switch-second, close-last ordering and
  the same exact notices:
  - **Extract into an EXISTING workspace over the same root** (palette "Extract tab to
    workspace…" + target picker): a gvim extension — Fresh only creates new co-tenants.
    The picker lists the other materialized/dormant workspaces whose canonical root equals
    the source's (a different root is refused, nothing moves). A dormant target gets the
    tab's FileState merged into its saved session (stash written in that workspace's
    session dir, same path replaced/updated in place); a materialized target opens the
    file in its live editor through restore.RestoreSession (cursor/stash applied) and is
    re-captured, so the target's own session keeps it. Only after that acceptance is the
    switch queued and the source tab closed; a refused switch keeps the source tab (the
    accepted copy stays in the target and the notice says the tab is still open), and a
    failed post-close materialization says the text is kept in the target.
  - **Extract a terminal tab into an existing workspace over the same root** (palette
    "Extract terminal tab to workspace…"): a gvim bounded slice of Fresh's terminal
    extraction. Fresh moves the PTY machinery and the running process between windows
    in-process; gvim has no cross-runtime PTY hand-off, so it moves the dock tab's SAVED
    record instead (transcript, styled screen snapshot, launch/resume argv, launch dir,
    live flag) and closes the source dock tab (which stops the source shell) only after
    the target accepted. A materialized target appends a dormant restored panel to its
    dock (no process starts, the current tabs/active tab are unchanged); a dormant target
    merges the record into its terminal.json. The target revives the terminal with a new
    PTY on the user's first dive (the normal live-restore rule), so the running session
    itself does not continue — reported as "it revives on the first dive".
  - **Extract several selected file tabs at once** (palette "Extract tabs to new
    workspace…", multi-select picker): Fresh has no multi-tab extract; this is gvim's
    bounded extension, limited to the open FILE tabs (a terminal tab has the action
    above, an untitled/welcome tab has no file state, and a large-file overlay is
    refused). Space marks tabs in a centred multi-select list, Enter extracts the marked
    set (or the highlighted tab alone when none is marked). One new co-tenant over the
    same root is written with all FileStates and stashes first; then the switch is queued
    and every source tab closes through CloseTabForWorkspaceExtractAt (by path, not only
    the active tab) after acceptance. A tab that changed mid-action is skipped (its text
    is still in the target; the report says how many closed), a refused switch discards
    the just-created co-tenant and moves nothing.
  - **Mixed file+terminal multi-selection (round 18, task-193; gvim extension).**
    Fresh's only extract action still moves a single tab (file tabs and terminal tabs
    dispatch to the same action); the mixed selection builds on the gvim multi-select
    extension above. The palette "Extract tabs to workspace…" picker now lists the
    open file tabs AND the dock's terminal tabs, type-labelled ("file", "file ·
    modified", "terminal"). Enter opens a target chooser whose first row is a NEW
    co-tenant over the same root, followed by every existing same-root workspace
    (ExtractTargets); with no existing target the new co-tenant is created directly.
    A new target writes one session (all FileStates + stashes) and one terminal record
    (all SavedTabs) before the switch is queued, then closes the source file tabs by
    path and the source terminal tabs by identity (highest index first, saved-name
    checked) only after the switch was accepted, re-captures the source and reports
    "Extracted N tabs into workspace". For an existing target the command first
    detects conflicts in a READ-ONLY pass — a selected file already present in the
    target's session/open editor, or a non-empty terminal tab name already in its
    terminal record/dock — and sends ExtractConflictMsg BEFORE any write; the TUI
    confirmation names the target and every conflict and states the semantics: Yes
    replaces the target's existing file state and ADDS a same-name terminal tab
    alongside (it is never replaced), No cancels with all target records and source
    tabs untouched ("Extraction cancelled; nothing was moved"). After acceptance all
    files and terminal records are transferred (dormant merge or warm editor/dock
    append), then the switch is queued and accepted, then the sources close; an
    acceptance failure stops with "N of M tabs transferred, sources kept", and a
    source tab that changed meanwhile is skipped and counted in the report. The same
    ordering guarantees as round 16 hold throughout (transfer accepted -> queued
    switch accepted -> source close), and Update does no I/O (capture on the UI
    goroutine, disk/materialization in the tea command, conflict detection in the
    command). Limits/residuals: a large-file overlay, a welcome tab or any tab that
    closed during capture refuses the whole action; extraction is co-tenant-only (a
    target over another root is refused); no PTY is handed over (the target revives
    the terminal on its first dive exactly like the terminal action); default-named
    terminal tabs (empty saved name) are not part of the name-conflict check and can
    duplicate names; there is no undo, no drag-and-drop target and no cross-workspace
    shared buffers (#19b).
  The close-workspace flow asks first when the
  workspace still runs terminals: the confirmation lists every one of them and its agent
  (the dock's `RunningTabs` snapshot), and the close report and protocol close answer say
  exactly what was stopped; a dormant close reports that nothing was stopped. Rename
  reports the new label on the status row (the protocol answer stays the list only).
  Fresh's separate diff_nav plugin is unrelated; the per-tab "Extract to New Workspace"
  item also appears in Fresh's tab context menu.
- **Remainder of #19a (not shipped).** A remote workspace's `authority_spec` field (registry
  v2 still records no authority), full Fresh window-scope chrome/focus announcer per
  registration (task-141), sharing the runtime with remote/authority workspaces, byte-budget
  and per-runtime instance-state bounds beyond the workspace count for
  warm co-tenants, the initial terminal-launch record in a workspace descriptor (the
  discovery Import can create a workspace but not attach an argv to it yet), shared
  shared-buffer follow-ups beyond the bounded slice above (plugin/API-created tab sharing,
  daemon open-file owner switch, large/remote sharing; #19b),
  a workspace switcher that shows live runtime details (the list shows label/root/state
  only), and making the capture write itself cancellable/ordered under a slow disk beyond
  the quit wait. The extract flow still leaves these open: the cross-workspace
  full shared-buffer view independence (#19b; bounded slice landed) and a live PTY/process hand-off (gvim moves terminal state and
  restarts in the target, Fresh moves the PTY in-process), large-file overlay extraction,
  extracting to a workspace over a DIFFERENT root (refused: extraction is co-tenant only,
  matching Fresh's root-preserving extract), and all-tabs-
  of-a-file semantics (there is no such Fresh action; the multi-select picker is the
  bounded substitute). The target picker is a plain list: no drag-and-drop between
  workspaces and no cross-workspace undo, same as the rest of the workspace UI.
  Multi-client dispatch (the round-12 daemon hosts exactly one attached client) stays
  open.
