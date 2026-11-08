package tui

import "testing"

func TestHiddenTabBarGivesItsRowToTheBody(t *testing.T) {
	tests := []struct {
		name        string
		in          layoutInput
		bodyTop     int
		menuRowHits bool // row 0 still hits the menu bar
	}{
		{"menu bar shown", layoutInput{hideTabBar: true, lines: 4}, 1, true},
		{"menu bar and tab bar hidden", layoutInput{hideTabBar: true, hideMenuBar: true, lines: 4}, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := layoutFor(80, 24, tt.in)
			if l.bodyTop != tt.bodyTop {
				t.Fatalf("bodyTop = %d, want %d", l.bodyTop, tt.bodyTop)
			}
			if got := l.hit(l.textX, tt.bodyTop); got.Kind != hitEditorText {
				t.Errorf("first body row hits %v, want the editor text", got.Kind)
			}
			if got := l.hit(1, 0); (got.Kind == hitMenuLabel) != tt.menuRowHits {
				t.Errorf("row 0 hits %v, menu label expected %v", got.Kind, tt.menuRowHits)
			}
		})
	}
}

func TestShownTabBarKeepsItsRow(t *testing.T) {
	l := layoutFor(80, 24, layoutInput{lines: 4, tabs: []TabInfo{{Name: "a.txt", Active: true}}})
	if l.bodyTop != 2 || l.stripY != 1 {
		t.Fatalf("bodyTop %d stripY %d, want 2 and 1 with the tab bar shown", l.bodyTop, l.stripY)
	}
	if got := l.hit(1, 1); got.Kind != hitTab {
		t.Errorf("row 1 at the first tab hits %v, want the tab", got.Kind)
	}
}

func TestPromptLineSitsUnderTheStatusRow(t *testing.T) {
	shown := layoutFor(80, 24, layoutInput{lines: 4})
	withPrompt := layoutFor(80, 24, layoutInput{lines: 4, showPromptLine: true})
	if shown.statusY != 23 || shown.bodyBottom != 22 {
		t.Fatalf("without the prompt line: status %d body bottom %d, want 23 and 22", shown.statusY, shown.bodyBottom)
	}
	if withPrompt.statusY != 22 || withPrompt.bodyBottom != 21 {
		t.Fatalf("with the prompt line: status %d body bottom %d, want 22 and 21", withPrompt.statusY, withPrompt.bodyBottom)
	}
	if !withPrompt.promptOn || withPrompt.promptY != 23 {
		t.Fatalf("prompt row %d on=%v, want row 23 on", withPrompt.promptY, withPrompt.promptOn)
	}
	if got := withPrompt.hit(10, 23); got.Kind != hitNone {
		t.Errorf("the prompt row hits %v, want nothing", got.Kind)
	}
}

func TestPromptLineStaysAboveTheFindBar(t *testing.T) {
	l := layoutFor(80, 24, layoutInput{lines: 4, showPromptLine: true, findRows: 2})
	if l.promptY != 21 || l.statusY != 20 {
		t.Fatalf("prompt %d status %d, want 21 and 20 above a two-row find bar", l.promptY, l.statusY)
	}
}

func TestHiddenVerticalScrollbarClearsTheScrollbarColumn(t *testing.T) {
	shown := layoutFor(80, 24, layoutInput{lines: 200, explorerRows: 0})
	hidden := layoutFor(80, 24, layoutInput{lines: 200, hideVScroll: true})
	if !shown.scrollbar {
		t.Fatal("setup: a long document shows no scrollbar")
	}
	if hidden.scrollbar {
		t.Fatal("the frame still reserves the scrollbar column")
	}
	for _, p := range hidden.panes {
		if p.scrollbar {
			t.Fatal("a pane still has the scrollbar with it hidden")
		}
	}
	x := hidden.editorX + hidden.panes[0].W - 1
	if got := hidden.hit(x, hidden.bodyTop); got.Kind == hitScrollbarTrack {
		t.Errorf("the last column hits the scrollbar with it hidden")
	}
}
