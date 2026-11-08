package ui

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestNarrowBarMarkerOpensTheHiddenLabelsThroughTheRoot: at 40 columns the last column
// of the bar is the overflow marker. A press on it opens the labels the bar does not
// show, and Enter on one of them opens that label's dropdown.
func TestNarrowBarMarkerOpensTheHiddenLabelsThroughTheRoot(t *testing.T) {
	m := menuModel(t, false)
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 16})
	m.Update(tea.MouseClickMsg(tea.Mouse{X: 39, Y: 0, Button: tea.MouseLeft}))
	if m.menu.Title() != "⋯" {
		t.Fatalf("a press on the marker opened %q, want the overflow list", m.menu.Title())
	}
	var labels []string
	for _, r := range m.menu.Rows() {
		labels = append(labels, r.Label)
	}
	if !slices.Equal(labels, []string{"LSP", "Help"}) {
		t.Fatalf("the overflow list is %v, want LSP and Help", labels)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.menu.Title() != "LSP" {
		t.Errorf("Enter on the first hidden label opened %q, want LSP", m.menu.Title())
	}
}
