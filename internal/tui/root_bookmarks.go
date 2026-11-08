package tui

import (
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/prompt"
)

// bookmarkTag is the tag of the slot prompt. jump tells the jump prompt from the set
// prompt, so the answer runs the right editor action.
type bookmarkTag struct{ jump bool }

// openBookmarkPrompt asks for a bookmark slot, a digit from 0 to 9, to set or to jump to.
func (m *Model) openBookmarkPrompt(jump bool) {
	label := "Set bookmark (0-9):"
	if jump {
		label = "Jump to bookmark (0-9):"
	}
	m.pushOverlay(prompt.NewLine(m.styles, prompt.LineOptions{
		Label:    label,
		Tag:      bookmarkTag{jump: jump},
		Validate: validateSlot,
	}))
}

// validateSlot accepts one digit, the name of a bookmark slot.
func validateSlot(text string) error {
	if len(text) != 1 || text[0] < '0' || text[0] > '9' {
		return errors.New("a bookmark slot is one digit from 0 to 9")
	}
	return nil
}

// bookmarkAnswer runs set_bookmark or jump_to_bookmark for the slot the prompt
// answered. The editor reports a jump to a slot that is not set as an error, which
// shows on the status row. A set shows the editor's own status.
func (m *Model) bookmarkAnswer(tag bookmarkTag, d prompt.Done) tea.Cmd {
	if d.Outcome != prompt.Submitted {
		return nil
	}
	action := editor.Action("set_bookmark")
	if tag.jump {
		action = "jump_to_bookmark"
	}
	res := m.editorDo(action, map[string]any{"char": d.Text})
	m.syncViewport()
	if res.Err != nil {
		m.notice = res.Err.Error()
	}
	return nil
}
