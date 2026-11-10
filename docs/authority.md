# Authority (workspace backend slot)

Status: #21a plus the round-13 remote completion. The slot, the local backend, the
remote-spec parsing, the SSH backend for terminal/one-shot/long-running children, the
remote `FileSystem`, and the connection-state hooks are implemented in `authority/` and
`internal/terminal/session_backend.go`. The editor's file I/O and the language servers of a
remote workspace are routed through the slot (§6), including automatic server restart after
a reconnect and remote external-change watching (the authority watch bridged into the root's
watch pipeline); the remaining residuals are listed in §7 (remote explorer, remote large
files, reconnect retry/backoff, remote trust store, terminal respawn as an open parity gap).

## 1. The slot and its four surfaces

`authority.Authority` is the single answer to "where does this primitive run?". Consumers ask the
slot; they never branch on the backend kind. Four routing surfaces:

- `FileSystem` — `ReadFile`, `ReadRange`, `WriteFile`, `Stat`, `ReadDir`, `Watch`. The value
  types are the plugin OS port's (`plugin/hostos`), so the local adapter is forwarding. The
  tree-changing operations are a separate optional surface, `FileMutations`
  (`Mkdir`/`Rename`/`Remove`, reached through `authority.Mutations(fs)`), so a read-only
  backend can mount a `FileSystem` without them; the local and SSH backends implement both.
- `OneShot` — `Run(ctx, argv, ProcOptions)`: run to completion and collect stdin/stdout/
  stderr/exit status (git, formatters, git grep, plugin `spawnProcess`).
- `LongRunning` — `Start(ctx, argv, ProcOptions)`: long-lived stdio children. The surface
  for language servers and tool agents; no LSP is routed through it yet.
- `Terminal` — `Launch(ctx, TerminalOptions)`: how the integrated terminal starts.
  `TerminalLaunch.Backend` is nil for a local PTY (the app starts it as today) and a
  transport for a re-parented terminal (SSH); `internal/terminal.StartWithBackend` wraps that
  transport in the same `Session` the dock already drives, so tabs, scrollback, resize and
  close work unchanged.

`Authority.State()` is the snapshot the status row and reconnect-on-activate read:
`{Kind, Label, Remote *RemoteSpec, Connected, Reason}`.

## 2. Trust gate and environment

Every launch (one-shot, long-running, terminal) runs the injected `Gate` before it starts,
and the injected `EnvProvider` is merged into the process options/environment. This is the
single choke point: no spawner needs to remember to consult trust or the activated env. The
local backend applies the same rule as the remote ones.

An `EnvProvider` is merged into LOCAL children only. A remote launch never sends the
local overlay: its environment is `env.Remote`, whose marker probe and activation
recipe run on the REMOTE host through the authority's one-shot surface (a raw exec with no
env map of its own, so capture cannot recurse). Marker detection checks both detectors'
markers and their require evidence. Before the workspace is trusted the authority command
gate would refuse a shell command, so detection uses the app's shell-free authority probe:
`authority.FileSystem.Stat` for each marker/require path (no remote process, no local
filesystem, at most `env.MaxMarkerProbePaths` paths and a 10 s shared timeout), letting an
untrusted workspace still display the remote detection result read-only (activation stays
trust-gated). A trusted workspace uses the single bounded, cancellable `sh -c` existence
probe instead (nothing is executed beyond the fixed shell test and rc files are not
sourced). A failed probe clears the detection and leaves an explicit Settings notice
(never a local scan or a stale result); the trusted activation baseline/recipe runs under
the user's interactive login
shell when one is available — `bash -ilc` or `zsh -ilc`, preferring `$SHELL` when it is
bash/zsh — so a detector that depends on interactive rc initialization captures the same
environment the remote terminal gets. If no such shell is found, or the interactive
invocation fails, the capture falls back to the previous `sh -c` path and records an
explicit notice (startup notice and the Settings Environment rows). The remote env inputs are
watched through the authority's own file watch: a change to the fixed `.envrc`,
`mise.toml`, `.mise.toml`, `.tool-versions`, `pyvenv.cfg` or `.venv` inputs, or to a
user-configured detector marker/require path, invalidates the cached detection/capture and
re-warms it in the background (no local filesystem is consulted). The watcher arms the
workspace root plus the existing parent directories of the watched paths (bounded:
`env.MaxDetectorWatchPaths` marker paths and `env.MaxDetectorWatchDirs` directories; excess
paths are reported in the Settings notice and still work through `Env: Reload`). Nested
content changes are seen because the parent directory is watched even when the root
directory entry does not change; when a watched parent directory appears, the next relevant
root event re-arms the watcher with it. A runtime config Reload that changes
`env.detectors` swaps the provider's detectors (invalidating detection/capture and
re-warming them in the background off the update loop) and re-plans the watcher from the new
list; the old watches are stopped and the new plan is still bounded by the same path and
directory caps, using only authority watches (no local read). A backend whose file surface cannot watch degrades safely: the watcher stops,
the Settings Environment rows say the inputs are not watched and point at `Env: Reload`,
and the manual reload path keeps working; other watch failures retry with the same bounded
backoff as the remote document watch. After every successful
authority reconnect the app re-probes the interactive shell in the background, off the UI
update loop, and refreshes the fallback state/notice: a probe that failed at startup is no
longer pinned to `sh -c` for the session, and a later successful probe clears the Settings
fallback row. The re-probe is the same fixed pass-through command through the authority gate
(an untrusted workspace refuses remote commands, so the fallback/notice reflects that), and
the session context bounds it. The captured delta is the environment the
remote terminal, the shell/formatter runner, remote LSP and remote project search/git grep
receive. That keeps a local secret (or any local variable) out of every ssh command and
makes the remote project environment actually take effect (R13-13); Fresh's
backend-agnostic provider re-captures through the active backend the same way.

## 3. Local backend

`authority.Local(LocalOptions{OS, Gate, Env, Shell})` mounts the four surfaces over the
editor's existing plugin OS port (`internal/pluginhost`). Files and processes are byte-identical
to calling the port directly (parity tests compare ReadFile/Stat/ReadDir/Write/Run results
and errors); the terminal launch returns a local launch with no backend, so the existing
`terminal.Start` path is used. `Shell` is a probe for the local shell override; empty
leaves the app's own default.

## 4. Remote specs

Two forms, matching Fresh:

- scp-style: `[user@]host:path[:line[:col]]`
- URL-style: `ssh://[user@][host][:port]/path[:line[:col]]`

Rules (all covered by table tests):

- `ssh://` is exclusive: a malformed URL is a startup error, never a local fallback. The
  user defaults to `$USER` then `$USERNAME`; a URL with neither and no user is an
  error. The port is accepted only in URL form (1..65535). The path is required and
  percent-decoded; IPv6 hosts are bracketed.
- scp without a user needs the path to start at `/` or `~`, so a local
  `FILE:LINE[:COL]` (including `main.go:42:7` and windows-ish `C:/x`) stays local.
  Use `user@host:...` to force a relative remote path. A colon inside the path stays a
  path unless the trailing suffix is numeric (`/a/b:name:12` is path `/a/b:name`,
  line 12).
- Positions are 1-based; 0 means "not given".
- `RemoteSpec.Workspace()` is the remote root the workspace would use; `PathMapper` is
  the host↔remote translation seam (identity for SSH; a container backend will map the
  project root). `RemoteCommand`/`ShellQuote`/`QuoteDir` compose the remote command line:
  `cd <dir> && env K=V ... argv...`, POSIX-quoted, with `~`/`~user` left for the remote shell
  to expand. The env entries are the session's REMOTE captured delta (`env.Remote`),
  never the local process environment.

## 5. SSH backend

`authority.ConnectSSH(ctx, SSHOptions{...})` dials and mounts a remote slot.

- Dependency: `golang.org/x/crypto/ssh` (pure Go). v0.58.0 was already an indirect module
  and is cached; this slice promotes it to a direct `require` with no new download. The
  purity gate and `CGO_ENABLED=0` builds stay green.
- Auth order: ssh-agent through `SSH_AUTH_SOCK`; identity files (`id_ed25519`,
  `id_ecdsa`, `id_rsa` by default, encrypted ones ask the injected passphrase callback); the
  injected password callback. No usable method is a clear error.
- Host keys: `~/.ssh/known_hosts` through `knownhosts.New` unless the caller injects a
  callback (tests use `ssh.FixedHostKey`). An unknown host is an error naming the
  `known_hosts` path and `ssh-keyscan`; the slice never silently trusts. An accept-new
  mode is a follow-up.
- Connect timeout: 15 s default (`SSHOptions.Timeout`) covering dial and handshake.
- One-shot: one ssh session per command; exit status → `ProcResult.Code`, stderr captured,
  stdout/stderr split, context cancellation kills the remote process. Non-zero exit is a
  result, not an error.
- Long-running: ssh session with stdin/stdout/stderr pipes, exposed as `hostos.Proc`
  (`Output` chunks, `Wait`, `Kill`, `Terminate`). This is the future LSP route; LSP is not
  routed in this slice.
- Terminal: exec channel with `RequestPty("xterm-256color", rows, cols)`, remote command
  `cd <workspace> && [env K=V ...] "$SHELL" -l` (the remote login shell; the local
  `terminal.shell` override deliberately does not apply; the optional env prefix is the
  session's remote captured project delta, POSIX-quoted as data), stderr merged into the
  output,
  resize through `WindowChange`, transport `Backend` wrapped by
  `terminal.StartWithBackend`.
- Files: the remote `FileSystem` is mounted (`authority/ssh_files.go`): bounded `sh -c`
  helper commands over the same exec channel (a read cap, stat with GNU and BSD/macOS
  `stat`, a sorted directory listing with an entry cap, atomic temp-file+rename writes
  that preserve the target mode and write through a symlink, and a polling watch). The
  decision, assumptions and limits are in `6.
- Reconnect: `Reconnect(ctx)` closes the carrier and redials the stored target; a session
  that observes the death marks `Connected=false` with a reason for the indicator. Existing
  remote terminals are not resurrected automatically: the transcript is kept read-only and
  the user starts a shell again. Respawn of the previous terminal on reconnect is an open
  parity gap; a safe version needs a recorded, replayable launch (the login shell only, or
  an argv the user explicitly chose), never a silent replay of an arbitrary command.

## 6. Remote file I/O (landed this round)

The SSH backend mounts a real `FileSystem`: every operation is one bounded helper command
run through the existing exec channel (`sh -c` + `RemoteCommand` quoting), and nothing is
installed on the remote host. This is the task's documented fallback, chosen on evidence,
not preference: the offline module cache has no usable SFTP implementation (`pkg/sftp` has
only a `.info` file and no module tree; no network), the dependency policy is pure Go, and
Fresh's own remote filesystem needs a Python 3 agent on the host — a requirement this port
deliberately does not take.

- Operations: `ReadFile` reads through `head -c <max+1>` so an oversized file is detected
  without transferring it (the error keeps the port's "larger than N bytes" shape); `Stat`
  answers kind/size/mtime/mode, follows a live symbolic link and reports a broken link as a
  symlink (the port's fallback); `ReadDir` returns a name-sorted listing with the port's
  hidden filter and a symlink's target kind; `WriteFile` supports truncate/append, `Parents`
  (`mkdir -p`) and `Atomic`; `Watch` polls (500 ms) because a remote host has no fsnotify
  channel.
- Server-side bounded directory listing (evaluated, NOT enabled): the remote finder fallback
  cannot get a provable hard server-side bound from the portable tools. A `find -print0 |
  head -c` pass limits output/memory bytes and avoids shell-glob materialisation, but a
  kind-filtered pass may walk entries without emitting them, so it is not a provable bound
  on traversal/entries; a `-exec` per-entry marker would spawn unbounded children for
  short names, and `-printf` is not portable to BSD/macOS. The earlier candidate therefore
  ships nothing: no `LimitedDirReader` capability is advertised, the finder keeps its
  existing `ReadDir` walk and its explicit oversized-directory error, and gvim does not
  claim a server-side bounded listing. The finder's existing entry/dir caps and the
  directory listing's 5000-entry/4 MiB error remain the limits.
- Atomic saves: the atomic path creates `mktemp` in the target directory, streams stdin into
  it with `cat`, applies the resolved mode, then `mv -f` over the target: a same-directory
  rename, which is the atomicity the local port relies on. The target's existing permission
  is preserved when `WriteOptions.Mode` is zero; an explicit mode replaces it. A symbolic
  link is resolved (bounded, 16 levels) and the target replaced, so the link stays a link —
  matching the local atomic path. A non-atomic write follows symlinks like `cat` and keeps
  an existing file's permission. These exact behaviours are asserted in
  `authority/ssh_files_test.go`, not assumed.

- Streamed atomic writes: `WriteStreamer` is the optional file-port surface for saving a
  file larger than memory. `WriteStream` creates one `mktemp` in the resolved target's
  directory (symlinks resolved, bounded 16 levels), each `Write` appends its bounded chunk
  with one `cat >> temp` helper, and `Commit` applies the mode and does one `mv -f` over
  the target; `Abort` removes the temporary file. The target is untouched until the commit,
  so a failed or cancelled write leaves it exactly as it was, and the local backend has the
  same temp+rename implementation. Only atomic, non-appending, no-parents writes are
  accepted (`Append`/`Parents`/`!Atomic` report `ErrUnsupportedFileIO` instead of
  silently differing from `WriteFile`). Memory is one caller chunk plus the small helper
  answer, never the whole file; every chunk and the commit are bounded by their contexts
  (a cancelled chunk is an error, and the abort still removes the temporary file with its
  own short bounded context).
- Copy-safe streamed writes have two capability-gated surfaces, and plugin `fs.copy`
  refuses a backend with neither before reading source data or touching a target:
  * **Local no-follow replace.** `NoFollowWriteStreamer` (`authority.NoFollowWriteStreams`)
    uses the same temp-file/`Commit`/`Abort` protocol but does **not** resolve a symbolic
    link at the final destination: `Commit` renames over the destination path itself,
    replacing a link instead of following it. Existing destinations can be atomically
    replaced. Ordinary `WriteStream` keeps the save semantics of following the final link.
  * **SSH new target only.** `NewTargetWriteStreamer` (`authority.NewTargetWriteStreams`)
    streams into a private `mktemp` in the destination directory and `Commit`s with the
    POSIX `link` utility (exact target) — one no-clobber hard link, never `ln`/`mv`/rm of
    the target. A pre-write probe in the destination directory must prove exact-target
    hardlink semantics (`link` present; a second link, symlink target and directory target
    all EEXIST without being followed); otherwise the operation is `unsupported` before
    source data is read. Existing files/directories/symlinks/dangling symlinks (and a
    target that appears concurrently) are `conflict` and are never followed, replaced or
    deleted; abort/failure attempts to remove only the private temp (best-effort, not a
    guarantee). The destination parent must already exist for the pre-write probe.
    `NewTargetProver` exposes that probe so a tree copy can refuse a backend before
    claiming/creating its destination root. These are pre-write path/capability proofs,
    not a general atomicity or durability guarantee: parent-component replacement after
    the proof remains a TOCTOU hole; the per-directory probe cache can become stale after
    a mount/replacement; a probe can pass on a non-EEXIST second-link failure while the
    later commit still fails safely; temp cleanup is best-effort; and the SSH helper path is
    tested against the local helper shell, not a real SSH host. When the helper succeeds,
    the target is complete and atomically linked, and an existing target is never followed,
    replaced or deleted.
  Plugin `fs.copy` canonicalizes the destination first, so a concurrent symlink to the
  source cannot redirect the local no-follow stream (the ordinary save stream would follow
  it at open). On SSH only new destinations are supported; replacing an existing
  destination is unsupported, and the SSH no-follow replace surface remains absent. Tests
  cover the local no-follow behaviour, the SSH new-target link commit (existing/
  concurrent targets, links, directories, abort/cancel, multi-chunk) and the plugin-host
  capability selection/refusal (`authority/ssh_new_target_stream_test.go`,
  `authority/write_stream_test.go`, `internal/pluginhost/authorityos_copy_safety_test.go`).
- Exclusive create: `CreateNew` is the optional `FileCreator` surface (reached with
  `authority.Creators(fs)`) for callers that must not replace an existing entry. The SSH
  backend creates the empty file with one POSIX noclobber redirection (`set -C; : > "$p"`,
  O_EXCL-like and atomic): anything already at the path — file, directory or dangling
  symbolic link — reports `fs.ErrExist` and is never followed, truncated or replaced; the
  local backend uses `O_WRONLY|O_CREATE|O_EXCL`. An explicit `Mode` is chmod'ed after the
  create (the explorer passes none, so the remote umask decides), and a chmod failure
  reports an error but never removes the path: between the exclusive create and the chmod
  another process could have replaced it, so a helper that deletes on its own failure could
  delete that process's file. Append/Parents are refused with `ErrUnsupportedFileIO`; a
  missing parent is an ordinary error; a backend without the surface makes the caller
  refuse explicitly instead of racing a check-then-write. The optional
  `FileContentCreator` (`CreateNewWithContent`, reached with `ContentCreators(fs)`) is the
  content-bearing variant: the SSH helper opens the target with a noclobber descriptor and
  writes stdin through that same fd, and the local backend writes through its one O_EXCL
  handle, so the claim and the data write cannot be split by a path replacement. Plugin
  `fs.write {exclusive=true}` uses it and a backend without it returns `unsupported` (no
  CreateNew-then-WriteFile fallback); a failed content write leaves the claimed file rather
  than deleting a path another process may have replaced. Fresh comparison: Fresh's
  `create_new_file` is O_EXCL locally but its remote authority returns Unsupported (the
  agent protocol has no exclusive create); gvim's helper channel can run the noclobber
  create, so the remote explorer gets the guarantee too.
- Bounds and errors: collection is bounded during the transfer, not after it. The session
  pump writes into a bounded buffer that keeps draining after its limit (so the remote
  process never blocks) and records the overflow; an over-cap answer is an explicit error,
  never a truncated success. Exact caps: whole-file read 64 MiB (the caller's `max` is
  first capped to it; `head -c effective+1` detects oversize without transferring it),
  directory listing transfer 4 MiB on top of the 5000-entry cap, stat/write helper stdout
  64 KiB, and helper stderr 64 KiB (a failing helper's message is never parsed beyond the
  cap — the error says the error output was over the cap). A streamed write buffers only
  the caller's chunk (a save window, 1 MiB) and each helper's bounded answer; the
  temporary file is on the backend, never in client memory. Every call is bounded by its
  context (the authority kills the remote process when it ends) and the SSH layer's connect
  timeout; a failed helper is an error, never an empty successful answer. Not-found and
  permission failures wrap `fs.ErrNotExist`/`fs.ErrPermission` so callers can distinguish
  them; a directory used as a file and a non-regular file keep the port's wording; any
  other failure carries the remote stderr text.
- Watch limits: polling cannot see a rename, so it is reported as `remove` + `create`; the
  latency is the poll interval plus the debounce; events are coalesced over the
  `hostos.WatchOptions.Debounce` window (50 ms default). A watch of a file reports that file;
  a watch of a directory reports its immediate children.
- Editor routing (landed): `document.FileAccess` is the document's file I/O seam
  (`ReadFile`/`Stat`/`WriteFileAtomic`); `authority`-backed access is installed on the editor
  once the SSH slot connects, and there is no local fallback at any site. Loads, saves,
  Save As, reload (`Revert`), the changed-on-disk check and the periodic disk check all go
  through the seam; a missing remote path is an error, never an empty "new file" buffer, and
  a directory target and a broken/non-regular remote kind are refused. Saves are the
  authority's atomic temp-file+rename; a failed remote save is an error and the buffer stays
  modified, so nothing is reported saved that was not. The remote open is async: the app
  installs the access after the connect attempt and parks the file path for the root's first
  command (`tui.Deps.StartupOpen`), which loads it off the update loop; a failed or
  disconnected open is a notice and stays retryable (it is retried after an authority
  reconnect), never an empty local buffer. Remote path rules: a path is used as written on
  the remote host (absolute or `~`-rooted; a relative path is rejected rather than resolved
  against the local working directory), `~` expands through the remote shell's `$HOME`
  (cached), and `~user` is refused (recorded residual).
- Remote external-change watching (landed): the directories of the open documents are
  watched on the remote host through `authority Files().Watch` (the polled remote watcher)
  and bridged into the root's existing watch pipeline (`RootPorts.Watch`/`WatchDirs`), so
  the tree reload and auto-revert work as for a local file. The bridge
  (`internal/app/remote_watch.go`) is asynchronous: one goroutine per directory with its
  own cancellable context, events coalesced off the update goroutine over the same quiet
  window (150 ms), no local filesystem fallback (it never uses fsnotify/os.Stat for the
  remote paths). Each watch is tagged with a generation; `Reconnected()` (called from the
  existing `AuthorityHooks.AfterReconnect` next to the LSP restart) bumps it, cancels the
  old watches and restarts them for the desired directories, so an event or late flush from
  the dead carrier is dropped before it can reach the UI. A watch that fails or ends while
  disconnected waits for the reconnect; while connected it retries with a bounded backoff
  (1 s doubling to 10 s). `Close` cancels every watch, waits for the goroutines and closes
  the channel. Fresh parity: Fresh's remote external-change detection is the metadata poll
  (`watch_file` records the authority mtime); gvim keeps that poll (`PlanDiskCheck` over
  `document.FileAccess`) and adds this authority watch bridge on top.
  The remote explorer's directories now ride the same bridge (landed): the app loader
  (`internal/app/remote_explorer.go`) adds every listed directory to
  `RootPorts.WatchDirs`, so an external change refreshes the remote listing through the
  normal debounce/coalescing path off the update loop, and the bridge's existing
  cancellation/reconnect handling (Reconnected restarts, Close cancels) applies unchanged.
  The loader probes directory watching once; when the backend refuses `Watch` (or no
  bridge is wired) the tree gets one 'remote directory watching is not supported;
  refresh manually (Ctrl+R)' notice and continues without watching, so a manual refresh
  always works. The same loader filters remote directories through the remote
  `.gitignore` files read via `authority.Files().ReadFile` (root/nested, go-git parser
  shared with the local explorer, at most 64 levels / 64 KiB per file / 4096 rules); an
  oversized or unreadable ignore file, or a depth/rule overflow, keeps the listing working
  with filtering incomplete and one notice. The remote finder index uses the same reader
  (its ignore batch lives for one index lifetime, so an edited ignore file reaches the
  palette on the next index restart/reconnect); there is no local filesystem fallback.
- Remote workspace hot-exit/recovery, trust and reconnect (landed): a direct remote launch
  stages the unsaved text of its open remote documents locally under a session directory
  keyed by the remote target (`<cache>/gvim/remote-sessions/<hash>/session`; a
  daemon-hosted remote workspace uses its workspace session directory), with the remote
  paths recorded in the staged session. Recovery is installed after the remote
  `FileAccess` exists, so the next launch reopens those remote paths through the authority
  with the staged text and reports the remote target; a clean exit clears the staging
  directory. The writer is the ordinary bounded `RecoveryWriter` driven from the root's
  recovery command, and it is a deliberate local-staging divergence from Fresh (which
  routes recovery through the authority): no local file named by the workspace is read or
  written, and a local disk never becomes a remote fallback. Workspace trust for a remote
  target is recorded in the same atomic `trust.json` store through explicit record keys
  (`trust.RecordKey`: user@host[:port]:workspace root) with `LevelForRecord`/`SetRecord`/
  `OpenRecord`; a trusted/restricted decision therefore survives a restart and a reconnect
  does not re-prompt, while local workspaces keep their canonical-root keys. Remote
  executable-content marker scanning needs an early remote connection and is not wired: an
  undecided remote target is opened markerless and records that level (residual). After a
  successful reconnect the existing cache/work refreshes run (authority watch Reconnected,
  finder Restart, git cache Invalidate, explorer reload and startup-open retry, LSP restart
  in a command) and the UI reports one notice, `SSH reconnected: remote workspace
  refreshed`, with a restarted-server count or refresh failure appended instead of
  separate reconnect/restart notices.
- LSP over SSH (landed): the language servers of a remote workspace start on the remote
  host through the authority's `LongRunning` surface (a separate ssh exec channel with
  piped stdio), not as local `exec.Command`s. The manager gained a `SetSpawner` seam (nil =
  the local `lsp.Start`, so local launches are unchanged); for a remote launch the app
  installs a spawner that starts the configured command verbatim on the remote host in the
  remote workspace, overrides the initialize root URI with the remote workspace path
  (`~` expanded through the remote shell's `$HOME`), clears the locally resolved Program
  (the remote shell resolves a bare command in the remote PATH), starts it in the session's
  REMOTE captured project environment delta (`env.Remote`) on top of the remote login
  environment - the local env overlay never applies remotely - and feeds the remote child's
  stdio through
  a pipe adapter into the normal lsp client. The server's stderr is captured for the error
  tail, and a configured `process_limits` is recorded as not applied remotely (the limits
  would run on the local ssh client, not the remote host). The workspace spawn gate still
  runs before the spawner, so the coarse remote trust rule decides first, and a denied gate
  never reaches the spawner. After a successful reconnect the root runs the app's
  post-reconnect command (off the update goroutine): the manager restarts each server that
  was running before the drop (`Manager.RestartAfterReconnect`). Only previously started
  servers are restarted — a server the user never started, stopped, or a muted language is
  left alone — and a live handle or a start already in flight is kept, so a reconnect can
  never create a duplicate. The spawn gate runs for every restart (trust), and the context
  is checked between servers (cancellation); an in-flight handshake is the manager's own
  bounded start and is not cancelled mid-flight. A manual restart after a reconnect keeps
  the same state. Terminal respawn after a reconnect is an **open parity gap**, not a
  permanent decision: replaying the previous command is not safe while it can be an
  arbitrary program, and there is no recorded "replayable launch" (the login shell or an
  argv the user explicitly chose) yet. Until that exists the restored transcript stays
  read-only and the user starts a shell explicitly; the safe respawn design is the next
  step.
- Security and assumptions: helper output is parsed as data and never executed; every path
  and mode is POSIX-quoted by `ShellQuote` (a path with spaces, quotes and a newline is
  covered by a test); no shell evaluation of file names occurs. The remote base system must
  provide a POSIX `sh`, `head`, `cat`, `chmod`, `mv`, `mkdir`, `dirname`, `readlink`,
  `mktemp` and a `stat` that is GNU coreutils or BSD/macOS `stat` (the helper detects which
  by its options); no package is installed and no interpreter (Python or otherwise) is
  required. A remote whose `stat` is neither flavour gets a clear error rather than silent
  wrong metadata.

## 7. Wiring (landed this round)

The app mounts the slot in `internal/app/authority_app.go` and keeps it on the runtime:

- Local launches: `authority.Local` over the plugin host's OS instance (the same one the plugin
  runtime uses; `pluginhost.Wiring.OS` is exposed for this), with the local project env
  overlay as the env provider and the workspace-trust check as the gate. The plugin host of a
  LOCAL launch sees that overlay too. A REMOTE launch mounts its authority before the plugin
  runtime and gives it an authority-backed plugin OS (`pluginhost.NewAuthorityOS` via the
  `pluginhost.Options.WrapOS` seam): plugin processes receive the remote captured environment
  from the authority, never the local overlay, so a remote-captured variable cannot leak back
  into local children.
- Remote launches' environment (R13-13): `env.Remote` detects the configured markers and
  require evidence on the remote host — shell-free authority Stat before trust (bounded path
  count + timeout), one bounded passive `sh -c` existence probe when trusted; a failed
  probe produces a Settings notice and is never a local scan — and activates the recipe
  through the authority one-shot surface under the remote interactive login shell
  (`bash -ilc`/`zsh -ilc` when detected), diffing the remote activated environment against
  the same interactive-login baseline. Its captured delta is what the remote terminal, shell/formatter runner,
  remote LSP, remote project search and git grep receive; the local overlay is passed to no
  remote process. Activation is trust-gated exactly like the local path. The provider is
  warmed in the background after the authority connects (the remote root is expanded first);
  the interactive-shell probe is refreshed in the background after a reconnect; spawn paths
  read the cached delta and never block the UI update loop on the remote host.
  Detection/activation is otherwise explicit (Env: Activate/Reload), matching local.
- Remote launches (`gvim user@host:/path`, `ssh://...`): `authority.NewSSH` mounts a
  disconnected slot; a startup attempt dials it, and a failure is a status notice
  ("activate the window to reconnect"). Activation (`tea.FocusMsg`) reconnects, asking
  for a password through the existing prompt overlay when the last failure was
  credentials (ssh-agent and identity files are tried first). The choice: lazy connect
  with a startup attempt, because a password prompt needs the running UI.
- Remote UX: a remote launch opens no local files or session, does not restore or save
  the local session, opens the SSH terminal at `RemoteSpec.Workspace()`, mounts a real
  remote `FileSystem` and routes the editor's file I/O through it (see §6). The file named
  by the remote target is opened by the root's first command; a directory target opens the
  terminal only (the remote explorer is a recorded residual). The launch is scoped: the
  local fsnotify watcher is replaced by the remote watch bridge (the directories of the open
  documents are watched on the remote host, so a change another program makes reaches the
  tree/auto-revert like a local one), the crash-recovery copy of remote text is off, and
  the language servers are routed to the remote host — each is explicit here, not a global
  change. The status row shows `SSH:user@host` or `SSH:user@host · disconnected`
  (hand-written English this round; the statusbar owner extracts
  `gvim.status.remote_ssh` / `remote_ssh_disconnected` in the i18n slice). After a
  reconnect, the remote language servers that were running are restarted automatically and
  a status notice says how many came back; a failed restart is a notice. The remote terminal
  is not respawned automatically while a safe replayable-launch record does not exist (an
  open parity gap: a silent replay of an arbitrary command is rejected, but restarting the
  recorded login shell with the transcript preserved is the planned fix); the transcript is
  kept read-only and the user starts the shell again explicitly for now.
- Launch routing, with no backend-kind checks in the callers: the dock terminal starter
  asks `Terminal().Launch` (a nil `Backend` starts the local PTY as before, a transport
  goes through `terminal.StartWithBackend`); git grep, the shell/format runner and the
  other one-shots go through `OneShot()` with the same argv, cwd, env and timeout as
  before. The gate runs at the slot's choke point, so an untrusted workspace refuses
  remote commands.
- CLI: `internal/cli` parses both forms with `authority.ParseTarget` before any
  `stat` (an `ssh://` URL is always remote, malformed means a startup error), stores
  `Targets.Remote` and rejects a second remote, local files/a directory next to a
  remote, `--diff` and stdin. A remote target is allowed with a daemon start
  (the jump box below); an attach parses it but ignores it, because the daemon owns SSH.
  `app.Options.Remote` repeats the rules in `Validate`.
Residuals of this slice (documented, not bugs):

- Remote residuals of the landed routing: remote explorer/listing (a directory target
  opens the terminal only; the palette/explorer still describe the local tree), remote
  external-change watching covers only the directories of open documents (the workspace tree
  itself is not watched because the remote explorer is not implemented, and the authority
  watch is polled at ~500 ms plus the 150 ms coalescing window, with a rename reported as
  remove+create; manual Revert/Reopen and the periodic disk check remain the fallback), remote
  large-file saves (remote large files edit as an overlay and save by streaming the merged
  file through the authority's atomic write stream: chunks into a remote temporary file,
  then one rename; an access without the stream surface keeps the old bounded
  merge-and-whole-file write up to remoteLargeSaveMax, and Fresh's host-side patched recipe
  remains the later slice; documented above), remote
  crash recovery and hot-exit (deliberately skipped for a remote launch so remote text is
  never copied into a local cache), `~user` expansion, special remote files (the helper's
  kind mapping only knows file/dir/symlink; a FIFO read is bounded by the context and fails
  rather than blocking forever), and a remote save interrupted by a carrier drop surfaces
  an error (never a local retry or a second write).
- LSP over SSH residuals: the reconnect restart covers the servers that were running before
  the drop (a server the user stopped or a muted language is kept as chosen; a start that is
  already in flight when the carrier drops again is not cancelled mid-handshake — the
  context is checked between servers), each server gets one restart attempt per reconnect
  (the carrier itself now retries in the background, but a failed restart waits for a manual
  Start/Restart), a remote server's `process_limits` are recorded but cannot
  be applied (the local pid is the ssh client), the remote command is resolved by the remote
  login shell's PATH only (no remote install probe; a missing server surfaces the start
  error), and the remote workspace's trust decision still uses the local workspace's trust
  (a remote trust store is a later slice). Remote terminal respawn is an open parity gap:
  the safe version (respawn the recorded login shell or a user-approved argv, transcript
  preserved, never a silent replay of an arbitrary command) is not landed yet, so the
  terminal is restarted explicitly today.
- Remote environment residuals: interactive-login capture supports bash and zsh only; other
  shells (fish, csh, ...) keep the `sh -c` fallback, and a bash/zsh whose interactive rc is
  unavailable or fails falls back per capture with a notice. Marker detection has two
  bounded paths: before trust, shell-free authority Stat for each marker/require path (path
  count capped by `env.MaxMarkerProbePaths`, a 10 s shared timeout, sequential Stat calls,
  cancellation through the caller context), which shows the remote detection result in an
  untrusted workspace without executing anything; once trusted, the single `sh -c`
  existence command (same paths, config caps, command timeout/output and caller context).
  A failed/cancelled probe clears detection, shows the failure notice and is retried on the
  next Warm/Overlay rather than cached; the untrusted path needs the authority file surface
  (a backend without it shows the explicit detection notice). Fresh's marker scan is a
  passive filesystem scan available while trust is undecided; gvim now matches that
  read-only visibility through authority Stat (the rest of the remote trust flow is
  unchanged). Shell startup output before the command is discarded via a NUL marker; the interactive shell is run once per baseline/recipe capture (two remote
  commands), so rc-sourcing latency is paid per capture but not per spawn. A post-reconnect
  re-probe updates the shell choice and the Settings fallback row but not the already-shown
  startup notice, and it does not re-detect the env markers or recapture the overlay (Env:
  Reload or a new activation re-captures); if the re-probe is cancelled/disconnected it
  leaves a fallback notice reflecting that attempt. Fixed env inputs and user-configured detector marker/require
  paths are watched through authority polled watches (one bounded directory snapshot per
  poll for the root and each existing parent directory; the plan is capped at
  MaxDetectorWatchPaths/MaxDetectorWatchDirs, excess paths are reported in Settings and
  remain usable via Env: Reload; a deeply nested path whose intermediate parents do not yet
  exist is picked up when a relevant root/parent event re-arms the plan, otherwise the next
  Env: Reload/reconnect re-evaluates; the polling latency/cost limits are the same as remote
  document watching; a runtime env.detectors change re-plans only the remote provider/watcher
  (a local launch's provider keeps its startup detector set), and if the watcher had already
  degraded to the no-watch notice the detector provider is updated but no watches are
  restarted), the
  detection result is cached until Refresh/SetRoot or an env-input event, a backend without
  the watch capability (or whose watch keeps failing) degrades to the manual Env: Reload path
  with a Settings notice, the detect/capture invalidation is asynchronous (an in-flight spawn
  can use the previous captured delta until the warm finishes), the Settings Environment rows
  show the remote facts only after the background warm, and there is no status-row indicator
  of the active remote environment yet (Fresh shows one). Local behavior is unchanged.
- SSH authentication/connection residuals: ssh-agent forwarding and \`ssh_config\` reading
  (ProxyJump, Host aliases, IdentityAgent, host-key algorithms) are not implemented; the known
  hosts file is the default \`~/.ssh/known_hosts\` only; a supplied password/passphrase/2FA
  answer is kept in memory for the session by default; with `ssh.keyring` on, a host password may be looked up/stored in the OS keyring (macOS security / Linux secret-tool, docs below) — passphrases and 2FA answers are never keyring-stored; the passphrase is
  asked for only after a dial reports it needs one (no interactive first-open prompt for a
  key that is decrypted on demand either way); keyboard-interactive is replayed as one answer
  per server question, with no server-specific OTP retry policy beyond re-asking the whole
  challenge; prompt input follows the existing prompt overlay style (no new masking option
  was added); the keepalive interval/timeout and the poll/backoff bounds are package
  variables, not user settings; a background retry that needs credentials opens the prompt
  once per disconnect cycle and then waits for activation if the user cancels; every test is
  offline against in-process servers (no real host/2FA provider).
- The remote workspace's trust decision uses the local workspace's trust in this slice
  (a remote workspace needs its own trust store in a later slice).
- A dropped carrier does not resurrect an open remote terminal (open parity gap, see
  above): activation reconnects the carrier, and opening the terminal again starts a fresh
  session set by the user.
- Plugin `gvim.fs`/`gvim.proc` route through the workspace authority in a remote or
  daemon-hosted remote workspace (the app wraps the local plugin OS with
  `pluginhost.NewAuthorityOS`: files, mutations, watches and processes go to the slot, the
  authority trust gate/env/cwd apply, and LookPath/Dirs/Getenv/Now stay documented local-only APIs;
  copy/realpath/walk and a backend without mutations return clear unsupported errors; remote
  fs.watch is the polling watch with a startup degradation notice). This closes the earlier
  residual that plugin file/process APIs always used the local OS.

## 7.1 Daemon-hosted remote authority (jump box)

A daemon start may combine a remote target with the daemon mode: the daemon runs on THIS
machine and hosts that remote workspace over its SSH authority, so only this machine needs
gvim installed. Fresh calls this the single-install jump box (docs/features/ssh.md). gvim
implements the bounded slice:

- The target is consumed only when the daemon starts. Its canonical text
  (user@host[:port]:path, or ssh://user@host[:port]/path for a non-default port, including
  an optional :LINE[:COL]) is persisted in the workspace registry as authority_spec; a
  workspace is keyed by (remote root, canonical spec), so a restart with the same target
  reuses the same stable id and session directory, and a different spec over the same path
  is a different workspace. Old registry files load as local, and a remote root is
  normalized as a POSIX path without any local stat.
- Before the socket starts hosting, the daemon connects its foreground remote workspace on
  the daemon terminal: a failed dial that reports a needed password, encrypted-key
  passphrase or keyboard-interactive (2FA) challenge prompts on the terminal and redials
  with the answer, with a small attempt bound. A daemon without an interactive stdin fails
  with an actionable error (start it from a terminal or use a key/agent) rather than
  hanging on a prompt. A dial failure that needs no input is likewise an actionable startup
  error naming the host.
- An attach only talks to the daemon socket (Hello and frames); it never dials SSH, so a
  reattach after a detach reuses the daemon's live authority. A remote target in the attach
  arguments is parsed and ignored, matching Fresh.
- A workspace binds one backend: a local workspace switched to keeps the local authority,
  while a dormant remote workspace materializes its own authority lazily from its stored
  spec (the workspace build sets its Remote options; the existing trust gate and a
  non-parseable spec keep the current workspace with a notice). Workspaces over different
  hosts can coexist in one daemon because each runtime owns its slot.
- Tests use an in-process fake authority (no real SSH): daemon start with a remote target
  and an attach/reattach, restart reuse of the persisted spec and stable id, lazy dormant
  remote materialization, an unparseable stored spec keeping the current workspace, the
  password/passphrase/2FA prompt loop, and the non-interactive startup failure. The
  registry side also covers spec round-trip, local discovery ignoring spec-bearing entries,
  and old-file tolerance.

Daemon-hosted remote workspaces also restore their session across a daemon restart. The
existing workspace capture writes the session layout, file states and stashes into the
workspace session directory (keyed by stable id) on exit; on the next start the remote
workspace is built with its stored authority spec, waits for the authority connection, then
reopens the saved files through the remote document access (the existence probe goes through
the authority file system, never a local stat), restoring tabs, active tab, cursor, pane
layout, explorer/sidebar state and unsaved stash text. A saved file that no longer exists on
the remote host keeps its unsaved stash and is reported by the normal session notice. A
daemon start whose reconnect fails restores nothing and leaves the session file and stashes
untouched for the next start (the runtime's capture is skipped, so an empty editor cannot
overwrite them). Direct remote launches still keep no local session, and a reattach after a
detach keeps the live daemon state (no restore runs then). The restore itself is a bounded
build-time step after the connect, never on the update loop; a failed individual file open is
a kept-stash notice like a local restore.

Residuals of this slice: real SSH is still untested (all tests are offline fakes); a remote
workspace whose remote files changed while the daemon was down restores the saved text and
reports the usual per-file open failures (no remote conflict detection beyond the normal
stash/mtime baseline); the restored terminal transcript comes back dead (the normal restored
session behavior, no process re-run); plugin gvim.fs/gvim.proc follow the workspace
authority (files, mutations, watches and processes, see the plugin OS paragraph above; the
remote explorer/terminal/search also go through the authority);
the remote workspace's trust decision is the local workspace's trust store; and per-file
ownership across a jump-box daemon and a direct editor on the same remote host is not
covered.

## 8. Tests

- Local parity: forwarding exactly matches direct OS-port calls (files, errors, env merge,
  gate refusal, watch events, shell probe).
- Spec parser: both forms, defaults, ports, IPv6, percent-decoding, line/column suffixes,
  colons in paths, windows-ish/local forms, malformed `ssh://` errors.
- Fake authority: state transitions and reconnect for the indicator/activation hooks.
- SSH authentication and keepalive (offline): an encrypted identity reports
  \`ErrNeedsPassphrase\` and \`ReconnectWithPassphrase\` redials with the secret; a
  keyboard-interactive 2FA challenge is recorded with its questions/echo flags and
  \`ReconnectWithAnswers\` completes the dial; a server that never answers keepalive pings
  flips the slot to disconnected with a keepalive reason and a reconnect restarts the
  carrier (\`authority/ssh_auth_test.go\`, \`authority/ssh_keepalive_test.go\`).
- TUI authority retry/prompt behavior: key passphrase prompt reconnects with the secret,
  2FA asks each question and replays the answers in order, the background poll retries with
  capped backoff and one dial in flight, and the status row shows "reconnecting" while a
  dial runs (\`internal/tui/authority_retry_test.go\`).
- Terminal transport: `StartWithBackend` over a fake backend (paint, input, resize, close).
- SSH backend against an in-process `x/crypto/ssh` server (unix-only build tag): one-shot
  command composition and exit codes, PTY terminal with echo/resize, long-running stdio,
  identity-file auth, unknown-host error, dropped-carrier detection and reconnect.
- Remote file helpers (`authority/ssh_files_test.go`, same in-process server running the
  composed command locally so the real helper text and quoting are under test): read and
  the read cap, missing/directory errors, atomic writes (content, mode, preserved mode,
  explicit mode, parents, append, empty file, symlink written through), stat kinds and
  the follow/broken-link rules, the sorted hidden-filtered listing with target kinds,
  polled watch create/write/remove, permission mapping, a hostile path with spaces/quotes/
  newline, and a dropped carrier failing file I/O clearly.
- Editor routing (fake in-memory authority FileSystem, `internal/app/remote_editor_test.go`):
  open and save go solely through the access (the local path is absent before and after a
  save), a remote changed-on-disk state refuses the save and writes nothing, a failed or
  disconnected open is an error with no tab bound to the remote path (no empty buffer), a
  directory target is a directory error (no buffer), and Save As plus Revert go through the
  same access. The asynchronous startup open and its reconnect retry are in the root
  (`Deps.StartupOpen`); the document seam has unit coverage through these editor tests and
  the existing editor suite.
- The root's startup-open retry: `internal/tui/remote_startup_test.go`
  TestStartupOpenRetriesAfterReconnect (a failed open is a notice with no document,
  repeated failed reconnects keep the open pending, the first successful reconnect retries
  once, and a later reconnect does not re-open).
- LSP over SSH: `lsp/client_remote_test.go` (caller-launched client captures the remote
  server's stderr tail and the limits note), `lsp/manager/spawner_test.go` (the injected
  spawner is used and the spawn gate denies before it), and
  `internal/app/remote_lsp_test.go` TestRemoteLSPSpawnerRunsOnTheAuthority (a real
  initialize handshake over the authority transport; remote cwd, configured command
  verbatim, remote limits note). The reconnect restart has behavior tests in
  `lsp/manager/reconnect_restart_test.go`: a dropped server restarts once with no duplicate,
  the spawn gate denies before the spawner (trust), a cancelled context starts nothing, and
  a manually stopped server or muted language is not restarted; plus the root hook tests in
  `internal/tui/authority_restart_test.go` (post-reconnect work schedules only on success,
  off the update goroutine, and reports count/failure). A real-host manual acceptance
  remains: start gvim on `user@host:/proj`, edit/save/revert a file, start a language
  server, drop the connection and reactivate the window — the server restarts and the
  notice says how many; restart the terminal explicitly.
- Remote external-change watching (`internal/app/remote_watch_test.go`, a fake authority slot
  and controllable hostos.Watch): events from the remote watch become `filewatch.Changed`
  values with the changed file and its directory; a burst is coalesced; Close cancels the
  authority watch, closes the channel and is idempotent; Reconnected cancels the old watch,
  starts a new one for the desired directory and drops an event/flush from the pre-reconnect
  watch; nothing is watched on a disconnected authority and a reconnect starts it; a
  duplicate Add gets one watch; a nil authority degrades safely. Fresh parity: Fresh's
  remote external changes are the authority metadata poll; gvim keeps that poll and adds this
  authority-watch bridge on top.
- CLI and app rules: remote forms reach `Targets.Remote`, malformed/mixed launches are startup
  errors, and `Options.Validate` repeats them.
- TUI: the connected/disconnected segment, activation reconnect, the password prompt and
  the focus path, driven by a fake hooks set (no SSH needed).
- Manual acceptance for a real host is recorded in the round report (no CI host is used):
  `gvim user@host:/tmp/proj` with an agent key opens the remote shell in /tmp/proj,
  shows `SSH:user@host`, survives a network drop with `· disconnected` until the window is
  activated, and asks for the password through the prompt when no agent/key is set.

### File mutations (remote explorer write foundation)

The tree-changing operations are their own optional surface, `FileMutations`
(`Mkdir(ctx, path, parents)`, `Rename(ctx, from, to)`, `Remove(ctx, path, recursive)`),
reached through `authority.Mutations(fs)`; a `FileSystem` that does not implement it makes
the caller report `ErrUnsupportedFileIO` instead of a panic, and test doubles are unaffected.
The local backend forwards to the plugin OS port; the SSH backend runs `mkdir [-p]`, `mv -f`
and `rm [-r] -f` as bounded helpers under the same quoting, stderr cap and context
cancellation as the read helpers, and maps a missing path/permission denial into the port's
errors. A remote delete is permanent: `Mutations.Remove` (recursive for a directory), no
trash. A trash move was audited and deferred: no current backend can prove an atomic
same-filesystem no-replace move (an `mv -n` may copy/unlink across filesystems or lose the
source to cancellation, and its no-clobber flags are not portable), and the authority has
no private-permissions directory surface for a trash; delete therefore keeps its explicit
permanent semantics instead of claiming an unproven recoverability.

The remote explorer's write UI is wired (`internal/app/remote_explorer_ops.go`): New File,
New Directory, Rename and Delete run through `Files()`/`authority.Creators(fs)`/
`authority.Mutations(fs)` with the workspace trust gate before every operation and the
explicit "remote workspace is disconnected; activate the window to reconnect" error while
the authority is down. Names are validated with the same `fsops.Validate` rules and must
stay inside the remote workspace root. New File uses the **exclusive-create** surface, so a
target created concurrently by another process is never overwritten: the backend either
creates the empty file atomically or reports `fs.ErrExist`; a backend without the surface is
refused explicitly (no Stat + WriteFile fallback). New Directory uses the authority's atomic
`DirectoryCreator.MkdirNew` surface: one no-replace `mkdir`, so an existing entry reports
`fs.ErrExist`, a missing parent reports `fs.ErrNotExist`, and a backend without the surface
is refused explicitly (no Stat + Mkdir fallback). The local explorer's New Directory is
`os.Mkdir` itself, the same atomic no-replace semantics. A rename has no atomic rename-new authority surface; instead of a Stat + replacing
`Mutations.Rename` (whose check-to-write race can overwrite a concurrent target), gvim
publishes the destination through the same bounded copy-and-remove path as Move: exclusive
file/directory create for the destination, source removal only after the copy succeeds.
Large trees are bounded by the copy limits and report the bound; metadata follows the copy
path's best-effort behavior, and a symbolic-link source is refused rather than followed. A remote delete asks first and the question
says it is permanent (no remote trash). After a successful operation the existing explorer
file-job refresh reloads the remote tree through its reload command, so the update loop
never waits on the host.

The trash design is recorded but not shipped: it requires an authority surface that proves
an atomic same-filesystem no-replace move and a private (0700) trash directory. Neither is
available today (SSH `mv -n` is not portable/atomic across filesystems and can copy+unlink;
`Mkdir` carries no private-permission option), so gvim does not create a trash directory and
does not claim delete is reversible. This remains the gating residual for trash recovery.

The local explorer uses the same publication rules through `fsops`:
a free copy creates its destination in place with O_EXCL files, `os.Mkdir` directories and
an O_EXCL-like symlink, so a concurrent creator cannot be overwritten. A copied file is
first written to a unique 0600 temporary file in the destination directory, given its
mode, and published with an atomic no-clobber `os.Link(temp, target)`; the temporary file
is then removed. A failed write removes the temporary file and leaves no target, and a
failed directory copy removes the directory it created, so a retry is not blocked by a
partial destination. If the filesystem cannot hard-link, the copy falls back to the
recorded direct O_EXCL write: `fsops.CopyWithResult` (and the Rename/Move result variants)
return the warning, the local adapter surfaces it through `TakeCopyWarning` (status notice),
and the fallback can leave a partial target on a later write failure. A failed source removal after a successful copy-and-remove
keeps both copies and reports the source-removal failure. Cleanup failures are joined into
the returned error, never silently ignored. Local Rename and Move publish with that bounded
copy-and-remove path (10000 entries / 256 MiB / 64 levels) because `os.Rename` replaces an
existing target; the source is removed only after the copy succeeds, and a larger tree is
refused before anything is touched. The explicit Overwrite policy keeps the local
staging/swap path (the old target is moved aside, the new copy swapped in); it is the only
path that replaces and the confirmation makes that choice explicit. That explicit overwrite
is not a no-replace guarantee: a destination re-created concurrently between the swap
renames can be replaced, which is the recorded non-atomicity.

The clipboard is wired on the same surface (`internal/app/remote_explorer_copy.go`, task
148). Copy walks the source subtree with `Files().ReadDir` (hidden entries included) and
publishes destinations without replacing anything: directories are claimed with
`DirectoryCreator.MkdirNew` and files with `FileContentCreator.CreateNewWithContent`, so
the top-level name has one atomic winner, a concurrent creator gets `fs.ErrExist` (the
root's existing conflict dialog), and binary data is carried byte-for-byte. A backend
without either no-replace surface is refused before any destination write; there is no
Stat + write fallback. The explicit Overwrite choice is different: the root removes the
old target first and writes through `Mutations.Mkdir`/`WriteFile(Atomic: true)`, so that
user-chosen replacement is not atomic and a concurrent re-creation can be replaced; the
confirmation remains the only way to take that path. Cut (`Move`) is copy-then-remove: the source is removed with
`Mutations.Remove` only after the whole copy succeeded. Bounds: 10000 entries, 256 MiB,
64 directory levels, and a 2-minute context (the caller's context still cancels). Refusals
are explicit and happen before the destination is created: symbolic links anywhere in the
subtree (the port follows links on Stat, so the no-follow answer comes from the parent
listing), the workspace root itself as source, paths outside the remote root, a destination
inside the source, and a copy/move onto an existing name in another directory now goes
through the explorer's conflict choice instead of blindly refusing: Cancel reports
`explorer.ErrConflict` and changes nothing. A single-item conflict asks Overwrite / Rename /
Cancel (Esc); a multi-item paste asks Fresh's multi dialog Overwrite (o) / Overwrite All
(O) / Skip (s) / Skip All (S) / Cancel (c, Esc). Overwrite handles the current item and
asks about the next conflict; Overwrite All applies the overwrite policy to every
remaining job of the paste so no further prompt appears; Skip skips only the current item
and asks about the next conflict; Skip All skips the current and every remaining item and
stops (a cut paste also drops those stranded sources from the clipboard, like Fresh's
finished batch); Cancel stops the current and remaining jobs, keeps the already-completed
files and leaves the clipboard for a retry. Stop notices say how many items of the paste
already completed; a later non-conflict failure stops the remaining jobs, keeps the
completed/partial results, and appends that count to the error notice. Rename (single-item
dialog) opens a prefilled "Paste as on SSH:…" name prompt and retries through the adapter's
`CopyAs`/`MoveAs` with that destination name; Overwrite removes the existing destination
first and then copies (a cut paste removes its source only after the copy succeeded). The
adapter also supports Skip directly (leave the destination, report its path; for a cut
paste return the source, which cancels the cut). The local conflict prompt (Overwrite /
Skip) is unchanged. Duplicate stays a
same-directory copy with Fresh's rule — "name copy.ext", then "name copy 2.ext", ... checked
against the remote directory (1000 attempts) — so it never prompts. A cancellation or I/O
failure in the write phase keeps the partial destination and the error says how many files
were written and where the partial copy is; gvim never deletes remote data on a failed
copy. This matches Fresh's multi dialog, with one recorded divergence: Fresh resolves
the whole multi-conflict queue first and only then runs the batch, so a Cancel there
executes nothing, while gvim runs the chain incrementally and a later Cancel / Skip All
therefore keeps the items already pasted (the notices count them) — no hidden undo. Every clipboard action
confirms first on a remote workspace and names the target (copy/cut/duplicate name the
entry or count, paste names the destination and count); a local workspace keeps the old
no-confirmation behavior. The tree refresh after success is the existing file-job refresh.

The remote explorer also has a direct drag/move gesture, modeled on Fresh's drag: a left
press on a row lifts the effective selection (a multi-row selection moves whole), moving
over a directory row highlights that drop target, and releasing opens a confirmation that
names the entries (or count) and the destination on the remote host — nothing moves before
that Move answer. Local workspaces do not enable the drag (the notice points at Cut and
Paste), and the same keyboard-only flow already exists there and remotely: Cut (Ctrl+X),
select the destination directory, Paste (Ctrl+V); it uses the same confirmation, conflict
and job machinery. Drops that would not move anything (the source's own parent, the source
itself, a directory dropped onto its own descendant) are filtered, a drag never touches the
clipboard, and only the remote authority is used. After the confirmation every move job
starts with the cancel conflict policy, so a taken destination goes through the existing
single/multi conflict dialogs and is overwritten only on an explicit choice; the move is
`MoveAs`'s copy-then-remove (complete copy first, then `Mutations.Remove`), so a failure or
cancellation in the copy phase keeps the source and any partial destination, and a failed
source removal keeps both and reports it. The drop target is only underlined while the drag
is held; there is no animation/preview beyond that.

Copies preserve permission bits and modification times best-effort when the backend
implements the optional `FileMetadata` surface (`SetMetadata(ctx, path, mode, mtime)`; the
local and SSH backends do): the source mode is also passed to the atomic file write, and
mode/mtime are applied to files and then directories after the bytes are complete. A
backend without the surface keeps the byte copy and is not claimed to preserve metadata. A
metadata failure never fails an otherwise complete copy or move; the adapter packages it as
a one-shot warning (`TakeCopyWarning`) which the file-job UI shows as a notice. A metadata
stat failure during planning is tolerated (the bytes still copy) and can leave that entry
with default metadata.

Remaining gaps: the drag/move has no cross-window or above-root target and no
animation/preview beyond the drop-target underline (and is disabled for local
workspaces), no journal undo/history for remote writes, no remote trash/restore UI
(deferred until the atomic same-filesystem no-replace move and private trash directory can
be proven; a remote delete is therefore documented and prompted as permanent), no
whole-subtree transaction or cleanup UI for a partial copy, FileEntry/Mode and mtimes are not
restored (the port's DirEntry has no mode), hard links/xattrs are not preserved, and a
copy/paste destinations are now no-replace (remote DirectoryCreator/FileContentCreator; local
O_EXCL/os.Mkdir/symlink create), and Rename uses a bounded copy-and-remove because no
backend has an atomic rename-new surface; explicit Overwrite is still remove/replace-based
and non-atomic by user choice.
Overwrite is remove-then-copy with no rollback: a process that re-creates the destination
between that removal and the copy is a documented race (the file copy atomically replaces
the re-created file because the user chose overwrite; a directory copy fails at Mkdir
instead of silently replacing it), and an Overwrite All decision applies only to the
remaining jobs of that one paste (it is not remembered for a later paste), while a later
Cancel / Skip All cannot undo items already pasted (incremental execution diverges from
Fresh's resolve-then-run batch). A drag/lifted move runs the same incremental job chain,
so a later Cancel/Skip cannot roll back already-moved entries and its confirmation is
batch-level (one Move/Cancel for the whole lifted selection); a drag does not change the
clipboard, so a pending cut is left as it was.
Real-host manual acceptance remains unautomated (the copy tests use a fake
authority file system, with a local decoy tree proving no local fallback).

### Remote project search candidate scope

Remote project search uses Fresh's project-file scope (`project_files`): a complete bounded
`git ls-files --cached --others --exclude-standard` listing is authoritative in a work tree
— tracked files (including tracked files that match .gitignore and tracked dotfiles) and
untracked non-hidden files are candidates, passed explicitly to rg/grep in bounded batches
so ignored trees are never scanned into the output cap. Each nested repository/submodule
listed by name (up to 8) is then resolved with its own bounded
`git ls-files --cached --others --exclude-standard`, so its `.gitignore` applies exactly as
git defines it (tracked-ignored files included, untracked ignored files left out); those
candidates join the explicit batches, and a complete resolution carries no degradation
notice. Resolution is all-or-nothing: more than 8 nested repositories, a failed/truncated
or non-UTF-8 listing, a path escaping the workspace, or a deeper nested
repository/submodule leaves every prefix unresolved and keeps the widened walk fallback.
That fallback searches the unresolved nested-prefix roots in place (not the whole tree), so
unrelated ignored entries cannot consume its output cap before them; the wider whole-tree
fallback is reserved for a candidate list too large for the bounded explicit batches. Both
prune hidden path segments and `IGNORED_DIRS` (`.git`, `node_modules`, `target`,
`__pycache__`, `.hg`, `.svn`, `.DS_Store`) in the candidate filter and carry the non-fatal
notice. A failed or timed-out top-level candidate listing keeps the whole-tree fallback
results and says the ignore scope is unavailable; a **truncated** listing keeps the same
results, marks the Done batch `Truncated`, and says the candidate listing itself was
truncated — an incomplete listing is never presented as a complete scope or dropped. If the widened run hits the remote output cap, the batches are marked `Truncated`
and the notice says the result is incomplete because candidate files beyond the cap were
not reported — never a complete-looking result; a partial nested candidate set is never
presented as complete, and a deeper nested repository's own `.gitignore` is not applied in
that fallback (recorded residual). The explicit-candidate
half keeps git's exception: a tracked candidate under such a directory is still searched
because git lists it; only walked/nested content is pruned. The panel's glob filter is carried as the raw value and applied to candidate paths,
whole-tree results and open-buffer snapshots even when the quick-open index snapshot is
unavailable: a glob can never silently widen the search to files outside it. The panel reads the optional finder capability as **one short-mu snapshot** (files,
loading, error, Idle, truncated and closed from the same generation), not as
`Snapshot().loading` followed by separate status reads: the read never waits for
`Restart`/`Close` to finish an old enumeration, and a Restart cannot combine an old empty
list with a new ready flag. A snapshot is authoritative only when that observation
says it is complete (not loading, not failed, not truncated, not `finder.Idle`'s
placeholder and not a closed index). A **ready** snapshot, even when the glob filters it
to zero files, returns a genuinely empty result without running the tool or re-adding
open buffers, matching the local searcher. A **loading, failed, truncated, Idle, closed
or capability-less** snapshot may be partial: it permits a bounded fallback scan (the
whole-tree path, a complete candidate list or a nested-prefix scan) with the glob still
applied (open buffers are included) and carries a localized
non-fatal notice naming the actual reason; a missing capability is treated as unknown,
never as ready. Dynamic finder error/notice text is cleaned with `textsafe`, bounded, and
inserted through the i18n catalogue before the panel sees it. That fallback's output cap is applied before the filter, so a
capped run is marked `Truncated` with an explicit cap-before-filter partial notice instead
of looking complete. The same filtered-cap notice is added once to a listed-but-widened or
nested-prefix scan that hits the cap before its filter (the duplicate generic widen-cap
message was removed). The fallback notice deliberately says “bounded fallback scan”,
not “whole-tree”: the route can be a complete git candidate list, a nested-prefix scan or
a whole-tree scan, and the notice is attached before that routing decision. When the snapshot exists it still bounds AllFiles: a
stale snapshot stays authoritative and the stale-snapshot notice points at File Explorer:
Refresh. Open buffers are searched from the editor's snapshots
(the current buffer first, then the other open buffers after the tool results), and that
file's remote disk line is skipped: unsaved edits are found exactly as Fresh's
project_files searches open buffers, with no extra remote read. An open file outside the
workspace root, or excluded by the panel's file snapshot, is left to the dedicated Open
Buffers scope. When the remote search tool cannot be probed at all (missing), or its run
fails/times out, those open-buffer snapshots are still streamed first and then a terminal
error batch names the original tool/probe failure and says only open buffers were searched
from their editor snapshots — the remote project files were not searched. The result is
never presented as complete, no local disk is read, and a user cancellation still closes
the batch stream without a Done batch (no open-snapshot rows are emitted after the cancel).
The same panel limit/caps apply to the snapshot matches. When the search is bounded by the
panel's quick-open file-index snapshot and the remote tool returns a matching path outside
that snapshot, the Done batch carries a non-fatal notice saying the result was limited to
the snapshot and that the path may be new since the index last refreshed (or excluded by
the glob), pointing at File Explorer: Refresh; the batch is not marked Truncated because
this is a snapshot bound, not an output cap, but the result is never presented as an
unqualified complete project search. The finder index still does not refresh itself on a
search; the notice is the safe slice until a panel-driven refresh is wired. All commands
run through the authority one-shot surface with the remote cwd, stay bounded/cancellable
(the search context bounds every nested listing), and never read the local disk; other
degradations are carried as non-fatal panel notices.

### Remote search output trust and path normalization

Remote search treats the tool's output as untrusted, because a remote file name can contain
a newline, a colon or `..` segments. Both tools are invoked with `--null`
(`rg --vimgrep --null ...` and `grep -r -n -I --null ...`), so each match is
`path NUL line:column:text` (rg) or `path NUL line:text` (grep) and a name containing a
colon or newline cannot be split into a different path or a fabricated second match. The
parser requires the NUL record delimiter: output without one (for example a tool that
ignores `--null`) is ignored instead of guessed at. Before a record becomes a result the
path is normalized: one leading `./` is stripped, `path.Clean` collapses `.`/`..`, and
absolute paths, paths that escape the root, empty/dot paths, NUL, control characters and
invalid UTF-8 are refused (a colon stays legal). The same normalization is applied to the
panel's quick-open snapshot keys (`remoteAllowedFiles`), to open-buffer paths
(`remoteRelPath`), and to `joinRemotePath`, so an `allowed`-less search cannot produce an
escaped result path and a stale or hostile snapshot cannot widen the scope. Each remote tool command keeps at most 8 MiB of output (the cap is per command, not a total: one
search can start at most 64 batched candidate commands and every command/deadline is bounded by
the 20 s search deadline, so a full search can return more than 8 MiB across batches while each
batch is capped). The result's `Match.Text` is display-sanitized before the panel sees it: invalid
UTF-8 bytes and Unicode Cc/Cf categories are removed (terminal escapes, BEL/NUL/CR, bidi overrides
and zero-width format characters included), while tab and newline keep their normal semantics and
all readable text (including CJK/emoji) is preserved. For tool hits the engine still matches
the raw line, then the match span is remapped through the sanitizer's raw-rune→display-rune map,
so a control/format character removed before the hit cannot shift `Match.Col`/`Len`/`EndCol` off
the painted text; an open-buffer snapshot searches the sanitized text directly, and the remote
git grep byte column is converted to the sanitized line's rune column. A match made entirely of
stripped characters has no readable span and is dropped; when the engine finds nothing the
synthetic fallback (tool line + byte column) is dropped too if the sanitized line or query has
zero readable runes, and otherwise its column/length are clamped inside the sanitized text. A
snapshot query containing a stripped character no longer matches (recorded residual); readable
queries are unaffected. This is a display-only bound: the stored file text
and the search engine are untouched. File *names* are normalized by `cleanRemoteRel` but a legal
Unicode Cf character in a remote path is not stripped before display (`Cf path display residual`);
the path is still displayed in the panel exactly as named on the remote host. The search
context still bounds every listing/tool command, cancellation still stops between records,
and no local file system is read. Remote Git Grep uses the same protection: its remote argv
adds `--null` and its output is parsed as `path NUL line NUL column NUL text`, with each path
through `cleanRemoteRel` and an empty/escaping path dropped (the local Git Grep adapter keeps
its colon format because that output never crosses the authority boundary). Tests cover
colon/newline spoof records, traversal and `..` paths, legacy colon-only output, legal colon
names, the `--null` argv for both tools, and the remote Git Grep NUL parser
(`TestParseRemoteSearchOutputRejectsSpoofedRecords`,
`TestRemoteSearchRejectsSpoofedRemoteFileNames`, `TestRemoteSearchIgnoresLegacyUnnulledOutput`,
`TestRemoteRelPathRejectsEscapes`, `TestRemoteAllowedFilesNormalizes`,
`TestRemoteSearchArgvUsesNullFileDelimiter`, `TestParseGitGrepNullOutputRejectsSpoofedPaths`).

### Remote Live Grep

A remote workspace's Live Grep is the same floating palette as the local one, but its
Files scope is served by the authority-backed Live Grep adapter
(`remoteLiveGrep` in `internal/app/remote_live_grep.go`), not by the local file reader.
The local Grep port is cleared in `remoteRootPorts` and replaced in
`installRemoteSearchPorts`, so a remote launch can never silently search the local
project: every file command goes through the remote project-search adapter and its
authority one-shot surface, with the same workspace spawn/trust gate (an untrusted
workspace is refused before a command runs), remote cwd, captured remote environment,
tool probe (rg then grep), NUL-delimited parsing, candidate scope, per-command 8 MiB
output cap and 20 s search deadline. The Files scope passes the remote quick-open
snapshot as the project-search candidate filter; an empty snapshot falls back to the
same Fresh project-file candidate scope (`git ls-files --cached --others
--exclude-standard`) the panel uses. The Ignored scope cannot be expressed by a
candidate list (untracked ignored files are not listed), so it runs the same adapter's
widened remote scan (`rg --no-ignore --hidden -g !.git`, or grep with skip
directories/--exclude-dir) through the same one-shot/caps/parser; `.git` and the skip
directories are filtered out defensively. Buffers and Terminals are
searched from the editor's in-memory snapshots after the file hits (no PTY, no
process, no local file read); they are client-side text, not remote files.

The palette implements the optional `ContextGrepper` seam
(`SearchContext(ctx, req) (GrepResult, error)`) and cancels the active remote search when
the query/scopes are superseded, the palette is dismissed/closed, a row is chosen, or an
export opens: the streaming project search stops between records and the authority
command's context closes. The search is bounded by the palette's 100-row result cap and
the project-search limits. The result carries the project-search `Notice` and
`Truncated` signal (and an Ignored-scan output-cap notice) alongside the successful hits;
the palette paints the hits and shows the incomplete/degradation state in its title
(`… · incomplete`) and footer; a tool exit>1 with printed stdout is parsed as a partial
result (the project adapter merges that stdout before the Done error), so candidates found
before the failure are not silently dropped, while the seq guard drops a late result from an older
query instead of overwriting the new query's notice or rows. The Grep result itself keeps
no cache. The Alt+Q quickfix export carries the palette's `Remote` flag and the result's
`Notice`/`Truncated`, so its file rows join the remote root with `/` and open through the
remote document access (never the local filepath path), and a degraded/capped result is
marked `· incomplete` in the list title with a leading notice header instead of looking
complete.

Each palette instance carries an instance token stamped on every owned asynchronous
message (file ranking, file poll tick, Live Grep result and preview result); a result from
a closed palette is dropped by the token/seq/rel guard even when a newly opened palette
reuses the same owner, query sequence and path. The per-file preview cache and displayed preview are
dropped when the authority transitions to disconnected or starts any reconnect attempt (including credential prompts,
and again on a failed attempt) and on a successful reconnect, and the preview sequence is bumped so a command
started for the old carrier cannot paint over the cleared state. The windowed `ReadWindow`
read is cancelled through its context; the portable `ReadHead` path has no caller
context/cancel (only its own bounded timeout), so its eventual result is discarded by the
sequence guard while the underlying bounded read may finish — a declared limitation, not a
cancellation claim. Remote row preview uses `remotePreviewer.ReadWindow`: a cancellable authority
`ReadRange` scan (64 KiB chunks, 8 MiB scan budget, five lines before the hit, twelve
lines maximum, 16 KiB display/64 KiB hard cap) returns complete remote lines around
the match; it never reads the local disk and falls back to a bounded whole-file
`ReadFile` only when the backend has no windowed primitive. The palette cancels the
window read on selection change/dismiss; a quick-open remote `path:line` query uses the
same window around its target line instead of only the head. Enter on a file hit emits the
remote absolute path (root joined with `/`, never the local separator), so the existing
remote document access opens the real remote file.

The project-search panel's selected-match preview uses the same windowed idea through its
optional `searchpanel.ContextWindowPreviewer` seam: `remoteSearch.PreviewWindowContext`
scans the remote file from the start in 64 KiB `ReadRange` chunks until the selected
match's surrounding window, with an 8 MiB scan budget, a 256 KiB output/line cap, the
panel's cancellation context and the existing stale-sequence guard. A window that reaches
EOF reports the true total line count; one that stops at its window reports `Window` +
`Truncated` and an unknown total (the header says `window only`), and a match beyond the
scan budget is an explicit error (never a silent file head or local fallback). Syntax
colour is applied only when the total is known and within the shared highlighter's line
cap, so a deep unknown-total window stays plain; the open-buffer path is unchanged. The
window scan validates the bytes it reads (no NUL/binary and strict UTF-8, with a carried
partial rune at chunk boundaries): a non-UTF-8 or binary remote file returns the explicit
`not UTF-8 text`/`binary` error (or the palette's binary placeholder) instead of being
silently sanitized into a plausible partial excerpt. A line exactly at the 256 KiB
output cap is kept as a complete line (the cap counts content, not the virtual
newline): at EOF that can be the whole file, and with following lines it is reported
as a `Window`+`Truncated` window. A single line above the cap is the explicit
`a remote line is longer than the preview line cap` error, never an empty window or a
generic scanner failure.

Remote Live Grep keeps no result cache of its own. Resume restores the query/modes/
scopes only (the palette re-runs the search), which is the honest behavior for remote
file contents, a reconnect or a root change; the remote file index and Grep port are
rebuilt with the workspace, and a disconnected authority reports its error instead of
falling back locally.

### Windowed remote reads (remote large-file mode)

The authority file port has `FileSystem.ReadRange(ctx, path, off, n)`: a bounded window
read (local `pread`, SSH `tail -c +off+1 | head -c n`), capped at 1 MiB per answer and
returning `io.EOF` for a short final window. Remote large-file mode builds on it: a remote
file at or above the editor's large-file threshold opens through a windowed
`buffer.Source` instead of being read whole.

### How the windows load

The document's window source keeps 1 MiB aligned windows with an LRU of 8 (about 8 MiB per
document) and a window-arrival generation. `ReadAt` is **cache-only**: a miss records the
window and returns `buffer.ErrSourcePending` immediately, doing no I/O, so a paint never
waits on the carrier. The buffer treats that sentinel as retryable (it is not a source
change: the buffer does not freeze, the document does not turn read-only for it, and the
line paints blank for the moment). A save also loads a window when its merge runs into a
missed one: that work happens in the save command, never on the update loop. The TUI's
large-file loop, which only exists while the active document is such a file, arms one short
delayed check per update, loads the missed window in a `tea.Cmd` under the large-state
context (cancelled when the model closes or the document goes away, which the source
observes through the context passed to `ReadRange`), and the reply makes the next update
repaint with the loaded text. One window after the requested range is prefetched when the LRU
has room; farther scrolling misses again and loads the next window. Local large files keep
the synchronous `fileSource` path unchanged and never arm the window loop.

### Editing and saving

Remote large files are **editable** with Fresh's large-file model: edits become an in-memory
overlay over the untouched windowed base (buffer's large-file overlay, already bounded by its
overlay budget). The status row says `remote large file (windowed): the bytes stay on the
remote host and load as you scroll; a save streams the merged file into one atomic write`
(an access without the stream surface says the old bounded wording instead).

A save merges the overlay with the unchanged head/tail read back through
`FileSystem.ReadRange` in bounded chunks, and (when the access implements the optional
`StreamAccess` part) streams the merged file into the authority's **streamed atomic write**:
the merged bytes go a chunk at a time into a remote temporary file, then one `mv -f`
replaces the target. Nothing materialises the merged file in client memory, so a remote
large file of arbitrary size saves with bounded memory; a failed chunk or a cancellation
aborts the stream (the temporary file is removed and the target is untouched), and the save
is prepared on the editor goroutine and runs in a `tea.Cmd` like every other save, so the
update loop never waits on the carrier. A remote file that changed after the plan refuses
the save with the ordinary changed-on-disk error (the stream is never opened); the plan's
remote stat and the post-write stat both go through the authority. After a successful write
the save command opens a fresh windowed source for the new file and carries it in the
result, so `ApplySavePlan` rebases the buffer without any remote read on the update
goroutine. Undo, dirty state and rebase behave like a local large file.

An access without the streaming part keeps the old fallback exactly as before: merge into
one byte slice, bounded by `remoteLargeSaveMax` (64 MiB), and write it whole; a larger
merged file refuses with a clear notice. The status row says so in that case (the normal
authority-backed access advertises the streamed save).

This is still a deliberate divergence from Fresh: Fresh writes a **patched recipe** that the
host applies (unchanged parts copied on the host), while gvim streams the already-merged
bytes from the client into a remote temporary file and renames it. The outcome is the same
for files larger than memory; a server-side patched recipe remains later work, and the
whole-file transport cap for files loaded without windowing is unchanged. Remote files at or
under the threshold still load whole through `ReadFile` (64 MiB transport cap) exactly as
before, and an over-cap file with no windowed reader still fails clearly rather than opening a
partial buffer. Every byte is read and written through the authority: there is no local
fallback, no temporary copy of the remote text on the local disk, and a remote file that is not
UTF-8 fails with a clear `too large to transcode` error instead of loading whole.
## SSH config, ProxyJump and agent forwarding

gvim dials with x/crypto/ssh, not the `ssh(1)` binary, so it reads a **bounded subset** of
the user's OpenSSH config itself (`authority/ssh_config.go`). The app passes the default
path (`~/.ssh/config`); embedding/tests may disable it (`SSHOptions.ConfigPath == ""`).
A missing file is not an error: the no-config behaviour is unchanged. Files are capped at
256 KiB / 4096-byte lines / 256 Host blocks; a malformed or oversized file is a clear dial
error, never a silent fallback.

Supported directives: `Host` patterns (`*`, `?`, `!` negation), `HostName`, `User`,
`Port`, `IdentityFile` (accumulates, as OpenSSH does; `~` and the `%h`/`%r`/`%p`/`%d`/
`%u`/`%%` tokens are expanded), `ProxyJump`, `IdentityAgent` (`none` disables) and
`ForwardAgent`. Resolution is OpenSSH's first-obtained-value rule: global options, then
matching Host blocks in file order. A `user@host` target keeps its explicit user and an
`ssh://host:port` target keeps its explicit port; a config `User`/`Port` fills a target
that did not spell one (a URL's `$USER` fallback is **not** an override). A configured
`HostName` always replaces the alias for dialing; the status label keeps the alias the
user typed.

**ProxyJump** is supported for **one hop**: the target is reached through the jump host's
`direct-tcpip` tunnel over the same client, and the target's host key is still verified
against the target's known_hosts entry. A comma list or a jump that itself has ProxyJump is
a clear error. Jump-specific credentials are not read (the same agent/identity/password is
used for both hops) — a recorded residual.

**Agent forwarding** (`ForwardAgent yes` in config, or `SSHOptions.ForwardAgent`) forwards
the local agent through `x/crypto/ssh/agent`: the backend sets up the forwarding listener on
the client and sends `auth-agent-req@openssh.com` on every session (one-shot, long-running
and the integrated terminal), so a remote ssh/git inside a session can use the local keys.
Forwarding without any local agent is a clear error at dial. The forwarded agent is not
persistent across a reconnect in a way the remote process could rely on; the session gets
the new connection's local agent.

**Host keys and algorithms** stay explicit and conservative: no `StrictHostKeyChecking`,
`UserKnownHostsFile`, `HostKeyAlgorithms`, `Ciphers`, `MACs` or `KexAlgorithms` directive is
read from the OpenSSH config (residual); the transport's own `ssh` config section below sets
the policy. Host keys always go through `knownhosts.New` (or the injected test callback),
and an unknown host is a clear error naming known_hosts and how to add the key. No local
fallback and no `ssh(1)` subprocess are involved.

Also ignored (recorded residual): `Include`, `Match`, `ProxyCommand`, `Canonicalize*`,
`ForwardAgent` per-host with a local command, system-wide `/etc/ssh/ssh_config`, and
multiple jump hops.

Tests: parser/Host-pattern/first-value/token/limit and target-override tables
(`ssh_config_test.go`); a real two-in-process-server ProxyJump run (jump proxies
`direct-tcpip` to the target) and a fake-agent forwarding run whose server reads the local
agent's identities through the forwarded channel (`ssh_config_remote_test.go`); the existing
suite proves the no-config path is unchanged.

## SSH host-key policy, host-key algorithms and password keyring (gvim extensions)

These are top-level `ssh` keys in the user config (a project file cannot set them: host-key
verification is a user security choice). Fresh delegates SSH to `ssh(1)` and gets OpenSSH's
`StrictHostKeyChecking=accept-new` for free; the pure-Go backend makes the choice explicit.

- **`ssh.known_hosts_policy`** — `"strict"` (default), `"accept-new"` or `"off"`.
  - **strict** refuses a host whose key is missing from known_hosts and says how to add it
    (`ssh-keyscan`); the known_hosts file itself is never created or written.
  - **accept-new** writes a **first-seen** host's normalized `[host]:port` line to the
    known_hosts file (mode 0600; the parent directory is created 0700, the same convention as
    OpenSSH's default paths) and returns a notice through the transport's notice queue
    (`Authority.TakeNotices`), which the app shows in its startup/reconnect notice row: the
    user learns the host was trusted on first use. Subsequent connections are known.
  - **A changed host key is always refused**, under every policy, with a "HOST KEY CHANGED …
    possible man-in-the-middle" error; accept-new never overwrites an existing line.
  - **off** is accepted by the config parser only so the transport can refuse it with one
    clear error; there is no mode that disables host-key verification.
- **`ssh.host_key_algorithms`** — ordered server host-key algorithm preference. The default
  is `ssh-ed25519, ecdsa-sha2-nistp256, ecdsa-sha2-nistp384, ecdsa-sha2-nistp521,
  rsa-sha2-512, rsa-sha2-256` (SHA-1 `ssh-rsa` and DSA are never preferred by default);
  certificate variants and the legacy names are accepted when explicitly configured. An
  unknown or empty name is a clear error **before** the dial, naming the supported set;
  duplicates keep their first position. The list is applied to both the ProxyJump hop and the
  target. The verified host key's type is reported in the connection detail
  (`Authority.State().Detail`), e.g. `host key ssh-ed25519 · known_hosts strict ·
  algorithms ssh-ed25519, …`; the residual is that x/crypto/ssh does not expose the
  negotiated signature algorithm separately from the presented key type, so an RSA host key
  is reported by its key type rather than which RSA-SHA2 variant signed the handshake.
- **`ssh.keyring`** — default **false**: no OS keyring is consulted and no password is
  stored. When true, the transport asks `authority.SystemPasswordStore()` for the host
  password before the UI password prompt, and after the server accepts a password the user
  supplied, stores it and queues an honest notice. Storage happens only with this explicit
  consent and only for an accepted password. The real backends are:
  - **macOS**: `/usr/bin/security` — lookup `find-generic-password -s gvim-ssh -a
    user@host -w`, store `add-generic-password -U -s gvim-ssh -a user@host -w PASSWORD`.
    The password is passed as the `-w` argv value, which briefly exposes it to process
    listings (a recorded platform limitation); there is no shell.
  - **Linux**: `secret-tool` over the Secret Service (D-Bus) — lookup `lookup service
    gvim-ssh account user@host`, store `store --label=... service gvim-ssh account
    user@host` with the password on stdin, never argv. It needs a running keyring/DBus
    session; without one the failure is reported honestly.
  - The tool is used only when `exec.LookPath` finds it; otherwise `SystemPasswordStore`
    returns the no-op store and a store attempt reports "not stored in the OS keyring"
    (never success). All invocations are argv without a shell, bounded (64 KiB per
    answer), inherit the process environment, and honor the dial's context (cancellation
    kills the child). One entry is keyed by `user@host` (no port; the resolved alias host
    is used), the tool's one trailing newline is stripped and password bytes otherwise
    preserved (embedded/trailing LF and CR included; a password that the tool cannot
    round-trip unchanged is a platform limitation). A lookup error queues a notice and
    falls through to the prompt; a store error is reported as "accepted but not stored",
    never success. Password storage for 2FA/keyboard-interactive answers and passphrases is
    not covered. Tests use a fake command runner (no real keychain/DBus).
- The app maps `config.SSHConfig` to `Options.SSHSecurity` and the transport notices into the
  startup/reconnect notice text; the config parser validates the document shape
  (unknown keys/values are diagnostics) while the transport still validates the policy and
  algorithm names at connect time with clear errors.

Tests: policy transitions with an in-process server (strict unknown refuses and writes
nothing; accept-new records once with a notice and is trusted next time; a changed key is
refused and the file is untouched; off refuses), algorithm parsing/order/duplication/unknown
names and the negotiated detail (Ed25519 success, RSA-only list fails as "no common
algorithm"), and a fake password store (retrieve before the prompt, no store with the knob
off, store + notice with it on, unavailable store reported honestly, lookup error falls
through to the prompt).
