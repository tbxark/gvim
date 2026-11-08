# gvim package architecture

Status: design record for the package reorganization (`master`, after the layout move).
This file describes the package tree, what each package is for, and the import rules the
compiler and `migration/tools/import_rules_check.sh` enforce. `migration/ARCH-v4.md` is the
earlier proposal-only record and is superseded by this file where they disagree.

The goal of the layout is that a reader (or a coding agent) can tell from the import path
what a package is responsible for, where a new file belongs, and which direction a
dependency may point.

## 1. Layers

```
cmd/gvim/                       the executable: main → cli → app
cmd/gvimdebug/                  developer page: runs gvim in a PTY, serves the screen

internal/                       private executable shell (no top-level package imports it)
  app/                          composition root: Options, Run, buildRuntime
  cli/                          cobra command tree, flags, DIR / FILE:LINE:COL
  tui/                          Bubble Tea v2 root model + widgets
  terminal/                     PTY + screen model behind the terminal dock
  filewatch/                    fsnotify watcher (explorer reloads, project switches)
  uitest/                       test-only in-process UI harness

<top level>                     TUI-free engine, shared libraries and services
  buffer/ editor/ search/ syntax/ fold/ diff/ encoding/ session/ decorations/ textlayout/
  atomicfile/ shellcmd/
  workspace/ explorer/ finder/ fuzzy/ fsops/ gitstate/
  config/ keymap/ theme/
  lsp/ pluginapi/ plugin/ commands/
```

Dependency direction:

```
stdlib / third party
      │
      ▼
engine leaves  (buffer, textlayout, diff, encoding, fuzzy, theme, fsops, gitstate, lsp,
                pluginapi, decorations, atomicfile, shellcmd)
      ▼
engine (editor, search, syntax, session, commands)
      ▼
internal/tui  (depends on engine value types and the plugin contract, never on the shell)
      ▼
internal/app  (composition root: the only package that imports the concrete adapters,
               the TUI sub-packages and the plugin runtime)
      ▼
internal/cli → cmd/gvim
```

## 2. Import rules

`migration/tools/import_rules_check.sh` reads the direct imports of every package
(`go list`) and fails with a rule id and the offending edge. The rules:

| Rule | Statement |
|---|---|
| R1 | No top-level package (not `internal/…`, not `cmd/…`) imports `internal/…`. |
| R2 | `charm.land/*` (Bubble Tea, bubbles, lipgloss) is imported only by `internal/tui/…`, `internal/terminal` and `internal/uitest`. The plain `github.com/charmbracelet/x/*` helper libraries (`ansi`, `term`, `vt`) are not the UI framework and stay free. |
| R3 | Leaf packages import no package of this module: `buffer`, `textlayout`, `diff`, `encoding`, `fuzzy`, `pluginapi`, `decorations`, `theme`, `fsops`, `gitstate`, `lsp`, `atomicfile`, `shellcmd`. |
| R4 | Engine direction. `editor` does not import the UI, app, cli, plugin runtime/contract, lsp, finder, fsops, workspace, keymap, theme, config or commands. `search` does not import `editor`. `syntax` imports only `theme` among module packages. `session` imports only `buffer`. `commands` imports only `fuzzy` and `keymap`. `workspace` imports only `config`. `finder` imports only `explorer` and `fuzzy`. |
| R5 | The TUI tree does not import `internal/app`, `internal/cli`, the plugin runtime, `lsp`, `finder` or `fsops`; a TUI sub-package does not import its parent `internal/tui`. |
| R6 | `internal/cli` imports `internal/app` and no other package of this module. |
| R7 | Only `cmd/gvim` and `internal/cli` import `internal/app`; only `cmd/gvim` imports `internal/cli`; only `internal/app` (and `plugin/plugintest`) imports the plugin runtime; only `internal/app` imports TUI sub-packages. |
| R8 | No package in the shipped binary imports test support: `buffer/buffertest`, `plugin/plugintest`, `internal/uitest` (and the app test harness `internal/apptest`, when it exists). |
| R9 | `pluginapi` and `decorations` import no module package and no charm package. |
| R10 | The plugin tree imports only the standard library, `gopher-lua`, `hujson`, `pluginapi`, `plugin/bundled`, `plugin/pattern` (plugintest may also import the runtime and other plugin sub-packages). No charm. |
| R11 | `editor`, `buffer` and `decorations` do not import `pluginapi`. |
| R12 | Only `internal/tui` (its root and its own sub-packages) and `internal/app` import TUI sub-packages. |

Run the gates:

```sh
go build ./... && CGO_ENABLED=0 go build ./...
go vet ./...
golangci-lint run ./...
go test ./...
bash migration/tools/purity_check.sh        # PURE_GO_OK
bash migration/tools/import_rules_check.sh  # IMPORT_RULES_OK
```

## 3. Package catalogue

### 3.1 Executables

| Package | Role |
|---|---|
| `cmd/gvim` | `main`: signal context, `cli.Execute(ctx, args, stdout, stderr, app.Run)`, exit code. No logic. |
| `cmd/gvimdebug` | Developer page: runs gvim in a PTY, forwards keys and serves the screen over HTTP for scripted screenshots. |

### 3.2 Private shell

| Package | Role | May import |
|---|---|---|
| `internal/cli` | cobra command tree, flags, target classification (DIR vs FILE:LINE:COL through an injected `stat`), exit codes. | `internal/app` (Options only), cobra, stdlib |
| `internal/app` | Composition root: `Options`, `Validate`, `Run`, `buildRuntime`; wires engine + TUI + plugin runtime. Feature wiring that is not a generic adapter lives here. | every engine package and `internal/*` host package, TUI |
| `internal/tui` | The Bubble Tea v2 root model: routing, focus, layout, hit map, painting; interfaces in `ports.go`. | engine value types, `pluginapi`, `decorations`, its own sub-packages, charm |
| `internal/tui/*` | Widgets and panels (see 4.2). | kits (`grid`, `space`, `style`, `icons`), engine value types, charm |
| `internal/terminal` | Runs a program in a PTY and keeps a model of its screen (`Screen`, `Cell`, styles, cursor, paste). Backend of the terminal dock. | `creack/pty`, `x/vt`, `ultraviolet`, stdlib |
| `internal/filewatch` | fsnotify watcher with debounce and changed-directory coalescing. | fsnotify, stdlib |
| `internal/uitest` | Test-only in-process Bubble Tea harness (keys, mouse, screen cells, PNG). | charm, stdlib |

### 3.3 Engine and libraries

| Package | Role | Key facts |
|---|---|---|
| `buffer` | The text model: lines of runes, positions, edits, undo/redo, observer, large-file mode. | Two storage modes in one package: in-memory lines, and a disk-backed `bigStore` for large files (`OpenLarge`, `Snapshot`, `IndexScan`, `Rebase`). A split was considered and rejected: the mode is part of `Buffer`, not a layer on it (see `buffer/doc.go`). |
| `editor` | The editing session: documents, tabs, actions and their handlers, edits, cursors/motions, folds, marks, save/revert/autosave planning, shell jobs, virtual documents, the plugin edit seam. | Depends on `buffer`, `search`, `syntax` (indirectly), `textlayout`, `fold`, `diff`, `encoding`, `session`. No UI. |
| `search` | Find, scan, replace and range search over `buffer` lines. | `large.go` is the visible-window search (renamed from a misleading name). |
| `syntax` | Chroma lexer resolution, token spans for a visible window, markdown inline styling, style table from a theme. | |
| `fold` | Indentation fold ranges. | Small leaf used by `editor`. |
| `diff` | Line diff (go-diff) into hunks; used by the compare view and plugin services. | |
| `encoding` | Decoding/encoding detection and conversion (UTF-8/16, charsets via `x/text`). | |
| `session` | Session file format and stash files (open tabs, cursors, unsaved overlays). | The restore *policy* lives in `internal/app`. Writes through `atomicfile`. |
| `decorations` | Plugin decoration store: namespaced items per buffer, anchors that follow edits, per-line query. | Pure data, stdlib only. |
| `textlayout` | Terminal text geometry: indent width, indent guides, word occurrences, match columns. | Leaf; used by `editor`/`fold` and the editor pane. |
| `atomicfile` | Atomic file replacement: temp+sync+rename, mode/owner/xattr, symlink following. | Leaf; used by `editor` (save) and `session` (stash files). |
| `shellcmd` | Runs one external program with a timeout and output caps; the exec half of `editor/shell.go`. | Leaf; `editor` wraps it for shell-command actions. |
| `workspace` | Project root switching and the recent-folders list. | |
| `explorer` | File-tree model, `ListDir` with options, gitignore matching, async reload. | |
| `finder` | Project file index (`git ls-files`/walk) and usage ranking. | |
| `fuzzy` | fzf-style scorer shared by the finder, the palette and the pickers. | |
| `fsops` | Create, mkdir, rename, move, copy, trash with conflict policy. | |
| `gitstate` | Git work-tree root, branch and per-file status with go-git. | |
| `config` | Layered JSONC configuration (defaults, user, project), languages, formatters, plugins. | `hujson`. |
| `keymap` | Embedded presets, chord parsing and canonicalization, resolve, user overrides. | Data files `keymap/*.json`. |
| `theme` | Theme data (colors, syntax keys) from embedded JSON. | Data files `theme/data/*.json`. |
| `lsp` | Pure-Go LSP client and multi-server manager (JSON-RPC, transport, positions, diagnostics, requests). | |
| `pluginapi` | The plugin contract: plain types, `Host`/`Runtime` interfaces, error codes, `Unsupported`. | stdlib only; imported by `internal/tui`, `internal/app` and `plugin`. |
| `plugin` | Lua plugin runtime: sandbox, tasks and the yield protocol, budgets/strikes, `gvim.*` API installers, data libraries, loader/lifecycle, OS port. | `plugin/bundled` (embedded Lua), `plugin/plugintest` (fake host harness), `plugin/pattern` (Lua pattern matcher). |
| `commands` | The command registry and palette model: generated Fresh command table, palette query modes, availability rules, filtering. | Feeds the command palette; keymap-aware. |

## 4. Inside the big packages

The layout moved code between packages. The following maps cover the packages that are
still large internally, so a reader can find a feature without grepping by memory.

### 4.1 `internal/tui` root (file clusters)

| Cluster | Files | Responsibility |
|---|---|---|
| core / dispatch | `model.go`, `root.go`, `ports.go`, `doc.go`, `dialogs.go`, `key_hints.go`, `keybind_known.go`, `chord_keys.go`, notices, `status_expiry.go`, `cont_*` | `Model` + `Deps`, `New/Run/Close/Init/Update/View`, update pipeline, key translation/dispatch, overlay stack, painting, ports |
| splits / tabs / compare | `split.go`, `split_layout.go`, `split_resize.go`, `split_drag.go`, `split_tabs.go`, `tabs.go`, `tab_drag.go`, `tab_drop.go`, `compare*.go` | pane tree and geometry, tab strip, drag/drop, side-by-side compare |
| terminal dock | `dock*.go` (+ `shell.go` for editor shell commands) | shell dock: PTY lifecycle, key/paste routing, paint |
| LSP UI | `lsp.go`, `lsp_detect.go`, `diag.go`, `root_signature.go` | language-server UI controller, diagnostics, goto/hover/completion/code actions/rename, signature help |
| plugin host | `plugin_*.go` | the UI side of the plugin contract: pump, events, keys/modes, prompts, windows, virtual buffers, decorations, manager, init.lua |
| overlays / utilities | `keybind_editor.go`, `help_list.go`, `help_utility.go`, `notice_warnings.go`, `help.go`, `settings.go`, `icon_preview.go`, `tour.go`, `root_stash.go`, `language_pick.go`, `files_encoding.go`, `keymapeditor.go`, `keybind_ports.go` | the list/box overlay kit, keybinding editor, help/settings/tour/notices/pickers |
| frame chrome | `layout.go`, `welcome.go`, `hit.go`, `footer.go` | the single geometry source, hit map, welcome screen, footer hints |
| view / display | `view.go`, `view_render.go` | display settings and per-document view settings |
| find glue | `find_route.go`, `find_highlight.go`, `find_mouse.go` | controller between the find bar and the editor |
| save / files / watch | `root_revert_fix.go`, `save.go`, `watch_reload.go`, `autosave.go`, `files_revert.go`, `files_recovery.go`, `files_overwrite.go` | save/save-as planning, auto-save tick, disk polling, reload, recovery copies |
| root features | `macro.go`, `root_dump.go`, `root_ports.go`, `root_commands.go`, `root_project.go`, `root_watch.go`, `root_fix.go`, `root_sidebar.go`, `root_bookmarks.go`, `git.go`, `live.go`, `root_*_fix.go` | project switch/recents, watcher glue, git marks, macros, bookmarks, live grep, sidebar, root ports |
| editor body | `motion_mouse.go`, `motion_rows.go`, `viewrow.go`, `occurrence.go` | click/drag/wheel, row-mapped motion, top-row memo, occurrence highlight |
| large-file mode | `large.go` | large-file UI: index progress, full-load prompt, streamed save/search, error dialogs |
| clipboard / popup | `clip.go`, `context_menu.go`, `popup.go` | OSC 52/HTML clipboard flows, right-click menus, popup adapter |

### 4.2 `internal/tui` sub-packages

`grid` (cell surface on `x/ansi`), `space` (padding and text fitting), `style` (theme →
lipgloss styles; the only place that happens), `icons` (Nerd Font glyphs), `editorpane`
(paints the text area; pure renderer, no tea), `menubar` (menu bar + dropdowns; generated
menu table), `explorer` (file-tree panel), `palette` (command palette overlay), `find`
(find bar), `prompt` (bottom-row prompts), `browser` (file/folder browser), `picker`
(plugin list picker), `popup` (anchored list popup), `ctxmenu` (right-click menu),
`dialog` (modal dialogs), `statusbar` (status row primitives).

### 4.3 `editor`

| Cluster | Files (examples) | Responsibility |
|---|---|---|
| document model | `document.go`, `doc*.go`, `edit*.go`, `changes.go`, `virtual.go`, `docid.go`, `cursors*.go` | per-document state: text, cursor/carets, selection, folds, view settings, saved/disk state, identity, change listeners |
| editing commands | `textops.go`, `textcmd.go`, `vi_actions.go`, `comment.go`, `bracket.go`, `indent*.go`, `smart*.go`, `dabbrev.go`, `clip_ops.go`, `replace.go`, `findsel.go`, `paragraph.go`, `block.go`, `case.go` | the actions handlers over the document |
| motions / navigation | `motion*.go`, `nav*.go`, `goto.go`, `marks.go`, `scroll.go`, `fold*.go`, `view_ops.go` | cursor moves, viewport moves, marks, folds |
| files / save | `load.go`, `save*.go`, `savefile*.go`, `files_*.go`, `doc_revert.go`, `encoding.go`, `binary.go`, `readonly.go`, `autosave_async.go`, `interrupted.go` | load/save, atomic replace, revert, auto-save planning, encoding/EOL, crash interruption |
| large-file mode | `large*.go`, `save_large.go`, `doc_index.go`, `scanbudget.go` | streamed load/save, index, guard rails, stash |
| session shell | `editor.go`, `handlers.go`, `tabs*.go`, `status*.go`, `settings.go`, `shell.go`, `pending.go`, `paths.go` | `Editor`, action registry, tabs, status row text, keymap settings, shell jobs, pending prompts |
| plugin seam | `edit_api.go`, `docops_id.go`, `changes.go`, `virtual.go`, `lspedit.go` | id-based operations, bulk edits, per-consumer change streams, virtual documents, LSP edit application |

### 4.4 `buffer`

One package with two storage modes: an in-memory `[][]rune` model (`buffer.go`,
`lines_tree.go`, `history.go`, `snapshot.go`, edits and observers) and a disk-backed large
mode (`large_*.go`: source, index, pages, overlay, writes, rebase, budget). `Buffer.big`
selects the mode; every public method handles both. The split into two packages was
considered and rejected (see `migration/ARCH-v4.md` history and `buffer/doc.go`).

### 4.5 `plugin`

| Cluster | Files (examples) | Responsibility |
|---|---|---|
| runtime kernel | `runtime.go`, `plugin.go`, `task.go`, `sandbox.go`, `slice.go`, `handles.go`, `convert.go`, `limits.go`, `log.go`, `hostcall.go`, `ioruntime.go`, `require.go`, `timerpace.go`, `errfmt.go`, `bindings.go`, `lint_bindings.go` | `Runtime`/plugin state, Lua entry and yield protocol, tasks, budgets/strikes, heap guard, handle registry, Go↔Lua conversion, sandbox, `require` |
| `gvim.*` API installers | `api_*.go` (one per area), `bindings.go`, `api_modules.go` | the Lua surface; `api_modules.go` lists the installers in order |
| data libraries | `data_*.go`, `stdlib*.go` | `gvim.json`, `gvim.re`, `gvim.settings`, `gvim.text`, `gvim.diff`, `gvim.time`, `gvim.utf8`, and the repaired Lua 5.1 stdlib |
| loader | `discover.go`, `manifest.go`, `order.go`, `lifecycle.go`, `control.go` | discovery, `plugin.json`, dependency order, enable/disable/reload, `plugin-state.json` |
| OS port | `os.go`, `nopos.go` | file/process/watch interface used by `gvim.fs` and `gvim.proc`; the real implementation is in `internal/app` |

The `gvim.*` installers intentionally stay in the runtime package: they call 120 kernel
symbols directly, so a separate package would only fake a boundary.

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
| root `main.go` | `cmd/gvim/main.go` | Standard command layout; the module root is no longer a package. |
| engine packages under `internal/` | top level | The engine is TUI-free and is the stable core: `buffer`, `editor`, `search`, `syntax`, `fold`, `diff`, `encoding`, `session`, `decorations`, `textlayout`, `workspace`, `explorer`, `finder`, `fuzzy`, `fsops`, `gitstate`, `config`, `keymap`, `theme`, `lsp`, `pluginapi`, `plugin`, `commands`. The executable shell stays private. |

Rejected renames: `buffer` → `text` (2,028 `buffer.Pos` references, no functional gain);
`internal/app` (the conventional name for a composition root is good).

## 6. Big-package status and work ahead

The reorganization is staged; each step keeps `go build ./... && go test ./...` green.

Done:

1. Top-of-tree layout move and package renames (§5).
2. `main` moved to `cmd/gvim`.
3. Import gate rewritten for the new tree (R1-R12) and generator paths updated.

Next (tracked as follow-up work, not required for the layout to be correct):

1. `internal/app` split into host packages (`uiadapter`, `pluginhost`, `lsphost`,
   `restore`, `keybindings`, `highlight`) with `app` left as the thin composition root.
2. TUI kits: an `overlay` contract package, a `list` kit and a `space`/text kit, removing
   the root's wrapper adapters and duplicated box/text helpers.
3. Further TUI extraction once the kits exist: split tree/geometry, frame (layout + hit),
   keybinding editor, terminal dock, utility screens; the plugin host and LSP UI last.
4. `editor` → `document` + `editor` split (the two god objects), with an alias bridge.
5. Plugin leaf splits (`hostos`, `luastdlib`) and an `lsp/manager` split.

The analysis these steps build on (file clusters, symbol-level boundaries, risk
ordering) is in the session's analysis notes; the durable conclusions are kept in this
file when each step lands.

## 7. What enforces the layout

- The compiler: import cycles and illegal `internal/` imports fail the build.
- `migration/tools/import_rules_check.sh`: R1-R12, run in every gate.
- `migration/tools/purity_check.sh`: the shipped binary stays pure Go (no cgo).
- `go vet ./...`, `golangci-lint run ./...`, `go test ./...`.
- Generated code stays generated: `commands/commands_gen.go`
  (`python3 commands/gen/generate.py refs/fresh`) and
  `internal/tui/menubar/menu_table_gen.go` (`python3 migration/tools/gen_menus.py --check`).
