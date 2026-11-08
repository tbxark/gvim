# Terminal dock

This is the design record for the integrated terminal: the shell the dock runs,
what the dock shows, the keys and mouse it takes, its selection and its
persistence, and what is deliberately not built yet. The code is
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

## Persistence and reflow

The dock's transcript survives an editor restart through the session directory:
`restore.SaveTerminal` writes `terminal.json` next to the session file (and
removes it when the dock has neither a shell nor a transcript). It holds a version,
the dock's open flag, its directory (the shell's last OSC 7 directory, else the
start directory), the pane width the transcript was captured at, and the newest
transcript rows: the prior transcript, the bounded history window and the live
screen rows, as text with trailing blanks trimmed and control characters stripped,
capped at 500 rows / 256 KiB (the loader re-applies the same bounds and ignores a
version it does not know).

The state is only restored on the full session-restore path (not `--no-restore`
or a hot-exit-only recovery). **A restored terminal is read-only and dead**:
running processes are not persisted, nothing is re-run automatically, and the dock
shows `Scrollback (restored)` over the saved transcript. Restart starts a shell in
the saved directory and appends its output below the transcript; Ctrl+Space cannot
return to a live screen that does not exist.

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

Fresh's automatic **agent resume** is deferred: Fresh tracks each terminal's launch
argv and can replay an agent's resume argv (`terminal.resume_agents`). gvim starts
only the configured interactive shell, so it has no tracked launch command or
agent argv to resume. The key is still accepted and ignored, and nothing is ever
re-executed automatically by a restore or a restart.

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
| `clipboard.use_osc52` | bool, `false` | let a program in the dock write the system clipboard with the OSC 52 escape (base64 text). Off by default: the child's output is untrusted data. When it is on, a write happens only if the workspace is trusted and only for the clipboard selection (selector `c` or empty); primary/select selections and cut-buffer numbers are ignored. The bytes still go to the emulator, which ignores the sequence. |

The other Fresh `terminal` keys are accepted and ignored, so a Fresh config file
loads without diagnostics: `resume_agents` and `skip_app_execution_alias`.
`terminal.shell`, `terminal.close_on_exit`, `terminal.pass_ctrl_v_without_text`,
`terminal.mouse_forwarding`, `terminal.jump_to_end_on_output` and
`terminal.mouse_drag_selects` are Fresh's keys with Fresh's values;
`terminal.scrollback_lines` is the one gvim addition.

**Trust.** The project config layer may set `close_on_exit`, `scrollback_lines`,
`mouse_forwarding`, `jump_to_end_on_output`, `mouse_drag_selects` and
`pass_ctrl_v_without_text` — they tune the dock — but it may not set
`terminal.shell`: a repository must not choose the program gvim executes. A
project `terminal.shell` is refused with a diagnostic, like the project `lsp`
command rule (C11 in `docs/trust.md`).

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
- **terminal tabs** in the dock: the dock is one shell per panel; title, restart,
  persistence and focus all key off it, so tabs are a larger slice.
- **automatic agent re-exec** (`terminal.resume_agents`): gvim starts only an
  interactive shell, so there is no tracked launch or agent argv to replay. The
  manual restart keeps the transcript instead (see Restart and agent resume).
- **live-screen reflow**: x/vt has no reflow, so a resize clips or pads the live
  screen; only the stored transcript is re-wrapped (see Persistence and reflow).
- **OSC 52 read (paste)**: the dock parses a child's OSC 52 *write*; a child
  *reading* the clipboard through OSC 52 (a paste request) is not answered, so a
  program that asks the terminal for the clipboard gets no reply. gvim's Ctrl+V
  paste path is the editor's, not the child's.
- **the kitty keyboard protocol**: x/vt v0.1.0 encodes child keys with legacy
  bytes only; its `SendKey` carries an explicit `TODO: Support Kitty, CSI u, and
  XTerm modifyOtherKeys` and the emulator exposes no keyboard-mode seam, so gvim
  cannot offer the protocol to the program inside the dock yet. The outer
  terminal's keyboard protocol is Bubble Tea's business, not x/vt's. Revisit when
  x/vt lands the encoding.
- **close_on_exit for report-split terminals**: gvim only has the dock.
