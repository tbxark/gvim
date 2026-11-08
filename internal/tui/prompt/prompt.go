// Package prompt holds the overlays that take one answer at the bottom row of
// the frame: a one-line text prompt (go to line, save as, the explorer's new
// names) and a Yes/No confirm (overwrite, delete).
//
// A prompt never acts on the editor or the file system. When it ends, it emits
// a Done message as a command, and the root applies the answer. Done carries the
// Tag the opener gave the prompt, so one root handler can serve several prompts.
// The root wraps *Line and *Confirm in its Overlay type, as it does for the find
// bar.
//
// Esc is not forwarded to an overlay (the root pops it), so a prompt that is
// closed with Esc emits no Done. The effect is the same as a No: nothing runs.
package prompt

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/space"
	"github.com/tbxark/gvim/internal/tui/style"
)

// Outcome is how a prompt ended.
type Outcome int

const (
	// Submitted is Enter on a text prompt. Done.Text holds the input.
	Submitted Outcome = iota
	// Yes is y, or Enter with Yes focused on a confirm.
	Yes
	// No is n, or Enter with No focused on a confirm.
	No
)

// Done is the message a prompt emits as a command when it ends. Tag is the value
// the opener passed in.
type Done struct {
	Tag     any
	Outcome Outcome
	Text    string // the input of a text prompt; empty for a confirm
}

// done returns the command that reports d.
func done(d Done) tea.Cmd {
	return func() tea.Msg { return d }
}

// ValidateLineCol accepts the input of the go-to-line prompt, "N" or "N:COL",
// as editor.ParseLineCol reads it. Any other input gets the legacy message.
func ValidateLineCol(text string) error {
	if _, _, ok := editor.ParseLineCol(text); !ok {
		return lineError{text: text}
	}
	return nil
}

// lineError is the rejection of a go-to-line input. Its text is the legacy
// message, which starts with a capital letter.
type lineError struct{ text string }

func (e lineError) Error() string { return "Not a line number: " + e.text }

// styles are the grid styles of the prompt rows.
type styles struct {
	bar, label, field, active, warn grid.Style
}

// newStyles derives the prompt styles from the chrome styles. The active choice
// takes the selected color of the overlays, so it matches the highlighted rows.
func newStyles(st style.Styles) styles {
	return styles{
		bar:    style.Cell(st.Menu),
		label:  style.Cell(st.Menu),
		field:  style.Cell(st.Editor),
		active: st.Overlay.Selected,
		warn:   style.Cell(st.Menu),
	}
}

// promptRow is the row a prompt draws on: the bottom row of the frame. It is -1
// for an empty frame.
func promptRow(h int) int {
	if h <= 0 {
		return -1
	}
	return h - 1
}

// LineOptions configure a text prompt.
type LineOptions struct {
	Label     string                  // shown before the input, for example "Go to line:"
	Input     string                  // the text the field starts with; the caret goes to its end
	SelectAll bool                    // Input is a prefill: the first printable key replaces it
	Tag       any                     // returned in Done
	Validate  func(text string) error // checked on Enter; a rejection keeps the prompt open and shows the error
	OnChange  func(text string)       // called with the new text after an edit changed it; may be nil
	// History holds earlier answers, oldest first. Up shows the previous one, Down the next, and
	// Down past the newest brings back what was typed before Up.
	History []string
}

// Line is a one-line text prompt. Enter submits the input, Esc closes the
// prompt, and every other key edits the text.
type Line struct {
	label    string
	field    textinput.Model
	tag      any
	validate func(text string) error
	onChange func(text string)
	errText  string
	styles   styles
	closed   bool
	prefill  bool // the text is an untouched prefill that the next typed text replaces

	history []string // earlier answers, oldest first
	at      int      // the entry of history shown; len(history) is the text being typed
	draft   string   // the text being typed, while an entry is shown
}

// NewLine returns a text prompt with the options of o. The field is focused.
func NewLine(st style.Styles, o LineOptions) *Line {
	f := textinput.New()
	f.Prompt = ""
	f.SetVirtualCursor(false)
	f.SetWidth(0)
	f.SetValue(o.Input)
	f.CursorEnd()
	f.Focus()
	return &Line{
		label:    o.Label,
		field:    f,
		tag:      o.Tag,
		validate: o.Validate,
		onChange: o.OnChange,
		styles:   newStyles(st),
		prefill:  o.SelectAll && o.Input != "",
		history:  o.History,
		at:       len(o.History),
	}
}

// Active reports whether the prompt is open.
func (l *Line) Active() bool { return !l.closed }

// Value returns the current input.
func (l *Line) Value() string { return l.field.Value() }

// Update handles one message. Key presses are handled as described on Line. A paste, and
// the clipboard text that Ctrl+V asked for, go to the text field. The returned command is
// the Done message when the prompt submits, the clipboard read of Ctrl+V, and nil
// otherwise.
func (l *Line) Update(msg tea.Msg) (*Line, tea.Cmd) {
	kp, ok := msg.(tea.KeyPressMsg)
	if !ok {
		if p, ok := msg.(tea.PasteMsg); ok {
			l.untouch(p.Content != "")
			p.Content = strings.ReplaceAll(strings.ReplaceAll(p.Content, "\r\n", "\n"), "\r", "\n")
			msg = p
		}
		l.edit(msg)
		return l, nil
	}
	switch tea.Key(kp).Keystroke() {
	case "esc":
		l.closed = true
		return l, nil
	case "up", "down", "ctrl+p", "ctrl+n":
		if len(l.history) > 0 {
			k := tea.Key(kp).Keystroke()
			l.walk(k == "up" || k == "ctrl+p")
			return l, nil
		}
	case "enter":
		text := l.field.Value()
		if l.validate != nil {
			if err := l.validate(text); err != nil {
				l.errText = err.Error()
				return l, nil
			}
		}
		l.closed = true
		return l, done(Done{Tag: l.tag, Outcome: Submitted, Text: text})
	}
	l.untouch(kp.Text != "" && kp.Mod&^tea.ModShift == 0)
	cmd := l.edit(kp)
	if tea.Key(kp).Keystroke() != "ctrl+v" {
		cmd = nil
	}
	return l, cmd
}

// walk shows the previous (back) or the next entry of the history. The text being typed is kept
// for the way back down.
func (l *Line) walk(back bool) {
	l.untouch(false)
	if l.at == len(l.history) {
		l.draft = l.field.Value()
	}
	if back {
		l.at = max(l.at-1, 0)
	} else {
		l.at = min(l.at+1, len(l.history))
	}
	text := l.draft
	if l.at < len(l.history) {
		text = l.history[l.at]
	}
	if text == l.field.Value() {
		return
	}
	l.field.SetValue(text)
	l.field.CursorEnd()
	l.errText = ""
	if l.onChange != nil {
		l.onChange(text)
	}
}

// untouch ends the prefill. The text it held goes when replaces is set.
func (l *Line) untouch(replaces bool) {
	if l.prefill {
		l.prefill = false
		if replaces {
			l.field.SetValue("")
		}
	}
}

// edit gives msg to the text field and clears the rejection when the text changed. It
// returns the command of the field, which is the clipboard read for Ctrl+V.
func (l *Line) edit(msg tea.Msg) tea.Cmd {
	before := l.field.Value()
	var cmd tea.Cmd
	l.field, cmd = l.field.Update(msg)
	if l.field.Value() != before {
		l.errText = ""
		if l.onChange != nil {
			l.onChange(l.field.Value())
		}
	}
	return cmd
}

// Draw paints the prompt on the bottom row of g: the label, the input, and a
// rejection message at the right when there is one.
func (l *Line) Draw(g *grid.Grid) {
	y := promptRow(g.Height())
	if y < 0 {
		return
	}
	w := g.Width()
	g.Fill(0, w, y, l.styles.bar)
	end := l.inputEnd(w)
	if end < w {
		g.Put(end+1, y, w, l.errText, l.styles.warn)
	}
	x := space.PadX + g.Put(space.PadX, y, end, l.label+" ", l.styles.label)
	shown, _ := space.Window(l.field.Value(), l.field.Position(), end-x)
	g.Put(x, y, end, shown, l.styles.field)
}

// inputEnd is the column where the input stops on a row w cells wide: before the
// rejection message when there is one.
func (l *Line) inputEnd(w int) int {
	if l.errText != "" {
		if x := w - ansi.StringWidth(l.errText) - 1; x > 0 {
			return x - 1
		}
	}
	return w
}

// Cursor returns the caret in a frame of w by h cells. ok is false when the
// caret is off the right edge.
func (l *Line) Cursor(w, h int) (x, y int, ok bool) {
	y = promptRow(h)
	if y < 0 {
		return 0, 0, false
	}
	x = space.PadX + ansi.StringWidth(l.label+" ")
	_, col := space.Window(l.field.Value(), l.field.Position(), l.inputEnd(w)-x)
	x += col
	if x >= w {
		return 0, 0, false
	}
	return x, y, true
}

// Confirm is a Yes/No question on the bottom row. Enter takes the focused
// answer, which starts on No so a stray Enter cannot confirm a destructive
// action. y and n answer at once, Left, Right and Tab move the focus, and Esc
// closes the prompt without an answer.
type Confirm struct {
	label  string
	tag    any
	yes    bool // the focus is on Yes
	closed bool
	styles styles

	yesText, noText string // the labels of the answers
}

// NewConfirm returns a Yes/No prompt that asks label. The focus starts on No.
func NewConfirm(st style.Styles, label string, tag any) *Confirm {
	return NewConfirmWith(st, label, tag, ConfirmOptions{})
}

// ConfirmOptions tune a Yes/No prompt. The zero value is the plain prompt.
type ConfirmOptions struct {
	Yes, No    string // the labels of the answers; empty keeps "Yes" and "No"
	DefaultYes bool   // the focus starts on Yes
}

// NewConfirmWith returns a Yes/No prompt that asks label, with the labels and the first
// focus of o.
func NewConfirmWith(st style.Styles, label string, tag any, o ConfirmOptions) *Confirm {
	c := &Confirm{label: label, tag: tag, styles: newStyles(st), yes: o.DefaultYes, yesText: o.Yes, noText: o.No}
	if c.yesText == "" {
		c.yesText = "Yes"
	}
	if c.noText == "" {
		c.noText = "No"
	}
	return c
}

// Active reports whether the prompt is open.
func (c *Confirm) Active() bool { return !c.closed }

// Update handles one message. The returned command is the Done message once an
// answer is given, and nil otherwise.
func (c *Confirm) Update(msg tea.Msg) (*Confirm, tea.Cmd) {
	kp, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return c, nil
	}
	switch tea.Key(kp).Keystroke() {
	case "esc":
		c.closed = true
		return c, nil
	case "enter":
		if c.yes {
			return c.answer(Yes)
		}
		return c.answer(No)
	case "left", "right", "tab", "shift+tab":
		c.yes = !c.yes
		return c, nil
	}
	switch kp.Text {
	case "y", "Y":
		return c.answer(Yes)
	case "n", "N":
		return c.answer(No)
	}
	return c, nil
}

// answer closes the prompt and reports the answer.
func (c *Confirm) answer(o Outcome) (*Confirm, tea.Cmd) {
	c.closed = true
	return c, done(Done{Tag: c.tag, Outcome: o})
}

// Draw paints the question on the bottom row of g, followed by the two answers.
// The focused answer takes the active style.
func (c *Confirm) Draw(g *grid.Grid) {
	y := promptRow(g.Height())
	if y < 0 {
		return
	}
	w := g.Width()
	g.Fill(0, w, y, c.styles.bar)
	x := space.PadX + g.Put(space.PadX, y, w, c.label+" ", c.styles.label)
	yesStyle, noStyle := c.styles.label, c.styles.label
	if c.yes {
		yesStyle = c.styles.active
	} else {
		noStyle = c.styles.active
	}
	x += g.Put(x, y, w, "[ "+c.yesText+" ]", yesStyle)
	x += g.Put(x, y, w, " ", c.styles.bar)
	g.Put(x, y, w, "[ "+c.noText+" ]", noStyle)
}

// Cursor reports no caret: a confirm has no text field.
func (c *Confirm) Cursor(w, h int) (x, y int, ok bool) {
	return 0, 0, false
}
