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

Top and bottom border rows are display rows that no source line owns. The shared
page-view extra-row hook and the plugin decoration row transform that would own
the only row-insertion model did not land in round 7 (deferred to round 8 with
plugin tier 1c-B/C; the agreed shape is ExtraRow/ExtraRows plus a Pane-level
extra-row builder that can see the frame's measure), so the top and bottom
borders are a documented residual until that hook exists; the mid border is a
source row and is already drawn. Cells that need
more than one visual row (a wrapped cell) are also residual: the
single-visual-row cells are the tested contract.

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

## Deferred

- table top/bottom border rows: they are display rows no source line owns and
  wait for the shared page-view extra-row hook (internal/tui/editorpane/rows_extra.go,
  deferred to round 8 with plugin tier 1c-B/C); the mid border is drawn from the
  source separator row;
- table cells that wrap over several visual rows (single-visual-row cells are
  drawn), cells with escaped pipes, and tables in nested containers;
- fence body lines whose continuation rows would need their own rails (the first
  visual row is framed), and fences nested inside blockquotes or lists;
- heading scrollbar markers and the dual/rendered view (the latter needs buffer
  groups from plugin tier 1b);
- task-list glyph conceal (the box stays visible as text), image conceal and
  inline HTML conceal;
- heading marker conceal and the rest of Fresh's heading polish;
- session persistence of the page-view mode and the per-buffer page width
  (session.FileState carries no view fields yet);
- per-split view mode: gvim keeps one view mode per document, Fresh per split;
- mapped occurrences, rulers, indent guides and whitespace marks in the page view,
  and composed plugin overlays;
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
`internal/tui/pageview_test.go` (Markdown detection, buffer override resolution,
large-file fallback, pane wiring), `editor/markdown_pageview_test.go` (per-buffer
toggle and width, status text, copy uses source markers, virtual space off), and
the existing `syntax` markdown tests for the marker iterator plus
`TestMarkdownFencesClassifies` and the table classifier tests.
