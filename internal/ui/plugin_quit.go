package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/ui/prompt"
)

// quit_discard (stage S22, AMENDMENTS C15) is the hidden action behind vi's :qa!. It is not
// in the palette, the menus or the keymap; a plugin reaches it with gvim.command.run, and
// it is a reentrant action (plugin_cmds.go), so it runs after the Lua slice that asked.
//
// It never quits on its own. It asks first, naming the tabs that hold unsaved changes, and
// only Yes goes on: each of those tabs is closed with force, which is what the Discard
// answer of the unsaved-changes dialog does, and then Model.quit takes the same final path
// as a quit that discarded its last dirty tab. Nothing is saved.

// quitDiscardTag is the tag of the question, which pluginAnswer routes to quitDiscardAnswer.
type quitDiscardTag struct{}

// maxNamedTabs is how many tab names the question lists before it counts the rest.
const maxNamedTabs = 5

// quitDiscard asks whether to quit and throw away the unsaved changes. With no unsaved
// tab it still asks: the question is the only way in.
func (m *Model) quitDiscard() tea.Cmd {
	var names []string
	for _, t := range m.ed.Tabs() {
		if t.Modified {
			names = append(names, t.Name)
		}
	}
	q := "Quit without saving?"
	if len(names) > 0 {
		list := names
		if len(list) > maxNamedTabs {
			list = list[:maxNamedTabs]
		}
		q = "Discard the unsaved changes in " + strings.Join(list, ", ")
		if rest := len(names) - len(list); rest > 0 {
			q += fmt.Sprintf(" and %d more", rest)
		}
		q += " and quit?"
	}
	m.pushOverlay(confirmOverlay{c: prompt.NewConfirm(m.styles, q, quitDiscardTag{})})
	return nil
}

// quitDiscardAnswer applies the answer: only Yes quits.
func (m *Model) quitDiscardAnswer(d prompt.Done) tea.Cmd {
	if d.Outcome != prompt.Yes {
		return nil
	}
	if t := m.tabPort(); t != nil {
		tabs := m.ed.Tabs()
		for i := len(tabs) - 1; i >= 0; i-- {
			if !tabs[i].Modified {
				continue
			}
			if err := m.closeTab(t, i, true); err != nil {
				m.notice = err.Error()
				return nil
			}
		}
		m.syncViewport()
	}
	return m.quit()
}
