package style

import (
	"strconv"

	"charm.land/lipgloss/v2"

	"github.com/tbxark/gvim/internal/theme"
)

// RainbowLevels is how many colors the rainbow keys give: indent_rainbow_1..6 and
// bracket_rainbow_1..6.
const RainbowLevels = 6

// Decor is the foreground colors of the decorations the editor text takes: indent
// guides, bracket highlights and whitespace marks inside a selection. Each role
// follows Fresh's fallback chain, so a theme that omits a key still has a color.
type Decor struct {
	WhitespaceSelected lipgloss.Style // editor.whitespace_indicator_selected_fg
	IndentGuide        lipgloss.Style // editor.indentation_guide_fg
	IndentRainbow      [RainbowLevels]lipgloss.Style
	BracketMatch       lipgloss.Style // editor.bracket_match_fg, the pair at the caret
	BracketRainbow     [RainbowLevels]lipgloss.Style
	// BracketPair holds the attributes of the pair at the caret when the brackets
	// are colored by depth: editor.bracket_rainbow_match_modifier, bold and
	// underlined when the theme gives none.
	BracketPair lipgloss.Style
}

// syntaxFallbacks are the syntax keys that the bracket rainbow levels fall back to.
var syntaxFallbacks = [RainbowLevels]string{"keyword", "function", "type", "string", "constant", "variable"}

// newDecor returns the decoration styles for th.
func newDecor(th theme.Theme) Decor {
	d := Decor{
		WhitespaceSelected: firstFG(th, ref{"editor", "whitespace_indicator_selected_fg"}, refWhitespace),
		IndentGuide:        firstFG(th, ref{"editor", "indentation_guide_fg"}, refWhitespace),
		BracketMatch:       firstFG(th, ref{"editor", "bracket_match_fg"}, ref{"editor", "cursor"}),
		BracketPair:        lipgloss.NewStyle().Bold(true).Underline(true),
	}
	for i := range RainbowLevels {
		n := strconv.Itoa(i + 1)
		d.IndentRainbow[i] = firstFG(th, ref{"editor", "indent_rainbow_" + n}, ref{"editor", "indentation_guide_fg"}, refWhitespace)
		d.BracketRainbow[i] = firstFG(th, ref{"editor", "bracket_rainbow_" + n}, ref{"syntax", syntaxFallbacks[i]})
	}
	if c, ok := th.Value("editor", "bracket_rainbow_match_modifier"); ok {
		d.BracketPair = modifiers(lipgloss.NewStyle(), c.Modifiers)
	}
	return d
}

// firstFG returns a style with the foreground of the first ref the theme defines.
func firstFG(th theme.Theme, refs ...ref) lipgloss.Style {
	for _, r := range refs {
		if _, ok := th.Value(r.section, r.key); ok {
			return build(th, r, ref{})
		}
	}
	return lipgloss.NewStyle()
}
