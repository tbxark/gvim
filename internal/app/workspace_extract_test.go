package app

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/session"
)

// extractionFile writes name under root/sub and returns the file path: a tab of the
// workspace rooted at root, so extraction can be a co-tenant over that same root.
func extractionFile(t *testing.T, root, name, text string) string {
	t.Helper()
	dir := filepath.Join(root, "sub")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// openUnsaved opens path in model and types text, returning the editor document.
func openUnsaved(t *testing.T, m *workspaceManager, id, path, text string) {
	t.Helper()
	m.mu.Lock()
	sess := m.sessions[id]
	m.mu.Unlock()
	openInModel(t, sess.rt.model, path)
	doc := sess.rt.ed.Active()
	if doc == nil || doc.Path != path {
		t.Fatalf("active document = %+v", doc)
	}
	doc.InsertText(text)
}

// TestWorkspaceExtractCarriesUnsavedTab is the co-tenant extraction end to end: the source
// workspace keeps its root and label, the extracted tab becomes a new workspace over the SAME
// root with Fresh's "label (2)" precedence, the unsaved text and cursor travel in the new
// workspace's session, the source tab is force-closed, and the file on disk is untouched.
func TestWorkspaceExtractCarriesUnsavedTab(t *testing.T) {
	m, sourceID := newWorkspaceManagerTest(t)
	m.mu.Lock()
	source := m.sessions[sourceID]
	root := source.ws.File.Root
	sourceLabel := source.ws.File.Label
	m.mu.Unlock()
	path := extractionFile(t, root, "a.txt", "one\ntwo\n")

	openUnsaved(t, m, sourceID, path, "NEW-")
	doc := source.rt.ed.Active()
	doc.Cursor = buffer.Pos{Line: 1, Col: 2}
	doc.Top = 1

	cmd, err := m.ExtractTab()
	if err != nil {
		t.Fatalf("ExtractTab = %v", err)
	}
	if cmd == nil {
		t.Fatal("ExtractTab returned no command")
	}
	// Update does no disk I/O: the registry has only the source workspace until the
	// command runs and the source tab is still open.
	if got := len(m.Entries()); got != 1 {
		t.Fatalf("extract created a workspace inside Update: %+v", m.Entries())
	}
	if !tabOpen(m, path) {
		t.Fatal("the source tab disappeared before the command ran")
	}
	cmd()

	entries := m.Entries()
	if len(entries) != 2 {
		t.Fatalf("entries after extraction = %+v", entries)
	}
	var destID string
	for _, e := range entries {
		if e.Root != root {
			t.Fatalf("extracted workspace root = %s, want the source root %s", e.Root, root)
		}
		if e.ID != sourceID {
			destID = e.ID
			if !e.Active || !e.Materialized {
				t.Fatalf("new workspace = %+v, want active materialized", e)
			}
			if e.Label != sourceLabel+" (2)" {
				t.Fatalf("co-tenant label = %q, want %q", e.Label, sourceLabel+" (2)")
			}
		}
	}
	if destID == "" {
		t.Fatal("no extracted workspace")
	}
	// The source tab was force-closed without asking (the text is in the new session);
	// the extracted workspace legitimately opens the file, so check the SOURCE runtime.
	if workspaceHasTab(m, sourceID, path) {
		t.Fatal("the source tab is still open after a successful extraction")
	}
	// The new workspace's session carries the file at the saved cursor with the stash.
	dir, err := m.reg.SessionDir(destID)
	if err != nil {
		t.Fatal(err)
	}
	layout, ok, err := session.Load(dir)
	if err != nil || !ok {
		t.Fatalf("load the extracted session: ok=%v err=%v", ok, err)
	}
	if len(layout.Files) != 1 {
		t.Fatalf("extracted layout = %+v", layout)
	}
	if want := []session.MruFile{{Path: path}}; !slices.Equal(layout.MRU, want) {
		t.Fatalf("extracted MRU = %+v, want %+v", layout.MRU, want)
	}
	fs := layout.Files[0]
	if fs.Path != path || fs.Line != 2 || fs.Col != 3 || fs.Top != 1 {
		t.Fatalf("extracted file state = %+v, want path, line 2, col 3, top 1", fs)
	}
	if fs.StashFile == "" {
		t.Fatal("the unsaved text was not stashed into the new workspace")
	}
	stash, err := os.ReadFile(filepath.Join(dir, fs.StashFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(stash) != "NEW-one\ntwo\n" {
		t.Fatalf("stash = %q, want the unsaved text", stash)
	}
	// The file on disk is untouched and the report names the co-tenant.
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "one\ntwo\n" {
		t.Fatalf("the file was written by the extraction: %q, %v", raw, err)
	}
	if !strings.Contains(m.lastNotice, "Extracted a.txt into workspace") || !strings.Contains(m.lastNotice, "(2)") {
		t.Fatalf("report = %q", m.lastNotice)
	}
	// The source workspace's re-capture after the close records it WITHOUT the extracted
	// tab: switching back must not resurrect a duplicate of it.
	if err := m.waitWrites(10e9); err != nil {
		t.Fatalf("capture write: %v", err)
	}
	sourceDir, err := m.reg.SessionDir(sourceID)
	if err != nil {
		t.Fatal(err)
	}
	if sourceLayout, ok, err := session.Load(sourceDir); err == nil && ok {
		for _, f := range sourceLayout.Files {
			if f.Path == path {
				t.Fatalf("the source workspace's session still restores the extracted tab: %+v", f)
			}
		}
	}
}

// TestWorkspaceExtractCoTenantLabelsAndIndependence checks a second extraction and the
// independence of the co-tenants: labels follow the numbered precedence, each session keeps
// its own layout, and closing one co-tenant leaves the others and their session data.
func TestWorkspaceExtractCoTenantLabelsAndIndependence(t *testing.T) {
	m, sourceID := newWorkspaceManagerTest(t)
	m.mu.Lock()
	source := m.sessions[sourceID]
	root := source.ws.File.Root
	sourceLabel := source.ws.File.Label
	m.mu.Unlock()
	first := extractionFile(t, root, "one.txt", "one\n")
	openUnsaved(t, m, sourceID, first, "A-")
	extractCmd(t, m)

	// Switch back to the source workspace and extract a second tab from it.
	selectCmd, err := m.Select(sourceID)
	runWorkspaceCmd(t, selectCmd, err)
	second := extractionFile(t, root, "two.txt", "two\n")
	openUnsaved(t, m, sourceID, second, "B-")
	extractCmd(t, m)

	entries := m.Entries()
	if len(entries) != 3 {
		t.Fatalf("entries after two extractions = %+v", entries)
	}
	labels := make(map[string]bool)
	var ids []string
	for _, e := range entries {
		if e.Root != root {
			t.Fatalf("workspace root = %s, want %s", e.Root, root)
		}
		labels[e.Label] = true
		ids = append(ids, e.ID)
	}
	for _, want := range []string{sourceLabel, sourceLabel + " (2)", sourceLabel + " (3)"} {
		if !labels[want] {
			t.Fatalf("labels = %v, missing %q", labels, want)
		}
	}
	// Each co-tenant's session carries exactly its own extracted tab; the source runtime
	// has neither (both were force-closed). The listing order is by focus, so identify
	// them by their session content rather than by creation order.
	have := map[string]string{}
	for _, id := range ids {
		if id == sourceID {
			continue
		}
		dir, err := m.reg.SessionDir(id)
		if err != nil {
			t.Fatal(err)
		}
		layout, ok, err := session.Load(dir)
		if err != nil || !ok || len(layout.Files) != 1 {
			t.Fatalf("session of %s = %+v, %v, %v", id, layout, ok, err)
		}
		have[filepath.Base(layout.Files[0].Path)] = id
	}
	if len(have) != 2 || have["one.txt"] == "" || have["two.txt"] == "" {
		t.Fatalf("co-tenant sessions = %v, want one.txt and two.txt", have)
	}
	for _, name := range []string{"one.txt", "two.txt"} {
		if workspaceHasTab(m, sourceID, filepath.Join(root, "sub", name)) {
			t.Fatalf("the source workspace still has the extracted tab %q", name)
		}
	}
	// Closing one co-tenant keeps the other workspaces and their session data.
	closer := have["one.txt"]
	closeCmd, err := m.Close(closer)
	runWorkspaceCmd(t, closeCmd, err)
	left := m.Entries()
	if len(left) != 2 {
		t.Fatalf("entries after closing one co-tenant = %+v", left)
	}
	for _, e := range left {
		if e.ID == closer {
			t.Fatalf("a closed co-tenant is still listed: %+v", e)
		}
	}
	kept := have["two.txt"]
	if _, err := os.Stat(filepath.Join(m.reg.Dir(), "sessions", kept)); err != nil {
		t.Fatalf("closing one co-tenant removed another's session dir: %v", err)
	}
}

// TestWorkspaceExtractRestartRestoresCoTenants: a daemon restart over the same registry
// restores both workspaces over the shared root (the freshest as foreground, the other
// dormant) with their independent sessions.
func TestWorkspaceExtractRestartRestoresCoTenants(t *testing.T) {
	m, sourceID := newWorkspaceManagerTest(t)
	m.mu.Lock()
	source := m.sessions[sourceID]
	root := source.ws.File.Root
	m.mu.Unlock()
	path := extractionFile(t, root, "restart.txt", "hello\n")
	openUnsaved(t, m, sourceID, path, "X-")
	extractCmd(t, m)
	entries := m.Entries()
	if len(entries) != 2 {
		t.Fatalf("entries before restart = %+v", entries)
	}
	dir := m.reg.Dir()
	m.closeAll()

	// Reopen the registry the way a daemon start does: same workspace dir and root.
	t.Setenv("GVIM_WORKSPACE_DIR", dir)
	reopened, err := newWorkspaceManager(t.Context(), Options{
		Root:       root,
		SessionDir: t.TempDir(),
		Daemon:     &DaemonOptions{Mode: DaemonServe},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.closeAll)
	after := reopened.Entries()
	if len(after) != 2 {
		t.Fatalf("entries after restart = %+v", after)
	}
	for _, e := range after {
		if e.Root != root {
			t.Fatalf("restored workspace root = %s, want %s", e.Root, root)
		}
	}
	// The registered workspaces are materializable after restart, each with its own
	// session: the co-tenant restores the extracted file at its saved cursor.
	var coTenant string
	for _, e := range after {
		if !e.Active {
			coTenant = e.ID
		}
	}
	if coTenant == "" {
		coTenant = after[0].ID
	}
	cmd, err := reopened.Select(coTenant)
	runWorkspaceCmd(t, cmd, err)
	reopened.mu.Lock()
	sess := reopened.sessions[coTenant]
	reopened.mu.Unlock()
	if sess == nil {
		t.Fatalf("the restored co-tenant %s is not materialized", coTenant)
	}
	found := false
	for _, tab := range sess.rt.ed.Tabs() {
		if tab.Path == path {
			found = true
		}
	}
	if !found {
		t.Fatalf("the co-tenant's session did not restore %s", path)
	}
}

// TestWorkspaceExtractRefusals checks the cases extraction still refuses instead of
// dropping the tab: no file path (the welcome screen). The tab stays open.
func TestWorkspaceExtractRefusals(t *testing.T) {
	m, _ := newWorkspaceManagerTest(t)
	if _, err := m.ExtractTab(); err == nil || !strings.Contains(err.Error(), "no file path") {
		t.Fatalf("ExtractTab on the welcome screen = %v", err)
	}
}

// extractCmd runs one extraction and returns the new workspace's command result.
func extractCmd(t *testing.T, m *workspaceManager) {
	t.Helper()
	cmd, err := m.ExtractTab()
	if err != nil {
		t.Fatalf("ExtractTab = %v", err)
	}
	if cmd == nil {
		t.Fatal("ExtractTab returned no command")
	}
	cmd()
}

// workspaceHasTab reports whether workspace id's runtime has path open in a tab.
func workspaceHasTab(m *workspaceManager, id, path string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	sess := m.sessions[id]
	if sess == nil {
		return false
	}
	for _, tab := range sess.rt.ed.Tabs() {
		if tab.Path == path {
			return true
		}
	}
	return false
}

// tabOpen reports whether the active workspace still has path open in a tab.
func tabOpen(m *workspaceManager, path string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	sess := m.sessions[m.active]
	if sess == nil {
		return false
	}
	for _, tab := range sess.rt.ed.Tabs() {
		if tab.Path == path {
			return true
		}
	}
	return false
}

// TestWorkspaceExtractBeginSelectRefusalKeepsTheTab is the data-safety regression for a
// synchronously refused switch: the source tab must still be open with its unsaved text and
// the just-created co-tenant must be dropped (the tab never moved, so nothing can be lost).
func TestWorkspaceExtractBeginSelectRefusalKeepsTheTab(t *testing.T) {
	m, sourceID := newWorkspaceManagerTest(t)
	m.mu.Lock()
	root := m.sessions[sourceID].ws.File.Root
	m.mu.Unlock()
	path := extractionFile(t, root, "refused.txt", "on disk\n")
	openUnsaved(t, m, sourceID, path, "UNSAVED-")
	m.beginSelectFn = func(string) (func() error, error) {
		return nil, errors.New("switch refused by test")
	}
	cmd, err := m.ExtractTab()
	if err != nil {
		t.Fatalf("ExtractTab = %v", err)
	}
	cmd()

	if !strings.Contains(m.lastNotice, "switch refused by test") || !strings.Contains(m.lastNotice, "tab stays open") {
		t.Fatalf("notice = %q", m.lastNotice)
	}
	if !tabOpen(m, path) {
		t.Fatal("a refused switch closed the source tab")
	}
	m.mu.Lock()
	doc := m.sessions[sourceID].rt.ed.Active()
	m.mu.Unlock()
	if doc == nil || !strings.Contains(doc.Buffer.Text(), "UNSAVED-") {
		t.Fatal("the refused extraction lost the tab's unsaved text")
	}
	if got := len(m.Entries()); got != 1 {
		t.Fatalf("a refused switch left a co-tenant behind: %+v", m.Entries())
	}
}

// TestWorkspaceExtractQueuedSwitchFailureKeepsTheText: once the switch has been accepted the
// tab is closed, so a later materialization failure must NOT discard the new workspace: its
// session holds the unsaved text, and the notice says exactly that (not "the tab stays open").
func TestWorkspaceExtractQueuedSwitchFailureKeepsTheText(t *testing.T) {
	m, sourceID := newWorkspaceManagerTest(t)
	m.mu.Lock()
	root := m.sessions[sourceID].ws.File.Root
	m.mu.Unlock()
	path := extractionFile(t, root, "queued.txt", "on disk\n")
	openUnsaved(t, m, sourceID, path, "KEPT-")
	var tabOpenAtQueue bool
	m.beginSelectFn = func(string) (func() error, error) {
		// The switch is queued while the source tab is still open (close happens after).
		tabOpenAtQueue = tabOpen(m, path)
		return func() error { return errors.New("materialize failed by test") }, nil
	}
	cmd, err := m.ExtractTab()
	if err != nil {
		t.Fatalf("ExtractTab = %v", err)
	}
	cmd()

	if !tabOpenAtQueue {
		t.Fatal("the source tab was closed before the switch was queued")
	}
	if tabOpen(m, path) {
		t.Fatal("the accepted extraction left the source tab open")
	}
	if strings.Contains(m.lastNotice, "tab stays open") {
		t.Fatalf("notice claims the tab is open after it was closed: %q", m.lastNotice)
	}
	if !strings.Contains(m.lastNotice, "text is kept there") || !strings.Contains(m.lastNotice, "materialize failed by test") {
		t.Fatalf("notice = %q", m.lastNotice)
	}
	entries := m.Entries()
	if len(entries) != 2 {
		t.Fatalf("a queued-switch failure discarded the text-bearing workspace: %+v", entries)
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
	if err != nil || !ok || len(layout.Files) != 1 || layout.Files[0].StashFile == "" {
		t.Fatalf("kept session = %+v, %v, %v", layout, ok, err)
	}
	stash, err := os.ReadFile(filepath.Join(dir, layout.Files[0].StashFile))
	if err != nil || string(stash) != "KEPT-on disk\n" {
		t.Fatalf("stash = %q, %v", stash, err)
	}
}
