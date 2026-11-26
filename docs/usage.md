# Usage

The command-line reference and the default-keymap cheat sheet. For the keymap presets and how to
rebind a key, see [`keybindings.md`](keybindings.md).

## Command-line options

```sh
gvim .                          # Open current directory
gvim -R .                       # Open in read-only code-viewer mode (edits & saves disabled)
gvim -R main.go:42:10           # Open file at line 42, column 10 in viewer mode
gvim --diff fileA.go fileB.go   # Compare two files side by side
gvim --review                   # Open directly in built-in Review Diff Mode
gvim --simple                   # Open a clean session without restoring previous tabs
git show HEAD | gvim -          # View piped stdin in a scratch buffer
```

| Flag | Description |
|---|---|
| `-R`, `--read-only` | Open in read-only / code-viewer mode (edits and writes disabled) |
| `--review` | Open directly in built-in Review Diff Mode (changed files sidebar + diff viewer) |
| `--diff` | Compare two files side by side |
| `--keymap NAME` | Keymap profile: `default`, `vscode`, `emacs`, `macos`, `macos-gui` |
| `--theme NAME` | Theme name (default `gopher`; also `dark`, `light`, `terminal`, etc.; `NO_COLOR=1` strips colors when `--theme` is unset) |
| `--config PATH` | Path to custom config file (default: `config.json` in the platform config directory, `~/.config/gvim/` on Linux) |
| `--log-file PATH` | Append status notices and plugin log to file |
| `--simple` | Open without restoring or saving session, and without file explorer |
| `--restore` | Force restoring previous session |
| `--no-restore` | Skip restoring previous tabs |
| `--stdin` (or `-`) | Read standard input into a scratch buffer |
| `--stdin-name NAME` | Name the stdin buffer, for its language and the Save-As suggestion |
| `--no-plugins` | Skip discovering or loading plugins (`--plugin` still loads) |
| `--plugin PATH` | Load the plugin at PATH (a `.lua` file or a plugin directory); repeatable |
| `--daemon[=NAME]` | Run the editor as a daemon on a Unix socket (foreground) |
| `-a`, `--attach[=NAME]` | Attach this terminal to a running daemon |

Subcommands: `gvim config` (inspect the configuration), `gvim grammar list` (the syntax
grammars), `gvim rex install` / `gvim rex uninstall` (the Rex terminal integration) and
`gvim version`. `gvim --help` has the full description of each flag.

## Keybinding cheat sheet

*(Based on the `default` profile. Press `Ctrl+P` then type `>` for the full command palette.)*

### Navigation & code reading
| Key | Action |
|---|---|
| `Ctrl+P` | Quick Open (Files, `>commands`, `:line`, `:+N` / `:-N`, `#buffers`) |
| `Ctrl+Shift+P` | Open Command Palette directly |
| `Ctrl+T` / `Ctrl+N` | New tab / buffer |
| `Ctrl+Tab` / `Ctrl+Shift+Tab` | Next / Previous open buffer (notebook-friendly) |
| `Ctrl+Shift+Space` / `Alt+Shift+Space` | Smart expand / shrink selection (word → brackets → line → block) |
| `Alt+Left` / `Alt+Right` | Navigate Back / Forward in position history |
| `Alt+J` | Flash Jump (2-character on-screen jump motion; needs the `flash` plugin, and while it is on it replaces the built-in `Alt+J` that toggles the Utility Dock) |
| `Ctrl+Shift+M` | Toggle Diagnostics Drawer (problems panel) |
| `F8` / `Shift+F8` | Next / Previous diagnostic error or warning |
| `Alt+Z` / `Alt+Shift+Z` | Toggle fold at cursor / Fold all blocks |
| `Ctrl+Alt+D` / `Ctrl+Alt+Shift+D` | Jump to next / previous Git diff change (hunk) |
| `Ctrl+]` | Jump to matching bracket |
| `Ctrl+L` | Expand line selection across all carets |
| `Ctrl+\` / `Alt+\` | Split right / split below |
| `Alt+]` / `Alt+[` | Switch to next / previous split pane |
| `Alt+Shift+W` | Close current split pane |
| `` Alt+` `` | Open a terminal in the dock |

### Search
| Key | Action |
|---|---|
| `Ctrl+F` | In-file find bar (`Alt+C` case, `Alt+W` word, `Alt+R` regex) |
| `F3` / `Shift+F3` | Next / Previous search match |
| `Alt+/` | Live Grep: project-wide search over files, open buffers, terminal transcripts, and LSP diagnostics (`Alt+D`) |
| `Alt+A` | Project Search & Replace (auto-prefilled from selection) |
| `Alt+Q` / `Alt+M` | In Live Grep: export results to quickfix list |
| `Alt+D` | In Live Grep: toggle searching LSP diagnostics |

### LSP & semantics
| Action | Description |
|---|---|
| `Go to Definition` | Jump to symbol definition (`F12`; `Ctrl+.` in the `macos` keymap) |
| `Alt+F12` | In-place definition preview card without switching tabs (the `lsp_peek_definition` action; `Ctrl+Alt+.` in the `macos` keymap; not a palette row) |
| `Find References` | Open references list with preview tabs (`Shift+F12`) |
| `Go to Implementation` | Jump to implementation (`Ctrl+F12`) |
| `Show Hover Info` | Show hover documentation popup (`Alt+K`) |
| `Go to LSP Symbol` | Fuzzy-find document symbols with enclosing-symbol preselected |
| `LSP: Workspace Symbols` | Fuzzy-find symbols across the whole workspace |
| `LSP: Incoming / Outgoing Calls` | Call Hierarchy: explore callers and callees |
| `LSP: Document Outline` | Interactive code symbol outline for active file |
| `LSP: Show Breadcrumbs` | Display enclosing symbol scope path (`Class > Method`) |
| `LSP: Breadcrumbs Outline` | Interactive enclosing scope hierarchy picker to jump to parent function/class |
| `List Bookmarks` | Interactive bookmarks picker (jump between slots 0–9) |

The symbol, call-hierarchy, outline and breadcrumb rows (`Go to LSP Symbol` through `LSP: Breadcrumbs Outline`) come from the bundled `lsp_navigation` plugin, which is off by default. Go to Definition and Implementation; Type Definition lists also come from it.

### Git & review
| Action | Description |
|---|---|
| `Git Gutter: Next/Prev Change` | Jump between modified regions |
| `Git: Toggle Blame` | Show/hide inline blame on the cursor line |
| `Git: Show Commit` | Show commit details popup for cursor line |
| `Git: Copy Commit Hash` | Copy commit SHA of current line to clipboard |
| `Review Diff Mode` | Toggle built-in Review Diff Mode (`Alt+Shift+R` or `gvim --review`) |
| `Merge: Start Resolution`, `Merge: Next Conflict`, `Merge: Previous Conflict`, ... | Resolve conflict markers (`Alt+M` mode or `Ctrl+Alt+C`; needs the `merge_conflict` plugin) |

Git Log, Merge conflict resolution and Flash Jump are bundled plugins that are
**off by default**; enable one with `"plugins": { "flash": { "enabled": true } }` in
`config.json` (see [`plugins/bundled.md`](plugins/bundled.md)). Git Gutter and Git Blame are on.

## Scrollbar overview ruler

The right vertical scrollbar track acts as a real-time overview ruler:
- **Red marks**: LSP diagnostic errors located in the document.
- **Yellow marks**: LSP diagnostic warnings.
- **Blue marks**: Diagnostic informational hints.
- **Green/Gutter marks**: Git added and modified regions.

## Sticky scroll

When scrolling down into long functions, classes, or code blocks, the top row of the editor pane
automatically pins the enclosing syntax-highlighted declaration header (e.g.
`func (m *Model) paintPanes(...) {`). Clicking the sticky header jumps to that scope; toggled via
**View → Sticky Scroll** or `View: Toggle Sticky Scroll` (`toggle_sticky_scroll`) in the Command
Palette.

## Mouse

| Action | Effect |
|---|---|
| `Alt+Click` | Add or remove a cursor (multi-cursor editing) |
| `Cmd+Click` / `Ctrl+Click` | Go to LSP definition (`Cmd+Click`); open URL (`Cmd/Ctrl+Click`); extend selection (`Ctrl+Click` on text) |
| `Shift+Click` | Extend selection from cursor |
| `Click / Double-click Gutter` | Select lines / toggle code fold (`⋯ N lines`) |
| `Click Markdown Checkbox` | Toggle `- [ ]` / `- [x]` directly |
| `Middle-click` | Paste clipboard in editor or terminal dock |
| `Double-click Split Border` | Equalize split panes to 50/50 |
| `Drag Dock Title / Tab "+"` | Resize terminal dock / open new terminal tab menu |

## Pager mode (`gvim -R` / `git diff | gvim`)

When viewing read-only buffers or piped input, single-key pager shortcuts activate:

| Key | Action |
|---|---|
| `Space` / `b` | Page Down / Page Up |
| `j` / `k` | Scroll Down / Up by line |
| `d` / `u` | Scroll the view down / up one line |
| `g` / `G` | Jump to start / end of document |
| `/` | Open in-file search bar |
| `n` / `N` | Next / Previous search match |
| `q` | Quit |

## Markdown modes

| Action | Description |
|---|---|
| `Markdown: Cycle Mode` | Cycle active pane: Source → Rendered → Dual → Source |
| `Markdown: Dual` | Side-by-side synchronized source and rendered split |
| `Markdown: Rendered` | Switch focused pane to composed page view |
| `Markdown: Source` | Switch focused pane to raw source view |

See [`markdown.md`](markdown.md) for the page view and scroll synchronization.
