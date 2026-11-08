package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/daemon"
	"github.com/tbxark/gvim/session"
)

// TestSharedBuffersForDaemonOnly: only a daemon-hosted local launch gets a registry; a
// direct launch and a remote workspace keep independent buffers.
func TestSharedBuffersForDaemonOnly(t *testing.T) {
	t.Parallel()
	if sharedBuffersFor(Options{}) != nil {
		t.Fatal("a direct launch got a shared-buffer registry")
	}
	if sharedBuffersFor(Options{Daemon: &DaemonOptions{}}) != nil {
		t.Fatal("a daemon launch without a registry got one")
	}
	reg := editor.NewSharedBuffers()
	if got := sharedBuffersFor(Options{Daemon: &DaemonOptions{}, sharedBuffers: reg}); got != reg {
		t.Fatalf("daemon registry = %p, want %p", got, reg)
	}
	if sharedBuffersFor(Options{Daemon: &DaemonOptions{}, Remote: &authority.RemoteSpec{}, sharedBuffers: reg}) != nil {
		t.Fatal("a remote workspace used the shared-buffer registry")
	}
}

// TestWorkspaceSharedBuffersAcrossWorkspaces drives the daemon wiring end to end: a file
// opened in the foreground workspace, then in a second materialized workspace, becomes a
// read-only mirror there until the workspace switch takes ownership; edits propagate; a
// denied request leaves a conflict notice; closing the owner keeps the buffer alive in
// the other workspace.
func TestWorkspaceSharedBuffersAcrossWorkspaces(t *testing.T) {
	isolateConfig(t)
	t.Setenv("GVIM_WORKSPACE_DIR", t.TempDir())
	rootA := t.TempDir()
	rootB := t.TempDir()
	path := filepath.Join(rootA, "shared.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := newWorkspaceManager(t.Context(), Options{
		Root:       rootA,
		SessionDir: t.TempDir(),
		Daemon:     &DaemonOptions{Mode: DaemonServe},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.closeAll)
	if m.shared == nil {
		t.Fatal("the daemon manager has no shared-buffer registry")
	}
	m.mu.Lock()
	aSession := m.sessions[m.active]
	m.mu.Unlock()
	if aSession == nil || aSession.rt == nil {
		t.Fatal("no foreground workspace runtime")
	}
	if err := aSession.rt.ed.OpenFile(path); err != nil {
		t.Fatalf("open in first workspace: %v", err)
	}
	if aSession.rt.ed.Active().SharedMirror() {
		t.Fatal("the first opener is a mirror")
	}
	created, createdErr := m.Create(rootB, "second")
	runWorkspaceCmd(t, created, createdErr)
	m.mu.Lock()
	bSession := m.sessions[m.active]
	m.mu.Unlock()
	if bSession == nil || bSession.rt == nil || bSession == aSession {
		t.Fatal("the second workspace did not materialize")
	}
	if err := bSession.rt.ed.OpenFile(path); err != nil {
		t.Fatalf("open in the second workspace: %v", err)
	}
	// The second workspace is focused, so it took ownership; the first is its mirror.
	if bSession.rt.ed.Active().SharedMirror() || bSession.rt.ed.Active().ReadOnly() {
		t.Fatal("the focused second workspace did not take ownership")
	}
	if !aSession.rt.ed.Active().SharedMirror() || !aSession.rt.ed.Active().ReadOnly() {
		t.Fatal("the first workspace is not a read-only mirror")
	}
	bSession.rt.ed.Active().Buffer.Insert(buffer.Pos{Line: 0, Col: 0}, "X")
	if got := aSession.rt.ed.Active().Buffer.Text(); got != "Xone\ntwo\n" {
		t.Fatalf("mirror text after edit = %q", got)
	}
	// A denied ownership request on switching back leaves the conflict in the mirror.
	m.shared.Deny = func(owner, requester *editor.Editor) error { return os.ErrPermission }
	t.Cleanup(func() { m.shared.Deny = nil })
	selectCmd, selectErr := m.Select(aSession.ws.File.StableID)
	runWorkspaceCmd(t, selectCmd, selectErr)
	if !aSession.rt.ed.Active().SharedMirror() {
		t.Fatal("the denied switch changed ownership")
	}
	if !strings.Contains(aSession.rt.ed.Active().Status, "conflict") {
		t.Fatalf("conflict status = %q", aSession.rt.ed.Active().Status)
	}
	// Clearing the refusal and switching away and back takes ownership normally (a
	// select of the already-active workspace is a no-op by design).
	m.shared.Deny = nil
	bCmd, bErr := m.Select(bSession.ws.File.StableID)
	runWorkspaceCmd(t, bCmd, bErr)
	aCmd, aErr := m.Select(aSession.ws.File.StableID)
	runWorkspaceCmd(t, aCmd, aErr)
	if aSession.rt.ed.Active().SharedMirror() {
		t.Fatal("the cleared switch did not take ownership")
	}
	// Closing the owning workspace promotes the mirror in the other workspace.
	m.shared.Deny = nil
	if aSession.ws.File.StableID == m.active {
		bCmd, bErr := m.Select(bSession.ws.File.StableID)
		runWorkspaceCmd(t, bCmd, bErr)
	}
	closeCmd, closeErr := m.Close(aSession.ws.File.StableID)
	runWorkspaceCmd(t, closeCmd, closeErr)
	m.mu.Lock()
	bSession = m.sessions[m.active]
	m.mu.Unlock()
	if bSession == nil {
		t.Fatal("the surviving workspace disappeared")
	}
	if bSession.rt.ed.Active().SharedMirror() || bSession.rt.ed.Active().ReadOnly() {
		t.Fatal("closing the owner lost the buffer instead of promoting the mirror")
	}
	if got := bSession.rt.ed.Active().Buffer.Text(); got == "" {
		t.Fatal("the promoted buffer is empty")
	}
}

// TestWorkspaceSharedBuffersStartupRestoreConflict: a dormant workspace restored at
// daemon startup from a saved session that has the same file open (with different
// unsaved stash text) does not open an independent copy: the live owner text stays
// authoritative, the restored workspace gets a mirror and a separate untitled conflict
// tab, and the switch then moves canonical ownership there.
func TestWorkspaceSharedBuffersStartupRestoreConflict(t *testing.T) {
	isolateConfig(t)
	workspacesDir := t.TempDir()
	t.Setenv("GVIM_WORKSPACE_DIR", workspacesDir)
	rootA := t.TempDir()
	rootB := t.TempDir()
	path := filepath.Join(rootA, "shared.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A dormant workspace B whose saved session restores the same file with a stale
	// unsaved stash text.
	idB, dirB := precreateDormantWorkspace(t, workspacesDir, rootB, "B", nil)
	stashName, err := session.StashContent(dirB, "stale unsaved\n")
	if err != nil {
		t.Fatal(err)
	}
	layout := session.Session{Version: 1, Root: rootB, Files: []session.FileState{{
		Path: path, StashFile: stashName, Line: 1,
	}}}
	if err := session.Save(dirB, layout); err != nil {
		t.Fatal(err)
	}
	reg, err := daemon.OpenRegistry(workspacesDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.UpdateLayout(idB, &layout); err != nil {
		t.Fatal(err)
	}
	m, err := newWorkspaceManager(t.Context(), Options{
		Root:       rootA,
		SessionDir: t.TempDir(),
		Daemon:     &DaemonOptions{Mode: DaemonServe},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.closeAll)
	m.mu.Lock()
	aSession := m.sessions[m.active]
	m.mu.Unlock()
	if aSession == nil || aSession.rt == nil {
		t.Fatal("no foreground workspace runtime")
	}
	// The foreground workspace owns the file with live unsaved text.
	if err := aSession.rt.ed.OpenFile(path); err != nil {
		t.Fatalf("open in the foreground workspace: %v", err)
	}
	aSession.rt.ed.Active().Buffer.Insert(buffer.Pos{Line: 0, Col: 0}, "owner ")
	ownerText := aSession.rt.ed.Active().Buffer.Text()
	// Materialize the dormant workspace: its session restore adopts the shared file.
	selectCmd, selectErr := m.Select(idB)
	runWorkspaceCmd(t, selectCmd, selectErr)
	m.mu.Lock()
	bSession := m.sessions[idB]
	m.mu.Unlock()
	if bSession == nil || bSession.rt == nil {
		t.Fatal("the restored workspace did not materialize")
	}
	// The restored workspace is now focused and owns the canonical document; the old
	// workspace is a read-only mirror with the same live unsaved text.
	if bSession.rt.ed.Active().SharedMirror() || bSession.rt.ed.Active().ReadOnly() {
		t.Fatal("the restored workspace did not take ownership")
	}
	if got := bSession.rt.ed.Active().Buffer.Text(); got != ownerText {
		t.Fatalf("restored workspace text = %q, want the live owner text %q", got, ownerText)
	}
	if !aSession.rt.ed.Active().SharedMirror() || !aSession.rt.ed.Active().ReadOnly() {
		t.Fatal("the original workspace is not a read-only mirror")
	}
	// The stale unsaved stash text is kept in an untitled conflict tab, not written
	// into the shared buffer and not silently discarded.
	var conflict *editor.Document
	for i := 0; i < len(bSession.rt.ed.Tabs()); i++ {
		if d := bSession.rt.ed.DocAt(i); d != nil && d.Path == "" {
			conflict = d
			break
		}
	}
	if conflict == nil {
		t.Fatal("the stale restored unsaved copy was discarded")
	}
	if got := conflict.Buffer.Text(); got != "stale unsaved\n" {
		t.Fatalf("conflict tab text = %q", got)
	}
	if !strings.Contains(conflict.Status, "Restored unsaved copy") {
		t.Fatalf("conflict tab status = %q", conflict.Status)
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "one\ntwo\n" {
		t.Fatalf("the startup restore wrote the shared file: %q, %v", raw, err)
	}
}

// conflictDocFrom returns the untitled [shared conflict] tab of ed, or nil.
func conflictDocFrom(ed *editor.Editor) *editor.Document {
	if ed == nil {
		return nil
	}
	for i := 0; i < len(ed.Tabs()); i++ {
		d := ed.DocAt(i)
		if d != nil && d.Path == "" && d.Modified() && strings.Contains(d.Label(), "[shared conflict]") {
			return d
		}
	}
	return nil
}

// TestWorkspaceSharedBuffersConflictLabelSurvivesRepeatedRestore: a startup-restore
// conflict tab keeps its [shared conflict] label, text and pathless safety across an
// eviction/rematerialize cycle and a second one; the live owner's text and the disk file
// are untouched, and Save As registers the new file in the shared registry so a later
// restore cannot turn it into an independent copy.
func TestWorkspaceSharedBuffersConflictLabelSurvivesRepeatedRestore(t *testing.T) {
	isolateConfig(t)
	workspacesDir := t.TempDir()
	t.Setenv("GVIM_WORKSPACE_DIR", workspacesDir)
	rootA := t.TempDir()
	rootB := t.TempDir()
	path := filepath.Join(rootA, "shared.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := newWorkspaceManager(t.Context(), Options{
		Root:       rootA,
		SessionDir: t.TempDir(),
		Daemon:     &DaemonOptions{Mode: DaemonServe},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.closeAll)
	m.mu.Lock()
	aSession := m.sessions[m.active]
	m.mu.Unlock()
	if aSession == nil || aSession.rt == nil {
		t.Fatal("no foreground workspace runtime")
	}
	if err := aSession.rt.ed.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	aSession.rt.ed.Active().Buffer.Insert(buffer.Pos{}, "owner ")

	// Dormant B restores the same path with stale unsaved text.
	wsB, _, err := m.reg.Create(rootB, "B")
	if err != nil {
		t.Fatal(err)
	}
	idB := wsB.File.StableID
	dirB, err := m.reg.SessionDir(idB)
	if err != nil {
		t.Fatal(err)
	}
	stash, err := session.StashContent(dirB, "stale unsaved\n")
	if err != nil {
		t.Fatal(err)
	}
	layout := session.Session{Version: 1, Root: rootB, Files: []session.FileState{{Path: path, StashFile: stash, Line: 1}}}
	if err := session.Save(dirB, layout); err != nil {
		t.Fatal(err)
	}
	if _, err := m.reg.UpdateLayout(idB, &layout); err != nil {
		t.Fatal(err)
	}
	selectB := func() *workspaceSession {
		t.Helper()
		cmd, serr := m.Select(idB)
		runWorkspaceCmd(t, cmd, serr)
		m.mu.Lock()
		sess := m.sessions[idB]
		m.mu.Unlock()
		if sess == nil {
			t.Fatal("B was not materialized")
		}
		return sess
	}
	evictB := func() {
		t.Helper()
		m.materializedCap = 1
		m.evictToCap()
		m.materializedCap = 8
		m.mu.Lock()
		_, still := m.sessions[idB]
		m.mu.Unlock()
		if still {
			t.Fatal("B was not evicted")
		}
	}
	bSession := selectB()
	conflict := conflictDocFrom(bSession.rt.ed)
	if conflict == nil {
		t.Fatalf("no conflict tab after startup restore: %+v", bSession.rt.ed.Tabs())
	}
	wantText := conflict.Buffer.Text()
	wantLabel := conflict.Label()

	// A owns again; evict B; change the live owner text while B is dormant.
	if cmd, serr := m.Select(aSession.ws.File.StableID); true {
		runWorkspaceCmd(t, cmd, serr)
	}
	evictB()
	aSession.rt.ed.Active().InsertText("-more")
	ownerText := aSession.rt.ed.Active().Buffer.Text()

	for cycle := 1; cycle <= 2; cycle++ {
		bSession = selectB()
		if got := bSession.rt.ed.Active().Buffer.Text(); got != ownerText {
			t.Fatalf("cycle %d: the restored workspace did not take the live owner text: %q, want %q", cycle, got, ownerText)
		}
		got := conflictDocFrom(bSession.rt.ed)
		if got == nil {
			t.Fatalf("cycle %d: the conflict tab lost its label on restore: %+v", cycle, bSession.rt.ed.Tabs())
		}
		if got.Label() != wantLabel || got.Buffer.Text() != wantText {
			t.Fatalf("cycle %d: conflict tab = label %q text %q, want label %q text %q", cycle, got.Label(), got.Buffer.Text(), wantLabel, wantText)
		}
		if got.Path != "" {
			t.Fatalf("cycle %d: the conflict tab regained a file path: %q", cycle, got.Path)
		}
		if raw, err := os.ReadFile(path); err != nil || string(raw) != "one\ntwo\n" {
			t.Fatalf("cycle %d: restore wrote the shared file: %q, %v", cycle, raw, err)
		}
		// Back to A and evict B again for the next cycle.
		if cmd, serr := m.Select(aSession.ws.File.StableID); true {
			runWorkspaceCmd(t, cmd, serr)
		}
		evictB()
	}

	// Save As the restored conflict tab: the new file becomes B's shared owner.
	bSession = selectB()
	conflict = conflictDocFrom(bSession.rt.ed)
	if conflict == nil {
		t.Fatal("no conflict tab before save-as")
	}
	idx := -1
	for i := 0; i < len(bSession.rt.ed.Tabs()); i++ {
		if bSession.rt.ed.DocAt(i) == conflict {
			idx = i
		}
	}
	bSession.rt.ed.ActivateTab(idx)
	newPath := filepath.Join(rootB, "kept.txt")
	if err := bSession.rt.ed.SaveAs(newPath); err != nil {
		t.Fatalf("save the restored conflict: %v", err)
	}
	if raw, err := os.ReadFile(newPath); err != nil || string(raw) != wantText {
		t.Fatalf("new file = %q, %v, want the conflict text", raw, err)
	}
	if got := m.shared.OwnerLabel(m.shared.CanonicalPath(newPath)); got != "B" {
		t.Fatalf("new file owner = %q, want B", got)
	}
	// One more cycle: the new file is restored and re-registered as shared.
	if cmd, serr := m.Select(aSession.ws.File.StableID); true {
		runWorkspaceCmd(t, cmd, serr)
	}
	evictB()
	bSession = selectB()
	found := false
	for i := 0; i < len(bSession.rt.ed.Tabs()); i++ {
		if d := bSession.rt.ed.DocAt(i); d != nil && d.Path == newPath {
			found = true
		}
	}
	if !found {
		t.Fatalf("the saved-as file was not restored: %+v", bSession.rt.ed.Tabs())
	}
	if got := m.shared.OwnerLabel(m.shared.CanonicalPath(newPath)); got != "B" {
		t.Fatalf("after restore, the new file owner = %q, want B", got)
	}
}
