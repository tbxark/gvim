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
