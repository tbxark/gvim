package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/browser"
	"github.com/tbxark/gvim/internal/tui/ctxmenu"
	"github.com/tbxark/gvim/internal/tui/find"
	"github.com/tbxark/gvim/internal/tui/list"
	"github.com/tbxark/gvim/internal/tui/palette"
	"github.com/tbxark/gvim/internal/tui/prompt"
)

// The keymap contexts the root resolves for the surfaces that take their keys in code: the
// top overlay (or the menu bar, or the dock) looks a key up in one of these before its own
// handler. They are Fresh's context names, so a user's Fresh keybindings carry over.
const (
	// KeyContextPrompt is a prompt or a confirm: one line of text, or a Yes/No answer.
	KeyContextPrompt KeyContext = "prompt"
	// KeyContextSearchPrompt is the find bar.
	KeyContextSearchPrompt KeyContext = "searchPrompt"
	// KeyContextPopup is a list-like overlay: the palette, a picker, the file browser.
	KeyContextPopup KeyContext = "popup"
	// KeyContextMenu is the menu bar's dropdown and the context menu.
	KeyContextMenu KeyContext = "menu"
	// KeyContextSettings is the Settings page.
	KeyContextSettings KeyContext = "settings"
	// KeyContextDock is the terminal dock's key handling.
	KeyContextDock KeyContext = "dock"
)

// overlayActionNames are the actions the overlay contexts run themselves. They are not the
// editor's actions: the root's overlay handler answers them, so they are known to the
// keybinding loader and to a user's keybindings in these contexts.
var overlayActionNames = []string{
	// prompt: the text field's supported operations.
	"prompt_confirm", "prompt_cancel",
	"prompt_backspace", "prompt_delete", "prompt_delete_to_line_end",
	"prompt_move_left", "prompt_move_right", "prompt_move_start", "prompt_move_end",
	"prompt_move_word_left", "prompt_move_word_right",
	"prompt_delete_word_backward", "prompt_delete_word_forward",
	"prompt_select_prev", "prompt_select_next", "prompt_page_up", "prompt_page_down",
	"prompt_accept_suggestion", "prompt_paste",
	// searchPrompt: the find bar's match steps and option toggles.
	"toggle_search_case_sensitive", "toggle_search_whole_word",
	"toggle_search_regex", "toggle_search_confirm_each",
	// popup: list-like overlays.
	"popup_select_prev", "popup_select_next", "popup_page_up", "popup_page_down",
	"popup_confirm", "popup_cancel",
	// menu: the menu bar dropdown and the context menu.
	"menu_left", "menu_right", "menu_up", "menu_down", "menu_execute", "menu_close",
	// settings: the Settings page.
	"close_settings", "settings_activate", "settings_inherit", "settings_help", "settings_search",
}

// overlayContextOf returns the keymap context of an overlay that takes its keys in code and
// consults the keymap before its own handler.
func overlayContextOf(o Overlay) (KeyContext, bool) {
	switch o.(type) {
	case *prompt.Line, *prompt.Confirm:
		return KeyContextPrompt, true
	case *find.Bar:
		return KeyContextSearchPrompt, true
	case *palette.Palette, *list.View, *browser.Browser:
		return KeyContextPopup, true
	case *ctxmenu.Menu:
		return KeyContextMenu, true
	case *settingsPage:
		return KeyContextSettings, true
	}
	return "", false
}

// menuKeymap resolves a key press in the menu context while a menu bar dropdown is open,
// so a user binding of Fresh's menu actions works there too.
func (m *Model) menuKeymap(k tea.Key) (tea.Cmd, bool) {
	if m.keys == nil {
		return nil, false
	}
	kp, _ := translateKey(k)
	cmd, found := m.keys.Resolve(kp, KeyContextMenu)
	if !found {
		if ub, ok := m.keys.(UnbindKeymap); ok && ub.Unbound(kp, KeyContextMenu) {
			return nil, true
		}
		return nil, false
	}
	if cmd.Action == "noop" {
		return nil, true
	}
	var code rune
	switch cmd.Action {
	case "menu_up":
		code = tea.KeyUp
	case "menu_down":
		code = tea.KeyDown
	case "menu_left":
		code = tea.KeyLeft
	case "menu_right":
		code = tea.KeyRight
	case "menu_execute":
		code = tea.KeyEnter
	case "menu_close":
		code = tea.KeyEscape
	default:
		if m.overlayRealAction(KeyContextMenu, cmd.Action) {
			return m.dispatch(cmd), true
		}
		return nil, false
	}
	msg := syntheticKey(code, 0)
	m.syncMenu()
	if !m.menu.Consumes(msg) {
		return nil, false
	}
	var out tea.Cmd
	m.menu, out = m.menu.Update(msg)
	out = tea.Batch(out, m.pending)
	m.pending = nil
	if m.quitting {
		m.quitting = false
		return tea.Batch(out, m.quit()), true
	}
	return out, true
}

// overlayKey resolves a key press in the top overlay's keymap context before the overlay's
// own handler. It reports whether it consumed the key. A binding runs the surface's action;
// an unbound key, or a bound action neither the surface nor the model knows, falls through
// to the built-in handling.
func (m *Model) overlayKey(k tea.Key) (tea.Cmd, bool) {
	if m.keys == nil || len(m.overlays) == 0 {
		return nil, false
	}
	if p, ok := m.overlays[len(m.overlays)-1].(*settingsPage); ok && p.help {
		// Any key closes the Settings help panel; Esc then closes the page itself.
		p.help = false
		return nil, true
	}
	ctx, ok := overlayContextOf(m.overlays[len(m.overlays)-1])
	if !ok {
		return nil, false
	}
	kp, _ := translateKey(k)
	cmd, found := m.keys.Resolve(kp, ctx)
	if !found {
		if ub, ok := m.keys.(UnbindKeymap); ok && ub.Unbound(kp, ctx) {
			// The user explicitly unbound the key in this context: swallow it instead of
			// falling back to the surface's built-in handling.
			return nil, true
		}
		return nil, false
	}
	return m.runOverlayAction(ctx, cmd)
}

// runOverlayAction runs one resolved overlay binding. kind says which surface it is: an
// action of the surface's context runs the matching built-in operation, a real model
// action runs through the model's dispatch (the same dispatch a preset or plugin binding
// uses), and anything else falls through.
func (m *Model) runOverlayAction(ctx KeyContext, cmd Command) (tea.Cmd, bool) {
	switch cmd.Action {
	case "noop":
		return nil, true
	case "prompt_confirm", "popup_confirm":
		return m.deliverOverlay(syntheticKey(tea.KeyEnter, 0)), true
	case "prompt_cancel", "popup_cancel", "menu_close":
		return m.cancelTopOverlay(), true
	case "close_settings":
		// The Settings page runs its own Cancel, including the pending-edits question,
		// when it takes Esc itself.
		return m.deliverOverlay(syntheticKey(tea.KeyEscape, 0)), true
	case "prompt_backspace":
		return m.deliverOverlay(syntheticKey(tea.KeyBackspace, 0)), true
	case "prompt_delete":
		return m.deliverOverlay(syntheticKey(tea.KeyDelete, 0)), true
	case "prompt_delete_to_line_end":
		return m.deliverOverlay(syntheticKey('k', tea.ModCtrl)), true
	case "prompt_delete_word_backward":
		return m.deliverOverlay(syntheticKey('w', tea.ModCtrl)), true
	case "prompt_delete_word_forward":
		return m.deliverOverlay(syntheticKey(tea.KeyDelete, tea.ModCtrl)), true
	case "prompt_move_left":
		return m.deliverOverlay(syntheticKey(tea.KeyLeft, 0)), true
	case "prompt_move_right":
		return m.deliverOverlay(syntheticKey(tea.KeyRight, 0)), true
	case "prompt_move_start":
		return m.deliverOverlay(syntheticKey(tea.KeyHome, 0)), true
	case "prompt_move_end":
		return m.deliverOverlay(syntheticKey(tea.KeyEnd, 0)), true
	case "prompt_move_word_left":
		return m.deliverOverlay(syntheticKey(tea.KeyLeft, tea.ModCtrl)), true
	case "prompt_move_word_right":
		return m.deliverOverlay(syntheticKey(tea.KeyRight, tea.ModCtrl)), true
	case "prompt_select_prev":
		return m.deliverOverlay(syntheticKey(tea.KeyUp, 0)), true
	case "prompt_select_next":
		return m.deliverOverlay(syntheticKey(tea.KeyDown, 0)), true
	case "prompt_page_up":
		return m.deliverOverlay(syntheticKey(tea.KeyPgUp, 0)), true
	case "prompt_page_down":
		return m.deliverOverlay(syntheticKey(tea.KeyPgDown, 0)), true
	case "prompt_accept_suggestion":
		return m.deliverOverlay(syntheticKey(tea.KeyTab, 0)), true
	case "prompt_paste":
		return m.deliverOverlay(syntheticKey('v', tea.ModCtrl)), true
	case "toggle_search_case_sensitive":
		return m.deliverOverlay(syntheticKey('c', tea.ModAlt)), true
	case "toggle_search_whole_word":
		return m.deliverOverlay(syntheticKey('w', tea.ModAlt)), true
	case "toggle_search_regex":
		return m.deliverOverlay(syntheticKey('r', tea.ModAlt)), true
	case "toggle_search_confirm_each":
		return m.deliverOverlay(syntheticKey('i', tea.ModAlt)), true
	case "popup_select_prev", "menu_up":
		return m.deliverOverlay(syntheticKey(tea.KeyUp, 0)), true
	case "popup_select_next", "menu_down":
		return m.deliverOverlay(syntheticKey(tea.KeyDown, 0)), true
	case "popup_page_up":
		return m.deliverOverlay(syntheticKey(tea.KeyPgUp, 0)), true
	case "popup_page_down":
		return m.deliverOverlay(syntheticKey(tea.KeyPgDown, 0)), true
	case "menu_left":
		return m.deliverOverlay(syntheticKey(tea.KeyLeft, 0)), true
	case "menu_right":
		return m.deliverOverlay(syntheticKey(tea.KeyRight, 0)), true
	case "menu_execute":
		return m.deliverOverlay(syntheticKey(tea.KeyEnter, 0)), true
	case "settings_activate":
		return m.deliverOverlay(syntheticKey(tea.KeyEnter, 0)), true
	case "settings_inherit":
		return m.deliverOverlay(syntheticKey(tea.KeyDelete, 0)), true
	case "settings_help":
		if p, ok := m.overlays[len(m.overlays)-1].(*settingsPage); ok {
			p.help = true
			return nil, true
		}
		return nil, false
	case "settings_search":
		if p, ok := m.overlays[len(m.overlays)-1].(*settingsPage); ok {
			p.startSearch()
			return nil, true
		}
		return nil, false
	case "find_next":
		// With the find bar open, stepping matches is the bar's own operation: the same
		// keys the editor's Find Next and Find Previous run through the bar.
		return m.deliverOverlay(syntheticKey(tea.KeyF3, 0)), true
	case "find_previous":
		return m.deliverOverlay(syntheticKey(tea.KeyF3, tea.ModShift)), true
	}
	// A real model action keeps the dispatch a preset or plugin binding would run, so a
	// user binding in an overlay context to save, a plugin action and so on still runs.
	if m.overlayRealAction(ctx, cmd.Action) {
		return m.dispatch(cmd), true
	}
	return nil, false
}

// overlayRealAction reports whether action is one the model's dispatch answers, as opposed
// to an overlay operation the surface itself owns.
func (m *Model) overlayRealAction(ctx KeyContext, action string) bool {
	if !keymapActionKnown(action) {
		return false
	}
	switch action {
	case "close", "close_tab", "quit", "quit_discard":
		// A close key in an overlay context should not close the tab under the overlay.
		return false
	}
	_ = ctx
	return true
}

// keymapActionKnown reports whether action is one the model dispatches: an editor action,
// a UI action, a command or a plugin's. It is the same set the keybinding loader accepts.
func keymapActionKnown(action string) bool {
	return overlayKnownActions[action]
}

// overlayKnownActions is the action set of KnownActions, built once.
var overlayKnownActions = func() map[string]bool {
	out := make(map[string]bool)
	for _, a := range KnownActions() {
		out[a] = true
	}
	return out
}()

// deliverOverlay gives msg to the top overlay and applies the overlay contract, as
// routeOverlay does. The keymap path uses it to run a bound action through the same
// handler the built-in key would reach.
func (m *Model) deliverOverlay(msg tea.Msg) tea.Cmd {
	if len(m.overlays) == 0 {
		return nil
	}
	top := m.overlays[len(m.overlays)-1]
	next, cmd := top.Update(msg)
	if next == nil {
		m.popOverlay()
	} else {
		m.overlays[len(m.overlays)-1] = next
	}
	for len(m.overlays) > 0 && !m.overlays[len(m.overlays)-1].Active() {
		m.popOverlay()
	}
	return cmd
}

// cancelTopOverlay closes the top overlay the way Esc does when the overlay does not take
// Esc itself: Dismiss if it has one, then pop.
func (m *Model) cancelTopOverlay() tea.Cmd {
	if len(m.overlays) == 0 {
		return nil
	}
	if d, ok := m.overlays[len(m.overlays)-1].(interface{ Dismiss() }); ok {
		d.Dismiss()
	}
	m.popOverlay()
	return m.plugDismissed()
}

// syntheticKey builds the key press a bound overlay action runs as. Text stays empty: a
// filled Text would make the widget match the letter alone instead of the modified
// keystroke.
func syntheticKey(code rune, mod tea.KeyMod) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Mod: mod}
}
