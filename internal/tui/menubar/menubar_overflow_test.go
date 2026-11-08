package menubar

import (
	"slices"
	"strings"
	"testing"
)

// TestBarShowsEveryLabelWhenTheFrameIsWideEnough: at 50 columns all seven labels fit and
// the bar has no overflow marker.
func TestBarShowsEveryLabelWhenTheFrameIsWideEnough(t *testing.T) {
	m := newBar(allOn(), 50, 20)
	for _, r := range labelRuns(false) {
		if got := m.Hit(r.x, 0); got.Kind != TargetLabel {
			t.Errorf("%s at column %d: %+v, want a label", r.title, r.x, got)
		}
	}
	if got := m.Hit(49, 0); got.Kind != TargetNone {
		t.Errorf("the last column of a wide bar is %+v, want no marker", got)
	}
}

// TestBarMarksTheLabelsThatDoNotFit: at 40 columns LSP and Help do not fit. The last
// column holds the overflow marker, and the labels before it stay clickable.
func TestBarMarksTheLabelsThatDoNotFit(t *testing.T) {
	m := newBar(allOn(), 40, 20)
	if got := m.Hit(39, 0); got.Kind != TargetMore {
		t.Fatalf("the last column of a narrow bar is %+v, want the overflow marker", got)
	}
	for _, r := range labelRuns(false) {
		got := m.Hit(r.x, 0)
		switch r.title {
		case "File", "Edit", "View", "Selection", "Go":
			if got.Kind != TargetLabel {
				t.Errorf("%s at column %d: %+v, want a label", r.title, r.x, got)
			}
		default:
			if got.Kind == TargetLabel {
				t.Errorf("%s is hidden at 40 columns, but column %d is its label", r.title, r.x)
			}
		}
	}
}

// TestMoreMarkerOpensTheHiddenLabels: a press on the marker opens its dropdown, which
// lists the hidden labels. Enter on one of them opens that label's dropdown.
func TestMoreMarkerOpensTheHiddenLabels(t *testing.T) {
	m, _ := send(newBar(allOn(), 40, 20), press(39, 0))
	if m.Title() != moreTitle {
		t.Fatalf("a press on the marker opened %q, want %q", m.Title(), moreTitle)
	}
	var titles []string
	for _, r := range m.Rows() {
		titles = append(titles, r.Label)
	}
	if !slices.Equal(titles, []string{"LSP", "Help"}) {
		t.Fatalf("the marker lists %v, want the hidden labels LSP and Help", titles)
	}
	m, _ = send(m, keyMsg("enter"))
	if m.Title() != "LSP" || !m.IsOpen() {
		t.Errorf("Enter on the first hidden label opened %q", m.Title())
	}
}

// TestHoverOnTheMarkerSwitchesToIt: as hovering another label does, hovering the marker
// while a dropdown is open switches to the dropdown of the hidden labels.
func TestHoverOnTheMarkerSwitchesToIt(t *testing.T) {
	m := newBar(allOn(), 40, 20).Activate()
	m, _ = send(m, motion(39, 0))
	if m.Title() != moreTitle || m.Selected() != 0 {
		t.Errorf("hover on the marker: title %q, highlight %d", m.Title(), m.Selected())
	}
}

// TestMoreDropdownStaysOpenWhenTheExplorerFocusIsSet: the marker's title is not a label
// of the bar, but its dropdown must stay open when the explorer focus is synced.
func TestMoreDropdownStaysOpenWhenTheExplorerFocusIsSet(t *testing.T) {
	m, _ := send(newBar(allOn(), 40, 20), press(39, 0))
	m = m.WithExplorerFocus(false)
	if m.Title() != moreTitle {
		t.Errorf("the marker dropdown closed on a focus sync, title %q", m.Title())
	}
}

// TestMoreMarkerIsPainted: the marker is the last cell of the bar row.
func TestMoreMarkerIsPainted(t *testing.T) {
	rows := paintRows(newBar(allOn(), 40, 20))
	if !strings.HasSuffix(rows[0], moreTitle) {
		t.Fatalf("bar row %q does not end with the marker", rows[0])
	}
	if strings.Contains(rows[0], "LSP") {
		t.Errorf("a hidden label is painted: %q", rows[0])
	}
}

// TestResizeClosesTheMoreList: the labels the list shows depend on the width, so a resize
// closes it. Setting the size it already has keeps it open, as every event does.
func TestResizeClosesTheMoreList(t *testing.T) {
	m, _ := send(newBar(allOn(), 40, 20), press(39, 0))
	if m.SetSize(40, 20).Title() != moreTitle {
		t.Fatal("setting the same size closed the list")
	}
	if got := m.SetSize(60, 20); got.IsOpen() {
		t.Errorf("a resize to 60 columns left the list open at %q", got.Title())
	}
}
