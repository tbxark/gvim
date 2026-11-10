# gvim

A fast, keyboard-driven terminal code viewer and editor written in pure Go.

Built on [Bubble Tea v2](https://github.com/charmbracelet/bubbletea) with a custom cell grid renderer, `gvim` is designed for reading code, navigating large projects, inspecting git diffs, and exploring codebases with speed and safety.

---

## Highlights

- **Fast Code Viewer & Read-Only Protection**
  - Launch with `gvim -R` / `gvim --view` to lock all buffers in read-only mode, protecting your codebase against accidental keystrokes, pastes, or saves.
  - Syntax highlighting for 30+ programming languages powered by TextMate and Tree-Sitter grammars.
  - Code folding based on indentation and syntax (`▼` / `▶`), supporting keyboard and mouse toggles.
  - Windowed streaming for large files (100+ MiB) without allocating the whole file into memory.

- **Semantic Navigation & LSP**
  - Go to Definition, Implementation, and Type Definition.
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

- **Multi-Modal Fast Search**
  - **Quick Open (`Ctrl+P`)**: fuzzy file search, line jump (`:line`), relative line jump (`:+N` / `:-N`), buffer list (`#`), and command palette (`>`).
  - **Live Grep (`Alt+A`)**: project-wide search with scopes for files, open buffers, terminal transcripts, and LSP diagnostics (`Alt+D`), exportable to a quickfix list (`Alt+Q` / `Alt+M`).
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
- Go 1.23 or later

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
| `--config PATH` | Path to custom config file (default: `~/.config/gvim/config.json`) |
| `--log-file PATH` | Append status notices and plugin log to file |
| `--simple` | Open without restoring or saving session, and without file explorer |
| `--restore` | Force restoring previous session |
| `--no-restore` | Skip restoring previous tabs |
| `--stdin` (or `-`) | Read standard input into a scratch buffer |
| `--no-plugins` | Skip discovering or loading plugins |

---

## Keybinding Cheat Sheet

*(Based on the `default` profile. Press `Ctrl+P` then type `>` for the full command palette.)*

### Navigation & Code Reading
| Key | Action |
|---|---|
| `Ctrl+P` | Quick Open (Files, `>commands`, `:line`, `#buffers`) |
| `Ctrl+Shift+P` | Open Command Palette directly |
| `Ctrl+T` / `Ctrl+N` | New tab / buffer |
| `Alt+Left` / `Alt+Right` | Navigate Back / Forward in position history |
| `Alt+J` | Flash Jump (2-character on-screen jump motion) |
| `Ctrl+]` | Jump to matching bracket |
| `Ctrl+L` | Expand line selection across all carets |
| `Ctrl+W v` / `Ctrl+W s` | Split window vertically / horizontally |
| `Ctrl+W w` / `Ctrl+W h/j/k/l` | Switch active split pane |
| `Ctrl+W c` / `Ctrl+W q` | Close current split pane |
| `Alt+` `` | Toggle terminal dock |

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
| `LSP: Go to Definition` | Jump to symbol definition |
| `LSP: Find References` | Open references list with preview tabs |
| `LSP: Go to Implementation` | Jump to implementation |
| `LSP: Hover` | Show hover documentation popup |
| `Go to LSP Symbol` | Fuzzy-find document symbols with enclosing-symbol preselected |
| `LSP: Workspace Symbols` | Fuzzy-find symbols across the whole workspace |
| `LSP: Incoming / Outgoing Calls` | Call Hierarchy: explore callers and callees |
| `List Bookmarks` | Interactive bookmarks picker (jump between slots 0–9) |

### Git & Review
| Action | Description |
|---|---|
| `Git Gutter: Next/Prev Change` | Jump between modified regions |
| `Git: Toggle Blame` | Show/hide inline blame on the cursor line |
| `Git: Show Commit` | Show commit details popup for cursor line |
| `Git: Copy Commit Hash` | Copy commit SHA of current line to clipboard |
| `Review Diff: Next/Prev Hunk` | Jump between hunks across modified files |
| `Merge: Conflict Resolution` | Jump between conflict markers (`Alt+M` mode or `Ctrl+Alt+C`) |

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
- [`docs/parity.md`](docs/parity.md) — Live Fresh parity audit matrix and objective record
