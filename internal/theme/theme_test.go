package theme_test

import (
	"fmt"
	"testing"

	"github.com/tbxark/gvim/internal/theme"
)

func TestEveryBundledThemeLoads(t *testing.T) {
	names := theme.Names()
	if len(names) != 10 {
		t.Fatalf("Names() = %v, want the 10 Fresh themes", names)
	}
	for _, name := range names {
		th, err := theme.Load(name)
		if err != nil {
			t.Errorf("Load(%q): %v", name, err)
			continue
		}
		if th.Name == "" {
			t.Errorf("Load(%q): empty name", name)
		}
		if _, ok := th.Value("editor", "bg"); !ok {
			t.Errorf("Load(%q): editor.bg missing", name)
		}
	}
}

func TestDarkEditorBackground(t *testing.T) {
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	got := th.RGBOr("editor", "bg", theme.RGB{})
	want := theme.RGB{R: 13, G: 17, B: 23}
	if got != want {
		t.Fatalf("editor.bg = %v, want %v", got, want)
	}
}

func TestRGBOrFallsBack(t *testing.T) {
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	fallback := theme.RGB{R: 1, G: 2, B: 3}
	if got := th.RGBOr("editor", "no_such_key", fallback); got != fallback {
		t.Fatalf("missing key = %v, want fallback", got)
	}
	// ui.tab_active_bg is the named color "Default" in terminal.json, not RGB.
	term, err := theme.Load("terminal")
	if err != nil {
		t.Fatal(err)
	}
	if got := term.RGBOr("ui", "tab_active_bg", fallback); got != fallback {
		t.Fatalf("named color = %v, want fallback", got)
	}
}

func TestNamedAndModifierValues(t *testing.T) {
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	term, err := theme.Load("terminal")
	if err != nil {
		t.Fatal(err)
	}
	c, ok := term.Value("ui", "tab_active_fg")
	if !ok || c.IsRGB || c.Name != "Yellow" {
		t.Fatalf("terminal tab_active_fg = %+v, %v; want name Yellow", c, ok)
	}
	s, ok := th.Value("search", "current_match_fg")
	if !ok || !s.IsRGB || len(s.Modifiers) != 1 || s.Modifiers[0] != "bold" {
		t.Fatalf("current_match_fg = %+v, %v; want RGB with bold", s, ok)
	}
	m, ok := th.Value("ui", "semantic_highlight_modifier")
	_ = m
	if ok {
		t.Fatalf("dark theme should not define semantic_highlight_modifier")
	}
}

func TestTerminalThemeModifiers(t *testing.T) {
	th, err := theme.Load("terminal")
	if err != nil {
		t.Fatal(err)
	}
	c, ok := th.Value("ui", "semantic_highlight_modifier")
	if !ok || len(c.Modifiers) != 2 || c.Modifiers[1] != "underlined" {
		t.Fatalf("semantic_highlight_modifier = %+v, %v", c, ok)
	}
}

func TestZeroThemeIsSafe(t *testing.T) {
	var th theme.Theme
	fallback := theme.RGB{R: 9}
	if got := th.RGBOr("editor", "bg", fallback); got != fallback {
		t.Fatalf("zero theme = %v, want fallback", got)
	}
	if got := th.Sections(); len(got) != 0 {
		t.Fatalf("zero theme sections = %v", got)
	}
}

func TestUnknownThemeAndBadValues(t *testing.T) {
	if _, err := theme.Load("nope"); err == nil {
		t.Fatal("unknown theme must fail")
	}
	bad := map[string]string{
		"bad json":                         `{`,
		"three-number out of range":        `{"editor":{"bg":[300,0,0]}}`,
		"four numbers":                     `{"editor":{"bg":[1,2,3,4]}}`,
		"object without color or modifier": `{"editor":{"bg":{}}}`,
	}
	for name, src := range bad {
		if _, err := theme.Parse([]byte(src)); err == nil {
			t.Errorf("%s: Parse succeeded, want error", name)
		}
	}
}

func ExampleLoad() {
	th, _ := theme.Load("dark")
	c := th.RGBOr("editor", "bg", theme.RGB{})
	fmt.Printf("%s background: #%02x%02x%02x\n", th.Name, c.R, c.G, c.B)
	// Output: dark background: #0d1117
}
