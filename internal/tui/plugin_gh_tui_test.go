package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/palette"
	"github.com/tbxark/gvim/pluginapi"
)

// actionPopupModel mounts an action popup from a plugin command and returns the model and
// runtime.
func actionPopupModel(t *testing.T) (*Model, *fakeRuntime) {
	t.Helper()
	ed := newFakeEditor("hello")
	m := newTestModel(t, ed, newFakeTree(3), 80, 24)
	rt := newFakeRuntime(t, "x.cmd")
	rt.wants["action_popup_result"] = true
	m.plug = newPluginState(m, RootPorts{Plugins: rt, PluginEditor: &fakePluginEditor{active: 1, bufs: []pluginapi.BufferInfo{{ID: 1}}}})
	useHost(rt, m)
	rt.onRun = func(string, map[string]any) {
		err := rt.host.ShowActionPopup(9, 3, pluginapi.ActionPopupSpec{
			Title: "Update", Message: "A new version",
			Actions: []pluginapi.ActionPopupAction{{ID: "install", Label: "Install"}, {ID: "later", Label: "Later"}},
		})
		if err != nil {
			t.Errorf("action popup: %v", err)
		}
	}
	return m, rt
}

func TestActionPopupOverlay(t *testing.T) {
	t.Parallel()
	m, rt := actionPopupModel(t)
	m.Update(palette.RunCommandMsg{Action: "x.cmd"})
	if len(m.overlays) == 0 {
		t.Fatalf("no overlay: %+v", m.overlays)
	}
	if _, ok := m.overlays[len(m.overlays)-1].(*pluginActionPopupOverlay); !ok {
		t.Fatalf("top overlay is %T, want the action popup", m.overlays[len(m.overlays)-1])
	}
	// Enter chooses the first action.
	_, cmd := m.Update(special(tea.KeyEnter))
	feedAll(t, m, cmd, 10)
	if len(rt.resolved) != 1 || rt.resolved[0].Values[0] != "install" {
		t.Fatalf("resolved = %+v, want install", rt.resolved)
	}
	var result map[string]any
	for _, e := range rt.emitted {
		if e.Name == "action_popup_result" {
			result = e.Data
		}
	}
	if result == nil || result["action"] != "install" {
		t.Fatalf("action_popup_result = %+v", result)
	}
	// A second popup closes with Escape and resolves nil.
	m.Update(palette.RunCommandMsg{Action: "x.cmd"})
	_, cmd = m.Update(special(tea.KeyEscape))
	feedAll(t, m, cmd, 10)
	if len(rt.resolved) != 2 || rt.resolved[1].Values[0] != nil {
		t.Fatalf("resolved = %+v, want nil", rt.resolved)
	}
}

// TestActionPopupDismissCarriesThePopupID checks that a popup closed without a choice
// reports its own id in action_popup_result, as a chosen action does.
func TestActionPopupDismissCarriesThePopupID(t *testing.T) {
	t.Parallel()
	m, rt := actionPopupModel(t)
	m.Update(palette.RunCommandMsg{Action: "x.cmd"})
	_, cmd := m.Update(special(tea.KeyEscape))
	feedAll(t, m, cmd, 10)
	var result map[string]any
	for _, e := range rt.emitted {
		if e.Name == "action_popup_result" {
			result = e.Data
		}
	}
	if result == nil || result["popup"] != uint64(3) || result["action"] != nil {
		t.Fatalf("action_popup_result = %+v, want popup 3 and no action", result)
	}
}

// promptModel opens a plugin input prompt from a command.
func promptModel(t *testing.T) (*Model, *fakeRuntime) {
	t.Helper()
	ed := newFakeEditor("hello")
	m := newTestModel(t, ed, newFakeTree(3), 80, 24)
	rt := newFakeRuntime(t, "x.cmd")
	m.plug = newPluginState(m, RootPorts{Plugins: rt, PluginEditor: &fakePluginEditor{active: 1, bufs: []pluginapi.BufferInfo{{ID: 1}}}})
	useHost(rt, m)
	rt.onRun = func(string, map[string]any) {
		if err := rt.host.Input(4, pluginapi.InputSpec{Prompt: "File:"}); err != nil {
			t.Errorf("input: %v", err)
		}
	}
	return m, rt
}

func TestPromptSuggestionsHost(t *testing.T) {
	t.Parallel()
	m, rt := promptModel(t)
	m.Update(palette.RunCommandMsg{Action: "x.cmd"})
	if m.plugTopLine() == nil {
		t.Fatalf("no prompt overlay: %+v", m.overlays)
	}
	// Clearing the list on the open prompt succeeds.
	if err := m.plug.host.SetPromptSuggestions(nil, -1); err != nil {
		t.Fatalf("clear suggestions: %v", err)
	}
	err := m.plug.host.SetPromptSuggestions([]pluginapi.PromptSuggestion{
		{Text: "one"}, {Text: "two", Value: "2"},
	}, 1)
	if err != nil {
		t.Fatalf("set suggestions: %v", err)
	}
	// Tab cycles to the next suggestion and fills it in; Enter submits the field.
	m.Update(special(tea.KeyTab))
	_, enterCmd := m.Update(special(tea.KeyEnter))
	feedAll(t, m, enterCmd, 10)
	if len(rt.resolved) != 1 || rt.resolved[0].Values[0] != "one" {
		t.Fatalf("resolved = %+v, want the first suggestion", rt.resolved)
	}
	// With no prompt open the call is not_found.
	m2, _ := promptModel(t)
	if err := m2.plug.host.SetPromptSuggestions(nil, -1); err == nil || pluginapi.CodeOf(err) != pluginapi.CodeNotFound {
		t.Fatalf("no-prompt error = %v, want not_found", err)
	}
}
