package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/list"
	"github.com/tbxark/gvim/theme"
)

const longWarning = "/private/tmp/gvim-dogfood/plugauthor/work/proj/.gvim/config.json: plugins.setdemo.enabled: project config cannot enable a plugin"

func warningModel(t *testing.T, w, h int) *Model {
	t.Helper()
	m := New(Deps{Editor: newFakeEditor("x"), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, Notice: longWarning})
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	rootPaletteRun(m, "show_warnings")
	return m
}

func TestWarningsListUsesTheWidthOfAWideTerminal(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ w, wantAtLeast int }{{80, 76}, {200, 150}} {
		m := warningModel(t, tt.w, 30)
		o, ok := m.overlays[len(m.overlays)-1].(*list.View)
		if !ok {
			t.Fatalf("width %d: no list overlay", tt.w)
		}
		frame := m.paint()
		_ = frame
		if o.Box.W < tt.wantAtLeast {
			t.Errorf("width %d: the warnings box is %d cells wide, want at least %d", tt.w, o.Box.W, tt.wantAtLeast)
		}
	}
	// At 200 columns the whole message fits, so no row is cut.
	m := warningModel(t, 200, 30)
	m.paint()
	if rows := screenRows(m); !strings.Contains(strings.Join(rows, "\n"), longWarning) {
		t.Errorf("the whole warning is not on screen at 200 columns:\n%s", strings.Join(rows, "\n"))
	}
}

func TestLongWarningRowsKeepTheirEndWhenCut(t *testing.T) {
	t.Parallel()
	m := warningModel(t, 80, 30)
	m.paint()
	joined := strings.Join(screenRows(m), "\n")
	if !strings.Contains(joined, "cannot enable a plugin") {
		t.Errorf("a cut warning lost its reason:\n%s", joined)
	}
	// The path gives up its folders first, so the base name stays, and the cut is marked.
	if !strings.Contains(joined, "config.json") || !strings.Contains(joined, "…") {
		t.Errorf("a cut warning must keep the file name and mark the cut:\n%s", joined)
	}
}

func TestEnterOnAWarningShowsItsFullText(t *testing.T) {
	t.Parallel()
	m := warningModel(t, 80, 30)
	m.paint()
	cmd := press(m, special(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("Enter on a warning row started nothing")
	}
	m.Update(cmd())
	top, ok := m.overlays[len(m.overlays)-1].(*list.TextBox)
	if !ok {
		t.Fatalf("top overlay is %T, want the text box of the warning", m.overlays[len(m.overlays)-1])
	}
	_, lines, _ := top.Content()
	if got := strings.Join(lines, " "); !strings.Contains(got, "project config cannot enable a plugin") || !strings.Contains(got, ".gvim/config.json:") {
		t.Errorf("the box holds %q, want the whole warning", got)
	}
	// Enter closes the box, and the list under it is still there.
	press(m, special(tea.KeyEnter))
	if _, ok := m.overlays[len(m.overlays)-1].(*list.View); !ok {
		t.Errorf("after Enter the top overlay is %T, want the list again", m.overlays[len(m.overlays)-1])
	}
}
