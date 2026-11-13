package syntax

import (
	"regexp"
	"slices"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// Chroma's lexers are regular-expression machines, and several of them get small
// things wrong that a reader notices at once: Java has no number tokens at all, the
// TypeScript lexer reads the "?" and ":" of a ternary as a type annotation, the Rust
// lexer ends a number before its suffix and paints every attribute as a preprocessor
// line, the TOML and Go lexers know no 0x, 0o or 0b literals. A fixup repairs the token
// stream of one language before it becomes spans. The table below says which fixups run
// for which lexer; a language that is not in it is not touched, nor read into memory.

// fixup rewrites the tokens of one lexer. It may split, merge and retype tokens, but the
// values of the result must join to the same text as the input.
type fixup func(toks []chroma.Token) []chroma.Token

// fixups maps a lexer name, as Config().Name has it, to its fixups, run in order.
var fixups = map[string][]fixup{
	"TypeScript": {numbers(jsNumber), tsGenerics, tsTernary, tsDecorators, tsValueTypes},
	"JavaScript": {numbers(jsNumber)},
	"react":      {numbers(jsNumber)},
	"Go":         {numbers(goNumber)},
	"Python":     {numbers(pyNumber)},
	"Rust":       {numbers(rustNumber), rustAttributes, rustLifetimes},
	"TOML":       {numbers(tomlNumber), tomlSpecials},
	"Java":       {numbers(javaNumber)},
	"Kotlin":     {numbers(javaNumber)},
	"Scala":      {numbers(javaNumber)},
	"C":          {numbers(cNumber), cDirectives},
	"C++":        {numbers(cNumber), cDirectives},
	"MySQL":      {numbers(sqlNumber)},
	"CSS":        {cssPseudo},
	"YAML":       {yamlFlow},
}

// Number literals, one expression per family of languages. Each is anchored at the
// start of the text it is matched against, and takes the longest literal there.
var (
	jsNumber = regexp.MustCompile(`^(?:0[xX][0-9a-fA-F]+(?:_[0-9a-fA-F]+)*n?|0[bB][01]+(?:_[01]+)*n?|0[oO][0-7]+(?:_[0-7]+)*n?|` +
		`(?:\d+(?:_\d+)*(?:\.(?:\d+(?:_\d+)*)?)?|\.\d+(?:_\d+)*)(?:[eE][+-]?\d+(?:_\d+)*)?n?)`)
	javaNumber = regexp.MustCompile(`^(?:0[xX][0-9a-fA-F_]+[lL]?|0[bB][01_]+[lL]?|` +
		`(?:\d[\d_]*(?:\.[\d_]+)?|\.\d[\d_]*)(?:[eE][+-]?\d[\d_]*)?[fFdDlLuU]?)`)
	cNumber = regexp.MustCompile(`^(?:0[xX][0-9a-fA-F']+(?:[uU]?[lL]{0,2}|[lL]{1,2}[uU])|0[bB][01']+(?:[uU]?[lL]{0,2}|[lL]{1,2}[uU])|` +
		`(?:\d[\d']*(?:\.[\d']*)?|\.\d[\d']*)(?:[eE][+-]?\d+)?(?:[fFlL]|[uU]?[lL]{0,2}|[lL]{1,2}[uU])?)`)
	goNumber = regexp.MustCompile(`^(?:0[xX][0-9a-fA-F_]+(?:\.[0-9a-fA-F_]*)?(?:[pP][+-]?\d+)?i?|0[oO][0-7_]+i?|0[bB][01_]+i?|` +
		`(?:\d[\d_]*(?:\.[\d_]*)?|\.\d[\d_]*)(?:[eE][+-]?\d[\d_]*)?i?)`)
	pyNumber = regexp.MustCompile(`^(?:0[xX][0-9a-fA-F_]+|0[oO][0-7_]+|0[bB][01_]+|` +
		`(?:\d[\d_]*(?:\.[\d_]*)?|\.\d[\d_]*)(?:[eE][+-]?\d[\d_]*)?[jJ]?)`)
	rustNumber = regexp.MustCompile(`^(?:0x[0-9a-fA-F_]+(?:[iu](?:8|16|32|64|128|size))?|0o[0-7_]+(?:[iu](?:8|16|32|64|128|size))?|` +
		`0b[01_]+(?:[iu](?:8|16|32|64|128|size))?|` +
		`\d[\d_]*(?:\.\d[\d_]*)?(?:[eE][+-]?[\d_]*\d[\d_]*)?(?:[iu](?:8|16|32|64|128|size)|f32|f64)?)`)
	tomlNumber = regexp.MustCompile(`^[+-]?(?:0x[0-9a-fA-F_]+|0o[0-7_]+|0b[01_]+|` +
		`\d[\d_]*(?:\.\d[\d_]*)?(?:[eE][+-]?\d[\d_]*)?)`)
	sqlNumber = regexp.MustCompile(`^(?:0[xX][0-9a-fA-F]+|(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?)`)
)

// maxNumberRun is how many bytes of the tokens after a number's first one are joined to
// look for the end of the literal. No literal is longer.
const maxNumberRun = 64

// numbers returns the fixup that finds the number literals of a language by the
// expression re. A literal starts at a number token, or at a name, keyword or
// error token that begins with a digit, which no identifier of these languages
// does. The literal may run on through the tokens that follow with no blank between,
// and the lexer may have cut it anywhere, as "0" then "xDEADBEEF" or "3" "." "5f". The
// tokens it covers become one number token, and the rest of the last one stays.
// A number token that is already as long as the match is left alone.
func numbers(re *regexp.Regexp) fixup {
	return func(toks []chroma.Token) []chroma.Token {
		out := make([]chroma.Token, 0, len(toks))
		for i := 0; i < len(toks); {
			t := toks[i]
			if !startsNumber(t) {
				out = append(out, t)
				i++
				continue
			}
			run, ends := numberRun(toks[i:])
			m := re.FindString(run)
			if m == "" || (isNumberToken(t.Type) && len(m) <= len(t.Value)) {
				out = append(out, t)
				i++
				continue
			}
			out = append(out, chroma.Token{Type: numberType(m), Value: m})
			// Skip the tokens the literal covers, keeping what is left of the last.
			k := 0
			for k < len(ends) && ends[k] < len(m) {
				k++
			}
			// ends counts only the part of a token before a blank or the run limit,
			// so what is left of the last token is found from where it starts.
			used := len(m)
			if k > 0 {
				used -= ends[k-1]
			}
			if v := toks[i+k].Value; used < len(v) {
				out = append(out, chroma.Token{Type: toks[i+k].Type, Value: v[used:]})
			}
			i += k + 1
		}
		return out
	}
}

// startsNumber reports whether t may be the first token of a number literal.
func startsNumber(t chroma.Token) bool {
	if t.Value == "" {
		return false
	}
	if isNumberToken(t.Type) {
		return true
	}
	if t.Value[0] < '0' || t.Value[0] > '9' {
		return false
	}
	switch t.Type.Category() {
	case chroma.Name, chroma.Keyword, chroma.Error, chroma.Text:
		return true
	}
	return false
}

func isNumberToken(t chroma.TokenType) bool { return t.InSubCategory(chroma.LiteralNumber) }

// numberRun joins the leading tokens of toks that hold no blank, up to maxNumberRun
// bytes, and returns the text with the offset at which each token of it ends. A token
// with a blank in it ends the run before the blank.
func numberRun(toks []chroma.Token) (run string, ends []int) {
	var b strings.Builder
	for _, t := range toks {
		v := t.Value
		cut := strings.IndexAny(v, " \t\r\n")
		if cut >= 0 {
			v = v[:cut]
		}
		if b.Len()+len(v) > maxNumberRun {
			v = v[:maxNumberRun-b.Len()]
			cut = 0
		}
		b.WriteString(v)
		ends = append(ends, b.Len())
		if cut >= 0 || b.Len() >= maxNumberRun {
			break
		}
	}
	return b.String(), ends
}

// numberType is the chroma type that says what kind of number m is.
func numberType(m string) chroma.TokenType {
	lower := strings.ToLower(m)
	switch {
	case strings.HasPrefix(lower, "0x"):
		return chroma.LiteralNumberHex
	case strings.HasPrefix(lower, "0o"):
		return chroma.LiteralNumberOct
	case strings.HasPrefix(lower, "0b"):
		return chroma.LiteralNumberBin
	case strings.ContainsAny(lower, ".e"):
		return chroma.LiteralNumberFloat
	}
	return chroma.LiteralNumberInteger
}

// isSpaceToken reports whether t holds nothing but blanks.
func isSpaceToken(t chroma.Token) bool { return strings.TrimSpace(t.Value) == "" }

// ternary operators

// tsTernary repairs the colour of a ternary in TypeScript. The lexer treats the ":"
// after the "?" as the start of a type annotation, so the word after it is painted as a
// type, as in "x ? 1 : null". The ":" that closes a "?" of the same bracket depth is an
// operator, and the word after it is read as an expression: a keyword or constant when
// it is one, a plain name otherwise.
func tsTernary(toks []chroma.Token) []chroma.Token {
	var pending []int // bracket depth of each open "?"
	depth := 0
	for i := range toks {
		t := &toks[i]
		switch {
		case t.Type == chroma.Punctuation && len(t.Value) == 1 && strings.Contains("([{", t.Value):
			depth++
		case t.Type == chroma.Punctuation && len(t.Value) == 1 && strings.Contains(")]}", t.Value):
			for len(pending) > 0 && pending[len(pending)-1] >= depth {
				pending = pending[:len(pending)-1]
			}
			depth--
		case t.Type == chroma.Punctuation && (t.Value == "," || t.Value == ";"):
			for len(pending) > 0 && pending[len(pending)-1] >= depth {
				pending = pending[:len(pending)-1]
			}
		case t.Type == chroma.Operator && t.Value == "?" && opensTernary(toks, i):
			pending = append(pending, depth)
		case len(pending) > 0 && pending[len(pending)-1] == depth && strings.TrimSpace(t.Value) == ":" &&
			(t.Type == chroma.Text || t.Type == chroma.Operator || t.Type == chroma.Punctuation):
			pending = pending[:len(pending)-1]
			t.Type = chroma.Operator
			if i+1 < len(toks) && toks[i+1].Type == chroma.KeywordType {
				toks[i+1].Type = expressionWordType(toks[i+1].Value)
			}
		}
	}
	// The lexer's " : " carries its blanks. The operator is the colon alone.
	out := make([]chroma.Token, 0, len(toks))
	for _, t := range toks {
		if t.Type == chroma.Operator && strings.TrimSpace(t.Value) == ":" && len(t.Value) > 1 {
			colon := strings.Index(t.Value, ":")
			out = append(out, chroma.Token{Type: chroma.Text, Value: t.Value[:colon]},
				chroma.Token{Type: chroma.Operator, Value: ":"},
				chroma.Token{Type: chroma.Text, Value: t.Value[colon+1:]})
			continue
		}
		out = append(out, t)
	}
	return out
}

// opensTernary reports whether the "?" token at i begins a ternary, which it does
// unless it is part of "?.", "??" or the "?:" of an optional member.
func opensTernary(toks []chroma.Token, i int) bool {
	if i+1 >= len(toks) {
		return false
	}
	next := toks[i+1]
	switch {
	case next.Type == chroma.Operator && next.Value == "?":
		return false
	case next.Type == chroma.Punctuation && next.Value == ".":
		return false
	case strings.TrimSpace(next.Value) == ":":
		return false
	}
	if i > 0 && toks[i-1].Type == chroma.Operator && toks[i-1].Value == "?" {
		return false
	}
	return true
}

// expressionWords are the words that read as keywords or constants in a value, and are
// not types, with the chroma type each has.
var expressionWords = map[string]chroma.TokenType{
	"true": chroma.KeywordConstant, "false": chroma.KeywordConstant, "null": chroma.KeywordConstant,
	"undefined": chroma.KeywordConstant, "NaN": chroma.KeywordConstant, "Infinity": chroma.KeywordConstant,
	"this": chroma.Keyword, "new": chroma.Keyword, "typeof": chroma.Keyword, "void": chroma.Keyword,
	"function": chroma.Keyword, "await": chroma.Keyword, "async": chroma.Keyword, "yield": chroma.Keyword,
	"delete": chroma.Keyword, "super": chroma.Keyword, "class": chroma.Keyword, "import": chroma.Keyword,
}

// expressionWordType is the chroma type of word, which the lexer took for a type, when
// it stands in a value.
func expressionWordType(word string) chroma.TokenType {
	if t, ok := expressionWords[word]; ok {
		return t
	}
	return chroma.NameOther
}

// tsGenerics repairs the type parameter in "A<T>", which the lexer reads as an HTML tag
// and paints as a keyword. A tag name that follows a name with no blank between, after a
// "<", is a type.
func tsGenerics(toks []chroma.Token) []chroma.Token {
	for i := 2; i < len(toks); i++ {
		if toks[i].Type == chroma.NameTag && toks[i-1].Value == "<" && toks[i-1].Type == chroma.Punctuation &&
			toks[i-2].Type.Category() == chroma.Name && !isSpaceToken(toks[i-2]) {
			toks[i].Type = chroma.KeywordType
		}
	}
	return toks
}

// tsDecorators repairs "@Component", which the lexer reads as a keyword declaration, to a
// decorator.
func tsDecorators(toks []chroma.Token) []chroma.Token {
	for i := range toks {
		if t := &toks[i]; t.Type.Category() == chroma.Keyword && len(t.Value) > 1 && t.Value[0] == '@' {
			t.Type = chroma.NameDecorator
		}
	}
	return toks
}

// tsBuiltinTypes are the lowercase names that are types in an annotation.
var tsBuiltinTypes = map[string]bool{
	"string": true, "number": true, "boolean": true, "any": true, "void": true, "never": true,
	"unknown": true, "object": true, "symbol": true, "bigint": true, "null": true, "undefined": true,
}

// tsValueTypes repairs the value in "{ key: value }", which the lexer reads as an
// annotation and paints as a type, as it does "foo" in "x ? 1 : foo". A word after a
// ":" is left a type when it is a builtin type or when its last segment starts with a
// capital, which is how types are written. Any other word is a plain name, and
// "true", "this", "new" and the like are the keywords and constants they are.
func tsValueTypes(toks []chroma.Token) []chroma.Token {
	for i := 1; i < len(toks); i++ {
		t := &toks[i]
		if t.Type != chroma.KeywordType || strings.TrimSpace(toks[i-1].Value) != ":" || toks[i-1].Type != chroma.Text {
			continue
		}
		if w, ok := expressionWords[t.Value]; ok && t.Value != "void" && t.Value != "null" && t.Value != "undefined" {
			t.Type = w
			continue
		}
		last := t.Value[strings.LastIndexByte(t.Value, '.')+1:]
		if last != "" && !tsBuiltinTypes[t.Value] && last[0] >= 'a' && last[0] <= 'z' {
			t.Type = chroma.NameOther
		}
	}
	return toks
}

// cssPseudo keeps the pseudo-classes and pseudo-elements of CSS, such as ":hover", the
// function colour. The lexer reports them as decorators, which are attributes now.
func cssPseudo(toks []chroma.Token) []chroma.Token {
	for i := range toks {
		if toks[i].Type == chroma.NameDecorator {
			toks[i].Type = chroma.NameFunction
		}
	}
	return toks
}

// rustLifetimes retypes "'a" and "'static", which the lexer takes for attributes and a
// builtin, to types. "'a" comes as the quote and the name, two tokens.
func rustLifetimes(toks []chroma.Token) []chroma.Token {
	for i := range toks {
		t := &toks[i]
		switch {
		case t.Value == "'" && t.Type == chroma.NameAttribute && i+1 < len(toks) && toks[i+1].Type == chroma.NameAttribute:
			t.Type, toks[i+1].Type = chroma.KeywordType, chroma.KeywordType
		case len(t.Value) > 1 && t.Value[0] == '\'' && t.Type == chroma.NameBuiltin:
			t.Type = chroma.KeywordType
		}
	}
	return toks
}

// rustAttributes retypes the attributes, "#[derive(Debug)]" and "#![allow(x)]", which the
// lexer reports as preprocessor lines, to decorators, so that they take the attribute
// colour and not the keyword one.
func rustAttributes(toks []chroma.Token) []chroma.Token {
	for i := range toks {
		if toks[i].Type == chroma.CommentPreproc {
			toks[i].Type = chroma.NameDecorator
		}
	}
	return toks
}

// tomlSpecialRe is the infinities and not-a-number of TOML, with their optional sign.
var tomlSpecialRe = regexp.MustCompile(`^[+-]?(?:inf|nan)$`)

// tomlSpecials repairs the numbers "+inf", "-inf", "nan" and their signed forms, which the
// lexer reports as an error and a name, or as one name.
func tomlSpecials(toks []chroma.Token) []chroma.Token {
	out := make([]chroma.Token, 0, len(toks))
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if (t.Value == "+" || t.Value == "-") && (t.Type == chroma.Error || t.Type == chroma.Punctuation || t.Type == chroma.Operator) &&
			i+1 < len(toks) && (toks[i+1].Value == "inf" || toks[i+1].Value == "nan") {
			out = append(out, chroma.Token{Type: chroma.LiteralNumberFloat, Value: t.Value + toks[i+1].Value})
			i++
			continue
		}
		if tomlSpecialRe.MatchString(t.Value) && (t.Type.Category() == chroma.Name || t.Type == chroma.Error) {
			t.Type = chroma.LiteralNumberFloat
		}
		out = append(out, t)
	}
	return out
}

// cDirectiveRe matches the directives whose line the C lexer paints whole as a
// preprocessor line, and splits off the name of the directive.
var cDirectiveRe = regexp.MustCompile(`^(#[ \t]*(?:define|undef|ifdef|ifndef|if|elif|elifdef|elifndef|pragma|error|warning|line)\b)([ \t]+)?`)

// cNamingDirectives are the directives whose first operand is a macro name.
var cNamingDirectives = []string{"define", "undef", "ifdef", "ifndef", "elifdef", "elifndef"}

// cDirectives splits a preprocessor line such as "#define MAX(a, b) ((a) > (b))" in
// the directive, a keyword, and the rest. The macro name is a function when "(" follows
// it and a constant otherwise, as in Fresh's C highlighting, and the rest is read as C is
// by the same lexer.
func cDirectives(toks []chroma.Token) []chroma.Token {
	out := make([]chroma.Token, 0, len(toks))
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		// The lexer gives the "#" and the directive as two tokens when a blank or nothing
		// is between them, and a macro with its body as the second one.
		if t.Type == chroma.CommentPreproc && strings.TrimSpace(t.Value) == "#" && i+1 < len(toks) && toks[i+1].Type == chroma.CommentPreproc {
			t.Value += toks[i+1].Value
			if cDirectiveRe.MatchString(t.Value) {
				i++
			} else {
				t.Value = toks[i].Value
			}
		}
		m := cDirectiveRe.FindStringSubmatch(t.Value)
		if t.Type != chroma.CommentPreproc || m == nil {
			out = append(out, t)
			continue
		}
		directive, rest := m[1], t.Value[len(m[0]):]
		out = append(out, chroma.Token{Type: chroma.CommentPreproc, Value: directive})
		if m[2] != "" {
			out = append(out, chroma.Token{Type: chroma.Text, Value: m[2]})
		}
		name := ""
		if slices.Contains(cNamingDirectives, strings.TrimLeft(directive[1:], " \t")) {
			name = identifierPrefix(rest)
		}
		if name != "" {
			typ := chroma.NameConstant
			if strings.HasPrefix(rest[len(name):], "(") && strings.HasSuffix(directive, "define") {
				typ = chroma.NameFunction
			}
			out = append(out, chroma.Token{Type: typ, Value: name})
			rest = rest[len(name):]
		}
		if rest != "" {
			out = append(out, relexC(rest)...)
		}
	}
	return out
}

// relexC reads the rest of a preprocessor line as C. A failure leaves it plain.
func relexC(text string) []chroma.Token {
	lexer := lexers.Get("c")
	if lexer == nil {
		return []chroma.Token{{Type: chroma.Text, Value: text}}
	}
	it, err := lexer.Tokenise(nil, text)
	if err != nil {
		return []chroma.Token{{Type: chroma.Text, Value: text}}
	}
	toks := numbers(cNumber)(it.Tokens())
	// The lexer ends the text with a newline that the line did not have.
	if n := len(toks); n > 0 && strings.HasSuffix(toks[n-1].Value, "\n") && !strings.HasSuffix(text, "\n") {
		toks[n-1].Value = strings.TrimSuffix(toks[n-1].Value, "\n")
		if toks[n-1].Value == "" {
			toks = toks[:n-1]
		}
	}
	return toks
}

// identifierPrefix returns the identifier that starts s, or "".
func identifierPrefix(s string) string {
	n := 0
	for n < len(s) {
		c := s[n]
		if c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || n > 0 && c >= '0' && c <= '9' {
			n++
			continue
		}
		break
	}
	return s[:n]
}
