package tui

import "testing"

func TestHiddenTabBarGivesItsRowToTheBody(t *testing.T) {
	tests := []struct {
		name        string
		in          layoutInput
		bodyTop     int
		menuRowHits bool // row 0 still hits the menu bar
	}{
		{"menu bar shown", layoutInput{HideTabBar: true, Lines: 4}, 1, true},
		{"menu bar and tab bar hidden", layoutInput{HideTabBar: true, HideMenuBar: true, Lines: 4}, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := layoutFor(80, 24, tt.in)
			if l.BodyTop != tt.bodyTop {
				t.Fatalf("bodyTop = %d, want %d", l.BodyTop, tt.bodyTop)
			}
			if got := l.Hit(l.TextX, tt.bodyTop); got.Kind != hitEditorText {
				t.Errorf("first body row hits %v, want the editor text", got.Kind)
			}
			if got := l.Hit(1, 0); (got.Kind == hitMenuLabel) != tt.menuRowHits {
				t.Errorf("row 0 hits %v, menu label expected %v", got.Kind, tt.menuRowHits)
			}
		})
	}
}

func TestShownTabBarKeepsItsRow(t *testing.T) {
	l := layoutFor(80, 24, layoutInput{Lines: 4, Tabs: []TabInfo{{Name: "a.txt", Active: true}}})
	if l.BodyTop != 2 || l.StripY != 1 {
		t.Fatalf("bodyTop %d stripY %d, want 2 and 1 with the tab bar shown", l.BodyTop, l.StripY)
	}
	if got := l.Hit(1, 1); got.Kind != hitTab {
		t.Errorf("row 1 at the first tab hits %v, want the tab", got.Kind)
	}
}

func TestPromptLineSitsUnderTheStatusRow(t *testing.T) {
	shown := layoutFor(80, 24, layoutInput{Lines: 4})
	withPrompt := layoutFor(80, 24, layoutInput{Lines: 4, ShowPromptLine: true})
	if shown.StatusY != 23 || shown.BodyBottom != 22 {
		t.Fatalf("without the prompt line: status %d body bottom %d, want 23 and 22", shown.StatusY, shown.BodyBottom)
	}
	if withPrompt.StatusY != 22 || withPrompt.BodyBottom != 21 {
		t.Fatalf("with the prompt line: status %d body bottom %d, want 22 and 21", withPrompt.StatusY, withPrompt.BodyBottom)
	}
	if !withPrompt.PromptOn || withPrompt.PromptY != 23 {
		t.Fatalf("prompt row %d on=%v, want row 23 on", withPrompt.PromptY, withPrompt.PromptOn)
	}
	if got := withPrompt.Hit(10, 23); got.Kind != hitNone {
		t.Errorf("the prompt row hits %v, want nothing", got.Kind)
	}
}

func TestPromptLineStaysAboveTheFindBar(t *testing.T) {
	l := layoutFor(80, 24, layoutInput{Lines: 4, ShowPromptLine: true, FindRows: 2})
	if l.PromptY != 21 || l.StatusY != 20 {
		t.Fatalf("prompt %d status %d, want 21 and 20 above a two-row find bar", l.PromptY, l.StatusY)
	}
}

func TestHiddenVerticalScrollbarClearsTheScrollbarColumn(t *testing.T) {
	shown := layoutFor(80, 24, layoutInput{Lines: 200, ExplorerRows: 0})
	hidden := layoutFor(80, 24, layoutInput{Lines: 200, HideVScroll: true})
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
	if got := hidden.Hit(x, hidden.BodyTop); got.Kind == hitScrollbarTrack {
		t.Errorf("the last column hits the scrollbar with it hidden")
	}
}
