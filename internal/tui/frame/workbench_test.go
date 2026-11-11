package frame

import "testing"

func TestClassicFrameReservesCellsAndFallsBackWhenShort(t *testing.T) {
	l := ComputeLayout(80, 24, Input{Status: StatusView{Message: "a very long status message that must wrap across the editor area and occupy more than one row in this terminal"}})
	if !l.FrameOn || l.FrameTop != 1 || l.FrameBottom != 22 || l.EditorX != 1 || l.EditorW != 78 {
		t.Fatalf("classic layout = %+v", l)
	}
	if len(l.Notice) > 0 && l.NoticeY+len(l.Notice) > l.FrameBottom {
		t.Fatalf("notice overlaps frame: notice at %d (%d rows), border at %d", l.NoticeY, len(l.Notice), l.FrameBottom)
	}
	for _, pt := range [][2]int{{0, 1}, {79, 4}, {20, 22}} {
		if got := l.Hit(pt[0], pt[1]); got.Kind != HitNone {
			t.Errorf("border hit at %v = %+v", pt, got)
		}
	}
	small := ComputeLayout(80, 3, Input{})
	if small.FrameOn {
		t.Fatal("frame should yield its cells in a short terminal")
	}
}
