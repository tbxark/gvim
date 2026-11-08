# Authority (workspace backend slot)

Status: first slice (#21a). The slot, the local backend, the remote-spec parsing, the SSH
backend for terminal and one-shot exec, and the connection-state hooks are implemented in
`authority/` and `internal/terminal/session_backend.go`. Wiring into the app/TUI is a
separate step (see §7); until it lands the editor still runs one local authority.

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
- Reconnect: `Reconnect(ctx)` closes the carrier and redials the stored target; a session
  that observes the death marks `Connected=false` with a reason for the indicator. Existing
  remote terminals are not resurrected (they are respawned through the slot on activation).

## 6. Remote file I/O (not in this slice)

The SSH `FileSystem` returns `authority.ErrUnsupportedFileIO`. Follow-up decision record:
x/crypto/ssh is cached but an SFTP implementation is not an offline dependency; the next
slice should either take `pkg/sftp` (dependency policy permitting) and implement
`FileSystem` over ssh sessions, or ship a small remote helper on the host (the daemon
protocol already exists) and route the same interface over it. Path translation, the
`PathMapper` seam and the spec parsing above are ready for either.

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
  the local session, opens the SSH terminal at `RemoteSpec.Workspace()` and says on the
  status row that remote file editing is the SFTP/remote-helper follow-up. It never
  silently edits local files. The status row shows `SSH:user@host` or
  `SSH:user@host · disconnected` (hand-written English this round; the statusbar owner
  extracts `gvim.status.remote_ssh` / `remote_ssh_disconnected` in the i18n slice).
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

- Remote file I/O is not routed: the editor's own file access still uses the local
  process, and a remote launch deliberately opens no files. The SSH FileSystem already
  returns `ErrUnsupportedFileIO`; the follow-up is SFTP (`pkg/sftp` when the dependency
  policy allows; no SFTP module is cached offline) or a remote helper, then routing the
  editor's file/watch calls through the slot.
- LSP spawns are not routed through `TLongRunning` yet; the surface exists.
- The remote workspace's trust decision uses the local workspace's trust in this slice
  (a remote workspace needs its own trust store in a later slice).
- A dropped carrier does not resurrect an open remote terminal; activation reconnects the
  carrier and opening the terminal again starts a fresh session.
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
- CLI and app rules: remote forms reach `Targets.Remote`, malformed/mixed launches are startup
  errors, and `Options.Validate` repeats them.
- TUI: the connected/disconnected segment, activation reconnect, the password prompt and
  the focus path, driven by a fake hooks set (no SSH needed).
- Manual acceptance for a real host is recorded in the round report (no CI host is used):
  `gvim user@host:/tmp/proj` with an agent key opens the remote shell in /tmp/proj,
  shows `SSH:user@host`, survives a network drop with `· disconnected` until the window is
  activated, and asks for the password through the prompt when no agent/key is set.
