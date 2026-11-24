package tui

import (
	"testing"

	"github.com/tbxark/gvim/internal/tui/frame"
)

func TestHiddenTabBarGivesItsRowToTheBody(t *testing.T) {
	tests := []struct {
		name     string
		in       frame.Input
		bodyTop  int
		labelRow int // -1 when the menu bar is hidden
	}{
		{"menu bar shown", frame.Input{HideTabBar: true, Lines: 4}, 2, 0},
		{"menu bar and tab bar hidden", frame.Input{HideTabBar: true, HideMenuBar: true, Lines: 4}, 1, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := layoutFor(80, 24, tt.in)
			if l.BodyTop != tt.bodyTop {
				t.Fatalf("bodyTop = %d, want %d", l.BodyTop, tt.bodyTop)
			}
			if got := l.Hit(l.TextX, tt.bodyTop); got.Kind != frame.HitEditorText {
				t.Errorf("first body row hits %v, want the editor text", got.Kind)
			}
			if l.MenuLabelY != tt.labelRow {
				t.Fatalf("label row = %d, want %d", l.MenuLabelY, tt.labelRow)
			}
			if tt.labelRow >= 0 {
				if got := l.Hit(1, tt.labelRow); got.Kind != frame.HitNone {
					t.Errorf("label row hits %v, want none (the menu bar owns its hit map)", got.Kind)
				}
			}
		})
	}
}

func TestShownTabBarKeepsItsRow(t *testing.T) {
	l := layoutFor(80, 24, frame.Input{Lines: 4, Tabs: []TabInfo{{Name: "a.txt", Active: true}}})
	if l.BodyTop != 3 || l.StripY != 2 {
		t.Fatalf("bodyTop %d stripY %d, want 3 and 2 under the menu band", l.BodyTop, l.StripY)
	}
	if got := l.Hit(1, l.StripY); got.Kind != frame.HitTab {
		t.Errorf("row %d at the first tab hits %v, want the tab", l.StripY, got.Kind)
	}
}

func TestHiddenVerticalScrollbarClearsTheScrollbarColumn(t *testing.T) {
	shown := layoutFor(80, 24, frame.Input{Lines: 200, ExplorerRows: 0})
	hidden := layoutFor(80, 24, frame.Input{Lines: 200, HideVScroll: true})
	if !shown.Scrollbar {
		t.Fatal("setup: a long document shows no scrollbar")
	}
	if hidden.Scrollbar {
		t.Fatal("the frame still reserves the scrollbar column")
	}
	for _, p := range hidden.Panes {
		if p.Scrollbar {
			t.Fatal("a pane still has the scrollbar with it hidden")
		}
	}
	x := hidden.EditorX + hidden.Panes[0].W - 1
	if got := hidden.Hit(x, hidden.BodyTop); got.Kind == frame.HitScrollbarTrack {
		t.Errorf("the last column hits the scrollbar with it hidden")
	}
}
