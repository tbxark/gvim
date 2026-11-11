package app

import (
	"strings"
	"testing"
)

// TestWorkspaceExtractRefusedWithoutSessionStore: with editor.save_session off the extracted
// workspace has no session to carry the tab (and its unsaved text) into. Extraction must then
// refuse before it closes the source tab or leaves a half-created workspace: no work is lost.
func TestWorkspaceExtractRefusedWithoutSessionStore(t *testing.T) {
	m, sourceID := newWorkspaceManagerTest(t)
	// The manager's isolated config enables saving; this launch turns it off for the new
	// workspace, so extracting has nowhere to persist the tab.
	m.opts.Config = userConfigFile(t, "{\"editor\":{\"save_session\":false}}")
	m.mu.Lock()
	source := m.sessions[sourceID]
	root := source.ws.File.Root
	m.mu.Unlock()
	path := extractionFile(t, root, "a.txt", "one\ntwo\n")
	openUnsaved(t, m, sourceID, path, "NEW-")

	cmd, err := m.ExtractTab()
	if err != nil {
		t.Fatalf("ExtractTab = %v", err)
	}
	if cmd == nil {
		t.Fatal("ExtractTab returned no command")
	}
	cmd()
	if !strings.Contains(m.lastNotice, "session saving is disabled") {
		t.Fatalf("refusal = %q, want the disabled session store", m.lastNotice)
	}
	if !tabOpen(m, path) {
		t.Fatal("the source tab was closed although the extraction could not persist it")
	}
	if got := len(m.Entries()); got != 1 {
		t.Fatalf("a workspace was left behind: %+v", m.Entries())
	}
}
