package style

import (
	"image/color"
	"strings"

	"github.com/tbxark/gvim/internal/decor"
	"github.com/tbxark/gvim/internal/theme"
	"github.com/tbxark/gvim/internal/ui/grid"
)

// semanticNames are the color names a plugin may use instead of a theme key. Each lists
// the theme keys that give it, the first the theme defines winning, and the RGB it has
// when the theme defines none. Resolving them when a frame is painted makes the
// decorations follow the theme the user picks.
var semanticNames = map[string]struct {
	keys     []string
	fallback color.RGBA
}{
	"error":   {[]string{"diagnostic.error_fg"}, color.RGBA{R: 255, G: 85, B: 85, A: 255}},
	"warning": {[]string{"diagnostic.warning_fg"}, color.RGBA{R: 255, G: 184, B: 108, A: 255}},
	"info":    {[]string{"diagnostic.info_fg"}, color.RGBA{R: 139, G: 233, B: 253, A: 255}},
	"hint":    {[]string{"diagnostic.hint_fg"}, color.RGBA{R: 98, G: 114, B: 164, A: 255}},
	"add":     {[]string{"syntax.string"}, color.RGBA{R: 80, G: 200, B: 120, A: 255}},
	"change":  {[]string{"diagnostic.warning_fg"}, color.RGBA{R: 227, G: 179, B: 65, A: 255}},
	"delete":  {[]string{"diagnostic.error_fg"}, color.RGBA{R: 248, G: 81, B: 73, A: 255}},
	"match":   {[]string{"search.match_bg"}, color.RGBA{R: 82, G: 65, B: 27, A: 255}},
	"accent":  {[]string{"ui.accent_bg"}, color.RGBA{R: 47, G: 129, B: 247, A: 255}},
	"dim":     {[]string{"editor.whitespace_indicator_fg", "editor.line_number_fg"}, color.RGBA{R: 110, G: 118, B: 129, A: 255}},
}

// ResolveColor returns the terminal color of a decoration color: a literal RGB value, a
// semantic name ("error", "add", "dim", ...), or a theme key "section.key". It returns nil
// when the color is unset, names nothing, or names a theme key the theme does not define
// or defines as the terminal default; the cell then keeps its own color.
func ResolveColor(th theme.Theme, c decor.Color) color.Color {
	switch {
	case c.Key == "" && c.Set:
		return color.RGBA{R: c.R, G: c.G, B: c.B, A: 255}
	case c.Key == "":
		return nil
	}
	if sem, ok := semanticNames[c.Key]; ok {
		for _, k := range sem.keys {
			if col := themeKey(th, k); col != nil {
				return col
			}
		}
		return sem.fallback
	}
	return themeKey(th, c.Key)
}

// themeKey resolves "section.key" against the theme.
func themeKey(th theme.Theme, key string) color.Color {
	section, name, ok := strings.Cut(key, ".")
	if !ok {
		return nil
	}
	return Color(th, section, name)
}

// DecorStyle converts the style of a decoration to the patch the pane lays over the look of
// the cells it covers: the colors resolve through the theme, and the attributes are the
// ones the style sets.
func DecorStyle(th theme.Theme, s decor.Style) grid.Style {
	return grid.Style{
		FG:        ResolveColor(th, s.FG),
		BG:        ResolveColor(th, s.BG),
		Bold:      s.Bold,
		Italic:    s.Italic,
		Underline: s.Underline,
		Strike:    s.Strike,
		Reverse:   s.Reverse,
	}
}
