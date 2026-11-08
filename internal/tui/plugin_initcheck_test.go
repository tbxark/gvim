package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/pluginapi"
	"github.com/tbxark/gvim/theme"
)

// linesPluginEditor is the plugin editor port with the text of its buffers.
type linesPluginEditor struct {
	*fakePluginEditor
	text map[pluginapi.BufferID][]string
}

func (e *linesPluginEditor) Lines(id pluginapi.BufferID, first, count int) ([]string, error) {
	lines := e.text[id]
	if count < 0 || first+count > len(lines) {
		return lines[first:], nil
	}
	return lines[first : first+count], nil
}

// FullText is the text of the buffer, its lines joined.
func (e *linesPluginEditor) FullText(id pluginapi.BufferID) (string, pluginapi.Version, error) {
	return strings.Join(e.text[id], "\n"), pluginapi.Version{}, nil
}

// init: Check init.lua checks the text of the buffer when init.lua is open, saved or not.
func TestInitCheckSendsTheOpenBuffer(t *testing.T) {
	t.Parallel()
	const path = "/cfg/init.lua"
	newModel := func(bufs ...pluginapi.BufferInfo) (*Model, *mgrRuntime) {
		rt := &mgrRuntime{fakeRuntime: newFakeRuntime(t), initFile: path}
		pe := &linesPluginEditor{
			fakePluginEditor: &fakePluginEditor{bufs: bufs},
			text:             map[pluginapi.BufferID][]string{2: {"local x = = 1", "return"}},
		}
		ed := &mgrEditor{fakeEditor: newFakeEditor("hello")}
		m := New(Deps{Editor: ed, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, Root: RootPorts{Plugins: rt, PluginEditor: pe}})
		m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
		return m, rt
	}
	t.Run("an open init.lua is sent with its text", func(t *testing.T) {
		t.Parallel()
		m, rt := newModel(pluginapi.BufferInfo{ID: 1, Path: "/w/a.txt"}, pluginapi.BufferInfo{ID: 2, Path: path, Modified: true})
		runPaletteAction(m, "init_check")
		want := pluginapi.Control{Op: "init_check", Text: "local x = = 1\nreturn"}
		if !slices.Equal(rt.controls, []pluginapi.Control{want}) {
			t.Fatalf("controls = %+v, want %+v", rt.controls, want)
		}
	})
	t.Run("without it the file on disk is checked", func(t *testing.T) {
		t.Parallel()
		m, rt := newModel(pluginapi.BufferInfo{ID: 1, Path: "/w/a.txt"})
		runPaletteAction(m, "init_check")
		if !slices.Equal(rt.controls, []pluginapi.Control{{Op: "init_check"}}) {
			t.Fatalf("controls = %+v", rt.controls)
		}
	})
}
