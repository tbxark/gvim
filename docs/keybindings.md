# Keybindings

gvim ships five keymaps: `default`, `macos`, `macos-gui`, `emacs` and `vscode`. The platform
picks the first (`macos` on macOS, `default` elsewhere); `--keymap NAME` or `"keymap"` in the
config chooses another, and *Select Keybinding Map* (or View > Keybinding Style) switches for the
current session. To keep a switch, set `"keymap"` in the config.

## Only the common shortcuts have keys

A preset binds about 60 to 80 editor keys (most actions have one): file (new, open, save, save as, close tab, quit), edit
(undo, redo, cut, copy, paste, select all, duplicate and delete line, move line, comment, indent),
search and navigation (find, find next and previous, replace, go to line, quick open, command
palette, live grep, back and forward, word, line and document moves, matching bracket), tabs and
splits, the explorer, the terminal, fold, multi-cursor basics and a few language-server keys. Every
other command is in the command palette and the menus without a key. Each preset keeps its own
idiom: Emacs has `C-x C-s` and the other `C-x` chords, VS Code has `Ctrl+K Ctrl+L`, macOS keeps its
Ctrl and Option keys, `macos-gui` uses Cmd.

Search and replace work on one buffer. Live Grep searches the project's files and cannot replace in
them, and gvim has no project-wide Search and Replace (Fresh has one). To change several files, use
Replace or Query Replace in each file.

The menus have no keys either: no `F10`, no `Alt+letter`, no underlined letters. Click a title, or
run *Open Menu Bar* from the palette and use the arrow keys, Enter and Esc.

Shortcut labels (menus, palette, welcome screen, status row, help, tour) come from the active keymap
and are written one way: `Ctrl+S`, `Ctrl+Shift+P`, `Alt+Left`, `Ctrl+X Ctrl+S` for a sequence. A
command with no key in the active keymap shows none. The help lists only keys that run their action,
so a key a preset gives to another action (Emacs's `Ctrl+R` is Find Previous) is not listed as the
old action's key.

Keys are read in the contexts gvim resolves: the editor (`normal`, with `global` as the fallback), the
file explorer, and the terminal dock. Prompts, popups, the menus and the find bar answer their keys in
code, so a binding in another `when` does nothing, and gvim skips it with a warning.

## Changing a key

*Edit > Keybinding Editor...* (or *Open Keybinding Editor* in the palette) lists the active bindings
with their key, action id, description, context and source, as Fresh's editor does.

- `Up`, `Down`, `PgUp`, `PgDn`, `Home`, `End` move.
- `/` searches the text of a row. `r` searches by key: press a key and only the rows that hold exactly
  that key stay. `Tab` switches between the two searches, `Esc` clears them.
- `c` cycles the context filter (all, global, normal, prompt, ...), `s` the source filter (all,
  custom, keymap). The header shows the filters that are on.
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

A key that starts a multi-key binding in the active keymap is a prefix, as in Fresh. Ctrl+X in Emacs
and Ctrl+K in VS Code no longer run their own command (Cut, delete to end of line): the first press is
held, the status row shows it (`Ctrl+X-`), and the next key completes the binding. A key that completes
no binding is dropped and the status row says so (`Ctrl+X Z is not bound`); it never edits the text.
`Esc` cancels. The held key waits without a timeout. A single key you bind yourself frees the
sequences that start with it. This is not the plugin prefix rule, which has a one-second timeout.

## Mistakes are warnings

An unknown action, an unknown key or modifier, a `when` that gvim does not resolve, or a misspelt field
skips that one entry and shows a warning (the status row shows the first with the count, and *Show
Warnings* lists them all, with the config file's own problems). gvim still starts and the key keeps its
meaning. The skipped entry is not listed in the editor, but it stays in the file when the editor saves,
so fixing it is up to you. A `keymap` name that does not exist falls back to the platform
keymap and lists the valid names. `gvim config show` prints the same warnings on stderr.

Only the user config sets `keymap` and `keybindings`. A project's `.gvim/config.json` that names
either is ignored with a warning, so a repository cannot rebind your keys.
