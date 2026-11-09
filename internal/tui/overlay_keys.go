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
	"prompt_accept_suggestion", "prompt_paste", "prompt_copy", "prompt_cut", "prompt_select_all",
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
	// Exact menu context and its unbind tombstone outrank the global fallback: a menu
	// F6 unbind must not run the global Remote Indicator action.
	if exact, ok := m.keys.(ExactKeymap); ok {
		if ub, ok := m.keys.(ExactUnbindKeymap); ok && ub.UnboundExact(kp, KeyContextMenu) {
			if m.exactUnbindConsumes(k, kp) {
				return nil, true
			}
			return nil, false
		}
		if cmd, found := exact.ResolveExact(kp, KeyContextMenu); found {
			return m.runMenuBinding(cmd)
		}
	}
	cmd, found := m.keys.Resolve(kp, KeyContextMenu)
	if !found {
		if ub, ok := m.keys.(UnbindKeymap); ok && ub.Unbound(kp, KeyContextMenu) {
			if m.exactUnbindConsumes(k, kp) {
				return nil, true
			}
			return nil, false
		}
		return nil, false
	}
	return m.runMenuBinding(cmd)
}

// runMenuBinding runs one resolved menu-context binding: a menu surface action is
// turned into the synthetic key the dropdown consumes, and a real model action runs
// through dispatch. Reported false leaves the key to the next router.
func (m *Model) runMenuBinding(cmd Command) (tea.Cmd, bool) {
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

// exactUnbindConsumes reports whether an exact-context unbind tombstone should consume
// k: either the global binding would otherwise run the Remote Indicator action, or the
// chord carries no text (Esc/Enter/named keys, where the overlay's built-in action is
// the fallback). A printable key is never swallowed by a tombstone, so prompt input and
// list/settings typeahead keep working.
func (m *Model) exactUnbindConsumes(k tea.Key, kp KeyPress) bool {
	if cmd, ok := m.keys.Resolve(kp, KeyContextGlobal); ok && cmd.Action == "show_remote_indicator_menu" {
		return true
	}
	_, text := translateKey(k)
	return text == ""
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
	// Exact context and its unbind tombstone outrank the global fallback: a modal
	// F6 unbind must not run the global Remote Indicator action, while an unbound
	// ordinary key still falls through to the modal's text handler.
	if exact, ok := m.keys.(ExactKeymap); ok {
		if ub, ok := m.keys.(ExactUnbindKeymap); ok && ub.UnboundExact(kp, ctx) {
			// Swallow the tombstone only for a chord that would otherwise run the
			// remote action or an overlay built-in. A printable key still reaches the
			// field/typeahead, even if the user unbound that character in this context.
			if m.exactUnbindConsumes(k, kp) {
				return nil, true
			}
			return nil, false
		}
		if cmd, found := exact.ResolveExact(kp, ctx); found {
			return m.runOverlayAction(ctx, cmd)
		}
	}
	cmd, found := m.keys.Resolve(kp, ctx)
	if !found {
		if ub, ok := m.keys.(UnbindKeymap); ok && ub.Unbound(kp, ctx) {
			// The user explicitly unbound the key in this context: swallow it in the same
			// cases the exact path does (the remote fallback or a non-text control key),
			// while a printable key still reaches the field/typeahead.
			if m.exactUnbindConsumes(k, kp) {
				return nil, true
			}
			return nil, false
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
	case "prompt_select_all":
		if line, ok := m.overlays[len(m.overlays)-1].(*prompt.Line); ok {
			line.SelectAll()
		}
		return nil, true
	case "prompt_copy":
		return m.promptClipboard(false), true
	case "prompt_cut":
		return m.promptClipboard(true), true
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

// promptClipboard is Fresh's prompt_copy/prompt_cut when the prompt field has no
// selection: the whole text prompt is copied, and a cut also clears it, but only after a
// native clipboard write confirms the copy. A confirm copies its question; cutting a
// confirm has nothing to clear and is a no-op. The command keeps the overlay's focus and
// mode unchanged.
func (m *Model) promptClipboard(cut bool) tea.Cmd {
	if len(m.overlays) == 0 {
		return nil
	}
	line, _ := m.overlays[len(m.overlays)-1].(*prompt.Line)
	if line != nil {
		if cut {
			return m.promptCut(line)
		}
		return m.promptCopy(promptClipTarget{line: line}, line.Value())
	}
	confirm, _ := m.overlays[len(m.overlays)-1].(*prompt.Confirm)
	if cut || confirm == nil {
		return nil
	}
	return m.promptCopy(promptClipTarget{confirm: confirm}, confirm.Question())
}

// beginPromptClip starts a native prompt clipboard operation. Only the latest
// operation's asynchronous result may act, and the permission stays set until
// finishPromptClip sees that operation report. The caller must have acquired the
// process-wide permission with promptClipNativeAcquire first.
func (m *Model) beginPromptClip(op *promptClipOp) (uint64, *promptClipOp) {
	m.promptClipToken++
	m.promptClipPending = true
	m.promptClipPendingToken = m.promptClipToken
	m.promptClipOp = op
	return m.promptClipToken, op
}

// finishPromptClip clears the pending operation when its result reports. It returns the
// operation so only its owner can release the process-wide permission; a stale result
// returns nil and cannot clear a newer workspace's permission.
func (m *Model) finishPromptClip(token uint64) *promptClipOp {
	if !m.promptClipPending || token != m.promptClipPendingToken {
		return nil
	}
	op := m.promptClipOp
	m.promptClipPending = false
	m.promptClipPendingToken = 0
	m.promptClipOp = nil
	return op
}

// cancelPromptClipOp invalidates the action token and marks a not-yet-started native write
// canceled. If no writer call has started (or one already finished), this releases the
// process-wide permission immediately: a canceled request that never reaches Write must
// not block other workspaces. A write already inside ClipboardWriter.Write cannot be
// cancelled; the permission stays set until that command reports or close releases it.
func (m *Model) cancelPromptClipOp() {
	op := m.promptClipOp
	if op != nil {
		op.cancel()
		if !op.started.Load() || op.finished.Load() {
			promptClipNativeRelease(op)
		}
	}
	m.promptClipToken++
}

// promptClipBusy reports whether a prompt write is pending or the process-wide native
// permission is held. A second copy/cut is refused instead of queued.
func (m *Model) promptClipBusy() bool {
	return m.promptClipPending || promptClipNativeBusy()
}

// promptCopy copies the whole prompt text without mutating the prompt. It is single-flight
// with cut: while a native write is pending another copy/cut is refused with a notice, so
// writes cannot run concurrently or land out of order. A native writer is the only path
// when one exists: no OSC 52 is sent beside it, so the two channels cannot reorder. Without
// a native writer only an OSC 52 write is requested as best effort; it is not confirmable.
func (m *Model) promptCopy(target promptClipTarget, text string) tea.Cmd {
	if text == "" {
		return nil
	}
	if m.promptClipBusy() {
		m.notice = uiText("gvim.notice.clipboard.busy", "A clipboard write is still in progress; try again")
		return nil
	}
	w, ok := m.dock.Clipboard().(ClipboardWriter)
	if !ok {
		m.promptClipToken++
		m.notice = uiText("gvim.notice.clipboard.copy_unconfirmed", "OSC 52 clipboard write requested; no native clipboard can confirm the copy")
		return tea.SetClipboard(text)
	}
	op := &promptClipOp{}
	if !promptClipNativeAcquire(op) {
		m.notice = uiText("gvim.notice.clipboard.busy", "A clipboard write is still in progress; try again")
		return nil
	}
	token, _ := m.beginPromptClip(op)
	owner := m.workspaceOwner
	closed := &m.closed
	m.notice = ""
	return func() tea.Msg {
		if !op.beginWrite() {
			op.finished.Store(true)
			if closed.Load() {
				promptClipNativeRelease(op)
			}
			return promptCopyMsg{target: target, text: text, skipped: true, owner: owner, token: token}
		}
		err := w.Write(text)
		op.finished.Store(true)
		if closed.Load() {
			promptClipNativeRelease(op)
		}
		return promptCopyMsg{target: target, text: text, err: err, owner: owner, token: token}
	}
}

// promptCut is the conservative cut: the line is not cleared up front. A native clipboard
// writer is asked first in a command (never in Update); only its successful result clears
// the same line. Without a native writer an OSC 52 write is still requested as a
// best-effort copy, but the line is kept and no Cut is claimed. A failed write keeps the
// text and reports the error, so neither the field nor onChange changes.
func (m *Model) promptCut(line *prompt.Line) tea.Cmd {
	text := line.Value()
	if text == "" {
		return nil
	}
	if m.promptClipBusy() {
		m.notice = uiText("gvim.notice.clipboard.busy", "A clipboard write is still in progress; try again")
		return nil
	}
	w, ok := m.dock.Clipboard().(ClipboardWriter)
	if !ok {
		m.promptClipToken++
		m.notice = uiText("gvim.notice.clipboard.cut_unconfirmed", "OSC 52 clipboard write requested; no native clipboard can confirm the cut, so the prompt was kept")
		return tea.SetClipboard(text)
	}
	op := &promptClipOp{}
	if !promptClipNativeAcquire(op) {
		m.notice = uiText("gvim.notice.clipboard.busy", "A clipboard write is still in progress; try again")
		return nil
	}
	token, _ := m.beginPromptClip(op)
	owner := m.workspaceOwner
	closed := &m.closed
	m.notice = ""
	return func() tea.Msg {
		if !op.beginWrite() {
			op.finished.Store(true)
			if closed.Load() {
				promptClipNativeRelease(op)
			}
			return promptCutMsg{line: line, text: text, skipped: true, owner: owner, token: token}
		}
		err := w.Write(text)
		op.finished.Store(true)
		if closed.Load() {
			promptClipNativeRelease(op)
		}
		return promptCutMsg{line: line, text: text, err: err, owner: owner, token: token}
	}
}
