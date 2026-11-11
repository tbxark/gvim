package dock

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/i18n"
	"github.com/tbxark/gvim/internal/terminal"
	"github.com/tbxark/gvim/internal/tui/frame"
)

// OpenPathMsg asks the root to open path, for a Ctrl+Click on a path in the
// terminal output. Line and Col are the 1-based position a trailing :line[:col]
// on the link asked for, zero when it had none. The dock resolves and checks the
// path; the root opens it through the same path the explorer and LSP jumps use.
type OpenPathMsg struct {
	Path string
	Line int
	Col  int
}

// linkMissMsg reports a Ctrl+Click whose token names no existing file.
type linkMissMsg struct{ token string }

// linkMissNotice is the status text of a Ctrl+Click that names no file.
func linkMissNotice(token string) string {
	return i18n.T("", "gvim.terminal.no_file_at", map[string]any{"token": token})
}

// Mouse handles a tea mouse message inside the dock's rows. It focuses the dock,
// forwards the event to the shell when the shell owns the mouse, opens a link on
// Ctrl+Click, makes and extends a text selection, and scrolls the scrollback
// view. count is the click count of a press (2 selects a word, 3 a row, in
// scrollback). It reports whether the message was the dock's, so the root takes
// no other action for it.
func (p *Panel) Mouse(msg tea.Msg, l frame.Layout, count int) (tea.Cmd, bool) {
	if !l.DockOn || (p.term == nil && !p.restored) {
		return nil, false
	}
	press, isPress := msg.(tea.MouseClickMsg)
	motion, isMotion := msg.(tea.MouseMotionMsg)
	release, isRelease := msg.(tea.MouseReleaseMsg)
	wheel, isWheel := msg.(tea.MouseWheelMsg)
	if !isPress && !isMotion && !isRelease && !isWheel {
		return nil, false
	}
	var mo tea.Mouse
	switch {
	case isPress:
		mo = tea.Mouse(press)
	case isMotion:
		mo = tea.Mouse(motion)
	case isRelease:
		mo = tea.Mouse(release)
	default:
		mo = tea.Mouse(wheel)
	}
	// The dock's cells start at the frame's DockX. The shell screen, the links and
	// the selection are measured in the dock's own columns, so the pointer is made
	// local once here.
	mo.X -= l.DockX
	// The title row is the dock's but not the shell's: a press there only takes
	// the focus. Cells outside the dock's columns are not ours unless a selection
	// drag is held: then the motion still extends it and the release ends it.
	selecting := p.sel.pending || p.sel.dragging
	if (mo.X < 0 || mo.X >= l.EditorW) && !selecting {
		return nil, false
	}
	bodyTop, bodyBottom := l.DockY+1, l.DockY+l.DockH-1
	if mo.Y == l.DockY && !selecting {
		if isPress {
			p.focusDock()
		}
		return nil, true
	}
	if (mo.Y < l.DockY || mo.Y > bodyBottom) && !selecting {
		return nil, false
	}
	row := min(max(mo.Y-bodyTop, 0), max(p.bodyRows(), 1)-1)
	// A drag that leaves the body band scrolls the view: -1 above, +1 below.
	dir := 0
	if mo.Y < bodyTop {
		dir = -1
	} else if mo.Y > bodyBottom {
		dir = 1
	}
	if isPress {
		p.focusDock()
	}
	if isWheel {
		p.ClearHover()
		return p.wheelMouse(mo, row, wheel.Button), true
	}

	// A buttonless Ctrl+motion over the output is the link hover: it underlines
	// the token under the pointer, with no IO. Any other mouse event clears it.
	if isMotion && mo.Button == tea.MouseNone {
		if mo.Mod&tea.ModCtrl != 0 && !p.altScreen() && (p.scroll.on || !p.forwardsMouse(terminal.MouseMotion, mo)) {
			p.updateHover(mo, row)
			return nil, true
		}
		p.ClearHover()
	} else if !isMotion {
		p.ClearHover()
	}

	if p.scroll.on {
		if p.altScreen() {
			return nil, true
		}
		// Ctrl+Click still opens a link from the read-only view.
		if isPress && mo.Button == tea.MouseLeft && mo.Mod&tea.ModCtrl != 0 && p.term != nil {
			return p.linkCmd(mo.X, row), true
		}
		return p.scrollMouse(mo, row, dir, count, l, isPress, isMotion, isRelease), true
	}

	// Ctrl+Click opens a path from the output; it is off on the alternate screen,
	// where the output is a full-screen program's own drawing.
	if isPress && mo.Button == tea.MouseLeft && mo.Mod&tea.ModCtrl != 0 {
		if p.altScreen() {
			return nil, true
		}
		return p.linkCmd(mo.X, row), true
	}
	if isPress && mo.Button == tea.MouseLeft {
		// Shift bypasses forwarding to select, except in the legacy alt_screen
		// mode, which sends everything to a full-screen program. A shell that does
		// not own the mouse leaves the press for a possible drag-selection.
		shiftBypass := mo.Mod&tea.ModShift != 0 && p.effects.MouseForwarding != MouseForwardAltScreen
		if shiftBypass || !p.forwardsMouse(terminal.MousePress, mo) {
			p.clearSelection()
			p.beginSelectionPress(row, mo.X, count, mo.Mod&tea.ModShift != 0)
			return nil, true
		}
		p.pressed = terminal.MouseLeft
		p.term.SendMouse(terminal.MouseEvent{Kind: terminal.MousePress, X: mo.X, Y: row, Button: terminal.MouseLeft, Mods: mouseMods(mo.Mod)})
		return nil, true
	}
	if isMotion && (p.sel.pending || p.sel.dragging) {
		return p.dragSelectionMove(row, mo.X, dir, l), true
	}
	if isRelease && mo.Button == tea.MouseLeft && (p.sel.pending || p.sel.dragging) {
		p.endSelectionPress()
		return nil, true
	}
	kind := terminal.MousePress
	if isMotion {
		kind = terminal.MouseMotion
	} else if isRelease {
		kind = terminal.MouseRelease
	}
	if !p.forwardsMouse(kind, mo) {
		return nil, true
	}
	button := mouseButton(mo.Button)
	if isMotion && p.pressed == 0 {
		button = terminal.MouseNone
	}
	if isPress {
		p.pressed = button
	}
	if isRelease {
		p.pressed = 0
	}
	p.term.SendMouse(terminal.MouseEvent{Kind: kind, X: mo.X, Y: row, Button: button, Mods: mouseMods(mo.Mod)})
	return nil, true
}

// scrollMouse handles the mouse in the read-only scrollback view: a press starts
// a selection (a click without a drag leaves none), a drag extends it, a double
// or triple click selects the word or the row, and a release ends the drag. The
// view never forwards the mouse to the shell.
func (p *Panel) scrollMouse(mo tea.Mouse, row, dir, count int, l frame.Layout, isPress, isMotion, isRelease bool) tea.Cmd {
	switch {
	case isPress && mo.Button == tea.MouseLeft:
		if count >= 2 {
			p.selectUnit(row, mo.X, count)
			return nil
		}
		p.clearSelection()
		p.beginSelectionPress(row, mo.X, count, mo.Mod&tea.ModShift != 0)
		return nil
	case isMotion && mo.Button == tea.MouseLeft && (p.sel.pending || p.sel.dragging):
		return p.dragSelectionMove(row, mo.X, dir, l)
	case isRelease && mo.Button == tea.MouseLeft:
		p.endSelectionPress()
		return nil
	}
	return nil
}

// wheelMouse handles a wheel notch: the scrollback view scrolls, a shell that
// asked for the mouse gets the notch, a full-screen program that did not ask for
// it gets arrow keys (the alternate-scroll rule), and the live main screen of a
// plain shell enters the scrollback view on an upward notch.
func (p *Panel) wheelMouse(mo tea.Mouse, row int, button tea.MouseButton) tea.Cmd {
	up := mo.Button == tea.MouseWheelUp
	if p.scroll.on {
		if up {
			p.scrollBy(-wheelRows)
		} else {
			p.scrollBy(wheelRows)
		}
		return p.afterScroll()
	}
	if p.term == nil || mo.Mod&tea.ModShift != 0 && p.effects.MouseForwarding != MouseForwardAltScreen {
		return p.scrollFromWheel(up)
	}
	reporting, _, _ := p.term.MouseReporting()
	alt := p.altScreen()
	legacy := p.effects.MouseForwarding == MouseForwardAltScreen
	switch {
	case reporting || (legacy && alt):
		p.term.SendMouse(terminal.MouseEvent{Kind: terminal.MouseWheel, X: mo.X, Y: row, Button: mouseButton(button), Mods: mouseMods(mo.Mod)})
		return nil
	case alt:
		// The alternate-scroll rule: a pager that does not use the mouse scrolls
		// with the arrow keys.
		seq := "\x1b[A"
		if !up {
			seq = "\x1b[B"
		}
		p.SendBytes([]byte(strings.Repeat(seq, wheelRows)))
		return nil
	default:
		return p.scrollFromWheel(up)
	}
}

// wheelRows is how many rows one wheel notch scrolls, as the editor's wheel does.
const wheelRows = 3

// scrollFromWheel enters the scrollback view on an upward notch, or scrolls it.
func (p *Panel) scrollFromWheel(up bool) tea.Cmd {
	if !up {
		return nil
	}
	if !p.scroll.on {
		cmd := p.enterScrollback()
		if p.scroll.on {
			// The notch scrolls the window up from the bottom and selects the last
			// visible row, so the view starts where the wheel asked.
			p.scroll.top = max(p.scroll.top-wheelRows, 0)
			p.scroll.selected = min(p.scroll.top+max(p.bodyRows(), 1)-1, max(p.combined()-1, 0))
			return tea.Batch(cmd, p.afterScroll())
		}
		return cmd
	}
	p.scrollBy(-wheelRows)
	return p.afterScroll()
}

// altScreen reports whether the shell is on the alternate screen.
func (p *Panel) altScreen() bool { return p.term != nil && p.term.AltScreen() }

// forwardsMouse applies terminal.mouse_forwarding: in the requested mode the shell
// gets press/release/wheel only when it asked for mouse reporting, and motion only
// when it asked for all motion; Shift bypasses forwarding so text stays reachable.
// The legacy alt_screen mode sends every event to an alternate-screen program and
// has no Shift bypass.
func (p *Panel) forwardsMouse(kind terminal.MouseKind, mo tea.Mouse) bool {
	if p.term == nil {
		return false
	}
	legacy := p.effects.MouseForwarding == MouseForwardAltScreen
	reporting, drag, all := p.term.MouseReporting()
	alt := p.altScreen()
	if legacy {
		return alt
	}
	if mo.Mod&tea.ModShift != 0 {
		return false
	}
	switch kind {
	case terminal.MouseMotion:
		// 1003 reports every motion; 1002 only motion with a button held.
		return all || (drag && p.pressed != 0)
	default:
		return reporting
	}
}

// mouseButton maps a bubbletea mouse button to the terminal's numbering.
func mouseButton(b tea.MouseButton) int {
	switch b {
	case tea.MouseLeft:
		return terminal.MouseLeft
	case tea.MouseMiddle:
		return terminal.MouseMiddle
	case tea.MouseRight:
		return terminal.MouseRight
	case tea.MouseWheelUp:
		return terminal.MouseWheelUp
	case tea.MouseWheelDown:
		return terminal.MouseWheelDown
	case tea.MouseWheelLeft:
		return terminal.MouseWheelLeft
	case tea.MouseWheelRight:
		return terminal.MouseWheelRight
	}
	return terminal.MouseNone
}

// mouseMods maps the modifiers of a mouse event.
func mouseMods(m tea.KeyMod) terminal.MouseMods {
	return terminal.MouseMods{
		Shift: m&tea.ModShift != 0,
		Alt:   m&tea.ModAlt != 0,
		Ctrl:  m&tea.ModCtrl != 0,
	}
}

// linkCmd resolves the path token at cell x of body row row and asks the root to
// open it, in a command: the existence check and the working-directory read are
// off the update loop.
func (p *Panel) linkCmd(x, row int) tea.Cmd {
	token, ok := p.linkAt(x, row)
	if !ok {
		return nil
	}
	t := p.term
	rootDir := ""
	if p.host.RootDir != nil {
		rootDir = p.host.RootDir()
	}
	stat := p.host.Stat
	if stat == nil {
		stat = func(path string) bool {
			_, err := os.Stat(path)
			return err == nil
		}
	}
	return func() tea.Msg {
		cwd := ""
		if t != nil {
			cwd = t.WorkingDirectory()
		}
		if path, line, col, ok := resolveLinkTarget(token, cwd, rootDir, stat); ok {
			return OpenPathMsg{Path: path, Line: line, Col: col}
		}
		return linkMissMsg{token: token}
	}
}

// linkAt returns the path token under cell x of body row row. The row is read
// from the scrollback snapshot or the live screen, whichever the dock shows.
func (p *Panel) linkAt(x, row int) (string, bool) {
	text, ok := p.visibleRowText(row)
	if !ok {
		return "", false
	}
	return tokenAt([]rune(text), x)
}

// visibleRowText is the text of body row row in the mode the dock shows.
func (p *Panel) visibleRowText(row int) (string, bool) {
	if p.scroll.on {
		return p.combinedText(p.scroll.top+row, true)
	}
	if p.term == nil {
		return "", false
	}
	sc := p.term.Screen()
	if row < 0 || row >= sc.Rows {
		return "", false
	}
	return screenRowText(sc, row, true), true
}

// linkRune reports whether r can be part of a path token. White space and the
// brackets and quotes that usually surround a path are not.
func linkRune(r rune) bool {
	if unicode.IsSpace(r) || unicode.IsControl(r) {
		return false
	}
	return !strings.ContainsRune("\"'<>()[]{}|\u0060", r)
}

// tokenAt returns the path token around rune column col of text: the run of
// linkRune characters that contains col, with the punctuation that trails a
// sentence trimmed. The token must look like a path: it holds a separator, a dot
// or a tilde. ok is false when there is no such token under col.
func tokenAt(text []rune, col int) (string, bool) {
	token, _, _, ok := linkTokenAt(text, col)
	return token, ok
}

// linkTokenAt is tokenAt that also returns the token's rune span [start, end) in
// text, after the trailing punctuation is trimmed, for the hover underline.
func linkTokenAt(text []rune, col int) (token string, start, end int, ok bool) {
	if col < 0 || col >= len(text) || !linkRune(text[col]) {
		return "", 0, 0, false
	}
	start, end = col, col+1
	for start > 0 && linkRune(text[start-1]) {
		start--
	}
	for end < len(text) && linkRune(text[end]) {
		end++
	}
	token = strings.TrimRight(string(text[start:end]), ".,;:!?")
	if token == "" || !plausibleLink(token) {
		return "", 0, 0, false
	}
	end = start + len([]rune(token))
	return token, start, end, true
}

// plausibleLink reports whether token looks like a path rather than a word: it
// holds a slash, a dot or a leading tilde.
func plausibleLink(token string) bool {
	if strings.HasPrefix(token, "~") {
		return true
	}
	return strings.ContainsAny(token, "/.")
}

// resolveLink finds the existing file token names, in Fresh's order: an absolute
// path (with ~ expanded), then relative to the terminal's OSC 7 working
// directory, then relative to the editor's working directory.
func resolveLink(token, termCwd, rootDir string, stat func(string) bool) (string, bool) {
	if token == "" {
		return "", false
	}
	var candidates []string
	if strings.HasPrefix(token, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			candidates = append(candidates, filepath.Join(home, strings.TrimPrefix(token, "~/")))
		}
	} else if filepath.IsAbs(token) {
		candidates = append(candidates, token)
	}
	if len(candidates) == 0 {
		if termCwd != "" {
			candidates = append(candidates, filepath.Join(termCwd, token))
		}
		if rootDir != "" {
			candidates = append(candidates, filepath.Join(rootDir, token))
		}
	}
	for _, path := range candidates {
		if stat(path) {
			return filepath.Clean(path), true
		}
	}
	return "", false
}

// resolveLinkTarget resolves a token that may end in :line or :line:col. A path
// with a suffix is tried first, with the position returned; the whole token is
// tried as a plain path next, so a file whose name really holds a colon still
// opens.
func resolveLinkTarget(token, termCwd, rootDir string, stat func(string) bool) (path string, line, col int, ok bool) {
	if stripped, ln, cl, has := splitLinkTarget(token); has {
		if path, ok := resolveLink(stripped, termCwd, rootDir, stat); ok {
			return path, ln, cl, true
		}
	}
	path, ok = resolveLink(token, termCwd, rootDir, stat)
	return path, 0, 0, ok
}

// splitLinkTarget splits a trailing :line or :line:col from token. The numbers
// are 1-based and must be positive; col is 0 when the token named no column. ok
// is false when there is no valid suffix, so the caller resolves the token whole.
func splitLinkTarget(token string) (path string, line, col int, ok bool) {
	idx := strings.LastIndex(token, ":")
	if idx <= 0 || idx == len(token)-1 {
		return "", 0, 0, false
	}
	n, err := strconv.Atoi(token[idx+1:])
	if err != nil || n < 1 {
		return "", 0, 0, false
	}
	head := token[:idx]
	if idx2 := strings.LastIndex(head, ":"); idx2 > 0 && idx2 < len(head)-1 {
		if m, err := strconv.Atoi(head[idx2+1:]); err == nil && m >= 1 {
			return head[:idx2], m, n, true
		}
	}
	return head, n, 0, true
}
