package grid

import (
	"image/color"
	"reflect"
	"testing"
)

// CapturingKeys reports whether the grid records theme-key ids.
func (g *Grid) CapturingKeys() bool { return g.keys != nil }

// ReleaseKeys stops recording and frees the capture buffer.
func (g *Grid) ReleaseKeys() { g.keys = nil }

// TestKeyCaptureOffByDefault checks that attribution is opt-in: without
// CaptureKeys nothing is recorded and KeysAt stays zero.
func TestKeyCaptureOffByDefault(t *testing.T) {
	t.Parallel()
	g := New(4, 2)
	st := Style{FGKey: 7, BGKey: 9}
	g.Put(0, 0, 4, "ab", st)
	g.Set(2, 0, "c", st)
	g.Fill(0, 4, 1, st)
	if g.CapturingKeys() {
		t.Fatal("a new grid captures keys")
	}
	for _, p := range [][2]int{{0, 0}, {1, 0}, {2, 0}, {3, 1}} {
		if fg, bg := g.KeysAt(p[0], p[1]); fg != 0 || bg != 0 {
			t.Errorf("KeysAt(%d,%d) = %d,%d, want 0,0 with capture off", p[0], p[1], fg, bg)
		}
	}
}

// TestKeyCaptureRecordsWrites checks the capture buffer: Put, Set and Fill record
// their style's ids per cell (wide graphemes on both cells), Release turns it off,
// and KeysAt is safe outside the grid.
func TestKeyCaptureRecordsWrites(t *testing.T) {
	t.Parallel()
	g := New(5, 2)
	g.CaptureKeys()
	if !g.CapturingKeys() {
		t.Fatal("CaptureKeys did not enable capture")
	}
	text := Style{FGKey: 3, BGKey: 4}
	wide := Style{FGKey: 5, BGKey: 6}
	fill := Style{FGKey: 7, BGKey: 8}
	g.Put(0, 0, 5, "ab", text)
	g.Put(2, 0, 5, "界", wide)
	g.Fill(0, 5, 1, fill)
	for _, tt := range []struct {
		x, y   int
		fg, bg uint16
	}{{0, 0, 3, 4}, {1, 0, 3, 4}, {2, 0, 5, 6}, {3, 0, 5, 6}, {0, 1, 7, 8}, {4, 1, 7, 8}} {
		if fg, bg := g.KeysAt(tt.x, tt.y); fg != tt.fg || bg != tt.bg {
			t.Errorf("KeysAt(%d,%d) = %d,%d, want %d,%d", tt.x, tt.y, fg, bg, tt.fg, tt.bg)
		}
	}
	if fg, bg := g.KeysAt(-1, 0); fg != 0 || bg != 0 {
		t.Error("KeysAt outside the grid did not return zero")
	}
	if fg, bg := g.KeysAt(0, 5); fg != 0 || bg != 0 {
		t.Error("KeysAt beyond the height did not return zero")
	}
	g.ReleaseKeys()
	if g.CapturingKeys() {
		t.Error("ReleaseKeys left capture on")
	}
	g.Put(0, 0, 5, "z", Style{FGKey: 1})
	if fg, _ := g.KeysAt(0, 0); fg != 0 {
		t.Error("a write after ReleaseKeys was captured")
	}
}

// TestCaptureDoesNotChangeRendering checks the capture keys never reach the cells'
// styles or the SGR output: a captured grid paints exactly like a plain one.
func TestCaptureDoesNotChangeRendering(t *testing.T) {
	t.Parallel()
	plain := New(3, 2)
	captured := New(3, 2)
	captured.CaptureKeys()
	st := Style{FG: color.RGBA{R: 1, G: 2, B: 3, A: 255}, BGKey: 9, Bold: true}
	for _, g := range []*Grid{plain, captured} {
		g.Put(0, 0, 3, "abc", st)
		g.Fill(0, 3, 1, st)
	}
	if !reflect.DeepEqual(plain.Plain(), captured.Plain()) {
		t.Error("capture changed the plain text")
	}
	if !reflect.DeepEqual(plain.Lines(), captured.Lines()) {
		t.Error("capture changed the SGR output")
	}
}

// TestResetBlanksTheGrid checks that Reset returns every cell to a blank default one and
// clears the key capture, so a surface can be reused for the next frame.
func TestResetBlanksTheGrid(t *testing.T) {
	t.Parallel()
	g := New(4, 2)
	g.CaptureKeys()
	st := Style{FGKey: 7, BGKey: 9, Bold: true}
	g.Put(0, 0, 4, "ab", st)
	g.Put(0, 1, 4, "你", st)
	g.Reset()
	for y := range 2 {
		for x := range 4 {
			if c := g.At(x, y); c.Text != " " || c.Style != (Style{}) {
				t.Fatalf("cell (%d, %d) = %+v after Reset, want a blank default cell", x, y, c)
			}
			if fg, bg := g.KeysAt(x, y); fg != 0 || bg != 0 {
				t.Fatalf("cell (%d, %d) keeps keys %d/%d after Reset", x, y, fg, bg)
			}
		}
	}
}
