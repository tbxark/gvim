# Keybindings

gvim ships five keymaps: `default`, `macos`, `macos-gui`, `emacs` and `vscode`. The platform
picks the first (`macos` on macOS, `default` elsewhere); `--keymap NAME` or `"keymap"` in the
config chooses another, and *Select Keybinding Map* (or View > Keybinding Style) switches for the
current session. To keep a switch, set `"keymap"` in the config.

## The presets are Fresh's

The five files hold Fresh's own bindings for the contexts gvim resolves: `global`, `normal`,
the file explorer (`file_explorer`, which Fresh spells `fileExplorer`) and the terminal dock.
They inherit the way Fresh's do: `default` is the base, `macos`, `emacs` and `vscode` inherit
it, and `macos-gui` inherits `macos`. A preset holds every binding of its ancestors plus its
own, so a key Fresh defines once is defined for every preset.

| Preset | Own bindings | In effect |
|---|---|---|
| `default` | 198 | 196 |
| `macos` | 53 | 212 |
| `macos-gui` | 50 | 261 |
| `emacs` | 78 | 243 |
| `vscode` | 9 | 201 |

"Own bindings" is the preset's file; "in effect" is its own and inherited bindings after
overrides, one per context and chord. The other Fresh bindings (the prompt, searchPrompt, popup,
completion, settings, menu, compositeBuffer and dock contexts) are not emitted: gvim's overlays
answer their keys in code, so a binding in one of those `when` contexts would never run, and the
loader skips a user binding there with a warning.

So the coverage is Fresh's: file, edit, selection (block selection too), the word, line, page and
document moves, multi-cursor (next match, above and below, line ends), marks and bookmark slots
0 to 9, macros, search and replace (including Query Replace and find-in-selection), the language
server (completion, hover, definition, references, implementation, rename, code actions), shell
commands, case transforms, folding and the view, tabs and splits, the explorer, the terminal
dock, the sidebar section cycle, code tours and compare files. Each preset keeps its own idiom:
Emacs has the `C-x` chords and the `M-` keys, VS Code has `Ctrl+K Ctrl+L` and friends, macOS
keeps its Ctrl and Option keys, `macos-gui` uses Cmd (a terminal that reports Super sends those;
the Ctrl keys it inherits still work when it does not), and `default` is the cross-platform set.

A few keys are gvim's, where Fresh has none: `Ctrl+Shift+P` (Command Palette), `Ctrl+Shift+S`
(Save As), `Ctrl+Shift+K` (Delete Line), `Ctrl+Shift+D` (Duplicate Line), `Alt+Z` (Toggle Fold),
`Ctrl+\` and `Alt+\` (Split Right and Split Below), `Alt+Shift+W` (Close Split), macOS
`Alt+Shift+S` (Save As), `macos-gui` `Cmd+Shift+D` (Duplicate Line) and `Cmd+Shift+F` (Live
Grep), Emacs `C-x C-n` (New File) and `C-x f` (Toggle Fold), and the VS Code preset's
`Ctrl+Shift+F` (Live Grep), `Ctrl+Shift+E` (Focus Explorer), `Ctrl+K Ctrl+L` (Toggle Fold),
`` Ctrl+` `` (Toggle Terminal) and `Ctrl+Shift+\` (Matching Bracket). Fresh leaves Duplicate
Line, Fold and most Save As keys to the palette; gvim keeps a key for them.

Shortcut labels (menus, palette, welcome screen, status row, help, tour) come from the active
keymap and are written one way: `Ctrl+S`, `Ctrl+Shift+P`, `Alt+Left`, `Ctrl+X Ctrl+S` for a
sequence. A command with no key in the active keymap shows none. The help lists only keys that
run their action, so a key a preset gives to another action (Emacs's `Ctrl+R` is Find Previous) is
not listed as the old action's key.

## Menu accelerators

The menu bar has Fresh's accelerators: `F10` opens the bar, and `Alt+F`, `Alt+E`, `Alt+V`,
`Alt+S`, `Alt+G`, `Alt+H` and `Alt+X` open the File, Edit, View, Selection, Go, Help and
Explorer menus. gvim's LSP menu, which Fresh does not have, is on `Alt+L`. Underlines and the
Alt chords follow the `editor.menu_bar_mnemonics` setting (on by default): with it on the bar
underlines each menu's Alt+letter and the chords open the menus; with it off the underlines and
the Alt+letter chords go, and F10 stays. A binding in the editor can shadow an accelerator, as
Fresh's own keymaps do, because the normal context wins: `Alt+L` is To Lower Case in the editor,
macOS and Emacs move a word on `Alt+F`, and Emacs's `Alt+G` starts its Go to Line chord. The
underline still shows; the key runs the context action. While the bar is hidden the accelerators
say so instead of opening a dropdown.

## Search and replace

Search and replace work on one buffer. Live Grep searches the project's files and cannot replace
in them, and gvim has no project-wide Search and Replace (Fresh has one). Fresh's `Alt+A`
(project-wide replace) opens gvim's Replace bar instead, and `Ctrl+Alt+R` runs Query Replace.
To change several files, use Replace or Query Replace in each file.

## Changing a key

*Edit > Keybinding Editor...* (or *Open Keybinding Editor* in the palette) lists the active bindings
with their key, action id, description, context and source, as Fresh's editor does.

- `Up`, `Down`, `PgUp`, `PgDn`, `Home`, `End` move.
- `/` searches the text of a row. `r` searches by key: press a key and only the rows that hold exactly
  that key stay. `Tab` switches between the two searches, `Esc` clears them.
- `c` cycles the context filter (all, global, normal, file_explorer, terminal), `s` the source
  filter (all, custom, keymap). The header shows the filters that are on.
- `Enter` edits the highlighted row: press the new key (press `Enter` on the key field first to record
  `Esc`, `Tab` or `Enter` themselves), `Tab` to the action (type to get suggestions) and the context,
  then `Enter` on the context or `Ctrl+S` to apply. `a` adds a binding. The dialog warns when the key
  is already bound.
- `d` resets a custom binding to the keymap's or unbinds a keymap one, `x` disables a key with a
  `noop` entry.

The footer names only the keys above, and always `Ctrl+S save`, even in a narrow terminal. The dialog
opens inside the table, one cell in from its border.

Changes stay **pending**: the table shows them at once, but nothing is written and no key changes
until you press `Ctrl+S` in the table, which writes the `keybindings` list of the user config file and
makes it live. Closing with pending changes asks *Save* (`S`), *Discard* (`D`) or *Cancel* (`Esc` or
`C`). Like Fresh's editor, the dialog cannot record a key sequence; sequences are listed and can be
reset, unbound or disabled, and are written by hand. On a terminal too small for the dialog the editor
says so and does not open it.

The list is Fresh's documented form, so it can also be written by hand. A file that holds the object
form, `"keybindings": {"bindings": [...]}`, keeps that form when the editor saves; a file without
bindings gets the list:

```json
{
  "keybindings": [
    { "key": "k", "modifiers": ["ctrl"], "action": "save", "when": "normal" },
    { "key": "s", "modifiers": ["ctrl"], "action": "unbind", "when": "normal" },
    { "keys": [{ "key": "x", "modifiers": ["ctrl"] }, { "key": "l" }], "action": "goto_line" }
  ]
}
```

`when` defaults to `normal`. Your bindings win over the keymap's, and a key you rebind replaces the
keymap's binding, as in Fresh (the editor's dialog does warn about a key already in use). Show Warnings
lists each keymap binding that one of your entries replaces, and the status row does not, because a
deliberate rebinding would show a problem on every start. `unbind` removes a keymap binding, `noop`
disables a key. A single key you bind frees the sequences that start with it.

Key and modifier names are Fresh's (docs/configuration/keyboard.md): `esc`, `pgup`, `pgdn`, `ret`,
`bs`, `del`, `ins`, `spc`, the punctuation names such as `plus`, `minus` and `bracketleft`, the keypad
names `kp_*`, `f1` to `f35`, and names are not case-sensitive. The modifiers are `ctrl` (or
`control`), `alt`, `shift` and `super` (or `cmd`, `command`, `meta`).

`"active_keybinding_map"`, Fresh's name for the keymap setting, is read as `"keymap"` (`keymap` wins if
both are set). Fresh's custom keymap files (`keybinding_maps`) are not supported: a non-empty one
produces a warning, so list the keys under `keybindings` instead.

## Prefix keys

A key that starts a multi-key binding in the active keymap is a prefix, as in Fresh. Emacs's
`Ctrl+X` and `Alt+G` and VS Code's `Ctrl+K` start chords: the first press is held, the status row
shows it (`Ctrl+X-`), and the next key completes the binding. A key that completes no binding is
dropped and the status row says so (`Ctrl+X Z is not bound`); it never edits the text. `Esc`
cancels. The held key waits without a timeout. A single key you bind yourself frees the sequences
that start with it. This is not the plugin prefix rule, which has a one-second timeout.

## Keys gvim cannot resolve

Keys are read in the contexts gvim resolves: the editor (`normal`, with `global` as the fallback),
the file explorer, and the terminal dock. Prompts, popups, the menus and the find bar answer their
keys in code, so a binding in another `when` does nothing, and gvim skips it with a warning. This is
the one place the port is narrower than Fresh: Fresh exposes those as first-class keymap contexts,
and gvim's presets therefore consume no bindings there.

## Differences from Fresh

- Remote workspaces are not implemented, so Fresh's `F6` (the Remote Indicator menu) is not bound.
- gvim has no project-wide Search and Replace; Fresh's `Alt+A` opens the in-buffer Replace bar.
- Fresh's macOS `Ctrl+T` and `macos-gui` `Cmd+Shift+O` open a Go to Symbol mode. gvim's palette has
  no symbol mode, so those bindings open the command palette and ignore the recorded `filter` argument.
- The gvim-only keys are the ones listed above under "The presets are Fresh's".
- `macos-gui` binds Cmd chords, which a terminal only sends with a keyboard protocol that reports
  Super; without it the preset still has the Ctrl keys it inherits from `macos`.

## Mistakes are warnings

An unknown action, an unknown key or modifier, a `when` that gvim does not resolve, or a misspelt field
skips that one entry and shows a warning (the status row shows the first with the count, and *Show
Warnings* lists them all, with the config file's own problems). gvim still starts and the key keeps its
meaning. The skipped entry is not listed in the editor, but it stays in the file when the editor saves,
so fixing it is up to you. A `keymap` name that does not exist falls back to the platform
keymap and lists the valid names. `gvim config show` prints the same warnings on stderr.

Only the user config sets `keymap` and `keybindings`. A project's `.gvim/config.json` that names
either is ignored with a warning, so a repository cannot rebind your keys.
