package theme

import (
	"math"
	"testing"
)

// TestBundledChromeTextMeetsContrast pins the recolor floor for the chrome's text
// roles: 4.5:1 for labels, selected rows, tabs, menus, the status row and popup
// text, and 3:1 for the muted line-number role. A named ANSI value is the
// terminal's own palette, so a pair that uses one is skipped; the terminal theme
// is entirely named colours and its contrast is terminal-defined.
func TestBundledChromeTextMeetsContrast(t *testing.T) {
	t.Parallel()
	pairs := []struct {
		name   string
		fg, bg [2]string
		min    float64
	}{
		{"editor", [2]string{"editor", "fg"}, [2]string{"editor", "bg"}, 4.5},
		{"selection", [2]string{"ui", "popup_selection_fg"}, [2]string{"ui", "popup_selection_bg"}, 4.5},
		{"active tab", [2]string{"ui", "tab_active_fg"}, [2]string{"ui", "tab_active_bg"}, 4.5},
		{"inactive tab", [2]string{"ui", "tab_inactive_fg"}, [2]string{"ui", "tab_inactive_bg"}, 4.5},
		{"menu", [2]string{"ui", "menu_fg"}, [2]string{"ui", "menu_bg"}, 4.5},
		{"status bar", [2]string{"ui", "status_bar_fg"}, [2]string{"ui", "status_bar_bg"}, 4.5},
		{"popup text", [2]string{"ui", "popup_text_fg"}, [2]string{"ui", "popup_bg"}, 4.5},
		{"line numbers", [2]string{"editor", "line_number_fg"}, [2]string{"editor", "line_number_bg"}, 3.0},
	}
	for _, name := range Names() {
		th, err := Load(name)
		if err != nil {
			t.Fatalf("Load(%q): %v", name, err)
		}
		checked := 0
		for _, p := range pairs {
			fg, ok := rgbValue(th, p.fg)
			if !ok {
				continue
			}
			bg, ok := rgbValue(th, p.bg)
			if !ok {
				continue
			}
			checked++
			if r := contrastRatio(fg, bg); r < p.min {
				t.Errorf("%s: %s is %.2f:1, want %.1f:1", name, p.name, r, p.min)
			}
		}
		if checked == 0 {
			t.Logf("%s: all chrome text pairs are named ANSI colours; contrast is terminal-defined", name)
		}
	}
}

// rgbValue resolves a theme key to its RGB value. ok is false for a named ANSI
// value, whose rendered colour is the terminal's.
func rgbValue(th Theme, key [2]string) ([3]uint8, bool) {
	c, ok := th.Value(key[0], key[1])
	if !ok || !c.IsRGB {
		return [3]uint8{}, false
	}
	return [3]uint8{c.RGB.R, c.RGB.G, c.RGB.B}, true
}

// contrastRatio is WCAG 2's relative-luminance ratio between two RGB colours.
func contrastRatio(a, b [3]uint8) float64 {
	la, lb := relativeLuminance(a), relativeLuminance(b)
	hi, lo := math.Max(la, lb), math.Min(la, lb)
	return (hi + 0.05) / (lo + 0.05)
}

func relativeLuminance(c [3]uint8) float64 {
	channel := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.04045 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(c[0]) + 0.7152*channel(c[1]) + 0.0722*channel(c[2])
}
