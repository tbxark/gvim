# Bundled plugins

gvim ships thirty-seven plugins inside the binary, written in Lua like any other (their sources are under
`plugin/bundled/plugins/<name>/` in the repository; **Plugin: Manager** shows them, and
`config.json` can override them). Nine are on by default; `flash`, `vi_mode`, `live_diff`, `git_log`,
`compare_files`, `merge_conflict`, `dashboard`, `code_tour`, `lsp_navigation`, `find_references`,
`git_grep`, `git_explorer`, `env_manager`, `asm_lsp`, `csharp_support`, `clangd_support`, `welcome`, `welcome_screen`, `markdown_source`, `rust_lsp`, `deno_lsp`, `diff_nav`, `slang_lsp`, `live_sessions`, `audit_mode`, `devcontainer`, `k8s_workspace` and `pkg` are off until you turn them on:

```jsonc
{ "plugins": { "vi_mode": { "enabled": true }, "git_blame": { "enabled": false } } }
```

A plugin you put in `~/.config/gvim/plugins/` with the same name replaces a bundled one.

| Plugin | Default | Commands (palette) | Settings (`plugins.<name>.settings`) |
|---|---|---|---|
| `goto_with_selection` | on | Go to Line with Selection | none |
| `tab_actions` | on | Close Other Tabs, Close All Tabs, Close Tabs To Right, Close Tabs To Left, Move Tab To Left, Move Tab To Right | none |
| `git_gutter` | on | Git Gutter: Refresh, Next Change, Previous Change | `debounce_ms` (300, 50 to 5000, project) |
| `diff_nav` | **off** | Next Change, Previous Change | none |
| `git_statusbar` | on | none (a status bar item) | none |
| `git_find_file` | on | Git Find File, Git Find File: Reload | none |
| `git_log` | **off** | Git Log, Git Log: Current File, Git Log: Close, Git Log: Refresh | none |
| `git_blame` | on (idle until toggled) | Git: Toggle Blame, Git: Show Commit, Git: Copy Commit Hash, Git: Blame Previous Revision, Git: Blame HEAD | `debounce_ms` (150, 50 to 2000, project), `summary_width` (60, 10 to 200, project) |
| `merge_conflict` | **off** | Merge: Start Resolution, Next Conflict, Previous Conflict, Use Ours, Take Theirs, Use Both, Resolve Conflict... | none |
| `diagnostics_panel` | on | Show Diagnostics Panel, Toggle Diagnostics Panel | none |
| `path_complete` | on | Path Completion: Complete Path | none |
| `lsp_help` | on | none (event driven: an LSP server error puts an install hint on the status row, and the LSP status click opens the help popup) | none |
| `asm_lsp` | **off** | Assembly: Create .asm-lsp.toml, Assembly: Show LSP Help | none |
| `csharp_support` | **off** | C#: Restore Project, C#: Show LSP Help | none |
| `clangd_support` | **off** | Clangd: Switch Source/Header, Clangd: Project Setup, Clangd: Open Project Config | none |
| `rust_lsp` | **off** | Rust: Copy rustup Install Command, Rust: Copy brew Install Command, Rust: Check rust-analyzer, Rust: Language Server Help | none |
| `deno_lsp` | **off** | Deno: Check Project, Deno: Copy Install Command, Deno: Language Server Help | none |
| `slang_lsp` | **off** | Slang: Check slangd / slangc, Slang: Copy Install Command, Slang: Language Server Help | none |
| `welcome` | **off** | Plugin Demo: Open Help, Plugin Demo: Save File, Plugin Demo: Say Hello, Plugin Demo: Insert Time, Plugin Demo: Insert Comment | none |
| `welcome_screen` | **off** | Welcome Screen: Show, Welcome Screen: Toggle Show on Startup | `auto_open` (true) |
| `markdown_source` | **off** | none (hidden Enter/Tab/Shift+Tab key handlers for Markdown lists) | none |
| `lsp_navigation` | **off** | Go to LSP Symbol, LSP: Show Definitions, LSP: Show Implementations, LSP: Show Type Definitions, LSP: Workspace Symbols, LSP: Incoming Calls, LSP: Outgoing Calls, LSP: Document Outline, LSP: Show Breadcrumbs, LSP: Breadcrumbs Outline | none |
| `find_references` | **off** | Show References Panel (and the hidden Hide References Panel close command) | none |
| `git_grep` | **off** | Git Grep, Git Grep: Toggle Case Sensitive | none |
| `git_explorer` | **off** | none (decorates the file explorer in place) | `color_names` (false) |
| `env_manager` | **off** | Env: Activate Detected Environment (venv / direnv / mise), Env: Reload Environment (re-capture after .envrc/mise.toml change), Env: Use System (Deactivate Environment), Env: Show Environment Status | none (detection is the user config `env.detectors`) |
| `dashboard` | **off** | Show Dashboard | `show_on_empty` (true), `refresh_seconds` (5, 1 to 60), `github_prs` (false), `github_checks` (false), `weather` (false), `weather_location` (empty), `weather_units` (metric), `network` (false) |
| `code_tour` | **off** | Tour: Load Definition..., Tour: Open Workspace Tour..., Tour: Next Step, Tour: Previous Step, Tour: Exit, Tour: Create from Current Location... | none |
| `compare_files` | **off** | Compare Files..., Compare Active File With..., Compare: Next Difference, Compare: Previous Difference, Compare: Switch Side, Compare: Copy Change to Other Side, Compare: Take Change from Other Side, Compare: Close | none |
| `live_diff` | **off** | Live Diff: Toggle (Global), Live Diff: Toggle (Buffer), Live Diff: vs HEAD, Live Diff: vs Disk (unsaved changes), Live Diff: vs Branch..., Live Diff: vs Default Branch, Live Diff: Refresh, Live Diff: Set Default Mode... | `default_mode` (head), `default_branch` (main), `debounce_ms` (75, 10 to 5000, project), `max_virtual_lines` (2000), `max_overlay_lines` (20000) |
| `live_sessions` | **off** | Live Agent Sessions, Live Agent Sessions: Refresh | none |
| `audit_mode` | **off** | Review Diff, Review Diff: Next Hunk, Review Diff: Previous Hunk, Review Diff: Refresh, Review Diff: Close | none |
| `devcontainer` | **off** | Dev Container: Show Info, Dev Container: Open Config, Dev Container: Refresh | none |
| `k8s_workspace` | **off** | K8s: Show Workspace Config, K8s: Open Config, K8s: Refresh Workspace Config | none |
| `pkg` | **off** | Package: Packages | none |
| `flash` | **off** | Flash: Jump | `labels`, `skip_rule` (true), `case_sensitive` |
| `vi_mode` | **off** | Vi Mode: Toggle | `auto_start` (true), `arrow_keys`, `ctrl_keys`, `ignore_case`, `smart_case`, `shift_width`, `clipboard_yank`, `search_word_under_cursor`, `hlsearch` |

Almost none of them binds a default key (Fresh's presets use most chords): `flash` binds `alt+j` (Flash: Jump), `merge_conflict` binds `alt+m` (Start Resolution) and `ctrl+alt+c` (Next Conflict), and `markdown_source` takes Enter, Tab and Shift+Tab in Markdown buffers. For the others, bind one yourself, with
`gvim.keymap.set("ctrl+alt+j", "flash.jump")` in your `init.lua`, or in `config.json`:

```json
{ "keybindings": { "bindings": [
  { "key": "j", "modifiers": ["ctrl", "alt"], "action": "flash.jump", "when": "normal" }
] } }
```

A plugin command's action id is `<plugin>.<name>`.

## What each one does

**goto_with_selection**: runs `set_mark`, then asks for a line and goes there, so the text between the
caret and the line is selected.

**tab_actions**: closes the other tabs, the tabs to the right or left, or all tabs; modified tabs are
skipped and counted in the status message. Moves the active tab left or right.

**git_gutter** (with its own merged Next/Previous Change; the standalone `diff_nav` plugin is separate): compares the open buffer with the file at
`HEAD` and draws a bar in the gutter for added and modified lines, a triangle where lines were deleted,
and marks on the scrollbar. **Next Change** and **Previous Change** are Fresh's merged diff navigation:
they collect the buffer's `HEAD` changed regions, the active hunks published by **live_diff** when that
plugin is loaded, the hunks of an active **compare_files** comparison, and the buffer's saved-diff lines
of unsaved changes, then deduplicate by line, sort ascending, and jump with Fresh's `Change n/total
[wrapped]` status. Unlike Fresh it compares the *buffer*,
so the marks follow unsaved typing after a short pause. The HEAD text is read once per file with
`git show` and compared in the host (no line arrays in Lua); a file outside a repository or untracked
still navigates its saved-diff/live_diff changes. In large-file mode it skips only the git HEAD
regions (the plugin never scans such a buffer) but still navigates saved-diff/live_diff targets;
when none exist it says `No changes`, without a special large-file refusal. Exports `hunks(buf_id)`
for other plugins (`{type = "added"|"modified"|"deleted", line, count}`; `line` is 0-based and a
deleted run points at the line before the seam). Its own **Next
Change**/**Previous Change** stay in the default set; the standalone `diff_nav` plugin adds Fresh's
`diff_nav.*` command namespace and i18n keys over the same sources, so both sets of rows are in the
palette. The side-by-side Compare view keeps its own **Compare: Next/Previous
Difference** and diff_nav now consumes the same hunks for its cross-context commands.

**diff_nav** (port of Fresh's `plugins/diff_nav.ts`): the unified **Next Change** and **Previous Change**
commands with Fresh's `diff_nav.next_change`/`diff_nav.prev_change` ids and translated
`cmd.next_change`/`cmd.prev_change`/`status.*` keys. Each invocation collects the change sources
Fresh merges: the hunks exported by **git_gutter** (`gvim.plugins.get("git_gutter").hunks(buf)`), the
hunks exported by **live_diff**, the hunks exported by an active **compare_files** comparison for the
buffer's side (`gvim.plugins.get("compare_files").hunks(buf)`; a deleted-only side targets the line
before its seam), and `buf:diff_saved()` for unsaved changes (for a deletion, the saved diff targets
the line before the seam, matching git_gutter's merged navigation). The Compare source makes the two
navigation entry points agree on the same hunks: while a Compare view is active, diff_nav's Next/Previous
Change walks the comparison changes instead of ignoring the cross-context view, while **Compare:
Next/Previous Difference** keeps its compare-side cursor/scroll behavior. A source that is unloaded,
does not export `hunks`, raises, has no git (git_gutter idle) or has no active comparison contributes
nothing; navigation still works from the remaining sources alone. Targets are sorted by byte position (line fallback when a
large/no-offset host cannot answer), deduplicated by line, and wrapped; the jump centers the target line
and the status row says `Change n/total` or `Change n/total [wrapped]` (Fresh's messages). Fresh ships
this plugin default-enabled; gvim keeps it opt-in (`default_enabled: false`) because adding it to
the ten-plugin default set measured 8.12 MiB against the 8 MiB idle-heap gate, and the nine
pre-existing default plugins stay unchanged. The original **git_gutter** Next/Previous Change rows
remain available in the default set, so default navigation is not lost; enabling this plugin adds
Fresh's `diff_nav.*` ids and i18n on top. It registers no keys or settings and runs everything in
plugin tasks/awaits (no blocking IO in Update). Both command sets then appear in the palette
(documented residual).

**git_statusbar**: the current branch (the first seven digits of the commit for a detached HEAD) on
the right of the status row. It refreshes when a file opens, a tab is activated, a file is saved,
when `HEAD` changes (it watches the directory that holds it), and when the terminal window regains
OS focus (`window_focused`, the gvim equivalent of Fresh's `focus_gained`; gvim's own
`focus_gained` is the different plugin-chrome keyboard hand-back event).

**git_find_file**: a fuzzy picker over the files git tracks (up to 50 000), opened relative to the
repository root; the list is read again each time.

**git_blame** (reduced; Fresh's panel is not ported, but a one-step inline history is): **Toggle Blame**
shows author, age and summary of the commit that last changed the cursor line, after the line; only 40
lines around the cursor are blamed (`git blame -L`), an edited buffer is blamed by its text, answers are
kept for 30 seconds. **Show Commit** opens the commit in a popup, **Copy Commit Hash** puts its hash on
the clipboard. **Git: Blame Previous Revision** resolves the first parent of the blamed commit and
re-blames the same cursor line at that revision, so the inline annotation walks one commit back;
**Git: Blame HEAD** returns to the working-tree revision. The history view is inline-only: it is not
Fresh's scrollable blame panel with parent-file content/history stack, and it refuses unsaved buffers
because unsaved lines cannot be mapped to a committed parent. Each step is one bounded parent resolve
plus one 40-line blame; the same safe git runner/authority path as Toggle Blame is used. History
commands require blame to be on, and every git await is followed by a capture check of buffer id/path/
version, cursor line, viewed revision and a generation counter; if the cursor, tab, buffer or blame
state changed meanwhile, the step is discarded instead of applying the old entry to the new line. An
update completion likewise skips painting when the viewed revision/generation is stale. A parent view
is tied to the path it was resolved for: Save As invalidates the old revision and the annotation
returns to HEAD for the new path. **Show Commit** and **Copy Commit Hash** capture the same
buffer/path/version/cursor/revision snapshot and discard their result if the target changed after a
git await (Show Commit also re-checks after reading the commit).

**merge_conflict** (reduced to in-buffer resolution; the three-panel merge view is not ported): marks
`<<<<<<<` / `|||||||` / `=======` / `>>>>>>>` blocks with highlights, jumps between them, and resolves the
one under the caret as ours, theirs or both in one undo step. **Start Resolution** turns on a key mode
(`n`/`p` next and previous, `u` ours, `t` theirs, `b` both, `q` leaves). It never reads a whole buffer:
it asks the host for the marker lines and rescans only what an edit touched. It binds `alt+m` to **Start Resolution** and `ctrl+alt+c` to **Next Conflict**.

**diagnostics_panel** (the first Fresh plugin port; reduced): lists every diagnostic the language
servers have published, grouped by file the way Fresh's panel groups them (a file header, then
`[E] line:col message` rows with the source in the right column; `E`/`W`/`I`/`H` are error, warning,
info and hint). **Show Diagnostics Panel** opens it filtered to the current file, **Toggle
Diagnostics Panel** opens or closes it, and the **All files** toggle inside the panel switches the
filter to every open document (Fresh's `a` key). Enter jumps to the diagnostic and closes the panel
so the keyboard goes back to the editor (a v1 widget panel holds it while it is focused; Fresh's dock
panel stays open), Esc closes it, `diagnostics_updated` rebuilds the rows live, and activating
another file re-filters them. Fresh's `syncWithEditor` is ported: moving the editor cursor onto a
diagnostic's exact file+line selects/reveals that row in the panel without changing focus or
opening a preview. Fresh's `navigateOnCursorMove` is ported too: moving the panel selection
previews the selected location after Fresh's 90 ms debounce, as a background preview tab
(`preview = true, background = true`, no focus change), so rapid browsing opens only the final
location; the debounce timer is cancelled when the panel closes, a row is activated, or the
rows are refreshed, and a tick additionally checks that its row is still the current selection
of the rebuilt node map before opening it. At most 500 diagnostics are listed (the API can return
50 000); a message is cut to its first line and 120 cells, and paths and sources are bounded too. Ported from
`refs/fresh/crates/fresh-editor/plugins/diagnostics_panel.ts` (275 lines) on the live-panel part of
its `plugins/lib/finder.ts` (1862 lines). Not ported from the Finder library: prompt mode and search
sources, the embedded preview pane, the utility-dock placement, the
`q`/`a`/`r` panel keys (the filter is a widget toggle, refresh is automatic, Esc closes) and Fresh's
severity colours. The diagnostic *code* is not shown because gvim's LSP API carries the source but
no code. It binds no key; bind `diagnostics_panel.toggle_diagnostics_panel` yourself. While the
panel is closed the plugin holds no hot-event subscription: the editor-cursor follow (`cursor_moved`)
is subscribed when the panel opens and dropped when it closes.

**git_log** (reduced to group-lite; Fresh's synthetic single-tab group is not ported): a magit-style
history view built as a layout of three split panels -- a one-line hint strip, the commit log, and the
selected commit's `git show --stat --patch`. The log rows carry Fresh's columns (short hash, relative
date, author initials, subject, [refs]) and theme colours. Moving the log cursor (j/k, arrows,
Page/Home/End) updates the detail after a 60 ms debounce; Enter on the log focuses the detail, Tab
switches pane, Enter on a detail row opens that file at its new-side line (a read-only
`git show <sha>:<path>` view), q/Esc closes, r refreshes and y copies the hash. The diff streams from
`git show --stat --patch` and is killed and footer-truncated at 4 MiB / 20 000 lines; per-file diffs
over 2000 changed lines are excluded with Fresh's footer, and every sha's diff is cached under
`<data>/git_log/` with a completion marker, so revisiting a commit is zero-git. Fresh's folds are
ported: the file and hunk folding ranges are published with `gvim.decor.set_folding_ranges`, the fold
marker shows in the gutter, a collapsed range hides its body behind the placeholder and the normal fold
action toggles it. The detail scroll position is remembered per commit via
`gvim.buf.get/set_window_state` (in-session, cleared with the buffer). Reductions (also in init.lua and
porting.md): the group is a layout of splits of the current tab and the panel buffers are also visible
as tabs; the layout supports fixed-height leaves (`{type="fixed",height=1}`), so Fresh's one-row
toolbar renders as one row (the split border can leave it a row off); the log is a styled buffer
whose cursor is the selection (a line decoration tints it), not a described List widget; the detail is raw git-show text carrying plugin decorations, and
the host's diff syntax paints added and removed rows in the theme's `editor.diff_*_bg` colours; there is no HEAD watcher (r refreshes, as Fresh's view).
Enter on a detail row opens the commit's historical blob (`git show <sha>:<file>`) as a read-only
virtual buffer, as Fresh does, so the detail rows are not line targets: a target opens the
working-tree file at a line, which is wrong for any commit that is not HEAD and cannot point at a
virtual buffer (see porting.md). It is **off by default** for the idle-heap gate (see Memory).

**live_diff** (feeds the merged navigation of git_gutter and the standalone diff_nav plugin): shows the open buffer against a baseline -- the
file at git HEAD (default), the file currently on disk, or a chosen git branch/ref -- with per-buffer
mode toggles and Fresh's eight Live Diff commands (no default keys). Added/modified new-side lines get
a background in the theme's `editor.diff_*_bg` colours and + / ~ gutter glyphs, word-level changes are
bold+underlined, and every hunk gets a scrollbar mark. The old side is fetched by the plugin
(`git show` for HEAD/branch, an encoding-aware disk read for disk mode) and diffed with
`gvim.diff`/Myers; word refinement runs in Lua under Fresh's caps. Large-file mode is refused with one
notice, and the plugin is idle outside a repository. It watches the git directory and, in disk mode,
the file itself, so commits and external writes refresh the diff. Deleted lines' old content is drawn
as tier-1c virtual rows above the seam, with the `-` gutter glyph and bold+underlined removed words, in
Fresh's priority order and capped at the host's 64 virtual rows per anchor plus an "… N more deleted
lines" summary row (the capability is live on the shipped host; an older host falls back to the line
backgrounds and new-side emphasis). It is opt-in like Fresh's live_diff.

**pkg** (reduced, read-only Fresh port): Fresh's `pkg.ts` is a decentralized git package manager
(install/update/remove from URLs or a registry, version pinning, registry sync, lockfiles). gvim has
no package store or git/network surface in this port, so **Package: Packages** only browses what the
editor already knows: `gvim.plugins.list()` rows grouped as INSTALLED (user/cli/init/buffer) and
BUNDLED, plus native built-in/user themes from `gvim.theme.all()` under THEMES. Each row shows the
package name, type `[P]`/`[T]`, version, enabled/disabled (or current/built-in/user for a native
theme) and a clipped description. Enter opens a read-only virtual detail panel with name, type,
version, status, source, path (plugin entry/dir, or `(embedded bundled plugin)`), description and
the manifest `requires` dependencies; a native theme is explicitly labelled as not a
package-manager package. The list is bounded to 1000 rows and clipped strings; the command schedules
the list build as a plugin task (the plugin/theme metadata APIs are synchronous and cheap), so it
never blocks Update, and picker/detail callbacks are tasks too. No filesystem or process I/O is
involved. Not ported/OPEN: install, install from
URL, update, remove, registry sync, lockfile, outdated/available registry rows, git clone/pull and
package-manager theme/language/bundle installs. No default keys; opt-in.

**k8s_workspace** (reduced, read-only Fresh port): **K8s: Show Workspace Config** reads and
validates `.fresh/k8s.json` as JSONC and opens a read-only virtual panel listing the default target
and every target's provider (`attach-existing`, `manifest`, `run` or `command`), cluster context,
namespace, manifest template, run name/image/timeout, command argv/cwd/down/status, workspace, vars
and `confirmCreate`. Configuration shape errors (missing required provider fields, unknown
`provider.kind`, an invalid `defaultTarget`, malformed vars/args) are listed in the panel and
reported in a notice; invalid JSON/read failures are reported too. **K8s: Open Config** opens the
file and **K8s: Refresh Workspace Config** re-reads/re-validates (refreshing an open panel). This
port never runs kubectl or a provider command, never creates a pod, probes RBAC/container env,
attaches, disconnects, or claims a cluster is connected: those need the devcontainer/kubectl and
`attachRemoteAgent` surfaces gvim does not have and are OPEN. Reads use `gvim.fs` (workspace
authority on a remote workspace), are bounded by a 1 MiB config/100 targets/100 vars/100 errors and
run as plugin-task awaits, never Update. No default keys; opt-in.

**devcontainer** (reduced, read-only Fresh port): **Dev Container: Show Info** discovers and parses
`.devcontainer/devcontainer.json`, `.devcontainer.json` or `.devcontainer/<subfolder>/devcontainer.json`
(Fresh's priority order) as JSONC through the host's bounded parser, then opens a read-only virtual
panel with the name/config path, image or Dockerfile/build or Compose source, features, forwarded
ports and attributes, container/remote environment, lifecycle commands, users, mounts and host
requirements. **Dev Container: Open Config** opens the detected file; **Dev Container: Refresh**
re-detects it (and refreshes an open info panel). Invalid JSON, unreadable/large files and a missing
config are reported in notices instead of a fake panel. Reads go through `gvim.fs` (the workspace
authority on a remote workspace) and are bounded; all work is async, never in Update. This port does
**not** run docker or the devcontainer CLI, attach/build/rebuild, run lifecycle commands, forward
ports, stream logs, show a status-bar summary or offer the attach prompts/scaffold/range features:
those need a container runtime and are OPEN. It never claims the editor is inside a container. No
default keys; opt-in.

**audit_mode** (reduced, read-only Fresh port): **Review Diff** reads `git status --porcelain -z
-uall` plus staged/unstaged `git diff --numstat` and lists staged, unstaged and untracked files
with their status and additions/deletions in a picker; **Review Diff: Refresh** re-reads that list
and **Review Diff: Close** drops the picker and any diff view. Choosing a file opens a read-only
`*Review Diff: <path>*` virtual buffer with git's unified diff (or, for an untracked file, the file
shown as a new file). Every new-side diff row is a line target that opens the working file at that
line; old-side-only and deleted rows have no working-tree target. **Review Diff: Next Hunk** and
**Review Diff: Previous Hunk** move the open diff view's cursor to the next/previous `@@` header
(the equivalents of Fresh's `n`/`p`); at a file boundary they load the next/previous file with a
hunk in the review's ordered file list and land on its first/last hunk. They say when the whole
review is at its first/last hunk or no diff is open. They only navigate when the review diff is the
active buffer; running them from another tab reports that the user must switch back first instead of
moving a hidden buffer. Each navigation is a new operation with a fresh diff generation and records
its source diff buffer/file; only the latest operation may present, so two overlapping next/prev
commands cannot let an older answer replace a newer one. Cross-file loads are bounded and also check
the active buffer, so close/reopen, file selection, tab switch, picker Esc or a Review Diff refresh
(which starts a new review-data epoch and invalidates in-flight navigation) discards a stale
navigation instead of opening a background diff. Overlapping refreshes are independent operations:
only the newest refresh may replace the file/repo state or show a notice, so an older success cannot
overwrite newer rows and an older error cannot appear after the newer result. It also follows the editor's own
`after_file_save`/`after_file_revert` events, the save/revert half of Fresh's review watch: with
the picker open it re-reads the list, and with a matching unstaged/untracked diff open it reloads
that read-only diff in place, preserving the current hunk/cursor and line targets without focusing
the buffer. When the review picker and a diff are open together, the list refresh and the matching
diff reload run as independent tasks: an unrelated save's winning list refresh cannot cancel the
open file's reload, and closing the picker mid-refresh does not cancel it either. Only a newer
matching reload, a closed/replaced diff or a different selection discards the reload. A failed
in-place reload reports a visible error. A staged diff is not reloaded by a working-tree save. Reload completions carry their own
operation sequence and diff generation, so a newer save, close/reopen, file selection or refresh
discards a stale reload. This is not Fresh's full watch: there is no background poll or `W` toggle,
so changes made outside gvim wait for the explicit **Review Diff: Refresh**. A truncated candidate still opens with an explicit
truncation notice; hitting the 200-candidate bound or failing to read a candidate is reported as
that reason, not as a global first/last hunk. All git calls go through the hardened `gvim.git` reader with bounded output; scans/diffs are async plugin tasks, no Update work,
and a closed picker/diff invalidates an in-flight answer by generation (an already-issued git
process is not killed, only its result discarded; this includes an untracked file's bounded
working-tree read, which is generation-checked after the read returns). The truncation footer is
reset on every refresh, including a clean or error result, and a path changed on both sides keeps
its staged and unstaged diffstats separate (totals add each category once). The port is read-only:
it does **not** stage,
unstage, discard, add comments or notes, export, review a commit range/stash, show side-by-side or
provide Fresh's background poll/`W` watch for changes made outside the editor; those need index writes, comment persistence or the orchestrator/range
machinery and are OPEN. Binary changes show git's message without line targets; very large output is
truncated and footered as such. No default keys; opt-in.

**live_sessions** (reduced Fresh port): Fresh keeps the orchestrator dock's external Claude/Codex
group current while the dock is open, polling local and cloud listings and handing rows to the
orchestrator, which owns opening/attaching. gvim has no orchestrator dock, cloud sign-in reader or
agent-attach/resume surface, so this opt-in plugin exposes the real subset gvim does have: the
native, bounded, trust-gated agent discovery (tmux sessions plus local Claude Code/Codex transcript
history) as a picker. **Live Agent Sessions** opens the picker, scans asynchronously and polls every
60 seconds while it stays open; **Live Agent Sessions: Refresh** re-lists immediately. A close
while a scan is still in flight discards that answer; reopening immediately also marks a refresh
pending, and when the old scan returns it starts the new scan at once instead of waiting for the
poll tick. Rows are capped at 200 and the app's scan caps apply. Enter opens a transcript row's file; live tmux rows are
informational and say that process attach is not ported, with their working directory. Empty,
no-store and unsupported states are explicit; a runtime without the discovery capability (a shared
daemon runtime, or a remote launch) reports unsupported rather than a fake empty list. Not ported:
cloud Claude/Codex sessions, orchestrator groups, process attach/resume, agent orchestration, vendor
CLI spawning, and the Settings/Menu toggles. No default keys.

**path_complete** (reduced; Fresh's passive prompt hook has no gvim seam): Fresh's plugin answers the
editor's built-in Open File / Save File As prompts with directory suggestions. gvim's Open File and
Switch Project are the native file browser and Save As is the Save-As dialog, and
`gvim.ui.set_prompt_suggestions` only fills a prompt a plugin opened, so there is no built-in prompt
event to answer; the completion engine is ported faithfully (directory/pattern parsing, hidden-file
rule, case-insensitive prefix, directories first then alphabetical, 100-row cap, and the *new file*
row for a path that does not exist yet) and exposed two ways: a `complete(input)` export for other
plugins, and one command, **Path Completion: Complete Path**, that opens a path prompt with live
suggestions (Enter on a directory descends, Enter on a file opens it, Esc cancels). Fresh's plugin
registers no command and no key; the gained command is the documented deviation, and binding it is
left to the user (no default key is added).

**lsp_help** (reduced): Fresh's per-language install hints for when a language server is missing or
fails. On `lsp_server_error` the plugin puts a Fresh-style line on the status row (*<Language> LSP
server not found — click the LSP status for install help*) and remembers the failure per language;
the next LSP-status click opens an action popup with the entry's explanation, copyable install
commands and gvim-native quick actions (Start/Restart LSP Server, Stop LSP Server, Toggle LSP for
This Buffer, Disable <Language> LSP, Go to Definition, Find References, Dismiss). Install hints are
carried for the languages a gvim user is likely to configure (Go, Rust, Python, TypeScript/
JavaScript, C/C++, Java, JSON, YAML, TOML, Markdown, Ruby, Bash, Lua, HTML, CSS, SQL). Reductions:
gvim's `lsp_server_error` payload carries only the error text (no language, error type or server
command), so the language is the active buffer's, the server command in the message is the entry's
documented default and *not found* is inferred from the error text; gvim's LSP status click also
opens the native server list, so the help popup stacks on it; there is no workspace-symbol quick
action in gvim's command table. Fresh has no i18n file for this plugin; the generic strings are
translated here and the per-server hints stay English. Like Fresh's, the plugin registers no
commands and no keys.

**asm_lsp** (reduced Fresh port): the helper for the `asm-lsp` assembly language server
(GAS/NASM/MASM, x86/x86-64/ARM64/RISC-V). asm-lsp needs a per-project `.asm-lsp.toml` to know
the dialect: without one it assumes GAS syntax on x86/x86-64, so NASM/MASM or ARM/RISC-V files get
wrong diagnostics, hover docs and definitions. When an assembly file opens (`.asm`, `.nasm`,
`.s`; already-open buffers are swept once plugins load) and neither the project `.asm-lsp.toml`
nor a global asm-lsp config exists, the plugin offers to create it, guessing the assembler from the
language and a bounded sample of the buffer (MASM's `proc`/`endp`/`assume`/`.model` tokens
versus NASM's default; `gas` for `.s`/`gas`) and the instruction set (ARM64 opcodes, RISC-V
opcodes, else x86/x86-64). The config is created exclusively at the project root (a `gvim.fs.write`
create-new, mapped to the workspace authority's `FileContentCreator` capability on a remote
workspace: one operation claims the path and writes the TOML, so a file another process created after
the offer is never overwritten; a backend without the capability refuses instead of racing a
create-then-write) with asm-lsp's
`[default_config]` shape and, for non-GAS dialects, the `default_diagnostics = false` block (its
built-in diagnostics only assemble GAS), then the native **lsp_restart** action is run so a running
server picks it up. The offer's other choices create the config for another dialect, dismiss it for
the session, or record "don't ask again" for that project in `gvim.state`. On an `lsp_server_error`
that names the assembly server the status row gets an install hint; clicking the LSP status opens
the help popup with copyable `cargo install asm-lsp` / `cargo binstall asm-lsp` commands, the
native Start/Restart LSP Server, Show LSP Status and **Disable Assembly LSP** actions, and Dismiss.
The disable action is not a catalog command: it re-checks that the active buffer is still an
assembly file and that `gvim.has("lsp")` is true, captures that buffer's path and language, and
dispatches gvim's controlled native `lsp_disable_language_target` action with both. The native
action re-validates that the same path/language is still active in the same Update slice before
muting, so a tab/document switch between the popup choice and the action cannot disable another
document's language. The mute itself is a **root-level language mute at the LSP port** (the port's
`SetLanguageEnabled`), not a per-buffer disable; the plugin cannot scope it to the originating
buffer or one root. The native LSP layer owns the asynchronous mute, its no-server notice and any
success notice, and the plugin never claims the mute completed. TUI integration tests exercise the
controlled target action's success, tab-switch refusal, language-mismatch refusal and nil-port
notice; there is still no plugin→TUI end-to-end test that loads asm_lsp itself into a real model. Commands:
**Assembly: Create .asm-lsp.toml** (re-opens the offer) and **Assembly: Show LSP Help**; it binds
no key and registers no settings. Reductions (also in porting.md): gvim's
`lsp_server_error` payload has no language/server/error-type fields, so the error is only claimed on
an assembly buffer whose message names `asm`/`assembly`; Fresh's per-language restart is replaced by
the native `lsp_restart` action for the active language; the
global-config check runs through the workspace file system, so a remote workspace checks the remote
home, not the local one; an overlong project root persists its "don't ask again" choice with a
truncated state key.


**csharp_support** (reduced Fresh port): the C# language-server helper. When a `.cs` file opens
(and, in the real editor, for the files already open when it loads), it walks up from the file at
most 20 directories looking for a `.csproj`/`.sln` and runs `dotnet restore` on that project root,
or on the file's directory when no project file is found. A restore runs once per project per
session; it follows the workspace authority, so a remote workspace restores on the remote host. The
workspace trust gate is honoured first: in a restricted/blocked workspace the restore is recorded as
pending and runs when `trust_changed` reports `trusted` (dotnet evaluates repository-controlled
MSBuild targets, so opening a C# file must not execute them before the user trusts the folder). The
status row reports restoring/completed/failed, with the first stderr line on failure. An
`lsp_server_error` that names the C# server (csharp/c#/roslyn/omnisharp) on a C# buffer puts an
install hint on the status row, and the LSP-status click opens the help popup with a copyable
`dotnet tool install --global csharp-ls` command, the native Start/Restart LSP Server and Show LSP
Status actions, **Disable C# LSP**, and Dismiss. The disable action captures the active C# buffer's
path/language and dispatches the controlled native `lsp_disable_language_target` action, which
re-validates that same document/language before a root-level LSP-port mute; it is not a catalog
command and the native LSP layer owns the async result/no-server notice (the plugin never claims
success). Commands: **C#: Restore Project** (force a restore for the active file's project) and
**C#: Show LSP Help**; it binds no key and registers no settings. It is opt-in
(`default_enabled: false`) because it runs dotnet. Reductions (also in porting.md): gvim's plugin API
cannot set the C# server's root URI, so Fresh's `setLspRootUri` step is replaced by gvim's LSP
configuration/root detection; there is no server-request event for Roslyn's
`workspace/_roslyn_projectNeedsRestore`, so that half of Fresh's restore flow is not ported (the
open-file restore covers the common case); the disable action is delegated to the controlled native
LSP action above (a popup/tab switch or language mismatch is refused before the port-level mute);
Fresh's `dotnet --version`
precheck is replaced by running restore and reporting a start error; and Fresh's i18n strings are
imported while gvim-only strings fall back to English.

**clangd_support** (reduced Fresh port): the C/C++ clangd helper. **Clangd: Switch Source/Header**
opens the counterpart file for the active C-family buffer; Fresh asks clangd for it with the
non-standard `textDocument/switchSourceHeader` request, which gvim's read-only LSP allowlist does not
carry, so the port uses a bounded same-named lookup instead: the stem is looked up with the header
extensions (`h`, `hh`, `hpp`, `hxx`, `hp`) for a source (`c`, `cc`, `cpp`, `cxx`) and with the source
extensions for a header, in the buffer's directory, the workspace root, `include/` and `src/` under
both, never outside the workspace. One match opens directly; several open a picker; none/unsupported
(or no active file) puts an explanatory status row. **Clangd: Project Setup** shows (and re-shows in a
read-only virtual buffer) the project root, whether `compile_commands.json` is present (checking the
root, `build/`, `out/` and `cmake-build-debug/`) or missing with the usual cmake/bear tip, whether
`.clangd` (or `.clangd/compile_flags.txt`) is present, build-system hints for CMake/Bazel/Makefile and
the general tips; it refreshes the same buffer instead of opening another. **Clangd: Open Project
Config** opens the `.clangd` next to the buffer or at the workspace root, or says it was not found.
Commands are palette rows; no keys and no settings. It is opt-in (`default_enabled: false`). Reductions
(also in porting.md): no clangd `textDocument/switchSourceHeader` (so no include-database-aware switch;
the heuristic does not follow `compile_commands.json` include paths or rename-case variations), no
`textDocument/clangd.fileStatus`/`$/memoryUsage` custom-notification status line (gvim's plugin API does
not expose server custom notifications), and the project setup is a virtual buffer rather than Fresh's
floating panel.

**welcome** (port of Fresh's `plugins/welcome.ts`): a deliberately small plugin-API demo, not a
feature plugin. It registers the same five palette commands as Fresh: **Plugin Demo: Open Help**
and **Plugin Demo: Save File** dispatch the built-in `show_help`/`save` actions, while **Plugin
Demo: Say Hello**, **Plugin Demo: Insert Time** and **Plugin Demo: Insert Comment** insert text at
the primary caret through `b:insert` (one undo step, no auto-indent) and report on the status row;
the time is the local `HH:MM:SS` clock. It binds no key and registers no settings, and is opt-in
(`default_enabled: false`) so it does not add demo rows to the default palette. Reductions (also in
porting.md): the demo comment chooses its line-comment prefix from the active buffer's language
(`--` for Lua/SQL, `#` for Python/Ruby/shell/YAML/TOML/Makefile/Dockerfile, `//` otherwise) instead
of Fresh's fixed `//`; Fresh's translated greeting/time/comment strings are imported, while
gvim-only strings fall back to English; and Fresh's debug-only load message is not reproduced
(gvim has no equivalent hidden
status).

**welcome_screen** (bounded, reduced port of Fresh's `plugins/welcome_screen.ts`): an opt-in
start page in a read-only virtual buffer. It renders an ASCII gvim wordmark, a one-line tagline,
three real doors (**Open a file** → native `open`, **New buffer** → native `new`, **Recent files** →
native `quick_open_buffers`), a bounded recent-file list from this session's opens (at most 8 rows,
seeded from at most 64 open buffers), real verbs (**Find file**, **Command palette**, **Settings**,
**File explorer**, **Help**) and a dashboard section whose rows come from
`gvim.dashboard.sections()` (at most 8 registered section titles; with no registrations the page
shows `No dashboard sections registered.`). Keys are j/k or arrows to move, Enter to activate,
q/Esc to close and r to refresh; when the page is the current buffer, the selected actionable row's
caret is revealed (set_cursor with reveal=true) so a row outside a small viewport is scrolled in
before Enter, while a background welcome tab never moves the caret or steals focus. Rows are also line targets (actions for the doors/verbs, paths for
recent files). Recent basenames/details and dashboard section titles are display-sanitized with the
same Cc/Cf/newline/invalid-UTF-8 cleaning used for dashboard external text before they reach a
page line, and line targets are computed from the resulting one-line rows; a hostile path cannot
inject a row or shift a click target. With `auto_open` (default true) the plugin opens the page as
a **background** tab once at startup and never steals the foreground; closing it is sticky (the
page does not reopen on file open/close). Fresh suppresses the page for a file launch
(`editor.fileLaunch()`): `gvim FILE`, `--simple` and `--diff` now report an immutable
`gvim.workspace.file_launch()` and `auto_open` skips only when it is true, so those launches open
only what was asked for; the explicit **Welcome Screen: Show** command still forces the page.
Standalone stdin is not a Fresh file launch (Fresh's `is_file_launch` excludes it), so it does
not suppress the page even though gvim still keeps stdin out of the session; explicit files
combined with stdin follow the files rule. Restored session tabs are never counted as a file
launch (a bare launch, a directory-only root and a restored session still get the background
page), and `--restore` with files follows Fresh's rule (session launch, flag false). A
daemon/shared runtime reports the active workspace build's flag; daemon workspaces are built from session state without launch files, so they report false. The value is fixed at startup
and is not inferred from the open buffers; command-line files and restored tabs can look the same. It starts no processes, performs no network/file writes or foreground IO, and uses only
existing plugin APIs. OPEN/reduced differences from Fresh (also in porting.md): Fresh's page is a
virtual buffer plus a page `WidgetPanel` with Contents, focus-follows-cursor and scroll-to-widget
composition; the current gvim plugin API has no equivalent page/widget-panel combination, so this
port uses the plain buffer and line targets above rather than claiming those interactions. It does
keep the selected row visible through caret reveal when the page is current; that is not Fresh's
page-level focus-follows-cursor/scroll-to-widget composition. It has
no onboarding levels, hover/chip styling, widget buttons, Contents sidebar, orchestrator/workspace/
agent cards or auto-attach toggle, and it does not call the orchestrator. Dashboard rows are only
the registered external-section metadata (the dashboard does not expose a rendered snapshot), not
built-in git/disk/weather content; selecting one asks the dashboard plugin to open via its public
command (a missing dashboard command reports an error), and with no registered section the page
shows its empty state.
The plugin is opt-in (`default_enabled: false`) whereas Fresh's page is part of its default startup
surface; with it enabled, `auto_open` controls the startup tab. The page footer also has a
**Show this screen on startup** switch (action row and the **Welcome Screen: Toggle Show on Startup**
command): it writes the persistent plugin state key `show_on_startup`, which takes precedence over
the `auto_open` setting for future launches; toggling it leaves the current page open. This is Fresh's
startup toggle behavior in a text/action row, not its checkbox widget styling.

**Persistence scope (reduced, not Fresh global).** A direct launch keeps this plugin state in the
editor-wide `plugin-data/welcome_screen/state.json`, so it survives a normal Stop and a new runtime
(the test writes a file, Stops, seeds a new runtime, and observes both false and true). A daemon
workspace instead scopes `gvim.state` to that workspace: the toggle affects only the current
daemon workspace, whereas Fresh's `getGlobalState("showOnStartup")` is editor-global. Writes are
asynchronous: `gvim.state.set` returns after updating memory, the file is written after a debounce
and synchronously on Stop; a write error is logged by the runtime but is **not** returned to this
plugin or shown as a toggle failure. Switching workspace queues the old workspace's state write as
a Work item, so a fast switch followed by exit can drop that queued write (the current namespace is
flushed on Stop; an old queued namespace is not guaranteed to be). The toggle is therefore best-
effort persistence, not a guarantee of Fresh's global/durable setting. Tests: `welcome_screen_test.go`
covers manifest/opt-in, page content, real door dispatch, line targets, recent-row activation,
close stickiness, background auto-open and no-focus-steal, selected-row reveal, auto_open=false,
the persistent startup toggle (row/command, next-startup auto-open and override precedence),
cross-Stop/new-runtime persistence, and dashboard metadata rows and empty fallback.

**rust_lsp** (reduced Fresh port): the Rust language-server helper. Fresh's `rust-lsp.ts` reacts to
Rust LSP errors with rows contributed to the native LSP-Servers popup, clears them on recovery, offers
Disable and a full/reduced-memory Configure Mode. gvim's plugin API has no LSP-menu contribution,
language-disable or register/restart-for-language seam, so the port exposes the user-visible
install/help half as four palette commands: **Rust: Copy rustup Install Command** copies
`rustup component add rust-analyzer` and **Rust: Copy brew Install Command** copies
`brew install rust-analyzer` (status row confirms); **Rust: Check rust-analyzer** runs
`rust-analyzer --version` through `gvim.proc` (authority-following and workspace-trust-gated, so
a remote workspace is probed on the remote host and a refused/untrusted probe is reported, not
bypassed). It reports the first version line only when `result.ok` is true (exit 0, no timeout or
signal); a non-zero exit, timeout or signal is a failed `--version` check. A failed probe is
reported as "could not run rust-analyzer on the workspace" with the local-only LookPath limitation
named — it is never reported as an authoritative missing binary for a remote workspace. The native Rust LSP server is gvim's built-in entry, not something this
plugin starts: it runs under the normal LSP rules (lsp_enabled, Rust root markers such as
Cargo.toml, auto_start), an explicit `lsp.rust` entry or a disabled Rust language replaces/skips
it, and this plugin's enable/disable toggle only controls the helper commands and never changes the
native selection. **Rust: Language Server Help** opens a
troubleshooting popup (install commands, the native-selection rules, PATH/restart/toolchain notes,
upstream docs link) with
copy-rustup/copy-brew/check actions, the native Start/Restart LSP Server and Show LSP Status actions,
and Dismiss. The help popup also offers **Disable Rust LSP**, which re-checks the active `.rs` buffer
and `gvim.has("lsp")`, then delegates to the controlled native `lsp_disable_language_target` action
with the captured path/language; the TUI validates that target before the root-level LSP-port mute,
and the plugin never claims the async result. No keys and no settings; opt-in
(`default_enabled: false`). Reductions (also in porting.md): no automatic LSP-error status or
contributed LSP-menu rows, no recovery detection, no Configure Mode/reduced-memory
registration/restart-for-language, and the plugin's local LookPath is never used for a presence
decision. The generic
`lsp_help` plugin already owns the automatic LSP-error status row/install popup for Rust; `rust_lsp`
adds the explicit copy/check/help palette commands instead of a second event handler.
**deno_lsp** (reduced Fresh port): the Deno helper plugin. Fresh's `deno_lsp.ts` selection
now lives in gvim's native core rather than in this plugin: when the probed workspace root (the
root gvim opened or switched to) has `deno.json`/`deno.jsonc`, the Deno runtime is confirmed
through the workspace authority under the workspace trust gate, `lsp_enabled` is true and the language is `typescript` or
`javascript`, core selects `deno lsp`. It applies only to an LSP manager created for that exact
workspace root (nested/per-file TS/JS roots keep their configured server) and installs the server
with `auto_start: false` (nothing is spawned silently). A Settings Save/Reload pushes the reloaded
config's server baseline into the LSP port: future managers and the next explicit Start/Restart use
that baseline (including a newly added explicit TS/JS server), while the reload itself does not
rebuild or restart an existing manager — it keeps its old config until the explicit action; Deno
auto-selection is not applied while an explicit/disabled entry is present. Settings reload and a live
trust revoke/restore pass that config through the LSP config gate before the baseline is pushed (the
Deno probe shares the gate): a Restricted/Blocked workspace cannot gain auto-starting servers via a
reload or trust change, a manager created after a revoke does not auto-start, and after a revoke an
already-created manager's old AutoStart configs no longer start new servers on Open without stopping
running processes; manual Start/Restart remains available. Auto-start and launch spawn are two live checks, not atomic: a revoke between them refuses only when
the spawn gate denies (Blocked, remote workspace not Trusted, or a project-contained executable);
under Restricted a system-installed binary outside the project may still start. A revoke after the
spawn gate passes can likewise let that in-flight start proceed. Neither check nor a revoke stops
existing processes; later Opens do not auto-start after the revoke. Both replacing and same-config
Restart preflight the spawn gate before stopping, so a denial preserves running servers/config;
preflight→launch is non-transactional (old servers may already be stopped and the new start refused
when the spawn gate denies, though under Restricted a system-installed binary outside the project
may still start); a repeated Start of a running server returns the live handle without spawning or
consulting the gate, even when Blocked; other start failures (missing binary/initialize) are not
transactional. Installing/clearing the selection never
stops a running server. An explicit Start/Restart LSP Server for typescript/javascript applies the
completed current-root selection to an existing manager: when its config differs, gvim replaces the
whole language server list and restarts only that language's servers (other languages/universal
servers untouched); when the selection was cleared, the same explicit action restores the configured
baseline. If a whole-language replacement is needed, Restart Server on any row of that language
replaces the entire list with the current selection (the clicked row's name is not used); otherwise
it restarts only the named server. A root-wide LSP Restart rebuilds managers, and the selection
applies when the root creates the next one; `deno lsp` stays `auto_start: false`, so an explicit
Start/Restart launches it. Explicit
`lsp` entries for typescript/javascript always win and opt out, as do disabled languages and
`lsp_enabled: false`; `tsx` is untouched. The bundled plugin's `default_enabled: false` toggle
only controls its helper commands and does **not** control native auto-selection, which runs in
core regardless of whether the plugin is loaded/enabled. The helper keeps three palette commands.
**Deno: Check Project** checks the workspace root (cwd fallback) for `deno.json` and `deno.jsonc` and runs
`deno --version` through `gvim.proc` (authority-following and workspace-trust-gated). It reports
ready only when the config exists and `result.ok` is true (exit 0, no timeout or signal); config
with a non-zero/timeout/signal `--version` is a failed check and is never reported as ready. It
also reports runtime without a Deno project config, probe start/trust-gate failures, or neither;
the probe-failure cases say the runtime could not be run on the workspace under the workspace trust
gate, name the local-only LookPath limitation, never claim a definitive missing binary, and include
the install command.
**Deno: Copy Install Command** copies the official
`curl -fsSL https://deno.land/install.sh | sh` command with a status-row confirmation; the help
popup still lists Homebrew, but a local `brew` lookup never chooses the remote host's installer.
**Deno: Language Server Help** opens a popup with both install commands, the native-selection and
explicit-config rules, the `typescript`/`javascript` `lsp` configuration entry (`deno lsp`,
`auto_start: false`, `root_markers: ["deno.json", "deno.jsonc"]`,
`initialization_options: { enable: true }`), the workspace-authority probe note and local-only
LookPath caveat, the native Start/Restart LSP Server and Show LSP Status actions, and the upstream
installation link. The popup also offers **Disable Deno LSP for this language** when the active
buffer is a TypeScript/JavaScript file (`.ts`, `.tsx`, `.js`, `.jsx`, `.mjs`, `.cjs`) and
`gvim.has("lsp")` is true: it re-checks that active TS/JS buffer and LSP feature, captures that
buffer's path/language and dispatches the controlled native `lsp_disable_language_target` action;
the TUI re-validates the target before the root-level LSP-port mute. The language gate is strict:
`buf:language()` must be `typescript` or `javascript` (Fresh's `DENO_LANGUAGES` only has those two);
a `.tsx` buffer is also accepted when its language is `tsx`, and a `.tsx` detected as `typescript`
targets `tsx` to match gvim's native `openLanguage` mapping; the controlled native target action
accepts that post-open tsx language for a TypeScript-detected .tsx buffer (TUI integration test),
so the mute reaches the same server that LSP opened. The extension set is supported only when the
buffer is already recognized as `typescript`/`javascript`: `.ts`, `.tsx`, `.js`, `.jsx`, `.mjs`,
`.cjs`. Fresh's default `languages` table maps JavaScript only js/jsx/mjs and has no cjs; gvim
matches that, so `.cjs` (and any other extension not mapped to javascript in the user's `languages`
entries) is text/unrecognized by default and must be configured before this action appears. `.jsx`
detected as react is not a JS language and is likewise rejected. A text/unrecognized language shows
`status.not_deno`, offers no disable row and never passes `text` to the native mute. A tab switch,
non-TS/JS buffer or missing LSP gives the matching notice and does not dispatch; the plugin never
claims the async result. No keys and no settings; opt-in (`default_enabled: false`). Reductions (also in porting.md):
the plugin itself does not select/restore language servers (core does), no active-window root
tracking, no LSP-menu contribution/register seam, and Configure/restart-for-language is absent;
Check Project's presence check is the authority-following `deno --version` probe; the plugin's
local LookPath is never used for presence. Tests: `plugin/bundled/deno_lsp_test.go` covers the
manifest/commands/no bindings, the curl installer regardless of a local brew, an authority probe
succeeding despite a local LookPath miss, ready only for a successful version check, non-zero
exit/timeout/signal never ready, probe start and workspace-trust-gate failures (not a missing-binary
claim), the probe-failure/ready/no-config states, `deno.jsonc` and a custom workspace root, the help
popup's native auto-selection/explicit-config/plugin-toggle text and actions plus
copy/check/native-restart dispatch, and the controlled Disable action (recognized TS/JS extensions, exact target args and
strict-language/tsx mapping, real default-config detection of unmapped `.cjs`, unrecognized-language/
no-LSP refusals, no false success).

**slang_lsp** (reduced Fresh port): the Slang shader language-server helper. Fresh's `slang-lsp.ts`
is an LSP URI-scheme provider: it claims `slang-synth://` and, when go-to-definition lands on a
builtin (float3, normalize, ...), dumps the builtin module with `slangd --print-builtin-module` to a
version-keyed cache and opens it read-only. gvim now handles those builtin external locations
**natively** (no plugin API): only the `slang-synth` scheme and a bounded module authority are
accepted; the fixed `slangd --print-builtin-module <module>` runs through the workspace authority
under the trust gate with a 5 s / 4 MiB bound; timeout, signal, truncation and a non-zero exit are
errors; the generated cache file is opened read-only at the LSP position. It is **local-authority
only** — a remote/SSH workspace refuses explicitly with no local fallback. There is still no generic
plugin URI-scheme/external-URI API. The opt-in helper keeps the install/verification/help surface as
three commands. **Slang: Check slangd /
slangc** runs `slangd --version` and `slangc -v` through `gvim.proc` (authority-following), uses
`result.ok` for the verdict, and reports each tool's first version line — a non-ok result includes
its exit code and first output line, and a start failure reports the actual error verbatim (a remote
authority error, sandbox refusal or broken executable is never rewritten as a missing binary).
**Slang: Copy Install Command** copies the official release instruction (Slang has no single
official package-manager command; the upstream install page points at binary releases from
https://github.com/shader-slang/slang/releases, with `slangc`/`slangd` to put on PATH). **Slang:
Language Server Help** opens a popup covering the `lsp.slang` configuration (and a user
`languages.slang` entry for `.slang`/`.slangh` when gvim does not detect them), the native
`slang-synth://` builtin-module flow (local-only, bounded, read-only cache), verification/restart
notes, the release link and https://shader-slang.org/docs/, with copy/check and the native
Start/Restart LSP Server and Show LSP Status actions. When the active buffer is `.slang`/`.slangh`
and `gvim.has("lsp")` is true, the popup also offers **Disable Slang LSP**: it re-checks the active
Slang buffer and LSP feature, captures that buffer's path/language and dispatches the controlled
native `lsp_disable_language_target` action; the TUI validates the target before the root-level
LSP-port mute. The gate requires `buf:language() == "slang"` exactly: Fresh has no default
`.slangh` language mapping, and gvim's default detection leaves it text/empty, so `.slangh` needs a
user `languages.slang` entry (or Set Language) before this action is available. A `.slang`/`.slangh`
buffer detected as text is not a Slang buffer, shows `status.not_slang`, offers no disable row and
never passes `text` to the native mute. A tab switch, non-Slang buffer or missing LSP gives the
matching notice and does not dispatch; no async success is claimed. No keys/settings; opt-in
(`default_enabled: false`). Reductions (also in porting.md): no generic plugin URI-scheme/external-URI API and no
plugin-provided schemes; the native flow is local-only with no remote generation; no version-keyed
persistent cache (each request regenerates); the helper itself does not handle synth URIs. The
install command is release guidance rather than a package-manager one-liner.

**markdown_source** (port of Fresh's `plugins/markdown_source.ts`): gvim's editor already
implements Fresh's Markdown source-mode list rules natively (`editor/markdown.go`, called from the
built-in `insert_newline`/`insert_tab`/`dedent_selection` actions), so this is the explicit Lua
port, opt-in (`default_enabled: false`) for plugin parity rather than a native-behaviour gap.
**Enter** on a list item continues it with the same indent and bullet, increments an ordered item
(keeping its digit count), starts checkboxes unchecked, and drops the marker of an empty item; a
non-list line copies its leading blanks. **Tab** on a blank unordered/checkbox item indents four
spaces and cycles the bullet forward (`* → - → + → *`, checkbox state kept); an ordered item or an
item with content inserts four spaces. **Shift+Tab** on such a blank item with at least four blanks
of indent removes four and cycles the bullet in reverse (`* → + → - → *`); otherwise it runs
`dedent_selection`. The three handlers are hidden commands bound in the normal editing context and
check the active buffer themselves; a non-Markdown buffer, a selection, extra carets or any active
input mode (vi) falls back to the editor's own action, so nothing outside Markdown source editing is
intercepted. Reductions (also in porting.md): no `markdown-source` buffer-local mode/status and no
auto-activation events — gvim's plugin API has no source/compose view query and a buffer mode would
add an `(Esc x3 leaves)` hint to every Markdown tab; the handlers stay normal-context and fall back
instead. The plugin does not add indentation/undo semantics beyond Fresh's: four spaces are
hardcoded (no `tab_size`/`use_tabs`), and the selection/extra-caret path delegates to the built-in
actions, which already step aside from the native Markdown rules.
**compare_files** (port of Fresh's own compare plugin; reduced): **Compare Files...** (or **Compare
Active File With...**) opens the two paths you type and shows them as two editable splits of the
current tab — changed lines get a background, in-line changes a highlighted span, a gutter glyph and
a scrollbar mark show each change, the panes scroll together anchored at every change, and
**Next/Previous Difference** walk the hunks. **Copy Change to Other Side** (`vimdiff` `dp`) and **Take
Change from Other Side** (`do`) merge the hunk under the caret through the host's compare offsets and
`replace_range`, one edit and one undo step per merge. The diff comes from
`gvim.diff.compare_buffers`, so no text crosses into Lua; a binary or large-file side is refused with
a status message. It exports its active-comparison hunks for **diff_nav** (`hunks(buf_id)`:
`{line, count, jump, side}`; a deleted-only side uses the line before the seam), so the two
navigation entry points agree on the same changes instead of diff_nav ignoring the Compare view.
It is opt-in (`default_enabled: false`) for the bundled idle-heap budget and
registers no default keybindings, matching Fresh; the native **Compare** view stays the default
surface.

**lsp_navigation** (with the shared finder): a port of Fresh's plugin on the shared `gvim.finder`
library. **Go to LSP Symbol** asks the server for `textDocument/documentSymbol`, lists the symbols as
`[kind] name` rows with the source line as the detail, filters them with Fresh's fuzzy algorithm
(re-sorted by source line) and previews the selection in the buffer: moving the highlight moves the
cursor onto the symbol and paints it in the search-match style, Enter commits the jump, Esc restores
the cursor and clears the highlight. Symbol names are refined against their line (servers report the
declaration start), and the kinds and both response shapes (DocumentSymbol/SymbolInformation) follow
Fresh. The three **LSP: Show Definitions / Show Implementations / Show Type Definitions** list
commands are gvim additions (in Fresh those are core actions that jump directly; gvim keeps its
native Go to Definition rows) and share the references location list: one result jumps, several open
the filterable list with preview tabs. **LSP: Workspace Symbols** (`workspace/symbol` with an empty
query), **LSP: Incoming Calls** / **Outgoing Calls** (call hierarchy; one row per call site) and
**LSP: Breadcrumbs Outline** (the symbols enclosing the caret, outermost first) use the same list;
**LSP: Show Breadcrumbs** prints the enclosing symbols as `A > B > C` in the status message and **LSP:
Document Outline** is **Go to LSP Symbol** under another name. Fresh's enclosing-symbol preselection is ported as a
user-visible approximation: the smallest symbol range containing the cursor is moved to the first
row for the empty query and previewed immediately (cursor + search-match highlight); gvim's picker
has no initial-selected-index option, so typed queries return to the normal line order and the
selected row is no longer forced. The highlight is a decoration, not an overlay namespace. No
default key binding (Fresh's plugin registers none).

**find_references** (reduced; command driven): Fresh's plugin answers the host's `lsp_references` /
`lsp_implementation` events with a finder list; gvim has no such plugin events (its native Find
References action opens its own UI), so the port exposes Fresh's own command keys — **Show References
Panel** asks the server for `textDocument/references` for the symbol under the cursor (UTF-16 column,
`includeDeclaration = true`, results capped at 100 rows) and shows a filterable location list:
`relative path:line` rows, the source line as the detail (open buffers read in place, other files via
one bounded read), a preview tab as the selection moves, Enter commits the jump and the hidden **Hide
References Panel** command closes the list. Empty results, a missing symbol/language and server errors
keep the Fresh status-line messages. Reductions: no reference/implementation events (commands
instead), no implementation listener (that list lives in lsp_navigation), and no source-split reuse
(preview tabs do). No default key binding (Fresh's core action has its own chord; the plugin
registers none).

**dashboard** (reduced Fresh port): a read-only start-screen tab shown when the editor has no file
open — a centred frame with the repository state (branch, upstream tracking `↑ahead ↓behind` or
`no upstream`, ahead/behind the default branch, working-tree counts, origin), recently visited
files, keyboard-navigable quick actions (settings,
explorer, live grep, theme, Git Log, diagnostics) and best-effort `df` disk rows rendered like
Fresh's disk section: a bounded ASCII `[###---]` meter (at most 18 cells), the percentage, and a
used / capacity cell; the percentage thresholds map to ok/warn/err. Only strict
integer cells are displayed: capacity/used must be plain non-negative integer KiB and the percentage
must be `^%d+%%?$` (digits with at most one trailing `%`), so decimals, signs, whitespace and
misplaced percent signs are unknown. A bad cell, zero/invalid capacity or `used > capacity` marks
that mount `unavailable` and omits the meter/used-capacity cell instead of showing bogus 0/0 or
100%; a valid numeric percentage (including df's odd out-of-range value) is clamped to 0..100, and
mount labels go through the Cc/Cf display sanitizer before painting. Tab/Down/j and
Shift-Tab/Up/k move, Enter activates (a buffer-local mode); **Show Dashboard** reopens it. It
auto-opens on an empty editor and after the last file closes (`show_on_empty`) and closes when a file
opens; a timer refreshes it (`refresh_seconds`). A plugin developer can override the auto-open decision
for the current process with `gvim.dashboard.set_auto_open` (session-scoped, not persisted; the
dashboard falls back to `show_on_empty` when it is unset), without opening/closing the visible
dashboard immediately. The recent-file rows are line targets
(`gvim.buf.set_line_targets`): a single click opens the file. Action rows (including the Git
section's branch-log row) publish action line targets running a hidden command the dashboard
plugin owns, so a mouse click goes through the same dispatcher as the keyboard mode while a target
still cannot dispatch another plugin's command. The branch-log row
is only advertised when the git_log plugin's command is registered; without it the row says
`branch log  unavailable (enable git_log)` and is not clickable. A GitHub `origin` (scp-like,
ssh:// or https form) is normalized to `https://github.com/owner/repo`: the repo and branch lines
become clickable through dashboard-owned action targets, and keyboard-equivalent **Open
Repository**/**Open Branch** rows are appended to the Actions section (the branch URL is
`<repo>/tree/<branch>`). Clicking or activating one calls `gvim.url.open`, which accepts only
http(s), shows a modal Yes/No confirmation naming the URL, and launches the platform browser
opener without a shell (see [api-system.md](api-system.md#gvimurl)); a non-GitHub https origin is
still openable as a repo row but has no branch URL, and unsafe/unopenable origins stay plain text.
The GitHub PR section is opt-in: set `plugins.dashboard.settings.github_prs = true` and
`plugins.dashboard.settings.network = true` (the host network gate; project config never grants
it), and a GitHub `origin` makes one bounded REST request
(`https://api.github.com/repos/<owner>/<repo>/pulls`) rendered as up to six open PRs. Each PR row is
a dashboard action row: Tab/Down/Up moves the focus onto it and Enter opens it, and the same line is
an action line target (`dashboard.click_pr_<n>`) so a mouse click runs the identical dashboard-owned
command. The URL is not taken from the response: the dashboard rebuilds
`https://github.com/<owner>/<repo>/pull/<number>` from the slug it normalized from the git remote
and a strictly validated positive integer PR number, then calls `gvim.url.open`, which re-applies
the http(s)-only scheme policy, rejects credentials/control characters and asks the modal Yes/No
confirmation (see [api-system.md](api-system.md#gvimurl)). API bodies are untrusted: non-string or
control/overlong titles are sanitized before painting, entries with a string, fractional, negative
or out-of-range number (and any `html_url`/other API-provided links) are ignored, and at most six
valid rows are kept from at most 200 examined entries. Without a token a private or missing
repository returns 404 and the section shows a muted `unavailable` line (no notice, the rest of the
dashboard is unaffected); failures and the disabled state are also one muted line. An additional
opt-in `github_checks = true` (with `github_prs` and `network`) enriches each shown PR with
Fresh's compact check tag: `pass`/`fail`/`run` from one public, unauthenticated
`GET /repos/<owner>/<repo>/commits/<head-sha>/status` per valid row. The repo slug and head SHA are
taken only from the already-validated remote/list response (never an API-provided URL), no token or
auth header is sent, and failures leave the tag absent or keep the last good one without a notice.
The unauthenticated 60-request/hour budget is bounded per **loaded dashboard plugin instance**: the
PR list refreshes at most every 2 minutes (≤30/h) and the status batch at most every 15 minutes, for
at most six status requests per batch and a 2 s timeout each (≤24/h; steady-state ceiling ~54/h);
this is asynchronous and never blocks the UI. GitHub's 60/h limit is per source IP, so multiple gvim
processes or other plugins share it and can still consume that headroom. Fresh's full check-run
rollup and unresolved review-thread columns need GraphQL/authentication and remain unported.
PR titles are untrusted display text: Cc controls (including C1) become spaces and Cf format marks
(bidi embedding/override/isolate marks, zero-width space/joiner/non-joiner, soft hyphen, BOM, tag
characters, interlinear annotation and the other assigned Cf ranges in the embedded Unicode-15.1
table) are removed before the 200-byte display cap, so a title cannot reorder or visually hide the
row. Residual: newly assigned/future Cf code points need a table update; non-Cf invisible marks
(combining/variation selectors), Unicode line/paragraph separators, homoglyph/confusable text and
invalid UTF-8 bytes are not normalized (invalid bytes become spaces); removing zero-width joiners can
change emoji presentation. Tiles/rows remain bounded to the sanitized/truncated title. The weather
section is also opt-in: set `plugins.dashboard.settings.weather = true` (and `network = true`),
optionally `weather_location` (a place name or coordinates; empty asks wttr.in to use the host's
public IP) and `weather_units = "metric" | "imperial"`. One bounded request to
`https://wttr.in[/<percent-encoded location>]?format=j1` (5 s timeout, 10 min refresh TTL) is
rendered as Fresh does: `now` with condition/temperature/feels-like/wind/humidity, `5pm` from
the nearest 3-hour sample and a `tomorrow` range/condition summary. The request carries no
credentials or caller headers; response text goes through the same Cc/Cf display sanitizer before
painting, stays in memory (no disk write) and never blocks the UI; a
timeout, offline response, malformed body or disabled network renders one muted
`unavailable`/`invalid response`/`network disabled` line with no notice. The third-party section
API: third-party plugins can register dashboard sections through
`gvim.dashboard.register_section` (bounded plain-data refresh callbacks; see
[api-system.md](api-system.md#gvimdashboard)); external section titles/rows go through the same
Cc/Cf display sanitizer as PR titles, so plugin- or network-provided bidi/zero-width marks cannot
spoof the row. gvim's built-in Weather/GitHub/Disk/Git rows are unchanged. Reductions: no Utility Dock placement and no sessions section; weather has no multi-day detail beyond today/tomorrow and no dedicated
location picker (the place setting is a plain string, and empty delegates geolocation to
wttr.in).

**code_tour** (reduced Fresh port): loads a Fresh `.fresh-tour.json` or a VS Code Edit CodeTour
`.tour` (auto-detected; bounded at 1 MiB / 200 steps), shows the steps in a floating widget card (list,
step counter and location, prose as styled plain text, Previous/Next/Jump/Exit buttons, hint bar) and
walks them: each step opens its file at the line range with `gvim.buf.open` and highlights it with
anchored decorations. The plugin keeps local line anchors because its step rows are a widget-panel
card, not buffer lines: `gvim.buf.set_line_targets` maps a *buffer* line to a file:line, so it cannot
express a widget row (see porting.md). One tour at a time; the last tour and step are
restored from `gvim.state` on the next load. **Tour: Create from Current Location...** writes a
VS Code-compatible starter `.tour` under `.tours/`, atomically. A VS Code tour's recorded commit
`ref` (or a Fresh manifest's `commit_hash`) is compared with the repository's current short HEAD
through the safe `gvim.git` reader; a mismatch puts Fresh's **recorded at … · you are on …** warning
in the panel header, and a branch/tag ref or a missing repository is silently ignored. Load/schema/
step errors stay in notices. No default bindings (Fresh's keys live in its dock-buffer mode; here
the panel owns the keyboard).

**git_grep** (with the shared finder; reduced adaptation): Fresh's live git grep. **Git Grep** opens a
live search over the files git tracks: each typed query runs `git grep -n --column -I [-i] -e
<query>` in the repository of the active buffer (else the workspace's), and the first 100 matches
are listed as repo-relative `path:line` rows with the source line as the detail; Enter opens the
file at the match's line and column. Case starts insensitive (Fresh's effective default) and the
choice is module-level, so it outlives one prompt, as in Fresh; git's basic regex is used (no `-E`).
git grep's exit codes follow Fresh: 0 matches, 1 no matches (a normal search), ≥2 a real error, and
a missing git binary or a non-repository has its own status line. The git call goes through the
hardened `gvim.git` gateway (same argv/env safety as the other git ports) with a 15 s timeout and a
2 MiB output cap. Reduced adaptation: Fresh's prompt toolbar (`setPromptToolbar` Case toggle +
`toggleOverlayToolbarWidget`, clickable/Space) has no gvim prompt surface, so the case state is
shown in the prompt's footer/hint line and flipped by Fresh's second palette command **Git Grep:
Toggle Case Sensitive** (it refreshes an open prompt, otherwise applies to the next search); Fresh
binds no chord, so neither does the port. No preview band: Fresh disables the Finder preview because
its floating overlay draws its own, and gvim has no overlay preview band, so passing rows open no
preview tabs; a superseded query's results are discarded by the picker generation guard instead of
killing the git process.

**git_explorer** (reduced Fresh port): shows each tracked file's git status in the file explorer —
`M`/`A`/`D`/`R`/`C`/`U` badges with Fresh's priorities (with `!` for conflicts; a staged modification
uses the added colour) and, with `color_names`, colors changed names and their ancestor directories —
from `git status --porcelain -z` in the workspace root's repository and in independent
nested repositories below it (Fresh's #2592 monorepo behavior): the root repo is decorated first,
then a bounded breadth-first directory scan finds directories whose `.git` entry is present (a
directory for a normal repo, a file for a worktree) and each is decorated from its OWN status root,
so a vendored repo's files are not left blank when the outer repo stops at its boundary. The scan
issues one `gvim.fs.list` per candidate directory (never a per-entry `exists` round trip, which
would be one remote authority round trip per file), up to 128 directory listings and 32 nested
repos with Fresh's depth 3, under a 1.5 s wall-clock budget; the same budget caps the per-repo
`git status` reads. When a bound is reached the repos found so far are decorated, the result is
partial, and a one-time warning notice says the discovery stopped instead of scanning without a
bound; unloading the plugin stops a yielded refresh from applying. It refreshes on ready,
after-file-open/save/revert, `focus_gained` and config changes plus a debounced watcher of the
root repository's git directory and each discovered nested repository's `.git` directory (at most
33 watcher directories, the discovery repo cap plus the root), so commits/checkouts elsewhere
update both root and nested badges. A linked-worktree `.git` file is not watched because its HEAD
lives elsewhere; those nested repos stay event-driven. It clears itself outside any repository. It drives the
explorer through `gvim.explorer.set_decorations/clear_decorations` and `set_slots/clear_slots` (the
host surface mirrors Fresh's setFileExplorerDecorations/Slots with per-plugin namespaces, priority
then namespace resolution, absolute/root-relative path normalization and owner cleanup); colour keys
reuse the theme's `ui.file_status_*_fg` keys, so a theme can recolour the badges. Reductions: no
hover tooltips, symbols only (no arbitrary icons), linked-worktree `.git` files are not watched
(regular nested `.git` directories are), and a setting change applies on the next refresh. It is opt-in: enabling it
costs about 1.5 MiB of idle heap, which the default set cannot afford.

**env_manager** (reduced Fresh port): detects and activates the project's environment — virtualenv,
direnv, mise, pipenv, poetry — so language servers, formatters, terminals, plugin processes and
shell commands inherit it. Detection and the activation trust gate are host features
(`env.detectors`, `gvim.env`): Fresh's six detectors are the bundled list, first match wins, a bare
`.venv` without an interpreter does not count, and nothing is executed until activation in a trusted
workspace. The plugin auto-activates path-only environments silently once the workspace is trusted,
asks the core trust dialog for others and re-runs activation on `trust_changed`; it shows an `env`
status-bar pill (`system` / `<name>` / `<name> ✓` / `<name> (locked)`) and offers**Env: Activate
Detected Environment**, **Env: Reload Environment**, **Env: Use System** and **Env: Show Environment
Status**. It is opt-in (memory, and activating a repository's shell snippet is a trust decision).
Reductions: no widget-panel UI yet, no file watcher (use Env: Reload), no devcontainer deferral or
authority handling, no automatic shell-environment activation (explicit command), one local
environment, and no persisted recipe. It declares no plugin settings; detection is configured
through the user config `env.detectors`.

**The Markdown outline is not a plugin in gvim.** Fresh ships the Contents section as the bundled
plugin `markdown_toc.ts`; gvim builds it into the TUI as the sidebar section under the File Explorer
(`docs/sidebar.md`), so there is no Lua plugin to load, and the palette rows **Markdown: Show
Outline**, **Markdown: Close Outline** and **Markdown: Contents — Follow Cursor/Scroll** are built-in
commands like the compare rows. The config keys stay under the plugin's old name, so a Fresh config
keeps working: `plugins.markdown_toc.enabled` and `plugins.markdown_toc.settings` with
`max_depth` (6, 1 to 6), `follow` (`cursor` or `scroll`), `fold_buffer` (false), `rows` (10,
1 to 200) and `auto_open` (true). The one residual gap against Fresh is `fold_buffer`: gvim's editor
folds are indentation folds, so a heading usually opens none to fold.

**flash**: label-based jump. **Flash: Jump** starts a session; every character you type extends a literal
pattern, every visible match in every window gets a label, and typing a label jumps there (Backspace
shortens the pattern, Enter jumps to the nearest match, Esc or any other key ends it). Labels follow
flash.nvim: the nearest matches get the most comfortable keys, and a letter that is also the next
character after a match is not used. The plugin binds `alt+j` to **Flash: Jump** while it is enabled.

**vi_mode**: vi-style modal editing: normal, insert, visual (character, line, block) and operator-pending
modes, counts, operators with motions and text objects, registers, marks, `.` repeat, and `:` commands.
It starts in normal mode (`auto_start`); **Vi Mode: Toggle** switches it off and on. The panic chord
`ctrl+^` clears every plugin input mode, vi's included: vi says so, and **Vi Mode: Toggle** brings it back.
Three quick `Esc` presses do **not** (vi binds `Esc`, so mashing it is normal). `ctrl+c` is an `Esc`: it
leaves insert and visual mode and cancels a pending command and the `:` and `/` prompts. Keys vi does not
bind (`ctrl+s`, `alt+...`, function keys) still reach the editor. A selection made with the mouse (a drag,
a double click) or with shift and the arrow keys is the visual mode, so `d`, `y`, `c` and the rest act on
it; a paste puts the text before the caret in normal mode (like `P`), replaces the selection in visual
mode and is inserted in insert mode. A Vim command vi does not have (`gn`, `gq`, `=`, `R`, `@`, ...)
says `vi: ... is not supported`, and the keys that belong to it (the motion of `gq`, the text typed
after `cgn` or `R` until `Esc`, the register of `q`) are dropped instead of running as commands.
The `:` and `/` prompts keep a history (Up and Down), the matches of a search are highlighted in the
window (`:noh` hides them, `:set nohls` or the `hlsearch` setting turns it off), a search that wraps says
so, and `:set` changes `hls`, `et`, `ts` and `sw` for real and refuses what the editor decides (`ai`, `is`).
Exports `toggle`, `enable`, `disable` and `is_enabled`. Its README
(`plugin/bundled/plugins/vi_mode/README.md`) lists the keys; the differences from Vim are:

* Vim's insert-mode ctrl keys (`ctrl+w`, `ctrl+u`, `ctrl+o`, `ctrl+r`, `ctrl+d`, ...) and the `ctrl+w`
  window prefix are not provided: they run the keymap's own binding (`ctrl+o` opens the file picker,
  `ctrl+r` Replace, and on the macOS keymap `ctrl+w` closes the tab);
* no macros (`q`, `@`; use the editor's own), jump list, replace mode (`R` outside visual), sentences,
  folds, `:!` filters, `:normal`, `gn`/`cgn`, `gq`/`gw`/`=`/`!`, `g;`, and the marks `'.` `` `[ `` `']` `^`
  (each says it is not supported);
* marks do not move when the text changes;
* a count insert (`3ix<Esc>`) or `.` repeat is one more undo step; a change operator and the text typed
  after it (`cw foo<Esc>`) are two undo steps, because the editor has no undo group that stays open
  while keys are typed;
* `j` and `k` are the editor's own moves (by display rows on a wrapped line); `d3j` and the visual mode
  count logical lines; the caret column after `u`, `guu`, `gUU`, `g~~`, `Vy`, `yip` can differ from Vim's;
* only the primary caret is used; block mode counts characters, not display cells;
* a file still being indexed (large-file mode) refuses commands with a message until the index is ready.
* search (`/`, `?`, `n`, `N`, `*`, `#`, `:g`) is not available on a file in large-file mode: vi says
  "no search in large-file mode"; use the editor's own find.

## Memory

Plugins are loaded eagerly and cost heap while idle (a few KB of bytecode per Lua function plus
tables). `TestBundledPluginsIdleHeap` in `internal/app` starts the **real runtime with the default-enabled
bundled set** in a real editor session with a file open, forces a full collection, and compares the live
heap with the same session under `--no-plugins`. The measurement runs in a **fresh process** (the test
starts the test binary again and runs only itself there), because the live heap after the whole package
is about 88 MiB and moves by several MiB between two measurements; it also fails when the cost reads
under 1 MiB, so a polluted baseline cannot pass unnoticed. The gate is 8 MiB: if the default set ever costs more,
the largest plugins must become `default_enabled: false` in their `plugin.json` and this page must say so.

Measured with the plugin set of this release (macOS arm64, Go 1.27, pure heap in use after a full GC):

| | Heap |
|---|---|
| the editor session without plugins | 2.4 MiB |
| the same session with the 9 default-enabled plugins loaded and idle | about 5.4 MiB |
| **cost of the default set** | **about 3.0 MiB** (gate: 8 MiB) |

The cost used to be about 5.8 MiB (7.25 MiB in an earlier release). The compiler leaves every function
prototype's code, constant and debug slices in oversized append-grown arrays, and gopher-lua kept them
for the life of the plugin (about 17 KiB per Lua function); `plugin.CompileSource` now copies them at
their exact length, which took the default set from 5.78 to 2.97 MiB. The gate passes with a wide
margin (about 5 MiB of headroom), but the opt-in plugins below stay off by default: they were
switched off when the cost was higher, and enabling one is the user's choice (`plugins.<name>.enabled`).
The shared Finder library is not part of the idle cost: `diagnostics_panel` requires
`gvim.finder` lazily on its first build. `live_diff` is opt-in like Fresh's, and `compare_files` ships
off as well.

Per plugin, the heap one plugin adds to an otherwise empty runtime (measured in the plugin test harness
over a fake host, so the real host adapters add to these figures; the nine default plugins sum to
about 2.2 MiB here against 3.0 MiB in the real editor): `goto_with_selection` 0.23 MiB,
`tab_actions` 0.22, `path_complete` 0.23, `lsp_help` 0.23, `git_statusbar` 0.24, `flash` 0.26,
`git_find_file` 0.27, `git_blame` 0.27, `diagnostics_panel` 0.27, `git_gutter` 0.28,
`merge_conflict` 0.31, `git_log` 0.49 and **`vi_mode` 1.45 MiB**, the largest by far (it is off by
default for that reason, like `flash`). Switching off the git plugins you do not use saves about 1 MiB.
