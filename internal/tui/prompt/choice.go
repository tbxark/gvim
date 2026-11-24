package prompt

// Choice is a centred card with several labelled answers, each with its own mnemonic
// key. It is the multi-entry paste-conflict dialog (Overwrite / Overwrite All / Skip /
// Skip All / Cancel): Fresh's conflict dialog is one of these and its keys are
// case-sensitive. Esc closes it like every prompt (no answer); a key that is not a mnemonic
// leaves it open.

import (
	"github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/overlay"
	"github.com/tbxark/gvim/internal/tui/space"
	"github.com/tbxark/gvim/internal/tui/style"
)

// ChoiceOption is one answer of a Choice prompt: the label and the exact key that answers
// it (case-sensitive, so "o" and "O" can differ).
type ChoiceOption struct {
	Label string
	Key   string
}

// Choice is the multi-answer prompt.
type Choice struct {
	title  string
	label  string
	tag    any
	styles styles
	opts   []ChoiceOption
	focus  int
	closed bool
	hits   []space.Rect
}

// NewChoice returns a prompt asking label with the given options; the first option is
// focused. An empty title falls back to "Choose". Esc closes without an answer.
func NewChoice(st style.Styles, title, label string, tag any, opts []ChoiceOption) *Choice {
	return &Choice{title: cardTitle(title, "gvim.prompt.title.choose", "Choose"), label: label, tag: tag, styles: newStyles(st), opts: opts}
}

// Active reports whether the prompt is open.
func (c *Choice) Active() bool { return !c.closed }

// AlertOverlay marks the choice prompt as an alert: it keeps the input until the
// user answers it. See overlay.Alert.
func (*Choice) AlertOverlay() {}

// Update handles one key: Esc closes without an answer; Enter answers the focused option;
// left/right/tab move the focus; an option's exact mnemonic key answers it. Anything else
// leaves the prompt open (a typo must not drop a paste queue).
func (c *Choice) Update(msg tea.Msg) (overlay.Overlay, tea.Cmd) {
	if m, ok := msg.(tea.MouseClickMsg); ok && m.Button == tea.MouseLeft {
		for i, hit := range c.hits {
			if hit.Contains(m.X, m.Y) {
				return c.answer(i)
			}
		}
	}
	kp, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return c, nil
	}
	switch tea.Key(kp).Keystroke() {
	case "esc":
		c.closed = true
		return c, nil
	case "enter":
		return c.answer(c.focus)
	case "left", "up", "shift+tab":
		if c.focus > 0 {
			c.focus--
		}
		return c, nil
	case "right", "down", "tab":
		if c.focus+1 < len(c.opts) {
			c.focus++
		}
		return c, nil
	}
	for i, opt := range c.opts {
		if kp.Text == opt.Key {
			return c.answer(i)
		}
	}
	return c, nil
}

// answer closes the prompt and reports option i.
func (c *Choice) answer(i int) (*Choice, tea.Cmd) {
	c.closed = true
	return c, done(Done{Tag: c.tag, Choice: i})
}

// buttonText is one option's button label: its mnemonic and its text.
func (c *Choice) buttonText(i int) string {
	return c.opts[i].Key + " " + c.opts[i].Label
}

// buttonRows groups the options into button rows of at most cw cells: a button takes
// its padded label, two buttons sit cardGap apart, and a button that does not fit the
// rest of a row starts a new one.
func (c *Choice) buttonRows(cw int) [][]int {
	rows := [][]int{{}}
	width := 0
	for i := range c.opts {
		row := rows[len(rows)-1]
		bw := c.styles.ov.ButtonWidth(c.buttonText(i))
		if len(row) > 0 && width+cardGap+bw > cw {
			rows = append(rows, []int{i})
			width = bw
			continue
		}
		if len(row) > 0 {
			width += cardGap
		}
		rows[len(rows)-1] = append(row, i)
		width += bw
	}
	return rows
}

// Draw paints the question in a centred card: the title on the border, the wrapped
// question, one blank row, then the buttons, which flow onto further centred rows
// when they do not fit the content width. The focused option takes the active style;
// every option's hit rectangle is recorded for a click.
func (c *Choice) Draw(g *grid.Grid) {
	c.hits = make([]space.Rect, len(c.opts))
	ov := c.styles.ov
	natural := ansi.StringWidth(c.title) + space.CardChrome
	natural = max(natural, ansi.StringWidth(c.label))
	for i := range c.opts {
		natural = max(natural, ov.ButtonWidth(c.buttonText(i)))
	}
	cw := space.CardWidth(g.Width(), natural)
	lines := space.Wrap(c.label, cw, 1<<10)
	rows := c.buttonRows(cw)
	// The body is the wrapped question, one blank row and the button rows.
	box := space.CardBox(g.Width(), g.Height(), cw, len(lines)+1+len(rows))
	ov.PaintBox(g, box.X, box.Y, box.W, box.H, c.title)
	x0, maxX := box.X+2, box.X+2+cw
	y := box.Y + 1
	for _, line := range lines {
		g.Put(x0, y, maxX, line, ov.Box)
		y++
	}
	y++ // the blank row above the buttons
	for _, row := range rows {
		total := -cardGap
		for _, i := range row {
			total += ov.ButtonWidth(c.buttonText(i)) + cardGap
		}
		x := x0 + max((cw-total)/2, 0)
		for _, i := range row {
			st := c.styles.field
			if i == c.focus {
				st = c.styles.active
			}
			n := ov.PaintButton(g, x, y, c.buttonText(i), -1, st)
			c.hits[i] = space.Rect{X: x, Y: y, W: n, H: 1}
			x += n + cardGap
		}
		y++
	}
}

// Cursor reports no caret: a choice has no text field.
func (c *Choice) Cursor(w, h int) (x, y int, ok bool) {
	return 0, 0, false
}

var _ overlay.Overlay = (*Choice)(nil)
