# Language servers

gvim detects language servers the way Fresh does. It has a built-in table of servers, one for
each language below, and it starts a language's server when you open a file of that language and
the server's program is on `PATH`. There is nothing to configure for the common ones: install the
server and open a file.

Where gvim differs from Fresh: Fresh's shipped entries all carry `auto_start: false`, so a server
there waits to be started from the palette, while its documentation says it is used automatically.
gvim follows the documentation and starts a built-in server when a file opens. An entry with
`"auto_start": false` (see below) gives the Fresh behaviour for one language.

## Several servers per language

A language may have more than one server, as in Fresh: an `lsp` entry is one server object or a
list of them.

```json
{
  "lsp": {
    "python": [
      { "command": "pyright-langserver", "args": ["--stdio"] },
      { "command": "ruff", "args": ["server"], "only_features": ["format", "diagnostics"] }
    ]
  }
}
```

A `universal_lsp` entry is a server that attaches to every buffer whatever its language, keyed by
a name of its choosing (Fresh's universal servers):

```json
{ "universal_lsp": { "spell": { "command": "harper-ls", "args": ["--stdio"] } } }
```

Every configured server that serves a document is sent its `didOpen`, and features are routed
between them:

- **Merged** features combine the answers of every eligible server: diagnostics, completion, code
  actions, document symbols and workspace symbols.
- **Exclusive** features go to the *first* eligible server, in config order: hover, definition,
  implementation, references, format, rename, signature help, inlay hints, folding ranges,
  semantic tokens and document highlights. An empty answer from that server is final; gvim does
  not fall back to the next one (Fresh's deliberate rule).

A server is eligible for a feature when its `only_features`/`except_features` filter allows it
*and* its initialize result advertises the capability. A server whose initialize has not finished
advertises nothing, so no request is routed to a server of unknown capabilities. A provider the
server registers later with `client/registerCapability` counts too: many servers advertise their
features dynamically, and gvim learns them without a restart.

The first server of the list is the one the status row names and the one the plugin request
(`gvim.lsp.request`) asks when it does not name a server.

## Server settings

Each server object is Fresh's `LspServerConfig`. The fields gvim reads:

| Field | Meaning |
|-------|---------|
| `command`, `args` | The program and its arguments. `args` without `command` keeps the built-in program. |
| `enabled` | `false` turns the server off. A language whose servers are all off gets no server, the built-in one included. |
| `auto_start` | `false` keeps the server dormant: it does not start when a file opens, but **Start/Restart LSP Server** runs it. |
| `name` | The display name, in messages and the status popup. Default is the program's name. |
| `root_markers` | The files (or globs) that mark a project root, as described below. |
| `env` | Extra environment variables for the process, laid over the inherited ones. |
| `initialization_options` | The JSON sent as `initialize`'s `initializationOptions`, verbatim. The same object answers the server's `workspace/configuration` requests, by section. |
| `language_id_overrides` | A file extension (without the dot) mapped to the `languageId` `didOpen` sends for it, for example `{"tsx": "typescriptreact"}`. |
| `only_features`, `except_features` | The feature filter described above. Use one or the other. The names are Fresh's: `diagnostics`, `completion`, `code_action`, `document_symbols`, `workspace_symbols`, `hover`, `definition`, `implementation`, `references`, `format`, `rename`, `signature_help`, `inlay_hints`, `folding_range`, `semantic_tokens`, `document_highlight`. An unknown name is a load diagnostic. |
| `process_limits` | Fresh's memory and CPU caps (`max_memory_percent` 1–100, `max_cpu_percent` 1–1000 where 100 is one core, `enabled` default on). Applied on Linux (cgroup v2, or an `sh ulimit -v` memory wrapper) and recorded as not applicable on macOS/Windows; see [Process limits](#process-limits). |

An entry cannot name a field Fresh does not have; a load reports it instead of guessing.

### Process limits

`process_limits` is Fresh's per-server resource cap. gvim applies what a pure-Go editor can and
reports the rest; a configured limit is never silently dropped.

| Platform | Memory cap | CPU cap |
|----------|-----------|---------|
| Linux | a user-delegated cgroup v2 `memory.max` (a percent of `/proc/meminfo` MemTotal); with no delegated cgroup, a `sh -c 'ulimit -v <KB>; exec "$@"'` wrapper around the server | a delegated cgroup v2 `cpu.max` quota (100% = one core); with no cgroup a CPU *percentage* has no rlimit equivalent (an rlimit CPU limit is seconds), so it is recorded as not applied |
| macOS, Windows | not applied | not applied |

On Linux the editor creates one `gvim-lsp-<pid>` cgroup under the same user-delegated cgroup
locations Fresh uses (`GVIM_CGROUP_BASE` overrides the base, for tests and unusual delegations),
writes the caps before the start and moves the child into it with `cgroup.procs` right after. A
missing `sh` with no cgroup leaves only the recorded reason. Elsewhere Go's `os/exec` has no
pre-exec hook and `syscall.SysProcAttr` has no rlimits, and the platform has no cgroup interface;
Fresh also applies these limits on Linux only. The server status popup shows, under a server that
configures limits, what applied or why nothing could — "process_limits (memory 50% of system
memory, cpu 90% (100% = one core)): applied: cgroup memory 50%, cgroup cpu 90%" on Linux, or
"process_limits (…): not applied on darwin: the limits need Linux cgroup v2 …" elsewhere.

**Settings page.** The Settings page's **Language Servers** section lists every
language with a server (the built-in table, the user's `lsp` entries, and
`universal_lsp`), with each language's effective servers and their live manager
state (running/starting/stopped/failed). Each language page shows the parsed
servers read-only and edits the language's whole server array as validated
JSON: `config.PatchBytes` cannot address one element of an array, so the page
writes the array at `lsp.<language>` (or `universal_lsp.<name>`) through the
same `config.ValidateEdits` a load uses. The project layer may only mute or
enable servers: a project edit that names a command, args, env or
initialization options is refused with the loader's diagnostic, and the project
page shows the layer's entries with the JSON row locked. **Start/Restart/Stop**
rows start or stop the configured servers by hand (they override `auto_start`
and mute) and are live actions, not config edits. `process_limits` is accepted,
validated and applied or reported as described under [Process limits](#process-limits) (see the
table above). The page lives in
`internal/app/settings_lsp.go`; its mechanics are documented in
`docs/settings.md`.

The status row has an `LSP` pill for the active file. Its states follow Fresh's LSP
indicator, in Fresh's priority order, with gvim's wording where Fresh's is less precise:

| Pill | Meaning |
|------|---------|
| `LSP` | the file is open on its language server |
| `LSP …` | the server is starting (Fresh shows a spinner in this state) |
| `LSP (off)` | a server is configured for the language, but none runs: switched off, muted, dormant (`auto_start: false`), started by hand and stopped, or turned off for this buffer |
| `LSP (missing)` | the server's program is not installed (Fresh has no state for this: it reports `LSP (error)` only after a failed start) |
| `LSP (failed)` | the program started and failed its handshake (Fresh's `LSP (error)`) |
| `LSP (n/a)` | this buffer is skipped (a large file, or LSP off for the buffer) while the language's server runs for another open buffer (Fresh's `LSP (n/a)`) |

A language with no server shows no pill.

Nothing is started when the program is missing. The first time a file of that language opens, the
status row says what is missing and how to get it, once per language and session:

```
gopls is not installed (the language server for go): go install golang.org/x/tools/gopls@latest
```

Run an LSP command on that language (hover, go to definition, rename, **Show LSP Status**) and the
same message comes back. After you install the server, run **Start/Restart LSP Server** and gvim
looks again.

The `PATH` search and every start run in a command, never in the update loop, so a slow disk or a
slow server cannot freeze typing.

## The server status popup

Click the `LSP` pill, or run **Show LSP Status**, for a popup of every configured server: its
name, language (or "universal"), state, and — per server — a **Restart** or **Stop** row (a
stopped or dormant server gets **Start**). A failed start shows the reason under its row, and a
server that configures `process_limits` shows what happened to them (see
[Process limits](#process-limits)).

The footer of the popup mutes or unmutes the language of the active document: **Mute LSP for go**
starts no server of that language, stops the ones it has, and dims the pill to `LSP (off)`
until the mute is lifted from the same popup, or **Start/Restart LSP Server** runs one by hand. A
mute lasts for the session.

The rows of the popup say what the configuration says: a server the user has not started is
"not running", a server that failed is "error", a live one is "ready". A server started by hand
counts as the user having opted in: it overrides `lsp_enabled`, `enabled` and the mute.

## Built-in servers

The program, and how to get it. A language that gvim's syntax detection names differently (for
example `C++` for `cpp`) uses the same entry.

| Language | Program | Install |
|----------|---------|---------|
| rust | `rust-analyzer` | `rustup component add rust-analyzer` |
| go | `gopls` | `go install golang.org/x/tools/gopls@latest` |
| typescript, javascript, tsx, jsx | `typescript-language-server --stdio` | `npm install -g typescript-language-server typescript` |
| python | `pylsp` | `pip install python-lsp-server` |
| java | `jdtls` | `brew install jdtls` |
| zig | `zls` | `brew install zls` |
| latex | `texlab` | `brew install texlab` |
| markdown | `marksman server` | `brew install marksman` |
| c, cpp | `clangd` | `brew install llvm` |
| kotlin | `kotlin-lsp --stdio` | `brew install JetBrains/utils/kotlin-lsp` |
| html, css, json, jsonc | `vscode-*-language-server --stdio` | `npm install -g vscode-langservers-extracted` |
| yaml | `yaml-language-server --stdio` | `npm install -g yaml-language-server` |
| toml | `taplo lsp stdio` | `brew install taplo` |
| lua | `lua-language-server` | `brew install lua-language-server` |
| bash | `bash-language-server start` | `npm install -g bash-language-server` |
| csharp | `csharp-ls` | `dotnet tool install -g csharp-ls` |

The rest of Fresh's table is there too: ruby, php, dart, nushell, solidity, terraform, cmake,
protobuf, graphql, sql, vue, svelte, astro, tailwindcss, nix, swift, scala, elixir, erlang,
haskell, ocaml, clojure, r, julia, perl, nim, gleam, racket, fsharp, templ, typst, c3 and slang.
`lsp/manager/defaults.go` is the whole table, with the install hint of each.

Verilog and SystemVerilog use `svls` (`cargo install svls`), and the assembly languages (asm, gas,
NASM) use `asm-lsp` (`cargo install asm-lsp`).

A `.h` file is opened as C++, so clangd reads it as C++, when its directory holds a C++ source or
header: `.cc`, `.cpp`, `.cxx`, `.C`, `.c++`, `.hpp`, `.hh` or `.hxx`. That is Fresh's rule for the
sibling files. Fresh also looks for a C++ `compile_commands.json` in the parent directories, which
gvim does not, so a header with no C++ sibling is opened as C.

Two servers do not start on their own, as in Fresh: `ols` (odin), which most platforms have no
binary for, and the Godot server (gdscript), which needs a running Godot editor. Turn one on with
an entry that has no command, or start it by hand (see below):

```json
{ "lsp": { "odin": { "enabled": true } } }
```

### Project roots

Each built-in entry has Fresh's `root_markers`: the files that mark a project, such as
`Cargo.toml` for rust, `go.mod` for go and `package.json` for typescript. A server runs in the
nearest directory above the file that has one of them (the name, or a glob such as `*.csproj`), so
a crate in a subdirectory of the folder you opened gets its own server root. The walk stops at the
folder you opened, and a file with no marker above it uses that folder, as does a language with
no markers. An `lsp` entry may set `root_markers` for its language. With several servers for one
language the first server's markers pick the root.

## Changing or turning servers off

Entries of the `lsp` key in `~/.config/gvim/config.json` are laid over the table. The key is the
language ID shown in the status row.

```json
{
  "lsp": {
    "python": { "command": "pyright-langserver", "args": ["--stdio"] },
    "go": { "command": "gopls", "args": ["-remote=auto"] },
    "rust": { "enabled": false }
  }
}
```

- A `command` replaces the program of the language, and `args` its arguments. `args` without a
  `command` keeps the built-in program.
- A list gives the language several servers (see above); giving the key once replaces the
  built-in entry, and each server object is laid over the built-in default, so an entry that only
  says `"enabled": true` runs the built-in program.
- `"enabled": false` turns a server off. A language whose servers are all off gets no server.
- `"auto_start": false` keeps the server dormant: it does not start when a file opens, but
  **Start/Restart LSP Server** runs it. `"auto_start": true` (or `"enabled": true`) on a manual
  server such as `ols` starts it on open.
- A language that is not in the table needs a `command`, and then runs like the others.
- `"lsp_enabled": false` at the top level turns every language server off, the entries of `lsp`
  included. The LSP commands then say so.
- `universal_lsp` runs a server alongside those of every language; it is a separate key so a
  config never has to repeat a server per language.

A server that is off for any of these reasons can still be started by hand, as in Fresh: with the
active file of that language, run **Start/Restart LSP Server** from the palette, or **Restart Server** in the LSP menu. That
starts the server for the language for the rest of the session, whatever `lsp_enabled`,
`"enabled": false`, `"auto_start": false` or the manual servers say.

### Who may choose the program

Only your own config file, `~/.config/gvim/config.json`, can name a program to run. A project's
`.gvim/config.json` comes with the code you opened, and opening a cloned repository must not run
whatever it names. Fresh gates language servers behind a workspace-trust prompt; gvim takes the
simpler rule: in a project file the `lsp` key can switch a language off or turn a built-in server on
(`enabled`, `auto_start`), name its `root_markers` and route its features (`only_features`,
`except_features`, `language_id_overrides`), but a field that chooses or shapes the program
(`command`, `args`, `name`, `env`, `initialization_options`, `process_limits`) is refused with a
message that says to set it in the user config, and your own entries stay as they are. A project
file's `lsp` entries are added to yours; they never replace them. The project layer cannot set
`universal_lsp` at all: a universal server would run for every buffer. The programs of the
built-in table are found on your `PATH`, as Fresh does.

Workspace trust adds a second gate above this rule: the whole project layer, this key included, is
read only when the workspace is Trusted (see [workspace trust](trust.md)). An untrusted workspace
also starts no language server on its own — `lsp_enabled` is forced off for the session, so the
status row shows `LSP (off)`; **Start/Restart LSP Server** still starts one, as Fresh's manual
start does in a restricted workspace.

While a server is off or missing, the editor works as before: syntax colors, completion of the
words in the buffer and everything else do not depend on it.

**Buffer-word completion.** **Show Completions** (`Ctrl+Space`) works without a language
server. It offers the words already in the buffer: the ones that start with the word before
the caret, ranked by proximity to the caret, then by being visible on screen, then by how
often they occur, with smart-case matching (an uppercase letter in the prefix matches
case-sensitively; otherwise the prefix's own casing is preferred) and the language's
`word_characters` respected. The scan is bounded (512 KiB of lines around the caret; 2-rune
minimum, 40 candidates) and runs in a command, never on the update loop. When a server is
running, its items come first and the buffer words follow in the same popup; a word whose
insert text the server already offered is dropped. While a server is slow the list is
discarded if the document version or the caret changed before the reply arrived, so typing
never shows a list for older text. In a large-file buffer the words are not offered yet (their
lines would have to be read from disk); that is a documented residual.

Auto-popup is ported: with `editor.completion_popup_auto_show` on, typing a word character
schedules a debounced popup after `editor.quick_suggestions_delay_ms` (default 150 ms; needs
`editor.quick_suggestions`). Any non-word character, Esc, accepting, or a document/caret change
cancels it, and there is no popup when the word prefix is empty. A character the running server
advertises through `completionProvider.triggerCharacters` (the static capability and dynamic
`client/registerCapability` registrations are both read) opens the list immediately with no
debounce when `editor.suggest_on_trigger_characters` is on; the trigger set is the union over the
servers that serve completion for the document. Completion inside strings/comments is not
suppressed (Fresh triggers there too).

An open completion list is live-filtered: typing further word characters inserts them and
narrows the rows that label or insert text starts with the longer word prefix, case-insensitively,
without asking the server again, and a row without a server edit range gets one covering the typed
prefix so accepting it replaces that prefix instead of duplicating it. Backspace while the list is
open deletes one character through the normal editor action and re-filters the complete stored
candidate pool (the server items plus buffer words, not just the rows currently visible) by the
shorter word prefix, again without asking the server, so a candidate a longer prefix hid reappears
when Backspace widens the word; the synthetic edit range is recomputed for the shorter word, so
accepting replaces only the text still in the buffer. The highlighted row
stays on the same candidate across typing and Backspace (a list re-filter keeps the candidate's
former index as its identity, since labels repeat across auto-import candidates), and falls back to
the first row only when the new prefix filters that candidate out. If typing empties the filter the
list dismisses and completion is asked once for the new prefix; if Backspace's wider filter somehow
empties it, the list dismisses without a new request (Fresh dismisses there too). Non-word
characters are NOT filter characters, which matches Fresh: its completion popup sends a filter key
only for alphanumeric or underscore characters and lets every other printable key pass through and
dismiss the popup; the Fresh e2e cases (space, semicolon, parentheses, equals, braces, comma, plus,
dot) all assert the popup closes and the character is inserted. gvim does the same here. A
non-word character that a server advertises as a completion trigger still closes the old list and
is inserted, but when the completion_popup_auto_show and suggest_on_trigger_characters options are
both on it immediately starts a FRESH completion request for the new caret and opens the new list
(a new request, so the old list's selected candidate identity does not carry over); with
suggest_on_trigger_characters off it starts no request. Any other non-word character cancels a
pending debounced auto-popup. Stale replies are still dropped by the document version/caret
guard, and a re-filtered list captures the document version after the edit, so accepting it can
never apply an item against older text.

Multi-buffer words are ported for the popup: a bounded window from the middle of every other open
tab (64 KiB each, at most 8, in tab order rather than Fresh's MRU order) contributes words below
the active buffer's, and a word the active buffer already has is not duplicated. Dabbrev cycling
(`dabbrev_expand`, emacs M-/) already existed in gvim: it completes from earlier words of the
active buffer, cycles on repeated presses with smart case and a bounded scan; Fresh's multi-buffer
dabbrev (other buffers after the active one) is not ported, a documented residual.

**Accepting a language-server completion.** A server item is inserted with everything the
server asked for in one edit: its snippet text is expanded, the import edits of an
auto-import candidate (`additionalTextEdits`) are applied together with the insert, and an
item whose server fills those in lazily is resolved first with `completionItem/resolve` in
a command (the popup never blocks on it). Snippets are the subset Fresh expands: tabstops
(empty or with a default), choices (the first one), the final cursor stop, and the
`\$`, `\\`, `\}` and `\|` escapes; the caret goes to the final stop, and a later
final stop wins. gvim advertises `snippetSupport` and tracks `insertTextFormat` (Fresh
deliberately does neither and only heuristically detects snippets) and advertises
`resolveSupport` for `additionalTextEdits` + `documentation` (only the edits are used;
documentation is not rendered). The insert and every additional edit are applied as one
atomic edit guarded by the document version captured when the completion was asked and by
an overlap check: if the document changed, or the main edit and an additional edit overlap,
nothing is written and the status row says why ("completion is stale…", "completion edits
overlap…"). A resolve reply is applied the same way; if the document changed while it was in
flight, it is refused instead of editing newer text. Not ported from Fresh: tabstop
navigation (Fresh does not navigate either — it inserts defaults and jumps to the final
stop), `labelDetails` rendering (Fresh shows the import path from it; gvim shows
label/detail only), documentation rendering, multi-cursor insertion (gvim has a single
caret), and the request-id bookkeeping Fresh keeps for a completion that is re-asked while
open.

## What the servers add

- **Diagnostics.** The `{diagnostics}` status element counts the diagnostics of the active
  file, as Fresh's element does: `[E:1 W:2 I:3]`, errors, warnings, and info and hints
  together, each part only when its count is above zero. The counts merge the diagnostics of every
  server that published for the file. Fresh's default element lists put the badge on the left after
  `{cursor}` (see [the status bar](ui.md)); `editor.status_bar` can move it to any side or
  position. The counts are the stored diagnostics of the file, so they stay on the row while the
  pill says the server is missing, failed or off, and they go when the server is stopped, as
  Fresh's do. While the server runs with no count known yet the element keeps a blank seven-cell
  slot, so its neighbours do not jump when the first diagnostic arrives; when the row is too narrow
  the element is elided with the rest of the left group's tail.
  Each affected line has a dot in the gutter, in the color of its worst severity, and the
  offending text is underlined. The end-of-line message (`editor.diagnostics_inline_text`) shows
  the line's most severe diagnostic at the right end of the line. A message whose server published
  it for an older version of the document than the server now has is drawn **dimmed**: the text has
  moved on since the server found it, so the message is out of date until the next publish.
  **Jump to Next Error** and **Jump to Previous Error** move to the next and previous error or
  warning and show its message in the status row. They are `F8` and `Shift+F8`, as in Fresh; the
  macOS keymap uses `Ctrl+'` for the next error, and the emacs keymap has no key for them. The
  command palette runs them in every keymap, and **Keybinding Editor** changes the keys.
- **Hover** shows the diagnostics under the caret, with their source, above what the server says
  about the symbol. A line wider than the popup is wrapped over several rows, never cut.
- **Go to definition, implementation, references, completion, rename, code actions, signature
  help.** Rename edits every file the server returns. A file that is not open opens in a new tab,
  edited and left unsaved, like Fresh. Save it with Save All.
- **Code actions.** The popup lists every action of every server that serves code actions for the
  language, with the server that offered it and its kind (`quickfix`, `refactor.extract`, …), and
  marks the action the server recommends. The diagnostics overlapping the caret are sent as
  context, which servers such as clangd and eslint need before they offer a quick fix. Choosing an
  action applies it one of three ways: an action with an edit applies the edit; an action with a
  command runs the command on the server; an action that has neither is resolved first
  (`codeAction/resolve`), and the resolved edit and/or command is applied. An action with both an
  edit and a command applies the edit first and then runs the command.
- **Workspace edits** (a rename, a code action, or a server's own `workspace/applyEdit`) are applied
  by one multi-file helper, in the order the server sent the `documentChanges`: a file operation
  and a text edit are applied in sequence, so a created file's text edit lands on the created file
  and a rename happens before the edits of the renamed path, as the protocol requires. Text edits
  of open documents are applied as one undo step each, with a version check read when the step
  runs: a text edit carries the document version the server computed it against, and gvim skips an
  edit whose document has moved on (the rest of the edit still applies). Files that are not open
  are read, opened as tabs, edited and left unsaved; the tab you were in becomes active again when
  the apply ends. A server-requested **delete is refused**: the request carries no user
  confirmation and leaves no trash to recover from, so gvim never deletes a file for a language
  server (Fresh's rule). A **create** or **rename** onto an existing file is refused for the same
  reason, and so is a failed operation. A refused or failed file operation **stops the rest of
  that edit**: the changes before it stay applied, the ones after it are reported as not applied,
  and the status row says the edit was only partly applied with the count. gvim is deliberately
  stricter here than Fresh, whose `apply_workspace_edit` continues with the remaining operations
  after a refused one and only counts the refusal; stopping means an edit whose file operations
  failed never applies later text edits to a state the server did not expect. Nothing is ever
  reported as success when part of the edit was refused.
  A server may also ask for an edit on its own, usually while a command runs
  (`workspace/executeCommand` → `workspace/applyEdit`). gvim answers that request immediately —
  the edit is queued, the server is told it was applied, and the editor applies it a moment later —
  so the command's own answer cannot deadlock behind the UI. The side effect is that the server
  knows the edit was accepted before it actually lands; if the edit is then refused (a stale
  version, a refused delete) the server has a success it cannot see doubted. The refusal is
  reported in the status row.
- **Inlay hints.** A server's inlay hints — parameter names, inferred types — are inserted
  **inline at the protocol position** (tier 2): the hint's UTF-16 character is converted to a rune
  column of its line, its cells are spliced into the display row before that character (at the end
  of the line when the position is the line's end), and the text after them moves right on screen.
  The cells are display-only and faint: they are not text of the document, so copy, edit, undo and
  save never see them, and the caret, the mouse, selection, search and diagnostics keep the line's
  own columns (a caret at the hint's anchor sits after the hint cells, as in Fresh; a press on a
  hint maps to the character it annotates). Fresh's spacing is used — a hint inside a line is
  followed by a space, one at the line's end is wrapped in spaces — and the protocol's
  `paddingLeft`/`paddingRight` add one more space on the named side. As in Fresh there is **no
  hiding on the caret's line**: hints show while the toggle is on. A wrapped line chooses the row
  its insertion point is on, and a point on a row boundary starts the next row. A hint whose row
  has no room is cut safely (never splitting a wide character, never pushing text past the pane)
  and the rest fall back to the end of the line.

  The end-of-line fallback remains for the cases the display row cannot take the cells: the page
  view (the composed columns are not the line's own), a tall line of a very long wrapped line, a
  line shown as a window of a large file, a row panned by horizontal scrolling, a row with no
  room left, and an insertion point inside a concealed range. Hints refresh after an edit
  (debounced), when a document opens, and the result is cached per document version. **Toggle
  Inlay Hints** (the command palette; `toggle_inlay_hints`) turns them on and off; they are on
  only when the server advertises `inlayHintProvider`. A server that does not serve them says so
  once, and the refresh stops for that language.
- **Semantic tokens.** A server's semantic tokens are painted as extra styling over the syntax
  colors, through the same theme keys the syntax highlighter uses, so no theme change is needed.
  The token type picks the color (keyword/modifier; function/method/macro/decorator;
  type/class/struct/interface/namespace/enum; variable/parameter/property/enumMember/event/label;
  number; string/regexp; operator; comment), and the modifiers adjust the face: `deprecated`
  takes the warning color, `declaration` is bold, `readonly` underlined and `async` italic. The
  client asks for the whole document, then for a delta when the server advertises
  `semanticTokensFullDelta`; the visible lines are painted, with a 20-line padding so a small
  scroll does not need another request. Token spans are overlays: they never change the saved
  text, they are dropped when the
  document closes or the server restarts, and an edit invalidates the cached token data before
  the next request. Plugin decorations paint above them.
- **Folding ranges.** A server's `foldingRange` results are merged into the document's fold set
  with the indentation folds: the fold gutter marks them, and the fold actions (toggle, hidden
  lines, the caret's skip over a collapsed fold) treat both alike. The merged set is found again
  when the text or the server ranges change; ranges past the end of the document are cut. This
  also gives the Contents sidebar a fold source for its `fold_buffer` setting later. The refresh
  happens after an edit (debounced) and on open; a server that does not serve folding ranges is
  asked once per language.
- **Diagnostics, pull.** A server that answers `textDocument/diagnostic` instead of publishing is
  asked for the document's diagnostics when no server published any: a server that published is
  never also pulled from, so a problem is reported once. Pulled sets are cached for the document
  version, so the 300 ms diagnostics poll does not ask again while the text has not changed.
- **Diagnostics, workspace.** A server that advertises `workspaceDiagnostics` in its
  `diagnosticProvider` is asked `workspace/diagnostic` for the whole workspace, so files the
  editor never opened get diagnostics too. The pull is throttled to once every 5 seconds; a
  capability change (a dynamic registration or unregistration, or a server's
  `workspace/diagnostic/refresh` request) or a change in the set of open documents asks again
  sooner. The workspace answer merges into the diagnostics panel's store with its source marked:
  a file the servers published for keeps its published set (merging both would report the same
  problem twice), and a workspace-only file is added with the source `workspace` and capped at
  50 diagnostics, most severe first, with exact duplicates removed. The panel and the fix list
  mark those files `(not open)`, and choosing one opens the file at the diagnostic. Each pull is
  bounded by the request timeout, and its request is **cancelled on the wire**: the request goes
  through a path that sends `$/cancelRequest` with the request's own id when its context is
  cancelled, so a pull that a newer pull supersedes, or one in flight when the servers stop,
  restart, are muted or a server is stopped/restarted, is cancelled on the server instead of
  being left to run. A cancelled pull is never cached and never merged (the last good workspace
  answer is kept and the next poll asks again), so a late answer cannot land in the store; the
  request timeout still bounds a server that ignores the cancellation. The server-initiated
  refresh request simply makes the next poll ask again.

## The diagnostics panel and the fix list

The diagnostics of every file a server reported on — open files and files it reported without
them being open — are collected in one workspace store. The 300 ms diagnostics poll replaces the
whole store each time, so a diagnostic a server dropped disappears, and stopping the servers
empties it; the panel and the list read the same store.

**Toggle Diagnostics Sidebar** (the palette, `toggle_diagnostics_panel`) shows the native
**Diagnostics** section of the sidebar. Its header carries the workspace counts (`E:1 W:2 I:3`,
the parts of `{diagnostics}`), or an ellipsis while the servers are still starting or the first
diagnostics are pending. The body lists each file with its per-file counts, then that file's
diagnostics most severe first, each with its severity mark, message, source (and the server's
diagnostic code when it sent one) and line:column. The plugin API's diagnostic info is separate:
it carries severity, line/column, message and source but not the code, so the bundled
diagnostics-panel plugin's rows show those fields. Up
and Down move the selection, PageUp/PageDown a page, Home and End its ends, Enter opens the
selected diagnostic's file at its position and gives the keyboard back to the editor, and Escape
does the same without jumping. A click on a row selects and jumps; a click on the header folds
the section. F6 reaches the section after the Contents section, matching its place in the column.
The section has no close button: the header click folds it and the palette row closes it.

**Diagnostics List** (`open_diagnostics_list`) opens the same diagnostics in the shared filterable
list overlay — the quickfix view: type to filter, choose a row to jump to it, and the list stays
open after a jump, like a fix list. Escape closes it.

The panel's empty states are its progress display, and so are the servers' own `$/progress`
reports. When a server begins a progress token (`window/workDoneProgress/create` plus a
`$/progress` `begin`), the first unfinished entry is shown in the section header after the counts
(`Diagnostics · gopls: Indexing — crates 42% +1 more`) and in the empty state
(`In progress: …`); `report` updates the message and percentage and `end` removes the entry, so
the text goes when the work finishes. At most 8 entries are kept per server and 8 across the
workspace, oldest dropped; they are cleared when the servers stop or restart. Without progress,
the empty states are "No language server is configured", "Starting language servers…" while a
server starts, "Waiting for diagnostics…" when the servers are on but nothing has arrived, and
"No diagnostics" otherwise. The status row keeps its own `LSP …` starting state; there is no new
animation anywhere.
- **Format Buffer.** It formats with an external formatter when there is one, and otherwise asks
  the language server (`textDocument/formatting`). The caret stays on its text when the formatter
  adds or removes lines above it. With neither a formatter nor a server, the status row says which
  is missing, for example:
  `Format Buffer: no formatter is set for zig (set formatters.zig in config.json), and zls is not installed ...`.

  The external formatters are Fresh's table, used when the program is on `PATH`: `gofmt` for go,
  `rustfmt --edition 2021` for rust, `ruff format` for python, `clang-format` for c and c++ and
  `prettier` for javascript, typescript, json, jsonc and yaml. `gofmt` ends the file with a newline,
  which a plain save does not (Fresh's `ensure_final_newline_on_save` is off by default). The
  `formatters` key of your config replaces the default for a language, by name or by extension, and
  `$FILE` in its command is the quoted path of the buffer:

  ```json
  { "formatters": { "go": "gofumpt", ".md": "prettier --stdin-filepath $FILE" } }
  ```

## When a server fails

A server that starts but fails its handshake is reported with its name, the command line and the
last lines it wrote to its error output, instead of a bare `initialize: EOF`:

```
gopls (language server for go) failed to start: lsp: initialize: EOF [command: gopls serve] [it said: flag provided but not defined: -serve]
```

gvim does not start it again for that language until **Start/Restart LSP Server**, and the status
popup shows it in the error state with the same reason.

Servers only start for files that have a name on disk, and not for files in large file mode (local, or a remote large file in windowed mode). A remote large file's save is independent of any language server: the overlay is merged and streamed through the authority's atomic write stream (or the bounded whole-file fallback), so a large file with no server still saves with bounded memory.

## What is not here yet

The workspace store lists the files the servers reported on: the `workspace/diagnostic` pull is
capability-gated, so a server that only publishes reports the files it chooses, and no
workspace-folder-wide request is made to a server that does not advertise it. Inlay hints are
inserted inline (tier 2); the end-of-line form remains only as the fallback listed with them (page
view, tall lines, windowed lines, panned rows, rows without room, concealed insertion points).
No built-in universal language server ships: Fresh's quicklsp is a separate, user-installed Rust
binary (its default `universal_lsp` config names `command: "quicklsp"` with `enabled: false`,
and there is no quicklsp crate in the editor sources), so it is a new server rather than editor
wiring. gvim supports user-configured `universal_lsp` servers fully (config merge, manager
defaults, status listing and tests); the universal-server item is a deliberate recorded residual,
not a missing integration. `process_limits` is applied on Linux and, on macOS and Windows, is
explicitly recorded as unreachable in the server status (see
[Process limits](#process-limits)). The per-OS limitation and the universal-server decision are
the remaining R7-5 residuals. See docs/parity.md, backlog #7.
## The project environment

A language server starts with the active project environment applied: the host adds
the activation overlay to every server process, after the server's configured
`env` (a variable the server config names wins). Activation is trust-gated, so a
server never inherits a snippet from an untrusted workspace; start an environment
with the `env-manager` plugin (off by default) or the `gvim.env.set` plugin API.
