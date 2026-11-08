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
- **fenced code**: the fence lines and their bodies are left literal (the table is
  computed from the source once per text version and memoized), so a code block is
  not composed.

The hiding is Fresh's `unless-cursor-in` activation scope: the caret's line is
shown raw (delimiters visible), and so is the line above a caret at the start of
a line. No other input reveals the markers: they are a view transform, not a
hover decoration.

Block-level heading text keeps the existing heading kind; the `#` markers are not
concealed in this slice (deferred with the rest of the heading polish).

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

- tables, code-fence frames with rails, heading scrollbar markers and the
  dual/rendered view (the latter needs buffer groups from plugin tier 1b);
- task-list glyph conceal (the `[ ]` stays visible as text), image conceal and
  inline HTML conceal;
- heading marker conceal and the rest of Fresh's heading polish;
- session persistence of the page-view mode and the per-buffer page width
  (`session.FileState` carries no view fields yet);
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
next-line reveal, rule and quote bar, list hanging indent, fenced code literal,
mouse hit through hidden markers, selection painting, non-Markdown unchanged),
`internal/tui/pageview_test.go` (Markdown detection, buffer override resolution,
large-file fallback, pane wiring), `editor/markdown_pageview_test.go` (per-buffer
toggle and width, status text, copy uses source markers, virtual space off), and
the existing `syntax` markdown tests for the marker iterator.
