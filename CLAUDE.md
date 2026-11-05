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
golangci-lint run ./...                          # v2; currently reports 0 issues
bash migration/tools/purity_check.sh             # pure-Go gate, prints PURE_GO_OK
bash migration/tools/import_rules_check.sh       # ARCH-v4 import graph, prints IMPORT_RULES_OK
go test ./...                                    # all packages pass today
go test ./internal/ui -run TestName              # single test
go test -race ./...                              # test-only; needs cgo
go run . [DIR] [FILE[:LINE[:COL]]]...            # launch the TUI
```

`master` has no commits yet and everything is untracked, so `git diff` shows nothing useful.

### Architecture

The design record is `migration/ARCH-v4.md` (package table, allowed imports, phases P0–P10,
acceptance gates). Where it conflicts with `BLUEPRINT*.md`, ARCH-v4 wins. It was written as a
proposal, so check names against the source. The shape that needs several files to see:

- `main.go` → `cli.Execute(ctx, args, stdout, stderr, app.Run)`. `internal/cli` is cobra only
  (flags, `DIR`/`FILE:LINE:COL` classification via an injected `stat`) and hands `app.Options`
  to a `Runner`. A file named `config`, `grammar`, `completion` or `help` must be written `./name`.
- `internal/app` is the composition root and the only package that imports concrete adapters
  (`buildModel` in `run.go`: explorer tree, editor, finder index, keymap, theme → `ui.Model`).
- `internal/ui` is one Bubble Tea v2 root model. It depends on interfaces it defines in
  `ports.go` (`EditorState`, `Document`, …); `app` satisfies them with adapters. `ui` must not
  import concrete `finder`, `fsops`, `lsp`; its sub-packages (`editorpane`, `menubar`, `palette`,
  `find`, `prompt`, `browser`, `explorer`, `statusbar`, `style`, `grid`) must not import `ui`.
  `layout.go` is the single geometry source for both painting and the hit map (`hit.go`).
- Domain packages (`buffer`, `editor`, `search`, `explorer`, `finder`, `fuzzy`, `palette`,
  `keymap`, `theme`, `syntax`, `fsops`, `workspace`, `config`, `lsp`) must not import bubbletea,
  bubbles or lipgloss. `ui/style` is the only place theme data becomes lipgloss styles.
- Rendering is a custom cell grid (`ui/grid`) on `charmbracelet/x/ansi`, not bubbles
  `viewport`/`textarea`. `Update` does no IO: file and finder work runs in `tea.Cmd`s. The palette
  and finder must not filter synchronously in `Update` (cap of 100 items fed to the list).
- Keymap presets (`internal/keymap/*.json`) and themes (`internal/theme/data/*.json`) are data
  files; config is JSONC (`hujson`).

### Testing

- Write plain Go unit tests: table-driven, asserting on values and behaviour. Do **not** add
  golden-file or snapshot tests for the TUI (no `testdata` screen dumps, no full-frame string
  comparisons, no new `migration/parity` scenarios). They are too costly to maintain.
- For `ui` code, drive `Update` with messages and assert on model state, emitted `tea.Cmd`s,
  layout rectangles and hit-map results. To check rendering, assert on a few specific cells or
  rows of the `grid`, not on the whole frame.
- Test domain packages (`buffer`, `editor`, `search`, `fuzzy`, ...) directly, without Bubble Tea.
- Fix a bug with a regression test that fails first. Run the single package's tests while
  iterating, then `go test ./...` before finishing.
- Existing `testdata` fixtures and the 26-scenario parity floor stay as they are. Do not extend
  them, and do not regenerate them to make a failing test pass.

### Generated and reference code

- `internal/palette/commands_gen.go`: regenerate with
  `python3 internal/palette/gen/generate.py refs/fresh`. Do not edit by hand.
- `internal/ui/menubar/menu_table_gen.go`: regenerate with `python3 migration/tools/gen_menus.py`
  (`--check` reports drift). It reads `refs/fresh`. Do not edit by hand.
- `refs/` (crush, fresh, lazygit, micro, orbiton, toast) are read-only reference checkouts, and
  `_attic/tui` is the retired legacy TUI. Neither is part of the build (`_attic` is ignored by
  `./...`, and `go list ./...` lists nothing under `refs`). Do not edit or import them.
- Parity tooling (`migration/tui_parity.py`, `migration/parity/`) compares against Fresh goldens
  and needs a Python venv with `pyte`. See `migration/parity/FLOOR.md` for the commands. Goldens
  other than the 26-scenario floor are informational only. This is a manual comparison tool, not
  a pattern to copy into Go tests (see Testing).

---

**These guidelines are working if:** fewer unnecessary changes in diffs, fewer rewrites due to overcomplication, and clarifying questions come before implementation rather than after mistakes.
