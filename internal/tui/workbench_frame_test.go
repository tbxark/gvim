package tui

import (
	"testing"

	"github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/internal/tui/frame"
)

func TestClassicWorkbenchDefaultGeometry(t *testing.T) {
	for _, tc := range []struct {
		name string
		side explorer.Side
	}{{"left", explorer.SideLeft}, {"right", explorer.SideRight}} {
		t.Run(tc.name, func(t *testing.T) {
			ed := newFakeEditor("hello")
			m := newTestModel(t, ed, newFakeTree(3), 80, 24)
			m.explorerOpen = true
			m.explorerSide = tc.side
			l := m.layout()
			if !l.FrameOn || l.FrameTop != 1 || l.FrameBottom != 22 || l.BodyTop != 3 || l.BodyBottom != 21 {
				t.Fatalf("classic geometry: %+v", l)
			}
			if l.EditorX < 1 || l.EditorX+l.EditorW > 79 {
				t.Fatalf("editor overlaps border: x=%d width=%d", l.EditorX, l.EditorW)
			}
			if hit := l.Hit(l.Panes[0].TextX, l.BodyTop); hit.Kind != frame.HitEditorText {
				t.Fatalf("editor text hit = %+v", hit)
			}
			g := m.paint()
			for _, c := range []struct {
				x, y int
				want string
			}{{0, 1, "┌"}, {79, 1, "┐"}, {0, 22, "└"}, {79, 22, "┘"}, {79, 3, "│"}} {
				if got := g.At(c.x, c.y).Text; got != c.want {
					t.Errorf("cell %d,%d = %q, want %q", c.x, c.y, got, c.want)
				}
			}
		})
	}
}
