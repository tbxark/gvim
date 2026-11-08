package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tbxark/gvim/session"
)

// The daemon-exit durability tests: a failed, timed-out or fallback-failing exit save
// must never look like a clean exit, and the unsaved text must be confirmed durable
// (capture, then the synchronous fallback, then the crash-recovery copy).

// materializeSecond creates and materializes a second workspace (the helper tests switch
// to it), with a file open and unsaved text, and returns its registry session directory.
func materializeSecond(t *testing.T, m *workspaceManager) (id, sessionDir string) {
	t.Helper()
	root := t.TempDir()
	cmd, err := m.Create(root, "second")
	if err != nil || cmd == nil {
		t.Fatalf("Create = %v, %v", cmd, err)
	}
	cmd()
	for _, e := range m.Entries() {
		if e.Active {
			id = e.ID
		}
	}
	if id == "" {
		t.Fatal("no active second workspace")
	}
	path := filepath.Join(root, "b.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	sess := m.sessions[id]
	m.mu.Unlock()
	openInModel(t, sess.rt.model, path)
	doc := sess.rt.ed.Active()
	if doc == nil || doc.Path != path {
		t.Fatalf("active document = %+v", doc)
	}
	doc.InsertText("UNSAVED-")
	sessionDir, err = m.reg.SessionDir(id)
	if err != nil {
		t.Fatal(err)
	}
	return id, sessionDir
}

// addUnsavedForeground opens a file in the foreground workspace and types into it.
func addUnsavedForeground(t *testing.T, m *workspaceManager, foregroundID string) string {
	t.Helper()
	m.mu.Lock()
	sess := m.sessions[foregroundID]
	m.mu.Unlock()
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	openInModel(t, sess.rt.model, path)
	doc := sess.rt.ed.Active()
	if doc == nil || doc.Path != path {
		t.Fatalf("active document = %+v", doc)
	}
	doc.InsertText("UNSAVED-")
	dir, err := m.reg.SessionDir(foregroundID)
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func sessionHasStash(t *testing.T, dir string) bool {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "session.json"))
	if err != nil {
		return false
	}
	return strings.Contains(string(raw), "stashFile")
}

func TestDaemonExitCheckedCleanSavesEveryWorkspace(t *testing.T) {
	m, foregroundID := newWorkspaceManagerTest(t)
	fgDir := addUnsavedForeground(t, m, foregroundID)
	_, secondDir := materializeSecond(t, m)
	if err := m.closeAllChecked(5 * time.Second); err != nil {
		t.Fatalf("clean exit returned an error: %v", err)
	}
	if !sessionHasStash(t, fgDir) {
		t.Errorf("the foreground workspace lost its unsaved text (session dir %s)", fgDir)
	}
	if !sessionHasStash(t, secondDir) {
		t.Errorf("the second workspace lost its unsaved text (session dir %s)", secondDir)
	}
	m.mu.Lock()
	left := len(m.sessions)
	m.mu.Unlock()
	if left != 0 {
		t.Errorf("the manager kept %d sessions after the checked exit", left)
	}
}

func TestDaemonExitCheckedAsyncFailureIsNotSuccess(t *testing.T) {
	m, foregroundID := newWorkspaceManagerTest(t)
	fgDir := addUnsavedForeground(t, m, foregroundID)
	m.writeSnapshotFn = func(*workspaceSession, *captureSnapshot) error {
		return errors.New("disk full")
	}
	err := m.closeAllChecked(5 * time.Second)
	if err == nil {
		t.Fatal("a failed exit capture returned success")
	}
	if !strings.Contains(err.Error(), "capture write failed") || !strings.Contains(err.Error(), "fallback save kept the session") {
		t.Fatalf("exit error = %v", err)
	}
	if !sessionHasStash(t, fgDir) {
		t.Errorf("the fallback did not keep the unsaved text (session dir %s)", fgDir)
	}
}

// TestDaemonExitCheckedFallbackFailureKeepsRecovery checks the worst case: the capture
// write fails AND the synchronous save fails, so the crash-recovery copy is written and
// the exit is still not a success.
func TestDaemonExitCheckedFallbackFailureKeepsRecovery(t *testing.T) {
	m, foregroundID := newWorkspaceManagerTest(t)
	addUnsavedForeground(t, m, foregroundID)
	m.writeSnapshotFn = func(*workspaceSession, *captureSnapshot) error {
		return errors.New("disk full")
	}
	m.mu.Lock()
	sess := m.sessions[foregroundID]
	m.mu.Unlock()
	// A session directory that cannot be created (its parent is a regular file) makes the
	// synchronous save fail without touching the real disk layout.
	blocker := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	sess.rt.sessionDir = filepath.Join(blocker, "session")
	err := m.closeAllChecked(5 * time.Second)
	if err == nil {
		t.Fatal("a failed fallback save returned success")
	}
	if !strings.Contains(err.Error(), "fallback save failed") {
		t.Fatalf("exit error = %v", err)
	}
	// The recovery copy is the safety net the error has to name (or report as failed).
	if !strings.Contains(err.Error(), "crash-recovery copy") {
		t.Fatalf("the exit error does not mention the recovery copy: %v", err)
	}
}

func TestDaemonExitCheckedTimeoutIsBoundedAndReported(t *testing.T) {
	m, foregroundID := newWorkspaceManagerTest(t)
	fgDir := addUnsavedForeground(t, m, foregroundID)
	realWrite := m.writeSnapshot
	release := make(chan struct{})
	entered := make(chan struct{})
	wrote := make(chan struct{})
	var enteredOnce, wroteOnce sync.Once
	// The fake blocks INSIDE writeSnapshot, so the writer holds the workspace lock: the
	// exit fallback must not write over it (that is the overwrite race).
	m.writeSnapshotFn = func(s *workspaceSession, snap *captureSnapshot) error {
		enteredOnce.Do(func() { close(entered) })
		<-release
		err := realWrite(s, snap)
		wroteOnce.Do(func() { close(wrote) })
		return err
	}
	m.mu.Lock()
	sess := m.sessions[foregroundID]
	m.mu.Unlock()
	// Put the writer inside writeSnapshot (holding the lock) before the exit begins.
	m.enqueueCapture(sess)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the writer never entered writeSnapshot")
	}
	start := time.Now()
	err := m.closeAllChecked(80 * time.Millisecond)
	elapsed := time.Since(start)
	if err == nil || !strings.Contains(err.Error(), "did not finish before the exit deadline") {
		t.Fatalf("timeout exit error = %v", err)
	}
	if !strings.Contains(err.Error(), "still in flight") || !strings.Contains(err.Error(), "crash-recovery copy was kept") {
		t.Fatalf("the exit error must name the in-flight write and the recovery copy: %v", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("the exit wait took %s, want one bounded deadline", elapsed)
	}
	// The fallback must NOT have written the session file while the write held the lock.
	if _, statErr := os.Stat(filepath.Join(fgDir, "session.json")); statErr == nil {
		entries, _ := os.ReadDir(fgDir)
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("the fallback wrote the session while the newest capture was in flight (err=%v, dir=%v)", err, names)
	}
	close(release)
	select {
	case <-wrote:
	case <-time.After(5 * time.Second):
		t.Fatal("the late writer never finished")
	}
	// The late (newest) write is the one that lands, and it has the unsaved text.
	if !sessionHasStash(t, fgDir) {
		t.Errorf("the in-flight write did not land the unsaved text (session dir %s)", fgDir)
	}
	// Wait for the writer's post-write registry update too, so the test's TempDir cleanup
	// never races a goroutine still writing under it.
	if err := m.waitWrites(5 * time.Second); err != nil {
		t.Fatalf("the late writer reported %v", err)
	}
}

// TestDaemonExitFallbackSupersedesQueuedOlderWrite is the delayed-writer regression: an
// older snapshot is queued but its writer is held BEFORE the per-workspace lock, then the
// exit fallback commits the newer live state. When the delayed writer finally runs it must
// skip (commit token), never overwrite the confirmed newer session with its older snapshot.
func TestDaemonExitFallbackSupersedesQueuedOlderWrite(t *testing.T) {
	m, foregroundID := newWorkspaceManagerTest(t)
	m.mu.Lock()
	sess := m.sessions[foregroundID]
	m.mu.Unlock()
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	openInModel(t, sess.rt.model, path)
	doc := sess.rt.ed.Active()
	doc.InsertText("OLD-")
	sessionDir, err := m.reg.SessionDir(foregroundID)
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	m.captureWriterGate = func() {
		once.Do(func() { close(entered) })
		<-release
	}
	old := m.enqueueCapture(sess) // snapshot "OLD-", delayed before the lock
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the delayed writer never reached the gate")
	}
	// The live state is newer than the queued snapshot.
	doc.InsertText("NEW-")
	committed, ferr := m.tryCommitExitCapture(sess)
	if !committed || ferr != nil {
		t.Fatalf("tryCommitExitCapture = %v, %v", committed, ferr)
	}
	want := stashedText(t, sessionDir)
	if !strings.Contains(want, "NEW-") {
		t.Fatalf("the fallback did not save the live state: %q", want)
	}
	close(release)
	<-old.done
	if err := m.waitWrites(5 * time.Second); err != nil {
		t.Fatalf("the delayed writer reported %v", err)
	}
	if got := stashedText(t, sessionDir); got != want {
		t.Fatalf("the delayed writer overwrote the confirmed state:\nhave %q\nwant %q", got, want)
	}
}

// stashedText reads the unsaved text the workspace's session file points at.
func stashedText(t *testing.T, dir string) string {
	t.Helper()
	layout, ok, err := session.Load(dir)
	if err != nil || !ok {
		t.Fatalf("load session in %s: ok=%v err=%v", dir, ok, err)
	}
	for _, f := range layout.Files {
		if f.StashFile == "" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, f.StashFile))
		if err != nil {
			t.Fatalf("read stash %s: %v", f.StashFile, err)
		}
		return string(raw)
	}
	return ""
}
