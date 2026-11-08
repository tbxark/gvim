# Bundled plugins

gvim ships ten plugins inside the binary, written in Lua like any other (their sources are under
`internal/plugin/bundled/plugins/<name>/` in the repository; **Plugin: Manager** shows them, and
`config.json` can override them). Eight are on by default; `flash` and `vi_mode` are off until you
turn them on:

```jsonc
{ "plugins": { "vi_mode": { "enabled": true }, "git_blame": { "enabled": false } } }
```

A plugin you put in `~/.config/gvim/plugins/` with the same name replaces a bundled one.

| Plugin | Default | Commands (palette) | Settings (`plugins.<name>.settings`) |
|---|---|---|---|
| `goto_with_selection` | on | Go to Line with Selection | none |
| `tab_actions` | on | Close Other Tabs, Close All Tabs, Close Tabs To Right, Close Tabs To Left, Move Tab To Left, Move Tab To Right | none |
| `git_gutter` | on | Git Gutter: Refresh, Next Change, Previous Change | `debounce_ms` (300, 50 to 5000, project) |
| `git_statusbar` | on | none (a status bar item) | none |
| `git_find_file` | on | Git Find File, Git Find File: Reload | none |
| `git_blame` | on (idle until toggled) | Git: Toggle Blame, Git: Show Commit, Git: Copy Commit Hash | `debounce_ms` (150, 50 to 2000, project), `summary_width` (60, 10 to 200, project) |
| `merge_conflict` | on | Merge: Start Resolution, Next Conflict, Previous Conflict, Use Ours, Take Theirs, Use Both, Resolve Conflict... | none |
| `markdown_toc` | on | Markdown: Show Outline, Markdown: Close Outline | `max_depth` (6, 1 to 6), `follow` (`cursor` or `scroll`) |
| `flash` | **off** | Flash: Jump | `labels`, `skip_rule` (true), `case_sensitive` |
| `vi_mode` | **off** | Vi Mode: Toggle | `auto_start` (true), `arrow_keys`, `ctrl_keys`, `ignore_case`, `smart_case`, `shift_width`, `clipboard_yank`, `search_word_under_cursor`, `hlsearch` |

None of them binds a default key (Fresh's presets use most chords); bind one yourself, with
`gvim.keymap.set("ctrl+alt+j", "flash.jump")` in your `init.lua`, or in `config.json`:

```json
{ "keybindings": { "bindings": [
  { "key": "j", "modifiers": ["ctrl", "alt"], "action": "flash.jump", "when": "normal" }
] } }
```

A plugin command's action id is `<plugin>.<name>`.

## What each one does

**goto_with_selection**: runs `set_mark`, then asks for a line and goes there, so the text between the
caret and the line is selected.

**tab_actions**: closes the other tabs, the tabs to the right or left, or all tabs; modified tabs are
skipped and counted in the status message. Moves the active tab left or right.

**git_gutter** (with Fresh's `diff_nav`): compares the open buffer with the file at `HEAD` and draws a
bar in the gutter for added and modified lines, a triangle where lines were deleted, and marks on the
scrollbar. **Next Change** and **Previous Change** jump between changed regions. Unlike Fresh it compares
the *buffer*, so the marks follow unsaved typing after a short pause. The HEAD text is read once per
file with `git show` and compared in the host (no line arrays in Lua); a file outside a repository or
untracked is left alone, and so is a buffer in large-file mode: it gets no marks, and **Next Change** and
**Previous Change** say that git changes are not tracked there. Exports `hunks(buf_id)` for other plugins (`{type = "added"|"modified"|"deleted",
line, count}`).

**git_statusbar**: the current branch (the first seven digits of the commit for a detached HEAD) on
the right of the status row. It refreshes when a file opens, a tab is activated, a file is saved and when
`HEAD` changes (it watches the directory that holds it).

**git_find_file**: a fuzzy picker over the files git tracks (up to 50 000), opened relative to the
repository root; the list is read again each time.

**git_blame** (reduced; Fresh's panel with history stepping is not ported): **Toggle Blame** shows
author, age and summary of the commit that last changed the cursor line, after the line; only 40 lines
around the cursor are blamed (`git blame -L`), an edited buffer is blamed by its text, answers are kept
for 30 seconds. **Show Commit** opens the commit in a popup, **Copy Commit Hash** puts its hash on the
clipboard.

**merge_conflict** (reduced to in-buffer resolution; the three-panel merge view is not ported): marks
`<<<<<<<` / `|||||||` / `=======` / `>>>>>>>` blocks with highlights, jumps between them, and resolves the
one under the caret as ours, theirs or both in one undo step. **Start Resolution** turns on a key mode
(`n`/`p` next and previous, `u` ours, `t` theirs, `b` both, `q` leaves). It never reads a whole buffer:
it asks the host for the marker lines and rescans only what an edit touched.

**markdown_toc** (the outline is a virtual buffer in a split; Fresh's sidebar widget and folds are not
ported): **Show Outline** lists the headings of a Markdown buffer (fenced code is skipped); Enter jumps
to a heading, `q` closes the outline, Esc goes back and leaves it open. It follows the buffer you work
in and refreshes 300 ms after an edit.

**flash**: label-based jump. **Flash: Jump** starts a session; every character you type extends a literal
pattern, every visible match in every window gets a label, and typing a label jumps there (Backspace
shortens the pattern, Enter jumps to the nearest match, Esc or any other key ends it). Labels follow
flash.nvim: the nearest matches get the most comfortable keys, and a letter that is also the next
character after a match is not used.

**vi_mode**: vi-style modal editing: normal, insert, visual (character, line, block) and operator-pending
modes, counts, operators with motions and text objects, registers, marks, `.` repeat, and `:` commands.
It starts in normal mode (`auto_start`); **Vi Mode: Toggle** switches it off and on. The panic chord
`ctrl+^` clears every plugin input mode, vi's included: vi says so, and **Vi Mode: Toggle** brings it back.
Three quick `Esc` presses do **not** (vi binds `Esc`, so mashing it is normal). `ctrl+c` is an `Esc`: it
leaves insert and visual mode and cancels a pending command and the `:` and `/` prompts. Keys vi does not
bind (`ctrl+s`, `alt+...`, function keys) still reach the editor. A selection made with the mouse (a drag,
a double click) or with shift and the arrow keys is the visual mode, so `d`, `y`, `c` and the rest act on
it; a paste puts the text before the caret in normal mode (like `P`), replaces the selection in visual
mode and is inserted in insert mode. A Vim command vi does not have (`gn`, `gq`, `=`, `R`, `@`, ...)
says `vi: ... is not supported`, and the keys that belong to it (the motion of `gq`, the text typed
after `cgn` or `R` until `Esc`, the register of `q`) are dropped instead of running as commands.
The `:` and `/` prompts keep a history (Up and Down), the matches of a search are highlighted in the
window (`:noh` hides them, `:set nohls` or the `hlsearch` setting turns it off), a search that wraps says
so, and `:set` changes `hls`, `et`, `ts` and `sw` for real and refuses what the editor decides (`ai`, `is`).
Exports `toggle`, `enable`, `disable` and `is_enabled`. Its README
(`internal/plugin/bundled/plugins/vi_mode/README.md`) lists the keys; the differences from Vim are:

* Vim's insert-mode ctrl keys (`ctrl+w`, `ctrl+u`, `ctrl+o`, `ctrl+r`, `ctrl+d`, ...) and the `ctrl+w`
  window prefix are not provided: they run the keymap's own binding (`ctrl+o` opens the file picker,
  `ctrl+r` Replace, and on the macOS keymap `ctrl+w` closes the tab);
* no macros (`q`, `@`; use the editor's own), jump list, replace mode (`R` outside visual), sentences,
  folds, `:!` filters, `:normal`, `gn`/`cgn`, `gq`/`gw`/`=`/`!`, `g;`, and the marks `'.` `` `[ `` `']` `^`
  (each says it is not supported);
* marks do not move when the text changes;
* a count insert (`3ix<Esc>`) or `.` repeat is one more undo step; a change operator and the text typed
  after it (`cw foo<Esc>`) are two undo steps, because the editor has no undo group that stays open
  while keys are typed;
* `j` and `k` are the editor's own moves (by display rows on a wrapped line); `d3j` and the visual mode
  count logical lines; the caret column after `u`, `guu`, `gUU`, `g~~`, `Vy`, `yip` can differ from Vim's;
* only the primary caret is used; block mode counts characters, not display cells;
* a file still being indexed (large-file mode) refuses commands with a message until the index is ready.
* search (`/`, `?`, `n`, `N`, `*`, `#`, `:g`) is not available on a file in large-file mode: vi says
  "no search in large-file mode"; use the editor's own find.

## Memory

Plugins are loaded eagerly and cost heap while idle (about 18 KB of bytecode per Lua function plus
tables). `TestBundledPluginsIdleHeap` in `internal/app` starts the **real runtime with the default-enabled
bundled set** in a real editor session with a file open, forces a full collection, and compares the live
heap with the same session under `--no-plugins`. The measurement runs in a **fresh process** (the test
starts the test binary again and runs only itself there), because the live heap after the whole package
is about 88 MiB and moves by several MiB between two measurements; it also fails when the cost reads
under 1 MiB, so a polluted baseline cannot pass unnoticed. The gate is 8 MiB: if the default set ever costs more,
the largest plugins must become `default_enabled: false` in their `plugin.json` and this page must say so.

Measured with the plugin set of this release (macOS arm64, Go 1.27, pure heap in use after a full GC):

| | Heap |
|---|---|
| the editor session without plugins | 1.6 MiB |
| the same session with the 8 default-enabled plugins loaded and idle | 8.1 MiB |
| **cost of the default set** | **about 6.5 MiB** (gate: 8 MiB) |

The gate passes, so no bundled plugin was flipped to `default_enabled: false` for memory. Per plugin, in
a bare runtime that holds only that plugin (each figure includes about 0.3 MiB for the runtime itself, and
the first one loaded pays for the shared prelude): `goto_with_selection` 0.6 MiB, `git_find_file` 0.7,
`git_statusbar` 0.8, `tab_actions` 0.85, `git_gutter` 1.0, `git_blame` 1.15, `markdown_toc` 1.2,
`merge_conflict` 1.35, `flash` 1.55, and **`vi_mode` 6.9 MiB**. `vi_mode` is the one that would bust the
budget (the default set plus vi would be about 13 MiB), which is why it is off by default; `flash` is off by
default as well. If you do not use the git plugins, switching them off in the config saves about
4 MiB.
