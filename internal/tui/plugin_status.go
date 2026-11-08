package tui

import (
	"math"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/space"
	"github.com/tbxark/gvim/pluginapi"
)

// The plugin items of the status row (stage S14). computeLayout places them with the other
// segments and gives each clickable one a run of kind hitStatusPlugin, so painting and hit
// testing read the same geometry.

// statusItem is one plugin item as the layout sees it.
type statusItem struct {
	id, plugin, text string
	right            bool
	prio             int
	clickable        bool
}

// statusState holds the items plugins set, by plugin.
type statusState struct {
	items map[string][]pluginapi.StatusItem
	order []string
}

// maxStatusItemCells bounds an item's text, whatever the plugin sent.
const maxStatusItemCells = 80

// SetStatusItems replaces the status items of plugin.
func (h *pluginHost) SetStatusItems(plugin string, items []pluginapi.StatusItem) {
	st := &h.m.plug.keys.status
	if len(items) == 0 {
		h.m.plugStatusGone(plugin, 0)
		return
	}
	if st.items == nil {
		st.items = make(map[string][]pluginapi.StatusItem)
	}
	if _, known := st.items[plugin]; !known {
		st.order = append(st.order, plugin)
	}
	st.items[plugin] = slices.Clone(items)
}

// plugStatusGone drops the status items that the plugin (at generation gen) set.
func (m *Model) plugStatusGone(plugin string, gen uint64) {
	_ = gen
	st := &m.plug.keys.status
	if _, ok := st.items[plugin]; !ok {
		return
	}
	delete(st.items, plugin)
	st.order = slices.DeleteFunc(st.order, func(s string) bool { return s == plugin })
}

// plugStatusItems returns the items the status row shows, in the order the layout places
// them: the active mode's label first, then the plugins' items by priority (higher first;
// a higher item is dropped last when the row is narrow), ties in registration order.
func (m *Model) plugStatusItems() []statusItem {
	if m.plug.rt == nil {
		return nil
	}
	ks := &m.plug.keys
	var out []statusItem
	if um := m.activeMode(); um != nil {
		// The mode of the editor shows the label its plugin gave it. The mode of a buffer, which
		// has no plugin code to draw one, always has a label, and one that does not bind Esc
		// says how to leave it.
		text, owner := um.info.Status, ks.editor.plugin
		if ks.editor == (modeRef{}) {
			owner = um.info.Plugin
			if text == "" {
				text = "-- " + strings.ToUpper(um.info.Name) + " --"
			}
			if um.root.next["escape"] == nil {
				text += " (Esc x3 leaves)"
			}
		}
		if text != "" {
			out = append(out, statusItem{
				id: "mode:" + um.info.Name, plugin: owner, text: plainCells(text), prio: math.MaxInt,
			})
		}
	}
	first := len(out)
	for _, p := range ks.status.order {
		for _, it := range ks.status.items[p] {
			out = append(out, statusItem{
				id: it.ID, plugin: p, text: plainCells(it.Text), right: it.Right, prio: it.Priority, clickable: it.Clickable,
			})
		}
	}
	slices.SortStableFunc(out[first:], func(a, b statusItem) int { return b.prio - a.prio })
	return out
}

// plainCells makes text one line of at most maxStatusItemCells cells.
func plainCells(s string) string {
	return space.End(cleanText(s), maxStatusItemCells)
}

// plugStatusClick runs the on_click function of the item at index in the list
// plugStatusItems gave the layout.
func (m *Model) plugStatusClick(index int) tea.Cmd {
	items := m.plugStatusItems()
	if index < 0 || index >= len(items) || !items[index].clickable {
		return nil
	}
	it := items[index]
	return m.pluginRun(pluginapi.StatusClickAction(it.plugin, it.id), nil, pluginapi.SrcMenu)
}
