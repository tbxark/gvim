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
env map of its own, so capture cannot recurse). The captured delta is the environment the
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
  LOCAL launch sees that overlay too; a remote launch's plugin host (which runs locally) gets
  no project environment, so a remote-captured variable never leaks back into local children.
- Remote launches' environment (R13-13): `env.Remote` detects the configured markers on the
  remote host (one bounded shell probe of the marker files) and activates the recipe through
  the authority one-shot surface, diffing the remote activated environment against the remote
  login baseline. Its captured delta is what the remote terminal, shell/formatter runner,
  remote LSP, remote project search and git grep receive; the local overlay is passed to no
  remote process. Activation is trust-gated exactly like the local path. The provider is
  warmed in the background after the authority connects (the remote root is expanded first);
  spawn paths read the cached delta and never block the UI update loop on the remote host.
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
  large-file saves larger than the bounded merge (remote large files edit as an overlay
  and save by merging into one atomic whole-file write, up to remoteLargeSaveMax; Fresh's
  host-side patched recipe is the later slice), remote
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
- Remote environment residuals: the probe/capture runs through `sh -c` on the ssh exec
  channel with the remote login baseline, not through the user's interactive remote login
  shell, so a detector whose activation needs interactive-shell initialization may capture a
  different delta than a remote terminal would; changed remote env input files are not
  watched (Env: Reload or a new activation re-captures), the detection result is cached until
  Refresh/reconnect, the Settings Environment rows show the remote facts only after the
  background warm, and there is no status-row indicator of the active remote environment yet
  (Fresh shows one). Local behavior is unchanged.
- SSH authentication/connection residuals: ssh-agent forwarding and \`ssh_config\` reading
  (ProxyJump, Host aliases, IdentityAgent, host-key algorithms) are not implemented; the known
  hosts file is the default \`~/.ssh/known_hosts\` only; a supplied password/passphrase/2FA
  answer is kept in memory for the session (no keyring/agent integration); the passphrase is
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
- Plugin `gvim.fs`/`gvim.proc` still use the plugin OS port directly; routing them through
  the authority is the follow-up once remote file I/O exists.

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
session behavior, no process re-run); plugin gvim.fs/gvim.proc still use the local plugin OS
port in a remote workspace (the remote explorer/terminal/search do go through the authority);
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
errors. A remote delete is permanent: there is no trash on the remote host.

The remote explorer's write UI is wired (`internal/app/remote_explorer_ops.go`): New File,
New Directory, Rename and Delete run through `Files()`/`authority.Mutations(fs)` with the
workspace trust gate before every operation and the explicit "remote workspace is
disconnected; activate the window to reconnect" error while the authority is down. Names are
validated with the same `fsops.Validate` rules and must stay inside the remote workspace
root; a create or rename refuses an existing target (checked before the mutation — the
FileMutations surface has no exclusive create, so a check-to-write race is a recorded
residual) and a remote delete asks first and says it is permanent (no remote trash). After a
successful operation the existing explorer file-job refresh reloads the remote tree through
its reload command, so the update loop never waits on the host. Cut/copy/paste of remote
subtrees, drag/move and duplicate stay refused with one notice (a tree capability, not a
hidden local operation); real-host manual acceptance and undo/history for remote writes
remain open.

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
remote host and load as you scroll; a save writes the merged file in one atomic write, up to
<bound>`.

A save merges the overlay with the unchanged head/tail read back through
`FileSystem.ReadRange` in bounded chunks, then writes the whole merged file with the
authority's atomic whole-file write. Because that write takes the file as one byte slice, the
merge is bounded (`remoteLargeSaveMax`, 64 MiB): a larger merged file refuses the save with a
clear notice instead of risking the machine. The save is prepared on the editor goroutine and
runs in a `tea.Cmd` like every other save, so the update loop never waits on the carrier. A
remote file that changed after the plan refuses the save with the ordinary changed-on-disk
error; the plan's remote stat and the post-write stat both go through the authority. After a
successful write the save command opens a fresh windowed source for the new file and carries
it in the result, so `ApplySavePlan` rebases the buffer without any remote read on the update
goroutine. Undo, dirty state and rebase behave like a local large file.

This is still a deliberate divergence from Fresh: Fresh writes a **patched recipe** that the
host applies (unchanged parts copied on the host), so it can save files larger than memory;
gvim's bounded merge-and-atomic-write is the slice that fits the existing remote write port. A
server-side patched recipe and an unbounded (spooled) save are later work. Remote files at or
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
read (residual); host keys always go through `knownhosts.New` (or the injected test
callback), and an unknown host is a clear error naming known_hosts and how to add the key.
x/crypto's default algorithm set applies. No local fallback and no `ssh(1)` subprocess are
involved.

Also ignored (recorded residual): `Include`, `Match`, `ProxyCommand`, `Canonicalize*`,
`ForwardAgent` per-host with a local command, system-wide `/etc/ssh/ssh_config`, and
multiple jump hops.

Tests: parser/Host-pattern/first-value/token/limit and target-override tables
(`ssh_config_test.go`); a real two-in-process-server ProxyJump run (jump proxies
`direct-tcpip` to the target) and a fake-agent forwarding run whose server reads the local
agent's identities through the forwarded channel (`ssh_config_remote_test.go`); the existing
suite proves the no-config path is unchanged.
