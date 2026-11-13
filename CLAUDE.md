# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

Default agent guidelines for a go-sphere project. Copy this file to the project root as
`CLAUDE.md` (or `AGENTS.md` for Codex and OpenCode), then fill in the project-specific
section at the bottom.

The behavioral rules are adapted from
[andrej-karpathy-skills](https://github.com/forrestchang/andrej-karpathy-skills).

**Tradeoff:** These guidelines bias toward caution over speed. For trivial tasks, use judgment.

## 1. Think Before Coding

**Don't assume. Don't hide confusion. Surface tradeoffs.**

Before implementing:
- State your assumptions explicitly. If uncertain, ask.
- If multiple interpretations exist, present them - don't pick silently.
- If a simpler approach exists, say so. Push back when warranted.
- If something is unclear, stop. Name what's confusing. Ask.

## 2. Simplicity First

**Minimum code that solves the problem. Nothing speculative.**

- No features beyond what was asked.
- No abstractions for single-use code.
- No "flexibility" or "configurability" that wasn't requested.
- No error handling for impossible scenarios.
- If you write 200 lines and it could be 50, rewrite it.

Ask yourself: "Would a senior engineer say this is overcomplicated?" If yes, simplify.

## 3. Surgical Changes

**Touch only what you must. Clean up only your own mess.**

When editing existing code:
- Don't "improve" adjacent code, comments, or formatting.
- Don't refactor things that aren't broken.
- Match existing style, even if you'd do it differently.
- If you notice unrelated dead code, mention it - don't delete it.

When your changes create orphans:
- Remove imports/variables/functions that YOUR changes made unused.
- Don't remove pre-existing dead code unless asked.

The test: Every changed line should trace directly to the user's request.

## 4. Goal-Driven Execution

**Define success criteria. Loop until verified.**

Transform tasks into verifiable goals:
- "Add validation" → "Write tests for invalid inputs, then make them pass"
- "Fix the bug" → "Write a test that reproduces it, then make it pass"
- "Refactor X" → "Ensure tests pass before and after"

For multi-step tasks, state a brief plan:
```
1. [Step] → verify: [check]
2. [Step] → verify: [check]
3. [Step] → verify: [check]
```

Strong success criteria let you loop independently. Weak criteria ("make it work") require constant clarification.

## 5. go-sphere Conventions

- Never edit generated code by hand (`*.pb.go`, ent output, swagger, generated TypeScript
  clients). Change the `.proto` or ent schema and re-run the project's generate target.
- Respect `.sphere/layout.json` ownership rules: files the layout owns are changed through
  the layout sync flow, not ad hoc.
- Run the project's own gates (`make test`, `make lint`, `make check`) before calling work
  done. Read the `Makefile` for fixture and generation targets.
- Do not run commands with outside side effects (publishing, tagging, pushing, deploying)
  without explicit approval.

## 6. Project-Specific Instructions

gvim is a terminal editor in Go: a port of the Fresh TUI (Rust, checked out under `refs/fresh`)
on Bubble Tea v2. It is **not** a proto/ent project, so section 5 applies only where it fits:
there is no `Makefile`, no `.sphere/layout.json`, and no generate target.

### Commands

```
go build ./... && CGO_ENABLED=0 go build ./...   # shipped binary must stay pure Go
go vet ./...
golangci-lint run ./...                          # v2; reports 0 issues
bash tools/purity_check.sh             # pure-Go gate, prints PURE_GO_OK
bash tools/import_rules_check.sh       # layering gate R1-R16, prints IMPORT_RULES_OK
bash tools/i18n_check.sh               # locale catalogue gate
python3 tools/gen_menus.py --check     # generated menu table is in sync
go test ./...
go test ./internal/tui -run TestName             # single test
go test -race ./...                              # test-only; needs cgo
go run . [DIR] [FILE[:LINE[:COL]]]...           # launch the TUI
```

### Architecture

The design record is `docs/architecture.md` (package table, import rules R1-R16,
package maps). The shape that needs several files to see:

- Three layers. `main.go` (module root, kept there so `go install` works) →
  `internal/cli` (cobra flags and arguments) →
  `internal/app.Run`. `internal/` is the private executable shell (`app` plus the host packages
  `uiadapter`, `pluginhost`, `lsphost`, `restore`, `keybindings`, `highlight`, the
  test-only `apptest`, then `configview`, `rexplugin`, `cli`, `tui`, `terminal`, `filewatch`,
  `daemon` (the attach/detach socket host and workspace registry), `agentdiscover`
  (tmux and agent-transcript session discovery) and `uitest`). `cmd/gvimdebug` is a
  developer page that runs gvim in a PTY. TUI-free engine and library packages are at the
  module top level: `buffer`, `document`, `editor`, `search`, `syntax`, `fold`, `diff`, `encoding`,
  `session`, `decorations`, `textlayout`, `workspace`, `explorer`, `finder`, `fuzzy`,
  `fsops`, `gitstate`, `config`, `keymap`, `theme`, `trust`, `outline` (the Markdown
  heading scan behind the sidebar's Contents section), `lsp`, `pluginapi`,
  `plugin`, `commands`, the LSP manager/test support (`lsp/manager`, `lsp/lsptest`), the
  plugin OS port (`plugin/hostos`), `authority` (the workspace backend: local or SSH),
  `env` (project environment detection and activation), `i18n` (the message
  catalogue), `updatecheck` (the release check), and the shared leaves `atomicfile`
  and `shellcmd`.
- Import direction: engine → shell, never the reverse. No top-level package imports
  `internal/`. Only `internal/app` imports `uiadapter`, `pluginhost` and `lsphost`;
  `restore` and `highlight` are also imported by `uiadapter`, and `keybindings` by
  `uiadapter`, `pluginhost` and `configview`. Only `internal/app` and
  `internal/uiadapter` import the TUI sub-packages; `internal/cli` imports `internal/app`
  for `Options`/`Run`, `internal/configview` + `syntax` for the config and grammar
  commands, `internal/rexplugin` for the Rex integration commands, and `authority` to
  classify remote targets.
  `tools/import_rules_check.sh` is the enforcement.
- `internal/app` is the composition root (`Options`, `Run`, `buildRuntime`, settings,
  logfile, live grep, default formatters, and the remote and daemon-workspace wiring); the
  `gvim config` backend is
  `internal/configview`. The host packages implement the ports:
  `uiadapter` (editor/document/dialogs/files/git/projects/terminal/clipboard),
  `pluginhost`, `lsphost`, `restore`, `keybindings`, `highlight`.
- `internal/tui` is one Bubble Tea v2 root model (`package tui`). It depends on
  interfaces it defines in `ports.go` (`EditorState`, `Document`, ...); `app` satisfies
  them with adapters. `tui` must not import concrete `finder`, `fsops`, `lsp` or the
  plugin runtime; its sub-packages (`overlay`, `list`, `frame`, `sidebar` (the explorer
  column's section stack and Contents widget), `split`, `tabstrip`, `dock`,
  `editorpane`, `menubar`,
  `explorer`, `palette`, `find`, `prompt`, `browser`, `picker`, `pluginpanel` (the
  plugin API's declarative widget panel), `searchpanel` (project search and replace),
  `popup`, `ctxmenu`, `dialog`, `style`, `grid`, `icons`, `space`, `textsafe` (the
  sanitizer for untrusted text)) must not import the root
  `tui`.
  `internal/tui/frame` is the single geometry source for painting and the hit map;
  `internal/tui/split` owns the split tree and `internal/tui/tabstrip` the strip paint.
  The sidebar column, its sections and the Markdown Contents section are recorded in
  `docs/sidebar.md`.
- Engine packages (`buffer`, `document`, `editor`, `search`, `syntax`, `fold`, `diff`,
  `encoding`, `session`, `decorations`, `textlayout`, `outline`) are TUI-free: no bubbletea, bubbles or
  lipgloss. `document` is the per-file model; `editor` is the tab/action session over it. `internal/tui/style` is the only place theme data becomes lipgloss styles.
- Rendering is a custom cell grid (`internal/tui/grid`) on `charmbracelet/x/ansi`, not
  bubbles `viewport`/`textarea`. `Update` does no IO: file and finder work runs in
  `tea.Cmd`s. The palette and finder must not filter synchronously in `Update` (cap of
  100 items fed to the list).
- Keymap presets (`keymap/*.json`) and themes (`theme/data/*.json`) are data files;
  config is JSONC (`hujson`).
- Big packages keep their internals findable: `docs/architecture.md` has a file map for
  the TUI root, `document`/`editor`, `buffer` (two storage modes), `plugin`
  (runtime/API/loader) and the app host packages.

### Testing

- Write plain Go unit tests: table-driven, asserting on values and behaviour. Do **not** add
  golden-file or snapshot tests for the TUI (no `testdata` screen dumps, no full-frame string
  comparisons, no new golden-screen scenarios). They are too costly to maintain.
- For `internal/tui` code, drive `Update` with messages and assert on model state, emitted `tea.Cmd`s,
  layout rectangles and hit-map results. To check rendering, assert on a few specific cells or
  rows of the `grid`, not on the whole frame.
- Test domain packages (`buffer`, `editor`, `search`, `fuzzy`, ...) directly, without Bubble Tea.
- Fix a bug with a regression test that fails first. Run the single package's tests while
  iterating, then `go test ./...` before finishing.
- The existing `testdata` fixture (`commands/testdata`) stays as it is. Do not extend it, and
  do not regenerate it to make a failing test pass.

### Generated and reference code

- `commands/commands_gen.go`: regenerate with
  `python3 commands/gen/generate.py refs/fresh`. Do not edit by hand.
- `internal/tui/menubar/menu_table_gen.go`: regenerate with `python3 tools/gen_menus.py`
  (`--check` reports drift). It reads `refs/fresh`. Do not edit by hand.
- `refs/` (crush, fresh, lazygit, micro, orbiton, toast) are read-only reference checkouts. They
  are not part of the build (`go list ./...` lists nothing under `refs`). Do not edit or import
  them.

---

**These guidelines are working if:** fewer unnecessary changes in diffs, fewer rewrites due to overcomplication, and clarifying questions come before implementation rather than after mistakes.
