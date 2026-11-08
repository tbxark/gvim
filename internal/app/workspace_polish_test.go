package app

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/daemon"
)

// TestWorkspaceRenameAndDormantCloseReport checks the cheap rename/dormant polish: renaming
// reports the new label (and the socket answer carries it), and closing a dormant workspace
// reports that nothing was killed because no runtime existed.
func TestWorkspaceRenameAndDormantCloseReport(t *testing.T) {
	m, foregroundID := newWorkspaceManagerTest(t)
	dormant := createDormant(t, m)

	if err := m.Rename(foregroundID, "renamed"); err != nil {
		t.Fatalf("Rename = %v", err)
	}
	if !strings.Contains(m.lastNotice, "Renamed workspace to \"renamed\"") {
		t.Fatalf("rename report = %q", m.lastNotice)
	}
	for _, e := range m.Entries() {
		if e.ID == foregroundID && e.Label != "renamed" {
			t.Fatalf("renamed workspace = %+v", e)
		}
	}
	res := m.handleRequest(daemon.WorkspaceRequest{Rename: &daemon.WorkspaceRename{ID: foregroundID, Label: "again"}})
	// A rename answers with the list only; the status-row report is the TUI's.
	if res.Notice != "" {
		t.Fatalf("protocol rename notice = %q, want none", res.Notice)
	}
	if res.List == nil {
		t.Fatal("the rename answer has no list")
	}
	renamed := false
	for _, e := range res.List.Entries {
		if e.ID == foregroundID && e.Label == "again" {
			renamed = true
		}
	}
	if !renamed {
		t.Fatalf("the rename is not in the answer: %+v", res.List.Entries)
	}

	res = m.handleRequest(daemon.WorkspaceRequest{Close: &daemon.WorkspaceClose{ID: dormant.File.StableID}})
	if !strings.Contains(res.Notice, "Closed a dormant workspace") {
		t.Fatalf("dormant close notice = %q", res.Notice)
	}
	entries := m.Entries()
	if len(entries) != 1 || entries[0].ID != foregroundID {
		t.Fatalf("entries after dormant close = %+v", entries)
	}
}
