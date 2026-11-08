package app

// Materialized-workspace eviction (#19b follow-up): a daemon keeps at most
// daemon.materialized_workspaces LOCAL workspaces warm. When a switch materializes one over
// the cap, the least-recently-focused non-active workspace is captured to its session
// directory and its runtime/UI is torn down; the registry entry and session data stay, so a
// later select materializes it again from the saved session. The active workspace is never
// evicted, and a workspace whose capture (and synchronous fallback save) fails is kept and
// reported rather than losing unsaved work. Remote/authority workspaces are excluded from
// this slice: they own an SSH runtime and reconnecting it on rematerialize is a separate
// design (they neither count toward the cap nor get evicted).

import (
	"errors"
	"fmt"
	"sort"

	"github.com/tbxark/gvim/internal/tui"
)

// materializedLocalLocked counts the warm local workspaces. Remote workspaces have their own
// runtime and are not part of this cap. Called under m.mu.
func (m *workspaceManager) materializedLocalLocked() int {
	n := 0
	for _, s := range m.sessions {
		if s.ws.File.AuthoritySpec == "" {
			n++
		}
	}
	return n
}

// evictionCandidatesLocked lists the evictable workspaces, least-recently-focused first:
// local, materialized, not the active one, not already being evicted, not skipped by this
// pass. Called under m.mu.
func (m *workspaceManager) evictionCandidatesLocked(skip map[string]bool) []string {
	type candidate struct {
		id    string
		focus int64
	}
	var cands []candidate
	for id, s := range m.sessions {
		if id == m.active || m.evicting[id] || skip[id] {
			continue
		}
		if s.ws.File.AuthoritySpec != "" {
			continue
		}
		f := m.lastFocus[id]
		cands = append(cands, candidate{id: id, focus: f.UnixMilli()})
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].focus != cands[j].focus {
			return cands[i].focus < cands[j].focus
		}
		return cands[i].id < cands[j].id
	})
	ids := make([]string, len(cands))
	for i, c := range cands {
		ids[i] = c.id
	}
	return ids
}

// scheduleEviction runs after a switch committed. It never blocks the UI: a daemon with a
// program queues one command for the eviction pass; startup/tests without a program run it
// inline (the only way to be over the cap there would be a cap of zero, which config
// rejects).
func (m *workspaceManager) scheduleEviction() {
	m.mu.Lock()
	over := m.materializedLocalLocked() > m.materializedCap
	h := m.host
	m.mu.Unlock()
	if !over {
		return
	}
	cmd := tui.WorkspaceCmdOf(func() { m.evictToCap() })
	if h == nil {
		m.evictToCap()
		return
	}
	h.Queue(cmd)
}

// evictToCap evicts least-recently-focused local workspaces until the cap holds. A workspace
// whose capture cannot be made durable is skipped (kept and reported) and the pass tries the
// next candidate: eviction never trades data safety for the bound.
func (m *workspaceManager) evictToCap() {
	m.evictMu.Lock()
	defer m.evictMu.Unlock()
	skipped := map[string]bool{}
	for {
		m.mu.Lock()
		if m.materializedLocalLocked() <= m.materializedCap {
			m.mu.Unlock()
			return
		}
		ids := m.evictionCandidatesLocked(skipped)
		if len(ids) == 0 {
			m.mu.Unlock()
			return
		}
		id := ids[0]
		sess := m.sessions[id]
		if sess == nil {
			m.mu.Unlock()
			continue
		}
		cap := m.materializedCap
		m.evicting[id] = true
		m.mu.Unlock()

		kept, err := m.evictWorkspace(sess, cap)

		m.mu.Lock()
		delete(m.evicting, id)
		m.mu.Unlock()
		if kept {
			skipped[id] = true
			m.notify(err)
		}
	}
}

// evictWorkspace captures and tears down one workspace. kept=true means it was not evicted
// (it became active, or the capture and the fallback save both failed) and err explains why.
func (m *workspaceManager) evictWorkspace(sess *workspaceSession, cap int) (kept bool, err error) {
	label := sess.ws.File.Label
	// Snapshot on the UI goroutine (it reads the editor state); the write itself runs in the
	// session's ordered capture writer, off the UI.
	var job *saveJob
	if !m.runOnUI(func() { job = m.enqueueCapture(sess) }) {
		return true, errors.New("workspace: the daemon is not ready")
	}
	var captureErr error
	if job != nil {
		<-job.done
		captureErr = job.err
	}
	if captureErr != nil {
		// Never evict unsaved work without a durable session: confirm with the synchronous
		// fallback save first. If that cannot be confirmed either, keep the workspace.
		if ferr := sess.rt.save(); ferr != nil {
			return true, fmt.Errorf("cannot evict workspace %q: the capture write failed (%v) and the fallback save failed: %w; the workspace stays materialized", label, captureErr, ferr)
		}
	}
	// Recheck under the lock: a switch that raced the pass must win over eviction.
	m.mu.Lock()
	stillIdle := m.active != sess.ws.File.StableID
	m.mu.Unlock()
	if !stillIdle {
		return true, fmt.Errorf("workspace %q became active; it was not evicted", label)
	}
	// The plugin namespace of this local workspace was flushed when the switch left it (a
	// shared runtime's SwitchWorkspaceState). Closing the model cancels any plugin UI waits
	// it still owns, then closes terminals, documents and the runtime.
	if m.shared != nil {
		m.runOnUI(func() { m.shared.DropEditor(sess.rt.ed) })
	}
	sess.rt.exit()
	m.mu.Lock()
	delete(m.sessions, sess.ws.File.StableID)
	h := m.host
	m.mu.Unlock()

	msg := fmt.Sprintf("Evicted workspace %q from memory (limit %d materialized workspaces)", label, cap)
	if captureErr != nil {
		msg += fmt.Sprintf("; its capture write failed (%v) but the fallback save kept the session", captureErr)
	}
	m.mu.Lock()
	m.lastEviction = msg
	m.mu.Unlock()
	if h != nil {
		h.Notify(msg)
	}
	return false, nil
}

// releasePluginWorkspace drops a torn-down workspace model from the daemon-wide plugin
// host's id registry (task-149): an evicted or closed model's window/buffer ids must stop
// resolving, and a rematerialized workspace is a new model with its own sequence. Cheap,
// goroutine-safe and safe after model.Close.
func (m *workspaceManager) releasePluginWorkspace(sess *workspaceSession) {
	if sess == nil || sess.rt == nil || sess.rt.model == nil || m.opts.PluginShared == nil {
		return
	}
	m.opts.PluginShared.ReleaseWorkspace(sess.rt.model)
}
