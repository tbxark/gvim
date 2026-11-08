package popup

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
)

// items returns n items labelled "i0", "i1", and so on.
func items(n int) []Item {
	out := make([]Item, n)
	for i := range out {
		out[i] = Item{Label: "i" + string(rune('0'+i))}
	}
	return out
}

func TestPlaceBox(t *testing.T) {
	tests := []struct {
		name   string
		anchor Point
		w, h   int
		fw, fh int
		want   rect
	}{
		{"below the anchor", Point{4, 2}, 10, 3, 80, 24, rect{4, 3, 10, 3}},
		{"flips above when there is no room below", Point{4, 22}, 10, 3, 80, 24, rect{4, 19, 10, 3}},
		{"clamps right edge into the frame", Point{75, 5}, 10, 3, 80, 24, rect{69, 6, 10, 3}},
		{"clamps left edge", Point{-3, 5}, 10, 3, 80, 24, rect{1, 6, 10, 3}},
		{"a narrow frame keeps no margin", Point{-3, 5}, 10, 3, 30, 24, rect{0, 6, 10, 3}},
		{"stays off the last row", Point{4, 20}, 10, 3, 80, 24, rect{4, 17, 10, 3}},
		{"clamps top when flipped does not fit", Point{1, 1}, 10, 30, 80, 24, rect{1, 0, 10, 30}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := placeBox(tt.anchor, tt.w, tt.h, tt.fw, tt.fh); got != tt.want {
				t.Fatalf("placeBox = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestBoxWidthFitsTitleAndDetail(t *testing.T) {
	tests := []struct {
		name  string
		title string
		items []Item
		fw    int
		want  int
	}{
		{"title wider than rows", "Remote workspace", []Item{{Label: "a"}}, 80, 22},
		{"label and detail", "T", []Item{{Label: "abc", Detail: "wxyz"}}, 80, 13},
		{"capped at the frame", "T", []Item{{Label: strings.Repeat("x", 50)}}, 20, 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := boxWidth(tt.title, tt.items, tt.fw); got != tt.want {
				t.Fatalf("boxWidth = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestShowOpensClosedPopupAndResetsHighlight(t *testing.T) {
	p := New(Styles{})
	if p.Active() {
		t.Fatal("a new popup is open")
	}
	p.Show(Point{1, 1}, "T", items(3))
	p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	p.Show(Point{1, 1}, "T", items(3))
	if !p.Active() || p.Selected() != 0 {
		t.Fatalf("after Show: active=%v selected=%d, want true and 0", p.Active(), p.Selected())
	}
}

func TestKeysMoveAndChoose(t *testing.T) {
	tests := []struct {
		name     string
		keys     []tea.KeyPressMsg
		wantSel  int
		wantCh   int // index of the chosen row, or -1 when nothing is chosen
		wantOpen bool
	}{
		{"down then enter chooses row 1", []tea.KeyPressMsg{{Code: tea.KeyDown}, {Code: tea.KeyEnter}}, 1, 1, false},
		{"up at the top stays at 0", []tea.KeyPressMsg{{Code: tea.KeyUp}}, 0, -1, true},
		{"down clamps at the last row", []tea.KeyPressMsg{{Code: tea.KeyDown}, {Code: tea.KeyDown}, {Code: tea.KeyDown}, {Code: tea.KeyDown}}, 2, -1, true},
		{"end then enter chooses the last row", []tea.KeyPressMsg{{Code: tea.KeyEnd}, {Code: tea.KeyEnter}}, 2, 2, false},
		{"page down clamps at the last row", []tea.KeyPressMsg{{Code: tea.KeyPgDown}}, 2, -1, true},
		{"other keys are swallowed", []tea.KeyPressMsg{{Code: 'x', Text: "x"}}, 0, -1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(Styles{})
			p.Show(Point{0, 0}, "T", items(3))
			var chosen []tea.Msg
			for _, k := range tt.keys {
				if cmd := p.Update(k); cmd != nil {
					chosen = append(chosen, cmd())
				}
			}
			if p.Active() != tt.wantOpen {
				t.Fatalf("active = %v, want %v", p.Active(), tt.wantOpen)
			}
			if p.Selected() != tt.wantSel {
				t.Fatalf("selected = %d, want %d", p.Selected(), tt.wantSel)
			}
			if tt.wantCh < 0 {
				if len(chosen) != 0 {
					t.Fatalf("emitted %v, want nothing", chosen)
				}
				return
			}
			if len(chosen) != 1 {
				t.Fatalf("emitted %d messages, want 1", len(chosen))
			}
			got, ok := chosen[0].(Chosen)
			if !ok || got.Index != tt.wantCh || got.Item.Label != "i"+string(rune('0'+tt.wantCh)) {
				t.Fatalf("chosen = %#v, want index %d", chosen[0], tt.wantCh)
			}
		})
	}
}

func TestChosenCarriesItemData(t *testing.T) {
	p := New(Styles{})
	p.Show(Point{0, 0}, "T", []Item{{Label: "a", Detail: "d", Data: 42}})
	cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := cmd().(Chosen)
	if got.Item.Data != 42 || got.Item.Detail != "d" {
		t.Fatalf("chosen item = %+v, want the data and detail passed to Show", got.Item)
	}
}

func TestEnterWithNoItemsKeepsPopupOpen(t *testing.T) {
	p := New(Styles{})
	p.Show(Point{0, 0}, "T", nil)
	if cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatalf("emitted a command for an empty popup")
	}
	if !p.Active() {
		t.Fatal("an empty popup closed on Enter")
	}
}

func TestClickChoosesRowAndClickOutsideCloses(t *testing.T) {
	tests := []struct {
		name     string
		x, y     int
		wantCh   int
		wantOpen bool
	}{
		{"click on row 1", 3, 3, 1, false}, // box is at (1,1): title row 1, rows 2..4
		{"click on the title row does nothing", 3, 1, -1, true},
		{"click outside closes", 30, 10, -1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(Styles{})
			p.Show(Point{1, 0}, "Title", items(3))
			g := grid.New(40, 10)
			p.Draw(g)
			var chosen tea.Msg
			if cmd := p.Update(tea.MouseClickMsg(tea.Mouse{X: tt.x, Y: tt.y, Button: tea.MouseLeft})); cmd != nil {
				chosen = cmd()
			}
			if p.Active() != tt.wantOpen {
				t.Fatalf("active = %v, want %v", p.Active(), tt.wantOpen)
			}
			if tt.wantCh < 0 {
				if chosen != nil {
					t.Fatalf("chosen %#v, want nothing", chosen)
				}
				return
			}
			if got, ok := chosen.(Chosen); !ok || got.Index != tt.wantCh {
				t.Fatalf("chosen %#v, want index %d", chosen, tt.wantCh)
			}
		})
	}
}

func TestDrawPaintsTitleRowsAndDetail(t *testing.T) {
	p := New(Styles{})
	p.Show(Point{0, 0}, "Remote", []Item{
		{Label: "Local", Detail: "here"},
		{Label: "Remote workspaces are not supported"},
	})
	g := grid.New(60, 8)
	p.Draw(g)
	rows := g.Plain()
	// The box sits below the anchor row 0, so its title is on row 1.
	if !strings.Contains(rows[1], "Remote") {
		t.Errorf("row 1 = %q, want the title", rows[1])
	}
	if !strings.Contains(rows[2], "Local") || !strings.Contains(rows[2], "here") {
		t.Errorf("row 2 = %q, want the first item and its detail", rows[2])
	}
	if !strings.Contains(rows[3], "Remote workspaces are not supported") {
		t.Errorf("row 3 = %q, want the second item", rows[3])
	}
}

func TestDrawScrollsToKeepHighlightVisible(t *testing.T) {
	p := New(Styles{})
	p.Show(Point{0, 0}, "T", items(10))
	g := grid.New(20, 5) // three item rows fit between the borders
	p.Draw(g)
	for range 5 {
		p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	g = grid.New(20, 5)
	p.Draw(g)
	rows := g.Plain()
	if !strings.Contains(rows[3], "i5") {
		t.Fatalf("after moving to row 5 the last visible row = %q, want i5", rows[3])
	}
}
