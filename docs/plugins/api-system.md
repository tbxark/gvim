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
* `write` with `exclusive = true` is a **create-new**: it fails with `conflict` (and leaves the
  existing entry untouched) when the path already exists as a file, directory or symlink, so a
  config file cannot be silently overwritten after an existence check raced another process. It
  cannot combine with `append` or `parents`, ignores `atomic` (the target is created directly, not
  renamed over), and uses the workspace authority's optional `FileCreator` capability on a remote
  workspace; a backend without that capability returns `unsupported` instead of falling back to a
  racy check-then-write. A failed create-new removes the file it just created.
* `copy` of a single file refuses a source that is not a regular file (a FIFO, a device) with
  `invalid`, as `read` does. Both copy forms canonicalize the source and destination before the
  first write: a destination that is the source or lies inside a copied directory tree — directly
  or through a symlinked/case-aliased parent — is refused with `invalid`; an unresolvable or
  dangling destination symlink is also refused rather than guessed. The local backend compares
  real paths and file identity (`os.SameFile`), so hard links, symlink components and case
  variants cannot bypass the check; a bounded tree walk is never used as a substitute for it.
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

### Remote workspaces

In a remote SSH workspace (a direct ssh:// launch or a daemon-hosted one) `gvim.fs` and `gvim.proc` follow the workspace: reads, writes, stats, listings, mkdir/remove/rename, watches, one-shot runs and stdio processes go through the workspace authority, so a plugin edits and spawns on the remote host and never on the local machine. Relative paths are resolved against the remote workspace root; the workspace trust gate is consulted
before every remote process launch (an untrusted workspace refuses plugin processes), and the
authority's own trust gate, working directory and environment apply to every process launch. If the authority backend has no file-mutation surface, mkdir/remove/rename return a clear `unsupported` error rather than falling back locally. `walk` and `copy` are implemented over the authority's bounded `ReadDir`/`ReadFile`/`WriteFile` surface and never touch the local machine: `walk` is depth-first in lexical order with the local hidden/skip-dirs/max-depth/max-entries semantics (symlinks are reported, never followed; unreadable subdirectories are skipped; cancellation returns `cancelled`), and `copy` copies one regular file or a directory tree remote-to-remote, refusing symbolic links in a tree (there is no remote readlink, so copying them as links is impossible and silently dropping them would be wrong). When the authority exposes a copy-safe stream, each file is copied in bounded 1 MiB `ReadRange` windows: memory stays one chunk, there is no 16 MiB file cap, and cancellation is checked between chunks. Two proof-backed surfaces exist, and the copy refuses a backend with neither before reading source data or touching a target:

* **Local authority — no-follow replace.** `WriteStreamNoFollow` writes a temp and atomically renames it over the destination path. It never follows a final link; an existing destination (file or link) is atomically replaced. Ordinary `WriteStream` keeps the save behavior of following a final link.
* **SSH authority — new target only.** `WriteStreamNewTarget` streams into a private `mktemp` file in the destination directory and publishes the complete file with the POSIX `link` utility (exact target, no `-f`) as one hard link. The destination must not exist: an existing file, directory, symlink or dangling symlink gives `conflict` and is left untouched; a creator that wins concurrently gives `conflict` and its file is untouched. The implementation never opens, follows, replaces or deletes the target; abort/failure removes only its own temporary file. Before streaming, a private probe in the destination directory must prove exact-target no-clobber hardlinks (`link` utility present; a second link, a symlink target and a directory target all fail EEXIST without being followed); if that cannot be proven the copy returns `unsupported` before source data is read. `link` is used rather than `ln` because `ln` treats an existing directory target as a directory to put the new name into, while the exact-target link utility reports EEXIST. Existing destinations are therefore unsupported on SSH (there is no no-follow replace there). A tree copy needs `DirectoryCreator` plus this new-target stream, and the destination parent must already exist so the private probe can run before the root claim; otherwise it is `unsupported` before any mkdir/write.

Residuals of the SSH new-target path (audited): (a) **Temp cleanup is best-effort, not guaranteed.** Commit transport errors/cancellation and a failed `rm -f` after a successful link can leave the private `.gvim-*` temp; a killed probe can leave `.gvim-lnprobe-*` files/directories. The target itself is still complete and this path never deletes a target. (b) **Parent-path TOCTOU is not closed.** Canonicalization/probe are a pre-write proof only: if a parent directory is renamed or replaced by a symlink after the proof, `link` resolves under the new parent and can create the new target in an unexpected directory (never over an existing target); there is no dirfd/openat containment recheck. (c) **The probe cache is per lexical directory and can go stale** after a replacement/mount change; a later commit fails safely or links under the current path, so it is not a permanent capability guarantee. (d) **The probe is a capability proof, not a strong atomicity proof**: if a second link fails for quota/permission/link-count rather than EEXIST the probe can still pass, but the later commit only fails with the target absent/untouched. (e) Tree proof covers the destination parent and assumes the created tree is on the same filesystem; a failure after the root claim can leave a partial tree (existing residual). (f) Tests exercise the local SSH helper shell, **not a real SSH host**; the helper depends on remote POSIX `mktemp`/`link`/`mkdir`/`chmod`/`ln -s`, and only the claim "hardlink no-clobber exact target; target complete atomically when the helper succeeds" is made — no durability/fsync or general filesystem-race guarantee. The temp file is appended by path, so a same-account process that can replace the private temp name is outside this protection.

`FileCreator` alone is not enough for either surface: it creates an empty file but gives no exclusive handle for the data write, so a create-then-`WriteFile` fallback could let another writer replace the path and then be overwritten by the copy. This port has no same-handle exclusive create+write primitive, so the only safe behavior is a clear refusal with no target created or written. With the local no-follow stream an existing destination is atomically replaced (the temp file is committed/aborted), so the hard-link boundary is safe there; the SSH link commit only ever creates a new entry. A directory tree is still bounded to 100 000 entries (`limit`), and its destination root is now claimed atomically through the authority's `DirectoryCreator` capability (one `mkdir` without `parents`, no `mkdir -p` on the destination): a directory created concurrently after the pre-check is refused with `conflict` instead of being merged into and having its files overwritten. A backend without that capability now returns `unsupported` for a directory copy instead of using the unsafe check-then-`mkdir -p` fallback that could merge into a concurrently created destination; the current local and SSH backends implement it. It also requires one of the copy-safe streams (no-follow local or new-target SSH) before the destination claim/creation because every file in the tree uses the same safe copy path; a backend without either capability is refused before any mkdir/write. `copy` also requires the authority's optional `RealPather` capability and canonicalizes the source and the destination through it before any write (a missing destination is resolved through its deepest existing ancestor; a dangling or unresolvable symlink is refused); a backend without `RealPather` returns `unsupported` rather than trusting a cleaned path or falling back locally. These checks are a pre-write proof: a concurrent symlink swap after canonicalization remains a documented residual. Parent directories are created before the claim (a benign operation), and a failure after the claim can still leave the newly created destination partially populated (recorded residual). A single-file copy keeps the local semantics of replacing an existing regular file (via the atomic streamed temp/commit); a directory at the file target is refused. A source that shrinks during the copy fails with `io` and leaves the destination untouched; growth beyond the size observed at the start is not copied (recorded residual). `realpath` resolves symlinks on the workspace machine through the authority's `RealPather` capability: the SSH backend runs one bounded `realpath` (or `readlink -f`) helper over the existing carrier and returns a canonical absolute remote path; the local machine is never consulted. It requires the remote host to have one of those tools (otherwise a clear `io` error, never a guessed cleaned path), requires the helper's answer to be absolute (`realpath: unexpected answer` otherwise), and honors cancellation. A missing final component — and a broken symlink — is `not_found` on both tools: the helper checks the resolved path's existence explicitly because GNU `readlink -f` can report a path that does not exist, so the remote API matches the local `EvalSymlinks` semantics instead of returning a guessed path. File names that end in LF or CR, or consist only of whitespace, are legal and preserved byte-exactly: only the helper protocol's single trailing newline is stripped, never name bytes. A symlink-loop/permission error keeps the tool's message with `io`. Fresh has no remote canonicalize in this backend, so this is a gvim extension. A remote copy does not preserve directory modes, mtimes or other metadata, and a cancelled/failed tree copy can leave a partial destination (as a local interrupted copy can); both are recorded residuals. `gvim.dirs`, `gvim.env.get`, lookpath inside `gvim.proc` and the clock stay **local-machine** APIs: they describe the machine gvim itself runs on, as documented for `gvim.dirs` and `gvim.env`. Remote `gvim.fs.watch` is the authority's polling watch (~500 ms per poll), not the local fsnotify watcher; a remote workspace with plugins shows a startup notice saying so. A local workspace is unchanged: the plugin OS is the same implementation as before.

## gvim.url

```text
gvim.url.open(url) -> true | nil, err
```

Opens an absolute http(s) URL in the user's desktop browser. Only `http` and `https` are accepted;
every other scheme (`javascript:`, `file:`, `data:`, `ssh:`, `mailto:`, ...), credentials in the
URL, whitespace or control characters, a hostless/opaque form and a URL over 2048 bytes are
refused with `invalid` **before** any prompt. For an accepted URL the editor shows a modal Yes/No
confirmation naming the URL (keyboard reachable: `y`/`n`, arrows and Enter, Esc; the focus starts
on No) and `gvim.url.open` returns once the question is queued — only **Yes** starts the opener.
The platform opener (`open` on macOS, `xdg-open` on Linux) is started with the URL as one argv
element; no shell is involved and the command string is not configurable. The opener runs off the
update loop and a start failure is reported on the status row. A platform without an opener
returns `unsupported`. In a remote workspace the URL opens on the machine running gvim (the
browser is local). This is the safer equivalent of Fresh's dashboard `openUrl` (which spawns
`xdg-open`/`open` fire-and-forget with no scheme policy or confirmation).

## gvim.http

```text
gvim.http.get(url, opts?) ⏳ -> {status = 200, body = "...", headers = {...}} | nil, err
opt 800 -> timeout_ms (default 5000, clamped 100..10000)
```

A bounded HTTPS GET for plugins. The explicit user opt-in is host-enforced: the plugin's USER
config layer must contain `plugins.<name>.settings.network = true` (the project layer never grants
it), otherwise the call fails with `unsupported` before any DNS or request. The policy:

* **Only `https`.** `http://`, `javascript:`, `file:`, `data:`, `ssh:` and every other scheme are
  refused; credentials in the URL (`user:pass@host`) are refused.
* **SSRF policy.** `localhost`/`.localhost`/`.local` names and loopback, private, link-local,
  multicast, unspecified, CGNAT, documentation/benchmarking literal addresses are refused. The
  IPv6 forms are covered too: site-local `fec0::/10` and the IPv4-embedding prefixes
  (`64:ff9b::/96` and `64:ff9b:1::/48` NAT64, `2002::/16` 6to4, `2001::/32` Teredo, and the
  deprecated IPv4-compatible `::/96`) are refused as a whole, because a translator or relay can
  decode a private IPv4 target from them. A DNS name is resolved by the host itself and **every**
  resolved address must be public; a DNS failure is final (no fallback to the hostname or another
  resolver). The dialer connects to a validated literal address; the proxy from the environment
  is ignored and TLS 1.2 is the floor.
* **Redirects.** At most 3, and every hop is revalidated with the same scheme/SSRF policy; a
  redirect to `http` or a local/private target fails the call.
* **Limits.** Default timeout 5 s (`timeout_ms` 100 ms..10 s); response body at most 1 MiB
  (pre-checked `Content-Length` plus a limited reader); at most 2 concurrent requests per plugin
  and 4 per plugin runtime (the daemon's shared runtime counts as one plugin runtime). The body
  stays in memory and is never written to disk.
* **Headers.** Callers cannot set request headers at all — the host sends a fixed
  `User-Agent: gvim-plugin/<version>` and `Accept: application/json`, so no Authorization/Cookie
  can be sent. Only safe response headers are returned: `content-type`, `content-length`, `etag`,
  `last-modified`, `cache-control`, `link` and `x-ratelimit-{limit,remaining,reset}`.

The call is awaitable Work: it never blocks the update loop. Failures are returned as `nil, err`
(`invalid` for policy, `timeout`, `too_large`, `io` for network/DNS, `unsupported` when the
network setting is off); a caller that wants a silent degradation handles the error. This is the
capability behind the dashboard's opt-in GitHub PR and weather sections; there is no generic
fetch API for non-JSON/plain-text scraping, no authentication/token, and no response cache.

## gvim.dashboard

Third-party dashboard sections, the gvim counterpart of Fresh's
`dashboard.registerSection` API. The dashboard plugin consumes them; a plugin can register
sections whether or not the dashboard is currently open.

```text
gvim.dashboard.register_section(name, refresh, opts?) -> id | nil, err
gvim.dashboard.remove_section(id_or_name) -> boolean
gvim.dashboard.sections() -> { {id, plugin, name, title, key, ttl_ms, timeout_ms, priority}, ... }
gvim.dashboard.set_auto_open(enabled) -> true
gvim.dashboard.auto_open() -> boolean | nil
```

`name` is the section's stable name within the registering plugin (1..64 characters, no
NUL); `refresh` is a Lua function (see the callback contract below); `opts`:

| key | meaning | default / bounds |
|---|---|---|
| `title` | section heading | name; clipped to 120 chars |
| `ttl_ms` | minimum age before a refresh is re-run | 30000, clamped 1000..600000 |
| `timeout_ms` | per-refresh budget; the dashboard abandons the call | 8000, clamped 100..30000 |
| `priority` | ordering hint, higher first | 0, clamped -100..100 |

Limits are host-enforced: 16 sections per plugin and 64 total, and the dashboard paints at
most 8 external sections and 64 rows per section (extra rows are clipped/skipped). `register_section` returns a
numeric id for `remove_section`; a duplicate name in the same plugin, a bad name, or a cap
violation returns `nil, err`. Removing only affects the caller's own sections. Sections are
removed when the plugin unloads.

`gvim.dashboard.sections()` returns the live registrations of all loaded plugins as plain
data; `key` is the internal proxy name the dashboard plugin uses to call the provider. The
key is returned only to the **bundled dashboard plugin**; other callers receive an empty
`key` and the cross-plugin proxy refuses a dashboard section key to them (lookup returns
nil, and a direct call is rejected even if a key were guessed). The callback is never
exposed as a Lua value across states. The dashboard plugin is the only consumer: after
opening it asks for the list and invokes each due provider through
`gvim.plugins.get(plugin)[key]`. The callback's `ctx` is plain dashboard-produced data;
providers should rely only on the documented fields (`width`, `plugin`, `name`) and treat
everything else as ordinary untrusted input.

### Refresh callback

```lua
gvim.dashboard.register_section("demo", function(ctx)
  -- ctx = { width = <dashboard inner width>, plugin = "demo", name = "demo" }
  return {
    { kind = "kv", label = "branch", value = "main" },
    { kind = "text", text = "a free-form row" },
    { kind = "blank" },
  }
end, { title = "Demo", ttl_ms = 5000, timeout_ms = 1000 })
```

The callback runs as a dashboard-spawned task, so it may use awaitable APIs; it must return
a bounded array of row tables (or `nil, err`):

* `{kind="kv", label=..., value=..., color=?}` — a label/value row (label clipped to 40,
  value to 500 chars).
* `{kind="text", text=..., color=?}` — a padded line (500 chars).
* `{kind="error", text=...}` — a muted/error status row.
* `{kind="blank"}` — an empty row.

`color` is one of `muted`, `accent`, `value`, `number`, `ok`, `warn`, `err`, `title`;
unknown colors fall back to `value`. The dashboard caps a section at 64 rows and clips all
text; display strings are parsed as UTF-8 and Cc controls become spaces while assigned Cf format
marks are removed (the same Cc/Cf sanitizer used for GitHub PR titles, so bidi/zero-width marks
cannot spoof a row); invalid UTF-8 bytes become spaces and unknown row kinds are skipped. A callback error,
timeout, missing/unloaded provider or invalid result marks only that section stale/unavailable
and keeps the last successful rows with a `stale` heading note; other sections and the rest of
the dashboard continue. A provider that is unloaded/disabled is discovered as unavailable on
the next refresh. The callback's `ttl_ms`/`timeout_ms` are enforced by the dashboard; the
host registration caps are the hard bound.

### Auto-open override

`gvim.dashboard.set_auto_open(enabled)` stores a **process-scoped** override for the bundled
dashboard's ambient auto-open behaviour. `gvim.dashboard.auto_open()` returns the override
(`true`/`false`) or `nil` when unset; the dashboard plugin then uses its user setting
`plugins.dashboard.settings.show_on_empty`. The last call from any loaded plugin wins; the
value is not persisted, survives a dashboard-plugin reload, and is reset by the next gvim
process. It does not open or close a visible dashboard immediately and does not change the
user configuration; the next automatic-open decision (startup/empty editor/last file closed)
uses the override. It has no callbacks, IO or bounded resources beyond one boolean, and never
blocks the UI.

Fresh difference: Fresh passes a `DashboardContext` with `kv`/`text`/`columns`/`newline`/
`error` drawing functions and a `SectionRefresh` promise. gvim keeps callbacks in the
provider state and returns plain row data instead, so no function crosses a plugin state and
the dashboard can bound/validate/sanitize rows before painting. Fresh's `setAutoOpen` is
exposed as the session override above. Fresh's global `removeSection` (removing other
plugins'/built-ins' names), `clearAllSections` and `builtinHandlers` are not exposed; gvim's
built-in sections are fixed and third-party registration/removal is per plugin. External rows are
text only (no column layout, click actions or URL targets); section row strings are
plugin-owned text, while dashboard-generated status strings live in the dashboard plugin's
`dashboard.i18n.json`. External refreshes run sequentially in the dashboard task: a slow
section can delay the painting of later external sections up to its timeout, but it never
blocks the editor's update loop; a timed-out dashboard task is cancelled, although a
provider's own nested work may finish later. A complete example plugin is
[examples/dashboard_section.lua](examples/dashboard_section.lua).

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
gvim.diff.compare_buffers(left, right) ⏳ -> CompareResult | nil, err
gvim.diff.replace_range(buf, start, end, text, expected_version?) -> boolean | nil, err
```

`Hunk` is `{ kind = "insert"|"delete"|"change", old_start, old_count, new_start, new_count }` with
**0-based** starts. The diff is Myers' (the editor's own), computed in the background; hunk boundaries
can differ from `git diff`'s patience diff. `text` and `buffer` take up to 1 000 000 lines and 16 MiB per
side; the buffer is snapshotted synchronously and the result carries `version`, the token of that
snapshot, so you can pass it as `{version = ...}` to a decoration call.
`next_hunk(hunks, line)` is the first hunk that starts after `line` (new side), `prev_hunk` the last that
starts before it.

`compare_buffers` is Fresh's `compareBuffers`: the line diff of two live buffers **in their own
bytes**, for a side-by-side view that paints and edits from host offsets without copying text into
Lua. It rejects a binary buffer (`invalid`) and one in large-file mode, or over 1 000 000 lines or
16 MiB per side (`too_large`). The result is:

```text
CompareResult = {
  hunks = { CompareHunk, ... },      -- increasing order; empty when equal
  left_lines, right_lines,           -- lines in each buffer
  left_len, right_len,               -- each buffer's length in bytes
  left_version, right_version,       -- content version tokens, for expected_version
}
CompareHunk = { left = CompareSide, right = CompareSide, eol_only = false }
CompareSide = { start, count,       -- 0-based first line and line count (0 on an empty side)
                from, to,           -- byte range the hunk replaces, terminator included
                jump,               -- cursor byte for a jump: from, or the line before a gap
                lines = { {start, end}, ... },       -- each line's text, without the break
                spans = { {start, end} | false, ... } }  -- the differing part of a paired line
```

Offsets are absolute bytes of the corresponding buffer in the editor's model: the document is
LF-normalised, so a line break counts one byte (the same model as `b:offset_of` and
`b:byte_length`), and every offset can be handed straight to `replace_range`. A paired line with no
differing text has `false` in `spans` (not `nil`: a nil hole would leave the array without a length
for `#`); a painted span never cuts a UTF-8 character. `eol_only` is always `false` here: gvim
normalises a buffer's line endings when it reads the file, so a pair that differs only by its
terminator is equal lines and produces no hunk (Fresh reports it as an eol-only hunk). The lines
are paired positionally within a hunk, which is the fallback pairing Fresh documents when it has no
per-line ops.

`replace_range` is Fresh's `replaceRange`: it replaces the bytes `start..end` of the buffer with
`text` as **one edit and one undo step**, through the same version-guarded atomic path as
`b:edit`. `expected_version` is a version token from `compare_buffers`; when the buffer has
changed since, nothing is done. It returns `false`, changing nothing, when the buffer is read-only
or binary, when the range is outside the buffer, or when `expected_version` is stale; a closed
buffer or another real failure is an error (`nil, err`). The text's line endings are normalised to
the document's before insertion. Deleting and then inserting is two edits (two undo steps), so
prefer this one call.
