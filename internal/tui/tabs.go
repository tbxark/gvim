package tui

import (
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/list"
	"github.com/tbxark/gvim/internal/tui/split"
)

// stripTabs is the editor's tabs with the file type icon of the icon set filled in,
// for the strip to place and draw. Plain mode has no file icons, so the tabs pass
// through as they are.
func (m *Model) stripTabs() []TabInfo {
	tabs := m.ed.Tabs()
	plain := m.view.icons.FileIcon("", false, false) == ""
	pair := m.splits.comparing && !m.splits.Root.Leaf()
	anyRO := false
	for i := range tabs {
		if !tabs[i].ReadOnly {
			if doc := m.docAt(i); doc != nil && isDocReadOnly(doc) {
				anyRO = true
				break
			}
		}
	}
	if plain && !pair && !anyRO {
		return tabs
	}
	out := slices.Clone(tabs)
	for i := range out {
		if !plain {
			out[i].Icon = m.view.icons.FileIcon(strings.TrimSuffix(out[i].Name, frame.PreviewSuffix), false, false)
		}
		if !out[i].ReadOnly {
			if doc := m.docAt(i); doc != nil && isDocReadOnly(doc) {
				out[i].ReadOnly = true
			}
		}
	}
	if pair {
		// The two files of a comparison are two tabs, so the strip says they belong together.
		for _, n := range []*split.Node{m.splits.Root.A, m.splits.Root.B} {
			if n.Tab >= 0 && n.Tab < len(out) {
				out[n.Tab].Name = frame.ComparePairMark + " " + out[n.Tab].Name
			}
		}
	}
	return out
}

// openTabList opens the list of the tab names for Switch to Tab by Name. Choosing a row
// activates that tab. With no tab open the notice says so, and no list opens.
func (m *Model) openTabList() tea.Cmd {
	tabs := m.ed.Tabs()
	if len(tabs) == 0 {
		m.notice = "No open tabs to switch to"
		return nil
	}
	rows := make([]list.Row, len(tabs))
	for i, t := range tabs {
		rows[i] = list.Row{Left: t.Name, Value: strconv.Itoa(i)}
	}
	run := func(v *list.View, r list.Row) tea.Cmd {
		v.Close()
		i, err := strconv.Atoi(r.Value)
		if err != nil {
			return nil
		}
		m.activateTab(i)
		m.focusEditor()
		m.notice = "Switched to " + r.Left
		return nil
	}
	m.pushOverlay(list.New("Switch to tab", rows, m.listChrome(), run))
	return nil
}
