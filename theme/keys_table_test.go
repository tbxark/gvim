package theme_test

import (
	"slices"
	"testing"

	"github.com/tbxark/gvim/theme"
)

// requiredKeys is the ported list from Fresh commit
// 2338640274915a37786810bd8b59439c6633ba64 (types.rs theme_color_keys: every
// color row without a fallback). The checked-table test fails if the ported
// table drifts from this list.
var requiredKeys = []string{
	"editor.bg", "editor.current_line_bg", "editor.cursor", "editor.fg",
	"editor.line_number_bg", "editor.line_number_fg", "editor.selection_bg",
	"ui.help_bg", "ui.help_fg", "ui.help_indicator_bg", "ui.help_indicator_fg",
	"ui.help_key_fg", "ui.help_separator_fg", "ui.popup_bg", "ui.popup_border_fg",
	"ui.popup_selection_bg", "ui.popup_text_fg", "ui.prompt_bg", "ui.prompt_fg",
	"ui.prompt_selection_bg", "ui.prompt_selection_fg", "ui.split_separator_fg",
	"ui.status_bar_bg", "ui.status_bar_fg", "ui.suggestion_bg",
	"ui.suggestion_selected_bg", "ui.tab_active_bg", "ui.tab_active_fg",
	"ui.tab_inactive_bg", "ui.tab_inactive_fg", "ui.tab_separator_bg",
	"syntax.comment", "syntax.constant", "syntax.function", "syntax.keyword",
	"syntax.operator", "syntax.string", "syntax.type", "syntax.variable",
	"diagnostic.error_bg", "diagnostic.error_fg", "diagnostic.hint_bg",
	"diagnostic.hint_fg", "diagnostic.info_bg", "diagnostic.info_fg",
	"diagnostic.warning_bg", "diagnostic.warning_fg", "search.match_bg",
	"search.match_fg",
}

func TestThemeKeyTableIsComplete(t *testing.T) {
	keys := theme.Keys()
	// 143 from Fresh's table plus gvim's two accent extension keys.
	if len(keys) != 145 {
		t.Fatalf("Keys() has %d keys, want 143 Fresh keys plus the 2 gvim accent keys", len(keys))
	}
	var required, fallback, optional int
	for _, key := range keys {
		switch key.Kind {
		case theme.RequiredKey:
			required++
		case theme.FallbackKey:
			fallback++
		case theme.OptionalKey:
			optional++
		}
		if key.Name() != key.Section+"."+key.Key {
			t.Errorf("key %q has a bad name", key.Name())
		}
	}
	if required != 49 || fallback != 93 || optional != 3 {
		t.Errorf("kinds = %d required, %d fallback, %d optional; want 49/93/3", required, fallback, optional)
	}
	if got := theme.RequiredKeys(); !slices.Equal(got, requiredKeys) {
		t.Fatalf("RequiredKeys drifted from the ported list: got %v, want %v", got, requiredKeys)
	}
}

func TestThemeKeyFallbackChains(t *testing.T) {
	for _, key := range theme.Keys() {
		if key.Kind != theme.FallbackKey {
			continue
		}
		seen := map[string]bool{key.Name(): true}
		source := key.Fallback
		for source != "" {
			next, ok := theme.KeyByName(source)
			if !ok {
				t.Fatalf("%s falls back to %q, which is not a theme key", key.Name(), source)
			}
			if seen[next.Name()] {
				t.Fatalf("%s has a fallback cycle through %q", key.Name(), next.Name())
			}
			seen[next.Name()] = true
			if next.Kind == theme.RequiredKey || next.Kind == theme.OptionalKey {
				break
			}
			source = next.Fallback
		}
		// A chain that ends without a required/optional key is a table bug.
		if _, ok := theme.KeyByName(source); !ok {
			t.Fatalf("%s: chain ended at missing key %q", key.Name(), source)
		}
	}
}

func TestEveryBundledThemeNamesTheRequiredKeys(t *testing.T) {
	for _, name := range theme.Names() {
		loaded, err := theme.Load(name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		for _, key := range requiredKeys {
			section, keyName, _ := cut(key)
			if _, ok := loaded.Value(section, keyName); !ok {
				t.Errorf("%s does not resolve required key %s", name, key)
			}
		}
	}
}

func TestGvimAccentExtensionKeys(t *testing.T) {
	for _, name := range []string{"ui.accent_bg", "ui.accent_fg"} {
		key, ok := theme.KeyByName(name)
		if !ok || key.Kind != theme.FallbackKey {
			t.Fatalf("%s = %+v, %v; want a fallback extension key", name, key, ok)
		}
	}
	if key, _ := theme.KeyByName("ui.accent_bg"); key.Fallback != "ui.popup_selection_bg" {
		t.Errorf("accent_bg fallback = %q, want ui.popup_selection_bg", key.Fallback)
	}
	th, err := theme.Parse([]byte(`{"name":"fresh-like","extends":"builtin://dark","editor":{"cursor":[1,2,3]}}`))
	if err != nil {
		t.Fatalf("Parse = %v", err)
	}
	if _, ok := th.Value("ui", "accent_bg"); !ok {
		t.Fatal("accent_bg did not resolve")
	}
}

func TestThemeKeyAliasesAndModifierOnly(t *testing.T) {
	key, ok := theme.KeyByName("ui.popup_fg")
	if !ok || key.Name() != "ui.popup_text_fg" {
		t.Fatalf("alias ui.popup_fg = %+v, %v", key, ok)
	}
	if _, ok := theme.KeyByName("ui.nope"); ok {
		t.Fatal("unknown key accepted")
	}
	mods := theme.ModifierOnlyKeys()
	if len(mods) != 3 || !slices.Contains(mods, "editor.selection_modifier") {
		t.Fatalf("ModifierOnlyKeys = %v", mods)
	}
}

// cut splits a canonical theme key.
func cut(name string) (section, key string, ok bool) {
	for i := 0; i < len(name); i++ {
		if name[i] == '.' {
			return name[:i], name[i+1:], true
		}
	}
	return "", "", false
}
