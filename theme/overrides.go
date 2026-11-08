package theme

import (
	"encoding/json"
	"fmt"
)

// WithOverrides layers session color overrides over base: keys are
// "section.key" theme keys (aliases accepted), values are "#RRGGBB", a named
// color, or "Default"/"Reset". A color override keeps the base's text
// attributes; an attribute-only key takes a comma-separated attribute list.
//
// The plugin API (gvim.theme.override) and the theme editor's live preview use
// this, so the layering is the same in both.
func WithOverrides(base Theme, overrides map[string]string) (Theme, error) {
	out := base.Clone()
	if len(overrides) == 0 {
		return out, nil
	}
	for key, text := range overrides {
		name, known := CanonicalKey(key)
		if !known {
			return Theme{}, fmt.Errorf("theme override: %q is not a theme key", key)
		}
		if isModifierOnly(name) {
			mods, err := ParseModifiers(text)
			if err != nil {
				return Theme{}, fmt.Errorf("theme override %s: %w", key, err)
			}
			out.setValue(name, Color{Modifiers: mods})
			if target, ok := modifierTarget(name); ok {
				if color, ok := out.value(target); ok {
					color.Modifiers = mods
					out.setValue(target, color)
				}
			}
			continue
		}
		color, err := ParseColorText(text)
		if err != nil {
			return Theme{}, fmt.Errorf("theme override %s: %w", key, err)
		}
		// Keep the attributes the key already carries; an override names a color.
		if old, ok := out.value(name); ok {
			color.Modifiers = append([]string(nil), old.Modifiers...)
		}
		out.setValue(name, color)
	}
	out.updateDark()
	return out, nil
}

// MarshalFile encodes t as a standalone theme file named name: the resolved
// value of every color key the theme has, with attributes as bundles. It is the
// theme editor's Save format and round-trips through the registry.
func (t Theme) MarshalFile(name string) ([]byte, error) {
	document := map[string]any{"name": name}
	for _, key := range themeKeys {
		if key.Kind == OptionalKey {
			if _, ok := t.Value(key.Section, key.Key); !ok {
				continue
			}
		}
		color, ok := t.Value(key.Section, key.Key)
		if !ok {
			continue
		}
		values, ok := document[key.Section].(map[string]json.RawMessage)
		if !ok {
			values = make(map[string]json.RawMessage)
			document[key.Section] = values
		}
		values[key.Key] = ColorJSON(color)
	}
	// The attribute-only keys without a color of their own keep their value;
	// the two with a color key are already saved on that color's bundle.
	for _, name := range modifierOnlyKeys {
		if _, ok := modifierTarget(name); ok {
			continue
		}
		color, ok := t.value(name)
		if !ok || len(color.Modifiers) == 0 {
			continue
		}
		section, keyName, ok := cutKey(name)
		if !ok {
			continue
		}
		values, ok := document[section].(map[string]json.RawMessage)
		if !ok {
			values = make(map[string]json.RawMessage)
			document[section] = values
		}
		encoded, err := json.Marshal(color.Modifiers)
		if err != nil {
			return nil, err
		}
		values[keyName] = encoded
	}
	return json.MarshalIndent(document, "", "  ")
}

// cutKey splits "section.key".
func cutKey(name string) (section, key string, ok bool) {
	section, key, ok = splitThemeKey(name)
	return section, key, ok
}

// splitThemeKey splits a "section.key" theme key.
func splitThemeKey(name string) (section, key string, ok bool) {
	for i := 0; i < len(name); i++ {
		if name[i] == '.' {
			return name[:i], name[i+1:], true
		}
	}
	return "", "", false
}

// WithValues layers explicit theme values over base. It is the layer the theme
// editor previews with: the editor holds the colors it changed and rebuilds the
// frame from them without writing a file. Keys are canonical "section.key"
// names (aliases accepted); an attribute-only key takes a Color whose Modifiers
// are the attributes.
func WithValues(base Theme, values map[string]Color) (Theme, error) {
	out := base.Clone()
	if len(values) == 0 {
		return out, nil
	}
	for key, value := range values {
		name, known := CanonicalKey(key)
		if !known {
			return Theme{}, fmt.Errorf("theme value: %q is not a theme key", key)
		}
		value.Modifiers = NormalizeModifiers(value.Modifiers)
		out.setValue(name, value)
		if isModifierOnly(name) {
			if target, ok := modifierTarget(name); ok {
				if color, ok := out.value(target); ok {
					color.Modifiers = value.Modifiers
					out.setValue(target, color)
				}
			}
		}
	}
	out.updateDark()
	return out, nil
}
