package app

import (
	"errors"
	"sort"

	"github.com/tbxark/gvim/internal/tui"
)

// quitSessions returns the materialized workspaces in a stable order on the UI goroutine.
func (m *workspaceManager) quitSessions() []*workspaceSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	sessions := make([]*workspaceSession, 0, len(ids))
	for _, id := range ids {
		sessions = append(sessions, m.sessions[id])
	}
	return sessions
}

func (m *workspaceManager) QuitDirtyCount() int {
	count := 0
	for _, s := range m.quitSessions() {
		for i := range s.rt.ed.Tabs() {
			d := s.rt.ed.DocAt(i)
			if d != nil && d.Modified() && !d.SharedMirror() {
				count++
			}
		}
	}
	return count
}

func (m *workspaceManager) QuitNextDirty() (*tui.Model, int) {
	for _, s := range m.quitSessions() {
		for i := range s.rt.ed.Tabs() {
			d := s.rt.ed.DocAt(i)
			if d != nil && d.Modified() && !d.SharedMirror() {
				return s.rt.model, i
			}
		}
	}
	return nil, -1
}

func (m *workspaceManager) QuitActivate(model *tui.Model) error {
	m.mu.Lock()
	var target *workspaceSession
	for id, s := range m.sessions {
		if s.rt.model == model {
			target = s
			m.active = id
			break
		}
	}
	host := m.host
	m.mu.Unlock()
	if target == nil {
		return errors.New("workspace: quit target no longer exists")
	}
	if m.shared != nil {
		m.shared.Focus(target.rt.ed)
	}
	m.activatePlugins(target)
	if host != nil {
		host.Show(model)
	}
	return nil
}

func (m *workspaceManager) QuitDiscardAll() error {
	for _, s := range m.quitSessions() {
		if err := s.rt.model.QuitDiscardModified(); err != nil {
			return err
		}
	}
	return nil
}
