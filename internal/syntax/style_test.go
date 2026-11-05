package syntax

import (
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2"

	"github.com/tbxark/gvim/internal/theme"
)

// tokenTypeOf lexes src with lexer and returns the token type of the first
// token whose value is want.
func tokenTypeOf(t *testing.T, lexer chroma.Lexer, src, want string) chroma.TokenType {
	t.Helper()
	it, err := lexer.Tokenise(nil, src)
	if err != nil {
		t.Fatal(err)
	}
	for tok := it(); tok != chroma.EOF; tok = it() {
		if tok.Value == want {
			return tok.Type
		}
	}
	t.Fatalf("token %q not found in %q", want, src)
	return 0
}

// TestGoKeywordAndCommentColors checks the spec's acceptance I for a .go file:
// the package keyword and a comment get different, non-default colors.
func TestGoKeywordAndCommentColors(t *testing.T) {
	src := "package main\n// hello\n"
	lexer := Detect("main.go", []byte(src))
	if lexer.Config().Name != "Go" {
		t.Fatalf("lexer = %q, want Go", lexer.Config().Name)
	}
	kw := CrushStyle.Get(tokenTypeOf(t, lexer, src, "package")).Colour
	cm := CrushStyle.Get(tokenTypeOf(t, lexer, src, "// hello")).Colour
	text := CrushStyle.Get(chroma.Text).Colour
	if !kw.IsSet() || !cm.IsSet() {
		t.Fatalf("keyword colour %v, comment colour %v: both must be set", kw, cm)
	}
	if kw == cm {
		t.Fatalf("keyword and comment share colour %v", kw)
	}
	if kw == text || cm == text {
		t.Fatalf("keyword or comment uses the default text colour")
	}
	if kw.String() != "#ff60ff" {
		t.Fatalf("keyword colour = %s, want crush #FF60FF", kw)
	}
	if cm.String() != "#858392" {
		t.Fatalf("comment colour = %s, want crush #858392", cm)
	}
	if CrushStyle.Get(chroma.Comment).Italic != chroma.Yes {
		t.Fatal("comment should be italic")
	}
}

func TestCrushStyleSlots(t *testing.T) {
	tests := []struct {
		name string
		typ  chroma.TokenType
		want string
	}{
		{"string", chroma.LiteralString, "#68FFD6"},
		{"number", chroma.LiteralNumber, "#F5EF34"},
		{"function", chroma.NameFunction, "#00A4FF"},
		{"operator", chroma.Operator, "#FFFAF1"},
		{"error", chroma.Error, "#EB4268"},
		{"keyword subtype inherits keyword", chroma.KeywordDeclaration, "#FF60FF"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CrushStyle.Get(tt.typ).Colour.String(); got != strings.ToLower(tt.want) {
				t.Fatalf("colour = %s, want %s", got, tt.want)
			}
		})
	}
}

// TestUncoloredSlotDoesNotInheritKeyword guards the noinherit entries: a type
// name with no palette color must not pick up the keyword color through its
// category.
func TestUncoloredSlotDoesNotInheritKeyword(t *testing.T) {
	if CrushStyle.Get(chroma.KeywordType).Colour.IsSet() {
		t.Fatal("KeywordType should have no crush color")
	}
	if CrushStyle.Get(chroma.NameVariable).Colour.IsSet() {
		t.Fatal("NameVariable should have no crush color")
	}
}

func TestStyleFromThemeUsesThemeRGB(t *testing.T) {
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	style, err := StyleFromTheme(th)
	if err != nil {
		t.Fatal(err)
	}
	// dark.json: "keyword": [86, 156, 214]
	if got := style.Get(chroma.Keyword).Colour.String(); got != "#569cd6" {
		t.Fatalf("keyword colour = %s, want theme #569cd6", got)
	}
	// Keys the theme defines win over the palette, and the rest keep it.
	if got := style.Get(chroma.Comment).Colour; got == CrushStyle.Get(chroma.Comment).Colour {
		t.Fatalf("comment colour should come from the theme, got %s", got)
	}
	if style.Get(chroma.Comment).Italic != chroma.Yes {
		t.Fatal("comment should stay italic with a theme")
	}
}

func TestStyleFromThemeTerminalDefaultIsUncolored(t *testing.T) {
	th, err := theme.Load("terminal")
	if err != nil {
		t.Fatal(err)
	}
	style, err := StyleFromTheme(th)
	if err != nil {
		t.Fatal(err)
	}
	// terminal.json sets syntax.variable to the terminal default.
	if style.Get(chroma.NameVariable).Colour.IsSet() {
		t.Fatal("terminal default variable should have no colour")
	}
	// terminal.json sets keyword to the named color Red, which is not RGB, so
	// the palette value is used.
	if got := style.Get(chroma.Keyword).Colour.String(); got != "#ff60ff" {
		t.Fatalf("named keyword colour = %s, want palette #ff60ff", got)
	}
}

func TestZeroThemeIsCrushPalette(t *testing.T) {
	style, err := StyleFromTheme(theme.Theme{})
	if err != nil {
		t.Fatal(err)
	}
	for _, typ := range []chroma.TokenType{chroma.Keyword, chroma.Comment, chroma.LiteralNumber} {
		if style.Get(typ).Colour != CrushStyle.Get(typ).Colour {
			t.Fatalf("zero theme %v colour differs from CrushStyle", typ)
		}
	}
}

func TestHexColour(t *testing.T) {
	if got := hexColour(theme.RGB{R: 1, G: 171, B: 255}); got != "#01ABFF" {
		t.Fatalf("hexColour = %s", got)
	}
}
