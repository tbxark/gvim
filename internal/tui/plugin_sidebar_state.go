package tui

import (
	"sort"
	"strconv"

	"github.com/tbxark/gvim/pluginapi"
)

// The plugin sidebar sections are runtime state: their stack order, the rows they
// asked for and their folded state. R9-4 saves them in the session beside the
// native sidebar state. The snapshot is bounded (a section count cap, clamped row
// counts, no section content) and additive: unknown or absent ids keep the mount
// order and the plugin defaults, and an older session decodes with no fields.
const (
	maxSessionPluginSections   = 64
	maxSessionPluginSectionRow = 1000
)

// pluginSidebarRestore is the pending sidebar section state of a restored
// session, kept until the sections mount (plugins mount after the model is built).
type pluginSidebarRestore struct {
	order     []string
	rows      map[string]int
	collapsed map[string]bool
}

// pluginSidebarState collects the mounted plugin sections' order, requested rows
// and folded state for the session. It is bounded and never stores section
// content.
func (m *Model) pluginSidebarState() (order []string, rows map[string]int, collapsed map[string]bool) {
	rows = map[string]int{}
	collapsed = map[string]bool{}
	for _, mp := range m.plug.panels.sidebarOrder(m) {
		if len(order) >= maxSessionPluginSections {
			break
		}
		id := strconv.FormatUint(uint64(mp.id), 10)
		order = append(order, id)
		if n := min(mp.mount.Rows, maxSessionPluginSectionRow); n > 0 {
			rows[id] = n
		}
		if m.plug.panels.collapsed[mp.id] {
			collapsed[id] = true
		}
	}
	if len(rows) == 0 {
		rows = nil
	}
	if len(collapsed) == 0 {
		collapsed = nil
	}
	return order, rows, collapsed
}

// stashPluginSidebarRestore keeps the restored state until the sections mount.
func (m *Model) stashPluginSidebarRestore(st SidebarState) {
	if len(st.PluginOrder) == 0 && len(st.PluginRows) == 0 && len(st.PluginCollapsed) == 0 {
		return
	}
	r := &pluginSidebarRestore{order: append([]string(nil), st.PluginOrder...),
		rows: st.PluginRows, collapsed: st.PluginCollapsed}
	if len(r.order) > maxSessionPluginSections {
		r.order = r.order[:maxSessionPluginSections]
	}
	m.plug.panels.restore = r
}

// applyPluginSidebarRestore applies the restored order/rows/fold state to the
// mounted sidebar sections and clears it. It runs once, when the first sections
// have mounted; sections that never mount keep their plugin defaults.
func (m *Model) applyPluginSidebarRestore() {
	ps := &m.plug.panels
	r := ps.restore
	if r == nil {
		return
	}
	sections := ps.sidebarOrder(m)
	if len(sections) == 0 {
		return
	}
	known := make(map[string]*mountedPanel, len(sections))
	for _, mp := range sections {
		known[strconv.FormatUint(uint64(mp.id), 10)] = mp
	}
	for id, on := range r.collapsed {
		mp := known[id]
		if mp == nil || !on {
			continue
		}
		if ps.collapsed == nil {
			ps.collapsed = map[pluginapi.PanelID]bool{}
		}
		ps.collapsed[mp.id] = true
	}
	for id, n := range r.rows {
		mp := known[id]
		if mp == nil || n <= 0 {
			continue
		}
		mp.mount.Rows = min(n, maxSessionPluginSectionRow)
	}
	if len(r.order) > 0 {
		rank := make(map[pluginapi.PanelID]int, len(r.order))
		for i, id := range r.order {
			if i >= maxSessionPluginSections {
				break
			}
			if parsed, err := strconv.ParseUint(id, 10, 64); err == nil {
				rank[pluginapi.PanelID(parsed)] = i
			}
		}
		var slots []int
		for i, id := range ps.order {
			if mp := ps.byID[id]; mp != nil && mp.mount.Placement == pluginapi.PlacementSidebar {
				slots = append(slots, i)
			}
		}
		type slotItem struct {
			id   pluginapi.PanelID
			rank int
			at   int
		}
		items := make([]slotItem, len(slots))
		for k, slot := range slots {
			id := ps.order[slot]
			rr, ok := rank[id]
			if !ok {
				rr = len(r.order) + k
			}
			items[k] = slotItem{id: id, rank: rr, at: k}
		}
		sort.SliceStable(items, func(a, b int) bool {
			if items[a].rank != items[b].rank {
				return items[a].rank < items[b].rank
			}
			return items[a].at < items[b].at
		})
		for k, slot := range slots {
			ps.order[slot] = items[k].id
		}
	}
	ps.restore = nil
}
