package theme

// Keys in the "syntax" section of a theme file. The syntax highlighter asks
// for these eight; every bundled theme defines all of them. Some themes also
// define variable_builtin and punctuation_* keys, which the highlighter does
// not use.
const (
	SyntaxKeyword  = "keyword"
	SyntaxString   = "string"
	SyntaxComment  = "comment"
	SyntaxFunction = "function"
	SyntaxType     = "type"
	SyntaxVariable = "variable"
	SyntaxConstant = "constant"
	SyntaxOperator = "operator"
)

// Syntax returns the value for key in the "syntax" section. ok is false when
// the theme does not define the key.
//
// The value is not always RGB. The bundled "terminal" theme uses ANSI names
// such as "Red" and "Default". Check IsTerminalDefault and IsRGB before using
// the value, and fall back to the editor foreground for anything else.
func (t Theme) Syntax(key string) (Color, bool) {
	return t.Value("syntax", key)
}

// IsTerminalDefault reports whether the color means "use the terminal's own
// default" rather than a fixed color. Fresh writes this as "Default" or
// "Reset".
func (c Color) IsTerminalDefault() bool {
	return !c.IsRGB && (c.Name == "Default" || c.Name == "Reset")
}
