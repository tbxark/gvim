# Markdown view: page view and compose

This is the design record for the Markdown page view ("compose" in Fresh): the
centered page, the inline compose that hides the markup, and the mapping between
the source text and the composed screen. The page view is an inline view
transform of the ordinary editor pane, not a rendered second pane: the buffer
stays the source, and every edit, save, copy and LSP position uses source bytes.
Fresh's plugin `markdown_compose.ts` is the reference; the deferred parts are
listed at the end.

## Enabling and geometry

`toggle_page_view` turns the view on or off for one buffer (the per-document
`ViewSettings.PageView` override); the language's `page_view` is the default, so
the toggle can turn a configured default off. `set_page_width` sets the buffer's
`page_width`. The effective measure resolves, highest first:

1. the buffer's page width (the `set_page_width` value),
2. the language's `page_width` (`languages.<id>.page_width`, or the global
   `editor.page_width` where the language sets none; `0`/null means the text
   area),
3. the viewport's text area.

The page is centered: the left margin is `(text area - measure) / 2`, and the
measure never goes below 20 cells or above the text area. The gutter becomes the
margin — the line numbers, the change bar and the fold markers are hidden, while
the vertical scrollbar stays. The built-in wrap still runs, but at the page
measure (Fresh turns the plugin wrap on and the editor wrap off for the same
effect); the horizontal scrollbar is off. Virtual space and vertical virtual
lines are off (the page wraps its text, so there is no empty region past a line).
A large-file document keeps the normal pane: compose falls back like Fresh's
plugin threshold, and the page margins with it.

Non-Markdown documents still get the centered page and the wrapped measure, but
no compose: their text is shown as it is.

## Compose: what the page view hides

For a Markdown document the page view composes each line with
`syntax.MarkdownInlineMarkers`, a small iterator that reuses the markdown inline
reader's delimiters rather than moving the parser:

- **emphasis** and **strong**: the `*`/`_` runs are hidden and the text keeps the
  existing `emphasis`/`strong` syntax kinds (which carry the italic/bold
  attributes);
- **inline code**: the backtick runs are hidden, the text keeps `KindString`;
- **links and images**: the opening `[`, the closing `](...)` (destination
  included) or `][label]` are hidden, and the link text keeps `KindLink`
  (underlined);
- **strike-through**: the `~~` runs are hidden, the text keeps `KindStrike`;
- **thematic break**: the whole line becomes a rule of `measure - 2` cells
  (`─`, painted with the comment color, the separator color a later theme hook can
  take over);
- **blockquote**: each `>` becomes a colored bar (`│ `) at the quote color; nested
  quotes stack bars; the continuation rows hang under the quoted text;
- **lists**: the marker is kept as text (`-`, `1.` …) and the continuation rows
  hang under the item's content, clamped by the wrap rule's minimum continuation
  width;
- **fenced code**: the block is framed (see below), and its body is never read as
  Markdown: the code keeps the syntax colours of the language the fence names;
- **tables**: the rows are box-drawn (see below).

The hiding is Fresh's `unless-cursor-in` activation scope: the caret's line is
shown raw (delimiters visible), and so is the line above a caret at the start of
a line. No other input reveals the markers: they are a view transform, not a
hover decoration.

Block-level heading text keeps the existing heading kind; the `#` markers are not
concealed in this slice (deferred with the rest of the heading polish).

## Code-fence frames

A fenced code block is drawn as a box, Fresh's codeFrameWidth: the open delimiter
becomes a top border with the language label taken from the info string, the close
delimiter a bottom border, and every body line gets side rails and padding. The
frame is measure-4-indent cells wide (the same four cells Fresh reserves: two for
the rails, two so the right rail never wraps), it sits at the block's indent, and
the body keeps its syntax spans inside the rails, so tokens, numbers and comments
are coloured as in the source. Empty body lines still get both rails. The
classification is syntax.MarkdownFences, a separate pass over the lines that
reuses the tokenizer's fence rules (mdFence, closesFence); no parser is moved, and
the table is memoized in the layout memo by text version.

Activation follows the rest of the compose: the caret on an open or close
delimiter line (or at the start of the line below it) reveals the raw delimiter,
while the caret INSIDE a body line keeps the frame - the source-display map makes
its column exact, and jumping the frame away under the caret would be wrong. A
body line longer than the inner width wraps: its first visual row is framed and
the continuation rows have no rails (a documented residual of the
single-visual-row frame). A fence inside a blockquote or a list is classified by
the tokenizer the same way it always was; the page view frames only the
top-level forms it can classify, and the nested forms are left raw.

## Tables

A Markdown table is drawn as a box: each row's pipes become the frame's vertical
edges, the cells are padded and aligned to one width per column, and the source
separator row becomes the mid border. The alignment comes from the separator's
colon markers (left, right, center; the default is left). The classification is
syntax.MarkdownTables, a separate pass over the lines that reuses the tokenizer's
table rules (mdTableRule and the pipe paragraph rule), with the trimmed cell
ranges, the pipe columns and the alignments; no parser is moved.

Column widths are the natural widths of the RAW cell texts (so the caret's
revealed row always fits), one set per table, computed from ALL of its rows once
per text version and memoized - a scroll cannot change a width and an edit
recomputes it. When the table is wider than the page, the widths are shrunk
proportionally with a three-cell floor, as Fresh's distributeColumnWidths does.
The row budget is the measure less the pipes and the cell padding.

The caret's row is revealed raw: its cell contents show their markup, padded to
the same allocated widths, so the cells' borders do not jump as the caret moves.
Every other row's cell contents are inline-composed like the rest of the page
(markers concealed, syntax kinds kept). Copy, cut and every edit still use the
source bytes; the source-display map keeps the caret, the mouse hit and the
selection exact inside the rewritten cells (pipes and padding map to their source
boundaries).

Top and bottom border rows are display rows that no source line owns. They are
drawn through the shared page-view extra-row hook (`Pane.PageExtraRows`,
`editorpane/rows_extra.go`): the top border is inserted above the header line and
the bottom border below the table's last line, as width-dependent rows built from
the same table model and column widths the rows use. The hook owns every row
calculation, so the caret, the mouse hit (an above row maps to the line start, a
below row to its end), selection, scroll and the scrollbar treat the borders as
ordinary display rows; a page too narrow for the table draws no borders (its
rows wrap). The mid border stays the source separator row.

When the table is wider than the page and its columns are shrunk, a cell whose
visible content does not fit its column wraps: the primary row shows the first
segment of every cell with the exact source mapping, and the remaining segments
are emitted by the same extra-row hook as continuation rows between the row and
the bottom border (rows with rails on both sides, ordered nearest first). While
the caret is on that source row the continuation rows stand down and the raw row
is revealed, so the caret never sits on a non-source row; pressing a continuation
row maps to the line end, which reveals the row and collapses the continuations
(Fresh's row-level reveal semantics). Selection paints the primary row (the hook's
rows take no selection), which is a documented limitation of wrapped cells.

The column widths are keyed by (text version, page measure): the two panes of
the dual view have different measures and must not share one width set. A pipe
escaped as `\\|` is cell text and can never open a cell; the composed cell shows
the pipe without the escape while the caret's revealed row keeps the source text.
A table inside a blockquote or a list item is not classified as a top-level table:
those lines are rendered literal (the documented container fallback).

## Dual view: source and page side by side

Besides the per-document page view there is a dual view of the same Markdown
document: a native 50/50 side-by-side split with the source on the left and the
composed page on the right, the focus on the source (Fresh's dual mode, minus its
buffer-group tab). It needs one Markdown document, a single editor pane, and a
document that is not in large-file mode; a compare view must be closed first, and
each refusal has its own notice.

The document's own view settings stay the default for single panes: the dual view
is a derived per-pane override, not document state. `panePageView` gives the
preview leaf the composed page (the same page width resolution as the single-pane
page view) and the source leaf no page view; every other pane keeps the
document's settings. Because the override is derived, closing the dual view gives
the document's own mode back untouched, and the unfocused preview never reveals
the caret's line (the document caret belongs to the focused pane).

Opening uses the ordinary split: the focused leaf becomes a 50/50 side-by-side
inner node, the first child is the old pane (it keeps its window id) and the
second shows the same tab, then the focus goes to the source. Closing removes the
preview leaf, focuses the source and clears the state; `close_split` on either
pane, a tab change or opening a compare view end the dual view too — one validity
check in the per-update sync clears it, and the document is never touched. The
commands are hand-written palette rows: Markdown: Source, Rendered, Dual, Toggle
Dual and Cycle Mode ([^cmd]). Source turns the document's page view off (and
closes a dual view), Rendered turns it on (and closes one), Dual opens the dual
view, Toggle Dual opens it or returns to source, and Cycle Mode goes source ->
rendered -> dual -> source. Each says the mode in the status line; the mode is
derived (dual while a valid dual view, rendered while the document's page view is
on, source otherwise).

Both leaves show the same tab, so there is one document and one caret: editing in
the source pane updates the composed pane in the same frame; undo, save and LSP
see one document; and clicking the preview focuses it, so caret motion, mouse hit
and selection in the composed pane work through the compose mapping. The scroll
sync is line-anchored and compare-style: after every update the unfocused leaf
takes the focused document's top line (the preview starts at that line; the source
keeps its own wrapped row). Exact row/byte anchors across the two different
wraps, a native Markdown menu row (the menu table is generated from Fresh and its
plugin contributions are not hand-edited), persisting the split, more than one
dual view of a document, and the page-view width/session persistence remain
residuals.

[^cmd]: The palette rows are the same five actions in `commands/markdown_commands.go`;
the native palette therefore lists 229 commands.

## Headings

An ATX heading hides its leading hash run and the blanks after it, keeping the
existing heading kind (bold keyword styling) for the text; the caret line and the
line above a column-0 caret reveal the markers like the other compose conceals.
A line with a hash run that does not end in a blank (or the line end) is not a
heading and stays literal. In the page view the heading lines are also marked on
the vertical scrollbar track (one mark per heading line, coloured like a
heading), so their places in the file stay visible while composing; the marks are
memoized per text version.

## Mapping through concealed bytes and soft rows

The composition changes the visible runes, so the pane keeps two maps per
composed line: `srcToDisp` from a source rune column (the caret's units) to the
visible rune column, and `dispToSrc` back. Hidden delimiters map to the nearest
visible boundary, so a caret inside hidden markup snaps to the visible edge on a
concealed line.

- **Caret**: the buffer position stays the source position. The pane maps it to
  the composed column before laying out the row, so the terminal cursor and the
  software caret sit where the character shows. On the caret's own line nothing
  is concealed, so the mapping is exact there; motion and editing stay byte-based
  (moving right across `**x**` moves the source caret through the markers and the
  line reveals while it is the caret line).
- **Mouse hit**: the pane maps the clicked display column back through
  `dispToSrc` before returning the buffer position, so clicks on concealed lines
  land on the source text.
- **Selection**: cells carry their source rune column, so a selection over hidden
  delimiters paints the visible cells that belong to the selected source range.
  Copy, cut, paste and every editing action read the source text: copying a
  concealed `**bold**` yields the markers and all.
- **Soft rows**: wrapping runs on the composed runes, so the existing row
  arithmetic (caret row, reveal, folds, hit rows) keeps working; the hanging
  indent is the composed prefix width and its blank cells pad the continuation
  rows.
- **Search highlights and diagnostics**: their source rune ranges are mapped
  through `srcToDisp` before they are painted, so a match on text inside hidden
  markup marks the visible cells. Occurrences, rulers, indent guides and the
  whitespace line-end marks are off in the page view (they are source-column
  decorations; a mapped version is deferred). Plugin overlays are not composed.
- **Folds** stay line-granular and work; the fold placeholders are hidden with the
  gutter but a collapsed fold still shows its inline ` ...`.

The composed line is memoized in the document's layout memo keyed by line and
measure (it does not depend on the caret; the reveal variant is built on top), and
the fence table by the document's text version, so a repaint does not re-read the
document.

## Large files and non-Markdown

A large-file document (the editor's windowed large mode) is not composed and not
paged: it keeps the normal full-width pane, like Fresh's plugin threshold. A
document that is not Markdown gets the page geometry only.

## Persistence

The per-file page view is part of the session: `session.FileState` carries
`pageView` (nil when the file kept no override, so the language's `page_view`
decides) and `pageWidth` (zero: the language or viewport width). Both are
omitted when unset, so a session written by an older version loads with the
defaults and a reader that ignores them is unaffected; the writer takes the
values from the document's view settings and the restore applies them after the
file is opened. The dual split itself is not persisted (Fresh does not persist a
buffer group either); it is documented with the other dual residuals below.

## Deferred

- fence body lines whose continuation rows would need their own rails (the first
  visual row is framed; a long body line wraps without rails on its continuation
  rows), and fences nested inside blockquotes or lists (left literal);
- task-list glyph conceal (the box stays visible as text), image conceal and
  inline HTML conceal;
- session persistence of a dual split (the editor view per file is persisted,
  see below; the dual view itself is session-only, as Fresh does not persist a
  buffer group);
- per-split view mode: gvim keeps one view mode per document, Fresh per split; the
  dual view adds one derived per-pane override (source vs preview) but does not
  make the mode a free per-pane setting;
- dual view: exact row/byte scroll anchors across the two wraps, a native Markdown
  menu row (the generated menu table is not hand-edited), persisting the split,
  and more than one dual view of a document; the dual view is offered for a
  single-pane Markdown document only;
- mapped occurrences, rulers and indent guides in the page view: they stay
  deliberately off there (the page view owns its own visual language; a test
  asserts rulers and guides do not paint). Whitespace marks are per-cell and
  still show on the composed text. Composed plugin overlays order after the
  compose (plugin pipeline) and stay out of normal panes; a mapped ordering test
  is deferred with the plugin tier-1c remainder;
- heading polish beyond the marker conceal and the scrollbar marks (setext
  heading text styling).
- horizontal scroll inside a page wider than the viewport (the measure clamps to
  the text area instead).

## Tests

Cell-level tests (no goldens):
`internal/tui/editorpane/pageview_test.go` (centered margin, hidden gutter, wrap at
the measure, vertical scrollbar kept, caret past the page), `compose_test.go`
(delimiters hidden for emphasis/strong/code/link/strike, caret-line and
next-line reveal, rule and quote bar, list hanging indent, fenced body literal,
mouse hit through hidden markers, selection painting, non-Markdown unchanged),
`compose_fence_test.go` (frame cells, language label, syntax kinds inside the
rails, caret reveal open/close against body, indentation),
`compose_table_test.go` (box-drawing rows, alignment, wide CJK cells, per-row
reveal with the frame width kept, hit into cells, selection over cells, widths
recomputed when the text version changes, tables inside fences left literal),
`pageview_table_borders_test.go` (top and bottom border cells, presses on a
border mapping to the line start/end, the caret row shifted by the inserted rows,
no borders for a wrapped table or a normal pane),
`compose_table_wrap_test.go` (wrapped cells with continuation rows and rails,
the primary row's exact hit mapping, continuation rows standing down under the
caret, escaped pipes inside cells, and the widths keyed by measure),
`compose_heading_test.go` (marker conceal and caret reveal), the TUI-level
`pageview_heading_test.go` (heading scrollbar marks, memo invalidation) and
`pageview_table_border_test.go` (borders in order around the header, separator
and body rows of the whole frame), `compose_fence_wrap_test.go` (continuation
rails, the caret line keeping the whole body, a quoted fence staying literal),
`session/pageview_test.go` (old-session migration, omitted fields) and
`internal/restore/session_pageview_test.go` (save/restore round trip of the
page view and width, optional fields for a plain file),
`internal/tui/pageview_test.go` (Markdown detection, buffer override resolution,
large-file fallback, pane wiring), `editor/markdown_pageview_test.go` (per-buffer
toggle and width, status text, copy uses source markers, virtual space off),
`internal/tui/dual_test.go` (per-pane page-view resolution with the document
untouched, 50/50 geometry and source focus, source edit showing in the compose
pane, compose-pane caret/hit/selection, line-anchored scroll sync both ways,
close_split closing/clearing the dual view, stale dual clearing, the five mode
commands and their notices, and the not-Markdown/compare-conflict/single-pane/
large-file refusals), and the existing `syntax` markdown tests for the marker
iterator plus `TestMarkdownFencesClassifies` and the table classifier tests.
