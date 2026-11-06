# GVIM Phase 1 Editor Spec

## 1. Goal and scope

Phase 1 is a usable terminal text editor for macOS (darwin/arm64 is the audited platform). It is built on bubbletea v2 and must stay pure Go.

In scope:
- Folder explorer rooted at a folder given on the command line or chosen with Open Folder. Recent folders are kept.
- Multi-tab editing with a tab strip, modified markers, tab switching, and tab close.
- Save. Atomic write with the original file mode kept (orbiton `writeFileSafely`, `savefile.go:44`, read).
- Undo and redo with micro-style grouping.
- New untitled buffers named `Untitled-N`. Save As for untitled buffers. Ctrl+S on untitled opens Save As (fresh behavior, observed).
- Unsaved-change prompts on tab close and on quit.
- Syntax highlighting with chroma.
- Full mouse and keyboard support, and a menu bar with underlined accelerators.
- Basic find and goto line, as optional items. Search uses stdlib `regexp`.

Out of scope: LSP, plugins, embedded terminal, Markdown, split panes, multi-cursor, macros, format, build, digraphs, comment toggle, header/source switch, reload-with-diff.

## 2. Library decisions

### Adopt

| Module and version | Replaces or serves | Purity evidence | Used by refs |
|---|---|---|---|
| `charm.land/bubbletea/v2 v2.1.1` (repo) | Input, rendering, mouse. Gives `MouseClickMsg`, `MouseReleaseMsg`, `MouseWheelMsg`, `MouseMotionMsg` (`mouse.go:83-128`, read) | Probe build exit 0 (run) | crush, toast |
| `charm.land/bubbles/v2 v2.2.1` | `textinput` for prompts (Save As, goto, open folder path), `spinner`. Not `list`, `tree` or `filepicker` (see Rejected) | Compiles, pure (run, tui) | crush (textinput, spinner) |
| `charm.land/lipgloss/v2 v2.0.6` | Styles, `StyleRanges` underline, `NewCompositor`/`NewLayer` for overlays and hit layers | Pure (run, tui) | crush, toast |
| `github.com/alecthomas/chroma/v2 v2.27.0` (repo) | Syntax highlighting in `internal/syntax` | Probe build exit 0, purity_check PURE_GO_OK (run) | Not used by refs. Refs use their own engines (micro `pkg/highlight`, orbiton `xyproto/syntax`). Chosen because it is pure and data-driven |
| `github.com/fsnotify/fsnotify v1.10.1` | Explorer refresh and external-change detection | Probe build exit 0 (run) | orbiton `monitor.go` |
| `github.com/atotto/clipboard v0.1.4` | System clipboard | Probe build exit 0 (run) | orbiton's `xyproto/clip` is a fork of it |
| `github.com/sahilm/fuzzy v0.1.3` | Palette and file-finder scoring | Pure (run, tui) | crush (`list/filterable.go:4` uses sahilm/fuzzy) |
| `github.com/charmbracelet/x/exp/charmtone v0.1.0` | Palette constants (crush Charmtone) | Pure (run, tui) | crush |
| `github.com/mattn/go-runewidth` (indirect, v0.0.16 per micro report) | Cell width | Not separately probed | micro, orbiton |
| `github.com/hymkor/trash-go v0.3.0` | Confirm delete moves to Trash instead of `os.RemoveAll` | Pure (run, tui: compile only) | Not used by refs. Caveat: not runtime-tested |
| `regexp` (stdlib) | Find | Stdlib | micro, orbiton |
| `encoding/json` (stdlib) | Config and recent-folders file | Stdlib | micro uses json5 fork, rejected |

Optional, deferred until needed: `github.com/charlievieth/fastwalk v1.0.14` (faster walk for finder), `github.com/bmatcuk/doublestar/v4 v4.10.4` (globs), `github.com/sergi/go-diff v1.4.0` (micro uses v1.1.0; reload diff only), `github.com/dlclark/regexp2/v2 v2.2.1` (lookaround only), `github.com/creack/pty v1.1.24` (terminal pane only), `github.com/zyedidia/clipper v0.1.1` (fallback clipboard if atotto is insufficient).

### Rejected

| Module | Reason |
|---|---|
| `github.com/smacker/go-tree-sitter` (in toast) | Cgo. `CGO_ENABLED=0 go build` fails with `undefined: Node` in `iter.go` (run, tui). Default env lists it in CgoFiles |
| `golang.design/x/clipboard v0.11.0` | Pulls `ebitengine/purego`, which imports `runtime/cgo`. purity_check exit 1 with `cgo files found in dependency graph:` / `runtime/cgo` (run, tui) |
| `github.com/micro-editor/tcell/v2 v2.0.13`, `gdamore/tcell/v2 v2.13.10`, `rivo/tview v0.42.0` | Bubbletea covers input and screen. Pure, but a different event model |
| `charm.land/huh/v2 v2.0.3` | Pure, but its own keymap and focus model and a large graph |
| `github.com/lrstanley/bubblezone/v2 v2.0.0` | Not needed. Custom hit map covers it (global state) |
| `github.com/sabhiram/go-gitignore` (2021) | Stale. If gitignore is needed later, test `go-git` gitignore (untested) |
| `bubbles/v2 list`, `tree`, `filepicker` | No mouse (list, tree). filepicker Back includes `esc` (`filepicker.go:88`). ARCH-v4 decision holds |
| `github.com/yuin/gopher-lua`, `micro-editor/json5`, `gopkg.in/yaml.v2` | Plugins and micro grammars are out of phase 1 |
| `xyproto/syntax`, `xyproto/mode`, `xyproto/vt`, `sajari/fuzzy` | Hand-written per-language scanners, terminal VT, spellcheck. Not for phase 1 |
| `ultraviolet/layout` (crush uses it, `model/ui.go:66`) | ARCH-v4 decision 12 says no. Revisit only if layout math gets hard |
| Undo snapshots (orbiton approach) | 512 full copies, memory-heavy. Write our own micro-style delta undo |

### Purity evidence

- Repo script (run, audit): `bash /Users/tbxark/Repos/tbxark/gvim/migration/tools/purity_check.sh` → `PURE_GO_OK`.
- Candidate probe (run, micro/orbiton): module `purityprobe` in `/tmp/gvim-analyst/purity-candidates`. `CGO_ENABLED=0 go build ./...` exit 0, no CgoFiles, no `runtime/cgo`, 208 deps, `go run .` prints `true`.
- Micro (run): `CGO_ENABLED=0 go build ./...` exit 0, CgoFiles empty, 306 deps.
- Script defect (read, tui): `go list` lines in `purity_check.sh` do not set `CGO_ENABLED=0`, so the second gate depends on the environment. Fix: export `CGO_ENABLED=0` for the `go list` calls too.

## 3. UX spec

### Layout (top to bottom)
- Row 0: menu bar. `File  Edit  View  Help`.
- Row 1: tab strip.
- Body: explorer panel (default width 32, matching crush sidebar 32, `model/ui.go:4490`; collapsible with Ctrl+B; divider draggable, min 16, max 60) on the left, editor on the right.
- Last row: status bar. Left: message. Center: path. Right: `Ln x, Col y`, `UTF-8`, line ending, chroma language name.
- Dialogs and dropdowns are overlays. Hit regions are rebuilt on each render from explicit rects, the way toast builds its tab hit map (`tabbar.go:189`).

### Colors (crush Charmtone Pantera, `styles/themes.go:55`)
- primary Charple `#6B50FF`, secondary Dolly `#FF60FF`, accent Bok `#68FFD6`
- onPrimary Butter `#FFFAF1`, fgBase Sash `#ECEBF0`, fgSubtle Smoke `#BFBCC8`, fgMoreSubtle Squid `#858392`, fgMostSubtle Oyster `#605F6B`
- bgBase Pepper `#201F26`, bgLeastVisible BBQ `#2D2C36`, bgLessVisible Char `#3A3943`, bgMostVisible Iron `#4D4C57`
- error Sriracha `#EB4268`, destructive Coral `#FF577D`, warning Mustard `#F5EF34`, info Malibu `#00A4FF`, success Julep `#00FFB2`
- Toast's alternative (`theme/system.go:50+`) derives UI colors from the terminal 16-color palette, with muted = blend(bg, fg, 0.52) and mutedStrong = 0.72. Use it as the fallback for 16-color terminals (proposed).

### Borders and glyphs
- Dialogs and dropdowns: `lipgloss.RoundedBorder()`, primary color (crush `quickstyle.go:1161`, toast `closedialog.go:105`).
- Dialog title: `╱` fill, primary to secondary gradient (crush `common/elements.go:253`).
- Focus bar: thick `▌` in primary when the panel is focused, thin `│` in bgMostVisible when blurred (crush `quickstyle.go:1058-1066`). Used on the explorer panel and the editor.
- Scrollbar: thumb `┃` in secondary, track `│` in separator (crush `styles.go:74-75`).
- Modified marker: `●` before the tab name (toast `tabbar.go:179`). The legacy `*` is dropped.
- Tab label: `" name × "`. Exit glyph `×` (toast `tabbar.go:15`).
- Tree glyphs: `▼` expanded, `▶` collapsed (bubbles tree `New`). Files are indented with no glyph.
- Status glyphs: `●` pending, `✓` ok, `×` error.

### Focus indication
- Focused panel shows the thick focus bar. The explorer and editor are never both thick.
- Selected explorer row: crush `SelectedItem`, bg primary, fg onPrimary, padding 0 1 (`quickstyle.go:1173`). When the explorer is blurred, the row uses bgMostVisible with fgSubtle.
- Active tab: bgLessVisible with fgBase. Inactive tab: bgBase with fgMoreSubtle.
- Focused dialog button: onPrimary on Dolly. Blurred button: fgBase on Char. Hovered button: Oyster bg (crush `quickstyle.go:905-906`).
- Menu title open: bg primary, fg onPrimary. Highlighted dropdown item: crush `SelectedItem`.
- Editor cursor: reverse video.

### Underlined accelerators
- Style: `lipgloss.StyleRanges(text, NewRange(i, i+1, style.Underline(true)))`, with only the accelerator rune underlined (crush `common/button.go:30` and `:56`). No brackets.
- Set the index explicitly. Crush's `UnderlineIndex` is 0-based and its zero value means the first letter (`dialog/quit.go:149-150` relies on that). Never leave it unset.
- Bug to fix (audit J): gvim underlines all 100 cells of row 0, including spaces. Fix in the menubar render. The render code was not located (audit: "did not locate the menubar render code"), so start in `internal/menubar`.
- Menu titles: `File` F, `Edit` E, `View` V, `Help` H. Dropdown items use the first free letter of the label, scanning left to right (orbiton's `selectionLettersForChoices`, `menu.go:78-112`, read).
- Dropdown items (proposed letters): File: New File N, Open File O, Open Folder F, Save S, Save As A, Close Tab W, Quit Q. Edit: Undo U, Redo R, Cut X, Copy C, Paste P, Select All S, Find F, Find Next N, Goto Line G, Duplicate D, Cut Line L. View: Explorer E, Focus Explorer F... (see the rule above; resolve clashes with the first free letter), Next Tab N, Previous Tab V, Command Palette C, Line Numbers L. Help: Keyboard Shortcuts K.

### Menu activation (fresh behavior, observed in the audit)
- F10 opens the File dropdown. Right and Left move between titles. Up and Down move in the dropdown. Enter activates. Esc closes. A letter in an open dropdown activates that item.
- Alt+F, Alt+E, Alt+V, Alt+H open the title directly. Caveat: macOS Terminal and iTerm2 need Option-as-Meta (or "Esc+") to send these. F10 is the universal path.
- Mouse click on a title opens it. Hover over another title while a menu is open switches to it. This needs `AllMotion` mode, which is enabled only while a menu is open (crush `ui.go:3861`).

### Dialog set
- Unsaved changes: body `Save changes to <name>?`. Buttons **S**ave (default focus), **D**iscard, **C**ancel. Keys: s, d, c, Esc (cancel). Enter activates the focused button. Grace period: ignore Enter for 425 ms after open, max 1500 ms (crush `dialog.go:63, :67`, `OpenDialogWithGrace` `:137`). Click outside does nothing.
- Quit with several dirty tabs: one unsaved dialog per tab, in tab order. Cancel aborts the quit. Untitled tabs go through Save As.
- Save As: title `Save As`, prefilled `<explorer root>/Untitled-N.txt`, `textinput`. Buttons **S**ave and **C**ancel. Enter saves. If the target exists, an overwrite dialog offers **R**eplace and **C**ancel (micro's YN prompt, `SaveAsCB`). A missing parent directory is an inline error.
- Confirm delete: `Move "<name>" to Trash?`. Buttons Move to **T**rash and **C**ancel. Keys: t, c, Esc.
- Open folder picker: `textinput` for path. Below it, the recent-folders list (up/down, Enter opens). Also a directory list for the current path (dirs only, `..` to go up). Buttons **O**pen (current directory) and **C**ancel.
- Error dialog for save failure (permission denied). No sudo fallback.

## 4. Keyboard map (macOS)

"Key" is the default in the macos keymap (`internal/keymap/macos.json`, inherits `default`, `keymap.go:142-146`). "New" marks a proposed change. Sources: audit = gvim PTY run, micro = `defaults_other.go`/`defaults_darwin.go` (read), toast = `config/keybinding.go:120` (read), fresh = legacy binary PTY run.

| Action | Key | Menu accel | Justification |
|---|---|---|---|
| Menu bar open | F10 | — | fresh (audit J, PASS) |
| Open title by Alt | Alt+F/E/V/H | title letter | fresh `menu_open` (audit J). Caveat: Option key |
| Menu navigate | ←/→ titles, ↑/↓ items, Enter, Esc | — | fresh (audit J) |
| Cycle focus (menu, tabs, explorer, editor) | F6 (New) | — | Proposed. No ref binds it |
| Focus explorer | Ctrl+E | View > F | macos `focus_file_explorer` (audit) |
| Toggle explorer | Ctrl+B | View > E | macos (audit); toast `ctrl+b` sidebar |
| New untitled file | Ctrl+N | File > N | macos `new` (audit); fresh menu `New File ⌃N`; toast `ctrl+n` |
| Open file (path prompt) | Ctrl+O | File > O | micro `ctrl-o` |
| Open folder | Alt+O (New, check macos.json) | File > F | Proposed. Reachable also via the palette |
| Save | Ctrl+S | File > S | macos `save` (audit); micro; toast; fresh |
| Save As | Alt+S (New) | File > A | Proposed. Ctrl+Shift+S cannot be sent in a terminal. fresh: Ctrl+S on untitled opens Save As (audit E) |
| Close tab | Ctrl+W (New), Alt+W kept | File > W | toast `ctrl+w`. Alt+W is macos `close_tab` (audit B). Ctrl+W currently is `select_word` (audit): remove that binding |
| Quit | Ctrl+Q | File > Q | macos `quit` (audit); fresh; toast; micro `ctrl-q` |
| Next tab | Ctrl+PgDn | View > N | macos `next_buffer` (audit B, PASS). Alt+, / Alt+. from micro as extra |
| Previous tab | Ctrl+PgUp | View > V | macos `prev_buffer` (audit B, PASS) |
| Command palette | Ctrl+T | View > C | macos palette (audit). toast uses ctrl+shift+p (not sendable) |
| Help | F1 | Help > K | orbiton `F1` hotkey overview |
| Cancel or close overlay | Esc | — | crush `dialog.go:26` (CloseKey esc, alt+esc) |
| Undo | Ctrl+Z | Edit > U | macos (audit D, PASS); micro; toast; fresh `Undo ⌃Z` |
| Redo | Ctrl+Y, Ctrl+R | Edit > R | macos (audit D, PASS); micro `ctrl-y` |
| Cut | Ctrl+X | Edit > X | micro; orbiton |
| Copy | Ctrl+C | Edit > C | micro; orbiton |
| Paste | Ctrl+V | Edit > P | micro; orbiton |
| Select all | Ctrl+A | Edit > S | micro `ctrl-a` |
| Find | Ctrl+F | Edit > F | micro `ctrl-f`; orbiton `keyloop.go:850` |
| Find next / previous | F3 / Shift+F3 | Edit > N | micro F3/F7. Shift+F3 proposed |
| Goto line | Ctrl+L | Edit > G | micro `ctrl-l`; orbiton `keyloop.go:1984` |
| Duplicate line | Ctrl+D | Edit > D | micro `Duplicate` (`actions.go:1436`). orbiton uses Ctrl+D for delete char: not adopted |
| Cut line | Ctrl+K | Edit > L | micro `ctrl-k` |
| Line numbers toggle | none | View > L | micro Ctrl-r conflicts with redo here |
| Explorer: move | ↑/↓ | — | Standard |
| Explorer: open file / expand | Enter, → | — | Standard. lazygit not analyzed |
| Explorer: collapse or parent | ← | — | Standard |
| Explorer: new file in folder | N | — | Proposed |
| Explorer: move to Trash | Del / Backspace | — | Proposed (confirm dialog) |
| Dialog: move focus | Tab, Shift+Tab, ←/→ | — | crush `quit.go` |
| Dialog: accept | Enter (after grace) | — | crush |
| Dialog: button letter | case-insensitive letter | — | crush `permissions.go:123-131` |
| Editor: select | Shift+arrows | — | micro |
| Editor: doc start / end | Ctrl+Home / Ctrl+End | — | micro |

Terminal caveats:
- Ctrl+S may be flow control under XON/XOFF. Bubbletea's raw mode should clear IXON. Verify in the PTY.
- Cmd keys and Ctrl+Shift chords are not sendable without the kitty keyboard protocol. None are used.
- Ctrl+Shift+Z is untested (audit, not verified).

## 5. Mouse map

| Target | Gesture | Result |
|---|---|---|
| Editor text | Click | Caret moves to cell (micro `MousePress`, `actions.go:59`) |
| Editor text | Double click | Select word. Triple click selects line (`DoubleClickThreshold = 400`, `config/globals.go:4`) |
| Editor text | Drag | Extend selection by char (word or line after double/triple) (micro `MouseDrag` `:111`) |
| Editor text | Wheel | Scroll 3 lines per notch (proposed). Audit: wheel ×3 scrolled 9 lines, PASS-ish |
| Editor text | Middle click | Paste primary (micro). Optional, needs primary selection |
| Tab | Click | Activate tab (micro `TabList.HandleEvent` `tab.go:109`) |
| Tab | Click × | Close tab. Modified tab opens unsaved dialog |
| Tab | Middle click | Close tab (toast) |
| Tab bar | Wheel | Scroll tabs at edges (micro) |
| Tab | Drag | Reorder (optional) |
| Explorer row | Click | Select row and focus explorer |
| Explorer row | Click on ▶/▼ glyph | Toggle folder |
| Explorer row | Double click folder | Toggle folder |
| Explorer row | Double click file | Open file, or focus its tab if already open |
| Explorer | Wheel | Scroll tree |
| Explorer divider | Drag | Resize explorer, min 16, max 60 |
| Menu title | Click | Open dropdown. Click the open title again to close |
| Menu title | Hover while a menu is open | Switch to that dropdown |
| Dropdown item | Hover | Highlight. Needs `AllMotion` while open |
| Dropdown item | Click | Activate |
| Click outside dropdown | Click | Close |
| Dialog button | Click | Activate (crush `button.go:117` `HitButtonIndex`) |
| Dialog | Click outside | No-op for destructive dialogs |
| Status bar | Click on the modified indicator | Save (optional) |

Hit regions: Compute explicit rects in `internal/ui` during render, and dispatch from `handleClick`. The audit found `hit.go:9-11` defines `hitTab`, `hitTabClose`, `hitTabNew`, but `handleClick` (`model.go:1016-1031`) has no branches for them. The explorer panel's `Update` is never called (`model.go:195-203`), and its `MouseClickMsg` handling (`explorer/panel.go:104-116`) is dead code. Both must be wired up.

## 6. File and buffer behavior

- Open folder: the folder argument is the explorer root and the explorer is visible at startup (audit A: PARTIAL, folder works but explorer is hidden until Ctrl+B). Open Folder changes the root. Recent folders are saved as JSON under `os.UserConfigDir()/gvim/recent.json` (proposed). The explorer shows dirs first, then files, sorted case-insensitively. Hidden files and `.git` are skipped by default (proposed).
- Open file: Ctrl+O prompts for a path. A path that is already open switches to its tab. A missing path opens an empty buffer bound to that path, and Save writes it (orbiton behavior, `-k` in `savefile.go:111`).
- Tabs: open, close, switch (Ctrl+PgDn/PgUp, click), middle-click close, and optional reorder by drag. Each tab owns its buffer, undo stack, and lexer.
- Modified marker: a tab is modified when the undo position differs from the position at the last save. This is the usual approach (proposed; micro's source was not checked for this).
- Save: a named buffer writes atomically (temp file, rename, mode kept). Clear the modified marker on success. Untitled buffers route to Save As. Save errors show the error dialog.
- Undo and redo (micro's `eventhandler.go`, read, not run): each edit is a delta `TextEvent`. `Execute` clears the redo stack (`:238`). Undo `:257`, redo `:304`. Events whose timestamps fall in the same 1000 ms wall-clock bucket are undone together (`undoThreshold` `:25`, `:257-279`). Consequence: a fast PTY typing run such as `one two three` undoes in one Ctrl+Z. Acceptance D must pause more than 1 s between words to see per-word groups. Breaking a group on save is a proposal, not from micro.
- New file: Ctrl+N creates `Untitled-N`, with N a per-session counter starting at 1. It is not modified until typed in. Ctrl+S on it opens Save As with the prefill above. On success the tab is renamed, the lexer is recomputed from the new filename, and the explorer refreshes if the path is under the root.
- Close tab: if modified, the unsaved dialog opens. Save saves and then closes (Save As first for untitled). Discard closes. Cancel does nothing. Errors from `internal/editor/editor.go:21` (`ErrUnsaved`) and `document.go:89-90` (`ErrNoPath`) are the existing hooks.
- Quit: Ctrl+Q goes through the same sequence as close for each dirty tab. Cancel stops the quit. With no dirty tabs, quit is immediate. (orbiton quits with no prompt, `keyloop.go:731-736`. gvim does the same today, audit G.)
- External change (stretch): fsnotify refreshes the explorer. A clean tab changed on disk reloads silently. A modified tab shows a Reload or Keep prompt (micro setting `reload` default `prompt`, `config/settings.go:85`).

## 7. Syntax highlighting

- Engine: chroma v2.27.0 through `internal/syntax`.
- Lexer selection: `lexers.Match(filename)`, then `lexers.Analyse(content)` on the first lines for untitled or unknown names, then `lexers.Fallback`.
- Tokenize the full buffer, because chroma lexers are stateful across lines. Cache the token output per tab, keyed on the buffer revision. Recompute only when the revision changes, on the next render. Render only the visible range.
- Size guard (proposed): above about 2 MiB, skip highlighting and render plain text. Measure before fixing the number.
- Theme keys: a chroma `Style` built with `chroma.MustNewStyle` from the crush palette. Proposed entries: Keyword `#FF60FF`, String `#68FFD6`, Number `#F5EF34`, Comment `#858392` (italic), Name.Function `#00A4FF`, Operator `#FFFAF1`, Error `#EB4268`. Map to the existing `internal/theme` keys (not read in this step).
- Acceptance I checks that a keyword and a comment get distinct colors that are not the default text color.

## 8. Work breakdown and gates

### Owners and files

| Owner | Files (verified where noted) | Scope |
|---|---|---|
| Shell and layout | `internal/ui/model.go` (verified: `handleClick` 1016-1031, `explorerPanel.Update` 195-203, `menu_open` dispatch 955-1000), `internal/ui/hit.go` (verified 9-11) | Layout rects, hit map, focus cycle, mouse routing, overlay stack |
| Menu bar | `internal/menubar` (render not located) | Underline fix, F10 and Alt activation, dropdowns, hover |
| Tabs | `internal/editorpane`, new tab-strip file under `internal/ui` | Tab strip, hit map, marker, close, middle-click, Ctrl+PgDn/PgUp |
| Explorer | `internal/ui/explorer/panel.go` (verified 104-116), `internal/explorer` | Tree, mouse, keys, fsnotify refresh, new file, delete |
| Buffer, undo, save | `internal/buffer`, new `undo.go` in `internal/buffer`, `internal/editor/editor.go` (verified :21), `internal/editor/document.go` (verified :89-90), `internal/fsops` | Delta undo, atomic save, untitled naming, marker |
| Dialogs | New dialog file under `internal/ui` (or existing overlays) | Unsaved, Save As, confirm delete, open folder, button component, grace |
| Syntax and theme | `internal/syntax`, `internal/theme`, `internal/style` | Chroma lexer selection, cache, chroma style |
| Keymap | `internal/keymap/macos.json`, `internal/keymap/keymap.go` (verified :142-146) | Ctrl+W, Alt+F removal (`f alt move_word_right`), Ctrl+N, Alt+S, F10, Alt+O |
| Gates | `migration/tools/purity_check.sh`, `migration/tools/import_rules_check.sh` (existence unverified) | Run gates, fix the CGO_ENABLED defect |

Repo packages `internal/fuzzy`, `internal/palette`, `internal/finder` exist. Replacing their scoring with `sahilm/fuzzy` is recommended only after reading them. That was not done here.

### Gates
- `go build ./...` and `CGO_ENABLED=0 go build ./...`
- `go vet ./...`
- `golangci-lint run ./...` (version not specified in the reports)
- `go test -race ./...`
- `go fix -diff ./...`
- `go mod tidy -diff`
- `bash migration/tools/purity_check.sh` (expect `PURE_GO_OK`, exit 0)
- `bash migration/tools/import_rules_check.sh` (existence unverified)
- PTY acceptance A through J (below). The harness is `/tmp/gvim-audit-work/drive.py` with `wait: 1` (first shot is blank without it). `/tmp` is not persistent, so copy the harness into the migration tooling.

### PTY acceptance (expected results after the work)
- A. `gvim .`: the explorer is visible at startup with root `ws` expanded. Enter on `README.md` opens tab `README.md` and status `Opened README.md`.
- B. Open `a.txt` and `b.txt`. Ctrl+PgDn shows `alpha`, Ctrl+PgUp shows `beta`. Type X in `b.txt`: tab reads `● b.txt ×`. Ctrl+W opens the unsaved dialog. Esc keeps the tab. Ctrl+W then D closes it. Alt+W on a clean tab closes without a dialog.
- C. Type `Xy ` in `a.txt`, then Ctrl+S. The marker clears and the file on disk begins `Xy alpha`.
- D. Type `one two three` with pauses of over 1 s between words. Ctrl+Z ×2 gives `one two`. Ctrl+Y gives `one two `. Ctrl+Z ×4 gives empty. A fast run undoes in one step (micro grouping).
- E. Ctrl+N twice gives `Untitled-1` and `Untitled-2`. Type `hello` in one, then Ctrl+S. The Save As prompt appears. Enter `new.txt`. The file on disk is `hello`, the tab is renamed `new.txt`, and no `[No Name]` appears.
- F. Dirty `a.txt`, close by Ctrl+W and by clicking ×. Both open the unsaved dialog. Cancel keeps the tab. Save writes and closes.
- G. Dirty tab, then Ctrl+Q. The dialog appears. Cancel keeps the process alive. Discard exits with status 0. Save writes, then exits.
- H. Mouse: click the `a.txt` tab (x 2 to 4, y 2) to activate it. Click × to close a clean tab. Single-click the explorer row to select. Click ▶ to expand. Double-click `README.md` to open it. Click `File` to open the dropdown. Wheel scrolls the editor.
- I. Open `main.go`. `package` uses the keyword color, comments use the comment color, strings and numbers are colored, and none of them use the default text color.
- J. Row 0 underlines only `F`, `E`, `V`, `H`. F10 opens File, and Right moves to Edit. Alt+E opens Edit (with Option-as-Meta). Alt+F opens File and no longer moves the word.

## 9. Known gaps and out of scope

Gaps against the audit (all FAIL items, with the spec section that fixes each):
- Tab mouse click and × click do nothing (audit H). Fix: `handleClick` branches, section 5.
- Explorer mouse is dead code (audit H, `model.go:195-203`). Fix: call `Update`.
- Explorer open on double click fails (audit H).
- Ctrl+W selects word, not close (audit B, F). Fix: section 4.
- Alt+W closes a modified tab with only a status message (audit B, F). Fix: unsaved dialog.
- Ctrl+S on untitled shows `buffer has no file name` (audit E, `document.go:89-90`). Fix: Save As prompt.
- Ctrl+N gives a second `[No Name]`. Fix: `Untitled-N`.
- Ctrl+Q quits without prompt (audit G). Fix: quit flow.
- Menu underline covers all of row 0 (audit J). Fix: explicit ranges.
- Alt+letter menus unimplemented (audit J, `menu_open` has no dispatch). Fix: menubar.
- Alt+F shadowed by `f alt move_word_right` in macos.json (audit J). Fix: remove that binding.
- Stale status text after tab switch (`Opened b.txt`, audit). Fix: status per tab.

Gaps in the research:
- lazygit was not analyzed by any report. Panel and key choices for the explorer rest on crush and toast only.
- fresh was analyzed only as a PTY behavior reference (`refs/fresh/target/release/fresh`, Cargo layout, probably Rust, not verified). Its source was not read, and it is not a library source.
- micro's darwin key defaults were not fully transcribed. The macOS column relies on the gvim macos keymap and the audit.
- Undo grouping and the save boundary were read from source, not run.
- Ctrl+Shift chords are untested. Option-as-Meta behavior depends on the terminal.
- `trash-go` is compile-verified only. `go-git` gitignore is untested.
- `import_rules_check.sh` appears in the gate list but no report verified that it exists.
- The purity script's `go list` lines need the CGO_ENABLED=0 fix.
- No large-file, encoding (non-UTF-8), CRLF, or Windows analysis.
- ARCH-v4 (`migration/ARCH-v4.md`) was referenced in the reports but not read in this step.

Out of scope (phase 1): LSP, plugins, terminal pane, Markdown, split panes, multi-cursor, macros, format, build, digraphs, comment toggle, header/source switch, reload-with-diff, sudo save, Windows.