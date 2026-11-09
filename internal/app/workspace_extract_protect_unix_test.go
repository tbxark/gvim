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

// TestMixedExtractRefusesDanglingSymlinkTargetRecord checks a dangling terminal.json
// symlink is not treated as a missing record by extraction: the pure loader fails, the
// whole mixed migration is refused before writing anything, and the symlink and its
// target are preserved exactly.
func TestMixedExtractRefusesDanglingSymlinkTargetRecord(t *testing.T) {
	m, sourceID := newWorkspaceManagerTest(t)
	root := activeRoot(m)
	targetID := coTenantOverRoot(t, m, root)
	path := extractionFile(t, root, "dangling.txt", "disk\n")
	openUnsaved(t, m, sourceID, path, "D-")
	seedDockTerminal(t, m, sourceID, "shell")
	dir, err := m.reg.SessionDir(targetID)
	if err != nil {
		t.Fatal(err)
	}
	linkTarget := filepath.Join(dir, "missing-terminal-record.json")
	link := filepath.Join(dir, "terminal.json")
	if err := os.Symlink(linkTarget, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	cmd, err := m.ExtractMixed(mixedSelection(t, m, path), targetID, false)
	if err != nil || cmd == nil {
		t.Fatalf("ExtractMixed = %v, %v", cmd, err)
	}
	cmd()
	if !strings.Contains(m.lastNotice, "dangling symlink") {
		t.Fatalf("notice = %q, want the dangling-symlink refusal", m.lastNotice)
	}
	if got, err := os.Readlink(link); err != nil || got != linkTarget {
		t.Fatalf("the dangling symlink changed: %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "session.json")); err == nil {
		t.Fatal("the refused extraction wrote the target session")
	}
	if !workspaceHasTab(m, sourceID, path) || len(dockTabNames(m, sourceID)) != 1 {
		t.Fatal("the refused extraction moved a source tab")
	}
}

// TestMixedExtractRefusesValidSymlinkTargetRecord checks a valid terminal.json
// symlink is a predictable save failure, so extraction pre-checks it and refuses the
// whole migration (files included) before any write; the same guard covers the
// single-terminal path.
func TestMixedExtractRefusesValidSymlinkTargetRecord(t *testing.T) {
	m, sourceID := newWorkspaceManagerTest(t)
	root := activeRoot(m)
	targetID := coTenantOverRoot(t, m, root)
	path := extractionFile(t, root, "validsymlink.txt", "disk\n")
	openUnsaved(t, m, sourceID, path, "VS-")
	seedDockTerminal(t, m, sourceID, "shell")
	dir, err := m.reg.SessionDir(targetID)
	if err != nil {
		t.Fatal(err)
	}
	targetJSON := filepath.Join(dir, "linked-terminal.json")
	record := []byte(`{"version":1,"open":true,"tabs":[{"name":"existing","open":true,"rows":["target"]}]}`)
	if err := os.WriteFile(targetJSON, record, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "terminal.json")
	if err := os.Symlink(targetJSON, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	cmd, err := m.ExtractMixed(mixedSelection(t, m, path), targetID, false)
	if err != nil || cmd == nil {
		t.Fatalf("ExtractMixed = %v, %v", cmd, err)
	}
	cmd()
	if !strings.Contains(m.lastNotice, "is a symlink") {
		t.Fatalf("mixed notice = %q, want the symlink pre-check refusal", m.lastNotice)
	}
	if _, err := os.Stat(filepath.Join(dir, "session.json")); err == nil {
		t.Fatal("the refused extraction wrote the target session (partial migration)")
	}
	if got, err := os.Readlink(link); err != nil || got != targetJSON {
		t.Fatalf("the symlink changed: %q, %v", got, err)
	}
	if got, err := os.ReadFile(targetJSON); err != nil || !bytes.Equal(got, record) {
		t.Fatalf("the symlink target changed: %q, %v", got, err)
	}
	if !workspaceHasTab(m, sourceID, path) || len(dockTabNames(m, sourceID)) != 1 {
		t.Fatal("the refused extraction moved a source tab")
	}
	// The single-terminal extraction uses the same pre-check.
	cmd, err = m.ExtractTerminalTabTo(targetID)
	if err != nil || cmd == nil {
		t.Fatalf("ExtractTerminalTabTo = %v, %v", cmd, err)
	}
	cmd()
	if !strings.Contains(m.lastNotice, "is a symlink") {
		t.Fatalf("single extraction notice = %q", m.lastNotice)
	}
	if len(dockTabNames(m, sourceID)) != 1 {
		t.Fatal("the single terminal extraction moved the source tab")
	}
}
