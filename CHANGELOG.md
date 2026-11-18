# Changelog

All notable changes to `gvim` will be documented in this file.

## [0.1.0] - 2026-11-18 (Public Preview)

First public preview release of `gvim`, a pure-Go terminal code viewer and editor built on Bubble Tea v2 with a zero-dependency cell-grid renderer.

### Highlights

- **Pure-Go Architecture (`CGO_ENABLED=0`)**: Single static binary targeting macOS, Linux, and Windows across `amd64` and `arm64`, with strict modular layering enforced by `tools/import_rules_check.sh` and `tools/purity_check.sh`.
- **High-Performance Core & Search**:
  - Row-buffered zero/low-allocation grid diffing and cached pointer-motion view skips.
  - Concurrency-bounded worker pools for Live Grep and Project Search, reusable DP scratch buffers in `fuzzy.Matcher`, bounded min-heap top-K selection in `finder.Rank`, O(1) primary shortcut lookup, and binary-searched indentation folds.
- **Friendly Mouse UX & Pointer Polish**:
  - `Alt+Click` multi-cursor toggle, `Cmd+Click` Go to Definition / clickable URL launch, and `Ctrl+Click` / `Shift+Click` selection extension.
  - Debounced mouse hover triggering LSP Hover docs, split border hover highlight & double-click 50/50 split equalization, gutter click/drag whole-line selection & double-click fold toggle.
  - Middle-click clipboard paste in editor and terminal dock, clickable Markdown task checkboxes (`- [ ]` / `- [x]`), clickable Sticky Scroll scope header, clickable status-bar diagnostics badge, dock drag-resize and `" + "` tab button, and mouse support across Explorer compact chains, Live Grep chips, Settings layers, and File Browser scrollbars.
- **UI/UX & Go Identity**:
  - New flagship `gopher` theme with Go Cyan (`#00ADD8`) accents alongside 10 classic themes (`11` bundled themes total), plus `NO_COLOR` support and full 16-color ANSI syntax highlighting in `terminal` theme.
  - Polished Welcome screen wordmark (`v0.1.0 · Go editor`), `Help: About gvim` modal, expanded `F1` Help reference sections, Sticky Scroll syntax highlighting & menu/palette commands, split-pane `toggle_scroll_sync`, collapsed fold line counts (`⋯ N lines`), italic preview tabs, `[RO]` indicators, colorized git/diagnostic status badges, and dynamic terminal window titles.
- **CI & Release Automation**: Added GitHub Actions CI (`ubuntu-latest` & `macos-latest`), GoReleaser configuration, and tagged release workflow.
