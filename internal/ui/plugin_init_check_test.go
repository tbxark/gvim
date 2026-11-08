package ui

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/pluginapi"
	"github.com/tbxark/gvim/internal/theme"
)

// initTextEditor is a plugin editor port with one buffer whose full text can be read.
type initTextEditor struct {
	*fakePluginEditor
	text string
}

func (e *initTextEditor) FullText(id pluginapi.BufferID) (string, pluginapi.Version, error) {
	return e.text, pluginapi.Version{}, nil
}

// TestInitCheckReadsTheDirtyBuffer: "init: Check init.lua" checks the open buffer of init.lua
// when it has unsaved changes, and the file on disk otherwise.
func TestInitCheckReadsTheDirtyBuffer(t *testing.T) {
	t.Parallel()
	const initPath = "/cfg/init.lua"
	tests := []struct {
		name string
		bufs []pluginapi.BufferInfo
		want pluginapi.Control
	}{
		{"not open", []pluginapi.BufferInfo{{ID: 1, Path: "/w/a.txt"}}, pluginapi.Control{Op: "init_check"}},
		{"open and saved", []pluginapi.BufferInfo{{ID: 1, Path: initPath}}, pluginapi.Control{Op: "init_check"}},
		{"open with changes", []pluginapi.BufferInfo{{ID: 1, Path: initPath, Modified: true}}, pluginapi.Control{Op: "init_check", Text: "local x = = 1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rt := &mgrRuntime{fakeRuntime: newFakeRuntime(t), initFile: initPath}
			ed := newFakeEditor("hello", "world")
			pe := &initTextEditor{fakePluginEditor: &fakePluginEditor{bufs: tt.bufs, active: 1}, text: "local x = = 1"}
			m := New(Deps{Editor: ed, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, Root: RootPorts{Plugins: rt, PluginEditor: pe}})
			m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			useHost(rt.fakeRuntime, m)
			runPaletteAction(m, "init_check")
			if !slices.Equal(rt.controls, []pluginapi.Control{tt.want}) {
				t.Fatalf("controls = %+v, want %+v", rt.controls, tt.want)
			}
		})
	}
}
