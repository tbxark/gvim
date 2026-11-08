# GAP-v3: gvim port vs full Fresh TUI

> Historical record: package paths in this file are pre-reorganization. See
> `docs/architecture.md` for the current layout.

> **Superseded (plugin runtime).** Section 4 ("Plugin language decision"), milestone M7, and the rows that mention JS plugins describe the old plan. The plugin runtime in this repository is **Lua on
> `github.com/yuin/gopher-lua`** (`internal/plugin`), not JavaScript. The JS system (`goja`, `esbuild`,
> `slowjs`) was removed completely, and `.js`/`.ts` files in the plugin folder are never executed.
> The design record is the Lua plugin design (`docs/plugins/` for users and plugin authors,
> `migration/ARCH-v4.md` for the package graph). Everything below that talks about a JavaScript or
> TypeScript runtime, `RegisterHostFunc`, Promises, `slowjs`, `goja` or `esbuild` is history.

Evidence tags: **[R]** verified by a command run in this survey (the purity matrix and repo resolve). **[P]** verified by a prior survey's PTY run or probe, not re-run here. **[I]** inferred from reading source or docs. **[E]** my effort estimate, unvalidated.

## 1. Summary

The port reproduces Fresh's keymap, palette and theme shape, but executes a small fraction of it. Most menus are not dropdowns, and no file-access, search, highlighting, LSP, plugin or terminal features exist.

| Area | Fresh (count) | Port (count) | Source |
|---|---|---|---|
| Top-level menu labels | 8 (Explorer only while explorer focused; Selection gated on a buffer) | 7 always, plus Explorer while focused | [P] menus survey; `layout.go:312` |
| Labels that open a dropdown | 8 | 2 (File, Edit) | [P] `menu_ui.go:83` `dropdownFor` |
| Dropdown rows | 138 = 103 leaves + 3 submenu headers + 31 separators + 1 version label | File 15 and Edit 22 match Fresh row-for-row | [P] PTY parse |
| Leaf actions in a dropdown | 103 | 27 (26%); 76 unreachable by mouse (View, Selection, Go, LSP, Explorer, Help) | [P] |
| Clickable non-separator rows (File/Edit) | 28 (27 leaves + "Copy with Formatting" header) | 28; 10 run an action, 18 show "Not supported in this port" | [P] |
| Leaf actions with an implementation anywhere | 103 | 20 | [P] |
| Leaf actions that execute from a menu | 103 | 10 (File: New, Save, Quit; Edit: Undo, Redo, Cut, Copy, Paste, Select All, Find) | [P] |
| Bar labels that press but open nothing | n/a | 5 (View, Selection, Go, LSP, Help) | [P] `mouse_actions.go:98–106` |
| Keymap bindings | 276 | 276 loaded; 66 (23.9%) bind to implemented actions | [P] identical `default.json` |
| Keymap distinct actions | 212 | 59 handled (27.8%): 53 editor handlers + 12 TUI names | [P] |
| Bindings by context (implemented/total) | n/a | normal 52/141; file_explorer 11/25; global 2/12; menu 1/6; the other 9 contexts 0 | [P] |
| Palette commands | 206 | 22 implemented (10.7%); the rest print "Not supported" | [P] `commands_gen.go` |
| File explorer rows (gap tables) | 36 rows | 7 done, 4 partial, 1 wrong (Right/Left), 24 missing | [I] my count of the explorer survey tables |
| File finder | 8 functional rows | 0 done (Ctrl+P is `>` only) | [P] explorer survey |
| Open folder / workspace | 6 rows | 0 done | [P] explorer survey |
| Editing features | 27 rows | 3 yes (Smart Home, Line numbers, Undo grouping), 3 partial (wrap, whitespace, encoding), 21 no | [I] my count of the editing survey table |
| Search and replace | 17 capability rows | 0 functional; 4 UI-only or partial (find bar, 3 toggles); 13 missing | [P] editing survey; `frame.go:33–37` |
| Syntax highlighting | 75 syntect grammars + 5 tree-sitter langs (`fresh-languages/Cargo.toml:19–35`) | 0; theme keys exist but nothing consumes them | [P] file counts; `theme.go:5` |
| LSP | 14 LSP menu items plus popups and diagnostics | 0 | [P] |
| Plugins | 50 bundled TS plugins; 358 `EditorAPI` method signatures (`fresh.d.ts` 5258–8659); Fresh's own backend 75/122 methods (61%) | 0 runtime | [P] |
| Terminal | View > Terminal, 5 items; terminal panes | 0 | [P] |
| Config files | layered JSONC (`configuration/index.md:37–47`) | 0 read; `theme.Load("dark")` hard-coded in `main.go` | [P] grep for config paths: no hits |
| Mouse parity (stored report, not re-run) | 27 cases | 25 PASS, 2 FAIL (M06 timing; M16 needs multi-cursor) | [P] `report-mouse-final.json` |

Denominators differ: "20 of 103" counts menu leaves, "59 of 212" counts keymap action names, and "22 of 206" counts palette commands. Do not add them.

Conflicts between the survey reports, resolved:
- **Ctrl+G.** The menus survey says port Ctrl+G "matches legacy" and lists Go to Line as Ctrl+L. `internal/keymap/default.json` lines 730–734 [R] bind `goto_line` to `ctrl+g` with `when: normal`, and the editing survey says the port's `goto_line` is a status-text stub (`handlers.go:163`). So Ctrl+G is a real gap, and the Ctrl+L shortcut in the menus table is unverified.
- **Ctrl+P.** Docs say file finder. Code (`app/prompt_lifecycle.rs:226–227`) and PTY say command mode. Code wins.
- **Ctrl+O.** Keymap and PTY say Open File; docs say Quick Open. Code wins.
- **Esc.** Bound to `remove_secondary_cursors`, so idle Esc prints "Not supported" [P]. This should be a no-op.

## 2. Backlog

Priority: P0 blocks normal use or causes wrong-target or data bugs. P1 is common. P2 is niche. Owners are port packages, plus proposed new packages marked (new). Milestones are ordered by dependency; the effort total is about 130–140 person-days [E] before the remaining plugin API.

### M1: Honest menu bar and key routing (about 7d, no new deps)
Exit: all 8 labels open dropdowns, and keys cannot hit the wrong target.

1. **P0, 3d, internal/tui.** Dropdowns for all 8 labels; Left/Right cycle; hover switching; Explorer label only while focused. Refs: `menu_ui.go:83`, `layout.go:312`, `mouse_actions.go:98–106`.
2. **P0, 2d, internal/tui (generated table).** Replace the hand list in `menu_ui.go:17–60` with a generated table from `refs/fresh/crates/fresh-editor-core/src/config.rs` `translated_menus()`: 103 leaves, 3 submenu headers, 31 separators, `when` enablement on 56 leaves (rendered disabled, skipped by keyboard). Generated file is committed; the generator never runs at build time.
3. **P0, 0.5d, internal/tui and internal/keymap.** Ctrl+C/X/V in the explorer context run `copy`/`cut`/`paste` on the active document (`internal/editor/handlers.go:109–126`) [I]. Stop-gap: show "Not supported" in explorer context until M2 item 15 lands.
4. **P0, 0.5d, internal/editor and internal/tui prompt.** Real Go-to-line prompt for Ctrl+G (`goto_line`), accepting `N` and `N:COL`. Today it is a stub at `handlers.go:163` [P].
5. **P0, 0.2d, internal/editor.** `remove_secondary_cursors` with one cursor becomes a silent no-op, not "Not supported" [P].
6. **P1, 1.5d, internal/editor.** Implement cheap bound actions that are missing: `toggle_line_numbers`, `toggle_line_wrap` (state flags), `select_line`, `move_to_paragraph_up/down`.

### M2: File access: open, save-as, finder, open folder (about 16.5d)
Exit: Ctrl+O, Save As, Ctrl+' and Switch Project work; `gvim DIR` roots at DIR.

7. **P0, 1.5d, internal/editor (document.go) and internal/tui prompt.** Save As for untitled buffers. Today: "buffer has no file name" [P]. Legacy: "Save as:" prompt.
8. **P0, 3d, internal/tui (new browser.go).** Open File browser for Ctrl+O and File > Open File: directory navigation, Enter on dir enters and Enter on file opens, type-to-filter, `path:line:col`, `..`, `/` and `~` rows, Show Hidden (Alt+.), Name/Size/Modified sort [I, from legacy source and PTY-observed].
9. **P0, 1d, internal/explorer.** `ListDir(dir, opts)` returning Name, IsDir, IsSymlink, IsHidden, Size, ModTime. Shared by the browser and the tree. Also make `showHidden` and `showIgnored` tree options; both are hard-coded in `Rows()` and `scan()`.
10. **P0, 2d, internal/finder (new).** File enumeration: `git ls-files` fast path (external git binary, not a Go module), walk fallback honoring `.gitignore` via the explorer matcher, background goroutine with cancel, cap 50,000, "Loading files…" status [I, legacy `providers.rs:304, 772–810`].
11. **P0, 2d, internal/finder (score.go) reusing internal/palette/fuzzy.go.** Path scorer with Fresh's weights: consecutive 16, word boundary 32, start 48, camel 24, gap −3/−5, exact 100, basename exact 80, contiguous 64, basename prefix 64, segment prefix 32; +200 prefix-probe boost; frecency `min(20, count×recency/100)`; top 100 [I, from `input/fuzzy/mod.rs:47–78`, `providers.rs:946–962`].
12. **P0, 1.5d, internal/tui/palette_ui.go and internal/palette/palette.go:136–146, 176–178.** Remove the forced `>`. Modes: no prefix = file, `>` = command, `:` = line, `#` = buffers. `setPaletteQuery` always prepends `>` today [P].
13. **P0, 0.5d, internal/keymap.** Bind `quick_open_files` (ctrl+') and `quick_open_buffers` (ctrl+;) to finder modes. Whether terminals can send ctrl+' is unverified [I].
14. **P0, 2d, internal/explorer, internal/tui, and new internal/workspace.** Switch Project and Open Folder: folder-only browser; Enter on a dir selects it; Esc cancels; "not a directory" status. Re-root explorer, finder and open-buffer paths in place. Also `gvim DIR` (today it errors with `read <dir>: is a directory`, and `main.go:57` uses `os.Getwd`) [P]. Legacy has 11 e2e tests (`tests/e2e/open_folder.rs`) [I].
15. **P0, 2d, new internal/fsops.** `CreateFile`, `Mkdir`, `Rename`, `Move`, recursive `Copy` with conflict policy (Overwrite, Skip, Cancel), " copy" naming, `Trash` via `hymkor/trash-go`, validation (no separators, stays inside root). Backs Ctrl+N, Ctrl+Shift+N, F2, Delete, Ctrl+R.
16. **P0, 1d, internal/editor and internal/tui.** Rename or move updates `Document.Path`; delete closes the document after confirm.
17. **P0, 1d, internal/tui (explorer_ui.go).** Confirm prompts for delete and the Overwrite/Skip conflict dialog.

### M3: Syntax highlighting (about 7d)
Exit: highlighting for common languages, using the theme's `syntax_*` keys.

18. **P1, 4d, new internal/syntax on `github.com/alecthomas/chroma/v2` v2.27.0.** Lexer detection: `lexers.Match(filename)`, then `lexers.Analyse(content)`, then `lexers.Fallback`, mirroring `refs/crush/internal/ui/common/highlight.go` [I]. Fresh's order is filename, extension, shebang (`docs/features/languages.md`). Map chroma token types to theme keys in `internal/theme`: keyword, string, comment, function, type, variable, constant, operator.
19. **P1, 2d, internal/syntax and internal/tui/chrome.go.** Render per-visible-line token spans. Chroma lexers are stream-based, so cache the tokenized buffer and invalidate on edit [I, unverified]. Viewport-only lexing would need state checkpoints.
20. **P2, 1d, internal/theme.** Check fidelity against the 10 bundled themes, which are byte-identical to `refs/fresh` [P].

### M4: Editing, search, config (about 21d)
Exit: search, replace, auto-indent, line endings and a config layer exist.

21. **P0, 4d, new internal/search (stdlib `regexp`) plus internal/editor and the find bar.** Matches, next and previous (F3, Shift+F3), highlights (theme keys `search.match_bg` and `current_match_bg` exist), "Match N of M", and Alt+C/W/R wired to actions. The toggles are mouse-only today (`mouse_actions.go:56`).
22. **P0, 3d, internal/search and internal/editor.** Replace, replace all, query replace (y/n/!/q). Capture groups via `regexp.Expand`.
23. **P0, 1d, internal/editor.** Line-ending detect and preserve (LF, CRLF, CR). Today the label is hard-coded to LF (`model.go:353`), and a CRLF file saves as mixed endings [P].
24. **P0, 1.5d, internal/editor.** Auto-indent on Enter; Tab inserts spaces with `tab_size`; Shift+Tab dedent. Today Enter adds no indent and Tab inserts a literal `\t` [P].
25. **P0, 2.5d, internal/editor.** `toggle_comment` (Ctrl+/), indent and dedent selection, `duplicate_line`, `delete_line`.
26. **P0, 1.5d, internal/editor.** Case conversion (Alt+U/L), `sort_lines`, trim trailing whitespace, ensure final newline on save.
27. **P0, 3d, new internal/config.** Config loader. JSONC via `github.com/tailscale/hujson` (see §3). User layer plus project `.gvim/config.json` searched upward. Fresh's paths are in `configuration/index.md:37–47`.
28. **P0, 1d, internal/keymap.** Keymap `inherits` (parsed at `keymap.go:69`, never used) and user `keybindings` overrides. Depends on item 27.
29. **P1, 2d, internal/tui statusbar.** Click pickers for encoding, line ending and language. Today a verified no-op [P]. Legacy opens prompts (`app/chrome/status_bar.rs:23–35`).
30. **P1, 1.5d, internal/tui.** Tab "+" popup (New Terminal / New File) and the tab right-click menu (8 items, `context_menu.rs:98–116`). Port creates a buffer directly and ignores right-click [P].

### M5: Explorer completion (about 8.5d, plus 3d P2)
Exit: explorer behaves like Fresh for navigation, selection and file ops.

31. **P1, 1d, internal/tui (model.go).** Right expands, Left collapses. Today both call `activateExplorer` (`model.go:165`) [P].
32. **P1, 1d, internal/tui and internal/explorer.** Up/Down auto-preview the file as a preview tab; PageUp/PageDown handled.
33. **P1, 1.5d, internal/explorer.** Type-to-search, fuzzy filter, Esc clears, next and previous match.
34. **P1, 2d, internal/explorer and internal/fsops.** Multi-select (Space, Shift+Up/Down, Ctrl+A); cut dims items; Esc cancels cut; copy/cut/paste via fsops.
35. **P1, 1d, internal/explorer.** Show Hidden (persisted) and Show Gitignored (Ctrl+I) toggles, using the item 9 options.
36. **P1, 1d, internal/explorer and new dependency fsnotify.** Change detection and `.gitignore` reload on change. Legacy polls every 3s (`app/file_operations.rs:1021`) [I]. Ctrl+R refresh.
37. **P1, 1d, internal/tui.** Explorer dropdown and right-click context menu (10 items, `context_menu.rs:307–318`) [I].
38. **P2, 3d, internal/explorer.** Sidebar width from config, left or right side, `follow_active_buffer`, `auto_open_on_last_buffer_close`, compact dirs, sticky parent rows, duplicate, copy path, drag-drop move, per-row git status.

### M6: Multi-cursor, selection modes, bookmarks, macros (about 26.5d, P1)
Exit: Ctrl+D and block selection work. M16 (multi-cursor mouse) passes.

39. **P1, 9d, internal/editor (Document).** `Document` holds `[]Cursor`. `add_cursor_next_match` (Ctrl+D), `add_cursor_above/below` (Ctrl+Alt+Up/Down), `add_cursors_to_line_ends`. Currently one `Cursor` and one `Sel` [P].
40. **P1, 3d, internal/editor and internal/tui.** Block selection (Alt+Shift+arrows) and mark mode (`set_mark`, `cancel_mark`, `clear_mark`).
41. **P1, 4d, internal/editor.** Bookmarks (Ctrl+Shift+0–9 set, Alt+0–9 jump) and macros (F4/F5 record and replay through the `Do` history).
42. **P1, 2.5d, internal/editor.** Auto-close brackets and quotes; surround selection; `goto_matching_bracket`.
43. **P1, 3d, internal/tui render.** Rulers, indent guides, whitespace master toggle, current-line highlight, occurrence highlight.
44. **P1, 2d, internal/editor and internal/config.** Auto-save interval; read-only mode (auto-detect plus toggle).
45. **P2, 3d, internal/editor.** Indent-based folding.

### M7: Plugin runtime, first slice (about 6d; full API later)
Exit: one JS plugin can register a command, read the buffer and react to a save.

46. **P1, 3d, new internal/plugin on `github.com/liustack/slowjs` v0.29.10.** Runtime: load plugins from the config dir, `RegisterHostFunc`, one goroutine owns the VM, an `ExecutePendingJobs` loop, panic and timeout guard (interrupt untested). See §4.
47. **P1, 3d, internal/plugin and an internal/editor API layer.** First slice, about 20 of 358 method signatures: `getActiveBufferPath`, `getBufferText`, `getCursor`, `setCursor`, `insertText`, `deleteRange`, `getSelection`, `openFile`, `setStatus`, `registerCommand` (into the palette), `on` for `buffer_saved`, `cursor_moved` and `buffer_closed`, `readConfig`, `log`. No async, timers or virtual buffers.
48. **P2, 15–25d, internal/plugin.** Remaining API: Promise-returning methods (Fresh's callbackId pattern), `setInterval`/`delay`, popups, virtual buffers, splits, prompts, process spawn, LSP bridge. Estimate from the editing survey [P].
49. **P2, 1d, internal/plugin with `github.com/evanw/esbuild` v0.28.2.** TypeScript transpile, only if TS plugins are needed. Smoke test showed type annotations stripped [P].

### M8: LSP (about 12d, estimate)
Exit: diagnostics, goto definition and hover work for one configured server.

50. **P2, 3d, new internal/lsp on `go.lsp.dev/protocol` v1.0.1 and `go.lsp.dev/jsonrpc2` v1.0.1.** Client lifecycle: initialize, didOpen, didChange, didClose, shutdown. Per-language server config. Restart, Stop and Toggle-for-buffer (LSP menu items).
51. **P2, 3d, internal/lsp and internal/editor.** Diagnostics: inline underline, status counts ("[⚠ 2]" in legacy, not in port [P]), diagnostics panel.
52. **P2, 2d, internal/lsp and internal/tui.** Goto definition (F12), references (Shift+F12), hover popup (Alt+K).
53. **P2, 3d, internal/lsp and internal/tui.** Completion popup (Ctrl+Space), signature help, rename (F2), code actions (Alt+.).
54. **P2, 1d, internal/lsp.** Inlay-hint toggle and LSP status screen.

### M9: Terminal panes (about 11d, estimate)
Exit: a shell runs in a bottom pane, with keyboard capture.

55. **P2, 4d, new internal/term on `github.com/charmbracelet/x/vt` v0.1.0 and `github.com/aymanbagabas/go-pty` v0.2.3.** Spawn PTY, read loop into the emulator, render into a pane. `creack/pty` is the Unix fallback.
56. **P2, 3d, internal/tui.** Pane layout: open, close, restart, split, utility dock; keyboard-capture toggle (`docs/features/terminal.md:67–74`).
57. **P2, 2d, internal/term and internal/tui.** Send selection to terminal; View > Terminal menu (5 items).
58. **P2, 2d, internal/term.** Windows ConPTY validation (`go-pty` claims it; unverified).

### M10: Settings, session, polish (about 20d, P2)
59. **P2, 5d, internal/tui.** Settings screen on the config layers from item 27.
60. **P2, 3d, new internal/session.** Hot exit and session restore (`docs/features/session-persistence.md:59–69`); per-root session save on switch.
61. **P2, 2d, internal/theme.** Theme and locale pickers (`select_theme`, `select_locale`); `theme` key with `builtin://`, `file://`, and paths (`docs/features/themes.md:6–10`). `main.go:49` hard-codes "dark".
62. **P2, 2d, internal/tui.** Dashboard and welcome screen (`docs/features/dashboard.md`).
63. **P2, 1d, internal/tui.** Workspace-trust gating (`docs/features/workspace-trust.md:5–17`). Port hard-codes " Trusted " at `layout_panels.go:51`.
64. **P2, 3d, new internal/gitx.** Git status for explorer rows and the Git surface (`docs/features/git.md:14–46`).
65. **P2, 2d, internal/tui.** Warnings indicator and messages log.
66. **P2, 1d, internal/keymap and internal/tui.** Keybinding editor; Keybinding Style submenu (Emacs, VSCode, macOS GUI). The presets are present as JSON (104, 4, 64 and 54 bindings) [P].
67. **P2, 1d, internal/tui.** Calibrate keyboard and event debug (View and Help menus).
68. **P2, 3d, internal/tui and internal/buffer.** Encoding beyond UTF-8 via `golang.org/x/text` (editing survey says pure Go; not built here). Non-UTF-8 files are refused today (`document.go:102`) [P].

## 3. Dependency decisions

Purity matrix [R]: each candidate was built in its own scratch module under `/tmp/gapv3-purity/<name>` with `CGO_ENABLED=0 go build ./...` for GOOS darwin, linux and windows on GOARCH arm64 (host). Also counted: non-std packages with CgoFiles under default CGO (`nonstd_cgo`), `runtime/cgo` in the CGO-disabled graph, and non-std deps. Script: `/tmp/gapv3-scripts/purity.sh`.

| Module@version (scratch build) | darwin | linux | windows | nonstd_cgo | runtime/cgo | nonstd deps |
|---|---|---|---|---|---|---|
| alecthomas/chroma/v2 v2.27.0 | OK | OK | OK | 0 | 0 | 6 |
| go.lsp.dev/protocol v1.0.1 | OK | OK | OK | 0 | 0 | 6 |
| go.lsp.dev/jsonrpc2 v1.0.1 | OK | OK | OK | 0 | 0 | 4 |
| liustack/slowjs v0.29.10 | OK | OK | OK (amd64 only) | 0 | 0 | 7 |
| dop251/goja v0.0.0-20261103214539-f5d1d0025744 | OK | OK | OK | 0 | 0 | 26 |
| yuin/gopher-lua v1.1.2 | OK | OK | OK | 0 | 0 | 5 |
| evanw/esbuild v0.28.2 | OK | OK | OK | 0 | 0 | 28 |
| charmbracelet/x/vt v0.1.0 | OK | OK | OK | 0 | 0 | 21 |
| aymanbagabas/go-pty v0.2.3 | OK | OK | OK | 0 | 0 | 12 |
| creack/pty v1.1.24 | OK | OK | OK (stub `pty_unsupported.go`; no PTY) | 0 | 0 | 2 |
| fsnotify/fsnotify v1.10.1 | OK | OK | OK | 0 | 0 | 4 |
| hymkor/trash-go v0.3.0 | OK | OK | OK | 0 | 0 | 4 |
| tailscale/hujson v0.0.0-20260727124030-b80ff77dac4f | OK | OK | OK | 0 | 0 | 2 |
| sourcegraph/jsonrpc2 v0.2.3 | OK | OK | OK | 0 | 0 | 2 |

slowjs per-arch [R]: OK on darwin amd64 and arm64, linux amd64 and arm64, windows amd64. FAIL on windows/arm64 with `internal/unsupported`, as the survey predicted.

Discrepancy [R vs P]: the platform survey gave chroma "10 non-std deps"; my `go list -deps` count is 6. Other counts agree. Method difference suspected (survey may have counted a wider graph); not resolved.

A passing `CGO_ENABLED=0` build does not prove that a module has no C in its source. slowjs ships C-derived engine code as Go assembly (see §5).

Decisions:

- **Syntax highlighting: `github.com/alecthomas/chroma/v2` v2.27.0.** Pure and verified. Same version as crush, which is the reference the owner named. Smoke-tested for 297 lexers [P]. Theme keys already exist.
  - Rejected: tree-sitter bindings (cgo, excluded by the constraint). `odvcencio/gotreesitter` v0.56.0 (pure, but 36 MB binary and 206 embedded grammars; LATER for parse-accurate highlighting). `frostybee/nuri` v1.0.1 (pure via wazero and an `onig.wasm`; closest to Fresh's TextMate engine family, but 0 stars and created 2026-06; LATER). `eugenioenko/textmate-go` v0.2.2 (low-level, no grammars bundled; LATER). `go-enry` v2.9.6 (detection only). `sourcegraph/syntaxhighlight` (stale since 2023).
- **LSP: `go.lsp.dev/protocol` v1.0.1 and `go.lsp.dev/jsonrpc2` v1.0.1.** Pure and verified. Alternative transport: `sourcegraph/jsonrpc2` v0.2.3 (pure, verified, 2 deps). Defer to M8.
  - Rejected: `tliron/glsp` v0.2.2 (a server framework, not a client).
- **Plugin runtime: `github.com/liustack/slowjs` v0.29.10.** Owner-named. Passes the purity matrix above. Detailed in §4.
  - Fallback: `dop251/goja` at the pinned pseudo-version above, which is pure Go from scratch and has a drop-in Promise and host-function model [P].
  - Rejected: `yuin/gopher-lua` v1.1.2 (Lua 5.1, no Promises; kept as the owner's named alternative). `modernc.org/quickjs` v0.27.0 (no upstream repo found). `fastschema/qjs` (QuickJS as embedded WASM, not Go-native). `grafana/sobek` (goja fork; LATER). `d5/tengo` (its own language; rejected).
- **Terminal emulator: `github.com/charmbracelet/x/vt` v0.1.0.** Pure and verified. Smoke test: cursor position after colored output was correct [P]. Young (two versions); pin it.
  - Rejected: `hinshun/vt10x` (stale since 2023), `ActiveState/vt10x` v1.3.1 (last release 2020). Fresh's `alacritty_terminal` and `portable-pty` are Rust and not C-free-verified [I].
- **PTY: `github.com/aymanbagabas/go-pty` v0.2.3** for cross-platform spawn. Fallback: `github.com/creack/pty` v1.1.24 on Unix only (no PTY on Windows, as the stub shows).
- **File watching: `github.com/fsnotify/fsnotify` v1.10.1.** Pure and verified.
  - Rejected for default use: 3-second mtime polling (the legacy approach). Kept as fallback only [I].
- **Trash: `github.com/hymkor/trash-go` v0.3.0.** Pure and verified.
  - Rejected: shelling out to per-OS trash tools (platform-specific, not a module) [I].
- **Config (JSONC): `github.com/tailscale/hujson` v0.0.0-20260727124030-b80ff77dac4f.** Pure and verified. Its API for stripping comments and trailing commas is not checked here. Test before use.
- **Fuzzy matching: no new module.** Reuse `internal/palette/fuzzy.go` with Fresh's weights (item 11).
  - Rejected: `github.com/sahilm/fuzzy` v0.1.3 (builds per the explorer survey, not re-run here). Its scoring cannot express Fresh's bonus weights without a wrapper that is longer than the scorer itself [I].
- **Regex: stdlib `regexp`.** RE2, no dependency. Fresh's Rust `regex` also lacks lookaround, so semantics roughly match [I].
- **Directory picker and file browser: in-house on `internal/explorer.ListDir`.** No module chosen. Not evaluated: `charmbracelet/bubbles` filepicker.
- **Clipboard: no module chosen; not surveyed.** Candidates not verified: OSC 52 terminal escape (no dependency, terminal-dependent), or a pure-Go clipboard module (none checked). Start with OSC 52 plus an in-process register, and revisit at M4 if needed.
- **Markdown rendering: none needed in M1–M10.** Fresh's markdown support is a plugin or compose mode. Not evaluated: `charmbracelet/glamour`.
- **Non-UTF-8 encoding: `golang.org/x/text`.** Pure per the editing survey [I]. Not built in this run.

## 4. Plugin language decision

**Pick: JavaScript on `github.com/liustack/slowjs` v0.29.10.** Fallback: `github.com/dop251/goja`, behind the same host-function interface. Lua (`gopher-lua`) is not chosen for M7.

Reasons:
- The owner named slowjs as the JavaScript option, and it passes the CGO_ENABLED=0 matrix on the platforms that matter (darwin arm64 is the dev host; linux amd64/arm64; windows amd64).
- Smoke tests [P]: `RegisterHostFunc` round trip worked, `Eval` returned 42, and a Promise job ran after `ExecutePendingJobs`.
- Fresh's plugins are TypeScript on QuickJS, so JS keeps the rewrite cost of porting its bundled plugin logic lowest. Lua would require rewriting every plugin.

Swap criteria, checked at the M7 gate. Move to goja if any fails:
- Builds on all target OS and arch pairs above.
- Promise and host-function round trip, and the 20-function slice tests pass.
- Interrupt or timeout works (untested).
- No panic from a plugin cannot be contained.

Owner decision needed: slowjs's engine is C-derived QuickJS compiled to checked-in Go assembly. 52 `.s` files are in the module, and 130 `.c` files are present but not compiled [P]. Supported platforms: linux and darwin on amd64 and arm64, plus windows amd64. slowjs passes the owner's stated "pure Go, no cgo" test, and the owner named it, but its C lineage may not meet a stricter "C-free" reading. goja is pure Go from scratch and is the reversal if the owner wants a stricter reading.

First milestone coverage: about 20 host functions out of Fresh's 358 `EditorAPI` method signatures (about 6%). Listed in backlog item 47. Excluded: Promise-returning methods, timers, popups, virtual buffers, splits, prompts, terminal, LSP, explorer, and process spawn. Fresh's own backend implements 75 of 122 methods (61%) [P], so the first slice is much smaller than the complete API.

## 5. Risks and what was not verified

- **Not re-run here:** every count in §1 except the mouse-report figure, which was not re-run either. Those numbers come from the four survey reports' PTY runs and probes. The only things I ran are the purity matrix and `go list -m all`.
- **Survey conflicts:** Ctrl+G (see §1); the Ctrl+L shortcut for Go to Line in the menus table (unverified); chroma's dependency count (6 vs 10).
- **Not verified:** whether disabled menu items are greyed (text captures cannot show it); the Terminal and Keybinding Style menu shortcuts (not captured); Ctrl+' reachability in common terminals.
- **Highlighting:** chroma's lexer set has not been compared with Fresh's 75 syntect grammars, so some languages may be missing or differ. Whole-buffer lexing versus viewport rendering is an inference (item 19). The 75 grammar count is from `find` and was not recounted.
- **slowjs:** 17 stars and about 3 weeks old at survey time. C-derived engine. Interrupt and ES-module support untested. Its windows/arm64 build fails.
- **charmbracelet/x/vt:** v0.1.0 with two versions published, so API churn is likely.
- **go-pty on Windows:** ConPTY support claimed, not verified.
- **Clipboard, directory picker, markdown rendering, and encoding (`x/text`):** not evaluated or built.
- **Effort numbers:** M1–M6 come from the editing survey's person-day table [P, unverified] and my estimates. M7–M10 are my estimates only.
- **Module purity:** a passing CGO_ENABLED=0 build does not prove the absence of C in source (see §4).
- **Repo:** I did not run the Go test suite or the PTY parity suites for this document. `go list -m all` in the repo resolves; its first line is `github.com/tbxark/gvim`.

## Milestones

- M1: Honest menu bar and key routing (about 7d)
- M2: File access: open, save-as, finder, open folder (about 16.5d)
- M3: Syntax highlighting with chroma (about 7d)
- M4: Editing, search, config (about 21d)
- M5: Explorer completion (about 8.5d, plus 3d P2)
- M6: Multi-cursor, block selection, bookmarks, macros (about 26.5d)
- M7: Plugin runtime with slowjs, first slice (about 6d; full API 15–25d later)
- M8: LSP (about 12d)
- M9: Terminal panes (about 11d)
- M10: Settings, session, polish (about 20d)

Verification output:
```
go list -m all 2>/dev/null | head -1
github.com/tbxark/gvim
```