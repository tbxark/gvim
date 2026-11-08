# Compare view

`gvim --diff LEFT RIGHT` shows two files side by side, with the lines that match on the same row
and the differences marked. It opens at the first difference and says how many there are, as Fresh
does. It is Fresh's Compare Files, and `git difftool` can use it:

```bash
gvim --diff old.conf new.conf
git config --global difftool.gvim.cmd 'gvim --diff "$LOCAL" "$REMOTE"'
```

The same view opens from the command palette with **Compare Files...** (it asks for the two
files in turn) and **Compare Active File With...** (the active file and one more). Both sides are
the files themselves: edit either one, undo, save, search. The marks follow your edits.

## Reading a view

- A line changed on both sides has a `~` in the gutter and a background on both sides, with the
  part of the line that differs in a stronger background.
- A line only the left file has is marked `-`, and a line only the right file has is marked `+`.
  The other side shows an empty filler row there, so the lines after the change stay level. A
  filler row has no line number, and the caret never stands on it.
- Two lines with the same text that end differently (a CRLF file against an LF one) are marked `~`
  too, with a note at the end of each, `CRLF line ending` or `LF line ending`. If that is the only
  difference, the status row says so instead of calling the files identical. A file with a final
  newline against one without shows the empty last line as an added line.
- The scrollbar of each side carries a mark for every difference, in the color of its kind, along
  the whole file, and the last column of a changed line shows the same color.
- The two sides scroll together, row for row, filler rows included: the wheel over either pane, the
  scrollbar, the keys and a jump all move both. Focusing the other pane does not move the view.
  Sideways, the other pane shows the same columns as the one you are in.
- Lines do not wrap in a comparison, because a wrapped line would push one side out of step with
  the other. Folds are not shown for the same reason.

Large files (large file mode) are not compared: the status row says so.

## Commands

Next and previous difference, switch side, copy and take change, and close are in the palette
while a comparison is open. They are not among the common keys, so the palette is how you reach
them; bind the ones you use with **Keybinding Editor**.

| Command | What it does |
|---------|--------------|
| **Compare Files...** | Pick two files and show them side by side |
| **Compare Active File With...** | Pick one file to show next to the active one |
| **Compare: Next Difference** | Moves both carets to the next difference, wrapping at the end. The status row says `Difference 2/5`, and `[wrapped]` after a wrap |
| **Compare: Previous Difference** | The same, backwards |
| **Compare: Switch Side** | Moves the focus to the other file. Its caret goes to the line on the same row as the one that leaves |
| **Compare: Copy Change to Other Side** | Makes the other file's lines of the change under the caret the same as this file's (vimdiff's `dp`) |
| **Compare: Take Change from Other Side** | Makes this file's lines of the change under the caret the same as the other file's (`do`) |
| **Compare: Close** | Closes the comparison |

On a side with no lines in a difference, the caret goes to the line before the gap, right above its
filler rows. A copy or a take is one edit, so one undo in that file puts it back, and it is not saved.

## Files the comparison opens

Each file keeps its own tab, one per side (Fresh shows the comparison as a single tab; gvim's tabs
are files, so the strip marks the two tabs of the pair with `⇄`, and the unsaved mark `●` of each side is on its own tab). Closing the tab of either side
ends the comparison, and the other file stays open. A file that was not open is opened in a tab for
the comparison. **Compare: Close** closes those tabs again, except one with unsaved changes, which stays open so you can save or discard it. A file
that was open before keeps its tab. `gvim --diff` opens both files, so closing the view leaves the
editor without them.

Unsaved text that gvim recovers after a crash opens in tabs after the two files, and does not stop
`gvim --diff` from starting.
