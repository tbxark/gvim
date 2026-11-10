# Porting

## From gvim's old JavaScript plugins

The goja/esbuild system is removed. A `.js` or `.ts` file in the plugin folder is **never run**: gvim
shows one notice (`N JavaScript plugin(s) ignored (gvim plugins are Lua now): a.js, b.ts`) and a
`legacy` row per file in **Plugin: Manager**. The old API was five functions:

| Old (JavaScript) | New (Lua) |
|---|---|
| `gvim.registerCommand(name, label, fn)` | `gvim.command.register{ name = name, title = label, run = fn }` |
| `gvim.notify(msg)`, `gvim.setStatus(text)` | `gvim.notify(msg)`, `gvim.status(text)` |
| `gvim.insertText(text)` | `gvim.command.run("insert_char", { text = text })` |
| `gvim.activePath()` | `gvim.buf.current():path()` |
| a `Promise` | not needed: `⏳` functions read like blocking calls inside a task |

## From Fresh's TypeScript plugins

gvim's API is **native Lua, not a Fresh shim**: names follow Fresh's events and concepts, but a port is
a rewrite in idiomatic Lua. The bundled plugins are examples of the technique (see
[bundled.md](bundled.md)). The map:

| Fresh (`editor.*`) | gvim |
|---|---|
| `getActiveBufferId`, `getBufferInfo`, `listBuffers`, `findBufferByPath` | `gvim.buf.current()`, `b:info()`, `gvim.buf.list()`, `gvim.buf.find(path)` |
| `getBufferText(id, start, end)`, `getBufferLength`, `getLineStartPosition(n)` | `b:text(range)`, `b:byte_length()`, `b:offset_of({line = n, col = 0})` |
| `compareBuffers(left, right)` | `gvim.diff.compare_buffers(left, right)`: same CompareResult fields in snake_case (`left_lines`, `left_len`, `left_version` = version token, `eol_only` always false) |
| `replaceRange(buf, start, end, text, expectedVersion)` | `gvim.diff.replace_range(buf, start, end, text, expected_version?)`: one edit and one undo step; `false` for the read-only/binary/out-of-range/stale refusals, exactly as Fresh |
| `getCursorPosition`, `getPrimaryCursor`, `getAllCursors`, `setBufferCursor` | `b:offset_of(b:cursor())`, `b:cursors()`, `b:set_cursor(pos)` |
| `insertText`, `deleteRange`, `replaceRange`, `insertAtCursor` | `b:insert`, `b:delete`, `b:replace`; typing semantics: `gvim.command.run("insert_char", {text = ...})` |
| `executeAction`, `executeActions` | `gvim.command.run`, `gvim.command.run_all` |
| `registerCommand`, `unregisterCommand`, `defineMode`, `setEditorMode`, `setContext` | `gvim.command.register/unregister`, `gvim.mode.define/enter`, `gvim.context.set` |
| `on("after_file_save", handler)` | `gvim.on("after_file_save", fn)`: the names are Fresh's; `buffer_id` becomes `ev.buf`, offsets become `Pos` |
| `setStatus`, `info`, `warn`, `error`, `debug` | `gvim.notify`, `gvim.log.*` |
| `prompt`, `startPrompt` with `setPromptSuggestions` | `gvim.ui.input` and `gvim.ui.set_prompt_suggestions` in its `on_change`, or `gvim.ui.picker` |
| `import { Finder, defaultFuzzyFilter, getRelativePath } from "./lib/finder.ts"` | `require "gvim.finder"`: `Finder.new`/`open`/`close`, `filter`/`score`, `tree`, `open_locations`, `read_snippets`, `relative_path` (see [The shared Finder](#the-shared-finder-port-residual)) |
| `showActionPopup` | `gvim.ui.action_popup` (`action_popup_result` event) |
| `addMenuItem` | `gvim.menu.add_item` / `remove_item` / `clear` |
| `t(...)`, plugin i18n files | `gvim.i18n.t` (`t` alias) + `<name>.i18n.json`; %-prefixed command titles are translated |
| `overrideThemeColors` | `gvim.theme.override` / `clear` / `overrides` |
| `getAllDiagnostics` | `gvim.lsp.all_diagnostics` |
| `addOverlay`, `clearNamespace`, `setLineIndicator(s)`, `setScrollbarMarkers`, `addVirtualText` | `ns:set` with `highlight`, `gutter`, `scroll`, `eol` items |
| `runOnTarget`, `spawnHostProcess` | `gvim.proc.run`, `gvim.proc.spawn` |
| `readFile`, `writeFile`, `readDir`, `fileExists`, `walkTree`, `watchPath` | `gvim.fs.*` |
| `delay`, `setTimeout`, `setInterval` | `gvim.timer.sleep/after/every` |
| `Promise.all` | `gvim.async.all` |
| `getGlobalState`, `setGlobalState`; `defineConfig*`, `getPluginConfig` | `gvim.state`; `gvim.settings.define` |
| `exportPluginApi`, `getPluginApi` | `gvim.plugins.export`, `gvim.plugins.get` |
| `dashboard.registerSection`, `registerSection` removal, `setAutoOpen` | `gvim.dashboard.register_section(name, refresh, opts)` / `remove_section` / `set_auto_open` / `auto_open`; callback returns bounded plain row data instead of drawing through a `DashboardContext`; dashboard owns timeout/TTL/failure isolation and row caps. Auto-open is a process-scoped override of the dashboard's `show_on_empty` setting (no persistence, no immediate open/close). No global `removeSection` (other plugins/built-ins), `clearAllSections` or `builtinHandlers` exposure; `sections()` lists live registrations. |
| `setFileExplorerDecorations(ns, …)`, `setFileExplorerSlots(ns, …)`, the two clears | `gvim.explorer.set_decorations(ns, items)`, `set_slots`, `clear_decorations`, `clear_slots`: snake_case items (`min_width`, `suppress_leading`, `suppress_trailing`, `name_color`, `suppress_name_color`), per-plugin namespaces as in Fresh, 10k entries/namespace and 64 namespaces/plugin; `FileExplorerTooltip` is not supported (no hover tooltips) |
| `computeLineDiff` | `gvim.diff.lines` (or `diff.buffer`) |
| `createVirtualBuffer*`, `setVirtualBufferContent`, `getTextPropertiesAtCursor` | `gvim.buf.create` plus `w:split` plus `b:set_content`, `b:props_at` |
| `beginKeyCapture`, `getNextKey`, `endKeyCapture` | `gvim.input.next_key` (no begin or end) |
| windows, orchestrator, authority, packages | not provided |

Widgets: Fresh's declarative widget API has a read-mostly v1 here (see
[api-ui.md](api-ui.md#widget-panels-and-sidebar-sections)): `mountWidgetPanel`/`updateWidgetPanel`/
`unmountWidgetPanel`, `widgetCommand`, `widgetMutate` and `mountSidebarSection` are
`gvim.ui.mount_widget_panel`, `update_widget_panel`, `unmount_widget_panel`, `widget_command`,
`widget_mutate` and `mount_sidebar_section`; the widget kinds are the interactive tier-1b set
(text, number, dropdown, radio, dual list and wrappers) and there are no floating/dock
placement controls.

Not provided at all (and not stubbed): the translated editor catalogues (the `locale`
setting, resolution and the English catalogue landed; plugin texts are translated; see
[api-core.md](api-core.md#gvimi18n) and [i18n.md](../i18n.md)), project-local plugins, a trust store,
synthetic buffer-group tabs and per-panel views of one buffer, a terminal API, Fresh's window and
authority runtimes, the package manager, the HTTP client API and `render_line`. The tier-1c
decoration surface is provided and painted (virtual lines, conceal, soft breaks, cursor-line
overlay, ranged scrollbar marks and plugin folds), gated by `gvim.has("decor_virtual_paint")`;
the 1c-C seams are provided too (split labels, per-plugin window state, open/focus/close split
wrappers and the path/focus/window/group events), as are `setLineTargets`
(`gvim.buf.set_line_targets`) and fixed-height group layout leaves (`{type="fixed",height=n}`,
height 1..100, re-applied on layout changes; the split border can leave it one row off).
Declared plugin settings (`gvim.settings.define`) are exposed read-only to the host and the
Settings page with their schema and effective values.

### What changes when you port

* **No Promises.** A ⏳ function suspends the calling task. `await foo()` becomes `foo()`;
  `Promise.all([a(), b()])` becomes `gvim.async.all{ a, b }`; `new Promise(resolve => ...)` becomes
  `gvim.async.suspend(function(resume) ... resume(v) end)`. Handlers are Lua functions, not names.
* **Positions.** Fresh uses byte *offsets* into the buffer; gvim uses `Pos{line, col}` (0-based line, 0-based
  byte column). `b:offset_of` and `b:pos_of` convert, on the LF-normalised text (CRLF files differ from
  Fresh's raw offsets). `line_text:sub(col + 1)` is the idiom.
* **Writes are synchronous** (no `flush`): the next read sees the edit. Every `b:edit` call is one undo
  step.
* **Errors are values**: `nil, "<code>: <message>"`, not exceptions.
* **Diff algorithm**: gvim diffs with Myers', not patience, so hunk boundaries can differ from Fresh's gutter.

### Live diff: no host baseline (port residual)

Fresh's `live_diff` diffs against a host-registered baseline (`registerDiffBaseline` plus a
patience `diffAgainstBaseline`, with `getBaselineLines` for the old text). gvim has no baseline
API and this port does not ask for one. Recorded differences:

* The old side is fetched by the plugin: `git show <rev>:./<file>` (HEAD/branch, decoded with the
  buffer's encoding) or `gvim.fs.read` + `gvim.text.decode` for disk mode. No repository, an
  untracked file or a missing file leaves the buffer unmarked; large-file mode is refused with one
  notice instead of diffed.
* The diff is computed with `gvim.diff.buffer` (Myers). Hunk boundaries can differ from Fresh's
  patience baseline, and the word-level refinement (char-LCS similarity, word LCS, caps) runs in Lua
  with a DP-cell budget instead of natively.
* gvim decorations have no `fgOnCollisionOnly` mode, so the diff line backgrounds use the theme's
  `editor.diff_*_bg` keys only (no `editor.diff_*_collision_fg`).
* Virtual rows with the deleted old content and their word emphasis use tier-1c virtual-line
  painting. The shipped TUI reports `gvim.has("decor_virtual_paint")` (the paint surface is
  provided), so the full Fresh view is
  what users get: one `virtual_line_above` row per deleted line with the `-` gutter glyph and
  bold+underlined removed-word `text_overlays`, in Fresh's priority order and cut to the host's
  64-rows-per-anchor cap plus an "… N more deleted lines" summary row. On a host without the
  capability (an older host or the reduced test path) it falls back to line backgrounds,
  new-side word emphasis, gutter indicators and scrollbar marks.
* Rendering caps are Fresh's, made host-aware: full → no-vlines (more than
  `max_virtual_lines` deleted lines) → outline (more than `max_overlay_lines` changed lines), and
  a deletion block longer than the host's 64-virtual-rows-per-anchor cap is summarised by an
  "`… N more deleted lines`“ row.

### Code tour: recorded-ref drift

Fresh's `code-tour.ts` computes `commitDrift` on open: a tour's recorded commit hash is compared
with `git rev-parse --short HEAD`, and a mismatch is shown in the panel header/prose. The port
parses code-tour's `commit_hash` and VS Code CodeTour's seven-to-forty-digit hex `ref` (branch and
tag refs are ignored, matching Fresh), resolves the repository from the active file or workspace,
and runs the bounded, neutralized `gvim.git` read; missing git/repository or a failed command is
silent. Loads carry a generation: a drift lookup that completes after another load or an exit is
discarded, so it cannot mount a stale tour. Tests: `code_tour_test.go` covers a mismatched
recorded ref showing the warning and a matching HEAD prefix showing none.

### Streaming a process into a buffer

A view that streams a command's output (a patch, a log, a diff) does not need a file-backed buffer
and `b:revert()`; a virtual buffer the plugin owns can be appended to as the data arrives. `git_log`
is the reference port:

* Create the panel buffer with `gvim.buf.create` (read-only), put it in the layout/tab where it
  belongs, and start the command with `gvim.proc.spawn` (not `run`, which waits for the whole
  output). Arguments are validated argv arrays; pin the format flags and the environment as the
  bundled `gvim.git` module does, so repository configuration cannot change the format the parser
  reads (or run a program).
* In `on_stdout`, append the chunk: `buf:set_read_only(false); buf:append(chunk);
  buf:set_read_only(true)` (a read-only virtual buffer refuses the append; the flag is
  plugin-facing). Do the incremental parse on the same chunk, carrying the partial last line across
  chunks and capping a single line's parse cost.
* Style rows as they arrive with one `ns:add` per run, not one per line: a `highlight` decoration
  whose range spans N equal-kind lines costs one host call for the whole run.
* **Ordering.** The process's exit callback may arrive before the queued `on_stdout` tasks of the
  same process. Finish one tick after `on_exit` (`gvim.timer.after(1, finish)`), or the last chunks
  would be dropped and a cache marked complete too early.
* Bound it: a byte cap and a line cap, kill the process when either is passed and append a footer;
  a generation token bumped on every new selection/refresh/close makes late callbacks from the
  killed stream no-ops. No whole-buffer read is needed to fill the view.
* Cache by content identity (a commit sha, a baseline id): stream into a file under
  `gvim.dirs.data()` while appending to the buffer, and write a small `.done` marker last, only on
  a clean exit. A cache hit becomes one bounded `gvim.fs.read` plus `buf:set_text`; an interrupted
  stream has no marker and is regenerated.
* `b:revert()` remains the right tool for a **file-backed** buffer (open the path, rewrite the
  file, revert); it costs a tab and a disk round-trip, so the append pattern above is preferred for
  a live view that is not a real file.

### Verifying a default-off bundled port in the real app

A bundled plugin with `default_enabled: false` is exercised by the fake host in its own
package tests, but its real-host path is worth one check because the enable path is config
driven. The procedure (and why it matters):

1. Isolate the config, then enable the plugin in the **user** config:
   `{"plugins": {"<name>": {"enabled": true}}}`.
2. Start the app session and wait for the plugin manager to report the plugin `loaded`; then
   let the session settle.
3. Open a buffer, then open the palette and query the plugin's command titles. The palette
   table is built from the loaded-plugin command table **when the palette opens**, so a
   palette opened before the load settles has no plugin rows and does not gain them while it
   stays open. The plugin's init log can appear while the command table is still settling,
   which makes "init log present, palette row missing" the signature of querying too early,
   not of a config-locked enable defect.
4. Run one of the rows and assert its real effect (the flash, git_log and live_diff ports
   each have such a smoke test; `internal/app/plugins_ports_palette_test.go` covers the
   config-locked enable → palette → command-run chain end to end).

### Path completion needs a built-in-prompt seam (port residual)

Fresh's `path_complete` is passive: it answers the editor's `prompt_changed` event for the
built-in Open File and Save File As prompts with directory suggestions. gvim has neither a
`prompt_changed`-style event nor a way to attach `gvim.ui.set_prompt_suggestions` to a prompt the
plugin did not open: Open File/Switch Project use the native file-browser overlay and Save As uses
the Save-As dialog. The bundled port keeps the completion engine (the exported `complete(input)`
plus a Path Completion prompt command that exercises it) and documents the gap; a future host seam
would emit a prompt-changed event for the browser/dialog inputs, and route the plugin's suggestion
list into those overlays, after which the port can drop its command and match Fresh exactly.

### Compare files: group-lite splits (port residual)

`compare_files` (port of Fresh's `compare_files.ts`) compares through group-lite splits of the
current tab showing the two existing file buffers — there is no synthetic group tab, so the title
segments are stored but not painted. The two files are opened as ordinary tabs when they were not
open (gvim has no tabless view of a real file buffer); **Compare: Close** closes the ones it opened
when they are unmodified, while a file that was already open keeps its tab. Line-ending-only hunks
do not exist because gvim normalises a buffer's line endings (`eol_only` is always false, so there
are no CRLF/missing-newline notes). Filler rows need the host's virtual-line painting
(`gvim.has("decor_virtual_paint")`); without it the panes are level only where both sides have lines.
Lines are paired positionally inside a hunk (Fresh's documented fallback), the command bodies ask
for paths with `gvim.ui.input` instead of Fresh's file picker, and there is no `fresh --diff` launch
hook or script-facing export (the native Compare view handles `--diff`). Enabling the plugin adds
palette rows that duplicate the native Compare commands, which is why it is off by default.

### Live agent sessions: native discovery, no orchestrator (port residual)

Fresh's `live_sessions.ts` is an orchestrator-dock feeder: it polls Claude/Codex local and cloud
listings, merges the answers, and calls the orchestrator's `setExternalSessions`; the orchestrator
draws the rows and owns opening/attaching. gvim has no orchestrator, cloud sign-in reader, or
process attach/resume, so the port is a picker over the native agent discovery that already exists
(`internal/agentdiscover`): tmux sessions and local Claude Code/Codex transcript history. The app
exposes one bounded, read-only service to plugins (`gvim.agent_sessions.discover`, through
`pluginapi.Services.DiscoverAgents`); it applies the same spawn trust gate and runs in a Work, never
on the update loop, and carries no process handle or attach/resume argv. The plugin polls every 60
seconds while the picker is open, coalesces refreshes, cancels the timer and discards stale answers
after close, and caps rows at 200 (the native scan's own caps apply). A close/reopen during an
in-flight scan leaves the old single-flight scan running; the reopened picker marks a refresh
pending, and the stale completion consumes it and starts the new scan immediately rather than
leaving the new picker in Scanning until the poll tick. Enter opens a transcript file;
tmux rows are informational (no attach). Shared daemon runtimes intentionally leave the service nil
because their scanner/trust root is per workspace, and remote launches are unsupported; the plugin
shows a real unsupported state instead of an empty scan. Not ported: cloud sessions, orchestrator
groups, attach/resume/orchestration, vendor CLI polling, and the Settings/Menu toggles. Tests:
`live_sessions_test.go` covers listing, transcript open, unsupported/empty states and polling;
`agent_sessions_api_test.go` covers the report shape and unsupported service.

### Audit mode: read-only review (port residual)

Fresh's `audit_mode.ts` is a magit-style review session: a buffer-group split with a file
sidebar, unified and side-by-side diff streams, staging/unstaging/discarding, hunk and line
comments, comment/note persistence, Markdown/JSON export, commit/range/stash reviews,
side-by-side composites and an external-change watch. The gvim port only implements the
read-only core that the existing APIs can make real: `Review Diff` gathers
`git status --porcelain -z -uall` and staged/unstaged `git diff --numstat` through the hardened
`gvim.git` gateway, lists the files and stats in a picker, opens one file's unified diff in a
read-only virtual buffer, and turns new-side diff rows into line targets that open the working
file at that line. `Review Diff: Next Hunk` / `Previous Hunk` move the diff cursor through the
`@@` rows and cross file boundaries in the review's ordered file list (next file first hunk /
previous file last hunk), matching Fresh's global `n`/`p` for the per-file diff views this port
builds (no default key; palette commands). They require the review diff to be the active buffer, so
a command run from another tab reports the mismatch rather than moving a hidden diff; closing the
diff/navigating away clears the hunk table. A cross-file candidate's git/untracked read is bounded
(200 candidates, normal diff caps) and checked against the diff generation and active buffer before
it can open, so close/reopen, file selection, tab changes or picker Esc discard it; stale same-file
diff loads are also generation-discarded. A truncated candidate opens but announces the truncation;
the 200-file bound and candidate read failures have their own notices and are never reported as a
global first/last hunk. `Review Diff: Refresh`
re-reads the list; `Review Diff: Close` drops it; untracked files are shown from a bounded
working-tree read as a synthetic new-file diff.

All reads are bounded (status/stat/diff byte caps, file/line/target caps), run in plugin-task
awaits, never in Update, and the trust-gated plugin process gateway applies. A close invalidates
an in-flight answer by generation (including an untracked file's working-tree read); the process is
not cancelled. The truncation footer is cleared on every refresh result, and numstat is keyed by
category+path so a both-sides (MM) path shows distinct staged/unstaged stats and total additions.
OPEN: stage/unstage/discard
(index writes), comments/notes and persistence, exports, range/stash modes, side-by-side/BufferGroup
layout, collapsing/filtering, watch polling, status-bar chrome and the review-mode keymap. Tests:
`audit_mode_test.go` covers the staged/unstaged/untracked overview with numstat, opening a
tracked file's diff and its new-side line target, next/previous hunk movement, cross-file hunk navigation, active-tab refusal, close/reopen hunk
replacement, stale same-file and cross-file diff loads, truncated candidates, the 200-file bound,
candidate read failures, picker-Esc cancellation, the untracked synthetic diff, the clean and not-a-repository states, and the read-only command list.

### Dev containers: read-only config inspection (port residual)

Fresh's `devcontainer.ts` is a full devcontainer integration: it detects and parses the config,
shows a status-bar summary and info panel, runs lifecycle commands, drives the `devcontainer` CLI
for attach/rebuild/detach, streams container/build logs, forwards ports, reacts to failed attaches
and offers attach prompts/scaffolding. gvim has no container runtime or attach surface, so the port
keeps only the real read-only part: discovery in Fresh's three-location priority order, JSONC
parsing with the bounded host parser, a read-only virtual info panel (image/build/compose, features,
ports/attributes, container+remote env, lifecycle commands, users, mounts, host requirements), an
Open Config command and a Refresh that re-detects and refreshes an open panel. Config reads go
through `gvim.fs` (workspace authority on remote workspaces), are capped (1 MiB config; bounded
features/ports/env/lifecycle/line counts) and run as plugin-task awaits, never Update. Invalid JSON,
unreadable/oversized files and missing configs are reported as notices; the port never runs docker
or the devcontainer CLI, never attaches/builds/runs lifecycle commands, forwards ports, streams
logs, shows a status-bar summary, or claims the editor is in a container. OPEN: all execution and
container state, status bar, attach/rebuild/logs/ports, prompts/scaffold, and Fresh's other modes.
Tests: `devcontainer_test.go` covers JSONC + info sections, priority order, Open Config (with no
process spawned), missing/invalid config, and the read-only command set.

### Kubernetes workspace: config inspection only (port residual)

Fresh's `k8s-workspace.ts` loads `.fresh/k8s.json`, resolves a target through one of four providers
(`attach-existing`, `manifest`, `run`, `command`) by spawning kubectl or the user's command, checks
`create pods/exec` RBAC, probes the in-pod environment and calls `attachRemoteAgent`; Disconnect
runs the provider teardown. gvim has no kubectl/attachRemoteAgent/orchestrator integration, so the
port is read-only configuration inspection only: JSONC parsing through the host parser, validation
of `targets`/`defaultTarget`/provider shapes, and a virtual panel showing cluster context,
namespace, manifest/run/command settings, workspace, vars and `confirmCreate`, plus an Open Config
command and Refresh. It never executes kubectl or a provider command, creates pods, probes RBAC, or
claims a connection. Reads use `gvim.fs` (workspace authority on remote workspaces), cap the config
at 1 MiB and target/vars/error counts, and run in plugin-task awaits, never Update. Invalid JSON,
read failures and shape errors are surfaced in notices and the panel. OPEN: connect/disconnect,
all four provider resolutions, RBAC/env probes, pod/container selection, attachRemoteAgent and
teardown. Tests: `k8s_workspace_test.go` covers valid JSONC targets/providers, validation errors and
invalid JSON, Open Config with zero process runs, missing config, the read-only command set, and
the inclusive 100/101 vars and 100/101 targets boundaries (the 101st entry is the error; exactly
100 is accepted).

### Package browser: metadata only (port residual)

Fresh's `pkg.ts` is a git-based plugin/theme package manager: install from URLs or a curated
registry, update/remove, version pinning, registry sync, lockfile generation and install, and
package actions. gvim has no such package store or git/network surface, so the port is a read-only
browser: `Package: Packages` reads `gvim.plugins.list()` (extended with manifest description,
requires, file/dir and enabled metadata) and `gvim.theme.all()`, groups INSTALLED/BUNDLED plugins and
THEMES, and Enter opens a virtual detail panel with name/type/version/status/source/path/description/
dependencies. Themes are native gvim themes, not package-manager theme packages, and are labelled
that way. The two metadata APIs are synchronous and cheap; the command schedules the list build as a
plugin async task so Update never blocks, the list is bounded to 1000 rows and clipped strings, and
no filesystem or process I/O is involved. OPEN: install/install-from-URL/update/remove,
registry sync, lockfile, outdated/available registry rows, git clone/pull, and packaged
theme/language/bundle management. Tests: `pkg_test.go` covers installed/bundled rows and alpha's
manifest detail (description/requires/path/status), native theme rows/details, the read-only command
list and zero process runs; `api_plugins_test.go` covers the extended list metadata.

### The shared Finder (port residual)

Fresh's `plugins/lib/finder.ts` (1862 lines) is split in three in the bundled `gvim.finder` library
(`plugin/bundled/lib/finder.lua`): Fresh's fuzzy ranking with hard caps; a prompt frontend over
`gvim.ui.picker` (`filter = "none"`, the library ranks in Lua so custom orderings such as symbols by
source line survive, and `on_close` runs exactly once with `selected` / `cancelled` / `programmatic`);
and a panel backend (`tree`) plus a locations finder (`open_locations`: relative `path:line` rows,
snippet descriptions from open buffers or one bounded read, preview tabs on move, commit on Enter,
cancel closes the preview). The filter source loads once and is ranked per query; a **search
source** (`opts.search`, Fresh's `mode = "search"`) is called from the picker's `on_query` callback
task with the debounced query (so it may wait on a process, as `git_grep` does) and its rows are
capped, with `min_query`, an `on_rows(shown, total)` status hook and `set_footer`/`set_status`/
`refresh` helpers. `diagnostics_panel` uses the tree backend and requires the library lazily on its
first build, so the idle default bundle does not pay for it (the 8 MiB gate).

Reduced or omitted from Fresh's Finder: search sources have no cancellation handle (a superseded
query's `set_items` is discarded by the picker generation guard instead of killing the process), no
per-query process spawn management, the embedded preview pane and utility-dock routing,
`navigateOnCursorMove`, `panelKeys` and the panel-mode prompt-type events; the panel backend covers
the grouped/deduped tree use only. `diagnostics_panel` implements Fresh's `syncWithEditor` itself
from the existing cursor_moved event: the exact active-file/line diagnostic is selected/revealed
through a widget mutation (no preview, no focus change). It also implements
`navigateOnCursorMove`: a 90 ms debounce (Fresh's Finder.PREVIEW_DEBOUNCE_MS) opens the latest
selected location with `gvim.buf.open(..., preview=true, background=true, focus=false)`,
leaving the panel focused and the host's single preview tab replacing the previous one; closing
the panel, activating a row, or refreshing the rows cancels the pending timer. The tick also
checks that its captured row is still the row at the current selection index in the rebuilt node
map, so a refresh that removes/moves the diagnostic cannot open its stale location. As with any
plugin `buf.open`, an open request already issued to the host is not cancellable. `lsp_navigation` and `find_references` add the LSP edges:
`gvim.finder.lsp_locations` normalizes `Location`/`Location[]`/`LocationLink[]` and converts UTF-16
columns to bytes when the target line is open. Both are command ports because gvim has no
`lsp_references`/`lsp_implementation` plugin events (the native Find References action has its own
UI); `lsp_navigation` also offers definition/implementation/type-definition list commands that are
gvim additions over Fresh's core direct-jump actions. Fresh's enclosing-symbol preselection is
approximated: the smallest containing symbol (same span/start/character tie-break) is placed first
for the empty query and previewed immediately. gvim's picker API has no initialSelectedIndex, so
the selected row is established by first-row ordering rather than an index; typed queries return to
the normal line sort.

### Git status bar: the OS-focus backstop (port residual)

Fresh's `git_statusbar.ts` rereads the branch on `focus_gained` because a `git checkout` in
another terminal can happen while the editor is unfocused. gvim's `focus_gained` is the different
plugin-chrome keyboard hand-back event, so the port subscribes to the documented
`window_focused` OS-focus event instead (and the runtime's known-event allowlist had to accept
that documented event; `window_blurred` is accepted for symmetry). The refresh is forced because
the HEAD watch may have missed a change while unfocused, runs through the same bounded git branch
read/path, and coalesces with the existing `running`/`again` guard. No new watcher, process,
trust path or cancellation handle is introduced. Tests: `git_statusbar_test.go` rescripts the
branch, emits `window_focused`, and asserts exactly one new symbolic-ref read and the new label;
`events_api_test.go` asserts both window events are subscribable.

### Git blame: inline parent stepping, no panel (port residual)

Fresh's `git_blame.ts` is a magit-style panel with a scrollable parent-file view, a history stack and
b/q navigation. gvim keeps the reduced inline blame described in bundled.md and now adds a one-step
history slice instead of pretending the panel exists:

* **Git: Blame Previous Revision** resolves the first parent of the blamed commit through the existing
  safe `gvim.git.run` wrapper (same neutralized git settings/authority as every git read), then
  re-blames only the current cursor line at that revision using the existing 40-line `git blame -L`
  path. **Git: Blame HEAD** clears the revision override. The annotation itself is the normal inline
  EOL decoration; the cache key includes the revision, so HEAD and parent entries do not mix;
* the history view is inline and one-step at a time, not Fresh's panel: there is no parent-file text
  view, scrollable blame body, b/q stack or sidebar. An unsaved buffer is refused (its unsaved lines
  cannot be mapped to a committed parent); a modified buffer that is already showing a parent
  revision is likewise refused until saved/reverted;
* no new trust path or blocking work: one bounded `rev-parse --verify <commit>^` plus the normal
  bounded blame call per step, through the same plugin task/authority path. Root commits or unreadable
  parent results are a warn notice; no history commands are claimed when git is unavailable;
* async safety: `blame_previous` captures the buffer id/path/version, cursor line, viewed revision and
  a generation before the first await, re-validates all of them after every git yield, and only then
  sets `view_rev`; an off toggle, tab/cursor/buffer change or revision change discards the step
  silently. The viewed revision is stored with its owning path: Save As invalidates it, bumps the
  generation and returns the annotation to HEAD for the new path instead of blaming the old parent
  history. `blame_head` and both history commands refuse while blame is off and never seed the next
  enable. `update` captures the same state, re-checks the revision/generation after its blame await,
  and does not paint/clear a stale result. `Show Commit`/`Copy Commit Hash` use the same snapshot and
  discard their target if the buffer/path/cursor/state changed; Show Commit also re-checks after its
  commit-read await. Closing a buffer increments the generation and drops its revision/path state.

Tests: `plugin/bundled/git_blame_test.go` covers the HEAD→parent→HEAD inline transitions, the parent
notice, a missing-parent refusal that leaves the HEAD annotation intact, the unsaved-buffer refusal,
and the existing Toggle/Show/Copy tests; the parent blame fixture is a real fake-git porcelain
response, not a stubbed plugin result. Delayed-runner regressions gate a real git `Run` and switch
state while it is in flight: cursor motion during the parent resolve must abort without a success
notice, an off toggle during the step/update must not paint or seed parent state, and history commands
invoked while off must leave the next enable at HEAD. Save As after a parent step must re-blame HEAD
on the new path; a cursor change during the delayed commit-read of Show Commit and during Copy Commit
Hash's blame read must drop the popup/clipboard result.

### Git grep: no prompt toolbar (port residual)

Fresh's `git_grep` uses a prompt **toolbar** (`setPromptToolbar` with a Case toggle widget) and drives
the toggle from `widget_event` / `toggleOverlayToolbarWidget`; gvim's prompt is `gvim.ui.picker` and has
no toolbar or in-prompt key surface. The port therefore keeps Fresh's module-level `case_sensitive`
choice (insensitive by default, like Fresh's effective config preset — gvim plugins cannot read
`editor.search.case_sensitive`), shows it in the picker footer/hint line, and flips it with Fresh's
second palette command (`Git Grep: Toggle Case Sensitive`), which refreshes an open prompt and
otherwise applies to the next search. Fresh binds no chord for the command, so the port registers none.
Fresh also disables the Finder's split preview because its floating overlay draws a preview band;
gvim has no overlay preview band, and the port opens no preview tabs either (Enter opens the hit).
The `git grep` process runs through the hardened `gvim.git` gateway (`grep -n --column -I [-i] -e`,
15 s timeout, 2 MiB output cap, first 100 rows, per-line caps); exit 0 and 1 are both success (git
grep exits 1 for no matches), >= 2 is the error status, and paths with colons still parse because the
line/column numbers anchor the match from the left.

### Environment activation is host-managed (port residual)

Fresh's `env_manager` plugin owns detection and activation. In gvim both are host features
(`env.detectors` in the user config, the `gvim.env` runtime API, the trust gate in the core trust
dialog), and the bundled plugin is the thin command/status layer: Activate a detected environment,
Reload after a `.envrc`/`mise.toml` change, Use System, and Status (the `env` status-bar pill shows
`system` / `<name>` / `<name> ✓` / `<name> (locked)`). Reductions: no widget-panel UI yet, no file
watcher (Reload is explicit), no devcontainer deferral/authority handling, no automatic
shell-environment activation (the command is explicit), one local environment, and no persisted
recipe; a bare `.venv` without an interpreter does not count and first match wins, as in Fresh.

### Line targets: what they can and cannot express

`gvim.buf.set_line_targets` maps a *buffer line* to a file path and 0-based line: pressing plain
Enter there (normal mode, count 1, no selection or extra carets) or a single left click opens that
location, in the split labelled `into` when one is live, else beside the pane holding the targets.
A target can also be an action: `{line=, action="<plugin>.<name>"}` runs a command the setting
plugin registered (through the normal dispatcher; another plugin's command is rejected), which
is how the dashboard makes its action rows clickable. Targets still cannot open arbitrary
commands, virtual buffers or a historical revision, and a widget-panel row is not a buffer line.
Four port decisions follow (rounds 11 and 22):

* `dashboard` uses targets for both kinds of row: its recent-file rows publish `{line, path}` and
  its focusable action rows (and a Git-section branch-log row, when git_log is available) publish
  `{line, action}` targets naming hidden commands the dashboard itself registers; the action then
  runs through the normal command dispatcher, exactly like the keyboard mode's Enter, and
  `clear_line_targets` runs on close/unload. The branch-log row is rendered as an explicit
  unavailable line (not a target) when the git_log command is not registered. The repository URL
  stays plain text because there is no safe URL-open action.
* `code_tour` cannot use targets: its steps are a widget-panel card, not lines of a buffer, and its
  step highlight is decoration anchoring in the source file. It keeps local line anchors.
* `git_log` should not use targets: its detail rows' Enter opens the commit's historical blob
  (`git show <sha>:<file>`) as a read-only virtual buffer, exactly like Fresh; a target opens the
  working-tree file, which is wrong for any commit that is not HEAD. The Enter mapping and hunk
  navigation stay local.

### diff_nav / R13-9: the standalone plugin and the git_gutter surface

Fresh's `diff_nav.ts` is the unified Next/Previous Change plugin over three sources: the hunks
`git_gutter` publishes, the `live_diff` hunks, and the saved-text diff. gvim first implemented that
merged navigation inside `git_gutter` (round 14/15) while the standalone plugin/i18n namespace stayed
open (R13-9 residual c). `plugin/bundled/plugins/diff_nav/` is now the standalone, opt-in port
with Fresh's command ids and i18n messages; Fresh ships the plugin default-enabled, gvim does not
because adding it to the default set measured 8.12 MiB against the 8 MiB idle-heap gate (the nine
pre-existing defaults stay unchanged, and git_gutter's own merged navigation remains default-on):

* **Next Change**/**Previous Change** (`diff_nav.next_change`/`diff_nav.prev_change`) collect
  `gvim.plugins.get("git_gutter").hunks(buf)` when git_gutter is loaded and exports it, the same
  export from `live_diff`, and `buf:diff_saved()`; each source is optional and a missing/older/
  erroring export, an absent git binary or an unloaded plugin contributes nothing (navigation still
  works from the remaining sources, including saved diff + live_diff with git_gutter disabled);
* the provider data shapes are the real exports, not test adapters: git_gutter
  `{type, line, count}` (0-based line; its deleted-run line is the line before the seam) and
  live_diff `{kind, newStart, newCount}` (0-based); `buf:diff_saved()` yields the host's
  `{kind, new_start, new_count}` hunks, and when `new_count == 0` (deletion) the jump line is
  `max(new_start - 1, 0)`, matching git_gutter's merged navigation;
* targets are sorted by byte position, deduplicated by line, and wrapped; the jump calls
  `buf:set_cursor(..., { center = true })`, and the status row uses Fresh's exact
  `Change n/total` / `Change n/total [wrapped]` / `No changes` i18n strings; malformed hunk entries
  and a line outside the buffer are clipped or skipped;
* it is `default_enabled: false` (Fresh's is default-enabled; recorded difference for the idle-heap
  gate), registers no keys/settings, and runs all host calls as plugin-task awaits (no blocking IO in
  Update).

`git_gutter` keeps its own merged Next/Previous Change commands and `hunks` export: they predate this
plugin, remain the default navigation while diff_nav is opt-in, and when both are enabled cause two
palette command sets over the same targets (documented residual). Remaining gaps: the side-by-side Compare view (compare_files/--diff)
keeps its own Next/Previous Difference rather than feeding the cross-context navigation; large-file
buffers can still only navigate sources that export something (a host that cannot answer offset_of
falls back to line ordering; git_gutter keeps its own large-file notice); live_diff must be loaded to
contribute its active-diff hunks.

Tests: `plugin/bundled/diff_nav_test.go` covers the manifest/default, the exact en i18n table,
the Fresh command ids/titles, merged git+live sorting and wrapping (next/prev, status strings), line
deduplication, saved-diff-only navigation with no providers, live_diff-only and no-change cases, a
raising provider degrading, prev from the end, and real interoperability with the actual git_gutter and
live_diff plugins (including the real three-source merge, a deleted HEAD hunk, and the saved deletion
seam).
### Assembly LSP: project config offer (port residual)

Fresh's `asm-lsp.ts` helper has three parts: the install-help popup for a missing server, the
"Disable Assembly LSP" popup action, and the `.asm-lsp.toml` config offer. gvim's port
(`plugin/bundled/plugins/asm_lsp/`) covers the config offer and install help in Lua over the
existing API; it exposes the disable action only by delegating to gvim's native action (it is not
reimplemented and is not a catalog command):

* The offer waits in `plugins_loaded`/`after_file_open` like Fresh, but gvim's `lsp_server_error`
  payload is only `{error}` (no language, `error_type` or `server_command`), so the port claims a
  failure only when the active buffer is an assembly file and the message names `asm`/`assembly`;
* "Disable Assembly LSP" is not a plugin/catalog command: before dispatch the plugin re-checks
  that the active buffer is still an assembly file and `gvim.has("lsp")`, captures that buffer's
  path and language, and runs the controlled native `lsp_disable_language_target` action with both.
  The native action re-validates the same path/language is still active before muting, preventing a
  popup/tab-switch TOCTOU from disabling another document's language; the mute itself is a
  **root-level language mute at the LSP port for all roots** (`SetLanguageEnabled`), not just that
  buffer. gvim has no plugin API to name a language independently of the controlled action. The
  native LSP layer owns the async result and no-server/no-port notice; the plugin reports only
  immediate dispatch errors and never claims success. The help popup also offers Start/Restart,
  Show LSP Status and copy-install actions;
* restarting after the config is written uses the native `lsp_restart` action for the active
  language, not Fresh's `restartLspForLanguage(offer.language)`;
* the global asm-lsp config check goes through the workspace file system, so a remote/SSH workspace
  checks the remote home (the local home is not reachable from the plugin API). The project config
  path uses `gvim.workspace.root()`, the authority-correct project directory;
* the "don't ask again" choice is per project root in `gvim.state`; a root long enough to overflow
  the 256-byte state-key cap persists a stable suffix instead of failing the write.
* the project config is created with `gvim.fs.write {exclusive = true}` rather than an atomic
  overwrite: the offer's existence check can race an external creator (the task-197 audit), so the
  create-new must claim the path or fail without replacing it. The plugin API gained that optional
  capability: the local plugin OS opens with `O_EXCL`, a remote workspace uses the authority's
  `FileContentCreator` (`CreateNewWithContent`) so the claim and the content write are one
  operation, and a backend without the capability returns `unsupported` — the plugin re-checks
  existence and reports either "created by another process, not overwritten" or an explicit create
  refusal, never a silent overwrite.

Tests: `plugin/bundled/asm_lsp_test.go` covers command registration/no bindings, the offer and
created TOML for NASM/GAS, MASM + ARM64 detection, the project/XDG-global skip rules, the never-ask
choice, the install-help popup/clipboard/native restart, the active-tab switch refusal and
no-LSP/no-dispatch for the controlled native disable action, and that non-assembly buffers are
ignored. TUI integration tests cover the target action itself (captured path/language success,
active-document and language-mismatch refusal, nil-port notice, and the old nil-port panic guard).
There is still **no plugin→TUI end-to-end test** that loads asm_lsp into a real model and observes
the root-level mute; the plugin side uses the fake host's command recorder and the native side is
covered separately in `internal/tui`.
The harness does not run the real loader's startup flush, so the tests raise `plugins_loaded`
explicitly.

### C# support: no LSP root or server-request seam (port residual)

Fresh's `csharp_support.ts` has four parts: nearest-project detection, `setLspRootUri`,
trust-gated `dotnet restore`, and Roslyn's `workspace/_roslyn_projectNeedsRestore` plus the
csharp-ls install help. The gvim port (`plugin/bundled/plugins/csharp_support/`, opt-in) keeps the
project detection and the trust-gated restore, and reuses the native LSP actions for help:

* the plugin API has no `setLspRootUri`: gvim's language configuration and its root detection choose
  the server root, so the port only runs restores (a project with no configured C# server still gets
  its packages restored);
* gvim has no plugin event for **server-to-client requests**, so Roslyn's
  `workspace/_roslyn_projectNeedsRestore` is not handled. The open-file restore covers the usual
  "first open of a project" case, but a server that asks for a restore later (for example after a
  project file changes) is not answered;
* the restore is once per project root per session (`restored` table) and skipped/deferred while
  `gvim.env.trust_level()` is not `trusted`; `trust_changed` drains the pending roots, as
  env_manager does. The process runs through `gvim.proc.run`, so it follows a remote workspace's
  authority; Fresh's `runOnTarget` is replaced by that one call;
* "Disable C# LSP" is delegated to the controlled native `lsp_disable_language_target` action:
  the plugin captures the active C# buffer's path/language, the TUI re-validates both before a
  root-level LSP-port mute, and a popup/tab switch or language mismatch is refused; it is not a
  catalog command and the native layer owns the async/no-server result. The help popup also offers
  Start/Restart LSP Server, Show LSP Status and the copy-install row;
* Fresh prechecks `dotnet --version` and caches the answer; the port runs `dotnet restore` directly
  and reports an exec error, which also avoids trusting a local-machine `which` in a remote workspace;
* i18n is English-only, unlike Fresh's translated status strings; the plugin registers two commands
  (Fresh has none) and no keys/settings.

Tests: `plugin/bundled/csharp_support_test.go` covers the command table/no bindings, nearest-project
restore and its once-per-project rule, the untrusted→`trust_changed` deferral, the no-project-file
fallback, restore failure and missing-dotnet status, the explicit restore command, install-help
popup/copy/native restart, the controlled disable target with no-LSP and active-tab-switch refusals,
and that non-C# buffers are ignored. The tests set the fake host's trust level and raise
`after_file_open` themselves. The native target action is covered separately in
`internal/tui/lsp_target_test.go`; there is no plugin→TUI end-to-end test.
### Clangd support: no server-specific LSP request (port residual)

Fresh's `clangd_support.ts` sends clangd's non-standard `textDocument/switchSourceHeader` request
through the language-server channel, shows a floating project-setup panel, and turns clangd's
`textDocument/clangd.fileStatus` notification into the status row. gvim's plugin API deliberately
allows only read requests from a fixed allowlist (it refuses server-specific methods before they
reach a server), has no plugin event for server custom notifications, and has no floating-panel
primitive for this content. The port (`plugin/bundled/plugins/clangd_support/`, opt-in) therefore:

* replaces the switch request with a same-stem lookup over C-family source/header extensions in the
  buffer directory, the workspace root and their `include/`/`src/` children. It does not read
  `compile_commands.json`, so it cannot resolve headers in arbitrary include paths or clangd's
  canonical counterpart when names/cases differ; one match opens, several use `gvim.ui.select`, and
  no match is a status row;
* renders the project setup as a read-only virtual buffer (one reused buffer, refreshed in place)
  rather than Fresh's `PanelManager` card; the analysis lines follow Fresh (compile_commands.json
  candidates, `.clangd`/`.clangd/compile_flags.txt`, CMake/Bazel/Makefile hints, general tips);
* opens `.clangd` from the buffer directory or the workspace root, like Fresh's `getCwd` + buffer
  directory targets, and reports when neither has one;
* has no `lsp/custom_notification` handling (`textDocument/clangd.fileStatus`, `$/memoryUsage`), so
  clangd's file-status line never reaches gvim's status row;
* registers the same three user commands as Fresh, no keys and no settings.

Tests: `plugin/bundled/clangd_support_test.go` covers the command table/no bindings, source→header and
header→source switches (single match, picker choice), no-match/unsupported/no-active-file statuses,
the project-setup buffer with a ready project and with missing compile/.clangd pieces (including buffer
reuse and build-system hints), and open-config found/missing. The tests register openable targets on
both the fake host and the fake OS.
### Welcome screen: bounded reduced port with OPEN gaps (port residual)

Fresh's `welcome_screen.ts` is a large startup surface, not a single command: a virtual buffer
plus a page `WidgetPanel` (wordmark, doors, verbs, onboarding levels), a sidebar `Contents`
outline, focus-follows-cursor and scroll-to-widget composition, and cards backed by the
orchestrator and the dashboard. **Capability audit:** gvim's plugin API can create a read-only
virtual buffer, set styled content and line targets, define a buffer mode/keymap, expose a palette
command and read dashboard section metadata (`gvim.dashboard.sections()`). It cannot mount Fresh's
page widget panel with `page`/`focusFollowsCursor`/`scrollToWidget` composition, render the
onboarding document, contribute a live sidebar `Contents` outline for this page, or consume the
dashboard's rendered built-ins/orchestrator exports. The gvim port (`plugin/bundled/plugins/welcome_screen/`,
opt-in) therefore implements one bounded real core rather than claiming the full page:

* wordmark, tagline, three doors (**Open a file**, **New buffer**, **Recent files**) that
  dispatch the real native actions `open`, `new` and `quick_open_buffers`;
* verbs (**Find file**, **Command palette**, **Settings**, **File explorer**, **Help**) that
  dispatch real editor actions, not help text or stubs;
* recent-file rows from this session's `after_file_open` events, deduplicated and capped at 8
  (seeded from at most 64 open buffers) with path line targets; activation opens the path. Recent
  basenames/details and dashboard titles use the dashboard's Cc/Cf/newline/invalid-UTF-8 display
  sanitizer, and the target line is computed from the cleaned one-line row, so a hostile path
  cannot inject a line or shift its click target;
* dashboard rows from the registered external-section metadata only, capped at 8, with
  `No dashboard sections registered.` when none; selecting a row opens the dashboard through its
  public command. No dashboard built-in content, provider callback, git/disk/weather copy or
  orchestrator data is read;
* j/k or arrows move, Enter activates, q/Esc closes, r refreshes; action rows are line targets
  registered by this plugin (the API rejects another plugin's action target). When the page is the
  current buffer the selected row's caret is moved with reveal=true so j/k cannot activate an
  off-screen row in a small viewport; a background page does not move the caret or steal focus.
  This is caret-based row visibility, not Fresh's page focus-follows-cursor/scroll-to-widget
  composition;
* Fresh's startup toggle is represented by a footer action row and the **Welcome Screen: Toggle
  Show on Startup** command. It writes the persistent plugin-state key `show_on_startup` through
  `gvim.state`; `show_on_startup()` prefers that boolean over the `auto_open` setting, and
  `auto_open()` reads it for future launches. The current page is not closed by the toggle and the
  explicit Show command still forces it. The precedence matches Fresh's
  `getGlobalState("showOnStartup")` over plugin config, and the rendering is a text row rather than
  Fresh's checkbox/button widget (no footer copy/styling), but the **scope/durability is reduced**:
  a direct launch uses the editor-wide per-plugin state file and the test covers Stop→new runtime;
  a daemon workspace scopes state to that workspace (Fresh's is global across workspaces), and the
  toggle cannot detect asynchronous write failures. `gvim.state.set` returns after the in-memory
  update; disk errors are runtime warnings only, not a return value or status row. State writes are
  debounced and flushed on Stop for the current namespace; a workspace switch queues the old
  namespace's write, and a fast switch followed by exit may lose that queued write. The toggle is
  therefore best-effort, not equivalent to Fresh's global/durable toggle.

The page is opened automatically as a **background** tab once at startup when `auto_open` is true
and never steals the foreground; it is not closed when files open and it does not reopen after the
reader closes it. The plugin stays opt-in (`default_enabled: false`) while Fresh's page is
enabled by default, so it adds no idle page or default palette row; `auto_open` only matters after
the plugin is enabled. It starts no process, performs no network access or file writes, does no
foreground IO and relies only on existing plugin APIs (no new trust path).

**File-launch guard (landed).** Fresh's guard is
`if (!force && editor.fileLaunch()) return` before creating the page: `fileLaunch()` is true for
`fresh FILE…` (including `$EDITOR`/git editor) and `fresh --simple`, false for bare `fresh`,
`fresh DIR`, a daemon and a session restore. gvim now keeps the `sessionPlan.fileLaunch` decision
(files/`--simple`/`--diff`, with `--restore` and the skip-session config taken into account;
stdin is not part of Fresh's `is_file_launch`, and gvim's stdin-still-sessionless rule stays
separate). It passes it through `tui.Deps` to the model and exposes it read-only as
`gvim.workspace.file_launch()`; `auto_open()` skips when it is true and the explicit
**Welcome Screen: Show** command keeps `force`. The value is immutable and is never inferred from
`gvim.buf.list()`: CLI files and restored-session tabs both exist before plugins load. A
daemon-hosted/shared runtime routes the host call to the active workspace's model, so the flag is
that workspace build's invocation property; `workspaceOptions` builds daemon workspaces without
launch files (they restore their session), so they report false.

**OPEN gaps (not implemented, not claimed):** Fresh's full onboarding ladder and section banners;
page `WidgetPanel`, widgets/buttons/chips, hover styling and composed page width; sidebar
`Contents` and focus/scroll interaction; recent-file persistence beyond the current session;
workspace/agent/orchestrator cards and auto-attach; audit-mode content; dashboard built-in
snapshots (only provider metadata is available to another plugin); and Fresh's localized long form.
The docs/bundled table entry and the plugin header carry the same reduced-scope note.

Tests: `plugin/bundled/welcome_screen_test.go` covers manifest/opt-in, page content and real door
dispatch, action/path line targets, recent-row activation, close stickiness, background
auto-open/no-focus-steal, `auto_open: false`, the persistent startup toggle (page row/command,
next-startup suppression/re-enable, its precedence over `auto_open`, and cross-Stop/new-runtime
state persistence for a direct launch), and dashboard metadata rows plus the empty fallback.
It loads the plugin through the real loader; no plugin→host end-to-end UI composition test is
claimed for the unavailable page widget panel.

### Welcome: a demo plugin, with localized status only in English (port residual)

Fresh's `welcome.ts` is a plugin-API demo, not a feature: five palette commands, two of which
dispatch built-in actions (`show_help`, `save`) and three of which insert text at the cursor. The
gvim port (`plugin/bundled/plugins/welcome/`, opt-in) keeps the same five commands and calls the
same gvim actions/buffer APIs; the differences are:

* `insertAtCursor` maps to `b:insert(b:cursor(), text)`, so it is a plain edit with one undo step
  and no auto-indent/auto-close; a read-only buffer reports the API's insert error on the status
  row;
* the example comment is language-aware (`--`, `#`, or `//`) instead of Fresh's hard-coded `//`, so
  it is valid in more than C-like files;
* the clock is `os.date("%H:%M:%S")` (local 24-hour time) rather than the runtime's locale
  `toLocaleTimeString`, and the status/greeting/comment strings are English-only (the port has one
  i18n file, `en`);
* Fresh's only load-time feedback is `debug` output; gvim's port logs the same debug line and
  deliberately sets no status row.

Tests: `plugin/bundled/welcome_test.go` covers the five command titles/no bindings, the two
built-in action dispatches, greeting and time insertion into an edited buffer with their status
lines, the language-aware comment prefix, and the read-only insert failure. The plugin is
`default_enabled: false` so the default palette and idle heap do not grow with demo rows.
### Markdown source: a native rule set, ported as an opt-in plugin (port residual)

Fresh's `markdown_source.ts` defines a `markdown-source` input mode, activates it when a Markdown
buffer is shown in source view, and intercepts Enter/Tab/Shift+Tab. gvim implemented those rules
directly in `editor/markdown.go` before any plugin existed (the built-in `insert_newline`,
`insert_tab` and `dedent_selection` actions call them for `.md`/`.markdown`/`.mdx` files and the
markdown/multimarkdown languages). The bundled plugin (`plugin/bundled/plugins/markdown_source/`,
opt-in) is therefore the explicit Lua port, with these differences:

* it does not define or auto-activate a `markdown-source` input mode. gvim's plugin API has no
  source/compose view query (the native rule set notes gvim has no other view), and a buffer-local
  mode would add a status label plus the `(Esc x3 leaves)` hint to every Markdown tab. Instead the
  plugin registers three hidden normal-context commands with `enter`/`tab`/`shift+tab` bindings;
  they check the active buffer and fall back to the built-in actions outside Markdown, with a
  selection, with extra carets or while a plugin input mode is active (Fresh steps aside for its
  input mode too);
* the checks are content-based, so `.mdx` (which gvim has no language entry for) works by suffix,
  matching the native rule set;
* every successful rule is a direct plugin-buffer edit (`b:delete`/`b:replace`/`b:insert` plus a
  cursor placement), one undo step per key, rather than a mode handler; failures (read-only buffer,
  stale state) are status rows;
* the four-space unit is hardcoded exactly like Fresh's `TAB_SIZE`; `tab_size`/`use_tabs` are not
  consulted, tabs already in the indent are preserved and Shift+Tab removes four bytes;
* the plugin is off by default because the native editor already provides the behaviour; enabling it
  adds the key bindings and keeps the default palette/idle set unchanged.

Tests: `plugin/bundled/markdown_source_test.go` covers the hidden key commands and bindings, the
Enter table (bullets, ordered increment, checkbox, empty-item marker removal, indented items,
non-list indent copy, cursor), the Tab table (bullet cycles, checkbox state, ordered/content
fallback, cursor), Shift+Tab (reverse cycle, indent threshold, `dedent_selection` fallback),
non-Markdown and selection fallbacks, and the read-only failure status.
### Rust LSP: no LSP-menu, disable or register seam (port residual)

Fresh's `rust-lsp.ts` is mainly an LSP error flow: it detects rust-analyzer failures, contributes
copy-install/disable rows to the native LSP-Servers popup, clears them when the server recovers, and
offers a Configure Mode command that registers a full or reduced-memory rust-analyzer and restarts
the language. gvim's plugin API has no LSP-menu contribution, no language-disable call and no
register/restart-for-language call, so the bundled port
(`plugin/bundled/plugins/rust_lsp/`, opt-in) keeps only the install/help surface as explicit
commands:

* `Rust: Copy rustup Install Command` / `Rust: Copy brew Install Command` use `gvim.clipboard.set`
  and report on the status row (`rustup component add rust-analyzer` and
  `brew install rust-analyzer`, the commands Fresh's popup offers);
* `Rust: Check rust-analyzer` runs `gvim.proc.run {"rust-analyzer", "--version"}` (5 s / 64 KiB
  bounds) through the workspace authority and its trust gate; the plugin's `LookPath` is a documented
  local-machine API and is not used for the presence decision. The check succeeds only when
  `result.ok` is true (exit 0, no timeout/signal). A failed probe (including exec failure or
  trust-gate refusal), a non-zero exit (first stderr line), a timeout and a signal each have their
  own status; a failed probe says it could not run on the workspace, names the local-only LookPath
  limitation, and never reports a remote workspace's binary as missing;
* `Rust: Language Server Help` is a `gvim.ui.action_popup` with Fresh's install commands, the
  native Rust LSP-selection rules (built-in Rust entry under the normal `lsp_enabled`/root-marker/
  `auto_start` rules; an explicit `lsp.rust` entry or a disabled Rust language replaces/skips it;
  the plugin toggle only controls the helper commands and never changes the native selection),
  PATH/restart/toolchain troubleshooting notes and the upstream installation link, plus copy/check
  rows and the native Start/Restart LSP Server and Show LSP Status actions.

**Disable Rust LSP** is available as a help-popup action, not an LSP-menu contribution: it
re-checks the active `.rs` buffer and `gvim.has("lsp")`, captures that buffer's path/language and
dispatches the controlled native `lsp_disable_language_target` action. The TUI validates the same
path/language before the root-level LSP-port mute; a popup/tab switch or missing LSP refuses without
dispatch, and the plugin never claims the asynchronous result. This is Fresh's disable behavior
without its contributed LSP-menu row/recovery flow.

Not ported: automatic status on `lsp_server_error`, contributed "Plugin actions" rows in the
native LSP-Servers popup, recovery detection via `lsp_status_clicked`, and Configure Mode
(registering full/reduced-memory rust-analyzer, process limits, restart). gvim's
`lsp_help` already puts a Rust install hint on the status row and opens its own help popup on an LSP
status click, so this plugin deliberately does not add a second `lsp_server_error` handler. The plugin
is `default_enabled: false`, registers no keys and no settings.

Tests: `plugin/bundled/rust_lsp_test.go` covers the visible command table/no bindings, both copy
commands (clipboard/status), the authority probe finding rust-analyzer despite a local LookPath
miss, the non-authoritative probe-failure/trust-gate-refusal status (no missing claim), found+version,
non-zero version exit, timeout/signal failures, exec failure, and the help popup's message/actions
plus copy/check/native-restart dispatch, and the controlled Disable Rust LSP target/no-LSP/tab-switch
refusals.
### Deno LSP: native auto-selection plus a reduced helper plugin (port updated)

Fresh's `deno_lsp.ts` selects `deno lsp` for TypeScript and JavaScript when the active window's
root holds `deno.json` or `deno.jsonc` and the Deno runtime is available; it remembers the
configured entries as a baseline and restores them when the active window is not a Deno project,
leaving a disabled language alone. That selection is now ported in **gvim's native core**
(`internal/app/deno_lsp.go`, documented in `docs/lsp.md`), not in the bundled plugin. The core
rules: it probes the workspace root gvim opened/switched to (not every nested per-file root); when
that root has `deno.json`/`deno.jsonc`, one bounded authority check confirms the runtime
(`sh -c 'command -v deno'` through `authority.OneShot()` under the workspace trust gate; remote
never uses local disk/PATH), `lsp_enabled` is true, and the language is `typescript` or
`javascript`, core registers `deno lsp` for a manager created for that exact root. Nested/per-file
TS/JS roots and other languages keep their configured servers, and `tsx` is untouched. An explicit
`lsp` entry for typescript/javascript always wins and opts out, as does a disabled language or
`lsp_enabled: false`. The provider registers `deno lsp` with `auto_start: false` (nothing is spawned silently).
Installing/clearing the selection never stops a running server. An explicit Start/Restart LSP Server
for typescript/javascript applies the completed current-root selection to an existing manager: if
its config differs (Deno confirmed, or a config reload/explicit/disabled change cleared it), gvim
replaces the whole language server list and restarts only that language's servers; other languages
and universal servers are untouched. If the selection was cleared, the same explicit action restores
the configured baseline. When a whole-language replacement is needed, Restart Server on any row of
that language replaces the entire server list with the current selection — the clicked row's name is
not used in that branch; otherwise Restart Server keeps its normal meaning and restarts only the
named server. A root-wide LSP Restart rebuilds managers, so the selection applies when the root
creates the next manager. A Settings Save/Reload pushes the reloaded config's server baseline into the LSP port
(`SetConfig`): future managers and the next explicit Start/Restart use that baseline, including a
newly added explicit typescript/javascript server, but the reload itself does not rebuild or restart
an existing manager — it keeps its old config until the explicit action. Deno auto-selection is not
applied while an explicit/disabled entry is present. Settings reload and a live trust revoke/restore
pass the reloaded config through the LSP config gate before it reaches the port (the Deno probe
evaluates the same gated config): Restricted/Blocked cannot gain auto-starting servers via reload or
trust change, a manager created after a revoke does not auto-start, and after a revoke an
already-created manager's old AutoStart configs no longer start new servers on Open without stopping
running processes; manual Start/Restart remains available. Auto-start and launch spawn are two live checks, not atomic: a revoke between them refuses only when
the spawn gate denies (Blocked, remote workspace not Trusted, or a project-contained executable);
under Restricted a system-installed binary outside the project may still start. A revoke after the
spawn gate passes can likewise let that in-flight start proceed. Neither check nor a revoke stops
existing processes; later Opens do not auto-start after the revoke. Both replacing and same-config
Restart preflight the spawn gate before stopping, so a denial preserves running servers and old
config; preflight→launch is non-transactional (old servers may already be stopped and the new start
refused when the spawn gate denies, though under Restricted a system-installed binary outside the
project may still start); a repeated Start of an already-running server returns the live handle
without spawning or consulting the gate, even when Blocked. Other start failures (missing binary,
initialize failure) are still not transactional. The authority probe returns false while the authority is disconnected (a reconnect
re-probes), requires a strictly clean exit 0 (timeout/signal/start-error/cancel results are
rejected), and provider updates are serialized with explicit Start/Restart (in-flight old-language
starts are waited out before replacement). Probe failures/timeouts/disconnects/trust denials select
nothing. The bundled plugin's `default_enabled: false` toggle only controls its
helper commands and does not control the native auto-selection.

The bundled port (`plugin/bundled/plugins/deno_lsp/`, opt-in) keeps the user-visible helper
surface instead:

* `Deno: Check Project` reads the workspace root (`gvim.dirs.cwd()` when there is none), checks
  `deno.json` and `deno.jsonc` through `gvim.fs.exists`/`gvim.path.join` and runs
  `deno --version` through `gvim.proc` (5 s / 64 KiB bounds, workspace authority and trust gate).
  It reports ready only for config plus `result.ok` (exit 0, no timeout/signal); config with a
  non-zero exit, timeout or signal is a failed check and is never reported ready. It also reports
  config with a probe start/trust-gate failure, runtime without a Deno project config, or neither.
  The plugin's `LookPath` is local-only and is not used for the presence decision; a failed probe
  says it could not run on the workspace, names that limitation and never claims the remote host
  lacks deno, and points at the install command/help popup.
* `Deno: Copy Install Command` always copies the official
  `curl -fsSL https://deno.land/install.sh | sh`, because a local `brew` lookup says nothing about
  a remote host; the help popup still offers explicit copy-brew for users who prefer it, and the
  status row reports the copied value.
* `Deno: Language Server Help` is a `gvim.ui.action_popup` with the install commands, the native
  auto-selection rules and explicit-config opt-out, the `typescript`/`javascript` `"lsp"`
  entries (`command: "deno"`, `args: ["lsp"]`, `auto_start: false`,
  `root_markers: ["deno.json", "deno.jsonc"]`, `initialization_options: {"enable": true}`),
  the workspace-authority probe note and local-only LookPath caveat, the native Start/Restart LSP
  Server and Show LSP Status actions, and the upstream installation link. When the active buffer is
  TypeScript/JavaScript (`.ts`, `.tsx`, `.js`, `.jsx`, `.mjs`, `.cjs`) and `gvim.has("lsp")`, it also
  offers `Disable Deno LSP for this language`: the action re-checks the active buffer/LSP feature
  after the popup, captures that buffer's path/language and dispatches the controlled native
  `lsp_disable_language_target` command; a tab switch to a non-target file or a missing LSP is a
  notice with no dispatch, and the plugin never claims the async mute succeeded. The language gate
  is strict: only `buf:language()` `typescript`/`javascript` is a Deno buffer (plus `.tsx` language
  `tsx`); `.tsx` detected as `typescript` targets `tsx` per native `openLanguage`, and the
  controlled target action accepts that post-open `tsx` language for a TypeScript-detected .tsx
  buffer (TUI integration test), so the mute reaches the same server LSP opened. The extension set
  is supported only when already recognized as `typescript`/`javascript`: `.ts`, `.tsx`, `.js`,
  `.jsx`, `.mjs`, `.cjs`. Fresh's default languages map javascript only js/jsx/mjs and has no cjs
  (gvim matches this); `.cjs` and any unmapped extension require a user `languages` entry before
  the action appears. `.jsx` detected as react is not a JS language. Text/unrecognized languages
  show `status.not_deno`, offer no action, and never pass `text` to the native mute. No LSP-menu row or native-selection configuration is added by this
  helper.

Not ported in the plugin: server selection/baseline restore (native core owns it now), the
active-window root decision, and the Fresh debug log. The presence check is the authority-following
`deno --version` probe (not the local-machine LookPath); the project-file existence checks and the
configuration itself are workspace-facing. The plugin is `default_enabled: false`, registers no
keys and no settings.

Tests: `plugin/bundled/deno_lsp_test.go` covers the manifest/command table/no bindings, the
always-curl installer and clipboard/status, an authority probe succeeding despite a local LookPath
miss, ready only for a successful version check, non-zero exit/timeout/signal never ready, probe
start/workspace-trust-gate failures (not a missing-binary claim), the probe-failure/ready/no-config
states plus `deno.jsonc` and a custom workspace root, the help popup's native
auto-selection/explicit-config/plugin-toggle text and actions plus copy/check/native-restart dispatch,
and the controlled Disable action (recognized TS/JS extensions, target args,
strict-language/tsx mapping, real default-config detection of unmapped `.cjs`,
unrecognized-language/no-LSP refusals, no false success).

### Slang: native slang-synth external locations + helper plugin (native flow landed)

Fresh's `slang-lsp.ts` is an LSP URI-scheme provider: it claims the `slang-synth://` scheme so
the core routes builtin go-to-definition targets to it, then answers
`lsp_open_external_uri` by running `slangd --print-builtin-module <module>` and opening the
dumped `.slang` source read-only (cache keyed by `slangc -v`). gvim now implements that
builtin external-location flow in **native core** (`internal/app/slang_synth.go`, the
`internal/lsphost` external-location opener and the read-only TUI goto), not through a plugin
URI hook:

* only the `slang-synth` scheme is accepted, with one bounded module authority
  (`[A-Za-z0-9._-]`, at most 64 bytes, no leading `.`/`-`); userinfo, port, query, fragment
  and `.`/`..`/backslash path segments are refused, and the path is never a filesystem path;
* it is **local authority only**: a remote/SSH workspace refuses explicitly instead of writing or
  opening on the other side or falling back to a local command; `<Program>
  --print-builtin-module <module>` runs through `authority.OneShot()` under the workspace trust
  gate, with no `os/exec` path, where `Program`/`env`/cwd come from the answering LSP server (a bare
  configured command falls back to that spelling resolved by the authority one-shot);
* bounds are 5 s / 4 MiB stdout; timeout, signal, start/cancel failure, truncation and non-zero
  exit are LSP location errors; invalid URIs and untrusted workspaces execute nothing;
* the dump is written atomically under a **content-addressed** user cache file
  (`gvim/slang-builtin/<module>-<sha256>.slang`, 0444 in a 0700 directory), so concurrent
  resolutions of the same module never overwrite a path another goto is about to read and an
  already-open document is reused only for identical bytes; a throttled non-blocking sweep drops
  entries older than seven days and keeps at most 128 older-than-an-hour files. The path is opened
  with the editor's generated/read-only marker at the LSP position. The editor still has no generic
  plugin URI-scheme/external-URI API.

The bundled port (`plugin/bundled/plugins/slang_lsp/`, opt-in) keeps only the user-facing
install/check/help half; it does not handle synth URIs:

* `Slang: Check slangd / slangc` runs `slangd --version` and `slangc -v` with
  `gvim.proc.run` (through the workspace authority, 5 s / 64 KiB bounds), decides with
  `result.ok`, and reports each tool's first version line; a non-ok result carries exit code +
  first stderr/stdout line, and a start failure carries the actual error (remote-authority/sandbox/
  broken-executable errors are never rewritten as "not found"); one tool failing does not hide the
  other's answer;
* `Slang: Copy Install Command` copies the official release instruction. Slang has no single
  official package-manager command (the upstream install page points at binary releases), so the
  clipboard text names the GitHub releases page and putting `slangc`/`slangd` on PATH rather than
  inventing a manager command;
* `Slang: Language Server Help` explains adding `lsp.slang` (and a user `languages.slang` entry
  for `.slang`/`.slangh` when needed), the native local-only `slang-synth://` flow, the
  missing generic plugin URI API, verification/restart steps, the releases/docs links, and offers
  copy/check plus the native Start/Restart LSP Server and Show LSP Status actions. For an active
  `.slang`/`.slangh` buffer with `gvim.has("lsp")`, it also offers **Disable Slang LSP** with the
  same post-popup target re-check and controlled `lsp_disable_language_target` path/language
  dispatch; the language must be exactly `slang`. Fresh has no default `.slangh` mapping and gvim
  detects it as text/empty unless a user `languages.slang` entry (or Set Language) applies, so a
  default `.slangh` is refused with `status.not_slang` and never sends `text`; tab switch/non-Slang/
  missing LSP is a notice with no dispatch and no async-success claim.

Not ported: a generic plugin-provided URI-scheme claim or `lsp_open_external_uri` hook (still no
plugin API), arbitrary external-URI providers, remote/SSH generation (the native flow refuses
explicitly), Fresh's version-keyed persistent cache (gvim keys the cache by content instead), custom
server argv/wrapper arguments (only the resolved program is reused) and an exact-binary guarantee when
the configured command is left bare for the child to resolve. The plugin is `default_enabled: false`,
registers no keys/settings, and the native Slang LSP entry (if the user configures one) is untouched.

Tests: `plugin/bundled/slang_lsp_test.go` covers the manifest/visible commands/no bindings, the
copy instruction (clipboard/status), both tools OK (version lines, argv order), one tool failing
(non-zero exit named), a start failure preserved verbatim and not rewritten as missing, and the help
popup's message/actions plus copy/check/native-restart dispatch, and the controlled Disable action
(both extensions, strict `slang` language, target args, real default-config detection of unmapped
`.slangh`, text-language/no-LSP refusals, no false success). The native flow is
covered by `internal/app/slang_synth_test.go` and `internal/lsphost/external_uri_test.go`.

### Notes for porters (learned from the bundled ports)

* **Never read a whole buffer.** Scanning a 150 000-line file line by line in Lua blows the time budget.
  Use `b:find` and `b:find_lines` (host side, chunked with `gvim.timer.sleep(0)`), and read single lines with
  `b:line(n)` only for the candidates. `merge_conflict` does this.
* **Use `w:visible()`** for "what is on screen" (soft wrap and folds make `top_line + height` wrong).
* **Debounce** hot events (`buffer_changed`, `cursor_moved`) with `gvim.on(..., {debounce = ms})`, and keep
  handlers under 50 ms: yield to the editor with `gvim.timer.sleep(0)` every few thousand items.
* **Diff on the host**: `gvim.diff.buffer(buf, old_text)` compares without building line arrays in Lua.
* **Pass `version`** to decoration and edit calls computed from text you read earlier, so a change in
  between is `stale` instead of a mis-placed mark.
* **Make initialisation idempotent.** A plugin that loads late is replayed `after_file_open` and
  `buffer_activated` for the open buffers, and you probably also scan `gvim.buf.list()`.
* **No `a, b = b, a` on locals** ([conventions.md](conventions.md#lua-51-notes)); no ⏳ call inside a
  `table.sort` comparator, a `gsub` callback, a metamethod, a `validate` callback or an iterator function.
* **Do not trust settings.** See [limits.md](limits.md#settings-are-untrusted-input).
