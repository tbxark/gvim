package tui

import (
	"image/color"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/tui/grid"
)

var (
	swatchRed  = color.RGBA{R: 200, G: 10, B: 10, A: 255}
	swatchBlue = color.RGBA{R: 10, G: 10, B: 200, A: 255}
)

// TestDrawSettingsSwatches checks the chip painter: two cells in the chip's own
// colours, a one-cell gap, and a stop before the row's value.
func TestDrawSettingsSwatches(t *testing.T) {
	t.Parallel()
	g := grid.New(30, 3)
	swatches := []SettingsSwatch{
		{Label: "bg", FG: color.White, BG: swatchRed},
		{Label: "accent", FG: color.White, BG: swatchBlue},
	}
	drawSettingsSwatches(g, 2, 20, 1, swatches)
	for _, tt := range []struct {
		x    int
		want color.Color
	}{{2, swatchRed}, {3, swatchRed}, {5, swatchBlue}, {6, swatchBlue}} {
		cell := g.At(tt.x, 1)
		if cell.Text != "█" || cell.Style.BG != tt.want {
			t.Errorf("cell %d = %q %v, want a chip cell in %v", tt.x, cell.Text, cell.Style.BG, tt.want)
		}
	}
	if chip := g.At(4, 1); chip.Text == "█" || chip.Style.BG != nil {
		t.Errorf("gap cell = %q %v, want no chip", chip.Text, chip.Style.BG)
	}
	// The end limit stops the row before the value column.
	drawSettingsSwatches(g, 2, 5, 2, swatches)
	if cell := g.At(5, 2); cell.Text == "█" {
		t.Errorf("chip painted past the end: %q", cell.Text)
	}
	if chipWidth(swatches) != 5 {
		t.Errorf("chipWidth = %d, want 5", chipWidth(swatches))
	}
}

// TestSettingsPagePaintsThemePreview checks the page: the theme row's chips are
// drawn after its name and the footer names the swatch roles.
func TestSettingsPagePaintsThemePreview(t *testing.T) {
	t.Parallel()
	port := &fakeSettingsPort{snap: SettingsSnapshot{Sections: []SettingsSection{{
		Name: "Interface",
		Items: []SettingsItem{{
			Key: "theme", Path: []string{"theme"}, Name: "theme", Section: "Interface",
			Kind: "enum", Value: "dark", Choices: []string{"dark"},
			Preview: []SettingsSwatch{
				{Label: "bg", FG: color.White, BG: swatchRed},
				{Label: "fg", FG: swatchRed, BG: color.White},
				{Label: "accent", FG: color.White, BG: swatchBlue},
				{Label: "popup", FG: color.White, BG: swatchBlue},
			},
		}},
	}}}}
	_, p := openSettingsPage(t, port)
	g := grid.New(120, 40)
	p.Draw(g)
	idx := settingsRowIndex(t, p, "theme")
	y := p.box.Y + 2 + idx
	chipX := p.box.X + 2 + len("theme") + 2
	cell := g.At(chipX, y)
	if cell.Text != "█" || cell.Style.BG != swatchRed {
		t.Errorf("first chip cell = %q %v, want the background chip", cell.Text, cell.Style.BG)
	}
	acc := g.At(chipX+2*3, y)
	if acc.Style.BG != swatchBlue {
		t.Errorf("accent chip = %v, want %v", acc.Style.BG, swatchBlue)
	}
	footer := strings.Join(g.Plain(), "\n")
	if !strings.Contains(footer, "swatches: bg · fg · accent · popup") {
		t.Errorf("footer lacks the swatch legend:\n%s", footer)
	}
}
