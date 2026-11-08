package editorpane

import (
	"image/color"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/tui/grid"
)

func contrastOf(a, b color.Color) float64 {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	return contrastRatio([3]uint8{uint8(ar >> 8), uint8(ag >> 8), uint8(ab >> 8)}, [3]uint8{uint8(br >> 8), uint8(bg >> 8), uint8(bb >> 8)})
}

func TestRepairedFG(t *testing.T) {
	dark := color.RGBA{R: 40, G: 44, B: 52, A: 255}
	light := color.RGBA{R: 235, G: 235, B: 225, A: 255}
	tests := []struct {
		name   string
		fg, bg color.Color
		repair bool
	}{
		{"readable pair is kept", color.RGBA{R: 230, G: 230, B: 230, A: 255}, dark, false},
		{"same color on dark", dark, dark, true},
		{"close luminance on dark", color.RGBA{R: 70, G: 76, B: 86, A: 255}, dark, true},
		{"close luminance on light", color.RGBA{R: 215, G: 215, B: 205, A: 255}, light, true},
		{"no foreground is left to the terminal", nil, dark, false},
		{"no background is left to the terminal", dark, nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := repairedFG(tc.fg, tc.bg)
			if (got != nil) != tc.repair {
				t.Fatalf("repairedFG = %v, want repaired = %v", got, tc.repair)
			}
			if got != nil {
				if c := contrastOf(got, tc.bg); c < minContrast {
					t.Errorf("repaired contrast = %.2f, want at least %.1f", c, minContrast)
				}
			}
		})
	}
}

func TestRepairedFGTakesTheNearestPaletteColor(t *testing.T) {
	// A mid gray comment over a light gray band moves to a darker gray, not to a hue.
	got := repairedFG(color.RGBA{R: 140, G: 140, B: 140, A: 255}, color.RGBA{R: 150, G: 150, B: 150, A: 255})
	if got == nil {
		t.Fatal("expected a repair")
	}
	r, g, b, _ := got.RGBA()
	if r != g || g != b {
		t.Errorf("repaired color %v is not a gray", got)
	}
	if l, _, _, _ := got.RGBA(); l >= 140<<8|140 {
		t.Errorf("repaired color %v is not darker than the original", got)
	}
}

func TestOccurrenceKeepsTextLegible(t *testing.T) {
	band := color.RGBA{R: 60, G: 66, B: 78, A: 255}
	hidden := color.RGBA{R: 70, G: 76, B: 88, A: 255} // text color close to the band
	clear := color.RGBA{R: 240, G: 240, B: 240, A: 255}
	tests := []struct {
		name string
		text color.Color
		keep bool
	}{
		{"text that would vanish is repaired", hidden, false},
		{"readable text is not touched", clear, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := newDoc("foo foo")
			d.cursor = buffer.Pos{Line: 0, Col: 1}
			st := Styles{Text: grid.Style{FG: tc.text}, Occurrence: grid.Style{BG: band}}
			for k := range st.Syntax {
				st.Syntax[k] = st.Text
			}
			p := Pane{Doc: d, Occurrences: true, Styles: st}
			g := p.Render(20, 1)
			on := cellAt(t, g, 10, 0) // the second foo
			if bgOf(on) != band {
				t.Fatalf("second foo bg = %v, want the occurrence", bgOf(on))
			}
			fg := on.Style.FG
			if tc.keep && fg != tc.text {
				t.Errorf("fg = %v, want the text color kept", fg)
			}
			if !tc.keep && contrastOf(fg, band) < minContrast {
				t.Errorf("fg %v over the occurrence has contrast %.2f", fg, contrastOf(fg, band))
			}
			// Cells outside the occurrence keep the plain text color.
			if off := cellAt(t, g, 9, 0); off.Style.FG != tc.text {
				t.Errorf("fg outside the occurrence = %v, want %v", off.Style.FG, tc.text)
			}
		})
	}
}
