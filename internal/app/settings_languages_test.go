package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tbxark/gvim/config"
)

// languagesLayer is a user config with one language entry setting a scalar, both
// lists, the formatter, on_save and indent.
const languagesLayer = `{
  "languages": {
    "go": {
      "tab_size": 2,
      "extensions": ["go"],
      "filenames": ["go.mod"],
      "formatter": {"command": "gofmt", "args": ["-w"], "stdin": true},
      "on_save": [{"command": "goimports"}],
      "indent": {"increase_indent_pattern": "{"}
    }
  }
}`

// TestSettingsLanguageInheritedRows checks the scalar rows of one language: a set
// field shows its value, an unset one is marked inherited and shows the value it
// inherits (the built-in entry over the global option).
func TestSettingsLanguageInheritedRows(t *testing.T) {
	t.Parallel()
	cfg, diags := loadLayerConfig(t, languagesLayer)
	if len(diags) != 0 {
		t.Fatalf("diagnostics: %v", diags)
	}
	s := lspSettingsService(t, "", languagesLayer, cfg, &fakeSettingsLSP{})
	st, err := s.Structure("languages.go", "user")
	if err != nil {
		t.Fatal(err)
	}
	if st.Kind != "fields" || !strings.Contains(st.Note, "inherit") {
		t.Fatalf("structure = %+v, want the fields page with the inherit note", st)
	}
	row := func(name string) (int, bool) {
		for i, r := range st.Rows {
			if r.Name == name {
				return i, true
			}
		}
		return -1, false
	}
	if i, ok := row("tab_size"); !ok || st.Rows[i].Value != "2" || st.Rows[i].Inherited {
		t.Fatalf("tab_size row = %+v (ok %v), want the override 2", st.Rows[max(i, 0)], ok)
	}
	if i, ok := row("use_tabs"); !ok || !st.Rows[i].Inherited || st.Rows[i].Value != "on" {
		t.Fatalf("use_tabs row = %+v (ok %v), want the inherited on", st.Rows[max(i, 0)], ok)
	}
	if i, ok := row("extensions"); !ok || st.Rows[i].Child != "languages.go.extensions" || !strings.Contains(st.Rows[i].Value, "1") {
		t.Fatalf("extensions row = %+v (ok %v), want the child row with one entry", st.Rows[max(i, 0)], ok)
	}
	if i, ok := row("on_save"); !ok || !strings.Contains(st.Rows[i].Value, "1 action") {
		t.Fatalf("on_save row = %+v (ok %v), want one action", st.Rows[max(i, 0)], ok)
	}
	// An unknown child key falls back to the scalar fields page.
	if fallback, err := s.Structure("languages.go.nonsense", "user"); err != nil || fallback.Kind != "fields" {
		t.Fatalf("fallback structure = %+v, %v", fallback, err)
	}
}

// TestSettingsLanguageStructuredPages checks the child structures the page opens:
// the two lists, the formatter fields and its args list, on_save as a validated
// whole JSON array, and the indent patterns. Every write it can produce passes
// the same config validation a real load uses.
func TestSettingsLanguageStructuredPages(t *testing.T) {
	t.Parallel()
	cfg, _ := loadLayerConfig(t, languagesLayer)
	s := lspSettingsService(t, "", languagesLayer, cfg, &fakeSettingsLSP{})

	// extensions and filenames are whole lists at their own paths.
	for _, tt := range []struct{ key, path string }{
		{"languages.go.extensions", "extensions"},
		{"languages.go.filenames", "filenames"},
	} {
		st, err := s.Structure(tt.key, "user")
		if err != nil {
			t.Fatal(err)
		}
		if st.Kind != "list" || len(st.Path) != 3 || st.Path[2] != tt.path || st.AddLabel == "" {
			t.Errorf("%s = %+v, want the whole-list page for %s", tt.key, st, tt.path)
		}
		if len(st.Rows) != 1 {
			t.Errorf("%s rows = %+v, want one entry", tt.key, st.Rows)
		}
	}

	// The formatter object: command and stdin set, timeout inherited.
	formatter, err := s.Structure("languages.go.formatter", "user")
	if err != nil {
		t.Fatal(err)
	}
	if formatter.Kind != "fields" {
		t.Fatalf("formatter = %+v, want fields", formatter)
	}
	var command, stdin, timeout, args string
	for _, r := range formatter.Rows {
		switch r.Name {
		case "command":
			command = r.Value
		case "stdin":
			stdin = r.Value
		case "timeout_ms":
			timeout = r.Value + "/" + boolText(r.Inherited)
		case "args":
			args = r.Value
		}
	}
	if command != "gofmt" || stdin != "on" || timeout != "10000/true" || !strings.Contains(args, "1 argument") {
		t.Errorf("formatter rows: command %q stdin %q timeout %q args %q", command, stdin, timeout, args)
	}
	argsPage, err := s.Structure("languages.go.formatter.args", "user")
	if err != nil {
		t.Fatal(err)
	}
	if argsPage.Kind != "list" || len(argsPage.Rows) != 1 || argsPage.Rows[0].Value != "-w" {
		t.Errorf("formatter args = %+v, want [-w]", argsPage)
	}

	// on_save is one validated JSON array row.
	onSave, err := s.Structure("languages.go.on_save", "user")
	if err != nil {
		t.Fatal(err)
	}
	var jsonRow = -1
	for i, r := range onSave.Rows {
		if r.Kind == "json" {
			jsonRow = i
		}
	}
	if jsonRow < 0 {
		t.Fatalf("on_save = %+v, want a JSON row", onSave.Rows)
	}
	raw := onSave.Rows[jsonRow].Value
	if !strings.Contains(raw, "goimports") {
		t.Errorf("on_save JSON = %q, want the action command", raw)
	}
	if diags := config.ValidateEdits([]config.Edit{{Path: []string{"languages", "go", "on_save"}, Raw: json.RawMessage(raw)}}, false); len(diags) != 0 {
		t.Errorf("the on_save JSON does not validate: %v", diags)
	}
	if diags := config.ValidateEdits([]config.Edit{{Path: []string{"languages", "go", "extensions"}, Raw: json.RawMessage(`["go","tmpl"]`)}}, false); len(diags) != 0 {
		t.Errorf("an extensions list does not validate: %v", diags)
	}

	// The indent patterns: one set, the others inherited.
	indent, err := s.Structure("languages.go.indent", "user")
	if err != nil {
		t.Fatal(err)
	}
	if indent.Kind != "fields" || len(indent.Rows) != 5 {
		t.Fatalf("indent = %+v, want five pattern rows", indent)
	}
	if r := indent.Rows[0]; r.Name != "increase_indent_pattern" || r.Value != "{" || r.Inherited {
		t.Errorf("increase pattern row = %+v, want the set pattern", r)
	}
	if r := indent.Rows[1]; !r.Inherited || r.Name != "decrease_indent_pattern" {
		t.Errorf("decrease pattern row = %+v, want inherited", r)
	}
}

// boolText renders a bool for test messages.
func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
