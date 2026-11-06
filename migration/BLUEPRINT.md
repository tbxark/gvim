# BLUEPRINT: gvim as a Go port of Fresh

Status: working blueprint, 2026-11-05. Written after a static audit of the Go tree at
HEAD `57311c1` plus the uncommitted changes listed in §6.0. Scope is the Fresh terminal editor
at `refs/fresh` (read-only reference). Where this file and `ARCH-v4.md` disagree on package
structure, ARCH-v4 wins. Where it disagrees with `GAP-v3.md` on status, the audit tables in §1.3
win, because GAP-v3 predates most of the current code.

Evidence labels: **[V]** verified by a command run in this pass. **[A]** taken from a read-only
audit report and not re-run. **[P]** proposal. **[U]** unverified.

## 1. Codebase audit, legacy census, latent bugs

### 1.1 Legacy census (Fresh, Rust) [V]

Non-test `.rs` lines per crate, counted with `find | xargs cat | wc -l`, excluding `target/` and
any `tests/` directory. Inline `#[cfg(test)]` modules are still counted, so these are upper bounds.

| Crate | Lines | Files | Role for gvim |
|---|---:|---:|---|
| `fresh-editor` | 348,451 | 1,119 | Application, input, render, prompts, providers, file explorer |
| `fresh-editor-core` | 96,298 | 106 | `config.rs` (menus), `keymaps/*.json`, `themes/*.json` |
| `fresh-plugin-runtime` | 20,956 | 8 | Plugin runtime (out of scope until R10) |
| `fresh-ui` | 19,882 | 59 | Shell chrome, menus, dialogs |
| `fresh-core` | 15,160 | 15 | `action.rs` (contexts, action enum) |
| `fresh-update` | 6,909 | 22 | Self-update (not ported) |
| `fresh-input-parser` | 5,504 | 11 | Terminal input (covered by bubbletea) |
| `fresh-js`, `fresh-parser-js`, `fresh-js-macros` | 5,503 | 19 | Plugin JS (R10) |
| `fresh-gui`, `fresh-winterm`, `fresh-languages`, `fresh-i18n` | 4,090 | 20 | GUI (not ported), Windows console, languages, i18n |

The full Rust side is roughly 500k non-test lines. A 1:1 port is a multi-month program, so this
blueprint is phased, and each phase must leave the tree green.

### 1.2 Go census [V]

- `internal/`: 18 packages. Non-test `.go` lines: 21,003. Total `.go` files, tests included: 209.
- Gates green at HEAD and after this pass: `go build ./...`, `CGO_ENABLED=0 go build ./...`,
  `go vet ./...`, `go test ./...`, `golangci-lint run ./...` (0 issues), `purity_check.sh`
  (PURE_GO_OK), `import_rules_check.sh` (IMPORT_RULES_OK).
- Go toolchain: `go1.27.3 darwin/arm64`. `go.mod` declares `go 1.27.3`. The latest stable release
  on `https://go.dev/dl/?mode=json` is `go1.27.3` [V, checked 2026-11-05].

### 1.3 Feature status against GAP-v3 (68 items) [A]

Static reads by three audit passes at HEAD. Statuses: DONE = implemented and wired to a key, menu
or UI path, with a test. PARTIAL = exists but incomplete or not wired. MISSING = no code path.
WRONG = behaves differently from the claim it makes.

| Milestone | Items | DONE | PARTIAL | MISSING | WRONG |
|---|---:|---:|---:|---:|---:|
| M1–M4 (menus, file access, syntax, editing, search, config) | 1–30 | 15 | 8 | 7 | 0 |
| M5–M10 (explorer, multi-cursor, bookmarks, folding, plugins, LSP, terminal, settings, session, git, encoding) | 31–68 | 1 | 10 | 26 | 1 |
| **Total** | **68** | **16** | **18** | **33** | **1** |

Before this pass, item 14 (open folder) and item 34 (multi-select file ops) had no working path.
The M1–M4 audit marked items 15 (`fsops`) and 16 (rename follows the open tab) DONE, but the
file-ops port was never connected, so neither was reachable from the UI. The M5–M10 audit caught
this, and §6.0 change 2 fixes it. The DONE count for M1–M4 above therefore overstates what was
reachable before this pass. Notes on the statuses that changed or matter most:

- **WRONG, item 63.** The status bar always shows " Trusted " (`internal/ui/layout.go:250`). No trust
  state exists. Fixed in §6.0 change 3.
- **Dead bindings.** Many keys in `internal/keymap/default.json` and many menu rows resolve to an
  action with no handler. `editor.Do` returns `ErrUnknownAction` (`internal/editor/editor.go:268-274`),
  and the UI shows "Not supported". Examples: `toggle_line_numbers`, `toggle_line_wrap`,
  `select_line`, `move_to_paragraph_*`, `block_select_*`, `add_cursor_*`, `set_bookmark`,
  `play_last_macro`, `goto_matching_bracket`, `file_explorer_search_*`, `lsp_*`, `completion_*`,
  `open_settings`, `select_theme`, `toggle_read_only`, `toggle_fold`, `toggle_keyboard_capture`.
- **Config is not applied.** `internal/app/run.go` never loads the layered config. `app.LoadConfig`
  is called only by the `gvim config show` command (`internal/cli/config.go:24`). Theme, keymap
  overrides, `explorer_show_hidden`, `tab_size`, `auto_indent`, line ending and the save-time
  options have no runtime effect.
- **Line endings.** The status bar and layout hard-code "LF" (`internal/ui/model.go:1450`,
  `internal/ui/layout.go:67`). No CR or CRLF handling exists in `editor`, `buffer` or `app`
  [A]. Whether a CRLF file is rewritten as LF on save is **[U]** and must be tested first.
- **Unwired packages.** `internal/lsp` and `internal/explorer/watch` have no importer outside their
  own tests [A]. The `statusbar` package and `footer.go` have no callers [A].
- **Menu accelerators.** Alt+letter menu opening is not wired. `default.json` binds seven
  `menu_open` keys, and no code handles them [A].
- **Quick-open keys on macOS.** `ctrl+'` and `ctrl+;` resolve to `jump_to_next_error` and
  `jump_to_previous_error` under the macOS preset, which matches `refs/fresh`'s own
  `macos.json` [A]. So the quick-open shortcuts are reachable only from the palette on macOS.
  See open question Q3.

### 1.4 Legacy inconsistencies to preserve or decide [A]

These are places where Fresh's docs and code disagree. The port follows the code.

1. Ctrl+P: docs say file finder, `app/prompt_lifecycle.rs:226-227` opens command mode. The port
   follows the code, so Ctrl+P is `>` (command) mode, and file mode is an explicit mode.
2. Ctrl+O: keymap and code say Open File. Docs say Quick Open. The port follows the code.
3. Go to line: on the default preset, Ctrl+G is `goto_line`. On the macOS preset, Ctrl+L is
   `goto_line`, and Ctrl+G is `find_next`, as in `refs/fresh`'s `macos.json`. The port follows the
   preset.
4. Esc on an idle buffer: bound to `remove_secondary_cursors`, which must be a silent no-op. The old
   port printed "Not supported" for it. Now `handlers.go:162-164` returns nil.
5. Fresh's default keymap lists the BackTab alias of `dedent_selection` three times
   (`crates/fresh-editor-core/keymaps/default.json:700-721`). The port keeps all three entries.

### 1.5 Latent defects found in the Go port (this pass) [A]

- `internal/ui/layout.go:250`: hard-coded " Trusted " label (see item 63).
- `internal/keymap/keymap.go`, `canonicalContext`: an unknown `when` string was kept as a custom
  context, so a typo in a user keymap created a binding that could never fire. Fresh's
  `KeyContext::from_when_clause` returns `None` for unknown input
  (`refs/fresh/crates/fresh-core/src/action.rs:34`). Fixed in §6.0 change 3: unknown contexts and
  `mode:` clauses are now load errors.
- Unused config fields: `line_ending`, `tab_size`, `auto_indent`, `trim_trailing_whitespace`,
  `ensure_final_newline` (`internal/config/config.go:33-53`) have no runtime consumer. Setting them
  does nothing, and the config tests do not catch that.
- `internal/explorer/listdir.go:40-42` says the tree uses `ListDir`. It does not: the tree scan still
  calls `os.ReadDir` (`internal/explorer/explorer.go:404`) [A].

### 1.6 Not done in this pass

- No audit of the Rust code for undefined behavior or logic bugs. Rust's memory safety removes most
  of that class, so the useful review is of logic against `refs/fresh` docs and tests. Not started.
- No telemetry ledger (`migration_telemetry.json`), no infographic, no legacy build, no A/B golden
  capture. Reasons are in §4.3 and §5.

## 2. Target Go architecture and module graph

### 2.1 Module and toolchain [V]

- Module: `github.com/tbxark/gvim`, `go 1.27.3`. No `toolchain` line.
- Direct dependencies in `go.mod` [V]: `charm.land/bubbles/v2` v2.2.1, `charm.land/bubbletea/v2`
  v2.1.1, `charm.land/lipgloss/v2` v2.0.6, `github.com/alecthomas/chroma/v2` v2.27.0,
  `github.com/charmbracelet/x/ansi` v0.11.9, `github.com/charmbracelet/x/vt` v0.1.0 (used by
  `internal/uitest`), `github.com/fsnotify/fsnotify` v1.10.1, `github.com/go-git/go-git/v5` v5.19.3
  (gitignore matching only), `github.com/hymkor/trash-go` v0.3.0, `github.com/spf13/cobra` v1.10.2,
  `github.com/tailscale/hujson` (JSONC), `go.lsp.dev/{jsonrpc2,protocol,uri}` v1.0.1,
  `golang.org/x/image` v0.47.0.
- `github.com/charmbracelet/ultraviolet` is a direct requirement at `go.mod:10` [V]. Its only importer
  is `internal/uitest/session.go` [V]. ARCH-v4 decision 12 keeps the custom grid, so the production
  binary does not use it. Confirm it is still needed, or drop it. The ARCH-v4 §1.12 line cite
  (`refs/crush/go.mod:16`) is also out of date: the crush audit puts ultraviolet at line 25.

### 2.2 Package layout

ARCH-v4 §2 is the authority. The packages present now [V]: `cli`, `app`, `ui` (with `browser`,
`dialog`, `editorpane`, `explorer`, `find`, `grid`, `menubar`, `palette`, `prompt`, `statusbar`,
`style`), `buffer`, `editor`, `search`, `fsops`, `workspace`, `finder`, `fuzzy`, `palette`, `keymap`,
`theme`, `syntax`, `explorer` (with `watch`), `config`, `lsp`, `uitest`.

Import rules are enforced by `migration/tools/import_rules_check.sh` [V]. The rules that matter most:
domain packages never import Bubble Tea, bubbles or lipgloss; `ui` depends on interfaces it defines
in `ports.go`; `app` is the only package that imports concrete adapters.

### 2.3 Mapping from Fresh sources to gvim [V]

| Fresh source (Rust) | gvim target (Go) | Transformation |
|---|---|---|
| `crates/fresh-editor-core/keymaps/default.json` (276 entries) | `internal/keymap/default.json` | Copied data; loader and conflict gate in `keymap.go`. `macos.json`, `emacs.json`, `vscode.json`, `macos-gui.json` follow the same pattern |
| `crates/fresh-editor-core/themes/*.json` (10 files) | `internal/theme/data/*.json` | Byte-identical copies [A, `cmp`] |
| `crates/fresh-editor-core/src/config.rs` `translated_menus()` | `internal/ui/menubar/menu_table_gen.go` | Generated by `migration/tools/gen_menus.py` [V]. Not edited by hand |
| `crates/fresh-editor/src/input/fuzzy/mod.rs` | `internal/fuzzy/fuzzy.go` | Scorer weights copied from Fresh's constants [A] |
| `crates/fresh-editor/src/input/quick_open/providers.rs` | `internal/finder` | Enumeration: `git ls-files` first, walk fallback, cap 50,000 [A] |
| `crates/fresh-core/src/action.rs` (`KeyContext`, `Action`) | `internal/keymap` (`Context`), `internal/editor` (action names) | Enum to string names; contexts validated at load (§1.5) |
| Command list (palette) | `internal/palette/commands_gen.go` | Generated by `internal/palette/gen/generate.py refs/fresh` [V] |
| `crates/fresh-ui` shell and `crates/fresh-editor` render | `internal/ui` (layout, hit map, overlays) | Re-implemented in Bubble Tea v2, not transliterated |

### 2.4 Layout contract (UI parity target) [A]

The UI is not reproduced 1:1, but the regions and their order should match. Fresh's frame layout is
in `refs/fresh/crates/fresh-editor/src/view/shell/frame.rs` (`frame_tree` at lines 518–615, [A]). The
comparison, from the layout audit:

| Region | Fresh | gvim now | Target |
|---|---|---|---|
| Menu bar | Row 0, toggled by `show_menu_bar` (default on) | Row 0, always on | Add toggle (R5) |
| Tab strip | One row per split pane, global toggle | One row over the editor only | Keep global; splits deferred (R9) |
| Explorer | Left or right, default 30%, min 5 cols | Left only, 32 cols, clamped 16–60 | Config width and side (R4) |
| Status bar | One row above prompt; hidden while suggestions or file browser are up | Always on | Match hide rules (R5) |
| Palette (Ctrl+P, `>`) | List above prompt row, replaces status row | Bottom box covering status row | Match Fresh (R5) |
| Find/replace | Options row and prompt row above status row | Overlay rows at the bottom | Keep |
| Goto | Prompt row | Prompt row | Keep |
| Open file / Save As | File browser above prompt row, full width | Centred box, max 72 cols | Keep centred box; flagged as a UI difference |
| Confirm | Centred card, dim scrim, Esc = rightmost (safe) button | Yes/No on the bottom row | Keep the bottom row; match Fresh's Esc (safe choice) |
| Popups, hover, right-click menus | Present | Absent | R5 |
| Splits, horizontal scrollbar, tab drag, dock | Present | Absent | R9 or later |

Key differences that are in scope for "same shortcuts as Fresh" (the user's requirement 2):
- Explorer Ctrl+B hides the panel in Fresh. gvim only moves focus. Change to hide (R4).
- Explorer Esc: Fresh clears cut, then multi-select, then search, else focuses the editor. gvim
  does nothing. Change (R4).
- F6: Fresh opens the Remote Indicator menu. gvim uses F6 for a focus cycle. Change to match Fresh
  (R5).
- Explorer directory click: Fresh toggles on release, gvim on press. Timing only. Keep, and note it.

## 3. API ergonomics: before and after

Real call sites, so the port reads like Go and not like a transliteration.

**Context resolution.** Fresh (`refs/fresh/crates/fresh-core/src/action.rs:34`):

```rust
pub fn from_when_clause(when: &str) -> Option<Self> {
    let trimmed = when.trim();
    if let Some(mode_name) = trimmed.strip_prefix("mode:") {
        return Some(Self::Mode(mode_name.to_string()));
    }
    Some(match trimmed { "global" => Self::Global, "prompt" => Self::Prompt, /* ... */ })
}
```

gvim (`internal/keymap/keymap.go`, `canonicalContext`) keeps the same aliases but returns a string
type and passes unknown input through. The planned After is a `(Context, error)` signature that
rejects unknown contexts at load, as Fresh's `None` does.

**Errors.** Fresh returns `Result`; gvim returns `(T, error)` and wraps with `%w`. From the port
boundary in `internal/ui/ports.go:100-104`:

```go
type FileOps interface {
	CreateFile(ctx context.Context, dir, name string) (string, error)
	Rename(ctx context.Context, path, newName string) (string, error)
	Trash(ctx context.Context, path string) error
}
```

Long-running file work takes a `context.Context` and runs in a `tea.Cmd`. `Update` does no IO
(ARCH-v4 §4).

**Action dispatch.** Fresh matches on an action enum. gvim keeps the action name as a string and
looks it up in the editor's handler table, which returns `ErrUnknownAction` for a missing handler
(`internal/editor/editor.go:268-274`). This is the source of the dead-binding problem in §1.3. The
planned After is a gate in tests (§4.1), so every bound action either has a handler or is listed as
unsupported.

## 4. Test, fuzz and parity strategy

### 4.1 Unit and contract tests (in force) [V]

- Plain Go tests, table-driven, asserting on values and behavior (CLAUDE.md §6). No golden or
  snapshot tests for the TUI. `ui` tests drive `Update` and assert on model state, emitted
  commands, layout rectangles, hit-map results, and a few specific grid cells.
- Keymap contracts: `presetCounts` pins each preset's binding count (default 276, macos 340,
  emacs 380, vscode 280, macos-gui 394). A conflict gate rejects one chord in one context bound to
  two different actions in one file (§6.0, change 1). Repeated identical entries are allowed.
- App wiring: `TestNewUIDepsWiresFileOpsToTheEditor` checks that the file-ops port is wired and that a
  rename moves the open document (§6.0, change 2).

**Gates:**

1. *Dead-binding gate (in force, §6.0 change 3).* `TestEveryBoundActionRunsOrIsListed` in
   `internal/app/dead_bindings_test.go` runs every action bound in `global`, `normal` or
   `file_explorer` through `Model.Run` on a fresh model, and fails if the action is unhandled and not
   listed in `unhandledActions`, or if a listed action now works. It lists 85 gaps. It does **not**
   cover: bindings in overlay contexts (prompt, popup, menu, settings, terminal, completion,
   searchPrompt, dock, compositeBuffer), menu rows, or palette-only commands. Those are the next
   gate to write.
2. *Unknown-context gate (in force, §6.0 change 3).* `TestLoadRejectsUnknownWhenClause`.
3. *Config-applied gate (not written).* Each config key that the docs promise has a test that asserts a
   runtime effect (for example `tab_size` changes the rendered tab width), not only that it parses.

### 4.2 Fuzz targets (planned) [P]

Pure functions with untrusted input, suitable for `go test -fuzz`:

- `keymap.Normalize` and `keymap.Parse` on user JSON (must not panic; must reject bad chords).
- `fuzzy` scorer on arbitrary query and target strings (must not panic; indices within bounds).
- `editor.ParseLineCol` on `N`, `N:COL`, and garbage (must return an error, not panic).

These are not run yet.

### 4.3 Parity with Fresh

- The legacy golden files that CLAUDE.md §6 refers to (`migration/parity/`, `migration/tui_parity.py`)
  are not on disk [V]. ARCH-v4 §8 also names `migration/bench_tui.py`, which is not on disk either.
  CLAUDE.md should be updated or the files restored. That is open question Q7.
- Real A/B parity needs a build of `refs/fresh` (cargo, external code). Per the skill's rule 2 this
  needs the user's confirmation before it runs (open question Q6). Until then, parity is checked by
  data: the keymap binding counts, the themes (byte-identical), and the menu table (generated from
  `config.rs`).
- Tolerance: there are no numeric outputs, so the parity rule is exact: each Fresh default keymap
  entry resolves to the same action in the default preset. Intentional differences go into the
  known-differences list in §2.4.

## 5. Benchmark and PGO plan

### 5.1 Stages

| Stage | Status | Reason |
|---|---|---|
| `legacy_baseline` | not measured | Needs a release build of `refs/fresh` (Q6) |
| `idiomatic_go` | not measured | The port is not yet at a stable feature set |
| `tuned_go` | not started | Needs profiles first (rule: measure before tuning) |
| `pgo` | skipped | The interactive TUI is not CPU-bound in the sense PGO targets. The legacy baseline is unmeasured, so the 1-second rule cannot be applied |

### 5.2 Candidate workloads (planned) [P]

1. File index and palette ranking over 50,000 paths (`internal/finder`, `internal/fuzzy`). This is the
   stated cap, so it is a fixed, reproducible input.
2. Buffer edits on a 100,000-line file (`internal/buffer`, `internal/editor`).
3. Whole-buffer syntax lexing on version change (`internal/syntax/highlighter.go`). This is the most
   likely hotspot. It has a 2 MiB guard but lexes the whole buffer, so it is a candidate for
   checkpointed lexing.

Method: `go test -bench` with `-benchmem`, then `go tool pprof` CPU and allocation profiles. Peak RSS
via `/usr/bin/time -l`. No optimization is made without a profile that points to it.

## 6. Phased execution roadmap

### 6.0 Done in this pass (verified)

1. **Keymap conflict gate.** `internal/keymap/keymap.go`: `parse` runs `checkConflicts` on the file's
   own bindings. One chord in one context bound to two different actions, or to one action with
   different args, is an error. An identical repeat is accepted. Inherited bindings are still
   overridable by a child preset. `TestLastBindingWins` was replaced by `TestLoadRejectsConflictingChord`
   (it failed before the change), `TestLoadAllowsRepeatedSameBinding` and
   `TestChildPresetOverridesInheritedChord`.
2. **File operations wired.** `internal/app/ui.go`: `newUIDeps` sets `Ops: newFileOps(ed)`. Before,
   New, Rename, Delete, Copy, Cut and Paste showed "File operations are not supported in this port".
   `TestNewUIDepsWiresFileOpsToTheEditor` failed before the change. Delete goes to the system trash
   (`fsops.Trash`, `hymkor/trash-go`) and still asks first.

Gates after both changes [V]: `go build ./...`, `CGO_ENABLED=0 go build ./...`, `go vet ./...`,
`go test ./...`, `golangci-lint run ./...` (0 issues), `purity_check.sh` (PURE_GO_OK),
`import_rules_check.sh` (IMPORT_RULES_OK), `go test -race ./internal/app ./internal/keymap`.

3. **R1 slice: honest UI and when-clauses.**
   - Unknown `when` clauses and `mode:` clauses are load errors (`keymap.go`, `canonicalContext`).
     `TestLoadRejectsUnknownWhenClause` failed before the change.
   - The hard-coded " Trusted " segment is removed from the status row (`internal/ui/layout.go`).
     `Local` stays, because gvim has no remote workspaces. Two tests that asserted "Trusted" now
     assert the row without it. `TestStatusLeftGroupKeepsItsPosition` failed before the change.
   - `noop` is a handler that does nothing, with or without a document (`internal/editor`). It is
     how a keymap unbinds a key, and the macOS preset binds Alt+0–9 to it, so those keys showed
     "Not supported". `TestNoopIsSilentWithAndWithoutDocument` failed before the change.
   - The dead-binding gate (§4.1-1) is added with 85 listed gaps.

   Gates after this slice: the same list as above, all green. `go test -race` passes on
   `internal/app`, `internal/keymap`, `internal/editor` and `internal/ui`.

### 6.1 Roadmap

Each milestone ends with the gates in §6.0 green, plus its own test. Items refer to GAP-v3
numbering. Milestones are ordered by user impact and dependency, not by GAP's M-numbers.

| # | Milestone | Items | Verify with |
|---|---|---|---|
| R1 | **Honest UI.** Done in §6.0 change 3: dead-binding gate (85 listed), unknown-`when` rejected, " Trusted " label removed, `noop` handled. Still open: gate overlay contexts and menu rows, then fill the 85 gaps in R3–R9. | 63, plus gate | `go test ./internal/keymap ./internal/ui/... ./internal/app ./internal/editor` |
| R2 | **Config applied at runtime.** Load the layered config in `app.Run`. Apply theme, keymap override, `explorer_show_hidden`, `tab_size`, `auto_indent`. Decide the config path (Q4). | 27, 28, 35, 61 | `go test ./internal/config ./internal/app`; a runtime-effect test per key (§4.1-3) |
| R3 | **Editing core.** Line-ending detect and round-trip (test first, §1.3). Auto-indent, Tab as spaces, Shift+Tab dedent. Duplicate and delete line, select line, paragraph moves, line numbers and wrap toggles. Case conversion, sort lines, save-time trim and final newline. Toggle comment needs a per-language comment token (Q8). | 6, 23, 24, 25, 26 | `go test ./internal/editor ./internal/buffer`; round-trip test for CRLF and LF |
| R4 | **Explorer and browser.** Arrow-key preview (32). Type-to-search and fuzzy filter (33). Multi-select, cut dimming, Esc cancel, multi-item copy and paste (34). Ctrl+I show-gitignored (35). Watcher and Ctrl+R (36). Right-click row menu (37). Config width and side (38). Browser sort by Name, Size, Modified (8). Tree on `ListDir` (9). Open-folder re-root instead of the stub (14). Ctrl+B hides, Esc order (§2.4). | 8, 9, 14, 32–38 | `go test ./internal/ui/explorer ./internal/ui/browser ./internal/explorer` |
| R5 | **Chrome and mouse parity.** Status-bar click pickers (29). Tab "+" popup and right-click tab menu (30). Right-click on explorer. Menu-bar and status-bar visibility (§2.4). Alt+letter menu opening (`menu_open`). F6 as Fresh. Palette layout (§2.4). | 29, 30, plus layout | `go test ./internal/ui/...`; hit-map tests |
| R6 | **Search and replace.** Render match highlights (21). Wire `query_replace` (Ctrl+Alt+R) and the y/n/!/q flow (22). | 21, 22 | `go test ./internal/search ./internal/ui/find` |
| R7 | **Multi-cursor, block, marks, bookmarks, macros.** `Document` with `[]Cursor` (39). Block select (40). Bookmarks and macros (41). Auto-close, surround, matching bracket (42). | 39–42 | `go test ./internal/editor` (cursor-set invariants as table tests) |
| R8 | **Display aids and modes.** Rulers, indent guides, whitespace, current-line, occurrence highlight (43). Auto-save and read-only (44). Indent folding (45). | 43–45 | `go test ./internal/ui/editorpane ./internal/editor` |
| R9 | **LSP.** Wire `internal/lsp` into `app` (50). Diagnostics (51). Goto definition, references, hover (52). Completion, rename, code actions (53). Inlay hints and status (54). Splits and horizontal scrollbar, deferred from §2.4 if needed. | 50–54 | Fake-server tests; the `lsp` package already has `TestInitializeRoundTrip` |
| R10 | **Plugins and settings.** Plugin runtime (46). First-slice API (47). Full API (48). TypeScript transpile (49). Settings screen (59). Blocked by Q2. | 46–49, 59 | Decided after Q2 |
| R11 | **Terminal.** PTY pane (55). Pane layout and keyboard capture (56). Send selection to terminal (57). Windows ConPTY (58). Blocked by Q1. | 55–58 | `CGO_ENABLED=0` must still pass on every OS |
| R12 | **Session, git, encoding, keybinding editor.** Hot exit and session restore (60). Git status (64). Keybinding editor and style presets (66). Calibrate input and event debug (67). Non-UTF-8 encodings (68, with `golang.org/x/text`, pure Go). | 60, 64, 66–68 | Per-item tests |

### 6.2 Dependency decisions (proposals) [P]

- Terminal (R11): a Unix PTY package such as `github.com/creack/pty` (MIT, pure Go on Unix), plus a
  ConPTY path for Windows. Check with `CGO_ENABLED=0` and `GOOS=windows CGO_ENABLED=0` before adding.
- Plugins (R10): `github.com/dop251/goja` (pure Go JavaScript) and `github.com/evanw/esbuild` (Go API,
  for TypeScript). GAP-v3 R9 names a slowjs C lineage. That choice is an owner decision (Q2).
- Encodings (R12): `golang.org/x/text`. Already in `go.mod` as indirect.
- Every new module must pass the purity gate first, and `govulncheck` after adding it.

### 6.3 Open questions for the user

- **Q1. Terminal in scope?** R11 needs a PTY dependency and Windows ConPTY support. Is a terminal pane
  required for "1:1"?
- **Q2. Plugin runtime.** Use goja plus esbuild, or port slowjs? Recommendation: goja plus esbuild,
  because both are pure Go.
- **Q3. macOS quick-open keys.** On macOS, `ctrl+'` and `ctrl+;` go to error navigation, matching
  Fresh. Keep Fresh's behavior, or give quick-open those keys? Requirement 2 says no conflicts, but
  it also allows matching Fresh exactly. Recommendation: keep Fresh's behavior and document it.
- **Q4. Config path.** Fresh reads `~/.config/fresh/config.json`. gvim uses
  `os.UserConfigDir()/gvim/config.json`, which is `~/Library/Application Support/gvim` on macOS.
  Recommendation: `~/.config/gvim` on all platforms, to match Fresh.
- **Q5. "One feature, one shortcut."** Requirement 2 can be read two ways. This blueprint enforces
  "one chord means one action". It does not enforce "one action has one key", because Fresh itself
  gives many actions more than one key. Say if you want the stricter rule.
- **Q6. Legacy build and A/B parity.** Building `refs/fresh` with cargo runs external code, so the
  skill requires your confirmation. It is needed for `legacy_baseline` and for real parity runs.
- **Q7. Parity files.** CLAUDE.md §6 refers to `migration/parity/` and `migration/tui_parity.py`, which
  are not on disk. Restore them, or update CLAUDE.md to match the current tree?
- **Q8. Comment token for toggle comment.** Per-language comment tokens: from chroma lexer metadata, or
  a table in `internal/syntax`?

### 6.4 Skill steps not run in this pass

The migrate-to-golang skill's outputs are not all produced yet. Stated plainly:

- Telemetry ledger, `log-test-run`, `log-parity-run`, `log-benchmark`, infographic: not produced.
  No legacy build, so no `legacy_baseline` numbers.
- Run in this pass, with results:
  - `go fix -diff ./...`: empty output, nothing to fix.
  - `govulncheck ./...`: "No vulnerabilities found."
  - `go test -cover ./...`: the 90% target is **not** met. Below 90%: `internal/app` 68.8%,
    `internal/lsp` 59.8%, `internal/uitest` 75.6%, `internal/fsops` 78.7%, `internal/workspace` 83.1%,
    `internal/ui/find` 84.3%, `internal/ui` 86.8%, `internal/ui/grid` 88.0%. The rest are 89.6% or
    higher, most above 90%. The root `main` package has no statements to cover. Raising `internal/app`
    and `internal/lsp` is part of R2 and R9.
- A/B parity on N deterministic cases: blocked by Q6.
- PGO: skipped, §5.1.

## 7. Status after the parallel rounds (2026-11-05)

This section records what was merged after §6.0 and §6.1, so the file matches the tree. Each
round was implemented in its own worktree from a snapshot, merged as patches with disjoint file
ownership, and checked by the gates in §6.0 (build, `CGO_ENABLED=0` build, vet, `go test ./...`,
`go test -race` on touched packages, golangci-lint, purity, import rules, `go fix -diff`, govulncheck).

### 7.1 Roadmap rows

| Row | Status | What is in the tree | Still open |
|---|---|---|---|
| R1 | done | Dead-binding gate (85 gaps listed at the time, now fewer). Unknown `when` rejected. " Trusted " removed. `noop` handled. | Overlay contexts and menu rows are not gated. |
| R2 | mostly done | Config applied at startup: theme, keymap, keybindings overrides, explorer show-hidden and ignored, editor options, line numbers, menu bar. `--config` replaces the user file. Missing `--config` file is an error. `config show` and `config paths` honour `--config`, `--keymap`, `--theme`. | Unknown names in config files are not validated by `config show`. Override action names are not validated. |
| R3 | mostly done | Line endings (CRLF and LF, majority rule). Save-time trim and final newline. Tabs and dedent, auto-indent. Paragraph moves, select line, expand line selection, duplicate, delete, open line, transpose, case conversion, toggle comment (token from file extension), scroll and recenter. | Line-wrap and line-number toggle wording. Sort lines (not bound). CR-only endings. |
| R4 | partly done | Explorer: ListDir scan, arrow preview, multi-select, cut dims, type-to-search, Ctrl+I, Esc order, Backspace, browser sort (Alt+S). Ctrl+B hides on the default preset. | Watcher not wired. Show-hidden not persisted. Search text not drawn. Browser column-header sort. |
| R5 | partly done | Menu accelerators (Alt+letter), hidden menu bar, status row hidden under palette and browser, EOL shown from the document, app-wide keys work from the explorer, tab strip one-tab scroll. | Tab and right-click popups (GAP 29, 30). Macros and statusbar package unwired. |
| R6 | done for the in-buffer bar | Replace family from the keymap, query-replace with y, n, a, !, q, c, highlight painting in the pane. | Wrap-around. Zero-match message. Project-wide search. Esc during query-replace does not stop the run. |
| R7 to R12 | not started | Multi-cursor, bookmarks, macros, LSP wiring, plugins, terminal, sessions, encodings. | As in §6.1. |

### 7.2 Review loop

Five review rounds ran, each as read-only reviewers and a verifier that reproduced findings in a
scratch worktree. Every confirmed finding was fixed with a failing test written first, except where
the decision is listed under 7.3. The last review (round 5) reproduced 10 findings. One medium
regression, introduced by the round-5 path fix, was fixed in this pass: `absPath` now keeps the
caller's directory spelling unless the lexical and physical directories are different directories.
Nine low findings remain; see 7.4.

### 7.3 Decisions still open

- **macOS trash location.** `fsops.Trash` uses the freedesktop home trash, not `~/.Trash` or Finder.
  Options: move into `~/.Trash` on the same volume, or ask Finder to delete. Owner decision.
- **macOS Ctrl+B in the explorer.** `macos.json` binds Ctrl+B in the file_explorer context to
  `focus_editor`, so on macOS it moves focus instead of hiding the panel. The default preset hides.
  Decide which behaviour macOS should have, then align the keymap data.
- **Q1 to Q7 in §6.3** are unchanged. Q8 is resolved: comment tokens come from the file extension
  (`internal/editor/comment.go`).
- **Same-size outside edits.** The on-save check compares size and mtime. An outside edit that keeps
  both is not detected, and a rename no longer refreshes the recorded state unless the bytes match.
- **Update does IO in two documented places.** `CheckRename` stats hard-link aliases from Update, and
  `followDisk` reads the moved file from Update. Both are documented exceptions to the project rule.
  Removing them needs a different design (for example, a command that runs before the rename).
- **`config show` and `config paths` with a missing `--config` file** fail with an error. Confirm that
  this is the intended behaviour.

### 7.4 Known limits (not fixed, low severity)

1. A file with mixed line endings, moved across devices before its first save, is refused by Save
   (the byte rule compares against the written form). Fail-safe.
2. A `..` element that passes through a symlink is resolved physically only when the directories
   differ. A path whose lexical and physical directories are the same keeps its spelling.
3. `Copy` containment checks are lexical for the top-level guard, so a source reached through a
   symlinked parent that physically lies inside `dst` is not refused. Content survives, the documented
   refusal is bypassed (pre-existing).
4. `physicalPath` and `linkTargetIsDest` still use `filepath.Abs` on some link-text inputs, which
   cleans `..` lexically. Reachable only from non-UI callers today.
5. A cut paste stops at its next job when the clipboard is replaced by another cut, and the notice
   says the cut was cancelled. The wording is imprecise.
6. A cut of a folder and its picked child moves only the folder. Copy of the same selection copies both.
   This is intentional for cut and reported as a scope decision.
7. Palette `FormatChord` renders the plus key as raw text, so `ctrl++` shows as text.
8. Primary's global-shadow rule matches Resolve for normal context but not for file_explorer.
9. Bindings in contexts the UI never resolves (dock, completion, terminal, and so on) are accepted by the loader.
10. `go fix -diff ./...` reports one skipped suggestion in `internal/editor` that conflicts with another
    edit. It is a notice, not a diff hunk.

### 7.5 Gate state at the end of this pass

Build, `CGO_ENABLED=0` build, vet, `go test ./...`, purity, import rules, govulncheck: green.
golangci-lint: 0 issues. `go test -race` on the touched packages: green. `go fix -diff`: only the
notice in 7.4-10. Coverage was not re-measured after the rounds.
