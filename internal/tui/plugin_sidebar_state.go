package tui

import (
	"sort"
	"strconv"

	"github.com/tbxark/gvim/pluginapi"
)

// The plugin sidebar sections are runtime state: their stack order, the rows they
// asked for and their folded state. The session saves them beside the
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
	// applied marks the mounted ids that were visited, so a late mount still gets
	// its saved state exactly once while a later user drag is not overwritten by
	// repeated lazy applications.
	applied map[pluginapi.PanelID]bool
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
	rows := pluginRowsOf(st.SectionRows)
	if len(st.PluginOrder) == 0 && len(rows) == 0 && len(st.PluginCollapsed) == 0 {
		return
	}
	r := &pluginSidebarRestore{order: append([]string(nil), st.PluginOrder...),
		rows: rows, collapsed: st.PluginCollapsed}
	if len(r.order) > maxSessionPluginSections {
		r.order = r.order[:maxSessionPluginSections]
	}
	m.plug.panels.restore = r
}

// applyPluginSidebarRestore applies the restored order/rows/fold state lazily: every
// call looks at the currently mounted sidebar sections, applies the saved state to
// ids it has not visited yet and, only when there was one, re-ranks the mounted order
// by the saved order. It therefore also covers a section that mounts late, while keeping
// the state bounded and never overwriting a later user drag (each id is applied once).
// Once every saved id has mounted the pending restore is dropped.
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
	if r.applied == nil {
		r.applied = make(map[pluginapi.PanelID]bool, len(sections))
	}
	// Rows and folds apply once per mounted id; a section that mounts later is picked
	// up on the next lazy call, and a drag done after this point is not overwritten.
	fresh := false
	for _, mp := range sections {
		if r.applied[mp.id] {
			continue
		}
		fresh = true
		r.applied[mp.id] = true
		id := strconv.FormatUint(uint64(mp.id), 10)
		if n := r.rows[id]; n > 0 {
			mp.mount.Rows = min(n, maxSessionPluginSectionRow)
		}
		if r.collapsed[id] {
			if ps.collapsed == nil {
				ps.collapsed = map[pluginapi.PanelID]bool{}
			}
			ps.collapsed[mp.id] = true
		}
	}
	if !fresh {
		return
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
	if r.allApplied() {
		ps.restore = nil
	}
}

// allApplied reports whether every saved section id has mounted and been visited, so
// nothing is left to restore. An id that does not parse is ignored.
func (r *pluginSidebarRestore) allApplied() bool {
	done := func(id string) bool {
		n := parsePanelID(id)
		return n == 0 || r.applied[pluginapi.PanelID(n)]
	}
	for _, id := range r.order {
		if !done(id) {
			return false
		}
	}
	for id := range r.rows {
		if !done(id) {
			return false
		}
	}
	for id := range r.collapsed {
		if !done(id) {
			return false
		}
	}
	return true
}

// pluginSidebarCaptureState is the session snapshot of the plugin sections. Unlike the
// layout path it never applies the pending restore: it starts from the mounted state and
// folds the pending order/rows/folds for ids that have not been applied yet (an id can be
// mounted but not visited by the first layout, or not mounted at all). Everything stays
// bounded and the model is not mutated.
func (m *Model) pluginSidebarCaptureState() (order []string, rows map[string]int, collapsed map[string]bool) {
	order, rows, collapsed = m.pluginSidebarState()
	r := m.plug.panels.restore
	if r == nil {
		return order, rows, collapsed
	}
	if rows == nil {
		rows = map[string]int{}
	}
	if collapsed == nil {
		collapsed = map[string]bool{}
	}
	mounted := make(map[string]bool, len(order))
	for _, id := range order {
		mounted[id] = true
	}
	add := func(id string) bool {
		if mounted[id] || len(order) >= maxSessionPluginSections {
			return false
		}
		mounted[id] = true
		order = append(order, id)
		return true
	}
	applyPending := func(id string) {
		if r.applied[pluginapi.PanelID(parsePanelID(id))] {
			return
		}
		if n := r.rows[id]; n > 0 {
			rows[id] = min(n, maxSessionPluginSectionRow)
		}
		if r.collapsed[id] {
			collapsed[id] = true
		}
	}
	for _, id := range r.order {
		if add(id) {
			applyPending(id)
			continue
		}
		if mounted[id] {
			applyPending(id)
		}
	}
	// A rows id that the order did not list still belongs to the snapshot if it has not
	// mounted; cap the appended count.
	pending := 0
	for id := range r.rows {
		if pending >= maxSessionPluginSections {
			break
		}
		if !mounted[id] && add(id) {
			applyPending(id)
			pending++
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

// parsePanelID parses a saved plugin panel id for the applied-state lookup; malformed
// ids are 0, which no mounted panel uses in the session state.
func parsePanelID(id string) uint64 {
	n, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return 0
	}
	return n
}
