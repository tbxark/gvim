package syntax

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

func TestClassifyTokenTypes(t *testing.T) {
	tests := []struct {
		typ  chroma.TokenType
		want Kind
	}{
		{chroma.Keyword, KindKeyword},
		{chroma.KeywordDeclaration, KindKeyword},
		{chroma.KeywordReserved, KindKeyword},
		{chroma.KeywordType, KindType},
		{chroma.KeywordConstant, KindConstant},
		{chroma.LiteralString, KindString},
		{chroma.LiteralStringDouble, KindString},
		{chroma.LiteralStringEscape, KindString},
		{chroma.LiteralStringChar, KindString},
		{chroma.LiteralNumber, KindNumber},
		{chroma.LiteralNumberInteger, KindNumber},
		{chroma.LiteralNumberFloat, KindNumber},
		{chroma.LiteralNumberHex, KindNumber},
		{chroma.Comment, KindComment},
		{chroma.CommentSingle, KindComment},
		{chroma.CommentMultiline, KindComment},
		{chroma.CommentHashbang, KindComment},
		{chroma.CommentPreproc, KindKeyword},
		{chroma.CommentPreprocFile, KindString},
		{chroma.NameFunction, KindFunction},
		{chroma.NameFunctionMagic, KindFunction},
		{chroma.NameDecorator, KindAttribute},
		{chroma.NameBuiltin, KindFunction},
		{chroma.NameBuiltinPseudo, KindBuiltin},
		{chroma.NameClass, KindType},
		{chroma.NameException, KindType},
		{chroma.NameNamespace, KindType},
		{chroma.NameVariable, KindVariable},
		{chroma.NameVariableGlobal, KindVariable},
		{chroma.NameAttribute, KindVariable},
		{chroma.NameProperty, KindVariable},
		{chroma.NameConstant, KindConstant},
		{chroma.NameEntity, KindConstant},
		{chroma.NameKeyword, KindKeyword},
		{chroma.NameTag, KindKeyword},
		{chroma.NameOperator, KindOperator},
		{chroma.NameOther, KindPlain},
		{chroma.Name, KindPlain},
		{chroma.Operator, KindOperator},
		{chroma.OperatorWord, KindKeyword},
		{chroma.Punctuation, KindPlain},
		{chroma.Text, KindPlain},
		{chroma.TextWhitespace, KindPlain},
		{chroma.Error, KindPlain},
		{chroma.GenericHeading, KindKeyword},
		{chroma.GenericEmph, KindPlain},
		{chroma.Other, KindPlain},
	}
	for _, tt := range tests {
		if got := Classify(tt.typ); got != tt.want {
			t.Errorf("Classify(%v) = %v, want %v", tt.typ, got, tt.want)
		}
	}
}

func TestKindThemeKeys(t *testing.T) {
	tests := map[Kind]string{
		KindPlain:    "",
		KindKeyword:  "keyword",
		KindString:   "string",
		KindComment:  "comment",
		KindFunction: "function",
		KindType:     "type",
		KindVariable: "variable",
		KindConstant: "constant",
		KindOperator: "operator",
		KindNumber:   "constant",

		KindAttribute: "constant",
		KindBuiltin:   "variable_builtin",
		KindHeading:   "keyword",
		KindStrong:    "constant",
		KindEmphasis:  "variable",
		KindLink:      "function",
		KindQuote:     "comment",
		KindStrike:    "comment",
	}
	for k, want := range tests {
		if got := k.ThemeKey(); got != want {
			t.Errorf("%v.ThemeKey() = %q, want %q", k, got, want)
		}
	}
}

// reconstruct rebuilds each line from its spans. It fails the test when a line
// is not covered exactly once, in order.
func reconstruct(t *testing.T, text []string, spans [][]Span) {
	t.Helper()
	if len(spans) != len(text) {
		t.Fatalf("got %d span rows for %d lines", len(spans), len(text))
	}
	for i, line := range text {
		pos := 0
		var b strings.Builder
		for _, s := range spans[i] {
			if s.Start != pos || s.End <= s.Start || s.End > len(line) {
				t.Fatalf("line %d: bad span %+v after pos %d (len %d): %v", i, s, pos, len(line), spans[i])
			}
			b.WriteString(line[s.Start:s.End])
			pos = s.End
		}
		if pos != len(line) {
			t.Fatalf("line %d: spans end at %d, line length %d", i, pos, len(line))
		}
		if b.String() != line {
			t.Fatalf("line %d: reconstructed %q, want %q", i, b.String(), line)
		}
	}
}

func kindsOf(spans []Span) []Kind {
	out := make([]Kind, 0, len(spans))
	for _, s := range spans {
		out = append(out, s.Kind)
	}
	return out
}

func TestTokenizeGoLine(t *testing.T) {
	text := []string{`package main`, `// hi`, `func main() { x := "s" }`}
	spans, err := Tokenize(lexers.Get("go"), text)
	if err != nil {
		t.Fatal(err)
	}
	reconstruct(t, text, spans)
	if got := spans[0]; len(got) != 2 || got[0] != (Span{0, 7, KindKeyword}) {
		t.Fatalf("package line spans = %+v", got)
	}
	if got := spans[1]; len(got) != 1 || got[0] != (Span{0, 5, KindComment}) {
		t.Fatalf("comment line spans = %+v", got)
	}
	// "func main() { x := "s" }": func=keyword, main=function, string at 24..27.
	var sawFunc, sawString bool
	for _, s := range spans[2] {
		if s.Kind == KindFunction && text[2][s.Start:s.End] == "main" {
			sawFunc = true
		}
		if s.Kind == KindString && text[2][s.Start:s.End] == `"s"` {
			sawString = true
		}
	}
	if !sawFunc || !sawString {
		t.Fatalf("func/string spans missing: %+v", spans[2])
	}
}

func TestTokenizeMultiLineComment(t *testing.T) {
	text := []string{"/* a", "   b */", "int x;"}
	spans, err := Tokenize(lexers.Get("c"), text)
	if err != nil {
		t.Fatal(err)
	}
	reconstruct(t, text, spans)
	if len(spans[0]) == 0 || spans[0][0].Kind != KindComment {
		t.Fatalf("first line should start with a comment: %+v", spans[0])
	}
	if len(spans[1]) == 0 || spans[1][0].Kind != KindComment {
		t.Fatalf("second line should continue the comment: %+v", spans[1])
	}
}

func TestTokenizeEmptyAndCRLF(t *testing.T) {
	text := []string{"", "a = 1\r", "", "b"}
	spans, err := Tokenize(lexers.Get("python"), text)
	if err != nil {
		t.Fatal(err)
	}
	reconstruct(t, text, spans)
	if spans[0] != nil || spans[2] != nil {
		t.Fatalf("empty lines should have nil spans: %+v %+v", spans[0], spans[2])
	}
}

func TestTokenizeNilLexerIsPlain(t *testing.T) {
	text := []string{"hello world", ""}
	spans, err := Tokenize(nil, text)
	if err != nil {
		t.Fatal(err)
	}
	reconstruct(t, text, spans)
	for _, s := range spans[0] {
		if s.Kind != KindPlain {
			t.Fatalf("fallback lexer produced %v", s.Kind)
		}
	}
}

func TestTokenizeKindsForPython(t *testing.T) {
	text := []string{"def f(x):", `    return "hi" + 42`}
	spans, err := Tokenize(lexers.Get("python"), text)
	if err != nil {
		t.Fatal(err)
	}
	reconstruct(t, text, spans)
	kinds := kindsOf(spans[1])
	if !containsKind(kinds, KindKeyword) || !containsKind(kinds, KindString) || !containsKind(kinds, KindNumber) {
		t.Fatalf("python line 2 kinds = %v", kinds)
	}
}

func containsKind(kinds []Kind, want Kind) bool {
	return slices.Contains(kinds, want)
}

// countingLexer counts Tokenise calls so cache tests can see re-lexing.
type countingLexer struct {
	chroma.Lexer
	calls *int
}

func (c countingLexer) Tokenise(opts *chroma.TokeniseOptions, text string) (chroma.Iterator, error) {
	*c.calls++
	return c.Lexer.Tokenise(opts, text)
}

func TestHighlighterCacheInvalidation(t *testing.T) {
	calls := 0
	h := NewHighlighter(countingLexer{Lexer: lexers.Get("go"), calls: &calls})
	text := []string{"package main", "var x = 1"}

	first := h.Lines(1, text)
	if calls != 1 {
		t.Fatalf("first Lines lexed %d times, want 1", calls)
	}
	reconstruct(t, text, first)

	again := h.Lines(1, text)
	if calls != 1 {
		t.Fatalf("same version re-lexed (calls=%d)", calls)
	}
	if &again[0][0] != &first[0][0] {
		t.Fatal("cached result should be the same slice")
	}

	edited := []string{"package main", "var y = 2"}
	h.Lines(2, edited)
	if calls != 2 {
		t.Fatalf("new version should re-lex (calls=%d)", calls)
	}

	h.Lines(2, edited)
	if calls != 2 {
		t.Fatalf("repeat of version 2 should be cached (calls=%d)", calls)
	}

	h.Invalidate()
	h.Lines(2, edited)
	if calls != 3 {
		t.Fatalf("Invalidate should force a re-lex (calls=%d)", calls)
	}

	h.Lines(2, []string{"package main", "var y = 2", "var z = 3"})
	if calls != 4 {
		t.Fatalf("line count change should re-lex (calls=%d)", calls)
	}
}

func TestHighlighterLanguage(t *testing.T) {
	h := NewHighlighter(nil)
	if got := h.Language(); got != "fallback" {
		t.Fatalf("nil lexer language = %q, want fallback", got)
	}
	h = NewHighlighter(Detect("main.go", nil))
	if got := h.Language(); got != "Go" {
		t.Fatalf("language = %q, want Go", got)
	}
}

// errLexer fails every Tokenise call.
type errLexer struct{ chroma.Lexer }

func (errLexer) Tokenise(*chroma.TokeniseOptions, string) (chroma.Iterator, error) {
	return nil, errTest
}

var errTest = errors.New("lex failed")

func TestHighlighterLexErrorFallsBackToPlain(t *testing.T) {
	h := NewHighlighter(errLexer{Lexer: lexers.Get("go")})
	text := []string{"package main", "", "x"}
	spans := h.Lines(1, text)
	reconstruct(t, text, spans)
	if spans[1] != nil {
		t.Fatalf("empty line should stay nil: %+v", spans[1])
	}
	for _, s := range spans[0] {
		if s.Kind != KindPlain {
			t.Fatalf("error fallback produced kind %v", s.Kind)
		}
	}
}
