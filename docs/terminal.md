# Terminal dock

This is the design record for the integrated terminal: the shells the dock runs
(terminal tabs and agent sessions), what the dock shows, the keys and mouse it
takes, its selection and persistence, and what is deliberately not built yet. The code is
`internal/terminal` (PTY, emulator, session API), `internal/tui/dock` (panel,
keys, painting, scrollback, selection, mouse, links), `internal/tui/dock_glue.go`
(the root glue), `internal/restore` (the saved transcript) and
`internal/uiadapter/terminal.go` (the shell starter and clipboard).

`internal/terminal` runs the shell in a PTY on Unix (`creack/pty`) or in a ConPTY
pseudo console on Windows (`x/sys/windows`), and models its screen with
`charmbracelet/x/vt`. The dock paints that screen into the frame's bottom band.
The parity row and the remaining rounds live in `docs/parity.md`.

## Two views: live and scrollback

The dock has one shell and two views of it:

- **Live**. The shell screen, with the shell's caret; keys go to the shell. This
  is the default, and it is what an exited shell keeps showing (frozen on its
  last screen).
- **Scrollback**. A pager over the emulator's main-screen history followed by the
  live screen rows (and, after a restart or a session restore, the previous
  transcript above them). It is read-only: it never writes to the shell and it
  shows no caret. The title row says
  `Scrollback — rows a-b of n (Ctrl+Space returns)`, and turns into
  `… — /query k/n` while a search is open (with ` rx` / ` case` markers for its
  options). A restored transcript says `Scrollback (restored)`.

The switch is **Ctrl+Space** while the dock has the focus; the dock reads the key
before the keymap, so no preset binds it. **Ctrl+]** is the only chord that gives
the keyboard back to the editor. Inside scrollback, **Esc** also returns to the
live screen. **Shift+PageUp** enters scrollback from the live screen, as Fresh's
does. A shell on the alternate screen has no history, so Ctrl+Space only reports
`The alternate screen has no scrollback.` there. Keyboard capture (F9) keeps
every key for the shell, including Ctrl+Space; turn capture off to reach
scrollback. Losing the dock's focus leaves scrollback, so the dock is live the
next time it is focused; a restored transcript keeps its read-only view instead.
An exited shell can take the focus again (a click, or the focus commands), and
Ctrl+Space then opens the read-only pager over its frozen screen and history.

Scrollback keys:

| key | action |
| --- | --- |
| Ctrl+Space, Esc | back to the live screen (not on a restored transcript) |
| Up, Down | move the selected row by one |
| PageUp, PageDown | move by a screen |
| Home, End | oldest row, newest row |
| `/` or Ctrl+F | open the search input; type the query, Enter commits, Esc cancels |
| Ctrl+R | toggle a regular-expression search |
| Ctrl+A | toggle a search that respects case |
| n, N | next, previous match |
| y | copy the selected row (or the selection) |
| Ctrl+C | copy the visible rows (or the selection) |

The search matches the prior transcript, the x/vt history and the live screen
rows; a bad regular expression is reported as a notice and keeps the previous
matches. A copy goes through the same clipboard path the editor's copy uses (the
terminal clipboard command and, when the app has one, the native clipboard
writer).

The history snapshot is fetched in a `tea.Cmd`: `Update` never waits for the
session, and the rows it copies are bounded (`terminal.MaxScrollbackSnapshot`).
A snapshot carries the shell generation, so a late answer from a shell that has
since been replaced is dropped. **New output returns the view to the live screen**
(Fresh's automatic scroll) unless a text selection is active; set
`terminal.jump_to_end_on_output = false` to stay in scrollback.

The mouse wheel is another way in: on the live main screen of a shell that did not
ask for the mouse, an upward notch enters scrollback and scrolls three rows. In
scrollback, the wheel scrolls the window.

## Text selection and copy

The dock has a read-only text selection over its combined rows: a restarted
shell's prior transcript, then the x/vt history, then the live screen rows. It is
owned by the dock; the session stays read-only and nothing is typed into it.

- A bare left click only focuses the dock. A left **drag** on the live grid drops
  the view into scrollback (the same transition as Ctrl+Space) and starts a
  character-flow selection anchored where the press was; the release keeps it.
  Dragging in scrollback selects the same way. `terminal.mouse_drag_selects =
  false` makes live-grid drags inert again while scrollback drags still select.
- **Shift+drag** bypasses mouse forwarding and selects even when a program owns
  the mouse (the legacy `alt_screen` forwarding mode has no Shift bypass, as in
  Fresh). An unshifted press still forwards to a mouse-hungry program.
- In scrollback, a **double click** selects the word under the cell and a
  **triple click** the whole row; on the live grid both stay inert (Fresh's rule).
  A drag after a word or line click extends by whole units, as Fresh does: the
  clicked word or row stays selected and the selection snaps to whole words (or
  whole rows) under the pointer.
- Selected cells are painted with the selection background, including blank cells
  past a row's text. `y` and Ctrl+C copy the selection through the clipboard
  path; with no selection they keep their older meaning (the selected row and the
  visible rows). A plain click clears the selection, and so does leaving the
  scrollback view.
- Selection positions are absolute combined rows. The absolute row of existing
  content does not change when the shell prints: history is append-only, and a
  screen row that scrolls into history keeps its absolute index, so new output
  appends below the selection instead of moving it. A selection holds the view
  against automatic scroll.
- **Drag auto-scroll**: dragging a selection above or below the visible rows
  scrolls the scrollback one row about every 80 ms (fixed, no acceleration, at
  most the same one-row step at either end), and the selection keeps extending to
  the edge row of the new window, so a selection can run past the window. The
  repeat stops at the top or bottom of the combined rows, when the pointer comes
  back inside, on release, when the selection is cleared, and when the dock loses
  the focus; the release may land outside the dock's rows.

## ConPTY on Windows

Windows has no Unix PTY: the Windows path is a **ConPTY** pseudo console
(`internal/terminal/pty_windows.go`, `//go:build windows`, pure Go through
`golang.org/x/sys/windows`). It creates two anonymous pipe pairs (child input,
child output), calls `CreatePseudoConsole` with the child-side handles, puts the
console handle in a `STARTUPINFOEX` attribute list (`PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE`),
and starts the shell with `CreateProcess` (`EXTENDED_STARTUPINFO_PRESENT |
CREATE_UNICODE_ENVIRONMENT`, a composed command line and a UTF-16 environment
block). A resize calls `ResizePseudoConsole`; closing kills the child, closes the
console and the pipes (`ClosePseudoConsole`). A failure to create the console is
reported with the Windows requirement: Windows 10 version 1809 (build 17763) or
later, or Windows Server 2019.

**Verification gates** (all run by CI-style checks on any host):

    GOOS=windows go build ./...
    GOOS=windows go vet ./internal/terminal
    GOOS=windows go test -c -o /tmp/terminal.windows.test.exe ./internal/terminal

The Windows-tagged unit test covers the pure helpers (the environment block and
the exit-status error); the console and process calls cannot run off Windows.

**Real-Windows acceptance checklist.** Parity is **unverified** until this pass is
done on a Windows 10 1809+ / Windows 11 host:

1. start gvim, open the dock, and check `cmd.exe` starts and paints its banner
   and prompt;
2. run `pwsh` (or PowerShell) from the dock and check its prompt, colours and a
   full-screen command (`cls`);
3. type a command with a pipe and redirection, run `dir`, and check the output
   and the exit code;
4. resize the window several times: the child sees the new size
   (`mode con`/`$Host.UI.RawUI.WindowSize`), and the live screen clips/pads;
5. exit the shell: the last screen stays read-only, the status row offers the
   restart, and restart keeps the transcript above the new shell;
6. scrollback: produce more than one screen of output, Ctrl+Space, select text,
   copy it; links in the scrollback still open;
7. Ctrl+V paste and Ctrl+C forwarding behave as on Unix;
8. close and reopen gvim with session restore on: the saved transcript returns
   `Scrollback (restored)`, read-only, and its transcript reflow behaves as on
   Unix.
Record the pass in `docs/parity.md`; until then the Windows row reads unverified.

## Terminal tabs

The dock runs up to 16 shells at once as **terminal tabs**. Every tab is the same
single-session terminal the dock had before (its own screen, scrollback, prior
transcript, selection, links and launch record), so switching tabs loses nothing
and a background shell keeps running: the dock syncs every tab (output/exit waits,
resizes, queued input), paints only the active one, and still keeps the
background tabs' exited markers current.

- **Strip.** The dock's title row is the tab strip: ` Terminal 1  build  Terminal 3 `,
  the active tab in the tab-strip active style, an exited tab with an `(exited)`
  marker, and a click on a label selects that tab. A tab is titled by its explicit
  rename; without one it takes the name of the program it runs, as Fresh does: the
  base name of the launch record's argv (the agent's configured argv, for a Run
  Agent tab), else the launch record's agent name, else `Terminal N`. Duplicate
  titles get Fresh's ` (2)`, ` (3)` suffix, and because the launch record is
  saved per tab, a restored dead tab comes back with the same title plus the
  `(exited)` marker. Labels have control characters stripped and are truncated to
  one tab's cell budget with an ellipsis, counted in terminal cells, so a wide
  glyph is never split and the strip's click targets measure the same cells.
- **Keys.** While the dock has the keyboard focus, Ctrl+PageDown / Ctrl+PageUp
  switch to the next/previous tab (wrapping); the keys work with or without
  keyboard capture, but not while the scrollback pager owns them. They are not
  keymap data. Everything else goes to the active shell as before.
- **Commands.** New Terminal Tab, Close Terminal Tab, Next Terminal Tab, Previous
  Terminal Tab, Rename Terminal Tab and Move Tab To Left / Move Tab To Right
  (hand-written English titles; the Move rows are Fresh's tab_actions command
  names, no Fresh i18n keys for the dock tabs); Rename opens a one-line prompt
  prefilled with the current name, and an empty answer restores the default
  `Terminal N`. Run Agent... is the terminal's agent row (see Agent sessions and
  Run Agent). There are no default keymap bindings beyond the documented dock keys.
- **Reordering.** Move Tab To Left / Move Tab To Right move the active tab one
  slot (the palette rows, and Ctrl+Shift+PageUp / Ctrl+Shift+PageDown while the
  dock has the keyboard). Fresh's tab drag is the mouse path: press a tab label,
  drag more than three cells (Fresh's threshold), and release over the strip. The
  drop inserts on the pointer's side of a label: its left half inserts before that
  tab, its right half after, and past the last label at the end; a `┃` marker in
  the active-tab style shows where it will land. A press that never passes the
  threshold is a click, and a drag released off the strip cancels without
  switching tabs or starting a selection. Reordering only permutes the tab list: a
  tab keeps its name, transcript, launch record and running PTY, the active tab
  follows its own move, and the dock does not start, resize or close any process.
  Shift on a page key only arrives in terminals that report modified keys
  (xterm-style CSI 5;6~ / CSI-u); where Ctrl+Shift+PageUp is indistinguishable from
  Ctrl+PageUp, the chord switches instead and the palette rows and the drag remain
  the guaranteed reorder paths. gvim's dock has no splits, so Fresh's drag-to-split
  and per-tab split moves are not offered (see Deferred).
- **Lifecycle.** A new tab starts its own shell in the workspace root and takes the
  focus. Closing a tab closes its shell immediately (gvim has no running-process
  prompt; closing the editor prompts as before) and removes the tab; closing the
  last tab leaves the dock closed. Restart restarts the active tab's shell with its
  transcript kept (and, when the launch record and terminal.resume_agents say so,
  rejoins its agent — that metadata is the launch record too). `close_on_exit`
  is per tab: a tab whose shell exits under it is removed rather than closing the
  whole dock; the last tab exiting hides the dock. A tab whose shell exits with
  close_on_exit off stays as read-only output with the exited marker.
- **Persistence.** `terminal.json` gains an additive `tabs` array (name, open, cwd,
  launch_dir, width, rows and the same launch/resume/agent/ephemeral record per tab)
  plus the active index; the top-level fields keep mirroring the active tab, so a
  reader that predates tabs restores that one tab exactly as before, and a record
  without `tabs` restores as one terminal. The format version stays 1. `cwd` is the
  terminal's reported directory and `launch_dir` the directory a recorded program
  was started in; both are additive, `launch_dir` is bounded on load, and an older
  record without `launch_dir` falls back to its `cwd`, so the format stays
  migration-safe. Bounds:
  at most 16 tabs, each with the existing per-transcript bound (500 rows / 1 MiB,
  trailing blanks trimmed, control characters stripped); a corrupt file cannot
  make the loader allocate more. The order of the `tabs` array, not the tab
  names, is the saved tab order, and the active index tells which tab is shown on
  restore, so a reorder survives an editor restart. The record also gains an
  additive `live` flag per tab (and mirrored at the top level): whether a process
  was running at the capture. It is optional, the format version stays 1, and an
  older record without it is exited, so migration keeps the pre-live-restore
  behavior. **Restore itself never starts a process**: every restored tab comes
  back as the read-only transcript the record describes, with its saved name and
  active index. A tab that was **exited at the capture** (`live` false, the old
  record's meaning) stays that way — read-only/dead with its `(exited)` marker —
  and only an explicit Restart starts anything. A tab that was **live at the
  capture** is dormant rather than exited and shows no `(exited)` marker; the
  first time the user *dives* into it after the restore (switches to the tab,
  focuses the dock on it, or opens the dock on it) the dock revives it with a
  fresh PTY: the recorded argv precedence (agent-resume when
  `terminal.resume_agents` is on, else the launch argv, else the plain shell) in
  its recorded `launch_dir`, with the saved transcript kept above it. This is
  Fresh's Live restore: the PTY is ephemeral and re-spawned on the dive, and an
  agent resume is deferred to that dive rather than done by the restore. The dive
  is attempted once: a failed start is a notice, not a retry loop (Restart still
  works). The gvim extension `terminal.restore_live` (default `true`) turns the
  dive revival off, making every restored tab read-only. In daemon mode the tab
  set is scoped per workspace (see Terminal tabs in daemon workspaces below).
- **Screen snapshot.** Next to the text rows the record carries an optional
  `screen` object: a bounded, **styled** snapshot of the visible screen (the
  screen width, rows of cells with the rune, foreground/background as `#rrggbb`
  and bold/underline/reverse). It is additive and omitted when there is nothing to
  keep — the format stays version 1 and an old record restores the plain text
  transcript exactly as before. Bounds: at most 200 rows and 512 columns; trailing
  blank cells are trimmed, control characters become spaces and invalid colours
  are dropped when the file is loaded, so a corrupt snapshot cannot paint
  garbage. On restore the snapshot replaces the styled tail of the saved rows only
  when its row texts match that tail **exactly**; a mismatched or corrupt
  snapshot is ignored whole and the plain rows stay. A revived tab therefore
  paints the saved screen with its colours/attributes above the new shell output
  (visible in the read-only view; the new output appends below), and a tab that
  was restored but not revived keeps its styled prior tail if it is saved again.
  A save after a revive captures the new live screen as the snapshot, so styles in
  older transcript rows beyond the visible tail are text-only from that point on
  (bounded-fidelity residual). Tab identity, order, the active tab, the
  revive-once rule and `terminal.restore_live` are unchanged.
- **Modes.** Fresh's explicit Live and Scrollback modes map to the dock's two
  views: a restored or exited tab opens read-only in the scrollback view
  (`Scrollback (restored)` / the frozen screen); Ctrl+Space toggles between that
  read-only pager and the live screen whenever a shell is attached; the first dive
  into a live-at-capture tab revives the PTY into **live mode** with the saved
  transcript (and its styled screen snapshot) above the new shell, and the pager
  still reaches both. An exited-at-capture tab never leaves the read-only mode
  until an explicit Restart.
### Terminal tabs in daemon workspaces (#19a)

The daemon workspace layer owns one runtime and model per materialized workspace,
so each workspace has its own dock and its own tab set: switching workspace swaps
the model, never a tab list, and tabs cannot bleed across workspaces. A workspace's
terminal record is `terminal.json` in its per-workspace session directory
(`sessions/<stable-id>/`, next to `session.json`), in the same `SavedState`
version-1 format (tabs, active index, launch records and the styled screen
snapshot; the bounds above apply), so a workspace switch/restart restores the same
screen fidelity as a local session.
The workspace registry file keeps identity and the editor layout, never the
terminal record.

- **Switching.** Selecting a workspace captures the outgoing one — its tab names,
  active tab, transcripts and launch records are snapshotted into its session
  directory — and shows the target. A target that is still warm keeps its live
  tabs and their PTYs: switching away does not kill a shell. A dormant target is
  materialized and its saved tabs are restored read-only/dead with their `live`
  flags, exactly as a single restored terminal: nothing starts automatically, and
  a tab that was live at the capture revives on the first dive into it in that
  workspace's dock (same Fresh parity, `terminal.restore_live` and
  `terminal.resume_agents` gates), while an exited tab stays dead until an
  explicit Restart. The outgoing workspace stays warm, so switching back returns to
  the same live shells and the same tab state.
- **Closing.** Closing a workspace (or quitting the daemon) saves its capture and
  closes its runtime, which closes the dock and kills its shells. The registry file
  is removed but the session directory is kept, so the workspace can come back with
  its transcript; nothing from a closed workspace runs.
- **Scope.** New Terminal Tab starts in that workspace's root; `close_on_exit`,
  the tab limit and the quick-switch keys are per workspace dock. The workspace
  label/indicator and the switcher are the workspace layer's UI (#19a); the dock
  strip only lists the tabs of the active workspace.
- **Verification.** `internal/app/workspace_terminal_tabs_unix_test.go` drives a
  real dock shell through switch-away/switch-back (same runtime, same pid alive),
  closing a workspace (pid killed, session directory kept), a two-tab capture and
  restore across a fresh workspace manager (names, active index, transcripts,
  launch records; no duplicate tabs on repeated capture), and read-only restore
  with no auto-launch (the shell's pid marker is untouched).

Residuals of this pairing: Fresh respawns a restored live terminal on focus and
fills it from a persisted **backing file** (full scrollback bytes plus a screen
snapshot); gvim persists bounded text rows plus the bounded styled visible-screen
snapshot described above, revives on the explicit dive with a fresh PTY, and
`terminal.restore_live = false` turns that off. Full backing-file semantics
remain out of scope: history beyond the visible screen is text-only (no per-cell
styles/bytes), and cursor state, alternate-screen/OSC state and hyperlinks are not
restored. Fresh can bring live tabs back without a dive because its layout focuses
the tab; gvim's dock is not restored into an editor split, so the dive is the
first user action on the dock. A buffer shared across workspaces and extract-tab
are the #19b policy decisions and remain open.

## Persistence and reflow

The dock's transcript survives an editor restart through the session directory:
`restore.SaveTerminal` writes `terminal.json` next to the session file (and
removes it when the dock has neither a shell nor a transcript). It holds a version,
the dock's open flag, its directory (the shell's last OSC 7 directory, else the
start directory), the pane width the transcript was captured at, and the newest
transcript rows: the prior transcript, the bounded history window and the live
screen rows, as text with trailing blanks trimmed and control characters stripped,
capped at 500 rows / 256 KiB (the loader re-applies the same bounds and ignores a
version it does not know). It also carries the optional bounded, styled visible
screen snapshot described under Terminal tabs; a restore overlays it on the
matching text tail, and an old record without it keeps the text-only transcript.

The state is only restored on the full session-restore path (not `--no-restore`
or a hot-exit-only recovery). **A restored terminal is read-only until the user
acts**: running processes are not persisted, and the dock shows
`Scrollback (restored)` over the saved transcript. A terminal that was live at
the capture revives with a fresh PTY on the user's first dive into the tab (see
Terminal tabs above; Fresh's Live restore, gated by `terminal.restore_live` and
the argv precedence under `terminal.resume_agents`); one that had exited stays
dead, and Restart starts a shell in the saved directory and appends its output
below the transcript. Ctrl+Space cannot return to a live screen that does not
exist yet — the dive is what brings the live screen back.

**Transcript reflow.** The live screen is x/vt's: a resize clips or pads it, and
the history rows keep the cell widths they had when they scrolled off. The stored
transcript (the prior rows a restarted shell keeps and the restored transcript) is
re-wrapped, through `terminal.ReflowLines`:

- the dock records the width the rows were captured at (the live shell's screen
  width) or the saved width of a restored transcript; when the pane's content width
  changes, the stored rows are re-wrapped to the new width once, and a saved
  transcript whose width differed says `Scrollback (restored, rewrapped)`;
- each row keeps its cell styles and wide glyphs (a double-width cell and its
  continuation stay together; a wide glyph is never split), trailing blanks are
  dropped, and every stored row stays a hard row;
- this is a **text re-wrap, not a semantic reflow**: the transcript stores display
  rows, not the soft/hard newline structure that produced them, so a row that was
  itself a soft-wrapped continuation is not rejoined with the row above it when
  the pane widens (running text re-wraps approximately). The live screen is still
  clipped and never reflowed (x/vt has no reflow), which stays a documented
  residual.

## Restart and agent resume

`Restart` (the palette's Restart Terminal Process and the status row's restart
affordance) starts a shell in the last OSC 7 directory (falling back to the
directory the shell was started in, then the workspace root). It first captures
the old shell's bounded transcript into the prior rows, so a dead shell's output
stays above the new shell's output — the minimum of Fresh's
restart-into-the-same-transcript. The command also works on a live shell; it then
appends that transcript too.

An agent terminal restarts through Fresh's precedence: the recorded **agent resume
argv** when `terminal.resume_agents` is on (the default), else the recorded
**launch argv**, else the plain configured shell. A recorded program restarts in
the directory it was **launched** in — the Run Agent dialog's directory answer is
saved as `launch_dir` and is kept apart from the terminal's reported cwd, so a
program that changes directory (or reports a different one) cannot move a resume
away from what the user chose. Plain shell tabs keep the live-directory rule above.
The restart is always user-initiated — the palette row, the status affordance or a
tab's restart — and nothing is ever re-executed automatically by a restore. A
failed start keeps the record, so a retry still resumes; the transcript of the old
shell stays above the new one, as with any restart. The launch record is also why
Run Agent's sessions survive an editor restart as dead, restartable tabs (see
Agent sessions and Run Agent).

## Agent sessions and Run Agent

The dock tracks how a terminal was launched. Each tab keeps a **launch record**:
the launch argv (program + arguments, passed to the PTY one argv element at a
time, never through a shell), the agent name it came from (empty for a plain
shell), the agent's resume argv when it has one, and whether the tab is an
**ephemeral** session terminal (an agent started by Run Agent) rather than a
user's shell. The record is bounded (at most 64 argv entries of 1024 bytes, agent
name 64 bytes), holds no environment variables and no clipboard/credential text,
and is persisted with the transcript so a restart can rejoin the session.

**Agent metadata convention.** gvim has no plugin agent registry (Fresh's agent
data comes from its plugins); the known agents are a config list:

```json
{
  "terminal": {
    "resume_agents": true,
    "agents": [
      { "name": "claude", "command": "claude",
        "args": ["--permission-mode", "plan"],
        "resume_args": ["--resume"] },
      { "name": "codex", "command": "codex",
        "args": [], "resume_args": ["--continue"] }
    ]
  }
}
```

`name` is the id the picker shows and the record stores; `command` + `args` is the
new-session argv; `command` + `resume_args` is the argv that rejoins the previous
session. `terminal.agents` is a gvim extension (Fresh has no such key) and is
**user-layer only**: a repository must not choose programs gvim runs. The list is
bounded (32 agents, unique names, at most 32 arguments each, one line per string);
a bad entry is a config diagnostic and the list is left unchanged. The config also
accepts `terminal.resume_agents` (Fresh's key, default on): with it off, a restart
re-runs the launch argv instead of the resume argv. An agent or resume program is
a local executable in a local PTY: the workspace authority (a remote/sandboxed
shell) applies to the dock's plain shell tabs only, because a re-parenting
authority would launch the remote login shell and ignore the agent argv.
Separately, a remote workspace's project search, Git Grep and project replace run
through the authority: the searches are bounded one-shot commands, and a replacement
reads and atomically rewrites each matched remote file through the authority file
system (same match semantics as locally, stale/changed files skipped, bounded
size/file/time budgets, cancellation, and a partial-write report with no cross-file
rollback or undo). Those are commands and file operations, not PTY sessions, so they
never reparent an agent.

**Run Agent...** (hand-written palette row; Fresh has no command of this title
because its agent sessions are plugin data) is a two-question dialog. The first
picker lists the configured agent names and filters as you type; choosing one
opens the second picker with the launch choices: **New session** (the agent's
`command` + `args`) and, when the entry has `resume_args`, **Resume last
session** (the recorded resume argv, a user-initiated rejoin that is allowed even
with `terminal.resume_agents` off, which only gates automatic restarts). It shows
where the terminal starts (the current workspace root, with a **Change
directory…** row that opens a directory prompt; the answer is stat-checked in a
command, not on the update loop, and a missing path is stated instead of
starting) and whether a later Restart will resume, with the
`terminal.resume_agents` state spelled out on the resume row. The chosen
agent starts in a new ephemeral dock tab named after it, and its launch record
makes Restart the resume flow. With no configured agents the row shows a notice
pointing at `terminal.agents`; there is no dock-row integration beyond starting
the tab (see Agent discovery for tmux sessions that already run outside gvim).

**Exit and restore.** A terminal that dies keeps its transcript and its launch
record, shows `(exited)` in the tab strip (and the agent name in the scrollback
title), and offers Restart. A restored session brings the tabs back **read-only and
dead**: no PTY is started, no agent is resumed, and only the user's restart uses
the saved record. This is deliberate (Fresh's rule): reopening a workspace never
silently re-runs a finished process, which for an agent would mean resuming the
conversation unasked.

## Agent discovery (tmux and local transcripts)

`internal/agentdiscover` finds agent work that already exists outside gvim so it
can be rejoined instead of started again, from two sources: tmux sessions (a
client is running or was left behind) and the local conversation stores of the
Claude Code and Codex CLIs (the conversation exists on disk, whether or not a
process runs now). It is **read-only discovery**, not control: a tmux tab attaches
the existing client at level 0 in Fresh's terms (tmux draws the panes, so gvim's
splits/scrollback/search stop at the attached client; control mode `tmux -CC` is
explicitly not built), and a transcript row starts the agent's own resume command.

- **Scan.** Walk `$TMUX_TMPDIR` (else `/tmp`) for `tmux-<uid>/` directories and
their sockets, then, when commands are allowed, run
`tmux -S <socket> list-sessions -F ...` and `list-panes -a -F ...` per socket.
Both are listing commands: no shell, no session is started or attached, and the
scan never writes. A scan is bounded (2 s per command, 256 KiB per command
output, 200 sessions, 2000 panes, 500 sockets, 2000 socket dirs) and returns a
bounded problem list instead of an error, so a missing tmux, a stale socket, or a
permission failure is shown as a stated reason.
- **Local agent transcripts.** The Claude Code store is
`~/.claude/projects/<bucket>/<uuid>.jsonl`; only that depth is listed (a deeper
transcript is a sub-agent's, shares its parent's id and cannot be resumed on its
own). The Codex store is `$CODEX_HOME/sessions/YYYY/MM/DD/rollout-*.jsonl`
(`CODEX_HOME` else `~/.codex`); a rollout whose `session_meta.originator` names
the ChatGPT desktop app is skipped, because it is a different product and gvim
knows no resume command for it. For each transcript the scan reads only a bounded
header (256 KiB for Claude, 64 KiB for Codex; empty, malformed or truncated lines
are skipped), takes `cwd` and the session id from the records (Claude's bucket name
is lossy and is only a fallback title), keeps the newest 50 per store before
reading, and stops a store walk at 5001 entries. The whole scan is bounded and
returns a problem list instead of an error. It starts **no process and runs no
command**: only the two stores are read, after an explicit user action. Transcript
bytes and session ids are untrusted data: a session id has to be a short
alphanumeric/`-._` token before it is placed in the fixed resume template, and
nothing else from a transcript becomes an argv. Claude resumes exactly
(`claude --resume <id>`); Codex has no id-addressed resume, so its fixed command is
`codex resume --last`, which rejoins the newest session in that directory — the row
says so, and the resume is inexact.
- **Rows.** A tmux session carries its name, `session_path`, creation time, attachment
state, pane count and the attach argv qualified by socket
(`tmux -S <socket> attach -t <name>` — a bare `tmux attach` cannot reach a server
on a non-default socket). Panes whose foreground command is a known agent name
(claude, codex, gemini, goose, aider, cursor-agent, copilot, qwen, droid,
opencode, amp, plus the config's `terminal.agents` command basenames) are
reported as **probable agents**: a name match, not an identity. A live server
with no sessions and a socket whose server is gone yield no row; a socket that
was not queried (commands off) or whose server failed is a weaker socket-only
row saying a server exists but its sessions are unknown.
- **Trust gate.** Listing sessions runs commands, so a scan is only allowed from
an explicit user action and only when the workspace trust policy allows spawning.
`internal/agentdiscover` takes an injected command runner; the app's runner first
calls the session's workspace spawn gate (`trust.DecideSpawn` with the live trust
level and the inherited PATH), then resolves the command to an absolute path with
the same PATH/cwd rules (`trust.ResolveSpawnPath`) and executes that exact path
through the bounded `ExecRunner` (no shell). The second resolution is not
redundant: os/exec's LookPath runs in the parent process before `cmd.Dir` applies,
so letting it resolve a relative PATH entry would run a different file than the
gate judged. So Trusted works, Blocked refuses every scan command, and Restricted
only refuses a tmux executable that resolves inside the workspace; a refusal
becomes a stated scan problem and the socket-only answer, which the picker shows.
The scan never runs from the update loop: the picker starts it as a command and
the report arrives as a message.
- **Discover Agent Sessions...** is the palette row (`discover_agent_sessions`,
hand-written English title; Fresh's command is 'Orchestrator: Import sessions',
and this row's Import is a secondary action on the discovered session rather than
a workspace-management command). It opens a filterable picker at once in a
scanning state; Ctrl+R scans again without leaving it, Esc closes it. A row reads
the tmux session name plus 'attached' and the probable agent name(s), with the
server, pane count and path on its detail line. Enter opens the session's
qualified attach argv in a **new ephemeral dock tab** named `tmux:<session>`
(plus the agents when known); the tab records the attach argv as both its launch
and its resume argv, so a user Restart reattaches. Its tab title and persisted
launch/agent metadata flow through the same terminal record as a Run Agent
session. A transcript row reads `Claude Code · <directory>` or `Codex CLI ·
<directory>`, with the resume command and the directory (and Codex's
inexactness note) on its detail line. Enter on it starts the agent's fixed resume
argv in a **new ephemeral dock tab** in the transcript's recorded directory; the
tab records the resume argv as launch and resume and a durable source identity
(`claude-code:<id>` / `codex-cli:<id>`), so choosing the same transcript again
focuses the tab instead of starting a second copy. A transcript with no recorded
directory cannot be resumed or imported (the row says why).
**i** imports the selected session as a workspace: it asks the daemon's
workspace layer to create (or reuse) the workspace for the session's
`session_path` as root, labelled `tmux:<session>`, **and to start the session's
attach argv in that workspace's dock once it is materialized and shown** — the
terminal is an ephemeral tab whose launch and resume argv are the qualified attach
argv, started in the session's directory, and recorded with a durable source
identity (`tmux:<server>/<session>`). The registry enforces one workspace per
canonical root, so a second session in the same directory reuses that workspace
and opens a **second attach tab** for its own session (the selected session is
never silently dropped); importing the same source again focuses the existing
attach tab instead of opening a duplicate. A daemon build whose workspace layer
predates the queued launch falls back to create-only with a notice that states the
reduction and prints the attach argv; a direct launch (no workspace layer) states
that instead of importing. Restart of the imported tab reattaches (the attach argv
is both launch and resume). Empty states are stated: no tmux/socket, no sessions,
socket exists but the server was not queried, a session with no directory cannot
be imported, or the scan's bounded problem list ('Discovery problems:').
- **Limitations.** Discovery is a snapshot: it does not watch the stores, so a new
conversation appears on the next explicit scan (Ctrl+R), and a transcript row says
a conversation exists, not that a process runs. Codex's resume is inexact
(newest-in-cwd), and its desktop-app sessions are not offered. Sub-agent Claude
transcripts are not listed, and a transcript whose records carry no cwd cannot be
imported. Everything is local only: no remote-machine stores. Registry v1 still
stores one workspace per canonical root and
has no initial-terminal-launch field, so a second tmux session sharing a directory
gets its own attach tab in the shared workspace but not its own workspace (layout
and files are shared; per-session workspaces need a source identity in the
registry). The attach tab is queued in the workspace's live model and its launch
record is saved in that workspace's `terminal.json`, but a daemon restart brings
the tab back dead/read-only like every terminal record — Import does not re-run
the attach argv automatically. Import needs a daemon-hosted session. No dock/tmux
rows outside the picker, and no scanning of other multiplexers (GNU
screen/zellij) or remote machines. Nothing is attached or resumed automatically:
only the picker's Enter or Import (user actions) start the attach, and a restored
session tab stays dead until Restart. Agent rows are a foreground-command name
match, not an identity.
`docs/internal/tmux-native-sessions.md` in Fresh is the reference for the rungs
above this one.

## Config

The `terminal` section is a top-level config object, like Fresh's. The keys gvim
uses:

| key | type and default | meaning |
| --- | --- | --- |
| `terminal.shell` | `{ "command": string, "args": [string] }`, unset | the program a terminal starts instead of `$SHELL` (then `/bin/sh`). An empty command means `$SHELL`. |
| `terminal.close_on_exit` | bool, `false` | hide the dock when its shell exits instead of keeping the last screen as read-only scrollback. |
| `terminal.scrollback_lines` | int, `10000` | **gvim extension**, not a Fresh key: the history rows the emulator keeps. `0` disables the history; the cap is `1000000`. |
| `terminal.mouse_forwarding` | `"requested"` (default) or `"alt_screen"` | when the mouse reaches the program inside the terminal. |
| `terminal.jump_to_end_on_output` | bool, `true` | new output returns the view to the live screen unless a selection is active. |
| `terminal.mouse_drag_selects` | bool, `true` | a live-grid drag selects text by dropping into scrollback; false makes live drags inert (scrollback drags and Shift+drags still select). |
| `terminal.pass_ctrl_v_without_text` | bool, `true` | a Ctrl+V whose system clipboard has no text (an image, say) goes to the shell as the key itself instead of pasting the last yank; false pastes the last yank. An unreadable clipboard pastes the last yank either way. |
| `terminal.resume_agents` | bool, `true` | a restart of an exited agent terminal runs its recorded resume argv instead of its launch argv. Turn it off to re-run the launch command. It also gates the resume argv of a live-at-capture tab revived on a dive. |
| `terminal.restore_live` | bool, `true` | **gvim extension**, Fresh's Live-restore behavior: a tab that was live when the session was saved revives with a fresh PTY on the user's first dive into it after a restore (switch/focus/show), keeping the saved transcript above it and using the same argv precedence as a restart. Exited-at-capture tabs and old records without the `live` flag never revive. `false` restores every tab read-only. User layer only. |
| `terminal.agents` | `[{ "name", "command", "args", "resume_args" }]`, empty | **gvim extension**: the agent presets Run Agent... offers and the resume metadata a restart uses (see Agent sessions and Run Agent). User layer only. |
| `clipboard.use_osc52` | bool, `false` | let a program in the dock write the system clipboard with the OSC 52 escape (base64 text). Off by default: the child's output is untrusted data. When it is on, a write happens only if the workspace is trusted and only for the clipboard selection (selector `c` or empty); primary/select selections and cut-buffer numbers are ignored. The bytes still go to the emulator, which ignores the sequence. |
| `clipboard.osc52_paste` | bool, `false` | **gvim extension**: answer a child's OSC 52 *read* request (`ESC ] 52 ; c ; ? BEL`) with the system clipboard, sent back to the child as terminal input. Off by default (a child reading the host clipboard is host data pasted into untrusted output). When on and the workspace is trusted: only selector `c`/empty is answered; text is capped at 64 KiB (larger, empty or unreadable clipboard = no reply); the response is base64 in `ESC ] 52 ; c ; … BEL` queued through the session's input inbox; restored/read-only transcripts and untrusted workspaces install no handler. |

The other Fresh `terminal` key is accepted and ignored, so a Fresh config file
loads without diagnostics: `skip_app_execution_alias`. `terminal.shell`,
`terminal.close_on_exit`, `terminal.pass_ctrl_v_without_text`,
`terminal.resume_agents`, `terminal.mouse_forwarding`,
`terminal.jump_to_end_on_output` and `terminal.mouse_drag_selects` are Fresh's keys
with Fresh's values; `terminal.scrollback_lines`, `terminal.agents` and
`terminal.restore_live` are gvim extensions.

**Trust.** The project config layer may set `close_on_exit`, `scrollback_lines`,
`mouse_forwarding`, `jump_to_end_on_output`, `mouse_drag_selects`,
`pass_ctrl_v_without_text` and `resume_agents` — they tune the dock — but it may
not set `terminal.shell`, `terminal.agents` or `terminal.restore_live`: a
repository must not choose the programs gvim executes, and it must not make gvim
start processes on restore. All three are refused with a diagnostic, like the
project `lsp` command rule (C11 in `docs/trust.md`).

## Working directory and clickable paths

A shell reports its working directory with OSC 7. The session keeps the last
value (a plain path or a `file://host/path` URL; the host is dropped and percent
escapes are decoded) and the dock hands it to the path resolver (and to a restart,
see above).

**Ctrl+Click on a path in the output opens it**, in the live view and in
scrollback. The token under the pointer is the run of non-space characters
around the cell, with trailing sentence punctuation trimmed, and it must look
like a path (a slash, a dot or a leading tilde). A trailing `:line` or
`:line:col` narrows the jump: the path with the suffix stripped is tried first,
then the whole token as a plain path, so a file whose name really holds a colon
still opens; the line and column must be positive numbers and the root opens the
file there. Resolution order is Fresh's: absolute path (with `~` expanded), then
relative to the shell's OSC 7 working directory, then relative to the editor's
working directory (the workspace root). The existence check and the directory
read run in a `tea.Cmd`; the dock then asks the root to open the path through the
same `openFile` path the explorer and LSP jumps use. A miss is a `No file at
<token>` notice. Ctrl+Click is off on the alternate screen, where the output is a
full-screen program's own drawing.

**Ctrl+hover** highlights a link before the click: the dock underlines the token
under the pointer, with no file-system access (the path is resolved only on the
click), and the status row says `Ctrl+Click to open <token>`. The underline clears
on any other mouse event, when the pointer leaves the dock's rows, when the view
changes and when the dock loses the focus; the alternate screen, being a
full-screen program's own drawing, has no hover.

The token is read from the row's text, so a column maps to a rune rather than to
a cell: a double-width character before the token shifts the hit spot by one.

## Ctrl+V and the clipboard

`Ctrl+V` in a live terminal pastes the system clipboard's text, with Fresh's
fallbacks (`terminal.pass_ctrl_v_without_text` controls the no-text case):

- a readable clipboard that holds text is pasted as a bracketed paste when the
  child asked for that mode;
- a readable clipboard that holds no text is a copy of something that is not
  text (an image), so by default the Ctrl+V key itself goes to the shell and a
  program such as an agent CLI can read the clipboard; with
  `pass_ctrl_v_without_text = false` gvim pastes its last yank instead, and does
  nothing when there is none;
- a clipboard that cannot be read at all (no display, over SSH) always pastes the
  last yank; only when there is none does the default pass the key, and with the
  setting off nothing happens.

The last yank is the last text a copy or cut in the editor (or the dock) put on
the clipboard. Editor-side Ctrl+V pastes are unaffected.

## Mouse forwarding

The dock reads the shell's mouse-reporting modes from the emulator (DECSET
1000/1002/1003) and the alternate-screen state, and forwards the mouse by
Fresh's rules:

- **`requested`** (default): press, release and wheel reach the shell only when
  the shell turned mouse reporting on; motion with a button held under drag
  reporting (1002) or all-motion (1003), buttonless motion only under all-motion.
  Shift bypasses forwarding (xterm's convention), so the press becomes a text
  selection instead. A wheel notch on an alternate-screen program that did not ask
  for the mouse is sent as three arrow keys (the alternate-scroll rule pagers rely
  on).
- **`alt_screen`** (legacy): every event goes to an alternate-screen program,
  with no Shift bypass.
- Forwarding never happens in scrollback mode. A press on the dock's title row
  only takes the focus.

The forwarded coordinates are relative to the shell's first row. The dock keeps
the pressed button for drag events, so a drag under all-motion reports the button
it holds.

## close_on_exit

With `terminal.close_on_exit = true`, a shell that exits hides the dock and
releases the shell; the frozen last screen is dropped. The default keeps the
dock showing the last screen, as before. Fresh also closes a pane when its only
tab was the terminal; gvim has no split terminals, so the dock is the whole
story for now. The status bar's terminal-restart indicator is unaffected: it can
restart an exited shell whether or not the dock was kept.

## Deferred

These are known gaps, not bugs:

- **reflow** of live or saved output: x/vt has no reflow, so a resize clips or
  pads the live screen and stored transcript rows keep their old widths (see
  Persistence and reflow).
- **drag-to-split and per-tab split terminals**: the dock's tabs reorder within
  its strip (see Reordering) but the dock is one shell per tab with no splits, so
  Fresh's drag-a-tab-onto-a-pane-to-split it, extract-tab and per-split tab strips
  are not offered. Workspace scoping landed with #19a (see Terminal tabs in daemon
  workspaces); sharing a buffer across workspaces is the #19b policy decision.
- **agent discovery remainder** (#20b): the local tmux scanner and the local
  Claude Code/Codex transcript scanner share one picker (see Agent discovery and
  Run Agent), the Run Agent dialog has a per-launch directory choice, and Import
  creates a workspace for a session's directory. Still open: an imported workspace
  cannot carry the attach argv yet (registry v1 has no initial-terminal-launch
  field), dock/agent rows outside the picker, scanning of GNU screen/zellij and
  remote machines, live-session detection for transcripts (a row is a
  conversation, not a running process), and any agent identity beyond the
  foreground-command name match.
- **live ephemeral PTY restore — implemented (liveness-gated, opt-out by
  `terminal.restore_live`)**: a tab that was live at the capture is restored
  dormant and revived with a fresh PTY on the user's first dive into it (Fresh's
  Live restore; argv precedence resume→launch→shell under
  `terminal.resume_agents`), with the saved transcript kept above and the bounded
  styled visible-screen snapshot preserved on the transcript tail. Exited-at-capture
  tabs and pre-`live` records stay read-only dead, and a failed revive is one
  notice, not a retry loop. Divergences from Fresh's backing file: only the visible
  screen is styled (older history is text with trimmed blanks/control
  characters), there are no backing-file bytes, cursor/alternate-screen/OSC state
  is not restored, and a save after a revive captures the new live screen; the
  revive needs an explicit dive (gvim's dock is not a restored editor split), and
  `terminal.restore_live = false` restores everything read-only. Remote launches
  still restore no layout/terminal state.
- **live-screen reflow** — **decision:** only the stored transcript is re-wrapped; the
  live screen stays clipped or padded on a resize because x/vt has no reflow and adding it
  would mean owning the emulator's scrollback reflow (see Persistence and reflow).
- **OSC 52 read (paste)** — **implemented (opt-in)**, unlike Fresh, which explicitly does
  not answer reads. gvim's own OSC 52 parser now recognizes the read request
  (`ESC ] 52 ; c ; ? BEL`) and the session's input inbox is a real app-to-terminal
  response channel: with `clipboard.osc52_paste` on and the workspace trusted, the dock
  reads the system clipboard through the existing timeout-bounded adapter and queues a
  base64 `ESC ] 52 ; c ; … BEL` response as terminal input, so the child receives it on
  its stdin exactly as it would from a terminal. Limits: selector `c`/empty only
  (primary/select and cut-buffer queries are ignored); one response per request; text over
  64 KiB, an empty clipboard or a read failure sends nothing (the child times out rather
  than hangs); the response is bounded under the parser's 128 KiB sequence cap; the outer
  terminal and x/vt are not involved (x/vt ignores incoming OSC 52, gvim's parser owns
  it); restored/read-only transcripts have no live PTY and no handler; an untrusted
  workspace installs no handler even when the config is on. There is no reply for a child
  that ignores its stdin or has no read path — the capability is "response can reach the
  child's PTY input", not a promise that a given program consumes it.
- **the kitty keyboard protocol** — **decision:** not ported, because there is no seam to
  port against. x/vt v0.1.0 encodes child keys with legacy bytes only; its `SendKey` carries
  the explicit `TODO: Support Kitty, CSI u, and XTerm modifyOtherKeys` and the emulator
  exposes no keyboard-mode API. Reimplementing the protocol inside gvim would mean owning
  the emulator's input encoding stack; revisit when x/vt lands it. The outer terminal's
  keyboard protocol is Bubble Tea's business, not x/vt's.
- **close_on_exit for report-split terminals**: gvim only has the dock.
