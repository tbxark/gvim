# Fresh parity record

Status: live working record for the "full Fresh feature parity" goal, first written 2026-11-08 and
refreshed after rounds 1–13 landed. Scope is Fresh's TUI functionality (Lua implementation and
visual style are the only expressly flexible parts); the objective boundary is recorded in §8.
Rounds 1–10 closed the workspace-trust surface/sidebar/status pills, virtual space/keymaps/status
bar/sessions, settings/terminal slice A/explorer/palette, LSP 4a/project search/settings/markdown
smart editing, LSP 4b A–D/plugin 1a/theme/terminal B/status elements, LSP 4b E/plugin 1b/
diagnostics_panel/markdown compose/Contents fold/search preview, LSP R6-5/plugin 1c-A/git_log
live_diff/markdown fences-tables/overlay keymaps/settings slice, plugin 1c-B-C/#15 compare/
compare_files/markdown dual/settings R7-3/path_complete+lsp_help/live_diff-git_log full paths/
streaming search, Finder port wave/dashboard-code_tour/markdown residual/search R8-5/inline-hint
tier 2/Contents state/tier-1c remainder, and git_grep/git_explorer/env-manager/explorer-env host
APIs/markdown-sidebar residuals/terminal+theme slices/LSP leftovers. Round 11 added the daemon
protocol/socket core, i18n foundation, terminal completion, theme provenance, stdin-as-a-buffer,
line-target migration and small LSP residuals; round 12 the daemon MVP end-to-end, terminal
tabs, #20a agent sessions, #21a authority+SSH terminal/exec, i18n catalogues and the LSP residual
sweep; round 13 the workspace registry/protocol/lifecycle (#19a), terminal tabs scoped per
workspace, tmux discovery/Import (#20b first slice), remote editor file I/O + LSP over SSH, the
spawn gate (#17), and the second i18n extraction (palette/menu/settings/authority). Every status
below was checked in this tree or in `refs/fresh`, not inferred from the old migration notes.
Update the affected row when a round lands instead of rewriting it.

Last verified: 2026-11-09, tree commit `d53d2cd` (the previous round's commit is folded in) plus the
merged round-45 work. Static gates PASS (Lead; default GOCACHE: build, CGO_ENABLED=0,
GOOS=windows, vet, golangci-lint 0, purity, import rules, menus, i18n_check with catalogue heap en
122 KiB / de 82 KiB / both 205 KiB, git diff --check). Final full
`go test ./... -p 1 -count=1 -timeout 1200s` EXIT 0: 76 packages ok, no FAIL line (log
`/tmp/gvim-r45-final2.log`). The audit ran build/gofmt/i18n_check plus targeted round-45 tests/diff
review. Real-host SSH/jump-box/agent-forwarding/search/explorer/replace/clipboard/streaming large-
file-save/plugin realpath-walk-copy, keychain security/secret-tool, real-tmux, Windows ConPTY,
real-host OSC52, multi-head/PTY workspace switching remain UNTESTED. Fresh reference: `refs/fresh`
same date.

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
auto-open/close, timer refresh; task177 added the upstream tracking row — `↑ahead ↓behind` or
`no upstream`; reduced: weather/GitHub/network, third-party section API/dock placement, mouse and
sessions remain OPEN and are not closed by no-network/dependency reasons; icon/style differences
are acceptable) and `code_tour` (Fresh `.fresh-tour.json` and VS Code
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
server owns the round-12 attach lifecycle (single-client at r12; r19 multi-client landed: broadcast/any input/smallest size), hello/resize/input/open_file/detach/ping,
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
r12: no daemon auto-start on launch (refusal policy) and single-client; r19 auto_attach default-off + multi-client landed; remaining reductions:
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

**Round 13 outcome (2026-11-09).** Six tracks landed; scope classification is now TUI-only:
**Workspace registry + lifecycle (#19a).** `internal/daemon/workspaces.go` implements the durable
registry on the round-12 daemon: one file per canonical root under the user data dir
(`<data>/gvim/workspaces/<encoded-root>.<stable-id>.json`, `GVIM_WORKSPACE_DIR` override), schema
v1 {version, stable_id minted once/adopted, label, root, last_focused_at, layout}
(`session.Session` layout captured later; `authority_spec` is a future additive field), atomic
temp+fsync+rename saves with no temp leftovers, newer-version refusal (never overwritten),
corrupt-file skipping without deletion, canonical/symlink dedup one workspace per root,
freshest-wins arbitration, and the session data under `workspaces/sessions/<stable-id>/`
(registry file removed but session data kept on close). The workspace protocol adds
workspace_list/select/new/close/rename client frames and a `workspaces` frame (list + active),
`Server.OnWorkspaceRequest` with notices for malformed/unknown requests and a no-workspace-layer
daemon; `Hello.Workspace` is parsed but the round-12 attach path still ignores it (open gap).
`internal/app/workspace_host.go` runs one tea program over a `tui.WorkspaceHost` that swaps the
active model; the launch root is materialized eagerly and the rest are inert descriptors; a
switch captures the outgoing workspace (snapshot on the UI goroutine, ordered per-workspace
asynchronous writer: stash files/session JSON/terminal record/search/layout, two snapshots
written in order, no disk I/O on the UI loop), builds a dormant target off the UI goroutine
(sequence number + cancellable context; stale results are closed; a failure leaves the current
workspace and notices), keeps warm runtimes (LSP/plugins/dock shells; switching back is a model
swap that does not re-run Init), and exposes Workspace: Switch/New/Close/Rename/Next (daemon
only), a switcher list, root-then-label prompts and a hand-written `WS <label>` status item.
Close switches away when closing the active workspace, cannot close the last one, captures and
closes a materialized runtime (registry file removed, session data kept, dock killed), and a
daemon quit snapshots every materialized workspace, waits for all writes (active first) before
closing the runtimes. An open-file request for a path open in **materialized** workspace switches
instead of duplicating (single-writer v1); discovery Import uses the additive
`WorkspaceLauncher.CreateWithLaunch` to queue the attach argv in the new workspace's dock.
Tests: registry CRUD/ids/dedup/version/atomicity/corrupt-file; wire round-trips/validation/
dispatch; manager port lifecycle; async materialize/stale/failed switch; capture compare vs
restore.WriteSession; capture-write-failure reporting; create-with-launch; terminal tabs across
switch/close/capture/restore. **Terminal tabs per workspace.** The dock keeps ≤16 single-session
tabs with strip/active/exited/click-select, Ctrl+PgUp/PgDn, palette rows, tab titles from the
launch argv/agent name with Fresh's ` (2)` suffix and `(exited)`, per-tab close_on_exit, and an
additive `SavedState` `tabs[]`/`active_tab` with mirror/bounds/old-file migration. In daemon
workspaces each workspace has its own dock/tab set (`terminal.json` in its session dir): warm
tabs keep live PTYs across switches, a dormant workspace restores read-only/dead tabs (no
auto-start), and a closed workspace kills its shells while keeping the transcript. Fresh's live
tab restore and tab reorder/per-tab splits remain open (R12-2/R13-2).
**Discovery + Import (#20b first slice).** `internal/agentdiscover` is a TUI-free, read-only tmux
scanner: walk `$TMUX_TMPDIR` (else /tmp) for tmux sockets, bounded `list-sessions`/`list-panes`
when commands are allowed (2 s per command, 256 KiB output, 200 sessions/2000 panes/500
sockets/2000 dirs), qualified `tmux -S <sock> attach -t <name>` argv, session path/created/
attached/panes, and probable-agent rows by pane foreground-command name match (built-in set +
`terminal.agents` basenames; a name match, not identity). The scan command goes through the
spawn gate then path resolution to the judged executable; commands off gives socket-only rows
saying a server exists but its sessions are unknown. The TUI picker (Discover Agent Sessions...,
hand-written English) scans off the UI loop, shows problems/filters/re-scans (Ctrl+R), Enter
opens the attach argv in a new ephemeral dock tab named `tmux:<session>` (launch+resume argv, so
Restart reattaches), and `i` imports: create/reuse the workspace for the session path, labelled
`tmux:<session>`, and queue the attach argv in that workspace's dock (durable source identity
`tmux:<server>/<session>`; a repeat import focuses its existing tab). Registry v1 has one
workspace per root and no initial-launch field, so a second tmux session in one directory shares
the workspace with a second attach tab; Import needs a daemon-hosted session; a restored attach
tab comes back dead; no dock/tmux rows outside the picker, no screen/zellij/remote scanning.
**Remote editor/LSP (#21 continuation).** `authority/ssh_files.go` mounts a real remote file
system implemented as bounded `sh -c` helper commands over the existing SSH exec channel (this
is the documented fallback chosen on evidence: the offline module cache has no usable SFTP
implementation and no network, the dependency policy is pure Go, and Fresh's remote filesystem
requires Python 3 on the host, which this port deliberately does not require). Read is bounded
by `head -c max+1` and a 64 MiB cap; stat understands GNU and BSD/macOS `stat` and reports
kind/size/mtime/mode with the port's symlink rules; ReadDir is name-sorted with the hidden
filter and a 5000-entry/4 MiB cap; writes support truncate/append/Parents/Atomic with a
same-directory `mktemp`+`cat`+`mv -f` rename that preserves the target mode and writes through
a symlink; Watch polls (500 ms, coalesced over the 50 ms debounce) because a remote host has no
fsnotify; helper stdout/stderr are 64 KiB-capped and overflow is an explicit error, never a
truncated success; paths/modes are POSIX-quoted and never shell-evaluated. The document file-I/O
seam (`document.FileAccess`) routes loads/saves/Save As/Revert/disk checks through the remote
access with no local fallback; the startup open is async and retried once after a reconnect; a
failed/disconnected open is a notice, never an empty local buffer; saves keep the buffer
modified on failure. Remote LSP starts servers on the remote host through the authority's
LongRunning surface (a manager `SetSpawner` seam, nil means local; remote initialize root and
login environment; stdio adapter; stderr tail captured; --process_limits recorded as not
applied remotely; the spawn gate runs before the spawner) and a successful reconnect restarts
the servers that were running, once each, without duplicates (the spawn gate runs for every
restart). Open gaps: remote explorer/listing, background remote external-change watch, remote
large-file lazy mode, remote crash recovery/hot-exit (deliberately not copied locally), ~user
expansion, special-file kinds, sudo save, reconnect retry/backoff, terminal respawn after a
dropped carrier, a per-remote trust store (the local workspace trust is used), and
devcontainer/k8s constructors. A real-SSH manual acceptance pass has not been run here.
**Spawn gate (#17).** `trust.DecideSpawn(level, root, cwd, argv, pathValue)` runs before every
launch: the authority's local Run/Start/dock Launch (git/find/shell/formatter/terminal), the
plugin OS (gvim.proc.run/spawn), the LSP manager (auto-start/manual/universal), and the
discovery runner. It resolves a bare name through the child's effective PATH (parent, project
env overlay, launch env; empty entry = cwd; relative entries), executes the resolved absolute
path so resolution matches the decision, checks explicit paths lexically and through symlinks
on both root and candidate, and checks the first token of the shell runner's `-c` string.
Blocked refuses every spawn; Restricted/Undecided refuse only project-internal executables;
refusals are typed (level, spelling, help) and shown by each surface; the gate decides, it does
not kill a running process when the level drops. Residuals: only the first shell token is
inspected (compound/quoted/variable-built commands not parsed), Windows PATH resolution has no
PATHEXT and no drive-letter-case normalisation, an environment injected below the three seams
would not be resolved, no kill on demote; the remote path keeps the coarse app rule because a
remote trust store is deferred. **i18n extraction 2 (#22b).** `i18n/locales/*.json` now carries
the palette command titles, menu bar/dropdown labels, a first settings-label subset (Interface/
Display/Terminal labels + the two Fresh-named rows) and the remote-authority status, about 400
English keys (en.json 416 keys; embedded catalogues total 294,448 bytes of JSON, ~194 KiB of
key+value text, 256 KiB cap test); the generators emit Fresh `cmd.*`/`menu.*` keys so identity
keys import Fresh translations, `fresh_map.json` maps differing keys, and gvim-only `gvim.*` keys
stay English until a translation pass; `i18n_check.sh`/`i18n_import.py --check` keep the
catalogues valid and deterministic. Remaining English boundary (open): prompt/dialog titles and
buttons, palette command descriptions, settings values and most section/row labels, keybinding
help text, plugin data, and the other owners' new strings. **Classification correction.** Per
the Lead's TUI-only scope decision, non-TUI surfaces (GUI/desktop, browser --web, WASM,
packaging, telemetry, self-update) are objective boundaries (§4/§8), while the update-check/
{update} TUI indicator (now landed partially, R13-12) and the remaining platform edges (GPM
console mouse, AltGr/CapsLock/key-release, plugin focus events) stay OPEN TUI gaps; all previously reopened TUI work (composite
API, orchestrator, live_sessions, agent_* ports, devcontainer/k8s, pkg, audit_mode, welcome_
screen, language helpers, shared buffers, URL themes, kitty/OSC52 read/reflow, universal server,
Windows ConPTY verification, etc.) remains open with concrete gaps, not substituted by size.
**Review findings and their fixes.** The audit's two correctness findings were fixed after the
audit and re-verified with targeted tests (no full suite): (1) **Close ordering/capture-vs-exit** —
`workspaceManager.Close` now takes the snapshot/queues the capture on the UI goroutine, waits
`job.done` before closing anything, and only then runs the synchronous fallback save;
`TestCloseAwaitsCaptureWriteBeforeExit` (blocked writer: no sync save/return before the queued
write finishes) and `TestCloseFailureReportedAndFallbackSaves` pin it. (2) **Close-during-
materialization** — closing a workspace whose switch is pending bumps the sequence, cancels the
build and clears `pendingID`, so `applyCommit` drops the stale build and the registry file stays
removed; `TestCloseDuringMaterializationDoesNotResurrect` deliberately ignores context cancel and
proves no resurrection/materialization/re-registration and no active-switch landing. (3) **Close
failure propagation/retention** — a failed capture write now attempts the synchronous fallback
save before closing; if that cannot be confirmed in either path, the workspace, runtime and
registry file are retained and the error is returned/reported (no silent drop/removal).
`TestCloseRetainsWorkspaceWhenBothWritesFail` proves both writes failing ⇒ error naming both
failures, workspace/runtime/entries retained, no session file, and a retried writable close
succeeding while keeping the unsaved text. The remaining open findings are unchanged: the
open-file single-writer rule scans only materialized sessions (a file open in a dormant layout
can be re-opened in the active workspace — open gap, #19b/R13-1), the capture snapshot→write
window loses the last capture on a crash (previous session intact), and real-host paths are
untested here (SSH remote editing/LSP, tmux discovery, Windows ConPTY; the full suite was run by
the Lead, not this audit).

**Phase-2 integration wave (2026-11-09, on `ecd27e6` + uncommitted work).** Eight tracks
landed; this audit verified them read-only per the merged working tree and re-ranked the gaps:
**Workspace open-file routing + dormant single-writer.** The daemon's open-file frame now carries
the client's cwd and either the legacy bare `paths` or the positional `files` (path + 1-based
line/column); a current client sends both for compatibility. Relative paths resolve against the
client's cwd, a materialized owner is switched to, a path open in a **dormant** workspace is
found from the registry layout (falling back to its session file; a stale entry whose file is
gone and has no stash is ignored), that workspace is materialized asynchronously and the
request position is applied (a saved position is used when the request has none, stash text
restores the file). A detected owner that fails to materialize is an error that keeps the
current workspace — the path is never re-opened elsewhere, which closes the earlier dormant
single-writer hole. Tests (all green, target runs): `TestOpenFileFromAnotherClientCwd` (now
passes; the earlier mid-landing failure was the debug/incomplete state),
`TestOpenFileDormantOwnerMaterializesAndSwitches`, `TestOpenFileDormantStashIsRestored`,
`TestOpenFileStaleDormantLayoutIgnored`, `TestOpenFileDormantOwnerMaterializeFailureKeepsCurrentState`,
`TestOpenTargetsFromRequestResolvesCwd`, `TestAttachClientTargetsSendsCwdAndPositions`.
**Daemon exit durability + late-writer isolation.** `closeAllChecked` (used by the daemon exit
path) snapshots every materialized workspace, waits on ONE shared 30 s deadline, retries a
failed/timed-out capture with the runtime's synchronous save (the close-workspace fallback),
writes the crash-recovery copy when that also fails, closes runtimes only after those attempts
and returns an error so the daemon exits non-zero. A stuck capture writer is isolated by the
per-workspace `captureWriter` lock + sequence (`mu`/`seq`/`committed`/`stopped`): when the exit
fallback TryLock fails because the newest write is inside `writeSnapshot` holding the lock, the
fallback must NOT write the session (the overwrite race), the runtime is marked `skipSave` so its
own exit save cannot race it either, the error says the newest capture is still in flight and
names the recovery copy, and the daemon exits non-zero; queued older snapshots are superseded by
the commit token/sequence (the delayed-writer regression). Tests −count=3 all green:
`TestDaemonExitCheckedCleanSavesEveryWorkspace`, `...AsyncFailureIsNotSuccess`,
`...FallbackFailureKeepsRecovery`, `...TimeoutIsBoundedAndReported`,
`TestDaemonExitFallbackSupersedesQueuedOlderWrite`; the older close/capture tests also pass
−count=2. Limits recorded accurately: the in-flight newest write is left to finish; the process
does not wait for it after the deadline, so on exit the session file may remain the previous
snapshot — the crash-recovery copy is the durable last resort and the error names it; a process
killed between snapshot and write loses that last capture (previous session file intact).
**Terminal tab reordering.** `Move Tab To Left/Right` palette rows (palette 236→238),
Ctrl+Shift+PageUp/PageDown when the terminal reports modified keys, and a mouse tab drag with
Fresh's three-cell threshold, insertion marker and left/right-half drop rule; off-strip release
cancels, a reorder permutes the tab list only (no process start/resize/close; a tab keeps its
transcript, launch record and live PTY), and the saved `tabs` array order persists the order
across restart. No splits, so Fresh's drag-to-split/per-tab split moves stay open; restored
tabs remain read-only/dead.
**Remote external-change watching.** The remote file surface's polled `Watch` is bridged into
the root's existing watch pipeline (`RootPorts.Watch`/`WatchDirs`): one goroutine per directory
with its own cancellable context, events coalesced over a 150 ms quiet window off the update
goroutine, no local fsnotify/`os.Stat` fallback, generation-tagged watches so a reconnect cancels
and restarts them and a stale carrier's event is dropped, bounded retry (1 s doubling to 10 s)
while connected and waiting for reconnect when disconnected, and `Close` cancels/waits. Tree
reload and auto-revert now behave as for a local file; Fresh's metadata-poll parity is kept as
well. Tests: `TestRemoteWatch{ForwardsEvents,CoalescesBursts,CloseCancels,ReconnectRestartsAndDropsStale,
WaitsForReconnect,AddIsIdempotent,NilSlot}` and `TestSetupRemoteWatchWiresTheRootPorts` (green).
**OSC 52 read/paste (opt-in).** gvim extension `clipboard.osc52_paste`, default off; the dock
answers a child's OSC 52 read only with selector `c`/empty, only when the live workspace-trust
closure says Trusted, only with a non-empty clipboard capped at 64 KiB (empty/error/oversize ⇒
no reply so the child times out), base64-encodes the response into the session's input inbox,
runs on the terminal read goroutine (never the UI goroutine), and installs no handler for
restored/read-only transcripts or untrusted workspaces. Tests:
`TestDockOSC52Query{Gating,ReadResults,DoesNotBlockOtherWork,ConcurrentTrustChanges}` and the
terminal session hook tests (green). Fresh itself documents OSC 52 read as not implemented, so
this is opt-in extra behaviour rather than a parity item.
**i18n extraction 3.** Command descriptions (generated `cmd.*_desc`, hand-written/re-named
`gvim.command.*`), every scalar settings row and section label (`gvim.settings.field.*`/
`gvim.settings.section.*` — including the status-bar section/fields), menu/help strings and the
remaining command titles; en.json is now 706 keys (+290 from 416), all-locale key+value text is
529,809 bytes (~517 KiB) under the 640 KiB cap, embedded JSON is 612,378 bytes (~612 KB rodata),
and `TestCatalogueHeapBudget` caps the lazily loaded catalogue maps at 2 MiB (English ~50 KiB,
de ~80 KiB measured). `tools/i18n_check.sh` is green. Remaining English boundary stays open:
most inline notices, prompt/dialog text and titles/buttons, structured settings sub-page labels,
settings values, keybinding help, plugin data and page/picker inline text.
**External agent-history discovery.** Read-only bounded scan of the Claude Code and Codex CLI
transcript stores (newest headers only; 50 sessions/tool, 5001 entries, 20 problems, 256 KiB
Claude / 64 KiB Codex headers, 400 record lines; transcript bytes are never used as argv; only a
strictly validated session id may fill the fixed resume template; Codex's `resume --last` is
labelled inexact) merged with the tmux findings in the discovery picker. Tests:
internal/agentdiscover (green). No keymap-editor history-scan change exists in this tree — the
new history scanner is this agent transcript scan; the report flags that in case a separate
keymap track was intended.
**Verified/cleared items.** Explorer natural ordering is present (directories first, numeric
digit runs, case-insensitive, GNU-sort-style) and is not a gap; status-bar i18n is the extracted
status-bar settings section/field labels plus the already-landed `gvim.status.*` remote pills
(no separate status-row string extraction); the plugin debug output (`WSDBG`/`TUIDBG`) is gone
and the earlier cross-cwd open-file test is green; no duplicate functions/temp harnesses were
found. Metrics: palette 238; bundled 22 (9 on)/13 off, idle heap ~7.27 MiB; plugin API 242
functions/1478 lines, level 2; settings 69; i18n en 706 keys / 529,809 B all-locale text.

**Round-14 wave (2026-11-09, on `f8657ec` + uncommitted work, tasks 89–104).** Six tracks are
verified stable in this snapshot; LSP completion's safety increment is verified with its residual UX gaps recorded:
- **Remote finder + search protection (task-99).** The remote workspace's quick-open index now
  enumerates only `authority.Files()` (local launch keeps `StartFiles`; a remote launch starts with
  `finder.Idle()` and installs the real index after the slot connects — no local fallback), the
  palette joins rows with the remote root's split separator, and `remoteRootPorts` clears the local
  adapters (preview, project switching, git status, project search/replace, git grep). Every remote
  project-search/Git Grep/replace/Live Grep entry point is refused with a notice before the local
  port is called. `~` roots are expanded with a real remote shell: `authority.QuoteDir` now keeps
  the slash after `~` unquoted (the old `~'/proj'` form did not expand), quotes only the tail, quotes
  invalid `~user` prefixes literally, and uses `"$HOME"` for a bare `~`; a real-shell test covers
  `~/my proj` and proves a `$(...)` path is never executed. Root read errors become an index error
  and a disabled palette row; unreadable subdirectories are skipped; >20 000 remote directories
  returns the bounded error with partial rows kept; >50 000 files sets the truncation row. Tests:
  `TestRemoteFileIndex{UsesAuthorityOnly,ExpandsTildeRoot,RootFailureSurfaced,TildeRootRealShell,
  TildeInjectionNotRun}`, `TestRemoteProjectsRefusesLocalSwitch`, `TestRemoteRootPortsDisableLocalAdapters`,
  `TestRemoteProjectSearchEntryPointsRefused`, `TestLocalProjectSearchStillOpens`,
  `TestRemoteLiveGrepRefusesLocalSearch`, `TestFileRowsSurfaceErrorAndTruncation`. Residuals:
  remote git marks/preview/refresh (§4 item 99) and remote search through the authority (new gap).
- **Daemon host single-writer (task-100).** A daemon writes an atomic per-root/per-name host mark
  before serving (and refuses to start if the mark cannot be written), clears it after the checked
  exit save, and stale marks are removed when their socket is dead. While any live daemon hosts a
  workspace, ALL direct editor launches in it — bare, file, `--simple`, `--diff`, stdin — are refused
  with the daemon names, socket, and `gvim -a`/`gvim --attach=NAME` attach instructions; this covers
  explicitly named daemons too. Tests: `TestDaemonHostMarks`, `TestDaemonNamedSingleWriterRefusal`,
  `TestDaemonRefusalKeepsUnsavedWork`, `TestDaemonHostMarkFailureRefusesToStart` + existing
  daemon/CLI suites. This closes the old data-safety risk R13-10 (deliberate divergence: Fresh can
  allow a second editor because it shares per-file buffers; gvim refuses until it has that ownership).
- **Split persistence (task-102).** Session files gain an additive optional `layout` (split tree,
  focused pane, each pane's tab index/path and scroll position; format version stays 1). Capture
  reads the model without switching tabs or scrolling; restore resolves leaves by stable path over
  the restored file list, rejects stale/invalid/malformed layouts back to one pane, skips the layout
  for compare/dual views, hot-exit clean-tab restore and remote launches, and daemon workspace
  restart restores it. Tests: `TestSplitLayoutValid/Bounds/RoundTripAndLegacy`,
  `TestWriteSessionLayoutRoundTrip/DropsStaleLayout`, `TestSplitCapture/Apply*`,
  `TestSplitTabMappingResolvesStableIdentity`, `TestBuildRuntimeRestoresSplitLayout` (+ stale/hot-exit/
  malformed cases), `TestWorkspaceDaemonRestartRestoresSplits` (+ stale).
- **Buffer-word completion (task-98, no-LSP half).** `editor.BufferWordCompletions` ranks bounded
  in-memory words around the caret (512 KiB window; 32 KiB large-file constant; min 2 runes, max 40,
  `languages.<id>.word_characters`, smart case, proximity→viewport→frequency); the TUI captures the
  snapshot on the update goroutine, opens the local list immediately, then runs the server call in a
  command and merges server items first/dedupes later; replies are dropped on document-version or
  caret change and a slow server can neither block nor resurrect a dismissed list. Works for untitled
  buffers/no-server sessions; large-file documents defer words (no disk reads on Update); manual
  Trigger Completion only (`completion_popup_auto_show` not ported); only the active buffer is
  scanned. Tests: editor `TestBufferWordCompletions*` + TUI `TestBufferWordCompletion*`. **LSP
  completion safety increment (task-97) is verified as landed**: snippet expansion (tabstops/
  defaults/first choice/final stop/escapes), UTF-16→rune conversion of additional edits,
  completionItem/resolve for lazy auto-imports in a command, an atomic insert (main + additional
  edits) guarded by the captured document version and an overlap check, stale/overlap refusals
  changing nothing, and keymap's local-first buffer-word merge. Tests:
  `TestExpandSnippet`/`TestIsSnippet`, `TestUICompletionConvertsAdditionalEditsFromUTF16`,
  `TestUICompletionExpandsSnippetFormat`, `TestUICompletionResolveToken`,
  `TestCompletionAdditionalEditsApplyAsOneGuardedEdit`, `TestCompletionIsRefusedWhenTheDocumentChanged`,
  `TestCompletionRefusesOverlappingEdits`, `TestCompletionSnippetPlacesTheCaretAtTheFinalStop`,
  `TestCompletionResolveThenApplies/FailureChangesNothing` + buffer-word local-first/merge tests.
  R13-7 remains OPEN only for the residual UX gaps listed there (auto-popup/quick-suggestions,
  active-buffer-only/large-file deferral, multi-cursor
  insertion, request-id bookkeeping); the safety increment itself is no longer pending.
- **diff_nav (task-101, R13-9).** Merged git/HEAD + live_diff + saved-diff navigation is stable;
  R13-9 remains PARTIAL with large-file, cross-Compare and command/i18n residuals (details in R13-9).
- **i18n recent palette actions (task-104).** The two Move Tab rows gained `gvim.command.terminal_tab_*`
  title/description keys plus `gvim.settings.field.file_explorer.compact_directories`; en.json is now
  710 keys, all-locale key+value text 534,728 bytes (~522 KiB) under the 640 KiB cap, embedded JSON
  617,897 bytes; `tools/i18n_check.sh` and catalogue tests are green. Remaining boundary unchanged
  (inline notices, prompts/dialogs, structured sub-page labels, values, keybinding help, plugin data).
Metrics: palette 238; bundled 22 (9 on)/13 off; idle heap ~7.27 MiB; plugin API 242/1478. The audit
ran the targeted tests named above (green) and build/vet; full suite was not rerun, and the LSP
completion work is not counted as landed.

**Round-15 outcome (2026-11-09, merged working tree on `4d327dd`; tasks 105–111,113,114 + Lead fix).**
Ten tracks landed; the audit read the diff and ran targeted tests (Lead ran the full suite).
**Remote project search + Git Grep (105).** For a remote workspace, Project Search and Git Grep
now run on the remote host through the authority one-shot surface, in the session's remote
captured env delta (`env.Remote`), with the spawn/trust gate applied: Project Search probes the
remote host for `rg` then `grep` (a missing tool is the explicit no-search-tool notice, never a
local fallback), runs with the remote workspace cwd, streams whole-file batches, caps matches/
flags truncation, cancels the remote command through the panel's cancel channel, and validates
the query locally before any remote run; Git Grep resolves the repository remotely
(`git rev-parse --show-toplevel`) then runs `git grep -n --column` and returns absolute remote
paths so hits open through the remote document access. The local ports are not used. Project
Replace stays refused with a remote-specific notice. Tests:
`internal/app/remote_search_test.go` (stream/caps, file snapshot, grep fallback, missing tool,
cancellation, unsupported scope, remote git grep, not-a-repository, port installation, timeout),
plus remote project-search TUI tests. Residuals: current-file and open-buffers search scopes,
the panel preview column, and safe remote replace.
**Remote explorer (106).** A read-only lazy remote tree over `authority.Files().ReadDir`
(`internal/tui/remote_tree.go` + `internal/app/remote_explorer.go`): every listing runs in a
command (Update reads nothing), generation-tagged stale results are dropped, expansion is
kept/pruned, hidden filter and natural order match the local tree, 5000-entry/20 s bounds,
trust-gated reads, explicit disconnected notice + reload on authority reconnect, remote file
open through the existing FileAccess, and read-only refusals for new/rename/delete/cut/paste.
Tests: remote_tree_test/model, explorer/async_test, app/remote_explorer_test. Gaps: remote
write ops, drag/move, .gitignore filtering, remote git marks, directory watching (file watch
covers open documents only), directory symlink treated as a directory, request-driven refresh,
Windows clients still use the root's `filepath` assumptions.
**Extract tab + close/eviction (107).** The tab menu/palette "Extract to New Workspace" captures
the tab on the UI goroutine (path, cursor/top, page-view, virtual column, unsaved text), then in
a command canonicalizes the file's folder, refuses the current or an already-registered root
with a notice while leaving the tab open, creates the workspace/session+stash, switches and
closes the source tab; failure before the close leaves the tab/text open and reports why.
Closing a workspace with running terminal tabs asks first and lists every terminal and its
agent; the close report/protocol answer says what was stopped; a dormant close reports none.
Rename reports the new label. Tests: workspace_extract_test (unsaved, refusals),
workspace_close_terminals_unix_test, TUI workspace action/close/tab-menu tests. Remaining #19b:
co-tenant workspaces over one root, extract into an existing workspace, terminal-tab
extraction, large-file overlay extraction, extracting multiple tabs, shared buffers/per-workspace
plugin state/eviction/registry additions (multi-client/auto_attach/plugin-state landed r17–r19; eviction/registry still open).
**Completion auto-popup + multi-buffer words (108).** With `editor.completion_popup_auto_show` on
and `quick_suggestions` enabled, typing a word character schedules a debounced popup after
`quick_suggestions_delay_ms` (default 150 ms); the timer carries path/caret/document version/
generation and opens only if still newest; non-word chars, Esc/dismiss/accept, another overlay,
document/caret change, empty prefix or no source suppress it. Other open tabs contribute a
bounded 64 KiB middle window each (≤8, tab order), merged below the active buffer's words and
deduped. Reuses the local-first/stale-guarded completion path. Tests: auto-popup on/off/quick-
suggestions/cancel/stale/empty/multi-buffer/settings-live in `internal/tui/completion_words_test.go`.
Residuals: LSP trigger characters are not exposed (`suggest_on_trigger_characters` parsed but no
effect), multi-buffer uses tab order rather than Fresh MRU, dabbrev cycling not ported, large-file
documents defer buffer words, plus the earlier multi-cursor/request-id
completion gaps.
**Terminal live restore (109).** `terminal.json` gains an additive per-tab/top-level `live` flag;
restore starts nothing: a tab live at capture is dormant (no exited marker) and the first dive
(switch/focus/show) revives a fresh PTY in the saved launch dir with resume→launch→shell
precedence under `terminal.resume_agents`, keeping the bounded saved transcript above; an
exited-at-capture or old-record tab stays read-only/dead; a failed dive is one notice, not a
retry loop; `terminal.restore_live` (gvim extension, default true, user layer only) can make
all restores read-only; daemon workspaces behave the same and warm tabs stay warm. Tests:
dock/live_restore_test (dive/resume/restore_live/failure), tabs/agent restore tests. Residuals:
Fresh revives on focus from a persisted backing file; gvim needs an explicit dive (dock is not a
restored editor split) and keeps bounded transcript text rather than a backing file/screen;
remote launches still restore no layout/terminal state.
**Input platform edges (110).** `chrome_focus_changed` (editor/explorer/dock/section; tabs is a
gvim extension; no menu/overlay/initial event; native sections report region=section) and
`key_release` (same shape as key capture; only backends reporting `tea.KeyReleaseMsg`, e.g. the
Windows console; legacy terminals silent, no kitty event flags requested/faked) are exposed to
plugins. AltGr-as-Ctrl+Alt text input, CapsLock/lock-bit ignoring, and Shift inference for
uppercase-without-Shift are normalized where the decoder preserves information. Tests:
internal/tui/input_edges_test.go. Residuals: no legacy key-release reporting, no kitty protocol,
GPM console mouse, terminal-window focus reporting only for remote reconnect, and platform
backend limits documented in docs/keybindings.md.
**i18n remainder (111).** en is 808 keys, every non-English locale 551 keys; all-locale key+value
text is 534 KiB, embedded JSON about 616 KiB; catalogue heap en 62 KiB / de 81 KiB / both ≈144 KiB
(`i18n_check.sh` green). Extracted: completion settings rows, dialog/prompt labels and buttons,
common-operation notices, explorer/compare/mouse toggles, structured settings page titles/add/
notes and Languages parts, the update indicator (`gvim.status.update_available`), and Fresh-key
maps; generated tables remain deterministic. Still English/inventory: most pick/browser/plugin/
theme-editor/settings-operation notices, LSP picker details, picker/list/browser chrome and
context menus, prompt hints/validation beyond go-to-line, settings values and structured
page field/row schema names, keybinding help, rows without a Fresh key; `terminal.restore_live`
is config-only (no Settings row/key); Fresh's self-update commands have no keys (not implemented).
**SSH keepalive/auth (113).** The carrier now sends keepalive pings (a server that stops
answering flips the slot disconnected with a keepalive reason; reconnect restarts it), retries
in the background with capped backoff/one dial in flight, prompts for a key passphrase
(`ErrNeedsPassphrase`/`ReconnectWithPassphrase`), asks each keyboard-interactive 2FA question
and replays answers in order, shows "reconnecting" in the status row, and keeps prompt input in
the existing prompt style. Tests: authority/ssh_auth_test, ssh_keepalive_test, TUI
authority_retry_test. Residuals: no ssh-agent forwarding or `ssh_config` reading (ProxyJump/
Host aliases/IdentityAgent/host-key algorithms), default `~/.ssh/known_hosts` only, secrets
kept in memory only (no keyring/agent integration), keyboard-interactive replayed one answer per
server question with no OTP retry policy, keepalive/backoff bounds are package vars not settings,
a background retry that needs credentials opens one prompt then waits for activation if
cancelled; all tests offline (no real host/2FA provider).
**Remote env provider (114).** A remote launch uses `env.Remote`: the configured markers are
probed and the activation recipe run on the REMOTE host through the authority one-shot surface
(never a local env package result, never the local overlay), diffed against the remote login
baseline; the captured delta is what the remote terminal, shell/formatter runner, remote LSP and
remote project search/git grep receive. Trust-gated and warmed in the background after connect
(spawn paths read the cached delta; the update loop never blocks on the remote host). Tests:
internal/app/remote_env_test.go (remote-only runner, end-to-end, local overlay not forwarded,
remote LSP env). Residuals: probe uses `sh -c`/remote login baseline rather than the interactive
remote login shell, env input files are not watched (Env: Reload/activate re-captures), cached
until refresh/reconnect, Settings Environment rows show remote facts only after the warm, no
status-row indicator of the active remote env (Fresh shows one).
**Update indicator (R13-12).** Partial as recorded: config check/feed policy/limits/stamp,
tea-command check, `{update}` painting; self-update/commands/update buffer/clickable/release page
remain open.
Metrics: palette 238, settings 69 in 14 sections, bundled 22 (9 on)/13 off, idle heap ~7.27 MiB,
plugin API 242 functions/1478 lines level 2. The full suite + static gates above are the final
record; no full-parity claim (real hosts untested; residuals listed in §5 R13-13…R13-18).

**Round-16 outcome (2026-11-09, merged working tree on `8882ae8`).** Six tasks (115/116/117/118/119/
121) landed plus the Lead's update command surface and the authority foundations for the next slice;
120 (remote large-file) and 123 (explorer write UI) are partial. The audit read the diff and ran
targeted tests (Lead ran the full suite and re-runs after this update).
**Remote project replace (115).** Search-and-Replace-in-Project now writes a remote workspace
through the authority instead of refusing: the confirmation names the remote target
(`SSH:user@host:/path`), each candidate is read through `authority.Files()`, the local
`search.Query`/`PlanReplace` semantics apply (case/word/regex/captures, checked matches,
`req.Limit`), and each file is written back with an atomic temp+rename preserving mode. Safety:
workspace trust gate before the first I/O; a stale search-time hash or a size/mtime change
between read and write skips the file; per-file 4 MiB, per-run 32 MiB, 512-file/20 s budgets;
cancellation between files and into every authority call; any read/write error aborts with a
precise file-naming error and the count already written. No cross-file rollback and no undo
history for direct writes (recorded residual); dirty open buffers are skipped; remote .gitignore/
large-file filtering is not in the candidate list. Tests: `remote_replace_test.go` (local decoy
untouched, stale/mtime skip, write-failure partial count, cancellation, trust refusal, bounds)
and `project_replace_remote_test.go` (remote confirmation/notice/no local edits).
**Remote data surfaces (116).** Remote explorer git letters come from `git status --porcelain=v1
-z` through the authority one-shot with the remote workspace cwd and captured env (8 s/8 MiB, 2 s
coalescing cache, invalidated on reconnect, local git never consulted); letters are A/M/D/U/R/C
with documented approximations (rename/copy marks the new path R, original unmarked; unmerged=C;
typechange=M; ignored omitted). Quick-open preview reads the selected remote file's head via
`ReadRange` (offset 0, min(requested,64 KiB)+1, honest truncated flag; bounded whole-file fallback
when a backend lacks ReadRange; disconnected is an error, never local). The finder index gained a
thread-safe in-place `Restart` (cancel/await old run, generation guard, done channel always
closed), restarted on reconnect and by File Explorer: Refresh (a full re-walk, not incremental);
palette open keeps previous rows until re-rank/reopen. Tests: remote_git/preview/finder_refresh,
palette remote. Residuals: palette rows carry no git letters (local rows do not either), the
project-search panel preview column is unwired, refresh is a full re-walk, an open palette keeps
old rows until re-rank/reopen, a preview cached before disconnect may linger, and there is no full
fake-slot buildRuntime reconnect test.
**Completion trigger chars/live re-filter/dabbrev (117).** The open servers' advertised
`completionProvider.triggerCharacters` (static + dynamic registrations, union over eligible
servers) open the popup immediately with no debounce when `suggest_on_trigger_characters` is on;
an open list live-filters on further word characters (case-insensitive label/insert prefix, no
new server request, synthetic range for server items without a text edit, dismiss+one re-ask when
the filter empties) with version/caret guards. Dabbrev cycling already existed (active-buffer
`dabbrev_expand`, M-/): Fresh's multi-buffer dabbrev is not ported (residual). Also residual:
at round 17 non-word/Backspace dismissed; Backspace re-filter landed r21; non-word close+pass-through matches Fresh (popup.rs + e2e), not a gap, and the
highlighted row returns to the first after re-filter (Fresh keeps row identity). Tests:
completion trigger/live-filter suites.
**Registry v2 + co-tenant extract (118).** Workspace files are now version 2: several workspaces
over one canonical root, stable-id identity, Fresh label precedence (`proj`, `proj (2)`, ...),
root-grouped ordering, v1 files load and migrate on save, dedup only for identical stable id, and
co-tenants get independent layouts/session directories. Extract-to-New-Workspace now moves a file
tab into a co-tenant over the SAME root (`CreateCoTenant`, unsaved stash, cursor/top/page flags);
the switch is queued FIRST and the source tab is closed only after it is accepted, so a refused
switch leaves the tab/text in place and drops the added co-tenant, while a failure after the queue
keeps the new workspace (its session holds the text) and reports exactly that; a second source
capture prevents duplicate resurrection. Large-file overlay/welcome cases still refused. Tests:
registry v2 migration/co-tenant/extract end-to-end/close-one/daemon restart.
**URL themes + packaged form (119).** A theme value may be an https URL (or loopback http) fetched
under the update-check transport policy with a size cap, existing theme parser/validation, cache
under the user config dir keyed by URL hash, last-known-good fallback offline, clear failure
notice, no fetch for builtin; `--theme` accepts the same forms and validates the policy. A
`repo#fragment` packaged-repository URL is rejected with the explicit "packaged repository themes"
message. Settings can select a URL theme fetched at this start; an uncached URL is reported, not
fetched in Update (startup-only URL fetch residual). Tests: theme/url_test (validation/round-trip,
cache hit/offline/LKG, policy). Residuals: packaged repo/marketplace/browser, startup-only fetch,
remote packs/local archives.
**i18n picker/context-menu/browser/help chrome (121).** Extracted picker/list/browser chrome
labels, context-menu rows, agent-discovery static prefixes, prompt field hints (remaining
dynamic validation is constrained by key guards), keybinding help, plugin-manager rows,
theme-editor/settings action notices where Fresh-key matching is exact; English catalogue values
byte-identical, generated tables deterministic, size/heap budgets re-measured. Final metrics: en
853 keys, every non-English locale 563; all-locale key+value 554,038 B (541 KiB), embedded JSON
641,439 B (626 KiB), catalogue heap en 58 KiB / de 81 KiB / both 140 KiB; i18n_check green.
Remaining English boundary: plugin-manager rows/notices, theme-editor/settings notices, some
agent-discovery Import detail, project-search/remote confirm text (in-flight/remote tracks),
remaining picker/list chrome/footer, prompt validation beyond go-to-line, settings values/
structured field+row schema names, keybinding help, rows without Fresh keys; `terminal.
restore_live` config-only.
**Lead update command surface + authority foundations.** The `{update}` indicator is clickable
(Update details: feed/current/latest/last-run/release page); palette gains **Check for Updates**
(immediate check, ignoring the daily debounce) and **Show Update Output** (details list) —
palette 238→240. Self-update/install/prompt and Fresh's update terminal buffer remain absent
(R13-12 residual). `authority.FileMutations` (Mkdir/Rename/Remove; local forwarding + SSH
mkdir/mv/rm helpers, optional surface) and `FileSystem.ReadRange` (local pread, SSH
tail|head window, 1 MiB cap, io.EOF short final window) landed as foundations; the remote
explorer write UI (R13-21/task 123) and windowed remote large-file mode (R13-22/task 120)
are NOT wired. Tests: `authority/mutations_test`/`filerange_test`, `update_commands_test`.
Metrics: palette 240, settings 69 in 14 sections, bundled 22 (9 on)/13 off, idle heap ~7.27 MiB,
plugin API 242 functions/1478 lines level 2. Final full suite + static gates recorded in §6/§7;
real hosts UNTESTED; no full-parity claim (residuals in §5 R13-11/R13-19…R13-23).

**Round-17 outcome (2026-11-09, merged working tree on `20bf31f`).** Seven tracks landed; the audit
read the diff and ran targeted tests (Lead ran the full suite and re-runs after this update).
**Remote explorer write operations (124).** New File, New Directory, Rename and Delete are wired
on the remote tree (context menu + palette), through `authority.Files()`/`authority.Mutations(fs)`:
empty-file atomic write after existence check, bounded `fsops.Validate` names inside the remote
root, target-exists refusal, workspace trust gate before every mutation, explicit disconnected
error, and permanent remote delete with a confirmation naming the remote target and stating no
trash. A successful write queues the existing remote reload, so Update never waits on the host.
Cut/copy/paste of subtrees, drag/move and duplicate stay refused as a tree capability; tests
remote_ops_ui/remote_explorer_ops (local decoy untouched, each success/failure, trust, disconnected,
name validation, recursive delete). Residuals: no undo/history for remote writes, check-to-write
race (FileMutations has no exclusive create/rename), remote .gitignore filtering no-op, no remote
directory watch (only open docs' dirs), Windows client paths, real-host acceptance.
**Daemon-hosted remote authority/jump box (125).** A daemon start may take a remote target
(`gvim --daemon=NAME user@host:/path`): the daemon runs locally, hosts the remote workspace over
the SSH authority, persists the canonical `authority_spec` in the registry (keyed by remote root +
spec), prompts for password/passphrase/2FA on the daemon terminal before hosting (non-interactive
start fails actionably), reconnects the same identity on restart, and `--attach` ignores the remote
target because the daemon owns SSH; dormant remote workspaces materialize lazily from the stored
spec. Tests daemon_remote (fake/in-process authority: start/reattach/restart reuse, lazy dormant,
invalid-spec safety, prompt loop, non-interactive failure). Residuals: real SSH untested; remote
tabs/stashes are NOT restored across a daemon restart (detach/reattach keeps live state);
plugin gvim.fs/gvim.proc still use the local OS port in a remote workspace; remote trust uses the
local workspace store; per-file ownership between a jump-box daemon and a direct editor is open.
**Per-workspace plugin state (126).** A daemon workspace's `gvim.state` is namespaced to
`sessions/<stable-id>/plugin-state.json` (one versioned object for all plugins), restored on
materialize, written through the existing 500 ms coalescing/atomic flush and Stop/exit flush;
1 MiB/plugin and 8 MiB/file bounds (over-bound save skipped with warning, never truncated), a
newer-version file is read-only and never overwritten, co-tenants/workspaces cannot see each
other's keys, and close flushes without leaking. Plugin enablement/settings/manager toggles stay
editor-wide. Tests workspace_plugin_state. Residual: the shared editor-wide plugin runtime plus
Fresh's plugin-window-scope chrome model/focus announcer is task 132 (still pending/not done).
**Remote large-file windowed mode (127; read-only at round 17 — superseded by round 18 editable overlay + bounded save, 134).** `buffer.WindowSource`/`ErrSourcePending` gives a
cache-only `ReadAt` that records a pending window instead of blocking; the root loads it in a
command and repaints. Remote files at/above the large threshold open read-only in the existing
large-file mode through `document/remote_large.go`: 1 MiB aligned windows, 8-window LRU (~8 MiB),
one-window-ahead prefetch, 25 ms demand-driven loads, close/context cancellation, a 4 KiB head
indexed at open, and a status line saying remote large file is read-only and loads as you scroll (round 17; round 18 supersedes this with the editable overlay/bounded save, 134);
non-UTF-8 fails clearly rather than loading whole. Tests buffer/document/app remote_large. Residuals:
read-only (remote overlay save/edit later), syntax highlight off like local large files, only
visible window bytes in memory, real-host large-file acceptance untested.
**SSH config, ProxyJump and agent forwarding (128).** Bounded `~/.ssh/config` parser (Host
patterns/negation, HostName/User/Port/IdentityFile, ProxyJump, IdentityAgent, token expansion;
256 KiB/4096-byte line/256 Host-block caps; malformed→clear error), one-hop ProxyJump through the
same client, agent forwarding via x/crypto/ssh for terminal/one-shot channels, target overrides
config, no-config unchanged. Tests config table/tokens/overrides/one-hop jump/agent forwarding
against in-process servers. Residuals: known_hosts default `~/.ssh/known_hosts` only and no
accept-new, no host-key algorithm config, ProxyJump one hop, no keyring/agent secret storage,
keyboard-interactive OTP retry policy unchanged, keepalive/backoff package vars not settings,
real SSH/agent untested.
**i18n remainder 2 (129).** Extracted plugin-manager rows/notices, theme-editor/settings action
notices, agent-discovery chrome/dynamic notes, LSP picker details, remote search/replace confirm
text, remaining palette chrome/footers and prompt validation, with English byte-identical and key
guards. Metrics: en 909 keys, every non-English locale 563; all-locale key+value 557,917 B
(545 KiB), embedded JSON 645,880 B (631 KiB), catalogue heap en 103 KiB / de 81 KiB / both
185 KiB; `i18n_check.sh` green. Residual: docs/i18n.md's "what stays English" inventory was not
refreshed in this slice and is stale for the extracted items; generated tables remain
deterministic; rows without Fresh keys stay English by fallback.
**Live-restore styled screen snapshot + dock modes (131).** The terminal record (schema v1)
gains an additive bounded `screen` object: visible screen width/rows of cells with rune, fg/bg
`#rrggbb`, bold/underline/reverse; ≤200 rows × 512 cols, trailing blanks trimmed, control chars
→ spaces, invalid colours dropped on load. Restore overlays it on the saved text tail only when
row texts match exactly; mismatched/corrupt screen ignored whole; old records keep the plain
transcript; revived tabs paint the styled saved screen above new shell output; tab identity/order/
revive-once/`restore_live` unchanged. Dock modes: restored/exited tabs open read-only scrollback
(`Scrollback (restored)`/frozen screen), Ctrl+Space toggles pager/live when a shell is attached,
first dive revives into live mode. Tests dock screen_snapshot/live_restore. Residuals: no backing-
file bytes, only visible screen styled (older history text-only), cursor/alternate-screen/OSC/
hyperlinks not restored, revive still needs a dive, remote launches restore no state; shared
buffers/extract remain #19b.
Metrics: palette 240, settings 69 in 14 sections, bundled 22 (9 on)/13 off, idle heap ~7.27 MiB,
plugin API 242 functions/1478 lines level 2. Final full suite + static gates recorded in §6/§7;
real hosts UNTESTED; no full-parity claim (residuals in §5 R13-1…R13-24).

**Round-18 outcome (2026-11-09, merged working tree on `03bdf29`).** Seven tasks (133–139) landed;
132 was merged into 133. The audit read the diff and ran targeted tests (Lead ran the full suite
and re-runs after this update).
**Shared daemon plugin runtime + host/OS mux (133).** One `plugin.Runtime` now serves every LOCAL
daemon workspace (`pluginhost.Daemon`): later workspaces contribute only their plugin OS, editor
adapter, keymap builder and state namespace (`pluginhost.Workspace`); `pluginhost.MuxOS` routes
file/process/watch calls to the ACTIVE workspace OS (root/env/trust), and `tui.PluginShared` is a
daemon-wide host mux whose model() reads the bound active model, so plugin UI/buffer calls route
to the active workspace. A switch rebinds host+OS and calls `Runtime.SwitchWorkspaceState` (leave
namespace flushed as Work, entering `sessions/<id>/plugin-state.json` becomes the store);
editor-wide registrations (commands, menus, keys/modes/contexts, status items) are snapshotted
from the leaving model and installed into the entering one with a keymap rebuild; workspace-scoped
chrome (widget panels/sidebar sections/prompt/picker/popup overlays/per-buffer decorations/folds/
groups/view state) stays with its model, matching Fresh's default window scope. Closing a workspace
does not stop the shared runtime; the daemon stops it once after `closeAll` (flushes active state,
shuts down registered OSes). Direct launches keep one runtime byte-identically. Remote/authority-
spec workspaces are EXCLUDED and keep their own runtime. Tests plugin_shared/shared. Residual:
full Fresh window-scope chrome/focus announcer per registration and retargeting plugin UI waits
across workspaces remains open; remote workspaces not shared (R13-24).
**Remote large-file overlay editing + windowed save (134).** Remote large files are no longer read-
only: they use the existing large-file overlay (bounded) over the untouched windowed base; the
status row says windowed/editable and that save writes the merged file in one atomic write up to
64 MiB; opening still indexes only a 4 KiB head and the existing large-index loop fills the rest.
Save plans through the existing Save/SaveAs path and runs in a tea.Cmd: remote stat/CheckDisk first
(unchanged file-change error), merge overlay over untouched head/tail via Snapshot.WriteTo into a
bounded buffer (remoteLargeSaveMax 64 MiB; over the bound refuses before writing), then one atomic
whole-file authority write; after success the command opens a fresh windowed source and
ApplySavePlan rebases without a remote read on Update. Undo/dirty/rebase behave like local large
files. Divergence: Fresh applies a patched recipe on the host and can save larger than memory;
gvim's bounded merge-and-atomic-write is the fitting slice (later slice for larger files).
Tests remote_large/save. Residual: >64 MiB saves, Fresh host-side patched recipe, large-file syntax
highlight off, real-host acceptance (R13-22).
**Remote workspace session restore across daemon restart (135).** A daemon-hosted remote workspace
captures its session (tabs/open files/unsaved stashes/cursor/layout) into the stable-id session dir
on exit; on restart, after the stored `authority_spec` reconnects, it reopens files through the
remote document access and restores tabs/active/cursor/pane layout/explorer/sidebar state/unsaved
text; a vanished file keeps its stash and gets the normal session notice. A failed reconnect
restores nothing and skips capture via `keepSavedSession` so the saved session/stashes survive for
the next start; detach/reattach keeps live daemon state; direct remote launches still keep no
session; co-tenants isolated. Tests remote_session_restore (fake authority). Residual: no remote
conflict detection beyond normal stash/mtime, restored terminal transcript dead/no process re-run,
plugin fs/proc still local OS, local trust store, ownership (R13-19).
**Remote .gitignore + directory watching (136; gvim extension).** The remote explorer and finder
now apply the same go-git ignore parser/matcher as the local side: remote workspace root + every
ancestor/nested .gitignore read through `authority.Files()` (never local), bounded 64 levels,
64 KiB/file, 4096 accumulated rules; ignored rows hidden by default and revealed by the existing
show-gitignored toggle; the finder filters through the same batch (one index lifetime). Remote
directory watching reuses the polled authority Watch bridge: the loader adds listed dirs to
`RootPorts.WatchDirs`, so external changes refresh through the normal debounce/coalescing path off
Update, cancelled on disconnect and restarted on reconnect; an unsupported backend gets one
"watch not supported; refresh manually (Ctrl+R)" notice and degrades to manual refresh. Gvim
extension: Fresh's remote explorer does not expose this local-style ignore/watch behavior; not a
Fresh parity gap. Residual: poll latency/rename-as-remove+create approximation, ignore edits reach
the explorer on next reload and the finder on next index restart, Windows client paths (R13-13).
**Extract variants (137; gvim extensions).** Fresh has only "extract to new workspace"; gvim adds
bounded extensions: (a) extract into an existing workspace picker over the same root (dormant
target merges FileState/stash into its session; materialized target opens through
restore.RestoreSession and re-captures; switch is queued only after target acceptance, then the
source tab closes; refused switch keeps source tab and says copy stays in target; failed post-close
materialization says text kept in target); (b) extract a terminal tab into an existing workspace by
moving its SAVED record (transcript/styled snapshot/launch-dir/live flag) — no cross-runtime PTY
hand-off, source shell stops after acceptance, target appends a dormant restored panel and revives
on first dive; (c) multi-select picker extracts several FILE tabs to one new co-tenant, writes all
states/stashes first, then queues switch and closes each source tab by path; a tab changed
mid-action is skipped with text still in target. Residual (extensions, not Fresh gaps): terminal
tabs not in multi-select/mixed selection unsupported, no all-tabs-of-a-file Fresh action (multi-
select substitute), no drag/drop or cross-workspace undo, large-file overlay refused; data-safety
ordering tested (R13-14).
**i18n remainder 3 + boundary refresh (138).** Settings action notices wired, agent-discovery
constants locale-live/restart boundary documented, LSP picker details, remaining palette chrome/
footers, prompt validation, settings value labels, remote explorer write strings and remote large-
file status extracted; docs/i18n.md "what stays English" inventory refreshed. Metrics: en 944
keys, every non-English locale 569; all-locale key+value 563,791 B (551 KiB), embedded JSON
652,944 B (638 KiB), catalogue heap en 105 KiB / de 82 KiB / both 187 KiB; cap 640 KiB,
`i18n_check.sh` green. Remaining English: rows without Fresh keys, structured field/row schema
names, some keybinding help/plugin-manager edges; `terminal.restore_live` config-only; self-
update commands absent (R13-5).
**Remote search scopes + preview column (139).** For a remote workspace, current-file and open-
buffers scopes search the editor's remote document snapshots (authority-loaded text, unsaved edits
included) with the same caps/batching/query validation, start no remote process and read no local
disk. The project-search panel preview column is wired for remote results: open-buffer text when
available, otherwise the file head (≤256 KiB) through the authority windowed/whole-file read with
an honest truncated marker and the panel's stale sequence guard; disconnected is an error, never a
local read. Residual: preview is head-only over 256 KiB and has no semantic/LSP spans; the bounded head IS syntax-highlighted (shared highlighter; r26/r28 implementation, r34 truncation/disconnect/no-extra-read regression tests). Remote .gitignore/candidate filtering is not applied to the search list (R13-11/R13-23).
Metrics: palette 240, settings 69 in 14 sections, bundled 22 (9 on)/13 off, idle heap ~7.27 MiB,
plugin API 242 functions/1478 lines level 2. Final full suite + static gates recorded in §6/§7;
real hosts UNTESTED; no full-parity claim (residuals in §5 R13-1…R13-24 and §8).

**Round-19 outcome (2026-11-09, merged working tree on `f691d44`).** Seven tasks (141–146, 148)
landed; the audit read the diff and ran targeted tests (Lead ran the full suite and re-runs after
this update).
**Full plugin window-scope chrome + focus announcer (141).** Chrome now has Fresh's per-mount scope
(editor/window/buffer) instead of the round-18 editor-wide-vs-model-local split: a widget panel/
sidebar mount declares its scope, editor-scoped editor-wide, window-scoped chrome stays with its
window/model, buffer-scoped per buffer. One focus announcer owns `active_window_changed` and
`focus_gained`; the paths that change focus call it and it diffs (workspace label, focused split
window id, active buffer id) so a split change reports exactly one transition; the buffer diff
emits `buffer_activated/deactivated` and seeds the announcer, and `PluginShared.Bind` seeds the
leaving model's announcement so a workspace switch forces the entering workspace's window/buffer
announcement. Widget focus remains `widget_event`. Tests plugin_shared. Residual: window/buffer ids
are per-workspace and not globally stable across a switch; a plugin caching an id must re-read it,
cross-workspace id namespacing is a follow-up (R13-24).
**Plugin gvim.fs/gvim.proc through the workspace authority (142).** In a remote or daemon-hosted
remote workspace the plugin OS is wrapped with `pluginhost.NewAuthorityOS`: file reads/writes/
stat/readdir/mkdir/remove/rename/watch and one-shot/stdio processes go through the workspace slot;
the authority trust gate, cwd and captured remote env apply, and gvim never falls back to the local
OS. LookPath/Dirs/Getenv/Now stay documented local-machine APIs; copy/realpath/walk and mutations
the backend lacks return clear unsupported errors; remote fs watch is the polling watch with a
startup degradation notice. Local workspaces keep the current OS byte-identically. Tests use a
fake authority with a local decoy. Residual: real SSH untested; local-only API semantics are a
deliberate boundary (R13-19/R13-3).
**Remote hot-exit/recovery, trust record and reconnect resilience (143).** A direct remote launch
stages unsaved remote-document text locally under `<cache>/gvim/remote-sessions/<hash>/session`
keyed by remote target (a daemon-hosted remote workspace uses its workspace session dir), with
remote paths recorded; recovery installs after the remote FileAccess exists, reopens those paths
through the authority and reports the remote target; clean exit clears staging. Remote trust is
recorded in the atomic `trust.json` via `trust.RecordKey` (`user@host[:port]:workspace root`) with
LevelForRecord/SetRecord/OpenRecord, so a decision survives restart and reconnect does not
re-prompt; local keys unchanged. Successful reconnect refreshes caches/work (watch Reconnected,
finder Restart, git cache Invalidate, explorer reload/open retry, LSP restart in a command) and
shows one notice with restart count/failure appended. Residual: local-staging is a deliberate
divergence from Fresh's authority-routed recovery; remote executable-content marker scanning
needs an early connection and is not wired (undecided remote opens markerless and records that
level); real SSH UNTESTED (R13-3).
**Per-workspace project plugin config + cross-workspace plugin UI waits (144).** On workspace
activate the runtime compares the workspace PROJECT `plugins.<name>.enabled/settings` layer with
its current layer and calls `Runtime.ApplyWorkspacePluginConfig`: only plugins whose enablement or
settings changed are unloaded/loaded/reloaded (with dependants), reads/compiles are Work, the
entering model gets one plugin pump, and a project layer never writes the editor-wide
`plugin-state.json` toggles. Cross-workspace waits: `PluginShared` keeps token→owning-model
registry; a wait opened in workspace A stays registered to A across a switch to B, B cannot reuse
the token, it resumes/answers in A when shown again, and closing A resolves every owned wait as
`cancelled` with a notice. Remote/authority workspaces keep their own runtime; queued work resumes
when a local workspace is active. Residual: hard kill immediately after a switch can lose the last
debounce window; remote/authority runtimes not shared; real-host untested.
**i18n remainder 4 (145).** Settings action notices keys wired; round-18 boundary items (remote
large-file status, prompt validation, settings value labels, leftover palette chrome/picker
footers, deliberately-English constants and gvim-only keys) reviewed/extracted where keyed; docs
i18n inventory/budgets refreshed (cap 640 KiB). Metrics: en 972 keys, every non-English locale
569; all-locale key+value 565,760 B (553 KiB), embedded JSON 655,193 B (≈640 KiB), catalogue heap
en 112 KiB / de 82 KiB / both 195 KiB; i18n_check green. Remaining English boundary is documented
in docs/i18n.md (rows without Fresh keys, schema names, some keybinding help/plugin-manager edges);
restore_live is config-only; self-update commands absent (R13-5).
**Daemon multi-client + auto-attach (146).** Several attach clients share one session: server
frames broadcast to all, input from any client reaches the program, the smallest attached client's
size wins (repaint on attach/resize), detach/drop removes only that client and the session keeps
running, a slow client is detached after one bounded write, and an unknown frame drops only that
client. Hello carries ProtocolVersion=1; an incompatible client gets a dedicated rejection frame
and exits nonzero before any state change. Auto-attach: user-level `auto_attach` (top-level bool,
default OFF; Fresh defaults on) makes a bare launch with no files/direct flags join a daemon that
already hosts the same workspace, prints a notice naming it, never starts a daemon, never applies
to remote-target launches (explicit -a required), and keeps the per-root named-daemon ambiguity
refusal. Tests daemon protocol/decision. Residual: gvim keeps per-root named daemons vs Fresh's
one-shared-daemon/"any flag is direct" nuances; auto_attach default differs Fresh by design; no
fork/daemonize; Windows named pipes later; eviction/workspace CLI remain open (R13-1).
**Remote explorer clipboard (148).** Cut, copy, paste and duplicate now run over the authority:
copy walks the subtree through ReadDir/ReadFile (hidden included) and writes directories with
Mutations.Mkdir and files with `WriteFile(Atomic: true)`, binary-safe; bounds 10 000 entries,
256 MiB, 64 directory levels, 2-minute context; cut is copy-then-remove with source removal only
after full copy success; paste into another directory refuses an existing name (no overwrite
prompt), same-directory duplicate uses Fresh's free-name rule; symlinks, the workspace root,
outside-root paths, destination-inside-source and over-bound cases are refused before creating
anything. Every action confirms first on a remote workspace naming the remote target, runs the
trust gate, returns the disconnected error, and refreshes the tree after success. Cancellation/
failure in the write phase keeps the partial destination and reports how many files/wrote where
(no rollback, no remote deletion). Tests fake authority + local decoy. Residual: no drag/move
between directories, no undo/history, no whole-subtree transaction/cleanup UI for a partial copy,
FileEntry/Mode/mtimes not restored (DirEntry has no mode), hardlinks/xattrs not preserved,
copy/rename existence check still races the write (no exclusive create), no overwrite prompt,
real-host manual acceptance unautomated (R13-13/R13-21).
Metrics: palette 240, settings 69 in 14 sections, bundled 22 (9 on)/13 off, idle heap ~7.27 MiB,
plugin API 242 functions/1478 lines level 2. Final full suite + static gates recorded in §6/§7;
real hosts UNTESTED; no full-parity claim (residuals in §5 R13-1…R13-24 and §8).

**Round-20 outcome (2026-11-09, merged working tree on `7b33521`).** Seven tasks (149–154, 156)
landed. Static gates ALL GREEN; final `go test ./... -p 1 -count=1 -timeout 1800s` EXIT 0, 76
packages ok, no FAIL (log `/tmp/gvim-r20-fixed.log`). The earlier failures had two root causes,
both fixed with deterministic regressions: the daemon called Program.Send while holding a lock
(three-way deadlock with the renderer; send moved outside the lock) and the test helper waiting
for pong discarded output frames (retained frames). The audit read the diff and ran targeted
tests; the Lead ran the full suite.

**Editor-global plugin window/buffer ids (149).** The shared daemon plugin runtime now exposes an
editor-global id namespace: a model gets a per-mux sequence on Bind (direct launches stay
sequence 0) and the API returns/exposes `seq<<40|local`; every host entry point decodes and
refuses a foreign or released id with `CodeClosed`, event payloads carry global ids, and
`PluginShared.ReleaseWorkspace` frees a closed/evicted sequence so a rematerialized workspace gets
a fresh one. Tests cover colliding local ids across two workspaces, cached id rejected after
close, global event ids, updated panel/event tests. Residual: the 40 low bits are a hard cap
(far beyond real bounds); editor-scoped overlays (prompts/pickers/popups) remain model-local;
editor-scoped widget instance state is rebuilt on entry, not cloned; a buffer-bearing mount
cannot be editor-scoped across workspaces (buffer id workspace-local, carry skips it).
**Streamed remote large-file save above the 64 MiB merge bound (150).** The file port gained an
optional `WriteStreamer`/`WriteStream`: one `mktemp` in the resolved target directory (symlinks
bounded 16 levels), each Write appends a bounded chunk through one helper, Commit applies the mode
and one `mv -f` over the target, Abort removes the temp; target untouched until commit and local
backend has the same temp+rename path. Only atomic/non-appending/no-parents writes accepted
(otherwise clear unsupported error). Remote large-file save walks the windowed source in 1 MiB
windows, applies the overlay, streams merged bytes with bounded memory; a failure/cancel aborts
(temp removed, target untouched), changed remote refuses before opening the stream, and an access
without streaming keeps the old 64 MiB merge-and-whole-file fallback. Tests verify multi-window
hash identity/failure/cancel/changed/fallback. Residual: Fresh's server-side patched recipe
(unchanged parts copied on host) remains later; whole-file transport cap unchanged for files
loaded without windowing; large-file syntax highlight off; real host UNTESTED (R13-22).
**Cross-workspace shared buffers (#19b bounded slice, 151).** Daemon LOCAL workspaces share one
`editor.SharedBuffers` registry; a file has one canonical owner workspace and read-only mirror
documents in other materialized workspaces. Opening an already-open path in the focused workspace
gives a read-only mirror and requests ownership; a non-focused owner leaves the mirror read-only
until focus. Ownership moves the canonical document (text/undo/unsaved/disk baseline) and leaves a
fresh mirror; owner edits reach mirrors through version-guarded buffer changes or full reload;
close/evict DropEditor promotes a mirror so unsaved state survives. Direct and remote/authority
workspaces keep independent buffers (nil registry); large/virtual/>8 MiB fall back to independent
opens. Residuals: only the OpenFile path starts sharing (OpenLoaded/startup restore opens an
independent copy; a daemon open-file request for an owned path switches to the owner), tab close
does not promote a mirror, remote workspaces independent, mirror cursor is per workspace unless
ownership moves, mirror plugin seams see a new document id after handoff.
**Materialized-workspace cap + LRU eviction (152).** User-only `daemon.materialized_workspaces`
(default 8, range 1–64; project config refused) caps warm LOCAL daemon workspaces. After a switch
materializes one, an eviction pass (a command, never UI I/O) captures the LRU non-active local
workspace's session through the ordered writer, falls back to the synchronous save if that fails,
then tears down the runtime (plugin waits/state, terminal, documents, LSP/dock); registry/session/
plugin-state stay for rematerialize. Active is never evicted; if capture and fallback both fail the
workspace is kept over the cap and reported, then the next candidate is tried; concurrent
select/close refuses during eviction and exit save serializes. A status notice names the evicted
workspace/cap. Remote/authority workspaces are excluded from the cap (own SSH runtime, documented
residual). Tests LRU/active/unsaved/fallback/reopen/plugin-state/config.
**SSH known_hosts policy, host-key algorithms and keyring (153).** User-config top-level `ssh` keys
(project refused): `known_hosts_policy` strict (default; refuses missing and never writes),
accept-new (writes a first-seen normalized line 0600, parent 0700, notices via
Authority.TakeNotices), off (parsed only to be refused — host-key verification cannot be
disabled); a changed key always refused under every policy. `host_key_algorithms` is an ordered
safe default list (ed25519/ecdsa/rsa-sha2; ssh-rsa/DSA never preferred), unknown/empty errors
before dial, duplicates keep first, applied to ProxyJump and target, verified key type shown in
connection detail. `ssh.keyring` default false; when true, PasswordStore is consulted before the
prompt and an accepted user password is stored with notice; store errors say accepted-but-not-
stored. Residuals: macOS-keychain/Linux-secret-service backends are follow-ups (SystemPasswordStore
no-op today), 2FA/keyboard-interactive answers are not stored, the negotiated signature algorithm
is not separately exposed from the presented key type, real hosts UNTESTED (R13-17).
**i18n remainder 5 (154).** Extracted queued authority/remote strings: SSH reconnect notice/refresh
failure suffix, language-server restart count plural, remote workspace restore notice, remote
trust display, workspace trust warning, plugin focus/scope notices, remote explorer clipboard/
recovery notices, remaining settings/status. Plural TN forms wired, docs inventory/budgets
refreshed. Metrics: en 998 keys, every non-English 575; all-locale key+value 573,676 B (560 KiB;
under the 640 KiB text cap), embedded JSON 664,221 B (≈649 KiB), catalogue heap en 114 KiB / de
82 KiB / both 197 KiB; i18n_check green. Remaining English boundary is documented (rows without
Fresh keys, structured schema names, some keybinding help/plugin-manager edges); restore_live
config-only; self-update commands absent (R13-5).
**Live-restore backing-file remainder (156).** The terminal record's styled `screen` snapshot now
carries a bounded 200-row/512 KiB styled history window, cursor row/col/visibility/shape, and an
alternate-screen flag (visible screen ≤200×512; history trimmed oldest-first, control chars→spaces,
invalid colours dropped); old records unchanged. Restore applies only on an exact match of every
visible/history row text, paints history/visible styles and a reversed cursor cell in the read-only
view; first dive hands the live cursor/new shell screen over; a non-revived tab keeps the full
snapshot on re-save (re-wrap drops metadata). Tests cursor/alt/history/bounds/corruption/old-
migration/daemon. Residuals: scrollback is bounded styled window, not byte-exact (deep history
text-only); OSC sequences (title/cwd/clipboard/hyperlinks) not replayed; per-cell hyperlinks not
representable; cursor shape recorded but read-only view paints block; alternate-screen content
recorded as flag/marker not replayed; revive needs explicit dive; remote launches restore no state
(R13-15).
Metrics: palette 240, settings 69 in 14 sections, bundled 22 (9 on)/13 off, idle heap ~7.27 MiB,
plugin API 242 functions/1478 lines level 2. Static gates passed; full suite EXIT 0 (76 packages, log /tmp/gvim-r20-fixed.log) after fixing
the daemon lock-held Program.Send deadlock and the test helper's pong frame drop. Real hosts UNTESTED; no full-parity claim (residuals in §5 R13-1…R13-24 and §8).

**Round-21 outcome (2026-11-09, merged working tree on `8406f7e`).** Four tasks (157–160) landed;
the audit read the diff and ran targeted tests (Lead ran the full suite).
**Exclusive remote create (157).** The file port gained the optional `FileCreator` surface
(`CreateNew`, reached with `authority.Creators(fs)`). Local uses`O_WRONLY|O_CREATE|O_EXCL`; SSH uses
one POSIX noclobber redirection (`set -C; : > "$p"`) so anything already at the path — file,
directory or dangling symlink — reports `fs.ErrExist` and is never followed, truncated or
replaced. An explicit Mode is chmod'ed after the create; a chmod failure reports an error but does
not delete the path (another process may have replaced it). Append/Parents are refused
`ErrUnsupportedFileIO`; a backend without the surface makes the caller refuse explicitly instead
of racing Stat+WriteFile. The remote explorer New File now uses this exclusively; a concurrent
create gets exactly one success and one ErrExist, and a dangling symlink is preserved. Fresh
comparison: Fresh's local create_new_file is O_EXCL but its remote authority returns Unsupported;
gvim's helper channel provides the remote guarantee. Residual: rename/copy still use a
pre-mutation existence check and can race an intervening create (no exclusive rename/copy in the
mutation surface) (R13-21).
**Shared-buffer close behavior (158).** Closing now follows Fresh's view-vs-buffer rule: closing a
mirror tab detaches the mirror without the unsaved prompt (the owner keeps the text); closing an
owner tab while a mirror exists promotes the canonical document to that mirror first (unsaved
text and undo history preserved) and still asks nothing; only the final view (no mirror left)
keeps the ordinary modified save/discard/cancel prompt, and a forced close then removes the
registry entry. `CloseByPath` (deleted file/folder) detaches all shared victims up front, so a
promoted owner leaves no lost text and a refused transfer leaves tabs untouched. Disabled close
prompts are skipped because the buffer survives elsewhere, not because text is discarded. Tests:
owner-tab promotion, mirror-tab no-prompt, last-view prompt, CloseByPath promotion, undo/unsaved
back-and-forth. Residuals: only the OpenFile path starts sharing (OpenLoaded/startup independent,
daemon open-file request switches to owner); editor-level Close used by an exiting runtime does
not promote (daemon close/evict calls DropEditor first); the two workspaces do not become two TUI
splits/panes; view position shared only on ownership; large/virtual/remote independent; mirror
plugin seams see a new document id after handoff (R13-1).
**Remote plugin walk/copy (159).** In a remote/daemon-hosted remote workspace, `gvim.fs.walk` and
`gvim.fs.copy` now run over the authority: walk is depth-first lexical with the local hidden/
skip-dirs/max-depth/max-entries semantics, reports symlinks but never follows them, skips
unreadable subdirectories, and cancellation returns cancelled; copy handles one regular file or a
directory tree remote-to-remote with whole-file (16 MiB/file) and entry caps, refusing symbolic
links in a tree (no remote readlink; silently dropping them would be wrong), and cancellation
returns cancelled. Tests fake-authority walk/copy/tree/symlink-refusal/cancellation. Residuals:
`gvim.fs.realpath` resolves through the authority's RealPather (tool/POSIX-remote dependency; real-host UNTESTED) (a cleaned path would be wrong for a symlink without
remote readlink); remote copy does not preserve directory modes/mtimes/other metadata; a cancelled/
failed tree copy can leave a partial destination (as an interrupted local copy can); gvim.dirs/
env.get/lookpath/clock remain local-machine APIs; remote fs.watch is the polling watch with a
startup notice (R13-19).
**Completion Backspace re-filter (160).** Backspace while the popup is open deletes one character
through the normal editor action and re-filters the COMPLETE stored candidate pool (server items
plus buffer words, not just visible rows) by the shorter prefix, without asking the server; a
candidate hidden by a longer prefix reappears when Backspace widens the word. The synthetic edit
range is recomputed for the shorter word, the highlighted row keeps the same candidate identity
across typing and Backspace (labels can repeat across auto-import candidates), falling back to the
first row only when the candidate is filtered out; typing that empties the filter dismisses and
asks once for the new prefix, Backspace-empty dismisses without a request (Fresh does too), and a
re-filtered list captures the post-edit document version so accepting cannot apply against older
text. Apple/atom regression added. Non-word characters close the popup and pass through, matching
Fresh (popup.rs + e2e), so this is not a gap; remaining completion residuals are MRU/dabbrev/
large-file/multi-cursor/request-id (R13-7).
Metrics: palette 240, settings 69 in 14 sections, bundled 22 (9 on)/13 off, idle heap ~7.27 MiB,
plugin API 242 functions/1478 lines level 2; i18n en 998 / 575 per locale, 560 KiB text (under
640 KiB cap), ≈649 KiB JSON, heap 114-82-197 KiB. Final full-suite + static gates recorded in
§6/§7; real hosts UNTESTED; no full-parity claim (residuals in §5 R13-1…R13-24 and §8).

**Round-22 outcome (2026-11-09, merged working tree on `04631b8`).** Three tasks landed (162–164);
165 was not implemented (rename keeps its original behavior, no regression; race remains open); 166
is this audit. Static gates pass; final full `go test ./... -p 1 -count=1 -timeout 1200s` EXIT 0:
76 packages ok, no FAIL (log `/tmp/gvim-r22-tests.log`). The audit read the diff and ran targeted
tests; the Lead ran the full suite.
**Remote plugin realpath (162).** `gvim.fs.realpath` now resolves symlinks on the workspace machine
through the authority's `RealPather` capability: the SSH backend runs one bounded `realpath` or
`readlink -f` helper over the existing carrier and returns a canonical absolute remote path; the
local machine is never consulted. It requires the remote host to have one of those tools (otherwise
a clear `io` error, never a guessed cleaned path), requires the helper's answer to be absolute
(`realpath: unexpected answer`), honours cancellation, and explicitly checks the resolved path's
existence so a missing final component or broken symlink is `not_found` (GNU `readlink -f` can
report a path that does not exist), matching local `EvalSymlinks`. File names ending in LF or CR, or
consisting only of whitespace, are preserved byte-exactly; only the helper protocol's single trailing
newline is stripped. A symlink-loop/permission error keeps the tool's message with `io`. This is a
gvim extension: Fresh's remote backend has no canonicalize. Residual: the remote host must provide
`realpath`/`readlink -f` and be a POSIX helper environment; a missing tool is a clear error, not a
fallback; real-host acceptance UNTESTED.
**Non-word/punctuation completion behavior verified (163).** Fresh's popup filters only alphanumeric
or underscore characters; every other printable key closes the popup and passes through, and the
Fresh e2e cases (space, semicolon, parentheses, equals, braces, comma, plus, dot) assert close +
insert. gvim now matches and tests this. If the non-word key is a server-advertised completion
trigger and `completion_popup_auto_show` + `suggest_on_trigger_characters` are both on, gvim inserts
it, closes the old list, then starts a FRESH completion request whose list opens with no carried-over
selection identity; with `suggest_on_trigger_characters` off it starts no request. Any other non-word
character cancels a pending debounced auto-popup. This is correctness parity, not a gap; remaining
completion residuals are MRU/dabbrev/large-file/multi-cursor/request-id (R13-7).
**Shared-buffer startup/OpenLoaded restore (164).** `OpenLoaded` for a path another materialized
workspace owns now adopts the owner's live document instead of opening an independent copy: an
unmodified restored copy becomes a read-only mirror; a restored UNSAVED copy whose text differs
from the live owner is kept as a separate untitled `[shared conflict]` tab (restored text preserved
for Save As; file path removed so it cannot overwrite the owner) while the restored workspace's
normal tab mirrors the owner; the disk file is untouched. Adoption is deferred to that workspace's
focus because the restore pipeline applies stash text after opening; a workspace materialized while
another is focused loads independently until Focus resolves it, when the restored unsaved text is
final. Daemon tests cover a dormant workspace restoring the same file with a stale stash. Residuals:
sharing starts on OpenFile and startup OpenLoaded, but not on a plugin/API-created tab for an
already-shared path; a daemon open-file request for an owned path still switches to the owner;
editor-level Close from an exiting runtime does not promote (daemon close/evict uses DropEditor);
no two TUI splits/panes; view position only on ownership; large/virtual/remote independent; mirror
plugin seams see a new doc id after handoff (R13-1).
**Remote rename race (165) NOT landed.** The attempted safe no-overwrite rename could not be
implemented without a regression, so the original rename behavior remains and no new regression was
added. The rename/copy check-to-write race stays OPEN: destination no-overwrite is only a pre-write
existence check for remote explorer copy/paste/rename/duplicate and remote plugin `gvim.fs.copy`; a
concurrent create between check and write can be overwritten. Only New File's exclusive create is
race-free (R13-21).
Metrics: palette 240, settings 69 in 14 sections, bundled 22 (9 on)/13 off, idle heap ~7.27 MiB,
plugin API 242 functions/1478 lines level 2; i18n en 998 / 575 per locale, 560 KiB text (under
640 KiB cap), ≈649 KiB JSON, heap 114-82-197 KiB. Final full-suite + static gates in §6/§7; real
hosts UNTESTED; no full-parity claim (residuals in §5 R13-1…R13-24 and §8).

**Round-23 outcome (2026-11-09, merged working tree on `615960c`).** Four tasks landed (167–170);
171 is this audit. Static gates pass; final full `go test ./... -p 1 -count=1 -timeout 1200s` EXIT
0: 76 packages ok, no FAIL (log `/tmp/gvim-r23-tests.log`). The audit read the diff and ran
targeted tests; the Lead ran the full suite.
**Completion candidate info verified (167).** Fresh renders `labelDetails` by preferring
`labelDetails.detail` (trim_start) plus `labelDetails.description` as the dimmed text next to the
bare label, falling back to the item's plain `detail` (refs/fresh
`crates/fresh-editor/src/app/popup_actions.rs`:687-715, e2e lsp.rs:10286 asserts the rendered import
path). gvim's `lsp/requests.go` `completionItem` already merges the same fields into `Detail`; tests
now assert parser + popup behavior (row label = bare identifier, dimmed labelDetails/detail,
filtering/acceptance on bare label/insert text). Fresh explicitly does **not** display completion
documentation: `create_client_capabilities` advertises `resolveSupport(documentation)` only so rust-
analyzer exposes `resolveProvider`, and the comment states the popup renders label/detail only, never
documentation (`async_handler.rs`:478-481); gvim makes the same compatibility advertisement and
likewise renders no documentation. Therefore labelDetails/docs are NOT gaps and the old residuals
are removed.
**Multi-buffer dabbrev (168).** `dabbrev_expand` (M-/) uses the same multi-buffer view as the
popup: first press collects the active buffer's earlier words nearest-first, then other open tabs'
words in TAB ORDER, from a bounded middle window per buffer (64 KiB/buffer, at most 8 tabs, 30
candidates total); repeated presses cycle through the list, the last press restores the typed
prefix, each replacement is one undo step, matching is smart-case, and a word already offered by
the active or an earlier tab is not repeated. Large/virtual other buffers are skipped; the active
large file keeps its bounded backward scan. Residual: the deliberate deviation is buffer order —
tab order, not Fresh's MRU order (same as popup multi-buffer words; gvim keeps no editor-wide MRU
list) (R13-7).
**Remote search ignore scope (169).** The grep/Live Grep fallback now uses Fresh's project-file
scope instead of whatever grep walks: in a git work tree it runs `git rev-parse --is-inside-work-
tree` then one bounded `git ls-files -z -s --cached --others --exclude-standard` and only results in
that listing enter a batch. Tracked files are kept even when they match .gitignore, tracked
dotfiles are listed as Fresh does, untracked ignored files are absent, untracked hidden entries
are dropped; nested repositories/submodules are accepted by their listed prefix (inner ignore
rules not applied by that one git call). Outside a git work tree only hidden entries are filtered.
If the probe/listing fails, times out or is truncated, the scope degrades to hidden-only filtering
(never a guessed .gitignore that could drop tracked files) without a separate notice. Bounded and
streamed like the search. Residuals: the rg path is unchanged (rg applies its own ignore/hidden
rules, so a tracked file matching .gitignore is still omitted there while Fresh/grep include it);
the tool output cap and panel Limit apply BEFORE this filter, so an ignored-heavy tree can return
fewer than Limit results; nested-repo inner ignore rules; degradation not surfaced; a finder file
snapshot, when supplied, is authoritative; the finder index itself still walks the backend rather
than using git ls-files; no local fallback (R13-11/R13-23).
**Plugin copy streaming (170).** When the authority exposes the streamed-write surface (local and
SSH backends do), `gvim.fs.copy` now copies each file in bounded 1 MiB `ReadRange` windows into an
atomic `WriteStream`: memory stays one chunk, the 16 MiB file cap is gone, the destination is
untouched until Commit (an existing target survives failure/cancellation; temp is aborted), and
cancellation is checked between chunks. A backend without the stream surface keeps the previous
whole-file fallback, whose 16 MiB `too_large` cap applies. Tree copy remains bounded to 100 000
entries; a source that shrinks during copy fails with `io` and leaves the destination untouched;
growth beyond the initial observed size is not copied (residual). Destination no-overwrite is still
only a pre-write existence check — a concurrent create between check and write can be overwritten
(no exclusive destination create), and this is not a no-overwrite guarantee. Metadata/tree partial-
destination residuals remain; real host UNTESTED (R13-19/R13-21).
Metrics: palette 240, settings 69 in 14 sections, bundled 22 (9 on)/13 off, idle heap ~7.27 MiB,
plugin API 242 functions/1478 lines level 2; i18n en 998 / 575 per locale, 560 KiB text (under
640 KiB cap), ≈649 KiB JSON, heap 114-82-197 KiB. Final full-suite + static gates in §6/§7; real
hosts UNTESTED; no full-parity claim (residuals in §5 R13-1…R13-24 and §8).

**Round-24 outcome (2026-11-09, merged working tree on `ae39c36`).** Five tasks landed (172–175,
177); 176 is this audit. Static gates pass; the final full `go test ./... -p 1 -count=1 -timeout
1200s` exited 0: 76 packages ok, no FAIL (log `/tmp/gvim-r24-tests.log`). The audit read the diff and
ran targeted tests; the Lead ran the full suite.
**Completion/dabbrev MRU order (172).** The editor now keeps tab-activation history
(`Editor.RecentTabIndices`, active first) keyed by document identity rather than tab index, so
closing/moving tabs keeps the surviving order and only drops the closed document. Popup multi-
buffer words and `dabbrev_expand` scan other tabs in Fresh's MRU order (active buffer always first);
an editor without that history falls back to tab order. The bounded window (64 KiB/buffer, ≤8
tabs, 30 candidates), cycle/prefix-restore/one-undo/smart-case/dedupe behaviour is unchanged.
Tests cover MRU after activation/close/move and the bounded dabbrev view (R13-7).
**Remote search candidate consistency (173).** Both remote search tools now share the Fresh
project-file candidate scope: in a git work tree a complete bounded `git ls-files --cached --others
--exclude-standard` listing is authoritative; grep fallback uses its ordinary recursive grep and
relies on that filter; with rg the command now runs `--no-ignore --hidden -g !.git` and is filtered
by the same candidate scope, so a tracked file matching .gitignore is searchable on BOTH tools
(rg's own ignore rules no longer omit it), while `!.git` keeps repository metadata out of the
widened scan. Outside a work tree grep filters hidden only and rg keeps its defaults. A failed/
timed-out/truncated probe/listing still degrades to hidden-only filtering (never a guessed
.gitignore that could drop tracked files) with no separate notice; panel Limit and the remote
tool output cap apply BEFORE this filter, so an ignored-heavy/widened scan can return fewer than
Limit and the widened rg scan can consume the cap before candidate-visible results; nested repos
are accepted by prefix without their inner ignore rules; a finder file snapshot is authoritative;
no local fallback (R13-11/R13-23).
**Remote plugin copy safety (174).** Tree copy now claims its destination root atomically through
the authority's `DirectoryCreator` capability (one `mkdir` without parents, no `mkdir -p` on the
destination): a directory created concurrently after the pre-check is refused as `conflict` rather
than merged into and overwritten. Current local and SSH backends implement it; a backend without
the capability keeps the previous check-then-`mkdir -p` behavior (recorded residual). Parents are
created before the claim (benign); a failure after the claim can leave the new destination partially
populated (residual). Descendant files use per-file atomic WriteStream behavior; a single-file copy
keeps local copy semantics of replacing an existing regular file (atomic temp/commit), a directory
at a file target is refused, a source that shrinks fails `io` with destination untouched, growth
beyond the size observed at start is not copied, and a backend without the stream surface keeps
the 16 MiB whole-file fallback (residual). Tree cap 100 000 entries; destination no-overwrite is
atomically guaranteed only for the tree ROOT, not for every descendant (no exclusive file create);
metadata not preserved; real host UNTESTED (R13-19/R13-21).
**Shared restore/conflict Save As fixes (175).** Deterministic overwrite fixes in editor/shared.go +
save_plan.go: Save As from a restored [shared conflict] untitled tab to a path another materialized
workspace owns is refused at plan time even with force (closing the local mirror tab no longer
bypasses it); Save As on a read-only mirror is refused; Save As to a NEW path registers it as the
saving workspace's canonical shared buffer (AfterSaveAs) so another workspace opening it shares/
requests ownership; Save As on a shared OWNER rekeys the entry (mirrors follow the renamed buffer,
old path stops being shared and reopens independently from its untouched disk file). Conflict label
persistence: optional session.FileState.label written by both session writers and restored by
RestoreSession, so eviction/rematerialize/daemon restart/repeated restore keep [shared conflict]
<name> instead of Untitled-N while text/pathless safety and owner/disk stay untouched. Tests:
conflict Save As cannot overwrite owner / Save-As-new-path becomes shared / owner rekeys views /
mirror Save As refused; session round-trip label; app conflict label survives repeated restore.
OPEN: Save As target ownership is checked only at PLAN time (a workspace can claim the path before
the async write/apply; apply keeps the copy independent and sets a status warning rather than re-
writing); a path owned by a dormant/evicted workspace is unknown to the registry until it
rematerializes (ordinary overwrite rules then, stale-session protection still guards restore);
sharing starts OpenFile/OpenLoaded but not plugin/API-created tabs; daemon open-file request switches
owner; mirrors keep separate cursor/view unless ownership moves; large/virtual/remote independent;
mirror plugin seams get a new doc id after handoff (R13-1).
**Dashboard upstream tracking (177).** The dashboard start tab now records branch upstream state —
`↑ahead ↓behind` or `no upstream` — alongside ahead/behind the default branch, working-tree counts
and origin. Fresh's weather section has since landed opt-in (r30); its other optional GitHub/network sections, third-party section API, Utility Dock placement, row mouse clicks and sessions section remain OPEN user work (not closed by no-network/
dependency/size); icon/style differences are acceptable. The rest of the dashboard (read-only
start tab, recent files, keyboard actions, `df` rows, auto-open/close, timer refresh) is a partial
native/plugin equivalent (R9-1/R13-6).
Metrics: palette 240, settings 69 in 14 sections, bundled 22 (9 on)/13 off, idle heap ~7.27 MiB,
plugin API 242 functions/1478 lines level 2; i18n en 998 / 575 per locale, 560 KiB text (under
640 KiB cap), ≈649 KiB JSON, heap 114-82-197 KiB. Full suite EXIT 0 (76 packages, log /tmp/gvim-r24-tests.log); real hosts UNTESTED; no
full-parity claim (residuals in §5 R13-1…R13-24 and §8).

**Round-45 outcome (2026-11-09, merged working tree on `d53d2cd`).** Five slices landed: native Deno
authority root selection + Fresh/plugin-toggle difference; Rust/Deno version probes through the
workspace authority; diff_nav deletion seam and real provider interaction tests; sidebar transcript
query history; finder-generation probe cache. Static gates pass; final full
`go test ./... -p 1 -count=1 -timeout 1200s` EXIT 0: 76 packages ok, no FAIL (log
`/tmp/gvim-r45-final2.log`). The audit read the diff and ran targeted tests; the Lead ran the full
suite.
**Native Deno LSP root selection (lsp; item 251).** Fresh's deno_lsp selection now lives in gvim's
native core, not only in the bundled helper plugin. A workspace root with deno.json/deno.jsonc is
probed asynchronously: the config is Stat'ed through the workspace authority and the runtime is
confirmed by one bounded (5 s, cancelable) sh -c 'command -v deno' through authority.OneShot()
under the workspace trust gate; a remote workspace never uses local disk or local PATH, and a
failure/timeout/disconnect/trust denial selects nothing. Explicit user decisions win: LSPEnabled
false, a disabled language, or an explicit lsp entry for typescript/javascript keeps the
configured server; only typescript/javascript are replaced (never tsx or another language). The
result is a per-root provider consulted only when a manager for the exact probed root is created;
deno lsp is registered with auto_start:false (never silently spawned; Start/Restart runs it), and
clearing/restoring the provider returns the configured baseline. A newer reconnect probe or a root
switch increments the generation, clears the old provider and drops an old answer; the provider
re-reads the live config for a future manager, so an explicit/disabled entry added after the probe
cannot be overridden. A manager created before the delayed answer keeps its configured server for
the session: a plain restart does not switch it, only a newly created manager/rebuilt port/relaunch
uses the provider (recorded lifecycle residual). Fresh ships deno_lsp default-enabled; gvim's
native selection runs in core regardless of the bundled helper plugin's toggle, while the helper
plugin is default_enabled:false: enabling/disabling it controls copy/check/help only and NEVER
controls native auto-selection (Fresh/gvim toggle difference recorded; explicit config is the
opt-out). Nested/per-file TS/JS roots keep their configured server; only the probed workspace root
is considered; Deno disappearing after a successful probe is not re-confirmed (residual). Tests:
exact-root/per-root provider, auto_start:false + manual start, authority-only/no local disk/PATH,
missing confirmations, generation/rebind late answers, explicit/disabled eligibility (R13-8/R13-3).
**Rust/Deno authority version probes (trust; item 252).** Rust: Check rust-analyzer and Deno: Check
Project now run rust-analyzer/deno --version through gvim.proc (authority-following, 5 s/64 KiB
bounds, trust gate) instead of declaring a binary missing from the local plugin LookPath. A failed
probe/exec failure/trust denial says it could not run on the workspace, names the local-only
LookPath limitation and never claims a remote host lacks the binary; non-zero exit reports the
first stderr line and empty version has its own status. Deno Copy Install Command always copies the
official curl installer because a local brew lookup says nothing about a remote host; help popups
list both alternatives and the native selection/opt-out rules. LookPath is never used for a
presence decision (R13-6/R13-13/R13-8).
**diff_nav deletion seam + real provider interaction (plugin; item 253).** The saved-diff source
now targets the line before the seam for a deletion (new_count == 0 => max(new_start-1, 0))
instead of new_start, matching git_gutter's merged navigation; real exports of git_gutter
{type,line,count} and live_diff {kind,newStart,newCount} plus buf:diff_saved() are exercised in
tests (including a deleted HEAD hunk and saved deletion seam), not test adapters. Missing/unloaded/
raising sources still contribute nothing; sort/dedup/wrap/set_cursor/status unchanged. Fresh/gvim
default difference and duplicate git_gutter palette rows remain documented residuals
(R13-6/R13-9).
**Sidebar transcript query history (sidebar; item 254).** A terminal tab's committed scrollback-
search queries are kept in memory, oldest first, bounded at 100 like Fresh's prompt InputHistory;
empty queries and an exact repeat of the newest are skipped. Opening the search input pre-fills and
selects the newest query, Up/Down recall older/newer entries with the live edit preserved as the
"current" entry, the first typed rune/backspace replaces the pre-filled selection, and Enter commits
(remembering), Esc restores. Losing dock focus/hiding the dock closes the input and invalidates its
pending preview answers, so no hidden input swallows keys. History is per terminal tab and
deliberately not persisted/shared across restore or tabs (Fresh persists prompt histories with the
workspace: documented residual); it never enters config/disk (R13-15).
**Finder-generation probe cache (keymap; item 255).** The literal-path probe's 16-path cache now
belongs to one file-index generation: finder Restart (manual refresh, reconnect, workspace rebuild)
bumps the generation and the palette drops the whole cache, so a path absent before the refresh is
probed again and a newly created file can appear without reopening the palette; the command also
re-checks the generation when it starts and ignores a cache filled for an older run. Negative
answers still cache within a generation (per open palette, bounded 16); external create/remove
without an index refresh can serve a stale answer until the next refresh/reopen; remote probe
remains exact Stat only, no directory enumeration (R3/R13-3).
Metrics: palette 240, settings 69 in 14 sections, bundled 30 (9 on)/21 off, 68 bundled-command
registration sites, plugin API 247 functions/1501 lines level 2; i18n en 1124 / 579 per locale, 575
KiB text (under 640 KiB cap), ≈665 KiB JSON, heap 122-82-205 KiB. Full suite EXIT 0 (76 packages,
log /tmp/gvim-r45-final2.log); real hosts UNTESTED; no full-parity claim (residuals in §5
R13-1…R13-24 and §8).

**Round-44 outcome (2026-11-09, merged working tree on `1f944ec`).** Two opt-in plugins landed:
standalone diff_nav and deno_lsp. Static gates pass; final full
`go test ./... -p 1 -count=1 -timeout 1200s` EXIT 0: 76 packages ok, no FAIL (log
`/tmp/gvim-r44-final.log`). The audit read the diff and ran targeted tests; the Lead ran the full
suite.
**diff_nav standalone opt-in port (item 249).** Fresh's plugins/diff_nav.ts is shipped as a bundled
Lua port but opt-in (default_enabled:false): Fresh ships it default-enabled, while adding it to
gvim's default set made TestBundledPluginsIdleHeap measure 8.12 MiB against the 8 MiB idle-heap gate
with 10 default plugins, so the nine pre-existing defaults stay unchanged, git_blame is untouched,
and git_gutter's own merged Next/Previous Change remains default navigation. Commands
diff_nav.next_change/prev_change with Fresh's ids/i18n collect gvim.plugins.get("git_gutter").hunks,
live_diff hunks and buf:diff_saved(); a missing/old/erroring export, unloaded plugin or absent git
contributes nothing (works saved-diff-only/live-only with git_gutter disabled); targets sort by byte
position (line fallback), dedupe by line and wrap; the jump calls set_cursor(center) and the status
row says Change n/total or Change n/total [wrapped]. No keys/settings; all host calls run in plugin
tasks/awaits (no blocking IO in Update). Residuals: Fresh default-enabled vs gvim opt-in (deliberate
idle-heap-gate difference, gate NOT raised); both enabled produce duplicate palette command rows
over the same targets; Compare-view hunks are not a cross-context source; large-file buffers only
navigate sources that export (line-order fallback, git_gutter keeps its large-file notice); live_diff
must be loaded for its hunks; real host UNTESTED (R13-9/R13-6).
**deno_lsp reduced opt-in port (item 250).** Fresh's deno_lsp.ts is shipped opt-in
(default_enabled:false; no keys/settings; 3 visible commands): Deno: Check Project (workspace root or
cwd; deno.json/deno.jsonc via gvim.fs.exists/path.join and gvim.proc.which deno; reports ready/
config-only/runtime-only/neither, missing-runtime states name install command and point at help),
Deno: Copy Install Command (brew install deno when brew is on PATH else the official curl installer;
gvim.clipboard.set + status), Deno: Language Server Help (action popup with both install commands,
the typescript/javascript lsp entries gvim needs (command deno, args lsp, auto_start false,
root_markers deno.json/deno.jsonc, initialization_options enable true), the local-LookPath caveat,
native Start/Restart LSP Server and Show LSP Status, upstream install link). Unported: automatic Deno
server selection/baseline restore, active-window root decision, disabled-language protection,
LSP-menu/disable/register seam, Fresh debug log. Residual: PATH lookup is the local machine's best-
effort (config/file checks and config follow the workspace authority), so a remote workspace's PATH
is not authoritative; real host UNTESTED (R13-6).
Metrics: palette 240, settings 69 in 14 sections, bundled 30 (9 on)/21 off, plugin API 247 functions/
1501 lines level 2; i18n en 1124 / 579 per locale, 575 KiB text (under 640 KiB cap), ≈665 KiB JSON,
heap 127-82-210 KiB. Full suite EXIT 0 (76 packages, log /tmp/gvim-r44-final.log); real hosts
UNTESTED; no full-parity claim (residuals in §5 R13-1…R13-24 and §8).

**Round-43 outcome (2026-11-09, merged working tree on `8ec6038`).** One slice landed: bundled
rust_lsp reduced port. Static gates pass; final full
`go test ./... -p 1 -count=1 -timeout 1200s` EXIT 0: 76 packages ok, no FAIL (log
`/tmp/gvim-r43-final.log`). The audit read the diff and ran targeted tests; the Lead ran the full
suite.
**rust_lsp reduced opt-in port (item 248).** New bundled Lua plugin rust_lsp (default_enabled:false;
no keys/settings; four visible palette rows): Rust: Copy rustup Install Command (clipboard "rustup
component add rust-analyzer" + status), Rust: Copy brew Install Command ("brew install rust-
analyzer"), Rust: Check rust-analyzer (gvim.proc.which LookPath then gvim.proc.run rust-analyzer
--version bounded 5 s/64 KiB; reports the path and first version line, missing PATH without spawning,
exec start failure, non-zero exit's first stderr line and empty version), and Rust: Language Server
Help (action popup with install commands/PATH/restart/toolchain notes/upstream docs link; copy
rustup/brew, check, native lsp_restart, show_lsp_status, dismiss). Unported: automatic
lsp_server_error status, contributed LSP-Servers popup rows/menu slices, recovery detection via
lsp_status_clicked, Disable Rust LSP and Configure Mode (registerLspServer full/reduced init options+
process limits, restartLspForLanguage) — all need plugin-API LSP menu/disable/register/restart seams
gvim lacks. Residuals: LookPath is a local-machine best-effort in remote workspaces (only --version
follows the authority); the generic lsp_help plugin already owns Rust LSP-error status/install popup,
so rust_lsp adds explicit commands instead of a second handler; native vim LSP actions are used for
restart/status; real host UNTESTED. Bundled set now 28 plugins (9 on/19 off), 63 registration sites;
default set and idle heap unchanged; tests cover manifest/visible commands/no bindings, copy
clipboard+status, missing PATH no process, found+version argv, non-zero version exit, exec start
failure, help popup message/actions and copy/check/native restart dispatch (R13-6).
Metrics: palette 240, settings 69 in 14 sections, bundled 28 (9 on)/19 off, plugin API 247 functions/
1501 lines level 2; i18n en 1124 / 579 per locale, 575 KiB text (under 640 KiB cap), ≈665 KiB JSON,
heap 122-82-205 KiB. Full suite EXIT 0 (76 packages, log /tmp/gvim-r43-final.log); real hosts
UNTESTED; no full-parity claim (residuals in §5 R13-1…R13-24 and §8).

**Round-42 outcome (2026-11-09, merged working tree on `fbf725a`).** Four slices landed: opt-in
markdown_source port; sidebar ps absolute-path hardening; keymap window_focused/window_blurred
plugin events; statusbar i18n. Static gates pass; final full
`go test ./... -p 1 -count=1 -timeout 1200s` EXIT 0: 76 packages ok, no FAIL (log
`/tmp/gvim-r42-final.log`). The audit read the diff and ran targeted tests; the Lead ran the full
suite.
**markdown_source opt-in port.** Fresh's plugins/markdown_source.ts is shipped as an explicit Lua
bundled port (default_enabled:false) while gvim's native editor/markdown.go rule set remains the
default behavior, unchanged. Hidden normal-context commands bind enter/tab/shift-tab and self-check:
Enter parses leading blanks + checkbox/ordered/unordered, continues same indent/bullet, ordered+1,
unchecked checkbox, empty item drops marker, non-list copies leading blanks, cursor placed; Tab on a
blank unordered/checkbox indents 4 spaces and cycles bullet *→-→+→* (checkbox state kept), ordered/
content inserts 4 spaces; Shift+Tab on blank unordered/checkbox with indent>=4 removes 4 bytes and
reverse-cycles *→+→-→*, else native dedent_selection. Non-Markdown/selection/extra carets/active plugin
input mode fall back to native insert_newline/insert_tab/dedent_selection; four-space unit hardcoded
like Fresh; failures status. Unported: no markdown-source buffer-local mode/auto-activation/status
(plugin API lacks source/compose view query) and no compose-view exclusion — handlers are normal-
context and self-check. Bundled set 27 plugins (9 on/18 off, markdown_source off), no palette command
change, default set/idle heap unchanged; tests cover manifest/hidden commands/bindings, Enter/Tab/
ShiftTab tables+cursor, fallbacks, read-only failure (R13-6).
**Sidebar ps absolute-path hardening.** The local foreground-process name fallback no longer resolves
ps through PATH: resolvePSPath tries only the fixed absolute {/bin/ps, /usr/bin/ps, /sbin/ps,
/usr/sbin/ps}, requires a regular executable file, and psComm executes that absolute path with no
shell, a bounded context/output and a kernel-read integer pgid. Missing/non-executable candidates
degrade to the recorded launch/OSC fallback; Linux /proc is preferred. This closes the previously
recorded PATH-injection residual. Residuals: the four fixed system locations remain a trust
assumption about root-owned system directories (a system-level replacement is outside gvim), unusual
prefixes/NixOS without one of them lose the live name and fall back, no hash/signature allowlist,
and real macOS/BSD/Linux process polling remains UNTESTED (R13-15).
**window_focused/window_blurred plugin events (keymap).** On a real host-window transition the model
emits window_focused/window_blurred (empty payload) through the normal plugin event pump AFTER updating
the dock's DECSET-1004 focus reporting; a repeated FocusMsg/BlurMsg for the same state raises nothing
(the model starts host-focused), no plugin callback runs inside the focus message itself, and with no
subscriber nothing is queued. FocusMsg still performs the remote reconnect-on-activate; BlurMsg is not
a reconnect point. focus_gained remains the chrome/editor keyboard event and is not raised by a
terminal focus change. These events report the host terminal window's OS focus, not which gvim
surface holds the keyboard; dock/palette/editor internal focus changes do not raise them. No data
risk; real host focus reporting depends on the outer terminal (as before) (R13-24/R13-15).
**Statusbar i18n.** The recent status-bar/degradation strings were extracted with English byte-
identical; formatting/fallback/key-guard tests refreshed. Catalogue remains en 1124 / de 579 keys,
575 KiB key+value text (under 640 KiB cap), ≈665 KiB embedded JSON, loader heap en 122 KiB / de 82
KiB / both 205 KiB (no net key-count change this slice) (R13-5).
Metrics: palette 240, settings 69 in 14 sections, bundled 27 (9 on)/18 off, plugin API 247 functions/
1501 lines level 2; i18n en 1124 / 579 per locale, 575 KiB text, ≈665 KiB JSON, heap 122-82-205 KiB.
Full suite EXIT 0 (76 packages, log /tmp/gvim-r42-final.log); real hosts UNTESTED; no full-parity
claim (residuals in §5 R13-1…R13-24 and §8).

**Round-41 outcome (2026-11-09, merged working tree on `ce03f16`).** Five slices landed (with the lsp
bounded-directory candidate WITHDRAWN after audit): completed-index exact probe + 16-path cache;
sidebar local foreground-process title; dashboard callback isolation; statusbar i18n; no server-side
bounded directory listing. Static gates pass; final full `go test ./... -p 1 -count=1 -timeout
1200s` EXIT 0: 76 packages ok, no FAIL (log `/tmp/gvim-r41-final.log`). The audit read the diff and
ran targeted tests; the Lead ran the full suite.
**No server-side bounded directory listing (lsp; candidate evaluated and WITHDRAWN).** The remote
finder fallback cannot get a provable hard server-side bound from the portable tools: a
find -print0 | head -c pass limits output/memory bytes and avoids shell-glob materialisation, but a
kind-filtered pass may walk entries without emitting them (so it is not a provable bound on traversal/
entries), a per-entry -exec marker spawns unbounded children for short names, and -printf is not
portable to BSD/macOS. Nothing ships: no LimitedDirReader/BoundedDirLister/ReadDirLimited capability is
advertised, SSH ReadDir keeps its existing behaviour and its 5000-entry/4 MiB error limits, the remote
finder fallback keeps the existing ReadDir walk and explicit oversized-directory error, and gvim does
NOT claim a server-side bounded listing or bounded remote read. The remote literal probe remains
exact-path Stat only (realpath+Stat, no directory enumeration). The earlier find+head candidate's
residual is retained as the reason: output bytes are bounded but per-directory helper subprocesses are
not; real SSH UNTESTED (R13-8/R13-3).
**Completed-index literal-path probe + 16-path cache (keymap).** The file mode now asks the index's
optional literal-path probe both while the index is enumerating AND after it has completed, as Fresh
does: a completed/truncated index can still miss an exact file, so a typed path probes directly. The
local probe resolves root and every candidate through symlinks and returns a path only when its real
target stays inside the real workspace; a local directory query lists that directory's visible regular
files, otherwise the parent's files whose names start with the query's basename, bounded at 4096
inspected entries/batches/cancel and at most 50 returned (hidden rules as before). The REMOTE probe
remains the exact-path Stat slice only (authority realpath+Stat, no local disk, no directory
enumeration). Probes run only in tea.Cmd, never Update/Draw; context cancels on query change/palette
close. The answer INCLUDING A NEGATIVE is cached for the palette's lifetime in a bounded 16-path cache
keyed by path text, so a loading poll, repeated key, :line[:col] change or mode-prefix escape for the
same path does not repeat the Stat; a changed path probes again. A result is displayed only when query
sequence and text still match. Confirmed paths merge before normal rank so fuzzy/prefix/:line[:col]/
100-row cap apply. Residuals: remote parent-basename/directory branches still not probed (need a
bounded directory reader that does not exist); local 4096 scan cap can miss later entries; probe branch
has no .gitignore filtering; the 16-path cache is per open palette, so a file created/removed after a
cached negative/positive needs the palette reopened; exact local symlink followed only while its real
target stays inside the workspace (realpath-check→Stat TOCTOU remains theoretical) (R3/R13-3).
**Sidebar local foreground-process title (sidebar).** Terminal tab labels now follow Fresh's
combine_terminal_title fully for LOCAL Unix shells: explicit rename wins; otherwise the terminal's live
foreground command and the recorded or live OSC title are combined as `command — title` (em dash),
unless the OSC title already names the command case-insensitively (e.g. vim's file - VIM), which is
used alone; either part alone is used as-is and both empty falls back to Terminal N. The command is
the foreground process name Fresh polls (~1 s): gvim does the same bounded, non-blocking poll in a
tea.Cmd — one tcgetpgrp ioctl on the PTY master plus a bounded /proc/<pgid>/comm read on Linux, or a
500 ms-bounded ps -o comm= -p <pgid> on other Unix (no shell) — caches the sanitized name per tab,
drops a late answer from an old shell generation, and updates within one poll; background tabs keep
polling. A remote, Windows, restored or exited terminal reports no poll capability and falls back to
the recorded launch program, then OSC title. Name is sanitized (controls/invalid UTF-8 dropped,
trimmed, bounded 64 runes); Linux comm truncates at 15 chars; missing ps degrades to fallback.
AUDITED RESIDUAL: the ps fallback is resolved through PATH by exec.CommandContext (no shell, pgid is
kernel-read integer, stdout bounded), but there is no absolute-path/allowlist; a poisoned or
project-activated PATH could put a different ps first and the once-per-second poll would execute it.
This is recorded, not called a proven security boundary. Real macOS/BSD/Linux PTY/process polling
remains UNTESTED (R13-15).
**Dashboard callback isolation (plugin).** gvim.dashboard.register_section/remove_section/sections
(third-party dashboard sections) register bounded plain-data refresh callbacks retained in the
provider's Lua state. The host enforces isDashboardConsumer: only the BUNDLED dashboard plugin (name
dashboard AND SourceBundled) receives a non-empty key from sections(); every other plugin gets key=""
and the cross-plugin proxy's lookup returns nil for a dashboard key (a direct call is rejected even if
a key were guessed). The callback value is never exposed across states; args are plain data; results
go through plainData and validateDashboardResult (nodes 4096/depth 8/string 4 KiB), registration caps
16 per plugin/64 total, name 64/title 120, ttl/timeout/priority clamps; dashboard side applies per-
section timeout/TTL/row caps and keeps failures/stale rows isolated. No command/URL/network execution
path is added; dashboard rows are text only. Residual/limits as documented: up to 8 external sections
painted/64 rows each, sequential provider refreshes can delay later sections up to a timeout, a timed-
out provider's nested work may finish later, external rows have no column layout/click actions/URL
targets, and real third-party/remote providers are UNTESTED (R13-6/R13-24).
**Statusbar i18n (statusbar).** Recent status-bar/degradation title strings are catalogued with English
byte-identical; formatting/fallback/key-guard tests refreshed. Catalogue now en 1124 / de 579 keys,
575 KiB key+value text (under 640 KiB cap), ≈665 KiB embedded JSON, loader heap en 122 KiB / de 82
KiB / both 205 KiB (R13-5).
Metrics: palette 240, settings 69 in 14 sections, bundled 26 (9 on)/17 off, plugin API 247 functions/
1501 lines level 2 (gvim.dashboard +3 functions); i18n en 1124 / 579 per locale, 575 KiB text, ≈665
KiB JSON, heap 122-82-205 KiB. Full suite EXIT 0 (76 packages, log /tmp/gvim-r41-final.log); real
hosts UNTESTED; no full-parity claim (residuals in §5 R13-1…R13-24 and §8).

**Round-40 outcome (2026-11-09, merged working tree on `6c6f71b`).** Five slices landed: local
bounded prefix/directory probe with remote exact-Stat fallback; remote external-snapshot notice;
sidebar F3/Shift+F3 preview stepping; Env:Reload watcher retry supervisor; SSH new-target link
capability (audited, with explicit residuals). Static gates pass; final full
`go test ./... -p 1 -count=1 -timeout 1200s` EXIT 0: 76 packages ok, no FAIL (log
`/tmp/gvim-r40-final.log`). The audit read the diff and ran targeted tests; the Lead ran the full
suite.
**Bounded local file probe; remote exact-Stat fallback (keymap).** The local literal-path probe now
supports Fresh's prefix slice beyond one exact path: a query naming an existing directory lists that
directory's visible regular files; otherwise it lists the parent and keeps regular files whose names
start with the query's basename. Local directory work is bounded in batches (at most 4096 inspected
entries, checking cancellation, at most 50 returned paths; hidden entries only when the typed
basename starts with a dot; a directory listing skips them), and every candidate is resolved through
symlinks and returned only when its real target stays inside the real workspace. The REMOTE probe
deliberately supports the exact-path Stat slice ONLY: it resolves root and literal candidate through
the authority's realpath capability and stats that one file, never the local disk, and refuses when
containment cannot be proven. It performs no remote directory enumeration because the authority
ReadDir shell glob materializes the whole directory before any client-side limit could trim it (so the
probe does not claim remote reads are bounded). Every probe runs only in a tea.Cmd, never
Update/Draw; context cancels on query change/palette close; answers are cached for loading poll ticks
and displayed only when query sequence/text still match; confirmed paths merge before normal rank so
fuzzy scoring/prefix boost/:line[:col]/file-row cap apply. Residuals: remote parent-basename/directory
branches are not probed (need a bounded directory-reader port); local scan cap can miss entries past
4096; probe branch has no .gitignore filtering; completed-but-truncated index not probed; exact local
symlink followed only while real target stays inside workspace (realpath-check→Stat TOCTOU remains
theoretical); hidden entries not listed for a hidden-directory query (R3/R13-3).
**Remote external-snapshot notice (lsp).** When a remote search is bounded by the panel's quick-open
file-index snapshot and the remote tool returns a matching path outside that snapshot, the Done batch
carries a non-fatal notice: the result was limited to the snapshot, the path may be new since the
index last refreshed (or excluded by the glob), pointing at File Explorer: Refresh. It is not marked
Truncated because this is a snapshot bound, not an output cap, but the result is never presented as
an unqualified complete project search. The finder index still does not refresh itself on a search;
the notice is the safe slice until a panel-driven refresh is wired. Authority-only/bounded/cancel/no
local disk/no complete-looking result (R13-11/R13-23).
**Sidebar terminal F3/Shift+F3 preview stepping (sidebar).** While the scrollback search input is
open, F3 and Shift+F3 step the preview forward/backward through the current query's matches (wrapping
at the ends) without closing the input or committing the query, mirroring Fresh's find_next/
find_previous bindings in the searchPrompt context. The first press on an edited/unrun query runs it
first; if the current query's answer has not arrived, the F3 request is ordered like any other
preview (F3 answer lands on the first match, Shift+F3 on the last — Fresh's backward first press lands
on the match before the cursor, a documented approximation). Bad regex is a notice that keeps
previous matches; Enter commits, Esc restores the previous view/search; Update/Draw never search
(R13-15).
**Env:Reload watcher retry supervisor (trust).** If the very first local watch start fails (most
commonly an unwatchable workspace root), Environment rows keep the Local Env: Reload notice, and a
later Env: Reload recovers it: the env-manager's first detection read (or set/clear action when an
environment is active) asks the app to start a fresh bounded watcher in the background (no fsnotify
call on the UI update loop); the dead predecessor's resources are released exactly once; a successful
retry re-runs passive detection and clears the notice; a live watcher is untouched so ordinary
activations do not churn fsnotify. Fresh never retries its best-effort watch registration, so this
recovery path is a deliberate gvim improvement; detection-only semantics and local wording preserved
(R13-18).
**SSH new-target link capability (plugin copy; audited).** Plugin copy now has two capability-gated
safe surfaces: local no-follow replace (temp+rename over the path, existing destination atomically
replaced) and SSH NEW-TARGET ONLY: stream into a private mktemp in the destination directory, commit
with the POSIX link utility (exact target, one no-clobber hard link; never ln/mv/rm of the target). A
pre-write probe in the destination directory proves exact-target hardlink semantics (link present;
second link/symlink target/directory target all EEXIST without following); otherwise unsupported
before source data is read. Existing files/directories/symlinks/dangling symlinks and concurrent
creators are conflict and are never followed, replaced or deleted; abort/failure removes only the
private temp. A tree copy needs DirectoryCreator plus this stream and proves the destination parent
BEFORE claiming the root; existing SSH destinations are unsupported (no no-follow replace). Audit
residuals recorded in docs: (a) temp cleanup is best-effort, not guaranteed — commit transport
errors/cancellation and failed rm after a successful link can leave .gvim-* temps, a killed probe can
leave .gvim-lnprobe-*; the target is still complete and is never deleted; (b) parent-path TOCTOU is
not closed — canonicalization/probe are pre-write proofs only, so a renamed/symlinked parent can make
link create a new target in an unexpected directory (never over an existing target; no dirfd/openat
containment recheck); (c) per-lexical-directory probe cache can go stale after replacement/mount (a
later commit fails safely or links under the current path); (d) probe is a capability proof, not a
strong atomicity proof (a second-link failure for quota/permission/link-count rather than EEXIST can
pass; later commit only fails safely with target absent/untouched); (e) tree proof covers the parent
and assumes the created tree is on the same filesystem, failure after root claim can leave a partial
tree; (f) tests exercise the local SSH helper shell, NOT a real SSH host — only the claim "hardlink
no-clobber exact target; target complete atomically when the helper succeeds" is made; no durability/
fsync or general filesystem-race guarantee; the temp is appended by path, so a same-account process
that can replace the private temp name is outside this protection (R13-19/R13-21).
Metrics: palette 240, settings 69 in 14 sections, bundled 26 (9 on)/17 off, plugin API 244 functions/
1489 lines level 2; i18n en 1117 / 579 per locale, 573 KiB text (under 640 KiB cap), ≈663 KiB JSON,
heap 126-88-214 KiB. Full suite EXIT 0 (76 packages, log /tmp/gvim-r40-final.log); real hosts
UNTESTED; no full-parity claim (residuals in §5 R13-1…R13-24 and §8).

**Round-39 outcome (2026-11-09, merged working tree on `bc5624b`).** Five slices landed: literal-
path quick-open probe; remote search tool-failure open-buffer fallback; local watcher closeResources;
incremental terminal scrollback search; SSH copy new-target link-commit research only (not
implemented). Static gates pass; final full `go test ./... -p 1 -count=1 -timeout 1200s` EXIT 0: 76
packages ok, no FAIL (log `/tmp/gvim-r39-final.log`). The audit read the diff and ran targeted tests;
the Lead ran the full suite.
**Literal-path quick-open probe (keymap; Fresh's fast prefix-probe slice).** While the file index is
still enumerating, file mode asks the index's optional exact-path probe for the literal file path the
query names, so a typed `etc/hosts` finds the file before enumeration reaches it. This is deliberately
ONE exact path — not a directory or basename-prefix listing. The local probe resolves the candidate's
real path and the root's real path and returns only when the candidate is a regular file inside the
workspace; the remote probe uses the authority's realpath capability and Stat, never the local disk,
and refuses when the backend cannot prove containment. The probe runs only in a tea.Cmd, never in
Update/Draw; its context is canceled when the query changes or the palette closes; its answer is
cached for the loading poll ticks and a result is displayed only when both the query sequence and text
still match. The confirmed path is merged before the normal rank, so the usual fuzzy scoring, prefix
boost, :line[:col] target and file-row cap apply. Missing paths, directories, unreadable paths, escapes
through .. or a nested symlink, and a completed index (no probe phase) produce no probe row; a probe
never falls back to another filesystem. Residuals: parent-directory basename-prefix matches and
directory content listings are not probed; a completed-but-truncated index is not probed; an exact
local symlink is followed only if its real target stays inside the workspace (a TOCTOU swap between
the realpath check and Stat remains theoretically possible) (R3/R13-3).
**Remote search tool-failure open-buffer fallback (lsp).** When the remote search tool cannot be probed
(missing) or its run fails/times out, the open-buffer snapshots are still streamed first, then a
terminal error batch names the original tool/probe failure and says only open buffers were searched
from their editor snapshots — the remote project files were not searched. The result is never
presented as complete; no local disk is read; a user cancellation still closes the batch stream
without a Done batch (no open-snapshot rows are emitted after the cancel). The same panel limit/caps
apply to snapshot matches. All commands still run through the authority one-shot surface with the
remote cwd, stay bounded/cancellable (the search context bounds every nested listing), and never read
the local disk; other degradations are non-fatal panel notices (R13-11/R13-23).
**Local watcher closeResources fix (trust).** The local env watcher now releases its file watcher
exactly once via closeResources/sync.Once across the startup-failure path, Close, and the goroutine
exiting on session cancellation (previously each path could close the watcher independently).
armDirs-before-RefreshDetection is preserved, real-fsnotify tests still cover marker lifecycle/nested
parent appearance/unwatchable root, and detection remains detection-only (no recipe re-activation, no
capture-cache recapture). Close is idempotent; no watcher/goroutine leak; local wording kept
(R13-18).
**Incremental terminal scrollback search (sidebar).** '/' or Ctrl+F opens the search input; while
the query is typed every change runs a search asynchronously and the view jumps to the first match
(a restored transcript previews its result without committing). An empty query cancels pending
previews and moves nothing; regex/case toggles re-run the preview. Only the newest request may apply:
each answer carries a request sequence, so a late answer for an older keystroke (or a preview
superseded by Enter) is dropped and cannot move the view or replace the committed search. Enter
commits the query and its matches (title shows k/n); Esc drops the preview, restores the view the
input opened on and keeps the previously committed search/matches. A bad regex is a notice that
keeps the previous matches. When the transcript is re-wrapped by a width change, Sync asynchronously
re-runs the committed query over the new rows (Update/Draw never search), keeping query/options and
n/N/title alignment; copy uses the editor clipboard path. Fresh's read-only scrollback is a normal
buffer, so its search naturally continues over re-wrapped text; this is the gvim bounded equivalent
(R13-15).
**SSH plugin copy new-target link-commit — research only, NOT implemented.** The SSH backend still
does not advertise NoFollowWriteStreamer and plugin fs.copy remains unsupported on SSH (before
reading/creating/writing). The research considered a future SSH link-commit for a new target, but it
was not implemented because POSIX mv follows a destination symlink-to-directory, and a rail with
check→rm→mv has TOCTOU windows (target swap between check and rm, a symlink-to-directory appearing
before mv so the temp is moved into it, failure/cancel after rm losing the destination entry) and
parent-path races; no portable remote rename is proven to replace a link atomically without a
check-then-remove helper. Local no-follow (temp+os.Rename) remains the only advertised capability,
with the documented local parent-path TOCTOU residual; no SSH strong/atomic no-overwrite guarantee is
claimed. The old rm-before-mv path must not be reintroduced without a fix for those exact windows
(R13-19/R13-21).
Metrics: palette 240, settings 69 in 14 sections, bundled 26 (9 on)/17 off, plugin API 244 functions/
1489 lines level 2; i18n en 1117 / 579 per locale, 573 KiB text (under 640 KiB cap), ≈663 KiB JSON,
heap 126-82-208 KiB. Full suite EXIT 0 (76 packages, log /tmp/gvim-r39-final.log); real hosts
UNTESTED; no full-parity claim (residuals in §5 R13-1…R13-24 and §8).

**Round-38 outcome (2026-11-09, merged working tree on `f3ab872`).** Six unboarded slices landed
(file multi-word matcher already present; buffer-mode fuzzy multi-word; remote AllFiles open-buffer
snapshots; local watcher real-fsnotify arm-before-refresh; terminal transcript re-search on resize;
plugin no-follow local-only with SSH copy unsupported). Static gates pass; final full
`go test ./... -p 1 -count=1 -timeout 1200s` EXIT 0: 76 packages ok, no FAIL (log
`/tmp/gvim-r38-final.log`). The audit read the diff and ran targeted tests; the Lead ran the full
suite.
**File multi-word matcher (already present) + buffer-mode fuzzy multi-word.** The file mode splits a
query on whitespace and every term must match the candidate path in any order, matching Fresh's file
finder (feat group → features/groups/view.tsx, etc hosts → /etc/hosts, save file → save_file.rs); a
legal path containing spaces still matches (my file → my file.txt) because whitespace separates terms;
a path:line[:col] suffix is stripped before matching. Buffer mode (#) now applies the same
Fresh-FuzzyMatcher behavior to tab labels: #file save → save_file.go and a fuzzy subsequence such as
#sflg matches without being contiguous; rows are ordered by score with tab order breaking ties. Terms
are prepared once per query, a reusable matcher is used, and an ASCII candidate that cannot contain a
term is rejected without rune conversion/allocation. The finder keeps 50,000 indexed files, 100
file-mode results, background-enumeration cancellation and the palette sequence guard (R13-3).
**Remote AllFiles search includes current open-buffer snapshots.** The panel's glob-filtered file
snapshot still bounds AllFiles, but open buffers are now searched from the editor's own snapshots
(the current buffer first, then the other open buffers after the tool results); that file's remote
disk line is skipped, so unsaved edits are found exactly as Fresh's project_files searches open
buffers, with no extra remote read. An open file outside the workspace root, or excluded by the
panel's file snapshot, is left to the dedicated Open Buffers scope. All commands still run through
the authority one-shot surface with the remote cwd, stay bounded/cancellable (search context bounds
every nested listing), never read the local disk, and carry a non-fatal notice on degradation
(R13-11/R13-23).
**Local detector watcher real fsnotify + arm-before-refresh (trust).** Event handling now arms the
nested watch (armDirs) BEFORE re-detection (RefreshDetection), closing the window where a file
created after a nested marker parent directory appears could be missed between the refresh and the
late Add. Real-fsnotify integration tests use temporary directories and the production filewatch
implementation (no fake watcher, no fixed sleeps): marker create/remove, a nested marker parent
appearing after start (root event must re-arm first), and an unwatchable root degrading to the
explicit Env: Reload notice. Still detection-only (no recipe re-activation, no capture-cache
recapture), non-blocking, no self-trigger loop/leak; local-wording notices only (R13-18).
**Terminal transcript re-search after resize/re-wrap.** When a restored transcript or a restarted
shell's prior rows are re-wrapped by a width change, the stored match row indexes no longer hold, so
Sync re-runs a committed query over the new rows asynchronously (Update/Draw never search): the
query and its regex/case options survive, matches jump to the same text, and n/N and the title stay
aligned. A copy goes through the same clipboard path the editor's copy uses. Fresh's read-only
terminal scrollback is a normal buffer, so its search naturally continues over re-wrapped text as
well; this is the gvim bounded equivalent (R13-15).
**Plugin no-follow stream: local only, SSH copy unsupported (final safety rollback).**
NoFollowWriteStreamer is implemented by the LOCAL authority only, using os.CreateTemp + os.Rename:
the temp file is committed with a rename over the final entry, so a final-component symlink is
replaced, not followed, and an existing target is atomically replaced (hard-link boundary safe).
The SSH backend deliberately does NOT advertise the capability: POSIX mv follows a destination
symlink-to-directory and no portable remote rename was proven to replace a link atomically without
a check-then-remove helper; the rejected rm-before-mv approach could lose a target on failure or
race the check. Plugin fs.copy requires the no-follow surface, so on SSH it returns unsupported
BEFORE reading, creating or writing anything; local copy still requires RealPather, DirectoryCreator
for trees, and rejects canonical destinations inside the source. Residuals: local parent-path
replacement TOCTOU (CreateTemp resolves dirname; Commit rename re-resolves; failure cleanup path can
be redirected), canonicalization is a pre-write proof (concurrent symlink swap after it remains
TOCTOU), local hard-link identity is protected by rename semantics but no strong no-overwrite/general
atomicity guarantee is claimed; remote SSH copy is unsupported rather than pretending to be atomic;
tree root claim/failure-after-claim partial destination, metadata/growth/cancel limits remain
(R13-19/R13-21).
Metrics: palette 240, settings 69 in 14 sections, bundled 26 (9 on)/17 off, plugin API 244 functions/
1489 lines level 2; i18n en 1117 / 579 per locale, 573 KiB text (under 640 KiB cap), ≈663 KiB JSON,
heap 120-82-203 KiB. Full suite EXIT 0 (76 packages, log /tmp/gvim-r38-final.log); real hosts
UNTESTED; no full-parity claim (residuals in §5 R13-1…R13-24 and §8).

**Round-37 outcome (2026-11-09, merged working tree on `c0b42a1`).** Tasks 255–257 landed plus the
alternate-screen exit and local-watcher scope/i18n fixes. Static gates pass; final full
`go test ./... -p 1 -count=1 -timeout 1200s` EXIT 0: 76 packages ok, no FAIL (log
`/tmp/gvim-r37-final.log`). The audit read the diff and ran targeted tests; the Lead ran the full
suite.
**Palette Tab keeps the file jump target (255; gvim extension).** In file mode Tab now fills the
ranked path together with the `:line[:col]` target the query named, so a jump target is not lost
(Fresh itself loses it). A Windows drive or any other colon in the path is left intact and the
target is read from the end; a file whose own name ends in `:number` is kept verbatim for that
accepted query instead of being re-read as a jump target; a name beginning with a palette mode
character (>, # or :) is written with a leading `./` escape so the query stays in file mode (the
ranker strips it). Command/buffer/line modes still fill the command/buffer name or line target
preserving the >, # or : prefix; the palette stays open, rows re-query, Enter runs/opens. Message/
disabled/no selection unchanged; Live Grep overlay Tab remains a no-op (R3/R13-3).
**Remote plugin copy requires the atomic streamed-write surface (256).** Every remote file copy now
goes through the authority's atomic WriteStream; a backend without that surface gets unsupported
BEFORE reading, creating or writing the destination. There is deliberately no create-then-WriteFile
fallback: FileCreator only creates an empty file and gives no exclusive handle for the data write, so
another writer could replace the path and then be overwritten by the copy. A directory copy also
requires the streamed-write surface (in addition to DirectoryCreator) before the destination claim/
walk. With the surface an existing destination is atomically replaced (temp commit/abort), so the
hard-link boundary is safe; a refused capability leaves no target created or written. Residuals:
canonicalization is a pre-write proof (concurrent symlink swap after it is a documented TOCTOU),
remote hard-link identity cannot be compared like local os.SameFile, parents are created before the
claim, failure after claim can leave a partial tree, and metadata/growth/cancel limits remain
(R13-19/R13-21).
**Nested-prefix fallback roots + cap notice (257).** When nested candidates cannot all be resolved,
the fallback now searches the unresolved nested-prefix ROOTS in place instead of the whole tree, so
unrelated ignored entries cannot consume its output cap before them; the wider whole-tree fallback is
reserved for a candidate list too large for the bounded explicit batches. Both prune hidden path
segments and IGNORED_DIRS (.git, node_modules, target, __pycache__, .hg, .svn, .DS_Store) in the
candidate filter and carry the non-fatal notice. If the widened run hits the remote output cap, the
batches are marked Truncated and the notice says the result is incomplete because candidate files
beyond the cap were not reported — never a complete-looking result; a partial nested candidate set is
never presented as complete, and a deeper nested repository's own .gitignore is not applied in that
fallback (recorded residual) (R13-11/R13-23).
**Alternate-screen exit shows the read-only frame + marker.** A RUNNING alt-screen shell still has no
history (Ctrl+Space reports “The alternate screen has no scrollback.”); once it exits, the frozen alt
frame is turned into the read-only transcript (its visible rows plus the exit marker, title
`Scrollback (exited, …)`), auto-opened at exit like a main-screen exit, and Ctrl+Space still toggles
it with the frozen frame. Nothing overwrites the frame's output: a save stores the frame rows with
exactly one marker after them, a restart continues below the captured frame and marker, and a restore
shows the same text record without duplicating either. The pager has no alternate-screen history
(x/vt keeps none), so only the visible frame and the marker are there (R13-15).
**Local watcher scope notice + i18n.** Local env-watch degradation notices now use catalogue keys
(unavailable/ended/root-failed/root-unwatchable/dirs/limit), and the watch-plan omitted/omittedDirs
counters drive the limit notice (marker paths/directories not watched; use Env: Reload after changes).
New dock strings (tab limit/numbered title/hover token), workspace extract title and the local-watch
keys are catalogued; en 1117 / de 579 keys, 573 KiB text (under 640 KiB cap), ≈663 KiB embedded
JSON, loader heap en 126 KiB / de 82 KiB / both 208 KiB (R13-5/R13-18).
Metrics: palette 240, settings 69 in 14 sections, bundled 26 (9 on)/17 off, plugin API 244 functions/
1489 lines level 2; i18n en 1117 / 579 per locale, 573 KiB text, ≈663 KiB JSON, heap 126-82-208 KiB.
Full suite EXIT 0 (76 packages, log /tmp/gvim-r37-final.log); real hosts UNTESTED; no full-parity
claim (residuals in §5 R13-1…R13-24 and §8).

**Round-36 outcome (2026-11-09, merged working tree on `40598e7`).** Five tasks landed (249–253);
254 is this audit. Static gates pass; final full `go test ./... -p 1 -count=1 -timeout 1200s` EXIT
0: 76 packages ok, no FAIL (log `/tmp/gvim-r36-final.log`). The audit read the diff and ran targeted
tests; the Lead ran the full suite.
**Host-window + dock focus merged for DECSET 1004 (249).** gvim enables View.ReportFocus for every
workspace, so Bubble Tea reports the real outer-window focus as FocusMsg/BlurMsg; the model merges it
with the dock's keyboard focus and forwards the effective state to the dock (Tabs.SetHostFocused/
Panel.SetHostFocused). A child receives focus-in only when window AND dock are focused, and focus-out
when either loses focus: switching to the editor, tab switch (old tab out/new in), hiding the dock, or
blurring the outer window; moving in pager/scrollback does not blur. Repeated/redundant calls (window
blur while dock already unfocused, window focus while a file buffer has the keyboard) send nothing, and
a new tab inherits the current window state. FocusMsg still performs the remote-authority
reconnect-on-activate; BlurMsg is never a reconnect point and only updates the merged focus. Reports
are only sent to a child that opted into 1004. Residuals: the window half depends on the outer terminal
honoring focus reporting (no events leaves the child believing focused; tmux needs its own focus
reporting configured), initial state is assumed focused until the first blur, focus mode is not
persisted in terminal.json (revived shell starts with 1004 off until the program asks), and a restored
read-only transcript has no child so receives nothing (R13-15).
**Local detector content-change invalidation — detection-only (250).** A bounded local fsnotify watch
invalidates detection when a configured detector marker/require content changes: bounded/cancellable/
non-blocking with a clear degradation path. It is DETECTION-ONLY: it does not re-activate the recipe
and does not recapture the capture cache. No self-trigger loop/resource leak; UI remains non-blocking
(R13-18).
**Remote nested-repository git candidates (251).** After the complete top-level Fresh git candidate
listing, up to 8 nested repositories/submodules listed by name are each resolved with their own
bounded `git -C <prefix> ls-files -z -s --cached --others --exclude-standard` through the authority
(same search context/cancellation, remote cwd, no local fallback). Their candidates join the explicit
rg/grep batches, so the nested repo's own .gitignore applies exactly as git defines it: tracked-
ignored and tracked dotfiles included, untracked non-hidden included, untracked ignored excluded;
success adds no widened scan/degradation notice. Resolution is ALL-OR-NOTHING: more than 8 prefixes,
a failed/truncated/non-UTF-8 listing, a path escaping the workspace, or a deeper nested repository/
submodule leaves every prefix unresolved and keeps the widened whole-tree walk (prunes hidden path
segments and IGNORED_DIRS, non-fatal notice) — a partial nested candidate set is never presented as
complete. Residual: a deeper nested repository's own .gitignore is not applied in that fallback
(R13-11/R13-23).
**Plugin copy root-source bypass + no-streamer hard-link safety (252).** A second read-only safety
review fixed a root source/subpath bypass: source `/` is now explicitly rejected (previously src + "/"
became `//` and could bypass the inside-tree/source comparison). The no-streamed-write fallback is
now allowed only for a NEW destination: if the destination already exists (or cannot be proven absent)
copy returns unsupported instead of truncating, because path canonicalization cannot identify a hard
link and an existing destination hard-linked to the source would otherwise destroy the source. With
the streamed-write surface an existing destination is atomically replaced (temp commit/abort), so the
hard-link boundary is safe there. Residuals: the no-streamer NEW-path case still has a concurrent-
creation race between the absence proof and the whole-file fallback write (not a no-overwrite
guarantee); canonicalization remains a pre-write proof (concurrent symlink swap after it is a
documented TOCTOU); remote hard-link identity cannot be compared like local os.SameFile; parents are
created before the claim; failure after claim can leave a partial destination; the 16 MiB whole-file
fallback, shrink/grow/cancel, metadata and partial-tree residuals are unchanged (R13-19/R13-21).
**Palette Tab accepts the selected suggestion (253).** Palette keyboard now matches Fresh's
prompt_accept_suggestion: Up/Down move selection, PageUp/PageDown page it, Enter runs/opens the
selected row, Escape closes. Tab ACCEPTS the selected suggestion into the query instead of running it
(file mode fills the ranked path; command/buffer/line modes fill the command/buffer name or line
target, preserving the >, # or : prefix); the palette stays open, rows re-query, Enter then runs/opens.
A message row, a disabled command or no selection leaves the query unchanged. In Live Grep's floating
overlay Tab is a no-op as in Fresh. Tests/docs cover keyboard/state; this was a concrete missing
interaction, not styling (R3/R13-3).
Metrics: palette 240, settings 69 in 14 sections, bundled 26 (9 on)/17 off, plugin API 244 functions/
1489 lines level 2; i18n en 1107 / 579 per locale, 572 KiB text (under 640 KiB cap), ≈662 KiB JSON,
heap 124-82-207 KiB. Full suite EXIT 0 (76 packages, log /tmp/gvim-r36-final.log); real hosts
UNTESTED; no full-parity claim (residuals in §5 R13-1…R13-24 and §8).

**Round-35 outcome (2026-11-09, merged working tree on `0650fd4`).** Five tasks landed (243–247);
248 is this audit. Static gates pass; final full `go test ./... -p 1 -count=1 -timeout 1200s` EXIT
0: 76 packages ok, no FAIL (log `/tmp/gvim-r35-final.log`). The audit read the diff and ran targeted
tests; the Lead ran the full suite.
**Local env detector hot reload — detection-only (243).** A local env.Provider config Reload
re-scans/updates the detector RESULTS passively; it does NOT invalidate or recapture the active
recipe/capture cache and does not re-activate/re-warm the environment. UI remains non-blocking and
this path is local-only. (Corrected per trust report: 243 is detection-only, not a capture re-warm.)
**Remote search candidate filter (244).** Walked/nested-content filtering now follows Fresh's walk
scope: hidden path segments and IGNORED_DIRS (.git, node_modules, target, __pycache__, .hg, .svn,
.DS_Store) are refused. Applied to the non-git/incomplete-listing fallback and to contents below
nested repositories/submodules in the widened fallback; nested prefixes whose own path is hidden or
under a skip dir are no longer registered. A complete git listing keeps its explicit-membership
exception: tracked/untracked candidates (including a tracked file under node_modules) stay
searchable exactly as Fresh's git path does. Unchanged: bounded explicit batches, cancellation/
timeouts, authority-only reads, non-fatal degradation notices, finder-snapshot authority, no local
fallback (R13-11/R13-23).
**Embedded-terminal focus events (DECSET 1004) (245; gvim extension).** A child asks for focus in/out
with `ESC [ ? 1004 h`; the session tracks the mode from the emulator and, while on, writes canonical
`CSI I`/`CSI O` into the child's input through the same asynchronous inbox as key input and emulator
replies. Reports are sent only on real transitions: dock/tab gains keyboard focus, editor takes it
back, tab switch (old out/new in), dock hidden; scrollback/pager movement does not blur the shell,
and a repeated focus call is ignored. If the child enables 1004 while the dock already has focus, the
session sends the initial focus-in after that output chunk; disabling the mode stops reports while
focus state is still tracked. A new/restarted shell learns the dock's current focus state on start
before any 1004 request; dead/restored tabs have no session and send nothing. Bytes are sent only
after opt-in, so programs that never ask see no change; mouse/keyboard/clipboard forwarding
unaffected. Fresh asks its host terminal for focus events but does not forward them to embedded
terminals, so this is a gvim extension. Remaining difference: only the dock's keyboard focus is
forwarded, not OS window focus (with View.ReportFocus for a remote authority, losing the outer
terminal window while the dock is focused sends no blur); focus mode is not persisted in
terminal.json (revived shell starts off until the program asks) (R13-15).
**Plugin copy canonicalization + capability refusal (246).** Both plugin copy forms now canonicalize
source and destination before the first write: a destination that is the source or lies inside a
copied directory tree — directly or through a symlinked/case-aliased parent — is refused with
invalid, as is an unresolvable/dangling destination symlink rather than guessing. The local backend
compares real paths + file identity (os.SameFile), so hard links, symlink components and case
variants cannot bypass the check; a bounded tree walk is never used as a substitute. Authority copy
requires the optional RealPather and canonicalizes source/destination before any write (a missing
destination is resolved through its deepest existing ancestor; dangling/unresolvable is refused); a
backend without RealPather returns unsupported rather than trusting a cleaned path or falling back
locally. A directory copy on a backend without DirectoryCreator also returns unsupported instead of
using the unsafe check-then-mkdir -p merge fallback. The root claim remains atomic root-only;
residuals: canonicalization is a pre-write proof, so a concurrent symlink swap after it (TOCTOU) is
documented; remote hard-link identity cannot be compared like local os.SameFile; parents are created
before the claim; failure after claim can leave a partial destination; single-file copy still
replaces an existing regular file; stream/no-stream 16 MiB/shrink/growth/cancel and metadata/partial
tree residuals unchanged (R13-19/R13-21).
**i18n extraction round-35 (247).** Recent terminal/env/workspace user-visible notices were extracted
with English byte-identical; catalogue rules/budget/docs/fallback and formatting tests refreshed.
Catalogue now en 1107 / de 579 keys, 572 KiB key+value text (under 640 KiB cap), ≈662 KiB embedded
JSON, loader heap en 119 KiB / de 88 KiB / both 207 KiB (R13-5).
Metrics: palette 240, settings 69 in 14 sections, bundled 26 (9 on)/17 off, plugin API 244 functions/
1489 lines level 2; i18n en 1107 / 579 per locale, 572 KiB text, ≈662 KiB JSON, heap 119-88-207 KiB.
Full suite EXIT 0 (76 packages, log /tmp/gvim-r35-final.log); real hosts UNTESTED; no full-parity
claim (residuals in §5 R13-1…R13-24 and §8).

**Round-34 outcome (2026-11-09, merged working tree on `564e2a4`).** Five tasks landed (237–241);
242 is this audit. Static gates pass; final full `go test ./... -p 1 -count=1 -timeout 1200s` EXIT
0: 76 packages ok, no FAIL (log `/tmp/gvim-r34-final.log`). The audit read the diff and ran targeted
tests; the Lead ran the full suite.
**Remote search preview syntax (237; recon + regression).** The bounded preview's syntax highlighting
was ALREADY implemented (commit 77c3ec7): remoteSearch.Preview highlights the bounded head it already
read through the shared previewHighlighterCache; open-buffer text is highlighted from req.Text; the
search panel paints PreviewSpans with theme SyntaxStyles; the documented 256 KiB/4000-line caps,
8-entry cache, CPU-only command and no-extra-remote-I/O behavior already hold. R34 added the missing
constraint tests: a truncated head, a disconnected preview and the no-additional-remote-read path
(spans come from the already-read text only). No duplicate implementation; docs already recorded it
(R13-23).
**Local large-file bounded edit context (238).** Quote auto-close and the indent rules' non-code mask
now work for local large files too, with a bounded window read through the buffer's page cache: a
query reads at most the 300-line reach around it within the 64 KiB text budget, never the whole file
(page-granular reads can touch slightly more than the budget); a line longer than the 4096-byte lexer
limit has no context and becomes the boundary of its neighbours' window instead of being
materialized. The same bounded window rule applies to windowed remote large files (only loaded
contiguous byte-bounded lines; window-arrival generation in the cache key); a context query is
read-only and does not change the document's editable/read-only semantics. Tests cover loaded
windows/long-line boundaries and the no-unbounded-UI-read constraint; local large files no longer
differ from remote in having no editing context (R13-15/R13-7).
**Runtime detector config reload: watcher re-plan (239).** A runtime config Reload that changes
`env.detectors` swaps the provider's detectors (invalidating detection/capture and re-warming them
in the background off the update loop) and re-plans the watcher from the new list: the old watches
are stopped and the new plan is bounded by the same path/directory caps, using only authority watches
(no local read). A backend whose file surface cannot watch degrades safely (watcher stops, Settings
says inputs are not watched and points at Env: Reload); if the watcher had already degraded to the
no-watch notice the detector provider is updated but no watches are restarted. A local launch's
provider keeps its startup detector set. Cancellation/no-local-fallback/caps preserved (R13-18).
**Terminal exit auto-opens the transcript (240).** At the exit instant the dock automatically opens
the exited shell's read-only transcript (Fresh turns an exited terminal into read-only scrollback),
whose last row is the marker, so the frozen output and the marker are visible without saving or
pressing Ctrl+Space; the marker can be selected/copied there, a save stores it as an ordinary bounded
transcript row, and a restart captures it before the new shell so restart/resume continues BELOW it and
never overwrites output. A hidden/background dock opens that transcript the first time it can be laid
out (next sync/show). While shown the title is `Scrollback (exited, exit N)`/`Scrollback (exited,
signal)` (plain `Scrollback (exited)` for exit 0/unknown) and the tab title stays exit; a restored
read-only tab shows the same suffix on `Scrollback (restored)`. Ctrl+Space still leaves the transcript
for the frozen last screen (and back again), a gvim extension on top of Fresh's read-only terminal.
The marker is written once per exit; an alternate-screen exit cannot open a pager (the frozen frame
stays, marker persisted for restore); `terminal.close_on_exit` hides the dock instead of keeping the
exited transcript. Old records restore as before (R13-15).
**i18n extraction round-34 (241).** Recent env/terminal user-visible notices moved into the catalogue
with English byte-identical; catalogue rules/budget/docs/fallback/key-guard tests refreshed. Catalogue
now en 1054 / de 579 keys, 568 KiB key+value text (under 640 KiB cap), ≈658 KiB embedded JSON, loader
heap en 120 KiB / de 82 KiB / both 203 KiB (R13-5).
Metrics: palette 240, settings 69 in 14 sections, bundled 26 (9 on)/17 off, plugin API 244 functions/
1489 lines level 2; i18n en 1054 / 579 per locale, 568 KiB text, ≈658 KiB JSON, heap 120-82-203 KiB.
Full suite EXIT 0 (76 packages, log /tmp/gvim-r34-final.log); real hosts UNTESTED; no full-parity
claim (residuals in §5 R13-1…R13-24 and §8).

**Round-33 outcome (2026-11-09, merged working tree on `8bbe790`).** Four tasks landed (231–234);
235 is read-only trash research; 236 is this audit. Static gates pass; final full
`go test ./... -p 1 -count=1 -timeout 1200s` EXIT 0: 76 packages ok, no FAIL (log
`/tmp/gvim-r33-final.log`). The audit read the diff and ran targeted tests; the Lead ran the full
suite.
**OSC 7 Windows paths + start-platform validation (231).** OSC 7 parsing matches Fresh's
parse_osc7_path with one deliberate hardening: the payload may be a plain path or file://host/path
(host dropped at first /, URI path percent-decoded, a leading /C:/ becomes drive-absolute C:/...),
and is accepted only when absolute in POSIX or Windows convention — leading /, UNC \\server\share,
or an ASCII drive letter followed by a separator (X:/... or X:\...). The drive-relative C:dir and
bare C: that Fresh's letter-colon check accepts are REJECTED here: they are relative to the drive's
current directory and unsafe as a start cwd. The flavour follows the reporting shell's OS, not gvim's;
a bare payload is not percent-decoded (like Fresh). Untrusted output is still cleaned and bounded
(non-empty, <=4096 bytes, valid UTF-8, no control characters) before it is kept; saved cwd is
cleaned again on capture/load. Start side mirrors platform semantics: a LOCAL start checks the saved
directory against this host's flavour (POSIX on Unix, drive-absolute/UNC on Windows) and a mismatch
is reported in the status row and replaced by the default local directory, never translated or passed
to a PTY of the other flavour. A REMOTE start (POSIX SSH host) requires a POSIX-absolute path; a
Windows path is refused with the remote-default notice before any authority stat. Residuals: Windows
paths kept exactly as reported (no slash normalisation/drive-case folding/conversion), cross-flavour
now refused at start rather than reaching the starter, Fresh also accepts drive-relative C:dir (gvim
deliberately rejects), a future Windows remote authority would need a flavour signal (SSH assumed
POSIX), invalid percent escapes stay verbatim and invalid UTF-8 is rejected (stricter than Fresh's
lossy decode), emulator-dropped/>4096 reports ignored, remote fallback is the authority default
(status-only), local saved dirs not pre-validated, remote check is point-in-time and a later remote
cd is not applied to the live session's files (R13-15).
**Bounded edit context for windowed remote large files (232).** Editing context for a windowed remote
large file follows the same loaded-window rule: the quote auto-close check and indent rules' non-code
mask ask StringContext/NonCodeContext caches that now read only loaded, contiguous, byte-bounded
(64 KiB) lines from the remote windows and never read or request an unloaded 1 MiB window. A query
outside the loaded windows answers “not in a string”/no mask without I/O, so typing a quote there
inserts normally without synchronous remote I/O. The window-arrival generation is part of the context
cache key, so when the paint later loads the window the next query re-lexes the context instead of
serving spans computed from blank placeholder lines; a loaded window's context is bounded by the
300-line reach, 64 KiB budget and 4096-byte line limit. Local large files keep their old behavior (no
editing context; reading their lines on the UI goroutine is not bounded by a windowed source)
(R13-15/R13-7).
**Custom detector marker/require watch (233).** The remote env-input watch now also covers
user-configured detector marker/require paths: a change invalidates cached detection/capture and
re-warms it in the background (no local filesystem). The watcher arms the workspace root plus the
existing parent directories of the watched paths, bounded by env.MaxDetectorWatchPaths marker paths
and env.MaxDetectorWatchDirs directories; excess paths are reported in the Settings notice and still
work through Env: Reload. Nested content changes are seen because the parent directory is watched even
when the root directory entry does not change; when a watched parent appears, the next relevant root
event re-arms the watcher. A backend whose file surface cannot watch degrades safely: watcher stops,
Settings says inputs are not watched and points at Env: Reload (manual reload works); other failures
retry with the same bounded backoff as remote document watching. Polling latency/cost limits are the
same as remote document watching; a deeply nested path whose intermediate parents do not exist yet
is picked up on a relevant root/parent event, otherwise the next Env: Reload/reconnect re-evaluates;
cancellation/no local fallback/caps preserved (R13-18).
**MRU in crash recovery and extraction (234).** Item 199 records it: RecoveryWriter sessions carry
Active + MRU (unsaved-doc refs, newest first) + FileState.Label so recoverCrash preserves the pre-
crash active tab and completion/dabbrev order; every extraction path writes the source MRU restricted
to moved docs with mergeExtractMru putting them before a dormant target's old refs and dropping a
same-path stale ref; materialized target keeps normal capture. Old sessions/recovery copies without
the fields restore exactly as before; stale/missing refs are skipped; no session Version bump
(additive optional fields) (R13-7/R13-1).
**Remote trash capability research (235; read-only, no code/docs changes).** VERDICT: safe remote trash
is NOT implementable on the current authority contract; keep permanent remote delete (explicit
confirmation) and mode/mtime preservation. Fresh's remote trash is not a safety proof: home trash +
create_dir_all + seconds timestamp + replacing rename, cross-device sibling fallback, no no-replace/
private-mode/device checks (its FS trait lacks them). Authority gaps: Rename is replacing by contract
(mv -f/os.Rename); Mkdir has no private mode/umask guarantee; FileStat/DirEntry have no device/owner
(SSH stat helper lacks them); SSH mv -n is non-POSIX/non-atomic and cross-FS copy+unlink can lose the
source on cancel; no renameat2/RENAME_NOREPLACE through the base surface. No trash/undo landed; the
existing permanent-delete residual and any future trash design remain OPEN/gated (R13-21).
Metrics: palette 240, settings 69 in 14 sections, bundled 26 (9 on)/17 off, plugin API 244 functions/
1489 lines level 2; i18n en 1046 / 579 per locale, 567 KiB text (under 640 KiB cap), ≈656 KiB JSON,
heap 118-82-201 KiB. Full suite EXIT 0 (76 packages, log /tmp/gvim-r33-final.log); real hosts
UNTESTED; no full-parity claim (residuals in §5 R13-1…R13-24 and §8).

**Round-32 outcome (2026-11-09, merged working tree on `9c1e936`).** Five tasks landed (225–229);
230 is this audit. Static gates pass; final full `go test ./... -p 1 -count=1 -timeout 1200s` EXIT
0: 76 packages ok, no FAIL (log `/tmp/gvim-r32-final.log`). The audit read the diff and ran targeted
tests; the Lead ran the full suite.
**Terminal OSC 7 cwd persistence (225).** A shell's OSC 7 report (plain path or file://host/path;
host dropped, percent escapes decoded) is cleaned and bounded before being kept: it must be a non-
empty POSIX-absolute path of at most 4096 bytes, valid UTF-8 and free of control characters,
otherwise the report is ignored (no cwd). The dock persists the last value in terminal.json (additive
tabs cwd field; version 1; launch_dir falls back to cwd for old records) and uses it for restart/
resume/dive starts (Fresh's respawn rule), with the saved transcript kept above the new shell.
Saved cwd is cleaned/bounded again on capture and load so a hand-edited record cannot smuggle
control characters/unbounded paths into a start. On a remote workspace a restart/dive with a saved
cwd verifies it through the authority file system (5 s bound, remote stat only — no local path read,
no shell probe); missing/non-directory/disconnected yields a clear status notice and the terminal
starts in the authority's default directory. Local starts keep existing behavior (missing local dir
is the starter's normal error). Residuals: only POSIX-absolute OSC 7 paths accepted (Windows-style
ignored); remote fallback is the authority default (SSH login home, not necessarily workspace root)
announced only in the status row; local saved dirs not pre-validated; remote check is a point-in-time
stat before launch (dir removed between check/PTY fails authoritative cd as start error); a remote
shell's later cd changes the saved cwd for next start but is not applied to live session files (no
remote terminal state replay) (R13-15).
**Remote delete stays permanent; copy mode/mtime best-effort (226).** The audited remote "trash"
design was WITHDRAWN and NOT shipped: no backend can prove an atomic same-filesystem no-replace move
(an mv -n may copy/unlink across filesystems or lose the source to cancellation, and its no-clobber
flags are not portable), and the authority has no private 0700 directory surface for a trash. Remote
delete therefore remains a permanent Mutations.Remove (recursive for a directory) with its explicit
confirmation that says it is permanent ("Remote deletes have no trash"); no trash directory, no
journal, no undo/restore UI, no recovery claim. Only the copy metadata slice landed: the source mode
is passed to the atomic file write and, on backends implementing authority.FileMetadata (local and
SSH), mode/mtime are applied to files then directories after the bytes complete, best effort. A
metadata failure is non-fatal and becomes a one-shot copy warning shown as a notice; a backend
without FileMetadata keeps the byte copy and claims nothing; per-entry Stat failures during planning
are tolerated. Not preserved: owner/uid/gid, hard links, xattrs; mode/mtime are best effort, not a
copy-fidelity guarantee. Gating residual for any future trash recovery: no portable atomic no-
replace move + no private-trash directory surface (R13-21).
**Pre-trust remote env marker probe (227).** Before the workspace is trusted the authority command
gate would refuse a shell command, so detection uses the app's shell-free authority probe:
authority.FileSystem.Stat for each marker/require path (no remote process, no local filesystem, at most
env.MaxMarkerProbePaths paths and a 10 s shared timeout, sequential Stat calls, cancellation through
the caller context). This lets an untrusted workspace display the remote detection result read-only;
activation remains trust-gated. A trusted workspace uses the single bounded cancellable sh -c
existence probe instead (markers + require evidence; fixed shell test, no rc sourced). A failed probe
clears detection and shows an explicit Settings notice (never a local scan or stale result) and is
retried on the next Warm/Overlay rather than cached; an untrusted path without the authority file
surface shows the explicit detection notice. Fresh's marker scan is a passive filesystem scan
available while trust is undecided; gvim now matches that read-only visibility through authority Stat
(the rest of the remote trust flow is unchanged). The trusted activation baseline/recipe still uses
the interactive login shell (bash -ilc/zsh -ilc where available) with sh -c fallback/notice (R13-18).
**Remote large-file windowed syntax highlighting (228).** Large-file highlighting remains windowed
for local and remote large files: the highlighter lexes a bounded window of lines around the lines on
screen and keeps a few windows, so scrolling inside a window lexes nothing and a new stretch lexes only
its own window; it never reads/highlights the whole file. For a windowed remote large file it reads
only lines whose 1 MiB source windows have already arrived: asking whether a line is available is a
pure cache check (Buffer.LineLoaded/Document.LineLoaded), and the first paint of an unloaded line
leaves it unstyled instead of turning the highlight into an extra remote read (the paint's own text
read is what records the window the root loads). When such a window arrives it changes the source's
window GENERATION without changing the text version; the highlighter keys its windows and long-line
segments by that generation, so the next repaint re-lexes them with the loaded text and scrolling the
same loaded window again performs no remote I/O. The pass is read-only: it never edits the document,
never changes the read-only flag of a binary remote large file, and cannot block on a remote read.
Servers still do not start for large-file buffers (local or remote windowed) (R13-15/R13-7).
**i18n extraction round-32 (229).** New English copy from the remote move confirmations/notices and
resolve multi-cursor refusal (plus the retired trash/move keys) is catalogued with English byte-
identical; budgets/docs and fallback/key-guard tests refreshed. Catalogue now en 1046 / de 579 keys,
567 KiB key+value text (under 640 KiB cap), ≈656 KiB embedded JSON, loader heap en 119 KiB / de 82
KiB / both 201 KiB (R13-5).
Metrics: palette 240, settings 69 in 14 sections, bundled 26 (9 on)/17 off, plugin API 244 functions/
1489 lines level 2; i18n en 1046 / 579 per locale, 567 KiB text, ≈656 KiB JSON, heap 119-82-201 KiB.
Full suite EXIT 0 (76 packages, log /tmp/gvim-r32-final.log); real hosts UNTESTED; no full-parity claim
(residuals in §5 R13-1…R13-24 and §8).

**Round-31 outcome (2026-11-09, merged working tree on `09ca1d2`).** Five tasks landed (219–223);
224 is this audit. Static gates pass; final full `go test ./... -p 1 -count=1 -timeout 1200s` EXIT
0: 76 packages ok, no FAIL (log `/tmp/gvim-r31-tests.log`). The audit read the diff and ran targeted
tests; the Lead ran the full suite.
**welcome demo reduced port (219).** New bundled Lua plugin welcome (off; no bindings/settings;
commands Plugin Demo: Open Help / Save File / Say Hello / Insert Time / Insert Comment; bundled set
now 26 plugins/9 on/17 off/59 commands; default palette/idle heap unchanged), port of Fresh
plugins/welcome.ts (78-line plugin-API demo), same five commands. Open Help/Save File dispatch native
show_help/save through gvim.command.run; Say Hello/Insert Time/Insert Comment insert at the primary
caret with b:insert (one undo, no auto-indent) and report status; time is local os.date %H:%M:%S;
comment prefix is language-aware (-- Lua/SQL, # Python/Ruby/shell/YAML/TOML/Makefile/Dockerfile, else
//); insert failures reported. Deviations: language-aware prefix vs fixed //, local 24 h HH:MM:SS vs
runtime-locale toLocaleTimeString, English-only vs 15 locales, no Fresh debug load status. Tests:
five command titles/no bindings; native action dispatch; greeting/time insertion text+status; Lua vs Go
comment prefix; read-only insert failure (R13-6).
**Remote browser drag/move (220).** A left press on a remote explorer row lifts the effective selection
(a multi-row selection moves whole); motion over a directory row (or a file's parent) underlines the
drop target; release emits MoveMsg and moves NOTHING yet. MoveMsg opens ONE confirmation naming the
entries (or count), remote target and destination with Move/Cancel; only the Move answer builds the
drag job chain (jobMove, cancel conflict policy, multi flag, completed count). Sources whose
destination is their own parent/self/a descendant are filtered, no-op drops send nothing, a drag never
touches the clipboard, and only the remote authority is used. After confirmation every move starts
with the cancel conflict policy, so a taken destination goes through the existing single/multi conflict
dialogs and is overwritten only on an explicit choice. The move is MoveAs's copy-then-remove (complete
copy first, then Mutations.Remove): a failure/cancellation in the copy phase keeps the source and any
partial destination, and a failed source removal keeps both and reports it. Local workspaces do not
enable the drag (notice points at Cut/Paste); the same keyboard Cut/select/Paste flow exists locally
and remotely with the same confirmation/conflict/job machinery. Drop target is underlined only while
the drag is held; no animation/preview beyond that. Remaining: no cross-window/above-root target, no
remote undo/history, no Fresh resolve-then-run batch (incremental job chain, so a later Cancel/Skip
cannot roll back already-moved entries; one batch-level Move/Cancel confirmation; drag leaves a pending
cut untouched) (R13-21).
**Trusted remote env marker probe (221).** Marker detection is trust-gated: only a trusted workspace
may run the one bounded, cancellable sh -c existence probe (it checks both the detectors' markers and
their require evidence; nothing beyond the fixed shell test executes and rc files are not sourced).
An untrusted workspace runs no remote command, shows an explicit Settings detection notice and no
detector. A failed/cancelled probe clears the detection and leaves a notice (never a local scan or a
stale result) and is retried on the next Warm/Overlay rather than cached. Fresh's marker scan is a
passive filesystem scan used while trust is undecided; gvim's remote probe needs the remote command
gate, so before trust the remote detector is simply absent (the rest of the remote trust flow is
unchanged). Trusted activation baseline/recipe still uses the interactive login shell (bash -ilc/zsh
-ilc where available) with sh -c fallback/notice (R13-18).
**MRU session persistence (222).** The editor's document-identity MRU (r24) is now saved in the session
as an additive optional Session.MRU []MruFile, where MruFile is {Path,Label}: the saved path of a named
document or the label of a pathless one. It is deliberately not a tab index or runtime identity, so a
reference cannot point at a different document after restart; session Version stays 1 and an older
session without the field keeps the restore-derived order. Restore maps the references back to the new
documents BEFORE multi-buffer completion/dabbrev read them; a reference that is missing, renamed away,
closed or no longer open is skipped while the remaining references keep their relative order (a stale
session cannot add a phantom buffer or reorder the others). Includes daemon workspace
eviction/restore; tests cover restart/recovery order and stale/renamed/closed references (R13-7/R13-1).
**i18n extraction round-31 (223).** The round-29/30 new English copy is catalogued with English
byte-identical: finder degradation notices, batch-paste counts/stop messages, resolve multi-cursor
refusal notices, weather status rows, plus the dashboard plugin's own i18n catalogue. Budgets/docs and
fallback/key-guard tests refreshed. Catalogue now en 1031 / de 579 keys, 565 KiB key+value text (under
640 KiB cap), ≈655 KiB embedded JSON, loader heap en 117 KiB / de 82 KiB / both 199 KiB (R13-5).
Metrics: palette 240, settings 69 in 14 sections, bundled 26 (9 on)/17 off, plugin API 244 functions/
1489 lines level 2; i18n en 1031 / 579 per locale, 565 KiB text, ≈655 KiB JSON, heap 117-82-199 KiB.
Full suite EXIT 0 (76 packages, log /tmp/gvim-r31-tests.log); real hosts UNTESTED; no full-parity claim
(residuals in §5 R13-1…R13-24 and §8).

**Round-30 outcome (2026-11-09, merged working tree on `0712aa9`).** Five tasks landed (213–217);
218 is this audit. Static gates pass; final full `go test ./... -p 1 -count=1 -timeout 1200s` EXIT
0: 76 packages ok, no FAIL (log `/tmp/gvim-r30-tests.log`). The audit read the diff and ran targeted
tests; the Lead ran the full suite.
**Opt-in dashboard weather (213).** With plugins.dashboard.settings.weather=true (and the host network
gate settings.network=true; project layer never grants), optional weather_location (a place name or
coordinates; empty asks wttr.in to use the host's public IP) and weather_units=metric|imperial, the
dashboard makes one bounded `https://wttr.in[/<percent-encoded location>]?format=j1` request (5 s
timeout, 10 min refresh TTL) through the restricted gvim.http capability. Rendered as Fresh: “now”
condition/temperature/feels-like/wind/humidity, “5pm” from the nearest 3-hour sample, and a
“tomorrow” range/condition summary. No credentials or caller headers, body stays in memory (no disk
write), never blocks the UI; timeout/offline/malformed body/disabled network renders one muted
unavailable/invalid response/network disabled line with no notice. Third-party section API remains
absent; reductions: no multi-day detail beyond today/tomorrow and no dedicated location picker (plain
string; empty delegates geolocation to wttr.in) (R9-1/R13-6).
**Terminal exit marker at the exit instant (214).** The `[Terminal process exited]` marker is no longer
appended only at save: it becomes the transcript's last row AT THE EXIT INSTANT as a synthetic row
while the frozen screen is still on show, so the scrollback pager (Ctrl+Space) shows it immediately and
it can be selected/copied without saving first. A save stores it as an ordinary bounded transcript row;
a restart captures it before the new shell, so restart/resume continues BELOW it and never overwrites
output. Tab title still carries the status; gvim keeps the frozen last screen in its live view on exit
(Fresh switches that buffer to read-only scrollback), so the marker is visible in the pager at the exit
instant but not painted on the frozen live screen; an alternate-screen exit has no pager (“The
alternate screen has no scrollback.”) though the marker row is persisted for restore. Old sessions keep
their behavior; exactly-once/no duplication across save/restart/restore (R13-15).
**Remote env-input file watching (215).** The authority's own file watch now watches .envrc, mise.toml,
.mise.toml, .tool-versions, pyvenv.cfg and the .venv directory: a change invalidates the cached
detection/capture and re-warms it in the background (no local filesystem consulted). The watcher arms
the workspace root plus existing nested input directories (.venv), so a content-only change to
.venv/pyvenv.cfg is seen even when the root directory entry does not change; when a nested directory
appears, the next root event re-arms the watcher with it. A backend whose file surface cannot watch
degrades safely: the watcher stops, Settings Environment rows say the inputs are not watched and point
at Env: Reload, and manual reload keeps working; other watch failures retry with the same bounded
backoff as the remote document watch. Detection is cached until Refresh/SetRoot or an env-input event;
invalidation is asynchronous (an in-flight spawn can use the previous captured delta until warm);
Settings rows show remote facts only after the background warm; no status-row active remote-
environment indicator yet; local unchanged (R13-18).
**Resolve multi-cursor completion (216).** When the accepted item has a ResolveToken and the active
document has extra carets, acceptance snapshots the document version and every caret, closes the popup
so the resolve never blocks the update loop, and applies the resolved candidate only if the active
document, its version and every caret are still exactly that snapshot. The reply then goes through the
same per-caret atomic path, with the resolved snippet stop and additionalTextEdits included: one atomic
edit/one undo, request version guard. An edit, a moved/added/collapsed caret, a switch to another
document, a failed resolve, or a missing version/editor capability refuses the whole accept with a
notice and writes nothing. Single-caret path unchanged (R13-7).
**Remote batch-paste conflicts: Skip/Skip All/Overwrite All (217).** A multi-item remote paste conflict
now opens Fresh's dialog Overwrite (o) / Overwrite All (O) / Skip (s) / Skip All (S) / Cancel (c, Esc)
through a Choice overlay. Overwrite handles the current item and asks about the next conflict;
Overwrite All applies the overwrite policy to every remaining job of that paste (no further prompts);
Skip skips only the current item and asks about the next; Skip All skips current and all remaining
items and stops (a cut paste also drops those stranded sources from the clipboard, like Fresh's
finished batch); Cancel stops current and remaining jobs, keeps already-completed files and leaves the
clipboard for a retry. Stop notices count completed items; a later non-conflict failure stops the
remaining jobs, keeps completed/partial results and appends the count to the error notice. Rename
remains a single-item prefilled “Paste as on SSH:…” CopyAs/MoveAs; Overwrite still removes the existing
destination first and copies (cut source removed only after success); the adapter's Skip leaves the
destination/reports the path and returns the source for cut. Local Overwrite/Skip prompt unchanged;
Duplicate stays a same-directory free-name copy. Recorded divergence: Fresh resolves the whole multi-
conflict queue first and runs the batch only then, so its Cancel executes nothing; gvim runs the chain
incrementally, so a later Cancel/Skip All keeps items already pasted (notices count them; no hidden
undo). Overwrite All applies only to the remaining jobs of that one paste and is not remembered for a
later paste; remove-then-copy remains no-rollback (a re-created destination between removal and copy is
a documented race; file copy atomically replaces it, directory copy fails at Mkdir); no local fallback
(R13-21).
Metrics: palette 240, settings 69 in 14 sections, bundled 25 (9 on)/16 off, plugin API 244 functions/
1489 lines level 2; i18n en 1015 / 579 per locale, 564 KiB text (under 640 KiB cap), ≈654 KiB JSON,
heap 110-82-193 KiB. Full suite EXIT 0 (76 packages, log /tmp/gvim-r30-tests.log); real hosts
UNTESTED; no full-parity claim (residuals in §5 R13-1…R13-24 and §8).

**Round-29 outcome (2026-11-09, merged working tree on `a6b49f2`).** Six tasks landed (206–210,212);
211 is this audit. Static gates pass; final full `go test ./... -p 1 -count=1 -timeout 1200s` EXIT
0: 76 packages ok, no FAIL (log `/tmp/gvim-r29-tests.log`). The audit read the diff and ran targeted
tests; the Lead ran the full suite.
**Kitty child-protocol corrections (206).** A real-PTY regression audit found/fixed four deterministic
gaps in the task-203 encoder: (1) UV `ModMeta` (XTerm meta bit) now maps to protocol Super (bit 8)
and UV `ModSuper` (Windows/Command) to protocol Meta (bit 32), correcting the reversed mapping;
(2) functional-key coverage extends beyond cursors/F-keys to lock, media and keypad keys, and under
report-all-keys modifier-only keys are reported (F13–F35 use protocol numbers; keys with no protocol
number or beyond F35 stay legacy); (3) Shift inference for a legacy-compatible uppercase key now
happens with other modifiers too (Alt+Shift+A keeps Shift), matching Fresh's normalize_key; (4)
push/pop edges: an omitted push flags value defaults to zero, a full stack evicts its oldest entry,
and popping past the bottom resets the flags. Real PTY tests cover the encoding; presses only/no
release event-type, alternate-key field carries the shifted key but no separate base-layout key, and
host-terminal kitty negotiation stays Bubble Tea's job (residuals/limits matching Fresh) (R13-15).
**Post-reconnect remote shell re-probe (207).** After every successful authority reconnect the app
re-probes the interactive shell in the background off the UI update loop and refreshes the fallback
state/notice: a probe that failed at startup is no longer pinned to `sh -c` for the session, and a
later successful probe clears the Settings fallback row. The re-probe is the same fixed pass-through
command through the authority gate (an untrusted workspace refuses remote commands, so the fallback/
notice reflects that) and the session context bounds it. It updates the shell choice and Settings
fallback row but not the already-shown startup notice, and does not re-detect env markers or
recapture the overlay (Env: Reload or a new activation recaptures); a cancelled/disconnected probe
leaves a fallback notice reflecting that attempt. Changed env files remain unwatched, detection is
cached until Refresh/SetRoot, Settings rows appear after the background warm, and there is still no
status-row active remote-environment indicator; local unchanged (R13-18).
**Dashboard GitHub PRs safely clickable + keyboard reachable (208).** PR rows are dashboard action
rows (Tab/Down/Up focus, Enter activates) and publish dashboard-owned action line targets
`dashboard.click_pr_<n>`; both paths dispatch a hidden dashboard command and then the existing
gvim.url.open (http(s)-only scheme/credentials/control policy + modal Yes/No confirmation, no shell
opener). The URL is NOT taken from untrusted REST data: it is rebuilt as
`https://github.com/<owner>/<repo>/pull/<number>` from the git-remote-derived slug and a strictly
validated positive integer number (1..999999999; string/fractional/negative/huge/missing skipped;
html_url and other API links ignored). Titles: string-only, control chars→space, whitespace collapsed/
trimmed, empty/non-string→(untitled), 200-rune truncation; draft only == true; at most 200 entries
examined and 6 valid rows kept; opt-in/network/unavailable unchanged. Tests: targets+click+keyboard
open canonical URL; untrusted fields ignored/invalid numbers/titles; 6/40 cap. Residuals: only
canonical github.com/<slug>/pull/<n> accepted (API/cross-repo/enterprise links not used), title
normalization/truncation, no author/label/review fields, confirmation per open, fixed 6/200 limits
(R9-1/R13-6).
**Remote finder degradation notice (209).** `finder.Index` gained a non-fatal `Notice()` and the
enumeration callback an additive `StartFuncNotify` seam (existing Start/StartFunc/startWith behavior
unchanged). In a remote git work tree, a git probe/listing failure or a truncated listing still runs
the recursive fallback walk and now carries one explicit non-fatal English notice (“Remote quick-
open: git could not run …” / “… git file list failed or was truncated …”); a plain non-git
workspace (Fresh walk scope) and the nested-repository fallback are not degradations and carry no
notice. The notice is generation-guarded, cleared when a run starts and on Restart (a reconnect/
refresh never shows a stale one); Quick Open shows it as one informative row after the results without
hiding or disabling them. A root read failure of the fallback walk remains the index error. Residuals:
fallback notice’s English literal awaits the next i18n extraction; fallback paths still use the
client-side remote ignore reader; nested contents only via fallback; non-UTF-8 omitted; truncated
listing falls back rather than keeping a partial one; no local fallback (R13-8/R13-3).
**Plugin HTTPS IPv6-transition SSRF fix (210).** The read-only security audit found a deterministic
SSRF gap: IPv6 transition/embedding forms could encode a private IPv4 target for a translator/relay.
The policy now refuses as a whole: site-local `fec0::/10`, NAT64 `64:ff9b::/96` and `64:ff9b:1::/48`,
6to4 `2002::/16`, Teredo `2001::/32`, and deprecated IPv4-compatible `::/96`; DNS every-address-public
validation, literal dial, redirect revalidation, proxy removal and TLS 1.2 floor are unchanged.
Real security regressions + docs cover the forms. Remaining HTTP policy: only HTTPS GET/no generic
fetch/auth/token/cache, safe response headers only, fixed 5 s/1 MiB/2+4 limits (R13-6/R13-24).
**Terminal title combination (212).** The tab label now follows Fresh's `combine_terminal_title`: an
explicit rename wins; otherwise the recorded launch program (argv[0]/agent — Fresh's “command leads”
half) and the recorded/live OSC title are combined as `command — title` (em dash), except when the OSC
title already names the command case-insensitively (e.g. vim's “file - VIM”), which is used alone;
either part alone is used as-is and both empty falls back to Terminal N. Untrusted titles are cleaned
again before painting/combining; record format stays v1, old readers ignore the title, old records
without a title keep exactly their old command/default label, and corrupt/overlong titles are cleaned
again on load. OPEN: combination uses the recorded launch program, not Fresh's live foreground-process
name (gvim keeps no pty foreground cache, so a program that later starts a different foreground
process inside the shell is not reflected) (R13-15).
Metrics: palette 240, settings 69 in 14 sections, bundled 25 (9 on)/16 off, plugin API 244 functions/
1489 lines level 2; i18n en 1015 / 579 per locale, 564 KiB text (under 640 KiB cap), ≈654 KiB JSON,
heap 115-82-198 KiB. Full suite EXIT 0 (76 packages, log /tmp/gvim-r29-tests.log); real hosts
UNTESTED; no full-parity claim (residuals in §5 R13-1…R13-24 and §8).

**Round-28 outcome (2026-11-09, merged working tree on `66f8f69`).** Six tasks landed (199–204);
205 is this audit. Static gates pass; final full `go test ./... -p 1 -count=1 -timeout 1200s` EXIT
0: 76 packages ok, no FAIL (log `/tmp/gvim-r28-tests.log`). The audit read the diff and ran targeted
tests; the Lead ran the full suite.
**Restricted plugin HTTPS GET + GitHub PR dashboard section (199).** `gvim.http.get(url, opts?)`
is a bounded HTTPS GET with a host-enforced user opt-in: the plugin's USER config layer must contain
`plugins.<name>.settings.network = true` (project layer never grants it), otherwise `unsupported`
before any DNS/request. Only https; credentials refused; SSRF policy refuses localhost/.local/
loopback/private/link-local/multicast/unspecified/CGNAT/documentation·benchmarking literal addresses,
resolves a DNS name host-side and requires EVERY resolved address public (DNS failure final, no
fallback/other resolver), dials a validated literal, ignores environment proxy, TLS 1.2 floor.
Redirects ≤3 and every hop revalidated (http/local/private fails); timeout default 5 s (100 ms–10 s
clamp); body ≤1 MiB (Content-Length precheck + limited reader); ≤2 concurrent/plugin and 4 global;
body memory-only never disk. Callers cannot set request headers (fixed User-Agent gvim-plugin/<version>
+ Accept application/json, no Authorization/Cookie); only safe response headers returned (content-type/
length/etag/last-modified/cache-control/link/x-ratelimit-*). Awaitable Work never blocks the update
loop; failures nil,err (invalid/timeout/too_large/io/unsupported). Dashboard opt-in
plugins.dashboard.settings.github_prs=true + network=true: a GitHub origin makes one bounded
https://api.github.com/repos/<owner>/<repo>/pulls request rendered as up to six open PRs; without a
token a private/missing repo 404 → muted “unavailable” line (rest of dashboard unaffected), failures/
disabled also one muted line. Weather is opt-in (r30; one bounded wttr.in j1 request through the restricted HTTPS capability); third-party section API still absent; no generic
fetch/auth/token/cache (R13-6/R13-24).
**Remote environment interactive-login capture (200).** Marker detection stays a passive `sh -c` probe
(may run in an untrusted workspace, so it must not source an interactive rc from a project cwd). The
trusted activation baseline/recipe runs under the user's interactive login shell when available —
`bash -ilc` or `zsh -ilc`, preferring `$SHELL` when it is bash/zsh — so a detector depending on
interactive rc initialization captures the same environment the remote terminal gets; if no such
shell is found or the interactive invocation fails, capture falls back to the previous `sh -c` path
with an explicit notice (startup notice and Settings Environment rows). Startup output before the
command is discarded via a NUL marker; one remote command per baseline/recipe capture. Residuals:
bash/zsh only (fish/csh etc keep sh -c), rc unavailable/fails falls back per capture with notice,
detection stays sh -c so semantics can differ, changed env input files are not watched (Env: Reload or
new activation recaptures), detection cached until Refresh/reconnect, Settings rows show remote facts
only after background warm, no status-row active remote-environment indicator; local unchanged (R13-18).
**Remote finder git candidate index (201).** In a remote git work tree the quick-open index fills from
Fresh's project-file list in ONE remote listing traversal: one bounded `git rev-parse --is-inside-work-
tree` + one bounded `git ls-files -z -s --cached --others --exclude-standard` (8 MiB/20 s,
authority one-shot) fed through the additive `finder.StartFunc`, replacing recursive per-directory
ReadDir and the client-side remote .gitignore reader on that path. Lists tracked files (including
tracked-ignored) + untracked non-ignored; emit filters .git internals/hidden segments/intermediate skip
dirs/invalid or non-UTF-8 names/remote depth bound; keeps the 50k cap, truncation and cancellation
semantics; every Restart/refresh re-runs the remote git listing (new carrier/work tree picked up).
No-git-work-tree, git missing/failing/truncated, or nested repo/submodule listed by name only fall
back to the previous recursive authority walk (bounded dirs/depth, skip dirs/hidden, no symlink
traversal, remote .gitignore reader) rather than dropping files; a root read failure is the index
error. Residuals: Quick Open's default hidden filter still omits tracked hidden files; fallback paths
still use the client-side remote ignore reader; nested repo contents appear only via fallback; non-UTF-8
omitted (Fresh too); truncated git listing falls back rather than keeping a partial one; no local
fallback (R13-8/R13-3).
**Terminal OSC title + alternate-screen restore (202).** A child's OSC 0 (icon+window title) and OSC 2
(window title) set the session title mirroring Fresh's Event::Title; last one kept, control chars/
invalid UTF-8 dropped, trimmed, bounded 256 runes, empty payload clears; OSC 1 only sets the icon name
and deliberately does not change the title. Title is additive in the saved record (per tab + mirrored
top level), restored tab keeps its recorded title and re-saves it; old records have no title and keep
derived label; label precedence explicit rename > launch program/agent name > recorded/live OSC title >
Terminal N; version 1, corrupt/overlong cleaned on load. Alternate-screen CONTENT is captured
first-class as the visible frame (styles/cursor, no history); a corrupt record carrying both alt and a
history window is ignored whole; restored read-only transcript paints the alt frame unchanged; a
revived shell starts on its own main screen, not replaying the alt program; alt-only restore as
read-only frame; main history while alt active is not captured; bounded by snapshot limits. Residuals:
OSC 1 icon names, OSC 7 cwd, OSC 52 clipboard and hyperlinks are not replayed; per-cell hyperlinks not
representable; read-only cursor block; cursor shape not inherited; recorded title not recombined with a
foreground-process name (no pty foreground cache); live tabs still need a dive (R13-15).
**Child-side kitty keyboard protocol (203).** The emulator answers the child's `CSI ? u` query with the
flags the child pushed and tracks `CSI > flags u` push, `CSI < n u` pop and `CSI = flags ; mode u` set/
add/remove in separate flags stacks for main/alternate screens (bounded 64 entries, partial 128 bytes;
sequences may split across PTY reads; unknown CSI/u ignored). While flags are nonzero the dock encodes
keys sent to that child in CSI-u form: disambiguation, Esc, modified printable keys, modified Enter/
Tab/Backspace, Ctrl+I/Alt+[ etc.; arrows use CSI A/F3 CSI 13~ (not DECCKM SS3); alternate-keys/
report-all-keys/associated-text add fields. Unmodified text/Enter/Tab/Backspace keep legacy bytes; no
flags or popped all = old legacy encoding. Limits matching Fresh: presses only (no release events even
with report-event-types), modifier-only/media keys and protocol F13+ not encoded; host-terminal kitty
negotiation stays Bubble Tea's job. Flags live in the session, reset on restart, per-screen restored
when returning to main. No keybinding change; child TUIs get the protocol (R13-15/R13-6).
**clangd_support reduced port (204).** New bundled Lua plugin clangd_support (off; no bindings/
settings; commands Clangd: Switch Source/Header / Project Setup / Open Project Config; bundled set
now 25 plugins/9 on/16 off/54 commands): bounded same-stem C/header lookup (c/cc/cpp/cxx ↔ h/hh/hpp/
hxx/hp) in buffer dir/workspace root/include//src children, one opens/many gvim.ui.select; project
setup analysis (compile_commands.json root/build/out/cmake-build-debug, .clangd/compile_flags.txt,
CMake/Bazel/Makefile hints/general tips) in reused read-only virtual buffer; Open Project Config finds
.clangd in buffer dir/workspace root or reports missing. Unported: no textDocument/
switchSourceHeader (LSP allowlist; heuristic workspace-local same-stem only), no clangd fileStatus/
$/memoryUsage custom notifications (no plugin server-custom-notification event/status line), virtual
buffer not floating PanelManager panel, no ancestor .clangd walk. Tests recorded (R13-6).
**Test lifecycle fix.** The full suite first exposed TestUserPluginWorksEndToEnd failing at TempDir
cleanup (“directory not empty”): the test session did not wait for background config/state/dock/plugin
stopping. The test lifecycle helper now closes the Model first and waits for saves/dock/plugin stop
before TempDir cleanup; targeted count=50 + app count=2 PASS, then full suite EXIT 0. This is test-
infrastructure only, not a product parity change.
Metrics: palette 240, settings 69 in 14 sections, bundled 25 (9 on)/16 off, plugin API 244 functions/
1489 lines level 2 (gvim.http.get added); i18n en 1015 / 579 per locale, 564 KiB text (under 640 KiB
cap), ≈654 KiB JSON, heap 110-82-193 KiB. Full suite EXIT 0 (76 packages, log /tmp/gvim-r28-tests.log);
real hosts UNTESTED; no full-parity claim (residuals in §5 R13-1…R13-24 and §8).

**Round-27 outcome (2026-11-09, merged working tree on `77c3ec7`).** Eight tasks landed (190–195,
197,198); 196 is this audit. Static gates pass; final full `go test ./... -p 1 -count=1 -timeout
1200s` EXIT 0: 76 packages ok, no FAIL (log `/tmp/gvim-r27-tests.log`). The audit read the diff and
ran targeted tests; the Lead ran the full suite.
**csharp_support reduced port (190).** New bundled Lua plugin (off; no bindings/settings; commands
C#: Restore Project / Show LSP Help; bundled set now 24 plugins/9 on/15 off/51 commands): nearest
.csproj/.sln walk (20 levels, fallback file dir) on .cs open; trust-gated dotnet restore once per
project/session through gvim.proc.run (authority-following), deferred on trust_changed; status
restoring/completed/failed; csharp-ls install help on lsp_server_error only for an active C# buffer +
error text naming csharp/c#/roslyn/omnisharp (payload only {error}), copy install command + native
lsp_restart/show_lsp_status. Unported: setLspRootUri (no plugin API), Roslyn
workspace/_roslyn_projectNeedsRestore server request (no plugin event; open-time restore covers the
common case), Disable C# LSP action (native LSP Stop/Disable), dotnet --version precheck replaced by
direct run + exec error report, i18n English-only. Tests recorded (commands/no bindings, nearest
restore once/project, untrusted→trust_changed, no-project fallback, failure/stderr, missing dotnet,
help popup/copy/native restart, non-C# ignored) (R13-6).
**asm_lsp exclusive project config (197).** Fixed the config check-to-write race: gvim.fs.write(path,
data, {exclusive=true}) is a create-new (local O_EXCL; remote authority.FileCreator.CreateNew then
write; append/parents invalid; backend without FileCreator → unsupported, no racy Stat fallback).
asm_lsp now creates the config exclusively after the popup, reports “Not overwriting <path>: it was
created by another process” (or explicit create-refused) and never runs lsp_restart on refusal;
TestAsmLspExclusiveCreateDoesNotOverwrite + io_fs/pluginhost local+authority tests. Residuals: local
direct O_EXCL write is not temp+rename (crash mid-write can leave partial content); remote CreateNew
then a non-streamed/non-atomic WriteFile (failure removes the claimed file only when authority
mutations exist, else leaves an empty file); exclusive is once-only create (no append/parents); asm
config residual list otherwise unchanged (R13-6).
**Multi-cursor snippet/auto-import completion (191).** With extra carets, accepting a server item that
expands a snippet or carries additionalTextEdits now applies the same per-caret rule: every caret
replaces its own word prefix with the expanded snippet text, the final stop is a RUNE offset into
that text (each caret lands inside its own insertion), and auto-import additionalTextEdits are
applied exactly once in the same atomic edit as the per-caret inserts. The combined edit list is
validated first: inverted/overlapping ranges, an insert strictly inside a replaced range, or two
zero-width inserts at the same point refuse the whole accept (ErrOverlappingEdits) and write
nothing; one undo step and the request version guard apply. A server item needing
completionItem/resolve still refuses with extra carets (edits arrive later; per-caret positions
cannot be guaranteed). One caret path unchanged. Residual: resolve-based accepts remain single-
caret (R13-7).
**Remote paste conflict flow (192).** Remote paste/duplicate now uses the explorer conflict choice
instead of blindly refusing: Cancel reports explorer.ErrConflict and changes nothing (Esc also drops
the rest of a multi-item paste); Rename opens a prefilled “Paste as on SSH:…” name prompt and retries
through CopyAs/MoveAs with the destination name (validated, same policies); Overwrite removes the
existing destination first and then copies (a cut removes its source only after a successful copy);
Skip leaves the destination/report path (cut returns the source, cancelling the cut). Local
Overwrite/Skip prompt unchanged; Duplicate stays a same-directory Fresh free-name copy (1000 tries)
and never prompts. A write-phase cancellation/I/O failure keeps the partial destination and reports
how many files/where; gvim never deletes remote data on a failed copy. OVERWRITE IS REMOVE-THEN-COPY
WITH NO ROLLBACK: a process that re-creates the destination between the removal and the copy is a
documented race (the file copy atomically replaces the re-created file because the user chose
overwrite; a directory copy fails at Mkdir instead of silently replacing it), and the conflict
decision is not remembered across a multi-item paste. Fresh's Overwrite All/Skip/Skip All are not
implemented (bounded per-item Overwrite/Rename/Cancel). No local fallback (R13-21).
**Mixed file+terminal tab extraction (193; gvim extension).** The palette “Extract tabs to
workspace…” picker now lists open file tabs AND dock terminal tabs, type-labelled (“file”, “file ·
modified”, “terminal”). Enter opens a target chooser: first row NEW co-tenant over the same root,
then every existing same-root workspace; with no existing target the new co-tenant is created
directly. A new target writes one session (all FileStates + stashes) and one terminal record (all
SavedTabs) before the switch is queued, then closes source file tabs by path and source terminal tabs
by identity (highest index first, saved-name checked) only after the switch was accepted, re-captures
the source and reports “Extracted N tabs into workspace”. For an existing target a READ-ONLY
conflict pass detects selected files already present in the target's session/open editor and
non-empty terminal tab names already in its terminal record/dock, sends ExtractConflictMsg BEFORE any
write; confirmation names the target and every conflict: Yes replaces the target's existing file
state and ADDS a same-name terminal tab alongside (never replaces it), No cancels with target
records/source tabs untouched. After acceptance files/terminal records are transferred (dormant
merge or warm editor/dock append), then switch queued/accepted, then sources close; an acceptance
failure stops with “N of M tabs transferred, sources kept”; a source tab changed meanwhile is skipped
and counted in the report. Ordering transfer accepted → queued switch accepted → source close; Update
does no I/O (capture UI goroutine, disk/materialization tea command, conflict detection command).
Residuals: large-file overlay/welcome/closed-during-capture refuses whole action; co-tenant-only
(other-root target refused); no PTY handed over (target revives on first dive like terminal action);
empty saved-name terminal tabs are outside the name-conflict check and can duplicate names; no undo/
drag-and-drop target/cross-workspace shared buffers; Fresh itself only extracts a single tab, so mixed
selection is a gvim extension (R13-14).
**Real SSH keychain backend (194).** With ssh.keyring on (default off), SystemPasswordStore()
uses macOS /usr/bin/security (find/add-generic-password, service gvim-ssh, account user@host; argv,
no shell; password is the -w argv value and briefly process-visible, recorded limitation) or Linux
secret-tool over Secret Service/DBus (lookup/store; password on stdin, never argv; needs running
keyring/DBus, honest failure otherwise). The tool is used only when exec.LookPath finds it; otherwise
the no-op store and a store attempt reports “not stored in the OS keyring” (never success). Lookup
before the prompt; store only after a user-supplied accepted password; errors queued honestly;
invocations argv/no shell, bounded 64 KiB, inherit env, honor dial context; one entry per user@host
(no port; resolved alias host), trailing newline stripped/other bytes preserved (embedded/trailing LF/
CR included; tool round-trip limitation). Passphrases and 2FA/keyboard-interactive answers are never
keyring-stored. Tests use a fake command runner (no real keychain/DBus); real hosts UNTESTED
(R13-17).
**Configurable update command in a dock terminal (195).** New user-layer-only config update_command
(gvim extension; project setting refused; trimmed, max 4096 bytes, no NUL). When set, the palette's
Update fresh (update_fresh) and Show update output (open_update_log) rows appear; otherwise hidden
and the raw actions keep the “gvim does not update itself” notice. Update fresh runs the command in
a new terminal dock tab named “Update” through the user's shell: $SHELL/-c on Unix (or /bin/sh) and
cmd.exe /C on Windows, so flags/shell syntax work. The tab keeps output after exit; a second Update
fresh restarts that same tab and keeps the previous transcript above the new run; Show update output
returns to it (before any run: no output yet). Start/dock failure is reported on the status row/dock;
non-zero exit stays visible and does not affect the editor. Independent of check_for_updates; bounded
command; no Settings-page row (user preference, not display/editing option); runs on the machine
running gvim, not a remote authority; with terminal.close_on_exit the exited tab is removed so output
is not kept (default keeps read-only scrollback). gvim has no download/install/self-update engine;
this is a bounded equivalent, not Fresh's updater (R13-12).
**i18n remainder 6 (198).** New pure-English strings moved into the catalogue with English
byte-identical: update_command missing/running/no-output/dock-failed notices, gvim.url.open
confirmation/unsafe/cancelled/error/opened, terminal restart/resume labels + signal suffix, palette
preview degraded notices, asm_lsp config-refused copy (gvim-only fallback). docs/i18n.md and budgets
refreshed; de fallback/key-guard tests. Catalogue now en 1015 / de 579 keys, 564 KiB key+value text
(under 640 KiB cap), about 654 KiB embedded JSON; loader heap en 116 KiB / de 82 KiB / both 198 KiB
(R13-5).
Metrics: palette 240, settings 69 in 14 sections, bundled 24 (9 on)/15 off, plugin API 243 functions/
1483 lines level 2; i18n en 1015 / 579 per locale, 564 KiB text, ≈654 KiB JSON, heap 116-82-198
KiB. Full suite EXIT 0 (76 packages, log /tmp/gvim-r27-tests.log); real hosts UNTESTED; no full-
parity claim (residuals in §5 R13-1…R13-24 and §8).

**Round-26 outcome (2026-11-09, merged working tree on `507e6f4`).** Five tasks landed (184–188);
189 is this audit. Static gates pass; final full `go test ./... -p 1 -count=1 -timeout 1200s` EXIT 0:
76 packages ok, no FAIL (log `/tmp/gvim-r26-tests.log`). The audit read the diff and ran targeted
tests; the Lead ran the full suite.
**Safe URL opening + dashboard links (184).** `gvim.url.open(url)` opens an absolute http(s) URL
in the user's desktop browser. Only http/https are accepted; every other scheme (javascript:,
file:, data:, ssh:, mailto:, …), credentials, whitespace/control characters, a hostless/opaque
form and a URL over 2048 bytes are refused with `invalid` BEFORE any prompt. An accepted URL shows
a modal Yes/No confirmation naming the URL (keyboard y/n, arrows and Enter, Esc; focus starts on
No); `gvim.url.open` returns once the question is queued, only Yes starts the opener. The platform
opener (macOS `open`, Linux `xdg-open`) receives the URL as one argv element — no shell, command not
configurable — runs off the update loop, reports a start failure on the status row, and returns
`unsupported` on a platform without an opener. In a remote workspace the browser is local (the URL
opens on the machine running gvim). Dashboard: a GitHub origin (scp-like, ssh:// or https form) is
normalized to `https://github.com/owner/repo`; repo and branch lines become clickable through
dashboard-owned action targets and keyboard-equivalent Open Repository/Open Branch rows are appended
to Actions (branch URL `<repo>/tree/<branch>`); clicking/activating calls gvim.url.open. A non-GitHub
https origin is openable as a repo row but has no branch URL; unsafe/unopenable origins stay plain
text. Residual: only http(s), no non-GitHub branch URL, opener is not configurable/user-extensible;
weather opt-in landed r30 (wttr.in, muted failures); other GitHub/network sections/third-party API/dock/mouse/sessions remain OPEN (not closed by no-network/dependency; icon/style differences acceptable) (R9-1/R13-6).
**Exited-terminal status-bar restart indicator (185).** `{terminal_restart}` is painted in the
status bar: `⟳ Resume <program>` when `terminal.resume_agents` is on and the record has a non-empty
resume argv, `⟳ Restart <program>` for a launch argv, or `⟳ Restart terminal` for a plain shell, plus
` (exit N)` only for a non-zero code (exit 0 omitted, as Fresh does) or ` (signal)` for a Unix signal.
The program is the basename of the resume (preferred) or launch argv's program. The element appears
for both a live exit and a restored exited tab, uses the theme's error foreground, is underlined+bold
while hovered, and disappears once the shell is restarted; its click is the existing
`restart_terminal` action, and the keyboard equivalent stays palette/menu “Restart Terminal Process”.
A narrow status row follows the frame's squeeze rule (label dropped/clipped with its group, never
overflowing). Divergences: the program name comes from the recorded launch/resume argv, not the OS
foreground process; hover is underline+bold rather than Fresh's ERROR-colour swap; signal exits append
` (signal)` because gvim records -1 where Fresh's i32 exit code has no signal field; and the
transcript marker is appended at capture/save/restart rather than at the exit instant because the
frozen screen stays on show (R13-15).
**Remote preview syntax highlighting (186).** The remote preview now carries syntax spans from the
same shared highlighter as the local preview: up to 256 KiB and 4000 lines, one cached highlighter
per file in an 8-entry bounded cache safe for concurrent preview commands, CPU-only inside the
preview command and with no extra remote I/O. An open-buffer preview is highlighted from the panel's
text; a remote head is highlighted over that bounded head. Files over the caps or without a grammar
stay plain text, and a truncated head can cut a multi-line construct (best-effort colouring).
Residual: the preview stays head-only for files over 256 KiB (including its colouring), and has no
semantic/LSP spans; Fresh renders its overlay preview as a real buffer through the text pipeline,
while gvim has no remote preview buffer and only this bounded-head highlighter; the explicit-
candidate path still needs a complete remote listing (R13-23/R13-11).
**asm_lsp reduced port (187).** New bundled Lua plugin asm_lsp (off; no bindings/settings; default
set/idle heap unchanged), reduced from Fresh `plugins/asm-lsp.ts`: on .asm/.nasm/.s buffers it offers
to create a project `.asm-lsp.toml` when neither project nor global asm-lsp config exists, detects
assembler (GAS/MASM/NASM) and ISA (ARM64/RISC-V/x86/x86-64), writes asm-lsp's `[default_config]` TOML
atomically at the workspace root (non-GAS adds `default_diagnostics = false`), then runs native
lsp_restart; choices include detected/another dialect/dismiss/never-ask per project; on an
lsp_server_error naming asm/assembly on an assembly buffer it adds an install hint and an LSP-status
help popup with copyable cargo install/binstall asm-lsp and native Start/Restart/Show LSP Status;
commands Assembly: Create .asm-lsp.toml / Show LSP Help. Reductions: no plugin-API “Disable Assembly
LSP” action, per-language restart replaced by native lsp_restart active language, error hook lacks
language/server/error_type (claimed only on asm buffer + message naming asm/assembly), global-config
check through workspace FS (remote checks remote home), overlong project root truncates the
never-ask state key. Bundled set now 23 plugins (9 on)/14 off, 49 commands; docs updated (R13-6).
**Multi-cursor completion acceptance (188).** In a document with extra carets, ordinary completion
typing/Backspace go through normal insert_char/delete_backward so all carets edit in lock-step (one
undo group) and the list re-filters from the primary caret's word; a non-word key follows the same
dismissal rules. Accepting a plain candidate replaces each caret's own word prefix with the same text
as one atomic edit (one undo step): each caret keeps its own prefix length and ends after its own
inserted text, and the document version captured at request time is the stale guard, exactly as the
single-caret accept. Two carets whose replacement ranges overlap (both inside one word) refuse the
accept as a whole and write nothing. Snippet and auto-import candidates remain single-caret in this
bounded slice: with extra carets the popup dismisses with a notice and nothing is written because
their caret offsets/additional edits are not defined per caret; with one caret the existing accept
path (server edit range, snippet expansion, additional import edits, resolve, cursor placement) is
unchanged. Residual: multi-cursor snippet/auto-import acceptance is not implemented (R13-7).
Metrics: palette 240, settings 69 in 14 sections, bundled 23 (9 on)/14 off, idle heap ~7.27 MiB,
plugin API 243 functions/1483 lines level 2 (`gvim.url.open` added); i18n en 998 / 575 per locale,
560 KiB text (under 640 KiB cap), ≈649 KiB JSON, heap 114-87-202 KiB. Full suite EXIT 0 (76
packages, log /tmp/gvim-r26-tests.log); real hosts UNTESTED; no full-parity claim (residuals in §5
R13-1…R13-24 and §8).

**Round-25 outcome (2026-11-09, merged working tree on `355e7fc`).** Five tasks landed (178–181,
183); 182 is this audit. Static gates pass; final full `go test ./... -p 1 -count=1 -timeout`
`1200s` EXIT 0: 76 packages ok, no FAIL (log `/tmp/gvim-r25-tests.log`). The audit read the diff
and ran targeted tests; the Lead ran the full suite.

**Shared Save As reservation (178).** SharedBuffers.ReserveSaveAs is acquired in Editor.PlanSaveAs
on the UI goroutine before the async write command: while held another workspace cannot Open the
target (“being saved by <workspace>”), cannot Register as owner, cannot plan another Save As, and the
same editor's second save is “already in progress”. ApplySavePlan releases on success/write failure/
cancellation, SavePlan.Release is the dropped-plan hook, DropEditor releases closed/evicted claims.
This closes the plan→write→apply window and extends Fresh's one-buffer-per-file save_target_refusal
across gvim's off-UI write. Residuals: in-process-only reservation; a dropped plan without Release/
DropEditor leaks the claim until editor/daemon exit; OpenLoaded/plugin tabs for the reserved path
still load (only prevented from registering owner); disk exists/force semantics unchanged
(RefuseExisting decided at write); non-existent paths are not protected against non-gvim writers;
dormant/evicted owner unknown until rematerialize; mirrors keep separate cursor/view; plugin seam
doc-id; real hosts UNTESTED (R13-1).
**Dashboard action line targets (179).** Action rows (including the Git section's branch-log row)
publish action line targets running a hidden command the dashboard plugin owns, so a mouse click goes
through the same dispatcher as keyboard mode; a target cannot dispatch another plugin's command. The
branch-log row is advertised only when git_log's command is registered; otherwise it says “branch log
unavailable (enable git_log)” and is not clickable. Repository URL stays plain text (no safe URL-open
action; shell-open deliberately avoided). Keyboard equivalents preserved. OPEN: clickable repository
URL not done; weather/GitHub/network sections, third-party section API, Utility Dock placement and
sessions remain OPEN (not closed by no-network/dependency; icon/style differences acceptable) (R9-1).
**Remote search explicit candidate batches + degradation notices (180).** When a complete git
candidate listing is available, candidates are passed to the tool explicitly in bounded batches (400
paths / 48 KiB of paths per command, at most 64 commands): tracked files (including tracked-ignored
and tracked dotfiles) plus untracked non-hidden files; the tool never scans ignored/hidden trees, so
ignored matches cannot consume the output cap/limit before candidates. Paths follow `--`, query stays
one `-e` argument, cancellation/timeout stop between batches, merged output capped 8 MiB with
truncation flag; regex engine unchanged and every line re-checked locally; candidate filter still
drops outside-scope entries. Nested repos/submodules or >64 command lists keep the widened
whole-tree rg scan and receive a non-fatal notice that ignored entries were scanned (results may be
capped); an unavailable/failed/truncated listing keeps the tool defaults and sends the non-fatal
notice “remote ignore scope unavailable: … tracked files that match .gitignore may be omitted”; a
non-git workspace gets no notice; the Notice field appears in the panel hint row, clears on a new
search and never fails it; finder snapshot remains authoritative. Residuals: explicit path needs a
complete listing; failed/truncated defaults can omit tracked-ignored files (notified); too-large/
nested lists force widened scan (notified); match limit after merging batch output → result can be
<Limit; no POSIX-ERE prefilter; finder snapshot/backend walk; no local fallback (R13-11/R13-23).
**Terminal exit status + marker (181).** On child reap the dock records `Session.ExitStatus()` (0,
non-zero, or -1/signal); a `[Terminal process exited]` marker is appended after the frozen screen
into the saved/restored/restarted transcript (restart/resume continues below it, never overwrites
output); tab title shows `(exited, exit N)`/`(exited, signal)`, clean exit 0 keeps plain `(exited)`
(matching Fresh's omission); restored read-only tab shows the same suffix on Scrollback (restored);
marker written once per exit (restore/restart dedupes) and not painted from the styled snapshot.
Additive exit_code/exit_known in the terminal record; old records restore exactly (unknown status, no
invented marker); marker is an ordinary bounded transcript row; top-level fields mirror active tab.
Deliberate divergences: no status-bar restart indicator yet (palette/menu Restart only; status-bar
element OPEN follow-up), no foreground program-name inference, marker appended at transcript capture/
save/restart rather than exit instant because frozen screen stays; remote no state (R13-15).
**Shared-buffer MRU handoff rank (183).** When a shared document's identity is replaced during
ownership transfer/adoption (transferLocked/adoptPendingLocked), the new identity inherits the MRU
rank of the replaced document, so a handoff does not lose the buffer's recency position in popup/
dabbrev MRU order; editor MRU bookkeeping (RecentTabIndices keyed by document identity) remains
unchanged and bounded tests cover handoff ranking.
Metrics: palette 240, settings 69 in 14 sections, bundled 22 (9 on)/13 off, idle heap ~7.27 MiB,
plugin API 242 functions/1478 lines level 2; i18n en 998 / 575 per locale, 560 KiB text (under
640 KiB cap), ≈649 KiB JSON, heap 109-82-192 KiB. Full suite EXIT 0 (76 packages, log
/tmp/gvim-r25-tests.log); real hosts UNTESTED; no
full-parity claim (residuals in §5 R13-1…R13-24 and §8).

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

Round-13 evidence: docs/architecture.md (workspace registry §8.1, protocol, switching lifecycle,
capture ordering, remainder), docs/terminal.md (per-workspace tabs, discovery/import, trust gate),
docs/authority.md (remote file I/O §6, editor routing, LSP over SSH, residuals), docs/trust.md
§3.1 (spawn gate matrix/resolution/residuals), docs/i18n.md (extraction 2/boundary),
docs/settings.md (translated labels); code: internal/daemon/{workspaces,workspace_protocol,
framewriter}.go, internal/app/{workspace_host,workspace_capture,remote_editor,remote_lsp,
agent_discover,spawn_gate}.go, internal/tui/{workspace_host,workspace_ui,workspace_tags,
agent_discover,command_i18n}.go, internal/tui/dock/{tabs,discover}.go, internal/agentdiscover/,
authority/ssh_files.go, document/fileaccess.go, trust/gate.go, lsp/manager spawner/reconnect,
i18n/locales/*.json + tools/i18n_*, and the round-13 tests named in the tracking block.

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
| Palette commands (runtime) | 206 core (Fresh's `CommandDef` table) | 240 — 206 generated + 34 non-generated: 7 hand-written native rows + 4 plugin-management + 3 round-4 + 2 round-5 + 2 round-6 + 5 round-8 Markdown + 2 round-9 LSP + 2 round-12 terminal/agent-discovery + 7 terminal-tab + round-16 update rows (Check for Updates, Show Update Output); extract-tab is a generated Fresh row whose action landed in round 15 | `commands/commands_gen.go` + native `commands/*.go`/`commands/update_commands.go`, `commands/palette.go`/`palette_test.go`, `internal/tui/palette`; Fresh `docs/features/command-palette.md`; 240 = the Fresh row set plus documented native rows, none hidden/extra; self-update rows remain absent (R13-12). |
| Documented "not applicable" commands | n/a | 4 | `internal/tui/notice_na.go` (detach, update, update log, wave). Inspect Theme now runs (round 5), and the round-1 trust command runs. |
| Menus / menu actions | 8 / 103 static actions, 31 separators, 2 static submenus + 1 dynamic | 8 / 103 static actions, 31 separators, 2 static submenus + the dynamic theme submenu | Static parity is exact against Fresh's `translated_menus()` (103 actions / 31 separators / 2 static submenus); the dynamic "Copy with Formatting" submenu is filled at run time from the theme catalog (`internal/tui/menubar/dynamic.go`). Round 5 adds plugin-contributed rows (`gvim.menu.add_item`, `internal/tui/menubar/plugins.go`, at most 32 rows, owner-cleaned). Help's label row says "gvim" where Fresh writes its version. |
| Keymap presets | 5 | 5 | Same names and inheritance graph. Round 7 closed R2-4: the overlay contexts (`prompt`, `searchPrompt`, `popup`, `menu`, `settings`, `dock`) consult the keymap before built-ins with fall-through; own/in-effect counts are now default 245/243, macos 60/266, macos-gui 52/317, emacs 98/309, vscode 9/248 (`keymap/common_set_test.go`, `docs/keybindings.md`). `completion`/`compositeBuffer` are not emitted; residual R7-4. |
| Bundled plugins | 50 | 30 (9 on by default) | Round 10 added git_grep/git_explorer/env_manager (all opt-in); r25 asm_lsp; r27 csharp_support; r28 clangd_support; r31 welcome; r42 markdown_source; r43 rust_lsp; r44 deno_lsp/diff_nav (all opt-in/reduced). 21 off: flash, vi_mode, live_diff, git_log, compare_files, merge_conflict, dashboard, code_tour, lsp_navigation, find_references, git_grep, git_explorer, env_manager, asm_lsp, csharp_support, clangd_support, welcome, markdown_source, rust_lsp, deno_lsp, diff_nav. `docs/plugins/bundled.md`. |
| Bundled plugin commands | ~180 | 68 registration sites | +2 r25 asm_lsp, +2 r27 csharp_support, +3 r28 clangd_support, +5 r31 welcome; r42 markdown_source 3 hidden commands; r43 rust_lsp 4 visible; r44 deno_lsp 3 visible + diff_nav 2 visible; palette core count unchanged. prior count 47. |
| Themes | 10 built-in themes, theme files by name/path/URL/`builtin://`, `extends` inheritance, attributes on every colour, theme editor, colour picker/mouse editing, inspect-at-cursor, plugin theme overrides, per-cell provenance, background images | 10 built-ins + user themes; plugin theme files; URL themes (round 16, cached/validated); no packaged repo/browser | `theme/data/*.json`, `theme/{theme,registry,form,url}.go`, `internal/tui/{theme_edit,inspect}.go`, `internal/app/theme_url.go`, `docs/themes.md`; Fresh `docs/features/themes.md`. Rounds 4–13 built themes/extends/editor/picker/inspect/provenance/plugin files. Round 16 adds URL themes: https (or loopback http) under the update transport policy, size cap, existing parser/validator, cache under user config keyed by URL hash, last-known-good offline, clear failure notice, builtins never fetch; `--theme` same forms and policy; Settings can select a URL fetched at this start (uncached reported, not fetched in Update); `repo#fragment` packaged-repository form rejected with the explicit message. Tests theme/url_test. Open: packaged repo install/marketplace/browser, startup-only URL fetch/cache refresh, background images, drag-and-drop/inheritance edges (R13-6/R13-20). |
| LSP server entries | built-in + plugin helpers + universal servers | 62 built-in entries; universal_lsp is config-only | `lsp/manager/defaults.go`; gvim routes lists/universal entries and draws hints/tokens/folds/diagnostics, but no built-in universal server ships (Fresh's quicklsp is a separate user-installed Rust binary — decision recorded; user-configured universal_lsp works fully). `process_limits` is enforced on Linux (cgroup v2/ulimit fallback) and recorded as unreachable on macOS/Windows. |
| Bundled plugin idle heap | n/a | about 7.27 MiB of the 8 MiB gate (9 default-enabled; ~9.3 MiB session baseline) | `TestBundledPluginsIdleHeap` measures the gate in a fresh process (a test budget, not a production cap); the production per-slice heap guard is a separate sampled circuit breaker (128 MiB limit, 1 ms samples after the first 2 ms, forced-GC confirmation; process-wide) and is not a strict hard limit. The heap-guard test now runs a single fresh child with strict parsing and keeps its 4× test ceiling; a late 1 ms sample can miss one doubling near that boundary (observed max 255 MiB for a 64 MiB test limit), so 4× carries a scheduling-tail risk and is a test contract, not a memory guarantee. Evidence: `plugin/budgets_test.go`, `docs/plugins/limits.md`, `docs/plugins/bundled.md`. |
| Plugin API surface | ~372 methods | 244 functions | `docs/plugins/gvim.d.lua` is 1489 lines, API level 2; r26 `gvim.url.open`; r28 `gvim.http.get` (HTTPS GET with user network opt-in/SSRF/timeout/body/concurrency limits). Remaining gaps: R9-5 (composite substitution/dock/per-panel/windowEmbed/markdown-in-text), package manager/generic HTTP fetch (gvim.http.get is bounded HTTPS GET only), project-local plugins. |
| Settings page | schema-driven page (~14k LOC in refs/fresh) over every setting | 69 settings in 14 sections + injected Languages entries/Language Servers/Plugins/Keybindings/Environment/Detectors pages; new round-20 knobs auto_attach/daemon.materialized_workspaces/ssh.* are top-level user config (no Settings rows, project refused where security/memory-relevant) | Rounds 3–11 built the page/editors/reset/Reload/Contents/non-scalar plumbing; rounds 12–19 added Environment/Plugins/Keybindings/declared settings, theme preview, locale row, completion rows, URL/update config-only rows, settings action notices. Values/keys/pending/reset/layer behaviour unchanged; structured field/row names stay schema identifiers. Residuals R8-3 remain open TUI gaps (declared plugin settings read-only, no gallery/install page, whole-array LSP JSON rather than per-field dialogs, no in-page key capture/second writer, no env activation, no per-field reset-vs-inherit/full undo, Reload palette-row only, generic Fresh widget runtime not ported). |
| Terminal | PTY terminal panes and utility dock, tabs (≤16, per-workspace in a daemon) with strip/rename/click/quick-switch/per-tab close_on_exit, tab reordering (palette rows, Ctrl+Shift+PgUp/PgDn where modified keys arrive, mouse drag; persisted order), live/scrollback modes, OSC 7 paths, launch/agent/resume records + Run Agent picker, restart, shell override, selection/copy, mouse forwarding/drag-select, drag auto-scroll, word/line drag, Ctrl+hover file:line:col links, pass_ctrl_v_without_text, ConPTY on Windows, SSH terminal, opt-in OSC 52 clipboard read/paste, Live restore with styled screen snapshot and dock modes | **partial** — rounds 10–17 built slice A/tabs/agents/remote terminal, per-workspace scoping, reorder, OSC 52 read and Live restore; round 17 adds a bounded styled visible-screen snapshot in the version-1 record (≤200×512 cells, rune/fg/bg/bold/underline/reverse, exact-tail match on restore, corrupt ignored), read-only Scrollback (restored)/frozen modes with Ctrl+Space pager/live toggle, and revive into live mode with snapshot above new output; tab identity/order/revive-once/restore_live unchanged. Evidence: `internal/terminal/*`, `internal/tui/dock/*`, `internal/restore/terminal.go`, `docs/terminal.md`. | Open: Fresh's full backing-file semantics (bytes/cursor/alternate-screen/OSC/hyperlinks), only visible screen styled (older history text-only), revive via explicit dive (dock not restored into editor split), remote launches restore no layout/terminal state, per-tab splits/kitty/reflow/report-split; R13-15/R13-6. |
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
| File explorer and sidebar | tree, file create/delete/rename/copy/cut/paste, hidden/gitignored toggles, git status marks, natural sorting, plugin rows/decorations/slots, context menu, async reload, width/side, follow active buffer, compact chains, sticky parents, sidebar sections, session state, Contents fold_buffer, remote tree with remote git letters, preview, refresh, local-style ignore filtering, directory watching, write operations and clipboard | **partial** (R3; R1; R6; R9; R10; r15 remote explorer; r16 git/preview/refresh; r17 writes; r18 ignore/watch gvim extension; r19 clipboard; r21 exclusive create) | `explorer/{options,sticky,gitignore}.go`, `internal/tui/explorer/`, `internal/tui/{root_explorer,remote_tree,markdown_toc}.go`, `internal/tui/sidebar/`, `internal/tui/palette/preview.go`, `internal/app/{remote_explorer,remote_explorer_ops,remote_explorer_copy,remote_git,remote_preview,remote_ignore}.go`, `finder/index.go`, `docs/sidebar.md`/`docs/architecture.md` §8.1 | Local tree/local residuals. Remote: authority-ReadDir lazy tree, git letters, palette head preview, finder Restart/reconnect/Refresh, r17 New File/New Directory/Rename/Delete, r18 remote .gitignore + polled directory watch (gvim extensions), r19 cut/copy/paste/duplicate over authority (bounds 10k entries/256 MiB/64 levels/2 min; atomic per-file writes, binary-safe; cut copy-then-remove; paste refuses existing name; duplicate Fresh free-name; symlink/root/outside/inside-source refusals; confirmations naming target; trust gate/disconnected/refresh; partial destination reported on cancel/failure). OPEN: no drag/move between directories, no undo/history, no whole-subtree transaction/cleanup UI, mode/mtime/hardlink/xattr not preserved, copy/paste/rename destination-exists refusal is only a pre-write check (concurrent create can be overwritten; no exclusive destination create), no remote overwrite prompt, watch latency/rename approximation, palette rows no git letters (local too), project-search preview? now wired, Windows paths; real-host acceptance. |
| Finder, quick open, live grep | Ctrl+P modes (file/command/line/buffer), fuzzy ranking, streaming enumeration (local + remote authority), finder preview (local only), live grep with provider toggles (case/regex/word/files/buffers/ignored/terminals/workspaces), quickfix export, resume | **partial** (R3 landed the preview and toggles; phase 2 adds remote finder) | `finder/finder.go`, `finder/remote.go`, `internal/app/{remote_finder,remote_watch}.go`, `internal/tui/palette/{preview,grep,palette,draw}.go`, `internal/app/{preview,live_grep}.go`, `docs/ui.md`; Fresh `docs/features/command-palette.md` | Remote launch uses the authority file system only (idle placeholder → real remote index; no local fallback), ~/… roots expand in a real shell, errors/truncation surface as rows, and all remote project-search/Git Grep/replace/Live Grep entry points are refused before touching local ports. Still open: **remote Live Grep/project search/Git Grep** (authority-backed search), **remote git marks/preview/refresh** (remote file finder rows are plain; palette preview nil; index is built once and not rebuilt on reconnect/watch), quickfix export, terminals/workspaces providers, syntax colours/scrolling in the local preview, and Resume re-runs the search instead of restoring cached matches (R3-4/R13-3). |

| Project-wide search, replace and Git Grep | plugin Search & Replace panel: match tree, scoped replace, globs, current-file/all-files/open-buffers, 10k cap, unsaved buffers, quickfix export, search history, selected-match preview with scrolling/syntax/soft wrap, multi-line matches, streaming results; git grep finder with case toggle and pathspec field; bundled git_grep plugin | **implemented** core; **partial** against the plugins (R4–R11) | `search/plan.go`, `internal/app/{projectsearch,gitgrep}.go`, `internal/tui/searchpanel/{glob,types,panel,draw,preview}.go` + `internal/tui/project_search.go`, `internal/tui/workspace_edit.go`, `plugin/bundled/plugins/git_grep/`, `commands/search_commands.go`, `docs/keybindings.md`, `docs/ui.md`; Fresh `plugins/search_replace.ts`, `plugins/git_grep.ts` | Round 9 previews/large-file distinction/Open Buffers; round 10 ported git_grep; round 11 added preview soft wrap/toggle (`Options.PreviewWrap`, `Alt+P` in panel and editor, wrapped rows with syntax-span/match remap, 8-row cap + ellipsis, display-row window, session-persisted) and recorded the recent-files provider decision (no app-level opened-file history; needs a history snapshot + streaming/replace semantics). Residuals R9-2: closed-oversize streamed writer, provider sources beyond files/open/current (diagnostics/terminal need provider-specific snapshots); R10-1 git_grep reductions. |
| Command palette and command table | 206 core commands + plugin commands, contexts/availability, prefix modes, key hints | **implemented** core / **partial** plugin commands | `commands/` (206 generated + native rows), `internal/tui/palette/`; Fresh `docs/features/command-palette.md` | The generated core table is exact (206/206). The runtime palette has 229 rows (unchanged in rounds 9–10): 206 generated + 23 non-generated (7 native + 4 plugin-management + 3 r4 + 2 r5 + 2 r6 + 5 Markdown mode rows) — `commands/palette_test.go`. Bundled plugin commands add rows only while their plugin is enabled (47 registration sites across twenty-two). |
| Menu bar, context menus, help, welcome | 8 menus, 103 static actions, dynamic theme submenu, mnemonics/underlined accelerators, context menus, help overlay, welcome screen, dashboard | **implemented** menus/help/welcome; dashboard is a partial plugin/native equivalent (task177 upstream tracking; r26 safe gvim.url.open + clickable repo/branch/Open Repository/Open Branch; r28/r29 opt-in GitHub PR network-gated rows clickable/keyboard with canonical-URL rebuild; weather opt-in landed r30; other GitHub/network sections/section API/dock/mouse/sessions OPEN) | `internal/tui/menubar/{menu_table_gen,menubar,dynamic}.go`, `keymap/menu.go` (`Map.MenuMnemonic`), `internal/tui/menu_mnemonic_test.go`, `internal/tui/menubar/mnemonic_test.go`, `internal/tui/context_menu.go`, `internal/tui/help.go`, `internal/tui/welcome.go`, `docs/ui.md` §6; Fresh `docs/internal/editor-ux-features.md`, `docs/features/dashboard.md` | Menus match Fresh's static table exactly (103 actions, 31 separators, 2 static submenus), and the dynamic "Copy with Formatting" submenu lists the 10 bundled themes (`menubar/dynamic.go`). Round 2 added the accelerators: underlined keymap-assigned letters and Alt+letter through ordinary keymap resolution, F10 for the bar, `editor.menu_bar_mnemonics` off removes both; dropdown rows carry no mnemonics (as in Fresh). Help's label says "gvim" instead of Fresh's version string. Residual: weather has since landed opt-in (r30; wttr.in via restricted HTTPS); other dashboard optional sections/features (GitHub-network/third-party API/dock/mouse/sessions) stay OPEN — not closed by network/dependency/size; task177 added upstream tracking; r26 adds safe URL opening and clickable repo/branch rows (non-GitHub branch URL unavailable, unsafe origins plain text). Not the menu system. |
| Status bar | configurable element lists (Fresh's defaults: left `{trust} {remote} {terminal_restart} {cursor} {diagnostics} {cursor_count} {messages}`; right `{read_only} {line_ending} {encoding} {language} {lsp} {warnings} {update} {palette}`), separator, per-element click actions, squeeze rule, `{filename}`/`{chord}`/`{clock}`/`{update}` | **implemented** (R2; parsed-but-unpainted elements documented; clickable update landed round 16) | `internal/tui/frame/frame.go` `StatusRuns`/`DefaultStatusLeft/Right`, `internal/tui/model.go` `statusView`/`statusRow`, `internal/tui/{status_elements,update_status}.go`, tests `statusbar_elements_test.go`/`statusbar_residual_test.go`/`frame/update_element_test.go`; `docs/ui.md` §5 | R2-3 closed round 5; phase-2/round-15 extracted section/field labels; remote pill uses `gvim.status.*`. `{update}` paints `Update: vX.Y.Z` (i18n key `gvim.status.update_available`) when the startup/last check found a newer release; round 16 makes it clickable to the Update details (feed/current/latest/last-run/release page) without self-update/install (R13-12 residual). `{warnings}` and plugin tokens remain unpainted; plugin status items keep anchors. |
| Config layers and Settings UI | 4 layers, per-buffer overrides, config write from toggles, Settings UI, `init.ts`, config reload, LSP/plugin/keybinding/env/locale sections | **partial** (page surface complete; r11 locale; r12 config additions) | `config/{settings_schema,settings_structured,contents,patch,env,locale,clipboard,terminal}.go`, `internal/app/settingssvc.go`, `internal/app/settings_*.go`, `internal/tui/{settings*,settings_theme_preview}.go`, `i18n/`, `docs/settings.md` | Rounds 3–11 built the page/structured editors/reset/Reload/Contents, non-scalar plumbing, Language Servers/Environment, Plugins/Keybindings, declared-settings rows, Environment/Detectors, theme preview and the Interface locale row (live). Round 12 adds the config `terminal.resume_agents` (Fresh key, default true) and `terminal.agents` (gvim extension, user-layer only, bounded) plus `clipboard.use_osc52` (r11). Residuals R8-3 as documented (docs/settings.md §6 exact). |
| Keymaps and keybinding editor | 5 presets, user keybindings, unbind/noop, chords, custom keybinding maps, keybinding editor, overlay contexts | **partial** (R2 data; R7 overlay contexts; R11 unbind tombstones) | `keymap/*.json`, `keymap/common_set_test.go`, `keymap/menu.go`, `internal/keybindings/` (`Unbound` adapter), `internal/tui/overlay_keys.go`, `internal/tui/keybind_editor.go`, `docs/keybindings.md` | Round 7 closed R2-4 (overlay contexts with fall-through; own/in-effect default 245/243, macos 60/266, macos-gui 52/317, emacs 98/309, vscode 9/248; loader warns/skips multi-key overlay bindings). Round 11's explicit unbind tombstones (`keymap.Map.Unbound`, TUI `UnbindKeymap` port) let an overlay's built-in fallback be suppressed for an unbound chord. Residuals R7-4: multi-key bindings are not consulted in overlay contexts (one-press router; pending-chord state is root-only), and the completion/compositeBuffer/text-field contexts stay outside the router (Fresh implements them per surface). |
| Themes and appearance | 10 built-in themes, theme files by name/path/URL, extends, colour picker/mouse editing, inspect with per-cell provenance, plugin theme overrides, background images | 10 built-ins + user themes; URL themes cached/validated (round 16); no packaged repo/browser | `theme/data/*.json`, `theme/{theme,registry,form,url}.go`, `internal/tui/{theme_edit,inspect}.go`, `internal/app/theme_url.go`, `docs/themes.md` | 10 built-ins/10 themes vs Fresh's 10 built-ins + user themes; plugin theme-file APIs; extends/colour picker/inspect landed earlier. Round 16 URL themes as §2 row: https/loopback http under update transport policy, 256 KiB/8 s caps, cache+last-known-good, invalid/oversized/offline notices, builtin no fetch; packaged repository form refused; Settings selects a URL fetched at this start. Open: packaged repo/browser/marketplace, background images, drag-and-drop/inheritance edges (R13-6/R13-20). |

### 3.3 Services

| Area | Fresh features (evidence) | gvim status | gvim evidence | Notable sub-gaps |
|---|---|---|---|---|
| LSP | diagnostics, hover, definition/implementation/references, completion (server + no-LSP buffer words; snippets/auto-import/resolve; auto-popup; multi-buffer words; trigger characters/live re-filter), rename, code actions/file ops, signature help, format, folds, inlay hints, semantic tokens, diagnostics panel/quickfix/inline/staleness, multi-server only/except, universal servers, status/mute, pull/workspace diagnostics, progress, process limits (Linux), env/init options, root markers, C header routing, remote LSP over SSH with remote env | **partial** (#7 A–E; rounds 7–14; round-15 auto-popup/multi-buffer; round-16 trigger/live-filter; r21 Backspace full-pool re-filter; r23 multi-buffer dabbrev; r26 multi-cursor plain accept; r27 snippet/auto-import multi-caret) | `lsp/{client,handler,requests,capabilities,snippet,progress,raw,limits}.go`, `lsp/manager/{manager,router}.go`, `internal/lsphost/{lsp,lsp_data}.go`, `internal/tui/{lsp,lsp_data,workspace_edit,diagpanel}.go` + inline hints, `editor/completion_words.go` + `internal/tui/completion_words.go`, `internal/app/{remote_lsp,remote_watch}.go`, `authority` LongRunning, `commands/lsp_commands.go`, `docs/lsp.md`/`docs/authority.md` | Rounds 4–15 as before (router/edits/status/staleness, hints/tokens/folds/pull diagnostics, panel/quickfix/progress, workspace edits, inline tier 2, cancel/process limits, remote LSP, completion safety/local-first/snippet/UTF-16/resolve/version+overlap guard/auto-popup/multi-buffer words). Round 16: advertised `completionProvider.triggerCharacters` (static+dynamic, union over eligible servers) open immediately with no debounce when `suggest_on_trigger_characters` is on; open list live-filters further word chars locally without a new request (case-insensitive label/insert prefix, synthetic range, dismiss+one re-ask when empty) with version/caret guards; active-buffer dabbrev cycling already existed (M-/). Verified: `labelDetails` is parsed into the item detail and rendered (Fresh renders labelDetails/detail next to the label; `refs/fresh/crates/fresh-editor/src/app/popup_actions.rs:707-714`; gvim `lsp/requests.go` completionItem; fallback item.detail). Fresh's completion popup renders label/detail only and **never documentation** (`refs/fresh/crates/fresh-editor/src/services/lsp/async_handler.rs:478-481`), so gvim not showing completion documentation is **not a gap**. Open (R13-7/R7-5/R13-3): non-word closes popup and passes through, matching Fresh (popup.rs/e2e; not a gap); Backspace now re-filters the full pool, highlighted row preserves candidate identity across typing/Backspace, falling back to first only when filtered out, Fresh multi-buffer dabbrev ported, large-file deferral, multi-cursor plain accept landed (snippet/import multi-caret still refused), request-id bookkeeping; LSP trigger chars landed but no server-side live-list refresh beyond local filter; no built-in universal server, per-OS limits Linux-only, remote retry only one restart, no remote install probe, remote trust uses local store; stricter stop-on-refused workspace-edit model (§4 item 18). |
| Plugins runtime and host API | TypeScript on QuickJS, sandbox-lite with per-plugin contexts, ~372-method API, overlays/virtual text/conceals/soft breaks/folds/line targets, explorer decorations/slots, project environment, prompts/toolbars, modes/contexts, windows/splits, virtual+composite buffers, buffer groups, terminals, authority/remote, LSP requests, HTTP, widgets/floating/dock panels/sidebar sections, i18n, package manager, menus, events | **partial, runtime different; API level 2 (tiers 1a+1b+1c + r10 edge APIs + r17 state + r18 shared runtime + r19 full scope/authority OS + r20 editor-global ids)** | `plugin/`, `pluginapi/`, `internal/pluginhost/`, `internal/tui/plugin*.go` + `plugin_shared.go` + `pluginpanel/`, `decorations/`, `env/`, `docs/plugins/` (6 API pages + events + 1489-line gvim.d.lua); r28 gvim.http.get bounded HTTPS GET — Lua 5.1 runtime difference; API level 2. Rounds 5–19 as before. Round 20: editor-global plugin window/buffer ids across the shared daemon runtime (per-mux sequence on Bind, seq<<40|local, host decode/refuse foreign/released CodeClosed, global ids in returns/events, ReleaseWorkspace frees closed/evicted sequence; direct launches sequence 0). OPEN: 40-bit local-id hard cap, editor-scoped overlays model-local, editor-scoped widget state rebuilt not cloned, buffer-bearing mount cannot be editor-scoped; remote/authority workspaces own runtime; R9-5 composite/dock/windowEmbed/package manager/project-local plugins (R13-24). |
| Bundled plugin feature set | 50 plugins (git, search, diagnostics, markdown, diff, agents, devcontainers, k8s, pkg, audit, helpers) | **partial: 30 ported (+ native equivalents where genuine); all remaining plugins are open work** | `plugin/bundled/plugins/` (30; 9 default-enabled), `plugin/bundled/lib/{finder.lua,git.lua}`, `docs/plugins/bundled.md`; Fresh `plugins/*.ts` + `docs/features/git.md` §Diff Chunk Navigation | Ported through r44: diagnostics_panel, git_log/live_diff, compare_files, path_complete/lsp_help, lsp_navigation/find_references/dashboard/code_tour, git_grep/git_explorer/env_manager (opt-in), asm_lsp/csharp_support/clangd_support/welcome (opt-in reduced), markdown_source (r42 opt-in parity port), rust_lsp (r43 opt-in reduced helper), deno_lsp (r44 opt-in reduced helper: Check Project/Copy Install/Language Server Help; r45 native core owns Deno auto-selection: authority-only root probe, explicit/disabled/lsp_enabled opt-out, AutoStart=false, per-root/stale gen, existing manager not restart-switched; Fresh default-enabled vs gvim helper off/native always-on toggle difference documented), diff_nav (r44 opt-in standalone Next/Previous Change over git_gutter/live_diff/saved diff; r45 real provider interaction + saved-deletion seam; Fresh default-enabled difference due 8 MiB idle-heap gate; duplicate git_gutter command rows residual). Remaining ports OPEN: orchestrator, live_sessions, agent_* family, devcontainer, k8s, pkg, audit_mode, remaining language helpers (slang etc.), diff_nav residuals (Compare/large-file/live_diff-load). `docs/plugins/bundled.md`/`porting.md`. |
| Terminal | PTY terminal panes and utility dock, terminal tabs (≤16) with strip/rename/per-tab close_on_exit/persistence/reordering, live/scrollback modes, keyboard capture, OSC 7 paths, launch records + agent resume + Run Agent picker, restart, shell override, selection/copy, mouse forwarding/drag-select, drag auto-scroll, word/line drag, Ctrl+hover file:line:col links, pass_ctrl_v_without_text, ConPTY, remote SSH terminal, opt-in OSC 52 read/paste, Live restore with styled screen/history snapshot/cursor/alt flag | **partial** (slices A+B; per-workspace tabs; remote terminal; phase-2 reorder+OSC52; r15 Live restore; r17 styled screen; r20 backing-file remainder) | `config/terminal.go`, `internal/terminal/{scrollback,mouse,saved,launch,session_backend,osc52}.go`, `internal/tui/dock/{tabs,discover,live_restore,screen_snapshot_test}.go`, `internal/app/{workspace_capture,clipboard_osc52,agent_discover}.go`, `internal/restore/{terminal,session}.go`, `authority`, `docs/terminal.md` | R15/V17 Live restore (live flag, revive-on-first-dive, transcript/styled screen). r28 adds OSC0/2 title persistence and alt-screen content restore; r26 adds `{terminal_restart}` status element; r28 child-side kitty keyboard protocol; r29 modifier/functional/push-pop fixes + title combine; presses only (Resume/Restart program/title, exit N only nonzero, signal, hover underline+bold, live+restored, click restart_terminal, keyboard equivalent; no OS-foreground program name/hover style differs/marker timing). Round 20: bounded 200-row/512 KiB styled history window + cursor row/col/visible/shape + alternate-screen flag in the v1 record, exact visible/history row-text match or ignored whole, old records unchanged, re-wrap drops metadata; read-only view paints history styles + reversed cursor; first dive hands live cursor/screen; daemon path. Tests cursor/alt/history/bounds/corruption/migration. OPEN: scrollback bounded styled not byte-exact (deep history text-only), OSC (title/cwd/clipboard/hyperlinks) not replayed, per-cell hyperlinks not representable, read-only cursor painted block, alt-screen marker not replayed, explicit dive, remote no state, per-tab splits/kitty/reflow/report-split; R13-15/R13-6. |
| Git surfaces | review diff (stage/unstage/discard/comments/export), range/stash reviews, git log with live diff, git blame panel with history, live diff baselines, diff chunk nav, git grep, git explorer, git find file, statusbar branch, gutter marks | **partial** | `gitstate/`, `plugin/bundled/plugins/git_gutter`, `git_statusbar`, `git_find_file`, `git_blame`, `merge_conflict`, `internal/tui/git.go`; Fresh `docs/features/git.md` | Missing: Review Diff (/range/stash), Git Log, Live Diff, Git Grep, Git Explorer, blame panel/history stepping (gvim has a reduced inline blame), merge view (reduced to in-buffer resolution), diff hunk sources beyond the HEAD gutter, review comments/export. Implemented: gutter hunk marks + next/prev change, branch statusbar, git find file, merge-conflict resolution, compare-as-difftool. |
| Session, restore, daemon | session restore/save, hot exit, crash recovery, remote hot-exit staging/recovery, split-layout persistence, --restore/--no-restore/--simple, daemon mode with attach/detach/open-file forwarding, multi-client attach + auto-attach, materialized-workspace cap/LRU eviction, cross-workspace shared buffers, workspace registry v2 with authority_spec, daemon-hosted remote authority/jump box, remote session restore, remote trust record/reconnect, checked exit durability, host marks, extract/close | **partial** (r12 daemon MVP; r13 #19a; r16 registry v2; r17 jump box; r18 remote session restore; r19 multi-client/auto-attach/hot-exit/trust/reconnect; r20 shared buffers/eviction) | `internal/app/{session_plan,session_open,hotexit,daemon_runtime,daemon_hosts,daemon_authority,workspace_host,workspace_capture,workspace_exit,remote_watch,remote_session,remote_recovery,workspace_shared,workspace_evict}.go`, `internal/editor/shared.go`, `internal/daemon/{workspaces,workspace_protocol,server,client}.go`, `internal/cli/{root,args}.go`, `internal/tui/{model,workspace_host,split_state,split/persist}.go`, `session`, `internal/restore`, `trust/`, `docs/architecture.md` §8/§8.1 | R12–19 as before (MVP/registry/open-file/durability/split/host marks/co-tenant/jump box/remote session restore/multi-client/auto-attach/remote hot-exit/trust/reconnect). Round 20: shared buffers for daemon LOCAL workspaces (owner+mirrors, ownership on focus/switch, version-guarded sync/full reload, close/evict promotes, direct/remote independent; residuals only OpenFile starts sharing/OpenLoaded-startup independent, tab close no promotion, large/virtual/>8 MiB independent, remote independent, mirror cursor separate, plugin id after handoff); materialized-workspace cap+LRU eviction (user-only daemon.materialized_workspaces default8 range1..64, project refused; capture-first/fallback, active never, capture+fallback failure keeps overcap+reports, remote excluded, status notice). OPEN: remote eviction excluded, #19b remainder shared-buffer paths, no fork/daemonize, Windows pipes, workspace CLI. Static gates passed; full suite EXIT 0 (76 packages, log /tmp/gvim-r20-fixed.log) after the daemon deadlock and helper frame-drop fixes. |
| Workspaces, windows, orchestrator, agents | orchestration dock, multiple workspaces per daemon, multi-client/auto-attach, switcher, New Workspace, Run Agent, agent discovery/resume, live sessions, extract variants, close confirmation, co-tenant workspaces, cross-workspace shared buffers, materialized-workspace cap+LRU eviction, jump-box remote workspaces, shared daemon plugin runtime + full window-scope/focus + editor-global plugin ids | **partial** (r13 #19a/#20b; r16 registry v2; r17 jump box; r18 shared runtime/session restore/extract variants; r19 full window scope/config/waits/multi-client/auto-attach; r20 shared buffers/eviction/editor-global ids; r21 shared-buffer close rule; r22 shared OpenLoaded/conflict label; r24 conflict Save As guard/new-path sharing/owner rekey; r27 mixed file+terminal extract) | `internal/daemon`, `internal/app/{workspace_host,workspace_capture,workspace_exit,workspace_extract,workspace_shared,workspace_evict,daemon_authority,remote_session,agent_discover,terminal_agents}.go`, `internal/editor/shared.go`, `internal/document/shared.go`, `internal/tui/{workspace_host,workspace_ui,workspace_tags,plugin_shared,agent_discover,agent_picker}.go`, `internal/pluginhost`, `internal/agentdiscover`, `docs/architecture.md` §8.1 — R13–19 as before. Round 20: cross-workspace shared buffers (OpenFile owner/mirror, ownership on focus/switch, change version guard/full reload, close/evict promotion, independent direct/remote); materialized-workspace cap+LRU eviction (capture-first, fallback, active never, overcap report, remote excluded); editor-global plugin window/buffer ids (seq<<40|local, CodeClosed foreign/released, ReleaseWorkspace). OPEN: sharing starts OpenFile/OpenLoaded but not plugin/API-created tabs, daemon open-file request switches owner, mirrors separate cursor/view unless ownership moves, Save As plan-to-write reservation landed r25 (in-process; dropped-plan leak/non-gvim writers/disk-exists semantics residuals); dormant-owner path unknown until rematerialize; plugin/API tabs load but cannot register owner; plugin seam new doc id; shared conflict label persistence landed r24; large/virtual/>8 MiB/remote independent, mirror cursor separate, plugin seam new doc id after handoff, remote eviction excluded, eviction/workspace CLI; auto_attach/named-daemon differences; extract mixed file+terminal landed r27 (co-tenant-only/no PTY handover/default-name conflicts/no undo-drag; residual), extract terminal-in-multi/drag-drop; orchestrator/live_sessions/agent_* (R13-1/R13-14/R13-24). |
| Remote authority | SSH editing, remote file system, processes/terminals, plugin fs/proc authority OS, remote LSP, watch, finder/search, replace, explorer writes/ignore/watch/clipboard, env, SSH auth/config/ProxyJump/agent forwarding/known_hosts policy/host-key algorithms/keyring, jump box, editable windowed remote large-file with STREAMED save (arbitrary size), remote hot-exit/trust/reconnect, saved-session restore, FileMutations/ReadRange/WriteStream **landed**; r21 plugin walk/copy (realpath landed r22; r23 remote search ignore scope + plugin copy streaming), OPEN: Fresh server-side patched recipe, whole-file non-windowed transport cap, search preview head-only >256 KiB/no semantic-LSP spans (bounded head syntax-highlighted r26/r28)/ignore, explorer drag/move/undo/whole-subtree transaction/mode-mtime-hardlinks-xattrs/existence race/overwrite prompt (r27 remote paste/duplicate Overwrite/Rename/Cancel landed; no All variants; overwrite remove-then-copy race)/watch latency, remote marker scan, hot-exit local-staging privacy divergence, ~user/special/sudo, reconnect/terminal respawn, keyring backends real macOS security/Linux secret-tool landed r27 (opt-in; real host UNTESTED)/2FA not stored, devcontainer/k8s, saved-session conflict detection, restored terminal dead/ownership, real SSH UNTESTED | P1 | XL | `authority`, `internal/app/remote_*`, `internal/pluginhost` authority OS, `document/{remote_large,large_save}.go`, `buffer`, `internal/tui`, `trust` — R13-3/R13-17/R13-19/R13-21/R13-22/R13-23 |

| Workspace trust | per-folder Restricted/Trusted/Blocked, marker detection, open-time prompt, status pill, per-spawn executable gate, config-layer gating, env activation gating | **implemented** (r1–r3 model/prompt/markers; r12 env gate; r13 spawn gate) | `trust/{trust,markers,env,gate}.go`, `internal/app/{trust,config_trust}.go`, `internal/tui/trust*.go`, `authority/local.go` gate hook + authoritytest, `internal/pluginhost/pluginos*.go`, `lsp/manager` gate, `internal/agentdiscover` runner gate, `docs/trust.md` | R13 closed the process-spawn gap: `trust.DecideSpawn` runs before every launch in the local authority (Run/Start/dock Launch: git/find/shell/formatter/terminal), the plugin OS (gvim.proc.run/spawn), the LSP manager (auto/manual/universal) and discovery; it resolves bare names through the child's effective PATH, executes the resolved absolute path, checks explicit paths lexically+symlinks, checks the shell `-c` first token, and refuses with a typed error naming level/spelling/help; Blocked refuses all, Restricted/Undecided refuse project-internal executables; the gate decides and does not kill running processes. Open (R13-4): only the shell first token is inspected (compound/quoted/variable-built commands not parsed), Windows PATHEXT/drive-case gaps, environment injected below the three seams unresolved, no kill on demote, and the remote path keeps the coarse app rule pending a remote trust store. |
| Markdown | syntax highlighting, markdown_source smart list editing, page view/compose, tables (wrapped cells), code-fence frames (continuation rails, container framing), dual view, heading conceal/scrollbar marks, page-view persistence, Contents section | **partial** (R1 Contents; R4 smart editing; R6 core; R7 fences/tables; R8 dual/borders; R9–11 residual slices) | `syntax/{markdown,markdown_inline,markdown_markers,markdown_fences,markdown_tables}.go`, `editor/markdown.go`, `internal/tui/editorpane/{compose,compose_layout,compose_fence,compose_table,compose_setext,pageview}.go` + `internal/tui/{pageview,dual}.go`, `internal/tui/sidebar/contents.go`, `session/pageview.go`, `docs/markdown.md`; Fresh `plugins/markdown_{toc,source,compose}.ts` | Round 9–10 landed wrapped cells, escaped pipes, container fallback, fence continuation rails, heading marks, page-view persistence, quote-container fences, setext conceal, task/image conceal, inline-HTML literal decision, mapped-decor no-paint, horizontal clamp, dual line-anchor/one-dual policy; round 11 verified composed plugin-overlay pipeline ordering and recorded the wrapped-cell selection decision (hook rows have no source line/row-source map) and tightened the dual/per-split wording. Remaining R10-2: free per-split view mode, dual split persistence, native Markdown menu row, exact byte dual anchors (approximation recorded), sidebar per-section widths/drag order. |
| Diagnostics surfaces | gutter marks, underline, inline end-of-line text, diagnostics panel, quickfix list, error navigation, hover fusion, pull/workspace diagnostics, progress | **partial** (rounds 4–10) | `internal/tui/{diag,diagpanel,lsp}.go`, `internal/tui/editorpane/{decor,inline_diag,inline_hints}.go`, `commands/lsp_commands.go`, `docs/lsp.md`; Fresh `docs/features/lsp.md` | Gutter dots, underline, inline end-of-line text with staleness, status counts, F8 jumps and hover diagnostics exist. Round 6 added the native Diagnostics panel (file→severity grouping, counts, jump/scroll/focus, progress/empty) and the Diagnostics List quickfix; round 7 workspace diagnostics for unopened files (`(not open)` rows) and `$/progress`; round 9 inline-hint tier 2; round 10 cancellation (pullSet/`$/cancelRequest`) and Linux process limits. Remaining: no built-in universal server (deliberate), per-OS process limits caveat, capability-gated workspace diagnostics (publish-only servers report their own set), plugin `DiagnosticInfo` carries no code (native ones do). |
| Scripting and CLI automation | `--cmd script`, `--cmd daemon open-file --wait`, stdin programs, `init.ts` startup script, agent channels, watch mode, CLI help for the API | **partial** (r11 stdin; r12 daemon flags+remote; r13 workspace CLI/remote) | `internal/cli/{root,args}.go`, `internal/app/{stdin,daemon_runtime,workspace_host,remote_editor,remote_lsp}.go`, `authority/spec.go`, `internal/tui.RunWithInput/RunWithIO`, `plugin_init.go`; Fresh `docs/features/scripting.md` | gvim has `init.lua`/Lua API, stdin-as-a-buffer, trust-gated `gvim config show/paths`, `gvim grammar`, foreground daemon/attach flags, workspace frames (workspace_list/select/new/close/rename; `Hello.Workspace` parsed but ignored), and remote `user@host:path`/`ssh://` launches with remote editor/LSP routing. Missing (open): Fresh's external `--cmd` script channel, `open-file --wait`, watch/agent capability, forked daemon/auto_attach, multiple clients, workspace CLI management. |
| Internationalization | 15 locales, locale detection/config, plugin i18n, translated command/menu/settings/status/dialog/prompt/picker/chrome/authority-remote strings | **partial** (r11 foundation; r12 catalogues; r13 extraction 2; r14–r18 slices; r19 remainder 4; r20 remainder 5) | `i18n/`, `tools/{i18n_import.py,i18n_check.sh}`, `commands/gen`/`tools/gen_menus.py`, `internal/tui/*i18n*`, `internal/app/settings_i18n.go`, `docs/i18n.md` | Round 20: extracted SSH reconnect notice/refresh-failure suffix, LSP restart-count plural (TN), remote workspace restore notice, remote trust display, workspace trust warning, plugin focus/scope notices, remote explorer clipboard/recovery notices, remaining settings/status strings; docs inventory/budgets refreshed. Metrics: en 998 keys, every non-English 575; key+value 573,676 B (560 KiB, under 640 KiB text cap), JSON 664,221 B (≈649 KiB), heap en 114 KiB / de 82 KiB / both 197 KiB; i18n_check green. OPEN (R13-5): rows without Fresh keys, structured field/row schema names, some keybinding help/plugin-manager edges; restore_live config-only; self-update commands absent. |
| Update, packaging, telemetry | update check + status-bar `{update}` element, in-editor self-update prompt, Update fresh/Show update output commands, update terminal buffer, telemetry toggle, packaging/installers, GUI/desktop, browser --web, WASM | **partial — update check + clickable indicator + Check for Updates/Show Update Output landed; self-update/install/prompt and update terminal buffer open (R13-12)** | `updatecheck/updatecheck.go`, `config/update.go`, `internal/app/update_check.go`, `internal/tui/{update_status,update_commands}.go`, `commands/update_commands.go`, `internal/tui/frame/frame.go`, `docs/settings.md`/`docs/ui.md`; Fresh `docs/features/update.md` | Landed (r15/r16): config `check_for_updates` default true (user layer; project layer refused), gvim extension `update_check_url`/`GVIM_RELEASES_URL`, https or loopback-http validation, 8 s/64 KiB, daily stamp, no-version skip, silent failure, tea-command check; `{update}` paints `Update: vX.Y.Z` and is clickable to Update details (feed/current/latest/last-run/release page); palette 240 gains Check for Updates (forced check ignoring debounce) and Show Update Output (details list). Open: self-update download/install/prompt, Fresh's "Update fresh" self-update command (gvim's rows only check/show), update terminal buffer (gvim shows a details list, not Fresh's terminal buffer), release-page click starts no install. Telemetry/GUI/browser --web/WASM/packaging remain objective boundaries (§8). |
| Platform input and OS integration | CSI-u/modifier disambiguation, AltGr/CapsLock/Shift quirks, key release, GPM mouse on Linux console, suspend, stdin streaming, ConPTY, OSC 52 clipboard (write + opt-in read), window title, chrome/plugin focus events | **partial** (stdin, ConPTY compile-verified, OSC52 write+read, reflow; round-15 input edges) | `internal/terminal/`, Bubble Tea v2 input, `internal/tui/{clip,input_edges,plugin_keys}.go`, `internal/app/{stdin,clipboard_osc52}.go`, `config/clipboard.go`, `internal/tui.RunWithInput/RunWithIO`, `docs/keybindings.md`/`docs/plugins/events.md` | Legacy normalization landed: uppercase-without-Shift infers Shift (Ctrl unaffected), CapsLock/lock bits ignored, named keys keep reported Shift; AltGr-as-Ctrl+Alt with one printable rune is text input (bound chord still wins); plugin events `chrome_focus_changed` (editor/explorer/dock/section; tabs gvim extension; no menu/overlay/initial) and `key_release` (same shape as capture; only backends reporting `tea.KeyReleaseMsg`, e.g. Windows console; legacy silent, no kitty flags requested/faked) — tests input_edges_test. Open TUI gaps: GPM console mouse, kitty keyboard, live-screen reflow, legacy key-release unavailable, terminal-window focus only for remote reconnect, ConPTY/real-host OSC52 unverified. |

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

54. **The dashboard is a partial native equivalent; Fresh's plugin features stay open.** gvim has a
    native read-only start tab (repository state — task177 upstream tracking row ahead/behind/no
    upstream — recent files, keyboard quick actions, best-effort `df` rows, auto-open/close, timer
    refresh) and the opt-in GitHub PR section (r28 network-gated REST; r29 rows are keyboard/click
    reachable action rows that rebuild a canonical github.com/<slug>/pull/<n> URL from validated
    fields — untrusted html_url/other API links ignored — then use gvim.url.open's http(s)/confirmation
    path; ≤6 rows from ≤200 entries, titles normalized/truncated). Fresh's dashboard plugin still has
    optional weather/other GitHub/network sections, a third-party section API, Utility Dock placement,
    generic row mouse clicks and a sessions section — these remain OPEN work with concrete gaps;
    nothing is closed by no-network/dependency/size. Icon/style differences are acceptable;
    `docs/plugins/bundled.md`.
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
66. **No built-in universal language server ships; this is an open TUI/LSP gap.** Fresh's
    quicklsp is a separate user-installed Rust binary (default `universal_lsp` names `command:
    "quicklsp"` with `enabled: false`; no quicklsp crate in the editor sources), and gvim fully
    supports user-configured `universal_lsp` servers, but the built-in/universal server capability
    itself is missing functionality: it stays OPEN with the concrete gap that the server does not
    ship (implementing or packaging a universal server remains work, not an exception).
    `docs/lsp.md`; R7-5.
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
68. **Theme gaps are open with the currently-implemented pieces stated.** Landed: accent audit
    (bundled themes fall back to popup selection; user themes may set accents), Settings four-chip
    previews, the colour picker/mouse editing overlay, and first-surface painted-key provenance
    (opt-in capture, exact keys where tagged, "matched by colour" fallback elsewhere; the picker
    and drag were not extended to provenance). OPEN TUI gaps, not decisions: URL/packaged theme
    refs (recognised and refused today — a real URL/packaging network path is unimplemented),
    background images, drag-and-drop of theme values, and user-theme inheritance beyond the
    one-level extends-to-built-in rule. `docs/themes.md`; R11-4/R13-6.
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

    **Memory-guard semantics (recorded after the heap-test diagnosis).** The 8 MiB idle-heap
    figure is a test budget measured in a fresh process, not a production cap. The production
    per-slice heap guard is a sampled circuit breaker: it reads the heap when a slice begins,
    samples every 1 ms after the first 2 ms, confirms live growth with a forced collection when
    the allocated-object growth crosses 128 MiB, and cancels at once when the growth passes the
    limit plus the live heap that was already there. It measures the whole process and cannot
    interrupt a Lua instruction, so it is not a strict hard limit/accountant; a doubling string
    typically peaks near twice the guard. The `TestHeapGuardStopsADoublingString` failure was
    diagnosed as shared-process `HeapSys` baseline contamination in a package run (other tests
    leave hundreds of MiB of live/garbage heap; package suites allocate concurrently), not a
    production failure. trust changed only the test: a single fresh child process, strict parsing
    of every reported field, and the 4× ceiling retained; isolated ×10 and the plugin package
    pass, timing tests unchanged. 4× is a test contract with scheduling-tail risk: a promptly
    scheduled guard peaks near 2× and a late 1 ms sample can miss another doubling (observed max
    255 MiB for the 64 MiB test limit); a value beyond 4× violates the test ceiling and may be sampling delay or a guard fault (a peak alone cannot prove the guard never fired). `docs/plugins/
    limits.md` (heap guard / residual risks) and `plugin/budgets_test.go`.
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
    daemon), no daemon auto-start on launch (refusal policy), r12 single-client attach (r19 multi-client/auto_attach landed), a
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
79. **Update indicator/check is partially landed; self-update and update commands are open**
    (R13-12). The Lead implemented the TUI-visible update check: `check_for_updates` (default
    true, user-layer only; a project layer setting is refused with a diagnostic), the gvim
    extension `update_check_url` and the `GVIM_RELEASES_URL` override, an https/loopback-http feed
    policy, one bounded request (8 s timeout, 64 KiB body cap), a daily debounce stamp
    `<config dir>/update-check.json`, no-version builds skipping the check, silent failures, a
    tea-command background check (never on the Update goroutine), and the `{update}` element
    painting `Update: vX.Y.Z`. It is config-only (no Settings-page row); `self_update` is accepted
    and ignored; the element is informational and not clickable. Still OPEN: self-update
    download/install/prompt, the Update fresh/Show update output commands (generated but hidden +
    NA notices), the update terminal buffer, and a clickable indicator/release-page action. Fresh has no `--no-upgrade-check` CLI flag (only the `check_for_updates` config key), so a flag is not a parity gap; an optional gvim extension only.
    Telemetry/self-update installation and the GUI/web/WASM/
    packaging matrices remain objective boundaries; the update indicator itself is no longer an
    open TUI gap. See §8 and R13-12.
80. **Platform edges are open TUI-input gaps with the landed parts stated.** stdin-as-a-buffer
    (bounded snapshot, gvim-only `--stdin-name`) is implemented. Still OPEN TUI input/behaviour,
    not out of scope: GPM mouse on the Linux console, AltGr/CapsLock/key-release edge cases, and
    plugin focus events beyond the landed focus_gained/active_window; each has concrete cases in
    §3's platform row. No platform-edge item is closed by size or effort.
81. **GUI/desktop, browser --web, WASM and packaging are outside the TUI objective.** The user
    asked for Fresh's TUI functionality; other front ends and distribution targets are objective
    boundaries, not backlog gaps, unless a specific TUI feature needs one (none does today).
    Daemon mode itself is TUI and in scope; daemon packaging is a distribution target. §8 records
    the boundary and the genuine-equivalence policy; nothing else is closed by it.
82. **The daemon/workspace MVP has open reductions (no closure).** Round-12 daemon: foreground
    (no fork/daemonize), positional/dormant open-file routing, checked exit durability/late-writer
    limits, daemon host marks that refuse every direct launch while a daemon hosts the workspace
    (R13-10 closed; deliberate divergence from Fresh's shared-buffer ownership), split persistence,
    extract/close confirmation, registry v2/co-tenants, jump box, remote session restore, and round-19
    multi-client attach + auto_attach. Multi-client: several attach clients share one session,
    server frames broadcast to all, input from any client reaches the program, the smallest attached
    client size wins, detach/drop removes only that client, slow client detached after one bounded
    write, unknown frame drops only that client, incompatible protocol version rejected before any
    state change. auto_attach is a top-level user knob default OFF (Fresh defaults on): a bare
    same-root launch joins a running daemon with a notice, never starts a daemon, never applies to
    remote-target launches (explicit -a required), and per-root named-daemon ambiguity still refuses.
    Still OPEN: no fork/daemonize, Windows named pipes later, eviction/workspace CLI, auto_attach
    default/named-daemon nuances differ Fresh, remote marker scan, remote hot-exit local-staging
    divergence, plugin id namespacing, shared buffers; exit limits unchanged (deadline leaves newest
    in-flight capture to finish; kill between snapshot/write loses that capture, previous file
    intact). `docs/architecture.md` §8/§8.1.
83. **Terminal tabs are per-workspace and reorderable; Live restore is landed partially.** The dock
    runs at most 16 single-session tabs (strip/active/exited/click-select, Ctrl+PgUp/PgDn,
    palette rows, per-tab close_on_exit), Fresh titles; `terminal.json` has additive tabs/
    active_tab with mirror/bounds/migration; per daemon workspace the tab set is separate (warm
    tabs keep live PTYs, dormant restores read-only/dead, closed workspace kills shells and keeps
    transcript). Phase 2 added reorder (palette/keys/drag, persisted order). Round 15 adds Live
    restore: `live` per-tab/top-level flag, restore starts nothing, a live-at-capture tab is
    dormant and revives on the first dive (switch/focus/show) with a fresh PTY in saved launch_dir
    using resume→launch→shell precedence, bounded saved transcript above; exited/old tabs remain
    dead, a failed dive is one notice, `terminal.restore_live` (default true, user layer) can
    disable revival; daemon workspaces behave the same. OPEN: Fresh revives on focus from a
    persisted backing file (gvim needs the explicit dive and keeps bounded transcript text),
    per-tab splits/drag-to-split, report-split close_on_exit, kitty keyboard, live-screen reflow,
    agent re-exec edges; remote launches still restore no layout/terminal state. `docs/terminal.md`;
    R13-15.
84. **Agent sessions are launch records + discovery; the plugin-era features are open.**
    `terminal.Launch` (argv/agent/resume argv/ephemeral, bounded), persisted launch records with
    no auto-resume, user-initiated restart precedence resume→launch→shell under
    `terminal.resume_agents`, the gvim-only `terminal.agents` list, a Run Agent picker with
    per-launch directory and resume choice, tmux discovery/Import (round 13) and the phase-2
    external history scan cover the core. The history scan is read-only/bounded over the Claude
    Code and Codex CLI transcript stores: newest headers only, 50 sessions/tool, 5001 entries,
    20 problems, 256/64 KiB header caps, 400 record lines, strict session-id token validation,
    fixed resume templates (Codex `resume --last` labelled inexact), transcript bytes never used
    as argv, merged with the tmux findings. OPEN gaps, not substitutions: dock/agent rows outside
    the picker, blocked/done/working state heuristics (display-only data driven), the remaining
    agent_* family plugins (the native history scanner covers part of their discovery surface),
    workspace-integrated session management (orchestrator), live_sessions, remote agent launches,
    git-worktree creation in the dialog, screen/zellij/remote scanning, and any identity beyond
    process-name matching. `docs/terminal.md`; R12-3/R13-2.
85. **Remote authority has landed I/O, LSP, watch, search/replace, explorer writes/clipboard,**
    **plugin authority OS, jump box, large-file editing, hot-exit/trust/reconnect and saved-session**
    **restore; residual remote work is open.** R12–18 landed slot/SSH/helper FS/editor/LSP/watch/
    finder/search/replace/git/preview/env/auth/jump-box/explorer writes/ignore/watch/large-file
    editing/session restore. Round 19 adds: plugin gvim.fs/gvim.proc through the workspace authority
    (trust gate/env/cwd; local-only LookPath/Dirs/Getenv/Now), remote hot-exit staged locally +
    authority recovery, remote trust record keyed user@host[:port]:workspace root, reconnect cache
    refresh/one notice, and explorer clipboard cut/copy/paste/duplicate (bounds/atomic/refusals/
    partial-copy reporting). OPEN: >64 MiB save/Fresh host recipe, search preview head-only >256 KiB/no semantic-LSP spans/
    ignore filtering, explorer drag/move/undo/whole-subtree transaction/mode-mtime-hardlink-xattr/
    existence race/overwrite prompt/watch latency, remote marker scan, hot-exit local-staging
    divergence, remote hot-exit status, ~user/special/sudo, reconnect/terminal respawn, per-remote
    trust store edge cases, devcontainer/k8s, SSH known_hosts accept-new/OTP/keyring, saved-session
    conflict detection, restored terminal dead/plugin FS local? now authority, ownership, real SSH
    UNTESTED (R13-3/R13-17/R13-19/R13-21/R13-22/R13-23).
86. **i18n extraction is landed through round 19; a remaining English boundary stays open.** en is
    972 keys, every non-English locale 569; all-locale key+value 565,760 B (553 KiB), embedded JSON
    655,193 B (≈640 KiB; cap 640 KiB), catalogue heap en 112 KiB / de 82 KiB / both 195 KiB;
    i18n_check green. Round 19 wired settings action notice keys and reviewed/extracted remote large-
    file status, remaining prompt validation, settings values, palette chrome/picker footers,
    deliberately-English constants and gvim-only keys; docs inventory/budgets refreshed. OPEN: rows
    without Fresh keys, structured field/row schema names, some keybinding help/plugin-manager
    edges; `terminal.restore_live` config-only; self-update commands absent. R13-5.
87. **Workspace/lifecycle: registry v2 + shared runtime/window scope + remote session restore +**
    **extract variants + multi-client/auto-attach; #19b still open.** Rounds 16–18 landed registry v2
    co-tenants/authority_spec, per-workspace plugin state, jump box, shared runtime for LOCAL daemon
    workspaces, remote session restore and extract variants. Round 19 adds: full per-mount chrome
    scope editor/window/buffer + focus announcer (active_window_changed/focus_gained, split/buffer
    ids), per-workspace project plugin config applied on activate (only changed plugins reload;
    project layer never writes editor-wide toggles), cross-workspace plugin waits stay/resume in
    owning workspace and cancel on close, daemon multi-client attach + auto_attach (default off;
    smallest-client size; incompatible version rejected). OPEN #19b: cross-workspace plugin id
    namespacing (ids per workspace), remote/authority workspaces own runtime, hard kill after switch
    may lose last debounce, shared buffers/daemon open-path map, eviction/multi-client? landed but
    eviction/workspace CLI open, per-workspace project config edge cases, auto_attach default/named-
    daemon nuances differ Fresh, extract terminal-in-multi/mixed selection, drag/drop/cross-
    workspace undo, large-file overlay extract, all-tabs-of-file substitute, orchestrator/live_
    sessions/agent_* ports (R13-1/R13-14/R13-24).
88. **Reference and workspace state are separate; the round-15 extract slice does not add sharing**
    **(Fresh's shared-buffer/co-tenant model stays open).** Registry stores identity + editor
    layout per workspace and the per-workspace session directory stores stashes/terminal record;
    open-file detects materialized and dormant owners and materializes the owner (single-writer v1
    across dormant workspaces). Extract-tab now roots a new workspace at the file's folder and
    carries its unsaved stash, but two workspaces still do not share a document, undo history or
    buffer; Fresh co-tenant workspaces over one root and extract into an existing workspace remain
    open #19b work. `docs/architecture.md` §8.1; R13-1/R13-14.
89. **Terminal-tab scoping is model-scoped, not Fresh's window model.** Each materialized workspace
    owns a dock/tab set; switching swaps models (never tab lists), so tabs cannot bleed across
    workspaces. Tabs reorder within the strip (phase 2: palette rows, modified-key chords, mouse
    drag, persisted order); Fresh's per-window tab sets, live tab restore and drag-to-split/
    per-tab splits are still open gaps (R13-2).
90. **Discovery is read-only; Import is registry-limited.** tmux scanning runs no session and
    attaches nothing; probable-agent rows are process-name matches, not identity. Phase 2 adds a
    read-only bounded external-history scan (Claude Code/Codex transcripts, fixed resume
    templates, strict ids, no argv from transcript bytes) merged with the tmux findings. Import
    creates/reuses the workspace of the session path (registry v1 cannot give a tmux session its
    own workspace identity or initial attach argv), repeat imports focus the existing tab,
    restored attach tabs come back dead, and dock/agent rows outside the picker, other
    multiplexers/remote scanning, blocked/done heuristics and worktree creation remain open
    (R13-2).
91. **Remote file I/O is a POSIX-helper implementation; round 15 closes search/explorer/env but**
    **leaves the rest open.** The FileSystem is bounded `sh` helper commands (read 64 MiB, dir 5000
    entries/4 MiB, helper stdout/stderr 64 KiB, 500 ms poll watch) requiring POSIX sh/head/cat/
    chmod/mv/mkdir/dirname/readlink/mktemp and GNU/BSD stat; the polled Watch bridges into the
    root watch pipeline; editor file I/O has no local fallback. Round 15 adds remote project
    search/git grep (authority one-shot, remote env, caps/stream/cancel, no local fallback), a
    read-only remote explorer over the same FileSystem, and remote env capture. OPEN: remote
    explorer write ops/.gitignore/git marks/watch, remote search Replace/scopes/preview, remote
    git marks/preview/finder refresh, remote large-file/hot-exit, ~user/special/sudo, reconnect
    edge cases/terminal respawn, per-remote trust store, devcontainer/k8s, remote finder
    round-trip performance (R13-3/R13-8/R13-11/R13-13…R13-18).
92. **Remote LSP over SSH now carries the remote env; retry/limits residuals remain.** Servers start
    on the remote host through the LongRunning surface (remote root, remote login env + captured
    `env.Remote` delta after round 15, stderr tail, process limits recorded not applied because the
    local pid is the ssh client; spawn gate runs before the spawner) and reconnect restarts
    previously running servers once each without duplicates; stopped/muted stay stopped. OPEN:
    retry/backoff beyond one restart, remote install probe, remote trust store, remote process-
    limit enforcement, remote env status indicator/interactive-shell capture residuals (R13-3/
    R13-18).
93. **The spawn gate is the process-level closure; its residual evasions are open.** `DecideSpawn`
    runs in the local authority (Run/Start/Launch), plugin OS, LSP manager and discovery, resolves
    bare names through the child's effective PATH and executes the resolved path, checks explicit
    and symlinked paths, and inspects the shell `-c` first token; Blocked refuses all, Restricted/
    Undecided refuse project-internal executables. OPEN: compound/quoted/variable-built shell
    commands are not parsed, Windows PATHEXT/drive-case normalisation, an environment injected
    below the three seams, no kill on demote, and the remote path keeps the coarse app rule
    pending a remote trust store (R13-4).
94. **TUI scope boundary and genuine-equivalence policy.** Fresh's TUI functionality is the goal;
    non-TUI surfaces (GUI/desktop, browser --web, WASM, packaging, telemetry, self-update install)
    are objective boundaries in §8, not backlog gaps. Explicit gvim extensions beyond Fresh are
    allowed and listed in §8: remote .gitignore/watch and extract variants are extensions, not Fresh
    gaps. After round 19 OPEN TUI work includes: cross-workspace plugin id namespacing and remote/
    authority workspace runtime sharing (R13-24); remote large-file >64 MiB save/Fresh host-patched
    recipe (R13-22); remote explorer drag/move/undo/whole-subtree transaction/mode-mtime-hardlink-
    xattr/existence race/overwrite prompt/watch latency/Windows paths (R13-13/R13-21); remote
    search preview head-only/no semantic-LSP spans/ignore filtering (R13-11/R13-23); remote replace no
    rollback/undo (R13-11); remote authority/session conflict detection/plugin marker scan/
    ownership (R13-3/R13-19); SSH known_hosts accept-new/host-key algorithms/OTP/keyring/real host
    (R13-17); remote env (R13-18); completion residual UX (R13-7); shared buffers and extract
    terminal-in-multi/mixed selection (R13-14/R13-1); live-restore full backing file/remote state
    (R13-15); input GPM/kitty/legacy release/reflow (R13-16/R13-6); diff_nav (R13-9); update self-
    update/terminal buffer (R13-12); URL packaged repo/marketplace (R13-20); plugin ports/composite/
    gallery; theme background/drag; auto_attach default/named-daemon differences; universal server;
    Windows ConPTY. Native equivalents only where genuinely user-facing with residual gaps; every
    other incomplete TUI feature stays open.
95. **LSP completion safety/auto-popup/multi-buffer/trigger/live-filter are landed; residual UX**
    **gaps open (R13-7).** Landed: no-LSP buffer-word provider + local-first merge; snippet
    expansion (tabstops/defaults/first choice/final stop/escapes); UTF-16 additional edits;
    resolve; atomic version+overlap-guarded insert; round-15 auto-popup (`completion_popup_auto_
    show`/`quick_suggestions`/`quick_suggestions_delay_ms`, debounced generation/path/version
    guard) and multi-buffer words (64 KiB middle window per other tab, ≤8, tab order, merged below
    active words); round-16 advertised trigger characters open immediately when
    `suggest_on_trigger_characters` is on (static+dynamic union), live word-character re-filter
    without a new request (case-insensitive label/insert prefix, synthetic range, dismiss+one re-
    ask when empty) with version/caret guard; active-buffer dabbrev cycling (M-/) already existed.
    Tests: completion trigger/live-filter/auto/multi-buffer/settings-live. Round-16 residuals were
    Backspace/row identity; Backspace full-pool re-filter and row identity landed r21; non-word
    close+pass-through matches Fresh (popup.rs/e2e), not a gap; remaining: multi-buffer dabbrev,
    large-file deferral, multi-cursor, request-id bookkeeping. `lsp/`, `editor/completion_words.go`, `internal/tui/{lsp,
    completion_words}.go`, `docs/lsp.md`.
96. **Remote finder + git marks + palette preview + index refresh are landed; residual data-**
    **surface gaps (R13-8/R13-23).** Remote launch uses an idle index then enumerates only
    `authority.Files()` (no local fallback; local project/preview/git/search ports cleared), palette
    slash paths, ~/… real-shell injection-safe expansion, root errors/truncation, 20k/50k bounds.
    Round 16: remote git letters from porcelain-v1 -z via authority one-shot (remote cwd + env,
    8 s/8 MiB, 2 s cache, reconnect invalidate, local git never consulted; A/M/D/U/R/C with
    rename-new-path/unmerged=C/typechange=M/ignored-omitted approximations); palette preview
    reads the selected remote file head via `ReadRange` (min(requested,64 KiB)+1, honest truncation,
    whole-file fallback, disconnected error, never local); finder index Restart is an in-place
    cancel/await/re-enumerate with generation guard and reconnect restart, File Explorer: Refresh
    re-enumerates on demand (full re-walk, not incremental). OPEN: palette file rows carry no git
    letters (local rows do not either), project-search panel preview column unwired, refresh is a
    full re-walk, an open palette keeps old rows until re-rank/reopen, a preview cached before
    disconnect may linger, no full fake-slot buildRuntime reconnect test, and the walk is one
    authority ReadDir exec per directory (not Fresh's server-side streaming walk).
97. **diff_nav (Diff Chunk Navigation) is partially covered; its residuals are recorded (R13-9).**
    Fresh's built-in `diff_nav` plugin merges three hunk sources — `git_gutter_hunks`,
    `live_diff_hunks` and `getBufferSavedDiff` — sorts/dedups them and adds Next/Previous Change
    with translated command/status keys (`refs/fresh/plugins/diff_nav.ts`,
    `plugins/diff_nav.i18n.json`; `refs/fresh/docs/features/git.md` §Diff Chunk Navigation).
    gvim's `git_gutter` now merges the git/HEAD regions, the `live_diff` hunks and the buffer's
    saved-diff ranges, dedups/sorts and wraps, with deterministic tests; that covers the merged
    navigation user behaviour while `git_gutter` is enabled. Remaining gaps: large-file mode is
    refused instead of navigating available sources; Compare-view hunks are not a source; and
    the commands/notices are English strings inside `git_gutter` instead of a standalone
    `diff_nav` plugin with Fresh's translated `cmd.*`/`status.*` keys (line-based targets vs
    byte positions is a smaller untested edge). `docs/plugins/bundled.md` describes the merged
    surface; this item records the exact residual surface rather than claiming full coverage.
98. **Direct launches are refused while a daemon hosts the workspace (R13-10 closed; deliberate
    divergence).** Round 12 allowed file/`--simple`/`--diff`/stdin launches to stay direct while a
    daemon hosted the same workspace, so two editors could race the same file. Round 14 adds
    per-root/per-name daemon host marks; all direct modes (including explicitly named daemons) now
    refuse with the daemon names/sockets and `gvim -a`/`gvim --attach=NAME` instructions, a daemon
    refuses to start if its mark cannot be written, and a refused direct launch leaves the daemon's
    unsaved work/disk file untouched (tests). This is a deliberate divergence from Fresh, which can
    allow a second editor because it shares per-file buffers; gvim will revisit when it has that
    ownership. Not an open gap anymore.
99. **Remote search now covers current-file/open-buffers scopes and a preview column; replace/**
    residuals stay open (R13-11/R13-23).** Project Search/Git Grep run on the host through one-shot
    in the captured remote env (trust gate, rg→grep probe, remote cwd, no shell/local fallback,
    streamed batches/caps/cancel, local query validation); Project Replace remains atomic/stale-
    skip/budget/cancel with no cross-file rollback/undo. Round 18 adds: current-file and open-buffers
    scopes search the editor's remote document snapshots (unsaved edits included, no remote process/
    local disk) with the same caps/batching/validation; the panel preview column uses open-buffer
    text or a file head (≤256 KiB) through authority ReadRange/whole-file with honest truncation and
    stale/disconnect guards. OPEN: preview is head-only over 256 KiB with no semantic/LSP spans (the bounded head is syntax-highlighted since r26/r28; r34 adds regression tests) and over
    256 KiB; remote .gitignore/candidate filtering is not applied to the search list; remote replace
    no rollback/undo; dirty buffers skipped. Tests remote_search/replace/project_search.
100. **Remote explorer has git letters, preview, refresh, writes, ignore/watch AND clipboard;**
     **drag/move/undo open (R13-13/R13-21).** Lazy authority-ReadDir tree (commands only, stale/
     generation, keep/prune, hidden/natural, bounds, trust gate, reconnect reload), remote git
     letters, palette preview, finder Restart/reconnect/Refresh, New File/New Directory/Rename/
     Delete (filesystem mutations, fsops names inside root, existence/target-exists checks, trust
     gate, disconnected error, permanent-delete confirmation naming target, queued reload), remote
     .gitignore + polled directory watch as gvim extensions. Round 19 clipboard: cut/copy/paste/
     duplicate through authority ReadDir/ReadFile/Mkdir/WriteFile(Atomic), binary-safe, bounds
     10k entries/256 MiB/64 levels/2 min, cut copy-then-remove, paste refuses existing name,
     duplicate Fresh free-name rule, symlink/root/outside/inside-source refusals before creation,
     target-naming confirmations, trust gate/disconnected/refresh, cancellation/failure keeps and
     reports the partial destination. OPEN: no drag/move between directories, no undo/history, no
     whole-subtree transaction/cleanup UI, FileEntry/Mode/mtimes not restored, hardlinks/xattrs not
     preserved, copy/paste/rename destination-exists refusal only a pre-write check (concurrent create overwritten; no exclusive destination create), no overwrite prompt, watch latency/rename
     approximation, palette rows no git letters (local too), Windows paths, real-host acceptance.
101. **Extract variants: co-tenant + existing workspace + terminal saved-state + multi-select**
     **(R13-14/R13-1; the last three are gvim extensions — Fresh only has extract-to-new-workspace).**
     Co-tenant same-root extract (round 16, switch-before-close safety) plus round 18: (a) extract
     into an existing same-root workspace via picker (dormant target merges FileState/stash into its
     session; materialized target opens through restore and re-captures; source tab closes only after
     target acceptance; refused switch keeps source tab and the copy stays in target; failed post-
     close says text kept in target); (b) terminal tab extracted to existing workspace by moving its
     saved record (transcript/styled snapshot/launch dir/live flag; no cross-runtime PTY hand-off;
     target appends dormant panel, source shell stops after acceptance, revive on first dive); (c)
     multi-select file tabs to one new co-tenant (all states/stashes first, switch then close each by
     path; changed mid-action skipped with text in target). OPEN residuals: terminal tabs not in
     multi-select/mixed selection, no drag/drop/cross-workspace undo, large-file overlay extraction
     refused, all-tabs-of-file has no Fresh action (multi-select substitute).
102. **Live restore now carries a styled visible-screen snapshot and dock modes (R13-15).**
    `terminal.json` v1 gains an additive bounded `screen`: ≤200×512 cells with rune/fg/bg
    `#rrggbb` + bold/underline/reverse, trailing blanks trimmed, control chars→spaces, invalid
    colours dropped on load; restore overlays it on the saved text tail only on an exact row-text
    match (mismatch/corrupt ignored whole; old records keep plain transcript). A revived tab paints
    the snapshot above new shell output; restored/exited tabs open read-only `Scrollback
    (restored)`/frozen, Ctrl+Space toggles pager/live when a shell is attached, first dive revives
    into live mode; tab identity/order/revive-once/restore_live unchanged. OPEN: no backing-file
    bytes, only visible screen styled (older history text-only), cursor/alternate-screen/OSC/
    hyperlinks not restored, revive still needs explicit dive, remote launches restore no state.
103. **Input-edge slice is landed with backend limits (R13-16).** Plugins receive
    `chrome_focus_changed` (editor/explorer/dock/section; tabs is a gvim extension; no menu/
    overlay/initial event; native sections region=section) and `key_release` (only backends
    reporting `tea.KeyReleaseMsg`, e.g. Windows console; legacy terminals silent; no kitty event
    flags requested/faked). Legacy normalization: uppercase-without-Shift infers Shift (Ctrl
    unaffected), CapsLock/lock bits ignored, named keys keep reported Shift; AltGr-as-Ctrl+Alt
    with one printable rune is text input (bound chord wins). Tests: input_edges_test, docs/
    keybindings.md. OPEN: GPM console mouse, kitty keyboard, legacy key-release unavailable,
    terminal-window focus only for remote reconnect, live-screen reflow.
104. **SSH keepalive/auth + config/ProxyJump/agent forwarding are landed; limits remain (R13-17).**
    Keepalive flips a silent carrier to disconnected and reconnects; background retry capped
    backoff/one dial; passphrase/keyboard-interactive 2FA prompts; status reconnecting. Round 17
    adds a bounded OpenSSH config parser (Host patterns/negation, HostName/User/Port/IdentityFile,
    256 KiB/4096-byte line/256 Host caps, token expansion, target overrides), one-hop ProxyJump
    through the same client, and agent forwarding for terminal/one-shot channels (x/crypto/ssh).
    OPEN: known_hosts default `~/.ssh/known_hosts` only/no accept-new, no host-key algorithm config,
    ProxyJump one hop, no keyring/agent secret storage, keyboard-interactive one-answer-per-question
    OTP policy unchanged, keepalive/backoff package vars not settings, real SSH/agent/2FA UNTESTED.
105. **Remote env provider is landed with residuals (R13-18).** `env.Remote` probes configured
    markers and runs the activation recipe on the REMOTE host through the authority one-shot
    surface (never local env/overlay), diffed against the remote login baseline; the captured
    delta drives remote terminal/shell/formatter/LSP/search/git-grep; trust-gated and warmed in
    the background (spawn paths use cached delta, Update never blocks on the host). Tests:
    remote_env_test (remote-only runner, end-to-end, local overlay not forwarded, remote LSP env).
    OPEN: probe uses `sh -c`/remote login baseline rather than the interactive remote login shell,
    env input files not watched (Reload/activate re-captures), cached until refresh/reconnect,
    Settings Environment rows show remote facts only after the warm, no status-row indicator of
    the active remote env (Fresh has one).

106. **Remote project replace is landed with atomic writes; no cross-file rollback/undo (R13-11).**
    The confirmation names the remote target; each candidate is read through authority Files,
    local Query/PlanReplace semantics apply, and the write is temp+rename with mode preserved;
    trust gate before first I/O; stale search-time hash or size/mtime change skips; per-file 4 MiB,
    per-run 32 MiB/512-file/20 s; cancellation between files and inside authority calls; any
    error aborts naming the file and the count already written. OPEN: no cross-file rollback/no
    undo for direct writes (undo across a remote run is the recorded residual), dirty open buffers
    are skipped, remote .gitignore/large-file filtering is not in the candidate list; local
    preview/git adapter stay cleared for remote launches.
107. **Registry v2 co-tenant workspaces + same-root extract (R13-1/R13-14).** Version-2 workspace
    files allow several workspaces per canonical root with stable-id identity, Fresh label
    precedence (`proj`, `proj (2)`...), root-grouped ordering, v1 load/migrate-on-save, dedup only
    identical stable id, independent layouts/session dirs; extract now moves a tab into a
    co-tenant over the SAME root and sequences switch-before-close so a refused switch keeps the
    tab/text and drops the added co-tenant; failure after the queue keeps the new workspace (text
    in its session) and reports it; second-capture guard. OPEN: eviction/memory for warm
    co-tenants, multi-client, registry `authority_spec`/initial-launch fields, extract into an
    existing workspace/terminal-tab/large-file/multiple, shared buffers/daemon open-path map,
    per-workspace plugin state, workspace CLI, capture cancellation; daemon-hosted remote
    authority/jump-box (R13-19).
108. **URL themes are landed with cache/policy/last-known-good; packaged repo/marketplace open**
    **(R13-20).** A theme value may be https (or loopback http) fetched under the update transport
    policy with a 256 KiB/8 s cap, existing parser/validator, cache under the user config dir
    keyed by URL hash, last-known-good offline fallback, clear failure notice, no fetch for
    builtin; `--theme` accepts the same forms and checks policy; Settings can select a URL already
    fetched at this start (uncached reported, not fetched in Update); a `repo#fragment` packaged-
    repository URL is refused with the explicit message. OPEN: packaged repository install/
    marketplace/browser, startup-only URL fetch/cache refresh, remote packs/local archives,
    background images/drag-and-drop/inheritance edges.
109. **Remote data surfaces landed: git letters, palette preview, finder refresh (R13-23/R13-8).**
    Explorer git letters from remote `git status --porcelain=v1 -z` one-shot (remote cwd+env,
    8 s/8 MiB, 2 s cache, reconnect invalidate, local git never consulted; A/M/D/U/R/C with
    rename-new-path/unmerged=C/typechange=M/ignored-omitted approximations); palette head preview
    via authority `ReadRange` (min(requested,64 KiB)+1, honest truncation, whole-file fallback,
    disconnected error, never local); finder index in-place Restart (cancel/await/generation
    guard, done channel closed), reconnect restart and File Explorer: Refresh (full re-walk).
    OPEN: palette rows carry no git letters (local rows do not either), project-search panel
    preview column unwired, refresh is full re-walk not incremental, open palette keeps old rows
    until re-rank/reopen, preview cached before disconnect may linger, no full fake-slot
    buildRuntime reconnect test; finder walk still per-directory round trips.
110. **Authority FileMutations + remote explorer write UI/clipboard are landed; write residuals**
     **(R13-21).** FileMutations (Mkdir/Rename/Remove; local + SSH helpers), New File/New Directory/
     Rename/Delete UI (fsops names inside root, existence/target-exists checks, trust gate,
     disconnected error, permanent-delete confirmation naming target, queued reload), and round-19
     cut/copy/paste/duplicate over authority (see item 131). OPEN: drag/move between directories,
     no undo/history, no whole-subtree transaction/cleanup UI for partial copies, FileEntry/Mode/
     mtimes not restored, hardlinks/xattrs not preserved, copy/paste/rename destination-exists refusal only at check time (concurrent create can be overwritten; no
     exclusive create), no overwrite prompt (collision refused), watch latency, Windows paths,
     real-host acceptance.
111. **Remote large-file mode is EDITABLE and saves via a STREAMED atomic write (r20; 150;**
     **R13-22).** Overlay over untouched windowed base (4 KiB head/large-index loop); save plans
     through Save/SaveAs in a tea.Cmd: remote stat/CheckDisk, windowed source walked in 1 MiB
     windows with overlay applied, merged bytes streamed through authority WriteStreamer
     (mktemp in target dir, bounded chunks appended, mode applied + one mv; local equivalent),
     arbitrary size with bounded memory; failure/cancel aborts (temp removed, target untouched);
     changed remote refuses before stream open; access without streaming keeps old 64 MiB merge-
     and-whole-file fallback; fresh windowed source rebased after save. DIVERGENCE: Fresh uses a
     server-side patched recipe (unchanged parts copied on host); gvim streams client-merged bytes
     to a remote temp then renames — same large-file outcome, host recipe later. OPEN: server-side
     patched recipe, whole-file transport cap for non-windowed loads, large-file syntax highlight
     off, real-host acceptance.
112. **Update command surface landed; self-update and update terminal buffer open (R13-12).**
    `{update}` is clickable to Update details (feed/current/latest/last-run/release page); palette
    240 gains Check for Updates (forced check ignoring daily debounce) and Show Update Output
    (details list). OPEN: self-update download/install/prompt, Fresh "Update fresh" self-update
    command, Fresh update terminal buffer (gvim shows a details list), release-page click starts
    no install; telemetry/GUI/browser --web/WASM/packaging are objective boundaries.

113. **Remote explorer write operations are wired (124; R13-21 partial).** New File, New
    Directory, Rename and Delete run on the remote tree through the authority Files/Mutations with
    fsops name validation inside the remote root, existence checks, target-exists refusal, trust
    gate before every mutation, the explicit disconnected error, permanent-delete confirmation
    naming the remote target, and a queued remote reload after success. Cut/copy/paste of subtrees,
    drag/move and duplicate stay refused as a capability; tests remote_ops_ui/remote_explorer_ops.
    OPEN: no undo/history, check-to-write race, remote .gitignore/watch/symlink/Windows, real host.
114. **Daemon-hosted remote authority/jump box is landed (125; R13-19 partial).** A daemon start
    with a remote target hosts that remote workspace locally; registry `authority_spec` persists the
    canonical target, the foreground remote connects on the daemon terminal (password/passphrase/
    2FA prompts, actionable non-interactive failure) before hosting, `--attach` ignores the target
    because the daemon owns SSH, restart reuses the same identity, and dormant remote workspaces
    connect lazily from their spec (invalid spec keeps current workspace). Tests daemon_remote
    (offline fakes). OPEN: remote tabs/stashes not restored across daemon restart, plugin gvim.fs/
    gvim.proc still local OS in a remote workspace, remote trust uses local store, per-file
    ownership vs direct editor, real SSH UNTESTED.
115. **Per-workspace plugin state is landed (126; R13-1 partial).** A daemon workspace's
    `gvim.state` is `sessions/<stable-id>/plugin-state.json` (versioned v1 object), restored on
    materialize, flushed atomically on close/exit, 1 MiB/plugin + 8 MiB/file caps with skip-warning
    over-bound and read-only newer versions; co-tenants/workspaces isolated; enablement/settings/
    manager toggles editor-wide. OPEN: Fresh's one editor-wide runtime + plugin-window-scope chrome
    and focus announcer (task 132, R13-24); current runtime is per workspace and UI registrations are
    per-model; shared runtime rebind/teardown tests not done.
116. **Remote large-file editable overlay + bounded save is landed (134; R13-22).** See item
     111: r17's windowed read-only mode is superseded by editable overlay editing and a bounded
     (64 MiB) merge-and-atomic whole-file save; remote stat/CheckDisk change checks, save in
     tea.Cmd, fresh windowed source rebased after save. OPEN: >64 MiB/Fresh host-patched recipe,
     large-file syntax highlight off, real-host acceptance.
117. **SSH config/ProxyJump/agent forwarding are landed (128; R13-17 partial).** Bounded `~/.ssh/
    config` parser (patterns/negation, HostName/User/Port/IdentityFile, ProxyJump, IdentityAgent,
    token expansion, 256 KiB/4096-byte/256-block caps, target overrides, malformed clear error), one-
    hop ProxyJump through the same client, x/crypto/ssh agent forwarding for terminal/one-shot.
    OPEN: known_hosts default only/no accept-new, no host-key algorithm config, ProxyJump one hop,
    no keyring, OTP retry policy, keepalive/backoff package vars, real SSH/agent UNTESTED.
118. **i18n remainder 2 is landed (129; R13-5 partial).** Plugin-manager rows/notices, theme/
    settings action notices, agent-discovery chrome/dynamic notes, LSP picker details, remote
    search/replace confirms, palette chrome/footers/prompt validation and settings values. Metrics:
    en 909 / 563 per non-English locale / 545 KiB text / 631 KiB JSON / heap 103-81-185 KiB. OPEN:
    docs/i18n.md boundary inventory stale after 129, rows without Fresh keys, structured schema
    names, keybinding help, plugin-manager edges; `terminal.restore_live` config-only.
119. **Live-restore styled screen snapshot + dock modes are landed (131; R13-15 partial).** See
    item 102: additive bounded styled `screen` in the v1 terminal record, exact-tail restore,
    corrupt ignored, read-only Scrollback (restored)/frozen modes with Ctrl+Space toggle, revived
    tab paints snapshot above new output. OPEN: no backing bytes, visible-screen styles only,
    cursor/alt-screen/OSC/hyperlinks missing, explicit dive, remote no state.
120. **Full plugin window-scope chrome + focus announcer + editor-global ids landed (141/149;**
     **residuals open; R13-24).** Per-mount chrome scope editor/window/buffer; focus announcer
     active_window_changed/focus_gained diffs workspace label + focused split window id + active
     buffer id; buffer activated/deactivated; switch seeding; widget focus widget_event. Editor-
     global plugin window/buffer ids: each model gets a per-mux sequence on Bind (direct=0), API
     exposes seq<<40|local, host entry points decode/refuse foreign or released with CodeClosed,
     returns/events use global ids, ReleaseWorkspace frees a closed/evicted sequence. Project
     plugin config on activate (only changed plugins reload; project layer never writes user
     toggles) and cross-workspace waits token→owning model (stay/resume/cancel). OPEN: 40-bit local
     id hard cap; editor-scoped overlays model-local; editor-scoped widget instance state rebuilt
     not cloned; buffer-bearing mount cannot be editor-scoped; remote/authority workspaces own
     runtime; hard-kill last debounce.
121. **Remote daemon-workspace session restore across restart is landed (135; R13-19 partial).**
     Daemon-hosted remote workspaces capture tabs/open files/unsaved stashes/cursor/layout on exit
     and restore them on restart after the stored authority spec reconnects (tabs/active/cursor/
     pane layout/explorer-sidebar state/unsaved text); vanished file keeps stash + normal notice;
     failed reconnect restores nothing and `keepSavedSession` skips capture so the saved session
     survives; detach/reattach keeps live state; co-tenants isolated; direct remote no session.
     OPEN: no remote conflict detection beyond stash/mtime, restored terminal transcript dead/no
     process re-run, plugin authority OS closes the FS-local residual (r19); local trust store/
     ownership, real SSH UNTESTED.
122. **Remote .gitignore filtering + directory watching are landed (136; gvim EXTENSION, NOT a**
     **Fresh parity gap).** Same go-git parser as local: remote root+ancestors/nested .gitignore
     through authority Files (64 levels, 64 KiB/file, 4096 rules); ignored hidden by default/
     revealed by show-gitignored; finder filters through same batch per index lifetime. Remote
     watching reuses polled authority Watch: loader adds listed dirs, refresh via normal debounce/
     coalescing off Update, cancel on disconnect/restart on reconnect; unsupported backend gets one
     manual-refresh notice. OPEN: poll latency (~500 ms+debounce), rename-as-remove+create
     approximation, ignore edits reach explorer next reload/finder next index restart, Windows
     client paths. Fresh's remote explorer does not expose this local-style behavior.
123. **Extract variants are landed as gvim extensions (137; R13-14).** (a) extract into an existing
     same-root workspace picker; (b) terminal tab saved-record migration to another workspace (no
     PTY hand-off, revives on first dive, source shell stops after acceptance); (c) multi-select
     file tabs to one new co-tenant. Fresh only has extract-to-new-workspace; these are extensions,
     not Fresh gaps. OPEN: terminal tabs not in multi-select/mixed selection, no drag/drop/cross-
     workspace undo, large-file overlay extraction refused, all-tabs-of-file has no Fresh action
     (multi-select substitute).
124. **i18n remainder 3 + docs boundary refresh (138; R13-5 partial).** Extracted settings/action
     notices, agent-discovery chrome, LSP picker details, palette chrome/footers, prompt validation,
     settings values, remote write strings/large-file status. OPEN: remaining rows without Fresh keys,
     structured schema names, keybinding help/
     plugin-manager edges.
125. **Remote search current-file/open-buffers scopes + preview column are landed (139; R13-11).**
     Remote current-file/open-buffers search the editor's remote document snapshots (unsaved edits
     included) with local caps/validation, no remote process/local disk; panel preview column uses
     open-buffer text or head ≤256 KiB via authority ReadRange/whole-file, honest truncation + stale/
     disconnect guard. OPEN: preview head-only >256 KiB with no semantic/LSP spans (bounded head is syntax-highlighted since r26/r28); remote
     .gitignore/candidate filtering not applied to search list; replace no cross-file rollback/undo.
126. **Plugin gvim.fs/gvim.proc through the workspace authority (142; R13-19/R13-3).** Remote or
     daemon-hosted remote workspaces wrap the plugin OS with pluginhost.NewAuthorityOS: file
     reads/writes/stat/readdir/mkdir/remove/rename/watch and one-shot/stdio processes go through
     the workspace slot with authority trust gate/env/cwd and remote captured env; local unchanged.
     LookPath/Dirs/Getenv/Now documented local-machine APIs; copy/realpath/walk and mutations the
     backend lacks return clear unsupported errors; remote fs watch is polling with startup
     degradation notice. Tests fake authority + local decoy. OPEN: real SSH untested; local-only
     APIs deliberate boundary; authority watch support per backend.
127. **Remote hot-exit/recovery, trust record and reconnect resilience (143; R13-3).** Direct remote
     launch stages unsaved remote-document text under <cache>/gvim/remote-sessions/<hash>/session
     keyed by remote target (daemon remote uses workspace session dir), remote paths recorded;
     recovery installs after FileAccess, reopens through authority, clean exit clears staging.
     Remote trust recorded in trust.json via trust.RecordKey user@host[:port]:workspace root with
     LevelForRecord/SetRecord/OpenRecord, survives restart/reconnect without re-prompt; local keys
     unchanged. Reconnect refreshes watch/finder/git/explorer/LSP and shows one notice with restart
     count/failure. OPEN: local-staging divergence from Fresh authority-routed recovery; remote
     executable marker scan not wired (undecided remote opened markerless, records level); real
     host untested.
128. **Per-workspace project plugin config + cross-workspace plugin UI waits (144; R13-1/R13-24).**
     On activate project plugins.enabled/settings layer compared; only changed plugins unload/
     load/reload with dependants; reads/compiles Work, one entering pump; project layer never writes
     editor-wide plugin-state toggles. Waits token→owning model: A's wait stays in A across switch,
     B cannot reuse, resumes in A, close A resolves cancelled+notice. OPEN: hard kill after switch
     can lose last debounce window; remote/authority runtimes not shared; real host untested.
129. **i18n remainder 4 (145; R13-5 partial).** Settings action notice keys wired; remote large-file
     status, prompt validation, settings value labels, palette chrome/picker footers, deliberately-
     English constants and gvim-only keys reviewed/extracted; docs inventory/budgets refreshed.
     Metrics en 972/569 per locale/553 KiB text/≈640 KiB JSON/heap 112-82-195 KiB. OPEN: rows
     without Fresh keys, structured schema names, some keybinding help/plugin-manager edges;
     restore_live config-only; self-update commands absent.
130. **Daemon multi-client + auto-attach (146; R13-1 partial).** Several attach clients share one
     session: frames broadcast to all, input from any, smallest attached client size wins, detach
     removes only that client, slow client bounded-write detached, unknown frame drops only that
     client; incompatible protocol version rejected before state change. Auto_attach top-level
     user knob default OFF (Fresh on): bare same-root launch joins daemon with notice, never starts
     a daemon, never remote (explicit -a required), named-daemon ambiguity refusal kept. OPEN:
     gvim per-root named daemons vs Fresh shared-daemon nuances, default difference by design, no
     fork/daemonize, Windows pipes, eviction/workspace CLI.
131. **Remote explorer clipboard (148; R13-13/R13-21).** Cut/copy/paste/duplicate over authority:
     copy walks ReadDir/ReadFile, writes Mkdir + WriteFile(Atomic), binary-safe; bounds 10k
     entries/256 MiB/64 levels/2 min; cut copy-then-remove after full success; paste refuses
     existing name; duplicate Fresh free-name rule; symlink/root/outside/inside-source refusals
     before creation; confirmations name remote target; trust gate/disconnected/refresh; partial
     destination kept and reported on cancel/failure (no rollback/deletion). OPEN: no drag/move,
     no undo/history, no whole-subtree transaction/cleanup UI, FileEntry/Mode/mtimes/hardlinks/
     xattrs not preserved, copy/paste/rename destination-exists refusal only at check time (concurrent create can be overwritten), no overwrite prompt, real host.
132. **Full plugin window-scope chrome + focus announcer (141; see item 120/R13-24).** Per-mount
     editor/window/buffer chrome scope, active_window_changed/focus_gained announcer with split/
     buffer ids, buffer activated/deactivated, switch seeding; OPEN per-workspace id namespacing
     (ids not globally stable across switches), remote/authority runtime exclusion, hard-kill
     debounce window, editor-scoped overlays/widget instance state model-local.
133. **Editor-global plugin window/buffer ids (149; R13-24).** Shared-runtime ids seq<<40|local,
     per-mux sequence on Bind/direct sequence 0, host entry points decode/refuse foreign or
     released ids with CodeClosed instead of silently acting, returns/events use global ids,
     ReleaseWorkspace frees a closed/evicted sequence; colliding local ids across workspaces get
     distinct globals; cached id after close rejected; panel/event tests updated. OPEN: 40-bit hard
     cap; editor-scoped overlays model-local; editor-scoped widget instance state rebuilt not
     cloned; buffer-bearing mount cannot be editor-scoped.
134. **Streamed remote large-file save above 64 MiB (150; R13-22).** authority WriteStreamer/
     WriteStream (SSH mktemp+bounded chunks+mode/mv; local temp+rename; Abort removes temp, target
     untouched until commit; only atomic/non-append/no-parents). Document save walks windowed
     source in 1 MiB windows, applies overlay, streams merged bytes with bounded memory; failure/
     cancel aborts; changed remote refuses before stream open; access without streaming keeps 64
     MiB fallback. OPEN: Fresh server-side patched recipe; non-windowed whole-file cap; syntax
     highlight off; real host.
135. **Cross-workspace shared buffers (151; R13-1/#19b).** Daemon LOCAL workspaces share one
     canonical owner + read-only mirrors; ownership moves on focus/switch with version-guarded
     change propagation/full reload; close/evict DropEditor promotes a mirror (unsaved survives);
     r21 tab close rules (owner promotes mirror, mirror detaches, final view prompts); r22 OpenLoaded
     adoption/conflict label; r24 conflict Save As guard/new-path sharing/owner rekey (item 152).
     Direct/remote independent; large/virtual/>8 MiB independent. OPEN: sharing starts OpenFile/
     OpenLoaded but not plugin/API-created tabs; daemon open-file request switches owner; mirrors
     keep separate cursor/view unless ownership moves; plugin seams see a new doc id after handoff;
     Save As plan-to-write reservation closes the previous plan-check→async-write race (in-process; dropped-plan leak/non-gvim writers/disk-exists semantics residuals); dormant/evicted owner path unknown until rematerialize.
136. **Materialized-workspace cap + LRU eviction (152; R13-1).** User-only daemon.
     materialized_workspaces default 8 range 1..64 (project refused); LRU non-active local;
     capture session first (ordered writer, synchronous fallback), then teardown; registry/session/
     plugin-state retained for rematerialize; active never; capture+fallback failure keeps over
     cap and reports then next candidate; concurrent select/close refused; exit save serialized;
     status notice names evicted workspace/cap. Remote/authority workspaces excluded from cap (own
     SSH runtime).
137. **SSH known_hosts policy/host-key algorithms/keyring (153; R13-17).** User-config top-level
     ssh keys (project refused): known_hosts_policy strict/accept-new (first-seen normalized line
     0600, parent 0700, notice)/off (parsed only to be refused; verification cannot be disabled);
     changed key always refused; host_key_algorithms ordered safe defaults (ed25519/ecdsa/rsa-
     sha2; ssh-rsa/DSA never preferred), unknown/empty clear error pre-dial, duplicates first,
     ProxyJump+target, verified key type in connection detail; keyring default false (PasswordStore
     before prompt; accepted user password stored with notice; store error accepted-but-not-
     stored). OPEN: mac/Linux keyring backends no-op today, 2FA answers not stored, negotiated
     signature algorithm not separately exposed, real hosts UNTESTED.
138. **i18n remainder 5 (154; R13-5 partial).** Extracted SSH reconnect/refresh failure suffix,
     LSP restart count plural (TN), remote restore/trust display, workspace trust warning, plugin
     focus/scope notices, remote explorer clipboard/recovery notices, remaining settings/status;
     docs inventory/budgets refreshed. Metrics en 998/575 per locale/560 KiB text (under 640 KiB
     cap)/≈649 KiB JSON/heap 114-82-197 KiB. OPEN: rows without Fresh keys, structured schema
     names, some keybinding help/plugin-manager edges; restore_live config-only; self-update
     commands absent.
139. **Live-restore backing remainder (156; R13-15).** Styled `screen` adds bounded 200-row/512 KiB
     history window, cursor row/col/visible/shape, alternate-screen flag; exact visible/history
     row-text match or ignored whole; old records unchanged; re-wrap drops metadata; read-only
     view paints history styles + reversed cursor; first dive hands live cursor/screen; daemon
     path. OPEN: scrollback bounded styled not byte-exact (deep history text-only); OSC not
     replayed; per-cell hyperlinks not representable; read-only cursor block; alt content marker
     not replayed; explicit dive; remote no state.
140. **Exclusive remote create (157; R13-21).** authority.Creators(fs)/CreateNew: local
     O_WRONLY|O_CREATE|O_EXCL; SSH POSIX noclobber set -C; : > "$p", so file/dir/dangling symlink
     at the path → fs.ErrExist without follow/truncate/replace; Mode chmod after create, chmod
     failure reports but never deletes the path; Append/Parents → ErrUnsupportedFileIO; backend
     without surface refused explicitly, no Stat+WriteFile fallback; explorer New File uses it.
     OPEN: rename/copy still use pre-mutation existence checks and can race an intervening create
     (no exclusive rename/copy in mutation surface).
141. **Shared-buffer close behavior (158; R13-1).** View-vs-buffer rule: mirror-tab close detaches
     without unsaved prompt; owner-tab close with a mirror promotes canonical doc first (unsaved+
     undo preserved) and asks nothing; final view (no mirror) keeps save/discard/cancel prompt and
     forced close removes registry entry; CloseByPath detaches victims up front. OPEN: only OpenFile
     starts sharing, OpenLoaded/startup independent, daemon open-file request switches owner,
     editor-level Close from exiting runtime does not promote (daemon close/evict uses DropEditor),
     no two TUI splits/panes, view position only on ownership, large/virtual/remote independent,
     mirror plugin seams new doc id after handoff.
142. **Remote plugin walk/copy (159; R13-19).** gvim.fs.walk depth-first lexical over authority
     (hidden/skip-dirs/depth/entries semantics, symlinks reported never followed, unreadable dirs
     skipped, cancellation); gvim.fs.copy one file or tree remote-to-remote with 16 MiB/file + entry
     caps, tree symlinks refused, cancellation honoured. Destination no-overwrite is only a pre-write existence check; a concurrent create can be overwritten (check-to-write race; no exclusive destination create). realpath landed r22 via authority RealPather
     (requires remote realpath/readlink -f and POSIX helper environment; missing tool is a clear io
     error, absolute answer/existence semantics, real-host UNTESTED). OPEN: realpath tool/POSIX
     dependency; copy does not preserve directory modes/mtimes/metadata, cancelled/failed tree copy
     may leave partial destination; gvim.dirs/env.get/lookpath/clock local-only; remote watch polling
     with startup notice.
143. **Completion Backspace re-filter (160; R13-7).** Backspace deletes via normal editor action and
     re-filters complete stored pool (server+buffer words, not visible rows) by shorter prefix with
     no server request; hidden candidate reappears when prefix widens; synthetic range recomputed;
     highlighted candidate identity preserved across typing/Backspace, fallback first only when
     filtered out; type-empties dismiss+one reask, Backspace-empty dismiss no request; post-edit
     document version captured; apple/atom regression. Non-word characters close the popup and pass
     through, matching Fresh (popup.rs + e2e), so this is not a gap; remaining residuals are
     MRU/dabbrev/large-file/multi-cursor/request-id.
144. **Remote plugin realpath (162; R13-19; gvim extension).** gvim.fs.realpath via authority.
     RealPather: SSH one bounded realpath/readlink -f over the carrier, canonical absolute remote
     path, never local; requires tool (missing → clear io, no guessed fallback), absolute answer
     enforced, cancellation honored, explicit resolved-path existence check (missing final/broken
     symlink → not_found matching local EvalSymlinks), LF/CR/all-whitespace names byte-exact (only
     single trailing protocol newline stripped), loop/permission errors keep tool message as io.
     Residual: remote host must have realpath/readlink -f and be POSIX helper environment; missing
     tool is a clear error not fallback; real-host UNTESTED.
145. **Non-word/punctuation completion behavior verified (163; R13-7; not a gap).** Fresh popup
     filters alnum/underscore only; other printable keys close+pass-through (e2e space/semicolon/
     parens/equals/braces/comma/plus/dot); gvim matches and tests. Advertised trigger + auto_show +
     suggest_on_trigger_characters on → insert, close old list, start FRESH completion request (no
     carried selection identity); trigger setting off → no request; other non-word cancels pending
     debounce. Remaining residuals: MRU/dabbrev/large-file/multi-cursor/request-id.
146. **Shared-buffer startup/OpenLoaded restore (164; R13-1).** OpenLoaded adoption: unmodified
     restored copy becomes read-only mirror; restored UNSAVED copy differing from live owner becomes
     separate untitled [shared conflict] tab (path removed so it cannot overwrite owner; disk
     untouched), normal tab mirrors owner; adoption deferred to workspace focus because stash text
     applies after opening; materialized-while-another-focused loads independently until Focus;
     daemon stale-stash test. R24 label persistence: session.FileState optional label; both session
     writers (restore.WriteSession and app workspace_capture) save a pathless modified buffer's
     label; RestoreSession restores it, so eviction/rematerialize/daemon restart/repeated restore
     keep [shared conflict] <name> (not Untitled-N) while text/pathless safety and owner/disk remain
     untouched). Residual: sharing starts OpenFile/OpenLoaded but not plugin/API-created tab; daemon
     open-file request switches owner; editor-level Close/exiting runtime no promote; no TUI
     splits; view position only on ownership; large/virtual/remote independent; mirror plugin doc-id.
147. **Remote rename race (165) NOT landed.** Safe no-overwrite rename could not be implemented
     without regression; original rename kept, no new regression. Rename/copy check-to-write race
     OPEN: explorer copy/paste/rename/duplicate and plugin gvim.fs.copy destination no-overwrite only
     at check time (concurrent create can be overwritten); only New File exclusive create race-free.
148. **Completion labelDetails/documentation verified (167; R13-7; not gaps).** Fresh popup_actions
     labelDetails.detail/description rendered dimmed next to bare label, fallback item.detail (e2e
     lsp.rs:10286); gvim lsp/requests.go merges same into Detail, parser+popup tests; Fresh never
     renders completion documentation (async_handler.rs:478-481; resolveSupport(documentation) only
     for rust-analyzer resolveProvider), gvim same. Old residuals removed; docs/tests corrected.
149. **Multi-buffer dabbrev (168; R13-7).** dabbrev_expand active words nearest-first then other
     tabs in TAB ORDER, bounded middle window 64 KiB/buffer, max 8 tabs, 30 candidates, cycle/
     restore prefix/one undo/smart-case/dedupe; large/virtual other buffers skipped; active large
     bounded backward scan. Residual: buffer order tab order not Fresh MRU (same as popup words; no
     editor-wide MRU list).
150. **Remote search ignore scope (169; R13-11/R13-23).** Grep fallback uses Fresh project-file
     scope: git rev-parse --is-inside-work-tree + bounded git ls-files -z -s --cached --others
     --exclude-standard; only listed results enter batch; tracked files kept even if gitignored,
     tracked dotfiles listed, untracked ignored absent, untracked hidden dropped; nested repos by
     prefix without inner ignore rules; outside worktree hidden-only; failed/truncated listing
     degrades to hidden-only (no guessed gitignore/notice). Residuals: rg path applies own ignore
     rules (tracked ignored omitted vs Fresh/grep); tool output cap + panel Limit apply before
     filter so result can be below Limit; nested-repo inner ignore; degradation no notice; finder
     snapshot authoritative; finder index still backend walk; no local fallback.
151. **Plugin fs.copy streaming (170; R13-19/R13-21).** With streamed-write surface (local/SSH),
     each file copied in 1 MiB ReadRange windows into atomic WriteStream: one-chunk memory, no
     16 MiB cap, destination untouched until Commit (existing target survives failure/cancel, temp
     aborted), cancellation between chunks; no-stream backend keeps 16 MiB whole-file fallback;
     tree 100k entries; source shrink io/destination untouched; growth beyond initial size not
     copied (residual); destination no-overwrite still only pre-check (concurrent create can be
     overwritten; not a guarantee); metadata/tree partial destination residual; real host UNTESTED.
152. **Shared restore/conflict-copy + Save As reservation (175/178; R13-1).** Deterministic
     overwrite fixes in editor/shared.go + save_plan.go: (a) conflict-tab Save As to another
     materialized owner's path refused at plan time even with force (CheckSaveAs; closing mirror no
     bypass); (b) Save As to a NEW path registers canonical shared buffer (AfterSaveAs); (c) owner
     Save As rekeys (mirrors follow; old path reopens independently from untouched disk); (d)
     read-only mirror Save As refused. Conflict label persists through session FileState.label (both
     writers/RestoreSession) across evict/rematerialize/restart/repeated restore. R25 plan-to-write
     reservation (task178): SharedBuffers.ReserveSaveAs(e,d,path) acquired in Editor.PlanSaveAs on
     the UI goroutine BEFORE the write command; while held other workspaces cannot Open the target
     ("being saved by <workspace>"), cannot Register as owner (OpenLoaded/plugin guard), cannot plan
     another Save As to it; same editor's second save gets "already in progress". SavePlan.reservation
     is released by ApplySavePlan on success/write failure/cancellation; SavePlan.Release is the
     explicit hook for a dropped plan; SharedBuffers.DropEditor releases claims of a closed/evicted
     workspace. This closes the plan-check→async-write window, extending Fresh's one-buffer-per-file
     save_target_refusal across gvim's off-UI write command. Tests: reservation blocks other
     workspace/released on failure/cancel/explicit release/dropped editor/concurrent single winner/
     async-window no independent copy; editor conflict Save As/owner rekey/mirror refusal; session
     round-trip label; app repeated restore. OPEN: reservation is in-process only; a dropped
     SavePlan without Release/DropEditor leaks the claim until the editor/daemon goes away;
     OpenLoaded/plugin-created tabs for a reserved path still LOAD (only prevented from Registering
     as owner), sharing starts OpenFile/OpenLoaded only; disk exists/force semantics unchanged
     (RefuseExisting decided when write runs); reservation does not protect a non-existent path on
     disk against non-gvim writers; a dormant/evicted owner path is unknown until rematerialize;
     daemon open-file request switches owner; mirrors keep separate cursor/view; large/virtual/remote
     independent; mirror plugin seams get a new doc id after handoff.
153. **Completion/dabbrev MRU order (172; R13-7).** Editor keeps tab-activation history
     RecentTabIndices active-first keyed by document identity (not tab index); close/move keeps
     surviving order, closed document dropped, editor without history falls back tab order. Popup
     multi-buffer words and dabbrev_expand scan other tabs in Fresh MRU order (active first);
     bounded 64 KiB/8 tabs/30 candidates, cycle/restore/one-undo/smart-case/dedupe unchanged.
     Tests activation/close/move MRU + bounded dabbrev. Remaining completion: large-file deferral,
     multi-cursor, request-id bookkeeping.
154. **Remote search candidate consistency (173; R13-11/R13-23).** Both tools share Fresh project-
     file scope: complete bounded git ls-files --cached --others --exclude-standard authoritative;
     grep fallback ordinary grep + candidate filter; rg runs --no-ignore --hidden -g !.git then
     filtered, so tracked-ignored files searchable on both tools (rg no longer omits them);
     .git kept out. Outside worktree grep hidden-only/rg defaults. Failed/truncated listing degrades
     hidden-only without notice; tool cap + panel Limit before filter can give <Limit and widened rg
     can consume cap; nested repo inner ignore; finder snapshot authoritative; no local fallback.
155. **Remote plugin copy tree safety (174; R13-19/R13-21).** Tree destination root atomically
     claimed via authority DirectoryCreator (mkdir without parents, no mkdir -p): concurrent
     directory after precheck → conflict, no merge/overwrite. Local/SSH implement; backend without
     capability keeps check-then-mkdir -p (residual). Parents before claim; failure after claim can
     leave partial destination. Descendant files use atomic per-file WriteStream; single-file copy
     keeps local replace-existing-regular-file semantics (atomic temp/commit), dir at file target
     refused, source shrink io/destination untouched, growth beyond initial size not copied, no-
     stream backend keeps 16 MiB fallback. Tree cap 100k entries; no-overwrite atomic guarantee only
     for tree ROOT, not every descendant; metadata not preserved; real host UNTESTED.
156. **Dashboard action line targets (179; R9-1).** Action rows incl. Git branch-log publish hidden
     command line targets owned by dashboard plugin; mouse click uses same dispatcher as keyboard;
     target cannot dispatch another plugin command. Branch-log only when git_log registered, else
     "branch log unavailable (enable git_log)" non-clickable. Repository URL plain text; no safe
     URL-open action (shell deliberately avoided). OPEN: clickable URL, weather/GitHub/network,
     third-party section API, Utility Dock placement, sessions; not closed by no-network/dependency;
     icon/style differences acceptable.
157. **Remote search explicit candidate batches + notices (180; R13-11/R13-23).** Complete git
     listing → candidates passed explicitly in bounded batches (400 paths/48 KiB paths/command, max 64
     commands): tracked incl tracked-ignored/dotfiles + untracked non-hidden; tool never scans ignored/
     hidden trees so ignored cannot consume cap/limit; -- separator, one -e query, cancellation/
     timeout between batches, merged 8 MiB cap/truncation, regex engine unchanged + local re-check,
     candidate filter drops outside scope. Nested repo/submodule or >64 lists keep widened whole-tree
     rg + notice ignored-scanned/results may be capped; unavailable/failed/truncated listing keeps
     tool defaults + notice "remote ignore scope unavailable ... tracked-ignored may be omitted";
     non-git no notice; Notice in panel hint clears on new search never fails; finder snapshot
     authoritative. Residuals: complete listing required; defaults can omit tracked-ignored
     (notified); widened scan for too-large/nested (notified); limit after merged batches → <Limit; no
     POSIX-ERE prefilter; finder snapshot/backend walk; no local fallback.
158. **Terminal exit status + marker (181; R13-15).** On reap dock records ExitStatus (0/nonzero/
     -1 signal); marker [Terminal process exited] after frozen screen into saved/restored/restarted
     transcript (restart/resume below it, never overwrite); tab title (exited, exit N)/(exited,
     signal), clean 0 plain (exited) matching Fresh; restored read-only title same; written once per
     exit/restart dedupe; not painted from styled snapshot. Additive exit_code/exit_known; old records
     restore exactly unknown/no invented marker; bounded ordinary row; top-level mirror active.
     Divergences: no status-bar restart indicator (palette/menu Restart only; status-bar element
     OPEN), no program-name inference, marker at capture/save/restart rather than exit instant; remote
     no state.
159. **Shared-buffer MRU handoff rank (183; R13-1/R13-7).** When shared document identity is replaced
     in transferLocked/adoptPendingLocked, new identity inherits replaced document's MRU rank so
     handoff keeps recency position in popup/dabbrev MRU order; RecentTabIndices document-identity
     bookkeeping unchanged; bounded tests cover handoff ranking.
160. **asm_lsp reduced port (187/197; R13-6).** New bundled Lua plugin `asm_lsp` (off by default,
     no bindings/settings, default set/idle heap unchanged), reduced from Fresh `plugins/asm-lsp.ts`:
     on .asm/.nasm/.s assembly buffers (sweep of open buffers at plugins_loaded), with no project
     .asm-lsp.toml and no global asm-lsp config, an action popup offers to create the project config;
     detects assembler (gas for .s/gas; MASM via proc/endp/assume/invoke/.model/.code/.data/.stack;
     else NASM) and ISA (ARM64/RISC-V tokens, else x86/x86-64); r27/task-197 creates asm-lsp
     [default_config] TOML with an EXCLUSIVE create (`gvim.fs.write(path, data, {exclusive=true})`)
     at workspace root .asm-lsp.toml (non-GAS adds default_diagnostics=false), never atomic-overwriting
     an externally created file; then runs native lsp_restart. Exclusive create: local plugin OS opens
     O_EXCL; remote authorityOS maps `authority.Creators(FileSystem).FileCreator.CreateNew` then writes
     the data; append/parents are invalid combinations; a backend without FileCreator returns
     `unsupported` (no racy Stat fallback). If another process won the path, asm_lsp reports
     “Not overwriting <path>: it was created by another process” (or an explicit create-refused
     status) and never runs lsp_restart. Choices: create detected / another named dialect / dismiss /
     never-ask-per-project in gvim.state. On lsp_server_error naming asm/assembly on an asm buffer,
     status install hint; LSP status click opens help popup with copyable cargo install/binstall
     asm-lsp, native Start/Restart LSP Server and Show LSP Status, Dismiss. Commands Assembly:
     Create .asm-lsp.toml / Show LSP Help. Tests: manifest/commands/no bindings; offer+created NASM/
     GAS; MASM/ARM64; skip project/XDG config; never-ask; install help/clipboard/native restart;
     non-assembly ignored; popup→external file→confirm exclusive-create does not overwrite (no
     lsp_restart); io_fs/pluginhost local+authority exclusive rows. OPEN vs Fresh: no Disable
     Assembly LSP plugin action (native LSP Stop/Disable; no plugin API to disable server), per-
     language restart replaced by native lsp_restart active language, gvim lsp_server_error lacks
     language/server/error_type so errors claimed only on asm buffer + message naming asm/assembly,
     global-config check through workspace FS (remote checks remote home, not local), overlong project
     root truncates the gvim.state never-key. Exclusive-create residuals: local writes directly with
     O_EXCL (not temp+rename), so a process crash mid-write can leave partial content; remote claims
     via CreateNew then uses a non-streamed/non-atomic WriteFile for the data, and failure removes the
     claimed file only when authority mutations exist (otherwise leaves an empty file); exclusive is
     once-only create (no append/parents) by design. docs/bundled.md now 24 plugins; docs/porting.md
     records reductions/fix. Remaining language helpers/other bundled ports OPEN.
161. **Safe URL open + dashboard links (184; R9-1/R13-6).** gvim.url.open: http(s) only; reject
     other schemes/credentials/whitespace-control/hostless-opaque/>2048B with invalid BEFORE prompt;
     modal Yes/No confirmation names URL (y/n/arrows/Enter/Esc, default No); returns queued, only Yes
     starts opener; platform opener one argv/no shell/not configurable/off update loop; start failure
     status; unsupported platform; remote workspace opens local browser. Dashboard GitHub origin
     normalized to https://github.com/owner/repo; repo/branch clickable via own action targets +
     keyboard Open Repository/Open Branch, branch URL <repo>/tree/<branch>; non-GitHub https repo row
     no branch URL; unsafe/unopenable plain. Residual: only http(s), no non-GitHub branch URL, opener
     not user-configurable; weather has since landed opt-in (r30); other GitHub/network sections/etc OPEN; icon/style differences acceptable.
162. **Exited-terminal status-bar restart indicator (185; R13-15).** {terminal_restart} paints
     ⟳ Resume <program> (resume_agents on + non-empty resume argv) / ⟳ Restart <program> (launch
     argv) / ⟳ Restart terminal (plain shell), plus (exit N) only non-zero (0 omitted) or (signal);
     program = basename resume preferred/launch argv; live exit + restored exited tab; error
     foreground; hover underline+bold; disappears after restart; click = restart_terminal, keyboard
     palette/menu Restart Terminal Process; narrow-row squeeze no overflow. Divergences: program from
     recorded argv not OS foreground; hover style vs Fresh ERROR swap; signal (-1) suffix; marker at
     capture/save/restart not exit instant.
163. **Remote preview syntax highlighting (186; R13-23).** Shared highlighter; up to 256 KiB/4000
     lines; one highlighter per file in 8-entry bounded cache concurrent-safe; CPU-only inside preview
     command, no extra remote I/O; open-buffer preview from panel text, remote head over bounded head;
     over caps/no grammar plain; truncated head may cut multi-line construct (best-effort). Residual:
     >256 KiB preview head-only (including colouring), no semantic/LSP spans, no remote preview buffer
     (Fresh renders real buffer through text pipeline), explicit-candidate path needs complete remote
     listing.
164. **Multi-cursor completion acceptance (188; R13-7).** Extra carets + popup: word/Backspace go
     through normal insert_char/delete_backward, all carets lock-step (one undo group), list re-filters
     primary caret word; non-word same dismissal. Plain candidate accept replaces each caret's own
     word prefix with same text as one atomic edit/one undo; each caret keeps own prefix length/ends
     after inserted text; request-time doc version stale guard. Overlapping replacement ranges refuse
     whole accept/write nothing. Snippet/auto-import remain single-caret: extra carets dismiss + notice,
     nothing written; single caret path unchanged. Residual: multi-cursor snippet/import acceptance
     not implemented.
165. **csharp_support reduced port (190; R13-6).** New bundled Lua plugin csharp_support (off; no
     bindings/settings; commands C#: Restore Project / Show LSP Help; default set/idle heap unchanged),
     reduced from Fresh plugins/csharp_support.ts. Landed: nearest .csproj/.sln walk (20 levels, fallback
     file dir) on .cs open; trust-gated dotnet restore once per project/session via gvim.proc.run
     (authority-following), deferred on trust_changed; status restoring/completed/failed; csharp-ls
     install help on lsp_server_error only for an active C# buffer + error text naming csharp/c#/roslyn/
     omnisharp (gvim payload only {error}); copy install command + native lsp_restart/show_lsp_status;
     two commands. Tests: command table/no bindings; nearest-project restore + once/project;
     untrusted→trust_changed deferral; no-project fallback; restore failure/stderr first line; missing
     dotnet; restore command; help popup/copy/native restart; non-C# ignored. OPEN vs Fresh: no
     setLspRootUri (no plugin API; gvim LSP config/root detection), no Roslyn
     workspace/_roslyn_projectNeedsRestore server-to-client request (no plugin event; open-time restore
     covers common case), no Disable C# LSP action (native LSP menu Stop/Disable), dotnet --version
     precheck replaced by direct run + exec error report, i18n English-only. Remaining language helpers/
     other plugins OPEN.
166. **Multi-cursor snippet/auto-import accept (191; R13-7).** Extra carets accept snippet/
     additionalTextEdits with per-caret rule: each caret replaces own word prefix with expanded
     snippet text, final stop is RUNE offset into that text (lands inside own insertion), auto-import
     edits applied exactly once in same atomic edit; combined edit list validated (inverted/overlap,
     insert inside replaced range, two zero-width inserts same point refuse whole accept with
     ErrOverlappingEdits/nothing written); one undo + request version guard. Resolve-based item still
     refuses extra carets. Single-caret path unchanged.
167. **Remote paste conflict flow (192; R13-21).** Remote paste/duplicate honours explorer conflict
     choice: Cancel explorer.ErrConflict/no change (Esc drops rest of multi-item paste); Rename
     prefilled “Paste as on SSH:…” prompt then CopyAs/MoveAs (validated, same policies); Overwrite
     removes existing destination first then copies (cut removes source only after success); Skip
     leaves destination/report path (cut returns source/cancels cut). Local Overwrite/Skip unchanged;
     Duplicate same-directory Fresh free-name (1000 tries), never prompts. Write-phase cancel/I/O
     keeps partial destination + count/path; gvim never deletes remote data on failed copy. OVERWRITE
     IS REMOVE-THEN-COPY, NO ROLLBACK: a process re-creating destination between removal and copy is
     a documented race (file copy atomically replaces it because user chose overwrite; directory
     copy fails at Mkdir not silently replacing); decision not remembered across multi-item paste;
     Fresh Overwrite All/Skip/Skip All not implemented (bounded per-item). No local fallback.
168. **Mixed file+terminal extraction (193; R13-14; gvim extension).** Palette picker lists open file
     tabs + dock terminal tabs, type-labelled; target chooser new co-tenant first then existing same-
     root workspaces. New target: one session (all FileStates/stashes) + one terminal record (all
     SavedTabs) written before switch queued, then source file tabs closed by path/terminal by identity
     (highest index first, saved-name checked) only after switch accepted, re-capture, report “Extracted
     N tabs into workspace”. Existing target: read-only conflict pass (selected file already in target
     session/open editor; non-empty terminal name in target record/dock) sends ExtractConflictMsg
     BEFORE writes; confirmation names target/conflicts: Yes replaces target file state + ADDS same-name
     terminal tab (never replaces), No cancels untouched. Transfer ordering accepted→switch accepted→
     source close; acceptance failure “N of M tabs transferred, sources kept”, changed source skipped/
     counted. Residuals: large overlay/welcome/closed-during-capture refuses whole; co-tenant-only
     (other root refused); no PTY handover (revive on first dive); empty saved-name tabs outside name-
     conflict and can duplicate; no undo/drag target/cross-workspace shared buffers; Fresh itself only
     extracts a single tab.
169. **Real SSH keychain backend (194; R13-17).** ssh.keyring on (default off) uses macOS
     /usr/bin/security (find/add-generic-password, service gvim-ssh, account user@host; argv/no shell;
     password -w argv briefly process-visible, recorded) or Linux secret-tool over Secret Service/DBus
     (lookup/store; password stdin never argv; needs keyring/DBus, honest failure). Used only when
     exec.LookPath finds it, else no-op store and “not stored in the OS keyring” (never success).
     Lookup before prompt; store only after user-supplied accepted password; errors honest; argv/no
     shell/bounded 64 KiB/env/context; one entry user@host (no port; resolved alias host), trailing
     newline stripped/other bytes preserved (embedded/trailing LF/CR included; tool round-trip limit).
     Passphrases/2FA/keyboard-interactive never keyring-stored; fake runner tests (no real keychain/
     DBus); real hosts UNTESTED.
170. **Configurable update command in dock terminal (195; R13-12).** New user-layer-only
     update_command (gvim extension; project setting refused; trimmed max 4096 bytes/no NUL). When
     set, palette Update fresh/Show update output rows appear; otherwise hidden and raw actions keep
     “gvim does not update itself”. Update fresh runs command in new dock tab “Update” via user's
     shell ($SHELL/-c or /bin/sh; cmd.exe /C Windows), flags/shell syntax work; output kept after exit,
     second run restarts same tab keeping previous transcript above, Show update output returns
     (before run: no output yet); start/dock failure status/dock; non-zero exit visible/no editor
     effect. Independent of check_for_updates; no Settings row (user preference); runs on gvim machine
     not remote authority; close_on_exit removes exited tab so output not kept (default keeps scrollback
     read-only). No download/install/self-update engine; bounded equivalent, not Fresh updater.
171. **i18n remainder 6 (198; R13-5).** New pure-English strings catalogued with English byte-
     identical: update_command missing/running/no-output/dock-failed, gvim.url.open confirmation/
     unsafe/cancelled/error/opened, terminal restart/resume + signal, palette preview degraded
     notices, asm_lsp config-refused (gvim-only fallback). docs/i18n.md + budgets refreshed; de
     fallback/key-guard tests. Catalogue en 1015 / de 579 keys, 564 KiB key+value (under 640 KiB
     cap), ≈654 KiB embedded JSON; loader heap en116/de82/both198.
172. **clangd_support reduced port (204; R13-6).** New bundled Lua plugin clangd_support (off; no
     bindings/settings; commands Clangd: Switch Source/Header / Project Setup / Open Project Config;
     default set/idle heap unchanged), reduced from Fresh plugins/clangd_support.ts. Landed: switch
     source/header as bounded same-stem lookup (C c/cc/cpp/cxx ↔ h/hh/hpp/hxx/hp) in buffer dir,
     workspace root and include//src/ children; one match opens, several use gvim.ui.select, no-match/
     unsupported/no-active-file statuses. Project setup analysis (project root; compile_commands.json
     in root/build/out/cmake-build-debug or missing+cmake/bear tip; .clangd or .clangd/compile_flags.txt
     present/missing; CMake/Bazel/Makefile hints; general tips) shown in a reused read-only virtual
     buffer. Open Project Config finds .clangd in buffer dir/workspace root or reports missing. Tests:
     command table/no bindings; source→header open; header→source picker; no-match/unsupported; project
     setup ready+missing (buffer reuse/hints); open-config found/missing. OPEN vs Fresh: no
     textDocument/switchSourceHeader (gvim LSP allowlist refuses server-specific methods; heuristic
     workspace-local same-stem only, no include-database-aware switch), no textDocument/clangd.
     fileStatus or $/memoryUsage custom notifications (no plugin server-custom-notification event/status
     line), a virtual buffer instead of a floating PanelManager panel, .clangd search only buffer dir/
     workspace root (no ancestor walk, matching Fresh's targets). Remaining language helpers/other
     plugins OPEN.
173. **Restricted plugin HTTPS GET + GitHub PR section (199; R13-6/R13-24).** gvim.http.get(url,
     opts?): user layer plugins.<name>.settings.network=true host-enforced (project never grants);
     only https/no credentials; SSRF localhost/.local/loopback/private/link-local/multicast/
     unspecified/CGNAT/doc-benchmark literal refused, DNS all addresses public/failure final, literal
     dial, env proxy ignored, TLS1.2 floor; redirects <=3 revalidated; timeout default5s clamp100ms-
     10s; body<=1MiB Content-Length+limited reader; <=2/plugin,4 global; memory-only; no caller headers
     (fixed UA/Accept JSON, no Authorization/Cookie), safe response headers only; awaitable Work no UI
     block; nil,err invalid/timeout/too_large/io/unsupported. Dashboard opt-in github_prs+network: GitHub
     origin one bounded api.github.com pulls request, <=6 open PRs; 404/failure/disabled muted line;
     weather opt-in (r30 wttr.in j1 via restricted HTTPS; muted failures)/third-party API absent/no generic fetch/auth/cache.
174. **Remote interactive-login env capture (200; R13-18).** Marker detection stays passive sh -c
     (untrusted-safe, no project rc); trusted baseline/recipe runs under user's interactive login shell
     when available (bash -ilc/zsh -ilc, prefer $SHELL), matching terminal rc initialization; absent/
     failure falls back to sh -c with explicit startup/Settings notice. Startup output discarded via NUL
     marker; one remote command per baseline/recipe. Residuals: bash/zsh only; rc unavailable/fails
     fallback notice; detection still sh -c (semantics can differ); env files not watched (Reload/new
     activation); cached until Refresh/reconnect; Settings rows after warm; no status-row active-env
     indicator; local unchanged.
175. **Remote finder git candidate index (201; R13-8/R13-3).** Remote git work tree quick-open fills
     from ONE bounded git rev-parse --is-inside-work-tree + ONE bounded git ls-files -z -s --cached
     --others --exclude-standard (8MiB/20s, authority one-shot) via additive finder.StartFunc, replacing
     recursive ReadDir + client remote .gitignore reader on that path; tracked incl tracked-ignored +
     untracked non-ignored; emit filters .git/hidden/intermediate skip/invalid/non-UTF8/depth; keeps 50k
     cap/truncation/cancel; Restart/refresh reruns listing. No-work-tree/git missing/fail/truncated/
     nested-by-name fall back to recursive authority walk (bounded dirs/depth, skip dirs/hidden, no
     symlink, remote gitignore); root read failure = index error. Residuals: Quick Open hidden filter
     omits tracked hidden; fallback uses client ignore reader; nested contents only fallback; non-UTF8
     omitted; truncated falls back not partial; no local fallback.
176. **Terminal OSC title + alternate-screen restore (202; R13-15).** OSC0/2 title (control/invalid
     UTF8 dropped, trim, <=256 runes, empty clears; OSC1 icon only) title additive v1 per tab+top,
     restored keeps, old records derived label; precedence rename > launch program/agent > recorded/live
     title > Terminal N; corrupt/overlong cleaned. Alt content captured first-class visible frame
     (styles/cursor/no history); alt+history corrupt ignored whole; restored read-only paints alt frame;
     revived shell own main screen; main history while alt not captured; bounded snapshot. Residuals:
     OSC1/OSC7/OSC52/hyperlinks not replayed; per-cell hyperlink unrepresentable; cursor block/shape not
     inherited; no foreground process title recombination; live tabs need dive.
177. **Child-side kitty keyboard protocol (203; R13-15/R13-6).** Emulator answers CSI ? u with current
     flags; tracks CSI > flags u push, CSI < n u pop, CSI = flags;mode u set/add/remove, separate main/
     alt stacks; bounded 64/128 bytes/partial split PTY reads; unknown ignored. Nonzero flags encode
     sent keys CSI-u (disambiguation/Esc/modified printable/Enter/Tab/Backspace/Ctrl+I/Alt+[; arrows CSI
     A/F3 CSI13~; alt-keys/report-all/associated-text fields); unmodified text/Enter/Tab/Backspace legacy;
     no flags→legacy. Presses only (no releases/report-event-type), modifier-only/media/F13+ not encoded;
     host kitty stays Bubble Tea; flags session-reset on restart, per-screen restored.
178. **Dashboard GitHub PR clickable rows (208; R9-1/R13-6).** PR rows are action rows in dashboard
     mode (Tab/Down/Up focus, Enter activates) and publish dashboard-owned action line targets
     `dashboard.click_pr_<n>`; both paths dispatch a hidden dashboard command then the existing
     gvim.url.open (http(s)-only scheme/credentials/control policy + modal Yes/No confirmation, no shell
     opener). The URL is NOT taken from untrusted REST data: it is rebuilt as
     `https://github.com/<owner>/<repo>/pull/<number>` from the git-remote-derived slug and a strictly
     validated positive integer number (1..999999999; string/fractional/negative/huge/missing skipped;
     html_url and other API links ignored). Titles: string-only, control chars→space, whitespace
     collapsed/trimmed, empty/non-string→(untitled), 200-rune truncation; draft only == true. At most 200
     entries examined and 6 valid rows kept; opt-in/network/unavailable unchanged. Tests: clickable
     targets + click + keyboard activate canonical URL; untrusted fields ignored/invalid numbers/titles;
     row cap 6 from 40. Residuals: only canonical github.com/<slug>/pull/<n> accepted (API-provided/
     cross-repo/enterprise links not used), title normalization/truncation, no author/label/review
     fields, confirmation per open, fixed 6/200 limits.
179. **Kitty child-protocol corrections (206; R13-15).** Real-PTY audit fixed: UV ModMeta→protocol
     Super(bit8), ModSuper→protocol Meta(bit32); functional keys extend to lock/media/keypad,
     report-all-keys modifier-only, F13-F35 protocol numbers, no-number/beyond-F35 legacy; Shift
     inference for legacy uppercase with other modifiers (Alt+Shift+A keeps Shift, Fresh normalize_key);
     omitted push flags=0, full stack evicts oldest, over-pop resets. Presses only/no release event-
     type; alternate-key shifted key without separate base-layout key; host kitty stays Bubble Tea.
180. **Post-reconnect remote shell re-probe (207; R13-18).** Successful authority reconnect re-probes
     interactive shell in background off UI loop, refreshes fallback state/notice; startup failure no
     longer pinned sh -c; later success clears Settings fallback row; same fixed pass-through command
     through authority gate (untrusted refuses), session ctx bounds; updates shell choice/fallback row
     not startup notice; no marker re-detection/overlay recapture; cancelled/disconnected leaves notice
     reflecting attempt. Residuals env files unwatched/cached/settings warm/no active-env status/local
     unchanged.
181. **Remote finder degradation notice (209; R13-8/R13-3).** finder.Index non-fatal Notice() +
     additive StartFuncNotify; git probe/listing failure or truncated listing still falls back to
     recursive walk and carries one explicit English non-fatal notice; plain non-git/nested fallback
     no notice; generation-guarded/cleared on run start/Restart (no stale reconnect); Quick Open shows
     one informative row after results without hiding/disabling; root read failure remains index error.
     Residuals notice English awaiting i18n; fallback client ignore reader; nested only fallback;
     non-UTF-8 omitted; truncated fallback not partial; no local fallback.
182. **Plugin HTTPS IPv6-transition SSRF fix (210; R13-6/R13-24).** Audit found deterministic SSRF gap:
     IPv6 transition/embedding forms encode private IPv4. Now refuses wholly site-local fec0::/10,
     NAT64 64:ff9b::/96 + 64:ff9b:1::/48, 6to4 2002::/16, Teredo 2001::/32, deprecated IPv4-
     compatible ::/96; DNS every-address-public/literal dial/redirect revalidation/proxy removal/
     TLS1.2 unchanged; real security regressions/docs. Remaining plugin HTTP HTTPS GET-only/no generic
     fetch/auth/token/cache/safe response headers/fixed limits.
183. **Terminal title combination (212; R13-15).** Tab label follows Fresh combine_terminal_title:
     explicit rename wins; else recorded launch program (argv[0]/agent) + recorded/live OSC title as
     “command — title” (em dash), unless OSC title case-insensitively names command then title alone;
     part alone as-is/both empty Terminal N. Untrusted cleaned again; v1/old readers ignore/old records
     old defaults/corrupt cleaned. OPEN: recorded launch program not live foreground process (no pty
     foreground cache; later foreground change not reflected).
184. **Opt-in dashboard weather (213; R9-1/R13-6).** plugins.dashboard.settings.weather=true +
     network=true user-layer; optional weather_location (place/coords, empty host IP via wttr.in),
     weather_units metric|imperial; one bounded https://wttr.in[/<encoded location>]?format=j1 (5s
     timeout,10min TTL) via gvim.http; renders now condition/temp/feels/wind/humidity, 5pm nearest
     3-hour sample, tomorrow range/condition; no credentials/caller headers, memory-only, no UI block;
     timeout/offline/malformed/disabled network -> one muted line no notice. Residuals: no multi-day
     beyond today/tomorrow, no location picker (plain string/empty geolocation), third-party section
     API absent.
185. **Terminal exit marker at exit instant (214; R13-15).** Marker becomes transcript last row AT
     EXIT (synthetic while frozen screen shown), pager Ctrl+Space shows immediately/select/copy without
     save; save stores ordinary bounded row; restart captures before new shell/restart-resume below
     never overwrites; tab title status; gvim keeps frozen live view so marker in pager not painted on
     frozen screen; alt-screen no pager message but marker persisted for restore; old sessions
     compatible/exactly once/no duplicate.
186. **Remote env-input watch (215; R13-18).** authority watch .envrc/mise.toml/.mise.toml/.tool-
     versions/pyvenv.cfg/.venv; change invalidates cached detection/capture and background re-warm, no
     local FS; arms root + existing nested input dirs so content-only pyvenv.cfg seen; new nested dir
     re-arms next root event; no-watch backend degrades: watcher stops, Settings rows say not watched +
     Env: Reload, manual reload works; other watch failures bounded backoff like document watch; cached
     until Refresh/SetRoot/env event; async invalidation (in-flight spawn previous delta); Settings after
     warm; no status-row active-env indicator; local unchanged.
187. **Resolve multi-cursor completion (216; R13-7).** ResolveToken + extra carets: acceptance
     snapshots doc version + every caret, closes popup (resolve no UI block), applies only if active doc/
     version/every caret still exactly snapshot; reply via same per-caret atomic path incl resolved
     snippet stop/additionalTextEdits, one atomic edit/undo/version guard; edit/moved/added/collapsed
     caret/switch doc/failed resolve/missing version-editor capability -> whole accept refused/no write;
     single-caret unchanged.
188. **Remote batch-paste conflict dialog (217; R13-21).** Multi-item paste: Choice overlay Overwrite
     (o)/Overwrite All(O)/Skip(s)/Skip All(S)/Cancel(c,Esc); current/next prompt semantics, Overwrite All
     remaining jobs no prompts, Skip only current, Skip All current+remaining stops (cut drops stranded
     clipboard), Cancel keeps completed/leaves clipboard retry, notices count completed, later failure
     stops remaining keeps completed/partial+count; Rename single prefilled CopyAs/MoveAs; Overwrite
     remove-then-copy (cut source after success); adapter Skip direct; local prompt unchanged; Duplicate
     free-name. Divergence: Fresh resolves queue before running, gvim incremental so later Cancel/Skip All
     keeps already-pasted items (no hidden undo); Overwrite All only remaining of that paste; remove-then-
     copy no rollback/re-creation race; no local fallback.
189. **welcome demo reduced port (219; R13-6).** New bundled Lua plugin welcome (off; no bindings/
     settings; commands Plugin Demo: Open Help / Save File / Say Hello / Insert Time / Insert Comment;
     bundled set 26 plugins/9 on/17 off/59 commands; default palette/idle heap unchanged), port of
     Fresh plugins/welcome.ts (78-line plugin-API demo), same five commands. Landed: Open Help/Save File
     dispatch native show_help/save via gvim.command.run; Say Hello/Insert Time/Insert Comment insert at
     primary caret with b:insert (one undo, no auto-indent) + status; time os.date %H:%M:%S; comment
     prefix language-aware (-- Lua/SQL, # Python/Ruby/shell/YAML/TOML/Makefile/Dockerfile, else //);
     insert failures reported. Tests: five command titles/no bindings; native action dispatch; greeting/
     time insertion text+status; Lua vs Go comment prefix; read-only insert failure. Deviations vs
     Fresh: language-aware prefix vs fixed //, local 24h HH:MM:SS vs locale toLocaleTimeString, English-
     only vs 15 locales, no load-time debug status; opt-in off so default palette/heap unchanged.
190. **Remote browser drag/move (220; R13-21).** Left press lifts effective selection (multi-row
     whole); motion over directory/file-parent underlines target; release emits MoveMsg not move. One
     confirmation names entries/count/remote target/destination Move/Cancel; only Move runs drag job
     chain (jobMove, cancel conflict policy, multi, completed count). Own parent/self/descendant filtered,
     no-op no send, drag never touches clipboard, authority-only. MoveAs copy-then-remove: copy
     failure/cancel keeps source+partial destination, source-remove failure keeps both/reports; taken
     destination single/multi conflict dialogs, overwrite only explicit. Local drag disabled (Cut/Paste
     notice); keyboard flow same confirmation/conflict/jobs. Residuals no cross-window/above-root, no
     remote undo/history, no resolve-then-run batch (incremental, later Cancel/Skip no rollback; one
     batch-level confirmation; pending cut untouched).
191. **Trusted remote env marker probe (221; R13-18).** Trust-gated: only trusted workspace runs one
     bounded cancellable sh -c existence probe (markers + require evidence; fixed shell test, no rc
     sourced); untrusted no remote command + explicit Settings notice + no detector; failed/cancelled
     probe clears detection + notice (never local scan/stale), retried next Warm/Overlay not cached.
     Fresh passive scan while trust undecided; gvim detector absent before trust. Activation still
     interactive login shell (+ sh -c fallback).
192. **MRU session persistence (222; R13-7/R13-1).** session.Session.MRU []MruFile {Path,Label}
     additive optional; named doc saved path/pathless label; not tab index/runtime identity; Version 1;
     older session no field keeps restore-derived order. Restore maps refs to new documents before
     multi-buffer completion/dabbrev; missing/renamed/closed/no-longer-open skipped, remaining relative
     order preserved (no phantom/reorder); daemon eviction/restore covered; tests restart/recovery +
     stale/renamed/closed.
193. **i18n extraction round-31 (223; R13-5).** finder degradation, batch-paste counts/stop, resolve
     multi-cursor refusal, weather status, dashboard plugin own i18n; English byte-identical;
     budgets/docs/fallback/key-guard tests. en 1031/de 579, 565 KiB text, ≈655 KiB JSON, heap
     117-82-199.
194. **Terminal OSC 7 cwd persistence (225; R13-15).** Last OSC 7 (plain path/file URL; host
     dropped/percent-decoded) cleaned/bounded: POSIX-absolute <=4096B, UTF-8, no control chars else
     ignored; saved terminal.json tabs cwd additive v1/launch_dir fallback; used for restart/resume/
     dive (transcript above); cleaned again capture/load. Remote saved cwd verified via authority
     Stat (5s, remote stat only, no shell/local); missing/non-dir/disconnected status notice +
     authority default dir; local existing behavior. Residuals Windows-style ignored, remote fallback
     home/status-only, local not prevalidated, point-in-time check, later cd not live-replayed.
195. **Remote delete permanent; copy metadata best effort (226; R13-21).** Trash design audited and
     WITHDRAWN/NOT shipped (no portable atomic same-FS no-replace; no private 0700 dir surface). Delete
     = Mutations.Remove recursive + explicit permanent confirmation (no trash); no journal/undo/
     restore UI/recovery. Copy: source mode to atomic write; FileMetadata backends set mode/mtime files
     then dirs after bytes, best effort; failure non-fatal one-shot warning; backend without metadata
     bytes-only/no claim; per-entry Stat tolerated. Not preserved owner/hardlink/xattr; no
     copy-fidelity guarantee. Gating residual portable no-replace/private trash.
196. **Pre-trust env marker probe (227; R13-18).** Before trust authority command gate refuses shell:
     shell-free authority.FileSystem.Stat per marker/require path (no remote proc/local FS; max
     MaxMarkerProbePaths, 10s shared timeout, sequential, caller cancel) => untrusted read-only
     detection result; activation trust-gated. Trusted: single bounded cancellable sh -c existence
     (fixed test, no rc). Failed probe clears detection + explicit Settings notice (never local/stale),
     next Warm/Overlay retry not cache; no file surface => explicit notice. Activation interactive
     login shell with sh -c fallback.
197. **Remote large-file windowed highlight generation (228; R13-15/R13-7).** Windowed lexing bounded
     lines around screen/few windows; remote only lines whose 1 MiB windows arrived; LineLoaded pure
     cache, unloaded first paint unstyled (no extra remote read); window arrival bumps source window
     GENERATION not text version; highlighter keys windows/long-line segments by generation, repaint
     re-lexes, same loaded window no remote I/O; read-only, no binary read-only flag change, no block.
     Servers still not started for large buffers.
198. **i18n extraction round-32 (229; R13-5).** Move confirmation/notice + resolve multi-cursor
     refusal new copy + retired trash/move keys; English byte-identical; budgets/docs/fallback tests.
     en 1046/de 579, 567 KiB text, ≈656 KiB JSON, heap 119-82-201.
199. **MRU persistence for crash recovery and extraction (234; R13-7/R13-1).** Session MRU/Active
     (r31) now also covers crash recovery and workspace extraction; no session Version bump (additive
     optional fields). Crash recovery: restore.RecoveryWriter session carries Active + MRU (refs limited
     to the unsaved documents it stashes, newest first) and FileState.Label for pathless buffers;
     recoverCrash uses the existing restoreFiles/applySessionMru, so the pre-crash active tab and
     completion/dabbrev MRU order survive an unclean exit. Old recovery copies without Active/MRU restore
     exactly as before (activation/saved-file order); a previous active tab in a running editor is still
     kept (existing behavior). Extraction: new single/multi/mixed co-tenant sessions carry the source
     editor MRU restricted to moved docs (newest first) and set Active to the MRU-first moved doc;
     transfer into a DORMANT existing workspace merges moved refs to the front via mergeExtractMru
     (drops an old ref for the same path, keeps unrelated old refs) and aligns Active; a materialized
     target keeps the normal live capture. Old sessions without MRU unchanged; stale/missing refs skipped
     by the existing matcher. Tests: recovery MRU+active round trip b,a,c; pathless label MRU; old copy
     without fields; extract merge order/dedup/pathless/nil, extractMru recency+subset; single/multi/
     mixed new co-tenant, dormant target, warm target capture assertions. Residuals: recovery MRU limited
     to unsaved docs in the copy; running-editor active preserved; extraction only named tabs;
     materialized target order reflects transfer activation; dormant-target Active now MRU-first
     (behavior change).
200. **OSC7 Windows paths + start-platform validation (231; R13-15).** parser accepts absolute
     POSIX/UNC/drive+separator only; drive-relative C:dir/bare C: rejected; file://host path
     host-dropped/percent-decoded/leading /C:/ becomes C:/; shell OS flavour not gvim; cleaned
     <=4096B/UTF-8/no-control. Start: local platform check mismatch => status + default local dir
     no translation; remote POSIX requires POSIX-absolute, Windows refused before authority stat.
     Residuals no normalization/translation, Fresh accepts drive-relative, future Windows remote
     needs flavour signal, invalid escapes verbatim/invalid UTF-8 rejected, emulator-dropped/>4096
     ignored, remote fallback authority default/status-only, local no prevalidate, point-in-time
     stat, later remote cd not live-replayed.
201. **Bounded remote large-file edit context (232; R13-15/R13-7).** StringContext/NonCodeContext
     read only loaded, contiguous, byte-bounded (64 KiB) remote-window lines; never unloaded 1MiB
     window; outside loaded answers not-in-string/no mask without I/O; window-arrival generation in
     cache key so paint-loaded window re-lexes (no stale blank-line spans); context bounded 300-line
     reach/64KiB/4096B line. Local large files keep old no-editing-context behavior.
202. **Custom detector marker/require watch (233; R13-18).** user-configured marker/require changes
     invalidate/re-warm via authority watch; arms root + existing parent dirs; bounded
     MaxDetectorWatchPaths/MaxDetectorWatchDirs, excess Settings notice + Env: Reload; parent content
     seen, appearing parent re-arms next root event; no-watch backend stops/Settings/Env Reload;
     other failures bounded backoff; polling same limits; cancel/no local fallback.
203. **Remote trash capability research (235; R13-21; read-only).** NOT implementable on current
     authority contract; keep permanent remote delete + mode/mtime. Fresh trash not safety proof (home
     trash/create_dir_all/replacing rename/cross-device fallback/no no-replace/private/device checks).
     Gaps: Rename replacing by contract; Mkdir no private mode; FileStat/DirEntry no device/owner;
     SSH mv -n non-POSIX/non-atomic cross-FS source loss; no renameat2. NO trash/undo landed;
     permanent-delete residual + future design OPEN/gated.
204. **Remote search preview syntax + regressions (237; R13-23).** Highlighting already implemented
     in 77c3ec7: remoteSearch.Preview highlights bounded head via shared previewHighlighterCache;
     open-buffer text from req.Text; panel paints PreviewSpans/SyntaxStyles; 256 KiB/4000-line caps,
     8-entry cache, CPU-only command/no extra remote I/O. R34 adds truncated-head/disconnected/
     no-additional-remote-read constraint tests; no duplicate implementation.
205. **Local large-file bounded edit context (238; R13-15/R13-7).** Quote auto-close/indent non-code
     mask now work for local large files via buffer page cache: at most 300-line reach/64 KiB budget,
     never whole file; >4096B line has no context and bounds neighbour window instead of
     materializing; remote windowed uses same loaded/byte-bounded rule/generation key; read-only/no
     editable/read-only change; loaded/long-line tests; no unbounded UI read.
206. **Detector config reload watcher re-plan (239; R13-18).** env.detectors Reload swaps detectors
     (invalidate/re-warm background off UI), re-plans watcher from new list, stops old watches, bounded
     same caps/authority-only/no local; no-watch backend degrades Settings/Env Reload; already-degraded
     provider updated but no watches restarted; local startup detector set unchanged; cancel/no-local/
     caps preserved.
207. **Terminal exit auto-opens transcript (240; R13-15).** At exit dock opens exited shell read-only
     transcript (Fresh read-only scrollback), marker last visible without save/Ctrl+Space; marker
     selectable/copy; save ordinary bounded row; restart captures before new shell/restart-resume below;
     hidden dock opens on next layout; title Scrollback (exited, exit N/signal)/(exited); restored
     suffix; Ctrl+Space frozen screen toggle; marker once; alt-screen no pager but persisted;
     close_on_exit hides dock; old records compatible.
208. **i18n extraction round-34 (241; R13-5).** Recent env/terminal notices catalogued English
     byte-identical; rules/budget/docs/fallback tests. en 1054/de 579, 568 KiB text, ≈658 KiB JSON,
     heap 120-82-203.
209. **Local env detector hot reload — detection-only (243; R13-18).** local env.Provider config
     Reload re-scans/updates detector results passively; does NOT invalidate/recapture active
     recipe/capture cache, no re-activate/re-warm; non-blocking/local-only. Corrected per trust report.
210. **Remote search walked candidate filter (244; R13-11/R13-23).** walked/nested content uses Fresh
     walk scope: hidden segments + IGNORED_DIRS (.git/node_modules/target/__pycache__/.hg/.svn/
     .DS_Store) refused; applied non-git/incomplete fallback and nested repo/submodule widened
     fallback; hidden/skip nested prefixes not registered. Complete git listing keeps explicit
     membership exception (tracked/untracked candidates incl tracked under node_modules searchable).
     Unchanged bounds/cancel/authority-only/non-fatal notices/finder snapshot/no local.
211. **Embedded terminal focus events DECSET 1004 (245; R13-15; gvim extension).** child enables ESC[?
     1004h; session tracks mode and sends canonical CSI I/O via async input inbox only after opt-in;
     real transitions dock/tab focus gained, editor takes back, tab switch, dock hidden; pager no blur;
     repeated ignored; enable-while-focused initial focus-in after chunk; disable stops reports but
     tracks; new/restarted shell learns current focus before request; dead/restored no session;
     mouse/keyboard/clipboard unaffected. Residual: dock keyboard focus only, not OS window focus;
     focus mode not persisted (revived starts off until asks).
212. **Plugin copy canonicalization/capability safety (246; R13-19/R13-21).** both forms canonicalize
     src/dst before first write; dst == src or inside copied tree (direct/symlink/case-alias parent)
     invalid; unresolvable/dangling dst symlink invalid. Local real paths + os.SameFile (hardlink/
     symlink/case cannot bypass); no bounded-walk substitute. Authority requires RealPather
     canonicalize src/dst (missing dst via deepest existing ancestor); no RealPather unsupported;
     directory copy no DirectoryCreator unsupported (no check-then-mkdir fallback). Root claim
     atomic root-only. Residuals concurrent symlink swap TOCTOU, remote hardlink identity, parents
     before claim, failure-after-claim partial destination, single-file replace, stream/no-stream
     16MiB/shrink/growth/cancel, metadata/partial tree.
213. **i18n extraction round-35 (247; R13-5).** terminal/env/workspace new notices English
     byte-identical; rules/budget/docs/fallback/formatting tests. en 1107/de 579, 572 KiB text,
     ≈662 KiB JSON, heap 119-88-207.
214. **Host window + dock focus merged for DECSET 1004 (249; R13-15).** View.ReportFocus all
     workspaces; FocusMsg/BlurMsg real outer focus merged with dock keyboard focus; child focus-in only
     both focused, out when either loses; tab switch/hide dock/blur; pager no blur; repeated/redundant
     ignored; new tab inherits; FocusMsg still remote reconnect-on-activate, Blur never reconnect; reports
     only after 1004 opt-in. Residuals outer-terminal focus support/tmux config, assumed focused initial,
     focus mode not persisted, restored no child.
215. **Local detector content-change invalidation — detection-only (250; R13-18).** bounded local
     fsnotify marker/require content change invalidates detection; bounded/cancel/non-blocking/clear
     degradation; detection-only — no recipe re-activation, no capture-cache recapture; no self-trigger
     loop/resource leak.
216. **Remote nested-repository git candidates (251; R13-11/R13-23).** up to 8 nested repos resolved
     one layer with bounded git -C <prefix> ls-files through authority (same context/cancel/remote cwd/
     no local); candidates join explicit batches, nested .gitignore applies (tracked-ignored/dotfiles in,
     untracked ignored out; no notice on success). All-or-nothing: >8/failed/truncated/non-UTF8/escaping/
     deeper nested => all unresolved + widened walk + notice, never partial-as-complete. Residual deeper
     nested .gitignore not applied in fallback.
217. **Plugin copy root-source bypass + no-streamer hard-link safety (252; R13-19/R13-21).** source /
     explicitly rejected (src+"/" -> // bypass fixed); no-streamer fallback only for NEW destination;
     existing/uncertain destination unsupported (cannot prove not hard-linked to source); streamer existing
     target atomically replaced safe. Residuals no-streamer new-path concurrent-creation race (no no-
     overwrite guarantee), canonicalization TOCTOU symlink swap, remote hardlink identity not SameFile,
     parents/failure-after-claim partial, 16MiB/shrink/grow/cancel/metadata/partial tree.
218. **Palette Tab accepts selected suggestion (253; R3/R13-3).** Up/Down/PageUp/PageDown/Enter/Esc; Tab
     fills selected suggestion into query (file ranked path; command/buffer/line name/target preserve
     >/#/: prefix), palette stays open/rows re-query, Enter runs; message/disabled/no selection unchanged;
     Live Grep floating overlay Tab no-op as Fresh; keyboard/state tests/docs.
219. **Palette Tab keeps file jump target (255; R3/R13-3; gvim extension).** file mode fills ranked
     path + query :line[:col]; Windows drive/other colon intact/target from end; filename ending
     :number verbatim; leading palette mode char escaped ./* to stay file mode; command/buffer/line
     modes as before; palette open/re-query/Enter; message/disabled/no selection unchanged; Live Grep
     overlay no-op.
220. **Remote plugin copy requires atomic WriteStream (256; R13-19/R13-21).** every file copied via
     authority WriteStream; no streamer → unsupported BEFORE read/create/write; no create-then-
     WriteFile fallback (FileCreator gives no exclusive data handle); directory copy also needs
     WriteStream+DirectoryCreator before claim/walk; streamer existing target atomically replaced;
     capability refusal leaves nothing created/written. Residuals canonicalization TOCTOU symlink swap,
     remote hardlink identity not os.SameFile, parents/failure-after-claim partial tree, metadata/
     growth/cancel limits.
221. **Nested-prefix fallback roots + cap notice (257; R13-11/R13-23).** unresolved nested prefixes
     searched in place (not whole tree); wider whole-tree only when candidate list too large for
     bounded explicit batches; both prune hidden/IGNORED_DIRS + non-fatal notice; output cap marks
     batches Truncated + says incomplete because candidate files beyond cap not reported; never
     complete-looking/partial-as-complete; deeper nested .gitignore residual.
222. **Alternate-screen exit read-only frame + marker (R13-15).** running alt-screen no history refusal;
     after exit frozen alt frame becomes read-only transcript (visible rows+marker, Scrollback exited
     title), auto-open at exit, Ctrl+Space toggle frozen frame; save one marker after rows/restart
     below/restore same record no duplicate; no output overwrite.
223. **Local watcher scope notice + i18n (R13-18/R13-5).** local env-watch notices catalogued
     (unavailable/ended/root-failed/root-unwatchable/dirs/limit); omitted/omittedDirs drives limit
     notice; new dock tab-limit/numbered/hover + extract title keys; en1117/de579, 573 KiB text,
     ≈663 KiB JSON, heap126-82-208.
224. **File multi-word + buffer-mode fuzzy multi-word (keymap; R13-3).** file mode whitespace terms
     all match path any order (spaces-in-path still match); path:line[:col] stripped first. Buffer mode
     (#) same Fresh FuzzyMatcher on tab labels: subsequence match, rows score/Tab-order tie; terms
     prepared once/reusable/ASCII fast reject; finder 50k/100 results/cancel/sequence guard.
225. **Remote AllFiles open-buffer snapshots (lsp; R13-11/R13-23).** panel glob snapshot bounds
     AllFiles; open buffers searched from editor snapshots (current first, others after tool results),
     disk line skipped; unsaved edits found as Fresh project_files; open outside root/snapshot excluded
     left to Open Buffers; authority-only/bounded/cancel/no local/degradation notice.
226. **Local watcher real fsnotify + arm-before-refresh (trust; R13-18).** event handling arms nested
     watch BEFORE re-detection, closing nested-dir refresh→Add gap; real-fsnotify integration tests
     (marker lifecycle, nested parent appear, unwatchable root notice) no fake/fixed sleeps;
     detection-only/no recipe or capture recapture/non-blocking/no loop/leak/local wording.
227. **Terminal transcript re-search on resize/re-wrap (sidebar; R13-15).** committed query re-run
     asynchronously on Sync when transcript rows re-wrap; query/regex/case survive, matches same text,
     n/N/title aligned, copy via editor clipboard path; Update/Draw never search; Fresh read-only
     scrollback normal-buffer equivalent.
228. **Plugin no-follow local-only; SSH copy unsupported (plugin streamer security; R13-19/R13-21).**
     local os.CreateTemp+os.Rename final entry replacement (symlink replaced not followed, existing
     target atomic replacement, hardlink safe); SSH deliberately no capability (POSIX mv follows
     dir symlink, no portable atomic rename; rm-before-mv rejected); fs.copy requires no-follow and on
     SSH unsupported BEFORE read/create/write; local requires RealPather/DirectoryCreator/inside-source
     checks. Residuals local parent-path TOCTOU, canonicalization pre-write proof/symlink swap
     TOCTOU, no general no-overwrite guarantee; tree failure-after-claim partial, metadata/growth/cancel.
229. **Literal-path quick-open probe (keymap; R3/R13-3).** while file index enumerating, file mode
     exact-path probe for literal query path (Fresh prefix-probe; etc/hosts before enumeration); only
     one exact regular file inside workspace; local realpath candidate/root contained; remote authority
     realpath+Stat/no local disk/refuse if containment unprovable; tea.Cmd only/context cancel/cache
     poll ticks/query seq+text match; merge before rank preserves fuzzy/prefix/:line[:col]/cap;
     missing/dir/unreadable/../symlink escape/completed index no row/no fallback. Residuals
     dir/basename-prefix listing not probed, truncated completed index not probed, local realpath-check
     vs Stat TOCTOU theoretical.
230. **Remote search tool-failure open-buffer fallback (lsp; R13-11/R13-23).** tool probe missing or
     run fail/timeout: open-buffer snapshots streamed first, terminal error batch names original failure
     and says only open buffers from editor snapshots were searched, remote project files not searched;
     never complete/no local disk; cancel closes without Done batch; panel limit/caps same.
231. **Local watcher closeResources (trust; R13-18).** sync.Once closeResources across startup failure/
     Close/session-cancel goroutine; watcher released exactly once, Close idempotent, no leak;
     arm-before-refresh/real fsnotify tests/detection-only/local wording preserved.
232. **Incremental terminal scrollback search (sidebar; R13-15).** '/'/Ctrl+F input; every typed change
     async search + jump first match (preview without commit); empty cancels; regex/case re-run; request
     sequence drops late/stale answers; Enter commits/title k/n; Esc restores view/keeps previous
     search; bad regex notice; resize re-wrapped Sync re-search; Update/Draw never search; copy editor
     clipboard; Fresh read-only scrollback equivalent.
233. **SSH plugin copy link-commit research only (plugin streamer; R13-19/R13-21).** NOT implemented;
     SSH no NoFollowWriteStreamer/fs.copy unsupported before read/create/write; future link-commit
     rejected due POSIX mv dir-symlink follow, check→rm→mv TOCTOU, failure-after-rm destination loss,
     parent path races; local no-follow only capability with local parent TOCTOU; no SSH strong/atomic
     no-overwrite claim.
234. **Bounded local probe; remote exact-Stat fallback (keymap; R3/R13-3).** local directory
     files/parent-basename-prefix probe bounded 4096 inspected/50 returned/batches/cancel; hidden rules/
     realpath containment; remote exact-path only via authority realpath+Stat/no local disk/no dir
     enumeration (authority ReadDir shell glob materializes whole dir before client caps; no claim remote
     reads bounded). tea.Cmd/context/poll cache/query seq+text; merge before rank preserves fuzzy/
     prefix/:line[:col]/cap. Residuals remote branches not probed/local 4096 miss/no gitignore/
     truncated index not probed/realpath→Stat TOCTOU/hidden dir query.
235. **Remote external-snapshot notice (lsp; R13-11/R13-23).** remote search bounded by panel quick-
     open snapshot + match outside snapshot: Done batch non-fatal notice result limited to snapshot/
     path may be new or glob-excluded/File Explorer: Refresh; not Truncated (snapshot bound), never
     complete-looking; finder does not self-refresh on search.
236. **Sidebar terminal F3/Shift+F3 preview stepping (sidebar; R13-15).** search input open: F3/
     Shift+F3 step preview forward/back through current query matches (wrap) without closing/committing;
     first press runs edited/unrun query; answer pending ordered F3 first/Shift+F3 last; bad regex notice;
     Enter commit/Esc restore; Update/Draw never search.
237. **Env:Reload watcher retry supervisor (trust; R13-18).** first local watch start failure keeps
     Local Env: Reload notice; later Env: Reload asks app to start fresh bounded watcher in background
     (no fsnotify UI call), predecessor resources released exactly once, successful retry re-runs passive
     detection/clears notice; live watcher untouched; detection-only/local wording; gvim improvement over
     Fresh no-retry.
238. **SSH new-target link capability (plugin copy; R13-19/R13-21; audited).** local no-follow replace;
     SSH new-target only: private mktemp + POSIX link exact target one no-clobber hard link, never ln/
     mv/rm target; probe exact-target hardlink semantics (second link/symlink/dir target EEXIST, no
     follow) else unsupported before source read; existing/concurrent targets conflict untouched; tree
     needs DirectoryCreator+stream and proves parent before root claim. Residuals temp cleanup best-
     effort (commit error/cancel/rm failure/probe kill leave temps; target complete never deleted);
     parent-path TOCTOU not closed (no dirfd/openat recheck; new target can land in unexpected dir, never
     over existing); per-dir probe cache stale; capability proof not strong atomicity; tree same-FS
     assumption/partial after claim; real SSH UNTESTED/local helper tests only; no durability/fsync/
     general filesystem-race guarantee; temp-by-path same-account replacement outside protection.
239. **No server-side bounded directory listing (lsp; candidate withdrawn).** find+head output
     byte-bounded but no provable traversal/entry bound; kind-filtered find may walk without emit,
     per-entry -exec spawns unbounded children, -printf nonportable. No LimitedDirReader/BoundedDirLister
     advertised; SSH ReadDir keeps 5000-entry/4MiB error; remote finder fallback unchanged; remote
     probe exact Stat only; no claim bounded remote read; real SSH UNTESTED.
240. **Dashboard callback isolation (plugin; R13-6/R13-24).** register_section/remove/sections; callbacks
     retained in provider state; only bundled dashboard (name+SourceBundled) gets key; others key="" and
     lookup nil/direct call rejected; plain args/results + validateDashboardResult nodes4096/depth8/string
     4KiB; caps 16/64/name64/title120/ttl/timeout/priority; dashboard timeout/TTL/row caps/error isolated;
     no command/URL/network; rows text-only; limits 8 sections/64 rows/sequential refresh/no layout/
     click/URL; third-party/remote providers UNTESTED.
241. **Sidebar local foreground-process title (sidebar; R13-15).** local Unix only: ~1s tea.Cmd poll
     tcgetpgrp + Linux /proc/<pgid>/comm or 500ms bounded ps -o comm= -p <pgid> (no shell); sanitize
     controls/UTF8/64 runes; per-tab cache/gen drop; remote/Windows/restored/exited fallback launch/OSC;
     background tabs poll. AUDITED RESIDUAL: ps resolved through PATH (no absolute/allowlist); poisoned/
     project-activated PATH could execute a different ps once/s; real macOS/BSD/Linux polling UNTESTED.
242. **Completed-index exact probe + 16-path cache (keymap; R3/R13-3).** probe both loading and completed
     index; local exact/dir/parent-prefix bounded 4096/50/async; remote exact Stat only/no enumeration;
     tea.Cmd/cancel/query seq+text; cache incl negative 16 paths per palette lifetime keyed path text
     (:line[:col]/mode escape same path no repeat Stat; changed path reprobe); merge before rank preserves
     fuzzy/prefix/target/cap. Residuals per palettes/lifetime/local 4096/no gitignore/remote branches/
     TOCTOU/truncated list not complete.
243. **Statusbar i18n (statusbar; R13-5).** recent status/degradation strings English byte-identical;
     formatting/fallback/key-guard tests; en1124/de579, 575 KiB text, ≈665 KiB JSON, heap122-82-205.
244. **markdown_source opt-in parity port (port; R13-6).** Fresh's plugins/markdown_source.ts
     shipped as bundled Lua port (default_enabled:false, off; gvim native editor/markdown.go rule set
     remains default/unchanged). Hidden normal-context commands bind enter/tab/shift-tab and self-check
     before acting: Enter parses leading blanks + checkbox/ordered/unordered and continues same indent/
     bullet, ordered number+1, unchecked checkbox, empty item drops marker, non-list copies leading
     blanks, cursor placed; Tab on blank unordered/checkbox indents 4 spaces + cycles bullet forward
     *→-→+→* (checkbox state kept), ordered/content inserts 4 spaces; Shift+Tab blank unordered/
     checkbox with indent>=4 removes 4 bytes + reverse cycle *→+→-→*, else native dedent_selection.
     Non-Markdown/selection/extra carets/active plugin input mode fall back to native insert_newline/
     insert_tab/dedent_selection. Four-space unit hardcoded like Fresh (no tab_size/use_tabs); failures
     status. Unported: no markdown-source buffer-local mode/auto-activation/status (plugin API lacks
     source/compose view query; buffer mode would add status + Esc hint), no compose-view exclusion;
     handlers are normal-context and self-check instead. Tests: manifest/hidden commands/exact bindings,
     Enter/Tab/ShiftTab tables+cursor, fallbacks, read-only failure. Residual: native duplicate opt-in,
     direct Lua edits on markdown path with native fallback; real host UNTESTED.
245. **Sidebar ps absolute-path hardening (sidebar; R13-15).** local foreground fallback no longer
     PATH-resolves ps: resolvePSPath fixed {/bin/usr/bin/sbin/usr/sbin}/ps, regular executable; psComm
     absolute path/no shell/bounded ctx+output/kernel integer pgid; missing/non-executable fallback;
     Linux /proc preferred. Residuals fixed system locations trust assumption/unusual prefixes/NixOS
     missing ps/no hash allowlist/real macOS-BSD-Linux polling UNTESTED.
246. **window_focused/window_blurred plugin events (keymap; R13-24/R13-15).** real host terminal
     window transition emits empty-payload events via plugin pump after dock SET1004 update; repeated
     FocusMsg/BlurMsg no event (model starts host-focused); no inline plugin callback; no subscriber no
     queue; FocusMsg still remote reconnect-on-activate, BlurMsg not reconnect; focus_gained remains
     chrome/editor keyboard event; host OS focus not gvim surface; internal focus does not raise; real
     outer-terminal focus reporting dependency unchanged.
247. **Statusbar i18n (statusbar; R13-5).** recent status/degradation strings English byte-identical;
     formatting/fallback/key-guard tests; en1124/de579, 575 KiB text, ≈665 KiB JSON, heap122-82-205
     (no net key-count change this slice).
248. **rust_lsp reduced port (port; R13-6).** New bundled Lua plugin rust_lsp (default off; no keys/
     settings; 4 visible palette commands): Rust: Copy rustup Install Command (clipboard "rustup
     component add rust-analyzer" + status), Rust: Copy brew Install Command ("brew install rust-
     analyzer"), Rust: Check rust-analyzer (gvim.proc.which LookPath then gvim.proc.run rust-analyzer
     --version bounded 5 s/64 KiB; reports path+first version line, missing PATH, exec failure, non-zero
     exit stderr first line, empty version), Rust: Language Server Help (action popup with install
     commands/PATH/restart/toolchain notes/upstream docs link; copy rustup/brew, check, native lsp_restart,
     show_lsp_status, dismiss). Unported: automatic lsp_server_error status, contributed LSP-Servers
     popup rows/menu slices, recovery detection via lsp_status_clicked, Disable Rust LSP, Configure Mode
     (registerLspServer full/reduced init options+process limits, restartLspForLanguage) — require plugin-
     API LSP menu/disable/register/restart seams gvim lacks. Residuals: LookPath is local-machine best-
     effort in remote workspaces (only --version follows authority); generic lsp_help already owns Rust
     LSP-error status/install popup so rust_lsp adds explicit commands instead of a second handler; native
     vim LSP actions used for restart/status. Tests: manifest/visible commands/no bindings; copy commands
     clipboard+status; missing PATH no process; found+version argv; non-zero version exit; exec start
     failure; help popup message/actions + copy/check/native restart dispatch. Bundled 28 (9 on/19 off),
     63 registration sites; idle heap unchanged; real host UNTESTED.
249. **Standalone diff_nav opt-in port (R13-9/R13-6).** Fresh's plugins/diff_nav.ts shipped as
     bundled Lua port but opt-in (default_enabled:false): Fresh default-enabled; adding it to the
     default set measured TestBundledPluginsIdleHeap 8.12 MiB > the 8 MiB gate with 10 defaults, so
     the nine pre-existing defaults stay unchanged and git_blame is untouched; git_gutter's own
     merged Next/Previous Change remains the default navigation. Commands diff_nav.next_change/
     prev_change with Fresh ids/i18n (cmd.next_change/prev_change, status.change/change_wrapped/
     no_changes, status.jump_failed). Collects gvim.plugins.get("git_gutter").hunks, live_diff
     hunks and buf:diff_saved(); missing/old/erroring export, unloaded plugin or absent git
     contributes nothing (works saved-diff-only/live-only with git_gutter disabled); sorts by byte
     position (line fallback), dedups by line, wraps; jump set_cursor center; status Change n/total
     [wrapped]. No keys/settings; all host calls in plugin tasks/awaits, no blocking IO in Update.
     Residuals: Fresh default-enabled vs gvim opt-in (idle-heap gate, deliberate documented
     difference); both enabled produce duplicate palette command rows over the same targets; Compare
     view hunks not a cross-context source; large-file only navigates sources that export (line-order
     fallback, git_gutter keeps its own large-file notice); live_diff must be loaded for its hunks;
     real host UNTESTED. Tests: manifest/opt-in, Fresh ids/titles, merged git+live sorting/wrap/
     statuses, line dedupe, saved-diff-only, live-only/no-changes, raising provider degrade, prev from
     end.
250. **deno_lsp reduced opt-in port (R13-6).** Fresh's deno_lsp.ts shipped opt-in reduced
     (default_enabled:false; no keys/settings; 3 visible commands): Deno: Check Project (workspace
     root or cwd; deno.json/deno.jsonc via gvim.fs.exists/path.join + gvim.proc.which deno; states
     ready/config-only/runtime-only/neither, missing-runtime names install command and points at help),
     Deno: Copy Install Command (brew install deno when brew on PATH else official curl command,
     clipboard + status), Deno: Language Server Help (action popup install commands/typescript+
     javascript lsp entries command deno args lsp auto_start false root_markers/initialization_options,
     local-LookPath caveat, native Start/Restart LSP Server/Show LSP Status, upstream install link).
     Unported: automatic Deno server selection/baseline restore, active-window root decision,
     disabled-language protection, LSP-menu/disable/register seam, Fresh debug log; PATH lookup is
     local-machine best effort in remote (config/file checks workspace-facing). Tests: manifest/
     commands/no bindings, Homebrew-vs-curl selection+clipboard/status, four Check states+jsonc+custom
     root, help popup message/actions/copy/check/native restart. Real host UNTESTED.
251. **Native Deno LSP root selection (lsp; R13-8/R13-3).** Fresh deno_lsp selection in native core;
     authority Stat + bounded OneShot sh -c command -v deno 5 s/cancel under trust gate, remote no
     local disk/PATH; failure/timeout/disconnect/trust denial selects nothing; explicit lsp entry/
     disabled language/lsp_enabled:false win; only typescript/javascript (tsx untouched). Per-root
     provider exact manager; auto_start:false/manual Start; clear restores baseline; generation/reconnect/
     root switch drops old answer; provider re-reads live config; existing manager keeps configured
     server (plain restart does not switch, only new manager/rebuild/relaunch); only probed workspace
     root; Deno disappearance not re-confirmed. Fresh deno_lsp default-enabled vs gvim native always
     on, bundled helper default off and toggle controls helper commands only (explicit config opt-out);
     real remote host UNTESTED.
252. **Rust/Deno authority version probes (trust; R13-6/R13-13/R13-8).** Check runs --version through
     gvim.proc (authority-following, 5 s/64 KiB, trust gate); no local LookPath presence claim;
     failure/trust denial says could not run on workspace + local-only LookPath caveat; non-zero
     first stderr/empty version statuses; Deno Copy Install always official curl (local brew says
     nothing about remote); help lists both/native rules/opt-out. Real host UNTESTED.
253. **diff_nav deletion seam + real provider interaction (R13-6/R13-9).** saved-diff deletion targets
     new_count==0 => max(new_start-1,0) (git_gutter alignment); real git_gutter/live_diff exports and
     buf:diff_saved tested including deleted HEAD hunk; missing/unloaded/raising sources no-op;
     Fresh default-enabled difference/duplicate rows residual.
254. **Sidebar transcript query history (R13-15).** per-tab in-memory oldest-first bounded 100, skip
     empty/newest repeat; prefill+select newest, Up/Down preserve live edit, typing replaces selection,
     Enter commit/Esc restore; focus loss/hide closes input+cancels preview; not persisted/shared
     (Fresh persists with workspace: residual); no config/disk.
255. **Finder-generation probe cache (R3/R13-3).** 16-path probe cache belongs to file-index
     generation; Restart bumps gen and palette drops cache; command re-checks gen; negative cached
     within generation; external create/remove without refresh can stale until refresh/reopen; remote
     exact Stat only/no enumeration.
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
  manager/generic HTTP (restricted gvim.http.get landed r28)/project-local plugins).
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
- **R7-5 (P3, S/M; two items, both OPEN)** — inline-hint tier 2 and workspace-diagnostic
  cancellation landed (rounds 9–11), so hints render inline with the documented fallbacks and
  workspace/diagnostic pulls send `$/cancelRequest` through `protocol.Call` (pullSet; cancelled
  pulls never cached/merged, last good answer kept, 10 s timeout fallback). `process_limits` is
  enforced on Linux (cgroup v2 + ./ulimit -v` fallback) and recorded unreachable on macOS/Windows,
  which matches Fresh's own Linux-only enforcement (a platform limitation shared with Fresh, not
  a gvim gap). OPEN TUI gap: no built-in universal language server ships (Fresh's quicklsp is a
  separate user-installed binary; implementing/packaging a universal server remains work, not an
  exception). `docs/lsp.md`.
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
- **R8-3 (P2, M; open TUI gaps, documented reasons in docs/settings.md §6)** — declared plugin
  settings are listed read-only with schema/default/bounds/layer, but editing them still happens
  in the raw user config (a structured per-plugin editor needs a plugin-owned schema→widget
  mapping plus reload/rollback the settings service does not own); the Plugins page has no
  gallery/install UI (plugins come from the binary, user dir and --plugin); LSP fields are edited
  as the whole language-array JSON because PatchBytes cannot address one array element (per-field
  dialogs need an array-index patch path); no in-page key capture or second keybindings writer;
  no env activation from the page; no per-field reset-vs-inherit display or full undo; Reload is a
  palette row only; Fresh's generic widget runtime is not ported. These are OPEN TUI features
  with concrete gaps, not closures.
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

- **R9-1 (P2, S, round-10 ports add R10-1)** — round-9 port-wave reductions: dashboard (task177 upstream tracking row ahead/behind/no upstream LANDED; r26 safe gvim.url.open + clickable repo/branch/Open Repository/Open Branch; r28 opt-in GitHub PR section/r29 PR rows keyboard+click reachable, canonical URL rebuilt from validated slug+number (untrusted html_url ignored) via gvim.url.open confirmation; OPEN: weather/other GitHub/
  network sections, third-party section API/Utility Dock placement, row mouse, sessions section —
  optional user features stay open, not closed by no-network/dependency; icon/style differences
  acceptable), code_tour (no Utility Dock placement, no per-step buffer modes,
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
  per-panel views, windowEmbed live panes, markdown-in-text, package manager/generic HTTP (restricted gvim.http.get landed r28)/project-local
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
  Reductions were R12-1 (foreground, no daemon auto-start on launch/refusal policy, single-client,
  single workspace, Windows pipes later); r17–r19 landed jump box/remote session restore and
  multi-client/auto_attach, so the remaining reductions are no fork/daemonize, Windows pipes,
  eviction/workspace CLI, auto_attach default/named-daemon nuances. Tests: daemon server suite, CLI flags, app daemon
  start/attach/detach/reattach/quit/session-save and single-writer refusal.
- **R11-2 (P1, L/XL; catalogues/extraction 2/3 landed; remainder R13-5)** — the i18n foundation,
  catalogues/import/validation, extraction 2 and extraction 3 are in: palette titles and
  descriptions, menu labels, every scalar settings row/section label (incl. status-bar
  section/fields), hand-written/re-named command keys and the remote status pills; en 706 keys,
  all-locale key+value ~517 KiB under the 640 KiB cap, with an initial embedded+heap budget check.
  OPEN remainder: most inline notices, prompt/dialog titles+buttons/text, structured sub-page
  labels, settings values, keybinding help, plugin data/page-picker inline text, and translated
  coverage beyond the mapped subset (R13-5). `docs/i18n.md`.
- **R11-3 (P2, M; open terminal remainder)** — rounds 12–13 landed the tabs container, per-workspace
  tab scoping, launch records/agent metadata, discovery Enters, remote SSH terminal,
  ConPTY/OSC52-write/transcript-reflow; phase 2 landed tab drag/reorder and trust-gated OSC 52
  read/paste. OPEN TUI gaps: kitty keyboard (x/vt v0.1.0 legacy-only), live-screen reflow (x/vt
  has none; stored rows re-wrap), live ephemeral PTY restore (restored tabs stay read-only/
  dead), drag-to-split/per-tab splits (dock has no splits), report-split close_on_exit, and agent
  re-exec edges. `docs/terminal.md`.
- **R11-4 (P2, S/M; open theme gaps)** — rounds 10–11 landed the accent audit, Settings four-chip
  previews, the colour picker/mouse editor and first-surface painted-key provenance. OPEN TUI
  gaps: background images are not implemented, drag-and-drop of theme values is not implemented,
  user-theme inheritance beyond one level of extends-to-built-in is not implemented, URL/packaged
  themes are recognised-and-refused (no network/packaging path), and untagged compose/page-view/
  plugin painters still fall back to "matched by colour". `docs/themes.md`.
Round 13 recorded five open gaps:

- **R13-1 (P1, L; #19b remainder open; r25 shared Save As reservation landed)** — registry v2/
  co-tenants, per-workspace state, jump box, shared runtime, remote session restore, extract variants,
  multi-client/auto-attach, full scope/config/waits, shared buffers/eviction, OpenLoaded adoption/
  conflict label, conflict Save As guard/new-path/owner rekey. R25 Save As reservation: ReserveSaveAs
  in PlanSaveAs on UI before async write; blocks Open/Register/PlanSaveAs for other workspaces and
  second save same editor; released by ApplySuccess/failure/cancel, explicit Release, DropEditor;
  closes plan-to-write window and extends Fresh save_target_refusal across off-UI write. OPEN:
  in-process-only reservation; dropped SavePlan without Release/DropEditor leaks claim until editor/
  daemon exits; OpenLoaded/plugin tabs for reserved path load but cannot Register owner; disk exists/
  force semantics unchanged (RefuseExisting at write); non-existent path not protected against non-
  gvim writers; dormant/evicted owner path unknown until rematerialize; plugin/API-created tab still
  independent; daemon open-file owner switch; editor-level Close no promote; no TUI splits; view
  position only ownership; large/virtual/remote independent; mirror plugin doc-id; remote eviction
  excluded; workspace CLI; hard-kill debounce.
- **R13-2 (P2, M; open discovery completion)** — dock/agent rows outside the picker,
  blocked/done/working heuristics (output/OSC data-driven, display-only), remaining agent_*
  family plugins (phase 2 landed the native external Claude/Codex transcript-history scanner and
  merged it with tmux findings), other multiplexer/remote scanning, per-session workspace
  identity/initial attach launch in the registry (same-root sessions still share a workspace with
  extra attach tabs), git-worktree creation in Run Agent, tmux level 1/2. `docs/terminal.md`.
- **R13-3 (P2, L; remote remainder after round 20)** — landed: watch, finder, search/replace, git,
  explorer read/write/ignore/watch/clipboard, env, SSH keepalive/auth/config/ProxyJump/agent
  forwarding + r20 known_hosts policy/host-key algorithms/keyring interface, jump box, remote session
  restore, remote hot-exit staged/recovered, trust record keyed target, reconnect refresh; remote
  large-file overlay edit + r20 STREAMED atomic save (arbitrary size bounded memory). OPEN: Fresh
  server-side patched recipe, non-windowed whole-file cap, search preview head-only >256 KiB/no semantic-LSP spans (bounded head syntax-highlighted r26/r28)/ignore
  filtering, explorer drag/move/undo/whole-subtree transaction/mode-mtime-hardlinks-xattrs/
  existence race/overwrite prompt/watch latency, remote marker scan, hot-exit local-staging
  privacy divergence, ~user/special/sudo, reconnect/terminal respawn, keyring backends no-op/2FA
  not stored, saved-session conflict detection, restored terminal dead/ownership, real SSH UNTESTED.
- **R13-4 (P2, S/M)** — spawn-gate residuals: only the shell `-c` first token is inspected
  (compound/quoted/variable-built commands not parsed), Windows PATHEXT and drive-letter-case
  normalisation missing, an environment injected below the authority/plugin-OS/LSP seams would not
  be resolved, no kill of a running process when the level drops, and the remote path keeps the
  coarse app rule pending a remote trust store. `docs/trust.md` §3.1.
- **R13-5 (P2, M; i18n remainder 6 landed; residual open)** — round-20 extraction plus r27
  remainder 6: update_command missing/running/no-output/dock-failed, gvim.url.open confirmation/
  unsafe/cancelled/error/opened, terminal restart/resume + signal, palette preview degradation,
  asm_lsp config-refused (gvim-only fallback), English byte-identical; docs/i18n + budgets refreshed;
  de fallback/key-guard tests. Metrics: en 1015, every non-English 579; key+value 564 KiB (under
  640 KiB cap), embedded JSON ≈654 KiB, heap en110/de82/both193; i18n_check green. OPEN: rows
  without Fresh keys, structured schema names, some keybinding help/plugin-manager edges;
  restore_live config-only; self-update absent.
- **R13-6 (P2, L/XL; open TUI backlog after round 19)** — GPM console mouse, kitty keyboard, legacy
  key-release, live-screen reflow, per-tab splits/report-split, universal language server, Windows
  ConPTY real-host, update self-update/terminal buffer (R13-12), completion residual (R13-7), remote
  residuals (R13-3/R13-8/R13-11/R13-13/R13-23), shared plugin runtime remote/id-scope (R13-24),
  extract/shared buffers (R13-14/R13-1), live-restore full backing file/remote state (R13-15), input
  edges (R13-16), theme background/drag, plugin gallery/env activation/in-page capture, composite API,
  orchestrator/live_sessions/agent_*/devcontainer/k8s/pkg/audit_mode/welcome_screen/remaining language
  helpers (r28 ported clangd_support reduced helper, opt-in; no server-specific switchSourceHeader/fileStatus notifications/floating panel/ancestor .clangd walk; r28 restricted gvim.http.get/plugin GitHub PR; r29 IPv6-transition SSRF refusal; r25 ported asm_lsp; r27/task197 exclusive create; r27 csharp_support; r28 clangd_support; r31 welcome demo (five commands/native dispatch/primary-caret inserts/language-aware comment/local time/English-only):
  nearest-project restore once/session + csharp-ls install help; no setLspRootUri/Roslyn needs-restore
  event/Disable action/--version precheck, English-only; no plugin-API Disable action/per-language
  restart/error metadata/remote-home global check/overlong never-key), auto_attach default/named-
  daemon nuances, Windows ConPTY.
- **R13-7 (P1, M; MRU/multi-buffer landed; completion residual)** — completion landed: buffer
  words/local-first; snippets/UTF-16/resolve/version+overlap; auto-popup/multi-buffer; trigger chars;
  live word re-filter; r21 Backspace full-pool/identity; r22 non-word verified Fresh not gap;
  labelDetails detail/description rendered and Fresh never shows completion documentation (not
  gaps); r23 multi-buffer dabbrev; r24 MRU order for popup/dabbrev (RecentTabIndices active-first
  document identity, close/move keeps order, fallback tab order); r26 multi-cursor plain accept (each
  caret own prefix, one atomic edit/undo, version guard, overlap refuses whole; snippet/import multi-
  caret refused with notice); r27 multi-cursor snippet/auto-import accept (per-caret expanded snippet +`,
- **R13-8 (P2, M; finder/git/preview/refresh landed; residual data-surface gaps open)** — remote
  file finder: idle index then authority-only enumeration (no local fallback; local preview/git/
  project ports cleared), palette slash paths, ~/… safe real-shell expansion, bounds/truncation.
  Round 16: remote git letters via porcelain-v1 -z one-shot (8 s/8 MiB, 2 s cache, reconnect
  invalidate, A/M/D/U/R/C approximations, local git never consulted); palette head preview via
  authority ReadRange (64 KiB+1/honest truncation/whole-file fallback/disconnected error); finder
  index in-place Restart with generation guard, reconnect restart and File Explorer: Refresh (full
  re-walk). OPEN: palette rows no git letters (local rows do not either), project-search preview
  column unwired, full re-walk, open palette old rows until re-rank/reopen, preview cached before
  disconnect may linger, no full fake-slot buildRuntime reconnect test, finder walk still per-
  directory round trips (not Fresh's server-side walk). Remote search/replace is R13-11.
- **R13-9 (P2, M; PARTIAL — merged navigation landed, residual gaps open)** —
  `git_gutter`'s Next/Previous Change now merges Fresh diff_nav's three sources: its own
  HEAD changed regions, `live_diff`'s exported hunks (when that plugin is loaded), and the
  buffer's saved-diff ranges (`b:diff_saved()`). Targets are deduplicated and sorted,
  navigation wraps, and the notices use Fresh's `Change n/total`/`[wrapped]` text. Tests:
  `TestGitGutterNavigationMergesLiveDiffAndSavedChanges`,
  `TestGitGutterNavigationDedupesMergedSources`,
  `TestGitGutterNavigationSurvivesABrokenLiveDiffExport`. This covers Fresh diff_nav's
  merged-navigation user behaviour while `git_gutter` is enabled (it is default-enabled).
  OPEN residuals: (a) **large-file mode is refused outright** (`Git changes are not tracked in
  large-file mode`), whereas Fresh's diff_nav has no command-level large-file gate and navigates
  whatever its view-state/saved-diff sources expose; (b) **cross-Compare is not covered** —
  the commands read the active editor buffer's sources only, and Compare-view hunks
  (`gvim.diff.compare_buffers`) are not a navigation target source, so navigating a Compare view
  does not use its hunks; (c) **commands/i18n gap** — Fresh ships a standalone `diff_nav` plugin
  with translated command/status keys (`diff_nav.i18n.json`: `cmd.next_change` (+desc),
  `cmd.prev_change` (+desc), `status.change`/`status.change_wrapped`/`status.no_changes` in 15
  locales), while gvim's Navigation lives inside `git_gutter` with hand-written English titles
  and notices and disappears when `git_gutter` is disabled; (d) targets are line-based (Fresh
  sorts/dedups by byte positions), so deleted-only/duplicate-line edge cases across merged
  sources are untested. `plugin/bundled/plugins/git_gutter/init.lua`,
  `plugin/bundled/git_gutter_test.go`, Fresh `plugins/diff_nav.ts`/`plugins/diff_nav.i18n.json`;
  §4 item 97.
- **R13-10 (CLOSED round 14)** — direct file/`--simple`/`--diff`/stdin launches no longer bypass the
  daemon single-writer rule: per-root/per-name daemon host marks make every direct mode refuse
  while a daemon hosts the workspace (named daemons too), with attach instructions; daemon start
  fails if its mark cannot be written; a refused launch leaves the daemon's unsaved work/disk file
  untouched. Deliberate divergence from Fresh (which shares per-file buffers); revisit when gvim
  has that ownership. Tests: daemon host marks, named refusal, no-lost-work, mark-failure.
- **R13-11 (P2, L; remote search tools/candidates)** — search/Git Grep via authority (trust gate,
  rg→grep, remote cwd, no local fallback, stream/caps/cancel, query validation); Replace atomic/
  stale/budget/cancel (no rollback/undo); current-file/open-buffers + preview (r18); r23 grep
  project-file scope; r24 both tools share candidate scope (rg --no-ignore --hidden -g !.git +
  candidate filter, so tracked-ignored searchable on both), complete bounded git ls-files
  authoritative, outside worktree rg defaults/grep hidden-only. OPEN: failed/truncated listing
  degrades hidden-only without notice (rg keeps defaults; tracked-ignored can be omitted); tool cap
  + panel Limit before filter → <Limit and widened rg can consume cap; nested repo inner ignore;
  finder snapshot authoritative; preview head-only >256 KiB/no semantic-LSP spans (bounded head syntax-highlighted); replace no rollback/undo.
- **R13-12 (P2, S/M; update check + r27 update-command terminal landed; self-update engine open)** —
  config check_for_updates default true (user layer; project refused), gvim update_check_url +
  GVIM_RELEASES_URL, https/loopback policy, 8 s/64 KiB, daily stamp, no-version skip, silent failure,
  tea-command check; {update} paints/clickable details; palette 240 Check for Updates/Show Update
  Output. r27 user-layer update_command (gvim extension; project refused; max 4096/no NUL): when set,
  palette Update fresh/Show update output appear and run the command in a retained dock tab “Update”
  via $SHELL -c (or /bin/sh; cmd.exe /C Windows), output kept/restart keeps prior transcript; start/
  dock errors status; non-zero exit visible; independent of check_for_updates; no Settings row; local
  machine; close_on_exit drops the exited tab. OPEN: no download/install/self-update engine, Fresh's
  actual updater, release-page click starts no install; telemetry/GUI/browser/WASM/packaging boundaries.
- **R13-13 (P2, M; remote explorer read-only+git+preview+refresh+writes+ignore/watch+clipboard;**
  **residual open)** — lazy authority-ReadDir tree, remote git letters, palette preview, finder
  refresh, New File/New Directory/Rename/Delete via Files/Mutations, r18 remote .gitignore + polled
  directory watch (gvim extension), r19 cut/copy/paste/duplicate over authority (bounds 10k/256
  MiB/64 levels/2 min; atomic per-file, binary-safe; cut copy-then-remove; paste refuses existing
  name; duplicate Fresh free-name; symlink/root/outside/inside-source refusals; target-naming
  confirmations; trust gate/disconnected/refresh; partial destination kept/reported). OPEN: no
  drag/move, no undo/history, no whole-subtree transaction/cleanup UI, FileEntry/Mode/mtimes not
  restored, hardlinks/xattrs not preserved, copy/paste/rename destination-exists refusal only at check time (concurrent create can be overwritten), no overwrite prompt,
  watch latency/rename approximation, palette rows no git letters (local too), Windows paths, real
  host.
- **R13-14 (P2, M; co-tenant/session restore/extract variants + r27 mixed extraction landed)** —
  registry v2/same-root extract, terminal saved-record migration (no PTY hand-off), multi-select file
  tabs to one new co-tenant, r27 mixed file+terminal selection picker/target chooser (new co-tenant or
  existing same-root) with read-only conflict pass and confirmation (Yes replaces file state + ADDS
  same-name terminal tab; No untouched), transfer→switch→source-close ordering, acceptance-failure
  report/changed-source skip; Fresh itself only extracts a single tab (gvim extension). OPEN: no PTY
  handover (revive first dive), empty saved-name terminal tabs can duplicate, no undo/drag target/
  cross-workspace shared buffers, large overlay/welcome/closed-during-capture refuses whole, co-tenant-
  only, all-tabs-of-file (no Fresh action; multi-select substitute), close/eviction edges.
- **R13-15 (P2, M; Live restore backing remainder + status restart indicator landed; residual open)** — round-20 styled
  `screen` adds bounded 200-row/512 KiB history window, cursor row/col/visible/shape, alternate-
  screen flag; exact visible/history row-text match or ignored whole; old records unchanged;
  re-wrap drops metadata; read-only view paints history styles + reversed cursor; first dive hands
  live cursor/screen; daemon path; tests cursor/alt/history/bounds/corruption/migration. OPEN:
  scrollback bounded styled not byte-exact (deep history text-only), OSC not replayed, per-cell
  hyperlinks not representable, read-only cursor block, alt content marker not replayed, explicit
  dive, remote no state.
- **R13-16 (P2, S/M; input edges PARTIAL — backend limits)** — `chrome_focus_changed`
  (editor/explorer/dock/section; tabs gvim extension; menu/overlay/initial excluded) and
  `key_release` (only backends reporting `tea.KeyReleaseMsg`, e.g. Windows console; legacy
  silent; no kitty flags requested/faked); uppercase-without-Shift infers Shift (Ctrl
  unaffected), lock bits ignored, named keys keep reported Shift, AltGr-as-Ctrl+Alt with one
  printable rune is text input (bound chord wins). Tests: input_edges_test. OPEN: GPM console
  mouse, kitty, legacy key-release unavailable, terminal-window focus only for remote reconnect,
  live-screen reflow.
- **R13-17 (P2, M; SSH known_hosts/algorithms + r27 real keychain backends landed; residual open)** —
  round-20 user-config ssh keys (project refused): known_hosts_policy strict/accept-new/off parsed only
  to refuse, changed key always refused, host_key_algorithms ordered safe defaults, ProxyJump+target,
  keyring default false (lookup before prompt; accepted user password stored with notice). r27 real
  backends: macOS /usr/bin/security (find/add-generic-password, service gvim-ssh, account user@host;
  argv/no shell; password -w argv briefly process-visible); Linux secret-tool over Secret Service/DBus
  (lookup/store; password stdin; needs running keyring/DBus); used only when LookPath finds it, else
  no-op + honest “not stored”; bounded 64 KiB/env/context; one entry user@host, trailing newline
  stripped/other bytes preserved; fake runner tests. OPEN: passphrases/2FA/keyboard-interactive never
  keyring-stored, negotiated signature algorithm not separately exposed, macOS argv exposure, real
  hosts UNTESTED.
- **R13-18 (P2, M; remote env LANDED — residuals open)** — `env.Remote` probes markers and runs
  activation on the REMOTE host via authority one-shot (never local env/overlay), diffed against
  the remote login baseline; captured delta drives remote terminal/shell/formatter/LSP/search/
  git-grep; trust-gated, background warm, spawn paths read cached delta (Update never blocks).
  Tests: remote_env_test (remote-only runner, end-to-end, local overlay not forwarded, remote
  LSP env). OPEN: probe uses `sh -c`/remote login baseline rather than interactive remote login
  shell; env input files not watched (Reload/activate re-captures); cached until refresh/
  reconnect; Settings Environment rows only after warm; no status-row indicator of active
  remote env (Fresh has one).

- **R13-19 (P2, L; remote plugin/Session/copy/realpath landed; residuals)** — jump box/session
  restore; plugin fs/proc authority OS; r21 walk/copy; r22 realpath RealPather (tool/POSIX/real host
  residuals); r23 fs.copy streaming (ReadRange+WriteStream, 1 MiB chunks, atomic, no 16 MiB cap,
  destination untouched until commit; no-stream 16 MiB fallback); r24 copy tree root atomically
  claimed via DirectoryCreator (concurrent dir conflict, no merge; backend without capability old
  check-then-mkdir -p residual; parents before claim; failure after claim partial destination).
  OPEN: tree-root-only no-overwrite guarantee (descendants use per-file atomic replace, no exclusive
  file create); single-file copy replaces existing regular file by design; source growth beyond
  initial size not copied; no-stream 16 MiB cap; metadata/dir modes/mtimes not preserved; partial
  tree after failure; tree 100k entries; realpath tool/POSIX/real-host; plugin local-only APIs;
  remote marker scan; hot-exit local-staging privacy/conflict; ownership; real SSH UNTESTED.
- **R13-20 (P3, M; URL themes landed; packaged repo/marketplace open)** — URL theme forms fetch
  https/loopback http under the update transport policy (256 KiB/8 s), validate with the existing
  parser, cache under the user config dir keyed by URL hash with last-known-good fallback, clear
  failure notice, no fetch for builtin; --theme same forms/policy; Settings selects a URL fetched
  at this start (uncached reported, no Update-loop fetch); `repo#fragment` packaged-repository
  form refused with the explicit message. OPEN: packaged repository install/marketplace/browser,
  startup-only fetch/cache refresh, remote packs/local archives, theme background/drag edges.
- **R13-21 (P2, M; exclusive create + r27 remote paste conflict landed; rename/copy/overwrite race**
  **open)** — authority FileMutations + explorer New File/New Directory/Rename/Delete; r19 cut/copy/
  paste/duplicate; r21 exclusive create surface (local O_EXCL, SSH set -C noclobber, ErrExist, chmod
  failure never deletes, no-surface refusal); r27 remote paste/duplicate conflict choice: Cancel
  explorer.ErrConflict/no change, Rename prefilled CopyAs/MoveAs, Overwrite remove-then-copy (cut
  source only after success), Skip leaves destination/returns cut source; partial destination kept/
  reported; Duplicate same-dir Fresh free-name. R22 safe rename not landed; original rename kept, no
  new regression. OPEN: overwrite is REMOVE-THEN-COPY, NO ROLLBACK (re-created destination between
  removal/copy = documented race; file copy atomically replaces it, dir copy fails Mkdir), no
  Overwrite All/Skip/Skip All (bounded per-item), conflict decision not remembered across multi-item
  paste; rename/copy and plugin gvim.fs.copy destination no-overwrite only check-time; no drag/move,
  undo/history, whole-subtree transaction/cleanup UI, mode/mtime/hardlink/xattr preservation, watch
  latency, Windows paths, real host.
- **R13-22 (P2, L; remote large-file EDITABLE + STREAMED save landed; residual open)** — overlay
  over windowed base; r20 authority WriteStreamer streams 1 MiB merged windows into a remote temp
  (mktemp/bounded chunks/mode+mv; local temp+rename), arbitrary size bounded memory, failure/cancel
  target untouched, changed remote refused before stream, fallback 64 MiB without streaming.
  DIVERGENCE: Fresh server-side patched recipe; gvim streams client-merged bytes. OPEN: host-side
  patched recipe, non-windowed whole-file cap, large-file syntax highlight off, real host.
- **R13-23 (P2, M; remote data surfaces/search candidate residuals + preview syntax landed)** — git letters/finder refresh,
  search scopes/preview, grep project-file scope, r24 both-tool candidate consistency landed;
  r26 remote preview syntax highlighting landed (shared highlighter <=256 KiB/4000 lines, 8-entry concurrent cache, no extra remote I/O; open-buffer panel text/remote bounded head; over caps/no grammar plain; >256 KiB head-only/no semantic/LSP/remote preview buffer). remaining: failed/truncated listing hidden-only no notice (rg defaults; tracked-ignored omission),
  tool cap/panel Limit before filter, widened rg cap consumption, nested repo inner ignore, finder
  snapshot authoritative/backend walk, preview head-only >256 KiB/no semantic-LSP spans (bounded head syntax-highlighted), palette rows no git letters,
  incremental refresh/old rows/cache linger/fake-slot reconnect/per-directory finder round trips.
- **R13-24 (P2, L; full window scope/focus + editor-global plugin ids LANDED; residual open;**
  **141/149)** — per-mount chrome scope editor/window/buffer, focus announcer with workspace label/
  split window id/active buffer id, buffer activated/deactivated, switch seeding; round-20 editor-
  global window/buffer ids (per-mux sequence on Bind/direct=0, seq<<40|local, host decode/refuse
  foreign/released CodeClosed, global returns/events, ReleaseWorkspace frees sequence); project
  plugin config on activate, cross-workspace waits token→owning model. OPEN: 40-bit id hard cap;
  editor-scoped overlays model-local; editor-scoped widget instance state rebuilt not cloned;
  buffer-bearing mount cannot be editor-scoped; remote/authority workspaces own runtime; hard-kill
  last debounce.
### 5.1 User-visible TUI and editing gaps (do first)

| # | Gap | Pri | Size | Owner area | Notes / depends on |
|---|---|---|---|---|---|
| 12 | Markdown residual: free per-split view mode, dual split persistence, native Markdown menu row; exact byte dual anchors stay the recorded line-anchor approximation and wrapped-cell selection the recorded primary-row-only decision. Round 9–11 landed the renderer core/fences/tables/wrapped cells/borders/dual/heading marks/container fences/setext/task-image conceal/page-view persistence/mapped-decisions/overlay ordering | P1 | L | `editor`, `syntax`, `internal/tui/editorpane`, `internal/tui`, `session` | R10-2 |

### 5.2 Plugin-API breadth (unblocks Fresh's plugin-era features)

| # | Gap | Pri | Size | Owner area | Notes / depends on |
|---|---|---|---|---|---|
| 13 | Plugin host remainder (tiers 1a–1c; explorer/env edges): **composite buffers API remains OPEN** (createCompositeBuffer layouts/alignment/cursor/hunks/close and #15's composite surface; the native Compare covers the user feature, not the API), dock/fullscreen placement, independent per-panel views, windowEmbed live panes, markdown-in-text, package manager/generic HTTP (restricted gvim.http.get landed r28)/project-local plugins, render_line | P2 | L | `pluginapi`, `plugin`, `decorations`, `internal/tui/pluginpanel`, `internal/tui/plugin_*.go` | R9-5; no substitution closure |

| 14 | Port the remaining bundled plugins / native selection behavior (OPEN): orchestrator, live_sessions, agent_* family, devcontainer, k8s-workspace, pkg, audit_mode, welcome_screen, language helpers, **diff_nav residuals (R13-9: merged navigation landed; r45 saved-deletion seam + real provider interop; large-file/Compare/command+i18n gaps open)**. Ported through r44: diagnostics_panel, git_log/live_diff, compare_files, path_complete/lsp_help, Finder/lsp_navigation/find_references/dashboard/code_tour, git_grep/git_explorer/env_manager, asm_lsp (reduced helper; no plugin-API Disable/per-language restart/error metadata/remote home checks/overlong never-key), csharp_support (reduced helper; no setLspRootUri/Roslyn needs-restore event/Disable action/--version precheck, English-only), clangd_support (reduced helper; no server-specific switchSourceHeader/fileStatus notifications/floating panel/ancestor .clangd walk), welcome (demo reduced; language-aware comment/local time/English-only), markdown_source (r42 opt-in parity port; native markdown rules remain default; hidden enter/tab/shift-tab handlers; no markdown-source mode/auto-activation/status/compose-view exclusion), rust_lsp (r43 reduced helper opt-in; copy rustup/brew install commands/check rust-analyzer/help popup; no auto lsp_server_error status/contributed LSP menu rows/recovery detection/Disable Rust LSP/Configure Mode), deno_lsp (r44 opt-in reduced Check/Install/Help; no automatic server selection/baseline/active-window root/LSP-menu seam), diff_nav (r44 opt-in standalone Next/Previous Change over git_gutter/live_diff/saved diff; Fresh default-enabled difference due 8 MiB gate; duplicate git_gutter rows residual); phase 2 added the native external Claude/Codex history scanner (part of the agent discovery surface, not a plugin port). Native equivalents only where genuine (compare_files user feature + #15 API open, native theme editor, native project search, native markdown compose) with residual gaps listed; dashboard/live_grep are partial equivalents whose missing features stay open; welcome_screen remains OPEN. Nothing is closed by size/dependency/effort | P1 | L each; XL total | `plugin/bundled/plugins`, `plugin/bundled/lib`, `internal/agentdiscover/` | R13-6/R13-9; dependencies only order the work |

| 16 | Plugin package manager/registry (Fresh `pkg`: install/update/remove from git, registry sync, consent dialog) — OPEN TUI feature (pkg plugin 3642 lines port plus the native package-manager surface); dependency ordering only, not a closure | P2 | L | `plugin/`, `shellcmd`, package manager | R13-6 |

| 17 | Workspace-trust enforcement: per-spawn executable-path gate and Blocked denial (R1-4) **landed round 13** (`trust.DecideSpawn` wired through the local authority, plugin OS, LSP manager and discovery; typed refusals; docs/trust §3.1). Remaining OPEN: R13-4 residuals (shell first-token only, Windows PATHEXT/case, below-seam env, no kill on demote, remote coarse rule pending a remote trust store) | P2 | S/M | `trust/`, authority/pluginhost/lsp/agentdiscover spawn sites | R13-4 |

### 5.3 Daemon, workspaces, remote, agents (architecture)

| # | Gap | Pri | Size | Owner area | Notes / depends on |
|---|---|---|---|---|---|
| 18 | Daemon mode: client/server split, attach/detach, multi-client attach, auto-attach, open-file forwarding, registry v2 (co-tenants+authority_spec), jump box, remote session restore, remote hot-exit/trust/reconnect, checked exit durability, host marks, extract/close — **landed (MVP) rounds 12–19; OPEN reductions** (R12-1/R13-1/R13-3/R13-14/R13-19): no fork/daemonize, Windows pipes later, eviction/workspace CLI; phase-2 positional/dormant open + delay limits; r14 host marks; r16 registry v2; r17 remote target --daemon hosts remote workspace; r18 remote session restore; r19 multi-client (broadcast frames, any input, smallest size wins, detach keeps session, slow-client bounded detach, incompatible protocol version rejected) + auto_attach top-level user knob default OFF (Fresh on; bare same-root join notice, no daemon start/remote apply, named-daemon ambiguity refusal) + remote hot-exit staging/trust record/reconnect refresh. Still open: auto_attach default/named-daemon nuances differ Fresh, remote marker scan, hot-exit local-staging divergence, plugin id namespacing, shared buffers; remote tabs/stashes restore landed r18; real SSH UNTESTED | P1 | XL | `internal/daemon`, `internal/app`, `internal/cli`, `internal/tui`, `session`, `internal/restore`, `trust` | R12-1/R13-1/R13-3/R13-14/R13-19; docs/architecture §8/§8.1 |





| 19 | Workspaces/windows: registry v2 (co-tenants + authority_spec), per-workspace plugin state, shared daemon plugin runtime + full per-mount window scope/focus announcer, per-workspace project plugin config + cross-workspace waits, remote session restore, extract variants, multi-client/auto-attach; OPEN #19b: cross-workspace plugin id namespacing (window/buffer ids per workspace), remote/authority workspaces own runtime, hard-kill last debounce, shared buffers/open-path map, eviction/workspace CLI, auto_attach per-root/named-daemon differences, extract terminal-in-multi/mixed selection/drag-drop/cross-workspace undo/all-tabs substitute, orchestrator/live_sessions/agent_* | P1 | L | `internal/daemon`, `internal/app/workspace_* + daemon_authority/remote_session`, `internal/tui/{workspace_*,plugin_shared,split_state}`, `internal/pluginhost`, `session`, `internal/restore` | R13-1/R13-3/R13-14/R13-19/R13-24; docs/architecture §8.1 |





| 20 | Agent sessions: tracked launch argv/agent metadata/resume, Run Agent picker, tmux discovery/Import (r13) and phase-2 external Claude Code/Codex transcript-history scanning **landed**, with these OPEN: dock/agent rows outside the picker, blocked/done heuristics, remaining agent_* family plugins, other multiplexers/remote scanning, per-session workspace identity/first attach launch, worktree creation, tmux level 1/2 | P1 | L | `internal/terminal`, `internal/tui/dock`, `internal/agentdiscover` (+`history.go`), `config/terminal.go`, plugin packages | R12-3/R13-2 |
| 21 | Remote authority: slot/local/SSH terminal+exec+file system, scp/ssh parsing, status/reconnect, editor I/O+LSP, plugin fs/proc authority OS, watch, finder/search, replace, explorer writes/ignore/watch/clipboard, env, SSH auth/config/ProxyJump/agent forwarding, jump box, editable windowed remote large-file bounded save, remote hot-exit/trust record/reconnect, saved-session restore, FileMutations/ReadRange **landed**, OPEN: >64 MiB/Fresh host recipe, search preview head-only >256 KiB/no semantic-LSP spans (bounded head syntax-highlighted r26/r28)/ignore, explorer drag/move/undo/whole-subtree transaction/mode-mtime-hardlinks-xattrs/existence race/overwrite prompt/watch latency, remote marker scan, hot-exit local-staging divergence, ~user/special/sudo, reconnect/terminal respawn, devcontainer/k8s, SSH known_hosts accept-new/OTP/keyring, saved-session conflict detection, restored terminal dead/ownership, real SSH UNTESTED | P1 | XL | `authority`, `internal/app/remote_* + remote_copy/remote_recovery`, `internal/pluginhost` (authority OS), `finder`, `document`, `internal/tui`, `trust` | R13-3/R13-17/R13-19/R13-21/R13-22/R13-23 |






### 5.4 Polish and optional (P2)

| # | Gap | Pri | Size | Owner area | Notes / depends on |
|---|---|---|---|---|---|
| 22 | i18n: foundation/catalogues/import/validation + extraction 2/3 + r14 palette + r15 remainder + r16 picker + r17 rem2 + r18 rem3 + r19 rem4 + r20 rem5 (15 locales; en 998 keys, every non-English 575; key+value 573,676 B/560 KiB under 640 KiB text cap; JSON 664,221 B/≈649 KiB; heap en 114 KiB/de 82 KiB/both 197 KiB; Fresh-key imports/fresh_map, deterministic CI, lazy load, Settings locale row, plugin i18n). OPEN: rows without Fresh keys, structured schema names, some keybinding help/plugin-manager edges; restore_live config-only; self-update commands absent (R13-5); full-suite EXIT 0 (76 packages, log /tmp/gvim-r20-fixed.log) | P1 | L infra; XL translation | `i18n/`, `internal/tui`, `commands/gen`, `tools`, `docs/i18n.md` | R13-5 |







| 23 | Terminal Windows support (ConPTY): implemented pure-Go x/sys/windows with windows build tag + GOOS=windows build/vet/test-compile, pure Windows helper tests, real-Windows acceptance checklist in docs/terminal.md; **parity UNVERIFIED until a real-Windows pass** (open, not accepted) | P2 | M | `internal/terminal` | R13-6 |
| 24 | Update check/self-update/telemetry: update check/{update} indicator (r15) + r16 clickable Update details + Check for Updates/Show Update Output rows (palette 240) **landed partially**; self-update download/install/prompt, Fresh "Update fresh" self-update command and update terminal buffer remain OPEN (R13-12). Detail click never installs; check tea-command/config-env-default feed precedence, https/loopback policy, 8 s/64 KiB, daily stamp, no-version skip, silent failures; telemetry/GUI/browser --web/WASM/packaging boundaries | P3 | S | `updatecheck/updatecheck.go`, `config/update.go`, `internal/app/update_check.go`, `internal/tui/{update_status,update_commands}.go`, `commands/update_commands.go`, `frame/frame.go` | R13-12 |




| 25 | Platform edges: stdin-as-a-buffer, opt-in/Trusted OSC52 read/paste, and round-15 legacy input slice landed — uppercase-without-Shift infers Shift (Ctrl unaffected), CapsLock/lock bits ignored, named keys keep reported Shift, AltGr-as-Ctrl+Alt printable text input; plugin `chrome_focus_changed` (editor/explorer/dock/section; tabs gvim extension) and `key_release` (only backends reporting `tea.KeyReleaseMsg`, e.g. Windows console; legacy silent, no kitty flags requested/faked); tests input_edges_test. OPEN: GPM console mouse, kitty keyboard, legacy key-release unavailable, terminal-window focus only for remote reconnect, live-screen reflow; ConPTY/real-host OSC52 UNVERIFIED | P2 | M | `internal/terminal`, `internal/tui/input_edges.go`, `internal/app/clipboard_osc52.go`, input layer, `internal/cli` | §4 item 94/R13-16; R11-3/R13-6 |


| 26 | Fresh GUI/desktop, browser `--web` UI, WASM build, packaging/installer matrix, daemon packaging: **outside the TUI objective** (objective boundary in §8 — the user asked for Fresh TUI functionality; Lua and visual style are the only flexible parts). Daemon mode itself is in scope; these are not backlog gaps unless a TUI feature needs one (none does). Not closed by size/effort — closed by not being TUI | objective boundary | — | n/a | §4 item 81; §8 |
| 27 | Remote explorer write/watch/clipboard: r17 New File/New Directory/Rename/Delete; r18 remote .gitignore + polled directory watch (gvim extensions); r19 cut/copy/paste/duplicate over authority (10k entries/256 MiB/64 levels/2 min, atomic per-file binary-safe, cut copy-then-remove, paste refuses existing name, duplicate Fresh free-name, symlink/root/outside/inside-source refusals before creation, confirmations naming target, trust gate/disconnected/refresh, partial destination kept/reported). OPEN: no drag/move, no undo/history, no whole-subtree transaction/cleanup UI, FileEntry/Mode/mtimes/hardlinks/xattrs not preserved, copy/paste/rename destination-exists refusal only at check time (concurrent create can be overwritten), no overwrite prompt, watch latency/rename approximation, Windows paths, real host | P2 | M | `authority`, `internal/app/{remote_explorer,remote_explorer_ops,remote_explorer_copy,remote_ignore}`, `internal/tui/remote_tree`/`explorer`, `explorer/gitignore.go` | R13-13/R13-21 |




| 28 | SSH auth/connection config parity: ssh-agent forwarding, ssh_config/ProxyJump/Host aliases/IdentityAgent/host-key algorithms, configurable known_hosts, keyring/agent secret storage, server-specific OTP retry policy, keepalive/backoff settings; all tests offline (no real host/2FA) | P2 | M | `authority/ssh.go`, `internal/tui/authority_retry.go` | R13-17 |
| 29 | Remote env residuals: probe through interactive remote login shell (not sh -c/baseline), watch remote env input files, un-cache/mark refresh, Settings Environment rows before warm, status-row indicator of active remote env | P3 | S/M | `internal/app/remote_env.go`, `env/`, status row | R13-18 |
| 30 | Terminal live-restore parity: Fresh focus-revival from a persisted backing file (gvim dive + bounded transcript text), remote restores no terminal/layout state; per-tab splits/kitty/reflow/report-split remain | P2 | M | `internal/tui/dock`, `internal/restore/terminal.go`, `config/terminal.go` | R13-15 |
| 31 | Input/backend edge limits: no kitty keyboard event flags requested/faked, no legacy key-release reporting, GPM console mouse, terminal-window focus only for remote reconnect, live-screen reflow | P2 | M | `internal/tui/input_edges.go`, `internal/terminal` | R13-16/R13-6 |

| 32 | Remote project replace residual: no cross-file rollback and no undo history for direct remote writes (undo across a run), dirty open buffers skipped, remote .gitignore/large-file filtering not in candidate list, local preview/git-status adapter stays cleared | P2 | M | `internal/app/remote_replace.go`, `internal/tui/project_search.go` | R13-19/R13-11 |

| 33 | Remote data surfaces residual: palette file rows carry no git letters (local rows do not either), project-search panel preview column unwired, finder File Explorer Refresh is a full re-walk not incremental, open palette keeps old rows until re-rank/reopen, preview cached before disconnect may linger, no full fake-slot buildRuntime reconnect test, finder walk per-directory round trips | P2 | M | `internal/app/{remote_git,remote_preview}.go`, `finder/index.go`, `internal/tui/palette/preview.go` | R13-8/R13-23 |

| 34 | URL/packaged themes residual: packaged repository form refused (`repo#fragment`), marketplace/browser not ported, URL fetch is startup-only (no in-session fetch/cache refresh), remote packs/local archives; theme background images/drag/inheritance edges | P3 | M | `theme/url.go`, `theme/registry.go`, `internal/app/theme_url.go`, `docs/themes.md` | R13-20/R13-6 |

| 35 | Remote large-file windowed mode: r17 read-only → r18 EDITABLE overlay → r20 STREAMED save above 64 MiB (authority WriteStreamer: SSH mktemp+bounded chunks+mode/mv, local temp+rename, Abort removes temp target untouched, atomic/non-append/no-parents only; 1 MiB merged windows streamed bounded memory; changed remote refuses before stream; fallback 64 MiB without streaming; fresh source rebase). DIVERGENCE: Fresh server-side patched recipe; gvim streams client-merged bytes. OPEN: host patched recipe, non-windowed whole-file cap, syntax highlight off, real host | P2 | L | `authority`, `document/{remote_large,large_save}.go`, `buffer`, `internal/app/remote_editor`, `internal/tui` | R13-22/R13-3 |




| 36 | Update command/self-update residual: self-update download/install/prompt, Fresh Update fresh command, Fresh update terminal buffer (gvim details list), clickable release page starts no install; telemetry/GUI/browser/WASM/packaging boundaries | P3 | S/M | `updatecheck`, `internal/app/update_check`, `internal/tui/{update_status,update_commands}`, `commands/update_commands.go` | R13-12 |
| 37 | Per-workspace plugin state + shared daemon runtime/scopes/ids: state sessions/<id>/plugin-state.json v1; one shared runtime LOCAL daemon workspaces; r19 per-mount editor/window/buffer chrome scope + focus announcer + project config + cross-workspace waits; r20 editor-global window/buffer ids (seq<<40|local, CodeClosed foreign/released, global returns/events, ReleaseWorkspace), plugin authority OS. OPEN: 40-bit hard cap; editor-scoped overlays/widget state model-local; buffer-bearing mount cannot be editor-scoped; remote/authority own runtime; hard-kill debounce | P2 | M | `internal/pluginhost`, `internal/tui/plugin_shared.go`, `internal/app/workspace_plugin_state_test`, docs/plugins — R13-1/R13-24 |




| 38 | Daemon jump-box residual: r18 remote session restore across restart landed; r19 plugin fs/proc authority OS, remote hot-exit staging/recovery, remote trust record keyed user@host[:port]:workspace root, reconnect cache refresh/one notice. OPEN: remote executable marker scan not wired (undecided remote markerless records level), hot-exit local-staging divergence from Fresh, conflict detection only stash/mtime, restored terminal transcript dead, plugin local-only API boundary, per-file ownership jump-box vs direct, real SSH UNTESTED | P2 | L | `internal/app/{daemon_authority,remote_session,remote_recovery,workspace_capture}`, `internal/pluginhost`, `trust`, `internal/daemon` | R13-19 |



| 39 | Live-restore full backing-file fidelity: only visible screen styled (≤200×512), no backing-file bytes, cursor/alternate-screen/OSC/hyperlinks not restored, revive needs explicit dive, remote launches restore no state | P3 | M | `internal/terminal/saved.go`, `internal/tui/dock`, `internal/restore/terminal.go` | R13-15 |

| 40 | SSH config/connection residuals: known_hosts default only/no accept-new, no host-key algorithm config, ProxyJump one hop, no keyring/agent secret storage, keyboard-interactive OTP retry policy unchanged, keepalive/backoff package vars not settings, real SSH/agent/2FA UNTESTED | P2 | M | `authority/ssh.go`, `authority/ssh_config.go`, `internal/tui/authority_retry.go` | R13-17 |

| 41 | Shared plugin runtime/window scope/chrome/ids: LOCAL daemon workspaces share one runtime, OS/host mux, switch rebind, editor-wide registration carry, workspace chrome per model; r19 per-mount editor/window/buffer scope + focus announcer + config/waits; r20 editor-global window/buffer ids (seq<<40|local, CodeClosed foreign/released, ReleaseWorkspace). OPEN: 40-bit id cap, editor-scoped overlays/widget instance state model-local, buffer-bearing mount cannot be editor-scoped, remote/authority runtime exclusion, hard-kill debounce | P2 | L | `internal/pluginhost`, `internal/tui/plugin_shared.go`, `internal/app/workspace_host`, docs/plugins | R13-24 |


| 46 | Plugin authority OS: remote/daemon-hosted remote workspaces wrap the plugin OS with pluginhost.NewAuthorityOS; files/mutations/watches/processes via workspace slot, trust gate/env/cwd, remote captured env; LookPath/Dirs/Getenv/Now local-only; walk/copy remote-to-remote (16 MiB/file, symlink refusal) and realpath through authority RealPather (tool/POSIX-remote dependency; absolute/existence semantics); unsupported mutations clear errors; remote watch polling with degradation notice; local unchanged. Residual: realpath tool availability/POSIX-only/real host UNTESTED; local-only API boundary/watch backend support | P2 | M | `internal/pluginhost/`, `internal/app/run.go`, `plugin/hostos`, docs/plugins | R13-19/R13-3 |

| 47 | Remote hot-exit/trust/reconnect: direct remote launch stages unsaved remote docs under remote-target session dir (daemon uses workspace session dir); recovery after FileAccess; clean exit clears; remote trust record keyed user@host[:port]:workspace root in trust.json; reconnect refreshes watch/finder/git/explorer/LSP + one notice. Residual: local-staging divergence from Fresh authority-routed recovery; marker scan not wired; real host | P2 | M | `internal/app/{remote_recovery,remote_session}.go`, `trust`, `internal/app/run.go` | R13-3 |

| 48 | Daemon multi-client + auto-attach: multiple attach clients broadcast frames/any input/smallest size wins/detach one keeps session/slow-client bounded detach/incompatible version rejected; auto_attach user knob default OFF (Fresh on), bare same-root join notice, no daemon start/remote apply, named-daemon ambiguity refusal. Residual: per-root named daemons vs Fresh shared-daemon nuances, default difference by design, no fork/daemonize, Windows pipes, eviction/workspace CLI | P2 | M | `internal/daemon`, `internal/app`, `internal/cli`, docs/architecture §8.1 | R13-1 |

| 49 | Full plugin window-scope chrome + focus announcer: per-mount editor/window/buffer scope; active_window_changed/focus_gained with workspace label/focused split window id/active buffer id; buffer activated/deactivated; switch seeding; widget focus widget_event. Residual: window/buffer ids per-workspace not globally stable across switches (cross-workspace id namespacing follow-up), editor-scoped overlays/widget instance state model-local | P2 | M | `internal/tui/plugin_shared.go`, `internal/pluginhost`, docs/plugins | R13-24 |

| 50 | Remote explorer clipboard: r19 cut/copy/paste/duplicate over authority (10k entries/256 MiB/64 levels/2 min, atomic per-file binary-safe, cut copy-then-remove, paste collision refuse, duplicate Fresh free-name, symlink/root/outside/inside-source refusals, confirmations naming target, trust gate/disconnected/refresh, partial destination reported). Residual: no drag/move, no undo/history, no whole-subtree transaction/cleanup UI, mode/mtime/hardlinks/xattrs not preserved, existence-check race, no overwrite prompt, real host | P2 | M | `internal/app/remote_explorer_copy.go`, `authority`, `internal/tui/remote_tree` | R13-13/R13-21 |


| 42 | Remote search scopes/preview: current-file/open-buffers scopes over remote document snapshots (unsaved edits, no remote process/local disk) and remote preview column (open-buffer text or head ≤256 KiB via authority windowed/whole-file, honest truncation/stale/disconnect) landed r18. OPEN: head-only >256 KiB/no semantic-LSP spans (bounded head is syntax-highlighted since r26/r28), remote .gitignore/candidate filtering not applied to search list | P2 | M | `internal/app/remote_search.go`, `internal/tui/project_search.go`/`searchpanel` | R13-11/R13-23 |

| 43 | Remote .gitignore/watch are gvim EXTENSIONS (not Fresh parity gaps): same go-git parser as local, remote root/nested .gitignore through authority (64 levels/64 KiB/4096 rules), ignored hidden default/show toggle, finder same batch per index lifetime; directory watch uses polled authority Watch (WatchDirs, debounce/off-Update, reconnect/cancel, unsupported manual notice). Residual: latency/rename approximation, ignore edits next reload/index restart, Windows paths | P3 | M | `internal/app/remote_ignore.go`, `explorer/gitignore.go`, `internal/app/remote_explorer.go` | R13-13 |

| 44 | Extract variants are gvim EXTENSIONS (Fresh only has extract-to-new-workspace): into existing same-root workspace (picker, target acceptance before source close), terminal tab saved-state migration (no PTY hand-off, target revives first dive, source shell stops after acceptance), multi-select file tabs to one new co-tenant. Residual: terminal tabs not in multi-select/mixed selection, no drag/drop/cross-workspace undo, large-file overlay refused, all-tabs-of-file no Fresh action (multi-select substitute) | P3 | M | `internal/app/workspace_extract.go`, `internal/tui/workspace_ui.go`, `internal/tui/list` | R13-14/R13-1 |

| 45 | Remote daemon-workspace session restore: landed r18 (tabs/open files/stashes/cursor/layout, reconnect before reopen, missing-file stash notice, keepSavedSession on failed reconnect, detach/reattach live state). Residual: conflict detection beyond stash/mtime, restored terminal transcript dead/no process re-run, plugin FS local, local trust, ownership, real SSH UNTESTED | P2 | M | `internal/app/{remote_session,workspace_capture,run}.go`, `internal/daemon/workspaces.go` | R13-19 |





### 5.5 Suggested round order for the remaining goal

1. **Round 20 — close the residual TUI gaps left by round 19.** Cross-workspace plugin id namespacing
   and remote/authority runtime sharing (R13-24); remote large-file >64 MiB/Fresh host recipe;
   remote explorer drag/move/undo/whole-subtree cleanup/mode-mtime-hardlink-xattr/existence race/
   overwrite prompt/watch latency/Windows paths; remote search preview head-only >256 KiB/no semantic-LSP spans (bounded head syntax-highlighted since r26/r28) and
   ignore/candidate filtering; remote authority marker scan/hot-exit local-staging/conflict
   detection/ownership; SSH known_hosts accept-new/host-key algorithms/OTP/keyring/real host;
   remote env; shared buffers; extract terminal-in-multi/mixed selection/drag-drop; live-restore
   full backing file; input GPM/kitty/legacy release/reflow; update self-update/terminal buffer;
   URL packaged repo/marketplace; theme background/drag; plugin ports/composite/gallery; auto_attach
   named-daemon/default nuances; universal server; Windows ConPTY; i18n remaining boundary.
2. **Round 21 — final verification and honest endgame.** Freeze the Fresh reference (commit/date),
   re-read refs/fresh docs/features + internal, re-run §2 counts (palette 240, menus/keymaps/themes,
   bundled 22 vs 50 + ports, plugin API 242 vs ~372, settings 69/14, e2e index) and refresh every §3
   row; walk every §4 divergence/§5 residual/closed id under TUI-only policy; run full gates
   (build×2, vet, lint, gofmt, purity/import, menus, i18n_check + heap, GOOS=windows, full go test,
   real-host SSH/tmux/ConPTY where reachable); docs-vs-code audit. Report explicit open items and
   objective boundaries, not substitutions; full parity only if open TUI list empty.

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
    docs/architecture §8): server attach lifecycle (r12 single-client; r19 multi-client landed: broadcast/any input/smallest size, hello/resize/input/open_file/
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
- **Round 13 landed (2026-11-09).** Six tracks landed (workspace registry/lifecycle #19a,
  per-workspace terminal tabs, tmux discovery/Import #20b first slice, remote editor file I/O +
  LSP over SSH #21 continuation, spawn gate #17, i18n extraction 2); this audit ran the static
  gates and reviewed the diff (the full `go test` suite was run by the Lead, not simultaneously
  by this audit; no scope creep; §7):
  - **Workspace registry/lifecycle (#19a)** (`internal/daemon/{workspaces,workspace_protocol,
    framewriter}.go`, `internal/app/{workspace_host,workspace_capture}.go`,
    `internal/tui/{workspace_host,workspace_ui,workspace_tags}.go`, `docs/architecture.md`
    §8.1): durable registry (one per-canonical-root file under the data dir, stable id/label/
    root/layout, atomic + versioned saves, dedup, corrupt/future skipped safely, per-workspace
    session dirs), workspace frames + server dispatch + app handlers, one tea program over a
    model-swapping host, eager foreground/inert placeholders, async switch with sequence +
    cancellation and stale-result closing, snapshot/write capture ordering, warm runtimes,
    workspace UI (switch/new/close/rename/next, switcher, `WS <label>`), close semantics and
    daemon-quit save/write/close ordering. Tests: registry, wire, manager lifecycle, async
    switch/failure/stale, capture compare vs restore.WriteSession, create-with-launch,
    workspace terminal tabs across switch/close/capture/restore.
  - **Terminal tabs per workspace** (`internal/tui/dock/tabs.go`, `internal/app/workspace_*`,
    `session/restore` formats, `docs/terminal.md`): each workspace has its own dock/tab set and
    `terminal.json`; warm tabs keep live PTYs across switches, dormant restores read-only/dead,
    close kills shells and keeps the transcript; titles from launch argv/agent with Fresh's
    ` (2)`/`(exited)` rules, launch_dir persistence, Import attach tabs keyed by durable source
    identity; additive SavedState `tabs[]`/`active_tab` with mirror/bounds/old-file migration.
    Tests: dock tabs, workspace terminal tabs (warm PTY survives, close kills, capture/restore
    names/active/transcripts/launch records), restore terminal tabs.
  - **Discovery + Import (#20b first slice)** (`internal/agentdiscover/`,
    `internal/tui/agent_discover.go`, `internal/app/{agent_discover,workspace_host}.go`,
    `commands/agent_discover_commands.go`, `docs/terminal.md`): read-only bounded tmux socket
    scan, trust-gated (spawn gate + resolved executable) list-sessions/list-panes, qualified
    attach argv, probable-agent name matches, socket-only rows, scanning picker (problems/
    filtering/Ctrl+R/Esc), Enter opens an attach tab, `i` imports as workspace + queued attach
    tab with repeat-import focus. Open limits (R13-2): no dock rows outside the picker, no
    per-session workspace identity/initial-launch registry field (same-root sessions share a
    workspace with extra attach tabs), no state heuristics, no other multiplexers/remote, no
    Claude/Codex scanners, attach tabs restored dead. Tests: scanner (fake runner, sockets,
    sessions/panes/agents/stale/problems/bounds), picker/import, app import launch.
  - **Remote editor file I/O + LSP over SSH (#21)** (`authority/ssh_files.go`,
    `document/fileaccess.go`, `internal/app/{remote_editor,remote_lsp}.go`, `lsp/manager`
    spawner/reconnect, `internal/tui/{authority_status,remote_startup}.go`,
    `docs/authority.md` §6/§7): real remote FileSystem as bounded POSIX `sh` helper commands
    (chosen on evidence: no usable offline SFTP module/no network, pure-Go policy, no remote
    Python requirement) with caps, GNU+BSD stat, sorted listing, atomic temp+rename writes
    (mode/symlink rules), polling watch, error mapping and POSIX quoting; editor loads/saves/
    Save As/Revert/disk checks routed through the seam with no local fallback; async startup
    open retried after reconnect; remote LSP via the LongRunning surface with a manager spawner
    seam, remote root/env, stderr tail, limits recorded not applied, spawn gate first, and one
    restart per reconnect without duplicates. Open residuals R13-3. Tests: ssh_files (read/cap/
    atomic/mode/symlink/stat/list/watch/quoting/drop), editor routing, startup retry, remote LSP
    handshake/spawner/gate/reconnect. Real-SSH acceptance unrun.
  - **Spawn gate (#17)** (`trust/gate.go` + authority/pluginhost/LSP/discovery wiring,
    `docs/trust.md` §3.1): `DecideSpawn` level matrix, child-PATH resolution + execute-resolved
    path, explicit/symlink root checks, shell `-c` first token, typed refusals; Blocked refuses
    all, Restricted/Undecided refuse project-internal executables; coverage tests in trust,
    authority, plugin OS, LSP manager and app. Open residuals R13-4.
  - **i18n extraction 2 (#22b)** (`i18n/locales/*.json`, generators, `internal/tui/
    {command_i18n,menubar/table}.go`, `internal/app/settings_i18n.go`, `docs/i18n.md`): ~400
    English keys (en 416), palette/menu titles, first settings-label subset and remote status;
    Fresh-key identity import + `fresh_map.json`; gvim-only keys English; deterministic import/
    validation/coverage CI; 15 locales; 294,448 JSON bytes (~194 KiB key+value; 256 KiB cap).
    Open boundary R13-5.
- **Round 13 review findings (all fixed and re-verified post-audit).** (1) Close ordering/capture-
  vs-exit: `workspaceManager.Close` now queues the capture on the UI goroutine and waits
  `job.done` before the synchronous runtime close/save, so the two writers cannot race;
  `TestCloseAwaitsCaptureWriteBeforeExit` (blocked writer proves no sync save/return early) and
  `TestCloseFailureReportedAndFallbackSaves` pin it. (2) Close-during-materialization: closing a
  workspace whose switch is pending bumps the sequence, cancels the build and clears `pendingID`,
  so `applyCommit` drops the stale build and the registry file stays removed;
  `TestCloseDuringMaterializationDoesNotResurrect` deliberately ignores the context cancel and
  proves no re-registration/materialization/landing. (3) Close failure propagation/retention: a
  failed capture write now attempts the synchronous fallback save before closing; if neither is
  confirmed the workspace, runtime and registry file are retained and the error is returned and
  reported (no silent drop/removal). `TestCloseRetainsWorkspaceWhenBothWritesFail` asserts the
  both-fail error names both failures, the workspace/runtime/entries survive with no session file,
  and a retried writable close succeeds keeping the unsaved text. Remaining open (unchanged):
  dormant-workspace single-writer gap; capture snapshot→write crash window; and untested real
  hosts (real-SSH remote editing/LSP, real tmux, real Windows ConPTY; the full suite was run by
  the Lead, not this audit).
- **Round 13 integration/gate notes.** After the three close fixes landed, this audit
  re-ran the targeted close/workspace tests (−count=2; all pass:
  `TestCloseAwaitsCaptureWriteBeforeExit`, `TestCloseFailureReportedAndFallbackSaves`,
  `TestCloseDuringMaterializationDoesNotResurrect`, `TestCloseRetainsWorkspaceWhenBothWritesFail`,
  `TestWorkspaceCloseWaitsForPendingWrite`, `TestWorkspaceCloseAllWithPendingWrite`,
  `TestWorkspaceCaptureWritesAreSerialized`, `TestWorkspaceMaterializeIsAsync`,
  `TestWorkspaceSwitchStaleResultCannotLand`) plus `go build`/`go vet ./internal/app`, gofmt on
  the touched files and `git diff --check` (clean). No full suite was re-run. Static gates were
  green earlier in this phase (build, CGO0 build, vet, golangci-lint 0, gofmt, purity, import
  rules, menu check, GOOS=windows build); the Lead's full `go test ./... -count=1` passed all
  packages and the i18n check script. Metrics: palette 236; bundled 22/9/13 and idle heap
  ~7.25 MiB unchanged; plugin API 242 functions/1478 lines; i18n en 416 keys / 15 locales /
  294,448 JSON bytes; no leftover temp harnesses.
- **Phase-2 integration wave (2026-11-09, on `ecd27e6` + uncommitted work).** Landed and
  verified read-only: workspace open-file positional/cwd frames + dormant-owner single-writer
  (cross-cwd/dormant/stale/stash/materialize-failure tests green; the earlier cross-cwd red was
  the incomplete/debug state and is green now); daemon `closeAllChecked` exit durability with
  shared deadline, fallback save, recovery-copy last resort, non-zero exit, and late-writer
  isolation (`captureWriter` lock/sequence/`committed`/`stopped` + `skipSave`; blocked writer
  holds the newest snapshot, fallback must not write, recovery copy is named; exit suite
  −count=3 all green, delayed-writer regression included); terminal tab reordering (palette
  rows/chords/drag, persisted order) and trust-gated OSC 52 read/paste (default off, Trusted
  only, 64 KiB, no reply on error/oversize); remote external-change watching (polled remote
  Watch bridged to RootPorts; generation/coalesce/retry/Close tests green); i18n extraction 3
  (en 706 keys; all-locale key+value 529,809 B under the 640 KiB cap; embedded JSON 612,378 B;
  heap budget test); external Claude/Codex transcript-history discovery (bounded/read-only,
  fixed resume templates); plugin debug markers (`WSDBG`/`TUIDBG`) are gone.
  - **Gate record (Lead+audit):** the Lead reports ALL static gates green (build ×2, vet,
    golangci-lint, gofmt, purity, import rules, menu, i18n, GOOS=windows) and the full
    `go test ./... -count=1` with one plugin failure at that phase-2 snapshot
    (`TestBusyTimerIsThrottled`; historical record). The current round-14 full-suite record
    supersedes this in §7: three plugin failures at that moment (heap guard later diagnosed as test baseline contamination + two
    busy-timer tests), recorded as failures with no flake assumption. This audit ran
    build/vet/gofmt/i18n-check and the targeted suites above (green; exit −count=3, close
    −count=2, remote watch, OSC52, tabs, workspace cross-cwd/dormant, agent discovery); it did
    not rerun the full suite.
  - **Exit limits recorded:** an in-flight newest capture at the deadline is left to finish;
    the process does not wait for it after the deadline, so the session file may keep the
    previous snapshot — the crash-recovery copy is the durable last resort and the error names
    it; a kill between snapshot and write loses that last capture (previous session intact).
  - **Remaining/open:** #19b/daemon reductions, discovery completion, remote completion (incl.
    finder), spawn-gate residuals, i18n remainder, terminal live restore/per-tab splits/kitty/
    reflow/report-split, theme/URL/background gaps, update indicator, platform edges, plugin
    ports, and the four newly recorded gaps R13-7 (completion fidelity), R13-8 (remote finder),
    R13-9 (diff_nav residuals: merged navigation landed; large-file/Compare/command+i18n remain), R13-10 (direct-launch single-writer warning). `docs/parity.md` is the
    record; the full suite later passed (see §7), but no full-parity claim is made.
- **Round-14 wave (2026-11-09, on `f8657ec` + uncommitted work, tasks 89–104).** This audit
  read the current tree and ran targeted tests (no full suite): `go build ./...` green; gofmt on
  changed files clean; `bash tools/i18n_check.sh` green; targeted green for authority QuoteDir
  real-shell, remote finder app/finder/palette, remote project/Live Grep guards, daemon host
  marks/named refusal/no-lost-work/mark-failure + CLI daemon flags, split session/restore/tui/
  app/workspace suites, buffer-word editor/TUI suites, git_gutter navigation, i18n catalogue/
  commands. LSP completion safety increment (task-97) is verified below with residual UX gaps, not counted as
  fully Fresh-identical; R13-7 stays open for the residual gaps. No commits were made by this audit.
  - **Heap-guard test diagnosis (not a production-guard change):** the failing
    `TestHeapGuardStopsADoublingString` was contaminated by the shared package process's
    `HeapSys`/garbage baseline (other tests leave hundreds of MiB; suites allocate concurrently),
    not by the guard logic. trust made it a single fresh child process with strict parsing of every
    reported field and kept the 4× ceiling; the production guard was not changed. Isolated ×10
    and the plugin package pass; the two timing tests are unchanged. The 4× bound is a test
    contract with scheduling-tail risk (a late 1 ms sample can miss one doubling; observed max
    255 MiB for the 64 MiB test limit) and the production guard remains a sampled circuit breaker,
    not a strict hard cap.
  - **Final verification (Lead):** `go test ./... -p 1 -count=1 -timeout 1200s` exited 0 with
    all packages passing (plugin 30.6s, internal/app 46.8s, lsp/manager included); static gates
    all green incl. build, CGO_ENABLED=0 build, GOOS=windows build, vet, lint 0, purity, import
    rules, menus, i18n, `git diff --check`. `TestDynamicRegistrationUnlocksRouting` was a fake-
    server synchronous-scheduling deadlock in the test infrastructure (not a product bug); it was
    changed to asynchronous dispatch and passed 40/40 stress runs. This is a full-suite pass, not
    a claim of full Fresh parity: real hosts (SSH remote finder/search, remote editing, Windows
    ConPTY, OSC52) remain untested.
  - **Remote finder + search protection (task-99):** ~/… real-shell expansion with injection-safe
    quoting (QuoteDir slash fix), only-authority enumeration (idle placeholder, no local fallback),
    local ports cleared and every remote project-search/Git Grep/replace/Live Grep entry point
    refused with a notice before local calls, error/truncation rows; tests named in the round-14
    block. Residuals recorded: remote git marks, remote preview, remote finder refresh/reindex,
    remote search through the authority (R13-11).
  - **Daemon host single-writer (task-100):** per-root/per-name marks, atomic write, stale mark
    cleanup, mark-failure refuses daemon start, all direct modes (incl named daemons) refuse with
    attach instructions, refused launch leaves unsaved work/disk untouched; R13-10 CLOSED as a
    deliberate divergence from Fresh's shared-buffer ownership.
  - **Split persistence (task-102):** additive optional session `layout`, capture without switching
    tabs, restore by stable path over restored files, invalid/stale/malformed → one pane,
    compare/dual/hot-exit-clean/remote skip, daemon workspace restart restore; tests green.
  - **Buffer words (task-98):** no-LSP provider + TUI local-first/two-phase merge, stale reply
    drops, large-file deferral, active-buffer only, manual trigger; tests green. **LSP completion
    safety increment (task-97) is verified: snippet expansion, UTF-16 additional edits, resolve,
    version+overlap guard, local-first merge; residual UX gaps are recorded in R13-7, which stays
    open for those gaps, not for the safety behaviour.**
  - **diff_nav (task-101) / i18n (task-104):** merged git/live/saved navigation stable (R13-9
    remains PARTIAL with large-file/cross-Compare/command-i18n residuals); Move Tab Left/Right
    command keys + file_explorer.compact_directories localization landed (en 710 keys;
    all-locale key+value 534,728 B/~522 KiB; JSON 617,897 B; i18n check green).
- **Round-15 final verification (2026-11-09, merged tree on `4d327dd`; tasks 105–111,113,114).**
  All ten round-15 tracks landed; the Lead's final gates on this merged tree: static ALL GREEN
  (build, CGO_ENABLED=0, GOOS=windows, vet, golangci-lint 0, gofmt, purity, import rules, menus
  check, i18n_check — catalogue heap en 62 KiB/de 81 KiB/both ≈144 KiB — and git diff --check);
  full `go test ./... -p 1 -count=1 -timeout 1200s` EXIT 0, 75 packages ok, no FAIL line (log
  `/tmp/gvim-r15-final.log`). Tracks: remote project search+Git Grep via authority (Replace still
  refused; current-file/open-buffers scopes + preview open); read-only lazy remote explorer
  (write ops/.gitignore/git marks/dir watch/symlink/Windows paths open); extract-tab first slice
  (file-folder root, unsaved stash, close confirmation; co-tenant/shared buffers/terminal-tab/
  large-file/multiple open); completion auto-popup + bounded multi-buffer words (LSP trigger
  characters/MRU order/dabbrev/large-file/multi-cursor/request-id open);
  terminal Live restore on first dive (focus/backing-file/remote state open); `chrome_focus_changed`
  + `key_release` + AltGr/CapsLock/Shift normalization (GPM/kitty/legacy release/reflow open);
  i18n remainder (en 808 keys/551 per locale/534 KiB); SSH keepalive+background retry+passphrase/
  2FA (ssh_config/agent forwarding/keyring/known_hosts/OTP open; offline tests only); remote env
  provider (`env.Remote`, local overlay never forwarded; interactive-shell/file-watch/cached/
  status indicator open); update indicator partial (R13-12).
  Heap-guard test diagnosis: `TestHeapGuardStopsADoublingString` failure was shared-process
  `HeapSys`/garbage baseline contamination; fixed test-only (single fresh child, strict field
  parsing, 4× ceiling retained; production guard unchanged). Isolated ×10 and plugin package pass.
  The 4× bound is a test contract with scheduling-tail risk (late 1 ms sample can miss one
  doubling; observed max 255 MiB for the 64 MiB limit); a value beyond it violates the test
  ceiling and may be sampling delay or a guard fault, not proof the guard never fired; the
  production guard is a sampled circuit breaker, not a strict hard limit.
  `TestDynamicRegistrationUnlocksRouting` was a fake-server synchronous-scheduling deadlock in
  test infrastructure; changed to async dispatch, 40/40 stress runs pass, no production change.
  The audit ran build/gofmt/i18n_check plus targeted tests and diff review only (no full suite);
  real-host SSH/2FA/remote search/explorer, real-tmux, Windows ConPTY, real-host OSC52 and
  multi-head/PTY workspace switching remain UNTESTED. No full-parity claim.
- **Round-16 final verification (2026-11-09, merged tree on `8882ae8`; 115/116/117/118/119/121 +**
  **Lead update commands/FileMutations/ReadRange foundations).** The Lead's final gates: static ALL
  GREEN (build, CGO_ENABLED=0, GOOS=windows, vet, golangci-lint 0, gofmt, purity, import rules,
  menus, i18n_check with catalogue heap en 58 KiB / de 81 KiB / both 140 KiB, git diff --check);
  full `go test ./... -p 1 -count=1 -timeout 1200s` EXIT 0, 75 packages ok (the Lead re-runs after
  this parity update to confirm; no FAIL line recorded). The audit ran build/gofmt/i18n_check plus
  targeted round-16 tests and diff review only, all green: remote replace (multi-file/decoy/stale/
  mtime/write-failure/cancel/trust/bounds), remote git/preview/finder-refresh, registry v2/
  co-tenant extract/close, completion trigger/live-filter/auto/multi-buffer, URL theme validation/
  cache/LKG/policy, update commands/click, FileMutations/ReadRange, i18n_check; full packages
  internal/app, internal/tui, internal/tui/palette, theme, authority.
  Tracks: 115 remote Replace (atomic writes/stale/changed skips/budgets/cancel; no cross-file
  rollback/undo residual); 116 remote git letters/palette preview/finder in-place refresh
  (residuals §R13-23); 117 advertised trigger characters/live re-filter (active dabbrev already
  existed; residual (round18) non-word/Backspace; Backspace fixed r21, non-word matches Fresh; row identity/multi-buffer dabbrev); 118 registry
  v2 co-tenant + same-root extract + data-safety switch-before-close ordering; 119 URL themes
  (cache/policy/LKG; packaged repo form refused); 121 i18n picker/context-menu/browser/help
  chrome (en 853 / 563 per locale / 541 KiB text / 626 KiB JSON / heap 58-81-140 KiB); Lead update
  commands (Check/Show/clickable, palette 240); FileMutations Mkdir/Rename/Remove foundation
  (R13-21 not wired) and ReadRange foundation (R13-22 remote large-file not done); 120 large-file
  mode and 123 explorer write UI are NOT complete and are recorded partial.
  Heap-guard test diagnosis remains as recorded: shared-process HeapSys/garbage contamination,
  test-only fresh-child/strict/4× fix, production guard a sampled circuit breaker; 4× scheduling-
  tail risk and "peak cannot prove never fired" wording retained. LSP manager fake-server async
  dispatch fix 40/40 remains. Real-host SSH/search/explorer/keepalive/2FA, real-tmux, Windows
  ConPTY, real-host OSC52 and multi-head/PTY workspace switching remain UNTESTED. No full-parity
  claim.
- **Round-17 final verification (2026-11-09, merged tree on `20bf31f`; 124–129,131).** The Lead's
  final gates: static ALL GREEN (build, CGO_ENABLED=0, GOOS=windows, vet, golangci-lint 0, gofmt,
  purity, import rules, menus, i18n_check with catalogue heap en 103 KiB / de 81 KiB / both
  185 KiB, git diff --check); full `go test ./... -p 1 -count=1 -timeout 1200s` EXIT 0, 76
  packages ok (the Lead re-runs after this parity update; no FAIL line recorded). The audit ran
  build/gofmt/i18n_check plus targeted round-17 tests and diff review only, all green: remote
  explorer ops/UI, daemon remote jump box/spec, per-workspace plugin state, remote large-file
  windowed document/buffer/app, SSH config/ProxyJump/agent forwarding, styled screen snapshot/dock
  modes, i18n check; packages internal/app, internal/tui, internal/tui/dock, internal/cli,
  internal/daemon, document, authority, theme.
  Tracks: 124 explorer New File/New Directory/Rename/Delete (trust/disconnected/permanent delete/
  refresh; cut/copy/paste/drag refused; no undo/check-to-write race residual), 125 daemon-hosted
  remote authority/jump box (registry authority_spec, start prompts, attach ignores target, restart
  identity, lazy dormant; remote session restore/plugin OS/trust/ownership residual), 126
  per-workspace plugin state (sessions/<id>/plugin-state.json v1, 1 MiB/plugin + 8 MiB file caps,
  atomic flush, newer-version read-only; shared runtime/chrome task 132 NOT done), 127 remote
  large-file windowed mode (read-only at round 17; superseded by round 18 editable/bounded save — ErrSourcePending seam, 1 MiB×8 LRU, 25 ms demand loads, 4 KiB
  head; save/edit later), 128 SSH config/ProxyJump/agent forwarding (bounded parser, one-hop jump,
  agent forwarding; known_hosts accept-new/OTP/keyring/real host residual), 129 i18n remainder 2
  (en 909 / 563 / 545 KiB text / 631 KiB JSON / heap 103-81-185 KiB; docs/i18n boundary stale),
  131 styled screen snapshot + dock modes (visible screen only; no backing bytes/cursor/alt-screen/
  OSC/hyperlinks; revive needs dive; remote no state).
  Heap-guard test diagnosis remains: shared-process HeapSys/garbage contamination, test-only fresh-
  child/strict/4× fix, production sampled circuit breaker; 4× scheduling-tail risk and "peak cannot
  prove never fired" retained. LSP manager fake-server async dispatch fix 40/40 remains. Real-host
  SSH/jump-box/agent-forwarding/search/explorer/replace/large-file, real-tmux, Windows ConPTY,
  real-host OSC52 and multi-head/PTY workspace switching remain UNTESTED. No full-parity claim.
- **Round-18 final verification (2026-11-09, merged tree on `03bdf29`; 133–139, 132 merged into**
  **133).** The Lead's final gates: static ALL GREEN (clean GOCACHE: build, CGO_ENABLED=0,
  GOOS=windows, vet, golangci-lint 0, gofmt, purity, import rules, menus, i18n_check with
  catalogue heap en 105 KiB / de 82 KiB / both 187 KiB); full
  `go test ./... -p 1 -count=1 -timeout 1800s` EXIT 0, 76 packages, log
  `/tmp/gvim-r18-final.log`. The audit ran build/gofmt/i18n_check plus targeted round-18 tests and
  diff review only (no full suite), all green: plugin_shared/shared runtime, remote large-file
  editable overlay/save, remote session restore, remote ignore/watch, extract variants, i18n check,
  remote search scopes/preview; packages internal/app, internal/tui, internal/pluginhost,
  internal/daemon, document, authority.
  Tracks: 133 shared daemon plugin runtime + host/OS mux (LOCAL daemon workspaces; editor-wide
  registration carry; state namespace switch; remote/authority-spec excluded; full Fresh window-
  scope/focus announcer residual), 134 remote large-file editable overlay + bounded 64 MiB
  merged atomic save (Fresh host-recipe divergence; >64 MiB residual), 135 remote daemon session
  restore across restart (keepSavedSession; conflict-detection/plugin-FS/trust/ownership residual),
  136 remote .gitignore + polled directory watch as gvim extension (latency/rename/ignore-refresh
  residuals), 137 extract variants as gvim extensions (Fresh only extract-to-new-workspace;
  terminal tabs not in multi-select etc.), 138 i18n remainder 3 + docs boundary refresh (en 944/
  569 / 551 KiB / 638 KiB / heap 105-82-187), 139 remote search current-file/open-buffers scopes +
  preview (plain/head-only >256 KiB residual).
  Heap-guard test diagnosis remains: shared-process HeapSys/garbage contamination, test-only fresh-
  child/strict/4× fix, production sampled circuit breaker; 4× scheduling-tail risk and "peak cannot
  prove never fired" retained. LSP manager fake-server async dispatch fix 40/40 remains. Real-host
  SSH/jump-box/agent-forwarding/search/explorer/replace/large-file save, real-tmux, Windows
  ConPTY, real-host OSC52 and multi-head/PTY workspace switching remain UNTESTED. No full-parity
  claim.
- **Round-19 final verification (2026-11-09, merged tree on `f691d44`; 141–146, 148).** The
  Lead's final gates: static ALL GREEN (clean GOCACHE: build, CGO_ENABLED=0, GOOS=windows, vet,
  golangci-lint 0, gofmt, purity, import rules, menus, i18n_check with catalogue heap en 112 KiB /
  de 82 KiB / both 195 KiB); full `go test ./... -p 1 -count=1 -timeout 1800s` EXIT 0, 76 packages,
  log `/tmp/gvim-r19-final.log`. The audit ran build/gofmt/i18n_check plus targeted round-19 tests
  and diff review only (no full suite), all green: plugin window-scope/focus announcer, plugin
  authority OS, remote hot-exit/trust/reconnect, per-workspace project plugin config/waits, i18n
  check, multi-client/auto-attach, remote explorer clipboard
  (packages internal/tui, internal/pluginhost, internal/app, internal/daemon, internal/cli, trust,
  authority).
  Tracks: 141 full per-mount chrome scope editor/window/buffer + focus announcer (window/buffer
  ids per-workspace not globally stable residual), 142 plugin fs/proc through workspace authority
  (local-only LookPath/Dirs/Getenv/Now, unsupported/errors, polling watch notice, real SSH
  untested), 143 remote hot-exit staged locally + authority recovery, remote trust record keyed
  target, reconnect cache refresh/one notice (local-staging divergence/marker scan/real host
  residuals), 144 per-workspace project plugin config (only changed plugins reload; project layer
  never writes user toggles) + cross-workspace waits stay/resume/cancel (hard-kill debounce
  residual), 145 i18n remainder 4 (en 972/569 / 553 KiB / ≈640 KiB / heap 112-82-195), 146 daemon
  multi-client + auto-attach (multi-client broadcast/any input/smallest size/detach one/slow-
  client bounded detach/incompatible version rejected; auto_attach default OFF vs Fresh on, named-
  daemon nuances residual), 148 remote explorer clipboard (cut/copy/paste/duplicate over authority,
  bounds/atomic/binary/collision refusal/partial reporting; no drag/undo/transaction/mode-
  preservation/existence race residual).
  Heap-guard test diagnosis remains: shared-process HeapSys/garbage contamination, test-only fresh-
  child/strict/4× fix, production sampled circuit breaker; 4× scheduling-tail risk and "peak cannot
  prove never fired" retained. LSP manager fake-server async dispatch fix 40/40 remains. Real-host
  SSH/jump-box/agent-forwarding/search/explorer/replace/clipboard/large-file save, real-tmux,
  Windows ConPTY, real-host OSC52 and multi-head/PTY workspace switching remain UNTESTED. No full-
  parity claim.
- **Round-20 final verification (2026-11-09, merged tree on `7b33521`; 149–154, 156).** Static gates
  ALL GREEN (Lead, clean GOCACHE: build, CGO_ENABLED=0, GOOS=windows, vet, golangci-lint 0, gofmt,
  purity, import rules, menus, i18n_check with catalogue heap en 114 KiB / de 82 KiB / both
  197 KiB). Final full `go test ./... -p 1 -count=1 -timeout 1800s` EXIT 0: 76 packages ok, no FAIL
  line (log `/tmp/gvim-r20-fixed.log`). The audit ran build/gofmt/i18n_check plus targeted
  round-20 checks and diff review only (no full suite); the Lead ran the full suite.
  The earlier failures had TWO root causes, both fixed with deterministic regressions: (a) the
  daemon called Program.Send while holding a lock, forming a three-way deadlock with the renderer;
  Program.Send was moved outside the lock; (b) the test helper waiting for pong discarded output
  frames; retained frames were added. Earlier hypotheses are not repeated as the full root cause.
  Tracks: 149 editor-global plugin window/buffer ids (seq<<40|local, per-mux sequence,
  CodeClosed foreign/released, global returns/events, ReleaseWorkspace; 40-bit cap/editor overlays/
  widget clone/buffer mount residuals); 150 streamed remote large-file save (WriteStreamer mktemp/
  chunks/mv, arbitrary size bounded memory, failure target untouched, changed remote refused,
  fallback 64 MiB; Fresh host patched recipe/non-windowed cap/syntax/real-host residuals); 151
  cross-workspace shared buffers (daemon LOCAL owner+mirrors, ownership focus/switch, version-
  guarded sync/full reload, close/evict promotion; OpenLoaded/startup, owner-switch request, tab-
  close promotion, large/virtual/>8MiB/remote, mirror cursor, plugin seam doc-id residuals); 152
  materialized-workspace cap+LRU eviction (user-only daemon.materialized_workspaces 8/1..64,
  capture-first/fallback, active never, overcap on double failure, remote excluded, notice); 153
  SSH known_hosts policy/algos/keyring interface (strict/accept-new/off-refused, changed key always
  refused, ordered safe defaults, keyring default off; keyring backends no-op/2FA not stored/
  signature detail/real-host residuals); 154 i18n remainder 5 (en 998/575, 560 KiB text under 640
  cap, ≈649 KiB JSON, heap 114-82-197); 156 live-restore styled history/cursor/alt remainder
  (200 rows/512 KiB, exact-match or ignored whole; deep history text-only, OSC/hyperlinks/alt
  replay/cursor-shape/dive/remote residuals).
  Heap-guard test diagnosis remains: shared-process HeapSys/garbage contamination, test-only fresh-
  child/strict/4× fix, production sampled circuit breaker; 4× scheduling-tail risk and "peak cannot
  prove never fired" retained. LSP manager fake-server async dispatch fix 40/40 remains. Real-host
  SSH/jump-box/agent-forwarding/search/explorer/replace/clipboard/streaming large-file save, real-
  tmux, Windows ConPTY, real-host OSC52, multi-head/PTY workspace switching remain UNTESTED.
  No full-parity claim.
- **Round-21 final verification (2026-11-09, merged tree on `8406f7e`; 157–160).** Static gates ALL
  GREEN (Lead: build, CGO_ENABLED=0, GOOS=windows, vet, golangci-lint 0, purity,
  import rules, menus, i18n_check with catalogue heap en 109 KiB / de 82 KiB / both 192 KiB,
  git diff --check). Final full `go test ./... -p 1 -count=1 -timeout 1200s` EXIT 0: 76 packages ok,
  no FAIL line (log `/tmp/gvim-r21-tests.log`). The audit ran build/gofmt/i18n_check plus targeted
  round-21 checks/diff review only (no full suite); the Lead ran the full suite.
  Tracks: 157 exclusive create (authority.Creators/CreateNew local O_EXCL, SSH set -C noclobber,
  ErrExist without follow/truncate, chmod failure never deletes, no-surface refusal; remote explorer
  New File exclusive; rename/copy destination-exists refusal is only at check time, so a concurrent create can be overwritten (check-to-write race remains)); 158 shared-buffer close rule
  (mirror-tab no prompt detach; owner-tab with mirror promotes canonical doc preserving unsaved+
  undo; final-view prompt; CloseByPath detaches victims; editor-level Close/OpenLoaded/startup/no-TUI-
  split/mirror-cursor/plugin-doc-id residuals); 159 remote plugin walk/copy over authority (depth-
  first lexical, symlinks reported not followed, unreadable skipped, cancellation; copy 16 MiB/file
  + entry caps, tree symlinks refused; realpath (r22, tool/POSIX dependency) landed; metadata not preserved, partial
  destination on failed tree copy; local-only dirs/env.get/lookpath/clock; polling watch notice); 160
  completion Backspace re-filter (complete stored pool, shorter prefix, no server request, synthetic
  range recomputed, selected identity preserved, typing/backspace empty behavior, post-edit version,
  apple/atom regression; non-word close+pass-through matches Fresh popup.rs/e2e, so not a gap).
  Heap-guard test diagnosis remains: shared-process HeapSys/garbage contamination, test-only fresh-
  child/strict/4× fix, production sampled circuit breaker; 4× scheduling-tail risk and "peak cannot
  prove never fired" retained. LSP manager fake-server async dispatch fix 40/40 remains. Real-host
  SSH/jump-box/agent-forwarding/search/explorer/replace/clipboard/streaming large-file save/plugin
  walk-copy, real-tmux, Windows ConPTY, real-host OSC52, multi-head/PTY workspace switching remain
  UNTESTED. No full-parity claim.
- **Round-22 final verification (2026-11-09, merged tree on `04631b8`; 162–164 landed, 165 NOT**
  **landed, 166 audit).** Static gates PASS (Lead: build, CGO_ENABLED=0, GOOS=windows, vet,
  golangci-lint 0, purity, import rules, menus, i18n_check with catalogue heap en 114 KiB / de 82
  KiB / both 197 KiB, git diff --check). Final full `go test ./... -p 1 -count=1 -timeout 1200s`
  EXIT 0: 76 packages ok, no FAIL line (log `/tmp/gvim-r22-tests.log`). The audit ran build/gofmt/
  i18n_check plus targeted round-22 tests/diff review only; the Lead ran the full suite.
  Tracks: 162 remote plugin realpath through authority RealPather (SSH bounded realpath/readlink -f,
  tool available/POSIX-remote dependency with clear io when missing, absolute answer, explicit
  existence check for missing final/broken symlink, LF/CR/whitespace byte-exact, loop/permission
  io; gvim extension; real host UNTESTED); 163 non-word/punctuation Fresh behavior verified+tests
  (filter alnum/underscore only; other printable closes+passes through; advertised trigger with auto_
  show+suggest_on_trigger starts a fresh request/list, no carried identity; off starts none; not a
  gap); 164 shared OpenLoaded adoption (unmodified restored copy mirror, differing unsaved restored
  copy separate untitled [shared conflict] tab with path removed/text preserved for Save As, normal
  tab mirrors owner; adoption deferred to focus because stash applies after open; daemon stale-stash
  test; plugin/API-created tab/daemon open-file owner-switch/editor-level Close/splits/view/mirror
  doc-id residuals); 165 NOT landed: safe no-overwrite rename cannot be implemented without
  regression, original rename kept, rename/copy check-to-write race OPEN (explorer copy/paste/rename/
  duplicate and plugin copy no-overwrite only at check time; only New File exclusive create race-
  free).
  Heap-guard semantics remain as recorded: shared-process HeapSys/garbage test contamination, test-
  only fresh-child/strict/4× fix, production sampled circuit breaker (not hard limit); beyond 4× is
  a test-ceiling violation, not proof the guard never fired. LSP manager fake-server async dispatch
  fix 40/40 remains. Real-host SSH/jump-box/agent-forwarding/search/explorer/replace/clipboard/
  streaming large-file save/plugin realpath-walk-copy, real-tmux, Windows ConPTY, real-host OSC52,
  multi-head/PTY workspace switching remain UNTESTED. No full-parity claim.
- **Round-23 final verification (2026-11-09, merged tree on `615960c`; 167–170 landed, 171**
  **audit).** Static gates PASS (Lead, default GOCACHE: build, CGO_ENABLED=0, GOOS=windows, vet,
  golangci-lint 0, purity, import rules, menus, i18n_check with catalogue heap en 114 KiB / de 82
  KiB / both 197 KiB, git diff --check). Final full `go test ./... -p 1 -count=1 -timeout 1200s`
  EXIT 0: 76 packages ok, no FAIL line (log `/tmp/gvim-r23-tests.log`). The audit ran build/gofmt/
  i18n_check plus targeted round-23 tests/diff review only; the Lead ran the full suite.
  Tracks: 167 Fresh labelDetails verified (detail/description rendered; documentation intentionally
  not shown by Fresh; tests/docs corrected, not gaps); 168 multi-buffer dabbrev (active nearest-
  first, other tabs tab-order, bounded 64 KiB/8 tabs/30 candidates, cycle/restore/undo/smart-case/
  dedupe; tab order not MRU residual); 169 remote search ignore scope (grep fallback git ls-files
  --cached --others --exclude-standard, tracked kept even if gitignored, untracked ignored/hidden
  dropped, nested repo by prefix, outside worktree hidden-only, degraded listing hidden-only without
  notice; rg tracked-ignored/result cap+Limit/nested inner ignore/finder backend walk residuals);
  170 plugin fs.copy streaming via authority ReadRange+WriteStream (1 MiB chunks, atomic temp/commit/
  abort, no 16 MiB cap, destination untouched until commit, cancellation between chunks; no-stream
  backend keeps 16 MiB fallback; destination precheck race/no-overwrite guarantee absent, source
  growth beyond initial size/metadata/partial tree residuals; real host UNTESTED).
  Heap-guard semantics remain as recorded: shared-process HeapSys/garbage test contamination, test-
  only fresh-child/strict/4× fix, production sampled circuit breaker (not a hard limit); beyond 4× is
  a test-ceiling violation, not proof the guard never fired. LSP manager fake-server async dispatch
  fix 40/40 remains. Real-host SSH/jump-box/agent-forwarding/search/explorer/replace/clipboard/
  streaming large-file save/plugin realpath-walk-copy, real-tmux, Windows ConPTY, real-host OSC52,
  multi-head/PTY workspace switching remain UNTESTED. No full-parity claim.
- **Round-24 verification (2026-11-09, merged tree on `ae39c36`; 172–175,177 landed; 176 audit).**
  STATIC GATES PASS (Lead, default GOCACHE: build, CGO_ENABLED=0, GOOS=windows, vet, golangci-lint
  0, purity, import rules, menus, i18n_check with catalogue heap en 114 KiB / de 82 KiB / both 197
  KiB, git diff --check). Final full `go test ./... -p 1 -count=1 -timeout 1200s` EXIT 0: 76
  packages ok, no FAIL line (log `/tmp/gvim-r24-tests.log`). The audit ran build/gofmt/i18n_check
  plus targeted round-24 tests/diff review only; the Lead ran the full suite. Tracks: 172 completion/dabbrev MRU (RecentTabIndices active-first, document
  identity; close/move preserved; fallback tab order); 173 both remote tools share Fresh candidate
  scope (rg --no-ignore --hidden -g !.git + candidate filter; tracked-ignored searchable on both;
  grep ordinary+filter; outside worktree rg defaults/grep hidden-only; failed/truncated hidden-only
  no notice; cap/Limit before filter; nested inner ignore; finder snapshot authoritative); 174 copy
  tree root atomically claimed via DirectoryCreator (concurrent dir conflict; no-capability backend old
  check-then-mkdir -p residual; failure after claim partial destination; tree-root-only guarantee,
  descendants per-file atomic replace; 16 MiB no-stream fallback; metadata/growth residuals); 175
  Save As conflict/mirror/new-path/owner-rekey fixes + conflict label persistence, plan-to-write race
  and dormant-owner residuals; 177 dashboard upstream tracking (weather has since landed opt-in r30; other GitHub/network sections stay OPEN).
  Heap-guard semantics remain as recorded: shared-process HeapSys/garbage test contamination, test-
  only fresh-child/strict/4× fix, production sampled circuit breaker (not hard limit); beyond 4× is
  a test-ceiling violation, not proof the guard never fired. LSP manager fake-server async dispatch
  fix 40/40 remains. Real-host checks remain UNTESTED. No full-parity claim.
- **Round-25 verification (2026-11-09, merged tree on `355e7fc`; 178–181,183 landed; 182 audit).**
  STATIC GATES PASS (Lead, default GOCACHE: build, CGO_ENABLED=0, GOOS=windows, vet, golangci-lint
  0, purity, import rules, menus, i18n_check with catalogue heap en 109 KiB / de 82 KiB / both 192
  KiB, git diff --check). Final full `go test ./... -p 1 -count=1 -timeout 1200s` EXIT 0: 76 packages ok, no FAIL line
  (log `/tmp/gvim-r25-tests.log`). The audit ran build/gofmt/i18n_check plus targeted round-25
  tests/diff review only. Tracks: 178 shared Save As reservation (plan→write→apply, dropped-plan/
  non-gvim-writer/disk-exists/dormant-owner residuals); 179 dashboard action line targets owned by
  own plugin (branch-log row only when git_log registered; URL plain/not clickable; weather/GitHub/
  network/etc OPEN); 180 remote search explicit candidate batches (400 paths/48 KiB/64 commands,
  no ignored-tree scan; widened fallback + non-fatal notices for nested/too-large/unavailable
  listing; match limit after merge; no local fallback); 181 terminal exit status + marker
  (exit_code/exit_known, transcript marker after frozen screen, title suffix, dedupe; no status-bar
  restart indicator/program-name inference); 183 MRU handoff rank inherited on shared document
  identity replacement.
  Heap-guard semantics remain as recorded: shared-process HeapSys/garbage test contamination, test-
  only fresh-child/strict/4× fix, production sampled circuit breaker (not hard limit); beyond 4× is
  a test-ceiling violation, not proof the guard never fired. LSP manager fake-server async dispatch
  fix 40/40 remains. Real-host checks remain UNTESTED. No full-parity claim.
- **Round-26 verification (2026-11-09, merged tree on `507e6f4`; 184–188 landed; 189 audit).**
  STATIC GATES PASS (Lead, default GOCACHE: build, CGO_ENABLED=0, GOOS=windows, vet, golangci-lint
  0, purity, import rules, menus, i18n_check with catalogue heap en 114 KiB / de 87 KiB / both 202
  KiB, git diff --check). Final full `go test ./... -p 1 -count=1 -timeout 1200s` EXIT 0: 76
  packages ok, no FAIL line (log `/tmp/gvim-r26-tests.log`). The audit ran build/gofmt/i18n_check
  plus targeted round-26 tests/diff review only; the Lead ran the full suite. Tracks: 184
  gvim.url.open (http(s) whitelist, confirmation, argv-only opener, unsafe refused) + dashboard
  clickable repo/branch/keyboard rows; 185 status-bar terminal restart indicator (Resume/Restart
  program, exit N only nonzero, signal, hover, restored state, keyboard equivalent); 186 remote
  preview syntax highlighting (shared bounded highlighter/cache, head-only >256 KiB residual); 187
  asm_lsp reduced Lua port (23 plugins/9 on/49 commands); 188 multi-cursor completion plain accept
  (atomic edit/version guard; snippet/import multi-caret refused).
  Heap-guard semantics remain as recorded: shared-process HeapSys/garbage test contamination, test-
  only fresh-child/strict/4× fix, production sampled circuit breaker (not hard limit); beyond 4× is
  a test-ceiling violation, not proof the guard never fired. LSP manager fake-server async dispatch
  fix 40/40 remains. Real-host checks remain UNTESTED. No full-parity claim.
- **Round-27 verification (2026-11-09, merged tree on `77c3ec7`; 190–195,197,198 landed; 196
  audit).** STATIC GATES PASS (Lead, default GOCACHE: build, CGO_ENABLED=0, GOOS=windows, vet,
  golangci-lint 0, purity, import rules, menus, i18n_check with catalogue heap en 116 KiB / de 82
  KiB / both 198 KiB, git diff --check). Final full `go test ./... -p 1 -count=1 -timeout 1200s`
  EXIT 0: 76 packages ok, no FAIL line (log `/tmp/gvim-r27-tests.log`). The audit ran build/gofmt/
  i18n_check plus targeted round-27 tests/diff review only; the Lead ran the full suite. Tracks: 190
  csharp_support reduced Lua port (24 plugins/9 on/15 off/51 commands; nearest-project restore +
  csharp-ls help); 191 multi-cursor snippet/auto-import accept (per-caret atomic edit/version guard;
  resolve-based extra-caret still refused); 192 remote paste conflict Overwrite/Rename/Cancel (no All
  variants; overwrite remove-then-copy race); 193 mixed file+terminal extract (co-tenant-only,
  conflict prompt, no PTY handover); 194 real SSH keychain backends (macOS security/Linux
  secret-tool, opt-in, fake-runner tests, real host UNTESTED); 195 user-layer update_command in a
  retained dock terminal; 197 exclusive asm_lsp config create (residuals recorded); 198 i18n
  remainder 6 (en1015/de579, 564 KiB text, ≈654 KiB JSON, heap 116-82-198).
  Heap-guard semantics remain as recorded: shared-process HeapSys/garbage test contamination, test-
  only fresh-child/strict/4× fix, production sampled circuit breaker (not hard limit); beyond 4× is
  a test-ceiling violation, not proof the guard never fired. LSP manager fake-server async dispatch
  fix 40/40 remains. Real-host checks remain UNTESTED. No full-parity claim.
- **Round-28 verification (2026-11-09, merged tree on `66f8f69`; 199–204 landed; 205 audit).**
  STATIC GATES PASS (Lead, default GOCACHE: build, CGO_ENABLED=0, GOOS=windows, vet, golangci-lint
  0, purity, import rules, menus, i18n_check with catalogue heap en 110 KiB / de 82 KiB / both 193
  KiB, git diff --check). Final full `go test ./... -p 1 -count=1 -timeout 1200s` EXIT 0: 76
  packages ok, no FAIL line (log `/tmp/gvim-r28-tests.log`). The first full run exposed a test-session
  cleanup race (TempDir cleanup vs background config/state/dock/plugin stop) in
  TestUserPluginWorksEndToEnd; the test lifecycle helper now closes the Model first and waits for
  saves/dock/plugin stop (targeted count=50 + app count=2 PASS), then the full suite EXIT 0. This is
  test infrastructure, not a product parity change. Tracks: 199 restricted plugin HTTPS GET + GitHub
  PR section; 200 remote interactive-login env capture + fallback notice; 201 remote finder single
  git-listing candidate index + recursive fallback; 202 OSC0/2 title persistence + alt-screen content
  restore; 203 child-side kitty keyboard protocol (presses only, bounded per-screen flag stacks); 204
  clangd_support reduced port (25 plugins/9 on/16 off/54 commands).
  Heap-guard semantics remain as recorded: shared-process HeapSys/garbage test contamination, test-
  only fresh-child/strict/4× fix, production sampled circuit breaker (not hard limit); beyond 4× is
  a test-ceiling violation, not proof the guard never fired. LSP manager fake-server async dispatch
  fix 40/40 remains. Real-host checks remain UNTESTED. No full-parity claim.
- **Round-29 verification (2026-11-09, merged tree on `a6b49f2`; 206–210,212 landed; 211 audit).**
  STATIC GATES PASS (Lead, default GOCACHE: build, CGO_ENABLED=0, GOOS=windows, vet, golangci-lint
  0, purity, import rules, menus, i18n_check with catalogue heap en 115 KiB / de 82 KiB / both 198
  KiB, git diff --check). Final full `go test ./... -p 1 -count=1 -timeout 1200s` EXIT 0: 76
  packages ok, no FAIL line (log `/tmp/gvim-r29-tests.log`). The audit ran build/gofmt/i18n_check
  plus targeted round-29 tests/diff review only; the Lead ran the full suite. Tracks: 206 kitty
  modifier/functional/push-pop fixes (real PTY); 207 post-reconnect remote shell re-probe/fallback
  refresh; 208 dashboard GitHub PR safe clickable/keyboard canonical URL; 209 remote finder git
  degradation notice; 210 plugin HTTPS IPv6-transition SSRF fix; 212 terminal title combination
  using recorded launch (not live foreground).
  Heap-guard semantics remain as recorded: shared-process HeapSys/garbage test contamination, test-
  only fresh-child/strict/4× fix, production sampled circuit breaker (not hard limit); beyond 4× is
  a test-ceiling violation, not proof the guard never fired. LSP manager fake-server async dispatch
  fix 40/40 remains. Real-host checks remain UNTESTED. No full-parity claim.
- **Round-30 verification (2026-11-09, merged tree on `0712aa9`; 213–217 landed; 218 audit).**
  STATIC GATES PASS (Lead, default GOCACHE: build, CGO_ENABLED=0, GOOS=windows, vet, golangci-lint
  0, purity, import rules, menus, i18n_check with catalogue heap en 110 KiB / de 82 KiB / both 193
  KiB, git diff --check). Final full `go test ./... -p 1 -count=1 -timeout 1200s` EXIT 0: 76
  packages ok, no FAIL line (log `/tmp/gvim-r30-tests.log`). The audit ran build/gofmt/i18n_check
  plus targeted round-30 tests/diff review only; the Lead ran the full suite. Tracks: 213 dashboard
  weather opt-in via restricted HTTPS; 214 terminal exit marker at exit instant (pager-visible,
  exactly-once/old-session compatible); 215 remote env-input file watch invalidation/re-warm +
  degradation notice; 216 resolve multi-cursor snapshot validation/atomic apply; 217 remote batch-paste
  Overwrite All/Skip/Skip All/Cancel (incremental execution divergence recorded).
  Heap-guard semantics remain as recorded: shared-process HeapSys/garbage test contamination, test-
  only fresh-child/strict/4× fix, production sampled circuit breaker (not hard limit); beyond 4× is
  a test-ceiling violation, not proof the guard never fired. LSP manager fake-server async dispatch
  fix 40/40 remains. Real-host checks remain UNTESTED. No full-parity claim.
- **Round-31 verification (2026-11-09, merged tree on `09ca1d2`; 219–223 landed; 224 audit).**
  STATIC GATES PASS (Lead, default GOCACHE: build, CGO_ENABLED=0, GOOS=windows, vet, golangci-lint
  0, purity, import rules, menus, i18n_check with catalogue heap en 117 KiB / de 82 KiB / both 199
  KiB, git diff --check). Final full `go test ./... -p 1 -count=1 -timeout 1200s` EXIT 0: 76
  packages ok, no FAIL line (log `/tmp/gvim-r31-tests.log`). The audit ran build/gofmt/i18n_check
  plus targeted round-31 tests/diff review only; the Lead ran the full suite. Tracks: 219 welcome demo
  port; 220 remote browser drag/move (copy-then-remove after confirmation); 221 trusted remote env
  marker probe (Require paths); 222 MRU session persistence; 223 i18n extraction incl dashboard
  plugin i18n.
  Heap-guard semantics remain as recorded: shared-process HeapSys/garbage test contamination, test-
  only fresh-child/strict/4× fix, production sampled circuit breaker (not hard limit); beyond 4× is
  a test-ceiling violation, not proof the guard never fired. LSP manager fake-server async dispatch
  fix 40/40 remains. Real-host checks remain UNTESTED. No full-parity claim.
- **Round-32 verification (2026-11-09, merged tree on `9c1e936`; 225–229 landed; 230 audit).**
  STATIC GATES PASS (Lead, default GOCACHE: build, CGO_ENABLED=0, GOOS=windows, vet, golangci-lint
  0, purity, import rules, menus, i18n_check with catalogue heap en 119 KiB / de 82 KiB / both 201
  KiB, git diff --check). Final full `go test ./... -p 1 -count=1 -timeout 1200s` EXIT 0: 76
  packages ok, no FAIL line (log `/tmp/gvim-r32-final.log`). This is the post-safety-rollback run.
  The audit ran build/gofmt/i18n_check plus targeted round-32 tests/diff review only; the Lead ran
  the full suite. Tracks: 225 OSC 7 cwd persistence/restore + authority Stat validation; 226 remote
  delete stays PERMANENT (trash/undo audited and WITHDRAWN, not shipped) + copy mode/mtime best
  effort; 227 pre-trust env marker detection via authority Stat (Require paths); 228 remote large-
  window highlight window-generation invalidation; 229 i18n extraction.
  Heap-guard semantics remain as recorded: shared-process HeapSys/garbage test contamination, test-
  only fresh-child/strict/4× fix, production sampled circuit breaker (not hard limit); beyond 4× is
  a test-ceiling violation, not proof the guard never fired. LSP manager fake-server async dispatch
  fix 40/40 remains. Real-host checks remain UNTESTED. No full-parity claim.
- **Round-33 verification (2026-11-09, merged tree on `8bbe790`; 231–234 landed; 235 read-only**
  **research; 236 audit).** STATIC GATES PASS (Lead, default GOCACHE: build, CGO_ENABLED=0,
  GOOS=windows, vet, golangci-lint 0, purity, import rules, menus, i18n_check with catalogue heap
  en 118 KiB / de 82 KiB / both 201 KiB, git diff --check). Final full
  `go test ./... -p 1 -count=1 -timeout 1200s` EXIT 0: 76 packages ok, no FAIL line (log
  `/tmp/gvim-r33-final.log`). The audit ran build/gofmt/i18n_check plus targeted round-33 tests/diff
  review only; the Lead ran the full suite. Tracks: 231 OSC7 Windows absolute paths + start-platform
  validation; 232 bounded loaded-window edit context for remote large files; 233 custom marker/require
  authority watch; 234 crash-recovery/extraction MRU (item 199); 235 trash capability research only
  (NOT implementable, no code/docs change, no trash/undo).
  Heap-guard semantics remain as recorded: shared-process HeapSys/garbage test contamination, test-
  only fresh-child/strict/4× fix, production sampled circuit breaker (not hard limit); beyond 4× is
  a test-ceiling violation, not proof the guard never fired. LSP manager fake-server async dispatch
  fix 40/40 remains. Real-host checks remain UNTESTED. No full-parity claim.
- **Round-34 verification (2026-11-09, merged tree on `564e2a4`; 237–241 landed; 242 audit).**
  STATIC GATES PASS (Lead, default GOCACHE: build, CGO_ENABLED=0, GOOS=windows, vet, golangci-lint
  0, purity, import rules, menus, i18n_check with catalogue heap en 120 KiB / de 82 KiB / both 203
  KiB, git diff --check). Final full `go test ./... -p 1 -count=1 -timeout 1200s` EXIT 0: 76
  packages ok, no FAIL line (log `/tmp/gvim-r34-final.log`). The audit ran build/gofmt/i18n_check
  plus targeted round-34 tests/diff review only; the Lead ran the full suite. Tracks: 237 remote
  search preview syntax already present + truncation/disconnect/no-extra-read regression tests; 238
  local large-file bounded edit context; 239 runtime detector config reload watcher re-plan; 240
  terminal exit auto-opens read-only transcript; 241 env/terminal i18n extraction.
  Heap-guard semantics remain as recorded: shared-process HeapSys/garbage test contamination, test-
  only fresh-child/strict/4× fix, production sampled circuit breaker (not hard limit); beyond 4× is
  a test-ceiling violation, not proof the guard never fired. LSP manager fake-server async dispatch
  fix 40/40 remains. Real-host checks remain UNTESTED. No full-parity claim.
- **Round-35 verification (2026-11-09, merged tree on `0650fd4`; 243–247 landed; 248 audit).**
  STATIC GATES PASS (Lead, default GOCACHE: build, CGO_ENABLED=0, GOOS=windows, vet, golangci-lint
  0, purity, import rules, menus, i18n_check with catalogue heap en 119 KiB / de 88 KiB / both 207
  KiB, git diff --check). Final full `go test ./... -p 1 -count=1 -timeout 1200s` EXIT 0: 76
  packages ok, no FAIL line (log `/tmp/gvim-r35-final.log`). The audit ran build/gofmt/i18n_check
  plus targeted round-35 tests/diff review only; the Lead ran the full suite. Tracks: 243 local env
  detector hot reload DETECTION-ONLY (passive rescan; no capture/recipe invalidate/re-warm); 244
  remote search walked candidate filter; 245 embedded-terminal DECSET 1004 focus events (gvim
  extension); 246 plugin copy canonicalization + capability refusal (TOCTOU/remote-hardlink
  residuals); 247 i18n extraction.
  Heap-guard semantics remain as recorded: shared-process HeapSys/garbage test contamination, test-
  only fresh-child/strict/4× fix, production sampled circuit breaker (not hard limit); beyond 4× is
  a test-ceiling violation, not proof the guard never fired. LSP manager fake-server async dispatch
  fix 40/40 remains. Real-host checks remain UNTESTED. No full-parity claim.
- **Round-36 verification (2026-11-09, merged tree on `40598e7`; 249–253 landed; 254 audit).**
  STATIC GATES PASS (Lead, default GOCACHE: build, CGO_ENABLED=0, GOOS=windows, vet, golangci-lint
  0, purity, import rules, menus, i18n_check with catalogue heap en 124 KiB / de 82 KiB / both 207
  KiB, git diff --check). Final full `go test ./... -p 1 -count=1 -timeout 1200s` EXIT 0: 76
  packages ok, no FAIL line (log `/tmp/gvim-r36-final.log`). The audit ran build/gofmt/i18n_check
  plus targeted round-36 tests/diff review only; the Lead ran the full suite. Tracks: 249 host-window
  + dock focus merged for DECSET 1004; 250 local detector content-change invalidation DETECTION-ONLY;
  251 remote nested-repository git candidate slice (up to 8, one layer, all-or-nothing); 252 plugin
  copy root-source bypass fix + no-streamer hard-link safety; 253 palette Tab accepts the selected
  suggestion.
  Heap-guard semantics remain as recorded: shared-process HeapSys/garbage test contamination, test-
  only fresh-child/strict/4× fix, production sampled circuit breaker (not hard limit); beyond 4× is
  a test-ceiling violation, not proof the guard never fired. LSP manager fake-server async dispatch
  fix 40/40 remains. Real-host checks remain UNTESTED. No full-parity claim.
- **Round-37 verification (2026-11-09, merged tree on `c0b42a1`; 255–257 + alt-screen/local-watch**
  **fixes landed).** STATIC GATES PASS (Lead, default GOCACHE: build, CGO_ENABLED=0, GOOS=windows,
  vet, golangci-lint 0, purity, import rules, menus, i18n_check with catalogue heap en 126 KiB / de
  82 KiB / both 208 KiB, git diff --check). Final full `go test ./... -p 1 -count=1 -timeout 1200s`
  EXIT 0: 76 packages ok, no FAIL line (log `/tmp/gvim-r37-final.log`). The audit ran build/gofmt/
  i18n_check plus targeted round-37 tests/diff review only; the Lead ran the full suite. Tracks: 255
  palette Tab preserves file :line[:col] target; 256 remote plugin copy requires atomic WriteStream
  (no fallback, unsupported before any destination write); 257 nested-prefix fallback roots + output-
  cap incomplete notice; alternate-screen exit shows read-only frame+marker; local watcher scope
  notices/i18n.
  Heap-guard semantics remain as recorded: shared-process HeapSys/garbage test contamination, test-
  only fresh-child/strict/4× fix, production sampled circuit breaker (not hard limit); beyond 4× is
  a test-ceiling violation, not proof the guard never fired. LSP manager fake-server async dispatch
  fix 40/40 remains. Real-host checks remain UNTESTED. No full-parity claim.
- **Round-38 verification (2026-11-09, merged tree on `f3ab872`).** STATIC GATES PASS (Lead,
  default GOCACHE: build, CGO_ENABLED=0, GOOS=windows, vet, golangci-lint 0, purity, import rules,
  menus, i18n_check with catalogue heap en 120 KiB / de 82 KiB / both 203 KiB, git diff --check).
  Final full `go test ./... -p 1 -count=1 -timeout 1200s` EXIT 0: 76 packages ok, no FAIL line (log
  `/tmp/gvim-r38-final.log`). The audit ran build/gofmt/i18n_check plus targeted round-38 tests/diff
  review only; the Lead ran the full suite. Tracks: file multi-word matcher (already present) +
  buffer-mode fuzzy multi-word; remote AllFiles open-buffer snapshots; local watcher real fsnotify
  arm-before-refresh; terminal transcript re-search after resize; plugin no-follow local-only with
  SSH fs.copy unsupported (final safety rollback).
  Heap-guard semantics remain as recorded: shared-process HeapSys/garbage test contamination, test-
  only fresh-child/strict/4× fix, production sampled circuit breaker (not hard limit); beyond 4× is
  a test-ceiling violation, not proof the guard never fired. LSP manager fake-server async dispatch
  fix 40/40 remains. Real-host checks remain UNTESTED. No full-parity claim.
- **Round-39 verification (2026-11-09, merged tree on `bc5624b`).** STATIC GATES PASS (Lead,
  default GOCACHE: build, CGO_ENABLED=0, GOOS=windows, vet, golangci-lint 0, purity, import rules,
  menus, i18n_check with catalogue heap en 126 KiB / de 82 KiB / both 208 KiB, git diff --check).
  Final full `go test ./... -p 1 -count=1 -timeout 1200s` EXIT 0: 76 packages ok, no FAIL line (log
  `/tmp/gvim-r39-final.log`). The audit ran build/gofmt/i18n_check plus targeted round-39 tests/diff
  review only; the Lead ran the full suite. Tracks: literal-path quick-open probe; remote search
  tool-failure open-buffer fallback; local watcher closeResources; incremental terminal scrollback
  search; SSH plugin-copy link-commit research only (not implemented; SSH copy still unsupported).
  Heap-guard semantics remain as recorded: shared-process HeapSys/garbage test contamination, test-
  only fresh-child/strict/4× fix, production sampled circuit breaker (not hard limit); beyond 4× is
  a test-ceiling violation, not proof the guard never fired. LSP manager fake-server async dispatch
  fix 40/40 remains. Real-host checks remain UNTESTED. No full-parity claim.
- **Round-40 verification (2026-11-09, merged tree on `6c6f71b`).** STATIC GATES PASS (Lead,
  default GOCACHE: build, CGO_ENABLED=0, GOOS=windows, vet, golangci-lint 0, purity, import rules,
  menus, i18n_check with catalogue heap en 126 KiB / de 88 KiB / both 214 KiB, git diff --check).
  Final full `go test ./... -p 1 -count=1 -timeout 1200s` EXIT 0: 76 packages ok, no FAIL line (log
  `/tmp/gvim-r40-final.log`). Audit: build/gofmt/i18n_check plus targeted round-40 tests/diff review.
  Tracks: local bounded prefix/dir probe + remote exact-Stat fallback; remote external-snapshot notice;
  F3/Shift+F3 preview stepping; Env:Reload watcher retry supervisor; SSH new-target link capability
  (audited residuals: temp cleanup best-effort/parent TOCTOU/stale cache/capability proof/real SSH
  UNTESTED). Real-host checks remain UNTESTED. No full-parity claim.
- **Round-41 verification (2026-11-09, merged tree on `ce03f16`).** STATIC GATES PASS (Lead,
  default GOCACHE: build, CGO_ENABLED=0, GOOS=windows, vet, golangci-lint 0, purity, import rules,
  menus, i18n_check with catalogue heap en 122 KiB / de 82 KiB / both 205 KiB, git diff --check).
  Final full `go test ./... -p 1 -count=1 -timeout 1200s` EXIT 0: 76 packages ok, no FAIL line (log
  `/tmp/gvim-r41-final.log`). Audit: build/gofmt/i18n_check plus targeted round-41 tests/diff review.
  Tracks: no server-side bounded directory listing (candidate WITHDRAWN); completed-index exact probe +
  16-path cache; sidebar local foreground-process title (ps absolute-path hardening (fixed system locations residual) recorded); dashboard callback
  isolation (bundled dashboard only); statusbar i18n. Real-host checks remain UNTESTED. No full-parity
  claim.
- **Round-42 verification (2026-11-09, merged tree on `fbf725a`).** STATIC GATES PASS (Lead,
  default GOCACHE: build, CGO_ENABLED=0, GOOS=windows, vet, golangci-lint 0, purity, import rules,
  menus, i18n_check with catalogue heap en 122 KiB / de 82 KiB / both 205 KiB, git diff --check).
  Final full `go test ./... -p 1 -count=1 -timeout 1200s` EXIT 0: 76 packages ok, no FAIL line (log
  `/tmp/gvim-r42-final.log`). Audit: build/gofmt/i18n_check plus targeted round-42 tests/diff review.
  Tracks: markdown_source opt-in port; ps absolute-path hardening; window_focused/window_blurred
  plugin events; statusbar i18n. Real-host checks remain UNTESTED. No full-parity claim.
- **Round-43 verification (2026-11-09, merged tree on `8ec6038`).** STATIC GATES PASS (Lead,
  default GOCACHE: build, CGO_ENABLED=0, GOOS=windows, vet, golangci-lint 0, purity, import rules,
  menus, i18n_check with catalogue heap en 122 KiB / de 82 KiB / both 205 KiB, git diff --check).
  Final full `go test ./... -p 1 -count=1 -timeout 1200s` EXIT 0: 76 packages ok, no FAIL line (log
  `/tmp/gvim-r43-final.log`). Audit: build/gofmt/i18n_check plus targeted round-43 tests/diff review.
  Track: rust_lsp reduced opt-in port (4 commands, no keys/settings; bundled 28/9 on/19 off/63
  registration sites; default set/idle heap unchanged; unported LSP error/menu/recovery/Disable/
  Configure Mode seams; LookPath local-only in remote; real host UNTESTED). No full-parity claim.
- **Round-44 verification (2026-11-09, merged tree on `1f944ec`).** STATIC GATES PASS (Lead,
  default GOCACHE: build, CGO_ENABLED=0, GOOS=windows, vet, golangci-lint 0, purity, import rules,
  menus, i18n_check with catalogue heap en 127 KiB / de 82 KiB / both 210 KiB, git diff --check).
  Final full `go test ./... -p 1 -count=1 -timeout 1200s` EXIT 0: 76 packages ok, no FAIL line (log
  `/tmp/gvim-r44-final.log`). Audit: build/gofmt/i18n_check plus targeted round-44 tests/diff review.
  Tracks: diff_nav standalone opt-in (Fresh default-enabled difference due 8.12 MiB vs 8 MiB gate;
  git_gutter default navigation kept); deno_lsp reduced opt-in (Check/Install/Help; no automatic
  selection/baseline/active-window/LSP-menu seam; PATH local-only in remote). Real-host checks
  remain UNTESTED. No full-parity claim.
- **Round-45 verification (2026-11-09, merged tree on `d53d2cd`).** STATIC GATES PASS (Lead,
  default GOCACHE: build, CGO_ENABLED=0, GOOS=windows, vet, golangci-lint 0, purity, import rules,
  menus, i18n_check with catalogue heap en 122 KiB / de 82 KiB / both 205 KiB, git diff --check).
  Final full `go test ./... -p 1 -count=1 -timeout 1200s` EXIT 0: 76 packages ok, no FAIL line (log
  `/tmp/gvim-r45-final2.log`). Audit: build/gofmt/i18n_check plus targeted round-45 tests/diff review.
  Tracks: native Deno authority root selection + Fresh/helper-toggle difference; Rust/Deno authority
  version probes; diff_nav saved-deletion seam + real provider interop; sidebar transcript query
  history; finder-generation probe cache. Real-host checks remain UNTESTED. No full-parity claim.
- The gaps the rounds opened are at the top of §5 (open TUI work, with concrete gaps:
  R2-5, R3-3…R3-4, R4-4…R4-5, R5-2…R5-6, R6-1…R6-3, R7-4…R7-5, R8-1…R8-4, R9-1…R9-5,
  R10-1…R10-4, R11-2…R11-4, R12-1…R12-5, R13-1…R13-24; #7/#8/#11/#15, R1-1, R1-2, R1-3,
  R2-4, R5-1, R5-5, R6-5, R6-6, R7-1/R7-2/R7-3, R8-5, R9-3/R9-4, R11-1, R13-10 are closed;
  R10-3/R10-4/R11-2…R11-4/R12-2…R12-5/R13-1…R13-24 partially landed with residual lists
  (round-45 landed native Deno selection, authority version probes, diff_nav deletion seam, transcript
  history, probe generation cache; full suite EXIT 0 (76 packages, log /tmp/gvim-r45-final2.log);
  §5.5 is round-46/47 plan; TUI-only scope/genuine-equivalence §4 item 94/§8; real-host checks
  UNTESTED).
- Statuses are read evidence from the tree at the time of writing. Rounds 2–45 had their gate sets
  run. Round 45 static gates PASS (Lead, default GOCACHE: build, CGO_ENABLED=0 build, GOOS=windows
  build, vet, golangci-lint 0, gofmt, purity, import rules, menus, i18n_check with catalogue heap en
  122 KiB / de 82 KiB / both 205 KiB, git diff --check). Final full
  `go test ./... -p 1 -count=1 -timeout 1200s` EXIT 0: 76 packages ok, no FAIL line, log
  `/tmp/gvim-r45-final2.log`. The audit ran build/gofmt/i18n_check plus targeted round-45 tests and
  diff review only (no full suite); the Lead ran the full suite. Real-host verification remains
  UNTESTED rather than passed: real-SSH editing/search/explorer/replace/clipboard/streaming large-file
  save/plugin realpath-walk-copy, keychain security/secret-tool, keepalive/2FA/agent-forwarding/jump-
  box/known_hosts policy, real-tmux, real-Windows ConPTY, real-host OSC52, multi-head/PTY workspace
  switching, local macOS/BSD/Linux process polling, outer-terminal focus reporting, real Deno/Rust
  workspace authority probes; ~/… expansion against a local real `sh`, not a live host. Treat
  statuses as pointers, not proof.
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
- Round-2/3/4/5/6/7/8/9/10/11/12/13 diff reviews (this audit): no scope creep outside the
  declared tracks. Round 13's 143 files (89 modified, 54 untracked; tracked diff +8913/−935
  including the generated command/menu tables regenerated by their generators) stayed within the
  six tracks and docs; refs/ and _attic/ were untouched; no golden tests were added; no duplicate
  top-level functions were found in the changed files; `git diff --check` flagged only the
  in-progress parity.md blank line at EOF, trimmed in this refresh. Shared files were reviewed
  for lost/duplicated edits: internal/daemon workspaces/protocol/framewriter, internal/app
  workspace_host/workspace_capture/remote_editor/remote_lsp/agent_discover, internal/tui
  workspace_host/workspace_ui/workspace_tags/agent_discover/command_i18n + dock tabs/discover,
  authority/ssh_files, trust/gate, lsp/manager spawner/reconnect, document/fileaccess,
  config/settings i18n wiring and the generated tables. Round-13 findings and fixes:
  (a) the audit's two workspace races plus the close failure-propagation/retention defect were
  fixed after the audit and re-verified with targeted tests (`TestCloseAwaitsCaptureWriteBeforeExit`,
  `TestCloseFailureReportedAndFallbackSaves`, `TestCloseDuringMaterializationDoesNotResurrect`,
  `TestCloseRetainsWorkspaceWhenBothWritesFail`; all −count=2 green); the dormant-workspace
  single-writer gap remains open (R13-1);
  (b) the remote file helper requires POSIX
  `sh/head/cat/chmod/mv/mkdir/dirname/readlink/mktemp` and GNU/BSD `stat` on the host (no install,
  no Python) — an implementation constraint, with its open residuals in R13-3; (c) the spawn
  gate's residual evasions are listed in R13-4; (d) real-SSH/tmux/ConPTY are untested here;
  (e) counts verified: palette 236, bundled 22/9/13, ~7.25 MiB, plugin API 242/1478, i18n 416 en
  keys/15 locales/294,448 JSON bytes; no leftover temp harnesses; (e) all reopened TUI features
  keep concrete gaps — the scope classification is TUI-only, native equivalents are listed only
  where genuinely user-facing (with residual gaps), and non-TUI surfaces are objective boundaries
  in §8.
- Phase-2 diff review (this audit): 71 changed files (61 modified + 10 untracked; ~+5.0k/−0.3k
  including the i18n locale expansion and the new workspace/clipboard/watch/discovery files)
  stayed within the declared tracks; refs/ and _attic/ untouched; no duplicate top-level
  functions; the `WSDBG`/`TUIDBG` debug markers are gone; `git diff --check` is clean (EOF
  blank line trimmed). No scope creep; the failing timing test above is the only known red.
- The round-2 formatting nit (an ATX heading appended directly after a list item without a
  blank line) no longer reproduces in `docs/ui.md`, `docs/sidebar.md`, `docs/lsp.md`,
  `docs/settings.md` or `docs/keybindings.md` as checked for this refresh.
- The `migration/` docs, including GAP-v3's numbers, are historical (§1.4). Do not plan from
  them.

## 8. TUI objective, genuine equivalences and gvim-only additions

**Objective.** The user asked for Fresh's TUI functionality; the only expressly flexible parts are
the plugin implementation (Lua instead of TypeScript) and the visual style. Everything a user can
do in Fresh's TUI is in scope. Incomplete TUI behaviour stays OPEN with concrete gaps; size,
dependency, effort, or missing permission are never closure reasons.

**Outside the TUI objective (boundaries, not backlog gaps).** Fresh's GUI/desktop front end, the
browser `--web` UI, the WASM build, packaging/installer matrices and daemon packaging are other
front ends/distribution targets, not TUI features; they are outside this objective unless a
specific TUI feature needs one (none does today). Daemon mode itself is in scope. Telemetry and
self-update are operations/distribution behaviour, not TUI. The update-check/`{update}` status
element IS TUI-visible and therefore stays an OPEN TUI gap (it needs a version/update source and
network path; no exception was granted). The remaining platform edges (GPM console mouse,
AltGr/CapsLock/key-release, plugin focus events) are OPEN TUI input gaps.

**Genuine native equivalences.** A native gvim feature may stand in for a Fresh plugin only when it
genuinely delivers the user-facing behaviour; its residual gaps stay open. Accepted today:
compare_files' user feature (native Compare; the composite-buffer API capability remains open),
the theme editor's user feature (native editor/inspect/picker/preview; Fresh-only editor features
remain open), project search's user feature (native panel; provider/large-file completions remain
open), and markdown compose/source's user feature (native page view/dual; remaining markdown gaps
stay open). dashboard/live_grep/welcome_screen are partial equivalents at best — their missing
user features stay open; gvim's merged diff-navigation landed in git_gutter but R13-9's large-file/Compare/command-i18n residuals stay open, not equivalent to the standalone diff_nav plugin.
No other missing Fresh TUI functionality is recorded as a substitution: plugin HTTP remains HTTPS
GET-only with user network opt-in/SSRF/timeout/body/concurrency limits, no generic fetch/auth/token/
cache; dashboard GitHub PR is opt-in/≤6 rows from ≤200 with keyboard/click, canonical URL rebuilt from validated slug+number and opened through gvim.url.open confirmation; weather opt-in landed r30; third-party section API and other dashboard sections remain OPEN; remote
env interactive capture is bash/zsh-only with sh -c fallback/notice, no active remote-env status
indicator, env files unwatched; remote finder git path can fall back to recursive walk, hides tracked
hidden files, omits non-UTF-8/nested contents only via fallback/truncated listing fallback; terminal
OSC1 icon/OSC7 cwd/OSC52/hyperlinks not replayed, cursor shape not inherited, alt frame read-only/no
main history while alt active; kitty child protocol presses only (no releases/report-event-type;
modifier-only/media/F13+ not encoded); clangd_support no server-specific switchSourceHeader/fileStatus
custom notifications/floating panel/ancestor .clangd walk; csharp_support no setLspRootUri/Roslyn
needs-restore event/Disable/--version precheck/English-only; asm_lsp exclusive create local crash-
partial/remote claim-write cleanup/once-only; multi-cursor resolve-based completion/large-file/request-
id; r30 batch paste Overwrite All/Skip/Skip All/Cancel landed (incremental vs Fresh resolve-first, overwrite remove-then-copy no rollback); mixed extract
co-tenant-only/no PTY handover/default-name conflicts/no undo-drag/shared buffers; SSH keychain opt-in
fake-tested (macOS argv exposure/Linux keyring-DBus, 2FA/passphrase not stored)/real host UNTESTED;
update_command no download/install engine/no Settings row; URL http(s)-only/no non-GitHub branch URL/
opener not configurable; dashboard weather opt-in landed r30; other GitHub/network sections/section API/dock/mouse/sessions OPEN;
terminal restart indicator lacks OS foreground name/different hover; r30 marker exit-instant but frozen live screen keeps it pager-only; remote preview
head-only >256 KiB/no semantic-LSP/no remote preview buffer; shared Save As reservation in-process/
dropped-plan leak/non-gvim writers/disk exists/dormant-owner; remote search failed/truncated defaults/
widened notices/match limit after merge/nested inner ignore/finder snapshot; plugin copy descendant
no-overwrite/partial tree/no-cap backend/16 MiB fallback; remote explorer drag/move/undo/whole-subtree
transaction/mode-mtime-hardlink-xattr/watch latency/Windows; remote marker scan/hot-exit local-staging
privacy/conflict/ownership; SSH 2FA/signature detail/real host; remote env input watch landed r30; fallback/no active-env status remains; live-restore
byte-exact/OSC gaps; input GPM/kitty host; URL packaged; auto_attach nuances; plugin ports (r28
clangd_support, r27 csharp_support, r25 asm_lsp ported; remaining OPEN); universal; ConPTY; i18n
boundary are OPEN (R13-1…R13-24). Landed/no longer open: r28 plugin HTTPS/GitHub PR, remote
interactive env capture, remote finder git listing, OSC title/alt restore, kitty child protocol,
clangd_support; prior rounds. Real hosts remain UNTESTED. Full suite EXIT 0 (76 packages, log
/tmp/gvim-r28-tests.log); first-run test lifecycle race fixed (test infrastructure).

**gvim-only additions (not parity requirements).** Round-18 remote `.gitignore` filtering + polled directory watching and extract variants (into an existing workspace, terminal saved-state migration, multi-select tabs) are gvim extensions, not Fresh gaps; they are documented in §4 items 122/123 and must keep working but do not count toward Fresh parity. `gvim config show`/`config paths` (show is
trust-gated), `gvim grammar`, the in-process `uitest` harness, `cmd/gvimdebug`, the screenshot sweep,
the config/UI-tree dump commands, the gvim-only `--stdin-name` flag and the bounded-snapshot stdin
behaviour, the gvim extension keys/locale-row semantics, the inspect-only provenance capture, the
opt-in/Trusted OSC52 write, transcript text re-wrap, the workspace registry/session-directory
layout, tmux discovery/Import as an extension of the oracle, and the `gvim.*` i18n keys. They do
not count toward Fresh parity, but they are the fastest way to verify new feature work; keep them
working.
