package theme_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/theme"
)

func mustParse(t *testing.T, body string) theme.Theme {
	t.Helper()
	th, err := theme.Parse([]byte(body))
	if err != nil {
		t.Fatalf("Parse error = %v", err)
	}
	return th
}

func TestParseWarnsUnknownKeysAndRejectsBadValues(t *testing.T) {
	file, err := theme.ParseFile([]byte(`{"name":"x","editor":{"bg":[1,2,3],"typo_key":[4,5,6]}}`))
	if err != nil {
		t.Fatalf("ParseFile error = %v", err)
	}
	if len(file.Warnings) != 1 || !strings.Contains(file.Warnings[0], "editor.typo_key") {
		t.Fatalf("warnings = %v, want the unknown key", file.Warnings)
	}
	if _, err := theme.Parse([]byte(`{"name":"x","editor":{"bg":"Nope"}}`)); err == nil || !strings.Contains(err.Error(), "editor.bg") {
		t.Fatalf("bad value error = %v, want it to name editor.bg", err)
	}
	if _, err := theme.Parse([]byte(`{"name":"x","editor":{"bg":[300,0,0]}}`)); err == nil {
		t.Fatal("out-of-range component accepted")
	}
}

func TestPartialThemePicksABaseByBackgroundLuminance(t *testing.T) {
	light := mustParse(t, `{"name":"cream","editor":{"bg":[250, 240, 220]}}`)
	lightBase, err := theme.Load("light")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := light.Value("ui", "menu_bg")
	want, ok := lightBase.Value("ui", "menu_bg")
	if !ok || !sameColor(got, want) {
		t.Fatalf("ui.menu_bg = %+v, want the light base %+v", got, want)
	}
	if light.Dark() {
		t.Error("cream theme reported dark")
	}
	dark := mustParse(t, `{"name":"partial","editor":{"cursor":[1,2,3]}}`)
	darkBase, _ := theme.Load("dark")
	got, _ = dark.Value("ui", "menu_bg")
	want, _ = darkBase.Value("ui", "menu_bg")
	if !sameColor(got, want) {
		t.Fatalf("partial theme ui.menu_bg = %+v, want the dark base %+v", got, want)
	}
}

func TestExtendsAParsedTheme(t *testing.T) {
	th, err := theme.Parse([]byte(`{"name":"tweak","extends":"builtin://light","editor":{"cursor":[255,105,180]}}`))
	if err != nil {
		t.Fatalf("Parse error = %v", err)
	}
	if got, _ := th.Value("editor", "cursor"); !got.IsRGB || got.RGB != (theme.RGB{R: 255, G: 105, B: 180}) {
		t.Fatalf("cursor = %+v, want the override", got)
	}
	light, _ := theme.Load("light")
	want, _ := light.Value("ui", "menu_bg")
	if got, _ := th.Value("ui", "menu_bg"); !sameColor(got, want) {
		t.Fatalf("menu_bg = %+v, want the light base %+v", got, want)
	}
	if _, err := theme.Parse([]byte(`{"name":"t","extends":"dark","editor":{"cursor":[1,2,3]}}`)); err != nil {
		t.Fatalf("bare extends error = %v", err)
	}
	if _, err := theme.Parse([]byte(`{"name":"t","extends":"my-user-theme","editor":{"cursor":[1,2,3]}}`)); err == nil {
		t.Fatal("extends a user theme accepted")
	}
}

func standaloneThemeBody() string {
	// A complete standalone theme: every required color named.
	body := `{"name":"standalone","editor":{"bg":[10,10,10],"fg":[240,240,240],"cursor":[255,0,0],"current_line_bg":[20,20,20],"line_number_bg":[15,15,15],"line_number_fg":[100,100,100],"selection_bg":[30,30,60]},"ui":{"help_bg":[1,1,1],"help_fg":[2,2,2],"help_indicator_bg":[3,3,3],"help_indicator_fg":[4,4,4],"help_key_fg":[5,5,5],"help_separator_fg":[6,6,6],"popup_bg":[7,7,7],"popup_border_fg":[8,8,8],"popup_selection_bg":[9,9,9],"popup_text_fg":[10,10,10],"prompt_bg":[11,11,11],"prompt_fg":[12,12,12],"prompt_selection_bg":[13,13,13],"prompt_selection_fg":[14,14,14],"split_separator_fg":[15,15,15],"status_bar_bg":[16,16,16],"status_bar_fg":[17,17,17],"suggestion_bg":[18,18,18],"suggestion_selected_bg":[19,19,19],"tab_active_bg":[20,20,20],"tab_active_fg":[21,21,21],"tab_inactive_bg":[22,22,22],"tab_inactive_fg":[23,23,23],"tab_separator_bg":[24,24,24]},"syntax":{"comment":[30,30,30],"constant":[31,31,31],"function":[32,32,32],"keyword":[33,33,33],"operator":[34,34,34],"string":[35,35,35],"type":[36,36,36],"variable":[37,37,37]},"diagnostic":{"error_bg":[40,40,40],"error_fg":[41,41,41],"hint_bg":[42,42,42],"hint_fg":[43,43,43],"info_bg":[44,44,44],"info_fg":[45,45,45],"warning_bg":[46,46,46],"warning_fg":[47,47,47]},"search":{"match_bg":[50,50,50],"match_fg":[51,51,51]}}`
	return body
}

func TestStandaloneFallbackChains(t *testing.T) {
	th := mustParse(t, standaloneThemeBody())
	got, ok := th.Value("editor", "inactive_cursor")
	want, _ := th.Value("editor", "line_number_fg")
	if !ok || !sameColor(got, want) {
		t.Fatalf("inactive_cursor = %+v (ok %v), want the line_number_fg fallback %+v", got, ok, want)
	}
	hover, _ := th.Value("ui", "menu_hover_bg")
	sel, _ := th.Value("ui", "popup_selection_bg")
	if !sameColor(hover, sel) {
		t.Fatalf("menu_hover_bg = %+v, want the popup selection %+v", hover, sel)
	}
}

func TestAttributesOnBackgroundAndLegacyModifiers(t *testing.T) {
	th := mustParse(t, `{"name":"attrs","extends":"builtin://dark","editor":{"selection_bg":{"color":"Default","modifier":["underlined"]},"selection_modifier":["reversed"]}}`)
	sel, _ := th.Value("editor", "selection_bg")
	if len(sel.Modifiers) != 1 || sel.Modifiers[0] != "reversed" {
		t.Fatalf("selection_bg modifiers = %v, want the legacy selection_modifier to win", sel.Modifiers)
	}
	mod, ok := th.Value("editor", "selection_modifier")
	if !ok || len(mod.Modifiers) != 1 || mod.Modifiers[0] != "reversed" {
		t.Fatalf("selection_modifier = %+v, %v", mod, ok)
	}
	if _, ok := th.Value("ui", "popup_text_fg"); !ok {
		t.Fatal("popup_text_fg missing")
	}
}

func TestNamedColorsAndReset(t *testing.T) {
	th := mustParse(t, `{"name":"named","extends":"builtin://dark","editor":{"fg":"Yellow","bg":"Default"}}`)
	fg, _ := th.Value("editor", "fg")
	if fg.IsRGB || fg.Name != "Yellow" || fg.IsTerminalDefault() {
		t.Fatalf("fg = %+v, want the named Yellow", fg)
	}
	if rgb, ok := fg.RGBA(); !ok || rgb != (theme.RGB{R: 205, G: 205, B: 0}) {
		t.Fatalf("Yellow fixed RGB = %+v, %v; want #CDCD00", rgb, ok)
	}
	bg, _ := th.Value("editor", "bg")
	if !bg.IsTerminalDefault() {
		t.Fatalf("bg = %+v, want the terminal default", bg)
	}
	if !th.Dark() {
		t.Error("a Default background should count as dark")
	}
}

func TestWithOverrides(t *testing.T) {
	base, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	over, err := theme.WithOverrides(base, map[string]string{
		"editor.bg":                "#112233",
		"ui.popup_fg":              "#445566",
		"ui.semantic_highlight_bg": "#778899",
	})
	if err != nil {
		t.Fatalf("WithOverrides error = %v", err)
	}
	bg, _ := over.Value("editor", "bg")
	if !bg.IsRGB || bg.RGB != (theme.RGB{R: 0x11, G: 0x22, B: 0x33}) {
		t.Fatalf("overridden bg = %+v", bg)
	}
	if _, ok := over.Value("ui", "popup_text_fg"); !ok {
		t.Fatal("alias override did not reach popup_text_fg")
	}
	if !over.Dark() {
		t.Error("the overridden #112233 background should be dark")
	}
	if _, err := theme.WithOverrides(base, map[string]string{"ui.nope": "#000000"}); err == nil {
		t.Fatal("unknown key override accepted")
	}
	if _, err := theme.WithOverrides(base, map[string]string{"editor.bg": "not-a-color"}); err == nil {
		t.Fatal("bad color override accepted")
	}
	light, err := theme.WithOverrides(base, map[string]string{"editor.bg": "#FAF0DC"})
	if err != nil {
		t.Fatal(err)
	}
	if light.Dark() {
		t.Error("#FAF0DC override should be light")
	}
}

func TestMarshalFileRoundTrips(t *testing.T) {
	original, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	data, err := original.MarshalFile("my-copy")
	if err != nil {
		t.Fatalf("MarshalFile error = %v", err)
	}
	file, err := theme.ParseFile(data)
	if err != nil {
		t.Fatalf("ParseFile of the saved theme = %v", err)
	}
	saved, err := theme.Resolve(file)
	if err != nil {
		t.Fatalf("Resolve of the saved theme = %v", err)
	}
	if saved.Name != "my-copy" {
		t.Errorf("name = %q, want my-copy", saved.Name)
	}
	for _, key := range theme.Keys() {
		before, ok := original.Value(key.Section, key.Key)
		if !ok {
			continue
		}
		after, ok := saved.Value(key.Section, key.Key)
		if !ok || before.IsRGB != after.IsRGB || before.RGB != after.RGB || before.Name != after.Name || !slices.Equal(before.Modifiers, after.Modifiers) {
			t.Fatalf("%s: saved %+v, want %+v", key.Name(), after, before)
		}
	}
}

func TestRegistryUserThemesAndReload(t *testing.T) {
	dir := t.TempDir()
	r := theme.NewRegistry(dir)
	if got := len(r.Infos()); got != 10 {
		t.Fatalf("Infos = %d, want the 10 bundled themes", got)
	}
	if _, err := r.Load("dark"); err != nil {
		t.Fatalf("Load(dark) = %v", err)
	}
	if _, err := r.Load("builtin://light"); err != nil {
		t.Fatalf("Load(builtin://light) = %v", err)
	}
	if _, err := r.Load("nope"); err == nil {
		t.Fatal("unknown theme loaded")
	}

	path := filepath.Join(dir, "my-theme.json")
	mustWrite(t, path, `{"name":"my-theme","extends":"builtin://dark","editor":{"cursor":[1,2,3]}}`)
	if err := r.Reload(); err != nil {
		t.Fatalf("Reload error = %v", err)
	}
	if !slices.Contains(r.Names(), "my-theme") {
		t.Fatalf("Names = %v, want the user theme", r.Names())
	}
	for _, ref := range []string{"my-theme", "my-theme.json"} {
		th, err := r.Load(ref)
		if err != nil {
			t.Fatalf("Load(%q) = %v", ref, err)
		}
		if c, _ := th.Value("editor", "cursor"); c.RGB != (theme.RGB{R: 1, G: 2, B: 3}) {
			t.Fatalf("Load(%q) cursor = %+v", ref, c)
		}
	}
	canonical, err := r.Canonical("my-theme")
	if err != nil || canonical != "my-theme.json" {
		t.Fatalf("Canonical = %q, %v; want my-theme.json", canonical, err)
	}

	mustWrite(t, path, `{"name":"my-theme","extends":"builtin://dark","editor":{"cursor":[9,9,9]}}`)
	if err := r.Reload(); err != nil {
		t.Fatal(err)
	}
	th, err := r.Load("my-theme")
	if err != nil {
		t.Fatal(err)
	}
	if c, _ := th.Value("editor", "cursor"); c.RGB != (theme.RGB{R: 9, G: 9, B: 9}) {
		t.Fatalf("reloaded cursor = %+v, want the edited value", c)
	}

	edited, err := theme.Load("light")
	if err != nil {
		t.Fatal(err)
	}
	edited, err = theme.WithOverrides(edited, map[string]string{"editor.cursor": "#ABCDEF"})
	if err != nil {
		t.Fatal(err)
	}
	savePath, err := r.ThemePath("saved-theme")
	if err != nil {
		t.Fatalf("ThemePath = %v", err)
	}
	data, err := edited.MarshalFile("saved-theme")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(savePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(savePath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.Reload(); err != nil {
		t.Fatal(err)
	}
	saved, err := r.Load("saved-theme")
	if err != nil {
		t.Fatalf("Load(saved-theme) = %v", err)
	}
	if c, _ := saved.Value("editor", "cursor"); c.RGB != (theme.RGB{R: 0xAB, G: 0xCD, B: 0xEF}) {
		t.Fatalf("saved cursor = %+v", c)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	absolute := filepath.Join(home, "outside.json")
	mustWrite(t, absolute, `{"name":"outside","extends":"builtin://dark"}`)
	if _, err := r.Load("file://${HOME}/outside.json"); err != nil {
		t.Fatalf("file:// with HOME = %v", err)
	}
	if _, err := r.Load("file://" + absolute); err != nil {
		t.Fatalf("file:// absolute = %v", err)
	}
	if _, err := r.Load("../outside.json"); err == nil {
		t.Fatal("a path escaping the theme dir was accepted")
	}

	if err := r.DeleteTheme("saved-theme"); err != nil {
		t.Fatalf("DeleteTheme = %v", err)
	}
	if _, err := os.Stat(savePath); !os.IsNotExist(err) {
		t.Fatalf("the theme file still exists: %v", err)
	}
	if err := r.DeleteTheme("dark"); err == nil {
		t.Fatal("a bundled theme was deleted")
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// sameColor compares two theme values, attributes included.
func sameColor(a, b theme.Color) bool {
	return a.IsRGB == b.IsRGB && a.RGB == b.RGB && a.Name == b.Name && slices.Equal(a.Modifiers, b.Modifiers)
}
