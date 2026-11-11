package tui

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/overlay"
	"github.com/tbxark/gvim/internal/tui/space"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/trust"
)

// The workspace-trust prompt is a security modal, Fresh's workspace trust
// dialog: a centred card that explains that the folder can run code, lists the
// markers that made it ask, and offers the three decisions. The texts are
// Fresh's locale strings; the card frame is gvim's (docs/ui.md).
//
// The prompt is exclusive: while it is up, it takes every key and mouse message
// and nothing behind it is interactive. A click on a radio selects the option
// and does not consent — only [ OK ] / Enter commits, the two-step Fresh's
// dialog keeps for the same reason.
const (
	trustDialogTitle = "⚠ SECURITY WARNING"

	trustCanExecute  = "This project folder can execute arbitrary code:"
	trustPathLabel   = "Path:"
	trustDetected    = "Detected: "
	trustHowProceed  = "How would you like to proceed?"
	trustOKLabel     = "OK"
	trustCancelLabel = "Cancel (Esc)"
	trustQuitLabel   = "Quit"

	// trustMinContent and trustMaxContent bound the card's content width in
	// cells. The maximum makes the card 68 cells wide, Fresh's dialog width.
	trustMinContent = 40
	trustMaxContent = 64
	// trustDescriptionIndent is the leading cells of an option description,
	// Fresh's hanging indent.
	trustDescriptionIndent = 6
)

// trustOption is one decision of the prompt: the radio label Fresh writes
// (mnemonic letter included) and the one-line description under it.
type trustOption struct {
	label       string
	description string
}

// trustOptions are the three decisions, in Fresh's order. The labels and
// descriptions are Fresh's trust.dialog.opt_* strings.
var trustOptions = [...]trustOption{
	{
		label:       "Trust folder & Allow Tooling (T)",
		description: "Runs everything: language servers, build scripts, tasks, env activation.",
	},
	{
		label:       "Keep Restricted (Default) (K)",
		description: "Runs system tools found on your PATH (git, ripgrep, the system python). Blocks: executables & scripts located inside this project (e.g. ./gradlew, .venv/bin/python, node_modules/.bin/*), env activation (.env/.envrc/mise), and language servers.",
	},
	{
		label:       "Block All Execution (B)",
		description: "Nothing runs — no system tools, language servers, scripts, or tasks.",
	},
}

// trustLineKind names what one row of the dialog body is.
type trustLineKind int

const (
	trustLineMessage trustLineKind = iota
	trustLinePath
	trustLineDetected
	trustLineBlank
	trustLineHow
	trustLineOption
	trustLineDescription
	trustLineButtons
)

// trustLine is one row of the dialog body. option is the radio index of an
// option row, or of the description rows under it.
type trustLine struct {
	kind   trustLineKind
	text   string
	option int
}

// trustDialog is the workspace-trust prompt: an overlay that asks for the
// decision, records it through the model's trust port, and closes. cancellable
// picks the secondary button: Cancel for the prompt the user opened, Quit for
// the mandatory open-time prompt.
type trustDialog struct {
	m           *Model
	state       trust.State
	cancellable bool
	sel         int // the highlighted option: 0 trust, 1 restrict, 2 block
	closed      bool

	// Geometry of the last draw, for the hit test.
	box       space.Rect
	radios    [3]space.Rect
	ok        space.Rect
	secondary space.Rect
	hover     int // 0 for OK, 1 for the secondary button, -1 for none
}

var _ overlay.Overlay = (*trustDialog)(nil)

// newTrustDialog returns the prompt over m's trust state. The selection starts
// on the workspace's current level, so a prompt opened from the command shows
// the active decision, and the open-time prompt starts on the safe Restricted
// row.
func newTrustDialog(m *Model, cancellable bool) *trustDialog {
	d := &trustDialog{m: m, state: m.trust.state, cancellable: cancellable, hover: -1}
	d.sel = trustSelectionFromLevel(d.state.Level)
	return d
}

// Active reports whether the prompt is still open.
func (d *trustDialog) Active() bool { return !d.closed }

// AlertOverlay marks the trust prompt as an alert: it keeps the input until the
// user answers it. See overlay.Alert.
func (*trustDialog) AlertOverlay() {}

// TakesEscape reports that the prompt handles Esc itself: the mandatory prompt
// swallows it, and the prompt the user opened closes on it.
func (d *trustDialog) TakesEscape() bool { return true }

// secondaryLabel returns the secondary button label: Cancel for a prompt the
// user opened, and Quit with the active keymap's quit key for the mandatory
// open-time prompt.
func (d *trustDialog) secondaryLabel() string {
	if d.cancellable {
		return trustCancelLabel
	}
	if hint := d.m.trustQuitHint(); hint != "" {
		return "Quit (" + hint + ")"
	}
	return trustQuitLabel
}

// Update handles one message. The prompt answers every key: arrows and the
// T/K/B mnemonics move the selection, Enter and O confirm it, Esc cancels the
// prompt the user opened and is inert on the mandatory one, and the user's
// quit key quits the editor from the mandatory one.
func (d *trustDialog) Update(msg tea.Msg) (overlay.Overlay, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		cmd = d.key(msg)
	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			cmd = d.click(tea.Mouse(msg))
		}
	case tea.MouseMotionMsg:
		mo := tea.Mouse(msg)
		d.hover = d.buttonAt(mo.X, mo.Y)
	case tea.MouseWheelMsg:
		// The card does not scroll: it is sized to fit the frame.
	}
	if d.closed {
		return nil, cmd
	}
	return d, cmd
}

// key handles one key press. It returns the command the press starts.
func (d *trustDialog) key(msg tea.KeyPressMsg) tea.Cmd {
	// The mandatory prompt has nothing to fall back to, so the user's global
	// quit key quits the editor, as Fresh's dialog does. A cancellable prompt
	// does not: Esc cancels it and the quit keys stay inert.
	if !d.cancellable {
		kp, _ := translateKey(tea.Key(msg))
		if d.m.isQuitKey(kp) {
			d.closed = true
			return d.m.quitLoop()
		}
	}
	switch tea.Key(msg).Keystroke() {
	case "esc":
		if d.cancellable {
			d.closed = true
		}
		return nil
	case "up":
		d.move(-1)
		return nil
	case "down":
		d.move(1)
		return nil
	case "enter":
		return d.confirm()
	}
	// The mnemonics: a plain letter selects a radio, O confirms the selection.
	if msg.Mod&^tea.ModShift != 0 {
		return nil
	}
	rs := []rune(msg.Text)
	if len(rs) != 1 {
		return nil
	}
	switch unicode.ToLower(rs[0]) {
	case 't':
		d.sel = 0
	case 'k':
		d.sel = 1
	case 'b':
		d.sel = 2
	case 'o':
		return d.confirm()
	}
	return nil
}

// move shifts the selected option by delta, wrapping around the three.
func (d *trustDialog) move(delta int) {
	d.sel = (d.sel + delta + len(trustOptions)) % len(trustOptions)
}

// confirm records the selected level and closes the prompt.
func (d *trustDialog) confirm() tea.Cmd {
	d.closed = true
	level := [...]trust.Level{trust.Trusted, trust.Restricted, trust.Blocked}[d.sel]
	return decideTrust(d.m.trust.port, level)
}

// click handles a left click. A click on a radio selects it and does not
// consent; a click on a button answers it.
func (d *trustDialog) click(mo tea.Mouse) tea.Cmd {
	for i, r := range d.radios {
		if r.Contains(mo.X, mo.Y) {
			d.sel = i
			return nil
		}
	}
	if d.ok.Contains(mo.X, mo.Y) {
		return d.confirm()
	}
	if d.secondary.Contains(mo.X, mo.Y) {
		d.closed = true
		if d.cancellable {
			return nil
		}
		return d.m.quitLoop()
	}
	return nil
}

// buttonAt returns the button under (x, y): 0 for OK, 1 for the secondary
// button, -1 for none.
func (d *trustDialog) buttonAt(x, y int) int {
	switch {
	case d.ok.Contains(x, y):
		return 0
	case d.secondary.Contains(x, y):
		return 1
	}
	return -1
}

// Draw paints the card and records its hit rectangles. The card is centered and
// keeps a row from the top and bottom edge when the frame has room; a frame too
// short for the whole text shrinks the option descriptions from the bottom of
// the card's size budget, so the buttons always stay on it.
func (d *trustDialog) Draw(g *grid.Grid) {
	ov := d.m.styles.Overlay
	cw := trustContentWidth(g.Width())
	w := cw + 4
	maxHeight := max(g.Height()-2, 3)
	lines := d.lines(cw, d.naturalDescRows(cw))
	for descRows := d.naturalDescRows(cw) - 1; descRows >= 0 && len(lines)+2 > maxHeight; descRows-- {
		lines = d.lines(cw, descRows)
	}
	h := min(len(lines)+2, g.Height())
	if w < 10 || h < 3 {
		return
	}
	x := max((g.Width()-w)/2, 0)
	y := max((g.Height()-h)/2, 0)
	d.box = space.Rect{X: x, Y: y, W: w, H: h}
	d.radios = [3]space.Rect{}
	d.ok, d.secondary = space.Rect{}, space.Rect{}
	ov.PaintBox(g, x, y, w, h, trustDialogTitle)
	inner, end := x+2, x+w-2
	for i, line := range lines {
		row := y + 1 + i
		if row > y+h-2 {
			break
		}
		switch line.kind {
		case trustLineMessage:
			g.Put(inner, row, end, line.text, ov.Box)
		case trustLinePath:
			next := inner + g.Put(inner, row, end, trustPathLabel+" ", ov.Muted)
			if next < end {
				g.Put(next, row, end, trustPathText(d.state.Root, end-next), ov.Box)
			}
		case trustLineDetected:
			g.Put(inner, row, end, line.text, ov.Muted)
		case trustLineHow:
			g.Put(inner, row, end, line.text, ov.Box)
		case trustLineOption:
			d.radios[line.option] = space.Rect{X: inner, Y: row, W: cw, H: 1}
			st := ov.Box
			if line.option == d.sel {
				st = ov.Selected
				g.Fill(inner, end, row, st)
			}
			g.Put(inner, row, end, line.text, st)
		case trustLineDescription:
			g.Put(inner, row, end, strings.Repeat(" ", trustDescriptionIndent)+line.text, ov.Muted)
		case trustLineButtons:
			d.drawTrustButtons(g, inner, row, cw, ov)
		}
	}
}

// drawTrustButtons paints [ OK ] centred in the left half of the row and the
// secondary button centred in the right half, and records their places.
func (d *trustDialog) drawTrustButtons(g *grid.Grid, x0, y, cw int, ov style.Overlay) {
	okLabel := "[ " + trustOKLabel + " ]"
	secLabel := "[ " + d.secondaryLabel() + " ]"
	half := cw / 2
	okX := x0 + max((half-ansi.StringWidth(okLabel))/2, 0)
	secX := x0 + half + max((cw-half-ansi.StringWidth(secLabel))/2, 0)
	d.ok = space.Rect{X: okX, Y: y, W: ansi.StringWidth(okLabel), H: 1}
	d.secondary = space.Rect{X: secX, Y: y, W: ansi.StringWidth(secLabel), H: 1}
	okStyle, secStyle := ov.Focus, ov.Idle
	if d.hover == 0 {
		okStyle = ov.Hover
	}
	if d.hover == 1 {
		secStyle = ov.Hover
	}
	g.Put(okX, y, x0+cw, okLabel, okStyle)
	g.Put(secX, y, x0+cw, secLabel, secStyle)
}

// lines returns the body rows of the dialog at content width cw, with each
// option description wrapped to descRows rows (0 shows the labels only).
func (d *trustDialog) lines(cw, descRows int) []trustLine {
	lines := []trustLine{
		{kind: trustLineMessage, text: trustCanExecute},
		{kind: trustLinePath},
	}
	if detected := trustDetected + strings.Join(d.state.Markers, ", "); len(d.state.Markers) > 0 {
		for _, row := range space.Wrap(detected, cw, 2) {
			lines = append(lines, trustLine{kind: trustLineDetected, text: row})
		}
	}
	lines = append(lines,
		trustLine{kind: trustLineBlank},
		trustLine{kind: trustLineHow, text: trustHowProceed},
		trustLine{kind: trustLineBlank},
	)
	for i, opt := range trustOptions {
		marker := "( )"
		if i == d.sel {
			marker = "(*)"
		}
		lines = append(lines, trustLine{kind: trustLineOption, text: marker + " " + opt.label, option: i})
		if descRows > 0 {
			for _, row := range space.Wrap(opt.description, cw-trustDescriptionIndent, descRows) {
				lines = append(lines, trustLine{kind: trustLineDescription, text: row, option: i})
			}
		}
		lines = append(lines, trustLine{kind: trustLineBlank})
	}
	return append(lines, trustLine{kind: trustLineButtons})
}

// naturalDescRows returns the most description rows any option needs at width
// cw: the height the dialog grows to before it is shrunk to fit the frame.
func (d *trustDialog) naturalDescRows(cw int) int {
	rows := 0
	for _, opt := range trustOptions {
		rows = max(rows, len(space.Wrap(opt.description, cw-trustDescriptionIndent, 1<<10)))
	}
	return rows
}

// trustContentWidth returns the content width of the card in a frame frameWidth
// cells wide: Fresh's 68-cell card, narrower when the frame or its margin leaves
// less room.
func trustContentWidth(frameWidth int) int {
	return max(min(trustMaxContent, frameWidth-4-2*space.Margin(frameWidth)), 1)
}

// trustPathText shortens root to at most w cells from the head: the end of the
// path names the folder, so that is the end that stays. Fresh's dialog elides
// the same way.
func trustPathText(root string, w int) string {
	if ansi.StringWidth(root) <= w {
		return root
	}
	if w <= 1 {
		return "…"
	}
	return "…" + space.Tail(root, w-1)
}

// isQuitKey reports whether kp is a key the active keymap binds to quit.
func (m *Model) isQuitKey(kp KeyPress) bool {
	cmd, ok := m.keys.Resolve(kp, KeyContextNormal)
	return ok && isQuitAction(cmd.Action)
}

// isQuitAction reports whether action quits the editor.
func isQuitAction(action string) bool { return action == "quit" || action == "force_quit" }

// trustQuitHint returns the name of the quit key the mandatory prompt shows on
// its secondary button: "Ctrl+Q" or "Cmd+Q" when the keymap binds one, and
// "C-x C-c" for the emacs keymap's two-step quit. It returns "" when the keymap
// has no quit key gvim can name this cheaply; the button then says "Quit".
func (m *Model) trustQuitHint() string {
	if m.keys == nil {
		return ""
	}
	if m.isQuitKey(KeyPress{Name: "q", Mods: []string{"ctrl"}}) {
		return "Ctrl+Q"
	}
	if m.isQuitKey(KeyPress{Name: "q", Mods: []string{"super"}}) {
		return "Cmd+Q"
	}
	if seq, ok := m.keys.(SequenceKeymap); ok {
		steps := []KeyPress{{Name: "x", Mods: []string{"ctrl"}}, {Name: "c", Mods: []string{"ctrl"}}}
		if cmd, whole, _ := seq.ResolveSequence(steps, KeyContextNormal); whole && isQuitAction(cmd.Action) {
			return "C-x C-c"
		}
	}
	return ""
}

// trustSelectionFromLevel returns the radio index that shows level.
func trustSelectionFromLevel(level trust.Level) int {
	switch trust.Effective(level) {
	case trust.Trusted:
		return 0
	case trust.Blocked:
		return 2
	default:
		return 1
	}
}
