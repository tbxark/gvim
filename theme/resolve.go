package theme

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// Resolve resolves one parsed theme file into a complete theme: its base (an
// explicit builtin extends, or the light/dark implicit base of a partial
// theme), its named values, and the fallback chains for the colors it leaves
// out. Sources: refs/fresh theme/types.rs resolve_base_theme, from_json,
// fill_fallbacks and apply_theme_overrides.
func Resolve(file ThemeFile) (Theme, error) {
	return resolveWith(file, EmbeddedTheme)
}

// resolveWith is Resolve with a builtin provider, so tests and a registry can
// resolve without the embedded data.
func resolveWith(file ThemeFile, builtin func(name string) (Theme, error)) (Theme, error) {
	base, hasBase, err := baseTheme(file, builtin)
	if err != nil {
		return Theme{}, err
	}
	var out Theme
	if hasBase {
		out = base.Clone()
	} else {
		out = Theme{sections: make(map[string]map[string]Color)}
	}
	if file.Name != "" {
		out.Name = file.Name
	} else if hasBase {
		out.Name = base.Name
	}
	out.warnings = append(out.warnings, file.Warnings...)
	out.applyValues(file, hasBase)
	if !hasBase {
		fillFallbacks(&out, file)
	}
	out.applyModifierOnly(file)
	// A modifier-only key the theme does not name keeps Fresh's default (the
	// bracket pair's bold and underlined).
	for name, mods := range defaultModifierOnly {
		if _, ok := out.value(name); !ok {
			out.setValue(name, Color{Modifiers: NormalizeModifiers(mods)})
		}
	}
	out.updateDark()
	return out, nil
}

// baseTheme chooses the base of a file: an explicit builtin extends; else a
// standalone theme when every required color is named; else the implicit
// builtin://light or builtin://dark by the luminance of editor.bg.
func baseTheme(file ThemeFile, builtin func(string) (Theme, error)) (Theme, bool, error) {
	if file.Extends != "" {
		name := strings.TrimPrefix(file.Extends, "builtin://")
		base, err := builtin(name)
		if err != nil {
			return Theme{}, false, fmt.Errorf(
				"extends: %q does not name a built-in theme. Inheriting from other user themes is not supported; built-ins: %s",
				file.Extends, strings.Join(EmbeddedNames(), ", "))
		}
		return base, true, nil
	}
	if !file.needsBase() {
		return Theme{}, false, nil
	}
	name := "dark"
	if bg, ok := file.Values["editor.bg"]; ok {
		if rgb, ok := bg.RGBA(); ok && Luminance(rgb) > 0.5 {
			name = "light"
		}
	}
	base, err := builtin(name)
	if err != nil {
		return Theme{}, false, err
	}
	return base, true, nil
}

// applyValues overlays the colors the file names onto out. A bare color clears
// the attributes of a bundle on the same key, except on the two keys whose
// attributes a separate attribute-only key also names: there a bare value keeps
// the base's attributes, as Fresh's apply_theme_overrides does.
func (t *Theme) applyValues(file ThemeFile, hasBase bool) {
	for name, color := range file.Values {
		key, ok := KeyByName(name)
		if !ok {
			continue
		}
		if hasBase && file.Bare[name] && hasModifierOnlyKey(key.Name()) {
			if old, ok := t.value(key.Name()); ok {
				color.Modifiers = NormalizeModifiers(old.Modifiers)
			}
		} else {
			color.Modifiers = NormalizeModifiers(color.Modifiers)
		}
		t.setValue(key.Name(), color)
	}
}

// fillFallbacks gives every color a standalone theme leaves out the value of the
// first key along its fallback chain that the file names.
func fillFallbacks(t *Theme, file ThemeFile) {
	for _, key := range themeKeys {
		if key.Kind != FallbackKey {
			continue
		}
		if _, named := file.Values[key.Name()]; named {
			continue
		}
		source := key.Fallback
		for source != "" {
			if _, named := file.Values[source]; named {
				break
			}
			sourceKey, ok := KeyByName(source)
			if !ok || sourceKey.Kind != FallbackKey {
				break
			}
			source = sourceKey.Fallback
		}
		if source == "" || source == key.Name() {
			continue
		}
		if color, ok := t.value(source); ok {
			t.setValue(key.Name(), color)
		}
	}
}

// applyModifierOnly applies the attribute-only keys the file names: they replace
// the attributes of their color key (selection_bg and semantic_highlight_bg) and
// are also readable under their own key.
func (t *Theme) applyModifierOnly(file ThemeFile) {
	for name, mods := range file.Modifiers {
		t.setValue(name, Color{Modifiers: NormalizeModifiers(mods)})
		if target, ok := modifierTarget(name); ok {
			if color, ok := t.value(target); ok {
				color.Modifiers = NormalizeModifiers(mods)
				t.setValue(target, color)
			}
		}
	}
}

// hasModifierOnlyKey reports whether an attribute-only key also names the
// attributes of color key: editor.selection_bg and ui.semantic_highlight_bg.
func hasModifierOnlyKey(name string) bool {
	switch name {
	case "editor.selection_bg", "ui.semantic_highlight_bg":
		return true
	}
	return false
}

// updateDark sets the theme's light/dark flag from the effective editor
// background's luminance.
func (t *Theme) updateDark() {
	t.dark, t.darkSet = true, true
	bg, ok := t.Value("editor", "bg")
	if !ok {
		return
	}
	rgb, ok := bg.RGBA()
	if !ok {
		return // the terminal default counts as dark
	}
	t.dark = Luminance(rgb) <= 0.5
}

// setValue stores one color under "section.key".
func (t *Theme) setValue(name string, color Color) {
	section, key, ok := strings.Cut(name, ".")
	if !ok {
		return
	}
	if t.sections == nil {
		t.sections = make(map[string]map[string]Color)
	}
	values := t.sections[section]
	if values == nil {
		values = make(map[string]Color)
		t.sections[section] = values
	}
	values[key] = color
}

// value returns the color stored under "section.key".
func (t Theme) value(name string) (Color, bool) {
	section, key, ok := strings.Cut(name, ".")
	if !ok {
		return Color{}, false
	}
	c, ok := t.sections[section][key]
	return c, ok
}

// Clone returns a deep copy of the theme.
func (t Theme) Clone() Theme {
	out := Theme{
		Name:     t.Name,
		sections: make(map[string]map[string]Color, len(t.sections)),
		dark:     t.dark,
		darkSet:  t.darkSet,
		warnings: append([]string(nil), t.warnings...),
	}
	for section, values := range t.sections {
		copied := make(map[string]Color, len(values))
		for key, color := range values {
			color.Modifiers = append([]string(nil), color.Modifiers...)
			copied[key] = color
		}
		out.sections[section] = copied
	}
	return out
}

// Warnings returns the problems a theme file had when it was parsed (unknown
// keys). A warning does not stop the theme from loading.
func (t Theme) Warnings() []string { return append([]string(nil), t.warnings...) }

// EmbeddedNames returns the names of the bundled themes, sorted.
func EmbeddedNames() []string {
	entries, err := files.ReadDir("data")
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		names = append(names, strings.TrimSuffix(entry.Name(), path.Ext(entry.Name())))
	}
	sort.Strings(names)
	return names
}

// EmbeddedTheme returns the resolved bundled theme called name.
func EmbeddedTheme(name string) (Theme, error) {
	data, err := files.ReadFile(path.Join("data", name+".json"))
	if err != nil {
		return Theme{}, fmt.Errorf("theme: unknown theme %q", name)
	}
	file, err := ParseFile(data)
	if err != nil {
		return Theme{}, err
	}
	// Bundled themes are standalone; a recursive extends would be a bug in the
	// bundled data, so refuse it instead of looping.
	if file.Extends != "" {
		return Theme{}, fmt.Errorf("theme %q: bundled themes cannot extend another theme", name)
	}
	return resolveWith(file, func(string) (Theme, error) {
		return Theme{}, fmt.Errorf("bundled theme %q extends another theme", name)
	})
}
