package sidebar

import (
	"slices"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/icons"
	"github.com/tbxark/gvim/internal/tui/space"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/outline"
)

// The Contents section's settings, as plugins.markdown_toc.settings writes them.
// The defaults are Fresh's markdown_toc defaults.
const (
	// DefaultRows is the body rows the section asks for out of the box.
	DefaultRows = 10
	// MaxRows bounds the rows a config or a divider drag can ask for.
	MaxRows = 200
	// DefaultMaxDepth is the deepest heading level the outline lists.
	DefaultMaxDepth = 6
)

// Follow names what the selected row follows.
type Follow int

const (
	// FollowCursor selects the heading the caret is under. It is the default.
	FollowCursor Follow = iota
	// FollowScroll selects the heading at the top of the pane.
	FollowScroll
)

// String is the setting's name.
func (f Follow) String() string {
	if f == FollowScroll {
		return "scroll"
	}
	return "cursor"
}

// ParseFollow reads the setting: "cursor" or "scroll". An unknown name is
// cursor, the default.
func ParseFollow(name string) Follow {
	if name == "scroll" {
		return FollowScroll
	}
	return FollowCursor
}

// Settings are the Contents section's settings.
type Settings struct {
	// MaxDepth is the deepest heading level listed, 1 to 6. Deeper headings
	// are not rows of the tree.
	MaxDepth int
	// Follow is which heading the selected row follows.
	Follow Follow
	// FoldBuffer asks that folding a heading in the outline folds its section
	// in the editor too. gvim's folds are indentation folds and a Markdown
	// heading usually opens none, so this is plumbed but has little effect yet;
	// docs/sidebar.md records that gap.
	FoldBuffer bool
	// Rows is the body rows the section asks for in the column.
	Rows int
	// AutoOpen shows the section for an active Markdown buffer and hides it
	// for any other.
	AutoOpen bool
}

// DefaultSettings returns Fresh's defaults: depth 6, follow the cursor, ten
// rows, opened automatically.
func DefaultSettings() Settings {
	return Settings{MaxDepth: DefaultMaxDepth, Follow: FollowCursor, Rows: DefaultRows, AutoOpen: true}
}

// ParseSettings reads the plugins.markdown_toc.settings object. A missing or
// invalid key keeps the default; an out-of-range number is clamped to its
// bounds. The numbers are the float64 a JSON decoder produces, and plain ints.
func ParseSettings(raw map[string]any) Settings {
	out := DefaultSettings()
	for key, v := range raw {
		switch key {
		case "max_depth":
			if n, ok := whole(v); ok {
				out.MaxDepth = min(max(n, 1), DefaultMaxDepth)
			}
		case "follow":
			if s, ok := v.(string); ok {
				out.Follow = ParseFollow(s)
			}
		case "fold_buffer":
			if b, ok := v.(bool); ok {
				out.FoldBuffer = b
			}
		case "rows":
			if n, ok := whole(v); ok {
				out.Rows = min(max(n, 1), MaxRows)
			}
		case "auto_open":
			if b, ok := v.(bool); ok {
				out.AutoOpen = b
			}
		}
	}
	return out
}

// whole reads a whole number from a decoded setting: JSON gives a float64, a
// test or a config set directly may give an int.
func whole(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		if n != float64(int(n)) {
			return 0, false
		}
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	}
	return 0, false
}

// entry is one visible row of the tree: a heading, its depth in the tree and
// the state of its fold.
type entry struct {
	heading  outline.Heading
	depth    int
	children bool
	expanded bool
}

// Row is one visible row of the tree as the hit map and the painter need it.
type Row struct {
	Heading outline.Heading
	// Depth is the row's indentation level: 0 for a level-1 heading, 1 for a
	// level-2 heading, and so on.
	Depth int
	// HasChildren reports that the heading has deeper headings after it, so it
	// wears a fold glyph.
	HasChildren bool
	// Expanded reports that its children are listed.
	Expanded bool
	// GlyphX is the frame column of the fold glyph; the title starts two
	// cells to its right.
	GlyphX int
}

// followKey records that Follow has run once, so the first call derives the
// selection and later ones compare their sources.
type followKey struct{ valid bool }

// Contents is the Contents section: the headings of the active Markdown buffer
// as a foldable tree, with the selected row following the caret (or the top of
// the pane). The zero value is a closed section with no rows; NewContents
// starts one from settings.
type Contents struct {
	st        Settings
	open      bool
	collapsed bool
	focus     bool

	headings            []outline.Heading
	entries             []entry
	expanded            map[int]bool // fold state keyed by the heading's byte offset
	selected            int          // position in entries, -1 when none
	scroll              int          // first visible position
	viewRows            int          // body rows the layout last gave the section
	stale               bool         // the document did not fit the scan bound
	followed            followKey
	lastCursor, lastTop int
	// FoldHook, when set, is called when a heading's fold changes, with the
	// heading and whether it is now expanded. The root uses it for
	// fold_buffer.
	FoldHook func(outline.Heading, bool)
}

// NewContents returns a Contents section for settings.
func NewContents(st Settings) Contents {
	return Contents{st: st, selected: -1}
}

// Open reports whether the section is part of the column.
func (c *Contents) Open() bool { return c.open }

// SetOpen adds or removes the section from the column. Closing forgets the
// selection's window but keeps the scan, so a re-open is immediate.
func (c *Contents) SetOpen(open bool) {
	if c.open == open {
		return
	}
	c.open = open
	if !open {
		c.focus = false
	}
}

// Headings returns the headings of the current scan, in line order. The root
// uses them to publish the section's fold ranges. The slice is a copy; a later
// scan replaces it.
func (c *Contents) Headings() []outline.Heading { return slices.Clone(c.headings) }

// Collapsed reports whether the reader folded the section into its header row.
func (c *Contents) Collapsed() bool { return c.collapsed }

// SetCollapsed folds or unfolds the section.
func (c *Contents) SetCollapsed(v bool) { c.collapsed = v }

// ToggleCollapsed folds the section or unfolds it.
func (c *Contents) ToggleCollapsed() { c.collapsed = !c.collapsed }

// Focused reports whether the section holds the keyboard.
func (c *Contents) Focused() bool { return c.focus }

// SetFocus gives the section the keyboard or takes it away.
func (c *Contents) SetFocus(v bool) { c.focus = v }

// Settings returns the section's settings.
func (c *Contents) Settings() Settings { return c.st }

// SetSettings replaces the settings. The visible tree is rebuilt when the depth
// limit changes, and the requested rows are taken as the new value.
func (c *Contents) SetSettings(st Settings) {
	c.st = st
	c.rebuild()
}

// Rows is the body rows the section asks for: the configured rows, or the value
// a divider drag chose for this session.
func (c *Contents) Rows() int {
	if c.st.Rows <= 0 {
		return DefaultRows
	}
	return min(c.st.Rows, MaxRows)
}

// SetRows asks for n body rows, clamped to 1..MaxRows. A drag on the section's
// header sets it.
func (c *Contents) SetRows(n int) {
	c.st.Rows = min(max(n, 1), MaxRows)
}

// AutoOpen reports whether the section opens by itself for a Markdown buffer.
func (c *Contents) AutoOpen() bool { return c.st.AutoOpen }

// FollowMode returns the effective follow mode.
func (c *Contents) FollowMode() Follow {
	if c.st.Follow == FollowScroll {
		return FollowScroll
	}
	return FollowCursor
}

// SetFollow changes the follow mode for the session and makes the next Follow
// re-derive the selection from its source.
func (c *Contents) SetFollow(f Follow) {
	c.st.Follow = f
	c.followed = followKey{}
}

// FoldBuffer reports whether folding a heading folds its section in the buffer.
func (c *Contents) FoldBuffer() bool { return c.st.FoldBuffer }

// Stale reports that the active document did not fit the scan bound, so the
// rows shown are the last ones scanned (or none).
func (c *Contents) Stale() bool { return c.stale }

// Len is the number of visible rows.
func (c *Contents) Len() int { return len(c.entries) }

// SelectedPos is the position in the visible rows of the selection, or -1.
func (c *Contents) SelectedPos() int { return c.selected }

// Scroll is the first visible position.
func (c *Contents) Scroll() int { return c.scroll }

// ViewRows is the body rows the section last had.
func (c *Contents) ViewRows() int { return c.viewRows }

// SetViewRows records the body rows the layout gave the section. It is the page
// size of Page and the window of Draw.
func (c *Contents) SetViewRows(n int) {
	c.viewRows = max(n, 0)
	c.reveal()
}

// Reset forgets the scan: another buffer's outline must never show for this
// one. The fold state and the window go with it.
func (c *Contents) Reset() {
	c.headings, c.entries = nil, nil
	c.expanded = nil
	c.selected, c.scroll = -1, 0
	c.stale = false
	c.followed = followKey{}
}

// Scan reads doc and rebuilds the tree. It returns false when doc is over the
// scan bound; the section keeps the rows it has and reports Stale.
func (c *Contents) Scan(doc outline.Lines) bool {
	if doc == nil {
		c.Reset()
		return true
	}
	heads, complete := outline.ScanDoc(doc, outline.MaxScanBytes)
	if !complete {
		c.stale = true
		return false
	}
	c.stale = false
	c.headings = heads
	// Expansion survives a rescan by key. A key is a byte offset, so an edit
	// above a heading renames it and it comes back expanded, as a new heading
	// does; headings below the edit keep what the reader chose.
	expanded := make(map[int]bool, len(heads))
	for _, h := range heads {
		expanded[h.Byte] = true
		if v, ok := c.expanded[h.Byte]; ok {
			expanded[h.Byte] = v
		}
	}
	c.expanded = expanded
	c.rebuild()
	c.followed = followKey{}
	return true
}

// rebuild recomputes the visible rows from the scanned headings and the fold
// state, keeping the selection on the heading it was on when it is still
// visible.
func (c *Contents) rebuild() {
	selectedByte := -1
	if c.selected >= 0 && c.selected < len(c.entries) {
		selectedByte = c.entries[c.selected].heading.Byte
	}
	c.entries = c.entries[:0]
	if c.expanded == nil {
		c.expanded = map[int]bool{}
	}
	maxDepth := c.st.MaxDepth
	if maxDepth <= 0 || maxDepth > DefaultMaxDepth {
		maxDepth = DefaultMaxDepth
	}
	// The headings the depth limit lists, in line order.
	kept := make([]int, 0, len(c.headings))
	for i, h := range c.headings {
		if h.Level <= maxDepth {
			kept = append(kept, i)
		}
	}
	children := make([]bool, len(kept))
	for k := range kept {
		children[k] = k+1 < len(kept) && c.headings[kept[k+1]].Level > c.headings[kept[k]].Level
	}
	hidden := 0
	for k, hi := range kept {
		h := c.headings[hi]
		if hidden > 0 {
			if h.Level > hidden {
				continue
			}
			hidden = 0
		}
		expanded := c.expanded[h.Byte]
		c.entries = append(c.entries, entry{heading: h, depth: h.Level - 1, children: children[k], expanded: expanded})
		if children[k] && !expanded {
			hidden = h.Level
		}
	}
	c.selected = -1
	for pos := range c.entries {
		if c.entries[pos].heading.Byte == selectedByte {
			c.selected = pos
			break
		}
	}
	if c.selected < 0 && len(c.entries) > 0 {
		c.selected = 0
	}
	c.reveal()
}

// Selected returns the selected heading.
func (c *Contents) Selected() (outline.Heading, bool) {
	if c.selected < 0 || c.selected >= len(c.entries) {
		return outline.Heading{}, false
	}
	return c.entries[c.selected].heading, true
}

// SelectedLine returns the zero-based line of the selected heading.
func (c *Contents) SelectedLine() (int, bool) {
	h, ok := c.Selected()
	return h.Line, ok
}

// RowAt returns the row bodyOffset rows below the window's top, as the hit map
// reports a press on the section's body.
func (c *Contents) RowAt(bodyOffset int) (Row, bool) {
	pos := c.scroll + bodyOffset
	if pos < 0 || pos >= len(c.entries) {
		return Row{}, false
	}
	return c.row(pos), true
}

// row converts an entry to the row a hit test and the painter read.
func (c *Contents) row(pos int) Row {
	e := c.entries[pos]
	return Row{
		Heading:     e.heading,
		Depth:       e.depth,
		HasChildren: e.children,
		Expanded:    e.expanded,
		GlyphX:      1 + space.PadX + 2*e.depth,
	}
}

// SelectPos selects the visible row at pos and scrolls it into the window.
func (c *Contents) SelectPos(pos int) {
	if len(c.entries) == 0 {
		return
	}
	c.selected = min(max(pos, 0), len(c.entries)-1)
	c.reveal()
}

// Move moves the selection by delta rows.
func (c *Contents) Move(delta int) {
	if len(c.entries) == 0 {
		return
	}
	if c.selected < 0 {
		c.selected = 0
	} else {
		c.selected += delta
	}
	c.selected = min(max(c.selected, 0), len(c.entries)-1)
	c.reveal()
}

// Page moves the selection by delta pages, a page being the body rows.
func (c *Contents) Page(delta int) {
	rows := max(c.viewRows, 1)
	c.Move(delta * rows)
}

// First selects the first visible row.
func (c *Contents) First() {
	c.SelectPos(0)
}

// Last selects the last visible row.
func (c *Contents) Last() {
	c.SelectPos(len(c.entries) - 1)
}

// Expand unfolds the selected heading, or moves to its first child when it is
// already unfolded.
func (c *Contents) Expand() {
	if c.selected < 0 || c.selected >= len(c.entries) {
		return
	}
	e := c.entries[c.selected]
	if !e.children {
		return
	}
	if !e.expanded {
		c.ToggleFoldPos(c.selected)
		return
	}
	c.SelectPos(c.selected + 1)
}

// Collapse folds the selected heading, or selects its parent when it is not a
// fold: the movement the explorer also gives Left.
func (c *Contents) Collapse() {
	if c.selected < 0 || c.selected >= len(c.entries) {
		return
	}
	e := c.entries[c.selected]
	if e.children && e.expanded {
		c.ToggleFoldPos(c.selected)
		return
	}
	for pos := c.selected - 1; pos >= 0; pos-- {
		if c.entries[pos].heading.Level < e.heading.Level {
			c.SelectPos(pos)
			return
		}
	}
}

// ToggleFoldPos folds or unfolds the row at pos. It reports whether the row is
// a heading with children, so the caller knows a fold changed.
func (c *Contents) ToggleFoldPos(pos int) bool {
	if pos < 0 || pos >= len(c.entries) {
		return false
	}
	e := c.entries[pos]
	if !e.children {
		return false
	}
	if c.expanded == nil {
		c.expanded = map[int]bool{}
	}
	c.expanded[e.heading.Byte] = !e.expanded
	c.rebuildKeep(pos)
	if c.FoldHook != nil {
		c.FoldHook(e.heading, !e.expanded)
	}
	return true
}

// rebuildKeep rebuilds the tree after a fold, keeping the selection on the row
// the reader acted on: the heading of the entry at pos.
func (c *Contents) rebuildKeep(pos int) {
	byteKey := -1
	if pos >= 0 && pos < len(c.entries) {
		byteKey = c.entries[pos].heading.Byte
	}
	c.rebuild()
	// rebuild falls back to the first row when the old selection is gone; put
	// it back on the heading the reader folded.
	if byteKey < 0 {
		return
	}
	for at := range c.entries {
		if c.entries[at].heading.Byte == byteKey {
			c.selected = at
			c.reveal()
			return
		}
	}
}

// Wheel scrolls the window by delta rows without moving the selection.
func (c *Contents) Wheel(delta int) {
	if len(c.entries) == 0 {
		return
	}
	c.scroll += delta
	c.clampScroll()
}

// Follow selects the heading that contains the caret line, or the one at the
// top of the pane in scroll mode and while the editor pane does not have the
// keyboard. It re-derives the selection only when its source moved, so browsing
// the tree with the arrow keys is not undone by the next frame, and entering
// the section (a focus change, not a scroll) keeps the row the reader chose.
func (c *Contents) Follow(cursorLine, topLine int, editorFocused bool) {
	cursorMoved := !c.followed.valid || cursorLine != c.lastCursor
	topMoved := !c.followed.valid || topLine != c.lastTop
	c.lastCursor, c.lastTop = cursorLine, topLine
	c.followed = followKey{valid: true}
	derive := false
	line := cursorLine
	switch {
	case c.FollowMode() == FollowScroll:
		derive, line = topMoved, topLine
	case !editorFocused:
		// The pane does not have the keyboard: what the reader looks at is its
		// top, not a caret they are not moving.
		derive, line = topMoved, topLine
	default:
		derive = cursorMoved
	}
	if !derive {
		return
	}
	if len(c.entries) == 0 {
		c.selected, c.scroll = -1, 0
		return
	}
	best := 0
	for pos := range c.entries {
		if c.entries[pos].heading.Line > line {
			break
		}
		best = pos
	}
	c.selected = best
	c.reveal()
}

// reveal scrolls the window so the selected row is inside it.
func (c *Contents) reveal() {
	if c.selected < 0 {
		return
	}
	rows := max(c.viewRows, 1)
	switch {
	case c.selected < c.scroll:
		c.scroll = c.selected
	case c.selected >= c.scroll+rows:
		c.scroll = c.selected - rows + 1
	}
	c.clampScroll()
}

// SetScroll sets the first visible row, clamped to the tree. A restored session
// state uses it after the first scan.
func (c *Contents) SetScroll(n int) {
	c.scroll = n
	c.clampScroll()
}

// clampScroll keeps the window over the rows.
func (c *Contents) clampScroll() {
	if c.scroll < 0 {
		c.scroll = 0
	}
	if rows := len(c.entries); c.scroll > rows {
		c.scroll = rows
	}
}

// Draw paints the section's body into its place in the column, which starts at
// column x and is width cells wide: the walls, the visible rows and the
// selection. A section with no rows paints nothing.
func (c *Contents) Draw(dst *grid.Grid, x, width int, sec Section, ic icons.Mode, st style.Styles) {
	rows := sec.Rows()
	c.viewRows = rows
	if width < 2 || rows <= 0 {
		return
	}
	right := x + width
	body := style.TaggedCell(st.Explorer, "editor", "fg", "editor", "bg")
	sel := style.TaggedCell(st.Selection, "editor", "fg", "editor", "selection_bg")
	accent := style.Cell(st.Accent).BG
	for y := sec.BodyTop; y <= sec.BodyBottom; y++ {
		dst.Fill(x, right, y, body)
		dst.Set(x, y, "│", body)
		dst.Set(right-1, y, "│", body)
	}
	c.clampScroll()
	if len(c.entries) == 0 {
		msg, faint := "(no headings)", true
		if c.stale {
			msg = "(the file is too large for an outline)"
		}
		style := body
		if faint {
			style.Faint = true
		}
		dst.Put(x+1+space.PadX, sec.BodyTop, right-space.PadX, msg, style)
		return
	}
	end := right - 1 - space.PadX
	for off := 0; off < rows; off++ {
		pos := c.scroll + off
		if pos >= len(c.entries) {
			break
		}
		e := c.entries[pos]
		y := sec.BodyTop + off
		rowStyle := body
		selected := pos == c.selected
		if selected {
			rowStyle = sel
			dst.Fill(x+1, right-1, y, rowStyle)
		}
		if selected && c.focus {
			bar := rowStyle
			bar.FG = accent
			dst.Set(x+1, y, "▌", bar)
		}
		glyphX := x + 1 + space.PadX + 2*e.depth
		nameX := glyphX + 2
		glyph := " "
		if e.children {
			glyph = ic.Glyph(icons.DirExpanded)
			if !e.expanded {
				glyph = ic.Glyph(icons.DirCollapsed)
			}
		}
		if glyph != " " && glyphX < end {
			dst.Set(glyphX, y, glyph, rowStyle)
		}
		dst.Put(nameX, y, end, ansi.Truncate(e.heading.Title, max(end-nameX, 0), "…"), rowStyle)
	}
}
