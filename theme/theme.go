// Package theme loads the color themes shipped with Fresh
// (crates/fresh-editor-core/themes/*.json) and the user themes in
// config.Dir()/themes, and resolves them into complete themes for the TUI.
//
// A theme file has sections (editor, ui, search, diagnostic, syntax). A value is
// one of: an [r, g, b] array, a color name the format knows ("Yellow",
// "Default"), an object {"color": [r,g,b], "modifier": [...]}, or a list of style
// modifiers. A theme may extend a built-in theme, and the colors it leaves out
// are filled from the base or from their fallback chain (see keys_table.go and
// docs/themes.md).
package theme

import (
	"embed"
	"encoding/json"
	"slices"
	"sort"
	"strings"
)

//go:embed data/*.json
var files embed.FS

// RGB is a 24-bit color.
type RGB struct{ R, G, B uint8 }

// Color is one resolved theme value. Exactly one of IsRGB or Name is set, unless
// the value names attributes alone (a bare modifier list).
type Color struct {
	RGB   RGB
	Name  string // the named color, "Default" for the terminal's own color
	IsRGB bool
	// Modifiers are the text attributes attached to the value ("bold",
	// "reversed", ...).
	Modifiers []string
}

// Theme is a loaded and resolved color theme: every color key it can have has
// been filled from its base theme or its fallback chain.
type Theme struct {
	Name     string
	sections map[string]map[string]Color
	dark     bool
	darkSet  bool
	warnings []string
}

// Names returns the names of the bundled themes, sorted. Pass one to Load.
func Names() []string { return EmbeddedNames() }

// Load returns the bundled theme called name, for example "dark". The name must
// match Names exactly; an unknown name is an error. User themes and the config
// forms are on Registry.
func Load(name string) (Theme, error) { return EmbeddedTheme(name) }

// Parse decodes a standalone theme file in Fresh's schema and resolves it.
// Unknown color keys are warnings, a value the format cannot read is an error.
func Parse(data []byte) (Theme, error) {
	file, err := ParseFile(data)
	if err != nil {
		return Theme{}, err
	}
	return Resolve(file)
}

// Value returns the resolved value for section and key. ok is false when either
// is missing. It never panics, including on a zero Theme.
func (t Theme) Value(section, key string) (Color, bool) {
	c, ok := t.sections[section][key]
	return c, ok
}

// RGBOr returns the RGB for section and key, or fallback when the key is
// missing, names a terminal color, or has no RGB value.
func (t Theme) RGBOr(section, key string, fallback RGB) RGB {
	c, ok := t.Value(section, key)
	if !ok || !c.IsRGB {
		return fallback
	}
	return c.RGB
}

// Sections returns the section names of the theme, sorted.
func (t Theme) Sections() []string {
	out := make([]string, 0, len(t.sections))
	for s := range t.sections {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// Keys returns the color keys the theme actually has a value for, in table
// order. The theme editor and inspect use it.
func (t Theme) Keys() []Key {
	out := make([]Key, 0, len(themeKeys))
	for _, key := range themeKeys {
		if _, ok := t.Value(key.Section, key.Key); ok {
			out = append(out, key)
		}
	}
	return out
}

// JSON encodes a theme value for the file format.
func (c Color) JSON() json.RawMessage { return ColorJSON(c) }

// String is the display form of the color ("#RRGGBB", "Default", "Red").
func (c Color) String() string { return strings.TrimSpace(c.Hex()) }

// WithoutColor returns a copy of the theme with all color values stripped to
// terminal default while preserving styling attributes and adding reverse-video
// or underline attributes to selections, active highlights, and search matches.
func (t Theme) WithoutColor() Theme {
	if len(t.sections) == 0 {
		return t
	}
	sections := make(map[string]map[string]Color, len(t.sections))
	for sec, keys := range t.sections {
		dst := make(map[string]Color, len(keys))
		for k, c := range keys {
			name := "Default"
			if isModifierOnly(sec + "." + k) {
				name = ""
			}
			dst[k] = Color{
				Name:      name,
				Modifiers: NormalizeModifiers(c.Modifiers),
			}
		}
		sections[sec] = dst
	}
	addMod := func(sec, key, mod string) {
		m := sections[sec]
		if m == nil {
			m = make(map[string]Color)
			sections[sec] = m
		}
		c, ok := m[key]
		if !ok {
			if isModifierOnly(sec + "." + key) {
				c = Color{}
			} else {
				c = Color{Name: "Default"}
			}
		}
		if !slices.Contains(c.Modifiers, mod) {
			c.Modifiers = append(slices.Clone(c.Modifiers), mod)
		}
		m[key] = c
	}
	for _, pair := range [][2]string{
		{"editor", "selection_bg"},
		{"ui", "accent_bg"},
		{"ui", "text_input_selection_bg"},
		{"ui", "popup_selection_bg"},
		{"ui", "menu_highlight_bg"},
		{"ui", "settings_selected_bg"},
		{"ui", "prompt_selection_bg"},
		{"ui", "suggestion_selected_bg"},
		{"search", "current_match_bg"},
	} {
		addMod(pair[0], pair[1], "reversed")
	}
	addMod("search", "match_bg", "underlined")
	addMod("ui", "semantic_highlight_bg", "underlined")
	addMod("editor", "selection_modifier", "reversed")
	addMod("ui", "semantic_highlight_modifier", "underlined")
	return Theme{
		Name:     t.Name,
		sections: sections,
		dark:     t.dark,
		darkSet:  t.darkSet,
		warnings: slices.Clone(t.warnings),
	}
}
