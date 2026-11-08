package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/style"
)

// utilities is the state of the overlays in this group: the settings that
// open_settings lists, the keymap switch, and the input events the diagnostic
// views show. The zero value has no settings and no switch.
type utilities struct {
	settings     Settings
	switchKeymap func(name string) (Keymap, error)
	events       eventRing
}

// utilityAction runs an action of the overlays in this group and reports whether
// the action is one of them. The root calls it before the editor, so an action
// named here never reaches the editor.
func (m *Model) utilityAction(action string) bool {
	if name, ok := strings.CutPrefix(action, "switch_keybinding_map:"); ok {
		m.switchKeymap(name)
		return true
	}
	switch action {
	case "show_help":
		m.openHelp("Help")
	case "keyboard_shortcuts":
		m.openHelp("Keyboard Shortcuts")
	case "open_keybinding_editor":
		m.openKeybindingEditor()
	case "switch_keybinding_map", "select_keybinding_map":
		m.openKeymapSwitch()
	case "open_settings":
		m.openSettings()
	case "settings_activate":
		m.openConfigFile()
	case "tour_next", "tour_prev":
		m.tourAction(action)
	case "show_remote_indicator_menu":
		m.showRemoteMenu()
	case "popup_focus":
		if !m.popupFocus() {
			m.notice = "No popup to focus"
		}
	case "calibrate_input":
		m.openCalibrate()
	case "event_debug":
		m.openEventDebug()
	default:
		return false
	}
	return true
}

// listChrome returns the overlay styles that the lists and the text boxes share.
func (m *Model) listChrome() style.Overlay {
	return m.styles.Overlay
}

// keymapChecked reports whether the menu row menuKey, such as "keymap_emacs", is
// the active keymap. Until a keymap name is known, the Default row is checked, as
// it was before the switch existed.
func (u utilities) keymapChecked(menuKey string) bool {
	name := strings.ReplaceAll(strings.TrimPrefix(menuKey, "keymap_"), "_", "-")
	active := u.settings.Keymap
	if active == "" {
		active = "default"
	}
	return active == name
}

// boxView is a centred box of text: a title, the body lines and a footer. The tour,
// the calibration view and the event view use it. content is read at every draw, so
// a view shows the live state it reads. onKey, when set, sees each key press the
// box takes, and returns true to close the box.
type boxView struct {
	content func() (title string, lines []string, footer string)
	onKey   func(k tea.Key) (done bool)
	tour    *tourState // set on the tour only: tour_next and tour_prev step it
	active  bool
	chrome  style.Overlay
	box     rect // the box at the last draw
}

// boxMaxWidth is the widest a text box is drawn.
const boxMaxWidth = 76

// draw paints the box with the border and title of the dialogs. The title is on
// the top border and the footer on the bottom one, so the body lines fill the rows
// between them. A long body keeps the first rows it can show.
func (v *boxView) draw(g *grid.Grid) {
	title, lines, footer := v.content()
	box := centred(g.Width(), g.Height(), boxMaxWidth, len(lines)+2)
	v.box = box
	if box.w == 0 || box.h < 3 {
		return
	}
	end := box.x + box.w - 2 // text stops before the padding cell at the right border
	v.chrome.PaintBox(g, box.x, box.y, box.w, box.h, title)
	for k := range min(len(lines), box.h-2) {
		g.Put(box.x+2, box.y+1+k, end, truncateCells(lines[k], end-(box.x+2)), v.chrome.Box)
	}
	g.Put(box.x+2, box.y+box.h-1, end, truncateCells(" "+footer+" ", end-(box.x+2)), v.chrome.Muted)
}

// boxOverlay adapts *boxView to Overlay. The box closes itself when onKey reports
// done, and the adapter reports that to the stack.
type boxOverlay struct{ v *boxView }

// Update passes a key press to onKey. It returns nil once the box is closed, so the
// stack pops it.
func (o boxOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok && o.v.onKey != nil && o.v.onKey(tea.Key(k)) {
		o.v.active = false
	}
	if !o.v.active {
		return nil, nil
	}
	return o, nil
}

// Draw paints the box over the frame.
func (o boxOverlay) Draw(g *grid.Grid) { o.v.draw(g) }

// Active reports whether the box is still open.
func (o boxOverlay) Active() bool { return o.v.active }

// keyLabel splits a key press for the calibration view: the key name without its
// modifiers, the modifiers and the text, as the terminal reported them.
func keyLabel(k tea.Key) (name, mods, text string) {
	base := k
	base.Mod = 0
	name = base.Keystroke()
	var parts []string
	for _, m := range []struct {
		bit  tea.KeyMod
		name string
	}{
		{tea.ModCtrl, "ctrl"}, {tea.ModAlt, "alt"}, {tea.ModShift, "shift"},
		{tea.ModMeta, "meta"}, {tea.ModHyper, "hyper"}, {tea.ModSuper, "super"},
	} {
		if k.Mod&m.bit != 0 {
			parts = append(parts, m.name)
		}
	}
	mods = "none"
	if len(parts) > 0 {
		mods = strings.Join(parts, "+")
	}
	text = "(none)"
	if k.Text != "" {
		text = fmt.Sprintf("%q", k.Text)
	}
	return name, mods, text
}
