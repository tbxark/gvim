package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/palette"
	"github.com/tbxark/gvim/pluginapi"
)

// interactiveMountFixture is a panel with a focused text field and a second control, for the
// keyboard/event routing tests.
func interactiveMountFixture() pluginapi.WidgetMount {
	return pluginapi.WidgetMount{
		Panel: pluginapi.WidgetPanel{
			Root: pluginapi.Widget{Kind: pluginapi.WidgetCol, Children: []pluginapi.Widget{
				{Kind: pluginapi.WidgetLabel, Text: "Name"},
				{Kind: pluginapi.WidgetText, Key: "name", Placeholder: "type"},
				{Kind: pluginapi.WidgetDropdown, Key: "kind", Options: []string{"a", "b"}},
			}},
			FocusKey: "name",
		},
		Title: "Edit", Placement: pluginapi.PlacementFloat, Focus: true, Closable: true,
	}
}

func interactivePanelModel(t *testing.T) (*Model, *fakeRuntime) {
	t.Helper()
	ed := newFakeEditor("hello")
	m := newTestModel(t, ed, newFakeTree(3), 80, 24)
	rt := newFakeRuntime(t, "x.cmd")
	rt.wants["widget_event"] = true
	m.plug = newPluginState(m, RootPorts{Plugins: rt, PluginEditor: &fakePluginEditor{active: 1, bufs: []pluginapi.BufferInfo{{ID: 1}}}})
	useHost(rt, m)
	rt.onRun = func(string, map[string]any) {
		if err := rt.host.MountWidgetPanel("p", 11, interactiveMountFixture()); err != nil {
			t.Errorf("mount: %v", err)
		}
	}
	return m, rt
}

func widgetEventData(rt *fakeRuntime, kind string) map[string]any {
	for i := len(rt.emitted) - 1; i >= 0; i-- {
		e := rt.emitted[i]
		if e.Name == "widget_event" && e.Data["event"] == kind {
			return e.Data
		}
	}
	return nil
}

func TestInteractivePanelTypingAndFocus(t *testing.T) {
	t.Parallel()
	m, rt := interactivePanelModel(t)
	m.Update(palette.RunCommandMsg{Action: "x.cmd"})
	mp := m.plug.panels.get(11)
	if mp == nil || mp.panel.FocusedKey() != "name" {
		t.Fatalf("panel = %+v", mp)
	}
	// A printable key inserts into the focused text field and reports a change with the
	// caret and the panel focus.
	m.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	m.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	ev := widgetEventData(rt, "change")
	if ev == nil || ev["value"] != "hi" || ev["cursor_byte"] != 2 || ev["focus_key"] != "name" {
		t.Fatalf("change event = %+v", ev)
	}
	// Tab moves the focus ring to the dropdown.
	m.Update(special(tea.KeyTab))
	if got := mp.panel.FocusedKey(); got != "kind" {
		t.Fatalf("focus after tab = %q", got)
	}
	if ev := widgetEventData(rt, "focus"); ev == nil || ev["widget"] != "kind" {
		t.Fatalf("focus event = %+v", ev)
	}
	// The plugin's widget_command types via the host call too (a mode would use it).
	if err := m.plug.host.WidgetCommand(11, pluginapi.WidgetAction{Kind: "focusAdvance", Delta: -1}); err != nil {
		t.Fatal(err)
	}
	m.pluginPump()
	if got := mp.panel.FocusedKey(); got != "name" {
		t.Fatalf("focus after widget_command = %q", got)
	}
	if err := m.plug.host.WidgetCommand(11, pluginapi.WidgetAction{Kind: "textInputChar", Text: "!"}); err != nil {
		t.Fatal(err)
	}
	m.pluginPump()
	ev = widgetEventData(rt, "change")
	if ev == nil || ev["value"] != "hi!" {
		t.Fatalf("text_input change = %+v", ev)
	}
}

func TestInteractivePanelMutations(t *testing.T) {
	t.Parallel()
	m, rt := interactivePanelModel(t)
	m.Update(palette.RunCommandMsg{Action: "x.cmd"})
	h := m.plug.host
	if err := h.WidgetMutate(11, pluginapi.WidgetMutation{Kind: "setValue", WidgetKey: "name", Value: "abc", CursorByte: 1, HasCursor: true}); err != nil {
		t.Fatal(err)
	}
	if err := h.WidgetMutate(11, pluginapi.WidgetMutation{Kind: "setDropdown", WidgetKey: "kind", Index: 1}); err != nil {
		t.Fatal(err)
	}
	mp := m.plug.panels.get(11)
	if got := mp.panel.FocusedKey(); got != "name" {
		t.Fatalf("focus shifted to %q", got)
	}
	// Unknown mutation kind is refused.
	if err := h.WidgetMutate(11, pluginapi.WidgetMutation{Kind: "nope"}); err == nil {
		t.Fatal("an unknown mutation kind succeeded")
	}
	m.pluginPump()
	_ = rt
}

func TestTextValueSemantics(t *testing.T) {
	t.Parallel()
	m, _ := interactivePanelModel(t)
	m.Update(palette.RunCommandMsg{Action: "x.cmd"})
	mp := m.plug.panels.get(11)
	if mp == nil {
		t.Fatal("no panel")
	}
	// A read-only text block is still a focusable non-editing field.
	readOnly := pluginapi.WidgetMount{Panel: pluginapi.WidgetPanel{
		Root: pluginapi.Widget{Kind: pluginapi.WidgetText, Key: "out", Text: "fixed", ReadOnly: true},
	}, Placement: pluginapi.PlacementFloat, Focus: true}
	if err := m.plug.host.MountWidgetPanel("p", 12, readOnly); err != nil {
		t.Fatal(err)
	}
	m.pluginPump()
	if got := m.plug.panels.get(12).panel.FocusedKey(); got != "out" {
		t.Fatalf("read-only field focus = %q", got)
	}
}
