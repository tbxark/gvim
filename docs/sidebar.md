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
| plugin sections (tier 1a) | `internal/tui/sidebar/sections.go` (the kind), `internal/tui/plugin_panels.go` (specs, paint, input, lifecycle), `plugin/panel_api_test.go` |
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

### Plugin sections (plugin API tier 1a)

A plugin can mount a declarative widget panel as a section of the column:
`gvim.ui.mount_sidebar_section(spec, opts)` (see
[plugins/api-ui.md](plugins/api-ui.md#widget-panels-and-sidebar-sections)). The contract:

* The section kind is `sidebar.KindPlugin` (value 100, reserved in
  `internal/tui/sidebar/sections.go`). `sidebar.Spec` and `sidebar.Section` carry two optional
  fields the native sections leave zero: `Title` (the header text; a plugin section names
  itself) and `NoClose` (a section mounted with `closable = false` has no ×). `sidebar.HeaderSpec`
  gained the same two fields, and `frame.setExplorer` passes them through, so painting and the
  hit map still agree.
* The root appends the mounted sections to `Model.sidebarSpecs` after the explorer and the
  Contents section, in mount order. `Rows` is the body height the plugin asked for (0 shares
  the column); the layout's squeeze and fold rules treat it like any section.
* Painting: `Model.paintSidebar` has one `KindPlugin` arm that fills the body cells with the
  overlay fill and draws the panel in the section rectangle (`Model.paintPluginSection`). The
  section's own header is painted by the shared `sidebar.PaintHeader`, so a plugin section
  looks like the native ones.
* Input: a click on the header folds or unfolds the section and focuses it; the × unmounts it
  and raises a `cancel` `widget_event`; a click in the body focuses the section and goes to the
  panel (a list row selects, a button activates, the wheel moves a list). A focused section
  takes the keyboard before the editor keymap and swallows the keys its widgets do not use;
  Escape gives the keyboard back.
* Lifecycle: the panel host owns the sections. Unmounting a plugin (or reloading it) removes
  its sections. Mounting shows the column when it was hidden.

Residual (v1): plugin sections are not in the F6 focus cycle, the header does not drag-resize
(rows come from the mount call), and sections cannot be reordered.

## 9. Explorer behaviours

The file explorer honours Fresh's `file_explorer` options (verified against
`refs/fresh/crates/fresh-editor-core/src/config.rs` and
`refs/fresh/docs/features/file-explorer.md`). gvim's config defaults are Fresh's.

| Key | Default | gvim |
|---|---|---|
| `file_explorer.width` | `"30%"` | a percentage of the frame or a column count; a plain number is a percentage, a float in 0..1 is the legacy fraction. The frame clamps the result to [16, 60] and keeps 20 columns for the editor on a narrow frame. A grip drag keeps the configured form: a percentage stays a percentage (the nearest of the bracketing values), a column count is exact. |
| `file_explorer.side` | `"left"` | "left" or "right". **Move File Explorer to Other Side** (`toggle_file_explorer_side`) flips it for the session and the status row says "File explorer moved to the left/right". Fresh persists the choice to the user config; gvim has no config writer yet, so the toggle is session-only (the config file still decides the next launch). |
| `file_explorer.follow_active_buffer` | false | reveal and select the active file in the tree when tabs change (or a preview tab is replaced). Off by default. It does nothing while the panel is hidden, while the keyboard is in the tree, or for a file outside the project, as Fresh's `follow_path_in_explorer`. |
| `file_explorer.auto_open_on_last_buffer_close` | true | close the last tab and the explorer opens and takes the keyboard; opening a buffer then gives the keyboard back to the editor (the panel stays visible). Fresh's behaviour: it does not close the panel when a buffer opens. |
| `file_explorer.compact_directories` | true | a folder that is expanded and whose only visible child is a folder has no row: the chain shares the deepest folder's row, drawn as `src/main/java` at the folded ancestors' depth. Expanding a folder also expands the single-child chain below it, so the chain stays one row; collapsing closes that row's own folder only. |
| sticky parents | always on | the run's first row's ancestors are pinned above it, up to 7 of them, never taking the whole body. The band is drawn on the gutter ground (faint when the theme has none), so it reads as pinned above the tree; a press on a pinned row selects that row. |
| `respect_gitignore`, `custom_ignore_patterns`, `preview_tabs`, `tree_indicator_collapsed`/"expanded" | — | accepted and ignored for now (documented gaps): gvim's explorer always respects .gitignore through its own reader and has its own preview-tab click behaviour and icon glyphs. |

**Reveal.** Showing the panel or focusing it (Ctrl+E) is the explicit "show me
where I am": the tree expands to the active file and selects it, and the status row
says why when it cannot (Fresh's strings): "File explorer focused; this buffer has
no file to reveal", or "File explorer focused; <name> is outside the project". A
follow is silent. Fresh has no palette "Reveal in File Explorer" command, so gvim
does not add one; the reveal happens on the toggle, the focus and the follow.

**Compact rows and clicks.** A compact row is one row: the indicator toggles the
deepest folder the row shows, a single press on a file previews it and a double
press opens it permanently, as any other row. Fresh also lets a press name one
segment of the chain (so it can be collapsed separately) and opens a context menu
per segment; gvim defers that nuance and documents it here.

**Side and width geometry.** The column keeps its own width against either frame
edge; the editor area (the tab strip, the welcome screen, the panes and the notice
band) is the rest of the frame, and the grip is the border facing the editor. The
zero-value `frame.Input` (no configured width, left side) is the layout gvim had
before these options existed, so tests and callers that set nothing are unchanged.
