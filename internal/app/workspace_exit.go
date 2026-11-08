package app

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

// The daemon-exit durability path (#19a review). The plain closeAll wrapper keeps the
// historical shape (tests and deferred cleanup call it and ignore the outcome); this
// checked form is what the daemon's exit path uses, so a failed capture can never be
// mistaken for a clean exit.

// workspaceExitTimeout bounds the WHOLE daemon-exit capture wait, not each write: the
// writes of every workspace share one deadline, so N hung writers cost one timeout.
var workspaceExitTimeout = 30 * time.Second

// exitCapture is one workspace's snapshot job for the exit wait.
type exitCapture struct {
	sess *workspaceSession
	job  *saveJob
	err  error // the capture error or the deadline miss, nil when the write finished clean
	// inFlight is set when the writer held the workspace lock at the deadline: the exit
	// must not write the session itself (the in-flight write owns the newest snapshot).
	inFlight bool
}

// closeAllChecked persists and closes every materialized workspace, active first, and
// returns an error when any workspace's unsaved data could not be CONFIRMED durable. It is
// the closeAll the daemon's exit path uses:
//
//   - every workspace is snapshotted (the UI is gone; the snapshot reads the runtime) and
//     queued on its ordered writer;
//   - the writes share ONE deadline (workspaceExitTimeout) instead of a per-write one;
//   - a failed or timed-out capture is retried with the runtime's synchronous save (the
//     same fallback closeWorkspace uses) before anything is closed;
//   - when that fallback also fails, the crash-recovery copy the runtime keeps is written
//     so the text survives for the next start, and the path is named in the error;
//   - the runtimes are closed only after those attempts, and an error is returned whenever
//     a capture failed or timed out, even if the fallback rescued the text: the daemon then
//     exits non-zero instead of reporting a clean exit after a failed write.
//
// Limits, documented rather than hidden: a capture still running at the deadline is left to
// finish (the fallback writes the same snapshot, and both writers' files are atomic and
// content-addressed, so the last writer wins with identical data); a process killed between
// the snapshot and its write loses that last capture while the previous session file stays
// intact; a workspace whose runtime has no session directory writes nothing (the registry
// always gives daemon workspaces one, so this is a guard, not a normal path).
func (m *workspaceManager) closeAllChecked(timeout time.Duration) error {
	m.mu.Lock()
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	active := m.sessions[m.active]
	delete(m.sessions, m.active)
	rest := make([]*workspaceSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		rest = append(rest, s)
	}
	m.mu.Unlock()
	sort.Slice(rest, func(i, j int) bool {
		return rest[i].ws.File.StableID < rest[j].ws.File.StableID
	})

	order := make([]*workspaceSession, 0, len(rest)+1)
	if active != nil {
		order = append(order, active)
	}
	order = append(order, rest...)

	captures := make([]*exitCapture, 0, len(order))
	for _, s := range order {
		captures = append(captures, &exitCapture{sess: s, job: m.enqueueCapture(s)})
	}

	// One deadline for every write.
	deadline := time.Now().Add(timeout)
	for _, c := range captures {
		if c.job == nil {
			continue
		}
		if !waitExitJob(c.job, deadline) {
			c.err = errors.New("capture write did not finish before the exit deadline")
			continue
		}
		c.err = c.job.err
	}

	var errs []error
	for _, c := range captures {
		if c.err == nil {
			continue
		}
		label := c.sess.ws.File.Label
		base := fmt.Errorf("save workspace %q on exit: the capture write failed: %w", label, c.err)
		committed, ferr := m.tryCommitExitCapture(c.sess)
		switch {
		case !committed:
			// A writer is inside writeSnapshot holding this workspace's lock with the
			// NEWEST snapshot. Writing the fallback now would be the overwrite race the
			// commit token exists to prevent: leave that write to finish (it is the newest
			// capture), skip this runtime's own exit save so it cannot race it either, and
			// keep the recovery copy as the last resort.
			c.inFlight = true
			errs = append(errs, fmt.Errorf("%w; the newest capture is still in flight (it holds the workspace's write lock) and is left to finish; %s", base, recoveryNote(m, c.sess)))
		case ferr != nil:
			errs = append(errs, fmt.Errorf("%w and the fallback save failed (%v); %s", base, ferr, recoveryNote(m, c.sess)))
		default:
			errs = append(errs, fmt.Errorf("%w; the synchronous fallback save kept the session and queued older snapshots were superseded", base))
		}
	}

	// A runtime whose newest capture is in flight must not write the session in its own
	// exit: that synchronous save is the other half of the overwrite race.
	for _, c := range captures {
		if c.inFlight {
			c.sess.rt.skipSave = true
		}
	}
	// Nothing here may keep a runtime alive: close after the durability attempts.
	if active != nil {
		active.rt.exit()
	}
	for _, s := range rest {
		s.rt.exit()
	}
	// The manager is done: drop the sessions so a second close (the test cleanup, a
	// deferred wrapper) is a no-op instead of capturing a closed runtime.
	m.mu.Lock()
	for _, s := range rest {
		delete(m.sessions, s.ws.File.StableID)
	}
	m.mu.Unlock()
	return errors.Join(errs...)
}

// waitExitJob reports whether job finished before the shared deadline. It re-checks once
// after the timer fires so a write that completed at the edge is not reported as a miss.
func waitExitJob(job *saveJob, deadline time.Time) bool {
	wait := time.Until(deadline)
	if wait <= 0 {
		select {
		case <-job.done:
			return true
		default:
			return false
		}
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-job.done:
		return true
	case <-timer.C:
		select {
		case <-job.done:
			return true
		default:
			return false
		}
	}
}

// tryCommitExitCapture supersedes the workspace's queued snapshots with a synchronous save
// of the live state (the newest state: the UI is gone). It returns committed=false when a
// writer is inside writeSnapshot holding the per-workspace lock: that write holds the
// NEWEST snapshot and must be left to finish, because a fallback write now would be
// overwritten by it later or would race it. The per-workspace lock is exactly what makes
// the answer unambiguous. err is the save's error when the fallback ran.
func (m *workspaceManager) tryCommitExitCapture(s *workspaceSession) (committed bool, err error) {
	m.mu.Lock()
	w := m.writers[s.ws.File.StableID]
	m.mu.Unlock()
	if w == nil {
		// No writer was ever created for this workspace: nothing to serialize with.
		return true, s.rt.save()
	}
	if !w.mu.TryLock() {
		return false, nil
	}
	defer w.mu.Unlock()
	if w.stopped {
		// An earlier exit fallback already committed; its save is the record.
		return true, nil
	}
	w.stopped = true
	w.committed = w.seq.Load()
	return true, s.rt.save()
}

// recoveryNote writes the runtime's crash-recovery copy for an exit save that could not be
// confirmed and returns the sentence the exit error carries: where the copy is, or why it
// could not be written.
func recoveryNote(m *workspaceManager, s *workspaceSession) string {
	if s.rt.recoveryWriter == nil {
		return "no crash-recovery copy is available"
	}
	if err := s.rt.recoveryWriter.Plan()(); err != nil {
		return fmt.Sprintf("the crash-recovery copy also failed (%v)", err)
	}
	_ = m
	return fmt.Sprintf("a crash-recovery copy was kept in %s", s.rt.recovery)
}
