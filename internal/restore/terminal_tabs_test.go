package restore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/terminal"
)

func TestTerminalTabsRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st := terminal.SavedState{Version: terminal.SavedStateVersion, Tabs: []terminal.SavedTab{
		{Name: "build", Open: true, Cwd: "/w", Width: 80, Rows: []string{"first"}, LaunchArgv: []string{"make"}},
		{Name: "agent", Open: true, Rows: []string{"second"}, Agent: "claude", ResumeArgv: []string{"claude", "--resume"}},
	}, ActiveTab: 1}
	if err := SaveTerminal(dir, st); err != nil {
		t.Fatal(err)
	}
	got, ok, err := LoadTerminal(dir)
	if err != nil || !ok {
		t.Fatalf("LoadTerminal = %v, %v", ok, err)
	}
	if len(got.Tabs) != 2 || got.ActiveTab != 1 {
		t.Fatalf("tabs = %+v active %d", got.Tabs, got.ActiveTab)
	}
	if got.Tabs[0].Name != "build" || got.Tabs[1].Name != "agent" {
		t.Fatalf("names = %q / %q", got.Tabs[0].Name, got.Tabs[1].Name)
	}
	if !strings.Contains(strings.Join(got.Tabs[1].Rows, "\n"), "second") {
		t.Fatalf("tab rows = %q", got.Tabs[1].Rows)
	}
	if got.Tabs[1].Agent != "claude" || len(got.Tabs[1].ResumeArgv) != 2 {
		t.Fatalf("launch record not carried into the tab: %+v", got.Tabs[1])
	}
}

func TestTerminalTabsMigrationAndBounds(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// A record with no tabs holds no terminal: it loads as nothing.
	none := `{"version":1,"open":true,"cwd":"/w","width":80,"rows":["old output"]}`
	if err := os.WriteFile(filepath.Join(dir, terminalFileName), []byte(none), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := LoadTerminal(dir); err != nil || ok {
		t.Fatalf("record without tabs load = %v, %v; want no terminal", ok, err)
	}

	// A corrupt record cannot exceed the tab or row bounds, and control characters
	// are stripped.
	var tabs []terminal.SavedTab
	for i := 0; i < maxSavedTerminalTabs+4; i++ {
		rows := make([]string, maxSavedTerminalRows+10)
		for j := range rows {
			rows[j] = "row\x01"
		}
		tabs = append(tabs, terminal.SavedTab{Name: "tab", Open: true, Rows: rows})
	}
	st := terminal.SavedState{Version: terminal.SavedStateVersion, Tabs: tabs}
	data, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, terminalFileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	got, ok, err := LoadTerminal(dir)
	if err != nil || !ok {
		t.Fatalf("corrupt record load = %v, %v", ok, err)
	}
	if len(got.Tabs) != maxSavedTerminalTabs {
		t.Fatalf("tabs = %d, want the cap %d", len(got.Tabs), maxSavedTerminalTabs)
	}
	for _, tab := range got.Tabs {
		if len(tab.Rows) > maxSavedTerminalRows {
			t.Fatalf("tab rows = %d, over the cap", len(tab.Rows))
		}
		for _, row := range tab.Rows {
			if strings.ContainsRune(row, 0x01) {
				t.Fatalf("control character survived: %q", row)
			}
		}
	}
}
