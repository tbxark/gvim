# ROUND-PLAN: finishing the remaining features

> Historical record: package paths in this file are pre-reorganization. See
> `docs/architecture.md` for the current layout.

> **Superseded (plugin runtime).** Q2 and the T2 track (goja runtime, esbuild transpile) describe the old plan. The plugin runtime in this repository is **Lua on
> `github.com/yuin/gopher-lua`** (`internal/plugin`), not JavaScript. The JS system (`goja`, `esbuild`,
> `slowjs`) was removed completely, and `.js`/`.ts` files in the plugin folder are never executed.
> The design record is the Lua plugin design (`docs/plugins/` for users and plugin authors,
> `migration/ARCH-v4.md` for the package graph). Everything below that talks about a JavaScript or
> TypeScript runtime, `RegisterHostFunc`, Promises, `slowjs`, `goja` or `esbuild` is history.

Status: in progress. Each phase is one workflow: parallel tracks in their own worktrees, one
integrator agent that applies the patches to the main tree and runs the gates, then parallel
read-only reviewers and a verifier. Tracks never edit the main tree. Each track exports
`git diff --cached <snapshot> --binary` to /tmp/gvim-patches-<phase>/<track>.patch.

## Defaults chosen where the blueprint had an open question

- Q1 terminal: in scope (PTY pane and dock).
- Q2 plugin runtime: Lua on github.com/yuin/gopher-lua (decided later; the goja and esbuild choice made here was replaced, and the JS system is removed). See docs/plugins/.
- Q3 macOS quick-open keys: keep Fresh's macos.json behaviour.
- Q4 config path: keep the current path (os.UserConfigDir()/gvim). Changing it is a separate decision.
- macOS trash: keep the freedesktop home trash (owner decision still open).
- macOS Ctrl+B in the explorer: keep Fresh's macos.json (focus the editor).

## Phases

P1 domain packages (new packages, no root wiring)
- T1 internal/term: PTY session (creack/pty on unix, ErrUnsupported on Windows) with a vt screen (charmbracelet/x/vt).
- T2 internal/plugin: goja runtime, esbuild TypeScript transpile, host API port, command registry, timeouts.
- T3 internal/lsp: session manager per language, requests (definition, references, hover, completion, rename, code action, implementation), diagnostics; fake-server tests.
- T4 internal/gitx and internal/session: go-git status and branch; session save/load with unsaved-content stash.
- T5 internal/encoding, internal/fold, internal/display, internal/diff: pure helpers with tests.

P2 editor core (internal/editor, disjoint files)
- E1 multi-cursor: Document holds []Cursor; add_cursor_*, block_select_*, remove_secondary_cursors.
- E2 marks and navigation: bookmarks, marks, navigate back and forward, goto_matching_bracket, dabbrev_expand, find_selection_next/previous, close, save_all, shell_command(_replace).
- E3 document state: encoding wiring on open and save, read-only flag, folds (toggle_fold), autosave due check.

P3 UI root and rendering
- U1 splits: split_horizontal/vertical, next/prev/close split, layout and hit map per pane.
- U2 popups and overlays: hover, completion, references, code action, rename popups; right-click menus; tab popup and drag; help, keyboard shortcuts, tour, settings, keybinding editor.
- U3 display: multi-cursor, rulers, indent guides, whitespace, current line, occurrence highlight, fold gutter, line wrap, diagnostics underline, horizontal scrollbar, status bar pickers (encoding, EOL, language).
- U4 root features: watcher wiring, open folder and switch project, session restore and save, git markers, macro recording and playback, input calibration, event debug, remote indicator menu, dock panel.

P4 app wiring: LSP per language and diagnostics to the UI, plugin host and palette commands, terminal dock, session on exit.

P5 gap sweep: probe palette and menu commands, implement or mark not applicable.

## Status

- P1 domain packages: done and integrated (term, plugin, lsp requests, gitx and session, encoding, fold, display, diff). Review fixes are in P2.
- P2 editor core: done and integrated (multi-cursor, marks, navigation, shell, encodings, read-only, folds, autosave API). Review fixes are in P3.
- P3 UI: integrated (splits and compare, overlays and settings, rendering, root features, shell split). Review findings are in P3b.
- P3b fixes: done and integrated.
- P4 app wiring: done and integrated (LSP, plugin host, terminal dock). P4b review fixes: done and integrated.
- P5 gap sweep: done and integrated (view toggles, editor text actions, splits, root features, terminal variants, palette gate). P5b review fixes: done and integrated.
- Final round: done and integrated (formatter lookup, copy path, async revert, auto-revert by file).

## Known open items after the final round

- A revert in flight is not covered by the write gate: a save planned between PlanRevert and ApplyRevert can write the discarded text.
- Auto-revert echo: the watcher's echo of the editor's own save can replay a clean revert and overwrite the Saved status.
- Watcher overflow names no files, so auto-revert misses flagged files in an overflowed burst.
- LSP goto and file open still read the disk on the UI goroutine (editor OpenFile).
- Plugin commands run synchronously in Update (bounded by plugin_timeout_ms).
- Terminal: Windows has no ConPTY; right-column dock placement is not built.
- Menu and palette commands listed in unhandledPaletteActions in dead_bindings_test.go are the remaining explicit gaps.
