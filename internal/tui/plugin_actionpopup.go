package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/popup"
	"github.com/tbxark/gvim/internal/tui/prompt"
	"github.com/tbxark/gvim/pluginapi"
)

// Action popups (plugin API tier 1a): gvim.ui.action_popup shows a message with action
// buttons and resolves the wait with the chosen action id. It is the plugin popup with a
// note block above the buttons, so it shares the popup overlay's input and frame.

// waitActionPopup is the wait kind of an action popup.
const waitActionPopup = "action_popup"

// Caps of one action popup.
const (
	maxActionActions = 16
	maxActionLabel   = 200
	maxActionID      = 128
	maxActionMessage = 4 << 10
)

// pluginActionPopupData is the payload of an action popup row (popup.Item.Data).
type pluginActionPopupData struct {
	tok    pluginapi.Token
	id     uint64
	action string
}

// pluginActionPopupTag says the action popup id of wait tok closed without a choice.
type pluginActionPopupTag struct {
	tok pluginapi.Token
	id  uint64
}

// ShowActionPopup shows a message with action buttons. The result is Values{action id} of
// the chosen button, or Values{nil} when the popup closed without one.
func (h *pluginHost) ShowActionPopup(t pluginapi.Token, id uint64, o pluginapi.ActionPopupSpec) error {
	m := h.model()
	if len(o.Actions) == 0 || len(o.Actions) > maxActionActions {
		return pluginapi.Errf(pluginapi.CodeInvalid, "an action popup takes 1 to %d actions", maxActionActions)
	}
	if len(o.Title) > maxActionLabel || len(o.Message) > maxActionMessage {
		return pluginapi.Errf(pluginapi.CodeLimit, "the title or message is too long")
	}
	for _, a := range o.Actions {
		if a.ID == "" || len(a.ID) > maxActionID || len(a.Label) > maxActionLabel {
			return pluginapi.Errf(pluginapi.CodeInvalid, "an action needs an id and a label")
		}
	}
	if err := m.plugWait(t, waitActionPopup); err != nil {
		return err
	}
	m.plug.later = append(m.plug.later, func() tea.Cmd { return m.openActionPopup(t, id, o) })
	return nil
}

func (m *Model) openActionPopup(t pluginapi.Token, id uint64, o pluginapi.ActionPopupSpec) tea.Cmd {
	if _, ok := m.plug.waits[t]; !ok {
		return nil
	}
	items := make([]popup.Item, len(o.Actions))
	for i, a := range o.Actions {
		items[i] = popup.Item{Label: cleanText(a.Label), Data: pluginActionPopupData{tok: t, id: id, action: a.ID}}
	}
	title := cleanText(o.Title)
	p := popup.New(popupStyles(m.styles))
	p.ShowNote(m.popupAnchor("center", title, items, 0), title, cleanText(o.Message), items)
	m.pushOverlay(&pluginActionPopupOverlay{plugBase: plugBase{m: m, tok: t}, id: id, p: p})
	return nil
}

// pluginActionPopupOverlay is the action popup of a plugin.
type pluginActionPopupOverlay struct {
	plugBase
	id uint64
	p  *popup.Popup
}

func (o *pluginActionPopupOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	if o.closed {
		return nil, nil
	}
	_, cmd := o.p.Update(msg)
	if o.p.Active() {
		return o, cmd
	}
	if cmd == nil {
		// A click outside closed it: no action was chosen.
		cmd = doneCmd(prompt.Done{Tag: pluginActionPopupTag{tok: o.tok, id: o.id}})
	}
	return nil, cmd
}

func (o *pluginActionPopupOverlay) Draw(g *grid.Grid) {
	if !o.closed {
		o.p.Draw(g)
	}
}
func (o *pluginActionPopupOverlay) Active() bool { return !o.closed && o.p.Active() }
func (o *pluginActionPopupOverlay) Dismiss() {
	o.dismissed(prompt.Done{Tag: pluginActionPopupTag{tok: o.tok, id: o.id}})
}

// AlertOverlay marks the action popup as an alert: the plugin waits for the chosen
// action. See overlay.Alert.
func (*pluginActionPopupOverlay) AlertOverlay() {}

// pluginActionPopupDone resolves the wait of an action popup that closed without a choice,
// and reports the result to the plugins.
func (m *Model) pluginActionPopupDone(tag pluginActionPopupTag) tea.Cmd {
	m.plugEmit("action_popup_result", map[string]any{"popup": tag.id, "action": nil})
	return m.plugFinish(pluginapi.Completion{Token: tag.tok, Values: []any{nil}})
}

// pluginActionChosen resolves the wait of an action popup whose button was chosen. It
// reports whether the row was an action popup's.
func (m *Model) pluginActionChosen(c popup.Chosen) (tea.Cmd, bool) {
	d, ok := c.Item.Data.(pluginActionPopupData)
	if !ok {
		return nil, false
	}
	m.plugEmit("action_popup_result", map[string]any{"popup": d.id, "action": d.action})
	return m.plugFinish(pluginapi.Completion{Token: d.tok, Values: []any{d.action}}), true
}
