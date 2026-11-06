package ui

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/theme"
)

// TestDumpConfigOpensTheSettingsAsJSONInANewTab checks that Dump Config opens an
// untitled tab and types the settings snapshot into it as JSON, with the caret at the
// top.
func TestDumpConfigOpensTheSettingsAsJSONInANewTab(t *testing.T) {
	t.Parallel()
	ed := newFakeEditor("x")
	m := New(Deps{
		Editor: ed, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock,
		Settings: Settings{Theme: "dracula", Keymap: "default", TabSize: 4, InsertSpaces: true, ExplorerShowHidden: true},
	})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	rootPaletteRun(m, "dump_config")

	names := ed.actionNames()
	if !slices.Contains(names, "new") || !slices.Contains(names, "insert_char") {
		t.Fatalf("editor ran %q, want a new tab with text", names)
	}
	var text string
	for i, a := range ed.actions {
		if a == "insert_char" {
			text, _ = ed.args[i]["text"].(string)
		}
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("dump is not JSON: %v\n%s", err, text)
	}
	if got["theme"] != "dracula" || got["tab_size"] != float64(4) || got["insert_spaces"] != true || got["explorer_show_hidden"] != true {
		t.Fatalf("dump = %v, want the settings snapshot", got)
	}
	if names[len(names)-1] != "move_document_start" {
		t.Fatalf("editor ran %q, want the caret moved to the top last", names)
	}
}

// TestDumpUITreeDescribesTheModel checks that Dump UI Tree opens a new tab that
// describes the tabs, the focus and the overlays of the model.
func TestDumpUITreeDescribesTheModel(t *testing.T) {
	t.Parallel()
	ed := newFakeEditor("x")
	m := newTestModel(t, ed, newFakeTree(2), 80, 24)
	rootPaletteRun(m, "dump_ui_tree")

	var text string
	for i, a := range ed.actions {
		if a == "insert_char" {
			text, _ = ed.args[i]["text"].(string)
		}
	}
	for _, line := range []string{"size 80x24", "focus editor", "tabs 1 active 1", "overlays 0"} {
		if !strings.Contains(text, line) {
			t.Fatalf("ui tree lacks %q:\n%s", line, text)
		}
	}
}
