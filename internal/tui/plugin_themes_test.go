package tui

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/theme"
)

// TestThemeFileHostAPI checks the theme-file surface the theme editor and pickers use over
// the ThemeCatalog port: listing, schema, data, save, delete and reload-and-apply.
func TestThemeFileHostAPI(t *testing.T) {
	t.Parallel()
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	cat := &fakeThemeCatalog{
		themes: map[string]theme.Theme{"mine.json": th},
		refs:   map[string]string{"mine.json": "mine"},
	}
	m := newThemeTestModel(t, cat)
	h := &pluginHost{m: m}

	files := h.ThemeFiles()
	if len(files) != 1 || files[0].Name != "mine" || files[0].Ref != "mine.json" || files[0].Builtin {
		t.Fatalf("files = %+v", files)
	}
	schema := h.ThemeSchema()
	if len(schema.Keys) == 0 || len(schema.ModifierOnly) == 0 || len(schema.NamedColors) == 0 {
		t.Fatalf("schema = %+v", schema)
	}
	found := false
	for _, k := range schema.Keys {
		if k.Name == "editor.bg" && k.Kind == "required" {
			found = true
		}
	}
	if !found {
		t.Fatalf("schema has no editor.bg required key: %+v", schema.Keys[:5])
	}
	if got := h.ThemeUserDir(); got != "" {
		t.Fatalf("user dir = %q", got)
	}
	data, err := h.ThemeData("mine.json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(data, "editor") {
		t.Fatalf("data = %q, want theme-file JSON", data)
	}
	ref, err := h.SaveThemeFile("copy", data)
	if err != nil {
		t.Fatal(err)
	}
	if ref != "copy.json" {
		t.Fatalf("saved ref = %q", ref)
	}
	if _, err := h.ThemeData("copy.json"); err != nil {
		t.Fatalf("the saved theme is not readable: %v", err)
	}
	if err := h.DeleteThemeFile("copy.json"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.SaveThemeFile("bad", "{not json"); err == nil {
		t.Fatal("invalid theme data was accepted")
	}
	if err := h.ReloadThemes("mine.json"); err != nil {
		t.Fatal(err)
	}
	if cat.reloads != 1 {
		t.Fatalf("reloads = %d", cat.reloads)
	}
	if got := m.theme.Name; got != th.Name {
		t.Fatalf("applied theme = %q, want %q", got, th.Name)
	}
}
