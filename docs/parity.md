# Fresh parity record

Status: live working record for the "full Fresh feature parity" goal, first written 2026-11-08 and
refreshed after rounds 1–12 landed (round 1: workspace trust, sidebar Contents, status pills;
round 2: virtual space, keymaps, status bar/menus, sessions; round 3: settings page, terminal
scrollback/links/mouse, explorer options, palette preview/toggles; round 4: LSP router/edits,
project-wide search/replace + Git Grep, settings coverage, virtual-space block/vertical, markdown
smart editing; round 5: LSP 4b data rendering, plugin API tier 1a, theme model/editor, terminal
slice B, project-search residuals, status-bar elements; round 6: LSP 4b E diagnostics panel/
quickfix/progress, plugin tier 1b, the diagnostics_panel port, the Markdown compose core,
Contents fold_buffer, search preview/multi-line; round 7: LSP R6-5, plugin tier 1c-A store half,
git_log/live_diff ports, markdown fences/tables, overlay keymap contexts, a settings R4-3 slice;
round 8: plugin tier 1c-B/C, #15 compare API + compare_files, markdown dual + table borders,
settings R7-3, path_complete/lsp_help, live_diff/git_log full paths, streaming search + Git Grep
globs; round 9: Finder port wave (lsp_navigation, find_references, dashboard, code_tour), Markdown
residual slice, search R8-5, inline-hint tier 2, Contents R6-6/sidebar state, tier-1c remainder +
declared-settings API; round 10: git_grep, git_explorer + explorer host API, env-manager + env
detection/activation, markdown/sidebar residuals, terminal slice A, theme slice A, LSP leftovers;
round 11: daemon protocol/socket core, i18n foundation #22a, terminal completion (ConPTY/OSC52
write/transcript reflow), theme provenance, stdin-as-a-buffer #25, line-target migration, LSP
small residuals; round 12: daemon #18a MVP end-to-end, terminal tabs, #20a agent sessions, #21a
authority + SSH terminal/exec, #22b i18n catalogues, and the LSP residual sweep). Every status
below was checked in this tree or in `refs/fresh`, not inferred from the old migration notes.
Update the affected row when a round lands instead of rewriting it.

Last verified: 2026-11-09, tree commit `4cf47b2` plus the uncommitted round-12 work. Round 12's
gate set was run by this audit: go build ./..., CGO_ENABLED=0 go build ./..., go vet,
golangci-lint 0 issues, gofmt clean, purity and import rules green, the generated menu check
clean, and GOOS=windows go build ./... green. go test ./... -count=1 had two failures —
internal/app TestDaemonHostsAttachDetachAndQuit (the attach read a repaint/output frame before
the expected pong under load) and plugin/bundled TestViSuspendedDropsItsStatusAndMode (the known
load flake) — both pass repeatedly in isolation; 70 packages pass. Fresh reference: the
`refs/fresh` checkout as read on the same date.

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

**Round 9 outcome (2026-11-08).** Six tracks landed; the port wave and the remaining plugin-host
surface were the headline:
**Finder library + port wave.** `plugin/bundled/lib/finder.lua` is a Lua port of Fresh's 1862-line
finder split into Fresh's fuzzy ranking with hard caps, a prompt frontend over `gvim.ui.picker`
(the library ranks in Lua so custom orderings survive; `on_close` runs once), and a panel/tree
backend plus a locations finder (relative `path:line` rows, snippets, preview tabs, Enter);
`diagnostics_panel` now uses it and lazy-requires it so the idle default set does not pay for it.
Ported on it: `lsp_navigation` (Go to LSP Symbol from `documentSymbol` with Fresh's fuzzy ranking
re-sorted by source line, preview-on-move, Enter jump, Esc restores; plus gvim-addition LSP
Show Definitions/Implementations/Type-Definitions list commands over native direct jumps; no
initial selected index, highlight as decoration, no default key), `find_references` (command-
driven because gvim has no `lsp_references`/`lsp_implementation` plugin events; Show/Hide
References Panel, bounded reads, preview tabs, 100-row cap, Fresh status messages), `dashboard`
(read-only start tab: repo state, recent files, keyboard-navigable actions, `df` disk rows;
auto-open/close, timer refresh; reduced: no weather/GitHub/network, no third-party section API/
dock placement, no mouse, no sessions) and `code_tour` (Fresh `.fresh-tour.json` and VS Code
`.tour` auto-detected, 1 MiB/200-step bounds, floating widget card with list/prose/buttons/hints,
step jumps with anchored decorations, last tour/step restored from plugin state, create-starter
command, atomic write; reductions: no dock placement, no per-step buffer modes, one tour, the
plugin keeps its own anchors with `set_line_targets` now available for a future version). Bundled
set: 19 plugins, 9 default-enabled, 10 off (flash, vi_mode, live_diff, git_log, compare_files,
merge_conflict, dashboard, code_tour, lsp_navigation, find_references); idle heap ~7.0 MiB of
the 8 MiB gate (~8.9 session baseline, ~1 MiB headroom). `merge_conflict` (1.35 MiB) flipped off
because round-9 host growth pushed the ten-plugin set to 8.07–8.10 MiB over the firm gate; the
loader's off-by-default list and the memory attribution were updated. **Markdown residual slice.**
Table widths memoized by (text version, page measure) — fixing a dual-pane width bug; escaped
pipes are cell text; tables in containers fall back literal; multi-visual-row cells via
`PageExtraRows` continuation rows with rails, caret reveal and mapped presses; fence continuation
rails; heading-marker conceal plus heading marks on the page-view scrollbar track (memoized);
page-view mode/width session persistence (`session.FileState.pageView/pageWidth`, omitted when
unset, migration-safe, restore applies after open); mapped occurrences/rulers/indent guides stay
deliberately off in the page view (composed plugin overlays order after the compose); dual
residuals stay (exact row/byte anchors, native menu row, split persistence, multiple duals).
**Search R8-5.** The preview protocol carries `Start`/`Total`/`Spans` with a bounded syntax
highlighter (LRU, 256 KiB/4000 lines) and `Options.SyntaxStyles`; the column scrolls (PgUp/PgDn,
Ctrl+Home/End, wheel over the column, three-line notch), has a header window indicator
(`Lfirst–last/total`), pages a long multi-line match without changing the replace range, and the
large-file replace distinction is explicit: an open buffer is searched from its snapshot whatever
its size (unsaved-tab/hash model, Save All writes), a closed file above the read cap/threshold is
never searched and shows a skip reason. `Alt+A` gains a third stop, Open Buffers (unsaved text).
**LSP R7-5.** Inline-hint tier 2 landed (protocol position → rune column, parts/padding,
display-only faint cells spliced before the anchor through one additive `Pane.InlineHints` hook;
wrap/soft-break/wide/truncation handled, no caret-line hiding, caret/mouse keep line columns);
the end-of-line form remains only for page view, tall/windowed lines, panned rows, no-room rows
and concealed insertion points. R7-5 shrinks to the universal server, `process_limits` and the
workspace-diagnostic cancel. **Contents R6-6.** R1-2 sidebar/session state persistence landed
(`tui.SidebarState` + `session.Sidebar`/`restore` conversions: width form/side, explorer fold/
selected path/scroll, Contents open/rows/window with a deferred restore after the first scan,
Diagnostics section; saved-layout restores only, bounded, migration-safe); the fold source
publishes heading ranges while the section is hidden and rescans on hidden changes, and
`contents.fold_buffer` applies live (`ApplyLive`) instead of restart. **Tier-1c remainder.**
Row-based scrollbar thumb with inserted rows (windowed/large documents keep line-based), virtual-
row wrapping (byte-safe chunks, overlays/glyph first chunk only, anchor mapping, caps),
`setLineTargets` (10k/buffer, path/split-label caps, Enter and click, `into` a labelled split,
owner cleanup, replacement semantics), fixed-height group leaves (`{type="fixed",height=N}` 1–100
rows re-applied on layout change, approximate border note; git_log's height-1 toolbar renders
one row), and the plugin declared-settings schema/values API exposed read-only in the Settings
Plugins page. API stays level 2 (242 functions/1478 lines). Palette stays 229; 47 plugin command
registration sites.

**Round 10 outcome (2026-11-08).** Seven tracks landed; the port wave, the env/explorer host
surfaces and the terminal/theme slices are in:
**git_grep port** (`docs/plugins/bundled.md`, `docs/plugins/porting.md`): Fresh's live git grep
over the shared Finder. Query runs `git grep -n --column -I [-i] -e` through the hardened
`gvim.git` gateway (15 s timeout, 2 MiB cap) in the active buffer's repository; repo-relative
`path:line` rows with the source line, Enter opens at line+column, 100-row/per-line caps, case
module-level (insensitive default) with Fresh's second palette command as the toggle; exit 0/1
are success. Reduced adaptation: no prompt toolbar, so the case state is in the picker
footer/hint and flipped by the command (refreshing an open prompt); no Finder preview band;
superseded queries are discarded by the picker generation guard (the source has no cancel
handle); Fresh binds no chord and neither does the port. **git_explorer** plus the explorer
host API: new pluginapi ExplorerHost (set/clear decorations and slots, namespaced per
(plugin,namespace), 10k entries/64 namespaces, symbol/text/leading caps, absolute-or-root-
relative path normalization with outside-root ignore, per-row priority-then-namespace-then-
call resolution, owner cleanup), the explorer panel's `RowDecor` painter (leading/trailing
slots, suppress flags, filename colour, theme-key/RGB resolution) and Lua
`gvim.explorer.set_decorations/set_slots/clear*`; the port uses `git status --porcelain -z`
from the workspace root, Fresh's M/A/D/R/C/U badges/priorities (`!` conflicts, staged
modification in the added colour), `color_names` ancestor slots, refreshes on
ready/open/save/revert/focus_gained/config plus a debounced .git-directory watcher, and is
opt-in (~1.5 MiB idle; no nested sub-repo discovery/hover tooltips/arbitrary icons).
**env-manager** plus the host env slice: `env.detectors` in the user config only (project
config refused; exact Fresh built-ins: .venv/venv path-only with interpreter requirement,
direnv/mise/pipenv/poetry shell; first match wins), a shared detector list with the trust
markers and passive `DetectEnv`/require-evidence detection at workspace open, `env/` provider
activation gated on Trusted (bounded `sh` capture of the set/unset delta), and the overlay
injected into every spawn the editor owns (shell commands/formatters, LSP servers, plugin
processes, terminal shells, app git-grep); pluginapi EnvHost + TUI EnvPort + Lua `gvim.env`
with detected/active/trust_level/set/clear and `env_changed`/`trust_changed` events; Settings
Environment rows + a read-only Detectors page. The port has Fresh's four i18n command keys,
the status pill, auto path-only activation once trusted and the core trust dialog request for
shell environments; it is opt-in (no widget panel/watcher/devcontainer, one local env,
set-only overlay, explicit shell activation). **Bundled set 22 plugins, 9 default-enabled,
13 off** (flash, vi_mode, live_diff, git_log, compare_files, merge_conflict, dashboard,
code_tour, lsp_navigation, find_references, git_grep, git_explorer, env_manager); idle heap
~7.25 MiB of the 8 MiB gate (measured 7.22–7.27, ~9.3 session baseline; round-10 host growth
moved it from 6.97; the two new opt-ins would each add ~1.5). **Markdown/sidebar R9-3/R9-4.**
Quote-container fences (QuoteDepth, framed at the container, prefix on continuation rows,
per-line literal fallback when markers disagree), setext heading styling/conceal with the
thematic-break distinction, task-list glyph conceal (☐/☑), image alt-text conceal, inline HTML
kept literal, mapped-decorations decision documented (occurrences/rulers/guides deliberately
off in page view + no-paint tests), horizontal-page clamp decision + test, dual line-anchor
approximation + one-dual-at-a-time policy, and sidebar plugin-section stack order/requested
rows/folds persisted (additive bounded Session.SidebarState fields, migration-safe, applied
when sections mount). Residuals: exact byte dual anchors, native menu row, per-section widths/
drag order, mapped ordering test. **Terminal slice A.** Drag auto-scroll (band/direction from
the drag, dock mouse forwarding while dragging, fixed ~80 ms seq-guarded one-row tick, stops at
ends/back-inside/release/clear/focus loss), word/line-wise drag extension after double/triple
click, Ctrl+hover underline + file:line:col parsing/resolution/open in a Cmd (OSC 7 dir then
workspace root, ambiguous colon paths fall back), `terminal.pass_ctrl_v_without_text` matrix
with the LastYank fallback; kitty keyboard stays a documented residual (x/vt v0.1.0 encodes
legacy keys only; its SendKey carries a TODO), ConPTY gets a precise deferred plan. **Theme
slice A.** The six bundled themes' hand-picked accent overrides were removed so `ui.accent_bg/
fg` falls back to the popup selection colours (audit tests at theme+style level pin the equality
and painted roles); Settings theme-row four-chip previews (`SettingsItem.Preview`, cached
catalogue colours, chips+footer); the colour picker/mouse editor overlay (`p` or a swatch
click, palette = working theme's colours in key order + named ANSI, arrows/pages/click/drag
live-preview on the working theme, Enter applies dirty, Esc/outside restores; hex/attributes
prompts unchanged); URL/packaged theme decision recorded (keep the clear refusal, no network
code/fetch until packaging/network policy). Residuals: per-cell provenance, background images,
drag-and-drop of theme values. **LSP leftovers.** Workspace-diagnostic pulls go through
`protocol.Call` so supersede/Stop/Restart/SetLanguageEnabled send `$/cancelRequest` with the
correct per-client id (pullSet, never cache/merge cancelled pulls, last-good answer kept,
10 s timeout fallback); `process_limits` is enforced on Linux (user-delegated cgroup v2
`memory.max`/`cpu.max` under `gvim-lsp-<pid>`, `GVIM_CGROUP_BASE` override, `sh -c 'ulimit -v'`
memory fallback, CPU percent without a cgroup recorded not applied) and explicitly
unreachable on macOS/Windows with the reason in the Show-LSP-Status popup + docs per-OS table;
the universal-server decision is recorded (no built-in quicklsp — a separate user-installed
binary — configured universal_lsp is supported). R7-5 is now only that universal-server
residual plus the per-OS limits caveat.

**Round 11 outcome (2026-11-09).** Six tracks landed; the daemon MVP is partial by design
(task-68 stays in progress for round 12):
**Daemon #18a (partial).** `internal/daemon` has the protocol and socket core: a frame stream
(4-byte big-endian length + type byte + payload, MaxPayload 1 MiB; client hello/input/resize/
detach/open_file/ping; server output/notice/detached/pong/exit; oversized frames fail the
connection), and the Unix socket (user cache dir 0700, per-root default name = slug + 8 hex of
the absolute root, `IsLive` probe, stale unlink, live-daemon refusal, 0600, `Attach`). Unit
tests cover round-trips/bounds, the live refusal, socket perms and the name/path helpers. NOT
landed (R11-1): app.Options Reader/Writer + `tui.RunWithIO` socketIO hosting/repaint, CLI
`--daemon`/`-a`/`--attach`, the Detach hook/palette row/ack, single-writer refusal,
`auto_attach`, session/PTY integration, docs/architecture and the #19 design outline. The
round-12 continuation is the Phase-1 #18a plan + #19a workspaces. **i18n #22a foundation.** New
TUI-free `i18n` leaf package (15 locales with code/English/native name, `Normalize`/`Resolve` from
config + LC_ALL/LC_MESSAGES/LANG, `T` with %{name}, `TN` with Fresh's CLDR plurals incl.
ru/uk/cs/fr, `Register`, an embedded English catalogue, locale→shorter-tag→en→key fallback);
config `locale` (null/auto/15 codes, user-layer only, project config refused with a diagnostic);
Settings Interface locale row (null encodes auto, ApplyLive pushes `i18n.SetLocale` +
`plugin.Runtime.SetLocale` live); startup resolves the locale before the UI/plugins; the
plugin host and Lua `gvim.i18n.locale` use it; the first extraction slice moved editor clipboard
statuses, root copied-path/formatted/inlay notices and dock user-visible notices to keys with
English-identical behaviour, and the remaining inventory is documented; `docs/i18n.md` +
settings/architecture/plugins docs. Generated command/palette/menu titles stay English by
design (R11-2). **Terminal completion R10-3.** A `ptyProc` seam with the Unix adapter; the
Windows ConPTY implementation is pure-Go x/sys/windows under `//go:build windows` (pipes,
CreatePseudoConsole, STARTUPINFOEX attribute list, CreateProcess, Resize/Close; GOOS=windows
build + vet + test-compile verified, with a real-Windows acceptance checklist — parity is
unverified until that pass); the child OSC 52 write parser (bounded, chunk-safe, selectors
c/empty, recovery) with `Session.SetOSC52Handler` gated by the new `clipboard.use_osc52`
(default off) AND Trusted workspaces, restored transcripts never hooked, bytes still passed to
x/vt; transcript-only reflow (`ReflowLines/ReflowRows`: styled cells/wide pairs kept, hard rows
stay, text re-wrap not semantic reflow, dock reflows on width change, restored mismatch
notice; live screen still clipped). Kitty keyboard stays a precise residual (x/vt TODO/no
seam); `pass_ctrl_v_without_text` was already complete. **Theme provenance R10-4 (partial).**
`grid.Style` carries `FGKey/BGKey` uint16 ids and `grid.Grid` an opt-in capture buffer
(`CaptureKeys`/`ReleaseKeys`/`KeysAt`; no allocation or render change when off); a dense role
table over `theme.Keys()` with alias canonicalization (`style/roles.go`, `TaggedCell/
TaggedStyle`); tagging at construction covers overlays, editorpane text/selection/gutter/
current-line/all syntax kinds, the status bar and menu band, plus cheap explorer/sidebar/
diagnostics chrome; the inspector prints exact `Painted by (fg/bg): section.key` with an
explicit "matched by colour" fallback for untagged sides; tests. The colour picker/drag was
deliberately not extended; residual drag-and-drop/background images/user-theme inheritance.
**Port follow-ups + #25 partial.** Dashboard recent-file rows are now line targets (single
click; cleared on close/unload; command rows cannot be targets), with code_tour (widget card)
and git_log (historical blob view) evaluated and documented as non-migrations in a new
porting section. stdin-as-a-buffer: `--stdin`/`-` plus an implicit pipe when no files, bounded
32 MiB, CRLF/EOL handling, empty valid, conflicts validated, an untitled `[stdin]` buffer with
label override/language sniff/unmodified-on-open/binary warning, file-launch session
semantics (never restored/saved/hot-exit), `tui.RunWithInput` + `tui.OpenInputTTY` additive (Run
untouched), a PTY piping e2e test, README/docs; differences recorded (no streaming
spool/daemon stdin; gvim-only `--stdin-name`). **LSP small residuals.** Preview soft wrap/toggle
(`Alt+P`, bounded 8-row cap + ellipsis, span/match remap, session default; recent-files source
recorded as a decision needing a new history snapshot); the composed plugin-overlay ordering
regression test (pipeline order verified); wrapped-cell selection recorded (hook rows have no
source line/row-source map) and dual/per-split wording tightened; R7-4's explicit unbind
tombstones now suppress the built-in overlay fallback (keymap `Unbound` + TUI `UnbindKeymap`
port + adapter, tests), with multi-key overlay chords and the completion/compositeBuffer/
text-field contexts remaining. Counts unchanged: 22 bundled/9 on/13 off, idle heap ~7.25 MiB
of 8 PASS, palette 229, plugin API 242 functions/1478 lines, API level 2.

**Round 12 outcome (2026-11-09).** Six tracks landed; the daemon MVP and architected slices are in:
**Daemon #18a MVP (complete).** `internal/daemon/{server,attach}.go` + platform resize shim: the
server owns the attach lifecycle (one client at a time, hello/resize/input/open_file/detach/ping,
a detached client's input blocks without ending the editor, output goes only to the attached
client with write-error detach, `OnProgram` repaint/resize hooks, `OnOpenFiles` forwarding,
`Close` sends exit) and the attach client (hello with the terminal size, optional open-file
frame, raw mode/restore, stdin→input, output→stdout, notices→stderr, SIGWINCH→resize). App:
`Options.Daemon/DaemonMode/DaemonOptions` with validation, the attach branch before
`requireTerminal` (no runtime/session), the serve branch (listener before `buildRuntime`,
per-root default name, socket line, detach wiring, `tui.RunWithIO(srv,srv,OnProgram)`, open-file
forwarding, session restore on start/save on exit, direct-mode/security checks), plus the single-
writer refusal for bare/session launches while a per-root daemon is live (file/--simple/--diff/
stdin launches stay direct). TUI `RunWithIO`/`Detach`/`OpenFileMessage`; CLI `--daemon[=NAME]` /
-a/`--attach[=NAME]` with validation/help; palette "Detach from Daemon" only under --daemon.
docs/architecture.md package rows + §8 daemon flow + the #19a outline; import rule R2 allows
internal/daemon to host the TUI program. Tests: daemon server forwarding/refusal/detach-reattach/
exit, CLI flag table, app start/attach/first-frame/detach/reattach/quit/session-save, single-
writer refusal + direct exceptions. Recorded MVP reductions: foreground daemon (no fork),
auto_attach start-if-absent NOT implemented (documented refusal policy instead), one client,
single workspace until #19, Windows named pipes later. **Terminal tabs.** `internal/tui/dock/
tabs.go`: a container of up to 16 single-session terminals (each with screen/scrollback/prior
transcript/selection/links/launch record), a tab strip with active/exited/click-select,
Ctrl+PgUp/PgDn quick-switch (not keymap data), palette New/Close/Next/Previous/Rename Terminal Tab
(hand-written English; 235 palette rows), per-tab close_on_exit, background tabs synced but
painted only when active; `terminal.json` gains additive `tabs[]`/`active_tab` (top-level fields
mirror the active tab, bounds and old-file migration, restored read-only/dead with no auto-
restart, Restart per tab with the saved launch record). **#20a agent sessions.**
`internal/terminal/launch.go` Launch argv/agent/resume-argv/ephemeral (bounds 64×1024, agent 64),
dock launch records + optional `Launcher/StartLaunch`, `terminal.resume_agents` (default true) and
`terminal.agents` (gvim extension, bounded/user-layer-only) config, persisted launch fields with
no auto-resume on restore, user-initiated restart precedence resume→launch→shell, agent naming in
tab titles, Run Agent... palette row + picker (hand-written English); no workspace/tmux
discovery yet. **#21a authority + SSH.** New `authority` package: the slot with four routing
surfaces (FileSystem/OneShot/LongRunning/Terminal), State/Label/Connected/Reconnect/Close, a
local backend byte-identical to the plugin OS port with trust-gate + env choke point, and a fake
for tests; Fresh-compatible scp/ssh:// parsing with typed diagnostics and path translation
(ssh:// exclusive, malformed is a startup error, remote never stat'ed, no local fallback; remote
excludes local files/root/diff/stdin/restore/--daemon); SSH backend via cached pure-Go
x/crypto/ssh (agent/identity/passphrase/password, known_hosts, POSIX-quoted one-shot exec,
long-running stdio, PTY terminal with resize/reconnect); status segment `SSH:user@host` /
disconnected (hand-written English, i18n keys reserved); reconnect-on-activate + password prompt;
remote launch UX opens no local files/session and opens the remote terminal at Workspace() with a
notice; one-shots (dock terminal/git grep/shell/formatter) routed through the slot;
docs/authority.md + architecture row. Deferred: remote file I/O (SFTP/remote-helper plan in
docs/authority.md §7), LSP routing, per-remote trust store, terminal respawn on drop, plugin
fs/proc routing, devcontainer/k8s. **i18n #22b catalogues.** `i18n/locales/*.json` embedded
(15 files, 12,646 bytes, cap test), English source of truth; 14 translated files carry the 13
mapped keys imported from Fresh via `i18n/fresh_map.json` + `tools/i18n_import.py --check` +
`tools/i18n_check.sh` CI; `ValidateCatalogue/CoverageFor` validate key shape/plurals/
placeholders/duplicates/unknown keys (coverage 100% for mapped keys); lazy loading; embedded
German rendering + fallback tests; plugin `t()` sees only its own .i18n.json; docs/i18n.md
rewritten with the English boundary (generated CommandDef/menu tables, hand-written command rows
incl. terminal-tab/Run Agent, settings labels and other in-round strings stay English).
**LSP residual sweep.** R1-3 closed (`gvim config show` trust-gates the project layer read-only
via `app.ProjectConfigTrusted`, skip note, trusted unchanged, `config paths` ungated, docs/tests);
R4-4 partial (virtual-caret session persistence `virtualLines/virtualColumn` migration-safe +
docs/internal/virtual-space-scoping.md with the exact residual list); R2-5 deferred with
docs/internal/hot-exit-store.md; R8-3 docs exact (stale bullets removed, decision list);
R11-3/R11-4 decisions recorded (kitty no seam, OSC52 read gap, live-screen reflow, background
images/drag-and-drop/user-theme inheritance, untagged painters colour fallback); R1-2 verified
closed and its stale open ranking entry removed. Counts: palette 235 (+6 rows: Run Agent + five
terminal-tab rows); bundled 22/9/13 and idle heap ~7.25 MiB of 8 unchanged; plugin API 242
functions/1478 lines; 47 registration sites; API level 2.

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

Round-9 evidence adds the updated plugin docs (`docs/plugins/`: bundled, porting, limits,
api-core, api-ui, api-buffers, and `gvim.d.lua`) with the Finder lib + ports, line targets,
fixed heights, plugin settings schema and caps; `docs/markdown.md` (residual slice + persistence),
`docs/ui.md`/`docs/keybindings.md` (search preview), `docs/lsp.md` (inline tier 2),
`docs/sidebar.md` (session state), `docs/settings.md` (plugin schema rows), and the new code:
`plugin/bundled/lib/finder.lua`, `plugin/bundled/plugins/{lsp_navigation,find_references,
dashboard,code_tour}/`, `internal/tui/editorpane/{inline_hints,compose_*}.go`,
`pluginapi/{linetargets,settings_api}.go` + `plugin/{api_linetargets,settings_api}.go` +
`internal/tui/plugin_linetargets.go`, `session/{sidebar,pageview}.go` +
`internal/restore/sidebar.go` + `internal/tui/sidebar_state.go`, `internal/tui/searchpanel/*`
preview protocol, with the round-9 tests named below.
Round-10 evidence adds the updated plugin docs (bundled/porting/api-core/api-ui/limits/
gvim.d.lua: git_grep adaptations, explorer decoration/slot API, env API/trust gate, 22-plugin
bundle and memory attribution), `docs/markdown.md` (R9-3), `docs/sidebar.md` (plugin-section
order/rows/folds), `docs/terminal.md` (slice A/link matrix/kitty/ConPTY), `docs/themes.md`
(accent audit/picker/preview/URL decision), `docs/settings.md` + `docs/trust.md` (Environment
rows/Detectors/environment gating), `docs/lsp.md` (cancel/process limits/universal decision),
and the new code: `env/{provider.go,...}`, `config/env.go`, `trust/env.go`,
`pluginapi/{env,explorer}.go` + `plugin/{api_explorer,env_api}.go`, `internal/tui/
{plugin_env,plugin_explorer,plugin_sidebar_state,theme_picker,settings_theme_preview}.go`,
`internal/tui/dock/hover.go`, `lsp/limits{,_linux,_other}.go`, `editor/compose_setext.go`,
the env_manager/git_explorer/git_grep port dirs, and the round-10 tests named below.

Round-11 evidence adds the new docs (i18n.md, terminal ConPTY/OSC52/reflow, themes provenance,
plugins/porting line-target section, settings locale, README/CLI stdin, ui/keybindings preview
wrap/unbind, markdown decisions, lsp wording) and the new code: `i18n/` (package +
catalogues/en.json), `internal/daemon/{protocol,socket}.go`, `config/{locale,clipboard}.go`,
`internal/app/{stdin,settings_locale,theme preview?}`, `internal/tui/{tty.go,RunWithInput,
paintInto/theme_inspect provenance,style/roles.go,theme_picker}`, `internal/tui/grid`
key-capture, `internal/terminal/{pty,pty_windows,osc52,reflow}.go`, `editor/{stdin,i18n}
statuses`, plus the round-11 tests named in the tracking block.

Round-12 evidence adds docs/authority.md, the architecture daemon §8 + authority rows, terminal
tabs/agents/resume_agents docs, docs/i18n.md (catalogues/import/validation/English boundary),
docs/internal/{virtual-space-scoping,hot-exit-store}.md, docs/trust.md config viewer,
docs/settings.md §6, docs/themes.md §8 and the new code: `internal/daemon/{server,attach}
*.go`, `authority/`, `internal/app/{daemon_runtime,authority_app,terminal_agents,config_trust}.go`,
`internal/terminal/{launch,session_backend}.go`, `internal/tui/dock/tabs.go` + agent picker/
authority status, `i18n/{catalogue.go,fresh_map.json,locales/*}`, `tools/i18n_*`,
`commands/terminal_commands.go`, plus the round-12 tests named in the tracking block.

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
| Palette commands (runtime) | 206 core (Fresh's `CommandDef` table) | 235 | 206 generated + 29 non-generated: 7 hand-written native rows + 4 plugin-management + 3 round-4 + 2 round-5 + 2 round-6 + 5 round-8 Markdown mode rows + 6 round-12 rows (Run Agent..., New/Close/Next/Previous/Rename Terminal Tab); `commands/palette_test.go` pins 235. The daemon adds a "Detach from Daemon" row only under --daemon (not counted). Plugin-command rows appear at run time only while their plugin is enabled (22 plugins, 47 registration sites). All rows have a handler or a documented reason. |
| Documented "not applicable" commands | n/a | 4 | `internal/tui/notice_na.go` (detach, update, update log, wave). Inspect Theme now runs (round 5), and the round-1 trust command runs. |
| Menus / menu actions | 8 / 103 static actions, 31 separators, 2 static submenus + 1 dynamic | 8 / 103 static actions, 31 separators, 2 static submenus + the dynamic theme submenu | Static parity is exact against Fresh's `translated_menus()` (103 actions / 31 separators / 2 static submenus); the dynamic "Copy with Formatting" submenu is filled at run time from the theme catalog (`internal/tui/menubar/dynamic.go`). Round 5 adds plugin-contributed rows (`gvim.menu.add_item`, `internal/tui/menubar/plugins.go`, at most 32 rows, owner-cleaned). Help's label row says "gvim" where Fresh writes its version. |
| Keymap presets | 5 | 5 | Same names and inheritance graph. Round 7 closed R2-4: the overlay contexts (`prompt`, `searchPrompt`, `popup`, `menu`, `settings`, `dock`) consult the keymap before built-ins with fall-through; own/in-effect counts are now default 245/243, macos 60/266, macos-gui 52/317, emacs 98/309, vscode 9/248 (`keymap/common_set_test.go`, `docs/keybindings.md`). `completion`/`compositeBuffer` are not emitted; residual R7-4. |
| Bundled plugins | 50 | 22 (9 on by default) | Round 10 added git_grep/git_explorer/env_manager (all opt-in); round 11 added no port (dashboard got line targets). 13 off: flash, vi_mode, live_diff, git_log, compare_files, merge_conflict, dashboard, code_tour, lsp_navigation, find_references, git_grep, git_explorer, env_manager. `docs/plugins/bundled.md`. |
| Bundled plugin commands | ~180 | 47 registration sites | unchanged in round 11 (dashboard's line-target migration adds no command; git_grep 2, env_manager 4, compare_files registers 8 through one table, lsp_help 0). |
| Themes | 10 built-in themes, theme files by name/path/URL/`builtin://`, `extends` inheritance, attributes on every color, theme editor, colour picker/mouse editing, inspect-at-cursor, plugin theme overrides, per-cell provenance, background images | 10 built-ins + user themes (round 5); plugin theme-file APIs (round 6); accent audit/picker/preview (round 10) | Round 5 registry/forms/extends/fallbacks/named colours/inspect/native editor/overrides; round 6 plugin theme-file APIs. Round 10: the six bundled themes no longer hand-pick accents (`ui.accent_*` falls back to popup selection; theme+style audit tests), the Settings theme row gains four-chip previews, and the colour picker/mouse editing overlay lands. URL/packaged themes: decision recorded, keep the clear refusal (no network code/fetch until packaging/network policy). Residuals R10-4: per-cell painted-key provenance, background images, drag-and-drop theme values. |
| LSP server entries | built-in + plugin helpers + universal servers | 62 built-in entries; universal_lsp is config-only | `lsp/manager/defaults.go`; gvim routes lists/universal entries and draws hints/tokens/folds/diagnostics, but no built-in universal server ships (Fresh's quicklsp is a separate user-installed Rust binary — decision recorded; user-configured universal_lsp works fully). `process_limits` is enforced on Linux (cgroup v2/ulimit fallback) and recorded as unreachable on macOS/Windows. |
| Bundled plugin idle heap | n/a | about 7.25 MiB of the 8 MiB gate (9 default-enabled; ~9.3 MiB session baseline) | `TestBundledPluginsIdleHeap`, `docs/plugins/bundled.md`; round 11 measured ~7.25 MiB PASS (host growth from the round-10 wave), no new enabled plugin. |
| Plugin API surface | ~372 methods | 242 functions | `docs/plugins/gvim.d.lua` is 1478 lines, API level 2; round 11 added no new API method (i18n/locale wiring feeds the existing `gvim.i18n`; line targets are the round-9 API). Remaining gaps: R9-5 (composite substitution/dock/per-panel/windowEmbed/markdown-in-text), package manager/HTTP, project-local plugins. |
| Settings page | schema-driven page (~14k LOC in refs/fresh) over every setting | 69 settings in 14 sections + injected Languages entries/Language Servers/Plugins/Keybindings/Environment/Detectors pages | Rounds 3–10 built the page/structured editors/reset/Reload/Contents, non-scalar plumbing, Language Servers/Environment and Plugins/Keybindings, Environment/Detectors, theme preview; round 11 the Interface locale row (null = auto, ApplyLive into i18n + plugin runtime). Round 12 adds no page row; `terminal.agents`/`resume_agents` are config-only (gvim extension/user layer) and the i18n locale row now switches the shipped catalogues. Residuals R8-3 (docs exact: declared plugin settings read-only + raw-config editing rationale, no gallery, whole-array LSP JSON, no in-page key capture, no env activation, no per-field reset/inherit/undo, Reload palette-row only, no generic widget runtime). |
| Terminal | PTY terminal panes and utility dock, terminal tabs (up to 16) with strip/rename/close_on_exit/persistence, live/scrollback modes per tab, keyboard capture, clickable paths (OSC 7), launch records + agent resume (Run Agent picker), restart exited process, shell override, send selection, mouse forwarding/drag-select, drag auto-scroll, word/line drag, Ctrl+hover file:line:col links, pass_ctrl_v_without_text, close_on_exit, ConPTY on Windows, remote SSH terminal via the authority slot | **partial** (slices A+B; R3-2 closed; round-10 slice A; round-11 completion; round-12 tabs/agents/remote terminal) | `config/terminal.go` (shell/agents/resume_agents), `internal/terminal/{scrollback,mouse,saved,launch,session_backend}.go`, `internal/tui/dock/{selection,transcript,scrollback,input,panel,render,ports,hover,tabs}.go` + `dock_glue.go`, `internal/restore/terminal.go`, `internal/uiadapter/terminal.go`, `authority` (SSH terminal), `docs/terminal.md` | Round 10 slice A (drag/word-drag/hover/pass_ctrl_v), round 11 completion (ConPTY compile-verified/unverified, OSC52 write opt-in/trusted, transcript reflow), round 12: tabs container (16 max, strip/exited/click-select, Ctrl+PgUp/PgDn, palette rows, per-tab close_on_exit, additive SavedState tabs/active_tab with bounds/old-file migration, restored read-only dead), launch records + agent metadata/resume (no auto-resume, local executables), remote SSH terminal through the authority slot. Residuals R11-3/R12-2: kitty keyboard, OSC52 read, live-screen reflow, live ephemeral PTY restore, workspace-scoped tabs (#19), tab drag/reorder/per-tab splits, agent re-exec edges, report-split close_on_exit. |
| Explorer options | `file_explorer.width/side/follow_active_buffer/auto_open_on_last_buffer_close/compact_directories/preview_tabs/…` + sticky parents | 5 options + sticky parents implemented | width percent/columns with grip, left/right side geometry, follow + reveal, auto-open on last close, compact chains, sticky band; side toggle session-only and 4 keys accepted-ignored (R3-3). |
| Finder / Live Grep | file preview, Live Grep toolbar (case/word/regex, files/buffers/ignored/terminals/workspaces), quickfix, streaming | preview column + 6 Live Grep toggles | `internal/tui/palette/{preview,grep}.go`; deferred quickfix, terminals/workspaces providers, streaming, Live Grep row preview, Resume cache (R3-4). |
| Project search / replace | plugin Search & Replace panel (match tree, scoped replace, globs, quickfix) + Git Grep | native panel + Git Grep (rounds 4–6) | Rounds 4–5 landed the panel/ports/checkboxes/scoped replace/globs/list export/history; round 6 added the selected-match preview column and multi-line match rows (exact ranges kept in replace): `internal/tui/searchpanel/{draw,panel,types}.go`, `docs/ui.md`. Residuals R6-3. |
| e2e feature index | 413 test files | not comparable | gvim uses Go unit tests; Fresh's file names are the feature index used below. |

## 3. Matrix by area

### 3.1 Engine and editing

| Area | Fresh features (evidence) | gvim status | gvim evidence | Notable sub-gaps |
|---|---|---|---|---|
| Text model, large files, encodings | rune text, undo/redo, two-mode large-file support, encoding detect/convert (UTF-8/16, Latin-1, CP125x, GBK/GB18030, Shift-JIS, EUC-KR), EOL detect | **implemented** | `buffer/`, `document/`, `encoding/`, `editor/large_*`, `editor/files_lineending*`, `docs/architecture.md`; Fresh `docs/features/encoding.md` | None found. Detection heuristics may order differently; per-file reload uses the same palette command. |
| Editing core, smart editing, vi | auto-indent/close/surround, smart home/backspace, bracket match, comment, case, transpose, sort/trim, shell command, Markdown source smart editing + page-view compose/fences/tables/dual/conceals, vi-mode plugin | **implemented** | `document/smart*.go`, `editor/comment.go`, `document/case.go`, `editor/shell.go`, `editor/markdown.go`, `internal/tui/editorpane/{compose,compose_fence,compose_table,compose_setext,pageview}.go`, `plugin/bundled/plugins/vi_mode`; Fresh `docs/features/editing.md` | Markdown compose/fences/tables (r6–7), dual/borders (r8), residual slice (r9: wrapped cells/continuations, escaped pipes, container fallback, fence rails, heading conceal/scrollbar marks, page-view persistence), round-10 polish (quote-container fences, setext styling/conceal with thematic-break distinction, task-list glyph conceal, image alt-text conceal, inline-HTML literal decision, mapped-decor no-paint decision, horizontal clamp). Remaining Markdown gaps are R9-3/R10-2's residual list. Fresh's paragraph motions and "insert/open line" exist; replacement-mode (`R`) is documented as unsupported in the vi plugin. |
| Selection, multi-cursor, marks | multi-cursor (Ctrl+D, above/below, line ends), block selection, mark mode, mouse word/line/drag selection | **implemented** | `document/cursors*.go`, `document/block.go`, `editor/marks.go`, `internal/tui/motion_mouse.go`, keys in `keymap/default.json`; Fresh `docs/features/editing.md` | `add_cursors_to_line_ends` is not bound by default (present in Fresh's preset); Fresh's double-click word drag is implemented. |
| Undo, save pipeline, file metadata | one-undo-step edits, atomic saves, interrupted-save recovery, permissions/read-only, final-newline/trim on save, auto-save, auto-revert | **implemented** | `document/save_plan.go`, `atomicfile/`, `editor/files_recovery.go`, `editor/interrupted.go`, `internal/restore`, `docs/architecture.md` | Fresh's `sudo save prompt` is missing (no `sudo` path found); permission/ownership preservation is present. |
| Search & replace (in-buffer) | find, replace, query replace (y/n/!/q), find in selection, selection next/prev, case/whole-word/regex toggles, confirm-each, search history, match counts | **implemented** | `search/`, `internal/tui/find/`, `internal/tui/find_route.go`, tests `find_*_test.go`; Fresh `docs/features/search-replace.md` | Project-wide search/replace and Git Grep landed rounds 4–9 (next row); remaining search-panel residuals are the closed oversize-file streamed writer, provider sources beyond files/open buffers/current buffer, and a preview wrap/toggle (R9-2). |
| Navigation, bookmarks, macros | position history, go to line/bracket, bookmarks 0-9 (set/jump/list), macros 0-9 + save-to-init/promote-to-command | **partial** | `editor/nav*.go`, `editor/goto.go`, `editor/marks.go`, `internal/tui/root_bookmarks.go`, `internal/tui/macro.go`; Fresh `docs/features/editing.md` | `List Bookmarks` is a stub ("editor does not list its bookmark slots", `internal/tui/root_commands.go:16`). One macro slot only: record/play/list work, `Macro: Save to init.ts`/`Promote to command` write an `init.lua` block (Fresh writes `init.ts`), no macro registers 0-9. |
| Display & view options | line numbers/relative, wrap + wrap_column + wrap indent, page view, virtual space (block/vertical), rulers, indent guides + rainbow, whitespace marks incl. newlines/CR, current line/column, occurrence, post-EOF tilde, scrollbars + markers, smooth scroll, inline diagnostics + staleness, inlay hints, semantic tokens, Markdown page view (compose, fences, tables, dual split, wrapped cells, container fences, setext/task/image conceal), animations, screensaver | **partial** (R2+R4+R5+R6+R7+R8+R9+R10 landed the user-visible subset) | `document/{virtualspace,block}.go`, `editor/{virtualspace,block,smart}.go`, `internal/tui/editorpane/{inline_diag,virtual_block,virtual_lines,compose,compose_layout,compose_fence,compose_table,compose_setext,pageview,rows_extra,conceal,inline_hints}.go` + `syntax/markdown_{markers,fences,tables}.go`, `internal/tui/{virtual_space,pageview,dual}.go`, `docs/markdown.md`; Fresh `docs/features/editing.md` | Round 9 landed inline-hint tier 2; round 10 added quote-container fence framing + per-line literal fallback, setext styling/conceal, task-list glyph conceal, image alt-text conceal, inline-HTML literal decision, the mapped-decorations no-paint decision, the horizontal-page clamp decision, the dual line-anchor approximation/one-dual policy, and sidebar plugin-section state. Remaining: R9-3/R10-2 residuals (exact byte dual anchors, native menu row, per-section widths/drag order, mapped ordering test); R4-4 virtual-space limits; style-only keys out of `ViewOptions` (§4 item 9). |
| Folding | indent folds + LSP `foldingRange` + outline/Contents folds + plugin namespace folds, fold indicators, fold-first toggle, per-buffer toggle | **implemented** core; all four sources | `editor/fold.go`, `document/fold.go`, `editor/{lsp_folds,outline_folds}.go` + `internal/uiadapter/{lsp,outline}_folds.go`, `internal/tui/plugin_folds.go`, `internal/tui/editorpane/` gutter; tests `decorations/tier1c_test.go`, `plugin/decor_tier1c_test.go`, `editor/{lsp,outline}_folds_test.go` | Round 5 LSP ranges, round 6 Contents outline source, round 7 plugin namespace store, round 8 pane integration + fold-first toggle, round 9 live `contents.fold_buffer` + hidden-section publication. Residuals: a source that changes ranges without an edit refreshes on the next edit/open; no fold command (Fresh parity). |
| Splits, tabs, panes, windows | splits (vertical/horizontal), tab strip, tab drag, preview tabs, buffer groups (fixed-height leaves), split labels, per-buffer window state, line targets, multiple windows/workspaces | **partial** | `internal/tui/split*`, `internal/tui/tabs.go`, `internal/tui/tab_drag.go`, `internal/tui/tab_drop.go`, preview tabs in `internal/tui/model.go`, plugin group-lite in `internal/tui/plugin_groups.go` + `pluginapi/buffergroup.go`, 1c-C seams in `internal/tui/{plugin_seams,plugin_events}.go`, line targets in `internal/tui/plugin_linetargets.go` + `pluginapi/linetargets.go`; Fresh `docs/features/navigation.md` | Round 6 groups, round 8 1c-C seams + ratio fix; round 9 added fixed-height group leaves (`{type="fixed",height=N}`, 1–100 rows, re-applied on layout change; git_log's height-1 toolbar renders one row) and line targets (Enter/click opens `path` at a 0-based line, `into` a labelled split, 10k/buffer cap, owner cleanup). Still open: no synthetic group tab/per-panel views, no second window/workspace model; "Move tab to other split"/"Extract Tab to New Workspace" are notices (R9-5/R8-1). |
| Compare files | `fresh --diff A B`, editable side-by-side, change copy/take, next/prev difference, switch side, scrollbar marks, plugin compare API (compareBuffers/replaceRange) and the compare_files plugin | **implemented** (native view + plugin API + opt-in plugin port) | `internal/tui/compare*.go`, `docs/compare.md`, `compare_*_test.go`, `internal/cli` `--diff`, `pluginapi/compare.go` + `internal/pluginhost/compare.go` (CompareResult/DiffSource/DiffHost, one-edit/one-undo replaceRange), `plugin/compare_geom.go` + tests, Lua `gvim.diff.compare_buffers/replace_range`, `plugin/bundled/plugins/compare_files/` (8 commands, group-lite splits, hunk-anchored scroll sync, copy/take, opt-in); Fresh `docs/features/compare-files.md` | Native Compare is gvim's default surface for Fresh's Compare Files (two file tabs, fillers, hunks, take/copy, synced scroll, large-file refusal; §4 item 1 for presentation). Round 8 added the plugin API and the port: group-lite splits of the two file buffers (opened as ordinary tabs when needed; Close closes only ones it opened when unmodified), hunk-anchored scroll sync, Fresh theme decorations, filler rows gated on `decor_virtual_paint`, copy/take through replace_range (one undo per merge), binary/large refusal, 8 Fresh commands/no default keys, opt-in for the heap gate (enabling duplicates the native palette rows). Residuals R8-4: no synthetic group tab, no EOL-only hunks (EOL normalisation), filler rows need the paint capability, positional pairing fallback, ui.input instead of Fresh's picker, no `--diff` launch hook/script-facing export. |

### 3.2 Project, chrome and settings

| Area | Fresh features (evidence) | gvim status | gvim evidence | Notable sub-gaps |
|---|---|---|---|---|
| File explorer and sidebar | tree, file create/delete/rename/copy/cut/paste, hidden/gitignored toggles, git status marks, plugin rows/decorations/slots, context menu, async reload, width/side, follow active buffer, compact chains, sticky parents, sidebar sections, session state, Contents fold_buffer | **partial** (R3 options; R1 Contents; R6 fold_buffer; R9 session state + live fold; R10 explorer host API + plugin-section state) | `explorer/{options,sticky}.go`, `internal/tui/explorer/` (`RowDecor` painter), `internal/tui/{root_explorer,markdown_toc}.go`, `internal/tui/sidebar/`, `internal/tui/{sidebar_state,plugin_sidebar_state}.go` + `session/sidebar.go` + `internal/restore/sidebar.go`, `pluginapi/explorer.go` + `plugin/api_explorer.go`, `config/contents.go`, `outline/`, `docs/sidebar.md` | Round 9 persisted sidebar UI state (width form/side, explorer fold/path/scroll, Contents after first scan, Diagnostics); round 10 closed R9-4's plugin-section persistence (stack order/requested rows/folds bounded and migration-safe, applied when sections mount) and added the plugin explorer decoration/slot host API with the panel painter (leading/trailing slots, suppress flags, filename colour, priority/namespace resolution, owner cleanup, theme-key/RGB colours) — the git_explorer port consumes it. Residuals R10-2: per-section widths and drag-and-drop section order (shared-column model + right-side floor documented), exact R9-4 leftovers; `respect_gitignore`/`custom_ignore_patterns`/`preview_tabs`/glyph keys accepted-ignored; side toggle session-only. |
| Finder, quick open, live grep | Ctrl+P modes (file/command/line/buffer), fuzzy ranking, streaming enumeration, finder preview, live grep with provider toggles (case/regex/word/files/buffers/ignored/terminals/workspaces), quickfix export, resume | **partial** (R3 landed the preview and toggles) | `finder/finder.go` (`EnumerateAll`, hidden/ignored walk), `internal/tui/palette/{preview,grep,palette,draw}.go`, `internal/app/{preview,live_grep}.go`, `docs/ui.md` palette details, tests `palette/{preview,grep,palette}_test.go`, `internal/app/live_grep_test.go`; Fresh `docs/features/command-palette.md`, `search-replace.md` | Round 3 landed: the file-mode preview column (head of the selected file, 16 KiB / 12 lines, `path:line:col` parsing, target line highlighted, binary/empty/unreadable placeholders, dropped under 64 inner cells) and Live Grep's toolbar — case (seeded by `editor.search.case_sensitive`), regex (on by default as Fresh's), whole word, files, buffers (1 MiB snapshot per buffer) and ignored/hidden sources, with match counts and error rows. Still open: quickfix export, terminals/workspaces providers (need the plugin/results-panel work), streaming provider model, preview for Live Grep rows (file mode only), syntax colours/scrolling in the preview, and Resume re-runs the search instead of restoring cached matches (R3-4). |
| Project-wide search, replace and Git Grep | plugin Search & Replace panel: match tree, scoped replace, globs, current-file/all-files/open-buffers, 10k cap, unsaved buffers, quickfix export, search history, selected-match preview with scrolling/syntax/soft wrap, multi-line matches, streaming results; git grep finder with case toggle and pathspec field; bundled git_grep plugin | **implemented** core; **partial** against the plugins (R4–R11) | `search/plan.go`, `internal/app/{projectsearch,gitgrep}.go`, `internal/tui/searchpanel/{glob,types,panel,draw,preview}.go` + `internal/tui/project_search.go`, `internal/tui/workspace_edit.go`, `plugin/bundled/plugins/git_grep/`, `commands/search_commands.go`, `docs/keybindings.md`, `docs/ui.md`; Fresh `plugins/search_replace.ts`, `plugins/git_grep.ts` | Round 9 previews/large-file distinction/Open Buffers; round 10 ported git_grep; round 11 added preview soft wrap/toggle (`Options.PreviewWrap`, `Alt+P` in panel and editor, wrapped rows with syntax-span/match remap, 8-row cap + ellipsis, display-row window, session-persisted) and recorded the recent-files provider decision (no app-level opened-file history; needs a history snapshot + streaming/replace semantics). Residuals R9-2: closed-oversize streamed writer, provider sources beyond files/open/current (diagnostics/terminal need provider-specific snapshots); R10-1 git_grep reductions. |
| Command palette and command table | 206 core commands + plugin commands, contexts/availability, prefix modes, key hints | **implemented** core / **partial** plugin commands | `commands/` (206 generated + native rows), `internal/tui/palette/`; Fresh `docs/features/command-palette.md` | The generated core table is exact (206/206). The runtime palette has 229 rows (unchanged in rounds 9–10): 206 generated + 23 non-generated (7 native + 4 plugin-management + 3 r4 + 2 r5 + 2 r6 + 5 Markdown mode rows) — `commands/palette_test.go`. Bundled plugin commands add rows only while their plugin is enabled (47 registration sites across twenty-two). |
| Menu bar, context menus, help, welcome | 8 menus, 103 static actions, dynamic theme submenu, mnemonics/underlined accelerators, context menus, help overlay, welcome screen, dashboard | **implemented** menus/help/welcome; dashboard **missing** (plugin) | `internal/tui/menubar/{menu_table_gen,menubar,dynamic}.go`, `keymap/menu.go` (`Map.MenuMnemonic`), `internal/tui/menu_mnemonic_test.go`, `internal/tui/menubar/mnemonic_test.go`, `internal/tui/context_menu.go`, `internal/tui/help.go`, `internal/tui/welcome.go`, `docs/ui.md` §6; Fresh `docs/internal/editor-ux-features.md`, `docs/features/dashboard.md` | Menus match Fresh's static table exactly (103 actions, 31 separators, 2 static submenus), and the dynamic "Copy with Formatting" submenu lists the 10 bundled themes (`menubar/dynamic.go`). Round 2 added the accelerators: underlined keymap-assigned letters and Alt+letter through ordinary keymap resolution, F10 for the bar, `editor.menu_bar_mnemonics` off removes both; dropdown rows carry no mnemonics (as in Fresh). Help's label says "gvim" instead of Fresh's version string. Residual: the dashboard plugin (backlog #14), not the menu system. |
| Status bar | configurable element lists (Fresh's defaults: left `{trust} {remote} {terminal_restart} {cursor} {diagnostics} {cursor_count} {messages}`; right `{read_only} {line_ending} {encoding} {language} {lsp} {warnings} {update} {palette}`), separator, per-element click actions, squeeze rule, `{filename}`/`{chord}`/`{clock}` | **implemented** (R2; parsed-but-unpainted elements documented) | `internal/tui/frame/frame.go` `StatusRuns`/`DefaultStatusLeft/Right`, `internal/tui/model.go` `statusView`/`statusRow`, `internal/tui/status_elements.go` (filename/chord/clock/tick), tests `statusbar_elements_test.go` + `statusbar_residual_test.go`, `docs/ui.md` §5; Fresh `config.rs` `StatusBarConfig`, `view/ui/status_bar.rs` | R2-3 closed in round 5: `{filename}` (workspace-relative or absolute path, document name when pathless, Fresh's " [+]" / " [RO]" markers), `{chord}` (shown only while a multi-key sequence waits, gvim's label style, muted — §4 item 22) and `{clock}` (24-hour `HH:MM`, static colon as Fresh's code — §4 item 23; a minute tick redraws while configured, a config change takes effect next launch). The filename resolves the root and the document through canonicalFilePath (the /private/var vs /var class) and falls back to the opened spelling outside the root (integration fix, statusbar_residual_test + status_filename_app_test). Still unpainted: plugin tokens `{plugin:token}`; `{warnings}`/`{update}` are deliberately empty (no warning-count domain/updater). Plugin status items keep their anchors. |
| Config layers and Settings UI | 4 layers, per-buffer overrides, config write from toggles, Settings UI, `init.ts`, config reload, LSP/plugin/keybinding/env/locale sections | **partial** (page surface complete; r11 locale; r12 config additions) | `config/{settings_schema,settings_structured,contents,patch,env,locale,clipboard,terminal}.go`, `internal/app/settingssvc.go`, `internal/app/settings_*.go`, `internal/tui/{settings*,settings_theme_preview}.go`, `i18n/`, `docs/settings.md` | Rounds 3–11 built the page/structured editors/reset/Reload/Contents, non-scalar plumbing, Language Servers/Environment, Plugins/Keybindings, declared-settings rows, Environment/Detectors, theme preview and the Interface locale row (live). Round 12 adds the config `terminal.resume_agents` (Fresh key, default true) and `terminal.agents` (gvim extension, user-layer only, bounded) plus `clipboard.use_osc52` (r11). Residuals R8-3 as documented (docs/settings.md §6 exact). |
| Keymaps and keybinding editor | 5 presets, user keybindings, unbind/noop, chords, custom keybinding maps, keybinding editor, overlay contexts | **partial** (R2 data; R7 overlay contexts; R11 unbind tombstones) | `keymap/*.json`, `keymap/common_set_test.go`, `keymap/menu.go`, `internal/keybindings/` (`Unbound` adapter), `internal/tui/overlay_keys.go`, `internal/tui/keybind_editor.go`, `docs/keybindings.md` | Round 7 closed R2-4 (overlay contexts with fall-through; own/in-effect default 245/243, macos 60/266, macos-gui 52/317, emacs 98/309, vscode 9/248; loader warns/skips multi-key overlay bindings). Round 11's explicit unbind tombstones (`keymap.Map.Unbound`, TUI `UnbindKeymap` port) let an overlay's built-in fallback be suppressed for an unbound chord. Residuals R7-4: multi-key bindings are not consulted in overlay contexts (one-press router; pending-chord state is root-only), and the completion/compositeBuffer/text-field contexts stay outside the router (Fresh implements them per surface). |
| Themes and appearance | 10 built-in themes, theme files by name/path/URL, extends, colour picker/mouse editing, inspect with per-cell provenance, plugin theme overrides, background images | 10 built-ins + user themes; plugin theme-file APIs; accent/picker/preview r10; provenance r11; r12 decisions | `theme/`, `internal/tui/{theme_editor,theme_picker,theme_inspect}.go`, `internal/tui/style/{style,roles}.go`, `internal/tui/grid` capture, `internal/app/settings_theme_preview.go`, `docs/themes.md` | Rounds 10–11 landed the accent audit, Settings four-chip preview, colour picker/mouse editor and first-surface painted-key provenance. Round 12 recorded the remaining decisions: background images are outside the colour theme model; drag-and-drop of theme values is not added; user-theme inheritance is one level of extends to a built-in theme (Fresh's own limit); untagged compose/page-view/plugin painters keep the documented "matched by colour" fallback. Remaining R11-4 items are decision-level, not missing keys. |

### 3.3 Services

| Area | Fresh features (evidence) | gvim status | gvim evidence | Notable sub-gaps |
|---|---|---|---|---|
| LSP | diagnostics, hover, definition/implementation/references, completion, rename, code actions + server-initiated file ops, signature help, format, folding ranges, inlay hints (inline tier 2), semantic tokens, diagnostics panel, quickfix list, inline diagnostics + staleness, multi-server with only/except, universal servers, per-server status popup/mute, pull diagnostics (cancelable), workspace diagnostics for unopened files, progress, process limits (Linux), env/init options, root markers, C header routing | **partial** (#7 A–E complete; rounds 7–11 landed workspace edits/diagnostics/progress, inline tier 2, cancellation, process limits and the small residuals) | `lsp/{client,handler,requests,capabilities,progress,raw,limits}.go`, `lsp/manager/{manager,router}.go`, `internal/lsphost/{lsp,lsp_data}.go`, `internal/tui/{lsp,lsp_data,workspace_edit,diagpanel}.go` + `internal/tui/editorpane/inline_hints.go`, `editor/{lsp,outline}_folds.go`, `commands/lsp_commands.go`, `docs/lsp.md` | Rounds 4–10 landed the router/edits/status/staleness, hints/tokens/folds/pull diagnostics, panel/quickfix/progress, workspace edits/diagnostics, inline tier 2, cancel and process limits. Round 11 closed the small LSP residuals: preview soft wrap/toggle (`Alt+P`, 8-row cap + ellipsis, span/match remap, session default; recent-files provider recorded as needing a new history snapshot), the composed plugin-overlay ordering regression test, wrapped-cell/dual wording decisions, and explicit unbind tombstones for the overlay fallback. R7-5 residual is only the deliberate universal-server decision (no built-in quicklsp; user-configured universal_lsp supported) + the per-OS process_limits caveat; the stricter-than-Fresh stop-on-refused workspace-edit model stays (§4 item 18). |
| Plugins runtime and host API | TypeScript on QuickJS, sandbox-lite with per-plugin contexts, ~372-method API, overlays/virtual text/conceals/soft breaks/folds/line targets, explorer decorations/slots, project environment, prompts/toolbars, modes/contexts, windows/splits (fixed heights), virtual+composite buffers, buffer groups, terminals, authority/remote, LSP requests, HTTP, widgets/floating/dock panels/sidebar sections, i18n, package manager, menus, events | **partial, runtime different; API level 2 (tiers 1a+1b+1c + round-10 edge APIs)** | `plugin/`, `pluginapi/` (incl. `widgetapi.go`, `buffergroup.go`, `compare.go`, `linetargets.go`, `settings_api.go`, `explorer.go`, `env.go`), `internal/pluginhost/`, `internal/tui/plugin*.go` + `pluginpanel/`, `plugin_groups.go`, `plugin_themes.go`, `plugin_folds.go`, `plugin_seams.go`, `plugin_linetargets.go`, `plugin_{explorer,env}.go`, `decorations/`, `env/`, `docs/plugins/` (6 API pages + `events.md`, 1478-line `gvim.d.lua`) | Lua 5.1 is the permitted runtime difference; API level 2. Rounds 5–9 added tiers 1a/1b/1c; round 10 added `gvim.explorer` decorations/slots (namespaced, caps, priority resolution, owner cleanup, host painter) and `gvim.env` + the host detection/activation gate (`env.detectors`, trust-gated overlay, spawn injection) with `env_changed`/`trust_changed` events. Remaining (R9-5/R10-1): composite API native substitution, dock/fullscreen placement, independent per-panel views, windowEmbed live panes, markdown-in-text, package manager/HTTP/project-local plugins, render_line; port reductions are R10-1. |
| Bundled plugin feature set | 50 plugins: git_log, live_diff, git_grep, git_explorer, git_blame, git_gutter, git_statusbar, git_find_file, search_replace, diff_nav, diagnostics_panel, code-tour, dashboard, welcome*, theme_editor, markdown_toc, markdown_compose, markdown_source, path_complete, lsp_help, lsp_navigation, find_references, compare_files, flash, vi_mode, merge_conflict, tab_actions, goto_with_selection, env-manager, devcontainer, k8s-workspace, agent_*/orchestrator/live_sessions, pkg, audit_mode, language helpers | **partial** | `plugin/bundled/plugins/` (22; 9 default-enabled), `plugin/bundled/lib/{finder.lua,git.lua}`, `docs/plugins/bundled.md`; Fresh `plugins/*.ts` | Ported: diagnostics_panel (r6), git_log/live_diff (r7; full paths r8), compare_files (r8, opt-in), path_complete/lsp_help (r8), lsp_navigation/find_references/dashboard/code_tour + Finder lib (r9), git_grep/git_explorer/env_manager (r10, all opt-in). Missing: devcontainer, k8s-workspace, orchestrator/agent sessions/live_sessions, pkg, audit_mode, language helpers; native substitutions recorded: markdown_compose/source, theme_editor, search_replace, composite buffers, dashboard start tab, live_grep/diff_nav/welcome candidates. Remaining ports are daemon-era (gated on #18–#21) or packaging-dependent (pkg on #16). |
| Terminal | PTY terminal panes and utility dock, terminal tabs (≤16) with strip/rename/per-tab close_on_exit/persistence, live/scrollback modes per tab, keyboard capture, OSC 7 paths, launch records + agent resume + Run Agent picker, restart exited process, shell override, send selection, mouse forwarding/drag-select, drag auto-scroll, word/line drag, Ctrl+hover file:line:col links, pass_ctrl_v_without_text, ConPTY on Windows, remote SSH terminal | **partial** (slices A+B; R3-2 closed; r10 slice A; r11 completion; r12 tabs/agents/remote terminal) | `config/terminal.go`, `internal/terminal/{scrollback,mouse,saved,launch,session_backend}.go`, `internal/tui/dock/{selection,transcript,scrollback,input,panel,render,ports,hover,tabs}.go`, `internal/restore/terminal.go`, `internal/uiadapter/terminal.go`, `authority/`, `docs/terminal.md` | Round 12 landed the tabs container (16 max, active/exited/click-select strip, Ctrl+PgUp/PgDn, five palette rows, per-tab close_on_exit, additive SavedState tabs/active_tab with top-level mirror + bounds + old-file migration; restored tabs read-only/dead with no auto-restart), launch records + agent metadata/resume (no auto-resume on restore, local executables), and the remote SSH terminal through the authority slot. Residuals R11-3/R12-2: kitty keyboard, OSC52 read, live-screen reflow, live ephemeral PTY restore, workspace-scoped tabs (#19), tab drag/reorder/per-tab splits, report-split close_on_exit, agent re-exec edges. |
| Git surfaces | review diff (stage/unstage/discard/comments/export), range/stash reviews, git log with live diff, git blame panel with history, live diff baselines, diff chunk nav, git grep, git explorer, git find file, statusbar branch, gutter marks | **partial** | `gitstate/`, `plugin/bundled/plugins/git_gutter`, `git_statusbar`, `git_find_file`, `git_blame`, `merge_conflict`, `internal/tui/git.go`; Fresh `docs/features/git.md` | Missing: Review Diff (/range/stash), Git Log, Live Diff, Git Grep, Git Explorer, blame panel/history stepping (gvim has a reduced inline blame), merge view (reduced to in-buffer resolution), diff hunk sources beyond the HEAD gutter, review comments/export. Implemented: gutter hunk marks + next/prev change, branch statusbar, git find file, merge-conflict resolution, compare-as-difftool. |
| Session, restore, daemon | session restore/save, hot exit, crash recovery, ephemeral-file patterns, per-file pins, --restore/--no-restore/--simple, daemon mode with attach/detach and open-file forwarding, direct-vs-daemon launch modes | **partial** (R2 hot exit/restore; round-12 daemon #18a MVP complete) | `internal/app/{session_plan,session_open,hotexit,daemon_runtime}.go`, `internal/app/run.go`, `internal/daemon/{server,attach,protocol,socket}.go`, `internal/cli/{root,args}.go`, `internal/tui/model.go` (RunWithIO/Detach/OpenFileMessage), `session/`, `internal/restore/session.go`, `docs/architecture.md` §8 | Round 12's daemon MVP: `gvim --daemon [NAME]` hosts the editor in the foreground on a Unix socket (0600 in 0700, per-root default name, stale unlink, live refusal), one attached client at a time (hello/resize/input/open-file/detach/ping; output only to the attached client; detached input blocks without stopping the editor; Close sends exit), `gvim -a/--attach` proxy with raw mode/SIGWINCH/notices, palette Detach under --daemon, session restore on start/save on exit (detach saves nothing), single-writer refusal for bare/session launches while the per-root daemon is live (file/--simple/--diff/stdin stay direct). Recorded reductions R12-1: foreground (no fork), auto_attach start-if-absent not implemented (documented refusal instead), one client, single workspace until #19, Windows named pipes later. Still open: per-buffer hot-exit store (R2-5, design doc), #19a workspaces. |
| Workspaces, windows, orchestrator, agents | orchestration dock, multiple workspaces per daemon, workspace switcher, New Workspace dialog, Run Agent (claude/codex/opencode/aider), agent session discovery/resume, live sessions, extract tab to workspace | **partial** (round 12: launch records + Run Agent picker; daemon one workspace; no discovery) | `internal/terminal/launch.go`, `internal/tui/{dock/tabs,agent_picker}.go`, `internal/app/terminal_agents.go`, `config/terminal.go` (agents/resume_agents), `internal/restore/terminal.go`, `internal/daemon/` (one workspace); Fresh `docs/features/session-persistence.md`, `internal/orchestrator-sessions.md` | Round 12 added terminal launch records (argv/agent/resume argv/ephemeral, bounds), the Run Agent... palette row + picker over `terminal.agents`, persisted launch fields with user-initiated restart precedence (resume→launch→shell) and no auto-resume on restore. Not yet: multiple workspaces (#19a), a workspace/dock switcher, tmux session discovery/rejoin (#20b), agent-session plugins/orchestrator, live sessions. |
| Remote authority | SSH editing (scp/URL forms), devcontainers (CLI attach/rebuild/ports/logs), Kubernetes workspaces, remote-aware LSP/terminal/processes, reconnect, remote indicator popup | **missing** | No `ssh`/`authority` implementation in the tree; `internal/tui/remote.go:10` popup says "Remote workspaces are not supported"; `docs/plugins/porting.md:46` "authority ... not provided"; Fresh `docs/features/ssh.md`, `docs/features/devcontainer.md` | Entire area absent. Requires an authority abstraction under fs/proc/terminal/LSP first. |
| Workspace trust | per-folder Restricted/Trusted/Blocked, executable-marker detection, one-time prompt, status pill, dialog command, persisted per folder, gates LSP/build/env activation, env detectors | **partial** (R1 landed) | `trust/` (levels + Undecided, store at `config.Dir()/trust.json`, atomic writes, marker scan, Fresh startup policy), `internal/app/trust.go` and `internal/app/run.go` (`Options.Untrusted`, `lsp_enabled` forced off), `internal/tui/trust*.go` (dialog, port, notices, pill), `config/trust_test.go`, `docs/trust.md`; Fresh `docs/features/workspace-trust.md` | Implemented in round 1: the three levels plus Undecided, per-folder persistence, executable-marker scan and prompt (silent trust for plain folders, Fresh's policy), the open-time prompt, the `Workspace Trust…` palette command and dialog, the trust status pill with click routing, the project config layer skipped unless Trusted, and LSP auto-start forced off unless Trusted (manual Start/Restart still works). Still open: no process gate — terminal/shell/formatter/LSP/plugin spawns are not consulted against the level, and Blocked enforces the same as Restricted (`docs/trust.md` §3); env detection/activation and devcontainer/tasks are absent; the config gate is a startup decision (a mid-session trust change applies project tooling at the next start, documented divergence). Tracked as backlog #17. |
| Markdown | syntax highlighting, markdown_source smart list editing, page view/compose, tables (wrapped cells), code-fence frames (continuation rails, container framing), dual view, heading conceal/scrollbar marks, page-view persistence, Contents section | **partial** (R1 Contents; R4 smart editing; R6 core; R7 fences/tables; R8 dual/borders; R9–11 residual slices) | `syntax/{markdown,markdown_inline,markdown_markers,markdown_fences,markdown_tables}.go`, `editor/markdown.go`, `internal/tui/editorpane/{compose,compose_layout,compose_fence,compose_table,compose_setext,pageview}.go` + `internal/tui/{pageview,dual}.go`, `internal/tui/sidebar/contents.go`, `session/pageview.go`, `docs/markdown.md`; Fresh `plugins/markdown_{toc,source,compose}.ts` | Round 9–10 landed wrapped cells, escaped pipes, container fallback, fence continuation rails, heading marks, page-view persistence, quote-container fences, setext conceal, task/image conceal, inline-HTML literal decision, mapped-decor no-paint, horizontal clamp, dual line-anchor/one-dual policy; round 11 verified composed plugin-overlay pipeline ordering and recorded the wrapped-cell selection decision (hook rows have no source line/row-source map) and tightened the dual/per-split wording. Remaining R10-2: free per-split view mode, dual split persistence, native Markdown menu row, exact byte dual anchors (approximation recorded), sidebar per-section widths/drag order. |
| Diagnostics surfaces | gutter marks, underline, inline end-of-line text, diagnostics panel, quickfix list, error navigation, hover fusion, pull/workspace diagnostics, progress | **partial** (rounds 4–10) | `internal/tui/{diag,diagpanel,lsp}.go`, `internal/tui/editorpane/{decor,inline_diag,inline_hints}.go`, `commands/lsp_commands.go`, `docs/lsp.md`; Fresh `docs/features/lsp.md` | Gutter dots, underline, inline end-of-line text with staleness, status counts, F8 jumps and hover diagnostics exist. Round 6 added the native Diagnostics panel (file→severity grouping, counts, jump/scroll/focus, progress/empty) and the Diagnostics List quickfix; round 7 workspace diagnostics for unopened files (`(not open)` rows) and `$/progress`; round 9 inline-hint tier 2; round 10 cancellation (pullSet/`$/cancelRequest`) and Linux process limits. Remaining: no built-in universal server (deliberate), per-OS process limits caveat, capability-gated workspace diagnostics (publish-only servers report their own set), plugin `DiagnosticInfo` carries no code (native ones do). |
| Scripting and CLI automation | `--cmd script`, `--cmd daemon open-file --wait`, stdin programs, `init.ts` startup script, agent channels, watch mode, CLI help for the API | **partial** (r11 stdin; r12 daemon flags + remote targets) | `internal/cli/{root,args}.go` (flags: config/keymap/theme/log-file/diff/no-plugins/plugin/stdin/stdin-name/daemon/attach), `internal/app/{stdin,daemon_runtime}.go`, `authority/spec.go` (scp/ssh:// parsing), `internal/tui.RunWithInput/RunWithIO`, `plugin_init.go`; Fresh `docs/features/scripting.md` | gvim has `init.lua`/the Lua API, stdin-as-a-buffer (r11), `gvim config show/paths` (show trust-gated r12), `gvim grammar`, daemon/attach flags (r12, foreground only, single writer/one workspace) and remote `user@host:path` / `ssh://` targets (remote launch opens the SSH terminal only; no remote files/LSP yet). Missing: Fresh's external `--cmd` script channel, `open-file --wait`, watch/agent capability, forked daemon/auto_attach. |
| Internationalization | 15 locales, locale detection/config, plugin i18n (`t()`, locale JSON, translated menus/commands/prompts) | **partial** (r11 foundation #22a; r12 catalogues/import/validation #22b) | `i18n/` (package, `catalogue.go`, `locales/*.json` embedded, `fresh_map.json`), `tools/{i18n_import.py,i18n_check.sh}`, `config` locale, Settings Interface row, `internal/pluginhost` locale + Lua `gvim.i18n.locale`, `docs/i18n.md` | Round 12: 15 embedded catalogues (English source of truth; the 14 translated files carry the 13 mapped-from-Fresh keys; 12 gvim-only keys stay English by design; total 12,646 bytes with a 64 KiB cap test), the deterministic Fresh import (`fresh_map.json` + `i18n_import.py --check`) and CI validation (`i18n_check.sh`: embedded files validate, English complete, mapped keys translated, committed files match Fresh when present); `ValidateCatalogue`/`CoverageFor` check key shape, plural categories, %{name} placeholders, duplicates, unknown keys and coverage; lazy loading, embedded German rendering + fallback tests; plugin `t()` sees only its own .i18n.json. English boundary: generated CommandDef/menu tables, hand-written command rows (terminal-tab/Run Agent), settings labels and other in-round strings until a generator/extraction learns keys. Remaining R12-5: that extraction + translated coverage beyond the mapped subset. |
| Update, packaging, telemetry | update check, in-editor self-update prompt, `--no-upgrade-check`, telemetry toggle, packaging matrix (deb/rpm/aur/nix/npm/brew/appimage/flatpak/winget), daemon packaging | **missing / deliberately not planned?** | `internal/tui/notice_na.go:12-13` (update/update log not applicable); `internal/cli/` has no update flags; Fresh `README.md` | Decide with the Lead: these are distribution features rather than editor features for a pure-Go terminal binary. If in scope, the in-editor update indicator belongs to the status bar (`{update}`). |
| Platform input and OS integration | CSI-u/modifier disambiguation, AltGr/CapsLock quirks, key release, GPM mouse on Linux console, suspend, stdin streaming, ConPTY, OSC 52 clipboard, Window title | **partial** (stdin-as-buffer and ConPTY/OSC52/reflow round 11) | `internal/terminal/` (x/vt, `pty{,_windows}.go`, `osc52.go`, `reflow.go`), Bubble Tea v2 input, `internal/tui/clip.go`, `internal/tui/root_terminal.go`, `internal/app/stdin.go`, `internal/tui.RunWithInput`, `docs/terminal.md` | Round 11: stdin-as-a-buffer (bounded snapshot, not streaming; keys from the controlling terminal); Windows ConPTY pure-Go (GOOS=windows build/vet/test-compile verified — parity unverified until the real-Windows checklist pass); child OSC 52 clipboard write (parser bounded/chunk-safe; `clipboard.use_osc52` default off AND trusted workspace only; selectors c/empty; restored transcripts never hooked; write only, no read/paste reply); stored-transcript text re-wrap (styled cells/wide glyphs hard rows; live screen still clipped). Residuals: kitty keyboard (x/vt TODO/no seam), live-screen reflow, OSC 52 read, GPM/AltGr/CapsLock/key-release, stdin streaming, plugin focus/mouse events beyond focus_gained/active_window. |

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
26. **Terminal persistence is per-tab read-only/dead state; stored rows re-wrap.** `terminal.json`
    keeps a bounded transcript (500 rows / 1 MiB, control-stripped) and — since round 12 — an
    additive `tabs[]`/`active_tab` with the top-level fields mirroring the active tab (old readers
    restore that one tab; old files restore as one terminal), at most 16 tabs, each with its own
    transcript, name, open flag, cwd, width and launch/resume/agent/ephemeral record. Restore
    brings every tab back **read-only and dead** with its saved name and active index; no PTY is
    restarted automatically and the launch record never starts anything (Fresh's no-auto-resume
    rule). A content-width change re-wraps the stored rows (styles/wide pairs preserved, hard
    rows stay, text re-wrap not semantic reflow; live screen still clipped). Fresh restarts
    terminal tabs live and appends the saved scrollback; gvim keeps the transcript read-only
    until the user restarts (R12-2). `docs/terminal.md`.
27. **Inlay hints use tier-2 inline placement, with a documented fallback set.** A server's hints
    are display-only faint cells spliced before the protocol position (UTF-16→rune column,
    parts/padding, wrap-aware, wide-safe truncation) through the additive `Pane.InlineHints`
    hook; caret/mouse/selection/search keep the line's own columns and there is no caret-line
    hiding. The end-of-line form remains only for page view/composed columns, tall/windowed
    lines, no-room rows, insertion inside concealed ranges, and panned rows (which hide hints
    until column 0). `docs/lsp.md`; R7-5.
28. **Pull diagnostics only fill a gap.** A server that publishes is never also pulled from, so
    one problem is reported once; pulled sets are cached per document version. Fresh's fusion
    rules are richer (hover fusion/per-server raw sets), which round 6's panel work may need.
    `docs/lsp.md`.
29. **The theme editor is a native list-driven overlay with a colour picker.** Fresh's editor is
    a plugin over the widget runtime; gvim's is a sheet over the resolved theme (swatch/value/
    attribute rows, hex/attributes prompts, live preview, Save/Save As/Delete, atomic write).
    Round 10 added the colour picker: `p` or a swatch click opens a swatch grid of the working
    theme's distinct colours in key order plus the named ANSI colours (eight chips/row), arrows/
    pages/click/drag move and live-preview on the working theme, Enter applies the edit (the
    editor becomes dirty and can save), Esc or a click outside restores and closes; mouse
    selection never commits on its own. Drag-and-drop of values and per-cell provenance remain
    (R10-4/R5-3). `docs/themes.md`.
30. **Inspect-at-cursor reports exact painted-key provenance for the first-surface subset, with a
    colour-match fallback.** Round 11 gives `grid.Style` compact `FGKey/BGKey` theme-key ids and
    `grid.Grid` an opt-in per-cell capture buffer that is allocated only by the inspector's
    captured repaint (no allocation/recording and identical cells otherwise); style builders
    tag the first-surface roles (editor text/syntax/gutter/selection/current line, status bar,
    menu band, popup/list/palette/Settings rows, explorer/sidebar/diagnostics chrome) and the
    inspector prints exact `Painted by (fg/bg): section.key` lines plus the pair line. Where a
    painter is untagged (compose/page-view/plugin/custom overlay styles), the popup says
    explicitly `Foreground/Background (matched by colour)`, listing the resolved keys whose
    rendered colour equals the cell, and the context facts pick the role. Fresh records the
    key on every painter; gvim's coverage is the first-surface subset by design (R10-4).
    `docs/themes.md`.
31. **Project-search "quickfix" is a transient list.** Alt+Q opens the result set in the shared
    list overlay (filterable, Enter jumps, list stays open); gvim has no persistent quickfix
    stack and the results panel remains the source of truth. `docs/ui.md`.
32. **The plugin action popup blocks the caller.** `gvim.ui.action_popup` waits for the choice
    and returns its id (and also emits `action_popup_result`); Fresh's `showActionPopup` returns a
    boolean and reports only through the hook. Both share the card/frame and the caps.

33. **The Markdown page view is a native inline transform, not Fresh's plugin.** The view mode
    is per document (Fresh: per split) with one derived per-pane override for the dual view, the
    compose rules are the shipped subset (conceals/reveal, rules, quote bars, hanging indents,
    code-fence frames with continuation rails, box-drawn tables with wrapped-cell continuation
    rows and outer borders, heading marker conceal + scrollbar marks, setext styling, task-list
    glyphs and image alt text), and the pane maps source↔display for caret/hit/selection/search/
    diagnostics/folds. Inline HTML is kept literal (no cell renderer); mapped occurrences/
    rulers/indent guides stay deliberately off in page view (no-paint tests); page-view mode and
    width persist in the session; a page wider than the pane is clamped (documented decision).
    Non-Markdown documents get the page geometry only, and large files stay full width.
    `docs/markdown.md`; R9-3/R10-2.
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
36. **Plugin buffer groups are group-lite.** A group is a layout of real splits of the current
    tab (no synthetic group tab): read-only panel buffers the host makes or existing buffers
    shown in their own windows, styled panel content, panel title/visibility/focus, ownership
    cleanup, and scroll-sync groups with anchor interpolation. Fixed-height leaves are
    supported (`{type="fixed",height=N}`, 1–100 rows, re-applied on layout change; approximate at
    split borders and in side-by-side parents; git_log's height-1 toolbar renders one row);
    group titles are stored for the port API but not painted; independent per-panel views of one
    buffer are deferred to the plugin-host remainder (R9-5). `docs/plugins/api-buffers.md`.
37. **The plugin API stays at level 2.** Round 6 added the interactive widget/instance-state
    surface, group-lite buffer groups + scroll-sync, theme-file APIs and
    `text_properties_at_cursor` without a level bump; a manifest may still require `"api": 2` and
    level-1 plugins keep working. `docs/plugins/README.md`.
38. **Search previews are bounded, scroll, colour and soft wrap.** The selected-match column is
    a window (24..60 cells, palette conventions, hidden below 64 inner cells, long lines
    truncate by default) whose window scrolls while the result rows have focus (PgUp/PgDn,
    Ctrl+Home/End, wheel over the column, three-line notch), whose lines are syntax-coloured
    when the port can highlight the file (bounded LRU cache 256 KiB/4000 lines), with a mapped
    header `Lfirst–last/total` indicator; a long multi-line match pages without changing the
    replace range. Round 11 adds soft wrap (`Alt+P` in the panel and editor, session default):
    wrapped rows remap the syntax spans and match highlight, cap at eight display rows per line
    plus an ellipsis, and the window is counted in display rows. `docs/ui.md`,
    `docs/keybindings.md`; R9-2.
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
43. **Bundled-plugin default policy is memory-bound.** 9 of the 22 bundled plugins are
    default-enabled; the 13 off are flash/vi_mode (size/heap), live_diff (opt-in like Fresh's),
    git_log/compare_files/merge_conflict (heap; merge_conflict flipped off in round 9 when the
    ten-plugin set hit 8.07–8.10 MiB), dashboard/code_tour/lsp_navigation/find_references
    (port phase), and the round-10 port wave git_grep/git_explorer/env_manager (heap, ~1.5 MiB
    each for the last two; see item 70). The default set costs about 7.25 MiB of the firm
    8 MiB gate (~9.3 MiB session baseline), enforced by `TestBundledPluginsIdleHeap`; the
    shared Finder library is lazy-required by diagnostics_panel so the idle set does not pay
    for it. `docs/plugins/bundled.md`.
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
46. **Settings coverage is complete for the page surface; plugin settings stay file-edited.**
    The Language Servers page edits each language's whole server array as validated JSON (array
    elements are not individually addressable), shows parsed servers read-only and accepts
    `process_limits` as shown-but-unapplied; Environment rows are read-only facts; per-language
    structured values show the inherited effective value and reset/inherit via whole-object
    writes; the Plugins page toggles `plugins.<name>.enabled` with file rollback/reload and —
    since round 9 — lists each plugin's declared settings with type, default, bounds/choices,
    effective value and supplying layer (read-only, from the runtime schema/values API); the
    Keybindings page is read-only and opens the Keybinding Editor (no in-page capture,
    deliberately). Residuals (R8-3): editing plugin settings still happens in the raw config
    (`plugins.<name>.settings`), no plugin gallery/install page, no in-page key capture,
    `env.detectors`/env activation, per-field LSP dialogs. `docs/settings.md`.
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
50. **The inserted-row mechanism has documented display limits.** Round 9 made virtual rows
    wrap like text (byte-safe chunks; gutter glyph and text overlays on the first chunk only;
    every chunk maps to the anchor line) and made the scrollbar thumb row-based when inserted
    rows are present (a large/windowed document keeps the line-based thumb), on top of the
    round-8 composition order (wrap+folds → markdown compose → conceal → soft breaks → virtual
    lines → cursor-line overlay → selection). Remaining limits: group fixed-height leaves are
    approximate (split border can leave one row off; a fixed leaf in a side-by-side split is
    ratio-exact only; a hidden fixed panel's height is re-applied when shown), the pane keeps no
    synthetic group tab or independent per-panel views, and there is no live `windowEmbed`
    pane/markdown-in-text/dock placement (R9-5). `docs/plugins/api-ui.md`, `api-buffers.md`.
51. **Bundled defaults are memory-bound (current counts in items 43/70).** The idle heap is
    gated by `TestBundledPluginsIdleHeap` and the shared Finder library is lazy-required by
    diagnostics_panel so the default set does not pay for it; the per-plugin attribution and
    the round-10 growth to ~7.25 MiB are documented in `docs/plugins/bundled.md` and item 70.
52. **Palette count 229 (unchanged in rounds 9–10).** Of the 229 rows 23 are non-generated: 7
    hand-written native rows, 4 plugin-management rows, 3 round-4 rows, 2 round-5 rows, 2
    round-6 diagnostics rows and the 5 round-8 Markdown mode rows; plugin commands appear only
    while the plugin loads (22 bundled plugins, 47 registration sites), and the opt-in
    compare_files port duplicates the native Compare rows without changing the count.
53. **Group layout ratios are fixed and fixed-height leaves are supported.** BufferGroupLayout.
    Ratio is the first child's share; SplitOptions.Ratio is the new (second) window's share —
    round 8 fixed the inversion with a regression test. Round 9 added fixed-height group leaves
    (`{type="fixed",id,height=N}`, 1–100 rows) which the host re-applies on every layout change
    by setting the split ratio above the leaf; the help is approximate at the split border and in
    side-by-side parents, and git_log's height-1 toolbar now renders as one row.
    `docs/plugins/api-buffers.md`.

54. **The dashboard is a native read-only start tab.** It replaces Fresh's dashboard plugin for
    the user-facing feature: a centred frame with repository state, recent files, keyboard
    quick actions and best-effort `df` rows, auto-opened on an empty editor/after the last file,
    timer-refreshed, with a buffer-local mode. Deliberately not ported: weather/GitHub/network
    sections, the third-party section API, Utility Dock placement, row mouse clicks and the
    sessions section. `docs/plugins/bundled.md`.
55. **code_tour is a floating-widget reduced port.** One tour at a time (Fresh/VS Code JSON
    auto-detected, 1 MiB/200-step bounds), prose as styled plain text, a list/counter/location
    plus Previous/Next/Jump/Exit buttons and a hint bar in a floating widget card; steps open the
    file at the line range through `gvim.buf.open` and highlight it with anchored decorations
    (the plugin keeps local line anchors; `gvim.buf.set_line_targets` now exists for a future
    version); last tour/step restore from plugin state, and a starter `.tour` writer.
    Deliberately not ported: Utility Dock placement, per-step buffer modes/dock keys,
    markdown-in-text prose. `docs/plugins/bundled.md`.
56. **The shared Finder is a three-backend port with documented omissions.**
    `plugin/bundled/lib/finder.lua` splits Fresh's 1862-line finder into: fuzzy ranking with hard
    caps; a prompt frontend over `gvim.ui.picker` (the library ranks in Lua so custom orderings
    such as symbols re-sorted by line survive; `on_close` runs once with
    selected/cancelled/programmatic); and a panel/tree backend plus a locations finder
    (relative `path:line` rows, snippets, preview tabs, Enter). Omitted: asynchronous
    SearchSources with cancellation handles/per-query process spawns (sources are Lua loaders;
    picker generations guard stale updates), the embedded preview pane/utility-dock routing,
    `syncWithEditor`, `navigateOnCursorMove`, `panelKeys` and panel-mode prompt events. The LSP
    edge normalizes `Location`/`Location[]`/`LocationLink[]` and UTF-16 columns. The library is
    lazy-required by diagnostics_panel so idle cost stays out of the default set.
    `docs/plugins/porting.md` ("The shared Finder").
57. **find_references is command-driven, not event-driven.** Fresh's plugin answers the host's
    `lsp_references`/`lsp_implementation` events; gvim has no such plugin events (its native Find
    References action opens its own UI), so the port exposes Fresh's own command keys: Show
    References Panel requests `textDocument/references` for the symbol under the cursor (UTF-16
    column, `includeDeclaration`, 100-row cap), shows a filterable location list with relative
    `path:line` rows/snippets and preview tabs; Enter jumps and the hidden Hide References Panel
    closes; implementation lists live in lsp_navigation. Fresh status messages are kept; the
    list does not reuse a source split (preview tabs do).
58. **Inline-hint tier 2 keeps a documented fallback set.** Hints render inline (protocol
    position, parts/padding, display-only faint cells before the anchor, wrap-aware, wide-safe
    truncation, no caret-line hiding; caret/mouse keep line columns) except for: page view/
    composed columns, tall lines of very long wrapped lines, lines shown as a window of a large
    file, rows with no room, and insertion points inside concealed ranges — those use the
    end-of-line form. Horizontally panned rows (`left > 0`) hide hints entirely (the documented
    hscroll case) and show them again at column 0; wrap-off long lines hit the no-room fallback
    painted/clipped at the row end. `docs/lsp.md`.
59. **Sidebar and page-view state are additive session fields.** `session.Sidebar`
    (`tui.SidebarState`: column width form/side, explorer fold/selected path/scroll, Contents
    open/rows/window, Diagnostics) and `FileState.pageView/pageWidth` are optional/omitted at zero,
    so older sessions load with defaults and the format version does not change; the sidebar
    state restores only when the saved layout is used (`editor.restore_previous_session` on, no
    `--no-restore`), and a Contents restore waits for the first scan. Plugin sections' order/
    sizes/folds and per-section widths are not saved (R9-4). `docs/sidebar.md`, `docs/markdown.md`.
60. **Search's large-file replace distinction is explicit.** An open buffer is searched from its
    snapshot whatever its size and replaced through the unsaved-tab/hash model (Save All writes
    it); a closed file above the search read cap (or above the large-file threshold in large-file
    mode) is never searched and shows a skip reason — the streamed writer for closed oversize
    files is the residual (R9-2). The preview protocol carries `Start`/`Total`/`Spans` with a
    bounded syntax highlighter (LRU 256 KiB/4000 lines) and `Options.SyntaxStyles`; long matches
    page without changing the replace range; a scrolled window paints from cache and re-asks at
    the cache edge. `docs/ui.md`.
61. **merge_conflict ships off for the firm memory gate.** Round-9 host growth (line targets,
    settings API, ports) pushed the ten-plugin default set to 8.07–8.10 MiB; merge_conflict
    (1.35 MiB) was flipped `default_enabled: false` so the nine-plugin set costs ~7.0 MiB of 8
    (~1 MiB headroom). The loader's off-by-default list and the per-plugin attribution in
    `docs/plugins/bundled.md` record it; users can re-enable it in config.

62. **git_grep is a reduced prompt adaptation.** Fresh shows a prompt toolbar (Case toggle widget
    driven by widget events); gvim's prompt is the picker and has no toolbar/in-prompt key
    surface, so the port shows the case state in the footer/hint and flips it with Fresh's
    second palette command (refreshing an open prompt, otherwise applying to the next search),
    opens no Finder preview band (no overlay preview in gvim) and opens the hit on Enter; the
    git call goes through the hardened `gvim.git` gateway (15 s timeout, 2 MiB cap, 100-row/
    per-line caps, exit 0/1 success) and a superseded query is discarded by the picker
    generation guard instead of cancelling the process (the Finder search source has no cancel
    handle). `docs/plugins/bundled.md`; R10-1.
63. **Explorer plugin decorations are namespaced, capped and painted by the panel.** The host
    surface mirrors Fresh's (set/clear decorations and slots, per-(plugin,namespace) isolation,
    10k entries/namespace, 64 namespaces/plugin, symbol 8 bytes/slot text 32 bytes/leading slot
    8 cells, absolute-or-root-relative paths with outside-root ignored, priority→namespace→call
    resolution, unload/reload cleanup). The explorer panel's `RowDecor` painter draws the leading
    slot/override, the trailing slot (plugin override, then decoration symbol, then the native
    git mark), suppress flags and the filename colour; the trailing slot sits at `end-tw` and the
    name ends immediately before it (the Lead's post-integration fix — no forced gap/padding
    cell). The git_explorer port only decorates the workspace-root repository (no nested
    sub-repo discovery), has no hover tooltips and symbols only (no arbitrary icons), refreshes
    on ready/open/save/revert/focus/config plus a debounced `.git` watcher, and is opt-in
    (~1.5 MiB idle). `docs/plugins/api-ui.md`, `docs/plugins/bundled.md`; R10-1.
64. **Project environments are host-managed and trust-gated.** Detection lives in the host
    (`env.detectors` user config only — a project config cannot set it; exactly Fresh's built-ins
    .venv/venv path-only requiring an interpreter, direnv/mise/pipenv/poetry shell; first match
    wins) and is passive at workspace open (stats markers only); it shares the marker list with
    the trust gate. Activation is gated on Trusted: a host-managed `sh` capture of the set/unset
    delta (bounded) becomes an overlay applied to every spawn the editor owns (shell commands/
    formatters, LSP servers, plugin processes, terminal shells, app git-grep). Plugins get
    `gvim.env.detected/active/trust_level/set/clear` plus `env_changed`/`trust_changed`; the
    env_manager port has Fresh's four i18n command keys, the status pill and path-only
    auto-activation once trusted, and asks the core trust dialog for shell environments.
    Reductions: one local environment, set-only overlay (no removal of previously captured
    vars), explicit shell activation, no widget panel/watcher/devcontainer/authority, no
    persisted recipe; the port is opt-in. `docs/trust.md`, `docs/plugins/api-core.md`; R10-1.
65. **process_limits is Linux-only and recorded per OS.** On Linux gvim applies Fresh's memory/
    CPU caps from the parent through a user-delegated cgroup v2 (`memory.max` as a percent of
    MemTotal, `cpu.max` quota with 100 ms period, `gvim-lsp-<pid>` under the same base,
    `GVIM_CGROUP_BASE` override, child moved in via `cgroup.procs` right after start); without a
    delegated cgroup memory falls back to an `sh -c 'ulimit -v KB; exec "$@"'` wrapper and a CPU
    percentage has no rlimit equivalent (an rlimit CPU limit is seconds), so it is recorded as
    not applied. macOS/Windows have no cgroup interface and Go's `os/exec` has no pre-exec hook,
    so the limits are explicitly recorded as unreachable in the Show-LSP-Status popup and the
    per-OS docs table (no silent no-op). `docs/lsp.md`; R7-5.
66. **No built-in universal language server ships; this is deliberate.** Fresh's quicklsp is a
    separate, user-installed Rust binary (default `universal_lsp` names `command: "quicklsp"` with
    `enabled: false`; no quicklsp crate in the editor sources), not editor wiring; porting it
    would be a new server project. gvim fully supports user-configured `universal_lsp` servers
    (config merge, manager defaults, status listing, tests). The residual stays open
    deliberately as documentation, not a missing-integration gap. `docs/lsp.md`; R7-5.
67. **Terminal slice A is a fixed-step adaptation; round 12 added tabs, agents and the remote
    terminal.** Drag auto-scroll is one fixed ~80 ms sequence-guarded tick (no acceleration),
    forwarding the mouse while dragging, extending to the edge row, stopping at ends/back-
    inside/release/selection-clear/focus-loss; word/line drag extension after double/triple
    click; Ctrl+hover underlines/resolves on click (absolute/~, OSC 7 dir, workspace root;
    :line[:col]); `pass_ctrl_v_without_text` matrix with LastYank. Round 11 completed R10-3
    (ConPTY compile-verified/unverified, OSC 52 write opt-in/Trusted, transcript reflow).
    Round 12 adds up to 16 terminal tabs (strip/active/exited/click-select, Ctrl+PgUp/PgDn,
    palette rows, per-tab close_on_exit, per-tab persistence as above), launch records with
    agent metadata/resume and the Run Agent picker, and the SSH terminal through the authority
    slot. Residuals: kitty keyboard, OSC 52 read, live-screen reflow, live PTY restore,
    workspace-scoped tabs (#19), drag/reorder/per-tab splits, agent re-exec edges, report-split
    close_on_exit. `docs/terminal.md`; R11-3/R12-2.
68. **Theme accents, URL refs and provenance are recorded decisions.** The six bundled themes no
    longer hand-pick accents: `ui.accent_bg/fg` fall back to `ui.popup_selection_bg/fg` and the
    audit pins that equality; a user theme may set accents explicitly. URL/packaged theme refs
    stay recognised and refused with a clear error — no network code/fetch until
    packaging/network policy. The Settings theme row shows four cached chips; the colour
    picker's mouse selection only previews, Enter is the only commit, Esc/outside restores.
    Round 11 adds first-surface painted-key provenance (see item 30): capture is opt-in and
    allocation-free when off, exact where tagged, colour-match fallback elsewhere; the picker
    and drag were not extended to provenance. Remaining (R10-4): drag-and-drop of theme values,
    background images, user-theme inheritance (Fresh's own limit), and the untagged-painter
    fallback. `docs/themes.md`.
69. **Markdown round-10/11 decisions are recorded.** Inline HTML stays literal (no cell
    renderer; hiding it would lose text); an image's leading `!` opens its marker so the alt
    text composes; task-list boxes become ☐/☑ glyphs (raw under the caret); setext headings
    keep tokenizer heading styling on the text line and conceal the underline, with
    thematic-break distinction from tokenizer spans; a fence inside a blockquote is framed at
    the container with a per-line literal fallback, list-indented fences keep their indent and
    nested fence markers stay literal; mapped occurrences/rulers/guides stay deliberately off
    in page view (no-paint tests) and composed plugin overlays paint after the compose (round
    11 regression test pins pipeline order); a page wider than the pane clamps; the dual view
    uses a line anchor (exact byte/row anchoring a recorded approximation) and one dual per
    document; wrapped-cell selection paints the primary row only (hook rows have no source
    line/row-source map — recorded decision). `docs/markdown.md`; R10-2.
70. **The bundled set is now 22 plugins, memory-bound.** 9 default-enabled; 13 off. Round-10
    host growth moved the default set from 6.97 to 7.22–7.27 MiB of the 8 MiB gate, so the three
    new ports all ship off (git_explorer/env_manager would each add ~1.5 MiB; git_grep likewise
    does not fit). The off list is flash, vi_mode, live_diff, git_log, compare_files,
    merge_conflict, dashboard, code_tour, lsp_navigation, find_references, git_grep,
    git_explorer, env_manager. The gate and per-plugin attribution are documented in
    `docs/plugins/bundled.md`.

71. **Daemon #18a landed (MVP) with documented reductions.** Round 12 completed the daemon
    end-to-end: `gvim --daemon [NAME]` hosts the ordinary runtime in the foreground on a Unix
    socket (0600 in 0700, per-root default name, stale unlink, live refusal) and serves one
    attached client at a time (hello/resize/input/open-file/detach/ping; output only to the
    attached client; detached input blocks without stopping the editor; Close sends exit;
    `OnProgram` repaint/resize), while `gvim -a/--attach` is a raw-mode proxy with SIGWINCH and
    notices. The hosted editor restores the session on start and saves it on exit (detach saves
    nothing); the palette gains Detach under --daemon; a live per-root daemon makes a bare/
    session launch refuse with an attach hint (file/--simple/--diff/stdin launches stay direct);
    unencrypted open-file frames are forwarded. Recorded MVP reductions: no fork (foreground
    daemon), no `auto_attach` start-if-absent (refusal policy instead), one attached client, a
    single workspace until #19, Windows named pipes later. `docs/architecture.md` §8; R12-1.
72. **i18n is a foundation, not a translation.** Round 11 landed the package/resolution/
    catalogues/plurals/Settings row/live locale switching and a first extraction slice; only
    English is embedded, so the UI reads English today. Generated command/palette/menu titles
    stay English by design until the #22b command-key mapping; plugin translations retain the
    existing .i18n.json workflow but now follow the live locale. Remaining: #22b catalogue import
    from Fresh where keys match, the full extraction inventory, the 15 locales and the
    translation/CI workflow. `docs/i18n.md`; R11-2.
73. **Windows ConPTY is implemented but parity-unverified.** The pure-Go x/sys/windows path is
    build- and vet-verified with GOOS=windows (plus a Windows test-compile) but cannot be run
    here; docs/terminal.md carries the real-Windows acceptance checklist (cmd/pwsh paint, pipe/
    redirection, resize, exit/restart transcript, scrollback selection/links, Ctrl+V/Ctrl+C,
    restored transcript reflow) and says the Windows row reads unverified until that pass is
    recorded. `docs/terminal.md`; R11-3.
74. **Child OSC 52 clipboard writes are opt-in, trust-gated and write-only.** `clipboard.use_osc52`
    defaults false (the child's output is untrusted data); when enabled, a write happens only in
    a Trusted workspace and only for the clipboard selection (selector c or empty; primary/
    select/cut-buffer ignored), the parser is bounded and chunk-safe, restored transcripts never
    hook the handler, and the bytes still go to x/vt unchanged. A child asking to READ the
    clipboard (OSC 52 paste request) gets no reply — gvim's paste path is the editor's. This is
    a recorded deviation from Fresh's terminal clipboard surface. `docs/terminal.md`; R11-3.
75. **Transcript reflow is a text re-wrap, not a semantic reflow.** On a content-width change
    gvim re-wraps the stored transcript rows (cell styles and wide glyph pairs preserved, hard
    rows kept hard, trailing blanks trimmed) and marks a restored mismatched transcript
    `Scrollback (restored, rewrapped)`; a previously soft-wrapped continuation is not rejoined
    when the pane widens, and the live x/vt screen is still clipped/padded. `docs/terminal.md`;
    R11-3.
76. **stdin-as-a-buffer is a bounded snapshot, not a stream.** `gvim -`/`--stdin` (and piped
    stdin with no files named) reads up to 32 MiB before the TUI into an untitled `[stdin]` tab,
    then reads keys from the controlling terminal; CRLF is normalized with the ending
    remembered, the buffer is unmodified on open (Save runs Save As), NUL bytes are kept with a
    warning, and the launch never restores or saves the session/hot-exit. `--stdin-name` is
    gvim-only; there is no streaming spool/lazy chunking and no daemon/attach stdin.
    `README.md`, `internal/cli/`; #25 decision.
77. **Theme provenance covers the first-surface subset only.** Tagging happens at style
    construction for editor text/syntax/gutter/selection/current-line, the status bar, menu band,
    popup/list/palette/Settings rows and cheap explorer/sidebar/diagnostics chrome;
    compose/page-view/plugin/custom-overlay painters stay untagged and the inspector falls back
    to "matched by colour" with context facts. The capture buffer is allocated only for the
    inspector's repaint, so normal frames record/render identically. `docs/themes.md`; R10-4.
78. **Line targets cannot express three port needs, recorded.** Dashboard recent-file rows are
    line targets now (single left click; cleared on close/unload; command rows stay
    keyboard-only). code_tour is a widget-panel card (not buffer lines; its highlight is
    decoration anchoring) and git_log's Enter opens a historical blob (`git show <sha>:<file>`
    in a virtual buffer), so a target would wrongly open the working-tree file — neither
    migrated. The new porting section "Line targets: what they can and cannot express" records
    it. `docs/plugins/porting.md`.
79. **Update check, self-update and telemetry are out of scope (final, Lead-signed).**
    gvim implements no update check, self-update or telemetry; no network code is added for
    upgrades or anonymous usage; the status bar's `{update}` element is not implemented;
    `--no-upgrade-check`, if given, is a documented no-op for Fresh compatibility. This is a
    deliberate scope decision: Fresh's install/update infrastructure is not part of gvim's
    parity surface (§5.4 #24, §8).
80. **Platform edges are partial (final, Lead-signed).** stdin-as-a-buffer is implemented
    (item 76); GPM mouse on the Linux console, AltGr/CapsLock/key-release edge cases and plugin
    focus events beyond the landed focus_gained/active_window stay deferred/out of scope, and no
    further cross-platform input matrix is promised beyond the shipped terminals (§5.4 #25,
    §8).
81. **Fresh's GUI/desktop, browser --web UI, WASM build and packaging matrices are out of scope
    (final, Lead-signed).** gvim ships as one pure-Go terminal binary; daemon mode itself (#18)
    is in scope, daemon packaging is not (§5.4 #26, §8).

82. **The daemon MVP is foreground/single-client/single-workspace by design.** The first slice
    keeps the server/proxy simple: `--daemon` runs in the foreground (no fork/daemonize), there
    is no `auto_attach` config and no start-if-absent — instead a live per-root daemon makes a
    bare or session launch refuse with an attach hint, while file, `--simple`, `--diff` and
    stdin launches intentionally stay direct (they do not share the session); one client is
    attached at a time (a second attach is refused, never silently taken over); the daemon hosts
    one workspace (the launch directory) until #19; Windows named pipes are later; the session
    is restored on daemon start and saved only on daemon exit. These are recorded reductions,
    not missing wiring. `docs/architecture.md` §8; R12-1.
83. **Terminal tabs are bounded and restore dead.** The dock runs at most 16 single-session
    tabs; the strip shows active/exited/click-select, Ctrl+PgUp/PgDn quick-switch (not keymap
    data), five hand-written palette rows (New/Close/Next/Previous/Rename), and
    `close_on_exit` is per tab. `terminal.json` gains an additive `tabs[]`/`active_tab` with the
    top-level fields mirroring the active tab, per-tab bounds and old-file migration; restored
    tabs are read-only and dead (no PTY restart, no auto-resume) and Restart per tab uses the
    saved launch record. No drag/reorder and no per-tab splits; workspace-scoped tab sets are
    deferred to #19 (a workspace switch would keep them today). `docs/terminal.md`; R12-2.
84. **Agent sessions are terminal launch records plus a config list, not a plugin registry.**
    A `terminal.Launch` carries the launch argv, the configured agent name, the resume argv and
    the ephemeral flag, bounded (64 argv entries × 1024 bytes, agent name 64) and persisted with
    the transcript; a restore never auto-runs it. Restart precedence is user-initiated: the
    recorded resume argv when `terminal.resume_agents` (Fresh key, default true) is on, else the
    launch argv, else the configured shell. Agents come from gvim's `terminal.agents` config
    extension (user-layer only, bounded; Fresh's agent data is plugin-owned), the Run Agent...
    palette row/picker is hand-written English, and launches are local executables (no remote
    helper or workspace discovery yet). `docs/terminal.md`; R12-3.
85. **Remote authority is a first slice: SSH terminal and one-shot exec only.** The `authority`
    slot routes four surfaces (FileSystem/OneShot/LongRunning/Terminal) with local forwarding
    byte-identical to the plugin OS port, a trust-gate + env choke point, and State/Label/
    Connected/Reconnect/Close; the SSH backend (cached pure-Go x/crypto/ssh) supports agent/
    identity/passphrase/password, known_hosts, POSIX-quoted one-shot exec, long-running stdio
    and a PTY terminal with resize/reconnect. Remote launches parse scp/ssh:// targets (ssh://
    exclusive; malformed is a startup error; a remote path is never stat'ed; no local fallback)
    and are mutually exclusive with local files, Root, --diff, stdin, --restore and --daemon;
    they open no local files/session and open the SSH terminal at `Workspace()` with a notice;
    the status row shows `SSH:user@host` or `· disconnected` (hand-written English) and
    activation reconnects, prompting for a password when needed. Not routed yet (R12-4): the
    editor's remote file I/O (SFTP vs remote-helper plan in docs/authority.md §7), LSP through
    the long-running surface, plugin gvim.fs/gvim.proc, a per-remote trust store, terminal
    respawn after a dropped carrier, devcontainer/k8s constructors. `docs/authority.md`.
86. **i18n ships English-keyed catalogues with an explicit English boundary.** The embedded
    `i18n/locales/*.json` (15 files, 12,646 bytes, 64 KiB cap test) have English as the source
    of truth; the 14 translated files carry only the 13 keys imported from Fresh through
    `fresh_map.json`/`i18n_import.py --check`, while 12 gvim-only keys deliberately stay English
    (a missing translation falls back en→key). `i18n_check.sh` and `ValidateCatalogue/
    CoverageFor` enforce key shape, plural categories, %{name} placeholders, duplicates,
    unknown keys and coverage; the package loads English once and translations lazily. The
    English boundary is explicit: generated CommandDef/menu tables, hand-written command rows
    (terminal-tab/Run Agent), settings labels and other in-round strings stay English until a
    generator/extraction learns keys; plugins' `t()` sees only their own .i18n.json.
    `docs/i18n.md`; R12-5.
87. **The LSP residual sweep is decision-complete.** R1-3 closed: `gvim config show` reads the
    recorded trust level read-only (no open-time auto-trust write) and skips the project layer
    for untrusted/undecided workspaces with a named-file warning; trusted merges as the editor
    does; `config paths` stays ungated. R4-4: virtual-caret session persistence landed
    (`virtualLines/virtualColumn`, migration-safe) with docs/internal/virtual-space-scoping.md
    recording block rune-vs-byte geometry, wrap/multi-caret/large-file off, bool-only per-buffer
    toggle, caret-row-only virtual lines, viewport/fold clamps. R2-5 stays deferred with
    docs/internal/hot-exit-store.md (side store + stash design and the one-session-file
    dependency). R8-3's decision list is exact in docs/settings.md §6; R11-3/R11-4 decisions
    are in docs/terminal.md/docs/themes.md; the stale R1-2 open entry was removed (R6-6 already
    recorded it closed). `docs/trust.md` §3.

## 5. Ranked backlog for the remaining rounds

Sizes are estimates: S ≈ a day or less, M ≈ 1–3 days, L ≈ 3–10 days, XL ≈ more (unvalidated).
The order follows the goal's dependency shape: user-visible TUI/editing gaps first, then the
plugin-API breadth that unblocks Fresh's plugin-era features, then the daemon/remote/agent
architecture. "Owner area" names the package that should own the work.

Rounds 1–9 closed the workspace-trust surface, native sidebar sections/Contents and the LSP pill
+ diagnostics badge (round 1); virtual space (`on`), inline diagnostics, Fresh's keymap data, the
config-driven status bar + menu accelerators, and the session settings/CLI (round 2); the editable
Settings page, terminal slice A, the explorer options + sticky parents, and the palette preview +
Live Grep toggles (round 3); LSP 4a, project-wide Search & Replace + Git Grep, settings coverage,
virtual space R2-1 and markdown smart editing (round 4); LSP 4b data rendering A–D, plugin host
tier 1a, theme model/editor, terminal slice B, project-search R4-2 and status bars R2-3 (round 5);
LSP 4b E, plugin tier 1b, the diagnostics_panel port, the markdown compose core, Contents
fold_buffer (R1-1) and search preview/multi-line (round 6); LSP R6-5, plugin tier 1c-A's
store/Lua half, git_log/live_diff ports, markdown fences/tables, overlay keymap contexts (R2-4)
and a settings R4-3 slice (round 7); plugin tier 1c-B/C, the #15 compare API + compare_files
port, the native markdown dual view + table borders, settings R7-3, the path_complete/lsp_help
ports, live_diff/git_log full paths and streaming search/Git Grep globs (round 8); and the Finder
library + lsp_navigation/find_references/dashboard/code_tour ports, the Markdown residual slice,
search R8-5, LSP inline-hint tier 2, Contents R6-6/sidebar state, and the tier-1c remainder +
declared-settings API (round 9). Retired ids: #7, #8, #11 and #15 are closed; §5.1 holds #12 only;
R1-1, R2-4, R5-1, R6-5, R6-6, R7-1/R7-2/R7-3 and R8-5 are closed, and the slice closures of
earlier rounds are in. The ranked items below are the remaining gaps, with the gaps the rounds
opened listed first as R1-*, R2-*, R3-*, R4-*, R5-*, R6-*, R7-*, R8-* and R9-*.

Round 1 recorded three remaining gaps (R1-1 closed in round 6; the spawn gate is also ranked as #17):

- **R1-1 (CLOSED round 6)** — Contents `fold_buffer`: the native setting (user layer, restart;
  plugin-key fallback) publishes per-heading ranges through the version-guarded outline fold
  source; folding a Contents row folds its section (no fold command, Fresh parity). See the
  File explorer/sidebar row and R6-6 for the small residual differences.
- **R1-2 (CLOSED round 9, entry corrected round 12)** — sidebar/session persistence landed with
  R6-6: `session.Session.Sidebar` + `session/sidebar.go`, `internal/restore/sidebar.go`,
  `internal/tui/sidebar_state.go` + `plugin_sidebar_state.go`/`markdown_toc` (plugin-section
  order/rows/folds since round 11) and `docs/sidebar.md`; the stale open ranking entry was removed.
- **R1-3 (CLOSED round 12)** — `gvim config show` trust-gates the project layer: `configview.Load`
  reads the recorded decision read-only (it never runs the open-time policy that would auto-
  trust/write) through `app.ProjectConfigTrusted` and skips the project layer for
  untrusted/undecided workspaces, printing "project config layer skipped: <file> (the workspace
  is not trusted; start gvim in it to decide trust)"; trusted merges as before. `config paths`
  still lists both locations ungated (documented). Tests: configview trust gate + CLI real-
  command tests; docs/trust.md §3 "Config viewer".
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
- **R2-5 (P2, M, deferred decision documented round 12)** — per-buffer hot-exit store and
  file-launch unsaved-text restore: docs/internal/hot-exit-store.md records the precise design
  (side store `<config.Dir()>/buffers/<sha256(canonical path)>.json` + stash side files through
  atomicfile; versioned entry path/size/mtime/savedAt/sessionID/stash reusing
  session.StashContent/StashOverlay; write on session save for every unsaved buffer regardless of
  launch args, delete on clean; read at file open when hot exit is on and the restore put no
  text; caps/LRU/retention GC; single-writer via the session lock/daemon attach;
  trust/ephemeral gating; unknown versions ignored). Deferred because the one-session-file model
  is load-bearing for restore order/quit prompt/hot-exit policy/daemon ownership — a partial
  store would create two writers of unsaved text, so it must be scheduled with
  launch/session-lock/daemon/--no-restore.
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
- **R4-4 (P2, S, virtual-caret persistence landed round 12)** — virtual-space limits: the caret's
  vertical position now persists (`session.FileState.virtualLines/virtualColumn`,
  omitzero/migration-safe, restored after the cursor), and docs/internal/virtual-space-scoping.md
  records the design/limits: block geometry is rune-based vs the linear byte-clamped rule (§4
  item 19), virtual space is off under wrap/multi-caret/large-file mode, the per-buffer toggle
  is bool-only (block stays a global editor mode; no tri-state override), vertical virtual lines
  are caret rows only and cannot be selected, the viewport/fold end clamps and the view never
  scrolls past text. Residual = per-buffer block override, virtual-line selection,
  wrap/multi-caret support, the fold-clamp nuance. Tests: session round-trip + old-JSON restore
  + restore/session_virtual_test.
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
- **R5-2 (P1, L/XL, plugin tiers 1a–1c complete except R9-5)** — rounds 5–9 landed tiers
  1a/1b/1c and the round-9 edge APIs (painted virtual-text surface incl. wrapped virtual rows/
  row-based scrollbar thumb, group-lite groups/fixed-height leaves/scroll sync, interactive
  widgets, theme-file APIs, split labels/window state/events/line targets, compare API, declared-
  settings schema/values). Remaining plugin-host gaps are R9-5 (composite API native
  substitution, dock/fullscreen placement, independent per-panel views, windowEmbed live panes,
  markdown-in-text, plugin explorer rows/decorations/slots with the git_explorer wave, package
  manager/HTTP/project-local plugins).
- **R5-3 (P2, M, slices A+B landed rounds 10–11; remaining R10-4/R11-4)** — theming is now:
  registry/forms/extends/fallbacks/named colours/inspect/native editor/plugin overrides (rounds
  5–6), accent audit + Settings four-chip preview + colour picker/mouse editing (round 10), and
  first-surface painted-key provenance through an opt-in capture repaint (round 11). URL/
  packaged themes stay a recorded refusal (no network/fetch until packaging policy). Remaining:
  drag-and-drop of theme values, background images, user-theme inheritance (Fresh's own limit),
  and untagged painters (compose/page-view/plugin) report the colour-match fallback.
  `docs/themes.md`.
- **R5-4 (P2, M/L, slices A+B landed rounds 10–11; remaining R11-3)** — round 10 landed drag
  auto-scroll, word/line-wise drag extension, Ctrl+hover file:line:col links and the
  pass_ctrl_v matrix; round 11 completed R10-3: Windows ConPTY (pure-Go, compile-verified,
  real-Windows pass pending), the child OSC 52 write (opt-in/trust-gated/write-only) and
  transcript-only reflow. Remaining is R11-3: kitty keyboard (x/vt legacy-only/TODO), OSC 52
  read/paste, live-screen reflow, terminal tabs, automatic agent re-exec and report-split
  close_on_exit. `docs/terminal.md`.
- **R5-5 (CLOSED round 9)** — preview column and multi-line rows (round 6), streaming/batched
  results and Git Grep pathspecs (round 8), then the round-9 preview protocol with scrolling/
  syntax colours, window indicator, long-match paging, the large-file replace distinction and
  the third `Alt+A` Open Buffers scope. What remains (closed oversize writer, other provider
  sources, preview wrap/toggle) is R9-2, not this item.
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
- **R6-6 (CLOSED round 9, residual R9-4)** — R1-2 sidebar/session persistence landed
  (`tui.SidebarState` + `session.Sidebar` + restore conversions; saved-layout only, bounded,
  migration-safe; Contents restore waits for the first scan), the fold source publishes and
  rescans heading ranges while the Contents section is hidden (Fresh tracking), and
  `contents.fold_buffer` applies live through the outline fold source (`ApplyLive`) instead of
  restart. Residuals moved to R9-4; round 10 closed the plugin-section persistence (order/
  rows/folds), so only per-section widths and the drag-and-drop order remain (plugin explorer
  rows wait on R9-5).
- **R7-4 (P2, S, unbind landed round 11)** — explicit unbind tombstones are recorded by the
  keymap (context + global) and exposed through `keymap.Map.Unbound` + the TUI `UnbindKeymap`
  port/adapter; the overlay router swallows an explicitly unbound chord instead of running the
  surface's built-in fallback (tests pin popup/prompt/no-unbind cases). Remaining: multi-key
  bindings are not consulted in overlay contexts (one-press router; pending-chord state is
  root-only), and the completion/compositeBuffer/text-field contexts stay outside the router
  (Fresh implements them per surface). `docs/keybindings.md`.
- **R7-5 (P3, S/M; only two items remain)** — inline-hint tier 2 and workspace-diagnostic
  cancellation landed (rounds 9–10): hints render inline with the documented end-of-line
  fallbacks, and workspace/diagnostic pulls go through `protocol.Call` so supersede/Stop/
  Restart/SetLanguageEnabled send `$/cancelRequest` with the correct per-client id (pullSet;
  cancelled pulls never cached/merged, last good answer kept, 10 s timeout fallback).
  `process_limits` is enforced on Linux (cgroup v2 memory/cpu, `gvim-lsp-<pid>`,
  `GVIM_CGROUP_BASE`, `sh ulimit -v` memory fallback; CPU percent without a cgroup recorded
  not applied) and explicitly unreachable on macOS/Windows (status popup + per-OS docs table).
  Remaining: (i) no built-in universal server — deliberate recorded decision (Fresh's
  quicklsp is a separate user-installed Rust binary; user-configured universal_lsp fully
  supported), and (ii) the per-OS limits caveat. `docs/lsp.md`.
- **R8-1 (P2, L, remainder closed round 9; residue R9-5)** — round 9 closed the rest of the
  tier-1c list: row-based scrollbar thumb with inserted rows (windowed/large documents keep the
  line-based thumb), wrapped virtual rows (byte-safe chunks, glyph/overlays first chunk only,
  anchor mapping), `setLineTargets` (10k/buffer, Enter/click, `into` split, owner cleanup),
  fixed-height group leaves, and the plugin declared-settings schema/values API (read-only
  Settings rows). What remains is R9-5: the composite API recorded as a native substitution,
  dock/fullscreen placement, independent per-panel views, windowEmbed live panes,
  markdown-in-text, plugin explorer rows/decorations/slots.
- **R8-2 (P2, M, residual slice landed round 9; remaining R9-3)** — round 9 closed escaped
  pipes, container fallback, multi-visual-row table cells with continuation rows/rails and
  caret reveal, fence continuation rails, heading-marker conceal + heading scrollbar marks, and
  page-view mode/width session persistence. Remaining (R9-3): nested fences, task-list/image/
  inline-HTML conceal, setext heading polish, mapped occurrences/rulers/indent guides decision,
  composed-overlay ordering test, dual exact row/byte anchors + native menu row + split
  persistence + multiple duals, free per-split mode, horizontal page scroll, wrapped-cell
  selection limitation.
- **R8-3 (P2, M, docs decision-complete round 12)** — docs/settings.md §6 is exact (stale
  env.detectors/plugin-settings bullets removed): declared plugin settings are listed read-only
  with schema/default/bounds/layer, editing stays in the raw user config because a structured
  per-plugin editor needs a plugin-owned schema→widget mapping plus reload/rollback the settings
  service does not own; no plugin gallery/install page (plugins come from the binary, user dir
  and --plugin; a gallery is a packaging-policy decision like URL themes); LSP fields are edited
  as the whole language array JSON because config.PatchBytes cannot address one array element; no
  in-page key capture or second keybindings writer; no env activation; no per-field
  reset-vs-inherit + full undo; Reload is a palette row only; Fresh's generic widget runtime is
  not ported. These are decision-level residuals, not incomplete paths.
- **R8-4 (P2, S)** — port reductions: compare_files (no synthetic group tab, no EOL-only hunks,
  fillers need virtual-line paint, positional pairing, ui.input paths, no `--diff` hook/export,
  opt-in duplicate palette rows), path_complete (no `prompt_changed` seam; exported engine + one
  command), lsp_help (payload lacks language/type/command, 16 hints, no workspace-symbol action),
  live_diff (plugin-side baselines, Myers vs patience caps, no collision-fg, large-file refused).
  `docs/plugins/porting.md`.
- **R8-5 (CLOSED round 9, residual R9-2)** — the search-panel residuals landed: scrolling and
  syntax-coloured preview (protocol Start/Total/Spans + bounded LRU highlighter +
  `Options.SyntaxStyles`), a long multi-line match paged without changing the replace range,
  the open-buffer-vs-closed-cap large-file replace/refuse distinction, and the third `Alt+A`
  Open Buffers scope. What remains is R9-2 (closed oversize streamed writer, provider sources
  beyond files/open/current, preview wrap/toggle).
Round 9 recorded five gaps:

- **R9-1 (P2, S, round-10 ports add R10-1)** — round-9 port-wave reductions: dashboard (no
  network/weather/GitHub sections, no third-party section API/Utility Dock placement, no row
  mouse, no sessions section), code_tour (no Utility Dock placement, no per-step buffer modes,
  one tour, local anchors rather than line targets yet, plain-text prose), lsp_navigation (no
  initial selected index; list commands are gvim additions), find_references (command-driven,
  no reference/implementation events, no source-split reuse), Finder (no async SearchSources/
  cancellation, embedded preview/utility-dock routing, syncWithEditor/navigateOnCursorMove/
  panelKeys/panel-mode events). Optional follow-ups: move code_tour/dashboard/git_log onto
  `set_line_targets` where clickable rows help; the rest waits on R9-5 (dock/per-panel) or
  the daemon-era rounds. `docs/plugins/porting.md`.
- **R9-2 (P2, S/M, preview wrap/toggle closed round 11)** — search residual: a streamed writer
  for replacing closed files above the search read cap (open buffers already covered), and
  provider sources beyond files/open buffers/current buffer — the recent-files source is a
  recorded decision (no app-level opened-file history exists; it needs a history snapshot +
  streaming/replace semantics) and diagnostics/terminal sources need provider-specific snapshot
  protocols. Preview soft wrap/toggle landed (`Options.PreviewWrap`, `Alt+P`, bounded rows/cap,
  remapped spans, session default), so that item is gone. `docs/ui.md`.
- **R9-3 (CLOSED round 10, residual R10-2)** — round 10 landed quote-container fences
  (framed at the container, prefix on continuation rows, per-line literal fallback), setext
  heading styling/conceal with the thematic-break distinction, task-list glyph conceal, image
  alt-text conceal, the inline-HTML-literal decision, the mapped-decorations decision +
  no-paint tests, the horizontal-page clamp decision + test, and the dual line-anchor
  approximation/one-dual policy. What remains (R10-2): exact byte dual anchors, the native
  Markdown menu row, per-split mode, wrapped-cell selection painting only the primary row, and
  the composed-overlay ordering test. `docs/markdown.md`.
- **R9-4 (CLOSED round 10, residual R10-2)** — round 10 persisted the sidebar's plugin
  sections: stack order, requested body rows and folded state by plugin-panel id, additive/
  bounded Session.SidebarState fields, migration-safe, applied when the sections mount (unknown
  ids keep mount order/defaults; at most 64 sections, rows clamped to 1000, no content stored).
  What remains (R10-2): per-section widths and the drag-and-drop section order (the shared-
  column model and right-side floor are documented decisions). `docs/sidebar.md`.
- **R9-5 (P2, L, explorer rows landed round 10)** — round 10's explorer decoration/slot host
  API + git_explorer port closed the plugin-explorer-rows part. Remaining: composite buffers
  recorded as a native substitution (API surface only), dock/fullscreen placement, independent
  per-panel views, windowEmbed live panes, markdown-in-text, package manager/HTTP/project-local
  plugins, render_line. `docs/plugins/api-ui.md`.
Round 10 recorded four gaps:

- **R10-1 (P2, S)** — round-10 port reductions: git_grep (no prompt toolbar — footer state +
  Fresh's palette toggle; no preview band; the Finder search source has no cancel handle, so
  superseded queries are discarded by generation; hardened git gateway caps), git_explorer (only
  the workspace-root repository, no nested sub-repo discovery/hover tooltips/arbitrary icons, a
  setting change applies on the next refresh, ~1.5 MiB idle so opt-in), env_manager (no widget
  panel/watcher/devcontainer/authority, one local environment, set-only overlay, explicit shell
  activation, no persisted recipe, opt-in), and the earlier ports' items in R9-1. Optional
  line-target migration for code_tour/dashboard/git_log (S/M) and dashboard/code_tour polish
  blocked on R9-5 dock/per-panel views. `docs/plugins/porting.md`.
- **R10-2 (P2, S, overlay ordering/decisions closed round 11)** — residual UI polish: free
  per-split view mode, dual split persistence and the native Markdown menu row; exact byte dual
  anchors stay the recorded line-anchor approximation and wrapped-cell selection the recorded
  primary-row-only decision (hook rows have no source line/row-source map); sidebar per-section
  widths and drag-and-drop section order (shared-column model documented). The composed plugin-
  overlay ordering regression test landed in round 11, so that item is gone. `docs/markdown.md`,
  `docs/sidebar.md`.
- **R10-3 (P2, M, completed except R11-3)** — terminal completion landed in round 11: ConPTY
  (pure-Go x/sys/windows, GOOS=windows build/vet/test-compile verified; real-Windows acceptance
  checklist pending — parity unverified), child OSC 52 clipboard write (opt-in
  `clipboard.use_osc52` default off + Trusted only, selectors c/empty, restored transcripts never
  hooked, bytes still to x/vt; write-only) and transcript-only reflow (stored rows re-wrapped
  with styles/wide pairs/hard rows; text re-wrap, live screen clipped). What remains moved to
  R11-3: kitty keyboard, OSC 52 read/paste, live-screen reflow, terminal tabs, agent re-exec,
  report-split close_on_exit. `docs/terminal.md`.
- **R10-4 (P2, M/L, provenance first surface landed round 11)** — theme completion landed the
  first-surface painted-key provenance (`grid.Style.FGKey/BGKey` + opt-in capture; role table
  over `theme.Keys()` with aliases; tagging at construction for editor text/syntax/gutter/
  selection/current-line, status bar, menu, popup/palette/Settings rows and cheap chrome; the
  inspector prints exact keys and falls back to "matched by colour" for untagged painters).
  What remains (R11-4): drag-and-drop of theme values, background images, user-theme
  inheritance, and the untagged composition/plugin painters. `docs/themes.md`.
Round 11 recorded four gaps:

- **R11-1 (CLOSED round 12, reductions R12-1)** — the daemon end-to-end landed: server/attach
  lifecycle (single client, hello/resize/input/open_file/detach/ping, blocking detached reader,
  output to the attached client only, repaint/resize hooks, exit on Close), the attach proxy
  (raw mode/restore, stdin→input, output→stdout, notices→stderr, SIGWINCH), app options/branches
  (attach before requireTerminal; serve builds the listener before the runtime, default per-root
  name, session restore/save, detach row, open-file forwarding), CLI --daemon/-a/--attach,
  single-writer refusal for bare/session launches, docs/architecture §8 and the #19a outline.
  Reductions are R12-1 (foreground, no auto_attach start-if-absent/refusal policy, one client,
  single workspace, Windows pipes later). Tests: daemon server suite, CLI flags, app daemon
  start/attach/detach/reattach/quit/session-save and single-writer refusal.
- **R11-2 (P1, L/XL, catalogues landed round 12; extraction remains R12-5)** — the i18n
  foundation (#22a) and the catalogue/import/validation slice (#22b) landed: 15 embedded
  catalogues with English as source of truth, 13 keys mapped from Fresh (fresh_map +
  i18n_import --check) + 12 gvim-only keys, ValidateCatalogue/CoverageFor, i18n_check.sh CI,
  lazy loading, embedded rendering/fallback tests, plugin t() scoped to its own .i18n.json and
  the Settings locale row switching live. Remaining: extract the English boundary (generated
  CommandDef/menu tables, hand-written command rows, settings labels, other in-round strings)
  and expand translated coverage beyond the mapped subset (R12-5). `docs/i18n.md`.
- **R11-3 (P2, M, terminal tabs/agents landed round 12; remainder R12-2)** — round 12 landed the
  terminal tabs container, launch records/agent metadata with user-initiated resume, and the
  remote SSH terminal through the authority slot. Remaining: kitty keyboard (x/vt legacy-only,
  TODO/no seam), OSC 52 read/paste (write-only today), live-screen reflow (x/vt has none; stored
  rows re-wrap), live ephemeral PTY restore (restored tabs stay read-only/dead),
  workspace-scoped tab sets (#19), tab drag/reorder/per-tab splits, report-split close_on_exit.
  `docs/terminal.md`.
- **R11-4 (P2, S/M, decisions recorded round 12)** — the theme remainder is decision-level:
  background images are outside the colour theme model (Fresh's editor background support is
  separate), drag-and-drop of theme values is deliberately not added (the picker's drag moves
  selection; nothing reorders; the Settings row opens Select Theme), user-theme inheritance is
  one level of extends to a built-in theme (Fresh's own limit), and untagged compose/page-view/
  plugin painters keep the "matched by colour" inspector fallback. `docs/themes.md` §8.
Round 12 recorded five gaps:

- **R12-1 (P2, S/M)** — daemon MVP reductions: foreground (no fork/daemonize), no
  `auto_attach`/start-if-absent (a live per-root daemon refuses bare/session launches with an
  attach hint; file/--simple/--diff/stdin launches stay direct), one attached client, single
  workspace (launch directory) until #19, Windows named pipes later. The #19a workspaces slice
  builds on this ownership model. `docs/architecture.md` §8.
- **R12-2 (P2, S/M)** — terminal-tab residual: live ephemeral PTY restore (restored tabs are
  read-only/dead until Restart), workspace-scoped tab sets (#19), tab drag/reorder and per-tab
  splits, agent re-exec edges, report-split close_on_exit. Bound semantics live in
  docs/terminal.md.
- **R12-3 (P2, M)** — agent-session residual: no workspace/tmux discovery (#20b), no dock
  agent-state heuristics or workspace integration, agents are a config list of local executables
  (no plugin-provided registry, no remote agent launches), and no automatic re-execution (by
  design) — only the user-initiated restart. `docs/terminal.md`.
- **R12-4 (P2, L)** — remote-authority residual: editor remote file I/O (SFTP vs remote-helper
  decision in docs/authority.md §7), LSP routing through the long-running surface, plugin
  gvim.fs/gvim.proc routing, a per-remote trust store (today the local workspace's trust is
  used), terminal respawn after a dropped carrier, and devcontainer/k8s constructors.
  `docs/authority.md`.
- **R12-5 (P2, M)** — i18n English boundary: extract command/palette/menu titles (generated and
  hand-written rows), settings labels and other in-round strings into keys; expand translated
  coverage beyond the mapped subset and keep the CI validators. `docs/i18n.md`.

### 5.1 User-visible TUI and editing gaps (do first)

| # | Gap | Pri | Size | Owner area | Notes / depends on |
|---|---|---|---|---|---|
| 12 | Markdown residual: free per-split view mode, dual split persistence, native Markdown menu row; exact byte dual anchors stay the recorded line-anchor approximation and wrapped-cell selection the recorded primary-row-only decision. Round 9–11 landed the renderer core/fences/tables/wrapped cells/borders/dual/heading marks/container fences/setext/task-image conceal/page-view persistence/mapped-decisions/overlay ordering | P1 | L | `editor`, `syntax`, `internal/tui/editorpane`, `internal/tui`, `session` | R10-2 |

### 5.2 Plugin-API breadth (unblocks Fresh's plugin-era features)

| # | Gap | Pri | Size | Owner area | Notes / depends on |
|---|---|---|---|---|---|
| 13 | Plugin host remainder (tiers 1a–1c landed rounds 5–9; explorer/env edges round 10): composite API recorded as a native substitution, dock/fullscreen placement, independent per-panel views, windowEmbed live panes, markdown-in-text, package manager/HTTP/project-local plugins, render_line | P2 | L | `pluginapi`, `plugin`, `decorations`, `internal/tui/pluginpanel`, `internal/tui/plugin_*.go` | R9-5; explorer rows closed by round 10's git_explorer host API |
| 14 | Port the remaining bundled plugins. Landed through round 10 (diagnostics_panel, git_log/live_diff with full paths, compare_files, path_complete/lsp_help, Finder/lsp_navigation/find_references/dashboard/code_tour, git_grep/git_explorer/env_manager + explorer/env host APIs); round 11 evaluated the line-target migration (dashboard landed; code_tour widget-card and git_log historical-blob documented non-migrations). Remaining ports are daemon/remote/packaging-era: devcontainer, k8s-workspace, orchestrator/agent sessions, live_sessions, pkg (after #16), audit_mode (native substitution), language helpers. Native substitutions confirmed: markdown_compose/source, theme_editor, search_replace, composite buffers, dashboard start tab; candidates to confirm: live_grep, diff_nav, welcome/welcome_screen | P1 | L each; XL total | `plugin/bundled/plugins`, `plugin/bundled/lib` | R9-5/R10-1; §5.5 round 12 |
| 16 | Plugin package manager/registry (Fresh `pkg`: install/update/remove from git, registry sync, consent dialog) | P2 | L | `plugin/`, `shellcmd` | #13 |
| 17 | Workspace-trust enforcement + env: per-spawn executable-path gate and Blocked spawn denial for in-workspace executables (terminal/shell/formatter/LSP/plugin processes) remains (R1-4); env detection/activation landed round 10 (`env.detectors` host detection, shared trust markers, Trusted-only bounded-activation overlay injected into every spawn, Environment/Detectors pages, `gvim.env` + events) | P1 | L | `trust/`, `env/`, spawn sites | R1-4; round-10 env slice closed the env half |

### 5.3 Daemon, workspaces, remote, agents (architecture)

| # | Gap | Pri | Size | Owner area | Notes / depends on |
|---|---|---|---|---|---|
| 18 | Daemon mode: client/server split, attach/detach, open-file forwarding, socket lifecycle, direct-vs-daemon launch modes, single-writer policy; **landed MVP round 12** (R12-1 reductions: foreground, no auto_attach/start-if-absent, one client, single workspace, Windows pipes later) | P1 | XL | `internal/daemon`, `internal/app`, `internal/cli`, `internal/tui` | R12-1; docs/architecture §8 |

| 19 | Workspaces/windows on the daemon ownership model: durable workspace identity {stable id,label,root}, per-workspace files, protocol workspace-list/select/new/close/rename, lazy materialization with inert placeholders, dock/session switcher + palette + New Workspace, switching (stop program/keep runtime warm), per-workspace save/restore + exit ordering; then extract-tab, one-buffer-across-workspaces policy, per-session plugin state; first slice #19a (L), follow-ups #19b (M/L) | P1 | XL | `internal/daemon`, `internal/tui`, `session` | #18 landed; docs/architecture §8 outline; §5.5 round 13 |

| 20 | Agent sessions: Run Agent dialog, tracked launch argv/agent metadata/resume, session discovery/rejoin v1 (tmux scan + Import as workspace), dock integration; **partial round 12 (#20a)**: launch records/resume + Run Agent picker landed, #20b discovery/rejoin/dock integration remains | P1 | L | `internal/terminal`, `internal/tui/dock`, `config/terminal.go`, plugin packages | R12-3; §5.5 round 13 |

| 21 | Remote authority: SSH file/process/terminal/LSP backend, devcontainer attach, Kubernetes workspaces, remote indicator/reconnect; **first slice round 12 (#21a)**: authority slot + local parity + scp/ssh parsing + SSH terminal/one-shot exec + status/reconnect/password prompt; remote file I/O/LSP/trust-store/respawn/devcontainer/k8s remain | P1 | XL | `authority`, `internal/terminal/session_backend.go`, `internal/app/authority_app.go` | R12-4; docs/authority.md |


### 5.4 Polish and optional (P2)

| # | Gap | Pri | Size | Owner area | Notes / depends on |
|---|---|---|---|---|---|
| 22 | i18n: #22a foundation + #22b catalogues/import/validation landed rounds 11–12 (15 embedded locales, English source of truth, 13 Fresh-mapped + 12 gvim-only keys, ValidateCatalogue/CoverageFor, i18n_check CI, lazy load, Settings locale row live, plugin t() scoped). Remaining R12-5: extract the English boundary (generated CommandDef/menu tables, hand-written rows, settings labels) and expand translated coverage | P1 | L infra; XL translation | `i18n/`, `internal/tui`, `commands/gen`, `tools` | R12-5 |
| 23 | Terminal Windows support (ConPTY): **implemented round 11** pure-Go x/sys/windows (windows build tag, GOOS=windows build/vet/test-compile verified, pure-helper Windows tests) with a real-Windows acceptance checklist in docs/terminal.md; parity unverified until that pass is recorded, and the Windows row reads unverified | P2 | M | `internal/terminal` | R11-3 |
| 24 | Update check/self-update/telemetry: **DECIDED OUT OF SCOPE (Lead-signed, final)** — no telemetry, no update check/self-update, no network upgrade code; the status-bar `{update}` element stays unimplemented and `--no-upgrade-check` is a documented no-op. Recorded in §4 item 79 and §8 | out of scope | — | n/a | Lead sign-off 2026-11-08 |
| 25 | Platform edges: **DECIDED PARTIAL (Lead-signed, final)** — stdin-as-a-buffer landed round 11 (bounded snapshot, `--stdin-name` gvim-only, file-launch semantics); GPM console mouse, AltGr/CapsLock/key-release edges and plugin focus events beyond focus_gained/active_window are deferred/out of scope. Recorded in §4 item 80 and §8 | partial | — | `internal/cli`, `internal/app/stdin.go`, `internal/tui.RunWithInput` | Lead sign-off 2026-11-08 |
| 26 | Fresh GUI/desktop, browser `--web` UI, WASM build, packaging/installer matrix, daemon packaging: **DECIDED OUT OF SCOPE (Lead-signed, final)** — gvim ships as one pure-Go terminal binary; daemon mode itself (#18) stays in scope, daemon packaging does not. Recorded in §4 item 81 and §8 | out of scope | — | n/a | Lead sign-off 2026-11-08 |

### 5.5 Suggested round order for the remaining goal

1. **Round 13 — #19a workspaces, terminal-tab persistence, #20b discovery, #21 remote files/LSP.**
   #19a on the round-12 daemon ownership model: registry of per-workspace files (stable id,
   label, root, layout, authority_spec), protocol workspace-list/select/new/close/rename, inert
   lazy placeholders, dock/session switcher + palette + New Workspace prompt, switching = stop
   the tea program/keep the runtime warm/start the target, per-workspace save/restore + daemon
   exit ordering, single-writer policy, socket + uitest-PTY tests; then terminal-tab persistence
   per workspace (records/active tab in the workspace file) and workspace-scoped dock tabs.
   #20b: tmux scanner (level 0), attach-argv rows, Import as workspace, dock rows/naming, Run
   Agent dialog/dock integration after #20a. #21: remote file I/O (SFTP-first vs remote-helper
   decision; atomic saves; polling watch), LSP over SSH through the long-running surface,
   reconnect/respawn hardening, per-remote trust store. i18n R12-5: command/menu extraction +
   coverage/CI. Residual sweeps (R11-3/R11-4/R10-2/R9-2/R8-3 remainders).
2. **Round 14 — workspaces follow-ups, remote/devcontainer and the daemon-era ports.** #19b
   extract tab, one-buffer-across-workspaces policy, dormant/rename; #20b dock rows/Run Agent
   polish; #21 devcontainer/k8s authority constructors and remote UX polish (per-host trust,
   sudo-save residual, remote explorer); daemon-era plugin ports begin (orchestrator,
   live_sessions, agent_* family after #19/#20; devcontainer/k8s-workspace after #21; language
   helpers; pkg after #16; audit_mode stays a native substitution); #17 trust spawn gate/R1-4;
   residuals (kitty/live-screen reflow/OSC52 read, theme drag/background, per-section widths,
   dual anchors/menu row) and the Windows ConPTY real-host pass.
3. **Round 15 — final parity pass.** Freeze the Fresh reference (record commit/date), re-read
   refs/fresh docs/features + internal, re-run the §2 counts (206 core commands/palette 235,
   keymaps, themes, 22 bundled plugins vs Fresh's 50 + native substitutions, plugin API 242 vs
   ~372, settings schema, e2e feature index) and refresh every §3 row; walk §4 divergences and
   §5 residuals/closed ids, confirming each or recording a deliberate endgame gap; run the full
   gate set (incl. Windows and the existing parity floor/testdata fixtures) and the docs-vs-code
   audit; update §8 (final #24/#25/#26 decisions, composite API substitution, native
   substitutions, GUI/web/WASM/packaging scope) and produce the endgame report — close the goal
   or state the documented endgame. No new features in the final pass except fixes for
   verification findings.

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
- **Round 9 landed (2026-11-08).** Six tracks complete (Finder library + port wave, Markdown
  residual slice, search R8-5, LSP inline-hint tier 2, Contents R6-6/sidebar state, tier-1c
  remainder + declared-settings API); this audit ran the gates and reviewed the diff (no scope
  creep; §7):
  - **Finder library + port wave** (`docs/plugins/{bundled,porting}.md`):
    `plugin/bundled/lib/finder.lua` (three backends: Lua-ranked prompt over `gvim.ui.picker`,
    panel tree, locations finder; Fresh's fuzzy ranking with hard caps; lazy-required by
    diagnostics_panel so idle cost stays off the default set) and the ports `lsp_navigation`
    (Go to LSP Symbol + three gvim-addition list commands; Fresh fuzzy ranking re-sorted by
    source line, preview-on-move, Enter/Esc), `find_references` (command-driven Show/Hide
    References Panel, UTF-16 columns, 100-row cap, preview tabs), `dashboard` (read-only start
    tab: repo state, recent files, keyboard actions, `df` rows; auto-open/close, timer; no
    network/third-party/mouse/sessions) and `code_tour` (Fresh/VS Code tour JSON, bounds,
    floating widget card, step jumps + anchored decorations, state restore, starter writer).
    Tests `plugin/bundled/{finder_lib,lsp_navigation,find_references,dashboard,code_tour}_test.go`
    plus `internal/app/plugins_round9_test.go` (real-host dashboard/code_tour). Bundled set
    19/9/10; `merge_conflict` flipped off when the ten-plugin set hit 8.07–8.10 MiB (heap
    ~7.0 MiB of 8, ~1 MiB headroom; loader + attribution updated).
  - **Markdown residual slice** (`docs/markdown.md`): table widths memoized by (text version,
    page measure) — dual-pane width bug fixed; escaped pipes as cell text; container tables
    fall back literal; multi-visual-row cells via `PageExtraRows` continuation rows (rails,
    caret reveal, mapped presses, selection paints the primary row); fence continuation rails;
    heading-marker conceal + heading marks on the page-view scrollbar track (memoized);
    page-view mode/width session persistence (`session.FileState.pageView/pageWidth`, omitted
    when unset, migration-safe, restore after open); mapped occurrences/rulers/indent guides
    stay deliberately off in page view and composed overlays order after the compose. Tests
    `compose_{table_wrap,heading,fence_wrap}_test.go`, `pageview_heading_test.go`,
    `syntax/markdown_tables_escape_test.go`, `session/pageview_test.go`,
    `internal/restore/session_pageview_test.go`.
  - **Search R8-5** (`docs/ui.md`, `docs/keybindings.md`): preview protocol Start/Total/Spans
    with a bounded syntax highlighter (LRU 256 KiB/4000 lines) and `Options.SyntaxStyles`;
    preview scrolling (PgUp/PgDn, Ctrl+Home/End, wheel over the column, three-line notch),
    header window indicator (`Lfirst–last/total`), long-match paging without changing the
    replace range, the open-buffer-vs-closed-cap large-file replace distinction, and `Alt+A`
    third stop Open Buffers. Tests `searchpanel/panel_test.go` preview set.
  - **LSP R7-5 inline-hint tier 2** (`docs/lsp.md`): `internal/tui/editorpane/inline_hints.go`
    (display-only faint cells spliced before the anchor through one additive
    `Pane.InlineHints` hook; protocol position as a rune column, parts/padding, wrap/soft-break/
    wide/truncation handling, no caret-line hiding; only paintLineRow/cellOf/posAt touched),
    `lsphost` conversion, `internal/tui/lsp_data.go` wiring. Tests
    `editorpane/inline_hints_test.go` (13 cell-level), `tui/lsp_data_test.go`. R7-5 shrinks to
    universal server/process_limits/workspace-diagnostic cancel.
  - **Contents R6-6** (`docs/sidebar.md`): `internal/tui/sidebar_state.go` + `session/sidebar.go`
    + `internal/restore/sidebar.go`; saved-layout-only restore, bounded/migration-safe state
    (width form/side, explorer fold/selected path/scroll, Contents open/rows/window restored
    after first scan, Diagnostics); hidden-section heading-range publication + hidden rescans;
    `contents.fold_buffer` applies live (`ApplyLive`). Tests `sidebar_state_test.go`,
    `session/sidebar_test.go`, `restore/sidebar_test.go`.
  - **Tier-1c remainder** (`docs/plugins/api-ui.md`, `api-buffers.md`, `api-core.md`,
    `limits.md`): row-based scrollbar thumb with inserted rows (windowed/large keeps line-
    based), wrapped virtual rows (byte-safe chunks, first-chunk glyph/overlays, anchor mapping,
    caps), `setLineTargets/clear_line_targets` (10k/buffer, path/split-label caps, Enter/click,
    `into` a labelled split, owner cleanup, replacement semantics), fixed-height group leaves
    (`{type="fixed",height=N}` 1–100 rows re-applied on layout change; git_log's height-1
    toolbar now one row) and the declared-settings schema/values host view feeding read-only
    Settings Plugins rows. Tests `plugin_linetargets_test.go`, `plugin_groups_ratio_test.go`,
    `plugin/settings_api_test.go`, decor/group/scrollbar tests.
- **Round 9 integration notes.** The Lead fixed three gofmt drifts (`internal/tui/
  markdown_toc_fold_test.go`, `pluginapi/linetargets.go`, `pluginapi/settings_api.go`) before my
  gate run; gofmt and every static gate are clean. The statusbar team's transient model.go
  truncation/restore was verified from the final diff: model.go adds only 22 lines / removes 3
  (restoredScroll, restoreSidebarState in New, applyGroupFixedHeights in Update, lineTargetAction
  dispatch, and pressEditor returning tea.Cmd with the line-target press), the inline-hint
  wiring lives in `internal/tui/lsp_data.go`, no duplicate functions/blocks were found, the
  re-added lineTargetAction/pressEditor are exercised by `plugin_linetargets_test.go`,
  `sidebar_state_test.go` and the group-ratio/inline-hint tests, and the full suite is green.
  Metrics: palette 229 (unchanged); `gvim.d.lua` 233 functions/1449 lines; bundled 19/9/10; 44
  plugin command registration sites; idle heap ~7.0 MiB of 8; API level 2.
- **Round 10 landed (2026-11-08).** Seven tracks complete (git_grep, git_explorer + explorer
  host API, env-manager + env host slice, Markdown/sidebar R9-3/R9-4, terminal slice A, theme
  slice A, LSP leftovers); this audit ran the gates and reviewed the diff (no scope creep; §7):
  - **git_grep port** (`docs/plugins/bundled.md`, `porting.md`): Finder search-source extension
    (`plugin/bundled/lib/finder.lua`) so a source may wait on a process; hardened `gvim.git`
    gateway argv (`grep -n --column -I [-i] -e`, 15 s, 2 MiB, 100-row/per-line caps, exit 0/1
    success), repo resolution from the active buffer/workspace, case module-level with the
    palette toggle + footer state, no preview band, Enter opens line+column. Tests
    `plugin/bundled/{git_grep,finder_search}_test.go` + real-host `TestGitGrepRealHost`.
  - **git_explorer + explorer host API** (`docs/plugins/api-ui.md`, `api-core.md`,
    `limits.md`): `pluginapi/explorer.go` + `plugin/api_explorer.go` (set/clear decorations
    and slots, per-(plugin,namespace) store, 10k/64 caps, symbol/slot/leading limits, path
    normalization, priority→namespace→call resolution, owner cleanup), `internal/tui/
    plugin_explorer.go`, the explorer `RowDecor` painter (leading/trailing slots, suppress
    flags, filename colour, theme-key/RGB), `plugin/plugintest/fake_explorer.go`; the port
    (`plugins/git_explorer/`) uses `git status --porcelain -z`, symbols/priorities/colours,
    `color_names` ancestor slots, a debounced `.git` watcher, opt-in ~1.5 MiB. Tests
    `internal/tui/explorer/decor_test.go`, `plugin/explorer_api_test.go`,
    `plugin/bundled/git_explorer_test.go`, real-host.
  - **env-manager + env host slice** (`docs/trust.md`, `settings.md`, `plugins/api-core.md`):
    `config/env.go` (`env.detectors` user-layer-only, exact Fresh built-ins/validation),
    `trust/env.go` (shared markers, passive `DetectEnv`/require evidence), `env/provider.go`
    (Trusted-only bounded `sh` capture set/unset delta overlay), spawn injection (shell/
    formatter/LSP/pluginhost/terminal/git-grep) through `internal/app/run.go` + the new
    `envPort`, `pluginapi/env.go` + `plugin/env_api.go` + `internal/tui/plugin_env.go`
    (`gvim.env`, `env_changed`/`trust_changed`), Settings Environment rows + read-only
    Detectors page; the port has Fresh's four i18n command keys, status pill, auto path-only
    activation and trust-dialog request, opt-in. Tests `env/provider_test.go`,
    `trust/env_test.go`, `config/env_test.go`, `plugin/env_api_test.go`,
    `internal/app/settings_env_test.go`, `plugin/bundled/env_manager_test.go`, real-host.
  - **Markdown/sidebar R9-3/R9-4** (`docs/markdown.md`, `docs/sidebar.md`): quote-container
    fence framing + continuation prefix + per-line fallback, setext styling/conceal +
    thematic-break distinction, task-list glyph conceal, image alt-text conceal, inline-HTML
    literal decision, mapped-decor no-paint tests, horizontal clamp + dual line-anchor/one-
    dual decisions, sidebar plugin-section stack order/rows/folds persisted (additive bounded
    Session.SidebarState, applied on mount). Tests `compose_conceal_test.go`,
    `compose_setext.go` + tests, `compose_fence_wrap_test.go`, `pageview_clamp_test.go`,
    `plugin_sidebar_state_test.go`, session/restore sidebar tests.
  - **Terminal slice A** (`docs/terminal.md`): `internal/tui/dock/hover.go` + input/panel
    changes (drag auto-scroll band/direction + forwarding + fixed 80 ms seq-guarded tick and
    stops, word/line-wise drag extension, Ctrl+hover underline + `file:line:col` parse/
    resolve/open through OpenPathMsg Line/Col, LastYank/`pass_ctrl_v_without_text` matrix);
    kitty/ConPTY documented residuals. Tests `dock/{mouse,hover}` tests.
  - **Theme slice A** (`docs/themes.md`, `settings.md`): six bundled themes' accent overrides
    removed (`theme/data/*.json`) with `theme/accent_audit_test.go` + `internal/tui/style/
    accent_audit_test.go` pinning accent==popup selection and painted roles; Settings theme
    preview (`internal/app/settings_theme_preview.go`, `SettingsItem.Preview`, cached
    catalogue chips); `internal/tui/theme_picker.go` colour picker/mouse overlay (palette =
    working theme colours + ANSI, live preview, Enter applies dirty, Esc/outside restore);
    URL/packaged decision recorded. Tests `theme_picker_test.go`, settings preview tests.
  - **LSP leftovers** (`docs/lsp.md`): `lsp/{raw,limits,limits_linux,limits_other}.go` +
    `internal/lsphost` pullSet + `internal/tui` status popup row + config validation;
    workspace/diagnostic cancel via `protocol.Call` (supersede/stop/restart cancel, never
    cache/merge cancelled, timeout fallback), Linux cgroup v2 process limits with `sh ulimit -v`
    memory fallback and per-OS recorded unreachability, universal-server decision recorded.
    Tests `lsp/cancel_test.go`, `lsp/limits*_test.go`, `internal/lsphost` pullSet test,
    `config`/`lsphost`/`tui` limits tests.
- **Round 10 integration notes.** The Lead fixed the explorer painter after plugin/trust
  finished: the trailing slot (plugin override/decoration symbol/native git mark) is written at
  `end-tw` and the row's end is that column, so the filename ends exactly before it instead of
  one column early over the padding cell — no forced gap. Verified in the final diff and by
  the native git-mark tests, plugin decor tests and git_explorer tests (all green, plus this
  audit's targeted run). The gate set now includes `tools/gen_menus.py --check` (clean).
  Metrics: bundled 22/9/13; 47 plugin command registration sites; idle heap ~7.25 MiB of 8
  (7.22–7.27, ~9.3 baseline); palette 229 (unchanged); `gvim.d.lua` 242 functions/1478 lines;
  API level 2; settings 69/14 + Environment/Detectors; no leftover temp harnesses (the new
  `internal/app/plugins_round10_test.go` is a permanent real-host test).
- **Round 11 landed (2026-11-09).** Six tracks landed, the daemon MVP is partial by design
  (task-68 stays in progress); this audit ran the gates and reviewed the diff (no scope creep;
  §7):
  - **Daemon #18a (partial)** (`internal/daemon/{protocol,socket}.go` + tests): the protocol
    frame stream (4-byte big-endian length + type byte + payload, `MaxPayload` 1 MiB; client
    hello/input/resize/detach/open_file/ping, server output/notice/detached/pong/exit; an
    oversized frame fails the connection) and the Unix socket (user cache dir 0700, per-root
    default name slug+8-hex of the absolute root, liveness probe, stale unlink, live-daemon
    refusal, socket 0600, `Attach`). Unit tests: frame round-trip/bounds, refused live daemon,
    socket perms, name/path. NOT landed (R11-1): app.Options Reader/Writer + `tui.RunWithIO`
    socketIO hosting/repaint, CLI `--daemon`/`-a`/`--attach`, Detach hook/palette row/ack,
    single-writer refusal, `auto_attach`, session/PTY integration, docs/architecture and the #19
    design outline; round 12 continues with the Phase-1 #18a/#19a plan.
  - **i18n #22a foundation** (`i18n/` + `catalogues/en.json`, `docs/i18n.md`, config/settings):
    the TUI-free leaf package (15 locales metadata, `Normalize`/`Resolve` from config then
    LC_ALL/LC_MESSAGES/LANG then en, `T` with %{name}, `TN` with Fresh's CLDR plural rules incl.
    ru/uk/cs/fr, `Register`, embedded English catalogue, locale→shorter-tag→en→key fallback);
    config `locale` (null/auto/15 codes, user-layer only; project config refused with a
    diagnostic); Settings Interface locale row (null encodes auto; ApplyLive pushes
    `i18n.SetLocale` + `plugin.Runtime.SetLocale` live); startup resolves before the UI/plugins;
    pluginhost locale + Lua `gvim.i18n.locale`; first extraction slice (editor clipboard
    statuses, root copied-path/formatted/inlay notices, dock notices) with English-identical
    behaviour and the remaining inventory documented. Generated command/palette/menu titles stay
    English by design (R11-2). Tests incl. `i18n/i18n_test.go`, `config/locale_test.go`,
    settings-locale, pluginhost/i18n tests.
  - **Terminal completion R10-3** (`docs/terminal.md`, `internal/terminal/`): `ptyProc` seam +
    Unix adapter; the Windows ConPTY path is pure-Go x/sys/windows under a windows build tag
    (pipes, `CreatePseudoConsole`, STARTUPINFOEX attribute list, `CreateProcess`,
    `ResizePseudoConsole`/`ClosePseudoConsole`; pure helpers tested Windows-only; GOOS=windows
    build/vet/test-compile verified; real-Windows acceptance checklist written, parity
    unverified); the child OSC 52 write parser (bounded/chunk-safe ES]52, selectors c/empty,
    recovery) with `Session.SetOSC52Handler` gated by new `clipboard.use_osc52` (default off)
    AND Trusted workspaces, restored transcripts never hooked, bytes still to x/vt; transcript-
    only reflow (`ReflowLines/ReflowRows`: styled cells/wide pairs kept, hard rows stay, text
    re-wrap not semantic reflow, dock reflows on content-width change, restored mismatch
    notice; live screen clipped). Kitty keyboard remains a precise residual (x/vt TODO/no seam);
    pass_ctrl_v was complete. Tests: pty/windows helpers, osc52 parser/unix, reflow, dock
    integration.
  - **Theme R10-4 provenance (partial)** (`docs/themes.md`, `internal/tui/{grid,style,
    theme_inspect}.go`): `grid.Style.FGKey/BGKey` uint16 theme-key ids + an opt-in per-cell
    capture buffer (`CaptureKeys`/`ReleaseKeys`/`KeysAt`; no allocation/size/render change when
    off); the dense role table over `theme.Keys()` with canonical alias resolution
    (`style/roles.go`, `TaggedCell/TaggedStyle`); tagging at construction for overlays,
    editorpane text/selection/gutter/current-line/all syntax kinds, the status bar and menu band
    plus cheap explorer/sidebar/diagnostics chrome; the inspector prints exact
    `Painted by (fg/bg): section.key` lines and the pair line, with an explicit "matched by
    colour" fallback for untagged sides. The picker/drag was deliberately not extended; residual
    drag-and-drop/background images/user-theme inheritance. Tests: grid keys capture, style
    roles, editorpane roles, inspector provenance.
  - **Port follow-ups + stdin-as-a-buffer (#25 partial)** (`README.md`, CLI help,
    `docs/plugins/porting.md`): dashboard recent-file rows are line targets now (per-paint
    {line,path}, cleared on close/unload; the mode keeps Enter so the win is a mouse click;
    command rows cannot be targets), while code_tour (widget-panel rows/decoration anchoring)
    and git_log (historical blob Enter vs working-tree target) were evaluated and recorded as
    non-migrations in the new porting section. stdin-as-a-buffer: `--stdin`/`-` (and piped stdin
    with no files), bounded 32 MiB, CRLF/EOL handling, empty valid, conflicts validated, an
    untitled `[stdin]` buffer with label override/language sniff/unmodified-on-open/binary
    warning, file-launch session semantics (never restored/saved/hot-exit), `tui.RunWithInput` +
    `tui.OpenInputTTY` additive (`Run` untouched), a PTY piping e2e test, README/docs; recorded
    differences (no streaming spool/daemon stdin; gvim-only `--stdin-name`). Tests: `internal/
    cli/stdin_test.go`, `internal/app/stdin{,_ui,_pty_unix}_test.go`.
  - **LSP small residuals** (`docs/ui.md`, `docs/keybindings.md`, `docs/markdown.md`): preview
    soft wrap/toggle (`Options.PreviewWrap`, `Alt+P` in panel and editor, wrapped rows with
    syntax-span/match remap, 8-row cap + ellipsis, display-row window, session default; the
    recent-files provider source recorded as a decision needing a new history snapshot); the
    composed plugin-overlay ordering regression test (pipeline order verified); wrapped-cell
    selection decision recorded (hook rows have no source line/row-source map) and the dual/per-
    split wording tightened (line-anchor approximation, one dual per document); R7-4 explicit
    unbind tombstones (keymap `Unbound` + TUI `UnbindKeymap` port/adapter; the overlay router
    swallows an unbound chord instead of the built-in fallback; tests).
- **Round 11 integration notes.** No Lead code fixes this round; the audit verified the shared-
  file hunks: `internal/tui/model.go` (`RunWithInput` + `paint`/`paintInto` split; `Run`
  unchanged), `internal/app/{run,options,session_plan}.go` (stdin read/buffer wiring, locale
  resolution before UI/plugins, file-launch session semantics), `internal/cli/root.go`
  (--stdin/--stdin-name validation), `internal/tui/ports.go`/`editorpane/styles.go` (tagging
  hooks) and `internal/keybindings/adapter.go` (Unbound); no lost/duplicated hunks, no duplicate
  top-level functions and `git diff --check` is clean. The statusbar paint is drawn through the
  same `paintInto` refactor (no separate painter file); the theme inspector's captured repaint is
  additive. Metrics unchanged: 22 bundled/9 on/13 off; 47 registration sites; idle heap
  ~7.25 MiB of 8 PASS; palette 229; `gvim.d.lua` 242 functions/1478 lines; API level 2;
  settings 69/14 + Interface locale. No leftover temp harnesses.

- **Round 12 landed (2026-11-09).** Six tracks landed (daemon #18a MVP, terminal tabs, #20a agent
  sessions, #21a authority + SSH, #22b i18n catalogues, LSP residual sweep); this audit ran
  the gates and reviewed the diff (no scope creep; §7):
  - **Daemon #18a MVP** (`internal/daemon/{server,attach,attach_resize_*}.go`, app/CLI/TUI wiring,
    docs/architecture §8): server attach lifecycle (one client, hello/resize/input/open_file/
    detach/ping; detached input blocks without ending the editor; output only to the attached
    client with write-error detach; `OnProgram` repaint/resize; `OnOpenFiles`; Close sends exit)
    and the attach proxy (hello+size, optional open-file frame, raw mode/restore, stdin→input,
    output→stdout, notices→stderr, SIGWINCH); app Options.Daemon/DaemonMode/DaemonOptions +
    validation, attach branch before requireTerminal (no runtime/session), serve branch (listener
    before buildRuntime, per-root default name, socket line, detach wiring,
    `tui.RunWithIO(srv,srv,OnProgram)`, open-file forwarding, session restore on start/save on
    exit), single-writer refusal for bare/session launches (file/--simple/--diff/stdin direct),
    palette Detach under --daemon; CLI --daemon[=NAME]/-a/--attach[=NAME]; import rule R2
    allows internal/daemon to host the TUI program. Tests: server forwarding/refusal/detach-
    reattach/exit, CLI parse/conflict table, app start/attach/first-frame/detach/reattach/quit/
    session-save + single-writer refusal. Reductions R12-1.
  - **Terminal tabs** (`internal/tui/dock/tabs.go`, `commands/terminal_commands.go`,
    session/restore formats, docs/terminal.md): up to 16 single-session tabs (each with
    screen/scrollback/prior transcript/selection/links/launch record), a strip with active/
    exited/click-select, Ctrl+PgUp/PgDn (not keymap data), palette New/Close/Next/Previous/
    Rename Terminal Tab (hand-written English; palette 235), per-tab close_on_exit, background
    tabs synced/painted-only-when-active; `terminal.json` gains additive `tabs[]`/`active_tab`
    with top-level mirror, bounds (16; per-tab 500 rows/1 MiB) and old-file migration; restored
    tabs read-only/dead (no auto-restart), Restart per tab with the saved launch record.
    Tests: dock tabs unit/UI, restore/terminal_tabs_test. Residuals R12-2.
  - **#20a agent sessions** (`internal/terminal/launch.go`, dock launch records,
    `internal/app/terminal_agents.go`, config terminal resume_agents/agents, docs/terminal.md):
    Launch argv/agent/resume-argv/ephemeral bounded at 64×1024/agent 64, persisted with the
    transcript (no auto-run on restore), user-initiated restart precedence resume→launch→shell
    gated by terminal.resume_agents (Fresh key, default true), config terminal.agents (gvim
    extension, user-layer only, bounded; names+command+args+resume_args), agent naming in tab
    titles, Run Agent... palette row + picker (hand-written English). No workspace/tmux
    discovery yet. Tests: dock agent, agent run/picker, config decode bounds. Residuals R12-3.
  - **#21a authority + SSH** (`authority/` incl. authoritytest fake, `internal/app/authority_app.go`,
    `internal/terminal/session_backend.go`, docs/authority.md + architecture row): the slot with
    FileSystem/OneShot/LongRunning/Terminal + State/Label/Connected/Reconnect/Close; local
    forwarding byte-identical to the plugin OS port with trust-gate + env choke point;
    Fresh-compatible scp/ssh:// parsing (ssh:// exclusive, malformed startup error, never
    stat'ed, no local fallback; remote excludes local files/root/diff/stdin/restore/--daemon)
    with typed diagnostics; SSH backend via cached pure-Go x/crypto/ssh (agent/identity/
    passphrase/password, known_hosts, POSIX-quoted one-shot exec, long-running stdio, PTY
    terminal + resize/reconnect); status segment SSH:user@host / disconnected (hand-written
    English, i18n keys reserved); reconnect-on-activate + password prompt; remote launches open
    no local files/session and open the remote terminal at Workspace() with a notice; one-shots
    (dock terminal/git grep/shell/formatter) routed through the slot. Tests: authority local
    forwarding/gate/env/watch, spec table, SSH one-shot/state/reconnect/terminal/long-running.
    Residuals R12-4.
  - **i18n #22b catalogues** (`i18n/{catalogue.go,fresh_map.json,locales/*.json}`,
    `tools/{i18n_import.py,i18n_check.sh}`, docs/i18n.md): 15 embedded catalogues (English
    source of truth; 14 files carry the 13 mapped keys imported from Fresh; 12 gvim-only keys
    stay English by design; 12,646 bytes, 64 KiB cap test), deterministic import with --check,
    ValidateCatalogue/CoverageFor (key shape/plurals/placeholders/duplicates/unknown/coverage),
    CI script, lazy loading, embedded German rendering + fallback tests, plugin t() scoped to
    its own .i18n.json. English boundary documented (generated CommandDef/menu tables,
    hand-written command rows incl. terminal-tab/Run Agent, settings labels, other in-round
    strings). Tests: catalogue/validate/coverage/import/render. Residual R12-5.
  - **LSP residual sweep**: R1-3 closed (config show trust-gated read-only via
    app.ProjectConfigTrusted; skip note; trusted unchanged; config paths ungated; tests/docs/
    CLI Long); R4-4 partial (virtual-caret persistence + docs/internal/virtual-space-scoping.md
    with exact residual list); R2-5 deferred with docs/internal/hot-exit-store.md; R8-3 exact
    decisions in docs/settings.md §6; R11-3/R11-4 decisions recorded (docs/terminal.md,
    docs/themes.md); R1-2 verified closed and its stale open entry removed.
- **Round 12 integration notes.** The Lead wrote the daemon track after the trust owner hit its
  context limit; the audit verified the shared-file interactions: internal/app run/options/
  session_plan/daemon_runtime/authority_app (daemon branches before requireTerminal, listener
  before buildRuntime, authority slot + one-shot routing + remote UX, terminal agents/dock
  effects), internal/cli root/args (daemon flags, remote target parsing before stat),
  internal/tui model/ports (RunWithIO/Detach/OpenFileMessage/authority status/tab actions),
  internal/pluginhost Wiring.OS (local authority over the same OS instance), config terminal
  agents/resume_agents bounds, session/restore formats (virtual caret; terminal tabs/launch
  records with old-file migration), and the daemon/authority/terminal-tab/session interactions
  (single-writer refusal, detach keeps the editor alive, restored tabs dead, launch record
  never auto-runs). No duplicate top-level functions were found; `git diff --check` reports
  only a new blank line at EOF in docs/architecture.md (docs hygiene, trim before commit).
  Metrics: palette 235; bundled 22/9/13, idle heap ~7.25 MiB of 8 PASS; plugin API 242
  functions/1478 lines; 47 registration sites; API level 2; i18n 25 English keys / 13 mapped /
  12 gvim-only / 15 locale files (12,646 bytes); terminal tabs max 16. No leftover temp
  harnesses.
- The gaps the rounds opened are at the top of §5 (R2-5, R3-3…R3-4, R4-4…R4-5,
  R5-2…R5-6, R6-1…R6-3, R7-4…R7-5, R8-1…R8-4, R9-1…R9-5, R10-1…R10-4, R11-2…R11-4,
  R12-1…R12-5; #7/#8/#11/#15, R1-1, R1-2, R1-3, R2-4, R5-1, R5-5, R6-5, R6-6,
  R7-1/R7-2/R7-3, R8-5, R9-3/R9-4 and R11-1 are closed; R10-3 completed except R11-3 and
  R10-4/R11-2 moved to residual lists); §5.5 is the round-13..15 plan built from the ranked
  backlog minus the closed items. The #24/#25/#26 decisions are final (Lead-signed) and
  recorded in §4 items 79–81, §5.4 and §8.
## 7. Verification limits and risks

- Statuses are read evidence from the tree at the time of writing. Rounds 2–12 had their full
  gate sets run by this audit on their trees: build ×2, vet, golangci-lint 0 issues, gofmt clean,
  purity and import rules green, the generated-menu check clean, and (from round 11) the
  GOOS=windows go build ./... cross-gate. Round 12's go test ./... -count=1 had two failures:
  internal/app `TestDaemonHostsAttachDetachAndQuit` (the attach read a repaint/output frame
  before the expected pong under full-suite load — a frame-timing sensitivity in the test, not
  a protocol bug) and plugin/bundled `TestViSuspendedDropsItsStatusAndMode` (the known
  full-parallel load flake); both pass repeatedly in isolation (2x). The other 70 packages
  pass. The lsp/manager dynamic-registration and authority SSH under-load hangs reported by
  peers were also isolated-green here. Round-1 statuses were read-code only (no gates were run
  by this audit then). A feature that exists but is broken would still read "implemented": treat
  a status as a pointer, not proof.
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
- Round-2/3/4/5/6/7/8/9/10/11/12 diff reviews (this audit): no scope creep outside the
  declared tracks. Round 12's 86 files (50 modified, 35 untracked, 1 deleted; tracked diff
  +1764/−244) stayed within the six tracks and their docs; the generated tables, refs/ and
  _attic/ were untouched; no golden tests were added. Shared files were verified for
  lost/duplicated edits (no duplicate top-level functions found): internal/app run/options/
  session_plan/daemon_runtime/authority_app, internal/cli root/args, internal/tui model/ports/
  dock glue, internal/pluginhost Wiring.OS, config terminal agents/resume_agents, and the
  session/restore formats (virtual caret; terminal tabs/launch records with migration).
  `git diff --check` reports only a trailing blank line at EOF in docs/architecture.md (docs
  hygiene; trim before commit). Round-12 findings (recorded, not fixed): (a) the daemon attach
  test's frame-order assumption is timing-sensitive under load — recommend reading/tolerating
  output frames until pong (or a keepalive frame) so the test cannot fail on repaint timing;
  (b) no code bugs found in spot checks — daemon server/attach lifecycle and single-writer
  refusal, tab bounds/migration/exited markers, launch-record bounds/resume precedence,
  authority spec/local parity/trust+env gate/SSH one-shot+terminal, i18n import/validation/
  coverage/fallback, LSP sweep docs all match docs/tests; (c) metrics verified (palette 235,
  22/9/13, ~7.25 MiB, 242 functions/1478 lines, 47 registration sites, API level 2, i18n 25
  English/13 mapped/12 gvim-only keys, 15 locale files 12,646 bytes, max 16 tabs); (d) the
  deliberate divergences are §4 items 18 and 40–87, including the #24/#25/#26 final decisions
  and the recorded daemon/terminal/agent/remote/i18n reductions.
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

Scope decisions with Lead sign-off (final; full wording in §4 items 79–81):

- **#24 update/self-update/telemetry: out of scope.** gvim adds no update check, self-update,
  telemetry or network upgrade code; the status-bar `{update}` element stays unimplemented and
  `--no-upgrade-check` is a documented no-op.
- **#25 platform edges: partial.** stdin-as-a-buffer is implemented (gvim's bounded-snapshot
  form, with the gvim-only `--stdin-name`); GPM console mouse, AltGr/CapsLock/key-release
  edges and plugin focus events beyond focus_gained/active_window stay deferred/out of scope.
- **#26 GUI/web/WASM/packaging: out of scope.** gvim ships as one pure-Go terminal binary;
  daemon mode itself (#18) is in scope, daemon packaging is not.
- gvim-only additions from round 11 kept deliberately: the `i18n` locale row/choice semantics
  (null = auto), the provenance capture repaint (inspect-only), the opt-in/trusted-only OSC 52
  write, and the transcript text re-wrap.
