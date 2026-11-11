package app

// Snapshot-and-write capture of a workspace's session (#19a). The UI goroutine takes a
// consistent in-memory snapshot of the editor (layout, unsaved buffer texts, large-file
// overlays, terminal record, find history); the stash files and session JSON are written
// by a worker, so a switch never waits on disk. Ordering: the writes of one workspace are
// tracked as jobs, a close waits for the workspace's job, and closeAll waits for every
// job before the runtimes are closed. A crash between a snapshot and its write loses that
// last capture (documented limitation; the previous session file is intact).

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/restore"
	"github.com/tbxark/gvim/internal/terminal"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/session"
)

// captureSnapshot is the UI-goroutine snapshot of one workspace.
type captureSnapshot struct {
	layout    session.Session
	texts     map[int]string
	stamps    map[int][2]int64
	overlay   map[int]session.OverlayStash
	failed    []captureOverlay
	terminal  terminal.SavedState
	search    session.Search
	hasSearch bool
	explorer  tui.ExplorerState
}

// captureOverlay is a large-file stash that could not be restored earlier and stays.
type captureOverlay struct {
	path        string
	st          session.OverlayStash
	size, mtime int64
}

// saveJob is one asynchronous capture write.
type saveJob struct {
	done chan struct{}
	err  error
}

// snapshotCapture takes the consistent in-memory snapshot of a workspace on the UI
// goroutine. It mirrors restore.WriteSession's document inspection but writes nothing.
// The unsaved text is read through Document.ReadRange (the checked helper), never
// Buffer.Text, so a large document cannot be silently materialised as empty.
func (m *workspaceManager) snapshotCapture(s *workspaceSession) (*captureSnapshot, error) {
	snap := &captureSnapshot{
		texts:    map[int]string{},
		stamps:   map[int][2]int64{},
		overlay:  map[int]session.OverlayStash{},
		terminal: s.rt.model.TerminalState(),
		explorer: s.rt.model.ExplorerState(),
	}
	snap.layout.Root = snap.explorer.Root
	snap.layout.Explorer = snap.explorer.Open
	snap.layout.ShowHidden = snap.explorer.ShowHidden
	snap.layout.ShowIgnored = snap.explorer.ShowIgnored
	snap.layout.Sidebar = restore.SaveSidebarState(snap.explorer.Sidebar)
	// The pane layout is a pure model capture (no tab switching, no IO); the
	// writer below stores it in the session JSON with the layout.
	snap.layout.Layout = s.rt.model.SplitState()
	if st, ok := s.rt.model.SearchState(); ok {
		snap.search = session.Search{
			Find: st.Find, Replace: st.Replace,
			Case: st.Case, Word: st.Word, Regex: st.Regex, Confirm: st.ConfirmEach,
		}
		snap.hasSearch = true
	}
	ed := s.rt.ed
	tabs := ed.Tabs()
	// Read the MRU before the capture loop activates every tab (that changes the
	// history); it is stored as saved paths/labels just like restore.WriteSession.
	mruTabs := ed.RecentTabIndices()
	orig := -1
	for i, t := range tabs {
		if t.Active {
			orig = i
		}
	}
	defer func() {
		if orig >= 0 {
			ed.ActivateTab(orig)
		}
	}()
	var files []session.FileState
	savedByTab := make(map[int]int, len(tabs))
	active := 0
	for i := range tabs {
		ed.ActivateTab(i)
		doc := ed.Active()
		if doc == nil || doc.Virtual() || (doc.Path == "" && !doc.Modified()) {
			continue
		}
		fs := session.FileState{
			Path: doc.Path,
			Line: doc.Cursor.Line + 1,
			Col:  doc.Cursor.Col + 1,
			Top:  doc.Top,
		}
		if doc.Path == "" {
			fs.Label = doc.Label()
		}
		if view := doc.View(); view != nil {
			if view.PageView != nil {
				on := *view.PageView
				fs.PageView = &on
			}
			fs.PageWidth = view.PageWidth
		}
		if n := doc.VirtualLines(); n > 0 {
			fs.VirtualLines, fs.VirtualColumn = n, doc.VirtualLineColumn()
		}
		if doc.Large() {
			if st, ok := doc.OverlayStash(); ok {
				snap.overlay[len(files)] = st
			}
			if st, ok := doc.FailedStash(); ok {
				size, mtime, _ := doc.DiskStamp()
				snap.failed = append(snap.failed, captureOverlay{path: doc.Path, st: st, size: size, mtime: mtime})
			}
		} else if doc.Modified() {
			text, err := doc.ReadRange(buffer.Pos{}, doc.Buffer.End())
			if err != nil {
				return nil, fmt.Errorf("snapshot %s: %w", doc.Path, err)
			}
			snap.texts[len(files)] = text
			if size, mtime, ok := doc.DiskStamp(); ok {
				snap.stamps[len(files)] = [2]int64{size, mtime}
			}
		}
		if i == orig {
			active = len(files)
		}
		savedByTab[i] = len(files)
		files = append(files, fs)
	}
	for _, tabIndex := range mruTabs {
		fileIndex, ok := savedByTab[tabIndex]
		if !ok {
			continue // a virtual tab or an empty untitled buffer is not saved
		}
		snap.layout.MRU = append(snap.layout.MRU, session.MruFile{Path: files[fileIndex].Path, Label: files[fileIndex].Label})
	}
	for _, f := range snap.failed {
		files = append(files, session.FileState{Path: f.path})
	}
	files = append(files, s.rt.kept...)
	snap.layout.Active = active
	snap.layout.Files = files
	return snap, nil
}

// captureWriter is the ordered write queue of one workspace: snapshots are written in
// the order they were taken, so a newer snapshot can never be overwritten by an older
// one, and a switch never waits on the previous write.
//
// mu serializes the writes of this workspace and is held ACROSS writeSnapshot, which is
// what lets the exit path tell an in-flight (newest) write from an idle writer: the exit
// fallback TryLocks it, and only when it succeeds may it supersede the queued snapshots.
// seq numbers the snapshots, committed is the newest one written (or superseded by the
// fallback), and stopped means a newer path committed: queued snapshots must not write.
// The UI goroutine never takes mu (enqueue only bumps the atomic seq), so a slow disk
// cannot freeze Update.
type captureWriter struct {
	q         chan *captureWrite
	mu        sync.Mutex
	seq       atomic.Uint64
	committed uint64
	stopped   bool
}

// captureWrite is one queued snapshot write.
type captureWrite struct {
	sess *workspaceSession
	snap *captureSnapshot
	job  *saveJob
	seq  uint64
}

// enqueueCapture snapshots the workspace on the UI goroutine and queues its write on the
// workspace's ordered writer. The write (stash files, session JSON, terminal record,
// find history and the registry layout update) runs entirely off the UI goroutine.
func (m *workspaceManager) enqueueCapture(s *workspaceSession) *saveJob {
	if s == nil || s.rt == nil {
		return nil
	}
	if s.rt.keepSavedSession {
		// The authority was disconnected when this remote workspace started, so its
		// session was not restored and must not be overwritten by this empty runtime.
		job := &saveJob{done: make(chan struct{})}
		close(job.done)
		return job
	}
	job := &saveJob{done: make(chan struct{})}
	var snap *captureSnapshot
	var snapErr error
	if m.snapshotCaptureFn != nil {
		snap, snapErr = m.snapshotCaptureFn(s)
	} else {
		snap, snapErr = m.snapshotCapture(s)
	}
	if snapErr != nil {
		job.err = snapErr
		// enqueueCapture runs inside a runOnUI closure (hostCall). notify would re-enter
		// Do from the UI goroutine and freeze it; write the notice directly instead.
		m.notifyOnUI(fmt.Errorf("snapshot workspace %q: %w", s.ws.File.Label, snapErr))
		close(job.done)
		return job
	}
	m.mu.Lock()
	m.writes = append(m.writes, job)
	w := m.writers[s.ws.File.StableID]
	if w == nil {
		w = &captureWriter{q: make(chan *captureWrite, 16)}
		m.writers[s.ws.File.StableID] = w
		go m.runWriter(w)
	}
	m.mu.Unlock()
	// The UI goroutine must not wait on the writer's lock; the sequence is atomic.
	seq := w.seq.Add(1)
	w.q <- &captureWrite{sess: s, snap: snap, job: job, seq: seq}
	return job
}

// runWriter writes one workspace's snapshots in queue order. The per-workspace lock is
// held across the write and the commit, so a snapshot either writes the newest confirmed
// state or is skipped as superseded; the delayed-writer regression covers the case the
// exit path raced a queued older snapshot.
func (m *workspaceManager) runWriter(w *captureWriter) {
	for c := range w.q {
		if m.captureWriterGate != nil {
			m.captureWriterGate()
		}
		write := m.writeSnapshot
		if m.writeSnapshotFn != nil {
			write = m.writeSnapshotFn
		}
		w.mu.Lock()
		if w.stopped || c.seq <= w.committed {
			// A newer path (the exit fallback, or a later snapshot) already committed:
			// this snapshot must not overwrite it, not even atomically with older data.
			w.mu.Unlock()
			c.job.err = nil
			close(c.job.done)
			continue
		}
		err := write(c.sess, c.snap)
		if err == nil && c.sess.rt.sessionDir != "" {
			// Registry disk work stays off the UI goroutine; the registry is
			// concurrency-safe and this is the newest written layout of the workspace.
			layout := c.snap.layout
			_, _ = m.reg.UpdateLayout(c.sess.ws.File.StableID, &layout)
			_, _ = m.reg.Touch(c.sess.ws.File.StableID, m.now())
			w.committed = c.seq
		}
		w.mu.Unlock()
		if err != nil {
			m.notify(fmt.Errorf("save workspace %q: %w", c.sess.ws.File.Label, err))
		}
		c.job.err = err
		close(c.job.done)
	}
}

// writeSnapshot writes the stash files, the session JSON, the terminal record and the
// find history of one snapshot into the workspace's session directory.
func (m *workspaceManager) writeSnapshot(s *workspaceSession, snap *captureSnapshot) error {
	if s.rt.sessionDir == "" {
		return nil
	}
	dir := s.rt.sessionDir
	layout := snap.layout
	for i, text := range snap.texts {
		name, err := session.StashContent(dir, text)
		if err != nil {
			return err
		}
		layout.Files[i].StashFile = name
		if stamp, ok := snap.stamps[i]; ok {
			layout.Files[i].Size, layout.Files[i].MTimeUnixNano = stamp[0], stamp[1]
		}
	}
	for i, st := range snap.overlay {
		name, err := session.StashOverlay(dir, st)
		if err != nil {
			return err
		}
		layout.Files[i].OverlayFile = name
	}
	for _, f := range snap.failed {
		name, err := session.StashOverlay(dir, f.st)
		if err != nil {
			return err
		}
		layout.Files = append(layout.Files, session.FileState{
			Path: f.path, OverlayFile: name, Size: f.size, MTimeUnixNano: f.mtime,
		})
	}
	if err := session.Save(dir, layout); err != nil {
		return err
	}
	snap.layout = layout
	if err := restore.SaveTerminal(dir, snap.terminal); err != nil {
		return err
	}
	if snap.hasSearch {
		if err := session.SaveSearch(dir, snap.search); err != nil {
			return err
		}
	}
	return nil
}

// waitWrites waits for every outstanding capture write, up to timeout per job. It reports
// the errors (a failed save is a lost capture and must be visible).
func (m *workspaceManager) waitWrites(timeout time.Duration) error {
	m.mu.Lock()
	jobs := append([]*saveJob(nil), m.writes...)
	m.mu.Unlock()
	var errs []error
	for _, job := range jobs {
		select {
		case <-job.done:
			if job.err != nil {
				errs = append(errs, job.err)
			}
		case <-time.After(timeout):
			errs = append(errs, errors.New("workspace: capture write did not finish"))
		}
	}
	return errors.Join(errs...)
}
