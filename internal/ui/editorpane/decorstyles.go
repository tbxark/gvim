package editorpane

import (
	"github.com/tbxark/gvim/internal/ui/grid"
	"github.com/tbxark/gvim/internal/ui/style"
)

// DecorStyles are the foregrounds of the text decorations. A zero value draws the
// decoration in the color of the text under it.
type DecorStyles struct {
	// WhitespaceSelected is the color of whitespace marks inside the selection.
	WhitespaceSelected grid.Style
	// IndentGuide and IndentRainbow color the indent guides; the rainbow is used
	// when the guides are colored by level.
	IndentGuide   grid.Style
	IndentRainbow [style.RainbowLevels]grid.Style
	// BracketMatch colors the bracket at the caret and its partner. BracketRainbow
	// colors the brackets by depth, and then BracketPair gives the attributes that
	// mark the pair instead.
	BracketMatch   grid.Style
	BracketRainbow [style.RainbowLevels]grid.Style
	BracketPair    grid.Style
	// FoldPlaceholder is the text drawn after the header of a collapsed fold.
	FoldPlaceholder grid.Style
}

// newDecorStyles builds the decoration styles from the theme's roles. The fold
// placeholder is the text style with the gutter's foreground, in italics.
func newDecorStyles(d style.Decor, text, gutter grid.Style) DecorStyles {
	out := DecorStyles{
		WhitespaceSelected: style.Cell(d.WhitespaceSelected),
		IndentGuide:        style.Cell(d.IndentGuide),
		BracketMatch:       style.Cell(d.BracketMatch),
		BracketPair:        style.Cell(d.BracketPair),
		FoldPlaceholder:    text,
	}
	out.FoldPlaceholder.FG = gutter.FG
	out.FoldPlaceholder.Italic = true
	for i := range style.RainbowLevels {
		out.IndentRainbow[i] = style.Cell(d.IndentRainbow[i])
		out.BracketRainbow[i] = style.Cell(d.BracketRainbow[i])
	}
	return out
}
