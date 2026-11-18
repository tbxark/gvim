package grid

import (
	"image/color"
	"strings"
	"testing"
)

func BenchmarkGridRenderFrameReuse(b *testing.B) {
	const w, h = 160, 48
	g := New(w, h)
	line := strings.Repeat("func example(x int) int { return x + 1 } // sample ", 3)
	textStyle := Style{FG: color.RGBA{R: 220, G: 223, B: 228, A: 255}, BG: color.RGBA{R: 15, G: 20, B: 25, A: 255}}
	kwStyle := Style{Bold: true, FG: color.RGBA{R: 0, G: 173, B: 216, A: 255}, BG: color.RGBA{R: 15, G: 20, B: 25, A: 255}}
	b.ReportAllocs()
	for b.Loop() {
		g.Resize(w, h)
		g.Clear(textStyle)
		for y := range h {
			g.Put(0, y, 5, " 42 │", kwStyle)
			g.Put(5, y, w, line, textStyle)
		}
		_ = g.String()
	}
}

func BenchmarkGridSubPanePaint(b *testing.B) {
	const w, h = 160, 48
	g := New(w, h)
	line := "return fmt.Errorf(\"bench %d\", idx)"
	st := Style{FG: color.RGBA{R: 200, G: 210, B: 220, A: 255}, BG: color.RGBA{R: 20, G: 24, B: 30, A: 255}}
	b.ReportAllocs()
	for b.Loop() {
		g.Resize(w, h)
		sub, ok := g.Sub(30, 2, 120, 40)
		if !ok {
			b.Fatal("Sub failed")
		}
		sub.Clear(st)
		for y := range sub.Height() {
			sub.Put(0, y, sub.Width(), line, st)
		}
		_ = g.String()
	}
}
