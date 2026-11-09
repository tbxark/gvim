package prompt

// Choice is a one-line overlay with several labelled answers, each with its own mnemonic
// key. It is the multi-entry paste-conflict dialog (Overwrite / Overwrite All / Skip /
// Skip All / Cancel): Fresh's conflict dialog is one of these and its keys are
// case-sensitive. Esc closes it like every prompt (no answer); a key that is not a mnemonic
// leaves it open.

import (
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
	label  string
	tag    any
	styles styles
	opts   []ChoiceOption
	focus  int
	closed bool
}

// NewChoice returns a prompt asking label with the given options; the first option is
// focused. Esc closes without an answer.
func NewChoice(st style.Styles, label string, tag any, opts []ChoiceOption) *Choice {
	return &Choice{label: label, tag: tag, styles: newStyles(st), opts: opts}
}

// Active reports whether the prompt is open.
func (c *Choice) Active() bool { return !c.closed }

// Update handles one key: Esc closes without an answer; Enter answers the focused option;
// left/right/tab move the focus; an option's exact mnemonic key answers it. Anything else
// leaves the prompt open (a typo must not drop a paste queue).
func (c *Choice) Update(msg tea.Msg) (overlay.Overlay, tea.Cmd) {
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

// Draw paints the question and the options on the bottom row. Each option shows its
// mnemonic in the button; the focused one takes the active style.
func (c *Choice) Draw(g *grid.Grid) {
	y := promptRow(g.Height())
	if y < 0 {
		return
	}
	w := g.Width()
	g.Fill(0, w, y, c.styles.bar)
	x := space.PadX + g.Put(space.PadX, y, w, c.label+" ", c.styles.label)
	for i, opt := range c.opts {
		st := c.styles.field
		if i == c.focus {
			st = c.styles.active
		}
		text := "[ " + opt.Key + " " + opt.Label + " ]"
		x += g.Put(x, y, w, text, st)
		if i+1 < len(c.opts) {
			x += g.Put(x, y, w, " ", c.styles.bar)
		}
	}
}

// Cursor reports no caret: a choice has no text field.
func (c *Choice) Cursor(w, h int) (x, y int, ok bool) {
	return 0, 0, false
}

var _ overlay.Overlay = (*Choice)(nil)
