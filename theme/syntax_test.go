package theme_test

import (
	"testing"

	"github.com/tbxark/gvim/theme"
)

// requiredSyntaxKeys are the keys the highlighter reads. Every bundled theme
// must define them.
var requiredSyntaxKeys = []string{
	theme.SyntaxKeyword,
	theme.SyntaxString,
	theme.SyntaxComment,
	theme.SyntaxFunction,
	theme.SyntaxType,
	theme.SyntaxVariable,
	theme.SyntaxConstant,
	theme.SyntaxOperator,
}

func TestEveryBundledThemeDefinesSyntaxKeys(t *testing.T) {
	for _, name := range theme.Names() {
		th, err := theme.Load(name)
		if err != nil {
			t.Fatalf("Load(%q): %v", name, err)
		}
		for _, key := range requiredSyntaxKeys {
			c, ok := th.Syntax(key)
			if !ok {
				t.Errorf("%s: syntax.%s missing", name, key)
				continue
			}
			if !c.IsRGB && c.Name == "" {
				t.Errorf("%s: syntax.%s has neither RGB nor a name", name, key)
			}
		}
	}
}

func TestSyntaxRGBThemesResolve(t *testing.T) {
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	// dark.json: "keyword": [255, 123, 114]
	want := theme.RGB{R: 255, G: 123, B: 114}
	if got := th.RGBOr("syntax", theme.SyntaxKeyword, theme.RGB{}); got != want {
		t.Fatalf("syntax.keyword = %v, want %v", got, want)
	}
}

func TestTerminalThemeSyntaxIsNamed(t *testing.T) {
	th, err := theme.Load("terminal")
	if err != nil {
		t.Fatal(err)
	}
	c, ok := th.Syntax(theme.SyntaxVariable)
	if !ok || c.IsRGB || !c.IsTerminalDefault() {
		t.Fatalf("terminal syntax.variable = %+v, %v; want terminal default", c, ok)
	}
	kw, ok := th.Syntax(theme.SyntaxKeyword)
	if !ok || kw.IsRGB || kw.Name != "Red" || kw.IsTerminalDefault() {
		t.Fatalf("terminal syntax.keyword = %+v, %v; want name Red", kw, ok)
	}
	// RGBOr must fall back for named colors.
	fb := theme.RGB{R: 1, G: 2, B: 3}
	if got := th.RGBOr("syntax", theme.SyntaxKeyword, fb); got != fb {
		t.Fatalf("named syntax color = %v, want fallback", got)
	}
}

func TestSyntaxMissingKey(t *testing.T) {
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := th.Syntax("number"); ok {
		t.Fatal("dark theme should not define syntax.number")
	}
	var zero theme.Theme
	if _, ok := zero.Syntax(theme.SyntaxKeyword); ok {
		t.Fatal("zero Theme should report missing syntax keys")
	}
}
