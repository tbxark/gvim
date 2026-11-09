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

// TestQuarantineStatErrorProtectsRecord checks an Lstat error other than "not exist"
// (no execute permission on the directory) goes through protectUnquarantined: the
// in-memory block (and the marker when writable) is set, so a later save cannot touch
// the record after permissions recover.
func TestQuarantineStatErrorProtectsRecord(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, terminalFileName)
	original := []byte(`{"version":1,"rows":["kept"]}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(dir, 0o700)
		resetSaveProtectionForTest(dir)
	})
	if _, err := QuarantineTerminalRecord(dir); err == nil {
		t.Fatal("an Lstat error was not reported as a protection failure")
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, protected := TerminalRecordProtected(dir); !protected {
		t.Fatal("the stat-error path did not protect the record")
	}
	if err := SaveTerminal(dir, terminalSavedForTest()); err == nil {
		t.Fatal("SaveTerminal wrote after a stat-error protection")
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(original) {
		t.Fatalf("the original record changed: %q, %v", got, err)
	}
}

// TestDanglingSymlinkIsNotMissingAndIsQuarantined checks a dangling terminal.json
// symlink is not treated as a missing file: the pure loader reports an explicit
// error (extraction aborts), the restore quarantines the symlink *as a symlink*
// (link target preserved), and the later save writes a fresh regular record.
func TestDanglingSymlinkIsNotMissingAndIsQuarantined(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, terminalFileName)
	target := filepath.Join(dir, "missing-target.json")
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, ok, err := LoadTerminal(dir); err == nil || ok {
		t.Fatalf("dangling symlink load = ok %v err %v, want an explicit error", ok, err)
	} else if !strings.Contains(err.Error(), "dangling symlink") {
		t.Fatalf("dangling symlink error = %v", err)
	}
	if got, err := os.Readlink(path); err != nil || got != target {
		t.Fatalf("the dangling symlink changed: %q, %v", got, err)
	}
	if _, ok, err := LoadTerminalForRestore(dir); err == nil || ok {
		t.Fatalf("restore of a dangling symlink = ok %v err %v", ok, err)
	}
	rejected := path + ".rejected"
	info, err := os.Lstat(rejected)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("quarantined record = %+v, %v; want the symlink preserved", info, err)
	}
	if got, err := os.Readlink(rejected); err != nil || got != target {
		t.Fatalf("quarantined symlink target = %q, %v; want %q preserved", got, err, target)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("the original symlink path still exists: %v", err)
	}
	if err := SaveTerminal(dir, terminalSavedForTest()); err != nil {
		t.Fatalf("fresh save after quarantining the symlink: %v", err)
	}
	st, ok, err := LoadTerminal(dir)
	if err != nil || !ok || len(st.Rows) != 1 || st.Rows[0] != "fresh" {
		t.Fatalf("fresh record = %+v, %v, %v", st, ok, err)
	}
}

// TestSaveTerminalRefusesSymlinkedRecord checks SaveTerminal never follows or
// replaces a symlink (valid or dangling), including the empty-dock removal path, so
// the link and its target stay as the user left them.
func TestSaveTerminalRefusesSymlinkedRecord(t *testing.T) {
	t.Run("valid target", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "outside.json")
		original := []byte(`{"version":1,"rows":["target"]}`)
		if err := os.WriteFile(target, original, 0o600); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, terminalFileName)
		if err := os.Symlink(target, path); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if err := SaveTerminal(dir, terminalSavedForTest()); err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("SaveTerminal over a symlink = %v, want a symlink refusal", err)
		}
		if got, err := os.Readlink(path); err != nil || got != target {
			t.Fatalf("the symlink changed: %q, %v", got, err)
		}
		if got, err := os.ReadFile(target); err != nil || string(got) != string(original) {
			t.Fatalf("the symlink target changed: %q, %v", got, err)
		}
		if err := SaveTerminal(dir, emptyTerminalSavedForTest()); err == nil {
			t.Fatal("an empty save removed the symlink")
		}
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("the symlink was removed by an empty save: %v", err)
		}
	})
	t.Run("dangling target", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, terminalFileName)
		if err := os.Symlink(filepath.Join(dir, "gone.json"), path); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if err := SaveTerminal(dir, terminalSavedForTest()); err == nil {
			t.Fatal("SaveTerminal replaced a dangling symlink")
		}
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("the dangling symlink was removed: %v", err)
		}
	})
}

// TestTerminalRecordSymlinkPrecheck checks the extraction pre-check helper: a
// readable symlinked record reports the link (saving would refuse it), while an
// absent path, a regular record and a dangling symlink do not match it (the dangling
// one is handled by the loader as an unusable record).
func TestTerminalRecordSymlinkPrecheck(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, terminalFileName)
	if link, ok := TerminalRecordSymlink(dir); ok || link != "" {
		t.Fatalf("absent record reported as a symlink: %q, %v", link, ok)
	}
	if err := os.WriteFile(path, []byte(`{"version":1,"rows":["out"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if link, ok := TerminalRecordSymlink(dir); ok || link != "" {
		t.Fatalf("regular record reported as a symlink: %q, %v", link, ok)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "real-terminal.json")
	if err := os.WriteFile(target, []byte(`{"version":1,"rows":["out"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if link, ok := TerminalRecordSymlink(dir); !ok || link != path {
		t.Fatalf("valid symlink precheck = %q, %v", link, ok)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "gone.json"), path); err != nil {
		t.Fatal(err)
	}
	if link, ok := TerminalRecordSymlink(dir); ok || link != "" {
		t.Fatalf("dangling symlink matched the readable-symlink precheck: %q, %v", link, ok)
	}
}
