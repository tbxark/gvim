// Package menubar is the menu bar of gvim: the row of labels, the dropdowns that
// open under them, and their keyboard and mouse handling. The rows come from
// the generated table in menu_table_gen.go, which follows the legacy Fresh menu.
//
// The menu bar runs no action itself. An activated row goes to Ports.Run, and
// the context keys that enable rows come from Ports.Holds. The root model
// implements Ports, so this package imports neither the editor nor the root.
//
// Geometry is computed here from the frame size, and Paint and Hit read the
// same geometry, so a label and its click target cannot drift apart.
//
// The menus have no mnemonics: no Alt+letter opens a title and no underlined letter
// activates a row. They are used with the mouse, with the arrow keys once open, and
// through the palette command that opens the bar.
package menubar

import (
	"errors"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/space"
	"github.com/tbxark/gvim/internal/tui/style"
)

// labelGap is the number of blank cells between two labels of the bar.
const labelGap = 3

// labelStart is the column of the first label.
const labelStart = 1

// moreTitle is the title of the overflow marker, the last cell of the bar when the
// labels do not all fit. Its dropdown lists the labels that are not shown.
const moreTitle = "⋯"

// moreCols is the number of columns the marker takes with the blank before it. The
// labels shown end before the last moreCols columns of a bar that overflows.
const moreCols = 2

// ErrUnsupported is the error Ports.Run returns for an action the port does not
// implement. A dropdown row with no action, such as a submenu, reports it too.
var ErrUnsupported = errors.New("menubar: action not supported")

// Ports is what the menu bar needs from the root. The root implements it.
type Ports interface {
	// Run runs the action of an activated row. It returns ErrUnsupported for an
	// action with no implementation yet.
	Run(action string) error
	// Holds reports whether the legacy context key holds in the current state.
	// A row whose when key does not hold is disabled when its dropdown opens.
	Holds(key string) bool
}

// KeyLabeler is an optional part of Ports. KeyLabel returns the shortcut of action in
// the active keymap, as the menu shows it, or "" when the keymap has none. ok is false
// when there is no keymap to ask, and the rows then keep the shortcut of the table.
type KeyLabeler interface {
	KeyLabel(action string) (label string, ok bool)
}

// Failed is emitted as a command when an activated row could not run. The root
// shows Label in the status row, and Err is ErrUnsupported when the row has no
// implementation.
type Failed struct {
	Label string
	Err   error
}

// TargetKind names what is under a pointer position in the bar or dropdown.
type TargetKind int

const (
	TargetNone      TargetKind = iota // outside the bar and the open dropdown
	TargetLabel                       // a bar label; Index is its position in Labels
	TargetItem                        // a dropdown row; Index is its position in Rows
	TargetBorder                      // a dropdown border or separator
	TargetSubItem                     // a row of the open submenu; Index is its position in SubRows
	TargetSubBorder                   // a submenu border or separator
	TargetMore                        // the overflow marker at the right end of the bar
)

// Target is the result of Hit.
type Target struct {
	Kind  TargetKind
	Index int
}

// Model is the menu bar state: the open dropdown, its rows and the highlighted
// row. It is a value type. Update returns the next state.
type Model struct {
	ports         Ports
	styles        styles
	explorerFocus bool
	w, h          int    // frame size, set by SetSize; geometry for Hit
	title         string // label of the open dropdown, "" when closed
	rows          []Row
	selected      int // index of the highlighted row, -1 when none can be highlighted
	scroll        int // index of the first row shown, when the rows do not fit the frame
	sub           subMenu
}

// subMenu is the open submenu of a row: the row it hangs from, its highlighted
// child and the first child shown.
type subMenu struct {
	open     bool
	parent   int
	selected int
	scroll   int
}

// keys are the bindings the open dropdown answers. Every other key falls through.
var keys = struct {
	Up, Down, Left, Right, Enter, Close key.Binding
}{
	Up:    key.NewBinding(key.WithKeys("up")),
	Down:  key.NewBinding(key.WithKeys("down")),
	Left:  key.NewBinding(key.WithKeys("left")),
	Right: key.NewBinding(key.WithKeys("right")),
	Enter: key.NewBinding(key.WithKeys("enter")),
	Close: key.NewBinding(key.WithKeys("esc")),
}

// New returns a closed menu bar that reads its rows through ports and draws
// with the chrome styles of st. A nil ports value disables every row.
func New(ports Ports, st style.Styles) Model {
	return Model{ports: ports, styles: newStyles(st), selected: -1}
}

// SetSize sets the frame size that Hit uses. Paint takes the size from its grid.
func (m Model) SetSize(w, h int) Model {
	resized := w != m.w || h != m.h
	m.w, m.h = w, h
	if resized && m.title == moreTitle {
		// The labels the list shows depend on the width, so a resize closes it.
		m = m.Close()
	}
	if m.IsOpen() {
		m = m.reveal()
	}
	return m
}

// WithExplorerFocus sets whether the explorer label joins the bar. An open
// dropdown whose label leaves the bar closes.
func (m Model) WithExplorerFocus(on bool) Model {
	m.explorerFocus = on
	if m.title != "" && m.title != moreTitle && !slices.Contains(barLabels(on), m.title) {
		m = m.Close()
	}
	return m
}

// Labels returns the bar labels in order for the explorer focus state.
func Labels(explorerFocus bool) []string {
	return barLabels(explorerFocus)
}

// IsOpen reports whether a dropdown is open.
func (m Model) IsOpen() bool { return m.title != "" }

// Title is the label of the open dropdown, or "" when the bar is closed.
func (m Model) Title() string { return m.title }

// Rows returns the rows of the open dropdown. The slice is a copy.
func (m Model) Rows() []Row { return slices.Clone(m.rows) }

// SubOpen reports whether a submenu is open.
func (m Model) SubOpen() bool { return m.sub.open }

// SubRows returns the rows of the open submenu, or nil when none is open. The slice
// is a copy.
func (m Model) SubRows() []Row {
	if !m.sub.open {
		return nil
	}
	return slices.Clone(m.rows[m.sub.parent].children)
}

// SubSelected is the highlighted row of the open submenu, or -1 when none is open or
// no row can be highlighted.
func (m Model) SubSelected() int {
	if !m.sub.open {
		return -1
	}
	return m.sub.selected
}

// Selected is the highlighted row, or -1 when no row can be highlighted.
func (m Model) Selected() int { return m.selected }

// Activate opens the File dropdown with its first selectable row highlighted.
// The root calls it for the menu-activate action (F10).
func (m Model) Activate() Model {
	return m.openTitle("File")
}

// Open opens the dropdown of the title at index i of Labels, with its first
// selectable row highlighted. An index outside the bar leaves the model as it is.
func (m Model) Open(i int) Model {
	titles := Labels(m.explorerFocus)
	if i < 0 || i >= len(titles) {
		return m
	}
	return m.openTitle(titles[i])
}

// Next opens the dropdown of the title to the right of the open one, wrapping
// around the bar. It does nothing when the bar is closed.
func (m Model) Next() Model {
	if !m.IsOpen() {
		return m
	}
	return m.move(1)
}

// Prev opens the dropdown of the title to the left of the open one, wrapping
// around the bar. It does nothing when the bar is closed.
func (m Model) Prev() Model {
	if !m.IsOpen() {
		return m
	}
	return m.move(-1)
}

// Select activates the highlighted row, as Enter does. A row with a submenu opens
// it. Any other row closes the dropdown whether or not it ran. It does nothing when
// the bar is closed.
func (m Model) Select() (Model, tea.Cmd) {
	if !m.IsOpen() {
		return m, nil
	}
	if m.sub.open {
		if m.sub.selected < 0 {
			return m.openSub(m.sub.parent), nil
		}
		return m.Close().run(m.rows[m.sub.parent].children[m.sub.selected])
	}
	if m.selected < 0 {
		return m.Close(), nil
	}
	return m.activate(m.selected)
}

// activate runs row i of the dropdown. A row with a submenu opens it and leaves the
// dropdown open.
func (m Model) activate(i int) (Model, tea.Cmd) {
	row := m.rows[i]
	if row.Opens != "" {
		return m.openTitle(row.Opens), nil
	}
	if row.Submenu && !row.Disabled && len(row.children) > 0 {
		return m.openSub(i), nil
	}
	return m.Close().run(row)
}

// openSub opens the submenu of row i with its first selectable row highlighted.
func (m Model) openSub(i int) Model {
	m.selected = i
	m.sub = subMenu{open: true, parent: i, selected: stepIn(m.rows[i].children, -1, 1)}
	return m.reveal()
}

// Close closes the dropdown. It does nothing when the bar is closed.
func (m Model) Close() Model {
	m.title, m.rows, m.selected, m.scroll, m.sub = "", nil, -1, 0, subMenu{}
	return m
}

// Consumes reports whether msg belongs to the open dropdown, so the root must not
// route it further. A key the dropdown answers is consumed: the arrows, Enter,
// and Esc. A left press on a label, a
// row or a border is consumed. A press anywhere else is not, because it closes
// the dropdown and still reaches what is under it. Hover is not consumed.
func (m Model) Consumes(msg tea.Msg) bool {
	if !m.IsOpen() {
		return false
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return key.Matches(msg, keys.Up, keys.Down, keys.Left, keys.Right, keys.Enter, keys.Close)
	case tea.MouseClickMsg:
		mo := msg.Mouse()
		return mo.Button == tea.MouseLeft && m.Hit(mo.X, mo.Y).Kind != TargetNone
	case tea.MouseWheelMsg:
		// A wheel notch over a dropdown scrolls it, not the editor under it.
		mo := msg.Mouse()
		k := m.Hit(mo.X, mo.Y).Kind
		return k != TargetNone && k != TargetLabel && k != TargetMore
	}
	return false
}

// Update handles key presses while a dropdown is open, presses on the bar and
// its dropdown, and hover while a dropdown is open. A press outside closes the
// dropdown and is not consumed. The returned command reports a row that failed,
// as Failed.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if m.IsOpen() {
			return m.key(msg)
		}
	case tea.MouseClickMsg:
		mo := msg.Mouse()
		if mo.Button == tea.MouseLeft {
			return m.click(mo)
		}
	case tea.MouseMotionMsg:
		if m.IsOpen() {
			return m.hover(msg.Mouse()), nil
		}
	case tea.MouseWheelMsg:
		if m.IsOpen() {
			return m.wheel(msg.Mouse()), nil
		}
	}
	return m, nil
}

// key runs one key press in an open dropdown. Keys it does not answer are left
// to the caller, which the Consumes check reports.
func (m Model) key(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Close):
		if m.sub.open {
			m.sub = subMenu{}
			return m, nil
		}
		return m.Close(), nil
	case key.Matches(msg, keys.Up):
		m = m.stepSelection(-1)
	case key.Matches(msg, keys.Down):
		m = m.stepSelection(1)
	case key.Matches(msg, keys.Left):
		if m.sub.open {
			m.sub = subMenu{}
		} else {
			m = m.Prev()
		}
	case key.Matches(msg, keys.Right):
		switch {
		case m.sub.open:
			if m.sub.selected < 0 {
				m = m.openSub(m.sub.parent)
			}
		case m.selected >= 0 && m.rows[m.selected].Submenu && len(m.rows[m.selected].children) > 0:
			m = m.openSub(m.selected)
		default:
			m = m.Next()
		}
	case key.Matches(msg, keys.Enter):
		return m.Select()
	}
	return m, nil
}

// stepSelection moves the highlight of the open list one selectable row in dir and
// scrolls it into view.
func (m Model) stepSelection(dir int) Model {
	if m.sub.open {
		m.sub.selected = stepFrom(m.rows[m.sub.parent].children, m.sub.selected, dir)
	} else {
		m.selected = stepFrom(m.rows, m.selected, dir)
	}
	return m.reveal()
}

// click handles a left press. A label toggles its dropdown, a row runs, a border
// closes, and any other place closes the dropdown and passes the press on.
func (m Model) click(mo tea.Mouse) (Model, tea.Cmd) {
	t := m.Hit(mo.X, mo.Y)
	switch t.Kind {
	case TargetLabel, TargetMore:
		title := m.titleOf(t)
		if m.IsOpen() && title == m.title {
			return m.Close(), nil
		}
		return m.openTitle(title), nil
	case TargetItem:
		return m.activate(t.Index)
	case TargetSubItem:
		return m.Close().run(m.rows[m.sub.parent].children[t.Index])
	}
	return m.Close(), nil
}

// run runs an activated row. Separators and disabled rows do nothing. A row
// with no action reports ErrUnsupported without calling the port.
func (m Model) run(row Row) (Model, tea.Cmd) {
	switch {
	case row.Separator || row.Disabled:
		return m, nil
	case row.Submenu || row.Action == "":
		return m, failed(row.Label, ErrUnsupported)
	}
	if m.ports == nil {
		return m, failed(row.Label, ErrUnsupported)
	}
	if err := m.ports.Run(row.Action); err != nil {
		return m, failed(row.Label, err)
	}
	return m, nil
}

// failed returns the command that reports a row failure.
func failed(label string, err error) tea.Cmd {
	return func() tea.Msg { return Failed{Label: label, Err: err} }
}

// openTitle opens the dropdown of the label title with its first selectable row
// highlighted. The rows are read from the table when the dropdown opens, and the
// overflow marker's rows are the labels the bar does not show.
func (m Model) openTitle(title string) Model {
	m.title = title
	if title == moreTitle {
		m.rows = m.overflowRows()
	} else {
		m.rows = dropdownRows(title, m.holds, m.keyOf())
	}
	m.scroll, m.sub = 0, subMenu{}
	m.selected = stepIn(m.rows, -1, 1)
	return m.reveal()
}

// overflowRows lists the labels the bar does not show at its width, in bar order. Each
// row opens the dropdown of its label.
func (m Model) overflowRows() []Row {
	shown, _ := barFit(labelRuns(m.explorerFocus), m.w)
	titles := Labels(m.explorerFocus)
	rows := make([]Row, 0, len(titles)-len(shown))
	for _, title := range titles[len(shown):] {
		rows = append(rows, Row{Label: title, Opens: title})
	}
	return rows
}

// titleOf is the title a bar target opens: the label at its index, or the marker.
func (m Model) titleOf(t Target) string {
	if t.Kind == TargetMore {
		return moreTitle
	}
	return Labels(m.explorerFocus)[t.Index]
}

// keyOf returns the lookup of the shortcut of an action in the active keymap, or nil
// when the ports have no keymap to ask.
func (m Model) keyOf() func(action string) (string, bool) {
	if kl, ok := m.ports.(KeyLabeler); ok {
		return kl.KeyLabel
	}
	return nil
}

// move opens the dropdown of the label dir places from the open one, wrapping
// around the bar.
func (m Model) move(dir int) Model {
	titles := Labels(m.explorerFocus)
	i := slices.Index(titles, m.title)
	if i < 0 {
		return m
	}
	return m.openTitle(titles[(i+dir+len(titles))%len(titles)])
}

// stepIn returns the index of the next selectable row of rows from start in
// direction dir (+1 or -1). It stays at start when no row is found.
func stepIn(rows []Row, start, dir int) int {
	for i := start + dir; i >= 0 && i < len(rows); i += dir {
		if rows[i].selectable() {
			return i
		}
	}
	return start
}

// holds evaluates a context key through the ports. Without ports nothing holds.
func (m Model) holds(name string) bool {
	return m.ports != nil && m.ports.Holds(name)
}

// labelRun is one bar label: its first column, its width and its text.
type labelRun struct {
	x, w  int
	title string
}

// labelRuns places the bar labels with labelGap blanks between them.
func labelRuns(explorerFocus bool) []labelRun {
	titles := barLabels(explorerFocus)
	var runs []labelRun
	x := labelStart
	for _, title := range titles {
		w := ansi.StringWidth(title)
		runs = append(runs, labelRun{x: x, w: w, title: title})
		x += w + labelGap
	}
	return runs
}

// barFit returns the leading runs that a w-column bar shows. When the last run does not
// fit, the labels shown end before the last moreCols columns, and more is true: the
// marker takes the last column.
func barFit(runs []labelRun, w int) (shown []labelRun, more bool) {
	if len(runs) == 0 {
		return runs, false
	}
	if last := runs[len(runs)-1]; last.x+last.w <= w {
		return runs, false
	}
	n := 0
	for n < len(runs) && runs[n].x+runs[n].w <= w-moreCols {
		n++
	}
	return runs[:n], true
}

// titleRun is the bar run that a title occupies in a w-column frame: its label, or the
// marker in the last column. ok is false for a label the bar does not have.
func titleRun(runs []labelRun, title string, w int) (labelRun, bool) {
	if title == moreTitle {
		return labelRun{x: w - 1, w: 1, title: moreTitle}, true
	}
	for _, r := range runs {
		if r.title == title {
			return r, true
		}
	}
	return labelRun{}, false
}

// dropBox is a dropdown rectangle: the dropdown of a label, or a submenu. Its top
// border is row top, its rows start at row top+1 and its bottom border is row
// bottom. When the rows do not fit, it shows the rows from index first.
type dropBox struct {
	x, w, bottom int
	top          int
	first        int
	rows         []Row
	selected     int
}

// visible is the number of rows the box shows.
func (b dropBox) visible() int { return b.bottom - b.top - 1 }

// geometry is the placement of the bar and of the open dropdown in a frame.
type geometry struct {
	labels []labelRun
	box    dropBox
	hasBox bool
	sub    dropBox
	hasSub bool
}

// geometry computes the placement for a w by h frame. The dropdown hangs one
// column left of its label, and is moved left when it would pass the right edge.
// Its width is the widest row with its shortcut, plus space.PadX on both sides and
// the two borders. Its bottom border stops above the status row, and a dropdown with
// more rows than fit shows the window that holds the highlighted row. The submenu
// of the highlighted row opens beside the dropdown, on its right when there is
// room and on its left otherwise.
func (m Model) geometry(w, h int) geometry {
	runs := labelRuns(m.explorerFocus)
	shown, more := barFit(runs, w)
	g := geometry{labels: shown}
	if more {
		// A copy: shown shares the backing array of runs, which the lookup below reads.
		g.labels = append(slices.Clone(shown), labelRun{x: w - 1, w: 1, title: moreTitle})
	}
	if !m.IsOpen() {
		return g
	}
	// The dropdown of a label the bar does not show still hangs from where it is, clamped
	// into the frame, so the keys can open it there.
	if r, ok := titleRun(runs, m.title, w); ok {
		g.box = layoutBox(m.rows, m.selected, m.scroll, r.x-1, 1, w, h)
		g.hasBox = usable(g.box)
	}
	if m.sub.open && g.hasBox {
		children := m.rows[m.sub.parent].children
		g.sub = m.subBox(g, children, w, h)
		g.hasSub = usable(g.sub)
	}
	return g
}

// subBox places the submenu of the highlighted row. It opens beside the dropdown,
// level with its parent row, on the right when there is room and on the left
// otherwise. When neither side has room it is placed against the frame's right edge
// and moved off the parent row, below it or above it, so the parent's label stays
// visible.
func (m Model) subBox(g geometry, children []Row, w, h int) dropBox {
	rowY := g.box.top + 1 + m.sub.parent - g.box.first
	top := max(1, min(rowY-1, h-3-len(children)))
	x := g.box.x + g.box.w
	bw := min(w, boxWidth(children))
	if x+bw <= w {
		return layoutBox(children, m.sub.selected, m.sub.scroll, x, top, w, h)
	}
	if g.box.x-bw >= 0 {
		return layoutBox(children, m.sub.selected, m.sub.scroll, g.box.x-bw, top, w, h)
	}
	x = w - bw
	if below := rowY + 1; below+1+len(children) <= h-2 {
		top = below
	} else if above := rowY - 2 - len(children); above >= 1 {
		top = above
	} else {
		top = min(rowY+1, max(h-4, 1))
	}
	return layoutBox(children, m.sub.selected, m.sub.scroll, x, top, w, h)
}

// usable reports whether a box is big enough to draw.
func usable(b dropBox) bool { return b.w >= 4 && b.visible() >= 1 }

// layoutBox places the rows in a box whose top border is row top, at column x or as
// near as the w by h frame allows. The last row of the frame is the status row, so
// the bottom border is at most on the row above it.
func layoutBox(rows []Row, selected, scroll, x, top, w, h int) dropBox {
	bw := min(w, boxWidth(rows))
	visible := min(len(rows), max(h-3-top, 0))
	first := min(max(scroll, 0), len(rows)-visible)
	if selected >= 0 {
		first = max(min(first, selected), selected-visible+1)
	}
	return dropBox{
		x: min(max(x, 0), max(w-bw, 0)), w: bw,
		top: top, bottom: top + 1 + visible,
		first: first, rows: rows, selected: selected,
	}
}

// reveal keeps the highlighted rows inside the window of the dropdown and of the
// submenu, after the highlight or the frame size changed.
func (m Model) reveal() Model {
	g := m.geometry(m.w, m.h)
	if g.hasBox {
		m.scroll = g.box.first
	}
	if g.hasSub {
		m.sub.scroll = g.sub.first
	}
	return m
}

// checkboxWidth is the width of the box a checkbox row draws before its label.
const checkboxWidth = 2

// contentWidth is the width of the widest row: its checkbox, its label, and its
// shortcut after one blank. Separators do not count.
func contentWidth(rows []Row) int {
	content := 0
	for _, r := range rows {
		if r.Separator {
			continue
		}
		cw := ansi.StringWidth(r.Label)
		if r.Checkbox {
			cw += checkboxWidth
		}
		if r.Keys != "" {
			cw += 1 + ansi.StringWidth(r.Keys)
		}
		content = max(content, cw)
	}
	return content
}

// boxWidth is the width of a dropdown that shows rows: the content between the
// two borders, with space.PadX of padding on each side.
func boxWidth(rows []Row) int { return contentWidth(rows) + 2 + 2*space.PadX }

// Hit returns what is at column x, row y of the frame set by SetSize.
func (m Model) Hit(x, y int) Target {
	g := m.geometry(m.w, m.h)
	if x < 0 || y < 0 || x >= m.w || y >= m.h {
		return Target{Kind: TargetNone}
	}
	if y == 0 {
		for i, r := range g.labels {
			if x >= r.x && x < r.x+r.w {
				if r.title == moreTitle {
					return Target{Kind: TargetMore}
				}
				return Target{Kind: TargetLabel, Index: i}
			}
		}
		return Target{Kind: TargetNone}
	}
	if g.hasSub {
		if t, ok := hitBox(g.sub, x, y, TargetSubItem, TargetSubBorder); ok {
			return t
		}
	}
	if g.hasBox {
		if t, ok := hitBox(g.box, x, y, TargetItem, TargetBorder); ok {
			return t
		}
	}
	return Target{Kind: TargetNone}
}

// hitBox returns what is at (x, y) of box: a border, a separator, or a row of the
// kind item. The border kind says which box the border belongs to. ok is false
// outside the box.
func hitBox(box dropBox, x, y int, item, border TargetKind) (Target, bool) {
	if x < box.x || x >= box.x+box.w || y < box.top || y > box.bottom {
		return Target{}, false
	}
	if y == box.top || y == box.bottom || x == box.x || x == box.x+box.w-1 {
		return Target{Kind: border}, true
	}
	i := box.first + y - box.top - 1
	if i >= len(box.rows) || box.rows[i].Separator {
		return Target{Kind: border}, true
	}
	return Target{Kind: item, Index: i}, true
}

// Paint draws the bar on row 0 of g and, when a dropdown is open, the dropdown
// below it. The open label takes the active style, and the highlighted row takes
// the selected style. A disabled row is drawn dim. No cell is underlined.
func (m Model) Paint(g *grid.Grid) {
	geo := m.geometry(g.Width(), g.Height())
	g.Fill(0, g.Width(), 0, m.styles.bar)
	for _, r := range geo.labels {
		st := m.styles.bar
		if r.title == m.title {
			st = m.styles.activeLabel
		}
		g.Put(r.x, 0, g.Width(), r.title, st)
	}
	if geo.hasBox {
		m.paintBox(g, geo.box)
	}
	if geo.hasSub {
		m.paintBox(g, geo.sub)
	}
}

// checkGlyph is the box a checkbox row draws before its label.
func checkGlyph(checked bool) string {
	if checked {
		return "☑ "
	}
	return "☐ "
}

// paintBox draws the rows of box in view: the rounded border, the rows with their
// shortcuts, and the separator rules.
func (m Model) paintBox(g *grid.Grid, box dropBox) {
	b := lipgloss.RoundedBorder()
	bg := m.styles.box
	border := m.styles.border
	x0, right := box.x, box.x+box.w-1
	rule := strings.Repeat(b.Top, box.w-2)
	g.Put(x0, box.top, x0+box.w, b.TopLeft+rule+b.TopRight, border)
	for k := range box.visible() {
		i := box.first + k
		y := box.top + 1 + k
		r := box.rows[i]
		st := bg
		switch {
		case r.Separator:
		case i == box.selected:
			st = m.styles.selected
		case r.Disabled:
			st = m.styles.disabled
		}
		g.Fill(x0, x0+box.w, y, bg)
		g.Set(x0, y, b.Left, border)
		g.Set(right, y, b.Right, border)
		if r.Separator {
			g.Put(x0+1+space.PadX, y, right-space.PadX, strings.Repeat("─", max(box.w-2-2*space.PadX, 0)), bg)
			continue
		}
		g.Fill(x0+1, right, y, st)
		edge := right - space.PadX
		x := x0 + 1 + space.PadX
		if r.Checkbox {
			g.Put(x, y, edge, checkGlyph(r.Checked), st)
			x += checkboxWidth
		}
		g.Put(x, y, edge, r.Label, st)
		if r.Keys != "" {
			g.Put(edge-ansi.StringWidth(r.Keys), y, edge, r.Keys, st)
		}
	}
	g.Put(x0, box.bottom, x0+box.w, b.BottomLeft+rule+b.BottomRight, border)
	paintMore(g, box, border)
}

// styles are the grid styles of the bar and the dropdown. The bar row keeps the
// theme, and the dropdown takes the overlay palette.
type styles struct {
	bar, activeLabel, box, border, selected, disabled grid.Style
}

// newStyles derives the menu roles from the chrome styles and the overlay
// palette. The open title and the highlighted row take the selected colors, and
// the dropdown border takes the primary color. A disabled row is faint.
func newStyles(st style.Styles) styles {
	ov := st.Overlay
	disabled := ov.Box
	disabled.Faint = true
	return styles{
		bar:         style.Cell(st.Menu),
		activeLabel: ov.Selected,
		box:         ov.Box,
		border:      ov.Border,
		selected:    ov.Selected,
		disabled:    disabled,
	}
}
