package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/internal/daemon"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/session"
	"github.com/tbxark/gvim/trust"
)

func TestRemoteWorkspaceIsolationInHostAndExtract(t *testing.T) {
	m, localID := newWorkspaceManagerTest(t)

	// Create a local directory with the exact same path as a remote workspace root and
	// mark it trusted with save_session=false. A remote workspace must use remote trust
	// and user config instead of reading that local project's .gvim.toml.
	decoyDir := t.TempDir()
	store, err := trust.DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(decoyDir, trust.Trusted); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(decoyDir, ".gvim.toml"), []byte("[editor]\nsave_session = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	decoyFile := filepath.Join(decoyDir, "shared.txt")
	if err := os.WriteFile(decoyFile, []byte("local decoy\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	specText, err := authoritySpecText(&authority.RemoteSpec{User: "u", Host: "h", Path: decoyDir})
	if err != nil {
		t.Fatal(err)
	}
	remoteWS, _, err := m.reg.CreateWithAuthority(decoyDir, "remote", specText)
	if err != nil {
		t.Fatal(err)
	}
	sessDir, err := m.workspaceSessionDir(remoteWS.File.StableID)
	if err != nil || sessDir == "" {
		t.Fatalf("remote workspaceSessionDir = %q, %v; read local decoy .gvim.toml", sessDir, err)
	}

	// dormantOwner must skip remote workspaces even when a saved layout path equals decoyFile.
	layout := session.Session{Version: session.Version, Root: decoyDir, Files: []session.FileState{{Path: decoyFile}}}
	if err := session.Save(sessDir, layout); err != nil {
		t.Fatal(err)
	}
	_, _ = m.reg.UpdateLayout(remoteWS.File.StableID, &layout)
	if ownerID, _, ok := m.dormantOwner(canonicalWorkspaceFile(decoyFile)); ok {
		t.Fatalf("dormantOwner matched remote workspace %q for local file", ownerID)
	}

	// Extract and openTarget isolation when the active session is remote.
	m.mu.Lock()
	sess := m.sessions[localID]
	origSpec := sess.ws.File.AuthoritySpec
	sess.ws.File.AuthoritySpec = "u@h:" + decoyDir
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		sess.ws.File.AuthoritySpec = origSpec
		m.mu.Unlock()
	}()

	if targets := m.ExtractTargets(); len(targets) != 0 {
		t.Fatalf("ExtractTargets on remote = %+v, want none", targets)
	}
	if _, err := m.ExtractTab(); err == nil || !strings.Contains(err.Error(), "remote workspace") {
		t.Fatalf("ExtractTab on remote = %v", err)
	}
	if _, err := m.ExtractTabs([]string{decoyFile}); err == nil || !strings.Contains(err.Error(), "remote workspace") {
		t.Fatalf("ExtractTabs on remote = %v", err)
	}
	if _, err := m.ExtractTabTo(remoteWS.File.StableID); err == nil || !strings.Contains(err.Error(), "remote workspace") {
		t.Fatalf("ExtractTabTo on remote = %v", err)
	}
	if _, err := m.ExtractTerminalTabTo(remoteWS.File.StableID); err == nil || !strings.Contains(err.Error(), "remote workspace") {
		t.Fatalf("ExtractTerminalTabTo on remote = %v", err)
	}
	if _, err := m.ExtractMixed(tui.ExtractSelection{Files: []string{decoyFile}}, "", false); err == nil || !strings.Contains(err.Error(), "remote workspace") {
		t.Fatalf("ExtractMixed on remote = %v", err)
	}

	m.openTarget(workspaceOpenTarget{path: decoyFile})
	m.mu.Lock()
	notice := m.lastNotice
	m.mu.Unlock()
	if !strings.Contains(notice, "remote workspace") {
		t.Fatalf("openTarget in remote workspace lastNotice = %q, want remote refusal notice", notice)
	}
	_ = daemon.Workspace{}
}
