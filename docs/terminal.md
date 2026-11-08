# Terminal dock

This is the design record for the integrated terminal: the shell the dock runs,
what the dock shows, the keys and mouse it takes, and what is deliberately not
built yet. The code is `internal/terminal` (PTY, emulator, session API),
`internal/tui/dock` (panel, keys, painting, scrollback, mouse, links) and
`internal/tui/dock_glue.go` (the root glue).

`internal/terminal` runs the shell in a PTY (`creack/pty`) and models its screen
with `charmbracelet/x/vt`. The dock paints that screen into the frame's bottom
band. The parity row and the remaining rounds live in `docs/parity.md`.

## Two views: live and scrollback

The dock has one shell and two read-only views of it:

- **Live**. The shell screen, with the shell's caret; keys go to the shell. This
  is the default, and it is what an exited shell keeps showing (frozen on its
  last screen).
- **Scrollback**. A pager over the emulator's main-screen history followed by the
  live screen rows. It is read-only: it never writes to the shell and it shows no
  caret. The title row says `Scrollback — rows a-b of n (Ctrl+Space returns)`,
  and turns into `… — /query k/n` while a search is open.

The switch is **Ctrl+Space** while the dock has the focus. **Ctrl+] keeps the
existing `terminal_escape` binding**, so there is still a chord that gives the
focus back to the editor. Inside scrollback, **Esc** also returns to the live
screen. A shell that is on the alternate screen has no history, so Ctrl+Space
only reports `The alternate screen has no scrollback.`, and a dock whose history
is disabled or still empty falls through to the same `terminal_escape` binding,
so the chord still gives the focus back there. Keyboard capture (F9) keeps every
key for the shell, including Ctrl+Space; turn capture off to reach scrollback.
Losing the dock's focus leaves scrollback, so the dock is live the next time it
is focused.

Scrollback keys:

| key | action |
| --- | --- |
| Ctrl+Space, Esc | back to the live screen |
| Up, Down | move the selected row by one |
| PageUp, PageDown | move by a screen |
| Home, End | oldest row, newest row |
| `/` | open the search input; type the query, Enter commits, Esc cancels |
| n, N | next, previous match |
| y | copy the selected row |
| Ctrl+C | copy the visible rows |

The selected row is painted with the selection background. A copy goes through
the same clipboard path the editor's copy uses (the terminal clipboard command
and, when the app has one, the native clipboard writer). The search is a plain
case-insensitive substring search over the history and the live screen rows; it
is not a regex search and it has no case option yet.

The history snapshot is fetched in a `tea.Cmd`: `Update` never waits for the
session, and the rows it copies are bounded (`terminal.MaxScrollbackSnapshot`).
A snapshot carries the shell generation, so a late answer from a shell that has
since been replaced is dropped. Scrollback is the x/vt history only: it is not
persisted, it is not reflowed on resize, and restarting a shell starts a new,
empty history.

The mouse wheel is the third way in: on the live main screen of a shell that did
not ask for the mouse, an upward notch enters scrollback and scrolls three rows.
In scrollback, the wheel scrolls the window.

## Config

The `terminal` section is a top-level config object, like Fresh's. The keys gvim
uses:

| key | type and default | meaning |
| --- | --- | --- |
| `terminal.shell` | `{ "command": string, "args": [string] }`, unset | the program a terminal starts instead of `$SHELL` (then `/bin/sh`). An empty command means `$SHELL`. |
| `terminal.close_on_exit` | bool, `false` | hide the dock when its shell exits instead of keeping the last screen as read-only scrollback. |
| `terminal.scrollback_lines` | int, `10000` | **gvim extension**, not a Fresh key: the history rows the emulator keeps. `0` disables the history; the cap is `1000000`. |
| `terminal.mouse_forwarding` | `"requested"` (default) or `"alt_screen"` | when the mouse reaches the program inside the terminal. |

The other Fresh `terminal` keys are accepted and ignored, so a Fresh config file
loads without diagnostics: `jump_to_end_on_output`, `resume_agents`,
`mouse_drag_selects`, `pass_ctrl_v_without_text` and
`skip_app_execution_alias`. `terminal.shell` and `terminal.close_on_exit` are
Fresh's names; `terminal.mouse_forwarding` is Fresh's key with Fresh's two
values; `terminal.scrollback_lines` is the one gvim addition.

**Trust.** The project config layer may set `close_on_exit`, `scrollback_lines`
and `mouse_forwarding` — they tune the dock — but it may not set
`terminal.shell`: a repository must not choose the program gvim executes. A
project `terminal.shell` is refused with a diagnostic, like the project
`lsp` command rule (C11 in `docs/trust.md`).

## Working directory and clickable paths

A shell reports its working directory with OSC 7. The session keeps the last
value (a plain path or a `file://host/path` URL; the host is dropped and percent
escapes are decoded) and the dock hands it to the path resolver.

**Ctrl+Click on a path in the output opens it**, in the live view and in
scrollback. The token under the pointer is the run of non-space characters
around the cell, with trailing sentence punctuation trimmed, and it must look
like a path (a slash, a dot or a leading tilde). Resolution order is Fresh's:
absolute path (with `~` expanded), then relative to the shell's OSC 7 working
directory, then relative to the editor's working directory (the workspace root).
The existence check and the directory read run in a `tea.Cmd`; the dock then
asks the root to open the path through the same `openFile` path the explorer and
LSP jumps use. A miss is a `No file at <token>` notice. Ctrl+Click is off on the
alternate screen, where the output is a full-screen program's own drawing.

The token is read from the row's text, so a column maps to a rune rather than to
a cell: a double-width character before the token shifts the hit spot by one.
There is no hover underline yet, and a `file:line:col` suffix is not decoded.

## Mouse forwarding

The dock reads the shell's mouse-reporting modes from the emulator (DECSET
1000/1002/1003) and the alternate-screen state, and forwards the mouse by
Fresh's rules:

- **`requested`** (default): press, release and wheel reach the shell only when
  the shell turned mouse reporting on; motion with a button held under drag
  reporting (1002) or all-motion (1003), buttonless motion only under all-motion.
  Shift bypasses forwarding entirely (xterm's convention), so the press stays
  with the editor side. A wheel notch on an alternate-screen program that
  did not ask for the mouse is sent as three arrow keys (the alternate-scroll
  rule pagers rely on).
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

- **drag-select and text selection** in a terminal, including Shift+drag
  selecting under a mouse-hungry program. The editor's selection machinery is not
  wired to the dock; Shift currently only means "do not forward".
- **reflow and persisted scrollback**: the x/vt history is not reflowed when the
  dock is resized, and it is not saved across restarts or session restore.
- **agent resume** (`terminal.resume_agents`) and restart-into-same-buffer: the
  dock's Restart starts a fresh session with the shell's argv, it does not append
  below the old transcript or resume a coding agent.
- **Windows ConPTY**: `terminal.Start` returns `ErrUnsupported` on Windows.
- **terminal tabs** in the dock.
- **close_on_exit for report-split terminals**: gvim only has the dock.
- **navigating the scrollback of an exited shell**: the frozen screen stays, but
  scrollback mode needs the focus and a shell that runs, so the history of a
  dead shell is only reachable by restarting it (which starts a new history).
- `jump_to_end_on_output`: the dock is live unless the reader enters scrollback,
  so there is no "new output pulls the view back" rule to tune yet.
- child OSC 52 clipboard, the kitty keyboard protocol, regex/case search options,
  and hover underlines for paths.
