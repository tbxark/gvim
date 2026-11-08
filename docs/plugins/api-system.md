# Files, processes, time, workspace, git, language servers

Everything marked ⏳ runs off the editor's update loop and resumes the task when it is done.

## gvim.path

Pure functions on path strings, no IO.

```text
gvim.path.join(...) -> string        gvim.path.dirname(p) -> string      gvim.path.basename(p) -> string
gvim.path.extname(p) -> string       gvim.path.is_absolute(p) -> boolean gvim.path.normalize(p) -> string
gvim.path.relative(base, p) -> string | nil, err   gvim.path.split(p) -> dir, file
gvim.path.to_uri(p) -> string        gvim.path.from_uri(u) -> string | nil, err
gvim.path.sep                        -- "/" or "\\"
```

`relative` fails with `nil, err` when one path is relative and the other absolute (for example a
root-relative hit of `workspace.grep` and `workspace.root()`): join the hit to the root first.

## gvim.fs

```text
gvim.fs.read(path, opts?) ⏳ -> string | nil, err       -- opts {max = 16 MiB}; over it: too_large
gvim.fs.write(path, data, opts?) ⏳ -> true | nil, err  -- opts {atomic = true, append = false, mode = 0644, parents = false}
gvim.fs.list(path, opts?) ⏳ -> { {name, kind, size, mtime, target_kind}, ... } | nil, err   -- opts {hidden = false}
gvim.fs.stat(path) ⏳ -> { kind, size, mtime, mode } | nil, err
gvim.fs.exists(path) ⏳ -> boolean
gvim.fs.mkdir(path, opts?) ⏳ -> true | nil, err        -- opts {parents = true}
gvim.fs.remove(path, opts?) ⏳ -> true | nil, err       -- opts {recursive = false}
gvim.fs.rename(from, to) ⏳ -> true | nil, err
gvim.fs.copy(from, to) ⏳ -> true | nil, err
gvim.fs.realpath(path) ⏳ -> string | nil, err
gvim.fs.walk(root, opts?) ⏳ -> { entries = { {path, rel, kind, size, mtime}, ... }, truncated = boolean } | nil, err
gvim.fs.watch(path, fn, opts?) -> Watcher | nil, err   -- not ⏳
watcher:stop()  watcher:active() -> boolean
```

* `kind` is `"file"`, `"dir"` or `"symlink"`. `mtime` is Unix seconds as a float; `mode` is the
  permission bits. `stat` follows symlinks, and a broken link is `kind = "symlink"`.
* `write` is **atomic** by default (a temporary file, then a rename; through a symlink it writes the
  target), except with `append = true`. `mode` 0 keeps the mode of the file it replaces, else 0644.
* `copy` of a single file refuses a source that is not a regular file (a FIFO, a device) with
  `invalid`, as `read` does.
* `remove` refuses `/`, `$HOME`, the workspace root and the gvim config directory, even with
  `recursive = true`.
* `walk` opts: `skip_dirs = {".git"}`, `hidden = false`, `max_depth`, `max_entries` (default and
  maximum 50 000 / 100 000). `list` returns at most 100 000 entries. These results are converted to
  Lua on the editor loop, hence the cap.
* **Watching.** `watch(path, fn, {debounce_ms = 50})` (10 to 5000) calls `fn({path, kind})` for
  coalesced changes; `kind` is `"create"`, `"write"`, `"remove"`, `"rename"` or `"error"` (then `err`
  says why). The callback is an event-class task. A file is watched through its parent directory, so
  an editor-style atomic replace keeps reporting. A directory watch is **not recursive**. At most 32
  watchers per plugin. On macOS a directory is watched with kqueue, which needs a descriptor per
  entry: a directory over 512 entries fails with `limit` delivered to the callback, also when it
  grows past 512 entries later (the watch then ends after the `limit` event). A normal `.git`
  directory is far below.
* Paths are used as given; relative paths are relative to the editor's current directory.

## gvim.proc and gvim.env

```text
gvim.proc.run(argv, opts?) ⏳ -> Result | nil, err
gvim.proc.spawn(argv, opts?) -> Process | nil, err
gvim.proc.which(name) ⏳ -> path | nil
gvim.env.get(name) -> string | nil
```

`argv` is an array and there is **no shell**: `{"git", "status", "-s"}`. At most 1024 entries and
1 MiB in all. Options of `run`: `cwd`, `env = {K = "V"}` (added to the environment; at most 256),
`stdin = "text"`, `timeout_ms = 60000` (at most 600 000), `max_output = 8 MiB` per stream,
`merge_stderr = false`. `spawn` takes the same options (`merge_stderr` included), plus
the ones below.

```lua
local r = gvim.proc.run({ "git", "rev-parse", "--short", "HEAD" }, { cwd = gvim.workspace.root() })
if r and r.ok then gvim.notify(r.stdout) end
```

`Result` is `{ code, signal, stdout, stderr, ok, timed_out, truncated }`; `ok` is true when `code == 0`
and there was no signal and no timeout. A non-zero exit is **not an error**: `run` returns `nil,
"exec: ..."` only when the process could not start. `truncated` says an output cap was hit.

`spawn` returns a `Process` at once and starts the child in the background. Extra options:
`on_start(pid)`, `on_stdout(chunk)`, `on_stderr(chunk)`, `on_exit(result)`, `lines = false` (`true`:
one callback per line, without the newline) and `stdin = true` (open a pipe for `p:write`). Callbacks
run as event-class tasks; output is delivered in chunks of at most 64 KiB or 20 ms. At most 16 live
processes per plugin.

```text
p.pid                       -- the process id (nil until the child started)
p:write(data) -> true | nil, err        -- queued (up to 8 MiB) until the child started
p:close_stdin()
p:kill(signal?) -> true | nil, err      -- "TERM" | "KILL" | "INT" | "HUP"
p:is_running() -> boolean
p:wait() ⏳ -> Result | nil, err
p:wait_started() ⏳ -> pid | nil, err
```

On unload or reload the plugin's processes get `SIGTERM` to their group and `SIGKILL` two seconds
later; when gvim exits they are killed at once.

## gvim.timer

```text
gvim.timer.after(ms, fn) -> Timer | nil, err      -- ms >= 1
gvim.timer.every(ms, fn) -> Timer | nil, err      -- ms >= 10; re-arms after fn finished, so slow callbacks never pile up
gvim.timer.sleep(ms) ⏳ -> true                   -- ms >= 0
gvim.timer.debounce(ms, fn) -> function           -- trailing edge; the wrapper passes the LAST call's arguments to fn
timer:cancel() -> boolean    timer:active() -> boolean
```

Callbacks are event-class tasks (`min(50 ms, plugin_timeout_ms)`). At most 64 live timers per plugin
(`limit`; `debounce` raises instead of returning an error). A wait costs nothing: a task that
sleeps resumes with a fresh budget.

`gvim.timer.sleep(0)` is the way to give the editor a turn in a long loop: every slice has a time
budget, and a loop that does 2000 items of work per turn stays inside it. A loop that yields often
with no work to do (a bare `sleep(0)` spin) is throttled: its turns wait for the throttle floor and it
is warned once. It is struck only while it keeps a core busy through that floor; a loop that does real
work between its turns is throttled at most. See [limits.md](limits.md) (Timer pace).

## gvim.async

```text
gvim.async.spawn(fn, ...) -> Task | nil, err
gvim.async.all(fns) ⏳ -> { {n = ..., ...}, ... }
gvim.async.suspend(fn, opts?) ⏳ -> values passed to resume
task:cancel()   task:done() -> boolean   task:wait(opts?) ⏳ -> its results (re-raises its error)
```

* `spawn` starts a new task (it first runs on the next turn, not inside the call) and returns it.
  `task:wait` waits for it and returns its results, or raises its error.
* `all(fns)` runs up to 16 functions concurrently, waits for all, and re-raises the first error after
  every one has settled. Each result is a table with its values and their count `n`.
* `suspend(fn)` calls `fn(resume)` at once and parks the task until `resume(...)` is called, from a
  callback, a timer or an event; the values given to `resume` are returned. A second `resume` is
  ignored. `suspend` and `task:wait` take `opts.timeout_ms`: after it they return
  `nil, "timeout: ..."`.

```lua
-- turn a callback API into a call
local result = gvim.async.suspend(function(resume)
  gvim.proc.spawn({ "make" }, { on_exit = function(r) resume(r) end })
end, { timeout_ms = 60000 })
```

## gvim.workspace

```text
gvim.workspace.root() -> string
gvim.workspace.files(opts?) -> { files = {...}, loading = boolean }
gvim.workspace.grep(query, opts?) ⏳ -> { {path, line, col, text}, ... } | nil, err
gvim.workspace.git_root(dir) ⏳ -> root | nil
gvim.workspace.git_branch(root) ⏳ -> name | nil, err
```

* `root` is the workspace root now (it changes when the user opens a project).
* `files` is a snapshot of the editor's file index, no IO; `opts.limit` (default and maximum 50 000);
  `loading` is true while the index is still being built. The paths are **relative to the workspace
  root**, with `/` separators.
* `grep` searches the files of the same file index (at most the first 2000 files of the index; a file
  that looks binary or is over 1 MiB is skipped), with one hit per matching line. In a git work
  tree the index is `git ls-files` (tracked and untracked files that are not ignored: a hidden
  directory or a `node_modules` that git does not ignore is searched). Without git it is a walk
  that honours `.gitignore` and skips hidden entries and `.git`, `node_modules`, `target`,
  `__pycache__`, `.hg` and `.svn`. opts `regex = false`, `case_sensitive = false`, `max = 2000` (at
  most 20 000): the search stops after `max` hits, and there is no `truncated` flag, so a result of
  exactly `max` hits may be incomplete. `path` is **relative to the workspace root**: `gvim.buf.open` takes it as it is, but
  join it with `gvim.workspace.root()` before `gvim.fs` or `gvim.path.relative`. `line` and
  `col` are **0-based**, `col` a **byte** offset into `text`.
* `git_root` and `git_branch` read the repository with the editor's own reader, with no `git`
  binary. They are the fallback of `gvim.git.root` and `gvim.git.branch` when no binary is
  installed; prefer `gvim.git`.

## gvim.git

A Lua module over `gvim.proc.run`; `require "gvim.git"` or `gvim.git`.

```text
gvim.git.available() -> boolean
gvim.git.run(args, opts?) ⏳ -> Result | nil, err      -- args = {"rev-parse", "HEAD"} (without "git")
gvim.git.repo(path) ⏳ -> { root, head } | nil | nil, err
gvim.git.root(path) ⏳ -> root | nil
gvim.git.branch(root?) ⏳ -> name, detached | nil | nil, err
gvim.git.show(path, opts?) ⏳ -> text | nil, err       -- opts {rev = "HEAD", for_buffer = Buffer}
gvim.git.ls_files(root?) ⏳ -> files, truncated | nil, err
gvim.git.blame(path, opts?) ⏳ -> { {line, commit, author, time, summary}, ... } | nil, err
gvim.git.commit(dir, hash) ⏳ -> lines | nil, err
```

Every call runs `git -c core.fsmonitor=false -c core.pager=cat -c diff.external= -c
core.attributesFile=<null device> -c protocol.ext.allow=never <args>`, adds `--no-ext-diff
--no-textconv` to `show`, `diff` and `log` (and `--no-textconv` to `blame`), and sets
`GIT_OPTIONAL_LOCKS=0 GIT_TERMINAL_PROMPT=0 GIT_CONFIG_NOSYSTEM=1`: reading a repository never runs
a program the repository names. Without a `git` binary `root` and `branch` answer through
`gvim.workspace`, and `run`, `show`, `ls_files`, `blame` and `commit` return
`nil, "unsupported: git binary not found"`; `gvim.has("git")` tells which case you are in.

`blame` options: `from`, `to` (0-based, `to` exclusive: blame only those lines), `contents` (blame this
text instead of the file, for an edited buffer), `rev`. `show` with `for_buffer` decodes the blob with
the buffer's encoding and normalises line endings. Output is capped (8 MiB; blame 4 MiB): over it is
`too_large`.

## gvim.lsp

```text
gvim.lsp.diagnostics(buf?) -> { {range, severity, message, source}, ... } | nil, err
gvim.lsp.request(method, params, opts?) ⏳ -> result | nil, err      -- opts {buf = Buffer}
gvim.lsp.all_diagnostics() -> { {path, diagnostics}, ... } | nil, err
```

Read access to the language servers gvim runs for the user (configured in `config.json`).
`gvim.has("lsp")` is true when at least one server is configured; without one both functions return
`unsupported: no language server is configured`.

* `diagnostics(buf)` returns the diagnostics the server last published for the buffer (the current
  buffer by default), at most 5000. `severity` is `"error"`, `"warning"`, `"info"` or `"hint"`; the
  `range` has **byte columns** (the editor converts them from UTF-16); `source` is the tool
  that found it and may be absent. A buffer without a file, or one no server reported on, has none
  (an empty list). The event `diagnostics_updated` ([events.md](events.md)) says when they change.
* `all_diagnostics()` returns the diagnostics of **every open document** a server has
  reported on, sorted by path: an array of `{path, diagnostics}` where `diagnostics` has the
  same rows as `diagnostics(buf)`. At most 50 000 diagnostics in all. It is the one-call form
  for a diagnostics panel that lists every file.
* `request(method, params, opts)` sends one request of the Language Server Protocol to the server
  that has the buffer open (`opts.buf`, default the current buffer) and returns the **decoded JSON
  result** (`nil` for `null`). `params` and the result are the protocol's own JSON, so positions are
  **UTF-16 columns**, uris are `file://` uris (`gvim.path.to_uri`), and nothing is converted. The
  buffer must be a file that a server has open, otherwise `not_found: no language server has ... open`;
  the server sees the text of the buffer as the editor last synchronised it (within a third of a
  second of typing).

```lua
gvim.on("cursor_moved", gvim.timer.debounce(200, function(ev)
  local pos = ev.pos
  local res = gvim.lsp.request("textDocument/hover", {
    textDocument = { uri = gvim.path.to_uri(ev.buf:path()) },
    position = { line = pos.line, character = pos.col }, -- byte col == UTF-16 col only for ASCII text
  }, { buf = ev.buf })
  if res and res.contents then gvim.log.info("hover: %s", gvim.json.encode(res.contents)) end
end))
```

Only requests that **read** are allowed, from this allowlist (anything else is refused with `invalid`,
before it reaches a server): `textDocument/` `hover`, `definition`, `declaration`, `typeDefinition`,
`implementation`, `references`, `documentSymbol`, `documentHighlight`, `documentLink`, `documentColor`,
`colorPresentation`, `codeAction`, `codeLens`, `completion`, `signatureHelp`, `formatting`,
`rangeFormatting`, `onTypeFormatting`, `rename`, `prepareRename`, `foldingRange`, `selectionRange`,
`linkedEditingRange`, `inlayHint`, `inlineValue`, `moniker`, `diagnostic`, `semanticTokens/full`,
`semanticTokens/range`, `prepareCallHierarchy`, `prepareTypeHierarchy`; `callHierarchy/incomingCalls`,
`callHierarchy/outgoingCalls`, `typeHierarchy/supertypes`, `typeHierarchy/subtypes`; `workspace/symbol`;
and the `resolve` requests `completionItem/resolve`, `codeAction/resolve`, `codeLens/resolve`,
`documentLink/resolve`, `inlayHint/resolve`, `workspaceSymbol/resolve`. Notifications of document
synchronisation (`textDocument/did*`, `textDocument/willSave*`, `notebookDocument/*`, `workspace/did*`),
the lifecycle (`initialize`, `shutdown`, `exit`), `workspace/executeCommand` (which lets a server do
anything), server-specific methods and every method starting with `$/`, `window/` or `client/` are not
on it. Methods that *return edits* (`textDocument/rename`, `textDocument/codeAction`) are allowed;
applying the edits is up to you. A request has a 10 second limit (`timeout`), the result is capped at
4 MiB (`too_large`), `params` must be a table of plain data, and an error response of the server comes
back as `io: ...`. The wait is cancelled when the plugin unloads or the panic chord is pressed.

JSON arrays and objects in `params` and in the result follow `json.decode` and `json.encode`: an empty
Lua table is the object `{}`, so send an empty array as `json.array({})` (a strict server such as gopls
needs `context = { diagnostics = json.array({}) }` in `textDocument/codeAction`). In a result, a `null`
inside a table or list is `json.null` (so a list keeps its length; compare with `== json.null`), an
empty list is a `json.array` table, and a `null` result as a whole is `nil`. `json.null` in `params`
is sent as `null`.

## gvim.diff

```text
gvim.diff.lines(a, b) ⏳ -> Hunk[] | nil, err               -- two arrays of lines, at most 100 000 each
gvim.diff.text(a, b, opts?) ⏳ -> Hunk[] | nil, err         -- two strings; opts {ignore_eol = true}
gvim.diff.buffer(buf, text, opts?) ⏳ -> Hunk[] | nil, err  -- `text` is the OLD side, the buffer the new side
gvim.diff.saved(buf) ⏳ -> Hunk[] | nil, err                -- the saved text (old) against the current text
gvim.diff.next_hunk(hunks, line) -> Hunk | nil
gvim.diff.prev_hunk(hunks, line) -> Hunk | nil
```

`Hunk` is `{ kind = "insert"|"delete"|"change", old_start, old_count, new_start, new_count }` with
**0-based** starts. The diff is Myers' (the editor's own), computed in the background; hunk boundaries
can differ from `git diff`'s patience diff. `text` and `buffer` take up to 1 000 000 lines and 16 MiB per
side; the buffer is snapshotted synchronously and the result carries `version`, the token of that
snapshot, so you can pass it as `{version = ...}` to a decoration call.
`next_hunk(hunks, line)` is the first hunk that starts after `line` (new side), `prev_hunk` the last that
starts before it.
