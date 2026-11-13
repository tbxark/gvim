# gvim

A fast, keyboard-driven terminal code viewer and editor written in pure Go.

Built on [Bubble Tea v2](https://github.com/charmbracelet/bubbletea) with a custom cell grid renderer, `gvim` is designed for reading code, navigating large projects, inspecting git diffs, and exploring codebases with speed and safety.

---

## Highlights

- **Fast Code Viewer & Read-Only Protection**
  - Launch with `gvim -R` / `gvim --view` to lock all buffers in read-only mode, protecting your codebase against accidental keystrokes, pastes, or saves.
  - Syntax highlighting for 30+ programming languages, powered by Chroma lexers.
  - Code folding based on indentation and syntax (`▼` / `▶`), supporting keyboard and mouse toggles.
  - Windowed streaming for large files (100+ MiB) without allocating the whole file into memory.

- **Semantic Navigation & LSP**
  - Go to Definition and Implementation; Type Definition lists come from the `lsp_navigation` plugin.
  - Find References with an interactive preview picker.
  - Document Symbols (`Go to LSP Symbol`) with automatic enclosing-symbol preselection.
  - Hover documentation and diagnostics filtering.
  - Cross-file navigation history stack: jump forward and back across files and locations (`Alt+Left` / `Alt+Right`).

- **Git & Diff Inspection**
  - **Side-by-side File Comparison**: `gvim --diff fileA fileB` or `Compare Files` palette command with synchronized scrolling and hunk jumping.
  - **Git Gutter**: live gutter change markers and change navigation (`Next Change` / `Previous Change`).
  - **Git Blame**: inline commit author, age, and summary on the cursor line, commit details popup, and parent revision blame.
  - **Git Log**: commit history browser with commit diff previews.
  - **Review Diff (Audit Mode)**: list staged, unstaged, and untracked changes, view unified diffs, and navigate hunks across files.
  - Git Log, Review Diff, Merge conflict resolution, Compare Files and Flash Jump are bundled plugins that are **off by default**; enable one with `"plugins": { "flash": { "enabled": true } }` in `config.json` (see `docs/plugins/bundled.md`). Its keys (`Alt+J` for Flash, `Alt+M` / `Ctrl+Alt+C` for Merge) apply once it is on. Git Gutter and Git Blame are on.

- **Multi-Modal Fast Search**
  - **Quick Open (`Ctrl+P`)**: fuzzy file search, line jump (`:line`), relative line jump (`:+N` / `:-N`), buffer list (`#`), and command palette (`>`).
  - **Live Grep (`Alt+/`)**: project-wide search with scopes for files, open buffers, terminal transcripts, and LSP diagnostics (`Alt+D`), exportable to a quickfix list (`Alt+Q` / `Alt+M`).
  - **In-File Find & Replace**: case-sensitive (`Alt+C`), whole-word (`Alt+W`), regex (`Alt+R`), and match counter.
  - **Flash Jump**: two-character on-screen jump motion for instant cursor movement.

- **Markdown Composed & Dual View**
  - Composed rendering: concealed formatting markers, formatted tables, task lists, and styled headings.
  - **Dual View**: source on the left, rendered view on the right, with synchronized line scrolling.
  - **Independent Split Views**: switch any individual split pane between rendered and source mode independently.
  - Outline table of contents in the sidebar.

- **Pure Go & Cross-Platform**
  - Ships as a single static binary (`CGO_ENABLED=0`) with zero runtime dependencies.
  - Cross-platform support for macOS, Linux, and Windows.
  - Keymap presets: `default`, `vscode`, `emacs`, `macos`, `macos-gui`.

---

## Installation & Build

### Requirements
- Go 1.27.3 or later (the `go` directive in `go.mod`)

### Install via Go
```sh
go install github.com/tbxark/gvim@latest
```

### Build from Source
```sh
git clone https://github.com/tbxark/gvim.git
cd gvim
go build -o gvim .
```

To build a pure-Go static binary with no CGO dependencies:
```sh
CGO_ENABLED=0 go build -o gvim .
```

---

## Usage

### Common Invocations

```sh
gvim .                          # Open current directory
gvim -R .                       # Open in read-only code-viewer mode (edits & saves disabled)
gvim -R main.go:42:10           # Open file at line 42, column 10 in viewer mode
gvim --diff fileA.go fileB.go   # Compare two files side by side
gvim --simple                   # Open a clean session without restoring previous tabs
git show HEAD | gvim -          # View piped stdin in a scratch buffer
```

### Command-Line Options

| Flag | Description |
|---|---|
| `-R`, `--read-only`, `--view` | Open in read-only / code-viewer mode (edits and writes disabled) |
| `--diff` | Compare two files side by side |
| `--keymap NAME` | Keymap profile: `default`, `vscode`, `emacs`, `macos`, `macos-gui` |
| `--theme NAME` | Theme name (e.g. `dark`, `light`) |
| `--config PATH` | Path to custom config file (default: `config.json` in the platform config directory, `~/.config/gvim/` on Linux) |
| `--log-file PATH` | Append status notices and plugin log to file |
| `--simple` | Open without restoring or saving session, and without file explorer |
| `--restore` | Force restoring previous session |
| `--no-restore` | Skip restoring previous tabs |
| `--stdin` (or `-`) | Read standard input into a scratch buffer |
| `--stdin-name NAME` | Name the stdin buffer, for its language and the Save-As suggestion |
| `--no-session` | Alias for `--no-restore` |
| `--no-plugins` | Skip discovering or loading plugins (`--plugin` still loads) |
| `--plugin PATH` | Load the plugin at PATH (a `.lua` file or a plugin directory); repeatable |
| `--daemon[=NAME]` | Run the editor as a daemon on a Unix socket (foreground) |
| `-a`, `--attach[=NAME]` | Attach this terminal to a running daemon |

Subcommands: `gvim config` (inspect the configuration), `gvim grammar list` (the syntax
grammars), `gvim rex install` / `gvim rex uninstall` (the Rex terminal integration) and
`gvim version`. `gvim --help` has the full description of each flag.

---

## Keybinding Cheat Sheet

*(Based on the `default` profile. Press `Ctrl+P` then type `>` for the full command palette.)*

### Navigation & Code Reading
| Key | Action |
|---|---|
| `Ctrl+P` | Quick Open (Files, `>commands`, `:line`, `#buffers`) |
| `Ctrl+Shift+P` | Open Command Palette directly |
| `Ctrl+T` / `Ctrl+N` | New tab / buffer |
| `Ctrl+Tab` / `Ctrl+Shift+Tab` | Next / Previous open buffer (notebook-friendly) |
| `Ctrl+Shift+Space` / `Alt+Shift+Space` | Smart expand / shrink selection (word → brackets → line → block) |
| `Alt+Left` / `Alt+Right` | Navigate Back / Forward in position history |
| `Alt+J` | Flash Jump (2-character on-screen jump motion; needs the `flash` plugin) |
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

### Code Search & Inspection
| Key | Action |
|---|---|
| `Ctrl+F` | In-file find bar (`Alt+C` case, `Alt+W` word, `Alt+R` regex) |
| `F3` / `Shift+F3` | Next / Previous search match |
| `Alt+A` | Project Search & Replace (auto-prefilled from selection) |
| `Alt+Q` / `Alt+M` | In Live Grep: export results to quickfix list |
| `Alt+D` | In Live Grep: toggle searching LSP diagnostics |

### LSP & Semantics
| Action | Description |
|---|---|
| `Go to Definition` | Jump to symbol definition (`F12`; `Ctrl+.` in the `macos` keymap) |
| `LSP: Peek Definition` | In-place definition preview card without switching tabs (`Alt+F12`; `Ctrl+Alt+.` in the `macos` keymap) |
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

The symbol, call-hierarchy, outline and breadcrumb rows (`Go to LSP Symbol` through `LSP: Breadcrumbs Outline`) come from the bundled `lsp_navigation` plugin, which is off by default.

### Git & Review
| Action | Description |
|---|---|
| `Git Gutter: Next/Prev Change` | Jump between modified regions |
| `Git: Toggle Blame` | Show/hide inline blame on the cursor line |
| `Git: Show Commit` | Show commit details popup for cursor line |
| `Git: Copy Commit Hash` | Copy commit SHA of current line to clipboard |
| `Review Diff: Next/Prev Hunk` | Jump between hunks across modified files |
| `Merge: Conflict Resolution` | Jump between conflict markers (`Alt+M` mode or `Ctrl+Alt+C`) |

### Scrollbar Overview Ruler
The right vertical scrollbar track acts as a real-time overview ruler:
- **Red marks**: LSP diagnostic errors located in the document.
- **Yellow marks**: LSP diagnostic warnings.
- **Blue marks**: Diagnostic informational hints.
- **Green/Gutter marks**: Git added and modified regions.
Allows scanning long files and spotting issues at a glance without having to scroll through thousands of lines.

### Sticky Scroll
When scrolling down into long functions, classes, or code blocks, the top row of the editor pane automatically pins the enclosing declaration header (e.g. `func (m *Model) paintPanes(...) {`), ensuring you never lose your place while reading code. Can be toggled on or off via the `toggle_sticky_scroll` action.

### Pager Mode (`gvim -R` / `git diff | gvim`)
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

### Markdown Modes
| Action | Description |
|---|---|
| `Markdown: Cycle Mode` | Cycle active pane: Source → Rendered → Dual → Source |
| `Markdown: Dual` | Side-by-side synchronized source and rendered split |
| `Markdown: Rendered` | Switch focused pane to composed page view |
| `Markdown: Source` | Switch focused pane to raw source view |

---

## Documentation

For architecture records, design documentation, and internal specifications, see:
- [`docs/architecture.md`](docs/architecture.md) — Module architecture, package boundaries, and import rules
- [`docs/ui.md`](docs/ui.md) — UI styling, component geometry, overlays, and prompt interaction
- [`docs/markdown.md`](docs/markdown.md) — Markdown page view, dual scroll synchronization, and parser mapping
- [`docs/settings.md`](docs/settings.md) — Configuration schema, JSONC persistence, and live settings
- [`docs/themes.md`](docs/themes.md) — Theme specification, color roles, and customization
- [`docs/terminal.md`](docs/terminal.md) — Pseudo-terminal dock and scrollback model
- [`docs/parity.md`](docs/parity.md) — Fresh parity matrix, deliberate differences and open gaps
