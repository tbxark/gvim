package searchpanel

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/space"
)

// The panel's labels and shared column widths.
const (
	searchLabelText  = "Search:  "
	replaceLabelText = "Replace: "
	globLabelText    = "Files:   "
	// labelCols is the width of the label column: both labels and their gap, so the two
	// inputs start in the same cell.
	labelCols = 9
	// fileIndent is the indent of a match row below its file header.
	fileIndent = "  "
	// optionGap is the blank cells between two option labels.
	optionGap = 2
	// emptyText and searchingText are what the result region says before and while it
	// has rows.
	emptyText     = "Type a search pattern"
	searchingText = "Searching…"
	noMatchesText = "No matches found"
)

// geometry is the placement of the panel in a frame of w by h cells: the card borders,
// the result rows, the two field rows (one in Git Grep) and the option row.
type geometry struct {
	left, right int
	top, bottom int

	resultsTop, results int
	searchY, replY      int
	globY               int
	optY                int

	x0, end int // the inner text columns: from the padding to the right padding
}

// layoutFor places the panel in a w by h frame. The card docks to the last row; it keeps
// the menu bar and the tab strip (rows 0 and 1) visible, and a frame with no room for
// that returns the zero geometry, which paints nothing.
func layoutFor(w, h int, git bool) geometry {
	rows := clamp(h/2, 4, 18)
	total := func(r int) int {
		if git {
			return r + 4
		}
		return r + 6
	}
	for rows > 1 && h-total(rows) < 2 {
		rows--
	}
	if h-total(rows) < 2 {
		return geometry{}
	}
	top := h - total(rows)
	m := space.Margin(w)
	left, right := m, w-1-m
	g := geometry{left: left, right: right, top: top, bottom: h - 1, results: rows}
	g.resultsTop = top + 1
	g.optY = g.bottom - 1
	if git {
		g.replY, g.globY = -1, -1
		g.searchY = g.optY - 1
	} else {
		g.globY = g.optY - 1
		g.replY = g.globY - 1
		g.searchY = g.replY - 1
	}
	g.x0 = left + 1 + space.PadX
	g.end = max(right-space.PadX, g.x0)
	return g
}

// Covers reports whether the point (x, y) of a w by h frame is on the panel card.
func (p *Panel) Covers(x, y, w, h int) bool {
	geo := layoutFor(w, h, p.git)
	if geo.top < 0 {
		return false
	}
	return x > geo.left && x < geo.right && y >= geo.top && y <= geo.bottom
}

// Draw paints the panel over the last rows of g. The card is the title border, the
// result rows, the search row, the replacement row (Search and Replace only), the option
// row, and the hints on the bottom border.
func (p *Panel) Draw(g *grid.Grid) {
	geo := layoutFor(g.Width(), g.Height(), p.git)
	if geo.top < 0 || geo.right <= geo.left+1 {
		return
	}
	p.width, p.height = g.Width(), g.Height()
	boxW, boxH := geo.right-geo.left+1, geo.bottom-geo.top+1
	p.styles.ov.PaintBox(g, geo.left, geo.top, boxW, boxH, p.title())
	p.paintResults(g, geo)
	p.drawField(g, geo.searchY, searchLabelText, &p.query, p.focus == focusSearch, geo)
	if geo.replY >= 0 {
		p.drawField(g, geo.replY, replaceLabelText, &p.replace, p.focus == focusReplace, geo)
	}
	if geo.globY >= 0 {
		p.drawField(g, geo.globY, globLabelText, &p.glob, p.focus == focusGlob, geo)
	}
	p.paintOptions(g, geo)
	p.styles.ov.PaintFooter(g, geo.left, geo.bottom, boxW, p.hint())
}

// title is the card's title: the panel's name and, after a search, the count.
func (p *Panel) title() string {
	base := "Search & Replace"
	if p.git {
		base = "Git Grep"
	}
	if p.query.Value() == "" {
		return base
	}
	if p.loading {
		return base + " · searching…"
	}
	if p.errText != "" {
		return base + " · error"
	}
	matches, files, truncated := p.Stats()
	if matches == 0 {
		return base + " · no matches"
	}
	title := fmt.Sprintf("%s · %d %s in %d %s", base, matches, plural(matches, "match"), files, plural(files, "file"))
	if selected, _ := p.ReplaceCount(); selected != matches {
		title += fmt.Sprintf(" · %d selected", selected)
	}
	if truncated {
		title += " · capped"
	}
	if p.skippedCount > 0 {
		title += fmt.Sprintf(" · %d skipped", p.skippedCount)
		if details := strings.Join(p.skipped, ", "); details != "" {
			title += " (" + details + ")"
		}
	}
	return title
}

// hint is the text on the bottom border: the notice while one is set, otherwise the
// keys the panel answers.
func (p *Panel) hint() string {
	if p.notice != "" {
		return p.notice
	}
	if p.git {
		return "Alt+C case · Enter open · Alt+Q list · Esc close"
	}
	return "Tab next · Space check · Enter open · Alt+Enter all · Shift+Enter scoped · Alt+Q list · Esc close"
}

// paintResults paints the result region: the empty and error states, or the window of
// file and match rows around the selection.
func (p *Panel) paintResults(g *grid.Grid, geo geometry) {
	if geo.results <= 0 {
		return
	}
	write := func(text string, st grid.Style) {
		g.Put(geo.x0, geo.resultsTop, geo.end, text, st)
	}
	switch {
	case p.errText != "":
		write("Error: "+p.errText, p.styles.label)
		return
	case p.loading && len(p.rows) == 0:
		write(searchingText, p.styles.label)
		return
	case p.query.Value() == "":
		write(emptyText, p.styles.label)
		return
	case len(p.rows) == 0:
		write(noMatchesText, p.styles.label)
		return
	}
	y := geo.resultsTop
	for k := 0; k < geo.results; k++ {
		i := p.top + k
		if i >= len(p.rows) {
			break
		}
		p.paintRow(g, geo, y, i)
		y++
	}
}

// paintRow paints one result row: a file header with the folded mark and its count, or
// a match with its line and column and the matched runes in bold.
func (p *Panel) paintRow(g *grid.Grid, geo geometry, y, i int) {
	r := p.rows[i]
	selected := i == p.selected
	base := p.styles.file
	if selected {
		g.Fill(geo.left+1, geo.right, y, p.styles.selected)
		base = p.styles.selected
	}
	box := checkbox(p.fileChecked(r.file))
	if r.kind == rowFile {
		mark := fileOpenMark
		if r.file < len(p.expanded) && !p.expanded[r.file] {
			mark = fileClosedMark
		}
		base.Bold = true
		done := 0
		if r.file < len(p.checked) {
			for _, on := range p.checked[r.file] {
				if on {
					done++
				}
			}
		}
		g.Put(geo.x0, y, geo.end, fmt.Sprintf("%s%s%s (%d/%d)", box, mark, r.path, done, r.count), base)
		return
	}
	box = checkbox(p.matchChecked(r.file, r.match))
	prefix := fmt.Sprintf("%s%s%d:%d ", box, fileIndent, r.line, r.col)
	x := geo.x0
	x += g.Put(x, y, geo.end, prefix, faint(base))
	// The text, with the matched runes bold.
	runes := []rune(r.text)
	hlStart := min(max(r.hlStart, 0), len(runes))
	hlEnd := min(max(r.hlEnd, hlStart), len(runes))
	x += g.Put(x, y, geo.end, string(runes[:hlStart]), base)
	bold := base
	bold.Bold = true
	x += g.Put(x, y, geo.end, string(runes[hlStart:hlEnd]), bold)
	g.Put(x, y, geo.end, string(runes[hlEnd:]), base)
}

// drawField paints one labelled input row on y. The focused label takes the active
// style, the input is the raised field of the card, and its text scrolls so the caret
// stays in the room (see space.Window).
func (p *Panel) drawField(g *grid.Grid, y int, label string, f *textinput.Model, focused bool, geo geometry) {
	if y < 0 {
		return
	}
	ls := p.styles.label
	if focused {
		ls = p.styles.active
	}
	g.Put(geo.x0, y, geo.end, label, ls)
	x := geo.x0 + labelCols
	if x >= geo.end {
		return
	}
	g.Fill(x, geo.end, y, p.styles.field)
	shown, _ := space.Window(f.Value(), f.Position(), geo.end-x)
	g.Put(x, y, geo.end, shown, p.styles.field)
}

// paintOptions paints the option row: the scope and the match-mode checkboxes on the
// left, and the Replace All label of a search that can be replaced on the right.
func (p *Panel) paintOptions(g *grid.Grid, geo geometry) {
	if geo.optY < 0 {
		return
	}
	type opt struct {
		on   bool
		text string
	}
	var opts []opt
	if p.git {
		opts = []opt{{p.modes.CaseSensitive, checkbox(p.modes.CaseSensitive) + "Case (Alt+C)"}}
	} else {
		opts = []opt{
			{p.scope == AllFiles, checkbox(p.scope == AllFiles) + "All Files (Alt+A)"},
			{p.modes.CaseSensitive, checkbox(p.modes.CaseSensitive) + "Case (Alt+C)"},
			{p.modes.Regex, checkbox(p.modes.Regex) + "Regex (Alt+R)"},
			{p.modes.WholeWord, checkbox(p.modes.WholeWord) + "Whole (Alt+W)"},
		}
	}
	right := ""
	if !p.git && p.query.Value() != "" {
		if matches, _ := p.ReplaceCount(); matches > 0 {
			right = fmt.Sprintf("Replace All (Alt+Enter): %d", matches)
		} else {
			right = "Replace All (Alt+Enter)"
		}
	}
	rightW := ansi.StringWidth(right)
	limit := geo.end
	if rightW > 0 && rightW+2 < geo.end-geo.x0 {
		g.Put(geo.end-rightW, geo.optY, geo.end, right, p.styles.label)
		limit = geo.end - rightW - 2
	}
	x := geo.x0
	for _, o := range opts {
		text := o.text
		if x+ansi.StringWidth(text) > limit {
			break
		}
		st := p.styles.label
		if o.on {
			st = p.styles.file
		}
		x += g.Put(x, geo.optY, limit, text, st)
		x += optionGap
	}
}

// Cursor returns the caret position of the focused input in a frame of w by h cells.
func (p *Panel) Cursor(w, h int) (x, y int, ok bool) {
	geo := layoutFor(w, h, p.git)
	if geo.top < 0 {
		return 0, 0, false
	}
	var row int
	var f *textinput.Model
	switch p.focus {
	case focusSearch:
		row, f = geo.searchY, &p.query
	case focusReplace:
		if geo.replY < 0 {
			return 0, 0, false
		}
		row, f = geo.replY, &p.replace
	case focusGlob:
		if geo.globY < 0 {
			return 0, 0, false
		}
		row, f = geo.globY, &p.glob
	default:
		return 0, 0, false
	}
	start := geo.x0 + labelCols
	if start >= geo.end {
		return 0, 0, false
	}
	_, col := space.Window(f.Value(), f.Position(), geo.end-start)
	x = start + col
	if x >= w {
		return 0, 0, false
	}
	return x, row, true
}

// plural returns one or the plural form of word for n.
func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	for _, end := range []string{"ch", "sh", "x", "s"} {
		if strings.HasSuffix(word, end) {
			return word + "es"
		}
	}
	return word + "s"
}

// checkbox is the box before an option name.
func checkbox(on bool) string {
	if on {
		return "[x] "
	}
	return "[ ] "
}

// faint returns st with the faint attribute.
func faint(st grid.Style) grid.Style {
	st.Faint = true
	return st
}
