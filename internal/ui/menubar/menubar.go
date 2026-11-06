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
// Every title and every dropdown row has an accelerator: the first letter of its
// label that no earlier label in the same list took. Paint underlines that one
// cell and nothing else.
package menubar

import (
	"errors"
	"slices"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/ui/grid"
	"github.com/tbxark/gvim/internal/ui/style"
)

// labelGap is the number of blank cells between two labels of the bar.
const labelGap = 3

// labelStart is the column of the first label.
const labelStart = 1

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
	TargetNone   TargetKind = iota // outside the bar and the open dropdown
	TargetLabel                    // a bar label; Index is its position in Labels
	TargetItem                     // a dropdown row; Index is its position in Rows
	TargetBorder                   // a dropdown border or separator
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
}

// keys are the bindings the open dropdown answers. Every other key falls through,
// except a letter that is the accelerator of a row, which Consumes reports.
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
	m.w, m.h = w, h
	return m
}

// WithExplorerFocus sets whether the explorer label joins the bar. An open
// dropdown whose label leaves the bar closes.
func (m Model) WithExplorerFocus(on bool) Model {
	m.explorerFocus = on
	if m.title != "" && !slices.Contains(barLabels(on), m.title) {
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

// Select activates the highlighted row, as Enter does. The dropdown closes
// whether or not the row ran. It does nothing when the bar is closed.
func (m Model) Select() (Model, tea.Cmd) {
	if !m.IsOpen() {
		return m, nil
	}
	if m.selected < 0 {
		return m.Close(), nil
	}
	row := m.rows[m.selected]
	return m.Close().run(row)
}

// Accelerator opens the title whose accelerator is letter (case-insensitive),
// whether or not a dropdown is open. It is the route for Alt+letter. It reports
// false, and leaves the model as it is, when no title has that letter. A letter
// in an open dropdown is a row accelerator and reaches Update instead.
func (m Model) Accelerator(letter rune) (Model, bool) {
	letter = unicode.ToLower(letter)
	for i, r := range labelRuns(m.explorerFocus) {
		if letter != 0 && r.acc.key == letter {
			return m.Open(i), true
		}
	}
	return m, false
}

// Close closes the dropdown. It does nothing when the bar is closed.
func (m Model) Close() Model {
	m.title, m.rows, m.selected = "", nil, -1
	return m
}

// Consumes reports whether msg belongs to the open dropdown, so the root must not
// route it further. A key the dropdown answers is consumed: the arrows, Enter,
// Esc, and a letter that is the accelerator of a row. A left press on a label, a
// row or a border is consumed. A press anywhere else is not, because it closes
// the dropdown and still reaches what is under it. Hover is not consumed.
func (m Model) Consumes(msg tea.Msg) bool {
	if !m.IsOpen() {
		return false
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if key.Matches(msg, keys.Up, keys.Down, keys.Left, keys.Right, keys.Enter, keys.Close) {
			return true
		}
		return m.rowWithAccel(letterOf(msg)) >= 0
	case tea.MouseClickMsg:
		mo := msg.Mouse()
		return mo.Button == tea.MouseLeft && m.Hit(mo.X, mo.Y).Kind != TargetNone
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
	}
	return m, nil
}

// key runs one key press in an open dropdown. Keys it does not answer are left
// to the caller, which the Consumes check reports.
func (m Model) key(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Close):
		return m.Close(), nil
	case key.Matches(msg, keys.Up):
		m.selected = m.step(m.selected, -1)
	case key.Matches(msg, keys.Down):
		m.selected = m.step(m.selected, 1)
	case key.Matches(msg, keys.Left):
		m = m.Prev()
	case key.Matches(msg, keys.Right):
		m = m.Next()
	case key.Matches(msg, keys.Enter):
		return m.Select()
	default:
		if i := m.rowWithAccel(letterOf(msg)); i >= 0 {
			row := m.rows[i]
			return m.Close().run(row)
		}
	}
	return m, nil
}

// click handles a left press. A label toggles its dropdown, a row runs, a border
// closes, and any other place closes the dropdown and passes the press on.
func (m Model) click(mo tea.Mouse) (Model, tea.Cmd) {
	t := m.Hit(mo.X, mo.Y)
	switch t.Kind {
	case TargetLabel:
		title := Labels(m.explorerFocus)[t.Index]
		if m.IsOpen() && title == m.title {
			return m.Close(), nil
		}
		return m.openTitle(title), nil
	case TargetItem:
		row := m.rows[t.Index]
		return m.Close().run(row)
	}
	return m.Close(), nil
}

// hover follows the pointer while a dropdown is open. A label that is not the
// open one switches to its dropdown, and a selectable row under the pointer is
// highlighted.
func (m Model) hover(mo tea.Mouse) Model {
	t := m.Hit(mo.X, mo.Y)
	switch t.Kind {
	case TargetLabel:
		if title := Labels(m.explorerFocus)[t.Index]; title != m.title {
			return m.openTitle(title)
		}
	case TargetItem:
		if m.rows[t.Index].selectable() {
			m.selected = t.Index
		}
	}
	return m
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
// highlighted. The rows are read from the table when the dropdown opens.
func (m Model) openTitle(title string) Model {
	m.title = title
	m.rows = dropdownRows(title, m.holds)
	m.selected = m.step(-1, 1)
	return m
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

// step returns the index of the next selectable row from start in direction dir
// (+1 or -1). It stays at start when no row is found.
func (m Model) step(start, dir int) int {
	for i := start + dir; i >= 0 && i < len(m.rows); i += dir {
		if m.rows[i].selectable() {
			return i
		}
	}
	return start
}

// rowWithAccel returns the index of the row whose accelerator is letter, or -1.
// A zero letter matches nothing.
func (m Model) rowWithAccel(letter rune) int {
	if letter == 0 {
		return -1
	}
	for i, r := range m.rows {
		if r.acc.key == letter {
			return i
		}
	}
	return -1
}

// holds evaluates a context key through the ports. Without ports nothing holds.
func (m Model) holds(name string) bool {
	return m.ports != nil && m.ports.Holds(name)
}

// labelRun is one bar label: its first column, its width, its text and the
// accelerator that opens it.
type labelRun struct {
	x, w  int
	title string
	acc   accel
}

// labelRuns places the bar labels with labelGap blanks between them.
func labelRuns(explorerFocus bool) []labelRun {
	titles := barLabels(explorerFocus)
	accels := pickAccels(titles)
	var runs []labelRun
	x := labelStart
	for i, title := range titles {
		w := ansi.StringWidth(title)
		runs = append(runs, labelRun{x: x, w: w, title: title, acc: accels[i]})
		x += w + labelGap
	}
	return runs
}

// dropBox is the open dropdown rectangle. Its top border is row 1, its bottom
// border is row bottom, and its rows start at row 2.
type dropBox struct {
	x, w, bottom int
}

// geometry is the placement of the bar and of the open dropdown in a frame.
type geometry struct {
	labels []labelRun
	box    dropBox
	hasBox bool
}

// geometry computes the placement for a w by h frame. The dropdown hangs one
// column left of its label. Its width is the widest row with its shortcut, plus
// a two-cell margin and the right border. It is clipped to the frame width, and
// its bottom border stops above the last row.
func (m Model) geometry(w, h int) geometry {
	g := geometry{labels: labelRuns(m.explorerFocus)}
	if !m.IsOpen() {
		return g
	}
	for _, r := range g.labels {
		if r.title != m.title {
			continue
		}
		box := dropBox{
			x:      max(r.x-1, 0),
			w:      min(w, contentWidth(m.rows)+3),
			bottom: min(2+len(m.rows), h-1),
		}
		g.box = box
		g.hasBox = box.w >= 4 && box.bottom >= 2
	}
	return g
}

// contentWidth is the width of the widest row: its label, and its shortcut
// after one blank. Separators do not count.
func contentWidth(rows []Row) int {
	content := 0
	for _, r := range rows {
		if r.Separator {
			continue
		}
		cw := ansi.StringWidth(r.Label)
		if r.Keys != "" {
			cw += 1 + ansi.StringWidth(r.Keys)
		}
		content = max(content, cw)
	}
	return content
}

// Hit returns what is at column x, row y of the frame set by SetSize.
func (m Model) Hit(x, y int) Target {
	g := m.geometry(m.w, m.h)
	if x < 0 || y < 0 || x >= m.w || y >= m.h {
		return Target{Kind: TargetNone}
	}
	if y == 0 {
		for i, r := range g.labels {
			if x >= r.x && x < r.x+r.w {
				return Target{Kind: TargetLabel, Index: i}
			}
		}
		return Target{Kind: TargetNone}
	}
	if !g.hasBox || x < g.box.x || x >= g.box.x+g.box.w || y > g.box.bottom {
		return Target{Kind: TargetNone}
	}
	if y == 1 || y == g.box.bottom {
		return Target{Kind: TargetBorder}
	}
	i := y - 2
	if i >= len(m.rows) || m.rows[i].Separator {
		return Target{Kind: TargetBorder}
	}
	return Target{Kind: TargetItem, Index: i}
}

// Paint draws the bar on row 0 of g and, when a dropdown is open, the dropdown
// below it. The open label takes the active style, and the highlighted row takes
// the selected style. A disabled row is drawn dim. The accelerator of each label
// and row is underlined, and no other cell is.
func (m Model) Paint(g *grid.Grid) {
	geo := m.geometry(g.Width(), g.Height())
	g.Fill(0, g.Width(), 0, m.styles.bar)
	for _, r := range geo.labels {
		st := m.styles.bar
		if r.title == m.title {
			st = m.styles.activeLabel
		}
		putText(g, r.x, 0, g.Width(), r.title, st, r.acc)
	}
	if geo.hasBox {
		m.paintBox(g, geo.box)
	}
}

// paintBox draws the dropdown rows inside box: the rounded border, the rows with
// their shortcuts, and the separator rules.
func (m Model) paintBox(g *grid.Grid, box dropBox) {
	b := lipgloss.RoundedBorder()
	bg := m.styles.box
	border := m.styles.border
	x0, right := box.x, box.x+box.w-1
	rule := strings.Repeat(b.Top, box.w-2)
	g.Put(x0, 1, x0+box.w, b.TopLeft+rule+b.TopRight, border)
	for i, r := range m.rows {
		y := 2 + i
		if y >= box.bottom {
			break
		}
		st := bg
		switch {
		case r.Separator:
		case i == m.selected:
			st = m.styles.selected
		case r.Disabled:
			st = m.styles.disabled
		}
		g.Fill(x0, x0+box.w, y, bg)
		g.Set(x0, y, b.Left, border)
		g.Set(right, y, b.Right, border)
		if r.Separator {
			g.Put(x0+2, y, right, strings.Repeat("─", box.w-3), bg)
			continue
		}
		g.Fill(x0+1, right, y, st)
		putText(g, x0+2, y, right, r.Label, st, r.acc)
		if r.Keys != "" {
			g.Put(right-ansi.StringWidth(r.Keys), y, right, r.Keys, st)
		}
	}
	g.Put(x0, box.bottom, x0+box.w, b.BottomLeft+rule+b.BottomRight, border)
}

// putText writes text at (x, y), clipped to the column maxX, and underlines the
// cell of its accelerator letter. The underline is set on that one cell, so the
// spaces around it keep the plain style.
func putText(g *grid.Grid, x, y, maxX int, text string, st grid.Style, acc accel) {
	g.Put(x, y, maxX, text, st)
	if acc.key == 0 || x+acc.col >= maxX {
		return
	}
	under := st
	under.Underline = true
	g.Set(x+acc.col, y, acc.text, under)
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
