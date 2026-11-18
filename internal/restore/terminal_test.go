package restore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/terminal"
)

func TestTerminalStateRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	want := terminal.SavedState{Version: terminal.SavedStateVersion, Tabs: []terminal.SavedTab{{Open: true, Cwd: "/work", Width: 120, Rows: []string{"old output", "second row"}}}}
	if err := SaveTerminal(dir, want); err != nil {
		t.Fatalf("SaveTerminal: %v", err)
	}
	got, ok, err := LoadTerminal(dir)
	if err != nil || !ok {
		t.Fatalf("LoadTerminal = %+v, %v, %v", got, ok, err)
	}
	if got.Version != want.Version || !activeSaved(got).Open || activeSaved(got).Cwd != activeSaved(want).Cwd || activeSaved(got).Width != activeSaved(want).Width || len(activeSaved(got).Rows) != len(activeSaved(want).Rows) {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
	if strings.Join(activeSaved(got).Rows, "\n") != strings.Join(activeSaved(want).Rows, "\n") {
		t.Fatalf("rows = %q, want %q", activeSaved(got).Rows, activeSaved(want).Rows)
	}
}

func TestTerminalStateMissingFile(t *testing.T) {
	t.Parallel()
	got, ok, err := LoadTerminal(t.TempDir())
	if err != nil || ok || len(activeSaved(got).Rows) != 0 {
		t.Fatalf("missing terminal state = %+v, %v, %v", got, ok, err)
	}
}

func TestTerminalStateOfAClosedDockRemovesTheFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := SaveTerminal(dir, terminal.SavedState{Version: terminal.SavedStateVersion, Tabs: []terminal.SavedTab{{Open: true, Rows: []string{"old"}}}}); err != nil {
		t.Fatal(err)
	}
	if err := SaveTerminal(dir, terminal.SavedState{Version: terminal.SavedStateVersion}); err != nil {
		t.Fatalf("SaveTerminal(closed): %v", err)
	}
	if _, ok, err := LoadTerminal(dir); err != nil || ok {
		t.Fatalf("a closed dock left a terminal state: %v, %v", ok, err)
	}
}

func TestTerminalStateVersionMismatchIsIgnored(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := SaveTerminal(dir, terminal.SavedState{Version: terminal.SavedStateVersion + 1, Tabs: []terminal.SavedTab{{Open: true, Rows: []string{"future"}}}}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := LoadTerminal(dir); err != nil || ok {
		t.Fatalf("a future version loaded: %v, %v", ok, err)
	}
}

func TestTerminalStateStripsControlCharactersAndBounds(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rows := make([]string, 0, maxSavedTerminalRows+100)
	for i := 0; i < maxSavedTerminalRows+100; i++ {
		rows = append(rows, "row")
	}
	rows = append(rows, "evil \x1b[31mred")
	if err := SaveTerminal(dir, terminal.SavedState{Version: terminal.SavedStateVersion, Tabs: []terminal.SavedTab{{Open: true, Rows: rows}}}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := LoadTerminal(dir)
	if err != nil || !ok {
		t.Fatalf("LoadTerminal: %v, %v", ok, err)
	}
	if len(activeSaved(got).Rows) > maxSavedTerminalRows {
		t.Fatalf("loaded %d rows, over the bound %d", len(activeSaved(got).Rows), maxSavedTerminalRows)
	}
	for _, row := range activeSaved(got).Rows {
		if strings.ContainsAny(row, "\x1b\x00\n\r") {
			t.Fatalf("row %q holds control characters", row)
		}
	}
}

func TestTerminalStateCorruptFileIsAnError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, terminalFileName), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadTerminal(dir); err == nil {
		t.Fatal("a corrupt terminal state loaded without an error")
	}
}

// resetSaveProtectionForTest clears the in-memory block for a test directory.
func resetSaveProtectionForTest(dir string) {
	saveDisabledMu.Lock()
	defer saveDisabledMu.Unlock()
	delete(saveDisabledDirs, dir)
}

func TestSaveTerminalDefaultsZeroVersion(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	err := SaveTerminal(dir, terminal.SavedState{
		Tabs: []terminal.SavedTab{{Open: true, Rows: []string{"hello"}}},
	})
	if err != nil {
		t.Fatalf("SaveTerminal: %v", err)
	}
	got, ok, err := LoadTerminal(dir)
	if err != nil || !ok || got.Version != terminal.SavedStateVersion {
		t.Fatalf("LoadTerminal = %+v, %v, %v; want version %d", got, ok, err, terminal.SavedStateVersion)
	}
}

func TestBoundTerminalStateAdjustsActiveTabWhenTabsDropped(t *testing.T) {
	t.Parallel()
	t.Run("trims leading tabs past maxSavedTerminalTabs", func(t *testing.T) {
		var tabs []terminal.SavedTab
		for i := range maxSavedTerminalTabs + 3 {
			tabs = append(tabs, terminal.SavedTab{Name: string(rune('a' + i)), Open: true})
		}
		st := boundTerminalState(terminal.SavedState{
			Version:   terminal.SavedStateVersion,
			Tabs:      tabs,
			ActiveTab: 5, // name 'f', shifts from 5 to 5-3 = 2
		})
		if st.ActiveTab != 2 || st.Tabs[st.ActiveTab].Name != "f" {
			t.Fatalf("ActiveTab = %d (%q), want 2 (\"f\")", st.ActiveTab, st.Tabs[st.ActiveTab].Name)
		}
	})

	t.Run("skips closed empty tabs before active", func(t *testing.T) {
		st := boundTerminalState(terminal.SavedState{
			Version: terminal.SavedStateVersion,
			Tabs: []terminal.SavedTab{
				{Name: "empty-closed-0", Open: false},
				{Name: "open-1", Open: true},
				{Name: "empty-closed-2", Open: false},
				{Name: "target-3", Open: true},
			},
			ActiveTab: 3,
		})
		if st.ActiveTab != 1 || st.Tabs[st.ActiveTab].Name != "target-3" {
			t.Fatalf("ActiveTab = %d (%q), want 1 (\"target-3\")", st.ActiveTab, st.Tabs[st.ActiveTab].Name)
		}
	})
}

func TestCleanTerminalTextMapsControlsAndC1ToSpace(t *testing.T) {
	t.Parallel()
	got := cleanTerminalText("a\x1bb\x7fc\u0085d\u009be")
	want := "a b c d e"
	if got != want {
		t.Fatalf("cleanTerminalText = %q, want %q", got, want)
	}
}
