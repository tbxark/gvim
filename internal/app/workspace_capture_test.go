package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/restore"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/session"
)

// pumpModel runs a model command tree to quiescence: commands are run like the Bubble Tea
// loop runs them, and their messages are fed back into the model. It is for small local
// file opens; a command that blocks (terminal I/O) would need the dock pump.
func pumpModel(t *testing.T, m *tui.Model, first tea.Cmd) {
	t.Helper()
	var run func(tea.Cmd)
	msgs := make(chan tea.Msg, 128)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		go func() {
			if msg := c(); msg != nil {
				select {
				case msgs <- msg:
				case <-time.After(5 * time.Second):
				}
			}
		}()
	}
	run(first)
	deadline := time.Now().Add(10 * time.Second)
	quiet := time.Now()
	for time.Now().Before(deadline) {
		select {
		case msg := <-msgs:
			if batch, ok := msg.(tea.BatchMsg); ok {
				for _, c := range batch {
					run(c)
				}
				continue
			}
			_, next := m.Update(msg)
			run(next)
			quiet = time.Now()
		case <-time.After(20 * time.Millisecond):
			if time.Since(quiet) > 200*time.Millisecond {
				return
			}
		}
	}
}

// openInModel opens path in the model the way an explorer activation does.
func openInModel(t *testing.T, m *tui.Model, path string) {
	t.Helper()
	_, cmd := m.Update(tui.OpenFileMessage(path))
	pumpModel(t, m, cmd)
}

// TestWorkspaceCaptureMatchesWriteSession compares the workspace capture with
// restore.WriteSession field for field for the same editor state: layout, cursor/top,
// active tab, explorer/sidebar and the unsaved stash.
func TestWorkspaceCaptureMatchesWriteSession(t *testing.T) {
	m, id := newWorkspaceManagerTest(t)
	m.mu.Lock()
	sess := m.sessions[id]
	m.mu.Unlock()
	root := sess.ws.File.Root
	pathA := filepath.Join(root, "a.txt")
	pathB := filepath.Join(root, "b.txt")
	if err := os.WriteFile(pathA, []byte("alpha\nbeta\ngamma\ndelta\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pathB, []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	openInModel(t, sess.rt.model, pathA)
	openInModel(t, sess.rt.model, pathB)
	doc := sess.rt.ed.Active()
	if doc == nil || doc.Path != pathB {
		t.Fatalf("active document = %+v", doc)
	}
	doc.InsertText("XX")
	doc.Cursor.Line, doc.Cursor.Col = 1, 2
	doc.Top = 1
	// Reference: the production session writer for this exact editor state.
	refDir := t.TempDir()
	if err := restore.WriteSession(sess.rt.ed, refDir, sess.rt.model.ExplorerState(), sess.rt.kept); err != nil {
		t.Fatalf("reference WriteSession: %v", err)
	}
	ref, ok, err := session.Load(refDir)
	if err != nil || !ok {
		t.Fatalf("reference session = %+v, %v, %v", ref, ok, err)
	}
	// Capture through the workspace layer.
	job := m.enqueueCapture(sess)
	<-job.done
	if job.err != nil {
		t.Fatalf("capture: %v", job.err)
	}
	got, ok, err := session.Load(sess.rt.sessionDir)
	if err != nil || !ok {
		t.Fatalf("captured session = %+v, %v, %v", got, ok, err)
	}
	if got.Root != ref.Root || got.Explorer != ref.Explorer || got.ShowHidden != ref.ShowHidden ||
		got.ShowIgnored != ref.ShowIgnored || got.Active != ref.Active {
		t.Fatalf("layout differs:\n got %+v\nref %+v", got, ref)
	}
	if !reflect.DeepEqual(got.Sidebar, ref.Sidebar) {
		t.Fatalf("sidebar differs:\n got %+v\nref %+v", got.Sidebar, ref.Sidebar)
	}
	if len(got.Files) != len(ref.Files) {
		t.Fatalf("files = %d, want %d", len(got.Files), len(ref.Files))
	}
	for i := range ref.Files {
		g, r := got.Files[i], ref.Files[i]
		gStash, rStash := g.StashFile, r.StashFile
		g.StashFile, r.StashFile = "", ""
		if g != r {
			t.Fatalf("file %d differs:\n got %+v\nref %+v", i, g, r)
		}
		if (gStash == "") != (rStash == "") {
			t.Fatalf("file %d stash presence differs: got %q ref %q", i, gStash, rStash)
		}
	}
}

// TestWorkspaceCaptureWritesAreSerialized checks a delayed first write cannot let an
// older snapshot overwrite a newer one: the per-workspace writer writes in snapshot order.
func TestWorkspaceCaptureWritesAreSerialized(t *testing.T) {
	m, id := newWorkspaceManagerTest(t)
	m.mu.Lock()
	sess := m.sessions[id]
	m.mu.Unlock()
	realWrite := m.writeSnapshot
	var mu sync.Mutex
	var order []string
	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var once sync.Once
	calls := 0
	m.writeSnapshotFn = func(s *workspaceSession, snap *captureSnapshot) error {
		mu.Lock()
		n := calls
		calls++
		order = append(order, fmt.Sprintf("start%d", n))
		mu.Unlock()
		switch n {
		case 0:
			once.Do(func() { close(firstStarted) })
			<-releaseFirst
			mu.Lock()
			order = append(order, "end0")
			mu.Unlock()
		case 1:
			close(secondStarted)
			mu.Lock()
			order = append(order, "end1")
			mu.Unlock()
		}
		return realWrite(s, snap)
	}
	first := m.enqueueCapture(sess)
	select {
	case <-firstStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("the first write never started")
	}
	second := m.enqueueCapture(sess)
	select {
	case <-secondStarted:
		t.Fatal("the second write started before the first finished")
	case <-time.After(200 * time.Millisecond):
	}
	close(releaseFirst)
	select {
	case <-first.done:
	case <-time.After(10 * time.Second):
		t.Fatal("the first write did not finish")
	}
	select {
	case <-second.done:
	case <-time.After(10 * time.Second):
		t.Fatal("the second write did not finish")
	}
	mu.Lock()
	defer mu.Unlock()
	if order[0] != "start0" || order[1] != "end0" || order[2] != "start1" {
		t.Fatalf("write order = %v, want start0, end0, start1", order)
	}
}

// TestWorkspaceCloseWaitsForPendingWrite checks a close does not deadlock while an earlier
// capture write is still running: it waits for its own write and completes.
func TestWorkspaceCloseWaitsForPendingWrite(t *testing.T) {
	m, _ := newWorkspaceManagerTest(t)
	// A second, materialized workspace to close.
	root := t.TempDir()
	cmd, err := m.Create(root, "second")
	if err != nil || cmd == nil {
		t.Fatalf("Create = %v, %v", cmd, err)
	}
	cmd()
	var idB string
	for _, e := range m.Entries() {
		if e.Active {
			idB = e.ID
		}
	}
	if idB == "" {
		t.Fatal("no active second workspace")
	}
	realWrite := m.writeSnapshot
	release := make(chan struct{})
	started := make(chan struct{})
	var once sync.Once
	m.writeSnapshotFn = func(s *workspaceSession, snap *captureSnapshot) error {
		once.Do(func() { close(started) })
		<-release
		return realWrite(s, snap)
	}
	m.mu.Lock()
	sess := m.sessions[idB]
	m.mu.Unlock()
	pending := m.enqueueCapture(sess)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the blocking write never started")
	}
	done := make(chan struct{})
	go func() {
		closeCmd, _ := m.Close(idB)
		if closeCmd != nil {
			closeCmd()
		}
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("Close returned while the write was still blocked")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Close did not return after the write finished")
	}
	<-pending.done
}

// TestWorkspaceCloseAllWithPendingWrite reports a failed pending write and still closes.
func TestWorkspaceCloseAllWithPendingWrite(t *testing.T) {
	m, _ := newWorkspaceManagerTest(t)
	m.writeSnapshotFn = func(*workspaceSession, *captureSnapshot) error {
		return errors.New("disk full")
	}
	m.mu.Lock()
	sess := m.sessions[m.active]
	m.mu.Unlock()
	job := m.enqueueCapture(sess)
	<-job.done
	if job.err == nil {
		t.Fatal("the failing write reported no error")
	}
	if err := m.waitWrites(5 * time.Second); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("waitWrites = %v, want the write error", err)
	}
	// closeAll must still close (it reports the failure and exits the runtime).
	closed := make(chan struct{})
	go func() { m.closeAll(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("closeAll hung after a failed write")
	}
}

// TestCloseAwaitsCaptureWriteBeforeExit checks the asynchronous capture is awaited before
// the runtime's synchronous close save: while the writer is blocked no session file may
// appear (exit has not run), and after release the close completes with the unsaved text
// saved.
func TestCloseAwaitsCaptureWriteBeforeExit(t *testing.T) {
	m, _ := newWorkspaceManagerTest(t)
	rootB := t.TempDir()
	cmd, err := m.Create(rootB, "second")
	if err != nil || cmd == nil {
		t.Fatalf("Create = %v, %v", cmd, err)
	}
	cmd()
	var idB string
	for _, e := range m.Entries() {
		if e.Active {
			idB = e.ID
		}
	}
	if idB == "" {
		t.Fatal("no active second workspace")
	}
	m.mu.Lock()
	sessB := m.sessions[idB]
	m.mu.Unlock()
	pathB := filepath.Join(rootB, "b.txt")
	if err := os.WriteFile(pathB, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	openInModel(t, sessB.rt.model, pathB)
	doc := sessB.rt.ed.Active()
	if doc == nil || doc.Path != pathB {
		t.Fatalf("active document = %+v", doc)
	}
	doc.InsertText("UNSAVED-")
	sessionDir, err := m.reg.SessionDir(idB)
	if err != nil {
		t.Fatal(err)
	}
	sessionPath := filepath.Join(sessionDir, "session.json")
	// The new workspace has no session file yet: its runtime has not been captured.
	if _, err := os.Stat(sessionPath); err == nil {
		t.Fatal("unexpected pre-existing session file")
	}
	realWrite := m.writeSnapshot
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	m.writeSnapshotFn = func(s *workspaceSession, snap *captureSnapshot) error {
		once.Do(func() { close(started) })
		<-release
		return realWrite(s, snap)
	}
	closeCmd, err := m.Close(idB)
	if err != nil || closeCmd == nil {
		t.Fatalf("Close = %v, %v", closeCmd, err)
	}
	closed := make(chan struct{})
	go func() {
		closeCmd()
		close(closed)
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the close's capture write never started")
	}
	// The writer is blocked: the runtime's synchronous save must not have run yet.
	if _, err := os.Stat(sessionPath); err == nil {
		t.Fatal("the close saved the session before the queued capture write finished")
	}
	select {
	case <-closed:
		t.Fatal("Close returned while the capture write was still blocked")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("Close did not return after the write finished")
	}
	raw, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("no session saved by capture or fallback: %v", err)
	}
	if !strings.Contains(string(raw), "stashFile") {
		t.Fatalf("the closed workspace lost its unsaved text: %s", raw)
	}
	if m.lastNotice != "" {
		t.Fatalf("a successful close reported: %q", m.lastNotice)
	}
}

// TestCloseFailureReportedAndFallbackSaves checks a failed capture write before close is
// reported (not silently dropped) and the synchronous fallback still saves the text.
func TestCloseFailureReportedAndFallbackSaves(t *testing.T) {
	m, _ := newWorkspaceManagerTest(t)
	rootB := t.TempDir()
	cmd, err := m.Create(rootB, "second")
	if err != nil || cmd == nil {
		t.Fatalf("Create = %v, %v", cmd, err)
	}
	cmd()
	var idB string
	for _, e := range m.Entries() {
		if e.Active {
			idB = e.ID
		}
	}
	m.mu.Lock()
	sessB := m.sessions[idB]
	m.mu.Unlock()
	pathB := filepath.Join(rootB, "b.txt")
	if err := os.WriteFile(pathB, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	openInModel(t, sessB.rt.model, pathB)
	sessB.rt.ed.Active().InsertText("UNSAVED-")
	m.writeSnapshotFn = func(*workspaceSession, *captureSnapshot) error {
		return errors.New("disk full")
	}
	closeCmd, err := m.Close(idB)
	if err != nil || closeCmd == nil {
		t.Fatalf("Close = %v, %v", closeCmd, err)
	}
	closeCmd()
	if !strings.Contains(m.lastNotice, "disk full") {
		t.Fatalf("the failed capture was not reported: %q", m.lastNotice)
	}
	dir, err := m.reg.SessionDir(idB)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "session.json"))
	if err != nil {
		t.Fatalf("the fallback close save wrote no session: %v", err)
	}
	if !strings.Contains(string(raw), "stashFile") {
		t.Fatalf("the fallback close save lost the unsaved text: %s", raw)
	}
}

// TestCloseDuringMaterializationDoesNotResurrect checks closing a workspace while its
// switch materialization is in flight cancels/invalidates that build: Close returns, the
// workspace is removed, the build may finish but must not commit or recreate it.
func TestCloseDuringMaterializationDoesNotResurrect(t *testing.T) {
	m, foregroundID := newWorkspaceManagerTest(t)
	dormant := createDormant(t, m)
	realBuild := m.build
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	m.build = func(ctx context.Context, opts Options) (*runtime, error) {
		if opts.Root == dormant.File.Root {
			once.Do(func() { close(started) })
			<-release
			// Ignore cancellation on purpose: without the close's sequence
			// invalidation this build would commit after the close.
			return realBuild(context.WithoutCancel(ctx), opts)
		}
		return realBuild(ctx, opts)
	}
	selectCmd, err := m.Select(dormant.File.StableID)
	if err != nil || selectCmd == nil {
		t.Fatalf("Select = %v, %v", selectCmd, err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the materialization never started")
	}
	// Close must not wait for the blocked build; it cancels/invalidates it.
	closeCmd, err := m.Close(dormant.File.StableID)
	if err != nil || closeCmd == nil {
		t.Fatalf("Close = %v, %v", closeCmd, err)
	}
	closed := make(chan struct{})
	go func() {
		closeCmd()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close waited for the in-flight materialization")
	}
	if _, ok, err := m.reg.Get(dormant.File.StableID); err != nil || ok {
		t.Fatalf("the closed workspace is still registered: ok=%v err=%v", ok, err)
	}
	// Let the stale build finish and try to commit.
	close(release)
	select {
	case <-selectCmdDone(selectCmd):
	case <-time.After(15 * time.Second):
		t.Fatal("the superseded switch did not finish")
	}
	entries := m.Entries()
	for _, e := range entries {
		if e.ID == dormant.File.StableID {
			t.Fatalf("the closed workspace was recreated: %+v", entries)
		}
	}
	activeID := ""
	for _, e := range entries {
		if e.Active {
			activeID = e.ID
		}
	}
	if activeID != foregroundID {
		t.Fatalf("the stale switch landed: active %q, want %q", activeID, foregroundID)
	}
	m.mu.Lock()
	_, materialized := m.sessions[dormant.File.StableID]
	m.mu.Unlock()
	if materialized {
		t.Fatal("the stale build stayed materialized after the close")
	}
	if _, ok, _ := m.reg.Get(dormant.File.StableID); ok {
		t.Fatal("the stale build recreated the registry file")
	}
}

// selectCmdDone runs a workspace command off the test goroutine and closes the result.
func selectCmdDone(cmd tui.WorkspaceCmd) chan struct{} {
	done := make(chan struct{})
	go func() {
		cmd()
		close(done)
	}()
	return done
}

// TestCloseRetainsWorkspaceWhenBothWritesFail checks a close whose queued capture fails
// AND whose synchronous fallback save fails keeps the workspace, runtime and registry
// file, and returns/propagates the error (protocol path) instead of closing silently.
func TestCloseRetainsWorkspaceWhenBothWritesFail(t *testing.T) {
	m, foregroundID := newWorkspaceManagerTest(t)
	rootB := t.TempDir()
	cmd, err := m.Create(rootB, "second")
	if err != nil || cmd == nil {
		t.Fatalf("Create = %v, %v", cmd, err)
	}
	cmd()
	var idB string
	for _, e := range m.Entries() {
		if e.Active {
			idB = e.ID
		}
	}
	if idB == "" || idB == foregroundID {
		t.Fatalf("no active second workspace: %+v", m.Entries())
	}
	m.mu.Lock()
	sessB := m.sessions[idB]
	m.mu.Unlock()
	pathB := filepath.Join(rootB, "b.txt")
	if err := os.WriteFile(pathB, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	openInModel(t, sessB.rt.model, pathB)
	sessB.rt.ed.Active().InsertText("UNSAVED-")
	m.writeSnapshotFn = func(*workspaceSession, *captureSnapshot) error {
		return errors.New("capture boom")
	}
	dir, err := m.reg.SessionDir(idB)
	if err != nil {
		t.Fatal(err)
	}
	// Both the ordered capture write and the synchronous fallback save write here; a
	// read-only directory makes both fail.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	err = m.closeSync(idB) // the protocol/daemon close path returns the error
	if err == nil {
		t.Fatal("the close reported success although neither write worked")
	}
	if !strings.Contains(err.Error(), "capture boom") || !strings.Contains(err.Error(), "fallback save failed") {
		t.Fatalf("close error = %v, want both failures named", err)
	}
	if !strings.Contains(m.lastNotice, "fallback save failed") {
		t.Fatalf("the failure was not reported to the user: %q", m.lastNotice)
	}
	// Nothing was closed or removed: the registry file, the runtime and the entries stay.
	if ws, ok, gerr := m.reg.Get(idB); gerr != nil || !ok || ws.File.Root == "" {
		t.Fatalf("the workspace was removed after a failed close: ok=%v err=%v", ok, gerr)
	}
	m.mu.Lock()
	_, materialized := m.sessions[idB]
	m.mu.Unlock()
	if !materialized {
		t.Fatal("the runtime was closed after a failed close")
	}
	found := false
	for _, e := range m.Entries() {
		if e.ID == idB {
			found = true
		}
	}
	if !found {
		t.Fatal("the workspace disappeared from the list after a failed close")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "session.json")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("a session was written although both writers failed: %v", statErr)
	}
	// Once the directory is writable and the writer works, the same close succeeds and
	// keeps the session data.
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	m.writeSnapshotFn = nil
	if err := m.closeSync(idB); err != nil {
		t.Fatalf("retried close: %v", err)
	}
	if _, ok, _ := m.reg.Get(idB); ok {
		t.Fatal("the retried close kept the workspace registered")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "session.json"))
	if err != nil {
		t.Fatalf("the retried close saved no session: %v", err)
	}
	if !strings.Contains(string(raw), "stashFile") {
		t.Fatalf("the retried close lost the unsaved text: %s", raw)
	}
}
