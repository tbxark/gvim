package pluginpanel

import (
	"image/color"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/space"
	"github.com/tbxark/gvim/pluginapi"
)

// hitKind names what one interactive region of the last draw is.
type hitKind uint8

const (
	hitButton hitKind = iota
	hitToggle
	hitListRow
	hitTreeRow
	hitTreeDisclosure
)

// hit is one interactive region of the last draw, in frame cells.
type hit struct {
	r        Rect
	kind     hitKind
	focusIdx int    // index into the focus ring, -1 for a non-focusable region
	path     string // path of the widget the region belongs to
	index    int    // item row (list) or node index (tree)
	disabled bool
}

// hitAt returns the hit under the cell, the last drawn region first.
func (p *Panel) hitAt(x, y int) (hit, bool) {
	for i := len(p.hits) - 1; i >= 0; i-- {
		if p.hits[i].r.Contains(x, y) {
			return p.hits[i], true
		}
	}
	return hit{}, false
}

// Draw paints the panel into r on g and records its hit regions. A framed panel paints the
// shared overlay card and its body inside the border; an unframed one paints inside r
// directly (a sidebar section's body, or the pane of a virtual buffer).
func (p *Panel) Draw(g *grid.Grid, r Rect) {
	p.rect = r
	p.hits = p.hits[:0]
	x, y, w, h := r.X, r.Y, r.W, r.H
	if p.framed {
		if w < 4 || h < 2 {
			p.rect = Rect{}
			return
		}
		p.chrome.PaintBox(g, r.X, r.Y, r.W, r.H, p.title)
		for row := r.Y + 1; row < r.Y+r.H-1; row++ {
			g.Fill(r.X+1, r.X+r.W-1, row, p.chrome.Box)
		}
		x, y, w, h = r.X+1+space.PadX, r.Y+1, r.W-2-2*space.PadX, r.H-2
	}
	if w <= 0 || h <= 0 {
		return
	}
	pt := &painter{g: g, p: p}
	pt.node(p.spec.Root, x, y, w, h)
}

// painter paints one subtree and numbers the focusable widgets in depth-first order, the
// same order Panel.collect built the focus ring in.
type painter struct {
	g  *grid.Grid
	p  *Panel
	fi int
}

// node paints one widget in the rectangle.
func (pt *painter) node(w pluginapi.Widget, x, y, wd, h int) {
	if wd <= 0 || h <= 0 {
		return
	}
	switch w.Kind {
	case pluginapi.WidgetRow:
		pt.row(w, x, y, wd, h)
	case pluginapi.WidgetCol:
		pt.col(w, x, y, wd, h)
	case pluginapi.WidgetLabel:
		pt.label(w, x, y, wd)
	case pluginapi.WidgetText:
		pt.text(w, x, y, wd, h)
	case pluginapi.WidgetRaw:
		pt.raw(w, x, y, wd, h)
	case pluginapi.WidgetHintBar:
		pt.hintBar(w, x, y, wd)
	case pluginapi.WidgetSpacer:
		// Blank: the panel fill already shows.
	case pluginapi.WidgetDivider:
		pt.divider(w, x, y, wd)
	case pluginapi.WidgetButton:
		pt.button(w, x, y, wd)
	case pluginapi.WidgetToggle:
		pt.toggle(w, x, y, wd)
	case pluginapi.WidgetList:
		pt.list(w, x, y, wd, h)
	case pluginapi.WidgetTree:
		pt.tree(w, x, y, wd, h)
	}
}

// ---- containers ----

// isBlock reports whether a widget lays out as a block inside a row (a column, a text
// block, a list or a tree). The other kinds are inline and take their natural width.
func isBlock(k pluginapi.WidgetKind) bool {
	switch k {
	case pluginapi.WidgetCol, pluginapi.WidgetText, pluginapi.WidgetRaw, pluginapi.WidgetList, pluginapi.WidgetTree:
		return true
	}
	return false
}

// isFlex reports whether a widget takes a share of the free width of a row: a block child
// or a flexible spacer.
func isFlex(w pluginapi.Widget) bool {
	return isBlock(w.Kind) || (w.Kind == pluginapi.WidgetSpacer && w.Flex)
}

// rowWidths returns the cell width of every child of a row layout.
func rowWidths(children []pluginapi.Widget, avail int) []int {
	widths := make([]int, len(children))
	fixed, flex := 0, 0
	for i, c := range children {
		if isFlex(c) {
			flex++
			continue
		}
		widths[i] = measureWidth(c)
		fixed += widths[i]
	}
	rest := max(avail-fixed, 0)
	if flex == 0 {
		return widths
	}
	share := rest / flex
	last := -1
	for i, c := range children {
		if !isFlex(c) {
			continue
		}
		widths[i] = share
		last = i
	}
	if last >= 0 {
		widths[last] += rest - share*flex
	}
	return widths
}

func (pt *painter) row(w pluginapi.Widget, x, y, availW, availH int) {
	if len(w.Children) == 0 {
		return
	}
	widths := rowWidths(w.Children, availW)
	cx := x
	for i, c := range w.Children {
		if widths[i] <= 0 {
			continue
		}
		cw := widths[i]
		if left := x + availW - cx; cw > left {
			cw = left
		}
		if cw <= 0 {
			break
		}
		pt.node(c, cx, y, cw, availH)
		cx += cw
	}
}

// colHeights returns the height every child of a column layout gets. Fixed children take
// their natural height; a list/tree with no visible_rows shares what is left.
func colHeights(children []pluginapi.Widget, avail int) []int {
	heights := make([]int, len(children))
	fixed, flex := 0, 0
	for i, c := range children {
		if autoHeight(c) {
			flex++
			continue
		}
		heights[i] = measureHeight(c)
		fixed += heights[i]
	}
	if flex == 0 {
		return heights
	}
	rest := max(avail-fixed, flex)
	share := rest / flex
	last := -1
	for i, c := range children {
		if !autoHeight(c) {
			continue
		}
		heights[i] = share
		last = i
	}
	if last >= 0 {
		heights[last] += rest - share*flex
	}
	return heights
}

func (pt *painter) col(w pluginapi.Widget, x, y, availW, availH int) {
	if len(w.Children) == 0 {
		return
	}
	heights := colHeights(w.Children, availH)
	cy := y
	for i, c := range w.Children {
		ch := heights[i]
		if left := y + availH - cy; ch > left {
			ch = left
		}
		if ch <= 0 {
			break
		}
		pt.node(c, x, cy, availW, ch)
		cy += ch
	}
}

// autoHeight reports whether a widget shares the height left by its siblings.
func autoHeight(w pluginapi.Widget) bool {
	return (w.Kind == pluginapi.WidgetList || w.Kind == pluginapi.WidgetTree) && w.VisibleRows <= 0
}

// ---- measure ----

// measureWidth is the natural cell width of a widget.
func measureWidth(w pluginapi.Widget) int {
	switch w.Kind {
	case pluginapi.WidgetRow:
		n := 0
		for _, c := range w.Children {
			if isFlex(c) {
				n++
				continue
			}
			n += measureWidth(c)
		}
		return n
	case pluginapi.WidgetCol:
		n := 0
		for _, c := range w.Children {
			n = max(n, measureWidth(c))
		}
		return n
	case pluginapi.WidgetLabel:
		if len(w.Runs) > 0 {
			n := 0
			for _, r := range w.Runs {
				n += ansi.StringWidth(Sanitize(r.Text))
			}
			return n
		}
		return ansi.StringWidth(Sanitize(w.Text))
	case pluginapi.WidgetText:
		n := 0
		for _, line := range strings.Split(Sanitize(w.Text), "\n") {
			n = max(n, ansi.StringWidth(line))
		}
		return n
	case pluginapi.WidgetRaw:
		n := 0
		for _, r := range w.Runs {
			n = max(n, ansi.StringWidth(Sanitize(r.Text)))
		}
		return n
	case pluginapi.WidgetHintBar:
		n := 0
		for i, r := range w.Runs {
			if i > 0 {
				n += 3
			}
			n += ansi.StringWidth(Sanitize(r.Text))
		}
		return n
	case pluginapi.WidgetSpacer:
		return max(w.Columns, 1)
	case pluginapi.WidgetDivider:
		return 1
	case pluginapi.WidgetButton:
		return ansi.StringWidth(Sanitize(w.Label)) + 2
	case pluginapi.WidgetToggle:
		return 4 + ansi.StringWidth(Sanitize(w.Label))
	case pluginapi.WidgetList:
		n := 0
		for _, it := range w.Items {
			line := ansi.StringWidth(Sanitize(it.Text))
			if it.Detail != "" {
				line += 2 + ansi.StringWidth(Sanitize(it.Detail))
			}
			n = max(n, line)
		}
		return n
	case pluginapi.WidgetTree:
		n := 0
		for _, nd := range w.Nodes {
			line := nd.Depth*2 + ansi.StringWidth(Sanitize(nd.Text)) + 2
			if w.Checkable {
				line += 4
			}
			if nd.Detail != "" {
				line += 2 + ansi.StringWidth(Sanitize(nd.Detail))
			}
			n = max(n, line)
		}
		return n
	}
	return 0
}

// measureHeight is the natural row height of a widget.
func measureHeight(w pluginapi.Widget) int {
	switch w.Kind {
	case pluginapi.WidgetRow:
		n := 1
		for _, c := range w.Children {
			n = max(n, measureHeight(c))
		}
		return n
	case pluginapi.WidgetCol:
		n := 0
		for _, c := range w.Children {
			n += measureHeight(c)
		}
		return max(n, 1)
	case pluginapi.WidgetText:
		return max(len(strings.Split(Sanitize(w.Text), "\n")), 1)
	case pluginapi.WidgetRaw:
		return max(len(w.Runs), 1)
	case pluginapi.WidgetList:
		if w.VisibleRows > 0 {
			return min(w.VisibleRows, max(len(w.Items), 1))
		}
		return min(max(len(w.Items), 1), DefaultVisibleRows)
	case pluginapi.WidgetTree:
		if w.VisibleRows > 0 {
			return min(w.VisibleRows, max(len(w.Nodes), 1))
		}
		return min(max(len(w.Nodes), 1), DefaultVisibleRows)
	}
	return 1
}

// ---- styles ----

// colorOf resolves a plugin color. A nil resolver, or a color that resolves to nothing,
// keeps def.
func (p *Panel) colorOf(c pluginapi.Color, def color.Color) color.Color {
	if p.resolve == nil {
		return def
	}
	if col := p.resolve(c); col != nil {
		return col
	}
	return def
}

// styleOf returns the base style of a widget: the box style with the widget's own colors
// and attributes.
func (p *Panel) styleOf(w pluginapi.Widget) grid.Style {
	st := p.chrome.Box
	if w.Style.FG.Key != "" || w.Style.FG.Set {
		st.FG = p.colorOf(w.Style.FG, st.FG)
	}
	if w.Style.BG.Key != "" || w.Style.BG.Set {
		st.BG = p.colorOf(w.Style.BG, st.BG)
	}
	st.Bold = st.Bold || w.Style.Bold
	st.Italic = st.Italic || w.Style.Italic
	st.Underline = st.Underline || w.Style.Underline
	st.Strike = st.Strike || w.Style.Strike
	st.Reverse = st.Reverse || w.Style.Reverse
	return st
}

// runStyle is the style of one styled run over base.
func (p *Panel) runStyle(base grid.Style, st pluginapi.Style) grid.Style {
	if st.FG.Key != "" || st.FG.Set {
		base.FG = p.colorOf(st.FG, base.FG)
	}
	if st.BG.Key != "" || st.BG.Set {
		base.BG = p.colorOf(st.BG, base.BG)
	}
	base.Bold = base.Bold || st.Bold
	base.Italic = base.Italic || st.Italic
	base.Underline = base.Underline || st.Underline
	base.Strike = base.Strike || st.Strike
	base.Reverse = base.Reverse || st.Reverse
	return base
}

// focusedStyle is the look of the focused control.
func (p *Panel) focusedStyle(base grid.Style) grid.Style {
	st := p.chrome.Focus
	if base.BG != nil && st.BG == nil {
		st.BG = base.BG
	}
	return st
}

// elide cuts text to w cells with an ellipsis.
func elide(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, "…")
}

// ---- leaves ----

func (pt *painter) label(node pluginapi.Widget, x, y, wd int) {
	base := pt.p.styleOf(node)
	if len(node.Runs) > 0 {
		cx := x
		for _, r := range node.Runs {
			if cx >= x+wd {
				break
			}
			text := elide(Sanitize(r.Text), x+wd-cx)
			cx += pt.g.Put(cx, y, x+wd, text, pt.p.runStyle(base, r.Style))
		}
		return
	}
	pt.g.Put(x, y, x+wd, elide(Sanitize(pt.p.value(node)), wd), base)
}

func (pt *painter) text(node pluginapi.Widget, x, y, wd, h int) {
	base := pt.p.styleOf(node)
	for i, line := range strings.Split(Sanitize(pt.p.value(node)), "\n") {
		if i >= h {
			break
		}
		pt.g.Put(x, y+i, x+wd, elide(line, wd), base)
	}
}

func (pt *painter) raw(node pluginapi.Widget, x, y, wd, h int) {
	for i, r := range node.Runs {
		if i >= h {
			break
		}
		base := pt.p.styleOf(node)
		pt.g.Put(x, y+i, x+wd, elide(Sanitize(r.Text), wd), pt.p.runStyle(base, r.Style))
	}
}

func (pt *painter) hintBar(node pluginapi.Widget, x, y, wd int) {
	base := pt.p.styleOf(node)
	if base.BG == nil {
		base.BG = pt.p.chrome.Box.BG
	}
	cx := x
	for i, r := range node.Runs {
		if cx >= x+wd {
			break
		}
		if i > 0 {
			cx += pt.g.Put(cx, y, x+wd, "  ", base)
		}
		cx += pt.g.Put(cx, y, x+wd, elide(Sanitize(r.Text), x+wd-cx), pt.p.runStyle(base, r.Style))
	}
}

func (pt *painter) divider(node pluginapi.Widget, x, y, wd int) {
	base := pt.p.chrome.Muted
	if base.BG == nil {
		base.BG = pt.p.chrome.Box.BG
	}
	ch := Sanitize(node.Text)
	if ch == "" || ansi.StringWidth(ch) != 1 {
		ch = "─"
	}
	for cx := x; cx < x+wd; cx++ {
		pt.g.Set(cx, y, ch, base)
	}
}

func (pt *painter) button(node pluginapi.Widget, x, y, wd int) {
	fi := -1
	focused := false
	if pt.p.acceptsFocus(node) {
		fi = pt.fi
		focused = fi == pt.p.focus && pt.p.focused
		pt.fi++
	}
	base := pt.p.styleOf(node)
	if node.Disabled {
		base.Faint = true
	}
	if focused {
		base = pt.p.focusedStyle(base)
	}
	text := "[" + Sanitize(node.Label) + "]"
	pt.g.Put(x, y, x+wd, elide(text, wd), base)
	if !node.Disabled {
		pt.p.hits = append(pt.p.hits, hit{r: Rect{X: x, Y: y, W: min(measureWidth(node), wd), H: 1}, kind: hitButton, focusIdx: fi})
	}
}

func (pt *painter) toggle(node pluginapi.Widget, x, y, wd int) {
	fi := -1
	focused := false
	if pt.p.acceptsFocus(node) {
		fi = pt.fi
		focused = fi == pt.p.focus && pt.p.focused
		pt.fi++
	}
	chip := "[ ]"
	switch {
	case node.Indeterminate:
		chip = "[-]"
	case pt.p.checked(node):
		chip = "[x]"
	}
	base := pt.p.styleOf(node)
	if focused {
		base = pt.p.focusedStyle(base)
	}
	text := chip
	if node.Label != "" {
		text += " " + Sanitize(node.Label)
	}
	pt.g.Put(x, y, x+wd, elide(text, wd), base)
	pt.p.hits = append(pt.p.hits, hit{r: Rect{X: x, Y: y, W: min(measureWidth(node), wd), H: 1}, kind: hitToggle, focusIdx: fi})
}

func (pt *painter) list(node pluginapi.Widget, x, y, wd, h int) {
	fi := -1
	focused := false
	if pt.p.acceptsFocus(node) {
		fi = pt.fi
		focused = fi == pt.p.focus && pt.p.focused
		pt.fi++
	}
	base := pt.p.styleOf(node)
	rows := min(len(node.Items), max(h, 0))
	if node.VisibleRows > 0 {
		rows = min(rows, node.VisibleRows)
	}
	sel := pt.p.selected(node)
	if sel < 0 && len(node.Items) > 0 {
		sel = 0
	}
	top := windowTop(sel, rows, len(node.Items))
	pt.p.lastVisibleRows = max(rows, 1)
	if len(node.Items) == 0 {
		pt.g.Put(x, y, x+wd, elide("(empty)", wd), pt.p.chrome.Muted)
		return
	}
	for k := 0; k < rows; k++ {
		i := top + k
		if i >= len(node.Items) {
			break
		}
		it := node.Items[i]
		st := base
		switch {
		case i == sel && focused:
			st = pt.p.focusedStyle(base)
		case i == sel:
			st = pt.p.chrome.Selected
		case it.Checked:
			st = pt.p.chrome.Selected
			st.Faint = false
		}
		if it.Disabled {
			st.Faint = true
		}
		pt.g.Fill(x, x+wd, y+k, st)
		text := Sanitize(it.Text)
		if it.Checked {
			text = "[x] " + text
		}
		cx := x + pt.g.Put(x, y+k, x+wd, elide(text, wd), st)
		if it.Detail != "" && cx < x+wd {
			detail := pt.p.chrome.Muted
			if i == sel {
				detail = st
				detail.Faint = true
			}
			pt.g.Put(cx+2, y+k, x+wd, elide(Sanitize(it.Detail), x+wd-cx-2), detail)
		}
		pt.p.hits = append(pt.p.hits, hit{r: Rect{X: x, Y: y + k, W: wd, H: 1}, kind: hitListRow, focusIdx: fi, index: i, disabled: it.Disabled, path: widgetPath(pt.p, fi)})
	}
}

// widgetPath returns the spec path of the focusable with index fi, "" when fi is -1.
func widgetPath(p *Panel, fi int) string {
	if fi < 0 || fi >= len(p.focusables) {
		return ""
	}
	return p.focusables[fi].path
}

// windowTop returns the first visible row of a window of rows items with the selection at
// sel inside it.
func windowTop(sel, rows, n int) int {
	if rows <= 0 || n <= rows {
		return 0
	}
	if sel < 0 {
		return 0
	}
	top := sel - rows/2
	if top < 0 {
		top = 0
	}
	if top > n-rows {
		top = n - rows
	}
	return top
}

// visNode is one tree node drawn, with its index in the spec's node list.
type visNode struct {
	node     pluginapi.WidgetNode
	index    int
	children bool
	expanded bool
}

func (pt *painter) tree(node pluginapi.Widget, x, y, wd, h int) {
	fi := -1
	focused := false
	if pt.p.acceptsFocus(node) {
		fi = pt.fi
		focused = fi == pt.p.focus && pt.p.focused
		pt.fi++
	}
	base := pt.p.styleOf(node)
	vis := pt.p.visible(node)
	rows := min(len(vis), max(h, 0))
	if node.VisibleRows > 0 {
		rows = min(rows, node.VisibleRows)
	}
	pt.p.lastVisibleRows = max(rows, 1)
	if len(vis) == 0 {
		pt.g.Put(x, y, x+wd, elide("(empty)", wd), pt.p.chrome.Muted)
		return
	}
	sel := pt.p.selected(node)
	selPos := -1
	for i, vn := range vis {
		if vn.index == sel {
			selPos = i
			break
		}
	}
	top := 0
	if selPos >= 0 {
		top = windowTop(selPos, rows, len(vis))
	}
	for k := 0; k < rows; k++ {
		pos := top + k
		if pos >= len(vis) {
			break
		}
		vn := vis[pos]
		st := base
		switch {
		case vn.index == sel && focused:
			st = pt.p.focusedStyle(base)
		case vn.index == sel:
			st = pt.p.chrome.Selected
		}
		pt.g.Fill(x, x+wd, y+k, st)
		indent := x + vn.node.Depth*2
		if indent >= x+wd {
			indent = x
		}
		disclosure := "  "
		if vn.children {
			switch {
			case vn.expanded:
				disclosure = "▾ "
			default:
				disclosure = "▸ "
			}
		}
		cx := indent
		cx += pt.g.Put(cx, y+k, x+wd, disclosure, pt.p.chrome.Muted)
		if node.Checkable {
			chip := "[ ]"
			if vn.node.Checked {
				chip = "[x]"
			}
			cx += pt.g.Put(cx, y+k, x+wd, chip+" ", st)
		}
		cx += pt.g.Put(cx, y+k, x+wd, elide(Sanitize(vn.node.Text), x+wd-cx), st)
		if vn.node.Detail != "" && cx < x+wd {
			detail := pt.p.chrome.Muted
			if vn.index == sel {
				detail = st
				detail.Faint = true
			}
			pt.g.Put(cx+2, y+k, x+wd, elide(Sanitize(vn.node.Detail), x+wd-cx-2), detail)
		}
		pt.p.hits = append(pt.p.hits, hit{r: Rect{X: x, Y: y + k, W: wd, H: 1}, kind: hitTreeRow, focusIdx: fi, index: vn.index, path: widgetPath(pt.p, fi)})
		if vn.children {
			pt.p.hits = append(pt.p.hits, hit{r: Rect{X: indent, Y: y + k, W: 2, H: 1}, kind: hitTreeDisclosure, focusIdx: fi, index: vn.index, path: widgetPath(pt.p, fi)})
		}
	}
}

// acceptsFocus reports whether a widget joins the focus ring, the same rule collect uses.
func (p *Panel) acceptsFocus(w pluginapi.Widget) bool {
	return isFocusable(w.Kind) && (w.Kind != pluginapi.WidgetButton || !w.Disabled)
}

// visible returns the tree nodes drawn with the current expanded state. A node is a parent
// when the following node is deeper (unless it says Leaf); its children are drawn only
// while it is expanded.
func (p *Panel) visible(w pluginapi.Widget) []visNode {
	out := make([]visNode, 0, len(w.Nodes))
	var stack []bool
	for i, nd := range w.Nodes {
		for len(stack) > nd.Depth {
			stack = stack[:len(stack)-1]
		}
		hidden := false
		for _, open := range stack {
			if !open {
				hidden = true
				break
			}
		}
		children := !nd.Leaf && i+1 < len(w.Nodes) && w.Nodes[i+1].Depth > nd.Depth
		if !hidden {
			out = append(out, visNode{node: nd, index: i, children: children, expanded: children && p.expanded(w, nd)})
		}
		if children {
			for len(stack) < nd.Depth {
				stack = append(stack, true)
			}
			if len(stack) == nd.Depth {
				stack = append(stack, p.expanded(w, nd))
			} else {
				stack[nd.Depth] = p.expanded(w, nd)
			}
		}
	}
	return out
}
