package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	commands "github.com/tbxark/gvim/commands"
	"github.com/tbxark/gvim/internal/tui/palette"
)

// dirtyEd is an lspEd whose tabs report the modified flag and a selection.
type dirtyEd struct {
	*lspEd
	modified  bool
	selection bool
}

func (e *dirtyEd) Tabs() []TabInfo {
	tabs := e.lspEd.Tabs()
	for i := range tabs {
		tabs[i].Modified = e.modified
	}
	return tabs
}

func (e *dirtyEd) Active() Document {
	d := e.lspEd.Active()
	if d == nil {
		return nil
	}
	return dirtyDoc{Document: d, modified: e.modified, selection: e.selection}
}

type dirtyDoc struct {
	Document
	modified, selection bool
}

func (d dirtyDoc) Modified() bool { return d.modified }
func (d dirtyDoc) HasPath() bool  { return true }
func (d dirtyDoc) Selection() (a, h buffer.Pos, ok bool) {
	return buffer.Pos{}, buffer.Pos{Col: 1}, d.selection
}

func TestPaletteStateFollowsTheEditor(t *testing.T) {
	tests := []struct {
		name      string
		modified  bool
		selection bool
		lsp       bool
		want      commands.State
	}{
		{"clean", false, false, false, commands.State{TextBuffer: true, Editable: true, HasPath: true}},
		{"modified", true, false, false, commands.State{TextBuffer: true, Editable: true, HasPath: true, Modified: true, AnyModified: true}},
		{"selection", false, true, false, commands.State{TextBuffer: true, Editable: true, HasPath: true, SelectionActive: true}},
		{"lsp", false, false, true, commands.State{TextBuffer: true, Editable: true, HasPath: true, HasLSPConfig: true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var port *fakeLSP
			if tc.lsp {
				port = newFakeLSP("go")
			}
			lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
			lt.m.ed = &dirtyEd{lspEd: lt.ed, modified: tc.modified, selection: tc.selection}
			if got := lt.m.paletteState(); got != tc.want {
				t.Errorf("paletteState() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestPaletteSaveFileRunsOnADirtyBuffer(t *testing.T) {
	lt := newLSPTest(t, nil, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	lt.m.ed = &dirtyEd{lspEd: lt.ed, modified: true}
	lt.m.openPalette(">save file")

	cmd := press(lt.m, special(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("Enter on Save File of a dirty buffer started no command")
	}
	run, ok := cmd().(palette.RunCommandMsg)
	if !ok || run.Action != "save" {
		t.Fatalf("Enter ran %#v, want RunCommandMsg{Action: save}", run)
	}
}

// TestPaletteTabAcceptsSuggestionWithoutRunningTheCommand drives Tab through the root:
// it must reach the palette overlay (not the editor), leave the palette open and run no
// command; Enter still runs the selected row.
func TestPaletteTabAcceptsSuggestionWithoutRunningTheCommand(t *testing.T) {
	lt := newLSPTest(t, nil, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	lt.m.ed = &dirtyEd{lspEd: lt.ed, modified: true}
	lt.m.openPalette(">save file")
	before := len(lt.m.overlays)
	if _, ok := lt.m.overlays[before-1].(*palette.Palette); !ok {
		t.Fatalf("the top overlay is %T, not the palette", lt.m.overlays[before-1])
	}
	cmd := press(lt.m, special(tea.KeyTab))
	for _, msg := range deliver(cmd) {
		if run, ok := msg.(palette.RunCommandMsg); ok {
			t.Fatalf("Tab ran %q; only Enter should run the selected command", run.Action)
		}
	}
	if len(lt.m.overlays) != before {
		t.Fatalf("Tab closed/changed the palette overlay: %d -> %d", before, len(lt.m.overlays))
	}
	if _, ok := lt.m.overlays[len(lt.m.overlays)-1].(*palette.Palette); !ok {
		t.Fatal("the palette is no longer the top overlay after Tab")
	}

	cmd = press(lt.m, special(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("Enter on the accepted Save File suggestion started no command")
	}
	run, ok := cmd().(palette.RunCommandMsg)
	if !ok || run.Action != "save" {
		t.Fatalf("Enter ran %#v, want RunCommandMsg{Action: save}", cmd())
	}
}
