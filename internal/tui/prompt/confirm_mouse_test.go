package prompt

import (
	tea "charm.land/bubbletea/v2"
	"github.com/tbxark/gvim/internal/tui/grid"
	"testing"
)

// clickInside sends a left click at the centre of r.
func clickInside(c *Confirm, i int) tea.Msg {
	r := c.hits[i]
	return tea.MouseClickMsg{X: r.X + r.W/2, Y: r.Y, Button: tea.MouseLeft}
}

func TestConfirmMouseAnswers(t *testing.T) {
	for _, tt := range []struct {
		name string
		hit  int
		want Outcome
	}{{"yes", 0, Yes}, {"no", 1, No}} {
		t.Run(tt.name, func(t *testing.T) {
			c := NewConfirmWith(testStyles(), "Q?", "tag", ConfirmOptions{Yes: "Yes", No: "No"})
			c.Draw(grid.New(40, 8))
			_, cmd := c.Update(clickInside(c, tt.hit))
			got, ok := emitted(cmd)
			if !ok || got.Outcome != tt.want || got.Tag != "tag" || c.Active() {
				t.Fatalf("click: %+v, %v, active=%v", got, ok, c.Active())
			}
		})
	}
}

func TestConfirmMouseIgnoresOutsideAndOtherButtons(t *testing.T) {
	c := NewConfirmWith(testStyles(), "Q?", nil, ConfirmOptions{Yes: "Yes", No: "No"})
	c.Draw(grid.New(40, 8))
	r := c.hits[0]
	for _, m := range []tea.MouseClickMsg{
		{X: 0, Y: 0, Button: tea.MouseLeft},
		{X: r.X, Y: r.Y + 1, Button: tea.MouseLeft},
		{X: r.X, Y: r.Y, Button: tea.MouseRight},
	} {
		_, cmd := c.Update(m)
		if cmd != nil || !c.Active() {
			t.Fatalf("unexpected answer for %+v", m)
		}
	}
}

func TestConfirmMouseUsesLatestVisibleGeometry(t *testing.T) {
	c := NewConfirmWith(testStyles(), "问题?", "tag", ConfirmOptions{Yes: "确认", No: "取消"})
	c.Draw(grid.New(40, 4))
	old := c.hits[0]
	c.Draw(grid.New(40, 8))
	_, cmd := c.Update(tea.MouseClickMsg{X: old.X, Y: old.Y, Button: tea.MouseLeft})
	if cmd != nil || !c.Active() {
		t.Fatal("old geometry answered after a resize")
	}
	_, cmd = c.Update(clickInside(c, 0))
	got, ok := emitted(cmd)
	if !ok || got.Outcome != Yes || got.Tag != "tag" {
		t.Fatalf("wide label click: %+v, %v", got, ok)
	}
}

func TestConfirmMouseDoesNotHitClippedAnswer(t *testing.T) {
	c := NewConfirmWith(testStyles(), "Q?", nil, ConfirmOptions{Yes: "Yes", No: "No"})
	c.Draw(grid.New(8, 6))
	// The Yes button is clipped by the card: a click past its recorded width, where
	// the unclipped button would reach, must not answer.
	r := c.hits[0]
	_, cmd := c.Update(tea.MouseClickMsg{X: r.X + r.W + 2, Y: r.Y, Button: tea.MouseLeft})
	if cmd != nil || !c.Active() {
		t.Fatal("clipped answer was clickable")
	}
}

func TestChoiceMouseAnswers(t *testing.T) {
	c := NewChoice(testStyles(), "Conflict", "Q?", "tag", []ChoiceOption{{Key: "a", Label: "Alpha"}, {Key: "b", Label: "Beta"}})
	c.Draw(grid.New(40, 8))
	r := c.hits[1]
	_, cmd := c.Update(tea.MouseClickMsg{X: r.X, Y: r.Y, Button: tea.MouseLeft})
	got, ok := emitted(cmd)
	if !ok || got.Choice != 1 || got.Tag != "tag" || c.Active() {
		t.Fatalf("click: %+v, %v, active=%v", got, ok, c.Active())
	}
}
