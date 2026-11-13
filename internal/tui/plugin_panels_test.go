package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/palette"
	"github.com/tbxark/gvim/internal/tui/sidebar"
	"github.com/tbxark/gvim/internal/tui/space"
	"github.com/tbxark/gvim/pluginapi"
)

// widgetMountFixture is a small panel: a label over a button and a checked toggle, with the
// button focused. It exercises the column layout, the focus ring and a toggle.
func widgetMountFixture() pluginapi.WidgetMount {
	return pluginapi.WidgetMount{
		Panel: pluginapi.WidgetPanel{
			Root: pluginapi.Widget{Kind: pluginapi.WidgetCol, Children: []pluginapi.Widget{
				{Kind: pluginapi.WidgetLabel, Text: "Files"},
				{Kind: pluginapi.WidgetButton, Key: "ok", Label: "OK"},
				{Kind: pluginapi.WidgetToggle, Key: "wrap", Label: "Wrap", Checked: true},
			}},
			FocusKey: "ok",
		},
		Title:     "Box",
		Placement: pluginapi.PlacementFloat,
		WidthPct:  50,
		HeightPct: 40,
		Focus:     true,
		Closable:  true,
	}
}

// widgetPanelModel returns a model with a fake plugin runtime that mounted the fixture panel
// from a command, and the harness's panel state.
func widgetPanelModel(t *testing.T) (*Model, *fakeRuntime) {
	t.Helper()
	ed := newFakeEditor("hello")
	m := newTestModel(t, ed, newFakeTree(3), 80, 24)
	rt := newFakeRuntime(t, "x.cmd")
	rt.wants["widget_event"] = true
	pe := &fakePluginEditor{active: 1, bufs: []pluginapi.BufferInfo{{ID: 1, Path: "/w/a.txt", Name: "a.txt"}}}
	m.plug = newPluginState(m, RootPorts{Plugins: rt, PluginEditor: pe})
	useHost(rt, m)
	rt.onRun = func(string, map[string]any) {
		if err := rt.host.MountWidgetPanel("p", 7, widgetMountFixture()); err != nil {
			t.Errorf("mount: %v", err)
		}
	}
	return m, rt
}

// widgetEvents returns the widget_event payloads the runtime was handed.
func widgetEvents(rt *fakeRuntime) []map[string]any {
	var out []map[string]any
	for _, e := range rt.emitted {
		if e.Name == "widget_event" {
			out = append(out, e.Data)
		}
	}
	return out
}

// lastWidgetEvent returns the last widget_event of the kind, nil when none.
func lastWidgetEvent(rt *fakeRuntime, kind string) map[string]any {
	for i := len(rt.emitted) - 1; i >= 0; i-- {
		e := rt.emitted[i]
		if e.Name == "widget_event" && e.Data["event"] == kind {
			return e.Data
		}
	}
	return nil
}

func TestWidgetPanelMountPaintsAndTakesKeys(t *testing.T) {
	t.Parallel()
	m, rt := widgetPanelModel(t)
	m.Update(palette.RunCommandMsg{Action: "x.cmd"})
	mp := m.plug.panels.get(7)
	if mp == nil {
		t.Fatalf("panel not mounted: %+v", m.plug.panels)
	}
	if m.plug.panels.focus != 7 {
		t.Fatalf("focus = %d, want the panel", m.plug.panels.focus)
	}
	if !mp.panel.Focused() || mp.panel.FocusedKey() != "ok" {
		t.Fatalf("panel focus = %v %q", mp.panel.Focused(), mp.panel.FocusedKey())
	}
	g := m.paint()
	if mp.rect.W <= 0 || mp.rect.H <= 0 {
		t.Fatalf("panel rect = %+v", mp.rect)
	}
	titleRow := g.Plain()[mp.rect.Y]
	if !strings.Contains(titleRow, "Box") {
		t.Errorf("title row %q does not show the title", titleRow)
	}
	body := strings.Join(g.Plain()[mp.rect.Y+1:mp.rect.Y+mp.rect.H], "\n")
	if !strings.Contains(body, "Files") || !strings.Contains(body, "[OK]") || !strings.Contains(body, "[x] Wrap") {
		t.Errorf("panel body does not show the widgets:\n%s", body)
	}

	// Tab moves the focus ring to the toggle, and the plugin hears it.
	m.Update(special(tea.KeyTab))
	if got := mp.panel.FocusedKey(); got != "wrap" {
		t.Fatalf("focus after tab = %q, want wrap", got)
	}
	if ev := lastWidgetEvent(rt, "focus"); ev == nil || ev["widget"] != "wrap" {
		t.Fatalf("focus event = %+v", ev)
	}

	// Enter flips the toggle.
	m.Update(special(tea.KeyEnter))
	if ev := lastWidgetEvent(rt, "toggle"); ev == nil || ev["widget"] != "wrap" || ev["checked"] != false {
		t.Fatalf("toggle event = %+v, want wrap false", ev)
	}
}

func TestWidgetPanelMouseAndEscape(t *testing.T) {
	t.Parallel()
	m, rt := widgetPanelModel(t)
	m.Update(palette.RunCommandMsg{Action: "x.cmd"})
	mp := m.plug.panels.get(7)
	if mp == nil {
		t.Fatal("panel not mounted")
	}
	m.paint()
	// A click on the toggle row flips it and reports the click.
	tx, ty := mp.rect.X+1+space.PadX, mp.rect.Y+1+2
	m.Update(click(tx, ty, tea.MouseLeft))
	if ev := lastWidgetEvent(rt, "mouse_click"); ev == nil || ev["x"] != tx || ev["y"] != ty {
		t.Fatalf("mouse_click = %+v", ev)
	}
	if ev := lastWidgetEvent(rt, "toggle"); ev == nil || ev["checked"] != false {
		t.Fatalf("toggle after the click = %+v", ev)
	}
	// Escape closes the closable panel: a cancel event, then it is gone.
	m.Update(special(tea.KeyEscape))
	if ev := lastWidgetEvent(rt, "cancel"); ev == nil {
		t.Fatalf("no cancel event: %+v", widgetEvents(rt))
	}
	if m.plug.panels.get(7) != nil {
		t.Fatal("the panel survived Escape")
	}
	if m.plug.panels.focus != 0 {
		t.Fatalf("focus = %d after the close, want none", m.plug.panels.focus)
	}
}

func TestWidgetPanelSidebarSection(t *testing.T) {
	t.Parallel()
	ed := newFakeEditor("hello")
	m := newTestModel(t, ed, newFakeTree(3), 80, 24)
	rt := newFakeRuntime(t, "x.cmd")
	rt.wants["widget_event"] = true
	m.plug = newPluginState(m, RootPorts{Plugins: rt, PluginEditor: &fakePluginEditor{active: 1, bufs: []pluginapi.BufferInfo{{ID: 1}}}})
	useHost(rt, m)
	rt.onRun = func(string, map[string]any) {
		err := rt.host.MountWidgetPanel("p", 9, pluginapi.WidgetMount{
			Panel: pluginapi.WidgetPanel{Root: pluginapi.Widget{Kind: pluginapi.WidgetLabel, Text: "Outline"}},
			Title: "Outline", Placement: pluginapi.PlacementSidebar, Rows: 5, Closable: true,
		})
		if err != nil {
			t.Errorf("mount sidebar: %v", err)
		}
	}
	m.Update(palette.RunCommandMsg{Action: "x.cmd"})
	if !m.explorerOpen {
		t.Fatal("mounting a section did not show the sidebar")
	}
	l := m.layout()
	var sec sidebar.Section
	found := false
	for _, s := range l.Sections {
		if s.Kind == sidebar.KindPlugin {
			sec, found = s, true
		}
	}
	if !found {
		t.Fatalf("no plugin section in %+v", l.Sections)
	}
	if sec.Title != "Outline" || sec.Rows() != 5 {
		t.Fatalf("section = %+v, want Outline with 5 rows", sec)
	}
	g := m.paint()
	if row := g.Plain()[sec.HeaderY]; !strings.Contains(row, "Outline") {
		t.Errorf("header %q does not show the title", row)
	}
	// A click (press + release, no motion) on the header folds the section.
	m.Update(click(l.ExX+3, sec.HeaderY, tea.MouseLeft))
	m.Update(tea.MouseReleaseMsg(tea.Mouse{X: l.ExX + 3, Y: sec.HeaderY, Button: tea.MouseLeft}))
	l = m.layout()
	for _, s := range l.Sections {
		if s.Kind == sidebar.KindPlugin && !s.Collapsed {
			t.Fatalf("the section did not fold: %+v", s)
		}
	}
}

func TestWidgetPanelMountErrors(t *testing.T) {
	t.Parallel()
	ed := newFakeEditor("hello")
	m := newTestModel(t, ed, newFakeTree(3), 80, 24)
	rt := newFakeRuntime(t)
	m.plug = newPluginState(m, RootPorts{Plugins: rt, PluginEditor: &fakePluginEditor{active: 1, bufs: []pluginapi.BufferInfo{{ID: 1}}}})
	useHost(rt, m)
	h := m.plug.host
	bad := pluginapi.WidgetMount{Panel: pluginapi.WidgetPanel{Root: pluginapi.Widget{Kind: "nope"}}, Placement: pluginapi.PlacementFloat}
	if err := h.MountWidgetPanel("p", 1, bad); err == nil {
		t.Fatal("an unknown kind mounted")
	}
	if err := h.MountWidgetPanel("p", 2, pluginapi.WidgetMount{Panel: pluginapi.WidgetPanel{Root: pluginapi.Widget{Kind: pluginapi.WidgetLabel}}, Placement: "nowhere"}); err == nil {
		t.Fatal("an unknown placement mounted")
	}
	if err := h.MountWidgetPanel("p", 3, pluginapi.WidgetMount{Panel: pluginapi.WidgetPanel{Root: pluginapi.Widget{Kind: pluginapi.WidgetLabel}}, Placement: pluginapi.PlacementBuffer, Buffer: 99}); err == nil {
		t.Fatal("a buffer panel of a buffer the plugin does not own mounted")
	}
	if err := h.UpdateWidgetPanel("q", 1, pluginapi.WidgetPanel{}); err == nil {
		t.Fatal("an update of an unmounted panel succeeded")
	}
}

// TestRemountPanelChecksOwnerAndAppliesChrome checks that mounting an id again is the
// owner's replace: another plugin is refused and the panel stays, and the owner's new
// title and closable flag reach the live panel.
func TestRemountPanelChecksOwnerAndAppliesChrome(t *testing.T) {
	t.Parallel()
	m, _ := widgetPanelModel(t)
	h := m.plug.host
	if err := h.MountWidgetPanel("p", 7, widgetMountFixture()); err != nil {
		t.Fatal(err)
	}
	intruder := widgetMountFixture()
	intruder.Title = "Hijacked"
	if err := h.MountWidgetPanel("other", 7, intruder); err == nil {
		t.Fatal("another plugin replaced the panel")
	}
	mp := m.plug.panels.get(7)
	if mp.owner != "p" || mp.panel.Title() != "Box" {
		t.Fatalf("owner %q title %q after the refused replace, want p and Box", mp.owner, mp.panel.Title())
	}
	next := widgetMountFixture()
	next.Title, next.Closable = "Renamed", false
	if err := h.MountWidgetPanel("p", 7, next); err != nil {
		t.Fatal(err)
	}
	if mp.panel.Title() != "Renamed" || mp.panel.Closable() {
		t.Fatalf("title %q closable %v after the replace, want Renamed and false", mp.panel.Title(), mp.panel.Closable())
	}
}
