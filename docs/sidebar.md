# The sidebar column and the Contents section

Status: shipped design record (round 1, 2026-11-08). The rules the left column
follows: how it is split into sections, how a section is drawn and hit-tested,
and how the Markdown Contents section behaves. The spacing and the frame rules
come from `docs/ui.md`; the package tree from `docs/architecture.md`.

Fresh's reference design is `refs/fresh/docs/internal/sidebar-sections-design.md`,
and the section itself is bundled there as the plugin
`refs/fresh/crates/fresh-editor/plugins/markdown_toc.ts`. gvim builds the section
into the TUI instead of a plugin (it has no widget runtime); the behaviour and
the config keys are Fresh's, the chrome follows gvim's style.

## 1. The column

The left column is a stack of sections, top to bottom. Section 0 is the File
Explorer; the Contents section sits under it while it is on screen, and a future
section takes the next slot. The column spans the frame's explorer width
(`frame.ExplorerCols`) from the explorer header row to the body's last row:

```text
┌ ▼ File Explorer ─────────⟳─×─┐   section 0 header: the column's top border
│▼ demo                        │   section 0 body: two walls, the tree rows
│  ▼ crates                    │
├ ▼ Contents ────────────────×─┤   section 1 header: one shared border row
│▼ Interface                   │   section 1 body
│  ▼ Window                    │
└──────────────────────────────┘   the column's own bottom border
```

**Adjacent sections share one border row**: a section's header row is its top
border, and it is the row the section above would otherwise have spent on a
bottom border. A section is exactly `[header row, body]`; its body draws its two
walls and nothing above or below them. With one section — the explorer and no
Contents — the column is the bordered box the explorer has always been: the same
cells in the same places, which the explorer's own tests pin.

With more than one section every header row gains a chevron (`▼` open, `▶`
collapsed) and is a control: a press on it gives that section the keyboard and,
for the Contents section, starts the divider drag (a release that never moved
folds the section). The chevron is the indicator, not the only target. With one
section there is no chevron and no shared row.

The close button is the `×` at the right of a header row: the explorer's hides
the whole column, as it always has; the Contents section's hides that section.

## 2. The frame contract

The geometry is computed once per frame by `frame.ComputeLayout` from
`Input.Sidebar` (`[]sidebar.Spec`), and painting and hit testing both read
`Layout.Sections` (`[]sidebar.Section`), so a header, a row and its click target
cannot drift apart.

| Field | Meaning |
|---|---|
| `Section.HeaderY` | the header row: the column's top border for section 0, the shared row for the rest |
| `Section.BodyTop`, `BodyBottom` | the body rows, inclusive; a folded section has `BodyBottom < BodyTop` and `Section.Rows() == 0` |
| `Section.Collapsed` | effective fold state: the reader's, or the squeeze of a short column |
| `Layout.SectionReload`, `Layout.SectionClose` | the button columns of each header, -1 when the width leaves no room |
| `Layout.ExTitleY`, `ExBodyTop`, `ExRows`, `ExReloadX`, `ExCloseX` | section 0's, unchanged names, now derived from `Sections[0]` |

Rows are shared like this (`sidebar.Layout`):

- A section that asked for rows (`Spec.Rows > 0`, Contents) is fixed at that
  many; the sections that asked for none (the explorer) share the remainder.
- A folded section gives its rows to the open ones. When every open section
  asked for rows, the last open one is the remainder.
- When the column is too short to give every open section one body row, sections
  fold from the bottom up — the tree stays. A fixed section shrinks, bottom up,
  before it would leave the explorer without its one row. Both are resolved for
  the frame, not written into the reader's state: a taller frame gets the
  requested rows back.
- When every section is folded the column is a stack of header rows over ground
  (a blank area in the column's background), closed by the bottom border.
- A column too short even for one header row and its bottom border shows the top
  section only; the rest are dropped for that frame.

The mouse targets (`frame.HitKind`), in the column:

| Kind | Cell |
|---|---|
| `HitExplorerRow`, `HitExplorerBody`, `HitExplorerTitleReload`, `HitExplorerTitleClose`, `HitExplorerGrip` | section 0's rows, blank body, buttons and right-edge grip, as before |
| `HitSidebarHeader` (`Index` = section) | a header row cell that is not a button |
| `HitSidebarClose` (`Section`) | a non-explorer section's `×` |
| `HitSidebarRow` (`Section`, `Index` = row offset in the section body) | a Contents row |
| `HitSidebarBody` (`Section`) | a non-explorer section's blank body or wall |

The grip is the column's, not a section's: a drag on the right border resizes the
column exactly as before, whatever section it is over. A press on the column's
bottom border is not a target when the column has more than one section.

## 3. The Contents section

The section lists the headings of the active Markdown buffer as an indented,
foldable tree. It is TUI-free where it can be: the scan is package `outline`,
the widget and its geometry are package `internal/tui/sidebar`, and the root
model (`internal/tui/markdown_toc.go`) owns the buffer lifecycle and the
keyboard.

**Heading scan** (`outline.Scan`): ATX headings (`#` to `######`, a closing
`#` run stripped) and setext headings (a paragraph line underlined with `===`
or `---`). Fenced code blocks are skipped, matched by their own character and
length, so a `# comment` in a shell block is not a heading. Positions are line
numbers (the editor jumps by line) and byte offsets (the fold state of a heading
survives a rescan by byte). The scan is bounded to `outline.MaxScanBytes`
(1 MiB, the same order as the editor's large-file threshold): a larger document
keeps the rows it last had and the section says the file is too large for an
outline. A document in large-file mode is not scanned.

**Rows**: one heading per row, indented two cells per level (gvim's tree
convention; Fresh's sidebar widget indents one column per level). A heading with
deeper headings after it wears the disclosure glyph (`▼` open, `▶` folded);
folding a heading hides its section of the tree, not the buffer. The selected
row follows the heading the caret is under, or the heading at the top of the
pane in `scroll` mode and while the editor pane does not have the keyboard. The
selection is always painted (a background band) so it can be read while editing;
the `▌` marker shows when the section has the keyboard.

**Refresh**: on a buffer switch and when the section opens, the buffer is
scanned at once. Edits arm one 300 ms debounce tick (Fresh's
`RESCAN_DEBOUNCE_MS`) and the scan runs when it fires; a stream of messages about
the same edit does not keep pushing it out.

**Jumping**: Enter (or a press on a row, or a press on another row's blank
cells) moves the caret to the heading's line. Enter also gives the keyboard back
to the editor; a click keeps it in the sidebar, as the explorer's click previews
without opening.

**Keyboard** (while the section has the keyboard): Up/Down move the selection,
PageUp/PageDown move a page, Home/End go to the first/last row, Left folds the
selected heading or selects its parent, Right unfolds it or steps into its first
child, Enter jumps, Esc leaves for the editor; Space/Enter unfold a folded
section. In `scroll` mode the caret does not move while browsing; the pane is
scrolled to the selected heading instead. The app-wide actions keep their Normal
bindings, so Ctrl+P, Ctrl+B and the tab and split keys work; typed text is
dropped and never reaches the buffer. F6 and `focus_next_sidebar_section` /
`focus_prev_sidebar_section` cycle explorer → Contents → editor; focusing a
folded section unfolds it.

**Mouse**: press a header row to focus the section, drag the Contents header to
resize it (the explorer keeps at least one row), a release that never moved
folds it; the `×` closes it; the wheel scrolls its window; a press on a row
selects and jumps, a press on its glyph folds it.

## 4. Settings

The config keys are Fresh's, under the bundled plugin's old name, so a Fresh
config and the previous gvim plugin config keep working. Only the user layer is
read (the project layer of a plugin's settings applies only to keys a plugin
declared as project-level, and markdown_toc declared none):

```jsonc
{
  "plugins": {
    "markdown_toc": {
      "enabled": true,                       // the section's master switch
      "settings": {
        "max_depth": 6,                      // integer 1-6: deepest heading level listed
        "follow": "cursor",                  // "cursor" | "scroll": what the selection follows
        "fold_buffer": false,                // also fold the buffer section on a fold
        "rows": 10,                          // integer 1-200: body rows the section asks for
        "auto_open": true                    // show for Markdown buffers, hide otherwise
      }
    }
  }
}
```

| Key | Type | Default | Meaning |
|---|---|---|---|
| `enabled` | boolean | `true` | the section exists at all; off means no rows and the palette command says it is disabled |
| `max_depth` | integer 1-6 | `6` | headings deeper than this are not rows of the tree |
| `follow` | `"cursor"`/`"scroll"` | `"cursor"` | which heading the selected row follows |
| `fold_buffer` | boolean | `false` | folding a heading also folds the editor's fold on that line (see the gap below) |
| `rows` | integer 1-200 | `10` | body rows the section asks for; a divider drag overrides it for the session |
| `auto_open` | boolean | `true` | show the section when a Markdown buffer becomes active, hide it when the buffer is not Markdown |

`rows` and the divider drag's value are session state, not saved in the session
file yet; the config value is used from the next start. The
`markdown_toc_toggle_follow` action flips `follow` for the session.

## 5. Commands

The palette rows keep the retired plugin's names, so they are where they were:

| Palette row | Action | Behaviour |
|---|---|---|
| Markdown: Show Outline | `markdown_toc_show` | show the section for the active Markdown buffer and give it the keyboard; a text buffer says "Not a Markdown buffer" |
| Markdown: Close Outline | `markdown_toc_close` | hide the section for the buffer being read |
| Markdown: Contents — Follow Cursor/Scroll | `markdown_toc_toggle_follow` | flip `follow` for the session |

The actions are in `commands/markdown_commands.go`, the hand-written table the
compare rows also use, because the section is not in the generated Fresh table.
They are ordinary ui actions, so a keybinding may name them.

**Auto-open**: with `auto_open` on, the section appears when a Markdown buffer
becomes active and hides when the active buffer is not Markdown; closing it hides
it for the buffer being read, and the next buffer activation brings it back (as
Fresh remounts its plugin section). With `auto_open` off it appears only through
Show Outline, and stays available across buffer switches once shown.

## 6. Differences from Fresh and residual gaps

- gvim builds the section into the TUI; Fresh ships it as the bundled plugin
  `markdown_toc.ts`. That is why the plugin's Lua source was retired here: the
  widget API it would need is not part of gvim's plugin host. The config keys
  and behaviour stay the plugin's.
- Indentation is two cells per heading level (gvim's tree convention) instead of
  Fresh's one column.
- The selection is painted as a background band even when the section does not
  have the keyboard, with the `▌` marker for focus; Fresh's tree marks the
  selection with `▌` only.
- The palette commands are Show Outline / Close Outline (the names gvim's plugin
  used) rather than Fresh's single Toggle Table of Contents; both reach the same
  section.
- **Residual gap — `fold_buffer`**: gvim's editor folds are indentation folds,
  and a Markdown heading usually opens none, so folding a Contents row folds the
  editor's fold on that line only when the line happens to open one. Fresh
  publishes per-heading fold ranges; gvim's editor has no port for ranges from
  outside yet. The setting is read, plumbed and documented here.
- Residual gap — the sidebar itself cannot move to the right side or be
  resized to fewer than its minimum (the pre-existing explorer limits), and the
  section list is not persisted in the session.

## 7. Files and tests

| Area | Where |
|---|---|
| heading scan | `outline/` (`outline_test.go` table tests: levels, setext, fences, caps) |
| section geometry, headers, Contents widget | `internal/tui/sidebar/` (`layout_test.go`, `header_test.go`, `contents_test.go`) |
| frame fields and hit kinds | `internal/tui/frame/frame.go`, `hit.go` |
| body painting | `internal/tui/explorer/panel.go` (`DrawBody`; `Draw` unchanged) |
| root wiring: lifecycle, follow, keys, mouse, paint | `internal/tui/markdown_toc.go` and `markdown_toc_test.go` |
| focus cycle | `internal/tui/root_sidebar.go` |
| palette rows | `commands/markdown_commands.go` |
| config wiring | `internal/app/markdown_toc.go` (plus `internal/app/plugins_markdown_toc_test.go`: the end-to-end tests) |

No test compares whole frames: the tests assert layout rectangles, hit targets,
widget state and a few painted cells.

## 8. Adding a section

1. Add a `sidebar.Kind` and a `HeaderFor` case (title, buttons) in
   `internal/tui/sidebar`.
2. Teach `sidebar.Layout` nothing: it already shares rows by `Spec.Rows`
   (0 = share the remainder) and folds under pressure.
3. Give the root a state value in `internal/tui/markdown_toc.go` style: a
   `sidebar.Spec` in `Model.sidebarSpecs`, a paint case in
   `Model.paintSidebar`, and a hit case in `Model.handleClick`.
4. Add it to `sidebarSections`/`sectionFocused`/`focusSection` if it takes the
   keyboard, and to `stageContents`-style stages if F6 should reach it.
5. Record it here and in `docs/ui.md`'s component map.
