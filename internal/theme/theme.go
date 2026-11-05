// Package theme loads the color themes shipped with Fresh
// (crates/fresh-editor-core/themes/*.json) and resolves named theme keys to
// colors for the TUI.
//
// A theme file has sections (editor, ui, search, diagnostic, syntax). A value
// is one of: an [r, g, b] array, a color name that ratatui understands
// ("Yellow", "DarkGray"), an object {"color": [r,g,b], "modifier": [...]}, or
// a list of style modifiers. Only RGB arrays and the RGB form of the object
// yield an RGB; the rest are kept as Color values with IsRGB false.
package theme

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

//go:embed data/*.json
var files embed.FS

// RGB is a 24-bit color.
type RGB struct{ R, G, B uint8 }

// Color is one resolved theme value. Exactly one of IsRGB or Name is set.
type Color struct {
	RGB   RGB
	Name  string // ratatui color name, when the theme used one ("Yellow")
	IsRGB bool
	// Modifiers are style modifiers attached to the value ("bold", "reversed").
	Modifiers []string
}

// Theme is a loaded color theme.
type Theme struct {
	Name     string
	sections map[string]map[string]Color
}

// Names returns the names of the bundled themes, sorted. Pass one to Load.
func Names() []string {
	entries, err := fs.ReadDir(files, "data")
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		names = append(names, strings.TrimSuffix(e.Name(), path.Ext(e.Name())))
	}
	sort.Strings(names)
	return names
}

// Load returns the bundled theme called name, for example "dark". The name
// must match Names exactly; an unknown name is an error.
func Load(name string) (Theme, error) {
	data, err := files.ReadFile(path.Join("data", name+".json"))
	if err != nil {
		return Theme{}, fmt.Errorf("theme: unknown theme %q", name)
	}
	return Parse(data)
}

// Parse decodes a theme file in Fresh's schema.
func Parse(data []byte) (Theme, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return Theme{}, fmt.Errorf("theme: parse: %w", err)
	}
	t := Theme{sections: make(map[string]map[string]Color)}
	if n, ok := raw["name"]; ok {
		if err := json.Unmarshal(n, &t.Name); err != nil {
			return Theme{}, fmt.Errorf("theme: name: %w", err)
		}
	}
	for section, body := range raw {
		if section == "name" {
			continue
		}
		var values map[string]json.RawMessage
		if err := json.Unmarshal(body, &values); err != nil {
			return Theme{}, fmt.Errorf("theme: section %q: %w", section, err)
		}
		colors := make(map[string]Color, len(values))
		for key, v := range values {
			c, err := parseValue(v)
			if err != nil {
				return Theme{}, fmt.Errorf("theme: %s.%s: %w", section, key, err)
			}
			colors[key] = c
		}
		t.sections[section] = colors
	}
	return t, nil
}

// parseValue decodes one theme value. Unrecognized shapes are an error so a
// broken theme file is reported at load time, not as a missing color later.
func parseValue(raw json.RawMessage) (Color, error) {
	var nums []int
	if err := json.Unmarshal(raw, &nums); err == nil {
		if len(nums) != 3 {
			return Color{}, fmt.Errorf("array of %d numbers is not a color", len(nums))
		}
		c, err := toRGB(nums)
		return Color{RGB: c, IsRGB: true}, err
	}
	var mods []string
	if err := json.Unmarshal(raw, &mods); err == nil {
		return Color{Modifiers: mods}, nil
	}
	var name string
	if err := json.Unmarshal(raw, &name); err == nil {
		return Color{Name: name}, nil
	}
	var obj struct {
		Color    json.RawMessage `json:"color"`
		Modifier []string        `json:"modifier"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return Color{}, fmt.Errorf("unrecognized value: %w", err)
	}
	switch {
	case obj.Color == nil && obj.Modifier == nil:
		return Color{}, fmt.Errorf("object has neither color nor modifier")
	case obj.Color == nil:
		return Color{Modifiers: obj.Modifier}, nil
	}
	c, err := parseValue(obj.Color)
	if err != nil {
		return Color{}, err
	}
	c.Modifiers = obj.Modifier
	return c, nil
}

func toRGB(v []int) (RGB, error) {
	for _, n := range v {
		if n < 0 || n > 255 {
			return RGB{}, fmt.Errorf("component %d out of range 0..255", n)
		}
	}
	return RGB{R: uint8(v[0]), G: uint8(v[1]), B: uint8(v[2])}, nil
}

// Value returns the raw value for section and key. ok is false when either is
// missing. It never panics, including on a zero Theme.
func (t Theme) Value(section, key string) (Color, bool) {
	c, ok := t.sections[section][key]
	return c, ok
}

// RGBOr returns the RGB for section and key, or fallback when the key is
// missing, names a terminal color, or has no RGB value. The TUI uses this so a
// theme that is missing a key still renders.
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
