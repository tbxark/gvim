package syntax

import (
	"fmt"

	"github.com/alecthomas/chroma/v2"

	"github.com/tbxark/gvim/theme"
)

// Crush palette (Charmtone Pantera, refs/crush) used when a theme does not
// give a color for a slot. Spec section 7.
const (
	crushKeyword  = "#FF60FF"
	crushString   = "#68FFD6"
	crushNumber   = "#F5EF34"
	crushComment  = "#858392"
	crushFunction = "#00A4FF"
	crushOperator = "#FFFAF1"
	crushError    = "#EB4268"
)

// styleSlot ties one theme "syntax" key to the chroma token types that
// Classify maps to the matching Kind. An empty colour means the slot has no
// palette default, and the tokens take the terminal default foreground.
type styleSlot struct {
	themeKey string
	colour   string
	italic   bool
	tokens   []chroma.TokenType
}

// styleSlots lists every token type a chroma style colors. Token types not
// listed here inherit from their category, so a Keyword subtype such as
// KeywordDeclaration takes the keyword color.
//
// Token types that Classify maps to a different Kind than their category
// (KeywordType is a type, KeywordConstant a constant) are listed under that
// Kind's slot, so they do not inherit the wrong color.
var styleSlots = []styleSlot{
	{
		themeKey: theme.SyntaxKeyword,
		colour:   crushKeyword,
		tokens: []chroma.TokenType{
			chroma.Keyword, chroma.OperatorWord, chroma.NameKeyword,
			chroma.NameTag, chroma.CommentPreproc, chroma.GenericHeading,
			chroma.GenericSubheading,
		},
	},
	{
		themeKey: theme.SyntaxString,
		colour:   crushString,
		tokens:   []chroma.TokenType{chroma.LiteralString, chroma.LiteralDate, chroma.CommentPreprocFile},
	},
	{
		themeKey: theme.SyntaxComment,
		colour:   crushComment,
		italic:   true,
		tokens:   []chroma.TokenType{chroma.Comment},
	},
	{
		themeKey: theme.SyntaxFunction,
		colour:   crushFunction,
		tokens: []chroma.TokenType{
			chroma.NameFunction, chroma.NameBuiltin,
		},
	},
	{
		themeKey: theme.SyntaxType,
		tokens: []chroma.TokenType{
			chroma.KeywordType, chroma.NameClass, chroma.NameException, chroma.NameNamespace,
		},
	},
	{
		themeKey: theme.SyntaxVariable,
		tokens: []chroma.TokenType{
			chroma.NameVariable, chroma.NameAttribute, chroma.NameProperty,
		},
	},
	{
		themeKey: theme.SyntaxVariableBuiltin,
		tokens:   []chroma.TokenType{chroma.NameBuiltinPseudo},
	},
	{
		themeKey: theme.SyntaxConstant,
		tokens: []chroma.TokenType{
			chroma.KeywordConstant, chroma.NameConstant, chroma.NameEntity, chroma.NameDecorator,
		},
	},
	{
		themeKey: theme.SyntaxConstant,
		colour:   crushNumber,
		tokens:   []chroma.TokenType{chroma.LiteralNumber},
	},
	{
		themeKey: theme.SyntaxOperator,
		colour:   crushOperator,
		tokens:   []chroma.TokenType{chroma.Operator, chroma.NameOperator},
	},
	{
		colour: crushError,
		tokens: []chroma.TokenType{chroma.Error},
	},
}

// CrushStyle is the chroma style for the crush palette alone. It is what
// StyleFromTheme returns for a theme that has no syntax colors.
var CrushStyle = mustStyle(theme.Theme{})

// StyleFromTheme returns a chroma style for th. Each slot takes the RGB value
// the theme gives for its "syntax" key. A slot the theme does not define, or
// defines with a color name such as "Red", takes the crush palette value. A
// slot the theme sets to the terminal default takes no color.
//
// Comments are always italic. The zero Theme yields the crush palette alone.
func StyleFromTheme(th theme.Theme) (*chroma.Style, error) {
	entries := make(chroma.StyleEntries, 32)
	for _, slot := range styleSlots {
		value := slotValue(th, slot)
		for _, tok := range slot.tokens {
			entries[tok] = value
		}
	}
	return chroma.NewStyle("gvim", entries)
}

// slotValue returns the chroma entry text for one slot. An entry with no
// color is "noinherit", so the token does not pick up its parent's color.
func slotValue(th theme.Theme, slot styleSlot) string {
	colour := slot.colour
	if slot.themeKey != "" {
		if c, ok := th.Syntax(slot.themeKey); ok {
			switch {
			case c.IsRGB:
				colour = hexColour(c.RGB)
			case c.IsTerminalDefault():
				colour = ""
			}
		}
	}
	if colour == "" {
		return "noinherit"
	}
	if slot.italic {
		return colour + " italic"
	}
	return colour
}

// hexColour formats c as "#RRGGBB".
func hexColour(c theme.RGB) string {
	return fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B)
}

// mustStyle is StyleFromTheme that panics on error. The palette is fixed, so
// an error is a programming mistake.
func mustStyle(th theme.Theme) *chroma.Style {
	s, err := StyleFromTheme(th)
	if err != nil {
		panic(err)
	}
	return s
}
