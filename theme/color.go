package theme

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// namedRGB are the fixed RGB values Fresh gives to the named ratatui colors
// (color_to_rgb in refs/fresh theme/types.rs). The terminal palette is still
// what a named color is drawn with; these numbers are for luminance choices
// (auto-base, dark/light), inspect and editor swatches, and plugin reads.
var namedRGB = map[string]RGB{
	"Black":        {R: 0, G: 0, B: 0},
	"Red":          {R: 205, G: 0, B: 0},
	"Green":        {R: 0, G: 205, B: 0},
	"Blue":         {R: 0, G: 0, B: 238},
	"Yellow":       {R: 205, G: 205, B: 0},
	"Magenta":      {R: 205, G: 0, B: 205},
	"Cyan":         {R: 0, G: 205, B: 205},
	"Gray":         {R: 229, G: 229, B: 229},
	"DarkGray":     {R: 127, G: 127, B: 127},
	"LightRed":     {R: 255, G: 0, B: 0},
	"LightGreen":   {R: 0, G: 255, B: 0},
	"LightBlue":    {R: 92, G: 92, B: 255},
	"LightYellow":  {R: 255, G: 255, B: 0},
	"LightMagenta": {R: 255, G: 0, B: 255},
	"LightCyan":    {R: 0, G: 255, B: 255},
	"White":        {R: 255, G: 255, B: 255},
}

// noColorNames are the spellings of the terminal's own default color.
var noColorNames = map[string]bool{"Default": true, "Reset": true}

// NamedRGB returns the fixed RGB value of a Fresh named color. It is false for
// the terminal default and for a name the theme format does not know.
func NamedRGB(name string) (RGB, bool) {
	rgb, ok := namedRGB[name]
	return rgb, ok
}

// NamedColorNames returns the named colors a theme value may use, sorted.
func NamedColorNames() []string {
	names := make([]string, 0, len(namedRGB)+2)
	for name := range namedRGB {
		names = append(names, name)
	}
	names = append(names, "Default", "Reset")
	sort.Strings(names)
	return names
}

// RGBA returns the fixed RGB of a color. It is false for the terminal default
// and for an unknown name.
func (c Color) RGBA() (RGB, bool) {
	if c.IsRGB {
		return c.RGB, true
	}
	return NamedRGB(c.Name)
}

// Hex returns the display form the theme editor and inspect popup use: the
// #RRGGBB of an RGB or fixed-RGB named color, else the name ("Default").
func (c Color) Hex() string {
	if rgb, ok := c.RGBA(); ok {
		return fmt.Sprintf("#%02X%02X%02X", rgb.R, rgb.G, rgb.B)
	}
	if c.Name != "" {
		return c.Name
	}
	return "Default"
}

// Luminance is the sRGB relative luminance of rgb (ITU-R BT.709), 0..1.
func Luminance(rgb RGB) float64 {
	return 0.2126*float64(rgb.R)/255.0 + 0.7152*float64(rgb.G)/255.0 + 0.0722*float64(rgb.B)/255.0
}

// Dark reports whether the theme's effective editor background is dark. The
// terminal default and an unresolvable background count as dark, as Fresh's
// is_light does.
func (t Theme) Dark() bool {
	if t.darkSet {
		return t.dark
	}
	return true
}

// ParseColorText parses a color as the theme editor and plugin overrides write
// it: "#RRGGBB", a Fresh named color ("Red"), or "Default"/"Reset". It does not
// accept attribute bundles.
func ParseColorText(text string) (Color, error) {
	s := strings.TrimSpace(text)
	if s == "" {
		return Color{}, fmt.Errorf("a color is required")
	}
	if strings.HasPrefix(s, "#") {
		hex := strings.TrimPrefix(s, "#")
		if len(hex) != 6 {
			return Color{}, fmt.Errorf("a hex color is #RRGGBB, got %q", text)
		}
		var rgb RGB
		n, err := strconv.ParseUint(hex, 16, 32)
		if err != nil {
			return Color{}, fmt.Errorf("a hex color is #RRGGBB, got %q", text)
		}
		rgb = RGB{R: uint8(n >> 16), G: uint8(n >> 8), B: uint8(n)}
		return Color{RGB: rgb, IsRGB: true}, nil
	}
	// Accept the lowercase and spaced spellings too, but keep the canonical name.
	for name := range namedRGB {
		if strings.EqualFold(name, s) {
			return Color{Name: name}, nil
		}
	}
	for name := range noColorNames {
		if strings.EqualFold(name, s) {
			return Color{Name: name}, nil
		}
	}
	switch strings.ToLower(s) {
	case "default", "reset":
		return Color{Name: "Default"}, nil
	}
	return Color{}, fmt.Errorf("unknown color %q (want #RRGGBB, a theme color name or Default)", text)
}

// ModifierNames are the text attributes a theme value may carry, in the order
// the theme editor shows them.
var ModifierNames = []string{"bold", "italic", "underlined", "dim", "reversed"}

// NormalizeModifiers canonicalizes attribute names (Fresh's aliases) and drops
// unknown ones, so a typo cannot break a render.
func NormalizeModifiers(names []string) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "bold":
			out = append(out, "bold")
		case "italic":
			out = append(out, "italic")
		case "underlined", "underline":
			out = append(out, "underlined")
		case "dim", "dimmed", "faint":
			out = append(out, "dim")
		case "reversed", "reverse":
			out = append(out, "reversed")
		}
	}
	return out
}

// ParseModifiers parses the theme editor's comma-separated attribute list.
func ParseModifiers(text string) ([]string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, nil
	}
	parts := strings.Split(text, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		name := strings.ToLower(strings.TrimSpace(part))
		if name == "" {
			continue
		}
		normalized := NormalizeModifiers([]string{name})
		if len(normalized) == 0 {
			return nil, fmt.Errorf("unknown text attribute %q (want bold, italic, underlined, dim or reversed)", strings.TrimSpace(part))
		}
		out = append(out, normalized[0])
	}
	return out, nil
}

// FormatModifiers is the theme editor form of an attribute list.
func FormatModifiers(names []string) string { return strings.Join(NormalizeModifiers(names), ",") }

// ParseColorJSON parses one theme-file value: an [r,g,b] array, a named color
// string, a {"color":..., "modifier":[...]} bundle, or a bare modifier list
// (kept for the file's attribute-only keys).
func ParseColorJSON(raw json.RawMessage) (Color, error) {
	var mods []string
	if err := json.Unmarshal(raw, &mods); err == nil {
		return Color{Modifiers: NormalizeModifiers(mods)}, nil
	}
	var nums []int
	if err := json.Unmarshal(raw, &nums); err == nil {
		if len(nums) != 3 {
			return Color{}, fmt.Errorf("array of %d numbers is not a color", len(nums))
		}
		rgb, err := toRGB(nums)
		if err != nil {
			return Color{}, err
		}
		return Color{RGB: rgb, IsRGB: true}, nil
	}
	var name string
	if err := json.Unmarshal(raw, &name); err == nil {
		return colorFromName(name)
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
		return Color{Modifiers: NormalizeModifiers(obj.Modifier)}, nil
	}
	c, err := ParseColorJSON(obj.Color)
	if err != nil {
		return Color{}, err
	}
	c.Modifiers = NormalizeModifiers(obj.Modifier)
	return c, nil
}

// colorFromName parses a named color value. A name the format does not know is
// an error: Fresh warns and falls back to White, but gvim rejects the theme
// with a diagnostic so a typo is found.
func colorFromName(name string) (Color, error) {
	if noColorNames[name] {
		return Color{Name: "Default"}, nil
	}
	if _, ok := namedRGB[name]; ok {
		return Color{Name: name}, nil
	}
	return Color{}, fmt.Errorf("unknown color name %q", name)
}

func toRGB(v []int) (RGB, error) {
	for _, n := range v {
		if n < 0 || n > 255 {
			return RGB{}, fmt.Errorf("component %d out of range 0..255", n)
		}
	}
	return RGB{R: uint8(v[0]), G: uint8(v[1]), B: uint8(v[2])}, nil
}

// ColorJSON is the theme-file value of a color: its bare form, or a bundle when
// it carries attributes.
func ColorJSON(c Color) json.RawMessage {
	body := colorBodyJSON(c)
	if len(c.Modifiers) == 0 {
		return body
	}
	bundle := map[string]any{"color": body, "modifier": c.Modifiers}
	encoded, _ := json.Marshal(bundle)
	return encoded
}

// colorBodyJSON is the bare value of a color, without attributes.
func colorBodyJSON(c Color) json.RawMessage {
	if c.IsRGB {
		encoded, _ := json.Marshal([]int{int(c.RGB.R), int(c.RGB.G), int(c.RGB.B)})
		return encoded
	}
	if c.Name == "" || c.Name == "Default" || c.Name == "Reset" {
		encoded, _ := json.Marshal("Default")
		return encoded
	}
	encoded, _ := json.Marshal(c.Name)
	return encoded
}
