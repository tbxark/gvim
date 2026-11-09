//go:build unix

package restore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/terminal"
)

func terminalSavedForTest() terminal.SavedState {
	return terminal.SavedState{Version: terminal.SavedStateVersion, Open: true, Rows: []string{"fresh"}}
}

func emptyTerminalSavedForTest() terminal.SavedState {
	return terminal.SavedState{Version: terminal.SavedStateVersion}
}

// TestUnquarantinableRecordDisablesSavesForThisProcess is the audit case where both
// the quarantine rename and the marker write fail (an unwritable directory): the
// process must still refuse future saves in memory, so a later normal save cannot
// overwrite the original record.
func TestUnquarantinableRecordDisablesSavesForThisProcess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, terminalFileName)
	original := []byte("{unreadable record")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(dir, 0o700)
		resetSaveProtectionForTest(dir)
	})
	if _, _, err := LoadTerminalForRestore(dir); err == nil {
		t.Fatal("an unquarantinable unreadable record was not reported")
	} else if !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("warning = %v, want the save-disable explanation", err)
	}
	// Make the directory writable again: the in-memory block must still refuse.
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	err := SaveTerminal(dir, terminalSavedForTest())
	if err == nil || !strings.Contains(err.Error(), "disabled for this process") {
		t.Fatalf("SaveTerminal error = %v, want the in-memory save block", err)
	}
	if got, readErr := os.ReadFile(path); readErr != nil || string(got) != string(original) {
		t.Fatalf("protected record changed: %q, %v", got, readErr)
	}
	// Even an empty-dock save (which would remove the file) must be refused.
	if err := SaveTerminal(dir, emptyTerminalSavedForTest()); err == nil {
		t.Fatal("an empty save removed the protected record")
	}
	if got, readErr := os.ReadFile(path); readErr != nil || string(got) != string(original) {
		t.Fatalf("empty save changed the protected record: %q, %v", got, readErr)
	}
}
