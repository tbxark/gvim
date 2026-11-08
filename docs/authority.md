# Authority (workspace backend slot)

Status: #21a plus the round-13 remote completion. The slot, the local backend, the
remote-spec parsing, the SSH backend for terminal/one-shot/long-running children, the
remote `FileSystem`, and the connection-state hooks are implemented in `authority/` and
`internal/terminal/session_backend.go`. The editor's file I/O and the language servers of a
remote workspace are routed through the slot (§6), including automatic server restart after
a reconnect; the remaining residuals are listed in §7 (remote explorer, external-change
watch, remote large files, reconnect retry/backoff, remote trust store, terminal respawn as
an open parity gap).

## 1. The slot and its four surfaces

`authority.Authority` is the single answer to "where does this primitive run?". Consumers ask the
slot; they never branch on the backend kind. Four routing surfaces:

- `FileSystem` — `ReadFile`, `WriteFile`, `Stat`, `ReadDir`, `Watch`. The value
  types are the plugin OS port's (`plugin/hostos`), so the local adapter is forwarding.
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
  to expand.

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
  `cd <workspace> && exec "$SHELL" -l` (the remote login shell; the local
  `terminal.shell` override deliberately does not apply), stderr merged into the output,
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
- Atomic saves: the atomic path creates `mktemp` in the target directory, streams stdin into
  it with `cat`, applies the resolved mode, then `mv -f` over the target: a same-directory
  rename, which is the atomicity the local port relies on. The target's existing permission
  is preserved when `WriteOptions.Mode` is zero; an explicit mode replaces it. A symbolic
  link is resolved (bounded, 16 levels) and the target replaced, so the link stays a link —
  matching the local atomic path. A non-atomic write follows symlinks like `cat` and keeps
  an existing file's permission. These exact behaviours are asserted in
  `authority/ssh_files_test.go`, not assumed.
- Bounds and errors: collection is bounded during the transfer, not after it. The session
  pump writes into a bounded buffer that keeps draining after its limit (so the remote
  process never blocks) and records the overflow; an over-cap answer is an explicit error,
  never a truncated success. Exact caps: whole-file read 64 MiB (the caller's `max` is
  first capped to it; `head -c effective+1` detects oversize without transferring it),
  directory listing transfer 4 MiB on top of the 5000-entry cap, stat/write helper stdout
  64 KiB, and helper stderr 64 KiB (a failing helper's message is never parsed beyond the
  cap — the error says the error output was over the cap). Every call is bounded by its
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
- LSP over SSH (landed): the language servers of a remote workspace start on the remote
  host through the authority's `LongRunning` surface (a separate ssh exec channel with
  piped stdio), not as local `exec.Command`s. The manager gained a `SetSpawner` seam (nil =
  the local `lsp.Start`, so local launches are unchanged); for a remote launch the app
  installs a spawner that starts the configured command verbatim on the remote host in the
  remote workspace, overrides the initialize root URI with the remote workspace path
  (`~` expanded through the remote shell's `$HOME`), clears the locally resolved Program
  (the remote shell resolves a bare command in the remote PATH), keeps the remote login
  environment rather than the local env overlay, and feeds the remote child's stdio through
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
  runtime uses; `pluginhost.Wiring.OS` is exposed for this), with the project env overlay
  as the env provider and the workspace-trust check as the gate for remote launches.
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
  terminal only (the remote explorer is a recorded residual). The launch is scoped: no local
  file watcher, no local crash-recovery copy of remote text, and no local language server
  for remote buffers (`deps.LSP` is nil for a remote launch) — each is explicit here, not a
  global change. The status row shows `SSH:user@host` or `SSH:user@host · disconnected`
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
  remote, `--diff`, stdin and `--daemon`; `app.Options.Remote` repeats the rules in
  `Validate`.

Residuals of this slice (documented, not bugs):

- Remote residuals of the landed routing: remote explorer/listing (a directory target
  opens the terminal only; the palette/explorer still describe the local tree), no remote
  external-change watch (a file changed on the host is picked up by Revert/Reopen and by the
  disk check, not by fsnotify-in-the-background), remote large-file mode (a document over
  the 64 MiB read cap fails with the too-large error instead of opening lazily), remote
  crash recovery and hot-exit (deliberately skipped for a remote launch so remote text is
  never copied into a local cache), `~user` expansion, special remote files (the helper's
  kind mapping only knows file/dir/symlink; a FIFO read is bounded by the context and fails
  rather than blocking forever), and a remote save interrupted by a carrier drop surfaces
  an error (never a local retry or a second write).
- LSP over SSH residuals: the reconnect restart covers the servers that were running before
  the drop (a server the user stopped or a muted language is kept as chosen; a start that is
  already in flight when the carrier drops again is not cancelled mid-handshake — the
  context is checked between servers), each server gets one restart attempt per reconnect
  (no background retry/backoff), a remote server's `process_limits` are recorded but cannot
  be applied (the local pid is the ssh client), the remote command is resolved by the remote
  login shell's PATH only (no remote install probe; a missing server surfaces the start
  error), and the remote workspace's trust decision still uses the local workspace's trust
  (a remote trust store is a later slice). Remote terminal respawn is an open parity gap:
  the safe version (respawn the recorded login shell or a user-approved argv, transcript
  preserved, never a silent replay of an arbitrary command) is not landed yet, so the
  terminal is restarted explicitly today.
- The remote workspace's trust decision uses the local workspace's trust in this slice
  (a remote workspace needs its own trust store in a later slice).
- A dropped carrier does not resurrect an open remote terminal (open parity gap, see
  above): activation reconnects the carrier, and opening the terminal again starts a fresh
  session set by the user.
- Plugin `gvim.fs`/`gvim.proc` still use the plugin OS port directly; routing them through
  the authority is the follow-up once remote file I/O exists.

## 8. Tests

- Local parity: forwarding exactly matches direct OS-port calls (files, errors, env merge,
  gate refusal, watch events, shell probe).
- Spec parser: both forms, defaults, ports, IPv6, percent-decoding, line/column suffixes,
  colons in paths, windows-ish/local forms, malformed `ssh://` errors.
- Fake authority: state transitions and reconnect for the indicator/activation hooks.
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
- CLI and app rules: remote forms reach `Targets.Remote`, malformed/mixed launches are startup
  errors, and `Options.Validate` repeats them.
- TUI: the connected/disconnected segment, activation reconnect, the password prompt and
  the focus path, driven by a fake hooks set (no SSH needed).
- Manual acceptance for a real host is recorded in the round report (no CI host is used):
  `gvim user@host:/tmp/proj` with an agent key opens the remote shell in /tmp/proj,
  shows `SSH:user@host`, survives a network drop with `· disconnected` until the window is
  activated, and asks for the password through the prompt when no agent/key is set.
