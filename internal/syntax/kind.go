package syntax

import "github.com/tbxark/gvim/internal/theme"

// Kind is the highlight class of a span. The set is intentionally small;
// several chroma token types map to the same Kind.
type Kind uint8

// Kinds, in the order Kind values are listed in this package.
const (
	KindPlain Kind = iota
	KindKeyword
	KindString
	KindComment
	KindFunction
	KindType
	KindVariable
	KindConstant
	KindOperator
	KindNumber
	// KindAttribute is a decorator or annotation, such as a Rust #[derive] or a
	// Python @decorator. Fresh has no colour of its own for it and paints it as a
	// constant.
	KindAttribute
	// KindBuiltin is a builtin variable such as self or this, which Fresh paints
	// with syntax.variable_builtin.
	KindBuiltin
	// The markup kinds are emitted by the markdown tokenizer. Their colours follow
	// Fresh's markdown scopes: a heading is a keyword, strong text a constant,
	// emphasis a variable, a link a function, a quote and strike-through a comment.
	// Each adds the attribute that a terminal can show and Fresh's colours alone
	// cannot.
	KindHeading
	KindStrong
	KindEmphasis
	KindStrongEmphasis
	KindLink
	KindQuote
	KindStrike

	// KindCount is the number of kinds, KindPlain included.
	KindCount
)

// Attr is a text attribute that a Kind adds to its colour.
type Attr uint8

// Attributes of a Kind.
const (
	AttrBold Attr = 1 << iota
	AttrItalic
	AttrUnderline
	AttrStrike
)

// kindInfo is what one Kind is called, the theme key it takes its colour from and the
// attributes it adds. A Kind with no theme key paints with the editor foreground.
type kindInfo struct {
	name     string
	themeKey string
	attrs    Attr
}

// kinds is indexed by Kind.
var kinds = [KindCount]kindInfo{
	KindPlain:          {name: "plain"},
	KindKeyword:        {name: "keyword", themeKey: theme.SyntaxKeyword},
	KindString:         {name: "string", themeKey: theme.SyntaxString},
	KindComment:        {name: "comment", themeKey: theme.SyntaxComment},
	KindFunction:       {name: "function", themeKey: theme.SyntaxFunction},
	KindType:           {name: "type", themeKey: theme.SyntaxType},
	KindVariable:       {name: "variable", themeKey: theme.SyntaxVariable},
	KindConstant:       {name: "constant", themeKey: theme.SyntaxConstant},
	KindOperator:       {name: "operator", themeKey: theme.SyntaxOperator},
	KindNumber:         {name: "number", themeKey: theme.SyntaxConstant},
	KindAttribute:      {name: "attribute", themeKey: theme.SyntaxConstant},
	KindBuiltin:        {name: "builtin", themeKey: theme.SyntaxVariableBuiltin},
	KindHeading:        {name: "heading", themeKey: theme.SyntaxKeyword, attrs: AttrBold},
	KindStrong:         {name: "strong", themeKey: theme.SyntaxConstant, attrs: AttrBold},
	KindEmphasis:       {name: "emphasis", themeKey: theme.SyntaxVariable, attrs: AttrItalic},
	KindStrongEmphasis: {name: "strong-emphasis", themeKey: theme.SyntaxConstant, attrs: AttrBold | AttrItalic},
	KindLink:           {name: "link", themeKey: theme.SyntaxFunction, attrs: AttrUnderline},
	KindQuote:          {name: "quote", themeKey: theme.SyntaxComment},
	KindStrike:         {name: "strike", themeKey: theme.SyntaxComment, attrs: AttrStrike},
}

// String returns a short lowercase name for k, for logs and tests.
func (k Kind) String() string {
	if k < KindCount {
		return kinds[k].name
	}
	return "plain"
}

// ThemeKey returns the key in the theme's "syntax" section for k. It is ""
// for KindPlain, which should use the editor foreground. KindNumber and
// KindAttribute have no theme key of their own, so they use "constant", as
// Fresh does.
func (k Kind) ThemeKey() string {
	if k < KindCount {
		return kinds[k].themeKey
	}
	return ""
}

// Attrs returns the text attributes that k adds to its colour.
func (k Kind) Attrs() Attr {
	if k < KindCount {
		return kinds[k].attrs
	}
	return 0
}

// ThemeFallbackKey returns the key to try when the theme has no value for
// ThemeKey, or "". Only KindBuiltin has one: a theme that does not know
// variable_builtin paints self and this as variables.
func (k Kind) ThemeFallbackKey() string {
	if k == KindBuiltin {
		return theme.SyntaxVariable
	}
	return ""
}
