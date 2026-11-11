package frame

import "testing"

// borderless is a Style with no chrome: the seam a new look uses to reserve nothing.
type borderless struct{}

func (borderless) Chrome() Chrome { return Chrome{} }

// TestBorderlessStyleReservesNoCells checks that a Style without a border takes no
// cells, so the geometry reads the Style and not only the default.
func TestBorderlessStyleReservesNoCells(t *testing.T) {
	l := ComputeLayout(80, 24, Input{Style: borderless{}, ExplorerOpen: true})
	if l.FrameOn || l.BodyTop != 2 || l.BodyBottom != 22 || l.EditorX != 32 || l.ExBottom != l.BodyBottom {
		t.Fatalf("borderless layout = %+v", l)
	}
}
