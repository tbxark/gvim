package theme

import (
	"encoding/json"
	"fmt"
	"sort"
)

// ThemeFile is one theme file as written on disk, before inheritance and
// fallbacks are resolved: the name, the optional extends, the values the file
// names and its warnings.
type ThemeFile struct {
	Name    string
	Extends string
	// Values are the color values the file names, by canonical "section.key".
	Values map[string]Color
	// Bare is true for a value written as a bare color rather than a
	// {"color","modifier"} bundle.
	Bare map[string]bool
	// Modifiers are the attribute-only keys the file names, by "section.key".
	Modifiers map[string][]string
	// Warnings are the unknown keys the file names. They are ignored, as Fresh
	// ignores them, and reported so a typo is visible.
	Warnings []string
}

// ParseFile decodes one theme file. A value the format cannot read is an error
// naming the key, so a broken theme is reported at load time and not as a
// missing color later. Unknown keys are warnings, not errors.
func ParseFile(data []byte) (ThemeFile, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return ThemeFile{}, fmt.Errorf("theme: parse: %w", err)
	}
	file := ThemeFile{
		Values:    make(map[string]Color),
		Bare:      make(map[string]bool),
		Modifiers: make(map[string][]string),
	}
	if name, ok := raw["name"]; ok {
		if err := json.Unmarshal(name, &file.Name); err != nil {
			return ThemeFile{}, fmt.Errorf("theme: name: %w", err)
		}
	}
	if extends, ok := raw["extends"]; ok {
		if err := json.Unmarshal(extends, &file.Extends); err != nil {
			return ThemeFile{}, fmt.Errorf("theme: extends: %w", err)
		}
	}
	sections := make([]string, 0, len(raw))
	for section := range raw {
		if section == "name" || section == "extends" {
			continue
		}
		sections = append(sections, section)
	}
	sort.Strings(sections)
	for _, section := range sections {
		var values map[string]json.RawMessage
		if err := json.Unmarshal(raw[section], &values); err != nil {
			file.Warnings = append(file.Warnings,
				fmt.Sprintf("theme %q: section %q is not an object of colors; ignored", file.Name, section))
			continue
		}
		for _, key := range sortedKeys(values) {
			if isNullJSON(values[key]) {
				continue // null means "not named", as Fresh's optional fields read it
			}
			name, known := CanonicalKey(section + "." + key)
			if !known {
				file.Warnings = append(file.Warnings,
					fmt.Sprintf("theme %q: `%s.%s` is not a theme key and is ignored", file.Name, section, key))
				continue
			}
			if isModifierOnly(name) {
				mods, err := parseModifierOnly(values[key])
				if err != nil {
					return ThemeFile{}, fmt.Errorf("theme: %s: %w", name, err)
				}
				file.Modifiers[name] = mods
				continue
			}
			c, err := ParseColorJSON(values[key])
			if err != nil {
				return ThemeFile{}, fmt.Errorf("theme: %s: %w", name, err)
			}
			file.Values[name] = c
			file.Bare[name] = isBareColorJSON(values[key])
		}
	}
	return file, nil
}

// parseModifierOnly parses an attribute-only key's value: a list of attribute
// names, or a bundle whose color is absent.
func parseModifierOnly(raw json.RawMessage) ([]string, error) {
	c, err := ParseColorJSON(raw)
	if err != nil {
		return nil, err
	}
	if c.IsRGB || c.Name != "" {
		return nil, fmt.Errorf("names text attributes, not a color")
	}
	return c.Modifiers, nil
}

// isBareColorJSON reports whether raw is a bare color (an array or a string)
// rather than a {"color","modifier"} bundle.
func isBareColorJSON(raw json.RawMessage) bool {
	var nums []int
	if err := json.Unmarshal(raw, &nums); err == nil {
		return true
	}
	var name string
	return json.Unmarshal(raw, &name) == nil
}

// isModifierOnly reports whether name is an attribute-only theme key.
func isModifierOnly(name string) bool {
	for _, key := range modifierOnlyKeys {
		if key == name {
			return true
		}
	}
	return false
}

// modifierTarget maps an attribute-only key to the color key whose attributes it
// names, when the format has one.
func modifierTarget(name string) (string, bool) {
	switch name {
	case "editor.selection_modifier":
		return "editor.selection_bg", true
	case "ui.semantic_highlight_modifier":
		return "ui.semantic_highlight_bg", true
	}
	return "", false
}

// needsBase reports whether the file must be layered on a base theme: it does
// not name every required color and does not extend one explicitly.
func (f ThemeFile) needsBase() bool {
	for _, key := range RequiredKeys() {
		if _, ok := f.Values[key]; !ok {
			return true
		}
	}
	return false
}

// isNullJSON reports whether raw is a JSON null.
func isNullJSON(raw json.RawMessage) bool {
	return string(raw) == "null"
}

// sortedKeys returns the keys of m, sorted.
func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
