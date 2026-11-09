# Bundled plugins

gvim ships thirty plugins inside the binary, written in Lua like any other (their sources are under
`plugin/bundled/plugins/<name>/` in the repository; **Plugin: Manager** shows them, and
`config.json` can override them). Nine are on by default; `flash`, `vi_mode`, `live_diff`, `git_log`,
`compare_files`, `merge_conflict`, `dashboard`, `code_tour`, `lsp_navigation`, `find_references`,
`git_grep`, `git_explorer`, `env_manager`, `asm_lsp`, `csharp_support`, `clangd_support`, `welcome`, `markdown_source`, `rust_lsp`, `deno_lsp` and `diff_nav` are off until you turn them on:

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
| `git_blame` | on (idle until toggled) | Git: Toggle Blame, Git: Show Commit, Git: Copy Commit Hash | `debounce_ms` (150, 50 to 2000, project), `summary_width` (60, 10 to 200, project) |
| `merge_conflict` | **off** | Merge: Start Resolution, Next Conflict, Previous Conflict, Use Ours, Take Theirs, Use Both, Resolve Conflict... | none |
| `diagnostics_panel` | on | Show Diagnostics Panel, Toggle Diagnostics Panel | none |
| `path_complete` | on | Path Completion: Complete Path | none |
| `lsp_help` | on | none (event driven: an LSP server error puts an install hint on the status row, and the LSP status click opens the help popup) | none |
| `asm_lsp` | **off** | Assembly: Create .asm-lsp.toml, Assembly: Show LSP Help | none |
| `csharp_support` | **off** | C#: Restore Project, C#: Show LSP Help | none |
| `clangd_support` | **off** | Clangd: Switch Source/Header, Clangd: Project Setup, Clangd: Open Project Config | none |
| `rust_lsp` | **off** | Rust: Copy rustup Install Command, Rust: Copy brew Install Command, Rust: Check rust-analyzer, Rust: Language Server Help | none |
| `deno_lsp` | **off** | Deno: Check Project, Deno: Copy Install Command, Deno: Language Server Help | none |
| `welcome` | **off** | Plugin Demo: Open Help, Plugin Demo: Save File, Plugin Demo: Say Hello, Plugin Demo: Insert Time, Plugin Demo: Insert Comment | none |
| `markdown_source` | **off** | none (hidden Enter/Tab/Shift+Tab key handlers for Markdown lists) | none |
| `lsp_navigation` | **off** | Go to LSP Symbol, LSP: Show Definitions, LSP: Show Implementations, LSP: Show Type Definitions | none |
| `find_references` | **off** | Show References Panel (and the hidden Hide References Panel close command) | none |
| `git_grep` | **off** | Git Grep, Git Grep: Toggle Case Sensitive | none |
| `git_explorer` | **off** | none (decorates the file explorer in place) | `color_names` (false) |
| `env_manager` | **off** | Env: Activate Detected Environment (venv / direnv / mise), Env: Reload Environment (re-capture after .envrc/mise.toml change), Env: Use System (Deactivate Environment), Env: Show Environment Status | none (detection is the user config `env.detectors`) |
| `dashboard` | **off** | Show Dashboard | `show_on_empty` (true), `refresh_seconds` (5) |
| `code_tour` | **off** | Tour: Load Definition..., Tour: Open Workspace Tour..., Tour: Next Step, Tour: Previous Step, Tour: Exit, Tour: Create from Current Location... | none |
| `compare_files` | **off** | Compare Files..., Compare Active File With..., Compare: Next Difference, Compare: Previous Difference, Compare: Switch Side, Compare: Copy Change to Other Side, Compare: Take Change from Other Side, Compare: Close | none |
| `live_diff` | **off** | Live Diff: Toggle (Global), Live Diff: Toggle (Buffer), Live Diff: vs HEAD, Live Diff: vs Disk (unsaved changes), Live Diff: vs Branch..., Live Diff: vs Default Branch, Live Diff: Refresh, Live Diff: Set Default Mode... | `default_mode` (head), `default_branch` (main), `debounce_ms` (75, 10 to 5000, project), `max_virtual_lines` (2000), `max_overlay_lines` (20000) |
| `flash` | **off** | Flash: Jump | `labels`, `skip_rule` (true), `case_sensitive` |
| `vi_mode` | **off** | Vi Mode: Toggle | `auto_start` (true), `arrow_keys`, `ctrl_keys`, `ignore_case`, `smart_case`, `shift_width`, `clipboard_yank`, `search_word_under_cursor`, `hlsearch` |

None of them binds a default key (Fresh's presets use most chords); bind one yourself, with
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
plugin is loaded, and the buffer's saved-diff lines of unsaved changes, then deduplicate by line, sort
ascending, and jump with Fresh's `Change n/total [wrapped]` status. Unlike Fresh it compares the *buffer*,
so the marks follow unsaved typing after a short pause. The HEAD text is read once per file with
`git show` and compared in the host (no line arrays in Lua); a file outside a repository or untracked
still navigates its saved-diff/live_diff changes, and a buffer in large-file mode keeps its own notice:
**Next Change** and **Previous Change** say that git changes are not tracked there. Exports `hunks(buf_id)`
for other plugins (`{type = "added"|"modified"|"deleted", line, count}`; `line` is 0-based and a
deleted run points at the line before the seam). Its own **Next
Change**/**Previous Change** remain for the pre-diff_nav behavior; the standalone `diff_nav` plugin
adds Fresh's `diff_nav.*` command namespace and i18n keys over the same three sources (so both sets
of rows are in the palette). The side-by-side Compare view keeps its own **Compare: Next/Previous
Difference**.

**diff_nav** (port of Fresh's `plugins/diff_nav.ts`): the unified **Next Change** and **Previous Change**
commands with Fresh's `diff_nav.next_change`/`diff_nav.prev_change` ids and translated
`cmd.next_change`/`cmd.prev_change`/`status.*` keys. Each invocation collects the three change sources
Fresh merges: the hunks exported by **git_gutter** (`gvim.plugins.get("git_gutter").hunks(buf)`), the
hunks exported by **live_diff**, and `buf:diff_saved()` for unsaved changes (for a deletion, the saved
diff targets the line before the seam, matching git_gutter's merged navigation). A source that is unloaded,
does not export `hunks`, raises, or has no git (git_gutter idle) contributes nothing; navigation still
works from live_diff and the saved diff alone. Targets are sorted by byte position (line fallback when a
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
the right of the status row. It refreshes when a file opens, a tab is activated, a file is saved and when
`HEAD` changes (it watches the directory that holds it).

**git_find_file**: a fuzzy picker over the files git tracks (up to 50 000), opened relative to the
repository root; the list is read again each time.

**git_blame** (reduced; Fresh's panel with history stepping is not ported): **Toggle Blame** shows
author, age and summary of the commit that last changed the cursor line, after the line; only 40 lines
around the cursor are blamed (`git blame -L`), an edited buffer is blamed by its text, answers are kept
for 30 seconds. **Show Commit** opens the commit in a popup, **Copy Commit Hash** puts its hash on the
clipboard.

**merge_conflict** (reduced to in-buffer resolution; the three-panel merge view is not ported): marks
`<<<<<<<` / `|||||||` / `=======` / `>>>>>>>` blocks with highlights, jumps between them, and resolves the
one under the caret as ours, theirs or both in one undo step. **Start Resolution** turns on a key mode
(`n`/`p` next and previous, `u` ours, `t` theirs, `b` both, `q` leaves). It never reads a whole buffer:
it asks the host for the marker lines and rescans only what an edit touched.

**diagnostics_panel** (the first Fresh plugin port; reduced): lists every diagnostic the language
servers have published, grouped by file the way Fresh's panel groups them (a file header, then
`[E] line:col message` rows with the source in the right column; `E`/`W`/`I`/`H` are error, warning,
info and hint). **Show Diagnostics Panel** opens it filtered to the current file, **Toggle
Diagnostics Panel** opens or closes it, and the **All files** toggle inside the panel switches the
filter to every open document (Fresh's `a` key). Enter jumps to the diagnostic and closes the panel
so the keyboard goes back to the editor (a v1 widget panel holds it while it is focused; Fresh's dock
panel stays open), Esc closes it, `diagnostics_updated` rebuilds the rows live, and activating
another file re-filters them. At most 500 diagnostics are listed (the API can return 50 000); a
message is cut to its first line and 120 cells, and paths and sources are bounded too. Ported from
`refs/fresh/crates/fresh-editor/plugins/diagnostics_panel.ts` (275 lines) on the live-panel part of
its `plugins/lib/finder.ts` (1862 lines). Not ported from the Finder library: prompt mode and search
sources, the preview pane, `syncWithEditor`/`navigateOnCursorMove`, the utility-dock placement, the
`q`/`a`/`r` panel keys (the filter is a widget toggle, refresh is automatic, Esc closes) and Fresh's
severity colours. The diagnostic *code* is not shown because gvim's LSP API carries the source but
no code. Like every bundled plugin it binds no key; bind `diagnostics_panel.toggle_diagnostics_panel`
yourself.

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
whose cursor is the selection (a line decoration tints it), not a described List widget; the detail is raw git-show text carrying plugin decorations,
not a host `.diff` background layer; there is no HEAD watcher (r refreshes, as Fresh's view).
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
create-new, mapped to the workspace authority's `FileCreator` capability on a remote workspace: a file
another process created after the offer is never overwritten; a backend without the capability
refuses instead of racing a check) with asm-lsp's
`[default_config]` shape and, for non-GAS dialects, the `default_diagnostics = false` block (its
built-in diagnostics only assemble GAS), then the native **lsp_restart** action is run so a running
server picks it up. The offer's other choices create the config for another dialect, dismiss it for
the session, or record "don't ask again" for that project in `gvim.state`. On an `lsp_server_error`
that names the assembly server the status row gets an install hint; clicking the LSP status opens
the help popup with copyable `cargo install asm-lsp` / `cargo binstall asm-lsp` commands, the
native Start/Restart LSP Server and Show LSP Status actions, and Dismiss. Commands: **Assembly:
Create .asm-lsp.toml** (re-opens the offer) and **Assembly: Show LSP Help**; it binds no key and
registers no settings. Reductions (also in porting.md): gvim's `lsp_server_error` payload has no
language/server/error-type fields, so the error is only claimed on an assembly buffer whose message
names `asm`/`assembly`; Fresh's "Disable Assembly LSP" popup action is not ported (gvim has no
plugin call to disable a language's server; the native LSP status menu offers Stop/Disable); Fresh's
per-language restart is replaced by the native `lsp_restart` action for the active language; the
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
Status actions, and Dismiss. Commands: **C#: Restore Project** (force a restore for the active file's
project) and **C#: Show LSP Help**; it binds no key and registers no settings. It is opt-in
(`default_enabled: false`) because it runs dotnet. Reductions (also in porting.md): gvim's plugin API
cannot set the C# server's root URI, so Fresh's `setLspRootUri` step is replaced by gvim's LSP
configuration/root detection; there is no server-request event for Roslyn's
`workspace/_roslyn_projectNeedsRestore`, so that half of Fresh's restore flow is not ported (the
open-file restore covers the common case); Fresh's "Disable C# LSP" action is replaced by the native
LSP status menu (gvim has no plugin call to disable a language's server); Fresh's `dotnet --version`
precheck is replaced by running restore and reporting a start error; and the plugin ships only English
i18n strings.

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
of Fresh's fixed `//`; the greeting/time/comment strings are English-only rather than Fresh's 15
locales; and Fresh's debug-only load message is not reproduced (gvim has no equivalent hidden
status).


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
and Dismiss. No keys and no settings; opt-in (`default_enabled: false`). Reductions (also in
porting.md): no automatic LSP-error status or contributed LSP-menu rows, no recovery detection, no
Disable Rust LSP action, no Configure Mode/reduced-memory registration/restart-for-language, and
the plugin's local LookPath is never used for a presence decision. The generic
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
installation link. No keys and no settings; opt-in (`default_enabled: false`). Reductions (also in
porting.md): the plugin itself does not select/restore language servers (core does), no
active-window root tracking, and no LSP-menu/disable/register seam; Check Project's presence check
is the authority-following `deno --version` probe; the plugin's local LookPath is never used for
presence. Tests: `plugin/bundled/deno_lsp_test.go` covers the
manifest/commands/no bindings, the curl installer regardless of a local brew, an authority probe
succeeding despite a local LookPath miss, ready only for a successful version check, non-zero
exit/timeout/signal never ready, probe start and workspace-trust-gate failures (not a missing-binary
claim), the probe-failure/ready/no-config states, `deno.jsonc` and a custom workspace root, the help
popup's native auto-selection/explicit-config/plugin-toggle text and actions plus
copy/check/native-restart dispatch.

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
a status message. It is opt-in (`default_enabled: false`) for the bundled idle-heap budget and
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
the filterable list with preview tabs. Reductions (also in porting.md): the picker has no initial
selected index, so the enclosing symbol is not preselected as in Fresh, and the highlight is a
decoration, not an overlay namespace. No default key binding (Fresh's plugin registers none).

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
explorer, live grep, theme, Git Log, diagnostics) and best-effort `df` disk rows. Tab/Down/j and
Shift-Tab/Up/k move, Enter activates (a buffer-local mode); **Show Dashboard** reopens it. It
auto-opens on an empty editor and after the last file closes (`show_on_empty`) and closes when a file
opens; a timer refreshes it (`refresh_seconds`). A plugin developer can override the auto-open decision
for the current process with `gvim.dashboard.set_auto_open` (session-scoped, not persisted; the
dashboard falls back to `show_on_empty` when it is unset), without opening/closing the visible
dashboard immediately. The recent-file rows are line targets
(`gvim.buf.set_line_targets`): a single click opens the file, while the command rows stay
keyboard-only because a target opens a file, not an action — this changed in round 22: action
rows (including the Git section's branch-log row) publish action line targets running a hidden
command the dashboard plugin owns, so a mouse click goes through the same dispatcher as the
keyboard mode while a target still cannot dispatch another plugin's command. The branch-log row
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
dashboard is unaffected); failures and the disabled state are also one muted line. The weather
section is also opt-in: set `plugins.dashboard.settings.weather = true` (and `network = true`),
optionally `weather_location` (a place name or coordinates; empty asks wttr.in to use the host's
public IP) and `weather_units = "metric" | "imperial"`. One bounded request to
`https://wttr.in[/<percent-encoded location>]?format=j1` (5 s timeout, 10 min refresh TTL) is
rendered as Fresh does: `now` with condition/temperature/feels-like/wind/humidity, `5pm` from
the nearest 3-hour sample and a `tomorrow` range/condition summary. The request carries no
credentials or caller headers, stays in memory (no disk write) and never blocks the UI; a
timeout, offline response, malformed body or disabled network renders one muted
`unavailable`/`invalid response`/`network disabled` line with no notice. The third-party section
API: third-party plugins can register dashboard sections through
`gvim.dashboard.register_section` (bounded plain-data refresh callbacks; see
[api-system.md](api-system.md#gvimdashboard)); gvim's built-in Weather/GitHub/Disk/Git rows
are unchanged. Reductions: no Utility Dock placement and no sessions section; weather has no multi-day detail beyond today/tomorrow and no dedicated
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
VS Code-compatible starter `.tour` under `.tours/`, atomically. Load/schema/step errors stay in
notices. No default bindings (Fresh's keys live in its dock-buffer mode; here the panel owns the
keyboard).

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
from `git status --porcelain -z` in the workspace root's repository. It refreshes on ready,
after-file-open/save/revert, `focus_gained` and config changes plus a debounced watcher of the
repository's git directory (so commits/checkouts elsewhere update the badges), and clears itself
outside a repository. It drives the
explorer through `gvim.explorer.set_decorations/clear_decorations` and `set_slots/clear_slots` (the
host surface mirrors Fresh's setFileExplorerDecorations/Slots with per-plugin namespaces, priority
then namespace resolution, absolute/root-relative path normalization and owner cleanup); colour keys
reuse the theme's `ui.file_status_*_fg` keys, so a theme can recolour the badges. Reductions: only the
workspace-root repository is decorated (no nested sub-repo discovery), no hover tooltips, symbols
only (no arbitrary icons), and a setting change applies on the next refresh. It is opt-in: enabling
it costs about 1.5 MiB of idle heap, which the default set cannot afford.

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
character after a match is not used.

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

Plugins are loaded eagerly and cost heap while idle (about 18 KB of bytecode per Lua function plus
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
| the editor session without plugins | 2.05 MiB |
| the same session with the 9 default-enabled plugins loaded and idle | about 9.3 MiB |
| **cost of the default set** | **about 7.25 MiB** (gate: 8 MiB) |

The gate passes with the nine default-enabled plugins (about 0.8 MiB of headroom). Round 9 added host
API surface and four new ports; the ten-plugin set first measured 8.07–8.10 MiB, over the gate, so
`merge_conflict` (about 1.35 MiB) ships `default_enabled: false` as well, and the round-10 ports are
opt-in too (the default set is unchanged; round-10 host growth moved the cost from 6.97 to about 7.25
MiB; `env_manager` and `git_explorer` would each cost about 1.5 MiB if enabled). The shared Finder
library is not part of the idle cost: `diagnostics_panel` requires
`gvim.finder` lazily on its first build (requiring it at load measured 8.7 MiB with the round-8 set,
over the gate).
The opt-in plugins stay off for the heap budget: a trial measurement with `git_log` loaded too read
about 8.7 MiB of idle heap (over the gate; that configuration never passed), `live_diff` is opt-in
like Fresh's, and `compare_files` ships off as well. Per plugin, in
a bare runtime that holds only that plugin (each figure includes about 0.3 MiB for the runtime itself, and
the first one loaded pays for the shared prelude): `goto_with_selection` 0.6 MiB, `git_find_file` 0.7,
`git_statusbar` 0.8, `tab_actions` 0.85, `diagnostics_panel` about 0.9 (measured as the rise of
the default set when it joined it, not in a bare runtime), `git_log` about 1.9 (the same kind of rise;
that is why it is off), `path_complete` and `lsp_help` together about 1.2 (the same rise; both are
small passive plugins, so they stay on), `git_gutter` 1.0, `git_blame` 1.15,
`merge_conflict` 1.35 (flipped off in round 9 when the ten-plugin set measured 8.07–8.10 MiB),
`flash` 1.55, and **`vi_mode` 6.9 MiB**. `vi_mode` is the one that would bust the
budget (the default set plus vi would be about 14 MiB), which is why it is off by default; `flash` is off by
default as well. If you do not use the git plugins, switching them off in the config saves about
4 MiB.
