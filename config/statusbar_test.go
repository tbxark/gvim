package config

import (
	"slices"
	"strings"
	"testing"
)

// TestStatusBarElementNamesAreFreshs checks that every element name Fresh's status
// bar parses is accepted, with or without braces, and that a plugin token is accepted
// too. The names are Fresh's StatusBarElement TryFrom list (fresh-editor-core
// config.rs).
func TestStatusBarElementNamesAreFreshs(t *testing.T) {
	names := []string{
		"filename", "read_only", "cursor", "cursor:compact", "diagnostics",
		"cursor_count", "messages", "chord", "line_ending", "encoding", "language",
		"lsp", "warnings", "update", "terminal_restart", "palette", "clock", "remote",
		"trust",
	}
	for _, name := range names {
		if !validStatusBarElement(name) || !validStatusBarElement("{"+name+"}") {
			t.Errorf("element %q is not accepted", name)
		}
	}
	if !validStatusBarElement("{my_plugin:branch}") {
		t.Error("a plugin token is not accepted")
	}
	for _, bad := range []string{"", "no_such_element", "{no_such_element}", "{}"} {
		if validStatusBarElement(bad) {
			t.Errorf("unknown element %q is accepted", bad)
		}
	}
}

// TestStatusBarConfigParsesTheLists checks the config round trip: the lists keep
// their spelling (braces included), an explicitly empty list stays empty, and the
// separator is kept as written.
func TestStatusBarConfigParsesTheLists(t *testing.T) {
	body := "{\"editor\": {\"status_bar\": {" +
		"\"left\": [\"{cursor}\", \"{messages}\", \"{trust}\"]," +
		"\"right\": [\"{language}\", \"{lsp}\", \"{diagnostics}\"]," +
		"\"separator\": \" | \"" +
		"}}}"
	cfg, diags := loadBody(t, body)
	if len(diags) != 0 {
		t.Fatalf("diagnostics: %v", diags)
	}
	if got, want := cfg.StatusBar.Left, []string{"{cursor}", "{messages}", "{trust}"}; !slices.Equal(got, want) {
		t.Errorf("left = %q, want %q", got, want)
	}
	if got, want := cfg.StatusBar.Right, []string{"{language}", "{lsp}", "{diagnostics}"}; !slices.Equal(got, want) {
		t.Errorf("right = %q, want %q", got, want)
	}
	if cfg.StatusBar.Separator != " | " {
		t.Errorf("separator = %q, want %q", cfg.StatusBar.Separator, " | ")
	}

	empty, diags := loadBody(t, "{\"editor\": {\"status_bar\": {\"left\": [], \"right\": []}}}")
	if len(diags) != 0 {
		t.Fatalf("empty lists: diagnostics: %v", diags)
	}
	if empty.StatusBar.Left == nil || len(empty.StatusBar.Left) != 0 {
		t.Errorf("an explicitly empty left list = %#v, want an empty non-nil list", empty.StatusBar.Left)
	}
	if empty.StatusBar.Right == nil || len(empty.StatusBar.Right) != 0 {
		t.Errorf("an explicitly empty right list = %#v, want an empty non-nil list", empty.StatusBar.Right)
	}
}

// TestStatusBarUnknownElementIsReported checks Fresh's TryFrom rule: a name the status
// bar does not know is a config diagnostic, not a silently dropped element.
func TestStatusBarUnknownElementIsReported(t *testing.T) {
	tests := []struct {
		name, body, side string
	}{
		{"left", "{\"editor\": {\"status_bar\": {\"left\": [\"{cursor}\", \"{no_such_element}\"]}}}", "left"},
		{"right", "{\"editor\": {\"status_bar\": {\"right\": [\"{nope}\"]}}}", "right"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := loadBody(t, tt.body)
			if len(diags) != 1 || diags[0].Key != "editor.status_bar" ||
				!strings.Contains(diags[0].Message, tt.side) ||
				!strings.Contains(diags[0].Message, "unknown status bar element") {
				t.Fatalf("diags = %+v, want one for editor.status_bar naming the unknown %s element", diags, tt.side)
			}
		})
	}
}
