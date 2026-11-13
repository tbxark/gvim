package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/pluginpanel"
	"github.com/tbxark/gvim/internal/tui/sidebar"
	"github.com/tbxark/gvim/pluginapi"
)

// TestSidebarPerSectionHeightDrags checks the generic divider: every section
// (explorer, Contents, Diagnostics, plugin) can be resized by dragging its header,
// with the dragged section clamped at one row and every other open section keeping at
// least one; a click without motion still folds it.
func TestSidebarPerSectionHeightDrags(t *testing.T) {
	t.Parallel()
	ed := newTOCEditor("notes.md", "# One", "## Two")
	m := newTOCModel(t, ed, nil)
	m.diag.open = true
	m.diag.rows = 6

	// Contents: dragging its header up grows it (the pre-existing behaviour).
	l := m.layout()
	cs, ok := contentsSection(l)
	if !ok {
		t.Fatal("no Contents section")
	}
	start := m.contents.Rows()
	m.Update(click(5, cs.HeaderY, tea.MouseLeft))
	m.Update(tea.MouseMotionMsg(tea.Mouse{X: 5, Y: cs.HeaderY - 2, Button: tea.MouseLeft}))
	m.Update(tea.MouseReleaseMsg(tea.Mouse{X: 5, Y: cs.HeaderY - 2, Button: tea.MouseLeft}))
	if got := m.contents.Rows(); got != start+2 {
		t.Fatalf("Contents rows = %d, want %d", got, start+2)
	}
	if m.contents.Collapsed() {
		t.Fatal("a Contents drag folded the section")
	}

	// Diagnostics: same generic drag on its own header.
	l = m.layout()
	ds := sectionOfKind(t, l, sidebar.KindDiagnostics)
	dstart := m.diag.Rows()
	m.Update(click(l.ExX+3, ds.HeaderY, tea.MouseLeft))
	m.Update(tea.MouseMotionMsg(tea.Mouse{X: l.ExX + 3, Y: ds.HeaderY - 2, Button: tea.MouseLeft}))
	m.Update(tea.MouseReleaseMsg(tea.Mouse{X: l.ExX + 3, Y: ds.HeaderY - 2, Button: tea.MouseLeft}))
	if got := m.diag.Rows(); got != dstart+2 {
		t.Fatalf("Diagnostics rows = %d, want %d", got, dstart+2)
	}

	// Explorer: its top header dragged down grows the tree section.
	l = m.layout()
	es := sectionOfKind(t, l, sidebar.KindExplorer)
	estart := m.explorerRows
	if estart <= 0 {
		estart = es.Rows()
	}
	m.Update(click(l.ExX+3, es.HeaderY, tea.MouseLeft))
	m.Update(tea.MouseMotionMsg(tea.Mouse{X: l.ExX + 3, Y: es.HeaderY + 2, Button: tea.MouseLeft}))
	m.Update(tea.MouseReleaseMsg(tea.Mouse{X: l.ExX + 3, Y: es.HeaderY + 2, Button: tea.MouseLeft}))
	if got := m.explorerRows; got != estart+2 {
		t.Fatalf("Explorer rows = %d, want %d", got, estart+2)
	}

	// Plugin: mount a sidebar section and drag its header up.
	mp := &mountedPanel{id: 77, owner: "p", mount: pluginapi.WidgetMount{
		Placement: pluginapi.PlacementSidebar, Rows: 4,
	}, panel: pluginpanel.New(pluginapi.WidgetPanel{}, pluginpanel.Options{})}
	m.plug.panels.add(mp)
	l = m.layout()
	ps := sectionOfKind(t, l, sidebar.KindPlugin)
	pstart := mp.mount.Rows
	m.Update(click(l.ExX+3, ps.HeaderY, tea.MouseLeft))
	m.Update(tea.MouseMotionMsg(tea.Mouse{X: l.ExX + 3, Y: ps.HeaderY - 1, Button: tea.MouseLeft}))
	m.Update(tea.MouseReleaseMsg(tea.Mouse{X: l.ExX + 3, Y: ps.HeaderY - 1, Button: tea.MouseLeft}))
	if got := mp.mount.Rows; got != pstart+1 {
		t.Fatalf("plugin rows = %d, want %d", got, pstart+1)
	}

	// A press/release without motion still folds the section (the headers keep their
	// click behaviour; drag is the new part).
	l = m.layout()
	ds = sectionOfKind(t, l, sidebar.KindDiagnostics)
	m.Update(click(l.ExX+3, ds.HeaderY, tea.MouseLeft))
	m.Update(tea.MouseReleaseMsg(tea.Mouse{X: l.ExX + 3, Y: ds.HeaderY, Button: tea.MouseLeft}))
	if !m.diag.Collapsed() {
		t.Fatal("a click on the Diagnostics header did not fold it")
	}
}

// sectionOfKind returns the layout section for kind.
func sectionOfKind(t *testing.T, l frame.Layout, kind sidebar.Kind) sidebar.Section {
	t.Helper()
	for _, sec := range l.Sections {
		if sec.Kind == kind {
			return sec
		}
	}
	t.Fatalf("no section of kind %v in %+v", kind, l.Sections)
	return sidebar.Section{}
}

// TestSidebarSectionHeightsRestoreAndClamp checks the session path: unified
// SectionRows collect, clamps on restore (Contents 1..200, others 1..1000, plugin id
// rows capped), and a late-mounting plugin section still gets its saved height.
func TestSidebarSectionHeightsRestoreAndClamp(t *testing.T) {
	t.Parallel()
	ed := newTOCEditor("notes.md", "# One")
	m := newTOCModel(t, ed, nil)
	m.diag.open = true
	m.explorerRows = 4
	m.contents.SetRows(12)
	m.diag.rows = 6
	mp := &mountedPanel{id: 79, owner: "p", mount: pluginapi.WidgetMount{
		Placement: pluginapi.PlacementSidebar, Rows: 5,
	}, panel: pluginpanel.New(pluginapi.WidgetPanel{}, pluginpanel.Options{})}
	m.plug.panels.add(mp)
	st := m.SidebarState()
	if st.SectionRows["explorer"] != 4 || st.SectionRows["contents"] != 12 ||
		st.SectionRows["diagnostics"] != 6 || st.SectionRows["plugin:79"] != 5 {
		t.Fatalf("collected section rows = %+v", st.SectionRows)
	}

	// Restore into a fresh model with clamped values, including a plugin section that
	// mounts after the first sidebar layout.
	restored := newTOCModel(t, newTOCEditor("notes.md", "# One"), nil)
	restored.diag.open = true
	restored.restoreSidebarState(SidebarState{
		SectionRows: map[string]int{
			"explorer": 5000, "contents": 5000, "diagnostics": -3, "plugin:79": 5000,
		},
	})
	if restored.explorerRows != maxSidebarSectionRows {
		t.Fatalf("explorer rows = %d, want %d", restored.explorerRows, maxSidebarSectionRows)
	}
	if restored.contents.Rows() != sidebar.MaxRows {
		t.Fatalf("contents rows = %d, want %d", restored.contents.Rows(), sidebar.MaxRows)
	}
	if restored.diag.Rows() != diagDefaultRows {
		t.Fatalf("negative diagnostics rows = %d, want the default %d", restored.diag.Rows(), diagDefaultRows)
	}
	// The plugin row waits for its mount and is clamped to the plugin cap.
	late := &mountedPanel{id: 79, owner: "p", mount: pluginapi.WidgetMount{
		Placement: pluginapi.PlacementSidebar, Rows: 2,
	}, panel: pluginpanel.New(pluginapi.WidgetPanel{}, pluginpanel.Options{})}
	restored.plug.panels.add(late)
	restored.applyPluginSidebarRestore()
	if late.mount.Rows != maxSessionPluginSectionRow {
		t.Fatalf("late plugin rows = %d, want the cap %d", late.mount.Rows, maxSessionPluginSectionRow)
	}
}

// TestSidebarSectionRowsRestore checks that SectionRows is the one source of the section
// heights: the explorer, Contents, Diagnostics and plugin sections all come from it.
func TestSidebarSectionRowsRestore(t *testing.T) {
	t.Parallel()
	m := newTOCModel(t, newTOCEditor("notes.md", "# One"), nil)
	m.diag.open = true
	m.restoreSidebarState(SidebarState{
		SectionRows:     map[string]int{"explorer": 7, "contents": 5, "diagnostics": 4, "plugin:79": 8},
		PluginOrder:     []string{"79"},
		PluginCollapsed: map[string]bool{"79": true},
	})
	if m.explorerRows != 7 {
		t.Fatalf("explorer rows = %d, want 7", m.explorerRows)
	}
	if got := m.contents.Rows(); got != 5 {
		t.Fatalf("contents rows = %d, want 5", got)
	}
	if got := m.diag.Rows(); got != 4 {
		t.Fatalf("diagnostics rows = %d, want 4", got)
	}
	late := &mountedPanel{id: 79, owner: "p", mount: pluginapi.WidgetMount{
		Placement: pluginapi.PlacementSidebar, Rows: 2,
	}, panel: pluginpanel.New(pluginapi.WidgetPanel{}, pluginpanel.Options{})}
	m.plug.panels.add(late)
	m.applyPluginSidebarRestore()
	if late.mount.Rows != 8 {
		t.Fatalf("plugin rows = %d, want 8", late.mount.Rows)
	}
}

// TestSidebarStateSavesOnlyExplicitHeights checks the capture rule: a config default
// contents.rows/diagnostics/explorer height is not written as a session override, so a
// later config edit wins on restart; only a drag/restore override is saved.
func TestSidebarStateSavesOnlyExplicitHeights(t *testing.T) {
	t.Parallel()
	defaults := DefaultMarkdownTOC()
	m := newTOCModel(t, newTOCEditor("notes.md", "# One"), &defaults)
	m.diag.open = true
	st := m.SidebarState()
	if len(st.SectionRows) != 0 {
		t.Fatalf("default heights were saved: %+v", st.SectionRows)
	}
	// A new config contents.rows applies on restart because the session did not override it.
	bigger := DefaultMarkdownTOC()
	bigger.Settings.Rows = 25
	next := newTOCModel(t, newTOCEditor("notes.md", "# One"), &bigger)
	next.restoreSidebarState(st)
	if got := next.contents.Rows(); got != 25 {
		t.Fatalf("contents rows after a config change + restart = %d, want 25", got)
	}

	// An explicit drag/restore value is saved and does win after a restart.
	m.contents.SetRows(9)
	m.diag.rows = 5
	m.explorerRows = 4
	explicit := m.SidebarState()
	if explicit.SectionRows["contents"] != 9 || explicit.SectionRows["diagnostics"] != 5 || explicit.SectionRows["explorer"] != 4 {
		t.Fatalf("explicit heights not saved: %+v", explicit.SectionRows)
	}
	again := newTOCModel(t, newTOCEditor("notes.md", "# One"), &bigger)
	again.restoreSidebarState(explicit)
	if got := again.contents.Rows(); got != 9 {
		t.Fatalf("explicit contents rows after restart = %d, want 9", got)
	}
}

// TestSidebarStateCaptureDoesNotApplyPluginRestore checks the capture path: taking the
// session snapshot must not apply pending plugin section state to the mounted model, and
// it still reports the pending saved rows (so an unmounted section is not lost).
func TestSidebarStateCaptureDoesNotApplyPluginRestore(t *testing.T) {
	t.Parallel()
	m := newTOCModel(t, newTOCEditor("notes.md", "# One"), nil)
	m.stashPluginSidebarRestore(SidebarState{
		PluginOrder: []string{"1"},
		SectionRows: map[string]int{"plugin:1": 9},
	})
	mountTestSection(m, 1, 3)
	st := m.SidebarState()
	if got := m.plug.panels.byID[1].mount.Rows; got != 3 {
		t.Fatalf("capture applied the pending restore: mounted rows = %d, want 3", got)
	}
	if m.plug.panels.restore == nil || len(m.plug.panels.restore.applied) != 0 {
		t.Fatalf("capture mutated the pending restore: %+v", m.plug.panels.restore)
	}
	if st.SectionRows["plugin:1"] != 9 {
		t.Fatalf("capture lost the pending plugin rows: %+v", st.SectionRows)
	}
}

// TestPluginUnmountClearsDividerPointer checks a plugin section unmounted mid-drag does
// not leave a divider pointing at the gone panel: the release/motion is dropped and a
// later message cannot resize it.
func TestPluginUnmountClearsDividerPointer(t *testing.T) {
	t.Parallel()
	ed := newTOCEditor("notes.md", "# One")
	m := newTOCModel(t, ed, nil)
	mp := &mountedPanel{id: 77, owner: "p", mount: pluginapi.WidgetMount{
		Placement: pluginapi.PlacementSidebar, Rows: 5,
	}, panel: pluginpanel.New(pluginapi.WidgetPanel{}, pluginpanel.Options{})}
	m.plug.panels.add(mp)
	l := m.layout()
	ps := sectionOfKind(t, l, sidebar.KindPlugin)
	m.Update(click(m.layout().ExX+3, ps.HeaderY, tea.MouseLeft))
	if !m.divider.active || m.divider.panel != mp {
		t.Fatalf("the plugin header press did not start its divider: %+v", m.divider)
	}
	m.unmountPanel(mp.id)
	if m.divider.active || m.divider.panel != nil {
		t.Fatalf("unmount left the divider pointing at the panel: %+v", m.divider)
	}
	// A later motion/release is a no-op (it must not panic or touch the gone section).
	rows := mp.mount.Rows
	m.Update(tea.MouseMotionMsg(tea.Mouse{X: m.layout().ExX + 3, Y: ps.HeaderY - 1, Button: tea.MouseLeft}))
	m.Update(tea.MouseReleaseMsg(tea.Mouse{X: m.layout().ExX + 3, Y: ps.HeaderY - 1, Button: tea.MouseLeft}))
	if mp.mount.Rows != rows {
		t.Fatalf("motion after unmount resized the gone plugin section: %d -> %d", rows, mp.mount.Rows)
	}
}
