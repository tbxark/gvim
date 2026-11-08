package restore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/terminal"
)

// TestTerminalScreenSnapshotRoundTrip checks the additive styled snapshot in the
// terminal record: it round-trips per tab (and the top-level mirror), and an old
// record without it loads with no snapshot.
func TestTerminalScreenSnapshotRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	screen := &terminal.SavedScreen{Cols: 4, Rows: []terminal.SavedRow{{Cells: []terminal.SavedCell{
		{Rune: 'o', Fg: "#ff0000", Bold: true},
		{Rune: 'k', Bg: "#0000ff"},
	}}}}
	st := terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true, Rows: []string{"ok"},
		Screen: screen,
		Tabs: []terminal.SavedTab{
			{Name: "a", Open: true, Rows: []string{"ok"}, Screen: screen},
			{Name: "b", Open: true, Rows: []string{"b"}},
		},
		ActiveTab: 0,
	}
	if err := SaveTerminal(dir, st); err != nil {
		t.Fatal(err)
	}
	got, ok, err := LoadTerminal(dir)
	if err != nil || !ok {
		t.Fatalf("LoadTerminal = %v, %v", ok, err)
	}
	if got.Screen == nil || len(got.Screen.Rows) != 1 || got.Screen.Rows[0].Cells[0].Fg != "#ff0000" {
		t.Fatalf("top-level snapshot = %+v", got.Screen)
	}
	if got.Tabs[0].Screen == nil || got.Tabs[1].Screen != nil {
		t.Fatalf("tab snapshots = %+v / %+v", got.Tabs[0].Screen, got.Tabs[1].Screen)
	}

	// An old record has no screen field anywhere and loads fine (text only).
	legacy := terminal.SavedState{Version: terminal.SavedStateVersion, Open: true, Rows: []string{"old"}}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, terminalFileName)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	old, ok, err := LoadTerminal(dir)
	if err != nil || !ok || old.Screen != nil {
		t.Fatalf("legacy record = %+v, %v, %v", old, ok, err)
	}
}

// TestLoadTerminalBoundsScreenSnapshot checks that a corrupt/hand-edited snapshot
// cannot make the loader paint garbage: rows/cols are bounded, control characters
// are replaced, invalid colours dropped and over-wide rows clipped, per tab too.
func TestLoadTerminalBoundsScreenSnapshot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rows := make([]terminal.SavedRow, terminal.MaxSavedScreenRows+20)
	for i := range rows {
		rows[i] = terminal.SavedRow{Cells: []terminal.SavedCell{
			{Rune: '\n', Fg: "not-a-color"},
			{Rune: 'x', Bg: "#00ff00"},
		}}
	}
	overCols := make([]terminal.SavedCell, terminal.MaxSavedScreenCols+10)
	for i := range overCols {
		overCols[i] = terminal.SavedCell{Rune: 'y'}
	}
	rows[0] = terminal.SavedRow{Cells: overCols}
	bad := &terminal.SavedScreen{Cols: 3, Rows: rows}
	st := terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true, Rows: []string{"x"},
		Screen: bad,
		Tabs:   []terminal.SavedTab{{Name: "t", Open: true, Rows: []string{"x"}, Screen: bad}},
	}
	if err := SaveTerminal(dir, st); err != nil {
		if !strings.Contains(err.Error(), "") {
			t.Fatal(err)
		}
		return
	}
	got, ok, err := LoadTerminal(dir)
	if err != nil || !ok {
		t.Fatalf("LoadTerminal = %v, %v", ok, err)
	}
	for name, snap := range map[string]*terminal.SavedScreen{"top": got.Screen, "tab": got.Tabs[0].Screen} {
		if snap == nil {
			t.Fatalf("%s snapshot missing", name)
		}
		if len(snap.Rows) > terminal.MaxSavedScreenRows || snap.Cols > terminal.MaxSavedScreenCols {
			t.Fatalf("%s snapshot not bounded: cols %d rows %d", name, snap.Cols, len(snap.Rows))
		}
		for _, row := range snap.Rows {
			if len(row.Cells) > snap.Cols {
				t.Fatalf("%s row too wide: %d > %d", name, len(row.Cells), snap.Cols)
			}
			for _, cell := range row.Cells {
				if cell.Rune == '\n' || cell.Rune == '\t' {
					t.Fatalf("%s control character survived: %q", name, cell.Rune)
				}
				if cell.Fg == "not-a-color" {
					t.Fatalf("%s invalid colour survived", name)
				}
			}
		}
	}
}
