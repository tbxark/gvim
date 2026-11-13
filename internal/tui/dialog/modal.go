package dialog

import (
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/style"
)

// The grace period. Enter is ignored for graceQuiet after the dialog opens.
// Each ignored Enter extends the window by graceQuiet, up to graceMax after
// opening (crush dialog.go:63 and :67 use the same two numbers).
const (
	graceQuiet = 425 * time.Millisecond
	graceMax   = 1500 * time.Millisecond
)

// button is one answer of a dialog. under is the index, in runes, of the
// letter that is underlined, and key is that letter in lower case. The index
// is set explicitly by the dialog, never inferred.
type button struct {
	label  string
	under  int
	key    rune
	choice Choice
}

// newButton returns a button whose hotkey is the letter at index under of label.
func newButton(label string, under int, c Choice) button {
	r := []rune(label)
	return button{label: label, under: under, key: unicode.ToLower(r[under]), choice: c}
}

// modal is the state every dialog shares: the title, the buttons and their
// focus, the grace period, the optional text field and recent list, and the
// geometry of the last draw. Focus is a stop index. The field (when there is
// one) is stop 0, the list (when there is one) comes next, and then the buttons.
type modal struct {
	clock    Clock
	title    string
	tag      any
	escape   Choice // the answer Esc gives
	closed   bool
	opened   time.Time
	graceEnd time.Time
	buttons  []button
	ov       style.Overlay // the overlay styles the box is drawn with
	focus    int
	hover    int // the button under the pointer, or -1
	errText  string
	field    *field
	list     *recentList
	onPress  func(i int) tea.Cmd // activates button i
	onPick   func() tea.Cmd      // Enter on the list

	// Geometry of the last draw, used for hit tests and the caret.
	box     rect
	hits    []rect // one per button, in order
	fieldAt rect
	caret   point
	caretOK bool
}

// setup prepares the shared state. The dialog opens now, so its grace period
// starts now.
func (m *modal) setup(clock Clock, title string, tag any, escape Choice, buttons []button, ov style.Overlay) {
	if clock == nil {
		clock = time.Now
	}
	m.clock = clock
	m.title = title
	m.tag = tag
	m.escape = escape
	m.buttons = buttons
	m.ov = ov
	m.opened = clock()
	m.graceEnd = m.opened.Add(graceQuiet)
	m.hover = -1
	m.onPress = m.choose
}

// Active reports whether the dialog is still open.
func (m *modal) Active() bool { return !m.closed }

// AlertOverlay marks every dialog as an alert: it keeps the input until the user
// answers it, and a transient overlay a background request opens goes under it. See
// overlay.Alert.
func (*modal) AlertOverlay() {}

// Cursor returns the caret when the text field has the focus.
func (m *modal) Cursor(w, h int) (int, int, bool) {
	if !m.caretOK || !m.fieldFocused() {
		return 0, 0, false
	}
	if m.caret.x >= w || m.caret.y >= h {
		return 0, 0, false
	}
	return m.caret.x, m.caret.y, true
}

// advance handles one message and returns the overlay that takes this one's
// place: self while the dialog is open, nil once it has answered.
func (m *modal) advance(self Overlay, msg tea.Msg) (Overlay, tea.Cmd) {
	cmd := m.update(msg)
	if m.closed {
		return nil, cmd
	}
	return self, cmd
}

// update routes one message and returns the command it starts.
func (m *modal) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.key(msg)
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			return m.click(msg.X, msg.Y)
		}
	case tea.MouseMotionMsg:
		m.hover = m.buttonAt(msg.X, msg.Y)
	default:
		if m.field != nil {
			before := m.field.value()
			m.field.update(msg)
			if m.field.value() != before {
				m.errText = ""
			}
		}
	}
	return nil
}

// key handles one key press.
func (m *modal) key(kp tea.KeyPressMsg) tea.Cmd {
	keystroke := tea.Key(kp).Keystroke()
	switch keystroke {
	case "esc":
		return m.finish(m.escape, "")
	case "enter":
		if m.inGrace() {
			m.extendGrace()
			return nil
		}
		if i, ok := m.buttonFocus(); ok {
			return m.onPress(i)
		}
		if m.listFocused() {
			return m.onPick()
		}
		return m.onPress(0)
	case "tab":
		m.step(1)
		return nil
	case "shift+tab":
		m.step(-1)
		return nil
	case "left", "right":
		if i, ok := m.buttonFocus(); ok {
			d := 1
			if keystroke == "left" {
				d = -1
			}
			n := len(m.buttons)
			m.focus = m.lead() + (i+d+n)%n
			return nil
		}
	case "up", "down":
		if m.list != nil {
			d := 1
			if keystroke == "up" {
				d = -1
			}
			m.focus = m.listStop()
			m.list.move(d)
			return nil
		}
	}
	if i, ok := m.hotkey(kp); ok {
		return m.onPress(i)
	}
	if m.field != nil {
		m.focus = 0
		before := m.field.value()
		cmd := m.field.key(kp)
		if m.field.value() != before {
			m.errText = ""
		}
		return cmd
	}
	return nil
}

// hotkey returns the button whose letter kp types, if any. Without a text
// field a plain letter (Shift allowed) activates. With one, Alt plus the letter
// activates, because the letter itself types.
func (m *modal) hotkey(kp tea.KeyPressMsg) (int, bool) {
	var r rune
	if m.field != nil {
		if kp.Mod&tea.ModAlt == 0 || kp.Mod&tea.ModCtrl != 0 {
			return 0, false
		}
		r = kp.Code
	} else {
		if kp.Mod&^tea.ModShift != 0 {
			return 0, false
		}
		rs := []rune(kp.Text)
		if len(rs) != 1 {
			return 0, false
		}
		r = rs[0]
	}
	r = unicode.ToLower(r)
	for i, b := range m.buttons {
		if b.key == r {
			return i, true
		}
	}
	return 0, false
}

// click handles a left click at (x, y).
func (m *modal) click(x, y int) tea.Cmd {
	if i := m.buttonAt(x, y); i >= 0 {
		m.setFocusButton(i)
		return m.onPress(i)
	}
	if m.field != nil && m.fieldAt.Contains(x, y) {
		m.focus = 0
		return nil
	}
	if m.list != nil {
		if k := m.list.rowAt(x, y); k >= 0 {
			m.focus = m.listStop()
			m.list.sel = m.list.top + k
		}
	}
	return nil
}

// buttonAt returns the index of the button drawn at (x, y), or -1.
func (m *modal) buttonAt(x, y int) int {
	for i, r := range m.hits {
		if r.Contains(x, y) {
			return i
		}
	}
	return -1
}

// inGrace reports whether Enter is still ignored.
func (m *modal) inGrace() bool {
	return m.clock().Before(m.graceEnd)
}

// extendGrace pushes the grace window out by graceQuiet from now, but not past
// graceMax after opening.
func (m *modal) extendGrace() {
	end := m.clock().Add(graceQuiet)
	if limit := m.opened.Add(graceMax); end.After(limit) {
		end = limit
	}
	if end.After(m.graceEnd) {
		m.graceEnd = end
	}
}

// finish closes the dialog with the answer c and returns the Result command.
func (m *modal) finish(c Choice, text string) tea.Cmd {
	m.closed = true
	return result(Result{Tag: m.tag, Choice: c, Text: text})
}

// choose answers with the choice of button i. It is the default onPress.
func (m *modal) choose(i int) tea.Cmd {
	return m.finish(m.buttons[i].choice, "")
}

// lead is the number of stops before the buttons: the field and the list.
func (m *modal) lead() int {
	return b2i(m.field != nil) + b2i(m.list != nil)
}

// listStop is the stop index of the list. It follows the field when there is one.
func (m *modal) listStop() int {
	return b2i(m.field != nil)
}

// fieldFocused reports whether the text field has the focus.
func (m *modal) fieldFocused() bool {
	return m.field != nil && m.focus == 0
}

// listFocused reports whether the list has the focus.
func (m *modal) listFocused() bool {
	return m.list != nil && m.focus == m.listStop()
}

// buttonFocus returns the index of the focused button, and false when a field
// or the list has the focus.
func (m *modal) buttonFocus() (int, bool) {
	if m.focus < m.lead() {
		return 0, false
	}
	return m.focus - m.lead(), true
}

// setFocusButton focuses button i.
func (m *modal) setFocusButton(i int) {
	m.focus = m.lead() + i
}

// step moves the focus d stops, wrapping around.
func (m *modal) step(d int) {
	n := m.lead() + len(m.buttons)
	m.focus = (m.focus + d + n) % n
}

// b2i is 1 for true and 0 for false.
func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
