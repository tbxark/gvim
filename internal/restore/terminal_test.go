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
