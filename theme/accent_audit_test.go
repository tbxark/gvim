package theme

import (
	"encoding/json"
	"testing"
)

// TestBundledThemesAccentIsPopupSelection pins the accent audit: every bundled
// theme's gvim extension colors ui.accent_bg/ui.accent_fg are exactly its popup
// selection colors, so the accent surfaces (status highlights, menu and modal
// highlights, explorer/diagnostics markers) are driven by the same colors Fresh
// uses for a selected popup row.
func TestBundledThemesAccentIsPopupSelection(t *testing.T) {
	t.Parallel()
	for _, name := range EmbeddedNames() {
		th, err := Load(name)
		if err != nil {
			t.Fatalf("Load(%q): %v", name, err)
		}
		for _, pair := range [][2]string{
			{"accent_bg", "popup_selection_bg"},
			{"accent_fg", "popup_selection_fg"},
		} {
			accent, ok := th.Value("ui", pair[0])
			if !ok {
				t.Errorf("%s: ui.%s is missing", name, pair[0])
				continue
			}
			selection, ok := th.Value("ui", pair[1])
			if !ok {
				t.Errorf("%s: ui.%s is missing", name, pair[1])
				continue
			}
			if accent.String() != selection.String() {
				t.Errorf("%s: ui.%s = %s, want the popup selection %s",
					name, pair[0], accent.String(), selection.String())
			}
			if accent.Modifiers == nil && selection.Modifiers == nil {
				continue
			}
			if !sameModifiers(accent.Modifiers, selection.Modifiers) {
				t.Errorf("%s: ui.%s attributes = %v, want the popup selection's %v",
					name, pair[0], accent.Modifiers, selection.Modifiers)
			}
		}
	}
}

// TestStandaloneThemeFillsAccentFromPopupSelection pins the fallback chain: a
// theme that names the popup selection colors but not gvim's accent keys resolves
// the accent through ui.accent_* -> ui.popup_selection_*.
func TestStandaloneThemeFillsAccentFromPopupSelection(t *testing.T) {
	t.Parallel()
	th, err := Parse(standaloneThemeJSON(t, map[string][3]int{
		"ui.popup_selection_bg": {1, 2, 3},
		"ui.popup_selection_fg": {4, 5, 6},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"accent_bg", "popup_selection_bg"}, {"accent_fg", "popup_selection_fg"}} {
		accent, ok := th.Value("ui", pair[0])
		if !ok {
			t.Fatalf("ui.%s did not resolve through the fallback", pair[0])
		}
		selection, _ := th.Value("ui", pair[1])
		if accent.String() != selection.String() {
			t.Errorf("ui.%s = %s, want the popup selection %s", pair[0], accent.String(), selection.String())
		}
	}
}

// TestAccentKeysAreEditorRows pins the editor/inspect coverage: a resolved theme
// lists the accent keys, so the theme editor can edit them and the inspector can
// name them (no row was missing; this keeps it that way).
func TestAccentKeysAreEditorRows(t *testing.T) {
	t.Parallel()
	th, err := Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, key := range th.Keys() {
		have[key.Name()] = true
	}
	for _, name := range []string{"ui.accent_bg", "ui.accent_fg"} {
		if !have[name] {
			t.Errorf("theme keys do not list %s", name)
		}
	}
}

// sameModifiers reports whether two modifier lists name the same attributes.
func sameModifiers(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]int{}
	for _, m := range a {
		seen[m]++
	}
	for _, m := range b {
		seen[m]--
	}
	for _, n := range seen {
		if n != 0 {
			return false
		}
	}
	return true
}

// standaloneThemeJSON builds a standalone theme file naming every required color
// (one default color) plus overrides, so it needs no base theme.
func standaloneThemeJSON(t *testing.T, overrides map[string][3]int) []byte {
	t.Helper()
	sections := map[string]map[string][]int{}
	set := func(name string, rgb [3]int) {
		section, key := splitKey(name)
		if sections[section] == nil {
			sections[section] = map[string][]int{}
		}
		sections[section][key] = rgb[:]
	}
	for _, name := range RequiredKeys() {
		set(name, [3]int{10, 20, 30})
	}
	for name, rgb := range overrides {
		set(name, rgb)
	}
	doc := map[string]any{"name": "accent-audit"}
	for section, values := range sections {
		doc[section] = values
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// splitKey splits "section.key" (keys never contain a dot).
func splitKey(name string) (section, key string) {
	for i := range name {
		if name[i] == '.' {
			return name[:i], name[i+1:]
		}
	}
	return name, ""
}
