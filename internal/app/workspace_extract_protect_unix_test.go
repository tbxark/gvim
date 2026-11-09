//go:build unix

package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/restore"
)

// TestMixedExtractRefusesInMemoryProtectedTargetRecord checks extraction sees the
// in-memory save-disabled state (no marker on disk, an unquarantinable record) BEFORE
// moving any tab: the mixed and single-terminal migrations refuse, the target record
// and the source tabs are untouched.
func TestMixedExtractRefusesInMemoryProtectedTargetRecord(t *testing.T) {
	m, sourceID := newWorkspaceManagerTest(t)
	root := activeRoot(m)
	targetID := coTenantOverRoot(t, m, root)
	path := extractionFile(t, root, "memprot.txt", "disk\n")
	openUnsaved(t, m, sourceID, path, "MP-")
	seedDockTerminal(t, m, sourceID, "shell")
	dir, err := m.reg.SessionDir(targetID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "terminal.json"), []byte("{unreadable"), 0o600); err != nil {
		t.Fatal(err)
	}
	// An unwritable directory makes both the quarantine rename and the marker write
	// fail, leaving the process-wide in-memory save block.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	if _, _, err := restore.LoadTerminalForRestore(dir); err == nil {
		t.Fatal("an unquarantinable record was not reported")
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if marker, protected := restore.TerminalRecordProtected(dir); !protected || marker != "" {
		t.Fatalf("in-memory protection not visible: %q, %v", marker, protected)
	}
	before := readFileBytes(t, filepath.Join(dir, "terminal.json"))

	cmd, err := m.ExtractMixed(mixedSelection(t, m, path), targetID, false)
	if err != nil || cmd == nil {
		t.Fatalf("ExtractMixed = %v, %v", cmd, err)
	}
	cmd()
	if !strings.Contains(m.lastNotice, "protected") {
		t.Fatalf("mixed notice = %q, want the protected-record refusal", m.lastNotice)
	}
	if got := readFileBytes(t, filepath.Join(dir, "terminal.json")); !bytes.Equal(got, before) {
		t.Fatal("the in-memory protected record changed during the mixed extraction")
	}
	if !workspaceHasTab(m, sourceID, path) || len(dockTabNames(m, sourceID)) != 1 {
		t.Fatal("the mixed extraction moved a source tab despite the protection")
	}

	cmd, err = m.ExtractTerminalTabTo(targetID)
	if err != nil || cmd == nil {
		t.Fatalf("ExtractTerminalTabTo = %v, %v", cmd, err)
	}
	cmd()
	if !strings.Contains(m.lastNotice, "protected") {
		t.Fatalf("single extraction notice = %q", m.lastNotice)
	}
	if len(dockTabNames(m, sourceID)) != 1 {
		t.Fatal("the single terminal extraction moved the source tab despite the protection")
	}
	if got := readFileBytes(t, filepath.Join(dir, "terminal.json")); !bytes.Equal(got, before) {
		t.Fatal("the in-memory protected record changed during the single extraction")
	}
}
