package tabstrip

import (
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/icons"
	"github.com/tbxark/gvim/internal/tui/style"
)

func TestActiveTabBoundariesMatch(t *testing.T) {
	tabs := []frame.TabInfo{{Name: "one.go"}, {Name: "two.go", Active: true}, {Name: "three.go"}}
	s := frame.PlaceStrip(0, 60, tabs, 0, icons.Plain)
	st := style.Styles{TabBar: lipgloss.NewStyle().Background(lipgloss.Color("0")), TabActive: lipgloss.NewStyle().Background(lipgloss.Color("4")), Scrollbar: lipgloss.NewStyle().Foreground(lipgloss.Color("8"))}
	g := grid.New(60, 1)
	Draw(g, st, 0, 0, s, frame.HitTarget{})
	var boundaries []grid.Cell
	for i, r := range s.Runs {
		if i > 0 && r.Kind == frame.HitTab && r.Index != s.Runs[i-1].Index && s.Runs[i-1].Kind == frame.HitTab {
			boundaries = append(boundaries, g.At(r.X, 0))
			if got := g.At(r.X+1, 0).Text; got != " " {
				t.Errorf("tab %d: cell after divider = %q, want padding", r.Index, got)
			}
			if hit, ok := runAt(s.Runs, r.X+1); !ok || hit.Kind != frame.HitTab || hit.Index != r.Index {
				t.Errorf("tab %d: padding hit = %+v", r.Index, hit)
			}
		}
	}
	if len(boundaries) != 2 || boundaries[0].Text != "│" || boundaries[1].Text != "│" || boundaries[0].Style != boundaries[1].Style {
		t.Fatalf("boundaries = %+v, want matching vertical lines", boundaries)
	}
}

// runAt returns the strip run covering column x.
func runAt(runs []frame.Run, x int) (frame.Run, bool) {
	for _, r := range runs {
		if x >= r.X && x < r.X+r.W {
			return r, true
		}
	}
	return frame.Run{}, false
}
