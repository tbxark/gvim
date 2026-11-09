package app

// Mixed file+terminal extraction (task-193): move a selection of open file tabs
// and dock terminal tabs into a NEW co-tenant workspace or an EXISTING one over the
// same root. This is a gvim extension (Fresh extracts one tab at a time); the
// data-safety ordering is the round-16 one: detect conflicts first (a read-only
// step, no write), ask before replacing an existing target file state (a same-name
// terminal tab is ADDED alongside, never replaced), then transfer every selected
// tab (dormant: merged sessions/terminal record; materialized: live editor/dock),
// queue and accept the switch, and only then close the source tabs, re-capturing the
// source. No local fallback; Update does no I/O (capture on the UI goroutine, disk
// work in the tea command).

import (
	"errors"
	"fmt"
	"sort"

	"github.com/tbxark/gvim/internal/daemon"
	"github.com/tbxark/gvim/internal/restore"
	"github.com/tbxark/gvim/internal/terminal"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/session"
)

// mixedExtract is the captured selection on its way to a target.
type mixedExtract struct {
	sourceID string
	files    []*extractTab
	terms    []terminal.SavedTab
	termRefs []tui.ExtractTerminalRef
	destID   string
	// mru is the source editor MRU order restricted to the selected file tabs
	// (newest first), carried into the target session.
	mru []session.MruFile
	// searchHistory is the source dock's search-query history when terminal tabs
	// are moved: a new target starts with it and an existing target merges it (the
	// history is dock/window-wide, like Fresh's prompt history).
	searchHistory []string
}

// ExtractMixed captures the selected file and dock terminal tabs for a mixed
// extraction. targetID "" means a new co-tenant; otherwise an existing workspace
// over the same root. replace is false for the first attempt; the command reports
// conflicts before any write through tui.ExtractConflictMsg.
func (m *workspaceManager) ExtractMixed(sel tui.ExtractSelection, targetID string, replace bool) (tui.WorkspaceCmd, error) {
	m.mu.Lock()
	sess := m.sessions[m.active]
	m.mu.Unlock()
	if sess == nil {
		return nil, errors.New("cannot extract: the daemon is not ready")
	}
	ext := &mixedExtract{sourceID: sess.ws.File.StableID, destID: targetID}
	seen := make(map[string]bool, len(sel.Files))
	for _, p := range sel.Files {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		doc := documentForPath(sess.rt.ed, p)
		if doc == nil {
			return nil, fmt.Errorf("cannot extract: %s is not open any more; nothing was moved", p)
		}
		file, err := captureExtractTab(doc, sess.ws.File.StableID, sess.ws.File.Root)
		if err != nil {
			return nil, fmt.Errorf("cannot extract %s: %v; nothing was moved", p, err)
		}
		ext.files = append(ext.files, file)
	}
	pathsSet := make(map[string]bool, len(ext.files))
	for _, f := range ext.files {
		pathsSet[f.path] = true
	}
	ext.mru = extractMru(sess.rt.ed, pathsSet)
	for _, ref := range sel.Terminals {
		tab, ok := sess.rt.model.TerminalTabStateAt(ref.Index)
		if !ok || tab.Name != ref.Name {
			return nil, errors.New("cannot extract: a terminal tab changed; nothing was moved")
		}
		ext.terms = append(ext.terms, tab)
		ext.termRefs = append(ext.termRefs, ref)
	}
	if len(ext.files) == 0 && len(ext.terms) == 0 {
		return nil, errors.New("cannot extract: no tabs were selected")
	}
	if len(ext.terms) > 0 {
		// The dock-wide history belongs to the moved terminal context; capture it on
		// the UI thread with the tab records (no IO).
		ext.searchHistory = sess.rt.model.TerminalSearchHistory()
	}
	if targetID == "" {
		return tui.WorkspaceCmdOf(func() { m.runMixedExtractNew(ext) }), nil
	}
	return tui.WorkspaceCmdWhen(func() any { return m.runMixedExtractExisting(ext, replace) }), nil
}

// mixedConflict is one target collision: a file path already present in the target
// workspace or a terminal tab with the same saved name.
type mixedConflict struct {
	File     string
	Terminal string
}

func (c mixedConflict) String() string {
	if c.Terminal != "" {
		return "terminal \"" + c.Terminal + "\""
	}
	return c.File
}

// runMixedExtractExisting transfers the selection into an existing same-root
// workspace. It returns a tui.ExtractConflictMsg (before any write) when conflicts
// exist and replace is false; the caller asks. A nil return means the work ran.
func (m *workspaceManager) runMixedExtractExisting(ext *mixedExtract, replace bool) any {
	m.mu.Lock()
	source := m.sessions[ext.sourceID]
	active := m.active
	m.mu.Unlock()
	if source == nil || active != ext.sourceID {
		m.notify(errors.New("cannot extract: the active workspace changed while extracting; nothing was moved"))
		return nil
	}
	target, ok, err := m.reg.Get(ext.destID)
	if err != nil || !ok {
		m.notify(errors.New("cannot extract: the target workspace is gone; nothing was moved"))
		return nil
	}
	sourceRoot, err := daemon.CanonicalRoot(source.ws.File.Root)
	if err != nil {
		m.notify(fmt.Errorf("cannot extract: %w", err))
		return nil
	}
	targetRoot, err := daemon.CanonicalRoot(target.File.Root)
	if err != nil || targetRoot != sourceRoot {
		m.notify(errors.New("cannot extract: the target workspace is not over this root; nothing was moved"))
		return nil
	}
	dir, err := m.reg.SessionDir(target.File.StableID)
	if err != nil {
		m.notify(fmt.Errorf("cannot extract: %w", err))
		return nil
	}
	// A protected target terminal record must refuse the whole mixed migration
	// before any write (files would be migrated too), not only the terminal half.
	if marker, protected := restore.TerminalRecordProtected(dir); protected {
		m.notify(fmt.Errorf("cannot extract: the target workspace's terminal record is protected (%s); inspect it and remove the marker before migrating terminals; nothing was moved", marker))
		return nil
	}
	conflicts, err := m.mixedExtractConflicts(target.File.StableID, dir, ext)
	if err != nil {
		m.notify(fmt.Errorf("cannot extract: read the target workspace: %w", err))
		return nil
	}
	if len(conflicts) > 0 && !replace {
		// Read-only conflict detection happened above; the prompt runs before ANY
		// write to the target or the source.
		texts := make([]string, len(conflicts))
		for i, c := range conflicts {
			texts[i] = c.String()
		}
		return tui.ExtractConflictMsg{
			Selection:   selectionOf(ext),
			TargetID:    target.File.StableID,
			TargetLabel: target.File.Label,
			Conflicts:   texts,
		}
	}
	acceptedFiles, acceptedTerms := 0, 0
	for _, f := range ext.files {
		file := f.file
		if f.unsaved != "" {
			name, err := session.StashContent(dir, f.unsaved)
			if err != nil {
				m.notify(fmt.Errorf("cannot extract: stash the unsaved text in the target workspace: %w; %d of %d tabs transferred, sources kept", err, acceptedFiles+acceptedTerms, ext.total()))
				return nil
			}
			file.StashFile = name
		}
		if err := m.acceptExtractTab(target.File.StableID, targetRoot, dir, file, ext.mru); err != nil {
			m.notify(fmt.Errorf("cannot extract: %w; %d of %d tabs transferred, sources kept", err, acceptedFiles+acceptedTerms, ext.total()))
			return nil
		}
		acceptedFiles++
	}
	for _, tab := range ext.terms {
		if err := m.acceptExtractTerminalTab(target, dir, tab); err != nil {
			m.notify(fmt.Errorf("cannot extract: %w; %d of %d tabs transferred, sources kept", err, acceptedFiles+acceptedTerms, ext.total()))
			return nil
		}
		acceptedTerms++
	}
	if acceptedTerms > 0 && len(ext.searchHistory) > 0 {
		if err := m.migrateMixedSearchHistory(target, dir, ext.searchHistory); err != nil {
			m.notify(fmt.Errorf("cannot extract: migrate the workspace search history: %v; %d of %d tabs transferred, sources kept", err, acceptedFiles+acceptedTerms, ext.total()))
			return nil
		}
	}
	m.closeMixedSources(ext, target.File.StableID, target.File.Label)
	return nil
}

// migrateMixedSearchHistory moves the source dock's search-query history (captured
// with the moved terminal tabs) into the target workspace. A materialized target
// merges it into its dock-wide store and captures it; a dormant one merges it into
// the saved terminal record. Both paths are data-only: they start no process, touch
// no PTY and keep the target's own queries first (terminal.MergeSearchHistory
// dedupes, bounds to the newest MaxSearchHistoryEntries; SaveTerminal cleans/bounds
// the record again).
func (m *workspaceManager) migrateMixedSearchHistory(target daemon.Workspace, dir string, history []string) error {
	m.mu.Lock()
	open := m.sessions[target.File.StableID]
	m.mu.Unlock()
	if open != nil {
		var serr error
		if !m.runOnUI(func() {
			m.mu.Lock()
			open := m.sessions[target.File.StableID]
			m.mu.Unlock()
			if open == nil {
				serr = errors.New("the target workspace closed while extracting")
				return
			}
			open.rt.model.MergeTerminalSearchHistory(history)
			m.enqueueCapture(open)
		}) {
			return errors.New("the daemon is not ready")
		}
		return serr
	}
	st, ok, err := restore.LoadTerminal(dir)
	if err != nil {
		return fmt.Errorf("read the target workspace's terminal record: %w", err)
	}
	if !ok {
		st = terminal.SavedState{Version: terminal.SavedStateVersion}
	}
	st.SearchHistory = terminal.MergeSearchHistory(st.SearchHistory, history)
	return restore.SaveTerminal(dir, st)
}

// mixedExtractConflicts lists the target collisions of the selection: file paths
// already in the target session/open editor, and non-empty terminal tab names already
// in its terminal record/dock. It only reads.
func (m *workspaceManager) mixedExtractConflicts(targetID, dir string, ext *mixedExtract) ([]mixedConflict, error) {
	savedPaths := make(map[string]bool)
	if layout, ok, err := session.Load(dir); err != nil {
		return nil, err
	} else if ok {
		for _, f := range layout.Files {
			savedPaths[f.Path] = true
		}
	}
	m.mu.Lock()
	target := m.sessions[targetID]
	m.mu.Unlock()
	termNames := make(map[string]bool)
	if target != nil {
		for _, t := range target.rt.ed.Tabs() {
			if t.Path != "" {
				savedPaths[t.Path] = true
			}
		}
		for _, ref := range target.rt.model.TerminalTabRefs() {
			if ref.Name != "" {
				termNames[ref.Name] = true
			}
		}
	}
	if st, ok, err := restore.LoadTerminal(dir); err != nil {
		return nil, err
	} else if ok {
		for _, tab := range st.Tabs {
			if tab.Name != "" {
				termNames[tab.Name] = true
			}
		}
	}
	var conflicts []mixedConflict
	for _, f := range ext.files {
		if savedPaths[f.path] {
			conflicts = append(conflicts, mixedConflict{File: f.path})
		}
	}
	for _, tab := range ext.terms {
		if tab.Name != "" && termNames[tab.Name] {
			conflicts = append(conflicts, mixedConflict{Terminal: tab.Name})
		}
	}
	return conflicts, nil
}

// runMixedExtractNew writes a new co-tenant with every selected file and terminal
// tab, then switches and closes the sources only after the switch was accepted.
func (m *workspaceManager) runMixedExtractNew(ext *mixedExtract) {
	m.mu.Lock()
	source := m.sessions[ext.sourceID]
	active := m.active
	m.mu.Unlock()
	if source == nil || active != ext.sourceID {
		m.notify(errors.New("cannot extract: the active workspace changed while extracting; nothing was moved"))
		return
	}
	canonical, err := daemon.CanonicalRoot(source.ws.File.Root)
	if err != nil {
		m.notify(fmt.Errorf("cannot extract: %w", err))
		return
	}
	ws, _, err := m.reg.CreateCoTenant(canonical, "")
	if err != nil {
		m.notify(fmt.Errorf("cannot extract: create the co-tenant workspace: %w", err))
		return
	}
	id := ws.File.StableID
	discard := func() { _ = m.reg.Remove(id) }
	dir, err := m.reg.SessionDir(id)
	if err != nil {
		discard()
		m.notify(fmt.Errorf("cannot extract: %w", err))
		return
	}
	layout := session.Session{Version: session.Version, Root: canonical, Explorer: true}
	for _, f := range ext.files {
		file := f.file
		if f.unsaved != "" {
			name, err := session.StashContent(dir, f.unsaved)
			if err != nil {
				discard()
				m.notify(fmt.Errorf("cannot extract: stash the unsaved text: %w", err))
				return
			}
			file.StashFile = name
		}
		layout.Files = append(layout.Files, file)
	}
	layout.MRU = ext.mru
	layout.Active = activeForMru(layout.Files, ext.mru)
	if err := session.Save(dir, layout); err != nil {
		discard()
		m.notify(fmt.Errorf("cannot extract: write the new workspace's session: %w", err))
		return
	}
	_, _ = m.reg.UpdateLayout(id, &layout)
	if len(ext.terms) > 0 {
		st := terminal.SavedState{
			Version: terminal.SavedStateVersion, Open: true,
			Tabs: append([]terminal.SavedTab(nil), ext.terms...),
			// The new workspace's dock inherits the migrated search-query history
			// (bounded/cleaned by SaveTerminal; no process is involved).
			SearchHistory: append([]string(nil), ext.searchHistory...),
		}
		if err := restore.SaveTerminal(dir, st); err != nil {
			discard()
			m.notify(fmt.Errorf("cannot extract: write the new workspace's terminal record: %w", err))
			return
		}
	}
	m.closeMixedSources(ext, id, ws.File.Label)
}

// closeMixedSources queues the switch and closes the selected source tabs only after
// it was accepted, then re-captures the source. A tab that changed meanwhile is
// skipped and reported (its text/record is already in the target).
func (m *workspaceManager) closeMixedSources(ext *mixedExtract, targetID, targetLabel string) {
	var wait func() error
	var serr error
	closedFiles, closedTerms := 0, 0
	if !m.runOnUI(func() {
		m.mu.Lock()
		cur := m.sessions[ext.sourceID]
		activeNow := m.active
		m.mu.Unlock()
		if activeNow != ext.sourceID || cur == nil {
			serr = errors.New("the active workspace changed while extracting")
			return
		}
		wait, serr = m.beginExtractSelect(targetID)
		if serr != nil {
			return
		}
		for _, f := range ext.files {
			if cur.rt.model.CloseTabForWorkspaceExtractAt(f.path) {
				closedFiles++
			}
		}
		// Terminal indices shift as tabs close: close from the highest index down and
		// verify each tab's saved name, so a changed tab is skipped rather than
		// closing the wrong one.
		refs := append([]tui.ExtractTerminalRef(nil), ext.termRefs...)
		sort.Slice(refs, func(i, j int) bool { return refs[i].Index > refs[j].Index })
		for _, ref := range refs {
			if cur.rt.model.CloseTerminalTabAtForExtract(ref.Index, ref.Name) {
				closedTerms++
			}
		}
		if closedFiles+closedTerms > 0 {
			m.enqueueCapture(cur)
		}
	}) {
		m.notify(errors.New("cannot extract: the daemon is not ready; nothing was closed"))
		return
	}
	if serr != nil {
		m.notify(fmt.Errorf("cannot extract: %w; nothing was closed (any transferred copies are kept in workspace %q)", serr, targetLabel))
		return
	}
	if wait != nil {
		if err := wait(); err != nil {
			m.notify(fmt.Errorf("cannot extract: open workspace %q: %v; the transferred tabs are kept there", targetLabel, err))
			return
		}
	}
	if skipped := ext.total() - closedFiles - closedTerms; skipped > 0 {
		m.report(appTextVars("gvim.workspace.report_tabs_changed", "Extracted %{count} tabs into workspace %{label}; %{changed} changed and stayed open", map[string]any{"count": ext.total() - skipped, "label": fmt.Sprintf("%q", targetLabel), "changed": skipped}))
		return
	}
	m.report(appTextVars("gvim.workspace.report_tabs", "Extracted %{count} tabs into workspace %{label}", map[string]any{"count": ext.total(), "label": fmt.Sprintf("%q", targetLabel)}))
}

// selectionOf rebuilds the TUI selection from the captured extraction (for the
// conflict report; the selection is the same one the user made).
func selectionOf(ext *mixedExtract) tui.ExtractSelection {
	sel := tui.ExtractSelection{Terminals: append([]tui.ExtractTerminalRef(nil), ext.termRefs...)}
	for _, f := range ext.files {
		sel.Files = append(sel.Files, f.path)
	}
	return sel
}

// total is the number of selected tabs.
func (e *mixedExtract) total() int { return len(e.files) + len(e.terms) }
