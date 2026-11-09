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
folding a heading hides its section of the tree, and with `fold_buffer` on it also
folds the heading's section in the buffer (§6). The selected
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
dropped and never reaches the buffer. The command cycle
`focus_next_sidebar_section` / `focus_prev_sidebar_section` follows the column's
**rendered order**: the explorer, Contents (when open), Diagnostics (when open; a
folded section is unfolded and takes the keyboard), each mounted plugin section, then
the editor; backwards walks the same stops. A focused plugin section takes the
keyboard through the provider's panel (its selected row/highlight is the panel's), and
leaving it (another stop, the editor, or F7) blurs the panel first, so its widget-panel
layer cannot swallow keys meant for the new focus. The default Alt+Shift+N / Alt+Shift+P
chords reach the keymap even while a plugin section has the keyboard: the panel's key
path passes the root's sidebar focus-cycle actions through (its Alt/Ctrl drop and a
plugin input mode/key capture do not eat them), so the real key leaves the panel and
moves to the next stop. **Blur scope:** a native focus move
clears only the widget-panel layer (`ps.focus`, the panel's focused state and the
provider's `focus_lost`); plugin input modes and `gvim.input.next_key` captures are
plugin-owned state and are not ended by a focus move (only the plugin's mode API, the
panic chords or unload end them). They cannot steal keys while a native chrome region
owns the keyboard — the plugin key layer is gated by editor key ownership — and they
own the editor again when it regains the keys. **F6/F7 (verified against Fresh):**
Fresh binds `F6` as the global Remote Indicator menu; gvim now does the same — F6 opens
a modal menu from the editor and every chrome focus scene (tab strip, explorer, Contents,
Diagnostics, dock, focused plugin section, open menu bar) that reports the **actual
workspace authority**: a local session says `Workspace: Local` and that this session has
no remote authority; a remote session says `Workspace: Remote`, shows the SSH authority
(`user@host`), its connected/reconnecting/disconnected state and reason, the remote
workspace root, and offers `Reconnect SSH` (the existing authority reconnect, run in a
command). It is a popup, not a focus move, so it leaves the current focus (a focused
plugin section stays focused). Fresh's
sidebar keyboard route is the `focus_next_sidebar_section` /
`focus_prev_sidebar_section` action (Alt+Shift+N/P), which walks the explorer and every
plugin section; gvim matches it. gvim's chrome focus cycle is a gvim extension and moved
to `F7`: native order menu → tab strip → explorer → Contents → Diagnostics (when open)
→ editor, never a plugin section. Plugin sections are reached with Alt+Shift+N/P; F7
from a focused plugin section clears its widget-panel layer and continues the native
order (a plugin mode or pending key capture remains plugin-owned and is gated while the
new stage owns keys).

**Mouse**: press a header row to focus the section; dragging any section's header
resizes that section's body (the dragged section and every other open section keep
at least one row), and a release that never moved folds it; the `×` closes it; the
wheel scrolls its window; a press on a row selects and jumps, a press on its glyph
folds it. The Explorer's top header grows it when dragged down; the other headers
grow their section when dragged up, the direction the Contents divider always had.

## 4. Settings

The config keys are Fresh's, under the bundled plugin's old name, so a Fresh
config and the previous gvim plugin config keep working. Only the user layer is
read (the project layer of a plugin's settings applies only to keys a plugin
declared as project-level, and markdown_toc declared none):

```jsonc
{
  // Native Contents keys: user layer only, applied live. A project file's
  // contents object (and plugins.markdown_toc.enabled=false) is accepted and
  // ignored: a repository cannot decide how the user reads an outline.
  "contents": {
    "fold_buffer": false,                    // also fold the buffer section on a fold
    "follow": "cursor",                      // "cursor" | "scroll": what the selection follows
    "auto_open": true,                       // show for Markdown buffers, hide otherwise
    "rows": 10                               // integer 1-200: body rows the section asks for
  },
  // Fresh's retired plugin path is still read as the fallback (user layer only).
  "plugins": {
    "markdown_toc": {
      "enabled": true,                       // the section's master switch (user layer only)
      "settings": {
        "max_depth": 6,                      // integer 1-6: deepest heading level listed
        "follow": "cursor",
        "foldBuffer": false,                 // Fresh names; fold_buffer also accepted
        "rows": 10,
        "autoOpen": true                     // auto_open also accepted
      }
    }
  }
}
```

| Key | Type | Default | Meaning |
|---|---|---|---|
| `enabled` | boolean | `true` | the section exists at all; off means no rows and the palette command says it is disabled |
| `max_depth` | integer 1-6 | `6` | headings deeper than this are not rows of the tree |
| `follow` | `"cursor"`/`"scroll"` | `"cursor"` | which heading the selected row follows; gvim's native key is `contents.follow` (user layer, applies live; the plugin path is the fallback and the native key wins) |
| `fold_buffer` | boolean | `false` | folding a heading also folds its section in the buffer, and unfolding opens it (see §6). gvim's native key is `contents.fold_buffer` (user layer, applies live); the Fresh-compatible `plugins.markdown_toc.settings.fold_buffer` still works, and the native key wins when both are set |
| `rows` | integer 1-200 | `10` | body rows the section asks for; a divider drag overrides it for the session, and a later live `contents.rows` edit resets the drag (the newer instruction). Native `contents.rows`, user layer, applies live |
| `auto_open` | boolean | `true` | show the section when a Markdown buffer becomes active, hide it when the buffer is not Markdown; native `contents.auto_open`, user layer, applies live (turning it on opens for the active Markdown buffer at once; off hides an auto-opened section but leaves a command-opened one) |

`rows`, the divider drag's value, the section's open/folded state and its window
scroll are saved in the session file (see §4b); without a saved session the
config value is used. The
`markdown_toc_toggle_follow` action flips `follow` for the session (a session
override that a later live config change does not clear; the configured value applies
until the command is used). All four native keys are Settings-page rows (section
Contents, user layer, applies live), shown with the effective value; writing them does
not touch the retired plugin's settings path. `rows` from a divider drag is saved in
the session and wins until a live `contents.rows` edit resets it.

## 4b. Session state and layout limits

The sidebar's UI state is saved with the session (`session.Sidebar`, beside the
explorer state) and restored only when the app restores the saved layout
(`editor.restore_previous_session` on, the session loaded, no `--no-restore`): a
hot-exit restore and a launch with explicit files keep the config defaults. The
saved state is bounded — booleans, numbers and the selected explorer path, no
file contents, diagnostics or plugin state:

| Saved | Restored as |
|---|---|
| the column's width form (automatic / percent / columns) and side | the runtime width and side, so a grip drag and a side toggle survive the session |
| the explorer section's fold, selected row path and scroll | the selection and window of the tree; the first frame does not reveal row 0 over it |
| the unified per-section heights (`SectionRows`: explorer, contents, diagnostics, plugin ids) — **only heights the reader explicitly set** by dragging/restoring, never a config default | each section's explicit body height at once; Contents 1..200, every other section (explorer, diagnostics, plugin ids) 1..1000; a section without an explicit height keeps the config default/flex remainder (Explorer by default). A present `SectionRows` map is authoritative: the legacy `ContentsRows`/`DiagnosticsRows`/`ExplorerRows`/`PluginRows` fields are read only when it is absent, so a stale legacy value cannot contradict it (and it is still written with explicit heights for older readers) |
| the Contents section's open/folded state and window scroll | the open flag applies to the first Markdown buffer of the restored session (it wins over `auto_open`) and the scroll after that buffer's first scan; a body height comes only from the explicit `SectionRows` entry |
| the Diagnostics section's open/folded state | at once; a body height comes only from the explicit `SectionRows` entry |
| the plugin sections' stack order, requested body rows and folded state (by plugin panel id) | when the sections mount: known ids take their rows/fold/order, unknown or absent ids keep the plugin's mount order and defaults; at most 64 sections, row counts clamped to 1000, no section content is stored |

Migration safety: the object is optional and every zero value is the default, so
a session file from before this state existed decodes with no sidebar state and
the config defaults win; the session format version does not change (an additive
optional object), and a reader that does not know it ignores it. Because the
session records only explicit heights, editing a config section height
(`contents.rows`, a plugin's requested rows) and restarting applies the new value
unless the previous session had a reader-dragged override for that section; the
capture path itself never applies a pending plugin restore (only the next sidebar
layout does), so taking a session snapshot is side-effect free. The plugin
sections' order, sizes and folds are part of this state (R9-4): their ids keep
the mount order and their requested rows/folds; restoring reorders the mounted
sections by the saved rank, applies the rows and folds to the ids that mount, and
leaves any section the plugin no longer mounts alone. The plugin host mounts
sections after the model is built, so the state is stashed and applied **lazily**:
a section that mounts later still gets its saved rows/fold/rank on the next sidebar
layout, and each id's rows/fold are applied once so a later drag is not overwritten.
The restore stays bounded (at most 64 ids, rows clamped to 1000).

**Layout limits and the right side.** The column is one width: the explorer, the
Contents section, the Diagnostics section and the plugin sections share
`file_explorer.width` (a percentage or a column count, the zero value as the
frame's automatic width). The grip on the column's inner border resizes it for
the session and the side toggle (`file_explorer.side`) moves the whole column to
the right edge, walls and close buttons mirrored; there is no separate Contents
width. A column too short for every open section folds the lower sections first
(the explorer keeps its header), and the Contents and Diagnostics headers can
still be dragged within that; there is no additional minimum beyond the frame's
explorer floor: the width form clamps to [16, 60] columns and the frame keeps 20
columns for the editor on a narrow frame, on either side. Every section's
body height is independent (the Explorer included); dragging a header resizes its
section from the absolute pointer delta and clamps both sides to one row, and the
height is saved in the session (below) so a restart restores it. The last open
section still flexes to fill the column, and a live config `contents.rows` change
resets that section's drag value. There is **one column width** — no per-section
widths — because that is Fresh's sidebar model too (the width grip spans the whole
column). Fresh also supports rows **or percent** per section; gvim stores rows only
(percent heights remain a residual). Fresh has **no drag-and-drop section order**
(it pins the explorer first and lists reordering as an unimplemented open question),
so gvim keeps the fixed explorer → Contents → Diagnostics → plugin order and does
not claim that as a Fresh gap.

## 5. Commands

The palette rows keep the retired plugin's names, so they are where they were:

| Palette row | Action | Behaviour |
|---|---|---|
| Markdown: Show Outline | `markdown_toc_show` | show the section for the active Markdown buffer and give it the keyboard; a text buffer says "Not a Markdown buffer". On an already open section it only reveals/focuses it: the reader's folds, scroll and selection survive |
| Markdown: Close Outline | `markdown_toc_close` | hide the section for the buffer being read |
| Markdown: Toggle Table of Contents | `markdown_toc_toggle` | close the section when open, otherwise open it (even with `auto_open` off) without taking the keyboard; a non-Markdown buffer reports "Not a Markdown buffer" |
| Markdown: Focus Contents | `markdown_toc_focus` | show the section for the active Markdown buffer and move the keyboard into it (the same reveal/focus, also usable when it is already open) |
| Markdown: Contents — Follow Cursor/Scroll | `markdown_toc_toggle_follow` | flip `follow` for the session (session override; a live config change does not clear it) |

The actions are in `commands/markdown_commands.go`, the hand-written table the
compare rows also use, because the section is not in the generated Fresh table.
They are ordinary ui actions, so a keybinding may name them.

Fresh's `markdown_toc` registers exactly Toggle Table of Contents, Follow
Cursor/Scroll and Focus Contents; gvim now has those three rows (the Toggle/Focus
names above), plus its own Show/Close Outline rows. There is **no fold command and no
menu row**: folding is the section's own disclosure gesture (a click on the row's
glyph, or Left/Right while the section has the keyboard). In scroll mode a jump to a
heading suppresses the first viewport change that still shows the target, so the
jump's own scroll does not move the selection to the heading at the pane top.

**Auto-open**: with `auto_open` on, the section appears when a Markdown buffer
becomes active and hides when the active buffer is not Markdown; closing it hides
it for the buffer being read, and the next buffer activation brings it back (as
Fresh remounts its plugin section). With `auto_open` off it appears only through
Show Outline / Toggle Table of Contents / Focus Contents, and stays available across
buffer switches once shown. The setting applies live through the Settings page.

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
- The palette commands match Fresh's three (`Markdown: Toggle Table of Contents`,
  `Contents — Follow Cursor/Scroll`, `Focus Contents`) and keep gvim's older
  Show/Close Outline rows as extras; all reach the same section.
- **`fold_buffer` is implemented** (round 6, R1-1; live since round 9), matching
  Fresh's `markdown_toc`: with the setting on, the section publishes every
  heading's section as a toggleable fold range at the scanned text version (so the
  standard fold keybinding sees them), nothing is pre-collapsed, and
  collapsing/expanding a row folds/unfolds that heading's range in the buffer.
  The ranges go through a separate outline fold source that is dropped when the
  text version changes, and the reader's folded headings are re-applied by heading
  byte offset after a rescan, so an edit never folds the wrong line. As in Fresh,
  a Markdown buffer stays tracked while the panel is hidden: the published ranges
  and the section-induced folds stay, edits still rescan (the same debounce), and
  a buffer switch or close clears them; folds the reader applied by hand in the
  buffer are not the section's and are left alone. The Settings row applies live:
  switching it on publishes at once, switching it off clears the outline source and
  opens the folds the section applied.
- Native sidebar state is persisted in the session (R1-2): the column's width
  form and side, the explorer fold/selection/scroll, the Contents section's
  open/folded state, rows and scroll, and the Diagnostics section's open/folded
  state and rows. Plugin sections, their order and their rows are plugin runtime
  state; R9-4 persists their stack order, requested rows and folded state in the
  sidebar session state (bounded, migration-safe); see §4b.

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
| fold_buffer ranges and lifecycle | `document/fold.go` (outline source, text-version guard), `editor/outline_folds.go`, `internal/uiadapter/outline_folds.go`, `internal/tui/markdown_toc.go` (publish/re-apply/clear), `internal/tui/sidebar/contents.go` (`Headings`); tests `editor/outline_folds_test.go`, `internal/tui/markdown_toc_fold_test.go` |
| native `contents.fold_buffer` | `config/contents.go` (parse, project layer ignored, effective value with the plugin-key fallback), `config/settings_schema.go` row (live), `internal/app/markdown_toc.go`; tests `config/contents_test.go` |
| sidebar session state | `session/sidebar.go`, `session.Session.Sidebar`, `internal/restore/sidebar.go`, `internal/tui/root.go` (`SidebarState`, `ExplorerState.Sidebar`), `internal/tui/sidebar_state.go`, `internal/tui/plugin_sidebar_state.go` (plugin sections' order/rows/folds, stashed until they mount), `internal/tui/markdown_toc.go` (pending Contents restore), `internal/app/run.go`; tests `session/sidebar_test.go`, `internal/restore/sidebar_test.go`, `internal/tui/sidebar_state_test.go`, `internal/tui/plugin_sidebar_state_test.go` |

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
   keyboard (plugin sections enter the command cycle this way; the F7 chrome cycle only
   visits the native stages, so add a `stageContents`-style stage only if F7 should reach it).
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
  Escape gives the keyboard back. `focus_next_sidebar_section` /
  `focus_prev_sidebar_section` include each mounted plugin section in rendered order after
  the native sections; leaving one for another stop, the editor or F7 raises the panel's
  `focus_lost` and clears its widget-panel layer first, so it cannot keep swallowing keys.
  Alt+Shift+N/P (the default focus-cycle chords) are passed through by the panel key
  path and by a plugin input mode/key capture, so the real key always reaches the root
  and leaves the panel; F7 likewise. F6 (the Remote Indicator menu) is a global popup and
  deliberately does not move or blur the focus.
  That is the only layer a native focus move clears: plugin input modes and
  `gvim.input.next_key` captures are plugin state. While the explorer, tab strip,
  Contents, Diagnostics, dock or welcome screen owns the keys, the plugin key layer is
  not consulted at all (so it cannot steal those keys); it owns the editor again when
  the editor regains the keys. The panic chords, the plugin's own mode API or unload
  is what ends a plugin mode.
* Lifecycle: the panel host owns the sections. Unmounting a plugin (or reloading it) removes
  its sections. Mounting shows the column when it was hidden.

**Design differences, not Fresh gaps:** plugin sections are not in the F7 chrome cycle.
Fresh's F6 is the Remote Indicator menu and its sidebar focus action is exactly
Alt+Shift+N/P, which gvim matches and which does include plugin sections; gvim's F7
cycle is its own native-chrome extension and deliberately never visits plugin sections.
Sections also cannot be reordered (Fresh has no drag-reorder either, and pins the
explorer first); their header drag-resizes like every other section, and the mount rows
are only the starting height.

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
