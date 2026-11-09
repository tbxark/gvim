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
	// Extraction's protection check must see the in-memory block even though no
	// marker exists on disk: an empty path with protected=true.
	if marker, protected := TerminalRecordProtected(dir); !protected || marker != "" {
		t.Fatalf("TerminalRecordProtected after the in-memory block = %q, %v", marker, protected)
	}
}

// TestProtectionMarkerDanglingSymlinkCounts checks the .protected check uses Lstat:
// a dangling symlink at the marker name must still protect the record (a Stat-based
// check would miss it).
func TestProtectionMarkerDanglingSymlinkCounts(t *testing.T) {
	dir := t.TempDir()
	original := []byte(`{"version":1,"rows":["kept"]}`)
	if err := os.WriteFile(filepath.Join(dir, terminalFileName), original, 0o600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, terminalFileName+terminalProtectMarkerSuffix)
	if err := os.Symlink(filepath.Join(dir, "does-not-exist"), marker); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	gotPath, protected := TerminalRecordProtected(dir)
	if !protected || gotPath != marker {
		t.Fatalf("dangling marker symlink: %q, %v", gotPath, protected)
	}
	if err := SaveTerminal(dir, terminalSavedForTest()); err == nil {
		t.Fatal("SaveTerminal wrote despite a dangling marker symlink")
	}
	if got, err := os.ReadFile(filepath.Join(dir, terminalFileName)); err != nil || string(got) != string(original) {
		t.Fatalf("the record changed despite the marker: %q, %v", got, err)
	}
}

// TestQuarantineStatErrorProtectsRecord checks a stat error other than "not exist"
// (a self-referential symlink gives ELOOP) goes through protectUnquarantined: the
// persistent marker and the in-memory block are set, and a later save cannot touch
// the record.
func TestQuarantineStatErrorProtectsRecord(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, terminalFileName)
	if err := os.Symlink(path, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	t.Cleanup(func() { resetSaveProtectionForTest(dir) })
	if _, err := QuarantineTerminalRecord(dir); err == nil {
		t.Fatal("a stat error was not reported as a protection failure")
	}
	if marker, protected := TerminalRecordProtected(dir); !protected || marker == "" {
		t.Fatalf("stat-error protection = marker %q protected %v", marker, protected)
	}
	if err := SaveTerminal(dir, terminalSavedForTest()); err == nil {
		t.Fatal("SaveTerminal wrote after a stat-error protection")
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("the original record was removed: %v", err)
	}
}
