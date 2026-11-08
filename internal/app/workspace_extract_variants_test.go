package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/restore"
	"github.com/tbxark/gvim/internal/terminal"
	"github.com/tbxark/gvim/session"
)

// coTenantOverRoot creates a second, dormant co-tenant workspace over root.
func coTenantOverRoot(t *testing.T, m *workspaceManager, root string) string {
	t.Helper()
	ws, _, err := m.reg.CreateCoTenant(root, "")
	if err != nil {
		t.Fatal(err)
	}
	return ws.File.StableID
}

// materializeWorkspace switches to id and waits for its runtime.
func materializeWorkspace(t *testing.T, m *workspaceManager, id string) {
	t.Helper()
	cmd, err := m.Select(id)
	runWorkspaceCmd(t, cmd, err)
}

// TestExtractTabToDormantWorkspace: the file tab moves into an existing co-tenant's
// session (unsaved text stashed there, cursor kept), the source tab is closed only
// after the target accepted, and the switch makes the target the active workspace.
func TestExtractTabToDormantWorkspace(t *testing.T) {
	m, sourceID := newWorkspaceManagerTest(t)
	root := func() string {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.sessions[sourceID].ws.File.Root
	}()
	targetID := coTenantOverRoot(t, m, root)
	path := extractionFile(t, root, "move.txt", "one\ntwo\n")
	openUnsaved(t, m, sourceID, path, "MOVED-")
	doc := func() *struct{ Line, Col, Top int } { return nil }
	_ = doc
	m.mu.Lock()
	source := m.sessions[sourceID]
	source.rt.ed.Active().Cursor = buffer.Pos{Line: 1, Col: 2}
	source.rt.ed.Active().Top = 1
	m.mu.Unlock()

	cmd, err := m.ExtractTabTo(targetID)
	if err != nil {
		t.Fatalf("ExtractTabTo = %v", err)
	}
	if cmd == nil {
		t.Fatal("no command")
	}
	// Update does no I/O: the target session is not written until the command runs.
	targetDir, err := m.reg.SessionDir(targetID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := session.Load(targetDir); ok {
		t.Fatal("ExtractTabTo wrote the target session inside Update")
	}
	cmd()
	if workspaceHasTab(m, sourceID, path) {
		t.Fatal("the source tab is still open after a successful transfer")
	}
	layout, ok, err := session.Load(targetDir)
	if err != nil || !ok || len(layout.Files) != 1 {
		t.Fatalf("target session = %+v, %v, %v", layout, ok, err)
	}
	fs := layout.Files[0]
	if fs.Path != path || fs.Line != 2 || fs.Col != 3 || fs.Top != 1 || fs.StashFile == "" {
		t.Fatalf("transferred file state = %+v", fs)
	}
	stash, err := os.ReadFile(filepath.Join(targetDir, fs.StashFile))
	if err != nil || string(stash) != "MOVED-one\ntwo\n" {
		t.Fatalf("stash = %q, %v", stash, err)
	}
	if raw, _ := os.ReadFile(path); string(raw) != "one\ntwo\n" {
		t.Fatalf("the file was written by the extraction: %q", raw)
	}
	if !strings.Contains(m.lastNotice, "Extracted move.txt into workspace") {
		t.Fatalf("report = %q", m.lastNotice)
	}
	// The target is now active and materialized with the file open (restored from
	// its session on materialization).
	entries := m.Entries()
	var targetActive bool
	for _, e := range entries {
		if e.ID == targetID && e.Active && e.Materialized {
			targetActive = true
		}
	}
	if !targetActive || !workspaceHasTab(m, targetID, path) {
		t.Fatalf("target state = %+v, has tab %v", entries, workspaceHasTab(m, targetID, path))
	}
}

// TestExtractTabToMaterializedWorkspace: a warm target opens the file in its live
// editor with the unsaved text, and only then is the source tab closed.
func TestExtractTabToMaterializedWorkspace(t *testing.T) {
	m, sourceID := newWorkspaceManagerTest(t)
	root := func() string {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.sessions[sourceID].ws.File.Root
	}()
	targetID := coTenantOverRoot(t, m, root)
	materializeWorkspace(t, m, targetID)
	materializeWorkspace(t, m, sourceID)
	path := extractionFile(t, root, "warm.txt", "disk\n")
	openUnsaved(t, m, sourceID, path, "WARM-")

	cmd, err := m.ExtractTabTo(targetID)
	if err != nil || cmd == nil {
		t.Fatalf("ExtractTabTo = %v, %v", cmd, err)
	}
	cmd()
	if workspaceHasTab(m, sourceID, path) {
		t.Fatal("the source tab is still open")
	}
	if !workspaceHasTab(m, targetID, path) {
		t.Fatal("the warm target did not open the file")
	}
	m.mu.Lock()
	target := m.sessions[targetID]
	m.mu.Unlock()
	if target == nil {
		t.Fatal("the warm target was rebuilt")
	}
	got := ""
	for _, tab := range target.rt.ed.Tabs() {
		if tab.Path == path {
			for i := range target.rt.ed.Tabs() {
				if target.rt.ed.Tabs()[i].Path == path {
					got = target.rt.ed.DocAt(i).Buffer.Text()
				}
			}
		}
	}
	if !strings.Contains(got, "WARM-") {
		t.Fatalf("the target document text = %q, want the unsaved text", got)
	}
}

// TestExtractTabToRefusalKeepsSource: a synchronously refused switch leaves the
// source tab open; the target copy is kept (the transfer happened first) and the
// notice says the tab stays open.
func TestExtractTabToRefusalKeepsSource(t *testing.T) {
	m, sourceID := newWorkspaceManagerTest(t)
	root := func() string {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.sessions[sourceID].ws.File.Root
	}()
	targetID := coTenantOverRoot(t, m, root)
	path := extractionFile(t, root, "refused.txt", "disk\n")
	openUnsaved(t, m, sourceID, path, "UNSAVED-")
	m.beginSelectFn = func(string) (func() error, error) { return nil, errors.New("switch refused by test") }
	cmd, err := m.ExtractTabTo(targetID)
	if err != nil || cmd == nil {
		t.Fatalf("ExtractTabTo = %v, %v", cmd, err)
	}
	cmd()
	if !tabOpen(m, path) {
		t.Fatal("a refused switch closed the source tab")
	}
	if !strings.Contains(m.lastNotice, "switch refused by test") || !strings.Contains(m.lastNotice, "tab stays open") {
		t.Fatalf("notice = %q", m.lastNotice)
	}
	targetDir, _ := m.reg.SessionDir(targetID)
	if layout, ok, _ := session.Load(targetDir); !ok || len(layout.Files) != 1 {
		t.Fatalf("the refused transfer was not kept in the target: %+v", layout)
	}
}

// TestExtractTabToRefusesOtherRoot: a workspace over another root cannot receive the
// tab; nothing moves and the source tab stays open.
func TestExtractTabToRefusesOtherRoot(t *testing.T) {
	m, sourceID := newWorkspaceManagerTest(t)
	other := t.TempDir()
	otherWS, _, err := m.reg.Create(other, "other")
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	root := m.sessions[sourceID].ws.File.Root
	m.mu.Unlock()
	path := extractionFile(t, root, "root.txt", "x\n")
	openUnsaved(t, m, sourceID, path, "X-")
	cmd, err := m.ExtractTabTo(otherWS.File.StableID)
	if err != nil || cmd == nil {
		t.Fatalf("ExtractTabTo = %v, %v", cmd, err)
	}
	cmd()
	if !tabOpen(m, path) {
		t.Fatal("a cross-root extraction closed the source tab")
	}
	if !strings.Contains(m.lastNotice, "not over this root") {
		t.Fatalf("notice = %q", m.lastNotice)
	}
}

// TestExtractTerminalTabToDormantWorkspace: the dock tab's saved record moves into
// the target's terminal record, the source dock tab closes after the switch was
// accepted, and the target restores the tab (dead until its first dive; no PTY is
// handed over).
func TestExtractTerminalTabToDormantWorkspace(t *testing.T) {
	isolateConfig(t)
	t.Setenv("GVIM_WORKSPACE_DIR", t.TempDir())
	root := t.TempDir()
	sourceID, _ := seedLiveWorkspace(t, root)
	cfg := liveRestoreConfig(t, true, true)
	m := tabTestManager(t, root, cfg)
	t.Cleanup(m.closeAll)
	targetID := coTenantOverRoot(t, m, root)
	m.mu.Lock()
	source := m.sessions[sourceID]
	tab, ok := source.rt.model.ActiveTerminalTab()
	m.mu.Unlock()
	if !ok {
		t.Fatal("the seeded workspace has no terminal tab")
	}
	cmd, err := m.ExtractTerminalTabTo(targetID)
	if err != nil || cmd == nil {
		t.Fatalf("ExtractTerminalTabTo = %v, %v", cmd, err)
	}
	cmd()
	// The source dock tab is gone.
	m.mu.Lock()
	_, stillThere := m.sessions[sourceID].rt.model.ActiveTerminalTab()
	m.mu.Unlock()
	if stillThere {
		t.Fatal("the source terminal tab is still open")
	}
	// The target's terminal record holds the tab and restores it dead.
	targetDir, err := m.reg.SessionDir(targetID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := targetDirTab(t, targetDir); !ok {
		t.Fatal("the target terminal record is missing")
	}
	if want := tab.Name; want != "" && rowsText(tab.Rows) == "" {
		t.Fatalf("captured tab has no transcript: %+v", tab)
	}
	if !strings.Contains(m.lastNotice, "revives on the first dive") {
		t.Fatalf("notice = %q", m.lastNotice)
	}
	// The materialized target (after the switch) restores the tab read-only.
	m.mu.Lock()
	target := m.sessions[targetID]
	m.mu.Unlock()
	if target == nil {
		t.Fatal("the target was not materialized by the switch")
	}
	st := target.rt.model.TerminalState()
	if len(st.Tabs) != 1 || st.Tabs[0].Name != tab.Name {
		t.Fatalf("target terminal tabs = %+v", st.Tabs)
	}
	if !st.Tabs[0].Live {
		t.Fatal("the moved tab lost its live flag; the target could not revive it on the first dive")
	}
}

// targetDirTab loads the terminal record written by an extraction.
func targetDirTab(t *testing.T, dir string) (terminal.SavedTab, bool) {
	t.Helper()
	st, ok, err := restore.LoadTerminal(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || len(st.Tabs) == 0 {
		return terminal.SavedTab{}, false
	}
	return st.Tabs[0], true
}

// TestExtractTabsToNewWorkspace: several selected file tabs open in one new
// co-tenant workspace, every unsaved text is stashed there, and every source tab is
// closed only after the switch was accepted.
func TestExtractTabsToNewWorkspace(t *testing.T) {
	m, sourceID := newWorkspaceManagerTest(t)
	m.mu.Lock()
	root := m.sessions[sourceID].ws.File.Root
	m.mu.Unlock()
	one := extractionFile(t, root, "one.txt", "one\n")
	two := extractionFile(t, root, "two.txt", "two\n")
	openUnsaved(t, m, sourceID, one, "A-")
	m.mu.Lock()
	source := m.sessions[sourceID]
	m.mu.Unlock()
	openInModel(t, source.rt.model, two)
	source.rt.ed.Active().InsertText("B-")

	cmd, err := m.ExtractTabs([]string{one, two})
	if err != nil || cmd == nil {
		t.Fatalf("ExtractTabs = %v, %v", cmd, err)
	}
	// Before the command nothing moved and no workspace exists.
	if len(m.Entries()) != 1 || !tabOpen(m, one) || !tabOpen(m, two) {
		t.Fatal("ExtractTabs moved something inside Update")
	}
	cmd()
	if workspaceHasTab(m, sourceID, one) || workspaceHasTab(m, sourceID, two) {
		t.Fatal("a source tab survived the extraction")
	}
	entries := m.Entries()
	if len(entries) != 2 {
		t.Fatalf("entries = %+v", entries)
	}
	var destID string
	for _, e := range entries {
		if e.ID != sourceID {
			destID = e.ID
		}
	}
	dir, err := m.reg.SessionDir(destID)
	if err != nil {
		t.Fatal(err)
	}
	layout, ok, err := session.Load(dir)
	if err != nil || !ok || len(layout.Files) != 2 {
		t.Fatalf("multi session = %+v, %v, %v", layout, ok, err)
	}
	stashed := map[string]string{}
	for _, f := range layout.Files {
		if f.StashFile == "" {
			t.Fatalf("file %s has no stash", f.Path)
		}
		data, err := os.ReadFile(filepath.Join(dir, f.StashFile))
		if err != nil {
			t.Fatal(err)
		}
		stashed[f.Path] = string(data)
	}
	if stashed[one] != "A-one\n" || stashed[two] != "B-two\n" {
		t.Fatalf("stashes = %+v", stashed)
	}
	if !strings.Contains(m.lastNotice, "Extracted 2 tabs into workspace") {
		t.Fatalf("report = %q", m.lastNotice)
	}
	// The new workspace is active and has both files open.
	if !workspaceHasTab(m, destID, one) || !workspaceHasTab(m, destID, two) {
		t.Fatalf("extracted workspace tabs = %+v", m.sessions[destID].rt.ed.Tabs())
	}
}

// TestExtractTabsRefusesUnknownPath: a selection that no longer names an open tab
// refuses the whole action before any I/O; no tab moves.
func TestExtractTabsRefusesUnknownPath(t *testing.T) {
	m, sourceID := newWorkspaceManagerTest(t)
	m.mu.Lock()
	root := m.sessions[sourceID].ws.File.Root
	m.mu.Unlock()
	path := extractionFile(t, root, "known.txt", "x\n")
	openUnsaved(t, m, sourceID, path, "Y-")
	if _, err := m.ExtractTabs([]string{path, filepath.Join(root, "missing.txt")}); err == nil || !strings.Contains(err.Error(), "not open any more") {
		t.Fatalf("ExtractTabs = %v", err)
	}
	if !tabOpen(m, path) || len(m.Entries()) != 1 {
		t.Fatal("a refused multi extraction moved something")
	}
}
