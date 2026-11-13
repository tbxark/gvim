// Package picker is the list picker overlay that plugins open: a titled box of rows
// with the footer text on its bottom border, and a query line on the bottom row of
// the frame, laid out like a card above a sheet input. Rows are items the owner gave;
// a fuzzy picker ranks them against the query and a live picker reports every query
// change so the owner can supply new items.
//
// A Picker acts on nothing. Every operation returns a Result: the events the owner must
// act on (Changed, Moved, Chosen, Closed) and the command that ranks the items. The
// ranking is a command: it never runs in Update or Draw, and the Ranked message it
// sends carries a generation, so the result for an older query is dropped. At most
// MaxItems items are accepted and at most MaxRows rows are ranked, kept and drawn
// from.
//
// Esc is not seen here: the root pops every overlay on Esc, and the owner calls Close
// when it does. A picker is not an overlay itself: the root hosts it in a manager or
// plugin overlay, which turns the events of a Result into commands, so the package does
// not import the root model.
package picker

import (
	"fmt"
	"slices"
	"sort"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/fuzzy"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/space"
	"github.com/tbxark/gvim/internal/tui/style"
)

const (
	// MaxItems is how many items a picker accepts; the rest are dropped.
	MaxItems = 50_000
	// MaxRows is how many rows a picker ranks and keeps.
	MaxRows = 100
	// boxRows is the most item rows the box shows at once.
	boxRows = 10
	// wheelRows is the number of rows one wheel notch moves the highlight.
	wheelRows = 3
	// pageRows is the number of rows PageUp and PageDown move the highlight.
	pageRows = 8
)

// Item is one row of a picker. ID is the owner's handle for the item and is reported in
// Moved and Chosen. A Disabled item is drawn faint and cannot be chosen.
type Item struct {
	ID                       int
	Text, Detail, Keybinding string
	Disabled                 bool
}

// Options configure a picker.
type Options struct {
	Title, Footer, Placeholder, InitialQuery string
	Items                                    []Item
	// Fuzzy ranks the items against the query. Without it the rows are the items as
	// given, and the owner changes them with SetItems.
	Fuzzy bool
	// Live reports every query change (Changed) so the owner can supply new items.
	Live bool
	// Preview reports every change of the highlighted item (Moved).
	Preview bool
}

// Event is something the owner must act on. The events of one Update are in order.
type Event interface{ event() }

// Changed says the query text changed. It is reported only by a Live picker.
type Changed struct{ Query string }

// NoItem is the Moved ID that says no row is shown, so nothing is highlighted.
const NoItem = -1

// Moved says the highlight is on the item with this ID, or NoItem when no row is shown
// (the filter matches nothing, or the items are empty). It is reported only by a Preview
// picker.
type Moved struct{ ID int }

// Chosen says the item with this ID was chosen; the picker closed.
type Chosen struct{ ID int }

// Closed says the picker was closed without a choice.
type Closed struct{}

func (Changed) event() {}
func (Moved) event()   {}
func (Chosen) event()  {}
func (Closed) event()  {}

// Result is what an operation on a Picker asks of its owner.
type Result struct {
	// Events are to be acted on, in order.
	Events []Event
	// Rank is the command that ranks the items. The Ranked message it sends goes back to
	// Update, which drops it when a newer ranking has started since.
	Rank tea.Cmd
	// Cmd is any other command: the clipboard read of Ctrl+V.
	Cmd tea.Cmd
}

// Ranked is the result of one ranking command. Update applies it when it answers the
// current generation.
type Ranked struct {
	gen   int
	query string
	rows  []row
}

// row is one visible item, copied from the item list, and the matched rune offsets of
// its text. A row does not depend on the item list, so the list can be replaced while
// the rows of the old one are still shown.
type row struct {
	it Item
	hl []int
}

// Picker is an open picker. Use New to make one.
type Picker struct {
	st    styles
	title string

	footer, status, placeholder string

	input textinput.Model
	query string // the query the rows answer, or are being ranked for
	// rowsQuery is the query the rows in rows were ranked for; it differs from query
	// while a ranking is in flight, and Enter must rank synchronously then.
	rowsQuery string

	items []Item
	rows  []row
	sel   int // index into rows
	top   int // first row shown

	fuzzy, live, preview bool

	gen   int  // generation of the latest ranking command
	open  bool // false once chosen or closed
	moved int  // ID of the last Moved reported; -1 for none

	box geom // placement of the last Draw, for mouse hit tests
}

// styles are the grid styles of a picker.
type styles struct {
	box, selected, muted, border, title, prompt grid.Style
}

func newStyles(st style.Styles) styles {
	ov := st.Overlay
	return styles{
		box: ov.Box, selected: ov.Selected, muted: ov.Muted,
		border: ov.Border, title: ov.Title,
		prompt: ov.Idle,
	}
}

// New returns an open picker. Call Init to get the first events and the first ranking.
func New(st style.Styles, o Options) *Picker {
	p := &Picker{
		st:          newStyles(st),
		title:       o.Title,
		footer:      o.Footer,
		placeholder: o.Placeholder,
		items:       clipItems(o.Items),
		fuzzy:       o.Fuzzy,
		live:        o.Live,
		preview:     o.Preview,
		open:        true,
		moved:       -1,
	}
	p.input = textinput.New()
	p.input.Prompt = ""
	p.input.SetValue(o.InitialQuery)
	p.input.CursorEnd()
	p.input.Focus() // Draw paints the caret cell
	p.query = p.input.Value()
	// The opening rows are the plain list: they answer an empty query only, so a fuzzy
	// picker with an initial query leaves rowsQuery empty until its ranking arrives.
	p.rows = p.plainRows()
	return p
}

// clipItems copies items and drops those past MaxItems. The picker never changes the
// slice it keeps, so a ranking command can read it while Update goes on.
func clipItems(items []Item) []Item {
	return slices.Clone(items[:min(len(items), MaxItems)])
}

// Init returns the events of the opening state and the command that ranks the opening
// query. The command is nil unless the picker is fuzzy and the query is not empty.
func (p *Picker) Init() Result {
	var r Result
	if p.fuzzy && p.query != "" {
		r.Rank = p.rank()
	}
	r.Events = p.reportMoved(nil)
	return r
}

// Active reports whether the picker is open.
func (p *Picker) Active() bool { return p.open }

// hasInput reports whether the picker has a query line: it filters or reports queries.
func (p *Picker) hasInput() bool { return p.fuzzy || p.live }

// Query is the text of the query line.
func (p *Picker) Query() string { return p.input.Value() }

// SelectedID is the ID of the highlighted item. ok is false when no row is shown.
func (p *Picker) SelectedID() (id int, ok bool) {
	if p.sel < 0 || p.sel >= len(p.rows) {
		return 0, false
	}
	return p.rows[p.sel].it.ID, true
}

// Rows is the number of rows shown.
func (p *Picker) Rows() int { return len(p.rows) }

// RowIDs are the IDs of the rows shown, top to bottom.
func (p *Picker) RowIDs() []int {
	out := make([]int, len(p.rows))
	for i, r := range p.rows {
		out[i] = r.it.ID
	}
	return out
}

// SetTitle replaces the title.
func (p *Picker) SetTitle(s string) { p.title = s }

// SetFooter replaces the footer text.
func (p *Picker) SetFooter(s string) { p.footer = s }

// SetStatus replaces the status text, which is shown at the right of the footer row
// instead of the row count. An empty status shows the count again.
func (p *Picker) SetStatus(s string) { p.status = s }

// SetItems replaces the items. The highlight stays on the same item when it is still
// listed. A fuzzy picker with a query ranks the new items by a command, and keeps the
// old rows until the result arrives.
func (p *Picker) SetItems(items []Item) Result {
	keep, had := p.SelectedID()
	p.items = clipItems(items)
	var r Result
	if p.fuzzy && p.query != "" {
		// The old rows stay on screen until the ranking answers.
		r.Rank = p.rank()
	} else {
		p.rows = p.plainRows()
		p.selectID(keep, had)
	}
	r.Events = p.reportMoved(nil)
	return r
}

// Select highlights the row of the item with this ID, when it is shown.
func (p *Picker) Select(id int) Result {
	p.selectID(id, true)
	return Result{Events: p.reportMoved(nil)}
}

// selectID moves the highlight to the row of the item with this ID. When there is none,
// or want is false, it goes to the first row.
func (p *Picker) selectID(id int, want bool) {
	p.sel, p.top = 0, 0
	if want {
		for i, r := range p.rows {
			if r.it.ID == id {
				p.sel = i
				break
			}
		}
	}
	p.keepVisible()
}

// Close closes the picker without a choice.
func (p *Picker) Close() Event {
	p.open = false
	return Closed{}
}

// plainRows are the first MaxRows items in the order given.
func (p *Picker) plainRows() []row {
	n := min(len(p.items), MaxRows)
	rows := make([]row, n)
	for i := range rows {
		rows[i] = row{it: p.items[i]}
	}
	return rows
}

// rank starts a new generation and returns the command that ranks the items against
// the current query.
func (p *Picker) rank() tea.Cmd {
	p.gen++
	gen, query, items := p.gen, p.query, p.items
	return func() tea.Msg { return Ranked{gen: gen, query: query, rows: rankItems(query, items)} }
}

// rankItems scores the item texts against query and keeps the best MaxRows, best
// first. Items of equal score keep their order.
func rankItems(query string, items []Item) []row {
	type hit struct {
		row   row
		score int
	}
	pat := fuzzy.NewPattern(query)
	var hits []hit
	for _, it := range items {
		if score, idx, ok := pat.Match(it.Text); ok {
			hits = append(hits, hit{row{it: it, hl: idx}, score})
		}
	}
	sort.SliceStable(hits, func(a, b int) bool { return hits[a].score > hits[b].score })
	rows := make([]row, min(len(hits), MaxRows))
	for i := range rows {
		rows[i] = hits[i].row
	}
	return rows
}

// Update handles one message and returns the events it caused and the command that
// continues the ranking. Key presses edit the query and move the highlight, Enter
// chooses the highlighted item, a left click chooses the row under it, the wheel moves
// the highlight, and a Ranked message for the latest generation replaces the rows.
func (p *Picker) Update(msg tea.Msg) Result {
	if !p.open {
		return Result{}
	}
	switch msg := msg.(type) {
	case Ranked:
		return Result{Events: p.applyRanked(msg)}
	case tea.KeyPressMsg:
		return p.key(msg)
	case tea.PasteMsg:
		return p.edit(msg)
	case tea.MouseClickMsg:
		return Result{Events: p.click(tea.Mouse(msg))}
	case tea.MouseMotionMsg:
		m := tea.Mouse(msg)
		if i, ok := p.rowAt(m.X, m.Y); ok {
			p.sel = i
			return Result{Events: p.reportMoved(nil)}
		}
	case tea.MouseWheelMsg:
		switch tea.Mouse(msg).Button {
		case tea.MouseWheelUp:
			return Result{Events: p.move(-wheelRows)}
		case tea.MouseWheelDown:
			return Result{Events: p.move(wheelRows)}
		}
	default:
		// The clipboard text that Ctrl+V asked for reaches the query line here.
		return p.edit(msg)
	}
	return Result{}
}

// key runs a key press: a list key moves the highlight or chooses, any other key goes
// to the query line.
func (p *Picker) key(msg tea.KeyPressMsg) Result {
	switch tea.Key(msg).String() {
	case "enter":
		return Result{Events: p.choose(p.sel)}
	case "up", "ctrl+p":
		return Result{Events: p.move(-1)}
	case "down", "ctrl+n":
		return Result{Events: p.move(1)}
	case "pgup":
		return Result{Events: p.move(-pageRows)}
	case "pgdown":
		return Result{Events: p.move(pageRows)}
	}
	return p.edit(msg)
}

// edit gives msg to the query line. A changed query is reported to a live picker and
// ranked by a fuzzy one. The command of Ctrl+V, which reads the clipboard, is returned
// too.
func (p *Picker) edit(msg tea.Msg) Result {
	if !p.hasInput() {
		return Result{}
	}
	before := p.input.Value()
	var read tea.Cmd
	p.input, read = p.input.Update(msg)
	if kp, ok := msg.(tea.KeyPressMsg); !ok || tea.Key(kp).Keystroke() != "ctrl+v" {
		read = nil
	}
	after := p.input.Value()
	if after == before {
		return Result{Cmd: read}
	}
	p.query = after
	r := Result{Cmd: read}
	if p.live {
		r.Events = append(r.Events, Changed{Query: after})
	}
	if !p.fuzzy {
		return r
	}
	if after == "" {
		// Nothing to rank: the first rows of the list, at once. Bump the generation so a
		// ranking in flight is dropped.
		p.gen++
		p.rows = p.plainRows()
		p.rowsQuery = ""
		p.sel, p.top = 0, 0
		r.Events = p.reportMoved(r.Events)
		return r
	}
	r.Rank = p.rank()
	return r
}

// applyRanked shows a ranked result when it answers the latest ranking. The highlight
// goes to the top row.
func (p *Picker) applyRanked(r Ranked) []Event {
	if r.gen != p.gen {
		return nil
	}
	p.rows = r.rows
	p.rowsQuery = r.query
	p.sel, p.top = 0, 0
	return p.reportMoved(nil)
}

// move shifts the highlight by d rows, clamped to the rows.
func (p *Picker) move(d int) []Event {
	if len(p.rows) == 0 {
		return nil
	}
	p.sel = min(max(p.sel+d, 0), len(p.rows)-1)
	p.keepVisible()
	return p.reportMoved(nil)
}

// keepVisible scrolls the window of rows so that the highlight is in it.
func (p *Picker) keepVisible() {
	n := p.window()
	if p.sel < p.top {
		p.top = p.sel
	}
	if p.sel >= p.top+n {
		p.top = p.sel - n + 1
	}
	p.top = max(0, min(p.top, len(p.rows)-n))
}

// window is the number of rows the box shows.
func (p *Picker) window() int {
	if p.box.rows > 0 {
		return p.box.rows
	}
	return boxRows
}

// reportMoved appends Moved when the highlight is on an item the owner has not been told
// about. Only a Preview picker reports it.
func (p *Picker) reportMoved(ev []Event) []Event {
	if !p.preview {
		return ev
	}
	id, ok := p.SelectedID()
	if !ok {
		id = NoItem
	}
	if id == p.moved {
		return ev
	}
	p.moved = id
	return append(ev, Moved{ID: id})
}

// choose closes the picker on row i. A disabled item cannot be chosen, and with no row
// there is nothing to choose.
func (p *Picker) choose(i int) []Event {
	if p.fuzzy && p.rowsQuery != p.query {
		// Enter arrived while a ranking was still in flight: rank the typed query now, so
		// the choice is the filtered first row and not a stale row of the old query. Only a
		// fuzzy picker ranks; the rows of the others are always the owner's items.
		if p.query != "" {
			p.rows = rankItems(p.query, p.items)
		} else {
			p.rows = p.plainRows()
		}
		p.rowsQuery = p.query
		p.sel, p.top = 0, 0
		i = p.sel
	}
	if i < 0 || i >= len(p.rows) {
		return nil
	}
	it := p.rows[i].it
	if it.Disabled {
		return nil
	}
	p.open = false
	return []Event{Chosen{ID: it.ID}}
}

// click chooses the row under a left click. A click outside the rows is swallowed.
func (p *Picker) click(m tea.Mouse) []Event {
	if m.Button != tea.MouseLeft {
		return nil
	}
	i, ok := p.rowAt(m.X, m.Y)
	if !ok {
		return nil
	}
	p.sel = i
	return p.choose(i)
}

// geom is the placement of the picker in a frame; painting and hit testing share it.
type geom struct {
	left, bw  int // the box's left border column and its width: the frame less its margins
	top, boxH int // the box, borders included; boxH is 0 when it does not fit
	rows      int // item rows in the box
	bottom    int // the box's bottom border row, which carries the footer
	queryY    int // -1 when there is no query line
}

// layoutFor places the picker in a w by h frame. The box keeps space.Margin from the
// left and right edge and one row from the top. With a query line the query is the
// last row of the frame and the box's bottom border sits one row above it; without
// one the box's bottom border is the last row.
func (p *Picker) layoutFor(w, h int) geom {
	m := space.Margin(w)
	g := geom{left: m, bw: w - 2*m, queryY: -1, bottom: h - 1}
	if p.hasInput() {
		g.queryY = h - 1
		g.bottom = h - 2
	}
	g.rows = min(boxRows, g.bottom-2)
	if g.rows < 1 {
		g.rows = 0
		return g
	}
	g.boxH = g.rows + 2
	g.top = g.bottom - g.boxH + 1
	return g
}

// rowAt maps a frame cell to the index of the row under it.
func (p *Picker) rowAt(x, y int) (int, bool) {
	g := p.box
	if g.boxH == 0 || x <= g.left || x >= g.left+g.bw-1 {
		return 0, false
	}
	k := y - g.top - 1
	if k < 0 || k >= g.rows {
		return 0, false
	}
	i := p.top + k
	if i >= len(p.rows) {
		return 0, false
	}
	return i, true
}

// Draw paints the box and the query line over g. It records the frame geometry, so
// that a mouse position maps to a row. It changes no row and no highlight, except to
// keep the highlight in the window when the frame height changed.
func (p *Picker) Draw(g *grid.Grid) {
	if !p.open || g.Width() <= 0 || g.Height() <= 0 {
		p.box = geom{}
		return
	}
	p.box = p.layoutFor(g.Width(), g.Height())
	p.keepVisible()
	geo := p.box

	if geo.boxH > 0 {
		ov := style.Overlay{Box: p.st.box, Muted: p.st.muted, Border: p.st.border, Title: p.st.title}
		ov.PaintBox(g, geo.left, geo.top, geo.bw, geo.boxH, p.title)
		p.paintRows(g, geo)
		p.paintFooter(g, geo, ov)
	}
	if geo.queryY >= 0 {
		// The query is the sheet's input row: it starts at the card's content column,
		// after the left border and its padding cell.
		p.paintQuery(g, geo.queryY, geo.left+1+space.PadX)
	}
}

// paintRows draws the rows in view inside the box.
func (p *Picker) paintRows(g *grid.Grid, geo geom) {
	if len(p.rows) == 0 {
		g.Put(geo.left+2, geo.top+1, geo.left+geo.bw-2, pickerText("gvim.picker.no_items", "No items"), p.st.muted)
		return
	}
	for k := range geo.rows {
		i := p.top + k
		if i >= len(p.rows) {
			break
		}
		y := geo.top + 1 + k
		sel := i == p.sel
		base := p.st.box
		if sel {
			base = p.st.selected
			g.Fill(geo.left+1, geo.left+geo.bw-1, y, base)
		}
		p.paintRow(g, geo, y, p.rows[i], base)
	}
}

// paintRow draws one row: the text with its matched runes in bold, the detail after
// it, and the key binding at the right edge.
func (p *Picker) paintRow(g *grid.Grid, geo geom, y int, r row, base grid.Style) {
	it := r.it
	end := geo.left + geo.bw - 2
	text := base
	if it.Disabled {
		text.Faint = true
	}
	if it.Keybinding != "" {
		kw := ansi.StringWidth(it.Keybinding)
		if x := end - kw; x > geo.left+4 {
			g.Put(x, y, end, it.Keybinding, faint(base))
			end = x - 2
		}
	}
	x := geo.left + 2
	if len(r.hl) == 0 {
		x += g.Put(x, y, end, space.End(it.Text, end-x), text)
	} else {
		x += p.putMatched(g, x, y, end, it.Text, r.hl, text)
	}
	if it.Detail != "" && x+3 < end {
		g.Put(x+2, y, end, space.End(it.Detail, end-x-2), faint(base))
	}
}

// putMatched writes s clipped to maxX, with the runes at the indexes in hl bold. It
// returns the cells written. A cut ends with an ellipsis, like space.End.
func (p *Picker) putMatched(g *grid.Grid, x, y, maxX int, s string, hl []int, st grid.Style) int {
	room := maxX - x
	if room <= 0 {
		return 0
	}
	cut := space.End(s, room)
	orig, shown := []rune(s), []rune(cut)
	start := x
	for i, c := range shown {
		cs := st
		if i < len(orig) && orig[i] == c && slices.Contains(hl, i) {
			cs.Bold = true
		}
		x += g.Put(x, y, maxX, string(c), cs)
	}
	return x - start
}

// paintFooter writes the footer on the box's bottom border: the footer text after the
// same one-cell padding as the title, and at the right the status, or the number of
// rows shown out of the number of items.
func (p *Picker) paintFooter(g *grid.Grid, geo geom, ov style.Overlay) {
	right := p.status
	if right == "" && p.fuzzy {
		right = fmt.Sprintf("%d/%d", len(p.rows), len(p.items))
	}
	width := geo.bw
	if rw := ansi.StringWidth(right); rw > 0 && geo.bw-rw-3 > 4 {
		g.Put(geo.left+geo.bw-3-rw, geo.bottom, geo.left+geo.bw-1, " "+right+" ", ov.Muted)
		width = geo.bw - rw - 3
	}
	ov.PaintFooter(g, geo.left, geo.bottom, width, p.footer)
}

// paintQuery writes the query on row y with the caret cell reversed. The row scrolls so
// that the caret stays in view. An empty query shows the placeholder.
func (p *Picker) paintQuery(g *grid.Grid, y, x0 int) {
	w := g.Width()
	x0 = min(x0, max(w-1, 0))
	g.Fill(0, w, y, p.st.prompt)
	runes := []rune(p.input.Value())
	if len(runes) == 0 && p.placeholder != "" {
		g.Put(x0+1, y, w, space.End(p.placeholder, w-x0-1), faint(p.st.prompt))
	}
	pos := min(max(p.input.Position(), 0), len(runes))
	start := 0
	for start < pos && ansi.StringWidth(string(runes[start:pos])) > w-x0-1 {
		start++
	}
	x := x0
	for i := start; i < len(runes); i++ {
		st := p.st.prompt
		if i == pos {
			st.Reverse = true
		}
		x += g.Put(x, y, w, string(runes[i]), st)
	}
	if pos >= len(runes) {
		caret := p.st.prompt
		caret.Reverse = true
		g.Put(x, y, w, " ", caret)
	}
}

// faint returns st with the faint attribute.
func faint(st grid.Style) grid.Style {
	st.Faint = true
	return st
}
