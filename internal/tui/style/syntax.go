package style

import (
	"image/color"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/syntax"
	"github.com/tbxark/gvim/theme"
)

// SyntaxFG resolves the foreground of one syntax kind. base is the editor
// foreground, which a missing or unknown value falls back to.
func SyntaxFG(th theme.Theme, kind syntax.Kind, base color.Color) color.Color {
	key := kind.ThemeKey()
	c, ok := th.Syntax(key)
	if fb := kind.ThemeFallbackKey(); !ok && fb != "" {
		key = fb
		c, ok = th.Syntax(key)
	}
	switch {
	case !ok:
		return base
	case c.IsTerminalDefault():
		return nil
	default:
		if col := Color(th, "syntax", key); col != nil {
			return col
		}
		return base
	}
}

// ApplyAttributeNames applies theme attribute names to a grid style.
func ApplyAttributeNames(st *grid.Style, names []string) {
	for _, name := range theme.NormalizeModifiers(names) {
		switch name {
		case "bold":
			st.Bold = true
		case "italic":
			st.Italic = true
		case "underlined":
			st.Underline = true
		case "dim":
			st.Faint = true
		case "reversed":
			st.Reverse = true
		}
	}
}
