package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/tailscale/hujson"
)

// Edit is one setting the Settings UI writes: its path in the config file and
// the JSON value to put there.
type Edit struct {
	Path []string
	Raw  json.RawMessage
}

// PatchBytes applies edits to config file data and returns the new file, so the
// Settings UI writes a value without rewriting the file: the comments, the
// trailing commas and the layout stay, and every key the edit does not name is
// kept as it is. A missing parent object is created (an edit of editor.tab_size
// in a file with no "editor" object writes one). The caller writes the result;
// internal/app/settingssvc.go replaces the file atomically through atomicfile.
// data nil or empty is read as an empty object. An invalid JSON-with-comments
// document is an error, and no partial output is returned.
func PatchBytes(data []byte, edits []Edit) ([]byte, error) {
	if len(edits) == 0 {
		return data, nil
	}
	if len(bytes.TrimSpace(data)) == 0 {
		data = []byte("{}")
	}
	root, err := hujson.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("invalid JSON with comments: %w", err)
	}
	if _, ok := root.Value.(*hujson.Object); !ok {
		return nil, errors.New("top level must be a JSON object")
	}
	indent := indentUnit(&root)
	for _, e := range edits {
		if len(e.Path) == 0 {
			return nil, errors.New("an edit needs a non-empty path")
		}
		if len(e.Raw) == 0 {
			return nil, fmt.Errorf("%s: the value is empty", strings.Join(e.Path, "."))
		}
		if err := setSetting(&root, e.Path, e.Raw, indent, 0); err != nil {
			return nil, err
		}
	}
	return root.Pack(), nil
}

// setSetting sets path to raw inside the object v. depth is the number of path
// elements above v (0 for the root), which gives the indentation of a member
// created here. It keeps the node the edit replaces as it was around the value:
// its indentation, a comment before it and a comment after it.
func setSetting(v *hujson.Value, path []string, raw json.RawMessage, indent string, depth int) error {
	obj, ok := v.Value.(*hujson.Object)
	if !ok {
		return fmt.Errorf("%s: must be a JSON object", path[0])
	}
	name := path[0]
	idx := -1
	for i, m := range obj.Members {
		if literalString(m.Name.Value) == name {
			idx = i
			break
		}
	}
	if len(path) > 1 {
		if idx < 0 {
			// Create the parent object the edit needs. The line break belongs to
			// the member name; the value follows the colon after one space.
			before := newlineIndent(indent, depth+1)
			member := hujson.ObjectMember{
				Name: hujson.Value{BeforeExtra: before, Value: hujson.String(name)},
				Value: hujson.Value{
					BeforeExtra: []byte(" "),
					Value:       &hujson.Object{AfterExtra: newlineIndent(indent, depth)},
				},
			}
			hadComma := hasTrailingComma(obj)
			obj.Members = append(obj.Members, member)
			restoreTrailingComma(obj, hadComma)
			idx = len(obj.Members) - 1
		}
		return setSetting(&obj.Members[idx].Value, path[1:], raw, indent, depth+1)
	}
	value, err := hujson.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s: the value is not JSON: %w", name, err)
	}
	if idx >= 0 {
		// Keep what surrounds the old value: its indentation and comments. A
		// trailing comment stays with the setting it describes.
		old := obj.Members[idx].Value
		value.BeforeExtra = old.BeforeExtra
		value.AfterExtra = old.AfterExtra
		obj.Members[idx].Value = value
		return nil
	}
	before := newlineIndent(indent, depth+1)
	hadComma := hasTrailingComma(obj)
	value.BeforeExtra = []byte(" ")
	obj.Members = append(obj.Members, hujson.ObjectMember{
		Name:  hujson.Value{BeforeExtra: before, Value: hujson.String(name)},
		Value: value,
	})
	restoreTrailingComma(obj, hadComma)
	return nil
}

// indentUnit returns the indentation one level uses in the file: the whitespace
// before the first member of the root object. A file with no member, or with no
// line break, uses two spaces, gvim's own config style.
func indentUnit(root *hujson.Value) string {
	obj, ok := root.Value.(*hujson.Object)
	if !ok || len(obj.Members) == 0 {
		return "  "
	}
	before := obj.Members[0].Name.BeforeExtra
	if i := bytes.LastIndexByte(before, '\n'); i >= 0 {
		if unit := string(before[i+1:]); strings.Trim(unit, " \t") == "" {
			return unit
		}
	}
	return "  "
}

// newlineIndent is the whitespace before a member at depth levels: a line break
// and depth indent units.
func newlineIndent(indent string, depth int) []byte {
	return []byte("\n" + strings.Repeat(indent, depth))
}

// hasTrailingComma reports whether the last member of obj is followed by a
// trailing comma.
func hasTrailingComma(obj *hujson.Object) bool {
	if len(obj.Members) == 0 {
		return false
	}
	return obj.Members[len(obj.Members)-1].Value.AfterExtra != nil
}

// restoreTrailingComma keeps an object's trailing-comma style after a member
// was appended: hujson writes the comma only when the last value's AfterExtra is
// non-nil.
func restoreTrailingComma(obj *hujson.Object, had bool) {
	if !had || len(obj.Members) == 0 {
		return
	}
	last := &obj.Members[len(obj.Members)-1].Value
	if last.AfterExtra == nil {
		last.AfterExtra = []byte{}
	}
}

// literalString returns the string a JSON string literal holds, or "" for any
// other literal.
func literalString(v hujson.ValueTrimmed) string {
	if literal, ok := v.(hujson.Literal); ok && literal.Kind() == '"' {
		return literal.String()
	}
	return ""
}

// ValidateEdits applies edits to a fresh default config as one layer and reports
// the problems. It is the check the Settings UI runs before it writes: the value
// goes through the same option tables as a real load, and a project edit through
// the same project-layer rules, so a value the editor could not read is refused
// before it reaches the file. project says the edits target the project layer.
func ValidateEdits(edits []Edit, project bool) []Diagnostic {
	keys, err := editsObject(edits)
	if err != nil {
		return []Diagnostic{{Message: err.Error()}}
	}
	cfg := Defaults()
	l := layerUser
	if project {
		l = layerProject
	}
	return applyObject(&cfg, keys, "", l)
}

// editsObject turns the edits into the top-level object a config file would
// hold: a nested map of the keys the edits set. An edit into a non-object (a
// setting) or two edits that would replace one another is an error.
func editsObject(edits []Edit) (map[string]json.RawMessage, error) {
	root := make(map[string]json.RawMessage)
	for _, e := range edits {
		if len(e.Path) == 0 {
			return nil, errors.New("an edit needs a non-empty path")
		}
		if err := setNested(root, e.Path, e.Raw); err != nil {
			return nil, err
		}
	}
	return root, nil
}

// setNested puts raw at path in m, creating the objects on the way.
func setNested(m map[string]json.RawMessage, path []string, raw json.RawMessage) error {
	key := path[0]
	if len(path) == 1 {
		m[key] = raw
		return nil
	}
	next := make(map[string]json.RawMessage)
	if existing, ok := m[key]; ok {
		if err := json.Unmarshal(existing, &next); err != nil {
			return fmt.Errorf("%s: must be a JSON object", key)
		}
	}
	if err := setNested(next, path[1:], raw); err != nil {
		return err
	}
	encoded, err := json.Marshal(next)
	if err != nil {
		return err
	}
	m[key] = encoded
	return nil
}
