package prompt

import (
	tea "charm.land/bubbletea/v2"
	"github.com/tbxark/gvim/internal/tui/grid"
	"testing"
)

func TestConfirmMouseAnswers(t *testing.T) {
	for _, tt := range []struct {
		name string
		x    int
		want Outcome
	}{{"yes", 5, Yes}, {"no", 13, No}} {
		t.Run(tt.name, func(t *testing.T) {
			c := NewConfirmWith(testStyles(), "Q?", "tag", ConfirmOptions{Yes: "Yes", No: "No"})
			c.Draw(grid.New(40, 4))
			_, cmd := c.Update(tea.MouseClickMsg{X: tt.x, Y: 3, Button: tea.MouseLeft})
			got, ok := emitted(cmd)
			if !ok || got.Outcome != tt.want || got.Tag != "tag" || c.Active() {
				t.Fatalf("click: %+v, %v, active=%v", got, ok, c.Active())
			}
		})
	}
}

func TestConfirmMouseIgnoresOutsideAndOtherButtons(t *testing.T) {
	c := NewConfirmWith(testStyles(), "Q?", nil, ConfirmOptions{Yes: "Yes", No: "No"})
	c.Draw(grid.New(40, 4))
	for _, m := range []tea.MouseClickMsg{{X: 0, Y: 3, Button: tea.MouseLeft}, {X: 5, Y: 2, Button: tea.MouseLeft}, {X: 5, Y: 3, Button: tea.MouseRight}} {
		_, cmd := c.Update(m)
		if cmd != nil || !c.Active() {
			t.Fatalf("unexpected answer for %+v", m)
		}
	}
}

func TestConfirmMouseUsesLatestVisibleGeometry(t *testing.T) {
	c := NewConfirmWith(testStyles(), "问题?", "tag", ConfirmOptions{Yes: "确认", No: "取消"})
	c.Draw(grid.New(40, 4))
	c.Draw(grid.New(40, 6))
	_, cmd := c.Update(tea.MouseClickMsg{X: 8, Y: 3, Button: tea.MouseLeft})
	if cmd != nil || !c.Active() {
		t.Fatal("old row answered after resize")
	}
	_, cmd = c.Update(tea.MouseClickMsg{X: 10, Y: 5, Button: tea.MouseLeft})
	got, ok := emitted(cmd)
	if !ok || got.Outcome != Yes || got.Tag != "tag" {
		t.Fatalf("wide label click: %+v, %v", got, ok)
	}
}

func TestConfirmMouseDoesNotHitClippedAnswer(t *testing.T) {
	c := NewConfirmWith(testStyles(), "Q?", nil, ConfirmOptions{Yes: "Yes", No: "No"})
	c.Draw(grid.New(8, 4))
	_, cmd := c.Update(tea.MouseClickMsg{X: 13, Y: 3, Button: tea.MouseLeft})
	if cmd != nil || !c.Active() {
		t.Fatal("clipped answer was clickable")
	}
}

func TestChoiceMouseAnswers(t *testing.T) {
	c := NewChoice(testStyles(), "Q?", "tag", []ChoiceOption{{Key: "a", Label: "Alpha"}, {Key: "b", Label: "Beta"}})
	c.Draw(grid.New(40, 4))
	_, cmd := c.Update(tea.MouseClickMsg{X: 18, Y: 3, Button: tea.MouseLeft})
	got, ok := emitted(cmd)
	if !ok || got.Choice != 1 || got.Tag != "tag" || c.Active() {
		t.Fatalf("click: %+v, %v, active=%v", got, ok, c.Active())
	}
}
