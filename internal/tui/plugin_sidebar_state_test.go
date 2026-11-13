package tui

import (
	"reflect"
	"testing"

	"github.com/tbxark/gvim/pluginapi"
)

// mountTestSection mounts a sidebar section directly into the model's plugin panel
// state, as the plugin host would.
func mountTestSection(m *Model, id pluginapi.PanelID, rows int) {
	ps := &m.plug.panels
	ps.add(&mountedPanel{id: id, owner: "p", mount: pluginapi.WidgetMount{
		Placement: pluginapi.PlacementSidebar, Rows: rows}})
}

func TestPluginSidebarStateRoundTrip(t *testing.T) {
	t.Parallel()
	m, _ := viewModel(t, DefaultViewOptions(), "abc\n", 80, 24)
	mountTestSection(m, 1, 4)
	mountTestSection(m, 2, 6)
	m.plug.panels.collapsed = map[pluginapi.PanelID]bool{2: true}

	order, rows, collapsed := m.pluginSidebarState()
	if !reflect.DeepEqual(order, []string{"1", "2"}) {
		t.Fatalf("order = %v, want the mount order", order)
	}
	if !reflect.DeepEqual(rows, map[string]int{"1": 4, "2": 6}) {
		t.Fatalf("rows = %v", rows)
	}
	if !reflect.DeepEqual(collapsed, map[string]bool{"2": true}) {
		t.Fatalf("collapsed = %v", collapsed)
	}

	// A fresh model with the restored state applies it when the sections mount.
	restored, _ := viewModel(t, DefaultViewOptions(), "abc\n", 80, 24)
	restored.stashPluginSidebarRestore(SidebarState{
		PluginOrder:     []string{"2", "1"},
		SectionRows:     map[string]int{"plugin:1": 9, "plugin:2": 3},
		PluginCollapsed: map[string]bool{"1": true},
	})
	mountTestSection(restored, 1, 4)
	mountTestSection(restored, 2, 6)
	restored.applyPluginSidebarRestore()
	if got := restored.plug.panels.sidebarOrder(restored); len(got) != 2 || got[0].id != 2 || got[1].id != 1 {
		t.Fatalf("restored order = %v, want section 2 then 1", got)
	}
	if restored.plug.panels.byID[1].mount.Rows != 9 || restored.plug.panels.byID[2].mount.Rows != 3 {
		t.Fatalf("restored rows = %d/%d, want 9/3",
			restored.plug.panels.byID[1].mount.Rows, restored.plug.panels.byID[2].mount.Rows)
	}
	if !restored.plug.panels.collapsed[1] {
		t.Fatal("the restored folded section is not folded")
	}
	// Every saved section has mounted, so nothing is pending any more and the order is
	// not ranked again by later layouts.
	if restored.plug.panels.restore != nil {
		t.Fatalf("the restore %+v outlived the mount of every saved section", restored.plug.panels.restore)
	}
	ps := &restored.plug.panels
	ps.order[0], ps.order[1] = ps.order[1], ps.order[0]
	restored.applyPluginSidebarRestore()
	if got := ps.sidebarOrder(restored); got[0].id != 1 {
		t.Fatalf("a later layout re-ranked the order: first section %d, want 1", got[0].id)
	}
}

// TestPluginSidebarRestoreSkipsRankingWhenNothingMounted checks that while a saved
// section has not mounted, the pending restore stays, and a call that sees no new
// section leaves the order alone.
func TestPluginSidebarRestoreSkipsRankingWhenNothingMounted(t *testing.T) {
	t.Parallel()
	m, _ := viewModel(t, DefaultViewOptions(), "abc\n", 80, 24)
	m.stashPluginSidebarRestore(SidebarState{PluginOrder: []string{"2", "1", "3"}})
	mountTestSection(m, 1, 4)
	mountTestSection(m, 2, 6)
	m.applyPluginSidebarRestore()
	if m.plug.panels.restore == nil {
		t.Fatal("the restore was dropped while section 3 has not mounted")
	}
	ps := &m.plug.panels
	ps.order[0], ps.order[1] = ps.order[1], ps.order[0]
	m.applyPluginSidebarRestore()
	if got := ps.sidebarOrder(m); got[0].id != 1 {
		t.Fatalf("a call with no new section re-ranked the order: first section %d, want 1", got[0].id)
	}
}

func TestPluginSidebarStateIsBounded(t *testing.T) {
	t.Parallel()
	m, _ := viewModel(t, DefaultViewOptions(), "abc\n", 80, 24)
	order := make([]string, maxSessionPluginSections+5)
	for i := range order {
		order[i] = "79"
	}
	m.stashPluginSidebarRestore(SidebarState{
		PluginOrder:     order,
		SectionRows:     map[string]int{"plugin:79": 5000},
		PluginCollapsed: map[string]bool{"79": true},
	})
	if got := len(m.plug.panels.restore.order); got != maxSessionPluginSections {
		t.Fatalf("pending order = %d, want the cap %d", got, maxSessionPluginSections)
	}
	mountTestSection(m, 79, 4)
	m.applyPluginSidebarRestore()
	if got := m.plug.panels.byID[79].mount.Rows; got != maxSessionPluginSectionRow {
		t.Fatalf("restored rows = %d, want the cap %d", got, maxSessionPluginSectionRow)
	}
}

// TestPluginSidebarLateMountRestore checks the lazy restore: a plugin section that
// mounts after the first sidebar layout still gets its saved rows, fold and rank, and a
// later user drag is not overwritten by subsequent lazy applications.
func TestPluginSidebarLateMountRestore(t *testing.T) {
	t.Parallel()
	m, _ := viewModel(t, DefaultViewOptions(), "abc\n", 80, 24)
	m.stashPluginSidebarRestore(SidebarState{
		PluginOrder:     []string{"2", "1"},
		SectionRows:     map[string]int{"plugin:1": 9, "plugin:2": 7},
		PluginCollapsed: map[string]bool{"2": true},
	})
	mountTestSection(m, 1, 4)
	m.applyPluginSidebarRestore()
	if got := m.plug.panels.byID[1].mount.Rows; got != 9 {
		t.Fatalf("early section rows = %d, want 9", got)
	}
	if m.plug.panels.restore == nil {
		t.Fatal("the restore was cleared by the early mount")
	}
	// The second section mounts later; the next lazy application still restores it,
	// including its rank ahead of section 1.
	mountTestSection(m, 2, 6)
	m.applyPluginSidebarRestore()
	if got := m.plug.panels.byID[2].mount.Rows; got != 7 {
		t.Fatalf("late section rows = %d, want 7", got)
	}
	if !m.plug.panels.collapsed[2] {
		t.Fatal("the late section did not get its saved fold")
	}
	got := m.plug.panels.sidebarOrder(m)
	if len(got) != 2 || got[0].id != 2 || got[1].id != 1 {
		t.Fatalf("late section order = %v, want 2 then 1", got)
	}
	// A user drag after the restore is not overwritten by another lazy application.
	m.plug.panels.byID[1].mount.Rows = 5
	m.applyPluginSidebarRestore()
	if rows := m.plug.panels.byID[1].mount.Rows; rows != 5 {
		t.Fatalf("a later lazy application overwrote the user's rows: %d", rows)
	}
}
